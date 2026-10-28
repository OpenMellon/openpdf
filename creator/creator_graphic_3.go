package creator

import (
	"encoding/xml"
	"io"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/internal/graphic2d"
	"github.com/OpenMellon/openpdf/model"
)

func newGraphicSVGElement(startElement xml.StartElement) *GraphicSVGElement {
	graphicSVGElement := &GraphicSVGElement{}
	m := make(map[string]string)
	for _, attr := range startElement.Attr {
		m[attr.Name.Local] = attr.Value
	}
	graphicSVGElement.Name = startElement.Name.Local
	graphicSVGElement.Attributes = m
	graphicSVGElement.scale = 1
	if graphicSVGElement.Name == "svg" {
		svgPoints, err := parseSVGPoints(m["viewBox"])
		if err != nil {
			common.Log.Debug("Unable to parse viewBox attribute: %v", err)
			return nil
		}
		if len(svgPoints) >= 4 {
			graphicSVGElement.ViewBox.X = svgPoints[0]
			graphicSVGElement.ViewBox.Y = svgPoints[1]
			graphicSVGElement.ViewBox.W = svgPoints[2]
			graphicSVGElement.ViewBox.H = svgPoints[3]
		}
		graphicSVGElement.Width = graphicSVGElement.ViewBox.W
		graphicSVGElement.Height = graphicSVGElement.ViewBox.H
		if s, ok := m["width"]; ok {
			if before, ok0 := strings.CutSuffix(s, "%"); ok0 {
				float, err := strconv.ParseFloat(before, 64)
				if err != nil {
					common.Log.Debug("Unable to parse width attribute: %v", err)
					return nil
				}
				graphicSVGElement.Width = float * graphicSVGElement.ViewBox.W
			} else {
				svgLength, err := parseSVGLength(s, 64)
				if err != nil {
					common.Log.Debug("Unable to parse width attribute: %v", err)
					return nil
				}
				graphicSVGElement.Width = svgLength
				if len(svgPoints) < 4 {
					graphicSVGElement.ViewBox.W = svgLength
				}
			}
		}
		if s, ok := m["height"]; ok {
			if before, ok0 := strings.CutSuffix(s, "%"); ok0 {
				float, err := strconv.ParseFloat(before, 64)
				if err != nil {
					common.Log.Debug("Unable to parse height attribute: %v", err)
					return nil
				}
				graphicSVGElement.Height = float * graphicSVGElement.ViewBox.H
			} else {
				svgLength, err := parseSVGLength(s, 64)
				if err != nil {
					common.Log.Debug("Unable to parse height attribute: %v", err)
					return nil
				}
				graphicSVGElement.Height = svgLength
				if len(svgPoints) < 4 {
					graphicSVGElement.ViewBox.H = svgLength
				}
			}
		}
		if graphicSVGElement.Width > 0 && graphicSVGElement.Height > 0 {
			graphicSVGElement.scale = graphicSVGElement.Width / graphicSVGElement.ViewBox.W
		}
	}
	return graphicSVGElement
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

func (gse *GraphicSVGElement) drawCircle(creator *contentstream.ContentCreator, resources *model.PdfPageResources) {
	creator.Add_q()
	gse.Style.toContentStream(creator, resources, gse)
	svgLength, err := parseSVGLength(gse.Attributes["cx"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `cx` value: %v", err.Error())
	}
	svgLength2, err := parseSVGLength(gse.Attributes["cy"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `cy` value: %v", err.Error())
	}
	svgLength3, err := parseSVGLength(gse.Attributes["r"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `r` value: %v", err.Error())
	}
	x2 := svgLength3 * gse.scale
	y2 := svgLength3 * gse.scale
	x1 := x2 * bezierCircleKappa
	y := y2 * bezierCircleKappa
	cubicBezierPath := contentdraw.NewCubicBezierPath()
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(-x2, 0, -x2, y, -x1, y2, 0, y2))
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(0, y2, x1, y2, x2, y, x2, 0))
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(x2, 0, x2, -y, x1, -y2, 0, -y2))
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(0, -y2, -x1, -y2, -x2, -y, -x2, 0))
	cubicBezierPath = cubicBezierPath.Offset(svgLength*gse.scale, svgLength2*gse.scale)
	if gse.Style.StrokeWidth > 0 {
		cubicBezierPath = cubicBezierPath.Offset(gse.Style.StrokeWidth/2, gse.Style.StrokeWidth/2)
	}
	contentdraw.DrawBezierPathWithCreator(cubicBezierPath, creator)
	gse.Style.fillStroke(creator)
	creator.Add_h()
	creator.Add_Q()
}

// Decode decodes the child elements of element.
func (gse *GraphicSVGElement) Decode(decoder *xml.Decoder) error {
	for {
		token, err := decoder.Token()
		if token == nil && err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch token2 := token.(type) {
		case xml.StartElement:
			graphicSVGElement := newGraphicSVGElement(token2)
			err := graphicSVGElement.Decode(decoder)
			if err != nil {
				return err
			}
			gse.Children = append(gse.Children, graphicSVGElement)
		case xml.CharData:
			s := strings.TrimSpace(string(token2))
			if s != "" {
				gse.Content = string(token2)
			}
		case xml.EndElement:
			if token2.Name.Local == gse.Name {
				return nil
			}
		}
	}
	return nil
}

func (gse *GraphicSVGElement) drawPolyline(cc *contentstream.ContentCreator, resources *model.PdfPageResources) {
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
	gse.Style.fillStroke(cc)
	cc.Add_h()
	cc.Add_Q()
}

func (gse *GraphicSVGElement) drawRect(cc *contentstream.ContentCreator, resources *model.PdfPageResources) {
	cc.Add_q()
	gse.Style.toContentStream(cc, resources, gse)
	svgLength, err := parseSVGLength(gse.Attributes["x"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `x` value: %v", err.Error())
	}
	svgLength2, err := parseSVGLength(gse.Attributes["y"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `y` value: %v", err.Error())
	}
	svgLength3, err := parseSVGLength(gse.Attributes["width"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing stroke width value: %v", err.Error())
	}
	svgLength4, err := parseSVGLength(gse.Attributes["height"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing stroke height value: %v", err.Error())
	}
	cc.Add_re(svgLength*gse.scale, svgLength2*gse.scale, svgLength3*gse.scale, svgLength4*gse.scale)
	gse.Style.fillStroke(cc)
	cc.Add_Q()
}

func (gse *GraphicSVGElement) parseColorPoints() []*ColorPoint {
	var colorPoints []*ColorPoint
	var err error
	for _, graphicSVGElement := range gse.Children {
		if graphicSVGElement.Name == "stop" {
			colorRGB := ColorBlack
			point := 0.0
			for attributeKey, attribute := range graphicSVGElement.Attributes {
				if attributeKey == "offset" {
					if before, ok := strings.CutSuffix(attribute, "%"); ok {
						float, err := strconv.ParseFloat(before, 64)
						if err != nil {
							continue
						}
						point = float / 100
					} else {
						point, err = strconv.ParseFloat(attribute, 64)
						if err != nil {
							continue
						}
					}
				} else if attributeKey == "stop-color" {
					if attribute[0] == '#' {
						colorRGB = ColorRGBFromHex(attribute)
					} else {
						rgba, ok := graphic2d.ColorMap[attribute]
						if ok {
							colorRGB = ColorRGBFrom8bit(rgba.R, rgba.G, rgba.B)
						}
					}
				}
			}
			colorPoints = append(colorPoints, NewColorPoint(colorRGB, point))
		}
	}
	return colorPoints
}
