// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"errors"
	"strconv"

	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/model"
)

// Finalize renders all blocks to the creator pages. In addition, it takes care
// of adding headers and footers, as well as generating the front page,
// table of contents and outlines.
// Finalize is automatically called before writing the document out. Calling the
// method manually can be useful when adding external pages to the creator,
// using the AddPage method, as it renders all creator blocks to the added
// pages, without having to write the document out.
// NOTE: TOC and outlines are generated only if the AddTOC and AddOutlines
// fields of the creator are set to true (enabled by default). Furthermore, TOCs
// and outlines without content are skipped. TOC and outline content is
// added automatically when using the chapter component. TOCs and outlines can
// also be set externally, using the SetTOC and SetOutlineTree methods.
// Finalize should only be called once, after all draw calls have taken place,
// as it will return immediately if the creator instance has been finalized.
func (c *Creator) Finalize() error {
	if c.finalized {
		return nil
	}
	pagesLen := len(c.pages)
	n := 0
	if c.genFrontPageFunc != nil {
		c2 := *c
		c.pages = nil
		c.activePage = nil
		c.initContext()
		frontpageFunctionArgs := FrontpageFunctionArgs{PageNum: 1, TotalPages: pagesLen}
		c.genFrontPageFunc(frontpageFunctionArgs)
		n += len(c.pages)
		c.pages = c2.pages
		c.activePage = c2.activePage
		c.chapters = c2.chapters
	}
	if c.AddTOC {
		c.initContext()
		c.context.Page = n + 1
		if c.CustomTOC && c.genTOCFunc != nil {
			c2 := *c
			c.pages = nil
			c.activePage = nil
			if err := c.genTOCFunc(c.toc); err != nil {
				return err
			}
			n += len(c.pages)
			c.pages = c2.pages
			c.activePage = c2.activePage
			c.chapters = c2.chapters
		} else {
			if c.genTOCFunc != nil {
				if err := c.genTOCFunc(c.toc); err != nil {
					return err
				}
			}
			pageBlocks, _, err := c.toc.GeneratePageBlocks(c.context)
			if err != nil {
				common.Log.Debug("Failed to generate blocks: %v", err)
				return err
			}
			n += len(pageBlocks)
		}
		lines := c.toc.Lines()
		for _, line := range lines {
			n2, err := strconv.Atoi(line.Page.Text)
			if err != nil {
				continue
			}
			line.Page.Text = strconv.Itoa(n2 + n)
			line.linkPage += int64(n)
		}
	}
	ok := false
	var pages []*model.PdfPage
	if c.genFrontPageFunc != nil {
		c2 := *c
		c.pages = nil
		c.activePage = nil
		frontpageFunctionArgs := FrontpageFunctionArgs{PageNum: 1, TotalPages: pagesLen}
		c.genFrontPageFunc(frontpageFunctionArgs)
		pagesLen += len(c.pages)
		pages = c.pages
		c.pages = append(c.pages, c2.pages...)
		c.activePage = c2.activePage
		c.chapters = c2.chapters
		ok = true
	}
	var pages2 []*model.PdfPage
	if c.AddTOC {
		c.initContext()
		if c.CustomTOC && c.genTOCFunc != nil {
			c2 := *c
			c.pages = nil
			c.activePage = nil
			if err := c.genTOCFunc(c.toc); err != nil {
				common.Log.Debug("Error generating TOC: %v", err)
				return err
			}
			pages2 = c.pages
			pagesLen += len(pages2)
			c.pages = c2.pages
			c.activePage = c2.activePage
			c.chapters = c2.chapters
		} else {
			if c.genTOCFunc != nil {
				if err := c.genTOCFunc(c.toc); err != nil {
					common.Log.Debug("Error generating TOC: %v", err)
					return err
				}
			}
			pageBlocks, _, _ := c.toc.GeneratePageBlocks(c.context)
			for _, pageBlock := range pageBlocks {
				pageBlock.SetPos(0, 0)
				pagesLen++
				page := c.newPage()
				pages2 = append(pages2, page)
				c.setActivePage(page)
				c.Draw(pageBlock)
			}
		}
		if ok {
			pages4 := pages
			pages3 := c.pages[len(pages):]
			c.pages = append([]*model.PdfPage{}, pages4...)
			c.pages = append(c.pages, pages2...)
			c.pages = append(c.pages, pages3...)
		} else {
			c.pages = append(pages2, c.pages...)
		}
	}
	if c.outline != nil && c.AddOutlines {
		var fn func(outlineItem *model.OutlineItem)
		fn = func(outlineItem *model.OutlineItem) {
			outlineItem.Dest.Page += int64(n)
			if page := int(outlineItem.Dest.Page); page >= 0 && page < len(c.pages) {
				outlineItem.Dest.PageObj = c.pages[page].GetPageAsIndirectObject()
			} else {
				common.Log.Debug("WARN: could not get page container for page %d", page)
			}
			outlineItem.Dest.Y = precision.RoundDefault(c.pageHeight - outlineItem.Dest.Y)
			items := outlineItem.Items()
			for _, item := range items {
				fn(item)
			}
		}
		items := c.outline.Items()
		for _, item := range items {
			fn(item)
		}
		if c.AddTOC {
			var pagesLen int
			if ok {
				pagesLen = len(pages)
			}
			outlineDest := model.NewOutlineDest(int64(pagesLen), 0, c.pageHeight)
			if pagesLen >= 0 && pagesLen < len(c.pages) {
				outlineDest.PageObj = c.pages[pagesLen].GetPageAsIndirectObject()
			} else {
				common.Log.Debug("WARN: could not get page container for page %d", pagesLen)
			}
			c.outline.Insert(0, model.NewOutlineItem("Table of Contents", outlineDest))
		}
	}
	if n > 0 {
		for i := range c.chapters {
			c.chapters[i].page += n
		}
	}
	for i, page := range c.pages {
		c.setActivePage(page)
		chapter := c.ChapterForPage(i + 1)
		if c.pageFinalizeFunc != nil {
			size, pageHeight, err := page.Size()
			if err != nil {
				return err
			}
			pageFinalizeFunctionArgs := PageFinalizeFunctionArgs{PageNum: i + 1, PageWidth: size, PageHeight: pageHeight, TOCPages: len(pages2), TotalPages: pagesLen, Chapter: chapter}
			if err := c.pageFinalizeFunc(pageFinalizeFunctionArgs); err != nil {
				common.Log.Debug("ERROR: page finalize callback: %v", err)
				return err
			}
		}
		if c.drawHeaderFunc != nil {
			block := NewBlock(c.pageWidth, c.pageMargins.Top)
			headerFunctionArgs := HeaderFunctionArgs{PageNum: i + 1, TotalPages: pagesLen, Chapter: chapter}
			c.drawHeaderFunc(block, headerFunctionArgs)
			block.SetPos(0, 0)
			if c.taggingEnabled && block.GetArtifact() == nil {
				block.MarkAsArtifact(model.ArtifactTypePagination)
			}
			if err := c.Draw(block); err != nil {
				common.Log.Debug("ERROR: drawing header: %v", err)
				return err
			}
		}
		if c.drawFooterFunc != nil {
			block := NewBlock(c.pageWidth, c.pageMargins.Bottom)
			footerFunctionArgs := FooterFunctionArgs{PageNum: i + 1, TotalPages: pagesLen, Chapter: chapter}
			c.drawFooterFunc(block, footerFunctionArgs)
			block.SetPos(0, c.pageHeight-block.height)
			if c.taggingEnabled && block.GetArtifact() == nil {
				block.MarkAsArtifact(model.ArtifactTypePagination)
			}
			if err := c.Draw(block); err != nil {
				common.Log.Debug("ERROR: drawing footer: %v", err)
				return err
			}
		}
		pageTransform, ok := c.pageTransforms[page]
		if block, ok2 := c.blocks[page]; ok2 {
			if ok {
				pageTransform.transformBlock(block)
			}
			if err := block.drawToPage(page); err != nil {
				common.Log.Debug("ERROR: drawing page %d blocks: %v", i+1, err)
				return err
			}
		}
		if ok {
			if err := pageTransform.transformPage(page); err != nil {
				common.Log.Debug("ERROR: could not transform page: %v", err)
				return err
			}
		}
	}
	c.finalized = true
	return nil
}

