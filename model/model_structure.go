package model

import (
	"errors"
	_ "image/gif"
	"sort"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// ArtifactSubtype represents the subtype of an artifact in a PDF document.
type ArtifactSubtype string

var (
	ArtifactSubtypeHeader    ArtifactSubtype = "Header"
	ArtifactSubtypeFooter    ArtifactSubtype = "Footer"
	ArtifactSubtypeWatermark ArtifactSubtype = "Watermark"
)

// ArtifactAttachPosition specifies the position where an artifact is attached to a page.
type ArtifactAttachPosition string

var (
	ArtifactAttachPositionTop    ArtifactAttachPosition = "Top"
	ArtifactAttachPositionBottom ArtifactAttachPosition = "Bottom"
	ArtifactAttachPositionLeft   ArtifactAttachPosition = "Left"
	ArtifactAttachPositionRight  ArtifactAttachPosition = "Right"
)

// ArtifactType represents the type of an artifact in a PDF document.
type ArtifactType string

var (
	ArtifactTypeNone       ArtifactType = ""
	ArtifactTypePagination ArtifactType = "Pagination"
	ArtifactTypeLayout     ArtifactType = "Layout"
	ArtifactTypePage       ArtifactType = "Page"
	ArtifactTypeBackground ArtifactType = "Background"
)

// Artifact represents a PDF artifact, which is a non-visual element
// that provides additional information about the document structure or content.
type Artifact struct {
	// Type of the artifact.
	Type ArtifactType

	// BBox of the artifact.
	//
	// Optional; required for background artifacts.
	//
	// An array of four numbers in default user space units giving the coordinates
	// of the left, bottom, right, and top edges, respectively, of the artifact’s
	// bounding box (the rectangle that completely encloses its visible extent).
	BBox *PdfRectangle

	// Attached positions for the artifact.
	//
	// Optional; pagination and full-page background artifacts only)
	//
	// An array of name objects containing one to four of the names Top, Bottom, Left, and
	// Right, specifying the edges of the page, if any, to which the artifact is
	// logically attached. Page edges shall be defined by the page’s crop box.
	// The ordering of names within the array is immaterial. Including both Left and Right or both Top and Bottom
	// indicates a full-width or full-height artifact, respectively.
	//
	// Use of this entry for background artifacts shall be limited to full-page artifacts.
	// Background artifacts that are not full-page take their dimensions from their parent structural element.
	AttachPositions []ArtifactAttachPosition

	// Subtype of the artifact, if applicable.
	//
	// The subtype of the artifact. This entry should appear
	// only when the Type entry has a value of Pagination.
	//
	// Several standard values are defined in `model.ArtifactSubtype` type.
	//
	// Additional values may be specified for this entry.
	Subtype string
}

func (a *Artifact) GenerateMap() map[string]core.PdfObject {
	objByKey := make(map[string]core.PdfObject)
	if a.Type != ArtifactTypeNone {
		objByKey["Type"] = core.MakeName(string(a.Type))
	}
	if a.BBox != nil {
		objByKey["BBox"] = core.MakeArrayFromFloats([]float64{a.BBox.Llx, a.BBox.Lly, a.BBox.Urx, a.BBox.Ury})
	}
	if len(a.AttachPositions) > 0 {
		var objs []core.PdfObject
		for _, attachPosition := range a.AttachPositions {
			objs = append(objs, core.MakeName(string(attachPosition)))
		}
		objByKey["Attach"] = core.MakeArray(objs...)
	}
	if a.Subtype != "" {
		objByKey["Subtype"] = core.MakeName(a.Subtype)
	}
	return objByKey
}

// IDTree represents the ID tree dictionary where the format of the content
// is using Name Trees as described in chapter 7.9.6.
type IDTree struct {
	// Limits shall be an array of two strings, that shall specify the (lexically) least and greatest keys included in the Names array.
	Limits *core.PdfObjectArray

	// Names shall be an array of the form
	//
	// [ key1 value1 key2 value2 … keyn valuen]
	//
	// where each keyi shall be a string and the corresponding valuei shall be the object
	// associated with that key. The keys shall be sorted in lexical order, as described below.
	Names *core.PdfObjectArray

	// Kids Shall be an array of indirect references to the immediate children of this node.
	Kids []*IDTree
}

// ToPdfObject converts the ID tree to a PDF object.
func (t *IDTree) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	if t.Names != nil && t.Names.Len() > 0 {
		dict.Set("Names", t.Names)
		dict.Set("Limits", t.Limits)
	}
	if len(t.Kids) > 0 {
		array := core.MakeArray()
		for _, kid := range t.Kids {
			array.Append(kid.ToPdfObject())
		}
		dict2 := core.MakeDict()
		dict2.Set("Kids", array)
		dict.Set("Kids", dict2)
	}
	return dict
}

