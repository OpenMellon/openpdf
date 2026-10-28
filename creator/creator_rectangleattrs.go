// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

var rectangleAttrs = map[string]attrApplyFunc{"x": floatAttr(func(rect *Rectangle, x float64) { rect.x = x }), "y": floatAttr(func(rect *Rectangle, y float64) { rect.y = y }), "width": floatAttr(func(rect *Rectangle, width float64) { rect.SetWidth(width) }), "height": floatAttr(func(rect *Rectangle, height float64) { rect.SetHeight(height) }), "fill-color": colorAttr(func(rect *Rectangle, col Color) { rect.SetFillColor(col) }), "fill-opacity": floatAttr(func(rect *Rectangle, opacity float64) { rect.SetFillOpacity(opacity) }), "border-color": colorAttr(func(rect *Rectangle, col Color) { rect.SetBorderColor(col) }), "border-opacity": floatAttr(func(rect *Rectangle, opacity float64) { rect.SetBorderOpacity(opacity) }), "border-width": floatAttr(func(rect *Rectangle, bw float64) { rect.SetBorderWidth(bw) }), "border-radius": borderRadiusAttr(func(rect *Rectangle, topLeft, topRight, bottomRight, bottomLeft float64) {
	rect.SetBorderRadius(topLeft, topRight, bottomLeft, bottomRight)
}), "border-top-left-radius": floatAttr(func(rect *Rectangle, borderRadiusTopLeft float64) { rect.borderRadiusTopLeft = borderRadiusTopLeft }), "border-top-right-radius": floatAttr(func(rect *Rectangle, borderRadiusTopRight float64) { rect.borderRadiusTopRight = borderRadiusTopRight }), "border-bottom-left-radius": floatAttr(func(rect *Rectangle, borderRadiusBottomLeft float64) {
	rect.borderRadiusBottomLeft = borderRadiusBottomLeft
}), "border-bottom-right-radius": floatAttr(func(rect *Rectangle, borderRadiusBottomRight float64) {
	rect.borderRadiusBottomRight = borderRadiusBottomRight
}), "position": enumAttr(positioningValues, func(rect *Rectangle, position Positioning) { rect.SetPositioning(position) }), "fit-mode": enumAttr(fitModeValues, func(rect *Rectangle, fitMode FitMode) { rect.SetFitMode(fitMode) }), "margin": marginAttr(func(rect *Rectangle, margins Margins) {
	rect.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
})}

// SetMargins sets the Chapter margins: left, right, top, bottom.
// Typically not needed as the creator's page margins are used.
func (ch *Chapter) SetMargins(left, right, top, bottom float64) {
	ch.margins.Left = left
	ch.margins.Right = right
	ch.margins.Top = top
	ch.margins.Bottom = bottom
}

func (tec *TextChunk) clone() *TextChunk {
	clone := *tec
	clone.annotations = copyAnnotations(tec.annotations)
	return &clone
}

func (i *Invoice) generateTotalBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	table := newTable(4)
	table.SetMargins(0, 0, 10, 10)
	rows := [][2]*InvoiceCell{i.subtotal}
	rows = append(rows, i.totalLines...)
	rows = append(rows, i.total)
	for _, row := range rows {
		invoiceCell, invoiceCell2 := row[0], row[1]
		if invoiceCell2.Value == "" {
			continue
		}
		table.SkipCells(2)
		cell := table.NewCell()
		cell.SetBackgroundColor(invoiceCell.BackgroundColor)
		cell.SetHorizontalAlignment(invoiceCell2.Alignment)
		i.setCellBorder(cell, invoiceCell)
		styledParagraph := newStyledParagraph(invoiceCell.TextStyle)
		styledParagraph.SetMargins(0, 0, 2, 1)
		styledParagraph.Append(invoiceCell.Value)
		cell.SetContent(styledParagraph)
		cell = table.NewCell()
		cell.SetBackgroundColor(invoiceCell2.BackgroundColor)
		cell.SetHorizontalAlignment(invoiceCell2.Alignment)
		i.setCellBorder(cell, invoiceCell)
		styledParagraph = newStyledParagraph(invoiceCell2.TextStyle)
		styledParagraph.SetMargins(0, 0, 2, 1)
		styledParagraph.Append(invoiceCell2.Value)
		cell.SetContent(styledParagraph)
	}
	return table.GeneratePageBlocks(ctx)
}

