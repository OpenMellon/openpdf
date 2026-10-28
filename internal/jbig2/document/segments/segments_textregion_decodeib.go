// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package segments

import (
	"fmt"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	arithmeticdecoder "github.com/OpenMellon/openpdf/internal/jbig2/decoder/arithmetic"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

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
