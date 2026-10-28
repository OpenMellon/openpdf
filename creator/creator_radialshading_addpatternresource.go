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
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	chartrender "github.com/unidoc/unichart/render"
)

// AddPatternResource adds pattern dictionary inside the resources dictionary.
func (rs *RadialShading) AddPatternResource(block *Block) (name core.PdfObjectName, err error) {
	return addShadingPatternResource(block, rs.ToPdfShadingPattern().ToPdfObject())
}

func (rs *RadialShading) shadingModel() *model.PdfShadingType3 {
	r, g, b := rs.base.backgroundColor.ToRGB()
	var point contentdraw.Point
	switch rs.anchor {
	case AnchorBottomLeft:
		point = contentdraw.Point{X: rs.boundingBox.Llx, Y: rs.boundingBox.Lly}
	case AnchorBottomRight:
		point = contentdraw.Point{X: rs.boundingBox.Urx, Y: rs.boundingBox.Ury - rs.boundingBox.Height()}
	case AnchorTopLeft:
		point = contentdraw.Point{X: rs.boundingBox.Llx, Y: rs.boundingBox.Lly + rs.boundingBox.Height()}
	case AnchorTopRight:
		point = contentdraw.Point{X: rs.boundingBox.Urx, Y: rs.boundingBox.Ury}
	case AnchorLeft:
		point = contentdraw.Point{X: rs.boundingBox.Llx, Y: rs.boundingBox.Lly + rs.boundingBox.Height()/2}
	case AnchorTop:
		point = contentdraw.Point{X: rs.boundingBox.Llx + rs.boundingBox.Width()/2, Y: rs.boundingBox.Ury}
	case AnchorRight:
		point = contentdraw.Point{X: rs.boundingBox.Urx, Y: rs.boundingBox.Lly + rs.boundingBox.Height()/2}
	case AnchorBottom:
		point = contentdraw.Point{X: rs.boundingBox.Urx + rs.boundingBox.Width()/2, Y: rs.boundingBox.Lly}
	default:
		point = contentdraw.NewPoint(rs.boundingBox.Llx+rs.boundingBox.Width()/2, rs.boundingBox.Lly+rs.boundingBox.Height()/2)
	}
	r0 := rs.r0
	r1 := rs.r1
	x := point.X + rs.x0
	y := point.Y + rs.y0
	if r0 == -1.0 {
		r0 = 0.0
	}
	if r1 == -1.0 {
		var floats []float64
		x2 := x - rs.boundingBox.Llx
		urx := rs.boundingBox.Urx - x
		y2 := y - rs.boundingBox.Lly
		x := x2*x2 + y2*y2
		floats = append(floats, math.Abs(x))
		y3 := rs.boundingBox.Lly + rs.boundingBox.Height() - y
		x3 := x2*x2 + y3*y3
		floats = append(floats, math.Abs(x3))
		y4 := y - rs.boundingBox.Lly
		x4 := urx*urx + y4*y4
		floats = append(floats, math.Abs(x4))
		ury := rs.boundingBox.Ury - y
		x5 := urx*urx + ury*ury
		floats = append(floats, math.Abs(x5))
		sort.Slice(floats, func(i, j int) bool { return floats[i] > floats[j] })
		r1 = math.Sqrt(floats[0])
	}
	pdfShadingType3 := model.NewPdfShadingType3()
	pdfShadingType3.ShadingType = core.MakeInteger(3)
	pdfShadingType3.ColorSpace = model.NewPdfColorspaceDeviceRGB()
	pdfShadingType3.Background = core.MakeArrayFromFloats([]float64{r, g, b})
	pdfShadingType3.AntiAlias = core.MakeBool(rs.base.antiAlias)
	pdfShadingType3.Coords = core.MakeArrayFromFloats([]float64{x, y, r0, x, y, r1})
	pdfShadingType3.Domain = core.MakeArrayFromFloats([]float64{0.0, 1.0})
	pdfShadingType3.Extend = core.MakeArray(core.MakeBool(rs.base.extends[0]), core.MakeBool(rs.base.extends[1]))
	pdfShadingType3.Function = rs.base.generatePdfFunctions()
	return pdfShadingType3
}

