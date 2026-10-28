package regress

import (
	"bytes"
	"compress/zlib"
	"encoding/ascii85"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OpenMellon/openpdf/model"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// genInputs rewrites the hand-authored PDFs under testdata/inputs. These files are the parser's foreign input: they
// are committed, never produced by OpenPDF's own writer except for the encrypted ones (so those prove decryption of
// earlier output, not of another implementation's), and only regenerated when a fixture is deliberately changed.
var genInputs = flag.Bool("gen-inputs", false, "regenerate the hand-authored PDFs under testdata/inputs")

// pdfBuilder lays out a PDF object by object and records offsets so xref data can be written exactly.
type pdfBuilder struct {
	buf     bytes.Buffer
	offsets map[int]int
}

func newPDFBuilder() *pdfBuilder {
	b := &pdfBuilder{offsets: map[int]int{}}
	b.buf.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	return b
}

func (b *pdfBuilder) obj(n int, body string) {
	b.offsets[n] = b.buf.Len()
	fmt.Fprintf(&b.buf, "%d 0 obj\n%s\nendobj\n", n, body)
}

func (b *pdfBuilder) stream(n int, dict string, data []byte) {
	b.offsets[n] = b.buf.Len()
	fmt.Fprintf(&b.buf, "%d 0 obj\n<< %s /Length %d >>\nstream\n", n, dict, len(data))
	b.buf.Write(data)
	b.buf.WriteString("\nendstream\nendobj\n")
}

// xref writes a classic cross-reference table and trailer for objects 1..size-1 and returns its offset.
func (b *pdfBuilder) xref(size int, extraTrailer string, startxrefOverride int) {
	start := b.buf.Len()
	fmt.Fprintf(&b.buf, "xref\n0 %d\n0000000000 65535 f \n", size)
	for n := 1; n < size; n++ {
		fmt.Fprintf(&b.buf, "%010d 00000 n \n", b.offsets[n])
	}
	fmt.Fprintf(&b.buf, "trailer\n<< /Size %d /Root 1 0 R %s >>\nstartxref\n", size, extraTrailer)
	if startxrefOverride >= 0 {
		start = startxrefOverride
	}
	fmt.Fprintf(&b.buf, "%d\n%%%%EOF\n", start)
}

func zlibBytes(data []byte) []byte {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	zw.Write(data)
	zw.Close()
	return buf.Bytes()
}

const helvetica = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"

// simpleDoc writes catalog(1), pages(2), page(3), font(4) and a content stream(5) with the given text.
func simpleDoc(b *pdfBuilder, text string) {
	b.obj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	b.obj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	b.obj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Contents 5 0 R /Resources << /Font << /F1 4 0 R >> >> >>")
	b.obj(4, helvetica)
	b.stream(5, "", []byte("BT /F1 18 Tf 20 100 Td ("+text+") Tj ET"))
}

func inputXrefTable() []byte {
	b := newPDFBuilder()
	simpleDoc(b, "Classic xref table")
	b.xref(6, "", -1)
	return b.buf.Bytes()
}

func inputIncremental() []byte {
	b := newPDFBuilder()
	simpleDoc(b, "Original text")
	firstXref := b.buf.Len()
	b.xref(6, "", -1)
	b.stream(5, "", []byte("BT /F1 18 Tf 20 100 Td (Updated text) Tj ET"))
	start := b.buf.Len()
	fmt.Fprintf(&b.buf, "xref\n0 1\n0000000000 65535 f \n5 1\n%010d 00000 n \ntrailer\n<< /Size 6 /Root 1 0 R /Prev %d >>\nstartxref\n%d\n%%%%EOF\n",
		b.offsets[5], firstXref, start)
	return b.buf.Bytes()
}

func inputDamagedXref() []byte {
	b := newPDFBuilder()
	simpleDoc(b, "Recovered after xref damage")
	for n := range b.offsets {
		b.offsets[n] += 7 // every entry points into the wrong place
	}
	b.xref(6, "", 99999)
	return b.buf.Bytes()
}

func inputXrefStream() []byte {
	b := newPDFBuilder()
	// Objects 1-4 live in an object stream (6); the content stream (5) and the xref stream (7) are plain.
	bodies := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Contents 5 0 R /Resources << /Font << /F1 4 0 R >> >> >>",
		helvetica,
	}
	var index, content strings.Builder
	for i, body := range bodies {
		fmt.Fprintf(&index, "%d %d ", i+1, content.Len())
		content.WriteString(body + "\n")
	}
	first := index.Len()
	b.stream(5, "", []byte("BT /F1 18 Tf 20 100 Td (Xref stream and object stream) Tj ET"))
	b.stream(6, fmt.Sprintf("/Type /ObjStm /N 4 /First %d /Filter /FlateDecode", first), zlibBytes([]byte(index.String()+content.String())))
	xrefOffset := b.buf.Len()
	var rows bytes.Buffer
	row := func(t byte, a uint32, c byte) {
		rows.WriteByte(t)
		binary.Write(&rows, binary.BigEndian, a)
		rows.WriteByte(c)
	}
	row(0, 0, 255)
	for i := 0; i < 4; i++ {
		row(2, 6, byte(i))
	}
	row(1, uint32(b.offsets[5]), 0)
	row(1, uint32(b.offsets[6]), 0)
	row(1, uint32(xrefOffset), 0)
	b.stream(7, "/Type /XRef /Size 8 /W [1 4 1] /Root 1 0 R /Filter /FlateDecode", zlibBytes(rows.Bytes()))
	fmt.Fprintf(&b.buf, "startxref\n%d\n%%%%EOF\n", xrefOffset)
	return b.buf.Bytes()
}

