package model

import (
	_ "image/gif"
	"slices"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// FieldFlag represents form field flags. Some of the flags can apply to all types of fields whereas other
// flags are specific.
type FieldFlag uint32

// Clear clears flag fl from the flag and returns the resulting flag.
func (f FieldFlag) Clear(fl FieldFlag) FieldFlag { return FieldFlag(f.Mask() &^ fl.Mask()) }

// Mask returns the uin32 bitmask for the specific flag.
func (f FieldFlag) Mask() uint32 { return uint32(f) }

// Has checks if flag fl is set in flag and returns true if so, false otherwise.
func (f FieldFlag) Has(fl FieldFlag) bool { return (f.Mask() & fl.Mask()) > 0 }

const (
	FieldFlagClear             FieldFlag = 0
	FieldFlagReadOnly          FieldFlag = 1
	FieldFlagRequired          FieldFlag = (1 << 1)
	FieldFlagNoExport          FieldFlag = (2 << 1)
	FieldFlagNoToggleToOff     FieldFlag = (1 << 14)
	FieldFlagRadio             FieldFlag = (1 << 15)
	FieldFlagPushbutton        FieldFlag = (1 << 16)
	FieldFlagRadiosInUnision   FieldFlag = (1 << 25)
	FieldFlagMultiline         FieldFlag = (1 << 12)
	FieldFlagPassword          FieldFlag = (1 << 13)
	FieldFlagFileSelect        FieldFlag = (1 << 20)
	FieldFlagDoNotScroll       FieldFlag = (1 << 23)
	FieldFlagComb              FieldFlag = (1 << 24)
	FieldFlagRichText          FieldFlag = (1 << 26)
	FieldFlagDoNotSpellCheck   FieldFlag = (1 << 22)
	FieldFlagCombo             FieldFlag = (1 << 17)
	FieldFlagEdit              FieldFlag = (1 << 18)
	FieldFlagSort              FieldFlag = (1 << 19)
	FieldFlagMultiSelect       FieldFlag = (1 << 21)
	FieldFlagCommitOnSelChange FieldFlag = (1 << 27)
)

// Set applies flag fl to the flag's bitmask and returns the combined flag.
func (f FieldFlag) Set(fl FieldFlag) FieldFlag { return FieldFlag(f.Mask() | fl.Mask()) }

// String returns a string representation of what flags are set.
func (f FieldFlag) String() string {
	s := ""
	if f == FieldFlagClear {
		s = "Clear"
		return s
	}
	if f&FieldFlagReadOnly > 0 {
		s += "|ReadOnly"
	}
	if f&FieldFlagRequired > 0 {
		s += "|Required"
	}
	if f&FieldFlagNoExport > 0 {
		s += "|NoExport"
	}
	if f&FieldFlagNoToggleToOff > 0 {
		s += "|NoToggleToOff"
	}
	if f&FieldFlagRadio > 0 {
		s += "|Radio"
	}
	if f&FieldFlagPushbutton > 0 {
		s += "|Pushbutton"
	}
	if f&FieldFlagRadiosInUnision > 0 {
		s += "|RadiosInUnision"
	}
	if f&FieldFlagMultiline > 0 {
		s += "|Multiline"
	}
	if f&FieldFlagPassword > 0 {
		s += "|Password"
	}
	if f&FieldFlagFileSelect > 0 {
		s += "|FileSelect"
	}
	if f&FieldFlagDoNotScroll > 0 {
		s += "|DoNotScroll"
	}
	if f&FieldFlagComb > 0 {
		s += "|Comb"
	}
	if f&FieldFlagRichText > 0 {
		s += "|RichText"
	}
	if f&FieldFlagDoNotSpellCheck > 0 {
		s += "|DoNotSpellCheck"
	}
	if f&FieldFlagCombo > 0 {
		s += "|Combo"
	}
	if f&FieldFlagEdit > 0 {
		s += "|Edit"
	}
	if f&FieldFlagSort > 0 {
		s += "|Sort"
	}
	if f&FieldFlagMultiSelect > 0 {
		s += "|MultiSelect"
	}
	if f&FieldFlagCommitOnSelChange > 0 {
		s += "|CommitOnSelChange"
	}
	return strings.Trim(s, "|")
}

// FieldFilterFunc represents a PDF field filtering function. If the function
// returns true, the PDF field is kept, otherwise it is discarded.
type FieldFilterFunc func(*PdfField) bool

// FieldAppearanceGenerator generates appearance stream for a given field.
type FieldAppearanceGenerator interface {
	ContentStreamWrapper
	GenerateAppearanceDict(acroForm *PdfAcroForm, field *PdfField, widget *PdfAnnotationWidget) (*core.PdfObjectDictionary, error)
}

// FieldImageProvider provides fields images for specified fields.
type FieldImageProvider interface {
	FieldImageValues() (map[string]*Image, error)
}

var fieldDictKeys = map[string]struct{}{"FT": {}, "Kids": {}, "T": {}, "TU": {}, "TM": {}, "Ff": {}, "V": {}, "DV": {}, "AA": {}, "DA": {}, "Q": {}, "DS": {}, "RV": {}}

func fieldWidgets(field *PdfField) []*PdfAnnotationWidget {
	widgets := append([]*PdfAnnotationWidget(nil), field.Annotations...)
	if signatureField, ok := field.GetContext().(*PdfFieldSignature); ok && signatureField.PdfAnnotationWidget != nil {
		ok := slices.Contains(widgets, signatureField.PdfAnnotationWidget)
		if !ok {
			widgets = append(widgets, signatureField.PdfAnnotationWidget)
		}
	}
	return widgets
}

// PdfFieldSignature signature field represents digital signatures and optional data for authenticating
// the name of the signer and verifying document contents.
type PdfFieldSignature struct {
	*PdfField
	*PdfAnnotationWidget
	V    *PdfSignature
	Lock *core.PdfIndirectObject
	SV   *core.PdfIndirectObject
}

// ToPdfObject returns an indirect object containing the signature field dictionary.
func (f *PdfFieldSignature) ToPdfObject() core.PdfObject {
	if f.PdfAnnotationWidget != nil {
		f.PdfAnnotationWidget.ToPdfObject()
	}
	f.PdfField.ToPdfObject()
	indirect := f.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("FT", core.MakeName("Sig"))
	dict.SetIfNotNil("Lock", f.Lock)
	dict.SetIfNotNil("SV", f.SV)
	if f.V != nil {
		dict.SetIfNotNil("V", f.V.ToPdfObject())
	}
	return indirect
}

// NewPdfFieldSignature returns an initialized signature field.
func NewPdfFieldSignature(signature *PdfSignature) *PdfFieldSignature {
	signatureField := &PdfFieldSignature{}
	signatureField.PdfField = NewPdfField()
	signatureField.SetContext(signatureField)
	signatureField.PdfAnnotationWidget = NewPdfAnnotationWidget()
	signatureField.PdfAnnotationWidget.SetContext(signatureField)
	signatureField.PdfAnnotationWidget.primitive = signatureField.primitive
	signatureField.T = core.MakeString("")
	signatureField.F = core.MakeInteger(132)
	signatureField.V = signature
	return signatureField
}

// PdfFieldText represents a text field where user can enter text.
type PdfFieldText struct {
	*PdfField
	DA     *core.PdfObjectString
	Q      *core.PdfObjectInteger
	DS     *core.PdfObjectString
	RV     core.PdfObject
	MaxLen *core.PdfObjectInteger
}

// ToPdfObject returns the text field dictionary within an indirect object (container).
func (f *PdfFieldText) ToPdfObject() core.PdfObject {
	f.PdfField.ToPdfObject()
	indirect := f.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.Set("FT", core.MakeName("Tx"))
	if f.DA != nil {
		dict.Set("DA", f.DA)
	}
	if f.Q != nil {
		dict.Set("Q", f.Q)
	}
	if f.DS != nil {
		dict.Set("DS", f.DS)
	}
	if f.RV != nil {
		dict.Set("RV", f.RV)
	}
	if f.MaxLen != nil {
		dict.Set("MaxLen", f.MaxLen)
	}
	return indirect
}

func newPdfFieldTextFromDict(dict *core.PdfObjectDictionary) (*PdfFieldText, error) {
	textField := &PdfFieldText{}
	textField.DA, _ = core.GetString(dict.Get("DA"))
	textField.Q, _ = core.GetInt(dict.Get("Q"))
	textField.DS, _ = core.GetString(dict.Get("DS"))
	textField.RV = dict.Get("RV")
	textField.MaxLen, _ = core.GetInt(dict.Get("MaxLen"))
	return textField, nil
}

// FieldValue is the fully qualified name of a terminal (leaf) form field
// paired with its raw value object.
type FieldValue struct {
	// Name is the fully qualified field name, e.g. "form.address.city".
	Name string

	// Value is the field's /V entry resolved to a direct object (a string,
	// name or array for regular fields), or nil when the field has no value.
	Value core.PdfObject

	// Field is the terminal field the value belongs to.
	Field *PdfField
}

func setFieldValue(field *PdfField, obj core.PdfObject) error {
	switch field.GetContext().(type) {
	case *PdfFieldText:
		switch v := obj.(type) {
		case *core.PdfObjectName:
			name := v
			common.Log.Debug("Unexpected: Got V as name -> converting to string '%s'", name.String())
			field.V = core.MakeEncodedString(v.String(), true)
		case *core.PdfObjectString:
			field.V = core.MakeEncodedString(decodedTextString(v), true)
		default:
			common.Log.Debug("ERROR: Unsupported text field V type: %T (%#v)", v, v)
		}
	case *PdfFieldButton:
		switch obj.(type) {
		case *core.PdfObjectName:
			if len(obj.String()) > 0 {
				field.V = obj
				setAnnotationsAppearanceState(field, obj)
			}
		case *core.PdfObjectString:
			if len(obj.String()) > 0 {
				field.V = core.MakeName(obj.String())
				setAnnotationsAppearanceState(field, field.V)
			}
		default:
			common.Log.Debug("ERROR: UNEXPECTED %s -> %v", field.PartialName(), obj)
			field.V = obj
		}
	case *PdfFieldChoice:
		switch obj.(type) {
		case *core.PdfObjectName:
			if len(obj.String()) > 0 {
				field.V = core.MakeString(obj.String())
				setAnnotationsAppearanceState(field, obj)
			}
		case *core.PdfObjectString:
			if len(obj.String()) > 0 {
				field.V = obj
				setAnnotationsAppearanceState(field, core.MakeName(obj.String()))
			}
		case *core.PdfObjectArray:
			field.V = obj
		default:
			common.Log.Debug("ERROR: UNEXPECTED %s -> %v", field.PartialName(), obj)
			field.V = obj
		}
	case *PdfFieldSignature:
		common.Log.Debug("Signature field %s: value not fillable - ignoring %v", field.PartialName(), obj)
	}
	return nil
}

// FieldProperties is a flat, language-friendly snapshot of a form field's
// commonly used attributes. Properties are resolved with parent inheritance
// applied (Ff, V, DV, DA, Q, MaxLen), so callers do not need to walk the
// field tree.
//
// AcroForm-level /DA and /Q defaults are intentionally not consulted in this
// version; most production forms set DA at the field level.
type FieldProperties struct {
	// Identity.
	FullName      string
	PartialName   string
	AlternateName string
	MappingName   string
	Type          FieldType

	// Value.
	Value        string
	DefaultValue string

	// Common flags (resolved with inheritance).
	ReadOnly bool
	Required bool
	NoExport bool

	// Text-field flags.
	Multiline       bool
	Password        bool
	FileSelect      bool
	DoNotScroll     bool
	Comb            bool
	RichText        bool
	DoNotSpellCheck bool

	// Choice-field flags.
	Combo             bool
	Edit              bool
	Sort              bool
	MultiSelect       bool
	CommitOnSelChange bool

	// Button-field flags.
	PushButton     bool
	Radio          bool
	NoToggleToOff  bool
	RadiosInUnison bool

	// Variable-text properties (parsed from inherited DA).
	FontName  string
	FontSize  float64
	TextColor PdfColor
	Alignment Quadding

	// Text-specific.
	MaxLength int

	// Choice-specific.
	Options []string

	// Widget appearance characteristics, taken from /MK on the first widget
	// annotation. Nil colors / empty strings when /MK is absent. For
	// multi-widget fields callers can use Widgets to inspect each one.
	BorderColor     PdfColor
	BackgroundColor PdfColor
	Caption         string
	Rotation        int

	// Per-widget snapshots for fields with more than one annotation
	// (radio groups, multi-page widgets, etc.).
	Widgets []WidgetProperties
}

// FieldType is the resolved kind of form field.
type FieldType int

const (
	FieldTypeUnknown FieldType = iota
	FieldTypeText
	FieldTypeButton
	FieldTypeChoice
	FieldTypeSignature
)

// FieldValueProvider provides field values from a data source such as FDF, JSON or any other.
type FieldValueProvider interface {
	FieldValues() (map[string]core.PdfObject, error)
}

// Quadding is the text alignment value derived from a field's /Q entry.
type Quadding int

const (
	QuaddingLeft     Quadding = 0
	QuaddingCentered Quadding = 1
	QuaddingRight    Quadding = 2
)

// VariableText contains the common attributes of a variable text.
// The VariableText is typically not used directly, but is can encapsulate by PdfField
// See section 12.7.3.3 "Variable Text" and Table 222 (pp. 434-436 PDF32000_2008).
type VariableText struct {
	DA *core.PdfObjectString
	Q  *core.PdfObjectInteger
	DS *core.PdfObjectString
	RV core.PdfObject
}

func widgetMKProperties(widget *PdfAnnotationWidget) (col, col2 PdfColor, s string, n int) {
	if widget == nil || widget.MK == nil {
		return
	}
	dict, ok := core.GetDict(widget.MK)
	if !ok {
		return
	}
	if bc, ok := core.GetArray(dict.Get("BC")); ok {
		col = colorFromArray(bc)
	}
	if bg, ok := core.GetArray(dict.Get("BG")); ok {
		col2 = colorFromArray(bg)
	}
	if ca, ok := core.GetString(dict.Get("CA")); ok {
		s = ca.Decoded()
	}
	if r, ok := core.GetInt(dict.Get("R")); ok {
		n = int(*r)
	}
	return
}

// WidgetProperties describes appearance characteristics for a single
// widget annotation of a form field.
type WidgetProperties struct {
	Rect            *PdfRectangle
	BorderColor     PdfColor
	BackgroundColor PdfColor
	Caption         string
	Rotation        int
	AppearanceState string
}

func newWidgetProperties(widget *PdfAnnotationWidget) WidgetProperties {
	var widgetProperties WidgetProperties
	widgetProperties.BorderColor, widgetProperties.BackgroundColor, widgetProperties.Caption, widgetProperties.Rotation = widgetMKProperties(widget)
	if widget == nil || widget.PdfAnnotation == nil {
		return widgetProperties
	}
	if rectArr, ok := core.GetArray(widget.Rect); ok {
		if rect, err := NewPdfRectangle(*rectArr); err == nil {
			widgetProperties.Rect = rect
		}
	}
	if name, ok := core.GetName(widget.AS); ok {
		widgetProperties.AppearanceState = string(*name)
	}
	return widgetProperties
}

// ButtonType represents the subtype of a button field, can be one of:
// - Checkbox (ButtonTypeCheckbox)
// - PushButton (ButtonTypePushButton)
// - RadioButton (ButtonTypeRadioButton)
type ButtonType int

const (
	ButtonTypeCheckbox ButtonType = iota
	ButtonTypePush     ButtonType = iota
	ButtonTypeRadio    ButtonType = iota
)
