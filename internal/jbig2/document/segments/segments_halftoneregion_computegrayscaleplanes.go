// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package segments

import (
	"errors"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	arithmeticdecoder "github.com/OpenMellon/openpdf/internal/jbig2/decoder/arithmetic"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder/huffman"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
	jbig2internal "github.com/OpenMellon/openpdf/internal/jbig2/internal"
)

func (hr *HalftoneRegion) computeGrayScalePlanes(bms []*bitmap.Bitmap, bitsPerValue int) ([][]int, error) {
	grayScale := make([][]int, hr.HGridHeight)
	for i := 0; i < len(grayScale); i++ {
		grayScale[i] = make([]int, hr.HGridWidth)
	}
	for i := 0; i < int(hr.HGridHeight); i++ {
		for j := 0; j < int(hr.HGridWidth); j += 8 {
			var colCount int
			if remaining := int(hr.HGridWidth) - j; remaining > 8 {
				colCount = 8
			} else {
				colCount = remaining
			}
			byteIndex := bms[0].GetByteIndex(j, i)
			for k := 0; k < colCount; k++ {
				col := k + j
				grayScale[i][col] = 0
				for l := 0; l < bitsPerValue; l++ {
					b, err := bms[l].GetByte(byteIndex)
					if err != nil {
						return nil, err
					}
					shifted := b >> uint(7-col&7)
					bit := shifted & 1
					weight := 1 << uint(l)
					value := int(bit) * weight
					grayScale[i][col] += value
				}
			}
		}
	}
	return grayScale, nil
}

func (gr *GenericRegion) updateOverrideFlags() error {
	const processName = "updateOverrideFlags"
	if gr.GBAtX == nil || gr.GBAtY == nil {
		return nil
	}
	if len(gr.GBAtX) != len(gr.GBAtY) {
		return jbig2errors.Errorf(processName, "incosistent AT pixel. Amount of 'x' pixels: %d, Amount of 'y' pixels: %d", len(gr.GBAtX), len(gr.GBAtY))
	}
	gr.GBAtOverride = make([]bool, len(gr.GBAtX))
	switch gr.GBTemplate {
	case 0:
		if !gr.UseExtTemplates {
			if gr.GBAtX[0] != 3 || gr.GBAtY[0] != -1 {
				gr.setOverrideFlag(0)
			}
			if gr.GBAtX[1] != -3 || gr.GBAtY[1] != -1 {
				gr.setOverrideFlag(1)
			}
			if gr.GBAtX[2] != 2 || gr.GBAtY[2] != -2 {
				gr.setOverrideFlag(2)
			}
			if gr.GBAtX[3] != -2 || gr.GBAtY[3] != -2 {
				gr.setOverrideFlag(3)
			}
		} else {
			if gr.GBAtX[0] != -2 || gr.GBAtY[0] != 0 {
				gr.setOverrideFlag(0)
			}
			if gr.GBAtX[1] != 0 || gr.GBAtY[1] != -2 {
				gr.setOverrideFlag(1)
			}
			if gr.GBAtX[2] != -2 || gr.GBAtY[2] != -1 {
				gr.setOverrideFlag(2)
			}
			if gr.GBAtX[3] != -1 || gr.GBAtY[3] != -2 {
				gr.setOverrideFlag(3)
			}
			if gr.GBAtX[4] != 1 || gr.GBAtY[4] != -2 {
				gr.setOverrideFlag(4)
			}
			if gr.GBAtX[5] != 2 || gr.GBAtY[5] != -1 {
				gr.setOverrideFlag(5)
			}
			if gr.GBAtX[6] != -3 || gr.GBAtY[6] != 0 {
				gr.setOverrideFlag(6)
			}
			if gr.GBAtX[7] != -4 || gr.GBAtY[7] != 0 {
				gr.setOverrideFlag(7)
			}
			if gr.GBAtX[8] != 2 || gr.GBAtY[8] != -2 {
				gr.setOverrideFlag(8)
			}
			if gr.GBAtX[9] != 3 || gr.GBAtY[9] != -1 {
				gr.setOverrideFlag(9)
			}
			if gr.GBAtX[10] != -2 || gr.GBAtY[10] != -2 {
				gr.setOverrideFlag(10)
			}
			if gr.GBAtX[11] != -3 || gr.GBAtY[11] != -1 {
				gr.setOverrideFlag(11)
			}
		}
	case 1:
		if gr.GBAtX[0] != 3 || gr.GBAtY[0] != -1 {
			gr.setOverrideFlag(0)
		}
	case 2:
		if gr.GBAtX[0] != 2 || gr.GBAtY[0] != -1 {
			gr.setOverrideFlag(0)
		}
	case 3:
		if gr.GBAtX[0] != 2 || gr.GBAtY[0] != -1 {
			gr.setOverrideFlag(0)
		}
	}
	return nil
}

