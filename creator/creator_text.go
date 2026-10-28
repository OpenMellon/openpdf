package creator

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// StyledParagraph represents text drawn with a specified font and can wrap across lines and pages.
// By default occupies the available width in the drawing context.
type StyledParagraph struct {
	taggedDrawable
	chunks            []*TextChunk
	defaultStyle      TextStyle
	linkStyle         TextStyle
	alignment         TextAlignment
	verticalAlignment TextVerticalAlignment
	lineHeight        float64
	enableWrap        bool
	width             float64
	enableWordWrap    bool
	maxLines          int
	defaultWrap       bool
	textOverflow      TextOverflow
	angle             float64
	margins           Margins
	positioning       Positioning
	xPos              float64
	yPos              float64
	unusedA           float64
	unusedB           float64
	lines             [][]*TextChunk
	beforeRender      func(para *StyledParagraph, ctx DrawContext)
	languageID        string
	artifact          *model.Artifact
	enableShaping     bool
	dropCaps          *DropCapsOptions
	dropCapsEnabled   bool
}

// SetDropCapsFirstWord is a convenience method to set traditional drop caps
// on the first word of the paragraph.
//
// Example:
//
//	p := c.NewStyledParagraph()
//	p.SetText("CHAPTER ONE: It was the best of times...")
//	p.SetDropCapsFirstWord(3, 8.0) // "CHAPTER" spans 3 lines with 8pt gap
func (sp *StyledParagraph) SetDropCapsFirstWord(numLines int, gap float64) {
	sp.SetDropCaps(DropCapsOptions{Type: DropCapsDrop, Scope: DropCapsFirstWord, NumLines: numLines, Gap: gap})
}

func (sp *StyledParagraph) createAccessibleLinkChunk(text string, annotation *model.PdfAnnotation, linkTagOptions LinkTagOptions) (*TextChunk, *model.KDict, error) {
	var kDict2 *model.KDict
	textChunk := NewTextChunk(text, sp.linkStyle)
	textChunk.AddAnnotation(annotation)
	if linkTagOptions.MCID > 0 {
		textChunk.SetMarkedContentID(linkTagOptions.MCID)
		textChunk.SetStructureType(model.StructureTypeLink)
		if linkTagOptions.AltText != "" && linkTagOptions.AltText != text {
			textChunk.SetAltText(linkTagOptions.AltText)
		}
		textChunk.AssociateAnnotationWithStructure(annotation)
		kDict, err := textChunk.GenerateKDict()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to generate KDict for link chunk: %v", err)
		}
		if linkTagOptions.AltText != "" && linkTagOptions.AltText != text {
			kDict.Alt = core.MakeString(linkTagOptions.AltText)
		}
		kDict2 = kDict
	}
	sp.appendChunk(textChunk)
	return textChunk, kDict2, nil
}

// SetTextAlignment sets the horizontal alignment of the text within the space provided.
func (sp *StyledParagraph) SetTextAlignment(align TextAlignment) { sp.alignment = align }

// RemoveArtifact removes the artifact associated with the paragraph, if any.
func (sp *StyledParagraph) RemoveArtifact() { sp.artifact = nil }

// GetDefaultLinkStyle returns the text style applied to link annotations
// appended to the paragraph.
func (sp *StyledParagraph) GetDefaultLinkStyle() TextStyle { return sp.linkStyle }

// AddInternalLinkWithTag adds a new internal link to the paragraph with proper tagging for accessibility.
// The text parameter represents the text that is displayed.
// The user is taken to the specified page, at the specified x and y coordinates.
// Position 0, 0 is at the top left of the page.
// The zoom of the destination page is controlled with the zoom parameter (0 keeps current zoom).
// The options parameter contains accessibility properties like tooltip, altText, and mcid.
func (sp *StyledParagraph) AddInternalLinkWithTag(text string, page int64, x, y, zoom float64, options LinkTagOptions) (*TextChunk, *model.KDict) {
	internalLinkAnnotation := newInternalLinkAnnotation(page-1, x, y, zoom, options.Tooltip)
	accessibleLinkChunk, kDict, err := sp.createAccessibleLinkChunk(text, internalLinkAnnotation, options)
	if err != nil {
		common.Log.Error("Failed to add internal link with tag: %v", err)
		return nil, nil
	}
	return accessibleLinkChunk, kDict
}

