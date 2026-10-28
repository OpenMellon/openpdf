// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package textencoding

import (
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/textencoding/internal/syncmap"
	"golang.org/x/text/transform"
)

var macRomanRunes = syncmap.NewByteRuneMap(map[byte]rune{0x1: '\x01', 0x2: '\x02', 0x3: '\x03', 0x4: '\x04', 0x5: '\x05', 0x6: '\x06', 0x7: '\a', 0x8: '\b', 0x9: '\t', 0xa: '\n', 0xb: '\v', 0xc: '\f', 0xd: '\r', 0xe: '\x0e', 0xf: '\x0f', 0x10: '\x10', 0x11: '\x11', 0x12: '\x12', 0x13: '\x13', 0x14: '\x14', 0x15: '\x15', 0x16: '\x16', 0x17: '\x17', 0x18: '\x18', 0x19: '\x19', 0x1a: '\x1a', 0x1b: '\x1b', 0x1c: '\x1c', 0x1d: '\x1d', 0x1e: '\x1e', 0x1f: '\x1f', 0x20: ' ', 0x21: '!', 0x22: '"', 0x23: '#', 0x24: '$', 0x25: '%', 0x26: '&', 0x27: '\'', 0x28: '(', 0x29: ')', 0x2a: '*', 0x2b: '+', 0x2c: ',', 0x2d: '-', 0x2e: '.', 0x2f: '/', 0x30: '0', 0x31: '1', 0x32: '2', 0x33: '3', 0x34: '4', 0x35: '5', 0x36: '6', 0x37: '7', 0x38: '8', 0x39: '9', 0x3a: ':', 0x3b: ';', 0x3c: '<', 0x3d: '=', 0x3e: '>', 0x3f: '?', 0x40: '@', 0x41: 'A', 0x42: 'B', 0x43: 'C', 0x44: 'D', 0x45: 'E', 0x46: 'F', 0x47: 'G', 0x48: 'H', 0x49: 'I', 0x4a: 'J', 0x4b: 'K', 0x4c: 'L', 0x4d: 'M', 0x4e: 'N', 0x4f: 'O', 0x50: 'P', 0x51: 'Q', 0x52: 'R', 0x53: 'S', 0x54: 'T', 0x55: 'U', 0x56: 'V', 0x57: 'W', 0x58: 'X', 0x59: 'Y', 0x5a: 'Z', 0x5b: '[', 0x5c: '\\', 0x5d: ']', 0x5e: '^', 0x5f: '_', 0x60: '`', 0x61: 'a', 0x62: 'b', 0x63: 'c', 0x64: 'd', 0x65: 'e', 0x66: 'f', 0x67: 'g', 0x68: 'h', 0x69: 'i', 0x6a: 'j', 0x6b: 'k', 0x6c: 'l', 0x6d: 'm', 0x6e: 'n', 0x6f: 'o', 0x70: 'p', 0x71: 'q', 0x72: 'r', 0x73: 's', 0x74: 't', 0x75: 'u', 0x76: 'v', 0x77: 'w', 0x78: 'x', 0x79: 'y', 0x7a: 'z', 0x7b: '{', 0x7c: '|', 0x7d: '}', 0x7e: '~', 0x7f: '\u007f', 0x80: 'Ä', 0x81: 'Å', 0x82: 'Ç', 0x83: 'É', 0x84: 'Ñ', 0x85: 'Ö', 0x86: 'Ü', 0x87: 'á', 0x88: 'à', 0x89: 'â', 0x8a: 'ä', 0x8b: 'ã', 0x8c: 'å', 0x8d: 'ç', 0x8e: 'é', 0x8f: 'è', 0x90: 'ê', 0x91: 'ë', 0x92: 'í', 0x93: 'ì', 0x94: 'î', 0x95: 'ï', 0x96: 'ñ', 0x97: 'ó', 0x98: 'ò', 0x99: 'ô', 0x9a: 'ö', 0x9b: 'õ', 0x9c: 'ú', 0x9d: 'ù', 0x9e: 'û', 0x9f: 'ü', 0xa0: '†', 0xa1: '°', 0xa2: '¢', 0xa3: '£', 0xa4: '§', 0xa5: '•', 0xa6: '¶', 0xa7: 'ß', 0xa8: '®', 0xa9: '©', 0xaa: '™', 0xab: '´', 0xac: '¨', 0xad: '≠', 0xae: 'Æ', 0xaf: 'Ø', 0xb0: '∞', 0xb1: '±', 0xb2: '≤', 0xb3: '≥', 0xb4: '¥', 0xb5: 'µ', 0xb6: '∂', 0xb7: '∑', 0xb8: '∏', 0xb9: 'π', 0xba: '∫', 0xbb: 'ª', 0xbc: 'º', 0xbd: 'Ω', 0xbe: 'æ', 0xbf: 'ø', 0xc0: '¿', 0xc1: '¡', 0xc2: '¬', 0xc3: '√', 0xc4: 'ƒ', 0xc5: '≈', 0xc6: '∆', 0xc7: '«', 0xc8: '»', 0xc9: '…', 0xca: '\u00a0', 0xcb: 'À', 0xcc: 'Ã', 0xcd: 'Õ', 0xce: 'Œ', 0xcf: 'œ', 0xd0: '–', 0xd1: '—', 0xd2: '“', 0xd3: '”', 0xd4: '‘', 0xd5: '’', 0xd6: '÷', 0xd7: '◊', 0xd8: 'ÿ', 0xd9: 'Ÿ', 0xda: '⁄', 0xdb: '€', 0xdc: '‹', 0xdd: '›', 0xde: 'ﬁ', 0xdf: 'ﬂ', 0xe0: '‡', 0xe1: '·', 0xe2: '‚', 0xe3: '„', 0xe4: '‰', 0xe5: 'Â', 0xe6: 'Ê', 0xe7: 'Á', 0xe8: 'Ë', 0xe9: 'È', 0xea: 'Í', 0xeb: 'Î', 0xec: 'Ï', 0xed: 'Ì', 0xee: 'Ó', 0xef: 'Ô', 0xf0: '\uf8ff', 0xf1: 'Ò', 0xf2: 'Ú', 0xf3: 'Û', 0xf4: 'Ù', 0xf5: 'ı', 0xf6: 'ˆ', 0xf7: '˜', 0xf8: '¯', 0xf9: '˘', 0xfa: '˙', 0xfb: '˚', 0xfc: '¸', 0xfd: '˝', 0xfe: '˛', 0xff: 'ˇ'})

