// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package cmap

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap/bcmaps"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
)

func (cmap *CMap) parseName() error {
	name := ""
	done := false
	for i := 0; i < 20 && !done; i++ {
		o, err := cmap.parseObject()
		if err != nil {
			return err
		}
		switch t := o.(type) {
		case cmapOperand:
			switch t.Operand {
			case "def":
				done = true
			default:
				common.Log.Debug("parseName: State error. o=%#v name=%#q", o, name)
				if name != "" {
					name = fmt.Sprintf("%s %s", name, t.Operand)
				}
				common.Log.Debug("parseName: Recovered. name=%#q", name)
			}
		case cmapName:
			name = t.Name
		}
	}
	if !done {
		common.Log.Debug("ERROR: parseName: No def ")
		return ErrBadCMap
	}
	cmap.name = name
	return nil
}

var (
	ErrBadCMap        = errors.New("bad cmap")
	ErrBadCMapComment = errors.New("comment should start with %")
	ErrBadCMapDict    = errors.New("invalid dict")
)

type CMap struct {
	*cMapParser
	name          string
	nbits         int
	ctype         int
	version       string
	useCMap       string
	systemInfo    CIDSystemInfo
	codespaces    []Codespace
	codeToCID     map[CharCode]CharCode
	cidToCode     map[CharCode]CharCode
	codeToUnicode map[CharCode]string
	unicodeToCode map[string]CharCode
	rawBytes      []byte
	stream        *core.PdfObjectStream
	wmode         integer
	mu            sync.RWMutex
	inverseBuilt  bool
}
type cmapInt struct{ val int64 }

func LoadCmapFromData(data []byte, isSimple bool) (*CMap, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("LoadCmapFromData: isSimple=%t", isSimple)
	}
	cmap := newCMap(isSimple)
	cmap.cMapParser = newCMapParser(data)
	err := cmap.parse()
	if err != nil {
		return nil, err
	}
	sort.Slice(cmap.codespaces, func(i, j int) bool { return cmap.codespaces[i].Low < cmap.codespaces[j].Low })
	if len(cmap.codespaces) == 0 {
		if cmap.useCMap != "" {
			return cmap, nil
		}
		common.Log.Debug("ERROR: No codespaces. cmap=%s", cmap)
	}
	return cmap, nil
}

type cmapArray struct{ Array []cmapObject }

func (cmap *CMap) parseSystemInfo() error {
	inDict := false
	haveKey := false
	key := ""
	done := false
	info := CIDSystemInfo{}
	for i := 0; i < 50 && !done; i++ {
		o, err := cmap.parseObject()
		if err != nil {
			return err
		}
		switch t := o.(type) {
		case cmapDict:
			dict := t.Dict
			v, ok := dict["Registry"]
			if !ok {
				common.Log.Debug("ERROR: Bad System Info")
				return ErrBadCMap
			}
			str, ok := v.(cmapString)
			if !ok {
				common.Log.Debug("ERROR: Bad System Info")
				return ErrBadCMap
			}
			info.Registry = str.String
			v, ok = dict["Ordering"]
			if !ok {
				common.Log.Debug("ERROR: Bad System Info")
				return ErrBadCMap
			}
			str, ok = v.(cmapString)
			if !ok {
				common.Log.Debug("ERROR: Bad System Info")
				return ErrBadCMap
			}
			info.Ordering = str.String
			supObj, ok := dict["Supplement"]
			if !ok {
				common.Log.Debug("ERROR: Bad System Info")
				return ErrBadCMap
			}
			supInt, ok := supObj.(cmapInt)
			if !ok {
				common.Log.Debug("ERROR: Bad System Info")
				return ErrBadCMap
			}
			info.Supplement = int(supInt.val)
			done = true
		case cmapOperand:
			switch t.Operand {
			case "begin":
				inDict = true
			case "end":
				done = true
			case "def":
				haveKey = false
			}
		case cmapName:
			if inDict {
				key = t.Name
				haveKey = true
			}
		case cmapString:
			if haveKey {
				switch key {
				case "Registry":
					info.Registry = t.String
				case "Ordering":
					info.Ordering = t.String
				}
			}
		case cmapInt:
			if haveKey {
				switch key {
				case "Supplement":
					info.Supplement = int(t.val)
				}
			}
		}
	}
	if !done {
		common.Log.Debug("ERROR: Parsed System Info dict incorrectly")
		return ErrBadCMap
	}
	cmap.systemInfo = info
	return nil
}
func (p *cMapParser) parseHexString() (cmapHexString, error) {
	p.reader.ReadByte()
	hexChars := []byte("0123456789abcdefABCDEF")
	buf := bytes.Buffer{}
	for {
		p.skipSpaces()
		b, err := p.reader.Peek(1)
		if err != nil {
			return cmapHexString{}, err
		}
		if b[0] == '>' {
			p.reader.ReadByte()
			break
		}
		c, _ := p.reader.ReadByte()
		if bytes.IndexByte(hexChars, c) >= 0 {
			buf.WriteByte(c)
		}
	}
	if buf.Len()%2 == 1 {
		common.Log.Debug("parseHexString: appending '0' to %#q", buf.String())
		buf.WriteByte('0')
	}
	numBytes := buf.Len() / 2
	data, _ := hex.DecodeString(buf.String())
	return cmapHexString{numBytes: numBytes, data: data}, nil
}