// SetAngle sets the rotation angle in degrees.
func (b *Block) SetAngle(angleDeg float64) { b.angle = angleDeg }

func (tp *templateProcessor) getNodeErrorLocation(node *templateNode, format string, a ...any) string {
	s := fmt.Sprintf(format, a...)
	s2 := fmt.Sprintf("%d", node.offset)
	if node.line != 0 {
		s2 = fmt.Sprintf("%d:%d", node.line, node.col)
	}
	if tp.filename != "" {
		return fmt.Sprintf("%s [%s:%s]", s, tp.filename, s2)
	}
	return fmt.Sprintf("%s [%s]", s, s2)
}

var functionDomain = []float64{0, 1, 0, 1}

func parseTextChunkTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseTextChunk(node, nil)
}

func (tp *templateProcessor) resolveLink(s string) (*model.PdfAnnotation, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if strings.HasPrefix(s, "url('") && strings.HasSuffix(s, "')") && len(s) > 7 {
		return newURILinkAnnotation(s[5:len(s)-2], ""), nil
	}
	if strings.HasPrefix(s, "page(") && strings.HasSuffix(s, ")") && len(s) > 6 {
		var (
			err  error
			n    int64
			val  float64
			val2 float64
			val3 = 1.0
			strs = strings.Split(s[5:len(s)-1], ",")
		)
		n, err = strconv.ParseInt(strings.TrimSpace(strs[0]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("link page parameter: %v", err)
		}
		if len(strs) >= 2 {
			val, err = strconv.ParseFloat(strings.TrimSpace(strs[1]), 64)
			if err != nil {
				return nil, fmt.Errorf("link X parameter: %v", err)
			}
		}
		if len(strs) >= 3 {
			val2, err = strconv.ParseFloat(strings.TrimSpace(strs[2]), 64)
			if err != nil {
				return nil, fmt.Errorf("link Y parameter: %v", err)
			}
		}
		if len(strs) >= 4 {
			val3, err = strconv.ParseFloat(strings.TrimSpace(strs[3]), 64)
			if err != nil {
				return nil, fmt.Errorf("link zoom parameter: %v", err)
			}
		}
		return newInternalLinkAnnotation(n-1, val, val2, val3, ""), nil
	}
	return nil, fmt.Errorf("unrecognized link %q", s)
}

// AnchorPoint defines anchor point where the center position of the radial gradient would be calculated.
type AnchorPoint int

func (tp *templateProcessor) process(ok bool) error {
	tree, err := tp.parseTree()
	if err != nil {
		return err
	}
	if err := tp.collectStyles(tree); err != nil {
		return err
	}
	for _, childValue := range tree.childValues {
		switch childValue2 := childValue.(type) {
		case *templateNode:
			if err := tp.walk(childValue2, nil, ok); err != nil {
				return err
			}
		case *templateText:
			if err := tp.checkTopLevelText(childValue2); err != nil {
				return err
			}
		}
	}
	return nil
}

var trailingDigitsRe = regexp.MustCompile("\\d+")

func newInternalLinkAnnotation(val int64, val2, val3, val4 float64, s string) *model.PdfAnnotation {
	pdfAnnotationLink := model.NewPdfAnnotationLink()
	borderStyle := model.NewBorderStyle()
	borderStyle.SetBorderWidth(0)
	pdfAnnotationLink.BS = borderStyle.ToPdfObject()
	if val < 0 {
		val = 0
	}
	pdfAnnotationLink.Dest = core.MakeArray(core.MakeInteger(val), core.MakeName("XYZ"), core.MakeFloat(val2), core.MakeFloat(val3), core.MakeFloat(val4))
	if s != "" {
		pdfAnnotationLink.Contents = core.MakeString(s)
	}
	return pdfAnnotationLink.PdfAnnotation
}

func parseLineTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseLine(node)
}

// SetBorderWidth sets the border width of the rectangle.
func (r *Rectangle) SetBorderWidth(bw float64) { r.borderWidth = bw }

