// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package ps implements various functionalities needed for handling Postscript for PDF uses, in particular
// for PDF function type 4.
//
// Package ps implements various functionalities needed for handling Postscript for PDF uses, in particular
// for PDF function type 4.
package ps

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/OpenMellon/openpdf/common"
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
	var s strings.Builder
	s.WriteString("{ ")
	for _, obj := range *prog {
		s.WriteString(obj.DebugString())
		s.WriteString(" ")
	}
	s.WriteString("}")
	return s.String()
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
	var s strings.Builder
	s.WriteString("[ ")
	for _, obj := range *stack {
		s.WriteString(obj.DebugString())
		s.WriteString(" ")
	}
	s.WriteString("]")
	return s.String()
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
