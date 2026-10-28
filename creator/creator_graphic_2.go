package creator

import (
	"math"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/graphic2d"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/model"
)

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

func (gse *GraphicSVGElement) toContentStream(creator *contentstream.ContentCreator, resources *model.PdfPageResources) {
	svgStyle, err := parseSVGStyle(gse.Attributes, gse.scale)
	if err != nil {
		common.Log.Debug("Unable to parse style attribute: %v", err)
	}
	gse.Style = svgStyle
	switch gse.Name {
	case "path":
		gse.drawPath(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "rect":
		gse.drawRect(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "circle":
		gse.drawCircle(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "ellipse":
		gse.drawEllipse(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "polyline":
		gse.drawPolyline(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "polygon":
		gse.drawPolygon(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "line":
		gse.drawLine(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "text":
		gse.drawText(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "g":
		attribute, ok := gse.Attributes["fill"]
		attribute2, ok2 := gse.Attributes["stroke"]
		attribute3, ok3 := gse.Attributes["stroke-width"]
		attribute4, ok4 := gse.Attributes["transform"]
		for _, graphicSVGElement := range gse.Children {
			if _, ok2 := graphicSVGElement.Attributes["fill"]; !ok2 && ok {
				graphicSVGElement.Attributes["fill"] = attribute
			}
			if _, ok := graphicSVGElement.Attributes["stroke"]; !ok && ok2 {
				graphicSVGElement.Attributes["stroke"] = attribute2
			}
			if _, ok := graphicSVGElement.Attributes["stroke-width"]; !ok && ok3 {
				graphicSVGElement.Attributes["stroke-width"] = attribute3
			}
			if _, ok := graphicSVGElement.Attributes["transform"]; !ok && ok4 {
				graphicSVGElement.Attributes["transform"] = attribute4
			}
			graphicSVGElement.toContentStream(creator, resources)
		}
	}
}

// SetScaling sets scaling value for graphic SVG and maintain the aspect ratio.
func (gse *GraphicSVGElement) SetScaling(xFactor, yFactor float64) {
	width := gse.Width / gse.ViewBox.W
	height := gse.Height / gse.ViewBox.H
	gse.setDefaultScaling(math.Max(width, height))
	for _, graphicSVGElement := range gse.Children {
		graphicSVGElement.SetScaling(xFactor, yFactor)
	}
}

// SetPos set position of the element on PDF page
func (gse *GraphicSVGElement) SetPos(x, y float64) { gse.xPos = x; gse.yPos = y }

func (gse *GraphicSVGElement) drawLine(cc *contentstream.ContentCreator, resources *model.PdfPageResources) {
	cc.Add_q()
	gse.Style.toContentStream(cc, resources, gse)
	svgLength, err := parseSVGLength(gse.Attributes["x1"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `cx` value: %v", err.Error())
	}
	svgLength2, err := parseSVGLength(gse.Attributes["y1"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `cy` value: %v", err.Error())
	}
	svgLength3, err := parseSVGLength(gse.Attributes["x2"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `rx` value: %v", err.Error())
	}
	svgLength4, err := parseSVGLength(gse.Attributes["y2"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `ry` value: %v", err.Error())
	}
	cc.Add_m(svgLength*gse.scale, svgLength2*gse.scale)
	cc.Add_l(svgLength3*gse.scale, svgLength4*gse.scale)
	gse.Style.fillStroke(cc)
	cc.Add_h()
	cc.Add_Q()
}

func (gse *GraphicSVGElement) drawEllipse(creator *contentstream.ContentCreator, resources *model.PdfPageResources) {
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
	svgLength3, err := parseSVGLength(gse.Attributes["rx"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `rx` value: %v", err.Error())
	}
	svgLength4, err := parseSVGLength(gse.Attributes["ry"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `ry` value: %v", err.Error())
	}
	x2 := svgLength3 * gse.scale
	y2 := svgLength4 * gse.scale
	offX := svgLength * gse.scale
	offY := svgLength2 * gse.scale
	x := x2 * bezierCircleKappa
	y := y2 * bezierCircleKappa
	cubicBezierPath := contentdraw.NewCubicBezierPath()
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(-x2, 0, -x2, y, -x, y2, 0, y2))
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(0, y2, x, y2, x2, y, x2, 0))
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(x2, 0, x2, -y, x, -y2, 0, -y2))
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(0, -y2, -x, -y2, -x2, -y, -x2, 0))
	cubicBezierPath = cubicBezierPath.Offset(offX, offY)
	if gse.Style.StrokeWidth > 0 {
		cubicBezierPath = cubicBezierPath.Offset(gse.Style.StrokeWidth/2, gse.Style.StrokeWidth/2)
	}
	contentdraw.DrawBezierPathWithCreator(cubicBezierPath, creator)
	gse.Style.fillStroke(creator)
	creator.Add_h()
	creator.Add_Q()
}

func (gse *GraphicSVGElement) drawText(cc *contentstream.ContentCreator, resources *model.PdfPageResources) {
	cc.Add_BT()
	svgLength, err := parseSVGLength(gse.Attributes["x"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `x` value: %v", err.Error())
	}
	svgLength2, err := parseSVGLength(gse.Attributes["y"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `y` value: %v", err.Error())
	}
	attribute := gse.Attributes["fill"]
	var r, g, b float64
	if rgba, ok := graphic2d.ColorMap[attribute]; ok {
		r2, g2, b2, _ := rgba.RGBA()
		r, g, b = float64(r2), float64(g2), float64(b2)
	} else if strings.HasPrefix(attribute, "rgb(") {
		r, g, b = parseRGBColor(attribute)
	} else {
		r, g, b = ColorRGBFromHex(attribute).ToRGB()
	}
	cc.Add_rg(r, g, b)
	value := defaultSVGFontSize
	if attribute, ok := gse.Attributes["font-size"]; ok {
		value, err = strconv.ParseFloat(attribute, 64)
		if err != nil {
			common.Log.Debug("Error while parsing `font-size` value: %v", err.Error())
			value = defaultSVGFontSize
		}
	}
	fontSize := gse.scale * value * PPI / svgDPI
	keyName := core.PdfObjectName("SFont")
	defaultFont := model.DefaultFont()
	attribute2, ok := gse.Attributes["font-family"]
	if ok {
		if svgStandardFont2, err := svgStandardFont(attribute2); err == nil {
			defaultFont = svgStandardFont2
			i := 1
			for resources.HasFontByName(keyName) {
				keyName = core.PdfObjectName("SFont" + strconv.Itoa(i))
				i++
			}
		}
	}
	value2 := 0.0
	attribute3, ok := gse.Attributes["text-anchor"]
	if ok && attribute3 != "start" {
		var wx float64
		for _, r := range gse.Content {
			runeMetrics, ok := defaultFont.GetRuneMetrics(r)
			if !ok {
				common.Log.Debug("ERROR: unsupported rune %v in font", r)
			}
			wx += runeMetrics.Wx
		}
		wx = wx * fontSize / 1000.0
		switch attribute3 {
		case "middle":
			value2 = -wx / 2
		case "end":
			value2 = -wx
		}
	}
	cc.Add_Tm(1, 0, 0, -1, svgLength*gse.scale+value2, svgLength2*gse.scale)
	resources.SetFontByName(keyName, defaultFont.ToPdfObject())
	cc.Add_Tf(keyName, fontSize)
	content := gse.Content
	str := core.MakeString(content)
	cc.Add_Tj(*str)
	cc.Add_ET()
}

func (gse *GraphicSVGElement) processDefs() {
	gse.linearShadings = make(map[string]*LinearShading)
	gse.radialShadings = make(map[string]*RadialShading)
	for _, graphicSVGElement := range gse.Children {
		if graphicSVGElement.Name == "defs" {
			for _, graphicSVGElement2 := range graphicSVGElement.Children {
				switch graphicSVGElement2.Name {
				case "linearGradient":
					attribute := graphicSVGElement2.Attributes["id"]
					colorPoints := graphicSVGElement2.parseColorPoints()
					linearShading := newLinearShading(colorPoints)
					gradientAngle := graphicSVGElement2.getGradientAngle()
					linearShading.SetAngle(-gradientAngle)
					linearShading.SetExtends(true, true)
					linearShading.SetBoundingBox(0, 0, gse.Width, gse.Height)
					gse.linearShadings[attribute] = linearShading
				case "radialGradient":
					attribute := graphicSVGElement2.Attributes["id"]
					colorPoints := graphicSVGElement2.parseColorPoints()
					radialShading := newRadialShading(gse.xPos, gse.ViewBox.H, 0, math.Min(gse.Width, gse.Height)/2, colorPoints)
					radialShading.SetExtends(true, true)
					radialShading.SetBoundingBox(0, 0, gse.Width, gse.Height)
					gse.radialShadings[attribute] = radialShading
				}
			}
		}
	}
}

func (gse *GraphicSVGElement) drawPath(cc *contentstream.ContentCreator, resources *model.PdfPageResources) {
	cc.Add_q()
	gse.Style.toContentStream(cc, resources, gse)
	applySVGTransform(gse, cc)
	svgPath, err := parseSVGPath(gse.Attributes["d"])
	if err != nil {
		common.Log.Error("ERROR: %s", err.Error())
	}
	var (
		startX, startY = 0.0, 0.0
		param, y       = 0.0, 0.0
		command2       *Command
	)
	for _, subpath := range svgPath.Subpaths {
		for _, command := range subpath.Commands {
			switch strings.ToLower(command.Symbol) {
			case "m":
				param, y = command.Params[0]*gse.scale, command.Params[1]*gse.scale
				if !command.isAbsolute() {
					param, y = startX+param-gse.ViewBox.X, startY+y-gse.ViewBox.Y
				}
				cc.Add_m(precision.RoundFloat(param, 3), precision.RoundFloat(y, 3))
				startX, startY = param, y
			case "c":
				param, value, param2, param3, param4, param5 := command.Params[0]*gse.scale, command.Params[1]*gse.scale, command.Params[2]*gse.scale, command.Params[3]*gse.scale, command.Params[4]*gse.scale, command.Params[5]*gse.scale
				if !command.isAbsolute() {
					param, value, param2, param3, param4, param5 = startX+param, startY+value, startX+param2, startY+param3, startX+param4, startY+param5
				}
				cc.Add_c(precision.RoundFloat(param, 3), precision.RoundFloat(value, 3), precision.RoundFloat(param2, 3), precision.RoundFloat(param3, 3), precision.RoundFloat(param4, 3), precision.RoundFloat(param5, 3))
				startX, startY = param4, param5
			case "s":
				param, value, param2, param3 := command.Params[0]*gse.scale, command.Params[1]*gse.scale, command.Params[2]*gse.scale, command.Params[3]*gse.scale
				if !command.isAbsolute() {
					param, value, param2, param3 = startX+param, startY+value, startX+param2, startY+param3
				}
				cc.Add_c(precision.RoundFloat(startX, 3), precision.RoundFloat(startY, 3), precision.RoundFloat(param, 3), precision.RoundFloat(value, 3), precision.RoundFloat(param2, 3), precision.RoundFloat(param3, 3))
				startX, startY = param2, param3
			case "l":
				param, value := command.Params[0]*gse.scale, command.Params[1]*gse.scale
				if !command.isAbsolute() {
					param, value = startX+param, startY+value
				}
				cc.Add_l(precision.RoundFloat(param, 3), precision.RoundFloat(value, 3))
				startX, startY = param, value
			case "h":
				param := command.Params[0] * gse.scale
				if !command.isAbsolute() {
					param = startX + param
				}
				cc.Add_l(precision.RoundFloat(param, 3), precision.RoundFloat(startY, 3))
				startX = param
			case "v":
				param := command.Params[0] * gse.scale
				if !command.isAbsolute() {
					param = startY + param
				}
				cc.Add_l(precision.RoundFloat(startX, 3), precision.RoundFloat(param, 3))
				startY = param
			case "q":
				param, y1, x, y := command.Params[0]*gse.scale, command.Params[1]*gse.scale, command.Params[2]*gse.scale, command.Params[3]*gse.scale
				if !command.isAbsolute() {
					param, y1, x, y = startX+param, startY+y1, startX+x, startY+y
				}
				quadraticToCubicBezier, point := graphic2d.QuadraticToCubicBezier(startX, startY, param, y1, x, y)
				cc.Add_c(precision.RoundFloat(quadraticToCubicBezier.X, 3), precision.RoundFloat(quadraticToCubicBezier.Y, 3), precision.RoundFloat(point.X, 3), precision.RoundFloat(point.Y, 3), precision.RoundFloat(x, 3), precision.RoundFloat(y, 3))
				startX, startY = x, y
			case "t":
				var point3, point4 graphic2d.Point
				param, y := command.Params[0]*gse.scale, command.Params[1]*gse.scale
				if !command.isAbsolute() {
					param, y = startX+param, startY+y
				}
				if command2 != nil && strings.ToLower(command2.Symbol) == "q" {
					point := graphic2d.Point{X: command2.Params[0] * gse.scale, Y: command2.Params[1] * gse.scale}
					point2 := graphic2d.Point{X: command2.Params[2] * gse.scale, Y: command2.Params[3] * gse.scale}
					sub := point2.Mul(2.0).Sub(point)
					point3, point4 = graphic2d.QuadraticToCubicBezier(startX, startY, sub.X, sub.Y, param, y)
				}
				cc.Add_c(precision.RoundFloat(point3.X, 3), precision.RoundFloat(point3.Y, 3), precision.RoundFloat(point4.X, 3), precision.RoundFloat(point4.Y, 3), precision.RoundFloat(param, 3), precision.RoundFloat(y, 3))
				startX, startY = param, y
			case "a":
				param, ry := command.Params[0]*gse.scale, command.Params[1]*gse.scale
				rot := command.Params[2]
				large := command.Params[3] > 0
				sweep := command.Params[4] > 0
				endX, endY := command.Params[5]*gse.scale, command.Params[6]*gse.scale
				if !command.isAbsolute() {
					endX, endY = startX+endX, startY+endY
				}
				ellipseToCubicBeziers := graphic2d.EllipseToCubicBeziers(startX, startY, param, ry, rot, large, sweep, endX, endY)
				for _, ellipseToCubicBezier := range ellipseToCubicBeziers {
					cc.Add_c(precision.RoundFloat(ellipseToCubicBezier[1].X, 3), precision.RoundFloat(ellipseToCubicBezier[1].Y, 3), precision.RoundFloat(ellipseToCubicBezier[2].X, 3), precision.RoundFloat(ellipseToCubicBezier[2].Y, 3), precision.RoundFloat(ellipseToCubicBezier[3].X, 3), precision.RoundFloat(ellipseToCubicBezier[3].Y, 3))
				}
				startX, startY = endX, endY
			case "z":
				cc.Add_h()
			}
			command2 = command
		}
	}
	gse.Style.fillStroke(cc)
	cc.Add_h()
	cc.Add_Q()
}

func (gse *GraphicSVGElement) setDefaultScaling(scale float64) {
	gse.scale = scale
	if gse.Style != nil && gse.Style.StrokeWidth > 0 {
		gse.Style.StrokeWidth = gse.Style.StrokeWidth * gse.scale
	}
	for _, graphicSVGElement := range gse.Children {
		graphicSVGElement.setDefaultScaling(scale)
	}
}

// ToContentCreator convert SVG and add elements contentstream then returns `contentstream.ContentCreator`.
func (gse *GraphicSVGElement) ToContentCreator(cc *contentstream.ContentCreator, res *model.PdfPageResources, scaleX, scaleY, translateX, translateY float64) *contentstream.ContentCreator {
	if gse.Name == "svg" {
		gse.SetScaling(scaleX, scaleY)
		cc.Add_cm(1, 0, 0, 1, translateX, translateY)
		gse.setDefaultScaling(gse.scale)
		cc.Add_W()
		cc.Add_n()
		gse.processDefs()
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.ViewBox = gse.ViewBox
			graphicSVGElement.linearShadings = gse.linearShadings
			graphicSVGElement.radialShadings = gse.radialShadings
			graphicSVGElement.toContentStream(cc, res)
		}
		return cc
	}
	return nil
}

func (gse *GraphicSVGElement) getGradientAngle() float64 {
	attribute, ok := gse.Attributes["gradientTransform"]
	if ok {
		fields := strings.FieldsSeq(attribute)
		for field := range fields {
			fieldsFunc := strings.FieldsFunc(field, isTransformSeparator)
			if len(fieldsFunc) < 2 {
				continue
			}
			if fieldsFunc[0] == "rotate" {
				colorComponent, err := parseColorComponent(fieldsFunc[1])
				if err != nil {
					common.Log.Debug("can't parse transform attribute %v", attribute)
					return 0.0
				}
				return colorComponent
			}
		}
	}
	return 0.0
}
