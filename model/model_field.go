package model

import (
	"bytes"
	"errors"
	"fmt"
	_ "image/gif"
	"slices"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// PdfField contains the common attributes of a form field. The context object contains the specific field data
// which can represent a button, text, choice or signature.
// The PdfField is typically not used directly, but is encapsulated by the more specific field types such as
// PdfFieldButton etc (i.e. the context attribute).
type PdfField struct {
	context      PdfModel
	primitive    *core.PdfIndirectObject
	Parent       *PdfField
	Annotations  []*PdfAnnotationWidget
	Kids         []*PdfField
	FT           *core.PdfObjectName
	T            *core.PdfObjectString
	TU           *core.PdfObjectString
	TM           *core.PdfObjectString
	Ff           *core.PdfObjectInteger
	V            core.PdfObject
	DV           core.PdfObject
	AA           core.PdfObject
	VariableText *VariableText
}

// GetContext returns the PdfField context which is the more specific field data type, e.g. PdfFieldButton
// for a button field.
func (f *PdfField) GetContext() PdfModel { return f.context }

// Flags returns the field flags for the field accounting for any inherited flags.
func (f *PdfField) Flags() FieldFlag {
	var fieldFlag FieldFlag
	ok, err := f.inherit(func(field *PdfField) bool {
		if field.Ff != nil {
			fieldFlag = FieldFlag(*field.Ff)
			return true
		}
		return false
	})
	if err != nil {
		common.Log.Debug("Error evaluating flags via inheritance: %v", err)
	}
	if !ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("No field flags found - assume clear")
		}
	}
	return fieldFlag
}

// FullName returns the full name of the field as in rootname.parentname.partialname.
func (f *PdfField) FullName() (string, error) {
	var buf bytes.Buffer
	strs := []string{}
	if f.T != nil {
		strs = append(strs, f.T.Decoded())
	}
	set := map[*PdfField]bool{}
	set[f] = true
	parent := f.Parent
	for parent != nil {
		if _, ok := set[parent]; ok {
			return buf.String(), errors.New("recursive traversal")
		}
		if parent.T == nil {
			return buf.String(), errors.New("field partial name (T) not specified")
		}
		strs = append(strs, parent.T.Decoded())
		set[parent] = true
		parent = parent.Parent
	}
	for i, str := range slices.Backward(strs) {
		buf.WriteString(str)
		if i > 0 {
			buf.WriteString(".")
		}
	}
	return buf.String(), nil
}

// GetContainingPdfObject returns the containing object for the PdfField, i.e. an indirect object
// containing the field dictionary.
func (f *PdfField) GetContainingPdfObject() core.PdfObject { return f.primitive }

func (f *PdfField) inheritedValue() core.PdfObject {
	var obj core.PdfObject
	f.inherit(func(field *PdfField) bool { obj = field.V; return obj != nil })
	return obj
}

// NewPdfField returns an initialized PdfField.
func NewPdfField() *PdfField { return &PdfField{primitive: core.MakeIndirectObject(core.MakeDict())} }

// SetContext sets the specific fielddata type, e.g. would be PdfFieldButton for a button field.
func (f *PdfField) SetContext(ctx PdfModel) { f.context = ctx }

// FieldType returns the resolved field type, walking up to the parent when
// /FT is inherited.
func (f *PdfField) FieldType() FieldType {
	if f == nil {
		return FieldTypeUnknown
	}
	var s string
	_, _ = f.inherit(func(field *PdfField) bool {
		if field.FT == nil {
			return false
		}
		s = string(*field.FT)
		return true
	})
	switch s {
	case "Tx":
		return FieldTypeText
	case "Btn":
		return FieldTypeButton
	case "Ch":
		return FieldTypeChoice
	case "Sig":
		return FieldTypeSignature
	}
	return FieldTypeUnknown
}

// SetFlag sets the flag for the field.
func (f *PdfField) SetFlag(flag FieldFlag) { f.Ff = core.MakeInteger(int64(flag)) }

func (f *PdfField) inherit(fn func(*PdfField) bool) (bool, error) {
	set := map[*PdfField]bool{}
	ok := false
	field := f
	for field != nil {
		if _, ok := set[field]; ok {
			return false, errors.New("recursive traversal")
		}
		found := fn(field)
		if found {
			ok = true
			break
		}
		set[field] = true
		field = field.Parent
	}
	return ok, nil
}

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

// PdfFieldChoice represents a choice field which includes scrollable list boxes and combo boxes.
type PdfFieldChoice struct {
	*PdfField
	Opt *core.PdfObjectArray
	TI  *core.PdfObjectInteger
	I   *core.PdfObjectArray
}

func newPdfFieldChoiceFromDict(dict *core.PdfObjectDictionary) (*PdfFieldChoice, error) {
	choiceField := &PdfFieldChoice{}
	choiceField.Opt, _ = core.GetArray(dict.Get("Opt"))
	choiceField.TI, _ = core.GetInt(dict.Get("TI"))
	choiceField.I, _ = core.GetArray(dict.Get("I"))
	return choiceField, nil
}

