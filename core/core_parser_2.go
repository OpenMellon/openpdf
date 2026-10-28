package core

import (
	"errors"
	"fmt"

	"github.com/OpenMellon/openpdf/common"
)

func (parser *PdfParser) parseHintStreamIfPresent() error {
	if parser.linearization == nil || len(parser.linearization.HintOffsets) == 0 {
		return nil
	}
	hintRange := parser.linearization.HintOffsets[0]
	if hintRange.Length <= 0 {
		return nil
	}
	hintStreamObject, err := parser.parseHintStreamObject(hintRange.Offset)
	if err != nil {
		return fmt.Errorf("primary hint stream: %w", err)
	}
	stream, err := DecodeStream(hintStreamObject)
	if err != nil {
		return fmt.Errorf("primary hint stream decode: %w", err)
	}
	if len(parser.linearization.HintOffsets) > 1 {
		secondRange := parser.linearization.HintOffsets[1]
		if secondRange.Length > 0 {
			secondStream, err2 := parser.parseHintStreamObject(secondRange.Offset)
			if err2 == nil {
				if data, err3 := DecodeStream(secondStream); err3 == nil {
					stream = append(stream, data...)
				} else {
					common.Log.Debug("WARN: secondary hint stream decode failed: %v", err3)
				}
			} else {
				common.Log.Debug("WARN: secondary hint stream parse failed: %v", err2)
			}
		}
	}
	hintTables, err := parseHintTables(stream, hintStreamObject.PdfObjectDictionary, parser.linearization.NumPages)
	if err != nil {
		return err
	}
	parser.linearization.HintTables = hintTables
	return nil
}

func (parser *PdfParser) parseHintStreamObject(offset int64) (*PdfObjectStream, error) {
	indirectObjectAt, err := parser.ParseIndirectObjectAt(offset)
	if err != nil {
		return nil, err
	}
	if stream, ok := indirectObjectAt.(*PdfObjectStream); ok {
		return stream, nil
	}
	if indirect, ok := indirectObjectAt.(*PdfIndirectObject); ok {
		if stream, isStream := indirect.PdfObject.(*PdfObjectStream); isStream {
			return stream, nil
		}
	}
	return nil, fmt.Errorf("hint object at offset %d is not a stream (%T)", offset, indirectObjectAt)
}

// LinearizationDictObjNum returns the object number of the linearization
// parameter dictionary (the first indirect object after the header), or 0 if
// the document does not begin with a linearization dictionary. Note this is
// populated whenever a leading linearization dictionary is found, even if
// IsLinearized() is false (e.g. it was superseded by a later revision).
func (parser *PdfParser) LinearizationDictObjNum() int { return parser.linearizationObjNum }

func (parser *PdfParser) validateFirstPageAndCount(linInfo *LinearizationInfo) []LinearizationViolation {
	var violations []LinearizationViolation
	leafCount, declaredCount, firstPageIndex, err := parser.walkPageTree()
	if err != nil {
		violations = append(violations, LinearizationViolation{Code: LinErrFirstPageObjectInvalid, Severity: SeverityError, Message: fmt.Sprintf("unable to walk the page tree: %v", err)})
		return violations
	}
	if int64(leafCount) != int64(linInfo.NumPages) {
		violations = append(violations, LinearizationViolation{Code: LinErrPageCountMismatch, Severity: SeverityWarning, Message: fmt.Sprintf("/N (%d) does not match the page count from the page tree (%d)", linInfo.NumPages, leafCount)})
	}
	if violation := checkPageCount(declaredCount, int64(leafCount)); violation != nil {
		violations = append(violations, *violation)
	}
	ok := false
	if byNumber, err2 := parser.LookupByNumber(int(linInfo.FirstPageObjNum)); err2 == nil {
		if dict, hasDict := GetDict(byNumber); hasDict {
			if typeName, hasTypeName := GetName(dict.Get("Type")); hasTypeName && string(*typeName) == "Page" {
				ok = true
			}
		}
	}
	if !ok {
		violations = append(violations, LinearizationViolation{Code: LinErrFirstPageObjectInvalid, Severity: SeverityError, Message: fmt.Sprintf("/O (%d) does not resolve to a /Type /Page object", linInfo.FirstPageObjNum)})
		return violations
	}
	if firstPageIndex >= 0 && linInfo.FirstPageObjNum != firstPageIndex {
		violations = append(violations, LinearizationViolation{Code: LinErrFirstPageObjectNotFirstPage, Severity: SeverityError, Message: fmt.Sprintf("/O (%d) is not the first page in the page tree (first page is object %d)", linInfo.FirstPageObjNum, firstPageIndex)})
	}
	if offset, ok2 := parser.objectFileOffset(int(linInfo.FirstPageObjNum)); ok2 {
		if linInfo.EndOfFirstPage > 0 && offset >= linInfo.EndOfFirstPage {
			violations = append(violations, LinearizationViolation{Code: LinErrFirstPageObjectOutsideSection, Severity: SeverityError, Message: fmt.Sprintf("first page object %d at offset %d is not within the first-page section (/E %d)", linInfo.FirstPageObjNum, offset, linInfo.EndOfFirstPage)})
		}
	}
	return violations
}

