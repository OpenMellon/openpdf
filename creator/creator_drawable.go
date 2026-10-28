package creator

import (
	"errors"
	"fmt"
	"reflect"

	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// Margins represents page margins or margins around an element.
type Margins struct {
	Left   float64
	Right  float64
	Top    float64
	Bottom float64
}

// Vertical returns total vertical (top + bottom) margin.
func (m *Margins) Vertical() float64 { return m.Bottom + m.Top }

func parseMargins(s string) (Margins, error) {
	floatFields, err := parseFloatFields(s)
	var margins Margins
	switch len(floatFields) {
	case 0:
	case 1:
		margins.Top, margins.Bottom, margins.Left, margins.Right = floatFields[0], floatFields[0], floatFields[0], floatFields[0]
	case 2:
		margins.Top, margins.Bottom = floatFields[0], floatFields[0]
		margins.Left, margins.Right = floatFields[1], floatFields[1]
	case 3:
		margins.Top = floatFields[0]
		margins.Left, margins.Right = floatFields[1], floatFields[1]
		margins.Bottom = floatFields[2]
	case 4:
		margins.Top, margins.Right, margins.Bottom, margins.Left = floatFields[0], floatFields[1], floatFields[2], floatFields[3]
	default:
		if err == nil {
			err = fmt.Errorf("margin expects 1-4 values, got %d", len(floatFields))
		}
	}
	return margins, err
}

// Horizontal returns total horizontal (left + right) margin.
func (m *Margins) Horizontal() float64 { return m.Left + m.Right }

type taggedDrawable struct {
	structType model.StructureType
	tagInfo    *model.StructureTagInfo
}

// GetStructureType returns the structure type of this drawable.
func (td *taggedDrawable) GetStructureType() model.StructureType { return td.tagInfo.StructureType }

// GenerateKDict generates a K dictionary for the drawable component.
func (td *taggedDrawable) GenerateKDict() (*model.KDict, error) {
	if td.tagInfo == nil {
		return nil, errors.New("structure tag info is not set")
	}
	return td.tagInfo.GenerateKDict(), nil
}

// SetStructPageNumber sets the page object where the structure element for this drawable is located.
func (td *taggedDrawable) SetStructPageNumber(pageNumber *int64) {
	if td.tagInfo == nil {
		td.tagInfo = model.NewStructureTagInfo()
		td.tagInfo.StructureType = td.structType
	}
	td.tagInfo.StructPageNumber = pageNumber
}

// SetStructureType sets the structure type for the drawable.
func (td *taggedDrawable) SetStructureType(structureType model.StructureType) {
	if td.tagInfo == nil {
		td.tagInfo = model.NewStructureTagInfo()
	}
	td.tagInfo.StructureType = structureType
}

// SetMarkedContentID sets marked content ID.
func (td *taggedDrawable) SetMarkedContentID(mcid int64) {
	if td.tagInfo == nil {
		td.tagInfo = model.NewStructureTagInfo()
		td.tagInfo.StructureType = td.structType
	}
	td.tagInfo.Mcid = mcid
}

// Positioning represents the positioning type for drawing creator components (relative/absolute).
type Positioning int

// IsAbsolute checks if the positioning is absolute.
func (pos Positioning) IsAbsolute() bool { return pos == PositionAbsolute }

// IsRelative checks if the positioning is relative.
func (pos Positioning) IsRelative() bool { return pos == PositionRelative }

const (
	PositionRelative Positioning = iota
	PositionAbsolute
)

var (
	positioningValues             = map[string]Positioning{"relative": PositionRelative, "absolute": PositionAbsolute}
	textRenderingModeValues       = map[string]TextRenderingMode{"fill": TextRenderingModeFill, "stroke": TextRenderingModeStroke, "fill-stroke": TextRenderingModeFillStroke, "invisible": TextRenderingModeInvisible, "fill-clip": TextRenderingModeFillClip, "stroke-clip": TextRenderingModeStrokeClip, "fill-stroke-clip": TextRenderingModeFillStrokeClip, "clip": TextRenderingModeClip}
	textAlignmentValues           = map[string]TextAlignment{"left": TextAlignmentLeft, "right": TextAlignmentRight, "center": TextAlignmentCenter, "justify": TextAlignmentJustify}
	textVerticalAlignmentValues   = map[string]TextVerticalAlignment{"baseline": TextVerticalAlignmentBaseline, "center": TextVerticalAlignmentCenter}
	textOverflowValues            = map[string]TextOverflow{"visible": TextOverflowVisible, "hidden": TextOverflowHidden}
	fitModeValues                 = map[string]FitMode{"none": FitModeNone, "fill-width": FitModeFillWidth}
	lineStyleValues               = map[string]contentdraw.LineStyle{"solid": contentdraw.LineStyleSolid, "dashed": contentdraw.LineStyleDashed}
	cellHorizontalAlignmentValues = map[string]CellHorizontalAlignment{"left": CellHorizontalAlignmentLeft, "center": CellHorizontalAlignmentCenter, "right": CellHorizontalAlignmentRight}
	cellVerticalAlignmentValues   = map[string]CellVerticalAlignment{"top": CellVerticalAlignmentTop, "middle": CellVerticalAlignmentMiddle, "bottom": CellVerticalAlignmentBottom}
	cellBorderStyleValues         = map[string]CellBorderStyle{"none": CellBorderStyleNone, "single": CellBorderStyleSingle, "double": CellBorderStyleDouble}
	horizontalAlignmentValues     = map[string]HorizontalAlignment{"left": HorizontalAlignmentLeft, "center": HorizontalAlignmentCenter, "right": HorizontalAlignmentRight}
	encoderFactories              = map[string]func() core.StreamEncoder{"flate": func() core.StreamEncoder { return core.NewFlateEncoder() }, "dct": func() core.StreamEncoder { return core.NewDCTEncoder() }}
)

// Background contains properties related to the background of a component.
type Background struct {
	FillColor               Color
	BorderColor             Color
	BorderSize              float64
	BorderRadiusTopLeft     float64
	BorderRadiusTopRight    float64
	BorderRadiusBottomLeft  float64
	BorderRadiusBottomRight float64
}

var backgroundAttrs = map[string]attrApplyFunc{"fill-color": colorAttr(func(background *Background, fillColor Color) { background.FillColor = fillColor }), "border-color": colorAttr(func(background *Background, borderColor Color) { background.BorderColor = borderColor }), "border-size": floatAttr(func(background *Background, borderSize float64) { background.BorderSize = borderSize }), "border-radius": borderRadiusAttr(func(background *Background, topLeft, topRight, bottomRight, bottomLeft float64) {
	background.SetBorderRadius(topLeft, topRight, bottomLeft, bottomRight)
}), "border-top-left-radius": floatAttr(func(background *Background, borderRadiusTopLeft float64) {
	background.BorderRadiusTopLeft = borderRadiusTopLeft
}), "border-top-right-radius": floatAttr(func(background *Background, borderRadiusTopRight float64) {
	background.BorderRadiusTopRight = borderRadiusTopRight
}), "border-bottom-left-radius": floatAttr(func(background *Background, borderRadiusBottomLeft float64) {
	background.BorderRadiusBottomLeft = borderRadiusBottomLeft
}), "border-bottom-right-radius": floatAttr(func(background *Background, borderRadiusBottomRight float64) {
	background.BorderRadiusBottomRight = borderRadiusBottomRight
})}

func parseBackgroundTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseBackground(node)
}

