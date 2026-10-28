package model

import (
	"errors"
	"fmt"
	_ "image/gif"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

func (r *PdfReader) loadStructure() error {
	if r.parser.GetCrypter() != nil && !r.parser.IsAuthenticated() {
		return fmt.Errorf("file need to be decrypted first")
	}
	trailer := r.parser.GetTrailer()
	if trailer == nil {
		return fmt.Errorf("missing trailer")
	}
	ref, ok := trailer.Get("Root").(*core.PdfObjectReference)
	if !ok {
		return fmt.Errorf("invalid Root (trailer: %s)", trailer)
	}
	lookupByReference, err := r.parser.LookupByReference(*ref)
	if err != nil {
		common.Log.Debug("ERROR: Failed to read root element catalog: %s", err)
		return err
	}
	indirect, ok := lookupByReference.(*core.PdfIndirectObject)
	if !ok {
		common.Log.Debug("ERROR: Missing catalog: (root %q) (trailer %s)", lookupByReference, trailer)
		return errors.New("missing catalog")
	}
	dict, ok := (*indirect).PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Invalid catalog (%s)", indirect.PdfObject)
		return errors.New("invalid catalog")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Catalog: %s", dict)
	}
	ref2, ok := dict.Get("Pages").(*core.PdfObjectReference)
	if !ok {
		return errors.New("pages in catalog should be a reference")
	}
	obj, err := r.parser.LookupByReference(*ref2)
	if err != nil {
		common.Log.Debug("ERROR: Failed to read pages")
		return err
	}
	indirect2, ok := obj.(*core.PdfIndirectObject)
	if !ok {
		common.Log.Debug("ERROR: Pages object invalid")
		common.Log.Debug("op: %p", indirect2)
		return errors.New("pages object invalid")
	}
	dict2, ok := indirect2.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Pages object invalid (%s)", indirect2)
		return errors.New("pages object invalid")
	}
	count, ok := core.GetInt(dict2.Get("Count"))
	if !ok {
		common.Log.Debug("ERROR: Pages count object invalid")
		return errors.New("pages count invalid")
	}
	if _, ok = core.GetName(dict2.Get("Type")); !ok {
		common.Log.Debug("Pages dict Type field not set. Setting Type to Pages.")
		dict2.Set("Type", core.MakeName("Pages"))
	}
	if rotate, ok := core.GetInt(dict2.Get("Rotate")); ok {
		n := int64(*rotate)
		r.Rotate = &n
	}
	r.root = ref
	r.catalog = dict
	r.pagesDict = dict2
	r.pagesObj = indirect2
	r.pageCount = int(*count)
	r.pageObjs = []*core.PdfIndirectObject{}
	set := map[core.PdfObject]struct{}{}
	err = r.buildPageList(indirect2, nil, set)
	if err != nil {
		return err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("---")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("TOC")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Pages")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("%d: %s", len(r.pageObjs), r.pageObjs)
	}
	if !r.skipOutlines {
		r.outlineTree, err = r.loadOutlines()
		if err != nil {
			common.Log.Debug("ERROR: Failed to build outline tree (%s)", err)
			return err
		}
	}
	r.AcroForm, err = r.loadForms()
	if err != nil {
		return err
	}
	r.DSS, err = r.loadDSS()
	if err != nil {
		return err
	}
	if !r.skipPerms {
		r.perms, err = r.loadPerms()
		if err != nil {
			return err
		}
	}
	return nil
}

// GetRevision returns the specific version of the PdfReader for the current Pdf document
func (r *PdfReader) GetRevision(revisionNumber int) (*PdfReader, error) {
	n := r.parser.GetRevisionNumber()
	if revisionNumber < 0 || revisionNumber > n {
		return nil, errors.New("wrong revision number")
	}
	if revisionNumber == n {
		return r, nil
	}
	if r.revisions[revisionNumber] != nil {
		return r.revisions[revisionNumber], nil
	}
	reader := r
	for i := n - 1; i >= revisionNumber; i-- {
		previousRevision, err := reader.GetPreviousRevision()
		if err != nil {
			return nil, err
		}
		r.revisions[i] = previousRevision
		reader = previousRevision
	}
	return reader, nil
}

