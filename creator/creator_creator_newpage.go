// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"image"
	imagedraw "image/draw"
	"strconv"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

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

// SetSideBorderColor sets the cell's side border color.
func (tc *TableCell) SetSideBorderColor(side CellBorderSide, col Color) {
	switch side {
	case CellBorderSideAll:
		tc.borderColorTop = col
		tc.borderColorBottom = col
		tc.borderColorLeft = col
		tc.borderColorRight = col
	case CellBorderSideTop:
		tc.borderColorTop = col
	case CellBorderSideBottom:
		tc.borderColorBottom = col
	case CellBorderSideLeft:
		tc.borderColorLeft = col
	case CellBorderSideRight:
		tc.borderColorRight = col
	}
}

// SetOpacity sets the cell's opacity in the range 0-1.
func (gc *GridCell) SetOpacity(opacity float64) { gc.opacity = opacity }

// SetViewerPreferences sets the viewer preferences for the PDF document.
func (c *Creator) SetViewerPreferences(viewerPreferences *model.ViewerPreferences) {
	c.viewerPreferences = viewerPreferences
}

func addShadingPatternResource(block *Block, pattern core.PdfObject) (core.PdfObjectName, error) {
	i := 1
	keyName := core.PdfObjectName("P" + strconv.Itoa(i))
	for block.resources.HasPatternByName(keyName) {
		i++
		keyName = core.PdfObjectName("P" + strconv.Itoa(i))
	}
	if err := block.resources.SetPatternByName(keyName, pattern); err != nil {
		return "", err
	}
	return keyName, nil
}

func (tp *templateProcessor) parseStyledParagraph(node *templateNode) (any, error) {
	styledParagraph := tp.creator.NewStyledParagraph()
	if err := tp.applyAttrs(node, styledParagraph, styledParagraphAttrs); err != nil {
		return nil, err
	}
	return styledParagraph, nil
}

// GetPosition returns the 1-based row and column the cell starts from.
// Consistent with GridCell.GetPosition.
func (tc *TableCell) GetPosition() (row, col int) { return tc.row, tc.col }

// MoveTo moves the drawing context to absolute coordinates (x, y).
func (c *Creator) MoveTo(x, y float64) { c.context.X = x; c.context.Y = y }

func newGraphicSVG(root *GraphicSVGElement) (*GraphicSVG, error) {
	return &GraphicSVG{root: root, positioning: PositionRelative, margins: Margins{Top: 10, Bottom: 10}, taggedDrawable: taggedDrawable{structType: model.StructureTypeFigure}}, nil
}

func (rc rgbColor) ToRGB() (float64, float64, float64) {
	return rc.r, rc.g, rc.b
}

// Table allows organizing content in an rows X columns matrix, which can spawn across multiple pages.
type Table struct {
	taggedDrawable
	rows             int
	cols             int
	curCell          int
	colWidths        []float64
	rowHeights       []float64
	defaultRowHeight float64
	cells            []*TableCell
	rowSpanRemaining []int
	positioning      Positioning
	xPos, yPos       float64
	margins          Margins
	hasHeaderRows    bool
	headerStartRow   int
	headerEndRow     int
	enableRowWrap    bool
	enablePageWrap   bool
	compactMode      bool
}

var (
	lengthUnits  = []string{"cm", "mm", "px", "pt"}
	unitToPoints = map[string]float64{"cm": cmToPt, "mm": mmToPt, "px": pxToPt, "pt": 1}
)

// ScaleToWidth scales the ellipse to the specified width. The height of
// the ellipse is scaled so that the aspect ratio is maintained.
func (e *Ellipse) ScaleToWidth(w float64) {
	height := e.height / e.width
	e.width = w
	e.height = w * height
}

// SetWidth sets the the Paragraph width. This is essentially the wrapping width,
// i.e. the width the text can extend to prior to wrapping over to next line.
func (sp *StyledParagraph) SetWidth(width float64) { sp.width = width; sp.wrapText() }

