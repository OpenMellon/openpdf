package creator

import (
	"math"

	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// GraphicSVG represents a drawable graphic SVG.
// It is used to render the graphic SVG components using a creator instance.
type GraphicSVG struct {
	taggedDrawable
	root        *GraphicSVGElement
	positioning Positioning
	xPos        float64
	yPos        float64
	margins     Margins
}

// NewGraphicSVGFromString creates a graphic SVG from a SVG string.
func NewGraphicSVGFromString(svgStr string) (*GraphicSVG, error) {
	return newGraphicSVGFromString(svgStr)
}

// SetMargins sets the margins of the graphic svg component.
func (gs *GraphicSVG) SetMargins(left, right, top, bottom float64) {
	gs.margins.Left = left
	gs.margins.Right = right
	gs.margins.Top = top
	gs.margins.Bottom = bottom
}

func newGraphicSVGFromString(svgStr string) (*GraphicSVG, error) {
	fromSVGString, err := ParseFromSVGString(svgStr)
	if err != nil {
		return nil, err
	}
	return newGraphicSVG(fromSVGString)
}

func newGraphicSVGFromFile(path string) (*GraphicSVG, error) {
	fromSVGFile, err := ParseFromSVGFile(path)
	if err != nil {
		return nil, err
	}
	return newGraphicSVG(fromSVGFile)
}

// ScaleToHeight sets the graphic svg scaling factor with the given height.
func (gs *GraphicSVG) ScaleToHeight(h float64) {
	width := gs.root.Width / gs.root.Height
	gs.root.Height = h
	gs.root.Width = h * width
	gs.root.SetScaling(width, width)
}

func newGraphicSVG(root *GraphicSVGElement) (*GraphicSVG, error) {
	return &GraphicSVG{root: root, positioning: PositionRelative, margins: Margins{Top: 10, Bottom: 10}, taggedDrawable: taggedDrawable{structType: model.StructureTypeFigure}}, nil
}

// ScaleToWidth sets the graphic svg scaling factor with the given width.
func (gs *GraphicSVG) ScaleToWidth(w float64) {
	height := gs.root.Height / gs.root.Width
	gs.root.Width = w
	gs.root.Height = w * height
	gs.root.SetScaling(height, height)
}

// Height returns the height of the graphic svg.
func (gs *GraphicSVG) Height() float64 { return gs.root.Height }

// GeneratePageBlocks draw graphic svg into block.
func (gs *GraphicSVG) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	ctx2 := ctx
	isRelative := gs.positioning.IsRelative()
	var blocks []*Block
	if isRelative {
		value := 1.0
		top := gs.margins.Top
		if gs.root.Height > ctx.Height-gs.margins.Top {
			blocks = []*Block{NewBlock(ctx.PageWidth, ctx.PageHeight-ctx.Y)}
			var err error
			if _, ctx, err = newPageBreak().GeneratePageBlocks(ctx); err != nil {
				return nil, ctx, err
			}
			top = 0
		}
		ctx.X += gs.margins.Left + value
		ctx.Y += top
		ctx.Width -= gs.margins.Left + gs.margins.Right + 2*value
		ctx.Height -= top
	} else {
		ctx.X = gs.xPos
		ctx.Y = gs.yPos
	}
	contentCreator := contentstream.NewContentCreator()
	contentCreator.Translate(0, ctx.PageHeight)
	contentCreator.Scale(1, -1)
	contentCreator.Translate(ctx.X, ctx.Y)
	width := gs.root.Width / gs.root.ViewBox.W
	height := gs.root.Height / gs.root.ViewBox.H
	xPos := 0.0
	yPos := 0.0
	if isRelative {
		xPos = gs.xPos - (gs.root.ViewBox.X * math.Max(width, height))
		yPos = gs.yPos - (gs.root.ViewBox.Y * math.Max(width, height))
	}
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	if gs.tagInfo != nil {
		contentCreator.Add_BDC(*core.MakeName(string(gs.tagInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(gs.tagInfo.Mcid)})
	}
	gs.root.SetPos(ctx.X, ctx.Y)
	gs.root.ToContentCreator(contentCreator, block.resources, width, height, xPos, yPos)
	if gs.tagInfo != nil {
		contentCreator.Add_EMC()
	}
	operations := contentCreator.Operations()
	operations.WrapIfNeeded()
	block.addWrappedContents(operations)
	if isRelative {
		bottom := gs.Height() + gs.margins.Bottom
		ctx.Y += bottom
		ctx.Height -= bottom
	} else {
		ctx = ctx2
	}
	blocks = append(blocks, block)
	return blocks, ctx, nil
}

// SetPos sets the position of the graphic svg to the specified coordinates.
// This method sets the graphic svg to use absolute positioning.
func (gs *GraphicSVG) SetPos(x, y float64) {
	gs.positioning = PositionAbsolute
	gs.xPos = x
	gs.yPos = y
}

// GetMargins returns the margins of the graphic svg (left, right, top, bottom).
func (gs *GraphicSVG) GetMargins() (float64, float64, float64, float64) {
	return gs.margins.Left, gs.margins.Right, gs.margins.Top, gs.margins.Bottom
}

// NewGraphicSVGFromFile creates a graphic SVG from a file.
func NewGraphicSVGFromFile(path string) (*GraphicSVG, error) { return newGraphicSVGFromFile(path) }

// Width returns the width of the graphic svg.
func (gs *GraphicSVG) Width() float64 { return gs.root.Width }

// Scale sets the scale ratio with `X` factor and `Y` factor for the graphic svg.
func (gs *GraphicSVG) Scale(xFactor, yFactor float64) {
	gs.root.Width = xFactor * gs.root.Width
	gs.root.Height = yFactor * gs.root.Height
	gs.root.SetScaling(xFactor, yFactor)
}
