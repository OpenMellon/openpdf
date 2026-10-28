package core

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"sync/atomic"

	"github.com/OpenMellon/openpdf/common"
)

func (parser *PdfParser) parseIndirectObject(cursor *readerAtCursor, visited map[int]struct{}) (PdfObject, error) {
	indirect := PdfIndirectObject{
		PdfObjectReference: PdfObjectReference{parser: parser},
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("-Read indirect obj")
	}
	data, err := cursor.Peek(20)
	if err != nil {
		if err != io.EOF {
			common.Log.Debug("ERROR: Fail to read indirect obj")
			return &indirect, err
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("(indirect obj peek \"%s\"", string(data))
	}
	stringSubmatchIndex := objHeaderRegexp.FindStringSubmatchIndex(string(data))
	if len(stringSubmatchIndex) < 6 {
		if err == io.EOF {
			return nil, err
		}
		common.Log.Debug("ERROR: Unable to find object signature (%s)", string(data))
		return &indirect, errors.New("unable to detect indirect object signature")
	}
	cursor.Discard(stringSubmatchIndex[0])
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Offsets % d", stringSubmatchIndex)
	}
	length := stringSubmatchIndex[1] - stringSubmatchIndex[0]
	peeked := make([]byte, length)
	_, err = cursor.ReadAtLeast(peeked, length)
	if err != nil {
		common.Log.Debug("ERROR: unable to read - %s", err)
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("textline: %s", peeked)
	}
	stringSubmatch := objHeaderRegexp.FindStringSubmatch(string(peeked))
	if len(stringSubmatch) < 3 {
		common.Log.Debug("ERROR: Unable to find object signature (%s)", string(peeked))
		return &indirect, errors.New("unable to detect indirect object signature")
	}
	objNum, _ := strconv.Atoi(stringSubmatch[1])
	genNum, _ := strconv.Atoi(stringSubmatch[2])
	indirect.ObjectNumber = int64(objNum)
	indirect.GenerationNumber = int64(genNum)
	for {
		afterObj, err2 := cursor.Peek(2)
		if err2 != nil {
			return &indirect, err2
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Ind. peek: %s (% x)!", string(afterObj), string(afterObj))
		}
		if IsWhiteSpace(afterObj[0]) {
			parser.skipSpaces(cursor)
		} else if afterObj[0] == '%' {
			parser.skipComments(cursor)
		} else if (afterObj[0] == '<') && (afterObj[1] == '<') {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Call parseDict")
			}
			indirect.PdfObject, err2 = parser.parseDict(cursor, 0)
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("EOF Call parseDict: %v", err2)
			}
			if err2 != nil {
				return &indirect, err2
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Parsed dictionary... finished.")
			}
		} else if (afterObj[0] == '/') || (afterObj[0] == '(') || (afterObj[0] == '[') || (afterObj[0] == '<') {
			indirect.PdfObject, err2 = parser.parseObject(cursor, 0)
			if err2 != nil {
				return &indirect, err2
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Parsed object ... finished.")
			}
		} else if afterObj[0] == ']' {
			common.Log.Debug("WARNING: ']' character not being used as an array ending marker. Skipping.")
			cursor.Discard(1)
		} else {
			if afterObj[0] == 'e' {
				textLine, err3 := parser.readTextLine(cursor)
				if err3 != nil {
					return nil, err3
				}
				if len(textLine) >= 6 && textLine[0:6] == "endobj" {
					break
				}
			} else if afterObj[0] == 's' {
				afterObj, _ = cursor.Peek(10)
				if string(afterObj[:6]) == "stream" {
					skip := 6
					if len(afterObj) > 6 {
						if IsWhiteSpace(afterObj[skip]) && afterObj[skip] != '\r' && afterObj[skip] != '\n' {
							common.Log.Debug("Non-conformant PDF not ending stream line properly with EOL marker")
							parser.metadata.nonConformantStream = true
							skip++
						}
						switch afterObj[skip] {
						case '\r':
							skip++
							if afterObj[skip] == '\n' {
								skip++
							}
						case '\n':
							skip++
						default:
							parser.metadata.nonConformantStream = true
						}
					}
					cursor.Discard(skip)
					dict, ok := indirect.PdfObject.(*PdfObjectDictionary)
					if !ok {
						return nil, errors.New("stream object missing dictionary")
					}
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("Stream dict %s", dict)
					}
					streamLength, err3 := parser.traceStreamLength(dict.Get("Length"), visited)
					if err3 != nil {
						common.Log.Debug("Fail to trace stream length: %v", err3)
						return nil, err3
					}
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("Stream length? %s", streamLength)
					}
					num, isNum := streamLength.(*PdfObjectInteger)
					if !isNum {
						return nil, errors.New("stream length needs to be an integer")
					}
					length2 := *num
					if length2 < 0 {
						return nil, errors.New("stream needs to be longer than 0")
					}
					fileOffset := cursor.GetFileOffset()
					nextOffset := parser.xrefNextObjectOffset(fileOffset)
					if fileOffset+int64(length2) > nextOffset && nextOffset > fileOffset {
						common.Log.Debug("Expected ending at %d", fileOffset+int64(length2))
						common.Log.Debug("Next object starting at %d", nextOffset)
						corrected := nextOffset - fileOffset - 17
						if corrected < 0 {
							return nil, errors.New("invalid stream length, going past boundaries")
						}
						common.Log.Debug("Attempting a length correction to %d...", corrected)
						length2 = PdfObjectInteger(corrected)
						dict.Set("Length", MakeInteger(corrected))
					}
					if int64(length2) > parser.fileSize {
						common.Log.Debug("ERROR: Stream length cannot be larger than file size")
						return nil, errors.New("invalid stream length, larger than file size")
					}
					streamData := make([]byte, length2)
					_, err3 = cursor.ReadAtLeast(streamData, int(length2))
					if err3 != nil {
						common.Log.Debug("ERROR stream (%d): %X", len(streamData), streamData)
						common.Log.Debug("ERROR: %v", err3)
						return nil, err3
					}
					stream := PdfObjectStream{
						Stream:              streamData,
						PdfObjectDictionary: indirect.PdfObject.(*PdfObjectDictionary),
						PdfObjectReference: PdfObjectReference{
							ObjectNumber:     indirect.ObjectNumber,
							GenerationNumber: indirect.GenerationNumber,
						},
					}
					stream.PdfObjectReference.parser = parser
					parser.skipSpaces(cursor)
					cursor.Discard(9)
					parser.skipSpaces(cursor)
					return &stream, nil
				}
			}
			if indirect.PdfObject != nil && afterObj[0] >= '0' && afterObj[0] <= '9' {
				tail := afterObj
				if len(afterObj) < 20 {
					tail, _ = cursor.Peek(20)
				}
				if objHeaderRegexp.Match(tail) {
					common.Log.Debug("WARNING: Missing endobj marker, detected new object starting")
					break
				}
			}
			indirect.PdfObject, err2 = parser.parseObject(cursor, 0)
			if indirect.PdfObject == nil {
				common.Log.Debug("INCOMPATIBILITY: Indirect object not containing an object - assuming null object")
				indirect.PdfObject = MakeNull()
			}
			return &indirect, err2
		}
	}
	if indirect.PdfObject == nil {
		common.Log.Debug("INCOMPATIBILITY: Indirect object not containing an object - assuming null object")
		indirect.PdfObject = MakeNull()
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Returning indirect!")
	}
	return &indirect, nil
}

