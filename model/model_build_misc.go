package model

import (
	"bytes"
	"fmt"
	_ "image/gif"
	"math"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
)

func buildSharedObjectHintTable(layout *linearizationLayout, preObjByNum map[int64]preserializedObject, n, count int64) (*core.SharedObjectHintTable, error) {
	firstShared := layout.firstShared
	mainShared := layout.mainShared
	if len(firstShared) == 0 {
		return &core.SharedObjectHintTable{BitsPerGroupIdentifier: 1, BitsPerSharedGroupLengthDelta: 1}, nil
	}
	nums := make([]int64, 0, len(firstShared)+len(mainShared))
	nums = append(nums, firstShared...)
	nums = append(nums, mainShared...)
	var num uint32
	size := nums[0]
	if len(mainShared) > 0 {
		num = uint32(mainShared[0])
		size = mainShared[0]
	}
	length, err := adjustHintOffset(preObjByNum[size].offset, n, count)
	if err != nil {
		return nil, err
	}
	var length2 uint32
	var length3 uint32
	for i, n := range nums {
		length4 := uint32(len(preObjByNum[n].data))
		if i == 0 || length4 < length2 {
			length2 = length4
		}
		if length4 > length3 {
			length3 = length4
		}
	}
	sharedObjectHintTable := &core.SharedObjectHintTable{FirstSharedObjNum: num, FirstSharedOffset: length, NumSharedFirstPage: uint32(len(firstShared)), NumSharedTotal: uint32(len(nums)), BitsPerGroupIdentifier: 1, LeastSharedGroupLength: length2, BitsPerSharedGroupLengthDelta: bitsRequired(length3 - length2)}
	sharedObjectHintTable.Entries = make([]core.SharedObjectEntry, len(nums))
	for i, n := range nums {
		length3 := uint32(len(preObjByNum[n].data))
		sharedObjectHintTable.Entries[i] = core.SharedObjectEntry{GroupLengthDelta: length3 - length2, SignaturePresent: false, ObjectsInGroup: 1}
	}
	return sharedObjectHintTable, nil
}

func buildMainXrefStream(writer *PdfWriter, mainXrefObjNum int64, nums []int64, preObjByNum map[int64]preserializedObject, n int64, hintOffset int64, linDictOffset int64, mainXrefOffset int64, firstXrefOffset int64, count int64, objStm *objectStreamsState) ([]byte, error) {
	table := map[int64]int{}
	for i, packedObjNum := range objStm.packedObjNums {
		table[packedObjNum] = i
	}
	entries := make([]xrefEntry, 0, len(nums))
	for _, num := range nums {
		if n, ok := table[num]; ok {
			entries = append(entries, xrefEntry{objNum: num, entryType: 2, offset: uint32(objStm.streamObjNum), genOrIndex: uint16(n)})
			continue
		}
		var count int64
		switch num {
		case linearizationDictObjNum2:
			count = linDictOffset
		case n:
			count = hintOffset
		case objStm.firstXrefObjNum:
			count = firstXrefOffset
		case objStm.mainXrefObjNum:
			count = mainXrefOffset
		default:
			preObj, ok := preObjByNum[num]
			if !ok {
				return nil, fmt.Errorf("main xref stream: missing offset for obj %d", num)
			}
			count = preObj.offset
		}
		if count > math.MaxUint32 {
			return nil, fmt.Errorf("xref stream: object %d offset %d exceeds 4 GB (xref-stream offset field is 4 bytes)", num, count)
		}
		entries = append(entries, xrefEntry{objNum: num, entryType: 1, offset: uint32(count), genOrIndex: 0})
	}
	return buildXrefStream(&xrefStreamPlan{objNum: mainXrefObjNum, entries: entries, size: count + 1, hasPrev: false, root: writer.root, info: writer.infoObj, encrypt: encryptedWriterCatalog(writer), id: writer.ids})
}

func buildLinearizationDict(n int64, fileLength int64, hintOffset int64, count int64, firstPageEnd int64, pageCount int, xrefFirstEntryOffset int64, firstPageObjNum int64) []byte {
	s := fmt.Sprintf("%d 0 obj\n<</Linearized 1/L %d/H[%d %d]/O %d/E %d/N %d/T %d", n, fileLength, hintOffset, count, firstPageObjNum, firstPageEnd, pageCount, xrefFirstEntryOffset)
	s2 := fmt.Sprintf("%d 0 obj\n<</Linearized 1/L %010d/H[%010d %010d]/O %010d/E %010d/N %010d/T %010d", n, fileLength, hintOffset, count, firstPageObjNum, firstPageEnd, pageCount, xrefFirstEntryOffset)
	num := max(len(s2)-len(s), 0)
	var buf bytes.Buffer
	buf.WriteString(s)
	for i := 0; i < num; i++ {
		buf.WriteByte(' ')
	}
	buf.WriteString(">>\nendobj\n")
	return buf.Bytes()
}

