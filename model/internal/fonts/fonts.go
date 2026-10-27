// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package fonts

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/typesetting/font"
	"github.com/OpenMellon/typesetting/font/cff"
	"github.com/OpenMellon/typesetting/font/opentype"
	"github.com/OpenMellon/typesetting/font/opentype/tables"
	"github.com/OpenMellon/typesetting/shaping"
	"golang.org/x/image/math/fixed"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
)

func courierBoldObliqueFont() StdFont {
	courierOnce.Do(initCourierMetrics)
	desc := Descriptor{Name: CourierBoldObliqueName, Family: string(CourierName), Weight: FontWeightBold, Flags: 0x0061, BBox: [4]float64{-57, -250, 869, 801}, ItalicAngle: -12, Ascent: 629, Descent: -157, CapHeight: 562, XHeight: 439, StemV: 106, StemH: 84}
	return NewStdFont(desc, courierBoldObliqueMetrics)
}
func buildCFF(parsed *cffParsed, charStrings [][]byte, charset []uint16, fdSelect []byte) ([]byte, error) {
	stringIndex := encodeIndex(parsed.strings)
	globalSubrIndex := encodeIndex(parsed.globalSubrs)
	charStringsIndex := encodeIndex(charStrings)
	charsetData := encodeCharset(charset)
	var fdSelectData []byte
	if parsed.isCID && fdSelect != nil {
		fdSelectData = encodeFDSelect(fdSelect)
	}
	type privateDictInfo struct {
		data []byte
		size int
	}
	privDicts := make([]privateDictInfo, parsed.numFDs)
	for fd := 0; fd < parsed.numFDs; fd++ {
		var privBuf bytes.Buffer
		privDict := parsed.privateDicts[fd]
		if privDict == nil {
			privDict = []byte{}
		}
		localSubrs := parsed.localSubrs[fd]
		if len(localSubrs) > 0 {
			stripped := stripSubrsOperator(privDict)
			subrsOffset := len(stripped) + 6
			var subrsOp bytes.Buffer
			subrsOp.Write(encodeInt32(subrsOffset))
			subrsOp.Write(encodeOperator(19, false))
			privBuf.Write(stripped)
			privBuf.Write(subrsOp.Bytes())
			privSize := privBuf.Len()
			privBuf.Write(encodeIndex(localSubrs))
			privDicts[fd] = privateDictInfo{data: privBuf.Bytes(), size: privSize}
		} else {
			stripped := stripSubrsOperator(privDict)
			privBuf.Write(stripped)
			privDicts[fd] = privateDictInfo{data: privBuf.Bytes(), size: privBuf.Len()}
		}
	}
	nameIndex := encodeIndex([][]byte{parsed.fontName})
	topDictStub := buildTopDictPlaceholder(parsed.isCID)
	topDictIndexSize := len(encodeIndex([][]byte{topDictStub}))
	headerSize := 4
	afterGlobalSubrs := headerSize + len(nameIndex) + topDictIndexSize + len(stringIndex) + len(globalSubrIndex)
	charStringsOffset := afterGlobalSubrs
	charsetOffset := charStringsOffset + len(charStringsIndex)
	fdSelectOffset := charsetOffset + len(charsetData)
	var fdArray []byte
	var fdArrayOffset int
	privOffsets := make([]int, parsed.numFDs)
	privSizes := make([]int, parsed.numFDs)
	if parsed.isCID {
		fdArrayOffset = fdSelectOffset + len(fdSelectData)
		stubDicts := make([][]byte, parsed.numFDs)
		for i := range stubDicts {
			stubDicts[i] = encodeFontDict(0, 0)
		}
		fdArraySize := len(encodeIndex(stubDicts))
		privStart := fdArrayOffset + fdArraySize
		privPos := privStart
		for fd := 0; fd < parsed.numFDs; fd++ {
			privOffsets[fd] = privPos
			privSizes[fd] = privDicts[fd].size
			privPos += len(privDicts[fd].data)
		}
		fontDicts := make([][]byte, parsed.numFDs)
		for fd := range fontDicts {
			fontDicts[fd] = encodeFontDict(privSizes[fd], privOffsets[fd])
		}
		fdArray = encodeIndex(fontDicts)
		if len(fdArray) != fdArraySize {
			return nil, fmt.Errorf("FDArray INDEX size mismatch: expected %d, got %d", fdArraySize, len(fdArray))
		}
	} else {
		offset := fdSelectOffset
		privOffsets[0] = offset
		privSizes[0] = privDicts[0].size
	}
	topDict := buildTopDict(parsed.isCID, charStringsOffset, charsetOffset, fdSelectOffset, fdArrayOffset, privOffsets, privSizes)
	topDictIndex := encodeIndex([][]byte{topDict})
	if len(topDictIndex) != topDictIndexSize {
		return nil, fmt.Errorf("Top DICT INDEX size mismatch: placeholder=%d, actual=%d", topDictIndexSize, len(topDictIndex))
	}
	var out bytes.Buffer
	out.Write([]byte{1, 0, 4, 4})
	out.Write(nameIndex)
	out.Write(topDictIndex)
	out.Write(stringIndex)
	out.Write(globalSubrIndex)
	out.Write(charStringsIndex)
	out.Write(charsetData)
	if parsed.isCID {
		out.Write(fdSelectData)
		out.Write(fdArray)
	}
	for fd := 0; fd < parsed.numFDs; fd++ {
		out.Write(privDicts[fd].data)
	}
	return out.Bytes(), nil
}
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

type StdFontName string

func (m *RuneCharSafeMap) Copy() *RuneCharSafeMap {
	cp := MakeRuneCharSafeMap(m.Length())
	m.Range(func(r rune, metrics CharMetrics) (stop bool) { cp.m[r] = metrics; return false })
	return cp
}
func (p *ttfParser) Parse() (TtfType, error) {
	version, err := p.ReadStr(4)
	if err != nil {
		return TtfType{}, err
	}
	if version == ttcTag {
		return p.parseTTC()
	} else if version != "\x00\x01\x00\x00" && version != "true" {
		common.Log.Debug("Unrecognized TrueType file format. version=%q", version)
	}
	numTables := int(p.ReadUShort())
	p.Skip(3 * 2)
	p.tableOffsets = make(map[string]uint32)
	var tag string
	for range numTables {
		tag, err = p.ReadStr(4)
		if err != nil {
			return TtfType{}, err
		}
		p.Skip(4)
		offset := p.ReadULong()
		p.Skip(4)
		p.tableOffsets[tag] = offset
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace(describeTables(p.tableOffsets))
	}
	if err = p.ParseComponents(); err != nil {
		return TtfType{}, err
	}
	return p.ttf, nil
}

const (
	CourierName            = StdFontName("Courier")
	CourierBoldName        = StdFontName("Courier-Bold")
	CourierObliqueName     = StdFontName("Courier-Oblique")
	CourierBoldObliqueName = StdFontName("Courier-BoldOblique")
)

type Font interface {
	Encoder() textencoding.TextEncoder
	GetRuneMetrics(r rune) (CharMetrics, bool)
}

var helveticaBoldMetrics *RuneCharSafeMap
var timesBoldMetrics *RuneCharSafeMap

func (f *OtfType) GetGlyphExtents(glyphID font.GID) (font.GlyphExtents, bool) {
	if f.Face != nil {
		return f.Face.GlyphExtents(glyphID)
	}
	return font.GlyphExtents{}, false
}
func (ttf *TtfType) String() string {
	return fmt.Sprintf("FONT_FILE2{%#q UnitsPerEm=%d Bold=%t ItalicAngle=%f "+"CapHeight=%d Chars=%d GlyphNames=%d}", ttf.PostScriptName, ttf.UnitsPerEm, ttf.Bold, ttf.ItalicAngle, ttf.CapHeight, len(ttf.Chars), len(ttf.GlyphNames))
}
func (p *ttfParser) parseCmapFormat12() error {
	numGroups := p.ReadULong()
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("parseCmapFormat12: %s numGroups=%d", p.ttf.String(), numGroups)
	}
	for i := uint32(0); i < numGroups; i++ {
		startCode := p.ReadULong()
		endCode := p.ReadULong()
		startGID := p.ReadULong()
		if startCode > 0x0010FFFF || (0xD800 <= startCode && startCode <= 0xDFFF) {
			return errors.New("invalid characters codes")
		}
		if endCode < startCode || endCode > 0x0010FFFF || (0xD800 <= endCode && endCode <= 0xDFFF) {
			return errors.New("invalid characters codes")
		}
		for code := startCode; code <= endCode; code++ {
			if code > 0x10FFFF {
				common.Log.Debug("Format 12 cmap contains character beyond UCS-4")
			}
			p.ttf.Chars[rune(code)] = GID(startGID)
			startGID++
		}
	}
	return nil
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

const ttcTag = "ttcf"

func (f *OtfType) GetBitmapSizes() []font.BitmapSize {
	if f.Font != nil {
		return f.Font.BitmapSizes()
	}
	return nil
}

type FontWeight int

var _ Font = StdFont{}

func (f *OtfType) GetGlyphAdvance(glyphID font.GID) fixed.Int26_6 {
	if f.Face != nil {
		adv := f.Face.HorizontalAdvance(glyphID)
		return fixed.Int26_6(adv * 64)
	}
	return 0
}

var helveticaObliqueMetrics *RuneCharSafeMap

func (p *ttfParser) Seek(tag string) error {
	offset, ok := p.tableOffsets[tag]
	if !ok {
		return fmt.Errorf("table not found: %s", tag)
	}
	p.reader.Seek(int64(offset), io.SeekStart)
	return nil
}
func (p *otfParser) parsePost() error {
	table := p.readTable("post")
	if L := len(table); L < 32 {
		return fmt.Errorf("reading Post: "+"EOF: expected length: 32, got %d", L)
	}
	formatType := roundTo(readFixed(table[0:]), 1)
	p.otf.ItalicAngle = readFixed(table[4:])
	p.otf.UnderlinePosition = int16(binary.BigEndian.Uint16(table[8:]))
	p.otf.UnderlineThickness = int16(binary.BigEndian.Uint16(table[12:]))
	p.otf.IsFixedPitch = (binary.BigEndian.Uint16(table[16:]) & 1) != 0
	switch formatType {
	case 1.0:
		p.otf.GlyphNames = macGlyphNames
	case 2.0:
		names, _, err := tables.ParsePostNames20(table[32:])
		if err != nil {
			return err
		}
		glyphNames := make([]GlyphName, len(names.Strings))
		for i, s := range names.Strings {
			glyphNames[i] = GlyphName(s)
		}
		p.otf.GlyphNames = append(p.otf.GlyphNames, glyphNames...)
	case 3.0:
		common.Log.Debug("No PostScript name information is provided for the font.")
	default:
		common.Log.Debug("ERROR: Unknown formatType=%f", formatType)
	}
	return nil
}
func timesBoldItalicFont() StdFont {
	timesOnce.Do(initTimesMetrics)
	desc := Descriptor{Name: TimesBoldItalicName, Family: timesFamily, Weight: FontWeightBold, Flags: 0x0060, BBox: [4]float64{-200, -218, 996, 921}, ItalicAngle: -15, Ascent: 683, Descent: -217, CapHeight: 669, XHeight: 462, StemV: 121, StemH: 42}
	return NewStdFont(desc, timesBoldItalicMetrics)
}
func (f StdFont) Descriptor() Descriptor { return f.desc }
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

