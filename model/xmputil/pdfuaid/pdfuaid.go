// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package pdfuaid

import (
	"fmt"

	"github.com/OpenMellon/go-xmp/xmp"
	"github.com/OpenMellon/openpdf/model/xmputil/pdfaextension"
)

// NewModel creates a new model.
func NewModel(name string) xmp.Model { return &Model{} }

func init() {
	xmp.Register(Namespace, xmp.XmpMetadata)
	pdfaextension.RegisterSchema(Namespace, &Schema)
}

var Schema = pdfaextension.Schema{NamespaceURI: Namespace.URI, Prefix: Namespace.Name, Schema: "PDF/UA ID Schema", Property: []pdfaextension.Property{{Category: pdfaextension.PropertyCategoryInternal, Description: "Part of ISO 14289 standard", Name: "part", ValueType: pdfaextension.ValueTypeNameInteger}, {Category: pdfaextension.PropertyCategoryInternal, Description: "Amendment of ISO 14289-1 standard", Name: "amd", ValueType: pdfaextension.ValueTypeNameText}, {Category: pdfaextension.PropertyCategoryInternal, Description: "Corrigendum of ISO 14289-1 standard", Name: "corr", ValueType: pdfaextension.ValueTypeNameText}, {Category: pdfaextension.PropertyCategoryInternal, Description: "Revision year of ISO 14289-2 standard", Name: "rev", ValueType: pdfaextension.ValueTypeNameInteger}}, ValueType: nil}

// Model is the XMP model for the PDF/UA identification metadata.
type Model struct {
	Part int    `xmp:"pdfuaid:part"`
	Amd  string `xmp:"pdfuaid:amd"`
	Corr string `xmp:"pdfuaid:corr"`
	Rev  int    `xmp:"pdfuaid:rev"`
}

var _ xmp.Model = (*Model)(nil)

// Namespaces implements xmp.Model interface.
func (m *Model) Namespaces() xmp.NamespaceList { return xmp.NamespaceList{Namespace} }

// SyncFromXMP implements xmp.Model interface.
func (m *Model) SyncFromXMP(d *xmp.Document) error { return nil }

// SetTag implements xmp.Model interface.
func (m *Model) SetTag(tag, value string) error {
	if err := xmp.SetNativeField(m, tag, value); err != nil {
		return fmt.Errorf("%s: %v", Namespace.GetName(), err)
	}
	return nil
}

// SyncToXMP implements xmp.Model interface.
func (m *Model) SyncToXMP(d *xmp.Document) error { return nil }

// MakeModel gets or creates a new model for the PDF/UA ID namespace.
func MakeModel(d *xmp.Document) (*Model, error) {
	model, err := d.MakeModel(Namespace)
	if err != nil {
		return nil, err
	}
	return model.(*Model), nil
}

// SyncModel implements xmp.Model interface.
func (m *Model) SyncModel(d *xmp.Document) error { return nil }

// Can implements xmp.Model interface.
func (m *Model) Can(nsName string) bool { return Namespace.GetName() == nsName }

var Namespace = xmp.NewNamespace("pdfuaid", "http://www.aiim.org/pdfua/ns/id/", NewModel)

// GetTag implements xmp.Model interface.
func (m *Model) GetTag(tag string) (string, error) {
	value, err := xmp.GetNativeField(m, tag)
	if err != nil {
		return "", fmt.Errorf("%s: %v", Namespace.GetName(), err)
	}
	return value, nil
}

// CanTag implements xmp.Model interface.
func (m *Model) CanTag(tag string) bool { _, err := xmp.GetNativeField(m, tag); return err == nil }
