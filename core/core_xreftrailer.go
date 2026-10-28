package core

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strconv"
)

func (parser *PdfParser) trailerTailOffset(start, end int64) (int64, bool) {
	if end <= start {
		return 0, false
	}
	readerAtCursor := newReaderAtCursor(parser.reader, end, start)
	parser.skipSpaces(readerAtCursor)
	if data, _ := readerAtCursor.Peek(6); string(data) == "endobj" {
		readerAtCursor.Discard(6)
	}
	if err := parser.skipComments(readerAtCursor); err != nil {
		return 0, false
	}
	if data, err := readerAtCursor.Peek(9); err != nil || string(data) != "startxref" {
		return 0, false
	}
	readerAtCursor.Discard(9)
	if skipped, err := parser.skipSpaces(readerAtCursor); err != nil || skipped == 0 {
		return 0, false
	}
	var data []byte
	for {
		b, err := readerAtCursor.ReadByte()
		if err != nil {
			break
		}
		if b < '0' || b > '9' {
			readerAtCursor.UnreadByte()
			break
		}
		if len(data) == 19 {
			return 0, false
		}
		data = append(data, b)
	}
	offset, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return 0, false
	}
	for {
		b, err2 := readerAtCursor.ReadByte()
		if err2 != nil {
			return offset, true
		}
		if !IsWhiteSpace(b) {
			return 0, false
		}
	}
}

// GetTrailer returns the PDFs trailer dictionary. The trailer dictionary is typically the starting point for a PDF,
// referencing other key objects that are important in the document structure.
func (parser *PdfParser) GetTrailer() *PdfObjectDictionary { return parser.trailer }

func checkFirstXrefPrev(present bool, prevOffset, endOfFirstPage, fileSize int64) *LinearizationViolation {
	ok := !present || prevOffset <= 0 || (endOfFirstPage > 0 && prevOffset < endOfFirstPage) || (fileSize > 0 && prevOffset >= fileSize)
	if ok {
		return &LinearizationViolation{Code: LinErrPrevChainBroken, Severity: SeverityError, Message: fmt.Sprintf("first-xref /Prev (present=%v, offset=%d) does not point past the first-page section into the file body (/E %d, size %d)", present, prevOffset, endOfFirstPage, fileSize)}
	}
	return nil
}

func (parser *PdfParser) checkFirstXrefPrevChain(linInfo *LinearizationInfo) *LinearizationViolation {
	dict, err := parser.FirstXrefTrailer()
	if err != nil || dict == nil {
		return &LinearizationViolation{Code: LinErrPrevChainBroken, Severity: SeverityError, Message: "unable to read the first xref trailer to verify the /Prev chain"}
	}
	prev := dict.Get("Prev")
	ok := prev != nil
	var prev2 int64
	if ok {
		if prevOffset, err2 := GetNumberAsInt64(prev); err2 == nil {
			prev2 = prevOffset
		} else {
			ok = false
		}
	}
	return checkFirstXrefPrev(ok, prev2, linInfo.EndOfFirstPage, parser.fileSize)
}

func (parser *PdfParser) parseFirstXrefTrailer() (*PdfObjectDictionary, error) {
	if parser.firstXrefOffset == 0 {
		return nil, errors.New("first xref location not recorded; linearization dict not parsed")
	}
	firstParser := &PdfParser{reader: parser.reader, fileSize: parser.fileSize, Opts: parser.Opts, decodedTotal: parser.decodedTotal}
	xrefs := parser.currentXrefs()
	m := make(map[int]XrefObject, len(xrefs.ObjectMap))
	maps.Copy(m, xrefs.ObjectMap)
	firstParser.setXrefs(XrefTable{ObjectMap: m})
	firstParser.objStreams = make(objectStreams)
	if parser.xrefType != nil {
		xrefType := *parser.xrefType
		firstParser.xrefType = &xrefType
	}
	cursor := firstParser.newCursor(parser.firstXrefOffset)
	return firstParser.parseXref(cursor, nil)
}

func checkMainXrefOffset(mainXrefOffset, endOfFirstPage, fileSize int64) *LinearizationViolation {
	if mainXrefOffset <= 0 || mainXrefOffset >= fileSize || mainXrefOffset <= endOfFirstPage {
		return &LinearizationViolation{Code: LinErrXRefOffsetMismatch, Severity: SeverityError, Message: fmt.Sprintf("/T (%d) does not point past the first-page section (/E %d) within the file (%d)", mainXrefOffset, endOfFirstPage, fileSize)}
	}
	return nil
}

var startxrefRegexp = regexp.MustCompile("startx?ref\\s*(\\d+)")

// FirstXrefTrailer returns the trailer dictionary of the first xref table
// in a linearized file (the one immediately after the linearization
// parameter dictionary). Returns nil if the file is not linearized, and an
// error if the first xref cannot be located or parsed.
//
// This is needed by PDF/A validators that must compare the /ID entry of
// the first xref's trailer against the main xref's trailer (ISO 19005-1
// rule 6.1.3-4).
func (parser *PdfParser) FirstXrefTrailer() (*PdfObjectDictionary, error) {
	if !parser.linearized {
		return nil, nil
	}
	parser.firstTrailerOnce.Do(func() { parser.firstTrailer, parser.firstTrailerErr = parser.parseFirstXrefTrailer() })
	return parser.firstTrailer, parser.firstTrailerErr
}
