package model

import (
	_ "image/gif"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
)

func (f *pdfFontSimple) getFontEncoding() (s string, glyphByCode map[textencoding.CharCode]textencoding.GlyphName, err error) {
	s = "StandardEncoding"
	if symbolicFontEncoding, ok := symbolicFontEncodings[f.baseFont]; ok {
		s = symbolicFontEncoding
	} else if f.fontFlags()&fontFlagSymbolic != 0 {
		for s2, symbolicFontEncoding := range symbolicFontEncodings {
			if strings.Contains(f.baseFont, s2) {
				s = symbolicFontEncoding
				break
			}
		}
	}
	if f.Encoding == nil {
		return s, nil, nil
	}
	switch v := f.Encoding.(type) {
	case *core.PdfObjectName:
		return string(*v), nil, nil
	case *core.PdfObjectDictionary:
		baseEncoding, ok := core.GetName(v.Get("BaseEncoding"))
		if ok {
			s = baseEncoding.String()
		}
		if differences := v.Get("Differences"); differences != nil {
			differencesArr, ok := core.GetArray(differences)
			if !ok {
				common.Log.Debug("ERROR: Bad font encoding dict=%+v Differences=%T", v, v.Get("Differences"))
				return "", nil, core.ErrTypeError
			}
			glyphByCode, err = textencoding.FromFontDifferences(differencesArr)
		}
		return s, glyphByCode, err
	default:
		common.Log.Debug("ERROR: Encoding not a name or dict (%T) %s", f.Encoding, f.Encoding)
		return "", nil, core.ErrTypeError
	}
}

func newPdfFontSimpleFromCommon(fc *fontCommon) *pdfFontSimple {
	return &pdfFontSimple{fontCommon: *fc}
}

type pdfFontSimple struct {
	fontCommon
	primitive       *core.PdfIndirectObject
	charWidths      map[textencoding.CharCode]float64
	encoder         textencoding.TextEncoder
	std14Encoder    textencoding.TextEncoder
	std14Descriptor *PdfFontDescriptor

	// Encoding is subject to limitations that are described in 9.6.6, "Character Encoding".
	// BaseFont is derived differently.
	FirstChar   core.PdfObject
	LastChar    core.PdfObject
	Widths      core.PdfObject
	Encoding    core.PdfObject
	fontMetrics *fonts.RuneCharSafeMap
}

// ToPdfObject converts the pdfFontSimple to its PDF representation for outputting.
func (f *pdfFontSimple) ToPdfObject() core.PdfObject {
	if f.primitive == nil {
		f.primitive = &core.PdfIndirectObject{}
	}
	dict := f.baseFields().asPdfObjectDictionary("")
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
	return f.primitive
}

func newPdfFontSimpleFromDict(dict *core.PdfObjectDictionary, fc *fontCommon, enc textencoding.TextEncoder) (*pdfFontSimple, error) {
	fontSimple := newPdfFontSimpleFromCommon(fc)
	fontSimple.std14Encoder = enc
	if enc == nil || dict.Get("Widths") != nil {
		firstChar := dict.Get("FirstChar")
		if firstChar == nil {
			firstChar = core.MakeInteger(0)
		}
		fontSimple.FirstChar = firstChar
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
		fontSimple.LastChar = firstChar
		n, ok = core.GetIntVal(firstChar)
		if !ok {
			common.Log.Debug("ERROR: Invalid LastChar type (%T)", firstChar)
			return nil, core.ErrTypeError
		}
		code2 := textencoding.CharCode(n)
		fontSimple.charWidths = make(map[textencoding.CharCode]float64)
		firstChar = dict.Get("Widths")
		if firstChar != nil {
			fontSimple.Widths = firstChar
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
			for i, float := range floats {
				fontSimple.charWidths[code+textencoding.CharCode(i)] = float
			}
		}
	}
	fontSimple.Encoding = core.TraceToDirectObject(dict.Get("Encoding"))
	return fontSimple, nil
}

func newPdfFontSimpleFromStdFont(stdFont fonts.StdFont) pdfFontSimple {
	descriptor := stdFont.Descriptor()
	return pdfFontSimple{fontCommon: fontCommon{subtype: "Type1", baseFont: stdFont.Name()}, fontMetrics: stdFont.GetMetricsTable(), std14Descriptor: &PdfFontDescriptor{FontName: core.MakeName(string(descriptor.Name)), FontFamily: core.MakeName(descriptor.Family), FontWeight: core.MakeFloat(float64(descriptor.Weight)), Flags: core.MakeInteger(int64(descriptor.Flags)), FontBBox: core.MakeArrayFromFloats(descriptor.BBox[:]), ItalicAngle: core.MakeFloat(descriptor.ItalicAngle), Ascent: core.MakeFloat(descriptor.Ascent), Descent: core.MakeFloat(descriptor.Descent), CapHeight: core.MakeFloat(descriptor.CapHeight), XHeight: core.MakeFloat(descriptor.XHeight), StemV: core.MakeFloat(descriptor.StemV), StemH: core.MakeFloat(descriptor.StemH)}, std14Encoder: stdFont.Encoder()}
}

