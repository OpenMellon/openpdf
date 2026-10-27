// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package fjson provides support for loading PDF form field data from JSON data/files.
package fjson

import (
	"encoding/json"
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"io"
	"os"
)

// SetImage assign model.Image to a specific field identified by fieldName.
func (data *FieldData) SetImage(fieldName string, img *model.Image, opt []string) error {
	value := fieldValue{Name: fieldName, ImageValue: img, Options: opt}
	data.values = append(data.values, value)
	return nil
}

// LoadFromPDF loads form field data from a PDF. The source must implement
// io.ReaderAt (*os.File, *bytes.Reader, and *io.SectionReader all qualify).
func LoadFromPDF(r io.ReaderAt) (*FieldData, error) {
	reader, err := model.NewPdfReader(r)
	if err != nil {
		return nil, err
	}
	if reader.AcroForm == nil {
		return nil, nil
	}
	var values []fieldValue
	fields := reader.AcroForm.AllFields()
	for _, field := range fields {
		var options []string
		seen := make(map[string]struct{})
		name, err := field.FullName()
		if err != nil {
			return nil, err
		}
		if text, ok := field.V.(*core.PdfObjectString); ok {
			values = append(values, fieldValue{Name: name, Value: text.Decoded()})
			continue
		}
		var value string
		for _, annotation := range field.Annotations {
			appearance, ok := core.GetName(annotation.AS)
			if ok {
				value = appearance.String()
			}
			appearances, ok := core.GetDict(annotation.AP)
			if !ok {
				continue
			}
			normal, _ := core.GetDict(appearances.Get("N"))
			for _, option := range normal.Keys() {
				name := option.String()
				if _, ok := seen[name]; !ok {
					options = append(options, name)
					seen[name] = struct{}{}
				}
			}
			down, _ := core.GetDict(appearances.Get("D"))
			for _, option := range down.Keys() {
				name := option.String()
				if _, ok := seen[name]; !ok {
					options = append(options, name)
					seen[name] = struct{}{}
				}
			}
		}
		entry := fieldValue{Name: name, Value: value, Options: options}
		values = append(values, entry)
	}
	data := FieldData{values: values}
	return &data, nil
}

// LoadFromPDFFile loads form field data from a PDF file.
func LoadFromPDFFile(filePath string) (*FieldData, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return LoadFromPDF(file)
}

// FieldValues implements model.FieldValueProvider interface.
func (data *FieldData) FieldValues() (map[string]core.PdfObject, error) {
	values := make(map[string]core.PdfObject)
	for _, field := range data.values {
		if len(field.Value) > 0 {
			values[field.Name] = core.MakeString(field.Value)
		}
	}
	return values, nil
}

type fieldValue struct {
	Name       string       `json:"name"`
	Value      string       `json:"value"`
	ImageValue *model.Image `json:"-"`

	// Options lists allowed values if present.
	Options []string `json:"options,omitempty"`
}

// FieldData represents form field data loaded from JSON file.
type FieldData struct{ values []fieldValue }

// FieldImageValues implements model.FieldImageProvider interface.
func (data *FieldData) FieldImageValues() (map[string]*model.Image, error) {
	values := make(map[string]*model.Image)
	for _, field := range data.values {
		if field.ImageValue != nil {
			values[field.Name] = field.ImageValue
		}
	}
	return values, nil
}

// JSON returns the field data as a string in JSON format.
func (data FieldData) JSON() (string, error) {
	encoded, err := json.MarshalIndent(data.values, "", "    ")
	return string(encoded), err
}

// LoadFromJSON loads JSON form data from `r`.
func LoadFromJSON(r io.Reader) (*FieldData, error) {
	var data FieldData
	err := json.NewDecoder(r).Decode(&data.values)
	if err != nil {
		return nil, err
	}
	return &data, nil
}

// SetImageFromFile assign image file to a specific field identified by fieldName.
func (data *FieldData) SetImageFromFile(fieldName string, imagePath string, opt []string) error {
	file, err := os.Open(imagePath)
	if err != nil {
		return err
	}
	defer file.Close()
	image, err := model.ImageHandling.Read(file)
	if err != nil {
		common.Log.Error("Error loading image: %s", err)
		return err
	}
	return data.SetImage(fieldName, image, opt)
}

// LoadFromJSONFile loads form field data from a JSON file.
func LoadFromJSONFile(filePath string) (*FieldData, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return LoadFromJSON(file)
}
