package creator

import (
	"image"
	"io"
	"os"
	"slices"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/model"
)

// Write output of creator to io.Writer interface.
func (c *Creator) Write(ws io.Writer) error {
	if err := c.Finalize(); err != nil {
		return err
	}
	name := ""
	if file, ok := ws.(*os.File); ok {
		name = file.Name()
	}
	pdfWriter := model.NewPdfWriter()
	pdfWriter.SetOptimizer(c.optimizer)
	pdfWriter.SetFileName(name)
	if c.acroForm != nil {
		err := pdfWriter.SetForms(c.acroForm)
		if err != nil {
			common.Log.Debug("Failure: %v", err)
			return err
		}
	}
	if c.outlineTree != nil {
		pdfWriter.AddOutlineTree(c.outlineTree)
	} else if c.outline != nil && c.AddOutlines {
		pdfWriter.AddOutlineTree(&c.outline.ToPdfOutline().PdfOutlineTreeNode)
	}
	if c.pageLabels != nil {
		if err := pdfWriter.SetPageLabels(c.pageLabels); err != nil {
			common.Log.Debug("ERROR: Could not set page labels: %v", err)
			return err
		}
	}
	if c.subsetFonts != nil {
		for _, subsetFont := range c.subsetFonts {
			err := subsetFont.SubsetRegistered()
			if err != nil {
				common.Log.Debug("ERROR: Could not subset font: %v", err)
				return err
			}
		}
	}
	if c.taggingEnabled && c.structTreeRoot != nil {
		pdfWriter.SetCatalogMarkInfo(core.MakeDictMap(map[string]core.PdfObject{"Marked": core.MakeBool(true)}))
	}
	if c.pdfWriterAccessFunc != nil {
		err := c.pdfWriterAccessFunc(&pdfWriter)
		if err != nil {
			common.Log.Debug("Failure: %v", err)
			return err
		}
	}
	m := make(map[int64]*core.PdfIndirectObject)
	for i, page := range c.pages {
		err := pdfWriter.AddPage(page)
		if err != nil {
			common.Log.Error("Failed to add Page: %v", err)
			return err
		}
		if c.structTreeRoot != nil {
			if c.taggingEnabled {
				page.SetStructParentsKey(i)
			}
			pageIndirectObject, err := pdfWriter.GetPageIndirectObject(i)
			if err != nil {
				common.Log.Debug("ERROR: Could not get page indirect object %v", err)
			} else {
				m[int64(i+1)] = pageIndirectObject
			}
		}
	}
	if c.structTreeRoot != nil {
		if c.structNamespace != nil && !slices.Contains(c.structTreeRoot.Namespaces, c.structNamespace) {
			c.structTreeRoot.AddNamespace(c.structNamespace)
		}
		var fn func(kDict *model.KDict, n int64)
		fn = func(kDict *model.KDict, n int64) {
			if kDict == nil {
				return
			}
			n2 := n
			if pageNumber := kDict.GetPageNumber(); pageNumber > 0 {
				n2 = pageNumber
			}
			if n2 > 0 {
				if page, ok := m[n2]; ok {
					kDict.SetPage(page)
				}
			}
			if c.structNamespace != nil {
				kDict.SetNamespace(c.structNamespace)
			}
			for _, kValue := range kDict.GetChildren() {
				if kDict := kValue.GetKDict(); kDict != nil {
					fn(kDict, n2)
				}
			}
		}
		for _, kDict := range c.structTreeRoot.K {
			fn(kDict, 0)
		}
		if err := pdfWriter.SetCatalogStructTreeRoot(c.structTreeRoot.ToPdfObject()); err != nil {
			common.Log.Debug("ERROR: Could not set StructTreeRoot: %v", err)
			return err
		}
		if c.structNamespace != nil {
			pdfWriter.SetVersion(2, 0)
		}
	}
	if c.viewerPreferences != nil {
		if err := pdfWriter.SetCatalogViewerPreferences(c.viewerPreferences.ToPdfObject()); err != nil {
			common.Log.Debug("ERROR: Could not set ViewerPreferences: %v", err)
			return err
		}
	}
	if c.language != "" {
		if err := pdfWriter.SetCatalogLanguage(core.MakeString(c.language)); err != nil {
			common.Log.Debug("ERROR: Could not set catalog language: %v", err)
			return err
		}
	}
	err := pdfWriter.Write(ws)
	if err != nil {
		return err
	}
	return nil
}

// NewTextStyle creates a new text style object which can be used to style
// chunks of text.
// Default attributes:
// Font: the creator default font (Helvetica, unless changed via SetDefaultFont)
// Font size: 10
// Encoding: the font's encoding (WinAnsiEncoding for the standard fonts)
// Text color: black
func (c *Creator) NewTextStyle() TextStyle { return defaultTextStyle(c.fontRegular) }

