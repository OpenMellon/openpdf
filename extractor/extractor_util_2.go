package extractor

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

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

var romanNumeralPattern string = "(?i)^(M{0,3})(C(?:D|M)|D?C{0,3})(X(?:L|C)|L?X{0,3})(I(?:V|X)|V?I{0,3})(\\)|\\.)|^\\((M{0,3})(C(?:D|M)|D?C{0,3})(X(?:L|C)|L?X{0,3})(I(?:V|X)|V?I{0,3})\\)"

var enumeratorPattern string = "^[a-zA-Z](\\)|\\.)|^[\\d]+(\\)|\\.)|^\\([a-zA-Z]\\)|^\\([\\d]+\\)"

const glyphSpaceUnit = 1.0 / 1000.0

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

const (
	msgFontConvert       = "ERROR: Can't convert font object, invalid type"
	msgFontPropsNotFound = "ERROR: Can't get font properties, font not found"
	msgFontStreamInvalid = "ERROR: Can't get font stream, invalid type"
	msgNoFontFound       = "ERROR: No font found"
)

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

const maxFormDepth = 20

const maxCachedFonts = 10

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var bulletChars = []string{"❖", "➢", "✓", "•", "\uf0a7", "□", "−", "■", "▪", "o"}

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

var bareNumberRegexp = regexp.MustCompile("^\\s*(\\d+\\.?|[Iiv]+)\\s*\\)?$")

func topEdgeRuling(rect model.PdfRectangle) *ruling {
	return &ruling{kind: rulingHorz, xy: rect.Ury, lo: rect.Llx, hi: rect.Urx}
}

func rightEdgeRuling(rect model.PdfRectangle) *ruling {
	return &ruling{kind: rulingVert, xy: rect.Urx, lo: rect.Lly, hi: rect.Ury}
}

func approxEqual(a, b float64) bool { return math.Abs(a-b) <= approxTolerance }

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

func intersectRects(a, b model.PdfRectangle) (model.PdfRectangle, bool) {
	if !rectsOverlap(a, b) {
		return model.PdfRectangle{}, false
	}
	return model.PdfRectangle{Llx: math.Max(a.Llx, b.Llx), Urx: math.Min(a.Urx, b.Urx), Lly: math.Max(a.Lly, b.Lly), Ury: math.Min(a.Ury, b.Ury)}, true
}

func roundToGrid(x float64) float64 { return coordGranularity * math.Round(x/coordGranularity) }

func roundToMultiple(v float64, multiple int) int {
	if multiple == 0 {
		multiple = 1
	}
	m := float64(multiple)
	return int(math.Round(v/m) * m)
}

func sortedIntSetKeys(m map[int]intSet) []int {
	keys := make([]int, 0, len(m))
	for i := range m {
		keys = append(keys, i)
	}
	sort.Ints(keys)
	return keys
}

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

func sortedTileKeys(m map[float64]gridTile) []float64 {
	keys := make([]float64, 0, len(m))
	for x := range m {
		keys = append(keys, x)
	}
	sort.Float64s(keys)
	return keys
}

func sortedLineDepths(m map[float64][]*textLine) []float64 {
	depths := []float64{}
	for d := range m {
		depths = append(depths, d)
	}
	sort.Float64s(depths)
	return depths
}

func sortedIndexes(n int, less func(int, int) bool) []int {
	idxs := make([]int, n)
	for i := range idxs {
		idxs[i] = i
	}
	sort.Slice(idxs, func(n2, n3 int) bool { return less(idxs[n2], idxs[n3]) })
	return idxs
}

func bottomEdgeRuling(rect model.PdfRectangle) *ruling {
	return &ruling{kind: rulingHorz, xy: rect.Lly, lo: rect.Llx, hi: rect.Urx}
}

func pointsEqual(a, b transform.Point) bool { return a.X == b.X && a.Y == b.Y }

func closeEnough(d float64) bool { return math.Abs(d) < alignTolerance }

func leftEdgeRuling(rect model.PdfRectangle) *ruling {
	return &ruling{kind: rulingVert, xy: rect.Llx, lo: rect.Lly, hi: rect.Ury}
}

func rectsOverlap(a, b model.PdfRectangle) bool { return xOverlap(a, b) && yOverlap(a, b) }

func translateBy(d transform.Point) transform.Matrix {
	return transform.TranslationMatrix(d.X, d.Y)
}

func splitRulingLists(rulings []rulingList) (rulingList, rulingList) {
	var all rulingList
	for _, l := range rulings {
		all = append(all, l...)
	}
	return all.vertsHorzs()
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

func unionRects(a, b model.PdfRectangle) model.PdfRectangle {
	return model.PdfRectangle{Llx: math.Min(a.Llx, b.Llx), Lly: math.Min(a.Lly, b.Lly), Urx: math.Max(a.Urx, b.Urx), Ury: math.Max(a.Ury, b.Ury)}
}

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

func unpoolImage(img model.Image) *model.Image {
	img.Data = core.UnpooledBytes(img.Data)
	img.SetAlpha(core.UnpooledBytes(img.AlphaData()))
	return &img
}

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

func startsWithBulletByte(b byte) bool {
	for _, bc := range bulletChars {
		if []byte(bc)[0] == b {
			return true
		}
	}
	return false
}

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

func hasDirectObject(marks *TextMarkArray) bool {
	return len(marks.marks) > 0 && marks.marks[0].DirectObject != nil
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

func findAllMatches(pattern string, text string) ([][]int, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("error compiling regex pattern: %w", err)
	}
	matches := re.FindAllStringIndex(text, -1)
	return matches, nil
}

func marksToString(marks *TextMarkArray) string {
	var out strings.Builder
	for _, mark := range marks.Elements() {
		out.WriteString(mark.Text)
	}
	return out.String()
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

func dropLastRune(s string) string { runes := []rune(s); return string(runes[:len(runes)-1]) }