func (p *otfParser) isImageFont() bool {
	if _, err := p.loader.RawTable(opentype.MustNewTag("EBDT")); err == nil {
		return true
	}
	if _, err := p.loader.RawTable(opentype.MustNewTag("EBLC")); err == nil {
		return true
	}
	if _, err := p.loader.RawTable(opentype.MustNewTag("bloc")); err == nil {
		return true
	}
	if _, err := p.loader.RawTable(opentype.MustNewTag("bdat")); err == nil {
		return true
	}
	if _, err := p.loader.RawTable(opentype.MustNewTag("sbix")); err == nil {
		return true
	}
	if _, err := p.loader.RawTable(opentype.MustNewTag("CBDT")); err == nil {
		return true
	}
	if _, err := p.loader.RawTable(opentype.MustNewTag("CBLC")); err == nil {
		return true
	}
	if _, err := p.loader.RawTable(opentype.MustNewTag("SVG ")); err == nil {
		return true
	}
	return false
}

type StdFont struct {
	desc    Descriptor
	metrics *RuneCharSafeMap
	encoder textencoding.TextEncoder
}

func (m *RuneCharSafeMap) Range(f func(r rune, metrics CharMetrics) (stop bool)) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for r, metrics := range m.m {
		if f(r, metrics) {
			break
		}
	}
}
func (p *otfParser) parseHead() error {
	p.otf.UnitsPerEm = p.font.Upem()
	head, _, err := tables.ParseHead(p.readTable("head"))
	if err != nil {
		return err
	}
	p.otf.IndexToLocFormat = head.IndexToLocFormat
	p.otf.Xmin = head.XMin
	p.otf.Xmax = head.XMax
	p.otf.Ymin = head.YMin
	p.otf.Ymax = head.YMax
	return nil
}
func (p *ttfParser) ReadShort() (v int16) {
	binary.Read(p.reader, binary.BigEndian, &v)
	return v
}
func (p *ttfParser) Skip(n int) { p.reader.Seek(int64(n), io.SeekCurrent) }
func (f StdFont) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	dict.Set("Type", core.MakeName("Font"))
	dict.Set("Subtype", core.MakeName("Type1"))
	dict.Set("BaseFont", core.MakeName(f.Name()))
	dict.Set("Encoding", f.encoder.ToPdfObject())
	return core.MakeIndirectObject(dict)
}
func (m *RuneCharSafeMap) Length() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.m)
}
func helveticaObliqueFont() StdFont {
	helveticaOnce.Do(initHelveticaMetrics)
	desc := Descriptor{Name: HelveticaObliqueName, Family: string(HelveticaName), Weight: FontWeightMedium, Flags: 0x0060, BBox: [4]float64{-170, -225, 1116, 931}, ItalicAngle: -12, Ascent: 718, Descent: -207, CapHeight: 718, XHeight: 523, StemV: 88, StemH: 76}
	return NewStdFont(desc, helveticaObliqueMetrics)
}

var helveticaBoldObliqueMetrics *RuneCharSafeMap

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

