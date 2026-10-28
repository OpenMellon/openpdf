package model

import (
	"errors"
	"fmt"
	_ "image/gif"
	"io"
	"os"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/core/security"
)

func (r *PdfReader) newPdfAnnotationPolyLineFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationPolyLine, error) {
	polyLine := PdfAnnotationPolyLine{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	polyLine.PdfAnnotationMarkup = markup
	polyLine.Vertices = dict.Get("Vertices")
	polyLine.LE = dict.Get("LE")
	polyLine.BS = dict.Get("BS")
	polyLine.IC = dict.Get("IC")
	polyLine.BE = dict.Get("BE")
	polyLine.IT = dict.Get("IT")
	polyLine.Measure = dict.Get("Measure")
	return &polyLine, nil
}

func (r *PdfReader) newPdfAnnotationSoundFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationSound, error) {
	sound := PdfAnnotationSound{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	sound.PdfAnnotationMarkup = markup
	sound.Name = dict.Get("Name")
	sound.Sound = dict.Get("Sound")
	return &sound, nil
}

func (r *PdfReader) newPdfAnnotationSquareFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationSquare, error) {
	square := PdfAnnotationSquare{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	square.PdfAnnotationMarkup = markup
	square.BS = dict.Get("BS")
	square.IC = dict.Get("IC")
	square.BE = dict.Get("BE")
	square.RD = dict.Get("RD")
	return &square, nil
}

func (r *PdfReader) newPdfAnnotationHighlightFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationHighlight, error) {
	highlight := PdfAnnotationHighlight{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	highlight.PdfAnnotationMarkup = markup
	highlight.QuadPoints = dict.Get("QuadPoints")
	return &highlight, nil
}

func (r *PdfReader) newPdfAnnotationTrapNetFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationTrapNet, error) {
	trapNet := PdfAnnotationTrapNet{}
	return &trapNet, nil
}

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

// GetCatalogMetadata gets the catalog defined XMP Metadata.
func (r *PdfReader) GetCatalogMetadata() (core.PdfObject, bool) {
	if r.catalog == nil {
		return nil, false
	}
	metadata := r.catalog.Get("Metadata")
	return metadata, metadata != nil
}

// GetCatalogStructTreeRoot gets the catalog StructTreeRoot object.
func (r *PdfReader) GetCatalogStructTreeRoot() (core.PdfObject, bool) {
	structTreeRoot := core.ResolveReference(r.catalog.Get("StructTreeRoot"))
	if structTreeRoot == nil {
		return nil, false
	}
	if !r.lazy {
		err := r.traverseObjectData(structTreeRoot)
		if err != nil {
			common.Log.Debug("ERROR: Failed to traverse StructTreeRoot object (%s)", err)
			return nil, false
		}
	}
	return structTreeRoot, true
}

func (r *PdfReader) loadAnnotations(annots core.PdfObject) ([]*PdfAnnotation, error) {
	annotsArr, ok := core.GetArray(annots)
	if !ok {
		return nil, fmt.Errorf("Annots not an array")
	}
	var annots2 []*PdfAnnotation
	for _, obj := range annotsArr.Elements() {
		obj = core.ResolveReference(obj)
		if _, ok := obj.(*core.PdfObjectNull); ok {
			continue
		}
		dict, ok := obj.(*core.PdfObjectDictionary)
		indirect, isIndirect := obj.(*core.PdfIndirectObject)
		if ok {
			indirect = &core.PdfIndirectObject{}
			indirect.PdfObject = dict
		} else {
			if !isIndirect {
				return nil, fmt.Errorf("annotation not in an indirect object")
			}
		}
		annot, err := r.newPdfAnnotationFromIndirectObject(indirect)
		if err != nil {
			return nil, err
		}
		switch v := annot.GetContext().(type) {
		case *PdfAnnotationWidget:
			for _, allField := range r.AcroForm.AllFields() {
				if allField.primitive == v.Parent {
					v.field = allField
					break
				}
			}
		}
		if annot != nil {
			annots2 = append(annots2, annot)
		}
	}
	return annots2, nil
}

