// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package segments

import (
	"encoding/binary"
	"math"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder/huffman"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

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
