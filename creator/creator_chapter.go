package creator

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/model"
)

// Chapter is used to arrange multiple drawables (paragraphs, images, etc) into a single section.
// The concept is the same as a book or a report chapter.
type Chapter struct {
	taggedDrawable
	number          int
	title           string
	heading         *StyledParagraph
	contents        []Drawable
	subchapterCount int
	showNumbering   bool
	includeInTOC    bool
	positioning     Positioning
	margins         Margins
	background      *Background
	parent          *Chapter
	toc             *TOC
	outline         *model.Outline
	outlineItem     *model.OutlineItem
	level           uint
	recordFunc      chapterRecordFunc
}

// SetIncludeInTOC sets a flag to indicate whether or not to include in tOC.
func (ch *Chapter) SetIncludeInTOC(includeInTOC bool) { ch.includeInTOC = includeInTOC }

func (ch *Chapter) drawBackground(background []*Block, ctx2, ctx3 DrawContext, m map[int]float64, m2 map[int]bool) ([]*Block, error) {
	backgroundLen := len(background)
	if backgroundLen == 0 || ch.background == nil {
		return background, nil
	}
	n, n2 := -1, -1
	for i, block := range background {
		if !block.isEmpty() {
			if n == -1 {
				n = i
			}
			n2 = i
		}
	}
	blocks := make([]*Block, 0, backgroundLen)
	ok2 := false
	for i, block := range background {
		if block.isEmpty() {
			if m2[i] {
				ok2 = true
			}
			blocks = append(blocks, block)
			continue
		}
		var (
			borderRadiusTopLeft     = ch.background.BorderRadiusTopLeft
			borderRadiusTopRight    = ch.background.BorderRadiusTopRight
			borderRadiusBottomLeft  = ch.background.BorderRadiusBottomLeft
			borderRadiusBottomRight = ch.background.BorderRadiusBottomRight
		)
		ctx := ctx2
		ctx.Page += i
		value, ok := m[i]
		ok3 := m2[i] || ok2 || i == n
		ok2 = false
		y := ctx2.Y
		if i != 0 {
			ctx.X = ctx.Margins.Left
			ctx.Y = ctx.Margins.Top
			ctx.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right
			y = ctx.Margins.Top
		}
		switch {
		case ok:
			ctx.Height = value - y
		case i == n2:
			ctx.Height = ctx3.Y - y
		default:
			ctx.Height = ctx.PageHeight - ctx.Margins.Bottom - y
		}
		if ctx.Height <= 0 {
			blocks = append(blocks, block)
			continue
		}
		if !ok3 {
			borderRadiusTopLeft = 0
			borderRadiusTopRight = 0
		}
		if !(i == n2 || ok) {
			borderRadiusBottomLeft = 0
			borderRadiusBottomRight = 0
		}
		rectangle := newRectangle(ctx.X, ctx.Y, ctx.Width, ctx.Height)
		rectangle.SetFillColor(ch.background.FillColor)
		rectangle.SetBorderColor(ch.background.BorderColor)
		rectangle.SetBorderWidth(ch.background.BorderSize)
		rectangle.SetBorderRadius(borderRadiusTopLeft, borderRadiusTopRight, borderRadiusBottomLeft, borderRadiusBottomRight)
		pageBlocks, _, err := rectangle.GeneratePageBlocks(ctx)
		if err != nil {
			return nil, err
		}
		if len(pageBlocks) == 0 {
			blocks = append(blocks, block)
			continue
		}
		pageBlock := pageBlocks[0]
		contentStreamOperations := &contentstream.ContentStreamOperations{}
		if err = mergeContentStreams(contentStreamOperations, block.resources, pageBlock.contents, pageBlock.resources); err != nil {
			return nil, err
		}
		*block.contents = append(*contentStreamOperations, *block.contents...)
		blocks = append(blocks, block)
	}
	return blocks, nil
}

