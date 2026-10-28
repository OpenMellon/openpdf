// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/graphic2d"
	"github.com/OpenMellon/openpdf/model"
)

// ToPdfShadingPattern generates a new model.PdfShadingPatternType3 object.
func (rs *RadialShading) ToPdfShadingPattern() *model.PdfShadingPatternType3 {
	r, g, b := rs.base.backgroundColor.ToRGB()
	shadingModel := rs.shadingModel()
	shadingModel.Background = core.MakeArrayFromFloats([]float64{r, g, b})
	pdfShadingPatternType3 := model.NewPdfShadingPatternType3()
	pdfShadingPatternType3.Shading = shadingModel
	return pdfShadingPatternType3
}

func (tp *templateProcessor) collectClasses(node *templateNode) error {
	if tp.options.Strict {
		for _, attr := range node.start.Attr {
			if attr.Name.Space == "" {
				return tp.nodeError(node, "<style> does not take attributes")
			}
		}
	}
	if err := tp.checkStyleText(node); err != nil {
		return err
	}
	for _, node2 := range node.children {
		if node2.start.Name.Local != classElementName {
			if tp.options.Strict {
				return tp.nodeError(node2, "only <class> is allowed inside <style>, found <%s>", node2.start.Name.Local)
			}
			tp.nodeLogDebug(node2, "Only <class> is allowed inside <style>. Skipping <%s>.", node2.start.Name.Local)
			continue
		}
		if tp.options.Strict && len(node2.children) > 0 {
			return tp.nodeError(node2, "<class> does not take child elements")
		}
		if err := tp.checkStyleText(node2); err != nil {
			return err
		}
		var (
			value string
			attrs []xml.Attr
		)
		for _, attr := range node2.start.Attr {
			if attr.Name.Space != "" {
				continue
			}
			if attr.Name.Local == "name" {
				value = attr.Value
				continue
			}
			attrs = append(attrs, attr)
		}
		if value == "" {
			if tp.options.Strict {
				return tp.nodeError(node2, "<class> requires a name")
			}
			tp.nodeLogDebug(node2, "Template <class> requires a name. Skipping.")
			continue
		}
		tp.classStyles[value] = attrs
	}
	return nil
}

// SetAntiAlias enables anti alias config.
//
// Anti alias is disabled by default.
func (rs *RadialShading) SetAntiAlias(enable bool) { rs.base.SetAntiAlias(enable) }

// Color returns the color of the line.
func (l *Line) Color() Color { return l.color }

// GraphicSVGElement represents SVG instances.
type GraphicSVGElement struct {
	// ViewBox represents viewBox value of element.
	ViewBox struct{ X, Y, W, H float64 }

	// Name of element.
	Name string

	// Attributes of element.
	Attributes map[string]string

	// Children elements.
	Children []*GraphicSVGElement

	// Content contains text value of element.
	Content string

	// Style element style attribute.
	Style *GraphicSVGStyle

	// Width of element.
	Width float64

	// Height of element.
	Height         float64
	scale          float64
	linearShadings map[string]*LinearShading
	radialShadings map[string]*RadialShading
	xPos           float64
	yPos           float64
}

// GetLineHeight returns the default line height multiplier of the paragraph.
func (sp *StyledParagraph) GetLineHeight() float64 { return sp.lineHeight }

// GetBackgroundColor returns the cell's background color.
func (tc *TableCell) GetBackgroundColor() Color { return tc.backgroundColor }

