package model

import (
	"errors"
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
)

func newPdfCIDFontType0FromDict(dict *core.PdfObjectDictionary, fc *fontCommon) (*pdfCIDFontType0, error) {
	if fc.subtype != "CIDFontType0" {
		common.Log.Debug("ERROR: Font SubType != CIDFontType0. font=%s", fc)
		return nil, core.ErrRangeError
	}
	cidFontType0 := newPdfCIDFontType0FromCommon(fc)
	cidSystemInfo, ok := core.GetDict(dict.Get("CIDSystemInfo"))
	if !ok {
		common.Log.Debug("ERROR: CIDSystemInfo (Required) missing. font=%s", fc)
		return nil, ErrRequiredAttributeMissing
	}
	cidFontType0.CIDSystemInfo = cidSystemInfo
	cidFontType0.DW = dict.Get("DW")
	cidFontType0.W = dict.Get("W")
	cidFontType0.DW2 = dict.Get("DW2")
	cidFontType0.W2 = dict.Get("W2")
	cidFontType0.defaultWidth = 1000.0
	if f, err := core.GetNumberAsFloat(cidFontType0.DW); err == nil {
		cidFontType0.defaultWidth = f
	}
	cidWidths, err := parseCIDWidths(cidFontType0.W)
	if err != nil {
		return nil, err
	}
	if cidWidths == nil {
		cidWidths = map[textencoding.CharCode]float64{}
	}
	cidFontType0.charWidths = cidWidths
	return cidFontType0, nil
}

func (f *pdfCIDFontType2) getFontDescriptor() *PdfFontDescriptor { return f.fontDescriptor }

func newPdfCIDFontType2FromTtf(ttf fonts.TtfType, data []byte, otf *fonts.OtfType) (PdfFont, error) {
	font := &pdfCIDFontType2{fontCommon: fontCommon{subtype: "CIDFontType2"}, CIDToGIDMap: core.MakeName("Identity")}
	if len(ttf.Widths) <= 0 {
		return PdfFont{}, errors.New("ERROR: Missing required attribute (Widths)")
	}
	f := 1000.0 / float64(ttf.UnitsPerEm)
	v := f * float64(ttf.Widths[0])
	table, numByV := scaleGlyphWidths(ttf.Chars, ttf.Widths, ttf.UnitsPerEm)
	font.runeToGID = table
	font.DW = core.MakeInteger(int64(v))
	v2 := fonts.GID(len(ttf.Widths))
	widthsArray := buildWidthsArray(numByV, uint16(v2))
	font.W = core.MakeIndirectObject(widthsArray)
	dict := core.MakeDict()
	dict.Set("Ordering", core.MakeString("Identity"))
	dict.Set("Registry", core.MakeString("Adobe"))
	dict.Set("Supplement", core.MakeInteger(0))
	font.CIDSystemInfo = dict
	fontDescriptor := newPdfFontDescriptorFromTtf(&ttf, v, fontFlagSymbolic)
	stream, err := core.MakeStream(data, core.NewFlateEncoder())
	if err != nil {
		common.Log.Debug("ERROR: Unable to make stream: %v", err)
		return PdfFont{}, err
	}
	stream.PdfObjectDictionary.Set("Length1", core.MakeInteger(int64(len(data))))
	fontDescriptor.FontFile2 = stream
	font.baseFont = ttf.PostScriptName
	font.fontDescriptor = fontDescriptor
	var enc textencoding.TextEncoder
	if otf != nil {
		font.SetOtfType(otf)
		enc = otf.NewEncoder()
	} else {
		enc = ttf.NewEncoder()
	}
	font2 := pdfFontType0{fontCommon: fontCommon{subtype: "Type0", baseFont: ttf.PostScriptName}, DescendantFont: &PdfFont{context: font}, Encoding: core.MakeName("Identity-H"), encoder: enc}
	if len(ttf.Chars) > 0 {
		textByCode := make(map[cmap.CharCode]string, len(ttf.Chars))
		for n, char := range ttf.Chars {
			code := cmap.CharCode(char)
			s := string(n)
			if s2, ok := textByCode[code]; !ok || (ok && len(s2) > 0 && s2 > s) {
				textByCode[code] = s
			}
		}
		font2.toUnicodeCMap = cmap.NewToUnicodeCMap(textByCode)
	}
	font3 := PdfFont{context: &font2}
	return font3, nil
}