type Descriptor struct {
	Name        StdFontName
	Family      string
	Weight      FontWeight
	Flags       uint
	BBox        [4]float64
	ItalicAngle float64
	Ascent      float64
	Descent     float64
	CapHeight   float64
	XHeight     float64
	StemV       float64
	StemH       float64
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
func (ttf *TtfType) NewEncoder() textencoding.TextEncoder {
	return textencoding.NewTrueTypeFontEncoder(ttf.Chars)
}
func (p *otfParser) parseCmap() error {
	p.otf.Chars = map[rune]GID{}
	it := p.font.Cmap.Iter()
	for it.Next() {
		r, gid := it.Char()
		p.otf.Chars[r] = GID(gid)
	}
	return nil
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

var timesBoldItalicWidths = []int16{667, 944, 667, 667, 667, 667, 667, 667, 667, 667, 667, 667, 667, 667, 667, 667, 722, 722, 722, 612, 667, 667, 667, 667, 667, 667, 667, 667, 667, 722, 500, 667, 722, 722, 722, 778, 389, 389, 389, 389, 389, 389, 389, 389, 500, 667, 667, 611, 611, 611, 611, 611, 889, 722, 722, 722, 722, 722, 722, 944, 722, 722, 722, 722, 722, 722, 722, 722, 611, 722, 667, 667, 667, 667, 556, 556, 556, 556, 556, 611, 611, 611, 611, 722, 722, 722, 722, 722, 722, 722, 722, 722, 667, 889, 667, 611, 611, 611, 611, 611, 611, 611, 500, 500, 500, 500, 333, 500, 722, 500, 500, 778, 500, 500, 570, 570, 500, 832, 500, 500, 278, 220, 348, 348, 333, 333, 333, 220, 350, 444, 444, 333, 444, 444, 333, 500, 333, 333, 250, 250, 747, 500, 500, 500, 500, 608, 500, 400, 333, 570, 500, 333, 278, 444, 444, 444, 444, 444, 444, 444, 500, 1000, 444, 1000, 500, 444, 570, 500, 389, 389, 333, 556, 500, 556, 500, 500, 167, 500, 500, 500, 500, 333, 570, 549, 500, 500, 333, 333, 556, 333, 333, 278, 278, 278, 278, 278, 278, 278, 278, 500, 500, 278, 278, 382, 278, 570, 549, 606, 494, 278, 778, 333, 606, 576, 570, 556, 556, 556, 556, 500, 549, 556, 500, 500, 500, 500, 500, 722, 333, 500, 500, 500, 500, 750, 750, 300, 266, 300, 500, 500, 500, 500, 333, 333, 494, 833, 250, 250, 1000, 570, 570, 500, 500, 500, 555, 500, 500, 500, 333, 333, 333, 278, 389, 389, 549, 389, 389, 747, 333, 389, 389, 389, 389, 389, 500, 333, 500, 500, 278, 250, 500, 600, 278, 366, 278, 500, 500, 750, 300, 333, 1000, 500, 300, 556, 556, 556, 556, 556, 556, 556, 500, 556, 556, 444, 667, 500, 444, 444, 444, 500, 389, 389, 389, 389, 500}

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

func (f *OtfType) ShapeText(text string, fontSize float64) shaping.Line {
	if f.Face == nil {
		return nil
	}
	return textencoding.ShapeTextLine(f.Face, text, fontSize)
}
func (f *OtfType) IsColorGlyph(glyphID font.GID) bool {
	if f.Face != nil {
		data := f.Face.GlyphData(glyphID)
		switch data.(type) {
		case *font.GlyphSVG, *font.GlyphBitmap:
			return true
		default:
			return false
		}
	}
	return false
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

const (
	HelveticaName            = StdFontName("Helvetica")
	HelveticaBoldName        = StdFontName("Helvetica-Bold")
	HelveticaObliqueName     = StdFontName("Helvetica-Oblique")
	HelveticaBoldObliqueName = StdFontName("Helvetica-BoldOblique")
)

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

var stdRunes = []rune{'A', 'Æ', 'Á', 'Ă', 'Â', 'Ä', 'À', 'Ā', 'Ą', 'Å', 'Ã', 'B', 'C', 'Ć', 'Č', 'Ç', 'D', 'Ď', 'Đ', '∆', 'E', 'É', 'Ě', 'Ê', 'Ë', 'Ė', 'È', 'Ē', 'Ę', 'Ð', '€', 'F', 'G', 'Ğ', 'Ģ', 'H', 'I', 'Í', 'Î', 'Ï', 'İ', 'Ì', 'Ī', 'Į', 'J', 'K', 'Ķ', 'L', 'Ĺ', 'Ľ', 'Ļ', 'Ł', 'M', 'N', 'Ń', 'Ň', 'Ņ', 'Ñ', 'O', 'Œ', 'Ó', 'Ô', 'Ö', 'Ò', 'Ő', 'Ō', 'Ø', 'Õ', 'P', 'Q', 'R', 'Ŕ', 'Ř', 'Ŗ', 'S', 'Ś', 'Š', 'Ş', 'Ș', 'T', 'Ť', 'Ţ', 'Þ', 'U', 'Ú', 'Û', 'Ü', 'Ù', 'Ű', 'Ū', 'Ų', 'Ů', 'V', 'W', 'X', 'Y', 'Ý', 'Ÿ', 'Z', 'Ź', 'Ž', 'Ż', 'a', 'á', 'ă', 'â', '´', 'ä', 'æ', 'à', 'ā', '&', 'ą', 'å', '^', '~', '*', '@', 'ã', 'b', '\\', '|', '{', '}', '[', ']', '˘', '¦', '•', 'c', 'ć', 'ˇ', 'č', 'ç', '¸', '¢', 'ˆ', ':', ',', '\uf6c3', '©', '¤', 'd', '†', '‡', 'ď', 'đ', '°', '¨', '÷', '$', '˙', 'ı', 'e', 'é', 'ě', 'ê', 'ë', 'ė', 'è', '8', '…', 'ē', '—', '–', 'ę', '=', 'ð', '!', '¡', 'f', 'ﬁ', '5', 'ﬂ', 'ƒ', '4', '⁄', 'g', 'ğ', 'ģ', 'ß', '`', '>', '≥', '«', '»', '‹', '›', 'h', '˝', '-', 'i', 'í', 'î', 'ï', 'ì', 'ī', 'į', 'j', 'k', 'ķ', 'l', 'ĺ', 'ľ', 'ļ', '<', '≤', '¬', '◊', 'ł', 'm', '¯', '−', 'µ', '×', 'n', 'ń', 'ň', 'ņ', '9', '≠', 'ñ', '#', 'o', 'ó', 'ô', 'ö', 'œ', '˛', 'ò', 'ő', 'ō', '1', '½', '¼', '¹', 'ª', 'º', 'ø', 'õ', 'p', '¶', '(', ')', '∂', '%', '.', '·', '‰', '+', '±', 'q', '?', '¿', '"', '„', '“', '”', '‘', '’', '‚', '\'', 'r', 'ŕ', '√', 'ř', 'ŗ', '®', '˚', 's', 'ś', 'š', 'ş', 'ș', '§', ';', '7', '6', '/', ' ', '£', '∑', 't', 'ť', 'ţ', 'þ', '3', '¾', '³', '˜', '™', '2', '²', 'u', 'ú', 'û', 'ü', 'ù', 'ű', 'ū', '_', 'ų', 'ů', 'v', 'w', 'x', 'y', 'ý', 'ÿ', '¥', 'z', 'ź', 'ž', 'ż', '0'}
var stdFonts = &fontMap{builders: make(map[StdFontName]func() StdFont)}
var helveticaOnce sync.Once

func helveticaFont() StdFont {
	helveticaOnce.Do(initHelveticaMetrics)
	desc := Descriptor{Name: HelveticaName, Family: string(HelveticaName), Weight: FontWeightMedium, Flags: 0x0020, BBox: [4]float64{-166, -225, 1000, 931}, ItalicAngle: 0, Ascent: 718, Descent: -207, CapHeight: 718, XHeight: 523, StemV: 88, StemH: 76}
	return NewStdFont(desc, helveticaMetrics)
}

var timesOnce sync.Once

func (p *ttfParser) ParseOS2() error {
	if err := p.Seek("OS/2"); err != nil {
		return err
	}
	version := p.ReadUShort()
	p.Skip(4 * 2)
	p.Skip(11*2 + 10 + 4*4 + 4)
	fsSelection := p.ReadUShort()
	p.ttf.Bold = (fsSelection & 32) != 0
	p.Skip(2 * 2)
	p.ttf.TypoAscender = p.ReadShort()
	p.ttf.TypoDescender = p.ReadShort()
	if version >= 2 {
		p.Skip(3*2 + 2*4 + 2)
		p.ttf.CapHeight = p.ReadShort()
	} else {
		p.ttf.CapHeight = 0
	}
	return nil
}
func (f StdFont) GetRuneMetrics(r rune) (CharMetrics, bool) {
	metrics, ok := f.metrics.Read(r)
	return metrics, ok
}
func (p *ttfParser) ParseCmap() error {
	var offset int64
	if err := p.Seek("cmap"); err != nil {
		return err
	}
	p.ReadUShort()
	numTables := int(p.ReadUShort())
	macOffset := int64(0)
	winOffset := int64(0)
	ucs4Offset := int64(0)
	for i := 0; i < numTables; i++ {
		platformID := p.ReadUShort()
		encodingID := p.ReadUShort()
		offset = int64(p.ReadULong())
		if platformID == 3 && encodingID == 1 {
			winOffset = offset
		} else if platformID == 3 && encodingID == 10 {
			ucs4Offset = offset
		} else if platformID == 1 && encodingID == 0 {
			macOffset = offset
		}
	}
	if macOffset != 0 {
		if err := p.parseCmapVersion(macOffset); err != nil {
			return err
		}
	}
	if winOffset != 0 {
		if err := p.parseCmapSubtable31(winOffset); err != nil {
			return err
		}
	}
	if ucs4Offset != 0 {
		if err := p.parseCmapVersion(ucs4Offset); err != nil {
			return err
		}
	}
	if winOffset == 0 && macOffset == 0 && ucs4Offset == 0 {
		common.Log.Debug("ttfParser.ParseCmap. No 31, 10, 310 table.")
	}
	return nil
}
func tableChecksum(data []byte) uint32 {
	var sum uint32
	for i := 0; i+4 <= len(data); i += 4 {
		sum += binary.BigEndian.Uint32(data[i : i+4])
	}
	return sum
}
func (p *ttfParser) Read32Fixed() float64 {
	whole := float64(p.ReadShort())
	frac := float64(p.ReadUShort()) / 65536.0
	return whole + frac
}
func NewStdFontByName(name StdFontName) (StdFont, bool) {
	build, ok := stdFonts.read(name)
	if !ok {
		return StdFont{}, false
	}
	return build(), true
}
func (p *otfParser) parseHmtx() error {
	hmtx, _, err := tables.ParseHmtx(p.readTable("hmtx"), int(p.numHMetrics), int(p.numGlyphs)-int(p.numHMetrics))
	if err != nil {
		return err
	}
	for _, m := range hmtx.Metrics {
		p.otf.Widths = append(p.otf.Widths, uint16(m.AdvanceWidth))
	}
	return nil
}
func (p *ttfParser) parseTTC() (TtfType, error) {
	p.Skip(2 * 2)
	numFonts := p.ReadULong()
	if numFonts < 1 {
		return TtfType{}, errors.New("no fonts in TTC file")
	}
	offset := p.ReadULong()
	_, err := p.reader.Seek(int64(offset), io.SeekStart)
	if err != nil {
		return TtfType{}, err
	}
	return p.Parse()
}

const headChecksumAdjustmentOffset = 8

func (f StdFont) Encoder() textencoding.TextEncoder { return f.encoder }

type otfParser struct {
	otf         OtfType
	reader      io.ReadSeeker
	loader      *opentype.Loader
	font        *font.Font
	fontPage    tables.FontPage
	numHMetrics uint16
	numGlyphs   uint16
}

func roundTo(v float64, unit float64) float64 { return math.Round(v/unit) * unit }
func (m CharMetrics) String() string          { return fmt.Sprintf("<%.1f,%.1f>", m.Wx, m.Wy) }
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
func (p *ttfParser) parseCmapSubtable10(offset int64) error {
	if p.ttf.Chars == nil {
		p.ttf.Chars = make(map[rune]GID)
	}
	p.reader.Seek(int64(p.tableOffsets["cmap"])+offset, io.SeekStart)
	var length, language uint32
	format := p.ReadUShort()
	if format < 8 {
		length = uint32(p.ReadUShort())
		language = uint32(p.ReadUShort())
	} else {
		p.ReadUShort()
		length = p.ReadULong()
		language = p.ReadULong()
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("parseCmapSubtable10: format=%d length=%d language=%d", format, length, language)
	}
	if format != 0 {
		return errors.New("unsupported cmap subtable format")
	}
	s, err := p.ReadStr(256)
	if err != nil {
		return err
	}
	glyphIDs := []byte(s)
	for code, gid := range glyphIDs {
		p.ttf.Chars[rune(code)] = GID(gid)
		if gid != 0 {
			fmt.Printf("\t0x%02x ➞ 0x%02x=%c\n", code, gid, rune(gid))
		}
	}
	return nil
}
func encodeOperator(op int, escape bool) []byte {
	if escape {
		return []byte{12, byte(op)}
	}
	return []byte{byte(op)}
}

type GID = textencoding.GID

func (p *ttfParser) parseCmapFormat6() error {
	firstCode := int(p.ReadUShort())
	entryCount := int(p.ReadUShort())
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("parseCmapFormat6: %s firstCode=%d entryCount=%d", p.ttf.String(), firstCode, entryCount)
	}
	for i := 0; i < entryCount; i++ {
		gid := GID(p.ReadUShort())
		p.ttf.Chars[rune(i+firstCode)] = gid
	}
	return nil
}

var zapfDingbatsMetrics = &RuneCharSafeMap{m: map[rune]CharMetrics{' ': {Wx: 278}, '→': {Wx: 838}, '↔': {Wx: 1016}, '↕': {Wx: 458}, '①': {Wx: 788}, '②': {Wx: 788}, '③': {Wx: 788}, '④': {Wx: 788}, '⑤': {Wx: 788}, '⑥': {Wx: 788}, '⑦': {Wx: 788}, '⑧': {Wx: 788}, '⑨': {Wx: 788}, '⑩': {Wx: 788}, '■': {Wx: 761}, '▲': {Wx: 892}, '▼': {Wx: 892}, '◆': {Wx: 788}, '●': {Wx: 791}, '◗': {Wx: 438}, '★': {Wx: 816}, '☎': {Wx: 719}, '☛': {Wx: 960}, '☞': {Wx: 939}, '♠': {Wx: 626}, '♣': {Wx: 776}, '♥': {Wx: 694}, '♦': {Wx: 595}, '✁': {Wx: 974}, '✂': {Wx: 961}, '✃': {Wx: 974}, '✄': {Wx: 980}, '✆': {Wx: 789}, '✇': {Wx: 790}, '✈': {Wx: 791}, '✉': {Wx: 690}, '✌': {Wx: 549}, '✍': {Wx: 855}, '✎': {Wx: 911}, '✏': {Wx: 933}, '✐': {Wx: 911}, '✑': {Wx: 945}, '✒': {Wx: 974}, '✓': {Wx: 755}, '✔': {Wx: 846}, '✕': {Wx: 762}, '✖': {Wx: 761}, '✗': {Wx: 571}, '✘': {Wx: 677}, '✙': {Wx: 763}, '✚': {Wx: 760}, '✛': {Wx: 759}, '✜': {Wx: 754}, '✝': {Wx: 494}, '✞': {Wx: 552}, '✟': {Wx: 537}, '✠': {Wx: 577}, '✡': {Wx: 692}, '✢': {Wx: 786}, '✣': {Wx: 788}, '✤': {Wx: 788}, '✥': {Wx: 790}, '✦': {Wx: 793}, '✧': {Wx: 794}, '✩': {Wx: 823}, '✪': {Wx: 789}, '✫': {Wx: 841}, '✬': {Wx: 823}, '✭': {Wx: 833}, '✮': {Wx: 816}, '✯': {Wx: 831}, '✰': {Wx: 923}, '✱': {Wx: 744}, '✲': {Wx: 723}, '✳': {Wx: 749}, '✴': {Wx: 790}, '✵': {Wx: 792}, '✶': {Wx: 695}, '✷': {Wx: 776}, '✸': {Wx: 768}, '✹': {Wx: 792}, '✺': {Wx: 759}, '✻': {Wx: 707}, '✼': {Wx: 708}, '✽': {Wx: 682}, '✾': {Wx: 701}, '✿': {Wx: 826}, '❀': {Wx: 815}, '❁': {Wx: 789}, '❂': {Wx: 789}, '❃': {Wx: 707}, '❄': {Wx: 687}, '❅': {Wx: 696}, '❆': {Wx: 689}, '❇': {Wx: 786}, '❈': {Wx: 787}, '❉': {Wx: 713}, '❊': {Wx: 791}, '❋': {Wx: 785}, '❍': {Wx: 873}, '❏': {Wx: 762}, '❐': {Wx: 762}, '❑': {Wx: 759}, '❒': {Wx: 759}, '❖': {Wx: 784}, '❘': {Wx: 138}, '❙': {Wx: 277}, '❚': {Wx: 415}, '❛': {Wx: 392}, '❜': {Wx: 392}, '❝': {Wx: 668}, '❞': {Wx: 668}, '❡': {Wx: 732}, '❢': {Wx: 544}, '❣': {Wx: 544}, '❤': {Wx: 910}, '❥': {Wx: 667}, '❦': {Wx: 760}, '❧': {Wx: 760}, '❶': {Wx: 788}, '❷': {Wx: 788}, '❸': {Wx: 788}, '❹': {Wx: 788}, '❺': {Wx: 788}, '❻': {Wx: 788}, '❼': {Wx: 788}, '❽': {Wx: 788}, '❾': {Wx: 788}, '❿': {Wx: 788}, '➀': {Wx: 788}, '➁': {Wx: 788}, '➂': {Wx: 788}, '➃': {Wx: 788}, '➄': {Wx: 788}, '➅': {Wx: 788}, '➆': {Wx: 788}, '➇': {Wx: 788}, '➈': {Wx: 788}, '➉': {Wx: 788}, '➊': {Wx: 788}, '➋': {Wx: 788}, '➌': {Wx: 788}, '➍': {Wx: 788}, '➎': {Wx: 788}, '➏': {Wx: 788}, '➐': {Wx: 788}, '➑': {Wx: 788}, '➒': {Wx: 788}, '➓': {Wx: 788}, '➔': {Wx: 894}, '➘': {Wx: 748}, '➙': {Wx: 924}, '➚': {Wx: 748}, '➛': {Wx: 918}, '➜': {Wx: 927}, '➝': {Wx: 928}, '➞': {Wx: 928}, '➟': {Wx: 834}, '➠': {Wx: 873}, '➡': {Wx: 828}, '➢': {Wx: 924}, '➣': {Wx: 924}, '➤': {Wx: 917}, '➥': {Wx: 930}, '➦': {Wx: 931}, '➧': {Wx: 463}, '➨': {Wx: 883}, '➩': {Wx: 836}, '➪': {Wx: 836}, '➫': {Wx: 867}, '➬': {Wx: 867}, '➭': {Wx: 696}, '➮': {Wx: 696}, '➯': {Wx: 874}, '➱': {Wx: 874}, '➲': {Wx: 760}, '➳': {Wx: 946}, '➴': {Wx: 771}, '➵': {Wx: 865}, '➶': {Wx: 771}, '➷': {Wx: 888}, '➸': {Wx: 967}, '➹': {Wx: 888}, '➺': {Wx: 831}, '➻': {Wx: 873}, '➼': {Wx: 927}, '➽': {Wx: 970}, '➾': {Wx: 918}, '\uf8d7': {Wx: 390}, '\uf8d8': {Wx: 390}, '\uf8d9': {Wx: 317}, '\uf8da': {Wx: 317}, '\uf8db': {Wx: 276}, '\uf8dc': {Wx: 276}, '\uf8dd': {Wx: 509}, '\uf8de': {Wx: 509}, '\uf8df': {Wx: 410}, '\uf8e0': {Wx: 410}, '\uf8e1': {Wx: 234}, '\uf8e2': {Wx: 234}, '\uf8e3': {Wx: 334}, '\uf8e4': {Wx: 334}}}

func (p *otfParser) parseHhea() error {
	hhea, _, err := tables.ParseHhea(p.readTable("hhea"))
	if err != nil {
		return err
	}
	p.numHMetrics = hhea.NumOfLongMetrics
	return nil
}
func courierObliqueFont() StdFont {
	courierOnce.Do(initCourierMetrics)
	desc := Descriptor{Name: CourierObliqueName, Family: string(CourierName), Weight: FontWeightMedium, Flags: 0x0061, BBox: [4]float64{-27, -250, 849, 805}, ItalicAngle: -12, Ascent: 629, Descent: -157, CapHeight: 562, XHeight: 426, StemV: 51, StemH: 51}
	return NewStdFont(desc, courierObliqueMetrics)
}
func OtfParse(r io.ReadSeeker) (OtfType, error) { p := &otfParser{reader: r}; return p.Parse() }

type RuneCharSafeMap struct {
	m  map[rune]CharMetrics
	mu sync.RWMutex
}

func buildTopDictPlaceholder(isCID bool) []byte {
	var buf bytes.Buffer
	buf.Write(encodeInt32(0))
	buf.Write(encodeOperator(15, false))
	buf.Write(encodeInt32(0))
	buf.Write(encodeOperator(17, false))
	if isCID {
		buf.Write(encodeCFFInt(0))
		buf.Write(encodeCFFInt(0))
		buf.Write(encodeCFFInt(0))
		buf.Write(encodeOperator(30, true))
		buf.Write(encodeInt32(0))
		buf.Write(encodeOperator(37, true))
		buf.Write(encodeInt32(0))
		buf.Write(encodeOperator(36, true))
	} else {
		buf.Write(encodeInt32(0))
		buf.Write(encodeInt32(0))
		buf.Write(encodeOperator(18, false))
	}
	return buf.Bytes()
}

var courierOnce sync.Once

func (p *ttfParser) ReadUShort() (v uint16) {
	binary.Read(p.reader, binary.BigEndian, &v)
	return v
}
func (p *ttfParser) parseCmapVersion(offset int64) error {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("parseCmapVersion: offset=%d", offset)
	}
	if p.ttf.Chars == nil {
		p.ttf.Chars = make(map[rune]GID)
	}
	p.reader.Seek(int64(p.tableOffsets["cmap"])+offset, io.SeekStart)
	var length, language uint32
	format := p.ReadUShort()
	if format < 8 {
		length = uint32(p.ReadUShort())
		language = uint32(p.ReadUShort())
	} else {
		p.ReadUShort()
		length = p.ReadULong()
		language = p.ReadULong()
	}
	common.Log.Debug("parseCmapVersion: format=%d length=%d language=%d", format, length, language)
	switch format {
	case 0:
		return p.parseCmapFormat0()
	case 6:
		return p.parseCmapFormat6()
	case 12:
		return p.parseCmapFormat12()
	default:
		common.Log.Debug("ERROR: Unsupported cmap format=%d", format)
		return nil
	}
}
func (p *ttfParser) ReadULong() (v uint32) {
	binary.Read(p.reader, binary.BigEndian, &v)
	return v
}

type cffParsed struct {
	fontName     []byte
	strings      [][]byte
	globalSubrs  [][]byte
	charStrings  [][]byte
	charset      []uint16
	localSubrs   [][][]byte
	isCID        bool
	fdSelect     fdSelectData
	numFDs       int
	privateDicts [][]byte
}

func (f *OtfType) NewEncoder() textencoding.TextEncoder {
	return textencoding.NewOpenTypeFontEncoder(f.Face, f.Chars)
}
func (p *ttfParser) ReadSByte() (v int8) {
	binary.Read(p.reader, binary.BigEndian, &v)
	return v
}
func init() {
	RegisterStdFont(CourierName, courierFont, "CourierCourierNew", "CourierNew")
	RegisterStdFont(CourierBoldName, courierBoldFont, "CourierNew,Bold")
	RegisterStdFont(CourierObliqueName, courierObliqueFont, "CourierNew,Italic")
	RegisterStdFont(CourierBoldObliqueName, courierBoldObliqueFont, "CourierNew,BoldItalic")
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

func (p *ttfParser) readByte() (v uint8) {
	binary.Read(p.reader, binary.BigEndian, &v)
	return v
}

var helveticaBoldWidths = []int16{722, 1000, 722, 722, 722, 722, 722, 722, 722, 722, 722, 722, 722, 722, 722, 722, 722, 722, 722, 612, 667, 667, 667, 667, 667, 667, 667, 667, 667, 722, 556, 611, 778, 778, 778, 722, 278, 278, 278, 278, 278, 278, 278, 278, 556, 722, 722, 611, 611, 611, 611, 611, 833, 722, 722, 722, 722, 722, 778, 1000, 778, 778, 778, 778, 778, 778, 778, 778, 667, 778, 722, 722, 722, 722, 667, 667, 667, 667, 667, 611, 611, 611, 667, 722, 722, 722, 722, 722, 722, 722, 722, 722, 667, 944, 667, 667, 667, 667, 611, 611, 611, 611, 556, 556, 556, 556, 333, 556, 889, 556, 556, 722, 556, 556, 584, 584, 389, 975, 556, 611, 278, 280, 389, 389, 333, 333, 333, 280, 350, 556, 556, 333, 556, 556, 333, 556, 333, 333, 278, 250, 737, 556, 611, 556, 556, 743, 611, 400, 333, 584, 556, 333, 278, 556, 556, 556, 556, 556, 556, 556, 556, 1000, 556, 1000, 556, 556, 584, 611, 333, 333, 333, 611, 556, 611, 556, 556, 167, 611, 611, 611, 611, 333, 584, 549, 556, 556, 333, 333, 611, 333, 333, 278, 278, 278, 278, 278, 278, 278, 278, 556, 556, 278, 278, 400, 278, 584, 549, 584, 494, 278, 889, 333, 584, 611, 584, 611, 611, 611, 611, 556, 549, 611, 556, 611, 611, 611, 611, 944, 333, 611, 611, 611, 556, 834, 834, 333, 370, 365, 611, 611, 611, 556, 333, 333, 494, 889, 278, 278, 1000, 584, 584, 611, 611, 611, 474, 500, 500, 500, 278, 278, 278, 238, 389, 389, 549, 389, 389, 737, 333, 556, 556, 556, 556, 556, 556, 333, 556, 556, 278, 278, 556, 600, 333, 389, 333, 611, 556, 834, 333, 333, 1000, 556, 333, 611, 611, 611, 611, 611, 611, 611, 556, 611, 611, 556, 778, 556, 556, 556, 556, 556, 500, 500, 500, 500, 556}

func IsStdFont(name StdFontName) bool { _, ok := stdFonts.read(name); return ok }

const (
	timesFamily         = "Times"
	TimesRomanName      = StdFontName("Times-Roman")
	TimesBoldName       = StdFontName("Times-Bold")
	TimesItalicName     = StdFontName("Times-Italic")
	TimesBoldItalicName = StdFontName("Times-BoldItalic")
)

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

var helveticaWidths = []int16{667, 1000, 667, 667, 667, 667, 667, 667, 667, 667, 667, 667, 722, 722, 722, 722, 722, 722, 722, 612, 667, 667, 667, 667, 667, 667, 667, 667, 667, 722, 556, 611, 778, 778, 778, 722, 278, 278, 278, 278, 278, 278, 278, 278, 500, 667, 667, 556, 556, 556, 556, 556, 833, 722, 722, 722, 722, 722, 778, 1000, 778, 778, 778, 778, 778, 778, 778, 778, 667, 778, 722, 722, 722, 722, 667, 667, 667, 667, 667, 611, 611, 611, 667, 722, 722, 722, 722, 722, 722, 722, 722, 722, 667, 944, 667, 667, 667, 667, 611, 611, 611, 611, 556, 556, 556, 556, 333, 556, 889, 556, 556, 667, 556, 556, 469, 584, 389, 1015, 556, 556, 278, 260, 334, 334, 278, 278, 333, 260, 350, 500, 500, 333, 500, 500, 333, 556, 333, 278, 278, 250, 737, 556, 556, 556, 556, 643, 556, 400, 333, 584, 556, 333, 278, 556, 556, 556, 556, 556, 556, 556, 556, 1000, 556, 1000, 556, 556, 584, 556, 278, 333, 278, 500, 556, 500, 556, 556, 167, 556, 556, 556, 611, 333, 584, 549, 556, 556, 333, 333, 556, 333, 333, 222, 278, 278, 278, 278, 278, 222, 222, 500, 500, 222, 222, 299, 222, 584, 549, 584, 471, 222, 833, 333, 584, 556, 584, 556, 556, 556, 556, 556, 549, 556, 556, 556, 556, 556, 556, 944, 333, 556, 556, 556, 556, 834, 834, 333, 370, 365, 611, 556, 556, 537, 333, 333, 476, 889, 278, 278, 1000, 584, 584, 556, 556, 611, 355, 333, 333, 333, 222, 222, 222, 191, 333, 333, 453, 333, 333, 737, 333, 500, 500, 500, 500, 500, 556, 278, 556, 556, 278, 278, 556, 600, 278, 317, 278, 556, 556, 834, 333, 333, 1000, 556, 333, 556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 500, 722, 500, 500, 500, 500, 556, 500, 500, 500, 500, 556}

type ttfParser struct {
	ttf          TtfType
	reader       io.ReadSeeker
	tableOffsets map[string]uint32
	numHMetrics  uint16
	numGlyphs    uint16
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
func timesItalicFont() StdFont {
	timesOnce.Do(initTimesMetrics)
	desc := Descriptor{Name: TimesItalicName, Family: timesFamily, Weight: FontWeightMedium, Flags: 0x0060, BBox: [4]float64{-169, -217, 1010, 883}, ItalicAngle: -15.5, Ascent: 683, Descent: -217, CapHeight: 653, XHeight: 441, StemV: 76, StemH: 32}
	return NewStdFont(desc, timesItalicMetrics)
}
func init() {
	RegisterStdFont(HelveticaName, helveticaFont, "Arial")
	RegisterStdFont(HelveticaBoldName, helveticaBoldFont, "Arial,Bold")
	RegisterStdFont(HelveticaObliqueName, helveticaObliqueFont, "Arial,Italic")
	RegisterStdFont(HelveticaBoldObliqueName, helveticaBoldObliqueFont, "Arial,BoldItalic")
}
func (p *ttfParser) ParseHead() error {
	if err := p.Seek("head"); err != nil {
		return err
	}
	p.Skip(3 * 4)
	magic := p.ReadULong()
	if magic != 0x5F0F3CF5 {
		common.Log.Debug("ERROR: Incorrect magic number. Font may not display correctly. %s", p)
	}
	p.Skip(2)
	p.ttf.UnitsPerEm = p.ReadUShort()
	p.Skip(2 * 8)
	p.ttf.Xmin = p.ReadShort()
	p.ttf.Ymin = p.ReadShort()
	p.ttf.Xmax = p.ReadShort()
	p.ttf.Ymax = p.ReadShort()
	return nil
}
func (p *otfParser) parseName() error {
	name, _, err := tables.ParseName(p.readTable("name"))
	if err != nil {
		return err
	}
	p.otf.PostScriptName = name.Name(6)
	return nil
}

type OtfType struct {
	*TtfType
	Loader           *opentype.Loader
	Font             *font.Font
	Face             *font.Face
	IndexToLocFormat int16
	Glyf             tables.Glyf
	CFF              *cff.CFF
	CFF2             *cff.CFF2
	OutlineType      OTFOutlineType
	RawData          []byte
}

func encodeInt32(v int) []byte {
	return []byte{29, byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}
func (f StdFont) Name() string { return string(f.desc.Name) }
func (m *RuneCharSafeMap) Read(b rune) (CharMetrics, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	metrics, ok := m.m[b]
	return metrics, ok
}

var timesItalicWidths = []int16{611, 889, 611, 611, 611, 611, 611, 611, 611, 611, 611, 611, 667, 667, 667, 667, 722, 722, 722, 612, 611, 611, 611, 611, 611, 611, 611, 611, 611, 722, 500, 611, 722, 722, 722, 722, 333, 333, 333, 333, 333, 333, 333, 333, 444, 667, 667, 556, 556, 611, 556, 556, 833, 667, 667, 667, 667, 667, 722, 944, 722, 722, 722, 722, 722, 722, 722, 722, 611, 722, 611, 611, 611, 611, 500, 500, 500, 500, 500, 556, 556, 556, 611, 722, 722, 722, 722, 722, 722, 722, 722, 722, 611, 833, 611, 556, 556, 556, 556, 556, 556, 556, 500, 500, 500, 500, 333, 500, 667, 500, 500, 778, 500, 500, 422, 541, 500, 920, 500, 500, 278, 275, 400, 400, 389, 389, 333, 275, 350, 444, 444, 333, 444, 444, 333, 500, 333, 333, 250, 250, 760, 500, 500, 500, 500, 544, 500, 400, 333, 675, 500, 333, 278, 444, 444, 444, 444, 444, 444, 444, 500, 889, 444, 889, 500, 444, 675, 500, 333, 389, 278, 500, 500, 500, 500, 500, 167, 500, 500, 500, 500, 333, 675, 549, 500, 500, 333, 333, 500, 333, 333, 278, 278, 278, 278, 278, 278, 278, 278, 444, 444, 278, 278, 300, 278, 675, 549, 675, 471, 278, 722, 333, 675, 500, 675, 500, 500, 500, 500, 500, 549, 500, 500, 500, 500, 500, 500, 667, 333, 500, 500, 500, 500, 750, 750, 300, 276, 310, 500, 500, 500, 523, 333, 333, 476, 833, 250, 250, 1000, 675, 675, 500, 500, 500, 420, 556, 556, 556, 333, 333, 333, 214, 389, 389, 453, 389, 389, 760, 333, 389, 389, 389, 389, 389, 500, 333, 500, 500, 278, 250, 500, 600, 278, 300, 278, 500, 500, 750, 300, 333, 980, 500, 300, 500, 500, 500, 500, 500, 500, 500, 500, 500, 500, 444, 667, 444, 444, 444, 444, 500, 389, 389, 389, 389, 500}
var symbolMetrics = &RuneCharSafeMap{m: map[rune]CharMetrics{' ': {Wx: 250}, '!': {Wx: 333}, '#': {Wx: 500}, '%': {Wx: 833}, '&': {Wx: 778}, '(': {Wx: 333}, ')': {Wx: 333}, '+': {Wx: 549}, ',': {Wx: 250}, '.': {Wx: 250}, '/': {Wx: 278}, '0': {Wx: 500}, '1': {Wx: 500}, '2': {Wx: 500}, '3': {Wx: 500}, '4': {Wx: 500}, '5': {Wx: 500}, '6': {Wx: 500}, '7': {Wx: 500}, '8': {Wx: 500}, '9': {Wx: 500}, ':': {Wx: 278}, ';': {Wx: 278}, '<': {Wx: 549}, '=': {Wx: 549}, '>': {Wx: 549}, '?': {Wx: 444}, '[': {Wx: 333}, ']': {Wx: 333}, '_': {Wx: 500}, '{': {Wx: 480}, '|': {Wx: 200}, '}': {Wx: 480}, '¬': {Wx: 713}, '°': {Wx: 400}, '±': {Wx: 549}, 'µ': {Wx: 576}, '×': {Wx: 549}, '÷': {Wx: 549}, 'ƒ': {Wx: 500}, 'Α': {Wx: 722}, 'Β': {Wx: 667}, 'Γ': {Wx: 603}, 'Ε': {Wx: 611}, 'Ζ': {Wx: 611}, 'Η': {Wx: 722}, 'Θ': {Wx: 741}, 'Ι': {Wx: 333}, 'Κ': {Wx: 722}, 'Λ': {Wx: 686}, 'Μ': {Wx: 889}, 'Ν': {Wx: 722}, 'Ξ': {Wx: 645}, 'Ο': {Wx: 722}, 'Π': {Wx: 768}, 'Ρ': {Wx: 556}, 'Σ': {Wx: 592}, 'Τ': {Wx: 611}, 'Υ': {Wx: 690}, 'Φ': {Wx: 763}, 'Χ': {Wx: 722}, 'Ψ': {Wx: 795}, 'α': {Wx: 631}, 'β': {Wx: 549}, 'γ': {Wx: 411}, 'δ': {Wx: 494}, 'ε': {Wx: 439}, 'ζ': {Wx: 494}, 'η': {Wx: 603}, 'θ': {Wx: 521}, 'ι': {Wx: 329}, 'κ': {Wx: 549}, 'λ': {Wx: 549}, 'ν': {Wx: 521}, 'ξ': {Wx: 493}, 'ο': {Wx: 549}, 'π': {Wx: 549}, 'ρ': {Wx: 549}, 'ς': {Wx: 439}, 'σ': {Wx: 603}, 'τ': {Wx: 439}, 'υ': {Wx: 576}, 'φ': {Wx: 521}, 'χ': {Wx: 549}, 'ψ': {Wx: 686}, 'ω': {Wx: 686}, 'ϑ': {Wx: 631}, 'ϒ': {Wx: 620}, 'ϕ': {Wx: 603}, 'ϖ': {Wx: 713}, '•': {Wx: 460}, '…': {Wx: 1000}, '′': {Wx: 247}, '″': {Wx: 411}, '⁄': {Wx: 167}, '€': {Wx: 750}, 'ℑ': {Wx: 686}, '℘': {Wx: 987}, 'ℜ': {Wx: 795}, 'Ω': {Wx: 768}, 'ℵ': {Wx: 823}, '←': {Wx: 987}, '↑': {Wx: 603}, '→': {Wx: 987}, '↓': {Wx: 603}, '↔': {Wx: 1042}, '↵': {Wx: 658}, '⇐': {Wx: 987}, '⇑': {Wx: 603}, '⇒': {Wx: 987}, '⇓': {Wx: 603}, '⇔': {Wx: 1042}, '∀': {Wx: 713}, '∂': {Wx: 494}, '∃': {Wx: 549}, '∅': {Wx: 823}, '∆': {Wx: 612}, '∇': {Wx: 713}, '∈': {Wx: 713}, '∉': {Wx: 713}, '∋': {Wx: 439}, '∏': {Wx: 823}, '∑': {Wx: 713}, '−': {Wx: 549}, '∗': {Wx: 500}, '√': {Wx: 549}, '∝': {Wx: 713}, '∞': {Wx: 713}, '∠': {Wx: 768}, '∧': {Wx: 603}, '∨': {Wx: 603}, '∩': {Wx: 768}, '∪': {Wx: 768}, '∫': {Wx: 274}, '∴': {Wx: 863}, '∼': {Wx: 549}, '≅': {Wx: 549}, '≈': {Wx: 549}, '≠': {Wx: 549}, '≡': {Wx: 549}, '≤': {Wx: 549}, '≥': {Wx: 549}, '⊂': {Wx: 713}, '⊃': {Wx: 713}, '⊄': {Wx: 713}, '⊆': {Wx: 713}, '⊇': {Wx: 713}, '⊕': {Wx: 768}, '⊗': {Wx: 768}, '⊥': {Wx: 658}, '⋅': {Wx: 250}, '⌠': {Wx: 686}, '⌡': {Wx: 686}, '〈': {Wx: 329}, '〉': {Wx: 329}, '◊': {Wx: 494}, '♠': {Wx: 753}, '♣': {Wx: 753}, '♥': {Wx: 753}, '♦': {Wx: 753}, '\uf6d9': {Wx: 790}, '\uf6da': {Wx: 790}, '\uf6db': {Wx: 890}, '\uf8e5': {Wx: 500}, '\uf8e6': {Wx: 603}, '\uf8e7': {Wx: 1000}, '\uf8e8': {Wx: 790}, '\uf8e9': {Wx: 790}, '\uf8ea': {Wx: 786}, '\uf8eb': {Wx: 384}, '\uf8ec': {Wx: 384}, '\uf8ed': {Wx: 384}, '\uf8ee': {Wx: 384}, '\uf8ef': {Wx: 384}, '\uf8f0': {Wx: 384}, '\uf8f1': {Wx: 494}, '\uf8f2': {Wx: 494}, '\uf8f3': {Wx: 494}, '\uf8f4': {Wx: 494}, '\uf8f5': {Wx: 686}, '\uf8f6': {Wx: 384}, '\uf8f7': {Wx: 384}, '\uf8f8': {Wx: 384}, '\uf8f9': {Wx: 384}, '\uf8fa': {Wx: 384}, '\uf8fb': {Wx: 384}, '\uf8fc': {Wx: 494}, '\uf8fd': {Wx: 494}, '\uf8fe': {Wx: 494}, '\uf8ff': {Wx: 790}}}

type CharMetrics struct {
	Wx float64
	Wy float64
}
type cffReader struct {
	data []byte
	pos  int
}

func zapfDingbatsFont() StdFont {
	enc := textencoding.NewZapfDingbatsEncoder()
	desc := Descriptor{Name: ZapfDingbatsName, Family: string(ZapfDingbatsName), Weight: FontWeightMedium, Flags: 0x0004, BBox: [4]float64{-1, -143, 981, 820}, ItalicAngle: 0, Ascent: 0, Descent: 0, CapHeight: 0, XHeight: 0, StemV: 90, StemH: 28}
	return NewStdFontWithEncoding(desc, zapfDingbatsMetrics, enc)
}
func NewStdFont(desc Descriptor, metrics *RuneCharSafeMap) StdFont {
	return NewStdFontWithEncoding(desc, metrics, textencoding.NewStandardEncoder())
}

var courierBoldObliqueMetrics *RuneCharSafeMap

func init() {
	RegisterStdFont(SymbolName, symbolFont, "Symbol,Italic", "Symbol,Bold", "Symbol,BoldItalic")
	RegisterStdFont(ZapfDingbatsName, zapfDingbatsFont)
}

const (
	OTFOutlineTypePostScript OTFOutlineType = 1
	OTFOutlineTypeTrueType   OTFOutlineType = 2
)

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
func helveticaBoldObliqueFont() StdFont {
	helveticaOnce.Do(initHelveticaMetrics)
	desc := Descriptor{Name: HelveticaBoldObliqueName, Family: string(HelveticaName), Weight: FontWeightBold, Flags: 0x0060, BBox: [4]float64{-174, -228, 1114, 962}, ItalicAngle: -12, Ascent: 718, Descent: -207, CapHeight: 718, XHeight: 532, StemV: 140, StemH: 118}
	return NewStdFont(desc, helveticaBoldObliqueMetrics)
}
func init() {
	RegisterStdFont(TimesRomanName, timesRomanFont, "TimesNewRoman", "Times")
	RegisterStdFont(TimesBoldName, timesBoldFont, "TimesNewRoman,Bold", "Times,Bold")
	RegisterStdFont(TimesItalicName, timesItalicFont, "TimesNewRoman,Italic", "Times,Italic")
	RegisterStdFont(TimesBoldItalicName, timesBoldItalicFont, "TimesNewRoman,BoldItalic", "Times,BoldItalic")
}

var timesBoldItalicMetrics *RuneCharSafeMap

func (p *otfParser) Parse() (OtfType, error) {
	data, err := io.ReadAll(p.reader)
	if err != nil {
		common.Log.Debug("ERROR: Unable to read font contents: %v", err)
		return OtfType{}, err
	}
	r := bytes.NewReader(data)
	p.loader, err = opentype.NewLoader(r)
	if err != nil {
		common.Log.Debug("ERROR: Unable to create loader: %v", err)
		return OtfType{}, err
	}
	p.font, err = font.NewFont(p.loader)
	if err != nil {
		common.Log.Debug("ERROR: Unable to create font: %v", err)
		return OtfType{}, err
	}
	p.otf = OtfType{TtfType: &TtfType{}, Loader: p.loader, Font: p.font, Face: font.NewFace(p.font)}
	if err = p.parseComponents(); err != nil {
		return OtfType{}, err
	}
	if p.otf.CFF != nil || p.otf.CFF2 != nil {
		p.otf.OutlineType = OTFOutlineTypePostScript
	} else if p.otf.Glyf != nil {
		p.otf.OutlineType = OTFOutlineTypeTrueType
	} else if p.isImageFont() {
		common.Log.Info("Font appears to be an image font (color bitmap or SVG based)")
	} else {
		return OtfType{}, fmt.Errorf("unable to determine outline type: no CFF, CFF2 or Glyf table found")
	}
	return p.otf, nil
}

var timesItalicMetrics *RuneCharSafeMap

func (p *ttfParser) parseCmapSubtable31(offset int64) error {
	startCodes := make([]rune, 0, 8)
	endCodes := make([]rune, 0, 8)
	idDeltas := make([]int16, 0, 8)
	idRangeOffsets := make([]uint16, 0, 8)
	p.ttf.Chars = make(map[rune]GID)
	p.reader.Seek(int64(p.tableOffsets["cmap"])+offset, io.SeekStart)
	format := p.ReadUShort()
	if format != 4 {
		common.Log.Debug("unexpected subtable format: %d (%w)", format)
		return nil
	}
	p.Skip(2 * 2)
	segCount := int(p.ReadUShort() / 2)
	p.Skip(3 * 2)
	for i := 0; i < segCount; i++ {
		endCodes = append(endCodes, rune(p.ReadUShort()))
	}
	p.Skip(2)
	for i := 0; i < segCount; i++ {
		startCodes = append(startCodes, rune(p.ReadUShort()))
	}
	for i := 0; i < segCount; i++ {
		idDeltas = append(idDeltas, p.ReadShort())
	}
	rangeOffsetPos, _ := p.reader.Seek(int64(0), io.SeekCurrent)
	for i := 0; i < segCount; i++ {
		idRangeOffsets = append(idRangeOffsets, p.ReadUShort())
	}
	for i := 0; i < segCount; i++ {
		start := startCodes[i]
		end := endCodes[i]
		delta := idDeltas[i]
		rangeOffset := idRangeOffsets[i]
		if rangeOffset > 0 {
			p.reader.Seek(rangeOffsetPos+2*int64(i)+int64(rangeOffset), io.SeekStart)
		}
		for code := start; code <= end; code++ {
			if code == 0xFFFF {
				break
			}
			var gid int32
			if rangeOffset > 0 {
				gid = int32(p.ReadUShort())
				if gid > 0 {
					gid += int32(delta)
				}
			} else {
				gid = code + int32(delta)
			}
			if gid >= 65536 {
				gid -= 65536
			}
			if gid > 0 {
				p.ttf.Chars[code] = GID(gid)
			}
		}
	}
	return nil
}

const (
	SymbolName       = StdFontName("Symbol")
	ZapfDingbatsName = StdFontName("ZapfDingbats")
)

func (p *otfParser) readTable(tag string) []byte {
	data, err := p.loader.RawTable(opentype.MustNewTag(tag))
	if err != nil {
		common.Log.Debug("ERROR: Unable to load table: %v", err)
	}
	return data
}
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
func courierFont() StdFont {
	courierOnce.Do(initCourierMetrics)
	desc := Descriptor{Name: CourierName, Family: string(CourierName), Weight: FontWeightMedium, Flags: 0x0021, BBox: [4]float64{-23, -250, 715, 805}, ItalicAngle: 0, Ascent: 629, Descent: -157, CapHeight: 562, XHeight: 426, StemV: 51, StemH: 51}
	return NewStdFont(desc, courierMetrics)
}
func encodeFontDict(privSize, privOffset int) []byte {
	var buf bytes.Buffer
	buf.Write(encodeInt32(privSize))
	buf.Write(encodeInt32(privOffset))
	buf.Write(encodeOperator(18, false))
	return buf.Bytes()
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
func (p *otfParser) parseOS2() error {
	os2, _, err := tables.ParseOs2(p.readTable("OS/2"))
	if err != nil {
		return err
	}
	p.otf.Bold = (os2.FsSelection & 32) != 0
	p.otf.TypoAscender = os2.STypoAscender
	p.otf.TypoDescender = os2.STypoDescender
	p.otf.CapHeight = 0
	p.fontPage = os2.FontPage()
	if os2.Version >= 2 {
		p.otf.CapHeight = int16(binary.BigEndian.Uint16(os2.HigherVersionData[10:]))
	}
	return nil
}
func timesBoldFont() StdFont {
	timesOnce.Do(initTimesMetrics)
	desc := Descriptor{Name: TimesBoldName, Family: timesFamily, Weight: FontWeightBold, Flags: 0x0020, BBox: [4]float64{-168, -218, 1000, 935}, ItalicAngle: 0, Ascent: 683, Descent: -217, CapHeight: 676, XHeight: 461, StemV: 139, StemH: 44}
	return NewStdFont(desc, timesBoldMetrics)
}
func (p *otfParser) parseGlyf() error {
	var glyf tables.Glyf
	glyfData, err := p.loader.RawTable(opentype.MustNewTag("glyf"))
	if err != nil {
		return err
	}
	locaData, err := p.loader.RawTable(opentype.MustNewTag("loca"))
	if err != nil {
		return err
	}
	loca, err := tables.ParseLoca(locaData, int(p.numGlyphs), p.otf.IndexToLocFormat == 1)
	if err == nil {
		glyf, _ = tables.ParseGlyf(glyfData, loca)
	}
	p.otf.Glyf = glyf
	return nil
}

var courierBoldMetrics *RuneCharSafeMap

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
func (p *ttfParser) ParseHhea() error {
	if err := p.Seek("hhea"); err != nil {
		return err
	}
	p.Skip(4 + 15*2)
	p.numHMetrics = p.ReadUShort()
	return nil
}
func describeTables(offsets map[string]uint32) string {
	var tags []string
	for tag := range offsets {
		tags = append(tags, tag)
	}
	sort.Slice(tags, func(i, j int) bool { return offsets[tags[i]] < offsets[tags[j]] })
	lines := []string{fmt.Sprintf("TrueType tables: %d", len(offsets))}
	for _, tag := range tags {
		lines = append(lines, fmt.Sprintf("\t%q %5d", tag, offsets[tag]))
	}
	return strings.Join(lines, "\n")
}
func encodeFDSelect(fds []byte) []byte {
	out := make([]byte, 1+len(fds))
	out[0] = 0
	copy(out[1:], fds)
	return out
}

var courierObliqueMetrics *RuneCharSafeMap
var helveticaMetrics *RuneCharSafeMap

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
func (p *ttfParser) ParsePost() error {
	if err := p.Seek("post"); err != nil {
		return err
	}
	formatType := p.Read32Fixed()
	p.ttf.ItalicAngle = p.Read32Fixed()
	p.ttf.UnderlinePosition = p.ReadShort()
	p.ttf.UnderlineThickness = p.ReadShort()
	p.ttf.IsFixedPitch = p.ReadULong() != 0
	p.ReadULong()
	p.ReadULong()
	p.ReadULong()
	p.ReadULong()
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("ParsePost: formatType=%f", formatType)
	}
	switch formatType {
	case 1.0:
		p.ttf.GlyphNames = macGlyphNames
	case 2.0:
		numGlyphs := int(p.ReadUShort())
		glyphNameIndex := make([]int, numGlyphs)
		p.ttf.GlyphNames = make([]GlyphName, numGlyphs)
		maxIndex := -1
		for i := 0; i < numGlyphs; i++ {
			idx := int(p.ReadUShort())
			glyphNameIndex[i] = idx
			if idx <= 0x7fff && idx > maxIndex {
				maxIndex = idx
			}
		}
		var extraNames []GlyphName
		if maxIndex >= len(macGlyphNames) {
			extraNames = make([]GlyphName, maxIndex-len(macGlyphNames)+1)
			for i := 0; i < maxIndex-len(macGlyphNames)+1; i++ {
				n := int(p.readByte())
				s, err := p.ReadStr(n)
				if err != nil {
					return err
				}
				extraNames[i] = GlyphName(s)
			}
		}
		for i := 0; i < numGlyphs; i++ {
			idx := glyphNameIndex[i]
			if idx < len(macGlyphNames) {
				p.ttf.GlyphNames[i] = macGlyphNames[idx]
			} else if idx >= len(macGlyphNames) && idx <= 32767 {
				p.ttf.GlyphNames[i] = extraNames[idx-len(macGlyphNames)]
			} else {
				p.ttf.GlyphNames[i] = ".undefined"
			}
		}
	case 2.5:
		offsets := make([]int, p.numGlyphs)
		for i := 0; i < len(offsets); i++ {
			off := int(p.ReadSByte())
			offsets[i] = i + 1 + off
		}
		p.ttf.GlyphNames = make([]GlyphName, len(offsets))
		for i := 0; i < len(p.ttf.GlyphNames); i++ {
			name := macGlyphNames[offsets[i]]
			p.ttf.GlyphNames[i] = name
		}
	case 3.0:
		common.Log.Debug("No PostScript name information is provided for the font.")
	default:
		common.Log.Debug("ERROR: Unknown formatType=%f", formatType)
	}
	return nil
}
func IsTTC(data []byte) bool                        { return len(data) >= 4 && string(data[:4]) == ttcTag }
func (f StdFont) GetMetricsTable() *RuneCharSafeMap { return f.metrics }
func (c *cffParsed) lookupFDSelect(gid uint16) byte {
	if int(gid) < len(c.fdSelect.fds) {
		return c.fdSelect.fds[gid]
	}
	return 0
}
func (p *ttfParser) ParseName() error {
	if err := p.Seek("name"); err != nil {
		return err
	}
	tableStart, _ := p.reader.Seek(0, io.SeekCurrent)
	p.ttf.PostScriptName = ""
	p.Skip(2)
	count := p.ReadUShort()
	stringOffset := p.ReadUShort()
	for i := uint16(0); i < count && p.ttf.PostScriptName == ""; i++ {
		p.Skip(3 * 2)
		nameID := p.ReadUShort()
		length := p.ReadUShort()
		offset := p.ReadUShort()
		if nameID == 6 {
			p.reader.Seek(tableStart+int64(stringOffset)+int64(offset), io.SeekStart)
			s, err := p.ReadStr(int(length))
			if err != nil {
				return err
			}
			s = strings.ReplaceAll(s, "\x00", "")
			re, err := regexp.Compile("[(){}<> /%[\\]]")
			if err != nil {
				return err
			}
			p.ttf.PostScriptName = re.ReplaceAllString(s, "")
		}
	}
	if p.ttf.PostScriptName == "" {
		common.Log.Debug("ParseName: The name PostScript was not found.")
	}
	return nil
}
func (p *ttfParser) ParseHmtx() error {
	if err := p.Seek("hmtx"); err != nil {
		return err
	}
	p.ttf.Widths = make([]uint16, 0, 8)
	for i := uint16(0); i < p.numHMetrics; i++ {
		p.ttf.Widths = append(p.ttf.Widths, p.ReadUShort())
		p.Skip(2)
	}
	if p.numHMetrics < p.numGlyphs && p.numHMetrics > 0 {
		lastWidth := p.ttf.Widths[p.numHMetrics-1]
		for i := p.numHMetrics; i < p.numGlyphs; i++ {
			p.ttf.Widths = append(p.ttf.Widths, lastWidth)
		}
	}
	return nil
}
func helveticaBoldFont() StdFont {
	helveticaOnce.Do(initHelveticaMetrics)
	desc := Descriptor{Name: HelveticaBoldName, Family: string(HelveticaName), Weight: FontWeightBold, Flags: 0x0020, BBox: [4]float64{-170, -228, 1003, 962}, ItalicAngle: 0, Ascent: 718, Descent: -207, CapHeight: 718, XHeight: 532, StemV: 140, StemH: 118}
	return NewStdFont(desc, helveticaBoldMetrics)
}
func (r *cffReader) readIndex() ([][]byte, error) {
	if r.pos+2 > len(r.data) {
		return nil, errors.New("INDEX: unexpected EOF reading count")
	}
	count := int(binary.BigEndian.Uint16(r.data[r.pos:]))
	r.pos += 2
	if count == 0 {
		return nil, nil
	}
	if r.pos+1 > len(r.data) {
		return nil, errors.New("INDEX: unexpected EOF reading offSize")
	}
	offSize := int(r.data[r.pos])
	r.pos++
	if offSize < 1 || offSize > 4 {
		return nil, fmt.Errorf("INDEX: invalid offSize %d", offSize)
	}
	offsets := make([]int, count+1)
	for i := 0; i <= count; i++ {
		if r.pos+offSize > len(r.data) {
			return nil, errors.New("INDEX: unexpected EOF reading offsets")
		}
		offsets[i] = readOffset(r.data[r.pos : r.pos+offSize])
		r.pos += offSize
	}
	base := r.pos - 1
	items := make([][]byte, count)
	for i := 0; i < count; i++ {
		start := base + offsets[i]
		end := base + offsets[i+1]
		if start < 0 || end > len(r.data) || start > end {
			return nil, fmt.Errorf("INDEX: invalid data range [%d:%d] (data len %d)", start, end, len(r.data))
		}
		items[i] = r.data[start:end]
	}
	r.pos = base + offsets[count]
	return items, nil
}

var timesWidths = []int16{722, 889, 722, 722, 722, 722, 722, 722, 722, 722, 722, 667, 667, 667, 667, 667, 722, 722, 722, 612, 611, 611, 611, 611, 611, 611, 611, 611, 611, 722, 500, 556, 722, 722, 722, 722, 333, 333, 333, 333, 333, 333, 333, 333, 389, 722, 722, 611, 611, 611, 611, 611, 889, 722, 722, 722, 722, 722, 722, 889, 722, 722, 722, 722, 722, 722, 722, 722, 556, 722, 667, 667, 667, 667, 556, 556, 556, 556, 556, 611, 611, 611, 556, 722, 722, 722, 722, 722, 722, 722, 722, 722, 722, 944, 722, 722, 722, 722, 611, 611, 611, 611, 444, 444, 444, 444, 333, 444, 667, 444, 444, 778, 444, 444, 469, 541, 500, 921, 444, 500, 278, 200, 480, 480, 333, 333, 333, 200, 350, 444, 444, 333, 444, 444, 333, 500, 333, 278, 250, 250, 760, 500, 500, 500, 500, 588, 500, 400, 333, 564, 500, 333, 278, 444, 444, 444, 444, 444, 444, 444, 500, 1000, 444, 1000, 500, 444, 564, 500, 333, 333, 333, 556, 500, 556, 500, 500, 167, 500, 500, 500, 500, 333, 564, 549, 500, 500, 333, 333, 500, 333, 333, 278, 278, 278, 278, 278, 278, 278, 278, 500, 500, 278, 278, 344, 278, 564, 549, 564, 471, 278, 778, 333, 564, 500, 564, 500, 500, 500, 500, 500, 549, 500, 500, 500, 500, 500, 500, 722, 333, 500, 500, 500, 500, 750, 750, 300, 276, 310, 500, 500, 500, 453, 333, 333, 476, 833, 250, 250, 1000, 564, 564, 500, 444, 444, 408, 444, 444, 444, 333, 333, 333, 180, 333, 333, 453, 333, 333, 760, 333, 389, 389, 389, 389, 389, 500, 278, 500, 500, 278, 250, 500, 600, 278, 326, 278, 500, 500, 750, 300, 333, 980, 500, 300, 500, 500, 500, 500, 500, 500, 500, 500, 500, 500, 500, 722, 500, 500, 500, 500, 500, 444, 444, 444, 444, 500}

func parseCFF(data []byte) (*cffParsed, error) {
	if len(data) < 4 {
		return nil, errors.New("CFF data too short")
	}
	if data[0] != 1 || data[1] != 0 {
		return nil, fmt.Errorf("unsupported CFF version %d.%d", data[0], data[1])
	}
	hdrSize := int(data[2])
	r := &cffReader{data: data, pos: hdrSize}
	parsed := &cffParsed{}
	names, err := r.readIndex()
	if err != nil {
		return nil, fmt.Errorf("Name INDEX: %w", err)
	}
	if len(names) != 1 {
		return nil, fmt.Errorf("expected 1 font in CFF, got %d", len(names))
	}
	parsed.fontName = names[0]
	topDicts, err := r.readIndex()
	if err != nil {
		return nil, fmt.Errorf("Top DICT INDEX: %w", err)
	}
	if len(topDicts) != 1 {
		return nil, fmt.Errorf("expected 1 top dict, got %d", len(topDicts))
	}
	top := parseTopDict(topDicts[0])
	parsed.strings, err = r.readIndex()
	if err != nil {
		return nil, fmt.Errorf("String INDEX: %w", err)
	}
	parsed.globalSubrs, err = r.readIndex()
	if err != nil {
		return nil, fmt.Errorf("Global Subrs INDEX: %w", err)
	}
	csReader := &cffReader{data: data, pos: top.charStringsOffset}
	parsed.charStrings, err = csReader.readIndex()
	if err != nil {
		return nil, fmt.Errorf("CharStrings INDEX: %w", err)
	}
	numGlyphs := uint16(len(parsed.charStrings))
	parsed.charset, err = parseCharset(data, top.charsetOffset, numGlyphs)
	if err != nil {
		return nil, fmt.Errorf("Charset: %w", err)
	}
	parsed.isCID = top.isCID
	if top.isCID {
		parsed.fdSelect, err = parseFDSelect(data, top.fdSelectOffset, numGlyphs)
		if err != nil {
			return nil, fmt.Errorf("FDSelect: %w", err)
		}
		fdReader := &cffReader{data: data, pos: top.fdArrayOffset}
		fdDicts, fdErr := fdReader.readIndex()
		if fdErr != nil {
			return nil, fmt.Errorf("FDArray INDEX: %w", fdErr)
		}
		parsed.numFDs = len(fdDicts)
		parsed.localSubrs = make([][][]byte, len(fdDicts))
		parsed.privateDicts = make([][]byte, len(fdDicts))
		for i, fdDict := range fdDicts {
			fdTop := parseTopDict(fdDict)
			if fdTop.privateSize > 0 && fdTop.privateOffset > 0 {
				privEnd := fdTop.privateOffset + fdTop.privateSize
				if privEnd <= len(data) {
					parsed.privateDicts[i] = data[fdTop.privateOffset:privEnd]
				}
				parsed.localSubrs[i], fdErr = readLocalSubrs(data, fdTop.privateOffset, fdTop.privateSize)
				if fdErr != nil {
					return nil, fmt.Errorf("FD %d local subrs: %w", i, fdErr)
				}
			}
		}
	} else {
		parsed.numFDs = 1
		parsed.localSubrs = make([][][]byte, 1)
		parsed.privateDicts = make([][]byte, 1)
		if top.privateSize > 0 && top.privateOffset > 0 {
			privEnd := top.privateOffset + top.privateSize
			if privEnd <= len(data) {
				parsed.privateDicts[0] = data[top.privateOffset:privEnd]
			}
			parsed.localSubrs[0], err = readLocalSubrs(data, top.privateOffset, top.privateSize)
			if err != nil {
				return nil, fmt.Errorf("local subrs: %w", err)
			}
		}
	}
	return parsed, nil
}

type OTFOutlineType int

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
func (p *ttfParser) ParseMaxp() error {
	if err := p.Seek("maxp"); err != nil {
		return err
	}
	p.Skip(4)
	p.numGlyphs = p.ReadUShort()
	return nil
}

var timesMetrics *RuneCharSafeMap

func parseCharset(data []byte, offset int, numGlyphs uint16) ([]uint16, error) {
	charset := make([]uint16, numGlyphs)
	if offset <= 2 {
		return charset, nil
	}
	if offset >= len(data) {
		return nil, errors.New("charset offset out of bounds")
	}
	format := data[offset]
	pos := offset + 1
	switch format {
	case 0:
		for i := uint16(1); i < numGlyphs; i++ {
			if pos+2 > len(data) {
				return nil, errors.New("charset data truncated")
			}
			charset[i] = binary.BigEndian.Uint16(data[pos:])
			pos += 2
		}
	case 1:
		for i := uint16(1); i < numGlyphs; {
			if pos+3 > len(data) {
				return nil, errors.New("charset data truncated")
			}
			first := binary.BigEndian.Uint16(data[pos:])
			nLeft := uint16(data[pos+2])
			pos += 3
			for j := uint16(0); j <= nLeft && i < numGlyphs; j++ {
				charset[i] = first + j
				i++
			}
		}
	case 2:
		for i := uint16(1); i < numGlyphs; {
			if pos+4 > len(data) {
				return nil, errors.New("charset data truncated")
			}
			first := binary.BigEndian.Uint16(data[pos:])
			nLeft := binary.BigEndian.Uint16(data[pos+2:])
			pos += 4
			for j := uint16(0); j <= nLeft && i < numGlyphs; j++ {
				charset[i] = first + j
				i++
			}
		}
	default:
		return nil, fmt.Errorf("unsupported charset format %d", format)
	}
	return charset, nil
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
func courierBoldFont() StdFont {
	courierOnce.Do(initCourierMetrics)
	desc := Descriptor{Name: CourierBoldName, Family: string(CourierName), Weight: FontWeightBold, Flags: 0x0021, BBox: [4]float64{-113, -250, 749, 801}, ItalicAngle: 0, Ascent: 629, Descent: -157, CapHeight: 562, XHeight: 439, StemV: 106, StemH: 84}
	return NewStdFont(desc, courierBoldMetrics)
}
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
func (p *ttfParser) ReadStr(length int) (string, error) {
	buf := make([]byte, length)
	n, err := p.reader.Read(buf)
	if err != nil {
		return "", err
	} else if n != length {
		return "", fmt.Errorf("unable to read %d bytes", length)
	}
	return string(buf), nil
}
func (p *otfParser) parseCFF() error {
	var cffFont *cff.CFF
	data, err := p.loader.RawTable(opentype.MustNewTag("CFF "))
	if err != nil {
		return err
	}
	cffFont, err = cff.Parse(data)
	if err != nil {
		return err
	}
	p.otf.CFF = cffFont
	return nil
}

const headTag = "head"

func (m *fontMap) read(name StdFontName) (func() StdFont, bool) {
	m.Lock()
	defer m.Unlock()
	builder, ok := m.builders[name]
	return builder, ok
}

var courierMetrics *RuneCharSafeMap

func TtfParse(r io.ReadSeeker) (TtfType, error) { p := &ttfParser{reader: r}; return p.Parse() }
func NewStdFontWithEncoding(desc Descriptor, metrics *RuneCharSafeMap, encoder textencoding.TextEncoder) StdFont {
	var nbsp rune = 0xA0
	if _, ok := metrics.Read(nbsp); !ok {
		nbspMetrics, _ := metrics.Read(0x20)
		metrics.Write(nbsp, nbspMetrics)
	}
	return StdFont{desc: desc, metrics: metrics, encoder: encoder}
}
func (p *ttfParser) parseCmapFormat0() error {
	s, err := p.ReadStr(256)
	if err != nil {
		return err
	}
	data := []byte(s)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("parseCmapFormat0: %s\ndataStr=%+q\ndata=[% 02x]", p.ttf.String(), s, data)
	}
	for code, gid := range data {
		p.ttf.Chars[rune(code)] = GID(gid)
	}
	return nil
}
func (p *otfParser) parseCFF2() error {
	var cff2 *cff.CFF2
	data, err := p.loader.RawTable(opentype.MustNewTag("CFF2"))
	if err != nil {
		return err
	}
	cff2, err = cff.ParseCFF2(data)
	if err != nil {
		return err
	}
	p.otf.CFF2 = cff2
	return nil
}
func (p *ttfParser) ParseComponents() error {
	if err := p.ParseHead(); err != nil {
		return err
	}
	if err := p.ParseHhea(); err != nil {
		return err
	}
	if err := p.ParseMaxp(); err != nil {
		return err
	}
	if err := p.ParseHmtx(); err != nil {
		return err
	}
	if _, ok := p.tableOffsets["name"]; ok {
		if err := p.ParseName(); err != nil {
			return err
		}
	}
	if _, ok := p.tableOffsets["OS/2"]; ok {
		if err := p.ParseOS2(); err != nil {
			return err
		}
	}
	if _, ok := p.tableOffsets["post"]; ok {
		if err := p.ParsePost(); err != nil {
			return err
		}
	}
	if _, ok := p.tableOffsets["cmap"]; ok {
		if err := p.ParseCmap(); err != nil {
			return err
		}
	}
	return nil
}
func (p *otfParser) parseComponents() error {
	if err := p.parseHead(); err != nil {
		return err
	}
	if err := p.parseHhea(); err != nil {
		return err
	}
	if err := p.parseMaxp(); err != nil {
		return err
	}
	if err := p.parseHmtx(); err != nil {
		return err
	}
	if err := p.parseName(); err != nil {
		return err
	}
	if err := p.parseOS2(); err != nil {
		return err
	}
	if err := p.parsePost(); err != nil {
		return err
	}
	if err := p.parseCmap(); err != nil {
		return err
	}
	err := p.parseGlyf()
	if err != nil {
		common.Log.Debug("Glyf table not found: %v", err)
	}
	err = p.parseCFF()
	if err != nil {
		common.Log.Debug("CFF table not found: %v", err)
	}
	err = p.parseCFF2()
	if err != nil {
		common.Log.Debug("CFF2 table not found: %v", err)
	}
	return nil
}

var timesBoldWidths = []int16{722, 1000, 722, 722, 722, 722, 722, 722, 722, 722, 722, 667, 722, 722, 722, 722, 722, 722, 722, 612, 667, 667, 667, 667, 667, 667, 667, 667, 667, 722, 500, 611, 778, 778, 778, 778, 389, 389, 389, 389, 389, 389, 389, 389, 500, 778, 778, 667, 667, 667, 667, 667, 944, 722, 722, 722, 722, 722, 778, 1000, 778, 778, 778, 778, 778, 778, 778, 778, 611, 778, 722, 722, 722, 722, 556, 556, 556, 556, 556, 667, 667, 667, 611, 722, 722, 722, 722, 722, 722, 722, 722, 722, 722, 1000, 722, 722, 722, 722, 667, 667, 667, 667, 500, 500, 500, 500, 333, 500, 722, 500, 500, 833, 500, 500, 581, 520, 500, 930, 500, 556, 278, 220, 394, 394, 333, 333, 333, 220, 350, 444, 444, 333, 444, 444, 333, 500, 333, 333, 250, 250, 747, 500, 556, 500, 500, 672, 556, 400, 333, 570, 500, 333, 278, 444, 444, 444, 444, 444, 444, 444, 500, 1000, 444, 1000, 500, 444, 570, 500, 333, 333, 333, 556, 500, 556, 500, 500, 167, 500, 500, 500, 556, 333, 570, 549, 500, 500, 333, 333, 556, 333, 333, 278, 278, 278, 278, 278, 278, 278, 333, 556, 556, 278, 278, 394, 278, 570, 549, 570, 494, 278, 833, 333, 570, 556, 570, 556, 556, 556, 556, 500, 549, 556, 500, 500, 500, 500, 500, 722, 333, 500, 500, 500, 500, 750, 750, 300, 300, 330, 500, 500, 556, 540, 333, 333, 494, 1000, 250, 250, 1000, 570, 570, 556, 500, 500, 555, 500, 500, 500, 333, 333, 333, 278, 444, 444, 549, 444, 444, 747, 333, 389, 389, 389, 389, 389, 500, 333, 500, 500, 278, 250, 500, 600, 333, 416, 333, 556, 500, 750, 300, 333, 1000, 500, 300, 556, 556, 556, 556, 556, 556, 556, 500, 556, 556, 500, 722, 500, 500, 500, 500, 500, 444, 444, 444, 444, 500}

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

var macGlyphNames = []GlyphName{".notdef", ".null", "nonmarkingreturn", "space", "exclam", "quotedbl", "numbersign", "dollar", "percent", "ampersand", "quotesingle", "parenleft", "parenright", "asterisk", "plus", "comma", "hyphen", "period", "slash", "zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "colon", "semicolon", "less", "equal", "greater", "question", "at", "A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z", "bracketleft", "backslash", "bracketright", "asciicircum", "underscore", "grave", "a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z", "braceleft", "bar", "braceright", "asciitilde", "Adieresis", "Aring", "Ccedilla", "Eacute", "Ntilde", "Odieresis", "Udieresis", "aacute", "agrave", "acircumflex", "adieresis", "atilde", "aring", "ccedilla", "eacute", "egrave", "ecircumflex", "edieresis", "iacute", "igrave", "icircumflex", "idieresis", "ntilde", "oacute", "ograve", "ocircumflex", "odieresis", "otilde", "uacute", "ugrave", "ucircumflex", "udieresis", "dagger", "degree", "cent", "sterling", "section", "bullet", "paragraph", "germandbls", "registered", "copyright", "trademark", "acute", "dieresis", "notequal", "AE", "Oslash", "infinity", "plusminus", "lessequal", "greaterequal", "yen", "mu", "partialdiff", "summation", "product", "pi", "integral", "ordfeminine", "ordmasculine", "Omega", "ae", "oslash", "questiondown", "exclamdown", "logicalnot", "radical", "florin", "approxequal", "Delta", "guillemotleft", "guillemotright", "ellipsis", "nonbreakingspace", "Agrave", "Atilde", "Otilde", "OE", "oe", "endash", "emdash", "quotedblleft", "quotedblright", "quoteleft", "quoteright", "divide", "lozenge", "ydieresis", "Ydieresis", "fraction", "currency", "guilsinglleft", "guilsinglright", "fi", "fl", "daggerdbl", "periodcentered", "quotesinglbase", "quotedblbase", "perthousand", "Acircumflex", "Ecircumflex", "Aacute", "Edieresis", "Egrave", "Iacute", "Icircumflex", "Idieresis", "Igrave", "Oacute", "Ocircumflex", "apple", "Ograve", "Uacute", "Ucircumflex", "Ugrave", "dotlessi", "circumflex", "tilde", "macron", "breve", "dotaccent", "ring", "cedilla", "hungarumlaut", "ogonek", "caron", "Lslash", "lslash", "Scaron", "scaron", "Zcaron", "zcaron", "brokenbar", "Eth", "eth", "Yacute", "yacute", "Thorn", "thorn", "minus", "multiply", "onesuperior", "twosuperior", "threesuperior", "onehalf", "onequarter", "threequarters", "franc", "Gbreve", "gbreve", "Idotaccent", "Scedilla", "scedilla", "Cacute", "cacute", "Ccaron", "ccaron", "dcroat"}

func buildTopDict(isCID bool, charStringsOffset, charsetOffset, fdSelectOffset, fdArrayOffset int, privOffsets, privSizes []int) []byte {
	var buf bytes.Buffer
	buf.Write(encodeInt32(charsetOffset))
	buf.Write(encodeOperator(15, false))
	buf.Write(encodeInt32(charStringsOffset))
	buf.Write(encodeOperator(17, false))
	if isCID {
		buf.Write(encodeCFFInt(0))
		buf.Write(encodeCFFInt(0))
		buf.Write(encodeCFFInt(0))
		buf.Write(encodeOperator(30, true))
		buf.Write(encodeInt32(fdSelectOffset))
		buf.Write(encodeOperator(37, true))
		buf.Write(encodeInt32(fdArrayOffset))
		buf.Write(encodeOperator(36, true))
	} else {
		buf.Write(encodeInt32(privSizes[0]))
		buf.Write(encodeInt32(privOffsets[0]))
		buf.Write(encodeOperator(18, false))
	}
	return buf.Bytes()
}
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
func (p *otfParser) parseMaxp() error {
	maxp, _, err := tables.ParseMaxp(p.readTable("maxp"))
	if err != nil {
		return err
	}
	p.numGlyphs = maxp.NumGlyphs
	return nil
}
func MakeRuneCharSafeMap(length int) *RuneCharSafeMap {
	return &RuneCharSafeMap{m: make(map[rune]CharMetrics, length)}
}
func timesRomanFont() StdFont {
	timesOnce.Do(initTimesMetrics)
	desc := Descriptor{Name: TimesRomanName, Family: timesFamily, Weight: FontWeightRoman, Flags: 0x0020, BBox: [4]float64{-168, -218, 1000, 898}, ItalicAngle: 0, Ascent: 683, Descent: -217, CapHeight: 662, XHeight: 450, StemV: 84, StemH: 28}
	return NewStdFont(desc, timesMetrics)
}
