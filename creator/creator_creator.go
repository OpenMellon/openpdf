package creator

import (
	"errors"
	"math"
	"strconv"

	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

// Creator is a wrapper around functionality for creating PDF reports and/or adding new
// content onto imported PDF pages, etc.
type Creator struct {
	// Errors keeps error messages that should not interrupt pdf processing and to be checked later.
	Errors []error

	// UnsupportedCharacterReplacement is character that will be used to replace unsupported glyph.
	// The value will be passed to drawing context.
	UnsupportedCharacterReplacement rune
	pages                           []*model.PdfPage
	blocks                          map[*model.PdfPage]*Block
	pageTransforms                  map[*model.PdfPage]*pageTransformations
	activePage                      *model.PdfPage
	pageSize                        PageSize
	context                         DrawContext
	pageMargins                     Margins
	pageWidth, pageHeight           float64
	chapterCount                    int
	chapters                        []chapterInfoEntry
	genFrontPageFunc                func(frontpageFunctionArgs FrontpageFunctionArgs)
	genTOCFunc                      func(toc *TOC) error
	drawHeaderFunc                  func(block *Block, headerFunctionArgs HeaderFunctionArgs)
	drawFooterFunc                  func(block *Block, footerFunctionArgs FooterFunctionArgs)
	pageFinalizeFunc                func(pageFinalizeFunctionArgs PageFinalizeFunctionArgs) error
	pdfWriterAccessFunc             func(writer *model.PdfWriter) error
	finalized                       bool

	// Controls whether a table of contents will be generated.
	AddTOC bool

	// CustomTOC specifies if the TOC is rendered by the user.
	// When the `CustomTOC` field is set to `true`, the default TOC component is not rendered.
	// Instead the TOC is drawn by the user, in the callback provided to
	// the `Creator.CreateTableOfContents` method.
	// If `CustomTOC` is set to `false`, the callback provided to
	// `Creator.CreateTableOfContents` customizes the style of the automatically generated TOC component.
	CustomTOC bool
	toc       *TOC

	// Controls whether outlines will be generated.
	AddOutlines       bool
	outline           *model.Outline
	outlineTree       *model.PdfOutlineTreeNode
	acroForm          *model.PdfAcroForm
	pageLabels        core.PdfObject
	optimizer         model.Optimizer
	subsetFonts       []*model.PdfFont
	fontRegular       *model.PdfFont
	fontBold          *model.PdfFont
	taggingEnabled    bool
	docKDict          *model.KDict
	pageMCIDs         map[int64]int64
	structTreeRoot    *model.StructTreeRoot
	structNamespace   *model.PdfStructureNamespace
	viewerPreferences *model.ViewerPreferences
	language          string

	// AutofixPageContentStream indicates whether the creator should attempt to fix
	// page content streams that have unclosed `q` and `Q` commands.
	// The default value is `true`.
	// When set to `false`, the creator will skip the content stream checking and wrapping.
	// This will speed up and optimize memory usage the creation of PDF, the drawback is
	// need to ensure that the source of PDF content streams are well-formed.
	AutofixPageContentStream bool
}

// NewPolyline creates a new polyline.
func (c *Creator) NewPolyline(points []contentdraw.Point) *Polyline { return newPolyline(points) }

// SetStructTreeRoot sets the structure tree root to be appended in the document that will be created.
func (c *Creator) SetStructTreeRoot(structTreeRoot *model.StructTreeRoot) {
	c.structTreeRoot = structTreeRoot
}

// Height returns the current page height.
func (c *Creator) Height() float64 { return c.pageHeight }

// NewRectangle creates a new rectangle with the left corner at (`x`, `y`),
// having the specified width and height.
// NOTE: In relative positioning mode, `x` and `y` are calculated using the
// current context. Furthermore, when the fit mode is set to fill the available
// space, the rectangle is scaled so that it occupies the entire context width
// while maintaining the original aspect ratio.
func (c *Creator) NewRectangle(x, y, width, height float64) *Rectangle {
	return newRectangle(x, y, width, height)
}

// NewCurve returns new instance of Curve between points (x1,y1) and (x2, y2) with control point (cx,cy).
func (c *Creator) NewCurve(x1, y1, cx, cy, x2, y2 float64) *Curve {
	return newCurve(x1, y1, cx, cy, x2, y2)
}

// Width returns the current page width.
func (c *Creator) Width() float64 { return c.pageWidth }

// EnableFontSubsetting enables font subsetting for `font` when the creator output is written to file.
// Embeds only the subset of the runes/glyphs that are actually used to display the file.
// Subsetting can reduce the size of fonts significantly.
func (c *Creator) EnableFontSubsetting(font *model.PdfFont) {
	c.subsetFonts = append(c.subsetFonts, font)
}

// CreateFrontPage sets a function to generate a front Page.
//
// Chapters drawn inside the callback (via NewChapter + Draw) are not tracked
// by Creator.ChapterForPage and will not appear in header/footer callback
// args. The front page is expected to hold cover content, not chapter bodies.
func (c *Creator) CreateFrontPage(genFrontPageFunc func(frontpageFunctionArgs FrontpageFunctionArgs)) {
	c.genFrontPageFunc = genFrontPageFunc
}

// SetForms adds an Acroform to a PDF file.  Sets the specified form for writing.
func (c *Creator) SetForms(form *model.PdfAcroForm) error { c.acroForm = form; return nil }

// TOC returns the table of contents component of the creator.
func (c *Creator) TOC() *TOC { return c.toc }

// CreateTableOfContents sets a function to generate table of contents.
//
// Chapters drawn inside the callback (via NewChapter + Draw) are not tracked
// by Creator.ChapterForPage and will not appear in header/footer callback
// args. The callback is expected to customize the TOC layout, not add
// chapter bodies.
func (c *Creator) CreateTableOfContents(genTOCFunc func(toc *TOC) error) {
	c.genTOCFunc = genTOCFunc
}

// SetLanguage sets the language identifier that will be stored inside document catalog.
func (c *Creator) SetLanguage(language string) { c.language = language }

// AddPage adds the specified page to the creator.
// NOTE: If the page has a Rotate flag, the creator will take care of
// transforming the contents to maintain the correct orientation.
func (c *Creator) AddPage(page *model.PdfPage) error {
	if c.AutofixPageContentStream {
		pageIfNeeded, err := c.wrapPageIfNeeded(page)
		if err != nil {
			return err
		}
		if pageIfNeeded != nil {
			page = pageIfNeeded
		}
	}
	mediaBox, err := page.GetMediaBox()
	if err != nil {
		common.Log.Debug("Failed to get page mediabox: %v", err)
		return err
	}
	mediaBox.Normalize()
	llx, lly := mediaBox.Llx, mediaBox.Lly
	rect := mediaBox
	if cropBox := page.CropBox; cropBox != nil && *cropBox != *mediaBox {
		cropBox.Normalize()
		llx, lly = cropBox.Llx, cropBox.Lly
		rect = cropBox
	}
	identityMatrix := transform.IdentityMatrix()
	rotate, err := page.GetRotate()
	if err != nil {
		common.Log.Debug("ERROR: %s - ignoring and assuming no rotation\n", err.Error())
	}
	isRightAngle := rotate%360 != 0 && rotate%90 == 0
	if isRightAngle {
		angle := float64((360 + rotate%360) % 360)
		switch angle {
		case 90:
			identityMatrix = identityMatrix.Translate(rect.Width(), 0)
		case 180:
			identityMatrix = identityMatrix.Translate(rect.Width(), rect.Height())
		case 270:
			identityMatrix = identityMatrix.Translate(0, rect.Height())
		}
		identityMatrix = identityMatrix.Mult(transform.RotationMatrix(angle * math.Pi / 180))
		identityMatrix = identityMatrix.Round(0.000001)
		rect2 := transformRectangle(rect, identityMatrix)
		rect = rect2
		rect.Normalize()
	}
	if llx != 0 || lly != 0 {
		identityMatrix = transform.TranslationMatrix(llx, lly).Mult(identityMatrix)
	}
	if !identityMatrix.Identity() {
		identityMatrix = identityMatrix.Round(0.000001)
		c.pageTransforms[page] = &pageTransformations{matrix: &identityMatrix}
	}
	c.pageWidth = rect.Width()
	c.pageHeight = rect.Height()
	c.initContext()
	c.pages = append(c.pages, page)
	c.context.Page++
	return nil
}

// NewPageBreak create a new page break.
func (c *Creator) NewPageBreak() *PageBreak { return newPageBreak() }

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

// Flip flips the active page on the specified axes.
// If `flipH` is true, the page is flipped horizontally. Similarly, if `flipV`
// is true, the page is flipped vertically. If both are true, the page is
// flipped both horizontally and vertically.
// NOTE: the flip transformations are applied when the creator is finalized,
// which is at write time in most cases.
func (c *Creator) Flip(flipH, flipV bool) error {
	activePage := c.getActivePage()
	if activePage == nil {
		return errors.New("no page active")
	}
	pageTransform, ok := c.pageTransforms[activePage]
	if !ok {
		pageTransform = &pageTransformations{}
		c.pageTransforms[activePage] = pageTransform
	}
	pageTransform.flipH = flipH
	pageTransform.flipV = flipV
	return nil
}

// RotateDeg rotates the current active page by angle degrees.  An error is returned on failure,
// which can be if there is no currently active page, or the angleDeg is not a multiple of 90 degrees.
func (c *Creator) RotateDeg(angleDeg int64) error {
	activePage := c.getActivePage()
	if activePage == nil {
		common.Log.Debug("Fail to rotate: no page currently active")
		return errors.New("no page active")
	}
	if angleDeg%90 != 0 {
		common.Log.Debug("ERROR: Page rotation angle not a multiple of 90")
		return errors.New("range check error")
	}
	var rotate int64
	if activePage.Rotate != nil {
		rotate = *activePage.Rotate
	}
	rotate += angleDeg
	activePage.Rotate = &rotate
	return nil
}

// NewPolyBezierCurve creates a new composite Bezier (polybezier) curve.
func (c *Creator) NewPolyBezierCurve(curves []contentdraw.CubicBezierCurve) *PolyBezierCurve {
	return newPolyBezierCurve(curves)
}

// NewCurvePolygon creates a new curve polygon.
func (c *Creator) NewCurvePolygon(rings [][]contentdraw.CubicBezierCurve) *CurvePolygon {
	return newCurvePolygon(rings)
}

// GetOptimizer returns current PDF optimizer.
func (c *Creator) GetOptimizer() model.Optimizer { return c.optimizer }
