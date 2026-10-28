package model

import (
	"errors"
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/license"
)

func (w *PdfWriter) prepareLinearizedDocument() error {
	if err := w.checkLicense(); err != nil {
		return err
	}
	if err := w.writeOutlines(); err != nil {
		return err
	}
	if err := w.writeAcroFormFields(); err != nil {
		return err
	}
	if err := w.writeNamesDictionary(); err != nil {
		return err
	}
	w.checkPendingObjects()
	if err := w.writeOutputIntents(); err != nil {
		return err
	}
	w.setCatalogVersion()
	w.copyObjects()
	w.refreshPagesAfterCopy()
	if err := w.optimize(); err != nil {
		return err
	}
	if err := w.optimizeDocument(); err != nil {
		return err
	}
	return nil
}

// AddPage adds a page to the PDF file. The new page should be an indirect object.
func (w *PdfWriter) AddPage(page *PdfPage) error {
	const trackNameAddPage = "model:PdfWriter.AddPage"
	obj := pageToPdfObjectLicensed(page)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("==========")
		common.Log.Trace("Appending to page list %T", obj)
	}
	indirect, ok := core.GetIndirect(obj)
	if !ok {
		return errors.New("page should be an indirect object")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("%s", indirect)
		common.Log.Trace("%s", indirect.PdfObject)
	}
	dict, ok := core.GetDict(indirect.PdfObject)
	if !ok {
		return errors.New("page object should be a dictionary")
	}
	name, ok := core.GetName(dict.Get("Type"))
	if !ok {
		return fmt.Errorf("page should have a Type key with a value of type name (%T)", dict.Get("Type"))
	}
	if name.String() != "Page" {
		return errors.New("field Type != Page (Required)")
	}
	names := []core.PdfObjectName{"Resources", "MediaBox", "CropBox", "Rotate"}
	parent, isParent := core.GetIndirect(dict.Get("Parent"))
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Page Parent: %T (%v)", dict.Get("Parent"), isParent)
	}
	for isParent {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Page Parent: %T", parent)
		}
		dict2, isDict := core.GetDict(parent.PdfObject)
		if !isDict {
			return errors.New("invalid Parent object")
		}
		for _, name := range names {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Field %s", name)
			}
			if dict.Get(name) != nil {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("- page has already")
				}
				continue
			}
			if obj := dict2.Get(name); obj != nil {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Inheriting field %s", name)
				}
				dict.Set(name, obj)
			}
		}
		parent, isParent = core.GetIndirect(dict2.Get("Parent"))
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Next parent: %T", dict2.Get("Parent"))
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Traversal done")
	}
	dict.Set("Parent", w.pages)
	indirect.PdfObject = dict
	dict2, ok := core.GetDict(w.pages.PdfObject)
	if !ok {
		return errors.New("invalid Pages obj (not a dict)")
	}
	kids, ok := core.GetArray(dict2.Get("Kids"))
	if !ok {
		return errors.New("invalid Pages Kids obj (not an array)")
	}
	kids.Append(indirect)
	w.pageDicts[dict] = struct{}{}
	w.pageObjs = append(w.pageObjs, indirect)
	count, ok := core.GetInt(dict2.Get("Count"))
	if !ok {
		return errors.New("invalid Pages Count object (not an integer)")
	}
	*count = *count + 1
	if page.reader == nil {
		err := license.Track(w.trackID, trackNameAddPage, w.fileName)
		if err != nil {
			return err
		}
	} else {
		err := license.Track(page.reader.trackID, trackNameAddPage, page.reader.fileName)
		if err != nil {
			return err
		}
	}
	w.addObject(indirect)
	err := w.addObjects(dict)
	if err != nil {
		return err
	}
	return nil
}

// GetPageIndirectObject returns the indirect object of page for the specified page number.
func (w *PdfWriter) GetPageIndirectObject(pageNum int) (*core.PdfIndirectObject, error) {
	if pageNum < 0 || pageNum >= len(w.pageObjs) {
		return nil, errors.New("page number out of range")
	}
	return w.pageObjs[pageNum], nil
}

