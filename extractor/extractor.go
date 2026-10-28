// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package extractor is used for quickly extracting PDF content through a simple interface.
// Currently offers functionality for extracting textual content.
package extractor

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/license"
	"github.com/OpenMellon/openpdf/internal/streamlimits"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
	imagedraw "golang.org/x/image/draw"
	"golang.org/x/text/unicode/norm"
)

func truncate(s string, maxLen int) string {
	if len(s) < maxLen {
		return s
	}
	return s[:maxLen]
}

var rulingKindNames = map[rulingKind]string{rulingNone: "none", rulingHorz: "horizontal", rulingVert: "vertical"}

// Len returns the number of TextMarks in `ma`.
func (tma *TextMarkArray) Len() int {
	if tma == nil {
		return 0
	}
	return len(tma.marks)
}

func (to *textObject) getFontDict(name string) (fontObj core.PdfObject, err error) {
	resources := to.resources
	if resources == nil {
		common.Log.Debug("getFontDict. No resources. name=%#q", name)
		return nil, nil
	}
	fontObj, ok := resources.GetFontByName(core.PdfObjectName(name))
	if !ok {
		common.Log.Debug("ERROR: getFontDict: Font not found: name=%#q", name)
		return nil, errors.New("font not in resources")
	}
	return fontObj, nil
}

func (tt *textTable) getComposite(col, row int) (paraList, model.PdfRectangle) {
	cell, ok := tt.compositeCells[cellKey(col, row)]
	if debugTables {
		fmt.Printf("        getComposite(%d,%d)->%s\n", col, row, cell.String())
	}
	if !ok {
		return nil, model.PdfRectangle{}
	}
	return cell.parasBBox()
}

func trimLastMarkRune(marks []TextMark, offset *int) []TextMark {
	last := marks[len(marks)-1]
	runes := []rune(last.Text)
	if len(runes) == 1 {
		marks = marks[:len(marks)-1]
		prev := marks[len(marks)-1]
		*offset = prev.Offset + len(prev.Text)
	} else {
		trimmed := dropLastRune(last.Text)
		*offset += len(trimmed) - len(last.Text)
		last.Text = trimmed
	}
	return marks
}

func topEdgeRuling(rect model.PdfRectangle) *ruling {
	return &ruling{kind: rulingHorz, xy: rect.Ury, lo: rect.Llx, hi: rect.Urx}
}

// ImageExtractOptions contains options for controlling image extraction from
// PDF pages.
type ImageExtractOptions struct{ IncludeInlineStencilMasks bool }

const (
	ExtractionModeLayout = iota
	ExtractionModePlain
	ExtractionModeLayoutNoBreaks
	ExtractionModeGrid
)

func maskToImage(img *model.Image, maskColor color.Color) image.Image {
	width, height := int(img.Width), int(img.Height)
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < height; i++ {
		for j := 0; j < width; j++ {
			pix, err := img.ColorAt(j, i)
			if err != nil {
				common.Log.Debug("WARN: could not retrieve image mask value at (%d, %d). Output may be incorrect.", j, i)
				continue
			}
			r, g, b, _ := pix.RGBA()
			var fill color.Color
			if r+g+b == 0 {
				fill = maskColor
			} else {
				fill = color.Transparent
			}
			out.Set(j, i, fill)
		}
	}
	return out
}
func (tl *textLine) bbox() model.PdfRectangle { return tl.PdfRectangle }
func (pt *PageText) makeGridText(marks []*textMark) string {
	if len(marks) == 0 {
		return ""
	}
	words := makeTextWords(marks, pt.pageSize, false)
	if len(words) == 0 {
		return ""
	}
	charWidth := medianCharWidth(words)
	if charWidth <= 0 {
		charWidth = defaultCharWidth
	}
	gapThreshold := charWidth * gapFactor
	words2 := makeGridWords(words)
	if len(words2) == 0 {
		return ""
	}
	minX := minLlx(words2)
	rows := groupGridRows(words2, charWidth)
	spacing := rowSpacing(rows, charWidth)
	return renderGrid(rows, minX, gapThreshold, spacing)
}

// FormContents returns the form XObjects painted on the page, directly or
// nested inside other forms, along with their parsed content stream
// operations. Every paint of a form shares one parse, so each form appears
// once regardless of how many times, or under how many resource names, it is
// painted.
func (pt *PageText) FormContents() []FormContent { return pt.forms }

// String returns a human readable description of `ss`.
func (ss *shapesState) String() string {
	return fmt.Sprintf("{%d subpaths fresh=%t}", len(ss.subpaths), ss.fresh)
}

func (tt *textTable) compositeRowCorridors() map[int][]float64 {
	corridors := make(map[int][]float64, tt.h)
	if debugTables {
		common.Log.Info("compositeRowCorridors: h=%d", tt.h)
	}
	for i := 1; i < tt.h; i++ {
		var cells []compositeCell
		for j := 0; j < tt.w; j++ {
			if cell, ok := tt.compositeCells[cellKey(j, i)]; ok {
				cells = append(cells, cell)
			}
		}
		if len(cells) == 0 {
			continue
		}
		rowCorr := rowCorridors(cells)
		corridors[i] = rowCorr
		if debugTables {
			fmt.Printf("   %2d: %6.2f\n", i, rowCorr)
		}
	}
	return corridors
}
func (wb *wordBag) maxDepth() float64          { return wb.pageHeight - wb.Lly }
func (sp *subpath) add(pts ...transform.Point) { sp.points = append(sp.points, pts...) }
func (to *textObject) setHorizScaling(scale float64) {
	if to == nil {
		return
	}
	to.state.th = scale
}

func (rl rulingList) toTilings() (rulingList, []gridTiling) {
	rl.log("toTilings")
	if len(rl) == 0 {
		return nil, nil
	}
	rl = rl.tidied("all")
	rl.log("tidied")
	rulings := rl.toGrids()
	tilings := make([]gridTiling, len(rulings))
	for i, rulings2 := range rulings {
		tilings[i] = rulings2.asTiling()
	}
	return rl, tilings
}

type subpath struct {
	points []transform.Point
	closed bool
}

func (tm *textMark) bbox() model.PdfRectangle { return tm.PdfRectangle }

// Search searches the pages specified by `pages`.
func (e *Editor) Search(pattern string, pages []int) (map[int]Match, error) {
	matches, _, err := e.getMatches(pattern, pages)
	return matches, err
}

func (to *textObject) setTextRise(rise float64) {
	if to == nil {
		return
	}
	to.state.trise = rise
}

func rightEdgeRuling(rect model.PdfRectangle) *ruling {
	return &ruling{kind: rulingVert, xy: rect.Urx, lo: rect.Lly, hi: rect.Ury}
}

func depthBinIndex(depth float64) int {
	var idx int
	if depth >= 0 {
		idx = int(depth / depthBinWidth)
	} else {
		idx = int(depth/depthBinWidth) - 1
	}
	return idx
}

func hasDirectObject(marks *TextMarkArray) bool {
	return len(marks.marks) > 0 && marks.marks[0].DirectObject != nil
}

func medianCharWidth(words []*textWord) float64 {
	widths := make([]float64, 0, len(words))
	for _, word := range words {
		numRunes := utf8.RuneCountInString(word.text)
		width := word.Urx - word.Llx
		if numRunes > 0 && width > 0 {
			widths = append(widths, width/float64(numRunes))
		}
	}
	if len(widths) == 0 {
		return 0
	}
	sort.Float64s(widths)
	return widths[len(widths)/2]
}

// Replace takes a pattern and replaces all the texts that much the pattern with `replacement`.
func (e *Editor) Replace(pattern string, replacement string, pages []int) error {
	matches := map[int]Match{}
	for _, pageNum := range pages {
		pdfPage, err := e.reader.GetPage(pageNum)
		if err != nil {
			return err
		}
		ex, err := New(pdfPage)
		if err != nil {
			return err
		}
		pageText, _, _, err := ex.ExtractPageText()
		if err != nil {
			return err
		}
		contents := ""
		marksText := pageText.marksText()
		allMatches, err := findAllMatches(pattern, marksText)
		if err != nil {
			return err
		}
		marks := pageText.Marks()
		boxes := []Box{}
		for _, loc := range allMatches {
			matchMarks, box, err2 := getMatchMarks(loc, marks, pattern)
			if err2 != nil {
				return err2
			}
			objMarks := groupMarksByObject(matchMarks)
			boxes = append(boxes, box)
			err2 = replaceTextMarks(objMarks, replacement)
			if err2 != nil {
				return err2
			}
		}
		contents = pageText.GetContentStreamOps().String()
		match := Match{Pattern: pattern, Indexes: allMatches, Locations: boxes}
		pdfPage.SetContentStreams([]string{contents}, core.NewFlateEncoder())
		matches[pageNum] = match
	}
	return nil
}

func isAdjacentRight(bag *wordBag, word *textWord, gap float64) bool {
	return bag.Urx <= word.Llx && word.Llx < bag.Urx+gap
}

// NewEditor returns a new Editor object
func NewEditor(reader *model.PdfReader) *Editor { return &Editor{reader: reader} }

var (
	errTypeCheck  = errors.New("type check error")
	errRangeCheck = errors.New("range check error")
)

func (cc compositeCell) parasBBox() (paraList, model.PdfRectangle) {
	return cc.paraList, cc.PdfRectangle
}

func (sp *subpath) isQuadrilateral() bool {
	if len(sp.points) < 4 || len(sp.points) > 5 {
		return false
	}
	if len(sp.points) == 5 {
		first := sp.points[0]
		last := sp.points[4]
		if first.X != last.X || first.Y != last.Y {
			return false
		}
	}
	return true
}
func approxEqual(a, b float64) bool { return math.Abs(a-b) <= approxTolerance }
func (ss *shapesState) lastpointEstablished() (transform.Point, bool) {
	if ss.fresh {
		return ss.currentPoint, false
	}
	numSubpaths := len(ss.subpaths)
	if numSubpaths > 0 && ss.subpaths[numSubpaths-1].closed {
		return ss.subpaths[numSubpaths-1].last(), false
	}
	return transform.Point{}, true
}

func (wb *wordBag) removeDuplicates() {
	if debugRemoveDuplicates {
		common.Log.Info("removeDuplicates: %q", wb.text())
	}
	for _, idx := range wb.depthIndexes() {
		if len(wb.bins[idx]) == 0 {
			continue
		}
		first := wb.bins[idx][0]
		tol := duplicateToleranceFactor * first.fontSize
		depth := first.depth
		for _, bandIdx := range wb.depthBand(depth, depth+tol) {
			dups := map[*textWord]struct{}{}
			band := wb.bins[bandIdx]
			for _, w := range band {
				if _, ok := dups[w]; ok {
					continue
				}
				for _, other := range band {
					if _, ok := dups[other]; ok {
						continue
					}
					if other != w && other.text == w.text && math.Abs(other.Llx-w.Llx) < tol && math.Abs(other.Urx-w.Urx) < tol && math.Abs(other.Lly-w.Lly) < tol && math.Abs(other.Ury-w.Ury) < tol {
						dups[other] = struct{}{}
					}
				}
			}
			if len(dups) > 0 {
				n := 0
				for _, kept := range band {
					if _, ok := dups[kept]; !ok {
						band[n] = kept
						n++
					}
				}
				wb.bins[bandIdx] = band[:len(band)-len(dups)]
				if len(wb.bins[bandIdx]) == 0 {
					delete(wb.bins, bandIdx)
				}
			}
		}
	}
}

// Options extractor options.
type Options struct {
	// DisableDocumentTags specifies whether to use the document tags during list extraction.
	DisableDocumentTags bool

	// ApplyCropBox will extract page text based on page cropbox if set to `true`.
	ApplyCropBox bool

	// Text extraction mode, default is ExtractionModeLayout.
	ExtractionMode ExtractionMode

	// IncludeAnnotations specifies whether to include annotations in the extraction process, default value is `false`.
	IncludeAnnotations bool

	// RelaxedMode specifies whether to use relaxed mode for processing the objects,
	// If enabled OpenPDF will automatically try to fix invalid parameters length and value.
	// Default is `false`.
	RelaxedMode bool

	// DisableDehyphenation specifies whether to disable de-hyphenation of words that are split across lines.
	// If `true`, words ending in a hyphen will not be combined with the next line.
	// Default is `false` (de-hyphenation enabled).
	DisableDehyphenation bool
}

func (pl paraList) inTile(tile gridTile) paraList {
	var paras paraList
	for _, para := range pl {
		if tile.contains(para.PdfRectangle) {
			paras = append(paras, para)
		}
	}
	if debugTables {
		fmt.Printf("   inTile:  %s inside=%d\n", tile, len(paras))
		for i, para := range paras {
			fmt.Printf("%4d: %s\n", i, para)
		}
		fmt.Println("")
	}
	return paras
}

func (to *textObject) getFontDirect(name string) (*model.PdfFont, error) {
	fontDict, err := to.getFontDict(name)
	if err != nil {
		return nil, err
	}
	font, err := model.NewPdfFontFromPdfObject(fontDict)
	if err != nil {
		common.Log.Debug("getFontDirect: NewPdfFontFromPdfObject failed. name=%#q err=%v", name, err)
	}
	return font, err
}

// Box represents the bounding box of a given textMark on pdf page.
// This might be used for different kinds of high lighting after doing the search
type Box struct{ BBox model.PdfRectangle }

func (ss *shapesState) fill(sections *[]pathSection) {
	section := pathSection{subpaths: ss.subpaths, Color: ss.textObj.getFillColor()}
	*sections = append(*sections, section)
	if debugRulings {
		rect := section.bbox()
		fmt.Printf("    FILL: %2d fills (%d new) ss=%s color=%3v %6.2f=%6.2fx%6.2f\n", len(*sections), len(section.subpaths), ss, section.Color, rect, rect.Width(), rect.Height())
		if debugSubpaths {
			for i, path := range section.subpaths {
				fmt.Printf("%8d: %s\n", i, path)
				if i == 10 {
					break
				}
			}
		}
	}
}

type lists []*list

func collectListItems(listV *list) []*list {
	var items []*list
	for _, child := range listV.children {
		switch child.kind {
		case "LI":
			lines := linesOfListItem(child)
			subItems := collectListItems(child)
			item := newList(lines, "bullet", subItems)
			text := joinLinesText(lines, "")
			item.text = text
			items = append(items, item)
		case "LBody":
			return collectListItems(child)
		case "L":
			nested := collectListItems(child)
			items = append(items, nested...)
			return items
		}
	}
	return items
}

func groupLinesByDepth(lines []*textLine) map[float64][]*textLine {
	sort.Slice(lines, func(i, j int) bool { return lines[i].depth < lines[j].depth })
	byStartX := map[float64][]*textLine{}
	for _, line := range lines {
		startX := lineStartX(line)
		startX = math.Round(startX)
		byStartX[startX] = append(byStartX[startX], line)
	}
	return byStartX
}

func listsFromStructTree(kvs []*model.KValue, linesByMCID map[int][]*textLine, pageObj core.PdfObject) []*list {
	items := []*list{}
	for _, kv := range kvs {
		dict := kv.GetKDict()
		kvs2 := dict.GetChildren()
		lines := []*textLine{}
		subItems := []*list{}
		kind := dict.S.(*core.PdfObjectName).String()
		firstChild := kvs2[0]
		n := firstChild.GetMCID()
		if len(kvs2) == 1 && n != nil && *n != -1 {
			if dict.Pg == pageObj {
				lines = linesByMCID[*n]
			}
		} else {
			subItems = listsFromStructTree(kvs2, linesByMCID, pageObj)
		}
		listV := newList(lines, kind, subItems)
		items = append(items, listV)
	}
	return items
}

func (tp *textPara) toTextMarks(offset *int, ctx *textContext) []TextMark {
	if tp.table == nil {
		return tp.toCellTextMarks(offset, ctx)
	}
	var marks []TextMark
	for row := 0; row < tp.table.h; row++ {
		for col := 0; col < tp.table.w; col++ {
			para := tp.table.get(col, row)
			if para == nil {
				marks = appendMetaMark(marks, offset, "\t")
			} else {
				cellMarks := para.toCellTextMarks(offset, ctx)
				marks = append(marks, cellMarks...)
			}
			marks = appendMetaMark(marks, offset, " ")
		}
		if row < tp.table.h-1 {
			marks = appendMetaMark(marks, offset, "\n")
		}
	}
	table := tp.table
	if table.isExportable() {
		tt := table.toTextTable(ctx)
		marks = attachTable(marks, &tt)
	}
	return marks
}

func (rl rulingList) secMinMax() (float64, float64) {
	lo, hi := rl[0].lo, rl[0].hi
	for _, r := range rl[1:] {
		if r.lo < lo {
			lo = r.lo
		}
		if r.hi > hi {
			hi = r.hi
		}
	}
	return lo, hi
}

// ExtractionMode defines different types of extraction mode.
type ExtractionMode int

func setStringFromText(str *core.PdfObjectString, text string, font *model.PdfFont) {
	codes, numMissing := font.StringToCharcodeBytes(text)
	if numMissing != 0 {
		common.Log.Debug("WARN: some runes could not be encoded.\n\t%s -> %v", text, codes)
	}
	pdfStr := core.MakeStringFromBytes(codes)
	*str = *pdfStr
}

func newTextPara(rect model.PdfRectangle, lines []*textLine) *textPara {
	return &textPara{PdfRectangle: rect, lines: lines}
}
func (tp *textPara) bbox() model.PdfRectangle { return tp.PdfRectangle }

// String returns a string descibing `i`.
func (gt gridTile) String() string {
	show := func(on bool, label string) string {
		if on {
			return label
		}
		return "_"
	}
	return fmt.Sprintf("%6.2f %1s%1s%1s%1s", gt.PdfRectangle, show(gt.llx, "L"), show(gt.urx, "R"), show(gt.lly, "B"), show(gt.ury, "T"))
}

func compareDepthThenLlx(a, b bounded) float64 {
	d := compareDepth(a, b)
	if !isZero(d) {
		return d
	}
	return compareLlx(a, b)
}

func (to *textObject) advanceTextMatrix(buf []byte) {
	if to.unsupportedFont {
		return
	}
	font := to.getCurrentFont()
	codes := font.BytesToCharcodes(buf)
	state := to.state
	tfs := state.tfs
	hScale := state.th / 100.0
	glyphUnit := glyphSpaceUnit
	if font.Subtype() == "Type3" {
		glyphUnit = 1
	}
	for _, code := range codes {
		if code == 0 {
			continue
		}
		wordSpace := 0.0
		if code == 32 {
			wordSpace = state.tw
		}
		metrics, ok := font.GetCharMetrics(code)
		if !ok {
			continue
		}
		glyphDisp := transform.Point{X: metrics.Wx * glyphUnit, Y: metrics.Wy * glyphUnit}
		shift := transform.Point{X: (glyphDisp.X*tfs + state.tc + wordSpace) * hScale}
		trans := translateBy(shift)
		to.tm.Concat(trans)
	}
}

func lineToRuling(p0, p1 transform.Point, col color.Color) (*ruling, bool) {
	lr := lineRuling{p0: p0, p1: p1, kind: rulingKindOfLine(p0, p1), Color: col}
	if lr.kind == rulingNone {
		return nil, false
	}
	return lr.asRuling()
}

func (tt *textTable) reduce() *textTable {
	rows := make([]int, 0, tt.h)
	cols := make([]int, 0, tt.w)
	for i := 0; i < tt.h; i++ {
		if !tt.emptyCompositeRow(i) {
			rows = append(rows, i)
		}
	}
	for i := 0; i < tt.w; i++ {
		if !tt.emptyCompositeColumn(i) {
			cols = append(cols, i)
		}
	}
	if len(rows) == tt.h && len(cols) == tt.w {
		return tt
	}
	reduced := textTable{exportable: tt.exportable, w: len(cols), h: len(rows), cells: make(map[uint64]*textPara, len(cols)*len(rows))}
	if debugTables {
		common.Log.Info("reduce: %dx%d -> %dx%d", tt.w, tt.h, len(cols), len(rows))
		common.Log.Info("reducedCols: %+v", cols)
		common.Log.Info("reducedRows: %+v", rows)
	}
	for newRow, row := range rows {
		for newCol, col := range cols {
			paras, rect := tt.getComposite(col, row)
			if paras == nil {
				continue
			}
			if debugTables {
				fmt.Printf("  %2d, %2d (%2d, %2d) %q\n", newCol, newRow, col, row, truncate(paras.merge().text(), 50))
			}
			reduced.putComposite(newCol, newRow, paras, rect)
		}
	}
	return &reduced
}

func (pl paraList) findTextTables() []*textTable {
	var tables []*textTable
	for _, para := range pl {
		if para.taken() || para.Width() == 0 {
			continue
		}
		table := para.isAtom()
		if table == nil {
			continue
		}
		table.growTable()
		if table.w*table.h < minTableParas {
			continue
		}
		table.markCells()
		table.log("grown")
		tables = append(tables, table)
	}
	return tables
}

// PageFonts represents extracted fonts on a PDF page.
type PageFonts struct{ Fonts []Font }

// ToTextMark returns the public view of `tm`.
func (tm *textMark) ToTextMark() TextMark {
	return TextMark{Text: tm.text, Original: tm.original, BBox: tm.origBBox, Font: tm.font, FontSize: tm.fontSize, FillColor: tm.fillColor, StrokeColor: tm.strokeColor, Orientation: tm.orientation, DirectObject: tm.directObject, ObjString: tm.objString, Tw: tm.Tw, Th: tm.Th, Tc: tm.charSpacing, Tfs: tm.tfs, Index: tm.index}
}

func (e *Extractor) isTextOnly() bool {
	if e.collectPaths {
		return false
	}
	if e.options == nil {
		return false
	}
	return e.options.ExtractionMode == ExtractionModeGrid
}

func sortLinesByDepth(lines []*textLine) {
	sort.Slice(lines, func(i, j int) bool {
		a, b := lines[i], lines[j]
		return a.depth < b.depth
	})
}

func checkLicense(pageText *PageText) error {
	key := license.GetLicenseKey()
	if key != nil && key.IsLicensed() || licenseBypass {
		return nil
	}
	fmt.Printf("Unlicensed copy of UniPDF\n")
	fmt.Println("")
	return errors.New("unipdf license code required")
}

// ExtractStrokePaths processes and extracts all stroke paths in content streams.
// Path geometry is collected regardless of the configured ExtractionMode, so
// callers using ExtractionModeGrid still get stroke paths from this entry point.
func (e *Extractor) ExtractStrokePaths() ([]StrokePath, error) {
	saved := e.collectPaths
	e.collectPaths = true
	defer func() { e.collectPaths = saved }()
	pageText, _, _, err := e.ExtractPageText()
	if err != nil {
		return nil, err
	}
	paths := []StrokePath{}
	for _, stroke := range pageText.strokes {
		sp := StrokePath{Color: stroke.Color, Points: []transform.Point{}}
		for _, sub := range stroke.subpaths {
			sp.Points = append(sp.Points, sub.points...)
		}
		paths = append(paths, sp)
	}
	return paths, nil
}

type list struct {
	lines    []*textLine
	kind     string
	children []*list
	text     string
}

func (iec *imageExtractContext) extractXObjectImage(name *core.PdfObjectName, gs contentstream.GraphicsState, resources *model.PdfPageResources) error {
	xobj, _ := resources.GetXObjectByName(*name)
	if xobj == nil {
		return nil
	}
	rgbImg, ok := iec.xObjectImages[xobj]
	if !ok {
		xImg, err := resources.GetXObjectImageByName(*name)
		if err != nil {
			return err
		}
		if xImg == nil {
			return nil
		}
		img, err := xImg.ToImage()
		if err != nil {
			return err
		}
		var mask image.Image
		if xImg.Mask != nil {
			if mask, err = loadMaskImage(xImg.Mask, color.Opaque); err != nil {
				common.Log.Debug("WARN: could not get explicit image mask. Output may be incorrect.")
			}
		} else if xImg.SMask != nil {
			mask, err = loadSoftMaskImage(xImg.SMask, color.Opaque)
			if err != nil {
				common.Log.Debug("WARN: could not get soft image mask. Output may be incorrect.")
			}
		}
		if mask != nil {
			goImg, err2 := img.ToGoImage()
			if err2 != nil {
				return err2
			}
			goImg = compositeImages(goImg, mask)
			switch xImg.ColorSpace.String() {
			case "DeviceGray", "Indexed":
				img, err2 = model.ImageHandling.NewGrayImageFromGoImage(goImg)
				if err2 != nil {
					return err2
				}
			default:
				img, err2 = model.ImageHandling.NewImageFromGoImage(goImg)
				if err2 != nil {
					return err2
				}
			}
		}
		rgb, err := xImg.ColorSpace.ImageToRGB(*img)
		if err != nil {
			return err
		}
		rgbImg = unpoolImage(rgb)
		iec.xObjectImages[xobj] = rgbImg
	}
	common.Log.Debug("@Do CTM: %s", gs.CTM.String())
	imgCopy := *rgbImg
	mark := ImageMark{Image: &imgCopy, Width: gs.CTM.ScalingFactorX(), Height: gs.CTM.ScalingFactorY(), Angle: gs.CTM.Angle()}
	mark.X, mark.Y = gs.CTM.Translation()
	iec.images = append(iec.images, mark)
	iec.xObjectCount++
	return nil
}

func (r *ruling) alignsSec(other *ruling) bool {
	const secondaryAlignSlack = alignTolerance + 1.0
	return r.lo-secondaryAlignSlack <= other.hi && other.lo-secondaryAlignSlack <= r.hi
}

// String returns a string describing `ma`.
func (tma TextMarkArray) String() string {
	numMarks := len(tma.marks)
	if numMarks == 0 {
		return "EMPTY"
	}
	first := tma.marks[0]
	last := tma.marks[numMarks-1]
	return fmt.Sprintf("{TEXTMARKARRAY: %d elements\n\tfirst=%s\n\t last=%s}", numMarks, first, last)
}

// New returns an Extractor instance for extracting content from the input PDF page.
func New(page *model.PdfPage) (*Extractor, error) { return NewWithOptions(page, nil) }
func (to *textObject) moveText(tx, ty float64)    { to.moveLP(tx, ty) }
func removeWordFromSlice(words []*textWord, word *textWord) []*textWord {
	for i, w := range words {
		if w == word {
			return removeWordAt(words, i)
		}
	}
	common.Log.Error("removeWord: words doesn't contain word=%s", word)
	return nil
}

func (sp *subpath) removeDuplicates() {
	if len(sp.points) == 0 {
		return
	}
	pts := []transform.Point{sp.points[0]}
	for _, pt := range sp.points[1:] {
		if !pointsEqual(pt, pts[len(pts)-1]) {
			pts = append(pts, pt)
		}
	}
	sp.points = pts
}

func loadMaskImage(obj core.PdfObject, maskColor color.Color) (image.Image, error) {
	stream, ok := core.GetStream(obj)
	if !ok {
		return nil, nil
	}
	xImg, err := model.NewXObjectImageFromStream(stream)
	if err != nil {
		return nil, err
	}
	img, err := xImg.ToImage()
	if err != nil {
		return nil, err
	}
	return maskToImage(img, maskColor), nil
}

func (pl paraList) lines() []*textLine {
	var lines []*textLine
	for _, para := range pl {
		lines = append(lines, para.lines...)
	}
	return lines
}

func reverseInts(in []int) []int {
	out := make([]int, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}

type textLine struct {
	model.PdfRectangle
	depth    float64
	words    []*textWord
	fontSize float64
}

func (fpe *formProcessingError) Error() string {
	return fmt.Sprintf("form %s: %v", fpe.name, fpe.err)
}

func replaceRange(s string, start, end int, repl string) string {
	if end > len(s)-1 {
		return s[:start] + repl
	}
	return s[:start] + repl + s[end:]
}

func (rr rectRuling) asRuling() (*ruling, bool) {
	r := ruling{kind: rr.kind, Color: rr.Color, mark: markKindFill}
	switch rr.kind {
	case rulingVert:
		r.xy = 0.5 * (rr.Llx + rr.Urx)
		r.lo = rr.Lly
		r.hi = rr.Ury
		width, ok := rr.checkWidth(rr.Llx, rr.Urx)
		if !ok {
			if debugRectRulings {
				common.Log.Error("rectRuling.asRuling: rulingVert !checkWidth v=%+v", rr)
			}
			return nil, false
		}
		r.width = width
	case rulingHorz:
		r.xy = 0.5 * (rr.Lly + rr.Ury)
		r.lo = rr.Llx
		r.hi = rr.Urx
		width, ok := rr.checkWidth(rr.Lly, rr.Ury)
		if !ok {
			if debugRectRulings {
				common.Log.Error("rectRuling.asRuling: rulingHorz !checkWidth v=%+v", rr)
			}
			return nil, false
		}
		r.width = width
	default:
		common.Log.Error("bad primary kind=%d", rr.kind)
		return nil, false
	}
	return &r, true
}

func (tp *textPara) writeCellText(writer io.Writer, ctx *textContext) {
	for i, line := range tp.lines {
		text := line.text()
		dehyphenate := !ctx.isDehyphenationDisabled() && line.endsInHyphen() && i != len(tp.lines)-1
		if dehyphenate {
			text = dropLastRune(text)
		}
		writer.Write([]byte(text))
		if !(dehyphenate || i == len(tp.lines)-1) {
			writer.Write([]byte(separatorFor(line.depth, tp.lines[i+1].depth)))
		}
	}
}

type gridTiling struct {
	model.PdfRectangle
	llx   []float64
	lly   []float64
	tiles map[float64]map[float64]gridTile
}
type rulingList []*ruling

// TextTable represents a table.
// Cells are ordered top-to-bottom, left-to-right.
// Cells[y] is the (0-offset) y'th row in the table.
// Cells[y][x] is the (0-offset) x'th column in the table.
type TextTable struct {
	model.PdfRectangle
	W, H  int
	Cells [][]TableCell
}

func makeTextWords(marks []*textMark, pageRect model.PdfRectangle, adjustTilt bool) []*textWord {
	var words []*textWord
	var cur *textWord
	if debugWords {
		common.Log.Info("makeTextWords: %d marks", len(marks))
	}
	flush := func() {
		if cur != nil {
			text := cur.computeText()
			if !isBlank(text) {
				cur.text = text
				words = append(words, cur)
				if debugWords {
					common.Log.Info("addNewWord: %d: word=%s", len(words)-1, cur.String())
					for i, m := range cur.marks {
						fmt.Printf("%4d: %s\n", i, m.String())
					}
				}
			}
			cur = nil
		}
	}
	for _, mark := range marks {
		if mergeDiacritics && cur != nil && len(cur.marks) > 0 {
			prev := cur.marks[len(cur.marks)-1]
			comb, isComb := combiningFor(mark.text)
			prevComb, prevIsComb := combiningFor(prev.text)
			if isComb && !prevIsComb && prev.inDiacriticArea(mark) {
				cur.addDiacritic(comb)
				continue
			}
			if prevIsComb && !isComb && mark.inDiacriticArea(prev) {
				cur.marks = cur.marks[:len(cur.marks)-1]
				cur.appendMark(mark, pageRect)
				cur.addDiacritic(prevComb)
				continue
			}
		}
		blank := isBlank(mark.text)
		if blank {
			flush()
			continue
		}
		if cur == nil && !blank {
			cur = newTextWord([]*textMark{mark}, pageRect)
			continue
		}
		fontSize := cur.fontSize
		depthDelta := math.Abs(depthFromTop(pageRect, mark)-cur.depth) / fontSize
		gap := horizontalGap(mark, cur) / fontSize
		width := cur.Width() + mark.Width()
		if adjustTilt && mark.trm.Angle() != 0.0 {
			tiltAdj := math.Abs(width * math.Tan(mark.trm.Angle()) / fontSize)
			if depthDelta > tiltAdj {
				depthDelta -= tiltAdj
			} else {
				depthDelta = 0.0
			}
		}
		if gap >= maxWordGapFactor || !(-maxOverlapFactor <= gap && depthDelta <= maxDepthDeltaFactor) {
			flush()
			cur = newTextWord([]*textMark{mark}, pageRect)
			continue
		}
		cur.appendMark(mark, pageRect)
	}
	flush()
	return words
}

func appendMarkWithOffset(marks []TextMark, offset *int, mark TextMark) []TextMark {
	mark.Offset = *offset
	marks = append(marks, mark)
	*offset += len(mark.Text)
	return marks
}

type rectRuling struct {
	kind rulingKind
	mark markKind
	color.Color
	model.PdfRectangle
}

func newSubpath(pt transform.Point) *subpath { return &subpath{points: []transform.Point{pt}} }
func (tp *textPara) writeText(writer io.Writer, ctx *textContext) {
	if tp.table == nil {
		tp.writeCellText(writer, ctx)
		return
	}
	for row := 0; row < tp.table.h; row++ {
		for col := 0; col < tp.table.w; col++ {
			para := tp.table.get(col, row)
			if para == nil {
				writer.Write([]byte("\t"))
			} else {
				sortLinesByDepth(para.lines)
				para.writeCellText(writer, ctx)
			}
			writer.Write([]byte(" "))
		}
		if row < tp.table.h-1 {
			writer.Write([]byte("\n"))
		}
	}
}

func (rl rulingList) connections(adj map[int]intSet, start int) intSet {
	connected := make(intSet)
	visited := make(intSet)
	var visit func(int)
	visit = func(cur int) {
		if !visited.has(cur) {
			visited.add(cur)
			for j := range rl {
				if adj[j].has(cur) {
					connected.add(j)
				}
			}
			for j := range rl {
				if connected.has(j) {
					visit(j)
				}
			}
		}
	}
	visit(start)
	return connected
}

func (ss *shapesState) quadraticTo(x1, y1, x2, y2 float64) {
	if debugShapes {
		common.Log.Info("quadraticTo:")
	}
	ss.addPoint(x2, y2)
}
func (sp *subpath) last() transform.Point { return sp.points[len(sp.points)-1] }
func compareLlxThenDepth(a, b bounded) float64 {
	d := compareLlx(a, b)
	if !isZero(d) {
		return d
	}
	return compareDepth(a, b)
}

var blackColorKey = newColorStateKey(model.NewPdfColorspaceDeviceGray(), model.NewPdfColorDeviceGray(0))

type inlineImageKey struct {
	image     *contentstream.ContentStreamInlineImage
	resources *model.PdfPageResources
}

func singleFloatOperand(op *contentstream.ContentStreamOperation) (float64, error) {
	if len(op.Params) != 1 {
		err := errors.New("incorrect parameter count")
		common.Log.Debug("ERROR: %#q should have %d input params, got %d %+v", op.Operand, 1, len(op.Params), op.Params)
		return 0.0, err
	}
	return core.GetNumberAsFloat(op.Params[0])
}

func (tp *textPara) isAtom() *textTable {
	topLeft := tp
	right := tp.right
	below := tp.below
	if right.taken() || below.taken() {
		return nil
	}
	diag := right.below
	if diag.taken() || diag != below.right {
		return nil
	}
	return newTwoByTwoTable(topLeft, right, below, diag)
}

func containsFont(fonts []Font, name string) bool {
	for _, font := range fonts {
		if font.FontName == name {
			return true
		}
	}
	return false
}

func (pt *PageText) getParagraphs() paraList {
	var rulings rulingList
	if useStrokeRulings {
		strokeRulings := makeStrokeRulings(pt.strokes)
		rulings = append(rulings, strokeRulings...)
	}
	if useFillRulings {
		fillRulings := makeFillRulings(pt.fills)
		rulings = append(rulings, fillRulings...)
	}
	rulings, tilings := rulings.toTilings()
	var paras paraList
	numMarks := len(pt.marks)
	for angle := 0; angle < 360 && numMarks > 0; angle += 90 {
		orientMarks := make([]*textMark, 0, len(pt.marks)-numMarks)
		for _, mark := range pt.marks {
			if mark.orientation == angle {
				orientMarks = append(orientMarks, mark)
			}
		}
		if len(orientMarks) > 0 {
			orientParas := makeTextPage(orientMarks, pt.pageSize, rulings, tilings, pt.options.mode == ExtractionModeLayoutNoBreaks)
			paras = append(paras, orientParas...)
			numMarks -= len(orientMarks)
		}
	}
	return paras
}
func isRatioSmall(num, den float64) bool { return num/math.Max(slopeEpsilon, den) < maxSlope }
func (rl rulingList) primaries() []float64 {
	set := make(map[float64]struct{}, len(rl))
	for _, r := range rl {
		set[r.xy] = struct{}{}
	}
	out := make([]float64, len(set))
	n := 0
	for v := range set {
		out[n] = v
		n++
	}
	sort.Float64s(out)
	return out
}

func (pl paraList) merge() *textPara {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("merge: paras=%d ===========x=============", len(pl))
	}
	if len(pl) == 0 {
		return nil
	}
	pl.sortReadingOrder()
	rect := pl[0].PdfRectangle
	lines := pl[0].lines
	for _, para := range pl[1:] {
		rect = unionRects(rect, para.PdfRectangle)
		lines = append(lines, para.lines...)
	}
	return newTextPara(rect, lines)
}

