// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package segments

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	arithmeticdecoder "github.com/OpenMellon/openpdf/internal/jbig2/decoder/arithmetic"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder/huffman"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder/mmr"
	arithmeticencoder "github.com/OpenMellon/openpdf/internal/jbig2/encoder/arithmetic"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
	jbig2internal "github.com/OpenMellon/openpdf/internal/jbig2/internal"
)

type Regioner interface {
	GetRegionBitmap() (*bitmap.Bitmap, error)
	GetRegionInfo() *RegionSegment
}

func (tr *TextRegion) createRegionBitmap() error {
	var err error
	tr.RegionBitmap, err = bitmap.New(int(tr.RegionInfo.BitmapWidth), int(tr.RegionInfo.BitmapHeight))
	if err != nil {
		return err
	}
	if tr.DefaultPixel != 0 {
		tr.RegionBitmap.SetDefaultPixel()
	}
	return nil
}

func (h *Header) String() string {
	builder := &strings.Builder{}
	builder.WriteString("\n[SEGMENT-HEADER]\n")
	fmt.Fprintf(builder, "\t- SegmentNumber: %v\n", h.SegmentNumber)
	fmt.Fprintf(builder, "\t- Type: %v\n", h.Type)
	fmt.Fprintf(builder, "\t- RetainFlag: %v\n", h.RetainFlag)
	fmt.Fprintf(builder, "\t- PageAssociation: %v\n", h.PageAssociation)
	fmt.Fprintf(builder, "\t- PageAssociationFieldSize: %v\n", h.PageAssociationFieldSize)
	builder.WriteString("\t- RTSEGMENTS:\n")
	for _, rtsNumber := range h.RTSNumbers {
		fmt.Fprintf(builder, "\t\t- %d\n", rtsNumber)
	}
	fmt.Fprintf(builder, "\t- HeaderLength: %v\n", h.HeaderLength)
	fmt.Fprintf(builder, "\t- SegmentDataLength: %v\n", h.SegmentDataLength)
	fmt.Fprintf(builder, "\t- SegmentDataStartOffset: %v\n", h.SegmentDataStartOffset)
	return builder.String()
}

func (pis *PageInformationSegment) readContainsRefinement() error {
	bit, err := pis.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		pis.mightContainRefinements = true
	}
	return nil
}

func (gr *GenericRegion) getPixel(x, y int) int8 {
	if x < 0 || x >= gr.Bitmap.Width {
		return 0
	}
	if y < 0 || y >= gr.Bitmap.Height {
		return 0
	}
	if gr.Bitmap.GetPixel(x, y) {
		return 1
	}
	return 0
}

func (sd *SymbolDictionary) huffDecodeBmSize() (int64, error) {
	if sd.huffBMSize == nil {
		var (
			userTableIndex int
			err            error
		)
		if sd.SdHuffDecodeHeightSelection == 3 {
			userTableIndex++
		}
		if sd.SdHuffDecodeWidthSelection == 3 {
			userTableIndex++
		}
		sd.huffBMSize, err = sd.getUserTable(userTableIndex)
		if err != nil {
			return 0, err
		}
	}
	return sd.huffBMSize.Decode(sd.reader)
}

type template1 struct{}

func (h *Header) writeSegmentDataLength(bw bitwise.BinaryWriter) (n int, err error) {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(h.SegmentDataLength))
	if n, err = bw.Write(buf); err != nil {
		return 0, jbig2errors.Wrap(err, "Header.writeSegmentDataLength", "")
	}
	return n, nil
}

func (pis *PageInformationSegment) encodeFlags(bw bitwise.BinaryWriter) (err error) {
	const processName = "encodeFlags"
	if err = bw.SkipBits(1); err != nil {
		return jbig2errors.Wrap(err, processName, "reserved bit")
	}
	var bit int
	if pis.CombinationOperatorOverrideAllowed() {
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return jbig2errors.Wrap(err, processName, "combination operator overridden")
	}
	bit = 0
	if pis.requiresAuxiliaryBuffer {
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return jbig2errors.Wrap(err, processName, "requires auxiliary buffer")
	}
	if err = bw.WriteBit((int(pis.combinationOperator) >> 1) & 0x01); err != nil {
		return jbig2errors.Wrap(err, processName, "combination operator first bit")
	}
	if err = bw.WriteBit(int(pis.combinationOperator) & 0x01); err != nil {
		return jbig2errors.Wrap(err, processName, "combination operator second bit")
	}
	bit = int(pis.DefaultPixelValue)
	if err = bw.WriteBit(bit); err != nil {
		return jbig2errors.Wrap(err, processName, "default page pixel value")
	}
	bit = 0
	if pis.mightContainRefinements {
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return jbig2errors.Wrap(err, processName, "contains refinement")
	}
	bit = 0
	if pis.IsLossless {
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return jbig2errors.Wrap(err, processName, "page is eventually lossless")
	}
	return nil
}

var (
	placeholderSegment Segmenter
	segmentCreators    = map[Type]func() Segmenter{TSymbolDictionary: func() Segmenter { return &SymbolDictionary{} }, TIntermediateTextRegion: func() Segmenter { return &TextRegion{} }, TImmediateTextRegion: func() Segmenter { return &TextRegion{} }, TImmediateLosslessTextRegion: func() Segmenter { return &TextRegion{} }, TPatternDictionary: func() Segmenter { return &PatternDictionary{} }, TIntermediateHalftoneRegion: func() Segmenter { return &HalftoneRegion{} }, TImmediateHalftoneRegion: func() Segmenter { return &HalftoneRegion{} }, TImmediateLosslessHalftoneRegion: func() Segmenter { return &HalftoneRegion{} }, TIntermediateGenericRegion: func() Segmenter { return &GenericRegion{} }, TImmediateGenericRegion: func() Segmenter { return &GenericRegion{} }, TImmediateLosslessGenericRegion: func() Segmenter { return &GenericRegion{} }, TIntermediateGenericRefinementRegion: func() Segmenter { return &GenericRefinementRegion{} }, TImmediateGenericRefinementRegion: func() Segmenter { return &GenericRefinementRegion{} }, TImmediateLosslessGenericRefinementRegion: func() Segmenter { return &GenericRefinementRegion{} }, TPageInformation: func() Segmenter { return &PageInformationSegment{} }, TEndOfPage: func() Segmenter { return placeholderSegment }, TEndOfStrip: func() Segmenter { return &EndOfStripe{} }, TEndOfFile: func() Segmenter { return placeholderSegment }, TProfiles: func() Segmenter { return placeholderSegment }, TTables: func() Segmenter { return &TableSegment{} }, TExtension: func() Segmenter { return placeholderSegment }, TBitmap: func() Segmenter { return placeholderSegment }}
)

func (gr *GenericRegion) computeSegmentDataStructure() error {
	gr.DataOffset = gr.reader.AbsolutePosition()
	gr.DataHeaderLength = gr.DataOffset - gr.DataHeaderOffset
	gr.DataLength = int64(gr.reader.AbsoluteLength()) - gr.DataHeaderLength
	return nil
}

func (gr *GenericRegion) GetRegionBitmap() (bm *bitmap.Bitmap, err error) {
	if gr.Bitmap != nil {
		return gr.Bitmap, nil
	}
	if gr.IsMMREncoded {
		if gr.mmrDecoder == nil {
			gr.mmrDecoder, err = mmr.New(gr.reader, int(gr.RegionSegment.BitmapWidth), int(gr.RegionSegment.BitmapHeight), gr.DataOffset, gr.DataLength)
			if err != nil {
				return nil, err
			}
		}
		gr.Bitmap, err = gr.mmrDecoder.UncompressMMR()
		return gr.Bitmap, err
	}
	if err = gr.updateOverrideFlags(); err != nil {
		return nil, err
	}
	var ltp int
	if gr.arithDecoder == nil {
		gr.arithDecoder, err = arithmeticdecoder.New(gr.reader)
		if err != nil {
			return nil, err
		}
	}
	if gr.cx == nil {
		gr.cx = arithmeticdecoder.NewStats(65536, 1)
	}
	gr.Bitmap, err = bitmap.New(int(gr.RegionSegment.BitmapWidth), int(gr.RegionSegment.BitmapHeight))
	if err != nil {
		return nil, err
	}
	width := int(uint32(gr.Bitmap.Width+7) & (^uint32(7)))
	for i := 0; i < gr.Bitmap.Height; i++ {
		if gr.IsTPGDon {
			var sltp int
			sltp, err = gr.decodeSLTP()
			if err != nil {
				return nil, err
			}
			ltp ^= sltp
		}
		if ltp == 1 {
			if i > 0 {
				if err = gr.copyLineAbove(i); err != nil {
					return nil, err
				}
			}
		} else {
			if err = gr.decodeLine(i, gr.Bitmap.Width, width); err != nil {
				return nil, err
			}
		}
	}
	return gr.Bitmap, nil
}

func (hr *HalftoneRegion) GetPatterns() ([]*bitmap.Bitmap, error) {
	var (
		bms []*bitmap.Bitmap
		err error
	)
	for _, rtSegment := range hr.header.RTSegments {
		var segment Segmenter
		segment, err = rtSegment.GetSegmentData()
		if err != nil {
			common.Log.Debug("GetSegmentData failed: %v", err)
			return nil, err
		}
		pd, ok := segment.(*PatternDictionary)
		if !ok {
			err = fmt.Errorf("related segment not a pattern dictionary: %T", segment)
			return nil, err
		}
		var patterns []*bitmap.Bitmap
		patterns, err = pd.GetDictionary()
		if err != nil {
			common.Log.Debug("pattern GetDictionary failed: %v", err)
			return nil, err
		}
		bms = append(bms, patterns...)
	}
	return bms, nil
}

func (h *Header) readHeaderFlags() error {
	const processName = "readHeaderFlags"
	bit, err := h.Reader.ReadBit()
	if err != nil {
		return jbig2errors.Wrap(err, processName, "retain flag")
	}
	if bit != 0 {
		h.RetainFlag = true
	}
	bit, err = h.Reader.ReadBit()
	if err != nil {
		return jbig2errors.Wrap(err, processName, "page association")
	}
	if bit != 0 {
		h.PageAssociationFieldSize = true
	}
	segmentType, err := h.Reader.ReadBits(6)
	if err != nil {
		return jbig2errors.Wrap(err, processName, "segment type")
	}
	h.Type = Type(int(segmentType))
	return nil
}

func (pis *PageInformationSegment) checkInput() error {
	if pis.PageBMHeight == math.MaxInt32 {
		if !pis.IsStripe {
			common.Log.Debug("PageInformationSegment.IsStripe should be true.")
		}
	}
	return nil
}

func (tr *TextRegion) encodeSymbols(bw bitwise.BinaryWriter) (n int, err error) {
	const processName = "encodeSymbols"
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, tr.NumberOfSymbols)
	if n, err = bw.Write(buf); err != nil {
		return n, jbig2errors.Wrap(err, processName, "NumberOfSymbolInstances")
	}
	classedPoints, err := bitmap.NewClassedPoints(tr.inLL, tr.comps)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "")
	}
	var stripT, firstS int
	e := arithmeticencoder.New()
	e.Init()
	if err = e.EncodeInteger(arithmeticencoder.IADT, 0); err != nil {
		return n, jbig2errors.Wrap(err, processName, "initial DT")
	}
	groupByY, err := classedPoints.GroupByY()
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "")
	}
	for _, cp := range groupByY {
		yAtIndex := int(cp.YAtIndex(0))
		deltaT := yAtIndex - stripT
		if err = e.EncodeInteger(arithmeticencoder.IADT, deltaT); err != nil {
			return n, jbig2errors.Wrap(err, processName, "")
		}
		var curS int
		for j, intSliceItem := range cp.IntSlice {
			switch j {
			case 0:
				xAtIndex := int(cp.XAtIndex(j)) - firstS
				if err = e.EncodeInteger(arithmeticencoder.IAFS, xAtIndex); err != nil {
					return n, jbig2errors.Wrap(err, processName, "")
				}
				firstS += xAtIndex
				curS = firstS
			default:
				xAtIndex := int(cp.XAtIndex(j)) - curS
				if err = e.EncodeInteger(arithmeticencoder.IADS, xAtIndex); err != nil {
					return n, jbig2errors.Wrap(err, processName, "")
				}
				curS += xAtIndex
			}
			get, err := tr.classIDs.Get(intSliceItem)
			if err != nil {
				return n, jbig2errors.Wrap(err, processName, "")
			}
			globalSymbolsMapElem, ok := tr.globalSymbolsMap[get]
			if !ok {
				globalSymbolsMapElem, ok = tr.localSymbolsMap[get]
				if !ok {
					return n, jbig2errors.Errorf(processName, "Symbol: '%d' is not found in global and local symbol map", get)
				}
			}
			if err = e.EncodeIAID(tr.symBits, globalSymbolsMapElem); err != nil {
				return n, jbig2errors.Wrap(err, processName, "")
			}
		}
		if err = e.EncodeOOB(arithmeticencoder.IADS); err != nil {
			return n, jbig2errors.Wrap(err, processName, "")
		}
	}
	e.Final()
	writeTo, err := e.WriteTo(bw)
	if err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	n += int(writeTo)
	return n, nil
}

func (hr *HalftoneRegion) computeX(m, n int) int {
	return hr.shiftAndFill(int(hr.HGridX) + m*int(hr.HRegionY) + n*int(hr.HRegionX))
}
func (t *template1) setIndex(ds *arithmeticdecoder.DecoderStats) { ds.SetIndex(0x080) }
func (h *Header) readSegmentNumber(r *bitwise.Reader) error {
	const processName = "readSegmentNumber"
	buf := make([]byte, 4)
	_, err := r.Read(buf)
	if err != nil {
		return jbig2errors.Wrap(err, processName, "")
	}
	h.SegmentNumber = binary.BigEndian.Uint32(buf)
	return nil
}

func (tr *TextRegion) readHuffmanFlags() error {
	var (
		bit  int
		bits uint64
		err  error
	)
	_, err = tr.reader.ReadBit()
	if err != nil {
		return err
	}
	bit, err = tr.reader.ReadBit()
	if err != nil {
		return err
	}
	tr.SbHuffRSize = int8(bit)
	bits, err = tr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	tr.SbHuffRDY = int8(bits) & 0xf
	bits, err = tr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	tr.SbHuffRDX = int8(bits) & 0xf
	bits, err = tr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	tr.SbHuffRDHeight = int8(bits) & 0xf
	bits, err = tr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	tr.SbHuffRDWidth = int8(bits) & 0xf
	bits, err = tr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	tr.SbHuffDT = int8(bits) & 0xf
	bits, err = tr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	tr.SbHuffDS = int8(bits) & 0xf
	bits, err = tr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	tr.SbHuffFS = int8(bits) & 0xf
	return nil
}

func (grr *GenericRefinementRegion) String() string {
	builder := &strings.Builder{}
	builder.WriteString("\n[GENERIC REGION]\n")
	builder.WriteString(grr.RegionInfo.String() + "\n")
	fmt.Fprintf(builder, "\t- IsTPGRon: %v\n", grr.IsTPGROn)
	fmt.Fprintf(builder, "\t- TemplateID: %v\n", grr.TemplateID)
	fmt.Fprintf(builder, "\t- GrAtX: %v\n", grr.GrAtX)
	fmt.Fprintf(builder, "\t- GrAtY: %v\n", grr.GrAtY)
	fmt.Fprintf(builder, "\t- ReferenceDX %v\n", grr.ReferenceDX)
	fmt.Fprintf(builder, "\t- ReferencDeY: %v\n", grr.ReferenceDY)
	return builder.String()
}
func (hr *HalftoneRegion) GetRegionInfo() *RegionSegment { return hr.RegionSegment }

type GenericRefinementRegion struct {
	refTemplate0    templater
	refTemplate1    templater
	reader          *bitwise.Reader
	header          *Header
	RegionInfo      *RegionSegment
	IsTPGROn        bool
	TemplateID      int8
	Template        templater
	GrAtX           []int8
	GrAtY           []int8
	RegionBitmap    *bitmap.Bitmap
	ReferenceBitmap *bitmap.Bitmap
	ReferenceDX     int32
	ReferenceDY     int32
	arithDecoder    *arithmeticdecoder.Decoder
	cx              *arithmeticdecoder.DecoderStats
	hasAtOverride   bool
	atOverride      []bool
}

func (grr *GenericRefinementRegion) Init(header *Header, r *bitwise.Reader) error {
	grr.header = header
	grr.reader = r
	grr.RegionInfo = NewRegionSegment(r)
	return grr.parseHeader()
}

func (pis *PageInformationSegment) String() string {
	builder := &strings.Builder{}
	builder.WriteString("\n[PAGE-INFORMATION-SEGMENT]\n")
	fmt.Fprintf(builder, "\t- BMHeight: %d\n", pis.PageBMHeight)
	fmt.Fprintf(builder, "\t- BMWidth: %d\n", pis.PageBMWidth)
	fmt.Fprintf(builder, "\t- ResolutionX: %d\n", pis.ResolutionX)
	fmt.Fprintf(builder, "\t- ResolutionY: %d\n", pis.ResolutionY)
	fmt.Fprintf(builder, "\t- CombinationOperator: %s\n", pis.combinationOperator)
	fmt.Fprintf(builder, "\t- CombinationOperatorOverride: %v\n", pis.combinationOperatorOverride)
	fmt.Fprintf(builder, "\t- IsLossless: %v\n", pis.IsLossless)
	fmt.Fprintf(builder, "\t- RequiresAuxiliaryBuffer: %v\n", pis.requiresAuxiliaryBuffer)
	fmt.Fprintf(builder, "\t- MightContainRefinements: %v\n", pis.mightContainRefinements)
	fmt.Fprintf(builder, "\t- IsStriped: %v\n", pis.IsStripe)
	fmt.Fprintf(builder, "\t- MaxStripeSize: %v\n", pis.MaxStripeSize)
	return builder.String()
}

func (gr *GenericRegion) String() string {
	builder := &strings.Builder{}
	builder.WriteString("\n[GENERIC REGION]\n")
	builder.WriteString(gr.RegionSegment.String() + "\n")
	fmt.Fprintf(builder, "\t- UseExtTemplates: %v\n", gr.UseExtTemplates)
	fmt.Fprintf(builder, "\t- IsTPGDon: %v\n", gr.IsTPGDon)
	fmt.Fprintf(builder, "\t- GBTemplate: %d\n", gr.GBTemplate)
	fmt.Fprintf(builder, "\t- IsMMREncoded: %v\n", gr.IsMMREncoded)
	fmt.Fprintf(builder, "\t- GBAtX: %v\n", gr.GBAtX)
	fmt.Fprintf(builder, "\t- GBAtY: %v\n", gr.GBAtY)
	fmt.Fprintf(builder, "\t- GBAtOverride: %v\n", gr.GBAtOverride)
	return builder.String()
}
func (ts *TableSegment) HtHigh() int32 { return ts.htHigh }
func (gr *GenericRegion) decodeTemplate3(lineNumber, width, paddedWidth int, byteIndex, prevByteIndex int) (err error) {
	const processName = "decodeTemplate3"
	var (
		context, overriddenContext int
		line1                      int
		b                          byte
		next, bit                  int
	)
	if lineNumber >= 1 {
		b, err = gr.Bitmap.GetByte(prevByteIndex)
		if err != nil {
			return jbig2errors.Wrap(err, processName, "line >= 1")
		}
		line1 = int(b)
	}
	context = (line1 >> 1) & 0x70
	for i := 0; i < paddedWidth; i = next {
		var (
			result     byte
			minorWidth int
		)
		next = i + 8
		if remaining := width - i; remaining > 8 {
			minorWidth = 8
		} else {
			minorWidth = remaining
		}
		if lineNumber >= 1 {
			line1 <<= 8
			if next < width {
				b, err = gr.Bitmap.GetByte(prevByteIndex + 1)
				if err != nil {
					return jbig2errors.Wrap(err, processName, "inner - line >= 1")
				}
				line1 |= int(b)
			}
		}
		for j := 0; j < minorWidth; j++ {
			if gr.hasAtOverride {
				overriddenContext = gr.overrideAtTemplate3(context, i+j, lineNumber, int(result), j)
				gr.cx.SetIndex(int32(overriddenContext))
			} else {
				gr.cx.SetIndex(int32(context))
			}
			bit, err = gr.arithDecoder.DecodeBit(gr.cx)
			if err != nil {
				return jbig2errors.Wrap(err, processName, "")
			}
			result |= byte(bit) << byte(7-j)
			context = ((context & 0x1f7) << 1) | bit | ((line1 >> uint(8-j)) & 0x010)
		}
		if err := gr.Bitmap.SetByte(byteIndex, result); err != nil {
			return jbig2errors.Wrap(err, processName, "")
		}
		byteIndex++
		prevByteIndex++
	}
	return nil
}

func (gr *GenericRegion) decodeTemplate2(lineNumber, width, paddedWidth int, byteIndex, prevByteIndex int) (err error) {
	const processName = "decodeTemplate2"
	var (
		context, overriddenContext int
		line1, line2               int
		b                          byte
		next, bit                  int
	)
	if lineNumber >= 1 {
		b, err = gr.Bitmap.GetByte(prevByteIndex)
		if err != nil {
			return jbig2errors.Wrap(err, processName, "lineNumber >= 1")
		}
		line1 = int(b)
	}
	if lineNumber >= 2 {
		b, err = gr.Bitmap.GetByte(prevByteIndex - gr.Bitmap.RowStride)
		if err != nil {
			return jbig2errors.Wrap(err, processName, "lineNumber >= 2")
		}
		line2 = int(b) << 4
	}
	context = (line1 >> 3 & 0x7c) | (line2 >> 3 & 0x380)
	for i := 0; i < paddedWidth; i = next {
		var (
			result     byte
			minorWidth int
		)
		next = i + 8
		if remaining := width - i; remaining > 8 {
			minorWidth = 8
		} else {
			minorWidth = remaining
		}
		if lineNumber > 0 {
			line1 <<= 8
			if next < width {
				b, err = gr.Bitmap.GetByte(prevByteIndex + 1)
				if err != nil {
					return jbig2errors.Wrap(err, processName, "lineNumber > 0")
				}
				line1 |= int(b)
			}
		}
		if lineNumber > 1 {
			line2 <<= 8
			if next < width {
				b, err = gr.Bitmap.GetByte(prevByteIndex - gr.Bitmap.RowStride + 1)
				if err != nil {
					return jbig2errors.Wrap(err, processName, "lineNumber > 1")
				}
				line2 |= int(b) << 4
			}
		}
		for j := 0; j < minorWidth; j++ {
			shift := uint(10 - j)
			if gr.hasAtOverride {
				overriddenContext = gr.overrideAtTemplate2(context, i+j, lineNumber, int(result), j)
				gr.cx.SetIndex(int32(overriddenContext))
			} else {
				gr.cx.SetIndex(int32(context))
			}
			bit, err = gr.arithDecoder.DecodeBit(gr.cx)
			if err != nil {
				return jbig2errors.Wrap(err, processName, "")
			}
			result |= byte(bit << uint(7-j))
			context = ((context & 0x1bd) << 1) | bit | ((line1 >> shift) & 0x4) | ((line2 >> shift) & 0x80)
		}
		if err := gr.Bitmap.SetByte(byteIndex, result); err != nil {
			return jbig2errors.Wrap(err, processName, "")
		}
		byteIndex++
		prevByteIndex++
	}
	return nil
}