// Rectangle defines a rectangle with upper left corner at (x,y) and a specified width and height.  The rectangle
// can have a colored fill and/or border with a specified width.
// Implements the Drawable interface and can be drawn on PDF using the Creator.
type Rectangle struct {
	taggedDrawable
	x                       float64
	y                       float64
	width                   float64
	height                  float64
	positioning             Positioning
	fillColor               Color
	fillOpacity             float64
	borderColor             Color
	borderWidth             float64
	borderOpacity           float64
	borderRadiusTopLeft     float64
	borderRadiusTopRight    float64
	borderRadiusBottomLeft  float64
	borderRadiusBottomRight float64
	margins                 Margins
	fitMode                 FitMode
}

func (gse *GraphicSVGElement) drawPolygon(cc *contentstream.ContentCreator, resources *model.PdfPageResources) {
	cc.Add_q()
	gse.Style.toContentStream(cc, resources, gse)
	svgPoints, err := parseSVGPoints(gse.Attributes["points"])
	if err != nil {
		common.Log.Debug("ERROR unable to parse points attribute: %v", err)
		return
	}
	if len(svgPoints)%2 > 0 {
		common.Log.Debug("ERROR invalid points attribute length")
		return
	}
	for i := 0; i < len(svgPoints); {
		if i == 0 {
			cc.Add_m(svgPoints[i]*gse.scale, svgPoints[i+1]*gse.scale)
		} else {
			cc.Add_l(svgPoints[i]*gse.scale, svgPoints[i+1]*gse.scale)
		}
		i += 2
	}
	cc.Add_l(svgPoints[0]*gse.scale, svgPoints[1]*gse.scale)
	gse.Style.fillStroke(cc)
	cc.Add_h()
	cc.Add_Q()
}

func buildPath(commands2 []*Command) *Path {
	path := &Path{}
	var commands []*Command
	for i, command := range commands2 {
		switch strings.ToLower(command.Symbol) {
		case svgCommands.moveSymbol:
			if len(commands) > 0 {
				path.Subpaths = append(path.Subpaths, &Subpath{commands})
			}
			commands = []*Command{command}
		case svgCommands.closeSymbol:
			commands = append(commands, command)
			path.Subpaths = append(path.Subpaths, &Subpath{commands})
			commands = []*Command{}
		default:
			commands = append(commands, command)
			if len(commands2) == i+1 {
				path.Subpaths = append(path.Subpaths, &Subpath{commands})
			}
		}
	}
	return path
}

type imageMaskKind int

func (tp *templateProcessor) checkClasses(node *templateNode) error {
	for _, s := range node.classNames() {
		if _, ok := tp.classStyles[s]; ok {
			continue
		}
		if tp.options.Strict {
			return tp.nodeError(node, "undefined class %q", s)
		}
		tp.nodeLogDebug(node, "Undefined class %q. Skipping.", s)
	}
	return nil
}

func encoderAttr(i any) attrApplyFunc {
	value2 := validateSetter("encoderAttr", i, 0, streamEncoderType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		encoderFactory, ok := encoderFactories[value]
		if !ok {
			if proc.options.Strict {
				return proc.nodeError(node, "invalid value %q for attribute %q", value, attrName)
			}
			return nil
		}
		callSetter(value2, target, reflect.ValueOf(encoderFactory()))
		return nil
	}
}

// AddExternalLinkWithTag adds a new external link to the paragraph with proper tagging for accessibility.
// The text parameter represents the text that is displayed and the url parameter sets the destination.
// The options parameter contains accessibility properties like tooltip, altText, and mcid.
func (sp *StyledParagraph) AddExternalLinkWithTag(text, url string, options LinkTagOptions) (*TextChunk, *model.KDict) {
	uriLinkAnnotation := newURILinkAnnotation(url, options.Tooltip)
	accessibleLinkChunk, kDict, err := sp.createAccessibleLinkChunk(text, uriLinkAnnotation, options)
	if err != nil {
		common.Log.Error("Failed to add external link with tag: %v", err)
		return nil, nil
	}
	return accessibleLinkChunk, kDict
}

// NewInvoice returns an instance of an empty invoice.
func (c *Creator) NewInvoice() *Invoice {
	textStyle := c.NewTextStyle()
	textStyle.Font = c.fontBold
	return newInvoice(c.NewTextStyle(), textStyle)
}
