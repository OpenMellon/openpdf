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
	"slices"
	"strings"
	"unicode"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

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

func parseRectangleTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseRectangle(node)
}

// SetPositioning sets the positioning of the ellipse (absolute or relative).
func (e *Ellipse) SetPositioning(position Positioning) { e.positioning = position }

func (tp *templateProcessor) parseEllipse(node *templateNode) (any, error) {
	ellipse := tp.creator.NewEllipse(0, 0, 0, 0)
	if err := tp.applyAttrs(node, ellipse, ellipseAttrs); err != nil {
		return nil, err
	}
	return ellipse, nil
}

// SetPos sets the position of the graphic svg to the specified coordinates.
// This method sets the graphic svg to use absolute positioning.
func (gs *GraphicSVG) SetPos(x, y float64) {
	gs.positioning = PositionAbsolute
	gs.xPos = x
	gs.yPos = y
}

// BorderColor returns the border color of the rectangle.
func (r *Rectangle) BorderColor() Color { return r.borderColor }

// SetBorderColor sets the cell's border color.
func (gc *GridCell) SetBorderColor(col Color) {
	gc.borderColorLeft = col
	gc.borderColorBottom = col
	gc.borderColorRight = col
	gc.borderColorTop = col
}

// Width returns the width of the line.
// NOTE: Depending on the fit mode the line is set to use, its width may be
// calculated at runtime (e.g. when using FitModeFillWidth).
func (l *Line) Width() float64 { return math.Abs(l.x2 - l.x1) }

// SetOutlineTree adds the specified outline tree to the PDF file generated
// by the creator. Adding an external outline tree disables the automatic
// generation of outlines done by the creator for the relevant components.
func (c *Creator) SetOutlineTree(outlineTree *model.PdfOutlineTreeNode) {
	c.outlineTree = outlineTree
}

// Wrap wraps the text of the chunk into lines based on its style and the
// specified width.
func (tec *TextChunk) Wrap(width float64) ([]string, error) {
	if int(width) <= 0 {
		return []string{tec.Text}, nil
	}
	var strs []string
	var rs []rune
	var value float64
	var floats []float64
	style := tec.Style
	isTextWriteDirectionLTR := core.IsTextWriteDirectionLTR(tec.Text)
	for _, r := range tec.Text {
		if r == '\u000A' {
			formatWriteDirectionLTR := core.FormatWriteDirectionLTR(string(rs), isTextWriteDirectionLTR)
			strs = append(strs, strings.TrimRightFunc(formatWriteDirectionLTR, unicode.IsSpace)+string(r))
			rs = nil
			value = 0
			floats = nil
			continue
		}
		ok2 := r == ' '
		runeMetrics, ok := style.Font.GetRuneMetrics(r)
		if !ok {
			common.Log.Debug("ERROR: Rune char metrics not found! rune=0x%04x=%c font=%s %#q", r, r, style.Font.BaseFont(), style.Font.Subtype())
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Font: %#v", style.Font)
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Encoder: %#v", style.Font.Encoder())
			}
			return nil, errors.New("glyph char metrics missing")
		}
		fontSize := style.FontSize * runeMetrics.Wx
		fontSize2 := fontSize
		if !ok2 {
			fontSize2 = fontSize + style.CharSpacing*1000.0
		}
		if value+fontSize > width*1000.0 {
			n := -1
			if !ok2 {
				for i, r := range slices.Backward(rs) {
					if r == ' ' {
						n = i
						break
					}
				}
			}
			formatWriteDirectionLTR := string(rs)
			if n > 0 {
				formatWriteDirectionLTR = string(rs[0 : n+1])
				rs = append(rs[n+1:], r)
				floats = append(floats[n+1:], fontSize2)
				value = 0
				for _, float := range floats {
					value += float
				}
			} else {
				if ok2 {
					rs = []rune{}
					floats = []float64{}
					value = 0
				} else {
					rs = []rune{r}
					floats = []float64{fontSize2}
					value = fontSize2
				}
			}
			formatWriteDirectionLTR = core.FormatWriteDirectionLTR(formatWriteDirectionLTR, isTextWriteDirectionLTR)
			strs = append(strs, strings.TrimRightFunc(formatWriteDirectionLTR, unicode.IsSpace))
		} else {
			rs = append(rs, r)
			value += fontSize2
			floats = append(floats, fontSize2)
		}
	}
	if len(rs) > 0 {
		formatWriteDirectionLTR := string(rs)
		formatWriteDirectionLTR = core.FormatWriteDirectionLTR(formatWriteDirectionLTR, isTextWriteDirectionLTR)
		strs = append(strs, formatWriteDirectionLTR)
	}
	return strs, nil
}

