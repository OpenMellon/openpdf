package fonts

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/textencoding"
)

func initCourierMetrics() {
	const courierGlyphWidth = 600
	courierMetrics = MakeRuneCharSafeMap(len(stdRunes))
	for _, r := range stdRunes {
		courierMetrics.Write(r, CharMetrics{Wx: courierGlyphWidth})
	}
	courierBoldMetrics = courierMetrics.Copy()
	courierBoldObliqueMetrics = courierMetrics.Copy()
	courierObliqueMetrics = courierMetrics.Copy()
}

func (m *RuneCharSafeMap) Copy() *RuneCharSafeMap {
	cp := MakeRuneCharSafeMap(m.Length())
	m.Range(func(r rune, metrics CharMetrics) (stop bool) { cp.m[r] = metrics; return false })
	return cp
}

type Font interface {
	Encoder() textencoding.TextEncoder
	GetRuneMetrics(r rune) (CharMetrics, bool)
}

func initHelveticaMetrics() {
	helveticaMetrics = MakeRuneCharSafeMap(len(stdRunes))
	helveticaBoldMetrics = MakeRuneCharSafeMap(len(stdRunes))
	for i, r := range stdRunes {
		helveticaMetrics.Write(r, CharMetrics{Wx: float64(helveticaWidths[i])})
		helveticaBoldMetrics.Write(r, CharMetrics{Wx: float64(helveticaBoldWidths[i])})
	}
	helveticaObliqueMetrics = helveticaMetrics.Copy()
	helveticaBoldObliqueMetrics = helveticaBoldMetrics.Copy()
}

type FontWeight int

var _ Font = StdFont{}

func readOffset(b []byte) int {
	switch len(b) {
	case 1:
		return int(b[0])
	case 2:
		return int(binary.BigEndian.Uint16(b))
	case 3:
		return int(b[0])<<16 | int(b[1])<<8 | int(b[2])
	case 4:
		return int(binary.BigEndian.Uint32(b))
	}
	return 0
}

type fontMap struct {
	sync.Mutex
	builders map[StdFontName]func() StdFont
}

const (
	FontWeightMedium FontWeight = iota
	FontWeightBold
	FontWeightRoman
)

func (m *RuneCharSafeMap) Range(f func(r rune, metrics CharMetrics) (stop bool)) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for r, metrics := range m.m {
		if f(r, metrics) {
			break
		}
	}
}

func (m *RuneCharSafeMap) Length() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.m)
}

func encodeCharset(charset []uint16) []byte {
	var buf bytes.Buffer
	buf.WriteByte(0)
	for i := 1; i < len(charset); i++ {
		binary.Write(&buf, binary.BigEndian, charset[i])
	}
	return buf.Bytes()
}

func (m *fontMap) write(name StdFontName, builder func() StdFont) {
	m.Lock()
	defer m.Unlock()
	m.builders[name] = builder
}

func parseDictOperand(b []byte) (int32, int) {
	b0 := b[0]
	switch {
	case b0 == 28:
		if len(b) < 3 {
			return 0, 1
		}
		return int32(int16(binary.BigEndian.Uint16(b[1:3]))), 3
	case b0 == 29:
		if len(b) < 5 {
			return 0, 1
		}
		return int32(binary.BigEndian.Uint32(b[1:5])), 5
	case b0 == 30:
		i := 1
		for i < len(b) {
			hi := b[i] >> 4
			lo := b[i] & 0x0f
			if hi == 0xf || lo == 0xf {
				return 0, i + 1
			}
			i++
		}
		return 0, i
	case b0 >= 32 && b0 <= 246:
		return int32(b0) - 139, 1
	case b0 >= 247 && b0 <= 250:
		if len(b) < 2 {
			return 0, 1
		}
		return (int32(b0)-247)*256 + int32(b[1]) + 108, 2
	case b0 >= 251 && b0 <= 254:
		if len(b) < 2 {
			return 0, 1
		}
		return -(int32(b0)-251)*256 - int32(b[1]) - 108, 2
	}
	return 0, 1
}

func RegisterStdFont(name StdFontName, fnc func() StdFont, aliases ...StdFontName) {
	if _, found := stdFonts.read(name); found {
		panic("font already registered: " + string(name))
	}
	stdFonts.write(name, fnc)
	for _, alias := range aliases {
		RegisterStdFont(alias, fnc)
	}
}

