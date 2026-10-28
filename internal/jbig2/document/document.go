// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package document

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"runtime/debug"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	"github.com/OpenMellon/openpdf/internal/jbig2/document/segments"
	"github.com/OpenMellon/openpdf/internal/jbig2/encoder/classer"
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

type Document struct {
	Pages                map[int]*Page
	NumberOfPagesUnknown bool
	NumberOfPages        uint32
	GBUseExtTemplate     bool
	InputStream          *bitwise.Reader
	GlobalSegments       *Globals
	OrganizationType     segments.OrganizationType
	Classer              *classer.Classer
	XRes, YRes           int
	FullHeaders          bool
	CurrentSegmentNumber uint32
	AverageTemplates     *bitmap.Bitmaps
	BaseIndexes          []int
	Refinement           bool
	RefineLevel          int
	headerLength         uint8
	writer               *bitwise.BufferedWriter
	EncodeGlobals        bool
	globalSymbolCount    int
	pageUniqueSymbols    map[int][]int
	pageComponents       map[int][]int
	classUsageCounts     []int
	globalSymbolMap      map[int]int
}

func (d *Document) completeSymbols() (err error) {
	const funcName = "completeSymbols"
	if d.Classer == nil {
		return nil
	}
	if d.Classer.UndilatedTemplates == nil {
		return jbig2errors.Error(funcName, "no templates defined for the classer")
	}
	singlePage := len(d.Pages) == 1
	classCounts := make([]int, d.Classer.UndilatedTemplates.Size())
	var classID int
	for i := 0; i < d.Classer.ClassIDs.Size(); i++ {
		classID, err = d.Classer.ClassIDs.Get(i)
		if err != nil {
			return jbig2errors.Wrap(err, funcName, "class ID's")
		}
		classCounts[classID]++
	}
	var sharedSymbols []int
	for i := 0; i < d.Classer.UndilatedTemplates.Size(); i++ {
		if classCounts[i] == 0 {
			return jbig2errors.Error(funcName, "no symbols instances found for given class? ")
		}
		if classCounts[i] > 1 || singlePage {
			sharedSymbols = append(sharedSymbols, i)
		}
	}
	d.globalSymbolCount = len(sharedSymbols)
	var pageNumber, compClassID int
	for i := 0; i < d.Classer.ComponentPageNumbers.Size(); i++ {
		pageNumber, err = d.Classer.ComponentPageNumbers.Get(i)
		if err != nil {
			return jbig2errors.Wrapf(err, funcName, "page: '%d' not found in the classer pagenumbers", i)
		}
		compClassID, err = d.Classer.ClassIDs.Get(i)
		if err != nil {
			return jbig2errors.Wrapf(err, funcName, "can't get symbol for page '%d' from classer", pageNumber)
		}
		if classCounts[compClassID] == 1 && !singlePage {
			d.pageUniqueSymbols[pageNumber] = append(d.pageUniqueSymbols[pageNumber], compClassID)
		}
	}
	if err = d.Classer.ComputeLLCorners(); err != nil {
		return jbig2errors.Wrap(err, funcName, "")
	}
	return nil
}

func (d *Document) GetNumberOfPages() (uint32, error) {
	if d.NumberOfPagesUnknown || d.NumberOfPages == 0 {
		if len(d.Pages) == 0 {
			if err := d.mapData(); err != nil {
				return 0, jbig2errors.Wrap(err, "Document.GetNumberOfPages", "")
			}
		}
		return uint32(len(d.Pages)), nil
	}
	return d.NumberOfPages, nil
}

func bitsForSymbolCount(n int) int {
	bits := 0
	isPowerOfTwo := (n & (n - 1)) == 0
	n >>= 1
	for ; n != 0; n >>= 1 {
		bits++
	}
	if isPowerOfTwo {
		return bits
	}
	return bits + 1
}

