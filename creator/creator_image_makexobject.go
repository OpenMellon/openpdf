// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"math"
	"slices"

	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func (im *Image) makeXObject() error {
	mask := im.mask
	var img2 *model.Image
	if mask != nil && mask.kind == imageMaskOptimizedAlpha {
		var ok bool
		if img2, ok = alphaToBitmask(im.img); !ok {
			common.Log.Debug("Image alpha channel is no longer binary; keeping it as an SMask")
			mask = nil
		}
	}
	if mask == nil {
		xObjectImage, err := model.NewXObjectImageFromImageLazy(im.img, nil, im.encoder, im.lazy)
		if err != nil {
			common.Log.Error("Failed to create xobject image: %s", err)
			return err
		}
		im.xobj = xObjectImage
		return nil
	}
	img := im.img
	encoder := im.encoder
	if len(im.img.AlphaData()) > 0 {
		img2 := *im.img
		img2.SetAlpha(nil)
		img = &img2
		if encoder == nil {
			encoder = core.NewFlateEncoder()
		}
	}
	if mask.kind == imageMaskColorKey && encoder == nil && !im.img.CanPassThroughJPEG() {
		encoder = core.NewFlateEncoder()
	}
	xObjectImage, err := model.NewXObjectImageFromImageLazy(img, nil, encoder, false)
	if err != nil {
		common.Log.Error("Failed to create xobject image: %s", err)
		return err
	}
	if err := im.applyMask(xObjectImage, img2); err != nil {
		return err
	}
	if im.lazy {
		if err := xObjectImage.MakeLazy(); err != nil {
			return err
		}
	}
	im.xobj = xObjectImage
	return nil
}

var emptyAttrs = map[string]attrApplyFunc{}

func (tn *templateNode) componentParent() *templateNode {
	for parent := tn.parent; parent != nil; parent = parent.parent {
		if parent.value != nil {
			return parent
		}
	}
	return nil
}

func (tp *templateProcessor) effectiveParentName(node *templateNode) string {
	if activeParent := node.activeParent(); activeParent != nil {
		return activeParent.start.Name.Local
	}
	if _, ok := tp.renderer.(*Block); ok {
		return "block"
	}
	return "creator"
}

// Height returns the height of a single line of this chunk: FontSize × effective
// line height. When Style.LineHeight is 0 (the default), the effective line
// height is 1.0. To account for the line height set on a containing paragraph,
// callers should either propagate it into Style.LineHeight or multiply the
// result accordingly.
func (tec *TextChunk) Height() float64 {
	return tec.Style.FontSize * tec.Style.effectiveLineHeight(1.0)
}

// ChapterForPage returns the chapter the given 1-based page number belongs to.
// The chapter returned is the one whose heading was most recently laid out at
// or before pageNum - when a subchapter heading appears on the same page as
// (or after) its parent's heading, the subchapter is returned, giving the
// innermost section the reader is currently in. Returns nil if no chapter
// heading has appeared by pageNum (e.g. front matter).
//
// ChapterForPage is populated as chapters are drawn via Creator.Draw.
//
// Page numbering depends on when ChapterForPage is called. Before Finalize
// (e.g. from a custom-TOC callback), pageNum refers to pages as drawn by
// Creator.Draw - any front-page or TOC pages have not yet been prepended.
// After Finalize runs (including from within a header, footer, or
// page-finalize callback), pageNum refers to the final assembled document,
// with front-page and TOC pages counted. Chapters drawn inside a
// CreateFrontPage or CreateTableOfContents callback are not tracked.
func (c *Creator) ChapterForPage(pageNum int) *ChapterInfo {
	for _, v := range slices.Backward(c.chapters) {
		if v.page <= pageNum {
			chapterInfo := v.chapter.chapterInfo()
			return &chapterInfo
		}
	}
	return nil
}

func setFirstError(err2 *error, err error) {
	if *err2 == nil {
		*err2 = err
	}
}

func (ls *LinearShading) shadingModel() *model.PdfShadingType2 {
	point := contentdraw.NewPoint(ls.boundingBox.Llx+ls.boundingBox.Width()/2, ls.boundingBox.Lly+ls.boundingBox.Height()/2)
	point2 := contentdraw.NewPoint(ls.boundingBox.Llx, ls.boundingBox.Lly+ls.boundingBox.Height()/2).Add(-point.X, -point.Y).Rotate(ls.angle).Add(point.X, point.Y)
	point2 = contentdraw.NewPoint(math.Max(math.Min(point2.X, ls.boundingBox.Urx), ls.boundingBox.Llx), math.Max(math.Min(point2.Y, ls.boundingBox.Ury), ls.boundingBox.Lly))
	point3 := contentdraw.NewPoint(ls.boundingBox.Urx, ls.boundingBox.Lly+ls.boundingBox.Height()/2).Add(-point.X, -point.Y).Rotate(ls.angle).Add(point.X, point.Y)
	point3 = contentdraw.NewPoint(math.Min(math.Max(point3.X, ls.boundingBox.Llx), ls.boundingBox.Urx), math.Min(math.Max(point3.Y, ls.boundingBox.Lly), ls.boundingBox.Ury))
	pdfShadingType2 := model.NewPdfShadingType2()
	pdfShadingType2.ShadingType = core.MakeInteger(2)
	pdfShadingType2.ColorSpace = model.NewPdfColorspaceDeviceRGB()
	pdfShadingType2.AntiAlias = core.MakeBool(ls.base.antiAlias)
	pdfShadingType2.Coords = core.MakeArrayFromFloats([]float64{point2.X, point2.Y, point3.X, point3.Y})
	pdfShadingType2.Extend = core.MakeArray(core.MakeBool(ls.base.extends[0]), core.MakeBool(ls.base.extends[1]))
	pdfShadingType2.Function = ls.base.generatePdfFunctions()
	return pdfShadingType2
}

