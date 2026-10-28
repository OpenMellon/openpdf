// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package segments

import (
	"fmt"
	"math"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder/huffman"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

func (gr *GenericRegion) decodeTemplate1(lineNumber, width, paddedWidth int, byteIndex, prevByteIndex int) (err error) {
	const processName = "decodeTemplate1"
	var (
		context, overriddenContext int
		line1, line2               int
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
	if lineNumber >= 2 {
		b, err = gr.Bitmap.GetByte(prevByteIndex - gr.Bitmap.RowStride)
		if err != nil {
			return jbig2errors.Wrap(err, processName, "line >= 2")
		}
		line2 = int(b) << 5
	}
	context = ((line1 >> 1) & 0x1f8) | ((line2 >> 1) & 0x1e00)
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
				line2 |= int(b) << 5
			}
		}
		for j := 0; j < minorWidth; j++ {
			if gr.hasAtOverride {
				overriddenContext = gr.overrideAtTemplate1(context, i+j, lineNumber, int(result), j)
				gr.cx.SetIndex(int32(overriddenContext))
			} else {
				gr.cx.SetIndex(int32(context))
			}
			bit, err = gr.arithDecoder.DecodeBit(gr.cx)
			if err != nil {
				return jbig2errors.Wrap(err, processName, "")
			}
			result |= byte(bit) << uint(7-j)
			shift := uint(8 - j)
			context = ((context & 0xefb) << 1) | bit | ((line1 >> shift) & 0x8) | ((line2 >> shift) & 0x200)
		}
		if err := gr.Bitmap.SetByte(byteIndex, result); err != nil {
			return jbig2errors.Wrap(err, processName, "")
		}
		byteIndex++
		prevByteIndex++
	}
	return nil
}

func (tr *TextRegion) initSymbols() error {
	const processName = "initSymbols"
	for _, rtSegment := range tr.Header.RTSegments {
		if rtSegment == nil {
			return jbig2errors.Error(processName, "nil segment provided for the text region Symbols")
		}
		if rtSegment.Type == 0 {
			segmentData, err := rtSegment.GetSegmentData()
			if err != nil {
				return jbig2errors.Wrap(err, processName, "")
			}
			sd, ok := segmentData.(*SymbolDictionary)
			if !ok {
				return jbig2errors.Error(processName, "referred To Segment is not a SymbolDictionary")
			}
			sd.cxIAID = tr.cxIAID
			dictionary, err := sd.GetDictionary()
			if err != nil {
				return jbig2errors.Wrap(err, processName, "")
			}
			tr.Symbols = append(tr.Symbols, dictionary...)
		}
	}
	tr.NumberOfSymbols = uint32(len(tr.Symbols))
	return nil
}

func (pis *PageInformationSegment) readIsStriped() error {
	bit, err := pis.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		pis.IsStripe = true
	}
	return nil
}

func (tr *TextRegion) Encode(w bitwise.BinaryWriter) (n int, err error) {
	const processName = "TextRegion.Encode"
	if n, err = tr.RegionInfo.Encode(w); err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	var m int
	if m, err = tr.encodeFlags(w); err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	n += m
	if m, err = tr.encodeSymbols(w); err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	n += m
	return n, nil
}

type Documenter interface {
	GetPage(int) (Pager, error)
	GetGlobalSegment(int) (*Header, error)
}

func (sd *SymbolDictionary) decodeHeightClassDeltaHeightWithHuffman() (int64, error) {
	switch sd.SdHuffDecodeHeightSelection {
	case 0:
		standardTable, err := huffman.GetStandardTable(4)
		if err != nil {
			return 0, err
		}
		return standardTable.Decode(sd.reader)
	case 1:
		standardTable, err := huffman.GetStandardTable(5)
		if err != nil {
			return 0, err
		}
		return standardTable.Decode(sd.reader)
	case 3:
		if sd.huffDH == nil {
			standardTable, err := huffman.GetStandardTable(0)
			if err != nil {
				return 0, err
			}
			sd.huffDH = standardTable
		}
		return sd.huffDH.Decode(sd.reader)
	}
	return 0, nil
}