// GeneratePageBlocks generate the Page blocks.  Multiple blocks are generated if the contents wrap
// over multiple pages.
func (ch *Chapter) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	ctx4 := ctx
	ok := ch.tagInfo != nil && ch.tagInfo.ApplyTag
	var mcid int64
	var (
		m  map[int]float64
		m2 map[int]bool
	)
	if ok {
		mcid = ch.tagInfo.Mcid
	}
	if ch.positioning.IsRelative() {
		ctx.X += ch.margins.Left
		ctx.Y += ch.margins.Top
		ctx.Width -= ch.margins.Left + ch.margins.Right
		ctx.Height -= ch.margins.Top
	}
	ctx3 := ctx
	if ok {
		ch.heading.SetMarkedContentID(mcid)
		ch.heading.SetStructureType(model.StructureTypeHeader)
	}
	pageBlocks, ctx2, err := ch.heading.GeneratePageBlocks(ctx)
	if err != nil {
		return pageBlocks, ctx, err
	}
	if ok {
		page := int64(ctx2.Page)
		ch.heading.SetStructPageNumber(&page)
		kDict, err := ch.heading.GenerateKDict()
		if err != nil {
			return nil, ctx, err
		}
		ch.tagInfo.ComponentKObj.AddKChild(kDict)
		mcid++
	}
	ctx = ctx2
	x := ctx.X
	y := ctx.Y - ch.heading.Height()
	page := int64(ctx.Page)
	headingNumber := ch.headingNumber()
	headingText := ch.headingText()
	if ch.recordFunc != nil {
		ch.recordFunc(ch, ctx.Page)
	}
	if ch.includeInTOC {
		tocLine := ch.toc.Add(headingNumber, ch.title, strconv.FormatInt(page, 10), ch.level)
		if ch.toc.showLinks {
			tocLine.SetLink(page, x, y)
		}
	}
	if ch.outlineItem == nil {
		ch.outlineItem = model.NewOutlineItem(headingText, model.NewOutlineDest(page-1, x, y))
		if ch.parent != nil {
			ch.parent.outlineItem.Add(ch.outlineItem)
		} else {
			ch.outline.Add(ch.outlineItem)
		}
	} else {
		dest := &ch.outlineItem.Dest
		dest.Page = page - 1
		dest.X = x
		dest.Y = y
	}
	for _, content := range ch.contents {
		if _, ok := content.(*PageBreak); ok {
			if m == nil {
				m = map[int]float64{}
				m2 = map[int]bool{}
			}
			n := len(pageBlocks) - 1
			m[n] = ctx.Y
			m2[n+1] = true
		}
		ok2 := false
		var structureTypeParagraph model.StructureType
		if ok {
			switch content2 := content.(type) {
			case *StyledParagraph:
				ok2 = true
				structureTypeParagraph = model.StructureTypeParagraph
				content.SetMarkedContentID(mcid)
				content.SetStructureType(structureTypeParagraph)
			case *Division:
				ok2 = true
				structureTypeParagraph = model.StructureTypeDivision
				content2.AddTag(ch.tagInfo.ComponentKObj)
			case *Table:
				ok2 = true
				structureTypeParagraph = model.StructureTypeTable
				content2.AddTag(ch.tagInfo.ComponentKObj)
			case *List:
				ok2 = true
				structureTypeParagraph = model.StructureTypeList
				content2.AddTag(ch.tagInfo.ComponentKObj)
			case *Chapter:
				ok2 = true
				structureTypeParagraph = model.StructureTypeSection
				content2.AddTag(ch.tagInfo.ComponentKObj)
			}
		}
		pageBlocks2, ctx2, err := content.GeneratePageBlocks(ctx)
		if err != nil {
			return pageBlocks, ctx, err
		}
		if ok && ok2 {
			page := int64(ctx2.Page)
			content.SetStructPageNumber(&page)
			if structureTypeParagraph == model.StructureTypeParagraph {
				kDict, err := content.GenerateKDict()
				if err != nil {
					return nil, ctx, err
				}
				ch.tagInfo.ComponentKObj.AddKChild(kDict)
			}
			mcid++
		}
		if len(pageBlocks2) < 1 {
			continue
		}
		pageBlocks[len(pageBlocks)-1].mergeBlocks(pageBlocks2[0])
		pageBlocks = append(pageBlocks, pageBlocks2[1:]...)
		ctx = ctx2
	}
	if ch.background != nil {
		var err error
		if pageBlocks, err = ch.drawBackground(pageBlocks, ctx3, ctx, m, m2); err != nil {
			return nil, ctx, err
		}
	}
	if ch.positioning.IsRelative() {
		ctx.X = ctx4.X
	}
	if ch.positioning.IsAbsolute() {
		return pageBlocks, ctx4, nil
	}
	return pageBlocks, ctx, nil
}