func (tp *templateProcessor) parseRadialGradientAttr(creator *Creator, s2 string) (Color, error) {
	radialGradientAttr := ColorBlack
	if s2 == "" {
		return radialGradientAttr, nil
	}
	var (
		err         error
		x           = 0.0
		y           = 0.0
		outerRadius = -1.0
		strs        = strings.Split(s2[16:len(s2)-1], ",")
	)
	fn := func(strs []string) bool {
		return len(strs) > 0 && (strs[0][0] == '#' || tp.parseColor(strs[0]) != nil)
	}
	fields := strings.Fields(strs[0])
	if len(fields) == 2 && !fn(fields) {
		if finiteFloat, err2 := parseFiniteFloat(fields[0]); err2 != nil {
			common.Log.Debug("Failed parsing radial gradient X position: %v", err2)
			setFirstError(&err, fmt.Errorf("invalid radial gradient X position %q", fields[0]))
		} else {
			x = finiteFloat
		}
		if finiteFloat, err2 := parseFiniteFloat(fields[1]); err2 != nil {
			common.Log.Debug("Failed parsing radial gradient Y position: %v", err2)
			setFirstError(&err, fmt.Errorf("invalid radial gradient Y position %q", fields[1]))
		} else {
			y = finiteFloat
		}
		strs = strs[1:]
	}
	if len(strs) == 0 {
		setFirstError(&err, errors.New("gradient has no color stops"))
		return radialGradientAttr, err
	}
	s := strings.TrimSpace(strs[0])
	if len(s) > 0 && !fn(strings.Fields(s)) {
		if finiteFloat, err2 := parseFiniteFloat(s); err2 != nil {
			common.Log.Debug("Failed parsing radial gradient size: %v", err2)
			setFirstError(&err, fmt.Errorf("radial gradient component %q is neither a size nor a color", s))
		} else {
			outerRadius = finiteFloat
		}
		strs = strs[1:]
	}
	colors, positions, err2 := tp.processGradientColorPair(strs)
	setFirstError(&err, err2)
	if colors == nil || positions == nil {
		return radialGradientAttr, err
	}
	if len(colors) == 1 {
		return colors[0], err
	}
	radialGradientColor := creator.NewRadialGradientColor(x, y, 0, outerRadius, []*ColorPoint{})
	for i := 0; i < len(colors); i++ {
		radialGradientColor.AddColorStop(colors[i], positions[i])
	}
	return radialGradientColor, err
}

func (s *shading) generatePdfFunctions() []model.PdfFunction {
	if len(s.colorPoints) == 0 {
		return nil
	} else if len(s.colorPoints) <= 2 {
		r, g, b := s.colorPoints[0].color.ToRGB()
		r2, g2, b2 := s.colorPoints[len(s.colorPoints)-1].color.ToRGB()
		return []model.PdfFunction{&model.PdfFunctionType2{Domain: []float64{0.0, 1.0}, Range: []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}, N: 1, C0: []float64{r, g, b}, C1: []float64{r2, g2, b2}}}
	} else {
		functions := []model.PdfFunction{}
		floats := []float64{}
		for i := 0; i < len(s.colorPoints)-1; i++ {
			r, g, b := s.colorPoints[i].color.ToRGB()
			r2, g2, b2 := s.colorPoints[i+1].color.ToRGB()
			functionType2 := &model.PdfFunctionType2{Domain: []float64{0.0, 1.0}, Range: []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}, N: 1, C0: []float64{r, g, b}, C1: []float64{r2, g2, b2}}
			functions = append(functions, functionType2)
			if i > 0 {
				floats = append(floats, s.colorPoints[i].point)
			}
		}
		encode := []float64{}
		for range functions {
			encode = append(encode, []float64{0.0, 1.0}...)
		}
		return []model.PdfFunction{&model.PdfFunctionType3{Domain: []float64{0.0, 1.0}, Range: []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}, Functions: functions, Bounds: floats, Encode: encode}}
	}
}

// Columns returns all the columns in the invoice line items table.
func (i *Invoice) Columns() []*InvoiceCell { return i.columns }

// SetTotal sets the total of the invoice.
func (i *Invoice) SetTotal(value string) { i.total[1].Value = value }

