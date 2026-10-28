package model

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	_ "image/gif"
	"io"
	"sort"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

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

func (w *PdfWriter) updateObjectNumbers() {
	objNumOffset := w.ObjNumOffset
	n := 0
	for _, object := range w.objects {
		count := int64(n + 1 + objNumOffset)
		ok := true
		if w.appendMode {
			if objNumMap, isObjNumMap := w.objNumMap[object]; isObjNumMap {
				count = objNumMap
				ok = false
			}
		}
		switch v := object.(type) {
		case *core.PdfIndirectObject:
			v.ObjectNumber = count
			v.GenerationNumber = 0
		case *core.PdfObjectStream:
			v.ObjectNumber = count
			v.GenerationNumber = 0
		case *core.PdfObjectStreams:
			v.ObjectNumber = count
			v.GenerationNumber = 0
		case *core.PdfObjectReference:
			v.ObjectNumber = count
			v.GenerationNumber = 0
		case *core.PdfObjectDictionary, *core.PdfObjectString:
		default:
			common.Log.Debug("ERROR: Unknown type %T - skipping", v)
			continue
		}
		if ok {
			n++
		}
	}
	fn := func(obj core.PdfObject) int64 {
		switch v := obj.(type) {
		case *core.PdfIndirectObject:
			return v.ObjectNumber
		case *core.PdfObjectStream:
			return v.ObjectNumber
		case *core.PdfObjectStreams:
			return v.ObjectNumber
		case *core.PdfObjectReference:
			return v.ObjectNumber
		}
		return 0
	}
	sort.SliceStable(w.objects, func(n, count int) bool { return fn(w.objects[n]) < fn(w.objects[count]) })
}

func (w *PdfWriter) getPdfVersion() string {
	return fmt.Sprintf("%d.%d", w.version.Major, w.version.Minor)
}

// GetVersion gets the document version.
func (w *PdfWriter) GetVersion() core.Version { return w.version }

// SetSkipObjectCopy controls whether Write skips the defensive deep copy of the
// source object graph.
//
// By default, when a mutating pipeline stage is present (optimizer, encryption,
// PDF/A conversion), the writer deep-copies source objects so those stages don't
// modify the originating PdfReader's objects. That copy is the single largest
// source of memory/time overhead when writing large documents.
//
// When skip is true the copy is bypassed and those stages mutate the source
// objects in place. This significantly reduces memory use and write time, but
// the source objects (and the PdfReader they came from) are consumed: they MUST
// NOT be read or written again after Write. Use this only for one-shot
// load -> (optimize/encrypt) -> write -> discard pipelines.
//
// The copy is still performed regardless of this setting in append mode and when
// orphan pages require it (see canSkipCopy), as those need the copy for
// correctness rather than source protection.
func (w *PdfWriter) SetSkipObjectCopy(skip bool) { w.skipObjectCopy = skip }

func (w *PdfWriter) snapshotObjectNumbers() func() {
	type savedNum struct {
		obj    core.PdfObject
		objNum int64
		genNum int64
	}
	savedNums := make([]savedNum, 0, len(w.objects))
	for _, object := range w.objects {
		switch v := object.(type) {
		case *core.PdfIndirectObject:
			savedNums = append(savedNums, savedNum{v, v.ObjectNumber, v.GenerationNumber})
		case *core.PdfObjectStream:
			savedNums = append(savedNums, savedNum{v, v.ObjectNumber, v.GenerationNumber})
		case *core.PdfObjectStreams:
			savedNums = append(savedNums, savedNum{v, v.ObjectNumber, v.GenerationNumber})
		case *core.PdfObjectReference:
			savedNums = append(savedNums, savedNum{v, v.ObjectNumber, v.GenerationNumber})
		}
	}
	return func() {
		for _, savedNum := range savedNums {
			switch v := savedNum.obj.(type) {
			case *core.PdfIndirectObject:
				v.ObjectNumber, v.GenerationNumber = savedNum.objNum, savedNum.genNum
			case *core.PdfObjectStream:
				v.ObjectNumber, v.GenerationNumber = savedNum.objNum, savedNum.genNum
			case *core.PdfObjectStreams:
				v.ObjectNumber, v.GenerationNumber = savedNum.objNum, savedNum.genNum
			case *core.PdfObjectReference:
				v.ObjectNumber, v.GenerationNumber = savedNum.objNum, savedNum.genNum
			}
		}
	}
}

// GetStandardApplier gets currently used StandardApplier..
func (w *PdfWriter) GetStandardApplier() StandardApplier { return w.standardApplier }

