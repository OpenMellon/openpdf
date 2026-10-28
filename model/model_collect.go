package model

import (
	_ "image/gif"

	"github.com/OpenMellon/openpdf/core"
)

func collectPageResourceObjects(page *PdfPage) map[core.PdfObjectName]core.PdfObject {
	objByName := make(map[core.PdfObjectName]core.PdfObject)
	if page.Resources == nil {
		return objByName
	}
	if page.Resources.Font != nil {
		if fontDict, ok := core.GetDict(page.Resources.Font); ok {
			for _, name := range fontDict.Keys() {
				objByName[name] = fontDict.Get(name)
			}
		}
	}
	if page.Resources.ExtGState != nil {
		if extGStateDict, ok := core.GetDict(page.Resources.ExtGState); ok {
			for _, name := range extGStateDict.Keys() {
				objByName[name] = extGStateDict.Get(name)
			}
		}
	}
	if page.Resources.XObject != nil {
		if xObjectDict, ok := core.GetDict(page.Resources.XObject); ok {
			for _, name := range xObjectDict.Keys() {
				objByName[name] = xObjectDict.Get(name)
			}
		}
	}
	if page.Resources.Pattern != nil {
		if patternDict, ok := core.GetDict(page.Resources.Pattern); ok {
			for _, name := range patternDict.Keys() {
				objByName[name] = patternDict.Get(name)
			}
		}
	}
	if page.Resources.Shading != nil {
		if shadingDict, ok := core.GetDict(page.Resources.Shading); ok {
			for _, name := range shadingDict.Keys() {
				objByName[name] = shadingDict.Get(name)
			}
		}
	}
	if page.Resources.ProcSet != nil {
		if procSetDict, ok := core.GetDict(page.Resources.ProcSet); ok {
			for _, name := range procSetDict.Keys() {
				objByName[name] = procSetDict.Get(name)
			}
		}
	}
	if page.Resources.Properties != nil {
		if propertiesDict, ok := core.GetDict(page.Resources.Properties); ok {
			for _, name := range propertiesDict.Keys() {
				objByName[name] = propertiesDict.Get(name)
			}
		}
	}
	return objByName
}

func collectIndirectObjects(indirect *core.PdfIndirectObject) []*core.PdfIndirectObject {
	var indirects []*core.PdfIndirectObject
	if indirect == nil {
		return indirects
	}
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return indirects
	}
	name, _ := dict.Get("Type").(*core.PdfObjectName)
	if name != nil && string(*name) == "Page" {
		indirects = append(indirects, indirect)
		return indirects
	}
	arr, ok := dict.Get("Kids").(*core.PdfObjectArray)
	if !ok {
		return indirects
	}
	for _, obj := range arr.Elements() {
		if indirect, ok := obj.(*core.PdfIndirectObject); ok {
			indirects = append(indirects, collectIndirectObjects(indirect)...)
		}
	}
	return indirects
}

func collectReachableObjectNumbers(indirect *core.PdfIndirectObject, numByObj map[core.PdfObject]int64) map[int64]struct{} {
	set := map[int64]struct{}{}
	set2 := map[core.PdfObject]struct{}{}
	var fn func(obj core.PdfObject)
	fn = func(obj core.PdfObject) {
		if obj == nil {
			return
		}
		if _, ok := set2[obj]; ok {
			return
		}
		set2[obj] = struct{}{}
		if n, ok := numByObj[obj]; ok {
			set[n] = struct{}{}
		}
		switch v := obj.(type) {
		case *core.PdfIndirectObject:
			fn(v.PdfObject)
		case *core.PdfObjectStream:
			if v.PdfObjectDictionary != nil {
				fn(v.PdfObjectDictionary)
			}
		case *core.PdfObjectDictionary:
			for _, name := range v.Keys() {
				if name == "Parent" {
					continue
				}
				fn(v.Get(name))
			}
		case *core.PdfObjectArray:
			for _, obj := range v.Elements() {
				fn(obj)
			}
		}
	}
	fn(indirect)
	return set
}

func collectStructureElements(obj core.PdfObject, obj2 core.PdfObject, objsByObj map[core.PdfObject][]core.PdfObject, objByKey map[string]core.PdfObject, objs *[]core.PdfObject) {
	var indirect *core.PdfIndirectObject
	if indirect2, ok := core.GetIndirect(obj); ok {
		indirect = indirect2
		obj = indirect2.PdfObject
	}
	switch v := obj.(type) {
	case *core.PdfObjectDictionary:
		if v.Get("S") == nil {
			return
		}
		v.Set("P", obj2)
		if pg := v.Get("Pg"); pg != nil {
			if pgIndirect, ok := core.GetIndirect(pg); ok && pgIndirect != nil && pgIndirect.PdfObject != nil {
				objsByObj[pg] = append(objsByObj[pg], indirect)
			}
		}
		if s := v.Get("S"); s != nil {
			if s2, ok := core.GetNameVal(s); ok {
				if s2 == string(StructureTypeLink) || s2 == string(StructureTypeAnnot) {
					if k := v.Get("K"); k != nil {
						if kArr, ok := core.GetArray(k); ok && kArr.Len() == 2 {
							ok := false
							found := false
							for _, obj := range kArr.Elements() {
								if dict, isDict := core.GetDict(obj); isDict {
									if _, valid := core.GetName(dict.Get("Type")); valid {
										found = true
									}
								} else if _, found := core.GetInt(obj); found {
									ok = true
								}
							}
							if ok && found {
								*objs = append(*objs, indirect)
							}
						} else if kDict, ok := core.GetDict(k); ok {
							if _, ok := core.GetName(kDict.Get("Type")); ok {
								*objs = append(*objs, indirect)
							}
						}
					}
				}
			}
		}
		if id := v.Get("ID"); id != nil {
			objByKey[id.String()] = obj
		}
		if k := v.Get("K"); k != nil {
			collectStructureElements(k, indirect, objsByObj, objByKey, objs)
		}
	case *core.PdfObjectArray:
		for _, obj3 := range v.Elements() {
			collectStructureElements(obj3, obj2, objsByObj, objByKey, objs)
		}
	default:
	}
}