func (w *PdfWriter) renumberForObjectStreams(layout *linearizationLayout, nums, nums2 []int64) (n, count, num int64) {
	var linearizationDictObjNum int64 = linearizationDictObjNum2
	for _, firstSection := range layout.firstSection {
		if firstSection > linearizationDictObjNum {
			linearizationDictObjNum = firstSection
		}
	}
	if layout.maxObjNum > linearizationDictObjNum {
		linearizationDictObjNum = layout.maxObjNum
	}
	size := linearizationDictObjNum + 1
	table := map[int64]int64{}
	for _, n := range nums2 {
		table[n] = size
		size++
	}
	n = size
	size++
	count = size
	size++
	num = size
	size++
	for _, n := range nums {
		table[n] = size
		size++
	}
	for _, object := range w.objects {
		n, _ := objectNumbers(object)
		if count, ok := table[n]; ok {
			switch v := object.(type) {
			case *core.PdfIndirectObject:
				v.ObjectNumber = count
			case *core.PdfObjectStream:
				v.ObjectNumber = count
			case *core.PdfObjectStreams:
				v.ObjectNumber = count
			}
		}
	}
	for i, mainSection := range layout.mainSection {
		if n, ok := table[mainSection]; ok {
			layout.mainSection[i] = n
		}
	}
	for i, mainShared := range layout.mainShared {
		if n, ok := table[mainShared]; ok {
			layout.mainShared[i] = n
		}
	}
	for i := range layout.pageObjs {
		for j, pageObj := range layout.pageObjs[i] {
			if n, ok := table[pageObj]; ok {
				layout.pageObjs[i][j] = n
			}
		}
	}
	return n, count, num
}

func (w *PdfWriter) setupObjectStreams(layout *linearizationLayout) (*objectStreamsState, error) {
	objByNum := map[int64]core.PdfObject{}
	for _, object := range w.objects {
		n, _ := objectNumbers(object)
		if n != 0 {
			objByNum[n] = object
		}
	}
	nums, nums2 := partitionObjectNumbers(w, layout, objByNum)
	renumberForObjectStreams, n, count := w.renumberForObjectStreams(layout, nums, nums2)
	objByNum = map[int64]core.PdfObject{}
	for _, object := range w.objects {
		n, _ := objectNumbers(object)
		if n != 0 {
			objByNum[n] = object
		}
	}
	nums3 := make([]int64, 0, len(nums))
	indirectByNum := map[int64]*core.PdfIndirectObject{}
	for _, object := range w.objects {
		num, _ := objectNumbers(object)
		if num <= count {
			continue
		}
		if indirect, ok := object.(*core.PdfIndirectObject); ok {
			nums3 = append(nums3, num)
			indirectByNum[num] = indirect
		}
	}
	sortInt64s(nums3)
	if len(nums3) > 0xFFFF {
		return nil, fmt.Errorf("linearization: %d compressible objects exceed the single object-stream index limit (65535); object-stream splitting is not yet supported", len(nums3))
	}
	indirects := make([]*core.PdfIndirectObject, 0, len(nums3))
	for _, num := range nums3 {
		indirects = append(indirects, indirectByNum[num])
	}
	objectStreams := core.MakeObjectStreams()
	for _, indirect := range indirects {
		objectStreams.Append(indirect)
	}
	objectStreams.ObjectNumber = renumberForObjectStreams
	set := map[int64]struct{}{}
	for _, num := range nums3 {
		set[num] = struct{}{}
	}
	nums4 := make([]int64, 0, len(layout.mainSection))
	for _, mainSection := range layout.mainSection {
		if _, ok := set[mainSection]; ok {
			continue
		}
		nums4 = append(nums4, mainSection)
	}
	nums4 = append(nums4, renumberForObjectStreams)
	layout.mainSection = nums4
	objByNum = map[int64]core.PdfObject{}
	for _, object := range w.objects {
		n, _ := objectNumbers(object)
		if n != 0 {
			objByNum[n] = object
		}
	}
	objs := make([]core.PdfObject, 0, len(layout.firstSection)+len(layout.mainSection))
	for _, firstSection := range layout.firstSection {
		if obj, ok := objByNum[firstSection]; ok {
			objs = append(objs, obj)
		}
	}
	for _, mainSection := range layout.mainSection {
		if mainSection == renumberForObjectStreams {
			objs = append(objs, objectStreams)
			continue
		}
		if obj, ok := objByNum[mainSection]; ok {
			objs = append(objs, obj)
		}
	}
	layout.orderedObjs = objs
	w.objects = append(w.objects, objectStreams)
	return &objectStreamsState{members: indirects, streams: objectStreams, streamObjNum: renumberForObjectStreams, mainXrefObjNum: n, firstXrefObjNum: count, packedObjNums: nums3}, nil
}

func (w *PdfWriter) serializeForLinearization(layout *linearizationLayout) ([]preserializedObject, error) {
	preObjs := make([]preserializedObject, 0, len(layout.orderedObjs))
	for _, orderedObj := range layout.orderedObjs {
		n, count := objectNumbers(orderedObj)
		if n == 0 {
			return nil, fmt.Errorf("linearization: object %T has no assigned object number", orderedObj)
		}
		if w.crypter != nil && orderedObj != w.encryptObj {
			if err := w.crypter.Encrypt(orderedObj, n, count); err != nil {
				return nil, fmt.Errorf("linearization: encrypt obj %d: %w", n, err)
			}
		}
		data, err := serializeObject(int(n), orderedObj, w.crypter)
		if err != nil {
			return nil, err
		}
		if stream, ok := orderedObj.(*core.PdfObjectStream); ok && stream.Lazy {
			w.lazyStreams = append(w.lazyStreams, stream)
		}
		preObjs = append(preObjs, preserializedObject{objNum: n, genNum: count, obj: orderedObj, data: data})
	}
	return preObjs, nil
}

