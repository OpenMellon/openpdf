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
	"github.com/OpenMellon/unitype"
)

type pdfFontType0 struct {
	fontCommon
	primitive      *core.PdfIndirectObject
	encoder        textencoding.TextEncoder
	Encoding       core.PdfObject
	DescendantFont *PdfFont
	predefinedCMap *cmap.CMap
}

func (f *pdfFontType0) subsetRegistered() error {
	switch v := f.DescendantFont.context.(type) {
	case *pdfCIDFontType2:
		return f.subsetTrueType(v)
	case *pdfCIDFontType0:
		return f.subsetCFF(v)
	default:
		common.Log.Debug("Font not supported for subsetting %T", f.DescendantFont)
		return nil
	}
}

func (f *pdfFontType0) subsetTrueType(font *pdfCIDFontType2) error {
	if font == nil {
		return nil
	}
	if font.fontDescriptor == nil {
		common.Log.Debug("Missing font descriptor")
		return nil
	}
	if f.encoder == nil {
		common.Log.Debug("No encoder - subsetting ignored")
		return nil
	}
	fontFile2Stream, ok := core.GetStream(font.fontDescriptor.FontFile2)
	if !ok {
		common.Log.Debug("Embedded font object not found -- ABORT subsetting")
		return errors.New("fontfile2 not found")
	}
	stream, err := core.DecodeStream(fontFile2Stream)
	if err != nil {
		common.Log.Debug("Decode error: %v", err)
		return err
	}
	font2, err := unitype.Parse(bytes.NewReader(stream))
	if err != nil {
		common.Log.Debug("Error parsing %d byte font", len(fontFile2Stream.Stream))
		return err
	}
	var runes []rune
	var gids []unitype.GlyphIndex
	switch v := f.encoder.(type) {
	case textencoding.GIDRegistry:
		registeredGIDs := v.RegisteredGIDs()
		if len(registeredGIDs) == 0 {
			common.Log.Debug("No registered GIDs - skip subsetting")
			return nil
		}
		gids = make([]unitype.GlyphIndex, len(registeredGIDs))
		for i, registeredGID := range registeredGIDs {
			gids[i] = unitype.GlyphIndex(registeredGID)
		}
	case textencoding.RuneRegistry:
		runes = v.RegisteredRunes()
		gids = font2.LookupRunes(runes)
		v.SubsetRegistered()
	case textencoding.SimpleEncoder:
		charcodes := v.Charcodes()
		for _, charcode := range charcodes {
			charcodeToRune, ok := v.CharcodeToRune(charcode)
			if !ok {
				common.Log.Debug("ERROR: unable convert charcode to rune: %d", charcode)
				continue
			}
			runes = append(runes, charcodeToRune)
		}
		gids = font2.LookupRunes(runes)
	default:
		return fmt.Errorf("unsupported encoder for subsetting: %T", f.encoder)
	}
	vals := shapedGlyphIDs(f.encoder)
	for _, val := range vals {
		gids = append(gids, unitype.GlyphIndex(val))
	}
	gids = sortedUniqueGlyphIndexes(gids)
	subsetKeepIndices, err := font2.SubsetKeepIndices(gids)
	if err != nil {
		common.Log.Debug("ERROR: %v", err)
		return err
	}
	var buf bytes.Buffer
	err = subsetKeepIndices.Write(&buf)
	if err != nil {
		common.Log.Debug("ERROR: %v", err)
		return err
	}
	if runes != nil {
		f.updateSubsetToUnicode(runes, vals)
	} else {
		f.filterSubsetToUnicode(gids)
	}
	registerGlyphWidthsFromGlyphIndexes(font.W, gids)
	registerOtfGlyphWidths(font.W, vals, font.GetOtfType())
	fontFile2Stream, err = core.MakeStream(buf.Bytes(), core.NewFlateEncoder())
	if err != nil {
		common.Log.Debug("ERROR: %v", err)
		return err
	}
	fontFile2Stream.Set("Length1", core.MakeInteger(int64(buf.Len())))
	if stream, ok := core.GetStream(font.fontDescriptor.FontFile2); ok {
		*stream = *fontFile2Stream
	} else {
		font.fontDescriptor.FontFile2 = fontFile2Stream
	}
	applySubsetTag(f, &font.baseFont, font.fontDescriptor, buf.Bytes())
	return nil
}

