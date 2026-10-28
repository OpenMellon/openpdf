package model

import (
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/core"
)

// NewPdfAnnotationRedact returns a new redact annotation.
func NewPdfAnnotationRedact() *PdfAnnotationRedact {
	annot := NewPdfAnnotation()
	redact := &PdfAnnotationRedact{}
	redact.PdfAnnotation = annot
	redact.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(redact)
	return redact
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationPrinterMark) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("PrinterMark"))
	dict.SetIfNotNil("MN", a.MN)
	return indirect
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationProjection) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	return indirect
}

func (a *PdfAnnotation) String() string {
	s := ""
	indirect, ok := a.ToPdfObject().(*core.PdfIndirectObject)
	if ok {
		s = fmt.Sprintf("%T: %s", a.context, indirect.PdfObject.String())
	}
	return s
}

// PdfAnnotationSquare represents Square annotations.
// (Section 12.5.6.8).
type PdfAnnotationSquare struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	BS core.PdfObject
	IC core.PdfObject
	BE core.PdfObject
	RD core.PdfObject
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationStamp) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Stamp"))
	dict.SetIfNotNil("Name", a.Name)
	return indirect
}

// NewPdfAnnotationSquiggly returns a new text squiggly annotation.
func NewPdfAnnotationSquiggly() *PdfAnnotationSquiggly {
	annot := NewPdfAnnotation()
	squiggly := &PdfAnnotationSquiggly{}
	squiggly.PdfAnnotation = annot
	squiggly.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(squiggly)
	return squiggly
}

// NewPdfAnnotationPolygon returns a new polygon annotation.
func NewPdfAnnotationPolygon() *PdfAnnotationPolygon {
	annot := NewPdfAnnotation()
	polygon := &PdfAnnotationPolygon{}
	polygon.PdfAnnotation = annot
	polygon.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(polygon)
	return polygon
}

// PdfAnnotationPolyLine represents PolyLine annotations.
// (Section 12.5.6.9).
type PdfAnnotationPolyLine struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	Vertices core.PdfObject
	LE       core.PdfObject
	BS       core.PdfObject
	IC       core.PdfObject
	BE       core.PdfObject
	IT       core.PdfObject
	Measure  core.PdfObject
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationScreen) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("Screen"))
	dict.SetIfNotNil("T", a.T)
	dict.SetIfNotNil("MK", a.MK)
	dict.SetIfNotNil("A", a.A)
	dict.SetIfNotNil("AA", a.AA)
	return indirect
}

// NewPdfAnnotationSound returns a new sound annotation.
func NewPdfAnnotationSound() *PdfAnnotationSound {
	annot := NewPdfAnnotation()
	sound := &PdfAnnotationSound{}
	sound.PdfAnnotation = annot
	sound.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(sound)
	return sound
}

// PdfAnnotationPopup represents Popup annotations.
// (Section 12.5.6.14).
type PdfAnnotationPopup struct {
	*PdfAnnotation
	Parent core.PdfObject
	Open   core.PdfObject
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationPolyLine) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("PolyLine"))
	dict.SetIfNotNil("Vertices", a.Vertices)
	dict.SetIfNotNil("LE", a.LE)
	dict.SetIfNotNil("BS", a.BS)
	dict.SetIfNotNil("IC", a.IC)
	dict.SetIfNotNil("BE", a.BE)
	dict.SetIfNotNil("IT", a.IT)
	dict.SetIfNotNil("Measure", a.Measure)
	return indirect
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationSquare) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	if a.PdfAnnotationMarkup != nil {
		a.appendToPdfDictionary(dict)
	}
	dict.SetIfNotNil("Subtype", core.MakeName("Square"))
	dict.SetIfNotNil("BS", a.BS)
	dict.SetIfNotNil("IC", a.IC)
	dict.SetIfNotNil("BE", a.BE)
	dict.SetIfNotNil("RD", a.RD)
	return indirect
}

