package creator

import (
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
)

// Rectangle defines a rectangle with upper left corner at (x,y) and a specified width and height.  The rectangle
// can have a colored fill and/or border with a specified width.
// Implements the Drawable interface and can be drawn on PDF using the Creator.
type Rectangle struct {
	taggedDrawable
	x                       float64
	y                       float64
	width                   float64
	height                  float64
	positioning             Positioning
	fillColor               Color
	fillOpacity             float64
	borderColor             Color
	borderWidth             float64
	borderOpacity           float64
	borderRadiusTopLeft     float64
	borderRadiusTopRight    float64
	borderRadiusBottomLeft  float64
	borderRadiusBottomRight float64
	margins                 Margins
	fitMode                 FitMode
}

// SetBorderOpacity sets the border opacity of the rectangle.
func (r *Rectangle) SetBorderOpacity(opacity float64) { r.borderOpacity = opacity }

// FillColor returns the fill color of the rectangle.
func (r *Rectangle) FillColor() Color { return r.fillColor }

func newRectangle(x, y, width, height float64) *Rectangle {
	return &Rectangle{x: x, y: y, width: width, height: height, positioning: PositionAbsolute, fillOpacity: 1.0, borderColor: ColorBlack, borderWidth: 1.0, borderOpacity: 1.0}
}

// GeneratePageBlocks draws the rectangle on a new block representing the page. Implements the Drawable interface.
func (r *Rectangle) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	var (
		blocks      []*Block
		block       = NewBlock(ctx.PageWidth, ctx.PageHeight)
		ctx2        = ctx
		borderWidth = r.borderWidth / 2
	)
	isRelative := r.positioning.IsRelative()
	if isRelative {
		r.applyFitMode(ctx.Width)
		ctx.X += r.margins.Left + borderWidth
		ctx.Y += r.margins.Top + borderWidth
		ctx.Width -= r.margins.Left + r.margins.Right
		ctx.Height -= r.margins.Top + r.margins.Bottom
		if r.height > ctx.Height {
			blocks = append(blocks, block)
			block = NewBlock(ctx.PageWidth, ctx.PageHeight)
			ctx.Page++
			ctx2 := ctx
			ctx2.Y = ctx.Margins.Top + r.margins.Top + borderWidth
			ctx2.X = ctx.Margins.Left + r.margins.Left + borderWidth
			ctx2.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom - r.margins.Top - r.margins.Bottom
			ctx2.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right - r.margins.Left - r.margins.Right
			ctx = ctx2
		}
	} else {
		ctx.X = r.x
		ctx.Y = r.y
	}
	rect := contentdraw.Rectangle{X: ctx.X, Y: ctx.PageHeight - ctx.Y - r.height, Width: r.width, Height: r.height, BorderRadiusTopLeft: r.borderRadiusTopLeft, BorderRadiusTopRight: r.borderRadiusTopRight, BorderRadiusBottomLeft: r.borderRadiusBottomLeft, BorderRadiusBottomRight: r.borderRadiusBottomRight, Opacity: 1.0}
	if r.fillColor != nil {
		rect.FillEnabled = true
		pdfColor := toPdfColor(r.fillColor)
		err := applyShapeColor(block, pdfColor, r.fillColor, func() Rectangle {
			return Rectangle{x: rect.X, y: rect.Y, width: rect.Width, height: rect.Height}
		})
		if err != nil {
			return nil, ctx, err
		}
		rect.FillColor = pdfColor
	}
	if r.borderColor != nil && r.borderWidth > 0 {
		rect.BorderEnabled = true
		rect.BorderColor = toPdfColor(r.borderColor)
		rect.BorderWidth = r.borderWidth
	}
	gsName, err := block.setOpacity(r.fillOpacity, r.borderOpacity)
	if err != nil {
		return nil, ctx, err
	}
	markedDraw, _, err := rect.MarkedDraw(gsName, r.tagInfo)
	if err != nil {
		return nil, ctx, err
	}
	if err = block.addContentsByString(string(markedDraw)); err != nil {
		return nil, ctx, err
	}
	if isRelative {
		ctx.X = ctx2.X
		ctx.Width = ctx2.Width
		height := r.height + borderWidth
		ctx.Y += height + r.margins.Bottom
		ctx.Height -= height
	} else {
		ctx = ctx2
	}
	blocks = append(blocks, block)
	return blocks, ctx, nil
}

// SetFillColor sets the fill color of the rectangle.
func (r *Rectangle) SetFillColor(col Color) { r.fillColor = col }

// GetMargins returns the margins of the rectangle: left, right, top, bottom.
func (r *Rectangle) GetMargins() (float64, float64, float64, float64) {
	return r.margins.Left, r.margins.Right, r.margins.Top, r.margins.Bottom
}

// SetPositioning sets the positioning of the rectangle (absolute or relative).
func (r *Rectangle) SetPositioning(position Positioning) { r.positioning = position }

// ScaleToWidth scales the rectangle to the specified width. The height of
// the rectangle is scaled so that the aspect ratio is maintained.
func (r *Rectangle) ScaleToWidth(w float64) {
	height := r.height / r.width
	r.width = w
	r.height = w * height
}

// Positioning returns the type of positioning the rectangle is set to use.
func (r *Rectangle) Positioning() Positioning { return r.positioning }

// BorderWidth returns the border width of the rectangle.
func (r *Rectangle) BorderWidth() float64 { return r.borderWidth }

// SetWidth sets the width of the rectangle.
func (r *Rectangle) SetWidth(width float64) { r.width = width }

func parseRectangleTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseRectangle(node)
}

