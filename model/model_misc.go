package model

import (
	"bytes"
	"crypto/x509"
	"errors"
	"fmt"
	_ "image/gif"
	"math"
	"slices"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

func adjustHintOffset(offset, n, count int64) (uint32, error) {
	num := offset
	if offset >= n {
		num = offset - count
	}
	if num < 0 || num > math.MaxUint32 {
		return 0, fmt.Errorf("hint table offset %d cannot be encoded in 32 bits (file exceeds 4 GB)", num)
	}
	return uint32(num), nil
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationTrapNet) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("TrapNet"))
	return indirect
}

// PdfAnnotationTrapNet represents TrapNet annotations.
// (Section 12.5.6.21).
type PdfAnnotationTrapNet struct{ *PdfAnnotation }

// ToPdfObject implements interface PdfModel.
func (a *PdfActionURI) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeURI)))
	dict.SetIfNotNil("URI", a.URI)
	dict.SetIfNotNil("IsMap", a.IsMap)
	return indirect
}

// Field returns the parent form field of the widget annotation, if one exists.
// NOTE: the method returns nil if the parent form field has not been parsed.
func (a *PdfAnnotationWidget) Field() *PdfField { return a.field }

func hasAcceptableKeyUsage(cert *x509.Certificate) bool {
	if len(cert.ExtKeyUsage) == 0 && len(cert.UnknownExtKeyUsage) == 0 {
		return true
	}
	for _, usage := range []x509.ExtKeyUsage{x509.ExtKeyUsageAny, x509.ExtKeyUsageEmailProtection, x509.ExtKeyUsageCodeSigning} {
		if hasExtKeyUsage(cert, usage) {
			return true
		}
	}
	for _, unknownExtKeyUsage := range cert.UnknownExtKeyUsage {
		if slices.ContainsFunc(documentSigningEKUOIDs, unknownExtKeyUsage.Equal) {
			return true
		}
	}
	return false
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationWidget) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	if a.building {
		return indirect
	}
	a.building = true
	dict.SetIfNotNil("Subtype", core.MakeName("Widget"))
	dict.SetIfNotNil("H", a.H)
	dict.SetIfNotNil("MK", a.MK)
	dict.SetIfNotNil("A", a.A)
	dict.SetIfNotNil("AA", a.AA)
	dict.SetIfNotNil("BS", a.BS)
	parent := a.Parent
	if a.field != nil {
		if a.field.primitive == a.primitive {
			a.field.ToPdfObject()
		}
		parent = a.field.GetContainingPdfObject()
	}
	if parent != indirect {
		dict.SetIfNotNil("Parent", parent)
	}
	a.building = false
	return indirect
}

// PdfAnnotationWidget represents Widget annotations.
// Note: Widget annotations are used to display form fields.
// (Section 12.5.6.19).
type PdfAnnotationWidget struct {
	*PdfAnnotation
	H        core.PdfObject
	MK       core.PdfObject
	A        core.PdfObject
	AA       core.PdfObject
	BS       core.PdfObject
	Parent   core.PdfObject
	field    *PdfField
	building bool
}

// PdfActionThread represents a thread action.
type PdfActionThread struct {
	*PdfAction
	F *PdfFilespec
	D core.PdfObject
	B core.PdfObject
}

// PdfActionURI represents an URI action.
type PdfActionURI struct {
	*PdfAction
	URI   core.PdfObject
	IsMap core.PdfObject
}

// PdfActionTrans represents a trans action.
type PdfActionTrans struct {
	*PdfAction
	Trans core.PdfObject
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotation3D) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("3D"))
	dict.SetIfNotNil("3DD", a.T3DD)
	dict.SetIfNotNil("3DV", a.T3DV)
	dict.SetIfNotNil("3DA", a.T3DA)
	dict.SetIfNotNil("3DI", a.T3DI)
	dict.SetIfNotNil("3DB", a.T3DB)
	return indirect
}

// NewPdfActionThread returns a new "thread" action.
func NewPdfActionThread() *PdfActionThread {
	action := NewPdfAction()
	threadAction := &PdfActionThread{}
	threadAction.PdfAction = action
	action.SetContext(threadAction)
	return threadAction
}

