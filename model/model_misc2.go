package model

import (
	"bytes"
	"errors"
	_ "image/gif"
	"math/bits"
	"strings"
	"unicode"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// GetBorderWidth returns the border style's width.
func (b *PdfBorderStyle) GetBorderWidth() float64 {
	if b.W == nil {
		return 1
	}
	return *b.W
}

func getASCIISections(data []byte) (s, s2 string, err error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("getASCIISections: %d ", len(data))
	}
	index := type1EncodingDictRegexp.FindIndex(data)
	if index == nil {
		common.Log.Debug("ERROR: getASCIISections. No dict.")
		return "", "", core.ErrTypeError
	}
	n := index[1]
	count := strings.Index(string(data[n:]), type1EncodingMarker)
	if count < 0 {
		s = string(data[n:])
		return s, "", nil
	}
	num := n + count
	s = string(data[n:num])
	size := num
	count = strings.Index(string(data[size:]), type1ReadonlyDefMarker)
	if count < 0 {
		common.Log.Debug("ERROR: getASCIISections. err=%v", err)
		return "", "", core.ErrTypeError
	}
	length := size + count
	s2 = string(data[size:length])
	return s, s2, nil
}

func (a *PdfAppender) updateObjectsDeep(obj core.PdfObject, set map[core.PdfObject]struct{}) {
	if set == nil {
		set = map[core.PdfObject]struct{}{}
	}
	if _, ok := set[obj]; ok || obj == nil {
		return
	}
	set[obj] = struct{}{}
	err := core.ResolveReferencesDeep(obj, a.traversed)
	if err != nil {
		common.Log.Debug("ERROR: %v", err)
	}
	switch v := obj.(type) {
	case *core.PdfIndirectObject:
		switch {
		case v.GetParser() == a.roReader.parser:
			return
		case v.GetParser() == a.Reader.parser:
			indirectObjectByNumber, _ := a.roReader.GetIndirectObjectByNumber(int(v.ObjectNumber))
			indirect, ok := indirectObjectByNumber.(*core.PdfIndirectObject)
			if ok && indirect != nil {
				s := string(v.PdfObject.Write())
				if indirect.PdfObject != v.PdfObject && string(indirect.PdfObject.Write()) != s {
					if strings.Contains(s, "/Sig") && strings.Contains(s, "/Subtype") {
						return
					}
					a.addNewObject(obj)
					a.objNumMap[obj] = v.ObjectNumber
				}
			}
		default:
			a.addNewObject(obj)
		}
		a.updateObjectsDeep(v.PdfObject, set)
	case *core.PdfObjectArray:
		for _, obj := range v.Elements() {
			a.updateObjectsDeep(obj, set)
		}
	case *core.PdfObjectDictionary:
		for _, name := range v.Keys() {
			a.updateObjectsDeep(v.Get(name), set)
		}
	case *core.PdfObjectStreams:
		if v.GetParser() != a.roReader.parser {
			for _, obj := range v.Elements() {
				a.updateObjectsDeep(obj, set)
			}
		}
	case *core.PdfObjectStream:
		switch {
		case v.GetParser() == a.roReader.parser:
			return
		case v.GetParser() == a.Reader.parser:
			if lookupByReference, err := a.roReader.parser.LookupByReference(v.PdfObjectReference); err == nil {
				var ok bool
				if lookupByReferenceStream, isLookupByReferenceStream := core.GetStream(lookupByReference); isLookupByReferenceStream && bytes.Equal(lookupByReferenceStream.Stream, v.Stream) {
					ok = true
				}
				if lookupByReferenceDict, isLookupByReferenceDict := core.GetDict(lookupByReference); ok && isLookupByReferenceDict {
					ok = string(lookupByReferenceDict.Write()) == string(v.PdfObjectDictionary.Write())
				}
				if ok {
					return
				}
			}
			if v.ObjectNumber != 0 {
				a.objNumMap[obj] = v.ObjectNumber
			}
		default:
			if _, ok := a.hasNewObject[obj]; !ok {
				a.addNewObject(obj)
			}
		}
		a.updateObjectsDeep(v.PdfObjectDictionary, set)
	}
}

