package creator

import (
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

// SetBuyerAddress sets the buyer address of the invoice.
func (i *Invoice) SetBuyerAddress(address *InvoiceAddress) { i.buyerAddress = address }

func (i *Invoice) drawAddress(invoiceAddress *InvoiceAddress) []*StyledParagraph {
	var paras []*StyledParagraph
	if invoiceAddress.Heading != "" {
		styledParagraph := newStyledParagraph(i.addressHeadingStyle)
		styledParagraph.SetMargins(0, 0, 0, 7)
		styledParagraph.Append(invoiceAddress.Heading)
		paras = append(paras, styledParagraph)
	}
	styledParagraph := newStyledParagraph(i.addressStyle)
	styledParagraph.SetLineHeight(1.2)
	separator := invoiceAddress.Separator
	if separator == "" {
		separator = i.addressSeparator
	}
	city := invoiceAddress.City
	if invoiceAddress.State != "" {
		if city != "" {
			city += separator
		}
		city += invoiceAddress.State
	}
	if invoiceAddress.Zip != "" {
		if city != "" {
			city += separator
		}
		city += invoiceAddress.Zip
	}
	if invoiceAddress.Name != "" {
		styledParagraph.Append(invoiceAddress.Name + "\n")
	}
	if invoiceAddress.Street != "" {
		styledParagraph.Append(invoiceAddress.Street + "\n")
	}
	if invoiceAddress.Street2 != "" {
		styledParagraph.Append(invoiceAddress.Street2 + "\n")
	}
	if city != "" {
		styledParagraph.Append(city + "\n")
	}
	if invoiceAddress.Country != "" {
		styledParagraph.Append(invoiceAddress.Country + "\n")
	}
	styledParagraph2 := newStyledParagraph(i.addressStyle)
	styledParagraph2.SetLineHeight(1.2)
	styledParagraph2.SetMargins(0, 0, 7, 0)
	if invoiceAddress.Phone != "" {
		styledParagraph2.Append(invoiceAddress.fmtLine(invoiceAddress.Phone, "Phone: ", invoiceAddress.HidePhoneLabel))
	}
	if invoiceAddress.Email != "" {
		styledParagraph2.Append(invoiceAddress.fmtLine(invoiceAddress.Email, "Email: ", invoiceAddress.HideEmailLabel))
	}
	paras = append(paras, styledParagraph, styledParagraph2)
	return paras
}

// SetDueDate sets the due date of the invoice.
func (i *Invoice) SetDueDate(dueDate string) (*InvoiceCell, *InvoiceCell) {
	i.dueDate[1].Value = dueDate
	return i.dueDate[0], i.dueDate[1]
}

// Title returns the title of the invoice.
func (i *Invoice) Title() string { return i.title }

func (i *Invoice) drawInformation() *Table {
	table := newTable(2)
	rows := append([][2]*InvoiceCell{i.number, i.date, i.dueDate}, i.infoLines...)
	for _, row := range rows {
		invoiceCell, invoiceCell2 := row[0], row[1]
		if invoiceCell2.Value == "" {
			continue
		}
		cell := table.NewCell()
		cell.SetBackgroundColor(invoiceCell.BackgroundColor)
		i.setCellBorder(cell, invoiceCell)
		styledParagraph := newStyledParagraph(invoiceCell.TextStyle)
		styledParagraph.Append(invoiceCell.Value)
		styledParagraph.SetMargins(0, 0, 2, 1)
		cell.SetContent(styledParagraph)
		cell = table.NewCell()
		cell.SetBackgroundColor(invoiceCell2.BackgroundColor)
		i.setCellBorder(cell, invoiceCell2)
		styledParagraph = newStyledParagraph(invoiceCell2.TextStyle)
		styledParagraph.Append(invoiceCell2.Value)
		styledParagraph.SetMargins(0, 0, 2, 1)
		cell.SetContent(styledParagraph)
	}
	return table
}

// AddInfo is used to append a piece of invoice information in the template
// information table.
func (i *Invoice) AddInfo(description, value string) (*InvoiceCell, *InvoiceCell) {
	invoiceCells := [2]*InvoiceCell{i.newCell(description, i.defaultCellProps), i.newCell(value, i.defaultCellProps)}
	i.infoLines = append(i.infoLines, invoiceCells)
	return invoiceCells[0], invoiceCells[1]
}

// AddLine appends a new line to the invoice line items table.
func (i *Invoice) AddLine(values ...string) []*InvoiceCell {
	columnsLen := len(i.columns)
	var invoiceCells []*InvoiceCell
	for j, value := range values {
		cell := i.newCell(value, i.lineProps)
		if j < columnsLen {
			cell.Alignment = i.columns[j].Alignment
		}
		invoiceCells = append(invoiceCells, cell)
	}
	i.lines = append(i.lines, invoiceCells)
	return invoiceCells
}

func (i *Invoice) drawSection(text, text2 string) []*StyledParagraph {
	var paras []*StyledParagraph
	if text != "" {
		styledParagraph := newStyledParagraph(i.noteHeadingStyle)
		styledParagraph.SetMargins(0, 0, 0, 5)
		styledParagraph.Append(text)
		paras = append(paras, styledParagraph)
	}
	if text2 != "" {
		styledParagraph := newStyledParagraph(i.noteStyle)
		styledParagraph.Append(text2)
		paras = append(paras, styledParagraph)
	}
	return paras
}

func (i *Invoice) newColumn(value string, alignment CellHorizontalAlignment) *InvoiceCell {
	invoiceCell := &InvoiceCell{i.headingCellProps, value}
	invoiceCell.Alignment = alignment
	return invoiceCell
}

// Logo returns the logo of the invoice.
func (i *Invoice) Logo() *Image { return i.logo }

// AddressHeadingStyle returns the style properties used to render the
// heading of the invoice address sections.
func (i *Invoice) AddressHeadingStyle() TextStyle { return i.headingStyle }

// Date returns the invoice date description and value cells.
// The returned values can be used to customize the styles of the cells.
func (i *Invoice) Date() (*InvoiceCell, *InvoiceCell) { return i.date[0], i.date[1] }

func (i *Invoice) setCellBorder(cell *TableCell, invoiceCell *InvoiceCell) {
	for _, borderSide := range invoiceCell.BorderSides {
		cell.SetBorder(borderSide, CellBorderStyleSingle, invoiceCell.BorderWidth)
	}
	cell.SetBorderColor(invoiceCell.BorderColor)
}

// DueDate returns the invoice due date description and value cells.
// The returned values can be used to customize the styles of the cells.
func (i *Invoice) DueDate() (*InvoiceCell, *InvoiceCell) {
	return i.dueDate[0], i.dueDate[1]
}

func (i *Invoice) SetStructPageNumber(pageNumber *int64) {}

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

// SetNotes sets the notes section of the invoice.
func (i *Invoice) SetNotes(title, content string) { i.notes = [2]string{title, content} }

// NoteHeadingStyle returns the style properties used to render the heading of
// the invoice note sections.
func (i *Invoice) NoteHeadingStyle() TextStyle { return i.noteHeadingStyle }

// SetColumns overwrites any columns in the line items table. This should be
// called before AddLine.
func (i *Invoice) SetColumns(cols []*InvoiceCell) { i.columns = cols }

// AddressStyle returns the style properties used to render the content of
// the invoice address sections.
func (i *Invoice) AddressStyle() TextStyle { return i.addressStyle }

// GeneratePageBlocks generate the Page blocks. Multiple blocks are generated
// if the contents wrap over multiple pages.
func (i *Invoice) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	ctx2 := ctx
	fns := []func(ctx DrawContext) ([]*Block, DrawContext, error){i.generateHeaderBlocks, i.generateInformationBlocks, i.generateLineBlocks, i.generateTotalBlocks, i.generateNoteBlocks}
	var blocks []*Block
	for _, fn := range fns {
		fn2, ctx2, err := fn(ctx)
		if err != nil {
			return blocks, ctx, err
		}
		if len(blocks) == 0 {
			blocks = fn2
		} else if len(fn2) > 0 {
			blocks[len(blocks)-1].mergeBlocks(fn2[0])
			blocks = append(blocks, fn2[1:]...)
		}
		ctx = ctx2
	}
	if i.positioning.IsRelative() {
		ctx.X = ctx2.X
	}
	if i.positioning.IsAbsolute() {
		return blocks, ctx2, nil
	}
	return blocks, ctx, nil
}

