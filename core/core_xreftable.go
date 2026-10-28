package core

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/OpenMellon/openpdf/common"
)

func (parser *PdfParser) xrefTableHealthCheck(trailer *PdfObjectDictionary) error {
	if size, ok := trailer.Get("Size").(*PdfObjectInteger); ok {
		half := int64(*size) / 2
		if int64(len(parser.currentXrefs().ObjectMap)) < half {
			return errors.New("mapped object too low compared with total trailer size")
		}
	}
	root := trailer.Get("Root")
	if root == nil {
		return errors.New("root object is missing")
	}
	obj, err := parser.Resolve(root)
	if err != nil {
		return err
	}
	if _, ok := obj.(*PdfObjectDictionary); !ok {
		return errors.New("catalog object is not a dictionary")
	}
	pages := obj.(*PdfObjectDictionary).Get("Pages")
	if pages == nil {
		return errors.New("pages object not found in catalog")
	}
	pagesObj, err := parser.Resolve(pages)
	if err != nil {
		return err
	}
	if _, ok := pagesObj.(*PdfObjectDictionary); !ok {
		return errors.New("pages object is not a dictionary")
	}
	return nil
}

func (parser *PdfParser) parseXrefTable(cursor *readerAtCursor) (*PdfObjectDictionary, error) {
	defer parser.repairedXrefs.Store(nil)
	var dict *PdfObjectDictionary
	textLine, err := parser.readTextLine(cursor)
	if err != nil {
		return nil, err
	}
	if parser.compliance && strings.Count(strings.TrimPrefix(textLine, "xref"), " ") > 0 {
		parser.metadata.invalidSeparationAfterXRef = true
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("xref first line: %s", textLine)
	}
	start := -1
	subsectionCount := 0
	ok := false
	s := ""
	for {
		parser.skipSpaces(cursor)
		_, err2 := cursor.Peek(1)
		if err2 != nil {
			return nil, err2
		}
		textLine, err2 = parser.readTextLine(cursor)
		if err2 != nil {
			return nil, err2
		}
		stringSubmatch := xrefSubsectionRegexp.FindStringSubmatch(textLine)
		if len(stringSubmatch) == 0 {
			ok2 := len(s) > 0
			s += textLine + "\n"
			if ok2 {
				stringSubmatch = xrefSubsectionRegexp.FindStringSubmatch(s)
			}
		}
		if len(stringSubmatch) == 3 {
			if parser.compliance && !parser.metadata.invalidSubsectionHeader {
				var (
					ok2    bool
					digits int
				)
				for _, r := range textLine {
					if unicode.IsDigit(r) {
						if ok2 {
							break
						}
						continue
					}
					if !ok2 {
						ok2 = true
					}
					digits++
				}
				if digits > 1 {
					parser.metadata.invalidSubsectionHeader = true
				}
			}
			firstObj, _ := strconv.Atoi(stringSubmatch[1])
			count, _ := strconv.Atoi(stringSubmatch[2])
			start = firstObj
			subsectionCount = count
			ok = true
			s = ""
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("xref subsection: first object: %d objects: %d", start, subsectionCount)
			}
			continue
		}
		strs := xrefEntryRegexp.FindStringSubmatch(textLine)
		if len(strs) == 4 {
			if !ok {
				common.Log.Debug("ERROR Xref invalid format!\n")
				return nil, errors.New("xref invalid format")
			}
			offset, _ := strconv.ParseInt(strs[1], 10, 64)
			gen, _ := strconv.Atoi(strs[2])
			kind := strs[3]
			s = ""
			if strings.ToLower(kind) == "n" && offset > 1 {
				xrefObj, ok2 := parser.currentXrefs().ObjectMap[start]
				if !ok2 || gen > xrefObj.Generation {
					entry := XrefObject{ObjectNumber: start, XType: XrefTypeTableEntry, Offset: offset, Generation: gen}
					parser.currentXrefs().ObjectMap[start] = entry
				}
			}
			start++
			continue
		}
		if (len(textLine) > 6) && (textLine[:7] == "trailer") {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Found trailer - %s", textLine)
			}
			if len(textLine) > 9 {
				fileOffset := cursor.GetFileOffset()
				cursor.SetFileOffset(fileOffset - int64(len(textLine)) + 7)
			}
			parser.skipSpaces(cursor)
			parser.skipComments(cursor)
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Reading trailer dict!")
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("peek: \"%s\"", textLine)
			}
			dict, err2 = parser.parseDict(cursor, 0)
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("EOF reading trailer dict!")
			}
			if err2 != nil {
				common.Log.Debug("Error parsing trailer dict (%s)", err2)
				return nil, err2
			}
			break
		}
		if textLine == "%%EOF" {
			common.Log.Debug("ERROR: end of file - trailer not found - error!")
			return nil, errors.New("end of file - trailer not found")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("xref more : %s", textLine)
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("EOF parsing xref table!")
	}
	if parser.xrefType == nil {
		xrefType := XrefTypeTableEntry
		parser.xrefType = &xrefType
	}
	return dict, nil
}

