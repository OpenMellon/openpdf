// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/model"
)

// This method is not supported by Border component and exists solely to satisfy the Drawable interface.
func (bo *border) GenerateKDict() (*model.KDict, error) { return nil, nil }

// ScaleToWidth scales the Block to a specified width, maintaining the same aspect ratio.
func (b *Block) ScaleToWidth(w float64) { sx := w / b.width; b.Scale(sx, sx) }

func newPathCommands() commands {
	m := map[string]int{"m": 2, "z": 0, "l": 2, "h": 1, "v": 1, "c": 6, "s": 4, "q": 4, "t": 2, "a": 7}
	var strs []string
	for key := range m {
		strs = append(strs, key)
	}
	return commands{strs, m, "m", "z"}
}

func splitGraphemeClusters(s string) []graphemeCluster {
	const maxTagSequence = 16
	rs2 := []rune(s)
	if len(rs2) == 0 {
		return nil
	}
	graphemeClusters := make([]graphemeCluster, 0, len(rs2))
	n := 0
	for n < len(rs2) {
		n2 := n
		rs := []rune{rs2[n]}
		n++
		if isRegionalIndicator(rs2[n2]) {
			if n < len(rs2) && isRegionalIndicator(rs2[n]) {
				rs = append(rs, rs2[n])
				n++
			}
			graphemeClusters = append(graphemeClusters, graphemeCluster{runes: rs, text: string(rs)})
			continue
		}
		if isKeycapBase(rs2[n2]) && n < len(rs2) && (isVariationSelector(rs2[n]) || isKeycapEnclosing(rs2[n])) {
			if isVariationSelector(rs2[n]) {
				rs = append(rs, rs2[n])
				n++
			}
			if n < len(rs2) && isKeycapEnclosing(rs2[n]) {
				rs = append(rs, rs2[n])
				n++
			}
			graphemeClusters = append(graphemeClusters, graphemeCluster{runes: rs, text: string(rs)})
			continue
		}
		if isEmoji(rs2[n2]) {
			for n < len(rs2) {
				if isVariationSelector(rs2[n]) {
					rs = append(rs, rs2[n])
					n++
					continue
				}
				if isSkinToneModifier(rs2[n]) {
					rs = append(rs, rs2[n])
					n++
					continue
				}
				if isTagCharacter(rs2[n]) {
					n2 := 0
					for n < len(rs2) && isTagCharacter(rs2[n]) && n2 < maxTagSequence {
						rs = append(rs, rs2[n])
						n2++
						n++
						if rs[len(rs)-1] == 0xE007F {
							break
						}
					}
					continue
				}
				if isZeroWidthJoiner(rs2[n]) {
					rs = append(rs, rs2[n])
					n++
					if n < len(rs2) && isEmoji(rs2[n]) {
						rs = append(rs, rs2[n])
						n++
						if n < len(rs2) && isVariationSelector(rs2[n]) {
							rs = append(rs, rs2[n])
							n++
						}
					}
					continue
				}
				break
			}
		}
		graphemeClusters = append(graphemeClusters, graphemeCluster{runes: rs, text: string(rs)})
	}
	return graphemeClusters
}

func valueToString(i any) (string, error) {
	if i == nil {
		return "", nil
	}
	valueOf := reflect.ValueOf(i)
	for (valueOf.Kind() == reflect.Pointer || valueOf.Kind() == reflect.Interface) && !valueOf.Type().Implements(stringerType) && !valueOf.Type().Implements(errorType) && !valueOf.IsNil() {
		valueOf = valueOf.Elem()
	}
	if !valueOf.IsValid() {
		return "", nil
	}
	if !valueOf.Type().Implements(stringerType) && !valueOf.Type().Implements(errorType) {
		switch valueOf.Kind() {
		case reflect.Chan, reflect.Func:
			return "", fmt.Errorf("can't print %s of type %s", valueOf.Kind(), valueOf.Type())
		}
	}
	return fmt.Sprint(valueOf.Interface()), nil
}

func parseListTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseList(node)
}

func (i *Invoice) drawInformation() *Table {
	table := newTable(2)
	rows := append([][2]*InvoiceCell{i.number, i.date, i.dueDate}, i.infoLines...)
	for _, row := range rows {
		invoiceCell, invoiceCell2 := row[0], row[1]
		if invoiceCell2.Value == "" {
			continue
		}
		cell := table.NewCell()
		cell.SetBackgroundColor(invoiceCell.BackgroundColor)
		i.setCellBorder(cell, invoiceCell)
		styledParagraph := newStyledParagraph(invoiceCell.TextStyle)
		styledParagraph.Append(invoiceCell.Value)
		styledParagraph.SetMargins(0, 0, 2, 1)
		cell.SetContent(styledParagraph)
		cell = table.NewCell()
		cell.SetBackgroundColor(invoiceCell2.BackgroundColor)
		i.setCellBorder(cell, invoiceCell2)
		styledParagraph = newStyledParagraph(invoiceCell2.TextStyle)
		styledParagraph.Append(invoiceCell2.Value)
		styledParagraph.SetMargins(0, 0, 2, 1)
		cell.SetContent(styledParagraph)
	}
	return table
}

// AddInternalLinkWithTag adds a new internal link to the paragraph with proper tagging for accessibility.
// The text parameter represents the text that is displayed.
// The user is taken to the specified page, at the specified x and y coordinates.
// Position 0, 0 is at the top left of the page.
// The zoom of the destination page is controlled with the zoom parameter (0 keeps current zoom).
// The options parameter contains accessibility properties like tooltip, altText, and mcid.
func (sp *StyledParagraph) AddInternalLinkWithTag(text string, page int64, x, y, zoom float64, options LinkTagOptions) (*TextChunk, *model.KDict) {
	internalLinkAnnotation := newInternalLinkAnnotation(page-1, x, y, zoom, options.Tooltip)
	accessibleLinkChunk, kDict, err := sp.createAccessibleLinkChunk(text, internalLinkAnnotation, options)
	if err != nil {
		common.Log.Error("Failed to add internal link with tag: %v", err)
		return nil, nil
	}
	return accessibleLinkChunk, kDict
}

