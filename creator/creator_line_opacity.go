// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/model"
)

// Opacity returns the opacity of the line.
func (l *Line) Opacity() float64 { return l.opacity }

// Chapter is used to arrange multiple drawables (paragraphs, images, etc) into a single section.
// The concept is the same as a book or a report chapter.
type Chapter struct {
	taggedDrawable
	number          int
	title           string
	heading         *StyledParagraph
	contents        []Drawable
	subchapterCount int
	showNumbering   bool
	includeInTOC    bool
	positioning     Positioning
	margins         Margins
	background      *Background
	parent          *Chapter
	toc             *TOC
	outline         *model.Outline
	outlineItem     *model.OutlineItem
	level           uint
	recordFunc      chapterRecordFunc
}

func nextResourceName(s string) string {
	allString := trailingDigitsRe.FindAllString(s, -1)
	if len(allString) == 0 {
		s = s + "0"
	} else {
		n, err := strconv.Atoi(allString[len(allString)-1])
		if err != nil {
			common.Log.Debug("Error converting digit component in resources name, fallback to basic method: %v", err)
			s = s + "0"
		} else {
			n++
			lastIndex := strings.LastIndex(s, allString[len(allString)-1])
			if lastIndex == -1 {
				s = fmt.Sprintf("%s%d", s[:len(s)-1], n)
			} else {
				s = s[:lastIndex] + strconv.Itoa(n)
			}
		}
	}
	return s
}

// SetLineOpacity sets the line opacity.
func (po *Polyline) SetLineOpacity(opacity float64) { po.lineOpacity = opacity }

// CurRow returns the currently active cell's row number.
func (t *Table) CurRow() int { curRow := (t.curCell-1)/t.cols + 1; return curRow }

func (t *Table) headerCut(headerCut int) int {
	if t.hasHeaderRows && headerCut == t.headerEndRow && headerCut < t.rows {
		return t.headerStartRow - 1
	}
	return headerCut
}

// GetRows returns the rows of the grid. The returned slice references the
// grid's internal rows; modifying its elements modifies the grid. Use
// len(GetRows()) to obtain the row count.
func (g *Grid) GetRows() []*GridRow { return g.rows }

func (tp *templateProcessor) pageBreakInvalidContext(node *templateNode) string {
	for activeParent := node.activeParent(); activeParent != nil; activeParent = activeParent.activeParent() {
		switch activeParent.start.Name.Local {
		case "division":
			continue
		case "chapter":
			return ""
		default:
			return activeParent.start.Name.Local
		}
	}
	return ""
}

// BorderOpacity returns the border opacity of the rectangle (0-1).
func (r *Rectangle) BorderOpacity() float64 { return r.borderOpacity }

// EnablePageWrap controls whether the division is wrapped across pages.
// If disabled, the division is moved in its entirety on a new page, if it
// does not fit in the available height. By default, page wrapping is enabled.
// If the height of the division is larger than an entire page, wrapping is
// enabled automatically in order to avoid unwanted behavior.
// Currently, page wrapping can only be disabled for vertical divisions.
func (d *Division) EnablePageWrap(enable bool) { d.enableBreak = enable }

func parseMargins(s string) (Margins, error) {
	floatFields, err := parseFloatFields(s)
	var margins Margins
	switch len(floatFields) {
	case 0:
	case 1:
		margins.Top, margins.Bottom, margins.Left, margins.Right = floatFields[0], floatFields[0], floatFields[0], floatFields[0]
	case 2:
		margins.Top, margins.Bottom = floatFields[0], floatFields[0]
		margins.Left, margins.Right = floatFields[1], floatFields[1]
	case 3:
		margins.Top = floatFields[0]
		margins.Left, margins.Right = floatFields[1], floatFields[1]
		margins.Bottom = floatFields[2]
	case 4:
		margins.Top, margins.Right, margins.Bottom, margins.Left = floatFields[0], floatFields[1], floatFields[2], floatFields[3]
	default:
		if err == nil {
			err = fmt.Errorf("margin expects 1-4 values, got %d", len(floatFields))
		}
	}
	return margins, err
}

// SetStyleBottom sets border style for bottom side.
func (bo *border) SetStyleBottom(style CellBorderStyle) { bo.styleBottom = style }

// FitMode returns the fit mode of the rectangle.
func (r *Rectangle) FitMode() FitMode { return r.fitMode }

// SetMargins sets the margins TOC line.
func (tl *TOCLine) SetMargins(left, right, top, bottom float64) {
	tl.left = left
	margins := &tl.paragraph.margins
	margins.Left = tl.left + float64(tl.level-1)*tl.levelOffset
	margins.Right = right
	margins.Top = top
	margins.Bottom = bottom
}

type chapterInfoEntry struct {
	chapter *Chapter
	page    int
}

func (ch *Chapter) headingNumber() string {
	var headingNumber string
	if ch.showNumbering {
		if ch.number != 0 {
			headingNumber = strconv.Itoa(ch.number) + "."
		}
		if ch.parent != nil {
			headingNumber2 := ch.parent.headingNumber()
			if headingNumber2 != "" {
				headingNumber = headingNumber2 + headingNumber
			}
		}
	}
	return headingNumber
}
func (b *Block) isEmpty() bool { return b.contents == nil || len(*b.contents) == 0 }

var listAttrs = map[string]attrApplyFunc{"indent": floatAttr(func(list *List, indent float64) { list.SetIndent(indent) }), "margin": marginAttr(func(list *List, margins Margins) {
	list.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
})}