func (w *PdfWriter) writeXRefStreams(n int, count int64) error {
	num := n + 1
	w.crossReferences[num] = crossReference{Type: 1, ObjectNumber: num, Offset: count}
	buffer := bytes.NewBuffer(nil)
	array := core.MakeArray()
	for i := 0; i <= n; {
		for ; i <= n; i++ {
			xref, ok := w.crossReferences[i]
			if ok && (!w.appendMode || w.appendMode && (xref.Type == 1 && xref.Offset >= w.originalSize || xref.Type == 0)) {
				break
			}
		}
		var count int
		for count = i + 1; count <= n; count++ {
			xref, ok := w.crossReferences[count]
			if ok && (!w.appendMode || w.appendMode && (xref.Type == 1 && xref.Offset > w.originalSize)) {
				continue
			}
			break
		}
		array.Append(core.MakeInteger(int64(i)), core.MakeInteger(int64(count-i)))
		for j := i; j < count; j++ {
			xref := w.crossReferences[j]
			switch xref.Type {
			case 0:
				binary.Write(buffer, binary.BigEndian, byte(0))
				binary.Write(buffer, binary.BigEndian, uint32(0))
				binary.Write(buffer, binary.BigEndian, uint16(0xFFFF))
			case 1:
				binary.Write(buffer, binary.BigEndian, byte(1))
				binary.Write(buffer, binary.BigEndian, uint32(xref.Offset))
				binary.Write(buffer, binary.BigEndian, uint16(xref.Generation))
			case 2:
				binary.Write(buffer, binary.BigEndian, byte(2))
				binary.Write(buffer, binary.BigEndian, uint32(xref.ObjectNumber))
				binary.Write(buffer, binary.BigEndian, uint16(xref.Index))
			}
		}
		i = count + 1
	}
	stream, err := core.MakeStream(buffer.Bytes(), core.NewFlateEncoder())
	if err != nil {
		return err
	}
	stream.ObjectNumber = int64(num)
	stream.PdfObjectDictionary.Set("Type", core.MakeName("XRef"))
	stream.PdfObjectDictionary.Set("W", core.MakeArray(core.MakeInteger(1), core.MakeInteger(4), core.MakeInteger(2)))
	stream.PdfObjectDictionary.Set("Index", array)
	stream.PdfObjectDictionary.Set("Size", core.MakeInteger(int64(num)))
	stream.PdfObjectDictionary.Set("Info", w.infoObj)
	stream.PdfObjectDictionary.Set("Root", w.root)
	if w.appendMode && w.prevXrefOffset > 0 {
		stream.PdfObjectDictionary.Set("Prev", core.MakeInteger(w.prevXrefOffset))
	}
	if w.crypter != nil {
		stream.Set("Encrypt", w.encryptObj)
	}
	if w.ids == nil && w.id0 != "" && w.id1 != "" {
		w.ids = core.MakeArray(core.MakeHexString(w.id0), core.MakeHexString(w.id1))
	}
	if w.ids != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Ids: %s", w.ids)
		}
		stream.Set("ID", w.ids)
	}
	return w.writeObject(int(stream.ObjectNumber), stream)
}

// SetVersion sets the PDF version of the output file.
func (w *PdfWriter) SetVersion(majorVersion, minorVersion int) {
	w.version.Major = majorVersion
	w.version.Minor = minorVersion
}

// GetPdfVersion gets the version of the PDF used within this document.
func (w *PdfWriter) GetPdfVersion() string { return w.getPdfVersion() }

func (w *PdfWriter) setWriter(writer io.Writer) {
	w.writePos = w.startOffset
	w.writeErr = nil
	w.bufWriter = bufio.NewWriter(writer)
}

func (w *PdfWriter) writeLinearized(writer io.Writer) error {
	if err := w.prepareLinearizedDocument(); err != nil {
		return err
	}
	if len(w.pageObjs) == 0 {
		return fmt.Errorf("linearization: cannot linearize a document with no pages")
	}
	ok := w.linearizationOptions.UseObjectStreams
	w.adjustXRefAffectedVersion(ok)
	w.updateObjectNumbers()
	layout, err := w.computeLinearizationLayout()
	if err != nil {
		return err
	}
	reserveLinearizationObjectNumbers := w.reserveLinearizationObjectNumbers(layout)
	var objStm *objectStreamsState
	if ok {
		objStm, err = w.setupObjectStreams(layout)
		if err != nil {
			return err
		}
	}
	serializeForLinearization, err := w.serializeForLinearization(layout)
	if err != nil {
		return err
	}
	w.ensureLinearizedDocumentIDs(serializeForLinearization)
	plan, err := planLinearization(w, layout, serializeForLinearization, reserveLinearizationObjectNumbers, objStm)
	if err != nil {
		return err
	}
	w2 := bufio.NewWriter(writer)
	if err := plan.emit(w2); err != nil {
		return err
	}
	return w2.Flush()
}

func (w *PdfWriter) writeOutlines() error {
	if w.outlineTree == nil {
		return nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("OutlineTree: %+v", w.outlineTree)
	}
	obj := w.outlineTree.ToPdfObject()
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Outlines: %+v (%T, p:%p)", obj, obj, obj)
	}
	w.catalog.Set("Outlines", obj)
	err := w.addObjects(obj)
	if err != nil {
		return err
	}
	return nil
}
