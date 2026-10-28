// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func parseSVGStyle(m map[string]string, value2 float64) (*GraphicSVGStyle, error) {
	graphicSVGStyle := newGraphicSVGStyle()
	fillColor, ok := m["fill"]
	if ok {
		graphicSVGStyle.FillColor = fillColor
		if fillColor == "none" {
			graphicSVGStyle.FillColor = ""
		}
	}
	s, ok2 := m["fill-opacity"]
	if ok2 {
		percentOrFloat, err := parsePercentOrFloat(s)
		if err != nil {
			return nil, err
		}
		graphicSVGStyle.FillOpacity = percentOrFloat
	}
	strokeColor, ok3 := m["stroke"]
	if ok3 {
		graphicSVGStyle.StrokeColor = strokeColor
		if strokeColor == "none" {
			graphicSVGStyle.StrokeColor = ""
		}
	}
	value, ok4 := m["stroke-width"]
	if ok4 {
		svgLength, err := parseSVGLength(value, 64)
		if err != nil {
			return nil, err
		}
		graphicSVGStyle.StrokeWidth = svgLength * value2
	}
	return graphicSVGStyle, nil
}

func newFilledCurve() *FilledCurve {
	filledCurve := FilledCurve{}
	filledCurve.curves = []contentdraw.CubicBezierCurve{}
	return &filledCurve
}

func (d *Division) ctxHeight(right float64) float64 {
	right -= d.margins.Left + d.margins.Right + d.padding.Left + d.padding.Right
	var ctxHeight float64
	for _, component := range d.components {
		ctxHeight += drawableHeight(component, right)
	}
	return ctxHeight
}

func newEllipse(xc, yc, width, height float64) *Ellipse {
	return &Ellipse{xc: xc, yc: yc, width: width, height: height, positioning: PositionAbsolute, fillOpacity: 1.0, borderColor: ColorBlack, borderWidth: 1.0, borderOpacity: 1.0}
}

// GetPos returns the absolute coordinates (x, y) of the paragraph. The
// coordinates are only meaningful when the paragraph uses absolute
// positioning (see GetPositioning).
func (sp *StyledParagraph) GetPos() (float64, float64) { return sp.xPos, sp.yPos }

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

func (tp *templateProcessor) processGradientColorPair(strs []string) (colors []Color, positions []float64, err error) {
	var flags []bool
	for _, s := range strs {
		fields := strings.Fields(s)
		if len(fields) == 0 {
			setFirstError(&err, errors.New("empty gradient color stop"))
			continue
		}
		if len(fields) > 2 {
			setFirstError(&err, fmt.Errorf("gradient stop %q has extra values", strings.TrimSpace(s)))
		}
		finiteFloat2, ok := 0.0, false
		if len(fields) > 1 {
			field := fields[1]
			if field == "0" {
				ok = true
			} else if !strings.HasSuffix(field, "%") {
				setFirstError(&err, fmt.Errorf("gradient stop position %q must be a percentage", field))
			} else if finiteFloat, err2 := parseFiniteFloat(field[:len(field)-1]); err2 != nil {
				common.Log.Debug("Failed parsing point value: %v", err2)
				setFirstError(&err, fmt.Errorf("invalid gradient stop position %q", field))
			} else {
				finiteFloat2, ok = finiteFloat/100.0, true
			}
		}
		color := tp.parseColor(fields[0])
		if color == nil {
			setFirstError(&err, fmt.Errorf("unresolved gradient stop color %q", fields[0]))
			continue
		}
		colors = append(colors, color)
		positions = append(positions, finiteFloat2)
		flags = append(flags, ok)
	}
	colorsLen := len(colors)
	if colorsLen == 0 {
		common.Log.Debug("Invalid gradient color definition!")
		setFirstError(&err, errors.New("gradient has no color stops"))
		return nil, nil, err
	}
	if colorsLen == 1 {
		setFirstError(&err, errors.New("gradient needs at least two color stops"))
	}
	if !flags[0] {
		positions[0], flags[0] = 0, true
	}
	if !flags[colorsLen-1] {
		positions[colorsLen-1], flags[colorsLen-1] = 1, true
	}
	position2 := 0.0
	for i, position := range positions {
		if !flags[i] {
			continue
		}
		if position < 0 || position > 1 {
			setFirstError(&err, fmt.Errorf("gradient stop position %g%% is outside 0-100%%", position*100))
			position = math.Max(0, math.Min(1, position))
		}
		if position < position2 {
			setFirstError(&err, fmt.Errorf("gradient stop position %g%% is less than an earlier stop", position*100))
			position = position2
		}
		positions[i], position2 = position, position
	}
	n := 0
	for i := 1; i < colorsLen; i++ {
		if !flags[i] {
			continue
		}
		for j := n + 1; j < i; j++ {
			positions[j] = positions[n] + float64(j-n)*(positions[i]-positions[n])/float64(i-n)
		}
		n = i
	}
	return colors, positions, err
}

