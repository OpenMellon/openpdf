package regress

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/OpenMellon/openpdf/model"
)

var update = flag.Bool("update", false, "rewrite the golden files under testdata")

// outEnv names a directory that receives actual and diff files when a comparison fails, so CI can upload them.
const outEnv = "OPENPDF_REGRESS_OUT"

func TestMain(m *testing.M) {
	flag.Parse()
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	model.SetPdfCreationDate(when)
	model.SetPdfModifiedDate(when)
	model.SetPdfProducer("OpenPDF regress")
	model.SetPdfCreator("OpenPDF regress")
	os.Exit(m.Run())
}

// outDir returns where failure artefacts for the named scenario go.
func outDir(t *testing.T, name string) string {
	t.Helper()
	if base := os.Getenv(outEnv); base != "" {
		dir := filepath.Join(base, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	// Not t.TempDir: it is deleted when the test ends, and the logged paths must stay readable.
	dir, err := os.MkdirTemp("", "openpdf-regress-"+name+"-")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// normalizeText trims trailing whitespace, converts line endings to LF and ends the text with one newline, so the
// repo's whitespace hooks leave goldens untouched.
func normalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

// goldens collects the artefacts of one scenario and compares them with, or writes them to, testdata/<name>.
type goldens struct {
	t     *testing.T
	name  string
	dir   string
	files map[string][]byte
	outd  string
}

// out returns the failure-artefact directory, created once per scenario so all its artefacts land together.
func (g *goldens) out() string {
	if g.outd == "" {
		g.outd = outDir(g.t, g.name)
	}
	return g.outd
}

func newGoldens(t *testing.T, name string) *goldens {
	return &goldens{t: t, name: name, dir: filepath.Join("testdata", name), files: map[string][]byte{}}
}

func (g *goldens) add(file string, data []byte) { g.files[file] = data }

// finish writes the artefacts under -update, otherwise fails the test for every artefact that differs.
func (g *goldens) finish() {
	t := g.t
	t.Helper()
	if *update {
		if err := os.RemoveAll(g.dir); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(g.dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for file, data := range g.files {
			if err := os.WriteFile(filepath.Join(g.dir, file), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	names := make([]string, 0, len(g.files))
	for file := range g.files {
		names = append(names, file)
	}
	sort.Strings(names)
	for _, file := range names {
		got := g.files[file]
		want, err := os.ReadFile(filepath.Join(g.dir, file))
		if err != nil {
			t.Errorf("%s: no golden (%v); run go test ./internal/regress -update", file, err)
			continue
		}
		g.compare(file, want, got)
	}
	entries, err := os.ReadDir(g.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if _, ok := g.files[e.Name()]; !ok {
			t.Errorf("%s: stale golden no longer produced; run go test ./internal/regress -update", e.Name())
		}
	}
}

func (g *goldens) compare(file string, want, got []byte) {
	t := g.t
	t.Helper()
	switch {
	case strings.HasSuffix(file, ".png"):
		comparePNG(t, g.out(), file, want, got)
	case bytes.Equal(want, got):
	case file == "doc.pdf" && pinnedToolchain() == "":
		t.Fatal("go.mod has no toolchain line, so the toolchain the byte goldens belong to is unknown")
	case file == "doc.pdf" && !onPinnedToolchain():
		// Compressed streams differ between Go releases, so exact bytes are only comparable on the toolchain
		// the goldens were generated with. The dump, text and pixels are still checked.
		t.Logf("doc.pdf bytes differ, not enforced on %s (go.mod toolchain is %s)", runtime.Version(), pinnedToolchain())
	case strings.HasSuffix(file, ".txt"):
		t.Errorf("%s differs from golden:\n%s", file, lineDiff(string(want), string(got)))
		saveActual(t, g.out(), file, got)
	default:
		t.Errorf("%s differs from golden (%d bytes want, %d got); see doc.dump.txt for what changed", file, len(want), len(got))
		saveActual(t, g.out(), file, got)
	}
}

func saveActual(t *testing.T, dir, file string, data []byte) {
	t.Helper()
	path := filepath.Join(dir, "actual-"+file)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Logf("could not save %s: %v", path, err)
		return
	}
	t.Logf("actual output saved to %s", path)
}

// lineDiff reports the first few lines that differ, enough to see the change without a diff dependency.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	shown := 0
	for i := 0; i < len(w) || i < len(g); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl == gl {
			continue
		}
		fmt.Fprintf(&b, "line %d:\n  want: %s\n  got:  %s\n", i+1, wl, gl)
		if shown++; shown == 8 {
			b.WriteString("  ...\n")
			break
		}
	}
	return b.String()
}

// pinnedToolchain returns the toolchain line from go.mod, the Go release the byte goldens are generated with.
func pinnedToolchain() string {
	data, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "toolchain "); ok {
			return v
		}
	}
	return ""
}

func onPinnedToolchain() bool { return runtime.Version() == pinnedToolchain() }
