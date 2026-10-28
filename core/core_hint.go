package core

import (
	"errors"
	"fmt"

	"github.com/OpenMellon/openpdf/common"
)

// HintTables holds the decoded hint stream tables from a linearized PDF.
// The Page Offset and Shared Object tables are mandatory; the rest are
// optional and may be nil if the producer did not emit them.
type HintTables struct {
	PageOffset       *PageOffsetHintTable
	SharedObject     *SharedObjectHintTable
	Thumbnail        *ThumbnailHintTable
	OutlineHierarchy *GenericHintTable
	ArticleThread    *GenericHintTable
	NamedDestination *GenericHintTable
	InteractiveForm  *GenericHintTable
	InformationDict  *GenericHintTable
	LogicalStructure *GenericHintTable
	PageLabel        *GenericHintTable
	Rendition        *GenericHintTable
	EmbeddedFile     *GenericHintTable
}

func parseHintTables(data []byte, dict *PdfObjectDictionary, numPages int) (*HintTables, error) {
	if dict == nil {
		return nil, errors.New("hint stream missing dictionary")
	}
	hints := &HintTables{}
	bitReader := newBitReader(data)
	pageOffsetHintTable, err := readPageOffsetHintTable(bitReader, numPages)
	if err != nil {
		return nil, fmt.Errorf("page offset hint table: %w", err)
	}
	hints.PageOffset = pageOffsetHintTable
	nonNegativeInt, ok := getNonNegativeInt(dict, "S")
	if !ok {
		return hints, errors.New("hint stream dictionary missing required /S offset")
	}
	if err2 := bitReader.seek(nonNegativeInt); err2 != nil {
		return hints, fmt.Errorf("seek to shared object table: %w", err2)
	}
	sharedObjectHintTable, err := readSharedObjectHintTable(bitReader)
	if err != nil {
		return hints, fmt.Errorf("shared object hint table: %w", err)
	}
	hints.SharedObject = sharedObjectHintTable
	sts := []struct {
		name   string
		assign func(*HintTables, *GenericHintTable)
	}{{"O", func(tables *HintTables, generic *GenericHintTable) { tables.OutlineHierarchy = generic }}, {"A", func(tables *HintTables, generic *GenericHintTable) { tables.ArticleThread = generic }}, {"E", func(tables *HintTables, generic *GenericHintTable) { tables.NamedDestination = generic }}, {"V", func(tables *HintTables, generic *GenericHintTable) { tables.InteractiveForm = generic }}, {"I", func(tables *HintTables, generic *GenericHintTable) { tables.InformationDict = generic }}, {"C", func(tables *HintTables, generic *GenericHintTable) { tables.LogicalStructure = generic }}, {"L", func(tables *HintTables, generic *GenericHintTable) { tables.PageLabel = generic }}, {"R", func(tables *HintTables, generic *GenericHintTable) { tables.Rendition = generic }}, {"B", func(tables *HintTables, generic *GenericHintTable) { tables.EmbeddedFile = generic }}}
	for _, st := range sts {
		offset, hasNonNegativeInt := getNonNegativeInt(dict, PdfObjectName(st.name))
		if !hasNonNegativeInt {
			continue
		}
		if err2 := bitReader.seek(offset); err2 != nil {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("WARN: optional hint table /%s seek failed: %v", st.name, err2)
			}
			continue
		}
		firstObjNum, err2 := bitReader.readBits(32)
		firstOffset, err3 := bitReader.readBits(32)
		numObjects, err4 := bitReader.readBits(32)
		groupLength, err5 := bitReader.readBits(32)
		if err2 != nil || err3 != nil || err4 != nil || err5 != nil {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("WARN: optional hint table /%s header truncated", st.name)
			}
			continue
		}
		st.assign(hints, &GenericHintTable{FirstObjNum: firstObjNum, FirstOffset: firstOffset, NumObjects: numObjects, GroupLength: groupLength})
	}
	if offset, hasNonNegativeInt := getNonNegativeInt(dict, "T"); hasNonNegativeInt {
		if err2 := bitReader.seek(offset); err2 == nil {
			if thumbnailHintTable, err3 := readThumbnailHintTable(bitReader); err3 == nil {
				hints.Thumbnail = thumbnailHintTable
			}
		}
	}
	return hints, nil
}

// HintRange is a (file offset, length) pair from the linearization
// dictionary's /H array describing where the hint stream(s) live.
type HintRange struct {
	Offset int64
	Length int64
}

func parseHintRanges(arr *PdfObjectArray) []HintRange {
	objs := arr.Elements()
	if len(objs) != 2 && len(objs) != 4 {
		return nil
	}
	nums := make([]int64, len(objs))
	for i, obj := range objs {
		n, err := GetNumberAsInt64(obj)
		if err != nil {
			return nil
		}
		nums[i] = n
	}
	hintRanges := []HintRange{{Offset: nums[0], Length: nums[1]}}
	if len(nums) == 4 {
		hintRanges = append(hintRanges, HintRange{Offset: nums[2], Length: nums[3]})
	}
	return hintRanges
}
