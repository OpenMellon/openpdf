// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package huffman

import (
	"errors"
	"fmt"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	jbig2internal "github.com/OpenMellon/openpdf/internal/jbig2/internal"
	"math"
	"strings"
	"sync"
)

func newStandardTable(defs [][]int32) (*StandardTable, error) {
	var codes []*Code
	for i := 0; i < len(defs); i++ {
		prefixLen := defs[i][0]
		rangeLen := defs[i][1]
		rangeLow := defs[i][2]
		var isLowerRange bool
		if len(defs[i]) > 3 {
			isLowerRange = true
		}
		codes = append(codes, NewCode(prefixLen, rangeLen, rangeLow, isLowerRange))
	}
	table := &StandardTable{root: newInternalNode(0)}
	if err := table.InitTree(codes); err != nil {
		return nil, err
	}
	return table, nil
}
func NewEncodedTable(table BasicTabler) (*EncodedTable, error) {
	t := &EncodedTable{root: &InternalNode{}, BasicTabler: table}
	if err := t.parseTable(); err != nil {
		return nil, err
	}
	return t, nil
}
func (t *EncodedTable) InitTree(codeTable []*Code) error {
	assignPrefixCodes(codeTable)
	for _, code := range codeTable {
		if err := t.root.append(code); err != nil {
			return err
		}
	}
	return nil
}
func (n *ValueNode) String() string {
	return fmt.Sprintf("%d/%d", n.rangeLen, n.rangeLow)
}
func newInternalNode(depth int32) *InternalNode                 { return &InternalNode{depth: depth} }
func (t *EncodedTable) Decode(r *bitwise.Reader) (int64, error) { return t.root.Decode(r) }
func maxInt32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
func (n *InternalNode) append(code *Code) (err error) {
	if code.prefixLen == 0 {
		return nil
	}
	shift := code.prefixLen - 1 - n.depth
	if shift < 0 {
		return errors.New("negative shifting is not allowed")
	}
	bit := (code.code >> uint(shift)) & 0x1
	if shift == 0 {
		if code.rangeLen == -1 {
			if bit == 1 {
				if n.one != nil {
					return fmt.Errorf("OOB already set for code %s", code)
				}
				n.one = newOutOfBandNode(code)
			} else {
				if n.zero != nil {
					return fmt.Errorf("OOB already set for code %s", code)
				}
				n.zero = newOutOfBandNode(code)
			}
		} else {
			if bit == 1 {
				if n.one != nil {
					return fmt.Errorf("value Node already set for code %s", code)
				}
				n.one = newValueNode(code)
			} else {
				if n.zero != nil {
					return fmt.Errorf("value Node already set for code %s", code)
				}
				n.zero = newValueNode(code)
			}
		}
	} else {
		if bit == 1 {
			if n.one == nil {
				n.one = newInternalNode(n.depth + 1)
			}
			if err = n.one.(*InternalNode).append(code); err != nil {
				return err
			}
		} else {
			if n.zero == nil {
				n.zero = newInternalNode(n.depth + 1)
			}
			if err = n.zero.(*InternalNode).append(code); err != nil {
				return err
			}
		}
	}
	return nil
}

type BasicTabler interface {
	HtHigh() int32
	HtLow() int32
	StreamReader() *bitwise.Reader
	HtPS() int32
	HtRS() int32
	HtOOB() int32
}
type Code struct {
	prefixLen    int32
	rangeLen     int32
	rangeLow     int32
	isLowerRange bool
	code         int32
}

func (t *FixedSizeTable) InitTree(codeTable []*Code) error {
	assignPrefixCodes(codeTable)
	for _, code := range codeTable {
		err := t.root.append(code)
		if err != nil {
			return err
		}
	}
	return nil
}

