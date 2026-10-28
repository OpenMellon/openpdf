package creator

import (
	"errors"
	"math"
	"strings"

	"github.com/OpenMellon/openpdf/model"
)

// List represents a list of items.
// The representation of a list item is as follows:
//
//	[marker] [content]
//
// e.g.:        • This is the content of the item.
// The supported components to add content to list items are:
// - StyledParagraph
// - List
type List struct {
	taggedDrawable
	items         []*listItem
	margins       Margins
	marker        TextChunk
	indent        float64
	defaultIndent bool
	positioning   Positioning
	textStyle     TextStyle
}

// SetIndent sets the left offset of the list when nested into another list.
func (li *List) SetIndent(indent float64) { li.indent = indent; li.defaultIndent = false }

func newList(style TextStyle) *List {
	return &List{marker: TextChunk{Text: "• ", Style: style}, indent: 0, defaultIndent: true, positioning: PositionRelative, textStyle: style, taggedDrawable: taggedDrawable{structType: model.StructureTypeList}}
}

// Margins returns the margins of the list: left, right, top, bottom.
func (li *List) Margins() (float64, float64, float64, float64) {
	return li.margins.Left, li.margins.Right, li.margins.Top, li.margins.Bottom
}

func parseListTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseList(node)
}

func (li *List) itemWidth(width, markerWidth float64) float64 {
	indent := width - li.margins.Horizontal() - li.indent
	return indent * (1 - markerWidth/width)
}

func (li *List) ctxHeight(width float64) float64 {
	itemWidth := li.itemWidth(width, li.markerWidth())
	var ctxHeight float64
	for _, item := range li.items {
		ctxHeight += item.ctxHeight(itemWidth)
	}
	return ctxHeight
}

// Height returns the height of the list.
func (li *List) Height() float64 {
	var ctxHeight float64
	for _, item := range li.items {
		ctxHeight += item.ctxHeight(li.Width())
	}
	return ctxHeight
}

func (li *List) split(ctx2 DrawContext, value float64) (head, tail *List) {
	var (
		value2           float64
		listItems, items []*listItem
	)
	markerWidth := li.markerWidth()
	itemWidth := li.itemWidth(ctx2.Width, markerWidth)
	for i, item := range li.items {
		ctxHeight := item.ctxHeight(itemWidth)
		value2 += ctxHeight
		if value2 <= ctx2.Height {
			listItems = append(listItems, item)
			continue
		}
		height := ctx2.Height - (value2 - ctxHeight)
		var content, content2 VectorDrawable
		style := item.marker.Style
		switch content3 := item.content.(type) {
		case *List:
			ctx := ctx2
			ctx.Width = itemWidth
			ctx.Height = math.Floor(height)
			head, tail := content3.split(ctx, value-content3.margins.Vertical())
			if head != nil {
				content = head
			}
			if tail != nil {
				content2 = tail
			}
			style = content3.marker.Style
		case *StyledParagraph:
			ctx := ctx2
			ctx.Width = itemWidth
			ctx.Height = math.Floor(height - content3.margins.Vertical() - 0.5*content3.getTextHeight())
			if ctx.Height > 0 {
				head, tail, err := content3.split(ctx)
				if err == nil && head != nil && tail != nil {
					content, content2 = head, tail
				}
			}
		case *Division:
			ctx := ctx2
			ctx.Width = itemWidth
			ctx.Height = math.Floor(height - content3.margins.Vertical())
			if ctx.Height > 0 {
				head, tail := content3.split(ctx, value-content3.margins.Vertical())
				if head != nil {
					content = head
				}
				if tail != nil {
					content2 = tail
				}
			}
		case *Table:
			ctx := ctx2
			ctx.Width = itemWidth
			ctx.Height = height
			head, tail := content3.split(ctx, value)
			if head != nil {
				content = head
			}
			if tail != nil {
				content2 = tail
			}
		}
		if content == nil {
			items = li.items[i:]
			break
		}
		listItem := newListItem()
		listItem.marker = item.marker
		listItem.content = content
		listItems = append(listItems, listItem)
		if content2 != nil {
			fontSize := style.FontSize
			runeMetrics, ok := style.Font.GetRuneMetrics(' ')
			if ok {
				fontSize = style.FontSize * runeMetrics.Wx * style.horizontalScale() / 1000.0
			}
			var text string
			if fontSize > 0 {
				text = strings.Repeat(" ", int(markerWidth/fontSize))
			}
			listItem := newListItem()
			listItem.marker = *NewTextChunk(text, style)
			listItem.content = content2
			items = append(items, listItem)
		}
		items = append(items, li.items[i+1:]...)
		break
	}
	if len(listItems) > 0 {
		head = newList(li.textStyle)
		*head = *li
		head.items = listItems
	}
	if len(items) > 0 {
		tail = newList(li.textStyle)
		*tail = *li
		tail.items = items
	}
	return head, tail
}

// AddTag adds a tag to the list.
func (li *List) AddTag(rootKObj *model.KDict) {
	if li.tagInfo == nil {
		li.tagInfo = model.NewStructureTagInfo()
		li.tagInfo.StructureType = model.StructureTypeList
	}
	li.tagInfo.ApplyTag = true
	li.tagInfo.ParentKObj = rootKObj
}

