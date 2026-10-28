// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package segments

import (
	"image"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder/huffman"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

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