var standardTableDefs = [][][]int32{{{1, 4, 0}, {2, 8, 16}, {3, 16, 272}, {3, 32, 65808}}, {{1, 0, 0}, {2, 0, 1}, {3, 0, 2}, {4, 3, 3}, {5, 6, 11}, {6, 32, 75}, {6, -1, 0}}, {{8, 8, -256}, {1, 0, 0}, {2, 0, 1}, {3, 0, 2}, {4, 3, 3}, {5, 6, 11}, {8, 32, -257, 999}, {7, 32, 75}, {6, -1, 0}}, {{1, 0, 1}, {2, 0, 2}, {3, 0, 3}, {4, 3, 4}, {5, 6, 12}, {5, 32, 76}}, {{7, 8, -255}, {1, 0, 1}, {2, 0, 2}, {3, 0, 3}, {4, 3, 4}, {5, 6, 12}, {7, 32, -256, 999}, {6, 32, 76}}, {{5, 10, -2048}, {4, 9, -1024}, {4, 8, -512}, {4, 7, -256}, {5, 6, -128}, {5, 5, -64}, {4, 5, -32}, {2, 7, 0}, {3, 7, 128}, {3, 8, 256}, {4, 9, 512}, {4, 10, 1024}, {6, 32, -2049, 999}, {6, 32, 2048}}, {{4, 9, -1024}, {3, 8, -512}, {4, 7, -256}, {5, 6, -128}, {5, 5, -64}, {4, 5, -32}, {4, 5, 0}, {5, 5, 32}, {5, 6, 64}, {4, 7, 128}, {3, 8, 256}, {3, 9, 512}, {3, 10, 1024}, {5, 32, -1025, 999}, {5, 32, 2048}}, {{8, 3, -15}, {9, 1, -7}, {8, 1, -5}, {9, 0, -3}, {7, 0, -2}, {4, 0, -1}, {2, 1, 0}, {5, 0, 2}, {6, 0, 3}, {3, 4, 4}, {6, 1, 20}, {4, 4, 22}, {4, 5, 38}, {5, 6, 70}, {5, 7, 134}, {6, 7, 262}, {7, 8, 390}, {6, 10, 646}, {9, 32, -16, 999}, {9, 32, 1670}, {2, -1, 0}}, {{8, 4, -31}, {9, 2, -15}, {8, 2, -11}, {9, 1, -7}, {7, 1, -5}, {4, 1, -3}, {3, 1, -1}, {3, 1, 1}, {5, 1, 3}, {6, 1, 5}, {3, 5, 7}, {6, 2, 39}, {4, 5, 43}, {4, 6, 75}, {5, 7, 139}, {5, 8, 267}, {6, 8, 523}, {7, 9, 779}, {6, 11, 1291}, {9, 32, -32, 999}, {9, 32, 3339}, {2, -1, 0}}, {{7, 4, -21}, {8, 0, -5}, {7, 0, -4}, {5, 0, -3}, {2, 2, -2}, {5, 0, 2}, {6, 0, 3}, {7, 0, 4}, {8, 0, 5}, {2, 6, 6}, {5, 5, 70}, {6, 5, 102}, {6, 6, 134}, {6, 7, 198}, {6, 8, 326}, {6, 9, 582}, {6, 10, 1094}, {7, 11, 2118}, {8, 32, -22, 999}, {8, 32, 4166}, {2, -1, 0}}, {{1, 0, 1}, {2, 1, 2}, {4, 0, 4}, {4, 1, 5}, {5, 1, 7}, {5, 2, 9}, {6, 2, 13}, {7, 2, 17}, {7, 3, 21}, {7, 4, 29}, {7, 5, 45}, {7, 6, 77}, {7, 32, 141}}, {{1, 0, 1}, {2, 0, 2}, {3, 1, 3}, {5, 0, 5}, {5, 1, 6}, {6, 1, 8}, {7, 0, 10}, {7, 1, 11}, {7, 2, 13}, {7, 3, 17}, {7, 4, 25}, {8, 5, 41}, {8, 32, 73}}, {{1, 0, 1}, {3, 0, 2}, {4, 0, 3}, {5, 0, 4}, {4, 1, 5}, {3, 3, 7}, {6, 1, 15}, {6, 2, 17}, {6, 3, 21}, {6, 4, 29}, {6, 5, 45}, {7, 6, 77}, {7, 32, 141}}, {{3, 0, -2}, {3, 0, -1}, {1, 0, 0}, {3, 0, 1}, {3, 0, 2}}, {{7, 4, -24}, {6, 2, -8}, {5, 1, -4}, {4, 0, -2}, {3, 0, -1}, {1, 0, 0}, {3, 0, 1}, {4, 0, 2}, {5, 1, 3}, {6, 2, 5}, {7, 4, 9}, {7, 32, -25, 999}, {7, 32, 25}}}