func (parser *PdfParser) parseXrefStream(cursor *readerAtCursor, xrefStm *PdfObjectInteger) (*PdfObjectDictionary, error) {
	defer parser.repairedXrefs.Store(nil)
	if xrefStm != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("XRefStm xref table object at %d", xrefStm)
		}
		cursor.SetFileOffset(int64(*xrefStm))
	}
	fileOffset := cursor.GetFileOffset()
	indirectObject, err := parser.parseIndirectObject(cursor, map[int]struct{}{})
	if err != nil {
		common.Log.Debug("ERROR: Failed to read xref object")
		return nil, errors.New("failed to read xref object")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("XRefStm object: %s", indirectObject)
	}
	stream, ok := indirectObject.(*PdfObjectStream)
	if !ok {
		common.Log.Debug("ERROR: XRefStm pointing to non-stream object!")
		return nil, errors.New("XRefStm pointing to a non-stream object")
	}
	pdfObjectDictionary := stream.PdfObjectDictionary
	size, ok := stream.Get("Size").(*PdfObjectInteger)
	if !ok {
		common.Log.Debug("ERROR: Missing size from xref stm")
		return nil, errors.New("missing Size from xref stm")
	}
	if int64(*size) > 8388607 {
		common.Log.Debug("ERROR: xref Size exceeded limit, over 8388607 (%d)", *size)
		return nil, errors.New("range check error")
	}
	w := stream.Get("W")
	arr, ok := w.(*PdfObjectArray)
	if !ok {
		return nil, errors.New("invalid W in xref stream")
	}
	wLen := arr.Len()
	if wLen != 3 {
		common.Log.Debug("ERROR: Unsupported xref stm (len(W) != 3 - %d)", wLen)
		return nil, errors.New("unsupported xref stm len(W) != 3")
	}
	var nums []int64
	for i := 0; i < 3; i++ {
		num, hasNum := GetInt(arr.Get(i))
		if !hasNum {
			return nil, errors.New("invalid w object type")
		}
		nums = append(nums, int64(*num))
	}
	data, err := DecodeStream(stream)
	if err != nil {
		common.Log.Debug("ERROR: Unable to decode stream: %v", err)
		return nil, err
	}
	typeEnd := int(nums[0])
	field2End := int(nums[0] + nums[1])
	field3End := int(nums[0] + nums[1] + nums[2])
	entrySize := int(nums[0] + nums[1] + nums[2])
	if typeEnd < 0 || field2End < 0 || field3End < 0 {
		common.Log.Debug("Error s value < 0 (%d,%d,%d)", typeEnd, field2End, field3End)
		return nil, errors.New("range check error")
	}
	if entrySize == 0 {
		common.Log.Debug("No xref objects in stream (deltab == 0)")
		return pdfObjectDictionary, nil
	}
	numEntries := len(data) / entrySize
	numObjects := 0
	index := stream.Get("Index")
	var objNums []int
	if index != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Index: %b", index)
		}
		indexArr, isArr := index.(*PdfObjectArray)
		if !isArr {
			common.Log.Debug("Invalid Index object (should be an array)")
			return nil, errors.New("invalid Index object")
		}
		if indexArr.Len()%2 != 0 {
			common.Log.Debug("WARNING Failure loading xref stm index not multiple of 2.")
			return nil, errors.New("range check error")
		}
		numObjects = 0
		indexPairs, err2 := indexArr.ToIntegerArray()
		if err2 != nil {
			common.Log.Debug("Error getting index array as integers: %v", err2)
			return nil, err2
		}
		for i := 0; i < len(indexPairs); i += 2 {
			first := indexPairs[i]
			count := indexPairs[i+1]
			for j := 0; j < count; j++ {
				objNums = append(objNums, first+j)
			}
			numObjects += count
		}
	} else {
		for i := 0; i < int(*size); i++ {
			objNums = append(objNums, i)
		}
		numObjects = int(*size)
	}
	if numEntries == numObjects+1 {
		common.Log.Debug("Incompatibility: Index missing coverage of 1 object - appending one - May lead to problems")
		maxNum := numObjects - 1
		for _, num := range objNums {
			if num > maxNum {
				maxNum = num
			}
		}
		objNums = append(objNums, maxNum+1)
		numObjects++
	}
	if numEntries != len(objNums) {
		common.Log.Debug("ERROR: xref stm: num entries != len(indices) (%d != %d)", numEntries, len(objNums))
		return nil, errors.New("xref stm num entries != len(indices)")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Objects count %d", numObjects)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Indices: % d", objNums)
	}
	bytesToInt := func(b []byte) int64 {
		var result int64
		for i := 0; i < len(b); i++ {
			result += int64(b[i]) * (1 << uint(8*(len(b)-i-1)))
		}
		return result
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Decoded stream length: %d", len(data))
	}
	entryIdx := 0
	for i := 0; i < len(data); i += entrySize {
		err2 := checkSliceBounds(len(data), i, i+typeEnd)
		if err2 != nil {
			common.Log.Debug("Invalid slice range: %v", err2)
			return nil, err2
		}
		field1 := data[i : i+typeEnd]
		err2 = checkSliceBounds(len(data), i+typeEnd, i+field2End)
		if err2 != nil {
			common.Log.Debug("Invalid slice range: %v", err2)
			return nil, err2
		}
		field2 := data[i+typeEnd : i+field2End]
		err2 = checkSliceBounds(len(data), i+field2End, i+field3End)
		if err2 != nil {
			common.Log.Debug("Invalid slice range: %v", err2)
			return nil, err2
		}
		field3 := data[i+field2End : i+field3End]
		entryType := bytesToInt(field1)
		val2 := bytesToInt(field2)
		val3 := bytesToInt(field3)
		if nums[0] == 0 {
			entryType = 1
		}
		if entryIdx >= len(objNums) {
			common.Log.Debug("XRef stream - Trying to access index out of bounds - breaking")
			break
		}
		objNum := objNums[entryIdx]
		entryIdx++
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("%d. p1: % x", objNum, field1)
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("%d. p2: % x", objNum, field2)
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("%d. p3: % x", objNum, field3)
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("%d. xref: %d %d %d", objNum, entryType, val2, val3)
		}
		switch entryType {
		case 0:
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("- Free object - can probably ignore")
			}
		case 1:
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("- In use - uncompressed via offset %b", field2)
			}
			if val2 == fileOffset {
				common.Log.Debug("Updating object number for XRef table %d -> %d", objNum, stream.ObjectNumber)
				objNum = int(stream.ObjectNumber)
			}
			if xrefObj, ok2 := parser.currentXrefs().ObjectMap[objNum]; !ok2 || int(val3) > xrefObj.Generation {
				entry := XrefObject{ObjectNumber: objNum, XType: XrefTypeTableEntry, Offset: val2, Generation: int(val3)}
				parser.currentXrefs().ObjectMap[objNum] = entry
			}
		case 2:
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("- In use - compressed object")
			}
			if _, ok2 := parser.currentXrefs().ObjectMap[objNum]; !ok2 {
				xrefObj := XrefObject{ObjectNumber: objNum, XType: XrefTypeObjectStream, OsObjNumber: int(val2), OsObjIndex: int(val3)}
				parser.currentXrefs().ObjectMap[objNum] = xrefObj
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("entry: %+v", xrefObj)
				}
			}
		default:
			common.Log.Debug("ERROR: --------INVALID TYPE XrefStm invalid?-------")
			continue
		}
	}
	if parser.xrefType == nil {
		xrefType := XrefTypeObjectStream
		parser.xrefType = &xrefType
	}
	return pdfObjectDictionary, nil
}

var xrefEntryRegexp = regexp.MustCompile("(\\d+)\\s+(\\d+)\\s+([nf])\\s*$")

var xrefSubsectionRegexp = regexp.MustCompile("(\\d+)\\s+(\\d+)\\s*$")