// SetPieceInfo sets the page-piece dictionary associated with the document.
func (w *PdfWriter) SetPieceInfo(pieceInfo core.PdfObject) error {
	catalog := w.catalog
	if pieceInfo != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Setting PieceInfo...")
		}
		catalog.Set("PieceInfo", pieceInfo)
		return w.addObjects(pieceInfo)
	}
	return nil
}

func (w *PdfWriter) refreshPagesAfterCopy() {
	if w.root == nil {
		return
	}
	dict, ok := w.root.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return
	}
	indirect, _ := dict.Get("Pages").(*core.PdfIndirectObject)
	if indirect == nil {
		return
	}
	w.pages = indirect
	w.pageObjs = collectIndirectObjects(indirect)
}

func (w *PdfWriter) reserveLinearizationObjectNumbers(layout *linearizationLayout) int64 {
	table := make(map[int64]int64, len(layout.firstSection)+len(layout.mainSection))
	n := int64(2)
	for _, firstSection := range layout.firstSection {
		table[firstSection] = n
		n++
	}
	count := n
	n++
	for _, mainSection := range layout.mainSection {
		table[mainSection] = n
		n++
	}
	fn := func(n int64) int64 {
		if count, ok := table[n]; ok {
			return count
		}
		return n
	}
	for _, object := range w.objects {
		switch v := object.(type) {
		case *core.PdfIndirectObject:
			v.ObjectNumber = fn(v.ObjectNumber)
		case *core.PdfObjectStream:
			v.ObjectNumber = fn(v.ObjectNumber)
		case *core.PdfObjectStreams:
			v.ObjectNumber = fn(v.ObjectNumber)
		}
	}
	for i := range layout.firstSection {
		layout.firstSection[i] = fn(layout.firstSection[i])
	}
	for i := range layout.mainSection {
		layout.mainSection[i] = fn(layout.mainSection[i])
	}
	for i := range layout.sharedObjs {
		layout.sharedObjs[i] = fn(layout.sharedObjs[i])
	}
	for i := range layout.firstShared {
		layout.firstShared[i] = fn(layout.firstShared[i])
	}
	for i := range layout.pageObjs {
		for j := range layout.pageObjs[i] {
			layout.pageObjs[i][j] = fn(layout.pageObjs[i][j])
		}
	}
	for i := range layout.mainShared {
		layout.mainShared[i] = fn(layout.mainShared[i])
	}
	for i := range layout.pageShared {
		for j := range layout.pageShared[i] {
			layout.pageShared[i][j] = fn(layout.pageShared[i][j])
		}
	}
	layout.firstPageObjNum = fn(layout.firstPageObjNum)
	layout.maxObjNum = count
	return count
}

func (w *PdfWriter) seekByName(obj core.PdfObject, strs []string, s string) ([]core.PdfObject, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Seek by name.. %T", obj)
	}
	var objs []core.PdfObject
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		return w.seekByName(indirect.PdfObject, strs, s)
	}
	if stream, ok := obj.(*core.PdfObjectStream); ok {
		return w.seekByName(stream.PdfObjectDictionary, strs, s)
	}
	if dict, ok := obj.(*core.PdfObjectDictionary); ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Dict")
		}
		for _, name := range dict.Keys() {
			obj := dict.Get(name)
			if string(name) == s {
				objs = append(objs, obj)
			}
			for _, str := range strs {
				if string(name) == str {
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("Follow key %s", str)
					}
					seekByName, err := w.seekByName(obj, strs, s)
					if err != nil {
						return objs, err
					}
					objs = append(objs, seekByName...)
					break
				}
			}
		}
		return objs, nil
	}
	return objs, nil
}

// SetRotation sets the rotation of all pages added to writer. The rotation is
// specified in degrees and must be a multiple of 90.
// The Rotate field of individual pages has priority over the global rotation.
func (w *PdfWriter) SetRotation(rotate int64) error {
	pagesDict, ok := core.GetDict(w.pages)
	if !ok {
		return ErrTypeCheck
	}
	pagesDict.Set("Rotate", core.MakeInteger(rotate))
	return nil
}

// SetPageLabels sets the PageLabels entry in the PDF catalog.
// See section 12.4.2 "Page Labels" (p. 382 PDF32000_2008).
func (w *PdfWriter) SetPageLabels(pageLabels core.PdfObject) error {
	if pageLabels == nil {
		return nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Setting catalog PageLabels...")
	}
	w.catalog.Set("PageLabels", pageLabels)
	return w.addObjects(pageLabels)
}
