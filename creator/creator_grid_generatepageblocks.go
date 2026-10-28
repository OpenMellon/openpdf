// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"errors"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// GeneratePageBlocks generates the grid page blocks. Multiple blocks are
// generated if the contents wrap over multiple pages.
// Implements the Drawable interface.
func (g *Grid) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	var blocks []*Block
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	g.updateRowHeights(ctx.Width - g.margins.Left - g.margins.Right)
	top := g.margins.Top
	ctx2 := ctx
	if g.positioning.IsAbsolute() {
		ctx.X = g.xPos
		ctx.Y = g.yPos
	} else {
		ctx.X += g.margins.Left
		ctx.Y += top
		ctx.Width -= g.margins.Left + g.margins.Right
		ctx.Height -= top
	}
	width := ctx.Width
	x := ctx.X
	y := ctx.Y
	height := ctx.Height
	var (
		i2               = 0
		ok               = false
		mcid             int64
		componentKObj    *model.KDict
		section          = GridRowSectionUnknown
		structureTagInfo *model.StructureTagInfo
		ok2              = g.tagInfo != nil && g.tagInfo.ApplyTag
		row3             *GridRow
	)
	if ok2 {
		beginTaggedContent(block, g.tagInfo)
		mcid = g.tagInfo.Mcid + 1
		componentKObj = g.tagInfo.ComponentKObj
	}
	for i := 0; i < len(g.rows); i++ {
		row := g.rows[i]
		contentHeight := float64(0.0)
		for i := i2; i < row.index; i++ {
			contentHeight += g.rows[i].contentHeight
		}
		ctx.Height = height - contentHeight
		if ok {
			if ok2 {
				if structureTagInfo != nil {
					endMarkedContent(block)
				}
				endMarkedContent(block)
			}
			blocks = append(blocks, block)
			block = NewBlock(ctx.PageWidth, ctx.PageHeight)
			x = ctx.Margins.Left + g.margins.Left
			y = ctx.Margins.Top
			ctx.Height = ctx.contentHeight()
			ctx.Page++
			height = ctx.Height
			i2 = i
			contentHeight = 0
			ok = false
			if ok2 {
				beginTaggedContent(block, g.tagInfo)
				if structureTagInfo != nil {
					if row.section == section {
						beginTaggedContent(block, structureTagInfo)
					} else {
						structureTagInfo = nil
						section = GridRowSectionUnknown
						componentKObj = g.tagInfo.ComponentKObj
					}
				}
			}
		}
		if row.contentHeight > ctx.Height {
			row2 := g.cloneRow(row.index)
			type cellSplit struct{ fit, rest VectorDrawable }
			cellSplits := make([]cellSplit, len(row.cells))
			ok2 := false
			for i, cell := range row.cells {
				width2 := cell.width(g.colWidths, width)
				if cell.content == nil || cell.height(width2) <= ctx.Height {
					continue
				}
				ctx2 := ctx
				ctx2.Width = width2 - cell.indent
				var fit, content VectorDrawable
				switch content2 := cell.content.(type) {
				case *StyledParagraph:
					ctx2.Height = math.Floor(ctx.Height - content2.margins.Top - content2.margins.Bottom - 0.5*content2.getTextHeight())
					head, tail, err := content2.split(ctx2)
					if err != nil {
						common.Log.Debug("ERROR: %v", err)
					}
					if head != nil && tail != nil {
						fit, content = head, tail
					}
				case *Division:
					ctx2.Height = math.Floor(ctx.Height - content2.margins.Top - content2.margins.Bottom)
					if head, tail := content2.split(ctx2, ctx.contentHeight()-content2.margins.Vertical()); tail != nil {
						content = tail
						if head != nil {
							fit = head
						}
					}
				case *List:
					ctx2.Height = math.Floor(ctx.Height - content2.margins.Vertical())
					if head, tail := content2.split(ctx2, ctx.contentHeight()-content2.margins.Vertical()); tail != nil {
						content = tail
						if head != nil {
							fit = head
						}
					}
				case *Table:
					if head, tail := content2.split(ctx2, ctx.contentHeight()); tail != nil {
						content = tail
						if head != nil {
							fit = head
						}
					}
				}
				if content == nil && cell.rowSpan == 1 {
					content = cell.content
				}
				if content != nil {
					cellSplits[i] = cellSplit{fit, content}
					if fit != nil {
						ok2 = true
					}
				}
			}
			ok = true
			if ok2 {
				for i, cellSplit := range cellSplits {
					if cellSplit.rest == nil {
						continue
					}
					if cellSplit.fit != nil {
						row.cells[i].SetContent(cellSplit.fit)
					} else {
						row.cells[i].content = nil
					}
					row2.cells[i].SetContent(cellSplit.rest)
				}
				g.insertRowAfter(row.index, row2)
				row.updateRowHeight(width)
				row2.updateRowHeight(width)
			} else if row3 != row && !(contentHeight == 0 && ctx.Height >= ctx.contentHeight()-0.1) {
				row3 = row
				i -= 1
				continue
			}
		}
		if ok2 {
			if section != row.section {
				if structureTagInfo != nil {
					endMarkedContent(block)
				}
				section = row.section
				componentKObj = g.addSectionTag(section)
				structureTagInfo = &model.StructureTagInfo{Mcid: mcid, StructureType: gridSectionStructureTypes[section]}
				beginTaggedContent(block, structureTagInfo)
				mcid++
			}
			structureTypeTableRow := model.StructureTypeTableRow
			beginTaggedContent(block, &model.StructureTagInfo{Mcid: mcid, StructureType: structureTypeTableRow})
			row.AddTag(componentKObj)
			mcid++
		}
		for _, cell := range row.cells {
			var kDictionary *model.KDict
			if ok2 {
				cell.SetMarkedContentID(mcid)
				structureTypeTableData := model.StructureTypeTableData
				if row.IsHeader() {
					structureTypeTableData = model.StructureTypeTableHeaderCell
				}
				cell.SetStructureType(structureTypeTableData)
				beginTaggedContent(block, &model.StructureTagInfo{Mcid: mcid, StructureType: structureTypeTableData})
				kDictionary = model.NewKDictionary()
				kDictionary.S = core.MakeName(string(structureTypeTableData))
				kValue := model.NewKValue()
				kValue.SetMCID(int(mcid))
				kDictionary.AddChild(kValue)
				row.tagInfo.ComponentKObj.AddKChild(kDictionary)
				mcid++
			}
			left := cell.width(g.colWidths, width)
			colWidth := float64(0.0)
			for i := 0; i < cell.col; i++ {
				colWidth += g.colWidths[i] * width
			}
			height := float64(0.0)
			for i := 0; i < cell.rowSpan; i++ {
				height += g.rows[cell.row+i].contentHeight
			}
			if cell.rowSpan == 1 && height > ctx.Height {
				height = ctx.Height
			}
			ctx.Width = left
			ctx.X = x + colWidth
			ctx.Y = y + contentHeight
			border := newBorder(ctx.X, ctx.Y, left, height)
			if cell.backgroundColor != nil {
				border.SetFillColor(cell.backgroundColor)
			}
			border.SetOpacity(cell.opacity)
			border.LineStyle = cell.borderLineStyle
			border.styleLeft = cell.borderStyleLeft
			border.styleRight = cell.borderStyleRight
			border.styleTop = cell.borderStyleTop
			border.styleBottom = cell.borderStyleBottom
			if cell.borderColorLeft != nil {
				border.SetColorLeft(cell.borderColorLeft)
			}
			if cell.borderColorBottom != nil {
				border.SetColorBottom(cell.borderColorBottom)
			}
			if cell.borderColorRight != nil {
				border.SetColorRight(cell.borderColorRight)
			}
			if cell.borderColorTop != nil {
				border.SetColorTop(cell.borderColorTop)
			}
			border.SetWidthBottom(cell.borderWidthBottom)
			border.SetWidthLeft(cell.borderWidthLeft)
			border.SetWidthRight(cell.borderWidthRight)
			border.SetWidthTop(cell.borderWidthTop)
			err := block.Draw(border)
			if err != nil {
				common.Log.Debug("ERROR: %v", err)
			}
			if cell.content != nil {
				width := cell.content.Width()
				top := cell.content.Height()
				width, top, y, kChild, ok, err := layoutCellContent(cell.content, width, top, cell.verticalAlignment, false, kDictionary, mcid, left)
				if err != nil {
					return nil, ctx, err
				}
				if kChild != nil {
					if !ok {
						kDictionary.AddKChild(kChild)
					}
					mcid++
				}
				switch cell.horizontalAlignment {
				case CellHorizontalAlignmentLeft:
					ctx.X += cell.indent
					ctx.Width -= cell.indent
				case CellHorizontalAlignmentCenter:
					if left2 := left - width; left2 > 0 {
						ctx.X += left2 / 2
						ctx.Width -= left2 / 2
					}
				case CellHorizontalAlignmentRight:
					if left > width {
						ctx.X = ctx.X + left - width - cell.indent
						ctx.Width -= cell.indent
					}
				}
				y2 := ctx.Y
				height2 := ctx.Height
				ctx.Y += y
				switch cell.verticalAlignment {
				case CellVerticalAlignmentTop:
				case CellVerticalAlignmentMiddle:
					if height2 := height - top; height2 > 0 {
						ctx.Y += height2 / 2
						ctx.Height -= height2 / 2
					}
				case CellVerticalAlignmentBottom:
					if height > top {
						ctx.Y = ctx.Y + height - top
						ctx.Height = height
					}
				}
				err = block.DrawWithContext(cell.content, ctx)
				if errors.Is(err, ErrContentNotFit) {
					if drawableToFit := splitDrawableToFit(cell.content, ctx); drawableToFit != nil {
						err = block.DrawWithContext(drawableToFit, ctx)
					}
				}
				if err != nil {
					common.Log.Debug("ERROR: %v", err)
				}
				ctx.Y = y2
				ctx.Height = height2
			}
			if ok2 {
				endMarkedContent(block)
			}
		}
		ctx.Y += math.Min(row.contentHeight, ctx.Height)
		if ok2 {
			endMarkedContent(block)
		}
	}
	if ok2 {
		if structureTagInfo != nil {
			endMarkedContent(block)
		}
		endMarkedContent(block)
	}
	blocks = append(blocks, block)
	if g.positioning.IsAbsolute() {
		return blocks, ctx2, nil
	}
	ctx.X = ctx2.X
	ctx.Width = ctx2.Width
	ctx.Y += g.margins.Bottom
	ctx.Height -= g.margins.Bottom
	return blocks, ctx, nil
}

// Rows returns the total number of rows the table has.
func (t *Table) Rows() int { return t.rows }
