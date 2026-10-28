package model

import (
	"bytes"
	"errors"
	"fmt"
	_ "image/gif"
	"strings"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
	"github.com/unidoc/typesetting/shaping"
)

// PdfFont represents an underlying font structure which can be of type:
// - Type0
// - Type1
// - TrueType
// etc.
type PdfFont struct{ context pdfFont }

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

func (f PdfFont) actualFont() pdfFont {
	if f.context == nil {
		common.Log.Debug("ERROR: actualFont. context is nil. font=%s", f)
	}
	return f.context
}

// BaseFont returns the font's "BaseFont" field.
func (f *PdfFont) BaseFont() string { return f.baseFields().baseFont }

// RunesToCharcodeBytes maps the provided runes to charcode bytes and it
// returns the resulting slice of bytes, along with the number of runes which
// could not be converted. If the number of misses is 0, all runes were
// successfully converted.
func (f *PdfFont) RunesToCharcodeBytes(data []rune) ([]byte, int) {
	var encs []textencoding.TextEncoder
	var cMapEncoder textencoding.CMapEncoder
	if toUnicodeCMap := f.baseFields().toUnicodeCMap; toUnicodeCMap != nil {
		cMapEncoder = textencoding.NewCMapEncoder("", nil, toUnicodeCMap)
	}
	encoder := f.Encoder()
	if encoder != nil {
		switch v := encoder.(type) {
		case textencoding.SimpleEncoder:
			baseName := v.BaseName()
			if _, ok := predefinedEncodingNames[baseName]; ok {
				encs = append(encs, encoder)
			}
		}
	}
	if len(encs) == 0 {
		if f.baseFields().toUnicodeCMap != nil {
			encs = append(encs, cMapEncoder)
		}
		if encoder != nil {
			encs = append(encs, encoder)
		}
	}
	var buf bytes.Buffer
	var n int
	for _, count := range data {
		var ok bool
		for _, enc := range encs {
			if data := enc.Encode(string(count)); len(data) > 0 {
				buf.Write(data)
				ok = true
				break
			}
		}
		if !ok {
			common.Log.Debug("ERROR: failed to map rune `%+q` to charcode", count)
			n++
		}
	}
	if n != 0 {
		common.Log.Debug("ERROR: could not convert all runes to charcodes.\n"+"\tnumRunes=%d numMisses=%d\n"+"\tfont=%s encoders=%+v", len(data), n, f, encs)
	}
	return buf.Bytes(), n
}

// RegisterLigatureMapping records the ToUnicode mapping for a shaped run so the
// emitted glyphs are extractable as text and kept through subsetting. It is the
// ToUnicode counterpart of ShapeText: ShapeText registers glyphs with the
// encoder for subsetting, while this records what text those glyphs represent
// and also registers their glyph IDs. Callers driving their own shaper should
// call both; PdfFont.ShapeText followed by this method (as the creator does) is
// the supported pairing.
func (f *PdfFont) RegisterLigatureMapping(shapingOutput shaping.Output, text string) error {
	if f == nil {
		return errors.New("font is nil")
	}
	runes := []rune(text)
	gidEncoder, _ := f.Encoder().(textencoding.GIDEncoder)
	v, _ := f.Encoder().(interface{ RegisterShapedGID(n uint16) })
	textByCode := make(map[cmap.CharCode]string)
	code := make(map[cmap.CharCode]string)
	n := -1
	for _, glyph := range shapingOutput.Glyphs {
		if glyph.GlyphID > 0xffff {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("RegisterLigatureMapping: glyph ID %d overflows uint16 - skipping", glyph.GlyphID)
			}
			continue
		}
		ok := glyph.TextIndex() != n
		n = glyph.TextIndex()
		glyphID := uint16(glyph.GlyphID)
		if v != nil {
			v.RegisterShapedGID(glyphID)
		}
		code2 := textencoding.CharCode(textencoding.GID(glyphID))
		if gidEncoder != nil {
			if gidToCharcode, ok := gidEncoder.GIDToCharcode(textencoding.GID(glyphID)); ok {
				code2 = gidToCharcode
			}
		}
		code3 := cmap.CharCode(code2)
		s := shapedGlyphText(runes, glyph)
		if s == "" {
			continue
		}
		if glyph.GlyphsCount() == 1 {
			textByCode[code3] = s
		} else if ok {
			if _, ok := code[code3]; !ok {
				code[code3] = s
			}
		}
	}
	switch v := f.context.(type) {
	case *pdfFontType0:
		v.applyLigatureToUnicode(textByCode, code)
	default:
	}
	return nil
}

// ToPdfObject converts the PdfFont object to its PDF representation.
func (f *PdfFont) ToPdfObject() core.PdfObject {
	if f.context == nil {
		common.Log.Debug("ERROR: font context is nil")
		return core.MakeNull()
	}
	return f.context.ToPdfObject()
}