func setAnnotationsAppearanceState(field *PdfField, obj core.PdfObject) {
	for _, annotation := range field.Annotations {
		annotation.AS = obj
		annotation.ToPdfObject()
	}
}

// PdfAnnotationWatermark represents Watermark annotations.
// (Section 12.5.6.22).
type PdfAnnotationWatermark struct {
	*PdfAnnotation
	FixedPrint core.PdfObject
}

// NewPdfAnnotation3D returns a new 3d annotation.
func NewPdfAnnotation3D() *PdfAnnotation3D {
	annot := NewPdfAnnotation()
	annot3D := &PdfAnnotation3D{}
	annot3D.PdfAnnotation = annot
	annot.SetContext(annot3D)
	return annot3D
}

// GetAlphabet returns a map of the runes in `text` and their frequencies.
func GetAlphabet(text string) map[rune]int {
	table := map[rune]int{}
	for _, n := range text {
		table[n]++
	}
	return table
}

// NewPdfActionTrans returns a new "trans" action.
func NewPdfActionTrans() *PdfActionTrans {
	action := NewPdfAction()
	transAction := &PdfActionTrans{}
	transAction.PdfAction = action
	action.SetContext(transAction)
	return transAction
}

// PdfAnnotationUnderline represents Underline annotations.
// (Section 12.5.6.10).
type PdfAnnotationUnderline struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	QuadPoints core.PdfObject
}

// NewPdfAnnotationWatermark returns a new watermark annotation.
func NewPdfAnnotationWatermark() *PdfAnnotationWatermark {
	annot := NewPdfAnnotation()
	watermark := &PdfAnnotationWatermark{}
	watermark.PdfAnnotation = annot
	annot.SetContext(watermark)
	return watermark
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationWatermark) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("Watermark"))
	dict.SetIfNotNil("FixedPrint", a.FixedPrint)
	return indirect
}

// PdfActionType represents an action type in PDF (section 12.6.4 p. 417).
type PdfActionType string

// ToPdfObject implements interface PdfModel.
func (a *PdfActionThread) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeThread)))
	if a.F != nil {
		dict.Set("F", a.F.ToPdfObject())
	}
	dict.SetIfNotNil("D", a.D)
	dict.SetIfNotNil("B", a.B)
	return indirect
}

func appearanceFormAndRect(annot *PdfAnnotation) (*XObjectForm, *PdfRectangle, error) {
	dict, ok := core.GetDict(annot.AP)
	if !ok {
		return nil, nil, errors.New("field missing AP dictionary")
	}
	if dict == nil {
		return nil, nil, nil
	}
	rectArr, ok := core.GetArray(annot.Rect)
	if !ok || rectArr.Len() != 4 {
		return nil, nil, errors.New("rect invalid")
	}
	rect, err := NewPdfRectangle(*rectArr)
	if err != nil {
		return nil, nil, err
	}
	n := core.TraceToDirectObject(dict.Get("N"))
	switch v := n.(type) {
	case *core.PdfObjectStream:
		stream := v
		xObjectForm, err := NewXObjectFormFromStream(stream)
		return xObjectForm, rect, err
	case *core.PdfObjectDictionary:
		dict := v
		if dict == nil {
			common.Log.Debug("ERROR: Failed to get appearance. Dictionary is nil")
			return nil, nil, nil
		}
		var obj core.PdfObject
		name, ok := core.GetName(annot.AS)
		if ok {
			obj = dict.Get(*name)
		} else {
			dict2 := annot.primitive.PdfObject.(*core.PdfObjectDictionary)
			if dict2 == nil {
				common.Log.Debug("ERROR: Failed to get appearance from value.")
				return nil, nil, nil
			}
			if v := dict2.Get("V"); v != nil {
				obj = dict.Get(core.PdfObjectName(v.String()))
			}
		}
		if obj == nil {
			common.Log.Debug("ERROR: AS state not specified in AP dict - ignoring")
			return nil, nil, nil
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			common.Log.Debug("ERROR: Unable to access appearance stream for %v", name)
			return nil, nil, errors.New("stream missing")
		}
		xObjectForm, err := NewXObjectFormFromStream(stream)
		return xObjectForm, rect, err
	}
	common.Log.Debug("Invalid type for N: %T", n)
	return nil, nil, errors.New("type check error")
}

