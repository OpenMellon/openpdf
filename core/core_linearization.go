package core

import (
	"errors"
	"fmt"
	"io"

	"github.com/OpenMellon/openpdf/common"
)

// LinearizationDictObjNum returns the object number of the linearization
// parameter dictionary (the first indirect object after the header), or 0 if
// the document does not begin with a linearization dictionary. Note this is
// populated whenever a leading linearization dictionary is found, even if
// IsLinearized() is false (e.g. it was superseded by a later revision).
func (parser *PdfParser) LinearizationDictObjNum() int { return parser.linearizationObjNum }

// LinearizationInfo holds the values from the linearization parameter
// dictionary (ISO 32000-2 Annex F, Table F.1) together with any decoded hint
// tables.
//
// Field correspondence:
//
//	FileLength       -> /L  (length of the entire file in bytes)
//	HintOffsets      -> /H  ([primary_offset primary_length] or [primary_offset primary_length secondary_offset secondary_length])
//	FirstPageObjNum  -> /O  (object number of the first page's page object)
//	EndOfFirstPage   -> /E  (offset of the end of the first page)
//	NumPages         -> /N  (number of pages in the document)
//	MainXrefOffset   -> /T  (offset of the first entry in the main cross-reference table)
//	FirstPageNum     -> /P  (page number of the first page; default 0)
type LinearizationInfo struct {
	FileLength      int64
	HintOffsets     []HintRange
	FirstPageObjNum int64
	EndOfFirstPage  int64
	NumPages        int
	MainXrefOffset  int64
	FirstPageNum    int
	HintTables      *HintTables
}

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

const maxLinearizationParamDictEnd = 1024

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

var firstPageSkipKeys = []string{"Parent", "Thumb", "B"}

// Severity classifies a linearization violation as a hard error or a warning.
// The ISO 32000-2 Annex F checks differ in how strictly they bear on whether a
// PDF works in Fast Web View: some violations break it outright (Error), others
// only degrade or signal staleness (Warning).
type Severity int

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

// LinearizationViolationCode identifies a specific linearization check failure.
type LinearizationViolationCode int

func checkPageCount(count, treeCount int64) *LinearizationViolation {
	if count > 0 && count != treeCount {
		return &LinearizationViolation{Code: LinErrPageCountTreeMismatch, Severity: SeverityWarning, Message: fmt.Sprintf("/Pages /Count (%d) does not match the walked leaf page count (%d)", count, treeCount)}
	}
	return nil
}

