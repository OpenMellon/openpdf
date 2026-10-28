package creator

import (
	"fmt"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/graphic2d"
	"github.com/OpenMellon/openpdf/model"
)

// GraphicSVGStyle represents style attributes for `GraphicSVG`.
type GraphicSVGStyle struct {
	FillColor   string
	StrokeColor string
	StrokeWidth float64
	FillOpacity float64
}

func (gss *GraphicSVGStyle) fillStroke(cc *contentstream.ContentCreator) {
	if gss.FillColor != "" && gss.StrokeColor != "" {
		cc.Add_B()
	} else if gss.FillColor != "" {
		cc.Add_f()
	} else if gss.StrokeColor != "" {
		cc.Add_S()
	}
}

func newGraphicSVGStyle() *GraphicSVGStyle {
	return &GraphicSVGStyle{FillColor: "#000000", StrokeColor: "", StrokeWidth: 0, FillOpacity: 1.0}
}

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
