// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package cmap

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap/bcmaps"
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
			if cid > math.MaxUint32 {
				return errors.New("invalid cid value")
			}
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
	slices.Sort(codes)
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

type (
	cMapParser    struct{ reader *bufio.Reader }
	cmapHexString struct {
		numBytes int
		data     []byte
	}
)

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