// ArtifactSubtype represents the subtype of an artifact in a PDF document.
type ArtifactSubtype string

// PdfBorderStyle represents a border style dictionary (12.5.4 Border Styles p. 394).
type PdfBorderStyle struct {
	W         *float64
	S         *BorderStyle
	D         *[]int
	container core.PdfObject
}

// BorderEffect represents a border effect (Table 167 p. 395).
type BorderEffect int

var (
	ArtifactAttachPositionTop    ArtifactAttachPosition = "Top"
	ArtifactAttachPositionBottom ArtifactAttachPosition = "Bottom"
	ArtifactAttachPositionLeft   ArtifactAttachPosition = "Left"
	ArtifactAttachPositionRight  ArtifactAttachPosition = "Right"
)

func getPdfAuthor() string { metadataMu.Lock(); defer metadataMu.Unlock(); return pdfAuthor }

// BorderStyle defines border type, typically used for annotations.
type BorderStyle int

// UpdatePage updates the `page` in the new revision if it has changed.
func (a *PdfAppender) UpdatePage(page *PdfPage) {
	a.updateObjectsDeep(page.ToPdfObject(), nil)
}

// PdfBorderEffect represents a PDF border effect.
type PdfBorderEffect struct {
	S *BorderEffect
	I *float64
}

func bitsRequired(n uint32) uint16 {
	if n == 0 {
		return 1
	}
	return uint16(bits.Len32(n))
}

var (
	ArtifactTypeNone       ArtifactType = ""
	ArtifactTypePagination ArtifactType = "Pagination"
	ArtifactTypeLayout     ArtifactType = "Layout"
	ArtifactTypePage       ArtifactType = "Page"
	ArtifactTypeBackground ArtifactType = "Background"
)

// ToPdfObject implements interface PdfModel.
func (b *PdfBorderStyle) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	if b.container != nil {
		if indirect, ok := b.container.(*core.PdfIndirectObject); ok {
			indirect.PdfObject = dict
		}
	}
	dict.Set("Subtype", core.MakeName("Border"))
	if b.W != nil {
		dict.Set("W", core.MakeFloat(*b.W))
	}
	if b.S != nil {
		dict.Set("S", core.MakeName(b.S.GetPdfName()))
	}
	if b.D != nil {
		dict.Set("D", core.MakeArrayFromIntegers(*b.D))
	}
	if b.container != nil {
		return b.container
	}
	return dict
}

// ArtifactType represents the type of an artifact in a PDF document.
type ArtifactType string

func applySubsetTag(font *pdfFontType0, s *string, fontDescriptor *PdfFontDescriptor, data []byte) {
	s2 := subsetTag(data)
	if len(font.baseFont) > 0 {
		font.baseFont = prependSubsetTag(font.baseFont, s2)
	}
	if len(*s) > 0 {
		*s = prependSubsetTag(*s, s2)
	}
	if len(font.name) > 0 {
		font.name = prependSubsetTag(font.name, s2)
	}
	if fontDescriptor != nil {
		fontNameName, ok := core.GetName(fontDescriptor.FontName)
		if ok && len(fontNameName.String()) > 0 {
			fontDescriptor.FontName = core.MakeName(prependSubsetTag(fontNameName.String(), s2))
		}
	}
}

func isBinaryData(data []byte) bool {
	if len(data) < 4 {
		return true
	}
	for i := range data[:4] {
		n := rune(i)
		if !unicode.Is(unicode.ASCII_Hex_Digit, n) && !unicode.IsSpace(n) {
			return true
		}
	}
	return false
}

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

const (
	BorderEffectNoEffect BorderEffect = iota
	BorderEffectCloudy   BorderEffect = iota
)

