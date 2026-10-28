// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package pdfaextension

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/unidoc/go-xmp/models/dc"
	"github.com/unidoc/go-xmp/models/pdf"
	xmpbase "github.com/unidoc/go-xmp/models/xmp_base"
	xmpmm "github.com/unidoc/go-xmp/models/xmp_mm"
	"github.com/unidoc/go-xmp/xmp"
)

// Namespaces implements xmp.Model interface.
func (mdl *Model) Namespaces() xmp.NamespaceList { return xmp.NamespaceList{Namespace} }

func init() {
	xmp.Register(Namespace, xmp.XmpMetadata)
	xmp.Register(SchemaNS)
	xmp.Register(PropertyNS)
	xmp.Register(ValueTypeNS)
	xmp.Register(FieldNS)
}

var FieldVersionSchema = Schema{NamespaceURI: "http://ns.adobe.com/xap/1.0/sType/Version#", Prefix: "stVer", Schema: "Basic value type Version", Property: []Property{{Category: PropertyCategoryInternal, Description: "Comments concerning what was changed", Name: "comments", ValueType: ValueTypeNameText}, {Category: PropertyCategoryInternal, Description: "High level, formal description of what operation the user performed.", Name: "event", ValueType: ValueTypeResourceEvent}, {Category: PropertyCategoryInternal, Description: "The date on which this version was checked in.", Name: "modifyDate", ValueType: ValueTypeNameDate}, {Category: PropertyCategoryInternal, Description: "The person who modified this version.", Name: "modifier", ValueType: ValueTypeNameProperName}, {Category: PropertyCategoryInternal, Description: "The new version number.", Name: "version", ValueType: ValueTypeNameText}}, ValueType: nil}

// IsZero checks if the resources list has no entries.
func (s Schemas) IsZero() bool { return len(s) == 0 }

const (
	PropertyCategoryUndefined PropertyCategory = iota
	PropertyCategoryInternal
	PropertyCategoryExternal
)

// FieldValueType is a schema that describes a field in a structured type.
type FieldValueType struct {
	Name        string        `xmp:"pdfaField:description"`
	Description string        `xmp:"pdfaField:name"`
	ValueType   ValueTypeName `xmp:"pdfaField:valueType"`
}

func (s Schemas) Typ() xmp.ArrayType { return xmp.ArrayTypeUnordered }

// Typ gets the array type of properties.
func (p Properties) Typ() xmp.ArrayType { return xmp.ArrayTypeOrdered }

// UnmarshalXMP implements xmp.Unmarshaler interface.
func (s *Schemas) UnmarshalXMP(d *xmp.Decoder, node *xmp.Node, m xmp.Model) error {
	return xmp.UnmarshalArray(d, node, s.Typ(), s)
}

// ClosedChoiceValueTypeName gets the closed choice of provided value type name.
func ClosedChoiceValueTypeName(vt ValueTypeName) ValueTypeName { return "Closed Choice of " + vt }

// GetSchema for provided namespace.
func GetSchema(namespaceURI string) (*Schema, bool) {
	schemaRegistryMu.RLock()
	defer schemaRegistryMu.RUnlock()
	schema, ok := schemaRegistry[namespaceURI]
	return schema, ok
}

// Typ gets the array type of the value types.
func (vt ValueTypes) Typ() xmp.ArrayType { return xmp.ArrayTypeOrdered }

// Model is the pdfa extension metadata model.
type Model struct {
	Schemas Schemas `xmp:"pdfaExtension:schemas"`
}

// Properties is a list of properties.
type Properties []Property

// CanTag implements xmp.Model interface.
func (mdl *Model) CanTag(tag string) bool {
	_, err := xmp.GetNativeField(mdl, tag)
	return err == nil
}

// SyncFromXMP implements xmp.Model interface.
func (mdl *Model) SyncFromXMP(d *xmp.Document) error { return nil }

var (
	schemaRegistry   = map[string]*Schema{}
	schemaRegistryMu sync.RWMutex
)

func init() {
	schemaRegistry = map[string]*Schema{xmpmm.NsXmpMM.URI: &XmpMediaManagementSchema, "xmpidq": &XmpIDQualSchema, pdf.NsPDF.URI: &PdfSchema, "stEvt": &FieldResourceEventSchema, "stVer": &FieldVersionSchema}
}

// MarshalXMP implements xmp.Marshaler interface.
func (vt ValueTypes) MarshalXMP(e *xmp.Encoder, node *xmp.Node, m xmp.Model) error {
	return xmp.MarshalArray(e, node, vt.Typ(), vt)
}

// UnmarshalXMP implements xmp.Unmarshaler interface.
func (vt *ValueTypes) UnmarshalXMP(d *xmp.Decoder, node *xmp.Node, m xmp.Model) error {
	return xmp.UnmarshalArray(d, node, vt.Typ(), vt)
}

