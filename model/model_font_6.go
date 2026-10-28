package model

import (
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
)

type pdfFontType3 struct {
	fontCommon
	primitive *core.PdfIndirectObject

	// These fields are specific to Type 3 fonts.
	CharProcs  core.PdfObject
	Encoding   core.PdfObject
	FontBBox   core.PdfObject
	FontMatrix core.PdfObject
	FirstChar  core.PdfObject
	LastChar   core.PdfObject
	Widths     core.PdfObject
	Resources  core.PdfObject
	charWidths map[textencoding.CharCode]float64
	encoder    textencoding.TextEncoder
}

func (f *pdfFontType3) baseFields() *fontCommon { return &f.fontCommon }

func newPdfFontType3FromDict(dict *core.PdfObjectDictionary, fc *fontCommon) (*pdfFontType3, error) {
	fontType3 := newPdfFontType3FromCommon(fc)
	firstChar := dict.Get("FirstChar")
	if firstChar == nil {
		firstChar = core.MakeInteger(0)
	}
	fontType3.FirstChar = firstChar
	n, ok := core.GetIntVal(firstChar)
	if !ok {
		common.Log.Debug("ERROR: Invalid FirstChar type (%T)", firstChar)
		return nil, core.ErrTypeError
	}
	code := textencoding.CharCode(n)
	firstChar = dict.Get("LastChar")
	if firstChar == nil {
		firstChar = core.MakeInteger(255)
	}
	fontType3.LastChar = firstChar
	n, ok = core.GetIntVal(firstChar)
	if !ok {
		common.Log.Debug("ERROR: Invalid LastChar type (%T)", firstChar)
		return nil, core.ErrTypeError
	}
	code2 := textencoding.CharCode(n)
	firstChar = dict.Get("Resources")
	if firstChar != nil {
		fontType3.Resources = firstChar
	}
	firstChar = dict.Get("CharProcs")
	if firstChar == nil {
		common.Log.Debug("ERROR: Invalid CharProcs (%v)", firstChar)
		return nil, core.ErrNotSupported
	}
	fontType3.CharProcs = firstChar
	firstChar = dict.Get("FontMatrix")
	if firstChar == nil {
		common.Log.Debug("ERROR: Invalid FontMatrix (%v)", firstChar)
		return nil, core.ErrNotSupported
	}
	fontType3.FontMatrix = firstChar
	fontType3.charWidths = make(map[textencoding.CharCode]float64)
	firstChar = dict.Get("Widths")
	if firstChar != nil {
		fontType3.Widths = firstChar
		firstCharArr, ok := core.GetArray(firstChar)
		if !ok {
			common.Log.Debug("ERROR: Widths attribute != array (%T)", firstChar)
			return nil, core.ErrTypeError
		}
		floats, err := firstCharArr.ToFloat64Array()
		if err != nil {
			common.Log.Debug("ERROR: converting widths to array")
			return nil, err
		}
		if len(floats) != int(code2-code+1) {
			common.Log.Debug("ERROR: Invalid widths length != %d (%d)", code2-code+1, len(floats))
			return nil, core.ErrRangeError
		}
		fontMatrixArr, ok := core.GetArray(fontType3.FontMatrix)
		if !ok {
			common.Log.Debug("ERROR: FontMatrix attribute != array (%T)", fontMatrixArr)
			return nil, err
		}
		floats2, err := fontMatrixArr.ToFloat64Array()
		if err != nil {
			common.Log.Debug("ERROR: converting FontMatrix to array")
			return nil, err
		}
		matrix := transform.NewMatrix(floats2[0], floats2[1], floats2[2], floats2[3], floats2[4], floats2[5])
		for i, float := range floats {
			f, _ := matrix.Transform(float, float)
			fontType3.charWidths[code+textencoding.CharCode(i)] = f
		}
	}
	fontType3.Encoding = core.TraceToDirectObject(dict.Get("Encoding"))
	toUnicode := dict.Get("ToUnicode")
	if toUnicode != nil {
		fontType3.toUnicode = core.TraceToDirectObject(toUnicode)
		cm, err := toUnicodeCMapFromObject(fontType3.toUnicode, &fontType3.fontCommon)
		if err != nil {
			return nil, err
		}
		fontType3.toUnicodeCMap = cm
	}
	if toUnicodeCMap := fontType3.toUnicodeCMap; toUnicodeCMap != nil {
		fontType3.encoder = textencoding.NewCMapEncoder("", nil, toUnicodeCMap)
	} else {
		fontType3.encoder = textencoding.NewPdfDocEncoder()
	}
	return fontType3, nil
}

func newPdfFontType3FromCommon(fc *fontCommon) *pdfFontType3 { return &pdfFontType3{fontCommon: *fc} }

// GetCharMetrics returns the char metrics for character code `code`.
func (f pdfFontType3) GetCharMetrics(code textencoding.CharCode) (fonts.CharMetrics, bool) {
	if charWidth, ok := f.charWidths[code]; ok {
		return fonts.CharMetrics{Wx: charWidth}, true
	}
	if fonts.IsStdFont(fonts.StdFontName(f.baseFont)) {
		return fonts.CharMetrics{Wx: 250}, true
	}
	return fonts.CharMetrics{}, false
}

