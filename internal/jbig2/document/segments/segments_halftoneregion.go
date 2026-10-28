// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package segments

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	arithmeticdecoder "github.com/OpenMellon/openpdf/internal/jbig2/decoder/arithmetic"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder/huffman"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder/mmr"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

type HalftoneRegion struct {
	reader               *bitwise.Reader
	header               *Header
	DataHeaderOffset     int64
	DataHeaderLength     int64
	DataOffset           int64
	DataLength           int64
	RegionSegment        *RegionSegment
	HDefaultPixel        int8
	CombinationOperator  bitmap.CombinationOperator
	HSkipEnabled         bool
	HTemplate            byte
	IsMMREncoded         bool
	HGridWidth           uint32
	HGridHeight          uint32
	HGridX               int32
	HGridY               int32
	HRegionX             uint16
	HRegionY             uint16
	HalftoneRegionBitmap *bitmap.Bitmap
	Patterns             []*bitmap.Bitmap
}

func (tr *TextRegion) setCodingStatistics() error {
	if tr.cxIADT == nil {
		tr.cxIADT = arithmeticdecoder.NewStats(512, 1)
	}
	if tr.cxIAFS == nil {
		tr.cxIAFS = arithmeticdecoder.NewStats(512, 1)
	}
	if tr.cxIADS == nil {
		tr.cxIADS = arithmeticdecoder.NewStats(512, 1)
	}
	if tr.cxIAIT == nil {
		tr.cxIAIT = arithmeticdecoder.NewStats(512, 1)
	}
	if tr.cxIARI == nil {
		tr.cxIARI = arithmeticdecoder.NewStats(512, 1)
	}
	if tr.cxIARDW == nil {
		tr.cxIARDW = arithmeticdecoder.NewStats(512, 1)
	}
	if tr.cxIARDH == nil {
		tr.cxIARDH = arithmeticdecoder.NewStats(512, 1)
	}
	if tr.cxIAID == nil {
		tr.cxIAID = arithmeticdecoder.NewStats(1<<uint(tr.symbolCodeLength), 1)
	}
	if tr.cxIARDX == nil {
		tr.cxIARDX = arithmeticdecoder.NewStats(512, 1)
	}
	if tr.cxIARDY == nil {
		tr.cxIARDY = arithmeticdecoder.NewStats(512, 1)
	}
	if tr.arithDecoder == nil {
		var err error
		tr.arithDecoder, err = arithmeticdecoder.New(tr.reader)
		if err != nil {
			return err
		}
	}
	return nil
}

func (rs *RegionSegment) readCombinationOperator() error {
	v, err := rs.reader.ReadBits(3)
	if err != nil {
		return err
	}
	rs.CombinaionOperator = bitmap.CombinationOperator(v & 0xF)
	return nil
}

var _ SegmentEncoder = &RegionSegment{}

func (tr *TextRegion) decodeRdw() (int64, error) {
	const processName = "decodeRdw"
	if tr.IsHuffmanEncoded {
		if tr.SbHuffRDWidth == 3 {
			if tr.huffRDW == nil {
				var (
					userTableIndex int
					err            error
				)
				if tr.SbHuffFS == 3 {
					userTableIndex++
				}
				if tr.SbHuffDS == 3 {
					userTableIndex++
				}
				if tr.SbHuffDT == 3 {
					userTableIndex++
				}
				tr.huffRDW, err = tr.getUserTable(userTableIndex)
				if err != nil {
					return 0, jbig2errors.Wrap(err, processName, "")
				}
			}
			return tr.huffRDW.Decode(tr.reader)
		}
		standardTable, err := huffman.GetStandardTable(14 + int(tr.SbHuffRDWidth))
		if err != nil {
			return 0, jbig2errors.Wrap(err, processName, "")
		}
		return standardTable.Decode(tr.reader)
	}
	v, err := tr.arithDecoder.DecodeInt(tr.cxIARDW)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "")
	}
	return int64(v), nil
}

func (sd *SymbolDictionary) InitEncode(symbols *bitmap.Bitmaps, symbolList []int, symbolMap map[int]int, unborderSymbols bool) error {
	const processName = "SymbolDictionary.InitEncode"
	sd.SdATX = []int8{3, -3, 2, -2}
	sd.SdATY = []int8{-1, -1, -2, -2}
	sd.symbols = symbols
	sd.symbolList = make([]int, len(symbolList))
	copy(sd.symbolList, symbolList)
	if len(sd.symbolList) != sd.symbols.Size() {
		return jbig2errors.Error(processName, "symbols and symbolList of different size")
	}
	sd.NumberOfNewSymbols = uint32(symbols.Size())
	sd.NumberOfExportedSymbols = uint32(symbols.Size())
	sd.symbolMap = symbolMap
	sd.unborderSymbols = unborderSymbols
	return nil
}

