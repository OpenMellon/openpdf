// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package textencoding

import (
	"encoding/binary"
	"slices"
	"sync"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/internal/textencoding/internal/syncmap"
	"github.com/OpenMellon/typesetting/font"
	"github.com/OpenMellon/typesetting/shaping"
	encoding2 "golang.org/x/text/encoding"
)

var runeToGlyphName14 = map[rune]string{0xfeb0: "zainfinalarabic", 0xfeaf: "zainisolated", 0x30b6: "zakatakana", 0x0595: "zaqefgadolhebrew", 0x0594: "zaqefqatanhebrew", 0x0598: "zarqahebrew", 0xfb36: "zayindageshhebrew", 0x3117: "zbopomofo", 0x017e: "zcaron", 0x24e9: "zcircle", 0x1e91: "zcircumflex", 0x2a1f: "zcmp", 0x0291: "zcurl", 0x017c: "zdotaccent", 0x1e93: "zdotbelow", 0x0499: "zedescendercyrillic", 0x04df: "zedieresiscyrillic", 0x305c: "zehiragana", 0x30bc: "zekatakana", 0x0030: "zero", 0x09e6: "zerobengali", 0x0966: "zerodeva", 0x0ae6: "zerogujarati", 0x0a66: "zerogurmukhi", 0x0660: "zerohackarabic", 0x2080: "zeroinferior", 0xff10: "zeromonospace", 0xf730: "zerooldstyle", 0x06f0: "zeropersian", 0x2070: "zerosuperior", 0x0e50: "zerothai", 0xfeff: "zerowidthjoiner", 0x200b: "zerowidthspace", 0x03b6: "zeta", 0x3113: "zhbopomofo", 0x056a: "zhearmenian", 0x03fe: "zhebreve", 0x04c2: "zhebrevecyrillic", 0x0436: "zhecyrillic", 0x0497: "zhedescendercyrillic", 0x04dd: "zhedieresiscyrillic", 0x3058: "zihiragana", 0x30b8: "zikatakana", 0x05ae: "zinorhebrew", 0x1e95: "zlinebelow", 0xff5a: "zmonospace", 0x305e: "zohiragana", 0x30be: "zokatakana", 0x24b5: "zparen", 0x2a20: "zpipe", 0x2a21: "zproject", 0x0290: "zretroflexhook", 0x0220: "zrthook", 0x01b6: "zstroke", 0x305a: "zuhiragana", 0x30ba: "zukatakana"}

var runeToGlyphName = syncmap.NewRuneStringMap(mergeMaps(runeToGlyphName1, runeToGlyphName2, runeToGlyphName3, runeToGlyphName4, runeToGlyphName5, runeToGlyphName6, runeToGlyphName7, runeToGlyphName8, runeToGlyphName9, runeToGlyphName10, runeToGlyphName11, runeToGlyphName12, runeToGlyphName13, runeToGlyphName14))

func (enc *differencesEncoding) Charcodes() []CharCode {
	codes := enc.base.Charcodes()
	sorted := true
	have := make(map[CharCode]struct{}, len(codes))
	for _, code := range codes {
		have[code] = struct{}{}
	}
	enc.byteToRune.Range(func(b byte, r rune) (stop bool) {
		c := CharCode(b)
		if _, found := have[c]; !found {
			codes = append(codes, c)
			sorted = false
		}
		return false
	})
	if !sorted {
		slices.Sort(codes)
	}
	return codes
}

type simpleMapping struct {
	name       string
	once       sync.Once
	byteToRune *syncmap.ByteRuneMap
	runeToByte *syncmap.RuneByteMap
}

func (m *simpleEncodersMap) Read(s string) (func() SimpleEncoder, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	newEnc, ok := m.encoders[s]
	return newEnc, ok
}

func (enc *OpenTypeFontEncoder) ShapeText(text string, fontSize float64) shaping.Line {
	line := ShapeTextLine(enc.face, text, fontSize)
	if line == nil {
		return nil
	}
	runes := []rune(text)
	for _, run := range line {
		for _, glyph := range run.Glyphs {
			if glyph.GlyphID > 0xffff {
				if common.Log.IsLogLevel(common.LogLevelDebug) {
					common.Log.Debug("ShapeText: glyph ID %d overflows uint16 - skipping", glyph.GlyphID)
				}
				continue
			}
			gid := uint16(glyph.GlyphID)
			enc.shapedGIDs.Write(gid)
			start := glyph.TextIndex()
			count := glyph.RunesCount()
			if start < 0 || count <= 0 || start+count > len(runes) {
				continue
			}
			var r rune
			var ok bool
			if count == 1 {
				r, ok = runes[start], true
			} else {
				r, ok = GlyphToRune(GlyphName(string(runes[start : start+count])))
			}
			if !ok {
				continue
			}
			if _, found := enc.runeToGID.Read(r); !found {
				enc.runeToGID.Write(r, gid)
			}
			enc.usedRunes.Write(r)
		}
	}
	return line
}