type Codespace struct {
	NumBytes int
	Low      CharCode
	High     CharCode
}

func (p *cMapParser) parseObject() (cmapObject, error) {
	p.skipSpaces()
	for {
		b, err := p.reader.Peek(2)
		if err != nil {
			return nil, err
		}
		if b[0] == '%' {
			p.parseComment()
			p.skipSpaces()
			continue
		} else if b[0] == '/' {
			name, nameErr := p.parseName()
			return name, nameErr
		} else if b[0] == '(' {
			str, strErr := p.parseString()
			return str, strErr
		} else if b[0] == '[' {
			arr, arrErr := p.parseArray()
			return arr, arrErr
		} else if (b[0] == '<') && (b[1] == '<') {
			dict, dictErr := p.parseDict()
			return dict, dictErr
		} else if b[0] == '<' {
			hexStr, hexErr := p.parseHexString()
			return hexStr, hexErr
		} else if core.IsDecimalDigit(b[0]) || (b[0] == '-' && core.IsDecimalDigit(b[1])) {
			num, numErr := p.parseNumber()
			if numErr != nil {
				return nil, numErr
			}
			return num, nil
		} else {
			op, opErr := p.parseOperand()
			if opErr != nil {
				return nil, opErr
			}
			return op, nil
		}
	}
}

type cmapFloat struct{ val float64 }

func (cmap *CMap) Type() int { return cmap.ctype }
func (cmap *CMap) BytesToCharcodes(data []byte) ([]CharCode, bool) {
	var codes []CharCode
	if cmap.nbits == 8 {
		for _, b := range data {
			codes = append(codes, CharCode(b))
		}
		return codes, true
	}
	for i := 0; i < len(data); {
		code, n, ok := cmap.matchCode(data[i:])
		if !ok {
			common.Log.Debug("ERROR: No code match at i=%d bytes=[% 02x]=%#q", i, data, string(data))
			return codes, false
		}
		codes = append(codes, code)
		i += n
	}
	return codes, true
}
func newCMap(isSimple bool) *CMap {
	nbits := 16
	if isSimple {
		nbits = 8
	}
	return &CMap{nbits: nbits, codeToCID: make(map[CharCode]CharCode), cidToCode: make(map[CharCode]CharCode), codeToUnicode: make(map[CharCode]string), unicodeToCode: make(map[string]CharCode)}
}
func (p *cMapParser) parseName() (cmapName, error) {
	name := ""
	started := false
	for {
		b, err := p.reader.Peek(1)
		if err == io.EOF {
			break
		}
		if err != nil {
			return cmapName{name}, err
		}
		if !started {
			if b[0] == '/' {
				started = true
				p.reader.ReadByte()
			} else {
				common.Log.Debug("ERROR: Name starting with %s (% x)", b, b)
				return cmapName{name}, fmt.Errorf("invalid name: (%c)", b[0])
			}
		} else {
			if core.IsWhiteSpace(b[0]) {
				break
			} else if (b[0] == '/') || (b[0] == '[') || (b[0] == '(') || (b[0] == ']') || (b[0] == '<') || (b[0] == '>') {
				break
			} else if b[0] == '#' {
				esc, err := p.reader.Peek(3)
				if err != nil {
					return cmapName{name}, err
				}
				p.reader.Discard(3)
				decoded, err := hex.DecodeString(string(esc[1:3]))
				if err != nil {
					return cmapName{name}, err
				}
				name += string(decoded)
			} else {
				c, _ := p.reader.ReadByte()
				name += string(c)
			}
		}
	}
	return cmapName{name}, nil
}
func encodeHexString(s string) string {
	runes := []rune(s)
	parts := make([]string, len(runes))
	for i, r := range runes {
		parts[i] = fmt.Sprintf("%04x", r)
	}
	return fmt.Sprintf("<%s>", strings.Join(parts, ""))
}

type CIDSystemInfo struct {
	Registry   string
	Ordering   string
	Supplement int
}

func (cmap *CMap) matchCode(data []byte) (code CharCode, n int, ok bool) {
	for i := 0; i < maxCodeBytes; i++ {
		if i < len(data) {
			code = code<<8 | CharCode(data[i])
			n++
		}
		ok = cmap.inCodespace(code, i+1)
		if ok {
			return code, n, true
		}
	}
	common.Log.Debug("ERROR: No codespace matches bytes=[% 02x]=%#q cmap=%s", data, string(data), cmap)
	return 0, 0, false
}

