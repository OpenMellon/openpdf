package contentstream

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// Add_K appends 'K' operand to the content stream:
// Set the stroking colorspace to DeviceCMYK and sets the c,m,y,k color (0-1 each component).
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_K(c, m, y, k float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "K"
	op.Params = floatsToObjects([]float64{c, m, y, k})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_rg appends 'rg' operand to the content stream:
// Same as RG but used for nonstroking operations.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_rg(r, g, b float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "rg"
	op.Params = floatsToObjects([]float64{r, g, b})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_k appends 'k' operand to the content stream:
// Same as K but used for nonstroking operations.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_k(c, m, y, k float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "k"
	op.Params = floatsToObjects([]float64{c, m, y, k})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_CS appends 'CS' operand to the content stream:
// Set the current colorspace for stroking operations.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_CS(name core.PdfObjectName) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "CS"
	op.Params = namesToObjects([]core.PdfObjectName{name})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_SC appends 'SC' operand to the content stream:
// Set color for stroking operations.  Input: c1, ..., cn.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_SC(c ...float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "SC"
	op.Params = floatsToObjects(c)
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_cs appends 'cs' operand to the content stream:
// Same as CS but for non-stroking operations.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_cs(name core.PdfObjectName) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "cs"
	op.Params = namesToObjects([]core.PdfObjectName{name})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_SCN appends 'SCN' operand to the content stream:
// Same as SC but supports more colorspaces.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_SCN(c ...float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "SCN"
	op.Params = floatsToObjects(c)
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_G appends 'G' operand to the content stream:
// Set the stroking colorspace to DeviceGray and sets the gray level (0-1).
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_G(gray float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "G"
	op.Params = floatsToObjects([]float64{gray})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_scn appends 'scn' operand to the content stream:
// Same as SC but for nonstroking operations.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_scn(c ...float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "scn"
	op.Params = floatsToObjects(c)
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_RG appends 'RG' operand to the content stream:
// Set the stroking colorspace to DeviceRGB and sets the r,g,b colors (0-1 each).
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_RG(r, g, b float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "RG"
	op.Params = floatsToObjects([]float64{r, g, b})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_g appends 'g' operand to the content stream:
// Same as G but used for nonstroking operations.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_g(gray float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "g"
	op.Params = floatsToObjects([]float64{gray})
	cc.operations = append(cc.operations, &op)
	return cc
}

// SetNonStrokingColor sets the non-stroking `color` where color can be one of
// PdfColorDeviceGray, PdfColorDeviceRGB, or PdfColorDeviceCMYK.
func (cc *ContentCreator) SetNonStrokingColor(color model.PdfColor) *ContentCreator {
	switch c := color.(type) {
	case *model.PdfColorDeviceGray:
		cc.Add_g(c.Val())
	case *model.PdfColorDeviceRGB:
		cc.Add_rg(c.R(), c.G(), c.B())
	case *model.PdfColorDeviceCMYK:
		cc.Add_k(c.C(), c.M(), c.Y(), c.K())
	case *model.PdfColorPatternType1:
		cc.Add_cs(*core.MakeName("Pattern"))
		cc.Add_scn_pattern(c.PatternName)
	case *model.PdfColorPatternType2:
		cc.Add_cs(*core.MakeName("Pattern"))
		cc.Add_scn_pattern(c.PatternName)
	case *model.PdfColorPatternType3:
		cc.Add_cs(*core.MakeName("Pattern"))
		cc.Add_scn_pattern(c.PatternName)
	default:
		common.Log.Debug("SetNonStrokingColor: unsupported color: %T", c)
	}
	return cc
}

// Add_SCN_pattern appends 'SCN' operand to the content stream for pattern `name`:
// SCN with name attribute (for pattern). Syntax: c1 ... cn name SCN.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_SCN_pattern(name core.PdfObjectName, c ...float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "SCN"
	op.Params = floatsToObjects(c)
	op.Params = append(op.Params, core.MakeName(string(name)))
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_scn_pattern appends 'scn' operand to the content stream for pattern `name`:
// scn with name attribute (for pattern). Syntax: c1 ... cn name scn.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_scn_pattern(name core.PdfObjectName, c ...float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "scn"
	op.Params = floatsToObjects(c)
	op.Params = append(op.Params, core.MakeName(string(name)))
	cc.operations = append(cc.operations, &op)
	return cc
}

// SetStrokingColor sets the stroking `color` where color can be one of
// PdfColorDeviceGray, PdfColorDeviceRGB, or PdfColorDeviceCMYK.
func (cc *ContentCreator) SetStrokingColor(color model.PdfColor) *ContentCreator {
	switch c := color.(type) {
	case *model.PdfColorDeviceGray:
		cc.Add_G(c.Val())
	case *model.PdfColorDeviceRGB:
		cc.Add_RG(c.R(), c.G(), c.B())
	case *model.PdfColorDeviceCMYK:
		cc.Add_K(c.C(), c.M(), c.Y(), c.K())
	case *model.PdfColorPatternType1:
		cc.Add_CS(*core.MakeName("Pattern"))
		cc.Add_SCN_pattern(c.PatternName)
	case *model.PdfColorPatternType2:
		cc.Add_CS(*core.MakeName("Pattern"))
		cc.Add_SCN_pattern(c.PatternName)
	case *model.PdfColorPatternType3:
		cc.Add_CS(*core.MakeName("Pattern"))
		cc.Add_SCN_pattern(c.PatternName)
	default:
		common.Log.Debug("SetStrokingColor: unsupported color: %T", c)
	}
	return cc
}

// Add_sh appends 'sh' operand to the content stream:
// Paints the shape and colour shading described by a shading dictionary specified by `name`,
// subject to the current clipping path
//
// See section 8.7.4 "Shading Patterns" and Table 77 (p. 190 PDF32000_2008).
func (cc *ContentCreator) Add_sh(name core.PdfObjectName) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "sh"
	op.Params = namesToObjects([]core.PdfObjectName{name})
	cc.operations = append(cc.operations, &op)
	return cc
}
