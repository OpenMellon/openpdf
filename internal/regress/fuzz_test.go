package regress

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/extractor"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/model"
)

// Plain go test runs only the seeds and any crashers committed under testdata/fuzz, so these double as regression
// tests. The nightly workflow runs each target with -fuzz for a bounded time; commit any crasher it finds.

const (
	// fuzzBudget bounds work per input: a pathological input fails the run (and is saved as a crasher) instead of
	// stalling the worker until -fuzztime ends.
	fuzzBudget = 10 * time.Second
	// fuzzMaxInput keeps mutated inputs small so each iteration stays fast and memory bounded.
	fuzzMaxInput = 1 << 20
)

// withinBudget runs fn and fails the test if it does not return in time. A stuck fn keeps running in its goroutine,
// but the failure makes the fuzzer record the input.
func withinBudget(t *testing.T, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(fuzzBudget):
		t.Fatalf("input did not finish within %s", fuzzBudget)
	}
}

func seedInputs(f *testing.F) {
	paths, err := filepath.Glob(filepath.Join("testdata", "inputs", "*.pdf"))
	if err != nil {
		f.Fatal(err)
	}
	if len(paths) == 0 {
		f.Fatal("no seed PDFs under testdata/inputs")
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
}

// FuzzReader feeds arbitrary bytes to the PDF reader and walks every page: loading it and extracting its text. Errors
// are expected for garbage; panics and hangs are the findings.
func FuzzReader(f *testing.F) {
	seedInputs(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > fuzzMaxInput {
			t.Skip()
		}
		withinBudget(t, func() {
			r, err := model.NewPdfReader(bytes.NewReader(data))
			if err != nil {
				return
			}
			if enc, _ := r.IsEncrypted(); enc {
				// Empty user password first, then the one the encrypted seed fixtures use.
				ok, _ := r.Decrypt(nil)
				if !ok {
					ok, _ = r.Decrypt([]byte("user"))
				}
				if !ok {
					return
				}
			}
			n, err := r.GetNumPages()
			if err != nil {
				return
			}
			for i := 1; i <= n && i <= 20; i++ {
				page, err := r.GetPage(i)
				if err != nil {
					continue
				}
				if ex, err := extractor.New(page); err == nil {
					_, _ = ex.ExtractText()
				}
			}
		})
	})
}

// FuzzContentStream feeds arbitrary bytes to the content stream parser, including inline images.
func FuzzContentStream(f *testing.F) {
	for _, seed := range []string{
		"BT /F1 12 Tf 72 720 Td (Hello) Tj ET",
		"q 100 0 0 100 10 10 cm BI /W 2 /H 2 /CS /G /BPC 8 /F /AHx ID 00FF00FF> EI Q",
		"1 0 0 rg 0 0 100 100 re f [3 2] 0 d 0 0 m 50 50 l S",
		"/GS1 gs /Im1 Do BT [(A) -120 (B)] TJ ET",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		if len(content) > fuzzMaxInput {
			t.Skip()
		}
		withinBudget(t, func() { _, _ = contentstream.NewContentStreamParser(content).Parse() })
	})
}

// FuzzCMap feeds arbitrary bytes to the CMap parser used for ToUnicode and encoding CMaps.
func FuzzCMap(f *testing.F) {
	f.Add([]byte("/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
		"1 begincodespacerange <0000> <FFFF> endcodespacerange\n" +
		"2 beginbfchar\n<0003> <0041>\n<0004> <0042>\nendbfchar\n" +
		"1 beginbfrange\n<0010> <0012> <0061>\nendbfrange\n" +
		"endcmap CMapName currentdict /CMap defineresource pop end end"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > fuzzMaxInput {
			t.Skip()
		}
		withinBudget(t, func() {
			_, _ = cmap.LoadCmapFromData(data, false)
			_, _ = cmap.LoadCmapFromData(data, true)
		})
	})
}