// NewGrid creates a new Grid with a specified number of columns.
func (c *Creator) NewGrid(cols int) *Grid { return newGrid(cols) }

// NewFunctionBasedGradient creates a function-based (Type 1) gradient color from a
// PostScript calculator program, which maps a point in the shading's domain to
// the colour components at that point. The program's output count selects the
// colourspace: 1 for DeviceGray, 3 for DeviceRGB and 4 for DeviceCMYK.
func (c *Creator) NewFunctionBasedGradient(content string) *FunctionBasedShading {
	return newFunctionBasedShading(content)
}

// RenderTemplate draws the parsed template into the creator using the specified
// `data`. The handle comes from ParseTemplate and can be rendered repeatedly,
// with different data, and into different creators. The `data` parameter can be
// nil.
func (c *Creator) RenderTemplate(pt *ParsedTemplate, data any) error {
	return pt.renderInto(c, c, data)
}

// SetPageMargins sets the page margins: left, right, top, bottom.
// The default page margins are 10% of document width.
func (c *Creator) SetPageMargins(left, right, top, bottom float64) {
	c.pageMargins.Left = left
	c.pageMargins.Right = right
	c.pageMargins.Top = top
	c.pageMargins.Bottom = bottom
}

func (c *Creator) newPage() *model.PdfPage {
	pdfPage := model.NewPdfPage()
	urx := c.pageSize[0]
	ury := c.pageSize[1]
	rect := model.PdfRectangle{Llx: 0, Lly: 0, Urx: urx, Ury: ury}
	pdfPage.MediaBox = &rect
	c.pageWidth = urx
	c.pageHeight = ury
	c.initContext()
	return pdfPage
}

// SetViewerPreferences sets the viewer preferences for the PDF document.
func (c *Creator) SetViewerPreferences(viewerPreferences *model.ViewerPreferences) {
	c.viewerPreferences = viewerPreferences
}

// MoveTo moves the drawing context to absolute coordinates (x, y).
func (c *Creator) MoveTo(x, y float64) { c.context.X = x; c.context.Y = y }

func (c *Creator) wrapPageIfNeeded(pageIfNeeded *model.PdfPage) (*model.PdfPage, error) {
	allContentStreams, err := pageIfNeeded.GetAllContentStreams()
	if err != nil {
		return nil, err
	}
	contentStreamParser := contentstream.NewContentStreamParser(allContentStreams)
	contentStreamOperations, err := contentStreamParser.Parse()
	if err != nil {
		return nil, err
	}
	if !contentStreamOperations.HasUnclosedQ() {
		return nil, nil
	}
	contentStreamOperations.WrapIfNeeded()
	stream, err := core.MakeStream(contentStreamOperations.Bytes(), core.NewFlateEncoder())
	if err != nil {
		return nil, err
	}
	pageIfNeeded.Contents = core.MakeArray(stream)
	return pageIfNeeded, nil
}

// MoveX moves the drawing context to absolute position x.
func (c *Creator) MoveX(x float64) { c.context.X = x }

// NewStyledParagraph creates a new styled paragraph.
// Default attributes:
// Font: the creator default font (Helvetica, unless changed via SetDefaultFont)
// Font size: 10
// Encoding: the font's encoding (WinAnsiEncoding for the standard fonts)
// Wrap: enabled
// Text color: black
func (c *Creator) NewStyledParagraph() *StyledParagraph { return newStyledParagraph(c.NewTextStyle()) }

// MoveY moves the drawing context to absolute position y.
func (c *Creator) MoveY(y float64) { c.context.Y = y }

// NewImage create a new image from an OpenPDF image (model.Image).
func (c *Creator) NewImage(img *model.Image) (*Image, error) { return newImage(img) }

// NewTOC creates a new table of contents.
func (c *Creator) NewTOC(title string) *TOC {
	textStyle := c.NewTextStyle()
	textStyle.Font = c.fontBold
	return newTOC(title, c.NewTextStyle(), textStyle)
}

// DrawTemplate renders the template provided through the specified reader,
// using the specified `data` and `options`.
// Creator templates are first executed as text/template *Template instances,
// so the specified `data` is inserted within the template.
// The second phase of processing is actually parsing the template, translating
// it into creator components and rendering them using the provided options.
// Both the `data` and `options` parameters can be nil.
func (c *Creator) DrawTemplate(r io.Reader, data any, options *TemplateOptions) error {
	return drawTemplate(c, r, data, options, c)
}

