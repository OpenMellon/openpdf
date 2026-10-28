// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package annotator provides an interface for creating annotations with appearance
// streams.  It goes beyond the models package which includes definitions of basic annotation models, in that it
// can create the appearance streams which specify the exact appearance as needed by many pdf viewers for consistent
// appearance of the annotations.
// It also contains methods for generating appearance streams for fields via widget annotations.
package annotator

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/creator"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/internal/timeutils"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/textshaping"
)

func rectangleAppearance(def RectangleAnnotationDef) (*core.PdfObjectDictionary, *model.PdfRectangle, error) {
	form := model.NewXObjectForm()
	form.Resources = model.NewPdfPageResources()
	gsName := ""
	if def.Opacity < 1.0 {
		gsDict := core.MakeDict()
		gsDict.Set("ca", core.MakeFloat(def.Opacity))
		gsDict.Set("CA", core.MakeFloat(def.Opacity))
		err := form.Resources.AddExtGState("gs1", gsDict)
		if err != nil {
			common.Log.Debug("Unable to add extgstate gs1")
			return nil, nil, err
		}
		gsName = "gs1"
	}
	content, bbox, rect, err := rectangleAppearanceStream(def, gsName)
	if err != nil {
		return nil, nil, err
	}
	err = form.SetContentStream(content, nil)
	if err != nil {
		return nil, nil, err
	}
	form.BBox = bbox.ToPdfObject()
	apDict := core.MakeDict()
	apDict.Set("N", form.ToPdfObject())
	return apDict, rect, nil
}

func inkAppearanceStream(inkDef *InkAnnotationDef) ([]byte, *model.PdfRectangle, error) {
	allCurves := [][]contentdraw.CubicBezierCurve{}
	for _, path := range inkDef.Paths {
		if path.Length() == 0 {
			continue
		}
		points := path.Points
		ctrl1, ctrl2, err := bezierControlPoints(points)
		if err != nil {
			return nil, nil, err
		}
		if len(ctrl1) != len(ctrl2) {
			return nil, nil, errors.New("inequal number of calculated first and second control point")
		}
		curves := []contentdraw.CubicBezierCurve{}
		for i := 0; i < len(ctrl1); i++ {
			curves = append(curves, contentdraw.CubicBezierCurve{P0: points[i], P1: ctrl1[i], P2: ctrl2[i], P3: points[i+1]})
		}
		if len(curves) > 0 {
			allCurves = append(allCurves, curves)
		}
	}
	content, bbox, err := strokeCurves(allCurves, inkDef.Color, inkDef.LineWidth)
	if err != nil {
		return nil, nil, err
	}
	return content, bbox, nil
}

// SetStyle applies appearance `style` to `fa`.
func (a *ImageFieldAppearance) SetStyle(style AppearanceStyle) { a.style = &style }

type listboxOption struct {
	value string
	label string
}

func circleAppearanceStream(def CircleAnnotationDef, gsName string) ([]byte, *model.PdfRectangle, *model.PdfRectangle, error) {
	circle := contentdraw.Circle{X: def.X, Y: def.Y, Width: def.Width, Height: def.Height, FillEnabled: def.FillEnabled, FillColor: def.FillColor, BorderEnabled: def.BorderEnabled, BorderWidth: def.BorderWidth, BorderColor: def.BorderColor, Opacity: def.Opacity}
	content, bbox, err := circle.Draw(gsName)
	if err != nil {
		return nil, nil, nil, err
	}
	rect := &model.PdfRectangle{}
	rect.Llx = def.X + bbox.Llx
	rect.Lly = def.Y + bbox.Lly
	rect.Urx = def.X + bbox.Urx
	rect.Ury = def.Y + bbox.Ury
	return content, bbox, rect, nil
}

func drawSignatureImage(img image.Image, name string, opts *SignatureFieldOpts, rect []float64, cc *contentstream.ContentCreator) (*core.PdfObjectName, *model.XObjectImage, error) {
	modelImg, err := model.DefaultImageHandler{}.NewImageFromGoImage(img)
	if err != nil {
		return nil, nil, err
	}
	xImg, err := model.NewXObjectImageFromImage(modelImg, nil, opts.Encoder)
	if err != nil {
		return nil, nil, err
	}
	w, h := float64(*xImg.Width), float64(*xImg.Height)
	rectW := rect[2] - rect[0]
	rectH := rect[3] - rect[1]
	x, y := rect[0], rect[1]
	if opts.AutoSize {
		scale := math.Min(rectW/w, rectH/h)
		w *= scale
		h *= scale
		x += (rectW / 2) - (w / 2)
		y += (rectH / 2) - (h / 2)
	}
	var imgName *core.PdfObjectName
	if nm, ok := core.GetName(xImg.Name); ok {
		imgName = nm
	} else {
		imgName = core.MakeName(name)
	}
	if cc != nil {
		cc.Add_q().Translate(x, y).Scale(w, h).Add_Do(*imgName).Add_Q()
	} else {
		return nil, nil, errors.New("ContentCreator is null")
	}
	return imgName, xImg, nil
}

func signatureAppearance(lines []*SignatureLine, opts *SignatureFieldOpts) (*core.PdfObjectDictionary, error) {
	if opts == nil {
		opts = NewSignatureFieldOpts()
	}
	var err error
	var fontName *core.PdfObjectName
	font := opts.Font
	if font != nil {
		desc, _ := font.GetFontDescriptor()
		if desc != nil {
			if nm, ok := desc.FontName.(*core.PdfObjectName); ok {
				fontName = nm
			}
		}
		if fontName == nil {
			fontName = core.MakeName("Font1")
		}
	} else {
		if font, err = model.NewStandard14Font("Helvetica"); err != nil {
			return nil, err
		}
		fontName = core.MakeName("Helv")
	}
	fontSize := opts.FontSize
	if fontSize <= 0 {
		fontSize = 10
	}
	if opts.LineHeight <= 0 {
		opts.LineHeight = 1
	}
	leading := opts.LineHeight * fontSize
	spaceMetrics, ok := font.GetRuneMetrics(' ')
	if !ok {
		return nil, errors.New("the font does not have a space glyph")
	}
	spaceWidth := spaceMetrics.Wx
	var maxWidth float64
	var textLines []string
	for _, line := range lines {
		if line.Text == "" {
			continue
		}
		text := line.Text
		if line.Desc != "" {
			text = line.Desc + ": " + text
		}
		textLines = append(textLines, text)
		var lineWidth float64
		for _, r := range text {
			metrics, ok := font.GetRuneMetrics(r)
			if !ok {
				continue
			}
			lineWidth += metrics.Wx
		}
		if lineWidth > maxWidth {
			maxWidth = lineWidth
		}
	}
	maxWidth = maxWidth * fontSize / 1000.0
	textHeight := float64(len(textLines)) * leading
	hasImage := opts.Image != nil
	rect := opts.Rect
	if rect == nil {
		rect = []float64{0, 0, maxWidth, textHeight}
		if hasImage {
			rect[2] = maxWidth * 2
			rect[3] = textHeight * 2
		}
		opts.Rect = rect
	}
	rect = []float64{math.Min(rect[0], rect[2]), math.Min(rect[1], rect[3]), math.Max(rect[0], rect[2]), math.Max(rect[1], rect[3])}
	rectW := rect[2] - rect[0]
	rectH := rect[3] - rect[1]
	imageRect, textRect := rect, rect
	var textW, textH float64
	if hasImage && len(textLines) > 0 {
		if opts.ImagePosition <= SignatureImageRight {
			leftHalf := []float64{rect[0], rect[1], rect[0] + (rectW / 2), rect[3]}
			rightHalf := []float64{rect[0] + (rectW / 2), rect[1], rect[2], rect[3]}
			if opts.ImagePosition == SignatureImageLeft {
				imageRect, textRect = leftHalf, rightHalf
			} else {
				imageRect, textRect = rightHalf, leftHalf
			}
		} else {
			bottomHalf := []float64{rect[0], rect[1], rect[2], rect[1] + (rectH / 2)}
			topHalf := []float64{rect[0], rect[1] + (rectH / 2), rect[2], rect[3]}
			if opts.ImagePosition == SignatureImageTop {
				imageRect, textRect = topHalf, bottomHalf
			} else {
				imageRect, textRect = bottomHalf, topHalf
			}
		}
	}
	textW = textRect[2] - textRect[0]
	textH = textRect[3] - textRect[1]
	var yOffset float64
	if opts.AutoSize {
		if maxWidth > textW || textHeight > textH {
			scale := math.Min(textW/maxWidth, textH/textHeight)
			fontSize *= scale
		}
		leading = opts.LineHeight * fontSize
		yOffset += (textH - float64(len(textLines))*leading) / 2
	}
	cc := contentstream.NewContentCreator()
	resources := model.NewPdfPageResources()
	resources.SetFontByName(*fontName, font.ToPdfObject())
	if opts.BorderSize <= 0 {
		opts.BorderSize = 0
		opts.BorderColor = model.NewPdfColorDeviceGray(1)
	}
	cc.Add_q()
	if opts.FillColor != nil {
		cc.SetNonStrokingColor(opts.FillColor)
	}
	if opts.BorderColor != nil {
		cc.SetStrokingColor(opts.BorderColor)
	}
	gsName := ""
	if opts.FillOpacity < 1.0 {
		idx := 0
		gsName = fmt.Sprintf("GS%d", idx)
		for {
			_, ok := resources.GetExtGState(core.PdfObjectName(gsName))
			if !ok {
				break
			}
			idx++
			gsName = fmt.Sprintf("GS%d", idx)
		}
		gsDict := core.MakeDict()
		gsDict.Set("ca", core.MakeFloat(opts.FillOpacity))
		err := resources.AddExtGState(core.PdfObjectName(gsName), core.MakeIndirectObject(gsDict))
		if err != nil {
			common.Log.Debug(err.Error())
		} else {
			cc.Add_gs(core.PdfObjectName(gsName))
		}
	}
	cc.Add_w(opts.BorderSize).Add_re(rect[0], rect[1], rectW, rectH)
	if opts.FillColor != nil && opts.BorderColor != nil {
		cc.Add_B()
	} else if opts.FillColor != nil {
		cc.Add_f()
	} else if opts.BorderColor != nil {
		cc.Add_S()
	}
	cc.Add_Q()
	if opts.WatermarkImage != nil {
		wmRect := []float64{rect[0], rect[1], rect[2], rect[3]}
		wmName, wmImg, err := drawSignatureImage(opts.WatermarkImage, "ImageWatermark", opts, wmRect, cc)
		if err != nil {
			return nil, err
		}
		resources.SetXObjectImageByName(*wmName, wmImg)
	}
	cc.Add_q()
	cc.Translate(textRect[0], textRect[3]-leading-yOffset)
	cc.Add_BT()
	encoder := font.Encoder()
	for _, textLine := range textLines {
		var buf []byte
		for _, r := range textLine {
			if unicode.IsSpace(r) {
				if len(buf) > 0 {
					cc.SetNonStrokingColor(opts.TextColor).Add_Tf(*fontName, fontSize).Add_TL(leading).Add_TJ([]core.PdfObject{core.MakeStringFromBytes(buf)}...)
					buf = nil
				}
				cc.Add_Tf(*fontName, fontSize).Add_TL(leading).Add_TJ([]core.PdfObject{core.MakeFloat(-spaceWidth)}...)
			} else {
				buf = append(buf, encoder.Encode(string(r))...)
			}
		}
		if len(buf) > 0 {
			cc.SetNonStrokingColor(opts.TextColor).Add_Tf(*fontName, fontSize).Add_TL(leading).Add_TJ([]core.PdfObject{core.MakeStringFromBytes(buf)}...)
		}
		cc.Add_Td(0, -leading)
	}
	cc.Add_ET()
	cc.Add_Q()
	if hasImage {
		imgName, imgXObj, err := drawSignatureImage(opts.Image, "ImageSignature", opts, imageRect, cc)
		if err != nil {
			return nil, err
		}
		resources.SetXObjectImageByName(*imgName, imgXObj)
	}
	form := model.NewXObjectForm()
	form.Resources = resources
	form.BBox = core.MakeArrayFromFloats(rect)
	if err := setClippedContent(form, cc.Bytes()); err != nil {
		return nil, err
	}
	apDict := core.MakeDict()
	apDict.Set("N", form.ToPdfObject())
	return apDict, nil
}

func lineAppearanceStream(def LineAnnotationDef, gsName string) ([]byte, *model.PdfRectangle, *model.PdfRectangle, error) {
	line := contentdraw.Line{X1: 0, Y1: 0, X2: def.X2 - def.X1, Y2: def.Y2 - def.Y1, LineColor: def.LineColor, Opacity: def.Opacity, LineWidth: def.LineWidth, LineEndingStyle1: def.LineEndingStyle1, LineEndingStyle2: def.LineEndingStyle2}
	content, bbox, err := line.Draw(gsName)
	if err != nil {
		return nil, nil, nil, err
	}
	rect := &model.PdfRectangle{}
	rect.Llx = def.X1 + bbox.Llx
	rect.Lly = def.Y1 + bbox.Lly
	rect.Urx = def.X1 + bbox.Urx
	rect.Ury = def.Y1 + bbox.Ury
	return content, bbox, rect, nil
}

// CheckboxFieldOptions defines optional parameters for a checkbox field a form.
type CheckboxFieldOptions struct{ Checked bool }

func (s *AppearanceStyle) processDA(field *model.PdfField, ops *contentstream.ContentStreamOperations, dr, resources *model.PdfPageResources, cc *contentstream.ContentCreator) (*AppearanceFont, bool, error) {
	var fallback *AppearanceFont
	var forceReplace bool
	if s.Fonts != nil {
		if s.Fonts.Fallback != nil {
			fallback = s.Fonts.Fallback
		}
		if fieldFallbacks := s.Fonts.FieldFallbacks; fieldFallbacks != nil {
			if f, ok := fieldFallbacks[field.PartialName()]; ok {
				fallback = f
			} else if fullName, err := field.FullName(); err == nil {
				if f, ok := fieldFallbacks[fullName]; ok {
					fallback = f
				}
			}
		}
		if fallback != nil {
			fallback.fillName()
		}
		forceReplace = s.Fonts.ForceReplace
	}
	var fontName string
	var fontSize float64
	var hasTf bool
	if ops != nil {
		for _, op := range *ops {
			if op.Operand == "Tf" && len(op.Params) == 2 {
				if name, ok := core.GetNameVal(op.Params[0]); ok {
					fontName = name
				}
				if size, err := core.GetNumberAsFloat(op.Params[1]); err == nil {
					fontSize = size
				}
				hasTf = true
				continue
			}
			cc.AddOperand(*op)
		}
	}
	var font *AppearanceFont
	var fontObj core.PdfObject
	if forceReplace && fallback != nil {
		font = fallback
	} else {
		if dr != nil && fontName != "" {
			if fo, ok := dr.GetFontByName(*core.MakeName(fontName)); ok {
				if pdfFont, err := model.NewPdfFontFromPdfObject(fo); err == nil {
					fontObj = fo
					font = &AppearanceFont{Name: fontName, Font: pdfFont, Size: fontSize}
				} else {
					common.Log.Debug("ERROR: could not load appearance font: %v", err)
				}
			}
		}
		if font == nil && fallback != nil {
			font = fallback
		}
		if font == nil {
			stdFont, err := model.NewStandard14Font("Helvetica")
			if err != nil {
				return nil, false, err
			}
			font = &AppearanceFont{Name: "Helv", Font: stdFont, Size: fontSize}
		}
	}
	if font.Size <= 0 && s.Fonts != nil && s.Fonts.FallbackSize > 0 {
		font.Size = s.Fonts.FallbackSize
	}
	fontNameObj := *core.MakeName(font.Name)
	if fontObj == nil {
		fontObj = font.Font.ToPdfObject()
	}
	if dr != nil && !dr.HasFontByName(fontNameObj) {
		dr.SetFontByName(fontNameObj, fontObj)
	}
	if resources != nil && !resources.HasFontByName(fontNameObj) {
		resources.SetFontByName(fontNameObj, fontObj)
	}
	return font, hasTf, nil
}