func (gss *GraphicSVGStyle) toContentStream(cc *contentstream.ContentCreator, resources *model.PdfPageResources, graphicSVGElement *GraphicSVGElement) {
	if gss == nil {
		return
	}
	if gss.FillColor != "" {
		var r, g, b float64
		if rgba, ok := graphic2d.ColorMap[gss.FillColor]; ok {
			r2, g2, b2, _ := rgba.RGBA()
			r, g, b = float64(r2), float64(g2), float64(b2)
			cc.Add_rg(r, g, b)
		} else if strings.HasPrefix(gss.FillColor, "rgb(") {
			r, g, b = parseRGBColor(gss.FillColor)
			cc.Add_rg(r, g, b)
		} else if strings.HasPrefix(gss.FillColor, "url(") {
			s := strings.TrimPrefix(gss.FillColor, "url('#")
			s = strings.TrimPrefix(s, "url(#")
			s = strings.TrimSuffix(s, "')")
			s = strings.TrimSuffix(s, ")")
			if graphicSVGElement.linearShadings[s] != nil {
				resources.SetPatternByName(*core.MakeName(s), graphicSVGElement.linearShadings[s].ToPdfShadingPattern().ToPdfObject())
				cc.Add_cs(*core.MakeName("Pattern"))
				cc.Add_scn_pattern(*core.MakeName(s))
			} else if graphicSVGElement.radialShadings[s] != nil {
				resources.SetPatternByName(*core.MakeName(s), graphicSVGElement.radialShadings[s].ToPdfShadingPattern().ToPdfObject())
				cc.Add_cs(*core.MakeName("Pattern"))
				cc.Add_scn_pattern(*core.MakeName(s))
			}
		} else {
			r, g, b = ColorRGBFromHex(gss.FillColor).ToRGB()
			cc.Add_rg(r, g, b)
		}
	}
	if gss.FillOpacity < 1.0 {
		n := 0
		keyName := core.PdfObjectName(fmt.Sprintf("GS%d", n))
		for {
			_, ok := resources.GetExtGState(keyName)
			if !ok {
				break
			}
			n++
			keyName = core.PdfObjectName(fmt.Sprintf("GS%d", n))
		}
		dict := core.MakeDict()
		dict.Set("ca", core.MakeFloat(gss.FillOpacity))
		err := resources.AddExtGState(keyName, core.MakeIndirectObject(dict))
		if err != nil {
			common.Log.Debug(err.Error())
			return
		}
		cc.Add_gs(keyName)
	}
	if gss.StrokeColor != "" {
		var r, g, b float64
		if rgba, ok := graphic2d.ColorMap[gss.StrokeColor]; ok {
			r2, g2, b2, _ := rgba.RGBA()
			r, g, b = float64(r2)/255.0, float64(g2)/255.0, float64(b2)/255.0
		} else if strings.HasPrefix(gss.FillColor, "rgb(") {
			r, g, b = parseRGBColor(gss.FillColor)
		} else {
			r, g, b = ColorRGBFromHex(gss.StrokeColor).ToRGB()
		}
		cc.Add_RG(r, g, b)
	}
	if gss.StrokeWidth > 0 {
		cc.Add_w(gss.StrokeWidth)
	}
}

func (b *Block) translate(tx, ty float64) {
	operations := contentstream.NewContentCreator().Translate(tx, -ty).Operations()
	*b.contents = append(*operations, *b.contents...)
	b.contents.WrapIfNeeded()
}

const (
	AnchorBottomLeft AnchorPoint = iota
	AnchorBottomRight
	AnchorTopLeft
	AnchorTopRight
	AnchorCenter
	AnchorLeft
	AnchorRight
	AnchorTop
	AnchorBottom
)

func templateValueToString(i any) (string, error) { return valueToString(i) }

// SetAnchor set gradient position anchor.
// Default to center.
func (rs *RadialShading) SetAnchor(anchor AnchorPoint) { rs.anchor = anchor }

// SetMarkedContentID sets marked content ID.
func (i *Invoice) SetMarkedContentID(id int64) {}

// SetFontSize sets the font size for the paragraph.
func (sp *StyledParagraph) SetFontSize(fontSize float64) {
	sp.defaultStyle.FontSize = fontSize
	for _, chunk := range sp.chunks {
		chunk.Style.FontSize = fontSize
	}
}

func (i *Invoice) generateNoteBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	division := newDivision()
	rows := append([][2]string{i.notes, i.terms}, i.sections...)
	for _, row := range rows {
		if row[1] != "" {
			section := i.drawSection(row[0], row[1])
			for _, d := range section {
				division.Add(d)
			}
			styledParagraph := newStyledParagraph(i.defaultStyle)
			styledParagraph.SetMargins(0, 0, 10, 0)
			division.Add(styledParagraph)
		}
	}
	return division.GeneratePageBlocks(ctx)
}

// GetHorizontalAlignment returns the cell's horizontal alignment of content.
func (tc *TableCell) GetHorizontalAlignment() CellHorizontalAlignment { return tc.horizontalAlignment }