// Indent returns the left offset of the list when nested into another list.
func (li *List) Indent() float64 { return li.indent }

// Marker returns the marker used for the list items.
// The marker instance can be used the change the text and the style
// of newly added list items.
func (li *List) Marker() *TextChunk { return &li.marker }

func parseListMarkerTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseListMarker(node)
}

// AddTextItem appends a new item with the specified text to the list.
// The method creates a styled paragraph with the specified text and returns
// it so that the item style can be customized.
// The method also returns the marker used for the newly added item.
// The marker object can be used to change the text and style of the marker
// for the current item.
func (li *List) AddTextItem(text string) (*StyledParagraph, *TextChunk, error) {
	styledParagraph := newStyledParagraph(li.textStyle)
	styledParagraph.Append(text)
	chunk, err := li.Add(styledParagraph)
	return styledParagraph, chunk, err
}

var listAttrs = map[string]attrApplyFunc{"indent": floatAttr(func(list *List, indent float64) { list.SetIndent(indent) }), "margin": marginAttr(func(list *List, margins Margins) {
	list.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
})}

// SetMargins sets the margins of the paragraph.
func (li *List) SetMargins(left, right, top, bottom float64) {
	li.margins.Left = left
	li.margins.Right = right
	li.margins.Top = top
	li.margins.Bottom = bottom
}

// GeneratePageBlocks generate the Page blocks. Multiple blocks are generated
// if the contents wrap over multiple pages.
func (li *List) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	var value float64
	var paras []*StyledParagraph
	ok := li.tagInfo != nil && li.tagInfo.ApplyTag
	for _, item := range li.items {
		styledParagraph := newStyledParagraph(li.textStyle)
		styledParagraph.SetEnableWrap(false)
		styledParagraph.SetTextAlignment(TextAlignmentRight)
		styledParagraph.Append(item.marker.Text).Style = item.marker.Style
		width := styledParagraph.getTextWidth() / 1000.0 / ctx.Width
		if value < width {
			value = width
		}
		paras = append(paras, styledParagraph)
	}
	table := newTable(2)
	table.SetColumnWidths(value, 1-value)
	table.SetMargins(li.margins.Left+li.indent, li.margins.Right, li.margins.Top, li.margins.Bottom)
	table.EnableRowWrap(true)
	if ok {
		table.SetStructPageNumber(li.tagInfo.StructPageNumber)
		table.AddTag(li.tagInfo.ParentKObj)
		table.SetStructureType(model.StructureTypeList)
		table.SetMarkedContentID(li.tagInfo.Mcid)
	}
	for i, item := range li.items {
		cell := table.NewCell()
		cell.SetIndent(0)
		cell.SetContent(paras[i])
		if ok {
			cell.SetStructPageNumber(li.tagInfo.StructPageNumber)
			cell.SetStructureType(model.StructureTypeLabel)
		}
		cell = table.NewCell()
		cell.SetIndent(0)
		cell.SetContent(item.content)
		if ok {
			cell.SetStructPageNumber(li.tagInfo.StructPageNumber)
			cell.SetStructureType(model.StructureTypeListBody)
		}
	}
	return table.GeneratePageBlocks(ctx)
}

// Width is not used. The list component is designed to fill into the available
// width depending on the context. Returns 0.
func (li *List) Width() float64 { return 0 }

func (li *List) markerWidth() float64 {
	var markerWidth float64
	for _, item := range li.items {
		para := StyledParagraph{chunks: []*TextChunk{&item.marker}}
		markerWidth2 := para.getTextWidth() / 1000.0
		if markerWidth < markerWidth2 {
			markerWidth = markerWidth2
		}
	}
	return markerWidth
}

// Add appends a new item to the list.
// The supported components are: *StyledParagraph, *Division, *Image, *Table, and *List.
// Returns the marker used for the newly added item. The returned marker
// object can be used to change the text and style of the marker for the
// current item.
func (li *List) Add(item VectorDrawable) (*TextChunk, error) {
	listItem2 := &listItem{content: item, marker: li.marker}
	switch item2 := item.(type) {
	case *StyledParagraph:
	case *List:
		if item2.defaultIndent {
			item2.indent = 15
		}
	case *Division:
	case *Image:
	case *Table:
	default:
		return nil, errors.New("this type of drawable is not supported in list")
	}
	li.items = append(li.items, listItem2)
	return &listItem2.marker, nil
}

type listItem struct {
	content VectorDrawable
	marker  TextChunk
}

func newListItem() *listItem { return &listItem{} }

func parseListItemTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseListItem(node)
}

func (lii *listItem) ctxHeight(width float64) float64 {
	var height float64
	switch content := lii.content.(type) {
	case *StyledParagraph:
		if content.enableWrap {
			content.SetWidth(width - content.margins.Horizontal())
		}
		height = content.Height() + content.margins.Vertical()
		height += 0.5 * content.getTextHeight()
	case *List:
		height = content.ctxHeight(width) + content.margins.Vertical()
	case *Image:
		height = content.Height() + content.margins.Vertical()
	case *Division:
		height = content.ctxHeight(width) + content.margins.Vertical() + content.padding.Vertical()
	case *Table:
		content.updateRowHeights(width - content.margins.Horizontal())
		height = content.Height() + content.margins.Vertical()
	default:
		height = lii.content.Height()
	}
	return height
}