func (f *pdfFontType0) filterSubsetToUnicode(gids []unitype.GlyphIndex) {
	if f.toUnicodeCMap == nil {
		return
	}
	textByCode := make(map[cmap.CharCode]string, len(gids))
	for _, gid := range gids {
		code := cmap.CharCode(gid)
		if charcodeToUnicode, ok := f.toUnicodeCMap.CharcodeToUnicode(code); ok {
			textByCode[code] = charcodeToUnicode
		}
	}
	f.toUnicodeCMap = cmap.NewToUnicodeCMap(textByCode)
	if identityEncoder, ok := f.encoder.(*textencoding.IdentityEncoder); ok {
		identityEncoder.SetToUnicodeCMap(f.toUnicodeCMap)
	}
}

func newPdfFontType0FromCommon(fc *fontCommon) *pdfFontType0 { return &pdfFontType0{fontCommon: *fc} }

// GetRuneMetrics returns the character metrics for the specified rune.
// A bool flag is returned to indicate whether or not the entry was found.
func (f pdfFontType0) GetRuneMetrics(r rune) (fonts.CharMetrics, bool) {
	if f.DescendantFont == nil {
		common.Log.Debug("ERROR: No descendant. font=%s", f)
		return fonts.CharMetrics{}, false
	}
	return f.DescendantFont.GetRuneMetrics(r)
}

func (f *pdfFontType0) applyLigatureToUnicode(textByCode, code map[cmap.CharCode]string) {
	if len(textByCode) == 0 && len(code) == 0 {
		return
	}
	if f.toUnicodeCMap == nil {
		f.toUnicodeCMap = cmap.NewToUnicodeCMap(map[cmap.CharCode]string{})
	}
	f.toUnicodeCMap.MergeCodeToUnicode(code, false)
	f.toUnicodeCMap.MergeCodeToUnicode(textByCode, true)
	if identityEncoder, ok := f.encoder.(*textencoding.IdentityEncoder); ok {
		identityEncoder.SetToUnicodeCMap(f.toUnicodeCMap)
	}
}

