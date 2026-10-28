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

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

var ErrUndefinedResult = errors.New("undefined result error")

func (op *PSOperand) mul(stack *PSStack) error {
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
	belowReal, isBelowReal := below.(*PSReal)
	belowInt, isBelowInt := below.(*PSInteger)
	if !isBelowReal && !isBelowInt {
		return ErrTypeCheck
	}
	if isTopInt && isBelowInt {
		product := topInt.Val * belowInt.Val
		err := stack.Push(MakeInteger(product))
		return err
	}
	var result float64
	if isTopReal {
		result = topReal.Val
	} else {
		result = float64(topInt.Val)
	}
	if isBelowReal {
		result *= belowReal.Val
	} else {
		result *= float64(belowInt.Val)
	}
	err = stack.Push(MakeReal(result))
	return err
}

func (op *PSOperand) lt(stack *PSStack) error {
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
	} else if below < top {
		err := stack.Push(MakeBool(true))
		return err
	} else {
		err := stack.Push(MakeBool(false))
		return err
	}
}

var ErrTypeCheck = errors.New("type check error")

// Exec executes the program, typically leaving output values on the stack.
func (prog *PSProgram) Exec(stack *PSStack) error {
	for _, obj := range *prog {
		var err error
		switch v := obj.(type) {
		case *PSInteger:
			integer := v
			err = stack.Push(integer)
		case *PSReal:
			realNum := v
			err = stack.Push(realNum)
		case *PSBoolean:
			boolean := v
			err = stack.Push(boolean)
		case *PSProgram:
			nested := v
			err = stack.Push(nested)
		case *PSOperand:
			op := v
			err = op.Exec(stack)
		default:
			return ErrTypeCheck
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *PSParser) skipComments() error {
	if _, err := p.skipSpaces(); err != nil {
		return err
	}
	first := true
	for {
		buf, err := p.reader.Peek(1)
		if err != nil {
			common.Log.Debug("Error %s", err.Error())
			return err
		}
		if first && buf[0] != '%' {
			return nil
		}
		first = false
		if (buf[0] != '\r') && (buf[0] != '\n') {
			p.reader.ReadByte()
		} else {
			break
		}
	}
	return p.skipComments()
}

func (op *PSOperand) sub(stack *PSStack) error {
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
	belowReal, isBelowReal := below.(*PSReal)
	belowInt, isBelowInt := below.(*PSInteger)
	if !isBelowReal && !isBelowInt {
		return ErrTypeCheck
	}
	if isTopInt && isBelowInt {
		diff := belowInt.Val - topInt.Val
		err := stack.Push(MakeInteger(diff))
		return err
	}
	var result float64 = 0
	if isBelowReal {
		result = belowReal.Val
	} else {
		result = float64(belowInt.Val)
	}
	if isTopReal {
		result -= topReal.Val
	} else {
		result -= float64(topInt.Val)
	}
	err = stack.Push(MakeReal(result))
	return err
}

// PSOperand represents a Postscript operand (text string).
type PSOperand string

func (prog *PSProgram) DebugString() string {
	s := "{ "
	for _, obj := range *prog {
		s += obj.DebugString()
		s += " "
	}
	s += "}"
	return s
}

func (op *PSOperand) ifCondition(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	below, err := stack.Pop()
	if err != nil {
		return err
	}
	topProg, isTopProg := top.(*PSProgram)
	if !isTopProg {
		return ErrTypeCheck
	}
	belowBool, isTopProg := below.(*PSBoolean)
	if !isTopProg {
		return ErrTypeCheck
	}
	if belowBool.Val {
		err := topProg.Exec(stack)
		return err
	}
	return nil
}

func (op *PSOperand) not(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	if topBool, isTopBool := top.(*PSBoolean); isTopBool {
		err = stack.Push(MakeBool(!topBool.Val))
		return err
	} else if topInt, isTopInt := top.(*PSInteger); isTopInt {
		err = stack.Push(MakeInteger(^topInt.Val))
		return err
	} else {
		return ErrTypeCheck
	}
}

func (op *PSOperand) floor(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	if topReal, isTopReal := top.(*PSReal); isTopReal {
		err = stack.Push(MakeReal(math.Floor(topReal.Val)))
	} else if topInt, isTopInt := top.(*PSInteger); isTopInt {
		err = stack.Push(MakeInteger(topInt.Val))
	} else {
		return ErrTypeCheck
	}
	return err
}

func (op *PSOperand) atan(stack *PSStack) error {
	top, err := stack.PopNumberAsFloat64()
	if err != nil {
		return err
	}
	below, err := stack.PopNumberAsFloat64()
	if err != nil {
		return err
	}
	if top == 0 {
		var err error
		if below < 0 {
			err = stack.Push(MakeReal(270))
		} else {
			err = stack.Push(MakeReal(90))
		}
		return err
	}
	ratio := below / top
	degrees := math.Atan(ratio) * 180 / math.Pi
	err = stack.Push(MakeReal(degrees))
	return err
}

func (op *PSOperand) exp(stack *PSStack) error {
	top, err := stack.PopNumberAsFloat64()
	if err != nil {
		return err
	}
	below, err := stack.PopNumberAsFloat64()
	if err != nil {
		return err
	}
	if math.Abs(top) < 1 && below < 0 {
		return ErrUndefinedResult
	}
	result := math.Pow(below, top)
	err = stack.Push(MakeReal(result))
	return err
}

// PSProgram defines a Postscript program which is a series of PS objects (arguments, commands, programs etc).
type PSProgram []PSObject

func (op *PSOperand) abs(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	if topReal, isTopReal := top.(*PSReal); isTopReal {
		val := topReal.Val
		if val < 0 {
			err = stack.Push(MakeReal(-val))
		} else {
			err = stack.Push(MakeReal(val))
		}
	} else if topInt, isTopInt := top.(*PSInteger); isTopInt {
		val := topInt.Val
		if val < 0 {
			err = stack.Push(MakeInteger(-val))
		} else {
			err = stack.Push(MakeInteger(val))
		}
	} else {
		return ErrTypeCheck
	}
	return err
}

func (op *PSOperand) ifelse(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	below, err := stack.Pop()
	if err != nil {
		return err
	}
	third, err := stack.Pop()
	if err != nil {
		return err
	}
	topProg, isTopProg := top.(*PSProgram)
	if !isTopProg {
		return ErrTypeCheck
	}
	belowProg, isTopProg := below.(*PSProgram)
	if !isTopProg {
		return ErrTypeCheck
	}
	thirdBool, isTopProg := third.(*PSBoolean)
	if !isTopProg {
		return ErrTypeCheck
	}
	if thirdBool.Val {
		err := belowProg.Exec(stack)
		return err
	}
	err = topProg.Exec(stack)
	return err
}

func (b *PSBoolean) Duplicate() PSObject {
	dup := PSBoolean{}
	dup.Val = b.Val
	return &dup
}

func (op *PSOperand) sin(stack *PSStack) error {
	top, err := stack.PopNumberAsFloat64()
	if err != nil {
		return err
	}
	result := math.Sin(top * math.Pi / 180.0)
	err = stack.Push(MakeReal(result))
	return err
}

func (op *PSOperand) ge(stack *PSStack) error {
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
	} else if below > top {
		err := stack.Push(MakeBool(true))
		return err
	} else {
		err := stack.Push(MakeBool(false))
		return err
	}
}

// NewPSProgram returns an empty, initialized PSProgram.
func NewPSProgram() *PSProgram { return &PSProgram{} }

// DebugString returns a descriptive string representation of the stack - intended for debugging.
func (stack *PSStack) DebugString() string {
	s := "[ "
	for _, obj := range *stack {
		s += obj.DebugString()
		s += " "
	}
	s += "]"
	return s
}
func (r *PSReal) String() string { return fmt.Sprintf("%.5f", r.Val) }

// PopNumberAsFloat64 pops and return the numeric value of the top of the stack as a float64.
// Real or integer only.
func (stack *PSStack) PopNumberAsFloat64() (float64, error) {
	top, err := stack.Pop()
	if err != nil {
		return 0, err
	}
	if topReal, isTopReal := top.(*PSReal); isTopReal {
		return topReal.Val, nil
	} else if topInt, isTopInt := top.(*PSInteger); isTopInt {
		return float64(topInt.Val), nil
	} else {
		return 0, ErrTypeCheck
	}
}

func (prog *PSProgram) Duplicate() PSObject {
	dup := &PSProgram{}
	for _, obj := range *prog {
		dup.Append(obj.Duplicate())
	}
	return dup
}

func (op *PSOperand) or(stack *PSStack) error {
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
		err = stack.Push(MakeBool(topBool.Val || belowBool.Val))
		return err
	}
	if topInt, isTopInt := top.(*PSInteger); isTopInt {
		belowInt, isBelowInt := below.(*PSInteger)
		if !isBelowInt {
			return ErrTypeCheck
		}
		err = stack.Push(MakeInteger(topInt.Val | belowInt.Val))
		return err
	}
	return ErrTypeCheck
}

