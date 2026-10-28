// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package redactor

import (
	"fmt"
	"slices"
	"strings"

	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/extractor"
	"github.com/OpenMellon/openpdf/model"
)

type localSpanMarks struct {
	marks *extractor.TextMarkArray
	idx   int
	term  string
}

func findMatches(terms []RedactionTerm, ops *contentstream.ContentStreamOperations, res *model.PdfPageResources) (*extractor.PageText, [][]*matchedIndex, [][]int, error) {
	ex, err := extractor.NewFromContents(ops.String(), res)
	if err != nil {
		return nil, nil, nil, err
	}
	pageText, _, _, err := ex.ExtractPageText()
	if err != nil {
		return nil, nil, nil, err
	}
	text, newlines := normalizeNewlines(pageText.Text())
	all := make([][]*matchedIndex, 0, len(terms))
	for _, term := range terms {
		matcher, matchErr := newRegexMatcher(term)
		if matchErr != nil {
			return nil, nil, nil, matchErr
		}
		found, matchErr := matcher.match(text)
		if matchErr != nil {
			return nil, nil, nil, matchErr
		}
		all = append(all, found)
	}
	return pageText, all, newlines, nil
}

type targetMap struct {
	text   string
	ranges [][]int
}

func rewriteFormStreams(edits []pageFormEdits) []RedactionFailure {
	var forms []*core.PdfObjectStream
	byForm := make(map[*core.PdfObjectStream][]formPaint)
	var failures []RedactionFailure
	for _, pe := range edits {
		for _, target := range pe.targets {
			stream, ok := core.GetStream(target.form.GetContainingPdfObject())
			if !ok {
				if target.edited {
					failures = append(failures, failuresForPaint(formPaint{page: pe.page, target: target}, fmt.Sprintf("form XObject %q has no stream primitive; the matched text remains in the form", target.name))...)
				}
				continue
			}
			if _, seen := byForm[stream]; !seen {
				forms = append(forms, stream)
			}
			byForm[stream] = append(byForm[stream], formPaint{page: pe.page, res: pe.res, target: target})
		}
	}
	type pageSpecific struct {
		form   *core.PdfObjectStream
		paints []formPaint
	}
	var specific []pageSpecific
	for _, form := range forms {
		paints := byForm[form]
		var edited []formPaint
		for _, p := range paints {
			if p.target.edited {
				edited = append(edited, p)
			}
		}
		if len(edited) == 0 {
			continue
		}
		if len(edited) == len(paints) && sameEdits(edited) {
			if err := reencodeStream(form, edited[0].target.ops); err != nil {
				for _, p := range edited {
					failures = append(failures, failuresForPaint(p, fmt.Sprintf("failed to re-encode form XObject %q: %v; the matched text remains in the form", p.target.name, err))...)
				}
			}
			continue
		}
		specific = append(specific, pageSpecific{form: form, paints: edited})
	}
	clonesByPage := map[int]*pageFormClones{}
	for _, s := range specific {
		for _, p := range s.paints {
			clones, ok := clonesByPage[p.page]
			if !ok {
				clones = newPageFormClones()
				clonesByPage[p.page] = clones
			}
			clone := clones.cloneOf(s.form)
			if err := reencodeStream(clone, p.target.ops); err != nil {
				failures = append(failures, failuresForPaint(p, fmt.Sprintf("failed to re-encode form XObject %q: %v; the matched text remains in the form", p.target.name, err))...)
				continue
			}
			if reason, ok := clones.rewireFormReferences(p.res, s.form, clone); !ok {
				failures = append(failures, failuresForPaint(p, fmt.Sprintf("form XObject %q needs a page specific edit but %s; the matched text remains in the form", p.target.name, reason))...)
			}
		}
	}
	return failures
}

func nestedRanges(a, b targetMap) (bool, []int) {
	contains := strings.Contains(a.text, b.text)
	var nested []int
	for _, ar := range a.ranges {
		for i, br := range b.ranges {
			if br[0] >= ar[0] && br[1] <= ar[1] {
				nested = append(nested, i)
			}
		}
	}
	return contains, nested
}

