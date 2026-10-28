package fonts

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/internal/textencoding"
)

type StdFontName string

func (ttf *TtfType) String() string {
	return fmt.Sprintf("FONT_FILE2{%#q UnitsPerEm=%d Bold=%t ItalicAngle=%f "+"CapHeight=%d Chars=%d GlyphNames=%d}", ttf.PostScriptName, ttf.UnitsPerEm, ttf.Bold, ttf.ItalicAngle, ttf.CapHeight, len(ttf.Chars), len(ttf.GlyphNames))
}

const ttcTag = "ttcf"

func (f StdFont) Descriptor() Descriptor { return f.desc }

type StdFont struct {
	desc    Descriptor
	metrics *RuneCharSafeMap
	encoder textencoding.TextEncoder
}

func (f StdFont) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	dict.Set("Type", core.MakeName("Font"))
	dict.Set("Subtype", core.MakeName("Type1"))
	dict.Set("BaseFont", core.MakeName(f.Name()))
	dict.Set("Encoding", f.encoder.ToPdfObject())
	return core.MakeIndirectObject(dict)
}

func (ttf *TtfType) NewEncoder() textencoding.TextEncoder {
	return textencoding.NewTrueTypeFontEncoder(ttf.Chars)
}

type TtfType struct {
	UnitsPerEm             uint16
	PostScriptName         string
	Bold                   bool
	ItalicAngle            float64
	IsFixedPitch           bool
	TypoAscender           int16
	TypoDescender          int16
	UnderlinePosition      int16
	UnderlineThickness     int16
	Xmin, Ymin, Xmax, Ymax int16
	CapHeight              int16
	Widths                 []uint16
	Chars                  map[rune]GID
	GlyphNames             []GlyphName
}

func writeOffset(buf *bytes.Buffer, size byte, v int) {
	switch size {
	case 1:
		buf.WriteByte(byte(v))
	case 2:
		binary.Write(buf, binary.BigEndian, uint16(v))
	case 3:
		buf.WriteByte(byte(v >> 16))
		buf.WriteByte(byte(v >> 8))
		buf.WriteByte(byte(v))
	case 4:
		binary.Write(buf, binary.BigEndian, uint32(v))
	}
}