func (ch *Chapter) headingText() string {
	title := ch.title
	if headingNumber := ch.headingNumber(); headingNumber != "" {
		title = fmt.Sprintf("%s %s", headingNumber, title)
	}
	return title
}

var chapterAttrs = map[string]attrApplyFunc{"show-numbering": boolAttr(func(chapter *Chapter, show bool) { chapter.SetShowNumbering(show) }), "include-in-toc": boolAttr(func(chapter *Chapter, includeInTOC bool) { chapter.SetIncludeInTOC(includeInTOC) }), "margin": marginAttr(func(chapter *Chapter, margins Margins) {
	chapter.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
})}

var chapterHeadingAttrs = map[string]attrApplyFunc{"font": fontAttr(func(para *StyledParagraph, font *model.PdfFont) { para.SetFont(font) }), "font-size": floatAttr(func(para *StyledParagraph, fontSize float64) { para.SetFontSize(fontSize) }), "text-align": enumAttr(textAlignmentValues, func(para *StyledParagraph, align TextAlignment) { para.SetTextAlignment(align) }), "line-height": floatAttr(func(para *StyledParagraph, lineheight float64) { para.SetLineHeight(lineheight) }), "enable-wrap": boolAttr(func(para *StyledParagraph, enableWrap bool) { para.SetEnableWrap(enableWrap) }), "color": colorAttr(func(para *StyledParagraph, color Color) { para.SetFontColor(color) }), "x": floatAttr(func(para *StyledParagraph, x float64) { para.SetPos(x, para.yPos) }), "y": floatAttr(func(para *StyledParagraph, y float64) { para.SetPos(para.xPos, y) }), "angle": floatAttr(func(para *StyledParagraph, angle float64) { para.SetAngle(angle) }), "margin": marginAttr(func(para *StyledParagraph, margins Margins) {
	para.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
}), "max-lines": intAttr(func(para *StyledParagraph, n int64) { para.SetMaxLines(int(n)) })}

func newChapter(parent *Chapter, toc *TOC, outline *model.Outline, recordFunc chapterRecordFunc, title string, number int, style TextStyle) *Chapter {
	var level uint = 1
	if parent != nil {
		level = parent.level + 1
	}
	chapter := &Chapter{number: number, title: title, showNumbering: true, includeInTOC: true, parent: parent, toc: toc, outline: outline, contents: []Drawable{}, level: level, recordFunc: recordFunc}
	styledParagraph := newStyledParagraph(style)
	text := styledParagraph.SetText(chapter.headingText())
	text.Style = style
	chapter.heading = styledParagraph
	return chapter
}

// GetMargins returns the Chapter's margin: left, right, top, bottom.
func (ch *Chapter) GetMargins() (float64, float64, float64, float64) {
	return ch.margins.Left, ch.margins.Right, ch.margins.Top, ch.margins.Bottom
}

func (ch *Chapter) chapterInfo() ChapterInfo {
	return ChapterInfo{Number: strings.TrimSuffix(ch.headingNumber(), "."), Title: ch.title, Level: ch.level}
}

// AddTag adds a tag to the chapter.
func (ch *Chapter) AddTag(rootKObj *model.KDict) {
	if ch.tagInfo == nil {
		ch.tagInfo = model.NewStructureTagInfo()
		ch.tagInfo.StructureType = model.StructureTypeSection
	}
	ch.tagInfo.ApplyTag = true
	ch.tagInfo.ParentKObj = rootKObj
	ch.tagInfo.ComponentKObj = ch.tagInfo.NewComponentKObj(ch.tagInfo.StructureType)
	ch.tagInfo.ParentKObj.AddKChild(ch.tagInfo.ComponentKObj)
}

