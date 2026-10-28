package model

import (
	"bytes"
	"errors"
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
	"github.com/unidoc/typesetting/shaping"
)

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

func (f *fontFile) loadFromSegments(data, data2 []byte) error {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("loadFromSegments: %d %d", len(data), len(data2))
	}
	err := f.parseASCIIPart(data)
	if err != nil {
		return err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("fontfile=%s", f)
	}
	if len(data2) == 0 {
		return nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("fontfile=%s", f)
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

const (
	fontFlagFixedPitch  = 0x00001
	fontFlagSerif       = 0x00002
	fontFlagSymbolic    = 0x00004
	fontFlagScript      = 0x00008
	fontFlagNonsymbolic = 0x00020
	fontFlagItalic      = 0x00040
	fontFlagAllCap      = 0x10000
	fontFlagSmallCap    = 0x20000
	fontFlagForceBold   = 0x40000
)

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

// String returns a human readable description of `fontfile`.
func (f *fontFile) String() string {
	s := "[None]"
	if f.encoder != nil {
		s = f.encoder.String()
	}
	return fmt.Sprintf("FONTFILE{%#q encoder=%s}", f.name, s)
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

func newFontFileFromPdfObject(obj core.PdfObject) (*fontFile, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("newFontFileFromPdfObject: obj=%s", obj)
	}
	file := &fontFile{}
	obj = core.TraceToDirectObject(obj)
	stream, ok := obj.(*core.PdfObjectStream)
	if !ok {
		common.Log.Debug("ERROR: FontFile must be a stream (%T)", obj)
		return nil, core.ErrTypeError
	}
	dict := stream.PdfObjectDictionary
	data, err := core.DecodeStream(stream)
	if err != nil {
		return nil, err
	}
	subtype, ok := core.GetNameVal(dict.Get("Subtype"))
	if !ok {
		file.subtype = subtype
		if subtype == "Type1C" {
			common.Log.Debug("Type1C fonts are currently not supported")
			return nil, ErrType1CFontNotSupported
		}
	}
	length1, _ := core.GetIntVal(dict.Get("Length1"))
	length2, _ := core.GetIntVal(dict.Get("Length2"))
	if length1 > len(data) {
		length1 = len(data)
	}
	if length1+length2 > len(data) {
		length2 = len(data) - length1
	}
	data2 := data[:length1]
	var data3 []byte
	if length2 > 0 {
		data3 = data[length1 : length1+length2]
	}
	if length1 > 0 && length2 > 0 {
		err := file.loadFromSegments(data2, data3)
		if err != nil {
			return nil, err
		}
	}
	return file, nil
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

func (f *fontFile) parseASCIIPart(data []byte) error {
	if len(data) < 2 || string(data[:2]) != "%!" {
		return errors.New("invalid start of ASCII segment")
	}
	asciiSections, s, err := getASCIISections(data)
	if err != nil {
		return err
	}
	type1Dictionary := parseType1Dictionary(asciiSections)
	f.name = type1Dictionary["FontName"]
	if f.name == "" {
		common.Log.Debug(" FontFile has no /FontName")
	}
	if s != "" {
		type1Encoding, err := parseType1Encoding(s)
		if err != nil {
			return err
		}
		customSimpleTextEncoder, err := textencoding.NewCustomSimpleTextEncoder(type1Encoding, nil)
		if err != nil {
			common.Log.Debug("ERROR :UNKNOWN GLYPH: err=%v", err)
			return nil
		}
		f.encoder = customSimpleTextEncoder
	}
	return nil
}

type fontFile struct {
	name    string
	subtype string
	encoder textencoding.SimpleEncoder
}

// NewPdfFontFromPdfObject loads a PdfFont from the dictionary `fontObj`.  If there is a problem an
// error is returned.
func NewPdfFontFromPdfObject(fontObj core.PdfObject) (*PdfFont, error) {
	return newPdfFontFromPdfObjectInternal(fontObj, true)
}

func newPdfFontFromPdfObjectInternal(fontObj core.PdfObject, ok bool) (*PdfFont, error) {
	dict, fc, err := newFontCommonFromPdfObject(fontObj)
	if dict != nil {
		fixFontWidthsRange(dict)
	}
	if err != nil {
		if err == ErrType1CFontNotSupported {
			fontSimple, fontSimpleErr := newPdfFontSimpleFromDict(dict, fc, nil)
			if fontSimpleErr != nil {
				common.Log.Debug("ERROR: While loading simple font: font=%s err=%v", fc, fontSimpleErr)
				return nil, err
			}
			return &PdfFont{context: fontSimple}, err
		}
		return nil, err
	}
	font := &PdfFont{}
	switch fc.subtype {
	case "Type0":
		if !ok {
			common.Log.Debug("ERROR: Loading type0 not allowed. font=%s", fc)
			return nil, errors.New("cyclical type0 loading")
		}
		fontType0, err := newPdfFontType0FromDict(dict, fc)
		if err != nil {
			common.Log.Debug("ERROR: While loading Type0 font. font=%s err=%v", fc, err)
			return nil, err
		}
		font.context = fontType0
	case "Type1", "MMType1", "TrueType":
		var font2 *pdfFontSimple
		stdFontByName, ok := fonts.NewStdFontByName(fonts.StdFontName(fc.baseFont))
		if ok {
			fontSimple := newPdfFontSimpleFromStdFont(stdFontByName)
			font2, err = newPdfFontSimpleFromDict(dict, fc, fontSimple.std14Encoder)
			if err != nil {
				common.Log.Debug("ERROR: Bad Standard14\n\tfont=%s\n\tstd=%+v", fc, fontSimple)
				return nil, err
			}
			font2.fontMetrics = fontSimple.fontMetrics
			if font2.Widths == nil {
				font2.charWidths = fontSimple.charWidths
			}
			if font2.std14Descriptor == nil {
				font2.std14Descriptor = fontSimple.std14Descriptor
			}
			mergeFontDescriptor(font2.fontDescriptor, fontSimple.std14Descriptor)
		} else {
			font2, err = newPdfFontSimpleFromDict(dict, fc, nil)
			if err != nil {
				common.Log.Debug("ERROR: While loading simple font: font=%s err=%v", fc, err)
				return nil, err
			}
		}
		err = font2.addEncoding()
		if err != nil {
			return nil, err
		}
		if ok && font2.Widths == nil {
			font2.updateStandard14Font()
		}
		if ok && font2.encoder == nil && font2.std14Encoder == nil {
			common.Log.Error("simplefont=%s", font2)
			common.Log.Error("fnt=%+v", stdFontByName)
		}
		if len(font2.charWidths) == 0 {
			common.Log.Debug("ERROR: No widths. font=%s", font2)
		}
		font.context = font2
	case "Type3":
		fontType3, err := newPdfFontType3FromDict(dict, fc)
		if err != nil {
			common.Log.Debug("ERROR: While loading type3 font: %v", err)
			return nil, err
		}
		font.context = fontType3
	case "CIDFontType0":
		cidFontType0, err := newPdfCIDFontType0FromDict(dict, fc)
		if err != nil {
			common.Log.Debug("ERROR: While loading cid font type0 font: %v", err)
			return nil, err
		}
		font.context = cidFontType0
	case "CIDFontType2":
		cidFontType2, err := newPdfCIDFontType2FromDict(dict, fc)
		if err != nil {
			common.Log.Debug("ERROR: While loading cid font type2 font. font=%s err=%v", fc, err)
			return nil, err
		}
		font.context = cidFontType2
	default:
		common.Log.Debug("ERROR: Unsupported font type: font=%s", fc)
		return nil, fmt.Errorf("unsupported font type: font=%s", fc)
	}
	return font, nil
}