// AltOfValueTypeName gets the ValueTypeName of the alt of given value type names.
func AltOfValueTypeName(vt ValueTypeName) ValueTypeName { return "alt " + vt }

// PropertyCategory is the property category enumerator.
type PropertyCategory int

// UnmarshalText implements encoding.TextUnmarshaler interface.
func (c *PropertyCategory) UnmarshalText(in []byte) error {
	switch string(in) {
	case "internal":
		*c = PropertyCategoryInternal
	case "external":
		*c = PropertyCategoryExternal
	default:
		*c = PropertyCategoryUndefined
	}
	return nil
}

// GetTag implements xmp.Model interface.
func (mdl *Model) GetTag(tag string) (string, error) {
	val, err := xmp.GetNativeField(mdl, tag)
	if err != nil {
		return "", fmt.Errorf("%s: %v", Namespace.GetName(), err)
	}
	return val, nil
}

// FillModel fills in the document XMP model.
func FillModel(d *xmp.Document, extModel *Model) error {
	var namespaces []*xmp.Namespace
	for _, ns := range d.Namespaces() {
		namespaces = append(namespaces, ns)
	}
	sort.Slice(namespaces, func(i, j int) bool { return namespaces[i].Name < namespaces[j].Name })
	for _, ns := range namespaces {
		switch ns {
		case Namespace, SchemaNS, PropertyNS, ValueTypeNS, FieldNS, xmpbase.NsXmp, dc.NsDc:
			continue
		default:
			registered, ok := GetSchema(ns.URI)
			if !ok {
				continue
			}
			model := d.FindModel(ns)
			filtered := *registered
			filtered.Property = Properties{}
			for _, prop := range registered.Property {
				used := isPropertySet(model, ns, prop)
				if used {
					filtered.Property = append(filtered.Property, prop)
				}
			}
			if len(filtered.Property) == 0 {
				continue
			}
			var replaced bool
			for i, existing := range extModel.Schemas {
				if existing.Schema == filtered.Schema {
					replaced = true
					extModel.Schemas[i] = filtered
					break
				}
			}
			if !replaced {
				extModel.Schemas = append(extModel.Schemas, filtered)
			}
		}
	}
	return nil
}

// MarshalText implements encoding.TextMarshaler interface.
func (c PropertyCategory) MarshalText() ([]byte, error) {
	switch c {
	case PropertyCategoryInternal:
		return []byte("internal"), nil
	case PropertyCategoryExternal:
		return []byte("external"), nil
	case PropertyCategoryUndefined:
		return []byte(""), nil
	default:
		return nil, fmt.Errorf("undefined property category value: %v", c)
	}
}

var (
	Namespace   = xmp.NewNamespace("pdfaExtension", "http://www.aiim.org/pdfa/ns/extension/", NewModel)
	SchemaNS    = xmp.NewNamespace("pdfaSchema", "http://www.aiim.org/pdfa/ns/schema#", nil)
	PropertyNS  = xmp.NewNamespace("pdfaProperty", "http://www.aiim.org/pdfa/ns/property#", nil)
	ValueTypeNS = xmp.NewNamespace("pdfaType", "http://www.aiim.org/pdfa/ns/type#", nil)
	FieldNS     = xmp.NewNamespace("pdfaField", "http://www.aiim.org/pdfa/ns/field#", nil)
)

// NewModel creates a new pdfAExtension model.
func NewModel(name string) xmp.Model { return &Model{} }

// MarshalXMP implements xmp.Marshaler interface.
func (p Properties) MarshalXMP(e *xmp.Encoder, node *xmp.Node, m xmp.Model) error {
	return xmp.MarshalArray(e, node, p.Typ(), p)
}

// Schemas is the array of xmp metadata extension resources.
type Schemas []Schema

// ValueTypeName is the name of the value type.
type ValueTypeName string

var PdfSchema = Schema{NamespaceURI: "http://ns.adobe.com/pdf/1.3/", Prefix: "pdf", Schema: "Adobe PDF Schema", Property: Properties{{Category: PropertyCategoryInternal, Description: "Keywords", Name: "Keywords", ValueType: ValueTypeNameText}, {Category: PropertyCategoryInternal, Description: "The PDF file version (for example: 1.0, 1.3, and so on).", Name: "PDFVersion", ValueType: ValueTypeNameText}, {Category: PropertyCategoryInternal, Description: "The name of the tool that created the PDF document.", Name: "Producer", ValueType: ValueTypeNameAgentName}, {Category: PropertyCategoryInternal, Description: "Indicates that this is a rights-managed resource..", Name: "Marked", ValueType: ValueTypeNameBoolean}}}

