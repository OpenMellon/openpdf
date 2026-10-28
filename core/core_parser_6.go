package core

import (
	"bytes"
	"errors"
	"io"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"

	"github.com/OpenMellon/openpdf/common"
)

func (parser *PdfParser) loadXrefs() (trailer *PdfObjectDictionary, err error) {
	parser.setXrefs(XrefTable{ObjectMap: map[int]XrefObject{}})
	parser.objStreams = make(objectStreams)
	defer func() {
		if err == nil {
			parser.setXrefs(*parser.currentXrefs())
		}
	}()
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("fsize: %d", parser.fileSize)
	}
	cursor := parser.newCursor(0)
	err = parser.seekToEOFMarker(cursor, parser.fileSize)
	if err != nil {
		common.Log.Debug("Failed seek to eof marker: %v", err)
		return nil, err
	}
	fileOffset := cursor.GetFileOffset()
	var tailLen int64 = 64
	start := max(fileOffset-tailLen, 0)
	cursor.SetFileOffset(start)
	data := make([]byte, tailLen)
	_, err = cursor.Read(data)
	if err != nil {
		common.Log.Debug("Failed reading while looking for startxref: %v", err)
		return nil, err
	}
	stringSubmatch := startxrefRegexp.FindStringSubmatch(string(data))
	if len(stringSubmatch) < 2 {
		common.Log.Debug("Error: startxref not found!")
		return nil, errors.New("startxref not found")
	}
	if len(stringSubmatch) > 2 {
		common.Log.Debug("ERROR: Multiple startxref (%s)!", data)
		return nil, errors.New("multiple startxref entries?")
	}
	offset, _ := strconv.ParseInt(stringSubmatch[1], 10, 64)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("startxref at %d", offset)
	}
	parser.startxrefOffset = offset
	if offset > parser.fileSize {
		common.Log.Debug("ERROR: Xref offset outside of file")
		common.Log.Debug("Attempting repair")
		offset, err = parser.repairLocateXref(cursor)
		if err != nil {
			common.Log.Debug("ERROR: Repair attempt failed (%s)")
			return nil, err
		}
	}
	cursor.SetFileOffset(offset)
	xref, err := parser.parseXref(cursor, &parser.xrefOffset)
	if err != nil {
		return nil, err
	}
	nums := []int64{offset}
	xRefStm := xref.Get("XRefStm")
	if xRefStm != nil {
		num, ok := xRefStm.(*PdfObjectInteger)
		if !ok {
			return nil, errors.New("XRefStm != int")
		}
		_, err = parser.parseXrefStream(cursor, num)
		if err != nil && parser.Opts != nil && !parser.Opts.RelaxedMode {
			return nil, err
		}
		if err == nil {
			nums = append(nums, int64(*num))
		}
	}
	xRefStm = xref.Get("Prev")
	for xRefStm != nil {
		num, ok := xRefStm.(*PdfObjectInteger)
		if !ok {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("Invalid Prev reference: Not a *PdfObjectInteger (%T)", xRefStm)
			}
			break
		}
		prevOffset := int64(*num)
		if slices.Contains(nums, prevOffset) || slices.Contains(parser.xrefOffsets, prevOffset) {
			common.Log.Debug("Preventing circular xref referencing")
			break
		}
		if prevOffset <= 0 || prevOffset >= parser.fileSize {
			common.Log.Debug("Prev xref offset outside of file, ignoring it")
			break
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Another Prev xref table object at %d", prevOffset)
		}
		cursor.SetFileOffset(prevOffset)
		dict, err2 := parser.parseXref(cursor, &parser.xrefOffset)
		if err2 != nil {
			common.Log.Debug("Warning: Error - Failed loading another (Prev) trailer")
			common.Log.Debug("Attempting to continue by ignoring it")
			break
		}
		parser.xrefOffsets = append(parser.xrefOffsets, prevOffset)
		xRefStm = dict.Get("Prev")
	}
	if parser.Opts != nil && parser.Opts.AutoRepairXrefTable {
		if parser.trailerHasEncrypt(xref) {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Encrypted file: deferring xref table health check until authentication")
			}
			parser.pendingXrefRepair = true
		} else if err2 := parser.checkAndRepairXrefTable(xref); err2 != nil {
			return xref, err2
		}
	}
	return xref, nil
}

