package redactor_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/OpenMellon/openpdf/creator"
	"github.com/OpenMellon/openpdf/extractor"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/redactor"
)

// Remove matching text from a PDF and draw a box over it.
func Example() {
	dir, _ := os.MkdirTemp("", "openpdf")
	defer os.RemoveAll(dir)
	in, out := filepath.Join(dir, "in.pdf"), filepath.Join(dir, "out.pdf")

	c := creator.New()
	c.NewPage()
	p := c.NewStyledParagraph()
	p.SetText("Account SECRET-1234 is active")
	if err := c.Draw(p); err != nil {
		log.Fatal(err)
	}
	if err := c.WriteToFile(in); err != nil {
		log.Fatal(err)
	}

	reader, f, err := model.NewPdfReaderFromFile(in, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	opts := &redactor.RedactionOptions{
		Terms: []redactor.RedactionTerm{{Pattern: regexp.MustCompile(`SECRET-\d+`)}},
	}
	r := redactor.New(reader, opts, redactor.RedactRectanglePropsNew())
	if err := r.Redact(); err != nil {
		log.Fatal(err)
	}
	if err := r.WriteToFile(out); err != nil {
		log.Fatal(err)
	}

	red, rf, err := model.NewPdfReaderFromFile(out, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer rf.Close()
	page, _ := red.GetPage(1)
	ex, _ := extractor.New(page)
	text, _ := ex.ExtractText()
	fmt.Println(strings.Contains(text, "SECRET"))
	// Output: false
}