func balanceGraphicsState(ops *contentstream.ContentStreamOperations, content []byte) []byte {
	if ops == nil {
		return content
	}
	depth, minDepth := 0, 0
	for _, op := range *ops {
		switch op.Operand {
		case "q":
			depth++
		case "Q":
			depth--
			if depth < minDepth {
				minDepth = depth
			}
		}
	}
	if depth == 0 && minDepth == 0 {
		return content
	}
	buf := []byte(strings.Repeat("q\n", -minDepth))
	buf = append(buf, content...)
	if n := len(buf); n > 0 && buf[n-1] != '\n' {
		buf = append(buf, '\n')
	}
	return append(buf, strings.Repeat("Q\n", depth-minDepth)...)
}

// CreateInkAnnotation creates an ink annotation object that can be added to the annotation list of a PDF page.
func CreateInkAnnotation(inkDef InkAnnotationDef) (*model.PdfAnnotation, error) {
	annot := model.NewPdfAnnotationInk()
	inkList := core.MakeArray()
	for _, path := range inkDef.Paths {
		if path.Length() == 0 {
			continue
		}
		coords := []float64{}
		for _, pt := range path.Points {
			x := precision.RoundDefault(pt.X)
			y := precision.RoundDefault(pt.Y)
			coords = append(coords, x, y)
		}
		inkList.Append(core.MakeArrayFromFloats(coords))
	}
	annot.InkList = inkList
	if inkDef.Color == nil {
		inkDef.Color = model.NewPdfColorDeviceRGB(0.0, 0.0, 0.0)
	}
	annot.C = core.MakeArrayFromFloats([]float64{inkDef.Color.R(), inkDef.Color.G(), inkDef.Color.B()})
	ap, rect, err := inkAppearance(&inkDef)
	if err != nil {
		return nil, err
	}
	annot.AP = ap
	annot.Rect = core.MakeArrayFromFloats([]float64{rect.Llx, rect.Lly, rect.Urx, rect.Ury})
	return annot.PdfAnnotation, nil
}

