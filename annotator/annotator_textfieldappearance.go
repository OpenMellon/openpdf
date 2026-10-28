// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package annotator provides an interface for creating annotations with appearance
// streams.  It goes beyond the models package which includes definitions of basic annotation models, in that it
// can create the appearance streams which specify the exact appearance as needed by many pdf viewers for consistent
// appearance of the annotations.
// It also contains methods for generating appearance streams for fields via widget annotations.
package annotator

import (
	"errors"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model"
)

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
