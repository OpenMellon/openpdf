package model

import (
	_ "image/gif"

	"github.com/OpenMellon/openpdf/core"
)

var (
	StructureTypeSpan               StructureType = "Span"
	StructureTypeQuote              StructureType = "Quote"
	StructureTypeNote               StructureType = "Note"
	StructureTypeReference          StructureType = "Reference"
	StructureTypeBibliography       StructureType = "BibEntry"
	StructureTypeCode               StructureType = "Code"
	StructureTypeLink               StructureType = "Link"
	StructureTypeAnnot              StructureType = "Annot"
	StructureTypeRuby               StructureType = "Ruby"
	StructureTypeWarichu            StructureType = "Warichu"
	StructureTypeRubyBase           StructureType = "RB"
	StructureTypeRubyText           StructureType = "RT"
	StructureTypeRubyPunctuation    StructureType = "RP"
	StructureTypeWarichuText        StructureType = "WT"
	StructureTypeWarichuPunctuation StructureType = "WP"
	StructureTypeFigure             StructureType = "Figure"
	StructureTypeFormula            StructureType = "Formula"
	StructureTypeForm               StructureType = "Form"
	StructureTypeDocumentFragment   StructureType = "DocumentFragment"
	StructureTypeAside              StructureType = "Aside"
	StructureTypeTitle              StructureType = "Title"
	StructureTypeFENote             StructureType = "FENote"
	StructureTypeSub                StructureType = "Sub"
	StructureTypeEm                 StructureType = "Em"
	StructureTypeStrong             StructureType = "Strong"
)

// PdfStructureNamespace represents a namespace dictionary as defined in
// ISO 32000-2 14.7.4.2, table 356.
type PdfStructureNamespace struct {
	uri             string
	schema          core.PdfObject
	roleMapNS       *core.PdfObjectDictionary
	indirect        *core.PdfIndirectObject
	roleMapIndirect *core.PdfIndirectObject
}

// SetSchema sets the namespace schema, a reference to a file containing a
// schema for the namespace (ISO 32000-2 14.7.4.2, table 356).
func (ns *PdfStructureNamespace) SetSchema(schema core.PdfObject) { ns.schema = schema }

// StructureType represents a standard structure type in a tagged PDF document.
type StructureType string

// ToPdfObject converts the namespace to a PDF object. The returned object is
// stable: repeated calls return the same indirect object, so multiple
// structure elements may reference one namespace.
func (ns *PdfStructureNamespace) ToPdfObject() core.PdfObject {
	if ns.indirect == nil {
		ns.indirect = core.MakeIndirectObject(core.MakeDict())
	}
	dict := ns.indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.Set("Type", core.MakeName("Namespace"))
	dict.Set("NS", core.MakeString(ns.uri))
	if ns.schema != nil {
		dict.Set("Schema", ns.schema)
	} else {
		dict.Remove("Schema")
	}
	if ns.roleMapNS != nil {
		if ns.roleMapIndirect == nil {
			ns.roleMapIndirect = core.MakeIndirectObject(ns.roleMapNS)
		} else {
			ns.roleMapIndirect.PdfObject = ns.roleMapNS
		}
		dict.Set("RoleMapNS", ns.roleMapIndirect)
	} else {
		dict.Remove("RoleMapNS")
	}
	return ns.indirect
}

// SetRoleMapNS sets the namespace role map.
// Keys are structure type names in this namespace. Values are either a name
// (target type in the default standard namespace) or a two element array of
// [name, namespace dictionary reference].
func (ns *PdfStructureNamespace) SetRoleMapNS(roleMap *core.PdfObjectDictionary) {
	ns.roleMapNS = roleMap
}

func newPdfStructureNamespaceFromObject(obj core.PdfObject) (*PdfStructureNamespace, bool) {
	dict, ok := core.GetDict(obj)
	if !ok {
		return nil, false
	}
	ns, ok := core.GetString(dict.Get("NS"))
	if !ok {
		return nil, false
	}
	structureNamespace := &PdfStructureNamespace{uri: ns.Decoded()}
	if indirect, ok := core.GetIndirect(obj); ok {
		structureNamespace.indirect = indirect
	}
	structureNamespace.schema = dict.Get("Schema")
	if roleMapNS, ok := core.GetDict(dict.Get("RoleMapNS")); ok {
		structureNamespace.roleMapNS = roleMapNS
	}
	return structureNamespace, true
}

// NS returns the namespace URI.
func (ns *PdfStructureNamespace) NS() string { return ns.uri }

// NewPdfStructureNamespace creates a new namespace dictionary with the given URI.
func NewPdfStructureNamespace(uri string) *PdfStructureNamespace {
	return &PdfStructureNamespace{uri: uri}
}

