package model

import (
	"errors"
	"fmt"
	_ "image/gif"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

func (r *PdfReader) loadOutlines() (*PdfOutlineTreeNode, error) {
	if r.parser.GetCrypter() != nil && !r.parser.IsAuthenticated() {
		return nil, fmt.Errorf("file need to be decrypted first")
	}
	catalog := r.catalog
	outlines := catalog.Get("Outlines")
	if outlines == nil {
		return nil, nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("-Has outlines")
	}
	obj := core.ResolveReference(outlines)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Outline root: %v", obj)
	}
	if ok := core.IsNullObject(obj); ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Outline root is null - no outlines")
		}
		return nil, nil
	}
	indirect, ok := obj.(*core.PdfIndirectObject)
	if !ok {
		if _, ok := core.GetDict(obj); !ok {
			common.Log.Debug("Invalid outline root - skipping")
			return nil, nil
		}
		common.Log.Debug("Outline root is a dict. Should be an indirect object")
		indirect = core.MakeIndirectObject(obj)
	}
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return nil, errors.New("outline indirect object should contain a dictionary")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Outline root dict: %v", dict)
	}
	outlineTree, _, err := r.buildOutlineTree(indirect, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Resulting outline tree: %v", outlineTree)
	}
	return outlineTree, nil
}

// GetOutlines returns a high-level Outline object, based on the outline tree
// of the reader.
func (r *PdfReader) GetOutlines() (*Outline, error) {
	if r == nil {
		return nil, errors.New("cannot create outline from nil reader")
	}
	outlineTree := r.GetOutlineTree()
	if outlineTree == nil {
		return nil, errors.New("the specified reader does not have an outline tree")
	}
	var fn func(node *PdfOutlineTreeNode, items *[]*OutlineItem)
	fn = func(node *PdfOutlineTreeNode, items *[]*OutlineItem) {
		if node == nil {
			return
		}
		if node.context == nil {
			common.Log.Debug("ERROR: missing outline entry context")
			return
		}
		var item *OutlineItem
		if item2, ok := node.context.(*PdfOutlineItem); ok {
			dest := item2.Dest
			if (dest == nil || core.IsNullObject(dest)) && item2.A != nil {
				if dict, ok := core.GetDict(item2.A); ok {
					if d, ok := core.GetArray(dict.Get("D")); ok {
						dest = d
					} else {
						d, ok := core.GetString(dict.Get("D"))
						if !ok {
							return
						}
						ref, ok := r.catalog.Get("Names").(*core.PdfObjectReference)
						if !ok {
							return
						}
						lookupByReference, err := r.parser.LookupByReference(*ref)
						if err != nil {
							common.Log.Debug("ERROR: Failed to read names reference (%s)", err.Error())
							return
						}
						indirect, ok := lookupByReference.(*core.PdfIndirectObject)
						if !ok {
							return
						}
						set := map[core.PdfObject]struct{}{}
						err = r.buildNameNodes(indirect, set)
						if err != nil {
							common.Log.Debug("ERROR: Failed to build name nodes (%s)", err.Error())
							return
						}
						for obj := range set {
							dict, ok := core.GetDict(obj)
							if !ok {
								continue
							}
							names, ok := core.GetArray(dict.Get("Names"))
							if !ok {
								continue
							}
							for i, obj := range names.Elements() {
								switch obj.(type) {
								case *core.PdfObjectString:
									if obj.String() == d.String() {
										if obj := names.Get(i + 1); obj != nil {
											if dict, ok := core.GetDict(obj); ok {
												dest = dict.Get("D")
												break
											}
										}
									}
								}
							}
						}
					}
				}
			}
			var dest2 OutlineDest
			if dest != nil && !core.IsNullObject(dest) {
				if outlineDest, err := newOutlineDestFromPdfObject(dest, r); err == nil {
					dest2 = *outlineDest
				} else {
					common.Log.Debug("WARN: could not parse outline dest (%v): %v", dest, err)
				}
			}
			item = NewOutlineItem(item2.Title.Decoded(), dest2)
			*items = append(*items, item)
			if item2.Next != nil {
				fn(item2.Next, items)
			}
		}
		if node.First != nil {
			if item != nil {
				items = &item.Entries
			}
			fn(node.First, items)
		}
	}
	outline := NewOutline()
	fn(outlineTree, &outline.Entries)
	return outline, nil
}

// GetOutlinesFlattened returns a flattened list of tree nodes and titles.
// NOTE: for most use cases, it is recommended to use the high-level GetOutlines
// method instead, which also provides information regarding the destination
// of the outline items.
func (r *PdfReader) GetOutlinesFlattened() ([]*PdfOutlineTreeNode, []string, error) {
	var nodes []*PdfOutlineTreeNode
	var strs []string
	var fn func(*PdfOutlineTreeNode, *[]*PdfOutlineTreeNode, *[]string, int)
	fn = func(node *PdfOutlineTreeNode, nodes *[]*PdfOutlineTreeNode, strs *[]string, n int) {
		if node == nil {
			return
		}
		if node.context == nil {
			common.Log.Debug("ERROR: Missing node.context")
			return
		}
		item, ok := node.context.(*PdfOutlineItem)
		if ok {
			*nodes = append(*nodes, &item.PdfOutlineTreeNode)
			s := strings.Repeat(" ", n*2) + item.Title.Decoded()
			*strs = append(*strs, s)
		}
		if node.First != nil {
			s := strings.Repeat(" ", n*2) + "+"
			*strs = append(*strs, s)
			fn(node.First, nodes, strs, n+1)
		}
		if ok && item.Next != nil {
			fn(item.Next, nodes, strs, n)
		}
	}
	fn(r.outlineTree, &nodes, &strs, 0)
	return nodes, strs, nil
}
