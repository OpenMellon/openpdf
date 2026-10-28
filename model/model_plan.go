package model

import (
	"fmt"
	_ "image/gif"
	"slices"
)

func planLinearizationPass(writer *PdfWriter, layout *linearizationLayout, preObjByNum map[int64]preserializedObject, n int64, objStm *objectStreamsState, data []byte, count int, nums []int64, nums2 []int64, num int64, size int, length int, length2 int) (*linearizedPlan, planSizes, error) {
	plan := &linearizedPlan{header: append([]byte(nil), data...)}
	length3 := int64(len(data))
	plan.linDictOffset = length3
	length3 += int64(count)
	plan.firstXrefOffset = length3
	var length4 int
	if objStm == nil {
		const placeholderOffset = int64(9999999999)
		length4 = measureXrefTable(writer, nums, num, placeholderOffset, true, false)
	} else {
		length4 = length
		if length4 < 0 {
			length4 = 256 + 7*len(nums)/2
		}
	}
	length3 += int64(length4)
	plan.hintOffset = length3
	length5 := size
	if length5 < 0 {
		length5 = 256 + 32*len(layout.firstSection) + 16*len(layout.sharedObjs)
	}
	length3 += int64(length5)
	length6 := length3
	table := make(map[int64]int64, len(layout.firstSection))
	for _, firstSection := range layout.firstSection {
		preObj, ok := preObjByNum[firstSection]
		if !ok {
			return nil, planSizes{}, fmt.Errorf("plan: missing serialized object %d", firstSection)
		}
		preObj.offset = length3
		preObjByNum[firstSection] = preObj
		table[firstSection] = length3
		length3 += int64(len(preObj.data))
	}
	plan.firstPageEnd = length3
	length7 := length3
	for _, mainSection := range layout.mainSection {
		preObj, ok := preObjByNum[mainSection]
		if !ok {
			return nil, planSizes{}, fmt.Errorf("plan: missing serialized object %d", mainSection)
		}
		preObj.offset = length3
		preObjByNum[mainSection] = preObj
		length3 += int64(len(preObj.data))
	}
	plan.mainXrefOffset = length3
	var data2 []byte
	var length8 int64
	if objStm == nil {
		data2, length8 = buildMainXrefTable(writer, nums2, num, preObjByNum, n, plan.hintOffset, plan.mainXrefOffset, plan.linDictOffset, plan.firstXrefOffset)
	} else {
		length9 := length2
		if length9 < 0 {
			length9 = 256 + 7*len(nums2)/2
		}
		data2 = make([]byte, length9)
		length8 = plan.mainXrefOffset
	}
	plan.mainXref = data2
	plan.mainXrefFirstEntry = length8
	length3 += int64(len(data2))
	if objStm != nil {
		length3 += int64(trailerOverheadBytes)
	}
	plan.fileLength = length3
	hintStream, length9, err := buildHintStream(layout, table, preObjByNum, plan.hintOffset, int64(length5))
	if err != nil {
		return nil, planSizes{}, err
	}
	hintStreamObject, err := buildHintStreamObject(n, hintStream, length9)
	if err != nil {
		return nil, planSizes{}, err
	}
	plan.hintStream = hintStreamObject
	length10 := len(hintStreamObject)
	length11 := length4
	length12 := len(data2)
	if objStm != nil {
		length13 := len(data2)
		firstXrefStream, err := buildFirstXrefStream(writer, objStm.firstXrefObjNum, nums, preObjByNum, n, plan.hintOffset, plan.linDictOffset, plan.firstXrefOffset, plan.mainXrefOffset, num, objStm)
		if err != nil {
			return nil, planSizes{}, err
		}
		plan.firstXref = padBytes(firstXrefStream, length4)
		length11 = len(plan.firstXref)
		mainXrefStream, err := buildMainXrefStream(writer, objStm.mainXrefObjNum, nums2, preObjByNum, n, plan.hintOffset, plan.linDictOffset, plan.mainXrefOffset, plan.firstXrefOffset, num, objStm)
		if err != nil {
			return nil, planSizes{}, err
		}
		plan.mainXref = padBytes(mainXrefStream, length13)
		length12 = len(plan.mainXref)
	}
	sizes := planSizes{hintLen: length10, firstXrefLen: length11, mainXrefLen: length12}
	if length10 != length5 || length11 != length4 || length12 != len(data2) {
		return plan, sizes, nil
	}
	if err := finalizeLinearizationDict(writer, plan, convergedPlanInputs{layout: layout, objects: preObjByNum, objStreams: objStm, hintObjNum: n, firstXrefObjs: nums, trailerSize: num, linDictLen: count, firstXrefLen: length4, hintLen: length10, xrefFirstEntryOffset: length8, firstSectionStart: length6, mainSectionStart: length7}); err != nil {
		return nil, planSizes{}, err
	}
	return plan, sizes, nil
}

func planLinearization(writer *PdfWriter, layout *linearizationLayout, preObjs []preserializedObject, n int64, objStm *objectStreamsState) (*linearizedPlan, error) {
	if len(preObjs) == 0 {
		return nil, fmt.Errorf("linearization plan: nothing to serialize")
	}
	data := []byte(fmt.Sprintf("\x25\x25\120\104\106\x2d\045\x64\056\x25\x64\x0a\x25\x25\342\xe3\xcf\323\012", writer.version.Major, writer.version.Minor))
	preObjByNum := make(map[int64]preserializedObject, len(preObjs))
	for _, preObj := range preObjs {
		preObjByNum[preObj.objNum] = preObj
	}
	linearizationDict := buildLinearizationDict(linearizationDictObjNum2, 0, 0, 0, 0, 0, 0, 0)
	count := len(linearizationDict)
	nums := append([]int64{linearizationDictObjNum2}, layout.firstSection...)
	nums = append(nums, n)
	if objStm != nil {
		nums = append(nums, objStm.firstXrefObjNum)
	}
	slices.Sort(nums)
	nums2 := append([]int64(nil), layout.mainSection...)
	if objStm != nil {
		nums2 = append(nums2, objStm.mainXrefObjNum)
		nums2 = append(nums2, objStm.firstXrefObjNum)
		nums2 = append(nums2, objStm.packedObjNums...)
	}
	slices.Sort(nums2)
	num := nums[len(nums)-1]
	if len(nums2) > 0 && nums2[len(nums2)-1] > num {
		num = nums2[len(nums2)-1]
	}
	size := -1
	length := -1
	length2 := -1
	for i := 0; i < maxLinearizationIterations; i++ {
		plan, sizes, err := planLinearizationPass(writer, layout, preObjByNum, n, objStm, data, count, nums, nums2, num, size, length, length2)
		if err != nil {
			return nil, err
		}
		if sizes.hintLen == size && sizes.firstXrefLen == length && sizes.mainXrefLen == length2 {
			updateSignatureByteRangeOffsets(preObjByNum)
			return plan, nil
		}
		size = sizes.hintLen
		length = sizes.firstXrefLen
		length2 = sizes.mainXrefLen
	}
	return nil, fmt.Errorf("linearization plan: variable-length sections did not stabilize in %d iterations", maxLinearizationIterations)
}

type planSizes struct {
	hintLen      int
	firstXrefLen int
	mainXrefLen  int
}