func (d *Document) Encode() (data []byte, err error) {
	const funcName = "Document.Encode"
	var n, eofLen int
	if d.FullHeaders {
		if n, err = d.encodeFileHeader(d.writer); err != nil {
			return nil, jbig2errors.Wrap(err, funcName, "")
		}
	}
	var (
		found bool
		seg   *segments.Header
		page  *Page
	)
	if err = d.completeClassifiedPages(); err != nil {
		return nil, jbig2errors.Wrap(err, funcName, "")
	}
	if err = d.produceClassifiedPages(); err != nil {
		return nil, jbig2errors.Wrap(err, funcName, "")
	}
	if d.GlobalSegments != nil {
		for _, seg = range d.GlobalSegments.Segments {
			if err = d.encodeSegment(seg, &n); err != nil {
				return nil, jbig2errors.Wrap(err, funcName, "")
			}
		}
	}
	for i := 1; i <= int(d.NumberOfPages); i++ {
		if page, found = d.Pages[i]; !found {
			return nil, jbig2errors.Errorf(funcName, "page: '%d' not found", i)
		}
		for _, seg = range page.Segments {
			if err = d.encodeSegment(seg, &n); err != nil {
				return nil, jbig2errors.Wrap(err, funcName, "")
			}
		}
	}
	if d.FullHeaders {
		if eofLen, err = d.encodeEOFHeader(d.writer); err != nil {
			return nil, jbig2errors.Wrap(err, funcName, "")
		}
		n += eofLen
	}
	data = d.writer.Data()
	if len(data) != n {
		common.Log.Debug("Bytes written (n): '%d' is not equal to the length of the data encoded: '%d'", n, len(data))
	}
	return data, nil
}

func newPage(doc *Document, pageNumber int) *Page {
	return &Page{Document: doc, PageNumber: pageNumber, Segments: []*segments.Header{}}
}

func (d *Document) AddClassifiedPage(bm *bitmap.Bitmap, method classer.Method) (err error) {
	const funcName = "Document.AddClassifiedPage"
	if !d.FullHeaders && d.NumberOfPages != 0 {
		return jbig2errors.Error(funcName, "document already contains page. FileMode disallows adding more than one page")
	}
	if d.Classer == nil {
		if d.Classer, err = classer.Init(classer.DefaultSettings()); err != nil {
			return jbig2errors.Wrap(err, funcName, "")
		}
	}
	pageNumber := int(d.nextPageNumber())
	page := &Page{Segments: []*segments.Header{}, Bitmap: bm, Document: d, FinalHeight: bm.Height, FinalWidth: bm.Width, PageNumber: pageNumber}
	d.Pages[pageNumber] = page
	switch method {
	case classer.RankHaus:
		page.EncodingMethod = RankHausEM
	case classer.Correlation:
		page.EncodingMethod = CorrelationEM
	}
	page.AddPageInformationSegment()
	if err = d.Classer.AddPage(bm, pageNumber, method); err != nil {
		return jbig2errors.Wrap(err, funcName, "")
	}
	if d.FullHeaders {
		page.AddEndOfPageSegment()
	}
	return nil
}

func (p *Page) createNormalPage(pageInfo *segments.PageInformationSegment) error {
	const funcName = "createNormalPage"
	var err error
	p.Bitmap, err = bitmap.New(pageInfo.PageBMWidth, pageInfo.PageBMHeight)
	if err != nil {
		return err
	}
	if pageInfo.DefaultPixelValue != 0 {
		p.Bitmap.SetDefaultPixel()
	}
	for _, seg := range p.Segments {
		switch seg.Type {
		case 6, 7, 22, 23, 38, 39, 42, 43:
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Getting Segment: %d", seg.SegmentNumber)
			}
			data, err := seg.GetSegmentData()
			if err != nil {
				return err
			}
			regioner, ok := data.(segments.Regioner)
			if !ok {
				common.Log.Debug("Segment: %T is not a Regioner", data)
				return jbig2errors.Errorf(funcName, "invalid jbig2 segment type - not a Regioner: %T", data)
			}
			regionBitmap, err := regioner.GetRegionBitmap()
			if err != nil {
				return jbig2errors.Wrap(err, funcName, "")
			}
			if p.fitsPage(pageInfo, regionBitmap) {
				p.Bitmap = regionBitmap
			} else {
				regionInfo := regioner.GetRegionInfo()
				op := p.getCombinationOperator(pageInfo, regionInfo.CombinaionOperator)
				err = bitmap.Blit(regionBitmap, p.Bitmap, int(regionInfo.XLocation), int(regionInfo.YLocation), op)
				if err != nil {
					return jbig2errors.Wrap(err, funcName, "")
				}
			}
		}
	}
	return nil
}

