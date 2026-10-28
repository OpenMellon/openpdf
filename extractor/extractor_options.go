package extractor

import (
	"fmt"
	"sort"
	"strings"
)

// ExtractionMode defines different types of extraction mode.
type ExtractionMode int

const (
	ExtractionModeLayout = iota
	ExtractionModePlain
	ExtractionModeLayoutNoBreaks
	ExtractionModeGrid
)

// Options extractor options.
type Options struct {
	// DisableDocumentTags specifies whether to use the document tags during list extraction.
	DisableDocumentTags bool

	// ApplyCropBox will extract page text based on page cropbox if set to `true`.
	ApplyCropBox bool

	// Text extraction mode, default is ExtractionModeLayout.
	ExtractionMode ExtractionMode

	// IncludeAnnotations specifies whether to include annotations in the extraction process, default value is `false`.
	IncludeAnnotations bool

	// RelaxedMode specifies whether to use relaxed mode for processing the objects,
	// If enabled OpenPDF will automatically try to fix invalid parameters length and value.
	// Default is `false`.
	RelaxedMode bool

	// DisableDehyphenation specifies whether to disable de-hyphenation of words that are split across lines.
	// If `true`, words ending in a hyphen will not be combined with the next line.
	// Default is `false` (de-hyphenation enabled).
	DisableDehyphenation bool
}

// Match defines the structure for each match, including pattern, indexes, and locations.
type Match struct {
	Pattern   string
	Indexes   [][]int
	Locations []Box
}

func getMatchMarks(loc []int, marks *TextMarkArray, pattern string) (*TextMarkArray, Box, error) {
	box := Box{}
	start := loc[0]
	end := loc[1]
	lead := len(pattern) - len(strings.TrimLeft(pattern, " "))
	trail := len(pattern) - len(strings.TrimRight(pattern, " \n"))
	start = start + lead
	end = end - trail
	span, err := marks.RangeOffset(start, end)
	if err != nil {
		return nil, box, err
	}
	bbox, ok := span.BBox()
	if !ok {
		return nil, box, fmt.Errorf("spanMarks.BBox has no bounding box. spanMarks=%s", span)
	}
	box = Box{BBox: bbox}
	return span, box, nil
}

func renderGrid(rows []gridRow, minX, unit, spacing float64) string {
	var builder strings.Builder
	for k, row := range rows {
		sort.Slice(row.words, func(i, j int) bool { return row.words[i].llx < row.words[j].llx })
		var builder2 strings.Builder
		for i, word := range row.words {
			if i > 0 {
				builder2.WriteString(spacesForGap(word.llx-row.words[i-1].urx, unit))
			} else {
				builder2.WriteString(spacesForOffset(word.llx-minX, unit))
			}
			builder2.WriteString(word.text)
		}
		bidi := reorderBidi([]rune(builder2.String()))
		builder.WriteString(bidi.text)
		builder.WriteByte('\n')
		if k+1 < len(rows) {
			if rows[k].y-rows[k+1].y > spacing {
				builder.WriteByte('\n')
			}
		}
	}
	return builder.String()
}

// RenderMode specifies the text rendering mode (Tmode), which determines whether showing text shall cause
// glyph outlines to be  stroked, filled, used as a clipping boundary, or some combination of the three.
// Stroking, filling, and clipping shall have the same effects for a text object as they do for a path object
// (see 8.5.3, "Path-Painting Operators" and 8.5.4, "Clipping Path Operators").
type RenderMode int

const (
	RenderModeStroke RenderMode = 1 << iota
	RenderModeFill
	RenderModeClip
)