func newColorStateKey(cs model.PdfColorspace, c model.PdfColor) colorStateKey {
	key := colorStateKey{}
	switch typed := cs.(type) {
	case *model.PdfColorspaceDeviceGray:
		key.kind = colorKindGray
	case *model.PdfColorspaceDeviceRGB:
		key.kind = colorKindRGB
	case *model.PdfColorspaceDeviceCMYK:
		key.kind = colorKindCMYK
	case *model.PdfColorspaceSpecialPattern:
		if typed.UnderlyingCS == nil {
			key.kind = colorKindPattern
		} else {
			key.colorspace = typed
		}
	default:
		key.colorspace = typed
	}
	if pat, ok := c.(*model.PdfColorPattern); ok {
		key.pattern = string(pat.PatternName)
	}
	if c != nil {
		if comps := c.Components(); len(comps) <= len(key.comps) {
			key.numComps = byte(len(comps))
			copy(key.comps[:], comps)
		} else {
			key.color = c
		}
	}
	return key
}
func (gt gridTile) complete() bool { return gt.numBorders() == 4 }
func (tt *textTable) emptyCompositeColumn(col int) bool {
	for row := 0; row < tt.h; row++ {
		if cell, ok := tt.compositeCells[cellKey(col, row)]; ok {
			if len(cell.paraList) > 0 {
				return false
			}
		}
	}
	return true
}

func (tl *textLine) appendWord(word *textWord) {
	tl.words = append(tl.words, word)
	tl.PdfRectangle = unionRects(tl.PdfRectangle, word.PdfRectangle)
	if word.fontSize > tl.fontSize {
		tl.fontSize = word.fontSize
	}
	if word.depth > tl.depth {
		tl.depth = word.depth
	}
}

func addInterval(vals []float64, lo, hi float64) []float64 {
	a, b := lo, hi
	if b < a {
		a, b = b, a
	}
	out := make([]float64, 0, len(vals)+2)
	out = append(out, lo)
	for _, v := range vals {
		if v <= a {
			continue
		} else if v >= b {
			break
		}
		out = append(out, v)
	}
	out = append(out, hi)
	return out
}

func (to *textObject) setFont(name string, size float64) error {
	if to == nil {
		return nil
	}
	to.state.tfs = size
	font, err := to.getFont(name)
	if err != nil {
		return err
	}
	to.state.tfont = font
	return nil
}

func (gt gridTiling) log(msg string) {
	if !debugGrids {
		return
	}
	common.Log.Info("tiling: %d x %d %q", len(gt.llx), len(gt.lly), msg)
	fmt.Printf("   llx=%.2f\n", gt.llx)
	fmt.Printf("   lly=%.2f\n", gt.lly)
	for i, lly := range gt.lly {
		row, ok := gt.tiles[lly]
		if !ok {
			continue
		}
		fmt.Printf("%4d: %6.2f\n", i, lly)
		for j, llx := range gt.llx {
			tile, ok2 := row[llx]
			if !ok2 {
				continue
			}
			fmt.Printf("%8d: %s\n", j, tile.String())
		}
	}
}

func (tt *textTable) get(col, row int) *textPara {
	return tt.cells[cellKey(col, row)]
}

func (wb *wordBag) applyRemovals(removals map[int]map[*textWord]struct{}) {
	for idx, rm := range removals {
		if len(rm) == 0 {
			continue
		}
		bin := wb.bins[idx]
		numKept := len(bin) - len(rm)
		if numKept == 0 {
			delete(wb.bins, idx)
			continue
		}
		kept := make([]*textWord, numKept)
		n := 0
		for _, w := range bin {
			if _, ok := rm[w]; !ok {
				kept[n] = w
				n++
			}
		}
		wb.bins[idx] = kept
	}
}

func getMatchMarks(loc []int, marks *TextMarkArray, pattern string) (*TextMarkArray, Box, error) {
	box := Box{}
	start := loc[0]
	end := loc[1]
	lead := len(pattern) - len(strings.TrimLeft(pattern, " "))
	trail := len(pattern) - len(strings.TrimRight(pattern, " \n"))
	start = start + lead
	end = end - trail
	span, err := marks.RangeOffset(start, end)
	if err != nil {
		return nil, box, err
	}
	bbox, ok := span.BBox()
	if !ok {
		return nil, box, fmt.Errorf("spanMarks.BBox has no bounding box. spanMarks=%s", span)
	}
	box = Box{BBox: bbox}
	return span, box, nil
}
func (fpe *formProcessingError) Unwrap() error { return fpe.err }
func intersectRects(a, b model.PdfRectangle) (model.PdfRectangle, bool) {
	if !rectsOverlap(a, b) {
		return model.PdfRectangle{}, false
	}
	return model.PdfRectangle{Llx: math.Max(a.Llx, b.Llx), Urx: math.Min(a.Urx, b.Urx), Lly: math.Max(a.Lly, b.Lly), Ury: math.Min(a.Ury, b.Ury)}, true
}

// Extractor stores and offers functionality for extracting content from PDF pages.
type Extractor struct {
	contents          string
	resources         *model.PdfPageResources
	mediaBox          model.PdfRectangle
	cropBox           *model.PdfRectangle
	structParentsKey  int
	fonts             map[string]fontEntry
	parsedForms       map[*core.PdfObjectStream]parsedForm
	formResults       map[formCacheKey]textResult
	annotationResults map[string]textResult
	formExtractions   int
	fontClock         int64
	options           *Options
	structTreeRoot    *model.StructTreeRoot
	pageObject        core.PdfObject
	annotations       []*model.PdfAnnotation
	collectPaths      bool
}

func horizontalGap(a, b bounded) float64 { return a.bbox().Llx - b.bbox().Urx }

// Marks returns the TextMark collection for a page. It represents all the text on the page.
func (pt PageText) Marks() *TextMarkArray { return &TextMarkArray{marks: pt.viewMarks} }

func fillStrings(strs []string, from int, to int, s string) {
	for i := from; i < to; i++ {
		strs[i] = s
	}
}

// String returns a description of `k`.
func (rk rulingKind) String() string {
	name, ok := rulingKindNames[rk]
	if !ok {
		return fmt.Sprintf("Not a ruling: %d", rk)
	}
	return name
}

func (tt *textTable) putComposite(col, row int, paras paraList, rect model.PdfRectangle) {
	if len(paras) == 0 {
		common.Log.Error("textTable) putComposite: empty paras")
		return
	}
	cell := compositeCell{PdfRectangle: rect, paraList: paras}
	if debugTables {
		fmt.Printf("        putComposite(%d,%d)<-%s\n", col, row, cell.String())
	}
	cell.updateBBox()
	tt.compositeCells[cellKey(col, row)] = cell
}

func subTextForMark(s string, start, length, limit int) string {
	if length == 0 {
		return ""
	}
	end := start + length
	if end >= limit || end > len(s) {
		end = len(s)
	} else {
		end = alignToRuneStart(s, end)
	}
	from := alignToRuneStart(s, start)
	if from >= end {
		return ""
	}
	return s[from:end]
}

func rowCorridors(cells []compositeCell) []float64 {
	var lines []*textLine
	numParas := 0
	for _, cell := range cells {
		numParas += len(cell.paraList)
		lines = append(lines, cell.lines()...)
	}
	sort.Slice(lines, func(i, j int) bool {
		a, b := lines[i], lines[j]
		da, db := a.depth, b.depth
		if !isZero(da - db) {
			return da < db
		}
		return a.Llx < b.Llx
	})
	if debugTables {
		fmt.Printf("    rowBorders: %d paras %d lines\n", numParas, len(lines))
		for i, l := range lines {
			fmt.Printf("%8d: %s\n", i, l)
		}
	}
	var corridors []float64
	lowest := lines[0]
	var groups [][]*textLine
	group := []*textLine{lowest}
	for i, line := range lines[1:] {
		if line.Ury < lowest.Lly {
			y := 0.5 * (line.Ury + lowest.Lly)
			if debugTables {
				fmt.Printf("%4d: %6.2f < %6.2f border=%6.2f\n"+"\t q=%s\n\t p=%s\n", i, line.Ury, lowest.Lly, y, lowest, line)
			}
			corridors = append(corridors, y)
			groups = append(groups, group)
			group = nil
		}
		group = append(group, line)
		if line.Lly < lowest.Lly {
			lowest = line
		}
	}
	if len(group) > 0 {
		groups = append(groups, group)
	}
	if debugTables {
		fmt.Printf("        rowCorridors=%6.2f\n", corridors)
	}
	if debugTables {
		common.Log.Info("row=%d", len(cells))
		for i, c := range cells {
			fmt.Printf("%4d: %s\n", i, c)
		}
		common.Log.Info("groups=%d", len(groups))
		for i, g := range groups {
			fmt.Printf("%4d: %d\n", i, len(g))
			for j, l := range g {
				fmt.Printf("%8d: %s\n", j, l)
			}
		}
	}
	allSpan := true
	for i, g := range groups {
		spans := true
		for j, c := range cells {
			if debugTables {
				fmt.Printf("   ~~~group %d of %d cell %d of %d %s\n", i, len(groups), j, len(cells), c)
			}
			if !c.hasLines(g) {
				if debugTables {
					fmt.Printf("   !!!group %d of %d cell %d of %d OUT\n", i, len(groups), j, len(cells))
				}
				spans = false
				break
			}
		}
		if !spans {
			allSpan = false
			break
		}
	}
	if !allSpan {
		if debugTables {
			common.Log.Info("row corridors don't span all cells in row. ignoring")
		}
		corridors = nil
	}
	if debugTables && corridors != nil {
		fmt.Printf("        ***rowCorridors=%6.2f\n", corridors)
	}
	return corridors
}

func (pl paraList) list() []*list {
	var listLines []*textLine
	var allLines []*textLine
	for _, para := range pl {
		ll := para.getListLines()
		listLines = append(listLines, ll...)
		allLines = append(allLines, para.lines...)
	}
	byStartX := groupLinesByDepth(listLines)
	items := buildListsFromLines(allLines, byStartX)
	return items
}

func groupLinesByMCID(paras *paraList) map[int][]*textLine {
	byMCID := map[int][]*textLine{}
	for _, para := range *paras {
		for _, line := range para.lines {
			if !hasSingleMCID(line) {
				common.Log.Debug("groupLines: The text line contains more than one mcid number. It should be split.")
				continue
			}
			mcid := line.words[0].marks[0].mcid
			byMCID[mcid] = append(byMCID[mcid], line)
		}
		if para.table != nil {
			cells := para.table.cells
			for _, cell := range cells {
				for _, line := range cell.lines {
					if !hasSingleMCID(line) {
						common.Log.Debug("groupLines: The text line contains more than one mcid number. It should be split.")
						continue
					}
					mcid := line.words[0].marks[0].mcid
					byMCID[mcid] = append(byMCID[mcid], line)
				}
			}
		}
	}
	return byMCID
}

// String returns a string describing `tm`.
func (tm TextMark) String() string {
	bbox := tm.BBox
	var font string
	if tm.Font != nil {
		font = tm.Font.String()
		if len(font) > 50 {
			font = font[:50] + "..."
		}
	}
	var meta string
	if tm.Meta {
		meta = " *M*"
	}
	return fmt.Sprintf("{TextMark: %d %q=%02x (%6.2f, %6.2f) (%6.2f, %6.2f) %s%s}", tm.Offset, tm.Text, []rune(tm.Text), bbox.Llx, bbox.Lly, bbox.Urx, bbox.Ury, font, meta)
}

type gridRow struct {
	words []gridWord
	y     float64
}

func buildList(root *model.StructTreeRoot, linesByMCID map[int][]*textLine, pageObj core.PdfObject) []*list {
	if root == nil {
		common.Log.Debug("buildList: treeRoot is nil")
		return nil
	}
	kvs := []*model.KValue{}
	var walk func(kv *model.KValue)
	walk = func(kv *model.KValue) {
		if kd := kv.GetKDict(); kd != nil {
			if kd.S.String() == "L" {
				kvs = append(kvs, kv)
			} else {
				for _, child := range kd.GetChildren() {
					walk(child)
				}
			}
		}
	}
	for _, kd := range root.K {
		rootKV := model.NewKValue()
		rootKV.SetKDict(kd)
		walk(rootKV)
	}
	top := listsFromStructTree(kvs, linesByMCID, pageObj)
	var out []*list
	for _, l := range top {
		items := collectListItems(l)
		out = append(out, items...)
	}
	return out
}

func makeWordBag(words []*textWord, pageHeight float64, vertRulings, horzRulings rulingList) *wordBag {
	bag := newWordBag(words[0], pageHeight, vertRulings, horzRulings)
	for _, word := range words[1:] {
		idx := depthBinIndex(word.depth)
		bag.bins[idx] = append(bag.bins[idx], word)
		bag.PdfRectangle = unionRects(bag.PdfRectangle, word.PdfRectangle)
	}
	bag.sort()
	return bag
}

func mergeWordBags(bags []*wordBag) []*wordBag {
	if len(bags) <= 1 {
		return bags
	}
	if debugWordBags {
		common.Log.Info("mergeWordBags:")
	}
	sort.Slice(bags, func(i, j int) bool {
		a, b := bags[i], bags[j]
		areaA := a.Width() * a.Height()
		areaB := b.Width() * b.Height()
		if areaA != areaB {
			return areaA > areaB
		}
		if a.Height() != b.Height() {
			return a.Height() > b.Height()
		}
		return i < j
	})
	var merged []*wordBag
	absorbed := make(intSet)
	for i := 0; i < len(bags); i++ {
		if absorbed.has(i) {
			continue
		}
		big := bags[i]
		for j := i + 1; j < len(bags); j++ {
			if absorbed.has(i) {
				continue
			}
			small := bags[j]
			expanded := big.PdfRectangle
			expanded.Llx -= big.maxFontSize
			if rectContains(expanded, small.PdfRectangle) {
				big.absorb(small)
				absorbed.add(j)
			}
		}
		merged = append(merged, big)
	}
	if len(bags) != len(merged)+len(absorbed) {
		common.Log.Error("mergeWordBags: %d→%d absorbed=%d", len(bags), len(merged), len(absorbed))
	}
	return merged
}

var metaMark = TextMark{Text: "[X]", Original: " ", Meta: true, FillColor: color.White, StrokeColor: color.White}

func (wb *wordBag) blocked(word *textWord) bool {
	if word.Urx < wb.Llx {
		wordRight := rightEdgeRuling(word.PdfRectangle)
		bagLeft := leftEdgeRuling(wb.PdfRectangle)
		if wb.vertRulings.blocks(wordRight, bagLeft) {
			if debugBlocked {
				common.Log.Info("blocked ←x: %s %s", word, wb)
			}
			return true
		}
	} else if wb.Urx < word.Llx {
		bagRight := rightEdgeRuling(wb.PdfRectangle)
		wordLeft := leftEdgeRuling(word.PdfRectangle)
		if wb.vertRulings.blocks(bagRight, wordLeft) {
			if debugBlocked {
				common.Log.Info("blocked x→ : %s %s", word, wb)
			}
			return true
		}
	}
	if word.Ury < wb.Lly {
		wordTop := topEdgeRuling(word.PdfRectangle)
		bagBottom := bottomEdgeRuling(wb.PdfRectangle)
		if wb.horzRulings.blocks(wordTop, bagBottom) {
			if debugBlocked {
				common.Log.Info("blocked ←y: %s %s", word, wb)
			}
			return true
		}
	} else if wb.Ury < word.Lly {
		bagTop := topEdgeRuling(wb.PdfRectangle)
		wordBottom := bottomEdgeRuling(word.PdfRectangle)
		if wb.horzRulings.blocks(bagTop, wordBottom) {
			if debugBlocked {
				common.Log.Info("blocked y→ : %s %s", word, wb)
			}
			return true
		}
	}
	return false
}

func (ss *shapesState) stroke(sections *[]pathSection) {
	section := pathSection{subpaths: ss.subpaths, Color: ss.textObj.getStrokeColor()}
	*sections = append(*sections, section)
	if debugRulings {
		fmt.Printf("    STROKE: %d strokes ss=%s color=%+v %6.2f\n", len(*sections), ss, ss.textObj.getStrokeColor(), section.bbox())
		if debugSubpaths {
			for i, sp := range ss.subpaths {
				fmt.Printf("%8d: %s\n", i, sp)
				if i == 10 {
					break
				}
			}
		}
	}
}

const (
	removeDuplicateWords = true
	mergeDiacritics      = true
	applyEnlargedBBoxes  = false
	useGridTables        = true
	useTextTables        = true
	useStrokeRulings     = true
	useFillRulings       = true
	checkGridCorners     = false
)

func (tl *textLine) toTextMarks(offset *int) []TextMark {
	var marks []TextMark
	for _, word := range tl.words {
		if word.spaceBefore {
			marks = appendMetaMark(marks, offset, " ")
		}
		wordMarks := word.toTextMarks(offset)
		marks = append(marks, wordMarks...)
	}
	return marks
}

type markKind int

func (wb *wordBag) depthIndexes() []int {
	if len(wb.bins) == 0 {
		return nil
	}
	idxs := make([]int, len(wb.bins))
	n := 0
	for i := range wb.bins {
		idxs[n] = i
		n++
	}
	sort.Ints(idxs)
	return idxs
}

func replaceSingleByteString(marks *TextMarkArray, repl string, offset *int, limit int) error {
	var prevMark TextMark
	for _, mark := range marks.Elements() {
		text := mark.Text
		font := mark.Font
		sub := subTextForMark(repl, *offset, len(text), limit)
		directObject := mark.DirectObject
		if directObject == nil && mark.Text == " " {
			objStr := prevMark.ObjString
			lastObj := objStr[len(objStr)-1]
			if lastObj != mark.Text {
				directObject = prevMark.DirectObject
				font = prevMark.Font
				pdfStr, ok := core.GetString(directObject)
				if !ok {
					return fmt.Errorf("unable to get string Bytes from directObject")
				}
				raw, ok := core.GetStringBytes(directObject)
				if !ok {
					return core.ErrTypeError
				}
				prevText := decodeBytes(raw, font)
				sub = prevText + sub
				setStringFromText(pdfStr, sub, font)
				*offset += len(text)
				continue
			}
		}
		pdfStr, ok2 := core.GetString(directObject)
		if !ok2 {
			return fmt.Errorf("unable to get string Bytes from directObject")
		}
		newText := ""
		raw, ok2 := core.GetStringBytes(directObject)
		if !ok2 {
			return core.ErrTypeError
		}
		oldText := decodeBytes(raw, font)
		newText = strings.Replace(oldText, text, sub, 1)
		setStringFromText(pdfStr, newText, font)
		*offset += len(text)
		prevMark = mark
	}
	return nil
}
func depthFromTop(rect model.PdfRectangle, b bounded) float64 { return rect.Ury - b.bbox().Lly }

// GridText returns the page text laid out on a fixed-width character grid
// using each word's spatial position. Multi-column documents and tables
// retain their visual column alignment, similar to the output of
// `pdftotext -layout`. Available in any extraction mode.
//
// In ExtractionModeGrid the grid layout is the same string returned by Text(),
// so the cached viewText is reused — including after ApplyArea has narrowed
// the scope, even if the filtered area produced an empty result. In other
// modes the grid is computed on demand from the page's full mark set.
func (pt PageText) GridText() string {
	if pt.options.mode == ExtractionModeGrid {
		return pt.text
	}
	return pt.makeGridText(pt.marks)
}

func (pl paraList) yNeighbours(tol float64) map[*textPara][]int {
	evs := make([]event, 2*len(pl))
	if tol == 0 {
		for i, para := range pl {
			evs[2*i] = event{para.Lly, true, i}
			evs[2*i+1] = event{para.Ury, false, i}
		}
	} else {
		for i, para := range pl {
			evs[2*i] = event{para.Lly - tol*para.fontsize(), true, i}
			evs[2*i+1] = event{para.Ury + tol*para.fontsize(), false, i}
		}
	}
	return pl.eventNeighbours(evs)
}

func (wb *wordBag) allWords() []*textWord {
	var words []*textWord
	for _, bin := range wb.bins {
		words = append(words, bin...)
	}
	return words
}
func isOdd(n int) bool { return (n & 1) != 0 }
func corridorOffsets(n int, corridors map[int][]float64) ([]int, int) {
	offsets := make([]int, n)
	total := 0
	for i := 0; i < n; i++ {
		offsets[i] = total
		total += len(corridors[i]) + 1
	}
	return offsets, total
}

func (pl paraList) extractTables(tilings []gridTiling) paraList {
	if debugTables {
		common.Log.Debug("extractTables=%d ===========x=============", len(pl))
	}
	if len(pl) < minTableParas {
		return pl
	}
	tables := pl.findTables(tilings)
	if debugTables {
		common.Log.Info("combined tables %d ================", len(tables))
		for i, table := range tables {
			table.log(fmt.Sprintf("combined %d", i))
		}
	}
	return pl.applyTables(tables)
}

func linesWithBullets(lines []*textLine) []*textLine {
	out := []*textLine{}
	for _, line := range lines {
		text := line.text()
		match := listBulletRegexp.Find([]byte(text))
		if match != nil {
			out = append(out, line)
		}
	}
	return out
}

func (pl paraList) computeEBBoxes() {
	if debugEBBoxes {
		common.Log.Info("computeEBBoxes:")
	}
	for _, para := range pl {
		para.eBBox = para.PdfRectangle
	}
	m := pl.yNeighbours(0)
	for i, p := range pl {
		box := p.eBBox
		leftLimit, rightLimit := -1.0e9, +1.0e9
		for _, nbIdx := range m[p] {
			nb := pl[nbIdx].eBBox
			if nb.Urx < box.Llx {
				leftLimit = math.Max(leftLimit, nb.Urx)
			} else if box.Urx < nb.Llx {
				rightLimit = math.Min(rightLimit, nb.Llx)
			}
		}
		for j, q := range pl {
			qBox := q.eBBox
			if i == j || qBox.Ury > box.Lly {
				continue
			}
			if leftLimit <= qBox.Llx && qBox.Llx < box.Llx {
				box.Llx = qBox.Llx
			} else if qBox.Urx <= rightLimit && box.Urx < qBox.Urx {
				box.Urx = qBox.Urx
			}
		}
		if debugEBBoxes {
			fmt.Printf("%4d: %6.2f→%6.2f %q\n", i, p.eBBox, box, truncate(p.text(), 50))
		}
		p.eBBox = box
	}
	if applyEnlargedBBoxes {
		for _, p := range pl {
			p.PdfRectangle = p.eBBox
		}
	}
}

func (pl paraList) tables(ctx *textContext) []TextTable {
	var tables []TextTable
	if debugTables {
		common.Log.Info("paras.tables:")
	}
	for _, para := range pl {
		table := para.table
		if table != nil && table.isExportable() {
			tables = append(tables, table.toTextTable(ctx))
		}
	}
	return tables
}

func newBidiText(s string, ltr bool, vertical bool) BidiText {
	dir := "ltr"
	if vertical {
		dir = "ttb"
	} else if !ltr {
		dir = "rtl"
	}
	return BidiText{text: s, direction: dir}
}

func (to *textObject) showText(obj core.PdfObject, raw []byte, mcid int, actualText string) error {
	return to.renderText(obj, raw, mcid, actualText)
}

func (wb *wordBag) pullWord(word *textWord, idx int, removals map[int]map[*textWord]struct{}) {
	wb.PdfRectangle = unionRects(wb.PdfRectangle, word.PdfRectangle)
	if word.fontSize > wb.maxFontSize {
		wb.maxFontSize = word.fontSize
	}
	wb.bins[idx] = append(wb.bins[idx], word)
	removals[idx][word] = struct{}{}
}
func (sp *subpath) clear()          { *sp = subpath{} }
func roundToGrid(x float64) float64 { return coordGranularity * math.Round(x/coordGranularity) }
func cellKey(col, row int) uint64   { return uint64(col)*0x1000000 + uint64(row) }
func (tp *textPara) taken() bool    { return tp == nil || tp.isTaken }
func (tt *textTable) emptyCompositeRow(row int) bool {
	for i := 0; i < tt.w; i++ {
		if cell, ok := tt.compositeCells[cellKey(i, row)]; ok {
			if len(cell.paraList) > 0 {
				return false
			}
		}
	}
	return true
}

func (wb *wordBag) advanceBinStart(idx int) int {
	binStart := wb.binStart[idx]
	bin := wb.bins[idx]
	for binStart < len(bin) {
		if _, ok := wb.removed[bin[binStart]]; !ok {
			break
		}
		binStart++
	}
	wb.binStart[idx] = binStart
	return binStart
}

func compositeImages(src, mask image.Image) image.Image {
	maskSize, srcSize := mask.Bounds().Size(), src.Bounds().Size()
	w, h := maskSize.X, maskSize.Y
	if srcSize.X > w {
		w = srcSize.X
	}
	if srcSize.Y > h {
		h = srcSize.Y
	}
	rect := image.Rect(0, 0, w, h)
	if maskSize.X != w || maskSize.Y != h {
		scaledMask := image.NewRGBA(rect)
		imagedraw.BiLinear.Scale(scaledMask, rect, src, mask.Bounds(), imagedraw.Over, nil)
		mask = scaledMask
	}
	if srcSize.X != w || srcSize.Y != h {
		scaledSrc := image.NewRGBA(rect)
		imagedraw.BiLinear.Scale(scaledSrc, rect, src, src.Bounds(), imagedraw.Over, nil)
		src = scaledSrc
	}
	out := image.NewRGBA(rect)
	imagedraw.DrawMask(out, rect, src, image.Point{}, mask, image.Point{}, imagedraw.Over)
	return out
}

func findAllMatches(pattern string, text string) ([][]int, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("error compiling regex pattern: %w", err)
	}
	matches := re.FindAllStringIndex(text, -1)
	return matches, nil
}

// Editor represents a document editor object
type Editor struct{ reader *model.PdfReader }

func (tt *textTable) put(col, row int, para *textPara) {
	tt.cells[cellKey(col, row)] = para
}

func (wb *wordBag) text() string {
	words := wb.allWords()
	strs := make([]string, len(words))
	for i, word := range words {
		strs[i] = word.text
	}
	return strings.Join(strs, " ")
}

func (to *textObject) getFont(name string) (*model.PdfFont, error) {
	if to.extractor.fonts != nil {
		dict, err := to.getFontDict(name)
		if err != nil {
			common.Log.Debug("ERROR: getFont: name=%s, error: %s", name, err.Error())
			return nil, err
		}
		to.extractor.fontClock++
		entry, ok := to.extractor.fonts[dict.String()]
		if ok {
			entry.lastUsed = to.extractor.fontClock
			return entry.font, nil
		}
	}
	dict, err2 := to.getFontDict(name)
	if err2 != nil {
		return nil, err2
	}
	fontDirect, err2 := to.getFontDirect(name)
	if err2 != nil {
		return nil, err2
	}
	if to.extractor.fonts != nil {
		entry := fontEntry{fontDirect, to.extractor.fontClock}
		if len(to.extractor.fonts) >= maxCachedFonts {
			var keys []string
			for key := range to.extractor.fonts {
				keys = append(keys, key)
			}
			sort.Slice(keys, func(i, j int) bool {
				return to.extractor.fonts[keys[i]].lastUsed < to.extractor.fonts[keys[j]].lastUsed
			})
			delete(to.extractor.fonts, keys[0])
		}
		to.extractor.fonts[dict.String()] = entry
	}
	return fontDirect, nil
}

func (rl rulingList) log(label string) {
	if !debugRulings {
		return
	}
	common.Log.Info("### %10s: vecs=%s", label, rl.String())
	for i, r := range rl {
		fmt.Printf("%4d: %s\n", i, r.String())
	}
}
func (lr lineRuling) xMean() float64 { return 0.5 * (lr.p0.X + lr.p1.X) }

// String returns a description of `b`.
func (wb *wordBag) String() string {
	var strs []string
	for _, n := range wb.depthIndexes() {
		bin := wb.bins[n]
		for _, bin2 := range bin {
			strs = append(strs, bin2.text)
		}
	}
	return fmt.Sprintf("%.2f fontsize=%.2f %d %q", wb.PdfRectangle, wb.maxFontSize, len(strs), strs)
}

type compositeCell struct {
	model.PdfRectangle
	paraList
}

func (is intSet) add(v int) { is[v] = struct{}{} }
func roundToMultiple(v float64, multiple int) int {
	if multiple == 0 {
		multiple = 1
	}
	m := float64(multiple)
	return int(math.Round(v/m) * m)
}

func (rl rulingList) toGrids() []rulingList {
	if debugRulings {
		common.Log.Info("toGrids: %s", rl)
	}
	inter := rl.intersections()
	if debugRulings {
		common.Log.Info("toGrids: vecs=%d intersects=%d ", len(rl), len(inter))
		for _, k := range sortedIntSetKeys(inter) {
			fmt.Printf("%4d: %+v\n", k, inter[k])
		}
	}
	conn := make(map[int]intSet, len(rl))
	for i := range rl {
		s := rl.connections(inter, i)
		if len(s) > 0 {
			conn[i] = s
		}
	}
	if debugRulings {
		common.Log.Info("toGrids: connects=%d", len(conn))
		for _, k := range sortedIntSetKeys(conn) {
			fmt.Printf("%4d: %+v\n", k, conn[k])
		}
	}
	order := sortedIndexes(len(rl), func(i, j int) bool {
		ci, cj := len(conn[i]), len(conn[j])
		if ci != cj {
			return ci > cj
		}
		return rl.comp(i, j)
	})
	if debugRulings {
		common.Log.Info("toGrids: ordering=%v", order)
	}
	groups := [][]int{{order[0]}}
nextIndex:
	for _, idx := range order[1:] {
		for gi, grp := range groups {
			for _, member := range grp {
				if conn[member].has(idx) {
					groups[gi] = append(grp, idx)
					continue nextIndex
				}
			}
		}
		groups = append(groups, []int{idx})
	}
	if debugRulings {
		common.Log.Info("toGrids: igrids=%v", groups)
	}
	sort.SliceStable(groups, func(i, j int) bool { return len(groups[i]) > len(groups[j]) })
	for _, grp := range groups {
		sort.Slice(grp, func(i, j int) bool { return rl.comp(grp[i], grp[j]) })
	}
	grids := make([]rulingList, len(groups))
	for gi, grp := range groups {
		grid := make(rulingList, len(grp))
		for mi, m := range grp {
			grid[mi] = rl[m]
		}
		grids[gi] = grid
	}
	if debugRulings {
		common.Log.Info("toGrids: grids=%+v", grids)
	}
	var actual []rulingList
	for _, g := range grids {
		if ag, ok := g.isActualGrid(); ok {
			g = ag
			g = g.snapToGroups()
			actual = append(actual, g)
		}
	}
	if debugRulings {
		logGrids("toGrids: actualGrids", actual)
		common.Log.Info("toGrids: grids=%d actualGrids=%d", len(grids), len(actual))
	}
	return actual
}

