package model

import (
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/uuid"
)

// GetRefObject returns the reference object of the KValue.
func (kv *KValue) GetRefObject() core.PdfObject { return kv.refObject }

// SetKDict sets the KDict for the KValue.
func (kv *KValue) SetKDict(kDict *KDict) { kv.Clear(); kv.kDict = kDict }

// SetRefObject sets the reference object for the KValue.
func (kv *KValue) SetRefObject(refObject core.PdfObject) {
	kv.Clear()
	kv.refObject = refObject
}

// ToPdfObject converts the K dictionary to a PDF object.
func (k *KDict) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	if k.ID != nil {
		dict.Set("ID", k.ID)
	}
	if k.K != nil {
		dict.Set("K", k.K)
	} else if len(k.children) > 0 {
		if len(k.children) == 1 {
			k.K = k.children[0].ToPdfObject()
		} else {
			array := core.MakeArray()
			for _, children := range k.children {
				array.Append(children.ToPdfObject())
			}
			k.K = array
		}
		dict.Set("K", k.K)
	}
	if k.S != nil {
		dict.Set("S", k.S)
	}
	if k.P != nil {
		dict.Set("P", k.P)
	}
	if k.Pg != nil {
		dict.Set("Pg", k.Pg)
	}
	if k.C != nil {
		dict.Set("C", k.C)
	}
	if k.R != nil {
		dict.Set("R", k.R)
	}
	if k.T != nil {
		dict.Set("T", k.T)
	}
	if k.Lang != nil {
		dict.Set("Lang", k.Lang)
	}
	if k.Alt != nil {
		dict.Set("Alt", k.Alt)
	}
	if k.E != nil {
		dict.Set("E", k.E)
	}
	if k.A != nil {
		dict.Set("A", k.A)
	} else if k.boundingBox != nil {
		array := core.MakeArrayFromFloats([]float64{k.boundingBox.Llx, k.boundingBox.Lly, k.boundingBox.Urx, k.boundingBox.Ury})
		dict2 := core.MakeDict()
		dict2.Set("BBox", array)
		dict2.Set("O", core.MakeName("Layout"))
		dict.Set("A", core.MakeIndirectObject(dict2))
	}
	if k.ActualText != nil {
		dict.Set("ActualText", k.ActualText)
	}
	if k.E != nil {
		dict.Set("E", k.E)
	}
	if k.Alt != nil {
		dict.Set("Alt", k.Alt)
	}
	if k.NS != nil {
		dict.Set("NS", k.NS.ToPdfObject())
	}
	if k.AF != nil {
		dict.Set("AF", k.AF)
	}
	return dict
}

// Clear clears the KValue.
func (kv *KValue) Clear() { kv.kDict = nil; kv.refObject = nil; kv.mcid = nil }

// ToPdfObject converts the K dictionary to a PDF object.
func (kv *KValue) ToPdfObject() core.PdfObject {
	if kv.kDict != nil {
		return core.MakeIndirectObject(kv.kDict.ToPdfObject())
	}
	if kv.refObject != nil {
		return kv.refObject
	}
	if kv.mcid != nil {
		return core.MakeInteger(int64(*kv.mcid))
	}
	return nil
}

// RemoveKChild removes a specific KDict child from the kChildren slice.
func (k *KDict) RemoveKChild(kChild *KDict) bool {
	for i, children := range k.children {
		if children.kDict == kChild {
			k.children = append(k.children[:i], k.children[i+1:]...)
			return true
		}
	}
	return false
}

// SetPageNumber sets the page number.
func (k *KDict) SetPageNumber(pageNumber int64) { k.pageNumber = pageNumber }

// SetBoundingBox sets the bounding box in the attribute object.
func (k *KDict) SetBoundingBox(x, y, width, height float64) {
	k.boundingBox = &PdfRectangle{Llx: x, Lly: y, Urx: x + width, Ury: y + height}
}

// SetMCID sets the MCID for the KValue.
func (kv *KValue) SetMCID(mcid int) { kv.Clear(); kv.mcid = &mcid }

// NewKValue creates a new K value object.
func NewKValue() *KValue { return &KValue{} }

// AddChild adds a child object.
func (k *KDict) AddChild(kv *KValue) { k.children = append(k.children, kv) }

