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

package xmp_test

import (
	"strings"
	"testing"

	"github.com/OpenMellon/openpdf/go-xmp/xmp"
)

// A self-contained model exercising the decode paths that abort the whole
// packet in strict mode: an unparseable leaf value (Date) and an unknown child
// element inside a struct property (Sub).
const rtNS = "http://ns.example.com/resilience/1.0/"

var nsRT = xmp.NewNamespace("rt", rtNS, func(string) xmp.Model { return &rtModel{} })

func init() { xmp.Register(nsRT) }

type rtSub struct {
	Known string `xmp:"rt:known"`
}

type rtModel struct {
	Text  string       `xmp:"rt:text"`
	Date  xmp.Date     `xmp:"rt:date"`
	Sub   rtSub        `xmp:"rt:sub"`
	Nums  []int        `xmp:"rt:num,attr"`
	Dates xmp.DateList `xmp:"rt:dates"`
}

func (m *rtModel) Can(ns string) bool              { return ns == nsRT.GetName() }
func (m *rtModel) Namespaces() xmp.NamespaceList   { return xmp.NamespaceList{nsRT} }
func (m *rtModel) SyncModel(*xmp.Document) error   { return nil }
func (m *rtModel) SyncFromXMP(*xmp.Document) error { return nil }
func (m *rtModel) SyncToXMP(*xmp.Document) error   { return nil }
func (m *rtModel) CanTag(string) bool              { return false }
func (m *rtModel) GetTag(string) (string, error)   { return "", nil }
func (m *rtModel) SetTag(string, string) error     { return nil }

func rtPacket(body string) string {
	return `<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:rt="` + rtNS + `">` + body +
		`</rdf:Description></rdf:RDF></x:xmpmeta>`
}

func decode(t *testing.T, body string, strict bool) (*rtModel, error) {
	t.Helper()
	doc := xmp.NewDocument()
	dec := xmp.NewDecoder(strings.NewReader(rtPacket(body)))
	dec.SetStrict(strict)
	if err := dec.Decode(doc); err != nil {
		return nil, err
	}
	m := doc.FindModel(nsRT)
	if m == nil {
		t.Fatal("decode: model was not loaded")
	}
	return m.(*rtModel), nil
}

func TestLenientSkipsUnparseableValue(t *testing.T) {
	m, err := decode(t, `<rt:text>hello</rt:text><rt:date>totally-not-a-date</rt:date>`, false)
	if err != nil {
		t.Fatalf("lenient decode: unexpected error: %v", err)
	}
	if m.Text != "hello" {
		t.Errorf("Text = %q, want %q", m.Text, "hello")
	}
	if !m.Date.IsZero() {
		t.Errorf("Date = %v, want zero (skipped)", m.Date.Value())
	}
}

func TestLenientSkipsUnknownChild(t *testing.T) {
	m, err := decode(t, `<rt:text>hello</rt:text>`+
		`<rt:sub rdf:parseType="Resource"><rt:known>ok</rt:known><rt:mystery>boom</rt:mystery></rt:sub>`, false)
	if err != nil {
		t.Fatalf("lenient decode: unexpected error: %v", err)
	}
	if m.Text != "hello" {
		t.Errorf("Text = %q, want %q", m.Text, "hello")
	}
	if m.Sub.Known != "ok" {
		t.Errorf("Sub.Known = %q, want %q", m.Sub.Known, "ok")
	}
}

func TestLenientSkipsBadSliceAttrElement(t *testing.T) {
	pkt := `<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:rt="` + rtNS + `" rt:num="notanumber">` +
		`<rt:text>hi</rt:text></rdf:Description></rdf:RDF></x:xmpmeta>`
	doc := xmp.NewDocument()
	dec := xmp.NewDecoder(strings.NewReader(pkt))
	dec.SetStrict(false)
	if err := dec.Decode(doc); err != nil {
		t.Fatalf("lenient decode: unexpected error: %v", err)
	}
	m := doc.FindModel(nsRT).(*rtModel)
	if m.Text != "hi" {
		t.Errorf("Text = %q, want %q", m.Text, "hi")
	}
	if len(m.Nums) != 0 {
		t.Errorf("Nums = %v, want empty (bad element skipped, not appended as zero)", m.Nums)
	}
}

func TestLenientSkipsBadArrayElement(t *testing.T) {
	body := `<rt:dates><rdf:Seq>` +
		`<rdf:li>2020-01-02T03:04:05Z</rdf:li>` +
		`<rdf:li>totally-not-a-date</rdf:li>` +
		`<rdf:li>2021-06-07T08:09:10Z</rdf:li>` +
		`</rdf:Seq></rt:dates>`
	m, err := decode(t, body, false)
	if err != nil {
		t.Fatalf("lenient decode: unexpected error: %v", err)
	}
	if len(m.Dates) != 2 {
		t.Fatalf("Dates len = %d, want 2 (bad element skipped, not stored as zero): %v", len(m.Dates), m.Dates)
	}
	for i, d := range m.Dates {
		if d.IsZero() {
			t.Errorf("Dates[%d] is zero-time; bad element was injected instead of skipped", i)
		}
	}
}

func TestStrictFailsOnBadArrayElement(t *testing.T) {
	body := `<rt:dates><rdf:Seq><rdf:li>2020-01-02T03:04:05Z</rdf:li>` +
		`<rdf:li>totally-not-a-date</rdf:li></rdf:Seq></rt:dates>`
	if _, err := decode(t, body, true); err == nil {
		t.Fatal("strict decode: expected error, got nil")
	}
}

func TestStrictFailsOnUnparseableValue(t *testing.T) {
	if _, err := decode(t, `<rt:text>hello</rt:text><rt:date>totally-not-a-date</rt:date>`, true); err == nil {
		t.Fatal("strict decode: expected error, got nil")
	}
}

func TestStrictFailsOnUnknownChild(t *testing.T) {
	body := `<rt:sub rdf:parseType="Resource"><rt:known>ok</rt:known><rt:mystery>boom</rt:mystery></rt:sub>`
	if _, err := decode(t, body, true); err == nil {
		t.Fatal("strict decode: expected error, got nil")
	}
}

// Default xmp.Unmarshal is strict; loose-TZ and PDF D: dates parse via the
// date-normalization layers, so they load without needing lenient mode.
func TestUnmarshalParsesLooseAndPDFDates(t *testing.T) {
	for _, tc := range []struct {
		name string
		val  string
	}{
		{"loose-tz", "2019-12-24T15:49:34.000+03"},
		{"pdf-date", "D:20200309102906+3'00'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := xmp.NewDocument()
			if err := xmp.Unmarshal([]byte(rtPacket(`<rt:text>hi</rt:text><rt:date>`+tc.val+`</rt:date>`)), doc); err != nil {
				t.Fatalf("Unmarshal: unexpected error: %v", err)
			}
			m := doc.FindModel(nsRT).(*rtModel)
			if m.Text != "hi" {
				t.Errorf("Text = %q, want %q", m.Text, "hi")
			}
			if m.Date.IsZero() {
				t.Errorf("Date = zero, want parsed value from %q", tc.val)
			}
		})
	}
}
