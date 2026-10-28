// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package core defines and implements the primitive PDF object types in golang, and provides functionality
// for parsing those from a PDF file stream. This includes I/O handling, cross references, repairs, encryption,
// encoding and other core capabilities.
package core

import (
	"errors"
	"fmt"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/lazystream"
)

func checkParamDictEnd(firstXrefOffset int64) *LinearizationViolation {
	if firstXrefOffset <= 0 || firstXrefOffset > maxLinearizationParamDictEnd {
		return &LinearizationViolation{Code: LinErrParamDictNotFirstObject, Severity: SeverityError, Message: fmt.Sprintf("linearization parameter dictionary does not fit within the first %d bytes (ends at %d)", maxLinearizationParamDictEnd, firstXrefOffset)}
	}
	return nil
}

func init() {
	lazystream.Take = func(owner, target any) ([]byte, bool) {
		crypt, ok := owner.(*PdfCrypt)
		stream, isStream := target.(*PdfObjectStream)
		if !ok || !isStream || crypt == nil {
			return nil, false
		}
		return crypt.takeLazyStreamData(stream)
	}
	lazystream.Drop = func(v any) {
		if crypt, ok := v.(*PdfCrypt); ok && crypt != nil {
			crypt.dropLazyStreamData()
		}
	}
}

func init() { maxDecodedStreamSize.Store(defaultMaxDecodedStreamSize) }

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