func textFieldAppearance(widget *model.PdfAnnotationWidget, field *model.PdfFieldText, dr *model.PdfPageResources, style AppearanceStyle) (*core.PdfObjectDictionary, error) {
	resources := model.NewPdfPageResources()
	rectArr, ok := core.GetArray(widget.Rect)
	if !ok {
		return nil, errors.New("invalid Rect")
	}
	rect, err := model.NewPdfRectangle(*rectArr)
	if err != nil {
		return nil, err
	}
	w, h := rect.Width(), rect.Height()
	formW, formH := w, h
	emptyAP := true
	form := model.NewXObjectForm()
	form.BBox = core.MakeArrayFromFloats([]float64{0, 0, formW, formH})
	if widget.AP != nil {
		if apDict, ok := core.GetDict(widget.AP); ok && apDict != nil {
			normal := core.TraceToDirectObject(apDict.Get("N"))
			switch stm := normal.(type) {
			case *core.PdfObjectStream:
				content, err := core.DecodeStream(stm)
				if err != nil {
					common.Log.Debug("ERROR unable decode content stream: %v", err.Error())
					break
				}
				ops, err := contentstream.NewContentStreamParser(string(content)).Parse()
				if err != nil {
					common.Log.Debug("ERROR unable parse content stream: %v", err.Error())
					break
				}
				proc := contentstream.NewContentStreamProcessor(*ops)
				proc.AddHandler(contentstream.HandlerConditionEnumAllOperands, "", func(op *contentstream.ContentStreamOperation, gs contentstream.GraphicsState, res *model.PdfPageResources) error {
					if op.Operand == "Tj" || op.Operand == "TJ" {
						if len(op.Params) == 1 {
							if str, ok := core.GetString(op.Params[0]); ok {
								emptyAP = strings.TrimSpace(str.Str()) == ""
							}
							return contentstream.ErrEarlyExit
						}
						return nil
					}
					return nil
				})
				proc.Process(resources)
				if !emptyAP {
					if resDict, ok := core.GetDict(stm.Get("Resources")); ok {
						resources, err = model.NewPdfPageResourcesFromDict(resDict)
						if err != nil {
							return nil, err
						}
					}
					if matrix, ok := core.GetArray(stm.Get("Matrix")); ok {
						form.Matrix = matrix
					}
					if err := setClippedContent(form, content); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	if emptyAP {
		mk, ok := core.GetDict(widget.MK)
		if ok {
			bs, _ := core.GetDict(widget.BS)
			err := style.applyAppearanceCharacteristics(mk, bs, nil)
			if err != nil {
				return nil, err
			}
		}
		daOps, err := contentstream.NewContentStreamParser(textFieldDA(field.PdfField)).Parse()
		if err != nil {
			return nil, err
		}
		cc := contentstream.NewContentCreator()
		if style.BorderSize > 0 {
			drawBorderAndFill(cc, style, w, h)
		}
		if style.DrawAlignmentReticle {
			reticleStyle := style
			reticleStyle.BorderSize = 0.2
			drawAlignmentReticle(cc, reticleStyle, w, h)
		}
		cc.Add_BMC("Tx")
		cc.Add_q()
		w, h = style.applyRotation(mk, w, h, cc)
		cc.Add_BT()
		daFont, hasTf, err := style.processDA(field.PdfField, daOps, dr, resources, cc)
		if err != nil {
			return nil, err
		}
		font := daFont.Font
		fontSize := daFont.Size
		fontName := core.MakeName(daFont.Name)
		if field.Flags().Has(model.FieldFlagMultiline) && field.MaxLen != nil {
			common.Log.Debug("Look for AP dictionary for N object content stream")
			if apFontName, apFontSize, found := findTfInAP(widget.AP, dr); found {
				fontName = apFontName
				fontSize = apFontSize
				hasTf = true
			}
		}
		autoSize := fontSize == 0
		if autoSize && hasTf {
			fontSize = h * style.AutoFontSizeFraction
		}
		encoder := font.Encoder()
		if encoder == nil {
			common.Log.Debug("WARN: font encoder is nil. Assuming identity encoder. Output may be incorrect.")
			encoder = textencoding.NewIdentityTextEncoder("Identity-H")
		}
		desc, err := font.GetFontDescriptor()
		if err != nil {
			common.Log.Debug("Error: Unable to get font descriptor")
		}
		var text string
		if str, ok := core.GetString(field.V); ok {
			text = str.Decoded()
		}
		if len(text) == 0 {
			return nil, nil
		}
		text = shapeText(text)
		lines := []string{text}
		multiline := false
		if field.Flags().Has(model.FieldFlagMultiline) {
			multiline = true
			text = strings.ReplaceAll(text, "\r\n", "\n")
			text = strings.ReplaceAll(text, "\r", "\n")
			lines = strings.Split(text, "\n")
		}
		srcLines := make([]string, len(lines))
		copy(srcLines, lines)
		lineHeightMul := style.MultilineLineHeight
		maxLineW := 0.0
		numLines := 0
		if encoder != nil {
			for fontSize >= 0 {
				wrapped := make([]string, len(lines))
				copy(wrapped, lines)
				srcWrapped := make([]string, len(srcLines))
				copy(srcWrapped, srcLines)
				maxLineW = 0.0
				numLines = 0
				total := len(wrapped)
				li := 0
				for li < total {
					var prevX float64
					lastSpace := -1
					x := defaultMarginLeft
					if style.MarginLeft != nil {
						x = *style.MarginLeft
					}
					for i, r := range wrapped[li] {
						if r == ' ' {
							lastSpace = i
						}
						metrics, ok := font.GetRuneMetrics(r)
						if !ok {
							common.Log.Debug("Font does not have rune metrics for %v - skipping", r)
							continue
						}
						prevX = x
						x += metrics.Wx
						if multiline && !autoSize && fontSize*x/1000.0 > w {
							restStart := i
							lineEnd := i
							if lastSpace > 0 {
								restStart = lastSpace + 1
								lineEnd = lastSpace
							}
							rest := wrapped[li][restStart:]
							srcRest := srcWrapped[li][restStart:]
							if li < len(wrapped)-1 {
								wrapped = append(wrapped[:li+1], wrapped[li:]...)
								wrapped[li+1] = rest
								srcWrapped = append(srcWrapped[:li+1], srcWrapped[li:]...)
								srcWrapped[li+1] = srcRest
							} else {
								wrapped = append(wrapped, rest)
								srcWrapped = append(srcWrapped, srcRest)
							}
							wrapped[li] = wrapped[li][0:lineEnd]
							srcWrapped[li] = srcWrapped[li][0:lineEnd]
							total++
							x = prevX
							break
						}
					}
					if x > maxLineW {
						maxLineW = x
					}
					wrapped[li] = string(encoder.Encode(wrapped[li]))
					if len(wrapped[li]) > 0 {
						numLines++
					}
					li++
				}
				lineStep := fontSize
				if numLines > 1 {
					lineStep *= lineHeightMul
				}
				neededH := float64(numLines) * lineStep
				if autoSize || neededH <= h {
					lines = wrapped
					srcLines = srcWrapped
					break
				}
				fontSize--
			}
		}
		marginLeft := defaultMarginLeft
		if style.MarginLeft != nil {
			marginLeft = *style.MarginLeft
		}
		if fontSize == 0 || autoSize && maxLineW > 0 && marginLeft+maxLineW*fontSize/1000.0 > w {
			fontSize = 0.95 * 1000.0 * (w - marginLeft) / maxLineW
		}
		align := quaddingLeft
		{
			if q, ok := core.GetIntVal(field.Q); ok {
				switch q {
				case 0:
					align = quaddingLeft
				case 1:
					align = quaddingCenter
				case 2:
					align = quaddingRight
				default:
					common.Log.Debug("ERROR: Unsupported quadding: %d - using left alignment", q)
				}
			}
		}
		lineSpacing := fontSize
		if multiline && numLines > 1 {
			lineSpacing = lineHeightMul * fontSize
		}
		var capHeight float64
		if desc != nil {
			capHeight, err = desc.GetCapHeight()
			if err != nil {
				common.Log.Debug("ERROR: Unable to get font CapHeight: %v", err)
			}
		}
		if int(capHeight) <= 0 {
			common.Log.Debug("WARN: CapHeight not available - setting to 1000")
			capHeight = 1000
		}
		scaledCap := capHeight / 1000.0 * fontSize
		y := 0.0
		{
			blockH := float64(numLines) * lineSpacing
			if autoSize && y+blockH > h {
				fontSize = 0.95 * (h - y) / float64(numLines)
				lineSpacing = fontSize
				if multiline && numLines > 1 {
					lineSpacing = lineHeightMul * fontSize
				}
				scaledCap = capHeight / 1000.0 * fontSize
				blockH = float64(numLines) * lineSpacing
			}
			if h > blockH {
				if multiline {
					if style.MultilineVAlignMiddle {
						pad := (h - (blockH + scaledCap)) / 2.0
						topY := pad + blockH + scaledCap - lineSpacing
						y = topY
						if numLines > 1 {
							y = y + (blockH / fontSize * float64(numLines)) - lineSpacing - scaledCap
						}
						if y < blockH {
							y = (h - scaledCap) / 2.0
						}
					} else {
						y = h - lineSpacing
						if y > fontSize {
							extra := 0.0
							if multiline && style.MultilineLineHeight > 1 && numLines > 1 {
								extra = style.MultilineLineHeight - 1
							}
							y -= fontSize * (0.5 - extra)
						}
					}
				} else {
					y = (h - scaledCap) / 2.0
				}
			}
		}
		if style.TextColor != nil {
			color := style.TextColor
			rgb, ok := color.(*model.PdfColorDeviceRGB)
			if !ok {
				rgb = model.NewPdfColorDeviceRGB(0, 0, 0)
			}
			cc.Add_rg(rgb.R(), rgb.G(), rgb.B())
		} else {
			for _, op := range *daOps {
				if op.Operand == "rg" || op.Operand == "g" {
					cc.AddOperand(*op)
				}
			}
		}
		cc.Add_Tf(*fontName, fontSize)
		cc.Add_Td(marginLeft, y)
		leftX := marginLeft
		curX := marginLeft
		for i, line := range lines {
			rawWidth := 0.0
			for _, r := range srcLines[i] {
				metrics, ok := font.GetRuneMetrics(r)
				if !ok {
					continue
				}
				rawWidth += metrics.Wx
			}
			textW := rawWidth / 1000.0 * fontSize
			free := w - textW
			var targetX float64
			switch align {
			case quaddingLeft:
				targetX = leftX
			case quaddingCenter:
				targetX = free / 2
			case quaddingRight:
				targetX = free
			}
			marginLeft = targetX - curX
			if marginLeft > 0.0 {
				cc.Add_Td(marginLeft, 0)
			}
			curX = targetX
			cc.Add_Tj(*core.MakeString(line))
			if i < len(lines)-1 {
				cc.Add_Td(0, -fontSize*lineHeightMul)
			}
		}
		cc.Add_ET()
		cc.Add_Q()
		cc.Add_EMC()
		if err := setClippedContent(form, cc.Bytes()); err != nil {
			return nil, err
		}
	}
	form.Resources = resources
	apDict := core.MakeDict()
	apDict.Set("N", form.ToPdfObject())
	return apDict, nil
}

func drawBorderAndFill(cc *contentstream.ContentCreator, style AppearanceStyle, w, h float64) {
	cc.Add_q().Add_re(0, 0, w, h).Add_w(style.BorderSize).SetStrokingColor(style.BorderColor).SetNonStrokingColor(style.FillColor).Add_B().Add_Q()
}

// NewSignatureFieldOpts returns a new initialized instance of options
// used to generate a signature appearance.
func NewSignatureFieldOpts() *SignatureFieldOpts {
	return &SignatureFieldOpts{Font: model.DefaultFont(), FontSize: 10, LineHeight: 1, AutoSize: true, TextColor: model.NewPdfColorDeviceGray(0), BorderColor: model.NewPdfColorDeviceGray(0), FillColor: model.NewPdfColorDeviceGray(1), FillOpacity: 1.0, Encoder: core.NewFlateEncoder(), ImagePosition: SignatureImageLeft}
}

// Style returns the appearance style of `fa`. If not specified, returns default style.
func (a ImageFieldAppearance) Style() AppearanceStyle {
	if a.style != nil {
		return *a.style
	}
	return AppearanceStyle{BorderSize: 0.0, BorderColor: model.NewPdfColorDeviceGray(0), FillColor: model.NewPdfColorDeviceGray(1), DrawAlignmentReticle: false}
}

func pushButtonAppearance(form *model.PdfAcroForm, widget *model.PdfAnnotationWidget, field *model.PdfFieldButton, style AppearanceStyle) (*core.PdfObjectDictionary, error) {
	rectArr, hasRect := core.GetArray(widget.Rect)
	if !hasRect {
		common.Log.Debug("ERROR: push button widget missing Rect - keeping existing appearance")
		if ap, ok := core.GetDict(widget.AP); ok && ap != nil {
			return ap, nil
		}
		return nil, nil
	}
	rect, err := model.NewPdfRectangle(*rectArr)
	if err != nil {
		common.Log.Debug("ERROR: invalid push button Rect: %v - keeping existing appearance", err)
		if ap, ok := core.GetDict(widget.AP); ok && ap != nil {
			return ap, nil
		}
		return nil, nil
	}
	w, h := rect.Width(), rect.Height()
	formW, formH := w, h
	mk, hasMK := core.GetDict(widget.MK)
	if hasMK && mk.Get("I") != nil {
		common.Log.Debug("Push button with icon (MK I) - keeping existing appearance")
		if ap, ok := core.GetDict(widget.AP); ok && ap != nil {
			return ap, nil
		}
		return nil, nil
	}
	if field.GetFillImage() != nil {
		return imageButtonAppearance(field, widget, style)
	}
	var caption string
	hasBG := false
	if hasMK {
		bs, _ := core.GetDict(widget.BS)
		if err := style.applyAppearanceCharacteristics(mk, bs, nil); err != nil {
			return nil, err
		}
		if str, ok := core.GetString(mk.Get("CA")); ok {
			caption = str.Decoded()
		}
		if style.AllowMK {
			if bg, ok := core.GetArray(mk.Get("BG")); ok && bg.Len() > 0 {
				hasBG = true
			}
			if bc, ok := core.GetArray(mk.Get("BC")); ok && bc.Len() > 0 && style.BorderSize <= 0 {
				if bs == nil || bs.Get("W") == nil {
					style.BorderSize = 1
				}
			}
		}
	}
	if caption == "" && !hasBG && style.BorderSize <= 0 && !style.DrawAlignmentReticle {
		if ap, ok := core.GetDict(widget.AP); ok && ap != nil && ap.Get("N") != nil {
			common.Log.Debug("Push button without MK appearance characteristics - keeping existing appearance")
			return ap, nil
		}
	}
	resources := model.NewPdfPageResources()
	cc := contentstream.NewContentCreator()
	cc.Add_q()
	w, h = style.applyRotation(mk, w, h, cc)
	borderW := style.BorderSize
	if borderW > 0 && hasBG {
		cc.Add_q().Add_re(borderW/2, borderW/2, w-borderW, h-borderW).Add_w(borderW).SetStrokingColor(style.BorderColor).SetNonStrokingColor(style.FillColor).Add_B().Add_Q()
	} else if borderW > 0 {
		cc.Add_q().Add_re(borderW/2, borderW/2, w-borderW, h-borderW).Add_w(borderW).SetStrokingColor(style.BorderColor).Add_S().Add_Q()
	} else if hasBG {
		cc.Add_q().Add_re(0, 0, w, h).SetNonStrokingColor(style.FillColor).Add_f().Add_Q()
	}
	if style.DrawAlignmentReticle {
		reticleStyle := style
		reticleStyle.BorderSize = 0.2
		drawAlignmentReticle(cc, reticleStyle, w, h)
	}
	if len(caption) > 0 {
		da := variableTextDA(field.PdfField)
		if da == "" && form.DA != nil {
			da = form.DA.Str()
		}
		daOps, err := contentstream.NewContentStreamParser(da).Parse()
		if err != nil {
			return nil, err
		}
		cc.Add_BT()
		daFont, _, err := style.processDA(field.PdfField, daOps, form.DR, resources, cc)
		if err != nil {
			return nil, err
		}
		if style.TextColor != nil {
			cc.SetNonStrokingColor(style.TextColor)
		}
		font := daFont.Font
		fontSize := daFont.Size
		fontName := core.MakeName(daFont.Name)
		autoSize := fontSize <= 0
		if autoSize {
			fontSize = h * style.AutoFontSizeFraction
		}
		encoder := font.Encoder()
		if encoder == nil {
			common.Log.Debug("WARN: font encoder is nil. Assuming identity encoder. Output may be incorrect.")
			encoder = textencoding.NewIdentityTextEncoder("Identity-H")
		}
		var textWidth float64
		for _, r := range caption {
			metrics, ok := font.GetRuneMetrics(r)
			if !ok {
				common.Log.Debug("ERROR: Rune not found in font: %v - skipping over", r)
				continue
			}
			textWidth += metrics.Wx
		}
		marginLeft := defaultMarginLeft
		if style.MarginLeft != nil {
			marginLeft = *style.MarginLeft
		}
		if textWidth > 0 && marginLeft+textWidth*fontSize/1000.0 > w {
			fontSize = 0.95 * 1000.0 * (w - marginLeft) / textWidth
			if fontSize <= 0 {
				fontSize = style.AutoFontSizeFraction * h
			}
		}
		var capHeight float64
		if desc, err := font.GetFontDescriptor(); err == nil && desc != nil {
			capHeight, err = desc.GetCapHeight()
			if err != nil {
				common.Log.Debug("ERROR: Unable to get font CapHeight: %v", err)
			}
		}
		if int(capHeight) <= 0 {
			common.Log.Debug("WARN: CapHeight not available - setting to 1000")
			capHeight = 1000
		}
		if fontSize*capHeight/1000.0 > h {
			fontSize = 0.95 * 1000.0 * h / capHeight
		}
		scaledCap := capHeight / 1000.0 * fontSize
		x := marginLeft
		if textWidth > 0 {
			x = (w - textWidth*fontSize/1000.0) / 2.0
			if x < marginLeft {
				x = marginLeft
			}
		}
		y := (h - scaledCap) / 2.0
		cc.Add_Tf(*fontName, fontSize)
		cc.Add_Td(x, y)
		cc.Add_Tj(*core.MakeStringFromBytes(encoder.Encode(caption)))
		cc.Add_ET()
	}
	cc.Add_Q()
	xform := model.NewXObjectForm()
	xform.Resources = resources
	xform.BBox = core.MakeArrayFromFloats([]float64{0, 0, formW, formH})
	if err := setClippedContent(xform, cc.Bytes()); err != nil {
		return nil, err
	}
	ap, ok := core.GetDict(widget.AP)
	if !ok || ap == nil {
		ap = core.MakeDict()
	}
	ap.Set("N", xform.ToPdfObject())
	ap.Remove("D")
	ap.Remove("R")
	return ap, nil
}

func signatureLines(sig *model.PdfSignature) []*SignatureLine {
	if sig == nil {
		return nil
	}
	var lines []*SignatureLine
	if sig.Name != nil && sig.Name.Decoded() != "" {
		lines = append(lines, NewSignatureLine("Name", sig.Name.Decoded()))
	}
	if sig.M != nil {
		if t, err := timeutils.ParsePdfTime(sig.M.Decoded()); err == nil {
			dateStr := t.Format("2006.01.02 15:04:05 -0700")
			dateStr = dateStr[:len(dateStr)-2] + "'" + dateStr[len(dateStr)-2:] + "'"
			lines = append(lines, NewSignatureLine("Date", dateStr))
		} else {
			common.Log.Debug("Unparseable signature date M %q - omitting the date line", sig.M.Decoded())
		}
	}
	if sig.Reason != nil && sig.Reason.Decoded() != "" {
		lines = append(lines, NewSignatureLine("Reason", sig.Reason.Decoded()))
	}
	if sig.Location != nil && sig.Location.Decoded() != "" {
		lines = append(lines, NewSignatureLine("Location", sig.Location.Decoded()))
	}
	return lines
}

// CreateLineAnnotation creates a line annotation object that can be added to page PDF annotations.
func CreateLineAnnotation(lineDef LineAnnotationDef) (*model.PdfAnnotation, error) {
	annot := model.NewPdfAnnotationLine()
	annot.L = core.MakeArrayFromFloats([]float64{lineDef.X1, lineDef.Y1, lineDef.X2, lineDef.Y2})
	le1 := core.MakeName("None")
	if lineDef.LineEndingStyle1 == contentdraw.LineEndingStyleArrow {
		le1 = core.MakeName("ClosedArrow")
	}
	le2 := core.MakeName("None")
	if lineDef.LineEndingStyle2 == contentdraw.LineEndingStyleArrow {
		le2 = core.MakeName("ClosedArrow")
	}
	annot.LE = core.MakeArray(le1, le2)
	if lineDef.Opacity < 1.0 {
		annot.CA = core.MakeFloat(lineDef.Opacity)
	}
	r, g, b := lineDef.LineColor.R(), lineDef.LineColor.G(), lineDef.LineColor.B()
	annot.IC = core.MakeArrayFromFloats([]float64{r, g, b})
	annot.C = core.MakeArrayFromFloats([]float64{r, g, b})
	bs := model.NewBorderStyle()
	bs.SetBorderWidth(lineDef.LineWidth)
	annot.BS = bs.ToPdfObject()
	ap, rect, err := lineAppearance(lineDef)
	if err != nil {
		return nil, err
	}
	annot.AP = ap
	annot.Rect = core.MakeArrayFromFloats([]float64{rect.Llx, rect.Lly, rect.Urx, rect.Ury})
	return annot.PdfAnnotation, nil
}

// SignatureLine represents a line of information in the signature field appearance.
type SignatureLine struct {
	Desc string
	Text string
}

const (
	quaddingLeft      quadding = 0
	quaddingCenter    quadding = 1
	quaddingRight     quadding = 2
	defaultMarginLeft float64  = 2.0
)

func combTextFieldAppearance(widget *model.PdfAnnotationWidget, field *model.PdfFieldText, dr *model.PdfPageResources, style AppearanceStyle) (*core.PdfObjectDictionary, error) {
	resources := model.NewPdfPageResources()
	rectArr, hasRect := core.GetArray(widget.Rect)
	if !hasRect {
		return nil, errors.New("invalid Rect")
	}
	rect, err := model.NewPdfRectangle(*rectArr)
	if err != nil {
		return nil, err
	}
	w, h := rect.Width(), rect.Height()
	formW, formH := w, h
	mk, ok := core.GetDict(widget.MK)
	if ok {
		bs, _ := core.GetDict(widget.BS)
		err := style.applyAppearanceCharacteristics(mk, bs, nil)
		if err != nil {
			return nil, err
		}
	}
	maxLen, ok := core.GetIntVal(field.MaxLen)
	if !ok {
		return nil, errors.New("maxlen not set")
	}
	if maxLen <= 0 {
		return nil, errors.New("maxLen invalid")
	}
	cellW := w / float64(maxLen)
	daOps, err := contentstream.NewContentStreamParser(textFieldDA(field.PdfField)).Parse()
	if err != nil {
		return nil, err
	}
	cc := contentstream.NewContentCreator()
	if style.BorderSize > 0 {
		drawBorderAndFill(cc, style, w, h)
	}
	if style.DrawAlignmentReticle {
		reticleStyle := style
		reticleStyle.BorderSize = 0.2
		drawAlignmentReticle(cc, reticleStyle, w, h)
	}
	cc.Add_BMC("Tx")
	cc.Add_q()
	_, h = style.applyRotation(mk, w, h, cc)
	cc.Add_BT()
	daFont, hasTf, err := style.processDA(field.PdfField, daOps, dr, resources, cc)
	if err != nil {
		return nil, err
	}
	font := daFont.Font
	fontName := core.MakeName(daFont.Name)
	fontSize := daFont.Size
	autoSize := fontSize == 0
	if autoSize && hasTf {
		fontSize = h * style.AutoFontSizeFraction
	}
	encoder := font.Encoder()
	if encoder == nil {
		common.Log.Debug("WARN: font encoder is nil. Assuming identity encoder. Output may be incorrect.")
		encoder = textencoding.NewIdentityTextEncoder("Identity-H")
	}
	var text string
	if str, ok := core.GetString(field.V); ok {
		text = str.Decoded()
	}
	text = shapeText(text)
	cc.Add_Tf(*fontName, fontSize)
	var maxGlyph float64
	for _, r := range text {
		metrics, ok := font.GetRuneMetrics(r)
		if !ok {
			common.Log.Debug("ERROR: Rune not found in font: %v - skipping over", r)
			continue
		}
		glyphSize := metrics.Wy
		if int(glyphSize) <= 0 {
			glyphSize = metrics.Wx
		}
		if glyphSize > maxGlyph {
			maxGlyph = glyphSize
		}
	}
	if int(maxGlyph) == 0 {
		common.Log.Debug("ERROR: Unable to determine max glyph size - using 1000")
		maxGlyph = 1000
	}
	desc, err := font.GetFontDescriptor()
	if err != nil {
		common.Log.Debug("Error: Unable to get font descriptor")
	}
	var capHeight float64
	if desc != nil {
		capHeight, err = desc.GetCapHeight()
		if err != nil {
			common.Log.Debug("ERROR: Unable to get font CapHeight: %v", err)
		}
	}
	if int(capHeight) <= 0 {
		common.Log.Debug("WARN: CapHeight not available - setting to 1000")
		capHeight = 1000.0
	}
	scaledCap := capHeight / 1000.0 * fontSize
	y := 0.0
	glyphH := 1.0 * fontSize * (maxGlyph / 1000.0)
	{
		needed := glyphH
		if autoSize && y+needed > h {
			fontSize = 0.95 * (h - y)
			scaledCap = capHeight / 1000.0 * fontSize
		}
		if h > scaledCap {
			y = (h - scaledCap) / 2.0
		}
	}
	cc.Add_Td(0, y)
	if q, ok := core.GetIntVal(field.Q); ok {
		switch q {
		case 2:
			if len(text) < maxLen {
				pad := float64(maxLen-len(text)) * cellW
				cc.Add_Td(pad, 0)
			}
		}
	}
	for i, r := range text {
		x := defaultMarginLeft
		if style.MarginLeft != nil {
			x = *style.MarginLeft
		}
		s := string(r)
		if encoder != nil {
			metrics, ok := font.GetRuneMetrics(r)
			if !ok {
				common.Log.Debug("ERROR: Rune not found in font: %v - skipping over", r)
				continue
			}
			s = string(encoder.Encode(s))
			glyphW := fontSize * metrics.Wx / 1000.0
			cx := (cellW - glyphW) / 2
			x = cx
		}
		cc.Add_Td(x, 0)
		cc.Add_Tj(*core.MakeString(s))
		if i != len(text)-1 {
			cc.Add_Td(cellW-x, 0)
		}
	}
	cc.Add_ET()
	cc.Add_Q()
	cc.Add_EMC()
	xform := model.NewXObjectForm()
	xform.Resources = resources
	xform.BBox = core.MakeArrayFromFloats([]float64{0, 0, formW, formH})
	if err := setClippedContent(xform, cc.Bytes()); err != nil {
		return nil, err
	}
	apDict := core.MakeDict()
	apDict.Set("N", xform.ToPdfObject())
	return apDict, nil
}

// NewComboboxField generates a new combobox form field with partial name `name` at location `rect`
// on specified `page` and with field specific options `opt`.
func NewComboboxField(page *model.PdfPage, name string, rect []float64, opt ComboboxFieldOptions) (*model.PdfFieldChoice, error) {
	if page == nil {
		return nil, errors.New("page not specified")
	}
	if len(name) <= 0 {
		return nil, errors.New("required attribute not specified")
	}
	if len(rect) != 4 {
		return nil, errors.New("invalid range")
	}
	base := model.NewPdfField()
	choice := &model.PdfFieldChoice{}
	base.SetContext(choice)
	choice.PdfField = base
	choice.T = core.MakeString(name)
	choice.Opt = core.MakeArray()
	for _, c := range opt.Choices {
		choice.Opt.Append(core.MakeString(c))
	}
	choice.SetFlag(model.FieldFlagCombo)
	widget := model.NewPdfAnnotationWidget()
	widget.Rect = core.MakeArrayFromFloats(rect)
	widget.P = page.ToPdfObject()
	widget.F = core.MakeInteger(4)
	widget.Parent = choice.ToPdfObject()
	choice.Annotations = append(choice.Annotations, widget)
	return choice, nil
}

// CircleAnnotationDef defines a circle annotation or ellipse at position (X, Y) and Width and Height.
// The annotation has various style parameters including Fill and Border options and Opacity.
type CircleAnnotationDef struct {
	X             float64
	Y             float64
	Width         float64
	Height        float64
	FillEnabled   bool
	FillColor     *model.PdfColorDeviceRGB
	BorderEnabled bool
	BorderWidth   float64
	BorderColor   *model.PdfColorDeviceRGB
	Opacity       float64
}

// SignatureFieldOpts represents a set of options used to configure
// an appearance widget dictionary.
type SignatureFieldOpts struct {
	// Rect represents the area the signature annotation is displayed on.
	Rect []float64

	// AutoSize specifies if the content of the appearance should be
	// scaled to fit in the annotation rectangle.
	AutoSize bool

	// Font specifies the font of the text content.
	Font *model.PdfFont

	// FontSize specifies the size of the text content.
	FontSize float64

	// LineHeight specifies the height of a line of text in the appearance annotation.
	LineHeight float64

	// TextColor represents the color of the text content displayed.
	TextColor model.PdfColor

	// FillColor represents the background color of the appearance annotation area.
	FillColor model.PdfColor

	// FillOpacity sets the opacity of the background of the appearance annotation area.
	// Range is 0.0 - 1.0. Default is 1.0, opaque.
	FillOpacity float64

	// BorderSize represents border size of the appearance annotation area.
	BorderSize float64

	// BorderColor represents the border color of the appearance annotation area.
	BorderColor model.PdfColor

	// WatermarkImage specifies the image used as a watermark that will be rendered
	// behind the signature.
	WatermarkImage image.Image

	// Image represents the image used for the signature appearance.
	Image image.Image

	// Encoder specifies the image encoder used for image signature. Defaults to flate encoder.
	Encoder core.StreamEncoder

	// ImagePosition specifies the image location relative to the text signature.
	ImagePosition SignatureImagePosition
}

func (s *AppearanceStyle) applyRotation(mk *core.PdfObjectDictionary, w, h float64, cc *contentstream.ContentCreator) (float64, float64) {
	if !s.AllowMK {
		return w, h
	}
	if mk == nil {
		return w, h
	}
	angle, _ := core.GetNumberAsFloat(mk.Get("R"))
	if angle == 0 {
		return w, h
	}
	negAngle := -angle
	bounds := contentdraw.Path{Points: []contentdraw.Point{contentdraw.NewPoint(0, 0).Rotate(negAngle), contentdraw.NewPoint(w, 0).Rotate(negAngle), contentdraw.NewPoint(0, h).Rotate(negAngle), contentdraw.NewPoint(w, h).Rotate(negAngle)}}.GetBoundingBox()
	cc.RotateDeg(angle)
	cc.Translate(bounds.X, bounds.Y)
	return bounds.Width, bounds.Height
}

func textFieldDA(field *model.PdfField) string {
	return inheritedString(field, func(f *model.PdfField) (string, bool) {
		if tf, ok := f.GetContext().(*model.PdfFieldText); ok && tf.DA != nil {
			return tf.DA.Str(), true
		}
		return "", false
	})
}

// FormResetActionOptions holds options for creating a form reset button.
type FormResetActionOptions struct {
	// Rectangle holds the button position, size, and color.
	Rectangle contentdraw.Rectangle

	// Label specifies the text that would be displayed on the button.
	Label string

	// LabelColor specifies the button label color.
	LabelColor model.PdfColor

	// Font specifies a font used for rendering the button label.
	// When omitted it will fallback to use a Helvetica font.
	Font *model.PdfFont

	// FontSize specifies the font size used in rendering the button label.
	// The default font size is 12pt.
	FontSize *float64

	// Fields specifies list of fields that could be resetted.
	// This list may contain indirect object to fields or field names.
	Fields *core.PdfObjectArray

	// IsExclusionList specifies that the fields in the `Fields` array would be excluded form reset process.
	IsExclusionList bool
}

// NewTextField generates a new text field with partial name `name` at location
// specified by `rect` on given `page` and with field specific options `opt`.
func NewTextField(page *model.PdfPage, name string, rect []float64, opt TextFieldOptions) (*model.PdfFieldText, error) {
	if page == nil {
		return nil, errors.New("page not specified")
	}
	if len(name) <= 0 {
		return nil, errors.New("required attribute not specified")
	}
	if len(rect) != 4 {
		return nil, errors.New("invalid range")
	}
	base := model.NewPdfField()
	tf := &model.PdfFieldText{}
	base.SetContext(tf)
	tf.PdfField = base
	tf.T = core.MakeString(name)
	if opt.MaxLen > 0 {
		tf.MaxLen = core.MakeInteger(int64(opt.MaxLen))
	}
	if len(opt.Value) > 0 {
		tf.V = core.MakeString(opt.Value)
	}
	if opt.TextColor != "" {
		color := creator.ColorRGBFromHex(opt.TextColor)
		r, g, b := color.ToRGB()
		fontSize := 12
		if opt.FontSize > 0 {
			fontSize = opt.FontSize
		}
		fontName := "Helvetica"
		if opt.FontName != "" {
			fontName = opt.FontName
		}
		da := fmt.Sprintf("/%s %d Tf %.3f %.3f %.3f rg", fontName, fontSize, r, g, b)
		tf.DA = core.MakeString(da)
	}
	base.SetContext(tf)
	widget := model.NewPdfAnnotationWidget()
	widget.Rect = core.MakeArrayFromFloats(rect)
	widget.P = page.ToPdfObject()
	widget.F = core.MakeInteger(4)
	widget.Parent = tf.ToPdfObject()
	tf.Annotations = append(tf.Annotations, widget)
	return tf, nil
}

func circleAppearance(def CircleAnnotationDef) (*core.PdfObjectDictionary, *model.PdfRectangle, error) {
	form := model.NewXObjectForm()
	form.Resources = model.NewPdfPageResources()
	gsName := ""
	if def.Opacity < 1.0 {
		gsDict := core.MakeDict()
		gsDict.Set("ca", core.MakeFloat(def.Opacity))
		gsDict.Set("CA", core.MakeFloat(def.Opacity))
		err := form.Resources.AddExtGState("gs1", gsDict)
		if err != nil {
			common.Log.Debug("Unable to add extgstate gs1")
			return nil, nil, err
		}
		gsName = "gs1"
	}
	content, bbox, rect, err := circleAppearanceStream(def, gsName)
	if err != nil {
		return nil, nil, err
	}
	err = form.SetContentStream(content, nil)
	if err != nil {
		return nil, nil, err
	}
	form.BBox = bbox.ToPdfObject()
	apDict := core.MakeDict()
	apDict.Set("N", form.ToPdfObject())
	return apDict, rect, nil
}

// NewCheckboxField generates a new checkbox field with partial name `name` at location `rect`
// on specified `page` and with field specific options `opt`.
func NewCheckboxField(page *model.PdfPage, name string, rect []float64, opt CheckboxFieldOptions) (*model.PdfFieldButton, error) {
	if page == nil {
		return nil, errors.New("page not specified")
	}
	if len(name) <= 0 {
		return nil, errors.New("required attribute not specified")
	}
	if len(rect) != 4 {
		return nil, errors.New("invalid range")
	}
	zapf, err := model.NewStandard14Font(model.ZapfDingbatsName)
	if err != nil {
		return nil, err
	}
	base := model.NewPdfField()
	cb := &model.PdfFieldButton{}
	base.SetContext(cb)
	cb.PdfField = base
	cb.T = core.MakeString(name)
	cb.SetType(model.ButtonTypeCheckbox)
	state := "Off"
	if opt.Checked {
		state = "Yes"
	}
	cb.V = core.MakeName(state)
	widget := model.NewPdfAnnotationWidget()
	widget.Rect = core.MakeArrayFromFloats(rect)
	widget.P = page.ToPdfObject()
	widget.F = core.MakeInteger(4)
	widget.Parent = cb.ToPdfObject()
	w := rect[2] - rect[0]
	h := rect[3] - rect[1]
	var buf bytes.Buffer
	buf.WriteString("q\n")
	buf.WriteString("0 0 1 rg\n")
	buf.WriteString("BT\n")
	buf.WriteString("/ZaDb 12 Tf\n")
	buf.WriteString("ET\n")
	buf.WriteString("Q\n")
	cc := contentstream.NewContentCreator()
	cc.Add_q()
	cc.Add_rg(0, 0, 1)
	cc.Add_BT()
	cc.Add_Tf(*core.MakeName("ZaDb"), 12)
	cc.Add_Td(0, 0)
	cc.Add_ET()
	cc.Add_Q()
	offForm := model.NewXObjectForm()
	offForm.SetContentStream(cc.Bytes(), core.NewRawEncoder())
	offForm.BBox = core.MakeArrayFromFloats([]float64{0, 0, w, h})
	offForm.Resources = model.NewPdfPageResources()
	offForm.Resources.SetFontByName("ZaDb", zapf.ToPdfObject())
	cc = contentstream.NewContentCreator()
	cc.Add_q()
	cc.Add_re(0, 0, w, h)
	cc.Add_W().Add_n()
	cc.Add_rg(0, 0, 1)
	cc.Translate(0, 3.0)
	cc.Add_BT()
	cc.Add_Tf(*core.MakeName("ZaDb"), 12)
	cc.Add_Td(0, 0)
	cc.Add_Tj(*core.MakeString("4"))
	cc.Add_ET()
	cc.Add_Q()
	onForm := model.NewXObjectForm()
	onForm.SetContentStream(cc.Bytes(), core.NewRawEncoder())
	onForm.BBox = core.MakeArrayFromFloats([]float64{0, 0, w, h})
	onForm.Resources = model.NewPdfPageResources()
	onForm.Resources.SetFontByName("ZaDb", zapf.ToPdfObject())
	states := core.MakeDict()
	states.Set("Off", offForm.ToPdfObject())
	states.Set("Yes", onForm.ToPdfObject())
	apDict := core.MakeDict()
	apDict.Set("N", states)
	widget.AP = apDict
	widget.AS = core.MakeName(state)
	cb.Annotations = append(cb.Annotations, widget)
	return cb, nil
}

// NewSignatureLine returns a new signature line displayed as a part of the
// signature field appearance.
func NewSignatureLine(desc, text string) *SignatureLine {
	return &SignatureLine{Desc: desc, Text: text}
}

func rectangleAppearanceStream(def RectangleAnnotationDef, gsName string) ([]byte, *model.PdfRectangle, *model.PdfRectangle, error) {
	rectangle := contentdraw.Rectangle{X: 0, Y: 0, Width: def.Width, Height: def.Height, FillEnabled: def.FillEnabled, FillColor: def.FillColor, BorderEnabled: def.BorderEnabled, BorderWidth: 2 * def.BorderWidth, BorderColor: def.BorderColor, Opacity: def.Opacity}
	content, bbox, err := rectangle.Draw(gsName)
	if err != nil {
		return nil, nil, nil, err
	}
	rect := &model.PdfRectangle{}
	rect.Llx = def.X + bbox.Llx
	rect.Lly = def.Y + bbox.Lly
	rect.Urx = def.X + bbox.Urx
	rect.Ury = def.Y + bbox.Ury
	return content, bbox, rect, nil
}

// NewSignatureField returns a new signature field with a visible appearance
// containing the specified signature lines and styled according to the
// specified options.
func NewSignatureField(signature *model.PdfSignature, lines []*SignatureLine, opts *SignatureFieldOpts) (*model.PdfFieldSignature, error) {
	if signature == nil {
		return nil, errors.New("signature cannot be nil")
	}
	ap, err := signatureAppearance(lines, opts)
	if err != nil {
		return nil, err
	}
	field := model.NewPdfFieldSignature(signature)
	field.Rect = core.MakeArrayFromFloats(opts.Rect)
	field.AP = ap
	return field, nil
}

type quadding int

func listBoxAppearance(form *model.PdfAcroForm, widget *model.PdfAnnotationWidget, field *model.PdfFieldChoice, style AppearanceStyle) (*core.PdfObjectDictionary, error) {
	rectArr, hasRect := core.GetArray(widget.Rect)
	if !hasRect {
		return nil, errors.New("invalid Rect")
	}
	rect, err := model.NewPdfRectangle(*rectArr)
	if err != nil {
		return nil, err
	}
	w, h := rect.Width(), rect.Height()
	formW, formH := w, h
	common.Log.Debug("Choice (list box), wa BS: %v", widget.BS)
	daOps, err := contentstream.NewContentStreamParser(textFieldDA(field.PdfField)).Parse()
	if err != nil {
		return nil, err
	}
	mk, hasMK := core.GetDict(widget.MK)
	if hasMK {
		bs, _ := core.GetDict(widget.BS)
		if err := style.applyAppearanceCharacteristics(mk, bs, nil); err != nil {
			return nil, err
		}
	}
	var options []listboxOption
	if field.Opt != nil {
		for _, opt := range field.Opt.Elements() {
			var option listboxOption
			if pair, ok := core.GetArray(opt); ok && pair.Len() == 2 {
				value, okValue := nameOrString(pair.Get(0))
				label, okLabel := nameOrString(pair.Get(1))
				if !okValue || !okLabel {
					common.Log.Debug("ERROR: Opt pair elements not name/string - %v", pair)
					return nil, errors.New("invalid option pair")
				}
				option.value, option.label = value, label
			} else if s, ok := nameOrString(opt); ok {
				option.label = s
				option.value = s
			} else {
				common.Log.Debug("ERROR: Opt not a name/string - %T", opt)
				return nil, errors.New("not a name/string")
			}
			options = append(options, option)
		}
	}
	selected := selectedOptions(field, options)
	resources := model.NewPdfPageResources()
	cc := contentstream.NewContentCreator()
	cc.Add_q()
	w, h = style.applyRotation(mk, w, h, cc)
	if style.BorderSize > 0 {
		drawBorderAndFill(cc, style, w, h)
	}
	if style.DrawAlignmentReticle {
		reticleStyle := style
		reticleStyle.BorderSize = 0.2
		drawAlignmentReticle(cc, reticleStyle, w, h)
	}
	daFont, _, err := style.processDA(field.PdfField, daOps, form.DR, resources, cc)
	if err != nil {
		return nil, err
	}
	font := daFont.Font
	fontSize := daFont.Size
	if fontSize <= 0 {
		fontSize = 12.0
	}
	encoder := font.Encoder()
	if encoder == nil {
		common.Log.Debug("WARN: font encoder is nil. Assuming identity encoder. Output may be incorrect.")
		encoder = textencoding.NewIdentityTextEncoder("Identity-H")
	}
	marginLeft := defaultMarginLeft
	if style.MarginLeft != nil {
		marginLeft = *style.MarginLeft
	}
	rowH := fontSize * 1.15
	if rowH > h && h > 0 {
		fontSize = h / 1.15
		rowH = h
	}
	maxRows := int(h / rowH)
	if maxRows < 1 {
		maxRows = 1
	}
	first := 0
	if ti, ok := core.GetIntVal(field.TI); ok && ti >= 0 && ti < len(options) {
		first = ti
	} else if len(selected) > 0 {
		minSel := len(options)
		for i := range selected {
			if i < minSel {
				minSel = i
			}
		}
		if minSel >= maxRows {
			first = minSel
			if maxFirst := len(options) - maxRows; first > maxFirst {
				first = maxFirst
			}
		}
	}
	highlight := style.ListBoxHighlightColor
	if highlight == nil {
		highlight = defaultHighlightColor()
	}
	borderW := style.BorderSize
	if borderW < 0 {
		borderW = 0
	}
	type visibleRow struct {
		index int
		textY float64
		rowY  float64
	}
	var rows []visibleRow
	top := h
	for i := first; i < len(options) && len(rows) < maxRows; i++ {
		bottom := top - rowH
		baseline := bottom + (rowH-fontSize)/2.0 + fontSize*0.2
		rows = append(rows, visibleRow{index: i, textY: baseline, rowY: bottom})
		top = bottom
	}
	hlW := w - 2*borderW
	if hlW < 0 {
		hlW = 0
	}
	for _, row := range rows {
		if !selected[row.index] {
			continue
		}
		cc.Add_q().SetNonStrokingColor(highlight).Add_re(borderW, row.rowY, hlW, rowH).Add_f().Add_Q()
	}
	cc.Add_BMC("Tx")
	cc.Add_q()
	cc.Add_BT()
	cc.Add_Tf(*core.MakeName(daFont.Name), fontSize)
	prevY := 0.0
	for i, row := range rows {
		if i == 0 {
			cc.Add_Td(marginLeft, row.textY)
		} else {
			cc.Add_Td(0, row.textY-prevY)
		}
		prevY = row.textY
		label := options[row.index].label
		if len(label) == 0 {
			continue
		}
		cc.Add_Tj(*core.MakeString(string(encoder.Encode(label))))
	}
	cc.Add_ET()
	cc.Add_Q()
	cc.Add_EMC()
	cc.Add_Q()
	xform := model.NewXObjectForm()
	xform.Resources = resources
	xform.BBox = core.MakeArrayFromFloats([]float64{0, 0, formW, formH})
	if err := setClippedContent(xform, cc.Bytes()); err != nil {
		return nil, err
	}
	apDict := core.MakeDict()
	apDict.Set("N", xform.ToPdfObject())
	return apDict, nil
}

func nameOrString(obj core.PdfObject) (string, bool) {
	if s, ok := core.GetString(obj); ok {
		return s.Decoded(), true
	}
	if nm, ok := core.GetName(obj); ok {
		return nm.String(), true
	}
	return "", false
}

func (f *AppearanceFont) fillName() {
	if f.Font == nil || f.Name != "" {
		return
	}
	desc := f.Font.FontDescriptor()
	if desc == nil || desc.FontName == nil {
		return
	}
	f.Name = desc.FontName.String()
}

// WrapContentStream ensures that the entire content stream for a `page` is wrapped within q ... Q operands.
// Ensures that following operands that are added are not affected by additional operands that are added.
// Implements interface model.ContentStreamWrapper.
func (a ImageFieldAppearance) WrapContentStream(page *model.PdfPage) error {
	content, err := page.GetAllContentStreams()
	if err != nil {
		return err
	}
	parser := contentstream.NewContentStreamParser(content)
	ops, err := parser.Parse()
	if err != nil {
		return err
	}
	ops.WrapIfNeeded()
	contents := []string{ops.String()}
	return page.SetContentStreams(contents, newFlateEncoder())
}

func imageForm(_, _ float64, img *model.Image, _ AppearanceStyle) (*model.XObjectForm, error) {
	xImg, err := model.NewXObjectImageFromImage(img, nil, core.NewFlateEncoder())
	if err != nil {
		return nil, err
	}
	xImg.Decode = core.MakeArrayFromFloats([]float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0})
	resources := model.NewPdfPageResources()
	resources.ProcSet = core.MakeArray(core.MakeName("PDF"), core.MakeName("ImageC"))
	resources.SetXObjectImageByName(core.PdfObjectName("Im0"), xImg)
	cc := contentstream.NewContentCreator()
	cc.Add_q()
	cc.Add_cm(float64(img.Width), 0, 0, float64(img.Height), 0, 0)
	cc.Add_Do("Im0")
	cc.Add_Q()
	form := model.NewXObjectForm()
	form.FormType = core.MakeInteger(1)
	form.BBox = core.MakeArrayFromFloats([]float64{0, 0, float64(img.Width), float64(img.Height)})
	form.Resources = resources
	form.SetContentStream(cc.Bytes(), newFlateEncoder())
	return form, nil
}

func drawAlignmentReticle(cc *contentstream.ContentCreator, style AppearanceStyle, w, h float64) {
	cc.Add_q().Add_re(0, 0, w, h).Add_re(0, h/2, w, h/2).Add_re(0, 0, w, h).Add_re(w/2, 0, w/2, h).Add_w(style.BorderSize).SetStrokingColor(style.BorderColor).SetNonStrokingColor(style.FillColor).Add_B().Add_Q()
}

func selectedOptions(field *model.PdfFieldChoice, options []listboxOption) map[int]bool {
	selected := map[int]bool{}
	if field.I != nil {
		for _, el := range field.I.Elements() {
			if idx, ok := core.GetIntVal(el); ok && idx >= 0 && idx < len(options) {
				selected[idx] = true
			}
		}
		return selected
	}
	var values []string
	switch v := core.TraceToDirectObject(field.V).(type) {
	case *core.PdfObjectString:
		values = append(values, v.Decoded())
	case *core.PdfObjectArray:
		for _, el := range v.Elements() {
			if s, ok := core.GetString(el); ok {
				values = append(values, s.Decoded())
			}
		}
	}
	for _, val := range values {
		found := false
		for i, opt := range options {
			if opt.value == val {
				selected[i] = true
				found = true
			}
		}
		if found {
			continue
		}
		for i, opt := range options {
			if opt.label == val {
				selected[i] = true
			}
		}
	}
	return selected
}

func variableTextDA(field *model.PdfField) string {
	return inheritedString(field, func(f *model.PdfField) (string, bool) {
		if f.VariableText != nil && f.VariableText.DA != nil {
			return f.VariableText.DA.Str(), true
		}
		return "", false
	})
}

// ImageFieldOptions defines optional parameters for a push button with image attach capability form field.
type ImageFieldOptions struct {
	Image *model.Image
	style AppearanceStyle
}

func newPushButton(page *model.PdfPage, rect contentdraw.Rectangle, name string, label string, labelColor model.PdfColor, font *model.PdfFont, fontSize *float64, action core.PdfObject) (*model.PdfFieldButton, error) {
	x, y := rect.X, rect.Y
	w := rect.Width
	h := rect.Height
	if rect.FillColor == nil {
		rect.FillColor = model.NewPdfColorDeviceGray(0.7)
	}
	if labelColor == nil {
		labelColor = model.NewPdfColorDeviceGray(0)
	}
	if font == nil {
		stdFont, err := model.NewStandard14Font("Helvetica")
		if err != nil {
			return nil, err
		}
		font = stdFont
	}
	base := model.NewPdfField()
	btn := &model.PdfFieldButton{}
	base.SetContext(btn)
	btn.PdfField = base
	btn.T = core.MakeString(name)
	btn.SetType(model.ButtonTypePush)
	btn.V = core.MakeName("Off")
	btn.Ff = core.MakeInteger(4)
	mk := core.MakeDict()
	mk.Set(*core.MakeName("CA"), core.MakeString(label))
	desc, err := font.GetFontDescriptor()
	if err != nil {
		return nil, err
	}
	fontName := core.MakeName("Helvetica")
	size := 12.0
	if desc != nil && desc.FontName != nil {
		fontName, _ = core.GetName(desc.FontName)
	}
	if fontSize != nil {
		size = *fontSize
	}
	cc := contentstream.NewContentCreator()
	cc.Add_q()
	cc.SetNonStrokingColor(rect.FillColor)
	cc.Add_re(0, 0, w, h)
	cc.Add_f()
	cc.Add_Q()
	cc.Add_q()
	cc.Add_BT()
	textW := 0.0
	for _, r := range label {
		metrics, ok := font.GetRuneMetrics(r)
		if !ok {
			common.Log.Debug("Font does not have rune metrics for %v - skipping", r)
			continue
		}
		textW += metrics.Wx
	}
	textW = textW / 1000.0 * size
	var capHeight float64
	if desc != nil {
		capHeight, err = desc.GetCapHeight()
		if err != nil {
			common.Log.Debug("ERROR: Unable to get font CapHeight: %v", err)
		}
	}
	if int(capHeight) <= 0 {
		common.Log.Debug("WARN: CapHeight not available - setting to 1000")
		capHeight = 1000
	}
	scaledCap := capHeight / 1000.0 * size
	ty := (h - scaledCap) / 2.0
	tx := (w - textW) / 2.0
	cc.Add_Tf(*fontName, size)
	cc.SetNonStrokingColor(labelColor)
	cc.Add_Td(tx, ty)
	cc.Add_Tj(*core.MakeString(label))
	cc.Add_ET()
	cc.Add_Q()
	form := model.NewXObjectForm()
	form.SetContentStream(cc.Bytes(), core.NewRawEncoder())
	form.BBox = core.MakeArrayFromFloats([]float64{0, 0, w, h})
	form.Resources = model.NewPdfPageResources()
	form.Resources.SetFontByName(*fontName, font.ToPdfObject())
	apDict := core.MakeDict()
	apDict.Set("N", form.ToPdfObject())
	widget := model.NewPdfAnnotationWidget()
	widget.Rect = core.MakeArrayFromFloats([]float64{x, y, x + w, y + h})
	widget.P = page.ToPdfObject()
	widget.F = core.MakeInteger(4)
	widget.Parent = btn.ToPdfObject()
	widget.A = action
	widget.MK = mk
	widget.AP = apDict
	btn.Annotations = append(btn.Annotations, widget)
	return btn, nil
}

// NewFormResetButtonField would create a reset button in specified page according to the parameter in `FormResetActionOptions`.
func NewFormResetButtonField(page *model.PdfPage, opt FormResetActionOptions) (*model.PdfFieldButton, error) {
	action := model.NewPdfActionResetForm()
	action.Fields = opt.Fields
	action.Flags = core.MakeInteger(0)
	if opt.IsExclusionList {
		action.Flags = core.MakeInteger(1)
	}
	btn, err := newPushButton(page, opt.Rectangle, "btnReset", opt.Label, opt.LabelColor, opt.Font, opt.FontSize, action.ToPdfObject())
	if err != nil {
		return nil, err
	}
	return btn, nil
}

func checkboxAppearance(widget *model.PdfAnnotationWidget, _ *model.PdfFieldButton, _ *model.PdfPageResources, style AppearanceStyle) (*core.PdfObjectDictionary, error) {
	rectArr, hasRect := core.GetArray(widget.Rect)
	if !hasRect {
		return nil, errors.New("invalid Rect")
	}
	rect, err := model.NewPdfRectangle(*rectArr)
	if err != nil {
		return nil, err
	}
	w, h := rect.Width(), rect.Height()
	formW, formH := w, h
	common.Log.Debug("Checkbox, wa BS: %v", widget.BS)
	zapf, err := model.NewStandard14Font("ZapfDingbats")
	if err != nil {
		return nil, err
	}
	mk, ok := core.GetDict(widget.MK)
	if ok {
		bs, _ := core.GetDict(widget.BS)
		err := style.applyAppearanceCharacteristics(mk, bs, zapf)
		if err != nil {
			return nil, err
		}
	}
	onForm := model.NewXObjectForm()
	{
		cc := contentstream.NewContentCreator()
		if style.BorderSize > 0 {
			drawBorderAndFill(cc, style, w, h)
		}
		if style.DrawAlignmentReticle {
			reticleStyle := style
			reticleStyle.BorderSize = 0.2
			drawAlignmentReticle(cc, reticleStyle, w, h)
		}
		w, h = style.applyRotation(mk, w, h, cc)
		fontSize := style.AutoFontSizeFraction * h
		metrics, ok := zapf.GetRuneMetrics(style.CheckmarkRune)
		if !ok {
			return nil, errors.New("glyph not found")
		}
		encoder := zapf.Encoder()
		encoded := encoder.Encode(string(style.CheckmarkRune))
		glyphW := metrics.Wx * fontSize / 1000.0
		markHeight := 705.0
		glyphH := markHeight / 1000.0 * fontSize
		x := defaultMarginLeft
		if style.MarginLeft != nil {
			x = *style.MarginLeft
		}
		y := 1.0
		if glyphW < w {
			x = (w - glyphW) / 2.0
		}
		if glyphH < h {
			y = (h - glyphH) / 2.0
		}
		cc.Add_q().Add_g(0).Add_BT().Add_Tf("ZaDb", fontSize).Add_Td(x, y).Add_Tj(*core.MakeStringFromBytes(encoded)).Add_ET().Add_Q()
		onForm.Resources = model.NewPdfPageResources()
		onForm.Resources.SetFontByName("ZaDb", zapf.ToPdfObject())
		onForm.BBox = core.MakeArrayFromFloats([]float64{0, 0, formW, formH})
		if err := setClippedContent(onForm, cc.Bytes()); err != nil {
			return nil, err
		}
	}
	offForm := model.NewXObjectForm()
	{
		cc := contentstream.NewContentCreator()
		if style.BorderSize > 0 {
			drawBorderAndFill(cc, style, w, h)
		}
		offForm.BBox = core.MakeArrayFromFloats([]float64{0, 0, formW, formH})
		if err := setClippedContent(offForm, cc.Bytes()); err != nil {
			return nil, err
		}
	}
	onName := core.PdfObjectName("Yes")
	ap, ok := core.GetDict(widget.AP)
	if ok && ap != nil {
		n := core.TraceToDirectObject(ap.Get("N"))
		switch d := n.(type) {
		case *core.PdfObjectDictionary:
			keys := d.Keys()
			for _, key := range keys {
				if key != "Off" {
					onName = key
				}
			}
		}
	}
	states := core.MakeDict()
	states.Set("Off", offForm.ToPdfObject())
	states.Set(onName, onForm.ToPdfObject())
	apDict := core.MakeDict()
	apDict.Set("N", states)
	return apDict, nil
}

func comboOptionAppearance(field *model.PdfField, w, h float64, text string, style AppearanceStyle, daOps *contentstream.ContentStreamOperations, dr *model.PdfPageResources, mk *core.PdfObjectDictionary) (*model.XObjectForm, error) {
	resources := model.NewPdfPageResources()
	formW, formH := w, h
	cc := contentstream.NewContentCreator()
	if style.BorderSize > 0 {
		drawBorderAndFill(cc, style, w, h)
	}
	if style.DrawAlignmentReticle {
		reticleStyle := style
		reticleStyle.BorderSize = 0.2
		drawAlignmentReticle(cc, reticleStyle, w, h)
	}
	cc.Add_BMC("Tx")
	cc.Add_q()
	cc.Add_BT()
	w, h = style.applyRotation(mk, w, h, cc)
	daFont, hasTf, err := style.processDA(field, daOps, dr, resources, cc)
	if err != nil {
		return nil, err
	}
	font := daFont.Font
	fontSize := daFont.Size
	fontName := core.MakeName(daFont.Name)
	autoSize := fontSize == 0
	if autoSize && hasTf {
		fontSize = h * style.AutoFontSizeFraction
	}
	encoder := font.Encoder()
	if encoder == nil {
		common.Log.Debug("WARN: font encoder is nil. Assuming identity encoder. Output may be incorrect.")
		encoder = textencoding.NewIdentityTextEncoder("Identity-H")
	}
	if len(text) == 0 {
		return nil, nil
	}
	marginLeft := defaultMarginLeft
	if style.MarginLeft != nil {
		marginLeft = *style.MarginLeft
	}
	textWidth := 0.0
	if encoder != nil {
		for _, r := range text {
			metrics, ok := font.GetRuneMetrics(r)
			if !ok {
				common.Log.Debug("Font does not have rune metrics for %v - skipping", r)
				continue
			}
			textWidth += metrics.Wx
		}
		text = string(encoder.Encode(text))
	}
	if fontSize == 0 || autoSize && textWidth > 0 && marginLeft+textWidth*fontSize/1000.0 > w {
		fontSize = 0.95 * 1000.0 * (w - marginLeft) / textWidth
	}
	lineH := 1.0 * fontSize
	y := 2.0
	{
		needed := lineH
		if autoSize && y+needed > h {
			fontSize = 0.95 * (h - y)
			lineH = 1.0 * fontSize
			needed = lineH
		}
		if h > needed {
			y = (h - needed) / 2.0
			y += 1.50
		}
	}
	cc.Add_Tf(*fontName, fontSize)
	cc.Add_Td(marginLeft, y)
	cc.Add_Tj(*core.MakeString(text))
	cc.Add_ET()
	cc.Add_Q()
	cc.Add_EMC()
	xform := model.NewXObjectForm()
	xform.Resources = resources
	xform.BBox = core.MakeArrayFromFloats([]float64{0, 0, formW, formH})
	if err := setClippedContent(xform, cc.Bytes()); err != nil {
		return nil, err
	}
	return xform, nil
}

// GenerateAppearanceDict generates an appearance dictionary for widget annotation `wa` for the `field` in `form`.
// Implements interface model.FieldAppearanceGenerator.
func (fa FieldAppearance) GenerateAppearanceDict(form *model.PdfAcroForm, field *model.PdfField, wa *model.PdfAnnotationWidget) (*core.PdfObjectDictionary, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("GenerateAppearanceDict for %v  V: %+v", field.PartialName(), field.V)
	}
	_, isText := field.GetContext().(*model.PdfFieldText)
	ap, hasAP := core.GetDict(wa.AP)
	hasAP = hasAP && ap != nil
	hasN := hasAP && ap.Get("N") != nil
	if hasN && fa.OnlyIfMissing && (!isText || !fa.RegenerateTextFields) {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Already populated - ignoring")
		}
		return ap, nil
	}
	if form.DR == nil {
		form.DR = model.NewPdfPageResources()
	}
	switch ctx := field.GetContext().(type) {
	case *model.PdfFieldText:
		tf := ctx
		if da := textFieldDA(tf.PdfField); da == "" {
			tf.DA = form.DA
		}
		if fa.style != nil {
			if fa.style.FieldColors != nil && fa.style.FieldColors[field.PartialName()] != nil {
				color := fa.style.FieldColors[field.PartialName()]
				da, err := daWithColor(tf, color)
				if err != nil {
					return nil, err
				}
				tf.DA = da
			} else if fa.style.TextColor != nil {
				da, err := daWithColor(tf, fa.style.TextColor)
				if err != nil {
					return nil, err
				}
				tf.DA = da
			}
		}
		switch {
		case tf.Flags().Has(model.FieldFlagPassword):
			return nil, nil
		case tf.Flags().Has(model.FieldFlagFileSelect):
			return nil, nil
		case tf.Flags().Has(model.FieldFlagComb):
			if tf.MaxLen != nil {
				ap, err := combTextFieldAppearance(wa, tf, form.DR, fa.Style())
				if err != nil {
					return nil, err
				}
				return ap, nil
			}
		}
		ap, err := textFieldAppearance(wa, tf, form.DR, fa.Style())
		if err != nil {
			return nil, err
		}
		return ap, nil
	case *model.PdfFieldButton:
		btn := ctx
		if btn.IsCheckbox() {
			ap, err := checkboxAppearance(wa, btn, form.DR, fa.Style())
			if err != nil {
				return nil, err
			}
			return ap, nil
		}
		if btn.IsPush() {
			ap, err := pushButtonAppearance(form, wa, btn, fa.Style())
			if err != nil {
				return nil, err
			}
			return ap, nil
		}
		common.Log.Debug("TODO: UNHANDLED button type: %+v", btn.GetType())
	case *model.PdfFieldChoice:
		choice := ctx
		switch {
		case choice.Flags().Has(model.FieldFlagCombo):
			ap, err := comboBoxAppearance(form, wa, choice, fa.Style())
			if err != nil {
				return nil, err
			}
			return ap, nil
		default:
			ap, err := listBoxAppearance(form, wa, choice, fa.Style())
			if err != nil {
				return nil, err
			}
			return ap, nil
		}
	case *model.PdfFieldSignature:
		sigField := ctx
		if hasN {
			return ap, nil
		}
		lines := signatureLines(sigField.V)
		if len(lines) == 0 {
			common.Log.Debug("Signature field %s has no printable signature info - skipping appearance generation", field.PartialName())
			return ap, nil
		}
		opts := NewSignatureFieldOpts()
		if arr, ok := core.GetArray(wa.Rect); ok {
			if rect, err := arr.ToFloat64Array(); err == nil && len(rect) == 4 {
				opts.Rect = []float64{math.Min(rect[0], rect[2]), math.Min(rect[1], rect[3]), math.Max(rect[0], rect[2]), math.Max(rect[1], rect[3])}
			}
		}
		da := variableTextDA(field)
		if da == "" && form.DA != nil {
			da = form.DA.Str()
		}
		daOps, err := contentstream.NewContentStreamParser(da).Parse()
		if err != nil {
			return nil, err
		}
		style := fa.Style()
		daFont, _, err := style.processDA(field, daOps, form.DR, nil, contentstream.NewContentCreator())
		if err != nil {
			return nil, err
		}
		opts.Font = daFont.Font
		if daFont.Size > 0 {
			opts.FontSize = daFont.Size
		}
		if color := colorFromOps(daOps); color != nil {
			opts.TextColor = color
		}
		if style.TextColor != nil {
			opts.TextColor = style.TextColor
		}
		sigAP, err := signatureAppearance(lines, opts)
		if err != nil {
			return nil, err
		}
		if hasAP {
			ap.Set("N", sigAP.Get("N"))
			return ap, nil
		}
		return sigAP, nil
	default:
		common.Log.Debug("TODO: UNHANDLED field type: %T", ctx)
	}
	return nil, nil
}

// Style returns the appearance style of `fa`. If not specified, returns default style.
func (fa FieldAppearance) Style() AppearanceStyle {
	if fa.style != nil {
		return *fa.style
	}
	margin := defaultMarginLeft
	return AppearanceStyle{AutoFontSizeFraction: 0.65, CheckmarkRune: '✔', BorderSize: 0.0, BorderColor: model.NewPdfColorDeviceGray(0), FillColor: model.NewPdfColorDeviceGray(1), MultilineLineHeight: 1.2, MultilineVAlignMiddle: false, DrawAlignmentReticle: false, AllowMK: true, MarginLeft: &margin, ListBoxHighlightColor: defaultHighlightColor()}
}

// AppearanceFont represents a font used for generating the appearance of a
// field in the filling/flattening process.
type AppearanceFont struct {
	// Name represents the name of the font which will be added to the
	// AcroForm resources (DR).
	Name string

	// Font represents the actual font used for the field appearance.
	Font *model.PdfFont

	// Size represents the size of the font used for the field appearance.
	// If the font size is 0, the value of the FallbackSize field of the
	// AppearanceFontStyle is used, if set. Otherwise, the font size is
	// calculated based on the available annotation height and on the
	// AutoFontSizeFraction field of the AppearanceStyle.
	Size float64
}

// TextFieldOptions defines optional parameter for a text field in a form.
type TextFieldOptions struct {
	MaxLen int
	Value  string

	// TextColor defines the color of the text in hex format. e.g #43fd23.
	// If it has an invalid value a #000000 (black) color is taken as default
	TextColor string

	// FontName defines the font of the text. Helvetica font is the default one.
	// It is recommended to use one of 14 standard PDF fonts.
	FontName string

	// FontSize defines the font size of the text, 12 is used by default.
	FontSize int
}

// FormSubmitActionOptions holds options for creating a form submit button.
type FormSubmitActionOptions struct {
	// Rectangle holds the button position, size, and color.
	Rectangle contentdraw.Rectangle

	// Url specifies the URL where the fieds will be submitted.
	Url string

	// Label specifies the text that would be displayed on the button.
	Label string

	// LabelColor specifies the button label color.
	LabelColor model.PdfColor

	// Font specifies a font used for rendering the button label.
	// When omitted it will fallback to use a Helvetica font.
	Font *model.PdfFont

	// FontSize specifies the font size used in rendering the button label.
	// The default font size is 12pt.
	FontSize *float64

	// Fields specifies list of fields that could be submitted.
	// This list may contain indirect object to fields or field names.
	Fields *core.PdfObjectArray

	// IsExclusionList specifies that the fields contain in `Fields` array would not be submitted.
	IsExclusionList bool

	// IncludeEmptyFields specifies if all fields would be submitted even though it's value is empty.
	IncludeEmptyFields bool

	// SubmitAsPDF specifies that the document shall be submitted as PDF.
	// If set then all the other flags shall be ignored.
	SubmitAsPDF bool
}

func shapeText(s string) string {
	if len(s) == 0 {
		return s
	}
	ltr := core.IsTextWriteDirectionLTR(s)
	if !ltr {
		shaped, err := textshaping.ArabicShape(s)
		if err != nil {
			common.Log.Debug("ERROR applying Arabic text shaping: %v", err.Error())
			return s
		}
		return core.FormatWriteDirectionLTR(shaped, false)
	}
	return s
}

// SignatureImagePosition specifies the image signature location relative to the text signature.
// If text signature is not defined, this position will be ignored.
type SignatureImagePosition int

func inheritedString(field *model.PdfField, get func(*model.PdfField) (string, bool)) string {
	for f := field; f != nil; f = f.Parent {
		if s, ok := get(f); ok {
			return s
		}
	}
	return ""
}

// InkAnnotationDef holds base information for constructing an ink annotation.
type InkAnnotationDef struct {
	// Paths is the array of stroked paths which compose the annotation.
	Paths []contentdraw.Path

	// Color is the color of the line. Default to black.
	Color *model.PdfColorDeviceRGB

	// LineWidth is the width of the line.
	LineWidth float64
}

// WrapContentStream ensures that the entire content stream for a `page` is wrapped within q ... Q operands.
// Ensures that following operands that are added are not affected by additional operands that are added.
// Implements interface model.ContentStreamWrapper.
func (fa FieldAppearance) WrapContentStream(page *model.PdfPage) error {
	content, err := page.GetAllContentStreams()
	if err != nil {
		return err
	}
	parser := contentstream.NewContentStreamParser(content)
	ops, err := parser.Parse()
	if err != nil {
		return err
	}
	ops.WrapIfNeeded()
	contents := []string{ops.String()}
	return page.SetContentStreams(contents, newFlateEncoder())
}

// NewImageField generates a new image field with partial name `name` at location `rect`
// on specified `page` and with field specific options `opt`.
func NewImageField(page *model.PdfPage, name string, rect []float64, opt ImageFieldOptions) (*model.PdfFieldButton, error) {
	if page == nil {
		return nil, errors.New("page not specified")
	}
	if len(name) <= 0 {
		return nil, errors.New("required attribute not specified")
	}
	if len(rect) != 4 {
		return nil, errors.New("invalid range")
	}
	base := model.NewPdfField()
	btn := &model.PdfFieldButton{}
	btn.PdfField = base
	base.SetContext(btn)
	btn.SetType(model.ButtonTypePush)
	btn.T = core.MakeString(name)
	widget := model.NewPdfAnnotationWidget()
	widget.Rect = core.MakeArrayFromFloats(rect)
	widget.P = page.ToPdfObject()
	widget.F = core.MakeInteger(4)
	widget.Parent = btn.ToPdfObject()
	w := rect[2] - rect[0]
	h := rect[3] - rect[1]
	style := opt.style
	cc := contentstream.NewContentCreator()
	if style.BorderSize > 0 {
		drawBorderAndFill(cc, style, w, h)
	}
	if style.DrawAlignmentReticle {
		reticleStyle := style
		reticleStyle.BorderSize = 0.2
		drawAlignmentReticle(cc, reticleStyle, w, h)
	}
	frm, err := imageForm(w, h, opt.Image, style)
	if err != nil {
		return nil, err
	}
	mk, ok := core.GetDict(widget.MK)
	if ok {
		mk.Set("l", frm.ToPdfObject())
	}
	xobjs := core.MakeDict()
	xobjs.Set("FRM", frm.ToPdfObject())
	resources := model.NewPdfPageResources()
	resources.ProcSet = core.MakeArray(core.MakeName("PDF"))
	resources.XObject = xobjs
	innerW := w - 2
	innerH := h - 2
	cc.Add_q()
	cc.Add_re(1, 1, innerW, innerH)
	cc.Add_W()
	cc.Add_n()
	innerW -= 2
	innerH -= 2
	cc.Add_q()
	cc.Add_re(2, 2, innerW, innerH)
	cc.Add_W()
	cc.Add_n()
	scale := math.Min(innerW/float64(opt.Image.Width), innerH/float64(opt.Image.Height))
	cc.Add_cm(scale, 0, 0, scale, (w/2)-(float64(opt.Image.Width)*scale/2)+2, 2)
	cc.Add_Do("FRM")
	cc.Add_Q()
	cc.Add_Q()
	form := model.NewXObjectForm()
	form.FormType = core.MakeInteger(1)
	form.Resources = resources
	form.BBox = core.MakeArrayFromFloats([]float64{0, 0, w, h})
	form.Matrix = core.MakeArrayFromFloats([]float64{1.0, 0.0, 0.0, 1.0, 0.0, 0.0})
	form.SetContentStream(cc.Bytes(), newFlateEncoder())
	apDict := core.MakeDict()
	apDict.Set("N", form.ToPdfObject())
	widget.AP = apDict
	btn.Annotations = append(btn.Annotations, widget)
	return btn, nil
}

// LineAnnotationDef defines a line between point 1 (X1,Y1) and point 2 (X2,Y2).  The line ending styles can be none
// (regular line), or arrows at either end.  The line also has a specified width, color and opacity.
type LineAnnotationDef struct {
	X1               float64
	Y1               float64
	X2               float64
	Y2               float64
	LineColor        *model.PdfColorDeviceRGB
	Opacity          float64
	LineWidth        float64
	LineEndingStyle1 contentdraw.LineEndingStyle
	LineEndingStyle2 contentdraw.LineEndingStyle
}

func inkAppearance(def *InkAnnotationDef) (*core.PdfObjectDictionary, *model.PdfRectangle, error) {
	form := model.NewXObjectForm()
	content, bbox, err := inkAppearanceStream(def)
	if err != nil {
		return nil, nil, err
	}
	err = form.SetContentStream(content, nil)
	if err != nil {
		return nil, nil, err
	}
	form.BBox = bbox.ToPdfObject()
	form.Resources = model.NewPdfPageResources()
	form.Resources.ProcSet = core.MakeArray(core.MakeName("PDF"))
	apDict := core.MakeDict()
	apDict.Set("N", form.ToPdfObject())
	return apDict, bbox, nil
}

// AppearanceStyle defines style parameters for appearance stream generation.
type AppearanceStyle struct {
	// How much of Rect height to fill when autosizing text.
	AutoFontSizeFraction float64

	// CheckmarkRune is a rune used for check mark in checkboxes (for ZapfDingbats font).
	CheckmarkRune rune
	BorderSize    float64
	BorderColor   model.PdfColor
	FillColor     model.PdfColor

	// ListBoxHighlightColor is the background color used to highlight the
	// selected option(s) of a list box choice field. If nil, a default
	// light blue is used.
	ListBoxHighlightColor model.PdfColor

	// Multiplier for lineheight for multi line text.
	MultilineLineHeight   float64
	MultilineVAlignMiddle bool

	// Visual guide checking alignment of field contents (debugging).
	DrawAlignmentReticle bool

	// Allow field MK appearance characteristics to override style settings.
	AllowMK bool

	// Fonts holds appearance styles for fonts.
	Fonts *AppearanceFontStyle

	// MarginLeft represents the amount of space to leave on the left side of
	// the form field bounding box when generating appearances (default: 2.0).
	MarginLeft *float64
	TextColor  model.PdfColor

	// FieldColors holds separate set of appearance styles for fields.
	FieldColors map[string]model.PdfColor
}

func bezierControlPoints(pts []contentdraw.Point) (ctrl1 []contentdraw.Point, ctrl2 []contentdraw.Point, err error) {
	n := len(pts) - 1
	if len(pts) < 1 {
		return nil, nil, errors.New("at least two points required to calculate curve control points")
	}
	if n == 1 {
		c1 := contentdraw.Point{X: (2*pts[0].X + pts[1].X) / 3, Y: (2*pts[0].Y + pts[1].Y) / 3}
		ctrl1 = append(ctrl1, c1)
		ctrl2 = append(ctrl2, contentdraw.Point{X: 2*c1.X - pts[0].X, Y: 2*c1.Y - pts[0].Y})
		return ctrl1, ctrl2, nil
	}
	rhs := make([]float64, n)
	for i := 1; i < n-1; i++ {
		rhs[i] = 4*pts[i].X + 2*pts[i+1].X
	}
	rhs[0] = pts[0].X + 2*pts[1].X
	rhs[n-1] = (8*pts[n-1].X + pts[n].X) / 2.0
	xs := firstControlPoints(rhs)
	for i := 1; i < n-1; i++ {
		rhs[i] = 4*pts[i].Y + 2*pts[i+1].Y
	}
	rhs[0] = pts[0].Y + 2*pts[1].Y
	rhs[n-1] = (8*pts[n-1].Y + pts[n].Y) / 2.0
	ys := firstControlPoints(rhs)
	ctrl1 = make([]contentdraw.Point, n)
	ctrl2 = make([]contentdraw.Point, n)
	for i := 0; i < n; i++ {
		ctrl1[i] = contentdraw.Point{X: precision.RoundDefault(xs[i]), Y: precision.RoundDefault(ys[i])}
		if i < n-1 {
			ctrl2[i] = contentdraw.Point{X: precision.RoundDefault(2*pts[i+1].X - xs[i+1]), Y: precision.RoundDefault(2*pts[i+1].Y - ys[i+1])}
		} else {
			ctrl2[i] = contentdraw.Point{X: precision.RoundDefault((pts[n].X + xs[n-1]) / 2), Y: precision.RoundDefault((pts[n].Y + ys[n-1]) / 2)}
		}
	}
	return ctrl1, ctrl2, nil
}

// AppearanceFontStyle defines font style characteristics for form fields,
// used in the filling/flattening process.
type AppearanceFontStyle struct {
	// Fallback represents a global font fallback, used for fields which do
	// not specify a font in their default appearance (DA). The fallback is
	// also used if there is a font specified in the DA, but it is not
	// found in the AcroForm resources (DR).
	Fallback *AppearanceFont

	// FallbackSize represents a global font size fallback used for fields
	// which do not specify a font size in their default appearance (DA).
	// The fallback size is applied only if its value is larger than zero.
	FallbackSize float64

	// FieldFallbacks defines font fallbacks for specific fields. The map keys
	// represent the names of the fields (which can be specified by their
	// partial or full names). Specific field fallback fonts take precedence
	// over the global font fallback.
	FieldFallbacks map[string]*AppearanceFont

	// ForceReplace forces the replacement of fonts in the filling/flattening
	// process, even if the default appearance (DA) specifies a valid font.
	// If no fallback font is provided, setting this field has no effect.
	ForceReplace bool
}

// RectangleAnnotationDef is a rectangle defined with a specified Width and Height and a lower left corner at (X,Y).
// The rectangle can optionally have a border and a filling color.
// The Width/Height includes the border (if any specified).
type RectangleAnnotationDef struct {
	X             float64
	Y             float64
	Width         float64
	Height        float64
	FillEnabled   bool
	FillColor     *model.PdfColorDeviceRGB
	BorderEnabled bool
	BorderWidth   float64
	BorderColor   *model.PdfColorDeviceRGB
	Opacity       float64
}

func colorFromOps(ops *contentstream.ContentStreamOperations) model.PdfColor {
	if ops == nil {
		return nil
	}
	var color model.PdfColor
	for _, op := range *ops {
		nums, err := core.GetNumbersAsFloat(op.Params)
		if err != nil {
			continue
		}
		switch {
		case op.Operand == "g" && len(nums) == 1:
			color = model.NewPdfColorDeviceGray(nums[0])
		case op.Operand == "rg" && len(nums) == 3:
			color = model.NewPdfColorDeviceRGB(nums[0], nums[1], nums[2])
		case op.Operand == "k" && len(nums) == 4:
			color = model.NewPdfColorDeviceCMYK(nums[0], nums[1], nums[2], nums[3])
		}
	}
	return color
}

func setClippedContent(form *model.XObjectForm, content []byte) error {
	var bbox []float64
	if arr, ok := core.GetArray(form.BBox); ok {
		if nums, err := core.GetNumbersAsFloat(arr.Elements()); err == nil && len(nums) == 4 {
			bbox = nums
		}
	}
	if bbox == nil {
		common.Log.Debug("ERROR: form XObject missing valid BBox - skipping explicit clip")
		return form.SetContentStream(content, newFlateEncoder())
	}
	x0, x1 := math.Min(bbox[0], bbox[2]), math.Max(bbox[0], bbox[2])
	y0, y1 := math.Min(bbox[1], bbox[3]), math.Max(bbox[1], bbox[3])
	w, h := x1-x0, y1-y0
	ops, err := contentstream.NewContentStreamParser(string(content)).Parse()
	if err != nil {
		common.Log.Debug("Unable to parse appearance stream for clip detection: %v", err)
		ops = nil
	}
	if isClippedTo(ops, x0, y0, w, h) {
		return form.SetContentStream(content, newFlateEncoder())
	}
	content = balanceGraphicsState(ops, content)
	cc := contentstream.NewContentCreator()
	cc.Add_q().Add_re(x0, y0, w, h).Add_W().Add_n()
	out := append(cc.Bytes(), content...)
	if n := len(out); n > 0 && out[n-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, 'Q')
	return form.SetContentStream(out, newFlateEncoder())
}

// ImageFieldAppearance implements interface model.FieldAppearanceGenerator and generates appearance streams
// for attaching an image to a button field.
type ImageFieldAppearance struct {
	OnlyIfMissing bool
	style         *AppearanceStyle
}

func firstControlPoints(rhs []float64) []float64 {
	var (
		n   = len(rhs)
		x   = make([]float64, n)
		tmp = make([]float64, n)
	)
	b := 2.0
	x[0] = rhs[0] / b
	for i := 1; i < n; i++ {
		tmp[i] = 1 / b
		if i < n-1 {
			b = 4.0
		} else {
			b = 3.5
		}
		b -= tmp[i]
		x[i] = (rhs[i] - x[i-1]) / b
	}
	for i := 1; i < n; i++ {
		x[n-i-1] -= tmp[n-i] * x[n-i]
	}
	return x
}

// GenerateAppearanceDict generates an appearance dictionary for widget annotation `wa` for the `field` in `form`.
// Implements interface model.FieldAppearanceGenerator.
func (a ImageFieldAppearance) GenerateAppearanceDict(form *model.PdfAcroForm, field *model.PdfField, wa *model.PdfAnnotationWidget) (*core.PdfObjectDictionary, error) {
	_, isButton := field.GetContext().(*model.PdfFieldButton)
	if !isButton {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Could only handle button - ignoring")
		}
		return nil, nil
	}
	ap, ok := core.GetDict(wa.AP)
	if ok && a.OnlyIfMissing {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Already populated - ignoring")
		}
		return ap, nil
	}
	if form.DR == nil {
		form.DR = model.NewPdfPageResources()
	}
	switch btn := field.GetContext().(type) {
	case *model.PdfFieldButton:
		if btn.IsPush() {
			ap, err := imageButtonAppearance(btn, wa, a.Style())
			if err != nil {
				return nil, err
			}
			return ap, nil
		}
	}
	return nil, nil
}

