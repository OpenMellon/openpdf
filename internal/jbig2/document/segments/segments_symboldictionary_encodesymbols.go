// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package segments

import (
	"fmt"
	"slices"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	arithmeticdecoder "github.com/OpenMellon/openpdf/internal/jbig2/decoder/arithmetic"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder/huffman"
	arithmeticencoder "github.com/OpenMellon/openpdf/internal/jbig2/encoder/arithmetic"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

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
