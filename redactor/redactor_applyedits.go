// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package redactor

import (
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/creator"
	"github.com/OpenMellon/openpdf/extractor"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model"
)

func applyEdits(targets []*editTarget, bySource map[core.PdfObject][]localSpanMarks, lookup targetOpLookup) []editFailure {
	var failures []editFailure
	for obj, spans := range bySource {
		if obj == nil {
			continue
		}
		var target *editTarget
		var op *contentstream.ContentStreamOperation
		opIdx := -1
		for _, t := range targets {
			if foundOp, foundIdx, found := lookup(t.ops, obj); found {
				target, op, opIdx = t, foundOp, foundIdx
				break
			}
		}
		if target == nil {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("PdfObject %s not found in the page or form XObject content streams", obj)
			}
			failures = append(failures, failuresForSpans(spans, "matched text object not found in the page or form XObject content streams")...)
			continue
		}
		var err error
		switch op.Operand {
		case "Tj":
			err = rewriteTj(op, obj, spans)
		case "TJ":
			err = rewriteTJ(op, obj, spans)
		case "'", "\"":
			err = expandQuoteOperator(target.ops, op.Operand, opIdx)
			if err == nil {
				err = rewriteTj(op, obj, spans)
			}
		default:
			err = fmt.Errorf("unsupported text showing operand %q", op.Operand)
		}
		if err != nil {
			failures = append(failures, failuresForSpans(spans, err.Error())...)
			continue
		}
		target.edited = true
		if target.editedOps == nil {
			target.editedOps = make(map[*contentstream.ContentStreamOperation]bool)
		}
		target.editedOps[op] = true
		_, terms := groupByTerm(spans)
		for _, term := range terms {
			if !slices.Contains(target.terms, term) {
				target.terms = append(target.terms, term)
			}
		}
	}
	return failures
}

type matchedIndex struct {
	start int
	end   int
	text  string
}

func expandQuoteOperator(ops *contentstream.ContentStreamOperations, operand string, idx int) error {
	if idx < 0 || idx >= len(*ops) {
		return fmt.Errorf("operation index %d out of range", idx)
	}
	op := (*ops)[idx]
	var prefix []*contentstream.ContentStreamOperation
	switch operand {
	case "'":
		if len(op.Params) < 1 {
			return fmt.Errorf("' operation with %d params", len(op.Params))
		}
		prefix = []*contentstream.ContentStreamOperation{{Operand: "T*"}}
		op.Params = []core.PdfObject{op.Params[0]}
	case "\"":
		if len(op.Params) < 3 {
			return fmt.Errorf("\" operation with %d params", len(op.Params))
		}
		tw, tc := op.Params[0], op.Params[1]
		prefix = []*contentstream.ContentStreamOperation{{Params: []core.PdfObject{tw}, Operand: "Tw"}, {Params: []core.PdfObject{tc}, Operand: "Tc"}, {Operand: "T*"}}
		op.Params = []core.PdfObject{op.Params[2]}
	default:
		return fmt.Errorf("unsupported operand %q", operand)
	}
	op.Operand = "Tj"
	out := make(contentstream.ContentStreamOperations, 0, len(*ops)+len(prefix))
	out = append(out, (*ops)[:idx]...)
	out = append(out, prefix...)
	out = append(out, (*ops)[idx:]...)
	*ops = out
	return nil
}

func (t *editTarget) editKeys() []string {
	keys := make([]string, 0, len(t.editedOps))
	for i, op := range *t.ops {
		if t.editedOps[op] {
			keys = append(keys, fmt.Sprintf("%d:%s", i, operationString(op)))
		}
	}
	return keys
}

func decodeOperand(obj core.PdfObject, font *model.PdfFont) ([]textencoding.CharCode, []string, error) {
	b, ok := core.GetStringBytes(obj)
	if !ok {
		return nil, nil, core.ErrTypeError
	}
	codes := font.BytesToCharcodes(b)
	strs, numChars, numMisses := font.CharcodesToStrings(codes, "")
	if numMisses > 0 {
		common.Log.Debug("decodeOperand: numChars=%d numMisses=%d", numChars, numMisses)
	}
	return codes, strs, nil
}

// Write writes the content of `re.creator` to writer of type io.Writer interface.
func (r *Redactor) Write(writer io.Writer) error { return r.creator.Write(writer) }

type formPaint struct {
	page   int
	res    *model.PdfPageResources
	target *editTarget
}

func (c *pageFormClones) unreferencedReason() string {
	var reasons []string
	if c.tooDeep {
		reasons = append(reasons, "forms nested deeper than the supported limit")
	}
	if c.cyclic {
		reasons = append(reasons, "forms referencing one another")
	}
	if len(reasons) == 0 {
		return "it is not referenced from the page resources (painted from outside the page, e.g. an annotation appearance)"
	}
	return "it is not referenced from the page resources other than possibly through " + strings.Join(reasons, " or ") + ", which were not followed"
}

