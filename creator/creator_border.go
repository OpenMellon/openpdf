package creator

import (
	"fmt"
	"reflect"

	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

type border struct {
	x           float64
	y           float64
	width       float64
	height      float64
	fillColor   Color
	colorLeft   Color
	widthLeft   float64
	colorBottom Color
	widthBottom float64
	colorRight  Color
	widthRight  float64
	colorTop    Color
	widthTop    float64
	LineStyle   contentdraw.LineStyle
	styleLeft   CellBorderStyle
	styleRight  CellBorderStyle
	styleTop    CellBorderStyle
	styleBottom CellBorderStyle
	opacity     float64
}

// SetColorTop sets border color for top.
func (bo *border) SetColorTop(col Color) { bo.colorTop = col }

// SetFillColor sets background color for border.
func (bo *border) SetFillColor(col Color) { bo.fillColor = col }

// This method is not supported by Border component and exists solely to satisfy the Drawable interface.
func (bo *border) GenerateKDict() (*model.KDict, error) { return nil, nil }

// GeneratePageBlocks implements drawable interface.
func (bo *border) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	x := bo.x
	pageHeight := ctx.PageHeight - bo.y
	if bo.fillColor != nil {
		rect := contentdraw.Rectangle{Opacity: 1.0, X: bo.x, Y: ctx.PageHeight - bo.y - bo.height, Height: bo.height, Width: bo.width}
		rect.FillEnabled = true
		pdfColor := toPdfColor(bo.fillColor)
		err := applyShapeColor(block, pdfColor, bo.fillColor, func() Rectangle {
			return Rectangle{x: rect.X, y: rect.Y, width: rect.Width, height: rect.Height}
		})
		if err != nil {
			return nil, ctx, err
		}
		rect.FillColor = pdfColor
		rect.BorderEnabled = false
		gsName := ""
		if bo.opacity < 1 {
			dict := core.MakeDict()
			dict.Set("ca", core.MakeFloat(bo.opacity))
			dict.Set("CA", core.MakeFloat(bo.opacity))
			err := block.resources.AddExtGState("gs1", dict)
			if err == nil {
				gsName = "gs1"
			}
		}
		data, _, err := rect.Draw(gsName)
		if err != nil {
			return nil, ctx, err
		}
		err = block.addContentsByString(string(data))
		if err != nil {
			return nil, ctx, err
		}
	}
	widthTop := bo.widthTop
	widthBottom := bo.widthBottom
	widthLeft := bo.widthLeft
	widthRight := bo.widthRight
	widthTop2 := bo.widthTop
	if bo.styleTop == CellBorderStyleDouble {
		widthTop2 += 2 * widthTop
	}
	widthBottom2 := bo.widthBottom
	if bo.styleBottom == CellBorderStyleDouble {
		widthBottom2 += 2 * widthBottom
	}
	widthLeft2 := bo.widthLeft
	if bo.styleLeft == CellBorderStyleDouble {
		widthLeft2 += 2 * widthLeft
	}
	widthRight2 := bo.widthRight
	if bo.styleRight == CellBorderStyleDouble {
		widthRight2 += 2 * widthRight
	}
	value := (widthTop2 - widthLeft2) / 2
	value2 := (widthTop2 - widthRight2) / 2
	value3 := (widthBottom2 - widthLeft2) / 2
	value4 := (widthBottom2 - widthRight2) / 2
	if bo.widthTop != 0 {
		value3 := x
		y1 := pageHeight
		if bo.styleTop == CellBorderStyleDouble {
			y1 -= widthTop
			basicLine := contentdraw.BasicLine{LineColor: toPdfColor(bo.colorTop), Opacity: 1.0, LineWidth: bo.widthTop, LineStyle: bo.LineStyle, X1: value3 - widthTop2/2 + value, Y1: y1 + 2*widthTop, X2: value3 + widthTop2/2 - value2 + bo.width, Y2: y1 + 2*widthTop}
			data, _, err := basicLine.Draw("")
			if err != nil {
				return nil, ctx, err
			}
			err = block.addContentsByString(string(data))
			if err != nil {
				return nil, ctx, err
			}
		}
		basicLine := contentdraw.BasicLine{LineWidth: bo.widthTop, Opacity: 1.0, LineColor: toPdfColor(bo.colorTop), LineStyle: bo.LineStyle, X1: value3 - widthTop2/2 + value + (widthLeft2 - bo.widthLeft), Y1: y1, X2: value3 + widthTop2/2 - value2 + bo.width - (widthRight2 - bo.widthRight), Y2: y1}
		data, _, err := basicLine.Draw("")
		if err != nil {
			return nil, ctx, err
		}
		err = block.addContentsByString(string(data))
		if err != nil {
			return nil, ctx, err
		}
	}
	if bo.widthBottom != 0 {
		value := x
		y1 := pageHeight - bo.height
		if bo.styleBottom == CellBorderStyleDouble {
			y1 += widthBottom
			basicLine := contentdraw.BasicLine{LineWidth: bo.widthBottom, Opacity: 1.0, LineColor: toPdfColor(bo.colorBottom), LineStyle: bo.LineStyle, X1: value - widthBottom2/2 + value3, Y1: y1 - 2*widthBottom, X2: value + widthBottom2/2 - value4 + bo.width, Y2: y1 - 2*widthBottom}
			data, _, err := basicLine.Draw("")
			if err != nil {
				return nil, ctx, err
			}
			err = block.addContentsByString(string(data))
			if err != nil {
				return nil, ctx, err
			}
		}
		basicLine := contentdraw.BasicLine{LineWidth: bo.widthBottom, Opacity: 1.0, LineColor: toPdfColor(bo.colorBottom), LineStyle: bo.LineStyle, X1: value - widthBottom2/2 + value3 + (widthLeft2 - bo.widthLeft), Y1: y1, X2: value + widthBottom2/2 - value4 + bo.width - (widthRight2 - bo.widthRight), Y2: y1}
		data, _, err := basicLine.Draw("")
		if err != nil {
			return nil, ctx, err
		}
		err = block.addContentsByString(string(data))
		if err != nil {
			return nil, ctx, err
		}
	}
	if bo.widthLeft != 0 {
		x1 := x
		value2 := pageHeight
		if bo.styleLeft == CellBorderStyleDouble {
			x1 += widthLeft
			basicLine := contentdraw.BasicLine{LineWidth: bo.widthLeft, Opacity: 1.0, LineColor: toPdfColor(bo.colorLeft), LineStyle: bo.LineStyle, X1: x1 - 2*widthLeft, Y1: value2 + widthLeft2/2 + value, X2: x1 - 2*widthLeft, Y2: value2 - widthLeft2/2 - value3 - bo.height}
			data, _, err := basicLine.Draw("")
			if err != nil {
				return nil, ctx, err
			}
			err = block.addContentsByString(string(data))
			if err != nil {
				return nil, ctx, err
			}
		}
		basicLine := contentdraw.BasicLine{LineWidth: bo.widthLeft, Opacity: 1.0, LineColor: toPdfColor(bo.colorLeft), LineStyle: bo.LineStyle, X1: x1, Y1: value2 + widthLeft2/2 + value - (widthTop2 - bo.widthTop), X2: x1, Y2: value2 - widthLeft2/2 - value3 - bo.height + (widthBottom2 - bo.widthBottom)}
		data, _, err := basicLine.Draw("")
		if err != nil {
			return nil, ctx, err
		}
		err = block.addContentsByString(string(data))
		if err != nil {
			return nil, ctx, err
		}
	}
	if bo.widthRight != 0 {
		x1 := x + bo.width
		value := pageHeight
		if bo.styleRight == CellBorderStyleDouble {
			x1 -= widthRight
			basicLine := contentdraw.BasicLine{LineWidth: bo.widthRight, Opacity: 1.0, LineColor: toPdfColor(bo.colorRight), LineStyle: bo.LineStyle, X1: x1 + 2*widthRight, Y1: value + widthRight2/2 + value2, X2: x1 + 2*widthRight, Y2: value - widthRight2/2 - value4 - bo.height}
			data, _, err := basicLine.Draw("")
			if err != nil {
				return nil, ctx, err
			}
			err = block.addContentsByString(string(data))
			if err != nil {
				return nil, ctx, err
			}
		}
		basicLine := contentdraw.BasicLine{LineWidth: bo.widthRight, Opacity: 1.0, LineColor: toPdfColor(bo.colorRight), LineStyle: bo.LineStyle, X1: x1, Y1: value + widthRight2/2 + value2 - (widthTop2 - bo.widthTop), X2: x1, Y2: value - widthRight2/2 - value4 - bo.height + (widthBottom2 - bo.widthBottom)}
		data, _, err := basicLine.Draw("")
		if err != nil {
			return nil, ctx, err
		}
		err = block.addContentsByString(string(data))
		if err != nil {
			return nil, ctx, err
		}
	}
	return []*Block{block}, ctx, nil
}

