package core

// ThumbnailHintTable corresponds to the optional thumbnail hint table
// (ISO 32000-2 Annex F.4.6). The reader retains the raw header so callers
// can use it; per-page entry decoding is best-effort.
type ThumbnailHintTable struct {
	FirstThumbnailObjNum        uint32
	FirstThumbnailOffset        uint32
	NumThumbnails               uint32
	NumSharedThumbnails         uint32
	BitsPerSharedRef            uint16
	BitsPerObjectCountDelta     uint16
	LeastObjectsInThumbnail     uint32
	BitsPerThumbnailLengthDelta uint16
	LeastThumbnailLength        uint32
}

func readThumbnailHintTable(br *bitReader) (*ThumbnailHintTable, error) {
	thumb := &ThumbnailHintTable{}
	sts := []struct {
		dest any
		bits uint
	}{{&thumb.FirstThumbnailObjNum, 32}, {&thumb.FirstThumbnailOffset, 32}, {&thumb.NumThumbnails, 32}, {&thumb.NumSharedThumbnails, 32}, {&thumb.BitsPerSharedRef, 16}, {&thumb.BitsPerObjectCountDelta, 16}, {&thumb.LeastObjectsInThumbnail, 32}, {&thumb.BitsPerThumbnailLengthDelta, 16}, {&thumb.LeastThumbnailLength, 32}}
	for _, st := range sts {
		bits, err := br.readBits(st.bits)
		if err != nil {
			return nil, err
		}
		switch v := st.dest.(type) {
		case *uint32:
			*v = bits
		case *uint16:
			*v = uint16(bits)
		}
	}
	return thumb, nil
}

// GenericHintTable is the common shape of the optional hint tables that all
// share the same four-field header: outline (F.4.7), article thread (F.4.8),
// named destination (F.4.9), interactive form (F.4.10), information dict
// (F.4.11), logical structure (F.4.12), page label (F.4.13), rendition
// (F.4.14) and embedded file stream (F.4.15). The v1 reader records the
// location header for each; per-entry decoding is producer-specific and not
// retained. If one of these tables ever needs fields beyond the common four,
// promote it back to a dedicated type.
type GenericHintTable struct {
	FirstObjNum uint32
	FirstOffset uint32
	NumObjects  uint32
	GroupLength uint32
}

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

// LinearizationViolationCode identifies a specific linearization check failure.
type LinearizationViolationCode int

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

// LinearizationViolation describes a single violation of the PDF linearization spec
// (ISO 32000-2 Annex F).
type LinearizationViolation struct {
	Code     LinearizationViolationCode
	Severity Severity
	Message  string
}
