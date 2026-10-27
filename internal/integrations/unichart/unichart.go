// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package unichart

import (
	"bytes"
	"fmt"
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/unichart/render"
	color2 "image/color"
	"io"
	"math"
)

func (r *Renderer) SetFont(font render.Font) {
	pdfFont, ok := font.(*model.PdfFont)
	if !ok {
		common.Log.Debug("ERROR: invalid font type")
		return
	}
	name, ok := r.fontNames[pdfFont]
	if !ok {
		name = uniqueFontName("Font", 1, r.resources.HasFontByName)
		if err := r.resources.SetFontByName(name, pdfFont.ToPdfObject()); err != nil {
			common.Log.Debug("ERROR: could not add font %v to resources", pdfFont)
		}
		r.fontNames[pdfFont] = name
	}
	r.cc.Add_Tf(name, r.fontSize)
	r.font = pdfFont
}
func (r *Renderer) SetFontColor(color color2.Color) { r.fontColor = color }
func NewRenderer(cc *contentstream.ContentCreator, res *model.PdfPageResources) func(int, int) (render.Renderer, error) {
	return func(width, height int) (render.Renderer, error) {
		r := &Renderer{width: width, height: height, dpi: 72, cc: cc, resources: res, fontNames: map[*model.PdfFont]core.PdfObjectName{}}
		r.ResetStyle()
		return r, nil
	}
}
func (r *Renderer) SetStrokeDashArray(dashArray []float64) {
	dashes := make([]int64, len(dashArray))
	for i, d := range dashArray {
		dashes[i] = int64(d)
	}
	r.cc.Add_d(dashes, 0)
}
func (r *Renderer) LineTo(x, y int) { r.cc.Add_l(float64(x), float64(y)) }
func (r *Renderer) Text(text string, x, y int) {
	r.cc.Add_q()
	r.SetFont(r.font)
	red, green, blue, _ := colorRGBAFloat(r.fontColor)
	r.cc.Add_rg(red, green, blue)
	r.cc.Translate(float64(x), float64(y)).Scale(1, -1)
	if rot := r.textRotation; rot != 0 {
		r.cc.RotateDeg(rot)
	}
	r.cc.Add_BT().Add_TL(r.fontSize)
	var (
		encoder = r.font.Encoder()
		lines   = r.wrapText(text)
		last    = len(lines)
	)
	for i, line := range lines {
		r.cc.Add_TJ(core.MakeStringFromBytes(encoder.Encode(line)))
		if i != last-1 {
			r.cc.Add_Tstar()
		}
	}
	r.cc.Add_ET()
	r.cc.Add_Q()
}
func (r *Renderer) ArcTo(cx, cy int, rx, ry, startAngle, deltaAngle float64) {
	startAngle = radToDeg(2.0*math.Pi - startAngle)
	deltaAngle = radToDeg(-deltaAngle)
	segAngle, segCount := deltaAngle, 1
	if math.Abs(deltaAngle) > 90.0 {
		segCount = int(math.Ceil(math.Abs(deltaAngle) / 90.0))
		segAngle = deltaAngle / float64(segCount)
	}
	var (
		halfAngle = degToRad(segAngle / 2)
		k         = math.Abs(4.0 / 3.0 * (1.0 - math.Cos(halfAngle)) / math.Sin(halfAngle))
		centerX   = float64(cx)
		centerY   = float64(cy)
	)
	for i := 0; i < segCount; i++ {
		a0 := degToRad(startAngle + float64(i)*segAngle)
		a1 := degToRad(startAngle + float64(i+1)*segAngle)
		cos0 := math.Cos(a0)
		cos1 := math.Cos(a1)
		sin0 := math.Sin(a0)
		sin1 := math.Sin(a1)
		var pts []float64
		if segAngle > 0 {
			pts = []float64{centerX + rx*cos0, centerY - ry*sin0, centerX + rx*(cos0-k*sin0), centerY - ry*(sin0+k*cos0), centerX + rx*(cos1+k*sin1), centerY - ry*(sin1-k*cos1), centerX + rx*cos1, centerY - ry*sin1}
		} else {
			pts = []float64{centerX + rx*cos0, centerY - ry*sin0, centerX + rx*(cos0+k*sin0), centerY - ry*(sin0-k*cos0), centerX + rx*(cos1-k*sin1), centerY - ry*(sin1+k*cos1), centerX + rx*cos1, centerY - ry*sin1}
		}
		if i == 0 {
			r.cc.Add_l(pts[0], pts[1])
		}
		r.cc.Add_c(pts[2], pts[3], pts[4], pts[5], pts[6], pts[7])
	}
}
func (r *Renderer) ResetStyle() {
	r.SetFillColor(color2.Black)
	r.SetStrokeColor(color2.Transparent)
	r.SetStrokeWidth(0)
	r.SetFont(model.DefaultFont())
	r.SetFontColor(color2.Black)
	r.SetFontSize(12)
	r.SetTextRotation(0)
}
func (r *Renderer) SetStrokeColor(color color2.Color) {
	r.strokeColor = color
	red, green, blue, _ := colorRGBAFloat(color)
	r.cc.Add_RG(red, green, blue)
}
func (r *Renderer) SetFontSize(size float64) { r.fontSize = size }
func (r *Renderer) ClearTextRotation()       { r.textRotation = 0 }
func (r *Renderer) Fill()                    { r.cc.Add_f() }
func radToDeg(rad float64) float64           { return rad * 180 / math.Pi }
func (r *Renderer) SetStrokeWidth(width float64) {
	r.strokeWidth = width
	r.cc.Add_w(width)
}
func (r *Renderer) Stroke()     { r.cc.Add_S() }
func (r *Renderer) FillStroke() { r.cc.Add_B() }
func (r *Renderer) Save(w io.Writer) error {
	if w == nil {
		return nil
	}
	_, err := io.Copy(w, bytes.NewBuffer(r.cc.Bytes()))
	return err
}
func (r *Renderer) SetClassName(name string) {}
func (r *Renderer) GetDPI() float64          { return r.dpi }
func (r *Renderer) SetFillColor(color color2.Color) {
	r.fillColor = color
	red, green, blue, _ := colorRGBAFloat(color)
	r.cc.Add_rg(red, green, blue)
}