// NewFormSubmitButtonField would create a submit button in specified page according to the parameter in `FormSubmitActionOptions`.
func NewFormSubmitButtonField(page *model.PdfPage, opt FormSubmitActionOptions) (*model.PdfFieldButton, error) {
	flags := int64(submitFlagExportFormat)
	if opt.IsExclusionList {
		flags |= submitFlagExclude
	}
	if opt.IncludeEmptyFields {
		flags |= submitFlagIncludeNoValueFields
	}
	if opt.SubmitAsPDF {
		flags |= submitFlagSubmitPDF
	}
	action := model.NewPdfActionSubmitForm()
	action.Flags = core.MakeInteger(flags)
	action.F = model.NewPdfFilespec()
	if opt.Fields != nil {
		action.Fields = opt.Fields
	}
	action.F.F = core.MakeString(opt.Url)
	action.F.FS = core.MakeName("URL")
	btn, err := newPushButton(page, opt.Rectangle, "btnSubmit", opt.Label, opt.LabelColor, opt.Font, opt.FontSize, action.ToPdfObject())
	if err != nil {
		return nil, err
	}
	return btn, nil
}
func newFlateEncoder() core.StreamEncoder { return core.NewFlateEncoder() }

// CreateCircleAnnotation creates a circle/ellipse annotation object with appearance stream that can be added to
// page PDF annotations.
func CreateCircleAnnotation(circDef CircleAnnotationDef) (*model.PdfAnnotation, error) {
	annot := model.NewPdfAnnotationCircle()
	if circDef.BorderEnabled {
		r, g, b := circDef.BorderColor.R(), circDef.BorderColor.G(), circDef.BorderColor.B()
		annot.C = core.MakeArrayFromFloats([]float64{r, g, b})
		bs := model.NewBorderStyle()
		bs.SetBorderWidth(circDef.BorderWidth)
		annot.BS = bs.ToPdfObject()
	}
	if circDef.FillEnabled {
		r, g, b := circDef.FillColor.R(), circDef.FillColor.G(), circDef.FillColor.B()
		annot.IC = core.MakeArrayFromFloats([]float64{r, g, b})
	} else {
		annot.IC = core.MakeArrayFromIntegers([]int{})
	}
	if circDef.Opacity < 1.0 {
		annot.CA = core.MakeFloat(circDef.Opacity)
	}
	ap, rect, err := circleAppearance(circDef)
	if err != nil {
		return nil, err
	}
	annot.AP = ap
	annot.Rect = core.MakeArrayFromFloats([]float64{rect.Llx, rect.Lly, rect.Urx, rect.Ury})
	return annot.PdfAnnotation, nil
}

