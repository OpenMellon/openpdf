package creator

import (
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
)

// Ellipse defines an ellipse with a center at (xc,yc) and a specified width and height.  The ellipse can have a colored
// fill and/or border with a specified width.
// Implements the Drawable interface and can be drawn on PDF using the Creator.
type Ellipse struct {
	taggedDrawable
	xc            float64
	yc            float64
	width         float64
	height        float64
	positioning   Positioning
	fillColor     Color
	fillOpacity   float64
	borderColor   Color
	borderWidth   float64
	borderOpacity float64
	margins       Margins
	fitMode       FitMode
}

var ellipseAttrs = map[string]attrApplyFunc{"cx": floatAttr(func(ellipse *Ellipse, xc float64) { ellipse.xc = xc }), "cy": floatAttr(func(ellipse *Ellipse, yc float64) { ellipse.yc = yc }), "width": floatAttr(func(ellipse *Ellipse, width float64) { ellipse.SetWidth(width) }), "height": floatAttr(func(ellipse *Ellipse, height float64) { ellipse.SetHeight(height) }), "fill-color": colorAttr(func(ellipse *Ellipse, col Color) { ellipse.SetFillColor(col) }), "fill-opacity": floatAttr(func(ellipse *Ellipse, opacity float64) { ellipse.SetFillOpacity(opacity) }), "border-color": colorAttr(func(ellipse *Ellipse, col Color) { ellipse.SetBorderColor(col) }), "border-opacity": floatAttr(func(ellipse *Ellipse, opacity float64) { ellipse.SetBorderOpacity(opacity) }), "border-width": floatAttr(func(ellipse *Ellipse, bw float64) { ellipse.SetBorderWidth(bw) }), "position": enumAttr(positioningValues, func(ellipse *Ellipse, position Positioning) { ellipse.SetPositioning(position) }), "fit-mode": enumAttr(fitModeValues, func(ellipse *Ellipse, fitMode FitMode) { ellipse.SetFitMode(fitMode) }), "margin": marginAttr(func(ellipse *Ellipse, margins Margins) {
	ellipse.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
})}

// SetWidth sets the width of the ellipse.
func (e *Ellipse) SetWidth(width float64) { e.width = width }

// ScaleToHeight scales the ellipse to the specified height. The width of
// the ellipse is scaled so that the aspect ratio is maintained.
func (e *Ellipse) ScaleToHeight(h float64) {
	width := e.width / e.height
	e.height = h
	e.width = h * width
}

// SetFillColor sets the fill color of the ellipse.
func (e *Ellipse) SetFillColor(col Color) { e.fillColor = col }

// SetBorderColor sets the border color of the ellipse.
func (e *Ellipse) SetBorderColor(col Color) { e.borderColor = col }

// SetFitMode sets the fit mode of the ellipse.
// NOTE: The fit mode is only applied if relative positioning is used.
func (e *Ellipse) SetFitMode(fitMode FitMode) { e.fitMode = fitMode }

// ScaleToWidth scales the ellipse to the specified width. The height of
// the ellipse is scaled so that the aspect ratio is maintained.
func (e *Ellipse) ScaleToWidth(w float64) {
	height := e.height / e.width
	e.width = w
	e.height = w * height
}

// BorderWidth returns the border width of the ellipse.
func (e *Ellipse) BorderWidth() float64 { return e.borderWidth }

// SetCoords sets the center coordinates of the ellipse.
func (e *Ellipse) SetCoords(xc, yc float64) { e.xc = xc; e.yc = yc }

// Positioning returns the type of positioning the ellipse is set to use.
func (e *Ellipse) Positioning() Positioning { return e.positioning }

// BorderColor returns the border color of the ellipse.
func (e *Ellipse) BorderColor() Color { return e.borderColor }

func (e *Ellipse) applyFitMode(left float64) {
	left -= e.margins.Left + e.margins.Right
	switch e.fitMode {
	case FitModeFillWidth:
		e.ScaleToWidth(left)
	}
}

