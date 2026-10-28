// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package textencoding

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/internal/strutils"
	"github.com/OpenMellon/openpdf/internal/textencoding/internal/syncmap"
	"github.com/OpenMellon/typesetting/font"
	"golang.org/x/text/transform"
)

var glyphNameToRune14 = map[string]rune{"zainfinalarabic": 0xfeb0, "zainisolated": 0xfeaf, "zakatakana": 0x30b6, "zaqefgadolhebrew": 0x0595, "zaqefqatanhebrew": 0x0594, "zarqahebrew": 0x0598, "zayindageshhebrew": 0xfb36, "zbopomofo": 0x3117, "zcaron": 0x017e, "zcircle": 0x24e9, "zcircumflex": 0x1e91, "zcmp": 0x2a1f, "zcurl": 0x0291, "zdotaccent": 0x017c, "zdotbelow": 0x1e93, "zedescendercyrillic": 0x0499, "zedieresiscyrillic": 0x04df, "zehiragana": 0x305c, "zekatakana": 0x30bc, "zero": 0x0030, "zerobengali": 0x09e6, "zerodeva": 0x0966, "zerogujarati": 0x0ae6, "zerogurmukhi": 0x0a66, "zerohackarabic": 0x0660, "zeroinferior": 0x2080, "zeromonospace": 0xff10, "zerooldstyle": 0xf730, "zeropersian": 0x06f0, "zerosuperior": 0x2070, "zerothai": 0x0e50, "zerowidthjoiner": 0xfeff, "zerowidthspace": 0x200b, "zeta": 0x03b6, "zhbopomofo": 0x3113, "zhearmenian": 0x056a, "zhebreve": 0x03fe, "zhebrevecyrillic": 0x04c2, "zhecyrillic": 0x0436, "zhedescendercyrillic": 0x0497, "zhedieresiscyrillic": 0x04dd, "zihiragana": 0x3058, "zikatakana": 0x30b8, "zinorhebrew": 0x05ae, "zlinebelow": 0x1e95, "zmonospace": 0xff5a, "zohiragana": 0x305e, "zokatakana": 0x30be, "zparen": 0x24b5, "zpipe": 0x2a20, "zproject": 0x2a21, "zretroflexhook": 0x0290, "zrthook": 0x0220, "zstroke": 0x01b6, "zuhiragana": 0x305a, "zukatakana": 0x30ba}

var glyphNameToRune = syncmap.NewStringRuneMap(mergeMaps(glyphNameToRune1, glyphNameToRune2, glyphNameToRune3, glyphNameToRune4, glyphNameToRune5, glyphNameToRune6, glyphNameToRune7, glyphNameToRune8, glyphNameToRune9, glyphNameToRune10, glyphNameToRune11, glyphNameToRune12, glyphNameToRune13, glyphNameToRune14))

func (enc CMapEncoder) Encode(str string) []byte {
	if enc.cidToUnicode == nil {
		return []byte{}
	}
	if enc.cidToUnicode.NBits() == 8 {
		return encodeSingleByte(enc, str)
	}
	return encodeTwoByte(enc, str)
}

const (
	symbolEncodingName       = "SymbolEncoding"
	zapfDingbatsEncodingName = "ZapfDingbatsEncoding"
)

func (enc *OpenTypeFontEncoder) RegisteredShapedGIDs() []GID {
	gids := make([]GID, 0, enc.shapedGIDs.Length())
	enc.shapedGIDs.Range(func(gid uint16) (stop bool) {
		gids = append(gids, GID(gid))
		return false
	})
	slices.Sort(gids)
	return gids
}

func RuneToString(r rune) string {
	if s, ok := ligatureStrings.Read(r); ok {
		return s
	}
	return string(r)
}
func (enc UTF16Encoder) CharcodeToRune(code CharCode) (rune, bool) { return rune(code), true }