func (tr *TextRegion) decodeDfs() (int64, error) {
	if tr.IsHuffmanEncoded {
		if tr.SbHuffFS == 3 {
			if tr.huffFS == nil {
				var err error
				tr.huffFS, err = tr.getUserTable(0)
				if err != nil {
					return 0, err
				}
			}
			return tr.huffFS.Decode(tr.reader)
		}
		standardTable, err := huffman.GetStandardTable(6 + int(tr.SbHuffFS))
		if err != nil {
			return 0, err
		}
		return standardTable.Decode(tr.reader)
	}
	dfs, err := tr.arithDecoder.DecodeInt(tr.cxIAFS)
	if err != nil {
		return 0, err
	}
	return int64(dfs), nil
}

func (hr *HalftoneRegion) GetRegionBitmap() (*bitmap.Bitmap, error) {
	if hr.HalftoneRegionBitmap != nil {
		return hr.HalftoneRegionBitmap, nil
	}
	var err error
	hr.HalftoneRegionBitmap, err = bitmap.New(int(hr.RegionSegment.BitmapWidth), int(hr.RegionSegment.BitmapHeight))
	if err != nil {
		return nil, err
	}
	if hr.Patterns == nil || (hr.Patterns != nil && len(hr.Patterns) == 0) {
		hr.Patterns, err = hr.GetPatterns()
		if err != nil {
			return nil, err
		}
	}
	if hr.HDefaultPixel == 1 {
		hr.HalftoneRegionBitmap.SetDefaultPixel()
	}
	log2Patterns := math.Ceil(math.Log(float64(len(hr.Patterns))) / math.Log(2))
	bitsPerValue := int(log2Patterns)
	var grayScale [][]int
	grayScale, err = hr.grayScaleDecoding(bitsPerValue)
	if err != nil {
		return nil, err
	}
	if err = hr.renderPattern(grayScale); err != nil {
		return nil, err
	}
	return hr.HalftoneRegionBitmap, nil
}

func (tr *TextRegion) decodeStripT() (t int64, err error) {
	if tr.IsHuffmanEncoded {
		if tr.SbHuffDT == 3 {
			if tr.huffDT == nil {
				var userTableIndex int
				if tr.SbHuffFS == 3 {
					userTableIndex++
				}
				if tr.SbHuffDS == 3 {
					userTableIndex++
				}
				tr.huffDT, err = tr.getUserTable(userTableIndex)
				if err != nil {
					return 0, err
				}
			}
			t, err = tr.huffDT.Decode(tr.reader)
			if err != nil {
				return 0, err
			}
		} else {
			var table huffman.Tabler
			table, err = huffman.GetStandardTable(11 + int(tr.SbHuffDT))
			if err != nil {
				return 0, err
			}
			t, err = table.Decode(tr.reader)
			if err != nil {
				return 0, err
			}
		}
	} else {
		var dt int32
		dt, err = tr.arithDecoder.DecodeInt(tr.cxIADT)
		if err != nil {
			return 0, err
		}
		t = int64(dt)
	}
	t *= int64(-tr.SbStrips)
	return t, nil
}

func (h *Header) readNumberOfReferredToSegments(r *bitwise.Reader) (uint64, error) {
	const processName = "readNumberOfReferredToSegments"
	count, err := r.ReadBits(3)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "count of rts")
	}
	count &= 0xf
	var countBits []byte
	if count <= 4 {
		countBits = make([]byte, 5)
		for i := 0; i <= 4; i++ {
			bit, err := r.ReadBit()
			if err != nil {
				return 0, jbig2errors.Wrap(err, processName, "short format")
			}
			countBits[i] = byte(bit)
		}
	} else {
		count, err = r.ReadBits(29)
		if err != nil {
			return 0, err
		}
		count &= math.MaxInt32
		bitLen := (count + 8) >> 3
		bitLen <<= 3
		countBits = make([]byte, bitLen)
		var i uint64
		for i = 0; i < bitLen; i++ {
			bit, err := r.ReadBit()
			if err != nil {
				return 0, jbig2errors.Wrap(err, processName, "long format")
			}
			countBits[i] = byte(bit)
		}
	}
	return count, nil
}

type PageInformationSegment struct {
	reader                      *bitwise.Reader
	PageBMHeight                int
	PageBMWidth                 int
	ResolutionX                 int
	ResolutionY                 int
	combinationOperatorOverride bool
	combinationOperator         bitmap.CombinationOperator
	requiresAuxiliaryBuffer     bool
	DefaultPixelValue           uint8
	mightContainRefinements     bool
	IsLossless                  bool
	IsStripe                    bool
	MaxStripeSize               uint16
}

const (
	ORandom OrganizationType = iota
	OSequential
)

func (h *Header) referenceSize() uint {
	switch {
	case h.SegmentNumber <= 255:
		return 1
	case h.SegmentNumber <= 65535:
		return 2
	default:
		return 4
	}
}

func (pis *PageInformationSegment) encodeStripingInformation(bw bitwise.BinaryWriter) (n int, err error) {
	const processName = "encodeStripingInformation"
	if !pis.IsStripe {
		if n, err = bw.Write([]byte{0x00, 0x00}); err != nil {
			return 0, jbig2errors.Wrap(err, processName, "no striping")
		}
		return n, nil
	}
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, pis.MaxStripeSize|1<<15)
	if n, err = bw.Write(buf); err != nil {
		return 0, jbig2errors.Wrapf(err, processName, "striping: %d", pis.MaxStripeSize)
	}
	return n, nil
}

func (gr *GenericRegion) setParameters(isMMREncoded bool, dataOffset, dataLength int64, bitmapHeight, bitmapWidth uint32) {
	gr.IsMMREncoded = isMMREncoded
	gr.DataOffset = dataOffset
	gr.DataLength = dataLength
	gr.RegionSegment.BitmapHeight = bitmapHeight
	gr.RegionSegment.BitmapWidth = bitmapWidth
	gr.mmrDecoder = nil
	gr.Bitmap = nil
}

func (sd *SymbolDictionary) decodeThroughTextRegion(width, height, numInstances uint32) error {
	if sd.textRegion == nil {
		sd.textRegion = newTextRegion(sd.reader, nil)
		sd.textRegion.setContexts(sd.cx, arithmeticdecoder.NewStats(512, 1), arithmeticdecoder.NewStats(512, 1), arithmeticdecoder.NewStats(512, 1), arithmeticdecoder.NewStats(512, 1), sd.cxIAID, arithmeticdecoder.NewStats(512, 1), arithmeticdecoder.NewStats(512, 1), arithmeticdecoder.NewStats(512, 1), arithmeticdecoder.NewStats(512, 1))
	}
	if err := sd.setSymbolsArray(); err != nil {
		return err
	}
	sd.textRegion.setParameters(sd.arithDecoder, sd.IsHuffmanEncoded, true, width, height, numInstances, 1, sd.numberOfImportedSymbols+sd.numberOfDecodedSymbols, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, sd.SdrTemplate, sd.SdrATX, sd.SdrATY, sd.allSymbols, sd.symbolCodeLength)
	return sd.addSymbol(sd.textRegion)
}

func (hr *HalftoneRegion) Init(hd *Header, r *bitwise.Reader) error {
	hr.reader = r
	hr.header = hd
	hr.RegionSegment = NewRegionSegment(r)
	return hr.parseHeader()
}

type Type int

func (pis *PageInformationSegment) CombinationOperatorOverrideAllowed() bool {
	return pis.combinationOperatorOverride
}

func (hr *HalftoneRegion) renderPattern(grayScale [][]int) (err error) {
	var x, y int
	for i := 0; i < int(hr.HGridHeight); i++ {
		for j := 0; j < int(hr.HGridWidth); j++ {
			x = hr.computeX(i, j)
			y = hr.computeY(i, j)
			pattern := hr.Patterns[grayScale[i][j]]
			if err = bitmap.Blit(pattern, hr.HalftoneRegionBitmap, x+int(hr.HGridX), y+int(hr.HGridY), hr.CombinationOperator); err != nil {
				return err
			}
		}
	}
	return nil
}

func (tr *TextRegion) getSymbols() error {
	if tr.Header.RTSegments != nil {
		return tr.initSymbols()
	}
	return nil
}

func (gr *GenericRegion) overrideAtTemplate0a(context, x, y, result, bitIndex, shift int) int {
	if gr.GBAtOverride[0] {
		context &= 0xFFEF
		if gr.GBAtY[0] == 0 && gr.GBAtX[0] >= -int8(bitIndex) {
			context |= (result >> uint(int8(shift)-gr.GBAtX[0]&0x1)) << 4
		} else {
			context |= int(gr.getPixel(x+int(gr.GBAtX[0]), y+int(gr.GBAtY[0]))) << 4
		}
	}
	if gr.GBAtOverride[1] {
		context &= 0xFBFF
		if gr.GBAtY[1] == 0 && gr.GBAtX[1] >= -int8(bitIndex) {
			context |= (result >> uint(int8(shift)-gr.GBAtX[1]&0x1)) << 10
		} else {
			context |= int(gr.getPixel(x+int(gr.GBAtX[1]), y+int(gr.GBAtY[1]))) << 10
		}
	}
	if gr.GBAtOverride[2] {
		context &= 0xF7FF
		if gr.GBAtY[2] == 0 && gr.GBAtX[2] >= -int8(bitIndex) {
			context |= (result >> uint(int8(shift)-gr.GBAtX[2]&0x1)) << 11
		} else {
			context |= int(gr.getPixel(x+int(gr.GBAtX[2]), y+int(gr.GBAtY[2]))) << 11
		}
	}
	if gr.GBAtOverride[3] {
		context &= 0x7FFF
		if gr.GBAtY[3] == 0 && gr.GBAtX[3] >= -int8(bitIndex) {
			context |= (result >> uint(int8(shift)-gr.GBAtX[3]&0x1)) << 15
		} else {
			context |= int(gr.getPixel(x+int(gr.GBAtX[3]), y+int(gr.GBAtY[3]))) << 15
		}
	}
	return context
}

func (grr *GenericRefinementRegion) decodeTypicalPredictedLineTemplate1(lineNumber, width, rowStride, refRowStride, paddedWidth, refOffset, byteIndex, referenceDY, refByteIndex int) (err error) {
	var (
		context, typicalContext int
		regionRow, refRowAbove  int
		refRow, refRowBelow     int
		b                       byte
	)
	if lineNumber > 0 {
		b, err = grr.RegionBitmap.GetByte(byteIndex - rowStride)
		if err != nil {
			return err
		}
		regionRow = int(b)
	}
	if referenceDY > 0 && referenceDY <= grr.ReferenceBitmap.Height {
		b, err = grr.ReferenceBitmap.GetByte(refByteIndex - refRowStride + refOffset)
		if err != nil {
			return err
		}
		refRowAbove = int(b) << 2
	}
	if referenceDY >= 0 && referenceDY < grr.ReferenceBitmap.Height {
		b, err = grr.ReferenceBitmap.GetByte(refByteIndex + refOffset)
		if err != nil {
			return err
		}
		refRow = int(b)
	}
	if referenceDY > -2 && referenceDY < grr.ReferenceBitmap.Height-1 {
		b, err = grr.ReferenceBitmap.GetByte(refByteIndex + refRowStride + refOffset)
		if err != nil {
			return err
		}
		refRowBelow = int(b)
	}
	context = ((regionRow >> 5) & 0x6) | ((refRowBelow >> 2) & 0x30) | (refRow & 0xc0) | (refRowAbove & 0x200)
	typicalContext = ((refRowBelow >> 2) & 0x70) | (refRow & 0xc0) | (refRowAbove & 0x700)
	var next int
	for i := 0; i < paddedWidth; i = next {
		var (
			minorWidth int
			result     int
		)
		next = i + 8
		if minorWidth = width - i; minorWidth > 8 {
			minorWidth = 8
		}
		inRegion := next < width
		inRef := next < grr.ReferenceBitmap.Width
		refOffsetNext := refOffset + 1
		if lineNumber > 0 {
			b = 0
			if inRegion {
				b, err = grr.RegionBitmap.GetByte(byteIndex - rowStride + 1)
				if err != nil {
					return err
				}
			}
			regionRow = (regionRow << 8) | int(b)
		}
		if referenceDY > 0 && referenceDY <= grr.ReferenceBitmap.Height {
			var refAbove int
			if inRef {
				b, err = grr.ReferenceBitmap.GetByte(refByteIndex - refRowStride + refOffsetNext)
				if err != nil {
					return err
				}
				refAbove = int(b) << 2
			}
			refRowAbove = (refRowAbove << 8) | refAbove
		}
		if referenceDY >= 0 && referenceDY < grr.ReferenceBitmap.Height {
			b = 0
			if inRef {
				b, err = grr.ReferenceBitmap.GetByte(refByteIndex + refOffsetNext)
				if err != nil {
					return err
				}
			}
			refRow = (refRow << 8) | int(b)
		}
		if referenceDY > -2 && referenceDY < (grr.ReferenceBitmap.Height-1) {
			b = 0
			if inRef {
				b, err = grr.ReferenceBitmap.GetByte(refByteIndex + refRowStride + refOffsetNext)
				if err != nil {
					return err
				}
			}
			refRowBelow = (refRowBelow << 8) | int(b)
		}
		for j := 0; j < minorWidth; j++ {
			var bit int
			typicalBits := (typicalContext >> 4) & 0x1ff
			switch typicalBits {
			case 0x1ff:
				bit = 1
			case 0x00:
				bit = 0
			default:
				grr.cx.SetIndex(int32(context))
				bit, err = grr.arithDecoder.DecodeBit(grr.cx)
				if err != nil {
					return err
				}
			}
			shift := uint(7 - j)
			result |= bit << shift
			context = ((context & 0x0d6) << 1) | bit | (regionRow>>shift+5)&0x002 | ((refRowBelow>>shift + 2) & 0x010) | ((refRow >> shift) & 0x040) | ((refRowAbove >> shift) & 0x200)
			typicalContext = ((typicalContext & 0xdb) << 1) | ((refRowBelow>>shift + 2) & 0x010) | ((refRow >> shift) & 0x080) | ((refRowAbove >> shift) & 0x400)
		}
		err = grr.RegionBitmap.SetByte(byteIndex, byte(result))
		if err != nil {
			return err
		}
		byteIndex++
		refByteIndex++
	}
	return nil
}

type EncodeInitializer interface{ InitEncode() }

func (tr *TextRegion) decodeRdh() (int64, error) {
	const processName = "decodeRdh"
	if tr.IsHuffmanEncoded {
		if tr.SbHuffRDHeight == 3 {
			if tr.huffRDH == nil {
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
				if tr.SbHuffRDWidth == 3 {
					userTableIndex++
				}
				tr.huffRDH, err = tr.getUserTable(userTableIndex)
				if err != nil {
					return 0, jbig2errors.Wrap(err, processName, "")
				}
			}
			return tr.huffRDH.Decode(tr.reader)
		}
		standardTable, err := huffman.GetStandardTable(14 + int(tr.SbHuffRDHeight))
		if err != nil {
			return 0, jbig2errors.Wrap(err, processName, "")
		}
		return standardTable.Decode(tr.reader)
	}
	rdh, err := tr.arithDecoder.DecodeInt(tr.cxIARDH)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "")
	}
	return int64(rdh), nil
}

func (tr *TextRegion) decodeIb(ri, id int64) (*bitmap.Bitmap, error) {
	const processName = "decodeIb"
	var (
		err error
		bm  *bitmap.Bitmap
	)
	if ri == 0 {
		if int(id) > len(tr.Symbols)-1 {
			return nil, jbig2errors.Error(processName, "decoding IB bitmap. index out of range")
		}
		return tr.Symbols[int(id)], nil
	}
	var rdw, rdh, rdx, rdy int64
	rdw, err = tr.decodeRdw()
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	rdh, err = tr.decodeRdh()
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	rdx, err = tr.decodeRdx()
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	rdy, err = tr.decodeRdy()
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	if tr.IsHuffmanEncoded {
		if _, err = tr.decodeSymInRefSize(); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "")
		}
		tr.reader.Align()
	}
	symbol := tr.Symbols[id]
	width := uint32(symbol.Width)
	height := uint32(symbol.Height)
	grReferenceDX := int32(uint32(rdw)>>1) + int32(rdx)
	grReferenceDY := int32(uint32(rdh)>>1) + int32(rdy)
	if tr.refinementRegion == nil {
		tr.refinementRegion = newGenericRefinementRegion(tr.reader, nil)
	}
	tr.refinementRegion.setParameters(tr.cxRefinement, tr.arithDecoder, tr.SbrTemplate, width+uint32(rdw), height+uint32(rdh), symbol, grReferenceDX, grReferenceDY, false, tr.SbrATX, tr.SbrATY)
	bm, err = tr.refinementRegion.GetRegionBitmap()
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "grf")
	}
	if tr.IsHuffmanEncoded {
		tr.reader.Align()
	}
	return bm, nil
}

func (sd *SymbolDictionary) setRetainedCodingContexts(src *SymbolDictionary) {
	sd.arithDecoder = src.arithDecoder
	sd.IsHuffmanEncoded = src.IsHuffmanEncoded
	sd.UseRefinementAggregation = src.UseRefinementAggregation
	sd.SdTemplate = src.SdTemplate
	sd.SdrTemplate = src.SdrTemplate
	sd.SdATX = src.SdATX
	sd.SdATY = src.SdATY
	sd.SdrATX = src.SdrATX
	sd.SdrATY = src.SdrATY
	sd.cx = src.cx
}

func (gr *GenericRegion) decodeSLTP() (int, error) {
	switch gr.GBTemplate {
	case 0:
		gr.cx.SetIndex(0x9B25)
	case 1:
		gr.cx.SetIndex(0x795)
	case 2:
		gr.cx.SetIndex(0xE5)
	case 3:
		gr.cx.SetIndex(0x195)
	}
	return gr.arithDecoder.DecodeBit(gr.cx)
}

func highestBit(n int) int {
	if n == 0 {
		return 0
	}
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	return (n + 1) >> 1
}

func (grr *GenericRefinementRegion) decodeSLTP() (int, error) {
	grr.Template.setIndex(grr.cx)
	return grr.arithDecoder.DecodeBit(grr.cx)
}

func (pd *PatternDictionary) GetDictionary() ([]*bitmap.Bitmap, error) {
	if pd.Patterns != nil {
		return pd.Patterns, nil
	}
	if !pd.IsMMREncoded {
		pd.setGbAtPixels()
	}
	genericRegion := NewGenericRegion(pd.reader)
	genericRegion.setParametersMMR(pd.IsMMREncoded, pd.DataOffset, pd.DataLength, uint32(pd.HdpHeight), (pd.GrayMax+1)*uint32(pd.HdpWidth), pd.HDTemplate, false, false, pd.GBAtX, pd.GBAtY)
	regionBitmap, err := genericRegion.GetRegionBitmap()
	if err != nil {
		return nil, err
	}
	if err = pd.extractPatterns(regionBitmap); err != nil {
		return nil, err
	}
	return pd.Patterns, nil
}

func (sd *SymbolDictionary) decodeHeightClassCollectiveBitmap(bmSize int64, height, width uint32) (*bitmap.Bitmap, error) {
	if bmSize == 0 {
		bm, err := bitmap.New(int(width), int(height))
		if err != nil {
			return nil, err
		}
		var b byte
		for i := 0; i < len(bm.Data); i++ {
			b, err = sd.reader.ReadByte()
			if err != nil {
				return nil, err
			}
			if err = bm.SetByte(i, b); err != nil {
				return nil, err
			}
		}
		return bm, nil
	}
	if sd.genericRegion == nil {
		sd.genericRegion = NewGenericRegion(sd.reader)
	}
	sd.genericRegion.setParameters(true, sd.reader.AbsolutePosition(), bmSize, height, width)
	regionBitmap, err := sd.genericRegion.GetRegionBitmap()
	if err != nil {
		return nil, err
	}
	return regionBitmap, nil
}

func (grr *GenericRefinementRegion) GetRegionBitmap() (*bitmap.Bitmap, error) {
	var err error
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("[GENERIC-REF-REGION] GetRegionBitmap begins...")
	}
	defer func() {
		if err != nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[GENERIC-REF-REGION] GetRegionBitmap failed. %v", err)
			}
		} else {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[GENERIC-REF-REGION] GetRegionBitmap finished.")
			}
		}
	}()
	if grr.RegionBitmap != nil {
		return grr.RegionBitmap, nil
	}
	ltp := 0
	if grr.ReferenceBitmap == nil {
		grr.ReferenceBitmap, err = grr.getGrReference()
		if err != nil {
			return nil, err
		}
	}
	if grr.arithDecoder == nil {
		grr.arithDecoder, err = arithmeticdecoder.New(grr.reader)
		if err != nil {
			return nil, err
		}
	}
	if grr.cx == nil {
		grr.cx = arithmeticdecoder.NewStats(8192, 1)
	}
	grr.RegionBitmap, err = bitmap.New(int(grr.RegionInfo.BitmapWidth), int(grr.RegionInfo.BitmapHeight))
	if err != nil {
		return nil, err
	}
	if grr.TemplateID == 0 {
		if err = grr.updateOverride(); err != nil {
			return nil, err
		}
	}
	width := (grr.RegionBitmap.Width + 7) & -8
	var refOffset int
	if grr.IsTPGROn {
		refOffset = int(-grr.ReferenceDY) * grr.ReferenceBitmap.RowStride
	}
	refOffsetNext := refOffset + 1
	for i := 0; i < grr.RegionBitmap.Height; i++ {
		if grr.IsTPGROn {
			sltp, err := grr.decodeSLTP()
			if err != nil {
				return nil, err
			}
			ltp ^= sltp
		}
		if ltp == 0 {
			err = grr.decodeOptimized(i, grr.RegionBitmap.Width, grr.RegionBitmap.RowStride, grr.ReferenceBitmap.RowStride, width, refOffset, refOffsetNext)
			if err != nil {
				return nil, err
			}
		} else {
			err = grr.decodeTypicalPredictedLine(i, grr.RegionBitmap.Width, grr.RegionBitmap.RowStride, grr.ReferenceBitmap.RowStride, width, refOffset)
			if err != nil {
				return nil, err
			}
		}
	}
	return grr.RegionBitmap, nil
}