// SetPdfWriterAccessFunc sets a PdfWriter access function/hook.
// Exposes the PdfWriter just prior to writing the PDF.  Can be used to encrypt the output PDF, etc.
//
// Example of encrypting with a user/owner password "password"
// Prior to calling c.WriteFile():
//
//	c.SetPdfWriterAccessFunc(func(w *model.PdfWriter) error {
//		userPass := []byte("password")
//		ownerPass := []byte("password")
//		err := w.Encrypt(userPass, ownerPass, nil)
//		return err
//	})
func (c *Creator) SetPdfWriterAccessFunc(pdfWriterAccessFunc func(writer *model.PdfWriter) error) {
	c.pdfWriterAccessFunc = pdfWriterAccessFunc
}

// SetOutlineTree adds the specified outline tree to the PDF file generated
// by the creator. Adding an external outline tree disables the automatic
// generation of outlines done by the creator for the relevant components.
func (c *Creator) SetOutlineTree(outlineTree *model.PdfOutlineTreeNode) {
	c.outlineTree = outlineTree
}

// NewLine creates a new line between (x1, y1) to (x2, y2),
// using default attributes.
// NOTE: In relative positioning mode, `x1` and `y1` are calculated using the
// current context and `x2`, `y2` are used only to calculate the position of
// the second point in relation to the first one (used just as a measurement
// of size). Furthermore, when the fit mode is set to fill the context width,
// `x2` is set to the right edge coordinate of the context.
func (c *Creator) NewLine(x1, y1, x2, y2 float64) *Line { return newLine(x1, y1, x2, y2) }

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

func (c *Creator) getActivePage() *model.PdfPage {
	if c.activePage == nil {
		if len(c.pages) == 0 {
			return nil
		}
		return c.pages[len(c.pages)-1]
	}
	return c.activePage
}

// NewTable create a new Table with a specified number of columns.
func (c *Creator) NewTable(cols int) *Table { return newTable(cols) }

// NewImageFromData creates an Image from image data.
func (c *Creator) NewImageFromData(data []byte) (*Image, error) { return newImageFromBytes(data) }

func (c *Creator) setActivePage(activePage *model.PdfPage) { c.activePage = activePage }

// NewStyledTOCLine creates a new table of contents line with the provided style.
func (c *Creator) NewStyledTOCLine(number, title, page TextChunk, level uint, style TextStyle) *TOCLine {
	return newTOCLineFromChunks(number, title, page, level, style)
}

// Draw processes the specified Drawable widget and generates blocks that can
// be rendered to the output document. The generated blocks can span over one
// or more pages. Additional pages are added if the contents go over the current
// page. Each generated block is assigned to the creator page it will be
// rendered to. In order to render the generated blocks to the creator pages,
// call Finalize, Write or WriteToFile.
func (c *Creator) Draw(d Drawable) error {
	if c.getActivePage() == nil {
		c.NewPage()
	}
	var (
		pagesLen      int64
		mcid          int64
		componentKObj *model.KDict
		n             int
	)
	if c.taggingEnabled {
		pagesLen = int64(len(c.pages))
		d.SetStructPageNumber(&pagesLen)
		mcid = c.pageMCIDs[pagesLen]
		n = len(c.docKDict.GetChildren())
		switch d2 := d.(type) {
		case *Table:
			d2.AddTag(c.docKDict)
			d2.SetMarkedContentID(mcid)
			componentKObj = d2.tagInfo.ComponentKObj
		case *Grid:
			d2.AddTag(c.docKDict)
			d2.SetMarkedContentID(mcid)
			componentKObj = d2.tagInfo.ComponentKObj
		case *List:
			d2.AddTag(c.docKDict)
			d2.SetMarkedContentID(mcid)
		case *Division:
			d2.AddTag(c.docKDict)
			d2.SetMarkedContentID(mcid)
			componentKObj = d2.tagInfo.ComponentKObj
		default:
			d2.SetMarkedContentID(mcid)
			kDict, err := d2.GenerateKDict()
			if err != nil {
				return err
			}
			if kDict != nil {
				c.docKDict.AddKChild(kDict)
			}
			componentKObj = kDict
		}
	}
	pageBlocks, ctx, err := d.GeneratePageBlocks(c.context)
	if err != nil {
		return err
	}
	if len(ctx.errs) > 0 {
		c.Errors = append(c.Errors, ctx.errs...)
	}
	if c.taggingEnabled {
		if componentKObj == nil {
			if children := c.docKDict.GetChildren(); len(children) > n {
				componentKObj = children[len(children)-1].GetKDict()
			}
		}
		if componentKObj != nil {
			mcid2 := mcid + 1
			if n := maxMCID(componentKObj); n >= mcid2 {
				mcid2 = n + 1
			}
			c.pageMCIDs[pagesLen] = mcid2
		}
	}
	for i, pageBlock := range pageBlocks {
		if i > 0 {
			c.NewPage()
		}
		activePage := c.getActivePage()
		if block, ok := c.blocks[activePage]; ok {
			if err := block.mergeBlocks(pageBlock); err != nil {
				return err
			}
			if err := mergeColorspaces(pageBlock.resources, block.resources); err != nil {
				return err
			}
		} else {
			c.blocks[activePage] = pageBlock
		}
	}
	c.context.X = ctx.X
	c.context.Y = ctx.Y
	c.context.Height = precision.RoundDefault(ctx.PageHeight - ctx.Y - ctx.Margins.Bottom)
	return nil
}