type Node interface {
	Decode(r *bitwise.Reader) (int64, error)
	String() string
}
type EncodedTable struct {
	BasicTabler
	root *InternalNode
}

func newOutOfBandNode(code *Code) *OutOfBandNode { return &OutOfBandNode{} }
func (t *StandardTable) RootNode() *InternalNode { return t.root }

var _ Node = &InternalNode{}

func NewFixedSizeTable(codeTable []*Code) (*FixedSizeTable, error) {
	t := &FixedSizeTable{root: &InternalNode{}}
	if err := t.InitTree(codeTable); err != nil {
		return nil, err
	}
	return t, nil
}
func newValueNode(code *Code) *ValueNode {
	return &ValueNode{rangeLen: code.rangeLen, rangeLow: code.rangeLow, isLowerRange: code.isLowerRange}
}
func (n *ValueNode) Decode(r *bitwise.Reader) (int64, error) {
	bits, err := r.ReadBits(byte(n.rangeLen))
	if err != nil {
		return 0, err
	}
	if n.isLowerRange {
		bits = -bits
	}
	return int64(n.rangeLow) + int64(bits), nil
}
func (c *Code) String() string {
	var bitString string
	if c.code != -1 {
		bitString = codeBitString(c.code, c.prefixLen)
	} else {
		bitString = "?"
	}
	return fmt.Sprintf("%s/%d/%d/%d", bitString, c.prefixLen, c.rangeLen, c.rangeLow)
}
func (t *FixedSizeTable) RootNode() *InternalNode { return t.root }
func (t *FixedSizeTable) String() string          { return t.root.String() + "\n" }

type ValueNode struct {
	rangeLen     int32
	rangeLow     int32
	isLowerRange bool
}

func (t *FixedSizeTable) Decode(r *bitwise.Reader) (int64, error) { return t.root.Decode(r) }

type Tabler interface {
	Decode(r *bitwise.Reader) (int64, error)
	InitTree(codeTable []*Code) error
	String() string
	RootNode() *InternalNode
}
type InternalNode struct {
	depth int32
	zero  Node
	one   Node
}

func (t *StandardTable) Decode(r *bitwise.Reader) (int64, error) { return t.root.Decode(r) }

type FixedSizeTable struct{ root *InternalNode }

