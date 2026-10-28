// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package segments

import (
	"encoding/binary"
	"math"

	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	arithmeticdecoder "github.com/OpenMellon/openpdf/internal/jbig2/decoder/arithmetic"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder/huffman"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

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