var symbolRunes = syncmap.NewByteRuneMap(map[byte]rune{0x20: ' ', 0x21: '!', 0x22: '∀', 0x23: '#', 0x24: '∃', 0x25: '%', 0x26: '&', 0x27: '∋', 0x28: '(', 0x29: ')', 0x2a: '∗', 0x2b: '+', 0x2c: ',', 0x2d: '−', 0x2e: '.', 0x2f: '/', 0x30: '0', 0x31: '1', 0x32: '2', 0x33: '3', 0x34: '4', 0x35: '5', 0x36: '6', 0x37: '7', 0x38: '8', 0x39: '9', 0x3a: ':', 0x3b: ';', 0x3c: '<', 0x3d: '=', 0x3e: '>', 0x3f: '?', 0x40: '≅', 0x41: 'Α', 0x42: 'Β', 0x43: 'Χ', 0x44: '∆', 0x45: 'Ε', 0x46: 'Φ', 0x47: 'Γ', 0x48: 'Η', 0x49: 'Ι', 0x4a: 'ϑ', 0x4b: 'Κ', 0x4c: 'Λ', 0x4d: 'Μ', 0x4e: 'Ν', 0x4f: 'Ο', 0x50: 'Π', 0x51: 'Θ', 0x52: 'Ρ', 0x53: 'Σ', 0x54: 'Τ', 0x55: 'Υ', 0x56: 'ς', 0x57: 'Ω', 0x58: 'Ξ', 0x59: 'Ψ', 0x5a: 'Ζ', 0x5b: '[', 0x5c: '∴', 0x5d: ']', 0x5e: '⊥', 0x5f: '_', 0x60: '\uf8e5', 0x61: 'α', 0x62: 'β', 0x63: 'χ', 0x64: 'δ', 0x65: 'ε', 0x66: 'φ', 0x67: 'γ', 0x68: 'η', 0x69: 'ι', 0x6a: 'ϕ', 0x6b: 'κ', 0x6c: 'λ', 0x6d: 'µ', 0x6e: 'ν', 0x6f: 'ο', 0x70: 'π', 0x71: 'θ', 0x72: 'ρ', 0x73: 'σ', 0x74: 'τ', 0x75: 'υ', 0x76: 'ϖ', 0x77: 'ω', 0x78: 'ξ', 0x79: 'ψ', 0x7a: 'ζ', 0x7b: '{', 0x7c: '|', 0x7d: '}', 0x7e: '∼', 0xa0: '€', 0xa1: 'ϒ', 0xa2: '′', 0xa3: '≤', 0xa4: '⁄', 0xa5: '∞', 0xa6: 'ƒ', 0xa7: '♣', 0xa8: '♦', 0xa9: '♥', 0xaa: '♠', 0xab: '↔', 0xac: '←', 0xad: '↑', 0xae: '→', 0xaf: '↓', 0xb0: '°', 0xb1: '±', 0xb2: '″', 0xb3: '≥', 0xb4: '×', 0xb5: '∝', 0xb6: '∂', 0xb7: '•', 0xb8: '÷', 0xb9: '≠', 0xba: '≡', 0xbb: '≈', 0xbc: '…', 0xbd: '\uf8e6', 0xbe: '\uf8e7', 0xbf: '↵', 0xc0: 'ℵ', 0xc1: 'ℑ', 0xc2: 'ℜ', 0xc3: '℘', 0xc4: '⊗', 0xc5: '⊕', 0xc6: '∅', 0xc7: '∩', 0xc8: '∪', 0xc9: '⊃', 0xca: '⊇', 0xcb: '⊄', 0xcc: '⊂', 0xcd: '⊆', 0xce: '∈', 0xcf: '∉', 0xd0: '∠', 0xd1: '∇', 0xd2: '\uf6da', 0xd3: '\uf6d9', 0xd4: '\uf6db', 0xd5: '∏', 0xd6: '√', 0xd7: '⋅', 0xd8: '¬', 0xd9: '∧', 0xda: '∨', 0xdb: '⇔', 0xdc: '⇐', 0xdd: '⇑', 0xde: '⇒', 0xdf: '⇓', 0xe0: '◊', 0xe1: '〈', 0xe2: '\uf8e8', 0xe3: '\uf8e9', 0xe4: '\uf8ea', 0xe5: '∑', 0xe6: '\uf8eb', 0xe7: '\uf8ec', 0xe8: '\uf8ed', 0xe9: '\uf8ee', 0xea: '\uf8ef', 0xeb: '\uf8f0', 0xec: '\uf8f1', 0xed: '\uf8f2', 0xee: '\uf8f3', 0xef: '\uf8f4', 0xf1: '〉', 0xf2: '∫', 0xf3: '⌠', 0xf4: '\uf8f5', 0xf5: '⌡', 0xf6: '\uf8f6', 0xf7: '\uf8f7', 0xf8: '\uf8f8', 0xf9: '\uf8f9', 0xfa: '\uf8fa', 0xfb: '\uf8fb', 0xfc: '\uf8fc', 0xfd: '\uf8fd', 0xfe: '\uf8fe'})