// WriteToFile writes the Creator output to file specified by path.
func (c *Creator) WriteToFile(outputPath string) error {
	ws, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer ws.Close()
	return c.Write(ws)
}

// SetTOC sets the table of content component of the creator.
// This method should be used when building a custom table of contents.
func (c *Creator) SetTOC(toc *TOC) {
	if toc == nil {
		return
	}
	c.toc = toc
}

// NewImageFromGoImage creates an Image from a go image.Image data structure.
func (c *Creator) NewImageFromGoImage(goimg image.Image) (*Image, error) {
	return newImageFromGoImage(goimg)
}

// MoveRight moves the drawing context right by relative displacement dx (negative goes left).
func (c *Creator) MoveRight(dx float64) { c.context.X += dx }

// SetDefaultFont sets the fonts used by default by creator components which
// have no font specified, replacing the standard Helvetica and Helvetica-Bold
// fonts. This covers text styles created via NewTextStyle, components built
// on them (e.g. NewStyledParagraph, NewTOC, NewInvoice) and template
// components with no `font` attribute. Setting a composite font enables
// scripts the standard fonts do not cover, such as Cyrillic or CJK. The
// method should be called before any components are created or drawn. A nil
// argument leaves the corresponding default font unchanged.
func (c *Creator) SetDefaultFont(fontRegular, fontBold *model.PdfFont) {
	fontRegular2 := c.fontRegular
	fontBold2 := c.fontBold
	if fontRegular != nil {
		c.fontRegular = fontRegular
	}
	if fontBold != nil {
		c.fontBold = fontBold
	}
	c.toc.replaceFonts(fontRegular2, c.fontRegular, fontBold2, c.fontBold)
}

// PageFinalize sets a function to be called for each page before finalization
// (i.e. the last stage of page processing before they get written out).
// The callback function allows final touch-ups for each page, and it
// provides information that might not be known at other stages of designing
// the document (e.g. the total number of pages). Unlike the header/footer
// functions, which are limited to the top/bottom margins of the page, the
// finalize function can be used draw components anywhere on the current page.
func (c *Creator) PageFinalize(pageFinalizeFunc func(pageFinalizeFunctionArgs PageFinalizeFunctionArgs) error) {
	c.pageFinalizeFunc = pageFinalizeFunc
}

// NewInvoice returns an instance of an empty invoice.
func (c *Creator) NewInvoice() *Invoice {
	textStyle := c.NewTextStyle()
	textStyle.Font = c.fontBold
	return newInvoice(c.NewTextStyle(), textStyle)
}

// SetPageLabels adds the specified page labels to the PDF file generated
// by the creator. See section 12.4.2 "Page Labels" (p. 382 PDF32000_2008).
// NOTE: for existing PDF files, the page label ranges object can be obtained
// using the model.PDFReader's GetPageLabels method.
func (c *Creator) SetPageLabels(pageLabels core.PdfObject) { c.pageLabels = pageLabels }

// NewChapter creates a new chapter with the specified title as the heading.
func (c *Creator) NewChapter(title string) *Chapter {
	c.chapterCount++
	textStyle := c.NewTextStyle()
	textStyle.FontSize = 16
	fn := func(chapter *Chapter, page int) {
		for i := range c.chapters {
			if c.chapters[i].chapter == chapter {
				c.chapters[i].page = page
				return
			}
		}
		c.chapters = append(c.chapters, chapterInfoEntry{chapter: chapter, page: page})
	}
	return newChapter(nil, c.toc, c.outline, fn, title, c.chapterCount, textStyle)
}

// NewRadialGradientColor creates a radial gradient color that could act as a color in other componenents.
// Note: The innerRadius must be smaller than outerRadius for the circle to render properly.
func (c *Creator) NewRadialGradientColor(x float64, y float64, innerRadius float64, outerRadius float64, colorPoints []*ColorPoint) *RadialShading {
	return newRadialShading(x, y, innerRadius, outerRadius, colorPoints)
}