func offsetSize(v int) byte {
	if v <= 0xFF {
		return 1
	}
	if v <= 0xFFFF {
		return 2
	}
	if v <= 0xFFFFFF {
		return 3
	}
	return 4
}

const headChecksumAdjustmentOffset = 8

func roundTo(v float64, unit float64) float64 { return math.Round(v/unit) * unit }

func NewFontFile2FromPdfObject(obj core.PdfObject) (TtfType, error) {
	obj = core.TraceToDirectObject(obj)
	stream, ok := obj.(*core.PdfObjectStream)
	if !ok {
		common.Log.Debug("ERROR: FontFile2 must be a stream (%T)", obj)
		return TtfType{}, core.ErrTypeError
	}
	data, err := core.DecodeStream(stream)
	if err != nil {
		return TtfType{}, err
	}
	parser := ttfParser{reader: bytes.NewReader(data)}
	return parser.Parse()
}

func encodeOperator(op int, escape bool) []byte {
	if escape {
		return []byte{12, byte(op)}
	}
	return []byte{byte(op)}
}

type GID = textencoding.GID

type RuneCharSafeMap struct {
	m  map[rune]CharMetrics
	mu sync.RWMutex
}

func init() {
	RegisterStdFont(CourierName, courierFont, "CourierCourierNew", "CourierNew")
	RegisterStdFont(CourierBoldName, courierBoldFont, "CourierNew,Bold")
	RegisterStdFont(CourierObliqueName, courierObliqueFont, "CourierNew,Italic")
	RegisterStdFont(CourierBoldObliqueName, courierBoldObliqueFont, "CourierNew,BoldItalic")
}

func initTimesMetrics() {
	timesMetrics = MakeRuneCharSafeMap(len(stdRunes))
	timesBoldMetrics = MakeRuneCharSafeMap(len(stdRunes))
	timesBoldItalicMetrics = MakeRuneCharSafeMap(len(stdRunes))
	timesItalicMetrics = MakeRuneCharSafeMap(len(stdRunes))
	for i, r := range stdRunes {
		timesMetrics.Write(r, CharMetrics{Wx: float64(timesWidths[i])})
		timesBoldMetrics.Write(r, CharMetrics{Wx: float64(timesBoldWidths[i])})
		timesBoldItalicMetrics.Write(r, CharMetrics{Wx: float64(timesBoldItalicWidths[i])})
		timesItalicMetrics.Write(r, CharMetrics{Wx: float64(timesItalicWidths[i])})
	}
}

type fdSelectData struct {
	format byte
	fds    []byte
}

func (m *RuneCharSafeMap) Write(b rune, r CharMetrics) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.m[b] = r
}

func init() {
	RegisterStdFont(HelveticaName, helveticaFont, "Arial")
	RegisterStdFont(HelveticaBoldName, helveticaBoldFont, "Arial,Bold")
	RegisterStdFont(HelveticaObliqueName, helveticaObliqueFont, "Arial,Italic")
	RegisterStdFont(HelveticaBoldObliqueName, helveticaBoldObliqueFont, "Arial,BoldItalic")
}

