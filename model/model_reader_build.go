package model

import (
	"errors"
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

func (r *PdfReader) buildNameNodes(indirect *core.PdfIndirectObject, set map[core.PdfObject]struct{}) error {
	if indirect == nil {
		return nil
	}
	if _, ok := set[indirect]; ok {
		common.Log.Debug("Cyclic recursion, skipping (%v)", indirect.ObjectNumber)
		return nil
	}
	set[indirect] = struct{}{}
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return errors.New("node not a dictionary")
	}
	if dests, ok := core.GetDict(dict.Get("Dests")); ok {
		kids, ok := core.GetArray(dests.Get("Kids"))
		if !ok {
			return errors.New("Invalid Kids array object")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Kids: %s", kids)
		}
		for i, obj := range kids.Elements() {
			indirect, ok := core.GetIndirect(obj)
			if !ok {
				common.Log.Debug("ERROR: child not indirect object - (%s)", indirect)
				return errors.New("child not indirect object")
			}
			kids.Set(i, indirect)
			err := r.buildNameNodes(indirect, set)
			if err != nil {
				return err
			}
		}
	}
	if dictDict, ok := core.GetDict(dict); ok {
		if !core.IsNullObject(dictDict.Get("Kids")) {
			if kids, ok := core.GetArray(dictDict.Get("Kids")); ok {
				for i, obj := range kids.Elements() {
					if indirect, ok := core.GetIndirect(obj); ok {
						kids.Set(i, indirect)
						err := r.buildNameNodes(indirect, set)
						if err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

func (r *PdfReader) buildOutlineTree(obj core.PdfObject, node *PdfOutlineTreeNode, node2 *PdfOutlineTreeNode, set map[core.PdfObject]struct{}) (*PdfOutlineTreeNode, *PdfOutlineTreeNode, error) {
	if set == nil {
		set = map[core.PdfObject]struct{}{}
	}
	set[obj] = struct{}{}
	indirect, ok := obj.(*core.PdfIndirectObject)
	if !ok {
		return nil, nil, fmt.Errorf("outline container not an indirect object %T", obj)
	}
	dict, isDict := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !isDict {
		return nil, nil, errors.New("not a dictionary object")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("build outline tree: dict: %v (%v) p: %p", dict, indirect, indirect)
	}
	if title := dict.Get("Title"); title != nil {
		item, err := r.newPdfOutlineItemFromIndirectObject(indirect)
		if err != nil {
			return nil, nil, err
		}
		item.Parent = node
		item.Prev = node2
		first := core.ResolveReference(dict.Get("First"))
		if _, ok := set[first]; first != nil && first != indirect && !ok {
			if !core.IsNullObject(first) {
				outlineTree, node, err := r.buildOutlineTree(first, &item.PdfOutlineTreeNode, nil, set)
				if err != nil {
					common.Log.Debug("DEBUG: could not build outline item tree: %v. Skipping node children.", err)
				} else {
					item.First = outlineTree
					item.Last = node
				}
			}
		}
		next := core.ResolveReference(dict.Get("Next"))
		if _, ok := set[next]; next != nil && next != indirect && !ok {
			if !core.IsNullObject(next) {
				outlineTree, node2, err := r.buildOutlineTree(next, node, &item.PdfOutlineTreeNode, set)
				if err != nil {
					common.Log.Debug("DEBUG: could not build outline tree for Next node: %v. Skipping node.", err)
				} else {
					item.Next = outlineTree
					return &item.PdfOutlineTreeNode, node2, nil
				}
			}
		}
		return &item.PdfOutlineTreeNode, &item.PdfOutlineTreeNode, nil
	}
	outline, err := newPdfOutlineFromIndirectObject(indirect)
	if err != nil {
		return nil, nil, err
	}
	outline.Parent = node
	if first := dict.Get("First"); first != nil {
		first = core.ResolveReference(first)
		if _, ok := set[first]; first != nil && first != indirect && !ok {
			obj := core.TraceToDirectObject(first)
			if _, ok := obj.(*core.PdfObjectNull); !ok && obj != nil {
				outlineTree, node, err := r.buildOutlineTree(first, &outline.PdfOutlineTreeNode, nil, set)
				if err != nil {
					common.Log.Debug("DEBUG: could not build outline tree: %v. Skipping node children.", err)
				} else {
					outline.First = outlineTree
					outline.Last = node
				}
			}
		}
	}
	return &outline.PdfOutlineTreeNode, &outline.PdfOutlineTreeNode, nil
}

func (r *PdfReader) buildPageList(indirect *core.PdfIndirectObject, indirect2 *core.PdfIndirectObject, set map[core.PdfObject]struct{}) error {
	if indirect == nil {
		return nil
	}
	if _, ok := set[indirect]; ok {
		common.Log.Debug("Cyclic recursion, skipping (%v)", indirect.ObjectNumber)
		return nil
	}
	set[indirect] = struct{}{}
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return errors.New("node not a dictionary")
	}
	name, ok := (*dict).Get("Type").(*core.PdfObjectName)
	if !ok {
		if dict.Get("Kids") == nil {
			return errors.New("node missing Type (Required)")
		}
		common.Log.Debug("ERROR: node missing Type, but has Kids. Assuming Pages node.")
		name = core.MakeName("Pages")
		dict.Set("Type", name)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("buildPageList node type: %s (%+v)", *name, indirect)
	}
	if *name == "Page" {
		page, err := r.newPdfPageFromDict(dict)
		if err != nil {
			return err
		}
		page.setContainer(indirect)
		if indirect2 != nil {
			dict.Set("Parent", indirect2)
		}
		r.pageObjs = append(r.pageObjs, indirect)
		r.PageList = append(r.PageList, page)
		return nil
	}
	if *name != "Pages" {
		if r.parser.Opts != nil && r.parser.Opts.RelaxedMode {
			common.Log.Warning("Warning: Table of content containing non Page/Pages object! (%s)", name)
			return nil
		}
		common.Log.Debug("ERROR: Table of content containing non Page/Pages object! (%s)", name)
		return errors.New("table of content containing non Page/Pages object")
	}
	if indirect2 != nil {
		dict.Set("Parent", indirect2)
	}
	if !r.lazy {
		err := r.traverseObjectData(indirect)
		if err != nil {
			return err
		}
	}
	resolve, err := r.parser.Resolve(dict.Get("Kids"))
	if err != nil {
		common.Log.Debug("ERROR: Failed loading Kids object")
		return err
	}
	var arr *core.PdfObjectArray
	arr, ok = resolve.(*core.PdfObjectArray)
	if !ok {
		indirect, isIndirect := resolve.(*core.PdfIndirectObject)
		if !isIndirect {
			return errors.New("invalid Kids object")
		}
		arr, ok = indirect.PdfObject.(*core.PdfObjectArray)
		if !ok {
			return errors.New("invalid Kids indirect object")
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Kids: %s", arr)
	}
	for i, obj := range arr.Elements() {
		indirect2, ok := core.GetIndirect(obj)
		if !ok {
			common.Log.Debug("ERROR: Page not indirect object - (%s)", indirect2)
			return errors.New("page not indirect object")
		}
		arr.Set(i, indirect2)
		err = r.buildPageList(indirect2, indirect, set)
		if err != nil {
			return err
		}
	}
	return nil
}
