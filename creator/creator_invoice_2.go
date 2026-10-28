package creator

func (i *Invoice) generateInformationBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	styledParagraph := newStyledParagraph(i.defaultStyle)
	styledParagraph.SetMargins(0, 0, 0, 20)
	address := i.drawAddress(i.sellerAddress)
	address = append(address, styledParagraph)
	address = append(address, i.drawAddress(i.buyerAddress)...)
	division := newDivision()
	for _, d := range address {
		division.Add(d)
	}
	information := i.drawInformation()
	table := newTable(2)
	table.SetMargins(0, 0, 25, 0)
	cell := table.NewCell()
	cell.SetIndent(0)
	cell.SetContent(division)
	cell = table.NewCell()
	cell.SetContent(information)
	return table.GeneratePageBlocks(ctx)
}

// SellerAddress returns the seller address used in the invoice template.
func (i *Invoice) SellerAddress() *InvoiceAddress { return i.sellerAddress }

// NoteStyle returns the style properties used to render the content of the
// invoice note sections.
func (i *Invoice) NoteStyle() TextStyle { return i.noteStyle }

// Total returns the invoice total description and value cells.
// The returned values can be used to customize the styles of the cells.
func (i *Invoice) Total() (*InvoiceCell, *InvoiceCell) { return i.total[0], i.total[1] }

// SetDate sets the date of the invoice.
func (i *Invoice) SetDate(date string) (*InvoiceCell, *InvoiceCell) {
	i.date[1].Value = date
	return i.date[0], i.date[1]
}

// SetSubtotal sets the subtotal of the invoice.
func (i *Invoice) SetSubtotal(value string) { i.subtotal[1].Value = value }

func (i *Invoice) generateHeaderBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	styledParagraph := newStyledParagraph(i.titleStyle)
	styledParagraph.SetEnableWrap(true)
	styledParagraph.Append(i.title)
	table := newTable(2)
	if i.logo != nil {
		cell := table.NewCell()
		cell.SetHorizontalAlignment(CellHorizontalAlignmentLeft)
		cell.SetVerticalAlignment(CellVerticalAlignmentMiddle)
		cell.SetIndent(0)
		cell.SetContent(i.logo)
		i.logo.ScaleToHeight(styledParagraph.Height() + 20)
	} else {
		table.SkipCells(1)
	}
	cell := table.NewCell()
	cell.SetHorizontalAlignment(CellHorizontalAlignmentRight)
	cell.SetVerticalAlignment(CellVerticalAlignmentMiddle)
	cell.SetContent(styledParagraph)
	return table.GeneratePageBlocks(ctx)
}

// NewColumn returns a new column for the line items invoice table.
func (i *Invoice) NewColumn(description string) *InvoiceCell {
	return i.newColumn(description, CellHorizontalAlignmentLeft)
}

// AddSection adds a new content section at the end of the invoice.
func (i *Invoice) AddSection(title, content string) {
	i.sections = append(i.sections, [2]string{title, content})
}

// SetAddressStyle sets the style properties used to render the content of
// the invoice address sections.
func (i *Invoice) SetAddressStyle(style TextStyle) { i.addressStyle = style }

// SetNumber sets the number of the invoice.
func (i *Invoice) SetNumber(number string) (*InvoiceCell, *InvoiceCell) {
	i.number[1].Value = number
	return i.number[0], i.number[1]
}

// InsertColumn inserts a column in the line items table at the specified index.
func (i *Invoice) InsertColumn(index uint, description string) *InvoiceCell {
	columnsLen := uint(len(i.columns))
	if index > columnsLen {
		index = columnsLen
	}
	column := i.NewColumn(description)
	i.columns = append(i.columns[:index], append([]*InvoiceCell{column}, i.columns[index:]...)...)
	return column
}

// SetTitle sets the title of the invoice.
func (i *Invoice) SetTitle(title string) { i.title = title }

// Number returns the invoice number description and value cells.
// The returned values can be used to customize the styles of the cells.
func (i *Invoice) Number() (*InvoiceCell, *InvoiceCell) {
	return i.number[0], i.number[1]
}

// Subtotal returns the invoice subtotal description and value cells.
// The returned values can be used to customize the styles of the cells.
func (i *Invoice) Subtotal() (*InvoiceCell, *InvoiceCell) {
	return i.subtotal[0], i.subtotal[1]
}

// NewCellProps returns the default properties of an invoice cell.
func (i *Invoice) NewCellProps() InvoiceCellProps {
	colorRGBFrom8bit := ColorRGBFrom8bit(255, 255, 255)
	return InvoiceCellProps{TextStyle: i.defaultStyle, Alignment: CellHorizontalAlignmentLeft, BackgroundColor: colorRGBFrom8bit, BorderColor: colorRGBFrom8bit, BorderWidth: 1, BorderSides: []CellBorderSide{CellBorderSideAll}}
}
