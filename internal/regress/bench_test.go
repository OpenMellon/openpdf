package regress

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/OpenMellon/openpdf/extractor"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/render"
)

// benchDocs are golden PDFs chosen to cover text, tables, images, outlines, merging and the optimizer. They are read
// from the committed goldens, so the benchmarks measure the same documents the regression tests check.
var benchDocs = []string{
	"truetype-text", "table", "images", "chapters-toc-outlines", "pdfutil-merge", "optimizer", "text-styles",
}

func forEachBenchDoc(b *testing.B, fn func(b *testing.B, data []byte)) {
	for _, name := range benchDocs {
		data, err := os.ReadFile(filepath.Join("testdata", name, "doc.pdf"))
		if err != nil {
			b.Fatal(err)
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			fn(b, data)
		})
	}
}

func openAll(b *testing.B, data []byte) (*model.PdfReader, []*model.PdfPage) {
	r, err := model.NewPdfReader(bytes.NewReader(data))
	if err != nil {
		b.Fatal(err)
	}
	n, err := r.GetNumPages()
	if err != nil {
		b.Fatal(err)
	}
	pages := make([]*model.PdfPage, 0, n)
	for i := 1; i <= n; i++ {
		p, err := r.GetPage(i)
		if err != nil {
			b.Fatal(err)
		}
		pages = append(pages, p)
	}
	return r, pages
}

// BenchmarkParse measures opening a document and loading every page.
func BenchmarkParse(b *testing.B) {
	forEachBenchDoc(b, func(b *testing.B, data []byte) {
		for b.Loop() {
			openAll(b, data)
		}
	})
}

// BenchmarkExtract measures text extraction from every page of an already open document.
func BenchmarkExtract(b *testing.B) {
	forEachBenchDoc(b, func(b *testing.B, data []byte) {
		_, pages := openAll(b, data)
		for b.Loop() {
			for _, p := range pages {
				ex, err := extractor.New(p)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := ex.ExtractText(); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

// BenchmarkRender measures rasterising every page at the device's default scale, with a fresh device each time so
// font and glyph caches are filled inside the measurement.
func BenchmarkRender(b *testing.B) {
	forEachBenchDoc(b, func(b *testing.B, data []byte) {
		_, pages := openAll(b, data)
		for b.Loop() {
			dev := render.NewImageDevice()
			for _, p := range pages {
				if _, err := dev.Render(p); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

// BenchmarkWrite measures opening a document and writing its pages through a new writer. Opening is included because
// AddPage mutates the pages it is given, so they cannot be reused across iterations.
func BenchmarkWrite(b *testing.B) {
	forEachBenchDoc(b, func(b *testing.B, data []byte) {
		for b.Loop() {
			_, pages := openAll(b, data)
			w := model.NewPdfWriter()
			for _, p := range pages {
				if err := w.AddPage(p); err != nil {
					b.Fatal(err)
				}
			}
			if err := w.Write(&bytes.Buffer{}); err != nil {
				b.Fatal(err)
			}
		}
	})
}
