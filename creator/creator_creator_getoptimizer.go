// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"unicode"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// GetOptimizer returns current PDF optimizer.
func (c *Creator) GetOptimizer() model.Optimizer { return c.optimizer }

func roundTo(value float64, multiple float64) float64 { return math.Round(value/multiple) * multiple }

// GeneratePageBlocks generate the Page blocks. Draws the Image on a block, implementing the Drawable interface.
func (im *Image) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	if im.xobj == nil {
		if err := im.makeXObject(); err != nil {
			return nil, ctx, err
		}
	}
	var blocks []*Block
	ctx2 := ctx
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	if im.positioning.IsRelative() {
		im.applyFitMode(ctx.Width)
		ctx.X += im.margins.Left
		ctx.Y += im.margins.Top
		ctx.Width -= im.margins.Left + im.margins.Right
		ctx.Height -= im.margins.Top + im.margins.Bottom
		if im.height > ctx.Height {
			blocks = append(blocks, block)
			block = NewBlock(ctx.PageWidth, ctx.PageHeight)
			ctx.Page++
			ctx2 := ctx
			ctx2.Y = ctx.Margins.Top + im.margins.Top
			ctx2.X = ctx.Margins.Left + im.margins.Left
			ctx2.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom - im.margins.Top - im.margins.Bottom
			ctx2.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right - im.margins.Left - im.margins.Right
			ctx = ctx2
		}
	} else {
		ctx.X = im.xPos
		ctx.Y = im.yPos
	}
	ctx, err := drawImageBlock(block, im, ctx)
	if err != nil {
		return nil, ctx, err
	}
	blocks = append(blocks, block)
	if im.positioning.IsAbsolute() {
		ctx = ctx2
	} else {
		ctx.X = ctx2.X
		ctx.Width = ctx2.Width
		ctx.Y += im.margins.Bottom
	}
	return blocks, ctx, nil
}

// CurCol returns the currently active cell's column number.
func (t *Table) CurCol() int { curCol := (t.curCell-1)%t.cols + 1; return curCol }

func (tp *templateProcessor) checkTopLevelText(templateText2 *templateText) error {
	if isBlank(templateText2.text) {
		return nil
	}
	if tp.options.Strict {
		return tp.nodeError(templateText2.pos(), "text content is not valid outside a tag")
	}
	tp.nodeLogDebug(templateText2.pos(), "Text content outside a tag. Skipping.")
	return nil
}

