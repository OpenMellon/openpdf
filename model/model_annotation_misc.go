package model

import (
	_ "image/gif"

	"github.com/OpenMellon/openpdf/core"
)

// PdfAnnotation represents an annotation in PDF (section 12.5 p. 389).
type PdfAnnotation struct {
	context      PdfModel
	Rect         core.PdfObject
	Contents     core.PdfObject
	P            core.PdfObject
	NM           core.PdfObject
	M            core.PdfObject
	F            core.PdfObject
	AP           core.PdfObject
	AS           core.PdfObject
	Border       core.PdfObject
	C            core.PdfObject
	StructParent core.PdfObject
	OC           core.PdfObject
	primitive    *core.PdfIndirectObject
}

// NewPdfAnnotationCaret returns a new caret annotation.
func NewPdfAnnotationCaret() *PdfAnnotationCaret {
	annot := NewPdfAnnotation()
	caret := &PdfAnnotationCaret{}
	caret.PdfAnnotation = annot
	caret.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(caret)
	return caret
}

// GetAction returns the PDF action for the annotation link.
func (a *PdfAnnotationLink) GetAction() (*PdfAction, error) {
	if a.action != nil {
		return a.action, nil
	}
	if a.A == nil {
		return nil, nil
	}
	if a.reader == nil {
		return nil, nil
	}
	action, err := a.reader.loadAction(a.A)
	if err != nil {
		return nil, err
	}
	a.action = action
	return a.action, nil
}

// NewPdfAnnotationInk returns a new ink annotation.
func NewPdfAnnotationInk() *PdfAnnotationInk {
	annot := NewPdfAnnotation()
	ink := &PdfAnnotationInk{}
	ink.PdfAnnotation = annot
	ink.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(ink)
	return ink
}

// NewPdfAnnotationFreeText returns a new free text annotation.
func NewPdfAnnotationFreeText() *PdfAnnotationFreeText {
	annot := NewPdfAnnotation()
	freeText := &PdfAnnotationFreeText{}
	freeText.PdfAnnotation = annot
	freeText.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(freeText)
	return freeText
}

// SetAction sets the PDF action for the annotation link.
func (a *PdfAnnotationLink) SetAction(action *PdfAction) {
	a.action = action
	if action == nil {
		a.A = nil
	}
}

// PdfAnnotationInk represents Ink annotations.
// (Section 12.5.6.13).
type PdfAnnotationInk struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	InkList core.PdfObject
	BS      core.PdfObject
}

func (a *PdfAnnotationMarkup) appendToPdfDictionary(dict *core.PdfObjectDictionary) {
	dict.SetIfNotNil("T", a.T)
	if a.Popup != nil {
		dict.Set("Popup", a.Popup.ToPdfObject())
	}
	dict.SetIfNotNil("CA", a.CA)
	dict.SetIfNotNil("RC", a.RC)
	dict.SetIfNotNil("CreationDate", a.CreationDate)
	dict.SetIfNotNil("IRT", a.IRT)
	dict.SetIfNotNil("Subj", a.Subj)
	dict.SetIfNotNil("RT", a.RT)
	dict.SetIfNotNil("IT", a.IT)
	dict.SetIfNotNil("ExData", a.ExData)
}

// GetContext returns the annotation context which contains the specific type-dependent context.
// The context represents the subannotation.
func (a *PdfAnnotation) GetContext() PdfModel {
	if a == nil {
		return nil
	}
	return a.context
}

// PdfAnnotationCaret represents Caret annotations.
// (Section 12.5.6.11).
type PdfAnnotationCaret struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	RD core.PdfObject
	Sy core.PdfObject
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationLink) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("Link"))
	if a.action != nil && a.action.context != nil {
		dict.Set("A", a.action.context.ToPdfObject())
	} else if a.A != nil {
		dict.Set("A", a.A)
	}
	dict.SetIfNotNil("Dest", a.Dest)
	dict.SetIfNotNil("H", a.H)
	dict.SetIfNotNil("PA", a.PA)
	dict.SetIfNotNil("QuadPoints", a.QuadPoints)
	dict.SetIfNotNil("BS", a.BS)
	return indirect
}

// GetContainingPdfObject implements interface PdfModel.
func (a *PdfAnnotation) GetContainingPdfObject() core.PdfObject { return a.primitive }

// PdfAnnotationMovie represents Movie annotations.
// (Section 12.5.6.17).
type PdfAnnotationMovie struct {
	*PdfAnnotation
	T     core.PdfObject
	Movie core.PdfObject
	A     core.PdfObject
}