func readPagesFromFile(r *os.File) ([]*model.PdfPage, error) {
	pdfReader, err := model.NewPdfReader(r)
	if err != nil {
		return nil, err
	}
	numPages, err := pdfReader.GetNumPages()
	if err != nil {
		return nil, err
	}
	var pages []*model.PdfPage
	for i := 0; i < numPages; i++ {
		page, err := pdfReader.GetPage(i + 1)
		if err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	return pages, nil
}

const (
	FitModeNone FitMode = iota
	FitModeFillWidth
)

func newImageFromBytes(b []byte) (*Image, error) {
	reader := bytes.NewReader(b)
	img, err := model.ImageHandling.Read(reader)
	if err != nil {
		common.Log.Error("Error loading image: %s", err)
		return nil, err
	}
	return newImage(img)
}

// SetMargins sets the margins of the chart component.
func (cha *Chart) SetMargins(left, right, top, bottom float64) {
	cha.margins.Left = left
	cha.margins.Right = right
	cha.margins.Top = top
	cha.margins.Bottom = bottom
}

// SetLineStyle sets the style for all the line components: number, title,
// separator, page. The style is applied only for new lines added to the
// TOC component.
func (to *TOC) SetLineStyle(style TextStyle) {
	to.SetLineNumberStyle(style)
	to.SetLineTitleStyle(style)
	to.SetLineSeparatorStyle(style)
	to.SetLinePageStyle(style)
}

func (tl *TOCLine) prepareParagraph(para *StyledParagraph, ctx DrawContext) {
	text := tl.Title.Text
	if tl.Number.Text != "" {
		text = " " + text
	}
	text += " "
	text2 := tl.Page.Text
	if text2 != "" {
		text2 = " " + text2
	}
	annotations := []*model.PdfAnnotation{}
	lineLink := tl.getLineLink()
	if lineLink != nil {
		annotations = append(annotations, lineLink)
	}
	para.chunks = []*TextChunk{{Text: tl.Number.Text, Style: tl.Number.Style, annotations: annotations}, {Text: text, Style: tl.Title.Style, annotations: annotations}, {Text: text2, Style: tl.Page.Style, annotations: annotations}}
	para.wrapText()
	linesLen := len(para.lines)
	if linesLen == 0 {
		return
	}
	value := ctx.Width*1000 - para.getTextLineWidth(para.lines[linesLen-1])
	textLineWidth := para.getTextLineWidth([]*TextChunk{&tl.Separator})
	count := int(value / textLineWidth)
	text3 := strings.Repeat(tl.Separator.Text, count)
	style := tl.Separator.Style
	chunk := para.Insert(2, text3)
	chunk.Style = style
	chunk.SetAnnotation(tl.getLineLink())
	value = value - float64(count)*textLineWidth
	if value > 500 {
		runeMetrics, ok := style.Font.GetRuneMetrics(' ')
		if ok && value > runeMetrics.Wx {
			wx := int(value / runeMetrics.Wx)
			if wx > 0 {
				style2 := style
				style2.FontSize = 1
				chunk = para.Insert(2, strings.Repeat(" ", wx))
				chunk.Style = style2
				chunk.SetAnnotation(tl.getLineLink())
			}
		}
	}
}

// Background contains properties related to the background of a component.
type Background struct {
	FillColor               Color
	BorderColor             Color
	BorderSize              float64
	BorderRadiusTopLeft     float64
	BorderRadiusTopRight    float64
	BorderRadiusBottomLeft  float64
	BorderRadiusBottomRight float64
}

func (im *Image) setMask(mask *imageMask) error {
	if im.mask != nil && im.mask.kind != mask.kind {
		return errors.New("image already has a mask of a different type")
	}
	im.mask = mask
	im.xobj = nil
	return nil
}

// Add adds a new line with the default style to the table of contents.
func (to *TOC) Add(number, title, page string, level uint) *TOCLine {
	line := to.AddLine(newTOCLineFromChunks(TextChunk{Text: number, Style: to.lineNumberStyle}, TextChunk{Text: title, Style: to.lineTitleStyle}, TextChunk{Text: page, Style: to.linePageStyle}, level, to.defaultStyle))
	if line == nil {
		return nil
	}
	lineMargins := &to.lineMargins
	line.SetMargins(lineMargins.Left, lineMargins.Right, lineMargins.Top, lineMargins.Bottom)
	line.SetLevelOffset(to.lineLevelOffset)
	line.Separator.Text = to.lineSeparator
	line.Separator.Style = to.lineSeparatorStyle
	return line
}

func newChart(chart chartrender.ChartRenderable) *Chart {
	return &Chart{chart: chart, positioning: PositionRelative, margins: Margins{Top: 10, Bottom: 10}, taggedDrawable: taggedDrawable{structType: model.StructureTypeFigure}}
}

func validateSetter(s string, i any, n int, types ...reflect.Type) reflect.Value {
	valueOf := reflect.ValueOf(i)
	if !valueOf.IsValid() || valueOf.Kind() != reflect.Func {
		panic(fmt.Sprintf("creator: %s requires a setter function, got %T", s, i))
	}
	typ := valueOf.Type()
	if typ.IsVariadic() || typ.NumIn() != len(types)+1 {
		panic(fmt.Sprintf("creator: %s setter must take a component and %d value(s), got %s", s, len(types), typ))
	}
	if typ.NumOut() != n {
		panic(fmt.Sprintf("creator: %s setter must return %d value(s), got %s", s, n, typ))
	}
	for i, typ2 := range types {
		if in := typ.In(i + 1); in != typ2 {
			panic(fmt.Sprintf("creator: %s setter value %d must be %s, got %s in %s", s, i+1, typ2, in, typ))
		}
	}
	if n == 1 && typ.Out(0) != errorIfaceType {
		panic(fmt.Sprintf("creator: %s setter must return error, got %s", s, typ))
	}
	return valueOf
}

// Width returns the width of the rectangle.
// NOTE: the returned value does not include the border width of the rectangle.
func (r *Rectangle) Width() float64 { return r.width }

func (tp *templateProcessor) parseTextChunk(node *templateNode, textChunk *TextChunk) (any, error) {
	if node.parent == nil {
		tp.nodeLogError(node, "Text chunk parent cannot be nil.")
		return nil, errInvalidParent
	}
	var (
		textStyle = tp.creator.NewTextStyle()
		ok2       bool
		ok3       bool
	)
	effectiveAttrValue, ok := tp.effectiveAttrValue(node, "link")
	if ok {
		componentParent := node.componentParent()
		para, ok := templateNodeValue(componentParent).(*StyledParagraph)
		if !ok {
			if tp.options.Strict {
				return nil, tp.nodeError(node, "link attribute is only valid on a paragraph text chunk")
			}
			tp.nodeLogError(node, "Link attribute only applicable to paragraph's text chunk.")
			ok2 = true
		} else {
			textStyle = para.linkStyle
			ok3 = true
		}
	}
	if textChunk == nil {
		textChunk = NewTextChunk("", textStyle)
	}
	if err := tp.applyAttrs(node, textChunk, textChunkAttrs); err != nil {
		return nil, err
	}
	if ok3 {
		if _, ok := tp.nodeOwnStyle(node)["color"]; !ok {
			textChunk.Style.Color = textStyle.Color
		}
	}
	if ok && !ok2 {
		link, err := tp.resolveLink(effectiveAttrValue)
		if err != nil {
			if err2 := tp.strictErr(node, "link", effectiveAttrValue, err); err2 != nil {
				return nil, err2
			}
		}
		if link != nil {
			textChunk.AddAnnotation(link)
		}
	}
	return textChunk, nil
}

// SetAddressHeadingStyle sets the style properties used to render the
// heading of the invoice address sections.
func (i *Invoice) SetAddressHeadingStyle(style TextStyle) { i.addressHeadingStyle = style }

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

// GetColumnWidths returns the column width fractions of the table. The
// fractions add up to 1. The returned slice references the table's internal
// column widths; modifying its elements modifies the table.
func (t *Table) GetColumnWidths() []float64 { return t.colWidths }

func (tp *templateProcessor) resolveFont(s string) (*model.PdfFont, error) {
	if font, ok := tp.fontCache[s]; ok {
		return font, nil
	}
	fontUncached, err := tp.resolveFontUncached(s)
	if err == nil {
		if tp.fontCache == nil {
			tp.fontCache = map[string]*model.PdfFont{}
		}
		tp.fontCache[s] = fontUncached
	}
	return fontUncached, err
}
