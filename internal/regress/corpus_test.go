package regress

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OpenMellon/openpdf/extractor"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/render"
)

const (
	corpusEnv     = "OPENPDF_CORPUS"
	corpusTimeout = 60 * time.Second
)

// TestCorpus opens every PDF under $OPENPDF_CORPUS, counts pages, extracts text and renders page 1, recording status,
// timings and hashes to $OPENPDF_REGRESS_OUT/corpus.csv (or a temp directory). A panic, error or timeout on one file
// is recorded and the walk continues (a hung file's goroutine cannot be cancelled and keeps running, so timings after
// a timeout are less reliable); the test fails at the end if any file failed. It is skipped when the variable is
// unset, so the normal suite never depends on a corpus.
func TestCorpus(t *testing.T) {
	root := os.Getenv(corpusEnv)
	if root == "" {
		t.Skipf("set %s to a directory of PDFs to run the corpus smoke test", corpusEnv)
	}
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".pdf") {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	outDir := os.Getenv(outEnv)
	if outDir == "" {
		outDir = t.TempDir()
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	csvPath := filepath.Join(outDir, "corpus.csv")
	f, err := os.Create(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Write([]string{"file", "status", "pages", "text_bytes", "text_fnv", "render_fnv", "open_ms", "text_ms", "render_ms", "error"})

	failed := 0
	for _, path := range files {
		rel, _ := filepath.Rel(root, path)
		res := runCorpusFile(path)
		if res.status != "ok" && res.status != "skipped" {
			failed++
			t.Errorf("%s: %s: %s", rel, res.status, res.err)
		}
		w.Write([]string{
			rel, res.status, fmt.Sprint(res.pages), fmt.Sprint(res.textBytes), res.textHash, res.renderHash,
			fmt.Sprint(res.open.Milliseconds()), fmt.Sprint(res.text.Milliseconds()), fmt.Sprint(res.render.Milliseconds()), res.err,
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		t.Fatalf("writing %s: %v", csvPath, err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d files, %d failed; results in %s", len(files), failed, csvPath)
}

type corpusResult struct {
	status, err, textHash, renderHash string
	pages, textBytes                  int
	open, text, render                time.Duration
}

// runCorpusFile processes one file in a goroutine so a hang times out and a panic is reported instead of crashing.
func runCorpusFile(path string) corpusResult {
	done := make(chan corpusResult, 1)
	go func() {
		var res corpusResult
		defer func() {
			if r := recover(); r != nil {
				res.status, res.err = "panic", fmt.Sprint(r)
			}
			done <- res
		}()
		res = processCorpusFile(path)
	}()
	select {
	case res := <-done:
		return res
	case <-time.After(corpusTimeout):
		return corpusResult{status: "timeout", err: "exceeded " + corpusTimeout.String()}
	}
}

func processCorpusFile(path string) (res corpusResult) {
	fail := func(stage string, err error) corpusResult {
		res.status, res.err = stage, err.Error()
		return res
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fail("read", err)
	}
	start := time.Now()
	r, err := model.NewPdfReader(bytes.NewReader(data))
	if err != nil {
		return fail("open", err)
	}
	enc, err := r.IsEncrypted()
	if err != nil {
		return fail("open", err)
	}
	if enc {
		if ok, err := r.Decrypt(nil); err != nil {
			return fail("decrypt", err)
		} else if !ok {
			res.status, res.err = "skipped", "encrypted, needs a password"
			return res
		}
	}
	if res.pages, err = r.GetNumPages(); err != nil {
		return fail("pages", err)
	}
	res.open = time.Since(start)

	start = time.Now()
	h := fnv.New64a()
	for i := 1; i <= res.pages; i++ {
		page, err := r.GetPage(i)
		if err != nil {
			return fail("page", err)
		}
		ex, err := extractor.New(page)
		if err != nil {
			return fail("extract", err)
		}
		text, err := ex.ExtractText()
		if err != nil {
			return fail("extract", err)
		}
		res.textBytes += len(text)
		h.Write([]byte(text))
	}
	res.text = time.Since(start)
	res.textHash = fmt.Sprintf("%016x", h.Sum64())

	if res.pages > 0 {
		start = time.Now()
		page, err := r.GetPage(1)
		if err != nil {
			return fail("page", err)
		}
		img, err := render.NewImageDevice().Render(page)
		if err != nil {
			return fail("render", err)
		}
		png, err := encodePNG(img)
		if err != nil {
			return fail("render", err)
		}
		rh := fnv.New64a()
		rh.Write(png)
		res.renderHash = fmt.Sprintf("%016x", rh.Sum64())
		res.render = time.Since(start)
	}
	res.status = "ok"
	return res
}