// NoteHeadingStyle returns the style properties used to render the heading of
// the invoice note sections.
func (i *Invoice) NoteHeadingStyle() TextStyle { return i.noteHeadingStyle }

func newChapter(parent *Chapter, toc *TOC, outline *model.Outline, recordFunc chapterRecordFunc, title string, number int, style TextStyle) *Chapter {
	var level uint = 1
	if parent != nil {
		level = parent.level + 1
	}
	chapter := &Chapter{number: number, title: title, showNumbering: true, includeInTOC: true, parent: parent, toc: toc, outline: outline, contents: []Drawable{}, level: level, recordFunc: recordFunc}
	styledParagraph := newStyledParagraph(style)
	text := styledParagraph.SetText(chapter.headingText())
	text.Style = style
	chapter.heading = styledParagraph
	return chapter
}

// Reset removes all the text chunks the paragraph contains.
func (sp *StyledParagraph) Reset() { sp.chunks = []*TextChunk{} }

// ScaleToWidth sets the graphic svg scaling factor with the given width.
func (gs *GraphicSVG) ScaleToWidth(w float64) {
	height := gs.root.Height / gs.root.Width
	gs.root.Width = w
	gs.root.Height = w * height
	gs.root.SetScaling(height, height)
}

func cropImageGeneric(img *model.Image, rect, rect2 image.Rectangle) (*model.Image, error) {
	goImage, err := img.ToGoImage()
	if err != nil {
		return nil, err
	}
	rgba := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	imagedraw.Draw(rgba, image.Rect(0, 0, rect2.Dx(), rect2.Dy()), goImage, rect2.Min, imagedraw.Src)
	image, err := model.ImageHandling.NewImageFromGoImage(rgba)
	if err != nil {
		return nil, err
	}
	if alpha := cropAlphaData(img, rect, rect2); alpha != nil {
		image.SetAlpha(alpha)
	}
	return image, nil
}

var svgCommands commands

// AddTag adds the table tag for Grid to the root K object.
func (g *Grid) AddTag(rootKObj *model.KDict) {
	if rootKObj == nil {
		common.Log.Debug("Table: AddTag called with nil rootKObj, skipping tagging.")
		return
	}
	if g.tagInfo == nil {
		g.tagInfo = model.NewStructureTagInfo()
		g.tagInfo.StructureType = model.StructureTypeTable
	}
	g.tagInfo.ApplyTag = true
	g.tagInfo.ParentKObj = rootKObj
	g.tagInfo.ComponentKObj = g.tagInfo.NewComponentKObj(g.tagInfo.StructureType)
	g.tagInfo.ParentKObj.AddKChild(g.tagInfo.ComponentKObj)
}

// GetTable returns the table the cell belongs to.
func (tc *TableCell) GetTable() *Table { return tc.table }

// Width returns the Block's width.
func (b *Block) Width() float64 { return b.width }

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

// PolyBezierCurve represents a composite curve that is the result of joining
// multiple cubic Bezier curves.
// Implements the Drawable interface and can be drawn on PDF using the Creator.
type PolyBezierCurve struct {
	taggedDrawable
	curve         *contentdraw.PolyBezierCurve
	fillOpacity   float64
	borderOpacity float64
	fillColor     Color
}

func transformRectangle(rect *model.PdfRectangle, matrix transform.Matrix) *model.PdfRectangle {
	var transformRectangle model.PdfRectangle
	transformRectangle.Llx, transformRectangle.Lly = matrix.Transform(rect.Llx, rect.Lly)
	transformRectangle.Urx, transformRectangle.Ury = matrix.Transform(rect.Urx, rect.Ury)
	transformRectangle.Normalize()
	return &transformRectangle
}