// NewPdfAnnotation returns an initialized generic PDF annotation model.
func NewPdfAnnotation() *PdfAnnotation {
	annot := &PdfAnnotation{}
	annot.primitive = core.MakeIndirectObject(core.MakeDict())
	return annot
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationHighlight) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Highlight"))
	dict.SetIfNotNil("QuadPoints", a.QuadPoints)
	return indirect
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationCaret) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Caret"))
	dict.SetIfNotNil("RD", a.RD)
	dict.SetIfNotNil("Sy", a.Sy)
	return indirect
}

// PdfAnnotationFreeText represents FreeText annotations.
// (Section 12.5.6.6).
type PdfAnnotationFreeText struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	DA core.PdfObject
	Q  core.PdfObject
	RC core.PdfObject
	DS core.PdfObject
	CL core.PdfObject
	IT core.PdfObject
	BE core.PdfObject
	RD core.PdfObject
	BS core.PdfObject
	LE core.PdfObject
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationFileAttachment) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("FileAttachment"))
	dict.SetIfNotNil("FS", a.FS)
	dict.SetIfNotNil("Name", a.Name)
	return indirect
}

// ToPdfObject implements interface PdfModel.
// Note: Call the sub-annotation's ToPdfObject to set both the generic and non-generic information.
func (a *PdfAnnotation) ToPdfObject() core.PdfObject {
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.Clear()
	dict.Set("Type", core.MakeName("Annot"))
	dict.SetIfNotNil("Rect", a.Rect)
	dict.SetIfNotNil("Contents", a.Contents)
	dict.SetIfNotNil("P", a.P)
	dict.SetIfNotNil("NM", a.NM)
	dict.SetIfNotNil("M", a.M)
	dict.SetIfNotNil("F", a.F)
	dict.SetIfNotNil("AP", a.AP)
	dict.SetIfNotNil("AS", a.AS)
	dict.SetIfNotNil("Border", a.Border)
	dict.SetIfNotNil("C", a.C)
	dict.SetIfNotNil("StructParent", a.StructParent)
	dict.SetIfNotNil("OC", a.OC)
	return indirect
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationLine) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Line"))
	dict.SetIfNotNil("L", a.L)
	dict.SetIfNotNil("BS", a.BS)
	dict.SetIfNotNil("LE", a.LE)
	dict.SetIfNotNil("IC", a.IC)
	dict.SetIfNotNil("LL", a.LL)
	dict.SetIfNotNil("LLE", a.LLE)
	dict.SetIfNotNil("Cap", a.Cap)
	dict.SetIfNotNil("IT", a.IT)
	dict.SetIfNotNil("LLO", a.LLO)
	dict.SetIfNotNil("CP", a.CP)
	dict.SetIfNotNil("Measure", a.Measure)
	dict.SetIfNotNil("CO", a.CO)
	return indirect
}

// NewPdfAnnotationCircle returns a new circle annotation.
func NewPdfAnnotationCircle() *PdfAnnotationCircle {
	annot := NewPdfAnnotation()
	circle := &PdfAnnotationCircle{}
	circle.PdfAnnotation = annot
	circle.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(circle)
	return circle
}

// NewPdfAnnotationLine returns a new line annotation.
func NewPdfAnnotationLine() *PdfAnnotationLine {
	annot := NewPdfAnnotation()
	line := &PdfAnnotationLine{}
	line.PdfAnnotation = annot
	line.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(line)
	return line
}

// GenerateKDict generates K Dictionary based on existing Annotation type.
func (a *PdfAnnotation) GenerateKDict() *KDict {
	if a == nil {
		return nil
	}
	kDictionary := NewKDictionary()
	kDictionary.S = core.MakeName(string(StructureTypeAnnot))
	kDictionary.AddRefChild(a.primitive)
	return kDictionary
}

// NewPdfAnnotationFileAttachment returns a new file attachment annotation.
func NewPdfAnnotationFileAttachment() *PdfAnnotationFileAttachment {
	annot := NewPdfAnnotation()
	fileAttachment := &PdfAnnotationFileAttachment{}
	fileAttachment.PdfAnnotation = annot
	fileAttachment.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(fileAttachment)
	return fileAttachment
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationInk) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Ink"))
	dict.SetIfNotNil("InkList", a.InkList)
	dict.SetIfNotNil("BS", a.BS)
	return indirect
}

// NewPdfAnnotationLink returns a new link annotation.
func NewPdfAnnotationLink() *PdfAnnotationLink {
	annot := NewPdfAnnotation()
	link := &PdfAnnotationLink{}
	link.PdfAnnotation = annot
	annot.SetContext(link)
	return link
}