// Height returns the height of the rectangle.
// NOTE: the returned value does not include the border width of the rectangle.
func (r *Rectangle) Height() float64 { return r.height }

// SetStyle sets the style for all the line components: number, title,
// separator, page.
func (tl *TOCLine) SetStyle(style TextStyle) {
	tl.Number.Style = style
	tl.Title.Style = style
	tl.Separator.Style = style
	tl.Page.Style = style
}

func (c *Creator) getActivePage() *model.PdfPage {
	if c.activePage == nil {
		if len(c.pages) == 0 {
			return nil
		}
		return c.pages[len(c.pages)-1]
	}
	return c.activePage
}

// AddInternalLink adds a new internal link to the paragraph.
// The text parameter represents the text that is displayed.
// The user is taken to the specified page, at the specified x and y
// coordinates. Position 0, 0 is at the top left of the page.
// The zoom of the destination page is controlled with the zoom
// parameter. Pass in 0 to keep the current zoom value.
func (sp *StyledParagraph) AddInternalLink(text string, page int64, x, y, zoom float64) *TextChunk {
	textChunk := NewTextChunk(text, sp.linkStyle)
	textChunk.AddAnnotation(newInternalLinkAnnotation(page-1, x, y, zoom, ""))
	return sp.appendChunk(textChunk)
}

// GenerateKDict generates a K dictionary for the image, wiring the alternate
// text (see SetAlternateText) into the Figure structure element's /Alt entry.
func (im *Image) GenerateKDict() (*model.KDict, error) {
	kDict, err := im.taggedDrawable.GenerateKDict()
	if err != nil {
		return nil, err
	}
	if kDict != nil && im.altText != "" {
		kDict.Alt = core.MakeEncodedString(im.altText, true)
	}
	if kDict != nil && im.tagInfo != nil {
		im.tagInfo.ComponentKObj = kDict
	}
	return kDict, nil
}

// AddTag adds the table tag to the root K object.
func (t *Table) AddTag(rootKObj *model.KDict) {
	if rootKObj == nil {
		common.Log.Debug("Table: AddTag called with nil rootKObj, skipping tagging.")
		return
	}
	if t.tagInfo == nil {
		t.tagInfo = model.NewStructureTagInfo()
		t.tagInfo.StructureType = t.structType
	}
	t.tagInfo.ApplyTag = true
	t.tagInfo.ParentKObj = rootKObj
	t.tagInfo.ComponentKObj = t.tagInfo.NewComponentKObj(t.tagInfo.StructureType)
	t.tagInfo.ParentKObj.AddKChild(t.tagInfo.ComponentKObj)
}

// ChapterInfo describes the chapter a page belongs to. It is surfaced to the
// header, footer, and page-finalize callbacks so they can render chapter-aware
// content such as a header like "1.2 Overview".
type ChapterInfo struct {
	// Number is the dotted chapter number (e.g. "1" for a top-level chapter,
	// "1.2.3" for a nested subchapter). Unlike the internal heading text, it
	// has no trailing dot.
	Number string

	// Title is the chapter title as supplied to NewChapter/NewSubchapter,
	// without any number prefix.
	Title string

	// Level is the depth in the chapter hierarchy: 1 for a top-level chapter,
	// 2 or more for nested subchapters.
	Level uint
}

// TotalLines returns all the rows in the invoice totals table as
// description-value cell pairs.
func (i *Invoice) TotalLines() [][2]*InvoiceCell {
	rows := [][2]*InvoiceCell{i.subtotal}
	rows = append(rows, i.totalLines...)
	return append(rows, i.total)
}

// SetLink makes the line an internal link.
// The text parameter represents the text that is displayed.
// The user is taken to the specified page, at the specified x and y
// coordinates. Position 0, 0 is at the top left of the page.
func (tl *TOCLine) SetLink(page int64, x, y float64) {
	tl.linkX = x
	tl.linkY = y
	tl.linkPage = page
	color := tl.paragraph.linkStyle.Color
	tl.Number.Style.Color = color
	tl.Title.Style.Color = color
	tl.Separator.Style.Color = color
	tl.Page.Style.Color = color
}

// NewTable create a new Table with a specified number of columns.
func (c *Creator) NewTable(cols int) *Table { return newTable(cols) }

// AddAnnotation adds an annotation on a TextChunk.
func (tec *TextChunk) AddAnnotation(annotation *model.PdfAnnotation) {
	if annotation == nil {
		return
	}
	tec.annotations = append(tec.annotations, annotation)
}
