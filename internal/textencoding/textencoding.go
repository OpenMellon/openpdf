// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package textencoding

import (
	"encoding/binary"
	"fmt"
	maps0 "maps"
	"slices"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/internal/textencoding/internal/syncmap"
	"github.com/unidoc/typesetting/di"
	"github.com/unidoc/typesetting/font"
	"github.com/unidoc/typesetting/shaping"
	"golang.org/x/image/math/fixed"
	encoding2 "golang.org/x/text/encoding"
)

var (
	_ SimpleEncoder      = (*simpleEncoding)(nil)
	_ encoding2.Encoding = (*simpleEncoding)(nil)
)

func (m singleFontMap) ResolveFace(r rune) *font.Face { return m.face }
func decodeTwoByteCodes(enc TextEncoder, buf []byte) string {
	runes := make([]rune, 0, len(buf)/2+len(buf)%2)
	for len(buf) > 0 {
		if len(buf) == 1 {
			buf = []byte{buf[0], 0}
		}
		code := CharCode(binary.BigEndian.Uint16(buf[:]))
		buf = buf[2:]
		r, ok := enc.CharcodeToRune(code)
		if !ok {
			common.Log.Debug("Failed to map charcode to rune. charcode=%#x", code)
			continue
		}
		runes = append(runes, r)
	}
	return string(runes)
}

func FromFontDifferences(diffList *core.PdfObjectArray) (map[CharCode]GlyphName, error) {
	diffs := make(map[CharCode]GlyphName)
	var code CharCode
	for _, obj := range diffList.Elements() {
		switch v := obj.(type) {
		case *core.PdfObjectInteger:
			code = CharCode(*v)
		case *core.PdfObjectName:
			name := string(*v)
			diffs[code] = GlyphName(name)
			code++
		default:
			common.Log.Debug("ERROR: Bad type. obj=%s", obj)
			return nil, core.ErrTypeError
		}
	}
	return diffs, nil
}

type (
	GID          uint16
	RuneRegistry interface {
		RegisteredRunes() []rune
		SubsetRegistered()
	}
)

func (d simpleDecoder) Reset() {}