type pdfCIDFontType2 struct {
	fontCommon
	primitive *core.PdfIndirectObject
	encoder   textencoding.TextEncoder

	// Table 117 – Entries in a CIDFont dictionary (page 269)
	// Dictionary that defines the character collection of the CIDFont (required).
	// See Table 116.
	CIDSystemInfo *core.PdfObjectDictionary

	// Glyph metrics fields (optional).
	DW  core.PdfObject
	W   core.PdfObject
	DW2 core.PdfObject
	W2  core.PdfObject

	// CIDs to glyph indices mapping (optional).
	CIDToGIDMap  core.PdfObject
	charWidths   map[textencoding.CharCode]float64
	defaultWidth float64
	runeToGID    map[rune]int
	otfType      *fonts.OtfType
}

func (f *pdfCIDFontType2) baseFields() *fontCommon { return &f.fontCommon }

// Encoder returns the font's text encoder.
func (f pdfCIDFontType0) Encoder() textencoding.TextEncoder { return f.encoder }

func (f *pdfCIDFontType0) getFontDescriptor() *PdfFontDescriptor { return f.fontDescriptor }

// GetCharMetrics returns the char metrics for character code `code`.
func (f pdfCIDFontType0) GetCharMetrics(code textencoding.CharCode) (fonts.CharMetrics, bool) {
	defaultWidth := f.defaultWidth
	if charWidth, ok := f.charWidths[code]; ok {
		defaultWidth = charWidth
	}
	return fonts.CharMetrics{Wx: defaultWidth}, true
}

// ToPdfObject converts the pdfCIDFontType0 to a PDF representation.
func (f *pdfCIDFontType0) ToPdfObject() core.PdfObject {
	if f.primitive == nil {
		f.primitive = &core.PdfIndirectObject{}
	}
	dict := f.baseFields().asPdfObjectDictionary("CIDFontType0")
	f.primitive.PdfObject = dict
	if f.CIDSystemInfo != nil {
		dict.Set("CIDSystemInfo", f.CIDSystemInfo)
	}
	if f.DW != nil {
		dict.Set("DW", f.DW)
	}
	if f.DW2 != nil {
		dict.Set("DW2", f.DW2)
	}
	if f.W != nil {
		dict.Set("W", f.W)
	}
	if f.W2 != nil {
		dict.Set("W2", f.W2)
	}
	if f.fontDescriptor != nil {
		dict.Set("FontDescriptor", f.fontDescriptor.ToPdfObject())
	}
	if f.CIDToGIDMap != nil {
		dict.Set("CIDToGIDMap", f.CIDToGIDMap)
	}
	return f.primitive
}

func newPdfCIDFontType0FromCommon(fc *fontCommon) *pdfCIDFontType0 {
	return &pdfCIDFontType0{fontCommon: *fc}
}

// Encoder returns the font's text encoder.
func (f pdfCIDFontType2) Encoder() textencoding.TextEncoder { return f.encoder }

