package model

import (
	"fmt"
	_ "image/gif"
	"sort"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/uuid"
)

// ToPdfObject converts the structure tree root to a PDF object.
func (r *StructTreeRoot) ToPdfObject() core.PdfObject {
	indirect := r.container
	if indirect == nil {
		indirect = &core.PdfIndirectObject{}
		indirect.PdfObject = core.MakeDict()
	}
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	var obj core.PdfObject
	if len(r.K) == 1 {
		obj = core.MakeIndirectObject(r.K[0].ToPdfObject())
	} else {
		array := core.MakeArray()
		for _, K := range r.K {
			array.Append(core.MakeIndirectObject(K.ToPdfObject()))
		}
		obj = array
	}
	var (
		objs      = []core.PdfObject{}
		objsByObj = map[core.PdfObject][]core.PdfObject{}
		objByKey  = map[string]core.PdfObject{}
	)
	collectStructureElements(obj, indirect, objsByObj, objByKey, &objs)
	dict.Set("Type", core.MakeName("StructTreeRoot"))
	dict.Set("K", obj)
	if r.IDTree != nil {
		dict.Set("IDTree", core.MakeIndirectObject(r.IDTree.ToPdfObject()))
	} else if len(objByKey) > 0 {
		array := core.MakeArray()
		strs := make([]string, 0, len(objByKey))
		for s := range objByKey {
			strs = append(strs, s)
		}
		sort.Strings(strs)
		for _, str := range strs {
			array.Append(core.MakeString(str))
			array.Append(objByKey[str])
		}
		r.IDTree = &IDTree{Names: array, Limits: core.MakeArray(core.MakeString(strs[0]), core.MakeString(strs[len(strs)-1]))}
		dict.Set("IDTree", core.MakeIndirectObject(r.IDTree.ToPdfObject()))
	}
	if r.ParentTree != nil {
		dict.Set("ParentTree", core.MakeIndirectObject(r.ParentTree))
	} else if len(objsByObj) > 0 || len(objs) > 0 {
		array := core.MakeArray()
		n := 0
		for obj, objs := range objsByObj {
			arr := core.MakeArray()
			for _, obj := range objs {
				arr.Append(obj)
			}
			integer := core.MakeInteger(int64(n))
			array.Append(integer)
			array.Append(core.MakeIndirectObject(arr))
			if indirect, ok := core.GetIndirect(obj); ok {
				if dict, ok := core.GetDict(indirect.PdfObject); ok {
					structParents := dict.Get("StructParents")
					if structParents != nil {
						if count, ok := core.GetIntVal(structParents); ok {
							if n < count {
								dict.Set("StructParents", integer)
							}
						}
					} else {
						dict.Set("StructParents", integer)
					}
				}
			}
			n++
		}
		fn := func(obj core.PdfObject, dict *core.PdfObjectDictionary) bool {
			dict2 := dict
			if obj := dict.Get("Type"); obj.String() == "OBJR" {
				if obj := dict.Get("Obj"); obj != nil {
					if dict3, ok := core.GetDict(obj); ok {
						dict2 = dict3
					}
				}
			}
			obj2 := dict2.Get("Type")
			if obj2 != nil && obj2.String() == "Annot" {
				integer := core.MakeInteger(int64(n))
				dict2.Set("StructParent", integer)
				array.Append(integer)
				array.Append(obj)
				n++
				return true
			}
			return false
		}
		for _, obj := range objs {
			if dict, ok := core.GetDict(obj); ok {
				if s := dict.Get("S"); s != nil {
					if s2, ok := core.GetNameVal(s); ok && (s2 == string(StructureTypeLink) || s2 == string(StructureTypeAnnot)) {
						if k := dict.Get("K"); k != nil {
							if kArr, ok := core.GetArray(k); ok {
								for _, obj2 := range kArr.Elements() {
									if dict, ok := core.GetDict(obj2); ok {
										fn(obj, dict)
									}
								}
							} else if kDict, ok := core.GetDict(k); ok {
								fn(obj, kDict)
							}
						}
					}
				}
			}
		}
		r.ParentTree = core.MakeDict()
		r.ParentTree.Set("Nums", array)
		r.ParentTreeNextKey = int64(n)
		dict.Set("ParentTree", core.MakeIndirectObject(r.ParentTree))
	}
	dict.Set("ParentTreeNextKey", core.MakeInteger(r.ParentTreeNextKey))
	if r.RoleMap != nil {
		dict.Set("RoleMap", r.RoleMap)
	}
	if r.ClassMap != nil {
		dict.Set("ClassMap", r.ClassMap)
	}
	array := core.MakeArray()
	for _, namespace := range r.Namespaces {
		if namespace == nil {
			continue
		}
		array.Append(namespace.ToPdfObject())
	}
	if array.Len() > 0 {
		dict.Set("Namespaces", array)
	} else {
		dict.Remove("Namespaces")
	}
	return indirect
}

