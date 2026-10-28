package extractor

import (
	"fmt"
	"sort"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/model"
)

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

func (cc compositeCell) parasBBox() (paraList, model.PdfRectangle) {
	return cc.paraList, cc.PdfRectangle
}

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

type compositeCell struct {
	model.PdfRectangle
	paraList
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
