package creator

import (
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	chartintegration "github.com/OpenMellon/openpdf/internal/integrations/unichart"
	"github.com/OpenMellon/openpdf/model"
	chartrender "github.com/unidoc/unichart/render"
)

// Chart represents a chart drawable.
// It is used to render unichart chart components using a creator instance.
type Chart struct {
	taggedDrawable
	chart       chartrender.ChartRenderable
	positioning Positioning
	xPos        float64
	yPos        float64
	margins     Margins
}

// GetMargins returns the margins of the chart (left, right, top, bottom).
func (cha *Chart) GetMargins() (float64, float64, float64, float64) {
	return cha.margins.Left, cha.margins.Right, cha.margins.Top, cha.margins.Bottom
}

// NewChart creates a new creator drawable based on the provided
// unichart chart component.
func NewChart(chart chartrender.ChartRenderable) *Chart { return newChart(chart) }

func parseChartTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseChart(node)
}

// GeneratePageBlocks draws the chart onto a block.
func (cha *Chart) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	ctx2 := ctx
	isRelative := cha.positioning.IsRelative()
	var blocks []*Block
	if isRelative {
		value := 1.0
		top := cha.margins.Top
		if float64(cha.chart.Height()) > ctx.Height-cha.margins.Top {
			blocks = []*Block{NewBlock(ctx.PageWidth, ctx.PageHeight-ctx.Y)}
			var err error
			if _, ctx, err = newPageBreak().GeneratePageBlocks(ctx); err != nil {
				return nil, ctx, err
			}
			top = 0
		}
		ctx.X += cha.margins.Left + value
		ctx.Y += top
		ctx.Width -= cha.margins.Left + cha.margins.Right + 2*value
		ctx.Height -= top
		cha.chart.SetWidth(int(ctx.Width))
	} else {
		ctx.X = cha.xPos
		ctx.Y = cha.yPos
	}
	contentCreator := contentstream.NewContentCreator()
	if cha.tagInfo != nil {
		contentCreator.Add_BDC(*core.MakeName(string(cha.tagInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(cha.tagInfo.Mcid)})
	}
	contentCreator.Translate(0, ctx.PageHeight)
	contentCreator.Scale(1, -1)
	contentCreator.Translate(ctx.X, ctx.Y)
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	cha.chart.Render(chartintegration.NewRenderer(contentCreator, block.resources), nil)
	if cha.tagInfo != nil {
		contentCreator.Add_EMC()
	}
	if err := block.addContentsByString(contentCreator.String()); err != nil {
		return nil, ctx, err
	}
	if isRelative {
		bottom := cha.Height() + cha.margins.Bottom
		ctx.Y += bottom
		ctx.Height -= bottom
	} else {
		ctx = ctx2
	}
	blocks = append(blocks, block)
	return blocks, ctx, nil
}

// Width returns the width of the chart. In relative positioning mode,
// all the available context width is used at render time.
func (cha *Chart) Width() float64 { return float64(cha.chart.Width()) }

// Height returns the height of the chart.
func (cha *Chart) Height() float64 { return float64(cha.chart.Height()) }

// SetMargins sets the margins of the chart component.
func (cha *Chart) SetMargins(left, right, top, bottom float64) {
	cha.margins.Left = left
	cha.margins.Right = right
	cha.margins.Top = top
	cha.margins.Bottom = bottom
}

func newChart(chart chartrender.ChartRenderable) *Chart {
	return &Chart{chart: chart, positioning: PositionRelative, margins: Margins{Top: 10, Bottom: 10}, taggedDrawable: taggedDrawable{structType: model.StructureTypeFigure}}
}

var chartAttrs = map[string]attrApplyFunc{"src": nil, "x": floatAttr(func(chart *Chart, x float64) { chart.SetPos(x, chart.yPos) }), "y": floatAttr(func(chart *Chart, y float64) { chart.SetPos(chart.xPos, y) }), "margin": marginAttr(func(chart *Chart, margins Margins) {
	chart.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
}), "width": floatAttr(func(chart *Chart, value float64) { chart.chart.SetWidth(int(value)) }), "height": floatAttr(func(chart *Chart, value float64) { chart.chart.SetHeight(int(value)) })}

// SetPos sets the position of the chart to the specified coordinates.
// This method sets the chart to use absolute positioning.
func (cha *Chart) SetPos(x, y float64) {
	cha.positioning = PositionAbsolute
	cha.xPos = x
	cha.yPos = y
}