func (pis *PageInformationSegment) readDefaultPixelValue() error {
	bit, err := pis.reader.ReadBit()
	if err != nil {
		return err
	}
	pis.DefaultPixelValue = uint8(bit & 0xf)
	return nil
}

func (sd *SymbolDictionary) String() string {
	builder := &strings.Builder{}
	builder.WriteString("\n[SYMBOL-DICTIONARY]\n")
	builder.WriteString(fmt.Sprintf("\t- SdrTemplate %v\n", sd.SdrTemplate))
	builder.WriteString(fmt.Sprintf("\t- SdTemplate %v\n", sd.SdTemplate))
	builder.WriteString(fmt.Sprintf("\t- isCodingContextRetained %v\n", sd.isCodingContextRetained))
	builder.WriteString(fmt.Sprintf("\t- isCodingContextUsed %v\n", sd.isCodingContextUsed))
	builder.WriteString(fmt.Sprintf("\t- SdHuffAggInstanceSelection %v\n", sd.SdHuffAggInstanceSelection))
	builder.WriteString(fmt.Sprintf("\t- SdHuffBMSizeSelection %v\n", sd.SdHuffBMSizeSelection))
	builder.WriteString(fmt.Sprintf("\t- SdHuffDecodeWidthSelection %v\n", sd.SdHuffDecodeWidthSelection))
	builder.WriteString(fmt.Sprintf("\t- SdHuffDecodeHeightSelection %v\n", sd.SdHuffDecodeHeightSelection))
	builder.WriteString(fmt.Sprintf("\t- UseRefinementAggregation %v\n", sd.UseRefinementAggregation))
	builder.WriteString(fmt.Sprintf("\t- isHuffmanEncoded %v\n", sd.IsHuffmanEncoded))
	builder.WriteString(fmt.Sprintf("\t- SdATX %v\n", sd.SdATX))
	builder.WriteString(fmt.Sprintf("\t- SdATY %v\n", sd.SdATY))
	builder.WriteString(fmt.Sprintf("\t- SdrATX %v\n", sd.SdrATX))
	builder.WriteString(fmt.Sprintf("\t- SdrATY %v\n", sd.SdrATY))
	builder.WriteString(fmt.Sprintf("\t- NumberOfExportedSymbols %v\n", sd.NumberOfExportedSymbols))
	builder.WriteString(fmt.Sprintf("\t- NumberOfNewSymbols %v\n", sd.NumberOfNewSymbols))
	builder.WriteString(fmt.Sprintf("\t- numberOfImportedSymbols %v\n", sd.numberOfImportedSymbols))
	builder.WriteString(fmt.Sprintf("\t- numberOfDecodedSymbols %v\n", sd.numberOfDecodedSymbols))
	return builder.String()
}

type PatternDictionary struct {
	reader           *bitwise.Reader
	DataHeaderOffset int64
	DataHeaderLength int64
	DataOffset       int64
	DataLength       int64
	GBAtX            []int8
	GBAtY            []int8
	IsMMREncoded     bool
	HDTemplate       byte
	HdpWidth         byte
	HdpHeight        byte
	Patterns         []*bitmap.Bitmap
	GrayMax          uint32
}

func (tr *TextRegion) InitEncode(globalSymbolsMap, localSymbolsMap map[int]int, comps []int, inLL *bitmap.Points, symbols *bitmap.Bitmaps, classIDs *basic.IntSlice, boxes *bitmap.Boxes, width, height, symBits int) {
	tr.RegionInfo = &RegionSegment{BitmapWidth: uint32(width), BitmapHeight: uint32(height)}
	tr.globalSymbolsMap = globalSymbolsMap
	tr.localSymbolsMap = localSymbolsMap
	tr.comps = comps
	tr.inLL = inLL
	tr.symbolBitmaps = symbols
	tr.classIDs = classIDs
	tr.boxes = boxes
	tr.symBits = symBits
}

func (gr *GenericRegion) writeGBAtPixels(bw bitwise.BinaryWriter) (n int, err error) {
	const processName = "writeGBAtPixels"
	if gr.UseMMR {
		return 0, nil
	}
	count := 1
	if gr.GBTemplate == 0 {
		count = 4
	} else if gr.UseExtTemplates {
		count = 12
	}
	if len(gr.GBAtX) != count {
		return 0, jbig2errors.Errorf(processName, "gb at pair number doesn't match to GBAtX slice len")
	}
	if len(gr.GBAtY) != count {
		return 0, jbig2errors.Errorf(processName, "gb at pair number doesn't match to GBAtY slice len")
	}
	for i := 0; i < count; i++ {
		if err = bw.WriteByte(byte(gr.GBAtX[i])); err != nil {
			return n, jbig2errors.Wrap(err, processName, "write GBAtX")
		}
		n++
		if err = bw.WriteByte(byte(gr.GBAtY[i])); err != nil {
			return n, jbig2errors.Wrap(err, processName, "write GBAtY")
		}
		n++
	}
	return n, nil
}

func (gr *GenericRegion) setParametersMMR(isMMREncoded bool, dataOffset, dataLength int64, bitmapHeight, bitmapWidth uint32, gbTemplate byte, isTPGDon, useSkip bool, gbAtX, gbAtY []int8) {
	gr.DataOffset = dataOffset
	gr.DataLength = dataLength
	gr.RegionSegment = &RegionSegment{}
	gr.RegionSegment.BitmapHeight = bitmapHeight
	gr.RegionSegment.BitmapWidth = bitmapWidth
	gr.GBTemplate = gbTemplate
	gr.IsMMREncoded = isMMREncoded
	gr.IsTPGDon = isTPGDon
	gr.GBAtX = gbAtX
	gr.GBAtY = gbAtY
}

func (gr *GenericRegion) overrideAtTemplate2(context, x, y, result, bitIndex int) int {
	context &= 0x3FB
	if gr.GBAtY[0] == 0 && gr.GBAtX[0] >= -int8(bitIndex) {
		context |= (result >> uint(7-(int8(bitIndex)+gr.GBAtX[0])) & 0x1) << 2
	} else {
		context |= int(gr.getPixel(x+int(gr.GBAtX[0]), y+int(gr.GBAtY[0]))) << 2
	}
	return context
}
func (ts *TableSegment) HtLow() int32 { return ts.htLow }
func (hr *HalftoneRegion) computeGrayScalePlanes(bms []*bitmap.Bitmap, bitsPerValue int) ([][]int, error) {
	grayScale := make([][]int, hr.HGridHeight)
	for i := 0; i < len(grayScale); i++ {
		grayScale[i] = make([]int, hr.HGridWidth)
	}
	for i := 0; i < int(hr.HGridHeight); i++ {
		for j := 0; j < int(hr.HGridWidth); j += 8 {
			var colCount int
			if remaining := int(hr.HGridWidth) - j; remaining > 8 {
				colCount = 8
			} else {
				colCount = remaining
			}
			byteIndex := bms[0].GetByteIndex(j, i)
			for k := 0; k < colCount; k++ {
				col := k + j
				grayScale[i][col] = 0
				for l := 0; l < bitsPerValue; l++ {
					b, err := bms[l].GetByte(byteIndex)
					if err != nil {
						return nil, err
					}
					shifted := b >> uint(7-col&7)
					bit := shifted & 1
					weight := 1 << uint(l)
					value := int(bit) * weight
					grayScale[i][col] += value
				}
			}
		}
	}
	return grayScale, nil
}

func (gr *GenericRegion) updateOverrideFlags() error {
	const processName = "updateOverrideFlags"
	if gr.GBAtX == nil || gr.GBAtY == nil {
		return nil
	}
	if len(gr.GBAtX) != len(gr.GBAtY) {
		return jbig2errors.Errorf(processName, "incosistent AT pixel. Amount of 'x' pixels: %d, Amount of 'y' pixels: %d", len(gr.GBAtX), len(gr.GBAtY))
	}
	gr.GBAtOverride = make([]bool, len(gr.GBAtX))
	switch gr.GBTemplate {
	case 0:
		if !gr.UseExtTemplates {
			if gr.GBAtX[0] != 3 || gr.GBAtY[0] != -1 {
				gr.setOverrideFlag(0)
			}
			if gr.GBAtX[1] != -3 || gr.GBAtY[1] != -1 {
				gr.setOverrideFlag(1)
			}
			if gr.GBAtX[2] != 2 || gr.GBAtY[2] != -2 {
				gr.setOverrideFlag(2)
			}
			if gr.GBAtX[3] != -2 || gr.GBAtY[3] != -2 {
				gr.setOverrideFlag(3)
			}
		} else {
			if gr.GBAtX[0] != -2 || gr.GBAtY[0] != 0 {
				gr.setOverrideFlag(0)
			}
			if gr.GBAtX[1] != 0 || gr.GBAtY[1] != -2 {
				gr.setOverrideFlag(1)
			}
			if gr.GBAtX[2] != -2 || gr.GBAtY[2] != -1 {
				gr.setOverrideFlag(2)
			}
			if gr.GBAtX[3] != -1 || gr.GBAtY[3] != -2 {
				gr.setOverrideFlag(3)
			}
			if gr.GBAtX[4] != 1 || gr.GBAtY[4] != -2 {
				gr.setOverrideFlag(4)
			}
			if gr.GBAtX[5] != 2 || gr.GBAtY[5] != -1 {
				gr.setOverrideFlag(5)
			}
			if gr.GBAtX[6] != -3 || gr.GBAtY[6] != 0 {
				gr.setOverrideFlag(6)
			}
			if gr.GBAtX[7] != -4 || gr.GBAtY[7] != 0 {
				gr.setOverrideFlag(7)
			}
			if gr.GBAtX[8] != 2 || gr.GBAtY[8] != -2 {
				gr.setOverrideFlag(8)
			}
			if gr.GBAtX[9] != 3 || gr.GBAtY[9] != -1 {
				gr.setOverrideFlag(9)
			}
			if gr.GBAtX[10] != -2 || gr.GBAtY[10] != -2 {
				gr.setOverrideFlag(10)
			}
			if gr.GBAtX[11] != -3 || gr.GBAtY[11] != -1 {
				gr.setOverrideFlag(11)
			}
		}
	case 1:
		if gr.GBAtX[0] != 3 || gr.GBAtY[0] != -1 {
			gr.setOverrideFlag(0)
		}
	case 2:
		if gr.GBAtX[0] != 2 || gr.GBAtY[0] != -1 {
			gr.setOverrideFlag(0)
		}
	case 3:
		if gr.GBAtX[0] != 2 || gr.GBAtY[0] != -1 {
			gr.setOverrideFlag(0)
		}
	}
	return nil
}

func (t Type) String() string {
	switch t {
	case TSymbolDictionary:
		return "Symbol Dictionary"
	case TIntermediateTextRegion:
		return "Intermediate Text Region"
	case TImmediateTextRegion:
		return "Immediate Text Region"
	case TImmediateLosslessTextRegion:
		return "Immediate Lossless Text Region"
	case TPatternDictionary:
		return "Pattern Dictionary"
	case TIntermediateHalftoneRegion:
		return "Intermediate Halftone Region"
	case TImmediateHalftoneRegion:
		return "Immediate Halftone Region"
	case TImmediateLosslessHalftoneRegion:
		return "Immediate Lossless Halftone Region"
	case TIntermediateGenericRegion:
		return "Intermediate Generic Region"
	case TImmediateGenericRegion:
		return "Immediate Generic Region"
	case TImmediateLosslessGenericRegion:
		return "Immediate Lossless Generic Region"
	case TIntermediateGenericRefinementRegion:
		return "Intermediate Generic Refinement Region"
	case TImmediateGenericRefinementRegion:
		return "Immediate Generic Refinement Region"
	case TImmediateLosslessGenericRefinementRegion:
		return "Immediate Lossless Generic Refinement Region"
	case TPageInformation:
		return "Page Information"
	case TEndOfPage:
		return "End Of Page"
	case TEndOfStrip:
		return "End Of Strip"
	case TEndOfFile:
		return "End Of File"
	case TProfiles:
		return "Profiles"
	case TTables:
		return "Tables"
	case TExtension:
		return "Extension"
	case TBitmap:
		return "Bitmap"
	}
	return "Invalid Segment Kind"
}
func NewRegionSegment(r *bitwise.Reader) *RegionSegment { return &RegionSegment{reader: r} }
func (eos *EndOfStripe) LineNumber() int                { return eos.lineNumber }
func (sd *SymbolDictionary) GetDictionary() ([]*bitmap.Bitmap, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("[SYMBOL-DICTIONARY] GetDictionary begins...")
	}
	defer func() {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("[SYMBOL-DICTIONARY] GetDictionary finished")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("[SYMBOL-DICTIONARY] Dictionary. \nEx: '%s', \nnew:'%s'", sd.exportedSymbols, sd.newSymbols)
		}
	}()
	if sd.exportedSymbols == nil {
		var err error
		if sd.UseRefinementAggregation {
			sd.symbolCodeLength = sd.getSbSymCodeLen()
		}
		if !sd.IsHuffmanEncoded {
			if err = sd.setCodingStatistics(); err != nil {
				return nil, err
			}
		}
		sd.newSymbols = make([]*bitmap.Bitmap, sd.NumberOfNewSymbols)
		var newSymbolWidths []int
		if sd.IsHuffmanEncoded && !sd.UseRefinementAggregation {
			newSymbolWidths = make([]int, sd.NumberOfNewSymbols)
		}
		if err = sd.setSymbolsArray(); err != nil {
			return nil, err
		}
		var hcHeight, hcDeltaHeight int64
		sd.numberOfDecodedSymbols = 0
		for sd.numberOfDecodedSymbols < sd.NumberOfNewSymbols {
			hcDeltaHeight, err = sd.decodeHeightClassDeltaHeight()
			if err != nil {
				return nil, err
			}
			hcHeight += hcDeltaHeight
			var symWidth, totalWidth uint32
			hcFirstSymbol := int64(sd.numberOfDecodedSymbols)
			for {
				var deltaWidth int64
				deltaWidth, err = sd.decodeDifferenceWidth()
				if errors.Is(err, jbig2internal.ErrOOB) {
					break
				}
				if err != nil {
					return nil, err
				}
				if sd.numberOfDecodedSymbols >= sd.NumberOfNewSymbols {
					break
				}
				symWidth += uint32(deltaWidth)
				totalWidth += symWidth
				if !sd.IsHuffmanEncoded || sd.UseRefinementAggregation {
					if !sd.UseRefinementAggregation {
						err = sd.decodeDirectlyThroughGenericRegion(symWidth, uint32(hcHeight))
						if err != nil {
							return nil, err
						}
					} else {
						err = sd.decodeAggregate(symWidth, uint32(hcHeight))
						if err != nil {
							return nil, err
						}
					}
				} else if sd.IsHuffmanEncoded && !sd.UseRefinementAggregation {
					newSymbolWidths[sd.numberOfDecodedSymbols] = int(symWidth)
				}
				sd.numberOfDecodedSymbols++
			}
			if sd.IsHuffmanEncoded && !sd.UseRefinementAggregation {
				var bmSize int64
				if sd.SdHuffBMSizeSelection == 0 {
					var table huffman.Tabler
					table, err = huffman.GetStandardTable(1)
					if err != nil {
						return nil, err
					}
					bmSize, err = table.Decode(sd.reader)
					if err != nil {
						return nil, err
					}
				} else {
					bmSize, err = sd.huffDecodeBmSize()
					if err != nil {
						return nil, err
					}
				}
				sd.reader.Align()
				var bm *bitmap.Bitmap
				bm, err = sd.decodeHeightClassCollectiveBitmap(bmSize, uint32(hcHeight), totalWidth)
				if err != nil {
					return nil, err
				}
				err = sd.decodeHeightClassBitmap(bm, hcFirstSymbol, int(hcHeight), newSymbolWidths)
				if err != nil {
					return nil, err
				}
			}
		}
		toExportFlags, err := sd.getToExportFlags()
		if err != nil {
			return nil, err
		}
		sd.setExportedSymbols(toExportFlags)
	}
	return sd.exportedSymbols, nil
}

func (sd *SymbolDictionary) readNumberOfExportedSymbols() error {
	count, err := sd.reader.ReadBits(32)
	if err != nil {
		return err
	}
	sd.NumberOfExportedSymbols = uint32(count & math.MaxUint32)
	return nil
}

var _ templater = &template0{}

func (gr *GenericRegion) copyLineAbove(lineNumber int) error {
	dstIndex := lineNumber * gr.Bitmap.RowStride
	srcIndex := dstIndex - gr.Bitmap.RowStride
	for i := 0; i < gr.Bitmap.RowStride; i++ {
		b, err := gr.Bitmap.GetByte(srcIndex)
		if err != nil {
			return err
		}
		srcIndex++
		if err = gr.Bitmap.SetByte(dstIndex, b); err != nil {
			return err
		}
		dstIndex++
	}
	return nil
}

func NewHeader(d Documenter, r *bitwise.Reader, offset int64, organizationType OrganizationType) (*Header, error) {
	header := &Header{Reader: r}
	if err := header.parse(d, r, offset, organizationType); err != nil {
		return nil, jbig2errors.Wrap(err, "NewHeader", "")
	}
	return header, nil
}

func (grr *GenericRefinementRegion) updateOverride() error {
	if grr.GrAtX == nil || grr.GrAtY == nil {
		return errors.New("AT pixels not set")
	}
	if len(grr.GrAtX) != len(grr.GrAtY) {
		return errors.New("AT pixel inconsistent")
	}
	grr.atOverride = make([]bool, len(grr.GrAtX))
	switch grr.TemplateID {
	case 0:
		if grr.GrAtX[0] != -1 && grr.GrAtY[0] != -1 {
			grr.atOverride[0] = true
			grr.hasAtOverride = true
		}
		if grr.GrAtX[1] != -1 && grr.GrAtY[1] != -1 {
			grr.atOverride[1] = true
			grr.hasAtOverride = true
		}
	case 1:
		grr.hasAtOverride = false
	}
	return nil
}
func (gr *GenericRegion) GetRegionInfo() *RegionSegment { return gr.RegionSegment }
func (sd *SymbolDictionary) encodeRefinementATFlags(bw bitwise.BinaryWriter) (n int, err error) {
	const processName = "encodeRefinementATFlags"
	if !sd.UseRefinementAggregation || sd.SdrTemplate != 0 {
		return 0, nil
	}
	for i := 0; i < 2; i++ {
		if err = bw.WriteByte(byte(sd.SdrATX[i])); err != nil {
			return n, jbig2errors.Wrapf(err, processName, "SdrATX[%d]", i)
		}
		n++
		if err = bw.WriteByte(byte(sd.SdrATY[i])); err != nil {
			return n, jbig2errors.Wrapf(err, processName, "SdrATY[%d]", i)
		}
		n++
	}
	return n, nil
}

func (gr *GenericRegion) setParametersWithAt(isMMREncoded bool, gbTemplate byte, isTPGDon, useSkip bool, gbAtX, gbAtY []int8, bitmapWidth, bitmapHeight uint32, cx *arithmeticdecoder.DecoderStats, arithDecoder *arithmeticdecoder.Decoder) {
	gr.IsMMREncoded = isMMREncoded
	gr.GBTemplate = gbTemplate
	gr.IsTPGDon = isTPGDon
	gr.GBAtX = gbAtX
	gr.GBAtY = gbAtY
	gr.RegionSegment.BitmapHeight = bitmapHeight
	gr.RegionSegment.BitmapWidth = bitmapWidth
	gr.mmrDecoder = nil
	gr.Bitmap = nil
	if cx != nil {
		gr.cx = cx
	}
	if arithDecoder != nil {
		gr.arithDecoder = arithDecoder
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("[GENERIC-REGION] setParameters SDAt: %s", gr)
	}
}

func (sd *SymbolDictionary) setCodingStatistics() error {
	if sd.cxIADT == nil {
		sd.cxIADT = arithmeticdecoder.NewStats(512, 1)
	}
	if sd.cxIADH == nil {
		sd.cxIADH = arithmeticdecoder.NewStats(512, 1)
	}
	if sd.cxIADW == nil {
		sd.cxIADW = arithmeticdecoder.NewStats(512, 1)
	}
	if sd.cxIAAI == nil {
		sd.cxIAAI = arithmeticdecoder.NewStats(512, 1)
	}
	if sd.cxIAEX == nil {
		sd.cxIAEX = arithmeticdecoder.NewStats(512, 1)
	}
	if sd.UseRefinementAggregation && sd.cxIAID == nil {
		sd.cxIAID = arithmeticdecoder.NewStats(1<<uint(sd.symbolCodeLength), 1)
		sd.cxIARDX = arithmeticdecoder.NewStats(512, 1)
		sd.cxIARDY = arithmeticdecoder.NewStats(512, 1)
	}
	if sd.cx == nil {
		sd.cx = arithmeticdecoder.NewStats(65536, 1)
	}
	if sd.arithDecoder == nil {
		var err error
		sd.arithDecoder, err = arithmeticdecoder.New(sd.reader)
		if err != nil {
			return err
		}
	}
	return nil
}

func (h *Header) readHeaderLength(r *bitwise.Reader, startPos int64) {
	h.HeaderLength = r.AbsolutePosition() - startPos
}

func (sd *SymbolDictionary) readNumberOfNewSymbols() error {
	count, err := sd.reader.ReadBits(32)
	if err != nil {
		return err
	}
	sd.NumberOfNewSymbols = uint32(count & math.MaxUint32)
	return nil
}