// AddColorStop add color stop info for rendering gradient color.
func (ls *LinearShading) AddColorStop(color Color, point float64) {
	ls.base.AddColorStop(color, point)
}

// SetLineMargins sets the margins for all new lines of the table of contents.
func (to *TOC) SetLineMargins(left, right, top, bottom float64) {
	lineMargins := &to.lineMargins
	lineMargins.Left = left
	lineMargins.Right = right
	lineMargins.Top = top
	lineMargins.Bottom = bottom
}

func (t *Table) addRowTag() *model.KDict {
	kDictionary := model.NewKDictionary()
	kDictionary.S = core.MakeName(string(model.StructureTypeTableRow))
	t.tagInfo.ComponentKObj.AddKChild(kDictionary)
	return kDictionary
}

// SetHeaderRows turns the selected table rows into headers that are repeated
// for every page the table spans. startRow and endRow are inclusive.
func (t *Table) SetHeaderRows(startRow, endRow int) error {
	if startRow <= 0 {
		return errors.New("header start row must be greater than 0")
	}
	if endRow <= 0 {
		return errors.New("header end row must be greater than 0")
	}
	if startRow > endRow {
		return errors.New("header start row  must be less than or equal to the end row")
	}
	t.hasHeaderRows = true
	t.headerStartRow = startRow
	t.headerEndRow = endRow
	return nil
}