func checkLinearizedVersion(linearizationDict *PdfObjectDictionary) *LinearizationViolation {
	linearized := linearizationDict.Get("Linearized")
	if linearized == nil {
		return nil
	}
	if _, err := GetNumberAsFloat(linearized); err != nil {
		return &LinearizationViolation{Code: LinErrInvalidVersion, Severity: SeverityWarning, Message: fmt.Sprintf("/Linearized value is not a number: %s", linearized.String())}
	}
	return nil
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

const (
	SeverityError Severity = iota
	SeverityWarning
)

// ValidateLinearization checks whether r contains a correctly linearized PDF as
// defined in ISO 32000-2 Annex F. It returns a slice of violations; an empty
// slice means the PDF passes all checks. If the PDF is not linearized it returns
// exactly [LinErrNotLinearized].
//
// The returned error is non-nil only when the file cannot be parsed at all and
// validation could not run; spec violations are reported through the slice.
func ValidateLinearization(r io.ReaderAt, size int64) ([]LinearizationViolation, error) {
	parserAt, err := NewParserAt(r, size)
	if err != nil {
		return nil, err
	}
	return parserAt.validateLinearization()
}

func checkFirstPageEndOffset(endOfFirstPage, firstXrefOffset, mainXrefOffset, fileSize int64) *LinearizationViolation {
	if endOfFirstPage <= firstXrefOffset || endOfFirstPage >= fileSize || (mainXrefOffset > 0 && endOfFirstPage >= mainXrefOffset) {
		return &LinearizationViolation{Code: LinErrFirstPageEndOffset, Severity: SeverityError, Message: fmt.Sprintf("/E (%d) is inconsistent with the file layout (lin dict ends %d, /T %d, size %d)", endOfFirstPage, firstXrefOffset, mainXrefOffset, fileSize)}
	}
	return nil
}

// ValidateLinearization runs the ISO 32000-2 Annex F checks against this
// already-parsed PDF. Callers that already hold a parser (e.g. a PdfReader)
// should use this rather than the package-level ValidateLinearization, which
// re-parses from scratch. Returns [LinErrNotLinearized] if the PDF is not
// linearized.
func (parser *PdfParser) ValidateLinearization() ([]LinearizationViolation, error) {
	return parser.validateLinearization()
}

func collectReferencedObjNums(dict *PdfObjectDictionary, keys ...string) []int {
	m := map[string]struct{}{}
	for _, key := range keys {
		m[key] = struct{}{}
	}
	seen := map[int]struct{}{}
	var nums []int
	var visit func(obj PdfObject)
	visit = func(obj PdfObject) {
		switch v := obj.(type) {
		case *PdfObjectReference:
			objNum := int(v.ObjectNumber)
			if _, ok := seen[objNum]; !ok {
				seen[objNum] = struct{}{}
				nums = append(nums, objNum)
			}
		case *PdfObjectArray:
			for _, elem := range v.Elements() {
				visit(elem)
			}
		case *PdfObjectDictionary:
			for _, key := range v.Keys() {
				visit(v.Get(key))
			}
		}
	}
	for _, key := range dict.Keys() {
		if _, ok := m[string(key)]; ok {
			continue
		}
		visit(dict.Get(key))
	}
	return nums
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

const (
	maxLinearizationPages   = 1 << 20
	maxSharedObjectsTotal   = 1 << 20
	maxSharedObjectsPerPage = 1 << 16
)

func checkTrailingStartxref(startxref, endOfFirstPage, fileSize int64) *LinearizationViolation {
	if startxref <= endOfFirstPage || (fileSize > 0 && startxref >= fileSize) {
		return &LinearizationViolation{Code: LinErrTrailingStartxrefMismatch, Severity: SeverityError, Message: fmt.Sprintf("trailing startxref (%d) does not point to the first-page xref (after lin dict %d, before /E %d)", startxref, endOfFirstPage, fileSize)}
	}
	return nil
}

// LinearizationInfo returns the populated LinearizationInfo for a linearized
// PDF, or nil for a non-linearized one. The returned value is shared state —
// callers should not mutate it.
func (parser *PdfParser) LinearizationInfo() *LinearizationInfo { return parser.linearization }

const (
	_ LinearizationViolationCode = iota
	LinErrNotLinearized
	LinErrParamDictNotFirstObject
	LinErrFileLengthMismatch
	LinErrFirstPageObjectInvalid
	LinErrFirstPageEndOffset
	LinErrPageCountMismatch
	LinErrHintStreamMissing
	LinErrXRefOffsetMismatch
	LinErrIDMismatch
	LinErrUpdateBreaksLinearization
	LinErrTrailingStartxrefMismatch
	LinErrHintPageCountMismatch
	LinErrFirstPageObjectNotFirstPage
	LinErrFirstPageObjectOutsideSection
	LinErrPageCountTreeMismatch
	LinErrHintStreamLengthMismatch
	LinErrHintStreamMisplaced
	LinErrOverflowHintStreamInvalid
	LinErrPrevChainBroken
	LinErrInvalidVersion
	LinErrFirstPageObjectUnresolved
	LinErrTrailerReadFailure
)

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

// LinearizationViolation describes a single violation of the PDF linearization spec
// (ISO 32000-2 Annex F).
type LinearizationViolation struct {
	Code     LinearizationViolationCode
	Severity Severity
	Message  string
}