func (gr *GenericRegion) overrideAtTemplate0b(context, x, y, result, bitIndex, shift int) int {
	if gr.GBAtOverride[0] {
		context &= 0xFFFD
		if gr.GBAtY[0] == 0 && gr.GBAtX[0] >= -int8(bitIndex) {
			context |= (result >> uint(int8(shift)-gr.GBAtX[0]&0x1)) << 1
		} else {
			context |= int(gr.getPixel(x+int(gr.GBAtX[0]), y+int(gr.GBAtY[0]))) << 1
		}
	}
	if gr.GBAtOverride[1] {
		context &= 0xDFFF
		if gr.GBAtY[1] == 0 && gr.GBAtX[1] >= -int8(bitIndex) {
			context |= (result >> uint(int8(shift)-gr.GBAtX[1]&0x1)) << 13
		} else {
			context |= int(gr.getPixel(x+int(gr.GBAtX[1]), y+int(gr.GBAtY[1]))) << 13
		}
	}
	if gr.GBAtOverride[2] {
		context &= 0xFDFF
		if gr.GBAtY[2] == 0 && gr.GBAtX[2] >= -int8(bitIndex) {
			context |= (result >> uint(int8(shift)-gr.GBAtX[2]&0x1)) << 9
		} else {
			context |= int(gr.getPixel(x+int(gr.GBAtX[2]), y+int(gr.GBAtY[2]))) << 9
		}
	}
	if gr.GBAtOverride[3] {
		context &= 0xBFFF
		if gr.GBAtY[3] == 0 && gr.GBAtX[3] >= -int8(bitIndex) {
			context |= (result >> uint(int8(shift)-gr.GBAtX[3]&0x1)) << 14
		} else {
			context |= int(gr.getPixel(x+int(gr.GBAtX[3]), y+int(gr.GBAtY[3]))) << 14
		}
	}
	if gr.GBAtOverride[4] {
		context &= 0xEFFF
		if gr.GBAtY[4] == 0 && gr.GBAtX[4] >= -int8(bitIndex) {
			context |= (result >> uint(int8(shift)-gr.GBAtX[4]&0x1)) << 12
		} else {
			context |= int(gr.getPixel(x+int(gr.GBAtX[4]), y+int(gr.GBAtY[4]))) << 12
		}
	}
	if gr.GBAtOverride[5] {
		context &= 0xFFDF
		if gr.GBAtY[5] == 0 && gr.GBAtX[5] >= -int8(bitIndex) {
			context |= (result >> uint(int8(shift)-gr.GBAtX[5]&0x1)) << 5
		} else {
			context |= int(gr.getPixel(x+int(gr.GBAtX[5]), y+int(gr.GBAtY[5]))) << 5
		}
	}
	if gr.GBAtOverride[6] {
		context &= 0xFFFB
		if gr.GBAtY[6] == 0 && gr.GBAtX[6] >= -int8(bitIndex) {
			context |= (result >> uint(int8(shift)-gr.GBAtX[6]&0x1)) << 2
		} else {
			context |= int(gr.getPixel(x+int(gr.GBAtX[6]), y+int(gr.GBAtY[6]))) << 2
		}
	}
	if gr.GBAtOverride[7] {
		context &= 0xFFF7
		if gr.GBAtY[7] == 0 && gr.GBAtX[7] >= -int8(bitIndex) {
			context |= (result >> uint(int8(shift)-gr.GBAtX[7]&0x1)) << 3
		} else {
			context |= int(gr.getPixel(x+int(gr.GBAtX[7]), y+int(gr.GBAtY[7]))) << 3
		}
	}
	if gr.GBAtOverride[8] {
		context &= 0xF7FF
		if gr.GBAtY[8] == 0 && gr.GBAtX[8] >= -int8(bitIndex) {
			context |= (result >> uint(int8(shift)-gr.GBAtX[8]&0x1)) << 11
		} else {
			context |= int(gr.getPixel(x+int(gr.GBAtX[8]), y+int(gr.GBAtY[8]))) << 11
		}
	}
	if gr.GBAtOverride[9] {
		context &= 0xFFEF
		if gr.GBAtY[9] == 0 && gr.GBAtX[9] >= -int8(bitIndex) {
			context |= (result >> uint(int8(shift)-gr.GBAtX[9]&0x1)) << 4
		} else {
			context |= int(gr.getPixel(x+int(gr.GBAtX[9]), y+int(gr.GBAtY[9]))) << 4
		}
	}
	if gr.GBAtOverride[10] {
		context &= 0x7FFF
		if gr.GBAtY[10] == 0 && gr.GBAtX[10] >= -int8(bitIndex) {
			context |= (result >> uint(int8(shift)-gr.GBAtX[10]&0x1)) << 15
		} else {
			context |= int(gr.getPixel(x+int(gr.GBAtX[10]), y+int(gr.GBAtY[10]))) << 15
		}
	}
	if gr.GBAtOverride[11] {
		context &= 0xFDFF
		if gr.GBAtY[11] == 0 && gr.GBAtX[11] >= -int8(bitIndex) {
			context |= (result >> uint(int8(shift)-gr.GBAtX[11]&0x1)) << 10
		} else {
			context |= int(gr.getPixel(x+int(gr.GBAtX[11]), y+int(gr.GBAtY[11]))) << 10
		}
	}
	return context
}

type Segmenter interface {
	Init(h *Header, r *bitwise.Reader) error
}

func (pis *PageInformationSegment) readIsLossless() error {
	bit, err := pis.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		pis.IsLossless = true
	}
	return nil
}

func (h *Header) readDataStartOffset(r *bitwise.Reader, ot OrganizationType) {
	if ot == OSequential {
		h.SegmentDataStartOffset = uint64(r.AbsolutePosition())
	}
}

func (tr *TextRegion) blit(bm *bitmap.Bitmap, curT int64) error {
	if tr.IsTransposed == 0 && (tr.ReferenceCorner == 2 || tr.ReferenceCorner == 3) {
		tr.currentS += int64(bm.Width - 1)
	} else if tr.IsTransposed == 1 && (tr.ReferenceCorner == 0 || tr.ReferenceCorner == 2) {
		tr.currentS += int64(bm.Height - 1)
	}
	currentS := tr.currentS
	if tr.IsTransposed == 1 {
		currentS, curT = curT, currentS
	}
	switch tr.ReferenceCorner {
	case 0:
		curT -= int64(bm.Height - 1)
	case 2:
		curT -= int64(bm.Height - 1)
		currentS -= int64(bm.Width - 1)
	case 3:
		currentS -= int64(bm.Width - 1)
	}
	err := bitmap.Blit(bm, tr.RegionBitmap, int(currentS), int(curT), tr.CombinationOperator)
	if err != nil {
		return err
	}
	if tr.IsTransposed == 0 && (tr.ReferenceCorner == 0 || tr.ReferenceCorner == 1) {
		tr.currentS += int64(bm.Width - 1)
	}
	if tr.IsTransposed == 1 && (tr.ReferenceCorner == 1 || tr.ReferenceCorner == 3) {
		tr.currentS += int64(bm.Height - 1)
	}
	return nil
}
func (ts *TableSegment) HtPS() int32 { return ts.htPS }
func (h *Header) writeReferredToSegments(bw bitwise.BinaryWriter) (n int, err error) {
	const processName = "writeReferredToSegments"
	var (
		v16 uint16
		v32 uint32
	)
	referenceSize := h.referenceSize()
	written := 1
	buf := make([]byte, referenceSize)
	for _, rtsNumber := range h.RTSNumbers {
		switch referenceSize {
		case 4:
			v32 = uint32(rtsNumber)
			binary.BigEndian.PutUint32(buf, v32)
			written, err = bw.Write(buf)
			if err != nil {
				return 0, jbig2errors.Wrap(err, processName, "uint32 size")
			}
		case 2:
			v16 = uint16(rtsNumber)
			binary.BigEndian.PutUint16(buf, v16)
			written, err = bw.Write(buf)
			if err != nil {
				return 0, jbig2errors.Wrap(err, processName, "uint16")
			}
		default:
			if err = bw.WriteByte(byte(rtsNumber)); err != nil {
				return 0, jbig2errors.Wrap(err, processName, "uint8")
			}
		}
		n += written
	}
	return n, nil
}

var _ templater = &template1{}

type EndOfStripe struct {
	reader     *bitwise.Reader
	lineNumber int
}

func (tr *TextRegion) checkInput() error {
	const processName = "checkInput"
	if !tr.UseRefinement {
		if tr.SbrTemplate != 0 {
			common.Log.Debug("SbrTemplate should be 0")
			tr.SbrTemplate = 0
		}
	}
	if tr.SbHuffFS == 2 || tr.SbHuffRDWidth == 2 || tr.SbHuffRDHeight == 2 || tr.SbHuffRDX == 2 || tr.SbHuffRDY == 2 {
		return jbig2errors.Error(processName, "huffman flag value is not permitted")
	}
	if !tr.UseRefinement {
		if tr.SbHuffRSize != 0 {
			common.Log.Debug("SbHuffRSize should be 0")
			tr.SbHuffRSize = 0
		}
		if tr.SbHuffRDY != 0 {
			common.Log.Debug("SbHuffRDY should be 0")
			tr.SbHuffRDY = 0
		}
		if tr.SbHuffRDX != 0 {
			common.Log.Debug("SbHuffRDX should be 0")
			tr.SbHuffRDX = 0
		}
		if tr.SbHuffRDWidth != 0 {
			common.Log.Debug("SbHuffRDWidth should be 0")
			tr.SbHuffRDWidth = 0
		}
		if tr.SbHuffRDHeight != 0 {
			common.Log.Debug("SbHuffRDHeight should be 0")
			tr.SbHuffRDHeight = 0
		}
	}
	return nil
}

func (pis *PageInformationSegment) Encode(w bitwise.BinaryWriter) (n int, err error) {
	const processName = "PageInformationSegment.Encode"
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(pis.PageBMWidth))
	n, err = w.Write(buf)
	if err != nil {
		return n, jbig2errors.Wrap(err, processName, "width")
	}
	binary.BigEndian.PutUint32(buf, uint32(pis.PageBMHeight))
	var m int
	m, err = w.Write(buf)
	if err != nil {
		return m + n, jbig2errors.Wrap(err, processName, "height")
	}
	n += m
	binary.BigEndian.PutUint32(buf, uint32(pis.ResolutionX))
	m, err = w.Write(buf)
	if err != nil {
		return m + n, jbig2errors.Wrap(err, processName, "x resolution")
	}
	n += m
	binary.BigEndian.PutUint32(buf, uint32(pis.ResolutionY))
	if m, err = w.Write(buf); err != nil {
		return m + n, jbig2errors.Wrap(err, processName, "y resolution")
	}
	n += m
	if err = pis.encodeFlags(w); err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	n++
	if m, err = pis.encodeStripingInformation(w); err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	n += m
	return n, nil
}
func (pis *PageInformationSegment) Size() int { return 19 }
func (sd *SymbolDictionary) setSymbolsArray() error {
	if sd.importedSymbols == nil {
		if err := sd.retrieveImportSymbols(); err != nil {
			return err
		}
	}
	if sd.allSymbols == nil {
		sd.allSymbols = append(sd.allSymbols, sd.importedSymbols...)
	}
	return nil
}

func (h *Header) subInputReader() (*bitwise.Reader, error) {
	segmentDataLength := int(h.SegmentDataLength)
	if h.SegmentDataLength == math.MaxInt32 {
		segmentDataLength = -1
	}
	return h.Reader.NewPartialReader(int(h.SegmentDataStartOffset), segmentDataLength, false)
}

func (sd *SymbolDictionary) readRefinementAtPixels(count int) error {
	sd.SdrATX = make([]int8, count)
	sd.SdrATY = make([]int8, count)
	var (
		b   byte
		err error
	)
	for i := 0; i < count; i++ {
		b, err = sd.reader.ReadByte()
		if err != nil {
			return err
		}
		sd.SdrATX[i] = int8(b)
		b, err = sd.reader.ReadByte()
		if err != nil {
			return err
		}
		sd.SdrATY[i] = int8(b)
	}
	return nil
}

func (sd *SymbolDictionary) encodeSymbols(bw bitwise.BinaryWriter) (n int, err error) {
	const processName = "encodeSymbol"
	e := arithmeticencoder.New()
	e.Init()
	selected, err := sd.symbols.SelectByIndexes(sd.symbolList)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "initial")
	}
	bitmapIndex := map[*bitmap.Bitmap]int{}
	for i, bm := range selected.Values {
		bitmapIndex[bm] = i
	}
	selected.SortByHeight()
	var prevHeight, newIndex int
	groupByHeight, err := selected.GroupByHeight()
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "")
	}
	for _, heightClass := range groupByHeight.Values {
		height := heightClass.Values[0].Height
		deltaHeight := height - prevHeight
		if err = e.EncodeInteger(arithmeticencoder.IADH, deltaHeight); err != nil {
			return 0, jbig2errors.Wrapf(err, processName, "IADH for dh: '%d'", deltaHeight)
		}
		prevHeight = height
		groupByWidth, err := heightClass.GroupByWidth()
		if err != nil {
			return 0, jbig2errors.Wrapf(err, processName, "height: '%d'", height)
		}
		var prevWidth int
		for _, widthClass := range groupByWidth.Values {
			for _, bm := range widthClass.Values {
				width := bm.Width
				deltaWidth := width - prevWidth
				if err = e.EncodeInteger(arithmeticencoder.IADW, deltaWidth); err != nil {
					return 0, jbig2errors.Wrapf(err, processName, "IADW for dw: '%d'", deltaWidth)
				}
				prevWidth += deltaWidth
				if err = e.EncodeBitmap(bm, false); err != nil {
					return 0, jbig2errors.Wrapf(err, processName, "Height: %d Width: %d", height, width)
				}
				origIndex := bitmapIndex[bm]
				sd.symbolMap[origIndex] = newIndex
				newIndex++
			}
		}
		if err = e.EncodeOOB(arithmeticencoder.IADW); err != nil {
			return 0, jbig2errors.Wrap(err, processName, "IADW")
		}
	}
	if err = e.EncodeInteger(arithmeticencoder.IAEX, 0); err != nil {
		return 0, jbig2errors.Wrap(err, processName, "exported symbols")
	}
	if err = e.EncodeInteger(arithmeticencoder.IAEX, len(sd.symbolList)); err != nil {
		return 0, jbig2errors.Wrap(err, processName, "number of symbols")
	}
	e.Final()
	written, err := e.WriteTo(bw)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "writing encoder context to 'w' writer")
	}
	return int(written), nil
}
func (gr *GenericRegion) Size() int { return gr.RegionSegment.Size() + 1 + 2*len(gr.GBAtX) }
func (tr *TextRegion) GetRegionBitmap() (*bitmap.Bitmap, error) {
	if tr.RegionBitmap != nil {
		return tr.RegionBitmap, nil
	}
	if !tr.IsHuffmanEncoded {
		if err := tr.setCodingStatistics(); err != nil {
			return nil, err
		}
	}
	if err := tr.createRegionBitmap(); err != nil {
		return nil, err
	}
	if err := tr.decodeSymbolInstances(); err != nil {
		return nil, err
	}
	return tr.RegionBitmap, nil
}

func (hr *HalftoneRegion) checkInput() error {
	if hr.IsMMREncoded {
		if hr.HTemplate != 0 {
			common.Log.Debug("HTemplate = %d should contain the value 0", hr.HTemplate)
		}
		if hr.HSkipEnabled {
			common.Log.Debug("HSkipEnabled 0 %v (should contain the value false)", hr.HSkipEnabled)
		}
	}
	return nil
}

func (sd *SymbolDictionary) setExportedSymbols(exportFlags []int) {
	for i := uint32(0); i < sd.numberOfImportedSymbols+sd.NumberOfNewSymbols; i++ {
		if exportFlags[i] == 1 {
			var bm *bitmap.Bitmap
			if i < sd.numberOfImportedSymbols {
				bm = sd.importedSymbols[i]
			} else {
				bm = sd.newSymbols[i-sd.numberOfImportedSymbols]
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[SYMBOL-DICTIONARY] Add ExportedSymbol: '%s'", bm)
			}
			sd.exportedSymbols = append(sd.exportedSymbols, bm)
		}
	}
}

func (gr *GenericRegion) parseHeader() (err error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("[GENERIC-REGION] ParsingHeader...")
	}
	defer func() {
		if err != nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[GENERIC-REGION] ParsingHeader Finished with error. %v", err)
			}
		} else {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[GENERIC-REGION] ParsingHeader Finished Successfully...")
			}
		}
	}()
	var (
		bit  int
		bits uint64
	)
	if err = gr.RegionSegment.parseHeader(); err != nil {
		return err
	}
	if _, err = gr.reader.ReadBits(3); err != nil {
		return err
	}
	bit, err = gr.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		gr.UseExtTemplates = true
	}
	bit, err = gr.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		gr.IsTPGDon = true
	}
	bits, err = gr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	gr.GBTemplate = byte(bits & 0xf)
	bit, err = gr.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		gr.IsMMREncoded = true
	}
	if !gr.IsMMREncoded {
		atCount := 1
		if gr.GBTemplate == 0 {
			atCount = 4
			if gr.UseExtTemplates {
				atCount = 12
			}
		}
		if err = gr.readGBAtPixels(atCount); err != nil {
			return err
		}
	}
	if err = gr.computeSegmentDataStructure(); err != nil {
		return err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("%s", gr)
	}
	return nil
}

func (sd *SymbolDictionary) Init(h *Header, r *bitwise.Reader) error {
	sd.Header = h
	sd.reader = r
	return sd.parseHeader()
}

func (tr *TextRegion) decodeCurrentT() (int64, error) {
	if tr.SbStrips != 1 {
		if tr.IsHuffmanEncoded {
			bits, err := tr.reader.ReadBits(byte(tr.LogSBStrips))
			return int64(bits), err
		}
		curT, err := tr.arithDecoder.DecodeInt(tr.cxIAIT)
		if err != nil {
			return 0, err
		}
		return int64(curT), nil
	}
	return 0, nil
}

type templater interface {
	form(refAbove, refMid, refBelow, regionAbove, lastBit int16) int16
	setIndex(ds *arithmeticdecoder.DecoderStats)
}

func (sd *SymbolDictionary) parseHeader() (err error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("[SYMBOL DICTIONARY][PARSE-HEADER] begins...")
	}
	defer func() {
		if err != nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[SYMBOL DICTIONARY][PARSE-HEADER] failed. %v", err)
			}
		} else {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[SYMBOL DICTIONARY][PARSE-HEADER] finished.")
			}
		}
	}()
	if err = sd.readRegionFlags(); err != nil {
		return err
	}
	if err = sd.setAtPixels(); err != nil {
		return err
	}
	if err = sd.setRefinementAtPixels(); err != nil {
		return err
	}
	if err = sd.readNumberOfExportedSymbols(); err != nil {
		return err
	}
	if err = sd.readNumberOfNewSymbols(); err != nil {
		return err
	}
	if err = sd.setInSyms(); err != nil {
		return err
	}
	if sd.isCodingContextUsed {
		rtSegments := sd.Header.RTSegments
		for _, rtSegment := range slices.Backward(rtSegments) {
			if rtSegment.Type == 0 {
				prevSD, ok := rtSegment.SegmentData.(*SymbolDictionary)
				if !ok {
					err = fmt.Errorf("related Segment: %v is not a Symbol Dictionary Segment", rtSegment)
					return err
				}
				if prevSD.isCodingContextUsed {
					sd.setRetainedCodingContexts(prevSD)
				}
				break
			}
		}
	}
	if err = sd.checkInput(); err != nil {
		return err
	}
	return nil
}

type OrganizationType uint8

func (sd *SymbolDictionary) setInSyms() error {
	if sd.Header.RTSegments != nil {
		return sd.retrieveImportSymbols()
	}
	sd.importedSymbols = make([]*bitmap.Bitmap, 0)
	return nil
}

func (tr *TextRegion) readUseRefinement() error {
	if !tr.UseRefinement || tr.SbrTemplate != 0 {
		return nil
	}
	var (
		b   byte
		err error
	)
	tr.SbrATX = make([]int8, 2)
	tr.SbrATY = make([]int8, 2)
	b, err = tr.reader.ReadByte()
	if err != nil {
		return err
	}
	tr.SbrATX[0] = int8(b)
	b, err = tr.reader.ReadByte()
	if err != nil {
		return err
	}
	tr.SbrATY[0] = int8(b)
	b, err = tr.reader.ReadByte()
	if err != nil {
		return err
	}
	tr.SbrATX[1] = int8(b)
	b, err = tr.reader.ReadByte()
	if err != nil {
		return err
	}
	tr.SbrATY[1] = int8(b)
	return nil
}

func (sd *SymbolDictionary) decodeRefinedSymbol(symWidth, hcHeight uint32) error {
	var (
		symbolID int
		rdx, rdy int32
	)
	if sd.IsHuffmanEncoded {
		id, err := sd.reader.ReadBits(byte(sd.symbolCodeLength))
		if err != nil {
			return err
		}
		symbolID = int(id)
		standardTable, err := huffman.GetStandardTable(15)
		if err != nil {
			return err
		}
		rd, err := standardTable.Decode(sd.reader)
		if err != nil {
			return err
		}
		rdx = int32(rd)
		rd, err = standardTable.Decode(sd.reader)
		if err != nil {
			return err
		}
		rdy = int32(rd)
		standardTable, err = huffman.GetStandardTable(1)
		if err != nil {
			return err
		}
		if _, err = standardTable.Decode(sd.reader); err != nil {
			return err
		}
		sd.reader.Align()
	} else {
		iaid, err := sd.arithDecoder.DecodeIAID(uint64(sd.symbolCodeLength), sd.cxIAID)
		if err != nil {
			return err
		}
		symbolID = int(iaid)
		rdx, err = sd.arithDecoder.DecodeInt(sd.cxIARDX)
		if err != nil {
			return err
		}
		rdy, err = sd.arithDecoder.DecodeInt(sd.cxIARDY)
		if err != nil {
			return err
		}
	}
	if err := sd.setSymbolsArray(); err != nil {
		return err
	}
	refSymbol := sd.allSymbols[symbolID]
	if err := sd.decodeNewSymbols(symWidth, hcHeight, refSymbol, rdx, rdy); err != nil {
		return err
	}
	if sd.IsHuffmanEncoded {
		sd.reader.Align()
	}
	return nil
}