const (
	StructureNamespacePDF2   = "http://iso.org/pdf2/ssn"
	StructureNamespacePDF17  = "http://iso.org/pdf/ssn"
	StructureNamespaceMathML = "http://www.w3.org/1998/Math/MathML"
)

var (
	StructureTypeParagraph       StructureType = "P"
	StructureTypeHeader          StructureType = "H"
	StructureTypeHeading1        StructureType = "H1"
	StructureTypeHeading2        StructureType = "H2"
	StructureTypeHeading3        StructureType = "H3"
	StructureTypeHeading4        StructureType = "H4"
	StructureTypeHeading5        StructureType = "H5"
	StructureTypeHeading6        StructureType = "H6"
	StructureTypeList            StructureType = "L"
	StructureTypeListItem        StructureType = "LI"
	StructureTypeLabel           StructureType = "Lbl"
	StructureTypeListBody        StructureType = "LBody"
	StructureTypeTable           StructureType = "Table"
	StructureTypeTableRow        StructureType = "TR"
	StructureTypeTableHeaderCell StructureType = "TH"
	StructureTypeTableData       StructureType = "TD"
	StructureTypeTableHead       StructureType = "THead"
	StructureTypeTableBody       StructureType = "TBody"
	StructureTypeTableFooter     StructureType = "TFoot"
)

// NewStructureTagInfo creates a new StructureTagInfo with default values.
func NewStructureTagInfo() *StructureTagInfo {
	return &StructureTagInfo{Mcid: 0, StructureType: StructureTypeUnknown}
}

// NewComponentKObj builds the component's K dictionary for the given structure
// type, seeding its page number when known so descendants without their own page
// inherit it (required for ParentTree reachability).
func (t *StructureTagInfo) NewComponentKObj(structureType StructureType) *KDict {
	kDictionary := NewKDictionary()
	kDictionary.S = core.MakeName(string(structureType))
	if t.StructPageNumber != nil {
		kDictionary.SetPageNumber(*t.StructPageNumber)
	}
	return kDictionary
}

// Schema returns the namespace schema, a reference to a file containing a
// schema for the namespace (ISO 32000-2 14.7.4.2, table 356). Nil if unset.
func (ns *PdfStructureNamespace) Schema() core.PdfObject { return ns.schema }

var (
	StructureTypeUnknown       StructureType = ""
	StructureTypeDocument      StructureType = "Document"
	StructureTypePart          StructureType = "Part"
	StructureTypeArticle       StructureType = "Art"
	StructureTypeSection       StructureType = "Sect"
	StructureTypeDivision      StructureType = "Div"
	StructureTypeBlockQuote    StructureType = "BlockQuote"
	StructureTypeCaption       StructureType = "Caption"
	StructureTypeTOC           StructureType = "TOC"
	StructureTypeTOCI          StructureType = "TOCI"
	StructureTypeIndex         StructureType = "Index"
	StructureTypeNonStructural StructureType = "NonStruct"
	StructureTypePrivate       StructureType = "Private"
)

// StructureTagInfo represents a tagged structure in a PDF document that used by Drawable components.
type StructureTagInfo struct {
	// Marked Content ID, if applicable.
	Mcid int64

	// Structure type, a name object identifying the nature of the object and its role within the document.
	StructureType StructureType

	// AnnotObjs is used to reference annotation objects in structure elements
	AnnotObjs []core.PdfObject

	// Whether the component that has StructureTagInfo is tagged for accessibility.
	ApplyTag bool

	// Parent K object for the component structure that also act as a parent node.
	ParentKObj *KDict

	// The page number where the structure element for this component is located.
	StructPageNumber *int64

	// K object for the component.
	ComponentKObj *KDict
}

// GenerateKDict generates a K dictionary object from the StructureTagInfo.
func (t *StructureTagInfo) GenerateKDict() *KDict {
	kdict := &KDict{S: core.MakeName(string(t.StructureType))}
	if t.StructPageNumber != nil {
		kdict.SetPageNumber(*t.StructPageNumber)
	}
	if len(t.AnnotObjs) == 0 {
		kdict.K = core.MakeInteger(t.Mcid)
		return kdict
	}
	array := core.MakeArray()
	array.Append(core.MakeInteger(t.Mcid))
	for _, annotObj := range t.AnnotObjs {
		dict := core.MakeDict()
		dict.Set("Type", core.MakeName("OBJR"))
		dict.Set("Obj", annotObj)
		array.Append(dict)
	}
	kdict.K = array
	return kdict
}

// SetObjAttrib adds an object attribute to the structure tag info.
// This is commonly used to associate annotations with structure elements.
func (t *StructureTagInfo) SetObjAttrib(obj core.PdfObject) {
	if t.AnnotObjs == nil {
		t.AnnotObjs = make([]core.PdfObject, 0)
	}
	t.AnnotObjs = append(t.AnnotObjs, obj)
}
