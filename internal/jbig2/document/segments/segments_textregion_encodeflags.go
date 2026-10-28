// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package segments

import (
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	arithmeticdecoder "github.com/OpenMellon/openpdf/internal/jbig2/decoder/arithmetic"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder/huffman"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

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