func RuneToGlyph(r rune) (GlyphName, bool) {
	name, ok := runeToGlyphName.Read(r)
	if !ok && r > 0 {
		name = fmt.Sprintf("uni%04x", r)
		ok = true
	}
	return GlyphName(name), ok
}

type differencesEncoding struct {
	base        SimpleEncoder
	differences *charCodeGlyphNameMap
	byteToRune  *syncmap.ByteRuneMap
	runeToByte  *syncmap.RuneByteMap
}

func (m *simpleEncodersMap) Range(f func(name string, newEnc func() SimpleEncoder) (stop bool)) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for name, newEnc := range m.encoders {
		if f(name, newEnc) {
			break
		}
	}
}

func (d simpleDecoder) Transform(dst, src []byte, _ bool) (nDst, nSrc int, _ error) {
	for len(src) != 0 {
		b := src[0]
		src = src[1:]
		r, ok := d.byteToRune.Read(b)
		if !ok {
			r = MissingCodeRune
		}
		if utf8.RuneLen(r) > len(dst) {
			return nDst, nSrc, transform.ErrShortDst
		}
		size := utf8.EncodeRune(dst, r)
		dst = dst[size:]
		nSrc++
		nDst += size
	}
	return nDst, nSrc, nil
}

var (
	_ RuneRegistry        = (*OpenTypeFontEncoder)(nil)
	_ GIDEncoder          = (*OpenTypeFontEncoder)(nil)
	_ ShapedGlyphRegistry = (*OpenTypeFontEncoder)(nil)
)

func (enc UTF16Encoder) Encode(str string) []byte { return []byte(strutils.StringToUTF16(str)) }

var (
	_ GIDRegistry = (*IdentityEncoder)(nil)
	_ GIDEncoder  = (*IdentityEncoder)(nil)
)

func NewIdentityTextEncoder(baseName string) *IdentityEncoder {
	return &IdentityEncoder{name: baseName, usedGIDs: syncmap.MakeUint16Set(0)}
}
func (enc *differencesEncoding) BaseName() string { return enc.base.BaseName() }
func ExpandLigatures(runes []rune) string {
	var buf bytes.Buffer
	for _, r := range runes {
		s := RuneToString(r)
		buf.WriteString(s)
	}
	return buf.String()
}

func (m *charCodeGlyphNameMap) Range(f func(code CharCode, glyph GlyphName) (stop bool)) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for code, glyph := range m.data {
		if f(code, glyph) {
			break
		}
	}
}

func NewMacRomanEncoder() SimpleEncoder                         { return macRomanMapping.NewEncoder() }
func (enc UTF16Encoder) RuneToCharcode(r rune) (CharCode, bool) { return CharCode(r), true }

type singleFontMap struct{ face *font.Face }

func NormalizePresentationForms(text string) string {
	return strings.Map(func(r rune) rune {
		if base, ok := presentationFormToBase.Read(r); ok {
			return base
		}
		return r
	}, text)
}

func (enc *differencesEncoding) CharcodeToRune(code CharCode) (rune, bool) {
	if code > 0xff {
		return MissingCodeRune, false
	}
	b := byte(code)
	if r, ok := enc.byteToRune.Read(b); ok {
		return r, true
	}
	return enc.base.CharcodeToRune(code)
}

func (enc *differencesEncoding) Encode(str string) []byte {
	runes := []rune(str)
	buf := bytes.NewBuffer(nil)
	buf.Grow(len(runes))
	for _, r := range runes {
		code, _ := enc.RuneToCharcode(r)
		buf.WriteByte(byte(code))
	}
	return buf.Bytes()
}
