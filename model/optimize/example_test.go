package optimize_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/OpenMellon/openpdf/creator"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/optimize"
)

// Shrink a PDF by deduplicating objects, compressing streams and
// downsampling images.
func Example() {
	dir, _ := os.MkdirTemp("", "openpdf")
	defer os.RemoveAll(dir)
	in, out := filepath.Join(dir, "in.pdf"), filepath.Join(dir, "out.pdf")

	c := creator.New()
	c.NewPage()
	if err := c.WriteToFile(in); err != nil {
		log.Fatal(err)
	}

	reader, f, err := model.NewPdfReaderFromFile(in, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	w := model.NewPdfWriter()
	w.SetOptimizer(optimize.New(optimize.Options{
		CombineDuplicateStreams:         true,
		CombineIdenticalIndirectObjects: true,
		CompressStreams:                 true,
		UseObjectStreams:                true,
		CleanUnusedResources:            true,
		ImageUpperPPI:                   150,
		ImageQuality:                    80,
	}))
	n, _ := reader.GetNumPages()
	for i := 1; i <= n; i++ {
		page, err := reader.GetPage(i)
		if err != nil {
			log.Fatal(err)
		}
		if err := w.AddPage(page); err != nil {
			log.Fatal(err)
		}
	}
	if err := w.WriteToFile(out); err != nil {
		log.Fatal(err)
	}
	fmt.Println("ok")
	// Output: ok
}