const (
	submitFlagExclude              = 1
	submitFlagIncludeNoValueFields = 2
	submitFlagExportFormat         = 4
	submitFlagGetMethod            = 8
	submitFlagSubmitCoordinates    = 16
	submitFlagXFDF                 = 32
	submitFlagIncludeAppendSaves   = 64
	submitFlagIncludeAnnotations   = 128
	submitFlagSubmitPDF            = 256
	submitFlagCanonicalFormat      = 512
	submitFlagExclNonUserAnnots    = 1024
	submitFlagExclFKey             = 2048
	submitFlagReserved13           = 4096
)

// CreateFileAttachmentAnnotation creates a file attachment annotation object that can be added to the annotation list of a PDF page.
func CreateFileAttachmentAnnotation(fileDef FileAnnotationDef) (*model.PdfAnnotation, error) {
	fileSpec := model.NewPdfFileSpecFromEmbeddedFile(fileDef.EmbeddedFile)
	if fileDef.Color == nil {
		fileDef.Color = model.NewPdfColorDeviceRGB(0.0, 0.0, 0.0)
	}
	if fileDef.Description == "" {
		fileDef.Description = fileDef.EmbeddedFile.Name
	}
	if fileDef.CreationDate == nil {
		now := time.Now()
		fileDef.CreationDate = &now
	}
	if fileDef.IconName == "" {
		fileDef.IconName = "PushPin"
	}
	date, err := model.NewPdfDateFromTime(*fileDef.CreationDate)
	if err != nil {
		return nil, err
	}
	annot := model.NewPdfAnnotationFileAttachment()
	annot.FS = fileSpec.ToPdfObject()
	annot.C = core.MakeArrayFromFloats([]float64{fileDef.Color.R(), fileDef.Color.G(), fileDef.Color.B()})
	annot.Contents = core.MakeString(fileDef.Description)
	annot.CreationDate = date.ToPdfObject()
	annot.M = date.ToPdfObject()
	annot.Name = core.MakeName(fileDef.IconName)
	annot.Rect = core.MakeArrayFromFloats([]float64{fileDef.X, fileDef.Y, fileDef.X + fileDef.Width, fileDef.Y + fileDef.Height})
	annot.T = core.MakeString(fileDef.Author)
	annot.Subj = core.MakeString(fileDef.Subject)
	return annot.PdfAnnotation, nil
}

