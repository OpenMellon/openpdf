package extractor

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"io"
	"math"
	"reflect"
	"sort"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

// BidiText represents a bidi text organized in its visual order
// with base direction of the text.
type BidiText struct {
	text      string
	direction string
}

func newBidiText(s string, ltr bool, vertical bool) BidiText {
	dir := "ltr"
	if vertical {
		dir = "ttb"
	} else if !ltr {
		dir = "rtl"
	}
	return BidiText{text: s, direction: dir}
}

var bidiTypesArabic = []string{"AN", "AN", "AN", "AN", "AN", "AN", "ON", "ON", "AL", "ET", "ET", "AL", "CS", "AL", "ON", "ON", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "AL", "AL", "", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "AN", "AN", "AN", "AN", "AN", "AN", "AN", "AN", "AN", "AN", "ET", "AN", "AN", "AL", "AL", "AL", "NSM", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "AN", "ON", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "AL", "AL", "NSM", "NSM", "ON", "NSM", "NSM", "NSM", "NSM", "AL", "AL", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "AL", "AL", "AL", "AL", "AL", "AL"}

var bidiTypesLatin = []string{"BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "S", "B", "S", "WS", "B", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "B", "B", "B", "S", "WS", "ON", "ON", "ET", "ET", "ET", "ON", "ON", "ON", "ON", "ON", "ES", "CS", "ES", "CS", "CS", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "CS", "ON", "ON", "ON", "ON", "ON", "ON", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "ON", "ON", "ON", "ON", "ON", "ON", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "ON", "ON", "ON", "ON", "BN", "BN", "BN", "BN", "BN", "BN", "B", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "CS", "ON", "ET", "ET", "ET", "ET", "ON", "ON", "ON", "ON", "L", "ON", "ON", "BN", "ON", "ON", "ET", "ET", "EN", "EN", "ON", "L", "ON", "ON", "ON", "EN", "L", "ON", "ON", "ON", "ON", "ON", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "ON", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "ON", "L", "L", "L", "L", "L", "L", "L", "L"}

const (
	colorKindOther byte = iota
	colorKindGray
	colorKindRGB
	colorKindCMYK
	colorKindPattern
)

func colorToRGB(cs model.PdfColorspace, c model.PdfColor) color.Color {
	if cs == nil || c == nil {
		return color.Black
	}
	rgbColor, err := cs.ColorToRGB(c)
	if err != nil {
		common.Log.Debug("WARN: could not convert color %v (%v) to RGB: %s", c, cs, err)
		return color.Black
	}
	rgb, ok := rgbColor.(*model.PdfColorDeviceRGB)
	if !ok {
		common.Log.Debug("WARN: converted color is not in the RGB colorspace: %v", rgbColor)
		return color.Black
	}
	return color.NRGBA{R: uint8(rgb.R() * 255), G: uint8(rgb.G() * 255), B: uint8(rgb.B() * 255), A: uint8(255)}
}

type colorStateKey struct {
	kind       byte
	colorspace model.PdfColorspace
	pattern    string
	numComps   byte
	comps      [4]float64
	color      model.PdfColor
}

func newColorStateKey(cs model.PdfColorspace, c model.PdfColor) colorStateKey {
	key := colorStateKey{}
	switch typed := cs.(type) {
	case *model.PdfColorspaceDeviceGray:
		key.kind = colorKindGray
	case *model.PdfColorspaceDeviceRGB:
		key.kind = colorKindRGB
	case *model.PdfColorspaceDeviceCMYK:
		key.kind = colorKindCMYK
	case *model.PdfColorspaceSpecialPattern:
		if typed.UnderlyingCS == nil {
			key.kind = colorKindPattern
		} else {
			key.colorspace = typed
		}
	default:
		key.colorspace = typed
	}
	if pat, ok := c.(*model.PdfColorPattern); ok {
		key.pattern = string(pat.PatternName)
	}
	if c != nil {
		if comps := c.Components(); len(comps) <= len(key.comps) {
			key.numComps = byte(len(comps))
			copy(key.comps[:], comps)
		} else {
			key.color = c
		}
	}
	return key
}

// TextMarkArray is a collection of TextMarks.
type TextMarkArray struct{ marks []TextMark }

// Len returns the number of TextMarks in `ma`.
func (tma *TextMarkArray) Len() int {
	if tma == nil {
		return 0
	}
	return len(tma.marks)
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

// Append appends `mark` to the mark array.
func (tma *TextMarkArray) Append(mark TextMark) { tma.marks = append(tma.marks, mark) }

// Elements returns the TextMarks in `ma`.
func (tma *TextMarkArray) Elements() []TextMark { return tma.marks }

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

func (tma *TextMarkArray) getTextMarkAtOffset(offset int) *TextMark {
	for _, mark := range tma.marks {
		if mark.Offset == offset {
			return &mark
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

func (tm *textMark) bbox() model.PdfRectangle { return tm.PdfRectangle }

// ToTextMark returns the public view of `tm`.
func (tm *textMark) ToTextMark() TextMark {
	return TextMark{Text: tm.text, Original: tm.original, BBox: tm.origBBox, Font: tm.font, FontSize: tm.fontSize, FillColor: tm.fillColor, StrokeColor: tm.strokeColor, Orientation: tm.orientation, DirectObject: tm.directObject, ObjString: tm.objString, Tw: tm.Tw, Th: tm.Th, Tc: tm.charSpacing, Tfs: tm.tfs, Index: tm.index}
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

// TableInfo gets table information of the textmark `tm`.
func (tm *TextMark) TableInfo() (*TextTable, [][]int) {
	if !tm.hasTable {
		return nil, nil
	}
	table := tm.table
	cell := table.getCellInfo(*tm)
	return table, cell
}

type textPara struct {
	model.PdfRectangle
	eBBox   model.PdfRectangle
	lines   []*textLine
	table   *textTable
	isTaken bool
	isEmpty bool
	left    *textPara
	right   *textPara
	above   *textPara
	below   *textPara
	lists   []list
}

func (tp *textPara) toTextMarks(offset *int, ctx *textContext) []TextMark {
	if tp.table == nil {
		return tp.toCellTextMarks(offset, ctx)
	}
	var marks []TextMark
	for row := 0; row < tp.table.h; row++ {
		for col := 0; col < tp.table.w; col++ {
			para := tp.table.get(col, row)
			if para == nil {
				marks = appendMetaMark(marks, offset, "\t")
			} else {
				cellMarks := para.toCellTextMarks(offset, ctx)
				marks = append(marks, cellMarks...)
			}
			marks = appendMetaMark(marks, offset, " ")
		}
		if row < tp.table.h-1 {
			marks = appendMetaMark(marks, offset, "\n")
		}
	}
	table := tp.table
	if table.isExportable() {
		tt := table.toTextTable(ctx)
		marks = attachTable(marks, &tt)
	}
	return marks
}

func newTextPara(rect model.PdfRectangle, lines []*textLine) *textPara {
	return &textPara{PdfRectangle: rect, lines: lines}
}

func (tp *textPara) bbox() model.PdfRectangle { return tp.PdfRectangle }

func (tp *textPara) writeCellText(writer io.Writer, ctx *textContext) {
	for i, line := range tp.lines {
		text := line.text()
		dehyphenate := !ctx.isDehyphenationDisabled() && line.endsInHyphen() && i != len(tp.lines)-1
		if dehyphenate {
			text = dropLastRune(text)
		}
		writer.Write([]byte(text))
		if !(dehyphenate || i == len(tp.lines)-1) {
			writer.Write([]byte(separatorFor(line.depth, tp.lines[i+1].depth)))
		}
	}
}

func (tp *textPara) writeText(writer io.Writer, ctx *textContext) {
	if tp.table == nil {
		tp.writeCellText(writer, ctx)
		return
	}
	for row := 0; row < tp.table.h; row++ {
		for col := 0; col < tp.table.w; col++ {
			para := tp.table.get(col, row)
			if para == nil {
				writer.Write([]byte("\t"))
			} else {
				sortLinesByDepth(para.lines)
				para.writeCellText(writer, ctx)
			}
			writer.Write([]byte(" "))
		}
		if row < tp.table.h-1 {
			writer.Write([]byte("\n"))
		}
	}
}

func (tp *textPara) isAtom() *textTable {
	topLeft := tp
	right := tp.right
	below := tp.below
	if right.taken() || below.taken() {
		return nil
	}
	diag := right.below
	if diag.taken() || diag != below.right {
		return nil
	}
	return newTwoByTwoTable(topLeft, right, below, diag)
}

func (tp *textPara) taken() bool { return tp == nil || tp.isTaken }

func (tp *textPara) fontsize() float64 { return tp.lines[0].fontSize }

// String returns a description of `p`.
func (tp *textPara) String() string {
	if tp.isEmpty {
		return fmt.Sprintf("%6.2f [EMPTY]", tp.PdfRectangle)
	}
	dims := ""
	if tp.table != nil {
		dims = fmt.Sprintf("[%dx%d] ", tp.table.w, tp.table.h)
	}
	return fmt.Sprintf("%6.2f %s%d lines %q", tp.PdfRectangle, dims, len(tp.lines), truncate(tp.text(), 50))
}

func (tp *textPara) getListLines() []*textLine {
	var lines []*textLine
	bulletLines := linesWithBullets(tp.lines)
	for _, line := range tp.lines {
		b := line.words[0].text[0]
		if startsWithBulletByte(b) {
			lines = append(lines, line)
		}
	}
	lines = append(lines, bulletLines...)
	return lines
}

func (tp *textPara) text() string {
	buf := new(bytes.Buffer)
	tp.writeText(buf, &textContext{})
	return buf.String()
}

func (tp *textPara) depth() float64 {
	if tp.isEmpty {
		return -1.0
	}
	if len(tp.lines) > 0 {
		return tp.lines[0].depth
	}
	return tp.table.depth()
}

func (tp *textPara) toCellTextMarks(offset *int, ctx *textContext) []TextMark {
	var marks []TextMark
	for i, line := range tp.lines {
		lineMarks := line.toTextMarks(offset)
		dehyphenate := !ctx.isDehyphenationDisabled() && line.endsInHyphen() && i != len(tp.lines)-1
		if dehyphenate {
			lineMarks = trimLastMarkRune(lineMarks, offset)
		}
		marks = append(marks, lineMarks...)
		if !(dehyphenate || i == len(tp.lines)-1) {
			marks = appendMetaMark(marks, offset, separatorFor(line.depth, tp.lines[i+1].depth))
		}
	}
	return marks
}