type cmapDict struct{ Dict map[string]cmapObject }

func (p *cMapParser) parseNumber() (cmapObject, error) {
	obj, err := core.ParseNumber(p.reader)
	if err != nil {
		return nil, err
	}
	switch t := obj.(type) {
	case *core.PdfObjectFloat:
		return cmapFloat{float64(*t)}, nil
	case *core.PdfObjectInteger:
		return cmapInt{int64(*t)}, nil
	}
	return nil, fmt.Errorf("unhandled number type %T", obj)
}
func (cmap *CMap) parseBfchar() error {
	for {
		o, err := cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		var code CharCode
		switch t := o.(type) {
		case cmapOperand:
			if t.Operand == tokEndBfChar {
				return nil
			}
			return errors.New("unexpected operand")
		case cmapHexString:
			code = hexToCharCode(t)
		default:
			return errors.New("unexpected type")
		}
		o, err = cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		var runes []rune
		switch t := o.(type) {
		case cmapOperand:
			if t.Operand == tokEndBfChar {
				return nil
			}
			common.Log.Debug("ERROR: Unexpected operand. %#v", t)
			return ErrBadCMap
		case cmapHexString:
			runes = hexToRunes(t)
		case cmapName:
			common.Log.Debug("ERROR: Unexpected name. %#v", t)
			runes = []rune{MissingCodeRune}
		default:
			common.Log.Debug("ERROR: Unexpected type. %#v", o)
			return ErrBadCMap
		}
		cmap.codeToUnicode[code] = string(runes)
	}
	return nil
}
func (cmap *CMap) parseWMode() error {
	var wmode int
	done := false
	for i := 0; i < 3 && !done; i++ {
		o, err := cmap.parseObject()
		if err != nil {
			return err
		}
		switch t := o.(type) {
		case cmapOperand:
			switch t.Operand {
			case "def":
				done = true
			default:
				common.Log.Error("parseWMode: state error. o=%#v", o)
				return ErrBadCMap
			}
		case cmapInt:
			wmode = int(t.val)
		}
	}
	cmap.wmode = integer{set: true, val: wmode}
	return nil
}

type CharCode uint32

func (cmap *CMap) Bytes() []byte {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("cmap.Bytes: cmap=%s", cmap.String())
	}
	if len(cmap.rawBytes) > 0 {
		return cmap.rawBytes
	}
	cmap.rawBytes = []byte(strings.Join([]string{cmapHeader, cmap.toBfData(), cmapFooter}, "\n"))
	return cmap.rawBytes
}
func newCMapParser(data []byte) *cMapParser {
	p := cMapParser{}
	buf := bytes.NewBuffer(data)
	p.reader = bufio.NewReader(buf)
	return &p
}
func loadBuiltinCMap(name string) (*CMap, error) {
	data, err := bcmaps.Asset(name)
	if err != nil {
		return nil, err
	}
	return LoadCmapFromDataCID(data)
}

const (
	maxEntriesPerBlock = 100
	cmapHeader         = "\n/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"
	cmapFooter         = "endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"
)

