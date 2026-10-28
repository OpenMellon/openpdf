package extractor

import (
	"errors"
	"fmt"
	"image/color"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

func (to *textObject) setHorizScaling(scale float64) {
	if to == nil {
		return
	}
	to.state.th = scale
}

func (to *textObject) setTextRise(rise float64) {
	if to == nil {
		return
	}
	to.state.trise = rise
}

func (to *textObject) advanceTextMatrix(buf []byte) {
	if to.unsupportedFont {
		return
	}
	font := to.getCurrentFont()
	codes := font.BytesToCharcodes(buf)
	state := to.state
	tfs := state.tfs
	hScale := state.th / 100.0
	glyphUnit := glyphSpaceUnit
	if font.Subtype() == "Type3" {
		glyphUnit = 1
	}
	for _, code := range codes {
		if code == 0 {
			continue
		}
		wordSpace := 0.0
		if code == 32 {
			wordSpace = state.tw
		}
		metrics, ok := font.GetCharMetrics(code)
		if !ok {
			continue
		}
		glyphDisp := transform.Point{X: metrics.Wx * glyphUnit, Y: metrics.Wy * glyphUnit}
		shift := transform.Point{X: (glyphDisp.X*tfs + state.tc + wordSpace) * hScale}
		trans := translateBy(shift)
		to.tm.Concat(trans)
	}
}
func (to *textObject) moveText(tx, ty float64) { to.moveLP(tx, ty) }

func (to *textObject) showText(obj core.PdfObject, raw []byte, mcid int, actualText string) error {
	return to.renderText(obj, raw, mcid, actualText)
}

func (to *textObject) setCharSpacing(spacing float64) {
	if to == nil {
		return
	}
	to.state.tc = spacing
	if debugTextState {
		common.Log.Info("setCharSpacing: %.2f state=%s", spacing, to.state.String())
	}
}
func (to *textObject) nextLine() { to.moveLP(0, -to.state.tl) }

func (to *textObject) getFillColor() color.Color {
	return colorToRGB(to.gs.ColorspaceNonStroking, to.gs.ColorNonStroking)
}

func (to *textObject) setTextRenderMode(mode int) {
	if to == nil {
		return
	}
	to.state.tmode = RenderMode(mode)
}

func (to *textObject) reset() {
	to.tm = transform.IdentityMatrix()
	to.tlm = transform.IdentityMatrix()
	to.marks = nil
}

func (to *textObject) showTextAdjusted(arr *core.PdfObjectArray, mcid int, actualText string) error {
	vertical := false
	rendered := false
	for _, obj := range arr.Elements() {
		switch obj.(type) {
		case *core.PdfObjectFloat, *core.PdfObjectInteger:
			num, err := core.GetNumberAsFloat(obj)
			if err != nil {
				common.Log.Debug("ERROR: showTextAdjusted. Bad numerical arg. o=%s args=%+v", obj, arr)
				return err
			}
			tx, ty := -num*0.001*to.state.tfs*to.state.th/100.0, 0.0
			if vertical {
				ty, tx = tx, ty
			}
			trans := translateBy(transform.Point{X: tx, Y: ty})
			to.tm.Concat(trans)
		case *core.PdfObjectString:
			direct := core.TraceToDirectObject(obj)
			raw, ok := core.GetStringBytes(direct)
			if !ok {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("showTextAdjusted: Bad string arg. o=%s args=%+v", obj, arr)
				}
				return core.ErrTypeError
			}
			if rendered {
				to.advanceTextMatrix(raw)
			} else {
				to.renderText(direct, raw, mcid, actualText)
				if actualText != "" {
					rendered = true
				}
			}
		default:
			common.Log.Debug("ERROR: showTextAdjusted. Unexpected type (%T) args=%+v", obj, arr)
			return core.ErrTypeError
		}
	}
	return nil
}

func (to *textObject) moveLP(tx, ty float64) {
	to.tlm.Concat(transform.NewMatrix(1, 0, 0, 1, tx, ty))
	to.tm = to.tlm
}

