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
	"math"
	"strings"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

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
