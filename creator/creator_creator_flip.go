// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"errors"
	"maps"
	"slices"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// Flip flips the active page on the specified axes.
// If `flipH` is true, the page is flipped horizontally. Similarly, if `flipV`
// is true, the page is flipped vertically. If both are true, the page is
// flipped both horizontally and vertically.
// NOTE: the flip transformations are applied when the creator is finalized,
// which is at write time in most cases.
func (c *Creator) Flip(flipH, flipV bool) error {
	activePage := c.getActivePage()
	if activePage == nil {
		return errors.New("no page active")
	}
	pageTransform, ok := c.pageTransforms[activePage]
	if !ok {
		pageTransform = &pageTransformations{}
		c.pageTransforms[activePage] = pageTransform
	}
	pageTransform.flipH = flipH
	pageTransform.flipV = flipV
	return nil
}

// GetCoords returns the (x1, y1), (x2, y2) points defining the Line.
func (l *Line) GetCoords() (float64, float64, float64, float64) {
	return l.x1, l.y1, l.x2, l.y2
}

// SetBorder sets the cell's border style.
func (tc *TableCell) SetBorder(side CellBorderSide, style CellBorderStyle, width float64) {
	if style == CellBorderStyleSingle && side == CellBorderSideAll {
		tc.borderStyleLeft = CellBorderStyleSingle
		tc.borderWidthLeft = width
		tc.borderStyleBottom = CellBorderStyleSingle
		tc.borderWidthBottom = width
		tc.borderStyleRight = CellBorderStyleSingle
		tc.borderWidthRight = width
		tc.borderStyleTop = CellBorderStyleSingle
		tc.borderWidthTop = width
	} else if style == CellBorderStyleDouble && side == CellBorderSideAll {
		tc.borderStyleLeft = CellBorderStyleDouble
		tc.borderWidthLeft = width
		tc.borderStyleBottom = CellBorderStyleDouble
		tc.borderWidthBottom = width
		tc.borderStyleRight = CellBorderStyleDouble
		tc.borderWidthRight = width
		tc.borderStyleTop = CellBorderStyleDouble
		tc.borderWidthTop = width
	} else if (style == CellBorderStyleSingle || style == CellBorderStyleDouble) && side == CellBorderSideLeft {
		tc.borderStyleLeft = style
		tc.borderWidthLeft = width
	} else if (style == CellBorderStyleSingle || style == CellBorderStyleDouble) && side == CellBorderSideBottom {
		tc.borderStyleBottom = style
		tc.borderWidthBottom = width
	} else if (style == CellBorderStyleSingle || style == CellBorderStyleDouble) && side == CellBorderSideRight {
		tc.borderStyleRight = style
		tc.borderWidthRight = width
	} else if (style == CellBorderStyleSingle || style == CellBorderStyleDouble) && side == CellBorderSideTop {
		tc.borderStyleTop = style
		tc.borderWidthTop = width
	}
}

// IsRelative checks if the positioning is relative.
func (pos Positioning) IsRelative() bool { return pos == PositionRelative }

func (co *Command) compare(command *Command) bool {
	if co.Symbol != command.Symbol {
		return false
	}
	for i, param := range co.Params {
		if param != command.Params[i] {
			return false
		}
	}
	return true
}

// RotateDeg rotates the current active page by angle degrees.  An error is returned on failure,
// which can be if there is no currently active page, or the angleDeg is not a multiple of 90 degrees.
func (c *Creator) RotateDeg(angleDeg int64) error {
	activePage := c.getActivePage()
	if activePage == nil {
		common.Log.Debug("Fail to rotate: no page currently active")
		return errors.New("no page active")
	}
	if angleDeg%90 != 0 {
		common.Log.Debug("ERROR: Page rotation angle not a multiple of 90")
		return errors.New("range check error")
	}
	var rotate int64
	if activePage.Rotate != nil {
		rotate = *activePage.Rotate
	}
	rotate += angleDeg
	activePage.Rotate = &rotate
	return nil
}

