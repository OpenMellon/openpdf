// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package segments

import (
	"encoding/binary"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	arithmeticdecoder "github.com/OpenMellon/openpdf/internal/jbig2/decoder/arithmetic"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

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
