package creator

import (
	"errors"
	"fmt"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/model"
)

// Division is a container component which can wrap across multiple pages.
// Currently supported drawable components:
// - *StyledParagraph
// - *Image
// - *Chart
//
// The component stacking behavior is vertical, where the drawables are drawn
// on top of each other.
type Division struct {
	taggedDrawable
	components  []VectorDrawable
	positioning Positioning
	margins     Margins
	padding     Margins
	inline      bool
	enableBreak bool
	background  *Background
}

// Margins returns the margins of the component.
func (d *Division) Margins() (left, right, top, bottom float64) {
	return d.margins.Left, d.margins.Right, d.margins.Top, d.margins.Bottom
}

// Add adds a VectorDrawable to the Division container.
// Currently supported VectorDrawables:
// - *StyledParagraph
// - *Image
// - *Chart
// - *Rectangle
// - *Ellipse
// - *Line
// - *Table
// - *Division
// - *List
// - *PageBreak
func (di *Division) Add(d VectorDrawable) error {
	switch d2 := d.(type) {
	case *StyledParagraph, *Image, *Chart, *Rectangle, *Ellipse, *Line, *Table, *Division, *List, *PageBreak:
	case containerDrawable:
		containerComponent, err := d2.ContainerComponent(di)
		if err != nil {
			return err
		}
		drawable, ok := containerComponent.(VectorDrawable)
		if !ok {
			return fmt.Errorf("result of ContainerComponent - %T doesn't implement VectorDrawable interface", containerComponent)
		}
		d = drawable
	default:
		return errors.New("unsupported type in Division")
	}
	di.components = append(di.components, d)
	return nil
}

// Inline returns whether the inline mode of the division is active.
func (d *Division) Inline() bool { return d.inline }

// SetBackground sets the background properties of the component.
func (d *Division) SetBackground(background *Background) { d.background = background }

// Height returns the height of the division, assuming all components are
// stacked on top of each other.
func (d *Division) Height() float64 {
	var height float64
	for _, component := range d.components {
		switch component2 := component.(type) {
		case marginDrawable:
			_, _, top, bottom := component2.GetMargins()
			height += component2.Height() + top + bottom
		default:
			height += component2.Height()
		}
	}
	return height
}

func (d *Division) split(ctx2 DrawContext, value float64) (head, tail *Division) {
	var (
		drawableHeight2       float64
		drawables, components []VectorDrawable
	)
	right := ctx2.Width - d.margins.Left - d.margins.Right - d.padding.Left - d.padding.Right
	bottom := ctx2.Height - d.padding.Top - d.padding.Bottom
	bottom2 := value - d.padding.Top - d.padding.Bottom
	for i, component := range d.components {
		drawableHeight3 := drawableHeight(component, right)
		if drawableHeight2+drawableHeight3 <= bottom {
			drawableHeight2 += drawableHeight3
			drawables = append(drawables, component)
			continue
		}
		switch component2 := component.(type) {
		case *StyledParagraph:
			ctx := ctx2
			ctx.Width = right
			ctx.Height = bottom - drawableHeight2 - component2.margins.Vertical() - 0.5*component2.getTextHeight()
			if ctx.Height > 0 {
				head, tail, err := component2.split(ctx)
				if err == nil && head != nil {
					drawables = append(drawables, head)
					drawableHeight2 += drawableHeight(head, ctx.Width)
				}
				if tail != nil {
					components = append(components, tail)
				}
			} else {
				components = append(components, component)
			}
		case *Division:
			ctx := ctx2
			ctx.Width = right
			ctx.Height = bottom - drawableHeight2
			if ctx.Height > 0 {
				head, tail := component2.split(ctx, bottom2-component2.margins.Vertical())
				if head != nil {
					drawables = append(drawables, head)
					drawableHeight2 += drawableHeight(head, ctx.Width)
				}
				if tail != nil {
					components = append(components, tail)
				}
			} else {
				components = append(components, component)
			}
		case *List:
			ctx := ctx2
			ctx.Width = right
			ctx.Height = bottom - drawableHeight2
			if ctx.Height > 0 {
				head, tail := component2.split(ctx, bottom2-component2.margins.Vertical())
				if head != nil {
					drawables = append(drawables, head)
					drawableHeight2 += drawableHeight(head, ctx.Width)
				}
				if tail != nil {
					components = append(components, tail)
				}
			} else {
				components = append(components, component)
			}
		case *Table:
			ctx := ctx2
			ctx.Width = right
			ctx.Height = bottom - drawableHeight2
			if ctx.Height > 0 {
				head, tail := component2.split(ctx, bottom2)
				if head != nil {
					drawables = append(drawables, head)
					drawableHeight2 += drawableHeight(head, ctx.Width)
				}
				if tail != nil {
					components = append(components, tail)
				}
			} else {
				components = append(components, component)
			}
		default:
			components = append(components, component)
		}
		components = append(components, d.components[i+1:]...)
		break
	}
	if len(drawables) > 0 {
		head = newDivision()
		*head = *d
		head.components = drawables
		if d.background != nil {
			head.background = &Background{}
			*head.background = *d.background
		}
	}
	if len(components) > 0 {
		tail = newDivision()
		*tail = *d
		tail.components = components
		if d.background != nil {
			tail.background = &Background{}
			*tail.background = *d.background
		}
	}
	return head, tail
}