func (cmap *CMap) parseCIDRange() error {
	for {
		o, err := cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		startHex, ok := o.(cmapHexString)
		if !ok {
			if op, isOp := o.(cmapOperand); isOp {
				if op.Operand == tokEndCIDRange {
					return nil
				}
				return errors.New("cid interval start must be a hex string")
			}
		}
		start := hexToCharCode(startHex)
		o, err = cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		endHex, ok := o.(cmapHexString)
		if !ok {
			return errors.New("cid interval end must be a hex string")
		}
		if len(startHex.data) != len(endHex.data) {
			return errors.New("unequal number of bytes in range")
		}
		end := hexToCharCode(endHex)
		if start > end {
			common.Log.Debug("ERROR: invalid CID range. start=0x%02x end=0x%02x", start, end)
			return ErrBadCMap
		}
		o, err = cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		cidStart, ok := o.(cmapInt)
		if !ok {
			return errors.New("cid start value must be an decimal number")
		}
		if cidStart.val < 0 {
			return errors.New("invalid cid start value")
		}
		cid := cidStart.val
		for code := start; code <= end; code++ {
			cmap.codeToCID[code] = CharCode(cid)
			cid++
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("CID range: <0x%X> <0x%X> %d", start, end, cidStart.val)
		}
	}
	return nil
}
func LoadPredefinedCMap(name string) (*CMap, error) {
	cmap, err := loadBuiltinCMap(name)
	if err != nil {
		return nil, err
	}
	if cmap.useCMap != "" {
		parent, parentErr := loadBuiltinCMap(cmap.useCMap)
		if parentErr != nil {
			return nil, parentErr
		}
		for code, cid := range parent.codeToCID {
			if _, exists := cmap.codeToCID[code]; !exists {
				cmap.codeToCID[code] = cid
			}
		}
		cmap.codespaces = append(cmap.codespaces, parent.codespaces...)
	}
	sort.Slice(cmap.codespaces, func(i, j int) bool { return cmap.codespaces[i].Low < cmap.codespaces[j].Low })
	cmap.inverseBuilt = false
	return cmap, nil
}
func (p *cMapParser) parseDict() (cmapDict, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Reading PDF Dict!")
	}
	dict := newCmapDict()
	c, _ := p.reader.ReadByte()
	if c != '<' {
		return dict, ErrBadCMapDict
	}
	c, _ = p.reader.ReadByte()
	if c != '<' {
		return dict, ErrBadCMapDict
	}
	for {
		p.skipSpaces()
		b, err := p.reader.Peek(2)
		if err != nil {
			return dict, err
		}
		if (b[0] == '>') && (b[1] == '>') {
			p.reader.ReadByte()
			p.reader.ReadByte()
			break
		}
		key, err := p.parseName()
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Key: %s", key.Name)
		}
		if err != nil {
			common.Log.Debug("ERROR: Returning name. err=%v", err)
			return dict, err
		}
		p.skipSpaces()
		val, err := p.parseObject()
		if err != nil {
			return dict, err
		}
		dict.Dict[key.Name] = val
		p.skipSpaces()
		b, err = p.reader.Peek(3)
		if err != nil {
			return dict, err
		}
		if string(b) == "def" {
			p.reader.Discard(3)
		}
	}
	return dict, nil
}
func LoadCmapFromDataCID(data []byte) (*CMap, error) { return LoadCmapFromData(data, false) }
func (cmap *CMap) toBfData() string {
	if len(cmap.codeToUnicode) == 0 {
		return ""
	}
	codes := make([]CharCode, 0, len(cmap.codeToUnicode))
	for code := range cmap.codeToUnicode {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	var ranges []charRange
	cur := charRange{codes[0], codes[0]}
	prevText := cmap.codeToUnicode[codes[0]]
	for _, code := range codes[1:] {
		text := cmap.codeToUnicode[code]
		if code == cur.last+1 && lastRune(text) == lastRune(prevText)+1 {
			cur.last = code
		} else {
			ranges = append(ranges, cur)
			cur.first, cur.last = code, code
		}
		prevText = text
	}
	ranges = append(ranges, cur)
	var chars []CharCode
	var bfRanges []fbRange
	for _, r := range ranges {
		if r.first == r.last {
			chars = append(chars, r.first)
		} else {
			bfRanges = append(bfRanges, fbRange{low: r.first, high: r.last, target: cmap.codeToUnicode[r.first]})
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("charRanges=%d fbChars=%d fbRanges=%d", len(ranges), len(chars), len(bfRanges))
	}
	var lines []string
	if len(chars) > 0 {
		nBlocks := (len(chars) + maxEntriesPerBlock - 1) / maxEntriesPerBlock
		for blk := 0; blk < nBlocks; blk++ {
			n := minInt(len(chars)-blk*maxEntriesPerBlock, maxEntriesPerBlock)
			lines = append(lines, fmt.Sprintf("%d beginbfchar", n))
			for i := 0; i < n; i++ {
				code := chars[blk*maxEntriesPerBlock+i]
				text := cmap.codeToUnicode[code]
				lines = append(lines, fmt.Sprintf("<%04x> %s", code, encodeHexString(text)))
			}
			lines = append(lines, "endbfchar")
		}
	}
	if len(bfRanges) > 0 {
		nBlocks := (len(bfRanges) + maxEntriesPerBlock - 1) / maxEntriesPerBlock
		for blk := 0; blk < nBlocks; blk++ {
			n := minInt(len(bfRanges)-blk*maxEntriesPerBlock, maxEntriesPerBlock)
			lines = append(lines, fmt.Sprintf("%d beginbfrange", n))
			for i := 0; i < n; i++ {
				r := bfRanges[blk*maxEntriesPerBlock+i]
				lines = append(lines, fmt.Sprintf("<%04x><%04x> %s", r.low, r.high, encodeHexString(r.target)))
			}
			lines = append(lines, "endbfrange")
		}
	}
	return strings.Join(lines, "\n")
}
func (cmap *CMap) parseCodespaceRange() error {
	for {
		o, err := cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		lowHex, ok := o.(cmapHexString)
		if !ok {
			if op, isOp := o.(cmapOperand); isOp {
				if op.Operand == tokEndCodespaceRange {
					return nil
				}
				return errors.New("unexpected operand")
			}
		}
		o, err = cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		highHex, ok := o.(cmapHexString)
		if !ok {
			return errors.New("non-hex high")
		}
		if len(lowHex.data) != len(highHex.data) {
			return errors.New("unequal number of bytes in range")
		}
		low := hexToCharCode(lowHex)
		high := hexToCharCode(highHex)
		if high < low {
			common.Log.Debug("ERROR: Bad codespace. low=0x%02x high=0x%02x", low, high)
			return ErrBadCMap
		}
		numBytes := highHex.numBytes
		cs := Codespace{NumBytes: numBytes, Low: low, High: high}
		cmap.codespaces = append(cmap.codespaces, cs)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Codespace low: 0x%X, high: 0x%X", low, high)
		}
	}
	if len(cmap.codespaces) == 0 {
		common.Log.Debug("ERROR: No codespaces in cmap.")
		return ErrBadCMap
	}
	return nil
}
func (cmap *CMap) CharcodeToUnicode(code CharCode) (string, bool) {
	if s, ok := cmap.codeToUnicode[code]; ok {
		return s, true
	}
	return MissingCodeString, false
}
func (cmap *CMap) CIDSystemInfo() CIDSystemInfo { return cmap.systemInfo }
func hexToRunes(h cmapHexString) []rune {
	if len(h.data) == 1 {
		return []rune{rune(h.data[0])}
	}
	data := h.data
	if len(data)%2 != 0 {
		data = append(data, 0)
		common.Log.Debug("ERROR: hexToRunes. Padding shex=%#v to %+v", h, data)
	}
	n := len(data) >> 1
	u16 := make([]uint16, n)
	for i := 0; i < n; i++ {
		u16[i] = uint16(data[i<<1])<<8 + uint16(data[i<<1+1])
	}
	runes := utf16.Decode(u16)
	return runes
}
func hexToCharCode(h cmapHexString) CharCode {
	code := CharCode(0)
	for _, b := range h.data {
		code <<= 8
		code |= CharCode(b)
	}
	return code
}
func IsPredefinedCMap(name string) bool { return bcmaps.AssetExists(name) }

type cMapParser struct{ reader *bufio.Reader }
type cmapHexString struct {
	numBytes int
	data     []byte
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type cmapName struct{ Name string }

func (cmap *CMap) Stream() (*core.PdfObjectStream, error) {
	if cmap.stream != nil {
		return cmap.stream, nil
	}
	stream, err := core.MakeStream(cmap.Bytes(), core.NewFlateEncoder())
	if err != nil {
		return nil, err
	}
	cmap.stream = stream
	return cmap.stream, nil
}
func (cmap *CMap) CIDToCharcode(cid CharCode) (CharCode, bool) {
	cmap.ensureInverseMappings()
	code, ok := cmap.cidToCode[cid]
	return code, ok
}

type cmapString struct {
	String string
}

func lastRune(s string) rune { runes := []rune(s); return runes[len(runes)-1] }

const (
	maxCodeBytes      = 4
	MissingCodeRune   = '\ufffd'
	MissingCodeString = string(MissingCodeRune)
)

func (p *cMapParser) parseArray() (cmapArray, error) {
	arr := cmapArray{}
	arr.Array = []cmapObject{}
	p.reader.ReadByte()
	for {
		p.skipSpaces()
		b, err := p.reader.Peek(1)
		if err != nil {
			return arr, err
		}
		if b[0] == ']' {
			p.reader.ReadByte()
			break
		}
		o, err := p.parseObject()
		if err != nil {
			return arr, err
		}
		arr.Array = append(arr.Array, o)
	}
	return arr, nil
}
func (cmap *CMap) Name() string { return cmap.name }
func (p *cMapParser) skipSpaces() (int, error) {
	n := 0
	for {
		b, err := p.reader.Peek(1)
		if err != nil {
			return 0, err
		}
		if core.IsWhiteSpace(b[0]) {
			p.reader.ReadByte()
			n++
		} else {
			break
		}
	}
	return n, nil
}
func (cmap *CMap) parseVersion() error {
	version := ""
	done := false
	for i := 0; i < 3 && !done; i++ {
		o, err := cmap.parseObject()
		if err != nil {
			return err
		}
		switch t := o.(type) {
		case cmapOperand:
			switch t.Operand {
			case "def":
				done = true
			default:
				common.Log.Debug("ERROR: parseVersion: state error. o=%#v", o)
				return ErrBadCMap
			}
		case cmapInt:
			version = fmt.Sprintf("%d", t.val)
		case cmapFloat:
			version = fmt.Sprintf("%f", t.val)
		case cmapString:
			version = t.String
		default:
			common.Log.Debug("ERROR: parseVersion: Bad type. o=%#v", o)
		}
	}
	cmap.version = version
	return nil
}
func (cmap *CMap) inCodespace(code CharCode, nBytes int) bool {
	for _, cs := range cmap.codespaces {
		if cs.Low <= code && code <= cs.High && nBytes == cs.NumBytes {
			return true
		}
	}
	return false
}
func (cmap *CMap) parseBfrange() error {
	for {
		var low CharCode
		o, err := cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		switch t := o.(type) {
		case cmapOperand:
			if t.Operand == tokEndBfRange {
				return nil
			}
			return errors.New("unexpected operand")
		case cmapHexString:
			low = hexToCharCode(t)
		default:
			return errors.New("unexpected type")
		}
		var high CharCode
		o, err = cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		switch t := o.(type) {
		case cmapOperand:
			common.Log.Debug("ERROR: Incomplete bfrange triplet")
			return ErrBadCMap
		case cmapHexString:
			high = hexToCharCode(t)
			if high > 0xffff {
				high = 0xffff
			}
		default:
			common.Log.Debug("ERROR: Unexpected type %T", o)
			return ErrBadCMap
		}
		o, err = cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		switch t := o.(type) {
		case cmapArray:
			if len(t.Array) != int(high-low)+1 {
				common.Log.Debug("ERROR: Invalid number of items in array")
				return ErrBadCMap
			}
			for code := low; code <= high; code++ {
				item := t.Array[code-low]
				itemHex, ok := item.(cmapHexString)
				if !ok {
					return errors.New("non-hex string in array")
				}
				runes := hexToRunes(itemHex)
				cmap.codeToUnicode[code] = string(runes)
			}
		case cmapHexString:
			target := hexToRunes(t)
			n := len(target)
			for code := low; code <= high; code++ {
				cmap.codeToUnicode[code] = string(target)
				if n > 0 {
					target[n-1]++
				} else {
					common.Log.Debug("No cmap target character specified for %#v", code)
				}
				if code == 1<<32-1 {
					break
				}
			}
		default:
			common.Log.Debug("ERROR: Unexpected type %T", o)
			return ErrBadCMap
		}
	}
	return nil
}
func NewCIDSystemInfo(obj core.PdfObject) (info CIDSystemInfo, err error) {
	dict, ok := core.GetDict(obj)
	if !ok {
		return CIDSystemInfo{}, core.ErrTypeError
	}
	registry, ok := core.GetStringVal(dict.Get("Registry"))
	if !ok {
		return CIDSystemInfo{}, core.ErrTypeError
	}
	ordering, ok := core.GetStringVal(dict.Get("Ordering"))
	if !ok {
		return CIDSystemInfo{}, core.ErrTypeError
	}
	supplement, ok := core.GetIntVal(dict.Get("Supplement"))
	if !ok {
		return CIDSystemInfo{}, core.ErrTypeError
	}
	return CIDSystemInfo{Registry: registry, Ordering: ordering, Supplement: supplement}, nil
}
func (cmap *CMap) WMode() (int, bool) { return cmap.wmode.val, cmap.wmode.set }
func (cmap *CMap) parse() error {
	var prev cmapObject
	for {
		o, err := cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			common.Log.Debug("ERROR: parsing CMap: %v", err)
			return err
		}
		switch t := o.(type) {
		case cmapOperand:
			op := t
			switch op.Operand {
			case tokBeginCodespaceRange:
				err := cmap.parseCodespaceRange()
				if err != nil {
					return err
				}
			case tokBeginCIDRange:
				err := cmap.parseCIDRange()
				if err != nil {
					return err
				}
			case tokBeginBfChar:
				err := cmap.parseBfchar()
				if err != nil {
					return err
				}
			case tokBeginBfRange:
				err := cmap.parseBfrange()
				if err != nil {
					return err
				}
			case tokUseCMap:
				if prev == nil {
					common.Log.Debug("ERROR: usecmap with no arg")
					return ErrBadCMap
				}
				nameObj, ok := prev.(cmapName)
				if !ok {
					common.Log.Debug("ERROR: usecmap arg not a name %#v", prev)
					return ErrBadCMap
				}
				cmap.useCMap = nameObj.Name
			case tokCIDSystemInfo:
				err := cmap.parseSystemInfo()
				if err != nil {
					return err
				}
			}
		case cmapName:
			nm := t
			switch nm.Name {
			case tokCIDSystemInfo:
				err := cmap.parseSystemInfo()
				if err != nil {
					return err
				}
			case tokCMapName:
				err := cmap.parseName()
				if err != nil {
					return err
				}
			case tokCMapType:
				err := cmap.parseType()
				if err != nil {
					return err
				}
			case tokCMapVersion:
				err := cmap.parseVersion()
				if err != nil {
					return err
				}
			case tokWMode:
				if err = cmap.parseWMode(); err != nil {
					return err
				}
			}
		}
		prev = o
	}
	return nil
}
func (cmap *CMap) parseType() error {
	typ := 0
	done := false
	for i := 0; i < 3 && !done; i++ {
		o, err := cmap.parseObject()
		if err != nil {
			return err
		}
		switch t := o.(type) {
		case cmapOperand:
			switch t.Operand {
			case "def":
				done = true
			default:
				common.Log.Error("parseType: state error. o=%#v", o)
				return ErrBadCMap
			}
		case cmapInt:
			typ = int(t.val)
		}
	}
	cmap.ctype = typ
	return nil
}
func (cmap *CMap) MergeCodeToUnicode(m map[CharCode]string, overwrite bool) {
	if len(m) == 0 {
		return
	}
	cmap.mu.Lock()
	defer cmap.mu.Unlock()
	if cmap.codeToUnicode == nil {
		cmap.codeToUnicode = make(map[CharCode]string, len(m))
	}
	changed := false
	for code, s := range m {
		if _, exists := cmap.codeToUnicode[code]; exists && !overwrite {
			continue
		}
		cmap.codeToUnicode[code] = s
		changed = true
	}
	if !changed {
		return
	}
	cmap.unicodeToCode = make(map[string]CharCode, len(cmap.codeToUnicode))
	cmap.inverseBuilt = false
	cmap.rawBytes = nil
	cmap.stream = nil
}
func (info *CIDSystemInfo) String() string {
	return fmt.Sprintf("%s-%s-%03d", info.Registry, info.Ordering, info.Supplement)
}
func (p *cMapParser) parseOperand() (cmapOperand, error) {
	op := cmapOperand{}
	buf := bytes.Buffer{}
	for {
		b, err := p.reader.Peek(1)
		if err != nil {
			if err == io.EOF {
				break
			}
			return op, err
		}
		if core.IsDelimiter(b[0]) {
			break
		}
		if core.IsWhiteSpace(b[0]) {
			break
		}
		c, _ := p.reader.ReadByte()
		buf.WriteByte(c)
	}
	if buf.Len() == 0 {
		return op, fmt.Errorf("invalid operand (empty)")
	}
	op.Operand = buf.String()
	return op, nil
}
func hexToRune(h cmapHexString) rune {
	runes := hexToRunes(h)
	if n := len(runes); n == 0 {
		common.Log.Debug("ERROR: hexToRune. Expected at least one rune shex=%#v", h)
		return MissingCodeRune
	}
	if len(runes) > 1 {
		common.Log.Debug("ERROR: hexToRune. Expected exactly one rune shex=%#v -> %#v", h, runes)
	}
	return runes[0]
}

type charRange struct {
	first CharCode
	last  CharCode
}

func (cmap *CMap) NBits() int { return cmap.nbits }
func (cmap *CMap) ensureInverseMappings() {
	cmap.mu.RLock()
	if cmap.inverseBuilt {
		cmap.mu.RUnlock()
		return
	}
	cmap.mu.RUnlock()
	cmap.mu.Lock()
	defer cmap.mu.Unlock()
	if cmap.inverseBuilt {
		return
	}
	for code, cid := range cmap.codeToCID {
		if prev, ok := cmap.cidToCode[cid]; !ok || (ok && prev > code) {
			cmap.cidToCode[cid] = code
		}
	}
	for code, s := range cmap.codeToUnicode {
		if prev, ok := cmap.unicodeToCode[s]; !ok || (ok && prev > code) {
			cmap.unicodeToCode[s] = code
		}
	}
	sort.Slice(cmap.codespaces, func(i, j int) bool { return cmap.codespaces[i].Low < cmap.codespaces[j].Low })
	cmap.inverseBuilt = true
}
func (cmap *CMap) CharcodeToCID(code CharCode) (CharCode, bool) {
	cid, ok := cmap.codeToCID[code]
	return cid, ok
}

type cmapObject interface{}
type cmapOperand struct{ Operand string }
type integer struct {
	set bool
	val int
}

func (cmap *CMap) CharcodeBytesToUnicode(data []byte) (string, int) {
	codes, ok := cmap.BytesToCharcodes(data)
	if !ok {
		common.Log.Debug("ERROR: CharcodeBytesToUnicode. Not in codespaces. data=[% 02x] cmap=%s", data, cmap)
		return "", 0
	}
	parts := make([]string, len(codes))
	var missing []CharCode
	for i, code := range codes {
		s, ok := cmap.codeToUnicode[code]
		if !ok {
			missing = append(missing, code)
			s = MissingCodeString
		}
		parts[i] = s
	}
	result := strings.Join(parts, "")
	if len(missing) > 0 {
		common.Log.Debug("ERROR: CharcodeBytesToUnicode. Not in map.\n"+"\tdata=[% 02x]=%#q\n"+"\tcharcodes=%02x\n"+"\tmissing=%d %02x\n"+"\tunicode=`%s`\n"+"\tcmap=%s", data, string(data), codes, len(missing), missing, result, cmap)
	}
	return result, len(missing)
}
func (cmap *CMap) StringToCID(s string) (CharCode, bool) {
	cmap.ensureInverseMappings()
	code, ok := cmap.unicodeToCode[s]
	return code, ok
}
func (p *cMapParser) parseComment() (string, error) {
	var buf bytes.Buffer
	_, err := p.skipSpaces()
	if err != nil {
		return buf.String(), err
	}
	first := true
	for {
		b, err := p.reader.Peek(1)
		if err != nil {
			common.Log.Debug("parseComment: err=%v", err)
			return buf.String(), err
		}
		if first && b[0] != '%' {
			return buf.String(), ErrBadCMapComment
		}
		first = false
		if (b[0] != '\r') && (b[0] != '\n') {
			c, _ := p.reader.ReadByte()
			buf.WriteByte(c)
		} else {
			break
		}
	}
	return buf.String(), nil
}
func (cmap *CMap) String() string {
	info := cmap.systemInfo
	parts := []string{fmt.Sprintf("nbits:%d", cmap.nbits), fmt.Sprintf("type:%d", cmap.ctype)}
	if cmap.version != "" {
		parts = append(parts, fmt.Sprintf("version:%s", cmap.version))
	}
	if cmap.useCMap != "" {
		parts = append(parts, fmt.Sprintf("usecmap:%#q", cmap.useCMap))
	}
	parts = append(parts, fmt.Sprintf("systemInfo:%s", info.String()))
	if len(cmap.codespaces) > 0 {
		parts = append(parts, fmt.Sprintf("codespaces:%d", len(cmap.codespaces)))
	}
	if len(cmap.codeToUnicode) > 0 {
		parts = append(parts, fmt.Sprintf("codeToUnicode:%d", len(cmap.codeToUnicode)))
	}
	return fmt.Sprintf("CMAP{%#q %s}", cmap.name, strings.Join(parts, " "))
}

type fbRange struct {
	low    CharCode
	high   CharCode
	target string
}

func (p *cMapParser) parseString() (cmapString, error) {
	p.reader.ReadByte()
	buf := bytes.Buffer{}
	depth := 1
	for {
		b, err := p.reader.Peek(1)
		if err != nil {
			return cmapString{buf.String()}, err
		}
		if b[0] == '\\' {
			p.reader.ReadByte()
			c, err := p.reader.ReadByte()
			if err != nil {
				return cmapString{buf.String()}, err
			}
			if core.IsOctalDigit(c) {
				peek, err := p.reader.Peek(2)
				if err != nil {
					return cmapString{buf.String()}, err
				}
				var digits []byte
				digits = append(digits, c)
				for _, d := range peek {
					if core.IsOctalDigit(d) {
						digits = append(digits, d)
					} else {
						break
					}
				}
				p.reader.Discard(len(digits) - 1)
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Numeric string \"%s\"", digits)
				}
				n, err := strconv.ParseUint(string(digits), 8, 32)
				if err != nil {
					return cmapString{buf.String()}, err
				}
				buf.WriteByte(byte(n))
				continue
			}
			switch c {
			case 'n':
				buf.WriteByte('\n')
			case 'r':
				buf.WriteByte('\r')
			case 't':
				buf.WriteByte('\t')
			case 'b':
				buf.WriteByte('\b')
			case 'f':
				buf.WriteByte('\f')
			case '(':
				buf.WriteByte('(')
			case ')':
				buf.WriteByte(')')
			case '\\':
				buf.WriteByte('\\')
			}
			continue
		} else if b[0] == '(' {
			depth++
		} else if b[0] == ')' {
			depth--
			if depth == 0 {
				p.reader.ReadByte()
				break
			}
		}
		c, _ := p.reader.ReadByte()
		buf.WriteByte(c)
	}
	return cmapString{buf.String()}, nil
}
func newCmapDict() cmapDict { return cmapDict{Dict: map[string]cmapObject{}} }
func NewToUnicodeCMap(codeToRune map[CharCode]string) *CMap {
	cmap := &CMap{name: "Adobe-Identity-UCS", ctype: 2, nbits: 16, systemInfo: CIDSystemInfo{Registry: "Adobe", Ordering: "UCS", Supplement: 0}, codespaces: []Codespace{{Low: 0, High: 0xffff}}, codeToUnicode: codeToRune, unicodeToCode: make(map[string]CharCode, len(codeToRune)), codeToCID: make(map[CharCode]CharCode, len(codeToRune)), cidToCode: make(map[CharCode]CharCode, len(codeToRune))}
	return cmap
}

const (
	tokCIDSystemInfo       = "CIDSystemInfo"
	tokBeginCMap           = "begincmap"
	tokEndCMap             = "endcmap"
	tokBeginCodespaceRange = "begincodespacerange"
	tokEndCodespaceRange   = "endcodespacerange"
	tokBeginBfChar         = "beginbfchar"
	tokEndBfChar           = "endbfchar"
	tokBeginBfRange        = "beginbfrange"
	tokEndBfRange          = "endbfrange"
	tokBeginCIDRange       = "begincidrange"
	tokEndCIDRange         = "endcidrange"
	tokUseCMap             = "usecmap"
	tokWMode               = "WMode"
	tokCMapName            = "CMapName"
	tokCMapType            = "CMapType"
	tokCMapVersion         = "CMapVersion"
)
