package contentstream

import (
	"errors"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

func isKnownOperand(s string) bool { _, ok := knownOperands[s]; return ok }

type handlerEntry struct {
	Condition HandlerConditionEnum
	Operand   string
	Handler   HandlerFunc
}

// ContentStreamProcessor defines a data structure and methods for processing a content stream, keeping track of the
// current graphics state, and allowing external handlers to define their own functions as a part of the processing,
// for example rendering or extracting certain information.
type ContentStreamProcessor struct {
	gsStack              GraphicStateStack
	operations           []*ContentStreamOperation
	graphicsState        GraphicsState
	initialGraphicsState *GraphicsState
	handlers             []handlerEntry
	currentIndex         int
	relaxedMode          bool
}

// SetRelaxedMode sets the relaxed mode for the content stream processor.
func (proc *ContentStreamProcessor) SetRelaxedMode(val bool) { proc.relaxedMode = val }

// HandlerFunc is the function syntax that the ContentStreamProcessor handler must implement.
type HandlerFunc func(op *ContentStreamOperation, gs GraphicsState, resources *model.PdfPageResources) error

// AddHandler adds a new ContentStreamProcessor `handler` of type `condition` for `operand`.
func (proc *ContentStreamProcessor) AddHandler(condition HandlerConditionEnum, operand string, handler HandlerFunc) {
	v := handlerEntry{}
	v.Condition = condition
	v.Operand = operand
	v.Handler = handler
	proc.handlers = append(proc.handlers, v)
}

// Process processes the entire list of operations. Maintains the graphics state that is passed to any
// handlers that are triggered during processing (either on specific operators or all).
func (proc *ContentStreamProcessor) Process(resources *model.PdfPageResources) error {
	proc.graphicsState.ColorspaceStroking = model.NewPdfColorspaceDeviceGray()
	proc.graphicsState.ColorspaceNonStroking = model.NewPdfColorspaceDeviceGray()
	proc.graphicsState.ColorStroking = model.NewPdfColorDeviceGray(0)
	proc.graphicsState.ColorNonStroking = model.NewPdfColorDeviceGray(0)
	proc.graphicsState.CTM = transform.IdentityMatrix()
	if init := proc.initialGraphicsState; init != nil {
		if init.ColorspaceStroking != nil {
			proc.graphicsState.ColorspaceStroking = init.ColorspaceStroking
			if init.ColorStroking != nil {
				proc.graphicsState.ColorStroking = init.ColorStroking
			} else {
				col, err := proc.getInitialColor(init.ColorspaceStroking)
				if err != nil {
					return err
				}
				proc.graphicsState.ColorStroking = col
			}
		}
		if init.ColorspaceNonStroking != nil {
			proc.graphicsState.ColorspaceNonStroking = init.ColorspaceNonStroking
			if init.ColorNonStroking != nil {
				proc.graphicsState.ColorNonStroking = init.ColorNonStroking
			} else {
				col, err := proc.getInitialColor(init.ColorspaceNonStroking)
				if err != nil {
					return err
				}
				proc.graphicsState.ColorNonStroking = col
			}
		}
		if init.CTM != (transform.Matrix{}) {
			proc.graphicsState.CTM = init.CTM
		}
	}
	for _, op := range proc.operations {
		var err error
		switch op.Operand {
		case "q":
			proc.gsStack.Push(proc.graphicsState)
		case "Q":
			if len(proc.gsStack) == 0 {
				common.Log.Debug("WARN: invalid `Q` operator. Graphics state stack is empty. Skipping.")
				continue
			}
			proc.graphicsState = proc.gsStack.Pop()
		case "CS":
			err = proc.handleCommand_CS(op, resources)
		case "cs":
			err = proc.handleCommand_cs(op, resources)
		case "SC":
			err = proc.handleCommand_SC(op, resources)
		case "SCN":
			err = proc.handleCommand_SCN(op, resources)
		case "sc":
			err = proc.handleCommand_sc(op, resources)
		case "scn":
			err = proc.handleCommand_scn(op, resources)
		case "G":
			err = proc.handleCommand_G(op, resources)
		case "g":
			err = proc.handleCommand_g(op, resources)
		case "RG":
			err = proc.handleCommand_RG(op, resources)
		case "rg":
			err = proc.handleCommand_rg(op, resources)
		case "K":
			err = proc.handleCommand_K(op, resources)
		case "k":
			err = proc.handleCommand_k(op, resources)
		case "cm":
			err = proc.handleCommand_cm(op, resources)
		}
		if err != nil {
			common.Log.Debug("Processor handling error (%s): %v", op.Operand, err)
			common.Log.Debug("Operand: %#v", op.Operand)
			return err
		}
		for _, v := range proc.handlers {
			var err2 error
			if v.Condition.All() {
				err2 = v.Handler(op, proc.graphicsState, resources)
			} else if v.Condition.Operand() && op.Operand == v.Operand {
				err2 = v.Handler(op, proc.graphicsState, resources)
			}
			if err2 != nil {
				common.Log.Debug("Processor handler error: %v", err2)
				return err2
			}
		}
	}
	return nil
}

// Operand returns true if `hce` is equivalent to HandlerConditionEnumOperand.
func (v HandlerConditionEnum) Operand() bool { return v == HandlerConditionEnumOperand }

// HandlerConditionEnum represents the type of operand content stream processor (handler).
// The handler may process a single specific named operand or all operands.
type HandlerConditionEnum int

// GraphicsState is a basic graphics state implementation for PDF processing.
// Initially only implementing and tracking a portion of the information specified. Easy to add more.
type GraphicsState struct {
	ColorspaceStroking    model.PdfColorspace
	ColorspaceNonStroking model.PdfColorspace
	ColorStroking         model.PdfColor
	ColorNonStroking      model.PdfColor
	CTM                   transform.Matrix
}

var errInvalidNumberOfParams = errors.New("invalid number of parameters")

// Transform returns coordinates x, y transformed by the CTM.
func (gs *GraphicsState) Transform(x, y float64) (float64, float64) {
	return gs.CTM.Transform(x, y)
}

// Push pushes `gs` on the `gsStack`.
func (stack *GraphicStateStack) Push(gs GraphicsState) { *stack = append(*stack, gs) }

// Pop pops and returns the topmost GraphicsState off the `gsStack`.
func (stack *GraphicStateStack) Pop() GraphicsState {
	gs := (*stack)[len(*stack)-1]
	*stack = (*stack)[:len(*stack)-1]
	return gs
}

// GraphicStateStack represents a stack of GraphicsState.
type GraphicStateStack []GraphicsState

var (
	ErrInvalidOperand = errors.New("invalid operand")
	ErrEarlyExit      = errors.New("terminate process early exit")
)

// NewContentStreamProcessor returns a new ContentStreamProcessor for operations `ops`.
func NewContentStreamProcessor(ops []*ContentStreamOperation) *ContentStreamProcessor {
	v := ContentStreamProcessor{}
	v.gsStack = GraphicStateStack{}
	gs := GraphicsState{}
	v.graphicsState = gs
	v.handlers = []handlerEntry{}
	v.currentIndex = 0
	v.operations = ops
	return &v
}

// SetInitialGraphicsState sets the graphics state that Process starts from
// instead of the PDF defaults (DeviceGray colorspaces, black colors, identity
// CTM). Use it for a stream that inherits the caller's state, such as a form
// XObject's content stream (PDF32000_2008 8.10.1). Nil fields and a zero CTM
// fall back to the defaults; a colorspace seeded without its color gets that
// colorspace's initial color, and a color seeded without its colorspace is
// ignored.
func (proc *ContentStreamProcessor) SetInitialGraphicsState(gs GraphicsState) {
	proc.initialGraphicsState = &gs
}

const (
	HandlerConditionEnumOperand HandlerConditionEnum = iota
	HandlerConditionEnumAllOperands
)

var knownOperands = map[string]struct{}{"b": {}, "B": {}, "b*": {}, "B*": {}, "BDC": {}, "BI": {}, "BMC": {}, "BT": {}, "BX": {}, "c": {}, "cm": {}, "CS": {}, "cs": {}, "d": {}, "d0": {}, "d1": {}, "Do": {}, "DP": {}, "EI": {}, "EMC": {}, "ET": {}, "EX": {}, "f": {}, "F": {}, "f*": {}, "G": {}, "g": {}, "gs": {}, "h": {}, "i": {}, "ID": {}, "j": {}, "J": {}, "K": {}, "k": {}, "l": {}, "m": {}, "M": {}, "MP": {}, "n": {}, "q": {}, "Q": {}, "re": {}, "RG": {}, "rg": {}, "ri": {}, "s": {}, "S": {}, "SC": {}, "sc": {}, "SCN": {}, "scn": {}, "sh": {}, "T*": {}, "Tc": {}, "Td": {}, "TD": {}, "Tf": {}, "Tj": {}, "TJ": {}, "TL": {}, "Tm": {}, "Tr": {}, "Ts": {}, "Tw": {}, "Tz": {}, "v": {}, "w": {}, "W": {}, "W*": {}, "y": {}, "'": {}, "\"": {}}

// All returns true if `hce` is equivalent to HandlerConditionEnumAllOperands.
func (v HandlerConditionEnum) All() bool { return v == HandlerConditionEnumAllOperands }
