package creator

import (
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/model"
)

// MoveDown moves the drawing context down by relative displacement dy (negative goes up).
func (c *Creator) MoveDown(dy float64) { c.context.Y += dy }

// SetOptimizer sets the optimizer to optimize PDF before writing.
func (c *Creator) SetOptimizer(optimizer model.Optimizer) { c.optimizer = optimizer }

// DrawHeader sets a function to draw a header on created output pages.
func (c *Creator) DrawHeader(drawHeaderFunc func(block *Block, headerFunctionArgs HeaderFunctionArgs)) {
	c.drawHeaderFunc = drawHeaderFunc
}

// NewPage adds a new Page to the Creator and sets as the active Page.
func (c *Creator) NewPage() *model.PdfPage {
	page := c.newPage()
	c.pages = append(c.pages, page)
	c.context.Page++
	return page
}

// NewDivision returns a new Division container component.
func (c *Creator) NewDivision() *Division { return newDivision() }

// SetStructureNamespace configures the PDF 2.0 structure namespace applied to the
// generated structure tree. It takes effect only when a structure tree is produced
// (via TagComponents or SetStructTreeRoot): every generated structure element then
// carries a shared /NS referencing this namespace, the namespace is declared on the
// StructTreeRoot /Namespaces array, and the document is written as PDF 2.0.
// Passing an empty URI clears the namespace; repeat calls with the same URI are
// idempotent (they keep one shared instance). Pass model.StructureNamespacePDF2 to
// author PDF/UA-2 documents.
func (c *Creator) SetStructureNamespace(uri string) {
	if uri == "" {
		c.structNamespace = nil
		return
	}
	if c.structNamespace != nil && c.structNamespace.NS() == uri {
		return
	}
	c.structNamespace = model.NewPdfStructureNamespace(uri)
}

// SetPageSize sets the Creator's page size.  Pages that are added after this will be created with
// this Page size.
// Does not affect pages already created.
//
// Common page sizes are defined as constants.
// Examples:
// 1. c.SetPageSize(creator.PageSizeA4)
// 2. c.SetPageSize(creator.PageSizeA3)
// 3. c.SetPageSize(creator.PageSizeLegal)
// 4. c.SetPageSize(creator.PageSizeLetter)
//
// For custom sizes: Use the PPMM (points per mm) and PPI (points per inch) when defining those based on
// physical page sizes:
//
// Examples:
// 1. 10x15 sq. mm: SetPageSize(PageSize{10*creator.PPMM, 15*creator.PPMM}) where PPMM is points per mm.
// 2. 3x2 sq. inches: SetPageSize(PageSize{3*creator.PPI, 2*creator.PPI}) where PPI is points per inch.
func (c *Creator) SetPageSize(size PageSize) {
	c.pageSize = size
	c.pageWidth = size[0]
	c.pageHeight = size[1]
	pageWidth := 0.1 * c.pageWidth
	c.pageMargins.Left = pageWidth
	c.pageMargins.Right = pageWidth
	c.pageMargins.Top = pageWidth
	c.pageMargins.Bottom = pageWidth
}

// DrawFooter sets a function to draw a footer on created output pages.
func (c *Creator) DrawFooter(drawFooterFunc func(block *Block, footerFunctionArgs FooterFunctionArgs)) {
	c.drawFooterFunc = drawFooterFunc
}

// NewFilledCurve returns a instance of filled curve.
func (c *Creator) NewFilledCurve() *FilledCurve { return newFilledCurve() }

// NewTOCLine creates a new table of contents line with the default style.
func (c *Creator) NewTOCLine(number, title, page string, level uint) *TOCLine {
	return newTOCLineFromStrings(number, title, page, level, c.NewTextStyle())
}

// NewImageFromFile creates an Image from a file.
func (c *Creator) NewImageFromFile(path string) (*Image, error) { return newImageFromFile(path) }

// TagComponents instructs Creator whether or not to tag any component added after calling this function.
func (c *Creator) TagComponents(enableTagging bool) {
	c.taggingEnabled = enableTagging
	if enableTagging {
		c.docKDict = model.NewKDictionary()
		c.docKDict.S = core.MakeName(string(model.StructureTypeDocument))
		c.docKDict.ID = core.MakeString(c.docKDict.GenerateRandomID())
		if c.structTreeRoot == nil {
			c.structTreeRoot = model.NewStructTreeRoot()
		}
		c.structTreeRoot.AddKDict(c.docKDict)
	}
}

// NewPolygon creates a new polygon.
func (c *Creator) NewPolygon(points [][]contentdraw.Point) *Polygon { return newPolygon(points) }

// NewList creates a new list.
func (c *Creator) NewList() *List { return newList(c.NewTextStyle()) }

// NewLinearGradientColor creates a linear gradient color that could act as a color in other components.
func (c *Creator) NewLinearGradientColor(colorPoints []*ColorPoint) *LinearShading {
	return newLinearShading(colorPoints)
}

// Context returns the current drawing context.
func (c *Creator) Context() DrawContext { return c.context }

func (c *Creator) initContext() {
	c.context.X = precision.RoundDefault(c.pageMargins.Left)
	c.context.Y = precision.RoundDefault(c.pageMargins.Top)
	c.context.Width = precision.RoundDefault(c.pageWidth - c.pageMargins.Right - c.pageMargins.Left)
	c.context.Height = precision.RoundDefault(c.pageHeight - c.pageMargins.Bottom - c.pageMargins.Top)
	c.context.PageHeight = precision.RoundDefault(c.pageHeight)
	c.context.PageWidth = precision.RoundDefault(c.pageWidth)
	c.context.Margins = c.pageMargins
	c.context.unsupportedRune = c.UnsupportedCharacterReplacement
}

// ValidateTemplate checks the parsed template against the specified `data`
// without drawing anything, reporting unknown tags, invalid nesting and, in
// strict mode, attribute errors. See ParsedTemplate for the checks it leaves to
// RenderTemplate. The `data` parameter can be nil.
func (c *Creator) ValidateTemplate(pt *ParsedTemplate, data any) error {
	return pt.validateWith(c, c, data)
}

// NewEllipse creates a new ellipse with the center at (`xc`, `yc`),
// having the specified width and height.
// NOTE: In relative positioning mode, `xc` and `yc` are calculated using the
// current context. Furthermore, when the fit mode is set to fill the available
// space, the ellipse is scaled so that it occupies the entire context width
// while maintaining the original aspect ratio.
func (c *Creator) NewEllipse(xc, yc, width, height float64) *Ellipse {
	return newEllipse(xc, yc, width, height)
}
