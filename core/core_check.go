package core

import (
	"errors"
	"fmt"
	"math"
)

func checkParamDictEnd(firstXrefOffset int64) *LinearizationViolation {
	if firstXrefOffset <= 0 || firstXrefOffset > maxLinearizationParamDictEnd {
		return &LinearizationViolation{Code: LinErrParamDictNotFirstObject, Severity: SeverityError, Message: fmt.Sprintf("linearization parameter dictionary does not fit within the first %d bytes (ends at %d)", maxLinearizationParamDictEnd, firstXrefOffset)}
	}
	return nil
}

func checkPredictorDimensions(columns, colors int) error {
	if columns < 0 || colors < 0 || int64(columns)*int64(colors) > math.MaxInt32 {
		return fmt.Errorf("%w: predictor Columns %d Colors %d", ErrRangeError, columns, colors)
	}
	return nil
}

func checkSliceBounds(length, low, high int) error {
	if low < 0 || low > length {
		return errors.New("slice index a out of bounds")
	}
	if high < low {
		return errors.New("invalid slice index b < a")
	}
	if high > length {
		return errors.New("slice index b out of bounds")
	}
	return nil
}

func checkHintStreams(linInfo *LinearizationInfo, fileSize int64) []LinearizationViolation {
	var violations []LinearizationViolation
	if len(linInfo.HintOffsets) == 0 {
		violations = append(violations, LinearizationViolation{Code: LinErrHintStreamMissing, Severity: SeverityWarning, Message: "/H is absent or malformed in the linearization parameter dictionary"})
		return violations
	}
	if linInfo.HintTables == nil {
		violations = append(violations, LinearizationViolation{Code: LinErrHintStreamMissing, Severity: SeverityWarning, Message: "primary hint stream could not be decoded"})
		return violations
	}
	if perPage := len(linInfo.HintTables.PageOffset.PerPage); int64(perPage) != int64(linInfo.NumPages) {
		violations = append(violations, LinearizationViolation{Code: LinErrHintPageCountMismatch, Severity: SeverityError, Message: fmt.Sprintf("page offset hint table has %d per-page entries but /N is %d", perPage, linInfo.NumPages)})
	}
	hintRange := linInfo.HintOffsets[0]
	if hintRange.Offset <= 0 || hintRange.Offset >= fileSize || (linInfo.EndOfFirstPage > 0 && hintRange.Offset >= linInfo.EndOfFirstPage) {
		violations = append(violations, LinearizationViolation{Code: LinErrHintStreamMisplaced, Severity: SeverityWarning, Message: fmt.Sprintf("primary hint stream offset (%d) is not within the first-page section (/E %d, size %d)", hintRange.Offset, linInfo.EndOfFirstPage, fileSize)})
	}
	if hintRange.Length <= 0 || hintRange.Offset < 0 || hintRange.Length > fileSize-hintRange.Offset {
		violations = append(violations, LinearizationViolation{Code: LinErrHintStreamLengthMismatch, Severity: SeverityWarning, Message: fmt.Sprintf("primary hint stream region (offset %d, length %d) exceeds the file size (%d)", hintRange.Offset, hintRange.Length, fileSize)})
	}
	if len(linInfo.HintOffsets) >= 2 {
		overflow := linInfo.HintOffsets[1]
		if overflow.Offset <= 0 || overflow.Length <= 0 || overflow.Offset >= fileSize || overflow.Length > fileSize-overflow.Offset {
			violations = append(violations, LinearizationViolation{Code: LinErrOverflowHintStreamInvalid, Severity: SeverityWarning, Message: fmt.Sprintf("overflow hint stream region (offset %d, length %d) is invalid for file size %d", overflow.Offset, overflow.Length, fileSize)})
		}
	}
	return violations
}

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

func checkFirstPageEndOffset(endOfFirstPage, firstXrefOffset, mainXrefOffset, fileSize int64) *LinearizationViolation {
	if endOfFirstPage <= firstXrefOffset || endOfFirstPage >= fileSize || (mainXrefOffset > 0 && endOfFirstPage >= mainXrefOffset) {
		return &LinearizationViolation{Code: LinErrFirstPageEndOffset, Severity: SeverityError, Message: fmt.Sprintf("/E (%d) is inconsistent with the file layout (lin dict ends %d, /T %d, size %d)", endOfFirstPage, firstXrefOffset, mainXrefOffset, fileSize)}
	}
	return nil
}

func checkTrailingStartxref(startxref, endOfFirstPage, fileSize int64) *LinearizationViolation {
	if startxref <= endOfFirstPage || (fileSize > 0 && startxref >= fileSize) {
		return &LinearizationViolation{Code: LinErrTrailingStartxrefMismatch, Severity: SeverityError, Message: fmt.Sprintf("trailing startxref (%d) does not point to the first-page xref (after lin dict %d, before /E %d)", startxref, endOfFirstPage, fileSize)}
	}
	return nil
}

func checkFirstXrefPrev(present bool, prevOffset, endOfFirstPage, fileSize int64) *LinearizationViolation {
	ok := !present || prevOffset <= 0 || (endOfFirstPage > 0 && prevOffset < endOfFirstPage) || (fileSize > 0 && prevOffset >= fileSize)
	if ok {
		return &LinearizationViolation{Code: LinErrPrevChainBroken, Severity: SeverityError, Message: fmt.Sprintf("first-xref /Prev (present=%v, offset=%d) does not point past the first-page section into the file body (/E %d, size %d)", present, prevOffset, endOfFirstPage, fileSize)}
	}
	return nil
}

func checkMainXrefOffset(mainXrefOffset, endOfFirstPage, fileSize int64) *LinearizationViolation {
	if mainXrefOffset <= 0 || mainXrefOffset >= fileSize || mainXrefOffset <= endOfFirstPage {
		return &LinearizationViolation{Code: LinErrXRefOffsetMismatch, Severity: SeverityError, Message: fmt.Sprintf("/T (%d) does not point past the first-page section (/E %d) within the file (%d)", mainXrefOffset, endOfFirstPage, fileSize)}
	}
	return nil
}