func (tr *TextRegion) computeSymbolCodeLength() error {
	if tr.IsHuffmanEncoded {
		return tr.symbolIDCodeLengths()
	}
	tr.symbolCodeLength = int8(math.Ceil(math.Log(float64(tr.NumberOfSymbols)) / math.Log(2)))
	return nil
}

func (pis *PageInformationSegment) readWidthAndHeight() error {
	v, err := pis.reader.ReadBits(32)
	if err != nil {
		return err
	}
	pis.PageBMWidth = int(v & math.MaxInt32)
	v, err = pis.reader.ReadBits(32)
	if err != nil {
		return err
	}
	pis.PageBMHeight = int(v & math.MaxInt32)
	return nil
}

type (
	template0 struct{}
	Pager     interface {
		GetSegment(int) (*Header, error)
		GetBitmap() (*bitmap.Bitmap, error)
	}
)

func (sd *SymbolDictionary) getUserTable(index int) (huffman.Tabler, error) {
	var tableIndex int
	for _, rtSegment := range sd.Header.RTSegments {
		if rtSegment.Type == 53 {
			if tableIndex == index {
				segmentData, err := rtSegment.GetSegmentData()
				if err != nil {
					return nil, err
				}
				basicTabler := segmentData.(huffman.BasicTabler)
				return huffman.NewEncodedTable(basicTabler)
			}
			tableIndex++
		}
	}
	return nil, nil
}

func (h *Header) CleanSegmentData() {
	if h.SegmentData != nil {
		h.SegmentData = nil
	}
}

func (hr *HalftoneRegion) combineGrayscalePlanes(bms []*bitmap.Bitmap, plane int) error {
	byteIndex := 0
	for i := 0; i < bms[plane].Height; i++ {
		for j := 0; j < bms[plane].Width; j += 8 {
			upper, err := bms[plane+1].GetByte(byteIndex)
			if err != nil {
				return err
			}
			lower, err := bms[plane].GetByte(byteIndex)
			if err != nil {
				return err
			}
			err = bms[plane].SetByte(byteIndex, bitmap.CombineBytes(lower, upper, bitmap.CmbOpXor))
			if err != nil {
				return err
			}
			byteIndex++
		}
	}
	return nil
}

func (hr *HalftoneRegion) computeY(m, n int) int {
	return hr.shiftAndFill(int(hr.HGridY) + m*int(hr.HRegionX) - n*int(hr.HRegionY))
}

func (rs *RegionSegment) String() string {
	builder := &strings.Builder{}
	builder.WriteString("\t[REGION SEGMENT]\n")
	builder.WriteString(fmt.Sprintf("\t\t- Bitmap (width, height) [%dx%d]\n", rs.BitmapWidth, rs.BitmapHeight))
	builder.WriteString(fmt.Sprintf("\t\t- Location (x,y): [%d,%d]\n", rs.XLocation, rs.YLocation))
	builder.WriteString(fmt.Sprintf("\t\t- CombinationOperator: %s", rs.CombinaionOperator))
	return builder.String()
}

func (gr *GenericRegion) overrideAtTemplate1(context, x, y, result, bitIndex int) int {
	context &= 0x1FF7
	if gr.GBAtY[0] == 0 && gr.GBAtX[0] >= -int8(bitIndex) {
		context |= (result >> uint(7-(int8(bitIndex)+gr.GBAtX[0])) & 0x1) << 3
	} else {
		context |= int(gr.getPixel(x+int(gr.GBAtX[0]), y+int(gr.GBAtY[0]))) << 3
	}
	return context
}

func (h *Header) writeReferredToCount(bw bitwise.BinaryWriter) (n int, err error) {
	const processName = "writeReferredToCount"
	h.RTSNumbers = make([]int, len(h.RTSegments))
	for i, rtSegment := range h.RTSegments {
		h.RTSNumbers[i] = int(rtSegment.SegmentNumber)
	}
	if len(h.RTSNumbers) <= 4 {
		var b byte
		if len(h.RetainBits) >= 1 {
			b = h.RetainBits[0]
		}
		b |= byte(len(h.RTSNumbers)) << 5
		if err = bw.WriteByte(b); err != nil {
			return 0, jbig2errors.Wrap(err, processName, "short format")
		}
		return 1, nil
	}
	numRtsNumbers := uint32(len(h.RTSNumbers))
	buf := make([]byte, 4+basic.Ceil(len(h.RTSNumbers)+1, 8))
	numRtsNumbers |= 0x7 << 29
	binary.BigEndian.PutUint32(buf, numRtsNumbers)
	copy(buf[1:], h.RetainBits)
	n, err = bw.Write(buf)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "long format")
	}
	return n, nil
}