func (op *PSOperand) add(stack *PSStack) error {
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
	belowReal, isBelowReal := below.(*PSReal)
	belowInt, isBelowInt := below.(*PSInteger)
	if !isBelowReal && !isBelowInt {
		return ErrTypeCheck
	}
	if isTopInt && isBelowInt {
		sum := topInt.Val + belowInt.Val
		err := stack.Push(MakeInteger(sum))
		return err
	}
	var result float64
	if isTopReal {
		result = topReal.Val
	} else {
		result = float64(topInt.Val)
	}
	if isBelowReal {
		result += belowReal.Val
	} else {
		result += float64(belowInt.Val)
	}
	err = stack.Push(MakeReal(result))
	return err
}

// PSObjectArrayToFloat64Array converts []PSObject into a []float64 array. Each PSObject must represent a number,
// otherwise a ErrTypeCheck error occurs.
func PSObjectArrayToFloat64Array(objects []PSObject) ([]float64, error) {
	var floats []float64
	for _, obj := range objects {
		if objInt, isObjInt := obj.(*PSInteger); isObjInt {
			floats = append(floats, float64(objInt.Val))
		} else if objReal, isObjReal := obj.(*PSReal); isObjReal {
			floats = append(floats, objReal.Val)
		} else {
			return nil, ErrTypeCheck
		}
	}
	return floats, nil
}

var ErrUnsupportedOperand = errors.New("unsupported operand")

func (op *PSOperand) exch(stack *PSStack) error {
	top, err := stack.Pop()
	if err != nil {
		return err
	}
	below, err := stack.Pop()
	if err != nil {
		return err
	}
	err = stack.Push(top)
	if err != nil {
		return err
	}
	err = stack.Push(below)
	return err
}

func (prog *PSProgram) String() string {
	s := "{ "
	for _, obj := range *prog {
		s += obj.String()
		s += " "
	}
	s += "}"
	return s
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
	s := "[ "
	for _, obj := range *stack {
		s += obj.String()
		s += " "
	}
	s += "]"
	return s
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