func (com *commands) isCommand(s string) bool {
	for _, name := range com.names {
		if strings.ToLower(s) == name {
			return true
		}
	}
	return false
}

// ParseFromSVGFile creates a GraphicSVG instance from file.
func ParseFromSVGFile(path string) (*GraphicSVGElement, error) {
	source, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	return ParseFromSVGStream(source)
}

// GeneratePageBlocks draws the rectangle on a new block representing the page. Implements the Drawable interface.
func (r *Rectangle) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	var (
		blocks      []*Block
		block       = NewBlock(ctx.PageWidth, ctx.PageHeight)
		ctx2        = ctx
		borderWidth = r.borderWidth / 2
	)
	isRelative := r.positioning.IsRelative()
	if isRelative {
		r.applyFitMode(ctx.Width)
		ctx.X += r.margins.Left + borderWidth
		ctx.Y += r.margins.Top + borderWidth
		ctx.Width -= r.margins.Left + r.margins.Right
		ctx.Height -= r.margins.Top + r.margins.Bottom
		if r.height > ctx.Height {
			blocks = append(blocks, block)
			block = NewBlock(ctx.PageWidth, ctx.PageHeight)
			ctx.Page++
			ctx2 := ctx
			ctx2.Y = ctx.Margins.Top + r.margins.Top + borderWidth
			ctx2.X = ctx.Margins.Left + r.margins.Left + borderWidth
			ctx2.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom - r.margins.Top - r.margins.Bottom
			ctx2.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right - r.margins.Left - r.margins.Right
			ctx = ctx2
		}
	} else {
		ctx.X = r.x
		ctx.Y = r.y
	}
	rect := contentdraw.Rectangle{X: ctx.X, Y: ctx.PageHeight - ctx.Y - r.height, Width: r.width, Height: r.height, BorderRadiusTopLeft: r.borderRadiusTopLeft, BorderRadiusTopRight: r.borderRadiusTopRight, BorderRadiusBottomLeft: r.borderRadiusBottomLeft, BorderRadiusBottomRight: r.borderRadiusBottomRight, Opacity: 1.0}
	if r.fillColor != nil {
		rect.FillEnabled = true
		pdfColor := toPdfColor(r.fillColor)
		err := applyShapeColor(block, pdfColor, r.fillColor, func() Rectangle {
			return Rectangle{x: rect.X, y: rect.Y, width: rect.Width, height: rect.Height}
		})
		if err != nil {
			return nil, ctx, err
		}
		rect.FillColor = pdfColor
	}
	if r.borderColor != nil && r.borderWidth > 0 {
		rect.BorderEnabled = true
		rect.BorderColor = toPdfColor(r.borderColor)
		rect.BorderWidth = r.borderWidth
	}
	gsName, err := block.setOpacity(r.fillOpacity, r.borderOpacity)
	if err != nil {
		return nil, ctx, err
	}
	markedDraw, _, err := rect.MarkedDraw(gsName, r.tagInfo)
	if err != nil {
		return nil, ctx, err
	}
	if err = block.addContentsByString(string(markedDraw)); err != nil {
		return nil, ctx, err
	}
	if isRelative {
		ctx.X = ctx2.X
		ctx.Width = ctx2.Width
		height := r.height + borderWidth
		ctx.Y += height + r.margins.Bottom
		ctx.Height -= height
	} else {
		ctx = ctx2
	}
	blocks = append(blocks, block)
	return blocks, ctx, nil
}

// SetLineColor sets the line color.
func (po *Polyline) SetLineColor(color Color) { po.polyline.LineColor = toPdfColor(color) }

// GetMargins returns the margins of the TOC line: left, right, top, bottom.
func (tl *TOCLine) GetMargins() (float64, float64, float64, float64) {
	margins := &tl.paragraph.margins
	return tl.left, margins.Right, margins.Top, margins.Bottom
}

// This method is not supported by Block component and exists solely to satisfy the Drawable interface.
func (b *Block) SetMarkedContentID(id int64) {}

// GetPos returns the absolute coordinates (x, y) of the grid's upper-left
// corner. The coordinates are only meaningful when the grid uses absolute
// positioning (see GetPositioning).
func (g *Grid) GetPos() (float64, float64) { return g.xPos, g.yPos }

// ColorRGBFrom8bit creates a Color from 8-bit (0-255) r,g,b values.
// Example:
//
//	red := ColorRGBFrom8Bit(255, 0, 0)
func ColorRGBFrom8bit(r, g, b byte) Color {
	return rgbColor{r: float64(r) / 255.0, g: float64(g) / 255.0, b: float64(b) / 255.0}
}

func (tp *templateProcessor) parseList(node *templateNode) (any, error) {
	list := tp.creator.NewList()
	if err := tp.applyAttrs(node, list, listAttrs); err != nil {
		return nil, err
	}
	return list, nil
}

var gridSectionStructureTypes = map[GridRowSection]model.StructureType{GridRowSectionBody: model.StructureTypeTableBody, GridRowSectionHeader: model.StructureTypeTableHead, GridRowSectionFooter: model.StructureTypeTableFooter, GridRowSectionUnknown: model.StructureTypeUnknown}

// Width returns 0. A page break has no width; the method exists so PageBreak
// satisfies the VectorDrawable interface and can be added to a Division.
func (pb *PageBreak) Width() float64 { return 0 }
