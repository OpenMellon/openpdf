// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package segments

import (
	"fmt"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	arithmeticdecoder "github.com/OpenMellon/openpdf/internal/jbig2/decoder/arithmetic"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder/huffman"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

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
