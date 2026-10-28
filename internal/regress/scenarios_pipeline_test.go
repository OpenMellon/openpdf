package regress

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/OpenMellon/openpdf/annotator"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/optimize"
	"github.com/OpenMellon/openpdf/pdfutil"
	"github.com/OpenMellon/openpdf/redactor"
	"github.com/OpenMellon/openpdf/sanitize"
)

func init() {
	scenarios = append(
		scenarios,
		scenario{name: "form-fields", build: buildFormFields},
		scenario{name: "form-filled", build: buildFormFilled},
		scenario{name: "form-flattened", build: buildFormFlattened},
		scenario{name: "pdfutil-merge", build: buildMerge, pixels: true},
		scenario{name: "pdfutil-extract-range", build: buildExtractRange, pixels: true},
		scenario{name: "pdfutil-grayscale", build: buildGrayscale, pixels: true},
		scenario{name: "optimizer", build: buildOptimized, pixels: true},
		scenario{name: "redactor", build: buildRedacted, pixels: true},
		scenario{name: "sanitize", build: buildSanitized, pixels: true},
		scenario{name: "encrypted-aes256", build: buildEncrypted, pixels: true, password: "user", noBytes: true},
	)
}

// twoPageDoc is a small embedded-font document used as input to the transforming scenarios.
func twoPageDoc(t *testing.T, label string) []byte {
	t.Helper()
	c := newCreator(t)
	for _, page := range []string{"one", "two"} {
		c.NewPage()
		p := c.NewStyledParagraph()
		p.SetText(label + " page " + page + ". Account SECRET-1234 belongs to Ada Lovelace.")
		draw(t, c, p)
		img, err := c.NewImageFromGoImage(testImage())
		if err != nil {
			t.Fatal(err)
		}
		img.SetPos(60, 200)
		img.ScaleToWidth(180)
		draw(t, c, img)
	}
	return write(t, c)
}