func newIDTreeFromPdfObject(obj core.PdfObject) *IDTree {
	obj = core.ResolveReference(obj)
	array := core.MakeArray()
	arr := core.MakeArray()
	idTrees := []*IDTree{}
	if dict, ok := core.GetDict(obj); ok {
		if names := dict.Get("Names"); names != nil {
			names = core.ResolveReference(names)
			if namesArr, ok := core.GetArray(names); ok {
				for _, obj := range namesArr.Elements() {
					array.Append(obj)
				}
			}
		}
		if limits := dict.Get("Limits"); limits != nil {
			limits = core.ResolveReference(limits)
			if limitsArr, ok := core.GetArray(limits); ok {
				for i := 0; i < 2; i++ {
					arr.Append(limitsArr.Get(i))
				}
			}
		}
		if kids := dict.Get("Kids"); kids != nil {
			kids = core.ResolveReference(kids)
			if kidsArr, ok := core.GetArray(kids); ok {
				for _, obj := range kidsArr.Elements() {
					idTree := newIDTreeFromPdfObject(obj)
					idTrees = append(idTrees, idTree)
				}
			}
		}
	}
	idTree := &IDTree{Names: array, Limits: arr}
	if len(idTrees) > 0 {
		idTree.Kids = idTrees
	}
	return idTree
}

// Names represents a PDF name tree.
//
// Ref: PDF32000_2008 chapter 7.7.4.
type Names struct {
	container *core.PdfIndirectObject

	// Dests is a name tree mapping name string to destinations.
	Dests *core.PdfObjectDictionary

	// AP is a name tree mapping name strings to annotation appearance streams.
	AP *core.PdfObjectDictionary

	// JavaScript is a name tree mapping name strings to JavaScript actions.
	JavaScript *core.PdfObjectDictionary

	// Pages is a name tree mapping name strings to visible pages for use in interactive forms.
	Pages *core.PdfObjectDictionary

	// Templates is a name tree mapping name strings to invisible (template) pages for use in interactive forms.
	Templates *core.PdfObjectDictionary

	// IDS is a name tree mapping digital identifies to Web Capture content sets.
	IDS *core.PdfObjectDictionary

	// URLS is a name tree mapping URLs to Web Capture content sets.
	URLS *core.PdfObjectDictionary

	// EmbeddedFiles is a name tree mapping name strings to file specifications for embedded file streams.
	EmbeddedFiles *core.PdfObjectDictionary

	// AlternatePresentations is a name tree mapping name strings to alternate presentations.
	AlternatePresentations *core.PdfObjectDictionary

	// Renditions is a name tree mapping name strings (which shall have Unicode encoding) to rendition objects.
	Renditions *core.PdfObjectDictionary
}

func newNames() *Names { return &Names{container: core.MakeIndirectObject(core.MakeDict())} }