// NewStructTreeRoot creates a new structure tree root dictionary.
func NewStructTreeRoot() *StructTreeRoot {
	return &StructTreeRoot{K: []*KDict{}, RoleMap: core.MakeDict(), ParentTreeNextKey: 0}
}

// NewStructTreeRootFromPdfObject creates a new structure tree root from a PDF object.
func NewStructTreeRootFromPdfObject(obj core.PdfObject) (*StructTreeRoot, error) {
	obj2 := core.ResolveReference(obj)
	dict, ok := core.GetDict(obj2)
	if !ok {
		return nil, fmt.Errorf("existing structure tree root is not a dictionary")
	}
	root := &StructTreeRoot{}
	root.container = core.MakeIndirectObject(core.MakeDict())
	if k := dict.Get("K"); k != nil {
		obj := core.ResolveReference(k)
		err := core.ResolveReferencesDeep(obj, nil)
		if err != nil {
			common.Log.Debug("Error resolving K object: %v", err)
		}
		root.K = []*KDict{}
		if arr, ok := core.GetArray(obj); ok {
			for i := 0; i < arr.Len(); i++ {
				obj := arr.Get(i)
				kDict, err := NewKDictFromPdfObject(obj)
				if err != nil {
					return nil, err
				}
				root.K = append(root.K, kDict)
			}
		} else {
			kDict, err := NewKDictFromPdfObject(obj)
			if err != nil {
				return nil, err
			}
			root.K = append(root.K, kDict)
		}
	}
	if idTree := dict.Get("IDTree"); idTree != nil {
		root.IDTree = newIDTreeFromPdfObject(idTree)
	}
	if parentTree := dict.Get("ParentTree"); parentTree != nil {
		obj := core.ResolveReference(parentTree)
		if dict, ok := core.GetDict(obj); ok {
			root.ParentTree = dict
		}
	}
	if parentTreeNextKey := dict.Get("ParentTreeNextKey"); parentTreeNextKey != nil {
		_, ok := core.GetInt(parentTreeNextKey)
		if ok {
			n, err := core.GetNumberAsInt64(parentTreeNextKey)
			if err != nil {
				return nil, err
			}
			root.ParentTreeNextKey = n
		}
	}
	if roleMap := dict.Get("RoleMap"); roleMap != nil {
		switch v := roleMap.(type) {
		case *core.PdfIndirectObject:
			if dict, ok := core.GetDict(v.PdfObject); ok {
				root.RoleMap = dict
			}
		case *core.PdfObjectDictionary:
			root.RoleMap = v
		case *core.PdfObjectString:
			root.RoleMap = v
		}
	}
	if classMap := dict.Get("ClassMap"); classMap != nil {
		if classMapDict, ok := core.GetDict(classMap); ok {
			root.ClassMap = classMapDict
		}
	}
	if namespaces, ok := core.GetArray(dict.Get("Namespaces")); ok {
		for _, obj := range namespaces.Elements() {
			if structureNamespace, ok := newPdfStructureNamespaceFromObject(obj); ok {
				root.Namespaces = append(root.Namespaces, structureNamespace)
			}
		}
	}
	return root, nil
}

// AddKDict adds a K dictionary object to the structure tree root.
func (r *StructTreeRoot) AddKDict(k *KDict) { r.K = append(r.K, k) }

// AddNamespace adds a namespace dictionary to the structure tree root.
func (r *StructTreeRoot) AddNamespace(ns *PdfStructureNamespace) {
	r.Namespaces = append(r.Namespaces, ns)
}

// StructTreeRoot represents the structure tree root dictionary.
// Reference: PDF documentation chapter 14.7 Logical Structure, table 322.
type StructTreeRoot struct {
	K                 []*KDict
	IDTree            *IDTree
	ParentTree        *core.PdfObjectDictionary
	ParentTreeNextKey int64
	RoleMap           core.PdfObject
	ClassMap          *core.PdfObjectDictionary

	// Namespaces are the namespace dictionaries used within the document
	// (ISO 32000-2, PDF 2.0). Nil for PDF 1.x structure trees.
	Namespaces []*PdfStructureNamespace
	container  *core.PdfIndirectObject
	ids        []uuid.UUID
}
