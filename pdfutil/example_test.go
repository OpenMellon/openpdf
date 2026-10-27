package pdfutil_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/OpenMellon/openpdf/creator"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/pdfutil"
)

func writeSample(path string, pages int) {
	c := creator.New()
	for i := 1; i <= pages; i++ {
		c.NewPage()
		p := c.NewStyledParagraph()
		p.SetText(fmt.Sprintf("Page %d", i))
		if err := c.Draw(p); err != nil {
			log.Fatal(err)
		}
	}
	if err := c.WriteToFile(path); err != nil {
		log.Fatal(err)
	}
}

func pageCount(path string) int {
	r, f, err := model.NewPdfReaderFromFile(path, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	n, err := r.GetNumPages()
	if err != nil {
		log.Fatal(err)
	}
	return n
}

// Merge several PDFs into one. The last argument controls whether form
// fields are merged too.
func ExampleMergePdf() {
	dir, _ := os.MkdirTemp("", "openpdf")
	defer os.RemoveAll(dir)
	a, b, out := filepath.Join(dir, "a.pdf"), filepath.Join(dir, "b.pdf"), filepath.Join(dir, "out.pdf")
	writeSample(a, 2)
	writeSample(b, 3)

	if err := pdfutil.MergePdf([]string{a, b}, out, false); err != nil {
		log.Fatal(err)
	}
	fmt.Println(pageCount(out))
	// Output: 5
}

// Keep only a range of pages (1-based, inclusive).
func ExampleExtractPageRange() {
	dir, _ := os.MkdirTemp("", "openpdf")
	defer os.RemoveAll(dir)
	in, out := filepath.Join(dir, "in.pdf"), filepath.Join(dir, "out.pdf")
	writeSample(in, 5)

	if err := pdfutil.ExtractPageRange(in, out, 2, 4, false); err != nil {
		log.Fatal(err)
	}
	fmt.Println(pageCount(out))
	// Output: 3
}