func newNamesFromPdfObject(names core.PdfObject) *Names {
	names2 := newNames()
	dict := names.Direct().(*core.PdfObjectDictionary)
	if dests := dict.Get("Dests"); dests != nil {
		names2.Dests = dests.Direct().(*core.PdfObjectDictionary)
	}
	if ap := dict.Get("AP"); ap != nil {
		names2.AP = ap.Direct().(*core.PdfObjectDictionary)
	}
	if javaScript := dict.Get("JavaScript"); javaScript != nil {
		names2.JavaScript = javaScript.Direct().(*core.PdfObjectDictionary)
	}
	if pages := dict.Get("Pages"); pages != nil {
		names2.Pages = pages.Direct().(*core.PdfObjectDictionary)
	}
	if templates := dict.Get("Templates"); templates != nil {
		names2.Templates = templates.Direct().(*core.PdfObjectDictionary)
	}
	if ids := dict.Get("IDS"); ids != nil {
		names2.IDS = ids.Direct().(*core.PdfObjectDictionary)
	}
	if urls := dict.Get("URLS"); urls != nil {
		names2.URLS = urls.Direct().(*core.PdfObjectDictionary)
	}
	if embeddedFiles := dict.Get("EmbeddedFiles"); embeddedFiles != nil {
		names2.EmbeddedFiles = embeddedFiles.Direct().(*core.PdfObjectDictionary)
	}
	if alternatePresentations := dict.Get("AlternatePresentations"); alternatePresentations != nil {
		names2.AlternatePresentations = alternatePresentations.Direct().(*core.PdfObjectDictionary)
	}
	if renditions := dict.Get("Renditions"); renditions != nil {
		names2.Renditions = renditions.Direct().(*core.PdfObjectDictionary)
	}
	return names2
}

// ToPdfObject generates a PdfObject representation of the Names struct.
func (n *Names) ToPdfObject() core.PdfObject {
	indirect := n.container
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("Invalid object")
		return indirect
	}
	if n.EmbeddedFiles != nil {
		dict.Set("EmbeddedFiles", core.MakeIndirectObject(n.EmbeddedFiles))
	}
	return indirect
}

func (n *Names) addEmbeddedFile(file *EmbeddedFile) error {
	if n.EmbeddedFiles == nil {
		n.EmbeddedFiles = core.MakeDict()
		n.EmbeddedFiles.Set("Names", core.MakeArray())
	}
	filespec := NewPdfFileSpecFromEmbeddedFile(file)
	names := n.EmbeddedFiles.Get("Names")
	arr, ok := names.(*core.PdfObjectArray)
	if !ok {
		return errors.New("Invalid Names array")
	}
	type FileSpecMap struct {
		name     string
		filespec *PdfFilespec
	}
	fileSpecMaps := []FileSpecMap{}
	for i := 0; i < len(arr.Elements()); i += 2 {
		if i%2 == 0 {
			obj := arr.Get(i)
			if obj != nil {
				str := obj.(*core.PdfObjectString)
				obj := arr.Get(i + 1)
				filespec, err := NewPdfFilespecFromObj(obj)
				if err != nil {
					return err
				}
				fileSpecMaps = append(fileSpecMaps, FileSpecMap{name: str.String(), filespec: filespec})
			}
		}
	}
	fileSpecMaps = append(fileSpecMaps, FileSpecMap{name: file.Name, filespec: filespec})
	sort.Slice(fileSpecMaps, func(n, count int) bool { return fileSpecMaps[n].name < fileSpecMaps[count].name })
	arr = core.MakeArray()
	for _, fileSpecMap := range fileSpecMaps {
		arr.Append(core.MakeString(fileSpecMap.name))
		arr.Append(fileSpecMap.filespec.ToPdfObject())
	}
	n.EmbeddedFiles.Set("Names", arr)
	return nil
}

// StructureType represents a standard structure type in a tagged PDF document.
type StructureType string

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

// Schema returns the namespace schema, a reference to a file containing a
// schema for the namespace (ISO 32000-2 14.7.4.2, table 356). Nil if unset.
func (ns *PdfStructureNamespace) Schema() core.PdfObject { return ns.schema }

const (
	StructureNamespacePDF2   = "http://iso.org/pdf2/ssn"
	StructureNamespacePDF17  = "http://iso.org/pdf/ssn"
	StructureNamespaceMathML = "http://www.w3.org/1998/Math/MathML"
)
