// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package segments

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder/huffman"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
	jbig2internal "github.com/OpenMellon/openpdf/internal/jbig2/internal"
)

func (h *Header) writeSegmentPageAssociation(bw bitwise.BinaryWriter) (n int, err error) {
	const processName = "writeSegmentPageAssociation"
	if h.pageSize() != 4 {
		if err = bw.WriteByte(byte(h.PageAssociation)); err != nil {
			return 0, jbig2errors.Wrap(err, processName, "pageSize != 4")
		}
		return 1, nil
	}
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(h.PageAssociation))
	if n, err = bw.Write(buf); err != nil {
		return 0, jbig2errors.Wrap(err, processName, "4 byte page number")
	}
	return n, nil
}

func (sd *SymbolDictionary) encodeNumSyms(bw bitwise.BinaryWriter) (n int, err error) {
	const processName = "encodeNumSyms"
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, sd.NumberOfExportedSymbols)
	if n, err = bw.Write(buf); err != nil {
		return n, jbig2errors.Wrap(err, processName, "exported symbols")
	}
	binary.BigEndian.PutUint32(buf, sd.NumberOfNewSymbols)
	m, err := bw.Write(buf)
	if err != nil {
		return n, jbig2errors.Wrap(err, processName, "new symbols")
	}
	return n + m, nil
}

func (hr *HalftoneRegion) parseHeader() error {
	if err := hr.RegionSegment.parseHeader(); err != nil {
		return err
	}
	bit, err := hr.reader.ReadBit()
	if err != nil {
		return err
	}
	hr.HDefaultPixel = int8(bit)
	bits, err := hr.reader.ReadBits(3)
	if err != nil {
		return err
	}
	hr.CombinationOperator = bitmap.CombinationOperator(bits & 0xf)
	bit, err = hr.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		hr.HSkipEnabled = true
	}
	bits, err = hr.reader.ReadBits(2)
	if err != nil {
		return err
	}
	hr.HTemplate = byte(bits & 0xf)
	bit, err = hr.reader.ReadBit()
	if err != nil {
		return err
	}
	if bit == 1 {
		hr.IsMMREncoded = true
	}
	bits, err = hr.reader.ReadBits(32)
	if err != nil {
		return err
	}
	hr.HGridWidth = uint32(bits & math.MaxUint32)
	bits, err = hr.reader.ReadBits(32)
	if err != nil {
		return err
	}
	hr.HGridHeight = uint32(bits & math.MaxUint32)
	bits, err = hr.reader.ReadBits(32)
	if err != nil {
		return err
	}
	hr.HGridX = int32(bits & math.MaxInt32)
	bits, err = hr.reader.ReadBits(32)
	if err != nil {
		return err
	}
	hr.HGridY = int32(bits & math.MaxInt32)
	bits, err = hr.reader.ReadBits(16)
	if err != nil {
		return err
	}
	hr.HRegionX = uint16(bits & math.MaxUint16)
	bits, err = hr.reader.ReadBits(16)
	if err != nil {
		return err
	}
	hr.HRegionY = uint16(bits & math.MaxUint16)
	if err = hr.computeSegmentDataStructure(); err != nil {
		return err
	}
	return hr.checkInput()
}

func (gr *GenericRegion) InitEncode(bm *bitmap.Bitmap, xLoc, yLoc, template int, duplicateLineRemoval bool) error {
	const processName = "GenericRegion.InitEncode"
	if bm == nil {
		return jbig2errors.Error(processName, "provided nil bitmap")
	}
	if xLoc < 0 || yLoc < 0 {
		return jbig2errors.Error(processName, "x and y location must be greater than 0")
	}
	gr.Bitmap = bm
	gr.GBTemplate = byte(template)
	switch gr.GBTemplate {
	case 0:
		gr.GBAtX = []int8{3, -3, 2, -2}
		gr.GBAtY = []int8{-1, -1, -2, -2}
	case 1:
		gr.GBAtX = []int8{3}
		gr.GBAtY = []int8{-1}
	case 2, 3:
		gr.GBAtX = []int8{2}
		gr.GBAtY = []int8{-1}
	default:
		return jbig2errors.Errorf(processName, "provided template: '%d' not in valid range {0,1,2,3}", template)
	}
	gr.RegionSegment = &RegionSegment{BitmapHeight: uint32(bm.Height), BitmapWidth: uint32(bm.Width), XLocation: uint32(xLoc), YLocation: uint32(yLoc)}
	gr.IsTPGDon = duplicateLineRemoval
	return nil
}