func (tr *TextRegion) setParameters(arithDecoder *arithmeticdecoder.Decoder, isHuffmanEncoded, useRefinement bool, bitmapWidth, bitmapHeight uint32, numberOfSymbolInstances uint32, sbStrips int8, numberOfSymbols uint32, defaultPixel int8, combinationOperator bitmap.CombinationOperator, isTransposed int8, referenceCorner int16, sbDsOffset, sbHuffFS, sbHuffDS, sbHuffDT, sbHuffRDWidth, sbHuffRDHeight, sbHuffRDX, sbHuffRDY, sbHuffRSize, sbrTemplate int8, sbrATX, sbrATY []int8, symbols []*bitmap.Bitmap, symbolCodeLength int8) {
	tr.arithDecoder = arithDecoder
	tr.IsHuffmanEncoded = isHuffmanEncoded
	tr.UseRefinement = useRefinement
	tr.RegionInfo.BitmapWidth = bitmapWidth
	tr.RegionInfo.BitmapHeight = bitmapHeight
	tr.NumberOfSymbolInstances = numberOfSymbolInstances
	tr.SbStrips = sbStrips
	tr.NumberOfSymbols = numberOfSymbols
	tr.DefaultPixel = defaultPixel
	tr.CombinationOperator = combinationOperator
	tr.IsTransposed = isTransposed
	tr.ReferenceCorner = referenceCorner
	tr.SbDsOffset = sbDsOffset
	tr.SbHuffFS = sbHuffFS
	tr.SbHuffDS = sbHuffDS
	tr.SbHuffDT = sbHuffDT
	tr.SbHuffRDWidth = sbHuffRDWidth
	tr.SbHuffRDHeight = sbHuffRDHeight
	tr.SbHuffRSize = sbHuffRSize
	tr.SbHuffRDX = sbHuffRDX
	tr.SbHuffRDY = sbHuffRDY
	tr.SbrTemplate = sbrTemplate
	tr.SbrATX = sbrATX
	tr.SbrATY = sbrATY
	tr.Symbols = symbols
	tr.symbolCodeLength = symbolCodeLength
}

func (h *Header) writeSegmentPageAssociation(bw bitwise.BinaryWriter) (n int, err error) {
	const processName = "writeSegmentPageAssociation"
	if h.pageSize() != 4 {
		if err = bw.WriteByte(byte(h.PageAssociation)); err != nil {
			return 0, jbig2errors.Wrap(err, processName, "pageSize != 4")
		}
		return 1, nil
	}
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(h.PageAssociation))
	if n, err = bw.Write(buf); err != nil {
		return 0, jbig2errors.Wrap(err, processName, "4 byte page number")
	}
	return n, nil
}

func (sd *SymbolDictionary) encodeNumSyms(bw bitwise.BinaryWriter) (n int, err error) {
	const processName = "encodeNumSyms"
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, sd.NumberOfExportedSymbols)
	if n, err = bw.Write(buf); err != nil {
		return n, jbig2errors.Wrap(err, processName, "exported symbols")
	}
	binary.BigEndian.PutUint32(buf, sd.NumberOfNewSymbols)
	m, err := bw.Write(buf)
	if err != nil {
		return n, jbig2errors.Wrap(err, processName, "new symbols")
	}
	return n + m, nil
}

func (hr *HalftoneRegion) parseHeader() error {
	if err := hr.RegionSegment.parseHeader(); err != nil {
		return err
	}
	bit, err := hr.reader.ReadBit()
	if err != nil {
		return err
	}
	hr.HDefaultPixel = int8(bit)
	bits, err := hr.reader.ReadBits(3)
	if err != nil {
		return err
	}
	hr.CombinationOperator = bitmap.CombinationOperator(bits & 0xf)
	bit, err = hr.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		hr.HSkipEnabled = true
	}
	bits, err = hr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	hr.HTemplate = byte(bits & 0xf)
	bit, err = hr.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		hr.IsMMREncoded = true
	}
	bits, err = hr.reader.ReadBits(32)
	if err != nil {
		return err
	}
	hr.HGridWidth = uint32(bits & math.MaxUint32)
	bits, err = hr.reader.ReadBits(32)
	if err != nil {
		return err
	}
	hr.HGridHeight = uint32(bits & math.MaxUint32)
	bits, err = hr.reader.ReadBits(32)
	if err != nil {
		return err
	}
	hr.HGridX = int32(bits & math.MaxInt32)
	bits, err = hr.reader.ReadBits(32)
	if err != nil {
		return err
	}
	hr.HGridY = int32(bits & math.MaxInt32)
	bits, err = hr.reader.ReadBits(16)
	if err != nil {
		return err
	}
	hr.HRegionX = uint16(bits & math.MaxUint16)
	bits, err = hr.reader.ReadBits(16)
	if err != nil {
		return err
	}
	hr.HRegionY = uint16(bits & math.MaxUint16)
	if err = hr.computeSegmentDataStructure(); err != nil {
		return err
	}
	return hr.checkInput()
}

func (gr *GenericRegion) InitEncode(bm *bitmap.Bitmap, xLoc, yLoc, template int, duplicateLineRemoval bool) error {
	const processName = "GenericRegion.InitEncode"
	if bm == nil {
		return jbig2errors.Error(processName, "provided nil bitmap")
	}
	if xLoc < 0 || yLoc < 0 {
		return jbig2errors.Error(processName, "x and y location must be greater than 0")
	}
	gr.Bitmap = bm
	gr.GBTemplate = byte(template)
	switch gr.GBTemplate {
	case 0:
		gr.GBAtX = []int8{3, -3, 2, -2}
		gr.GBAtY = []int8{-1, -1, -2, -2}
	case 1:
		gr.GBAtX = []int8{3}
		gr.GBAtY = []int8{-1}
	case 2, 3:
		gr.GBAtX = []int8{2}
		gr.GBAtY = []int8{-1}
	default:
		return jbig2errors.Errorf(processName, "provided template: '%d' not in valid range {0,1,2,3}", template)
	}
	gr.RegionSegment = &RegionSegment{BitmapHeight: uint32(bm.Height), BitmapWidth: uint32(bm.Width), XLocation: uint32(xLoc), YLocation: uint32(yLoc)}
	gr.IsTPGDon = duplicateLineRemoval
	return nil
}

func (tr *TextRegion) symbolIDCodeLengths() error {
	var (
		cs             []*huffman.Code
		bits           uint64
		runLengthTable huffman.Tabler
		err            error
	)
	for i := 0; i < 35; i++ {
		bits, err = tr.reader.ReadBits(4)
		if err != nil {
			return err
		}
		codeLen := int(bits & 0xf)
		if codeLen > 0 {
			cs = append(cs, huffman.NewCode(int32(codeLen), 0, int32(i), false))
		}
	}
	runLengthTable, err = huffman.NewFixedSizeTable(cs)
	if err != nil {
		return err
	}
	var (
		prevLen     int64
		symIdx      uint32
		symbolCodes []*huffman.Code
		runCode     int64
	)
	for symIdx < tr.NumberOfSymbols {
		runCode, err = runLengthTable.Decode(tr.reader)
		if err != nil {
			return err
		}
		if runCode < 32 {
			if runCode > 0 {
				symbolCodes = append(symbolCodes, huffman.NewCode(int32(runCode), 0, int32(symIdx), false))
			}
			prevLen = runCode
			symIdx++
		} else {
			var repeat, repeatLen int64
			switch runCode {
			case 32:
				bits, err = tr.reader.ReadBits(2)
				if err != nil {
					return err
				}
				repeat = 3 + int64(bits)
				if symIdx > 0 {
					repeatLen = prevLen
				}
			case 33:
				bits, err = tr.reader.ReadBits(3)
				if err != nil {
					return err
				}
				repeat = 3 + int64(bits)
			case 34:
				bits, err = tr.reader.ReadBits(7)
				if err != nil {
					return err
				}
				repeat = 11 + int64(bits)
			}
			for j := 0; j < int(repeat); j++ {
				if repeatLen > 0 {
					symbolCodes = append(symbolCodes, huffman.NewCode(int32(repeatLen), 0, int32(symIdx), false))
				}
				symIdx++
			}
		}
	}
	tr.reader.Align()
	tr.symbolCodeTable, err = huffman.NewFixedSizeTable(symbolCodes)
	return err
}

func (tr *TextRegion) decodeRI() (int64, error) {
	if !tr.UseRefinement {
		return 0, nil
	}
	if tr.IsHuffmanEncoded {
		bit, err := tr.reader.ReadBit()
		return int64(bit), err
	}
	ri, err := tr.arithDecoder.DecodeInt(tr.cxIARI)
	return int64(ri), err
}

func (pis *PageInformationSegment) readResolution() error {
	bits, err := pis.reader.ReadBits(32)
	if err != nil {
		return err
	}
	pis.ResolutionX = int(bits & math.MaxInt32)
	bits, err = pis.reader.ReadBits(32)
	if err != nil {
		return err
	}
	pis.ResolutionY = int(bits & math.MaxInt32)
	return nil
}
func (ts *TableSegment) HtRS() int32 { return ts.htRS }
func (ts *TableSegment) Init(h *Header, r *bitwise.Reader) error {
	ts.reader = r
	return ts.parseHeader()
}

func (grr *GenericRefinementRegion) decodeOptimized(lineNumber, width, rowStride, refRowStride, paddedWidth, refOffset, refOffsetNext int) error {
	var (
		err     error
		refX    int
		regionX int
	)
	referenceDY := lineNumber - int(grr.ReferenceDY)
	if referenceDX := int(-grr.ReferenceDX); referenceDX > 0 {
		refX = referenceDX
	}
	byteIndex := grr.ReferenceBitmap.GetByteIndex(refX, referenceDY)
	if grr.ReferenceDX > 0 {
		regionX = int(grr.ReferenceDX)
	}
	regionByteIndex := grr.RegionBitmap.GetByteIndex(regionX, lineNumber)
	switch grr.TemplateID {
	case 0:
		err = grr.decodeTemplate(lineNumber, width, rowStride, refRowStride, paddedWidth, refOffset, refOffsetNext, regionByteIndex, referenceDY, byteIndex, grr.refTemplate0)
	case 1:
		err = grr.decodeTemplate(lineNumber, width, rowStride, refRowStride, paddedWidth, refOffset, refOffsetNext, regionByteIndex, referenceDY, byteIndex, grr.refTemplate1)
	}
	return err
}

func (tr *TextRegion) decodeSymbolInstances() error {
	stripT, err := tr.decodeStripT()
	if err != nil {
		return err
	}
	var (
		firstS        int64
		instanceCount uint32
	)
	for instanceCount < tr.NumberOfSymbolInstances {
		dt, err := tr.decodeDT()
		if err != nil {
			return err
		}
		stripT += dt
		var dfs int64
		first := true
		tr.currentS = 0
		for {
			if first {
				dfs, err = tr.decodeDfs()
				if err != nil {
					return err
				}
				firstS += dfs
				tr.currentS = firstS
				first = false
			} else {
				ids, err := tr.decodeIds()
				if errors.Is(err, jbig2internal.ErrOOB) {
					break
				}
				if err != nil {
					return err
				}
				if instanceCount >= tr.NumberOfSymbolInstances {
					break
				}
				tr.currentS += ids + int64(tr.SbDsOffset)
			}
			currentT, err := tr.decodeCurrentT()
			if err != nil {
				return err
			}
			t := stripT + currentT
			id, err := tr.decodeID()
			if err != nil {
				return err
			}
			ri, err := tr.decodeRI()
			if err != nil {
				return err
			}
			ib, err := tr.decodeIb(ri, id)
			if err != nil {
				return err
			}
			if err = tr.blit(ib, t); err != nil {
				return err
			}
			instanceCount++
		}
	}
	return nil
}

func (grr *GenericRefinementRegion) getGrReference() (*bitmap.Bitmap, error) {
	segments := grr.header.RTSegments
	if len(segments) == 0 {
		return nil, errors.New("referenced segment not exists")
	}
	segmentData, err := segments[0].GetSegmentData()
	if err != nil {
		return nil, err
	}
	r, ok := segmentData.(Regioner)
	if !ok {
		return nil, fmt.Errorf("referred to segment is not a regioner: %T", segmentData)
	}
	return r.GetRegionBitmap()
}

func (h *Header) writeSegmentNumber(bw bitwise.BinaryWriter) (n int, err error) {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, h.SegmentNumber)
	if n, err = bw.Write(buf); err != nil {
		return 0, jbig2errors.Wrap(err, "Header.writeSegmentNumber", "")
	}
	return n, nil
}

func (h *Header) GetSegmentData() (Segmenter, error) {
	var segmentData Segmenter
	if h.SegmentData != nil {
		segmentData = h.SegmentData
	}
	if segmentData == nil {
		segmentCreator, ok := segmentCreators[h.Type]
		if !ok {
			return nil, fmt.Errorf("type: %s/ %d creator not found. ", h.Type, h.Type)
		}
		segmentData = segmentCreator()
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("[SEGMENT-HEADER][#%d] GetSegmentData at Offset: %04X", h.SegmentNumber, h.SegmentDataStartOffset)
		}
		subInputReader, err := h.subInputReader()
		if err != nil {
			return nil, err
		}
		if err := segmentData.Init(h, subInputReader); err != nil {
			common.Log.Debug("Init failed: %v for type: %T", err, segmentData)
			return nil, err
		}
		h.SegmentData = segmentData
	}
	return segmentData, nil
}

func (gr *GenericRegion) decodeTemplate1(lineNumber, width, paddedWidth int, byteIndex, prevByteIndex int) (err error) {
	const processName = "decodeTemplate1"
	var (
		context, overriddenContext int
		line1, line2               int
		b                          byte
		next, bit                  int
	)
	if lineNumber >= 1 {
		b, err = gr.Bitmap.GetByte(prevByteIndex)
		if err != nil {
			return jbig2errors.Wrap(err, processName, "line >= 1")
		}
		line1 = int(b)
	}
	if lineNumber >= 2 {
		b, err = gr.Bitmap.GetByte(prevByteIndex - gr.Bitmap.RowStride)
		if err != nil {
			return jbig2errors.Wrap(err, processName, "line >= 2")
		}
		line2 = int(b) << 5
	}
	context = ((line1 >> 1) & 0x1f8) | ((line2 >> 1) & 0x1e00)
	for i := 0; i < paddedWidth; i = next {
		var (
			result     byte
			minorWidth int
		)
		next = i + 8
		if remaining := width - i; remaining > 8 {
			minorWidth = 8
		} else {
			minorWidth = remaining
		}
		if lineNumber > 0 {
			line1 <<= 8
			if next < width {
				b, err = gr.Bitmap.GetByte(prevByteIndex + 1)
				if err != nil {
					return jbig2errors.Wrap(err, processName, "line > 0")
				}
				line1 |= int(b)
			}
		}
		if lineNumber > 1 {
			line2 <<= 8
			if next < width {
				b, err = gr.Bitmap.GetByte(prevByteIndex - gr.Bitmap.RowStride + 1)
				if err != nil {
					return jbig2errors.Wrap(err, processName, "line > 1")
				}
				line2 |= int(b) << 5
			}
		}
		for j := 0; j < minorWidth; j++ {
			if gr.hasAtOverride {
				overriddenContext = gr.overrideAtTemplate1(context, i+j, lineNumber, int(result), j)
				gr.cx.SetIndex(int32(overriddenContext))
			} else {
				gr.cx.SetIndex(int32(context))
			}
			bit, err = gr.arithDecoder.DecodeBit(gr.cx)
			if err != nil {
				return jbig2errors.Wrap(err, processName, "")
			}
			result |= byte(bit) << uint(7-j)
			shift := uint(8 - j)
			context = ((context & 0xefb) << 1) | bit | ((line1 >> shift) & 0x8) | ((line2 >> shift) & 0x200)
		}
		if err := gr.Bitmap.SetByte(byteIndex, result); err != nil {
			return jbig2errors.Wrap(err, processName, "")
		}
		byteIndex++
		prevByteIndex++
	}
	return nil
}

func (tr *TextRegion) initSymbols() error {
	const processName = "initSymbols"
	for _, rtSegment := range tr.Header.RTSegments {
		if rtSegment == nil {
			return jbig2errors.Error(processName, "nil segment provided for the text region Symbols")
		}
		if rtSegment.Type == 0 {
			segmentData, err := rtSegment.GetSegmentData()
			if err != nil {
				return jbig2errors.Wrap(err, processName, "")
			}
			sd, ok := segmentData.(*SymbolDictionary)
			if !ok {
				return jbig2errors.Error(processName, "referred To Segment is not a SymbolDictionary")
			}
			sd.cxIAID = tr.cxIAID
			dictionary, err := sd.GetDictionary()
			if err != nil {
				return jbig2errors.Wrap(err, processName, "")
			}
			tr.Symbols = append(tr.Symbols, dictionary...)
		}
	}
	tr.NumberOfSymbols = uint32(len(tr.Symbols))
	return nil
}

func (pis *PageInformationSegment) readIsStriped() error {
	bit, err := pis.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		pis.IsStripe = true
	}
	return nil
}

func (tr *TextRegion) Encode(w bitwise.BinaryWriter) (n int, err error) {
	const processName = "TextRegion.Encode"
	if n, err = tr.RegionInfo.Encode(w); err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	var m int
	if m, err = tr.encodeFlags(w); err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	n += m
	if m, err = tr.encodeSymbols(w); err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	n += m
	return n, nil
}

type Documenter interface {
	GetPage(int) (Pager, error)
	GetGlobalSegment(int) (*Header, error)
}

func (sd *SymbolDictionary) decodeHeightClassDeltaHeightWithHuffman() (int64, error) {
	switch sd.SdHuffDecodeHeightSelection {
	case 0:
		standardTable, err := huffman.GetStandardTable(4)
		if err != nil {
			return 0, err
		}
		return standardTable.Decode(sd.reader)
	case 1:
		standardTable, err := huffman.GetStandardTable(5)
		if err != nil {
			return 0, err
		}
		return standardTable.Decode(sd.reader)
	case 3:
		if sd.huffDH == nil {
			standardTable, err := huffman.GetStandardTable(0)
			if err != nil {
				return 0, err
			}
			sd.huffDH = standardTable
		}
		return sd.huffDH.Decode(sd.reader)
	}
	return 0, nil
}

func newTextRegion(reader *bitwise.Reader, header *Header) *TextRegion {
	textRegion := &TextRegion{reader: reader, Header: header, RegionInfo: NewRegionSegment(reader)}
	return textRegion
}

func (tr *TextRegion) readAmountOfSymbolInstances() error {
	count, err := tr.reader.ReadBits(32)
	if err != nil {
		return err
	}
	tr.NumberOfSymbolInstances = uint32(count & math.MaxUint32)
	pixels := tr.RegionInfo.BitmapWidth * tr.RegionInfo.BitmapHeight
	if pixels < tr.NumberOfSymbolInstances {
		common.Log.Debug("Limiting the number of decoded symbol instances to one per pixel ( %d instead of %d)", pixels, tr.NumberOfSymbolInstances)
		tr.NumberOfSymbolInstances = pixels
	}
	return nil
}

func (tr *TextRegion) String() string {
	builder := &strings.Builder{}
	builder.WriteString("\n[TEXT REGION]\n")
	builder.WriteString(tr.RegionInfo.String() + "\n")
	builder.WriteString(fmt.Sprintf("\t- SbrTemplate: %v\n", tr.SbrTemplate))
	builder.WriteString(fmt.Sprintf("\t- SbDsOffset: %v\n", tr.SbDsOffset))
	builder.WriteString(fmt.Sprintf("\t- DefaultPixel: %v\n", tr.DefaultPixel))
	builder.WriteString(fmt.Sprintf("\t- CombinationOperator: %v\n", tr.CombinationOperator))
	builder.WriteString(fmt.Sprintf("\t- IsTransposed: %v\n", tr.IsTransposed))
	builder.WriteString(fmt.Sprintf("\t- ReferenceCorner: %v\n", tr.ReferenceCorner))
	builder.WriteString(fmt.Sprintf("\t- UseRefinement: %v\n", tr.UseRefinement))
	builder.WriteString(fmt.Sprintf("\t- IsHuffmanEncoded: %v\n", tr.IsHuffmanEncoded))
	if tr.IsHuffmanEncoded {
		builder.WriteString(fmt.Sprintf("\t- SbHuffRSize: %v\n", tr.SbHuffRSize))
		builder.WriteString(fmt.Sprintf("\t- SbHuffRDY: %v\n", tr.SbHuffRDY))
		builder.WriteString(fmt.Sprintf("\t- SbHuffRDX: %v\n", tr.SbHuffRDX))
		builder.WriteString(fmt.Sprintf("\t- SbHuffRDHeight: %v\n", tr.SbHuffRDHeight))
		builder.WriteString(fmt.Sprintf("\t- SbHuffRDWidth: %v\n", tr.SbHuffRDWidth))
		builder.WriteString(fmt.Sprintf("\t- SbHuffDT: %v\n", tr.SbHuffDT))
		builder.WriteString(fmt.Sprintf("\t- SbHuffDS: %v\n", tr.SbHuffDS))
		builder.WriteString(fmt.Sprintf("\t- SbHuffFS: %v\n", tr.SbHuffFS))
	}
	builder.WriteString(fmt.Sprintf("\t- SbrATX: %v\n", tr.SbrATX))
	builder.WriteString(fmt.Sprintf("\t- SbrATY: %v\n", tr.SbrATY))
	builder.WriteString(fmt.Sprintf("\t- NumberOfSymbolInstances: %v\n", tr.NumberOfSymbolInstances))
	builder.WriteString(fmt.Sprintf("\t- SbrATX: %v\n", tr.SbrATX))
	return builder.String()
}

func (tr *TextRegion) decodeIds() (int64, error) {
	const processName = "decodeIds"
	if tr.IsHuffmanEncoded {
		if tr.SbHuffDS == 3 {
			if tr.huffDS == nil {
				userTableIndex := 0
				if tr.SbHuffFS == 3 {
					userTableIndex++
				}
				var err error
				tr.huffDS, err = tr.getUserTable(userTableIndex)
				if err != nil {
					return 0, jbig2errors.Wrap(err, processName, "")
				}
			}
			return tr.huffDS.Decode(tr.reader)
		}
		standardTable, err := huffman.GetStandardTable(8 + int(tr.SbHuffDS))
		if err != nil {
			return 0, jbig2errors.Wrap(err, processName, "")
		}
		return standardTable.Decode(tr.reader)
	}
	ids, err := tr.arithDecoder.DecodeInt(tr.cxIADS)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "cxIADS")
	}
	return int64(ids), nil
}

