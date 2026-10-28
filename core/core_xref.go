package core

import (
	"errors"
	"regexp"
	"slices"
	"sort"
	"strconv"

	"github.com/OpenMellon/openpdf/common"
)

// GetXrefOffset returns the offset of the xref table.
func (parser *PdfParser) GetXrefOffset() int64 { return parser.xrefOffset }

// GetXrefTable returns the PDF's current xref-table snapshot. The returned
// XrefTable is a shallow value copy: its ObjectMap and sortedObjects fields
// still alias the live snapshot's underlying map and slice, so the result
// should be treated as read-only. Mutating result.ObjectMap from the caller
// will corrupt parser state. Subsequent repair-driven snapshot swaps are
// not reflected in an already-returned copy.
func (parser *PdfParser) GetXrefTable() XrefTable { return *parser.currentXrefs() }

var xrefLineRegexp = regexp.MustCompile("[\\r\\n]\\s*(xref)\\s*[\\r\\n]")

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

type xrefType int

var xrefKeywordRegexp = regexp.MustCompile("\\s*xref\\s*")

func sortedXrefObjects(objectMap map[int]XrefObject) []XrefObject {
	count := 0
	for _, xrefObj := range objectMap {
		if xrefObj.Offset > 0 {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	xrefObjs := make([]XrefObject, 0, count)
	for _, xrefObj := range objectMap {
		if xrefObj.Offset > 0 {
			xrefObjs = append(xrefObjs, xrefObj)
		}
	}
	sort.Slice(xrefObjs, func(i, j int) bool { return xrefObjs[i].Offset < xrefObjs[j].Offset })
	return xrefObjs
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

// XrefObject defines a cross reference entry which is a map between object number (with generation number) and the
// location of the actual object, either as a file offset (xref table entry), or as a location within an xref
// stream object (xref object stream).
type XrefObject struct {
	XType        xrefType
	ObjectNumber int
	Generation   int

	// For normal xrefs (defined by OFFSET)
	Offset int64

	// For xrefs to object streams.
	OsObjNumber int
	OsObjIndex  int
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

func logXrefTable(table XrefTable) {
	common.Log.Debug("=X=X=X=")
	common.Log.Debug("Xref table:")
	idx := 0
	for _, xrefObj := range table.ObjectMap {
		common.Log.Debug("i+1: %d (obj num: %d gen: %d) -> %d", idx+1, xrefObj.ObjectNumber, xrefObj.Generation, xrefObj.Offset)
		idx++
	}
}

const (
	XrefTypeTableEntry   xrefType = iota
	XrefTypeObjectStream xrefType = iota
)

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

// XrefTable represents the cross references in a PDF, i.e. the table of
// objects and information where to access within the PDF file. Instances are
// treated as immutable snapshots once published via parser.setXrefs — repair
// paths build a new XrefTable rather than mutating an existing one.
type XrefTable struct {
	ObjectMap map[int]XrefObject
	sorted    []XrefObject
}