func (enc *differencesEncoding) ToPdfObject() core.PdfObject {
	d := core.MakeDict()
	d.Set("Type", core.MakeName("Encoding"))
	d.Set("BaseEncoding", enc.base.ToPdfObject())
	if diffs := makeDifferencesArray(enc.differences); diffs != nil {
		d.Set("Differences", diffs)
	} else {
		common.Log.Debug("WARN: font Differences array is nil. Output may be incorrect.")
	}
	return core.MakeIndirectObject(d)
}
func (enc *simpleEncoding) BaseName() string { return enc.name }

type GIDEncoder interface {
	CharcodeToGID(code CharCode) (GID, bool)
	GIDToCharcode(gid GID) (CharCode, bool)
}
type simpleDecoder struct{ byteToRune *syncmap.ByteRuneMap }

var ligatureStrings = syncmap.NewRuneStringMap(map[rune]string{'Ꜳ': "AA", 'ꜳ': "aa", 'Ꜵ': "aa", 'ꜵ': "ao", 'Ꜷ': "AU", 'ꜷ': "au", 'Ꜽ': "AY", 'ꜽ': "ay", '\U0001f670': "et", 'ﬀ': "ff", 'ﬃ': "ffi", 'ﬄ': "ffl", 'ﬁ': "fi", 'ﬂ': "fl", 'Œ': "OE", 'œ': "oe", 'Ꝏ': "OO", 'ꝏ': "oo", 'ﬆ': "st", 'ﬅ': "ſt", 'Ꜩ': "TZ", 'ꜩ': "tz", 'ᵫ': "ue", 'Ꝡ': "VY", 'ꝡ': "vy", 0xe000: "ft", 0xe001: "fj", 0xe002: "fb", 0xe003: "fh", 0xe004: "fk", 0xe005: "tt", 0xe006: "tf", 0xe007: "ffj", 0xe008: "ffb", 0xe009: "ffh", 0xe00a: "ffk", 0xe00b: "T_h"})

func (m *charCodeGlyphNameMap) Read(key CharCode) (GlyphName, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	glyph, ok := m.data[key]
	return glyph, ok
}

func ApplyDifferences(base SimpleEncoder, differences map[CharCode]GlyphName) SimpleEncoder {
	if len(differences) == 0 {
		return base
	}
	tuples := make([]charCodeGlyphNameTuple, len(differences))
	var i int
	for code, glyph := range differences {
		tuples[i] = charCodeGlyphNameTuple{CharCode: code, GlyphName: glyph}
		i++
	}
	enc := &differencesEncoding{base: base, differences: newCharCodeGlyphNameMap(tuples...), byteToRune: syncmap.MakeByteRuneMap(len(differences)), runeToByte: syncmap.MakeRuneByteMap(len(differences))}
	if baseDiff, isDiff := base.(*differencesEncoding); isDiff {
		merged := make(map[CharCode]GlyphName)
		baseDiff.differences.Range(func(code CharCode, glyph GlyphName) (stop bool) { merged[code] = glyph; return false })
		enc.differences.Range(func(code CharCode, glyph GlyphName) (stop bool) {
			merged[code] = glyph
			return false
		})
		differences = merged
	}
	for code, glyph := range differences {
		b := byte(code)
		r, ok := GlyphToRune(glyph)
		if ok {
			enc.runeToByte.Write(r, b)
		} else {
			common.Log.Debug("ERROR: No match for glyph=%q differences=%+v", glyph, differences)
		}
		enc.byteToRune.Write(b, r)
	}
	return enc
}

func newSimpleEncoding(name string, byteToRune *syncmap.ByteRuneMap) SimpleEncoder {
	enc := &simpleEncoding{name: name, byteToRune: byteToRune, runeToByte: syncmap.MakeRuneByteMap(byteToRune.Length())}
	enc.byteToRune.Range(func(b byte, r rune) (stop bool) {
		if prev, found := enc.runeToByte.Read(r); !found || b < prev {
			enc.runeToByte.Write(r, b)
		}
		return false
	})
	return enc
}