// NewPdfReaderAt is the explicit-size form of NewPdfReader. Use this when
// the source's byte length cannot be auto-detected — e.g. a custom io.ReaderAt
// that doesn't implement Size/Seek/Stat.
func NewPdfReaderAt(r io.ReaderAt, size int64) (*PdfReader, error) {
	return newPdfReaderTracked(r, size, &ReaderOpts{}, false, false)
}

// NewPdfReader returns a new PdfReader for the given io.ReaderAt source.
// The source's byte length is auto-detected via Size() / Seek() / Stat() —
// *os.File, *bytes.Reader, and *io.SectionReader all work without a manual
// size argument. For exotic io.ReaderAt sources that can't be auto-sized,
// use NewPdfReaderAt.
//
// Immediately loads and traverses the PDF structure including pages and page
// contents (if not encrypted). Loads entire document structure into memory.
// Alternatively, a lazy-loading reader can be created with NewPdfReaderLazy
// which loads references from disk on demand.
func NewPdfReader(r io.ReaderAt) (*PdfReader, error) {
	sizeOfReaderAt, err := core.SizeOfReaderAt(r)
	if err != nil {
		return nil, err
	}
	return NewPdfReaderAt(r, sizeOfReaderAt)
}

// GetCatalogMarkInfo gets catalog MarkInfo object.
func (r *PdfReader) GetCatalogMarkInfo() (core.PdfObject, bool) {
	if r.catalog == nil {
		return nil, false
	}
	markInfo := r.catalog.Get("MarkInfo")
	return markInfo, markInfo != nil
}

// GetCatalogViewerPreferences gets catalog ViewerPreferences object.
func (r *PdfReader) GetCatalogViewerPreferences() (core.PdfObject, bool) {
	if r.catalog == nil {
		return nil, false
	}
	viewerPreferences := r.catalog.Get("ViewerPreferences")
	return viewerPreferences, viewerPreferences != nil
}

// GetCatalogVersion gets the catalog /Version entry. Per ISO 32000-2 §7.7.2
// (Table 29) this entry supersedes the file header version only when it is
// later than the header; callers must apply that max rule themselves (this
// accessor just returns the raw entry).
func (r *PdfReader) GetCatalogVersion() (core.PdfObject, bool) {
	if r.catalog == nil {
		return nil, false
	}
	version := r.catalog.Get("Version")
	return version, version != nil
}

func newPdfReaderCore(r io.ReaderAt, opts *ReaderOpts, autoDecrypt, ok bool) (*PdfReader, error) {
	if opts == nil {
		opts = NewReaderOpts()
	}
	s2 := ""
	if file, ok := r.(*os.File); ok {
		s2 = file.Name()
	}
	var rs io.ReadSeeker
	if rs2, ok := r.(io.ReadSeeker); ok {
		rs = rs2
	}
	opts2 := *opts
	reader := &PdfReader{readSeeker: rs, traversed: map[core.PdfObject]struct{}{}, modelManager: newModelManager(), lazy: opts.LazyLoad, complianceMode: opts.ComplianceMode, skipOutlines: ok, skipPerms: ok, autoDecrypt: autoDecrypt, opts: &opts2, fileName: s2}
	return reader, nil
}

// AcroFormNeedsRepair returns true if the document contains widget annotations
// linked to fields which are not referenced in the AcroForm. The AcroForm can
// be repaired using the RepairAcroForm method of the reader.
func (r *PdfReader) AcroFormNeedsRepair() (bool, error) {
	var fields []*PdfField
	if r.AcroForm != nil {
		fields = r.AcroForm.AllFields()
	}
	set := make(map[*PdfField]struct{}, len(fields))
	for _, field := range fields {
		set[field] = struct{}{}
	}
	for _, page := range r.PageList {
		annotations, err := page.GetAnnotations()
		if err != nil {
			return false, err
		}
		for _, annotation := range annotations {
			widget, ok := annotation.GetContext().(*PdfAnnotationWidget)
			if !ok {
				continue
			}
			field := widget.Field()
			if field == nil {
				return true, nil
			}
			if _, ok := set[field]; !ok {
				return true, nil
			}
		}
	}
	return false, nil
}

