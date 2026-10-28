package extractor

import (
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/model"
	"golang.org/x/text/unicode/norm"
)

type textLine struct {
	model.PdfRectangle
	depth    float64
	words    []*textWord
	fontSize float64
}

func (tl *textLine) bbox() model.PdfRectangle { return tl.PdfRectangle }

func (tl *textLine) appendWord(word *textWord) {
	tl.words = append(tl.words, word)
	tl.PdfRectangle = unionRects(tl.PdfRectangle, word.PdfRectangle)
	if word.fontSize > tl.fontSize {
		tl.fontSize = word.fontSize
	}
	if word.depth > tl.depth {
		tl.depth = word.depth
	}
}

func (tl *textLine) toTextMarks(offset *int) []TextMark {
	var marks []TextMark
	for _, word := range tl.words {
		if word.spaceBefore {
			marks = appendMetaMark(marks, offset, " ")
		}
		wordMarks := word.toTextMarks(offset)
		marks = append(marks, wordMarks...)
	}
	return marks
}

func (tl *textLine) pullWord(bag *wordBag, word *textWord, idx int) {
	tl.appendWord(word)
	bag.removeWord(word, idx)
}

func (tl *textLine) endsInHyphen() bool {
	word := tl.words[len(tl.words)-1]
	text := word.text
	last, size := utf8.DecodeLastRuneInString(text)
	if size <= 0 || !unicode.Is(unicode.Hyphen, last) {
		return false
	}
	if word.spaceBefore && endsWithHyphen(text) {
		return true
	}
	return endsWithHyphen(tl.text())
}

func (tl *textLine) text() string {
	var parts []string
	for _, word := range tl.words {
		if word.spaceBefore {
			parts = append(parts, " ")
		}
		parts = append(parts, word.text)
	}
	text := strings.Join(parts, "")
	bidi := reorderBidi([]rune(text))
	return bidi.text
}

// String returns a description of `l`.
func (tl *textLine) String() string {
	return fmt.Sprintf("%.2f %6.2f fontsize=%.2f \"%s\"", tl.depth, tl.PdfRectangle, tl.fontSize, tl.text())
}

func (tl *textLine) markWordBoundaries() {
	minGap := wordBoundaryGapFactor * tl.fontSize
	for i, word := range tl.words[1:] {
		if horizontalGap(word, tl.words[i]) >= minGap {
			word.spaceBefore = true
		}
	}
}

type textWord struct {
	model.PdfRectangle
	depth       float64
	text        string
	marks       []*textMark
	fontSize    float64
	spaceBefore bool
}

func makeTextWords(marks []*textMark, pageRect model.PdfRectangle, adjustTilt bool) []*textWord {
	var words []*textWord
	var cur *textWord
	if debugWords {
		common.Log.Info("makeTextWords: %d marks", len(marks))
	}
	flush := func() {
		if cur != nil {
			text := cur.computeText()
			if !isBlank(text) {
				cur.text = text
				words = append(words, cur)
				if debugWords {
					common.Log.Info("addNewWord: %d: word=%s", len(words)-1, cur.String())
					for i, m := range cur.marks {
						fmt.Printf("%4d: %s\n", i, m.String())
					}
				}
			}
			cur = nil
		}
	}
	for _, mark := range marks {
		if mergeDiacritics && cur != nil && len(cur.marks) > 0 {
			prev := cur.marks[len(cur.marks)-1]
			comb, isComb := combiningFor(mark.text)
			prevComb, prevIsComb := combiningFor(prev.text)
			if isComb && !prevIsComb && prev.inDiacriticArea(mark) {
				cur.addDiacritic(comb)
				continue
			}
			if prevIsComb && !isComb && mark.inDiacriticArea(prev) {
				cur.marks = cur.marks[:len(cur.marks)-1]
				cur.appendMark(mark, pageRect)
				cur.addDiacritic(prevComb)
				continue
			}
		}
		blank := isBlank(mark.text)
		if blank {
			flush()
			continue
		}
		if cur == nil && !blank {
			cur = newTextWord([]*textMark{mark}, pageRect)
			continue
		}
		fontSize := cur.fontSize
		depthDelta := math.Abs(depthFromTop(pageRect, mark)-cur.depth) / fontSize
		gap := horizontalGap(mark, cur) / fontSize
		width := cur.Width() + mark.Width()
		if adjustTilt && mark.trm.Angle() != 0.0 {
			tiltAdj := math.Abs(width * math.Tan(mark.trm.Angle()) / fontSize)
			if depthDelta > tiltAdj {
				depthDelta -= tiltAdj
			} else {
				depthDelta = 0.0
			}
		}
		if gap >= maxWordGapFactor || !(-maxOverlapFactor <= gap && depthDelta <= maxDepthDeltaFactor) {
			flush()
			cur = newTextWord([]*textMark{mark}, pageRect)
			continue
		}
		cur.appendMark(mark, pageRect)
	}
	flush()
	return words
}

func newTextWord(marks []*textMark, pageRect model.PdfRectangle) *textWord {
	bbox := marks[0].PdfRectangle
	fontSize := marks[0].fontSize
	for _, mark := range marks[1:] {
		bbox = unionRects(bbox, mark.PdfRectangle)
		if mark.fontSize > fontSize {
			fontSize = mark.fontSize
		}
	}
	return &textWord{PdfRectangle: bbox, marks: marks, depth: pageRect.Ury - bbox.Lly, fontSize: fontSize}
}

func (tw *textWord) computeText() string {
	texts := make([]string, len(tw.marks))
	for i, mark := range tw.marks {
		texts[i] = mark.text
	}
	return strings.Join(texts, "")
}

// String returns a description of `w`.
func (tw *textWord) String() string {
	return fmt.Sprintf("%.2f %6.2f fontsize=%.2f \"%s\"", tw.depth, tw.PdfRectangle, tw.fontSize, tw.text)
}

func (tw *textWord) appendMark(mark *textMark, pageRect model.PdfRectangle) {
	tw.marks = append(tw.marks, mark)
	tw.PdfRectangle = unionRects(tw.PdfRectangle, mark.PdfRectangle)
	if mark.fontSize > tw.fontSize {
		tw.fontSize = mark.fontSize
	}
	tw.depth = pageRect.Ury - tw.Lly
}

func (tw *textWord) addDiacritic(s string) {
	mark := tw.marks[len(tw.marks)-1]
	mark.text += s
	mark.text = norm.NFKC.String(mark.text)
}

func (tw *textWord) toTextMarks(offset *int) []TextMark {
	var marks []TextMark
	for _, mark := range tw.marks {
		marks = appendMarkWithOffset(marks, offset, mark.ToTextMark())
	}
	return marks
}

func (tw *textWord) bbox() model.PdfRectangle { return tw.PdfRectangle }