type Header struct {
	SegmentNumber            uint32
	Type                     Type
	RetainFlag               bool
	PageAssociation          int
	PageAssociationFieldSize bool
	RTSegments               []*Header
	HeaderLength             int64
	SegmentDataLength        uint64
	SegmentDataStartOffset   uint64
	Reader                   *bitwise.Reader
	SegmentData              Segmenter
	RTSNumbers               []int
	RetainBits               []uint8
}

func (h *Header) Encode(w bitwise.BinaryWriter) (n int, err error) {
	const processName = "Header.Write"
	var bw bitwise.BinaryWriter
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("[SEGMENT-HEADER][ENCODE] Begins")
	}
	defer func() {
		if err != nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[SEGMENT-HEADER][ENCODE] Failed. %v", err)
			}
		} else {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[SEGMENT-HEADER] %v", h)
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[SEGMENT-HEADER][ENCODE] Finished")
			}
		}
	}()
	w.FinishByte()
	if h.SegmentData != nil {
		se, ok := h.SegmentData.(SegmentEncoder)
		if !ok {
			return 0, jbig2errors.Errorf(processName, "Segment: %T doesn't implement SegmentEncoder interface", h.SegmentData)
		}
		bw = bitwise.BufferedMSB()
		n, err = se.Encode(bw)
		if err != nil {
			return 0, jbig2errors.Wrap(err, processName, "")
		}
		h.SegmentDataLength = uint64(n)
	}
	if h.pageSize() == 4 {
		h.PageAssociationFieldSize = true
	}
	var m int
	m, err = h.writeSegmentNumber(w)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "")
	}
	n += m
	if err = h.writeFlags(w); err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	n++
	m, err = h.writeReferredToCount(w)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "")
	}
	n += m
	m, err = h.writeReferredToSegments(w)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "")
	}
	n += m
	m, err = h.writeSegmentPageAssociation(w)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "")
	}
	n += m
	m, err = h.writeSegmentDataLength(w)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "")
	}
	n += m
	h.HeaderLength = int64(n) - int64(h.SegmentDataLength)
	if bw != nil {
		if _, err = w.Write(bw.Data()); err != nil {
			return n, jbig2errors.Wrap(err, processName, "write segment data")
		}
	}
	return n, nil
}

func (pis *PageInformationSegment) Init(h *Header, r *bitwise.Reader) (err error) {
	pis.reader = r
	if err = pis.parseHeader(); err != nil {
		return jbig2errors.Wrap(err, "PageInformationSegment.Init", "")
	}
	return nil
}

func (gr *GenericRegion) overrideAtTemplate3(context, x, y, result, bitIndex int) int {
	context &= 0x3EF
	if gr.GBAtY[0] == 0 && gr.GBAtX[0] >= -int8(bitIndex) {
		context |= (result >> uint(7-(int8(bitIndex)+gr.GBAtX[0])) & 0x1) << 4
	} else {
		context |= int(gr.getPixel(x+int(gr.GBAtX[0]), y+int(gr.GBAtY[0]))) << 4
	}
	return context
}

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

func (grr *GenericRefinementRegion) decodeTypicalPredictedLineTemplate0(lineNumber, width, rowStride, refRowStride, paddedWidth, deltaRefStride, byteIndex, referenceDY, refByteIndex int) error {
	var (
		context, overriddenContext, regionAbove, refAbove, refMid, refBelow int
		b                                                                   byte
		err                                                                 error
	)
	if lineNumber > 0 {
		b, err = grr.RegionBitmap.GetByte(byteIndex - rowStride)
		if err != nil {
			return err
		}
		regionAbove = int(b)
	}
	if referenceDY > 0 && referenceDY <= grr.ReferenceBitmap.Height {
		b, err = grr.ReferenceBitmap.GetByte(refByteIndex - refRowStride + deltaRefStride)
		if err != nil {
			return err
		}
		refAbove = int(b) << 4
	}
	if referenceDY >= 0 && referenceDY < grr.ReferenceBitmap.Height {
		b, err = grr.ReferenceBitmap.GetByte(refByteIndex + deltaRefStride)
		if err != nil {
			return err
		}
		refMid = int(b) << 1
	}
	if referenceDY > -2 && referenceDY < grr.ReferenceBitmap.Height-1 {
		b, err = grr.ReferenceBitmap.GetByte(refByteIndex + refRowStride + deltaRefStride)
		if err != nil {
			return err
		}
		refBelow = int(b)
	}
	context = ((regionAbove >> 5) & 0x6) | ((refBelow >> 2) & 0x30) | (refMid & 0x180) | (refAbove & 0xc00)
	var nextPos int
	for i := 0; i < paddedWidth; i = nextPos {
		var decoded int
		nextPos = i + 8
		var bitCount int
		if bitCount = width - i; bitCount > 8 {
			bitCount = 8
		}
		hasNextRegion := nextPos < width
		hasNextRef := nextPos < grr.ReferenceBitmap.Width
		nextDelta := deltaRefStride + 1
		if lineNumber > 0 {
			b = 0
			if hasNextRegion {
				b, err = grr.RegionBitmap.GetByte(byteIndex - rowStride + 1)
				if err != nil {
					return err
				}
			}
			regionAbove = (regionAbove << 8) | int(b)
		}
		if referenceDY > 0 && referenceDY <= grr.ReferenceBitmap.Height {
			var refAboveByte int
			if hasNextRef {
				b, err = grr.ReferenceBitmap.GetByte(refByteIndex - refRowStride + nextDelta)
				if err != nil {
					return err
				}
				refAboveByte = int(b) << 4
			}
			refAbove = (refAbove << 8) | refAboveByte
		}
		if referenceDY >= 0 && referenceDY < grr.ReferenceBitmap.Height {
			var refMidByte int
			if hasNextRef {
				b, err = grr.ReferenceBitmap.GetByte(refByteIndex + nextDelta)
				if err != nil {
					return err
				}
				refMidByte = int(b) << 1
			}
			refMid = (refMid << 8) | refMidByte
		}
		if referenceDY > -2 && referenceDY < (grr.ReferenceBitmap.Height-1) {
			b = 0
			if hasNextRef {
				b, err = grr.ReferenceBitmap.GetByte(refByteIndex + refRowStride + nextDelta)
				if err != nil {
					return err
				}
			}
			refBelow = (refBelow << 8) | int(b)
		}
		for j := 0; j < bitCount; j++ {
			var bit int
			predicted := false
			typicalContext := (context >> 4) & 0x1ff
			switch typicalContext {
			case 0x1ff:
				predicted = true
				bit = 1
			case 0x00:
				predicted = true
			}
			if !predicted {
				if grr.hasAtOverride {
					overriddenContext = grr.overrideAtTemplate0(context, i+j, lineNumber, decoded, j)
					grr.cx.SetIndex(int32(overriddenContext))
				} else {
					grr.cx.SetIndex(int32(context))
				}
				bit, err = grr.arithDecoder.DecodeBit(grr.cx)
				if err != nil {
					return err
				}
			}
			shift := uint(7 - j)
			decoded |= bit << shift
			context = ((context & 0xdb6) << 1) | bit | (regionAbove>>shift+5)&0x002 | ((refBelow>>shift + 2) & 0x010) | ((refMid >> shift) & 0x080) | ((refAbove >> shift) & 0x400)
		}
		err = grr.RegionBitmap.SetByte(byteIndex, byte(decoded))
		if err != nil {
			return err
		}
		byteIndex++
		refByteIndex++
	}
	return nil
}

type SegmentEncoder interface {
	Encode(bw bitwise.BinaryWriter) (n int, err error)
}

func (hr *HalftoneRegion) shiftAndFill(value int) int {
	value >>= 8
	if value < 0 {
		topBit := int(math.Log(float64(highestBit(value))) / math.Log(2))
		signBits := 31 - topBit
		for i := 1; i < signBits; i++ {
			value |= 1 << uint(31-i)
		}
	}
	return value
}

const (
	TSymbolDictionary                         Type = 0
	TIntermediateTextRegion                   Type = 4
	TImmediateTextRegion                      Type = 6
	TImmediateLosslessTextRegion              Type = 7
	TPatternDictionary                        Type = 16
	TIntermediateHalftoneRegion               Type = 20
	TImmediateHalftoneRegion                  Type = 22
	TImmediateLosslessHalftoneRegion          Type = 23
	TIntermediateGenericRegion                Type = 36
	TImmediateGenericRegion                   Type = 38
	TImmediateLosslessGenericRegion           Type = 39
	TIntermediateGenericRefinementRegion      Type = 40
	TImmediateGenericRefinementRegion         Type = 42
	TImmediateLosslessGenericRefinementRegion Type = 43
	TPageInformation                          Type = 48
	TEndOfPage                                Type = 49
	TEndOfStrip                               Type = 50
	TEndOfFile                                Type = 51
	TProfiles                                 Type = 52
	TTables                                   Type = 53
	TExtension                                Type = 62
	TBitmap                                   Type = 70
)

func (grr *GenericRefinementRegion) decodeTemplate(lineNumber, width, rowStride, refRowStride, paddedWidth, refOffset, refOffsetNext, regionByteIndex, referenceDY, refByteIndex int, refTemplate templater) (err error) {
	var (
		refAbove, refMid, refBelow, regionAbove, lastBit        int16
		refAboveByte, refMidByte, refBelowByte, regionAboveByte int
		b                                                       byte
	)
	if referenceDY >= 1 && (referenceDY-1) < grr.ReferenceBitmap.Height {
		b, err = grr.ReferenceBitmap.GetByte(refByteIndex - refRowStride)
		if err != nil {
			return err
		}
		refAboveByte = int(b)
	}
	if referenceDY >= 0 && referenceDY < grr.ReferenceBitmap.Height {
		b, err = grr.ReferenceBitmap.GetByte(refByteIndex)
		if err != nil {
			return err
		}
		refMidByte = int(b)
	}
	if referenceDY >= -1 && (referenceDY+1) < grr.ReferenceBitmap.Height {
		b, err = grr.ReferenceBitmap.GetByte(refByteIndex + refRowStride)
		if err != nil {
			return err
		}
		refBelowByte = int(b)
	}
	refByteIndex++
	if lineNumber >= 1 {
		b, err = grr.RegionBitmap.GetByte(regionByteIndex - rowStride)
		if err != nil {
			return err
		}
		regionAboveByte = int(b)
	}
	regionByteIndex++
	referenceDX := grr.ReferenceDX % 8
	shift := 6 + referenceDX
	refCol := refByteIndex % refRowStride
	if shift >= 0 {
		if shift < 8 {
			refAbove = int16(refAboveByte>>uint(shift)) & 0x07
		}
		if shift < 8 {
			refMid = int16(refMidByte>>uint(shift)) & 0x07
		}
		if shift < 8 {
			refBelow = int16(refBelowByte>>uint(shift)) & 0x07
		}
		if shift == 6 && refCol > 1 {
			if referenceDY >= 1 && (referenceDY-1) < grr.ReferenceBitmap.Height {
				b, err = grr.ReferenceBitmap.GetByte(refByteIndex - refRowStride - 2)
				if err != nil {
					return err
				}
				refAbove |= int16(b<<2) & 0x04
			}
			if referenceDY >= 0 && referenceDY < grr.ReferenceBitmap.Height {
				b, err = grr.ReferenceBitmap.GetByte(refByteIndex - 2)
				if err != nil {
					return err
				}
				refMid |= int16(b<<2) & 0x04
			}
			if referenceDY >= -1 && referenceDY+1 < grr.ReferenceBitmap.Height {
				b, err = grr.ReferenceBitmap.GetByte(refByteIndex + refRowStride - 2)
				if err != nil {
					return err
				}
				refBelow |= int16(b<<2) & 0x04
			}
		}
		if shift == 0 {
			refAboveByte = 0
			refMidByte = 0
			refBelowByte = 0
			if refCol < refRowStride-1 {
				if referenceDY >= 1 && referenceDY-1 < grr.ReferenceBitmap.Height {
					b, err = grr.ReferenceBitmap.GetByte(refByteIndex - refRowStride)
					if err != nil {
						return err
					}
					refAboveByte = int(b)
				}
				if referenceDY >= 0 && referenceDY < grr.ReferenceBitmap.Height {
					b, err = grr.ReferenceBitmap.GetByte(refByteIndex)
					if err != nil {
						return err
					}
					refMidByte = int(b)
				}
				if referenceDY >= -1 && referenceDY+1 < grr.ReferenceBitmap.Height {
					b, err = grr.ReferenceBitmap.GetByte(refByteIndex + refRowStride)
					if err != nil {
						return err
					}
					refBelowByte = int(b)
				}
			}
			refByteIndex++
		}
	} else {
		refAbove = int16(refAboveByte<<1) & 0x07
		refMid = int16(refMidByte<<1) & 0x07
		refBelow = int16(refBelowByte<<1) & 0x07
		refAboveByte = 0
		refMidByte = 0
		refBelowByte = 0
		if refCol < refRowStride-1 {
			if referenceDY >= 1 && referenceDY-1 < grr.ReferenceBitmap.Height {
				b, err = grr.ReferenceBitmap.GetByte(refByteIndex - refRowStride)
				if err != nil {
					return err
				}
				refAboveByte = int(b)
			}
			if referenceDY >= 0 && referenceDY < grr.ReferenceBitmap.Height {
				b, err = grr.ReferenceBitmap.GetByte(refByteIndex)
				if err != nil {
					return err
				}
				refMidByte = int(b)
			}
			if referenceDY >= -1 && referenceDY+1 < grr.ReferenceBitmap.Height {
				b, err = grr.ReferenceBitmap.GetByte(refByteIndex + refRowStride)
				if err != nil {
					return err
				}
				refBelowByte = int(b)
			}
			refByteIndex++
		}
		refAbove |= int16((refAboveByte >> 7) & 0x07)
		refMid |= int16((refMidByte >> 7) & 0x07)
		refBelow |= int16((refBelowByte >> 7) & 0x07)
	}
	regionAbove = int16(regionAboveByte >> 6)
	lastBit = 0
	shiftLeft := (2 - referenceDX) % 8
	refAboveByte <<= uint(shiftLeft)
	refMidByte <<= uint(shiftLeft)
	refBelowByte <<= uint(shiftLeft)
	regionAboveByte <<= 2
	var bit int
	for i := 0; i < width; i++ {
		bitIndex := i & 0x07
		form := refTemplate.form(refAbove, refMid, refBelow, regionAbove, lastBit)
		if grr.hasAtOverride {
			b, err = grr.RegionBitmap.GetByte(grr.RegionBitmap.GetByteIndex(i, lineNumber))
			if err != nil {
				return err
			}
			grr.cx.SetIndex(int32(grr.overrideAtTemplate0(int(form), i, lineNumber, int(b), bitIndex)))
		} else {
			grr.cx.SetIndex(int32(form))
		}
		bit, err = grr.arithDecoder.DecodeBit(grr.cx)
		if err != nil {
			return err
		}
		if err = grr.RegionBitmap.SetPixel(i, lineNumber, byte(bit)); err != nil {
			return err
		}
		refAbove = ((refAbove << 1) | 0x01&int16(refAboveByte>>7)) & 0x07
		refMid = ((refMid << 1) | 0x01&int16(refMidByte>>7)) & 0x07
		refBelow = ((refBelow << 1) | 0x01&int16(refBelowByte>>7)) & 0x07
		regionAbove = ((regionAbove << 1) | 0x01&int16(regionAboveByte>>7)) & 0x07
		lastBit = int16(bit)
		if (i-int(grr.ReferenceDX))%8 == 5 {
			refAboveByte = 0
			refMidByte = 0
			refBelowByte = 0
			if ((i-int(grr.ReferenceDX))/8)+1 < grr.ReferenceBitmap.RowStride {
				if referenceDY >= 1 && (referenceDY-1) < grr.ReferenceBitmap.Height {
					b, err = grr.ReferenceBitmap.GetByte(refByteIndex - refRowStride)
					if err != nil {
						return err
					}
					refAboveByte = int(b)
				}
				if referenceDY >= 0 && referenceDY < grr.ReferenceBitmap.Height {
					b, err = grr.ReferenceBitmap.GetByte(refByteIndex)
					if err != nil {
						return err
					}
					refMidByte = int(b)
				}
				if referenceDY >= -1 && (referenceDY+1) < grr.ReferenceBitmap.Height {
					b, err = grr.ReferenceBitmap.GetByte(refByteIndex + refRowStride)
					if err != nil {
						return err
					}
					refBelowByte = int(b)
				}
			}
			refByteIndex++
		} else {
			refAboveByte <<= 1
			refMidByte <<= 1
			refBelowByte <<= 1
		}
		if bitIndex == 5 && lineNumber >= 1 {
			if ((i >> 3) + 1) >= grr.RegionBitmap.RowStride {
				regionAboveByte = 0
			} else {
				b, err = grr.RegionBitmap.GetByte(regionByteIndex - rowStride)
				if err != nil {
					return err
				}
				regionAboveByte = int(b)
			}
			regionByteIndex++
		} else {
			regionAboveByte <<= 1
		}
	}
	return nil
}

func (tr *TextRegion) decodeRdy() (int64, error) {
	const processName = "decodeRdy"
	if tr.IsHuffmanEncoded {
		if tr.SbHuffRDY == 3 {
			if tr.huffRDY == nil {
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
				if tr.SbHuffRDWidth == 3 {
					userTableIndex++
				}
				if tr.SbHuffRDHeight == 3 {
					userTableIndex++
				}
				if tr.SbHuffRDX == 3 {
					userTableIndex++
				}
				tr.huffRDY, err = tr.getUserTable(userTableIndex)
				if err != nil {
					return 0, jbig2errors.Wrap(err, processName, "")
				}
			}
			return tr.huffRDY.Decode(tr.reader)
		}
		standardTable, err := huffman.GetStandardTable(14 + int(tr.SbHuffRDY))
		if err != nil {
			return 0, err
		}
		return standardTable.Decode(tr.reader)
	}
	v, err := tr.arithDecoder.DecodeInt(tr.cxIARDY)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "")
	}
	return int64(v), nil
}

func (gr *GenericRegion) Encode(w bitwise.BinaryWriter) (n int, err error) {
	const processName = "GenericRegion.Encode"
	if gr.Bitmap == nil {
		return 0, jbig2errors.Error(processName, "provided nil bitmap")
	}
	encode, err := gr.RegionSegment.Encode(w)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "RegionSegment")
	}
	n += encode
	if err = w.SkipBits(4); err != nil {
		return n, jbig2errors.Wrap(err, processName, "skip reserved bits")
	}
	var bit int
	if gr.IsTPGDon {
		bit = 1
	}
	if err = w.WriteBit(bit); err != nil {
		return n, jbig2errors.Wrap(err, processName, "tpgdon")
	}
	bit = 0
	if err = w.WriteBit(int(gr.GBTemplate>>1) & 0x01); err != nil {
		return n, jbig2errors.Wrap(err, processName, "first gbtemplate bit")
	}
	if err = w.WriteBit(int(gr.GBTemplate) & 0x01); err != nil {
		return n, jbig2errors.Wrap(err, processName, "second gbtemplate bit")
	}
	if gr.UseMMR {
		bit = 1
	}
	if err = w.WriteBit(bit); err != nil {
		return n, jbig2errors.Wrap(err, processName, "use MMR bit")
	}
	n++
	if encode, err = gr.writeGBAtPixels(w); err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	n += encode
	e := arithmeticencoder.New()
	if err = e.EncodeBitmap(gr.Bitmap, gr.IsTPGDon); err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	e.Final()
	var written int64
	if written, err = e.WriteTo(w); err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	n += int(written)
	return n, nil
}

func (sd *SymbolDictionary) checkInput() error {
	if sd.SdHuffDecodeHeightSelection == 2 {
		common.Log.Debug("Symbol Dictionary Decode Height Selection: %d value not permitted", sd.SdHuffDecodeHeightSelection)
	}
	if sd.SdHuffDecodeWidthSelection == 2 {
		common.Log.Debug("Symbol Dictionary Decode Width Selection: %d value not permitted", sd.SdHuffDecodeWidthSelection)
	}
	if sd.IsHuffmanEncoded {
		if sd.SdTemplate != 0 {
			common.Log.Debug("SDTemplate = %d (should be 0)", sd.SdTemplate)
		}
		if !sd.UseRefinementAggregation {
			if !sd.UseRefinementAggregation {
				if sd.isCodingContextRetained {
					common.Log.Debug("IsCodingContextRetained = true (should be false)")
					sd.isCodingContextRetained = false
				}
				if sd.isCodingContextUsed {
					common.Log.Debug("isCodingContextUsed = true (should be false)")
					sd.isCodingContextUsed = false
				}
			}
		}
	} else {
		if sd.SdHuffBMSizeSelection != 0 {
			common.Log.Debug("SdHuffBMSizeSelection should be 0")
			sd.SdHuffBMSizeSelection = 0
		}
		if sd.SdHuffDecodeWidthSelection != 0 {
			common.Log.Debug("SdHuffDecodeWidthSelection should be 0")
			sd.SdHuffDecodeWidthSelection = 0
		}
		if sd.SdHuffDecodeHeightSelection != 0 {
			common.Log.Debug("SdHuffDecodeHeightSelection should be 0")
			sd.SdHuffDecodeHeightSelection = 0
		}
	}
	if !sd.UseRefinementAggregation {
		if sd.SdrTemplate != 0 {
			common.Log.Debug("SDRTemplate = %d (should be 0)", sd.SdrTemplate)
			sd.SdrTemplate = 0
		}
	}
	if !sd.IsHuffmanEncoded || !sd.UseRefinementAggregation {
		if sd.SdHuffAggInstanceSelection {
			common.Log.Debug("SdHuffAggInstanceSelection = %d (should be 0)", sd.SdHuffAggInstanceSelection)
		}
	}
	return nil
}

