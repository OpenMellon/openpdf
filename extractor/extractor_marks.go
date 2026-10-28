package extractor

import (
	"errors"
	"fmt"
	"image/color"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

// Len returns the number of TextMarks in `ma`.
func (tma *TextMarkArray) Len() int {
	if tma == nil {
		return 0
	}
	return len(tma.marks)
}

func trimLastMarkRune(marks []TextMark, offset *int) []TextMark {
	last := marks[len(marks)-1]
	runes := []rune(last.Text)
	if len(runes) == 1 {
		marks = marks[:len(marks)-1]
		prev := marks[len(marks)-1]
		*offset = prev.Offset + len(prev.Text)
	} else {
		trimmed := dropLastRune(last.Text)
		*offset += len(trimmed) - len(last.Text)
		last.Text = trimmed
	}
	return marks
}

func (tm *textMark) bbox() model.PdfRectangle { return tm.PdfRectangle }

// Search searches the pages specified by `pages`.
func (e *Editor) Search(pattern string, pages []int) (map[int]Match, error) {
	matches, _, err := e.getMatches(pattern, pages)
	return matches, err
}

// Replace takes a pattern and replaces all the texts that much the pattern with `replacement`.
func (e *Editor) Replace(pattern string, replacement string, pages []int) error {
	matches := map[int]Match{}
	for _, pageNum := range pages {
		pdfPage, err := e.reader.GetPage(pageNum)
		if err != nil {
			return err
		}
		ex, err := New(pdfPage)
		if err != nil {
			return err
		}
		pageText, _, _, err := ex.ExtractPageText()
		if err != nil {
			return err
		}
		contents := ""
		marksText := pageText.marksText()
		allMatches, err := findAllMatches(pattern, marksText)
		if err != nil {
			return err
		}
		marks := pageText.Marks()
		boxes := []Box{}
		for _, loc := range allMatches {
			matchMarks, box, err2 := getMatchMarks(loc, marks, pattern)
			if err2 != nil {
				return err2
			}
			objMarks := groupMarksByObject(matchMarks)
			boxes = append(boxes, box)
			err2 = replaceTextMarks(objMarks, replacement)
			if err2 != nil {
				return err2
			}
		}
		contents = pageText.GetContentStreamOps().String()
		match := Match{Pattern: pattern, Indexes: allMatches, Locations: boxes}
		pdfPage.SetContentStreams([]string{contents}, core.NewFlateEncoder())
		matches[pageNum] = match
	}
	return nil
}

// NewEditor returns a new Editor object
func NewEditor(reader *model.PdfReader) *Editor { return &Editor{reader: reader} }

// ToTextMark returns the public view of `tm`.
func (tm *textMark) ToTextMark() TextMark {
	return TextMark{Text: tm.text, Original: tm.original, BBox: tm.origBBox, Font: tm.font, FontSize: tm.fontSize, FillColor: tm.fillColor, StrokeColor: tm.strokeColor, Orientation: tm.orientation, DirectObject: tm.directObject, ObjString: tm.objString, Tw: tm.Tw, Th: tm.Th, Tc: tm.charSpacing, Tfs: tm.tfs, Index: tm.index}
}

// String returns a string describing `ma`.
func (tma TextMarkArray) String() string {
	numMarks := len(tma.marks)
	if numMarks == 0 {
		return "EMPTY"
	}
	first := tma.marks[0]
	last := tma.marks[numMarks-1]
	return fmt.Sprintf("{TEXTMARKARRAY: %d elements\n\tfirst=%s\n\t last=%s}", numMarks, first, last)
}

func replaceRange(s string, start, end int, repl string) string {
	if end > len(s)-1 {
		return s[:start] + repl
	}
	return s[:start] + repl + s[end:]
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

func subTextForMark(s string, start, length, limit int) string {
	if length == 0 {
		return ""
	}
	end := start + length
	if end >= limit || end > len(s) {
		end = len(s)
	} else {
		end = alignToRuneStart(s, end)
	}
	from := alignToRuneStart(s, start)
	if from >= end {
		return ""
	}
	return s[from:end]
}

// String returns a string describing `tm`.
func (tm TextMark) String() string {
	bbox := tm.BBox
	var font string
	if tm.Font != nil {
		font = tm.Font.String()
		if len(font) > 50 {
			font = font[:50] + "..."
		}
	}
	var meta string
	if tm.Meta {
		meta = " *M*"
	}
	return fmt.Sprintf("{TextMark: %d %q=%02x (%6.2f, %6.2f) (%6.2f, %6.2f) %s%s}", tm.Offset, tm.Text, []rune(tm.Text), bbox.Llx, bbox.Lly, bbox.Urx, bbox.Ury, font, meta)
}

func findAllMatches(pattern string, text string) ([][]int, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("error compiling regex pattern: %w", err)
	}
	matches := re.FindAllStringIndex(text, -1)
	return matches, nil
}

// Editor represents a document editor object
type Editor struct{ reader *model.PdfReader }

// TextMarkArray is a collection of TextMarks.
type TextMarkArray struct{ marks []TextMark }

// TextMark represents extracted text on a page with information regarding both textual content,
// formatting (font and size) and positioning.
// It is the smallest unit of text on a PDF page, typically a single character.
//
// getBBox() in test_text.go shows how to compute bounding boxes of substrings of extracted text.
// The following code extracts the text on PDF page `page` into `text` then finds the bounding box
// `bbox` of substring `term` in `text`.
//
//	ex, _ := New(page)
//	// handle errors
//	pageText, _, _, err := ex.ExtractPageText()
//	// handle errors
//	text := pageText.Text()
//	textMarks := pageText.Marks()
//
//		start := strings.Index(text, term)
//	 end := start + len(term)
//	 spanMarks, err := textMarks.RangeOffset(start, end)
//	 // handle errors
//	 bbox, ok := spanMarks.BBox()
//	 // handle errors
type TextMark struct {
	// Text is the extracted text.
	Text string

	// Original is the text in the PDF. It has not been decoded like `Text`.
	Original string

	// BBox is the bounding box of the text.
	BBox model.PdfRectangle

	// Font is the font the text was drawn with.
	Font *model.PdfFont

	// FontSize is the font size the text was drawn with.
	FontSize float64

	// Offset is the offset of the start of TextMark.Text in the extracted text. If you do this
	//   text, textMarks := pageText.Text(), pageText.Marks()
	//   marks := textMarks.Elements()
	// then marks[i].Offset is the offset of marks[i].Text in text.
	Offset int

	// Meta is set true for spaces and line breaks that we insert in the extracted text. We insert
	// spaces (line breaks) when we see characters that are over a threshold horizontal (vertical)
	//  distance  apart. See wordJoiner (lineJoiner) in PageText.computeViews().
	Meta bool

	// FillColor is the fill color of the text.
	// The color is nil for spaces and line breaks (i.e. the Meta field is true).
	FillColor color.Color

	// StrokeColor is the stroke color of the text.
	// The color is nil for spaces and line breaks (i.e. the Meta field is true).
	StrokeColor color.Color

	// Orientation is the text orientation
	Orientation int

	// DirectObject is the underlying PdfObject (Text Object) that represents the visible texts. This is introduced to get
	// a simple access to the TextObject in case editing or replacment of some text is needed. E.g during redaction.
	DirectObject core.PdfObject

	// ObjString is a decoded string operand of a text-showing operator. It has the same value as `Text` attribute except
	// when many glyphs are represented with the same Text Object that contains multiple length string operand in which case
	// ObjString spans more than one character string that falls in different TextMark objects.
	ObjString []string
	Tw        float64
	Th        float64
	Tc        float64

	// Tfs is the font size set by the Tf operator, in unscaled text space units. Unlike
	// FontSize it is not affected by the CTM or the text matrix.
	Tfs      float64
	Index    int
	hasTable bool
	table    *TextTable
}

// Match defines the structure for each match, including pattern, indexes, and locations.
type Match struct {
	Pattern   string
	Indexes   [][]int
	Locations []Box
}

func marksToString(marks *TextMarkArray) string {
	var out strings.Builder
	for _, mark := range marks.Elements() {
		out.WriteString(mark.Text)
	}
	return out.String()
}

func (e *Editor) getMatches(pattern string, pages []int) (map[int]Match, map[int][]*TextMarkArray, error) {
	matches := map[int]Match{}
	marksByPage := map[int][]*TextMarkArray{}
	for _, pageNum := range pages {
		page, err := e.reader.GetPage(pageNum)
		if err != nil {
			return nil, nil, err
		}
		ex, err := New(page)
		if err != nil {
			return nil, nil, err
		}
		pageText, _, _, err := ex.ExtractPageText()
		if err != nil {
			return nil, nil, err
		}
		marksText := pageText.marksText()
		allMatches, err := findAllMatches(pattern, marksText)
		if err != nil {
			return nil, nil, err
		}
		if len(allMatches) == 0 {
			common.Log.Info("No match found for %s on page %d", pattern, pageNum)
		}
		marks := pageText.Marks()
		boxes := []Box{}
		for _, allMatch := range allMatches {
			_, box, err2 := getMatchMarks(allMatch, marks, pattern)
			if err2 != nil {
				return nil, nil, err2
			}
			boxes = append(boxes, box)
		}
		match := Match{Pattern: pattern, Indexes: allMatches, Locations: boxes}
		matches[pageNum] = match
	}
	return matches, marksByPage, nil
}

// Append appends `mark` to the mark array.
func (tma *TextMarkArray) Append(mark TextMark) { tma.marks = append(tma.marks, mark) }

// WriteToFile writes the edited content to `outputPath`.
func (e *Editor) WriteToFile(outputPath string) error {
	writer, err := e.reader.ToWriter(nil)
	if err != nil {
		return fmt.Errorf("failed to convert to writer %v", err)
	}
	writer.WriteToFile(outputPath)
	return nil
}

func adjacentObjStringIndex(marks *TextMarkArray, idx int) int {
	elems := marks.Elements()
	prev := idx - 1
	next := idx + 1
	found := -1
	if prev >= 0 {
		pm := elems[prev]
		objStr := pm.ObjString
		numObj := len(objStr)
		pi := pm.Index
		if pi+1 < numObj {
			return prev
		}
	}
	if next < len(elems) {
		nm := elems[next]
		nextObjStr := nm.ObjString
		if nextObjStr[0] != nm.Text {
			return next
		}
	}
	if found == -1 && elems[idx].Text == " " {
		return prev
	}
	return found
}

// TableInfo gets table information of the textmark `tm`.
func (tm *TextMark) TableInfo() (*TextTable, [][]int) {
	if !tm.hasTable {
		return nil, nil
	}
	table := tm.table
	cell := table.getCellInfo(*tm)
	return table, cell
}

// Elements returns the TextMarks in `ma`.
func (tma *TextMarkArray) Elements() []TextMark { return tma.marks }

type textMark struct {
	model.PdfRectangle
	orientation  int
	text         string
	original     string
	font         *model.PdfFont
	fontSize     float64
	charSpacing  float64
	trm          transform.Matrix
	end          transform.Point
	origBBox     model.PdfRectangle
	fillColor    color.Color
	strokeColor  color.Color
	directObject core.PdfObject
	objString    []string
	Tw           float64
	Th           float64
	tfs          float64
	mcid         int
	index        int
}

func (tm *textMark) inDiacriticArea(mark *textMark) bool {
	dllx := tm.Llx - mark.Llx
	durx := tm.Urx - mark.Urx
	dlly := tm.Lly - mark.Lly
	return math.Abs(dllx+durx) < tm.Width()*diacriticAreaFactor && math.Abs(dlly) < tm.Height()*diacriticAreaFactor
}

// String returns a description of `tm`.
func (tm *textMark) String() string {
	return fmt.Sprintf("%.2f fontsize=%.2f \"%s\"", tm.PdfRectangle, tm.fontSize, tm.text)
}

func (tma *TextMarkArray) exists(m TextMark) bool {
	for _, other := range tma.Elements() {
		if reflect.DeepEqual(m.DirectObject, other.DirectObject) && reflect.DeepEqual(m.BBox, other.BBox) && other.Text == m.Text {
			return true
		}
	}
	return false
}

// RangeOffset returns the TextMarks in `ma` that overlap text[start:end] in the extracted text.
// These are tm: `start` <= tm.Offset + len(tm.Text) && tm.Offset < `end` where
// `start` and `end` are offsets in the extracted text.
// NOTE: TextMarks can contain multiple characters. e.g. "ffi" for the ﬃ ligature so the first and
// last elements of the returned TextMarkArray may only partially overlap text[start:end].
func (tma *TextMarkArray) RangeOffset(start, end int) (*TextMarkArray, error) {
	if tma == nil {
		return nil, errors.New("ma==nil")
	}
	if end < start {
		return nil, fmt.Errorf("end < start. RangeOffset not defined. start=%d end=%d ", start, end)
	}
	numMarks := len(tma.marks)
	if numMarks == 0 {
		return tma, nil
	}
	if start < tma.marks[0].Offset {
		start = tma.marks[0].Offset
	}
	if end > tma.marks[numMarks-1].Offset+1 {
		end = tma.marks[numMarks-1].Offset + 1
	}
	iStart := sort.Search(numMarks, func(i int) bool { return tma.marks[i].Offset+len(tma.marks[i].Text)-1 >= start })
	if !(0 <= iStart && iStart < numMarks) {
		err := fmt.Errorf("out of range. start=%d iStart=%d len=%d\n\tfirst=%v\n\t last=%v", start, iStart, numMarks, tma.marks[0], tma.marks[numMarks-1])
		return nil, err
	}
	iEnd := sort.Search(numMarks, func(i int) bool { return tma.marks[i].Offset > end-1 })
	if !(0 <= iEnd && iEnd < numMarks) {
		err := fmt.Errorf("out of range. end=%d iEnd=%d len=%d\n\tfirst=%v\n\t last=%v", end, iEnd, numMarks, tma.marks[0], tma.marks[numMarks-1])
		return nil, err
	}
	if iEnd <= iStart {
		return nil, fmt.Errorf("iEnd <= iStart: start=%d end=%d iStart=%d iEnd=%d", start, end, iStart, iEnd)
	}
	return &TextMarkArray{marks: tma.marks[iStart:iEnd]}, nil
}
func dropLastRune(s string) string { runes := []rune(s); return string(runes[:len(runes)-1]) }

func (tma *TextMarkArray) getTextMarkAtOffset(offset int) *TextMark {
	for _, mark := range tma.marks {
		if mark.Offset == offset {
			return &mark
		}
	}
	return nil
}

func replaceTextMarks(marks []*TextMarkArray, s string) error {
	offset := 0
	total := 0
	for _, mark := range marks {
		if !hasDirectObject(mark) {
			continue
		}
		for _, m := range mark.Elements() {
			total += len(m.Text)
		}
	}
	for _, mark := range marks {
		if !hasDirectObject(mark) {
			continue
		}
		objStr := mark.marks[0].DirectObject.String()
		if len(objStr) > 1 {
			err := replaceMultiByteString(mark, s, &offset, total)
			if err != nil {
				return err
			}
		} else if len(objStr) == 1 {
			err := replaceSingleByteString(mark, s, &offset, total)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// BBox returns the smallest axis-aligned rectangle that encloses all the TextMarks in `ma`.
func (tma *TextMarkArray) BBox() (model.PdfRectangle, bool) {
	var rect model.PdfRectangle
	found := false
	for _, mark := range tma.marks {
		if mark.Meta || isBlank(mark.Text) {
			continue
		}
		if found {
			rect = unionRects(rect, mark.BBox)
		} else {
			rect = mark.BBox
			found = true
		}
	}
	return rect, found
}
