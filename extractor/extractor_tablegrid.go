package extractor

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/model"
)

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

type gridTiling struct {
	model.PdfRectangle
	llx   []float64
	lly   []float64
	tiles map[float64]map[float64]gridTile
}

func (gt gridTile) complete() bool { return gt.numBorders() == 4 }

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

type gridRow struct {
	words []gridWord
	y     float64
}

func cellKey(col, row int) uint64 { return uint64(col)*0x1000000 + uint64(row) }

func sortedTileKeys(m map[float64]gridTile) []float64 {
	keys := make([]float64, 0, len(m))
	for x := range m {
		keys = append(keys, x)
	}
	sort.Float64s(keys)
	return keys
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

type gridTile struct {
	model.PdfRectangle
	ury, llx, lly, urx bool
}

type gridWord struct {
	text     string
	llx      float64
	urx      float64
	ury      float64
	fontSize float64
	index    int
}

func logGrids(msg string, grids []rulingList) {
	common.Log.Info("$$ %d grids - %s", len(grids), msg)
	for i, grid := range grids {
		fmt.Printf("%4d: %s\n", i, grid.String())
	}
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

func newGridTile(rect model.PdfRectangle, left, right, bottom, top *ruling) gridTile {
	llx := rect.Llx
	urx := rect.Urx
	lly := rect.Lly
	ury := rect.Ury
	return gridTile{PdfRectangle: rect, llx: left != nil && left.encloses(lly, ury), urx: right != nil && right.encloses(lly, ury), lly: bottom != nil && bottom.encloses(llx, urx), ury: top != nil && top.encloses(llx, urx)}
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
