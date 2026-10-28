// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package cmap

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap/bcmaps"
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