// MarkAsArtifact marks the paragraph as an artifact of the specified type.
//
// This would override regular structure tagging and accessibility
// properties, and the paragraph will be treated as an artifact in the PDF.
func (sp *StyledParagraph) MarkAsArtifact(artifactType model.ArtifactType) *model.Artifact {
	sp.artifact = &model.Artifact{Type: artifactType}
	return sp.artifact
}

func (t *Table) insertRows(rowHeightsLen, row int) {
	if row <= 0 {
		return
	}
	for _, cell := range t.cells {
		switch {
		case cell.row > rowHeightsLen:
			cell.row += row
		case cell.row+cell.rowSpan-1 > rowHeightsLen:
			cell.rowSpan += row
		}
	}
	if t.hasHeaderRows {
		if rowHeightsLen < t.headerStartRow {
			t.headerStartRow += row
		}
		if rowHeightsLen < t.headerEndRow {
			t.headerEndRow += row
		}
	}
	if rowHeightsLen > len(t.rowHeights) {
		rowHeightsLen = len(t.rowHeights)
	}
	floats := make([]float64, 0, len(t.rowHeights)+row)
	floats = append(floats, t.rowHeights[:rowHeightsLen]...)
	floats = append(floats, make([]float64, row)...)
	floats = append(floats, t.rowHeights[rowHeightsLen:]...)
	t.rowHeights = floats
	t.rows += row
}

// SetColumns overwrites any columns in the line items table. This should be
// called before AddLine.
func (i *Invoice) SetColumns(cols []*InvoiceCell) { i.columns = cols }

// MoveX moves the drawing context to absolute position x.
func (c *Creator) MoveX(x float64) { c.context.X = x }

func reverseFloats(reverseFloats []float64) []float64 {
	for i, j := 0, len(reverseFloats)-1; i < j; i, j = i+1, j-1 {
		reverseFloats[i], reverseFloats[j] = reverseFloats[j], reverseFloats[i]
	}
	return reverseFloats
}

// SetFillColor sets the fill color.
func (p *Polygon) SetFillColor(color Color) {
	p.fillColor = color
	p.polygon.FillColor = toPdfColor(color)
}

func newURILinkAnnotation(s, s2 string) *model.PdfAnnotation {
	pdfAnnotationLink := model.NewPdfAnnotationLink()
	borderStyle := model.NewBorderStyle()
	borderStyle.SetBorderWidth(0)
	pdfAnnotationLink.BS = borderStyle.ToPdfObject()
	pdfActionURI := model.NewPdfActionURI()
	pdfActionURI.URI = core.MakeString(s)
	pdfAnnotationLink.SetAction(pdfActionURI.PdfAction)
	if s2 != "" {
		pdfAnnotationLink.Contents = core.MakeString(s2)
	}
	return pdfAnnotationLink.PdfAnnotation
}

func (t *Table) moveToNextAvailableCell() int {
	moveToNextAvailableCell := (t.curCell-1)%t.cols + 1
	for {
		if moveToNextAvailableCell-1 >= len(t.rowSpanRemaining) {
			if t.rowSpanRemaining[0] == 0 {
				return moveToNextAvailableCell
			}
			moveToNextAvailableCell = 1
		} else if t.rowSpanRemaining[moveToNextAvailableCell-1] == 0 {
			return moveToNextAvailableCell
		}
		t.curCell++
		t.rowSpanRemaining[moveToNextAvailableCell-1]--
		moveToNextAvailableCell++
	}
}

// Ellipse defines an ellipse with a center at (xc,yc) and a specified width and height.  The ellipse can have a colored
// fill and/or border with a specified width.
// Implements the Drawable interface and can be drawn on PDF using the Creator.
type Ellipse struct {
	taggedDrawable
	xc            float64
	yc            float64
	width         float64
	height        float64
	positioning   Positioning
	fillColor     Color
	fillOpacity   float64
	borderColor   Color
	borderWidth   float64
	borderOpacity float64
	margins       Margins
	fitMode       FitMode
}