// RepairAcroForm attempts to rebuild the AcroForm fields using the widget
// annotations present in the document pages. Pass nil for the opts parameter
// in order to use the default options.
// NOTE: Currently, the opts parameter is declared in order to enable adding
// future options, but passing nil will always result in the default options
// being used.
func (r *PdfReader) RepairAcroForm(opts *AcroFormRepairOptions) error {
	var fields []*PdfField
	set := map[*core.PdfIndirectObject]struct{}{}
	for _, page := range r.PageList {
		annotations, err := page.GetAnnotations()
		if err != nil {
			return err
		}
		for _, annotation := range annotations {
			var field *PdfField
			switch v := annotation.GetContext().(type) {
			case *PdfAnnotationWidget:
				if v.field != nil {
					field = v.field
					break
				}
				if parentIndirect, ok := core.GetIndirect(v.Parent); ok {
					field, err = r.newPdfFieldFromIndirectObject(parentIndirect, nil)
					if err == nil {
						break
					}
					common.Log.Debug("WARN: could not parse form field %+v: %v", parentIndirect, err)
				}
				if v.primitive != nil {
					field, err = r.newPdfFieldFromIndirectObject(v.primitive, nil)
					if err == nil {
						break
					}
					common.Log.Debug("WARN: could not parse form field %+v: %v", v.primitive, err)
				}
			}
			if field == nil {
				continue
			}
			if _, ok := set[field.primitive]; ok {
				continue
			}
			set[field.primitive] = struct{}{}
			fields = append(fields, field)
		}
	}
	if len(fields) == 0 {
		return nil
	}
	if r.AcroForm == nil {
		r.AcroForm = NewPdfAcroForm()
	}
	r.AcroForm.Fields = &fields
	return nil
}

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

func (r *PdfReader) newPdfPageFromDict(dict *core.PdfObjectDictionary) (*PdfPage, error) {
	page := NewPdfPage()
	page.pageDict = dict
	page.originalDict = dict.ShallowCopy()
	name, ok := dict.Get("Type").(*core.PdfObjectName)
	if !ok {
		return nil, errors.New("missing/invalid Page dictionary Type")
	}
	if *name != "Page" {
		return nil, errors.New("page dictionary Type != Page")
	}
	if parent := dict.Get("Parent"); parent != nil {
		page.Parent = parent
	}
	if lastModified := dict.Get("LastModified"); lastModified != nil {
		lastModifiedStr, ok := core.GetString(lastModified)
		if !ok {
			return nil, errors.New("page dictionary LastModified != string")
		}
		date, err := NewPdfDate(lastModifiedStr.Str())
		if err != nil {
			return nil, err
		}
		page.LastModified = &date
	}
	if resources := dict.Get("Resources"); resources != nil && !core.IsNullObject(resources) {
		resourcesDict, ok := core.GetDict(resources)
		if !ok {
			return nil, fmt.Errorf("invalid resource dictionary (%T)", resources)
		}
		var err error
		page.Resources, err = NewPdfPageResourcesFromDict(resourcesDict)
		if err != nil {
			return nil, err
		}
	} else {
		parentResources, err := page.getParentResources()
		if err != nil {
			return nil, err
		}
		if parentResources == nil {
			parentResources = NewPdfPageResources()
		}
		page.Resources = parentResources
	}
	if mediaBox := dict.Get("MediaBox"); mediaBox != nil {
		mediaBoxArr, ok := core.GetArray(mediaBox)
		if !ok {
			return nil, errors.New("page MediaBox not an array")
		}
		var err error
		page.MediaBox, err = NewPdfRectangle(*mediaBoxArr)
		if err != nil {
			return nil, err
		}
	}
	if cropBox := dict.Get("CropBox"); cropBox != nil {
		cropBoxArr, ok := core.GetArray(cropBox)
		if !ok {
			return nil, errors.New("page CropBox not an array")
		}
		var err error
		page.CropBox, err = NewPdfRectangle(*cropBoxArr)
		if err != nil {
			return nil, err
		}
	}
	if bleedBox := dict.Get("BleedBox"); bleedBox != nil {
		bleedBoxArr, ok := core.GetArray(bleedBox)
		if !ok {
			return nil, errors.New("page BleedBox not an array")
		}
		var err error
		page.BleedBox, err = NewPdfRectangle(*bleedBoxArr)
		if err != nil {
			return nil, err
		}
	}
	if trimBox := dict.Get("TrimBox"); trimBox != nil {
		trimBoxArr, ok := core.GetArray(trimBox)
		if !ok {
			return nil, errors.New("page TrimBox not an array")
		}
		var err error
		page.TrimBox, err = NewPdfRectangle(*trimBoxArr)
		if err != nil {
			return nil, err
		}
	}
	if artBox := dict.Get("ArtBox"); artBox != nil {
		artBoxArr, ok := core.GetArray(artBox)
		if !ok {
			return nil, errors.New("page ArtBox not an array")
		}
		var err error
		page.ArtBox, err = NewPdfRectangle(*artBoxArr)
		if err != nil {
			return nil, err
		}
	}
	if boxColorInfo := dict.Get("BoxColorInfo"); boxColorInfo != nil {
		page.BoxColorInfo = boxColorInfo
	}
	if contents := dict.Get("Contents"); contents != nil {
		page.Contents = contents
	}
	if rotate := dict.Get("Rotate"); rotate != nil {
		n, err := core.GetNumberAsInt64(rotate)
		if err != nil {
			return nil, errors.New("invalid Page Rotate object")
		}
		page.Rotate = &n
	}
	if group := dict.Get("Group"); group != nil {
		page.Group = group
	}
	if thumb := dict.Get("Thumb"); thumb != nil {
		page.Thumb = thumb
	}
	if b := dict.Get("B"); b != nil {
		page.B = b
	}
	if dur := dict.Get("Dur"); dur != nil {
		page.Dur = dur
	}
	if trans := dict.Get("Trans"); trans != nil {
		page.Trans = trans
	}
	if aa := dict.Get("AA"); aa != nil {
		page.AA = aa
	}
	if metadata := dict.Get("Metadata"); metadata != nil {
		page.Metadata = metadata
	}
	if pieceInfo := dict.Get("PieceInfo"); pieceInfo != nil {
		page.PieceInfo = pieceInfo
	}
	if structParents := dict.Get("StructParents"); structParents != nil {
		page.StructParents = structParents
	}
	if id := dict.Get("ID"); id != nil {
		page.ID = id
	}
	if pz := dict.Get("PZ"); pz != nil {
		page.PZ = pz
	}
	if separationInfo := dict.Get("SeparationInfo"); separationInfo != nil {
		page.SeparationInfo = separationInfo
	}
	if tabs := dict.Get("Tabs"); tabs != nil {
		page.Tabs = tabs
	}
	if templateInstantiated := dict.Get("TemplateInstantiated"); templateInstantiated != nil {
		page.TemplateInstantiated = templateInstantiated
	}
	if presSteps := dict.Get("PresSteps"); presSteps != nil {
		page.PresSteps = presSteps
	}
	if userUnit := dict.Get("UserUnit"); userUnit != nil {
		page.UserUnit = userUnit
	}
	if vp := dict.Get("VP"); vp != nil {
		page.VP = vp
	}
	if annots := dict.Get("Annots"); annots != nil {
		page.Annots = annots
	}
	page.reader = r
	return page, nil
}