// SetStyleTop sets border style for top side.
func (bo *border) SetStyleTop(style CellBorderStyle) { bo.styleTop = style }

// SetWidthBottom sets border width for bottom.
func (bo *border) SetWidthBottom(bw float64) { bo.widthBottom = bw }

// SetWidthRight sets border width for right.
func (bo *border) SetWidthRight(bw float64) { bo.widthRight = bw }

// SetWidthTop sets border width for top.
func (bo *border) SetWidthTop(bw float64) { bo.widthTop = bw }

// This method is not supported by Border component and exists solely to satisfy the Drawable interface.
func (bo *border) SetMarkedContentID(id int64) {}

// SetColorLeft sets border color for left.
func (bo *border) SetColorLeft(col Color) { bo.colorLeft = col }

func parseBorderRadius(s string) (topLeft, topRight, bottomRight, bottomLeft float64, err error) {
	floatFields, err := parseFloatFields(s)
	switch len(floatFields) {
	case 0:
	case 1:
		topLeft, topRight, bottomRight, bottomLeft = floatFields[0], floatFields[0], floatFields[0], floatFields[0]
	case 2:
		topLeft, bottomRight = floatFields[0], floatFields[0]
		topRight, bottomLeft = floatFields[1], floatFields[1]
	case 3:
		topLeft = floatFields[0]
		topRight, bottomLeft = floatFields[1], floatFields[1]
		bottomRight = floatFields[2]
	case 4:
		topLeft, topRight, bottomRight, bottomLeft = floatFields[0], floatFields[1], floatFields[2], floatFields[3]
	default:
		if err == nil {
			err = fmt.Errorf("border-radius expects 1-4 values, got %d", len(floatFields))
		}
	}
	return topLeft, topRight, bottomRight, bottomLeft, err
}

