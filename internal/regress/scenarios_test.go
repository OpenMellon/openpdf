package regress

import (
	"bytes"
	"fmt"
	"strconv"
	"testing"

	"github.com/OpenMellon/openpdf/creator"
	"github.com/OpenMellon/openpdf/extractor"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/render"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// scenario builds one PDF and says which artefacts it is checked against.
type scenario struct {
	name  string
	build func(t *testing.T) []byte
	// pixels renders every page to a PNG golden. Only valid when all text is embedded TrueType: render falls back
	// to host fonts for anything else, so pixels would differ between machines.
	pixels bool
	// password opens an encrypted result. noBytes skips doc.pdf and the dump, for output that is not deterministic
	// (encryption uses random salts and file IDs); text and pixels are still checked.
	password string
	noBytes  bool
}

var scenarios = []scenario{
	{name: "truetype-text", build: buildTrueTypeText, pixels: true},
}

func TestScenarios(t *testing.T) {
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			// Golden tests share the package-level metadata globals set in TestMain, so they never run in parallel.
			data := sc.build(t)
			g := newGoldens(t, sc.name)
			if !sc.noBytes {
				g.add("doc.pdf", data)
				dump, err := dumpPDF(data)
				if err != nil {
					t.Fatalf("dump: %v", err)
				}
				g.add("doc.dump.txt", []byte(dump))
			}
			text, pages := readPages(t, data, sc.password)
			g.add("text.txt", []byte(text))
			dev := render.NewImageDevice()
			for i, page := range pages {
				img, err := dev.Render(page)
				if err != nil {
					t.Fatalf("render page %d: %v", i+1, err)
				}
				// Without embedded fonts the pixels depend on the host, so rendering is only a smoke check.
				if sc.pixels {
					png, err := encodePNG(img)
					if err != nil {
						t.Fatal(err)
					}
					g.add(fmt.Sprintf("page-%d.png", i+1), png)
				}
			}
			g.finish()
		})
	}
}

// readPages opens the PDF and returns the normalised extracted text of every page, plus the pages.
func readPages(t *testing.T, data []byte, password string) (string, []*model.PdfPage) {
	t.Helper()
	r, err := model.NewPdfReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if password != "" {
		if ok, err := r.Decrypt([]byte(password)); err != nil || !ok {
			t.Fatalf("decrypt: ok=%v err=%v", ok, err)
		}
	}
	n, err := r.GetNumPages()
	if err != nil {
		t.Fatal(err)
	}
	var text string
	var pages []*model.PdfPage
	for i := 1; i <= n; i++ {
		page, err := r.GetPage(i)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		ex, err := extractor.New(page)
		if err != nil {
			t.Fatalf("extractor page %d: %v", i, err)
		}
		s, err := ex.ExtractText()
		if err != nil {
			t.Fatalf("extract page %d: %v", i, err)
		}
		text += "=== page " + strconv.Itoa(i) + " ===\n" + s + "\n"
		pages = append(pages, page)
	}
	return normalizeText(text), pages
}

// newCreator returns a creator whose default fonts are the embedded Go fonts, so all text is embedded TrueType.
func newCreator(t *testing.T) *creator.Creator {
	t.Helper()
	regular, err := model.NewPdfFontFromTTF(bytes.NewReader(goregular.TTF))
	if err != nil {
		t.Fatal(err)
	}
	bold, err := model.NewPdfFontFromTTF(bytes.NewReader(gobold.TTF))
	if err != nil {
		t.Fatal(err)
	}
	c := creator.New()
	c.SetDefaultFont(regular, bold)
	return c
}

func write(t *testing.T, c *creator.Creator) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func buildTrueTypeText(t *testing.T) []byte {
	c := newCreator(t)
	c.NewPage()
	p := c.NewStyledParagraph()
	p.SetText("The quick brown fox jumps over the lazy dog. Sphinx of black quartz, judge my vow. " +
		"Pack my box with five dozen liquor jugs. How vexingly quick daft zebras jump!")
	p.SetFontSize(14)
	p.SetLineHeight(1.3)
	if err := c.Draw(p); err != nil {
		t.Fatal(err)
	}
	return write(t, c)
}
