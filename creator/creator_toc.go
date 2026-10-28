package creator

import (
	"strings"

	"github.com/OpenMellon/openpdf/model"
)

// TOC represents a table of contents component.
// It consists of a paragraph heading and a collection of
// table of contents lines.
// The representation of a table of contents line is as follows:
//
//	[number] [title]      [separator] [page]
//
// e.g.: Chapter1 Introduction ........... 1
type TOC struct {
	taggedDrawable
	heading            *StyledParagraph
	lines              []*TOCLine
	lineNumberStyle    TextStyle
	lineTitleStyle     TextStyle
	lineSeparatorStyle TextStyle
	linePageStyle      TextStyle
	lineSeparator      string
	lineLevelOffset    float64
	lineMargins        Margins
	positioning        Positioning
	defaultStyle       TextStyle
	showLinks          bool
}

// SetLineSeparatorStyle sets the style for the separator part of all new
// lines of the table of contents.
func (to *TOC) SetLineSeparatorStyle(style TextStyle) { to.lineSeparatorStyle = style }

func (to *TOC) replaceFonts(font3, font, font4, font2 *model.PdfFont) {
	styles := []*TextStyle{&to.lineNumberStyle, &to.lineTitleStyle, &to.lineSeparatorStyle, &to.linePageStyle, &to.defaultStyle}
	for _, line := range to.lines {
		styles = append(styles, &line.Number.Style, &line.Title.Style, &line.Separator.Style, &line.Page.Style, &line.paragraph.defaultStyle, &line.paragraph.linkStyle)
	}
	styles2 := []*TextStyle{&to.heading.defaultStyle, &to.heading.linkStyle}
	for _, chunk := range to.heading.chunks {
		styles2 = append(styles2, &chunk.Style)
	}
	fn := func(styles []*TextStyle, font3, font, font4, font2 *model.PdfFont) {
		for _, style := range styles {
			switch style.Font {
			case font3:
				style.Font = font
			case font4:
				style.Font = font2
			}
		}
	}
	fn(styles, font3, font, font4, font2)
	fn(styles2, font4, font2, font3, font)
}

// SetLineSeparator sets the separator for all new lines of the table of contents.
func (to *TOC) SetLineSeparator(separator string) { to.lineSeparator = separator }

// Heading returns the heading component of the table of contents.
func (to *TOC) Heading() *StyledParagraph { return to.heading }

// SetLineNumberStyle sets the style for the numbers part of all new lines
// of the table of contents.
func (to *TOC) SetLineNumberStyle(style TextStyle) { to.lineNumberStyle = style }

// SetLineMargins sets the margins for all new lines of the table of contents.
func (to *TOC) SetLineMargins(left, right, top, bottom float64) {
	lineMargins := &to.lineMargins
	lineMargins.Left = left
	lineMargins.Right = right
	lineMargins.Top = top
	lineMargins.Bottom = bottom
}

// SetLineTitleStyle sets the style for the title part of all new lines
// of the table of contents.
func (to *TOC) SetLineTitleStyle(style TextStyle) { to.lineTitleStyle = style }

// SetLineLevelOffset sets the amount of space an indentation level occupies
// for all new lines of the table of contents.
func (to *TOC) SetLineLevelOffset(levelOffset float64) { to.lineLevelOffset = levelOffset }

// SetHeading sets the text and the style of the heading of the TOC component.
func (to *TOC) SetHeading(text string, style TextStyle) {
	heading := to.Heading()
	heading.Reset()
	chunk := heading.Append(text)
	chunk.Style = style
}

// Lines returns all the lines the table of contents has.
func (to *TOC) Lines() []*TOCLine { return to.lines }

// SetShowLinks sets visibility of links for the TOC lines.
func (to *TOC) SetShowLinks(showLinks bool) { to.showLinks = showLinks }

// SetLineStyle sets the style for all the line components: number, title,
// separator, page. The style is applied only for new lines added to the
// TOC component.
func (to *TOC) SetLineStyle(style TextStyle) {
	to.SetLineNumberStyle(style)
	to.SetLineTitleStyle(style)
	to.SetLineSeparatorStyle(style)
	to.SetLinePageStyle(style)
}