func (parser *PdfParser) parseNull(cursor *readerAtCursor) (PdfObjectNull, error) {
	_, err := cursor.Discard(4)
	return PdfObjectNull{}, err
}

// ParseDict reads and parses a PDF dictionary object enclosed with '<<' and
// '>>' at the start of the source (offset 0). Convenience wrapper for
// ParseDictAt(0), intended for parsers built from a buffer where the dict
// is the entire content (e.g. NewParserFromString-based tests). Callers that
// need to parse at a different offset must use ParseDictAt(offset); the
// parser no longer carries an internal cursor for SetFileOffset-then-ParseDict
// patterns to work against.
func (parser *PdfParser) ParseDict() (*PdfObjectDictionary, error) { return parser.ParseDictAt(0) }

func (parser *PdfParser) initParser() error {
	pdfVersion, minor, err := parser.parsePdfVersion()
	if err != nil {
		common.Log.Error("Unable to parse version: %v", err)
		return err
	}
	parser.version.Major = pdfVersion
	parser.version.Minor = minor
	if parser.trailer, err = parser.loadXrefs(); err != nil {
		common.Log.Debug("ERROR: Failed to load xref table! %s", err)
		return err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Trailer: %s", parser.trailer)
	}
	linearizedDictionary, err := parser.parseLinearizedDictionary()
	if err != nil {
		return err
	}
	var ok bool
	if linearizedDictionary != nil {
		parser.linearized, err = parser.checkLinearizedInformation(linearizedDictionary)
		if err != nil {
			return err
		}
		ok = parser.linearized
		if parser.linearized && parser.linearizationObjNum > 0 {
			if byNumber, err2 := parser.LookupByNumber(parser.linearizationObjNum); err2 == nil {
				if dict, hasDict := GetDict(byNumber); !hasDict || dict.Get("Linearized") == nil {
					parser.linearized = false
				}
			}
		}
		if parser.linearized {
			parser.linearizationDict = linearizedDictionary
			parser.linearization = parser.buildLinearizationInfo(linearizedDictionary)
			if err2 := parser.parseHintStreamIfPresent(); err2 != nil {
				common.Log.Debug("WARN: hint stream decode failed: %v (continuing without hint tables)", err2)
			}
		}
	}
	if len(parser.currentXrefs().ObjectMap) == 0 {
		return fmt.Errorf("empty XREF table - Invalid")
	}
	parser.numRevisions = len(parser.xrefOffsets)
	if ok && parser.numRevisions != 0 {
		parser.numRevisions--
	}
	parser.revisions = make([]*PdfParser, parser.numRevisions)
	return nil
}