// SetWidthTop sets border width for top.
func (bo *border) SetWidthTop(bw float64) { bo.widthTop = bw }

func (tp *templateProcessor) parseChapter(node *templateNode) (any, error) {
	fn := tp.creator.NewChapter
	if componentParent := node.componentParent(); componentParent != nil {
		if chapter, ok := componentParent.value.(*Chapter); ok {
			fn = chapter.NewSubchapter
		}
	}
	chapter := fn("")
	if err := tp.applyAttrs(node, chapter, chapterAttrs); err != nil {
		return nil, err
	}
	return chapter, nil
}

// SetIndent sets the cell's left indent.
func (gc *GridCell) SetIndent(indent float64) { gc.indent = indent }

// SetAlternateText sets the alternate text for the image.
func (im *Image) SetAlternateText(text string) { im.altText = text }

// NewPolyBezierCurve creates a new composite Bezier (polybezier) curve.
func (c *Creator) NewPolyBezierCurve(curves []contentdraw.CubicBezierCurve) *PolyBezierCurve {
	return newPolyBezierCurve(curves)
}

func init() {
	for _, templateTag := range templateTags {
		for allowedParentKey := range templateTag.allowedParents {
			containerTags[allowedParentKey] = struct{}{}
		}
	}
}

func (gss *GraphicSVGStyle) fillStroke(cc *contentstream.ContentCreator) {
	if gss.FillColor != "" && gss.StrokeColor != "" {
		cc.Add_B()
	} else if gss.FillColor != "" {
		cc.Add_f()
	} else if gss.StrokeColor != "" {
		cc.Add_S()
	}
}

// NewCurvePolygon creates a new curve polygon.
func (c *Creator) NewCurvePolygon(rings [][]contentdraw.CubicBezierCurve) *CurvePolygon {
	return newCurvePolygon(rings)
}

// DueDate returns the invoice due date description and value cells.
// The returned values can be used to customize the styles of the cells.
func (i *Invoice) DueDate() (*InvoiceCell, *InvoiceCell) {
	return i.dueDate[0], i.dueDate[1]
}

// Height returns the height of the list.
func (li *List) Height() float64 {
	var ctxHeight float64
	for _, item := range li.items {
		ctxHeight += item.ctxHeight(li.Width())
	}
	return ctxHeight
}

func (tp *templateProcessor) childStyle(node *templateNode) map[string]string {
	nodeOwnStyle := tp.nodeOwnStyle(node)
	if len(nodeOwnStyle) == 0 {
		return node.attrs
	}
	m := make(map[string]string, len(node.attrs)+len(nodeOwnStyle))
	maps.Copy(m, node.attrs)
	maps.Copy(m, nodeOwnStyle)
	return m
}

// LinkTagOptions represents optional parameters for tagged link annotations,
// providing flexibility for adding accessibility features.
type LinkTagOptions struct {
	// MCID is the marked content identifier for structure tree association
	MCID int64

	// AltText is alternative text for screen readers (only used if different from visible text)
	AltText string

	// Tooltip sets the annotation's Contents field (tooltip on hover)
	Tooltip string
}

func (i *Invoice) SetStructPageNumber(pageNumber *int64) {}
func (t *Table) addListItemTag() *model.KDict {
	kDictionary := model.NewKDictionary()
	kDictionary.S = core.MakeName(string(model.StructureTypeListItem))
	t.tagInfo.ComponentKObj.AddKChild(kDictionary)
	return kDictionary
}

type containerDrawable interface {
	Drawable

	// ContainerComponent checks if the component is allowed to be added into provided 'container' and returns
	// preprocessed copy of itself. If the component is not changed it is allowed to return itself in a callback way.
	// If the component is not compatible with provided container this method should return an error.
	ContainerComponent(drawable Drawable) (Drawable, error)
}

func (t *Table) getLastCellFromCol(n int) (int, *TableCell) {
	for i, v := range slices.Backward(t.cells) {
		if v.col <= n && n <= v.col+v.colSpan {
			return i, v
		}
	}
	return 0, nil
}

