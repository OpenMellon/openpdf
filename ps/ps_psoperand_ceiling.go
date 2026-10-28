// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package ps implements various functionalities needed for handling Postscript for PDF uses, in particular
// for PDF function type 4.
//
// Package ps implements various functionalities needed for handling Postscript for PDF uses, in particular
// for PDF function type 4.
package ps

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

func (op *PSOperand) ceiling(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	if topReal, isTopReal := top.(*PSReal); isTopReal {
		err = stack.Push(MakeReal(math.Ceil(topReal.Val)))
	} else if topInt, isTopInt := top.(*PSInteger); isTopInt {
		err = stack.Push(MakeInteger(topInt.Val))
	} else {
		err = ErrTypeCheck
	}
	return err
}

func (op *PSOperand) cvr(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	if topReal, isTopReal := top.(*PSReal); isTopReal {
		err = stack.Push(MakeReal(topReal.Val))
	} else if topInt, isTopInt := top.(*PSInteger); isTopInt {
		err = stack.Push(MakeReal(float64(topInt.Val)))
	} else {
		return ErrTypeCheck
	}
	return err
}

var ErrStackUnderflow = errors.New("stack underflow")

const epsilon = 0.000001

// PSReal represents a real number.
type PSReal struct{ Val float64 }

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (op *PSOperand) ne(stack *PSStack) error {
	err := op.eq(stack)
	if err != nil {
		return err
	}
	err = op.not(stack)
	return err
}

// MakeOperand returns a new PSOperand object based on string `val`.
func MakeOperand(val string) *PSOperand { op := PSOperand(val); return &op }

func (op *PSOperand) log(stack *PSStack) error {
	top, err := stack.PopNumberAsFloat64()
	if err != nil {
		return err
	}
	result := math.Log10(top)
	err = stack.Push(MakeReal(result))
	return err
}

func (op *PSOperand) gt(stack *PSStack) error {
	top, err := stack.PopNumberAsFloat64()
	if err != nil {
		return err
	}
	below, err := stack.PopNumberAsFloat64()
	if err != nil {
		return err
	}
	if math.Abs(below-top) < epsilon {
		err := stack.Push(MakeBool(false))
		return err
	} else if below > top {
		err := stack.Push(MakeBool(true))
		return err
	} else {
		err := stack.Push(MakeBool(false))
		return err
	}
}

func (op *PSOperand) round(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	if topReal, isTopReal := top.(*PSReal); isTopReal {
		err = stack.Push(MakeReal(math.Floor(topReal.Val + 0.5)))
	} else if topInt, isTopInt := top.(*PSInteger); isTopInt {
		err = stack.Push(MakeInteger(topInt.Val))
	} else {
		return ErrTypeCheck
	}
	return err
}

// Append appends an object to the PSProgram.
func (prog *PSProgram) Append(obj PSObject) { *prog = append(*prog, obj) }
func (op *PSOperand) Duplicate() PSObject   { dup := *op; return &dup }

var ErrStackOverflow = errors.New("stack overflow")

func (p *PSParser) parseFunction() (*PSProgram, error) {
	c, _ := p.reader.ReadByte()
	if c != '{' {
		return nil, errors.New("invalid function")
	}
	prog := NewPSProgram()
	for {
		p.skipSpaces()
		p.skipComments()
		peek, err := p.reader.Peek(2)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Peek string: %s", string(peek))
		}
		if peek[0] == '}' {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("EOF function")
			}
			p.reader.ReadByte()
			break
		} else if peek[0] == '{' {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Function!")
			}
			nested, err := p.parseFunction()
			if err != nil {
				return nil, err
			}
			prog.Append(nested)
		} else if core.IsDecimalDigit(peek[0]) || (peek[0] == '-' && core.IsDecimalDigit(peek[1])) {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Number!")
			}
			num, err := p.parseNumber()
			if err != nil {
				return nil, err
			}
			prog.Append(num)
		} else {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Operand or bool?")
			}
			peek, _ = p.reader.Peek(5)
			str := string(peek)
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Peek str: %s", str)
			}
			if (len(str) > 4) && (str[:5] == "false") {
				boolean, err := p.parseBool()
				if err != nil {
					return nil, err
				}
				prog.Append(boolean)
			} else if (len(str) > 3) && (str[:4] == "true") {
				boolean, err := p.parseBool()
				if err != nil {
					return nil, err
				}
				prog.Append(boolean)
			} else {
				op, err := p.parseOperand()
				if err != nil {
					return nil, err
				}
				prog.Append(op)
			}
		}
	}
	return prog, nil
}

func (op *PSOperand) cos(stack *PSStack) error {
	top, err := stack.PopNumberAsFloat64()
	if err != nil {
		return err
	}
	result := math.Cos(top * math.Pi / 180.0)
	err = stack.Push(MakeReal(result))
	return err
}

// MakeReal returns a new PSReal object initialized with `val`.
func MakeReal(val float64) *PSReal { r := PSReal{}; r.Val = val; return &r }

func (op *PSOperand) truncate(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	if topReal, isTopReal := top.(*PSReal); isTopReal {
		result := int(topReal.Val)
		err = stack.Push(MakeReal(float64(result)))
	} else if topInt, isTopInt := top.(*PSInteger); isTopInt {
		err = stack.Push(MakeInteger(topInt.Val))
	} else {
		return ErrTypeCheck
	}
	return err
}

// PSExecutor has its own execution stack and is used to executre a PS routine (program).
type PSExecutor struct {
	Stack   *PSStack
	program *PSProgram
}

