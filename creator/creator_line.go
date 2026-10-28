package creator

import (
	"math"
	"regexp"

	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
)

// Line defines a line between point 1 (X1, Y1) and point 2 (X2, Y2).
// The line width, color, style (solid or dashed) and opacity can be
// configured. Implements the Drawable interface.
type Line struct {
	taggedDrawable
	x1          float64
	y1          float64
	x2          float64
	y2          float64
	color       Color
	style       contentdraw.LineStyle
	opacity     float64
	dashArray   []int64
	dashPhase   int64
	lineWidth   float64
	positioning Positioning
	fitMode     FitMode
	margins     Margins
}

// SetColor sets the line color. Use ColorRGBFromHex, ColorRGBFrom8bit or
// ColorRGBFromArithmetic to create the color object.
func (l *Line) SetColor(color Color) { l.color = color }

// Height returns the height of the line.
func (l *Line) Height() float64 {
	lineWidth := l.lineWidth
	if l.x1 == l.x2 {
		lineWidth /= 2
	}
	return math.Abs(l.y2-l.y1) + lineWidth
}

// SetMargins sets the margins of the line.
// NOTE: line margins are only applied if relative positioning is used.
func (l *Line) SetMargins(left, right, top, bottom float64) {
	l.margins.Left = left
	l.margins.Right = right
	l.margins.Top = top
	l.margins.Bottom = bottom
}

var newlineWhitespaceRe = regexp.MustCompile("[ \\t\\r\\n]*\\n[ \\t\\r\\n]*")

// GeneratePageBlocks draws the line on a new block representing the page.
// Implements the Drawable interface.
func (l *Line) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	var (
		blocks         []*Block
		block          = NewBlock(ctx.PageWidth, ctx.PageHeight)
		ctx2           = ctx
		x1, pageHeight = l.x1, ctx.PageHeight - l.y1
		x2, y2         = l.x2, ctx.PageHeight - l.y2
	)
	isRelative := l.positioning.IsRelative()
	if isRelative {
		ctx.X += l.margins.Left
		ctx.Y += l.margins.Top
		ctx.Width -= l.margins.Left + l.margins.Right
		ctx.Height -= l.margins.Top + l.margins.Bottom
		x1, pageHeight, x2, y2 = l.computeCoords(ctx)
		if l.Height() > ctx.Height {
			blocks = append(blocks, block)
			block = NewBlock(ctx.PageWidth, ctx.PageHeight)
			ctx.Page++
			ctx2 := ctx
			ctx2.Y = ctx.Margins.Top + l.margins.Top
			ctx2.X = ctx.Margins.Left + l.margins.Left
			ctx2.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom - l.margins.Top - l.margins.Bottom
			ctx2.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right - l.margins.Left - l.margins.Right
			ctx = ctx2
			x1, pageHeight, x2, y2 = l.computeCoords(ctx)
		}
	}
	basicLine := contentdraw.BasicLine{X1: x1, Y1: pageHeight, X2: x2, Y2: y2, LineColor: toPdfColor(l.color), Opacity: l.opacity, LineWidth: l.lineWidth, LineStyle: l.style, DashArray: l.dashArray, DashPhase: l.dashPhase}
	gsName, err := block.setOpacity(1.0, l.opacity)
	if err != nil {
		return nil, ctx, err
	}
	markedDraw, _, err := basicLine.MarkedDraw(gsName, l.tagInfo)
	if err != nil {
		return nil, ctx, err
	}
	if err = block.addContentsByString(string(markedDraw)); err != nil {
		return nil, ctx, err
	}
	if isRelative {
		ctx.X = ctx2.X
		ctx.Width = ctx2.Width
		height := l.Height()
		ctx.Y += height + l.margins.Bottom
		ctx.Height -= height
	} else {
		ctx = ctx2
	}
	blocks = append(blocks, block)
	return blocks, ctx, nil
}

// SetFitMode sets the fit mode of the line.
// NOTE: The fit mode is only applied if relative positioning is used.
func (l *Line) SetFitMode(fitMode FitMode) { l.fitMode = fitMode }

// SetLineWidth sets the line width.
func (l *Line) SetLineWidth(width float64) { l.lineWidth = width }

// GetCoords returns the (x1, y1), (x2, y2) points defining the Line.
func (l *Line) GetCoords() (float64, float64, float64, float64) {
	return l.x1, l.y1, l.x2, l.y2
}

// LineWidth returns the width of the line.
func (l *Line) LineWidth() float64 { return l.lineWidth }

// Style returns the style of the line.
func (l *Line) Style() contentdraw.LineStyle { return l.style }

// Width returns the width of the line.
// NOTE: Depending on the fit mode the line is set to use, its width may be
// calculated at runtime (e.g. when using FitModeFillWidth).
func (l *Line) Width() float64 { return math.Abs(l.x2 - l.x1) }