func (parser *PdfParser) validateLinearization() ([]LinearizationViolation, error) {
	if !parser.IsLinearized() {
		return []LinearizationViolation{{Code: LinErrNotLinearized, Severity: SeverityError, Message: "PDF is not linearized"}}, nil
	}
	var violations []LinearizationViolation
	linInfo := parser.LinearizationInfo()
	if violation := checkParamDictEnd(parser.firstXrefOffset); violation != nil {
		violations = append(violations, *violation)
	}
	if linInfo != nil && linInfo.FileLength != parser.fileSize {
		violations = append(violations, LinearizationViolation{Code: LinErrFileLengthMismatch, Severity: SeverityError, Message: fmt.Sprintf("/L (%d) does not match actual file size (%d)", linInfo.FileLength, parser.fileSize)})
	}
	if parser.linearizationDict != nil {
		if violation := checkLinearizedVersion(parser.linearizationDict); violation != nil {
			violations = append(violations, *violation)
		}
	}
	if linInfo != nil {
		if violation := checkFirstPageEndOffset(linInfo.EndOfFirstPage, parser.firstXrefOffset, linInfo.MainXrefOffset, parser.fileSize); violation != nil {
			violations = append(violations, *violation)
		}
		if violation := checkMainXrefOffset(linInfo.MainXrefOffset, linInfo.EndOfFirstPage, parser.fileSize); violation != nil {
			violations = append(violations, *violation)
		}
	}
	if linInfo != nil {
		violations = append(violations, parser.validateFirstPageAndCount(linInfo)...)
		violations = append(violations, parser.validateFirstPageSelfContained(linInfo)...)
	}
	if linInfo != nil {
		if len(linInfo.HintOffsets) > 0 && linInfo.HintTables == nil {
			_ = parser.parseHintStreamIfPresent()
		}
		violations = append(violations, checkHintStreams(linInfo, parser.fileSize)...)
	}
	if linInfo != nil {
		if violation := checkTrailingStartxref(parser.GetXrefOffset(), parser.firstXrefOffset, linInfo.EndOfFirstPage); violation != nil {
			violations = append(violations, *violation)
		}
		if violation := parser.checkFirstXrefPrevChain(linInfo); violation != nil {
			violations = append(violations, *violation)
		}
	}
	if violation := parser.checkLinearizedTrailerID(); violation != nil {
		violations = append(violations, *violation)
	}
	if parser.GetRevisionNumber() > 0 {
		violations = append(violations, LinearizationViolation{Code: LinErrUpdateBreaksLinearization, Severity: SeverityWarning, Message: "incremental update present after linearization; linearization is stale"})
	}
	return violations, nil
}

// ValidateLinearizationTrailerID runs only the linearization /ID consistency
// check (first-xref trailer vs main trailer), without the full Annex F battery
// (no page-tree walk or hint decoding). Returns nil if the PDF is not
// linearized or the /ID values match. Intended for callers — such as the PDF/A
// verifier — that only need the /ID rule and want to avoid the O(pages) cost of
// the full validator.
func (parser *PdfParser) ValidateLinearizationTrailerID() *LinearizationViolation {
	if !parser.IsLinearized() {
		return nil
	}
	return parser.checkLinearizedTrailerID()
}

func (parser *PdfParser) validateFirstPageSelfContained(linInfo *LinearizationInfo) []LinearizationViolation {
	var violations []LinearizationViolation
	byNumber, err := parser.LookupByNumber(int(linInfo.FirstPageObjNum))
	if err != nil {
		return nil
	}
	dict, ok := GetDict(byNumber)
	if !ok {
		return nil
	}
	for _, objNum := range collectReferencedObjNums(dict, firstPageSkipKeys...) {
		obj, err2 := parser.LookupByNumber(objNum)
		if err2 != nil || obj == nil || IsNullObject(obj) {
			violations = append(violations, LinearizationViolation{Code: LinErrFirstPageObjectUnresolved, Severity: SeverityError, Message: fmt.Sprintf("first page dependency object %d cannot be resolved", objNum)})
			continue
		}
		offset, ok2 := parser.objectFileOffset(objNum)
		if !ok2 {
			continue
		}
		if linInfo.EndOfFirstPage > 0 && offset >= linInfo.EndOfFirstPage {
			violations = append(violations, LinearizationViolation{Code: LinErrFirstPageObjectOutsideSection, Severity: SeverityError, Message: fmt.Sprintf("first page dependency object %d at offset %d is outside the first-page section (/E %d)", objNum, offset, linInfo.EndOfFirstPage)})
		}
	}
	return violations
}