var (
	macExpertMapping = newSimpleMapping(macExpertEncodingName, macExpertRunes)
	macRomanMapping  = newSimpleMapping(macRomanEncodingName, macRomanRunes)
)
var macExpertRunes = syncmap.NewByteRuneMap(map[byte]rune{0x20: ' ', 0x21: '\uf721', 0x22: '\uf6f8', 0x23: '\uf7a2', 0x24: '\uf724', 0x25: '\uf6e4', 0x26: '\uf726', 0x27: '\uf7b4', 0x28: '⁽', 0x29: '⁾', 0x2a: '‥', 0x2b: '․', 0x2c: ',', 0x2d: '-', 0x2e: '.', 0x2f: '⁄', 0x30: '\uf730', 0x31: '\uf731', 0x32: '\uf732', 0x33: '\uf733', 0x34: '\uf734', 0x35: '\uf735', 0x36: '\uf736', 0x37: '\uf737', 0x38: '\uf738', 0x39: '\uf739', 0x3a: ':', 0x3b: ';', 0x3d: '\uf6de', 0x3f: '\uf73f', 0x44: '\uf7f0', 0x47: '¼', 0x48: '½', 0x49: '¾', 0x4a: '⅛', 0x4b: '⅜', 0x4c: '⅝', 0x4d: '⅞', 0x4e: '⅓', 0x4f: '⅔', 0x56: 'ﬀ', 0x57: 'ﬁ', 0x58: 'ﬂ', 0x59: 'ﬃ', 0x5a: 'ﬄ', 0x5b: '₍', 0x5d: '₎', 0x5e: '\uf6f6', 0x5f: '\uf6e5', 0x60: '\uf760', 0x61: '\uf761', 0x62: '\uf762', 0x63: '\uf763', 0x64: '\uf764', 0x65: '\uf765', 0x66: '\uf766', 0x67: '\uf767', 0x68: '\uf768', 0x69: '\uf769', 0x6a: '\uf76a', 0x6b: '\uf76b', 0x6c: '\uf76c', 0x6d: '\uf76d', 0x6e: '\uf76e', 0x6f: '\uf76f', 0x70: '\uf770', 0x71: '\uf771', 0x72: '\uf772', 0x73: '\uf773', 0x74: '\uf774', 0x75: '\uf775', 0x76: '\uf776', 0x77: '\uf777', 0x78: '\uf778', 0x79: '\uf779', 0x7a: '\uf77a', 0x7b: '₡', 0x7c: '\uf6dc', 0x7d: '\uf6dd', 0x7e: '\uf6fe', 0x81: '\uf6e9', 0x82: '\uf6e0', 0x87: '\uf7e1', 0x88: '\uf7e0', 0x89: '\uf7e2', 0x8a: '\uf7e4', 0x8b: '\uf7e3', 0x8c: '\uf7e5', 0x8d: '\uf7e7', 0x8e: '\uf7e9', 0x8f: '\uf7e8', 0x90: '\uf7ea', 0x91: '\uf7eb', 0x92: '\uf7ed', 0x93: '\uf7ec', 0x94: '\uf7ee', 0x95: '\uf7ef', 0x96: '\uf7f1', 0x97: '\uf7f3', 0x98: '\uf7f2', 0x99: '\uf7f4', 0x9a: '\uf7f6', 0x9b: '\uf7f5', 0x9c: '\uf7fa', 0x9d: '\uf7f9', 0x9e: '\uf7fb', 0x9f: '\uf7fc', 0xa1: '⁸', 0xa2: '₄', 0xa3: '₃', 0xa4: '₆', 0xa5: '₈', 0xa6: '₇', 0xa7: '\uf6fd', 0xa9: '\uf6df', 0xaa: '₂', 0xac: '\uf7a8', 0xae: '\uf6f5', 0xaf: '\uf6f0', 0xb0: '₅', 0xb2: '\uf6e1', 0xb3: '\uf6e7', 0xb4: '\uf7fd', 0xb6: '\uf6e3', 0xb9: '\uf7fe', 0xbb: '₉', 0xbc: '₀', 0xbd: '\uf6ff', 0xbe: '\uf7e6', 0xbf: '\uf7f8', 0xc0: '\uf7bf', 0xc1: '₁', 0xc2: '\uf6f9', 0xc9: '\uf7b8', 0xcf: '\uf6fa', 0xd0: '‒', 0xd1: '\uf6e6', 0xd6: '\uf7a1', 0xd8: '\uf7ff', 0xda: '¹', 0xdb: '²', 0xdc: '³', 0xdd: '⁴', 0xde: '⁵', 0xdf: '⁶', 0xe0: '⁷', 0xe1: '⁹', 0xe2: '⁰', 0xe4: '\uf6ec', 0xe5: '\uf6f1', 0xe6: '\uf6f3', 0xe9: '\uf6ed', 0xea: '\uf6f2', 0xeb: '\uf6eb', 0xf1: '\uf6ee', 0xf2: '\uf6fb', 0xf3: '\uf6f4', 0xf4: '\uf7af', 0xf5: '\uf6ea', 0xf6: 'ⁿ', 0xf7: '\uf6ef', 0xf8: '\uf6e2', 0xf9: '\uf6e8', 0xfa: '\uf6f7', 0xfb: '\uf6fc'})

func ShapeTextLine(face *font.Face, text string, fontSize float64) shaping.Line {
	if face == nil {
		return nil
	}
	runes := []rune(text)
	input := shaping.Input{Text: runes, RunStart: 0, RunEnd: len(runes), Face: face, Size: fixed.I(int(fontSize * 64)), Direction: di.DirectionLTR}
	segmenter := shaping.Segmenter{}
	shaper := shaping.HarfbuzzShaper{}
	runs := segmenter.Split(input, singleFontMap{face})
	line := make(shaping.Line, len(runs))
	for i, run := range runs {
		line[i] = shaper.Shape(run)
	}
	return line
}
func NewWinAnsiEncoder() SimpleEncoder { return winAnsiMapping.NewEncoder() }
func (enc *TrueTypeFontEncoder) GlyphToCharcode(glyph GlyphName) (CharCode, bool) {
	if len(glyph) == 7 && glyph[0:3] == "uni" {
		var v uint16
		n, err := fmt.Sscanf(string(glyph), "uni%X", &v)
		if n == 1 && err == nil {
			return enc.RuneToCharcode(rune(v))
		}
	}
	if r, ok := glyphNameToRune.Read(string(glyph)); ok {
		return enc.RuneToCharcode(r)
	}
	common.Log.Debug("Symbol encoding error: unable to find glyph->charcode entry (%s)", glyph)
	return 0, false
}

func (enc *IdentityEncoder) CharcodeToRune(code CharCode) (rune, bool) {
	enc.usedGIDs.Write(uint16(code))
	if enc.toUnicode != nil {
		if s, ok := enc.toUnicode.CharcodeToUnicode(cmap.CharCode(code)); ok {
			if runes := []rune(s); len(runes) > 0 {
				return runes[0], true
			}
		}
	}
	return MissingCodeRune, false
}

