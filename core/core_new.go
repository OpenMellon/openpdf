package core

import (
	"bytes"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/OpenMellon/openpdf/common"
)

// NewEncoderFromStream creates a StreamEncoder based on the stream's dictionary.
func NewEncoderFromStream(streamObj *PdfObjectStream) (StreamEncoder, error) {
	toDirectObject := TraceToDirectObject(streamObj.Get("Filter"))
	if toDirectObject == nil {
		return NewRawEncoder(), nil
	}
	if _, ok := toDirectObject.(*PdfObjectNull); ok {
		return NewRawEncoder(), nil
	}
	name, ok := toDirectObject.(*PdfObjectName)
	if !ok {
		arr, isArr := toDirectObject.(*PdfObjectArray)
		if !isArr {
			return nil, fmt.Errorf("filter not a Name or Array object")
		}
		if arr.Len() == 0 {
			return NewRawEncoder(), nil
		}
		if arr.Len() != 1 {
			multiEncoderFromStream, err := newMultiEncoderFromStream(streamObj)
			if err != nil {
				common.Log.Error("Failed creating multi encoder: %v", err)
				return nil, err
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Multi enc: %s\n", multiEncoderFromStream)
			}
			return multiEncoderFromStream, nil
		}
		toDirectObject = arr.Get(0)
		name, isArr = toDirectObject.(*PdfObjectName)
		if !isArr {
			return nil, fmt.Errorf("filter array member not a Name object")
		}
	}
	if encoder, ok2 := customStreamEncoders.Load(name.String()); ok2 {
		return encoder.(StreamEncoder), nil
	}
	var encoder StreamEncoder
	var err error
	switch *name {
	case StreamEncodingFilterNameFlate:
		encoder, err = newFlateEncoderFromStream(streamObj, nil)
	case StreamEncodingFilterNameLZW:
		encoder, err = newLZWEncoderFromStream(streamObj, nil)
	case StreamEncodingFilterNameDCT:
		encoder, err = newDCTEncoderFromStream(streamObj, nil)
	case StreamEncodingFilterNameRunLength:
		encoder, err = newRunLengthEncoderFromStream(streamObj, nil)
	case StreamEncodingFilterNameASCIIHex:
		encoder = NewASCIIHexEncoder()
	case StreamEncodingFilterNameASCII85, "A85":
		encoder = NewASCII85Encoder()
	case StreamEncodingFilterNameCCITTFax:
		encoder, err = newCCITTFaxEncoderFromStream(streamObj, nil)
	case StreamEncodingFilterNameJBIG2:
		encoder, err = newJBIG2EncoderFromStream(streamObj, nil)
	case StreamEncodingFilterNameJPX:
		encoder = NewJPXEncoder()
	default:
		common.Log.Debug("ERROR: Unsupported encoding method!")
		return nil, fmt.Errorf("unsupported encoding method (%s)", *name)
	}
	if err != nil {
		return nil, err
	}
	applyDecodeLimits(encoder, decodeLimitsFromStream(streamObj))
	return encoder, nil
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
