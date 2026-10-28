// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/model"
)

func parseCommands(tokens []token) ([]*Command, error) {
	var (
		commands []*Command
		floats   []float64
	)
	for _, token := range slices.Backward(tokens) {
		if token.isCommand {
			paramCount := svgCommands.paramCounts[strings.ToLower(token.text)]
			floatsLen := len(floats)
			if paramCount == 0 && floatsLen == 0 {
				command := &Command{Symbol: token.text}
				commands = append([]*Command{command}, commands...)
			} else if paramCount != 0 && floatsLen%paramCount == 0 {
				floatsLen2 := floatsLen / paramCount
				for i := 0; i < floatsLen2; i++ {
					text := token.text
					if text == "m" && i < floatsLen2-1 {
						text = "l"
					}
					if text == "M" && i < floatsLen2-1 {
						text = "L"
					}
					command := &Command{text, reverseFloats(floats[:paramCount])}
					commands = append([]*Command{command}, commands...)
					floats = floats[paramCount:]
				}
			} else {
				pathParserError2 := pathParserError{"Incorrect number of parameters for " + token.text}
				return nil, pathParserError2
			}
		} else {
			svgLength, err := parseSVGLength(token.text, 64)
			if err != nil {
				return nil, err
			}
			floats = append(floats, svgLength)
		}
	}
	return commands, nil
}

// GeneratePageBlocks draws the polygon on a new block representing the page.
// Implements the Drawable interface.
func (p *Polygon) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	gsName, err := block.setOpacity(p.fillOpacity, p.borderOpacity)
	if err != nil {
		return nil, ctx, err
	}
	polygon := p.polygon
	polygon.FillEnabled = polygon.FillColor != nil
	polygon.BorderEnabled = polygon.BorderColor != nil && polygon.BorderWidth > 0
	points := polygon.Points
	rect := model.PdfRectangle{}
	ok := false
	for i := range points {
		for j := range points[i] {
			point := &points[i][j]
			point.Y = ctx.PageHeight - point.Y
			if !ok {
				rect.Llx = point.X
				rect.Lly = point.Y
				rect.Urx = point.X
				rect.Ury = point.Y
				ok = true
			} else {
				rect.Llx = math.Min(rect.Llx, point.X)
				rect.Lly = math.Min(rect.Lly, point.Y)
				rect.Urx = math.Max(rect.Urx, point.X)
				rect.Ury = math.Max(rect.Ury, point.Y)
			}
		}
	}
	if polygon.FillEnabled {
		err := applyShapeColor(block, p.polygon.FillColor, p.fillColor, func() Rectangle {
			return Rectangle{x: rect.Llx, y: rect.Lly, width: rect.Width(), height: rect.Height()}
		})
		if err != nil {
			return nil, ctx, err
		}
	}
	markedDraw, _, err := polygon.MarkedDraw(gsName, p.tagInfo)
	if err != nil {
		return nil, ctx, err
	}
	if err = block.addContentsByString(string(markedDraw)); err != nil {
		return nil, ctx, err
	}
	return []*Block{block}, ctx, nil
}

// Width returns Image's document width.
func (im *Image) Width() float64 { return im.width }

func (ia *InvoiceAddress) fmtLine(s, s2 string, ok bool) string {
	if ok {
		s2 = ""
	}
	return fmt.Sprintf("%s%s\n", s2, s)
}

// SetBackgroundColor sets the cell's background color.
func (gc *GridCell) SetBackgroundColor(col Color) { gc.backgroundColor = col }

// AddTag adds a tag to the division.
func (d *Division) AddTag(rootKObj *model.KDict) {
	if d.tagInfo == nil {
		d.tagInfo = model.NewStructureTagInfo()
		d.tagInfo.StructureType = model.StructureTypeDivision
	}
	d.tagInfo.ApplyTag = true
	d.tagInfo.ParentKObj = rootKObj
	d.tagInfo.ComponentKObj = d.tagInfo.NewComponentKObj(d.tagInfo.StructureType)
	d.tagInfo.ParentKObj.AddKChild(d.tagInfo.ComponentKObj)
}

// Cols returns the number of columns the grid has.
func (g *Grid) Cols() int { return g.cols }

// SetInline sets the inline mode of the division.
func (d *Division) SetInline(inline bool) { d.inline = inline }