// SetNoteHeadingStyle sets the style properties used to render the heading
// of the invoice note sections.
func (i *Invoice) SetNoteHeadingStyle(style TextStyle) { i.noteHeadingStyle = style }

// SetTitleStyle sets the style properties of the invoice title.
func (i *Invoice) SetTitleStyle(style TextStyle) { i.titleStyle = style }

// SetNoteStyle sets the style properties used to render the content of the
// invoice note sections.
func (i *Invoice) SetNoteStyle(style TextStyle) { i.noteStyle = style }

// AppendColumn appends a column to the line items table.
func (i *Invoice) AppendColumn(description string) *InvoiceCell {
	column := i.NewColumn(description)
	i.columns = append(i.columns, column)
	return column
}

// AddTotalLine adds a new line in the invoice totals table.
func (i *Invoice) AddTotalLine(desc, value string) (*InvoiceCell, *InvoiceCell) {
	invoiceCell := &InvoiceCell{i.totalCellProps, desc}
	invoiceCell2 := &InvoiceCell{i.totalCellProps, value}
	i.totalLines = append(i.totalLines, [2]*InvoiceCell{invoiceCell, invoiceCell2})
	return invoiceCell, invoiceCell2
}

func (i *Invoice) GenerateKDict() (*model.KDict, error) { return nil, nil }