// PdfAnnotationLine represents Line annotations.
// (Section 12.5.6.7).
type PdfAnnotationLine struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	L       core.PdfObject
	BS      core.PdfObject
	LE      core.PdfObject
	IC      core.PdfObject
	LL      core.PdfObject
	LLE     core.PdfObject
	Cap     core.PdfObject
	IT      core.PdfObject
	LLO     core.PdfObject
	CP      core.PdfObject
	Measure core.PdfObject
	CO      core.PdfObject
}

// PdfAnnotationHighlight represents Highlight annotations.
// (Section 12.5.6.10).
type PdfAnnotationHighlight struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	QuadPoints core.PdfObject
}

// NewPdfAnnotationMovie returns a new movie annotation.
func NewPdfAnnotationMovie() *PdfAnnotationMovie {
	annot := NewPdfAnnotation()
	movie := &PdfAnnotationMovie{}
	movie.PdfAnnotation = annot
	annot.SetContext(movie)
	return movie
}

// PdfAnnotationLink represents Link annotations.
// (Section 12.5.6.5 p. 403).
type PdfAnnotationLink struct {
	*PdfAnnotation
	A          core.PdfObject
	Dest       core.PdfObject
	H          core.PdfObject
	PA         core.PdfObject
	QuadPoints core.PdfObject
	BS         core.PdfObject
	action     *PdfAction
	reader     *PdfReader
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationCircle) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Circle"))
	dict.SetIfNotNil("BS", a.BS)
	dict.SetIfNotNil("IC", a.IC)
	dict.SetIfNotNil("BE", a.BE)
	dict.SetIfNotNil("RD", a.RD)
	return indirect
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationMovie) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("Movie"))
	dict.SetIfNotNil("T", a.T)
	dict.SetIfNotNil("Movie", a.Movie)
	dict.SetIfNotNil("A", a.A)
	return indirect
}

// CopyFrom copies all the fields from another PdfAnnotation
func (a *PdfAnnotation) CopyFrom(another *PdfAnnotation) {
	if another == nil {
		return
	}
	a.Rect = another.Rect
	a.Contents = another.Contents
	a.P = another.P
	a.NM = another.NM
	a.M = another.M
	a.F = another.F
	a.AP = another.AP
	a.AS = another.AS
	a.Border = another.Border
	a.C = another.C
	a.StructParent = another.StructParent
	a.OC = another.OC
}

// PdfAnnotationCircle represents Circle annotations.
// (Section 12.5.6.8).
type PdfAnnotationCircle struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	BS core.PdfObject
	IC core.PdfObject
	BE core.PdfObject
	RD core.PdfObject
}

// NewPdfAnnotationHighlight returns a new text highlight annotation.
func NewPdfAnnotationHighlight() *PdfAnnotationHighlight {
	annot := NewPdfAnnotation()
	highlight := &PdfAnnotationHighlight{}
	highlight.PdfAnnotation = annot
	highlight.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(highlight)
	return highlight
}

// PdfAnnotationFileAttachment represents FileAttachment annotations.
// (Section 12.5.6.15).
type PdfAnnotationFileAttachment struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	FS   core.PdfObject
	Name core.PdfObject
}

// SetContext sets the sub annotation (context).
func (a *PdfAnnotation) SetContext(ctx PdfModel) { a.context = ctx }

// PdfAnnotationMarkup represents additional fields for mark-up annotations.
// (Section 12.5.6.2 p. 399).
type PdfAnnotationMarkup struct {
	T            core.PdfObject
	Popup        *PdfAnnotationPopup
	CA           core.PdfObject
	RC           core.PdfObject
	CreationDate core.PdfObject
	IRT          core.PdfObject
	Subj         core.PdfObject
	RT           core.PdfObject
	IT           core.PdfObject
	ExData       core.PdfObject
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationFreeText) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("FreeText"))
	dict.SetIfNotNil("DA", a.DA)
	dict.SetIfNotNil("Q", a.Q)
	dict.SetIfNotNil("RC", a.RC)
	dict.SetIfNotNil("DS", a.DS)
	dict.SetIfNotNil("CL", a.CL)
	dict.SetIfNotNil("IT", a.IT)
	dict.SetIfNotNil("BE", a.BE)
	dict.SetIfNotNil("RD", a.RD)
	dict.SetIfNotNil("BS", a.BS)
	dict.SetIfNotNil("LE", a.LE)
	return indirect
}