// Width is not used as the division component is designed to fill all the
// available space, depending on the context. Returns 0.
func (d *Division) Width() float64 { return 0 }

func parseDivisionTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseDivision(node)
}

// SetPadding sets the padding of the component. The padding represents
// inner margins which are applied around the contents of the division.
// The background of the component is not affected by its padding.
func (d *Division) SetPadding(left, right, top, bottom float64) {
	d.padding.Left = left
	d.padding.Right = right
	d.padding.Top = top
	d.padding.Bottom = bottom
}

// EnablePageWrap controls whether the division is wrapped across pages.
// If disabled, the division is moved in its entirety on a new page, if it
// does not fit in the available height. By default, page wrapping is enabled.
// If the height of the division is larger than an entire page, wrapping is
// enabled automatically in order to avoid unwanted behavior.
// Currently, page wrapping can only be disabled for vertical divisions.
func (d *Division) EnablePageWrap(enable bool) { d.enableBreak = enable }

var divisionAttrs = map[string]attrApplyFunc{"enable-page-wrap": boolAttr(func(division *Division, enable bool) { division.EnablePageWrap(enable) }), "margin": marginAttr(func(division *Division, margins Margins) {
	division.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
}), "padding": marginAttr(func(division *Division, margins Margins) {
	division.SetPadding(margins.Left, margins.Right, margins.Top, margins.Bottom)
})}

// AddTag adds a tag to the division.
func (d *Division) AddTag(rootKObj *model.KDict) {
	if d.tagInfo == nil {
		d.tagInfo = model.NewStructureTagInfo()
		d.tagInfo.StructureType = model.StructureTypeDivision
	}
	d.tagInfo.ApplyTag = true
	d.tagInfo.ParentKObj = rootKObj
	d.tagInfo.ComponentKObj = d.tagInfo.NewComponentKObj(d.tagInfo.StructureType)
	d.tagInfo.ParentKObj.AddKChild(d.tagInfo.ComponentKObj)
}

// SetInline sets the inline mode of the division.
func (d *Division) SetInline(inline bool) { d.inline = inline }

