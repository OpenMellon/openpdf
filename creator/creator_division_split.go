// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	chartrender "github.com/unidoc/unichart/render"
)

func (d *Division) split(ctx2 DrawContext, value float64) (head, tail *Division) {
	var (
		drawableHeight2       float64
		drawables, components []VectorDrawable
	)
	right := ctx2.Width - d.margins.Left - d.margins.Right - d.padding.Left - d.padding.Right
	bottom := ctx2.Height - d.padding.Top - d.padding.Bottom
	bottom2 := value - d.padding.Top - d.padding.Bottom
	for i, component := range d.components {
		drawableHeight3 := drawableHeight(component, right)
		if drawableHeight2+drawableHeight3 <= bottom {
			drawableHeight2 += drawableHeight3
			drawables = append(drawables, component)
			continue
		}
		switch component2 := component.(type) {
		case *StyledParagraph:
			ctx := ctx2
			ctx.Width = right
			ctx.Height = bottom - drawableHeight2 - component2.margins.Vertical() - 0.5*component2.getTextHeight()
			if ctx.Height > 0 {
				head, tail, err := component2.split(ctx)
				if err == nil && head != nil {
					drawables = append(drawables, head)
					drawableHeight2 += drawableHeight(head, ctx.Width)
				}
				if tail != nil {
					components = append(components, tail)
				}
			} else {
				components = append(components, component)
			}
		case *Division:
			ctx := ctx2
			ctx.Width = right
			ctx.Height = bottom - drawableHeight2
			if ctx.Height > 0 {
				head, tail := component2.split(ctx, bottom2-component2.margins.Vertical())
				if head != nil {
					drawables = append(drawables, head)
					drawableHeight2 += drawableHeight(head, ctx.Width)
				}
				if tail != nil {
					components = append(components, tail)
				}
			} else {
				components = append(components, component)
			}
		case *List:
			ctx := ctx2
			ctx.Width = right
			ctx.Height = bottom - drawableHeight2
			if ctx.Height > 0 {
				head, tail := component2.split(ctx, bottom2-component2.margins.Vertical())
				if head != nil {
					drawables = append(drawables, head)
					drawableHeight2 += drawableHeight(head, ctx.Width)
				}
				if tail != nil {
					components = append(components, tail)
				}
			} else {
				components = append(components, component)
			}
		case *Table:
			ctx := ctx2
			ctx.Width = right
			ctx.Height = bottom - drawableHeight2
			if ctx.Height > 0 {
				head, tail := component2.split(ctx, bottom2)
				if head != nil {
					drawables = append(drawables, head)
					drawableHeight2 += drawableHeight(head, ctx.Width)
				}
				if tail != nil {
					components = append(components, tail)
				}
			} else {
				components = append(components, component)
			}
		default:
			components = append(components, component)
		}
		components = append(components, d.components[i+1:]...)
		break
	}
	if len(drawables) > 0 {
		head = newDivision()
		*head = *d
		head.components = drawables
		if d.background != nil {
			head.background = &Background{}
			*head.background = *d.background
		}
	}
	if len(components) > 0 {
		tail = newDivision()
		*tail = *d
		tail.components = components
		if d.background != nil {
			tail.background = &Background{}
			*tail.background = *d.background
		}
	}
	return head, tail
}

// Chart represents a chart drawable.
// It is used to render unichart chart components using a creator instance.
type Chart struct {
	taggedDrawable
	chart       chartrender.ChartRenderable
	positioning Positioning
	xPos        float64
	yPos        float64
	margins     Margins
}

// EnableWordWrap sets the paragraph word wrap flag.
func (sp *StyledParagraph) EnableWordWrap(val bool) { sp.enableWordWrap = val }

func templateXMLEscape(i any) (string, error) {
	valueToString2, err := valueToString(i)
	if err != nil {
		return "", err
	}
	var w bytes.Buffer
	xml.EscapeText(&w, []byte(valueToString2))
	return w.String(), nil
}

func (tp *templateProcessor) parseListItem(node *templateNode) (any, error) {
	componentParent := node.componentParent()
	if componentParent == nil {
		tp.nodeLogError(node, "List item parent cannot be nil.")
		return nil, errInvalidParent
	}
	list, ok := componentParent.value.(*List)
	if !ok {
		tp.nodeLogError(node, "List item parent is not List.")
		return nil, errInvalidParent
	}
	listItem := newListItem()
	listItem.marker = list.marker
	if err := tp.applyAttrs(node, listItem, emptyAttrs); err != nil {
		return nil, err
	}
	return listItem, nil
}

// DrawContext defines the drawing context. The DrawContext is continuously used and updated when
// drawing the page contents in relative mode.  Keeps track of current X, Y position, available
// height as well as other page parameters such as margins and dimensions.
type DrawContext struct {
	// Current page number.
	Page int

	// Current position.  In a relative positioning mode, a drawable will be placed at these coordinates.
	X, Y float64

	// Context dimensions.  Available width and height (on current page).
	Width, Height float64

	// Page Margins.
	Margins Margins

	// Absolute Page size, widths and height.
	PageWidth  float64
	PageHeight float64

	// Controls whether the components are stacked horizontally
	Inline          bool
	unsupportedRune rune
	errs            []error
}

func (t *Table) resetColumnWidths() {
	t.colWidths = []float64{}
	colWidth := float64(1.0) / float64(t.cols)
	for i := 0; i < t.cols; i++ {
		t.colWidths = append(t.colWidths, colWidth)
	}
}