// Insert adds a new text chunk at the specified position in the paragraph.
func (sp *StyledParagraph) Insert(index uint, text string) *TextChunk {
	chunksLen := uint(len(sp.chunks))
	if index > chunksLen {
		index = chunksLen
	}
	textChunk := NewTextChunk(text, sp.defaultStyle)
	sp.chunks = append(sp.chunks[:index], append([]*TextChunk{textChunk}, sp.chunks[index:]...)...)
	sp.wrapText()
	return textChunk
}

func (sp *StyledParagraph) getLineMetrics(n int) (maxCapHeight, maxFontSize, minDescent float64) {
	if sp.lines == nil || (sp.lines != nil && len(sp.lines) == 0) {
		sp.wrapText()
	}
	if n < 0 || n > len(sp.lines)-1 {
		common.Log.Debug("ERROR: invalid paragraph line index %d. Returning 0, 0", n)
		return 0, 0, 0
	}
	return chunksFontMetrics(sp.lines[n])
}

// AddExternalLink adds a new external link to the paragraph.
// The text parameter represents the text that is displayed and the url
// parameter sets the destination of the link.
func (sp *StyledParagraph) AddExternalLink(text, url string) *TextChunk {
	s := url
	if strings.HasPrefix(url, "http") {
		s = text
	}
	textChunk := NewTextChunk(text, sp.linkStyle)
	textChunk.AddAnnotation(newURILinkAnnotation(url, s))
	return sp.appendChunk(textChunk)
}

// GetLines returns the text chunk lines produced after wrapping the paragraph
// to the available width. The returned slice references the paragraph's
// internal lines; modifying its elements modifies the paragraph.
func (sp *StyledParagraph) GetLines() [][]*TextChunk { return sp.lines }

// GetMaxLines returns the maximum number of lines drawn for the paragraph.
// A value of 0 means no limit.
func (sp *StyledParagraph) GetMaxLines() int { return sp.maxLines }

// SetAngle sets the rotation angle of the text.
func (sp *StyledParagraph) SetAngle(angle float64) { sp.angle = angle }

// GetDropCapsOptions returns the drop caps options set on the paragraph, if any.
func (sp *StyledParagraph) GetDropCapsOptions() *DropCapsOptions { return sp.dropCaps }

// SetEnableWrap sets the line wrapping enabled flag.
func (sp *StyledParagraph) SetEnableWrap(enableWrap bool) {
	sp.enableWrap = enableWrap
	sp.defaultWrap = false
}

// SetDropCapsFirstCharacter is a convenience method to set traditional drop caps
// on the first character of the paragraph.
//
// Example:
//
//	p := c.NewStyledParagraph()
//	p.SetText("This paragraph starts with a large T...")
//	p.SetDropCapsFirstCharacter(3, 5.0) // T spans 3 lines with 5pt gap
func (sp *StyledParagraph) SetDropCapsFirstCharacter(numLines int, gap float64) {
	sp.SetDropCaps(DropCapsOptions{Type: DropCapsDrop, Scope: DropCapsFirstCharacter, NumLines: numLines, Gap: gap})
}