func (sd *SymbolDictionary) decodeDifferenceWidth() (int64, error) {
	if sd.IsHuffmanEncoded {
		switch sd.SdHuffDecodeWidthSelection {
		case 0:
			standardTable, err := huffman.GetStandardTable(2)
			if err != nil {
				return 0, err
			}
			return standardTable.Decode(sd.reader)
		case 1:
			standardTable, err := huffman.GetStandardTable(3)
			if err != nil {
				return 0, err
			}
			return standardTable.Decode(sd.reader)
		case 3:
			if sd.huffDW == nil {
				var userTableIndex int
				if sd.SdHuffDecodeHeightSelection == 3 {
					userTableIndex++
				}
				userTable, err := sd.getUserTable(userTableIndex)
				if err != nil {
					return 0, err
				}
				sd.huffDW = userTable
			}
			return sd.huffDW.Decode(sd.reader)
		}
	} else {
		v, err := sd.arithDecoder.DecodeInt(sd.cxIADW)
		if err != nil {
			return 0, err
		}
		return int64(v), nil
	}
	return 0, nil
}

func (sd *SymbolDictionary) huffDecodeRefAggNInst() (int64, error) {
	if !sd.SdHuffAggInstanceSelection {
		standardTable, err := huffman.GetStandardTable(1)
		if err != nil {
			return 0, err
		}
		return standardTable.Decode(sd.reader)
	}
	if sd.huffAggInst == nil {
		var (
			userTableIndex int
			err            error
		)
		if sd.SdHuffDecodeHeightSelection == 3 {
			userTableIndex++
		}
		if sd.SdHuffDecodeWidthSelection == 3 {
			userTableIndex++
		}
		if sd.SdHuffBMSizeSelection == 3 {
			userTableIndex++
		}
		sd.huffAggInst, err = sd.getUserTable(userTableIndex)
		if err != nil {
			return 0, err
		}
	}
	return sd.huffAggInst.Decode(sd.reader)
}

func (sd *SymbolDictionary) decodeNewSymbols(width, height uint32, bm *bitmap.Bitmap, refDX, refDY int32) error {
	if sd.refinementRegion == nil {
		sd.refinementRegion = newGenericRefinementRegion(sd.reader, nil)
		if sd.arithDecoder == nil {
			var err error
			sd.arithDecoder, err = arithmeticdecoder.New(sd.reader)
			if err != nil {
				return err
			}
		}
		if sd.cx == nil {
			sd.cx = arithmeticdecoder.NewStats(65536, 1)
		}
	}
	sd.refinementRegion.setParameters(sd.cx, sd.arithDecoder, sd.SdrTemplate, width, height, bm, refDX, refDY, false, sd.SdrATX, sd.SdrATY)
	return sd.addSymbol(sd.refinementRegion)
}

func (gr *GenericRegion) readGBAtPixels(count int) error {
	const processName = "readGBAtPixels"
	gr.GBAtX = make([]int8, count)
	gr.GBAtY = make([]int8, count)
	for i := 0; i < count; i++ {
		v, err := gr.reader.ReadByte()
		if err != nil {
			return jbig2errors.Wrapf(err, processName, "X at i: '%d'", i)
		}
		gr.GBAtX[i] = int8(v)
		v, err = gr.reader.ReadByte()
		if err != nil {
			return jbig2errors.Wrapf(err, processName, "Y at i: '%d'", i)
		}
		gr.GBAtY[i] = int8(v)
	}
	return nil
}

func (h *Header) readSegmentDataLength(r *bitwise.Reader) (err error) {
	h.SegmentDataLength, err = r.ReadBits(32)
	if err != nil {
		return err
	}
	h.SegmentDataLength &= math.MaxInt32
	return nil
}

func (gr *GenericRegion) decodeLine(lineNumber, width, paddedWidth int) error {
	const processName = "decodeLine"
	byteIndex := gr.Bitmap.GetByteIndex(0, lineNumber)
	rowStride := byteIndex - gr.Bitmap.RowStride
	switch gr.GBTemplate {
	case 0:
		if !gr.UseExtTemplates {
			return gr.decodeTemplate0a(lineNumber, width, paddedWidth, byteIndex, rowStride)
		}
		return gr.decodeTemplate0b(lineNumber, width, paddedWidth, byteIndex, rowStride)
	case 1:
		return gr.decodeTemplate1(lineNumber, width, paddedWidth, byteIndex, rowStride)
	case 2:
		return gr.decodeTemplate2(lineNumber, width, paddedWidth, byteIndex, rowStride)
	case 3:
		return gr.decodeTemplate3(lineNumber, width, paddedWidth, byteIndex, rowStride)
	}
	return jbig2errors.Errorf(processName, "invalid GBTemplate provided: %d", gr.GBTemplate)
}

func (sd *SymbolDictionary) decodeAggregate(width, height uint32) error {
	var (
		numInstances int64
		err          error
	)
	if sd.IsHuffmanEncoded {
		numInstances, err = sd.huffDecodeRefAggNInst()
		if err != nil {
			return err
		}
	} else {
		v, err := sd.arithDecoder.DecodeInt(sd.cxIAAI)
		if err != nil {
			return err
		}
		numInstances = int64(v)
	}
	if numInstances > 1 {
		return sd.decodeThroughTextRegion(width, height, uint32(numInstances))
	} else if numInstances == 1 {
		return sd.decodeRefinedSymbol(width, height)
	}
	return nil
}

func (tr *TextRegion) getUserTable(index int) (huffman.Tabler, error) {
	const processName = "getUserTable"
	var tableIndex int
	for _, rtSegment := range tr.Header.RTSegments {
		if rtSegment.Type == 53 {
			if tableIndex == index {
				segmentData, err := rtSegment.GetSegmentData()
				if err != nil {
					return nil, err
				}
				ts, ok := segmentData.(*TableSegment)
				if !ok {
					common.Log.Debug(fmt.Sprintf("segment with Type 53 - and index: %d not a TableSegment", rtSegment.SegmentNumber))
					return nil, jbig2errors.Error(processName, "segment with Type 53 is not a *TableSegment")
				}
				return huffman.NewEncodedTable(ts)
			}
			tableIndex++
		}
	}
	return nil, nil
}

type TextRegion struct {
	reader                            *bitwise.Reader
	RegionInfo                        *RegionSegment
	SbrTemplate                       int8
	SbDsOffset                        int8
	DefaultPixel                      int8
	CombinationOperator               bitmap.CombinationOperator
	IsTransposed                      int8
	ReferenceCorner                   int16
	LogSBStrips                       int16
	UseRefinement                     bool
	IsHuffmanEncoded                  bool
	SbHuffRSize                       int8
	SbHuffRDY                         int8
	SbHuffRDX                         int8
	SbHuffRDHeight                    int8
	SbHuffRDWidth                     int8
	SbHuffDT                          int8
	SbHuffDS                          int8
	SbHuffFS                          int8
	SbrATX                            []int8
	SbrATY                            []int8
	NumberOfSymbolInstances           uint32
	currentS                          int64
	SbStrips                          int8
	NumberOfSymbols                   uint32
	RegionBitmap                      *bitmap.Bitmap
	Symbols                           []*bitmap.Bitmap
	arithDecoder                      *arithmeticdecoder.Decoder
	refinementRegion                  *GenericRefinementRegion
	cxIADT                            *arithmeticdecoder.DecoderStats
	cxIAFS                            *arithmeticdecoder.DecoderStats
	cxIADS                            *arithmeticdecoder.DecoderStats
	cxIAIT                            *arithmeticdecoder.DecoderStats
	cxIARI                            *arithmeticdecoder.DecoderStats
	cxIARDW                           *arithmeticdecoder.DecoderStats
	cxIARDH                           *arithmeticdecoder.DecoderStats
	cxIAID                            *arithmeticdecoder.DecoderStats
	cxIARDX                           *arithmeticdecoder.DecoderStats
	cxIARDY                           *arithmeticdecoder.DecoderStats
	cxRefinement                      *arithmeticdecoder.DecoderStats
	symbolCodeLength                  int8
	symbolCodeTable                   *huffman.FixedSizeTable
	Header                            *Header
	huffFS                            huffman.Tabler
	huffDS                            huffman.Tabler
	huffDT                            huffman.Tabler
	huffRDW                           huffman.Tabler
	huffRDH                           huffman.Tabler
	huffRDX                           huffman.Tabler
	huffRDY                           huffman.Tabler
	huffRSize                         huffman.Tabler
	globalSymbolsMap, localSymbolsMap map[int]int
	comps                             []int
	inLL                              *bitmap.Points
	symbolBitmaps                     *bitmap.Bitmaps
	classIDs                          *basic.IntSlice
	unused, symBits                   int
	boxes                             *bitmap.Boxes
}

func (grr *GenericRefinementRegion) setParameters(cx *arithmeticdecoder.DecoderStats, arithDecoder *arithmeticdecoder.Decoder, templateID int8, bitmapWidth, bitmapHeight uint32, referenceBitmap *bitmap.Bitmap, referenceDX, referenceDY int32, isTPGROn bool, grAtX []int8, grAtY []int8) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("[GENERIC-REF-REGION] setParameters")
	}
	if cx != nil {
		grr.cx = cx
	}
	if arithDecoder != nil {
		grr.arithDecoder = arithDecoder
	}
	grr.TemplateID = templateID
	grr.RegionInfo.BitmapWidth = bitmapWidth
	grr.RegionInfo.BitmapHeight = bitmapHeight
	grr.ReferenceBitmap = referenceBitmap
	grr.ReferenceDX = referenceDX
	grr.ReferenceDY = referenceDY
	grr.IsTPGROn = isTPGROn
	grr.GrAtX = grAtX
	grr.GrAtY = grAtY
	grr.RegionBitmap = nil
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("[GENERIC-REF-REGION] setParameters finished. %s", grr)
	}
}

func (hr *HalftoneRegion) grayScaleDecoding(bitsPerValue int) ([][]int, error) {
	var (
		atX []int8
		atY []int8
	)
	if !hr.IsMMREncoded {
		atX = make([]int8, 4)
		atY = make([]int8, 4)
		if hr.HTemplate <= 1 {
			atX[0] = 3
		} else if hr.HTemplate >= 2 {
			atX[0] = 2
		}
		atY[0] = -1
		atX[1] = -3
		atY[1] = -1
		atX[2] = 2
		atY[2] = -2
		atX[3] = -2
		atY[3] = -2
	}
	bms := make([]*bitmap.Bitmap, bitsPerValue)
	genericRegion := NewGenericRegion(hr.reader)
	genericRegion.setParametersMMR(hr.IsMMREncoded, hr.DataOffset, hr.DataLength, hr.HGridHeight, hr.HGridWidth, hr.HTemplate, false, hr.HSkipEnabled, atX, atY)
	plane := bitsPerValue - 1
	var err error
	bms[plane], err = genericRegion.GetRegionBitmap()
	if err != nil {
		return nil, err
	}
	for plane > 0 {
		plane--
		genericRegion.Bitmap = nil
		bms[plane], err = genericRegion.GetRegionBitmap()
		if err != nil {
			return nil, err
		}
		if err = hr.combineGrayscalePlanes(bms, plane); err != nil {
			return nil, err
		}
	}
	return hr.computeGrayScalePlanes(bms, bitsPerValue)
}
func (grr *GenericRefinementRegion) GetRegionInfo() *RegionSegment { return grr.RegionInfo }
func (ts *TableSegment) StreamReader() *bitwise.Reader             { return ts.reader }
func (tr *TextRegion) decodeID() (int64, error) {
	if tr.IsHuffmanEncoded {
		if tr.symbolCodeTable == nil {
			v, err := tr.reader.ReadBits(byte(tr.symbolCodeLength))
			return int64(v), err
		}
		return tr.symbolCodeTable.Decode(tr.reader)
	}
	return tr.arithDecoder.DecodeIAID(uint64(tr.symbolCodeLength), tr.cxIAID)
}

func (pis *PageInformationSegment) readCombinationOperatorOverrideAllowed() error {
	bit, err := pis.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		pis.combinationOperatorOverride = true
	}
	return nil
}

func (pd *PatternDictionary) readIsMMREncoded() error {
	bit, err := pd.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit != 0 {
		pd.IsMMREncoded = true
	}
	return nil
}

func (pis *PageInformationSegment) readRequiresAuxiliaryBuffer() error {
	bit, err := pis.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		pis.requiresAuxiliaryBuffer = true
	}
	return nil
}

func (sd *SymbolDictionary) getToExportFlags() ([]int, error) {
	var (
		flag                    int
		runLength               int32
		err                     error
		numberOfImportedSymbols = int32(sd.numberOfImportedSymbols + sd.NumberOfNewSymbols)
		flags                   = make([]int, numberOfImportedSymbols)
	)
	for i := int32(0); i < numberOfImportedSymbols; i += runLength {
		if sd.IsHuffmanEncoded {
			standardTable, err := huffman.GetStandardTable(1)
			if err != nil {
				return nil, err
			}
			v, err := standardTable.Decode(sd.reader)
			if err != nil {
				return nil, err
			}
			runLength = int32(v)
		} else {
			runLength, err = sd.arithDecoder.DecodeInt(sd.cxIAEX)
			if err != nil {
				return nil, err
			}
		}
		if runLength != 0 {
			if i+runLength > numberOfImportedSymbols {
				return nil, jbig2errors.Error("SymbolDictionary.getToExportFlags", "malformed input data provided. index out of range")
			}
			for j := i; j < i+runLength; j++ {
				flags[j] = flag
			}
		}
		if flag == 0 {
			flag = 1
		} else {
			flag = 0
		}
	}
	return flags, nil
}

func (t *template0) form(refAbove, refMid, refBelow, regionAbove, lastBit int16) int16 {
	return (refAbove << 10) | (refMid << 7) | (refBelow << 4) | (regionAbove << 1) | lastBit
}

func (sd *SymbolDictionary) getSymbol(index int) (*bitmap.Bitmap, error) {
	const processName = "getSymbol"
	bitmap, err := sd.symbols.GetBitmap(sd.symbolList[index])
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "can't get symbol")
	}
	return bitmap, nil
}

func (grr *GenericRefinementRegion) parseHeader() (err error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("[GENERIC-REF-REGION] parsing Header...")
	}
	now := time.Now()
	defer func() {
		if err == nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[GENERIC-REF-REGION] parsing header finishid in: %d ns", time.Since(now).Nanoseconds())
			}
		} else {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[GENERIC-REF-REGION] parsing header failed: %s", err)
			}
		}
	}()
	if err = grr.RegionInfo.parseHeader(); err != nil {
		return err
	}
	_, err = grr.reader.ReadBits(6)
	if err != nil {
		return err
	}
	grr.IsTPGROn, err = grr.reader.ReadBool()
	if err != nil {
		return err
	}
	var bit int
	bit, err = grr.reader.ReadBit()
	if err != nil {
		return err
	}
	grr.TemplateID = int8(bit)
	switch grr.TemplateID {
	case 0:
		grr.Template = grr.refTemplate0
		if err = grr.readAtPixels(); err != nil {
			return err
		}
	case 1:
		grr.Template = grr.refTemplate1
	}
	return nil
}

func (pd *PatternDictionary) readTemplate() error {
	v, err := pd.reader.ReadBits(2)
	if err != nil {
		return err
	}
	pd.HDTemplate = byte(v)
	return nil
}

func (h *Header) readSegmentPageAssociation(d Documenter, r *bitwise.Reader, count uint64, referredNumbers ...int) (err error) {
	const processName = "readSegmentPageAssociation"
	if !h.PageAssociationFieldSize {
		v, err := r.ReadBits(8)
		if err != nil {
			return jbig2errors.Wrap(err, processName, "short format")
		}
		h.PageAssociation = int(v & 0xFF)
	} else {
		v, err := r.ReadBits(32)
		if err != nil {
			return jbig2errors.Wrap(err, processName, "long format")
		}
		h.PageAssociation = int(v & math.MaxInt32)
	}
	if count == 0 {
		return nil
	}
	if h.PageAssociation != 0 {
		page, err := d.GetPage(h.PageAssociation)
		if err != nil {
			return jbig2errors.Wrap(err, processName, "associated page not found")
		}
		var number int
		for i := uint64(0); i < count; i++ {
			number = referredNumbers[i]
			h.RTSegments[i], err = page.GetSegment(number)
			if err != nil {
				var err2 error
				h.RTSegments[i], err2 = d.GetGlobalSegment(number)
				if err2 != nil {
					return jbig2errors.Wrapf(err, processName, "reference segment not found at page: '%d' nor in globals", h.PageAssociation)
				}
			}
		}
		return nil
	}
	for i := uint64(0); i < count; i++ {
		h.RTSegments[i], err = d.GetGlobalSegment(referredNumbers[i])
		if err != nil {
			return jbig2errors.Wrapf(err, processName, "global segment: '%d' not found", referredNumbers[i])
		}
	}
	return nil
}

func (pis *PageInformationSegment) CombinationOperator() bitmap.CombinationOperator {
	return pis.combinationOperator
}

func (tr *TextRegion) readRegionFlags() error {
	var (
		bit  int
		bits uint64
		err  error
	)
	bit, err = tr.reader.ReadBit()
	if err != nil {
		return err
	}
	tr.SbrTemplate = int8(bit)
	bits, err = tr.reader.ReadBits(5)
	if err != nil {
		return err
	}
	tr.SbDsOffset = int8(bits)
	if tr.SbDsOffset > 0x0f {
		tr.SbDsOffset -= 0x20
	}
	bit, err = tr.reader.ReadBit()
	if err != nil {
		return err
	}
	tr.DefaultPixel = int8(bit)
	bits, err = tr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	tr.CombinationOperator = bitmap.CombinationOperator(int(bits) & 0x3)
	bit, err = tr.reader.ReadBit()
	if err != nil {
		return err
	}
	tr.IsTransposed = int8(bit)
	bits, err = tr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	tr.ReferenceCorner = int16(bits) & 0x3
	bits, err = tr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	tr.LogSBStrips = int16(bits) & 0x3
	tr.SbStrips = 1 << uint(tr.LogSBStrips)
	bit, err = tr.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		tr.UseRefinement = true
	}
	bit, err = tr.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		tr.IsHuffmanEncoded = true
	}
	return nil
}

func (h *Header) pageSize() uint {
	if h.PageAssociation <= 255 {
		return 1
	}
	return 4
}

func (tr *TextRegion) parseHeader() error {
	var err error
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("[TEXT REGION][PARSE-HEADER] begins...")
	}
	defer func() {
		if err != nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[TEXT REGION][PARSE-HEADER] failed. %v", err)
			}
		} else {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[TEXT REGION][PARSE-HEADER] finished.")
			}
		}
	}()
	if err = tr.RegionInfo.parseHeader(); err != nil {
		return err
	}
	if err = tr.readRegionFlags(); err != nil {
		return err
	}
	if tr.IsHuffmanEncoded {
		if err = tr.readHuffmanFlags(); err != nil {
			return err
		}
	}
	if err = tr.readUseRefinement(); err != nil {
		return err
	}
	if err = tr.readAmountOfSymbolInstances(); err != nil {
		return err
	}
	if err = tr.getSymbols(); err != nil {
		return err
	}
	if err = tr.computeSymbolCodeLength(); err != nil {
		return err
	}
	if err = tr.checkInput(); err != nil {
		return err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("%s", tr.String())
	}
	return nil
}

func (pis *PageInformationSegment) readCombinationOperator() error {
	v, err := pis.reader.ReadBits(2)
	if err != nil {
		return err
	}
	pis.combinationOperator = bitmap.CombinationOperator(int(v))
	return nil
}

func (sd *SymbolDictionary) decodeDirectlyThroughGenericRegion(width, height uint32) error {
	if sd.genericRegion == nil {
		sd.genericRegion = NewGenericRegion(sd.reader)
	}
	sd.genericRegion.setParametersWithAt(false, byte(sd.SdTemplate), false, false, sd.SdATX, sd.SdATY, width, height, sd.cx, sd.arithDecoder)
	return sd.addSymbol(sd.genericRegion)
}

func (gr *GenericRegion) setOverrideFlag(index int) {
	gr.GBAtOverride[index] = true
	gr.hasAtOverride = true
}

func (sd *SymbolDictionary) encodeFlags(bw bitwise.BinaryWriter) (n int, err error) {
	const processName = "encodeFlags"
	if err = bw.SkipBits(3); err != nil {
		return 0, jbig2errors.Wrap(err, processName, "empty bits")
	}
	var bit int
	if sd.SdrTemplate > 0 {
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return n, jbig2errors.Wrap(err, processName, "sdrTemplate")
	}
	bit = 0
	if sd.SdTemplate > 1 {
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return n, jbig2errors.Wrap(err, processName, "sdTemplate")
	}
	bit = 0
	if sd.SdTemplate == 1 || sd.SdTemplate == 3 {
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return n, jbig2errors.Wrap(err, processName, "sdTemplate")
	}
	bit = 0
	if sd.isCodingContextRetained {
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return n, jbig2errors.Wrap(err, processName, "coding context retained")
	}
	bit = 0
	if sd.isCodingContextUsed {
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return n, jbig2errors.Wrap(err, processName, "coding context used")
	}
	bit = 0
	if sd.SdHuffAggInstanceSelection {
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return n, jbig2errors.Wrap(err, processName, "sdHuffAggInst")
	}
	bit = int(sd.SdHuffBMSizeSelection)
	if err = bw.WriteBit(bit); err != nil {
		return n, jbig2errors.Wrap(err, processName, "sdHuffBmSize")
	}
	bit = 0
	if sd.SdHuffDecodeWidthSelection > 1 {
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return n, jbig2errors.Wrap(err, processName, "sdHuffWidth")
	}
	bit = 0
	switch sd.SdHuffDecodeWidthSelection {
	case 1, 3:
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return n, jbig2errors.Wrap(err, processName, "sdHuffWidth")
	}
	bit = 0
	if sd.SdHuffDecodeHeightSelection > 1 {
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return n, jbig2errors.Wrap(err, processName, "sdHuffHeight")
	}
	bit = 0
	switch sd.SdHuffDecodeHeightSelection {
	case 1, 3:
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return n, jbig2errors.Wrap(err, processName, "sdHuffHeight")
	}
	bit = 0
	if sd.UseRefinementAggregation {
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return n, jbig2errors.Wrap(err, processName, "sdRefAgg")
	}
	bit = 0
	if sd.IsHuffmanEncoded {
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return n, jbig2errors.Wrap(err, processName, "sdHuff")
	}
	return 2, nil
}