const (
	ActionTypeGoTo        PdfActionType = "GoTo"
	ActionTypeGoTo3DView  PdfActionType = "GoTo3DView"
	ActionTypeGoToE       PdfActionType = "GoToE"
	ActionTypeGoToR       PdfActionType = "GoToR"
	ActionTypeHide        PdfActionType = "Hide"
	ActionTypeImportData  PdfActionType = "ImportData"
	ActionTypeJavaScript  PdfActionType = "JavaScript"
	ActionTypeLaunch      PdfActionType = "Launch"
	ActionTypeMovie       PdfActionType = "Movie"
	ActionTypeNamed       PdfActionType = "Named"
	ActionTypeRendition   PdfActionType = "Rendition"
	ActionTypeResetForm   PdfActionType = "ResetForm"
	ActionTypeSetOCGState PdfActionType = "SetOCGState"
	ActionTypeSound       PdfActionType = "Sound"
	ActionTypeSubmitForm  PdfActionType = "SubmitForm"
	ActionTypeThread      PdfActionType = "Thread"
	ActionTypeTrans       PdfActionType = "Trans"
	ActionTypeURI         PdfActionType = "URI"
)

// NewPdfAnnotationUnderline returns a new text underline annotation.
func NewPdfAnnotationUnderline() *PdfAnnotationUnderline {
	annot := NewPdfAnnotation()
	underline := &PdfAnnotationUnderline{}
	underline.PdfAnnotation = annot
	underline.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(underline)
	return underline
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionTrans) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeTrans)))
	dict.SetIfNotNil("Trans", a.Trans)
	return indirect
}

func anyCRLCovers(crls []crlEvidence, cert *x509.Certificate, t time.Time) bool {
	for _, crl := range crls {
		if crl.isDelta() || !crl.covers(cert) {
			continue
		}
		if crlIsCurrent(crl.ThisUpdate, crl.NextUpdate, cert, t, crl.online) {
			return true
		}
		if crl.Number == nil || crl.ThisUpdate.After(cert.NotAfter) {
			continue
		}
		for _, crl2 := range crls {
			if crl2.isDelta() && crl2.number.Cmp(crl.Number) <= 0 && crl2.newerThan(crl.parsedCRL) && bytes.Equal(crl2.idpRaw, crl.idpRaw) && crlIsCurrent(crl2.ThisUpdate, crl2.NextUpdate, cert, t, crl2.online) {
				return true
			}
		}
	}
	return false
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationUnderline) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Underline"))
	dict.SetIfNotNil("QuadPoints", a.QuadPoints)
	return indirect
}

// NewPdfActionURI returns a new "Uri" action.
func NewPdfActionURI() *PdfActionURI {
	action := NewPdfAction()
	uriAction := &PdfActionURI{}
	uriAction.PdfAction = action
	action.SetContext(uriAction)
	return uriAction
}

// AlphaMapFunc represents a alpha mapping function: byte -> byte. Can be used for
// thresholding the alpha channel, i.e. setting all alpha values below threshold to transparent.
type AlphaMapFunc func(b byte) byte

// AnnotFilterFunc represents a PDF annotation filtering function. If the function
// returns true, the annotation is kept, otherwise it is discarded.
type AnnotFilterFunc func(*PdfAnnotation) bool

// NewPdfAnnotationTrapNet returns a new trapnet annotation.
func NewPdfAnnotationTrapNet() *PdfAnnotationTrapNet {
	annot := NewPdfAnnotation()
	trapNet := &PdfAnnotationTrapNet{}
	trapNet.PdfAnnotation = annot
	annot.SetContext(trapNet)
	return trapNet
}

// NewPdfAnnotationWidget returns an initialized annotation widget.
func NewPdfAnnotationWidget() *PdfAnnotationWidget {
	annot := NewPdfAnnotation()
	widget := &PdfAnnotationWidget{}
	widget.PdfAnnotation = annot
	annot.SetContext(widget)
	return widget
}

// PdfAnnotation3D represents 3D annotations.
// (Section 13.6.2).
type PdfAnnotation3D struct {
	*PdfAnnotation
	T3DD core.PdfObject
	T3DV core.PdfObject
	T3DA core.PdfObject
	T3DI core.PdfObject
	T3DB core.PdfObject
}