func newTextRegion(reader *bitwise.Reader, header *Header) *TextRegion {
	textRegion := &TextRegion{reader: reader, Header: header, RegionInfo: NewRegionSegment(reader)}
	return textRegion
}

func (tr *TextRegion) readAmountOfSymbolInstances() error {
	count, err := tr.reader.ReadBits(32)
	if err != nil {
		return err
	}
	tr.NumberOfSymbolInstances = uint32(count & math.MaxUint32)
	pixels := tr.RegionInfo.BitmapWidth * tr.RegionInfo.BitmapHeight
	if pixels < tr.NumberOfSymbolInstances {
		common.Log.Debug("Limiting the number of decoded symbol instances to one per pixel ( %d instead of %d)", pixels, tr.NumberOfSymbolInstances)
		tr.NumberOfSymbolInstances = pixels
	}
	return nil
}

func (tr *TextRegion) String() string {
	builder := &strings.Builder{}
	builder.WriteString("\n[TEXT REGION]\n")
	builder.WriteString(tr.RegionInfo.String() + "\n")
	builder.WriteString(fmt.Sprintf("\t- SbrTemplate: %v\n", tr.SbrTemplate))
	builder.WriteString(fmt.Sprintf("\t- SbDsOffset: %v\n", tr.SbDsOffset))
	builder.WriteString(fmt.Sprintf("\t- DefaultPixel: %v\n", tr.DefaultPixel))
	builder.WriteString(fmt.Sprintf("\t- CombinationOperator: %v\n", tr.CombinationOperator))
	builder.WriteString(fmt.Sprintf("\t- IsTransposed: %v\n", tr.IsTransposed))
	builder.WriteString(fmt.Sprintf("\t- ReferenceCorner: %v\n", tr.ReferenceCorner))
	builder.WriteString(fmt.Sprintf("\t- UseRefinement: %v\n", tr.UseRefinement))
	builder.WriteString(fmt.Sprintf("\t- IsHuffmanEncoded: %v\n", tr.IsHuffmanEncoded))
	if tr.IsHuffmanEncoded {
		builder.WriteString(fmt.Sprintf("\t- SbHuffRSize: %v\n", tr.SbHuffRSize))
		builder.WriteString(fmt.Sprintf("\t- SbHuffRDY: %v\n", tr.SbHuffRDY))
		builder.WriteString(fmt.Sprintf("\t- SbHuffRDX: %v\n", tr.SbHuffRDX))
		builder.WriteString(fmt.Sprintf("\t- SbHuffRDHeight: %v\n", tr.SbHuffRDHeight))
		builder.WriteString(fmt.Sprintf("\t- SbHuffRDWidth: %v\n", tr.SbHuffRDWidth))
		builder.WriteString(fmt.Sprintf("\t- SbHuffDT: %v\n", tr.SbHuffDT))
		builder.WriteString(fmt.Sprintf("\t- SbHuffDS: %v\n", tr.SbHuffDS))
		builder.WriteString(fmt.Sprintf("\t- SbHuffFS: %v\n", tr.SbHuffFS))
	}
	builder.WriteString(fmt.Sprintf("\t- SbrATX: %v\n", tr.SbrATX))
	builder.WriteString(fmt.Sprintf("\t- SbrATY: %v\n", tr.SbrATY))
	builder.WriteString(fmt.Sprintf("\t- NumberOfSymbolInstances: %v\n", tr.NumberOfSymbolInstances))
	builder.WriteString(fmt.Sprintf("\t- SbrATX: %v\n", tr.SbrATX))
	return builder.String()
}

