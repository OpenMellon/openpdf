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
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

func (prog *PSProgram) String() string {
	var s strings.Builder
	s.WriteString("{ ")
	for _, obj := range *prog {
		s.WriteString(obj.String())
		s.WriteString(" ")
	}
	s.WriteString("}")
	return s.String()
}

// MakeBool returns a new PSBoolean object initialized with `val`.
func MakeBool(val bool) *PSBoolean { b := PSBoolean{}; b.Val = val; return &b }

// PSStack defines a stack of PSObjects. PSObjects can be pushed on or pull from the stack.
type PSStack []PSObject

// PSParser is a basic Postscript parser.
type PSParser struct{ reader *bufio.Reader }

// Execute executes the program for an input parameters `objects` and returns a slice of output objects.
func (ex *PSExecutor) Execute(objects []PSObject) ([]PSObject, error) {
	for _, obj := range objects {
		err := ex.Stack.Push(obj)
		if err != nil {
			return nil, err
		}
	}
	err := ex.program.Exec(ex.Stack)
	if err != nil {
		common.Log.Debug("Exec failed: %v", err)
		return nil, err
	}
	out := []PSObject(*ex.Stack)
	ex.Stack.Empty()
	return out, nil
}
func (i *PSInteger) String() string { return fmt.Sprintf("%d", i.Val) }

var ErrRangeCheck = errors.New("range check error")

// Exec executes the operand `op` in the state specified by `stack`.
func (op *PSOperand) Exec(stack *PSStack) error {
	err := ErrUnsupportedOperand
	switch *op {
	case "abs":
		err = op.abs(stack)
	case "add":
		err = op.add(stack)
	case "and":
		err = op.and(stack)
	case "atan":
		err = op.atan(stack)
	case "bitshift":
		err = op.bitshift(stack)
	case "ceiling":
		err = op.ceiling(stack)
	case "copy":
		err = op.copy(stack)
	case "cos":
		err = op.cos(stack)
	case "cvi":
		err = op.cvi(stack)
	case "cvr":
		err = op.cvr(stack)
	case "div":
		err = op.div(stack)
	case "dup":
		err = op.dup(stack)
	case "eq":
		err = op.eq(stack)
	case "exch":
		err = op.exch(stack)
	case "exp":
		err = op.exp(stack)
	case "floor":
		err = op.floor(stack)
	case "ge":
		err = op.ge(stack)
	case "gt":
		err = op.gt(stack)
	case "idiv":
		err = op.idiv(stack)
	case "if":
		err = op.ifCondition(stack)
	case "ifelse":
		err = op.ifelse(stack)
	case "index":
		err = op.index(stack)
	case "le":
		err = op.le(stack)
	case "log":
		err = op.log(stack)
	case "ln":
		err = op.ln(stack)
	case "lt":
		err = op.lt(stack)
	case "mod":
		err = op.mod(stack)
	case "mul":
		err = op.mul(stack)
	case "ne":
		err = op.ne(stack)
	case "neg":
		err = op.neg(stack)
	case "not":
		err = op.not(stack)
	case "or":
		err = op.or(stack)
	case "pop":
		err = op.pop(stack)
	case "round":
		err = op.round(stack)
	case "roll":
		err = op.roll(stack)
	case "sin":
		err = op.sin(stack)
	case "sqrt":
		err = op.sqrt(stack)
	case "sub":
		err = op.sub(stack)
	case "truncate":
		err = op.truncate(stack)
	case "xor":
		err = op.xor(stack)
	}
	return err
}

func (op *PSOperand) pop(stack *PSStack) error {
	_, err := stack.Pop()
	if err != nil {
		return err
	}
	return nil
}

// NewPSExecutor returns an initialized PSExecutor for an input `program`.
func NewPSExecutor(program *PSProgram) *PSExecutor {
	ex := &PSExecutor{}
	ex.Stack = NewPSStack()
	ex.program = program
	return ex
}

func (p *PSParser) parseOperand() (*PSOperand, error) {
	var operand []byte
	for {
		peek, err := p.reader.Peek(1)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if core.IsDelimiter(peek[0]) {
			break
		}
		if core.IsWhiteSpace(peek[0]) {
			break
		}
		c, _ := p.reader.ReadByte()
		operand = append(operand, c)
	}
	if len(operand) == 0 {
		return nil, errors.New("invalid operand (empty)")
	}
	return MakeOperand(string(operand)), nil
}