func (r *Redactor) allTerms() []RedactionTerm {
	out := make([]RedactionTerm, 0, len(r.opts.Terms)+len(r.extraTerms))
	out = append(out, r.opts.Terms...)
	out = append(out, r.extraTerms...)
	return out
}

type replacement struct {
	text string
	adj  float64
	off  int
}

func pruneNestedTargets(ts []*targetMap) {
	for i, a := range ts {
		for j, b := range ts {
			if i != j {
				nested, idx := nestedRanges(*a, *b)
				if nested {
					removeRanges(b, idx)
				}
			}
		}
	}
}

func neighbourIndex(marks *extractor.TextMarkArray, i int) int {
	elems := marks.Elements()
	prev := i - 1
	next := i + 1
	res := -1
	if prev >= 0 {
		pm := elems[prev]
		objStr := pm.ObjString
		n := len(objStr)
		idx := pm.Index
		if idx+1 < n {
			res = prev
			return res
		}
	}
	if next < len(elems) {
		nm := elems[next]
		nObjStr := nm.ObjString
		if nObjStr[0] != nm.Text {
			res = next
			return res
		}
	}
	return res
}

type pageFormClones struct {
	clones                     map[*core.PdfObjectStream]*core.PdfObjectStream
	isClone                    map[*core.PdfObjectStream]bool
	privateDicts               map[*core.PdfObjectDictionary]bool
	noRewire                   map[formSearch]bool
	rewired                    map[*core.PdfObjectStream]bool
	walks                      int
	tooDeep, cyclic, walkLimit bool
}

func (c *pageFormClones) current(s *core.PdfObjectStream) *core.PdfObjectStream {
	if cl, ok := c.clones[s]; ok {
		return cl
	}
	return s
}

func normalizeNewlines(s string) (string, [][]int) {
	re := regexp.MustCompile("\\n")
	locs := re.FindAllStringIndex(s, -1)
	out := re.ReplaceAllString(s, " ")
	return out, locs
}

func verifyRedaction(terms []RedactionTerm, ops *contentstream.ContentStreamOperations, res *model.PdfPageResources) ([]editFailure, error) {
	_, matchSets, _, err := findMatches(terms, ops, res)
	if err != nil {
		return nil, err
	}
	var failures []editFailure
	for _, ms := range matchSets {
		for _, m := range ms {
			failures = append(failures, editFailure{term: m.text, reason: "term is still extractable after redaction"})
		}
	}
	return failures, nil
}

func findPlaceholders(text string, spans []localSpanMarks) ([]placeHolders, error) {
	prev := ""
	out := []placeHolders{}
	for i, span := range spans {
		marks := span.marks
		term := span.term
		spanText := markArrayText(marks)
		if spanText != prev {
			var offsets []int
			if i == 0 && term != spanText {
				idx := strings.Index(text, spanText)
				offsets = []int{idx}
			} else if i == len(spans)-1 {
				idx := strings.LastIndex(text, spanText)
				offsets = []int{idx}
			} else {
				offsets = indexAll(text, spanText)
			}
			if len(offsets) == 0 {
				return nil, fmt.Errorf("span %q not found in text object of term %q", spanText, term)
			}
			adj, err := spanAdjustment(marks, offsets[0], offsets[0]+len(spanText))
			if err != nil {
				return nil, err
			}
			ph := placeHolders{offsets: offsets, text: spanText, adj: adj}
			out = append(out, ph)
		}
		prev = spanText
	}
	return out, nil
}

// RedactionTerm holds the regexp pattern and the replacement string for the redaction process.
type RedactionTerm struct{ Pattern *regexp.Regexp }

// New instantiates a Redactor object with given PdfReader and `regex` pattern.
func New(reader *model.PdfReader, opts *RedactionOptions, rectProps *RectangleProps) *Redactor {
	if rectProps == nil {
		rectProps = RedactRectanglePropsNew()
	}
	return &Redactor{reader: reader, opts: opts, creator: creator.New(), rectProps: rectProps, lookup: findTextOperation}
}

// Error implements the error interface.
func (e *IncompleteRedactionError) Error() string {
	parts := make([]string, 0, len(e.Failures))
	for _, f := range e.Failures {
		parts = append(parts, fmt.Sprintf("page %d: %q (%s)", f.Page, f.Term, f.Reason))
	}
	return fmt.Sprintf("redaction incomplete: %d matched term(s) could not be physically removed: %s", len(e.Failures), strings.Join(parts, "; "))
}
