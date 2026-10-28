// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"fmt"
	"image"
	"log"
	"math"
	"reflect"
	"strings"
	"text/template/parse"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

// SetStructureType sets the structure type for the table.
func (t *Table) SetStructureType(structureType model.StructureType) {
	if t.tagInfo == nil {
		t.tagInfo = model.NewStructureTagInfo()
	}
	t.tagInfo.StructureType = structureType
	if t.tagInfo.ComponentKObj != nil {
		if t.tagInfo.ParentKObj != nil {
			t.tagInfo.ParentKObj.RemoveKChild(t.tagInfo.ComponentKObj)
		}
		t.tagInfo.ComponentKObj = t.tagInfo.NewComponentKObj(structureType)
		t.tagInfo.ParentKObj.AddKChild(t.tagInfo.ComponentKObj)
	}
}

func parseBackgroundTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseBackground(node)
}

// GetMargins returns the Image's margins: left, right, top, bottom.
func (im *Image) GetMargins() (float64, float64, float64, float64) {
	return im.margins.Left, im.margins.Right, im.margins.Top, im.margins.Bottom
}

// SellerAddress returns the seller address used in the invoice template.
func (i *Invoice) SellerAddress() *InvoiceAddress { return i.sellerAddress }

// SetBorderColor sets the border color.
func (p *Polygon) SetBorderColor(color Color) { p.polygon.BorderColor = toPdfColor(color) }

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

func addEscapeToPipe(pipeNode *parse.PipeNode) {
	if pipeNode == nil || len(pipeNode.Decl) > 0 || len(pipeNode.Cmds) == 0 {
		return
	}
	if pipeEndsWithRaw(pipeNode) {
		return
	}
	commandNode := &parse.CommandNode{NodeType: parse.NodeCommand}
	commandNode.Args = append(commandNode.Args, parse.NewIdentifier(xmlEscapeFuncName))
	pipeNode.Cmds = append(pipeNode.Cmds, commandNode)
}

// TextOverflow determines the behavior of paragraph text which does
// not fit in the available space.
type TextOverflow int

// AddLine adds a new line with the provided style to the table of contents.
func (to *TOC) AddLine(line *TOCLine) *TOCLine {
	if line == nil {
		return nil
	}
	to.lines = append(to.lines, line)
	return line
}

// SkipRows skips over a specified number of rows in the table.
func (t *Table) SkipRows(num int) {
	n := num*t.cols - 1
	if n < 0 {
		common.Log.Debug("Table: cannot skip back to previous cells")
		return
	}
	for i := 0; i < n; i++ {
		t.NewCell()
	}
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

func isNilValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return value.IsNil()
	}
	return false
}

// TextVerticalAlignment controls the vertical position of the text
// in a styled paragraph.
type TextVerticalAlignment int

// MarkAsArtifact marks the image as an artifact of the specified type.
//
// This would override regular structure tagging and accessibility
// properties, and the image will be treated as an artifact in the PDF.
func (im *Image) MarkAsArtifact(artifactType model.ArtifactType) *model.Artifact {
	im.artifact = &model.Artifact{Type: artifactType}
	return im.artifact
}

func cropAlphaData(img *model.Image, rect, rect2 image.Rectangle) []byte {
	alphaData := img.AlphaData()
	ok := rect != rect2
	if len(alphaData) == 0 && !ok {
		return nil
	}
	width, height := int(img.Width), int(img.Height)
	n := 0
	if len(alphaData) != 0 {
		switch img.BitsPerComponent {
		case 8:
			n = 1
		case 16:
			n = 2
		}
		if n == 0 {
			return nil
		}
		if len(alphaData) != n*width*height {
			common.Log.Debug("Alpha channel has %d bytes, expected %d; missing samples are treated as opaque", len(alphaData), n*width*height)
		}
	}
	dx := rect.Dx()
	data := make([]byte, dx*rect.Dy())
	for i := rect2.Min.Y; i < rect2.Max.Y; i++ {
		data2 := data[(i-rect2.Min.Y)*dx:][:rect2.Dx()]
		for j := range data2 {
			n2 := n * (i*width + rect2.Min.X + j)
			if n > 0 && n2 < len(alphaData) {
				data2[j] = alphaData[n2]
			} else {
				data2[j] = 0xff
			}
		}
	}
	for _, b := range data {
		if b != 0xff {
			return data
		}
	}
	return nil
}

// SetMargins sets the margins of the ellipse.
// NOTE: ellipse margins are only applied if relative positioning is used.
func (e *Ellipse) SetMargins(left, right, top, bottom float64) {
	e.margins.Left = left
	e.margins.Right = right
	e.margins.Top = top
	e.margins.Bottom = bottom
}

// NoteStyle returns the style properties used to render the content of the
// invoice note sections.
func (i *Invoice) NoteStyle() TextStyle { return i.noteStyle }

// String implements error interface.
func (ure UnsupportedRuneError) Error() string { return ure.Message }

