// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package segments

import (
	"fmt"
	"image"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	arithmeticdecoder "github.com/OpenMellon/openpdf/internal/jbig2/decoder/arithmetic"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder/huffman"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

func (sd *SymbolDictionary) retrieveImportSymbols() error {
	for _, rtSegment := range sd.Header.RTSegments {
		if rtSegment.Type == 0 {
			segmentData, err := rtSegment.GetSegmentData()
			if err != nil {
				return err
			}
			importedSD, ok := segmentData.(*SymbolDictionary)
			if !ok {
				return fmt.Errorf("provided Segment Data is not a SymbolDictionary Segment: %T", segmentData)
			}
			dictionary, err := importedSD.GetDictionary()
			if err != nil {
				return fmt.Errorf("related segment with index: %d getDictionary failed. %s", rtSegment.SegmentNumber, err.Error())
			}
			sd.importedSymbols = append(sd.importedSymbols, dictionary...)
			sd.numberOfImportedSymbols += importedSD.NumberOfExportedSymbols
		}
	}
	return nil
}

func (sd *SymbolDictionary) Encode(w bitwise.BinaryWriter) (n int, err error) {
	const processName = "SymbolDictionary.Encode"
	if sd == nil {
		return 0, jbig2errors.Error(processName, "symbol dictionary not defined")
	}
	if n, err = sd.encodeFlags(w); err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	encodeATFlags, err := sd.encodeATFlags(w)
	if err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	n += encodeATFlags
	if encodeATFlags, err = sd.encodeRefinementATFlags(w); err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	n += encodeATFlags
	if encodeATFlags, err = sd.encodeNumSyms(w); err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	n += encodeATFlags
	if encodeATFlags, err = sd.encodeSymbols(w); err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	n += encodeATFlags
	return n, nil
}

func (rs *RegionSegment) parseHeader() error {
	const processName = "parseHeader"
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("[REGION][PARSE-HEADER] Begin")
	}
	defer func() {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("[REGION][PARSE-HEADER] Finished")
		}
	}()
	v, err := rs.reader.ReadBits(32)
	if err != nil {
		return jbig2errors.Wrap(err, processName, "width")
	}
	rs.BitmapWidth = uint32(v & math.MaxUint32)
	v, err = rs.reader.ReadBits(32)
	if err != nil {
		return jbig2errors.Wrap(err, processName, "height")
	}
	rs.BitmapHeight = uint32(v & math.MaxUint32)
	v, err = rs.reader.ReadBits(32)
	if err != nil {
		return jbig2errors.Wrap(err, processName, "x location")
	}
	rs.XLocation = uint32(v & math.MaxUint32)
	v, err = rs.reader.ReadBits(32)
	if err != nil {
		return jbig2errors.Wrap(err, processName, "y location")
	}
	rs.YLocation = uint32(v & math.MaxUint32)
	if _, err = rs.reader.ReadBits(5); err != nil {
		return jbig2errors.Wrap(err, processName, "diry read")
	}
	if err = rs.readCombinationOperator(); err != nil {
		return jbig2errors.Wrap(err, processName, "combination operator")
	}
	return nil
}

func (pd *PatternDictionary) setGbAtPixels() {
	if pd.HDTemplate == 0 {
		pd.GBAtX = make([]int8, 4)
		pd.GBAtY = make([]int8, 4)
		pd.GBAtX[0] = -int8(pd.HdpWidth)
		pd.GBAtY[0] = 0
		pd.GBAtX[1] = -3
		pd.GBAtY[1] = -1
		pd.GBAtX[2] = 2
		pd.GBAtY[2] = -2
		pd.GBAtX[3] = -2
		pd.GBAtY[3] = -2
	} else {
		pd.GBAtX = []int8{-int8(pd.HdpWidth)}
		pd.GBAtY = []int8{0}
	}
}

func (sd *SymbolDictionary) decodeHeightClassBitmap(bm *bitmap.Bitmap, hcFirstSymbol int64, hcHeight int, widths []int) error {
	for i := hcFirstSymbol; i < int64(sd.numberOfDecodedSymbols); i++ {
		var xOffset int
		for j := hcFirstSymbol; j <= i-1; j++ {
			xOffset += widths[j]
		}
		rect := image.Rect(xOffset, 0, xOffset+widths[i], hcHeight)
		symbol, err := bitmap.Extract(rect, bm)
		if err != nil {
			return err
		}
		sd.newSymbols[i] = symbol
		sd.allSymbols = append(sd.allSymbols, symbol)
	}
	return nil
}

func newGenericRefinementRegion(reader *bitwise.Reader, header *Header) *GenericRefinementRegion {
	return &GenericRefinementRegion{reader: reader, RegionInfo: NewRegionSegment(reader), header: header, refTemplate0: &template0{}, refTemplate1: &template1{}}
}

func (sd *SymbolDictionary) getSbSymCodeLen() int8 {
	codeLen := int8(math.Ceil(math.Log(float64(sd.numberOfImportedSymbols+sd.NumberOfNewSymbols)) / math.Log(2)))
	if sd.IsHuffmanEncoded && codeLen < 1 {
		return 1
	}
	return codeLen
}

