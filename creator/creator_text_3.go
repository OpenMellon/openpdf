package creator

// GetTextAlignment returns the horizontal alignment of the text within the
// paragraph.
func (sp *StyledParagraph) GetTextAlignment() TextAlignment { return sp.alignment }

// SetDropCapsWithStyle is a convenience method to set drop caps with a custom style.
// This allows you to customize the appearance of the drop cap (color, font, etc.)
// while specifying the basic drop caps parameters.
//
// Example:
//
//	p := c.NewStyledParagraph()
//	p.SetText("This is a paragraph with a styled drop cap...")
//	style := creator.TextStyle{
//	    Font:  boldFont,
//	    Color: creator.ColorRGBFrom8bit(200, 0, 0), // Red
//	}
//	p.SetDropCapsWithStyle(creator.DropCapsDrop, creator.DropCapsFirstCharacter, 3, 5.0, &style)
func (sp *StyledParagraph) SetDropCapsWithStyle(dropType DropCapsType, scope DropCapsScope, numLines int, gap float64, style *TextStyle) {
	sp.SetDropCaps(DropCapsOptions{Type: dropType, Scope: scope, NumLines: numLines, Gap: gap, Style: style})
}

func parseStyledParagraphTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseStyledParagraph(node)
}

// SetLineHeight sets the default line height for chunks in this paragraph
// (1.0 default). Individual chunks can override it via TextStyle.LineHeight.
func (sp *StyledParagraph) SetLineHeight(lineheight float64) { sp.lineHeight = lineheight }

func (sp *StyledParagraph) appendChunk(chunk *TextChunk) *TextChunk {
	sp.chunks = append(sp.chunks, chunk)
	sp.wrapText()
	return chunk
}

// SetFontColor sets the font color for the paragraph.
func (sp *StyledParagraph) SetFontColor(color Color) {
	sp.defaultStyle.Color = color
	for _, chunk := range sp.chunks {
		chunk.Style.Color = color
	}
}

func (sp *StyledParagraph) getMaxLineWidth() float64 {
	if sp.lines == nil || (sp.lines != nil && len(sp.lines) == 0) {
		sp.wrapText()
	}
	var maxLineWidth float64
	for _, line := range sp.lines {
		textLineWidth := sp.getTextLineWidth(line)
		if textLineWidth > maxLineWidth {
			maxLineWidth = textLineWidth
		}
	}
	return maxLineWidth
}

// SetDropCapsInline sets inline drop caps for the paragraph, where all lines
// flow beside the enlarged character(s).
//
// Example:
//
//	p := c.NewStyledParagraph()
//	p.SetText("This paragraph has inline drop caps...")
//	p.SetDropCapsInline(creator.DropCapsFirstCharacter, 2, 5.0)
func (sp *StyledParagraph) SetDropCapsInline(scope DropCapsScope, numLines int, gap float64) {
	sp.SetDropCaps(DropCapsOptions{Type: DropCapsInline, Scope: scope, NumLines: numLines, Gap: gap})
}