// SetBorderRadius sets the radius of the background corners.
func (ba *Background) SetBorderRadius(topLeft, topRight, bottomLeft, bottomRight float64) {
	ba.BorderRadiusTopLeft = topLeft
	ba.BorderRadiusTopRight = topRight
	ba.BorderRadiusBottomLeft = bottomLeft
	ba.BorderRadiusBottomRight = bottomRight
}

type containerDrawable interface {
	Drawable

	// ContainerComponent checks if the component is allowed to be added into provided 'container' and returns
	// preprocessed copy of itself. If the component is not changed it is allowed to return itself in a callback way.
	// If the component is not compatible with provided container this method should return an error.
	ContainerComponent(drawable Drawable) (Drawable, error)
}

// Drawable is a widget that can be used to draw with the Creator.
type Drawable interface {
	// GeneratePageBlocks draw onto blocks representing Page contents. As the content can wrap over many pages, multiple
	// templates are returned, one per Page.  The function also takes a draw context containing information
	// where to draw (if relative positioning) and the available height to draw on accounting for Margins etc.
	GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error)

	// SetMarkedContentID sets the marked content id for the drawable.
	SetMarkedContentID(mcid int64)

	// SetStructureType sets the structure type for the drawable.
	SetStructureType(structType model.StructureType)

	// GenerateKDict generates a K dictionary for the drawable.
	GenerateKDict() (*model.KDict, error)

	// SetStructPageNumber sets the page number where the structure element for this drawable is located.
	SetStructPageNumber(pageNum *int64)
}

