package core

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestReaderAtCursorSeekAfterPeek(t *testing.T) {
	source := bytes.NewReader([]byte("abcdef"))
	cursor := newReaderAtCursor(source, 6, 0)
	if _, err := cursor.Peek(4); err != nil {
		t.Fatal(err)
	}
	if pos, err := cursor.Seek(2, io.SeekCurrent); err != nil || pos != 2 {
		t.Fatalf("Seek current = %d, %v", pos, err)
	}
	if b, err := cursor.ReadByte(); err != nil || b != 'c' {
		t.Fatalf("ReadByte = %q, %v", b, err)
	}
	if pos, err := cursor.Seek(-1, io.SeekEnd); err != nil || pos != 5 {
		t.Fatalf("Seek end = %d, %v", pos, err)
	}
	if b, err := cursor.ReadByte(); err != nil || b != 'f' {
		t.Fatalf("last byte = %q, %v", b, err)
	}
	if _, err := cursor.Seek(-1, io.SeekStart); err == nil {
		t.Fatal("negative seek should fail")
	}
}

func TestParseStringEscapesAndNestedParentheses(t *testing.T) {
	input := []byte(`(A\050B\051\n(C))`)
	cursor := newReaderAtCursor(bytes.NewReader(input), int64(len(input)), 0)
	var parser PdfParser
	value, err := parser.parseString(cursor)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := value.Str(), "A(B)\n(C)"; got != want {
		t.Fatalf("parsed string = %q, want %q", got, want)
	}
}

func TestParseNameHexEscapeAndDelimiter(t *testing.T) {
	input := []byte("/Hello#20world/Next")
	cursor := newReaderAtCursor(bytes.NewReader(input), int64(len(input)), 0)
	var parser PdfParser
	name, err := parser.parseName(cursor)
	if err != nil || string(name) != "Hello world" {
		t.Fatalf("name = %q, %v", name, err)
	}
	next, err := cursor.ReadByte()
	if err != nil || next != '/' {
		t.Fatalf("delimiter = %q, %v", next, err)
	}
}

func TestParseHexStringWithWhitespace(t *testing.T) {
	input := []byte("<48 65 6c 6c 6f>")
	cursor := newReaderAtCursor(bytes.NewReader(input), int64(len(input)), 0)
	var parser PdfParser
	value, err := parser.parseHexString(cursor)
	if err != nil || value.Str() != "Hello" {
		t.Fatalf("hex string = %v, %v", value, err)
	}
}

func TestParseBooleanTokens(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  PdfObjectBool
	}{{"true ", true}, {"false ", false}} {
		cursor := newReaderAtCursor(bytes.NewReader([]byte(tc.input)), int64(len(tc.input)), 0)
		var parser PdfParser
		got, err := parser.parseBool(cursor)
		if err != nil || got != tc.want {
			t.Fatalf("parseBool(%q) = %v, %v", tc.input, got, err)
		}
		if b, err := cursor.ReadByte(); err != nil || b != ' ' {
			t.Fatalf("delimiter = %q, %v", b, err)
		}
	}
}

func TestSkipCommentsAndWhitespace(t *testing.T) {
	input := []byte("  % first\r\n% second\n  /Name")
	cursor := newReaderAtCursor(bytes.NewReader(input), int64(len(input)), 0)
	var parser PdfParser
	if err := parser.skipComments(cursor); err != nil {
		t.Fatal(err)
	}
	if value, err := cursor.ReadByte(); err != nil || value != '/' {
		t.Fatalf("next = %q, %v", value, err)
	}
}

func TestParseObjectDispatch(t *testing.T) {
	for _, tc := range []struct {
		input string
		check func(PdfObject) bool
	}{
		{"/Name ", func(o PdfObject) bool { v, ok := o.(*PdfObjectName); return ok && string(*v) == "Name" }},
		{"true ", func(o PdfObject) bool { v, ok := o.(*PdfObjectBool); return ok && bool(*v) }},
		{"12 0 R ", func(o PdfObject) bool {
			v, ok := o.(*PdfObjectReference)
			return ok && v.ObjectNumber == 12 && v.GenerationNumber == 0
		}},
		{"42 ", func(o PdfObject) bool { v, ok := o.(*PdfObjectInteger); return ok && int64(*v) == 42 }},
	} {
		cursor := newReaderAtCursor(bytes.NewReader([]byte(tc.input)), int64(len(tc.input)), 0)
		var parser PdfParser
		got, err := parser.parseObject(cursor, 0)
		if err != nil || !tc.check(got) {
			t.Errorf("parseObject(%q) = %v, %v", tc.input, got, err)
		}
	}
}

func TestParseNestedArray(t *testing.T) {
	input := []byte("[1 /Name [true false]] ")
	cursor := newReaderAtCursor(bytes.NewReader(input), int64(len(input)), 0)
	var parser PdfParser
	obj, err := parser.parseObject(cursor, 0)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := obj.(*PdfObjectArray)
	if !ok || array.Len() != 3 {
		t.Fatalf("array = %v", obj)
	}
	nested, ok := array.Elements()[2].(*PdfObjectArray)
	if !ok || nested.Len() != 2 {
		t.Fatalf("nested array = %v", array.Elements()[2])
	}
}

func TestParseDictionaryWithArray(t *testing.T) {
	input := []byte("<< /Count 2 /Values [1 2] /Enabled true >> ")
	cursor := newReaderAtCursor(bytes.NewReader(input), int64(len(input)), 0)
	var parser PdfParser
	obj, err := parser.parseObject(cursor, 0)
	if err != nil {
		t.Fatal(err)
	}
	dict, ok := obj.(*PdfObjectDictionary)
	if !ok {
		t.Fatalf("object = %T", obj)
	}
	count, ok := dict.Get("Count").(*PdfObjectInteger)
	if !ok || *count != 2 {
		t.Fatalf("Count = %v", dict.Get("Count"))
	}
	values, ok := dict.Get("Values").(*PdfObjectArray)
	if !ok || values.Len() != 2 {
		t.Fatalf("Values = %v", dict.Get("Values"))
	}
	enabled, ok := dict.Get("Enabled").(*PdfObjectBool)
	if !ok || !bool(*enabled) {
		t.Fatalf("Enabled = %v", dict.Get("Enabled"))
	}
}

func TestParseArrayNestingLimit(t *testing.T) {
	input := []byte("[]")
	cursor := newReaderAtCursor(bytes.NewReader(input), int64(len(input)), 0)
	var parser PdfParser
	if _, err := parser.parseArray(cursor, maxObjectNestingDepth); !errors.Is(err, errObjectNestingTooDeep) {
		t.Fatalf("nesting limit error = %v", err)
	}
}

func TestOffsetReaderAt(t *testing.T) {
	reader := newOffsetReaderAt(bytes.NewReader([]byte("abcdef")), 2)
	buf := make([]byte, 2)
	if n, err := reader.ReadAt(buf, 1); err != nil || n != 2 || string(buf) != "de" {
		t.Fatalf("ReadAt = %q, %d, %v", buf, n, err)
	}
	if _, err := reader.ReadAt(buf, -1); err == nil {
		t.Fatal("negative offset should fail")
	}
}

func TestParseReferenceText(t *testing.T) {
	ref, err := parseReferenceText("12 3 R")
	if err != nil || ref.ObjectNumber != 12 || ref.GenerationNumber != 3 {
		t.Fatalf("reference = %v, %v", ref, err)
	}
	if _, err := parseReferenceText("invalid"); err == nil {
		t.Fatal("invalid reference should fail")
	}
}