const (
	SignatureImageLeft SignatureImagePosition = iota
	SignatureImageRight
	SignatureImageTop
	SignatureImageBottom
)

// FileAnnotationDef holds base information for constructing an file attachment annotation.
type FileAnnotationDef struct {
	// Bounding box of the annotation.
	X      float64
	Y      float64
	Width  float64
	Height float64

	// EmbeddedFile is the file information to be attached.
	EmbeddedFile *model.EmbeddedFile

	// Author is the author of the attachment file.
	Author string

	// Subject is the subject of the attachment file.
	Subject string

	// Description of the file attachment that will be displayed as a comment on the PDF reader.
	Description string

	// IconName is The name of an icon that shall be used in displaying the annotation.
	// Conforming readers shall provide predefined icon appearances for at least the following standard names:
	//
	// - Graph
	// - PushPin
	// - Paperclip
	// - Tag
	//
	// Additional names may be supported as well. Default value: "PushPin".
	IconName string

	// Color is the color of the annotation.
	Color *model.PdfColorDeviceRGB

	// CreationDate is the date and time when the file attachment was created.
	// If not set, the current time is used.
	CreationDate *time.Time
}

func strokeCurves(curves [][]contentdraw.CubicBezierCurve, color *model.PdfColorDeviceRGB, lineWidth float64) ([]byte, *model.PdfRectangle, error) {
	cc := contentstream.NewContentCreator()
	cc.Add_q().SetStrokingColor(color).Add_w(lineWidth)
	path := contentdraw.NewCubicBezierPath()
	for _, curve := range curves {
		path.Curves = append(path.Curves, curve...)
		for i, c := range curve {
			if i == 0 {
				cc.Add_m(c.P0.X, c.P0.Y)
			} else {
				cc.Add_l(c.P0.X, c.P0.Y)
			}
			cc.Add_c(c.P1.X, c.P1.Y, c.P2.X, c.P2.Y, c.P3.X, c.P3.Y)
		}
	}
	cc.Add_S().Add_Q()
	return cc.Bytes(), path.GetBoundingBox().ToPdfRectangle(), nil
}