// SetLogo sets the logo of the invoice.
func (i *Invoice) SetLogo(logo *Image) { i.logo = logo }

// Sections returns the custom content sections of the invoice as
// title-content pairs.
func (i *Invoice) Sections() [][2]string { return i.sections }

// TitleStyle returns the style properties used to render the invoice title.
func (i *Invoice) TitleStyle() TextStyle { return i.titleStyle }

// TotalLines returns all the rows in the invoice totals table as
// description-value cell pairs.
func (i *Invoice) TotalLines() [][2]*InvoiceCell {
	rows := [][2]*InvoiceCell{i.subtotal}
	rows = append(rows, i.totalLines...)
	return append(rows, i.total)
}

// InfoLines returns all the rows in the invoice information table as
// description-value cell pairs.
func (i *Invoice) InfoLines() [][2]*InvoiceCell {
	rows := [][2]*InvoiceCell{i.number, i.date, i.dueDate}
	return append(rows, i.infoLines...)
}

// Notes returns the notes section of the invoice as a title-content pair.
func (i *Invoice) Notes() (string, string) { return i.notes[0], i.notes[1] }

// SetSellerAddress sets the seller address of the invoice.
func (i *Invoice) SetSellerAddress(address *InvoiceAddress) { i.sellerAddress = address }

func (i *Invoice) SetStructureType(structureType model.StructureType) {}

// Terms returns the terms and conditions section of the invoice as a
// title-content pair.
func (i *Invoice) Terms() (string, string) { return i.terms[0], i.terms[1] }

// BuyerAddress returns the buyer address used in the invoice template.
func (i *Invoice) BuyerAddress() *InvoiceAddress { return i.buyerAddress }