// GetCharMetrics returns the character metrics for the specified character code.  A bool flag is
// returned to indicate whether or not the entry was found in the glyph to charcode mapping.
// How it works:
//  1. Return a value the /Widths array (charWidths) if there is one.
//  2. If the font has the same name as a standard 14 font then return width=250.
//  3. Otherwise return no match and let the caller substitute a default.
func (f pdfFontSimple) GetCharMetrics(code textencoding.CharCode) (fonts.CharMetrics, bool) {
	if charWidth, ok := f.charWidths[code]; ok {
		return fonts.CharMetrics{Wx: charWidth}, true
	}
	if fonts.IsStdFont(fonts.StdFontName(f.baseFont)) {
		return fonts.CharMetrics{Wx: 250}, true
	}
	return fonts.CharMetrics{}, false
}

// IsSimple returns true if `font` is a simple font.
func (f *PdfFont) IsSimple() bool { _, ok := f.context.(*pdfFontSimple); return ok }

// GetRuneMetrics returns the character metrics for the rune.
// A bool flag is returned to indicate whether or not the entry was found.
func (f pdfFontSimple) GetRuneMetrics(r rune) (fonts.CharMetrics, bool) {
	if f.fontMetrics != nil {
		metrics, ok := f.fontMetrics.Read(r)
		if ok {
			return metrics, true
		}
	}
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

func newPdfFontSimpleFromStdName(basefont StdFontName) (pdfFontSimple, error) {
	stdFontByName, ok := fonts.NewStdFontByName(basefont)
	if !ok {
		return pdfFontSimple{}, ErrFontNotSupported
	}
	fontSimple := newPdfFontSimpleFromStdFont(stdFontByName)
	return fontSimple, nil
}

func (f *pdfFontSimple) updateStandard14Font() {
	enc, ok := f.Encoder().(textencoding.SimpleEncoder)
	if !ok {
		common.Log.Error("Wrong encoder type: %T. font=%s.", f.Encoder(), f)
		return
	}
	charcodes := enc.Charcodes()
	f.charWidths = make(map[textencoding.CharCode]float64, len(charcodes))
	for _, charcode := range charcodes {
		charcodeToRune, _ := enc.CharcodeToRune(charcode)
		metrics, _ := f.fontMetrics.Read(charcodeToRune)
		f.charWidths[charcode] = metrics.Wx
	}
}

func (f *pdfFontSimple) getFontDescriptor() *PdfFontDescriptor {
	if fontDescriptor := f.fontDescriptor; fontDescriptor != nil {
		return fontDescriptor
	}
	return f.std14Descriptor
}

// SetEncoder sets the encoding for the underlying font.
func (f *pdfFontSimple) SetEncoder(encoder textencoding.SimpleEncoder) { f.encoder = encoder }

// Encoder returns the font's text encoder.
func (f *pdfFontSimple) Encoder() textencoding.TextEncoder {
	if f.encoder != nil {
		return f.encoder
	}
	if f.std14Encoder != nil {
		return f.std14Encoder
	}
	simpleTextEncoder, _ := textencoding.NewSimpleTextEncoder("StandardEncoding", nil)
	return simpleTextEncoder
}

func (f *pdfFontSimple) baseFields() *fontCommon { return &f.fontCommon }

func (f *pdfFontSimple) addEncoding() error {
	var (
		s           string
		glyphByCode map[textencoding.CharCode]textencoding.GlyphName
		enc         textencoding.SimpleEncoder
	)
	if f.Encoder() != nil {
		enc, ok := f.Encoder().(textencoding.SimpleEncoder)
		if ok && enc != nil {
			s = enc.BaseName()
		}
	}
	if f.Encoding != nil {
		fontEncoding, code, err := f.getFontEncoding()
		if err != nil {
			common.Log.Debug("ERROR: BaseFont=%q Subtype=%q Encoding=%s (%T) err=%v", f.baseFont, f.subtype, f.Encoding, f.Encoding, err)
			return err
		}
		if fontEncoding != "" {
			s = fontEncoding
		}
		glyphByCode = code
		enc, err = textencoding.NewSimpleTextEncoder(s, glyphByCode)
		if err != nil {
			return err
		}
	}
	if enc == nil {
		fontDescriptor := f.fontDescriptor
		if fontDescriptor != nil {
			switch f.subtype {
			case "Type1":
				if fontDescriptor.fontFile != nil && fontDescriptor.encoder != nil {
					common.Log.Debug("Using fontFile")
					enc = fontDescriptor.encoder
				}
			case "TrueType":
				if fontDescriptor.ttf != nil {
					common.Log.Debug("Using FontFile2")
					encoder, err := fontDescriptor.ttf.MakeEncoder()
					if err == nil {
						enc = encoder
					}
					if f.toUnicodeCMap == nil {
						f.toUnicodeCMap = fontDescriptor.ttf.MakeToUnicode()
					}
				}
			}
		}
	}
	if enc != nil {
		if glyphByCode != nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("differences=%+v font=%s", glyphByCode, f.baseFields())
			}
			enc = textencoding.ApplyDifferences(enc, glyphByCode)
		}
		f.SetEncoder(enc)
	}
	return nil
}
