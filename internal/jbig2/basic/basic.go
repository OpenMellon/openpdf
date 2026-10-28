// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package basic

import "github.com/OpenMellon/openpdf/internal/jbig2/errors"

func (m IntsMap) Get(key uint64) (int, bool) {
	values, ok := m[key]
	if !ok {
		return 0, false
	}
	if len(values) == 0 {
		return 0, false
	}
	return values[0], true
}
func (m IntsMap) Delete(key uint64) { delete(m, key) }
func (s NumSlice) Get(i int) (float32, error) {
	if i < 0 || i > len(s)-1 {
		return 0, errors.Errorf("NumSlice.Get", "index: '%d' out of range", i)
	}
	return s[i], nil
}

func Min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

type IntSlice []int

func (s IntSlice) Get(index int) (int, error) {
	if index > len(s)-1 {
		return 0, errors.Errorf("IntSlice.Get", "index: %d out of range", index)
	}
	return s[index], nil
}

func (s *IntSlice) Copy() *IntSlice {
	dup := IntSlice(make([]int, len(*s)))
	copy(dup, *s)
	return &dup
}
func (m IntsMap) Add(key uint64, value int) { m[key] = append(m[key], value) }
func (s *Stack) peek() (any, bool) {
	top := s.top()
	if top == -1 {
		return nil, false
	}
	return s.Data[top], true
}

func Max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func Ceil(numerator, denominator int) int {
	if numerator%denominator == 0 {
		return numerator / denominator
	}
	return (numerator / denominator) + 1
}

func (s *Stack) Pop() (value any, ok bool) {
	value, ok = s.peek()
	if !ok {
		return nil, ok
	}
	s.Data = s.Data[:s.top()]
	return value, true
}
func (s *Stack) Len() int { return len(s.Data) }
func Abs(v int) int {
	if v > 0 {
		return v
	}
	return -v
}
func NewIntSlice(i int) *IntSlice { s := IntSlice(make([]int, i)); return &s }

type Stack struct {
	Data []any
	Aux  *Stack
}

func (m IntsMap) GetSlice(key uint64) ([]int, bool) {
	values, ok := m[key]
	if !ok {
		return nil, false
	}
	return values, true
}

type NumSlice []float32

func (s *NumSlice) AddInt(v int) { *s = append(*s, float32(v)) }
func (s IntSlice) Size() int     { return len(s) }

type IntsMap map[uint64][]int

func Sign(v float32) float32 {
	if v >= 0.0 {
		return 1.0
	}
	return -1.0
}
func (s *NumSlice) Add(v float32)           { *s = append(*s, v) }
func (s *Stack) Peek() (value any, ok bool) { return s.peek() }
func (s *Stack) Push(v any)                 { s.Data = append(s.Data, v) }
func (s *IntSlice) Add(v int) error {
	if s == nil {
		return errors.Error("IntSlice.Add", "slice not defined")
	}
	*s = append(*s, v)
	return nil
}
func (s *Stack) top() int { return len(s.Data) - 1 }
func (s NumSlice) GetInt(i int) (int, error) {
	const funcName = "GetInt"
	if i < 0 || i > len(s)-1 {
		return 0, errors.Errorf(funcName, "index: '%d' out of range", i)
	}
	v := s[i]
	return int(v + Sign(v)*0.5), nil
}
func NewNumSlice(i int) *NumSlice { s := NumSlice(make([]float32, i)); return &s }
func (s NumSlice) GetIntSlice() []int {
	ints := make([]int, len(s))
	for i, v := range s {
		ints[i] = int(v)
	}
	return ints
}