// Color interface represents colors in the PDF creator.
type Color interface {
	ToRGB() (float64, float64, float64)
}

func parseInt64List(s string) ([]int64, error) {
	fields := strings.Fields(s)
	nums := make([]int64, len(fields))
	var err error
	for i, field := range fields {
		i2, err2 := strconv.ParseInt(field, 10, 64)
		if err2 != nil {
			setFirstError(&err, err2)
			i2 = 0
		}
		nums[i] = i2
	}
	return nums, err
}

func colorspaceForComponents(n int) (model.PdfColorspace, error) {
	switch n {
	case 1:
		return model.NewPdfColorspaceDeviceGray(), nil
	case 3:
		return model.NewPdfColorspaceDeviceRGB(), nil
	case 4:
		return model.NewPdfColorspaceDeviceCMYK(), nil
	}
	return nil, fmt.Errorf("program returns %d values, expected 1, 3 or 4 color components", n)
}

func (t *Table) clampCut(row int) int {
	for ok := true; ok && row > 0; {
		ok = false
		for _, cell := range t.cells {
			if cell.row <= row && row < cell.row+cell.rowSpan-1 {
				row = cell.row - 1
				ok = true
			}
		}
		if t.hasHeaderRows && row >= t.headerStartRow && row < t.headerEndRow {
			row = t.headerStartRow - 1
			ok = true
		}
	}
	return row
}

// GeneratePageBlocks generates a page break block.
func (pb *PageBreak) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	blocks := []*Block{NewBlock(ctx.PageWidth, ctx.PageHeight-ctx.Y), NewBlock(ctx.PageWidth, ctx.PageHeight)}
	ctx.Page++
	ctx2 := ctx
	ctx2.Y = ctx.Margins.Top
	ctx2.X = ctx.Margins.Left
	ctx2.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom
	ctx2.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right
	ctx = ctx2
	return blocks, ctx, nil
}

// NewImage create a new image from an OpenPDF image (model.Image).
func (c *Creator) NewImage(img *model.Image) (*Image, error) { return newImage(img) }

// NewTOC creates a new table of contents.
func (c *Creator) NewTOC(title string) *TOC {
	textStyle := c.NewTextStyle()
	textStyle.Font = c.fontBold
	return newTOC(title, c.NewTextStyle(), textStyle)
}

func (g *Grid) resetColumnWidths() {
	g.colWidths = []float64{}
	colWidth := float64(1.0) / float64(g.cols)
	for i := 0; i < g.cols; i++ {
		g.colWidths = append(g.colWidths, colWidth)
	}
}

func (im *Image) applyFitMode(left float64) {
	left -= im.margins.Left + im.margins.Right
	switch im.fitMode {
	case FitModeFillWidth:
		im.ScaleToWidth(left)
	}
}

// ToPdfShadingPattern generates a new model.PdfShadingPatternType2 object.
func (ls *LinearShading) ToPdfShadingPattern() *model.PdfShadingPatternType2 {
	r, g, b := ls.base.backgroundColor.ToRGB()
	shadingModel := ls.shadingModel()
	shadingModel.Background = core.MakeArrayFromFloats([]float64{r, g, b})
	pdfShadingPatternType2 := model.NewPdfShadingPatternType2()
	pdfShadingPatternType2.Shading = shadingModel
	return pdfShadingPatternType2
}

func (sp *StyledParagraph) getTextHeight() float64 {
	var textHeight float64
	for _, chunk := range sp.chunks {
		fontSize := chunk.Style.FontSize * chunk.Style.effectiveLineHeight(sp.lineHeight)
		if fontSize > textHeight {
			textHeight = fontSize
		}
	}
	return textHeight
}

// GetDefaultStyle returns the default text style of the paragraph. This is the
// style applied to chunks appended without an explicit style.
func (sp *StyledParagraph) GetDefaultStyle() TextStyle { return sp.defaultStyle }

// SetBorderWidth sets the border width.
func (cp *CurvePolygon) SetBorderWidth(borderWidth float64) { cp.polygon.BorderWidth = borderWidth }

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

// SetAnnotation sets an annotation on a TextChunk,
// this will replace any existing annotation that has been set.
// Supplying a nil value as parameter would clear all the annotations.
func (tec *TextChunk) SetAnnotation(annotation *model.PdfAnnotation) {
	if annotation == nil {
		tec.ClearAnnotations()
	} else {
		tec.annotations = []*model.PdfAnnotation{annotation}
	}
}

func isHexColor(s string) bool {
	if (len(s) != 4 && len(s) != 7) || s[0] != '#' {
		return false
	}
	for _, r := range s[1:] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

// GetSideBorderColor returns the cell's border color for the specified side.
// Use CellBorderSideTop/Bottom/Left/Right to read individual sides. Passing
// CellBorderSideAll returns the top side's value only; it is not a valid
// round-trip key — use individual side constants to verify per-side state.
func (gc *GridCell) GetSideBorderColor(side CellBorderSide) Color {
	switch side {
	case CellBorderSideBottom:
		return gc.borderColorBottom
	case CellBorderSideLeft:
		return gc.borderColorLeft
	case CellBorderSideRight:
		return gc.borderColorRight
	default:
		return gc.borderColorTop
	}
}

// SetFitMode sets the fit mode of the image.
// NOTE: The fit mode is only applied if relative positioning is used.
func (im *Image) SetFitMode(fitMode FitMode) { im.fitMode = fitMode }