// SetStyleRight sets border style for right side.
func (bo *border) SetStyleRight(style CellBorderStyle) { bo.styleRight = style }

// SetStyleBottom sets border style for bottom side.
func (bo *border) SetStyleBottom(style CellBorderStyle) { bo.styleBottom = style }

// This method is not supported by Border component and exists solely to satisfy the Drawable interface.
func (bo *border) SetStructPageNumber(pageNumber *int64) {}

// This method is not supported by Border component and exists solely to satisfy the Drawable interface.
func (bo *border) SetStructureType(structureType model.StructureType) {}

// GetCoords returns coordinates of border.
func (bo *border) GetCoords() (float64, float64) { return bo.x, bo.y }

// SetColorRight sets border color for right.
func (bo *border) SetColorRight(col Color) { bo.colorRight = col }

func borderRadiusAttr(i any) attrApplyFunc {
	value2 := validateSetter("borderRadiusAttr", i, 0, float64Type, float64Type, float64Type, float64Type)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		topLeft, topRight, bottomRight, bottomLeft, err := parseBorderRadius(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
		}
		callSetter(value2, target, reflect.ValueOf(topLeft), reflect.ValueOf(topRight), reflect.ValueOf(bottomRight), reflect.ValueOf(bottomLeft))
		return nil
	}
}

// SetStyleLeft sets border style for left side.
func (bo *border) SetStyleLeft(style CellBorderStyle) { bo.styleLeft = style }

// SetOpacity sets opacity for border in range 0-1.
func (bo *border) SetOpacity(opacity float64) { bo.opacity = opacity }

func newBorder(x, y, width, height float64) *border {
	border2 := &border{}
	border2.x = x
	border2.y = y
	border2.width = width
	border2.height = height
	border2.colorTop = ColorBlack
	border2.colorBottom = ColorBlack
	border2.colorLeft = ColorBlack
	border2.colorRight = ColorBlack
	border2.widthTop = 0
	border2.widthBottom = 0
	border2.widthLeft = 0
	border2.widthRight = 0
	border2.opacity = 1.0
	border2.LineStyle = contentdraw.LineStyleSolid
	return border2
}

// SetWidthLeft sets border width for left.
func (bo *border) SetWidthLeft(bw float64) { bo.widthLeft = bw }

// SetColorBottom sets border color for bottom.
func (bo *border) SetColorBottom(col Color) { bo.colorBottom = col }