// Add adds a new line with the default style to the table of contents.
func (to *TOC) Add(number, title, page string, level uint) *TOCLine {
	line := to.AddLine(newTOCLineFromChunks(TextChunk{Text: number, Style: to.lineNumberStyle}, TextChunk{Text: title, Style: to.lineTitleStyle}, TextChunk{Text: page, Style: to.linePageStyle}, level, to.defaultStyle))
	if line == nil {
		return nil
	}
	lineMargins := &to.lineMargins
	line.SetMargins(lineMargins.Left, lineMargins.Right, lineMargins.Top, lineMargins.Bottom)
	line.SetLevelOffset(to.lineLevelOffset)
	line.Separator.Text = to.lineSeparator
	line.Separator.Style = to.lineSeparatorStyle
	return line
}

// AddLine adds a new line with the provided style to the table of contents.
func (to *TOC) AddLine(line *TOCLine) *TOCLine {
	if line == nil {
		return nil
	}
	to.lines = append(to.lines, line)
	return line
}

// GeneratePageBlocks generate the Page blocks. Multiple blocks are generated
// if the contents wrap over multiple pages.
func (to *TOC) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	ctx2 := ctx
	pageBlocks, ctx, err := to.heading.GeneratePageBlocks(ctx)
	if err != nil {
		return pageBlocks, ctx, err
	}
	for _, line := range to.lines {
		linkPage := line.linkPage
		if !to.showLinks {
			line.linkPage = 0
		}
		pageBlocks2, ctx2, err := line.GeneratePageBlocks(ctx)
		line.linkPage = linkPage
		if err != nil {
			return pageBlocks, ctx, err
		}
		if len(pageBlocks2) < 1 {
			continue
		}
		pageBlocks[len(pageBlocks)-1].mergeBlocks(pageBlocks2[0])
		pageBlocks = append(pageBlocks, pageBlocks2[1:]...)
		ctx = ctx2
	}
	if to.positioning.IsRelative() {
		ctx.X = ctx2.X
	}
	if to.positioning.IsAbsolute() {
		return pageBlocks, ctx2, nil
	}
	return pageBlocks, ctx, nil
}

// SetLinePageStyle sets the style for the page part of all new lines
// of the table of contents.
func (to *TOC) SetLinePageStyle(style TextStyle) { to.linePageStyle = style }

// TOCLine represents a line in a table of contents.
// The component can be used both in the context of a
// table of contents component and as a standalone component.
// The representation of a table of contents line is as follows:
/*
         [number] [title]      [separator] [page]
   e.g.: Chapter1 Introduction ........... 1
*/
type TOCLine struct {
	paragraph *StyledParagraph

	// Holds the text and style of the number part of the TOC line.
	Number TextChunk

	// Holds the text and style of the title part of the TOC line.
	Title TextChunk

	// Holds the text and style of the separator part of the TOC line.
	Separator TextChunk

	// Holds the text and style of the page part of the TOC line.
	Page        TextChunk
	left        float64
	level       uint
	levelOffset float64
	positioning Positioning
	linkX       float64
	linkY       float64
	linkPage    int64
}

// SetLevel sets the indentation level of the TOC line.
func (tl *TOCLine) SetLevel(level uint) {
	tl.level = level
	tl.paragraph.margins.Left = tl.left + float64(tl.level-1)*tl.levelOffset
}

// GetMargins returns the margins of the TOC line: left, right, top, bottom.
func (tl *TOCLine) GetMargins() (float64, float64, float64, float64) {
	margins := &tl.paragraph.margins
	return tl.left, margins.Right, margins.Top, margins.Bottom
}

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

func (tl *TOCLine) getLineLink() *model.PdfAnnotation {
	if tl.linkPage <= 0 {
		return nil
	}
	return newInternalLinkAnnotation(tl.linkPage-1, tl.linkX, tl.linkY, 0, "")
}

// LevelOffset returns the amount of space an indentation level occupies.
func (tl *TOCLine) LevelOffset() float64 { return tl.levelOffset }

// Link returns link information for this line.
func (tl *TOCLine) Link() (page int64, x, y float64) {
	return tl.linkPage, tl.linkX, tl.linkY
}

// Level returns the indentation level of the TOC line.
func (line *TOCLine) Level() uint { return line.level }