func newPdfParserAt(r io.ReaderAt, size int64, opts *ParserOpts, decodedTotal *atomic.Int64) (*PdfParser, error) {
	parser := &PdfParser{decodedTotal: decodedTotal, reader: r, fileSize: size, xrefOffsets: make([]int64, 0), revisionParsers: make(map[*PdfParser]*PdfParser), Opts: opts}
	if err := parser.initParser(); err != nil {
		return nil, err
	}
	return parser, nil
}

func (parser *PdfParser) newCursor(offset int64) *readerAtCursor {
	return newReaderAtCursor(parser.reader, parser.fileSize, offset)
}

func (parser *PdfParser) computeRevisionEndOffsets() ([]int64, bool, error) {
	eofMarkers, err := parser.scanEOFMarkers()
	if err != nil {
		return nil, false, err
	}
	if len(eofMarkers) == 0 {
		return nil, false, errEOFNotFound
	}
	want := parser.numRevisions + 1
	ok := false
	if markers, hasMarkers := parser.filterEOFMarkersByContext(eofMarkers); len(markers) >= want {
		eofMarkers = markers
		ok = hasMarkers
	}
	nums := make([]int64, len(eofMarkers))
	for i, marker := range eofMarkers {
		nums[i] = marker.endOffset
	}
	if len(nums) <= want {
		return nums, ok && len(nums) == want, nil
	}
	sorted := append([]int64(nil), parser.xrefOffsets...)
	slices.Sort(sorted)
	if len(sorted) > want-1 {
		sorted = sorted[len(sorted)-(want-1):]
	}
	ends := make([]int64, 0, want)
	idx := 0
	for _, off := range sorted {
		for idx < len(nums) && nums[idx] <= off {
			idx++
		}
		if idx >= len(nums)-1 {
			return nums[len(nums)-want:], false, nil
		}
		ends = append(ends, nums[idx])
		idx++
	}
	return append(ends, nums[len(nums)-1]), false, nil
}

func (parser *PdfParser) newRevisionParser(section *io.SectionReader) (*PdfParser, error) {
	if parser.compliance {
		compliancePdfParser, err := newCompliancePdfParser(section, section.Size(), parser.decodedTotal)
		if err != nil {
			return nil, err
		}
		compliancePdfParser.Opts = parser.Opts
		return compliancePdfParser, nil
	}
	var opts *ParserOpts
	if parser.Opts != nil {
		optsCopy := *parser.Opts
		opts = &optsCopy
	}
	return newPdfParserAt(section, section.Size(), opts, parser.decodedTotal)
}

// GetRevision returns PdfParser for the specific version of the Pdf document.
func (parser *PdfParser) GetRevision(revisionNumber int) (*PdfParser, error) {
	numRevisions := parser.numRevisions
	if numRevisions == revisionNumber {
		return parser, nil
	}
	if revisionNumber < 0 || numRevisions < revisionNumber {
		return nil, errors.New("undefined revisionNumber version")
	}
	if parser.revisions[revisionNumber] != nil {
		return parser.revisions[revisionNumber], nil
	}
	cur := parser
	for ; numRevisions > revisionNumber; numRevisions-- {
		previousRevisionParser, err := cur.GetPreviousRevisionParser()
		if err != nil {
			return nil, err
		}
		parser.revisions[numRevisions-1] = previousRevisionParser
		parser.revisionParsers[cur] = previousRevisionParser
		cur = previousRevisionParser
	}
	return cur, nil
}

func (parser *PdfParser) sectioningRevisionEnds() ([]int64, error) {
	if parser.numRevisions == 0 {
		return nil, errors.New("this is first revision")
	}
	nums, ok, err := parser.revisionEndOffsets()
	if err != nil {
		if errors.Is(err, errEOFNotFound) {
			return nil, fmt.Errorf("%w: no %%%%EOF markers located", ErrRevisionBoundaryNotFound)
		}
		return nil, err
	}
	want := parser.numRevisions + 1
	if len(nums) != want {
		return nil, fmt.Errorf("%w: located %d of %d revision end markers", ErrRevisionBoundaryNotFound, len(nums), want)
	}
	if !ok {
		return nil, fmt.Errorf("%w: %%%%EOF markers cannot be reconciled with %d revisions", ErrRevisionBoundaryNotFound, want)
	}
	return nums, nil
}

