package core

import (
	"errors"
	"fmt"
	"io"
	"slices"
)

// HasOddLengthHexStrings checks if the document has odd length hexadecimal strings.
func (meta ParserMetadata) HasOddLengthHexStrings() bool { return meta.oddLengthHexStrings }

// ParserMetadata is the parser based metadata information about document.
// The data here could be used on document verification.
type ParserMetadata struct {
	headerPosition             int
	eolAfterHeader             bool
	headerCommentBytes         [4]byte
	dataAfterEOF               bool
	oddLengthHexStrings        bool
	invalidHexRunes            bool
	nonConformantStream        bool
	invalidSubsectionHeader    bool
	invalidSeparationAfterXRef bool
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

// HasInvalidSubsectionHeader implements core.ParserMetadata interface.
func (meta ParserMetadata) HasInvalidSubsectionHeader() bool { return meta.invalidSubsectionHeader }

// HasInvalidSeparationAfterXRef implements core.ParserMetadata interface.
func (meta ParserMetadata) HasInvalidSeparationAfterXRef() bool {
	return meta.invalidSeparationAfterXRef
}

// HeaderCommentBytes gets the header comment bytes.
func (meta ParserMetadata) HeaderCommentBytes() [4]byte { return meta.headerCommentBytes }

// HasEOLAfterHeader gets information if there is a EOL after the version header.
func (meta ParserMetadata) HasEOLAfterHeader() bool { return meta.eolAfterHeader }

// HasDataAfterEOF checks if there is some data after EOF marker.
func (meta ParserMetadata) HasDataAfterEOF() bool { return meta.dataAfterEOF }

// HasNonConformantStream implements core.ParserMetadata.
func (meta ParserMetadata) HasNonConformantStream() bool { return meta.nonConformantStream }

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

// HasInvalidHexRunes implements core.ParserMetadata interface.
func (meta ParserMetadata) HasInvalidHexRunes() bool { return meta.invalidHexRunes }

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

// HeaderPosition gets the file header position.
func (meta ParserMetadata) HeaderPosition() int { return meta.headerPosition }

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