func encodeInt32(v int) []byte {
	return []byte{29, byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}

func (m *RuneCharSafeMap) Read(b rune) (CharMetrics, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	metrics, ok := m.m[b]
	return metrics, ok
}

func init() {
	RegisterStdFont(SymbolName, symbolFont, "Symbol,Italic", "Symbol,Bold", "Symbol,BoldItalic")
	RegisterStdFont(ZapfDingbatsName, zapfDingbatsFont)
}

func fixHeadChecksum(data []byte, entryOffset int) {
	if entryOffset == 0 {
		return
	}
	tableOffset := int(binary.BigEndian.Uint32(data[entryOffset+8 : entryOffset+12]))
	tableLength := int(binary.BigEndian.Uint32(data[entryOffset+12 : entryOffset+16]))
	if tableLength < headChecksumAdjustmentOffset+4 || tableOffset > len(data)-tableLength {
		return
	}
	adjust := data[tableOffset+headChecksumAdjustmentOffset : tableOffset+headChecksumAdjustmentOffset+4]
	binary.BigEndian.PutUint32(adjust, 0)
	paddedLen := (tableLength + 3) &^ 3
	binary.BigEndian.PutUint32(data[entryOffset+4:entryOffset+8], tableChecksum(data[tableOffset:tableOffset+paddedLen]))
	binary.BigEndian.PutUint32(adjust, 0xB1B0AFBA-tableChecksum(data))
}

func findSubrsOffset(dict []byte) int {
	var operands [48]int32
	nOperands := 0
	i := 0
	for i < len(dict) {
		b0 := dict[i]
		if b0 >= 32 || b0 == 28 || b0 == 29 || b0 == 30 {
			val, n := parseDictOperand(dict[i:])
			if nOperands < len(operands) {
				operands[nOperands] = val
				nOperands++
			}
			i += n
			continue
		}
		if b0 == 12 {
			nOperands = 0
			i += 2
			continue
		}
		if b0 == 19 {
			if nOperands > 0 {
				return int(operands[nOperands-1])
			}
		}
		nOperands = 0
		i++
	}
	return 0
}

func init() {
	RegisterStdFont(TimesRomanName, timesRomanFont, "TimesNewRoman", "Times")
	RegisterStdFont(TimesBoldName, timesBoldFont, "TimesNewRoman,Bold", "Times,Bold")
	RegisterStdFont(TimesItalicName, timesItalicFont, "TimesNewRoman,Italic", "Times,Italic")
	RegisterStdFont(TimesBoldItalicName, timesBoldItalicFont, "TimesNewRoman,BoldItalic", "Times,BoldItalic")
}

func encodeFontDict(privSize, privOffset int) []byte {
	var buf bytes.Buffer
	buf.Write(encodeInt32(privSize))
	buf.Write(encodeInt32(privOffset))
	buf.Write(encodeOperator(18, false))
	return buf.Bytes()
}

func encodeFDSelect(fds []byte) []byte {
	out := make([]byte, 1+len(fds))
	out[0] = 0
	copy(out[1:], fds)
	return out
}

func readLocalSubrs(data []byte, privOffset, privSize int) ([][]byte, error) {
	if privSize == 0 || privOffset+privSize > len(data) {
		return nil, nil
	}
	privDict := data[privOffset : privOffset+privSize]
	subrsRel := findSubrsOffset(privDict)
	if subrsRel == 0 {
		return nil, nil
	}
	subrsPos := privOffset + subrsRel
	if subrsPos >= len(data) {
		return nil, nil
	}
	r := &cffReader{data: data, pos: subrsPos}
	return r.readIndex()
}

func encodeCFFInt(v int) []byte {
	if v >= -107 && v <= 107 {
		return []byte{byte(v + 139)}
	}
	if v >= 108 && v <= 1131 {
		v -= 108
		return []byte{byte(v/256 + 247), byte(v % 256)}
	}
	if v >= -1131 && v <= -108 {
		v = -v - 108
		return []byte{byte(v/256 + 251), byte(v % 256)}
	}
	if v >= -32768 && v <= 32767 {
		return []byte{28, byte(v >> 8), byte(v)}
	}
	return []byte{29, byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}

func readFixed(b []byte) float64 {
	whole := float64(binary.BigEndian.Uint16(b[0:2]))
	frac := float64(binary.BigEndian.Uint16(b[2:4])) / 65536.0
	return whole + frac
}

type GlyphName = textencoding.GlyphName

func parseFDSelect(data []byte, offset int, numGlyphs uint16) (fdSelectData, error) {
	if offset >= len(data) {
		return fdSelectData{}, errors.New("FDSelect offset out of bounds")
	}
	format := data[offset]
	sel := fdSelectData{format: 0}
	switch format {
	case 0:
		start := offset + 1
		end := start + int(numGlyphs)
		if end > len(data) {
			return fdSelectData{}, errors.New("FDSelect data truncated")
		}
		sel.fds = make([]byte, numGlyphs)
		copy(sel.fds, data[start:end])
	case 3:
		pos := offset + 1
		if pos+2 > len(data) {
			return fdSelectData{}, errors.New("FDSelect format 3 truncated")
		}
		nRanges := int(binary.BigEndian.Uint16(data[pos:]))
		pos += 2
		type range3 struct {
			first uint16
			fd    byte
		}
		ranges := make([]range3, nRanges)
		for i := 0; i < nRanges; i++ {
			if pos+3 > len(data) {
				return fdSelectData{}, errors.New("FDSelect range data truncated")
			}
			ranges[i].first = binary.BigEndian.Uint16(data[pos:])
			ranges[i].fd = data[pos+2]
			pos += 3
		}
		if pos+2 > len(data) {
			return fdSelectData{}, errors.New("FDSelect sentinel truncated")
		}
		sel.fds = make([]byte, numGlyphs)
		for i := 0; i < nRanges; i++ {
			next := numGlyphs
			if i+1 < nRanges {
				next = ranges[i+1].first
			}
			for g := ranges[i].first; g < next; g++ {
				if int(g) < len(sel.fds) {
					sel.fds[g] = ranges[i].fd
				}
			}
		}
	default:
		return fdSelectData{}, fmt.Errorf("unsupported FDSelect format %d", format)
	}
	return sel, nil
}

const headTag = "head"

func (m *fontMap) read(name StdFontName) (func() StdFont, bool) {
	m.Lock()
	defer m.Unlock()
	builder, ok := m.builders[name]
	return builder, ok
}

var macGlyphNames = []GlyphName{".notdef", ".null", "nonmarkingreturn", "space", "exclam", "quotedbl", "numbersign", "dollar", "percent", "ampersand", "quotesingle", "parenleft", "parenright", "asterisk", "plus", "comma", "hyphen", "period", "slash", "zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "colon", "semicolon", "less", "equal", "greater", "question", "at", "A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z", "bracketleft", "backslash", "bracketright", "asciicircum", "underscore", "grave", "a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z", "braceleft", "bar", "braceright", "asciitilde", "Adieresis", "Aring", "Ccedilla", "Eacute", "Ntilde", "Odieresis", "Udieresis", "aacute", "agrave", "acircumflex", "adieresis", "atilde", "aring", "ccedilla", "eacute", "egrave", "ecircumflex", "edieresis", "iacute", "igrave", "icircumflex", "idieresis", "ntilde", "oacute", "ograve", "ocircumflex", "odieresis", "otilde", "uacute", "ugrave", "ucircumflex", "udieresis", "dagger", "degree", "cent", "sterling", "section", "bullet", "paragraph", "germandbls", "registered", "copyright", "trademark", "acute", "dieresis", "notequal", "AE", "Oslash", "infinity", "plusminus", "lessequal", "greaterequal", "yen", "mu", "partialdiff", "summation", "product", "pi", "integral", "ordfeminine", "ordmasculine", "Omega", "ae", "oslash", "questiondown", "exclamdown", "logicalnot", "radical", "florin", "approxequal", "Delta", "guillemotleft", "guillemotright", "ellipsis", "nonbreakingspace", "Agrave", "Atilde", "Otilde", "OE", "oe", "endash", "emdash", "quotedblleft", "quotedblright", "quoteleft", "quoteright", "divide", "lozenge", "ydieresis", "Ydieresis", "fraction", "currency", "guilsinglleft", "guilsinglright", "fi", "fl", "daggerdbl", "periodcentered", "quotesinglbase", "quotedblbase", "perthousand", "Acircumflex", "Ecircumflex", "Aacute", "Edieresis", "Egrave", "Iacute", "Icircumflex", "Idieresis", "Igrave", "Oacute", "Ocircumflex", "apple", "Ograve", "Uacute", "Ucircumflex", "Ugrave", "dotlessi", "circumflex", "tilde", "macron", "breve", "dotaccent", "ring", "cedilla", "hungarumlaut", "ogonek", "caron", "Lslash", "lslash", "Scaron", "scaron", "Zcaron", "zcaron", "brokenbar", "Eth", "eth", "Yacute", "yacute", "Thorn", "thorn", "minus", "multiply", "onesuperior", "twosuperior", "threesuperior", "onehalf", "onequarter", "threequarters", "franc", "Gbreve", "gbreve", "Idotaccent", "Scedilla", "scedilla", "Cacute", "cacute", "Ccaron", "ccaron", "dcroat"}

func encodeIndex(items [][]byte) []byte {
	count := len(items)
	if count == 0 {
		return []byte{0, 0}
	}
	total := 0
	for _, item := range items {
		total += len(item)
	}
	offSize := offsetSize(total + 1)
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(count))
	buf.WriteByte(offSize)
	off := 1
	for i := 0; i <= count; i++ {
		writeOffset(&buf, offSize, off)
		if i < count {
			off += len(items[i])
		}
	}
	for _, item := range items {
		buf.Write(item)
	}
	return buf.Bytes()
}

func MakeRuneCharSafeMap(length int) *RuneCharSafeMap {
	return &RuneCharSafeMap{m: make(map[rune]CharMetrics, length)}
}
