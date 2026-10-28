package model

import (
	"bytes"
	"errors"
	_ "image/gif"
	"slices"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// GetContext returns the PdfField context which is the more specific field data type, e.g. PdfFieldButton
// for a button field.
func (f *PdfField) GetContext() PdfModel { return f.context }

func newPdfFieldChoiceFromDict(dict *core.PdfObjectDictionary) (*PdfFieldChoice, error) {
	choiceField := &PdfFieldChoice{}
	choiceField.Opt, _ = core.GetArray(dict.Get("Opt"))
	choiceField.TI, _ = core.GetInt(dict.Get("TI"))
	choiceField.I, _ = core.GetArray(dict.Get("I"))
	return choiceField, nil
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

func hasFieldKeys(dict *core.PdfObjectDictionary) bool {
	for _, name := range dict.Keys() {
		if _, ok := fieldDictKeys[name.String()]; ok {
			return true
		}
	}
	return false
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

// PdfFieldButton represents a button field which includes push buttons, checkboxes, and radio buttons.
type PdfFieldButton struct {
	*PdfField
	Opt         *core.PdfObjectArray
	buttonImage *Image
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

// IsRadio returns true if the button field represents a radio button, false otherwise.
func (f *PdfFieldButton) IsRadio() bool { return f.GetType() == ButtonTypeRadio }

// SetFillImage attach a model.Image to push button.
func (f *PdfFieldButton) SetFillImage(image *Image) {
	if f.IsPush() {
		f.buttonImage = image
	}
}

// FieldFlag represents form field flags. Some of the flags can apply to all types of fields whereas other
// flags are specific.
type FieldFlag uint32

// FieldFilterFunc represents a PDF field filtering function. If the function
// returns true, the PDF field is kept, otherwise it is discarded.
type FieldFilterFunc func(*PdfField) bool

// Clear clears flag fl from the flag and returns the resulting flag.
func (f FieldFlag) Clear(fl FieldFlag) FieldFlag { return FieldFlag(f.Mask() &^ fl.Mask()) }

// FieldAppearanceGenerator generates appearance stream for a given field.
type FieldAppearanceGenerator interface {
	ContentStreamWrapper
	GenerateAppearanceDict(acroForm *PdfAcroForm, field *PdfField, widget *PdfAnnotationWidget) (*core.PdfObjectDictionary, error)
}

func (f *PdfField) inheritedValue() core.PdfObject {
	var obj core.PdfObject
	f.inherit(func(field *PdfField) bool { obj = field.V; return obj != nil })
	return obj
}

// Mask returns the uin32 bitmask for the specific flag.
func (f FieldFlag) Mask() uint32 { return uint32(f) }

// FieldImageProvider provides fields images for specified fields.
type FieldImageProvider interface {
	FieldImageValues() (map[string]*Image, error)
}

// Has checks if flag fl is set in flag and returns true if so, false otherwise.
func (f FieldFlag) Has(fl FieldFlag) bool { return (f.Mask() & fl.Mask()) > 0 }

var fieldDictKeys = map[string]struct{}{"FT": {}, "Kids": {}, "T": {}, "TU": {}, "TM": {}, "Ff": {}, "V": {}, "DV": {}, "AA": {}, "DA": {}, "Q": {}, "DS": {}, "RV": {}}

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

// NewPdfField returns an initialized PdfField.
func NewPdfField() *PdfField { return &PdfField{primitive: core.MakeIndirectObject(core.MakeDict())} }

// SetContext sets the specific fielddata type, e.g. would be PdfFieldButton for a button field.
func (f *PdfField) SetContext(ctx PdfModel) { f.context = ctx }

// IsCheckbox returns true if the button field represents a checkbox, false otherwise.
func (f *PdfFieldButton) IsCheckbox() bool { return f.GetType() == ButtonTypeCheckbox }

// IsPush returns true if the button field represents a push button, false otherwise.
func (f *PdfFieldButton) IsPush() bool { return f.GetType() == ButtonTypePush }

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

// SetFlag sets the flag for the field.
func (f *PdfField) SetFlag(flag FieldFlag) { f.Ff = core.MakeInteger(int64(flag)) }

// PdfFieldChoice represents a choice field which includes scrollable list boxes and combo boxes.
type PdfFieldChoice struct {
	*PdfField
	Opt *core.PdfObjectArray
	TI  *core.PdfObjectInteger
	I   *core.PdfObjectArray
}

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