func (parser *PdfParser) parseXref(cursor *readerAtCursor, xrefOffset *int64) (*PdfObjectDictionary, error) {
	parser.skipSpaces(cursor)
	const xrefProbeLen = 20
	data, _ := cursor.Peek(xrefProbeLen)
	for i := 0; i < 2; i++ {
		if xrefOffset != nil && *xrefOffset == 0 {
			*xrefOffset = cursor.GetFileOffset()
		}
		if objHeaderRegexp.Match(data) {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("xref points to an object. Probably xref object")
			}
			common.Log.Debug("starting with \"%s\"", string(data))
			xrefStream, err := parser.parseXrefStream(cursor, nil)
			return parser.recordXrefSectionEnd(cursor, xrefStream, err)
		}
		if xrefKeywordRegexp.Match(data) {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Standard xref section table!")
			}
			xrefTable, err := parser.parseXrefTable(cursor)
			return parser.recordXrefSectionEnd(cursor, xrefTable, err)
		}
		fileOffset := cursor.GetFileOffset()
		if xrefOffset != nil && *xrefOffset == 0 {
			*xrefOffset = fileOffset
		}
		cursor.SetFileOffset(fileOffset - xrefProbeLen)
		defer cursor.SetFileOffset(fileOffset)
		peeked, _ := cursor.Peek(xrefProbeLen)
		data = append(peeked, data...)
	}
	common.Log.Debug("Warning: Unable to find xref table or stream. Repair attempted: Looking for earliest xref from bottom.")
	if err := parser.repairSeekXrefMarker(cursor); err != nil {
		common.Log.Debug("Repair failed - %v", err)
		return nil, err
	}
	xrefTable, err := parser.parseXrefTable(cursor)
	return parser.recordXrefSectionEnd(cursor, xrefTable, err)
}

func (parser *PdfParser) recordXrefSectionEnd(cursor *readerAtCursor, trailer *PdfObjectDictionary, err error) (*PdfObjectDictionary, error) {
	if err == nil {
		parser.xrefSectionEnds = append(parser.xrefSectionEnds, cursor.GetFileOffset())
	}
	return trailer, err
}

func (parser *PdfParser) xrefNextObjectOffset(offset int64) int64 {
	xrefs := parser.currentXrefs()
	if len(xrefs.sorted) == 0 {
		return 0
	}
	idx := sort.Search(len(xrefs.sorted), func(i int) bool { return xrefs.sorted[i].Offset >= offset })
	offset2 := int64(0)
	if idx < len(xrefs.sorted) {
		offset2 = xrefs.sorted[idx].Offset
	}
	return offset2
}

// GetXrefType returns the type of the first xref object (table or stream).
func (parser *PdfParser) GetXrefType() *xrefType { return parser.xrefType }

func (parser *PdfParser) currentXrefs() *XrefTable {
	if xrefs := parser.xrefs.Load(); xrefs != nil {
		return xrefs
	}
	xrefs := &XrefTable{ObjectMap: map[int]XrefObject{}}
	if parser.xrefs.CompareAndSwap(nil, xrefs) {
		return xrefs
	}
	return parser.xrefs.Load()
}

func (parser *PdfParser) setXrefs(table XrefTable) {
	table.sorted = sortedXrefObjects(table.ObjectMap)
	parser.xrefs.Store(&table)
}

