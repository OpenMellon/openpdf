// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"encoding/xml"
	"errors"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/model"
)

// Invoice represents a configurable invoice template.
type Invoice struct {
	title               string
	logo                *Image
	buyerAddress        *InvoiceAddress
	sellerAddress       *InvoiceAddress
	addressSeparator    string
	number              [2]*InvoiceCell
	date                [2]*InvoiceCell
	dueDate             [2]*InvoiceCell
	infoLines           [][2]*InvoiceCell
	columns             []*InvoiceCell
	lines               [][]*InvoiceCell
	subtotal            [2]*InvoiceCell
	total               [2]*InvoiceCell
	totalLines          [][2]*InvoiceCell
	notes               [2]string
	terms               [2]string
	sections            [][2]string
	defaultStyle        TextStyle
	headingStyle        TextStyle
	titleStyle          TextStyle
	addressStyle        TextStyle
	addressHeadingStyle TextStyle
	noteStyle           TextStyle
	noteHeadingStyle    TextStyle
	defaultCellProps    InvoiceCellProps
	headingCellProps    InvoiceCellProps
	lineProps           InvoiceCellProps
	totalCellProps      InvoiceCellProps
	positioning         Positioning
}

// SetStructPageNumber sets the page object where the structure element for this drawable is located.
func (td *taggedDrawable) SetStructPageNumber(pageNumber *int64) {
	if td.tagInfo == nil {
		td.tagInfo = model.NewStructureTagInfo()
		td.tagInfo.StructureType = td.structType
	}
	td.tagInfo.StructPageNumber = pageNumber
}

func (t *Table) splitSubtables(i2 int, ctx2 DrawContext, totalWidth, height float64) map[int][2]*Table {
	row := t.cells[i2].row
	var subtableSplits []subtableSplit
	ok2 := false
	for i := i2; i < len(t.cells) && t.cells[i].row == row; i++ {
		cell := t.cells[i]
		table, ok := cell.content.(*Table)
		if !ok {
			continue
		}
		width := cell.width(t.colWidths, totalWidth)
		height2 := cell.height(width)
		if !table.enablePageWrap && height2 <= height {
			continue
		}
		if height2 > ctx2.Height {
			ok2 = true
		}
		ctx := ctx2
		ctx.Width = width - cell.indent
		subtableSplits = append(subtableSplits, subtableSplit{index: i, table: table, ctx: ctx, height: height})
	}
	if !ok2 {
		return nil
	}
	rows, ok := mergeSubtableSplits(subtableSplits)
	if !ok {
		return nil
	}
	if allFirstPartsEmpty(rows) {
		return nil
	}
	m := make(map[int][2]*Table, len(subtableSplits))
	for i, subtableSplit := range subtableSplits {
		m[subtableSplit.index] = rows[i]
	}
	return m
}

// ToRGB implements interface Color.
// Note: It's not directly used since shading color works differently than regular color.
func (fbs *FunctionBasedShading) ToRGB() (float64, float64, float64) { return 0, 0, 0 }

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

