package core

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/OpenMellon/openpdf/common"
	"golang.org/x/sync/singleflight"
)

// ParserOpts defines configuration options for Parser.
// These options allow setting customization flags to control parsing behaviors.
type ParserOpts struct {
	// RelaxedMode enables tolerant parsing by ignoring certain non-critical PDF errors.
	// When set to true, the parser will attempt to continue processing despite certain
	// structural errors such as:
	// - Missing or invalid /XRefStm entries
	// - Non-/Page or non-/Pages objects found in the page tree (/Kids array)
	RelaxedMode bool

	// AutoRepairXrefTable enables automatic repair of the xref table when it is found to be malformed or missing objects.
	// The check runs when the parser is constructed; for encrypted files it is
	// deferred to Decrypt, so a file that is never decrypted is never repaired.
	AutoRepairXrefTable bool

	// MaxDecodedStreamSize is the maximum number of bytes a single stream, or a single filter
	// stage of a filter chain, may decode to. 0 selects DefaultMaxDecodedStreamSize.
	// Decoding past the limit fails with ErrDecodedSizeLimitExceeded.
	// Not covered: decoders registered with RegisterCustomStreamEncoder, and inline images,
	// which have no stream object and use DefaultMaxDecodedStreamSize. JBIG2 is checked against
	// the page size its page information segment declares; a striped page that leaves its
	// height open is checked after decoding, bounded by the decoder's 256 MiB per-bitmap cap.
	MaxDecodedStreamSize int64

	// MaxDecodedTotalSize is the maximum number of bytes that stream decodes through the parser
	// and its revisions may produce together. Every Flate, LZW, RunLength, ASCIIHex, ASCII85,
	// CCITTFax, DCT and JBIG2 decode counts, including repeated decodes of the same stream, each stage of
	// a filter chain, and the object, cross-reference and hint streams the parser decodes itself.
	// Concurrent decodes each check the budget when they start, so together they can exceed it
	// by up to one MaxDecodedStreamSize each. 0 disables the limit.
	MaxDecodedTotalSize int64
}

func newCompliancePdfParser(r io.ReaderAt, fileSize int64, decodedTotal *atomic.Int64) (parser *PdfParser, err error) {
	parser = &PdfParser{decodedTotal: decodedTotal, reader: r, fileSize: fileSize, compliance: true, revisionParsers: make(map[*PdfParser]*PdfParser)}
	if err = parser.parseDetailedHeader(); err != nil {
		return nil, err
	}
	if parser.trailer, err = parser.loadXrefs(); err != nil {
		common.Log.Debug("ERROR: Failed to load xref table! %s", err)
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Trailer: %s", parser.trailer)
	}
	if len(parser.currentXrefs().ObjectMap) == 0 {
		return nil, fmt.Errorf("empty XREF table - Invalid")
	}
	linearizedDictionary, err := parser.parseLinearizedDictionary()
	if err != nil {
		return nil, err
	}
	if linearizedDictionary != nil {
		ok, err2 := parser.checkLinearizedInformation(linearizedDictionary)
		if err2 != nil {
			return nil, err2
		}
		if ok {
			parser.linearized = true
			parser.linearizationDict = linearizedDictionary
			parser.linearization = parser.buildLinearizationInfo(linearizedDictionary)
		}
	}
	parser.numRevisions = len(parser.xrefOffsets)
	if parser.linearized && parser.numRevisions != 0 {
		parser.numRevisions--
	}
	parser.revisions = make([]*PdfParser, parser.numRevisions)
	return parser, nil
}

// NewParserWithOpts creates a parser with the given options. The source's
// byte length is auto-detected. For exotic io.ReaderAt sources that can't be
// auto-sized, use NewParserWithOptsAt.
func NewParserWithOpts(r io.ReaderAt, opts ParserOpts) (*PdfParser, error) {
	size, err := SizeOfReaderAt(r)
	if err != nil {
		return nil, err
	}
	return NewParserWithOptsAt(r, size, opts)
}

// NewParserAt creates a parser with an explicit document size. Use this when
// the source's byte length cannot be auto-detected from its type — e.g. a
// custom io.ReaderAt that doesn't implement Size/Seek/Stat.
func NewParserAt(r io.ReaderAt, size int64) (*PdfParser, error) {
	return newPdfParserAt(r, size, nil, new(atomic.Int64))
}