func (to *textObject) getStrokeColor() color.Color {
	return colorToRGB(to.gs.ColorspaceStroking, to.gs.ColorStroking)
}

func (to *textObject) setWordSpacing(spacing float64) {
	if to == nil {
		return
	}
	to.state.tw = spacing
}

func (to *textObject) moveTextSetLeading(tx, ty float64) {
	to.state.tl = -ty
	to.moveLP(tx, ty)
}

func (to *textObject) setTextLeading(leading float64) {
	if to == nil {
		return
	}
	to.state.tl = leading
}

func (to *textObject) renderText(obj core.PdfObject, raw []byte, mcid int, actualText string) error {
	if to.unsupportedFont {
		common.Log.Debug("renderText: Invalid font. Not processing.")
		return nil
	}
	font := to.getCurrentFont()
	codes := font.BytesToCharcodes(raw)
	var (
		strs      []string
		numChars  int
		numMisses int
	)
	if actualText != "" {
		allCtrl := true
		for _, r := range actualText {
			if r > 0x1F {
				allCtrl = false
				break
			}
		}
		if allCtrl {
			return nil
		}
		strs = []string{actualText}
		numChars = 1
	} else {
		strs, numChars, numMisses = font.CharcodesToStrings(codes, "")
		if numMisses > 0 {
			common.Log.Debug("renderText: numChars=%d numMisses=%d", numChars, numMisses)
		}
	}
	to.state.numChars += numChars
	to.state.numMisparsedChars += numMisses
	state := to.state
	tfs := state.tfs
	hScale := state.th / 100.0
	glyphUnit := glyphSpaceUnit
	if font.Subtype() == "Type3" {
		glyphUnit = 1
	}
	spaceMetrics, ok := font.GetRuneMetrics(' ')
	if !ok {
		spaceMetrics, ok = font.GetCharMetrics(32)
	}
	if !ok {
		spaceMetrics, _ = model.DefaultFont().GetRuneMetrics(' ')
	}
	spaceWidth := spaceMetrics.Wx * glyphUnit
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("spaceWidth=%.2f text=%q font=%s fontSize=%.2f", spaceWidth, strs, font, tfs)
	}
	trm0 := transform.NewMatrix(tfs*hScale, 0, 0, tfs, 0, state.trise)
	if debugTextState {
		common.Log.Info("renderText: %d codes=%+v texts=%q", len(codes), codes, strs)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("renderText: %d codes=%+v runes=%q", len(codes), codes, len(strs))
	}
	fillColor := to.getFillColor()
	strokeColor := to.getStrokeColor()
	if actualText != "" {
		runes := []rune(strs[0])
		trm := to.gs.CTM.Mult(to.tm).Mult(trm0)
		codeStrs, _, _ := font.CharcodesToStrings(codes, "")
		for i, code := range codes {
			wordSpace := 0.0
			if i < len(codeStrs) {
				if cr := []rune(codeStrs[i]); len(cr) == 1 && cr[0] == 32 {
					wordSpace = state.tw
				}
			}
			metrics, ok2 := font.GetCharMetrics(code)
			if !ok2 {
				continue
			}
			disp := transform.Point{X: metrics.Wx * glyphUnit, Y: metrics.Wy * glyphUnit}
			shift := transform.Point{X: (disp.X*tfs + state.tc + wordSpace) * hScale}
			trans := translateBy(shift)
			to.tm.Concat(trans)
		}
		pos := translationPoint(to.gs.CTM.Mult(to.tm))
		mark, ok3 := to.newTextMark(textencoding.ExpandLigatures(runes), trm, pos, math.Abs(spaceWidth*trm.ScalingFactorX()), font, to.state.tc, fillColor, strokeColor, obj, strs, 0, mcid)
		mark.tfs = tfs
		if ok3 {
			to.marks = append(to.marks, &mark)
		}
		return nil
	}
	for i, str := range strs {
		runes := []rune(str)
		if len(runes) == 1 && runes[0] == '\x00' {
			continue
		}
		code := codes[i]
		trm := to.gs.CTM.Mult(to.tm).Mult(trm0)
		wordSpace := 0.0
		if len(runes) == 1 && runes[0] == 32 {
			wordSpace = state.tw
		}
		metrics, ok2 := font.GetCharMetrics(code)
		if !ok2 {
			common.Log.Debug("ERROR: No metric for code=%d r=0x%04x=%+q %s", code, runes, runes, font)
			return fmt.Errorf("no char metrics: font=%s code=%d", font.String(), code)
		}
		disp := transform.Point{X: metrics.Wx * glyphUnit, Y: metrics.Wy * glyphUnit}
		shiftNoTc := transform.Point{X: (disp.X*tfs + wordSpace) * hScale}
		shift := transform.Point{X: (disp.X*tfs + state.tc + wordSpace) * hScale}
		if debugTextState {
			common.Log.Info("tfs=%.2f tc=%.2f tw=%.2f th=%.2f", tfs, state.tc, state.tw, hScale)
			common.Log.Info("dx,dy=%.3f t0=%.3f t=%.3f", disp, shiftNoTc, shift)
		}
		t0m := translateBy(shiftNoTc)
		advance := translateBy(shift)
		glyphTM := to.gs.CTM.Mult(to.tm).Mult(t0m)
		if debugTextRender {
			common.Log.Info("end:\n\tCTM=%s\n\t tm=%s\n"+"\t td=%s xlat=%s\n"+"\ttd0=%s\n\t  → %s xlat=%s", to.gs.CTM, to.tm, advance, translationPoint(to.gs.CTM.Mult(to.tm).Mult(advance)), t0m, glyphTM, translationPoint(glyphTM))
		}
		mark, ok3 := to.newTextMark(textencoding.ExpandLigatures(runes), trm, translationPoint(glyphTM), math.Abs(spaceWidth*trm.ScalingFactorX()), font, to.state.tc, fillColor, strokeColor, obj, strs, i, mcid)
		mark.tfs = tfs
		to.tm.Concat(advance)
		if !ok3 {
			common.Log.Debug("Text mark outside page. Skipping")
			continue
		}
		if font == nil {
			common.Log.Debug("ERROR: No font.")
		} else if font.Encoder() == nil {
			common.Log.Debug("ERROR: No encoding. font=%s", font)
		} else {
			if r, ok4 := font.Encoder().CharcodeToRune(code); ok4 {
				mark.original = string(r)
			}
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("i=%d code=%d mark=%s trm=%s", i, code, mark, trm)
		}
		to.marks = append(to.marks, &mark)
	}
	return nil
}