func (c *pageFormClones) privateDict(d *core.PdfObjectDictionary) *core.PdfObjectDictionary {
	if c.privateDicts[d] {
		return d
	}
	dup := core.MakeDict()
	for _, k := range d.Keys() {
		dup.Set(k, d.Get(k))
	}
	c.privateDicts[dup] = true
	return dup
}

func sameEdits(paints []formPaint) bool {
	first := paints[0].target
	keys := first.editKeys()
	for _, p := range paints[1:] {
		t := p.target
		if len(*t.ops) != len(*first.ops) {
			return false
		}
		if !slices.Equal(t.editKeys(), keys) {
			return false
		}
	}
	return true
}

type matchedBBox struct {
	box  model.PdfRectangle
	text string
}

// RedactionFailure describes a matched term occurrence that could not be
// verified as physically removed from the document content.
type RedactionFailure struct {
	// Page is the 1-based number of the page the term was matched on.
	Page int

	// Term is the matched text. It is empty when a whole page could not be
	// re-verified; Reason then carries the verification error.
	Term string

	// Reason describes why the removal failed or could not be verified.
	Reason string
}

func rewriteTj(op *contentstream.ContentStreamOperation, obj core.PdfObject, spans []localSpanMarks) error {
	var arr *core.PdfObjectArray
	byTerm, terms := groupByTerm(spans)
	if len(terms) == 1 {
		term := terms[0]
		group := byTerm[term]
		if len(group) == 1 {
			span := group[0]
			marks := span.marks
			font := spanFont(marks)
			text, err := decodeText(obj, font)
			if err != nil {
				return err
			}
			parts, err := splitAroundMatch(span, marks, font, text, term)
			if err != nil {
				return err
			}
			arr = core.MakeArray(parts...)
		} else {
			marks := group[0].marks
			font := spanFont(marks)
			text, err := decodeText(obj, font)
			if err != nil {
				return err
			}
			phs, err := findPlaceholders(text, group)
			if err != nil {
				return err
			}
			repls := placeholdersToReplacements(phs)
			items := buildTJArray(text, repls, font)
			arr = core.MakeArray(items...)
		}
	} else if len(terms) > 1 {
		first := spans[0]
		marks := first.marks
		_, idx := firstDirectObject(marks)
		mark := marks.Elements()[idx]
		font := mark.Font
		text, err := decodeText(obj, font)
		if err != nil {
			return err
		}
		phs, err := findPlaceholders(text, spans)
		if err != nil {
			return err
		}
		repls := placeholdersToReplacements(phs)
		items := buildTJArray(text, repls, font)
		arr = core.MakeArray(items...)
	}
	op.Params[0] = arr
	op.Operand = "TJ"
	return nil
}

func buildTJArray(text string, repls []replacement, font *model.PdfFont) []core.PdfObject {
	out := []core.PdfObject{}
	pos := 0
	rest := text
	for i, r := range repls {
		off := r.off
		adj := r.adj
		matched := r.text
		adjObj := core.MakeFloat(adj)
		if pos > off || off == -1 {
			continue
		}
		before := text[pos:off]
		enc := encodeForFont(before, font)
		str := core.MakeStringFromBytes(enc)
		out = append(out, str)
		out = append(out, adjObj)
		end := off + len(matched)
		rest = text[end:]
		pos = end
		if i == len(repls)-1 {
			enc = encodeForFont(rest, font)
			str = core.MakeStringFromBytes(enc)
			out = append(out, str)
		}
	}
	return out
}

func spanAdjustment(marks *extractor.TextMarkArray, start, end int) (float64, error) {
	obj, idx := firstDirectObject(marks)
	if obj == nil {
		return 0.0, fmt.Errorf("span marks carry no text object. spanMarks=%s", marks)
	}
	if start < 0 || start >= end {
		return 0.0, fmt.Errorf("span %q not found in text object %v", markArrayText(marks), obj)
	}
	mark := marks.Elements()[idx]
	codes, strs, err := decodeOperand(obj, mark.Font)
	if err != nil {
		return 0.0, err
	}
	total := 0.0
	pos := 0
	for i, s := range strs {
		next := pos + len(s)
		if pos < end && next > start {
			adv, err := glyphAdvance(mark.Font, codes[i], s, mark)
			if err != nil {
				return 0.0, err
			}
			total += adv
		}
		pos = next
	}
	return tjAdjustment(total, mark.Tfs, mark.Th)
}