// PdfParser parses a PDF file and provides access to the object structure of the PDF.
type PdfParser struct {
	version              Version
	reader               io.ReaderAt
	fileSize             int64
	xrefs                atomic.Pointer[XrefTable]
	xrefOffset           int64
	xrefType             *xrefType
	objStreams           objectStreams
	trailer              *PdfObjectDictionary
	crypter              *PdfCrypt
	encryptObj           *PdfIndirectObject
	repairOnce           sync.Once
	repair               repairResult
	repairedXrefs        atomic.Pointer[XrefTable]
	pendingXrefRepair    bool
	objCache             sync.Map
	lookupGroup          singleflight.Group
	objStreamMu          sync.Mutex
	metadata             ParserMetadata
	compliance           bool
	xrefOffsets          []int64
	numRevisions         int
	startxrefOffset      int64
	xrefSectionEnds      []int64
	linearized           bool
	linearizedFileLength int64
	linearizationDict    *PdfObjectDictionary
	linearization        *LinearizationInfo
	linearizationObjNum  int
	firstTrailerOnce     sync.Once
	firstTrailer         *PdfObjectDictionary
	firstTrailerErr      error
	firstXrefOffset      int64
	revisionParsers      map[*PdfParser]*PdfParser
	revisionEndsOnce     sync.Once
	revisionEnds         []int64
	revisionEndsErr      error
	revisionEndsValid    bool
	revisions            []*PdfParser

	// Opts holds different parsing options.
	Opts         *ParserOpts
	decodedTotal *atomic.Int64
}

// NewParser creates a new parser for a PDF source. The source's byte length
// is auto-detected via Size() / Seek() / Stat() — *os.File, *bytes.Reader,
// and *io.SectionReader all work. For exotic sources that can't be
// auto-sized, use NewParserAt.
func NewParser(r io.ReaderAt) (*PdfParser, error) {
	size, err := SizeOfReaderAt(r)
	if err != nil {
		return nil, err
	}
	return NewParserAt(r, size)
}

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

// NewParserWithOptsAt creates a parser with explicit size and options.
func NewParserWithOptsAt(r io.ReaderAt, size int64, opts ParserOpts) (*PdfParser, error) {
	return newPdfParserAt(r, size, &opts, new(atomic.Int64))
}

// NewParserFromString is used for testing purposes.
func NewParserFromString(txt string) *PdfParser {
	reader := bytes.NewReader([]byte(txt))
	parser := &PdfParser{decodedTotal: new(atomic.Int64), reader: reader, fileSize: int64(len(txt)), revisionParsers: make(map[*PdfParser]*PdfParser)}
	parser.setXrefs(XrefTable{ObjectMap: map[int]XrefObject{}})
	return parser
}

// NewCompliancePdfParser creates a PdfParser that extracts additional metadata
// for standards compliance checks (PDF/A, PDF/X). The source's byte length is
// auto-detected. For exotic io.ReaderAt sources that can't be auto-sized, use
// NewCompliancePdfParserAt.
func NewCompliancePdfParser(r io.ReaderAt) (*PdfParser, error) {
	size, err := SizeOfReaderAt(r)
	if err != nil {
		return nil, err
	}
	return NewCompliancePdfParserAt(r, size)
}

// NewCompliancePdfParserAt is the explicit-size form of NewCompliancePdfParser.
func NewCompliancePdfParserAt(r io.ReaderAt, size int64) (parser *PdfParser, err error) {
	return newCompliancePdfParser(r, size, new(atomic.Int64))
}

func newPdfParserAt(r io.ReaderAt, size int64, opts *ParserOpts, decodedTotal *atomic.Int64) (*PdfParser, error) {
	parser := &PdfParser{decodedTotal: decodedTotal, reader: r, fileSize: size, xrefOffsets: make([]int64, 0), revisionParsers: make(map[*PdfParser]*PdfParser), Opts: opts}
	if err := parser.initParser(); err != nil {
		return nil, err
	}
	return parser, nil
}