func (enc *IdentityEncoder) RegisteredGIDs() []GID {
	gids := make([]GID, 0, enc.usedGIDs.Length())
	enc.usedGIDs.Range(func(gid uint16) (stop bool) {
		gids = append(gids, GID(gid))
		return false
	})
	slices.Sort(gids)
	return gids
}

func (enc *simpleEncoding) Decode(raw []byte) string {
	buf, _ := enc.NewDecoder().Bytes(raw)
	return string(buf)
}

func RegisterSimpleEncoding(name string, fnc func() SimpleEncoder) {
	if _, exists := simpleEncoders.Read(name); exists {
		panic("already registered")
	}
	simpleEncoders.Write(name, fnc)
}

const maxStringEntries = 10

var (
	symbolMapping       = newSimpleMapping(symbolEncodingName, symbolRunes)
	zapfDingbatsMapping = newSimpleMapping(zapfDingbatsEncodingName, zapfDingbatsRunes)
)

func (enc CMapEncoder) RuneToCharcode(r rune) (CharCode, bool) {
	if enc.cidToUnicode == nil {
		return 0, false
	}
	cid, ok := enc.cidToUnicode.StringToCID(string(r))
	if !ok {
		return 0, false
	}
	if enc.codeToCID != nil {
		charcode, ok := enc.codeToCID.CIDToCharcode(cid)
		if !ok {
			return 0, false
		}
		return CharCode(charcode), true
	}
	return CharCode(cid), true
}
func (enc *simpleEncoding) ToPdfObject() core.PdfObject       { return core.MakeName(enc.name) }
func (enc *OpenTypeFontEncoder) RegisterShapedGID(gid uint16) { enc.shapedGIDs.Write(gid) }
func (enc *IdentityEncoder) SetToUnicodeCMap(tu *cmap.CMap)   { enc.toUnicode = tu }
func (enc CMapEncoder) String() string                        { return enc.name }

type OpenTypeFontEncoder struct {
	*TrueTypeFontEncoder
	face       *font.Face
	shapedGIDs *syncmap.Uint16Set
}

func encodeTwoByte(enc TextEncoder, s string) []byte {
	runes := []rune(s)
	buf := make([]byte, 0, len(runes)*2)
	for _, r := range runes {
		code, ok := enc.RuneToCharcode(r)
		if !ok {
			norm := NormalizeRunePresentationForms(r)
			code, ok = enc.RuneToCharcode(norm)
			if !ok {
				common.Log.Debug("Failed to map rune to charcode. rune=%+q", r)
				continue
			}
		}
		var pair [2]byte
		binary.BigEndian.PutUint16(pair[:], uint16(code))
		buf = append(buf, pair[:]...)
	}
	return buf
}

const (
	MissingCodeRune   = '\ufffd'
	MissingCodeString = string(MissingCodeRune)
)

func (enc *IdentityEncoder) RegisterGID(gid GID) { enc.usedGIDs.Write(uint16(gid)) }

var simpleEncoders = &simpleEncodersMap{encoders: map[string]func() SimpleEncoder{}}

const (
	pdfDocEncodingName   = "PdfDocEncoding"
	standardEncodingName = "StandardEncoding"
)

type CMapEncoder struct {
	name         string
	codeToCID    *cmap.CMap
	cidToUnicode *cmap.CMap
}

func (enc *TrueTypeFontEncoder) Decode(raw []byte) string { return decodeTwoByteCodes(enc, raw) }
func newCharCodeGlyphNameMap(tuples ...charCodeGlyphNameTuple) *charCodeGlyphNameMap {
	m := map[CharCode]GlyphName{}
	for _, t := range tuples {
		m[t.CharCode] = t.GlyphName
	}
	return &charCodeGlyphNameMap{data: m}
}

func (enc *TrueTypeFontEncoder) RuneToCharcode(r rune) (CharCode, bool) {
	gid, ok := enc.RuneToGID(r)
	if !ok {
		return 0, false
	}
	return enc.GIDToCharcode(gid)
}
func (enc *IdentityEncoder) Decode(raw []byte) string { return decodeTwoByteCodes(enc, raw) }
func (enc *simpleEncoding) NewEncoder() *encoding2.Encoder {
	return &encoding2.Encoder{Transformer: &simpleEncoder{runeToByte: enc.runeToByte}}
}
func (enc *TrueTypeFontEncoder) Encode(str string) []byte    { return encodeTwoByte(enc, str) }
func (enc UTF16Encoder) String() string                      { return enc.name }
func (enc *TrueTypeFontEncoder) ToPdfObject() core.PdfObject { return core.MakeNull() }