func (t Type) String() string {
	switch t {
	case TSymbolDictionary:
		return "Symbol Dictionary"
	case TIntermediateTextRegion:
		return "Intermediate Text Region"
	case TImmediateTextRegion:
		return "Immediate Text Region"
	case TImmediateLosslessTextRegion:
		return "Immediate Lossless Text Region"
	case TPatternDictionary:
		return "Pattern Dictionary"
	case TIntermediateHalftoneRegion:
		return "Intermediate Halftone Region"
	case TImmediateHalftoneRegion:
		return "Immediate Halftone Region"
	case TImmediateLosslessHalftoneRegion:
		return "Immediate Lossless Halftone Region"
	case TIntermediateGenericRegion:
		return "Intermediate Generic Region"
	case TImmediateGenericRegion:
		return "Immediate Generic Region"
	case TImmediateLosslessGenericRegion:
		return "Immediate Lossless Generic Region"
	case TIntermediateGenericRefinementRegion:
		return "Intermediate Generic Refinement Region"
	case TImmediateGenericRefinementRegion:
		return "Immediate Generic Refinement Region"
	case TImmediateLosslessGenericRefinementRegion:
		return "Immediate Lossless Generic Refinement Region"
	case TPageInformation:
		return "Page Information"
	case TEndOfPage:
		return "End Of Page"
	case TEndOfStrip:
		return "End Of Strip"
	case TEndOfFile:
		return "End Of File"
	case TProfiles:
		return "Profiles"
	case TTables:
		return "Tables"
	case TExtension:
		return "Extension"
	case TBitmap:
		return "Bitmap"
	}
	return "Invalid Segment Kind"
}
func NewRegionSegment(r *bitwise.Reader) *RegionSegment { return &RegionSegment{reader: r} }
func (eos *EndOfStripe) LineNumber() int                { return eos.lineNumber }
func (sd *SymbolDictionary) GetDictionary() ([]*bitmap.Bitmap, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("[SYMBOL-DICTIONARY] GetDictionary begins...")
	}
	defer func() {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("[SYMBOL-DICTIONARY] GetDictionary finished")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("[SYMBOL-DICTIONARY] Dictionary. \nEx: '%s', \nnew:'%s'", sd.exportedSymbols, sd.newSymbols)
		}
	}()
	if sd.exportedSymbols == nil {
		var err error
		if sd.UseRefinementAggregation {
			sd.symbolCodeLength = sd.getSbSymCodeLen()
		}
		if !sd.IsHuffmanEncoded {
			if err = sd.setCodingStatistics(); err != nil {
				return nil, err
			}
		}
		sd.newSymbols = make([]*bitmap.Bitmap, sd.NumberOfNewSymbols)
		var newSymbolWidths []int
		if sd.IsHuffmanEncoded && !sd.UseRefinementAggregation {
			newSymbolWidths = make([]int, sd.NumberOfNewSymbols)
		}
		if err = sd.setSymbolsArray(); err != nil {
			return nil, err
		}
		var hcHeight, hcDeltaHeight int64
		sd.numberOfDecodedSymbols = 0
		for sd.numberOfDecodedSymbols < sd.NumberOfNewSymbols {
			hcDeltaHeight, err = sd.decodeHeightClassDeltaHeight()
			if err != nil {
				return nil, err
			}
			hcHeight += hcDeltaHeight
			var symWidth, totalWidth uint32
			hcFirstSymbol := int64(sd.numberOfDecodedSymbols)
			for {
				var deltaWidth int64
				deltaWidth, err = sd.decodeDifferenceWidth()
				if errors.Is(err, jbig2internal.ErrOOB) {
					break
				}
				if err != nil {
					return nil, err
				}
				if sd.numberOfDecodedSymbols >= sd.NumberOfNewSymbols {
					break
				}
				symWidth += uint32(deltaWidth)
				totalWidth += symWidth
				if !sd.IsHuffmanEncoded || sd.UseRefinementAggregation {
					if !sd.UseRefinementAggregation {
						err = sd.decodeDirectlyThroughGenericRegion(symWidth, uint32(hcHeight))
						if err != nil {
							return nil, err
						}
					} else {
						err = sd.decodeAggregate(symWidth, uint32(hcHeight))
						if err != nil {
							return nil, err
						}
					}
				} else if sd.IsHuffmanEncoded && !sd.UseRefinementAggregation {
					newSymbolWidths[sd.numberOfDecodedSymbols] = int(symWidth)
				}
				sd.numberOfDecodedSymbols++
			}
			if sd.IsHuffmanEncoded && !sd.UseRefinementAggregation {
				var bmSize int64
				if sd.SdHuffBMSizeSelection == 0 {
					var table huffman.Tabler
					table, err = huffman.GetStandardTable(1)
					if err != nil {
						return nil, err
					}
					bmSize, err = table.Decode(sd.reader)
					if err != nil {
						return nil, err
					}
				} else {
					bmSize, err = sd.huffDecodeBmSize()
					if err != nil {
						return nil, err
					}
				}
				sd.reader.Align()
				var bm *bitmap.Bitmap
				bm, err = sd.decodeHeightClassCollectiveBitmap(bmSize, uint32(hcHeight), totalWidth)
				if err != nil {
					return nil, err
				}
				err = sd.decodeHeightClassBitmap(bm, hcFirstSymbol, int(hcHeight), newSymbolWidths)
				if err != nil {
					return nil, err
				}
			}
		}
		toExportFlags, err := sd.getToExportFlags()
		if err != nil {
			return nil, err
		}
		sd.setExportedSymbols(toExportFlags)
	}
	return sd.exportedSymbols, nil
}

