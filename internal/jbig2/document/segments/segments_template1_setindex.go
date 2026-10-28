// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package segments

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	arithmeticdecoder "github.com/OpenMellon/openpdf/internal/jbig2/decoder/arithmetic"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder/huffman"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

func (t *template1) setIndex(ds *arithmeticdecoder.DecoderStats) { ds.SetIndex(0x080) }
func (h *Header) readSegmentNumber(r *bitwise.Reader) error {
	const processName = "readSegmentNumber"
	buf := make([]byte, 4)
	_, err := r.Read(buf)
	if err != nil {
		return jbig2errors.Wrap(err, processName, "")
	}
	h.SegmentNumber = binary.BigEndian.Uint32(buf)
	return nil
}

func (tr *TextRegion) readHuffmanFlags() error {
	var (
		bit  int
		bits uint64
		err  error
	)
	_, err = tr.reader.ReadBit()
	if err != nil {
		return err
	}
	bit, err = tr.reader.ReadBit()
	if err != nil {
		return err
	}
	tr.SbHuffRSize = int8(bit)
	bits, err = tr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	tr.SbHuffRDY = int8(bits) & 0xf
	bits, err = tr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	tr.SbHuffRDX = int8(bits) & 0xf
	bits, err = tr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	tr.SbHuffRDHeight = int8(bits) & 0xf
	bits, err = tr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	tr.SbHuffRDWidth = int8(bits) & 0xf
	bits, err = tr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	tr.SbHuffDT = int8(bits) & 0xf
	bits, err = tr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	tr.SbHuffDS = int8(bits) & 0xf
	bits, err = tr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	tr.SbHuffFS = int8(bits) & 0xf
	return nil
}

func (grr *GenericRefinementRegion) String() string {
	builder := &strings.Builder{}
	builder.WriteString("\n[GENERIC REGION]\n")
	builder.WriteString(grr.RegionInfo.String() + "\n")
	fmt.Fprintf(builder, "\t- IsTPGRon: %v\n", grr.IsTPGROn)
	fmt.Fprintf(builder, "\t- TemplateID: %v\n", grr.TemplateID)
	fmt.Fprintf(builder, "\t- GrAtX: %v\n", grr.GrAtX)
	fmt.Fprintf(builder, "\t- GrAtY: %v\n", grr.GrAtY)
	fmt.Fprintf(builder, "\t- ReferenceDX %v\n", grr.ReferenceDX)
	fmt.Fprintf(builder, "\t- ReferencDeY: %v\n", grr.ReferenceDY)
	return builder.String()
}
func (hr *HalftoneRegion) GetRegionInfo() *RegionSegment { return hr.RegionSegment }

type GenericRefinementRegion struct {
	refTemplate0    templater
	refTemplate1    templater
	reader          *bitwise.Reader
	header          *Header
	RegionInfo      *RegionSegment
	IsTPGROn        bool
	TemplateID      int8
	Template        templater
	GrAtX           []int8
	GrAtY           []int8
	RegionBitmap    *bitmap.Bitmap
	ReferenceBitmap *bitmap.Bitmap
	ReferenceDX     int32
	ReferenceDY     int32
	arithDecoder    *arithmeticdecoder.Decoder
	cx              *arithmeticdecoder.DecoderStats
	hasAtOverride   bool
	atOverride      []bool
}

func (grr *GenericRefinementRegion) Init(header *Header, r *bitwise.Reader) error {
	grr.header = header
	grr.reader = r
	grr.RegionInfo = NewRegionSegment(r)
	return grr.parseHeader()
}

func (pis *PageInformationSegment) String() string {
	builder := &strings.Builder{}
	builder.WriteString("\n[PAGE-INFORMATION-SEGMENT]\n")
	fmt.Fprintf(builder, "\t- BMHeight: %d\n", pis.PageBMHeight)
	fmt.Fprintf(builder, "\t- BMWidth: %d\n", pis.PageBMWidth)
	fmt.Fprintf(builder, "\t- ResolutionX: %d\n", pis.ResolutionX)
	fmt.Fprintf(builder, "\t- ResolutionY: %d\n", pis.ResolutionY)
	fmt.Fprintf(builder, "\t- CombinationOperator: %s\n", pis.combinationOperator)
	fmt.Fprintf(builder, "\t- CombinationOperatorOverride: %v\n", pis.combinationOperatorOverride)
	fmt.Fprintf(builder, "\t- IsLossless: %v\n", pis.IsLossless)
	fmt.Fprintf(builder, "\t- RequiresAuxiliaryBuffer: %v\n", pis.requiresAuxiliaryBuffer)
	fmt.Fprintf(builder, "\t- MightContainRefinements: %v\n", pis.mightContainRefinements)
	fmt.Fprintf(builder, "\t- IsStriped: %v\n", pis.IsStripe)
	fmt.Fprintf(builder, "\t- MaxStripeSize: %v\n", pis.MaxStripeSize)
	return builder.String()
}