func makeDifferencesArray(m *charCodeGlyphNameMap) *core.PdfObjectArray {
	if m.Length() == 0 {
		return nil
	}
	codes := make([]CharCode, 0, m.Length())
	m.Range(func(code CharCode, _ GlyphName) (stop bool) {
		codes = append(codes, code)
		return false
	})
	slices.Sort(codes)
	prev := codes[0]
	glyph, _ := m.Read(prev)
	arr := []core.PdfObject{core.MakeInteger(int64(prev)), core.MakeName(string(glyph))}
	for _, code := range codes[1:] {
		if code == prev+1 {
			glyph, _ = m.Read(code)
			arr = append(arr, core.MakeName(string(glyph)))
		} else {
			arr = append(arr, core.MakeInteger(int64(code)))
		}
		prev = code
	}
	return core.MakeArray(arr...)
}

func (m *charCodeGlyphNameMap) Write(k CharCode, v GlyphName) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[k] = v
}

func encodeSingleByte(enc TextEncoder, s string) []byte {
	buf := make([]byte, 0, len(s))
	for _, r := range s {
		code, ok := enc.RuneToCharcode(r)
		if !ok || code > 0xff {
			common.Log.Debug("Failed to map rune to charcode for rune 0x%04x", r)
			continue
		}
		buf = append(buf, byte(code))
	}
	return buf
}

func (enc *simpleEncoding) NewDecoder() *encoding2.Decoder {
	return &encoding2.Decoder{Transformer: simpleDecoder{byteToRune: enc.byteToRune}}
}

func NewOpenTypeFontEncoder(face *font.Face, runeToGIDMap map[rune]GID) *OpenTypeFontEncoder {
	gidMap := syncmap.MakeRuneUint16Map(len(runeToGIDMap))
	for r, gid := range runeToGIDMap {
		gidMap.Write(r, uint16(gid))
	}
	return &OpenTypeFontEncoder{TrueTypeFontEncoder: &TrueTypeFontEncoder{runeToGID: gidMap, usedRunes: syncmap.MakeRuneSet(0)}, face: face, shapedGIDs: syncmap.MakeUint16Set(0)}
}

type TrueTypeFontEncoder struct {
	runeToGID *syncmap.RuneUint16Map
	usedRunes *syncmap.RuneSet
}

func (enc *simpleEncoding) RuneToCharcode(r rune) (CharCode, bool) {
	b, ok := enc.runeToByte.Read(r)
	if enc.usedRunes == nil {
		enc.usedRunes = syncmap.MakeRuneSet(1)
	}
	enc.usedRunes.Write(r)
	return CharCode(b), ok
}

type charCodeGlyphNameTuple struct {
	CharCode  CharCode
	GlyphName GlyphName
}

func (enc *IdentityEncoder) SetCIDToGIDMap(m []uint16) { enc.cidToGID = m }
func NewSymbolEncoder() SimpleEncoder                  { return symbolMapping.NewEncoder() }

var (
	_ RuneRegistry = (*TrueTypeFontEncoder)(nil)
	_ GIDEncoder   = (*TrueTypeFontEncoder)(nil)
)

func NewStandardEncoder() SimpleEncoder { return standardMapping.NewEncoder() }

type TextEncoder interface {
	String() string
	Encode(s string) []byte
	Decode(raw []byte) string
	RuneToCharcode(r rune) (CharCode, bool)
	CharcodeToRune(code CharCode) (rune, bool)
	ToPdfObject() core.PdfObject
}
type CharCode uint16