// Width returns the width of the graphic svg.
func (gs *GraphicSVG) Width() float64 { return gs.root.Width }

func parseNumberList(s string) (numbers []float64, err error) {
	var value float64
	n := 0
	ok := true
	for i, r := range s {
		if r == '.' {
			if ok {
				ok = false
				continue
			}
			value, err = parseSVGLength(s[n:i], 64)
			if err != nil {
				return
			}
			numbers = append(numbers, value)
			n = i
		}
	}
	value, err = parseSVGLength(s[n:], 64)
	if err != nil {
		return
	}
	numbers = append(numbers, value)
	return
}

func (t *Table) sortCells() {
	sort.Slice(t.cells, func(i, j int) bool {
		row := t.cells[i].row
		row2 := t.cells[j].row
		if row < row2 {
			return true
		}
		if row > row2 {
			return false
		}
		return t.cells[i].col < t.cells[j].col
	})
}

// Lines returns all the rows of the invoice line items table.
func (i *Invoice) Lines() [][]*InvoiceCell { return i.lines }

// Block contains a portion of PDF Page contents. It has a width and a position and can
// be placed anywhere on a Page.  It can even contain a whole Page, and is used in the creator
// where each Drawable object can output one or more blocks, each representing content for separate pages
// (typically needed when Page breaks occur).
type Block struct {
	contents    *contentstream.ContentStreamOperations
	resources   *model.PdfPageResources
	positioning Positioning
	xPos, yPos  float64
	width       float64
	height      float64
	angle       float64
	margins     Margins
	annotations []*model.PdfAnnotation
	artifact    *model.Artifact
	background  *Background
}

func parseEllipseTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseEllipse(node)
}

func (i *Invoice) newCell(value string, invoiceCellProps InvoiceCellProps) *InvoiceCell {
	return &InvoiceCell{invoiceCellProps, value}
}

func (im *Image) applyMask(xObjectImage *model.XObjectImage, img2 *model.Image) error {
	switch im.mask.kind {
	case imageMaskSoft:
		grayMaskXObject, err := makeGrayMaskXObject(im.mask.img)
		if err != nil {
			return err
		}
		xObjectImage.SMask = grayMaskXObject.ToPdfObject()
	case imageMaskStencil, imageMaskOptimizedAlpha:
		img := img2
		var data []uint8
		if im.mask.kind == imageMaskStencil {
			img = im.mask.img
			if im.mask.threshold != nil {
				data = []uint8{*im.mask.threshold}
			}
		}
		xObjectImageStencilMask, err := model.NewXObjectImageStencilMask(img, nil, data...)
		if err != nil {
			return err
		}
		xObjectImage.Mask = xObjectImageStencilMask.ToPdfObject()
	case imageMaskColorKey:
		if err := validateColorKeyMask(im.mask.colorKey, xObjectImage); err != nil {
			return err
		}
		xObjectImage.Mask = core.MakeArrayFromIntegers(im.mask.colorKey)
	}
	return nil
}

// VectorDrawable is a Drawable with a specified width and height.
type VectorDrawable interface {
	Drawable

	// Width returns the width of the Drawable.
	Width() float64

	// Height returns the height of the Drawable.
	Height() float64
}

const (
	TextRenderingModeFill TextRenderingMode = iota
	TextRenderingModeStroke
	TextRenderingModeFillStroke
	TextRenderingModeInvisible
	TextRenderingModeFillClip
	TextRenderingModeStrokeClip
	TextRenderingModeFillStrokeClip
	TextRenderingModeClip
)

const (
	DefaultHorizontalScaling = 100
)