func (parser *PdfParser) revisionEndOffsets() ([]int64, bool, error) {
	parser.revisionEndsOnce.Do(func() {
		parser.revisionEnds, parser.revisionEndsValid, parser.revisionEndsErr = parser.computeRevisionEndOffsets()
	})
	if parser.revisionEndsErr != nil {
		return nil, false, parser.revisionEndsErr
	}
	return parser.revisionEnds, parser.revisionEndsValid, nil
}

// GetRevisionNumber returns the current version of the Pdf document.
func (parser *PdfParser) GetRevisionNumber() int { return parser.numRevisions }

// ParserMetadata gets the pdf parser metadata.
func (parser *PdfParser) ParserMetadata() (ParserMetadata, error) {
	if !parser.compliance {
		return ParserMetadata{}, fmt.Errorf("parser was not marked for getting detailed metadata")
	}
	return parser.metadata, nil
}

// GetPreviousRevision returns an io.SectionReader covering the previous
// revision of the document, ending at the boundary RevisionEndOffsets reports
// for it (past its %%EOF marker and the end-of-line sequence following it).
// The concrete *io.SectionReader satisfies both io.ReadSeeker and io.ReaderAt
// so it can be fed directly into NewParser / NewPdfReader.
// If the revision boundaries cannot be located, an error wrapping
// ErrRevisionBoundaryNotFound is returned.
func (parser *PdfParser) GetPreviousRevision() (*io.SectionReader, error) {
	nums, err := parser.sectioningRevisionEnds()
	if err != nil {
		return nil, err
	}
	return io.NewSectionReader(parser.reader, 0, nums[parser.numRevisions-1]), nil
}

// RevisionEndOffsets returns the exclusive end offset of each revision of the
// document, in ascending order: element i is the offset just past revision
// i's %%EOF marker plus at most one immediately following end-of-line
// sequence. The last element is the end of the final revision, which equals
// Size() unless trailing bytes follow the final %%EOF marker.
//
// The list is best-effort on damaged files: revision ends are located by
// scanning for physical %%EOF markers, so when the markers cannot be fully
// reconciled with the xref-derived revision count (see GetRevisionNumber) the
// result may hold fewer entries than there are revisions. The scan runs once
// per parser and is cached.
func (parser *PdfParser) RevisionEndOffsets() ([]int64, error) {
	nums, _, err := parser.revisionEndOffsets()
	if err != nil {
		return nil, err
	}
	ends := make([]int64, len(nums))
	copy(ends, nums)
	return ends, nil
}

// GetPreviousRevisionParser returns PdfParser for the previous version of the Pdf document.
func (parser *PdfParser) GetPreviousRevisionParser() (*PdfParser, error) {
	if parser.numRevisions == 0 {
		return nil, errors.New("this is first revision")
	}
	if prev, ok := parser.revisionParsers[parser]; ok {
		return prev, nil
	}
	nums, err := parser.sectioningRevisionEnds()
	if err != nil {
		return nil, err
	}
	sectionReader := io.NewSectionReader(parser.reader, 0, nums[parser.numRevisions-1])
	revisionParser, err := parser.newRevisionParser(sectionReader)
	if err != nil {
		return nil, err
	}
	if revisionParser.numRevisions == parser.numRevisions-1 {
		revisionParser.revisionEndsOnce.Do(func() {
			ends := make([]int64, parser.numRevisions)
			copy(ends, nums[:parser.numRevisions])
			revisionParser.revisionEnds = ends
			revisionParser.revisionEndsValid = true
		})
	}
	revisionParser.revisionParsers = parser.revisionParsers
	parser.revisionParsers[parser] = revisionParser
	return revisionParser, nil
}

// GetXrefOffset returns the offset of the xref table.
func (parser *PdfParser) GetXrefOffset() int64 { return parser.xrefOffset }

// GetXrefTable returns the PDF's current xref-table snapshot. The returned
// XrefTable is a shallow value copy: its ObjectMap and sortedObjects fields
// still alias the live snapshot's underlying map and slice, so the result
// should be treated as read-only. Mutating result.ObjectMap from the caller
// will corrupt parser state. Subsequent repair-driven snapshot swaps are
// not reflected in an already-returned copy.
func (parser *PdfParser) GetXrefTable() XrefTable { return *parser.currentXrefs() }