var zapfDingbatsRunes = syncmap.NewByteRuneMap(map[byte]rune{0x20: ' ', 0x21: '✁', 0x22: '✂', 0x23: '✃', 0x24: '✄', 0x25: '☎', 0x26: '✆', 0x27: '✇', 0x28: '✈', 0x29: '✉', 0x2a: '☛', 0x2b: '☞', 0x2c: '✌', 0x2d: '✍', 0x2e: '✎', 0x2f: '✏', 0x30: '✐', 0x31: '✑', 0x32: '✒', 0x33: '✓', 0x34: '✔', 0x35: '✕', 0x36: '✖', 0x37: '✗', 0x38: '✘', 0x39: '✙', 0x3a: '✚', 0x3b: '✛', 0x3c: '✜', 0x3d: '✝', 0x3e: '✞', 0x3f: '✟', 0x40: '✠', 0x41: '✡', 0x42: '✢', 0x43: '✣', 0x44: '✤', 0x45: '✥', 0x46: '✦', 0x47: '✧', 0x48: '★', 0x49: '✩', 0x4a: '✪', 0x4b: '✫', 0x4c: '✬', 0x4d: '✭', 0x4e: '✮', 0x4f: '✯', 0x50: '✰', 0x51: '✱', 0x52: '✲', 0x53: '✳', 0x54: '✴', 0x55: '✵', 0x56: '✶', 0x57: '✷', 0x58: '✸', 0x59: '✹', 0x5a: '✺', 0x5b: '✻', 0x5c: '✼', 0x5d: '✽', 0x5e: '✾', 0x5f: '✿', 0x60: '❀', 0x61: '❁', 0x62: '❂', 0x63: '❃', 0x64: '❄', 0x65: '❅', 0x66: '❆', 0x67: '❇', 0x68: '❈', 0x69: '❉', 0x6a: '❊', 0x6b: '❋', 0x6c: '●', 0x6d: '❍', 0x6e: '■', 0x6f: '❏', 0x70: '❐', 0x71: '❑', 0x72: '❒', 0x73: '▲', 0x74: '▼', 0x75: '◆', 0x76: '❖', 0x77: '◗', 0x78: '❘', 0x79: '❙', 0x7a: '❚', 0x7b: '❛', 0x7c: '❜', 0x7d: '❝', 0x7e: '❞', 0x80: '\uf8d7', 0x81: '\uf8d8', 0x82: '\uf8d9', 0x83: '\uf8da', 0x84: '\uf8db', 0x85: '\uf8dc', 0x86: '\uf8dd', 0x87: '\uf8de', 0x88: '\uf8df', 0x89: '\uf8e0', 0x8a: '\uf8e1', 0x8b: '\uf8e2', 0x8c: '\uf8e3', 0x8d: '\uf8e4', 0xa1: '❡', 0xa2: '❢', 0xa3: '❣', 0xa4: '❤', 0xa5: '❥', 0xa6: '❦', 0xa7: '❧', 0xa8: '♣', 0xa9: '♦', 0xaa: '♥', 0xab: '♠', 0xac: '①', 0xad: '②', 0xae: '③', 0xaf: '④', 0xb0: '⑤', 0xb1: '⑥', 0xb2: '⑦', 0xb3: '⑧', 0xb4: '⑨', 0xb5: '⑩', 0xb6: '❶', 0xb7: '❷', 0xb8: '❸', 0xb9: '❹', 0xba: '❺', 0xbb: '❻', 0xbc: '❼', 0xbd: '❽', 0xbe: '❾', 0xbf: '❿', 0xc0: '➀', 0xc1: '➁', 0xc2: '➂', 0xc3: '➃', 0xc4: '➄', 0xc5: '➅', 0xc6: '➆', 0xc7: '➇', 0xc8: '➈', 0xc9: '➉', 0xca: '➊', 0xcb: '➋', 0xcc: '➌', 0xcd: '➍', 0xce: '➎', 0xcf: '➏', 0xd0: '➐', 0xd1: '➑', 0xd2: '➒', 0xd3: '➓', 0xd4: '➔', 0xd5: '→', 0xd6: '↔', 0xd7: '↕', 0xd8: '➘', 0xd9: '➙', 0xda: '➚', 0xdb: '➛', 0xdc: '➜', 0xdd: '➝', 0xde: '➞', 0xdf: '➟', 0xe0: '➠', 0xe1: '➡', 0xe2: '➢', 0xe3: '➣', 0xe4: '➤', 0xe5: '➥', 0xe6: '➦', 0xe7: '➧', 0xe8: '➨', 0xe9: '➩', 0xea: '➪', 0xeb: '➫', 0xec: '➬', 0xed: '➭', 0xee: '➮', 0xef: '➯', 0xf1: '➱', 0xf2: '➲', 0xf3: '➳', 0xf4: '➴', 0xf5: '➵', 0xf6: '➶', 0xf7: '➷', 0xf8: '➸', 0xf9: '➹', 0xfa: '➺', 0xfb: '➻', 0xfc: '➼', 0xfd: '➽', 0xfe: '➾'})

func (enc *IdentityEncoder) String() string { return enc.name }

// mergeMaps combines the given maps into one. It lets the large generated lookup tables be
// declared as several smaller literals across files.
func mergeMaps[K comparable, V any](maps ...map[K]V) map[K]V {
	n := 0
	for _, m := range maps {
		n += len(m)
	}
	out := make(map[K]V, n)
	for _, m := range maps {
		maps0.Copy(out, m)
	}
	return out
}
