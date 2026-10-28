package model

import (
	"bytes"
	"errors"
	"fmt"
	_ "image/gif"
	"io"
	"os"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
	"github.com/unidoc/typesetting/shaping"
)

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

// SubsetRegistered subsets the font to only the glyphs that have been registered by the encoder.
//
// NOTE: This only works on fonts that support subsetting. For unsupported fonts this is a no-op, although a debug
// message is emitted.  Currently supported fonts are embedded Truetype CID fonts (type 0).
//
// NOTE: Make sure to call this soon before writing (once all needed runes have been registered).
// If using package creator, use its EnableFontSubsetting method instead.
func (f *PdfFont) SubsetRegistered() error {
	switch v := f.context.(type) {
	case *pdfFontType0:
		err := v.subsetRegistered()
		if err != nil {
			common.Log.Debug("Subset error: %v", err)
			return err
		}
		if v.primitive != nil {
			if v.encoder != nil {
				v.encoder.ToPdfObject()
			}
			v.ToPdfObject()
		}
	default:
		common.Log.Debug("Font %T does not support subsetting", v)
	}
	return nil
}

// NewPdfFontFromTTF loads a TTF font and returns a PdfFont type that can be
// used in text styling functions.
// Uses a WinAnsiTextEncoder and loads only character codes 32-255.
// NOTE: For composite fonts such as used in symbolic languages, use NewCompositePdfFont.
func NewPdfFontFromTTF(r io.ReadSeeker) (*PdfFont, error) {
	const defaultFirstChar = textencoding.CharCode(32)
	const defaultLastChar = textencoding.CharCode(255)
	all, err := io.ReadAll(r)
	if err != nil {
		common.Log.Debug("ERROR: Unable to read font contents: %v", err)
		return nil, err
	}
	if fonts.IsTTC(all) {
		all, err = fonts.SfntFromTTC(all, 0)
		if err != nil {
			common.Log.Debug("ERROR: extracting font from TTC: %v", err)
			return nil, err
		}
	}
	ttfParse, err := fonts.TtfParse(bytes.NewReader(all))
	if err != nil {
		common.Log.Debug("ERROR: loading TTF font: %v", err)
		return nil, err
	}
	font := &pdfFontSimple{charWidths: make(map[textencoding.CharCode]float64), fontCommon: fontCommon{subtype: "TrueType"}}
	font.encoder = textencoding.NewWinAnsiEncoder()
	font.baseFont = ttfParse.PostScriptName
	font.FirstChar = core.MakeInteger(int64(defaultFirstChar))
	font.LastChar = core.MakeInteger(int64(defaultLastChar))
	f := 1000.0 / float64(ttfParse.UnitsPerEm)
	if len(ttfParse.Widths) <= 0 {
		return nil, errors.New("ERROR: Missing required attribute (Widths)")
	}
	v := f * float64(ttfParse.Widths[0])
	floats := make([]float64, 0, defaultLastChar-defaultFirstChar+1)
	for i := defaultFirstChar; i <= defaultLastChar; i++ {
		charcodeToRune, ok := font.Encoder().CharcodeToRune(i)
		if !ok {
			common.Log.Debug("Rune not found (code: %d)", i)
			floats = append(floats, v)
			continue
		}
		char, isChar := ttfParse.Chars[charcodeToRune]
		if !isChar {
			common.Log.Debug("Rune not in TTF Chars")
			floats = append(floats, v)
			continue
		}
		v := f * float64(ttfParse.Widths[char])
		floats = append(floats, v)
	}
	font.Widths = core.MakeIndirectObject(core.MakeArrayFromFloats(floats))
	if len(floats) < int(defaultLastChar-defaultFirstChar+1) {
		common.Log.Debug("ERROR: Invalid length of widths, %d < %d", len(floats), 255-32+1)
		return nil, core.ErrRangeError
	}
	for i := defaultFirstChar; i <= defaultLastChar; i++ {
		font.charWidths[i] = floats[i-defaultFirstChar]
	}
	font.Encoding = core.MakeName("WinAnsiEncoding")
	fontDescriptor := newPdfFontDescriptorFromTtf(&ttfParse, v, fontFlagNonsymbolic)
	stream, err := core.MakeStream(all, core.NewFlateEncoder())
	if err != nil {
		common.Log.Debug("ERROR: Unable to make stream: %v", err)
		return nil, err
	}
	stream.PdfObjectDictionary.Set("Length1", core.MakeInteger(int64(len(all))))
	fontDescriptor.FontFile2 = stream
	font.fontDescriptor = fontDescriptor
	font2 := &PdfFont{context: font}
	return font2, nil
}

// ShapeText shapes `text` with HarfBuzz and registers the resulting glyphs with
// the font's encoder so that subsetting keeps them (ligatures and contextual
// forms included). It returns nil for fonts without an OpenType encoder.
func (f *PdfFont) ShapeText(text string, fontSize float64) shaping.Line {
	openTypeFontEncoder, ok := f.Encoder().(*textencoding.OpenTypeFontEncoder)
	if !ok {
		return nil
	}
	return openTypeFontEncoder.ShapeText(text, fontSize)
}

// NewPdfFontFromTTFFile loads a TTF font file and returns a PdfFont type
// that can be used in text styling functions.
// Uses a WinAnsiTextEncoder and loads only character codes 32-255.
// NOTE: For composite fonts such as used in symbolic languages, use NewCompositePdfFontFile.
func NewPdfFontFromTTFFile(filePath string) (*PdfFont, error) {
	open, err := os.Open(filePath)
	if err != nil {
		common.Log.Debug("ERROR: reading TTF font file: %v", err)
		return nil, err
	}
	defer open.Close()
	return NewPdfFontFromTTF(open)
}

// Subtype returns the font's "Subtype" field.
func (f *PdfFont) Subtype() string {
	subtype := f.baseFields().subtype
	if font, ok := f.context.(*pdfFontType0); ok {
		subtype = subtype + ":" + font.DescendantFont.Subtype()
	}
	return subtype
}

// String returns a string that describes `font`.
func (f *PdfFont) String() string {
	s := ""
	if f.context.Encoder() != nil {
		s = f.context.Encoder().String()
	}
	return fmt.Sprintf("FONT{%T %s %s}", f.context, f.baseFields().coreString(), s)
}

// ToUnicode returns the name of the font's "ToUnicode" field if there is one, or "" if there isn't.
func (f *PdfFont) ToUnicode() string {
	if f.baseFields().toUnicodeCMap == nil {
		return ""
	}
	return f.baseFields().toUnicodeCMap.Name()
}

// StringToCharcodeBytes maps the provided string runes to charcode bytes and
// it returns the resulting slice of bytes, along with the number of runes
// which could not be converted. If the number of misses is 0, all string runes
// were successfully converted.
func (f *PdfFont) StringToCharcodeBytes(str string) ([]byte, int) {
	return f.RunesToCharcodeBytes([]rune(str))
}

// IsSimple returns true if `font` is a simple font.
func (f *PdfFont) IsSimple() bool { _, ok := f.context.(*pdfFontSimple); return ok }
