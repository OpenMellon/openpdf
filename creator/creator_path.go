package creator

import (
	"math"
	"strings"

	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// FilledCurve represents a closed path of Bezier curves with a border and fill.
type FilledCurve struct {
	taggedDrawable
	curves        []contentdraw.CubicBezierCurve
	FillEnabled   bool
	fillColor     Color
	BorderEnabled bool
	BorderWidth   float64
	borderColor   Color
}

// GeneratePageBlocks draws the filled curve on page blocks.
func (fc *FilledCurve) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	data, _, err := fc.draw(block, "")
	if err != nil {
		return nil, ctx, err
	}
	err = block.addContentsByString(string(data))
	if err != nil {
		return nil, ctx, err
	}
	return []*Block{block}, ctx, nil
}

// SetFillColor sets the fill color for the path.
func (fc *FilledCurve) SetFillColor(color Color) { fc.fillColor = color }

func (fc *FilledCurve) draw(block *Block, s string) ([]byte, *model.PdfRectangle, error) {
	cubicBezierPath := contentdraw.NewCubicBezierPath()
	for _, curve := range fc.curves {
		cubicBezierPath = cubicBezierPath.AppendCurve(curve)
	}
	creator := contentstream.NewContentCreator()
	if fc.tagInfo != nil {
		creator.Add_BDC(*core.MakeName(string(fc.tagInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(fc.tagInfo.Mcid)})
	}
	creator.Add_q()
	if fc.FillEnabled && fc.fillColor != nil {
		pdfColor := toPdfColor(fc.fillColor)
		err := applyShapeColor(block, pdfColor, fc.fillColor, func() Rectangle {
			cubicBezierPath := contentdraw.NewCubicBezierPath()
			for _, curve := range fc.curves {
				cubicBezierPath = cubicBezierPath.AppendCurve(curve)
			}
			boundingBox := cubicBezierPath.GetBoundingBox()
			if fc.BorderEnabled {
				boundingBox.Height += fc.BorderWidth
				boundingBox.Width += fc.BorderWidth
				boundingBox.X -= fc.BorderWidth / 2
				boundingBox.Y -= fc.BorderWidth / 2
			}
			return Rectangle{x: boundingBox.X, y: boundingBox.Y, width: boundingBox.Width, height: boundingBox.Height}
		})
		if err != nil {
			return nil, nil, err
		}
		creator.SetNonStrokingColor(pdfColor)
	}
	if fc.BorderEnabled {
		if fc.borderColor != nil {
			creator.SetStrokingColor(toPdfColor(fc.borderColor))
		}
		creator.Add_w(fc.BorderWidth)
	}
	if len(s) > 1 {
		creator.Add_gs(core.PdfObjectName(s))
	}
	contentdraw.DrawBezierPathWithCreator(cubicBezierPath, creator)
	creator.Add_h()
	if fc.FillEnabled && fc.BorderEnabled {
		creator.Add_B()
	} else if fc.FillEnabled {
		creator.Add_f()
	} else if fc.BorderEnabled {
		creator.Add_S()
	}
	creator.Add_Q()
	if fc.tagInfo != nil {
		creator.Add_EMC()
	}
	boundingBox := cubicBezierPath.GetBoundingBox()
	if fc.BorderEnabled {
		boundingBox.Height += fc.BorderWidth
		boundingBox.Width += fc.BorderWidth
		boundingBox.X -= fc.BorderWidth / 2
		boundingBox.Y -= fc.BorderWidth / 2
	}
	rect := &model.PdfRectangle{}
	rect.Llx = boundingBox.X
	rect.Lly = boundingBox.Y
	rect.Urx = boundingBox.X + boundingBox.Width
	rect.Ury = boundingBox.Y + boundingBox.Height
	return creator.Bytes(), rect, nil
}

// AppendCurve appends a Bezier curve to the filled curve.
func (fc *FilledCurve) AppendCurve(curve contentdraw.CubicBezierCurve) *FilledCurve {
	fc.curves = append(fc.curves, curve)
	return fc
}

func newFilledCurve() *FilledCurve {
	filledCurve := FilledCurve{}
	filledCurve.curves = []contentdraw.CubicBezierCurve{}
	return &filledCurve
}

// SetBorderColor sets the border color for the path.
func (fc *FilledCurve) SetBorderColor(color Color) { fc.borderColor = color }

// Path is a collection of all the subpaths in 'd' attribute.
type Path struct{ Subpaths []*Subpath }

func newPathCommands() commands {
	m := map[string]int{"m": 2, "z": 0, "l": 2, "h": 1, "v": 1, "c": 6, "s": 4, "q": 4, "t": 2, "a": 7}
	var strs []string
	for key := range m {
		strs = append(strs, key)
	}
	return commands{strs, m, "m", "z"}
}

func (pa *Path) compare(path *Path) bool {
	if len(pa.Subpaths) != len(path.Subpaths) {
		return false
	}
	for i, subpath := range pa.Subpaths {
		if !subpath.compare(path.Subpaths[i]) {
			return false
		}
	}
	return true
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

// Polyline represents a slice of points that are connected as straight lines.
// Implements the Drawable interface and can be rendered using the Creator.
type Polyline struct {
	taggedDrawable
	polyline    *contentdraw.Polyline
	lineOpacity float64
}

// SetLineColor sets the line color.
func (po *Polyline) SetLineColor(color Color) { po.polyline.LineColor = toPdfColor(color) }

// GeneratePageBlocks draws the polyline on a new block representing the page.
// Implements the Drawable interface.
func (po *Polyline) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	gsName, err := block.setOpacity(po.lineOpacity, po.lineOpacity)
	if err != nil {
		return nil, ctx, err
	}
	points := po.polyline.Points
	for i := range points {
		point := &points[i]
		point.Y = ctx.PageHeight - point.Y
	}
	markedDraw, _, err := po.polyline.MarkedDraw(gsName, po.tagInfo)
	if err != nil {
		return nil, ctx, err
	}
	if err = block.addContentsByString(string(markedDraw)); err != nil {
		return nil, ctx, err
	}
	return []*Block{block}, ctx, nil
}

// SetLineWidth sets the line width.
func (po *Polyline) SetLineWidth(lineWidth float64) { po.polyline.LineWidth = lineWidth }

func newPolyline(points []contentdraw.Point) *Polyline {
	return &Polyline{polyline: &contentdraw.Polyline{Points: points, LineColor: model.NewPdfColorDeviceRGB(0, 0, 0), LineWidth: 1.0}, lineOpacity: 1.0}
}

// SetLineOpacity sets the line opacity.
func (po *Polyline) SetLineOpacity(opacity float64) { po.lineOpacity = opacity }

// PolyBezierCurve represents a composite curve that is the result of joining
// multiple cubic Bezier curves.
// Implements the Drawable interface and can be drawn on PDF using the Creator.
type PolyBezierCurve struct {
	taggedDrawable
	curve         *contentdraw.PolyBezierCurve
	fillOpacity   float64
	borderOpacity float64
	fillColor     Color
}

// SetBorderOpacity sets the border opacity.
func (pbc *PolyBezierCurve) SetBorderOpacity(opacity float64) { pbc.borderOpacity = opacity }

// GeneratePageBlocks draws the composite Bezier curve on a new block
// representing the page. Implements the Drawable interface.
func (pbc *PolyBezierCurve) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	gsName, err := block.setOpacity(pbc.fillOpacity, pbc.borderOpacity)
	if err != nil {
		return nil, ctx, err
	}
	curve := pbc.curve
	curve.FillEnabled = curve.FillColor != nil
	var (
		pageHeight        = ctx.PageHeight
		curves            = curve.Curves
		cubicBezierCurves = make([]contentdraw.CubicBezierCurve, 0, len(curve.Curves))
	)
	rect := model.PdfRectangle{}
	for i := range curve.Curves {
		curve := curves[i]
		curve.P0.Y = pageHeight - curve.P0.Y
		curve.P1.Y = pageHeight - curve.P1.Y
		curve.P2.Y = pageHeight - curve.P2.Y
		curve.P3.Y = pageHeight - curve.P3.Y
		cubicBezierCurves = append(cubicBezierCurves, curve)
		bounds := curve.GetBounds()
		if i == 0 {
			rect = bounds
		} else {
			rect.Llx = math.Min(rect.Llx, bounds.Llx)
			rect.Lly = math.Min(rect.Lly, bounds.Lly)
			rect.Urx = math.Max(rect.Urx, bounds.Urx)
			rect.Ury = math.Max(rect.Ury, bounds.Ury)
		}
	}
	curve.Curves = cubicBezierCurves
	defer func() { curve.Curves = curves }()
	if curve.FillEnabled {
		err := applyShapeColor(block, pbc.curve.FillColor, pbc.fillColor, func() Rectangle {
			return Rectangle{x: rect.Llx, y: rect.Lly, width: rect.Width(), height: rect.Height()}
		})
		if err != nil {
			return nil, ctx, err
		}
	}
	markedDraw, _, err := curve.MarkedDraw(gsName, pbc.tagInfo)
	if err != nil {
		return nil, ctx, err
	}
	if err = block.addContentsByString(string(markedDraw)); err != nil {
		return nil, ctx, err
	}
	return []*Block{block}, ctx, nil
}

