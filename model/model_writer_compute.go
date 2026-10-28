package model

import (
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/core"
)

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
