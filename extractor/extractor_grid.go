package extractor

import (
	"fmt"
	"image"
	"sort"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/model"
	imagedraw "golang.org/x/image/draw"
)

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

type compositeCell struct {
	model.PdfRectangle
	paraList
}

func (cc compositeCell) parasBBox() (paraList, model.PdfRectangle) {
	return cc.paraList, cc.PdfRectangle
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

func (cc compositeCell) String() string {
	txt := ""
	if len(cc.paraList) > 0 {
		txt = truncate(cc.paraList.merge().text(), 50)
	}
	return fmt.Sprintf("%6.2f %d paras %q", cc.PdfRectangle, len(cc.paraList), txt)
}

func (cc *compositeCell) updateBBox() {
	for _, para := range cc.paraList {
		cc.PdfRectangle = unionRects(cc.PdfRectangle, para.PdfRectangle)
	}
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

type gridTile struct {
	model.PdfRectangle
	ury, llx, lly, urx bool
}

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

func (gt gridTile) complete() bool { return gt.numBorders() == 4 }

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

func newGridTile(rect model.PdfRectangle, left, right, bottom, top *ruling) gridTile {
	llx := rect.Llx
	urx := rect.Urx
	lly := rect.Lly
	ury := rect.Ury
	return gridTile{PdfRectangle: rect, llx: left != nil && left.encloses(lly, ury), urx: right != nil && right.encloses(lly, ury), lly: bottom != nil && bottom.encloses(llx, urx), ury: top != nil && top.encloses(llx, urx)}
}

type gridTiling struct {
	model.PdfRectangle
	llx   []float64
	lly   []float64
	tiles map[float64]map[float64]gridTile
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

type gridRow struct {
	words []gridWord
	y     float64
}

type gridWord struct {
	text     string
	llx      float64
	urx      float64
	ury      float64
	fontSize float64
	index    int
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

// TableCell is a cell in a TextTable.
type TableCell struct {
	model.PdfRectangle

	// Text is the extracted text.
	Text string

	// Marks returns the TextMarks corresponding to the text in Text.
	Marks TextMarkArray
}