// SetFillOpacity sets the fill opacity.
func (pbc *PolyBezierCurve) SetFillOpacity(opacity float64) { pbc.fillOpacity = opacity }

// SetFillColor sets the fill color.
func (pbc *PolyBezierCurve) SetFillColor(color Color) {
	pbc.fillColor = color
	pbc.curve.FillColor = toPdfColor(color)
}

// SetBorderWidth sets the border width.
func (pbc *PolyBezierCurve) SetBorderWidth(borderWidth float64) {
	pbc.curve.BorderWidth = borderWidth
}

// SetBorderColor sets the border color.
func (pbc *PolyBezierCurve) SetBorderColor(color Color) { pbc.curve.BorderColor = toPdfColor(color) }

func newPolyBezierCurve(curves []contentdraw.CubicBezierCurve) *PolyBezierCurve {
	return &PolyBezierCurve{curve: &contentdraw.PolyBezierCurve{Curves: curves, BorderColor: model.NewPdfColorDeviceRGB(0, 0, 0), BorderWidth: 1.0}, fillOpacity: 1.0, borderOpacity: 1.0}
}

type pathParserError struct{ msg string }

func (ppe pathParserError) Error() string { return ppe.msg }

// Polygon represents a polygon shape.
// Implements the Drawable interface and can be drawn on PDF using the Creator.
type Polygon struct {
	taggedDrawable
	polygon       *contentdraw.Polygon
	fillOpacity   float64
	borderOpacity float64
	fillColor     Color
}