// Schema is the pdfa extension schema.
type Schema struct {
	// NamespaceURI is schema namespace URI.
	NamespaceURI string `xmp:"pdfaSchema:namespaceURI"`

	// Prefix is preferred schema namespace prefix.
	Prefix string `xmp:"pdfaSchema:prefix"`

	// Schema is optional description.
	Schema string `xmp:"pdfaSchema:schema"`

	// Property is description of schema properties.
	Property Properties `xmp:"pdfaSchema:property"`

	// ValueType is description of schema-specific value types.
	ValueType ValueTypes `xmp:"pdfaSchema:valueType"`
}

func isPropertySet(model xmp.Model, ns *xmp.Namespace, prop Property) bool {
	v := reflect.ValueOf(model).Elem()
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("xmp")
		if tag == "" {
			continue
		}
		if !strings.HasPrefix(tag, ns.Name) {
			continue
		}
		idx := strings.IndexRune(tag, ':')
		if idx == -1 {
			continue
		}
		name := tag[idx+1:]
		if name == prop.Name {
			fv := v.Field(i)
			return !fv.IsZero()
		}
	}
	return false
}

// SyncModel implements xmp.Model interface.
func (mdl *Model) SyncModel(d *xmp.Document) error { return nil }

// MakeModel creates or gets a model from document.
func MakeModel(d *xmp.Document) (*Model, error) {
	m, err := d.MakeModel(Namespace)
	if err != nil {
		return nil, err
	}
	return m.(*Model), nil
}

// RegisterSchema registers schema extension definition.
func RegisterSchema(ns *xmp.Namespace, schema *Schema) {
	schemaRegistryMu.Lock()
	defer schemaRegistryMu.Unlock()
	schemaRegistry[ns.URI] = schema
}

// ValueTypes is the slice of value types described by a schema
// (pdfaSchema:valueType). Each entry is a pdfaType-namespaced value type, not a
// pdfaField-namespaced field (those live in ValueType.Field).
type ValueTypes []ValueType

// SyncToXMP implements xmp.Model interface.
func (mdl *Model) SyncToXMP(d *xmp.Document) error { return nil }

// BagOfValueTypeName gets the ValueTypeName of the bag of provided value type names.
func BagOfValueTypeName(vt ValueTypeName) ValueTypeName { return "bag " + vt }

// SetTag implements xmp.Model interface.
func (mdl *Model) SetTag(tag, value string) error {
	if err := xmp.SetNativeField(mdl, tag, value); err != nil {
		return fmt.Errorf("%s: %v", Namespace.GetName(), err)
	}
	return nil
}

var FieldResourceEventSchema = Schema{NamespaceURI: "http://ns.adobe.com/xap/1.0/sType/ResourceEvent#", Prefix: "stEvt", Schema: "Definition of basic value type ResourceEvent", Property: []Property{{Category: PropertyCategoryInternal, Description: "The action that occurred. Defined values are: converted, copied, created, cropped, edited, fil-tered, formatted, version_updated, printed, published, managed, produced, resized.New values should be verbs in the past tense.", Name: "action", ValueType: "Open Choice"}, {Category: PropertyCategoryInternal, Description: "The instance ID of the modified resource", Name: "instanceID", ValueType: ValueTypeNameURI}, {Category: PropertyCategoryInternal, Description: "Additional description of the action", Name: "parameters", ValueType: ValueTypeNameText}, {Category: PropertyCategoryInternal, Description: "The software agent that performed the action", Name: "softwareAgent", ValueType: ValueTypeNameAgentName}, {Category: PropertyCategoryInternal, Description: "Optional timestamp of when the action occurred", Name: "when", ValueType: ValueTypeNameDate}}, ValueType: nil}

// Can implements xmp.Model interface.
func (mdl *Model) Can(nsName string) bool { return Namespace.GetName() == nsName }

const (
	ValueTypeNameBoolean        ValueTypeName = "Boolean"
	ValueTypeNameDate           ValueTypeName = "Date"
	ValueTypeNameInteger        ValueTypeName = "Integer"
	ValueTypeNameReal           ValueTypeName = "Real"
	ValueTypeNameText           ValueTypeName = "Text"
	ValueTypeNameAgentName      ValueTypeName = "AgentName"
	ValueTypeNameProperName     ValueTypeName = "ProperName"
	ValueTypeNameXPath          ValueTypeName = "XPath"
	ValueTypeNameGUID           ValueTypeName = "GUID"
	ValueTypeNameLocale         ValueTypeName = "Locale"
	ValueTypeNameMIMEType       ValueTypeName = "MIMEType"
	ValueTypeNameRenditionClass ValueTypeName = "RenditionClass"
	ValueTypeNameResourceRef    ValueTypeName = "ResourceRef"
	ValueTypeNameURL            ValueTypeName = "URL"
	ValueTypeNameURI            ValueTypeName = "URI"
	ValueTypeNameVersion        ValueTypeName = "Version"
)