// graphicsDoc writes a one-page document with no text, so it renders identically on every host.
func graphicsDoc(b *pdfBuilder, pageExtra, resources string, contents ...[]byte) {
	b.obj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	b.obj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	refs := make([]string, len(contents))
	for i := range contents {
		refs[i] = fmt.Sprintf("%d 0 R", 4+i)
	}
	b.obj(3, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] %s /Contents [%s] /Resources << %s >> >>",
		pageExtra, strings.Join(refs, " "), resources))
}

func inputInlineImage() []byte {
	b := newPDFBuilder()
	pixels := "FF0000 00FF00 0000FF FFFF00 00FFFF FF00FF 000000 FFFFFF 808080 FF8000 0080FF 80FF00 800000 008000 000080 C0C0C0"
	content := "0.9 g 0 0 300 200 re f\nq 120 0 0 120 90 40 cm\nBI /W 4 /H 4 /CS /RGB /BPC 8 /F /AHx\nID\n" +
		strings.ReplaceAll(pixels, " ", "") + ">\nEI\nQ\n"
	graphicsDoc(b, "", "", []byte(content))
	b.stream(4, "", []byte(content))
	b.xref(5, "", -1)
	return b.buf.Bytes()
}

func inputFilters() []byte {
	b := newPDFBuilder()
	draw := []byte("1 0 0 rg 20 20 100 60 re f\n0 0 1 RG 4 w 150 40 m 280 160 l S\n")
	var a85 bytes.Buffer
	enc := ascii85.NewEncoder(&a85)
	enc.Write(zlibBytes(draw))
	enc.Close()
	a85.WriteString("~>")
	// Run-length: a literal run (length byte n-1 then n bytes) and an end-of-data marker.
	rl2 := []byte("0 0.6 0 rg 160 120 120 60 re f")
	var runlength bytes.Buffer
	runlength.WriteByte(byte(len(rl2) - 1))
	runlength.Write(rl2)
	runlength.WriteByte(128)
	hexData := hex.EncodeToString([]byte("0.5 g 20 120 100 60 re f"))
	b.obj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	b.obj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	b.obj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Contents [4 0 R 5 0 R 6 0 R] >>")
	b.stream(4, "/Filter [/ASCII85Decode /FlateDecode]", a85.Bytes())
	b.stream(5, "/Filter /RunLengthDecode", runlength.Bytes())
	b.stream(6, "/Filter /ASCIIHexDecode", []byte(hexData+">"))
	b.xref(7, "", -1)
	return b.buf.Bytes()
}

func inputPageTree() []byte {
	b := newPDFBuilder()
	b.obj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	// MediaBox and Rotate are inherited from the root; the inner node overrides Rotate.
	b.obj(2, "<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 3 /MediaBox [0 0 300 200] /Rotate 90 >>")
	b.obj(3, "<< /Type /Page /Parent 2 0 R /Contents 7 0 R >>")
	b.obj(4, "<< /Type /Pages /Parent 2 0 R /Kids [5 0 R 6 0 R] /Count 2 /Rotate 0 >>")
	b.obj(5, "<< /Type /Page /Parent 4 0 R /Contents 7 0 R >>")
	b.obj(6, "<< /Type /Page /Parent 4 0 R /MediaBox [0 0 200 300] /Contents 7 0 R >>")
	b.stream(7, "", []byte("1 0 0 rg 20 20 120 60 re f 0 0 1 rg 20 100 60 60 re f"))
	b.xref(8, "", -1)
	return b.buf.Bytes()
}