// GeneratePageBlocks draws the ellipse on a new block representing the page.
func (e *Ellipse) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	var (
		blocks []*Block
		block  = NewBlock(ctx.PageWidth, ctx.PageHeight)
		ctx2   = ctx
	)
	isRelative := e.positioning.IsRelative()
	if isRelative {
		e.applyFitMode(ctx.Width)
		ctx.X += e.margins.Left
		ctx.Y += e.margins.Top
		ctx.Width -= e.margins.Left + e.margins.Right
		ctx.Height -= e.margins.Top + e.margins.Bottom
		if e.height > ctx.Height {
			blocks = append(blocks, block)
			block = NewBlock(ctx.PageWidth, ctx.PageHeight)
			ctx.Page++
			ctx2 := ctx
			ctx2.Y = ctx.Margins.Top + e.margins.Top
			ctx2.X = ctx.Margins.Left + e.margins.Left
			ctx2.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom - e.margins.Top - e.margins.Bottom
			ctx2.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right - e.margins.Left - e.margins.Right
			ctx = ctx2
		}
	} else {
		ctx.X = e.xc - e.width/2
		ctx.Y = e.yc - e.height/2
	}
	circle := contentdraw.Circle{X: ctx.X, Y: ctx.PageHeight - ctx.Y - e.height, Width: e.width, Height: e.height, BorderWidth: e.borderWidth, Opacity: 1.0}
	if e.fillColor != nil {
		circle.FillEnabled = true
		pdfColor := toPdfColor(e.fillColor)
		err := applyShapeColor(block, pdfColor, e.fillColor, func() Rectangle {
			return Rectangle{x: circle.X, y: circle.Y, width: circle.Width, height: circle.Height}
		})
		if err != nil {
			return nil, ctx, err
		}
		circle.FillColor = pdfColor
	}
	if e.borderColor != nil {
		circle.BorderEnabled = false
		if e.borderWidth > 0 {
			circle.BorderEnabled = true
		}
		circle.BorderColor = toPdfColor(e.borderColor)
		circle.BorderWidth = e.borderWidth
	}
	gsName, err := block.setOpacity(e.fillOpacity, e.borderOpacity)
	if err != nil {
		return nil, ctx, err
	}
	markedDraw, _, err := circle.MarkedDraw(gsName, e.tagInfo)
	if err != nil {
		return nil, ctx, err
	}
	err = block.addContentsByString(string(markedDraw))
	if err != nil {
		return nil, ctx, err
	}
	if isRelative {
		ctx.X = ctx2.X
		ctx.Width = ctx2.Width
		ctx.Y += e.height + e.margins.Bottom
		ctx.Height -= e.height
	} else {
		ctx = ctx2
	}
	blocks = append(blocks, block)
	return blocks, ctx, nil
}

// SetPositioning sets the positioning of the ellipse (absolute or relative).
func (e *Ellipse) SetPositioning(position Positioning) { e.positioning = position }

// SetBorderWidth sets the border width of the ellipse.
func (e *Ellipse) SetBorderWidth(bw float64) { e.borderWidth = bw }

// SetBorderOpacity sets the border opacity of the ellipse.
func (e *Ellipse) SetBorderOpacity(opacity float64) { e.borderOpacity = opacity }

// GetCoords returns the center coordinates of ellipse (`xc`, `yc`).
func (e *Ellipse) GetCoords() (float64, float64) { return e.xc, e.yc }

// FitMode returns the fit mode of the ellipse.
func (e *Ellipse) FitMode() FitMode { return e.fitMode }

// BorderOpacity returns the border opacity of the ellipse (0-1).
func (e *Ellipse) BorderOpacity() float64 { return e.borderOpacity }

// SetHeight sets the height of the ellipse.
func (e *Ellipse) SetHeight(height float64) { e.height = height }

// GetMargins returns the margins of the ellipse: left, right, top, bottom.
func (e *Ellipse) GetMargins() (float64, float64, float64, float64) {
	return e.margins.Left, e.margins.Right, e.margins.Top, e.margins.Bottom
}

// FillOpacity returns the fill opacity of the ellipse (0-1).
func (e *Ellipse) FillOpacity() float64 { return e.fillOpacity }

func newEllipse(xc, yc, width, height float64) *Ellipse {
	return &Ellipse{xc: xc, yc: yc, width: width, height: height, positioning: PositionAbsolute, fillOpacity: 1.0, borderColor: ColorBlack, borderWidth: 1.0, borderOpacity: 1.0}
}

func parseEllipseTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseEllipse(node)
}

// Height returns the height of the ellipse.
func (e *Ellipse) Height() float64 { return e.height }

// Width returns the width of the ellipse.
func (e *Ellipse) Width() float64 { return e.width }

// SetMargins sets the margins of the ellipse.
// NOTE: ellipse margins are only applied if relative positioning is used.
func (e *Ellipse) SetMargins(left, right, top, bottom float64) {
	e.margins.Left = left
	e.margins.Right = right
	e.margins.Top = top
	e.margins.Bottom = bottom
}

// FillColor returns the fill color of the ellipse.
func (e *Ellipse) FillColor() Color { return e.fillColor }

// SetFillOpacity sets the fill opacity of the ellipse.
func (e *Ellipse) SetFillOpacity(opacity float64) { e.fillOpacity = opacity }

// Scale scales the ellipse dimensions by the specified factors.
func (e *Ellipse) Scale(xFactor, yFactor float64) {
	e.width = xFactor * e.width
	e.height = yFactor * e.height
}