type TableSegment struct {
	reader *bitwise.Reader
	htOOB  int32
	htPS   int32
	htRS   int32
	htLow  int32
	htHigh int32
}

func (tr *TextRegion) setContexts(cxRefinement *arithmeticdecoder.DecoderStats, cxIADT *arithmeticdecoder.DecoderStats, cxIAFS *arithmeticdecoder.DecoderStats, cxIADS *arithmeticdecoder.DecoderStats, cxIAIT *arithmeticdecoder.DecoderStats, cxIAID *arithmeticdecoder.DecoderStats, cxIARDW *arithmeticdecoder.DecoderStats, cxIARDH *arithmeticdecoder.DecoderStats, cxIARDX *arithmeticdecoder.DecoderStats, cxIARDY *arithmeticdecoder.DecoderStats) {
	tr.cxIADT = cxIADT
	tr.cxIAFS = cxIAFS
	tr.cxIADS = cxIADS
	tr.cxIAIT = cxIAIT
	tr.cxIARDW = cxIARDW
	tr.cxIARDH = cxIARDH
	tr.cxIAID = cxIAID
	tr.cxIARDX = cxIARDX
	tr.cxIARDY = cxIARDY
	tr.cxRefinement = cxRefinement
}

func (pd *PatternDictionary) readPatternWidthAndHeight() error {
	v, err := pd.reader.ReadByte()
	if err != nil {
		return err
	}
	pd.HdpWidth = v
	v, err = pd.reader.ReadByte()
	if err != nil {
		return err
	}
	pd.HdpHeight = v
	return nil
}

func (grr *GenericRefinementRegion) readAtPixels() error {
	grr.GrAtX = make([]int8, 2)
	grr.GrAtY = make([]int8, 2)
	v, err := grr.reader.ReadByte()
	if err != nil {
		return err
	}
	grr.GrAtX[0] = int8(v)
	v, err = grr.reader.ReadByte()
	if err != nil {
		return err
	}
	grr.GrAtY[0] = int8(v)
	v, err = grr.reader.ReadByte()
	if err != nil {
		return err
	}
	grr.GrAtX[1] = int8(v)
	v, err = grr.reader.ReadByte()
	if err != nil {
		return err
	}
	grr.GrAtY[1] = int8(v)
	return nil
}

type RegionSegment struct {
	reader             *bitwise.Reader
	BitmapWidth        uint32
	BitmapHeight       uint32
	XLocation          uint32
	YLocation          uint32
	CombinaionOperator bitmap.CombinationOperator
}

func (sd *SymbolDictionary) setAtPixels() error {
	if sd.IsHuffmanEncoded {
		return nil
	}
	count := 1
	if sd.SdTemplate == 0 {
		count = 4
	}
	if err := sd.readAtPixels(count); err != nil {
		return err
	}
	return nil
}

type GenericRegion struct {
	reader           *bitwise.Reader
	DataHeaderOffset int64
	DataHeaderLength int64
	DataOffset       int64
	DataLength       int64
	RegionSegment    *RegionSegment
	UseExtTemplates  bool
	IsTPGDon         bool
	GBTemplate       byte
	IsMMREncoded     bool
	UseMMR           bool
	GBAtX            []int8
	GBAtY            []int8
	GBAtOverride     []bool
	hasAtOverride    bool
	Bitmap           *bitmap.Bitmap
	arithDecoder     *arithmeticdecoder.Decoder
	cx               *arithmeticdecoder.DecoderStats
	mmrDecoder       *mmr.Decoder
}

func (pd *PatternDictionary) computeSegmentDataStructure() error {
	pd.DataOffset = pd.reader.AbsolutePosition()
	pd.DataHeaderLength = pd.DataOffset - pd.DataHeaderOffset
	pd.DataLength = int64(pd.reader.AbsoluteLength()) - pd.DataHeaderLength
	return nil
}

func (pis *PageInformationSegment) readMaxStripeSize() error {
	v, err := pis.reader.ReadBits(15)
	if err != nil {
		return err
	}
	pis.MaxStripeSize = uint16(v & math.MaxUint16)
	return nil
}