// AddKChild adds a child K dictionary object.
func (k *KDict) AddKChild(kChild *KDict) {
	k.children = append(k.children, &KValue{kDict: kChild})
}

// SetNamespace sets the PDF 2.0 structure namespace of this element.
func (k *KDict) SetNamespace(ns *PdfStructureNamespace) { k.NS = ns }

// AddMCIDChild adds a child MCID object.
func (k *KDict) AddMCIDChild(mcid int) {
	k.children = append(k.children, &KValue{mcid: &mcid})
}

// KDict represents a K dictionary object.
type KDict struct {
	// The structure type, a name object identifying the nature of the
	// structure element and its role within the document,
	// such as a chapter, paragraph, or footnote
	S core.PdfObject

	// The structure element that is the immediate parent of this one
	// in the structure hierarchy.
	P core.PdfObject

	// The element identifier, a byte string designating this structure element.
	ID *core.PdfObjectString

	// A page object representing a page on which some or all of the content
	// items designated by the K entry shall be rendered.
	Pg core.PdfObject

	// The children of this structure element.
	K core.PdfObject

	// A single attribute object or array of attribute objects associated
	// with this structure element.
	A core.PdfObject

	// An attribute class name or array of class names associated with
	// this structure element.
	C core.PdfObject

	// The current revision number of this structure element
	R *core.PdfObjectInteger

	// The title of the structure element, a text string representing
	// it in human-readable form.
	T *core.PdfObjectString

	// A language identifier specifying the natural language for all text
	// in the structure element except where overridden by
	// language specifications for nested structure elements or marked content.
	Lang *core.PdfObjectString

	// An alternate description of the structure element and its
	// children in human-readable form, which is useful when extracting
	// the document’s contents in support of accessibility to users with
	// disabilities or for other purposes.
	Alt *core.PdfObjectString

	// The expanded form of an abbreviation.
	E *core.PdfObjectString

	// Text that is an exact replacement for the structure element and its children.
	ActualText *core.PdfObjectString

	// NS is the namespace this structure element's type belongs to (PDF 2.0).
	NS *PdfStructureNamespace

	// AF is an array of associated file specification dictionaries (PDF 2.0).
	AF          *core.PdfObjectArray
	children    []*KValue
	pageNumber  int64
	boundingBox *PdfRectangle
}

// GetChildren returns the children of the K dictionary object.
func (k *KDict) GetChildren() []*KValue { return k.children }

// SetPage directly sets the page object.
func (k *KDict) SetPage(page *core.PdfIndirectObject) { k.Pg = page }

// RemoveChildAt removes a child at the specified index from the kChildren slice.
func (k *KDict) RemoveChildAt(index int) bool {
	if index < 0 || index >= len(k.children) {
		return false
	}
	k.children = append(k.children[:index], k.children[index+1:]...)
	return true
}

// AddAssociatedFile adds an associated file specification to this element (PDF 2.0).
func (k *KDict) AddAssociatedFile(filespec core.PdfObject) {
	if k.AF == nil {
		k.AF = core.MakeArray()
	}
	k.AF.Append(filespec)
}

// GetPageNumber returns the page number that has been assigned to the K object.
func (k *KDict) GetPageNumber() int64 { return k.pageNumber }

// GenerateRandomID generates a random ID for the K dictionary object.
func (k *KDict) GenerateRandomID() string {
	id, err := uuid.NewUUID()
	if err != nil {
		common.Log.Debug("Error generating UUID: %v", err)
	}
	if id != uuid.Nil {
		s := id.String()
		k.ID = core.MakeString(s)
		return s
	}
	return ""
}

// GetMCID returns the MCID of the KValue.
func (kv *KValue) GetMCID() *int { return kv.mcid }

// NewKDictionary creates a new K dictionary object.
func NewKDictionary() *KDict { return &KDict{children: make([]*KValue, 0), pageNumber: -1} }

// AddRefChild adds a child reference object.
func (k *KDict) AddRefChild(kChild core.PdfObject) {
	k.children = append(k.children, &KValue{refObject: kChild})
}

