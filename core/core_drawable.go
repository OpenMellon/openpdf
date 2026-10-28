package core

import (
	"image"
	"image/color"
)

// DrawableImage is same as golang image/draw's Image interface that allow drawing images.
type DrawableImage interface {
	ColorModel() color.Model
	Bounds() image.Rectangle
	At(x, y int) color.Color
	Set(x, y int, c color.Color)
}