func buildFirstXrefTable(writer *PdfWriter, firstXrefObjs []int64, objects map[int64]preserializedObject, hintObjNum int64, hintOffset int64, mainXrefOffset int64, linDictOffset int64, trailerSize int64) []byte {
	table := map[int64]int64{}
	for _, firstXrefObj := range firstXrefObjs {
		switch firstXrefObj {
		case linearizationDictObjNum2:
			table[firstXrefObj] = linDictOffset
		case hintObjNum:
			table[firstXrefObj] = hintOffset
		default:
			table[firstXrefObj] = objects[firstXrefObj].offset
		}
	}
	xrefTable, _ := buildXrefTable(writer, firstXrefObjs, table, trailerSize, mainXrefOffset, true, 0, false)
	return xrefTable
}

func buildObjectStreamBytes(n int64, objStreams *core.PdfObjectStreams, crypter *core.PdfCrypt) ([]byte, error) {
	var buf bytes.Buffer
	var buf2 bytes.Buffer
	var count int64
	for _, obj := range objStreams.Elements() {
		indirect, ok := obj.(*core.PdfIndirectObject)
		if !ok {
			return nil, fmt.Errorf("object stream %d: contains non-indirect %T", n, obj)
		}
		data := append(indirect.PdfObject.Write(), ' ')
		fmt.Fprintf(&buf, "%d %d ", indirect.ObjectNumber, count)
		buf2.Write(data)
		count += int64(len(data))
	}
	data := append(buf.Bytes(), buf2.Bytes()...)
	stream, err := core.MakeStream(data, core.NewFlateEncoder())
	if err != nil {
		return nil, fmt.Errorf("object stream %d: encode: %w", n, err)
	}
	stream.ObjectNumber = n
	stream.PdfObjectDictionary.Set("Type", core.MakeName("ObjStm"))
	stream.PdfObjectDictionary.Set("N", core.MakeInteger(int64(objStreams.Len())))
	stream.PdfObjectDictionary.Set("First", core.MakeInteger(int64(buf.Len())))
	if crypter != nil {
		if err := crypter.Encrypt(stream, n, 0); err != nil {
			return nil, fmt.Errorf("object stream %d: encrypt: %w", n, err)
		}
	}
	var buf3 bytes.Buffer
	fmt.Fprintf(&buf3, "%d 0 obj\n", n)
	buf3.Write(stream.PdfObjectDictionary.Write())
	buf3.WriteString("\nstream\n")
	buf3.Write(stream.Stream)
	buf3.WriteString("\nendstream\nendobj\n")
	return buf3.Bytes(), nil
}

func buildFirstXrefStream(writer *PdfWriter, firstXrefObjNum int64, nums []int64, preObjByNum map[int64]preserializedObject, n int64, hintOffset int64, linDictOffset int64, firstXrefOffset int64, mainXrefOffset int64, count int64, objStm *objectStreamsState) ([]byte, error) {
	entries := make([]xrefEntry, 0, len(nums))
	for _, num := range nums {
		var count int64
		switch num {
		case linearizationDictObjNum2:
			count = linDictOffset
		case n:
			count = hintOffset
		case objStm.firstXrefObjNum:
			count = firstXrefOffset
		default:
			preObj, ok := preObjByNum[num]
			if !ok {
				return nil, fmt.Errorf("first xref stream: missing offset for obj %d", num)
			}
			count = preObj.offset
		}
		if count > math.MaxUint32 {
			return nil, fmt.Errorf("xref stream: object %d offset %d exceeds 4 GB (xref-stream offset field is 4 bytes)", num, count)
		}
		entries = append(entries, xrefEntry{objNum: num, entryType: 1, offset: uint32(count), genOrIndex: 0})
	}
	return buildXrefStream(&xrefStreamPlan{objNum: firstXrefObjNum, entries: entries, size: count + 1, prev: mainXrefOffset, hasPrev: true, root: writer.root, info: writer.infoObj, encrypt: encryptedWriterCatalog(writer), id: writer.ids})
}

func buildHintStream(layout *linearizationLayout, table map[int64]int64, preObjByNum map[int64]preserializedObject, hintOffset, n int64) ([]byte, int64, error) {
	pageOffsetHintTable, err := buildPageOffsetHintTable(layout, table, preObjByNum, hintOffset, n)
	if err != nil {
		return nil, 0, err
	}
	sharedObjectHintTable, err := buildSharedObjectHintTable(layout, preObjByNum, hintOffset, n)
	if err != nil {
		return nil, 0, err
	}
	return core.EncodeMandatoryHintStream(pageOffsetHintTable, sharedObjectHintTable)
}