// ToPdfObject returns the choice field dictionary within an indirect object (container).
func (f *PdfFieldChoice) ToPdfObject() core.PdfObject {
	f.PdfField.ToPdfObject()
	indirect := f.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.Set("FT", core.MakeName("Ch"))
	if f.Opt != nil {
		dict.Set("Opt", f.Opt)
	}
	if f.TI != nil {
		dict.Set("TI", f.TI)
	}
	if f.I != nil {
		dict.Set("I", f.I)
	}
	return indirect
}

// FieldFlattenOpts defines a set of options which can be used to configure
// the field flattening process.
type FieldFlattenOpts struct {
	// FilterFunc allows filtering the form fields used in the flattening
	// process. If the filter function returns true, the field is flattened,
	// otherwise it is skipped.
	// If a non-terminal field is discarded, all of its children (the fields
	// present in the Kids array) are discarded as well.
	// Non-terminal fields are kept in the AcroForm if one or more of their
	// child fields have not been selected for flattening.
	// If a filter function is not provided, all form fields are flattened.
	FilterFunc FieldFilterFunc

	// AnnotFilterFunc allows filtering the annotations in the flattening
	// process. If the filter function returns true, the annotation is flattened,
	// otherwise it is skipped.
	// Widget (form field) annotations are not subject to this filter: the
	// page-level flattening path always flattens them, and the reader-level
	// path selects them via FilterFunc.
	AnnotFilterFunc AnnotFilterFunc
}

// PdfFieldButton represents a button field which includes push buttons, checkboxes, and radio buttons.
type PdfFieldButton struct {
	*PdfField
	Opt         *core.PdfObjectArray
	buttonImage *Image
}

func newPdfFieldButtonFromDict(dict *core.PdfObjectDictionary) (*PdfFieldButton, error) {
	buttonField := &PdfFieldButton{}
	buttonField.PdfField = NewPdfField()
	buttonField.SetContext(buttonField)
	buttonField.Opt, _ = core.GetArray(dict.Get("Opt"))
	widget := NewPdfAnnotationWidget()
	widget.A, _ = core.GetDict(dict.Get("A"))
	widget.AP, _ = core.GetDict(dict.Get("AP"))
	widget.SetContext(buttonField)
	buttonField.Annotations = append(buttonField.Annotations, widget)
	return buttonField, nil
}

// SetType sets the field button's type.  Can be one of:
// - PdfFieldButtonPush for push button fields
// - PdfFieldButtonCheckbox for checkbox fields
// - PdfFieldButtonRadio for radio button fields
// This sets the field's flag appropriately.
func (f *PdfFieldButton) SetType(btype ButtonType) {
	n := uint32(0)
	if f.Ff != nil {
		n = uint32(*f.Ff)
	}
	switch btype {
	case ButtonTypePush:
		n |= FieldFlagPushbutton.Mask()
	case ButtonTypeRadio:
		n |= FieldFlagRadio.Mask()
	}
	f.Ff = core.MakeInteger(int64(n))
}

// IsRadio returns true if the button field represents a radio button, false otherwise.
func (f *PdfFieldButton) IsRadio() bool { return f.GetType() == ButtonTypeRadio }

// SetFillImage attach a model.Image to push button.
func (f *PdfFieldButton) SetFillImage(image *Image) {
	if f.IsPush() {
		f.buttonImage = image
	}
}

// GetType returns the button field type which returns one of the following
// - PdfFieldButtonPush for push button fields
// - PdfFieldButtonCheckbox for checkbox fields
// - PdfFieldButtonRadio for radio button fields
func (f *PdfFieldButton) GetType() ButtonType {
	buttonType := ButtonTypeCheckbox
	if f.Ff != nil {
		if (uint32(*f.Ff) & FieldFlagPushbutton.Mask()) > 0 {
			buttonType = ButtonTypePush
		} else if (uint32(*f.Ff) & FieldFlagRadio.Mask()) > 0 {
			buttonType = ButtonTypeRadio
		}
	}
	return buttonType
}

// GetFillImage get attached model.Image in push button.
func (f *PdfFieldButton) GetFillImage() *Image {
	if f.IsPush() {
		return f.buttonImage
	}
	return nil
}

// ToPdfObject returns the button field dictionary within an indirect object.
func (f *PdfFieldButton) ToPdfObject() core.PdfObject {
	f.PdfField.ToPdfObject()
	indirect := f.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.Set("FT", core.MakeName("Btn"))
	if f.Opt != nil {
		dict.Set("Opt", f.Opt)
	}
	return indirect
}

// IsCheckbox returns true if the button field represents a checkbox, false otherwise.
func (f *PdfFieldButton) IsCheckbox() bool { return f.GetType() == ButtonTypeCheckbox }

// IsPush returns true if the button field represents a push button, false otherwise.
func (f *PdfFieldButton) IsPush() bool { return f.GetType() == ButtonTypePush }
