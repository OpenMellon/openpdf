package regress

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/core"
)

// elided keys hold values that legitimately vary between runs, builds or Go releases. Length is the size of the
// compressed stream, which the standard library's flate encoder changes between releases; the decoded stream body
// is dumped instead.
var elided = map[core.PdfObjectName]bool{
	"ID": true, "CreationDate": true, "ModDate": true, "Producer": true, "Length": true,
}

// dumpPDF returns a normalised, human-readable listing of every object in the PDF: objects in number order,
// dictionary keys sorted, streams decoded (binary ones summarised by length and SHA-256).
func dumpPDF(data []byte) (string, error) {
	parser, err := core.NewParser(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%%PDF-%s\n", versionString(parser.PdfVersion()))
	b.WriteString("trailer ")
	writeObject(&b, parser.GetTrailer(), 0)
	b.WriteString("\n")
	for _, num := range parser.GetObjectNums() {
		obj, err := parser.LookupByNumber(num)
		if err != nil {
			return "", fmt.Errorf("object %d: %w", num, err)
		}
		fmt.Fprintf(&b, "\n%d 0 obj\n", num)
		if ind, ok := obj.(*core.PdfIndirectObject); ok {
			obj = ind.PdfObject
		}
		writeObject(&b, obj, 0)
		b.WriteString("\n")
	}
	return normalizeText(b.String()), nil
}

func versionString(v core.Version) string { return fmt.Sprintf("%d.%d", v.Major, v.Minor) }

func writeObject(b *strings.Builder, obj core.PdfObject, depth int) {
	pad := strings.Repeat("  ", depth+1)
	switch o := obj.(type) {
	case nil:
		b.WriteString("null")
	case *core.PdfIndirectObject:
		fmt.Fprintf(b, "%d %d R", o.ObjectNumber, o.GenerationNumber)
	case *core.PdfObjectReference:
		fmt.Fprintf(b, "%d %d R", o.ObjectNumber, o.GenerationNumber)
	case *core.PdfObjectDictionary:
		writeDict(b, o, depth)
	case *core.PdfObjectArray:
		b.WriteString("[")
		for i, e := range o.Elements() {
			if i > 0 {
				b.WriteString(" ")
			}
			writeObject(b, e, depth)
		}
		b.WriteString("]")
	case *core.PdfObjectStream:
		b.WriteString(pad)
		writeDict(b, o.PdfObjectDictionary, depth)
		b.WriteString("\n")
		b.WriteString(pad + "stream\n")
		writeStreamBody(b, o)
		b.WriteString(pad + "endstream")
	default:
		b.Write(o.Write())
	}
}

func writeDict(b *strings.Builder, d *core.PdfObjectDictionary, depth int) {
	keys := d.Keys()
	slices.Sort(keys)
	b.WriteString("<<")
	for _, k := range keys {
		if elided[k] {
			fmt.Fprintf(b, " /%s <elided>", k)
			continue
		}
		fmt.Fprintf(b, " /%s ", k)
		writeObject(b, d.Get(k), depth+1)
	}
	b.WriteString(" >>")
}

func writeStreamBody(b *strings.Builder, s *core.PdfObjectStream) {
	body, err := core.DecodeStream(s)
	if err != nil {
		body = s.Stream
		fmt.Fprintf(b, "  <undecoded: %v>\n", err)
	}
	if utf8.Valid(body) && !bytes.ContainsRune(body, 0) && len(body) < 64<<10 {
		for line := range strings.SplitSeq(strings.TrimRight(string(body), "\n"), "\n") {
			b.WriteString("  " + line + "\n")
		}
		return
	}
	fmt.Fprintf(b, "  <binary %d bytes sha256=%x>\n", len(body), sha256.Sum256(body))
}
