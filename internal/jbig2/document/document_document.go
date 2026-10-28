// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package document

import (
	"fmt"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	"github.com/OpenMellon/openpdf/internal/jbig2/document/segments"
	"github.com/OpenMellon/openpdf/internal/jbig2/encoder/classer"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

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