// GetCatalogLanguage gets catalog Language object.
func (r *PdfReader) GetCatalogLanguage() (core.PdfObject, bool) {
	if r.catalog == nil {
		return nil, false
	}
	lang := r.catalog.Get("Lang")
	return lang, lang != nil
}

func (r *PdfReader) newPdfAcroFormFromDict(indirect *core.PdfIndirectObject, dict *core.PdfObjectDictionary) (*PdfAcroForm, error) {
	acroForm := NewPdfAcroForm()
	if indirect != nil {
		acroForm.primitive = indirect
		indirect.PdfObject = core.MakeDict()
	}
	if fields := dict.Get("Fields"); fields != nil && !core.IsNullObject(fields) {
		fieldsArr, ok := core.GetArray(fields)
		if !ok {
			return nil, fmt.Errorf("fields not an array (%T)", fields)
		}
		var fields []*PdfField
		for _, obj := range fieldsArr.Elements() {
			indirect, ok := core.GetIndirect(obj)
			if !ok {
				if _, ok := obj.(*core.PdfObjectNull); ok {
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("Skipping over null field")
					}
					continue
				}
				common.Log.Debug("Field not contained in indirect object %T", obj)
				return nil, fmt.Errorf("field not in an indirect object")
			}
			field, err := r.newPdfFieldFromIndirectObject(indirect, nil)
			if err != nil {
				return nil, err
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("AcroForm Field: %+v", *field)
			}
			fields = append(fields, field)
		}
		acroForm.Fields = &fields
	}
	if needAppearances := dict.Get("NeedAppearances"); needAppearances != nil {
		needAppearancesBool, ok := core.GetBool(needAppearances)
		if ok {
			acroForm.NeedAppearances = needAppearancesBool
		} else {
			common.Log.Debug("ERROR: NeedAppearances invalid (got %T)", needAppearances)
		}
	}
	if sigFlags := dict.Get("SigFlags"); sigFlags != nil {
		sigFlagsInt, ok := core.GetInt(sigFlags)
		if ok {
			acroForm.SigFlags = sigFlagsInt
		} else {
			common.Log.Debug("ERROR: SigFlags invalid (got %T)", sigFlags)
		}
	}
	if co := dict.Get("CO"); co != nil {
		coArr, ok := core.GetArray(co)
		if ok {
			acroForm.CO = coArr
		} else {
			common.Log.Debug("ERROR: CO invalid (got %T)", co)
		}
	}
	if dr := dict.Get("DR"); dr != nil {
		if drDict, ok := core.GetDict(dr); ok {
			resources, err := NewPdfPageResourcesFromDict(drDict)
			if err != nil {
				common.Log.Error("Invalid DR: %v", err)
				return nil, err
			}
			acroForm.DR = resources
		} else {
			common.Log.Debug("ERROR: DR invalid (got %T)", dr)
		}
	}
	if da := dict.Get("DA"); da != nil {
		daStr, ok := core.GetString(da)
		if ok {
			acroForm.DA = daStr
		} else {
			common.Log.Debug("ERROR: DA invalid (got %T)", da)
		}
	}
	if q := dict.Get("Q"); q != nil {
		qInt, ok := core.GetInt(q)
		if ok {
			acroForm.Q = qInt
		} else {
			common.Log.Debug("ERROR: Q invalid (got %T)", q)
		}
	}
	if xfa := dict.Get("XFA"); xfa != nil {
		acroForm.XFA = xfa
	}
	if adbeEchoSign := dict.Get("ADBE_EchoSign"); adbeEchoSign != nil {
		acroForm.ADBEEchoSign = adbeEchoSign
	}
	acroForm.ToPdfObject()
	return acroForm, nil
}

// CheckAccessRights checks access rights and permissions for a specified password.  If either user/owner
// password is specified,  full rights are granted, otherwise the access rights are specified by the
// Permissions flag.
//
// The bool flag indicates that the user can access and view the file.
// The AccessPermissions shows what access the user has for editing etc.
// An error is returned if there was a problem performing the authentication.
//
// For AES-256 the password is prepared as described on Decrypt.
func (r *PdfReader) CheckAccessRights(password []byte) (bool, security.Permissions, error) {
	return r.parser.CheckAccessRights(password)
}