func (p *PSParser) parseNumber() (PSObject, error) {
	num, err := core.ParseNumber(p.reader)
	if err != nil {
		return nil, err
	}
	switch n := num.(type) {
	case *core.PdfObjectFloat:
		return MakeReal(float64(*n)), nil
	case *core.PdfObjectInteger:
		return MakeInteger(int(*n)), nil
	}
	return nil, fmt.Errorf("unhandled number type %T", num)
}

func (op *PSOperand) div(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	below, err := stack.Pop()
	if err != nil {
		return err
	}
	topReal, isTopReal := top.(*PSReal)
	topInt, isTopInt := top.(*PSInteger)
	if !isTopReal && !isTopInt {
		return ErrTypeCheck
	}
	if isTopReal && topReal.Val == 0 {
		return ErrUndefinedResult
	}
	if isTopInt && topInt.Val == 0 {
		return ErrUndefinedResult
	}
	belowReal, isBelowReal := below.(*PSReal)
	belowInt, isBelowInt := below.(*PSInteger)
	if !isBelowReal && !isBelowInt {
		return ErrTypeCheck
	}
	var result float64
	if isBelowReal {
		result = belowReal.Val
	} else {
		result = float64(belowInt.Val)
	}
	if isTopReal {
		result /= topReal.Val
	} else {
		result /= float64(topInt.Val)
	}
	err = stack.Push(MakeReal(result))
	return err
}

func (op *PSOperand) idiv(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	below, err := stack.Pop()
	if err != nil {
		return err
	}
	topInt, isTopInt := top.(*PSInteger)
	if !isTopInt {
		return ErrTypeCheck
	}
	if topInt.Val == 0 {
		return ErrUndefinedResult
	}
	belowInt, isTopInt := below.(*PSInteger)
	if !isTopInt {
		return ErrTypeCheck
	}
	result := belowInt.Val / topInt.Val
	err = stack.Push(MakeInteger(result))
	return err
}

// PSInteger represents an integer.
type PSInteger struct{ Val int }

func (op *PSOperand) and(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	below, err := stack.Pop()
	if err != nil {
		return err
	}
	if topBool, isTopBool := top.(*PSBoolean); isTopBool {
		belowBool, isBelowBool := below.(*PSBoolean)
		if !isBelowBool {
			return ErrTypeCheck
		}
		err = stack.Push(MakeBool(topBool.Val && belowBool.Val))
		return err
	}
	if topInt, isTopInt := top.(*PSInteger); isTopInt {
		belowInt, isBelowInt := below.(*PSInteger)
		if !isBelowInt {
			return ErrTypeCheck
		}
		err = stack.Push(MakeInteger(topInt.Val & belowInt.Val))
		return err
	}
	return ErrTypeCheck
}
func (b *PSBoolean) String() string { return fmt.Sprintf("%v", b.Val) }
func (p *PSParser) skipSpaces() (int, error) {
	count := 0
	for {
		buf, err := p.reader.Peek(1)
		if err != nil {
			return 0, err
		}
		if core.IsWhiteSpace(buf[0]) {
			p.reader.ReadByte()
			count++
		} else {
			break
		}
	}
	return count, nil
}

// Empty empties the stack.
func (stack *PSStack) Empty()         { *stack = []PSObject{} }
func (r *PSReal) Duplicate() PSObject { dup := PSReal{}; dup.Val = r.Val; return &dup }
func (op *PSOperand) le(stack *PSStack) error {
	top, err := stack.PopNumberAsFloat64()
	if err != nil {
		return err
	}
	below, err := stack.PopNumberAsFloat64()
	if err != nil {
		return err
	}
	if math.Abs(below-top) < epsilon {
		err := stack.Push(MakeBool(true))
		return err
	} else if below < top {
		err := stack.Push(MakeBool(true))
		return err
	} else {
		err := stack.Push(MakeBool(false))
		return err
	}
}
func (op *PSOperand) String() string { return string(*op) }

// Push pushes an object on top of the stack.
func (stack *PSStack) Push(obj PSObject) error {
	if len(*stack) > 100 {
		return ErrStackOverflow
	}
	*stack = append(*stack, obj)
	return nil
}

