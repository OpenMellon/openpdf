package creator

import (
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func drawImageBlock(block *Block, img *Image, imageBlock DrawContext) (DrawContext, error) {
	imageBlock2 := imageBlock
	n := 1
	keyName := core.PdfObjectName(fmt.Sprintf("Img%d", n))
	for block.resources.HasXObjectByName(keyName) {
		n++
		keyName = core.PdfObjectName(fmt.Sprintf("Img%d", n))
	}
	err := block.resources.SetXObjectImageByNameLazy(keyName, img.xobj, img.lazy)
	if err != nil {
		return imageBlock, err
	}
	n2 := 0
	gsName := core.PdfObjectName(fmt.Sprintf("GS%d", n2))
	for block.resources.HasExtGState(gsName) {
		n2++
		gsName = core.PdfObjectName(fmt.Sprintf("GS%d", n2))
	}
	dict := core.MakeDict()
	dict.Set("BM", core.MakeName("Normal"))
	if img.opacity < 1.0 {
		dict.Set("CA", core.MakeFloat(img.opacity))
		dict.Set("ca", core.MakeFloat(img.opacity))
	}
	err = block.resources.AddExtGState(gsName, core.MakeIndirectObject(dict))
	if err != nil {
		return imageBlock, err
	}
	width := img.Width()
	height := img.Height()
	_, y := img.rotatedSize()
	x := imageBlock.X
	ty := imageBlock.PageHeight - imageBlock.Y - height
	if img.positioning.IsRelative() {
		ty -= (y - height) / 2
		switch img.horizontalAlignment {
		case HorizontalAlignmentCenter:
			x += (imageBlock.Width - width) / 2
		case HorizontalAlignmentRight:
			x = imageBlock.PageWidth - imageBlock.Margins.Right - img.margins.Right - width
		}
	}
	angle := img.angle
	if img.artifact == nil && img.tagInfo != nil && img.tagInfo.ComponentKObj != nil {
		width2, height2 := img.rotatedSize()
		x2 := x + width/2
		ty2 := ty + height/2
		img.tagInfo.ComponentKObj.SetBoundingBox(x2-width2/2, ty2-height2/2, width2, height2)
	}
	contentCreator := contentstream.NewContentCreator()
	if img.artifact == nil {
		if img.tagInfo != nil {
			contentCreator.Add_BDC(*core.MakeName(string(img.tagInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(img.tagInfo.Mcid)})
		}
	} else {
		m := img.artifact.GenerateMap()
		if len(m) > 0 {
			contentCreator.Add_BDC(*core.MakeName(string("Artifact")), m)
		} else {
			contentCreator.Add_BMC(*core.MakeName(string("Artifact")))
		}
	}
	contentCreator.Add_gs(gsName)
	contentCreator.Translate(x, ty)
	if angle != 0 {
		contentCreator.Translate(width/2, height/2)
		contentCreator.RotateDeg(angle)
		contentCreator.Translate(-width/2, -height/2)
	}
	contentCreator.Scale(width, height).Add_Do(keyName)
	if img.tagInfo != nil || img.artifact != nil {
		contentCreator.Add_EMC()
	}
	operations := contentCreator.Operations()
	operations.WrapIfNeeded()
	block.addWrappedContents(operations)
	if img.positioning.IsRelative() {
		imageBlock.Y += y
		imageBlock.Height -= y
		return imageBlock, nil
	}
	return imageBlock2, nil
}

func drawColorGlyph(ctx DrawContext, add_q *contentstream.ContentCreator, block *Block, text string, x float64, style *TextStyle, propertyList map[string]core.PdfObject, s model.StructureType) (bool, error) {
	if text == "" {
		return false, nil
	}
	r, _ := utf8.DecodeRuneInString(text)
	if !isEmoji(r) && !isKeycapSequence(text) {
		return false, nil
	}
	var colorGlyphInfo *model.ColorGlyphInfo
	if style.MultiFont != nil {
		style.MultiFont.Reset()
		colorGlyphInfo = style.MultiFont.GetColorGlyphInfoByText(text)
	} else if otfType := style.Font.GetOTFType(); otfType != nil {
		colorGlyphInfo = model.GetColorGlyphInfoByText(otfType, text)
	}
	if colorGlyphInfo != nil {
		if colorGlyphInfo.GlyphType == model.ColorGlyphTypeSVG && len(colorGlyphInfo.SVGData) > 0 {
			svgData := string(colorGlyphInfo.SVGData)
			graphicSVG, err := newGraphicSVGFromString(svgData)
			if err != nil {
				return false, err
			}
			graphicSVG.ScaleToHeight(style.FontSize)
			graphicSVG.SetPos(x, ctx.Y)
			if len(propertyList) > 0 {
				add_q.Add_EMC()
			}
			add_q = add_q.Add_ET().Add_q()
			pageBlocks, ctx2, err := graphicSVG.GeneratePageBlocks(ctx)
			if err != nil {
				return false, err
			}
			if len(pageBlocks) != 1 {
				return false, fmt.Errorf("expected 1 block from emoji image, got %d", len(pageBlocks))
			}
			block.mergeBlocks(pageBlocks[0])
			width := graphicSVG.Width()
			add_q.Add_Q().Add_BT().Translate(width, 0)
			ctx2.X += width
			if len(propertyList) > 0 {
				add_q.Add_BDC(*core.MakeName(string(s)), propertyList)
			}
			return true, nil
		} else if colorGlyphInfo.GlyphType == model.ColorGlyphTypeBitmap && len(colorGlyphInfo.BitmapData) > 0 {
			image, err := newImageFromBytes(colorGlyphInfo.BitmapData)
			if err != nil {
				return false, err
			}
			image.SetEncoder(core.NewDCTEncoder())
			if colorGlyphInfo.GetColorGlyphFormat() == "PNG" {
				image.SetEncoder(core.NewFlateEncoder())
			}
			fontSize := style.FontSize / float64(colorGlyphInfo.BitmapHeight)
			w := float64(colorGlyphInfo.BitmapWidth) * fontSize
			h := float64(colorGlyphInfo.BitmapHeight) * fontSize
			image.SetWidth(w)
			image.SetHeight(h)
			image.SetPos(x, ctx.Y)
			if len(propertyList) > 0 {
				add_q.Add_EMC()
			}
			add_q = add_q.Add_ET().Add_q()
			pageBlocks, ctx2, err := image.GeneratePageBlocks(ctx)
			if err != nil {
				return false, err
			}
			if len(pageBlocks) != 1 {
				return false, fmt.Errorf("expected 1 block from emoji image, got %d", len(pageBlocks))
			}
			block.mergeBlocks(pageBlocks[0])
			add_q.Add_Q().Add_BT().Translate(w, 0)
			ctx2.X += w
			if len(propertyList) > 0 {
				add_q.Add_BDC(*core.MakeName(string(s)), propertyList)
			}
			return true, nil
		}
	}
	return false, nil
}

func drawParagraphChunks(block *Block, para *StyledParagraph, rows4 [][]*TextChunk, ctx DrawContext) (DrawContext, [][]*TextChunk, error) {
	d := &paragraphDrawer{
		block:      block,
		para:       para,
		ctx:        ctx,
		lines:      rows4,
		fontSize:   para.defaultStyle.FontSize,
		isRelative: para.positioning.IsRelative(),
		colors:     map[Color]model.PdfColor{},
		colorSpans: map[Color]*[2]float64{},
	}
	return d.draw()
}

func drawTemplate(creator *Creator, r io.Reader, data any, options *TemplateOptions, renderer componentRenderer) error {
	if creator == nil {
		common.Log.Error("Creator instance cannot be nil.")
		return errInvalidCreator
	}
	template, err := ParseTemplate(r, options)
	if err != nil {
		return err
	}
	return template.renderInto(creator, renderer, data)
}

// DrawContext defines the drawing context. The DrawContext is continuously used and updated when
// drawing the page contents in relative mode.  Keeps track of current X, Y position, available
// height as well as other page parameters such as margins and dimensions.
type DrawContext struct {
	// Current page number.
	Page int

	// Current position.  In a relative positioning mode, a drawable will be placed at these coordinates.
	X, Y float64

	// Context dimensions.  Available width and height (on current page).
	Width, Height float64

	// Page Margins.
	Margins Margins

	// Absolute Page size, widths and height.
	PageWidth  float64
	PageHeight float64

	// Controls whether the components are stacked horizontally
	Inline          bool
	unsupportedRune rune
	errs            []error
}

func (dc DrawContext) contentHeight() float64 {
	return dc.PageHeight - dc.Margins.Top - dc.Margins.Bottom
}
