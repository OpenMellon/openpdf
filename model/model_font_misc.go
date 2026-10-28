package model

import (
	"fmt"
	_ "image/gif"
	"strings"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
)

func (c fontCommon) asPdfObjectDictionary(s string) *core.PdfObjectDictionary {
	if s != "" && c.subtype != "" && s != c.subtype {
		common.Log.Debug("ERROR: asPdfObjectDictionary. Overriding subtype to %#q %s", s, c)
	} else if s == "" && c.subtype == "" {
		common.Log.Debug("ERROR: asPdfObjectDictionary no subtype. font=%s", c)
	} else if c.subtype == "" {
		c.subtype = s
	}
	dict := core.MakeDict()
	dict.Set("Type", core.MakeName("Font"))
	dict.Set("BaseFont", core.MakeName(c.baseFont))
	dict.Set("Subtype", core.MakeName(c.subtype))
	if c.fontDescriptor != nil {
		dict.Set("FontDescriptor", c.fontDescriptor.ToPdfObject())
	}
	if c.toUnicode != nil {
		dict.Set("ToUnicode", c.toUnicode)
	} else if c.toUnicodeCMap != nil {
		stream, err := c.toUnicodeCMap.Stream()
		if err != nil {
			common.Log.Debug("WARN: could not get CMap stream. err=%v", err)
		} else {
			dict.Set("ToUnicode", stream)
		}
	}
	return dict
}

// CharcodeBytesToUnicode converts PDF character codes `data` to a Go unicode string.
//
// 9.10 Extraction of Text Content (page 292)
// The process of finding glyph descriptions in OpenType fonts by a conforming reader shall be the following:
//   - For Type 1 fonts using “CFF” tables, the process shall be as described in 9.6.6.2, "Encodings
//     for Type 1 Fonts".
//   - For TrueType fonts using “glyf” tables, the process shall be as described in 9.6.6.4,
//     "Encodings for TrueType Fonts". Since this process sometimes produces ambiguous results,
//     conforming writers, instead of using a simple font, shall use a Type 0 font with an Identity-H
//     encoding and use the glyph indices as character codes, as described following Table 118.
func (f *PdfFont) CharcodeBytesToUnicode(data []byte) (string, int, int) {
	charcodesToStrings, _, n := f.CharcodesToStrings(f.BytesToCharcodes(data), "")
	runes := []rune(strings.Join(charcodesToStrings, ""))
	expandLigatures := textencoding.ExpandLigatures(runes)
	return expandLigatures, utf8.RuneCountInString(expandLigatures), n
}

func (c fontCommon) isCIDFont() bool {
	if c.subtype == "" {
		common.Log.Debug("ERROR: isCIDFont. context is nil. font=%s", c)
	}
	ok := false
	switch c.subtype {
	case "Type0", "CIDFontType0", "CIDFontType2":
		ok = true
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("isCIDFont: isCID=%t font=%s", ok, c)
	}
	return ok
}

// PdfFont represents an underlying font structure which can be of type:
// - Type0
// - Type1
// - TrueType
// etc.
type PdfFont struct{ context pdfFont }

