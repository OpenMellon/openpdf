// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package pdfaid

import (
	"fmt"

	"github.com/OpenMellon/go-xmp/xmp"
	"github.com/OpenMellon/openpdf/model/xmputil/pdfaextension"
)

// Namespaces implements xmp.Model interface.
func (m *Model) Namespaces() xmp.NamespaceList { return xmp.NamespaceList{Namespace} }

func init() {
	xmp.Register(Namespace, xmp.XmpMetadata)
	pdfaextension.RegisterSchema(Namespace, &Schema)
}

// SyncFromXMP implements xmp.Model interface.
func (m *Model) SyncFromXMP(d *xmp.Document) error { return nil }

// Can implements xmp.Model interface.
func (m *Model) Can(nsName string) bool { return Namespace.GetName() == nsName }

// MakeModel gets or create sa new model for PDF/A ID namespace.
func MakeModel(d *xmp.Document) (*Model, error) {
	model, err := d.MakeModel(Namespace)
	if err != nil {
		return nil, err
	}
	return model.(*Model), nil
}

var Namespace = xmp.NewNamespace("pdfaid", "http://www.aiim.org/pdfa/ns/id/", NewModel)

// SetTag implements xmp.Model interface.
func (m *Model) SetTag(tag, value string) error {
	if err := xmp.SetNativeField(m, tag, value); err != nil {
		return fmt.Errorf("%s: %v", Namespace.GetName(), err)
	}
	return nil
}

var _ xmp.Model = (*Model)(nil)

// Model is the XMP model for the PdfA metadata.
type Model struct {
	Part        int    `xmp:"pdfaid:part"`
	Conformance string `xmp:"pdfaid:conformance"`
	Rev         int    `xmp:"pdfaid:rev"`
}

var Schema = pdfaextension.Schema{NamespaceURI: Namespace.URI, Prefix: Namespace.Name, Schema: "PDF/A ID Schema", Property: []pdfaextension.Property{{Category: pdfaextension.PropertyCategoryInternal, Description: "Part of PDF/A standard", Name: "part", ValueType: pdfaextension.ValueTypeNameInteger}, {Category: pdfaextension.PropertyCategoryInternal, Description: "Amendment of PDF/A standard", Name: "amd", ValueType: pdfaextension.ValueTypeNameText}, {Category: pdfaextension.PropertyCategoryInternal, Description: "Conformance level of PDF/A standard", Name: "conformance", ValueType: pdfaextension.ValueTypeNameText}}, ValueType: nil}

// NewModel creates a new model.
func NewModel(name string) xmp.Model { return &Model{} }

// SyncModel implements xmp.Model interface.
func (m *Model) SyncModel(d *xmp.Document) error { return nil }

// SyncToXMP implements xmp.Model interface.
func (m *Model) SyncToXMP(d *xmp.Document) error { return nil }

// CanTag implements xmp.Model interface.
func (m *Model) CanTag(tag string) bool { _, err := xmp.GetNativeField(m, tag); return err == nil }

// GetTag implements xmp.Model interface.
func (m *Model) GetTag(tag string) (string, error) {
	value, err := xmp.GetNativeField(m, tag)
	if err != nil {
		return "", fmt.Errorf("%s: %v", Namespace.GetName(), err)
	}
	return value, nil
}