func NewCode(prefixLength, rangeLength, rangeLow int32, isLowerRange bool) *Code {
	return &Code{prefixLen: prefixLength, rangeLen: rangeLength, rangeLow: rangeLow, isLowerRange: isLowerRange, code: -1}
}
func (t *StandardTable) InitTree(codeTable []*Code) error {
	assignPrefixCodes(codeTable)
	for _, code := range codeTable {
		if err := t.root.append(code); err != nil {
			return err
		}
	}
	return nil
}
func (n *OutOfBandNode) Decode(r *bitwise.Reader) (int64, error) { return 0, jbig2internal.ErrOOB }
func (n *OutOfBandNode) String() string {
	return fmt.Sprintf("%064b", int64(math.MaxInt64))
}
func codeBitString(code, length int32) string {
	var bit int32
	runes := make([]rune, length)
	for i := int32(1); i <= length; i++ {
		bit = code >> uint(length-i) & 1
		if bit != 0 {
			runes[i-1] = '1'
		} else {
			runes[i-1] = '0'
		}
	}
	return string(runes)
}
func (t *EncodedTable) parseTable() error {
	var (
		codes                         []*Code
		prefixLen, rangeLen, rangeLow int32
		bits                          uint64
		err                           error
	)
	reader := t.StreamReader()
	cur := t.HtLow()
	for cur < t.HtHigh() {
		bits, err = reader.ReadBits(byte(t.HtPS()))
		if err != nil {
			return err
		}
		prefixLen = int32(bits)
		bits, err = reader.ReadBits(byte(t.HtRS()))
		if err != nil {
			return err
		}
		rangeLen = int32(bits)
		codes = append(codes, NewCode(prefixLen, rangeLen, rangeLow, false))
		cur += 1 << uint(rangeLen)
	}
	bits, err = reader.ReadBits(byte(t.HtPS()))
	if err != nil {
		return err
	}
	prefixLen = int32(bits)
	rangeLen = 32
	rangeLow = t.HtLow() - 1
	codes = append(codes, NewCode(prefixLen, rangeLen, rangeLow, true))
	bits, err = reader.ReadBits(byte(t.HtPS()))
	if err != nil {
		return err
	}
	prefixLen = int32(bits)
	rangeLen = 32
	rangeLow = t.HtHigh()
	codes = append(codes, NewCode(prefixLen, rangeLen, rangeLow, false))
	if t.HtOOB() == 1 {
		bits, err = reader.ReadBits(byte(t.HtPS()))
		if err != nil {
			return err
		}
		prefixLen = int32(bits)
		codes = append(codes, NewCode(prefixLen, -1, -1, false))
	}
	if err = t.InitTree(codes); err != nil {
		return err
	}
	return nil
}
func GetStandardTable(number int) (Tabler, error) {
	if number <= 0 || number > len(standardTables) {
		return nil, errors.New("index out of range")
	}
	idx := number - 1
	standardTableOnce[idx].Do(func() { standardTables[idx], standardTableErrs[idx] = newStandardTable(standardTableDefs[idx]) })
	if standardTableErrs[idx] != nil {
		return nil, standardTableErrs[idx]
	}
	return standardTables[idx], nil
}

type OutOfBandNode struct{}

var _ Tabler = &EncodedTable{}

func (n *InternalNode) pad(sb *strings.Builder) {
	for i := int32(0); i < n.depth; i++ {
		sb.WriteString("   ")
	}
}

var _ Node = &ValueNode{}
var (
	standardTables    = make([]Tabler, len(standardTableDefs))
	standardTableOnce = make([]sync.Once, len(standardTableDefs))
	standardTableErrs = make([]error, len(standardTableDefs))
)

func assignPrefixCodes(codes []*Code) {
	var maxPrefixLen int32
	for _, c := range codes {
		maxPrefixLen = maxInt32(maxPrefixLen, c.prefixLen)
	}
	lenCount := make([]int32, maxPrefixLen+1)
	for _, c := range codes {
		lenCount[c.prefixLen]++
	}
	var curCode int32
	firstCode := make([]int32, len(lenCount)+1)
	lenCount[0] = 0
	for curLen := int32(1); curLen <= int32(len(lenCount)); curLen++ {
		firstCode[curLen] = (firstCode[curLen-1] + (lenCount[curLen-1])) << 1
		curCode = firstCode[curLen]
		for _, c := range codes {
			if c.prefixLen == curLen {
				c.code = curCode
				curCode++
			}
		}
	}
}
func (n *InternalNode) String() string {
	sb := &strings.Builder{}
	sb.WriteString("\n")
	n.pad(sb)
	sb.WriteString("0: ")
	sb.WriteString(n.zero.String() + "\n")
	n.pad(sb)
	sb.WriteString("1: ")
	sb.WriteString(n.one.String() + "\n")
	return sb.String()
}
func (t *EncodedTable) RootNode() *InternalNode { return t.root }
func (n *InternalNode) Decode(r *bitwise.Reader) (int64, error) {
	bit, err := r.ReadBit()
	if err != nil {
		return 0, err
	}
	if bit == 0 {
		return n.zero.Decode(r)
	}
	return n.one.Decode(r)
}

type StandardTable struct{ root *InternalNode }

func (t *StandardTable) String() string { return t.root.String() + "\n" }
func (t *EncodedTable) String() string  { return t.root.String() + "\n" }

var _ Node = &OutOfBandNode{}