func (e *simpleEncoder) Transform(dst, src []byte, atEOF bool) (nDst, nSrc int, _ error) {
	for len(src) != 0 {
		if !utf8.FullRune(src) && !atEOF {
			return nDst, nSrc, transform.ErrShortSrc
		} else if len(dst) == 0 {
			return nDst, nSrc, transform.ErrShortDst
		}
		r, size := utf8.DecodeRune(src)
		if r == utf8.RuneError {
			r = MissingCodeRune
		}
		src = src[size:]
		nSrc += size
		b, ok := e.runeToByte.Read(r)
		if !ok {
			b, _ = e.runeToByte.Read(MissingCodeRune)
		}
		dst[0] = b
		dst = dst[1:]
		nDst++
	}
	return nDst, nSrc, nil
}

func (enc *simpleEncoding) CharcodeToRune(code CharCode) (rune, bool) {
	if code > 0xff {
		return MissingCodeRune, false
	}
	b := byte(code)
	r, ok := enc.byteToRune.Read(b)
	if enc.usedRunes == nil {
		enc.usedRunes = syncmap.MakeRuneSet(1)
	}
	enc.usedRunes.Write(r)
	return r, ok
}
func NewPdfDocEncoder() SimpleEncoder { return pdfDocMapping.NewEncoder() }