// NewPdfAnnotationPrinterMark returns a new printermark annotation.
func NewPdfAnnotationPrinterMark() *PdfAnnotationPrinterMark {
	annot := NewPdfAnnotation()
	printerMark := &PdfAnnotationPrinterMark{}
	printerMark.PdfAnnotation = annot
	annot.SetContext(printerMark)
	return printerMark
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationSound) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Sound"))
	dict.SetIfNotNil("Sound", a.Sound)
	dict.SetIfNotNil("Name", a.Name)
	return indirect
}

// PdfAnnotationStamp represents Stamp annotations.
// (Section 12.5.6.12).
type PdfAnnotationStamp struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	Name core.PdfObject
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationText) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	if a.PdfAnnotationMarkup != nil {
		a.appendToPdfDictionary(dict)
	}
	dict.SetIfNotNil("Subtype", core.MakeName("Text"))
	dict.SetIfNotNil("Open", a.Open)
	dict.SetIfNotNil("Name", a.Name)
	dict.SetIfNotNil("State", a.State)
	dict.SetIfNotNil("StateModel", a.StateModel)
	return indirect
}

// NewPdfAnnotationSquare returns a new square annotation.
func NewPdfAnnotationSquare() *PdfAnnotationSquare {
	annot := NewPdfAnnotation()
	square := &PdfAnnotationSquare{}
	square.PdfAnnotation = annot
	square.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(square)
	return square
}

// NewPdfAnnotationRichMedia returns a new rich media annotation.
func NewPdfAnnotationRichMedia() *PdfAnnotationRichMedia {
	annot := NewPdfAnnotation()
	richMedia := &PdfAnnotationRichMedia{}
	richMedia.PdfAnnotation = annot
	annot.SetContext(richMedia)
	return richMedia
}

// NewPdfAnnotationPolyLine returns a new polyline annotation.
func NewPdfAnnotationPolyLine() *PdfAnnotationPolyLine {
	annot := NewPdfAnnotation()
	polyLine := &PdfAnnotationPolyLine{}
	polyLine.PdfAnnotation = annot
	polyLine.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(polyLine)
	return polyLine
}

// PdfAnnotationRedact represents Redact annotations.
// (Section 12.5.6.23).
type PdfAnnotationRedact struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	QuadPoints  core.PdfObject
	IC          core.PdfObject
	RO          core.PdfObject
	OverlayText core.PdfObject
	Repeat      core.PdfObject
	DA          core.PdfObject
	Q           core.PdfObject
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationPolygon) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Polygon"))
	dict.SetIfNotNil("Vertices", a.Vertices)
	dict.SetIfNotNil("LE", a.LE)
	dict.SetIfNotNil("BS", a.BS)
	dict.SetIfNotNil("IC", a.IC)
	dict.SetIfNotNil("BE", a.BE)
	dict.SetIfNotNil("IT", a.IT)
	dict.SetIfNotNil("Measure", a.Measure)
	return indirect
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationRedact) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Redact"))
	dict.SetIfNotNil("QuadPoints", a.QuadPoints)
	dict.SetIfNotNil("IC", a.IC)
	dict.SetIfNotNil("RO", a.RO)
	dict.SetIfNotNil("OverlayText", a.OverlayText)
	dict.SetIfNotNil("Repeat", a.Repeat)
	dict.SetIfNotNil("DA", a.DA)
	dict.SetIfNotNil("Q", a.Q)
	return indirect
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationPopup) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("Popup"))
	dict.SetIfNotNil("Parent", a.Parent)
	dict.SetIfNotNil("Open", a.Open)
	return indirect
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationSquiggly) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Squiggly"))
	dict.SetIfNotNil("QuadPoints", a.QuadPoints)
	return indirect
}

// PdfAnnotationSquiggly represents Squiggly annotations.
// (Section 12.5.6.10).
type PdfAnnotationSquiggly struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	QuadPoints core.PdfObject
}

