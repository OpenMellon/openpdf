// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package document

import (
	"fmt"
	"runtime/debug"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	"github.com/OpenMellon/openpdf/internal/jbig2/document/segments"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

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
