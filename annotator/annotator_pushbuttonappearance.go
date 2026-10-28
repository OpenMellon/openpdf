// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package annotator provides an interface for creating annotations with appearance
// streams.  It goes beyond the models package which includes definitions of basic annotation models, in that it
// can create the appearance streams which specify the exact appearance as needed by many pdf viewers for consistent
// appearance of the annotations.
// It also contains methods for generating appearance streams for fields via widget annotations.
package annotator

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/internal/timeutils"
	"github.com/OpenMellon/openpdf/model"
)

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
