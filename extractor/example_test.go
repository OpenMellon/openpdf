package extractor_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/OpenMellon/openpdf/creator"
	"github.com/OpenMellon/openpdf/extractor"
	"github.com/OpenMellon/openpdf/model"
)

// Print the text of every page.
func ExampleExtractor_ExtractText() {
	dir, _ := os.MkdirTemp("", "openpdf")
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "in.pdf")

	c := creator.New()
	for _, s := range []string{"Hello", "World"} {
		c.NewPage()
		p := c.NewStyledParagraph()
		p.SetText(s)
		if err := c.Draw(p); err != nil {
			log.Fatal(err)
		}
	}
	if err := c.WriteToFile(path); err != nil {
		log.Fatal(err)
	}

	reader, f, err := model.NewPdfReaderFromFile(path, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	n, err := reader.GetNumPages()
	if err != nil {
		log.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		page, err := reader.GetPage(i)
		if err != nil {
			log.Fatal(err)
		}
		ex, err := extractor.New(page)
		if err != nil {
			log.Fatal(err)
		}
		text, err := ex.ExtractText()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("page %d: %s\n", i, strings.TrimSpace(text))
	}
	// Output:
	// page 1: Hello
	// page 2: World
}
