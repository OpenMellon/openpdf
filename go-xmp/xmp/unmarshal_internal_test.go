// Copyright (c) 2017-2018 Alexander Eichhorn
//
// Licensed under the Apache License, Version 2.0 (the "License"): you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.

package xmp

import (
	"encoding/xml"
	"reflect"
	"strings"
	"testing"
)

// A parse failure on a value with no associated struct field (finfo == nil,
// e.g. array elements or a scalar DecodeElement target) must not panic on
// fieldInfo.String() and must honor strict/lenient mode.
func TestUnmarshalNilFieldInfoNoPanic(t *testing.T) {
	node := NewNode(xml.Name{Local: "x"})
	node.Value = "not-an-int"

	var n int
	d := NewDecoder(strings.NewReader(""))
	if err := d.unmarshal(reflect.ValueOf(&n), nil, node); err == nil {
		t.Fatal("strict: expected error for unparseable int, got nil")
	}

	d.SetStrict(false)
	if err := d.unmarshal(reflect.ValueOf(&n), nil, node); err != nil {
		t.Fatalf("lenient: unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("lenient: n = %d, want 0 (skipped)", n)
	}
}