// PSObject represents a postscript object.
type PSObject interface {
	// Duplicate makes a fresh copy of the PSObject.
	Duplicate() PSObject

	// DebugString returns a descriptive representation of the PSObject with more information than String()
	// for debugging purposes.
	DebugString() string

	// String returns a string representation of the PSObject.
	String() string
}

// MakeInteger returns a new PSInteger object initialized with `val`.
func MakeInteger(val int) *PSInteger     { i := PSInteger{}; i.Val = val; return &i }
func (b *PSBoolean) DebugString() string { return fmt.Sprintf("bool:%v", b.Val) }

// String returns a string representation of the stack.
func (stack *PSStack) String() string {
	var s strings.Builder
	s.WriteString("[ ")
	for _, obj := range *stack {
		s.WriteString(obj.String())
		s.WriteString(" ")
	}
	s.WriteString("]")
	return s.String()
}

func (op *PSOperand) sqrt(stack *PSStack) error {
	top, err := stack.PopNumberAsFloat64()
	if err != nil {
		return err
	}
	if top < 0 {
		return ErrRangeCheck
	}
	result := math.Sqrt(top)
	err = stack.Push(MakeReal(result))
	return err
}

func (i *PSInteger) DebugString() string {
	return fmt.Sprintf("int:%d", i.Val)
}

func (op *PSOperand) eq(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	below, err := stack.Pop()
	if err != nil {
		return err
	}
	topBool, isTopBool := top.(*PSBoolean)
	belowBool, isBelowBool := below.(*PSBoolean)
	if isTopBool || isBelowBool {
		var err error
		if isTopBool && isBelowBool {
			err = stack.Push(MakeBool(topBool.Val == belowBool.Val))
		} else {
			err = stack.Push(MakeBool(false))
		}
		return err
	}
	var topNum float64
	var belowNum float64
	if topInt, isTopInt := top.(*PSInteger); isTopInt {
		topNum = float64(topInt.Val)
	} else if topReal, isTopReal := top.(*PSReal); isTopReal {
		topNum = topReal.Val
	} else {
		return ErrTypeCheck
	}
	if belowInt, isBelowInt := below.(*PSInteger); isBelowInt {
		belowNum = float64(belowInt.Val)
	} else if belowReal, isBelowReal := below.(*PSReal); isBelowReal {
		belowNum = belowReal.Val
	} else {
		return ErrTypeCheck
	}
	if math.Abs(belowNum-topNum) < epsilon {
		err = stack.Push(MakeBool(true))
	} else {
		err = stack.Push(MakeBool(false))
	}
	return err
}

// Pop pops an object from the top of the stack.
func (stack *PSStack) Pop() (PSObject, error) {
	if len(*stack) < 1 {
		return nil, ErrStackUnderflow
	}
	top := (*stack)[len(*stack)-1]
	*stack = (*stack)[0 : len(*stack)-1]
	return top, nil
}

func (op *PSOperand) DebugString() string {
	return fmt.Sprintf("op:'%s'", *op)
}

func (op *PSOperand) cvi(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	if topReal, isTopReal := top.(*PSReal); isTopReal {
		n := int(topReal.Val)
		err = stack.Push(MakeInteger(n))
	} else if topInt, isTopInt := top.(*PSInteger); isTopInt {
		n := topInt.Val
		err = stack.Push(MakeInteger(n))
	} else {
		return ErrTypeCheck
	}
	return err
}
func (r *PSReal) DebugString() string { return fmt.Sprintf("real:%.5f", r.Val) }
func (p *PSParser) parseBool() (*PSBoolean, error) {
	buf, err := p.reader.Peek(4)
	if err != nil {
		return MakeBool(false), err
	}
	if (len(buf) >= 4) && (string(buf[:4]) == "true") {
		p.reader.Discard(4)
		return MakeBool(true), nil
	}
	buf, err = p.reader.Peek(5)
	if err != nil {
		return MakeBool(false), err
	}
	if (len(buf) >= 5) && (string(buf[:5]) == "false") {
		p.reader.Discard(5)
		return MakeBool(false), nil
	}
	return MakeBool(false), errors.New("unexpected boolean string")
}

func (op *PSOperand) ln(stack *PSStack) error {
	top, err := stack.PopNumberAsFloat64()
	if err != nil {
		return err
	}
	result := math.Log(top)
	err = stack.Push(MakeReal(result))
	return err
}

func (op *PSOperand) index(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	idx, isTopInt := top.(*PSInteger)
	if !isTopInt {
		return ErrTypeCheck
	}
	if idx.Val < 0 {
		return ErrRangeCheck
	}
	if idx.Val > len(*stack)-1 {
		return ErrStackUnderflow
	}
	elem := (*stack)[len(*stack)-1-idx.Val]
	err = stack.Push(elem.Duplicate())
	return err
}

// NewPSStack returns an initialized PSStack.
func NewPSStack() *PSStack { return &PSStack{} }

// NewPSParser returns a new instance of the PDF Postscript parser from input data.
func NewPSParser(content []byte) *PSParser {
	p := PSParser{}
	buf := bytes.NewBuffer(content)
	p.reader = bufio.NewReader(buf)
	return &p
}
func (i *PSInteger) Duplicate() PSObject { dup := PSInteger{}; dup.Val = i.Val; return &dup }
func (op *PSOperand) bitshift(stack *PSStack) error {
	shift, err := stack.PopInteger()
	if err != nil {
		return err
	}
	val, err := stack.PopInteger()
	if err != nil {
		return err
	}
	var result int
	if shift >= 0 {
		result = val << uint(shift)
	} else {
		result = val >> uint(-shift)
	}
	err = stack.Push(MakeInteger(result))
	return err
}