// NewKDictFromPdfObject creates a new K dictionary object from a PDF object.
func NewKDictFromPdfObject(obj core.PdfObject) (*KDict, error) {
	obj2 := core.ResolveReference(obj)
	if obj2 == nil {
		return nil, fmt.Errorf("K object is nil")
	}
	dict, ok := core.GetDict(obj2)
	if !ok {
		return nil, fmt.Errorf("K object is not a dictionary")
	}
	kdict := &KDict{}
	if s := dict.Get("S"); s != nil {
		kdict.S = s
	}
	if p := dict.Get("P"); p != nil {
		kdict.P = p
	}
	if id := dict.Get("ID"); id != nil {
		if idStr, ok := core.GetString(id); ok {
			kdict.ID = idStr
		}
	}
	if pg := dict.Get("Pg"); pg != nil {
		kdict.Pg = pg
	}
	if k := dict.Get("K"); k != nil {
		kdict.K = k
		switch v := k.(type) {
		case *core.PdfObjectArray:
			if kArr, ok := core.GetArray(k); ok {
				for _, obj := range kArr.Elements() {
					switch v := obj.(type) {
					case *core.PdfIndirectObject:
						kDict, err := NewKDictFromPdfObject(v)
						if err != nil {
							common.Log.Debug("Error creating K child: %v", err)
							continue
						}
						kdict.children = append(kdict.children, &KValue{kDict: kDict})
					case *core.PdfObjectInteger:
						if n, ok := core.GetIntVal(obj); ok {
							kdict.children = append(kdict.children, &KValue{mcid: &n})
						}
					case *core.PdfObjectDictionary:
						kdict.children = append(kdict.children, &KValue{refObject: obj})
					}
				}
			}
		case *core.PdfIndirectObject:
			kDict, err := NewKDictFromPdfObject(v)
			if err != nil {
				common.Log.Debug("Error creating K child: %v", err)
			}
			if kDict != nil {
				kdict.children = append(kdict.children, &KValue{kDict: kDict})
			}
		case *core.PdfObjectDictionary:
			kdict.children = append(kdict.children, &KValue{refObject: v})
		case *core.PdfObjectInteger:
			if n, ok := core.GetIntVal(k); ok {
				kdict.children = append(kdict.children, &KValue{mcid: &n})
			}
		}
	}
	if a := dict.Get("A"); a != nil {
		kdict.A = a
	}
	if c := dict.Get("C"); c != nil {
		kdict.C = c
	}
	if r := dict.Get("R"); r != nil {
		if rInt, ok := core.GetInt(r); ok {
			kdict.R = rInt
		}
	}
	if t := dict.Get("T"); t != nil {
		if str, ok := core.GetString(t); ok {
			kdict.T = str
		}
	}
	if lang := dict.Get("Lang"); lang != nil {
		if langStr, ok := core.GetString(lang); ok {
			kdict.Lang = langStr
		}
	}
	if alt := dict.Get("Alt"); alt != nil {
		if altStr, ok := core.GetString(alt); ok {
			kdict.Alt = altStr
		}
	}
	if e := dict.Get("E"); e != nil {
		if eStr, ok := core.GetString(e); ok {
			kdict.E = eStr
		}
	}
	if actualText := dict.Get("ActualText"); actualText != nil {
		if actualTextStr, ok := core.GetString(actualText); ok {
			kdict.ActualText = actualTextStr
		}
	}
	if ns := dict.Get("NS"); ns != nil {
		if structureNamespace, ok := newPdfStructureNamespaceFromObject(ns); ok {
			kdict.NS = structureNamespace
		}
	}
	if af, ok := core.GetArray(dict.Get("AF")); ok {
		kdict.AF = af
	}
	return kdict, nil
}

// KValue is a wrapper object to hold various type of K's children objects.
type KValue struct {
	kDict     *KDict
	refObject core.PdfObject
	mcid      *int
}

// RemoveChild removes a specific KValue child from the kChildren slice.
func (k *KDict) RemoveChild(kv *KValue) bool {
	for i, children := range k.children {
		if children == kv {
			k.children = append(k.children[:i], k.children[i+1:]...)
			return true
		}
	}
	return false
}

// GetKDict returns the KDict of the KValue.
func (kv *KValue) GetKDict() *KDict { return kv.kDict }