func (gr *GenericRegion) String() string {
	builder := &strings.Builder{}
	builder.WriteString("\n[GENERIC REGION]\n")
	builder.WriteString(gr.RegionSegment.String() + "\n")
	fmt.Fprintf(builder, "\t- UseExtTemplates: %v\n", gr.UseExtTemplates)
	fmt.Fprintf(builder, "\t- IsTPGDon: %v\n", gr.IsTPGDon)
	fmt.Fprintf(builder, "\t- GBTemplate: %d\n", gr.GBTemplate)
	fmt.Fprintf(builder, "\t- IsMMREncoded: %v\n", gr.IsMMREncoded)
	fmt.Fprintf(builder, "\t- GBAtX: %v\n", gr.GBAtX)
	fmt.Fprintf(builder, "\t- GBAtY: %v\n", gr.GBAtY)
	fmt.Fprintf(builder, "\t- GBAtOverride: %v\n", gr.GBAtOverride)
	return builder.String()
}
func (ts *TableSegment) HtHigh() int32 { return ts.htHigh }
func (gr *GenericRegion) decodeTemplate3(lineNumber, width, paddedWidth int, byteIndex, prevByteIndex int) (err error) {
	const processName = "decodeTemplate3"
	var (
		context, overriddenContext int
		line1                      int
		b                          byte
		next, bit                  int
	)
	if lineNumber >= 1 {
		b, err = gr.Bitmap.GetByte(prevByteIndex)
		if err != nil {
			return jbig2errors.Wrap(err, processName, "line >= 1")
		}
		line1 = int(b)
	}
	context = (line1 >> 1) & 0x70
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
		if lineNumber >= 1 {
			line1 <<= 8
			if next < width {
				b, err = gr.Bitmap.GetByte(prevByteIndex + 1)
				if err != nil {
					return jbig2errors.Wrap(err, processName, "inner - line >= 1")
				}
				line1 |= int(b)
			}
		}
		for j := 0; j < minorWidth; j++ {
			if gr.hasAtOverride {
				overriddenContext = gr.overrideAtTemplate3(context, i+j, lineNumber, int(result), j)
				gr.cx.SetIndex(int32(overriddenContext))
			} else {
				gr.cx.SetIndex(int32(context))
			}
			bit, err = gr.arithDecoder.DecodeBit(gr.cx)
			if err != nil {
				return jbig2errors.Wrap(err, processName, "")
			}
			result |= byte(bit) << byte(7-j)
			context = ((context & 0x1f7) << 1) | bit | ((line1 >> uint(8-j)) & 0x010)
		}
		if err := gr.Bitmap.SetByte(byteIndex, result); err != nil {
			return jbig2errors.Wrap(err, processName, "")
		}
		byteIndex++
		prevByteIndex++
	}
	return nil
}