func (i *Invoice) generateLineBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	table := newTable(len(i.columns))
	table.SetMargins(0, 0, 25, 0)
	for _, column := range i.columns {
		styledParagraph := newStyledParagraph(column.TextStyle)
		styledParagraph.SetMargins(0, 0, 1, 0)
		styledParagraph.Append(column.Value)
		cell := table.NewCell()
		cell.SetHorizontalAlignment(column.Alignment)
		cell.SetBackgroundColor(column.BackgroundColor)
		i.setCellBorder(cell, column)
		cell.SetContent(styledParagraph)
	}
	for _, line := range i.lines {
		for _, invoiceCell := range line {
			styledParagraph := newStyledParagraph(invoiceCell.TextStyle)
			styledParagraph.SetMargins(0, 0, 3, 2)
			styledParagraph.Append(invoiceCell.Value)
			cell := table.NewCell()
			cell.SetHorizontalAlignment(invoiceCell.Alignment)
			cell.SetBackgroundColor(invoiceCell.BackgroundColor)
			i.setCellBorder(cell, invoiceCell)
			cell.SetContent(styledParagraph)
		}
	}
	return table.GeneratePageBlocks(ctx)
}

// SetTerms sets the terms and conditions section of the invoice.
func (i *Invoice) SetTerms(title, content string) { i.terms = [2]string{title, content} }

// NewCell returns a new invoice table cell.
func (i *Invoice) NewCell(value string) *InvoiceCell {
	return i.newCell(value, i.NewCellProps())
}

// Lines returns all the rows of the invoice line items table.
func (i *Invoice) Lines() [][]*InvoiceCell { return i.lines }

func (i *Invoice) newCell(value string, invoiceCellProps InvoiceCellProps) *InvoiceCell {
	return &InvoiceCell{invoiceCellProps, value}
}

// SetAddressHeadingStyle sets the style properties used to render the
// heading of the invoice address sections.
func (i *Invoice) SetAddressHeadingStyle(style TextStyle) { i.addressHeadingStyle = style }

// Columns returns all the columns in the invoice line items table.
func (i *Invoice) Columns() []*InvoiceCell { return i.columns }

// SetTotal sets the total of the invoice.
func (i *Invoice) SetTotal(value string) { i.total[1].Value = value }

// SetMarkedContentID sets marked content ID.
func (i *Invoice) SetMarkedContentID(id int64) {}

func (i *Invoice) generateNoteBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	division := newDivision()
	rows := append([][2]string{i.notes, i.terms}, i.sections...)
	for _, row := range rows {
		if row[1] != "" {
			section := i.drawSection(row[0], row[1])
			for _, d := range section {
				division.Add(d)
			}
			styledParagraph := newStyledParagraph(i.defaultStyle)
			styledParagraph.SetMargins(0, 0, 10, 0)
			division.Add(styledParagraph)
		}
	}
	return division.GeneratePageBlocks(ctx)
}

func (i *Invoice) generateTotalBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	table := newTable(4)
	table.SetMargins(0, 0, 10, 10)
	rows := [][2]*InvoiceCell{i.subtotal}
	rows = append(rows, i.totalLines...)
	rows = append(rows, i.total)
	for _, row := range rows {
		invoiceCell, invoiceCell2 := row[0], row[1]
		if invoiceCell2.Value == "" {
			continue
		}
		table.SkipCells(2)
		cell := table.NewCell()
		cell.SetBackgroundColor(invoiceCell.BackgroundColor)
		cell.SetHorizontalAlignment(invoiceCell2.Alignment)
		i.setCellBorder(cell, invoiceCell)
		styledParagraph := newStyledParagraph(invoiceCell.TextStyle)
		styledParagraph.SetMargins(0, 0, 2, 1)
		styledParagraph.Append(invoiceCell.Value)
		cell.SetContent(styledParagraph)
		cell = table.NewCell()
		cell.SetBackgroundColor(invoiceCell2.BackgroundColor)
		cell.SetHorizontalAlignment(invoiceCell2.Alignment)
		i.setCellBorder(cell, invoiceCell)
		styledParagraph = newStyledParagraph(invoiceCell2.TextStyle)
		styledParagraph.SetMargins(0, 0, 2, 1)
		styledParagraph.Append(invoiceCell2.Value)
		cell.SetContent(styledParagraph)
	}
	return table.GeneratePageBlocks(ctx)
}