func (parser *PdfParser) rebuildXrefTableTopDown() error {
	xrefs, err := parser.repairRebuildXrefsTopDown()
	if err != nil {
		common.Log.Debug("ERROR: Failed xref rebuild repair (%s)", err)
		return err
	}
	if parser.currentXrefs() == parser.repairedXrefs.Load() {
		return nil
	}
	rebuilt := XrefTable{ObjectMap: make(map[int]XrefObject, len(xrefs.ObjectMap))}
	maps.Copy(rebuilt.ObjectMap, xrefs.ObjectMap)
	for i, xrefObj := range parser.currentXrefs().ObjectMap {
		if xrefObj.XType != XrefTypeObjectStream {
			continue
		}
		osXref, ok := xrefs.ObjectMap[xrefObj.OsObjNumber]
		if !ok {
			continue
		}
		if existing, ok2 := rebuilt.ObjectMap[i]; !ok2 || osXref.Offset > existing.Offset {
			rebuilt.ObjectMap[i] = xrefObj
		}
	}
	if latest := parser.currentXrefs(); maps.Equal(rebuilt.ObjectMap, latest.ObjectMap) {
		common.Log.Debug("Repaired xref table unchanged")
		parser.repairedXrefs.Store(latest)
		return nil
	}
	parser.setXrefs(rebuilt)
	parser.repairedXrefs.Store(parser.currentXrefs())
	parser.clearObjCache()
	common.Log.Debug("Repaired xref table built")
	return nil
}

func (parser *PdfParser) checkAndRepairXrefTable(trailer *PdfObjectDictionary) error {
	err := parser.xrefTableHealthCheck(trailer)
	if err == nil {
		return nil
	}
	common.Log.Debug("Xref table health check failed: %v, attempt repair", err)
	if err2 := parser.rebuildXrefTableTopDown(); err2 != nil {
		common.Log.Debug("Failed to rebuild xrefs: %v, file unrecoverable", err2)
		return err2
	}
	return nil
}

func (parser *PdfParser) rebuildXrefTable(visited map[int]struct{}) error {
	xrefs := XrefTable{}
	xrefs.ObjectMap = map[int]XrefObject{}
	m := map[int]int{}
	nums := make([]int, 0, len(parser.currentXrefs().ObjectMap))
	for i := range parser.currentXrefs().ObjectMap {
		nums = append(nums, i)
	}
	sort.Ints(nums)
	ok := true
	var delta int
	for i, oldNum := range nums {
		xrefObj := parser.currentXrefs().ObjectMap[oldNum]
		var (
			obj PdfObject
			err error
		)
		if _, ok2 := visited[oldNum]; ok2 {
			obj, _, err = parser.lookupByNumber(oldNum, false, visited)
		} else {
			obj, _, err = parser.lookupByNumberWrapperInternal(oldNum, false, visited)
		}
		if err != nil {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("ERROR: Unable to look up object (%s)", err)
				common.Log.Debug("ERROR: Xref table completely broken - attempting to repair ")
			}
			return parser.rebuildXrefTableTopDown()
		}
		newNum, newGen, err := indirectObjectIDs(obj)
		if err != nil {
			return err
		}
		if _, ok2 := xrefs.ObjectMap[int(newNum)]; ok2 {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("Xref renumbering collision: entries %d and %d both resolve to object %d - rebuilding top-down", m[int(newNum)], oldNum, newNum)
			}
			return parser.rebuildXrefTableTopDown()
		}
		if i == 0 {
			delta = int(newNum) - oldNum
		} else if int(newNum)-oldNum != delta {
			ok = false
		}
		xrefObj.ObjectNumber = int(newNum)
		xrefObj.Generation = int(newGen)
		xrefs.ObjectMap[int(newNum)] = xrefObj
		m[int(newNum)] = oldNum
	}
	if !ok {
		for _, num := range nums {
			if _, ok2 := xrefs.ObjectMap[num]; ok2 {
				continue
			}
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("Xref renumbering lost object %d - rebuilding top-down", num)
			}
			return parser.rebuildXrefTableTopDown()
		}
	}
	parser.setXrefs(xrefs)
	parser.clearObjCache()
	common.Log.Debug("New xref table built")
	logXrefTable(*parser.currentXrefs())
	return nil
}