// SetPdfAuthor sets the Author attribute of the output PDF.
func SetPdfAuthor(author string) { metadataMu.Lock(); defer metadataMu.Unlock(); pdfAuthor = author }

// SetBorderWidth sets the style's border width.
func (b *PdfBorderStyle) SetBorderWidth(width float64) { b.W = &width }

// GetPdfName returns the PDF name used to indicate the border style.
// (Table 166 p. 395).
func (b *BorderStyle) GetPdfName() string {
	switch *b {
	case BorderStyleSolid:
		return "S"
	case BorderStyleDashed:
		return "D"
	case BorderStyleBeveled:
		return "B"
	case BorderStyleInset:
		return "I"
	case BorderStyleUnderline:
		return "U"
	}
	return ""
}

// ArtifactAttachPosition specifies the position where an artifact is attached to a page.
type ArtifactAttachPosition string

// NewBorderStyle returns an initialized PdfBorderStyle.
func NewBorderStyle() *PdfBorderStyle { borderStyle := &PdfBorderStyle{}; return borderStyle }

const (
	BorderStyleSolid     BorderStyle = iota
	BorderStyleDashed    BorderStyle = iota
	BorderStyleBeveled   BorderStyle = iota
	BorderStyleInset     BorderStyle = iota
	BorderStyleUnderline BorderStyle = iota
)

func newPdfBorderStyleFromPdfObject(obj core.PdfObject) (*PdfBorderStyle, error) {
	borderStyle := &PdfBorderStyle{}
	borderStyle.container = obj
	var dict *core.PdfObjectDictionary
	obj = core.TraceToDirectObject(obj)
	dict, ok := obj.(*core.PdfObjectDictionary)
	if !ok {
		return nil, errors.New("type check")
	}
	if obj := dict.Get("Type"); obj != nil {
		name, ok := obj.(*core.PdfObjectName)
		if !ok {
			common.Log.Debug("Incompatibility with Type not a name object: %T", obj)
		} else {
			if *name != "Border" {
				common.Log.Debug("Warning, Type != Border: %s", *name)
			}
		}
	}
	if w := dict.Get("W"); w != nil {
		f, err := core.GetNumberAsFloat(w)
		if err != nil {
			common.Log.Debug("Error retrieving W: %v", err)
			return nil, err
		}
		borderStyle.W = &f
	}
	if s := dict.Get("S"); s != nil {
		name, ok := s.(*core.PdfObjectName)
		if !ok {
			return nil, errors.New("border S not a name object")
		}
		var style BorderStyle
		switch *name {
		case "S":
			style = BorderStyleSolid
		case "D":
			style = BorderStyleDashed
		case "B":
			style = BorderStyleBeveled
		case "I":
			style = BorderStyleInset
		case "U":
			style = BorderStyleUnderline
		default:
			common.Log.Debug("Invalid style name %s", *name)
			return nil, errors.New("style type range check")
		}
		borderStyle.S = &style
	}
	if d := dict.Get("D"); d != nil {
		arr, ok := d.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Border D dash not an array: %T", d)
			return nil, errors.New("border D type check error")
		}
		nums, err := arr.ToIntegerArray()
		if err != nil {
			common.Log.Debug("Border D Problem converting to integer array: %v", err)
			return nil, err
		}
		borderStyle.D = &nums
	}
	return borderStyle, nil
}

// UpdateObject marks `obj` as updated and to be included in the following revision.
func (a *PdfAppender) UpdateObject(obj core.PdfObject) {
	a.replaceObject(obj, obj)
	if _, ok := a.hasNewObject[obj]; !ok {
		a.newObjects = append(a.newObjects, obj)
		a.hasNewObject[obj] = struct{}{}
	}
}

var (
	ArtifactSubtypeHeader    ArtifactSubtype = "Header"
	ArtifactSubtypeFooter    ArtifactSubtype = "Footer"
	ArtifactSubtypeWatermark ArtifactSubtype = "Watermark"
)

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