// Write output of creator to io.Writer interface.
func (c *Creator) Write(ws io.Writer) error {
	if err := c.Finalize(); err != nil {
		return err
	}
	name := ""
	if file, ok := ws.(*os.File); ok {
		name = file.Name()
	}
	pdfWriter := model.NewPdfWriter()
	pdfWriter.SetOptimizer(c.optimizer)
	pdfWriter.SetFileName(name)
	if c.acroForm != nil {
		err := pdfWriter.SetForms(c.acroForm)
		if err != nil {
			common.Log.Debug("Failure: %v", err)
			return err
		}
	}
	if c.outlineTree != nil {
		pdfWriter.AddOutlineTree(c.outlineTree)
	} else if c.outline != nil && c.AddOutlines {
		pdfWriter.AddOutlineTree(&c.outline.ToPdfOutline().PdfOutlineTreeNode)
	}
	if c.pageLabels != nil {
		if err := pdfWriter.SetPageLabels(c.pageLabels); err != nil {
			common.Log.Debug("ERROR: Could not set page labels: %v", err)
			return err
		}
	}
	if c.subsetFonts != nil {
		for _, subsetFont := range c.subsetFonts {
			err := subsetFont.SubsetRegistered()
			if err != nil {
				common.Log.Debug("ERROR: Could not subset font: %v", err)
				return err
			}
		}
	}
	if c.taggingEnabled && c.structTreeRoot != nil {
		pdfWriter.SetCatalogMarkInfo(core.MakeDictMap(map[string]core.PdfObject{"Marked": core.MakeBool(true)}))
	}
	if c.pdfWriterAccessFunc != nil {
		err := c.pdfWriterAccessFunc(&pdfWriter)
		if err != nil {
			common.Log.Debug("Failure: %v", err)
			return err
		}
	}
	m := make(map[int64]*core.PdfIndirectObject)
	for i, page := range c.pages {
		err := pdfWriter.AddPage(page)
		if err != nil {
			common.Log.Error("Failed to add Page: %v", err)
			return err
		}
		if c.structTreeRoot != nil {
			if c.taggingEnabled {
				page.SetStructParentsKey(i)
			}
			pageIndirectObject, err := pdfWriter.GetPageIndirectObject(i)
			if err != nil {
				common.Log.Debug("ERROR: Could not get page indirect object %v", err)
			} else {
				m[int64(i+1)] = pageIndirectObject
			}
		}
	}
	if c.structTreeRoot != nil {
		if c.structNamespace != nil && !slices.Contains(c.structTreeRoot.Namespaces, c.structNamespace) {
			c.structTreeRoot.AddNamespace(c.structNamespace)
		}
		var fn func(kDict *model.KDict, n int64)
		fn = func(kDict *model.KDict, n int64) {
			if kDict == nil {
				return
			}
			n2 := n
			if pageNumber := kDict.GetPageNumber(); pageNumber > 0 {
				n2 = pageNumber
			}
			if n2 > 0 {
				if page, ok := m[n2]; ok {
					kDict.SetPage(page)
				}
			}
			if c.structNamespace != nil {
				kDict.SetNamespace(c.structNamespace)
			}
			for _, kValue := range kDict.GetChildren() {
				if kDict := kValue.GetKDict(); kDict != nil {
					fn(kDict, n2)
				}
			}
		}
		for _, kDict := range c.structTreeRoot.K {
			fn(kDict, 0)
		}
		if err := pdfWriter.SetCatalogStructTreeRoot(c.structTreeRoot.ToPdfObject()); err != nil {
			common.Log.Debug("ERROR: Could not set StructTreeRoot: %v", err)
			return err
		}
		if c.structNamespace != nil {
			pdfWriter.SetVersion(2, 0)
		}
	}
	if c.viewerPreferences != nil {
		if err := pdfWriter.SetCatalogViewerPreferences(c.viewerPreferences.ToPdfObject()); err != nil {
			common.Log.Debug("ERROR: Could not set ViewerPreferences: %v", err)
			return err
		}
	}
	if c.language != "" {
		if err := pdfWriter.SetCatalogLanguage(core.MakeString(c.language)); err != nil {
			common.Log.Debug("ERROR: Could not set catalog language: %v", err)
			return err
		}
	}
	err := pdfWriter.Write(ws)
	if err != nil {
		return err
	}
	return nil
}

// ConvertToBinary converts current image data into binary (Bi-level image) format.
// If provided image is RGB or GrayScale the function converts it into binary image
// using histogram auto threshold method.
func (im *Image) ConvertToBinary() error { return im.img.ConvertToBinary() }

// GetAngle returns the rotation angle of the text, in degrees.
func (sp *StyledParagraph) GetAngle() float64 { return sp.angle }

func linkTextStyle(font *model.PdfFont) TextStyle {
	return TextStyle{Color: ColorRGBFrom8bit(0, 0, 238), Font: font, FontSize: 10, OutlineSize: 1, HorizontalScaling: DefaultHorizontalScaling, UnderlineStyle: TextDecorationLineStyle{Offset: 1, Thickness: 1}}
}

const (
	CellVerticalAlignmentTop CellVerticalAlignment = iota
	CellVerticalAlignmentMiddle
	CellVerticalAlignmentBottom
)

// SetNotes sets the notes section of the invoice.
func (i *Invoice) SetNotes(title, content string) { i.notes = [2]string{title, content} }

