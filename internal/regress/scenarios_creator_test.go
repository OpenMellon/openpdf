package regress

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"strconv"
	"testing"

	"github.com/OpenMellon/openpdf/creator"
)

func init() {
	scenarios = append(
		scenarios,
		scenario{name: "text-styles", build: buildTextStyles, pixels: true},
		scenario{name: "table", build: buildTable, pixels: true},
		scenario{name: "images", build: buildImages, pixels: true},
		scenario{name: "shapes", build: buildShapes, pixels: true},
		scenario{name: "chapters-toc-outlines", build: buildChapters, pixels: true},
		scenario{name: "header-footer", build: buildHeaderFooter, pixels: true},
		scenario{name: "page-size-rotation", build: buildPageSizeRotation, pixels: true},
		scenario{name: "watermark", build: buildWatermark, pixels: true},
		scenario{name: "standard14-text", build: buildStandard14},
	)
}

func draw(t *testing.T, c *creator.Creator, d creator.Drawable) {
	t.Helper()
	if err := c.Draw(d); err != nil {
		t.Fatal(err)
	}
}

func buildTextStyles(t *testing.T) []byte {
	c := newCreator(t)
	c.NewPage()
	title := c.NewStyledParagraph()
	title.SetText("Styles and wrapping")
	title.SetFontSize(22)
	draw(t, c, title)
	for _, col := range []creator.Color{creator.ColorBlack, creator.ColorRed, creator.ColorBlue} {
		p := c.NewStyledParagraph()
		chunk := p.Append("Coloured text that is long enough to wrap onto a second line when the margins are narrow. ")
		chunk.Style.Color = col
		chunk.Style.FontSize = 12
		p.SetMargins(0, 0, 8, 0)
		draw(t, c, p)
	}
	c.NewPage()
	p := c.NewStyledParagraph()
	p.SetText("Second page, ünïcödé: café – naïve – 100% – ½")
	draw(t, c, p)
	return write(t, c)
}

func buildTable(t *testing.T) []byte {
	c := newCreator(t)
	c.NewPage()
	table := c.NewTable(3)
	if err := table.SetColumnWidths(0.4, 0.3, 0.3); err != nil {
		t.Fatal(err)
	}
	rows := [][3]string{{"Region", "Q1", "Q2"}, {"EMEA", "1,200", "1,340"}, {"APAC", "950", "1,010"}, {"AMER", "2,030", "2,200"}}
	for r, row := range rows {
		for _, text := range row {
			p := c.NewStyledParagraph()
			p.SetText(text)
			cell := table.NewCell()
			cell.SetBorder(creator.CellBorderSideAll, creator.CellBorderStyleSingle, 1)
			cell.SetBorderColor(creator.ColorBlack)
			if r == 0 {
				cell.SetBackgroundColor(creator.ColorRGBFromHex("#ccddee"))
			}
			if err := cell.SetContent(p); err != nil {
				t.Fatal(err)
			}
		}
	}
	draw(t, c, table)
	return write(t, c)
}

// testImage is a gradient with a hard-edged square, so scaling and colour handling both show up in the render.
func testImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 120, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 120; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 2), G: uint8(y * 3), B: 128, A: 255})
		}
	}
	for y := 20; y < 40; y++ {
		for x := 20; x < 40; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 255, B: 0, A: 255})
		}
	}
	return img
}

func buildImages(t *testing.T) []byte {
	c := newCreator(t)
	c.NewPage()
	img := testImage()
	png, err := c.NewImageFromGoImage(img)
	if err != nil {
		t.Fatal(err)
	}
	png.SetPos(60, 60)
	png.ScaleToWidth(240)
	draw(t, c, png)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	jpg, err := c.NewImageFromData(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	jpg.SetPos(60, 300)
	jpg.ScaleToWidth(240)
	draw(t, c, jpg)
	return write(t, c)
}

func buildShapes(t *testing.T) []byte {
	c := newCreator(t)
	c.NewPage()
	rect := c.NewRectangle(60, 60, 200, 100)
	rect.SetFillColor(creator.ColorRGBFromHex("#ffcc00"))
	rect.SetBorderColor(creator.ColorBlack)
	rect.SetBorderWidth(3)
	draw(t, c, rect)

	ell := c.NewEllipse(400, 110, 160, 90)
	ell.SetFillColor(creator.ColorBlue)
	ell.SetBorderColor(creator.ColorRed)
	ell.SetBorderWidth(2)
	draw(t, c, ell)

	line := c.NewLine(60, 220, 540, 300)
	line.SetColor(creator.ColorRed)
	line.SetLineWidth(4)
	draw(t, c, line)
	return write(t, c)
}

func buildChapters(t *testing.T) []byte {
	c := newCreator(t)
	c.AddTOC = true
	c.CreateTableOfContents(func(toc *creator.TOC) error {
		toc.SetShowLinks(true)
		return nil
	})
	for _, title := range []string{"Introduction", "Method", "Results"} {
		ch := c.NewChapter(title)
		p := c.NewStyledParagraph()
		p.SetText("Body text for the " + title + " chapter.")
		if err := ch.Add(p); err != nil {
			t.Fatal(err)
		}
		draw(t, c, ch)
	}
	return write(t, c)
}

func buildHeaderFooter(t *testing.T) []byte {
	c := newCreator(t)
	c.DrawHeader(func(block *creator.Block, args creator.HeaderFunctionArgs) {
		p := c.NewStyledParagraph()
		p.SetText("Quarterly report")
		p.SetPos(50, 20)
		if err := block.Draw(p); err != nil {
			t.Error(err)
		}
	})
	c.DrawFooter(func(block *creator.Block, args creator.FooterFunctionArgs) {
		p := c.NewStyledParagraph()
		p.SetText("Page " + strconv.Itoa(args.PageNum) + " of " + strconv.Itoa(args.TotalPages))
		p.SetPos(250, 20)
		if err := block.Draw(p); err != nil {
			t.Error(err)
		}
	})
	for i := 0; i < 2; i++ {
		c.NewPage()
		p := c.NewStyledParagraph()
		p.SetText("Content of page " + strconv.Itoa(i+1))
		draw(t, c, p)
	}
	return write(t, c)
}

func buildPageSizeRotation(t *testing.T) []byte {
	c := newCreator(t)
	c.SetPageSize(creator.PageSizeA5)
	c.NewPage()
	p := c.NewStyledParagraph()
	p.SetText("A5 page")
	draw(t, c, p)

	c.SetPageSize(creator.PageSizeLetter)
	page := c.NewPage()
	rot := int64(90)
	page.Rotate = &rot
	q := c.NewStyledParagraph()
	q.SetText("Letter page rotated by 90 degrees")
	draw(t, c, q)
	return write(t, c)
}

func buildWatermark(t *testing.T) []byte {
	c := newCreator(t)
	c.NewPage()
	p := c.NewStyledParagraph()
	p.SetText("Confidential contract text under a watermark.")
	draw(t, c, p)
	mark, err := c.NewImageFromGoImage(testImage())
	if err != nil {
		t.Fatal(err)
	}
	mark.SetPos(150, 300)
	mark.SetAngle(45)
	mark.SetOpacity(0.3)
	draw(t, c, mark)
	return write(t, c)
}

// buildStandard14 uses the built-in Helvetica, which render only draws with host fonts, so it has no pixel golden.
func buildStandard14(t *testing.T) []byte {
	c := creator.New()
	c.NewPage()
	p := c.NewStyledParagraph()
	p.SetText("Plain Helvetica text from the standard 14 fonts, wrapped over a few lines of the page.")
	draw(t, c, p)
	return write(t, c)
}
