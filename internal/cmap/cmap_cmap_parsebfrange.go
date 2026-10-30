// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package cmap

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/intconv"
)

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
			high = min(hexToCharCode(t), 0xffff)
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
			typ = intconv.ClampInt(t.val)
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

type (
	cmapObject  any
	cmapOperand struct{ Operand string }
	integer     struct {
		set bool
		val int
	}
)

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
				n, err := strconv.ParseUint(string(digits), 8, 16)
				if err != nil {
					return cmapString{buf.String()}, err
				}
				buf.WriteByte(byte(n & 0xff))
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
