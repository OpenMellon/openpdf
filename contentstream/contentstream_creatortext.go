package contentstream

import (
	"github.com/OpenMellon/openpdf/core"
)

// Add_Tz appends 'Tz' operand to the content stream:
// Set horizontal scaling.
//
// See section 9.3 "Text State Parameters and Operators" and
// Table 105 (pp. 251-252 PDF32000_2008).
func (cc *ContentCreator) Add_Tz(scale float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Tz"
	op.Params = floatsToObjects([]float64{scale})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_Tw appends 'Tw' operand to the content stream:
// Set word spacing.
//
// See section 9.3 "Text State Parameters and Operators" and
// Table 105 (pp. 251-252 PDF32000_2008).
func (cc *ContentCreator) Add_Tw(wordSpace float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Tw"
	op.Params = floatsToObjects([]float64{wordSpace})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_Do adds 'Do' operation to the content stream:
// Displays an XObject (image or form) specified by `name`.
//
// See section 8.8 "External Objects" and Table 87 (pp. 209-220 PDF32000_2008).
func (cc *ContentCreator) Add_Do(name core.PdfObjectName) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Do"
	op.Params = namesToObjects([]core.PdfObjectName{name})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_TD appends 'TD' operand to the content stream:
// Move to start of next line with offset (`tx`, `ty`).
//
// See section 9.4.2 "Text Positioning Operators" and
// Table 108 (pp. 257-258 PDF32000_2008).
func (cc *ContentCreator) Add_TD(tx, ty float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "TD"
	op.Params = floatsToObjects([]float64{tx, ty})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_quote appends "'" operand to the content stream:
// Move to next line and show a string.
//
// See section 9.4.3 "Text Showing Operators" and
// Table 209 (pp. 258-259 PDF32000_2008).
func (cc *ContentCreator) Add_quote(textstr core.PdfObjectString) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "'"
	op.Params = stringsToObjects([]core.PdfObjectString{textstr})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_Tm appends 'Tm' operand to the content stream:
// Set the text line matrix.
//
// See section 9.4.2 "Text Positioning Operators" and
// Table 108 (pp. 257-258 PDF32000_2008).
func (cc *ContentCreator) Add_Tm(a, b, c, d, e, f float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Tm"
	op.Params = floatsToObjects([]float64{a, b, c, d, e, f})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_Ts appends 'Ts' operand to the content stream:
// Set text rise.
//
// See section 9.3 "Text State Parameters and Operators" and
// Table 105 (pp. 251-252 PDF32000_2008).
func (cc *ContentCreator) Add_Ts(rise float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Ts"
	op.Params = floatsToObjects([]float64{rise})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_ET appends 'ET' operand to the content stream:
// End text.
//
// See section 9.4 "Text Objects" and Table 107 (p. 256 PDF32000_2008).
func (cc *ContentCreator) Add_ET() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "ET"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_quotes appends `"` operand to the content stream:
// Move to next line and show a string, using `aw` and `ac` as word
// and character spacing respectively.
//
// See section 9.4.3 "Text Showing Operators" and
// Table 209 (pp. 258-259 PDF32000_2008).
func (cc *ContentCreator) Add_quotes(textstr core.PdfObjectString, aw, ac float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "\""
	op.Params = floatsToObjects([]float64{aw, ac})
	op.Params = append(op.Params, stringsToObjects([]core.PdfObjectString{textstr})...)
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_TJ appends 'TJ' operand to the content stream:
// Show one or more text string. Array of numbers (displacement) and strings.
//
// See section 9.4.3 "Text Showing Operators" and
// Table 209 (pp. 258-259 PDF32000_2008).
func (cc *ContentCreator) Add_TJ(vals ...core.PdfObject) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "TJ"
	op.Params = []core.PdfObject{core.MakeArray(vals...)}
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_Tj appends 'Tj' operand to the content stream:
// Show a text string.
//
// See section 9.4.3 "Text Showing Operators" and
// Table 209 (pp. 258-259 PDF32000_2008).
func (cc *ContentCreator) Add_Tj(textstr core.PdfObjectString) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Tj"
	op.Params = stringsToObjects([]core.PdfObjectString{textstr})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_Td appends 'Td' operand to the content stream:
// Move to start of next line with offset (`tx`, `ty`).
//
// See section 9.4.2 "Text Positioning Operators" and
// Table 108 (pp. 257-258 PDF32000_2008).
func (cc *ContentCreator) Add_Td(tx, ty float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Td"
	op.Params = floatsToObjects([]float64{tx, ty})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_BDC appends 'BDC' operand to the content stream:
// Begins a marked-content sequence with an associated property list terminated by a balancing EMC operator.
// `tag` shall be a name object indicating the role or significance of
// the sequence.
// `propertyList` shall be a dictionary containing the properties of the
//
// See section 14.6 "Marked Content" and Table 320 (p. 561 PDF32000_2008).
func (cc *ContentCreator) Add_BDC(tag core.PdfObjectName, propertyList map[string]core.PdfObject) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "BDC"
	op.Params = namesToObjects([]core.PdfObjectName{tag})
	if len(propertyList) > 0 {
		op.Params = append(op.Params, core.MakeDictMap(propertyList))
	}
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_Tc appends 'Tc' operand to the content stream:
// Set character spacing.
//
// See section 9.3 "Text State Parameters and Operators" and
// Table 105 (pp. 251-252 PDF32000_2008).
func (cc *ContentCreator) Add_Tc(charSpace float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Tc"
	op.Params = floatsToObjects([]float64{charSpace})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_BMC appends 'BMC' operand to the content stream:
// Begins a marked-content sequence terminated by a balancing EMC operator.
// `tag` shall be a name object indicating the role or significance of
// the sequence.
//
// See section 14.6 "Marked Content" and Table 320 (p. 561 PDF32000_2008).
func (cc *ContentCreator) Add_BMC(tag core.PdfObjectName) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "BMC"
	op.Params = namesToObjects([]core.PdfObjectName{tag})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_BT appends 'BT' operand to the content stream:
// Begin text.
//
// See section 9.4 "Text Objects" and Table 107 (p. 256 PDF32000_2008).
func (cc *ContentCreator) Add_BT() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "BT"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_Tf appends 'Tf' operand to the content stream:
// Set font and font size specified by font resource `fontName` and `fontSize`.
//
// See section 9.3 "Text State Parameters and Operators" and
// Table 105 (pp. 251-252 PDF32000_2008).
func (cc *ContentCreator) Add_Tf(fontName core.PdfObjectName, fontSize float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Tf"
	op.Params = namesToObjects([]core.PdfObjectName{fontName})
	op.Params = append(op.Params, floatsToObjects([]float64{fontSize})...)
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_Tstar appends 'T*' operand to the content stream:
// Move to the start of next line.
//
// See section 9.4.2 "Text Positioning Operators" and
// Table 108 (pp. 257-258 PDF32000_2008).
func (cc *ContentCreator) Add_Tstar() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "T*"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_TL appends 'TL' operand to the content stream:
// Set leading.
//
// See section 9.3 "Text State Parameters and Operators" and
// Table 105 (pp. 251-252 PDF32000_2008).
func (cc *ContentCreator) Add_TL(leading float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "TL"
	op.Params = floatsToObjects([]float64{leading})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_Tr appends 'Tr' operand to the content stream:
// Set text rendering mode.
//
// See section 9.3 "Text State Parameters and Operators" and
// Table 105 (pp. 251-252 PDF32000_2008).
func (cc *ContentCreator) Add_Tr(render int64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Tr"
	op.Params = integersToObjects([]int64{render})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_EMC appends 'EMC' operand to the content stream:
// Ends a marked-content sequence.
//
// See section 14.6 "Marked Content" and Table 320 (p. 561 PDF32000_2008).
func (cc *ContentCreator) Add_EMC() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "EMC"
	cc.operations = append(cc.operations, &op)
	return cc
}
