package model

import (
	"errors"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// PdfAnnotationStamp represents Stamp annotations.
// (Section 12.5.6.12).
type PdfAnnotationStamp struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	Name core.PdfObject
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

// NewPdfAnnotationStamp returns a new stamp annotation.
func NewPdfAnnotationStamp() *PdfAnnotationStamp {
	annot := NewPdfAnnotation()
	stamp := &PdfAnnotationStamp{}
	stamp.PdfAnnotation = annot
	stamp.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(stamp)
	return stamp
}

// PdfAnnotationSquiggly represents Squiggly annotations.
// (Section 12.5.6.10).
type PdfAnnotationSquiggly struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	QuadPoints core.PdfObject
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

// NewPdfAnnotationPolygon returns a new polygon annotation.
func NewPdfAnnotationPolygon() *PdfAnnotationPolygon {
	annot := NewPdfAnnotation()
	polygon := &PdfAnnotationPolygon{}
	polygon.PdfAnnotation = annot
	polygon.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(polygon)
	return polygon
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

// NewPdfAnnotationPolyLine returns a new polyline annotation.
func NewPdfAnnotationPolyLine() *PdfAnnotationPolyLine {
	annot := NewPdfAnnotation()
	polyLine := &PdfAnnotationPolyLine{}
	polyLine.PdfAnnotation = annot
	polyLine.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(polyLine)
	return polyLine
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

// NewPdfAnnotationSound returns a new sound annotation.
func NewPdfAnnotationSound() *PdfAnnotationSound {
	annot := NewPdfAnnotation()
	sound := &PdfAnnotationSound{}
	sound.PdfAnnotation = annot
	sound.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(sound)
	return sound
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

// PdfAnnotationPopup represents Popup annotations.
// (Section 12.5.6.14).
type PdfAnnotationPopup struct {
	*PdfAnnotation
	Parent core.PdfObject
	Open   core.PdfObject
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

// NewPdfAnnotationPopup returns a new popup annotation.
func NewPdfAnnotationPopup() *PdfAnnotationPopup {
	annot := NewPdfAnnotation()
	popup := &PdfAnnotationPopup{}
	popup.PdfAnnotation = annot
	annot.SetContext(popup)
	return popup
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

// NewPdfAnnotationText returns a new text annotation.
func NewPdfAnnotationText() *PdfAnnotationText {
	annot := NewPdfAnnotation()
	text := &PdfAnnotationText{}
	text.PdfAnnotation = annot
	text.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(text)
	return text
}

// PdfAnnotationRichMedia represents Rich Media annotations.
type PdfAnnotationRichMedia struct {
	*PdfAnnotation
	RichMediaSettings core.PdfObject
	RichMediaContent  core.PdfObject
}

// NewPdfAnnotationRichMedia returns a new rich media annotation.
func NewPdfAnnotationRichMedia() *PdfAnnotationRichMedia {
	annot := NewPdfAnnotation()
	richMedia := &PdfAnnotationRichMedia{}
	richMedia.PdfAnnotation = annot
	annot.SetContext(richMedia)
	return richMedia
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

// PdfAnnotationStrikeOut represents StrikeOut annotations.
// (Section 12.5.6.10).
type PdfAnnotationStrikeOut struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	QuadPoints core.PdfObject
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

// NewPdfAnnotationStrikeOut returns a new text strikeout annotation.
func NewPdfAnnotationStrikeOut() *PdfAnnotationStrikeOut {
	annot := NewPdfAnnotation()
	strikeOut := &PdfAnnotationStrikeOut{}
	strikeOut.PdfAnnotation = annot
	strikeOut.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(strikeOut)
	return strikeOut
}

// PdfAnnotationTrapNet represents TrapNet annotations.
// (Section 12.5.6.21).
type PdfAnnotationTrapNet struct{ *PdfAnnotation }

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationTrapNet) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("TrapNet"))
	return indirect
}

// NewPdfAnnotationTrapNet returns a new trapnet annotation.
func NewPdfAnnotationTrapNet() *PdfAnnotationTrapNet {
	annot := NewPdfAnnotation()
	trapNet := &PdfAnnotationTrapNet{}
	trapNet.PdfAnnotation = annot
	annot.SetContext(trapNet)
	return trapNet
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

// Field returns the parent form field of the widget annotation, if one exists.
// NOTE: the method returns nil if the parent form field has not been parsed.
func (a *PdfAnnotationWidget) Field() *PdfField { return a.field }

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

// NewPdfAnnotation3D returns a new 3d annotation.
func NewPdfAnnotation3D() *PdfAnnotation3D {
	annot := NewPdfAnnotation()
	annot3D := &PdfAnnotation3D{}
	annot3D.PdfAnnotation = annot
	annot.SetContext(annot3D)
	return annot3D
}

// PdfAnnotationWatermark represents Watermark annotations.
// (Section 12.5.6.22).
type PdfAnnotationWatermark struct {
	*PdfAnnotation
	FixedPrint core.PdfObject
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

// PdfAnnotationUnderline represents Underline annotations.
// (Section 12.5.6.10).
type PdfAnnotationUnderline struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	QuadPoints core.PdfObject
}

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
func (a *PdfAnnotationUnderline) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Underline"))
	dict.SetIfNotNil("QuadPoints", a.QuadPoints)
	return indirect
}

// AnnotFilterFunc represents a PDF annotation filtering function. If the function
// returns true, the annotation is kept, otherwise it is discarded.
type AnnotFilterFunc func(*PdfAnnotation) bool

// PdfBorderStyle represents a border style dictionary (12.5.4 Border Styles p. 394).
type PdfBorderStyle struct {
	W         *float64
	S         *BorderStyle
	D         *[]int
	container core.PdfObject
}

// GetBorderWidth returns the border style's width.
func (b *PdfBorderStyle) GetBorderWidth() float64 {
	if b.W == nil {
		return 1
	}
	return *b.W
}

// ToPdfObject implements interface PdfModel.
func (b *PdfBorderStyle) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	if b.container != nil {
		if indirect, ok := b.container.(*core.PdfIndirectObject); ok {
			indirect.PdfObject = dict
		}
	}
	dict.Set("Subtype", core.MakeName("Border"))
	if b.W != nil {
		dict.Set("W", core.MakeFloat(*b.W))
	}
	if b.S != nil {
		dict.Set("S", core.MakeName(b.S.GetPdfName()))
	}
	if b.D != nil {
		dict.Set("D", core.MakeArrayFromIntegers(*b.D))
	}
	if b.container != nil {
		return b.container
	}
	return dict
}

// SetBorderWidth sets the style's border width.
func (b *PdfBorderStyle) SetBorderWidth(width float64) { b.W = &width }

func newPdfBorderStyleFromPdfObject(obj core.PdfObject) (*PdfBorderStyle, error) {
	borderStyle := &PdfBorderStyle{}
	borderStyle.container = obj
	var dict *core.PdfObjectDictionary
	obj = core.TraceToDirectObject(obj)
	dict, ok := obj.(*core.PdfObjectDictionary)
	if !ok {
		return nil, errors.New("type check")
	}
	if obj := dict.Get("Type"); obj != nil {
		name, ok := obj.(*core.PdfObjectName)
		if !ok {
			common.Log.Debug("Incompatibility with Type not a name object: %T", obj)
		} else {
			if *name != "Border" {
				common.Log.Debug("Warning, Type != Border: %s", *name)
			}
		}
	}
	if w := dict.Get("W"); w != nil {
		f, err := core.GetNumberAsFloat(w)
		if err != nil {
			common.Log.Debug("Error retrieving W: %v", err)
			return nil, err
		}
		borderStyle.W = &f
	}
	if s := dict.Get("S"); s != nil {
		name, ok := s.(*core.PdfObjectName)
		if !ok {
			return nil, errors.New("border S not a name object")
		}
		var style BorderStyle
		switch *name {
		case "S":
			style = BorderStyleSolid
		case "D":
			style = BorderStyleDashed
		case "B":
			style = BorderStyleBeveled
		case "I":
			style = BorderStyleInset
		case "U":
			style = BorderStyleUnderline
		default:
			common.Log.Debug("Invalid style name %s", *name)
			return nil, errors.New("style type range check")
		}
		borderStyle.S = &style
	}
	if d := dict.Get("D"); d != nil {
		arr, ok := d.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Border D dash not an array: %T", d)
			return nil, errors.New("border D type check error")
		}
		nums, err := arr.ToIntegerArray()
		if err != nil {
			common.Log.Debug("Border D Problem converting to integer array: %v", err)
			return nil, err
		}
		borderStyle.D = &nums
	}
	return borderStyle, nil
}

// BorderEffect represents a border effect (Table 167 p. 395).
type BorderEffect int

const (
	BorderEffectNoEffect BorderEffect = iota
	BorderEffectCloudy   BorderEffect = iota
)