// SetDropCaps sets drop caps options for the paragraph.
// The drop caps will be applied during text wrapping and rendering.
//
// Note: Drop caps are currently not supported for RTL (right-to-left) text.
// If applied to RTL text, the feature will be automatically disabled during rendering
// with a debug log notification.
//
// Example:
//
//	p := c.NewStyledParagraph()
//	p.SetText("This is a paragraph with a beautiful drop cap...")
//	p.SetDropCaps(creator.DropCapsOptions{
//	    Type:     creator.DropCapsDrop,
//	    Scope:    creator.DropCapsFirstCharacter,
//	    NumLines: 3,
//	    Gap:      5.0,
//	})
//
// Example with custom style:
//
//	p := c.NewStyledParagraph()
//	p.SetText("This is a paragraph with a styled drop cap...")
//	dropCapStyle := creator.TextStyle{
//	    Font:  boldFont,
//	    Color: creator.ColorRGBFrom8bit(200, 0, 0), // Red
//	}
//	p.SetDropCaps(creator.DropCapsOptions{
//	    Type:     creator.DropCapsDrop,
//	    Scope:    creator.DropCapsFirstCharacter,
//	    NumLines: 3,
//	    Gap:      5.0,
//	    Style:    &dropCapStyle,
//	})
func (sp *StyledParagraph) SetDropCaps(options DropCapsOptions) {
	if options.Type == DropCapsNone {
		sp.dropCaps = nil
		return
	}
	if options.NumLines <= 0 {
		options.NumLines = 3
	}
	if options.Gap <= 0 {
		options.Gap = 5.0
	}
	sp.dropCaps = &options
}

// SetLanguageIdentifier sets the language identifier for the paragraph.
func (sp *StyledParagraph) SetLanguageIdentifier(id string) {
	if sp.tagInfo == nil {
		sp.tagInfo = model.NewStructureTagInfo()
		sp.tagInfo.StructureType = model.StructureTypeParagraph
	}
	sp.languageID = id
}

// GetAngle returns the rotation angle of the text, in degrees.
func (sp *StyledParagraph) GetAngle() float64 { return sp.angle }

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

// Append adds a new text chunk to the paragraph.
func (sp *StyledParagraph) Append(text string) *TextChunk {
	textChunk := NewTextChunk(text, sp.defaultStyle)
	return sp.appendChunk(textChunk)
}

func newStyledParagraph(defaultStyle TextStyle) *StyledParagraph {
	return &StyledParagraph{chunks: []*TextChunk{}, defaultStyle: defaultStyle, linkStyle: linkTextStyle(defaultStyle.Font), lineHeight: 1.0, alignment: TextAlignmentLeft, enableWrap: true, defaultWrap: true, enableWordWrap: false, angle: 0, unusedA: 1, unusedB: 1, positioning: PositionRelative, languageID: "", artifact: nil, taggedDrawable: taggedDrawable{structType: model.StructureTypeParagraph}}
}

// Width returns the width of the Paragraph.
func (sp *StyledParagraph) Width() float64 {
	if sp.enableWrap && int(sp.width) > 0 {
		return sp.width
	}
	return sp.getTextWidth() / 1000.0
}

// GeneratePageBlocks generates the page blocks. Multiple blocks are generated
// if the contents wrap over multiple pages. Implements the Drawable interface.
func (sp *StyledParagraph) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	ctx2 := ctx
	var blocks []*Block
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	if sp.positioning.IsRelative() {
		ctx.X += sp.margins.Left
		ctx.Y += sp.margins.Top
		ctx.Width -= sp.margins.Left + sp.margins.Right
		ctx.Height -= sp.margins.Top
		sp.SetWidth(ctx.Width)
	} else {
		if int(sp.width) <= 0 {
			sp.SetWidth(sp.getTextWidth() / 1000.0)
		}
		ctx.X = sp.xPos
		ctx.Y = sp.yPos
	}
	if sp.beforeRender != nil {
		sp.beforeRender(sp, ctx)
	}
	if err := sp.wrapText(); err != nil {
		return nil, ctx, err
	}
	lines := sp.lines
	n := 0
	for {
		paragraphChunks, lines2, err := drawParagraphChunks(block, sp, lines, ctx)
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
			return nil, ctx, err
		}
		ctx = paragraphChunks
		blocks = append(blocks, block)
		if lines = lines2; len(lines2) == 0 {
			break
		}
		if len(lines2) == n {
			return nil, ctx, errors.New("not enough space for paragraph")
		}
		sp.dropCaps = nil
		block = NewBlock(ctx.PageWidth, ctx.PageHeight)
		ctx.Page++
		paragraphChunks = ctx
		paragraphChunks.Y = ctx.Margins.Top
		paragraphChunks.X = ctx.Margins.Left + sp.margins.Left
		paragraphChunks.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom
		paragraphChunks.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right - sp.margins.Left - sp.margins.Right
		ctx = paragraphChunks
		n = len(lines2)
	}
	if sp.positioning.IsRelative() {
		ctx.Y += sp.margins.Bottom
		ctx.Height -= sp.margins.Bottom
		if !ctx.Inline {
			ctx.X = ctx2.X
			ctx.Width = ctx2.Width
		}
		return blocks, ctx, nil
	}
	return blocks, ctx2, nil
}