func (rs *RegionSegment) Encode(w bitwise.BinaryWriter) (n int, err error) {
	const processName = "RegionSegment.Encode"
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, rs.BitmapWidth)
	n, err = w.Write(buf)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "Width")
	}
	binary.BigEndian.PutUint32(buf, rs.BitmapHeight)
	var written int
	written, err = w.Write(buf)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "Height")
	}
	n += written
	binary.BigEndian.PutUint32(buf, rs.XLocation)
	written, err = w.Write(buf)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "XLocation")
	}
	n += written
	binary.BigEndian.PutUint32(buf, rs.YLocation)
	written, err = w.Write(buf)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "YLocation")
	}
	n += written
	if err = w.WriteByte(byte(rs.CombinaionOperator) & 0x07); err != nil {
		return 0, jbig2errors.Wrap(err, processName, "combination operator")
	}
	n++
	return n, nil
}

func NewGenericRegion(r *bitwise.Reader) *GenericRegion {
	return &GenericRegion{RegionSegment: NewRegionSegment(r), reader: r}
}

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

func (tr *TextRegion) encodeFlags(bw bitwise.BinaryWriter) (n int, err error) {
	const processName = "encodeFlags"
	if err = bw.WriteBit(int(tr.SbrTemplate)); err != nil {
		return n, jbig2errors.Wrap(err, processName, "sbrTemplate")
	}
	if _, err = bw.WriteBits(uint64(tr.SbDsOffset), 5); err != nil {
		return n, jbig2errors.Wrap(err, processName, "sbDsOffset")
	}
	if err = bw.WriteBit(int(tr.DefaultPixel)); err != nil {
		return n, jbig2errors.Wrap(err, processName, "DefaultPixel")
	}
	if _, err = bw.WriteBits(uint64(tr.CombinationOperator), 2); err != nil {
		return n, jbig2errors.Wrap(err, processName, "CombinationOperator")
	}
	if err = bw.WriteBit(int(tr.IsTransposed)); err != nil {
		return n, jbig2errors.Wrap(err, processName, "is transposed")
	}
	if _, err = bw.WriteBits(uint64(tr.ReferenceCorner), 2); err != nil {
		return n, jbig2errors.Wrap(err, processName, "reference corner")
	}
	if _, err = bw.WriteBits(uint64(tr.LogSBStrips), 2); err != nil {
		return n, jbig2errors.Wrap(err, processName, "LogSBStrips")
	}
	var bit int
	if tr.UseRefinement {
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return n, jbig2errors.Wrap(err, processName, "use refinement")
	}
	bit = 0
	if tr.IsHuffmanEncoded {
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return n, jbig2errors.Wrap(err, processName, "use huffman")
	}
	n = 2
	return n, nil
}
func (tr *TextRegion) GetRegionInfo() *RegionSegment { return tr.RegionInfo }
func (ts *TableSegment) parseHeader() error {
	var (
		bit int
		v   uint64
		err error
	)
	bit, err = ts.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		return fmt.Errorf("invalid table segment definition. B.2.1 Code Table flags: Bit 7 must be zero. Was: %d", bit)
	}
	if v, err = ts.reader.ReadBits(3); err != nil {
		return err
	}
	ts.htRS = (int32(v) + 1) & 0xf
	if v, err = ts.reader.ReadBits(3); err != nil {
		return err
	}
	ts.htPS = (int32(v) + 1) & 0xf
	if v, err = ts.reader.ReadBits(32); err != nil {
		return err
	}
	ts.htLow = int32(v & math.MaxInt32)
	if v, err = ts.reader.ReadBits(32); err != nil {
		return err
	}
	ts.htHigh = int32(v & math.MaxInt32)
	return nil
}

func (tr *TextRegion) Init(header *Header, r *bitwise.Reader) error {
	tr.Header = header
	tr.reader = r
	tr.RegionInfo = NewRegionSegment(tr.reader)
	return tr.parseHeader()
}

func (h *Header) parse(d Documenter, r *bitwise.Reader, offset int64, ot OrganizationType) (err error) {
	const processName = "parse"
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("[SEGMENT-HEADER][PARSE] Begins")
	}
	defer func() {
		if err != nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[SEGMENT-HEADER][PARSE] Failed. %v", err)
			}
		} else {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[SEGMENT-HEADER][PARSE] Finished")
			}
		}
	}()
	_, err = r.Seek(offset, io.SeekStart)
	if err != nil {
		return jbig2errors.Wrap(err, processName, "seek start")
	}
	if err = h.readSegmentNumber(r); err != nil {
		return jbig2errors.Wrap(err, processName, "")
	}
	if err = h.readHeaderFlags(); err != nil {
		return jbig2errors.Wrap(err, processName, "")
	}
	var count uint64
	count, err = h.readNumberOfReferredToSegments(r)
	if err != nil {
		return jbig2errors.Wrap(err, processName, "")
	}
	h.RTSNumbers, err = h.readReferredToSegmentNumbers(r, int(count))
	if err != nil {
		return jbig2errors.Wrap(err, processName, "")
	}
	err = h.readSegmentPageAssociation(d, r, count, h.RTSNumbers...)
	if err != nil {
		return jbig2errors.Wrap(err, processName, "")
	}
	if h.Type != TEndOfFile {
		if err = h.readSegmentDataLength(r); err != nil {
			return jbig2errors.Wrap(err, processName, "")
		}
	}
	h.readDataStartOffset(r, ot)
	h.readHeaderLength(r, offset)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("%s", h)
	}
	return nil
}

var _ huffman.BasicTabler = &TableSegment{}

func (rs *RegionSegment) Size() int { return 17 }
func (tr *TextRegion) decodeSymInRefSize() (int64, error) {
	const processName = "decodeSymInRefSize"
	if tr.SbHuffRSize == 0 {
		standardTable, err := huffman.GetStandardTable(1)
		if err != nil {
			return 0, jbig2errors.Wrap(err, processName, "")
		}
		return standardTable.Decode(tr.reader)
	}
	if tr.huffRSize == nil {
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
		if tr.SbHuffRDWidth == 3 {
			userTableIndex++
		}
		if tr.SbHuffRDHeight == 3 {
			userTableIndex++
		}
		if tr.SbHuffRDX == 3 {
			userTableIndex++
		}
		if tr.SbHuffRDY == 3 {
			userTableIndex++
		}
		tr.huffRSize, err = tr.getUserTable(userTableIndex)
		if err != nil {
			return 0, jbig2errors.Wrap(err, processName, "")
		}
	}
	v, err := tr.huffRSize.Decode(tr.reader)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "")
	}
	return v, nil
}

func (tr *TextRegion) decodeRdx() (int64, error) {
	const processName = "decodeRdx"
	if tr.IsHuffmanEncoded {
		if tr.SbHuffRDX == 3 {
			if tr.huffRDX == nil {
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
				if tr.SbHuffRDWidth == 3 {
					userTableIndex++
				}
				if tr.SbHuffRDHeight == 3 {
					userTableIndex++
				}
				tr.huffRDX, err = tr.getUserTable(userTableIndex)
				if err != nil {
					return 0, jbig2errors.Wrap(err, processName, "")
				}
			}
			return tr.huffRDX.Decode(tr.reader)
		}
		standardTable, err := huffman.GetStandardTable(14 + int(tr.SbHuffRDX))
		if err != nil {
			return 0, jbig2errors.Wrap(err, processName, "")
		}
		return standardTable.Decode(tr.reader)
	}
	v, err := tr.arithDecoder.DecodeInt(tr.cxIARDX)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "")
	}
	return int64(v), nil
}

func (gr *GenericRegion) decodeTemplate0a(lineNumber, width, paddedWidth int, byteIndex, prevByteIndex int) (err error) {
	const processName = "decodeTemplate0a"
	var (
		context, overriddenContext int
		line1, line2               int
		b                          byte
		next                       int
	)
	if lineNumber >= 1 {
		b, err = gr.Bitmap.GetByte(prevByteIndex)
		if err != nil {
			return jbig2errors.Wrap(err, processName, "line >= 1")
		}
		line1 = int(b)
	}
	if lineNumber >= 2 {
		b, err = gr.Bitmap.GetByte(prevByteIndex - gr.Bitmap.RowStride)
		if err != nil {
			return jbig2errors.Wrap(err, processName, "line >= 2")
		}
		line2 = int(b) << 6
	}
	context = (line1 & 0xf0) | (line2 & 0x3800)
	for i := 0; i < paddedWidth; i = next {
		var (
			result     byte
			minorWidth int
		)
		next = i + 8
		if remaining := width - i; remaining > 8 {
			minorWidth = 8
		} else {
			minorWidth = remaining
		}
		if lineNumber > 0 {
			line1 <<= 8
			if next < width {
				b, err = gr.Bitmap.GetByte(prevByteIndex + 1)
				if err != nil {
					return jbig2errors.Wrap(err, processName, "line > 0")
				}
				line1 |= int(b)
			}
		}
		if lineNumber > 1 {
			rowStride := prevByteIndex - gr.Bitmap.RowStride + 1
			line2 <<= 8
			if next < width {
				b, err = gr.Bitmap.GetByte(rowStride)
				if err != nil {
					return jbig2errors.Wrap(err, processName, "line > 1")
				}
				line2 |= int(b) << 6
			} else {
				line2 |= 0
			}
		}
		for j := 0; j < minorWidth; j++ {
			shift := uint(7 - j)
			if gr.hasAtOverride {
				overriddenContext = gr.overrideAtTemplate0a(context, i+j, lineNumber, int(result), j, int(shift))
				gr.cx.SetIndex(int32(overriddenContext))
			} else {
				gr.cx.SetIndex(int32(context))
			}
			var bit int
			bit, err = gr.arithDecoder.DecodeBit(gr.cx)
			if err != nil {
				return jbig2errors.Wrap(err, processName, "")
			}
			result |= byte(bit) << shift
			context = ((context & 0x7bf7) << 1) | bit | ((line1 >> shift) & 0x10) | ((line2 >> shift) & 0x800)
		}
		if err := gr.Bitmap.SetByte(byteIndex, result); err != nil {
			return jbig2errors.Wrap(err, processName, "")
		}
		byteIndex++
		prevByteIndex++
	}
	return nil
}

func (pd *PatternDictionary) checkInput() error {
	if pd.HdpHeight < 1 || pd.HdpWidth < 1 {
		return errors.New("invalid Header Value: Width/Height must be greater than zero")
	}
	if pd.IsMMREncoded {
		if pd.HDTemplate != 0 {
			common.Log.Debug("variable HDTemplate should not contain the value 0")
		}
	}
	return nil
}

func (sd *SymbolDictionary) readAtPixels(count int) error {
	sd.SdATX = make([]int8, count)
	sd.SdATY = make([]int8, count)
	var (
		v   byte
		err error
	)
	for i := 0; i < count; i++ {
		v, err = sd.reader.ReadByte()
		if err != nil {
			return err
		}
		sd.SdATX[i] = int8(v)
		v, err = sd.reader.ReadByte()
		if err != nil {
			return err
		}
		sd.SdATY[i] = int8(v)
	}
	return nil
}

func (t *template1) form(refAbove, refMid, refBelow, regionAbove, lastBit int16) int16 {
	return ((refAbove & 0x02) << 8) | (refMid << 6) | ((refBelow & 0x03) << 4) | (regionAbove << 1) | lastBit
}
func (t *template0) setIndex(ds *arithmeticdecoder.DecoderStats) { ds.SetIndex(0x100) }
func (sd *SymbolDictionary) setRefinementAtPixels() error {
	if !sd.UseRefinementAggregation || sd.SdrTemplate != 0 {
		return nil
	}
	if err := sd.readRefinementAtPixels(2); err != nil {
		return err
	}
	return nil
}

func (eos *EndOfStripe) Init(h *Header, r *bitwise.Reader) error {
	eos.reader = r
	return eos.parseHeader()
}

func (sd *SymbolDictionary) encodeATFlags(bw bitwise.BinaryWriter) (n int, err error) {
	const processName = "encodeATFlags"
	if sd.IsHuffmanEncoded || sd.SdTemplate != 0 {
		return 0, nil
	}
	count := 4
	if sd.SdTemplate != 0 {
		count = 1
	}
	for i := 0; i < count; i++ {
		if err = bw.WriteByte(byte(sd.SdATX[i])); err != nil {
			return n, jbig2errors.Wrapf(err, processName, "SdATX[%d]", i)
		}
		n++
		if err = bw.WriteByte(byte(sd.SdATY[i])); err != nil {
			return n, jbig2errors.Wrapf(err, processName, "SdATY[%d]", i)
		}
		n++
	}
	return n, nil
}

func (sd *SymbolDictionary) decodeHeightClassDeltaHeight() (int64, error) {
	if sd.IsHuffmanEncoded {
		return sd.decodeHeightClassDeltaHeightWithHuffman()
	}
	v, err := sd.arithDecoder.DecodeInt(sd.cxIADH)
	if err != nil {
		return 0, err
	}
	return int64(v), nil
}

func (grr *GenericRefinementRegion) overrideAtTemplate0(context, x, y, result, bitIndex int) int {
	if grr.atOverride[0] {
		context &= 0xfff7
		if grr.GrAtY[0] == 0 && int(grr.GrAtX[0]) >= -bitIndex {
			context |= (result >> uint(7-(bitIndex+int(grr.GrAtX[0]))) & 0x1) << 3
		} else {
			context |= grr.getPixel(grr.RegionBitmap, x+int(grr.GrAtX[0]), y+int(grr.GrAtY[0])) << 3
		}
	}
	if grr.atOverride[1] {
		context &= 0xefff
		if grr.GrAtY[1] == 0 && int(grr.GrAtX[1]) >= -bitIndex {
			context |= (result >> uint(7-(bitIndex+int(grr.GrAtX[1]))) & 0x1) << 12
		} else {
			context |= grr.getPixel(grr.ReferenceBitmap, x+int(grr.GrAtX[1]), y+int(grr.GrAtY[1]))
		}
	}
	return context
}

func (eos *EndOfStripe) parseHeader() error {
	v, err := eos.reader.ReadBits(32)
	if err != nil {
		return err
	}
	eos.lineNumber = int(v & math.MaxInt32)
	return nil
}

func (grr *GenericRefinementRegion) decodeTypicalPredictedLine(lineNumber, width, rowStride, refRowStride, paddedWidth, deltaRefStride int) error {
	referenceDY := lineNumber - int(grr.ReferenceDY)
	byteIndex := grr.ReferenceBitmap.GetByteIndex(0, referenceDY)
	regionByteIndex := grr.RegionBitmap.GetByteIndex(0, lineNumber)
	var err error
	switch grr.TemplateID {
	case 0:
		err = grr.decodeTypicalPredictedLineTemplate0(lineNumber, width, rowStride, refRowStride, paddedWidth, deltaRefStride, regionByteIndex, referenceDY, byteIndex)
	case 1:
		err = grr.decodeTypicalPredictedLineTemplate1(lineNumber, width, rowStride, refRowStride, paddedWidth, deltaRefStride, regionByteIndex, referenceDY, byteIndex)
	}
	return err
}

func (pd *PatternDictionary) extractPatterns(bm *bitmap.Bitmap) error {
	var i int
	bms := make([]*bitmap.Bitmap, pd.GrayMax+1)
	for i <= int(pd.GrayMax) {
		hdpWidth := int(pd.HdpWidth) * i
		rect := image.Rect(hdpWidth, 0, hdpWidth+int(pd.HdpWidth), int(pd.HdpHeight))
		pattern, err := bitmap.Extract(rect, bm)
		if err != nil {
			return err
		}
		bms[i] = pattern
		i++
	}
	pd.Patterns = bms
	return nil
}

func (tr *TextRegion) decodeDT() (dt int64, err error) {
	if tr.IsHuffmanEncoded {
		if tr.SbHuffDT == 3 {
			dt, err = tr.huffDT.Decode(tr.reader)
			if err != nil {
				return 0, err
			}
		} else {
			var table huffman.Tabler
			table, err = huffman.GetStandardTable(11 + int(tr.SbHuffDT))
			if err != nil {
				return 0, err
			}
			dt, err = table.Decode(tr.reader)
			if err != nil {
				return 0, err
			}
		}
	} else {
		var v int32
		v, err = tr.arithDecoder.DecodeInt(tr.cxIADT)
		if err != nil {
			return 0, err
		}
		dt = int64(v)
	}
	dt *= int64(tr.SbStrips)
	return dt, nil
}

var (
	_ Regioner  = &TextRegion{}
	_ Segmenter = &TextRegion{}
)

func (grr *GenericRefinementRegion) getPixel(bm *bitmap.Bitmap, x, y int) int {
	if x < 0 || x >= bm.Width {
		return 0
	}
	if y < 0 || y >= bm.Height {
		return 0
	}
	if bm.GetPixel(x, y) {
		return 1
	}
	return 0
}
func (ts *TableSegment) HtOOB() int32 { return ts.htOOB }
func (gr *GenericRegion) decodeTemplate0b(lineNumber, width, paddedWidth int, byteIndex, prevByteIndex int) (err error) {
	const processName = "decodeTemplate0b"
	var (
		context, overriddenContext int
		line1, line2               int
		b                          byte
		next                       int
	)
	if lineNumber >= 1 {
		b, err = gr.Bitmap.GetByte(prevByteIndex)
		if err != nil {
			return jbig2errors.Wrap(err, processName, "line >= 1")
		}
		line1 = int(b)
	}
	if lineNumber >= 2 {
		b, err = gr.Bitmap.GetByte(prevByteIndex - gr.Bitmap.RowStride)
		if err != nil {
			return jbig2errors.Wrap(err, processName, "line >= 2")
		}
		line2 = int(b) << 6
	}
	context = (line1 & 0xf0) | (line2 & 0x3800)
	for i := 0; i < paddedWidth; i = next {
		var (
			result     byte
			minorWidth int
		)
		next = i + 8
		if remaining := width - i; remaining > 8 {
			minorWidth = 8
		} else {
			minorWidth = remaining
		}
		if lineNumber > 0 {
			line1 <<= 8
			if next < width {
				b, err = gr.Bitmap.GetByte(prevByteIndex + 1)
				if err != nil {
					return jbig2errors.Wrap(err, processName, "line > 0")
				}
				line1 |= int(b)
			}
		}
		if lineNumber > 1 {
			line2 <<= 8
			if next < width {
				b, err = gr.Bitmap.GetByte(prevByteIndex - gr.Bitmap.RowStride + 1)
				if err != nil {
					return jbig2errors.Wrap(err, processName, "line > 1")
				}
				line2 |= int(b) << 6
			}
		}
		for j := 0; j < minorWidth; j++ {
			shift := uint(7 - j)
			if gr.hasAtOverride {
				overriddenContext = gr.overrideAtTemplate0b(context, i+j, lineNumber, int(result), j, int(shift))
				gr.cx.SetIndex(int32(overriddenContext))
			} else {
				gr.cx.SetIndex(int32(context))
			}
			var bit int
			bit, err = gr.arithDecoder.DecodeBit(gr.cx)
			if err != nil {
				return jbig2errors.Wrap(err, processName, "")
			}
			result |= byte(bit << shift)
			context = ((context & 0x7bf7) << 1) | bit | ((line1 >> shift) & 0x10) | ((line2 >> shift) & 0x800)
		}
		if err := gr.Bitmap.SetByte(byteIndex, result); err != nil {
			return jbig2errors.Wrap(err, processName, "")
		}
		byteIndex++
		prevByteIndex++
	}
	return nil
}

func (h *Header) writeFlags(bw bitwise.BinaryWriter) (err error) {
	const processName = "Header.writeFlags"
	typeByte := byte(h.Type)
	if err = bw.WriteByte(typeByte); err != nil {
		return jbig2errors.Wrap(err, processName, "writing segment type number failed")
	}
	if !h.RetainFlag && !h.PageAssociationFieldSize {
		return nil
	}
	if err = bw.SkipBits(-8); err != nil {
		return jbig2errors.Wrap(err, processName, "skipping back the bits failed")
	}
	var bit int
	if h.RetainFlag {
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return jbig2errors.Wrap(err, processName, "retain retain flags")
	}
	bit = 0
	if h.PageAssociationFieldSize {
		bit = 1
	}
	if err = bw.WriteBit(bit); err != nil {
		return jbig2errors.Wrap(err, processName, "page association flag")
	}
	bw.FinishByte()
	return nil
}

var _ SegmentEncoder = &GenericRegion{}

func (pd *PatternDictionary) readGrayMax() error {
	v, err := pd.reader.ReadBits(32)
	if err != nil {
		return err
	}
	pd.GrayMax = uint32(v & math.MaxUint32)
	return nil
}

func (pd *PatternDictionary) parseHeader() error {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("[PATTERN-DICTIONARY][parseHeader] begin")
	}
	defer func() {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("[PATTERN-DICTIONARY][parseHeader] finished")
		}
	}()
	_, err := pd.reader.ReadBits(5)
	if err != nil {
		return err
	}
	if err = pd.readTemplate(); err != nil {
		return err
	}
	if err = pd.readIsMMREncoded(); err != nil {
		return err
	}
	if err = pd.readPatternWidthAndHeight(); err != nil {
		return err
	}
	if err = pd.readGrayMax(); err != nil {
		return err
	}
	if err = pd.computeSegmentDataStructure(); err != nil {
		return err
	}
	return pd.checkInput()
}

func (pis *PageInformationSegment) parseHeader() (err error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("[PageInformationSegment] ParsingHeader...")
	}
	defer func() {
		msg := "[PageInformationSegment] ParsingHeader Finished"
		if err != nil {
			msg += " with error " + err.Error()
		} else {
			msg += " successfully"
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace(msg)
		}
	}()
	if err = pis.readWidthAndHeight(); err != nil {
		return err
	}
	if err = pis.readResolution(); err != nil {
		return err
	}
	_, err = pis.reader.ReadBit()
	if err != nil {
		return err
	}
	if err = pis.readCombinationOperatorOverrideAllowed(); err != nil {
		return err
	}
	if err = pis.readRequiresAuxiliaryBuffer(); err != nil {
		return err
	}
	if err = pis.readCombinationOperator(); err != nil {
		return err
	}
	if err = pis.readDefaultPixelValue(); err != nil {
		return err
	}
	if err = pis.readContainsRefinement(); err != nil {
		return err
	}
	if err = pis.readIsLossless(); err != nil {
		return err
	}
	if err = pis.readIsStriped(); err != nil {
		return err
	}
	if err = pis.readMaxStripeSize(); err != nil {
		return err
	}
	if err = pis.checkInput(); err != nil {
		return err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("%s", pis)
	}
	return nil
}