func inputAcroForm() []byte {
	b := newPDFBuilder()
	b.obj(1, "<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] /DR << /Font << /Helv 4 0 R >> >> /DA (/Helv 12 Tf 0 g) >> >>")
	b.obj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	b.obj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Annots [5 0 R] >>")
	b.obj(4, helvetica)
	b.obj(5, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (email) /V (ada@example.com) /Rect [20 100 220 124] /F 4 /DA (/Helv 12 Tf 0 g) /AP << /N 6 0 R >> >>")
	b.stream(6, "/Type /XObject /Subtype /Form /BBox [0 0 200 24] /Resources << /Font << /Helv 4 0 R >> >>", []byte("/Tx BMC BT /Helv 12 Tf 0 g 2 7 Td (ada@example.com) Tj ET EMC"))
	b.xref(7, "", -1)
	return b.buf.Bytes()
}

// inputCIDIdentityH embeds Go Regular as a Type 0 font with Identity-H encoding, so text is written as glyph IDs and
// only the ToUnicode map says what it spells.
func inputCIDIdentityH(t *testing.T) []byte {
	f, err := sfnt.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	var sb sfnt.Buffer
	text := "CID font, Identity-H"
	var hexText, bfchars, widths strings.Builder
	seen := map[sfnt.GlyphIndex]bool{}
	for _, r := range text {
		g, err := f.GlyphIndex(&sb, r)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&hexText, "%04X", g)
		if seen[g] {
			continue
		}
		seen[g] = true
		adv, err := f.GlyphAdvance(&sb, g, fixed.I(1000), 0)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&bfchars, "<%04X> <%04X>\n", g, r)
		fmt.Fprintf(&widths, "%d [%d] ", g, adv.Round())
	}
	cmap := "/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n/CMapName /Adobe-Identity-UCS def /CMapType 2 def\n" +
		"1 begincodespacerange <0000> <FFFF> endcodespacerange\n" + fmt.Sprintf("%d beginbfchar\n%sendbfchar\n", len(seen), bfchars.String()) +
		"endcmap CMapName currentdict /CMap defineresource pop end end"
	b := newPDFBuilder()
	b.obj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	b.obj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	b.obj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>")
	b.stream(4, "", []byte("BT /F1 20 Tf 20 100 Td <"+hexText.String()+"> Tj ET"))
	b.obj(5, "<< /Type /Font /Subtype /Type0 /BaseFont /GoRegular /Encoding /Identity-H /DescendantFonts [6 0 R] /ToUnicode 9 0 R >>")
	b.obj(6, fmt.Sprintf("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /GoRegular /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 7 0 R /CIDToGIDMap /Identity /DW 500 /W [%s] >>", widths.String()))
	b.obj(7, "<< /Type /FontDescriptor /FontName /GoRegular /Flags 4 /FontBBox [-215 -265 1055 1119] /ItalicAngle 0 /Ascent 771 /Descent -193 /CapHeight 723 /StemV 70 /FontFile2 8 0 R >>")
	b.stream(8, fmt.Sprintf("/Length1 %d /Filter /FlateDecode", len(goregular.TTF)), zlibBytes(goregular.TTF))
	b.stream(9, "", []byte(cmap))
	b.xref(10, "", -1)
	return b.buf.Bytes()
}

// inputEncrypted writes a small embedded-font document through OpenPDF's own writer with the given cipher. The
// committed file is the fixture: reading it exercises decryption of data written elsewhere and earlier.
func inputEncrypted(t *testing.T, alg model.EncryptionAlgorithm) []byte {
	return writePages(t, twoPageDoc(t, "Locked"), func(w *model.PdfWriter) {
		if err := w.Encrypt([]byte("user"), []byte("owner"), &model.EncryptOptions{Algorithm: alg}); err != nil {
			t.Fatal(err)
		}
	}, nil)
}

func TestGenerateInputs(t *testing.T) {
	if !*genInputs {
		t.Skip("run with -gen-inputs to rewrite testdata/inputs")
	}
	files := map[string][]byte{
		"xref-table.pdf":         inputXrefTable(),
		"incremental-update.pdf": inputIncremental(),
		"damaged-xref.pdf":       inputDamagedXref(),
		"xref-stream.pdf":        inputXrefStream(),
		"inline-image.pdf":       inputInlineImage(),
		"stream-filters.pdf":     inputFilters(),
		"page-tree.pdf":          inputPageTree(),
		"acroform.pdf":           inputAcroForm(),
		"cid-identity-h.pdf":     inputCIDIdentityH(t),
		"rc4-128.pdf":            inputEncrypted(t, model.RC4_128bit),
		"aes-128.pdf":            inputEncrypted(t, model.AES_128bit),
		"aes-256.pdf":            inputEncrypted(t, model.AES_256bit),
	}
	dir := filepath.Join("testdata", "inputs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		path := filepath.Join(dir, name)
		// Encrypted files use random salts, so an existing one is kept; delete it to regenerate it deliberately.
		encrypted := name == "rc4-128.pdf" || strings.HasPrefix(name, "aes-")
		if _, err := os.Stat(path); err == nil && encrypted {
			continue
		}
		writeFile(t, path, data)
	}
}