// This method is not supported by PageBreak component and exists solely to satisfy the Drawable interface.
func (pb *PageBreak) SetStructureType(structureType model.StructureType) {}

// MarkAsArtifact marks the block content as an artifact of the specified type.
//
// All of the block's content is wrapped in an /Artifact marked content sequence
// and the block is excluded from the structure tree. This is used for content
// such as running headers and footers that must not be part of the logical
// structure of a tagged PDF.
func (b *Block) MarkAsArtifact(artifactType model.ArtifactType) *model.Artifact {
	b.artifact = &model.Artifact{Type: artifactType}
	return b.artifact
}

func newInvoice(defaultStyle, textStyle TextStyle) *Invoice {
	invoice := &Invoice{title: "INVOICE", addressSeparator: ", ", defaultStyle: defaultStyle, headingStyle: textStyle}
	invoice.sellerAddress = &InvoiceAddress{Separator: invoice.addressSeparator}
	invoice.buyerAddress = &InvoiceAddress{Heading: "Bill to", Separator: invoice.addressSeparator}
	colorRGBFrom8bit := ColorRGBFrom8bit(245, 245, 245)
	color := ColorRGBFrom8bit(155, 155, 155)
	invoice.titleStyle = textStyle
	invoice.titleStyle.Color = color
	invoice.titleStyle.FontSize = 20
	invoice.addressStyle = defaultStyle
	invoice.addressHeadingStyle = textStyle
	invoice.noteStyle = defaultStyle
	invoice.noteHeadingStyle = textStyle
	invoice.defaultCellProps = invoice.NewCellProps()
	invoice.defaultCellProps.BackgroundColor = colorRGBFrom8bit
	invoice.defaultCellProps.TextStyle = textStyle
	invoice.headingCellProps = invoice.NewCellProps()
	invoice.headingCellProps.TextStyle = textStyle
	invoice.headingCellProps.BackgroundColor = colorRGBFrom8bit
	invoice.headingCellProps.BorderColor = colorRGBFrom8bit
	invoice.lineProps = invoice.NewCellProps()
	invoice.lineProps.BorderColor = colorRGBFrom8bit
	invoice.lineProps.BorderSides = []CellBorderSide{CellBorderSideBottom}
	invoice.lineProps.Alignment = CellHorizontalAlignmentRight
	invoice.totalCellProps = invoice.NewCellProps()
	invoice.totalCellProps.Alignment = CellHorizontalAlignmentRight
	invoice.number = [2]*InvoiceCell{invoice.newCell("Invoice number", invoice.defaultCellProps), invoice.newCell("", invoice.defaultCellProps)}
	invoice.date = [2]*InvoiceCell{invoice.newCell("Date", invoice.defaultCellProps), invoice.newCell("", invoice.defaultCellProps)}
	invoice.dueDate = [2]*InvoiceCell{invoice.newCell("Due Date", invoice.defaultCellProps), invoice.newCell("", invoice.defaultCellProps)}
	invoice.subtotal = [2]*InvoiceCell{invoice.newCell("Subtotal", invoice.totalCellProps), invoice.newCell("", invoice.totalCellProps)}
	totalCellProps := invoice.totalCellProps
	totalCellProps.TextStyle = textStyle
	totalCellProps.BackgroundColor = colorRGBFrom8bit
	totalCellProps.BorderColor = colorRGBFrom8bit
	invoice.total = [2]*InvoiceCell{invoice.newCell("Total", totalCellProps), invoice.newCell("", totalCellProps)}
	invoice.notes = [2]string{"Notes", ""}
	invoice.terms = [2]string{"Terms and conditions", ""}
	invoice.columns = []*InvoiceCell{invoice.newColumn("Description", CellHorizontalAlignmentLeft), invoice.newColumn("Quantity", CellHorizontalAlignmentRight), invoice.newColumn("Unit price", CellHorizontalAlignmentRight), invoice.newColumn("Amount", CellHorizontalAlignmentRight)}
	return invoice
}

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