// IsLinearized reports whether the PDF is a linearized ("fast web view") file
// as defined in ISO 32000-2 Annex F.
func (parser *PdfParser) IsLinearized() bool { return parser.linearized }

func (parser *PdfParser) buildLinearizationInfo(dict *PdfObjectDictionary) *LinearizationInfo {
	linInfo := &LinearizationInfo{FileLength: parser.linearizedFileLength}
	if oN, err := GetNumberAsInt64(dict.Get("O")); err == nil {
		linInfo.FirstPageObjNum = oN
	}
	if eN, err := GetNumberAsInt64(dict.Get("E")); err == nil {
		linInfo.EndOfFirstPage = eN
	}
	if nN, err := GetNumberAsInt64(dict.Get("N")); err == nil {
		if nN > 0 && nN <= maxLinearizationPages {
			linInfo.NumPages = int(nN)
		}
	}
	if tN, err := GetNumberAsInt64(dict.Get("T")); err == nil {
		linInfo.MainXrefOffset = tN
	}
	if pN, err := GetNumberAsInt64(dict.Get("P")); err == nil {
		linInfo.FirstPageNum = int(pN)
	}
	if hArr, ok := GetArray(dict.Get("H")); ok {
		linInfo.HintOffsets = parseHintRanges(hArr)
	}
	return linInfo
}

func (parser *PdfParser) walkPageTree() (pageCount int, declaredCount int64, firstPageIndex int64, err error) {
	firstPageIndex = -1
	trailer := parser.GetTrailer()
	if trailer == nil {
		return 0, 0, -1, errors.New("linearization: missing trailer")
	}
	obj, err := parser.Resolve(trailer.Get("Root"))
	if err != nil {
		return 0, 0, -1, err
	}
	dict, ok := GetDict(obj)
	if !ok {
		return 0, 0, -1, errors.New("linearization: /Root is not a dictionary")
	}
	pages := dict.Get("Pages")
	pagesObj, err := parser.Resolve(pages)
	if err != nil {
		return 0, 0, -1, err
	}
	pagesDict, ok := GetDict(pagesObj)
	if !ok {
		return 0, 0, -1, errors.New("linearization: /Pages is not a dictionary")
	}
	if count, err2 := GetNumberAsInt64(pagesDict.Get("Count")); err2 == nil {
		declaredCount = count
	}
	m := map[int]struct{}{}
	var walk func(node PdfObject) error
	walk = func(node PdfObject) error {
		objNum := int64(-1)
		if ref, isRef := node.(*PdfObjectReference); isRef {
			objNum = ref.ObjectNumber
			if _, ok2 := m[int(objNum)]; ok2 {
				return nil
			}
			m[int(objNum)] = struct{}{}
		}
		resolved, err2 := parser.Resolve(node)
		if err2 != nil {
			return err2
		}
		nodeDict, hasDict := GetDict(resolved)
		if !hasDict {
			return errors.New("linearization: page tree node is not a dictionary")
		}
		if typeName, hasTypeName := GetName(nodeDict.Get("Type")); hasTypeName && string(*typeName) == "Page" {
			pageCount++
			if firstPageIndex < 0 {
				firstPageIndex = objNum
			}
			return nil
		}
		kids, hasKids := GetArray(nodeDict.Get("Kids"))
		if !hasKids {
			kidsObj, err3 := parser.Resolve(nodeDict.Get("Kids"))
			if err3 != nil {
				return err3
			}
			if kids, hasKids = GetArray(kidsObj); !hasKids {
				return errors.New("linearization: page tree node missing /Kids")
			}
		}
		for _, elem := range kids.Elements() {
			if err3 := walk(elem); err3 != nil {
				return err3
			}
		}
		return nil
	}
	if err2 := walk(pages); err2 != nil {
		return pageCount, declaredCount, firstPageIndex, err2
	}
	return pageCount, declaredCount, firstPageIndex, nil
}