// GetRuneMetrics returns the char metrics for a rune.
// TODO(peterwilliams97) There is nothing callers can do if no CharMetrics are found so we might as
// well give them 0 width. There is no need for the bool return.
func (f *PdfFont) GetRuneMetrics(r rune) (CharMetrics, bool) {
	actualFont := f.actualFont()
	if actualFont == nil {
		common.Log.Debug("ERROR: GetGlyphCharMetrics Not implemented for font type=%#T", f.context)
		return fonts.CharMetrics{}, false
	}
	if runeMetrics, ok := actualFont.GetRuneMetrics(r); ok {
		return runeMetrics, true
	}
	if fontDescriptor, err := f.GetFontDescriptor(); err == nil && fontDescriptor != nil {
		return fonts.CharMetrics{Wx: fontDescriptor.missingWidth}, true
	}
	common.Log.Debug("GetGlyphCharMetrics: No metrics for font=%s", f)
	return fonts.CharMetrics{}, false
}

// GetFontDescriptor returns the font descriptor for `font`.
func (f PdfFont) GetFontDescriptor() (*PdfFontDescriptor, error) {
	return f.context.getFontDescriptor(), nil
}

// GetOTFType checks if the font is an OTF font and returns the OTF type if it is
func (f *PdfFont) GetOTFType() *fonts.OtfType {
	if f == nil {
		return nil
	}
	if font, ok := f.context.(*pdfFontType0); ok {
		if descendantFont := font.DescendantFont; descendantFont != nil {
			if font, ok := descendantFont.context.(*pdfCIDFontType0); ok {
				if otfType := font.GetOtfType(); otfType != nil {
					return otfType
				}
			}
			if font, ok := descendantFont.context.(*pdfCIDFontType2); ok {
				if otfType := font.GetOtfType(); otfType != nil {
					return otfType
				}
			}
		}
	}
	return nil
}

// GetEmojiImageSize returns the dimensions of an emoji image if available.
// For SVG emojis, this may return 0,0 as SVG is scalable.
func (f *PdfFont) GetEmojiImageSize(r rune) (n, count int, err error) {
	otfType := f.GetOTFType()
	if otfType == nil {
		return 0, 0, fmt.Errorf("font does not support OTF color glyphs")
	}
	colorGlyphInfoByRune := GetColorGlyphInfoByRune(otfType, r)
	if colorGlyphInfoByRune == nil {
		return 0, 0, fmt.Errorf("no color glyph information found for rune %c (U+%04X)", r, r)
	}
	n, count = colorGlyphInfoByRune.GetImageSize()
	return n, count, nil
}

// Encoder returns the font's text encoder.
func (f *PdfFont) Encoder() textencoding.TextEncoder {
	actualFont := f.actualFont()
	if actualFont == nil {
		common.Log.Debug("ERROR: Encoder not implemented for font type=%#T", f.context)
		return nil
	}
	return actualFont.Encoder()
}

// GetEmojiImageData extracts image data from an emoji rune if the font supports it.
// Returns the raw image data, MIME type, and file extension.
// This is a convenience method that combines OTF font detection and color glyph extraction.
func (f *PdfFont) GetEmojiImageData(r rune) (data []byte, s string, s2 string, err error) {
	otfType := f.GetOTFType()
	if otfType == nil {
		return nil, "", "", fmt.Errorf("font does not support OTF color glyphs")
	}
	colorGlyphInfoByRune := GetColorGlyphInfoByRune(otfType, r)
	if colorGlyphInfoByRune == nil {
		return nil, "", "", fmt.Errorf("no color glyph information found for rune %c (U+%04X)", r, r)
	}
	data, s, s2 = colorGlyphInfoByRune.GetImageData()
	if data == nil {
		return nil, "", "", fmt.Errorf("no image data available for rune %c (U+%04X)", r, r)
	}
	return data, s, s2, nil
}

// FontDescriptor returns font's PdfFontDescriptor. This may be a builtin descriptor for standard 14
// fonts but must be an explicit descriptor for other fonts.
func (f *PdfFont) FontDescriptor() *PdfFontDescriptor {
	if f.baseFields().fontDescriptor != nil {
		return f.baseFields().fontDescriptor
	}
	if fontDescriptor := f.context.getFontDescriptor(); fontDescriptor != nil {
		return fontDescriptor
	}
	common.Log.Error("All fonts have a Descriptor. font=%s", f)
	return nil
}

// NewPdfFontFromPdfObject loads a PdfFont from the dictionary `fontObj`.  If there is a problem an
// error is returned.
func NewPdfFontFromPdfObject(fontObj core.PdfObject) (*PdfFont, error) {
	return newPdfFontFromPdfObjectInternal(fontObj, true)
}