func (gr *GenericRegion) decodeTemplate2(lineNumber, width, paddedWidth int, byteIndex, prevByteIndex int) (err error) {
	const processName = "decodeTemplate2"
	var (
		context, overriddenContext int
		line1, line2               int
		b                          byte
		next, bit                  int
	)
	if lineNumber >= 1 {
		b, err = gr.Bitmap.GetByte(prevByteIndex)
		if err != nil {
			return jbig2errors.Wrap(err, processName, "lineNumber >= 1")
		}
		line1 = int(b)
	}
	if lineNumber >= 2 {
		b, err = gr.Bitmap.GetByte(prevByteIndex - gr.Bitmap.RowStride)
		if err != nil {
			return jbig2errors.Wrap(err, processName, "lineNumber >= 2")
		}
		line2 = int(b) << 4
	}
	context = (line1 >> 3 & 0x7c) | (line2 >> 3 & 0x380)
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
					return jbig2errors.Wrap(err, processName, "lineNumber > 0")
				}
				line1 |= int(b)
			}
		}
		if lineNumber > 1 {
			line2 <<= 8
			if next < width {
				b, err = gr.Bitmap.GetByte(prevByteIndex - gr.Bitmap.RowStride + 1)
				if err != nil {
					return jbig2errors.Wrap(err, processName, "lineNumber > 1")
				}
				line2 |= int(b) << 4
			}
		}
		for j := 0; j < minorWidth; j++ {
			shift := uint(10 - j)
			if gr.hasAtOverride {
				overriddenContext = gr.overrideAtTemplate2(context, i+j, lineNumber, int(result), j)
				gr.cx.SetIndex(int32(overriddenContext))
			} else {
				gr.cx.SetIndex(int32(context))
			}
			bit, err = gr.arithDecoder.DecodeBit(gr.cx)
			if err != nil {
				return jbig2errors.Wrap(err, processName, "")
			}
			result |= byte(bit << uint(7-j))
			context = ((context & 0x1bd) << 1) | bit | ((line1 >> shift) & 0x4) | ((line2 >> shift) & 0x80)
		}
		if err := gr.Bitmap.SetByte(byteIndex, result); err != nil {
			return jbig2errors.Wrap(err, processName, "")
		}
		byteIndex++
		prevByteIndex++
	}
	return nil
}

func (tr *TextRegion) decodeDfs() (int64, error) {
	if tr.IsHuffmanEncoded {
		if tr.SbHuffFS == 3 {
			if tr.huffFS == nil {
				var err error
				tr.huffFS, err = tr.getUserTable(0)
				if err != nil {
					return 0, err
				}
			}
			return tr.huffFS.Decode(tr.reader)
		}
		standardTable, err := huffman.GetStandardTable(6 + int(tr.SbHuffFS))
		if err != nil {
			return 0, err
		}
		return standardTable.Decode(tr.reader)
	}
	dfs, err := tr.arithDecoder.DecodeInt(tr.cxIAFS)
	if err != nil {
		return 0, err
	}
	return int64(dfs), nil
}

func (hr *HalftoneRegion) GetRegionBitmap() (*bitmap.Bitmap, error) {
	if hr.HalftoneRegionBitmap != nil {
		return hr.HalftoneRegionBitmap, nil
	}
	var err error
	hr.HalftoneRegionBitmap, err = bitmap.New(int(hr.RegionSegment.BitmapWidth), int(hr.RegionSegment.BitmapHeight))
	if err != nil {
		return nil, err
	}
	if hr.Patterns == nil || (hr.Patterns != nil && len(hr.Patterns) == 0) {
		hr.Patterns, err = hr.GetPatterns()
		if err != nil {
			return nil, err
		}
	}
	if hr.HDefaultPixel == 1 {
		hr.HalftoneRegionBitmap.SetDefaultPixel()
	}
	log2Patterns := math.Ceil(math.Log(float64(len(hr.Patterns))) / math.Log(2))
	bitsPerValue := int(log2Patterns)
	var grayScale [][]int
	grayScale, err = hr.grayScaleDecoding(bitsPerValue)
	if err != nil {
		return nil, err
	}
	if err = hr.renderPattern(grayScale); err != nil {
		return nil, err
	}
	return hr.HalftoneRegionBitmap, nil
}

func (tr *TextRegion) decodeStripT() (t int64, err error) {
	if tr.IsHuffmanEncoded {
		if tr.SbHuffDT == 3 {
			if tr.huffDT == nil {
				var userTableIndex int
				if tr.SbHuffFS == 3 {
					userTableIndex++
				}
				if tr.SbHuffDS == 3 {
					userTableIndex++
				}
				tr.huffDT, err = tr.getUserTable(userTableIndex)
				if err != nil {
					return 0, err
				}
			}
			t, err = tr.huffDT.Decode(tr.reader)
			if err != nil {
				return 0, err
			}
		} else {
			var table huffman.Tabler
			table, err = huffman.GetStandardTable(11 + int(tr.SbHuffDT))
			if err != nil {
				return 0, err
			}
			t, err = table.Decode(tr.reader)
			if err != nil {
				return 0, err
			}
		}
	} else {
		var dt int32
		dt, err = tr.arithDecoder.DecodeInt(tr.cxIADT)
		if err != nil {
			return 0, err
		}
		t = int64(dt)
	}
	t *= int64(-tr.SbStrips)
	return t, nil
}