func (tr *TextRegion) symbolIDCodeLengths() error {
	var (
		cs             []*huffman.Code
		bits           uint64
		runLengthTable huffman.Tabler
		err            error
	)
	for i := 0; i < 35; i++ {
		bits, err = tr.reader.ReadBits(4)
		if err != nil {
			return err
		}
		codeLen := int(bits & 0xf)
		if codeLen > 0 {
			cs = append(cs, huffman.NewCode(int32(codeLen), 0, int32(i), false))
		}
	}
	runLengthTable, err = huffman.NewFixedSizeTable(cs)
	if err != nil {
		return err
	}
	var (
		prevLen     int64
		symIdx      uint32
		symbolCodes []*huffman.Code
		runCode     int64
	)
	for symIdx < tr.NumberOfSymbols {
		runCode, err = runLengthTable.Decode(tr.reader)
		if err != nil {
			return err
		}
		if runCode < 32 {
			if runCode > 0 {
				symbolCodes = append(symbolCodes, huffman.NewCode(int32(runCode), 0, int32(symIdx), false))
			}
			prevLen = runCode
			symIdx++
		} else {
			var repeat, repeatLen int64
			switch runCode {
			case 32:
				bits, err = tr.reader.ReadBits(2)
				if err != nil {
					return err
				}
				repeat = 3 + int64(bits)
				if symIdx > 0 {
					repeatLen = prevLen
				}
			case 33:
				bits, err = tr.reader.ReadBits(3)
				if err != nil {
					return err
				}
				repeat = 3 + int64(bits)
			case 34:
				bits, err = tr.reader.ReadBits(7)
				if err != nil {
					return err
				}
				repeat = 11 + int64(bits)
			}
			for j := 0; j < int(repeat); j++ {
				if repeatLen > 0 {
					symbolCodes = append(symbolCodes, huffman.NewCode(int32(repeatLen), 0, int32(symIdx), false))
				}
				symIdx++
			}
		}
	}
	tr.reader.Align()
	tr.symbolCodeTable, err = huffman.NewFixedSizeTable(symbolCodes)
	return err
}

func (tr *TextRegion) decodeRI() (int64, error) {
	if !tr.UseRefinement {
		return 0, nil
	}
	if tr.IsHuffmanEncoded {
		bit, err := tr.reader.ReadBit()
		return int64(bit), err
	}
	ri, err := tr.arithDecoder.DecodeInt(tr.cxIARI)
	return int64(ri), err
}

func (pis *PageInformationSegment) readResolution() error {
	bits, err := pis.reader.ReadBits(32)
	if err != nil {
		return err
	}
	pis.ResolutionX = int(bits & math.MaxInt32)
	bits, err = pis.reader.ReadBits(32)
	if err != nil {
		return err
	}
	pis.ResolutionY = int(bits & math.MaxInt32)
	return nil
}
func (ts *TableSegment) HtRS() int32 { return ts.htRS }
func (ts *TableSegment) Init(h *Header, r *bitwise.Reader) error {
	ts.reader = r
	return ts.parseHeader()
}