var (
	stdRunes      = []rune{'A', 'Æ', 'Á', 'Ă', 'Â', 'Ä', 'À', 'Ā', 'Ą', 'Å', 'Ã', 'B', 'C', 'Ć', 'Č', 'Ç', 'D', 'Ď', 'Đ', '∆', 'E', 'É', 'Ě', 'Ê', 'Ë', 'Ė', 'È', 'Ē', 'Ę', 'Ð', '€', 'F', 'G', 'Ğ', 'Ģ', 'H', 'I', 'Í', 'Î', 'Ï', 'İ', 'Ì', 'Ī', 'Į', 'J', 'K', 'Ķ', 'L', 'Ĺ', 'Ľ', 'Ļ', 'Ł', 'M', 'N', 'Ń', 'Ň', 'Ņ', 'Ñ', 'O', 'Œ', 'Ó', 'Ô', 'Ö', 'Ò', 'Ő', 'Ō', 'Ø', 'Õ', 'P', 'Q', 'R', 'Ŕ', 'Ř', 'Ŗ', 'S', 'Ś', 'Š', 'Ş', 'Ș', 'T', 'Ť', 'Ţ', 'Þ', 'U', 'Ú', 'Û', 'Ü', 'Ù', 'Ű', 'Ū', 'Ų', 'Ů', 'V', 'W', 'X', 'Y', 'Ý', 'Ÿ', 'Z', 'Ź', 'Ž', 'Ż', 'a', 'á', 'ă', 'â', '´', 'ä', 'æ', 'à', 'ā', '&', 'ą', 'å', '^', '~', '*', '@', 'ã', 'b', '\\', '|', '{', '}', '[', ']', '˘', '¦', '•', 'c', 'ć', 'ˇ', 'č', 'ç', '¸', '¢', 'ˆ', ':', ',', '\uf6c3', '©', '¤', 'd', '†', '‡', 'ď', 'đ', '°', '¨', '÷', '$', '˙', 'ı', 'e', 'é', 'ě', 'ê', 'ë', 'ė', 'è', '8', '…', 'ē', '—', '–', 'ę', '=', 'ð', '!', '¡', 'f', 'ﬁ', '5', 'ﬂ', 'ƒ', '4', '⁄', 'g', 'ğ', 'ģ', 'ß', '`', '>', '≥', '«', '»', '‹', '›', 'h', '˝', '-', 'i', 'í', 'î', 'ï', 'ì', 'ī', 'į', 'j', 'k', 'ķ', 'l', 'ĺ', 'ľ', 'ļ', '<', '≤', '¬', '◊', 'ł', 'm', '¯', '−', 'µ', '×', 'n', 'ń', 'ň', 'ņ', '9', '≠', 'ñ', '#', 'o', 'ó', 'ô', 'ö', 'œ', '˛', 'ò', 'ő', 'ō', '1', '½', '¼', '¹', 'ª', 'º', 'ø', 'õ', 'p', '¶', '(', ')', '∂', '%', '.', '·', '‰', '+', '±', 'q', '?', '¿', '"', '„', '“', '”', '‘', '’', '‚', '\'', 'r', 'ŕ', '√', 'ř', 'ŗ', '®', '˚', 's', 'ś', 'š', 'ş', 'ș', '§', ';', '7', '6', '/', ' ', '£', '∑', 't', 'ť', 'ţ', 'þ', '3', '¾', '³', '˜', '™', '2', '²', 'u', 'ú', 'û', 'ü', 'ù', 'ű', 'ū', '_', 'ų', 'ů', 'v', 'w', 'x', 'y', 'ý', 'ÿ', '¥', 'z', 'ź', 'ž', 'ż', '0'}
	stdFonts      = &fontMap{builders: make(map[StdFontName]func() StdFont)}
	helveticaOnce sync.Once
)

func (f StdFont) GetRuneMetrics(r rune) (CharMetrics, bool) {
	metrics, ok := f.metrics.Read(r)
	return metrics, ok
}

func tableChecksum(data []byte) uint32 {
	var sum uint32
	for i := 0; i+4 <= len(data); i += 4 {
		sum += binary.BigEndian.Uint32(data[i : i+4])
	}
	return sum
}

func NewStdFontByName(name StdFontName) (StdFont, bool) {
	build, ok := stdFonts.read(name)
	if !ok {
		return StdFont{}, false
	}
	return build(), true
}

func (f StdFont) Encoder() textencoding.TextEncoder { return f.encoder }

func SubsetCFF(cffData []byte, keepGIDs []uint16) ([]byte, error) {
	parsed, err := parseCFF(cffData)
	if err != nil {
		return nil, fmt.Errorf("CFF subset parse: %w", err)
	}
	keep := make(map[uint16]bool, len(keepGIDs)+1)
	keep[0] = true
	var maxGID uint16
	for _, gid := range keepGIDs {
		if int(gid) < len(parsed.charStrings) {
			keep[gid] = true
			if gid > maxGID {
				maxGID = gid
			}
		}
	}
	emptyCharString := []byte{0x0e}
	n := int(maxGID) + 1
	charStrings := make([][]byte, n)
	charset := make([]uint16, n)
	for i := 0; i < n; i++ {
		gid := uint16(i)
		if keep[gid] {
			charStrings[i] = parsed.charStrings[gid]
		} else {
			charStrings[i] = emptyCharString
		}
		if int(gid) < len(parsed.charset) {
			charset[i] = parsed.charset[gid]
		}
	}
	var fdSelect []byte
	if parsed.isCID {
		fdSelect = make([]byte, n)
		for i := 0; i < n; i++ {
			fdSelect[i] = parsed.lookupFDSelect(uint16(i))
		}
	}
	return buildCFF(parsed, charStrings, charset, fdSelect)
}

