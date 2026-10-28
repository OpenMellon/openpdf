// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"image"
	"reflect"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

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

// SetFillColor sets the fill color for the path.
func (fc *FilledCurve) SetFillColor(color Color) { fc.fillColor = color }
func (co *Command) isAbsolute() bool             { return co.Symbol == strings.ToUpper(co.Symbol) }

// GeneratePageBlocks generate the Page blocks. Multiple blocks are generated
// if the contents wrap over multiple pages.
func (tl *TOCLine) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	ctx2 := ctx
	pageBlocks, ctx, err := tl.paragraph.GeneratePageBlocks(ctx)
	if err != nil {
		return pageBlocks, ctx, err
	}
	if tl.positioning.IsRelative() {
		ctx.X = ctx2.X
	}
	if tl.positioning.IsAbsolute() {
		return pageBlocks, ctx2, nil
	}
	return pageBlocks, ctx, nil
}

func floatArrayAttr(i any) attrApplyFunc {
	value2 := validateSetter("floatArrayAttr", i, 0, float64SliceType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		floatArray, err := parseFloatArray(value)
		if err != nil {
			if strictError := proc.strictErr(node, attrName, value, err); strictError != nil {
				return strictError
			}
		}
		callSetter(value2, target, reflect.ValueOf(floatArray))
		return nil
	}
}

// GetArtifact returns the artifact associated with the image, if any.
func (im *Image) GetArtifact() *model.Artifact { return im.artifact }

// NewGraphicSVGFromString creates a graphic SVG from a SVG string.
func NewGraphicSVGFromString(svgStr string) (*GraphicSVG, error) {
	return newGraphicSVGFromString(svgStr)
}

// SetFillColor sets the fill color of the ellipse.
func (e *Ellipse) SetFillColor(col Color) { e.fillColor = col }

// EnableFontSubsetting enables font subsetting for `font` when the creator output is written to file.
// Embeds only the subset of the runes/glyphs that are actually used to display the file.
// Subsetting can reduce the size of fonts significantly.
func (c *Creator) EnableFontSubsetting(font *model.PdfFont) {
	c.subsetFonts = append(c.subsetFonts, font)
}

func (pat *ParsedTemplate) renderInto(creator *Creator, renderer componentRenderer, data any) error {
	if creator == nil {
		common.Log.Error("Creator instance cannot be nil.")
		return errInvalidCreator
	}
	data2, err := pat.execute(data)
	if err != nil {
		return err
	}
	return newTemplateProcessor(creator, pat.source, data2, pat.options, renderer).render()
}

// TextDecorationLineStyle represents the style of lines used to decorate
// a text chunk (e.g. underline).
type TextDecorationLineStyle struct {
	// Color represents the color of the line (default: the color of the text).
	Color Color

	// Offset represents the vertical offset of the line (default: 1).
	Offset float64

	// Thickness represents the thickness of the line (default: 1).
	Thickness float64
}

// CreateFrontPage sets a function to generate a front Page.
//
// Chapters drawn inside the callback (via NewChapter + Draw) are not tracked
// by Creator.ChapterForPage and will not appear in header/footer callback
// args. The front page is expected to hold cover content, not chapter bodies.
func (c *Creator) CreateFrontPage(genFrontPageFunc func(frontpageFunctionArgs FrontpageFunctionArgs)) {
	c.genFrontPageFunc = genFrontPageFunc
}

// SetBorderColor sets the border color.
func (cp *CurvePolygon) SetBorderColor(color Color) { cp.polygon.BorderColor = toPdfColor(color) }

func newImageFromGoImage(img image.Image) (*Image, error) {
	image, err := model.ImageHandling.NewImageFromGoImage(img)
	if err != nil {
		return nil, err
	}
	return newImage(image)
}
func isSkinToneModifier(r rune) bool { return r >= 0x1F3FB && r <= 0x1F3FF }

// NewBlockFromPage creates a Block from a PDF Page.  Useful for loading template pages as blocks
// from a PDF document and additional content with the creator.
func NewBlockFromPage(page *model.PdfPage) (*Block, error) {
	block := &Block{}
	allContentStreams, err := page.GetAllContentStreams()
	if err != nil {
		return nil, err
	}
	contentStreamParser := contentstream.NewContentStreamParser(allContentStreams)
	contents, err := contentStreamParser.Parse()
	if err != nil {
		return nil, err
	}
	contents.WrapIfNeeded()
	block.contents = contents
	if page.Resources != nil {
		block.resources = page.Resources
	} else {
		block.resources = model.NewPdfPageResources()
	}
	mediaBox, err := page.GetMediaBox()
	if err != nil {
		return nil, err
	}
	if mediaBox.Llx != 0 || mediaBox.Lly != 0 {
		block.translate(-mediaBox.Llx, mediaBox.Lly)
	}
	block.width = mediaBox.Urx - mediaBox.Llx
	block.height = mediaBox.Ury - mediaBox.Lly
	if page.Rotate != nil {
		block.angle = -float64(*page.Rotate)
	}
	return block, nil
}

type token struct {
	text      string
	isCommand bool
}

// SetBorderOpacity sets the border opacity.
func (pbc *PolyBezierCurve) SetBorderOpacity(opacity float64) { pbc.borderOpacity = opacity }
