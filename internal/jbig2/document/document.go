// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package document

import (
	"encoding/binary"
	"io"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	"github.com/OpenMellon/openpdf/internal/jbig2/document/segments"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

func (p *Page) clearSegmentData() {
	for i := range p.Segments {
		p.Segments[i].CleanSegmentData()
	}
}

func (d *Document) addSymbolDictionary(pageNumber int, symbols *bitmap.Bitmaps, symbolList []int, symbolMap map[int]int, unborderSymbols bool) (*segments.Header, error) {
	const funcName = "addSymbolDictionary"
	dict := &segments.SymbolDictionary{}
	if err := dict.InitEncode(symbols, symbolList, symbolMap, unborderSymbols); err != nil {
		return nil, err
	}
	header := &segments.Header{Type: segments.TSymbolDictionary, PageAssociation: pageNumber, SegmentData: dict}
	if pageNumber == 0 {
		if d.GlobalSegments == nil {
			d.GlobalSegments = &Globals{}
		}
		d.GlobalSegments.AddSegment(header)
		return header, nil
	}
	page, found := d.Pages[pageNumber]
	if !found {
		return nil, jbig2errors.Errorf(funcName, "page: '%d' not found", pageNumber)
	}
	var (
		idx int
		seg *segments.Header
	)
	for idx, seg = range page.Segments {
		if seg.Type == segments.TPageInformation {
			break
		}
	}
	idx++
	page.Segments = append(page.Segments, nil)
	copy(page.Segments[idx+1:], page.Segments[idx:])
	page.Segments[idx] = header
	return header, nil
}

func (d *Document) GetGlobalSegment(i int) (*segments.Header, error) {
	seg, err := d.GlobalSegments.GetSegment(i)
	if err != nil {
		return nil, jbig2errors.Wrap(err, "GetGlobalSegment", "")
	}
	return seg, nil
}

func (g *Globals) GetSegmentByIndex(index int) (*segments.Header, error) {
	const funcName = "Globals.GetSegmentByIndex"
	if g == nil {
		return nil, jbig2errors.Error(funcName, "globals not defined")
	}
	if len(g.Segments) == 0 {
		return nil, jbig2errors.Error(funcName, "globals are empty")
	}
	if index > len(g.Segments)-1 {
		return nil, jbig2errors.Error(funcName, "index out of range")
	}
	return g.Segments[index], nil
}

type Page struct {
	Segments           []*segments.Header
	PageNumber         int
	Bitmap             *bitmap.Bitmap
	FinalHeight        int
	FinalWidth         int
	ResolutionX        int
	ResolutionY        int
	IsLossless         bool
	Document           *Document
	FirstSegmentNumber int
	EncodingMethod     EncodingMethod
	BlackIsOne         bool
}

func (p *Page) getHeight() (int, error) {
	const funcName = "getHeight"
	if p.FinalHeight != 0 {
		return p.FinalHeight, nil
	}
	pageInfoHeader := p.getPageInformationSegment()
	if pageInfoHeader == nil {
		return 0, jbig2errors.Error(funcName, "nil page information")
	}
	data, err := pageInfoHeader.GetSegmentData()
	if err != nil {
		return 0, jbig2errors.Wrap(err, funcName, "")
	}
	pageInfo, ok := data.(*segments.PageInformationSegment)
	if !ok {
		return 0, jbig2errors.Errorf(funcName, "page information segment is of invalid type: '%T'", data)
	}
	if pageInfo.PageBMHeight == math.MaxInt32 {
		_, err = p.GetBitmap()
		if err != nil {
			return 0, jbig2errors.Wrap(err, funcName, "")
		}
	} else {
		p.FinalHeight = pageInfo.PageBMHeight
	}
	return p.FinalHeight, nil
}
func (p *Page) GetWidth() (int, error) { return p.getWidth() }

const (
	GenericEM EncodingMethod = iota
	CorrelationEM
	RankHausEM
)

func (p *Page) AddEndOfPageSegment() {
	header := &segments.Header{Type: segments.TEndOfPage, PageAssociation: p.PageNumber}
	p.Segments = append(p.Segments, header)
}

func (p *Page) getResolutionY() (int, error) {
	const funcName = "getResolutionY"
	if p.ResolutionY != 0 {
		return p.ResolutionY, nil
	}
	pageInfoHeader := p.getPageInformationSegment()
	if pageInfoHeader == nil {
		return 0, jbig2errors.Error(funcName, "nil page information")
	}
	data, err := pageInfoHeader.GetSegmentData()
	if err != nil {
		return 0, jbig2errors.Wrap(err, funcName, "")
	}
	pageInfo, ok := data.(*segments.PageInformationSegment)
	if !ok {
		return 0, jbig2errors.Errorf(funcName, "page information segment is of invalid type:'%T'", data)
	}
	p.ResolutionY = pageInfo.ResolutionY
	return p.ResolutionY, nil
}

func (p *Page) fitsPage(pageInfo *segments.PageInformationSegment, bm *bitmap.Bitmap) bool {
	return p.countRegions() == 1 && pageInfo.DefaultPixelValue == 0 && pageInfo.PageBMWidth == bm.Width && pageInfo.PageBMHeight == bm.Height
}

func (d *Document) parseFileHeader() error {
	const funcName = "parseFileHeader"
	_, err := d.InputStream.Seek(8, io.SeekStart)
	if err != nil {
		return jbig2errors.Wrap(err, funcName, "id")
	}
	_, err = d.InputStream.ReadBits(5)
	if err != nil {
		return jbig2errors.Wrap(err, funcName, "reserved bits")
	}
	bit, err := d.InputStream.ReadBit()
	if err != nil {
		return jbig2errors.Wrap(err, funcName, "extended templates")
	}
	if bit == 1 {
		d.GBUseExtTemplate = true
	}
	bit, err = d.InputStream.ReadBit()
	if err != nil {
		return jbig2errors.Wrap(err, funcName, "unknown page number")
	}
	if bit != 1 {
		d.NumberOfPagesUnknown = false
	}
	bit, err = d.InputStream.ReadBit()
	if err != nil {
		return jbig2errors.Wrap(err, funcName, "organization type")
	}
	d.OrganizationType = segments.OrganizationType(bit)
	if !d.NumberOfPagesUnknown {
		d.NumberOfPages, err = d.InputStream.ReadUint32()
		if err != nil {
			return jbig2errors.Wrap(err, funcName, "number of pages")
		}
		d.headerLength = 13
	}
	return nil
}

func (g *Globals) GetSymbolDictionary() (*segments.Header, error) {
	const funcName = "Globals.GetSymbolDictionary"
	if g == nil {
		return nil, jbig2errors.Error(funcName, "globals not defined")
	}
	if len(g.Segments) == 0 {
		return nil, jbig2errors.Error(funcName, "globals are empty")
	}
	for _, seg := range g.Segments {
		if seg.Type == segments.TSymbolDictionary {
			return seg, nil
		}
	}
	return nil, jbig2errors.Error(funcName, "global symbol dictionary not found")
}

func (d *Document) produceClassifiedPage(page *Page, globalDict *segments.Header) (err error) {
	const funcName = "produceClassifiedPage"
	var localSymbolMap map[int]int
	symbolCount := d.globalSymbolCount
	dictSegments := []*segments.Header{globalDict}
	if len(d.pageUniqueSymbols[page.PageNumber]) > 0 {
		localSymbolMap = map[int]int{}
		localDict, err := d.addSymbolDictionary(page.PageNumber, d.Classer.UndilatedTemplates, d.pageUniqueSymbols[page.PageNumber], localSymbolMap, false)
		if err != nil {
			return jbig2errors.Wrap(err, funcName, "")
		}
		dictSegments = append(dictSegments, localDict)
		symbolCount += len(d.pageUniqueSymbols[page.PageNumber])
	}
	comps := d.pageComponents[page.PageNumber]
	common.Log.Debug("Page: '%d' comps: %v", page.PageNumber, comps)
	page.addTextRegionSegment(dictSegments, d.globalSymbolMap, localSymbolMap, d.pageComponents[page.PageNumber], d.Classer.PtaLL, d.Classer.UndilatedTemplates, d.Classer.ClassIDs, nil, bitsForSymbolCount(symbolCount), len(d.pageComponents[page.PageNumber]))
	return nil
}

func InitEncodeDocument(fullHeaders bool) *Document {
	return &Document{FullHeaders: fullHeaders, writer: bitwise.BufferedMSB(), Pages: map[int]*Page{}, pageUniqueSymbols: map[int][]int{}, globalSymbolMap: map[int]int{}, pageComponents: map[int][]int{}}
}

type Globals struct {
	Segments []*segments.Header
}

func (p *Page) composePageBitmap() error {
	const funcName = "composePageBitmap"
	if p.PageNumber == 0 {
		return nil
	}
	pageInfoHeader := p.getPageInformationSegment()
	if pageInfoHeader == nil {
		return jbig2errors.Error(funcName, "page information segment not found")
	}
	data, err := pageInfoHeader.GetSegmentData()
	if err != nil {
		return err
	}
	pageInfo, ok := data.(*segments.PageInformationSegment)
	if !ok {
		return jbig2errors.Error(funcName, "page information segment is of invalid type")
	}
	if err = p.createPage(pageInfo); err != nil {
		return jbig2errors.Wrap(err, funcName, "")
	}
	p.clearSegmentData()
	return nil
}
func (d *Document) nextPageNumber() uint32 { d.NumberOfPages++; return d.NumberOfPages }
func (d *Document) mapData() error {
	const funcName = "mapData"
	var (
		headers []*segments.Header
		offset  int64
		segType segments.Type
	)
	hasHeader, err := d.isFileHeaderPresent()
	if err != nil {
		return jbig2errors.Wrap(err, funcName, "")
	}
	if hasHeader {
		if err = d.parseFileHeader(); err != nil {
			return jbig2errors.Wrap(err, funcName, "")
		}
		offset += int64(d.headerLength)
		d.FullHeaders = true
	}
	var (
		page *Page
		eof  bool
	)
	for segType != 51 && !eof {
		header, err := segments.NewHeader(d, d.InputStream, offset, d.OrganizationType)
		if err != nil {
			return jbig2errors.Wrap(err, funcName, "")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Decoding segment number: %d, Type: %s", header.SegmentNumber, header.Type)
		}
		segType = header.Type
		if segType != segments.TEndOfFile {
			if header.PageAssociation != 0 {
				page = d.Pages[header.PageAssociation]
				if page == nil {
					page = newPage(d, header.PageAssociation)
					d.Pages[header.PageAssociation] = page
					if d.NumberOfPagesUnknown {
						d.NumberOfPages++
					}
				}
				page.Segments = append(page.Segments, header)
			} else {
				d.GlobalSegments.AddSegment(header)
			}
		}
		headers = append(headers, header)
		offset = d.InputStream.AbsolutePosition()
		if d.OrganizationType == segments.OSequential {
			offset += int64(header.SegmentDataLength)
		}
		eof, err = d.reachedEOF(offset)
		if err != nil {
			common.Log.Debug("jbig2 document reached EOF with error: %v", err)
			return jbig2errors.Wrap(err, funcName, "")
		}
	}
	d.determineRandomDataOffsets(headers, uint64(offset))
	return nil
}

func (d *Document) encodeFileHeader(w bitwise.BinaryWriter) (n int, err error) {
	const funcName = "encodeFileHeader"
	n, err = w.Write(fileHeaderID)
	if err != nil {
		return n, jbig2errors.Wrap(err, funcName, "id")
	}
	if err = w.WriteByte(0x01); err != nil {
		return n, jbig2errors.Wrap(err, funcName, "flags")
	}
	n++
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, d.NumberOfPages)
	written, err := w.Write(buf)
	if err != nil {
		return written, jbig2errors.Wrap(err, funcName, "page number")
	}
	n += written
	return n, nil
}

func (p *Page) AddGenericRegion(bm *bitmap.Bitmap, xloc, yloc, template int, tp segments.Type, duplicateLineRemoval bool) error {
	const funcName = "Page.AddGenericRegion"
	region := &segments.GenericRegion{}
	if err := region.InitEncode(bm, xloc, yloc, template, duplicateLineRemoval); err != nil {
		return jbig2errors.Wrap(err, funcName, "")
	}
	header := &segments.Header{Type: segments.TImmediateGenericRegion, PageAssociation: p.PageNumber, SegmentData: region}
	p.Segments = append(p.Segments, header)
	return nil
}

func (d *Document) reachedEOF(offset int64) (bool, error) {
	const funcName = "reachedEOF"
	_, err := d.InputStream.Seek(offset, io.SeekStart)
	if err != nil {
		common.Log.Debug("reachedEOF - d.InputStream.Seek failed: %v", err)
		return false, jbig2errors.Wrap(err, funcName, "input stream seek failed")
	}
	_, err = d.InputStream.ReadBits(32)
	if err == io.EOF {
		return true, nil
	} else if err != nil {
		return false, jbig2errors.Wrap(err, funcName, "")
	}
	return false, nil
}