// GetRuneMetrics returns the character metrics for the specified rune.
// A bool flag is returned to indicate whether or not the entry was found.
func (f pdfFontType3) GetRuneMetrics(r rune) (fonts.CharMetrics, bool) {
	encoder := f.Encoder()
	if encoder == nil {
		common.Log.Debug("No encoder for fonts=%s", f)
		return fonts.CharMetrics{}, false
	}
	runeToCharcode, ok := encoder.RuneToCharcode(r)
	if !ok {
		if r != ' ' {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("No charcode for rune=%v font=%s", r, f)
			}
		}
		return fonts.CharMetrics{}, false
	}
	charMetrics, isCharMetrics := f.GetCharMetrics(runeToCharcode)
	return charMetrics, isCharMetrics
}

func (f *pdfFontType3) getFontDescriptor() *PdfFontDescriptor { return f.fontDescriptor }

// Encoder returns the font's text encoder.
func (f pdfFontType3) Encoder() textencoding.TextEncoder { return f.encoder }

// ToPdfObject converts the font to a PDF representation.
func (f *pdfFontType3) ToPdfObject() core.PdfObject {
	if f.primitive == nil {
		f.primitive = &core.PdfIndirectObject{}
	}
	dict := f.baseFields().asPdfObjectDictionary("Type3")
	f.primitive.PdfObject = dict
	if f.FirstChar != nil {
		dict.Set("FirstChar", f.FirstChar)
	}
	if f.LastChar != nil {
		dict.Set("LastChar", f.LastChar)
	}
	if f.Widths != nil {
		dict.Set("Widths", f.Widths)
	}
	if f.Encoding != nil {
		dict.Set("Encoding", f.Encoding)
	} else if f.encoder != nil {
		obj := f.encoder.ToPdfObject()
		if obj != nil {
			dict.Set("Encoding", obj)
		}
	}
	if f.FontBBox != nil {
		dict.Set("FontBBox", f.FontBBox)
	}
	if f.FontMatrix != nil {
		dict.Set("FontMatirx", f.FontMatrix)
	}
	if f.CharProcs != nil {
		dict.Set("CharProcs", f.CharProcs)
	}
	if f.Resources != nil {
		dict.Set("Resources", f.Resources)
	}
	return f.primitive
}

// StdFontName represents name of a standard font.
type StdFontName = fonts.StdFontName

// CharMetrics represents width and height metrics of a glyph.
type CharMetrics = fonts.CharMetrics

// multiFontEncoder implements a an Encoder that holds a list of fonts provided.
type MultipleFontEncoder struct {
	fontList    []*PdfFont
	CurrentFont *PdfFont
}

// Reset sets the multi font encoder to its initial state.
func (e *MultipleFontEncoder) Reset() { e.CurrentFont = e.fontList[0] }

// GetRuneMetrics iterates through each font in the list of fonts the returns the fonts.CharMetrics from working font.
func (e *MultipleFontEncoder) GetRuneMetrics(r rune) (fonts.CharMetrics, bool) {
	currentFont := e.CurrentFont
	runeMetrics, ok := currentFont.GetRuneMetrics(r)
	for i := 1; i < len(e.fontList) && runeMetrics.Wx == 0; i++ {
		currentFont = e.fontList[i]
		runeMetrics, ok = currentFont.GetRuneMetrics(r)
	}
	return runeMetrics, ok
}

// Encoder iterates through the list of fonts and returns a working encoder
func (e *MultipleFontEncoder) Encoder(rn rune) (textencoding.TextEncoder, bool) {
	currentFont := e.CurrentFont
	encoder := currentFont.Encoder()
	_, ok := encoder.RuneToCharcode(rn)
	for i := 1; i < len(e.fontList) && !ok; i++ {
		currentFont = e.fontList[i]
		encoder = currentFont.Encoder()
		_, ok = encoder.RuneToCharcode(rn)
		e.CurrentFont = currentFont
	}
	return encoder, ok
}

// GetColorGlyphInfoByRune iterates through each font in the list of fonts and returns the color glyph info from working font.
func (e *MultipleFontEncoder) GetColorGlyphInfoByRune(r rune) *ColorGlyphInfo {
	otfType := e.CurrentFont.GetOTFType()
	if otfType != nil {
		colorGlyphInfoByRune := GetColorGlyphInfoByRune(otfType, r)
		if colorGlyphInfoByRune != nil {
			return colorGlyphInfoByRune
		}
	}
	for _, font := range e.fontList {
		otfType = font.GetOTFType()
		if otfType != nil {
			colorGlyphInfoByRune := GetColorGlyphInfoByRune(otfType, r)
			if colorGlyphInfoByRune != nil {
				e.CurrentFont = font
				return colorGlyphInfoByRune
			}
		}
	}
	return nil
}

// GetColorGlyphInfoByText iterates through each font in the list of fonts and returns the color glyph info from working font.
// This handles multi-rune sequences like ZWJ emoji (e.g., "🙋‍♂️").
func (e *MultipleFontEncoder) GetColorGlyphInfoByText(text string) *ColorGlyphInfo {
	otfType := e.CurrentFont.GetOTFType()
	if otfType != nil {
		colorGlyphInfoByText := GetColorGlyphInfoByText(otfType, text)
		if colorGlyphInfoByText != nil {
			return colorGlyphInfoByText
		}
	}
	for _, font := range e.fontList {
		otfType = font.GetOTFType()
		if otfType != nil {
			colorGlyphInfoByText := GetColorGlyphInfoByText(otfType, text)
			if colorGlyphInfoByText != nil {
				e.CurrentFont = font
				return colorGlyphInfoByText
			}
		}
	}
	return nil
}

// NewMultipleFontEncoder returns instantiates a new *MultipleFontEncoder
func NewMultipleFontEncoder(fonts []*PdfFont) *MultipleFontEncoder {
	return &MultipleFontEncoder{fontList: fonts, CurrentFont: fonts[0]}
}
