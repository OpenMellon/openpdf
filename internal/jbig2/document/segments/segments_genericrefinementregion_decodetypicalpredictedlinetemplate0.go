// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package segments

import (
	"math"

	"github.com/OpenMellon/openpdf/internal/bitwise"
)

func (grr *GenericRefinementRegion) decodeTypicalPredictedLineTemplate0(lineNumber, width, rowStride, refRowStride, paddedWidth, deltaRefStride, byteIndex, referenceDY, refByteIndex int) error {
	var (
		context, overriddenContext, regionAbove, refAbove, refMid, refBelow int
		b                                                                   byte
		err                                                                 error
	)
	if lineNumber > 0 {
		b, err = grr.RegionBitmap.GetByte(byteIndex - rowStride)
		if err != nil {
			return err
		}
		regionAbove = int(b)
	}
	if referenceDY > 0 && referenceDY <= grr.ReferenceBitmap.Height {
		b, err = grr.ReferenceBitmap.GetByte(refByteIndex - refRowStride + deltaRefStride)
		if err != nil {
			return err
		}
		refAbove = int(b) << 4
	}
	if referenceDY >= 0 && referenceDY < grr.ReferenceBitmap.Height {
		b, err = grr.ReferenceBitmap.GetByte(refByteIndex + deltaRefStride)
		if err != nil {
			return err
		}
		refMid = int(b) << 1
	}
	if referenceDY > -2 && referenceDY < grr.ReferenceBitmap.Height-1 {
		b, err = grr.ReferenceBitmap.GetByte(refByteIndex + refRowStride + deltaRefStride)
		if err != nil {
			return err
		}
		refBelow = int(b)
	}
	context = ((regionAbove >> 5) & 0x6) | ((refBelow >> 2) & 0x30) | (refMid & 0x180) | (refAbove & 0xc00)
	var nextPos int
	for i := 0; i < paddedWidth; i = nextPos {
		var decoded int
		nextPos = i + 8
		var bitCount int
		if bitCount = width - i; bitCount > 8 {
			bitCount = 8
		}
		hasNextRegion := nextPos < width
		hasNextRef := nextPos < grr.ReferenceBitmap.Width
		nextDelta := deltaRefStride + 1
		if lineNumber > 0 {
			b = 0
			if hasNextRegion {
				b, err = grr.RegionBitmap.GetByte(byteIndex - rowStride + 1)
				if err != nil {
					return err
				}
			}
			regionAbove = (regionAbove << 8) | int(b)
		}
		if referenceDY > 0 && referenceDY <= grr.ReferenceBitmap.Height {
			var refAboveByte int
			if hasNextRef {
				b, err = grr.ReferenceBitmap.GetByte(refByteIndex - refRowStride + nextDelta)
				if err != nil {
					return err
				}
				refAboveByte = int(b) << 4
			}
			refAbove = (refAbove << 8) | refAboveByte
		}
		if referenceDY >= 0 && referenceDY < grr.ReferenceBitmap.Height {
			var refMidByte int
			if hasNextRef {
				b, err = grr.ReferenceBitmap.GetByte(refByteIndex + nextDelta)
				if err != nil {
					return err
				}
				refMidByte = int(b) << 1
			}
			refMid = (refMid << 8) | refMidByte
		}
		if referenceDY > -2 && referenceDY < (grr.ReferenceBitmap.Height-1) {
			b = 0
			if hasNextRef {
				b, err = grr.ReferenceBitmap.GetByte(refByteIndex + refRowStride + nextDelta)
				if err != nil {
					return err
				}
			}
			refBelow = (refBelow << 8) | int(b)
		}
		for j := 0; j < bitCount; j++ {
			var bit int
			predicted := false
			typicalContext := (context >> 4) & 0x1ff
			switch typicalContext {
			case 0x1ff:
				predicted = true
				bit = 1
			case 0x00:
				predicted = true
			}
			if !predicted {
				if grr.hasAtOverride {
					overriddenContext = grr.overrideAtTemplate0(context, i+j, lineNumber, decoded, j)
					grr.cx.SetIndex(int32(overriddenContext))
				} else {
					grr.cx.SetIndex(int32(context))
				}
				bit, err = grr.arithDecoder.DecodeBit(grr.cx)
				if err != nil {
					return err
				}
			}
			shift := uint(7 - j)
			decoded |= bit << shift
			context = ((context & 0xdb6) << 1) | bit | (regionAbove>>shift+5)&0x002 | ((refBelow>>shift + 2) & 0x010) | ((refMid >> shift) & 0x080) | ((refAbove >> shift) & 0x400)
		}
		err = grr.RegionBitmap.SetByte(byteIndex, byte(decoded))
		if err != nil {
			return err
		}
		byteIndex++
		refByteIndex++
	}
	return nil
}

type SegmentEncoder interface {
	Encode(bw bitwise.BinaryWriter) (n int, err error)
}

func (hr *HalftoneRegion) shiftAndFill(value int) int {
	value >>= 8
	if value < 0 {
		topBit := int(math.Log(float64(highestBit(value))) / math.Log(2))
		signBits := 31 - topBit
		for i := 1; i < signBits; i++ {
			value |= 1 << uint(31-i)
		}
	}
	return value
}

const (
	TSymbolDictionary                         Type = 0
	TIntermediateTextRegion                   Type = 4
	TImmediateTextRegion                      Type = 6
	TImmediateLosslessTextRegion              Type = 7
	TPatternDictionary                        Type = 16
	TIntermediateHalftoneRegion               Type = 20
	TImmediateHalftoneRegion                  Type = 22
	TImmediateLosslessHalftoneRegion          Type = 23
	TIntermediateGenericRegion                Type = 36
	TImmediateGenericRegion                   Type = 38
	TImmediateLosslessGenericRegion           Type = 39
	TIntermediateGenericRefinementRegion      Type = 40
	TImmediateGenericRefinementRegion         Type = 42
	TImmediateLosslessGenericRefinementRegion Type = 43
	TPageInformation                          Type = 48
	TEndOfPage                                Type = 49
	TEndOfStrip                               Type = 50
	TEndOfFile                                Type = 51
	TProfiles                                 Type = 52
	TTables                                   Type = 53
	TExtension                                Type = 62
	TBitmap                                   Type = 70
)
