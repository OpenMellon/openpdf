// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package syncmap

import (
	"maps"
	"sync"
)

type StringsTuple struct{ Key, Value string }

type StringsMap struct {
	m  map[string]string
	mu sync.RWMutex
}

func NewStringsMap(tuples []StringsTuple) *StringsMap {
	m := map[string]string{}
	for _, t := range tuples {
		m[t.Key] = t.Value
	}
	return &StringsMap{m: m}
}

func (s *StringsMap) Read(k string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[k]
	return v, ok
}

func (s *StringsMap) Write(k string, v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
}

func (s *StringsMap) Range(f func(k string, v string) (stop bool)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for k, v := range s.m {
		if f(k, v) {
			break
		}
	}
}

func (s *StringsMap) Copy() *StringsMap {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := map[string]string{}
	maps.Copy(m, s.m)
	return &StringsMap{m: m}
}

type RuneByteMap struct {
	m  map[rune]byte
	mu sync.RWMutex
}

func MakeRuneByteMap(length int) *RuneByteMap {
	return &RuneByteMap{m: make(map[rune]byte, length)}
}

func (s *RuneByteMap) Length() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}

func (s *RuneByteMap) Read(k rune) (byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[k]
	return v, ok
}

func (s *RuneByteMap) Write(k rune, v byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
}

func (s *RuneByteMap) Range(f func(k rune, v byte) (stop bool)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for k, v := range s.m {
		if f(k, v) {
			break
		}
	}
}

type ByteRuneMap struct {
	m  map[byte]rune
	mu sync.RWMutex
}

func NewByteRuneMap(m map[byte]rune) *ByteRuneMap { return &ByteRuneMap{m: m} }

func MakeByteRuneMap(length int) *ByteRuneMap {
	return &ByteRuneMap{m: make(map[byte]rune, length)}
}

func (s *ByteRuneMap) Length() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}

func (s *ByteRuneMap) Read(k byte) (rune, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[k]
	return v, ok
}

func (s *ByteRuneMap) Write(k byte, v rune) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
}

func (s *ByteRuneMap) Range(f func(k byte, v rune) (stop bool)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for k, v := range s.m {
		if f(k, v) {
			break
		}
	}
}

type RuneRuneMap struct {
	m  map[rune]rune
	mu sync.RWMutex
}

func NewRuneRuneMap(m map[rune]rune) *RuneRuneMap { return &RuneRuneMap{m: m} }

func (s *RuneRuneMap) Length() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}

func (s *RuneRuneMap) Read(k rune) (rune, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[k]
	return v, ok
}

func (s *RuneRuneMap) Write(k rune, v rune) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
}

func (s *RuneRuneMap) Range(f func(k rune, v rune) (stop bool)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for k, v := range s.m {
		if f(k, v) {
			break
		}
	}
}

type RuneStringMap struct {
	m  map[rune]string
	mu sync.RWMutex
}

func NewRuneStringMap(m map[rune]string) *RuneStringMap { return &RuneStringMap{m: m} }

func (s *RuneStringMap) Length() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}

func (s *RuneStringMap) Read(k rune) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[k]
	return v, ok
}

func (s *RuneStringMap) Write(k rune, v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
}

func (s *RuneStringMap) Range(f func(k rune, v string) (stop bool)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for k, v := range s.m {
		if f(k, v) {
			break
		}
	}
}

type StringRuneMap struct {
	m  map[string]rune
	mu sync.RWMutex
}

func NewStringRuneMap(m map[string]rune) *StringRuneMap { return &StringRuneMap{m: m} }

func (s *StringRuneMap) Length() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}

func (s *StringRuneMap) Read(k string) (rune, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[k]
	return v, ok
}

func (s *StringRuneMap) Write(k string, v rune) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
}

func (s *StringRuneMap) Range(f func(k string, v rune) (stop bool)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for k, v := range s.m {
		if f(k, v) {
			break
		}
	}
}

type RuneUint16Map struct {
	m  map[rune]uint16
	mu sync.RWMutex
}

func MakeRuneUint16Map(length int) *RuneUint16Map {
	return &RuneUint16Map{m: make(map[rune]uint16, length)}
}

func (s *RuneUint16Map) Length() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}

func (s *RuneUint16Map) Read(k rune) (uint16, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[k]
	return v, ok
}

func (s *RuneUint16Map) Write(k rune, v uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
}

func (s *RuneUint16Map) Range(f func(k rune, v uint16) (stop bool)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for k, v := range s.m {
		if f(k, v) {
			break
		}
	}
}

func (s *RuneUint16Map) Delete(k rune) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, k)
}

func (s *RuneUint16Map) RangeDelete(f func(k rune, v uint16) (del bool, stop bool)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.m {
		del, stop := f(k, v)
		if del {
			delete(s.m, k)
		}
		if stop {
			break
		}
	}
}

type Uint16Set struct {
	m  map[uint16]struct{}
	mu sync.RWMutex
}

func MakeUint16Set(length int) *Uint16Set { return &Uint16Set{m: make(map[uint16]struct{}, length)} }

func (s *Uint16Set) Length() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}

func (s *Uint16Set) Write(k uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = struct{}{}
}

func (s *Uint16Set) Exists(k uint16) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.m[k]
	return ok
}

func (s *Uint16Set) Range(f func(k uint16) (stop bool)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for k := range s.m {
		if f(k) {
			break
		}
	}
}

type RuneSet struct {
	m  map[rune]struct{}
	mu sync.RWMutex
}

func MakeRuneSet(length int) *RuneSet { return &RuneSet{m: make(map[rune]struct{}, length)} }

func (s *RuneSet) Length() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}

func (s *RuneSet) Write(k rune) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = struct{}{}
}

func (s *RuneSet) Exists(k rune) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.m[k]
	return ok
}

func (s *RuneSet) Range(f func(k rune) (stop bool)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for k := range s.m {
		if f(k) {
			break
		}
	}
}