// SetBorderWidth sets the border width of the ellipse.
func (e *Ellipse) SetBorderWidth(bw float64) { e.borderWidth = bw }

// FitMode returns the fit mode of the image.
func (im *Image) FitMode() FitMode    { return im.fitMode }
func isRegionalIndicator(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

// AddPatternResource adds pattern dictionary inside the resources dictionary.
func (ls *LinearShading) AddPatternResource(block *Block) (name core.PdfObjectName, err error) {
	return addShadingPatternResource(block, ls.ToPdfShadingPattern().ToPdfObject())
}

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

func (ch *Chapter) chapterInfo() ChapterInfo {
	return ChapterInfo{Number: strings.TrimSuffix(ch.headingNumber(), "."), Title: ch.title, Level: ch.level}
}

// NewLine creates a new line between (x1, y1) to (x2, y2),
// using default attributes.
// NOTE: In relative positioning mode, `x1` and `y1` are calculated using the
// current context and `x2`, `y2` are used only to calculate the position of
// the second point in relation to the first one (used just as a measurement
// of size). Furthermore, when the fit mode is set to fill the context width,
// `x2` is set to the right edge coordinate of the context.
func (c *Creator) NewLine(x1, y1, x2, y2 float64) *Line { return newLine(x1, y1, x2, y2) }

func firstLastTextChild(vals []any) (int, int) {
	n, n2 := -1, -1
	for i, v := range vals {
		if hasTextChunk(v) {
			if n < 0 {
				n = i
			}
			n2 = i
		}
	}
	return n, n2
}

// ToRGB implements interface Color.
// Note: It's not directly used since shading color works differently than regular color.
func (ls *LinearShading) ToRGB() (float64, float64, float64) { return 0, 0, 0 }

func (pt *pageTransformations) transformBlock(block *Block) {
	if pt.matrix != nil {
		block.transform(*pt.matrix)
	}
}

// AppendCurve appends a Bezier curve to the filled curve.
func (fc *FilledCurve) AppendCurve(curve contentdraw.CubicBezierCurve) *FilledCurve {
	fc.curves = append(fc.curves, curve)
	return fc
}

func (tp *templateProcessor) parseLinearGradientAttr(creator *Creator, s2 string) (Color, error) {
	linearGradientAttr := ColorBlack
	if s2 == "" {
		return linearGradientAttr, nil
	}
	linearGradientColor := creator.NewLinearGradientColor([]*ColorPoint{})
	linearGradientColor.SetExtends(true, true)
	var (
		strs = strings.Split(s2[16:len(s2)-1], ",")
		s    = strings.TrimSpace(strs[0])
		err  error
	)
	if strings.HasSuffix(s, "deg") {
		finiteFloat, err2 := parseFiniteFloat(s[:len(s)-3])
		if err2 != nil {
			common.Log.Debug("Failed parsing gradient angle: %v", err2)
			setFirstError(&err, fmt.Errorf("invalid gradient angle %q", s))
		} else {
			linearGradientColor.SetAngle(finiteFloat)
		}
		strs = strs[1:]
	}
	colors, positions, err2 := tp.processGradientColorPair(strs)
	setFirstError(&err, err2)
	if colors == nil || positions == nil {
		return linearGradientAttr, err
	}
	if len(colors) == 1 {
		return colors[0], err
	}
	for i := 0; i < len(colors); i++ {
		linearGradientColor.AddColorStop(colors[i], positions[i])
	}
	return linearGradientColor, err
}

// SetText replaces all the text of the paragraph with the specified one.
func (sp *StyledParagraph) SetText(text string) *TextChunk {
	sp.Reset()
	return sp.Append(text)
}

// This method is not supported by PageBreak component and exists solely to satisfy the Drawable interface.
func (pb *PageBreak) GenerateKDict() (*model.KDict, error) { return nil, nil }
func newPageBreak() *PageBreak                             { return &PageBreak{} }

// SetBorderOpacity sets the border opacity of the ellipse.
func (e *Ellipse) SetBorderOpacity(opacity float64) { e.borderOpacity = opacity }