// SetWidth sets the the Paragraph width. This is essentially the wrapping width,
// i.e. the width the text can extend to prior to wrapping over to next line.
func (sp *StyledParagraph) SetWidth(width float64) { sp.width = width; sp.wrapText() }

// Reset removes all the text chunks the paragraph contains.
func (sp *StyledParagraph) Reset() { sp.chunks = []*TextChunk{} }

// MarkAsArtifact marks the paragraph as an artifact of the specified type.
//
// This would override regular structure tagging and accessibility
// properties, and the paragraph will be treated as an artifact in the PDF.
func (sp *StyledParagraph) MarkAsArtifact(artifactType model.ArtifactType) *model.Artifact {
	sp.artifact = &model.Artifact{Type: artifactType}
	return sp.artifact
}

// SetFont sets the font for the paragraph.
func (sp *StyledParagraph) SetFont(font *model.PdfFont) {
	sp.defaultStyle.Font = font
	for _, chunk := range sp.chunks {
		chunk.Style.Font = font
	}
}

// EnableWordWrap sets the paragraph word wrap flag.
func (sp *StyledParagraph) EnableWordWrap(val bool) { sp.enableWordWrap = val }

func (sp *StyledParagraph) getTextHeight() float64 {
	var textHeight float64
	for _, chunk := range sp.chunks {
		fontSize := chunk.Style.FontSize * chunk.Style.effectiveLineHeight(sp.lineHeight)
		if fontSize > textHeight {
			textHeight = fontSize
		}
	}
	return textHeight
}

// GetDefaultStyle returns the default text style of the paragraph. This is the
// style applied to chunks appended without an explicit style.
func (sp *StyledParagraph) GetDefaultStyle() TextStyle { return sp.defaultStyle }

func (sp *StyledParagraph) splitToPieces(ctx DrawContext) ([]*StyledParagraph, error) {
	if err := sp.wrapChunks(false); err != nil {
		return nil, err
	}
	if !sp.positioning.IsRelative() || len(sp.lines) <= 1 {
		sp.lines = nil
		return []*StyledParagraph{sp}, nil
	}
	fn := func(chunks []*TextChunk, chunks2 []*TextChunk) []*TextChunk {
		if len(chunks2) == 0 {
			return chunks
		}
		chunksLen := len(chunks)
		if chunksLen == 0 {
			return append(chunks, chunks2...)
		}
		if chunks[chunksLen-1].Style == chunks2[0].Style {
			chunks[chunksLen-1].Text += chunks2[0].Text
		} else {
			chunks = append(chunks, chunks2[0])
		}
		return append(chunks, chunks2[1:]...)
	}
	fn2 := func(chunks []*TextChunk) *StyledParagraph {
		para := *sp
		para.chunks = chunks
		para.lines = nil
		return &para
	}
	var (
		paras  []*StyledParagraph
		chunks []*TextChunk
		value  float64
	)
	for _, line := range sp.lines {
		var value2 float64
		chunks2 := make([]*TextChunk, 0, len(line))
		for _, chunk := range line {
			if fontSize := chunk.Style.FontSize * chunk.Style.effectiveLineHeight(sp.lineHeight); fontSize > value2 {
				value2 = fontSize
			}
			chunks2 = append(chunks2, chunk.clone())
		}
		if value+value2 >= ctx.Height && len(chunks) > 0 {
			paras = append(paras, fn2(chunks))
			chunks = nil
			value = 0
		}
		chunks = fn(chunks, chunks2)
		value += value2
	}
	if len(chunks) > 0 {
		paras = append(paras, fn2(chunks))
	}
	sp.lines = nil
	return paras, nil
}