// SetStyle sets the style for all the line components: number, title,
// separator, page.
func (tl *TOCLine) SetStyle(style TextStyle) {
	tl.Number.Style = style
	tl.Title.Style = style
	tl.Separator.Style = style
	tl.Page.Style = style
}

// SetLink makes the line an internal link.
// The text parameter represents the text that is displayed.
// The user is taken to the specified page, at the specified x and y
// coordinates. Position 0, 0 is at the top left of the page.
func (tl *TOCLine) SetLink(page int64, x, y float64) {
	tl.linkX = x
	tl.linkY = y
	tl.linkPage = page
	color := tl.paragraph.linkStyle.Color
	tl.Number.Style.Color = color
	tl.Title.Style.Color = color
	tl.Separator.Style.Color = color
	tl.Page.Style.Color = color
}

// SetMargins sets the margins TOC line.
func (tl *TOCLine) SetMargins(left, right, top, bottom float64) {
	tl.left = left
	margins := &tl.paragraph.margins
	margins.Left = tl.left + float64(tl.level-1)*tl.levelOffset
	margins.Right = right
	margins.Top = top
	margins.Bottom = bottom
}

// SetLevelOffset sets the amount of space an indentation level occupies.
func (tl *TOCLine) SetLevelOffset(levelOffset float64) {
	tl.levelOffset = levelOffset
	tl.paragraph.margins.Left = tl.left + float64(tl.level-1)*tl.levelOffset
}

func (tl *TOCLine) prepareParagraph(para *StyledParagraph, ctx DrawContext) {
	text := tl.Title.Text
	if tl.Number.Text != "" {
		text = " " + text
	}
	text += " "
	text2 := tl.Page.Text
	if text2 != "" {
		text2 = " " + text2
	}
	annotations := []*model.PdfAnnotation{}
	lineLink := tl.getLineLink()
	if lineLink != nil {
		annotations = append(annotations, lineLink)
	}
	para.chunks = []*TextChunk{{Text: tl.Number.Text, Style: tl.Number.Style, annotations: annotations}, {Text: text, Style: tl.Title.Style, annotations: annotations}, {Text: text2, Style: tl.Page.Style, annotations: annotations}}
	para.wrapText()
	linesLen := len(para.lines)
	if linesLen == 0 {
		return
	}
	value := ctx.Width*1000 - para.getTextLineWidth(para.lines[linesLen-1])
	textLineWidth := para.getTextLineWidth([]*TextChunk{&tl.Separator})
	count := int(value / textLineWidth)
	text3 := strings.Repeat(tl.Separator.Text, count)
	style := tl.Separator.Style
	chunk := para.Insert(2, text3)
	chunk.Style = style
	chunk.SetAnnotation(tl.getLineLink())
	value = value - float64(count)*textLineWidth
	if value > 500 {
		runeMetrics, ok := style.Font.GetRuneMetrics(' ')
		if ok && value > runeMetrics.Wx {
			wx := int(value / runeMetrics.Wx)
			if wx > 0 {
				style2 := style
				style2.FontSize = 1
				chunk = para.Insert(2, strings.Repeat(" ", wx))
				chunk.Style = style2
				chunk.SetAnnotation(tl.getLineLink())
			}
		}
	}
}

func newTOCLineFromStrings(text, text2, text3 string, level uint, style TextStyle) *TOCLine {
	return newTOCLineFromChunks(TextChunk{Text: text, Style: style}, TextChunk{Text: text2, Style: style}, TextChunk{Text: text3, Style: style}, level, style)
}

func newTOCLineFromChunks(number, title, page TextChunk, level uint, style TextStyle) *TOCLine {
	styledParagraph := newStyledParagraph(style)
	styledParagraph.SetEnableWrap(true)
	styledParagraph.SetTextAlignment(TextAlignmentLeft)
	styledParagraph.SetMargins(0, 0, 2, 2)
	tocLine := &TOCLine{paragraph: styledParagraph, Number: number, Title: title, Page: page, Separator: TextChunk{Text: ".", Style: style}, left: 0, level: level, levelOffset: 10, positioning: PositionRelative}
	styledParagraph.margins.Left = tocLine.left + float64(tocLine.level-1)*tocLine.levelOffset
	styledParagraph.beforeRender = tocLine.prepareParagraph
	return tocLine
}