func parseCIDWidths(obj core.PdfObject) (map[textencoding.CharCode]float64, error) {
	if obj == nil {
		return nil, nil
	}
	arr, ok := core.GetArray(obj)
	if !ok {
		return nil, nil
	}
	valByCode := map[textencoding.CharCode]float64{}
	arrLen := arr.Len()
	for i := 0; i < arrLen-1; i++ {
		obj := core.TraceToDirectObject(arr.Get(i))
		n, ok := core.GetIntVal(obj)
		if !ok {
			return nil, fmt.Errorf("bad font W obj0: i=%d %#v", i, obj)
		}
		i++
		if i > arrLen-1 {
			return nil, fmt.Errorf("bad font W array: arr2=%+v", arr)
		}
		obj2 := core.TraceToDirectObject(arr.Get(i))
		switch obj2.(type) {
		case *core.PdfObjectArray:
			arr, _ := core.GetArray(obj2)
			if floats, err := arr.ToFloat64Array(); err == nil {
				for i := 0; i < len(floats); i++ {
					valByCode[textencoding.CharCode(n+i)] = floats[i]
				}
			} else {
				return nil, fmt.Errorf("bad font W array obj1: i=%d %#v", i, obj2)
			}
		case *core.PdfObjectInteger:
			count, ok := core.GetIntVal(obj2)
			if !ok {
				return nil, fmt.Errorf("bad font W int obj1: i=%d %#v", i, obj2)
			}
			i++
			if i > arrLen-1 {
				return nil, fmt.Errorf("bad font W array: arr2=%+v", arr)
			}
			obj := arr.Get(i)
			f, err := core.GetNumberAsFloat(obj)
			if err != nil {
				return nil, fmt.Errorf("bad font W int obj2: i=%d %#v", i, obj)
			}
			for i := n; i <= count; i++ {
				valByCode[textencoding.CharCode(i)] = f
			}
		default:
			return nil, fmt.Errorf("bad font W obj1 type: i=%d %#v", i, obj2)
		}
	}
	return valByCode, nil
}

// GetRuneMetrics returns the character metrics for the specified rune.
// A bool flag is returned to indicate whether or not the entry was found.
func (f pdfCIDFontType2) GetRuneMetrics(r rune) (fonts.CharMetrics, bool) {
	runeToGID, ok := f.runeToGID[r]
	if !ok {
		num, ok := core.GetInt(f.DW)
		if !ok {
			return fonts.CharMetrics{}, false
		}
		runeToGID = int(*num)
	}
	return fonts.CharMetrics{Wx: float64(runeToGID)}, true
}

// GetOtfType returns the OTF type information if available.
func (f *pdfCIDFontType2) GetOtfType() *fonts.OtfType { return f.otfType }

// ToPdfObject converts the pdfCIDFontType2 to a PDF representation.
func (f *pdfCIDFontType2) ToPdfObject() core.PdfObject {
	if f.primitive == nil {
		f.primitive = &core.PdfIndirectObject{}
	}
	dict := f.baseFields().asPdfObjectDictionary("CIDFontType2")
	f.primitive.PdfObject = dict
	if f.CIDSystemInfo != nil {
		dict.Set("CIDSystemInfo", f.CIDSystemInfo)
	}
	if f.DW != nil {
		dict.Set("DW", f.DW)
	}
	if f.DW2 != nil {
		dict.Set("DW2", f.DW2)
	}
	if f.W != nil {
		dict.Set("W", f.W)
	}
	if f.W2 != nil {
		dict.Set("W2", f.W2)
	}
	if f.CIDToGIDMap != nil {
		dict.Set("CIDToGIDMap", f.CIDToGIDMap)
	}
	return f.primitive
}

// GetCharMetrics returns the char metrics for character code `code`.
func (f pdfCIDFontType2) GetCharMetrics(code textencoding.CharCode) (fonts.CharMetrics, bool) {
	if charWidth, ok := f.charWidths[code]; ok {
		return fonts.CharMetrics{Wx: charWidth}, true
	}
	n := rune(code)
	runeToGID, ok := f.runeToGID[n]
	if !ok {
		runeToGID = int(f.defaultWidth)
	}
	return fonts.CharMetrics{Wx: float64(runeToGID)}, true
}

// SetOtfType sets the OTF type information.
func (f *pdfCIDFontType2) SetOtfType(otfType *fonts.OtfType) { f.otfType = otfType }

// GetOtfType returns the OTF type information if available
func (f *pdfCIDFontType0) GetOtfType() *fonts.OtfType { return f.otfType }