func (to *textObject) setTextMatrix(vals []float64) {
	if len(vals) != 6 {
		common.Log.Debug("ERROR: len(f) != 6 (%d)", len(vals))
		return
	}
	a, b, c, d, e, f := vals[0], vals[1], vals[2], vals[3], vals[4], vals[5]
	to.tm = transform.NewMatrix(a, b, c, d, e, f)
	to.tlm = to.tm
}

func (to *textObject) checkOp(op *contentstream.ContentStreamOperation, numParams int, strict bool) (ok bool, err error) {
	if to == nil {
		var params []core.PdfObject
		if numParams > 0 {
			params = op.Params
			if len(params) > numParams {
				params = params[:numParams]
			}
		}
		common.Log.Debug("%#q operand outside text. params=%+v", op.Operand, params)
	}
	if numParams >= 0 {
		if len(op.Params) != numParams {
			if strict {
				err = errors.New("incorrect parameter count")
			}
			common.Log.Debug("ERROR: %#q should have %d input params, got %d %+v", op.Operand, numParams, len(op.Params), op.Params)
			return false, err
		}
	}
	return true, nil
}

func (to *textObject) newTextMark(text string, trm transform.Matrix, end transform.Point, spaceWidth float64, font *model.PdfFont, charSpacing float64, fill, stroke color.Color, obj core.PdfObject, objStrs []string, index int, mcid int) (textMark, bool) {
	angle := trm.Angle()
	orient := roundToMultiple(angle, angleRoundingStep)
	var fontSize float64
	if orient%180 != 90 {
		fontSize = trm.ScalingFactorY()
	} else {
		fontSize = trm.ScalingFactorX()
	}
	start := translationPoint(trm)
	origBBox := model.PdfRectangle{Llx: start.X, Lly: start.Y, Urx: end.X, Ury: end.Y}
	switch orient % 360 {
	case 90:
		origBBox.Urx -= fontSize
	case 180:
		origBBox.Ury -= fontSize
	case 270:
		origBBox.Urx += fontSize
	case 0:
		origBBox.Ury += fontSize
	default:
		orient = 0
		origBBox.Ury += fontSize
	}
	if origBBox.Llx > origBBox.Urx {
		origBBox.Llx, origBBox.Urx = origBBox.Urx, origBBox.Llx
	}
	if origBBox.Lly > origBBox.Ury {
		origBBox.Lly, origBBox.Ury = origBBox.Ury, origBBox.Lly
	}
	inPage := true
	if to.extractor.mediaBox.Width() > 0 {
		clipped, overlap := intersectRects(origBBox, to.extractor.mediaBox)
		if !overlap {
			inPage = false
			common.Log.Debug("Text mark outside page. bbox=%g mediaBox=%g text=%q", origBBox, to.extractor.mediaBox, text)
		}
		origBBox = clipped
	}
	bbox := origBBox
	mb := to.extractor.mediaBox
	switch orient % 360 {
	case 90:
		mb.Urx, mb.Ury = mb.Ury, mb.Urx
		bbox = model.PdfRectangle{Llx: mb.Urx - origBBox.Ury, Urx: mb.Urx - origBBox.Lly, Lly: origBBox.Llx, Ury: origBBox.Urx}
	case 180:
		bbox = model.PdfRectangle{Llx: mb.Urx - origBBox.Llx, Urx: mb.Urx - origBBox.Urx, Lly: mb.Ury - origBBox.Lly, Ury: mb.Ury - origBBox.Ury}
	case 270:
		mb.Urx, mb.Ury = mb.Ury, mb.Urx
		bbox = model.PdfRectangle{Llx: origBBox.Ury, Urx: origBBox.Lly, Lly: mb.Ury - origBBox.Llx, Ury: mb.Ury - origBBox.Urx}
	}
	if bbox.Llx > bbox.Urx {
		bbox.Llx, bbox.Urx = bbox.Urx, bbox.Llx
	}
	if bbox.Lly > bbox.Ury {
		bbox.Lly, bbox.Ury = bbox.Ury, bbox.Lly
	}
	mark := textMark{text: text, PdfRectangle: bbox, origBBox: origBBox, font: font, fontSize: fontSize, charSpacing: charSpacing, trm: trm, end: end, orientation: orient, fillColor: fill, strokeColor: stroke, directObject: obj, objString: objStrs, Th: to.state.th, Tw: to.state.tw, mcid: mcid, index: index}
	if debugWords {
		common.Log.Info("newTextMark: start=%.2f end=%.2f %s", start, end, mark.String())
	}
	return mark, inPage
}

type textObject struct {
	extractor       *Extractor
	resources       *model.PdfPageResources
	gs              contentstream.GraphicsState
	state           *textState
	savedStates     *stateStack
	tm              transform.Matrix
	tlm             transform.Matrix
	marks           []*textMark
	unsupportedFont bool
}

func newTextObject(ex *Extractor, resources *model.PdfPageResources, gs contentstream.GraphicsState, state *textState, stack *stateStack) *textObject {
	return &textObject{extractor: ex, resources: resources, gs: gs, savedStates: stack, state: state, tm: transform.IdentityMatrix(), tlm: transform.IdentityMatrix()}
}