func buildMainXrefTable(writer *PdfWriter, nums []int64, n int64, preObjByNum map[int64]preserializedObject, count int64, hintOffset int64, mainXrefOffset int64, linDictOffset int64, firstXrefOffset int64) ([]byte, int64) {
	table := map[int64]int64{}
	for _, num := range nums {
		switch num {
		case linearizationDictObjNum2:
			table[num] = linDictOffset
		case count:
			table[num] = hintOffset
		default:
			table[num] = preObjByNum[num].offset
		}
	}
	xrefTable, num := buildXrefTable(writer, nums, table, n, 0, false, firstXrefOffset, true)
	size := mainXrefOffset + num
	return xrefTable, size
}

func buildWidthsArray(numByV map[fonts.GID]int, n uint16) *core.PdfObjectArray {
	arr := &core.PdfObjectArray{}
	v := fonts.GID(n)
	for i := fonts.GID(0); i < v; {
		n, ok := numByV[i]
		if !ok {
			i++
			continue
		}
		v2 := i
		for i := v2 + 1; i < v; i++ {
			if count, ok := numByV[i]; !ok || n != count {
				break
			}
			v2 = i
		}
		arr.Append(core.MakeInteger(int64(i)))
		arr.Append(core.MakeInteger(int64(v2)))
		arr.Append(core.MakeInteger(int64(n)))
		i = v2 + 1
	}
	return arr
}

func buildPageOffsetHintTable(layout *linearizationLayout, table map[int64]int64, preObjByNum map[int64]preserializedObject, n, count int64) (*core.PageOffsetHintTable, error) {
	if layout.pageCount <= 0 {
		return nil, fmt.Errorf("hint encoder: numPages=%d", layout.pageCount)
	}
	nums := make([]uint32, layout.pageCount)
	nums2 := make([]uint32, layout.pageCount)
	for i := 0; i < layout.pageCount; i++ {
		var nums3 []int64
		if i < len(layout.pageObjs) {
			nums3 = layout.pageObjs[i]
		}
		if i == 0 && len(nums3) == 0 {
			nums3 = layout.firstShared
		}
		nums[i] = uint32(len(nums3))
		for _, num := range nums3 {
			nums2[i] += uint32(len(preObjByNum[num].data))
		}
	}
	num := nums[0]
	size := nums[0]
	for _, length := range nums {
		if length < num {
			num = length
		}
		if length > size {
			size = length
		}
	}
	length := nums2[0]
	length2 := nums2[0]
	for _, length3 := range nums2 {
		if length3 < length {
			length = length3
		}
		if length3 > length2 {
			length2 = length3
		}
	}
	length3, err := firstPageHintOffset(layout, table, n, count)
	if err != nil {
		return nil, err
	}
	table2 := make(map[int64]uint32, len(layout.firstShared)+len(layout.mainShared))
	length4 := uint32(0)
	for _, firstShared := range layout.firstShared {
		if _, ok := table2[firstShared]; !ok {
			table2[firstShared] = length4
			length4++
		}
	}
	for _, mainShared := range layout.mainShared {
		if _, ok := table2[mainShared]; !ok {
			table2[mainShared] = length4
			length4++
		}
	}
	numsList := make([][]uint32, layout.pageCount)
	var length5, length6 uint32
	for i := 1; i < layout.pageCount; i++ {
		if i >= len(layout.pageShared) {
			continue
		}
		nums := make([]uint32, 0, len(layout.pageShared[i]))
		for _, pageShared := range layout.pageShared[i] {
			length7, ok := table2[pageShared]
			if !ok {
				continue
			}
			nums = append(nums, length7)
			if length7 > length6 {
				length6 = length7
			}
		}
		numsList[i] = nums
		if uint32(len(nums)) > length5 {
			length5 = uint32(len(nums))
		}
	}
	pageOffsetHintTable := &core.PageOffsetHintTable{LeastObjectsInPage: num, FirstPageOffset: length3, BitsPerObjectCountDelta: bitsRequired(size - num), LeastPageLength: length, BitsPerPageLengthDelta: bitsRequired(length2 - length), LeastContentStreamOffset: 0, BitsPerContentStreamOffsetDelta: 1, LeastContentStreamLength: 0, BitsPerContentStreamLengthDelta: 1, BitsPerNumSharedObjects: bitsRequired(length5), BitsPerSharedIdentifier: bitsRequired(length6), BitsPerSharedNumerator: 1, SharedDenominator: 1}
	pageOffsetHintTable.PerPage = make([]core.PageOffsetEntry, layout.pageCount)
	for i := range pageOffsetHintTable.PerPage {
		nums3 := numsList[i]
		nums4 := make([]uint32, len(nums3))
		pageOffsetHintTable.PerPage[i] = core.PageOffsetEntry{ObjectCountDelta: nums[i] - num, PageLengthDelta: nums2[i] - length, NumSharedObjects: uint32(len(nums3)), SharedObjectIdentifiers: nums3, SharedObjectNumerators: nums4}
	}
	return pageOffsetHintTable, nil
}

func buildHintStreamObject(n int64, data []byte, count int64) ([]byte, error) {
	return core.BuildHintStreamObject(n, data, count)
}