func (parser *PdfParser) seekToEOFMarker(cursor *readerAtCursor, size int64) error {
	var scanned int64
	var chunk int64 = 2048
	for scanned < size-4 {
		if size <= (chunk + scanned) {
			chunk = size - scanned
		}
		cursor.SetFileOffset(size - scanned - chunk)
		data := make([]byte, chunk)
		cursor.Read(data)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Looking for EOF marker: \"%s\"", string(data))
		}
		allStringIndex := eofRegexp.FindAllStringIndex(string(data), -1)
		if allStringIndex != nil {
			nums := allStringIndex[len(allStringIndex)-1]
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Ind: % d", allStringIndex)
			}
			pos := size - scanned - chunk + int64(nums[0])
			cursor.SetFileOffset(pos)
			return nil
		}
		common.Log.Debug("Warning: EOF marker not found! - continue seeking")
		scanned += chunk - 4
	}
	common.Log.Debug("Error: EOF marker was not found.")
	return errEOFNotFound
}

func (parser *PdfParser) repairLocateXref(cursor *readerAtCursor) (int64, error) {
	backLen := int64(1000)
	cursor.SetFileOffset(cursor.GetFileOffset() - backLen)
	fileOffset := cursor.GetFileOffset()
	data := make([]byte, backLen)
	cursor.Read(data)
	allStringIndex := xrefLineRegexp.FindAllStringIndex(string(data), -1)
	if len(allStringIndex) < 1 {
		common.Log.Debug("ERROR: Repair: xref not found!")
		return 0, errors.New("repair: xref not found")
	}
	idx := int64(allStringIndex[len(allStringIndex)-1][0])
	pos := fileOffset + idx
	return pos, nil
}

func (parser *PdfParser) checkPostEOFData() error {
	const eofMarkerText = "%%EOF"
	data := make([]byte, len([]byte(eofMarkerText))+1)
	start := max(parser.fileSize-int64(len(data)), 0)
	if _, err := parser.reader.ReadAt(data, start); err != nil && err != io.EOF {
		return err
	}
	if string(data) == eofMarkerText || string(data) == eofMarkerText+"\n" {
		parser.metadata.dataAfterEOF = true
	}
	return nil
}

func (parser *PdfParser) filterEOFMarkersByContext(markers []eofMarker) ([]eofMarker, bool) {
	m := make(map[int64]struct{}, len(parser.xrefOffsets)+1)
	if parser.startxrefOffset > 0 {
		m[parser.startxrefOffset] = struct{}{}
	}
	for _, xrefOffset := range parser.xrefOffsets {
		m[xrefOffset] = struct{}{}
	}
	nums := append([]int64(nil), parser.xrefSectionEnds...)
	slices.Sort(nums)
	var result []eofMarker
	seen := make(map[int64]struct{}, len(m))
	prevEnd := make(map[int]int64, len(nums))
	ok := true
	for _, marker := range markers {
		idx := sort.Search(len(nums), func(i int) bool { return nums[i] > marker.markerOffset }) - 1
		if idx < 0 {
			continue
		}
		if prevOff, ok2 := prevEnd[idx]; ok2 {
			if tailOffset, ok3 := parser.trailerTailOffset(prevOff, marker.markerOffset); ok3 {
				if _, ok4 := m[tailOffset]; ok4 {
					ok = false
				}
			}
			continue
		}
		tail, ok2 := parser.trailerTailOffset(nums[idx], marker.markerOffset)
		if !ok2 {
			continue
		}
		if _, ok3 := m[tail]; !ok3 {
			continue
		}
		prevEnd[idx] = marker.endOffset
		if _, ok3 := seen[tail]; ok3 {
			ok = false
		}
		seen[tail] = struct{}{}
		result = append(result, marker)
	}
	return result, ok
}

func (parser *PdfParser) scanEOFMarkers() ([]eofMarker, error) {
	data := []byte("%%EOF")
	const eofScanChunkSize = int64(64 * 1024)
	extra := int64(len(data)) + 2
	var markers []eofMarker
	buf := make([]byte, eofScanChunkSize+extra)
	for i := int64(0); i < parser.fileSize; i += eofScanChunkSize {
		readLen := eofScanChunkSize + extra
		if i+readLen > parser.fileSize {
			readLen = parser.fileSize - i
		}
		chunk := buf[:readLen]
		if at, err := parser.reader.ReadAt(chunk, i); err != nil && (err != io.EOF || int64(at) != readLen) {
			return nil, err
		}
		for j := int64(0); ; {
			rel := int64(bytes.Index(chunk[j:], data))
			if rel < 0 {
				break
			}
			found := j + rel
			j = found + int64(len(data))
			if found >= eofScanChunkSize {
				continue
			}
			end := j
			if end < readLen && chunk[end] == '\r' {
				end++
			}
			if end < readLen && chunk[end] == '\n' {
				end++
			}
			markers = append(markers, eofMarker{markerOffset: i + found, endOffset: i + end})
		}
	}
	return markers, nil
}