// SetSchema sets the schema into given model.
func (mdl *Model) SetSchema(namespaceURI string, s Schema) {
	for i := 0; i < len(mdl.Schemas); i++ {
		if mdl.Schemas[i].NamespaceURI == namespaceURI {
			mdl.Schemas[i] = s
			return
		}
	}
	mdl.Schemas = append(mdl.Schemas, s)
}

var XmpMediaManagementSchema = Schema{NamespaceURI: "http://ns.adobe.com/xap/1.0/mm/", Prefix: "xmpMM", Schema: "XMP Media Management", Property: []Property{{Category: PropertyCategoryInternal, Description: "A reference to the original document from which this one is derived.", Name: "DerivedFrom", ValueType: ValueTypeNameResourceRef}, {Category: PropertyCategoryInternal, Description: "The common identifier for all versions and renditions of a resource", Name: "DocumentID", ValueType: ValueTypeNameURI}, {Category: PropertyCategoryInternal, Description: "UUID based identifier for specific incarnation of a document", Name: "InstanceID", ValueType: ValueTypeNameURI}, {Category: PropertyCategoryInternal, Description: "The common identifier for all versions and renditions of a document", Name: "OriginalDocumentID", ValueType: ValueTypeNameURI}, {Category: PropertyCategoryInternal, Description: "The rendition class name for this resource", Name: "RenditionClass", ValueType: ValueTypeNameRenditionClass}, {Category: PropertyCategoryInternal, Description: "Can be used to provide additional rendition parameters that are too complex or verbose to encode in", Name: "RenditionParams", ValueType: ValueTypeNameText}, {Category: PropertyCategoryInternal, Description: "The document version identifier for this resource.", Name: "VersionID", ValueType: ValueTypeNameText}, {Category: PropertyCategoryExternal, Description: "The version history associated with this resource", Name: "Versions", ValueType: SeqOfValueTypeName(ValueTypeNameVersion)}, {Category: PropertyCategoryInternal, Description: "The referenced resource's manager", Name: "Manager", ValueType: ValueTypeNameAgentName}, {Category: PropertyCategoryInternal, Description: "The referenced resource's manager variant.", Name: "ManagerVariant", ValueType: ValueTypeNameText}, {Category: PropertyCategoryInternal, Description: "The referenced resource's manager variant.", Name: "ManagerVariant", ValueType: ValueTypeNameText}}}

// Property is a schema that describes single property.
type Property struct {
	// Category is the property category.
	Category PropertyCategory `xmp:"pdfaProperty:category"`

	// Description of the property.
	Description string `xmp:"pdfaProperty:description"`

	// Name is a property name.
	Name string `xmp:"pdfaProperty:name"`

	// ValueType is the property value type.
	ValueType ValueTypeName `xmp:"pdfaProperty:valueType"`
}

var XmpIDQualSchema = Schema{NamespaceURI: "http://ns.adobe.com/xmp/Identifier/qual/1.0/", Prefix: "xmpidq", Schema: "XMP xmpidq qualifier", Property: []Property{{Category: PropertyCategoryInternal, Name: "Scheme", Description: "A qualifier providing the n ame of the formal identification scheme used for an item in the xmp:Identifier array.", ValueType: ValueTypeNameText}}, ValueType: nil}

// SeqOfValueTypeName gets a value type name of a sequence of input value type names.
func SeqOfValueTypeName(vt ValueTypeName) ValueTypeName { return "seq " + vt }

// MarshalXMP implements xmp.Marshaler interface.
func (s Schemas) MarshalXMP(e *xmp.Encoder, node *xmp.Node, m xmp.Model) error {
	return xmp.MarshalArray(e, node, s.Typ(), s)
}

// ValueType is the pdfa extension value type schema.
type ValueType struct {
	Description  string           `xmp:"pdfaType:description"`
	Field        []FieldValueType `xmp:"pdfaType:field"`
	NamespaceURI string           `xmp:"pdfaType:namespaceURI"`
	Prefix       string           `xmp:"pdfaType:prefix"`
	Type         string           `xmp:"pdfaType:type"`
}

const (
	ValueTypeResourceEvent ValueTypeName = "ResourceEvent"
)

// UnmarshalXMP implements xmp.Unmarshaler interface.
func (p *Properties) UnmarshalXMP(d *xmp.Decoder, node *xmp.Node, m xmp.Model) error {
	return xmp.UnmarshalArray(d, node, p.Typ(), p)
}