type topDictSubset struct {
	charStringsOffset int
	charsetOffset     int
	fdArrayOffset     int
	fdSelectOffset    int
	privateOffset     int
	privateSize       int
	isCID             bool
}

func IsStdFont(name StdFontName) bool { _, ok := stdFonts.read(name); return ok }

func (f StdFont) Name() string { return string(f.desc.Name) }

func NewStdFont(desc Descriptor, metrics *RuneCharSafeMap) StdFont {
	return NewStdFontWithEncoding(desc, metrics, textencoding.NewStandardEncoder())
}

func stripSubrsOperator(dict []byte) []byte {
	var out bytes.Buffer
	i := 0
	start := 0
	for i < len(dict) {
		b0 := dict[i]
		if b0 >= 32 || b0 == 28 || b0 == 29 || b0 == 30 {
			_, n := parseDictOperand(dict[i:])
			i += n
			continue
		}
		if b0 == 12 {
			if i+2 > len(dict) {
				break
			}
			out.Write(dict[start : i+2])
			i += 2
			start = i
			continue
		}
		if b0 == 19 {
			i++
			start = i
			continue
		}
		out.Write(dict[start : i+1])
		i++
		start = i
	}
	return out.Bytes()
}

const (
	SymbolName       = StdFontName("Symbol")
	ZapfDingbatsName = StdFontName("ZapfDingbats")
)

func symbolFont() StdFont {
	enc := textencoding.NewSymbolEncoder()
	desc := Descriptor{Name: SymbolName, Family: string(SymbolName), Weight: FontWeightMedium, Flags: 0x0004, BBox: [4]float64{-180, -293, 1090, 1010}, ItalicAngle: 0, Ascent: 0, Descent: 0, CapHeight: 0, XHeight: 0, StemV: 85, StemH: 92}
	return NewStdFontWithEncoding(desc, symbolMetrics, enc)
}

func TtfParseFile(fileStr string) (TtfType, error) {
	f, err := os.Open(fileStr)
	if err != nil {
		return TtfType{}, err
	}
	defer f.Close()
	return TtfParse(f)
}

func (ttf *TtfType) MakeToUnicode() *cmap.CMap {
	m := make(map[cmap.CharCode]string)
	if len(ttf.GlyphNames) == 0 {
		for r := range ttf.Chars {
			m[cmap.CharCode(r)] = string(r)
		}
		return cmap.NewToUnicodeCMap(m)
	}
	for r, gid := range ttf.Chars {
		code := cmap.CharCode(r)
		glyphName := ttf.GlyphNames[gid]
		glyphRune, ok := textencoding.GlyphToRune(glyphName)
		if !ok {
			common.Log.Debug("No rune. code=0x%04x glyph=%q", r, glyphName)
			glyphRune = textencoding.MissingCodeRune
		}
		m[code] = string(glyphRune)
	}
	return cmap.NewToUnicodeCMap(m)
}