func newPdfFontType0FromDict(dict *core.PdfObjectDictionary, fc *fontCommon) (*pdfFontType0, error) {
	descendantFonts, ok := core.GetArray(dict.Get("DescendantFonts"))
	if !ok {
		common.Log.Debug("ERROR: Invalid DescendantFonts - not an array %s", fc)
		return nil, core.ErrRangeError
	}
	if descendantFonts.Len() != 1 {
		common.Log.Debug("ERROR: Array length != 1 (%d)", descendantFonts.Len())
		return nil, core.ErrRangeError
	}
	font, err := newPdfFontFromPdfObjectInternal(descendantFonts.Get(0), false)
	if err != nil {
		common.Log.Debug("ERROR: Failed loading descendant font: err=%v %s", err, fc)
		return nil, err
	}
	encoding := dict.Get("Encoding")
	s := ""
	fontType0 := newPdfFontType0FromCommon(fc)
	fontType0.DescendantFont = font
	switch v := encoding.(type) {
	case *core.PdfObjectName:
		s, ok = core.GetNameVal(encoding)
		if ok {
			if s == "Identity-H" || s == "Identity-V" {
				identityTextEncoder := textencoding.NewIdentityTextEncoder(s)
				identityTextEncoder.SetToUnicodeCMap(fc.toUnicodeCMap)
				if cidToGIDMap := cidToGIDMapFromObject(font.GetCIDToGIDMapObject()); cidToGIDMap != nil {
					identityTextEncoder.SetCIDToGIDMap(cidToGIDMap)
				}
				fontType0.encoder = identityTextEncoder
			} else if cmap.IsPredefinedCMap(s) {
				fontType0.predefinedCMap, err = cmap.LoadPredefinedCMap(s)
				if err != nil {
					common.Log.Debug("WARN: could not load predefined CMap %s: %v", s, err)
				}
			} else {
				common.Log.Debug("Unhandled cmap %q", s)
			}
		}
	case *core.PdfObjectStream:
		if fontType0.toUnicode == nil {
			cidSystemInfo, err := cmap.NewCIDSystemInfo(v.PdfObjectDictionary.Get("CIDSystemInfo"))
			if err != nil {
				common.Log.Debug("Unable to get CIDSystemInfo: %v", err)
			}
			s := fmt.Sprintf("%s-%s-UCS2", cidSystemInfo.Registry, cidSystemInfo.Ordering)
			if cmap.IsPredefinedCMap(s) {
				fontType0.predefinedCMap, err = cmap.LoadPredefinedCMap(s)
				if err != nil {
					common.Log.Debug("WARN: could not load predefined CMap %s: %v", s, err)
				}
			} else {
				s = v.PdfObjectDictionary.Get("CMapName").String()
				stream, err := core.DecodeStream(v)
				if err != nil {
					common.Log.Debug("Unable to decode stream: %v", err)
					return fontType0, err
				}
				if ok := s == "OneByteIdentityH" || s == "OneByteIdentityV"; ok {
					fontType0.predefinedCMap, err = cmap.LoadCmapFromData(stream, ok)
					if err != nil {
						common.Log.Debug("Unable to load CMap from stream: %v -- %v", err, string(stream))
						return fontType0, err
					}
					textByCode := make(map[cmap.CharCode]string, 256)
					for i := 0x00; i <= 0xFF; i++ {
						textByCode[cmap.CharCode(i)] = string(rune(i))
					}
					fontType0.toUnicodeCMap = cmap.NewToUnicodeCMap(textByCode)
				}
			}
		}
	}
	if toUnicodeCMap := font.baseFields().toUnicodeCMap; toUnicodeCMap != nil {
		if name := toUnicodeCMap.Name(); name == "Adobe-CNS1-UCS2" || name == "Adobe-GB1-UCS2" || name == "Adobe-Japan1-UCS2" || name == "Adobe-Korea1-UCS2" {
			fontType0.encoder = textencoding.NewCMapEncoder(s, fontType0.predefinedCMap, toUnicodeCMap)
		}
	}
	return fontType0, nil
}

func (f *pdfFontType0) bytesToCharcodes(data []byte) ([]textencoding.CharCode, bool) {
	if f.predefinedCMap == nil {
		return nil, false
	}
	bytesToCharcodes, ok := f.predefinedCMap.BytesToCharcodes(data)
	if !ok {
		return nil, false
	}
	codes := make([]textencoding.CharCode, len(bytesToCharcodes))
	for i, bytesToCharcode := range bytesToCharcodes {
		codes[i] = textencoding.CharCode(bytesToCharcode)
	}
	return codes, true
}

// GetCharMetrics returns the char metrics for character code `code`.
func (f pdfFontType0) GetCharMetrics(code textencoding.CharCode) (fonts.CharMetrics, bool) {
	if f.DescendantFont == nil {
		common.Log.Debug("ERROR: No descendant. font=%s", f)
		return fonts.CharMetrics{}, false
	}
	return f.DescendantFont.GetCharMetrics(code)
}

func (f *pdfFontType0) baseFields() *fontCommon { return &f.fontCommon }

// Encoder returns the font's text encoder.
func (f pdfFontType0) Encoder() textencoding.TextEncoder { return f.encoder }

func (f *pdfFontType0) updateSubsetToUnicode(runes []rune, vals []uint16) {
	if f.toUnicodeCMap == nil {
		return
	}
	toUnicodeCMap := f.toUnicodeCMap
	textByCode := make(map[cmap.CharCode]string, len(runes)+len(vals))
	for _, n := range runes {
		runeToCharcode, ok := f.encoder.RuneToCharcode(n)
		if !ok {
			continue
		}
		textByCode[cmap.CharCode(runeToCharcode)] = string(n)
	}
	for _, val := range vals {
		code := cmap.CharCode(val)
		if _, ok := textByCode[code]; ok {
			continue
		}
		if charcodeToUnicode, ok := toUnicodeCMap.CharcodeToUnicode(code); ok {
			textByCode[code] = charcodeToUnicode
		}
	}
	f.toUnicodeCMap = cmap.NewToUnicodeCMap(textByCode)
}