var standardRunes = syncmap.NewByteRuneMap(map[byte]rune{0x20: ' ', 0x21: '!', 0x22: '"', 0x23: '#', 0x24: '$', 0x25: '%', 0x26: '&', 0x27: '’', 0x28: '(', 0x29: ')', 0x2a: '*', 0x2b: '+', 0x2c: ',', 0x2d: '-', 0x2e: '.', 0x2f: '/', 0x30: '0', 0x31: '1', 0x32: '2', 0x33: '3', 0x34: '4', 0x35: '5', 0x36: '6', 0x37: '7', 0x38: '8', 0x39: '9', 0x3a: ':', 0x3b: ';', 0x3c: '<', 0x3d: '=', 0x3e: '>', 0x3f: '?', 0x40: '@', 0x41: 'A', 0x42: 'B', 0x43: 'C', 0x44: 'D', 0x45: 'E', 0x46: 'F', 0x47: 'G', 0x48: 'H', 0x49: 'I', 0x4a: 'J', 0x4b: 'K', 0x4c: 'L', 0x4d: 'M', 0x4e: 'N', 0x4f: 'O', 0x50: 'P', 0x51: 'Q', 0x52: 'R', 0x53: 'S', 0x54: 'T', 0x55: 'U', 0x56: 'V', 0x57: 'W', 0x58: 'X', 0x59: 'Y', 0x5a: 'Z', 0x5b: '[', 0x5c: '\\', 0x5d: ']', 0x5e: '^', 0x5f: '_', 0x60: '‘', 0x61: 'a', 0x62: 'b', 0x63: 'c', 0x64: 'd', 0x65: 'e', 0x66: 'f', 0x67: 'g', 0x68: 'h', 0x69: 'i', 0x6a: 'j', 0x6b: 'k', 0x6c: 'l', 0x6d: 'm', 0x6e: 'n', 0x6f: 'o', 0x70: 'p', 0x71: 'q', 0x72: 'r', 0x73: 's', 0x74: 't', 0x75: 'u', 0x76: 'v', 0x77: 'w', 0x78: 'x', 0x79: 'y', 0x7a: 'z', 0x7b: '{', 0x7c: '|', 0x7d: '}', 0x7e: '~', 0xa1: '¡', 0xa2: '¢', 0xa3: '£', 0xa4: '⁄', 0xa5: '¥', 0xa6: 'ƒ', 0xa7: '§', 0xa8: '¤', 0xa9: '\'', 0xaa: '“', 0xab: '«', 0xac: '‹', 0xad: '›', 0xae: 'ﬁ', 0xaf: 'ﬂ', 0xb1: '–', 0xb2: '†', 0xb3: '‡', 0xb4: '·', 0xb6: '¶', 0xb7: '•', 0xb8: '‚', 0xb9: '„', 0xba: '”', 0xbb: '»', 0xbc: '…', 0xbd: '‰', 0xbf: '¿', 0xc1: '`', 0xc2: '´', 0xc3: 'ˆ', 0xc4: '˜', 0xc5: '¯', 0xc6: '˘', 0xc7: '˙', 0xc8: '¨', 0xca: '˚', 0xcb: '¸', 0xcc: '˝', 0xcd: '˛', 0xce: 'ˇ', 0xcf: '—', 0xe1: 'Æ', 0xe3: 'ª', 0xe8: 'Ł', 0xe9: 'Ø', 0xea: 'Œ', 0xeb: 'º', 0xf1: 'æ', 0xf5: 'ı', 0xf8: 'ł', 0xf9: 'ø', 0xfa: 'œ', 0xfb: 'ß'})

type simpleEncodersMap struct {
	encoders map[string]func() SimpleEncoder
	mu       sync.RWMutex
}

var glyphNameAliases = syncmap.NewStringsMap(glyphNameAliasTuples[:])

func (enc *TrueTypeFontEncoder) RegisteredRunes() []rune {
	runes := make([]rune, enc.usedRunes.Length())
	i := 0
	enc.usedRunes.Range(func(r rune) (stop bool) {
		runes[i] = r
		i++
		return false
	})
	slices.Sort(runes)
	return runes
}
func (enc *simpleEncoding) String() string { return "simpleEncoding(" + enc.name + ")" }
func (m *charCodeGlyphNameMap) Delete(key CharCode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
}

func (enc *TrueTypeFontEncoder) GIDToCharcode(gid GID) (CharCode, bool) {
	return CharCode(gid), true
}

func (m *charCodeGlyphNameMap) RangeDelete(f func(code CharCode, glyph GlyphName) (del bool, stop bool)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for code, glyph := range m.data {
		del, stop := f(code, glyph)
		if del {
			delete(m.data, code)
		}
		if stop {
			break
		}
	}
}
func NewUTF16TextEncoder(baseName string) UTF16Encoder { return UTF16Encoder{baseName} }