// GetCharMetrics returns the char metrics for character code `code`.
// How it works:
//  1. It calls the GetCharMetrics function for the underlying font, either a simple font or
//     a Type0 font. The underlying font GetCharMetrics() functions do direct charcode ➞  metrics
//     mappings.
//  2. If the underlying font's GetCharMetrics() doesn't have a CharMetrics for `code` then a
//     a CharMetrics with the FontDescriptor's /MissingWidth is returned.
//  3. If there is no /MissingWidth then a failure is returned.
//
// TODO(peterwilliams97) There is nothing callers can do if no CharMetrics are found so we might as
// well give them 0 width. There is no need for the bool return.
//
// TODO(gunnsth): Reconsider whether needed or if can map via GlyphName.
func (f *PdfFont) GetCharMetrics(code textencoding.CharCode) (CharMetrics, bool) {
	var metrics fonts.CharMetrics
	switch v := f.context.(type) {
	case *pdfFontSimple:
		if charMetrics, ok := v.GetCharMetrics(code); ok {
			return charMetrics, ok
		}
	case *pdfFontType0:
		if charMetrics, ok := v.GetCharMetrics(code); ok {
			return charMetrics, ok
		}
	case *pdfCIDFontType0:
		if charMetrics, ok := v.GetCharMetrics(code); ok {
			return charMetrics, ok
		}
	case *pdfCIDFontType2:
		if charMetrics, ok := v.GetCharMetrics(code); ok {
			return charMetrics, ok
		}
	case *pdfFontType3:
		if charMetrics, ok := v.GetCharMetrics(code); ok {
			return charMetrics, ok
		}
	default:
		common.Log.Debug("ERROR: GetCharMetrics not implemented for font type=%T.", f.context)
		return metrics, false
	}
	if fontDescriptor, err := f.GetFontDescriptor(); err == nil && fontDescriptor != nil {
		return fonts.CharMetrics{Wx: fontDescriptor.missingWidth}, true
	}
	common.Log.Debug("GetCharMetrics: No metrics for font=%s", f)
	return metrics, false
}

type pdfFont interface {
	fonts.Font

	// ToPdfObject returns a PDF representation of the font and implements interface Model.
	ToPdfObject() core.PdfObject
	getFontDescriptor() *PdfFontDescriptor
	baseFields() *fontCommon
}

type fontCommon struct {
	baseFont       string
	subtype        string
	name           string
	toUnicode      core.PdfObject
	toUnicodeCMap  *cmap.CMap
	fontDescriptor *PdfFontDescriptor
	objectNumber   int64
}

// CharcodesToStrings returns the unicode strings corresponding to `charcodes`.
// The int returns are the number of strings and the number of unconvereted codes.
// NOTE: The number of strings returned is equal to the number of charcodes
func (f *PdfFont) CharcodesToStrings(charcodes []textencoding.CharCode, replacementText string) ([]string, int, int) {
	baseFields := f.baseFields()
	strs := make([]string, 0, len(charcodes))
	n := 0
	encoder := f.Encoder()
	ok := baseFields.toUnicodeCMap != nil && f.IsSimple() && f.Subtype() == "TrueType" && !strings.Contains(baseFields.toUnicodeCMap.Name(), "Identity-")
	if !ok && encoder != nil {
		switch v := encoder.(type) {
		case textencoding.SimpleEncoder:
			baseName := v.BaseName()
			if _, ok := predefinedEncodingNames[baseName]; ok {
				for _, charcode := range charcodes {
					if charcodeToRune, ok := encoder.CharcodeToRune(charcode); ok {
						strs = append(strs, string(charcodeToRune))
					} else {
						common.Log.Debug("ERROR: No rune. code=0x%04x charcodes=[% 04x] CID=%t\n"+"\tfont=%s\n\tencoding=%s", charcode, charcodes, baseFields.isCIDFont(), f, encoder)
						n++
						strs = append(strs, cmap.MissingCodeString)
					}
				}
				return strs, len(strs), n
			}
		}
	}
	gidRegistry, _ := encoder.(textencoding.GIDRegistry)
	_, found := encoder.(*textencoding.IdentityEncoder)
	for _, charcode := range charcodes {
		if baseFields.toUnicodeCMap != nil {
			if charcodeToUnicode, ok := baseFields.toUnicodeCMap.CharcodeToUnicode(cmap.CharCode(charcode)); ok {
				lastRuneInString, _ := utf8.DecodeLastRuneInString(charcodeToUnicode)
				s := unicodeCategory(lastRuneInString)
				if s != "Co" {
					if gidRegistry != nil {
						gidRegistry.RegisterGID(textencoding.GID(charcode))
					}
					strs = append(strs, charcodeToUnicode)
					continue
				}
				common.Log.Debug("ERROR: ToUnicode Cmap has incorrect mapping. code=0x%04x is mapped to an invalid code point %s", charcode, charcodeToUnicode)
			}
		}
		if encoder != nil {
			if charcodeToRune, ok := encoder.CharcodeToRune(charcode); ok {
				s := unicodeCategory(charcodeToRune)
				if s != "Co" {
					strs = append(strs, string(charcodeToRune))
					continue
				}
				common.Log.Debug("ERROR: encoder failed. code=0x%04x is decoded to an invalid run %s", charcode, string(charcodeToRune))
			}
			if found {
				if n := rune(charcode); unicodeCategory(n) != "Co" {
					strs = append(strs, string(n))
					continue
				}
			}
		}
		if replacementText != "" {
			strs = append(strs, replacementText)
		} else {
			common.Log.Debug("ERROR: No rune. code=0x%04x charcodes=[% 04x] CID=%t\n"+"\tfont=%s\n\tencoding=%s", charcode, charcodes, baseFields.isCIDFont(), f, encoder)
			n++
			strs = append(strs, cmap.MissingCodeString)
		}
	}
	if n != 0 {
		common.Log.Debug("ERROR: Couldn't convert to unicode. Using input.\n"+"\tnumChars=%d numMisses=%d\n"+"\tfont=%s", len(charcodes), n, f)
	}
	return strs, len(strs), n
}

