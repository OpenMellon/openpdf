package model

import (
	"bufio"
	"errors"
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

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

func (w *PdfWriter) computeLinearizationLayout() (*linearizationLayout, error) {
	if len(w.pageObjs) == 0 {
		return nil, fmt.Errorf("linearization layout: no pages")
	}
	numByObj := make(map[core.PdfObject]int64, len(w.objects))
	objByNum := make(map[int64]core.PdfObject, len(w.objects))
	for _, object := range w.objects {
		n, _ := objectNumbers(object)
		if n == 0 {
			continue
		}
		numByObj[object] = n
		objByNum[n] = object
	}
	sets := make([]map[int64]struct{}, len(w.pageObjs))
	for i, pageObj := range w.pageObjs {
		sets[i] = collectReachableObjectNumbers(pageObj, numByObj)
	}
	set := map[int64]struct{}{}
	fn := func(obj core.PdfObject) {
		if obj == nil {
			return
		}
		if n, ok := numByObj[obj]; ok {
			set[n] = struct{}{}
		}
	}
	fn(w.root)
	fn(w.pages)
	if w.encryptObj != nil {
		fn(w.encryptObj)
	}
	if w.infoObj != nil {
		fn(w.infoObj)
	}
	if w.acroForm != nil {
		if indirect, ok := w.catalog.Get("AcroForm").(*core.PdfIndirectObject); ok && indirect != nil {
			fn(indirect)
		}
	}
	if indirect, ok := w.catalog.Get("Outlines").(*core.PdfIndirectObject); ok && indirect != nil {
		fn(indirect)
	}
	if indirect, ok := w.catalog.Get("Names").(*core.PdfIndirectObject); ok && indirect != nil {
		fn(indirect)
	}
	set2 := sets[0]
	set3 := map[int64]struct{}{}
	sets2 := make([]map[int64]struct{}, len(sets))
	for i := range sets2 {
		sets2[i] = map[int64]struct{}{}
	}
	set4 := map[int64]struct{}{}
	table := make(map[int64]int, len(objByNum))
	table2 := make(map[int64]int)
	for i := 1; i < len(sets); i++ {
		for n := range sets[i] {
			table[n]++
			if table[n] == 1 {
				table2[n] = i
			}
		}
	}
	for n := range objByNum {
		_, ok := set2[n]
		_, found := set[n]
		count := table[n]
		switch {
		case found:
		case ok:
		case count > 1:
			set3[n] = struct{}{}
		case count == 1:
			sets2[table2[n]][n] = struct{}{}
		default:
			set4[n] = struct{}{}
		}
	}
	table3 := make(map[int64]int, len(objByNum))
	for i := 0; i < len(sets); i++ {
		for n := range sets[i] {
			table3[n]++
		}
	}
	n := numByObj[w.pageObjs[0]]
	nums := make([]int64, 0, len(set))
	set5 := map[int64]struct{}{}
	fn2 := func(n int64) {
		if n == 0 {
			return
		}
		if _, ok := set[n]; !ok {
			return
		}
		if _, ok := set2[n]; ok {
			return
		}
		if _, ok := set5[n]; ok {
			return
		}
		if _, ok := objByNum[n]; !ok {
			return
		}
		set5[n] = struct{}{}
		nums = append(nums, n)
	}
	if w.root != nil {
		fn2(numByObj[w.root])
	}
	if w.pages != nil {
		fn2(numByObj[w.pages])
	}
	if w.infoObj != nil {
		fn2(numByObj[w.infoObj])
	}
	for _, object := range w.objects {
		n, _ := objectNumbers(object)
		fn2(n)
	}
	nums2 := []int64{n}
	set6 := map[int64]struct{}{n: {}}
	for _, object := range w.objects {
		count, _ := objectNumbers(object)
		if count == n {
			continue
		}
		if _, ok := set2[count]; !ok {
			continue
		}
		if _, ok := set5[count]; ok {
			continue
		}
		if _, ok := set6[count]; ok {
			continue
		}
		set6[count] = struct{}{}
		nums2 = append(nums2, count)
	}
	nums3 := make([]int64, 0, len(nums)+len(nums2))
	nums3 = append(nums3, nums...)
	nums3 = append(nums3, nums2...)
	nums4 := append([]int64(nil), nums2...)
	numsList := make([][]int64, len(w.pageObjs))
	numsList[0] = append([]int64(nil), nums4...)
	table4 := make(map[int64]int)
	for i := 1; i < len(sets2); i++ {
		for n := range sets2[i] {
			table4[n] = i
		}
	}
	sets3 := make([]map[int64]struct{}, len(w.pageObjs))
	for i := 1; i < len(w.pageObjs); i++ {
		n := numByObj[w.pageObjs[i]]
		numsList[i] = []int64{n}
		sets3[i] = map[int64]struct{}{n: {}}
	}
	for _, object := range w.objects {
		n, _ := objectNumbers(object)
		count, ok := table4[n]
		if !ok {
			continue
		}
		if _, ok := sets3[count][n]; ok {
			continue
		}
		sets3[count][n] = struct{}{}
		numsList[count] = append(numsList[count], n)
	}
	nums5 := make([]int64, 0, len(set3))
	for _, object := range w.objects {
		n, _ := objectNumbers(object)
		if _, ok := set3[n]; ok {
			nums5 = append(nums5, n)
		}
	}
	nums6 := make([]int64, 0, len(nums5)+len(set4)+len(objByNum))
	nums6 = append(nums6, nums5...)
	for i := 1; i < len(w.pageObjs); i++ {
		nums6 = append(nums6, numsList[i]...)
	}
	for _, object := range w.objects {
		n, _ := objectNumbers(object)
		if _, ok := set4[n]; ok {
			nums6 = append(nums6, n)
		}
	}
	nums7 := make([]int64, 0, len(nums2)+len(nums5))
	nums7 = append(nums7, nums2...)
	nums7 = append(nums7, nums5...)
	set7 := make(map[int64]struct{}, len(nums7))
	for _, num := range nums7 {
		set7[num] = struct{}{}
	}
	list := make([][]int64, len(w.pageObjs))
	for i := range list {
		var nums8 []int64
		for _, num := range nums7 {
			if _, ok := sets[i][num]; !ok {
				continue
			}
			if table3[num] <= 1 {
				continue
			}
			nums8 = append(nums8, num)
		}
		list[i] = nums8
	}
	nums8 := append([]int64(nil), nums7...)
	objs := make([]core.PdfObject, 0, len(nums3)+len(nums6))
	for _, num := range nums3 {
		objs = append(objs, objByNum[num])
	}
	for _, num := range nums6 {
		objs = append(objs, objByNum[num])
	}
	return &linearizationLayout{firstSection: nums3, mainSection: nums6, sharedObjs: nums8, orderedObjs: objs, firstShared: nums4, pageObjs: numsList, pageShared: list, mainShared: nums5, pageCount: len(w.pageObjs), firstPageObjNum: n}, nil
}

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