func indexAll(s, sub string) []int {
	if len(sub) == 0 {
		return nil
	}
	var out []int
	for i := 0; i < len(s); {
		j := strings.Index(s[i:], sub)
		if j < 0 {
			return out
		}
		out = append(out, i+j)
		i += j + len(sub)
	}
	return out
}

func newRegexMatcher(term RedactionTerm) (*regexMatcher, error) {
	return &regexMatcher{term: term}, nil
}

func spanFont(marks *extractor.TextMarkArray) *model.PdfFont {
	_, idx := firstDirectObject(marks)
	mark := marks.Elements()[idx]
	font := mark.Font
	return font
}

func splitAroundMatch(span localSpanMarks, marks *extractor.TextMarkArray, font *model.PdfFont, text, term string) ([]core.PdfObject, error) {
	spanText := markArrayText(marks)
	textLen := len(text)
	spanLen := len(spanText)
	start := -1
	if spanText != term {
		idx := span.idx
		if idx == 0 {
			start = strings.LastIndex(text, spanText)
		} else {
			start = strings.Index(text, spanText)
		}
	} else {
		start = strings.Index(text, spanText)
	}
	end := start + spanLen
	Tj, err := spanAdjustment(marks, start, end)
	if err != nil {
		return nil, err
	}
	adj := core.MakeFloat(Tj)
	out := []core.PdfObject{}
	if start == 0 && end == textLen {
		out = append(out, adj)
	} else if start == 0 && end < textLen {
		enc := encodeForFont(text[end:], font)
		str := core.MakeStringFromBytes(enc)
		out = append(out, adj, str)
	} else if start > 0 && end >= textLen {
		enc := encodeForFont(text[:start], font)
		str := core.MakeStringFromBytes(enc)
		out = append(out, str, adj)
	} else if start > 0 && end < textLen {
		pre := encodeForFont(text[:start], font)
		post := encodeForFont(text[end:], font)
		preStr := core.MakeStringFromBytes(pre)
		postStr := core.MakeString(string(post))
		out = append(out, preStr, adj, postStr)
	}
	return out, nil
}

func removeRanges(t *targetMap, drop []int) {
	var kept [][]int
	for i, r := range t.ranges {
		if slices.Contains(drop, i) {
			continue
		}
		kept = append(kept, r)
	}
	t.ranges = kept
}

type targetOpLookup func(*contentstream.ContentStreamOperations, core.PdfObject) (*contentstream.ContentStreamOperation, int, bool)

func splitOverlaps(ms []*matchedIndex, ranges [][]int) []*matchedIndex {
	out := []*matchedIndex{}
	for _, m := range ms {
		found, contained := containedRanges(m, ranges)
		if found {
			parts := splitAroundRanges(m, contained)
			out = append(out, parts...)
		} else {
			out = append(out, m)
		}
	}
	return out
}

func locateMatch(rng []int, marks *extractor.TextMarkArray, text string) (*extractor.TextMarkArray, matchedBBox, error) {
	box := matchedBBox{}
	start := rng[0]
	end := rng[1]
	lead := len(text) - len(strings.TrimLeft(text, " "))
	trail := len(text) - len(strings.TrimRight(text, " \n"))
	start = start + lead
	end = end - trail
	text = strings.Trim(text, " \n")
	sub, err := marks.RangeOffset(start, end)
	if err != nil {
		return nil, box, err
	}
	bbox, ok := sub.BBox()
	if !ok {
		return nil, box, fmt.Errorf("spanMarks.BBox has no bounding box. spanMarks=%s", sub)
	}
	box = matchedBBox{text: text, box: bbox}
	return sub, box, nil
}

func groupByTerm(spans []localSpanMarks) (map[string][]localSpanMarks, []string) {
	byTerm := make(map[string][]localSpanMarks)
	order := []string{}
	for _, s := range spans {
		term := s.term
		if prev, ok := byTerm[term]; ok {
			byTerm[term] = append(prev, s)
		} else {
			byTerm[term] = []localSpanMarks{s}
			order = append(order, term)
		}
	}
	return byTerm, order
}
