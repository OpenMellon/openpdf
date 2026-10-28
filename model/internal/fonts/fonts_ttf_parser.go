package fonts

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/OpenMellon/openpdf/common"
)

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

func (p *ttfParser) Seek(tag string) error {
	offset, ok := p.tableOffsets[tag]
	if !ok {
		return fmt.Errorf("table not found: %s", tag)
	}
	p.reader.Seek(int64(offset), io.SeekStart)
	return nil
}

func (p *ttfParser) ReadShort() (v int16) {
	binary.Read(p.reader, binary.BigEndian, &v)
	return v
}

func (p *ttfParser) Skip(n int) { p.reader.Seek(int64(n), io.SeekCurrent) }

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

func (p *ttfParser) Read32Fixed() float64 {
	whole := float64(p.ReadShort())
	frac := float64(p.ReadUShort()) / 65536.0
	return whole + frac
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

func (p *ttfParser) ReadSByte() (v int8) {
	binary.Read(p.reader, binary.BigEndian, &v)
	return v
}

func (p *ttfParser) readByte() (v uint8) {
	binary.Read(p.reader, binary.BigEndian, &v)
	return v
}

type ttfParser struct {
	ttf          TtfType
	reader       io.ReadSeeker
	tableOffsets map[string]uint32
	numHMetrics  uint16
	numGlyphs    uint16
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

func (p *ttfParser) ParseHhea() error {
	if err := p.Seek("hhea"); err != nil {
		return err
	}
	p.Skip(4 + 15*2)
	p.numHMetrics = p.ReadUShort()
	return nil
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

func (p *ttfParser) ParseMaxp() error {
	if err := p.Seek("maxp"); err != nil {
		return err
	}
	p.Skip(4)
	p.numGlyphs = p.ReadUShort()
	return nil
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
