package model

import (
	"fmt"
	_ "image/gif"
	"slices"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// IsTerminal returns true for terminal fields, false otherwise.
// Terminal fields are fields whose descendants are only widget annotations.
func (f *PdfField) IsTerminal() bool { return len(f.Kids) == 0 }

// ToPdfObject sets the common field elements.
// Note: Call the more field context's ToPdfObject to set both the generic and
// non-generic information.
func (f *PdfField) ToPdfObject() core.PdfObject {
	indirect := f.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	array := core.MakeArray()
	for _, kid := range f.Kids {
		array.Append(kid.ToPdfObject())
	}
	for _, annotation := range f.Annotations {
		if annotation.primitive != f.primitive {
			array.Append(annotation.GetContext().ToPdfObject())
		}
	}
	if f.Parent != nil {
		dict.SetIfNotNil("Parent", f.Parent.GetContainingPdfObject())
	}
	if array.Len() > 0 {
		dict.Set("Kids", array)
	}
	dict.SetIfNotNil("FT", f.FT)
	dict.SetIfNotNil("T", f.T)
	dict.SetIfNotNil("TU", f.TU)
	dict.SetIfNotNil("TM", f.TM)
	dict.SetIfNotNil("Ff", f.Ff)
	dict.SetIfNotNil("V", f.V)
	dict.SetIfNotNil("DV", f.DV)
	dict.SetIfNotNil("AA", f.AA)
	if f.VariableText != nil {
		dict.SetIfNotNil("DA", f.VariableText.DA)
		dict.SetIfNotNil("Q", f.VariableText.Q)
		dict.SetIfNotNil("DS", f.VariableText.DS)
		dict.SetIfNotNil("RV", f.VariableText.RV)
	}
	return indirect
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

func (f *PdfField) resolveMaxLen() int {
	var n int
	_, _ = f.inherit(func(field *PdfField) bool {
		textField, ok := field.GetContext().(*PdfFieldText)
		if !ok || textField.MaxLen == nil {
			return false
		}
		n = int(*textField.MaxLen)
		return true
	})
	return n
}

// PartialName returns the partial name of the field.
func (f *PdfField) PartialName() string {
	s := ""
	if f.T != nil {
		s = f.T.Decoded()
	} else {
		common.Log.Debug("Field missing T field (incompatible)")
	}
	return s
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

// FieldType is the resolved kind of form field.
type FieldType int

// String returns a string representation of the field.
func (f *PdfField) String() string {
	if indirect, ok := f.ToPdfObject().(*core.PdfIndirectObject); ok {
		return fmt.Sprintf("%T: %s", f.context, indirect.PdfObject.String())
	}
	return ""
}

func (f *PdfField) resolveDA() string {
	var s string
	_, _ = f.inherit(func(field *PdfField) bool {
		if field.VariableText != nil && field.VariableText.DA != nil {
			s = field.VariableText.DA.Decoded()
			return true
		}
		return false
	})
	return s
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

const (
	FieldTypeUnknown FieldType = iota
	FieldTypeText
	FieldTypeButton
	FieldTypeChoice
	FieldTypeSignature
)

// GetProperties returns a snapshot of the field's resolved properties,
// including parent-inherited values for V, DV, Ff, DA, Q, MaxLen. The
// returned pointer is freshly allocated; mutating it does not affect the
// underlying field.
//
// AcroForm-level /DA and /Q defaults are not consulted.
func (f *PdfField) GetProperties() *FieldProperties {
	if f == nil {
		return nil
	}
	fieldProperties := &FieldProperties{PartialName: f.PartialName(), Type: f.FieldType()}
	if fullName, err := f.FullName(); err == nil {
		fieldProperties.FullName = fullName
	}
	if f.TU != nil {
		fieldProperties.AlternateName = f.TU.Decoded()
	}
	if f.TM != nil {
		fieldProperties.MappingName = f.TM.Decoded()
	}
	fieldProperties.Value = f.resolveValueString(false)
	fieldProperties.DefaultValue = f.resolveValueString(true)
	flags := f.Flags()
	fieldProperties.ReadOnly = flags&FieldFlagReadOnly > 0
	fieldProperties.Required = flags&FieldFlagRequired > 0
	fieldProperties.NoExport = flags&FieldFlagNoExport > 0
	fieldProperties.Multiline = flags&FieldFlagMultiline > 0
	fieldProperties.Password = flags&FieldFlagPassword > 0
	fieldProperties.FileSelect = flags&FieldFlagFileSelect > 0
	fieldProperties.DoNotScroll = flags&FieldFlagDoNotScroll > 0
	fieldProperties.Comb = flags&FieldFlagComb > 0
	fieldProperties.RichText = flags&FieldFlagRichText > 0
	fieldProperties.DoNotSpellCheck = flags&FieldFlagDoNotSpellCheck > 0
	fieldProperties.Combo = flags&FieldFlagCombo > 0
	fieldProperties.Edit = flags&FieldFlagEdit > 0
	fieldProperties.Sort = flags&FieldFlagSort > 0
	fieldProperties.MultiSelect = flags&FieldFlagMultiSelect > 0
	fieldProperties.CommitOnSelChange = flags&FieldFlagCommitOnSelChange > 0
	fieldProperties.PushButton = flags&FieldFlagPushbutton > 0
	fieldProperties.Radio = flags&FieldFlagRadio > 0
	fieldProperties.NoToggleToOff = flags&FieldFlagNoToggleToOff > 0
	fieldProperties.RadiosInUnison = flags&FieldFlagRadiosInUnision > 0
	fieldProperties.FontName, fieldProperties.FontSize, fieldProperties.TextColor = parseDefaultAppearance(f.resolveDA())
	fieldProperties.Alignment = f.resolveQ()
	fieldProperties.MaxLength = f.resolveMaxLen()
	if choiceField, ok := f.GetContext().(*PdfFieldChoice); ok && choiceField.Opt != nil {
		fieldProperties.Options = stringsFromArray(choiceField.Opt)
	}
	widgets := fieldWidgets(f)
	if len(widgets) > 0 {
		fieldProperties.BorderColor, fieldProperties.BackgroundColor, fieldProperties.Caption, fieldProperties.Rotation = widgetMKProperties(widgets[0])
		fieldProperties.Widgets = make([]WidgetProperties, 0, len(widgets))
		for _, widget := range widgets {
			fieldProperties.Widgets = append(fieldProperties.Widgets, newWidgetProperties(widget))
		}
	}
	return fieldProperties
}

// FieldValueProvider provides field values from a data source such as FDF, JSON or any other.
type FieldValueProvider interface {
	FieldValues() (map[string]core.PdfObject, error)
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

func (f *PdfField) resolveValueString(ok bool) string {
	var obj core.PdfObject
	_, _ = f.inherit(func(field *PdfField) bool {
		var obj2 core.PdfObject
		if ok {
			obj2 = field.DV
		} else {
			obj2 = field.V
		}
		if obj2 == nil {
			return false
		}
		obj = obj2
		return true
	})
	if obj != nil {
		return decodedString(obj)
	}
	if ok {
		return ""
	}
	for _, annotation := range f.Annotations {
		if annotation == nil || annotation.PdfAnnotation == nil || annotation.AS == nil {
			continue
		}
		if name, ok := core.GetName(annotation.AS); ok {
			return string(*name)
		}
	}
	return ""
}

func (f *PdfField) resolveQ() Quadding {
	quadding := QuaddingLeft
	_, _ = f.inherit(func(field *PdfField) bool {
		if field.VariableText != nil && field.VariableText.Q != nil {
			quadding2 := Quadding(*field.VariableText.Q)
			switch quadding2 {
			case QuaddingLeft, QuaddingCentered, QuaddingRight:
				quadding = quadding2
			default:
				quadding = QuaddingLeft
			}
			return true
		}
		return false
	})
	return quadding
}

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