func (sd *SymbolDictionary) readRegionFlags() error {
	var (
		bits uint64
		bit  int
	)
	_, err := sd.reader.ReadBits(3)
	if err != nil {
		return err
	}
	bit, err = sd.reader.ReadBit()
	if err != nil {
		return err
	}
	sd.SdrTemplate = int8(bit)
	bits, err = sd.reader.ReadBits(2)
	if err != nil {
		return err
	}
	sd.SdTemplate = int8(bits & 0xf)
	bit, err = sd.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		sd.isCodingContextRetained = true
	}
	bit, err = sd.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		sd.isCodingContextUsed = true
	}
	bit, err = sd.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		sd.SdHuffAggInstanceSelection = true
	}
	bit, err = sd.reader.ReadBit()
	if err != nil {
		return err
	}
	sd.SdHuffBMSizeSelection = int8(bit)
	bits, err = sd.reader.ReadBits(2)
	if err != nil {
		return err
	}
	sd.SdHuffDecodeWidthSelection = int8(bits & 0xf)
	bits, err = sd.reader.ReadBits(2)
	if err != nil {
		return err
	}
	sd.SdHuffDecodeHeightSelection = int8(bits & 0xf)
	bit, err = sd.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		sd.UseRefinementAggregation = true
	}
	bit, err = sd.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		sd.IsHuffmanEncoded = true
	}
	return nil
}

func (gr *GenericRegion) Init(h *Header, r *bitwise.Reader) error {
	gr.RegionSegment = NewRegionSegment(r)
	gr.reader = r
	return gr.parseHeader()
}

func (hr *HalftoneRegion) computeSegmentDataStructure() error {
	hr.DataOffset = hr.reader.AbsolutePosition()
	hr.DataHeaderLength = hr.DataOffset - hr.DataHeaderOffset
	hr.DataLength = int64(hr.reader.AbsoluteLength()) - hr.DataHeaderLength
	return nil
}

func (pd *PatternDictionary) Init(h *Header, r *bitwise.Reader) error {
	pd.reader = r
	return pd.parseHeader()
}

type SymbolDictionary struct {
	reader                      *bitwise.Reader
	SdrTemplate                 int8
	SdTemplate                  int8
	isCodingContextRetained     bool
	isCodingContextUsed         bool
	SdHuffAggInstanceSelection  bool
	SdHuffBMSizeSelection       int8
	SdHuffDecodeWidthSelection  int8
	SdHuffDecodeHeightSelection int8
	UseRefinementAggregation    bool
	IsHuffmanEncoded            bool
	SdATX                       []int8
	SdATY                       []int8
	SdrATX                      []int8
	SdrATY                      []int8
	NumberOfExportedSymbols     uint32
	NumberOfNewSymbols          uint32
	Header                      *Header
	numberOfImportedSymbols     uint32
	importedSymbols             []*bitmap.Bitmap
	numberOfDecodedSymbols      uint32
	newSymbols                  []*bitmap.Bitmap
	huffDH                      huffman.Tabler
	huffDW                      huffman.Tabler
	huffBMSize                  huffman.Tabler
	huffAggInst                 huffman.Tabler
	exportedSymbols             []*bitmap.Bitmap
	allSymbols                  []*bitmap.Bitmap
	arithDecoder                *arithmeticdecoder.Decoder
	textRegion                  *TextRegion
	genericRegion               *GenericRegion
	refinementRegion            *GenericRefinementRegion
	cx                          *arithmeticdecoder.DecoderStats
	cxIADH                      *arithmeticdecoder.DecoderStats
	cxIADW                      *arithmeticdecoder.DecoderStats
	cxIAAI                      *arithmeticdecoder.DecoderStats
	cxIAEX                      *arithmeticdecoder.DecoderStats
	cxIARDX                     *arithmeticdecoder.DecoderStats
	cxIARDY                     *arithmeticdecoder.DecoderStats
	cxIADT                      *arithmeticdecoder.DecoderStats
	cxIAID                      *arithmeticdecoder.DecoderStats
	symbolCodeLength            int8
	symbols                     *bitmap.Bitmaps
	symbolList                  []int
	symbolMap                   map[int]int
	unborderSymbols             bool
}

func (sd *SymbolDictionary) addSymbol(r Regioner) error {
	regionBitmap, err := r.GetRegionBitmap()
	if err != nil {
		return err
	}
	sd.newSymbols[sd.numberOfDecodedSymbols] = regionBitmap
	sd.allSymbols = append(sd.allSymbols, regionBitmap)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("[SYMBOL DICTIONARY] Added symbol: %s", regionBitmap)
	}
	return nil
}

func (h *Header) readReferredToSegmentNumbers(r *bitwise.Reader, count int) ([]int, error) {
	const processName = "readReferredToSegmentNumbers"
	numbers := make([]int, count)
	if count > 0 {
		h.RTSegments = make([]*Header, count)
		var (
			number uint64
			err    error
		)
		for i := 0; i < count; i++ {
			number, err = r.ReadBits(byte(h.referenceSize()) << 3)
			if err != nil {
				return nil, jbig2errors.Wrapf(err, processName, "'%d' referred segment number", i)
			}
			numbers[i] = int(number & math.MaxInt32)
		}
	}
	return numbers, nil
}