func (grr *GenericRefinementRegion) decodeOptimized(lineNumber, width, rowStride, refRowStride, paddedWidth, refOffset, refOffsetNext int) error {
	var (
		err     error
		refX    int
		regionX int
	)
	referenceDY := lineNumber - int(grr.ReferenceDY)
	if referenceDX := int(-grr.ReferenceDX); referenceDX > 0 {
		refX = referenceDX
	}
	byteIndex := grr.ReferenceBitmap.GetByteIndex(refX, referenceDY)
	if grr.ReferenceDX > 0 {
		regionX = int(grr.ReferenceDX)
	}
	regionByteIndex := grr.RegionBitmap.GetByteIndex(regionX, lineNumber)
	switch grr.TemplateID {
	case 0:
		err = grr.decodeTemplate(lineNumber, width, rowStride, refRowStride, paddedWidth, refOffset, refOffsetNext, regionByteIndex, referenceDY, byteIndex, grr.refTemplate0)
	case 1:
		err = grr.decodeTemplate(lineNumber, width, rowStride, refRowStride, paddedWidth, refOffset, refOffsetNext, regionByteIndex, referenceDY, byteIndex, grr.refTemplate1)
	}
	return err
}

func (tr *TextRegion) decodeSymbolInstances() error {
	stripT, err := tr.decodeStripT()
	if err != nil {
		return err
	}
	var (
		firstS        int64
		instanceCount uint32
	)
	for instanceCount < tr.NumberOfSymbolInstances {
		dt, err := tr.decodeDT()
		if err != nil {
			return err
		}
		stripT += dt
		var dfs int64
		first := true
		tr.currentS = 0
		for {
			if first {
				dfs, err = tr.decodeDfs()
				if err != nil {
					return err
				}
				firstS += dfs
				tr.currentS = firstS
				first = false
			} else {
				ids, err := tr.decodeIds()
				if errors.Is(err, jbig2internal.ErrOOB) {
					break
				}
				if err != nil {
					return err
				}
				if instanceCount >= tr.NumberOfSymbolInstances {
					break
				}
				tr.currentS += ids + int64(tr.SbDsOffset)
			}
			currentT, err := tr.decodeCurrentT()
			if err != nil {
				return err
			}
			t := stripT + currentT
			id, err := tr.decodeID()
			if err != nil {
				return err
			}
			ri, err := tr.decodeRI()
			if err != nil {
				return err
			}
			ib, err := tr.decodeIb(ri, id)
			if err != nil {
				return err
			}
			if err = tr.blit(ib, t); err != nil {
				return err
			}
			instanceCount++
		}
	}
	return nil
}

func (grr *GenericRefinementRegion) getGrReference() (*bitmap.Bitmap, error) {
	segments := grr.header.RTSegments
	if len(segments) == 0 {
		return nil, errors.New("referenced segment not exists")
	}
	segmentData, err := segments[0].GetSegmentData()
	if err != nil {
		return nil, err
	}
	r, ok := segmentData.(Regioner)
	if !ok {
		return nil, fmt.Errorf("referred to segment is not a regioner: %T", segmentData)
	}
	return r.GetRegionBitmap()
}

func (h *Header) writeSegmentNumber(bw bitwise.BinaryWriter) (n int, err error) {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, h.SegmentNumber)
	if n, err = bw.Write(buf); err != nil {
		return 0, jbig2errors.Wrap(err, "Header.writeSegmentNumber", "")
	}
	return n, nil
}

func (h *Header) GetSegmentData() (Segmenter, error) {
	var segmentData Segmenter
	if h.SegmentData != nil {
		segmentData = h.SegmentData
	}
	if segmentData == nil {
		segmentCreator, ok := segmentCreators[h.Type]
		if !ok {
			return nil, fmt.Errorf("type: %s/ %d creator not found. ", h.Type, h.Type)
		}
		segmentData = segmentCreator()
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("[SEGMENT-HEADER][#%d] GetSegmentData at Offset: %04X", h.SegmentNumber, h.SegmentDataStartOffset)
		}
		subInputReader, err := h.subInputReader()
		if err != nil {
			return nil, err
		}
		if err := segmentData.Init(h, subInputReader); err != nil {
			common.Log.Debug("Init failed: %v for type: %T", err, segmentData)
			return nil, err
		}
		h.SegmentData = segmentData
	}
	return segmentData, nil
}
