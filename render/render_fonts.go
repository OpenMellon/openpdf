package render

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/adrg/sysfont"
)

func systemFontFinder() *sysfont.Finder {
	if f := systemFontFinderCache.Load(); f != nil {
		return f
	}
	systemFontFinderMu.Lock()
	defer systemFontFinderMu.Unlock()
	if f := systemFontFinderCache.Load(); f != nil {
		return f
	}
	f := sysfont.NewFinder(&sysfont.FinderOpts{Extensions: []string{".ttf", ".ttc"}})
	systemFontFinderCache.Store(f)
	return f
}

func fontStyleHints(fontName string, desc *model.PdfFontDescriptor) (bold, italic, serif bool) {
	lower := strings.ToLower(fontName)
	if strings.Contains(lower, "italic") || strings.Contains(lower, "oblique") {
		italic = true
	}
	if strings.Contains(lower, "bold") {
		bold = true
	}
	if desc != nil {
		if flags, ok := core.GetIntVal(desc.Flags); ok {
			if flags&flagItalic != 0 {
				italic = true
			}
			if flags&flagForceBold != 0 {
				bold = true
			}
			if flags&flagSerif != 0 {
				serif = true
			}
		}
		if angle, err := core.GetNumberAsFloat(desc.ItalicAngle); err == nil && angle != 0 {
			italic = true
		}
		if weight, err := core.GetNumberAsFloat(desc.FontWeight); err == nil && weight >= 600 {
			bold = true
		}
	}
	if strings.Contains(lower, "sans") {
		serif = false
	} else if strings.Contains(lower, "serif") {
		serif = true
	}
	return bold, italic, serif
}

func fontNameCandidates(name string, bold, italic, serif bool) []string {
	var suffixes []string
	switch {
	case bold && italic:
		suffixes = []string{" Bold Italic", " Bold Oblique"}
	case bold:
		suffixes = []string{" Bold"}
	case italic:
		suffixes = []string{" Italic", " Oblique"}
	default:
		suffixes = []string{""}
	}
	families := []string{"Arial", "DejaVu Sans", "Times New Roman"}
	if serif {
		families = []string{"Times New Roman", "DejaVu Serif", "Arial", "DejaVu Sans"}
	}
	out := []string{name}
	for _, family := range families {
		for _, suffix := range suffixes {
			out = append(out, family+suffix)
		}
		if bold || italic {
			out = append(out, family)
		}
	}
	return out
}

const (
	flagSerif     = 0x00002
	flagItalic    = 0x00040
	flagForceBold = 0x40000
)

// ResetSharedFontCaches clears the process-wide shared font finder and file caches.
// Future cache misses will rescan system fonts and reload font files from disk.
func ResetSharedFontCaches() {
	systemFontFinderMu.Lock()
	systemFontFinderCache.Store(nil)
	systemFontFinderMu.Unlock()
	fontFileCache.Range(func(key, _ any) bool { fontFileCache.Delete(key); return true })
}

const maxFontCacheEntries = 128

var (
	systemFontFinderCache atomic.Pointer[sysfont.Finder]
	systemFontFinderMu    sync.Mutex
	fontFileCache         sync.Map
)

const maxFontDictCacheEntries = 128

func fontNameMatchesStyle(name string, bold, italic bool) bool {
	lower := strings.ToLower(name)
	if bold && !strings.Contains(lower, "bold") {
		return false
	}
	if italic && !strings.Contains(lower, "italic") && !strings.Contains(lower, "oblique") {
		return false
	}
	return true
}