// ToPdfObject converts the font to a PDF representation.
func (f *pdfFontType0) ToPdfObject() core.PdfObject {
	if f.primitive == nil {
		f.primitive = &core.PdfIndirectObject{}
	}
	dict := f.baseFields().asPdfObjectDictionary("Type0")
	f.primitive.PdfObject = dict
	if f.Encoding != nil {
		dict.Set("Encoding", f.Encoding)
	} else if f.encoder != nil {
		dict.Set("Encoding", f.encoder.ToPdfObject())
	}
	if f.DescendantFont != nil {
		dict.Set("DescendantFonts", core.MakeArray(f.DescendantFont.ToPdfObject()))
	}
	return f.primitive
}

func (f *pdfFontType0) subsetCFF(font *pdfCIDFontType0) error {
	if font == nil {
		return nil
	}
	otfType := font.GetOtfType()
	if otfType == nil || otfType.Loader == nil {
		common.Log.Debug("No OTF data for CFF subsetting")
		return nil
	}
	if font.fontDescriptor == nil {
		common.Log.Debug("Missing font descriptor for CFF subsetting")
		return nil
	}
	if f.encoder == nil {
		common.Log.Debug("No encoder - CFF subsetting ignored")
		return nil
	}
	data, err := cffTable(otfType.Loader)
	if err != nil {
		common.Log.Debug("Unable to get CFF table: %v", err)
		return nil
	}
	var runes []rune
	var vals []uint16
	switch v := f.encoder.(type) {
	case textencoding.GIDRegistry:
		for _, registeredGID := range v.RegisteredGIDs() {
			vals = append(vals, uint16(registeredGID))
		}
		if len(vals) == 0 {
			common.Log.Debug("No registered GIDs - skip CFF subsetting")
			return nil
		}
	case textencoding.RuneRegistry:
		runes = v.RegisteredRunes()
		vals = runesToGIDs(runes, otfType.Chars)
		v.SubsetRegistered()
	default:
		common.Log.Debug("Unsupported encoder for CFF subsetting: %T", f.encoder)
		return nil
	}
	vals2 := shapedGlyphIDs(f.encoder)
	vals = append(vals, vals2...)
	vals = sortedUniqueUint16(vals)
	subsetCFF, err := fonts.SubsetCFF(data, vals)
	if err != nil {
		common.Log.Debug("CFF subset error: %v", err)
		return err
	}
	stream, err := core.MakeStream(subsetCFF, core.NewFlateEncoder())
	if err != nil {
		common.Log.Debug("ERROR: %v", err)
		return err
	}
	stream.Set("Subtype", core.MakeName("CIDFontType0C"))
	if fontFile3Stream, ok := core.GetStream(font.fontDescriptor.FontFile3); ok {
		*fontFile3Stream = *stream
	} else {
		font.fontDescriptor.FontFile3 = stream
	}
	if runes != nil {
		f.updateSubsetToUnicode(runes, vals2)
	} else {
		gids := make([]unitype.GlyphIndex, len(vals))
		for i, val := range vals {
			gids[i] = unitype.GlyphIndex(val)
		}
		f.filterSubsetToUnicode(gids)
	}
	registerGlyphWidthsFromGIDs(font.W, vals)
	registerOtfGlyphWidths(font.W, vals2, otfType)
	applySubsetTag(f, &font.baseFont, font.fontDescriptor, subsetCFF)
	return nil
}

func (f *pdfFontType0) getFontDescriptor() *PdfFontDescriptor {
	if f.fontDescriptor == nil && f.DescendantFont != nil {
		return f.DescendantFont.FontDescriptor()
	}
	return f.fontDescriptor
}