func SfntFromTTC(data []byte, index int) ([]byte, error) {
	if !IsTTC(data) {
		return nil, errors.New("not a TrueType Collection")
	}
	if len(data) < 12 {
		return nil, errors.New("truncated TrueType Collection header")
	}
	numFonts := int(binary.BigEndian.Uint32(data[8:12]))
	if index < 0 || index >= numFonts {
		return nil, fmt.Errorf("font index %d out of range, collection holds %d", index, numFonts)
	}
	if numFonts > (len(data)-12)/4 {
		return nil, errors.New("truncated TrueType Collection offset table")
	}
	fontOffset := int(binary.BigEndian.Uint32(data[12+4*index : 16+4*index]))
	if fontOffset < 0 || fontOffset > len(data)-12 {
		return nil, errors.New("invalid font offset in TrueType Collection")
	}
	numTables := int(binary.BigEndian.Uint16(data[fontOffset+4 : fontOffset+6]))
	dirStart := fontOffset + 12
	if numTables == 0 || numTables > (len(data)-dirStart)/16 {
		return nil, errors.New("invalid table directory in TrueType Collection")
	}
	type table struct {
		tag  string
		data []byte
	}
	headerSize := 12 + 16*numTables
	total := headerSize
	tables := make([]table, 0, numTables)
	for i := range numTables {
		entry := data[dirStart+16*i : dirStart+16*(i+1)]
		offset := int(binary.BigEndian.Uint32(entry[8:12]))
		length := int(binary.BigEndian.Uint32(entry[12:16]))
		if offset < 0 || length < 0 || offset > len(data) || length > len(data)-offset {
			return nil, fmt.Errorf("table %q of TrueType Collection is out of bounds", entry[:4])
		}
		tables = append(tables, table{tag: string(entry[:4]), data: data[offset : offset+length]})
		total += (length + 3) &^ 3
	}
	out := make([]byte, total)
	copy(out, data[fontOffset:fontOffset+headerSize])
	next, headEntry := headerSize, 0
	for i, t := range tables {
		copy(out[next:], t.data)
		entry := out[12+16*i : 12+16*(i+1)]
		binary.BigEndian.PutUint32(entry[8:12], uint32(next))
		next += (len(t.data) + 3) &^ 3
		if t.tag == headTag {
			headEntry = 12 + 16*i
		}
	}
	fixHeadChecksum(out, headEntry)
	return out, nil
}

func IsTTC(data []byte) bool { return len(data) >= 4 && string(data[:4]) == ttcTag }

func (f StdFont) GetMetricsTable() *RuneCharSafeMap { return f.metrics }

func (ttf *TtfType) MakeEncoder() (textencoding.SimpleEncoder, error) {
	encoding := make(map[textencoding.CharCode]GlyphName)
	for code := textencoding.CharCode(0); code <= 256; code++ {
		r := rune(code)
		gid, ok := ttf.Chars[r]
		if !ok {
			continue
		}
		var glyph GlyphName
		if int(gid) >= 0 && int(gid) < len(ttf.GlyphNames) {
			glyph = ttf.GlyphNames[gid]
		} else {
			uniRune := rune(gid)
			if name, found := textencoding.RuneToGlyph(uniRune); found {
				glyph = name
			}
		}
		if glyph != "" {
			encoding[code] = glyph
		}
	}
	if len(encoding) == 0 {
		common.Log.Debug("WARNING: Zero length TrueType encoding. ttf=%s Chars=[% 02x]", ttf, ttf.Chars)
	}
	return textencoding.NewCustomSimpleTextEncoder(encoding, nil)
}

func TtfParse(r io.ReadSeeker) (TtfType, error) { p := &ttfParser{reader: r}; return p.Parse() }

func NewStdFontWithEncoding(desc Descriptor, metrics *RuneCharSafeMap, encoder textencoding.TextEncoder) StdFont {
	var nbsp rune = 0xA0
	if _, ok := metrics.Read(nbsp); !ok {
		nbspMetrics, _ := metrics.Read(0x20)
		metrics.Write(nbsp, nbspMetrics)
	}
	return StdFont{desc: desc, metrics: metrics, encoder: encoder}
}

func parseTopDict(dict []byte) topDictSubset {
	var top topDictSubset
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
			if i+1 >= len(dict) {
				break
			}
			op := dict[i+1]
			switch op {
			case 30:
				top.isCID = true
			case 36:
				if nOperands > 0 {
					top.fdArrayOffset = int(operands[nOperands-1])
				}
			case 37:
				if nOperands > 0 {
					top.fdSelectOffset = int(operands[nOperands-1])
				}
			}
			nOperands = 0
			i += 2
		} else {
			switch b0 {
			case 15:
				if nOperands > 0 {
					top.charsetOffset = int(operands[nOperands-1])
				}
			case 17:
				if nOperands > 0 {
					top.charStringsOffset = int(operands[nOperands-1])
				}
			case 18:
				if nOperands >= 2 {
					top.privateSize = int(operands[nOperands-2])
					top.privateOffset = int(operands[nOperands-1])
				}
			}
			nOperands = 0
			i++
		}
	}
	return top
}