func (tt *textTable) isExportable() bool {
	if tt.exportable {
		return true
	}
	isSparse := func(row int) bool {
		para := tt.get(0, row)
		if para == nil {
			return false
		}
		text := para.text()
		numRunes := utf8.RuneCountInString(text)
		isNum := bareNumberRegexp.MatchString(text)
		return numRunes <= 1 || isNum
	}
	for i := 0; i < tt.h; i++ {
		if !isSparse(i) {
			return true
		}
	}
	return false
}

func (ss *shapesState) addPoint(x, y float64) {
	sp := ss.establishSubpath()
	pt := ss.devicePoint(x, y)
	if sp == nil {
		ss.fresh = true
		ss.currentPoint = pt
	} else {
		sp.add(pt)
	}
}

func (pl paraList) reorder(idxs []int) {
	paras := make(paraList, len(pl))
	for i, idx := range idxs {
		paras[i] = pl[idx]
	}
	copy(pl, paras)
}

func joinLinesText(lines []*textLine, prefix string) string {
	var builder strings.Builder
	nextDepth := 0.0
	for i, line := range lines {
		text := line.text()
		depth := line.depth
		if i < len(lines)-1 {
			nextDepth = lines[i+1].depth
		} else {
			nextDepth = 0.0
		}
		builder.WriteString(prefix)
		builder.WriteString(text)
		if nextDepth != depth {
			builder.WriteString("\n")
		} else {
			builder.WriteString(" ")
		}
	}
	return builder.String()
}

func sortedLineDepths(m map[float64][]*textLine) []float64 {
	depths := []float64{}
	for d := range m {
		depths = append(depths, d)
	}
	sort.Float64s(depths)
	return depths
}

func (to *textObject) setCharSpacing(spacing float64) {
	if to == nil {
		return
	}
	to.state.tc = spacing
	if debugTextState {
		common.Log.Info("setCharSpacing: %.2f state=%s", spacing, to.state.String())
	}
}

// RenderMode specifies the text rendering mode (Tmode), which determines whether showing text shall cause
// glyph outlines to be  stroked, filled, used as a clipping boundary, or some combination of the three.
// Stroking, filling, and clipping shall have the same effects for a text object as they do for a path object
// (see 8.5.3, "Path-Painting Operators" and 8.5.4, "Clipping Path Operators").
type RenderMode int

func (rl rulingList) intersections() map[int]intSet {
	var vert, horz []int
	for i, r := range rl {
		switch r.kind {
		case rulingVert:
			vert = append(vert, i)
		case rulingHorz:
			horz = append(horz, i)
		}
	}
	if len(vert) < minGridCols+1 || len(horz) < minGridRows+1 {
		return nil
	}
	if len(vert)+len(horz) > maxGridRulings {
		common.Log.Debug("intersections: TOO MANY rulings vecs=%d = %d x %d", len(rl), len(vert), len(horz))
		return nil
	}
	inter := make(map[int]intSet, len(vert)+len(horz))
	for _, vert2 := range vert {
		for _, horz2 := range horz {
			if rl[vert2].intersects(rl[horz2]) {
				if _, ok := inter[vert2]; !ok {
					inter[vert2] = make(intSet)
				}
				if _, ok := inter[horz2]; !ok {
					inter[horz2] = make(intSet)
				}
				inter[vert2].add(horz2)
				inter[horz2].add(vert2)
			}
		}
	}
	return inter
}

func makeTextPage(marks []*textMark, pageSize model.PdfRectangle, rulings rulingList, tilings []gridTiling, noBreaks bool) paraList {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("makeTextPage: %d elements pageSize=%.2f", len(marks), pageSize)
	}
	if len(marks) == 0 {
		return nil
	}
	words := makeTextWords(marks, pageSize, false)
	if len(words) == 0 {
		return nil
	}
	rulings.log("makeTextPage")
	vertRulings, horzRulings := rulings.vertsHorzs()
	bag := makeWordBag(words, pageSize.Ury, vertRulings, horzRulings)
	bags := splitWordBag(bag, pageSize.Ury, vertRulings, horzRulings, noBreaks)
	bags = mergeWordBags(bags)
	paras := make(paraList, 0, len(bags))
	for _, bag2 := range bags {
		para := bag2.arrangeText(noBreaks)
		if para != nil {
			paras = append(paras, para)
		}
	}
	if len(paras) >= minTableParas {
		paras = paras.extractTables(tilings)
	}
	paras.sortReadingOrder()
	paras.sortTopoOrder()
	paras.log("sorted in reading order")
	return paras
}

const (
	debugEBBoxes          = false
	debugWords            = false
	debugOperands         = false
	debugTextRender       = false
	debugShapes           = false
	debugTextState        = false
	debugSplitBags        = false
	debugTopoOrder        = false
	debugWordBags         = false
	debugWordBagsPage     = debugWordBags && true
	debugWordBagsArrange  = debugWordBagsPage && false
	debugRemoveDuplicates = debugWordBags && true
	debugTables           = false
	debugGrowTable        = debugTables && false
	debugTablesVerbose    = debugTables && true
	debugRulings          = false
	debugSubpaths         = debugRulings && false
	debugMergeRulings     = debugRulings && false
	debugGrids            = debugRulings && true
	debugRectRulings      = debugRulings && false
	debugBlocked          = debugRulings && false
)

func (ss *stateStack) pop() *textState {
	if ss.empty() {
		return nil
	}
	state := *(*ss)[len(*ss)-1]
	*ss = (*ss)[:len(*ss)-1]
	return &state
}

func newLineFromBag(bag *wordBag, n int) *textLine {
	word := bag.firstWord(n)
	line := textLine{PdfRectangle: word.PdfRectangle, fontSize: word.fontSize, depth: word.depth}
	line.pullWord(bag, word, n)
	return &line
}

func writeListText(listV *list, builder *strings.Builder, indent *string) {
	s := indentListText(listV, indent)
	builder.WriteString(s)
	for _, children := range listV.children {
		childIndent := *indent + "   "
		writeListText(children, builder, &childIndent)
	}
}
func (to *textObject) nextLine() { to.moveLP(0, -to.state.tl) }

var romanNumeralPattern string = "(?i)^(M{0,3})(C(?:D|M)|D?C{0,3})(X(?:L|C)|L?X{0,3})(I(?:V|X)|V?I{0,3})(\\)|\\.)|^\\((M{0,3})(C(?:D|M)|D?C{0,3})(X(?:L|C)|L?X{0,3})(I(?:V|X)|V?I{0,3})\\)"

func (rl rulingList) isActualGrid() (rulingList, bool) {
	verts, horzs := rl.augmentGrid()
	if !(len(verts) >= minGridCols+1 && len(horzs) >= minGridRows+1) {
		if debugRulings {
			common.Log.Info("isActualGrid: Not aligned. %d x %d < %d x %d", len(verts), len(horzs), minGridCols+1, minGridRows+1)
		}
		return nil, false
	}
	if debugRulings {
		common.Log.Info("isActualGrid: %s : %t & %t → %t", rl, len(verts) >= 2, len(horzs) >= 2, len(verts) >= 2 && len(horzs) >= 2)
		for i, r := range rl {
			fmt.Printf("%4d: %v\n", i, r)
		}
	}
	if checkGridCorners {
		firstV, lastV := verts[0], verts[len(verts)-1]
		firstH, lastH := horzs[0], horzs[len(horzs)-1]
		if !(closeEnough(firstV.xy-firstH.lo) && closeEnough(lastV.xy-firstH.hi) && closeEnough(firstH.xy-firstV.hi) && closeEnough(lastH.xy-firstV.lo)) {
			if debugRulings {
				common.Log.Info("isActualGrid:  Not aligned.\n\tv0=%s\n\tv1=%s\n\th0=%s\n\th1=%s", firstV, lastV, firstH, lastH)
			}
			return nil, false
		}
	} else {
		if !verts.aligned() {
			if debugMergeRulings {
				common.Log.Info("isActualGrid: Not aligned verts. %d", len(verts))
			}
			return nil, false
		}
		if !horzs.aligned() {
			if debugRulings {
				common.Log.Info("isActualGrid: Not aligned horzs. %d", len(horzs))
			}
			return nil, false
		}
	}
	all := append(verts, horzs...)
	return all, true
}

func buildListsFromLines(lines []*textLine, byStartX map[float64][]*textLine) []*list {
	depths := sortedLineDepths(byStartX)
	items := []*list{}
	if len(depths) == 0 {
		return items
	}
	depth2 := depths[0]
	level := 1
	topLines := byStartX[depth2]
	for i, line := range topLines {
		var limit float64
		children := []*list{}
		depth := line.depth
		nextDepth := -1.0
		if i < len(topLines)-1 {
			nextDepth = topLines[i+1].depth
		}
		if level < len(depths) {
			children = buildChildLists(lines, byStartX, depths, level, depth, nextDepth)
		}
		limit = nextDepth
		if len(children) > 0 {
			firstChild := children[0]
			if len(firstChild.lines) > 0 {
				limit = firstChild.lines[0].depth
			}
		}
		itemLines := []*textLine{line}
		cont := bulletContinuationLines(line, lines, depth, limit)
		itemLines = append(itemLines, cont...)
		item := newList(itemLines, "bullet", children)
		item.text = joinLinesText(itemLines, "")
		items = append(items, item)
	}
	return items
}

func sortedIndexes(n int, less func(int, int) bool) []int {
	idxs := make([]int, n)
	for i := range idxs {
		idxs[i] = i
	}
	sort.Slice(idxs, func(n2, n3 int) bool { return less(idxs[n2], idxs[n3]) })
	return idxs
}

func (tt *textTable) subdivide() *textTable {
	tt.logComposite("subdivide")
	rowCorr := tt.compositeRowCorridors()
	colCorr := tt.compositeColCorridors()
	if debugTables {
		common.Log.Info("subdivide:\n\trowCorridors=%s\n\tcolCorridors=%s", corridorsString(rowCorr), corridorsString(colCorr))
	}
	if len(rowCorr) == 0 || len(colCorr) == 0 {
		return tt
	}
	fixCells(rowCorr)
	fixCells(colCorr)
	if debugTables {
		common.Log.Info("subdivide fixed:\n\trowCorridors=%s\n\tcolCorridors=%s", corridorsString(rowCorr), corridorsString(colCorr))
	}
	rowOff, totalRows := corridorOffsets(tt.h, rowCorr)
	colOff, totalCols := corridorOffsets(tt.w, colCorr)
	cells := make(map[uint64]*textPara, totalCols*totalRows)
	out := &textTable{PdfRectangle: tt.PdfRectangle, exportable: tt.exportable, h: totalRows, w: totalCols, cells: cells}
	if debugTables {
		common.Log.Info("subdivide: composite = %d x %d cells= %d x %d\n"+"\trowCorridors=%s\n"+"\tcolCorridors=%s\n"+"\tyOffsets=%+v\n"+"\txOffsets=%+v", tt.w, tt.h, totalCols, totalRows, corridorsString(rowCorr), corridorsString(colCorr), rowOff, colOff)
	}
	for row := 0; row < tt.h; row++ {
		r0 := rowOff[row]
		for col := 0; col < tt.w; col++ {
			c0 := colOff[col]
			if debugTables {
				fmt.Printf("%6d, %2d: x0=%d y0=%d\n", col, row, c0, r0)
			}
			cell, ok := tt.compositeCells[cellKey(col, row)]
			if !ok {
				continue
			}
			sub := cell.split(rowCorr[row], colCorr[col])
			for subRow := 0; subRow < sub.h; subRow++ {
				for subCol := 0; subCol < sub.w; subCol++ {
					para := sub.get(subCol, subRow)
					out.put(c0+subCol, r0+subRow, para)
					if debugTables {
						fmt.Printf("%8d, %2d: %s\n", c0+subCol, r0+subRow, para)
					}
				}
			}
		}
	}
	return out
}

func decodeBytes(buf []byte, font *model.PdfFont) string {
	codes := font.BytesToCharcodes(buf)
	strs, numChars, numMisses := font.CharcodesToStrings(codes, "")
	if numMisses > 0 {
		common.Log.Debug("renderText: numChars=%d numMisses=%d", numChars, numMisses)
	}
	text := strings.Join(strs, "")
	return text
}

var enumeratorPattern string = "^[a-zA-Z](\\)|\\.)|^[\\d]+(\\)|\\.)|^\\([a-zA-Z]\\)|^\\([\\d]+\\)"

func (rr rectRuling) checkWidth(lo, hi float64) (float64, bool) {
	w := hi - lo
	ok := w <= alignTolerance
	return w, ok
}

func overlapsHorizontally(bag *wordBag, word *textWord, gap float64) bool {
	return word.Llx < bag.Urx+gap && bag.Llx-gap < word.Urx
}

func minLlx(words []gridWord) float64 {
	minX := words[0].llx
	for _, word := range words[1:] {
		if word.llx < minX {
			minX = word.llx
		}
	}
	return minX
}

// String returns a string describing the current state of the textState stack.
func (ss *stateStack) String() string {
	lines := []string{fmt.Sprintf("---- font stack: %d", len(*ss))}
	for i, st := range *ss {
		str := "<nil>"
		if st != nil {
			str = st.String()
		}
		lines = append(lines, fmt.Sprintf("\t%2d: %s", i, str))
	}
	return strings.Join(lines, "\n")
}

func (to *textObject) getFillColor() color.Color {
	return colorToRGB(to.gs.ColorspaceNonStroking, to.gs.ColorNonStroking)
}

func nextLineDepth(line *textLine, lines []*textLine) float64 {
	var next float64 = -1
	for _, l := range lines {
		if l.depth > line.depth {
			if math.Round(l.Llx) >= math.Round(line.Llx) {
				next = l.depth
			} else {
				break
			}
		}
	}
	return next
}

var listBulletRegexp *regexp.Regexp = regexp.MustCompile(romanNumeralPattern + "|" + enumeratorPattern)

func combiningFor(s string) (string, bool) {
	runes := []rune(s)
	if len(runes) != 1 {
		return "", false
	}
	comb, ok := accentToCombining[runes[0]]
	return comb, ok
}

const (
	colorKindOther byte = iota
	colorKindGray
	colorKindRGB
	colorKindCMYK
	colorKindPattern
)

type textPara struct {
	model.PdfRectangle
	eBBox   model.PdfRectangle
	lines   []*textLine
	table   *textTable
	isTaken bool
	isEmpty bool
	left    *textPara
	right   *textPara
	above   *textPara
	below   *textPara
	lists   []list
}

func softMaskToImage(img *model.Image, maskColor color.Color) image.Image {
	width, height := int(img.Width), int(img.Height)
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < height; i++ {
		for j := 0; j < width; j++ {
			colorAt, err := img.ColorAt(j, i)
			if err != nil {
				common.Log.Debug("WARN: could not retrieve image mask value at (%d, %d). Output may be incorrect.", j, i)
				continue
			}
			r, g, b, _ := colorAt.RGBA()
			var fill color.Color
			if r+g+b == 0 {
				fill = color.Transparent
			} else {
				fill = maskColor
			}
			out.Set(j, i, fill)
		}
	}
	return out
}

type pathSection struct {
	subpaths []*subpath
	color.Color
}