// GetPageLabels returns the PageLabels entry in the PDF catalog.
// See section 12.4.2 "Page Labels" (p. 382 PDF32000_2008).
func (r *PdfReader) GetPageLabels() (core.PdfObject, error) {
	pageLabels := core.ResolveReference(r.catalog.Get("PageLabels"))
	if pageLabels == nil {
		return nil, nil
	}
	if !r.lazy {
		err := r.traverseObjectData(pageLabels)
		if err != nil {
			return nil, err
		}
	}
	return pageLabels, nil
}

// GetPage returns the PdfPage model for the specified page number.
func (r *PdfReader) GetPage(pageNumber int) (*PdfPage, error) {
	if r.parser.GetCrypter() != nil && !r.parser.IsAuthenticated() {
		return nil, fmt.Errorf("file needs to be decrypted first")
	}
	if len(r.pageObjs) < pageNumber {
		return nil, errors.New("invalid page number (page count too short)")
	}
	n := pageNumber - 1
	if n < 0 {
		return nil, fmt.Errorf("page numbering must start at 1")
	}
	page := r.PageList[n]
	return page, nil
}

// PageFromIndirectObject returns the PdfPage and page number for a given indirect object.
func (r *PdfReader) PageFromIndirectObject(ind *core.PdfIndirectObject) (*PdfPage, int, error) {
	if len(r.PageList) != len(r.pageObjs) {
		return nil, 0, errors.New("page list invalid")
	}
	for i, pageObj := range r.pageObjs {
		if pageObj == ind {
			return r.PageList[i], i + 1, nil
		}
	}
	return nil, 0, errors.New("page not found")
}

// ValidateLinearization validates the PDF's linearization structure against
// ISO 32000-2 Annex F, returning a slice of violations (empty if it passes).
// Returns [core.LinErrNotLinearized] if the PDF is not linearized.
func (r *PdfReader) ValidateLinearization() ([]core.LinearizationViolation, error) {
	return r.parser.ValidateLinearization()
}

// ValidateLinearizationTrailerID runs only the linearization /ID consistency
// check (first-xref trailer vs main trailer), without the full Annex F battery.
// Returns nil if the PDF is not linearized or the /ID values match.
func (r *PdfReader) ValidateLinearizationTrailerID() *core.LinearizationViolation {
	return r.parser.ValidateLinearizationTrailerID()
}