// Add adds a new Drawable to the chapter.
// Currently supported Drawables:
// - *StyledParagraph
// - *Image
// - *Chart
// - *Table
// - *Division
// - *List
// - *Rectangle
// - *Ellipse
// - *Line
// - *Block,
// - *PageBreak
// - *Chapter
func (ch *Chapter) Add(d Drawable) error {
	if Drawable(ch) == d {
		common.Log.Debug("ERROR: Cannot add itself")
		return errors.New("range check error")
	}
	switch d2 := d.(type) {
	case *StyledParagraph, *Image, *Chart, *Table, *Division, *List, *Rectangle, *Ellipse, *Line, *Block, *PageBreak, *Chapter:
		ch.contents = append(ch.contents, d)
	case containerDrawable:
		containerComponent, err := d2.ContainerComponent(ch)
		if err != nil {
			return err
		}
		ch.contents = append(ch.contents, containerComponent)
	default:
		common.Log.Debug("Unsupported: %T", d)
		return errors.New("type check error")
	}
	return nil
}

func (ch *Chapter) headingNumber() string {
	var headingNumber string
	if ch.showNumbering {
		if ch.number != 0 {
			headingNumber = strconv.Itoa(ch.number) + "."
		}
		if ch.parent != nil {
			headingNumber2 := ch.parent.headingNumber()
			if headingNumber2 != "" {
				headingNumber = headingNumber2 + headingNumber
			}
		}
	}
	return headingNumber
}

// SetShowNumbering sets a flag to indicate whether or not to show chapter numbers as part of title.
func (ch *Chapter) SetShowNumbering(show bool) {
	ch.showNumbering = show
	ch.heading.SetText(ch.headingText())
}

func parseChapterTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseChapter(node)
}

// SetMargins sets the Chapter margins: left, right, top, bottom.
// Typically not needed as the creator's page margins are used.
func (ch *Chapter) SetMargins(left, right, top, bottom float64) {
	ch.margins.Left = left
	ch.margins.Right = right
	ch.margins.Top = top
	ch.margins.Bottom = bottom
}

// NewSubchapter creates a new child chapter with the specified title.
func (ch *Chapter) NewSubchapter(title string) *Chapter {
	style := defaultTextStyle(ch.heading.chunks[0].Style.Font)
	style.FontSize = 14
	ch.subchapterCount++
	chapter := newChapter(ch, ch.toc, ch.outline, ch.recordFunc, title, ch.subchapterCount, style)
	ch.Add(chapter)
	return chapter
}

// GetHeading returns the chapter heading paragraph. Used to give access to address style: font, sizing etc.
func (ch *Chapter) GetHeading() *StyledParagraph { return ch.heading }

func parseChapterHeadingTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseChapterHeading(node)
}

// SetBackground sets the background properties of the chapter. The background
// is drawn behind the chapter content on every page the chapter occupies.
func (ch *Chapter) SetBackground(background *Background) { ch.background = background }

type chapterRecordFunc func(chapter *Chapter, page int)

// ChapterInfo describes the chapter a page belongs to. It is surfaced to the
// header, footer, and page-finalize callbacks so they can render chapter-aware
// content such as a header like "1.2 Overview".
type ChapterInfo struct {
	// Number is the dotted chapter number (e.g. "1" for a top-level chapter,
	// "1.2.3" for a nested subchapter). Unlike the internal heading text, it
	// has no trailing dot.
	Number string

	// Title is the chapter title as supplied to NewChapter/NewSubchapter,
	// without any number prefix.
	Title string

	// Level is the depth in the chapter hierarchy: 1 for a top-level chapter,
	// 2 or more for nested subchapters.
	Level uint
}

type chapterInfoEntry struct {
	chapter *Chapter
	page    int
}