// NewPdfAnnotationScreen returns a new screen annotation.
func NewPdfAnnotationScreen() *PdfAnnotationScreen {
	annot := NewPdfAnnotation()
	screen := &PdfAnnotationScreen{}
	screen.PdfAnnotation = annot
	annot.SetContext(screen)
	return screen
}

// PdfAnnotationSound represents Sound annotations.
// (Section 12.5.6.16).
type PdfAnnotationSound struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	Sound core.PdfObject
	Name  core.PdfObject
}

// PdfAnnotationText represents Text annotations.
// (Section 12.5.6.4 p. 402).
type PdfAnnotationText struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	Open       core.PdfObject
	Name       core.PdfObject
	State      core.PdfObject
	StateModel core.PdfObject
}

// NewPdfAnnotationText returns a new text annotation.
func NewPdfAnnotationText() *PdfAnnotationText {
	annot := NewPdfAnnotation()
	text := &PdfAnnotationText{}
	text.PdfAnnotation = annot
	text.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(text)
	return text
}

// PdfAnnotationPolygon represents Polygon annotations.
// (Section 12.5.6.9).
type PdfAnnotationPolygon struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	Vertices core.PdfObject
	LE       core.PdfObject
	BS       core.PdfObject
	IC       core.PdfObject
	BE       core.PdfObject
	IT       core.PdfObject
	Measure  core.PdfObject
}

// NewPdfAnnotationStamp returns a new stamp annotation.
func NewPdfAnnotationStamp() *PdfAnnotationStamp {
	annot := NewPdfAnnotation()
	stamp := &PdfAnnotationStamp{}
	stamp.PdfAnnotation = annot
	stamp.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(stamp)
	return stamp
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationStrikeOut) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("StrikeOut"))
	dict.SetIfNotNil("QuadPoints", a.QuadPoints)
	return indirect
}

// PdfAnnotationStrikeOut represents StrikeOut annotations.
// (Section 12.5.6.10).
type PdfAnnotationStrikeOut struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	QuadPoints core.PdfObject
}

// PdfAnnotationProjection represents Projection annotations.
type PdfAnnotationProjection struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
}

// PdfAnnotationRichMedia represents Rich Media annotations.
type PdfAnnotationRichMedia struct {
	*PdfAnnotation
	RichMediaSettings core.PdfObject
	RichMediaContent  core.PdfObject
}

// PdfAnnotationScreen represents Screen annotations.
// (Section 12.5.6.18).
type PdfAnnotationScreen struct {
	*PdfAnnotation
	T  core.PdfObject
	MK core.PdfObject
	A  core.PdfObject
	AA core.PdfObject
}

// PdfAnnotationPrinterMark represents PrinterMark annotations.
// (Section 12.5.6.20).
type PdfAnnotationPrinterMark struct {
	*PdfAnnotation
	MN core.PdfObject
}

// NewPdfAnnotationStrikeOut returns a new text strikeout annotation.
func NewPdfAnnotationStrikeOut() *PdfAnnotationStrikeOut {
	annot := NewPdfAnnotation()
	strikeOut := &PdfAnnotationStrikeOut{}
	strikeOut.PdfAnnotation = annot
	strikeOut.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(strikeOut)
	return strikeOut
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationRichMedia) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("RichMedia"))
	dict.SetIfNotNil("RichMediaSettings", a.RichMediaSettings)
	dict.SetIfNotNil("RichMediaContent", a.RichMediaContent)
	return indirect
}

// NewPdfAnnotationProjection returns a new projection annotation.
func NewPdfAnnotationProjection() *PdfAnnotationProjection {
	annot := NewPdfAnnotation()
	projection := &PdfAnnotationProjection{}
	projection.PdfAnnotation = annot
	projection.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(projection)
	return projection
}

// NewPdfAnnotationPopup returns a new popup annotation.
func NewPdfAnnotationPopup() *PdfAnnotationPopup {
	annot := NewPdfAnnotation()
	popup := &PdfAnnotationPopup{}
	popup.PdfAnnotation = annot
	annot.SetContext(popup)
	return popup
}
