package extractor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/OpenMellon/openpdf/model"
)

// Box represents the bounding box of a given textMark on pdf page.
// This might be used for different kinds of high lighting after doing the search
type Box struct{ BBox model.PdfRectangle }

type markKind int

// String returns a description of `k`.
func (mk markKind) String() string {
	name, ok := markKindNames[mk]
	if !ok {
		return fmt.Sprintf("Not a mark: %d", mk)
	}
	return name
}

var markKindNames = map[markKind]string{markKindStroke: "stroke", markKindFill: "fill", markKindAugment: "augment"}

const (
	markKindNone markKind = iota
	markKindStroke
	markKindFill
	markKindAugment
)

type intSet map[int]struct{}

func (is intSet) add(v int) { is[v] = struct{}{} }

// String returns a human readable description of `s`.
func (is intSet) String() string {
	var idxs []int
	for i := range is {
		if is.has(i) {
			idxs = append(idxs, i)
		}
	}
	sort.Ints(idxs)
	return fmt.Sprintf("%+v", idxs)
}

func (is intSet) has(v int) bool { _, ok := is[v]; return ok }

func (is intSet) del(v int) { delete(is, v) }

type bounded interface{ bbox() model.PdfRectangle }

type event struct {
	pos     float64
	isStart bool
	idx     int
}

type stateStack []*textState

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

func (ss *stateStack) empty() bool { return len(*ss) == 0 }

func (ss *stateStack) size() int { return len(*ss) }

func (ss *stateStack) push(state *textState) { saved := *state; *ss = append(*ss, &saved) }

func (ss *stateStack) top() *textState {
	if ss.empty() {
		return nil
	}
	return (*ss)[ss.size()-1]
}
