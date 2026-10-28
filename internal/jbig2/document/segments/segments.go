// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package segments

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	arithmeticdecoder "github.com/OpenMellon/openpdf/internal/jbig2/decoder/arithmetic"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder/mmr"
	arithmeticencoder "github.com/OpenMellon/openpdf/internal/jbig2/encoder/arithmetic"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
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
