package model

import (
	"bufio"
	"errors"
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

func (w *PdfWriter) setCatalogVersion() {
	w.catalog.Set("Version", core.MakeName(fmt.Sprintf("%d.%d", w.version.Major, w.version.Minor)))
}

func (w *PdfWriter) setDocumentIDs(id0, s string) {
	w.ids = core.MakeArray(core.MakeHexString(id0), core.MakeHexString(s))
}

// ApplyStandard is used to apply changes required on the document to match the rules required by the input standard.
// The writer's content would be changed after all the document parts are already established during the Write method.
// A good example of the StandardApplier could be a PDF/A Profile (i.e.: pdfa.Profile1A). In such a case PdfWriter would
// set up all rules required by that Profile.
func (w *PdfWriter) ApplyStandard(optimizer StandardApplier) { w.standardApplier = optimizer }

func (w *PdfWriter) canSkipCopy() bool {
	if w.appendMode {
		return false
	}
	if !w.skipObjectCopy {
		if w.crypter != nil || w.optimizer != nil || w.standardApplier != nil {
			return false
		}
	}
	return !w.hasOrphanPages()
}

// SetCatalogMarkInfo sets the catalog MarkInfo dictionary.
func (w *PdfWriter) SetCatalogMarkInfo(info core.PdfObject) error {
	if info == nil {
		w.catalog.Remove("MarkInfo")
		return nil
	}
	if ref, ok := info.(*core.PdfObjectReference); ok {
		info = ref.Resolve()
		if info == nil {
			w.catalog.Remove("MarkInfo")
			return nil
		}
	}
	if _, ok := info.(*core.PdfIndirectObject); ok {
		w.addObject(info)
	}
	w.catalog.Set("MarkInfo", info)
	return nil
}

func (w *PdfWriter) adjustXRefAffectedVersion(ok bool) {
	if ok && w.version.Major == 1 && w.version.Minor < 5 {
		w.version.Minor = 5
	}
}

func (w *PdfWriter) setDocInfo(obj core.PdfObject) {
	if w.hasObject(w.infoObj) {
		delete(w.hasObjects, w.infoObj)
		delete(w.traversed, w.infoObj)
		for i, object := range w.objects {
			if object == w.infoObj {
				copy(w.objects[i:], w.objects[i+1:])
				w.objects[len(w.objects)-1] = nil
				w.objects = w.objects[:len(w.objects)-1]
				break
			}
		}
	}
	indirect := core.PdfIndirectObject{
		PdfObject: obj,
	}
	w.infoObj = &indirect
	w.addObject(&indirect)
}

func (w *PdfWriter) copyObject(obj core.PdfObject, objByObj map[core.PdfObject]core.PdfObject, set map[core.PdfObject]struct{}, ok bool) core.PdfObject {
	found := !w.appendMode && set != nil
	if obj2, isObjByObj := objByObj[obj]; isObjByObj {
		if found && !ok {
			delete(set, obj)
		}
		return obj2
	}
	if obj == nil {
		null := core.MakeNull()
		return null
	}
	obj2 := obj
	switch v := obj.(type) {
	case *core.PdfObjectArray:
		array := core.MakeArray()
		obj2 = array
		objByObj[obj] = obj2
		for _, obj := range v.Elements() {
			array.Append(w.copyObject(obj, objByObj, set, ok))
		}
	case *core.PdfObjectStreams:
		objStreams := &core.PdfObjectStreams{PdfObjectReference: v.PdfObjectReference}
		obj2 = objStreams
		objByObj[obj] = obj2
		for _, obj := range v.Elements() {
			objStreams.Append(w.copyObject(obj, objByObj, set, ok))
		}
	case *core.PdfObjectStream:
		stream := &core.PdfObjectStream{Stream: v.Stream, PdfObjectReference: v.PdfObjectReference, Lazy: v.Lazy, TempFile: v.TempFile}
		if v.Lazy {
			if w.lazySources == nil {
				w.lazySources = map[*core.PdfObjectStream]*core.PdfObjectStream{}
			}
			w.lazySources[stream] = v
		}
		obj2 = stream
		objByObj[obj] = obj2
		stream.PdfObjectDictionary = w.copyObject(v.PdfObjectDictionary, objByObj, set, ok).(*core.PdfObjectDictionary)
	case *core.PdfObjectDictionary:
		var done bool
		if found && !ok {
			if s, _ := core.GetNameVal(v.Get("Type")); s == "Page" {
				_, valid := w.pageDicts[v]
				ok = !valid
				done = ok
			}
		}
		dict := core.MakeDict()
		obj2 = dict
		objByObj[obj] = obj2
		for _, name := range v.Keys() {
			dict.Set(name, w.copyObject(v.Get(name), objByObj, set, ok))
		}
		if done {
			obj2 = core.MakeNull()
			ok = false
		}
	case *core.PdfIndirectObject:
		indirect := &core.PdfIndirectObject{PdfObjectReference: v.PdfObjectReference}
		obj2 = indirect
		objByObj[obj] = obj2
		indirect.PdfObject = w.copyObject(v.PdfObject, objByObj, set, ok)
	case *core.PdfObjectString:
		if w.crypter != nil {
			str := *v
			obj2 = &str
			objByObj[obj] = obj2
		} else {
			obj2 = obj
		}
	case *core.PdfObjectName, *core.PdfObjectNull, *core.PdfObjectInteger, *core.PdfObjectFloat, *core.PdfObjectBool:
		obj2 = obj
	case *core.PdfObjectReference:
		ref := *v
		obj2 = &ref
		objByObj[obj] = obj2
	case *pdfSignDictionary:
		signDict := &pdfSignDictionary{PdfObjectDictionary: core.MakeDict(), handler: v.handler, signature: v.signature}
		obj2 = signDict
		objByObj[obj] = obj2
		for _, name := range v.Keys() {
			signDict.Set(name, w.copyObject(v.Get(name), objByObj, set, ok))
		}
	default:
		common.Log.Info("TODO(a5i): implement copyObject for %+v", obj)
	}
	if found && ok {
		set[obj] = struct{}{}
	}
	return obj2
}

func (w *PdfWriter) AttachFile(file *EmbeddedFile) error {
	names := w.names
	if names == nil {
		names = newNames()
	}
	err := names.addEmbeddedFile(file)
	if err != nil {
		return err
	}
	w.names = names
	return nil
}

// PdfWriter handles outputing PDF content.
type PdfWriter struct {
	root                 *core.PdfIndirectObject
	pages                *core.PdfIndirectObject
	pageDicts            map[core.PdfObject]struct{}
	pageObjs             []*core.PdfIndirectObject
	objects              []core.PdfObject
	hasObjects           map[core.PdfObject]struct{}
	spareObjs            []*core.PdfIndirectObject
	outlineTree          *PdfOutlineTreeNode
	lazySources          map[*core.PdfObjectStream]*core.PdfObjectStream
	lazyStreams          []*core.PdfObjectStream
	catalog              *core.PdfObjectDictionary
	spareObjects         []core.PdfObject
	infoObj              *core.PdfIndirectObject
	bufWriter            *bufio.Writer
	writePos             int64
	writeErr             error
	crypter              *core.PdfCrypt
	encryptDict          *core.PdfObjectDictionary
	encryptObj           *core.PdfIndirectObject
	ids                  *core.PdfObjectArray
	version              core.Version
	useObjectStreams     *bool
	missingParents       map[core.PdfObject][]*core.PdfObjectDictionary
	acroForm             *PdfAcroForm
	names                *Names
	optimizer            Optimizer
	standardApplier      StandardApplier
	skipObjectCopy       bool
	crossReferences      map[int]crossReference
	startOffset          int64
	ObjNumOffset         int
	appendMode           bool
	prevXrefs            core.XrefTable
	prevXrefOffset       int64
	originalSize         int64
	objNumMap            map[core.PdfObject]int64
	traversed            map[core.PdfObject]struct{}
	fileName             string
	outputIntents        []*PdfOutputIntent
	useHashBasedID       bool
	id0, id1             string
	linearized           bool
	linearizationOptions LinearizationOptions
	written              bool
	writeFailed          bool
	copied               bool
	encrypted            bool
}

// SetDocInfo set document info.
// This will overwrite any globally declared document info.
func (w *PdfWriter) SetDocInfo(info *PdfInfo) { w.setDocInfo(info.ToPdfObject()) }

// SetCatalogViewerPreferences sets the catalog ViewerPreferences dictionary.
func (w *PdfWriter) SetCatalogViewerPreferences(pref core.PdfObject) error {
	if pref == nil {
		w.catalog.Remove("ViewerPreferences")
		return nil
	}
	if ref, ok := pref.(*core.PdfObjectReference); ok {
		pref = ref.Resolve()
		if pref == nil {
			w.catalog.Remove("ViewerPreferences")
			return nil
		}
	}
	if _, ok := pref.(*core.PdfIndirectObject); ok {
		w.addObject(pref)
	}
	w.catalog.Set("ViewerPreferences", pref)
	return nil
}

func (w *PdfWriter) checkCrossReferenceStream() bool {
	ok := w.version.Major > 1 || (w.version.Major == 1 && w.version.Minor > 4)
	if w.useObjectStreams != nil {
		ok = *w.useObjectStreams
	}
	return ok
}

// SetCatalogStructTreeRoot sets the catalog struct tree root object.
func (w *PdfWriter) SetCatalogStructTreeRoot(tree core.PdfObject) error {
	if tree == nil {
		w.catalog.Remove("StructTreeRoot")
		return nil
	}
	w.catalog.Set("StructTreeRoot", tree)
	return w.addObjects(tree)
}

func (w *PdfWriter) copyObjects() {
	w.copied = true
	objByObj := make(map[core.PdfObject]core.PdfObject)
	objs := make([]core.PdfObject, 0, len(w.objects))
	set := make(map[core.PdfObject]struct{}, len(w.objects))
	set2 := make(map[core.PdfObject]struct{})
	for _, object := range w.objects {
		copyObject := w.copyObject(object, objByObj, set2, false)
		if _, ok := set2[object]; ok {
			continue
		}
		objs = append(objs, copyObject)
		set[copyObject] = struct{}{}
	}
	w.objects = objs
	w.hasObjects = set
	w.infoObj = w.copyObject(w.infoObj, objByObj, nil, false).(*core.PdfIndirectObject)
	w.root = w.copyObject(w.root, objByObj, nil, false).(*core.PdfIndirectObject)
	if w.encryptObj != nil {
		w.encryptObj = w.copyObject(w.encryptObj, objByObj, nil, false).(*core.PdfIndirectObject)
	}
	if w.appendMode {
		numByObj := make(map[core.PdfObject]int64)
		for obj, objNumMap := range w.objNumMap {
			if obj2, ok := objByObj[obj]; ok {
				numByObj[obj2] = objNumMap
			} else {
				common.Log.Debug("ERROR: append mode - object copy not in map")
			}
		}
		w.objNumMap = numByObj
	}
}

// SetCatalogLanguage sets the catalog language.
func (w *PdfWriter) SetCatalogLanguage(lang core.PdfObject) error {
	if lang == nil {
		w.catalog.Remove("Lang")
		return nil
	}
	if _, ok := lang.(*core.PdfIndirectObject); ok {
		w.addObject(lang)
	}
	w.catalog.Set("Lang", lang)
	return nil
}

func (w *PdfWriter) checkPendingObjects() {
	for obj, missingParent := range w.missingParents {
		if !w.hasObject(obj) {
			common.Log.Debug("WARN Pending object %+v %T (%p) never added for writing", obj, obj, obj)
			for _, dict := range missingParent {
				for _, name := range dict.Keys() {
					obj2 := dict.Get(name)
					if obj2 == obj {
						common.Log.Debug("Pending object found! and replaced with null")
						dict.Set(name, core.MakeNull())
						break
					}
				}
			}
		}
	}
}

// SetCatalogMetadata sets the catalog metadata (XMP) stream object.
func (w *PdfWriter) SetCatalogMetadata(meta core.PdfObject) error {
	if meta == nil {
		w.catalog.Remove("Metadata")
		return nil
	}
	metaStream, ok := core.GetStream(meta)
	if !ok {
		return errors.New("catalog metadata must be a valid stream")
	}
	w.addObject(metaStream)
	w.catalog.Set("Metadata", metaStream)
	return nil
}

// NewPdfWriter initializes a new PdfWriter.
func NewPdfWriter() PdfWriter {
	writer := PdfWriter{}
	writer.hasObjects = map[core.PdfObject]struct{}{}
	writer.objects = []core.PdfObject{}
	writer.missingParents = map[core.PdfObject][]*core.PdfObjectDictionary{}
	writer.traversed = map[core.PdfObject]struct{}{}
	writer.version.Major = 1
	writer.version.Minor = 3
	dict := core.MakeDict()
	sts := []struct {
		key   core.PdfObjectName
		value string
	}{{"Producer", getPdfProducer()}, {"Creator", getPdfCreator()}, {"Author", getPdfAuthor()}, {"Subject", getPdfSubject()}, {"Title", getPdfTitle()}, {"Keywords", getPdfKeywords()}}
	for _, st := range sts {
		if st.value != "" {
			dict.Set(st.key, core.MakeString(st.value))
		}
	}
	if creationDate := getCreationDate(); !creationDate.IsZero() {
		if date, err := NewPdfDateFromTime(creationDate); err == nil {
			dict.Set("CreationDate", date.ToPdfObject())
		}
	}
	if modifiedDate := getModifiedDate(); !modifiedDate.IsZero() {
		if date, err := NewPdfDateFromTime(modifiedDate); err == nil {
			dict.Set("ModDate", date.ToPdfObject())
		}
	}
	indirect := core.PdfIndirectObject{
		PdfObject: dict,
	}
	writer.infoObj = &indirect
	writer.addObject(&indirect)
	indirect2 := core.PdfIndirectObject{}
	dict2 := core.MakeDict()
	dict2.Set("Type", core.MakeName("Catalog"))
	indirect2.PdfObject = dict2
	writer.root = &indirect2
	writer.addObject(writer.root)
	indirect3 := core.PdfIndirectObject{}
	dict3 := core.MakeDict()
	dict3.Set("Type", core.MakeName("Pages"))
	arr := core.PdfObjectArray{}
	dict3.Set("Kids", &arr)
	dict3.Set("Count", core.MakeInteger(0))
	indirect3.PdfObject = dict3
	writer.pages = &indirect3
	writer.pageDicts = map[core.PdfObject]struct{}{}
	writer.pageObjs = []*core.PdfIndirectObject{}
	writer.addObject(writer.pages)
	dict2.Set("Pages", &indirect3)
	writer.catalog = dict2
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Catalog %s", indirect2)
	}
	return writer
}