// ColorGrayFromHex converts color hex code to gray color for using with creator.
// NOTE: If there is a problem interpreting the string, then will use black color and log a debug message.
// Example hex code: #ff -> white.
func ColorGrayFromHex(hexStr string) Color {
	colorGray := grayColor{}
	if (len(hexStr) != 2 && len(hexStr) != 3) || hexStr[0] != '#' {
		common.Log.Debug("Invalid hex code: %s", hexStr)
		return colorGray
	}
	var gray int
	if len(hexStr) == 2 {
		var shortGray int
		n, err := fmt.Sscanf(hexStr, "#%1x", &shortGray)
		if err != nil {
			common.Log.Debug("Invalid hex code: %s, error: %v", hexStr, err)
			return colorGray
		}
		if n != 1 {
			common.Log.Debug("Invalid hex code: %s", hexStr)
			return colorGray
		}
		gray = shortGray*16 + shortGray
	} else {
		n, err := fmt.Sscanf(hexStr, "#%2x", &gray)
		if err != nil {
			common.Log.Debug("Invalid hex code: %s", hexStr)
			return colorGray
		}
		if n != 1 {
			common.Log.Debug("Invalid hex code: %s, n != 3 (%d)", hexStr, n)
			return colorGray
		}
	}
	colorGray.gray = float64(gray) / 255.0
	return colorGray
}

// Crop crops the Image to the specified bounds. Parts of the rectangle
// outside the image are padded with transparent pixels. The alpha channel,
// if any, is cropped along with the color samples, as is the matching region
// of a soft or stencil mask set on the image, and the image is rebuilt from
// the cropped data on its next draw.
func (im *Image) Crop(x0, y0, x1, y1 int) {
	rect := image.Rect(x0, y0, x1, y1)
	img, err := cropImage(im.img, rect)
	if err != nil {
		log.Fatalf("Error cropping image: %v", err)
	}
	if mask := im.mask; mask != nil && mask.img != nil && im.img.Width > 0 && im.img.Height > 0 {
		width, height := float64(mask.img.Width), float64(mask.img.Height)
		width2, height2 := float64(im.img.Width), float64(im.img.Height)
		rect2 := image.Rect(int(math.Floor(float64(rect.Min.X)*width/width2)), int(math.Floor(float64(rect.Min.Y)*height/height2)), int(math.Ceil(float64(rect.Max.X)*width/width2)), int(math.Ceil(float64(rect.Max.Y)*height/height2)))
		if rect2.Dx() < 1 {
			rect2.Max.X = rect2.Min.X + 1
		}
		if rect2.Dy() < 1 {
			rect2.Max.Y = rect2.Min.Y + 1
		}
		img, err := cropImage(mask.img, rect2)
		if err != nil {
			log.Fatalf("Error cropping image mask: %v", err)
		}
		img.SetAlpha(nil)
		mask.img = img
	}
	width := float64(img.Width)
	height := float64(img.Height)
	im.img = img
	im.origWidth = width
	im.origHeight = height
	im.width = width
	im.height = height
	im.xobj = nil
}

// DrawFooter sets a function to draw a footer on created output pages.
func (c *Creator) DrawFooter(drawFooterFunc func(block *Block, footerFunctionArgs FooterFunctionArgs)) {
	c.drawFooterFunc = drawFooterFunc
}

func splitUnitSuffix(value2 string) (value, unit string) {
	if value2 == "" || (value2[len(value2)-1] >= '0' && value2[len(value2)-1] <= '9') {
		return value2, ""
	}
	value = value2
	for _, lengthUnit := range lengthUnits {
		if strings.Contains(value, lengthUnit) {
			unit = lengthUnit
		}
		value = strings.TrimSuffix(value, lengthUnit)
	}
	return
}

func newCurvePolygon(rings [][]contentdraw.CubicBezierCurve) *CurvePolygon {
	return &CurvePolygon{polygon: &contentdraw.CurvePolygon{Rings: rings}, fillOpacity: 1.0, borderOpacity: 1.0}
}

// SetPos sets the grid positioning to absolute mode and specifies the upper-left corner
// coordinates as (x,y).
// Note that this is only sensible to use when the grid does not wrap over multiple pages.
// TODO: Should be able to set width too (not just based on context/relative positioning mode).
func (g *Grid) SetPos(x, y float64) {
	g.positioning = PositionAbsolute
	g.xPos = x
	g.yPos = y
}

func (b *Block) transform(matrix transform.Matrix) {
	operations := contentstream.NewContentCreator().Add_cm(matrix[0], matrix[1], matrix[3], matrix[4], matrix[6], matrix[7]).Operations()
	*b.contents = append(*operations, *b.contents...)
	b.contents.WrapIfNeeded()
}

// AddPatternResource adds pattern dictionary inside the resources dictionary.
func (fbs *FunctionBasedShading) AddPatternResource(block *Block) (name core.PdfObjectName, err error) {
	pdfShadingPattern, err := fbs.ToPdfShadingPattern()
	if err != nil {
		return "", err
	}
	return addShadingPatternResource(block, pdfShadingPattern.ToPdfObject())
}