func readerOf(t *testing.T, data []byte) *model.PdfReader {
	t.Helper()
	r, err := model.NewPdfReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// writePages copies every page of data through a fresh writer configured by setup, and returns the written PDF.
func writePages(t *testing.T, data []byte, setup func(w *model.PdfWriter), each func(p *model.PdfPage)) []byte {
	t.Helper()
	r := readerOf(t, data)
	w := model.NewPdfWriter()
	if setup != nil {
		setup(&w)
	}
	n, err := r.GetNumPages()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		page, err := r.GetPage(i)
		if err != nil {
			t.Fatal(err)
		}
		if each != nil {
			each(page)
		}
		if err := w.AddPage(page); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := w.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// formDoc builds a one-page form whose text, checkbox and combobox widgets are on the page.
func formDoc(t *testing.T) []byte {
	t.Helper()
	page := model.NewPdfPage()
	page.MediaBox = &model.PdfRectangle{Llx: 0, Lly: 0, Urx: 612, Ury: 792}
	text, err := annotator.NewTextField(page, "name", []float64{50, 700, 300, 730}, annotator.TextFieldOptions{})
	if err != nil {
		t.Fatal(err)
	}
	check, err := annotator.NewCheckboxField(page, "agree", []float64{50, 650, 70, 670}, annotator.CheckboxFieldOptions{Checked: true})
	if err != nil {
		t.Fatal(err)
	}
	combo, err := annotator.NewComboboxField(page, "colour", []float64{50, 600, 200, 625}, annotator.ComboboxFieldOptions{Choices: []string{"Red", "Green", "Blue"}})
	if err != nil {
		t.Fatal(err)
	}
	fields := []*model.PdfField{text.PdfField, check.PdfField, combo.PdfField}
	for _, f := range fields {
		for _, wa := range f.Annotations {
			page.AddAnnotation(wa.PdfAnnotation)
		}
	}
	form := model.NewPdfAcroForm()
	form.Fields = &fields
	w := model.NewPdfWriter()
	if err := w.AddPage(page); err != nil {
		t.Fatal(err)
	}
	if err := w.SetForms(form); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := w.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func buildFormFields(t *testing.T) []byte { return formDoc(t) }

type fieldValues map[string]core.PdfObject

func (v fieldValues) FieldValues() (map[string]core.PdfObject, error) { return v, nil }

func fillForm(t *testing.T, data []byte) *model.PdfReader {
	t.Helper()
	r := readerOf(t, data)
	vals := fieldValues{"name": core.MakeString("Ada Lovelace"), "colour": core.MakeString("Green")}
	if err := r.AcroForm.FillWithAppearance(vals, annotator.FieldAppearance{OnlyIfMissing: true}); err != nil {
		t.Fatal(err)
	}
	return r
}

func writeReader(t *testing.T, r *model.PdfReader) []byte {
	t.Helper()
	w := model.NewPdfWriter()
	n, err := r.GetNumPages()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		page, err := r.GetPage(i)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.AddPage(page); err != nil {
			t.Fatal(err)
		}
	}
	if r.AcroForm != nil {
		if err := w.SetForms(r.AcroForm); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := w.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func buildFormFilled(t *testing.T) []byte {
	return writeReader(t, fillForm(t, formDoc(t)))
}

func buildFormFlattened(t *testing.T) []byte {
	r := fillForm(t, formDoc(t))
	if err := r.FlattenFieldsWithOpts(annotator.FieldAppearance{OnlyIfMissing: true}, nil); err != nil {
		t.Fatal(err)
	}
	return writeReader(t, r)
}

func buildMerge(t *testing.T) []byte {
	var out bytes.Buffer
	inputs := []io.ReaderAt{bytes.NewReader(twoPageDoc(t, "First")), bytes.NewReader(twoPageDoc(t, "Second"))}
	if err := pdfutil.MergePdfStreams(inputs, &out, false); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func buildExtractRange(t *testing.T) []byte {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.pdf"), filepath.Join(dir, "out.pdf")
	writeFile(t, in, twoPageDoc(t, "Source"))
	if err := pdfutil.ExtractPageRange(in, out, 2, 2, false); err != nil {
		t.Fatal(err)
	}
	return readFile(t, out)
}

func buildGrayscale(t *testing.T) []byte {
	return writePages(t, twoPageDoc(t, "Grey"), nil, func(p *model.PdfPage) {
		if err := pdfutil.ConvertPageToGrayscale(p); err != nil {
			t.Fatal(err)
		}
	})
}

func buildOptimized(t *testing.T) []byte {
	return writePages(t, twoPageDoc(t, "Optimized"), func(w *model.PdfWriter) {
		w.SetOptimizer(optimize.New(optimize.Options{
			CombineDuplicateStreams:         true,
			CombineIdenticalIndirectObjects: true,
			CompressStreams:                 true,
			UseObjectStreams:                true,
			CleanUnusedResources:            true,
			ImageUpperPPI:                   150,
			ImageQuality:                    80,
		}))
	}, nil)
}

func buildRedacted(t *testing.T) []byte {
	r := readerOf(t, twoPageDoc(t, "Redacted"))
	opts := &redactor.RedactionOptions{Terms: []redactor.RedactionTerm{{Pattern: regexp.MustCompile(`SECRET-\d+`)}}}
	red := redactor.New(r, opts, redactor.RedactRectanglePropsNew())
	if err := red.Redact(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := red.Write(&buf); err != nil {
		t.Fatal(err)
	}
	if text, _ := readPages(t, buf.Bytes(), ""); strings.Contains(text, "SECRET") {
		t.Fatal("redacted text is still extractable")
	}
	return buf.Bytes()
}

func buildSanitized(t *testing.T) []byte {
	r := readerOf(t, twoPageDoc(t, "Sanitized"))
	page, err := r.GetPage(1)
	if err != nil {
		t.Fatal(err)
	}
	link := model.NewPdfAnnotationLink()
	link.Rect = core.MakeArrayFromFloats([]float64{60, 700, 200, 720})
	uri := model.NewPdfActionURI()
	uri.URI = core.MakeString("https://example.com/tracking")
	link.SetContext(uri)
	link.A = uri.ToPdfObject()
	page.AddAnnotation(link.PdfAnnotation)
	linked := writeReader(t, r)
	if !bytes.Contains(linked, []byte("example.com/tracking")) {
		t.Fatal("input lost its URI action, so the scenario would not exercise the sanitizer")
	}
	out := writePages(t, linked, func(w *model.PdfWriter) {
		w.SetOptimizer(sanitize.New(sanitize.SanitizationOpts{URI: true, JavaScript: true}))
	}, nil)
	if bytes.Contains(out, []byte("example.com/tracking")) {
		t.Fatal("sanitizer left the URI action in the output")
	}
	return out
}

func buildEncrypted(t *testing.T) []byte {
	out := writePages(t, twoPageDoc(t, "Secret"), func(w *model.PdfWriter) {
		if err := w.Encrypt([]byte("user"), []byte("owner"), &model.EncryptOptions{Algorithm: model.AES_256bit}); err != nil {
			t.Fatal(err)
		}
	}, nil)
	r := readerOf(t, out)
	if enc, err := r.IsEncrypted(); err != nil || !enc {
		t.Fatalf("output is not encrypted: enc=%v err=%v", enc, err)
	}
	if ok, _ := r.Decrypt([]byte("wrong")); ok {
		t.Fatal("wrong password opened the file")
	}
	if m := r.GetEncryptionMethod(); !strings.Contains(m, "AESV3") {
		t.Fatalf("encryption method %q, want AESV3 (AES-256)", m)
	}
	return out
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