type Renderer struct {
	width        int
	height       int
	dpi          float64
	cc           *contentstream.ContentCreator
	resources    *model.PdfPageResources
	fillColor    color2.Color
	strokeColor  color2.Color
	strokeWidth  float64
	font         *model.PdfFont
	fontSize     float64
	fontColor    color2.Color
	textRotation float64
	fontNames    map[*model.PdfFont]core.PdfObjectName
}

func degToRad(deg float64) float64 { return deg * math.Pi / 180.0 }
func (r *Renderer) MeasureText(text string) render.Box {
	height := r.fontSize
	desc, err := r.font.GetFontDescriptor()
	if err != nil {
		common.Log.Debug("WARN: Unable to get font descriptor")
	} else {
		capHeight, err := desc.GetCapHeight()
		if err != nil {
			common.Log.Debug("WARN: Unable to get font cap height: %v", err)
		} else {
			height = capHeight / 1000.0 * r.fontSize
		}
	}
	var (
		maxWidth = 0.0
		lines    = r.wrapText(text)
	)
	for _, line := range lines {
		if w := r.getTextWidth(line); w > maxWidth {
			maxWidth = w
		}
	}
	box := render.NewBox(0, 0, int(maxWidth), int(height))
	if rot := r.textRotation; rot != 0 {
		box = box.Corners().Rotate(rot).Box()
	}
	return box
}
func (r *Renderer) QuadCurveTo(cx, cy, x, y int) {
	r.cc.Add_v(float64(x), float64(y), float64(cx), float64(cy))
}
func (r *Renderer) SetDPI(dpi float64) { r.dpi = dpi }
func (r *Renderer) getTextWidth(text string) float64 {
	var total float64
	for _, ch := range text {
		metrics, ok := r.font.GetRuneMetrics(ch)
		if !ok {
			common.Log.Debug("ERROR: unsupported rune %v in font", ch)
		}
		total += metrics.Wx
	}
	return r.fontSize * total / 1000.0
}
func uniqueFontName(prefix string, start int, exists func(core.PdfObjectName) bool) core.PdfObjectName {
	name := core.PdfObjectName(fmt.Sprintf("%s%d", prefix, start))
	for n := start; exists(name); {
		n++
		name = core.PdfObjectName(fmt.Sprintf("%s%d", prefix, n))
	}
	return name
}
func (r *Renderer) Close()                          { r.cc.Add_h() }
func (r *Renderer) SetTextRotation(radians float64) { r.textRotation = radToDeg(-radians) }
func (r *Renderer) Circle(radius float64, x, y int) {
	rad := radius
	if w := r.strokeWidth; w != 0 {
		rad -= w / 2
	}
	k := rad * 0.551784
	path := draw.CubicBezierPath{Curves: []draw.CubicBezierCurve{draw.NewCubicBezierCurve(-rad, 0, -rad, k, -k, rad, 0, rad), draw.NewCubicBezierCurve(0, rad, k, rad, rad, k, rad, 0), draw.NewCubicBezierCurve(rad, 0, rad, -k, k, -rad, 0, -rad), draw.NewCubicBezierCurve(0, -rad, -k, -rad, -rad, -k, -rad, 0)}}
	if w := r.strokeWidth; w != 0 {
		path = path.Offset(w/2, w/2)
	}
	path = path.Offset(float64(x), float64(y))
	draw.DrawBezierPathWithCreator(path, r.cc)
}
func (r *Renderer) MoveTo(x, y int) { r.cc.Add_m(float64(x), float64(y)) }
func colorRGBA8(c color2.Color) (uint8, uint8, uint8, uint8) {
	red, green, blue, alpha := c.RGBA()
	return uint8(red >> 8), uint8(green >> 8), uint8(blue >> 8), uint8(alpha >> 8)
}
func (r *Renderer) wrapText(text string) []string {
	var (
		lines []string
		line  []rune
	)
	for _, ch := range text {
		if ch == '\n' {
			lines = append(lines, string(line))
			line = []rune{}
			continue
		}
		line = append(line, ch)
	}
	if len(line) > 0 {
		lines = append(lines, string(line))
	}
	return lines
}
func colorRGBAFloat(c color2.Color) (float64, float64, float64, float64) {
	red, green, blue, alpha := colorRGBA8(c)
	return float64(red) / 255, float64(green) / 255, float64(blue) / 255, float64(alpha) / 255
}