// SetFillOpacity sets the fill opacity.
func (p *Polygon) SetFillOpacity(opacity float64) { p.fillOpacity = opacity }

// SetFillColor sets the fill color.
func (p *Polygon) SetFillColor(color Color) {
	p.fillColor = color
	p.polygon.FillColor = toPdfColor(color)
}

func newPolygon(points [][]contentdraw.Point) *Polygon {
	return &Polygon{polygon: &contentdraw.Polygon{Points: points}, fillOpacity: 1.0, borderOpacity: 1.0}
}

// SetBorderOpacity sets the border opacity.
func (p *Polygon) SetBorderOpacity(opacity float64) { p.borderOpacity = opacity }

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

// SetBorderColor sets the border color.
func (p *Polygon) SetBorderColor(color Color) { p.polygon.BorderColor = toPdfColor(color) }

// SetBorderWidth sets the border width.
func (p *Polygon) SetBorderWidth(borderWidth float64) { p.polygon.BorderWidth = borderWidth }

// Subpath is a collection of Commands, beginning with moveto command and
// usually ending with closepath command.
type Subpath struct{ Commands []*Command }

func (su *Subpath) compare(subpath *Subpath) bool {
	if len(su.Commands) != len(subpath.Commands) {
		return false
	}
	for i, command := range su.Commands {
		if !command.compare(subpath.Commands[i]) {
			return false
		}
	}
	return true
}
