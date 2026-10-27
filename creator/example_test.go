package creator_test

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"path/filepath"

	"github.com/OpenMellon/openpdf/creator"
)

// Create a one-page report with a heading and a table.
func Example() {
	dir, err := os.MkdirTemp("", "openpdf")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	c := creator.New()
	c.NewPage()

	title := c.NewStyledParagraph()
	title.SetText("Quarterly report")
	title.SetFontSize(20)
	if err := c.Draw(title); err != nil {
		log.Fatal(err)
	}

	table := c.NewTable(2)
	if err := table.SetColumnWidths(0.6, 0.4); err != nil {
		log.Fatal(err)
	}
	for _, row := range [][2]string{{"Region", "Revenue"}, {"EMEA", "1,200"}, {"APAC", "950"}} {
		for _, text := range row {
			p := c.NewStyledParagraph()
			p.SetText(text)
			if err := table.NewCell().SetContent(p); err != nil {
				log.Fatal(err)
			}
		}
	}
	if err := c.Draw(table); err != nil {
		log.Fatal(err)
	}

	out := filepath.Join(dir, "report.pdf")
	if err := c.WriteToFile(out); err != nil {
		log.Fatal(err)
	}
	info, err := os.Stat(out)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("written:", info.Size() > 0)
	// Output: written: true
}

// Stamp a translucent, rotated image over page content as a watermark.
func Example_watermark() {
	img := image.NewRGBA(image.Rect(0, 0, 200, 60))
	for x := 0; x < 200; x++ {
		for y := 0; y < 60; y++ {
			img.Set(x, y, color.RGBA{R: 200, A: 255})
		}
	}

	c := creator.New()
	c.NewPage()

	p := c.NewStyledParagraph()
	p.SetText("Confidential")
	if err := c.Draw(p); err != nil {
		log.Fatal(err)
	}

	mark, err := c.NewImageFromGoImage(img)
	if err != nil {
		log.Fatal(err)
	}
	mark.SetPos(150, 400)
	mark.SetAngle(45)
	mark.SetOpacity(0.2)
	if err := c.Draw(mark); err != nil {
		log.Fatal(err)
	}

	if err := c.Finalize(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("ok")
	// Output: ok
}
