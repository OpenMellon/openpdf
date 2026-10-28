// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package segments

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder/huffman"
	arithmeticencoder "github.com/OpenMellon/openpdf/internal/jbig2/encoder/arithmetic"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
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