const whitespaceChars = " \t\r\n"

func newImage(img *model.Image) (*Image, error) {
	width := float64(img.Width)
	height := float64(img.Height)
	return &Image{img: img, origWidth: width, origHeight: height, width: width, height: height, angle: 0, opacity: 1.0, positioning: PositionRelative, taggedDrawable: taggedDrawable{structType: model.StructureTypeFigure}}, nil
}

// NewTextStyle creates a new text style object which can be used to style
// chunks of text.
// Default attributes:
// Font: the creator default font (Helvetica, unless changed via SetDefaultFont)
// Font size: 10
// Encoding: the font's encoding (WinAnsiEncoding for the standard fonts)
// Text color: black
func (c *Creator) NewTextStyle() TextStyle { return defaultTextStyle(c.fontRegular) }
func newListItem() *listItem               { return &listItem{} }
func (sp *StyledParagraph) getTextWidth() float64 {
	var charSpacing float64
	chunksLen := len(sp.chunks)
	for i, chunk := range sp.chunks {
		style := &chunk.Style
		textLen := len(chunk.Text)
		for j, r := range chunk.Text {
			if r == '\u000A' {
				continue
			}
			runeMetrics, ok := style.Font.GetRuneMetrics(r)
			if !ok {
				common.Log.Debug("Rune char metrics not found! %v\n", r)
				return -1
			}
			charSpacing += style.FontSize * runeMetrics.Wx * style.horizontalScale()
			if r != ' ' && (i != chunksLen-1 || j != textLen-1) {
				charSpacing += style.CharSpacing * 1000.0
			}
		}
	}
	return charSpacing
}

// LineWidth returns the width of the line.
func (l *Line) LineWidth() float64 { return l.lineWidth }

// NewGrid creates a new Grid with a specified number of columns.
func (c *Creator) NewGrid(cols int) *Grid { return newGrid(cols) }

// FooterFunctionArgs holds the input arguments to a footer drawing function.
// It is designed as a struct, so additional parameters can be added in the future with backwards
// compatibility.
type FooterFunctionArgs struct {
	PageNum    int
	TotalPages int

	// Chapter describes the chapter the current page belongs to, resolved via
	// Creator.ChapterForPage. It is nil on pages that precede the first chapter.
	Chapter *ChapterInfo
}

func parseSVGPoints(s string) ([]float64, error) {
	n := -1
	var svgPoints []float64
	r2 := ' '
	for i, r := range s {
		if !unicode.IsNumber(r) && r != '.' && !(r == '-' && r2 == 'e') && r != 'e' {
			if n != -1 {
				numbers, err := parseNumberList(s[n:i])
				if err != nil {
					return svgPoints, err
				}
				svgPoints = append(svgPoints, numbers...)
			}
			if r == '-' {
				n = i
			} else {
				n = -1
			}
		} else if n == -1 {
			n = i
		}
		r2 = r
	}
	if n != -1 && n != len(s) {
		numbers, err := parseNumberList(s[n:])
		if err != nil {
			return svgPoints, err
		}
		svgPoints = append(svgPoints, numbers...)
	}
	return svgPoints, nil
}

func normalizeTemplateNumber(v any) (any, error) {
	switch n := v.(type) {
	case uint8:
		return int64(n), nil
	case int8:
		return int64(n), nil
	case uint16:
		return int64(n), nil
	case int16:
		return int64(n), nil
	case uint32:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case uint64:
		return int64(n), nil
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	case float32:
		return float64(n), nil
	case float64:
		return n, nil
	}
	return nil, fmt.Errorf("invalid value, %v is not a number", v)
}

// Append adds a new text chunk to the paragraph.
func (sp *StyledParagraph) Append(text string) *TextChunk {
	textChunk := NewTextChunk(text, sp.defaultStyle)
	return sp.appendChunk(textChunk)
}