func (sp *StyledParagraph) wrapWordChunks() {
	if !sp.enableWordWrap {
		return
	}
	var (
		chunks []*TextChunk
		font   *model.PdfFont
	)
	for _, chunk := range sp.chunks {
		text := []rune(chunk.Text)
		if font == nil {
			font = chunk.Style.Font
		}
		annotations := chunk.annotations
		verticalAlignment := chunk.VerticalAlignment
		if len(chunks) > 0 {
			if len(text) == 1 && unicode.IsPunct(text[0]) && chunk.Style.Font == font {
				text2 := []rune(chunks[len(chunks)-1].Text)
				chunks[len(chunks)-1].Text = string(append(text2, text[0]))
				continue
			} else {
				_, err := strconv.Atoi(chunk.Text)
				if err == nil {
					text2 := []rune(chunks[len(chunks)-1].Text)
					textLen := len(text2)
					if textLen >= 2 {
						_, err := strconv.Atoi(string(text2[textLen-2]))
						if err == nil && unicode.IsPunct(text2[textLen-1]) {
							chunks[len(chunks)-1].Text = string(append(text2, text...))
							continue
						}
					}
				}
			}
		}
		intoWords, err := splitIntoWords(chunk.Text)
		if err != nil {
			common.Log.Debug("ERROR: Unable to break string to words: %v", err)
			intoWords = []string{chunk.Text}
		}
		for _, intoWord := range intoWords {
			textChunk := NewTextChunk(intoWord, chunk.Style)
			textChunk.annotations = copyAnnotations(annotations)
			textChunk.VerticalAlignment = verticalAlignment
			chunks = append(chunks, textChunk)
		}
		font = chunk.Style.Font
	}
	if len(chunks) > 0 {
		sp.chunks = chunks
	}
}

// SetTextOverflow controls the behavior of paragraph text which
// does not fit in the available space.
func (sp *StyledParagraph) SetTextOverflow(textOverflow TextOverflow) {
	sp.textOverflow = textOverflow
}

func (sp *StyledParagraph) split(ctx DrawContext) (head, tail *StyledParagraph, err error) {
	if err = sp.wrapChunks(false); err != nil {
		return nil, nil, err
	}
	if len(sp.lines) == 1 && sp.lineHeight > ctx.Height {
		return sp, nil, nil
	}
	fn := func(chunks []*TextChunk, chunks2 []*TextChunk) []*TextChunk {
		if len(chunks2) == 0 {
			return chunks
		}
		chunksLen := len(chunks)
		if chunksLen == 0 {
			return append(chunks, chunks2...)
		}
		if chunks[chunksLen-1].Style == chunks2[0].Style {
			chunks[chunksLen-1].Text += chunks2[0].Text
		} else {
			chunks = append(chunks, chunks2[0])
		}
		return append(chunks, chunks2[1:]...)
	}
	fn2 := func(para *StyledParagraph, chunks []*TextChunk) *StyledParagraph {
		if len(chunks) == 0 {
			return nil
		}
		para2 := *para
		para2.chunks = chunks
		return &para2
	}
	var (
		value   float64
		chunks  []*TextChunk
		chunks2 []*TextChunk
	)
	for _, line := range sp.lines {
		var value2 float64
		chunks3 := make([]*TextChunk, 0, len(line))
		for _, chunk := range line {
			if fontSize := chunk.Style.FontSize * chunk.Style.effectiveLineHeight(sp.lineHeight); fontSize > value2 {
				value2 = fontSize
			}
			chunks3 = append(chunks3, chunk.clone())
		}
		if sp.positioning.IsRelative() {
			if value+value2 >= ctx.Height {
				chunks2 = fn(chunks2, chunks3)
			} else {
				chunks = fn(chunks, chunks3)
			}
		}
		value += value2
	}
	sp.lines = nil
	if len(chunks2) == 0 {
		return sp, nil, nil
	}
	return fn2(sp, chunks), fn2(sp, chunks2), nil
}
