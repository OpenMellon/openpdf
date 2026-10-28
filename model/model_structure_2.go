package model

import (
	_ "image/gif"

	"github.com/OpenMellon/openpdf/core"
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