func (d *Division) drawBackground(background []*Block, ctx2, ctx3 DrawContext, ok2 bool, m map[int]float64, m2 map[int]bool) ([]*Block, error) {
	backgroundLen := len(background)
	if backgroundLen == 0 || d.background == nil {
		return background, nil
	}
	n, n2 := -1, -1
	for i, block := range background {
		if !block.isEmpty() {
			if n == -1 {
				n = i
			}
			n2 = i
		}
	}
	ok3 := len(d.components) == 0
	if n == -1 && ok3 {
		n, n2 = 0, len(background)-1
	}
	blocks := make([]*Block, 0, len(background))
	ok4 := false
	for i, block := range background {
		var (
			borderRadiusTopLeft     = d.background.BorderRadiusTopLeft
			borderRadiusTopRight    = d.background.BorderRadiusTopRight
			borderRadiusBottomLeft  = d.background.BorderRadiusBottomLeft
			borderRadiusBottomRight = d.background.BorderRadiusBottomRight
		)
		ctx := ctx2
		ctx.Page += i
		value, ok := m[i]
		ok5 := m2[i] || ok4 || i == n
		if i == 0 {
			if ok2 {
				blocks = append(blocks, block)
				continue
			}
			if ok {
				ctx.Height = value - ctx2.Y
			} else if backgroundLen == 1 {
				ctx.Height = ctx3.Y - ctx2.Y
			}
		} else {
			ctx.X = ctx.Margins.Left + d.margins.Left
			ctx.Y = ctx.Margins.Top
			ctx.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right - d.margins.Left - d.margins.Right
			if ok {
				ctx.Height = value - ctx.Margins.Top
			} else if i == backgroundLen-1 {
				ctx.Height = ctx3.Y - ctx.Margins.Top - d.margins.Top
			} else {
				ctx.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom
			}
			if !ok5 {
				borderRadiusTopLeft = 0
				borderRadiusTopRight = 0
			}
		}
		if (block.isEmpty() && !ok3) || ctx.Height <= 0 {
			if m2[i] {
				ok4 = true
			}
			blocks = append(blocks, block)
			continue
		}
		ok4 = false
		if i != n2 && !ok {
			borderRadiusBottomLeft = 0
			borderRadiusBottomRight = 0
		}
		rectangle := newRectangle(ctx.X, ctx.Y, ctx.Width, ctx.Height)
		rectangle.SetFillColor(d.background.FillColor)
		rectangle.SetBorderColor(d.background.BorderColor)
		rectangle.SetBorderWidth(d.background.BorderSize)
		rectangle.SetBorderRadius(borderRadiusTopLeft, borderRadiusTopRight, borderRadiusBottomLeft, borderRadiusBottomRight)
		pageBlocks, _, err := rectangle.GeneratePageBlocks(ctx)
		if err != nil {
			return nil, err
		}
		if len(pageBlocks) == 0 {
			blocks = append(blocks, block)
			continue
		}
		pageBlock := pageBlocks[0]
		if err = pageBlock.mergeBlocks(block); err != nil {
			return nil, err
		}
		blocks = append(blocks, pageBlock)
	}
	return blocks, nil
}

// Polygon represents a polygon shape.
// Implements the Drawable interface and can be drawn on PDF using the Creator.
type Polygon struct {
	taggedDrawable
	polygon       *contentdraw.Polygon
	fillOpacity   float64
	borderOpacity float64
	fillColor     Color
}

func (pat *ParsedTemplate) execute(data any) ([]byte, error) {
	buffer := bytes.NewBuffer(nil)
	if err := pat.tmpl.Execute(buffer, data); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (tp *templateProcessor) parseTableCell(node *templateNode) (any, error) {
	componentParent := node.componentParent()
	if componentParent == nil {
		tp.nodeLogError(node, "Table cell parent cannot be nil.")
		return nil, errInvalidParent
	}
	table, ok := componentParent.value.(*Table)
	if !ok {
		tp.nodeLogError(node, "Table cell parent (%T) is not a table.", componentParent.value)
		return nil, errInvalidParent
	}
	var n, n2 int64
	for _, s := range []string{"colspan", "rowspan"} {
		effectiveAttrValue, ok := tp.effectiveAttrValue(node, s)
		if !ok {
			continue
		}
		int642, err := parseInt64(effectiveAttrValue)
		if err != nil {
			if err2 := tp.strictErr(node, s, effectiveAttrValue, err); err2 != nil {
				return nil, err2
			}
			int642 = 0
		}
		if s == "colspan" {
			n = int642
		} else {
			n2 = int642
		}
	}
	if n <= 0 {
		n = 1
	}
	if n2 <= 0 {
		n2 = 1
	}
	multiCell := table.MultiCell(int(n2), int(n))
	if err := tp.applyAttrs(node, multiCell, tableCellAttrs); err != nil {
		return nil, err
	}
	return multiCell, nil
}

// SetAntiAlias enables anti alias config.
//
// Anti alias is disabled by default.
func (s *shading) SetAntiAlias(enable bool) { s.antiAlias = enable }

// MoveRight moves the drawing context right by relative displacement dx (negative goes left).
func (c *Creator) MoveRight(dx float64) { c.context.X += dx }

func parseChapterTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseChapter(node)
}

func newRadialShading(x0 float64, y0 float64, r0 float64, r1 float64, colorPoints []*ColorPoint) *RadialShading {
	return &RadialShading{base: &shading{backgroundColor: ColorWhite, antiAlias: false, extends: []bool{false, false}, colorPoints: colorPoints}, x0: x0, y0: y0, r0: r0, r1: r1, anchor: AnchorCenter}
}

// GetPositioning returns the positioning mode (relative or absolute) of the
// paragraph.
func (sp *StyledParagraph) GetPositioning() Positioning { return sp.positioning }

// SetBorderLineStyle sets border style (currently dashed or plain).
func (tc *TableCell) SetBorderLineStyle(style contentdraw.LineStyle) { tc.borderLineStyle = style }

// SetColumnWidths sets the fractional column widths.
// Each width should be in the range 0-1 and is a fraction of the table width.
// The number of width inputs must match number of columns, otherwise an error is returned.
func (g *Grid) SetColumnWidths(widths ...float64) error {
	if len(widths) != g.cols {
		common.Log.Debug("Mismatching number of widths and columns")
		return errors.New("range check error")
	}
	g.colWidths = widths
	return nil
}

// NewGraphicSVGFromFile creates a graphic SVG from a file.
func NewGraphicSVGFromFile(path string) (*GraphicSVG, error) { return newGraphicSVGFromFile(path) }
