package model

import (
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// AcroFormRepairOptions contains options for rebuilding the AcroForm.
type AcroFormRepairOptions struct{}

// PdfAcroForm represents the AcroForm dictionary used for representation of form data in PDF.
type PdfAcroForm struct {
	Fields          *[]*PdfField
	NeedAppearances *core.PdfObjectBool
	SigFlags        *core.PdfObjectInteger
	CO              *core.PdfObjectArray
	DR              *PdfPageResources
	DA              *core.PdfObjectString
	Q               *core.PdfObjectInteger
	XFA             core.PdfObject

	// ADBEEchoSign extra objects from Adobe Acrobat, causing signature invalid if not exists.
	ADBEEchoSign core.PdfObject
	primitive    *core.PdfIndirectObject
	direct       bool
}

// NewPdfAcroForm returns a new PdfAcroForm with an initialized container (indirect object).
func NewPdfAcroForm() *PdfAcroForm {
	return &PdfAcroForm{Fields: &[]*PdfField{}, primitive: core.MakeIndirectObject(core.MakeDict())}
}

func (a *PdfAcroForm) filteredFields(filterFunc FieldFilterFunc, ok bool) []*PdfField {
	if a == nil {
		return nil
	}
	return filterFieldsRecursive(a.Fields, filterFunc, ok)
}

// AllFields returns a flattened list of all fields in the form.
func (a *PdfAcroForm) AllFields() []*PdfField {
	if a == nil {
		return nil
	}
	var fields []*PdfField
	if a.Fields != nil {
		for _, field := range *a.Fields {
			fields = append(fields, flattenFieldTree(field)...)
		}
	}
	return fields
}

func (a *PdfAcroForm) fillImageWithAppearance(fieldImageProvider FieldImageProvider, appGen FieldAppearanceGenerator) error {
	if a == nil {
		return nil
	}
	fieldImageValues, err := fieldImageProvider.FieldImageValues()
	if err != nil {
		return err
	}
	for _, allField := range a.AllFields() {
		partialName := allField.PartialName()
		var img *Image
		var ok bool
		if fullName, err := allField.FullName(); err == nil {
			img, ok = fieldImageValues[fullName]
		}
		if !ok {
			img, ok = fieldImageValues[partialName]
		}
		if !ok {
			common.Log.Debug("WARN: form field %s not found in the provider. Skipping.", partialName)
			continue
		}
		switch v := allField.GetContext().(type) {
		case *PdfFieldButton:
			if v.IsPush() {
				v.SetFillImage(img)
			}
		}
		if appGen == nil {
			continue
		}
		for _, annotation := range allField.Annotations {
			generateAppearanceDict, err := appGen.GenerateAppearanceDict(a, allField, annotation)
			if err != nil {
				return err
			}
			annotation.AP = generateAppearanceDict
			annotation.ToPdfObject()
		}
	}
	return nil
}

// ToPdfObject converts PdfAcroForm to a PdfObject, i.e. an indirect object containing the
// AcroForm dictionary.
func (a *PdfAcroForm) ToPdfObject() core.PdfObject {
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	if a.Fields != nil {
		arr := core.PdfObjectArray{}
		for _, field := range *a.Fields {
			model := field.GetContext()
			if model != nil {
				arr.Append(model.ToPdfObject())
			} else {
				arr.Append(field.ToPdfObject())
			}
		}
		dict.Set("Fields", &arr)
	}
	if a.NeedAppearances != nil {
		dict.Set("NeedAppearances", a.NeedAppearances)
	} else {
		if needAppearances := dict.Get("NeedAppearances"); needAppearances != nil {
			dict.Remove("NeedAppearances")
		}
	}
	if a.SigFlags != nil {
		dict.Set("SigFlags", a.SigFlags)
	}
	if a.CO != nil {
		dict.Set("CO", a.CO)
	}
	if a.DR != nil {
		dict.Set("DR", a.DR.ToPdfObject())
	}
	if a.DA != nil {
		dict.Set("DA", a.DA)
	}
	if a.Q != nil {
		dict.Set("Q", a.Q)
	}
	if a.XFA != nil {
		dict.Set("XFA", a.XFA)
	}
	if a.ADBEEchoSign != nil {
		dict.Set("ADBE_EchoSign", a.ADBEEchoSign)
	}
	return indirect
}

func (a *PdfAcroForm) fill(provider FieldValueProvider, appGen FieldAppearanceGenerator) error {
	if a == nil {
		return nil
	}
	fieldValues, err := provider.FieldValues()
	if err != nil {
		return err
	}
	for _, allField := range a.AllFields() {
		partialName := allField.PartialName()
		var obj core.PdfObject
		var ok bool
		if fullName, err := allField.FullName(); err == nil {
			obj, ok = fieldValues[fullName]
		}
		if !ok {
			obj, ok = fieldValues[partialName]
		}
		if !ok {
			common.Log.Debug("WARN: form field %s not found in the provider. Skipping.", partialName)
			continue
		}
		if err := setFieldValue(allField, obj); err != nil {
			return err
		}
		if appGen == nil {
			continue
		}
		for _, widget := range fieldWidgets(allField) {
			generateAppearanceDict, err := appGen.GenerateAppearanceDict(a, allField, widget)
			if err != nil {
				return err
			}
			widget.AP = generateAppearanceDict
			widget.ToPdfObject()
		}
	}
	return nil
}

// FillWithAppearance populates `form` with values provided by `provider`.
// If not nil, `appGen` is used to generate appearance dictionaries for the
// field annotations, based on the specified settings. Otherwise, appearance
// generation is skipped.
// e.g.: appGen := annotator.FieldAppearance{OnlyIfMissing: true, RegenerateTextFields: true}
// NOTE: In next major version this functionality will be part of Fill. (v4)
func (a *PdfAcroForm) FillWithAppearance(provider FieldValueProvider, appGen FieldAppearanceGenerator) error {
	err := a.fill(provider, appGen)
	if err != nil {
		return err
	}
	if _, ok := provider.(FieldImageProvider); ok {
		err = a.fillImageWithAppearance(provider.(FieldImageProvider), appGen)
	}
	return err
}

// Fill populates `form` with values provided by `provider`.
func (a *PdfAcroForm) Fill(provider FieldValueProvider) error { return a.fill(provider, nil) }

// AllFieldValues returns the value of every terminal (leaf) field of the
// form, in document order. Values are returned verbatim: text as the stored
// PDF text string, check box and radio button states as names, multi-select
// choice values as arrays. A field without a value of its own inherits the
// value of its nearest ancestor that has one. Non-terminal fields are
// omitted, as are fields whose fully qualified name cannot be determined.
// One entry is returned per name: widget kids that carry field entries load
// as unnamed child fields and share their parent's name, and the first of
// them with a value supplies it.
func (a *PdfAcroForm) AllFieldValues() []FieldValue {
	var fieldValues []FieldValue
	numByKey := map[string]int{}
	for _, allField := range a.AllFields() {
		if !allField.IsTerminal() {
			continue
		}
		fullName, err := allField.FullName()
		if err != nil || fullName == "" {
			common.Log.Debug("WARN: skipping form field %q with unresolvable name: %v", fullName, err)
			continue
		}
		obj := core.TraceToDirectObject(allField.inheritedValue())
		if n, ok := numByKey[fullName]; ok {
			if fieldValues[n].Value == nil && obj != nil {
				fieldValues[n].Value = obj
				fieldValues[n].Field = allField
			}
			continue
		}
		numByKey[fullName] = len(fieldValues)
		fieldValues = append(fieldValues, FieldValue{Name: fullName, Value: obj, Field: allField})
	}
	return fieldValues
}

func (a *PdfAcroForm) signatureFields() []*PdfFieldSignature {
	var signatureFields []*PdfFieldSignature
	for _, allField := range a.AllFields() {
		switch v := allField.GetContext().(type) {
		case *PdfFieldSignature:
			signatureField := v
			signatureFields = append(signatureFields, signatureField)
		}
	}
	return signatureFields
}

// GetContainingPdfObject returns the container of the PdfAcroForm (indirect object).
func (a *PdfAcroForm) GetContainingPdfObject() core.PdfObject { return a.primitive }