// Parse parses the postscript and store as a program that can be executed.
func (p *PSParser) Parse() (*PSProgram, error) {
	p.skipSpaces()
	peek, err := p.reader.Peek(2)
	if err != nil {
		return nil, err
	}
	if peek[0] != '{' {
		return nil, errors.New("invalid PS Program not starting with {")
	}
	prog, err := p.parseFunction()
	if err != nil && err != io.EOF {
		return nil, err
	}
	return prog, err
}

func (op *PSOperand) mod(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	below, err := stack.Pop()
	if err != nil {
		return err
	}
	topInt, isTopInt := top.(*PSInteger)
	if !isTopInt {
		return ErrTypeCheck
	}
	if topInt.Val == 0 {
		return ErrUndefinedResult
	}
	belowInt, isTopInt := below.(*PSInteger)
	if !isTopInt {
		return ErrTypeCheck
	}
	result := belowInt.Val % topInt.Val
	err = stack.Push(MakeInteger(result))
	return err
}

func (op *PSOperand) roll(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	below, err := stack.Pop()
	if err != nil {
		return err
	}
	shift, isTopInt := top.(*PSInteger)
	if !isTopInt {
		return ErrTypeCheck
	}
	count, isTopInt := below.(*PSInteger)
	if !isTopInt {
		return ErrTypeCheck
	}
	if count.Val < 0 {
		return ErrRangeCheck
	}
	if count.Val == 0 || count.Val == 1 {
		return nil
	}
	if count.Val > len(*stack) {
		return ErrStackUnderflow
	}
	for i := 0; i < absInt(shift.Val); i++ {
		var window []PSObject
		window = (*stack)[len(*stack)-count.Val : len(*stack)]
		if shift.Val > 0 {
			last := window[len(window)-1]
			window = append([]PSObject{last}, window[0:len(window)-1]...)
		} else {
			first := window[len(window)-count.Val]
			window = append(window[1:], first)
		}
		rolled := append((*stack)[0:len(*stack)-count.Val], window...)
		stack = &rolled
	}
	return nil
}

func (op *PSOperand) copy(stack *PSStack) error {
	n, err := stack.PopInteger()
	if err != nil {
		return err
	}
	if n < 0 {
		return ErrRangeCheck
	}
	if n > len(*stack) {
		return ErrRangeCheck
	}
	*stack = append(*stack, (*stack)[len(*stack)-n:]...)
	return nil
}

// PopInteger specificially pops an integer from the top of the stack, returning the value as an int.
func (stack *PSStack) PopInteger() (int, error) {
	top, err := stack.Pop()
	if err != nil {
		return 0, err
	}
	if topInt, isTopInt := top.(*PSInteger); isTopInt {
		return topInt.Val, nil
	}
	return 0, ErrTypeCheck
}

func (op *PSOperand) xor(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	below, err := stack.Pop()
	if err != nil {
		return err
	}
	if topBool, isTopBool := top.(*PSBoolean); isTopBool {
		belowBool, isBelowBool := below.(*PSBoolean)
		if !isBelowBool {
			return ErrTypeCheck
		}
		err = stack.Push(MakeBool(topBool.Val != belowBool.Val))
		return err
	}
	if topInt, isTopInt := top.(*PSInteger); isTopInt {
		belowInt, isBelowInt := below.(*PSInteger)
		if !isBelowInt {
			return ErrTypeCheck
		}
		err = stack.Push(MakeInteger(topInt.Val ^ belowInt.Val))
		return err
	}
	return ErrTypeCheck
}

// PSBoolean represents a boolean value.
type PSBoolean struct{ Val bool }

func (op *PSOperand) neg(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	if topReal, isTopReal := top.(*PSReal); isTopReal {
		err = stack.Push(MakeReal(-topReal.Val))
		return err
	} else if topInt, isTopInt := top.(*PSInteger); isTopInt {
		err = stack.Push(MakeInteger(-topInt.Val))
		return err
	} else {
		return ErrTypeCheck
	}
}

func (op *PSOperand) dup(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	err = stack.Push(top)
	if err != nil {
		return err
	}
	err = stack.Push(top.Duplicate())
	return err
}