func (tp *templateProcessor) walk(activeParent2 *templateNode, attrs map[string]string, ok2 bool) error {
	if activeParent2.start.Name.Local == styleElementName {
		return nil
	}
	activeParent2.attrs = attrs
	templateTag, ok := templateTags[activeParent2.start.Name.Local]
	if !ok {
		if tp.options.Strict {
			return tp.nodeError(activeParent2, "unknown tag <%s>", activeParent2.start.Name.Local)
		}
		tp.nodeLogDebug(activeParent2, "Unsupported template tag <%s>. Skipping. Output may be incorrect.", activeParent2.start.Name.Local)
	} else {
		effectiveParentName := tp.effectiveParentName(activeParent2)
		s, ok3 := effectiveParentName, false
		if _, ok := templateTag.allowedParents[effectiveParentName]; ok {
			ok3 = true
			if activeParent2.start.Name.Local == "page-break" {
				if pageBreakInvalidContext := tp.pageBreakInvalidContext(activeParent2); pageBreakInvalidContext != "" {
					ok3, s = false, pageBreakInvalidContext
				}
			}
		}
		if !ok3 {
			if tp.options.Strict {
				return tp.nodeError(activeParent2, "tag <%s> is not valid inside <%s>", activeParent2.start.Name.Local, s)
			}
			tp.nodeLogDebug(activeParent2, "Tag <%s> is not valid inside <%s>. Skipping.", activeParent2.start.Name.Local, s)
		} else {
			if templateTag2 := templateTags[effectiveParentName]; templateTag2 != nil && templateTag2.singleChild && !templateTag.allowMultipleChildren {
				if activeParent := activeParent2.activeParent(); !activeParent.hasChild {
					activeParent.hasChild = true
				} else {
					if tp.options.Strict {
						return tp.nodeError(activeParent2, "tag <%s> takes a single child; wrap multiple components in <division>", effectiveParentName)
					}
					tp.nodeLogError(activeParent2, "tag <%s> takes a single child; wrap multiple components in <division>. Skipping <%s>.", effectiveParentName, activeParent2.start.Name.Local)
					ok2 = true
				}
			}
			if ok2 {
				activeParent2.hasHandler = templateTag.parse != nil
				if err := tp.applyAttrs(activeParent2, nil, templateTag.attrs); err != nil {
					return err
				}
				if err := tp.validateLink(activeParent2); err != nil {
					return err
				}
				if activeParent2.start.Name.Local == "table" {
					if err := tp.validateTable(activeParent2); err != nil {
						return err
					}
				}
			} else if parse := templateTag.parse; parse != nil {
				value, err := parse(tp, activeParent2)
				if err != nil {
					return err
				}
				activeParent2.value = value
				activeParent2.hasHandler = value != nil
			}
		}
	}
	childStyle := tp.childStyle(activeParent2)
	activeParent2.textAttrs = childStyle
	n, n2 := -1, -1
	if activeParent2.hasHandler && activeParent2.start.Name.Local == "paragraph" {
		n, n2 = firstLastTextChild(activeParent2.childValues)
	}
	for i, childValue := range activeParent2.childValues {
		switch childValue2 := childValue.(type) {
		case *templateNode:
			if err := tp.walk(childValue2, childStyle, ok2); err != nil {
				return err
			}
		case *templateText:
			activeParent := activeParent2
			if !activeParent.hasHandler {
				activeParent = activeParent.activeParent()
			}
			if activeParent == nil {
				if !isBlank(childValue2.text) {
					tp.nodeLogDebug(childValue2.pos(), "Text content inside <%s> has no enclosing tag that takes it. Skipping.", activeParent2.start.Name.Local)
				}
				continue
			}
			text := childValue2.text
			if activeParent.start.Name.Local == "paragraph" {
				ok := activeParent == activeParent2
				text = collapseWhitespace(text, ok && (n < 0 || i <= n), ok && (n2 < 0 || i >= n2))
			}
			ok, err := tp.checkNodeText(activeParent, text, childValue2)
			if err != nil {
				return err
			}
			if ok && !ok2 {
				if err := tp.addNodeText(activeParent, text, childValue2); err != nil {
					return err
				}
			}
		}
	}
	if !ok2 && activeParent2.value != nil {
		if table, ok := activeParent2.value.(*Table); ok {
			tp.clampTableHeaderRows(activeParent2, table)
		}
		if err := tp.renderNode(activeParent2); err != nil {
			return err
		}
	}
	return nil
}

// SetSellerAddress sets the seller address of the invoice.
func (i *Invoice) SetSellerAddress(address *InvoiceAddress) { i.sellerAddress = address }

func (grc grayColor) ToRGB() (float64, float64, float64) {
	return grc.gray, grc.gray, grc.gray
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

func (tp *templateProcessor) clampTableHeaderRows(node *templateNode, table *Table) {
	if !table.hasHeaderRows || table.headerEndRow <= table.rows {
		return
	}
	tp.nodeLogDebug(node, "Table header rows %d-%d exceed the table's %d rows.", table.headerStartRow, table.headerEndRow, table.rows)
	if table.headerStartRow > table.rows {
		table.hasHeaderRows = false
		table.headerStartRow, table.headerEndRow = 0, 0
	} else {
		table.headerEndRow = table.rows
	}
}

func (tp *templateProcessor) effectiveAttrs(node *templateNode, m map[string]attrApplyFunc) []xml.Attr {
	var attrs []xml.Attr
	if _, ok := styledTextTags[node.start.Name.Local]; ok {
		for attrKey, attr := range node.attrs {
			if _, ok := m[attrKey]; ok {
				attrs = append(attrs, xml.Attr{Name: xml.Name{Local: attrKey}, Value: attr})
			}
		}
	}
	for _, s := range node.classNames() {
		attrs = append(attrs, tp.classStyles[s]...)
	}
	for _, attr := range node.start.Attr {
		if attr.Name.Space == "" && attr.Name.Local == classAttrName {
			continue
		}
		attrs = append(attrs, attr)
	}
	return attrs
}

// ToContentCreator convert SVG and add elements contentstream then returns `contentstream.ContentCreator`.
func (gse *GraphicSVGElement) ToContentCreator(cc *contentstream.ContentCreator, res *model.PdfPageResources, scaleX, scaleY, translateX, translateY float64) *contentstream.ContentCreator {
	if gse.Name == "svg" {
		gse.SetScaling(scaleX, scaleY)
		cc.Add_cm(1, 0, 0, 1, translateX, translateY)
		gse.setDefaultScaling(gse.scale)
		cc.Add_W()
		cc.Add_n()
		gse.processDefs()
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.ViewBox = gse.ViewBox
			graphicSVGElement.linearShadings = gse.linearShadings
			graphicSVGElement.radialShadings = gse.radialShadings
			graphicSVGElement.toContentStream(cc, res)
		}
		return cc
	}
	return nil
}