func (p *Page) getResolutionX() (int, error) {
	const funcName = "getResolutionX"
	if p.ResolutionX != 0 {
		return p.ResolutionX, nil
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
	p.ResolutionX = pageInfo.ResolutionX
	return p.ResolutionX, nil
}

func (d *Document) encodeSegment(seg *segments.Header, total *int) error {
	const funcName = "encodeSegment"
	seg.SegmentNumber = d.nextSegmentNumber()
	n, err := seg.Encode(d.writer)
	if err != nil {
		return jbig2errors.Wrapf(err, funcName, "segment: '%d'", seg.SegmentNumber)
	}
	*total += n
	return nil
}

func (p *Page) GetBitmap() (bm *bitmap.Bitmap, err error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace(fmt.Sprintf("[PAGE][#%d] GetBitmap begins...", p.PageNumber))
	}
	defer func() {
		if err != nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace(fmt.Sprintf("[PAGE][#%d] GetBitmap failed. %v", p.PageNumber, err))
			}
		} else {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace(fmt.Sprintf("[PAGE][#%d] GetBitmap finished", p.PageNumber))
			}
		}
	}()
	if p.Bitmap != nil {
		return p.Bitmap, nil
	}
	err = p.composePageBitmap()
	if err != nil {
		return nil, err
	}
	return p.Bitmap, nil
}

var fileHeaderID = []byte{0x97, 0x4A, 0x42, 0x32, 0x0D, 0x0A, 0x1A, 0x0A}

func (d *Document) produceClassifiedPages() (err error) {
	const funcName = "produceClassifiedPages"
	if d.Classer == nil {
		return nil
	}
	var (
		page       *Page
		found      bool
		globalDict *segments.Header
	)
	for i := 1; i <= int(d.NumberOfPages); i++ {
		if page, found = d.Pages[i]; !found {
			return jbig2errors.Errorf(funcName, "page: '%d' not found", i)
		}
		if page.EncodingMethod == GenericEM {
			continue
		}
		if globalDict == nil {
			if globalDict, err = d.GlobalSegments.GetSymbolDictionary(); err != nil {
				return jbig2errors.Wrap(err, funcName, "")
			}
		}
		if err = d.produceClassifiedPage(page, globalDict); err != nil {
			return jbig2errors.Wrapf(err, funcName, "page: '%d'", i)
		}
	}
	return nil
}
func (p *Page) GetResolutionY() (int, error) { return p.getResolutionY() }
func (g *Globals) AddSegment(segment *segments.Header) {
	g.Segments = append(g.Segments, segment)
}

func (p *Page) collectPageStripes() (stripes []segments.Segmenter, err error) {
	const funcName = "collectPageStripes"
	var data segments.Segmenter
	for _, seg := range p.Segments {
		switch seg.Type {
		case 6, 7, 22, 23, 38, 39, 42, 43:
			data, err = seg.GetSegmentData()
			if err != nil {
				return nil, jbig2errors.Wrap(err, funcName, "")
			}
			stripes = append(stripes, data)
		case 50:
			data, err = seg.GetSegmentData()
			if err != nil {
				return nil, err
			}
			eos, ok := data.(*segments.EndOfStripe)
			if !ok {
				return nil, jbig2errors.Errorf(funcName, "EndOfStripe is not of valid type: '%T'", data)
			}
			stripes = append(stripes, eos)
			p.FinalHeight = eos.LineNumber()
		}
	}
	return stripes, nil
}