// SetDashPattern sets the dash pattern of the line.
// NOTE: the dash pattern is taken into account only if the style of the
// line is set to dashed.
func (l *Line) SetDashPattern(dashArray []int64, dashPhase int64) {
	l.dashArray = dashArray
	l.dashPhase = dashPhase
}

// FitMode returns the fit mode of the line.
func (l *Line) FitMode() FitMode { return l.fitMode }

// Opacity returns the opacity of the line.
func (l *Line) Opacity() float64 { return l.opacity }

// Positioning returns the type of positioning the line is set to use.
func (l *Line) Positioning() Positioning { return l.positioning }

// SetPositioning sets the positioning of the line (absolute or relative).
func (l *Line) SetPositioning(positioning Positioning) { l.positioning = positioning }

// SetStyle sets the style of the line (solid or dashed).
func (l *Line) SetStyle(style contentdraw.LineStyle) { l.style = style }

// GetMargins returns the margins of the line: left, right, top, bottom.
func (l *Line) GetMargins() (float64, float64, float64, float64) {
	return l.margins.Left, l.margins.Right, l.margins.Top, l.margins.Bottom
}

// Color returns the color of the line.
func (l *Line) Color() Color { return l.color }

func parseLineTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseLine(node)
}

// Length calculates and returns the length of the line.
func (l *Line) Length() float64 {
	return math.Sqrt(math.Pow(l.x2-l.x1, 2.0) + math.Pow(l.y2-l.y1, 2.0))
}

// DashPattern returns the dash pattern of the line.
func (l *Line) DashPattern() (dashArray []int64, dashPhase int64) { return l.dashArray, l.dashPhase }

// SetOpacity sets the opacity of the line (0-1).
func (l *Line) SetOpacity(opacity float64) { l.opacity = opacity }

func newLine(x1, y1, x2, y2 float64) *Line {
	return &Line{x1: x1, y1: y1, x2: x2, y2: y2, color: ColorBlack, opacity: 1.0, lineWidth: 1.0, dashArray: []int64{1, 1}, positioning: PositionAbsolute}
}

func (l *Line) computeCoords(ctx DrawContext) (x1, y1, x2, y2 float64) {
	x1 = ctx.X
	x2 = x1 + l.x2 - l.x1
	lineWidth := l.lineWidth
	if l.x1 == l.x2 {
		lineWidth /= 2
	}
	if l.y1 < l.y2 {
		y1 = ctx.PageHeight - ctx.Y - lineWidth
		y2 = y1 - l.y2 + l.y1
	} else {
		y2 = ctx.PageHeight - ctx.Y - lineWidth
		y1 = y2 - l.y1 + l.y2
	}
	switch l.fitMode {
	case FitModeFillWidth:
		x2 = x1 + ctx.Width
	}
	return x1, y1, x2, y2
}

var lineAttrs = map[string]attrApplyFunc{"x1": floatAttr(func(line *Line, x1 float64) { line.x1 = x1 }), "y1": floatAttr(func(line *Line, y1 float64) { line.y1 = y1 }), "x2": floatAttr(func(line *Line, x2 float64) { line.x2 = x2 }), "y2": floatAttr(func(line *Line, y float64) { line.y2 = y }), "thickness": floatAttr(func(line *Line, width float64) { line.SetLineWidth(width) }), "color": colorAttr(func(line *Line, color Color) { line.SetColor(color) }), "style": enumAttr(lineStyleValues, func(line *Line, style contentdraw.LineStyle) { line.SetStyle(style) }), "dash-array": intArrayAttr(func(line *Line, dashArray []int64) { line.SetDashPattern(dashArray, line.dashPhase) }), "dash-phase": intAttr(func(line *Line, dashPhase int64) { line.SetDashPattern(line.dashArray, dashPhase) }), "opacity": floatAttr(func(line *Line, opacity float64) { line.SetOpacity(opacity) }), "position": enumAttr(positioningValues, func(line *Line, positioning Positioning) { line.SetPositioning(positioning) }), "fit-mode": enumAttr(fitModeValues, func(line *Line, fitMode FitMode) { line.SetFitMode(fitMode) }), "margin": marginAttr(func(line *Line, margins Margins) {
	line.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
})}

// lineState is the per-line state shared by the chunks of one line.
type lineState struct {
	index   int
	chunks  []*TextChunk
	x       float64
	advance float64
	extent  float64
	isLast  bool

	// spaceWx is the width of a space, adjusted per chunk or for justification.
	spaceWx float64
	// widths holds the measured width of each chunk (in thousandths of a unit).
	widths []float64
	// refMetrics are the metrics of the largest chunk on the line.
	refMetrics *fontMetrics
	// chunkIndex counts the chunks drawn so far (excluding a drop cap).
	chunkIndex int
}

// lineWidths is the result of measuring the chunks of a line.
type lineWidths struct {
	total      float64
	spaceWx    float64
	spaceCount uint
	widths     []float64
	refMetrics *fontMetrics
}