func (sd *SymbolDictionary) readNumberOfExportedSymbols() error {
	count, err := sd.reader.ReadBits(32)
	if err != nil {
		return err
	}
	sd.NumberOfExportedSymbols = uint32(count & math.MaxUint32)
	return nil
}

var _ templater = &template0{}

func (gr *GenericRegion) copyLineAbove(lineNumber int) error {
	dstIndex := lineNumber * gr.Bitmap.RowStride
	srcIndex := dstIndex - gr.Bitmap.RowStride
	for i := 0; i < gr.Bitmap.RowStride; i++ {
		b, err := gr.Bitmap.GetByte(srcIndex)
		if err != nil {
			return err
		}
		srcIndex++
		if err = gr.Bitmap.SetByte(dstIndex, b); err != nil {
			return err
		}
		dstIndex++
	}
	return nil
}

func NewHeader(d Documenter, r *bitwise.Reader, offset int64, organizationType OrganizationType) (*Header, error) {
	header := &Header{Reader: r}
	if err := header.parse(d, r, offset, organizationType); err != nil {
		return nil, jbig2errors.Wrap(err, "NewHeader", "")
	}
	return header, nil
}

func (grr *GenericRefinementRegion) updateOverride() error {
	if grr.GrAtX == nil || grr.GrAtY == nil {
		return errors.New("AT pixels not set")
	}
	if len(grr.GrAtX) != len(grr.GrAtY) {
		return errors.New("AT pixel inconsistent")
	}
	grr.atOverride = make([]bool, len(grr.GrAtX))
	switch grr.TemplateID {
	case 0:
		if grr.GrAtX[0] != -1 && grr.GrAtY[0] != -1 {
			grr.atOverride[0] = true
			grr.hasAtOverride = true
		}
		if grr.GrAtX[1] != -1 && grr.GrAtY[1] != -1 {
			grr.atOverride[1] = true
			grr.hasAtOverride = true
		}
	case 1:
		grr.hasAtOverride = false
	}
	return nil
}
func (gr *GenericRegion) GetRegionInfo() *RegionSegment { return gr.RegionSegment }
func (sd *SymbolDictionary) encodeRefinementATFlags(bw bitwise.BinaryWriter) (n int, err error) {
	const processName = "encodeRefinementATFlags"
	if !sd.UseRefinementAggregation || sd.SdrTemplate != 0 {
		return 0, nil
	}
	for i := 0; i < 2; i++ {
		if err = bw.WriteByte(byte(sd.SdrATX[i])); err != nil {
			return n, jbig2errors.Wrapf(err, processName, "SdrATX[%d]", i)
		}
		n++
		if err = bw.WriteByte(byte(sd.SdrATY[i])); err != nil {
			return n, jbig2errors.Wrapf(err, processName, "SdrATY[%d]", i)
		}
		n++
	}
	return n, nil
}

func (gr *GenericRegion) setParametersWithAt(isMMREncoded bool, gbTemplate byte, isTPGDon, useSkip bool, gbAtX, gbAtY []int8, bitmapWidth, bitmapHeight uint32, cx *arithmeticdecoder.DecoderStats, arithDecoder *arithmeticdecoder.Decoder) {
	gr.IsMMREncoded = isMMREncoded
	gr.GBTemplate = gbTemplate
	gr.IsTPGDon = isTPGDon
	gr.GBAtX = gbAtX
	gr.GBAtY = gbAtY
	gr.RegionSegment.BitmapHeight = bitmapHeight
	gr.RegionSegment.BitmapWidth = bitmapWidth
	gr.mmrDecoder = nil
	gr.Bitmap = nil
	if cx != nil {
		gr.cx = cx
	}
	if arithDecoder != nil {
		gr.arithDecoder = arithDecoder
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("[GENERIC-REGION] setParameters SDAt: %s", gr)
	}
}