func (p *Page) createPage(pageInfo *segments.PageInformationSegment) error {
	var err error
	if !pageInfo.IsStripe || pageInfo.PageBMHeight != -1 {
		err = p.createNormalPage(pageInfo)
	} else {
		err = p.createStripedPage(pageInfo)
	}
	return err
}

func (p *Page) countRegions() int {
	var n int
	for _, seg := range p.Segments {
		switch seg.Type {
		case 6, 7, 22, 23, 38, 39, 42, 43:
			n++
		}
	}
	return n
}
func (p *Page) nextSegmentNumber() uint32 { return p.Document.nextSegmentNumber() }
func (p *Page) getWidth() (int, error) {
	const funcName = "getWidth"
	if p.FinalWidth != 0 {
		return p.FinalWidth, nil
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
	p.FinalWidth = pageInfo.PageBMWidth
	return p.FinalWidth, nil
}

func (p *Page) GetSegment(number int) (*segments.Header, error) {
	const funcName = "Page.GetSegment"
	for _, seg := range p.Segments {
		if seg.SegmentNumber == uint32(number) {
			return seg, nil
		}
	}
	numbers := make([]uint32, len(p.Segments))
	for i, seg := range p.Segments {
		numbers[i] = seg.SegmentNumber
	}
	return nil, jbig2errors.Errorf(funcName, "segment with number: '%d' not found in the page: '%d'. Known segment numbers: %v", number, p.PageNumber, numbers)
}

func (d *Document) nextSegmentNumber() uint32 {
	n := d.CurrentSegmentNumber
	d.CurrentSegmentNumber++
	return n
}

func (d *Document) completeClassifiedPages() (err error) {
	const funcName = "completeClassifiedPages"
	if d.Classer == nil {
		return nil
	}
	d.classUsageCounts = make([]int, d.Classer.UndilatedTemplates.Size())
	for i := 0; i < d.Classer.ClassIDs.Size(); i++ {
		classID, err := d.Classer.ClassIDs.Get(i)
		if err != nil {
			return jbig2errors.Wrapf(err, funcName, "class with id: '%d'", i)
		}
		d.classUsageCounts[classID]++
	}
	var sharedSymbols []int
	for i := 0; i < d.Classer.UndilatedTemplates.Size(); i++ {
		if d.NumberOfPages == 1 || d.classUsageCounts[i] > 1 {
			sharedSymbols = append(sharedSymbols, i)
		}
	}
	var (
		page  *Page
		found bool
	)
	for idx, pageNumber := range *d.Classer.ComponentPageNumbers {
		if page, found = d.Pages[pageNumber]; !found {
			return jbig2errors.Errorf(funcName, "page: '%d' not found", idx)
		}
		if page.EncodingMethod == GenericEM {
			common.Log.Error("Generic page with number: '%d' mapped as classified page", idx)
			continue
		}
		d.pageComponents[pageNumber] = append(d.pageComponents[pageNumber], idx)
		classID, err := d.Classer.ClassIDs.Get(idx)
		if err != nil {
			return jbig2errors.Wrapf(err, funcName, "no such classID: %d", idx)
		}
		if d.classUsageCounts[classID] == 1 && d.NumberOfPages != 1 {
			symbols := append(d.pageUniqueSymbols[pageNumber], classID)
			d.pageUniqueSymbols[pageNumber] = symbols
		}
	}
	if err = d.Classer.ComputeLLCorners(); err != nil {
		return jbig2errors.Wrap(err, funcName, "")
	}
	if _, err = d.addSymbolDictionary(0, d.Classer.UndilatedTemplates, sharedSymbols, d.globalSymbolMap, false); err != nil {
		return jbig2errors.Wrap(err, funcName, "")
	}
	return nil
}

func (p *Page) Encode(w bitwise.BinaryWriter) (total int, err error) {
	const funcName = "Page.Encode"
	var n int
	for _, seg := range p.Segments {
		if n, err = seg.Encode(w); err != nil {
			return total, jbig2errors.Wrap(err, funcName, "")
		}
		total += n
	}
	return total, nil
}

func (g *Globals) GetSegment(segmentNumber int) (*segments.Header, error) {
	const funcName = "Globals.GetSegment"
	if g == nil {
		return nil, jbig2errors.Error(funcName, "globals not defined")
	}
	if len(g.Segments) == 0 {
		return nil, jbig2errors.Error(funcName, "globals are empty")
	}
	var seg *segments.Header
	for _, seg = range g.Segments {
		if seg.SegmentNumber == uint32(segmentNumber) {
			break
		}
	}
	if seg == nil {
		return nil, jbig2errors.Error(funcName, "segment not found")
	}
	return seg, nil
}

func (d *Document) isFileHeaderPresent() (bool, error) {
	d.InputStream.Mark()
	for _, want := range fileHeaderID {
		got, err := d.InputStream.ReadByte()
		if err != nil {
			return false, err
		}
		if want != got {
			d.InputStream.Reset()
			return false, nil
		}
	}
	d.InputStream.Reset()
	return true, nil
}

func (p *Page) createStripedPage(pageInfo *segments.PageInformationSegment) error {
	const funcName = "createStripedPage"
	stripes, err := p.collectPageStripes()
	if err != nil {
		return jbig2errors.Wrap(err, funcName, "")
	}
	var y int
	for _, stripe := range stripes {
		if eos, ok := stripe.(*segments.EndOfStripe); ok {
			y = eos.LineNumber() + 1
		} else {
			regioner := stripe.(segments.Regioner)
			info := regioner.GetRegionInfo()
			op := p.getCombinationOperator(pageInfo, info.CombinaionOperator)
			regionBitmap, err := regioner.GetRegionBitmap()
			if err != nil {
				return jbig2errors.Wrap(err, funcName, "")
			}
			err = bitmap.Blit(regionBitmap, p.Bitmap, int(info.XLocation), y, op)
			if err != nil {
				return jbig2errors.Wrap(err, funcName, "")
			}
		}
	}
	return nil
}
func (p *Page) GetResolutionX() (int, error) { return p.getResolutionX() }
func (p *Page) GetHeight() (int, error)      { return p.getHeight() }
func (p *Page) getCombinationOperator(pageInfo *segments.PageInformationSegment, op bitmap.CombinationOperator) bitmap.CombinationOperator {
	if pageInfo.CombinationOperatorOverrideAllowed() {
		return op
	}
	return pageInfo.CombinationOperator()
}
func (p *Page) String() string { return fmt.Sprintf("Page #%d", p.PageNumber) }
func (p *Page) lastSegmentNumber() (n uint32, err error) {
	const funcName = "lastSegmentNumber"
	if len(p.Segments) == 0 {
		return n, jbig2errors.Errorf(funcName, "no segments found in the page '%d'", p.PageNumber)
	}
	return p.Segments[len(p.Segments)-1].SegmentNumber, nil
}

func (d *Document) encodeEOFHeader(w bitwise.BinaryWriter) (n int, err error) {
	header := &segments.Header{SegmentNumber: d.nextSegmentNumber(), Type: segments.TEndOfFile}
	if n, err = header.Encode(w); err != nil {
		return 0, jbig2errors.Wrap(err, "encodeEOFHeader", "")
	}
	return n, nil
}

func (d *Document) determineRandomDataOffsets(headers []*segments.Header, offset uint64) {
	if d.OrganizationType != segments.ORandom {
		return
	}
	for _, seg := range headers {
		seg.SegmentDataStartOffset = offset
		offset += seg.SegmentDataLength
	}
}

func (p *Page) addTextRegionSegment(dictSegments []*segments.Header, globalSymbolMap, localSymbolMap map[int]int, comps []int, inLL *bitmap.Points, symbols *bitmap.Bitmaps, classIDs *basic.IntSlice, boxes *bitmap.Boxes, symBits, numSymbols int) {
	region := &segments.TextRegion{NumberOfSymbols: uint32(numSymbols)}
	region.InitEncode(globalSymbolMap, localSymbolMap, comps, inLL, symbols, classIDs, boxes, p.FinalWidth, p.FinalHeight, symBits)
	header := &segments.Header{RTSegments: dictSegments, SegmentData: region, PageAssociation: p.PageNumber, Type: segments.TImmediateTextRegion}
	dictType := segments.TPageInformation
	if localSymbolMap != nil {
		dictType = segments.TSymbolDictionary
	}
	var idx int
	for ; idx < len(p.Segments); idx++ {
		if p.Segments[idx].Type == dictType {
			idx++
			break
		}
	}
	p.Segments = append(p.Segments, nil)
	copy(p.Segments[idx+1:], p.Segments[idx:])
	p.Segments[idx] = header
}

func (p *Page) AddPageInformationSegment() {
	pageInfo := &segments.PageInformationSegment{PageBMWidth: p.FinalWidth, PageBMHeight: p.FinalHeight, ResolutionX: p.ResolutionX, ResolutionY: p.ResolutionY, IsLossless: p.IsLossless}
	if p.BlackIsOne {
		pageInfo.DefaultPixelValue = uint8(0x1)
	}
	header := &segments.Header{PageAssociation: p.PageNumber, SegmentDataLength: uint64(pageInfo.Size()), SegmentData: pageInfo, Type: segments.TPageInformation}
	p.Segments = append(p.Segments, header)
}

func decodeDocument(r *bitwise.Reader, globals *Globals) (*Document, error) {
	d := &Document{Pages: make(map[int]*Page), InputStream: r, OrganizationType: segments.OSequential, NumberOfPagesUnknown: true, GlobalSegments: globals, headerLength: 9}
	if d.GlobalSegments == nil {
		d.GlobalSegments = &Globals{}
	}
	if err := d.mapData(); err != nil {
		return nil, err
	}
	return d, nil
}

type EncodingMethod int

func DecodeDocument(input *bitwise.Reader, globals *Globals) (*Document, error) {
	return decodeDocument(input, globals)
}

func (d *Document) GetPage(pageNumber int) (segments.Pager, error) {
	const funcName = "Document.GetPage"
	if pageNumber < 0 {
		common.Log.Debug("JBIG2 Page - GetPage: %d. Page cannot be lower than 0. %s", pageNumber, debug.Stack())
		return nil, jbig2errors.Errorf(funcName, "invalid jbig2 document - provided invalid page number: %d", pageNumber)
	}
	if pageNumber > len(d.Pages) {
		common.Log.Debug("Page not found: %d. %s", pageNumber, debug.Stack())
		return nil, jbig2errors.Error(funcName, "invalid jbig2 document - page not found")
	}
	page, found := d.Pages[pageNumber]
	if !found {
		common.Log.Debug("Page not found: %d. %s", pageNumber, debug.Stack())
		return nil, jbig2errors.Errorf(funcName, "invalid jbig2 document - page not found")
	}
	return page, nil
}

func (d *Document) AddGenericPage(bm *bitmap.Bitmap, duplicateLineRemoval bool) (err error) {
	const funcName = "Document.AddGenericPage"
	if !d.FullHeaders && d.NumberOfPages != 0 {
		return jbig2errors.Error(funcName, "document already contains page. FileMode disallows adding more than one page")
	}
	page := &Page{Segments: []*segments.Header{}, Bitmap: bm, Document: d, FinalHeight: bm.Height, FinalWidth: bm.Width, IsLossless: true, BlackIsOne: bm.Color == bitmap.Chocolate}
	page.PageNumber = int(d.nextPageNumber())
	d.Pages[page.PageNumber] = page
	bm.InverseData()
	page.AddPageInformationSegment()
	if err = page.AddGenericRegion(bm, 0, 0, 0, segments.TImmediateGenericRegion, duplicateLineRemoval); err != nil {
		return jbig2errors.Wrap(err, funcName, "")
	}
	if d.FullHeaders {
		page.AddEndOfPageSegment()
	}
	return nil
}

func (p *Page) getPageInformationSegment() *segments.Header {
	for _, seg := range p.Segments {
		if seg.Type == segments.TPageInformation {
			return seg
		}
	}
	common.Log.Debug("Page information segment not found for page: %s.", p)
	return nil
}