func findTfInAP(ap core.PdfObject, dr *model.PdfPageResources) (*core.PdfObjectName, float64, bool) {
	var (
		fontName *core.PdfObjectName
		fontSize float64
		found    bool
	)
	if apDict, ok := core.GetDict(ap); ok && apDict != nil {
		n := core.TraceToDirectObject(apDict.Get("N"))
		switch stm := n.(type) {
		case *core.PdfObjectStream:
			content, err := core.DecodeStream(stm)
			if err != nil {
				common.Log.Debug("ERROR unable decode content stream: %v", err.Error())
				return nil, 0, false
			}
			ops, err := contentstream.NewContentStreamParser(string(content)).Parse()
			if err != nil {
				common.Log.Debug("ERROR unable parse content stream: %v", err.Error())
				return nil, 0, false
			}
			proc := contentstream.NewContentStreamProcessor(*ops)
			proc.AddHandler(contentstream.HandlerConditionEnumOperand, "Tf", func(op *contentstream.ContentStreamOperation, gs contentstream.GraphicsState, res *model.PdfPageResources) error {
				if len(op.Params) == 2 {
					if nm, ok := core.GetName(op.Params[0]); ok {
						fontName = nm
					}
					if size, err := core.GetNumberAsFloat(op.Params[1]); err == nil {
						fontSize = size
					}
					found = true
					return contentstream.ErrEarlyExit
				}
				return nil
			})
			proc.Process(dr)
			return fontName, fontSize, found
		}
	}
	return nil, 0, false
}