func drawableHeight(drawable VectorDrawable, left float64) float64 {
	switch para := drawable.(type) {
	case *StyledParagraph:
		if para.enableWrap {
			para.SetWidth(left - para.margins.Left - para.margins.Right)
		}
		return para.Height() + para.margins.Top + para.margins.Bottom + (0.5 * para.getTextHeight())
	case *Image:
		para.applyFitMode(left)
		return para.Height() + para.margins.Top + para.margins.Bottom
	case *Rectangle:
		para.applyFitMode(left)
		return para.Height() + para.margins.Top + para.margins.Bottom + para.borderWidth
	case *Ellipse:
		para.applyFitMode(left)
		return para.Height() + para.margins.Top + para.margins.Bottom
	case *Division:
		return para.ctxHeight(left) + para.margins.Top + para.margins.Bottom + para.padding.Top + para.padding.Bottom
	case *Table:
		para.updateRowHeights(left - para.margins.Left - para.margins.Right)
		return para.Height() + para.margins.Top + para.margins.Bottom
	case *List:
		return para.ctxHeight(left) + para.margins.Top + para.margins.Bottom
	case marginDrawable:
		_, _, top, bottom := para.GetMargins()
		return para.Height() + top + bottom
	default:
		return para.Height()
	}
}

var containerTags = map[string]struct{}{}

// HorizontalAlignment represents the horizontal alignment of components
// within a page.
type HorizontalAlignment int

const (
	HorizontalAlignmentLeft HorizontalAlignment = iota
	HorizontalAlignmentCenter
	HorizontalAlignmentRight
)

// FitMode defines resizing options of an object inside a container.
type FitMode int

const (
	FitModeNone FitMode = iota
	FitModeFillWidth
)

type componentRenderer interface{ Draw(drawable Drawable) error }

// VectorDrawable is a Drawable with a specified width and height.
type VectorDrawable interface {
	Drawable

	// Width returns the width of the Drawable.
	Width() float64

	// Height returns the height of the Drawable.
	Height() float64
}

// AnchorPoint defines anchor point where the center position of the radial gradient would be calculated.
type AnchorPoint int

const (
	AnchorBottomLeft AnchorPoint = iota
	AnchorBottomRight
	AnchorTopLeft
	AnchorTopRight
	AnchorCenter
	AnchorLeft
	AnchorRight
	AnchorTop
	AnchorBottom
)

func marginAttr(i any) attrApplyFunc {
	value2 := validateSetter("marginAttr", i, 0, marginsType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		margins, err := parseMargins(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
		}
		callSetter(value2, target, reflect.ValueOf(margins))
		return nil
	}
}

type marginDrawable interface {
	VectorDrawable
	GetMargins() (float64, float64, float64, float64)
}