func (c *Creator) setActivePage(activePage *model.PdfPage) { c.activePage = activePage }

// GetTextOverflow returns the behavior applied to paragraph text which does
// not fit in the available space.
func (sp *StyledParagraph) GetTextOverflow() TextOverflow { return sp.textOverflow }

func (tp *templateProcessor) addNodeText(parent *templateNode, text string, templateText2 *templateText) error {
	value := parent.value
	if value == nil {
		return nil
	}
	switch value2 := value.(type) {
	case *TextChunk:
		if parent.hasText {
			value2.Text += text
		} else {
			value2.Text = text
			parent.hasText = true
		}
	case *StyledParagraph:
		switch parent.start.Name.Local {
		case "paragraph":
			if isBlank(text) {
				return nil
			}
			node := &templateNode{start: xml.StartElement{Name: xml.Name{Local: "text-chunk"}}, parent: parent, attrs: parent.textAttrs, line: templateText2.line, col: templateText2.col, offset: templateText2.offset}
			textChunk, err := tp.parseTextChunk(node, nil)
			if err != nil {
				return err
			}
			chunk := textChunk.(*TextChunk)
			chunk.Text = text
			value2.appendChunk(chunk)
		case "chapter-heading":
			if componentParent := parent.componentParent(); componentParent != nil {
				if chapter, ok := componentParent.value.(*Chapter); ok {
					if parent.hasText {
						chapter.title += text
					} else {
						chapter.title = text
						parent.hasText = true
					}
					value2.SetText(chapter.headingText())
				}
			}
		}
	}
	return nil
}

// NewColorPoint creates a new color and point object for use in the gradient rendering process.
func NewColorPoint(color Color, point float64) *ColorPoint { return newColorPoint(color, point) }

// SetBoundingBox set gradient color bounding box where the gradient would be rendered.
func (rs *RadialShading) SetBoundingBox(x, y, width, height float64) {
	rs.boundingBox = &model.PdfRectangle{Llx: x, Lly: y, Urx: x + width, Ury: y + height}
}

// GetArtifact returns the artifact associated with the paragraph, if any.
func (sp *StyledParagraph) GetArtifact() *model.Artifact { return sp.artifact }

var stdFontNames = map[string]model.StdFontName{"courier": model.CourierName, "courier-bold": model.CourierBoldName, "courier-oblique": model.CourierObliqueName, "courier-bold-oblique": model.CourierBoldObliqueName, "helvetica": model.HelveticaName, "helvetica-bold": model.HelveticaBoldName, "helvetica-oblique": model.HelveticaObliqueName, "helvetica-bold-oblique": model.HelveticaBoldObliqueName, "symbol": model.SymbolName, "zapf-dingbats": model.ZapfDingbatsName, "times": model.TimesRomanName, "times-bold": model.TimesBoldName, "times-italic": model.TimesItalicName, "times-bold-italic": model.TimesBoldItalicName}

// SetFillColor sets the fill color.
func (cp *CurvePolygon) SetFillColor(color Color) {
	cp.fillColor = color
	cp.polygon.FillColor = toPdfColor(color)
}

func (tp *templateProcessor) applyAttrs(node *templateNode, target any, m map[string]attrApplyFunc) error {
	if err := tp.checkClasses(node); err != nil {
		return err
	}
	for _, attr := range tp.effectiveAttrs(node, m) {
		if attr.Name.Space != "" {
			continue
		}
		local := attr.Name.Local
		attrApplyFunc, ok := m[local]
		if !ok {
			_, ok := carrierAttrs[local]
			_, ok2 := containerTags[node.start.Name.Local]
			if ok && ok2 {
				if err := tp.validateCarrierValue(node, local, attr.Value); err != nil {
					return err
				}
				continue
			}
			if tp.options.Strict {
				return tp.nodeError(node, "unknown attribute %q on <%s>", local, node.start.Name.Local)
			}
			tp.nodeLogDebug(node, "Unsupported %s attribute: `%s`. Skipping.", node.start.Name.Local, local)
			continue
		}
		if attrApplyFunc == nil {
			continue
		}
		if err := attrApplyFunc(tp, node, local, target, attr.Value); err != nil {
			return err
		}
	}
	return nil
}

func newGraphicSVGStyle() *GraphicSVGStyle {
	return &GraphicSVGStyle{FillColor: "#000000", StrokeColor: "", StrokeWidth: 0, FillOpacity: 1.0}
}

// TableCell defines a table cell which can contain a Drawable as content.
type TableCell struct {
	taggedDrawable
	backgroundColor     Color
	opacity             float64
	borderLineStyle     contentdraw.LineStyle
	borderStyleLeft     CellBorderStyle
	borderColorLeft     Color
	borderWidthLeft     float64
	borderStyleBottom   CellBorderStyle
	borderColorBottom   Color
	borderWidthBottom   float64
	borderStyleRight    CellBorderStyle
	borderColorRight    Color
	borderWidthRight    float64
	borderStyleTop      CellBorderStyle
	borderColorTop      Color
	borderWidthTop      float64
	row, col            int
	rowSpan             int
	colSpan             int
	wrapDisabled        bool
	content             VectorDrawable
	horizontalAlignment CellHorizontalAlignment
	verticalAlignment   CellVerticalAlignment
	indent              float64
	table               *Table
}
