package extractor

import (
	"errors"
	"fmt"
	"image/color"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

func setStringFromText(str *core.PdfObjectString, text string, font *model.PdfFont) {
	codes, numMissing := font.StringToCharcodeBytes(text)
	if numMissing != 0 {
		common.Log.Debug("WARN: some runes could not be encoded.\n\t%s -> %v", text, codes)
	}
	pdfStr := core.MakeStringFromBytes(codes)
	*str = *pdfStr
}

func appendMarkWithOffset(marks []TextMark, offset *int, mark TextMark) []TextMark {
	mark.Offset = *offset
	marks = append(marks, mark)
	*offset += len(mark.Text)
	return marks
}

func singleFloatOperand(op *contentstream.ContentStreamOperation) (float64, error) {
	if len(op.Params) != 1 {
		err := errors.New("incorrect parameter count")
		common.Log.Debug("ERROR: %#q should have %d input params, got %d %+v", op.Operand, 1, len(op.Params), op.Params)
		return 0.0, err
	}
	return core.GetNumberAsFloat(op.Params[0])
}

func fillStrings(strs []string, from int, to int, s string) {
	for i := from; i < to; i++ {
		strs[i] = s
	}
}

var metaMark = TextMark{Text: "[X]", Original: " ", Meta: true, FillColor: color.White, StrokeColor: color.White}

// RenderMode specifies the text rendering mode (Tmode), which determines whether showing text shall cause
// glyph outlines to be  stroked, filled, used as a clipping boundary, or some combination of the three.
// Stroking, filling, and clipping shall have the same effects for a text object as they do for a path object
// (see 8.5.3, "Path-Painting Operators" and 8.5.4, "Clipping Path Operators").
type RenderMode int

func (ss *stateStack) pop() *textState {
	if ss.empty() {
		return nil
	}
	state := *(*ss)[len(*ss)-1]
	*ss = (*ss)[:len(*ss)-1]
	return &state
}

// String returns a string describing the current state of the textState stack.
func (ss *stateStack) String() string {
	lines := []string{fmt.Sprintf("---- font stack: %d", len(*ss))}
	for i, st := range *ss {
		str := "<nil>"
		if st != nil {
			str = st.String()
		}
		lines = append(lines, fmt.Sprintf("\t%2d: %s", i, str))
	}
	return strings.Join(lines, "\n")
}

type textContext struct{ disableDehyphenation bool }

const (
	RenderModeStroke RenderMode = 1 << iota
	RenderModeFill
	RenderModeClip
)

type stateStack []*textState

func (tc *textContext) isDehyphenationDisabled() bool {
	if tc == nil {
		return false
	}
	return tc.disableDehyphenation
}

func newTextState(mediaBox model.PdfRectangle) textState {
	return textState{th: 100, tmode: RenderModeFill, rect: mediaBox}
}
func (ss *stateStack) empty() bool { return len(*ss) == 0 }

type textResult struct {
	pageText          PageText
	numChars          int
	numMisparsedChars int
	form              *model.XObjectForm
}

func (ss *stateStack) size() int             { return len(*ss) }
func (ss *stateStack) push(state *textState) { saved := *state; *ss = append(*ss, &saved) }

// String returns a description of `state`.
func (ts *textState) String() string {
	s := "[NOT SET]"
	if ts.tfont != nil {
		s = ts.tfont.BaseFont()
	}
	return fmt.Sprintf("tc=%.2f tw=%.2f tfs=%.2f font=%q", ts.tc, ts.tw, ts.tfs, s)
}

func twoFloatOperands(objs []core.PdfObject) (x, x2 float64, err error) {
	if len(objs) != 2 {
		return 0, 0, fmt.Errorf("invalid number of params: %d", len(objs))
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objs)
	if err != nil {
		return 0, 0, err
	}
	return numbersAsFloat[0], numbersAsFloat[1], nil
}

type textState struct {
	tc                float64
	tw                float64
	th                float64
	tl                float64
	tfs               float64
	tmode             RenderMode
	trise             float64
	tfont             *model.PdfFont
	rect              model.PdfRectangle
	numChars          int
	numMisparsedChars int
}

func appendMetaMark(marks []TextMark, offset *int, text string) []TextMark {
	mark := metaMark
	mark.Text = text
	return appendMarkWithOffset(marks, offset, mark)
}

func translationPoint(m transform.Matrix) transform.Point {
	tx, ty := m.Translation()
	return transform.Point{X: tx, Y: ty}
}

func (ss *stateStack) top() *textState {
	if ss.empty() {
		return nil
	}
	return (*ss)[ss.size()-1]
}
