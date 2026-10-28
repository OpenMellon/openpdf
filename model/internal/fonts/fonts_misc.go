package fonts

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
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

const (
	CourierName            = StdFontName("Courier")
	CourierBoldName        = StdFontName("Courier-Bold")
	CourierObliqueName     = StdFontName("Courier-Oblique")
	CourierBoldObliqueName = StdFontName("Courier-BoldOblique")
)

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

func (m CharMetrics) String() string { return fmt.Sprintf("<%.1f,%.1f>", m.Wx, m.Wy) }

func courierObliqueFont() StdFont {
	courierOnce.Do(initCourierMetrics)
	desc := Descriptor{Name: CourierObliqueName, Family: string(CourierName), Weight: FontWeightMedium, Flags: 0x0061, BBox: [4]float64{-27, -250, 849, 805}, ItalicAngle: -12, Ascent: 629, Descent: -157, CapHeight: 562, XHeight: 426, StemV: 51, StemH: 51}
	return NewStdFont(desc, courierObliqueMetrics)
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

type CharMetrics struct {
	Wx float64
	Wy float64
}

type cffReader struct {
	data []byte
	pos  int
}

var courierBoldObliqueMetrics *RuneCharSafeMap

func courierFont() StdFont {
	courierOnce.Do(initCourierMetrics)
	desc := Descriptor{Name: CourierName, Family: string(CourierName), Weight: FontWeightMedium, Flags: 0x0021, BBox: [4]float64{-23, -250, 715, 805}, ItalicAngle: 0, Ascent: 629, Descent: -157, CapHeight: 562, XHeight: 426, StemV: 51, StemH: 51}
	return NewStdFont(desc, courierMetrics)
}

var courierBoldMetrics *RuneCharSafeMap

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

var (
	courierObliqueMetrics *RuneCharSafeMap
	helveticaMetrics      *RuneCharSafeMap
)

func (c *cffParsed) lookupFDSelect(gid uint16) byte {
	if int(gid) < len(c.fdSelect.fds) {
		return c.fdSelect.fds[gid]
	}
	return 0
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

func courierBoldFont() StdFont {
	courierOnce.Do(initCourierMetrics)
	desc := Descriptor{Name: CourierBoldName, Family: string(CourierName), Weight: FontWeightBold, Flags: 0x0021, BBox: [4]float64{-113, -250, 749, 801}, ItalicAngle: 0, Ascent: 629, Descent: -157, CapHeight: 562, XHeight: 439, StemV: 106, StemH: 84}
	return NewStdFont(desc, courierBoldMetrics)
}

var courierMetrics *RuneCharSafeMap

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