// CharcodesToUnicode converts the character codes `charcodes` to a slice of runes.
// How it works:
//  1. Use the ToUnicode CMap if there is one.
//  2. Use the underlying font's encoding.
func (f *PdfFont) CharcodesToUnicode(charcodes []textencoding.CharCode) []rune {
	charcodesToStrings, _, _ := f.CharcodesToStrings(charcodes, "")
	return []rune(strings.Join(charcodesToStrings, ""))
}

// IsCID returns true if the underlying font is CID.
func (f *PdfFont) IsCID() bool { return f.baseFields().isCIDFont() }

func (c fontCommon) coreString() string {
	s := ""
	if c.fontDescriptor != nil {
		s = c.fontDescriptor.String()
	}
	return fmt.Sprintf("%#q %#q %q obj=%d ToUnicode=%t flags=0x%0x %s", c.subtype, c.baseFont, c.name, c.objectNumber, c.toUnicode != nil, c.fontFlags(), s)
}

func newFontCommonFromPdfObject(obj core.PdfObject) (*core.PdfObjectDictionary, *fontCommon, error) {
	fc := &fontCommon{}
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		fc.objectNumber = indirect.ObjectNumber
	}
	dict, ok := core.GetDict(obj)
	if !ok {
		common.Log.Debug("ERROR: Font not given by a dictionary (%T)", obj)
		return nil, nil, ErrFontNotSupported
	}
	s, ok := core.GetNameVal(dict.Get("Type"))
	if !ok {
		common.Log.Debug("ERROR: Font Incompatibility. Type (Required) missing")
		return nil, nil, ErrRequiredAttributeMissing
	}
	if s != "Font" {
		common.Log.Debug("ERROR: Font Incompatibility. Type=%q. Should be %q.", s, "Font")
		return nil, nil, core.ErrTypeError
	}
	subtype, ok := core.GetNameVal(dict.Get("Subtype"))
	if !ok {
		common.Log.Debug("ERROR: Font Incompatibility. Subtype (Required) missing")
		return nil, nil, ErrRequiredAttributeMissing
	}
	fc.subtype = subtype
	name, ok := core.GetNameVal(dict.Get("Name"))
	if ok {
		fc.name = name
	}
	toUnicode := dict.Get("ToUnicode")
	if toUnicode != nil {
		fc.toUnicode = core.TraceToDirectObject(toUnicode)
		cm, err := toUnicodeCMapFromObject(fc.toUnicode, fc)
		if err != nil {
			return dict, fc, err
		}
		fc.toUnicodeCMap = cm
	} else if subtype == "CIDFontType0" || subtype == "CIDFontType2" {
		cidSystemInfo, err := cmap.NewCIDSystemInfo(dict.Get("CIDSystemInfo"))
		if err != nil {
			return dict, fc, err
		}
		s := fmt.Sprintf("%s-%s-UCS2", cidSystemInfo.Registry, cidSystemInfo.Ordering)
		if cmap.IsPredefinedCMap(s) {
			fc.toUnicodeCMap, err = cmap.LoadPredefinedCMap(s)
			if err != nil {
				common.Log.Debug("WARN: could not load predefined CMap %s: %v", s, err)
			}
		}
	}
	fontDescriptor := dict.Get("FontDescriptor")
	if fontDescriptor != nil {
		desc, err := newPdfFontDescriptorFromPdfObject(fontDescriptor)
		if err != nil {
			common.Log.Debug("ERROR: Bad font descriptor. err=%v", err)
			return dict, fc, err
		}
		fc.fontDescriptor = desc
	}
	if subtype != "Type3" {
		baseFont, ok := core.GetNameVal(dict.Get("BaseFont"))
		if !ok {
			common.Log.Debug("ERROR: Font Incompatibility. BaseFont (Required) missing")
			return dict, fc, ErrRequiredAttributeMissing
		}
		fc.baseFont = baseFont
	}
	return dict, fc, nil
}