// ComboboxFieldOptions defines optional parameters for a combobox form field.
type ComboboxFieldOptions struct {
	// Choices is the list of string values that can be selected.
	Choices []string
}

func imageButtonAppearance(btn *model.PdfFieldButton, widget *model.PdfAnnotationWidget, style AppearanceStyle) (*core.PdfObjectDictionary, error) {
	rectArr, hasRect := core.GetArray(widget.Rect)
	if !hasRect {
		return nil, errors.New("invalid Rect")
	}
	rect, err := model.NewPdfRectangle(*rectArr)
	if err != nil {
		return nil, err
	}
	w, h := rect.Width(), rect.Height()
	cc := contentstream.NewContentCreator()
	if style.BorderSize > 0 {
		drawBorderAndFill(cc, style, w, h)
	}
	if style.DrawAlignmentReticle {
		reticleStyle := style
		reticleStyle.BorderSize = 0.2
		drawAlignmentReticle(cc, reticleStyle, w, h)
	}
	img := btn.GetFillImage()
	frm, err := imageForm(w, h, img, style)
	if err != nil {
		return nil, err
	}
	mk, hasMK := core.GetDict(widget.MK)
	if hasMK {
		mk.Set("I", frm.ToPdfObject())
	}
	xobjs := core.MakeDict()
	xobjs.Set("FRM", frm.ToPdfObject())
	resources := model.NewPdfPageResources()
	resources.ProcSet = core.MakeArray(core.MakeName("PDF"))
	resources.XObject = xobjs
	innerW := w - 2
	innerH := h - 2
	cc.Add_q()
	cc.Add_re(1, 1, innerW, innerH)
	cc.Add_W()
	cc.Add_n()
	innerW -= 2
	innerH -= 2
	cc.Add_q()
	cc.Add_re(2, 2, innerW, innerH)
	cc.Add_W()
	cc.Add_n()
	scale := math.Min(innerW/float64(img.Width), innerH/float64(img.Height))
	cc.Add_cm(scale, 0, 0, scale, (w/2)-(float64(img.Width)*scale/2)+2, 2)
	cc.Add_Do("FRM")
	cc.Add_Q()
	cc.Add_Q()
	form := model.NewXObjectForm()
	form.FormType = core.MakeInteger(1)
	form.Resources = resources
	form.BBox = core.MakeArrayFromFloats([]float64{0, 0, w, h})
	form.Matrix = core.MakeArrayFromFloats([]float64{1.0, 0.0, 0.0, 1.0, 0.0, 0.0})
	form.SetContentStream(cc.Bytes(), newFlateEncoder())
	apDict := core.MakeDict()
	apDict.Set("N", form.ToPdfObject())
	return apDict, nil
}

func daWithColor(tf *model.PdfFieldText, color model.PdfColor) (*core.PdfObjectString, error) {
	ops := contentstream.ContentStreamOperations{}
	da := textFieldDA(tf.PdfField)
	parsed, err := contentstream.NewContentStreamParser(da).Parse()
	if err != nil {
		return nil, err
	}
	for _, op := range *parsed {
		if op.Operand == "g" || op.Operand == "rg" {
			continue
		}
		ops = append(ops, op)
	}
	rgb, ok := color.(*model.PdfColorDeviceRGB)
	if !ok {
		return nil, err
	}
	r, g, b := core.MakeFloat(rgb[0]), core.MakeFloat(rgb[1]), core.MakeFloat(rgb[2])
	rgOp := &contentstream.ContentStreamOperation{Params: []core.PdfObject{r, g, b}, Operand: "rg"}
	ops = append(ops, rgOp)
	s := ops.String()
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Trim(s, " ")
	return core.MakeHexString(s), nil
}

func lineAppearance(def LineAnnotationDef) (*core.PdfObjectDictionary, *model.PdfRectangle, error) {
	form := model.NewXObjectForm()
	form.Resources = model.NewPdfPageResources()
	gsName := ""
	if def.Opacity < 1.0 {
		gsDict := core.MakeDict()
		gsDict.Set("ca", core.MakeFloat(def.Opacity))
		err := form.Resources.AddExtGState("gs1", gsDict)
		if err != nil {
			common.Log.Debug("Unable to add extgstate gs1")
			return nil, nil, err
		}
		gsName = "gs1"
	}
	content, bbox, rect, err := lineAppearanceStream(def, gsName)
	if err != nil {
		return nil, nil, err
	}
	err = form.SetContentStream(content, nil)
	if err != nil {
		return nil, nil, err
	}
	form.BBox = bbox.ToPdfObject()
	apDict := core.MakeDict()
	apDict.Set("N", form.ToPdfObject())
	return apDict, rect, nil
}

// SetStyle applies appearance `style` to `fa`.
func (fa *FieldAppearance) SetStyle(style AppearanceStyle) { fa.style = &style }

func comboBoxAppearance(form *model.PdfAcroForm, widget *model.PdfAnnotationWidget, choice *model.PdfFieldChoice, style AppearanceStyle) (*core.PdfObjectDictionary, error) {
	rectArr, hasRect := core.GetArray(widget.Rect)
	if !hasRect {
		return nil, errors.New("invalid Rect")
	}
	rect, err := model.NewPdfRectangle(*rectArr)
	if err != nil {
		return nil, err
	}
	w, h := rect.Width(), rect.Height()
	common.Log.Debug("Choice, wa BS: %v", widget.BS)
	daOps, err := contentstream.NewContentStreamParser(textFieldDA(choice.PdfField)).Parse()
	if err != nil {
		return nil, err
	}
	mk, hasMK := core.GetDict(widget.MK)
	if hasMK {
		bs, _ := core.GetDict(widget.BS)
		err := style.applyAppearanceCharacteristics(mk, bs, nil)
		if err != nil {
			return nil, err
		}
	}
	states := core.MakeDict()
	for _, opt := range choice.Opt.Elements() {
		if pair, ok := core.GetArray(opt); ok && pair.Len() == 2 {
			opt = pair.Get(1)
		}
		var label string
		if str, ok := core.GetString(opt); ok {
			label = str.Decoded()
		} else if nm, ok := core.GetName(opt); ok {
			label = nm.String()
		} else {
			common.Log.Debug("ERROR: Opt not a name/string - %T", opt)
			return nil, errors.New("not a name/string")
		}
		if len(label) > 0 {
			xform, err := comboOptionAppearance(choice.PdfField, w, h, label, style, daOps, form.DR, mk)
			if err != nil {
				return nil, err
			}
			states.Set(*core.MakeName(label), xform.ToPdfObject())
		}
	}
	apDict := core.MakeDict()
	apDict.Set("N", states)
	return apDict, nil
}

func isClippedTo(ops *contentstream.ContentStreamOperations, x, y, w, h float64) bool {
	if ops == nil || len(*ops) < 5 {
		return false
	}
	head := (*ops)[:4]
	if head[0].Operand != "q" || head[1].Operand != "re" || (head[2].Operand != "W" && head[2].Operand != "W*") || head[3].Operand != "n" {
		return false
	}
	params, err := core.GetNumbersAsFloat(head[1].Params)
	if err != nil || len(params) != 4 {
		return false
	}
	const eps = 1e-3
	if math.Abs(params[0]-x) >= eps || math.Abs(params[1]-y) >= eps || math.Abs(params[2]-w) >= eps || math.Abs(params[3]-h) >= eps {
		return false
	}
	depth := 0
	for i, op := range *ops {
		switch op.Operand {
		case "q":
			depth++
		case "Q":
			depth--
		}
		if depth <= 0 && i < len(*ops)-1 {
			return false
		}
	}
	return depth == 0
}

func (s *AppearanceStyle) applyAppearanceCharacteristics(mk *core.PdfObjectDictionary, bs *core.PdfObjectDictionary, font *model.PdfFont) error {
	if !s.AllowMK {
		return nil
	}
	if CA, ok := core.GetString(mk.Get("CA")); ok && font != nil {
		caBytes := CA.Bytes()
		if len(caBytes) != 0 {
			runes := font.CharcodesToUnicode(font.BytesToCharcodes(caBytes))
			if len(runes) == 1 && runes[0] != textencoding.MissingCodeRune {
				s.CheckmarkRune = runes[0]
			}
		}
	}
	if BC, ok := core.GetArray(mk.Get("BC")); ok {
		comps, err := BC.ToFloat64Array()
		if err != nil {
			return err
		}
		switch len(comps) {
		case 1:
			s.BorderColor = model.NewPdfColorDeviceGray(comps[0])
		case 3:
			s.BorderColor = model.NewPdfColorDeviceRGB(comps[0], comps[1], comps[2])
		case 4:
			s.BorderColor = model.NewPdfColorDeviceCMYK(comps[0], comps[1], comps[2], comps[3])
		default:
			common.Log.Debug("ERROR: BC - Invalid number of color components (%d)", len(comps))
		}
		if bs != nil {
			if w, err := core.GetNumberAsFloat(bs.Get("W")); err == nil {
				s.BorderSize = w
			}
		}
	}
	if BG, ok := core.GetArray(mk.Get("BG")); ok {
		comps, err := BG.ToFloat64Array()
		if err != nil {
			return err
		}
		switch len(comps) {
		case 1:
			s.FillColor = model.NewPdfColorDeviceGray(comps[0])
		case 3:
			s.FillColor = model.NewPdfColorDeviceRGB(comps[0], comps[1], comps[2])
		case 4:
			s.FillColor = model.NewPdfColorDeviceCMYK(comps[0], comps[1], comps[2], comps[3])
		default:
			common.Log.Debug("ERROR: BG - Invalid number of color components (%d)", len(comps))
		}
	}
	return nil
}
func defaultHighlightColor() model.PdfColor { return model.NewPdfColorDeviceRGB(0.6, 0.757, 0.855) }

// FieldAppearance implements interface model.FieldAppearanceGenerator and generates appearance streams
// for fields taking into account what value is in the field. A common use case is for generating the
// appearance stream prior to flattening fields.
//
// If `OnlyIfMissing` is true, the field appearance is generated only for fields that do not have an
// appearance stream specified.
// If `RegenerateTextFields` is true, all text fields are regenerated (even if OnlyIfMissing is true).
type FieldAppearance struct {
	OnlyIfMissing        bool
	RegenerateTextFields bool
	style                *AppearanceStyle
}

// CreateRectangleAnnotation creates a rectangle annotation object that can be added to page PDF annotations.
func CreateRectangleAnnotation(rectDef RectangleAnnotationDef) (*model.PdfAnnotation, error) {
	annot := model.NewPdfAnnotationSquare()
	if rectDef.BorderEnabled {
		r, g, b := rectDef.BorderColor.R(), rectDef.BorderColor.G(), rectDef.BorderColor.B()
		annot.C = core.MakeArrayFromFloats([]float64{r, g, b})
		bs := model.NewBorderStyle()
		bs.SetBorderWidth(rectDef.BorderWidth)
		annot.BS = bs.ToPdfObject()
	}
	if rectDef.FillEnabled {
		r, g, b := rectDef.FillColor.R(), rectDef.FillColor.G(), rectDef.FillColor.B()
		annot.IC = core.MakeArrayFromFloats([]float64{r, g, b})
	} else {
		annot.IC = core.MakeArrayFromIntegers([]int{})
	}
	if rectDef.Opacity < 1.0 {
		annot.CA = core.MakeFloat(rectDef.Opacity)
	}
	ap, rect, err := rectangleAppearance(rectDef)
	if err != nil {
		return nil, err
	}
	annot.AP = ap
	annot.Rect = core.MakeArrayFromFloats([]float64{rect.Llx, rect.Lly, rect.Urx, rect.Ury})
	return annot.PdfAnnotation, nil
}