// GetRuneMetrics returns the character metrics for the specified rune.
// A bool flag is returned to indicate whether or not the entry was found.
func (f pdfCIDFontType0) GetRuneMetrics(r rune) (fonts.CharMetrics, bool) {
	runeToGID, ok := f.runeToGID[r]
	if !ok {
		num, ok := core.GetInt(f.DW)
		if !ok {
			return fonts.CharMetrics{}, false
		}
		runeToGID = int(*num)
	}
	return fonts.CharMetrics{Wx: float64(runeToGID)}, true
}

func cidToGIDMapFromObject(obj core.PdfObject) []uint16 {
	stream, ok := core.GetStream(obj)
	if !ok {
		return nil
	}
	data, err := core.DecodeStream(stream)
	if err != nil {
		common.Log.Debug("WARN: could not decode CIDToGIDMap stream: %v", err)
		return nil
	}
	if len(data) == 0 || len(data)%2 != 0 {
		common.Log.Debug("WARN: invalid CIDToGIDMap stream length %d; using Identity", len(data))
		return nil
	}
	n := min(len(data)/2, 65536)
	vals := make([]uint16, n)
	for i := 0; i < n; i++ {
		vals[i] = uint16(data[2*i])<<8 | uint16(data[2*i+1])
	}
	return vals
}

func newPdfCIDFontType2FromDict(dict *core.PdfObjectDictionary, fc *fontCommon) (*pdfCIDFontType2, error) {
	if fc.subtype != "CIDFontType2" {
		common.Log.Debug("ERROR: Font SubType != CIDFontType2. font=%s", fc)
		return nil, core.ErrRangeError
	}
	cidFontType2 := newPdfCIDFontType2FromCommon(fc)
	cidSystemInfo, ok := core.GetDict(dict.Get("CIDSystemInfo"))
	if !ok {
		common.Log.Debug("ERROR: CIDSystemInfo (Required) missing. font=%s", fc)
		return nil, ErrRequiredAttributeMissing
	}
	cidFontType2.CIDSystemInfo = cidSystemInfo
	cidFontType2.DW = dict.Get("DW")
	cidFontType2.W = dict.Get("W")
	cidFontType2.DW2 = dict.Get("DW2")
	cidFontType2.W2 = dict.Get("W2")
	cidFontType2.CIDToGIDMap = dict.Get("CIDToGIDMap")
	cidFontType2.defaultWidth = 1000.0
	if f, err := core.GetNumberAsFloat(cidFontType2.DW); err == nil {
		cidFontType2.defaultWidth = f
	}
	cidWidths, err := parseCIDWidths(cidFontType2.W)
	if err != nil {
		return nil, err
	}
	if cidWidths == nil {
		cidWidths = map[textencoding.CharCode]float64{}
	}
	cidFontType2.charWidths = cidWidths
	return cidFontType2, nil
}

type pdfCIDFontType0 struct {
	fontCommon
	primitive *core.PdfIndirectObject
	encoder   textencoding.TextEncoder

	// Table 117 – Entries in a CIDFont dictionary (page 269)
	// (Required) Dictionary that defines the character collection of the CIDFont.
	// See Table 116.
	CIDSystemInfo *core.PdfObjectDictionary

	// Glyph metrics fields (optional).
	DW  core.PdfObject
	W   core.PdfObject
	DW2 core.PdfObject
	W2  core.PdfObject

	// CIDs to glyph indices mapping (optional).
	CIDToGIDMap  core.PdfObject
	charWidths   map[textencoding.CharCode]float64
	defaultWidth float64
	runeToGID    map[rune]int
	otfType      *fonts.OtfType
}

func (f *pdfCIDFontType0) baseFields() *fontCommon { return &f.fontCommon }

func newPdfCIDFontType2FromCommon(fc *fontCommon) *pdfCIDFontType2 {
	return &pdfCIDFontType2{fontCommon: *fc}
}

// SetOtfType sets the OTF type information
func (f *pdfCIDFontType0) SetOtfType(otfType *fonts.OtfType) { f.otfType = otfType }