// String returns a string that describes `base`.
func (c fontCommon) String() string { return fmt.Sprintf("FONT{%s}", c.coreString()) }

// GetCIDToGIDMapObject returns the underlying CIDToGIDMap object if the font
// type is CIDFontType2. For a Type0 font it delegates to the descendant CIDFont.
func (f *PdfFont) GetCIDToGIDMapObject() core.PdfObject {
	switch v := f.context.(type) {
	case *pdfCIDFontType2:
		return v.CIDToGIDMap
	case *pdfFontType0:
		if v.DescendantFont != nil {
			return v.DescendantFont.GetCIDToGIDMapObject()
		}
	}
	return nil
}

// BytesToCharcodes converts the bytes in a PDF string to character codes.
func (f *PdfFont) BytesToCharcodes(data []byte) []textencoding.CharCode {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("BytesToCharcodes: data=[% 02x]=%#q", data, data)
	}
	if font, ok := f.context.(*pdfFontType0); ok && font.predefinedCMap != nil {
		if bytesToCharcodes, ok := font.bytesToCharcodes(data); ok {
			return bytesToCharcodes
		}
	}
	var (
		codes      = make([]textencoding.CharCode, 0, len(data)+len(data)%2)
		baseFields = f.baseFields()
	)
	if baseFields.toUnicodeCMap != nil {
		if bytesToCharcodes, ok := baseFields.toUnicodeCMap.BytesToCharcodes(data); ok {
			for _, bytesToCharcode := range bytesToCharcodes {
				codes = append(codes, textencoding.CharCode(bytesToCharcode))
			}
			return codes
		}
	}
	if baseFields.isCIDFont() {
		if len(data) == 1 {
			data = []byte{0, data[0]}
		}
		if len(data)%2 != 0 {
			common.Log.Debug("ERROR: Padding data=%+v to even length", data)
			data = append(data, 0)
		}
		for i := 0; i < len(data); i += 2 {
			n := uint16(data[i])<<8 | uint16(data[i+1])
			codes = append(codes, textencoding.CharCode(n))
		}
	} else {
		for _, b := range data {
			codes = append(codes, textencoding.CharCode(b))
		}
	}
	return codes
}

func (f *PdfFont) baseFields() *fontCommon {
	if f.context == nil {
		common.Log.Debug("ERROR: baseFields. context is nil.")
		return nil
	}
	return f.context.baseFields()
}

func (c fontCommon) fontFlags() int {
	if c.fontDescriptor == nil {
		return 0
	}
	return c.fontDescriptor.flags
}

func (f PdfFont) actualFont() pdfFont {
	if f.context == nil {
		common.Log.Debug("ERROR: actualFont. context is nil. font=%s", f)
	}
	return f.context
}

// BaseFont returns the font's "BaseFont" field.
func (f *PdfFont) BaseFont() string { return f.baseFields().baseFont }