func compareDepth(a, b bounded) float64 { return bboxDepth(a) - bboxDepth(b) }
func (wb *wordBag) arrangeText(noBreaks bool) *textPara {
	wb.sort()
	if removeDuplicateWords {
		wb.removeDuplicates()
	}
	wb.beginDeferredRemoval()
	defer wb.endDeferredRemoval()
	var lines []*textLine
	for _, binIdx := range wb.depthIndexes() {
		for !wb.empty(binIdx) {
			firstReadingIndex := wb.firstReadingIndex(binIdx)
			seed := wb.firstWord(firstReadingIndex)
			line := newLineFromBag(wb, firstReadingIndex)
			fontSize := seed.fontSize
			if fontSize < minFontSize {
				fontSize = minFontSize
			}
			depthLo := seed.depth - lineMergeFactor*fontSize
			depthHi := seed.depth + lineMergeFactor*fontSize
			maxGap := lineHeightMax * fontSize
			minOverlap := lineHeightMin * fontSize
		scanBands:
			for {
				var next *textWord
				nextIdx := 0
				for _, bi := range wb.depthBand(depthLo, depthHi) {
					cand := wb.highestWord(bi, depthLo, depthHi)
					if cand == nil {
						continue
					}
					gap := horizontalGap(cand, line.words[len(line.words)-1])
					if gap < -minOverlap {
						break scanBands
					}
					if !noBreaks && gap > maxGap {
						continue
					}
					if next != nil && compareLlx(cand, next) >= 0 {
						continue
					}
					next = cand
					nextIdx = bi
				}
				if next == nil {
					break
				}
				line.pullWord(wb, next, nextIdx)
			}
			line.markWordBoundaries()
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return nil
	}
	sort.Slice(lines, func(i, j int) bool { return compareDepthThenLlx(lines[i], lines[j]) < 0 })
	para := newTextPara(wb.PdfRectangle, lines)
	if debugWordBags {
		common.Log.Info("arrangeText !!! para=%s", para.String())
		if debugWordBagsPage {
			for i, line := range para.lines {
				fmt.Printf("%4d: %s\n", i, line.String())
				if debugWordBagsArrange {
					for j, word := range line.words {
						fmt.Printf("%8d: %s\n", j, word.String())
						for k, mark := range word.marks {
							fmt.Printf("%12d: %s\n", k, mark.String())
						}
					}
				}
			}
		}
	}
	return para
}
func (wb *wordBag) minDepth() float64 { return wb.pageHeight - (wb.Ury - wb.maxFontSize) }
func alignToRuneStart(s string, i int) int {
	for i > 0 && i < len(s) && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

func yOverlap(a, b model.PdfRectangle) bool {
	return a.Lly <= b.Ury && b.Lly <= a.Ury
}

func (pl paraList) xNeighbours(tol float64) map[*textPara][]int {
	evs := make([]event, 2*len(pl))
	if tol == 0 {
		for i, para := range pl {
			evs[2*i] = event{para.Llx, true, i}
			evs[2*i+1] = event{para.Urx, false, i}
		}
	} else {
		for i, para := range pl {
			evs[2*i] = event{para.Llx - tol*para.fontsize(), true, i}
			evs[2*i+1] = event{para.Urx + tol*para.fontsize(), false, i}
		}
	}
	return pl.eventNeighbours(evs)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (to *textObject) setTextRenderMode(mode int) {
	if to == nil {
		return
	}
	to.state.tmode = RenderMode(mode)
}

func (to *textObject) reset() {
	to.tm = transform.IdentityMatrix()
	to.tlm = transform.IdentityMatrix()
	to.marks = nil
}

func newTextWord(marks []*textMark, pageRect model.PdfRectangle) *textWord {
	bbox := marks[0].PdfRectangle
	fontSize := marks[0].fontSize
	for _, mark := range marks[1:] {
		bbox = unionRects(bbox, mark.PdfRectangle)
		if mark.fontSize > fontSize {
			fontSize = mark.fontSize
		}
	}
	return &textWord{PdfRectangle: bbox, marks: marks, depth: pageRect.Ury - bbox.Lly, fontSize: fontSize}
}

// BidiText represents a bidi text organized in its visual order
// with base direction of the text.
type BidiText struct {
	text      string
	direction string
}

func (tl *textLine) pullWord(bag *wordBag, word *textWord, idx int) {
	tl.appendWord(word)
	bag.removeWord(word, idx)
}

func newList(lines []*textLine, kind string, children []*list) *list {
	return &list{lines: lines, kind: kind, children: children}
}

type (
	paraList    []*textPara
	patternWalk struct {
		stream *core.PdfObjectStream
		ctm    transform.Matrix
	}
)

func compareLlx(a, b bounded) float64 { return a.bbox().Llx - b.bbox().Llx }
func (to *textObject) showTextAdjusted(arr *core.PdfObjectArray, mcid int, actualText string) error {
	vertical := false
	rendered := false
	for _, obj := range arr.Elements() {
		switch obj.(type) {
		case *core.PdfObjectFloat, *core.PdfObjectInteger:
			num, err := core.GetNumberAsFloat(obj)
			if err != nil {
				common.Log.Debug("ERROR: showTextAdjusted. Bad numerical arg. o=%s args=%+v", obj, arr)
				return err
			}
			tx, ty := -num*0.001*to.state.tfs*to.state.th/100.0, 0.0
			if vertical {
				ty, tx = tx, ty
			}
			trans := translateBy(transform.Point{X: tx, Y: ty})
			to.tm.Concat(trans)
		case *core.PdfObjectString:
			direct := core.TraceToDirectObject(obj)
			raw, ok := core.GetStringBytes(direct)
			if !ok {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("showTextAdjusted: Bad string arg. o=%s args=%+v", obj, arr)
				}
				return core.ErrTypeError
			}
			if rendered {
				to.advanceTextMatrix(raw)
			} else {
				to.renderText(direct, raw, mcid, actualText)
				if actualText != "" {
					rendered = true
				}
			}
		default:
			common.Log.Debug("ERROR: showTextAdjusted. Unexpected type (%T) args=%+v", obj, arr)
			return core.ErrTypeError
		}
	}
	return nil
}
func (lr lineRuling) yMean() float64 { return 0.5 * (lr.p0.Y + lr.p1.Y) }

// FormContent holds a form XObject painted on the page together with its
// parsed content stream operations. The DirectObject fields of the text marks
// that originate from a form reference PdfObjects inside Operations, so
// consumers that rewrite content streams (e.g. the redactor) must edit
// Operations and re-encode Form rather than the page content stream.
// Marks merged from annotation appearance streams (extraction with
// Options.IncludeAnnotations) are not covered: the objects they reference
// belong to appearance stream parses that are not reported here.
type FormContent struct {
	// Name is the resource name the form was first painted under.
	Name string

	// Form is the form XObject. Its underlying stream primitive is shared
	// with every other page or form that references the same XObject.
	Form *model.XObjectForm

	// Operations is the parse of the form's content stream that the merged
	// text marks reference.
	Operations *contentstream.ContentStreamOperations
}

func (pl paraList) log(s string) {
	if !debugTopoOrder {
		return
	}
	common.Log.Info("%8s: %d paras =======-------=======", s, len(pl))
	for i, para := range pl {
		if para == nil {
			continue
		}
		text := para.text()
		s2 := "  "
		if para.table != nil {
			s2 = fmt.Sprintf("[%dx%d]", para.table.w, para.table.h)
		}
		fmt.Printf("%4d: %6.2f %s %q\n", i, para.PdfRectangle, s2, truncate(text, 50))
	}
}

// Font represents the font properties on a PDF page.
type Font struct {
	PdfFont *model.PdfFont

	// FontName represents Font Name from font properties.
	FontName string

	// FontType represents Font Subtype entry in the font dictionary inside page resources.
	// Examples : type0, Type1, MMType1, Type3, TrueType, CIDFont.
	FontType string

	// ToUnicode is true if font provides a `ToUnicode` mapping.
	ToUnicode bool

	// IsCID is true if underlying font is a composite font.
	// Composite font is represented by a font dictionary whose Subtype is `Type0`
	IsCID bool

	// IsSimple is true if font is simple font.
	// A simple font is limited to only 8 bit (255) character codes.
	IsSimple bool

	// FontData represents the raw data of the embedded font file.
	// It can have format TrueType (TTF), PostScript Font (PFB) or Compact Font Format (CCF).
	// FontData value can be indicates from `FontFile`, `FontFile2` or `FontFile3` inside Font Descriptor.
	// At most, only one of `FontFile`, `FontFile2` or `FontFile3` will be FontData value.
	FontData []byte

	// FontFileName is a name representing the font. it has format:
	// (Font Name) + (Font Type Extension), example: helvetica.ttf.
	FontFileName string

	// FontDescriptor represents metrics and other attributes inside font properties from PDF Structure (Font Descriptor).
	FontDescriptor *model.PdfFontDescriptor
}

// List returns all the list objects detected on the page.
// It detects all the bullet point Lists from a given pdf page and builds a slice of bullet list objects.
// A given bullet list object has a tree structure.
// Each bullet point list is extracted with the text content it contains and all the sub lists found under it as children in the tree.
// The rest content of the pdf is ignored and only text in the bullet point lists are extracted.
// The list extraction is done in two ways.
// 1. If the document is tagged then the lists are extracted using the tags provided in the document.
// 2. Otherwise the bullet lists are extracted from the raw text using regex matching.
// By default the document tag is used if available.
// However this can be disabled using `DisableDocumentTags` in the `Options` object.
// Sometimes disabling document tags option might give a better bullet list extraction if the document was tagged incorrectly.
//
//	    options := &Options{
//		     DisableDocumentTags: false, // this means use document tag if available
//	    }
//	    ex, err := NewWithOptions(page, options)
//	    // handle error
//	    pageText, _, _, err := ex.ExtractPageText()
//	    // handle error
//	    lists := pageText.List()
//	    txt := lists.Text()
func (pt PageText) List() lists {
	useTags := !pt.options.disableDocumentTags
	paras := pt.getParagraphs()
	items := paras.list()
	if pt.structTreeRoot != nil && useTags {
		byMCID := groupLinesByMCID(&paras)
		if len(pt.structTreeRoot.K) == 0 {
			common.Log.Debug("List: structTreeRoot doesn't have any content, using text matching method instead.")
			return items
		}
		items = buildList(pt.structTreeRoot, byMCID, pt.pageObject)
	}
	return items
}

func sortedTileKeys(m map[float64]gridTile) []float64 {
	keys := make([]float64, 0, len(m))
	for x := range m {
		keys = append(keys, x)
	}
	sort.Float64s(keys)
	return keys
}

func spacesForGap(gap, charW float64) string {
	if gap <= 0 {
		return " "
	}
	n := min(max(int(gap/charW), 1), maxGapSpaces)
	return strings.Repeat(" ", n)
}

func sortedIntSetKeys(m map[int]intSet) []int {
	keys := make([]int, 0, len(m))
	for i := range m {
		keys = append(keys, i)
	}
	sort.Ints(keys)
	return keys
}

func (r *ruling) gridIntersecting(other *ruling) bool {
	return approxEqual(r.lo, other.lo) && approxEqual(r.hi, other.hi)
}

// TextMarkArray is a collection of TextMarks.
type TextMarkArray struct{ marks []TextMark }

// TextMark represents extracted text on a page with information regarding both textual content,
// formatting (font and size) and positioning.
// It is the smallest unit of text on a PDF page, typically a single character.
//
// getBBox() in test_text.go shows how to compute bounding boxes of substrings of extracted text.
// The following code extracts the text on PDF page `page` into `text` then finds the bounding box
// `bbox` of substring `term` in `text`.
//
//	ex, _ := New(page)
//	// handle errors
//	pageText, _, _, err := ex.ExtractPageText()
//	// handle errors
//	text := pageText.Text()
//	textMarks := pageText.Marks()
//
//		start := strings.Index(text, term)
//	 end := start + len(term)
//	 spanMarks, err := textMarks.RangeOffset(start, end)
//	 // handle errors
//	 bbox, ok := spanMarks.BBox()
//	 // handle errors
type TextMark struct {
	// Text is the extracted text.
	Text string

	// Original is the text in the PDF. It has not been decoded like `Text`.
	Original string

	// BBox is the bounding box of the text.
	BBox model.PdfRectangle

	// Font is the font the text was drawn with.
	Font *model.PdfFont

	// FontSize is the font size the text was drawn with.
	FontSize float64

	// Offset is the offset of the start of TextMark.Text in the extracted text. If you do this
	//   text, textMarks := pageText.Text(), pageText.Marks()
	//   marks := textMarks.Elements()
	// then marks[i].Offset is the offset of marks[i].Text in text.
	Offset int

	// Meta is set true for spaces and line breaks that we insert in the extracted text. We insert
	// spaces (line breaks) when we see characters that are over a threshold horizontal (vertical)
	//  distance  apart. See wordJoiner (lineJoiner) in PageText.computeViews().
	Meta bool

	// FillColor is the fill color of the text.
	// The color is nil for spaces and line breaks (i.e. the Meta field is true).
	FillColor color.Color

	// StrokeColor is the stroke color of the text.
	// The color is nil for spaces and line breaks (i.e. the Meta field is true).
	StrokeColor color.Color

	// Orientation is the text orientation
	Orientation int

	// DirectObject is the underlying PdfObject (Text Object) that represents the visible texts. This is introduced to get
	// a simple access to the TextObject in case editing or replacment of some text is needed. E.g during redaction.
	DirectObject core.PdfObject

	// ObjString is a decoded string operand of a text-showing operator. It has the same value as `Text` attribute except
	// when many glyphs are represented with the same Text Object that contains multiple length string operand in which case
	// ObjString spans more than one character string that falls in different TextMark objects.
	ObjString []string
	Tw        float64
	Th        float64
	Tc        float64

	// Tfs is the font size set by the Tf operator, in unscaled text space units. Unlike
	// FontSize it is not affected by the CTM or the text matrix.
	Tfs      float64
	Index    int
	hasTable bool
	table    *TextTable
}

func (gt gridTiling) complete() bool {
	for _, tile := range gt.tiles {
		for _, tile2 := range tile {
			if !tile2.complete() {
				return false
			}
		}
	}
	return true
}

type rulingKind int

// ExtractPageText returns the text contents of `e` (an Extractor for a page) as a PageText.
// TODO(peterwilliams97): The stats complicate this function signature and aren't very useful.
//
//	Replace with a function like Extract() (*PageText, error)
func (e *Extractor) ExtractPageText() (*PageText, int, int, error) {
	pageText, numChars, numMisparsed, err := e.extractPageText(e.contents, e.resources, transform.IdentityMatrix(), 0, false, nil)
	if err != nil && !errors.Is(err, model.ErrColorOutOfRange) {
		return nil, 0, 0, err
	}
	if e.options != nil {
		pageText.options.mode = e.options.ExtractionMode
		pageText.options.disableDocumentTags = e.options.DisableDocumentTags
		pageText.options.disableDehyphenation = e.options.DisableDehyphenation
	}
	pageText.computeViews()
	err = checkLicense(pageText)
	if err != nil {
		return nil, 0, 0, err
	}
	if e.options != nil {
		if e.options.ApplyCropBox && e.cropBox != nil {
			pageText.ApplyArea(*e.cropBox)
		}
	}
	return pageText, numChars, numMisparsed, nil
}

func newTwoByTwoTable(tl, tr, bl, br *textPara) *textTable {
	table := &textTable{w: 2, h: 2, cells: make(map[uint64]*textPara, 4)}
	table.put(0, 0, tl)
	table.put(1, 0, tr)
	table.put(0, 1, bl)
	table.put(1, 1, br)
	return table
}

type imageExtractContext struct {
	images          []ImageMark
	inlineCount     int
	xObjectCount    int
	formCount       int
	xObjectImages   map[*core.PdfObjectStream]*model.Image
	inlineImages    map[inlineImageKey]*model.Image
	parsedStreams   map[*core.PdfObjectStream]*parsedImageStream
	visitedPatterns map[patternWalk]struct{}
	patternStack    []patternWalk
	ctm             transform.Matrix
	options         *ImageExtractOptions
	walker          streamlimits.Walker
}

func (rl rulingList) vertsHorzs() (rulingList, rulingList) {
	var verts, horzs rulingList
	for _, r := range rl {
		switch r.kind {
		case rulingVert:
			verts = append(verts, r)
		case rulingHorz:
			horzs = append(horzs, r)
		}
	}
	return verts, horzs
}

type textContext struct{ disableDehyphenation bool }

func (wb *wordBag) removeWord(word *textWord, idx int) {
	if wb.removed != nil {
		if _, ok := wb.removed[word]; !ok {
			wb.removed[word] = struct{}{}
			wb.removedCount[idx]++
		}
		return
	}
	bin := wb.bins[idx]
	bin = removeWordFromSlice(bin, word)
	if len(bin) == 0 {
		delete(wb.bins, idx)
	} else {
		wb.bins[idx] = bin
	}
}

func unpoolImage(img model.Image) *model.Image {
	img.Data = core.UnpooledBytes(img.Data)
	img.SetAlpha(core.UnpooledBytes(img.AlphaData()))
	return &img
}

func (tl *textLine) endsInHyphen() bool {
	word := tl.words[len(tl.words)-1]
	text := word.text
	last, size := utf8.DecodeLastRuneInString(text)
	if size <= 0 || !unicode.Is(unicode.Hyphen, last) {
		return false
	}
	if word.spaceBefore && endsWithHyphen(text) {
		return true
	}
	return endsWithHyphen(tl.text())
}

func bottomEdgeRuling(rect model.PdfRectangle) *ruling {
	return &ruling{kind: rulingHorz, xy: rect.Lly, lo: rect.Llx, hi: rect.Urx}
}

func (rl rulingList) removeDuplicates() rulingList {
	if len(rl) == 0 {
		return nil
	}
	rl.sort()
	out := rulingList{rl[0]}
	for _, r := range rl[1:] {
		if r.equals(out[len(out)-1]) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func (tt *textTable) log(msg string) {
	if !debugTables {
		return
	}
	common.Log.Info("~~~ %s: %d x %d grid=%t\n      %6.2f", msg, tt.w, tt.h, tt.exportable, tt.PdfRectangle)
	for i := 0; i < tt.h; i++ {
		for j := 0; j < tt.w; j++ {
			para := tt.get(j, i)
			if para == nil {
				continue
			}
			fmt.Printf("%4d %2d: %6.2f %q %d\n", j, i, para.PdfRectangle, truncate(para.text(), 50), utf8.RuneCountInString(para.text()))
		}
	}
}

func (tt *textTable) compositeColCorridors() map[int][]float64 {
	out := make(map[int][]float64, tt.w)
	if debugTables {
		common.Log.Info("compositeColCorridors: w=%d ", tt.w)
	}
	for i := 0; i < tt.w; i++ {
		out[i] = nil
	}
	return out
}

func (rl rulingList) bbox() model.PdfRectangle {
	var rect model.PdfRectangle
	if len(rl) == 0 {
		common.Log.Error("rulingList.bbox: no rulings")
		return model.PdfRectangle{}
	}
	if rl[0].kind == rulingHorz {
		rect.Llx, rect.Urx = rl.secMinMax()
		rect.Lly, rect.Ury = rl.primMinMax()
	} else {
		rect.Llx, rect.Urx = rl.primMinMax()
		rect.Lly, rect.Ury = rl.secMinMax()
	}
	return rect
}

func dedupeGridWords(words []gridWord) []gridWord {
	if len(words) < 2 {
		return words
	}
	byY := make(map[int][]int)
	for i, w := range words {
		key := int(w.ury * 10)
		byY[key] = append(byY[key], i)
	}
	dup := make([]bool, len(words))
	for i, w := range words {
		if dup[i] {
			continue
		}
		runes := utf8.RuneCountInString(w.text)
		if runes == 0 || runes > maxDedupeRunes {
			continue
		}
		yKey := int(w.ury * 10)
		span := int(dupPositionTolerance*10) + 1
		for d := -span; d <= span; d++ {
			cands := byY[yKey+d]
			for _, j := range cands {
				if i == j || dup[j] {
					continue
				}
				other := words[j]
				if utf8.RuneCountInString(other.text) <= runes {
					continue
				}
				if strings.Contains(other.text, w.text) && math.Abs(w.ury-other.ury) <= dupPositionTolerance && w.llx >= other.llx-dupPositionTolerance && w.urx <= other.urx+dupPositionTolerance {
					dup[i] = true
					break
				}
			}
			if dup[i] {
				break
			}
		}
	}
	anyDup := false
	for _, f := range dup {
		if f {
			anyDup = true
			break
		}
	}
	if !anyDup {
		return words
	}
	out := make([]gridWord, 0, len(words))
	for i, w := range words {
		if !dup[i] {
			out = append(out, w)
		}
	}
	return out
}

type formProcessingError struct {
	name string
	err  error
}

// Match defines the structure for each match, including pattern, indexes, and locations.
type Match struct {
	Pattern   string
	Indexes   [][]int
	Locations []Box
}

// PageTextOptions holds various options available in extraction process.
type PageTextOptions struct {
	disableDocumentTags  bool
	mode                 ExtractionMode
	disableDehyphenation bool
}

func isDuplicateGridWord(words []gridWord, word *textWord) bool {
	if utf8.RuneCountInString(word.text) < minDupRunes {
		return false
	}
	from := max(len(words)-dupLookback, 0)
	for i := from; i < len(words); i++ {
		prev := words[i]
		if prev.text == word.text && math.Abs(prev.llx-word.Llx) <= dupPositionTolerance && math.Abs(prev.ury-word.Ury) <= dupPositionTolerance {
			return true
		}
	}
	return false
}

func (rl rulingList) snapToGroupsDirection() rulingList {
	rl.sortStrict()
	groups := make(map[*ruling]rulingList, len(rl))
	cur := rl[0]
	startGroup := func(r *ruling) { cur = r; groups[cur] = rulingList{r} }
	startGroup(rl[0])
	for _, r := range rl[1:] {
		if r.xy < cur.xy-zeroTolerance {
			common.Log.Error("snapToGroupsDirection: Wrong primary order.\n\tv0=%s\n\t v=%s", cur, r)
		}
		if r.xy > cur.xy+alignTolerance {
			startGroup(r)
		} else {
			groups[cur] = append(groups[cur], r)
		}
	}
	pos := make(map[*ruling]float64, len(groups))
	leader := make(map[*ruling]*ruling, len(rl))
	for lead, members := range groups {
		pos[lead] = members.mergePrimary()
		for _, m := range members {
			leader[m] = lead
		}
	}
	for _, r := range rl {
		r.xy = pos[leader[r]]
	}
	out := make(rulingList, 0, len(rl))
	for _, members := range groups {
		parts := members.splitSec()
		for i, part := range parts {
			merged := part.merge()
			if len(out) > 0 {
				last := out[len(out)-1]
				if last.alignsPrimary(merged) && last.alignsSec(merged) {
					common.Log.Error("snapToGroupsDirection: Duplicate i=%d\n\tw=%s\n\tv=%s", i, last, merged)
					continue
				}
			}
			out = append(out, merged)
		}
	}
	out.sortStrict()
	return out
}

func rulingKindOfLine(p0, p1 transform.Point) rulingKind {
	dx := math.Abs(p0.X - p1.X)
	dy := math.Abs(p0.Y - p1.Y)
	return kindFromDeltas(dx, dy, minRulingLength)
}

func (gt gridTile) contains(rect model.PdfRectangle) bool {
	if gt.numBorders() < 3 {
		return false
	}
	if gt.llx && rect.Llx < gt.Llx-containTolerance {
		return false
	}
	if gt.urx && rect.Urx > gt.Urx+containTolerance {
		return false
	}
	if gt.lly && rect.Lly < gt.Lly-containTolerance {
		return false
	}
	if gt.ury && rect.Ury > gt.Ury+containTolerance {
		return false
	}
	return true
}

func (rl rulingList) aligned() bool {
	if len(rl) < 2 {
		return false
	}
	counts := make(map[*ruling]int)
	counts[rl[0]] = 0
	for _, r := range rl[1:] {
		matched := false
		for rep := range counts {
			if r.gridIntersecting(rep) {
				counts[rep]++
				matched = true
				break
			}
		}
		if !matched {
			counts[r] = 0
		}
	}
	numUnmatched := 0
	for _, cnt := range counts {
		if cnt == 0 {
			numUnmatched++
		}
	}
	frac := float64(numUnmatched) / float64(len(rl))
	ok := frac <= 1.0-alignedFraction
	if debugRulings {
		common.Log.Info("aligned=%t unmatched=%.2f=%d/%d vecs=%s", ok, frac, numUnmatched, len(rl), rl.String())
	}
	return ok
}

func (ss *shapesState) devicePoint(x, y float64) transform.Point {
	m := ss.parentCTM.Mult(ss.ctm)
	x, y = m.Transform(x, y)
	return transform.NewPoint(x, y)
}

func (tw *textWord) computeText() string {
	texts := make([]string, len(tw.marks))
	for i, mark := range tw.marks {
		texts[i] = mark.text
	}
	return strings.Join(texts, "")
}

func (tt *textTable) growTable() {
	addRow := func(row paraList) {
		tt.h++
		for i := 0; i < tt.w; i++ {
			row2 := row[i]
			tt.put(i, tt.h-1, row2)
		}
	}
	addCol := func(col paraList) {
		tt.w++
		for i := 0; i < tt.h; i++ {
			col2 := col[i]
			tt.put(tt.w-1, i, col2)
		}
	}
	if debugGrowTable {
		tt.log("growTable")
	}
	for i := 0; ; i++ {
		grew := false
		down := tt.getDown()
		right := tt.getRight()
		if debugGrowTable {
			fmt.Printf("%4d: %s\n", i, tt)
			fmt.Printf("        down=%s\n", down)
			fmt.Printf("       right=%s\n", right)
		}
		if down != nil && right != nil {
			corner := down[len(down)-1]
			if !corner.taken() && corner == right[len(right)-1] {
				addRow(down)
				if right = tt.getRight(); right != nil {
					addCol(right)
					tt.put(tt.w-1, tt.h-1, corner)
				}
				grew = true
			}
		}
		if !grew && down != nil {
			addRow(down)
			grew = true
		}
		if !grew && right != nil {
			addCol(right)
			grew = true
		}
		if !grew {
			break
		}
	}
}

func (ss *shapesState) moveTo(x, y float64) {
	ss.fresh = true
	ss.currentPoint = ss.devicePoint(x, y)
	if debugShapes {
		common.Log.Info("moveTo: %.2f,%.2f device=%.2f", x, y, ss.currentPoint)
	}
}

func marksToString(marks *TextMarkArray) string {
	var out strings.Builder
	for _, mark := range marks.Elements() {
		out.WriteString(mark.Text)
	}
	return out.String()
}

func (rl rulingList) merge() *ruling {
	sumXY := rl[0].xy
	lo := rl[0].lo
	hi := rl[0].hi
	for _, r := range rl[1:] {
		sumXY += r.xy
		if r.lo < lo {
			lo = r.lo
		}
		if r.hi > hi {
			hi = r.hi
		}
	}
	merged := &ruling{kind: rl[0].kind, mark: rl[0].mark, Color: rl[0].Color, xy: sumXY / float64(len(rl)), lo: lo, hi: hi}
	if debugMergeRulings {
		common.Log.Info("merge: %2d vecs %s", len(rl), merged)
		for i, r := range rl {
			fmt.Printf("%4d: %s\n", i, r)
		}
	}
	return merged
}

func (pl paraList) findTables(tilings []gridTiling) []*textTable {
	pl.addNeighbours()
	sort.Slice(pl, func(n, n2 int) bool { return compareLlxThenDepth(pl[n], pl[n2]) < 0 })
	var tables []*textTable
	if useGridTables {
		gridTables := pl.findGridTables(tilings)
		tables = append(tables, gridTables...)
	}
	if useTextTables {
		textTables := pl.findTextTables()
		tables = append(tables, textTables...)
	}
	return tables
}

// String returns a description of `k`.
func (mk markKind) String() string {
	name, ok := markKindNames[mk]
	if !ok {
		return fmt.Sprintf("Not a mark: %d", mk)
	}
	return name
}

// String returns a description of `w`.
func (tw *textWord) String() string {
	return fmt.Sprintf("%.2f %6.2f fontsize=%.2f \"%s\"", tw.depth, tw.PdfRectangle, tw.fontSize, tw.text)
}

func reverseStrings(strs []string, from int, to int) {
	for i, j := from, to-1; i < j; i, j = i+1, j-1 {
		tmp := strs[i]
		strs[i] = strs[j]
		strs[j] = tmp
	}
}

func (tl *textLine) text() string {
	var parts []string
	for _, word := range tl.words {
		if word.spaceBefore {
			parts = append(parts, " ")
		}
		parts = append(parts, word.text)
	}
	text := strings.Join(parts, "")
	bidi := reorderBidi([]rune(text))
	return bidi.text
}

func groupGridRows(words []gridWord, charWidth float64) []gridRow {
	rows := groupWordsIntoRows(words)
	sort.Slice(rows, func(i, j int) bool { return rows[i].y > rows[j].y })
	var merged []gridRow
	for _, row := range rows {
		tol := charWidth * rowToleranceFactor
		if len(row.words) > 0 {
			fontTol := row.words[0].fontSize
			if fontTol*rowToleranceFactor > tol {
				tol = fontTol * rowToleranceFactor
			}
		}
		placed := false
		for i := len(merged) - 1; i >= 0 && i >= len(merged)-rowSearchDepth; i-- {
			if math.Abs(merged[i].y-row.y) <= tol {
				merged[i].words = append(merged[i].words, row.words...)
				total := float64(len(merged[i].words))
				added := float64(len(row.words))
				merged[i].y = merged[i].y*(total-added)/total + row.y*added/total
				placed = true
				break
			}
		}
		if !placed {
			merged = append(merged, gridRow{words: append([]gridWord{}, row.words...), y: row.y})
		}
	}
	merged = refineGridRows(merged, charWidth)
	sort.Slice(merged, func(i, j int) bool { return merged[i].y > merged[j].y })
	return merged
}
func (pt PageText) marksText() string { return pt.cachedMarksText }
func bulletContinuationLines(line *textLine, lines []*textLine, startDepth, endDepth float64) []*textLine {
	out := []*textLine{}
	for _, l := range lines {
		if l.depth >= startDepth {
			if endDepth != -1 && l.depth < endDepth {
				if l.text() != line.text() {
					if math.Round(l.Llx) < math.Round(line.Llx) {
						break
					}
					out = append(out, l)
				}
			} else if endDepth == -1 {
				if l.depth == line.depth {
					if l.text() != line.text() {
						out = append(out, l)
					}
					continue
				}
				nextDepth := nextLineDepth(line, lines)
				if nextDepth != -1 && l.depth <= nextDepth {
					out = append(out, l)
				}
			}
		}
	}
	return out
}

func (to *textObject) getCurrentFont() *model.PdfFont {
	tfont := to.state.tfont
	if tfont == nil {
		common.Log.Debug("ERROR: No font defined. Using default.")
		return model.DefaultFont()
	}
	return tfont
}

var accentToCombining = map[rune]string{0x0060: "̀", 0x02CB: "̀", 0x0027: "́", 0x00B4: "́", 0x02B9: "́", 0x02CA: "́", 0x005E: "̂", 0x02C6: "̂", 0x007E: "̃", 0x02DC: "̃", 0x00AF: "̄", 0x02C9: "̄", 0x02D8: "̆", 0x02D9: "̇", 0x00A8: "̈", 0x00B0: "̊", 0x02DA: "̊", 0x02BA: "̋", 0x02DD: "̋", 0x02C7: "̌", 0x02C8: "̍", 0x0022: "̎", 0x02BB: "̒", 0x02BC: "̓", 0x0486: "̓", 0x055A: "̓", 0x02BD: "̔", 0x0485: "̔", 0x0559: "̔", 0x02D4: "̝", 0x02D5: "̞", 0x02D6: "̟", 0x02D7: "̠", 0x02B2: "̡", 0x00B8: "̧", 0x02CC: "̩", 0x02B7: "̫", 0x02CD: "̱", 0x005F: "̲", 0x204E: "͙"}

type ruling struct {
	kind rulingKind
	mark markKind
	color.Color
	xy    float64
	lo    float64
	hi    float64
	width float64
}

func (pl paraList) sortReadingOrder() {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("sortReadingOrder: paras=%d ===========x=============", len(pl))
	}
	if len(pl) <= 1 {
		return
	}
	pl.computeEBBoxes()
	sort.Slice(pl, func(i, j int) bool { return compareDepthThenLlx(pl[i], pl[j]) <= 0 })
}

var markKindNames = map[markKind]string{markKindStroke: "stroke", markKindFill: "fill", markKindAugment: "augment"}

func pointsEqual(a, b transform.Point) bool { return a.X == b.X && a.Y == b.Y }

// ApplyArea processes the page text only within the specified area `bbox`.
// Each time ApplyArea is called, it updates the result set in `pt`.
// Can be called multiple times in a row with different bounding boxes.
func (pt *PageText) ApplyArea(bbox model.PdfRectangle) {
	inArea := make([]*textMark, 0, len(pt.marks))
	for _, mark := range pt.marks {
		if rectsOverlap(mark.bbox(), bbox) {
			inArea = append(inArea, mark)
		}
	}
	if pt.options.mode == ExtractionModeGrid {
		pt.text = pt.makeGridText(inArea)
		return
	}
	var paras paraList
	var plain strings.Builder
	numInArea := len(inArea)
	for angle := 0; angle < 360 && numInArea > 0; angle += 90 {
		angleMarks := make([]*textMark, 0, len(inArea)-numInArea)
		for _, inArea2 := range inArea {
			if inArea2.orientation == angle {
				angleMarks = append(angleMarks, inArea2)
			}
		}
		if len(angleMarks) > 0 {
			if pt.options.mode == ExtractionModePlain {
				plain.WriteString(makeSimpleText(angleMarks, pt.pageSize))
			} else {
				angleParas := makeTextPage(angleMarks, pt.pageSize, nil, nil, pt.options.mode == ExtractionModeLayoutNoBreaks)
				paras = append(paras, angleParas...)
			}
			numInArea -= len(angleMarks)
		}
	}
	if pt.options.mode == ExtractionModePlain {
		pt.text = plain.String()
	} else {
		buf := new(bytes.Buffer)
		ctx := &textContext{disableDehyphenation: pt.options.disableDehyphenation}
		paras.writeText(buf, ctx)
		pt.text = buf.String()
		pt.cachedMarksText = pt.text
		pt.viewMarks = paras.toTextMarks(ctx)
		pt.tables = paras.tables(ctx)
	}
}

func (to *textObject) moveLP(tx, ty float64) {
	to.tlm.Concat(transform.NewMatrix(1, 0, 0, 1, tx, ty))
	to.tm = to.tlm
}

func (cc compositeCell) hasLines(lines []*textLine) bool {
	for i, line := range lines {
		overlaps := rectsOverlap(cc.PdfRectangle, line.PdfRectangle)
		if debugTables {
			fmt.Printf("      ^^^intersects=%t %d of %d\n", overlaps, i, len(lines))
			fmt.Printf("      ^^^composite=%s\n", cc)
			fmt.Printf("       line=%s\n", line)
		}
		if overlaps {
			return true
		}
	}
	return false
}

const glyphSpaceUnit = 1.0 / 1000.0
const (
	RenderModeStroke RenderMode = 1 << iota
	RenderModeFill
	RenderModeClip
)

func (wb *wordBag) scanBand(label string, bag *wordBag, accept func(b *wordBag, w *textWord) bool, lo, hi, maxDiff float64, single, fixed bool) int {
	maxFontSize := bag.maxFontSize
	var removals map[int]map[*textWord]struct{}
	if !single {
		removals = wb.makeRemovals()
	}
	margin := lineMergeFactor * maxFontSize
	numPulled := 0
	for _, bi := range wb.depthBand(lo-margin, hi+margin) {
		if len(wb.bins[bi]) == 0 {
			continue
		}
		for _, w := range wb.bins[bi] {
			if !(lo-margin <= w.depth && w.depth <= hi+margin) {
				continue
			}
			if !accept(bag, w) {
				continue
			}
			relDiff := 2.0 * math.Abs(w.fontSize-bag.maxFontSize) / (w.fontSize + bag.maxFontSize)
			ratio := math.Max(w.fontSize/bag.maxFontSize, bag.maxFontSize/w.fontSize)
			sizeDiff := math.Min(relDiff, ratio)
			if maxDiff > 0 && sizeDiff > maxDiff {
				continue
			}
			if bag.blocked(w) {
				continue
			}
			if !single {
				bag.pullWord(w, bi, removals)
			}
			numPulled++
			if !fixed {
				if w.depth < lo {
					lo = w.depth
				}
				if w.depth > hi {
					hi = w.depth
				}
			}
			if single {
				break
			}
		}
	}
	if !single {
		wb.applyRemovals(removals)
	}
	return numPulled
}

func replaceMultiByteString(marks *TextMarkArray, repl string, offset *int, limit int) error {
	obj := marks.Elements()[0].DirectObject
	pdfStr, ok := core.GetString(obj)
	if !ok {
		return fmt.Errorf("unable to get string Bytes from directObject")
	}
	markText := marksToString(marks)
	raw, ok := core.GetStringBytes(obj)
	if !ok {
		return core.ErrTypeError
	}
	font := marks.Elements()[0].Font
	strText := decodeBytes(raw, font)
	sub := subTextForMark(repl, *offset, len(markText), limit)
	newText := ""
	words := strings.Split(strText, " ")
	lastWord := words[len(words)-1]
	if lastWord == markText && *offset == 0 {
		pos := strings.LastIndex(strText, markText)
		newText = replaceRange(strText, pos, len(markText)+pos, sub)
	} else {
		newText = strings.Replace(strText, markText, sub, 1)
	}
	setStringFromText(pdfStr, newText, font)
	*offset += len(markText)
	return nil
}

func (tw *textWord) appendMark(mark *textMark, pageRect model.PdfRectangle) {
	tw.marks = append(tw.marks, mark)
	tw.PdfRectangle = unionRects(tw.PdfRectangle, mark.PdfRectangle)
	if mark.fontSize > tw.fontSize {
		tw.fontSize = mark.fontSize
	}
	tw.depth = pageRect.Ury - tw.Lly
}

// Tables returns the tables extracted from the page.
func (pt PageText) Tables() []TextTable {
	if debugTables {
		common.Log.Info("Tables: %d", len(pt.tables))
	}
	return pt.tables
}

// ImageMark represents an image drawn on a page and its position in device coordinates.
// All coordinates are in device coordinates.
type ImageMark struct {
	// Image is the image converted to RGB. Each mark has its own Image, but
	// marks of the same image drawn more than once on a page share its Data
	// and alpha. Release on one mark leaves the others intact; changing the
	// data in place, for example with AlphaMap, changes every mark of that
	// image, so copy it first.
	Image *model.Image

	// Dimensions of the image as displayed in the PDF.
	Width  float64
	Height float64

	// Position of the image in PDF coordinates (lower left corner).
	X float64
	Y float64

	// Angle in degrees, if rotated.
	Angle float64
}
type gridTile struct {
	model.PdfRectangle
	ury, llx, lly, urx bool
}

func makeStrokeRulings(sections []pathSection) rulingList {
	granularize(sections)
	if debugRulings {
		common.Log.Info("makeStrokeRulings: %d strokes", len(sections))
	}
	var rulings rulingList
	for _, section := range sections {
		for _, sp := range section.subpaths {
			if len(sp.points) < 2 {
				continue
			}
			p := sp.points[0]
			for _, next := range sp.points[1:] {
				if r, ok := lineToRuling(p, next, section.Color); ok {
					rulings = append(rulings, r)
				}
				p = next
			}
		}
	}
	if debugRulings {
		common.Log.Info("makeStrokeRulings: %s", rulings)
	}
	return rulings
}

func (e *Extractor) extractPageText(contents string, resources *model.PdfPageResources, ctm transform.Matrix, formDepth int, isAnnotation bool, gs *contentstream.GraphicsState) (*PageText, int, int, error) {
	contentStreamParser, err := contentstream.NewContentStreamParser(contents).Parse()
	if err != nil {
		common.Log.Debug("ERROR: extractPageText parse failed. err=%v", err)
		pageText := &PageText{pageSize: e.mediaBox, structTreeRoot: e.structTreeRoot, pageObject: e.pageObject}
		return pageText, 0, 0, err
	}
	return e.extractPageTextFromOps(contentStreamParser, resources, ctm, formDepth, isAnnotation, gs)
}

// Text returns the text content of the `bulletLists`.
func (l *lists) Text() string {
	builder := &strings.Builder{}
	for _, listV := range *l {
		text := listV.Text()
		builder.WriteString(text)
	}
	return builder.String()
}
func paraXOverlaps(para, para2 *textPara) bool { return xOverlap(para.eBBox, para2.eBBox) }

// String returns a string describing `pt`.
func (pt PageText) String() string {
	header := fmt.Sprintf("PageText: %d elements", len(pt.marks))
	parts := []string{"-" + header}
	for _, mark := range pt.marks {
		parts = append(parts, mark.String())
	}
	parts = append(parts, "+"+header)
	return strings.Join(parts, "\n")
}

func (tt *textTable) getDown() paraList {
	paras := make(paraList, tt.w)
	for i := 0; i < tt.w; i++ {
		para := tt.get(i, tt.h-1).below
		if para.taken() {
			return nil
		}
		paras[i] = para
	}
	for i := 0; i < tt.w-1; i++ {
		if paras[i].right != paras[i+1] {
			return nil
		}
	}
	return paras
}

func (wb *wordBag) getDepthIdx(depth float64) int {
	idxs := wb.depthIndexes()
	idx := depthBinIndex(depth)
	if idx < idxs[0] {
		return idxs[0]
	}
	if idx > idxs[len(idxs)-1] {
		return idxs[len(idxs)-1]
	}
	return idx
}

// GetContentStreamOps returns the contentStreamOps field of `pt`.
func (pt *PageText) GetContentStreamOps() *contentstream.ContentStreamOperations {
	return pt.ops
}

func (pl paraList) eventNeighbours(evs []event) map[*textPara][]int {
	sort.Slice(evs, func(i, j int) bool {
		a, b := evs[i], evs[j]
		pa, pb := a.pos, b.pos
		if pa != pb {
			return pa < pb
		}
		if a.isStart != b.isStart {
			return a.isStart
		}
		return i < j
	})
	overlaps := make(map[int]intSet)
	active := make(intSet)
	for _, ev := range evs {
		if ev.isStart {
			overlaps[ev.idx] = make(intSet)
			for k := range active {
				if k != ev.idx {
					overlaps[ev.idx].add(k)
					overlaps[k].add(ev.idx)
				}
			}
			active.add(ev.idx)
		} else {
			active.del(ev.idx)
		}
	}
	out := map[*textPara][]int{}
	for idx, set := range overlaps {
		para := pl[idx]
		if len(set) == 0 {
			out[para] = nil
			continue
		}
		idxs := make([]int, len(set))
		n := 0
		for k := range set {
			idxs[n] = k
			n++
		}
		out[para] = idxs
	}
	return out
}

func (e *Editor) getMatches(pattern string, pages []int) (map[int]Match, map[int][]*TextMarkArray, error) {
	matches := map[int]Match{}
	marksByPage := map[int][]*TextMarkArray{}
	for _, pageNum := range pages {
		page, err := e.reader.GetPage(pageNum)
		if err != nil {
			return nil, nil, err
		}
		ex, err := New(page)
		if err != nil {
			return nil, nil, err
		}
		pageText, _, _, err := ex.ExtractPageText()
		if err != nil {
			return nil, nil, err
		}
		marksText := pageText.marksText()
		allMatches, err := findAllMatches(pattern, marksText)
		if err != nil {
			return nil, nil, err
		}
		if len(allMatches) == 0 {
			common.Log.Info("No match found for %s on page %d", pattern, pageNum)
		}
		marks := pageText.Marks()
		boxes := []Box{}
		for _, allMatch := range allMatches {
			_, box, err2 := getMatchMarks(allMatch, marks, pattern)
			if err2 != nil {
				return nil, nil, err2
			}
			boxes = append(boxes, box)
		}
		match := Match{Pattern: pattern, Indexes: allMatches, Locations: boxes}
		matches[pageNum] = match
	}
	return matches, marksByPage, nil
}

func xOverlap(a, b model.PdfRectangle) bool {
	return b.Llx <= a.Urx && a.Llx <= b.Urx
}

type shapesState struct {
	ctm          transform.Matrix
	parentCTM    transform.Matrix
	subpaths     []*subpath
	fresh        bool
	currentPoint transform.Point
	textObj      *textObject
}
type stateStack []*textState

// Append appends `mark` to the mark array.
func (tma *TextMarkArray) Append(mark TextMark) { tma.marks = append(tma.marks, mark) }

func (to *textObject) getStrokeColor() color.Color {
	return colorToRGB(to.gs.ColorspaceStroking, to.gs.ColorStroking)
}

func (wb *wordBag) highestWord(idx int, lo, hi float64) *textWord {
	bin := wb.bins[idx]
	start := 0
	if wb.removed != nil {
		start = wb.advanceBinStart(idx)
	}
	for i := start; i < len(bin); i++ {
		w := bin[i]
		if wb.removed != nil {
			if _, ok := wb.removed[w]; ok {
				continue
			}
		}
		if lo <= w.depth && w.depth <= hi {
			return w
		}
	}
	return nil
}

func (r *ruling) intersects(other *ruling) bool {
	perp := (r.kind == rulingVert && other.kind == rulingHorz) || (other.kind == rulingVert && r.kind == rulingHorz)
	spans := func(a, b *ruling) bool {
		return a.lo-approxTolerance <= b.xy && b.xy <= a.hi+approxTolerance
	}
	ab := spans(r, other)
	ba := spans(other, r)
	if debugRulings {
		fmt.Printf("    intersects:  orthogonal=%t o1=%t o2=%t → %t\n"+"       v=%s\n"+"      w=%s\n", perp, ab, ba, perp && ab && ba, r, other)
	}
	return perp && ab && ba
}

func (pl paraList) readBefore(llyIdx []int, i, j int) bool {
	a, b := pl[i], pl[j]
	if paraXOverlaps(a, b) && a.Lly > b.Lly {
		return true
	}
	if !(a.eBBox.Urx < b.eBBox.Llx) {
		return false
	}
	lo, hi := a.Lly, b.Lly
	if lo > hi {
		hi, lo = lo, hi
	}
	x0 := math.Max(a.eBBox.Llx, b.eBBox.Llx)
	x1 := math.Min(a.eBBox.Urx, b.eBBox.Urx)
	between := pl.llyRange(llyIdx, lo, hi)
	for _, k := range between {
		if k == i || k == j {
			continue
		}
		c := pl[k]
		if c.eBBox.Llx <= x1 && x0 <= c.eBBox.Urx {
			return false
		}
	}
	return true
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func (tp *textPara) fontsize() float64 { return tp.lines[0].fontSize }

type gridWord struct {
	text     string
	llx      float64
	urx      float64
	ury      float64
	fontSize float64
	index    int
}

func (pt *PageText) appendFormContent(name string, form *model.XObjectForm, ops *contentstream.ContentStreamOperations) {
	if form == nil || ops == nil {
		return
	}
	for _, f := range pt.forms {
		if f.Operations == ops {
			return
		}
	}
	pt.forms = append(pt.forms, FormContent{Name: name, Form: form, Operations: ops})
}

func (rl rulingList) findPrimSec(xy, sec float64) *ruling {
	for _, r := range rl {
		if isZero(r.xy-xy) && r.lo-approxTolerance <= sec && sec <= r.hi+approxTolerance {
			return r
		}
	}
	return nil
}
func closeEnough(d float64) bool { return math.Abs(d) < alignTolerance }
func (pl paraList) topoOrder() []int {
	if debugTopoOrder {
		common.Log.Info("topoOrder:")
	}
	n := len(pl)
	visited := make([]bool, n)
	order := make([]int, 0, n)
	llyOrder := pl.llyOrdering()
	var visit func(i int)
	visit = func(i int) {
		visited[i] = true
		for j := 0; j < n; j++ {
			if !visited[j] {
				if pl.readBefore(llyOrder, i, j) {
					visit(j)
				}
			}
		}
		order = append(order, i)
	}
	for i := 0; i < n; i++ {
		if !visited[i] {
			visit(i)
		}
	}
	return reverseInts(order)
}

func startsWithBulletByte(b byte) bool {
	for _, bc := range bulletChars {
		if []byte(bc)[0] == b {
			return true
		}
	}
	return false
}

func (wb *wordBag) firstWord(idx int) *textWord {
	bin := wb.bins[idx]
	if wb.removed == nil {
		return bin[0]
	}
	start := wb.advanceBinStart(idx)
	if start >= len(bin) {
		return nil
	}
	return bin[start]
}

func (tc *textContext) isDehyphenationDisabled() bool {
	if tc == nil {
		return false
	}
	return tc.disableDehyphenation
}
func isEven(n int) bool { return (n & 1) == 0 }

// WriteToFile writes the edited content to `outputPath`.
func (e *Editor) WriteToFile(outputPath string) error {
	writer, err := e.reader.ToWriter(nil)
	if err != nil {
		return fmt.Errorf("failed to convert to writer %v", err)
	}
	writer.WriteToFile(outputPath)
	return nil
}

// String returns a human readable description of `s`.
func (is intSet) String() string {
	var idxs []int
	for i := range is {
		if is.has(i) {
			idxs = append(idxs, i)
		}
	}
	sort.Ints(idxs)
	return fmt.Sprintf("%+v", idxs)
}

func (to *textObject) setWordSpacing(spacing float64) {
	if to == nil {
		return
	}
	to.state.tw = spacing
}

func adjacentObjStringIndex(marks *TextMarkArray, idx int) int {
	elems := marks.Elements()
	prev := idx - 1
	next := idx + 1
	found := -1
	if prev >= 0 {
		pm := elems[prev]
		objStr := pm.ObjString
		numObj := len(objStr)
		pi := pm.Index
		if pi+1 < numObj {
			return prev
		}
	}
	if next < len(elems) {
		nm := elems[next]
		nextObjStr := nm.ObjString
		if nextObjStr[0] != nm.Text {
			return next
		}
	}
	if found == -1 && elems[idx].Text == " " {
		return prev
	}
	return found
}

func newTextState(mediaBox model.PdfRectangle) textState {
	return textState{th: 100, tmode: RenderModeFill, rect: mediaBox}
}

type formCacheKey struct {
	stream      *core.PdfObjectStream
	ctm         transform.Matrix
	resources   *model.PdfPageResources
	strokeColor colorStateKey
	fillColor   colorStateKey
}

func (iec *imageExtractContext) extractFormImages(stream *core.PdfObjectStream, name *core.PdfObjectName, gs contentstream.GraphicsState, resources *model.PdfPageResources) error {
	ps, ok := iec.parsedStreams[stream]
	if !ok {
		xf, err := model.NewXObjectFormFromStream(stream)
		if err != nil {
			return err
		}
		content, err := xf.GetContentStream()
		if err != nil {
			return err
		}
		ops, err := contentstream.NewContentStreamParser(string(content)).Parse()
		if err != nil {
			return err
		}
		matrix, err := transform.MatrixFromPdfObject(xf.Matrix)
		if err != nil {
			common.Log.Debug("ERROR: form %s: %v; using identity", name, err)
		}
		ps = &parsedImageStream{ops: ops, resources: xf.Resources, matrix: matrix}
		iec.parsedStreams[stream] = ps
	}
	iec.walker.Charge(stream, len(*ps.ops), 0, 0)
	res := ps.resources
	if res == nil {
		res = resources
	}
	formGS := gs
	formGS.CTM = gs.CTM.Mult(ps.matrix)
	numImages, inlineCount, xObjectCount, formCount := len(iec.images), iec.inlineCount, iec.xObjectCount, iec.formCount
	numPatternStack := len(iec.patternStack)
	err2 := iec.extractOperationImages(ps.ops, res, &formGS)
	var fpe *formProcessingError
	if err2 != nil && !errors.As(err2, &fpe) && (newColorStateKey(gs.ColorspaceStroking, gs.ColorStroking) != blackColorKey || newColorStateKey(gs.ColorspaceNonStroking, gs.ColorNonStroking) != blackColorKey) {
		iec.images = iec.images[:numImages]
		iec.inlineCount, iec.xObjectCount, iec.formCount = inlineCount, xObjectCount, formCount
		for _, p := range iec.patternStack[numPatternStack:] {
			delete(iec.visitedPatterns, p)
		}
		iec.patternStack = iec.patternStack[:numPatternStack]
		err2 = iec.extractOperationImages(ps.ops, res, &contentstream.GraphicsState{CTM: formGS.CTM})
	}
	if err2 != nil {
		return &formProcessingError{name: name.String(), err: err2}
	}
	iec.formCount++
	return nil
}

func (iec *imageExtractContext) extractContentStreamImages(contents string, resources *model.PdfPageResources, gs *contentstream.GraphicsState) error {
	parser := contentstream.NewContentStreamParser(contents)
	ops, err := parser.Parse()
	if err != nil {
		return err
	}
	return iec.extractOperationImages(ops, resources, gs)
}
func (is intSet) has(v int) bool { _, ok := is[v]; return ok }
func logGrids(msg string, grids []rulingList) {
	common.Log.Info("$$ %d grids - %s", len(grids), msg)
	for i, grid := range grids {
		fmt.Printf("%4d: %s\n", i, grid.String())
	}
}

func (rl rulingList) splitSec() []rulingList {
	sort.Slice(rl, func(i, j int) bool {
		a, b := rl[i], rl[j]
		if a.lo != b.lo {
			return a.lo < b.lo
		}
		return a.hi < b.hi
	})
	seen := make(map[*ruling]struct{}, len(rl))
	collect := func(seed *ruling) rulingList {
		group := rulingList{seed}
		seen[seed] = struct{}{}
		for _, r := range rl {
			if _, ok := seen[r]; ok {
				continue
			}
			if slices.ContainsFunc(group, r.alignsSec) {
				group = append(group, r)
				seen[r] = struct{}{}
			}
		}
		return group
	}
	groups := []rulingList{collect(rl[0])}
	for _, r := range rl[1:] {
		if _, ok := seen[r]; ok {
			continue
		}
		groups = append(groups, collect(r))
	}
	return groups
}

// TableInfo gets table information of the textmark `tm`.
func (tm *TextMark) TableInfo() (*TextTable, [][]int) {
	if !tm.hasTable {
		return nil, nil
	}
	table := tm.table
	cell := table.getCellInfo(*tm)
	return table, cell
}

var bidiTypesArabic = []string{"AN", "AN", "AN", "AN", "AN", "AN", "ON", "ON", "AL", "ET", "ET", "AL", "CS", "AL", "ON", "ON", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "AL", "AL", "", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "AN", "AN", "AN", "AN", "AN", "AN", "AN", "AN", "AN", "AN", "ET", "AN", "AN", "AL", "AL", "AL", "NSM", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "AN", "ON", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "AL", "AL", "NSM", "NSM", "ON", "NSM", "NSM", "NSM", "NSM", "AL", "AL", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "AL", "AL", "AL", "AL", "AL", "AL"}

func (wb *wordBag) empty(idx int) bool {
	words, ok := wb.bins[idx]
	if !ok {
		return true
	}
	if wb.removed != nil {
		return wb.removedCount[idx] >= len(words)
	}
	return false
}

const (
	rulingNone rulingKind = iota
	rulingHorz
	rulingVert
)

var licenseBypass = false

// NewFromContents creates a new extractor from contents and page resources.
func NewFromContents(contents string, resources *model.PdfPageResources) (*Extractor, error) {
	const funcName = "extractor.NewFromContents"
	ex := &Extractor{contents: contents, resources: resources, fonts: map[string]fontEntry{}, parsedForms: map[*core.PdfObjectStream]parsedForm{}, formResults: map[formCacheKey]textResult{}}
	license.TrackUse(funcName)
	return ex, nil
}

func (pl paraList) toTextMarks(ctx *textContext) []TextMark {
	offset := 0
	var marks []TextMark
	for i, para := range pl {
		if para.isEmpty {
			continue
		}
		paraMarks := para.toTextMarks(&offset, ctx)
		marks = append(marks, paraMarks...)
		if i != len(pl)-1 {
			if paraDepthEqual(para, pl[i+1]) {
				marks = appendMetaMark(marks, &offset, " ")
			} else {
				marks = appendMetaMark(marks, &offset, "\n")
				marks = appendMetaMark(marks, &offset, "\n")
			}
		}
	}
	marks = appendMetaMark(marks, &offset, "\n")
	marks = appendMetaMark(marks, &offset, "\n")
	return marks
}
func (ss *stateStack) empty() bool { return len(*ss) == 0 }
func endsWithHyphen(s string) bool {
	if utf8.RuneCountInString(s) < minHyphenatedLen {
		return false
	}
	last, size := utf8.DecodeLastRuneInString(s)
	if size <= 0 || !unicode.Is(unicode.Hyphen, last) {
		return false
	}
	last, size = utf8.DecodeLastRuneInString(s[:len(s)-size])
	return size > 0 && !unicode.IsSpace(last)
}

func tileRowKeys(m map[float64]map[float64]gridTile) []float64 {
	keys := make([]float64, 0, len(m))
	for v := range m {
		keys = append(keys, v)
	}
	sort.Float64s(keys)
	n := len(keys)
	for i := 0; i < n/2; i++ {
		keys[i], keys[n-1-i] = keys[n-1-i], keys[i]
	}
	return keys
}

func (to *textObject) moveTextSetLeading(tx, ty float64) {
	to.state.tl = -ty
	to.moveLP(tx, ty)
}
func (is intSet) del(v int) { delete(is, v) }

// String returns a human readable description of `path`.
func (sp *subpath) String() string {
	points := sp.points
	numPoints := len(points)
	if numPoints <= 5 {
		return fmt.Sprintf("%d: %6.2f", numPoints, points)
	}
	return fmt.Sprintf("%d: %6.2f %6.2f ... %6.2f", numPoints, points[0], points[1], points[numPoints-1])
}

func (ss *shapesState) lineTo(x, y float64) {
	if debugShapes {
		common.Log.Info("lineTo(%.2f,%.2f p=%.2f", x, y, ss.devicePoint(x, y))
	}
	ss.addPoint(x, y)
}

// Elements returns the TextMarks in `ma`.
func (tma *TextMarkArray) Elements() []TextMark { return tma.marks }

func separatorFor(d1, d2 float64) string {
	newLine := !isZero(d1 - d2)
	if newLine {
		return "\n"
	}
	return " "
}

func (iec *imageExtractContext) extractInlineImage(inline *contentstream.ContentStreamInlineImage, gs contentstream.GraphicsState, resources *model.PdfPageResources) error {
	key := inlineImageKey{image: inline, resources: resources}
	rgbImg, ok := iec.inlineImages[key]
	if !ok {
		img, err := inline.ToImage(resources)
		if err != nil {
			return err
		}
		cs, err := inline.GetColorSpace(resources)
		if err != nil {
			return err
		}
		if cs == nil {
			cs = model.NewPdfColorspaceDeviceGray()
		}
		rgb, err := cs.ImageToRGB(*img)
		if err != nil {
			return err
		}
		rgbImg = unpoolImage(rgb)
		iec.inlineImages[key] = rgbImg
	}
	imgCopy := *rgbImg
	mark := ImageMark{Image: &imgCopy, Width: gs.CTM.ScalingFactorX(), Height: gs.CTM.ScalingFactorY(), Angle: gs.CTM.Angle()}
	mark.X, mark.Y = gs.CTM.Translation()
	iec.images = append(iec.images, mark)
	iec.inlineCount++
	return nil
}

func (rl rulingList) mergePrimary() float64 {
	sum := rl[0].xy
	for _, r := range rl[1:] {
		sum += r.xy
	}
	return sum / float64(len(rl))
}

func (wb *wordBag) depthRange(lo, hi int) []int {
	var idxs []int
	for idx := range wb.bins {
		if lo <= idx && idx <= hi {
			idxs = append(idxs, idx)
		}
	}
	if len(idxs) == 0 {
		return nil
	}
	sort.Ints(idxs)
	return idxs
}

type textMark struct {
	model.PdfRectangle
	orientation  int
	text         string
	original     string
	font         *model.PdfFont
	fontSize     float64
	charSpacing  float64
	trm          transform.Matrix
	end          transform.Point
	origBBox     model.PdfRectangle
	fillColor    color.Color
	strokeColor  color.Color
	directObject core.PdfObject
	objString    []string
	Tw           float64
	Th           float64
	tfs          float64
	mcid         int
	index        int
}
type textWord struct {
	model.PdfRectangle
	depth       float64
	text        string
	marks       []*textMark
	fontSize    float64
	spaceBefore bool
}

func (rl rulingList) blocks(a, b *ruling) bool {
	if a.lo > b.hi || b.lo > a.hi {
		return false
	}
	lo := math.Max(a.lo, b.lo)
	hi := math.Min(a.hi, b.hi)
	if a.xy > b.xy {
		a, b = b, a
	}
	for _, r := range rl {
		if a.xy <= r.xy+alignTolerance && r.xy <= b.xy+alignTolerance && r.lo <= hi && lo <= r.hi {
			return true
		}
	}
	return false
}

func (sp *subpath) makeRectRuling(col color.Color) (*ruling, bool) {
	if debugRectRulings {
		common.Log.Info("makeRectRuling: path=%v", sp)
	}
	pts := sp.points[:4]
	kinds := make(map[int]rulingKind, len(pts))
	for i, p0 := range pts {
		p1 := sp.points[(i+1)%4]
		kinds[i] = rulingKindOfSegment(p0, p1)
		if debugRectRulings {
			fmt.Printf("%4d: %s = %6.2f - %6.2f", i, kinds[i], p0, p1)
		}
	}
	if debugRectRulings {
		fmt.Printf("   kinds=%+v\n", kinds)
	}
	var vert, horz []int
	for i, kind := range kinds {
		switch kind {
		case rulingHorz:
			horz = append(horz, i)
		case rulingVert:
			vert = append(vert, i)
		}
	}
	if debugRectRulings {
		fmt.Printf("   horzs=%d %+v\n", len(horz), horz)
		fmt.Printf("   verts=%d %+v\n", len(vert), vert)
	}
	valid := (len(horz) == 2 && len(vert) == 2) || (len(horz) == 2 && len(vert) == 0 && isNearlyHorizontal(pts[horz[0]], pts[horz[1]])) || (len(vert) == 2 && len(horz) == 0 && isFlatSegment(pts[vert[0]], pts[vert[1]]))
	if debugRectRulings {
		fmt.Printf("   horzs=%d verts=%d ok=%t\n", len(horz), len(vert), valid)
	}
	if !valid {
		if debugRectRulings {
			common.Log.Error("!!makeRectRuling: path=%v", sp)
			fmt.Printf("   horzs=%d verts=%d ok=%t\n", len(horz), len(vert), valid)
		}
		return &ruling{}, false
	}
	if len(vert) == 0 {
		for i, kind := range kinds {
			if kind != rulingHorz {
				vert = append(vert, i)
			}
		}
	}
	if len(horz) == 0 {
		for i, kind := range kinds {
			if kind != rulingVert {
				horz = append(horz, i)
			}
		}
	}
	if debugRectRulings {
		common.Log.Info("makeRectRuling: horzs=%d verts=%d points=%d\n"+"\t horzs=%+v\n"+"\t verts=%+v\n"+"\tpoints=%+v", len(horz), len(vert), len(pts), horz, vert, pts)
	}
	var vp0, vp1, hp0, hp1 transform.Point
	if pts[horz[0]].Y > pts[horz[1]].Y {
		hp0, hp1 = pts[horz[0]], pts[horz[1]]
	} else {
		hp0, hp1 = pts[horz[1]], pts[horz[0]]
	}
	if pts[vert[0]].X > pts[vert[1]].X {
		vp0, vp1 = pts[vert[0]], pts[vert[1]]
	} else {
		vp0, vp1 = pts[vert[1]], pts[vert[0]]
	}
	rect := model.PdfRectangle{Llx: vp0.X, Urx: vp1.X, Lly: hp1.Y, Ury: hp0.Y}
	if rect.Llx > rect.Urx {
		rect.Llx, rect.Urx = rect.Urx, rect.Llx
	}
	if rect.Lly > rect.Ury {
		rect.Lly, rect.Ury = rect.Ury, rect.Lly
	}
	rr := rectRuling{PdfRectangle: rect, kind: rectRulingKind(rect), Color: col}
	if rr.kind == rulingNone {
		if debugRectRulings {
			common.Log.Error("makeRectRuling: kind=nil")
		}
		return nil, false
	}
	r, ok := rr.asRuling()
	if !ok {
		if debugRectRulings {
			common.Log.Error("makeRectRuling: !isRuling")
		}
		return nil, false
	}
	if debugRulings {
		fmt.Printf("   r=%s\n", r.String())
	}
	return r, true
}

func (rl rulingList) augmentGrid() (rulingList, rulingList) {
	verts, horzs := rl.vertsHorzs()
	if len(verts) == 0 || len(horzs) == 0 {
		return verts, horzs
	}
	origVerts, origHorzs := verts, horzs
	vbox := verts.bbox()
	hbox := horzs.bbox()
	if debugRulings {
		common.Log.Info("augmentGrid: bboxV=%6.2f", vbox)
		common.Log.Info("augmentGrid: bboxH=%6.2f", hbox)
	}
	var left, right, bottom, top *ruling
	if hbox.Llx < vbox.Llx-approxTolerance {
		left = &ruling{mark: markKindAugment, kind: rulingVert, xy: hbox.Llx, lo: vbox.Lly, hi: vbox.Ury}
		verts = append(rulingList{left}, verts...)
	}
	if hbox.Urx > vbox.Urx+approxTolerance {
		right = &ruling{mark: markKindAugment, kind: rulingVert, xy: hbox.Urx, lo: vbox.Lly, hi: vbox.Ury}
		verts = append(verts, right)
	}
	if vbox.Lly < hbox.Lly-approxTolerance {
		bottom = &ruling{mark: markKindAugment, kind: rulingHorz, xy: vbox.Lly, lo: hbox.Llx, hi: hbox.Urx}
		horzs = append(rulingList{bottom}, horzs...)
	}
	if vbox.Ury > hbox.Ury+approxTolerance {
		top = &ruling{mark: markKindAugment, kind: rulingHorz, xy: vbox.Ury, lo: hbox.Llx, hi: hbox.Urx}
		horzs = append(horzs, top)
	}
	if len(verts)+len(horzs) == len(rl) {
		return origVerts, origHorzs
	}
	all := append(verts, horzs...)
	rl.log("unaugmented")
	all.log("augmented")
	return verts, horzs
}

func (pl paraList) llyOrdering() []int {
	order := make([]int, len(pl))
	for i := range pl {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		return pl[a].Lly < pl[b].Lly
	})
	return order
}

func rectContains(a, b model.PdfRectangle) bool {
	return a.Llx <= b.Llx && b.Urx <= a.Urx && a.Lly <= b.Lly && b.Ury <= a.Ury
}

// String returns a description of `p`.
func (tp *textPara) String() string {
	if tp.isEmpty {
		return fmt.Sprintf("%6.2f [EMPTY]", tp.PdfRectangle)
	}
	dims := ""
	if tp.table != nil {
		dims = fmt.Sprintf("[%dx%d] ", tp.table.w, tp.table.h)
	}
	return fmt.Sprintf("%6.2f %s%d lines %q", tp.PdfRectangle, dims, len(tp.lines), truncate(tp.text(), 50))
}

func (to *textObject) setTextLeading(leading float64) {
	if to == nil {
		return
	}
	to.state.tl = leading
}

func (ss *shapesState) drawRectangle(x, y, w, h float64) {
	if debugShapes {
		p0 := ss.devicePoint(x, y)
		p1 := ss.devicePoint(x+w, y+h)
		rect := model.PdfRectangle{Llx: p0.X, Lly: p0.Y, Urx: p1.X, Ury: p1.Y}
		common.Log.Info("drawRectangle: %6.2f", rect)
	}
	ss.newSubPath()
	ss.moveTo(x, y)
	ss.lineTo(x+w, y)
	ss.lineTo(x+w, y+h)
	ss.lineTo(x, y+h)
	ss.closePath()
}

func (r *ruling) alignsPrimary(other *ruling) bool {
	return r.kind == other.kind && math.Abs(r.xy-other.xy) < alignTolerance*0.5
}

func (tm *textMark) inDiacriticArea(mark *textMark) bool {
	dllx := tm.Llx - mark.Llx
	durx := tm.Urx - mark.Urx
	dlly := tm.Lly - mark.Lly
	return math.Abs(dllx+durx) < tm.Width()*diacriticAreaFactor && math.Abs(dlly) < tm.Height()*diacriticAreaFactor
}

func (r *ruling) encloses(lo, hi float64) bool {
	return r.lo-approxTolerance <= lo && hi <= r.hi+approxTolerance
}

func (tt *textTable) depth() float64 {
	minDepth := 1e10
	for i := 0; i < tt.w; i++ {
		para := tt.get(i, 0)
		if para == nil || para.isEmpty {
			continue
		}
		minDepth = math.Min(minDepth, para.depth())
	}
	return minDepth
}

func tileColumnKeys(m map[float64]map[float64]gridTile) []float64 {
	keys := make([]float64, 0, len(m))
	seen := make(map[float64]struct{}, len(m))
	for _, row := range m {
		for v := range row {
			if _, ok := seen[v]; ok {
				continue
			}
			keys = append(keys, v)
			seen[v] = struct{}{}
		}
	}
	sort.Float64s(keys)
	return keys
}

func buildChildLists(lines []*textLine, m map[float64][]*textLine, depths []float64, level int, parentDepth, nextDepth float64) []*list {
	items := []*list{}
	lvl := level
	level = level + 1
	d := depths[lvl]
	levelLines := m[d]
	cands := linesBetweenDepths(levelLines, nextDepth, parentDepth)
	for i, line := range cands {
		var limit float64
		subItems := []*list{}
		depth := line.depth
		next := nextDepth
		if i < len(cands)-1 {
			next = cands[i+1].depth
		}
		if level < len(depths) {
			subItems = buildChildLists(lines, m, depths, level, depth, next)
		}
		limit = next
		if len(subItems) > 0 {
			subItem := subItems[0]
			if len(subItem.lines) > 0 {
				limit = subItem.lines[0].depth
			}
		}
		itemLines := []*textLine{line}
		cont := bulletContinuationLines(line, lines, depth, limit)
		itemLines = append(itemLines, cont...)
		item := newList(itemLines, "bullet", subItems)
		item.text = joinLinesText(itemLines, "")
		items = append(items, item)
	}
	return items
}

func (e *Extractor) extractPageTextFromOps(ops *contentstream.ContentStreamOperations, resources *model.PdfPageResources, parentCTM transform.Matrix, formDepth int, isAnnotation bool, initGS *contentstream.GraphicsState) (*PageText, int, int, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("extractPageTextFromOps: level=%d", formDepth)
	}
	pageText := &PageText{pageSize: e.mediaBox, structTreeRoot: e.structTreeRoot, pageObject: e.pageObject}
	state := newTextState(e.mediaBox)
	var stack stateStack
	var gs contentstream.GraphicsState
	if initGS != nil {
		gs = *initGS
	}
	tobj := newTextObject(e, resources, gs, &state, &stack)
	shapes := shapesState{parentCTM: parentCTM, ctm: transform.IdentityMatrix(), textObj: tobj}
	var inText bool
	mcid := -1
	actualText := ""
	if formDepth > maxFormDepth {
		err := errors.New("form stack overflow")
		common.Log.Debug("ERROR: extractPageTextFromOps. recursion level=%d err=%v", formDepth, err)
		return pageText, state.numChars, state.numMisparsedChars, err
	}
	pageText.ops = ops
	proc := contentstream.NewContentStreamProcessor(*ops)
	if initGS != nil {
		proc.SetInitialGraphicsState(*initGS)
	}
	if e.options != nil {
		proc.SetRelaxedMode(e.options.RelaxedMode)
	}
	proc.AddHandler(contentstream.HandlerConditionEnumAllOperands, "", func(op *contentstream.ContentStreamOperation, gstate contentstream.GraphicsState, res *model.PdfPageResources) error {
		operand := op.Operand
		if debugOperands {
			common.Log.Info("&&& op=%s", op)
		}
		switch operand {
		case "q":
			if debugShapes {
				common.Log.Info("ctm=%s", shapes.ctm)
			}
			stack.push(&state)
		case "Q":
			if !stack.empty() {
				state = *stack.pop()
			}
			shapes.ctm = gstate.CTM
			if debugShapes {
				common.Log.Info("ctm=%s", shapes.ctm)
			}
		case "BDC":
			if len(op.Params) < 2 {
				common.Log.Debug("ERROR: BDC op=%s insufficient params", op)
				return nil
			}
			dict, ok := core.GetDict(op.Params[1])
			if !ok {
				common.Log.Debug("ERROR: BDC op=%s GetDict failed", op)
				return nil
			}
			mcidObj := dict.Get("MCID")
			if mcidObj != nil {
				v, ok2 := core.GetIntVal(mcidObj)
				if !ok2 {
					common.Log.Debug("ERROR: BDC op=%s. Bad numerical object. o=%s", op, mcidObj)
				}
				mcid = v
			} else {
				mcid = -1
			}
			if e.structTreeRoot != nil && mcid != -1 && e.structParentsKey != -1 && e.structTreeRoot.ParentTree != nil {
				parentsKey := e.structParentsKey
				var walk func(kvs []*model.KValue) bool
				walk = func(kvs []*model.KValue) bool {
					for _, kv := range kvs {
						if kd := kv.GetKDict(); kd != nil {
							children := kd.GetChildren()
							if len(children) == 1 && children[0].GetMCID() != nil {
								if *children[0].GetMCID() == mcid {
									if kd.ActualText != nil {
										actualText = kd.ActualText.Decoded()
										return true
									}
									return false
								}
							} else {
								return walk(children)
							}
						}
					}
					return false
				}
				if nums := e.structTreeRoot.ParentTree.Get("Nums"); nums != nil {
					arr, ok2 := core.GetArray(nums)
					if !ok2 {
						common.Log.Debug("ERROR: BDC op=%s. Bad Nums array. o=%s", op, nums)
					} else {
						for i := 0; i < arr.Len(); i += 2 {
							if key, ok3 := core.GetInt(arr.Get(i)); ok3 {
								if int(*key) == parentsKey {
									if entry := arr.Get(i + 1); entry != nil {
										if entries, ok4 := core.GetArray(entry); ok4 {
											for _, kdObj := range entries.Elements() {
												kd, err := model.NewKDictFromPdfObject(kdObj)
												if err != nil {
													common.Log.Debug("ERROR: BDC op=%s. Bad KDict. o=%s", op, kdObj)
													continue
												}
												children := kd.GetChildren()
												if len(children) == 1 && children[0].GetMCID() != nil {
													if *children[0].GetMCID() == mcid {
														if kd.ActualText != nil {
															actualText = kd.ActualText.Decoded()
														}
														break
													} else if walk(children) {
														break
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
			if actualText == "" {
				if str, ok2 := core.GetString(dict.Get("ActualText")); ok2 {
					actualText = str.Decoded()
				}
			}
		case "EMC":
			mcid = -1
			actualText = ""
		case "BT":
			if inText {
				common.Log.Debug("BT called while in a text object")
				pageText.marks = append(pageText.marks, tobj.marks...)
			}
			inText = true
			btGS := gstate
			if isAnnotation {
				btGS = contentstream.GraphicsState{}
				btGS.CTM = shapes.ctm
			}
			btGS.CTM = parentCTM.Mult(btGS.CTM)
			tobj = newTextObject(e, res, btGS, &state, &stack)
			shapes.textObj = tobj
		case "ET":
			if !inText {
				common.Log.Debug("ET called outside of a text object")
			}
			inText = false
			pageText.marks = append(pageText.marks, tobj.marks...)
			tobj.reset()
		case "T*":
			tobj.nextLine()
		case "Td":
			if ok, err := tobj.checkOp(op, 2, true); !ok {
				common.Log.Debug("ERROR: err=%v", err)
				return err
			}
			tx, ty, err2 := twoFloatOperands(op.Params)
			if err2 != nil {
				return err2
			}
			tobj.moveText(tx, ty)
		case "TD":
			if ok, err := tobj.checkOp(op, 2, true); !ok {
				common.Log.Debug("ERROR: err=%v", err)
				return err
			}
			tx, ty, err2 := twoFloatOperands(op.Params)
			if err2 != nil {
				common.Log.Debug("ERROR: err=%v", err2)
				return err2
			}
			tobj.moveTextSetLeading(tx, ty)
		case "Tj":
			if ok, err := tobj.checkOp(op, 1, true); !ok {
				common.Log.Debug("ERROR: Tj op=%s err=%v", op, err)
				return err
			}
			obj := core.TraceToDirectObject(op.Params[0])
			stringBytes, ok2 := core.GetStringBytes(obj)
			if !ok2 {
				common.Log.Debug("ERROR: Tj op=%s GetStringBytes failed", op)
				return core.ErrTypeError
			}
			return tobj.showText(obj, stringBytes, mcid, actualText)
		case "TJ":
			if ok, err := tobj.checkOp(op, 1, true); !ok {
				common.Log.Debug("ERROR: TJ err=%v", err)
				return err
			}
			array, ok2 := core.GetArray(op.Params[0])
			if !ok2 {
				common.Log.Debug("ERROR: TJ op=%s GetArrayVal failed", op)
				return nil
			}
			return tobj.showTextAdjusted(array, mcid, actualText)
		case "'":
			if ok, err := tobj.checkOp(op, 1, true); !ok {
				common.Log.Debug("ERROR: ' err=%v", err)
				return err
			}
			obj := core.TraceToDirectObject(op.Params[0])
			stringBytes, ok2 := core.GetStringBytes(obj)
			if !ok2 {
				common.Log.Debug("ERROR: ' op=%s GetStringBytes failed", op)
				return core.ErrTypeError
			}
			tobj.nextLine()
			return tobj.showText(obj, stringBytes, mcid, actualText)
		case "\"":
			if ok, err := tobj.checkOp(op, 3, true); !ok {
				common.Log.Debug("ERROR: \" err=%v", err)
				return err
			}
			charSpace, wordSpace, err2 := twoFloatOperands(op.Params[:2])
			if err2 != nil {
				return err2
			}
			obj := core.TraceToDirectObject(op.Params[2])
			stringBytes, ok2 := core.GetStringBytes(obj)
			if !ok2 {
				common.Log.Debug("ERROR: \" op=%s GetStringBytes failed", op)
				return core.ErrTypeError
			}
			tobj.setCharSpacing(charSpace)
			tobj.setWordSpacing(wordSpace)
			tobj.nextLine()
			return tobj.showText(obj, stringBytes, mcid, actualText)
		case "TL":
			leading, err := singleFloatOperand(op)
			if err != nil {
				common.Log.Debug("ERROR: TL err=%v", err)
				return err
			}
			tobj.setTextLeading(leading)
		case "Tc":
			charSpace, err := singleFloatOperand(op)
			if err != nil {
				common.Log.Debug("ERROR: Tc err=%v", err)
				return err
			}
			tobj.setCharSpacing(charSpace)
		case "Tf":
			if ok, err := tobj.checkOp(op, 2, true); !ok {
				common.Log.Debug("ERROR: Tf err=%v", err)
				return err
			}
			nameVal, ok2 := core.GetNameVal(op.Params[0])
			if !ok2 {
				common.Log.Debug("ERROR: Tf op=%s GetNameVal failed", op)
				return core.ErrTypeError
			}
			numberAsFloat, err2 := core.GetNumberAsFloat(op.Params[1])
			if !ok2 {
				common.Log.Debug("ERROR: Tf op=%s GetFloatVal failed. err=%v", op, err2)
				return err2
			}
			err2 = tobj.setFont(nameVal, numberAsFloat)
			tobj.unsupportedFont = errors.Is(err2, core.ErrNotSupported)
			if err2 != nil && !tobj.unsupportedFont {
				return err2
			}
		case "Tm":
			if ok, err := tobj.checkOp(op, 6, true); !ok {
				common.Log.Debug("ERROR: Tm err=%v", err)
				return err
			}
			numbersAsFloat, err2 := core.GetNumbersAsFloat(op.Params)
			if err2 != nil {
				common.Log.Debug("ERROR: err=%v", err2)
				return err2
			}
			tobj.setTextMatrix(numbersAsFloat)
		case "Tr":
			if ok, err := tobj.checkOp(op, 1, true); !ok {
				common.Log.Debug("ERROR: Tr err=%v", err)
				return err
			}
			intVal, ok2 := core.GetIntVal(op.Params[0])
			if !ok2 {
				common.Log.Debug("ERROR: Tr op=%s GetIntVal failed", op)
				return core.ErrTypeError
			}
			tobj.setTextRenderMode(intVal)
		case "Ts":
			if ok, err := tobj.checkOp(op, 1, true); !ok {
				common.Log.Debug("ERROR: Ts err=%v", err)
				return err
			}
			numberAsFloat, err2 := core.GetNumberAsFloat(op.Params[0])
			if err2 != nil {
				common.Log.Debug("ERROR: err=%v", err2)
				return err2
			}
			tobj.setTextRise(numberAsFloat)
		case "Tw":
			if ok, err := tobj.checkOp(op, 1, true); !ok {
				common.Log.Debug("ERROR: err=%v", err)
				return err
			}
			numberAsFloat, err2 := core.GetNumberAsFloat(op.Params[0])
			if err2 != nil {
				common.Log.Debug("ERROR: err=%v", err2)
				return err2
			}
			tobj.setWordSpacing(numberAsFloat)
		case "Tz":
			if ok, err := tobj.checkOp(op, 1, true); !ok {
				common.Log.Debug("ERROR: err=%v", err)
				return err
			}
			numberAsFloat, err2 := core.GetNumberAsFloat(op.Params[0])
			if err2 != nil {
				common.Log.Debug("ERROR: err=%v", err2)
				return err2
			}
			tobj.setHorizScaling(numberAsFloat)
		case "cm":
			if !isAnnotation {
				shapes.ctm = gstate.CTM
			}
			if shapes.ctm.Singular() {
				fixed := transform.IdentityMatrix().Translate(shapes.ctm.Translation())
				common.Log.Debug("Singular ctm=%s→%s", shapes.ctm, fixed)
				shapes.ctm = fixed
			}
			if debugShapes {
				common.Log.Info("ctm=%s", shapes.ctm)
			}
			if inText {
				tobj.gs.CTM = parentCTM.Mult(gstate.CTM)
			}
		case "m":
			if e.isTextOnly() {
				break
			}
			if len(op.Params) != 2 {
				common.Log.Debug("WARN: error while processing `m` operator: %v. Output may be incorrect.", errRangeCheck)
				return nil
			}
			numbersAsFloat, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				return err
			}
			shapes.moveTo(numbersAsFloat[0], numbersAsFloat[1])
		case "l":
			if e.isTextOnly() {
				break
			}
			if len(op.Params) != 2 {
				common.Log.Debug("WARN: error while processing `l` operator: %v. Output may be incorrect.", errRangeCheck)
				return nil
			}
			numbersAsFloat, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				return err
			}
			shapes.lineTo(numbersAsFloat[0], numbersAsFloat[1])
		case "c":
			if e.isTextOnly() {
				break
			}
			if len(op.Params) != 6 {
				return errRangeCheck
			}
			numbersAsFloat, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				return err
			}
			common.Log.Debug("Cubic bezier params: %.2f", numbersAsFloat)
			shapes.cubicTo(numbersAsFloat[0], numbersAsFloat[1], numbersAsFloat[2], numbersAsFloat[3], numbersAsFloat[4], numbersAsFloat[5])
		case "v", "y":
			if e.isTextOnly() {
				break
			}
			if len(op.Params) != 4 {
				return errRangeCheck
			}
			numbersAsFloat, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				return err
			}
			common.Log.Debug("Cubic bezier params: %.2f", numbersAsFloat)
			shapes.quadraticTo(numbersAsFloat[0], numbersAsFloat[1], numbersAsFloat[2], numbersAsFloat[3])
		case "h":
			if !e.isTextOnly() {
				shapes.closePath()
			}
		case "re":
			if e.isTextOnly() {
				break
			}
			if len(op.Params) != 4 {
				return errRangeCheck
			}
			numbersAsFloat, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				return err
			}
			shapes.drawRectangle(numbersAsFloat[0], numbersAsFloat[1], numbersAsFloat[2], numbersAsFloat[3])
			shapes.closePath()
		case "S":
			if !e.isTextOnly() {
				shapes.stroke(&pageText.strokes)
			}
			shapes.clearPath()
		case "s":
			if !e.isTextOnly() {
				shapes.closePath()
				shapes.stroke(&pageText.strokes)
			}
			shapes.clearPath()
		case "F":
			if !e.isTextOnly() {
				shapes.fill(&pageText.fills)
			}
			shapes.clearPath()
		case "f", "f*":
			if !e.isTextOnly() {
				shapes.closePath()
				shapes.fill(&pageText.fills)
			}
			shapes.clearPath()
		case "B", "B*":
			if !e.isTextOnly() {
				shapes.fill(&pageText.fills)
				shapes.stroke(&pageText.strokes)
			}
			shapes.clearPath()
		case "b", "b*":
			if !e.isTextOnly() {
				shapes.closePath()
				shapes.fill(&pageText.fills)
				shapes.stroke(&pageText.strokes)
			}
			shapes.clearPath()
		case "n":
			shapes.clearPath()
		case "Do":
			if len(op.Params) == 0 {
				common.Log.Debug("ERROR: expected XObject name operand for Do operator. Got %+v.", op.Params)
				return core.ErrRangeError
			}
			name, ok := core.GetName(op.Params[0])
			if !ok {
				common.Log.Debug("ERROR: invalid Do operator XObject name operand: %+v.", op.Params[0])
				return core.ErrTypeError
			}
			xObjectByName, xType := res.GetXObjectByName(*name)
			if xType != model.XObjectTypeForm {
				break
			}
			form, ok := e.parsedForms[xObjectByName]
			if !ok {
				xObjectFormByName, err := res.GetXObjectFormByName(*name)
				if err != nil {
					common.Log.Debug("ERROR: %v", err)
					return err
				}
				contentStream, err := xObjectFormByName.GetContentStream()
				if err != nil {
					common.Log.Debug("ERROR: %v", err)
					return err
				}
				contentStreamParser, err := contentstream.NewContentStreamParser(string(contentStream)).Parse()
				if err != nil {
					common.Log.Debug("ERROR: form parse failed. err=%v", err)
					return err
				}
				form = parsedForm{ops: contentStreamParser, form: xObjectFormByName}
				e.parsedForms[xObjectByName] = form
			}
			resources2 := form.form.Resources
			if resources2 == nil {
				resources2 = res
			}
			formGS := contentstream.GraphicsState{ColorspaceStroking: gstate.ColorspaceStroking, ColorspaceNonStroking: gstate.ColorspaceNonStroking, ColorStroking: gstate.ColorStroking, ColorNonStroking: gstate.ColorNonStroking}
			key := makeFormCacheKey(xObjectByName, parentCTM.Mult(gstate.CTM), resources2, formGS)
			result, ok := e.formResults[key]
			if !ok {
				ctm := gstate.CTM
				if array, ok2 := core.GetArray(form.form.Matrix); ok2 {
					m, err := transform.MatrixFromPdfObject(array)
					if err != nil {
						return err
					}
					ctm = gstate.CTM.Mult(m)
				}
				e.formExtractions++
				formText, numChars, numMisparsed, err2 := e.extractPageTextFromOps(form.ops, resources2, parentCTM.Mult(ctm), formDepth+1, false, &formGS)
				var fpe *formProcessingError
				if err2 != nil && !errors.As(err2, &fpe) && (key.strokeColor != blackColorKey || key.fillColor != blackColorKey) {
					e.formExtractions++
					formText, numChars, numMisparsed, err2 = e.extractPageTextFromOps(form.ops, resources2, parentCTM.Mult(ctm), formDepth+1, false, nil)
				}
				if err2 != nil {
					common.Log.Debug("ERROR: %v", err2)
					return &formProcessingError{name: name.String(), err: err2}
				}
				result = textResult{pageText: *formText, numChars: numChars, numMisparsedChars: numMisparsed, form: form.form}
				e.formResults[key] = result
			}
			shapes.ctm = gstate.CTM
			if debugShapes {
				common.Log.Info("ctm=%s", shapes.ctm)
			}
			pageText.marks = append(pageText.marks, result.pageText.marks...)
			pageText.strokes = append(pageText.strokes, result.pageText.strokes...)
			pageText.fills = append(pageText.fills, result.pageText.fills...)
			state.numChars += result.numChars
			state.numMisparsedChars += result.numMisparsedChars
			if result.form != nil {
				pageText.appendFormContent(name.String(), result.form, result.pageText.ops)
				for _, form2 := range result.pageText.forms {
					pageText.appendFormContent(form2.Name, form2.Form, form2.Operations)
				}
			}
		case "rg", "g", "k", "cs", "sc", "scn":
			tobj.gs.ColorspaceNonStroking = gstate.ColorspaceNonStroking
			tobj.gs.ColorNonStroking = gstate.ColorNonStroking
		case "RG", "G", "K", "CS", "SC", "SCN":
			tobj.gs.ColorspaceStroking = gstate.ColorspaceStroking
			tobj.gs.ColorStroking = gstate.ColorStroking
		}
		return nil
	})
	err3 := proc.Process(resources)
	if e.options != nil && e.options.IncludeAnnotations && !isAnnotation {
		for _, annotation := range e.annotations {
			dict, ok := core.GetDict(annotation.AP)
			if !ok {
				continue
			}
			get, ok := dict.Get("N").(*core.PdfObjectStream)
			if !ok {
				continue
			}
			decodeStream, err2 := core.DecodeStream(get)
			if err2 != nil {
				common.Log.Debug("Error on decode stream: %v", err2)
				continue
			}
			apObj := get.PdfObjectDictionary.Get("Resources")
			pdfPageResourcesFromDict, err2 := model.NewPdfPageResourcesFromDict(apObj.(*core.PdfObjectDictionary))
			if err2 != nil {
				common.Log.Debug("Error on getting annotation resources: %v", err2)
				continue
			}
			m := transform.IdentityMatrix()
			if get2, ok2 := get.PdfObjectDictionary.Get("Matrix").(*core.PdfObjectArray); ok2 {
				matrixFromPdfObject, err := transform.MatrixFromPdfObject(get2)
				if err != nil {
					common.Log.Debug("Invalid annotation appearance Matrix: %v", err)
					continue
				}
				m = matrixFromPdfObject
			}
			result, ok := e.annotationResults[get.String()]
			if !ok {
				pageText2, numChars, numMisparsed, err := e.extractPageText(string(decodeStream), pdfPageResourcesFromDict, m, formDepth+1, true, nil)
				if err != nil {
					common.Log.Debug("ERROR extracting annotation texts: %v", err)
					continue
				}
				result = textResult{pageText: *pageText2, numChars: numChars, numMisparsedChars: numMisparsed}
				e.annotationResults[get.String()] = result
			}
			pageText.marks = append(pageText.marks, result.pageText.marks...)
			pageText.strokes = append(pageText.strokes, result.pageText.strokes...)
			pageText.fills = append(pageText.fills, result.pageText.fills...)
			state.numChars += result.numChars
			state.numMisparsedChars += result.numMisparsedChars
		}
	}
	return pageText, state.numChars, state.numMisparsedChars, err3
}

func rulingKindOfSegment(p0, p1 transform.Point) rulingKind {
	dx := math.Abs(p0.X - p1.X)
	dy := math.Abs(p0.Y - p1.Y)
	return kindFromDeltas(dx, dy, maxSlope)
}

// TableCell is a cell in a TextTable.
type TableCell struct {
	model.PdfRectangle

	// Text is the extracted text.
	Text string

	// Marks returns the TextMarks corresponding to the text in Text.
	Marks TextMarkArray
}

func (wb *wordBag) absorb(bag *wordBag) {
	removals := bag.makeRemovals()
	for idx, bin := range bag.bins {
		for _, w := range bin {
			wb.pullWord(w, idx, removals)
		}
	}
	bag.applyRemovals(removals)
}

func (tw *textWord) addDiacritic(s string) {
	mark := tw.marks[len(tw.marks)-1]
	mark.text += s
	mark.text = norm.NFKC.String(mark.text)
}

const (
	zeroTolerance              = 1.0e-6
	coordGranularity           = 1.0e-4
	angleRoundingStep          = 10
	depthBinWidth              = 6
	lineMergeFactor            = 0.5
	maxWordGapFactor           = 0.12
	maxOverlapFactor           = 0.19
	maxDepthDeltaFactor        = 0.04
	scanMarginFactor           = 1.0
	verticalGapFraction        = 0.04
	minFontSize                = 12
	horizontalScanOffsetFactor = 0.4
	horizontalGapFraction      = 0.7
	otherScanOffsetFactor      = 1.0
	otherGapFraction           = 0.1
	lineHeightMax              = 1.4
	lineHeightMin              = 0.46
	wordBoundaryGapFactor      = 0.02
	duplicateToleranceFactor   = 0.2
	diacriticAreaFactor        = 0.5
	minHyphenatedLen           = 4
	readingOrderFontFactor     = 4.0
	minTableParas              = 6
	maxEmptyFraction           = 0.3
	xNeighbourTolerance        = 0.01
	yNeighbourTolerance        = 0.02
	minGridCols                = 2
	minGridRows                = 2
	maxGridRulings             = 500
	minRulingLength            = 4.0
	maxSlope                   = 0.05
	slopeEpsilon               = 0.1
	approxTolerance            = 2.0
	alignTolerance             = 2.0
	containTolerance           = 1.5
	reduceTilingTolerance      = 3.0
	alignedFraction            = 0.25
)

// StrokePath is a stroked path.
type StrokePath struct {
	Points []transform.Point
	color.Color
}

func (to *textObject) renderText(obj core.PdfObject, raw []byte, mcid int, actualText string) error {
	if to.unsupportedFont {
		common.Log.Debug("renderText: Invalid font. Not processing.")
		return nil
	}
	font := to.getCurrentFont()
	codes := font.BytesToCharcodes(raw)
	var (
		strs      []string
		numChars  int
		numMisses int
	)
	if actualText != "" {
		allCtrl := true
		for _, r := range actualText {
			if r > 0x1F {
				allCtrl = false
				break
			}
		}
		if allCtrl {
			return nil
		}
		strs = []string{actualText}
		numChars = 1
	} else {
		strs, numChars, numMisses = font.CharcodesToStrings(codes, "")
		if numMisses > 0 {
			common.Log.Debug("renderText: numChars=%d numMisses=%d", numChars, numMisses)
		}
	}
	to.state.numChars += numChars
	to.state.numMisparsedChars += numMisses
	state := to.state
	tfs := state.tfs
	hScale := state.th / 100.0
	glyphUnit := glyphSpaceUnit
	if font.Subtype() == "Type3" {
		glyphUnit = 1
	}
	spaceMetrics, ok := font.GetRuneMetrics(' ')
	if !ok {
		spaceMetrics, ok = font.GetCharMetrics(32)
	}
	if !ok {
		spaceMetrics, _ = model.DefaultFont().GetRuneMetrics(' ')
	}
	spaceWidth := spaceMetrics.Wx * glyphUnit
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("spaceWidth=%.2f text=%q font=%s fontSize=%.2f", spaceWidth, strs, font, tfs)
	}
	trm0 := transform.NewMatrix(tfs*hScale, 0, 0, tfs, 0, state.trise)
	if debugTextState {
		common.Log.Info("renderText: %d codes=%+v texts=%q", len(codes), codes, strs)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("renderText: %d codes=%+v runes=%q", len(codes), codes, len(strs))
	}
	fillColor := to.getFillColor()
	strokeColor := to.getStrokeColor()
	if actualText != "" {
		runes := []rune(strs[0])
		trm := to.gs.CTM.Mult(to.tm).Mult(trm0)
		codeStrs, _, _ := font.CharcodesToStrings(codes, "")
		for i, code := range codes {
			wordSpace := 0.0
			if i < len(codeStrs) {
				if cr := []rune(codeStrs[i]); len(cr) == 1 && cr[0] == 32 {
					wordSpace = state.tw
				}
			}
			metrics, ok2 := font.GetCharMetrics(code)
			if !ok2 {
				continue
			}
			disp := transform.Point{X: metrics.Wx * glyphUnit, Y: metrics.Wy * glyphUnit}
			shift := transform.Point{X: (disp.X*tfs + state.tc + wordSpace) * hScale}
			trans := translateBy(shift)
			to.tm.Concat(trans)
		}
		pos := translationPoint(to.gs.CTM.Mult(to.tm))
		mark, ok3 := to.newTextMark(textencoding.ExpandLigatures(runes), trm, pos, math.Abs(spaceWidth*trm.ScalingFactorX()), font, to.state.tc, fillColor, strokeColor, obj, strs, 0, mcid)
		mark.tfs = tfs
		if ok3 {
			to.marks = append(to.marks, &mark)
		}
		return nil
	}
	for i, str := range strs {
		runes := []rune(str)
		if len(runes) == 1 && runes[0] == '\x00' {
			continue
		}
		code := codes[i]
		trm := to.gs.CTM.Mult(to.tm).Mult(trm0)
		wordSpace := 0.0
		if len(runes) == 1 && runes[0] == 32 {
			wordSpace = state.tw
		}
		metrics, ok2 := font.GetCharMetrics(code)
		if !ok2 {
			common.Log.Debug("ERROR: No metric for code=%d r=0x%04x=%+q %s", code, runes, runes, font)
			return fmt.Errorf("no char metrics: font=%s code=%d", font.String(), code)
		}
		disp := transform.Point{X: metrics.Wx * glyphUnit, Y: metrics.Wy * glyphUnit}
		shiftNoTc := transform.Point{X: (disp.X*tfs + wordSpace) * hScale}
		shift := transform.Point{X: (disp.X*tfs + state.tc + wordSpace) * hScale}
		if debugTextState {
			common.Log.Info("tfs=%.2f tc=%.2f tw=%.2f th=%.2f", tfs, state.tc, state.tw, hScale)
			common.Log.Info("dx,dy=%.3f t0=%.3f t=%.3f", disp, shiftNoTc, shift)
		}
		t0m := translateBy(shiftNoTc)
		advance := translateBy(shift)
		glyphTM := to.gs.CTM.Mult(to.tm).Mult(t0m)
		if debugTextRender {
			common.Log.Info("end:\n\tCTM=%s\n\t tm=%s\n"+"\t td=%s xlat=%s\n"+"\ttd0=%s\n\t  → %s xlat=%s", to.gs.CTM, to.tm, advance, translationPoint(to.gs.CTM.Mult(to.tm).Mult(advance)), t0m, glyphTM, translationPoint(glyphTM))
		}
		mark, ok3 := to.newTextMark(textencoding.ExpandLigatures(runes), trm, translationPoint(glyphTM), math.Abs(spaceWidth*trm.ScalingFactorX()), font, to.state.tc, fillColor, strokeColor, obj, strs, i, mcid)
		mark.tfs = tfs
		to.tm.Concat(advance)
		if !ok3 {
			common.Log.Debug("Text mark outside page. Skipping")
			continue
		}
		if font == nil {
			common.Log.Debug("ERROR: No font.")
		} else if font.Encoder() == nil {
			common.Log.Debug("ERROR: No encoding. font=%s", font)
		} else {
			if r, ok4 := font.Encoder().CharcodeToRune(code); ok4 {
				mark.original = string(r)
			}
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("i=%d code=%d mark=%s trm=%s", i, code, mark, trm)
		}
		to.marks = append(to.marks, &mark)
	}
	return nil
}

func (to *textObject) setTextMatrix(vals []float64) {
	if len(vals) != 6 {
		common.Log.Debug("ERROR: len(f) != 6 (%d)", len(vals))
		return
	}
	a, b, c, d, e, f := vals[0], vals[1], vals[2], vals[3], vals[4], vals[5]
	to.tm = transform.NewMatrix(a, b, c, d, e, f)
	to.tlm = to.tm
}

func leftEdgeRuling(rect model.PdfRectangle) *ruling {
	return &ruling{kind: rulingVert, xy: rect.Llx, lo: rect.Lly, hi: rect.Ury}
}

// ExtractPageImages returns the image contents of the page extractor, including data
// and position, size information for each image.
// A set of options to control page image extraction can be passed in. The options
// parameter can be nil for the default options. By default, inline stencil masks
// are not extracted.
func (e *Extractor) ExtractPageImages(options *ImageExtractOptions) (*PageImages, error) {
	ctx := &imageExtractContext{options: options}
	err := ctx.extractContentStreamImages(e.contents, e.resources, nil)
	if err != nil {
		return nil, err
	}
	return &PageImages{Images: ctx.images}, nil
}

// String returns a description of `tm`.
func (tm *textMark) String() string {
	return fmt.Sprintf("%.2f fontsize=%.2f \"%s\"", tm.PdfRectangle, tm.fontSize, tm.text)
}

func (pl paraList) findTableGrid(tiling gridTiling) (*textTable, map[*textPara]struct{}) {
	numLlx := len(tiling.llx)
	numLly := len(tiling.lly)
	table := textTable{
		exportable: true, w: numLlx, h: numLly, cells: make(map[uint64]*textPara, numLlx*numLly), compositeCells: make(map[uint64]compositeCell, numLlx*numLly),
		PdfRectangle: tiling.PdfRectangle,
	}
	used := make(map[*textPara]struct{})
	maxEmpty := int((1.0 - maxEmptyFraction) * float64(numLlx*numLly))
	numEmpty := 0
	if debugGrids {
		common.Log.Info("findTableGrid: %d x %d", numLlx, numLly)
	}
	for row, lly := range tiling.lly {
		rowTiles, ok := tiling.tiles[lly]
		if !ok {
			continue
		}
		for col, llx := range tiling.llx {
			tile, ok2 := rowTiles[llx]
			if !ok2 {
				continue
			}
			paras := pl.inTile(tile)
			if len(paras) == 0 {
				numEmpty++
				if numEmpty > maxEmpty {
					if debugGrids {
						common.Log.Info("!numEmpty=%d", numEmpty)
					}
					return nil, nil
				}
			} else {
				table.putComposite(col, row, paras, tile.PdfRectangle)
				for _, para := range paras {
					used[para] = struct{}{}
				}
			}
		}
	}
	numHeader := 0
	for i := 0; i < numLlx; i++ {
		para := table.get(i, 0)
		if para == nil || !para.isEmpty {
			numHeader++
		}
	}
	if numHeader == 0 {
		if debugGrids {
			common.Log.Info("!numHeader=0")
		}
		return nil, nil
	}
	reduced := table.reduceTiling(tiling, reduceTilingTolerance)
	reduced = reduced.subdivide()
	return reduced, used
}

func (tt *textTable) getRight() paraList {
	paras := make(paraList, tt.h)
	for i := 0; i < tt.h; i++ {
		para := tt.get(tt.w-1, i).right
		if para.taken() {
			return nil
		}
		paras[i] = para
	}
	for i := 0; i < tt.h-1; i++ {
		if paras[i].below != paras[i+1] {
			return nil
		}
	}
	return paras
}

type parsedForm struct {
	ops  *contentstream.ContentStreamOperations
	form *model.XObjectForm
}

func (r *ruling) equals(other *ruling) bool {
	return r.kind == other.kind && approxEqual(r.xy, other.xy) && approxEqual(r.lo, other.lo) && approxEqual(r.hi, other.hi)
}

func reorderBidi(runes []rune) BidiText {
	baseLevel := -1
	vertical := false
	ltr := true
	numRunes := len(runes)
	chars := make([]string, numRunes)
	types := make([]string, numRunes)
	if numRunes == 0 || vertical {
		return newBidiText(string(runes), ltr, vertical)
	}
	numRTL := 0
	for i, runeV := range runes {
		chars[i] = string(runeV)
		typ := "L"
		if runeV <= 0x00ff {
			typ = bidiTypesLatin[runeV]
		} else if 0x0590 <= runeV && runeV <= 0x05f4 {
			typ = "R"
		} else if 0x0600 <= runeV && runeV <= 0x06ff {
			low := runeV & 0xff
			if int(low) >= len(bidiTypesArabic) {
				common.Log.Debug("Bidi: invalid Unicode character " + string(runeV))
			}
			typ = bidiTypesArabic[runeV&0xff]
		} else if (0x0700 <= runeV && runeV <= 0x08ac) || (0xfb50 <= runeV && runeV <= 0xfdff) || (0xfe70 <= runeV && runeV <= 0xfeff) {
			typ = "AL"
		}
		if typ == "R" || typ == "AL" || typ == "AN" {
			numRTL++
		}
		types[i] = typ
	}
	if numRTL == 0 {
		ltr = true
		return newBidiText(string(runes), ltr, false)
	}
	if baseLevel == -1 {
		if float64(numRTL)/float64(numRunes) < 0.3 && numRunes > 4 {
			ltr = true
			baseLevel = 0
		} else {
			ltr = false
			baseLevel = 1
		}
	}
	var levels []int
	for range runes {
		levels = append(levels, baseLevel)
	}
	baseType := "L"
	if isOdd(baseLevel) {
		baseType = "R"
	}
	baseDir := baseType
	prevType := baseDir
	lastStrong := baseDir
	for i := range runes {
		if types[i] == "NSM" {
			types[i] = lastStrong
		} else {
			lastStrong = types[i]
		}
	}
	lastStrong = baseDir
	var s string
	for i := range runes {
		s = types[i]
		switch s {
		case "EN":
			if lastStrong == "AL" {
				types[i] = "AN"
			} else {
				types[i] = "EN"
			}
		case "R", "L", "AL":
			lastStrong = s
		}
	}
	for i := range runes {
		typeV := types[i]
		if typeV == "AL" {
			types[i] = "R"
		}
	}
	for i := 1; i < (len(runes) - 1); i++ {
		if types[i] == "ES" && types[i-1] == "EN" && types[i+1] == "EN" {
			types[i] = "EN"
		}
		if types[i] == "CS" && (types[i-1] == "EN" || types[i-1] == "AN") && types[i+1] == types[i-1] {
			types[i] = types[i-1]
		}
	}
	for i := range runes {
		if types[i] == "EN" {
			for j := i - 1; j >= 0; j-- {
				if types[j] != "ET" {
					break
				}
				types[j] = "EN"
			}
			for j := i + 1; j < numRunes; j++ {
				if types[j] != "ET" {
					break
				}
				types[j] = "EN"
			}
		}
	}
	for i := range runes {
		typeV := types[i]
		if typeV == "WS" || typeV == "ES" || typeV == "ET" || typeV == "CS" {
			types[i] = "ON"
		}
	}
	lastStrong = "sor"
	for i := range runes {
		typeV := types[i]
		switch typeV {
		case "EN":
			if lastStrong == "L" {
				types[i] = "L"
			} else {
				types[i] = "EN"
			}
		case "R", "L":
			lastStrong = typeV
		}
	}
	for i := 0; i < len(runes); i++ {
		if types[i] == "ON" {
			runEnd := nextIndexNot(types, i+1, "ON")
			before := prevType
			if i > 0 {
				before = types[i-1]
			}
			after := prevType
			if runEnd+1 < numRunes {
				after = types[runEnd+1]
			}
			if before != "L" {
				before = "R"
			}
			if after != "L" {
				after = "R"
			}
			if before == after {
				fillStrings(types, i, runEnd, before)
			}
			i = runEnd - 1
		}
	}
	for i := range runes {
		if types[i] == "ON" {
			types[i] = baseType
		}
	}
	for i := range runes {
		typeV := types[i]
		if isEven(levels[i]) {
			switch typeV {
			case "R":
				levels[i]++
			case "AN", "EN":
				levels[i] += 2
			}
		} else if typeV == "L" || typeV == "AN" || typeV == "EN" {
			levels[i]++
		}
	}
	maxLevel := -1
	minOdd := 99
	var lvl int
	for i := 0; i < len(levels); i++ {
		lvl = levels[i]
		if maxLevel < lvl {
			maxLevel = lvl
		}
		if minOdd > lvl && isOdd(lvl) {
			minOdd = lvl
		}
	}
	for i := maxLevel; i >= minOdd; i-- {
		runStart := -1
		for j := 0; j < len(levels); j++ {
			if levels[j] < i {
				if runStart >= 0 {
					reverseStrings(chars, runStart, j)
					runStart = -1
				}
			} else if runStart < 0 {
				runStart = j
			}
		}
		if runStart >= 0 {
			reverseStrings(chars, runStart, len(levels))
		}
	}
	for i := 0; i < len(chars); i++ {
		char := chars[i]
		if char == "<" || char == ">" {
			chars[i] = ""
		}
	}
	return newBidiText(strings.Join(chars, ""), ltr, false)
}

func (tma *TextMarkArray) exists(m TextMark) bool {
	for _, other := range tma.Elements() {
		if reflect.DeepEqual(m.DirectObject, other.DirectObject) && reflect.DeepEqual(m.BBox, other.BBox) && other.Text == m.Text {
			return true
		}
	}
	return false
}

type textResult struct {
	pageText          PageText
	numChars          int
	numMisparsedChars int
	form              *model.XObjectForm
}

func hasSingleMCID(line *textLine) bool {
	single := true
	firstMCID := -1
	for _, word := range line.words {
		for _, mark := range word.marks {
			mcid := mark.mcid
			if firstMCID == -1 {
				firstMCID = mcid
			} else {
				if firstMCID != mcid {
					single = false
					break
				}
			}
		}
	}
	return single
}

// PageImages represents extracted images on a PDF page with spatial information:
// display position and size.
type PageImages struct{ Images []ImageMark }

func (ss *shapesState) newSubPath() {
	ss.clearPath()
	if debugShapes {
		common.Log.Info("newSubPath: %s", ss)
	}
}
func rectsOverlap(a, b model.PdfRectangle) bool { return xOverlap(a, b) && yOverlap(a, b) }
func nextIndexNot(strs []string, from int, val string) int {
	i := from
	for ; i < len(strs); i++ {
		if strs[i] != val {
			return i
		}
	}
	return i
}
func (pl paraList) sortTopoOrder() { order := pl.topoOrder(); pl.reorder(order) }
func (ps pathSection) bbox() model.PdfRectangle {
	start := ps.subpaths[0].points[0]
	rect := model.PdfRectangle{Llx: start.X, Urx: start.X, Lly: start.Y, Ury: start.Y}
	grow := func(p transform.Point) {
		if p.X < rect.Llx {
			rect.Llx = p.X
		} else if p.X > rect.Urx {
			rect.Urx = p.X
		}
		if p.Y < rect.Lly {
			rect.Lly = p.Y
		} else if p.Y > rect.Ury {
			rect.Ury = p.Y
		}
	}
	for _, p := range ps.subpaths[0].points[1:] {
		grow(p)
	}
	for _, sp := range ps.subpaths[1:] {
		for _, p := range sp.points {
			grow(p)
		}
	}
	return rect
}

func attachTable(marks []TextMark, table *TextTable) []TextMark {
	var out []TextMark
	for _, mark := range marks {
		mark.hasTable = true
		mark.table = table
		out = append(out, mark)
	}
	return out
}

func (rl rulingList) sortStrict() {
	sort.Slice(rl, func(i, j int) bool {
		a, b := rl[i], rl[j]
		ka, kb := a.kind, b.kind
		if ka != kb {
			return ka > kb
		}
		va, vb := a.xy, b.xy
		if !isZero(va - vb) {
			return va < vb
		}
		va, vb = a.lo, b.lo
		if va != vb {
			return va < vb
		}
		return a.hi < b.hi
	})
}

// ExtractTextWithStats works like ExtractText but returns the number of characters in the output
// (`numChars`) and the number of characters that were not decoded (`numMisses`).
func (e *Extractor) ExtractTextWithStats() (text string, numChars int, numMisses int, err error) {
	pageText, numChars, numMisses, err := e.ExtractPageText()
	if err != nil {
		return "", numChars, numMisses, err
	}
	return pageText.Text(), numChars, numMisses, nil
}

const (
	msgFontConvert       = "ERROR: Can't convert font object, invalid type"
	msgFontPropsNotFound = "ERROR: Can't get font properties, font not found"
	msgFontStreamInvalid = "ERROR: Can't get font stream, invalid type"
	msgNoFontFound       = "ERROR: No font found"
)

func (iec *imageExtractContext) processOperand(op *contentstream.ContentStreamOperation, gs contentstream.GraphicsState, resources *model.PdfPageResources) error {
	if op.Operand == "BI" && len(op.Params) == 1 {
		inline, ok2 := op.Params[0].(*contentstream.ContentStreamInlineImage)
		if !ok2 {
			return nil
		}
		if isMask, ok := core.GetBoolVal(inline.ImageMask); ok {
			if isMask && !iec.options.IncludeInlineStencilMasks {
				return nil
			}
		}
		return iec.extractInlineImage(inline, gs, resources)
	} else if op.Operand == "Do" && len(op.Params) == 1 {
		name, ok := core.GetName(op.Params[0])
		if !ok {
			common.Log.Debug("ERROR: Type")
			return errTypeCheck
		}
		xobj, xType := resources.GetXObjectByName(*name)
		switch xType {
		case model.XObjectTypeImage:
			return iec.extractXObjectImage(name, gs, resources)
		case model.XObjectTypeForm:
			if !iec.walker.Enter(xobj, name.String()) {
				return nil
			}
			defer iec.walker.Leave(xobj)
			return iec.extractFormImages(xobj, name, gs, resources)
		}
	} else if (op.Operand == "scn" && isPatternColorspace(gs.ColorspaceNonStroking) || op.Operand == "SCN" && isPatternColorspace(gs.ColorspaceStroking)) && len(op.Params) == 1 {
		name, ok := core.GetName(op.Params[0])
		if !ok {
			common.Log.Debug("ERROR: Type")
			return errTypeCheck
		}
		pat, ok := resources.GetPatternByName(*name)
		if !ok {
			common.Log.Debug("ERROR: Pattern not found")
			return nil
		}
		if pat.IsTiling() {
			stream, ok2 := core.GetStream(core.TraceToDirectObject(pat.GetContainingPdfObject()))
			if !ok2 {
				return nil
			}
			pw := patternWalk{stream: stream, ctm: iec.ctm}
			if _, ok3 := iec.visitedPatterns[pw]; ok3 {
				return nil
			}
			if !iec.walker.Enter(stream, name.String()) {
				return nil
			}
			defer iec.walker.Leave(stream)
			ps, ok2 := iec.parsedStreams[stream]
			if !ok2 {
				tiling := pat.GetAsTilingPattern()
				content, err := tiling.GetContentStream()
				if err != nil {
					return err
				}
				ops, err := contentstream.NewContentStreamParser(string(content)).Parse()
				if err != nil {
					return err
				}
				matrix, err := transform.MatrixFromPdfObject(tiling.Matrix)
				if err != nil {
					common.Log.Debug("ERROR: pattern %s: %v; using identity", name, err)
				}
				ps = &parsedImageStream{ops: ops, resources: tiling.Resources, matrix: matrix}
				iec.parsedStreams[stream] = ps
			}
			iec.walker.Charge(stream, len(*ps.ops), 0, 0)
			iec.visitedPatterns[pw] = struct{}{}
			iec.patternStack = append(iec.patternStack, pw)
			patGS := contentstream.GraphicsState{CTM: iec.ctm.Mult(ps.matrix)}
			err2 := iec.extractOperationImages(ps.ops, ps.resources, &patGS)
			if err2 != nil {
				return err2
			}
		}
	}
	return nil
}

// ExtractText processes and extracts all text data in content streams and returns as a string.
// It takes into account character encodings in the PDF file, which are decoded by
// CharcodeBytesToUnicode.
// Characters that can't be decoded are replaced with MissingCodeRune ('\ufffd' = �).
func (e *Extractor) ExtractText() (string, error) {
	textWithStats, _, _, err := e.ExtractTextWithStats()
	return textWithStats, err
}
func (tt *textTable) bbox() model.PdfRectangle { return tt.PdfRectangle }

const (
	gapFactor               = 1.5
	rowToleranceFactor      = 0.5
	rowHeightFactor         = 0.35
	maxIndexGap             = 3
	rowSearchDepth          = 5
	splitGapFactor          = 10.0
	mergeGapFactor          = 1.0
	medianSpacingFactor     = 1.8
	maxGapSpaces            = 40
	maxOffsetSpaces         = 60
	defaultCharWidth        = 6.0
	defaultRowSpacingFactor = 5
	maxDedupeRunes          = 3
	dupPositionTolerance    = 2.0
	dupLookback             = 30
	minDupRunes             = 2
)

func colorToRGB(cs model.PdfColorspace, c model.PdfColor) color.Color {
	if cs == nil || c == nil {
		return color.Black
	}
	rgbColor, err := cs.ColorToRGB(c)
	if err != nil {
		common.Log.Debug("WARN: could not convert color %v (%v) to RGB: %s", c, cs, err)
		return color.Black
	}
	rgb, ok := rgbColor.(*model.PdfColorDeviceRGB)
	if !ok {
		common.Log.Debug("WARN: converted color is not in the RGB colorspace: %v", rgbColor)
		return color.Black
	}
	return color.NRGBA{R: uint8(rgb.R() * 255), G: uint8(rgb.G() * 255), B: uint8(rgb.B() * 255), A: uint8(255)}
}

func (cc compositeCell) String() string {
	txt := ""
	if len(cc.paraList) > 0 {
		txt = truncate(cc.paraList.merge().text(), 50)
	}
	return fmt.Sprintf("%6.2f %d paras %q", cc.PdfRectangle, len(cc.paraList), txt)
}

func (pl paraList) writeText(writer io.Writer, ctx *textContext) {
	for i, para := range pl {
		if para.isEmpty {
			continue
		}
		para.writeText(writer, ctx)
		if i != len(pl)-1 {
			if paraDepthEqual(para, pl[i+1]) {
				writer.Write([]byte(" "))
			} else {
				writer.Write([]byte("\n"))
				writer.Write([]byte("\n"))
			}
		}
	}
	writer.Write([]byte("\n"))
	writer.Write([]byte("\n"))
}
func (ss *stateStack) size() int { return len(*ss) }
func translateBy(d transform.Point) transform.Matrix {
	return transform.TranslationMatrix(d.X, d.Y)
}

func (tt *textTable) computeBbox() model.PdfRectangle {
	var rect model.PdfRectangle
	set := false
	for i := 0; i < tt.h; i++ {
		for j := 0; j < tt.w; j++ {
			para := tt.get(j, i)
			if para == nil {
				continue
			}
			if !set {
				rect = para.PdfRectangle
				set = true
			} else {
				rect = unionRects(rect, para.PdfRectangle)
			}
		}
	}
	return rect
}

// RangeOffset returns the TextMarks in `ma` that overlap text[start:end] in the extracted text.
// These are tm: `start` <= tm.Offset + len(tm.Text) && tm.Offset < `end` where
// `start` and `end` are offsets in the extracted text.
// NOTE: TextMarks can contain multiple characters. e.g. "ffi" for the ﬃ ligature so the first and
// last elements of the returned TextMarkArray may only partially overlap text[start:end].
func (tma *TextMarkArray) RangeOffset(start, end int) (*TextMarkArray, error) {
	if tma == nil {
		return nil, errors.New("ma==nil")
	}
	if end < start {
		return nil, fmt.Errorf("end < start. RangeOffset not defined. start=%d end=%d ", start, end)
	}
	numMarks := len(tma.marks)
	if numMarks == 0 {
		return tma, nil
	}
	if start < tma.marks[0].Offset {
		start = tma.marks[0].Offset
	}
	if end > tma.marks[numMarks-1].Offset+1 {
		end = tma.marks[numMarks-1].Offset + 1
	}
	iStart := sort.Search(numMarks, func(i int) bool { return tma.marks[i].Offset+len(tma.marks[i].Text)-1 >= start })
	if !(0 <= iStart && iStart < numMarks) {
		err := fmt.Errorf("out of range. start=%d iStart=%d len=%d\n\tfirst=%v\n\t last=%v", start, iStart, numMarks, tma.marks[0], tma.marks[numMarks-1])
		return nil, err
	}
	iEnd := sort.Search(numMarks, func(i int) bool { return tma.marks[i].Offset > end-1 })
	if !(0 <= iEnd && iEnd < numMarks) {
		err := fmt.Errorf("out of range. end=%d iEnd=%d len=%d\n\tfirst=%v\n\t last=%v", end, iEnd, numMarks, tma.marks[0], tma.marks[numMarks-1])
		return nil, err
	}
	if iEnd <= iStart {
		return nil, fmt.Errorf("iEnd <= iStart: start=%d end=%d iStart=%d iEnd=%d", start, end, iStart, iEnd)
	}
	return &TextMarkArray{marks: tma.marks[iStart:iEnd]}, nil
}

func (to *textObject) checkOp(op *contentstream.ContentStreamOperation, numParams int, strict bool) (ok bool, err error) {
	if to == nil {
		var params []core.PdfObject
		if numParams > 0 {
			params = op.Params
			if len(params) > numParams {
				params = params[:numParams]
			}
		}
		common.Log.Debug("%#q operand outside text. params=%+v", op.Operand, params)
	}
	if numParams >= 0 {
		if len(op.Params) != numParams {
			if strict {
				err = errors.New("incorrect parameter count")
			}
			common.Log.Debug("ERROR: %#q should have %d input params, got %d %+v", op.Operand, numParams, len(op.Params), op.Params)
			return false, err
		}
	}
	return true, nil
}

func indentListText(listV *list, indent *string) string {
	lines := strings.Split(listV.text, "\n")
	builder := &strings.Builder{}
	for _, line := range lines {
		if line != "" {
			builder.WriteString(*indent)
			builder.WriteString(line)
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func splitRulingLists(rulings []rulingList) (rulingList, rulingList) {
	var all rulingList
	for _, l := range rulings {
		all = append(all, l...)
	}
	return all.vertsHorzs()
}

// PageText represents the layout of text on a device page.
type PageText struct {
	marks           []*textMark
	text            string
	cachedMarksText string
	viewMarks       []TextMark
	tables          []TextTable
	pageSize        model.PdfRectangle
	strokes         []pathSection
	fills           []pathSection
	structTreeRoot  *model.StructTreeRoot
	pageObject      core.PdfObject
	ops             *contentstream.ContentStreamOperations
	forms           []FormContent
	options         PageTextOptions
}

func loadSoftMaskImage(obj core.PdfObject, c color.Color) (image.Image, error) {
	stream, ok := core.GetStream(obj)
	if !ok {
		return nil, nil
	}
	xObjectImageFromStream, err := model.NewXObjectImageFromStream(stream)
	if err != nil {
		return nil, err
	}
	img, err := xObjectImageFromStream.ToImage()
	if err != nil {
		return nil, err
	}
	return softMaskToImage(img, c), nil
}

func paraDepthEqual(a, b *textPara) bool {
	if a.isEmpty || b.isEmpty {
		return true
	}
	return isZero(a.depth() - b.depth())
}
func dropLastRune(s string) string { runes := []rune(s); return string(runes[:len(runes)-1]) }
func (tt *textTable) logComposite(msg string) {
	if !debugTables {
		return
	}
	common.Log.Info("~~~Para %d x %d %s", tt.w, tt.h, msg)
	fmt.Printf("%5s |", "")
	for i := 0; i < tt.w; i++ {
		fmt.Printf("%3d |", i)
	}
	fmt.Println("")
	fmt.Printf("%5s +", "")
	for i := 0; i < tt.w; i++ {
		fmt.Printf("%3s +", "---")
	}
	fmt.Println("")
	for i := 0; i < tt.h; i++ {
		fmt.Printf("%5d |", i)
		for j := 0; j < tt.w; j++ {
			paras, _ := tt.compositeCells[cellKey(j, i)].parasBBox()
			fmt.Printf("%3d |", len(paras))
		}
		fmt.Println("")
	}
	common.Log.Info("~~~Text %d x %d %s", tt.w, tt.h, msg)
	fmt.Printf("%5s |", "")
	for i := 0; i < tt.w; i++ {
		fmt.Printf("%12d |", i)
	}
	fmt.Println("")
	fmt.Printf("%5s +", "")
	for i := 0; i < tt.w; i++ {
		fmt.Print("-------------+")
	}
	fmt.Println("")
	for i := 0; i < tt.h; i++ {
		fmt.Printf("%5d |", i)
		for j := 0; j < tt.w; j++ {
			paras, _ := tt.compositeCells[cellKey(j, i)].parasBBox()
			txt := ""
			para := paras.merge()
			if para != nil {
				txt = para.text()
			}
			txt = fmt.Sprintf("%q", truncate(txt, 12))
			txt = txt[1 : len(txt)-1]
			fmt.Printf("%12s |", txt)
		}
		fmt.Println("")
	}
}

func makeFormCacheKey(stream *core.PdfObjectStream, ctm transform.Matrix, resources *model.PdfPageResources, gs contentstream.GraphicsState) formCacheKey {
	return formCacheKey{stream: stream, ctm: ctm, resources: resources, strokeColor: newColorStateKey(gs.ColorspaceStroking, gs.ColorStroking), fillColor: newColorStateKey(gs.ColorspaceNonStroking, gs.ColorNonStroking)}
}

// Text returns the extracted page text.
func (pt PageText) Text() string { return pt.text }

func (rl rulingList) snapToGroups() rulingList {
	vertsHorzs, rulings := rl.vertsHorzs()
	if len(vertsHorzs) > 0 {
		vertsHorzs = vertsHorzs.snapToGroupsDirection()
	}
	if len(rulings) > 0 {
		rulings = rulings.snapToGroupsDirection()
	}
	vertsHorzs2 := append(vertsHorzs, rulings...)
	vertsHorzs2.log("snapToGroups")
	return vertsHorzs2
}

// String returns a description of `l`.
func (tl *textLine) String() string {
	return fmt.Sprintf("%.2f %6.2f fontsize=%.2f \"%s\"", tl.depth, tl.PdfRectangle, tl.fontSize, tl.text())
}

// String returns a description of `t`.
func (tt *textTable) String() string {
	return fmt.Sprintf("%d x %d %t", tt.w, tt.h, tt.exportable)
}

func (rl rulingList) primMinMax() (float64, float64) {
	lo, hi := rl[0].xy, rl[0].xy
	for _, r := range rl[1:] {
		if r.xy < lo {
			lo = r.xy
		} else if r.xy > hi {
			hi = r.xy
		}
	}
	return lo, hi
}

func granularize(sections []pathSection) {
	if coordGranularity < 0.0 {
		return
	}
	if debugRulings {
		common.Log.Info("granularize: %d subpath sections", len(sections))
	}
	for i, section := range sections {
		for j, sp := range section.subpaths {
			for k, point := range sp.points {
				sp.points[k] = transform.Point{X: roundToGrid(point.X), Y: roundToGrid(point.Y)}
				if debugRulings {
					point2 := sp.points[k]
					if !pointsEqual(point, point2) {
						pt := transform.Point{X: point2.X - point.X, Y: point2.Y - point.Y}
						fmt.Printf("%4d - %4d - %4d: %.2f → %.2f (%g)\n", i, j, k, point, point2, pt)
					}
				}
			}
		}
	}
}
func isZero(x float64) bool { return math.Abs(x) < zeroTolerance }
func fixCells(corridors map[int][]float64) {
	if len(corridors) <= 1 {
		return
	}
	keys := sortedIntKeys(corridors)
	if debugTables {
		common.Log.Info("fixCells: keys=%+v", keys)
	}
	var start, prevKey int
	for start, prevKey = range keys {
		if corridors[prevKey] != nil {
			break
		}
	}
	for i, key := range keys[start:] {
		cur := corridors[key]
		if cur == nil {
			continue
		}
		if debugTables {
			fmt.Printf("%4d: k0=%d k1=%d\n", start+i, prevKey, key)
		}
		prev := corridors[key]
		if prev[len(prev)-1] > cur[0] {
			prev[len(prev)-1] = cur[0]
			corridors[prevKey] = prev
		}
		prevKey = key
	}
}
func (ss *stateStack) push(state *textState) { saved := *state; *ss = append(*ss, &saved) }
func linesOfListItem(listV *list) []*textLine {
	for _, children := range listV.children {
		switch children.kind {
		case "LBody":
			if len(children.lines) != 0 {
				return children.lines
			}
			return linesOfListItem(children)
		case "Span":
			return children.lines
		case "InlineShape":
			return children.lines
		}
	}
	return nil
}

// ExtractFonts returns all font information from the page extractor, including
// font name, font type, the raw data of the embedded font file (if embedded), font descriptor and more.
//
// The argument `previousPageFonts` is used when trying to build a complete font catalog for multiple pages or the entire document.
// The entries from `previousPageFonts` are added to the returned result unless already included in the page, i.e. no duplicate entries.
//
// NOTE: If previousPageFonts is nil, all fonts from the page will be returned. Use it when building up a full list of fonts for a document or page range.
func (e *Extractor) ExtractFonts(previousPageFonts *PageFonts) (*PageFonts, error) {
	fonts := PageFonts{}
	err := fonts.extractPageResourcesToFont(e.resources)
	if err != nil {
		return nil, err
	}
	if previousPageFonts != nil {
		for _, font := range previousPageFonts.Fonts {
			if !containsFont(fonts.Fonts, font.FontName) {
				fonts.Fonts = append(fonts.Fonts, font)
			}
		}
	}
	return &PageFonts{Fonts: fonts.Fonts}, nil
}

func (wb *wordBag) makeRemovals() map[int]map[*textWord]struct{} {
	removals := make(map[int]map[*textWord]struct{}, len(wb.bins))
	for i := range wb.bins {
		removals[i] = make(map[*textWord]struct{})
	}
	return removals
}

func (tt *textTable) markCells() {
	for row := 0; row < tt.h; row++ {
		for col := 0; col < tt.w; col++ {
			para := tt.get(col, row)
			if para != nil {
				para.isTaken = true
			}
		}
	}
}

func corridorsString(corridors map[int][]float64) string {
	idxs := sortedIntKeys(corridors)
	parts := make([]string, len(corridors))
	for i, idx := range idxs {
		parts[i] = fmt.Sprintf("%d: %.2f", idx, corridors[idx])
	}
	return fmt.Sprintf("{%s}", strings.Join(parts, ", "))
}

const maxFormDepth = 20

func (wb *wordBag) endDeferredRemoval() {
	if wb.removed == nil {
		return
	}
	for i, removedCount := range wb.removedCount {
		if removedCount == 0 {
			continue
		}
		bin := wb.bins[i]
		if removedCount >= len(bin) {
			delete(wb.bins, i)
			continue
		}
		kept := 0
		for _, bin2 := range bin {
			if _, ok := wb.removed[bin2]; !ok {
				bin[kept] = bin2
				kept++
			}
		}
		for j := kept; j < len(bin); j++ {
			bin[j] = nil
		}
		wb.bins[i] = bin[:kept]
	}
	wb.removed = nil
	wb.removedCount = nil
	wb.binStart = nil
}

func (tl *textLine) markWordBoundaries() {
	minGap := wordBoundaryGapFactor * tl.fontSize
	for i, word := range tl.words[1:] {
		if horizontalGap(word, tl.words[i]) >= minGap {
			word.spaceBefore = true
		}
	}
}

func (pl paraList) findGridTables(tilings []gridTiling) []*textTable {
	if debugTables {
		common.Log.Info("findGridTables: %d paras", len(pl))
		for i, para := range pl {
			fmt.Printf("%4d: %s\n", i, para)
		}
	}
	var tables []*textTable
	for i, tiling := range tilings {
		tableGrid, used := pl.findTableGrid(tiling)
		if tableGrid != nil {
			tableGrid.log(fmt.Sprintf("findTableWithGrids: %d", i))
			tables = append(tables, tableGrid)
			tableGrid.markCells()
		}
		for para := range used {
			para.isTaken = true
		}
	}
	if debugTables {
		common.Log.Info("findGridTables: %d tables", len(tables))
	}
	return tables
}

func (ss *shapesState) cubicTo(x1, y1, x2, y2, x3, y3 float64) {
	if debugShapes {
		common.Log.Info("cubicTo:")
	}
	ss.addPoint(x3, y3)
}

func rowSpacing(rows []gridRow, charWidth float64) float64 {
	if len(rows) < 3 {
		return charWidth * defaultRowSpacingFactor
	}
	gaps := make([]float64, 0, len(rows))
	for i := 0; i+1 < len(rows); i++ {
		if gap := rows[i].y - rows[i+1].y; gap > 0 {
			gaps = append(gaps, gap)
		}
	}
	if len(gaps) < 3 {
		return charWidth * defaultRowSpacingFactor
	}
	sort.Float64s(gaps)
	return gaps[len(gaps)/2] * medianSpacingFactor
}

func (tma *TextMarkArray) getTextMarkAtOffset(offset int) *TextMark {
	for _, mark := range tma.marks {
		if mark.Offset == offset {
			return &mark
		}
	}
	return nil
}

func (iec *imageExtractContext) extractOperationImages(ops *contentstream.ContentStreamOperations, resources *model.PdfPageResources, gs *contentstream.GraphicsState) error {
	if iec.xObjectImages == nil {
		iec.xObjectImages = map[*core.PdfObjectStream]*model.Image{}
		iec.inlineImages = map[inlineImageKey]*model.Image{}
		iec.parsedStreams = map[*core.PdfObjectStream]*parsedImageStream{}
		iec.visitedPatterns = map[patternWalk]struct{}{}
	}
	if iec.options == nil {
		iec.options = &ImageExtractOptions{}
	}
	savedCTM := iec.ctm
	iec.ctm = transform.IdentityMatrix()
	if gs != nil && gs.CTM != (transform.Matrix{}) {
		iec.ctm = gs.CTM
	}
	defer func() { iec.ctm = savedCTM }()
	proc := contentstream.NewContentStreamProcessor(*ops)
	if gs != nil {
		proc.SetInitialGraphicsState(*gs)
	}
	proc.AddHandler(contentstream.HandlerConditionEnumAllOperands, "", iec.processOperand)
	return proc.Process(resources)
}

func groupWordsIntoRows(words []gridWord) []gridRow {
	if len(words) == 0 {
		return nil
	}
	sorted := make([]gridWord, len(words))
	copy(sorted, words)
	sort.Slice(sorted, func(n, n2 int) bool { return sorted[n].index < sorted[n2].index })
	var rows []gridRow
	cur := gridRow{words: []gridWord{sorted[0]}, y: sorted[0].ury}
	for i := 1; i < len(sorted); i++ {
		prev := sorted[i-1]
		w := sorted[i]
		nearIdx := w.index-prev.index <= maxIndexGap
		tol := prev.fontSize * rowHeightFactor
		sameY := math.Abs(w.ury-prev.ury) < tol
		if nearIdx && sameY {
			cur.words = append(cur.words, w)
			n := float64(len(cur.words))
			cur.y = cur.y*(n-1)/n + w.ury/n
		} else {
			rows = append(rows, cur)
			cur = gridRow{words: []gridWord{w}, y: w.ury}
		}
	}
	rows = append(rows, cur)
	return rows
}

func isNearlyHorizontal(a, b transform.Point) bool {
	dx := math.Abs(a.X - b.X)
	dy := math.Abs(a.Y - b.Y)
	return isRatioSmall(dy, dx)
}
func lineStartX(line *textLine) float64 { return line.words[0].Llx }
func (tw *textWord) toTextMarks(offset *int) []TextMark {
	var marks []TextMark
	for _, mark := range tw.marks {
		marks = appendMarkWithOffset(marks, offset, mark.ToTextMark())
	}
	return marks
}
func (tw *textWord) bbox() model.PdfRectangle { return tw.PdfRectangle }
func (pl paraList) applyTables(tables []*textTable) paraList {
	var paras paraList
	for _, table := range tables {
		paras = append(paras, table.newTablePara())
	}
	for _, para := range pl {
		if para.isTaken {
			continue
		}
		paras = append(paras, para)
	}
	return paras
}

func unionRects(a, b model.PdfRectangle) model.PdfRectangle {
	return model.PdfRectangle{Llx: math.Min(a.Llx, b.Llx), Lly: math.Min(a.Lly, b.Lly), Urx: math.Max(a.Urx, b.Urx), Ury: math.Max(a.Ury, b.Ury)}
}

func (gt gridTile) numBorders() int {
	count := 0
	if gt.llx {
		count++
	}
	if gt.urx {
		count++
	}
	if gt.lly {
		count++
	}
	if gt.ury {
		count++
	}
	return count
}

var bidiTypesLatin = []string{"BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "S", "B", "S", "WS", "B", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "B", "B", "B", "S", "WS", "ON", "ON", "ET", "ET", "ET", "ON", "ON", "ON", "ON", "ON", "ES", "CS", "ES", "CS", "CS", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "CS", "ON", "ON", "ON", "ON", "ON", "ON", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "ON", "ON", "ON", "ON", "ON", "ON", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "ON", "ON", "ON", "ON", "BN", "BN", "BN", "BN", "BN", "BN", "B", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "CS", "ON", "ET", "ET", "ET", "ET", "ON", "ON", "ON", "ON", "L", "ON", "ON", "BN", "ON", "ON", "ET", "ET", "EN", "EN", "ON", "L", "ON", "ON", "ON", "EN", "L", "ON", "ON", "ON", "ON", "ON", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "ON", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "ON", "L", "L", "L", "L", "L", "L", "L", "L"}

func sortedIntKeys(m map[int][]float64) []int {
	keys := make([]int, len(m))
	n := 0
	for i := range m {
		keys[n] = i
		n++
	}
	sort.Ints(keys)
	return keys
}

func makeGridWords(words []*textWord) []gridWord {
	out := make([]gridWord, 0, len(words))
	for i, word := range words {
		if len(word.text) == 0 {
			continue
		}
		if isDuplicateGridWord(out, word) {
			continue
		}
		out = append(out, gridWord{text: word.text, llx: word.Llx, urx: word.Urx, ury: word.Ury, fontSize: word.fontSize, index: i})
	}
	return dedupeGridWords(out)
}

const (
	markKindNone markKind = iota
	markKindStroke
	markKindFill
	markKindAugment
)

func isFlatSegment(p0, p1 transform.Point) bool {
	dx := math.Abs(p0.X - p1.X)
	dy := math.Abs(p0.Y - p1.Y)
	return isRatioSmall(dx, dy)
}

func (cc *compositeCell) updateBBox() {
	for _, para := range cc.paraList {
		cc.PdfRectangle = unionRects(cc.PdfRectangle, para.PdfRectangle)
	}
}

func (tt *textTable) reduceTiling(tiling gridTiling, tol float64) *textTable {
	rows := make([]int, 0, tt.h)
	cols := make([]int, 0, tt.w)
	llx := tiling.llx
	lly := tiling.lly
	for i := 0; i < tt.h; i++ {
		merge := i > 0 && math.Abs(lly[i-1]-lly[i]) < tol && tt.emptyCompositeRow(i)
		if !merge {
			rows = append(rows, i)
		}
	}
	for i := 0; i < tt.w; i++ {
		merge := i < tt.w-1 && math.Abs(llx[i+1]-llx[i]) < tol && tt.emptyCompositeColumn(i)
		if !merge {
			cols = append(cols, i)
		}
	}
	if len(rows) == tt.h && len(cols) == tt.w {
		return tt
	}
	reduced := textTable{exportable: tt.exportable, w: len(cols), h: len(rows), compositeCells: make(map[uint64]compositeCell, len(cols)*len(rows))}
	if debugTables {
		common.Log.Info("reduceTiling: %dx%d -> %dx%d", tt.w, tt.h, len(cols), len(rows))
		common.Log.Info("reducedCols: %+v", cols)
		common.Log.Info("reducedRows: %+v", rows)
	}
	for newRow, row := range rows {
		for newCol, col := range cols {
			composite, rect := tt.getComposite(col, row)
			if len(composite) == 0 {
				continue
			}
			if debugTables {
				fmt.Printf("  %2d, %2d (%2d, %2d) %q\n", newCol, newRow, col, row, truncate(composite.merge().text(), 50))
			}
			reduced.putComposite(newCol, newRow, composite, rect)
		}
	}
	return &reduced
}

type textTable struct {
	model.PdfRectangle
	w, h           int
	exportable     bool
	cells          map[uint64]*textPara
	compositeCells map[uint64]compositeCell
}

func replaceTextMarks(marks []*TextMarkArray, s string) error {
	offset := 0
	total := 0
	for _, mark := range marks {
		if !hasDirectObject(mark) {
			continue
		}
		for _, m := range mark.Elements() {
			total += len(m.Text)
		}
	}
	for _, mark := range marks {
		if !hasDirectObject(mark) {
			continue
		}
		objStr := mark.marks[0].DirectObject.String()
		if len(objStr) > 1 {
			err := replaceMultiByteString(mark, s, &offset, total)
			if err != nil {
				return err
			}
		} else if len(objStr) == 1 {
			err := replaceSingleByteString(mark, s, &offset, total)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func newGridTile(rect model.PdfRectangle, left, right, bottom, top *ruling) gridTile {
	llx := rect.Llx
	urx := rect.Urx
	lly := rect.Lly
	ury := rect.Ury
	return gridTile{PdfRectangle: rect, llx: left != nil && left.encloses(lly, ury), urx: right != nil && right.encloses(lly, ury), lly: bottom != nil && bottom.encloses(llx, urx), ury: top != nil && top.encloses(llx, urx)}
}

// String returns a description of `state`.
func (ts *textState) String() string {
	s := "[NOT SET]"
	if ts.tfont != nil {
		s = ts.tfont.BaseFont()
	}
	return fmt.Sprintf("tc=%.2f tw=%.2f tfs=%.2f font=%q", ts.tc, ts.tw, ts.tfs, s)
}

func splitWordBag(src *wordBag, pageHeight float64, vertRulings, horzRulings rulingList, noBreaks bool) []*wordBag {
	var out []*wordBag
	for _, binIdx := range src.depthIndexes() {
		grew := false
		for !src.empty(binIdx) {
			seedIdx := src.firstReadingIndex(binIdx)
			seed := src.firstWord(seedIdx)
			bag := newWordBag(seed, pageHeight, vertRulings, horzRulings)
			src.removeWord(seed, seedIdx)
			if debugSplitBags {
				common.Log.Info("firstWord ^^^^ %s", seed.String())
			}
			for loop := true; loop; loop = grew {
				grew = false
				adjGap := otherScanOffsetFactor * bag.maxFontSize
				readingGap := horizontalScanOffsetFactor * bag.maxFontSize
				if noBreaks {
					readingGap = math.MaxFloat64
				}
				depthGap := scanMarginFactor * bag.maxFontSize
				if debugSplitBags {
					common.Log.Info("paraWords depth %.2f - %.2f maxIntraDepthGap=%.2f maxIntraReadingGap=%.2f", bag.minDepth(), bag.maxDepth(), depthGap, readingGap)
				}
				if src.scanBand("vertical", bag, withGap(overlapsHorizontally, 0), bag.minDepth()-depthGap, bag.maxDepth()+depthGap, verticalGapFraction, false, false) > 0 {
					grew = true
				}
				if src.scanBand("horizontal", bag, withGap(overlapsHorizontally, readingGap), bag.minDepth(), bag.maxDepth(), horizontalGapFraction, false, false) > 0 {
					grew = true
				}
				if grew {
					continue
				}
				numPulled := src.scanBand("", bag, withGap(isAdjacentRight, adjGap), bag.minDepth(), bag.maxDepth(), otherGapFraction, true, false)
				if numPulled > 0 {
					numLines := (bag.maxDepth() - bag.minDepth()) / bag.maxFontSize
					if (numPulled > 1 && float64(numPulled) > 0.3*numLines) || numPulled <= 10 {
						if src.scanBand("other", bag, withGap(isAdjacentRight, adjGap), bag.minDepth(), bag.maxDepth(), otherGapFraction, false, true) > 0 {
							grew = true
						}
					}
				}
			}
			out = append(out, bag)
		}
	}
	return out
}

func (sp *subpath) close() {
	if !pointsEqual(sp.points[0], sp.last()) {
		sp.add(sp.points[0])
	}
	sp.closed = true
	sp.removeDuplicates()
}

func withGap(test func(*wordBag, *textWord, float64) bool, gap float64) func(*wordBag, *textWord) bool {
	return func(bag *wordBag, w *textWord) bool { return test(bag, w, gap) }
}

func isPatternColorspace(cs model.PdfColorspace) bool {
	_, ok := cs.(*model.PdfColorspaceSpecialPattern)
	return ok
}

func (tt TextTable) getCellInfo(mark TextMark) [][]int {
	for i, cell := range tt.Cells {
		for j := range cell {
			cellMarks := &cell[j].Marks
			if cellMarks.exists(mark) {
				return [][]int{{i}, {j}}
			}
		}
	}
	return nil
}

const maxCachedFonts = 10

func (tp *textPara) getListLines() []*textLine {
	var lines []*textLine
	bulletLines := linesWithBullets(tp.lines)
	for _, line := range tp.lines {
		b := line.words[0].text[0]
		if startsWithBulletByte(b) {
			lines = append(lines, line)
		}
	}
	lines = append(lines, bulletLines...)
	return lines
}

// NewWithOptions an Extractor instance for extracting content from the input PDF page with options.
func NewWithOptions(page *model.PdfPage, options *Options) (*Extractor, error) {
	const funcName = "extractor.NewWithOptions"
	contents, err := page.GetAllContentStreams()
	if err != nil {
		return nil, err
	}
	var treeRoot *model.StructTreeRoot
	rootObj, ok := page.GetStructTreeRoot()
	if !ok {
		common.Log.Debug("The pdf document is not tagged. StructTreeRoot doesn't exist.")
	} else {
		treeRoot, err = model.NewStructTreeRootFromPdfObject(*rootObj)
		if err != nil {
			return nil, fmt.Errorf("error loading struct tree root: %v", err)
		}
	}
	pageObj := page.GetContainingPdfObject()
	mediaBox, err := page.GetMediaBox()
	if err != nil {
		return nil, fmt.Errorf("extractor requires mediaBox. %v", err)
	}
	ex := &Extractor{contents: contents, resources: page.Resources, mediaBox: *mediaBox, cropBox: page.CropBox, structParentsKey: page.GetStructParentsKey(), fonts: map[string]fontEntry{}, parsedForms: map[*core.PdfObjectStream]parsedForm{}, formResults: map[formCacheKey]textResult{}, annotationResults: map[string]textResult{}, options: options, structTreeRoot: treeRoot, pageObject: pageObj}
	if ex.mediaBox.Llx > ex.mediaBox.Urx {
		common.Log.Info("MediaBox has X coordinates reversed. %.2f Fixing.", ex.mediaBox)
		ex.mediaBox.Llx, ex.mediaBox.Urx = ex.mediaBox.Urx, ex.mediaBox.Llx
	}
	if ex.mediaBox.Lly > ex.mediaBox.Ury {
		common.Log.Info("MediaBox has Y coordinates reversed. %.2f Fixing.", ex.mediaBox)
		ex.mediaBox.Lly, ex.mediaBox.Ury = ex.mediaBox.Ury, ex.mediaBox.Lly
	}
	if ex.options != nil {
		if ex.options.IncludeAnnotations {
			ex.annotations, err = page.GetAnnotations()
			if err != nil {
				common.Log.Debug("Error getting annotations: %v", err)
			}
		}
	}
	license.TrackUse(funcName)
	return ex, nil
}

type colorStateKey struct {
	kind       byte
	colorspace model.PdfColorspace
	pattern    string
	numComps   byte
	comps      [4]float64
	color      model.PdfColor
}

func makeSimpleText(marks []*textMark, pageSize model.PdfRectangle) string {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("makeSimpleText: %d elements pageSize=%.2f", len(marks), pageSize)
	}
	out := ""
	if len(marks) == 0 {
		return out
	}
	words := makeTextWords(marks, pageSize, true)
	if len(words) == 0 {
		return out
	}
	prevDepth := 0.0
	first := true
	line := ""
	for _, word := range words {
		fontSize := word.fontSize
		if fontSize > minFontSize {
			fontSize = minFontSize
		}
		if (word.depth-prevDepth > lineMergeFactor*fontSize && prevDepth != 0.0) || (prevDepth-word.depth > fontSize*10) {
			bidi := reorderBidi([]rune(line))
			line = bidi.text
			line += "\n"
			out += line
			line = ""
		} else {
			if !first {
				line += " "
			}
		}
		line += word.text
		first = false
		prevDepth = word.depth
	}
	if line != "" {
		bidi := reorderBidi([]rune(line))
		line = bidi.text
		line += "\n"
		out += line
	}
	return out
}

func renderGrid(rows []gridRow, minX, unit, spacing float64) string {
	var builder strings.Builder
	for k, row := range rows {
		sort.Slice(row.words, func(i, j int) bool { return row.words[i].llx < row.words[j].llx })
		var builder2 strings.Builder
		for i, word := range row.words {
			if i > 0 {
				builder2.WriteString(spacesForGap(word.llx-row.words[i-1].urx, unit))
			} else {
				builder2.WriteString(spacesForOffset(word.llx-minX, unit))
			}
			builder2.WriteString(word.text)
		}
		bidi := reorderBidi([]rune(builder2.String()))
		builder.WriteString(bidi.text)
		builder.WriteByte('\n')
		if k+1 < len(rows) {
			if rows[k].y-rows[k+1].y > spacing {
				builder.WriteByte('\n')
			}
		}
	}
	return builder.String()
}

func (pl paraList) addNeighbours() {
	splitLR := func(idxs []int, para *textPara) ([]*textPara, []*textPara) {
		left := make([]*textPara, 0, len(idxs)-1)
		right := make([]*textPara, 0, len(idxs)-1)
		for _, idx := range idxs {
			pl2 := pl[idx]
			if pl2.Urx <= para.Llx {
				left = append(left, pl2)
			} else if pl2.Llx >= para.Urx {
				right = append(right, pl2)
			}
		}
		return left, right
	}
	splitUD := func(idxs []int, para *textPara) ([]*textPara, []*textPara) {
		above := make([]*textPara, 0, len(idxs)-1)
		below := make([]*textPara, 0, len(idxs)-1)
		for _, idx := range idxs {
			pl2 := pl[idx]
			if pl2.Ury <= para.Lly {
				below = append(below, pl2)
			} else if pl2.Lly >= para.Ury {
				above = append(above, pl2)
			}
		}
		return above, below
	}
	nbrs := pl.yNeighbours(yNeighbourTolerance)
	for _, para := range pl {
		idxs := nbrs[para]
		if len(idxs) == 0 {
			continue
		}
		left, right := splitLR(idxs, para)
		if len(left) == 0 && len(right) == 0 {
			continue
		}
		if len(left) > 0 {
			nearestLeft := left[0]
			for _, cand := range left[1:] {
				if cand.Urx >= nearestLeft.Urx {
					nearestLeft = cand
				}
			}
			for _, cand := range left {
				if cand != nearestLeft && cand.Urx > nearestLeft.Llx {
					nearestLeft = nil
					break
				}
			}
			if nearestLeft != nil && yOverlap(para.PdfRectangle, nearestLeft.PdfRectangle) {
				para.left = nearestLeft
			}
		}
		if len(right) > 0 {
			nearestRight := right[0]
			for _, cand := range right[1:] {
				if cand.Llx <= nearestRight.Llx {
					nearestRight = cand
				}
			}
			for _, cand := range right {
				if cand != nearestRight && cand.Llx < nearestRight.Urx {
					nearestRight = nil
					break
				}
			}
			if nearestRight != nil && yOverlap(para.PdfRectangle, nearestRight.PdfRectangle) {
				para.right = nearestRight
			}
		}
	}
	nbrs = pl.xNeighbours(xNeighbourTolerance)
	for _, para := range pl {
		idxs := nbrs[para]
		if len(idxs) == 0 {
			continue
		}
		above, below := splitUD(idxs, para)
		if len(above) == 0 && len(below) == 0 {
			continue
		}
		if len(below) > 0 {
			nearestBelow := below[0]
			for _, cand := range below[1:] {
				if cand.Ury >= nearestBelow.Ury {
					nearestBelow = cand
				}
			}
			for _, cand := range below {
				if cand != nearestBelow && cand.Ury > nearestBelow.Lly {
					nearestBelow = nil
					break
				}
			}
			if nearestBelow != nil && xOverlap(para.PdfRectangle, nearestBelow.PdfRectangle) {
				para.below = nearestBelow
			}
		}
		if len(above) > 0 {
			nearestAbove := above[0]
			for _, cand := range above[1:] {
				if cand.Lly <= nearestAbove.Lly {
					nearestAbove = cand
				}
			}
			for _, cand := range above {
				if cand != nearestAbove && cand.Lly < nearestAbove.Ury {
					nearestAbove = nil
					break
				}
			}
			if nearestAbove != nil && xOverlap(para.PdfRectangle, nearestAbove.PdfRectangle) {
				para.above = nearestAbove
			}
		}
	}
	for _, para := range pl {
		if para.left != nil && para.left.right != para {
			para.left = nil
		}
		if para.above != nil && para.above.below != para {
			para.above = nil
		}
		if para.right != nil && para.right.left != para {
			para.right = nil
		}
		if para.below != nil && para.below.above != para {
			para.below = nil
		}
	}
}
func bboxDepth(b bounded) float64 { return -b.bbox().Lly }
func (wb *wordBag) depthBand(lo, hi float64) []int {
	if len(wb.bins) == 0 {
		return nil
	}
	return wb.depthRange(wb.getDepthIdx(lo), wb.getDepthIdx(hi))
}

func refineGridRows(rows []gridRow, charWidth float64) []gridRow {
	var out []gridRow
	for _, row := range rows {
		if len(row.words) < 4 {
			out = append(out, row)
			continue
		}
		sort.Slice(row.words, func(n, n2 int) bool { return row.words[n].llx < row.words[n2].llx })
		maxGap := 0.0
		splitIdx := -1
		for i := 1; i < len(row.words); i++ {
			gap := row.words[i].llx - row.words[i-1].urx
			if gap > maxGap {
				maxGap = gap
				splitIdx = i
			}
		}
		if maxGap < charWidth*splitGapFactor || splitIdx <= 0 {
			out = append(out, row)
			continue
		}
		yLeft, yRight := 0.0, 0.0
		for _, word := range row.words[:splitIdx] {
			yLeft += word.ury
		}
		yLeft /= float64(splitIdx)
		for _, word := range row.words[splitIdx:] {
			yRight += word.ury
		}
		yRight /= float64(len(row.words) - splitIdx)
		dy := math.Abs(yLeft - yRight)
		if dy < charWidth*mergeGapFactor {
			out = append(out, row)
			continue
		}
		left := make([]gridWord, splitIdx)
		copy(left, row.words[:splitIdx])
		right := make([]gridWord, len(row.words)-splitIdx)
		copy(right, row.words[splitIdx:])
		out = append(out, gridRow{words: left, y: yLeft})
		out = append(out, gridRow{words: right, y: yRight})
	}
	return out
}

func (rl rulingList) comp(i, j int) bool {
	a, b := rl[i], rl[j]
	ka, kb := a.kind, b.kind
	if ka != kb {
		return ka > kb
	}
	if ka == rulingNone {
		return false
	}
	orient := func(flag bool) bool {
		if ka == rulingHorz {
			return flag
		}
		return !flag
	}
	va, vb := a.xy, b.xy
	if va != vb {
		return orient(va > vb)
	}
	va, vb = a.lo, b.lo
	if va != vb {
		return orient(va < vb)
	}
	return orient(a.hi < b.hi)
}

func (lr lineRuling) asRuling() (*ruling, bool) {
	r := ruling{kind: lr.kind, Color: lr.Color, mark: markKindStroke}
	switch lr.kind {
	case rulingVert:
		r.xy = lr.xMean()
		r.lo = math.Min(lr.p0.Y, lr.p1.Y)
		r.hi = math.Max(lr.p0.Y, lr.p1.Y)
	case rulingHorz:
		r.xy = lr.yMean()
		r.lo = math.Min(lr.p0.X, lr.p1.X)
		r.hi = math.Max(lr.p0.X, lr.p1.X)
	default:
		common.Log.Error("bad primary kind=%d", lr.kind)
		return nil, false
	}
	return &r, true
}

func (tp *textPara) text() string {
	buf := new(bytes.Buffer)
	tp.writeText(buf, &textContext{})
	return buf.String()
}

type bounded interface{ bbox() model.PdfRectangle }

func (to *textObject) newTextMark(text string, trm transform.Matrix, end transform.Point, spaceWidth float64, font *model.PdfFont, charSpacing float64, fill, stroke color.Color, obj core.PdfObject, objStrs []string, index int, mcid int) (textMark, bool) {
	angle := trm.Angle()
	orient := roundToMultiple(angle, angleRoundingStep)
	var fontSize float64
	if orient%180 != 90 {
		fontSize = trm.ScalingFactorY()
	} else {
		fontSize = trm.ScalingFactorX()
	}
	start := translationPoint(trm)
	origBBox := model.PdfRectangle{Llx: start.X, Lly: start.Y, Urx: end.X, Ury: end.Y}
	switch orient % 360 {
	case 90:
		origBBox.Urx -= fontSize
	case 180:
		origBBox.Ury -= fontSize
	case 270:
		origBBox.Urx += fontSize
	case 0:
		origBBox.Ury += fontSize
	default:
		orient = 0
		origBBox.Ury += fontSize
	}
	if origBBox.Llx > origBBox.Urx {
		origBBox.Llx, origBBox.Urx = origBBox.Urx, origBBox.Llx
	}
	if origBBox.Lly > origBBox.Ury {
		origBBox.Lly, origBBox.Ury = origBBox.Ury, origBBox.Lly
	}
	inPage := true
	if to.extractor.mediaBox.Width() > 0 {
		clipped, overlap := intersectRects(origBBox, to.extractor.mediaBox)
		if !overlap {
			inPage = false
			common.Log.Debug("Text mark outside page. bbox=%g mediaBox=%g text=%q", origBBox, to.extractor.mediaBox, text)
		}
		origBBox = clipped
	}
	bbox := origBBox
	mb := to.extractor.mediaBox
	switch orient % 360 {
	case 90:
		mb.Urx, mb.Ury = mb.Ury, mb.Urx
		bbox = model.PdfRectangle{Llx: mb.Urx - origBBox.Ury, Urx: mb.Urx - origBBox.Lly, Lly: origBBox.Llx, Ury: origBBox.Urx}
	case 180:
		bbox = model.PdfRectangle{Llx: mb.Urx - origBBox.Llx, Urx: mb.Urx - origBBox.Urx, Lly: mb.Ury - origBBox.Lly, Ury: mb.Ury - origBBox.Ury}
	case 270:
		mb.Urx, mb.Ury = mb.Ury, mb.Urx
		bbox = model.PdfRectangle{Llx: origBBox.Ury, Urx: origBBox.Lly, Lly: mb.Ury - origBBox.Llx, Ury: mb.Ury - origBBox.Urx}
	}
	if bbox.Llx > bbox.Urx {
		bbox.Llx, bbox.Urx = bbox.Urx, bbox.Llx
	}
	if bbox.Lly > bbox.Ury {
		bbox.Lly, bbox.Ury = bbox.Ury, bbox.Lly
	}
	mark := textMark{text: text, PdfRectangle: bbox, origBBox: origBBox, font: font, fontSize: fontSize, charSpacing: charSpacing, trm: trm, end: end, orientation: orient, fillColor: fill, strokeColor: stroke, directObject: obj, objString: objStrs, Th: to.state.th, Tw: to.state.tw, mcid: mcid, index: index}
	if debugWords {
		common.Log.Info("newTextMark: start=%.2f end=%.2f %s", start, end, mark.String())
	}
	return mark, inPage
}

func newWordBag(word *textWord, pageHeight float64, vertRulings, horzRulings rulingList) *wordBag {
	idx := depthBinIndex(word.depth)
	words := []*textWord{word}
	bag := wordBag{bins: map[int][]*textWord{idx: words}, PdfRectangle: word.PdfRectangle, maxFontSize: word.fontSize, pageHeight: pageHeight, vertRulings: vertRulings, horzRulings: horzRulings}
	return &bag
}

func isBlank(s string) bool {
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

var bulletChars = []string{"❖", "➢", "✓", "•", "\uf0a7", "□", "−", "■", "▪", "o"}

type intSet map[int]struct{}

func spacesForOffset(offset, unit float64) string {
	if offset <= unit {
		return ""
	}
	n := min(int(offset/unit+0.5), maxOffsetSpaces)
	return strings.Repeat(" ", n)
}

func groupMarksByObject(marks *TextMarkArray) []*TextMarkArray {
	elems := marks.Elements()
	numMarks := len(elems)
	var prevObj core.PdfObject
	groups := []*TextMarkArray{}
	cur := &TextMarkArray{}
	idx := -1
	for i, m := range elems {
		obj := m.DirectObject
		idx = m.Index
		if obj == nil {
			adj := adjacentObjStringIndex(marks, i)
			if prevObj != nil {
				if adj == -1 || adj > i {
					groups = append(groups, cur)
					cur = &TextMarkArray{}
				}
			}
		} else if prevObj == nil {
			if idx == 0 && i > 0 {
				groups = append(groups, cur)
				cur = &TextMarkArray{}
			}
		} else {
			if obj != prevObj {
				groups = append(groups, cur)
				cur = &TextMarkArray{}
			}
		}
		prevObj = obj
		cur.Append(m)
		if i == (numMarks - 1) {
			groups = append(groups, cur)
		}
	}
	return groups
}

func (ss *shapesState) clearPath() {
	ss.subpaths = nil
	ss.fresh = false
	if debugShapes {
		common.Log.Info("CLEAR: ss=%s", ss)
	}
}

func twoFloatOperands(objs []core.PdfObject) (x, x2 float64, err error) {
	if len(objs) != 2 {
		return 0, 0, fmt.Errorf("invalid number of params: %d", len(objs))
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objs)
	if err != nil {
		return 0, 0, err
	}
	return numbersAsFloat[0], numbersAsFloat[1], nil
}

type textObject struct {
	extractor       *Extractor
	resources       *model.PdfPageResources
	gs              contentstream.GraphicsState
	state           *textState
	savedStates     *stateStack
	tm              transform.Matrix
	tlm             transform.Matrix
	marks           []*textMark
	unsupportedFont bool
}

// String returns a description of `v`.
func (r *ruling) String() string {
	if r.kind == rulingNone {
		return "NOT RULING"
	}
	primAxis, secAxis := "x", "y"
	if r.kind == rulingHorz {
		primAxis, secAxis = "y", "x"
	}
	widthStr := ""
	if r.width != 0.0 {
		widthStr = fmt.Sprintf(" width=%.2f", r.width)
	}
	return fmt.Sprintf("%10s %s=%6.2f %s=%6.2f - %6.2f (%6.2f) %s %v%s", r.kind, primAxis, r.xy, secAxis, r.lo, r.hi, r.hi-r.lo, r.mark, r.Color, widthStr)
}

func (tt *textTable) newTablePara() *textPara {
	rect := tt.computeBbox()
	para := &textPara{PdfRectangle: rect, eBBox: rect, table: tt}
	if debugTables {
		common.Log.Info("newTablePara: %s", para)
	}
	return para
}

func (rl rulingList) tidied(label string) rulingList {
	uniq := rl.removeDuplicates()
	uniq.log("uniques")
	snapped := uniq.snapToGroups()
	if snapped == nil {
		return nil
	}
	snapped.sort()
	if debugRulings {
		common.Log.Info("tidied: %q vecs=%d uniques=%d coalesced=%d", label, len(rl), len(uniq), len(snapped))
	}
	snapped.log("coalesced")
	return snapped
}

type event struct {
	pos     float64
	isStart bool
	idx     int
}

// Text gets the extracted text contained in `l`.
func (l *list) Text() string {
	b := &strings.Builder{}
	indent := ""
	writeListText(l, b, &indent)
	return b.String()
}

func makeFillRulings(sections []pathSection) rulingList {
	granularize(sections)
	if debugRulings {
		common.Log.Info("makeFillRulings: %d fills", len(sections))
	}
	var rulings rulingList
	for _, section := range sections {
		for _, path := range section.subpaths {
			if !path.isQuadrilateral() {
				if debugRulings {
					common.Log.Error("!isQuadrilateral: %s", path)
				}
				continue
			}
			if r, ok := path.makeRectRuling(section.Color); ok {
				rulings = append(rulings, r)
			} else {
				if debugRectRulings {
					common.Log.Error("!makeRectRuling: %s", path)
				}
			}
		}
	}
	if debugRulings {
		common.Log.Info("makeFillRulings: %s", rulings.String())
	}
	return rulings
}

type textState struct {
	tc                float64
	tw                float64
	th                float64
	tl                float64
	tfs               float64
	tmode             RenderMode
	trise             float64
	tfont             *model.PdfFont
	rect              model.PdfRectangle
	numChars          int
	numMisparsedChars int
}

func (cc compositeCell) split(ys, xs []float64) *textTable {
	numRows := len(ys) + 1
	numCols := len(xs) + 1
	if debugTables {
		common.Log.Info("compositeCell.split: %d x %d\n\tcomposite=%s\n"+"\trowCorridors=%6.2f\n\tcolCorridors=%6.2f", numCols, numRows, cc, ys, xs)
		fmt.Printf("    %d paras\n", len(cc.paraList))
		for i, para := range cc.paraList {
			fmt.Printf("%4d: %s\n", i, para.String())
		}
		fmt.Printf("    %d lines\n", len(cc.lines()))
		for i, line := range cc.lines() {
			fmt.Printf("%4d: %s\n", i, line)
		}
	}
	ys = addInterval(ys, cc.Ury, cc.Lly)
	xs = addInterval(xs, cc.Llx, cc.Urx)
	cells := make(map[uint64]*textPara, numCols*numRows)
	table := textTable{w: numCols, h: numRows, cells: cells}
	paras := cc.paraList
	sort.Slice(paras, func(i, j int) bool {
		a, b := paras[i], paras[j]
		la, lb := a.Lly, b.Lly
		if la != lb {
			return la < lb
		}
		return a.Llx < b.Llx
	})
	rects := make(map[uint64]model.PdfRectangle, numCols*numRows)
	for row, lly := range ys[1:] {
		ury := ys[row]
		for col, urx := range xs[1:] {
			llx := xs[col]
			rects[cellKey(col, row)] = model.PdfRectangle{Llx: llx, Urx: urx, Lly: lly, Ury: ury}
		}
	}
	if debugTables {
		common.Log.Info("compositeCell.split: rects")
		fmt.Printf("    ")
		for i := 0; i < numCols; i++ {
			fmt.Printf("%30d, ", i)
		}
		fmt.Println()
		for i := 0; i < numRows; i++ {
			fmt.Printf("  %2d:", i)
			for j := 0; j < numCols; j++ {
				fmt.Printf("%6.2f, ", rects[cellKey(j, i)])
			}
			fmt.Println()
		}
	}
	find := func(line *textLine) (int, int) {
		for r := 0; r < numRows; r++ {
			for c := 0; c < numCols; c++ {
				if rectContains(rects[cellKey(c, r)], line.PdfRectangle) {
					return c, r
				}
			}
		}
		return -1, -1
	}
	byCell := make(map[uint64][]*textLine, numCols*numRows)
	for _, line := range paras.lines() {
		c, r := find(line)
		if c < 0 {
			continue
		}
		byCell[cellKey(c, r)] = append(byCell[cellKey(c, r)], line)
	}
	for row := 0; row < len(ys)-1; row++ {
		ury := ys[row]
		lly := ys[row+1]
		for col := 0; col < len(xs)-1; col++ {
			llx := xs[col]
			urx := xs[col+1]
			rect := model.PdfRectangle{Llx: llx, Urx: urx, Lly: lly, Ury: ury}
			cellLines := byCell[cellKey(col, row)]
			if len(cellLines) == 0 {
				continue
			}
			para := newTextPara(rect, cellLines)
			table.put(col, row, para)
		}
	}
	return &table
}

type wordBag struct {
	model.PdfRectangle
	maxFontSize              float64
	vertRulings, horzRulings rulingList
	pageHeight               float64
	bins                     map[int][]*textWord
	removed                  map[*textWord]struct{}
	removedCount             map[int]int
	binStart                 map[int]int
}

func (pf *PageFonts) extractPageResourcesToFont(resources *model.PdfPageResources) error {
	if resources.Font == nil {
		return errors.New(msgNoFontFound)
	}
	fontDict, ok := core.GetDict(resources.Font)
	if !ok {
		return errors.New(msgFontConvert)
	}
	for _, key := range fontDict.Keys() {
		var (
			hasToUnicode = true
			data         []byte
			fileName     string
		)
		fontObj, ok2 := resources.GetFontByName(key)
		if !ok2 {
			return errors.New(msgFontPropsNotFound)
		}
		pdfFont, err := model.NewPdfFontFromPdfObject(fontObj)
		if err != nil {
			return err
		}
		descriptor := pdfFont.FontDescriptor()
		fontName := pdfFont.FontDescriptor().FontName.String()
		subtype := pdfFont.Subtype()
		if containsFont(pf.Fonts, fontName) {
			continue
		}
		if len(pdfFont.ToUnicode()) == 0 {
			hasToUnicode = false
		}
		if descriptor.FontFile != nil {
			if stream, ok3 := core.GetStream(descriptor.FontFile); ok3 {
				data, err = core.DecodeStream(stream)
				if err != nil {
					return err
				}
				fileName = fontName + ".pfb"
			}
		} else if descriptor.FontFile2 != nil {
			if stream, ok3 := core.GetStream(descriptor.FontFile2); ok3 {
				data, err = core.DecodeStream(stream)
				if err != nil {
					return err
				}
				fileName = fontName + ".ttf"
			}
		} else if descriptor.FontFile3 != nil {
			if stream, ok3 := core.GetStream(descriptor.FontFile3); ok3 {
				data, err = core.DecodeStream(stream)
				if err != nil {
					return err
				}
				fileName = fontName + ".cff"
			}
		}
		if len(fileName) < 1 {
			common.Log.Debug(msgFontStreamInvalid)
		}
		font := Font{FontName: fontName, PdfFont: pdfFont, IsCID: pdfFont.IsCID(), IsSimple: pdfFont.IsSimple(), ToUnicode: hasToUnicode, FontType: subtype, FontData: data, FontFileName: fileName, FontDescriptor: descriptor}
		pf.Fonts = append(pf.Fonts, font)
	}
	return nil
}

func (rl rulingList) asTiling() gridTiling {
	if debugGrids {
		common.Log.Info("rulingList.asTiling: vecs=%d ==================+++ ==================", len(rl))
	}
	for i, r := range rl[1:] {
		rl2 := rl[i]
		if rl2.alignsPrimary(r) && rl2.alignsSec(r) {
			common.Log.Error("asTiling: Duplicate rulings.\n\tv=%s\n\tw=%s", r, rl2)
		}
	}
	rl.sortStrict()
	rl.log("snapped")
	verts, horzs := rl.vertsHorzs()
	xs := verts.primaries()
	ys := horzs.primaries()
	numCols := len(xs) - 1
	numRows := len(ys) - 1
	if numCols == 0 || numRows == 0 {
		return gridTiling{}
	}
	rect := model.PdfRectangle{Llx: xs[0], Urx: xs[numCols], Lly: ys[0], Ury: ys[numRows]}
	if debugGrids {
		common.Log.Info("rulingList.asTiling: verts=%d", len(verts))
		for i, vert := range verts {
			fmt.Printf("%4d: %s\n", i, vert)
		}
		common.Log.Info("rulingList.asTiling: horzs=%d", len(horzs))
		for i, horz := range horzs {
			fmt.Printf("%4d: %s\n", i, horz)
		}
		common.Log.Info("rulingList.asTiling:  wxh=%dx%d\n\tllx=%.2f\n\tlly=%.2f", numCols, numRows, xs, ys)
	}
	tiles := make([]gridTile, numCols*numRows)
	for row := numRows - 1; row >= 0; row-- {
		y0 := ys[row]
		y1 := ys[row+1]
		for col := 0; col < numCols; col++ {
			x0 := xs[col]
			x1 := xs[col+1]
			left := verts.findPrimSec(x0, y0)
			right := verts.findPrimSec(x1, y0)
			bottom := horzs.findPrimSec(y0, x0)
			top := horzs.findPrimSec(y1, x0)
			cellRect := model.PdfRectangle{Llx: x0, Urx: x1, Lly: y0, Ury: y1}
			tile := newGridTile(cellRect, left, right, bottom, top)
			tiles[row*numCols+col] = tile
			if debugGrids {
				fmt.Printf("  x=%2d y=%2d: %s %6.2f x %6.2f\n", col, row, tile.String(), tile.Width(), tile.Height())
			}
		}
	}
	if debugGrids {
		common.Log.Info("rulingList.asTiling: coalesce horizontal. %6.2f", rect)
	}
	rowTiles := make([]map[float64]gridTile, numRows)
	for row := numRows - 1; row >= 0; row-- {
		if debugGrids {
			fmt.Printf("  y=%2d\n", row)
		}
		rowTiles[row] = make(map[float64]gridTile, numCols)
		for col := 0; col < numCols; col++ {
			t := tiles[row*numCols+col]
			if debugGrids {
				fmt.Printf("  %4d: %s\n", col, t)
			}
			if !t.llx {
				continue
			}
			last := col
			for k := col + 1; !t.urx && k < numCols; k++ {
				next := tiles[row*numCols+k]
				t.Urx = next.Urx
				t.ury = t.ury || next.ury
				t.lly = t.lly || next.lly
				t.urx = next.urx
				if debugGrids {
					fmt.Printf("   %4d: %s → %s\n", k, next, t)
				}
				last = k
			}
			if debugGrids {
				fmt.Printf("   %2d - %2d → %s\n", col, last, t)
			}
			col = last
			rowTiles[row][t.Llx] = t
		}
	}
	merged := make(map[float64]map[float64]gridTile, numRows)
	used := make(map[float64]map[float64]struct{}, numRows)
	for row := numRows - 1; row >= 0; row-- {
		lly := tiles[row*numCols].Lly
		merged[lly] = make(map[float64]gridTile, numCols)
		used[lly] = make(map[float64]struct{}, numCols)
	}
	if debugGrids {
		common.Log.Info("rulingList.asTiling: coalesce vertical. %6.2f", rect)
	}
	for row := numRows - 1; row >= 0; row-- {
		lly := tiles[row*numCols].Lly
		rowMap := rowTiles[row]
		if debugGrids {
			fmt.Printf("  y=%2d\n", row)
		}
		for _, llx := range sortedTileKeys(rowMap) {
			if _, ok := used[lly][llx]; ok {
				continue
			}
			t := rowMap[llx]
			if debugGrids {
				fmt.Printf("     v0=%s\n", t.String())
			}
			for j := row - 1; j >= 0; j-- {
				if t.lly {
					break
				}
				other := rowTiles[j]
				nt, ok := other[llx]
				if !ok {
					break
				}
				if nt.Urx != t.Urx {
					break
				}
				t.lly = nt.lly
				t.Lly = nt.Lly
				if debugGrids {
					fmt.Printf("        v=%s v0=%s\n", nt.String(), t.String())
				}
				used[nt.Lly][nt.Llx] = struct{}{}
			}
			if row == 0 {
				t.lly = true
			}
			if t.complete() {
				merged[lly][llx] = t
			}
		}
	}
	tiling := gridTiling{PdfRectangle: rect, llx: tileColumnKeys(merged), lly: tileRowKeys(merged), tiles: merged}
	tiling.log("Created")
	return tiling
}

func removeWordAt(words []*textWord, idx int) []*textWord {
	numWords := len(words)
	copy(words[idx:], words[idx+1:])
	return words[:numWords-1]
}

func rectRulingKind(rect model.PdfRectangle) rulingKind {
	width := rect.Width()
	height := rect.Height()
	if width > height {
		if width >= minRulingLength {
			return rulingHorz
		}
	} else {
		if height >= minRulingLength {
			return rulingVert
		}
	}
	return rulingNone
}

func newTextObject(ex *Extractor, resources *model.PdfPageResources, gs contentstream.GraphicsState, state *textState, stack *stateStack) *textObject {
	return &textObject{extractor: ex, resources: resources, gs: gs, savedStates: stack, state: state, tm: transform.IdentityMatrix(), tlm: transform.IdentityMatrix()}
}

func kindFromDeltas(dx, dy, minLen float64) rulingKind {
	if dx >= minLen && isRatioSmall(dy, dx) {
		return rulingHorz
	}
	if dy >= minLen && isRatioSmall(dx, dy) {
		return rulingVert
	}
	return rulingNone
}

func appendMetaMark(marks []TextMark, offset *int, text string) []TextMark {
	mark := metaMark
	mark.Text = text
	return appendMarkWithOffset(marks, offset, mark)
}

func translationPoint(m transform.Matrix) transform.Point {
	tx, ty := m.Translation()
	return transform.Point{X: tx, Y: ty}
}

func (tp *textPara) depth() float64 {
	if tp.isEmpty {
		return -1.0
	}
	if len(tp.lines) > 0 {
		return tp.lines[0].depth
	}
	return tp.table.depth()
}

func (pl paraList) llyRange(idxs []int, lo, hi float64) []int {
	n := len(pl)
	if hi < pl[idxs[0]].Lly || lo > pl[idxs[n-1]].Lly {
		return nil
	}
	start := sort.Search(n, func(i int) bool { return pl[idxs[i]].Lly >= lo })
	end := sort.Search(n, func(i int) bool { return pl[idxs[i]].Lly > hi })
	return idxs[start:end]
}

func (wb *wordBag) firstReadingIndex(idx int) int {
	fontSize := wb.firstWord(idx).fontSize
	binBottom := float64(idx+1) * depthBinWidth
	limit := binBottom + readingOrderFontFactor*fontSize
	best := idx
	for _, bi := range wb.depthBand(binBottom, limit) {
		w := wb.firstWord(bi)
		if w == nil {
			continue
		}
		if compareLlx(w, wb.firstWord(best)) < 0 {
			best = bi
		}
	}
	return best
}

type parsedImageStream struct {
	ops       *contentstream.ContentStreamOperations
	resources *model.PdfPageResources
	matrix    transform.Matrix
}

func (tp *textPara) toCellTextMarks(offset *int, ctx *textContext) []TextMark {
	var marks []TextMark
	for i, line := range tp.lines {
		lineMarks := line.toTextMarks(offset)
		dehyphenate := !ctx.isDehyphenationDisabled() && line.endsInHyphen() && i != len(tp.lines)-1
		if dehyphenate {
			lineMarks = trimLastMarkRune(lineMarks, offset)
		}
		marks = append(marks, lineMarks...)
		if !(dehyphenate || i == len(tp.lines)-1) {
			marks = appendMetaMark(marks, offset, separatorFor(line.depth, tp.lines[i+1].depth))
		}
	}
	return marks
}

func (ss *stateStack) top() *textState {
	if ss.empty() {
		return nil
	}
	return (*ss)[ss.size()-1]
}

type fontEntry struct {
	font     *model.PdfFont
	lastUsed int64
}

func (wb *wordBag) sort() {
	for _, bin := range wb.bins {
		sort.Slice(bin, func(i, j int) bool { return compareLlx(bin[i], bin[j]) < 0 })
	}
}

var bareNumberRegexp = regexp.MustCompile("^\\s*(\\d+\\.?|[Iiv]+)\\s*\\)?$")

func (rl rulingList) sort() { sort.Slice(rl, rl.comp) }
func (tt *textTable) toTextTable(ctx *textContext) TextTable {
	if debugTables {
		common.Log.Info("toTextTable: %d x %d", tt.w, tt.h)
	}
	cells := make([][]TableCell, tt.h)
	for i := 0; i < tt.h; i++ {
		cells[i] = make([]TableCell, tt.w)
		for j := 0; j < tt.w; j++ {
			para := tt.get(j, i)
			if para == nil {
				continue
			}
			sortLinesByDepth(para.lines)
			if debugTables {
				fmt.Printf("%4d %2d: %s\n", j, i, para)
			}
			cells[i][j].Text = para.text()
			offset := 0
			cells[i][j].Marks.marks = para.toTextMarks(&offset, ctx)
		}
	}
	table := TextTable{
		W: tt.w, H: tt.h, Cells: cells,
		PdfRectangle: tt.bbox(),
	}
	return table
}

func (pt *PageText) computeViews() {
	if pt.options.mode == ExtractionModePlain {
		pt.text = pt.getText()
		return
	}
	if pt.options.mode == ExtractionModeGrid {
		pt.text = pt.makeGridText(pt.marks)
		return
	}
	paras := pt.getParagraphs()
	buffer := new(bytes.Buffer)
	ctx := &textContext{disableDehyphenation: pt.options.disableDehyphenation}
	paras.writeText(buffer, ctx)
	pt.text = buffer.String()
	pt.cachedMarksText = pt.text
	pt.viewMarks = paras.toTextMarks(ctx)
	pt.tables = paras.tables(ctx)
	if debugTables {
		common.Log.Info("computeViews: tables=%d", len(pt.tables))
	}
}

// BBox returns the smallest axis-aligned rectangle that encloses all the TextMarks in `ma`.
func (tma *TextMarkArray) BBox() (model.PdfRectangle, bool) {
	var rect model.PdfRectangle
	found := false
	for _, mark := range tma.marks {
		if mark.Meta || isBlank(mark.Text) {
			continue
		}
		if found {
			rect = unionRects(rect, mark.BBox)
		} else {
			rect = mark.BBox
			found = true
		}
	}
	return rect, found
}

type lineRuling struct {
	kind rulingKind
	mark markKind
	color.Color
	p0, p1 transform.Point
}

func (ss *shapesState) closePath() {
	if ss.fresh {
		ss.subpaths = append(ss.subpaths, newSubpath(ss.currentPoint))
		ss.fresh = false
	} else if len(ss.subpaths) == 0 {
		if debugShapes {
			common.Log.Debug("closePath with no path")
		}
		ss.fresh = false
		return
	}
	ss.subpaths[len(ss.subpaths)-1].close()
	if debugShapes {
		common.Log.Info("closePath: %s", ss)
	}
}

// String returns a human readable description of `vecs`.
func (rl rulingList) String() string {
	if len(rl) == 0 {
		return "{ EMPTY }"
	}
	vertsHorzs, rulings := rl.vertsHorzs()
	numVertsHorzs := len(vertsHorzs)
	numRulings := len(rulings)
	if numVertsHorzs == 0 || numRulings == 0 {
		return fmt.Sprintf("{%d x %d}", numVertsHorzs, numRulings)
	}
	rect := model.PdfRectangle{Llx: vertsHorzs[0].xy, Urx: vertsHorzs[numVertsHorzs-1].xy, Lly: rulings[numRulings-1].xy, Ury: rulings[0].xy}
	return fmt.Sprintf("{%d x %d: %6.2f}", numVertsHorzs, numRulings, rect)
}

func (ss *shapesState) establishSubpath() *subpath {
	lastpointEstablished, ok := ss.lastpointEstablished()
	if !ok {
		ss.subpaths = append(ss.subpaths, newSubpath(lastpointEstablished))
	}
	if len(ss.subpaths) == 0 {
		return nil
	}
	ss.fresh = false
	return ss.subpaths[len(ss.subpaths)-1]
}

func linesBetweenDepths(lines []*textLine, maxDepth, minDepth float64) []*textLine {
	var lines2 []*textLine
	for _, line := range lines {
		if maxDepth == -1 {
			if line.depth > minDepth {
				lines2 = append(lines2, line)
			}
		} else {
			if line.depth > minDepth && line.depth < maxDepth {
				lines2 = append(lines2, line)
			}
		}
	}
	return lines2
}

func (pt *PageText) getText() string {
	var out strings.Builder
	numMarks := len(pt.marks)
	for i := 0; i < 360 && numMarks > 0; i += 90 {
		angleMarks := make([]*textMark, 0, len(pt.marks)-numMarks)
		for _, mark := range pt.marks {
			if mark.orientation == i {
				angleMarks = append(angleMarks, mark)
			}
		}
		if len(angleMarks) > 0 {
			out.WriteString(makeSimpleText(angleMarks, pt.pageSize))
			numMarks -= len(angleMarks)
		}
	}
	return out.String()
}

func (wb *wordBag) beginDeferredRemoval() {
	wb.removed = make(map[*textWord]struct{})
	wb.removedCount = make(map[int]int, len(wb.bins))
	wb.binStart = make(map[int]int, len(wb.bins))
}