func parseSVGLength(value string, bitSize int) (float64, error) {
	s, unit := splitUnitSuffix(value)
	float, err := strconv.ParseFloat(s, bitSize)
	if err != nil {
		return 0, err
	}
	if unitToPoint, ok := unitToPoints[unit]; ok {
		float = float * unitToPoint
	} else {
		float = float * pxToPt
	}
	return float, nil
}

func (i *Invoice) setCellBorder(cell *TableCell, invoiceCell *InvoiceCell) {
	for _, borderSide := range invoiceCell.BorderSides {
		cell.SetBorder(borderSide, CellBorderStyleSingle, invoiceCell.BorderWidth)
	}
	cell.SetBorderColor(invoiceCell.BorderColor)
}

var chapterAttrs = map[string]attrApplyFunc{"show-numbering": boolAttr(func(chapter *Chapter, show bool) { chapter.SetShowNumbering(show) }), "include-in-toc": boolAttr(func(chapter *Chapter, includeInTOC bool) { chapter.SetIncludeInTOC(includeInTOC) }), "margin": marginAttr(func(chapter *Chapter, margins Margins) {
	chapter.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
})}

// CellBorderStyle defines the table cell's border style.
type CellBorderStyle int

// SetLanguageIdentifier sets the language identifier for the paragraph.
func (sp *StyledParagraph) SetLanguageIdentifier(id string) {
	if sp.tagInfo == nil {
		sp.tagInfo = model.NewStructureTagInfo()
		sp.tagInfo.StructureType = model.StructureTypeParagraph
	}
	sp.languageID = id
}

type templateText struct {
	text   string
	line   int
	col    int
	offset int64
}

// Line defines a line between point 1 (X1, Y1) and point 2 (X2, Y2).
// The line width, color, style (solid or dashed) and opacity can be
// configured. Implements the Drawable interface.
type Line struct {
	taggedDrawable
	x1          float64
	y1          float64
	x2          float64
	y2          float64
	color       Color
	style       contentdraw.LineStyle
	opacity     float64
	dashArray   []int64
	dashPhase   int64
	lineWidth   float64
	positioning Positioning
	fitMode     FitMode
	margins     Margins
}