type SimpleEncoder interface {
	TextEncoder
	BaseName() string
	Charcodes() []CharCode
}
type charCodeGlyphNameMap struct {
	data map[CharCode]GlyphName
	mu   sync.RWMutex
}

func (enc *IdentityEncoder) GIDToCharcode(gid GID) (CharCode, bool) { return CharCode(gid), true }
func (e *simpleEncoder) Reset()                                     {}
func (enc *simpleEncoding) Charcodes() []CharCode {
	codes := make([]CharCode, 0, enc.byteToRune.Length())
	enc.byteToRune.Range(func(b byte, _ rune) (stop bool) { codes = append(codes, CharCode(b)); return false })
	slices.Sort(codes)
	return codes
}

func (enc CMapEncoder) ToPdfObject() core.PdfObject {
	if enc.name != "" {
		return core.MakeName(enc.name)
	}
	return core.MakeNull()
}

func NormalizeRunePresentationForms(r rune) rune {
	if base, ok := presentationFormToBase.Read(r); ok {
		return base
	}
	return r
}
func NewMacExpertEncoder() SimpleEncoder { return macExpertMapping.NewEncoder() }
func GlyphToRune(glyph GlyphName) (rune, bool) {
	name := string(glyph)
	if strings.Contains(name, ".") {
		match := glyphSuffixRe.FindStringSubmatch(name)
		if match != nil {
			name = match[1]
		}
	}
	if alias, ok := glyphNameAliases.Read(name); ok {
		name = alias
	}
	if r, ok := glyphNameToRune.Read(name); ok {
		return r, true
	}
	if r, ok := ligatureGlyphToRune.Read(name); ok {
		return r, true
	}
	if match := glyphUniRe.FindStringSubmatch(name); match != nil {
		code, err := strconv.ParseInt(match[1], 16, 32)
		if err == nil {
			return rune(code), true
		}
	}
	if match := glyphLetterDigitsRe.FindStringSubmatch(name); match != nil {
		code, err := strconv.Atoi(match[1])
		if err == nil {
			return rune(code), true
		}
	}
	return rune(0), false
}
func (enc *TrueTypeFontEncoder) CharcodeToGID(code CharCode) (GID, bool) { return GID(code), true }
func (enc UTF16Encoder) ToPdfObject() core.PdfObject {
	if enc.name != "" {
		return core.MakeName(enc.name)
	}
	return core.MakeNull()
}

func NewSimpleTextEncoder(baseName string, differences map[CharCode]GlyphName) (SimpleEncoder, error) {
	newEnc, ok := simpleEncoders.Read(baseName)
	if !ok {
		common.Log.Debug("ERROR: NewSimpleTextEncoder. Unknown encoding %q", baseName)
		return nil, nil
	}
	enc := newEnc()
	if len(differences) != 0 {
		enc = ApplyDifferences(enc, differences)
	}
	return enc, nil
}

func (enc *TrueTypeFontEncoder) CharcodeToRune(code CharCode) (rune, bool) {
	found := rune(-1)
	enc.runeToGID.Range(func(r rune, gid uint16) (stop bool) {
		c := CharCode(gid)
		if c == code {
			found = r
			return true
		}
		return false
	})
	if found != -1 {
		return found, true
	}
	common.Log.Debug("CharcodeToRune: No match. code=0x%04x enc=%s", code, enc)
	return 0, false
}

func newSimpleMapping(name string, byteToRune *syncmap.ByteRuneMap) *simpleMapping {
	return &simpleMapping{name: name, byteToRune: byteToRune}
}

func (enc CMapEncoder) CharcodeToRune(code CharCode) (rune, bool) {
	s, ok := enc.charcodeToString(code)
	if len(s) < 1 {
		return MissingCodeRune, false
	}
	return []rune(s)[0], ok
}

func (enc *TrueTypeFontEncoder) RuneToGID(r rune) (GID, bool) {
	gid, ok := enc.runeToGID.Read(r)
	if !ok {
		common.Log.Debug("Missing rune %d (%+q) from encoding", r, r)
		return 0, false
	}
	enc.usedRunes.Write(r)
	return GID(gid), true
}