// BorderColor returns the border color of the rectangle.
func (r *Rectangle) BorderColor() Color { return r.borderColor }

// SetHeight sets the height of the rectangle.
func (r *Rectangle) SetHeight(height float64) { r.height = height }

// Scale scales the rectangle dimensions by the specified factors.
func (r *Rectangle) Scale(xFactor, yFactor float64) {
	r.width = xFactor * r.width
	r.height = yFactor * r.height
}

// Height returns the height of the rectangle.
// NOTE: the returned value does not include the border width of the rectangle.
func (r *Rectangle) Height() float64 { return r.height }

// SetBorderRadius sets the radius of the rectangle corners.
func (r *Rectangle) SetBorderRadius(topLeft, topRight, bottomLeft, bottomRight float64) {
	r.borderRadiusTopLeft = topLeft
	r.borderRadiusTopRight = topRight
	r.borderRadiusBottomLeft = bottomLeft
	r.borderRadiusBottomRight = bottomRight
}

func (r *Rectangle) applyFitMode(borderWidth float64) {
	borderWidth -= r.margins.Left + r.margins.Right + r.borderWidth
	switch r.fitMode {
	case FitModeFillWidth:
		r.ScaleToWidth(borderWidth)
	}
}

// BorderOpacity returns the border opacity of the rectangle (0-1).
func (r *Rectangle) BorderOpacity() float64 { return r.borderOpacity }

// FitMode returns the fit mode of the rectangle.
func (r *Rectangle) FitMode() FitMode { return r.fitMode }

// SetMargins sets the margins of the rectangle.
// NOTE: rectangle margins are only applied if relative positioning is used.
func (r *Rectangle) SetMargins(left, right, top, bottom float64) {
	r.margins.Left = left
	r.margins.Right = right
	r.margins.Top = top
	r.margins.Bottom = bottom
}

// Width returns the width of the rectangle.
// NOTE: the returned value does not include the border width of the rectangle.
func (r *Rectangle) Width() float64 { return r.width }

var rectangleAttrs = map[string]attrApplyFunc{"x": floatAttr(func(rect *Rectangle, x float64) { rect.x = x }), "y": floatAttr(func(rect *Rectangle, y float64) { rect.y = y }), "width": floatAttr(func(rect *Rectangle, width float64) { rect.SetWidth(width) }), "height": floatAttr(func(rect *Rectangle, height float64) { rect.SetHeight(height) }), "fill-color": colorAttr(func(rect *Rectangle, col Color) { rect.SetFillColor(col) }), "fill-opacity": floatAttr(func(rect *Rectangle, opacity float64) { rect.SetFillOpacity(opacity) }), "border-color": colorAttr(func(rect *Rectangle, col Color) { rect.SetBorderColor(col) }), "border-opacity": floatAttr(func(rect *Rectangle, opacity float64) { rect.SetBorderOpacity(opacity) }), "border-width": floatAttr(func(rect *Rectangle, bw float64) { rect.SetBorderWidth(bw) }), "border-radius": borderRadiusAttr(func(rect *Rectangle, topLeft, topRight, bottomRight, bottomLeft float64) {
	rect.SetBorderRadius(topLeft, topRight, bottomLeft, bottomRight)
}), "border-top-left-radius": floatAttr(func(rect *Rectangle, borderRadiusTopLeft float64) { rect.borderRadiusTopLeft = borderRadiusTopLeft }), "border-top-right-radius": floatAttr(func(rect *Rectangle, borderRadiusTopRight float64) { rect.borderRadiusTopRight = borderRadiusTopRight }), "border-bottom-left-radius": floatAttr(func(rect *Rectangle, borderRadiusBottomLeft float64) {
	rect.borderRadiusBottomLeft = borderRadiusBottomLeft
}), "border-bottom-right-radius": floatAttr(func(rect *Rectangle, borderRadiusBottomRight float64) {
	rect.borderRadiusBottomRight = borderRadiusBottomRight
}), "position": enumAttr(positioningValues, func(rect *Rectangle, position Positioning) { rect.SetPositioning(position) }), "fit-mode": enumAttr(fitModeValues, func(rect *Rectangle, fitMode FitMode) { rect.SetFitMode(fitMode) }), "margin": marginAttr(func(rect *Rectangle, margins Margins) {
	rect.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
})}

// SetBorderWidth sets the border width of the rectangle.
func (r *Rectangle) SetBorderWidth(bw float64) { r.borderWidth = bw }

// ScaleToHeight scales the rectangle to the specified height. The width of
// the rectangle is scaled so that the aspect ratio is maintained.
func (r *Rectangle) ScaleToHeight(h float64) {
	width := r.width / r.height
	r.height = h
	r.width = h * width
}

// SetFillOpacity sets the fill opacity of the rectangle.
func (r *Rectangle) SetFillOpacity(opacity float64) { r.fillOpacity = opacity }

// SetBorderColor sets border color of the rectangle.
func (r *Rectangle) SetBorderColor(col Color) { r.borderColor = col }

// SetCoords sets the upper left corner coordinates of the rectangle.
func (r *Rectangle) SetCoords(x, y float64) { r.x = x; r.y = y }

// FillOpacity returns the fill opacity of the rectangle (0-1).
func (r *Rectangle) FillOpacity() float64 { return r.fillOpacity }

// GetCoords returns the upper left corner coordinates of the rectangle (`x`, `y`).
func (r *Rectangle) GetCoords() (float64, float64) { return r.x, r.y }

// SetFitMode sets the fit mode of the rectangle.
// NOTE: The fit mode is only applied if relative positioning is used.
func (r *Rectangle) SetFitMode(fitMode FitMode) { r.fitMode = fitMode }
