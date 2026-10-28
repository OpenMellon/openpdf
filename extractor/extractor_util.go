package extractor

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

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

func reverseInts(in []int) []int {
	out := make([]int, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}

func reverseStrings(strs []string, from int, to int) {
	for i, j := from, to-1; i < j; i, j = i+1, j-1 {
		tmp := strs[i]
		strs[i] = strs[j]
		strs[j] = tmp
	}
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

func replaceRange(s string, start, end int, repl string) string {
	if end > len(s)-1 {
		return s[:start] + repl
	}
	return s[:start] + repl + s[end:]
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

func decodeBytes(buf []byte, font *model.PdfFont) string {
	codes := font.BytesToCharcodes(buf)
	strs, numChars, numMisses := font.CharcodesToStrings(codes, "")
	if numMisses > 0 {
		common.Log.Debug("renderText: numChars=%d numMisses=%d", numChars, numMisses)
	}
	text := strings.Join(strs, "")
	return text
}

func combiningFor(s string) (string, bool) {
	runes := []rune(s)
	if len(runes) != 1 {
		return "", false
	}
	comb, ok := accentToCombining[runes[0]]
	return comb, ok
}

func alignToRuneStart(s string, i int) int {
	for i > 0 && i < len(s) && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

var accentToCombining = map[rune]string{0x0060: "̀", 0x02CB: "̀", 0x0027: "́", 0x00B4: "́", 0x02B9: "́", 0x02CA: "́", 0x005E: "̂", 0x02C6: "̂", 0x007E: "̃", 0x02DC: "̃", 0x00AF: "̄", 0x02C9: "̄", 0x02D8: "̆", 0x02D9: "̇", 0x00A8: "̈", 0x00B0: "̊", 0x02DA: "̊", 0x02BA: "̋", 0x02DD: "̋", 0x02C7: "̌", 0x02C8: "̍", 0x0022: "̎", 0x02BB: "̒", 0x02BC: "̓", 0x0486: "̓", 0x055A: "̓", 0x02BD: "̔", 0x0485: "̔", 0x0559: "̔", 0x02D4: "̝", 0x02D5: "̞", 0x02D6: "̟", 0x02D7: "̠", 0x02B2: "̡", 0x00B8: "̧", 0x02CC: "̩", 0x02B7: "̫", 0x02CD: "̱", 0x005F: "̲", 0x204E: "͙"}

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

func isBlank(s string) bool {
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func isPatternColorspace(cs model.PdfColorspace) bool {
	_, ok := cs.(*model.PdfColorspaceSpecialPattern)
	return ok
}

func isOdd(n int) bool { return (n & 1) != 0 }

func isEven(n int) bool { return (n & 1) == 0 }

func isZero(x float64) bool { return math.Abs(x) < zeroTolerance }

func isNearlyHorizontal(a, b transform.Point) bool {
	dx := math.Abs(a.X - b.X)
	dy := math.Abs(a.Y - b.Y)
	return isRatioSmall(dy, dx)
}

func isFlatSegment(p0, p1 transform.Point) bool {
	dx := math.Abs(p0.X - p1.X)
	dy := math.Abs(p0.Y - p1.Y)
	return isRatioSmall(dx, dy)
}

func isAdjacentRight(bag *wordBag, word *textWord, gap float64) bool {
	return bag.Urx <= word.Llx && word.Llx < bag.Urx+gap
}

func isRatioSmall(num, den float64) bool { return num/math.Max(slopeEpsilon, den) < maxSlope }

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

func truncate(s string, maxLen int) string {
	if len(s) < maxLen {
		return s
	}
	return s[:maxLen]
}

var (
	errTypeCheck  = errors.New("type check error")
	errRangeCheck = errors.New("range check error")
)

// New returns an Extractor instance for extracting content from the input PDF page.
func New(page *model.PdfPage) (*Extractor, error) { return NewWithOptions(page, nil) }

// NewFromContents creates a new extractor from contents and page resources.
func NewFromContents(contents string, resources *model.PdfPageResources) (*Extractor, error) {
	ex := &Extractor{contents: contents, resources: resources, fonts: map[string]fontEntry{}, parsedForms: map[*core.PdfObjectStream]parsedForm{}, formResults: map[formCacheKey]textResult{}}
	return ex, nil
}

// NewWithOptions an Extractor instance for extracting content from the input PDF page with options.
func NewWithOptions(page *model.PdfPage, options *Options) (*Extractor, error) {
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
	return ex, nil
}

func newTwoByTwoTable(tl, tr, bl, br *textPara) *textTable {
	table := &textTable{w: 2, h: 2, cells: make(map[uint64]*textPara, 4)}
	table.put(0, 0, tl)
	table.put(1, 0, tr)
	table.put(0, 1, bl)
	table.put(1, 1, br)
	return table
}

func newLineFromBag(bag *wordBag, n int) *textLine {
	word := bag.firstWord(n)
	line := textLine{PdfRectangle: word.PdfRectangle, fontSize: word.fontSize, depth: word.depth}
	line.pullWord(bag, word, n)
	return &line
}

var blackColorKey = newColorStateKey(model.NewPdfColorspaceDeviceGray(), model.NewPdfColorDeviceGray(0))

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

func removeWordFromSlice(words []*textWord, word *textWord) []*textWord {
	for i, w := range words {
		if w == word {
			return removeWordAt(words, i)
		}
	}
	common.Log.Error("removeWord: words doesn't contain word=%s", word)
	return nil
}

func removeWordAt(words []*textWord, idx int) []*textWord {
	numWords := len(words)
	copy(words[idx:], words[idx+1:])
	return words[:numWords-1]
}