func (d *Division) drawBackground(background []*Block, ctx2, ctx3 DrawContext, ok2 bool, m map[int]float64, m2 map[int]bool) ([]*Block, error) {
	backgroundLen := len(background)
	if backgroundLen == 0 || d.background == nil {
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
	ok3 := len(d.components) == 0
	if n == -1 && ok3 {
		n, n2 = 0, len(background)-1
	}
	blocks := make([]*Block, 0, len(background))
	ok4 := false
	for i, block := range background {
		var (
			borderRadiusTopLeft     = d.background.BorderRadiusTopLeft
			borderRadiusTopRight    = d.background.BorderRadiusTopRight
			borderRadiusBottomLeft  = d.background.BorderRadiusBottomLeft
			borderRadiusBottomRight = d.background.BorderRadiusBottomRight
		)
		ctx := ctx2
		ctx.Page += i
		value, ok := m[i]
		ok5 := m2[i] || ok4 || i == n
		if i == 0 {
			if ok2 {
				blocks = append(blocks, block)
				continue
			}
			if ok {
				ctx.Height = value - ctx2.Y
			} else if backgroundLen == 1 {
				ctx.Height = ctx3.Y - ctx2.Y
			}
		} else {
			ctx.X = ctx.Margins.Left + d.margins.Left
			ctx.Y = ctx.Margins.Top
			ctx.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right - d.margins.Left - d.margins.Right
			if ok {
				ctx.Height = value - ctx.Margins.Top
			} else if i == backgroundLen-1 {
				ctx.Height = ctx3.Y - ctx.Margins.Top - d.margins.Top
			} else {
				ctx.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom
			}
			if !ok5 {
				borderRadiusTopLeft = 0
				borderRadiusTopRight = 0
			}
		}
		if (block.isEmpty() && !ok3) || ctx.Height <= 0 {
			if m2[i] {
				ok4 = true
			}
			blocks = append(blocks, block)
			continue
		}
		ok4 = false
		if i != n2 && !ok {
			borderRadiusBottomLeft = 0
			borderRadiusBottomRight = 0
		}
		rectangle := newRectangle(ctx.X, ctx.Y, ctx.Width, ctx.Height)
		rectangle.SetFillColor(d.background.FillColor)
		rectangle.SetBorderColor(d.background.BorderColor)
		rectangle.SetBorderWidth(d.background.BorderSize)
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
		if err = pageBlock.mergeBlocks(block); err != nil {
			return nil, err
		}
		blocks = append(blocks, pageBlock)
	}
	return blocks, nil
}

// Padding returns the padding of the component.
func (d *Division) Padding() (left, right, top, bottom float64) {
	return d.padding.Left, d.padding.Right, d.padding.Top, d.padding.Bottom
}

func (d *Division) ctxHeight(right float64) float64 {
	right -= d.margins.Left + d.margins.Right + d.padding.Left + d.padding.Right
	var ctxHeight float64
	for _, component := range d.components {
		ctxHeight += drawableHeight(component, right)
	}
	return ctxHeight
}

// SetMargins sets the margins of the component. The margins are applied
// around the division.
func (d *Division) SetMargins(left, right, top, bottom float64) {
	d.margins.Left = left
	d.margins.Right = right
	d.margins.Top = top
	d.margins.Bottom = bottom
}

// GeneratePageBlocks generates the page blocks for the Division component.
// Multiple blocks are generated if the contents wrap over multiple pages.
func (d *Division) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	var (
		blocks     []*Block
		ok2        bool
		err        error
		isRelative = d.positioning.IsRelative()
		top        = d.margins.Top
		ok3        = d.tagInfo != nil && d.tagInfo.ApplyTag
		mcid       int64
		m          map[int]float64
		m2         map[int]bool
	)
	if ok3 {
		mcid = d.tagInfo.Mcid
	}
	if isRelative && !d.enableBreak && !d.inline {
		ctxHeight := d.ctxHeight(ctx.Width)
		if ctxHeight > ctx.Height-d.margins.Top && ctxHeight <= ctx.PageHeight-ctx.Margins.Top-ctx.Margins.Bottom {
			if blocks, ctx, err = newPageBreak().GeneratePageBlocks(ctx); err != nil {
				return nil, ctx, err
			}
			ok2 = true
			top = 0
		}
	}
	ctx3 := ctx
	ctx2 := ctx
	if isRelative {
		ctx.X += d.margins.Left
		ctx.Y += top
		ctx.Width -= d.margins.Left + d.margins.Right
		ctx.Height -= top
		ctx2 = ctx
		ctx.X += d.padding.Left
		ctx.Y += d.padding.Top
		ctx.Width -= d.padding.Left + d.padding.Right
		ctx.Height -= d.padding.Top
		ctx.Margins.Top += d.padding.Top
		ctx.Margins.Bottom += d.padding.Bottom
		ctx.Margins.Left += d.margins.Left + d.padding.Left
		ctx.Margins.Right += d.margins.Right + d.padding.Right
	}
	ctx.Inline = d.inline
	ctx4 := ctx
	ctx5 := ctx
	var height float64
	for _, component := range d.components {
		if _, ok := component.(*PageBreak); ok {
			if ctx.Inline {
				continue
			}
			if len(blocks) > 0 {
				if m == nil {
					m = map[int]float64{}
					m2 = map[int]bool{}
				}
				n := len(blocks) - 1
				m[n] = ctx.Y
				m2[n+1] = true
			}
		}
		if ctx.Inline {
			if (ctx.X-ctx4.X)+component.Width() <= ctx.Width {
				ctx.Y = ctx5.Y
				ctx.Height = ctx5.Height
			} else {
				ctx.X = ctx4.X
				ctx.Width = ctx4.Width
				ctx5.Y += height
				ctx5.Height -= height
				height = 0
			}
		}
		_, ok := component.(*StyledParagraph)
		structureTypeParagraph := model.StructureTypeParagraph
		if ok3 && ok {
			component.SetMarkedContentID(mcid)
			component.SetStructureType(structureTypeParagraph)
		}
		pageBlocks, ctx2, err := component.GeneratePageBlocks(ctx)
		if err != nil {
			common.Log.Debug("Error generating page blocks: %v", err)
			return nil, ctx, err
		}
		if ok3 && ok {
			page := int64(ctx2.Page)
			component.SetStructPageNumber(&page)
			kDict, err := component.GenerateKDict()
			if err != nil {
				return nil, ctx, err
			}
			d.tagInfo.ComponentKObj.AddKChild(kDict)
			mcid++
		}
		if len(pageBlocks) < 1 {
			continue
		}
		if len(blocks) > 0 {
			blocks[len(blocks)-1].mergeBlocks(pageBlocks[0])
			blocks = append(blocks, pageBlocks[1:]...)
		} else {
			if contents := pageBlocks[0].contents; contents == nil || len(*contents) == 0 {
				ok2 = true
			}
			blocks = append(blocks, pageBlocks[0:]...)
		}
		y := 0.0
		switch component2 := component.(type) {
		case *StyledParagraph:
			y = (0.5 * component2.getTextHeight())
		}
		ctx2.Y += y
		ctx2.Height -= y
		if ctx.Inline {
			if ctx.Page != ctx2.Page {
				ctx4.Y = ctx.Margins.Top
				ctx4.Height = ctx.PageHeight - ctx.Margins.Top
				ctx5.Y = ctx4.Y
				ctx5.Height = ctx4.Height
				height = ctx2.Height - ctx4.Height
			} else {
				if height2 := ctx.Height - ctx2.Height; height2 > height {
					height = height2
				}
			}
		} else {
			ctx2.X = ctx.X
		}
		ctx = ctx2
	}
	if len(d.components) == 0 {
		block := NewBlock(ctx.Width, 0)
		blocks = append(blocks, block)
	}
	ctx.Inline = ctx3.Inline
	ctx.Margins = ctx3.Margins
	if isRelative {
		ctx.X = ctx3.X
		ctx.Width = ctx3.Width
		ctx.Y += d.padding.Bottom
		ctx.Height -= d.padding.Bottom
	}
	if d.background != nil {
		blocks, err = d.drawBackground(blocks, ctx2, ctx, ok2, m, m2)
		if err != nil {
			return nil, ctx, err
		}
	}
	if d.positioning.IsAbsolute() {
		return blocks, ctx3, nil
	}
	ctx.Y += d.margins.Bottom
	ctx.Height -= d.margins.Bottom
	return blocks, ctx, nil
}

func newDivision() *Division {
	return &Division{enableBreak: true, taggedDrawable: taggedDrawable{structType: model.StructureTypeDivision}}
}