func (tr *TextRegion) decodeIds() (int64, error) {
	const processName = "decodeIds"
	if tr.IsHuffmanEncoded {
		if tr.SbHuffDS == 3 {
			if tr.huffDS == nil {
				userTableIndex := 0
				if tr.SbHuffFS == 3 {
					userTableIndex++
				}
				var err error
				tr.huffDS, err = tr.getUserTable(userTableIndex)
				if err != nil {
					return 0, jbig2errors.Wrap(err, processName, "")
				}
			}
			return tr.huffDS.Decode(tr.reader)
		}
		standardTable, err := huffman.GetStandardTable(8 + int(tr.SbHuffDS))
		if err != nil {
			return 0, jbig2errors.Wrap(err, processName, "")
		}
		return standardTable.Decode(tr.reader)
	}
	ids, err := tr.arithDecoder.DecodeInt(tr.cxIADS)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "cxIADS")
	}
	return int64(ids), nil
}

type Header struct {
	SegmentNumber            uint32
	Type                     Type
	RetainFlag               bool
	PageAssociation          int
	PageAssociationFieldSize bool
	RTSegments               []*Header
	HeaderLength             int64
	SegmentDataLength        uint64
	SegmentDataStartOffset   uint64
	Reader                   *bitwise.Reader
	SegmentData              Segmenter
	RTSNumbers               []int
	RetainBits               []uint8
}

func (h *Header) Encode(w bitwise.BinaryWriter) (n int, err error) {
	const processName = "Header.Write"
	var bw bitwise.BinaryWriter
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("[SEGMENT-HEADER][ENCODE] Begins")
	}
	defer func() {
		if err != nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[SEGMENT-HEADER][ENCODE] Failed. %v", err)
			}
		} else {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[SEGMENT-HEADER] %v", h)
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("[SEGMENT-HEADER][ENCODE] Finished")
			}
		}
	}()
	w.FinishByte()
	if h.SegmentData != nil {
		se, ok := h.SegmentData.(SegmentEncoder)
		if !ok {
			return 0, jbig2errors.Errorf(processName, "Segment: %T doesn't implement SegmentEncoder interface", h.SegmentData)
		}
		bw = bitwise.BufferedMSB()
		n, err = se.Encode(bw)
		if err != nil {
			return 0, jbig2errors.Wrap(err, processName, "")
		}
		h.SegmentDataLength = uint64(n)
	}
	if h.pageSize() == 4 {
		h.PageAssociationFieldSize = true
	}
	var m int
	m, err = h.writeSegmentNumber(w)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "")
	}
	n += m
	if err = h.writeFlags(w); err != nil {
		return n, jbig2errors.Wrap(err, processName, "")
	}
	n++
	m, err = h.writeReferredToCount(w)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "")
	}
	n += m
	m, err = h.writeReferredToSegments(w)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "")
	}
	n += m
	m, err = h.writeSegmentPageAssociation(w)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "")
	}
	n += m
	m, err = h.writeSegmentDataLength(w)
	if err != nil {
		return 0, jbig2errors.Wrap(err, processName, "")
	}
	n += m
	h.HeaderLength = int64(n) - int64(h.SegmentDataLength)
	if bw != nil {
		if _, err = w.Write(bw.Data()); err != nil {
			return n, jbig2errors.Wrap(err, processName, "write segment data")
		}
	}
	return n, nil
}

func (pis *PageInformationSegment) Init(h *Header, r *bitwise.Reader) (err error) {
	pis.reader = r
	if err = pis.parseHeader(); err != nil {
		return jbig2errors.Wrap(err, "PageInformationSegment.Init", "")
	}
	return nil
}

func (gr *GenericRegion) overrideAtTemplate3(context, x, y, result, bitIndex int) int {
	context &= 0x3EF
	if gr.GBAtY[0] == 0 && gr.GBAtX[0] >= -int8(bitIndex) {
		context |= (result >> uint(7-(int8(bitIndex)+gr.GBAtX[0])) & 0x1) << 4
	} else {
		context |= int(gr.getPixel(x+int(gr.GBAtX[0]), y+int(gr.GBAtY[0]))) << 4
	}
	return context
}