// ValidateLinearization runs the ISO 32000-2 Annex F checks against this
// already-parsed PDF. Callers that already hold a parser (e.g. a PdfReader)
// should use this rather than the package-level ValidateLinearization, which
// re-parses from scratch. Returns [LinErrNotLinearized] if the PDF is not
// linearized.
func (parser *PdfParser) ValidateLinearization() ([]LinearizationViolation, error) {
	return parser.validateLinearization()
}

func (parser *PdfParser) parseLinearizedDictionary() (*PdfObjectDictionary, error) {
	fileSize := parser.fileSize
	cursor := parser.newCursor(0)
	var pos int64
	var window int64 = 2048
	for pos < fileSize-4 {
		if fileSize <= (window + pos) {
			window = fileSize - pos
		}
		cursor.SetFileOffset(pos)
		data := make([]byte, window)
		_, err := cursor.Read(data)
		if err != nil {
			return nil, err
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Looking for first indirect object: \"%s\"", string(data))
		}
		allStringIndex := objHeaderRegexp.FindAllStringIndex(string(data), -1)
		if allStringIndex != nil {
			nums := allStringIndex[0]
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Ind: % d", allStringIndex)
			}
			cursor.SetFileOffset(pos + int64(nums[0]))
			indirectObject, err2 := parser.parseIndirectObject(cursor, map[int]struct{}{})
			if err2 != nil {
				return nil, nil
			}
			if indirect, ok := GetIndirect(indirectObject); ok {
				if dict, hasDict := GetDict(indirect.PdfObject); hasDict {
					if linearized := dict.Get("Linearized"); linearized != nil {
						parser.firstXrefOffset = cursor.GetFileOffset()
						parser.linearizationObjNum = int(indirect.ObjectNumber)
						return dict, nil
					}
					return nil, nil
				}
			}
			return nil, nil
		}
		pos += window - 4
	}
	return nil, errors.New("the first object not found")
}

func (parser *PdfParser) checkLinearizedInformation(dict *PdfObjectDictionary) (bool, error) {
	var err error
	parser.linearizedFileLength, err = GetNumberAsInt64(dict.Get("L"))
	if err != nil {
		return false, err
	}
	cursor := parser.newCursor(0)
	err = parser.seekToEOFMarker(cursor, parser.linearizedFileLength)
	switch err {
	case nil:
		return true, nil
	case errEOFNotFound:
		return false, nil
	default:
		return false, err
	}
}

// LinearizationInfo returns the populated LinearizationInfo for a linearized
// PDF, or nil for a non-linearized one. The returned value is shared state —
// callers should not mutate it.
func (parser *PdfParser) LinearizationInfo() *LinearizationInfo { return parser.linearization }

func (parser *PdfParser) checkLinearizedTrailerID() *LinearizationViolation {
	dict, err := parser.FirstXrefTrailer()
	if err != nil || dict == nil {
		return &LinearizationViolation{Code: LinErrTrailerReadFailure, Severity: SeverityError, Message: "unable to read the first xref trailer for /ID comparison"}
	}
	trailer := parser.GetTrailer()
	if trailer == nil {
		return &LinearizationViolation{Code: LinErrTrailerReadFailure, Severity: SeverityError, Message: "missing main trailer for /ID comparison"}
	}
	id := dict.Get("ID")
	obj := trailer.Get("ID")
	if id == nil || obj == nil {
		return &LinearizationViolation{Code: LinErrIDMismatch, Severity: SeverityError, Message: "/ID must be present in both the first xref trailer and the main trailer"}
	}
	if !equalIDObjects(id, obj) {
		return &LinearizationViolation{Code: LinErrIDMismatch, Severity: SeverityError, Message: "/ID in the first xref trailer differs from /ID in the main trailer"}
	}
	return nil
}

func (parser *PdfParser) traceStreamLength(obj PdfObject, visited map[int]struct{}) (PdfObject, error) {
	internal, err := parser.resolveInternal(obj, visited)
	if err != nil {
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Stream length? %s", internal)
	}
	return internal, nil
}

func (parser *PdfParser) resolveReference(ref *PdfObjectReference) (PdfObject, bool, error) {
	if cachedObject, ok := parser.LoadCachedObject(int(ref.ObjectNumber)); ok {
		return cachedObject, true, nil
	}
	byReference, err := parser.LookupByReference(*ref)
	if err != nil {
		return nil, false, err
	}
	parser.StoreCachedObject(int(ref.ObjectNumber), byReference)
	return byReference, false, nil
}

// LookupByReference looks up a PdfObject by a reference.
func (parser *PdfParser) LookupByReference(ref PdfObjectReference) (PdfObject, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Looking up reference %s", ref.String())
	}
	return parser.LookupByNumber(int(ref.ObjectNumber))
}