func (parser *PdfParser) repairSeekXrefMarker(cursor *readerAtCursor) error {
	fileSize := parser.fileSize
	re := regexp.MustCompile("\\sxref\\s*")
	var pos int64
	var window int64 = 1000
	for pos < fileSize {
		if fileSize <= (window + pos) {
			window = fileSize - pos
		}
		cursor.SetFileOffset(fileSize - pos - window)
		data := make([]byte, window)
		cursor.Read(data)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Looking for xref : \"%s\"", string(data))
		}
		allStringIndex := re.FindAllStringIndex(string(data), -1)
		if allStringIndex != nil {
			nums := allStringIndex[len(allStringIndex)-1]
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Ind: % d", allStringIndex)
			}
			cursor.SetFileOffset(fileSize - pos - window + int64(nums[0]))
			for {
				peeked, err := cursor.Peek(1)
				if err != nil {
					return err
				}
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("B: %d %c", peeked[0], peeked[0])
				}
				if !IsWhiteSpace(peeked[0]) {
					break
				}
				cursor.Discard(1)
			}
			return nil
		}
		common.Log.Debug("Warning: EOF marker not found! - continue seeking")
		pos += window
	}
	common.Log.Debug("Error: Xref table marker was not found.")
	return errors.New("xref not found ")
}

func (parser *PdfParser) doRepairRebuildXrefsTopDown() (*XrefTable, error) {
	cursor := parser.newCursor(0)
	winLen := 20
	data := make([]byte, winLen)
	xrefs := XrefTable{}
	xrefs.ObjectMap = make(map[int]XrefObject)
	for {
		b, err := cursor.ReadByte()
		if err != nil {
			if err == io.EOF {
				break
			} else {
				return nil, err
			}
		}
		if b == 'j' && data[winLen-1] == 'b' && data[winLen-2] == 'o' && IsWhiteSpace(data[winLen-3]) {
			pos := winLen - 4
			for IsWhiteSpace(data[pos]) && pos > 0 {
				pos--
			}
			if pos == 0 || !IsDecimalDigit(data[pos]) {
				continue
			}
			for IsDecimalDigit(data[pos]) && pos > 0 {
				pos--
			}
			if pos == 0 || !IsWhiteSpace(data[pos]) {
				continue
			}
			for IsWhiteSpace(data[pos]) && pos > 0 {
				pos--
			}
			if pos == 0 || !IsDecimalDigit(data[pos]) {
				continue
			}
			for IsDecimalDigit(data[pos]) && pos > 0 {
				pos--
			}
			if pos == 0 {
				continue
			}
			objOffset := cursor.GetFileOffset() - int64(winLen-pos)
			candidate := append(data[pos+1:], b)
			objSignature, genNum, err2 := parseObjSignature(string(candidate))
			if err2 != nil {
				common.Log.Debug("Unable to parse object number: %v", err2)
				return nil, err2
			}
			if xrefObj, ok := xrefs.ObjectMap[objSignature]; !ok || xrefObj.Generation <= genNum {
				entry := XrefObject{}
				entry.XType = XrefTypeTableEntry
				entry.ObjectNumber = objSignature
				entry.Generation = genNum
				entry.Offset = objOffset
				xrefs.ObjectMap[objSignature] = entry
			}
		}
		data = append(data[1:winLen], b)
	}
	return &xrefs, nil
}

func (parser *PdfParser) repairRebuildXrefsTopDown() (*XrefTable, error) {
	parser.repairOnce.Do(func() { parser.repair.xrefs, parser.repair.err = parser.doRepairRebuildXrefsTopDown() })
	return parser.repair.xrefs, parser.repair.err
}
