// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package redactor

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/creator"
	"github.com/OpenMellon/openpdf/extractor"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model"
)

func (c *pageFormClones) rewireXObjects(xobjects *core.PdfObjectDictionary, orig, clone *core.PdfObjectStream, depth int, visiting map[*core.PdfObjectStream]bool) (*core.PdfObjectDictionary, bool) {
	if xobjects == nil {
		return nil, false
	}
	if depth >= maxFormDepth {
		c.tooDeep = true
		return xobjects, false
	}
	c.walks++
	if c.walks > maxFormWalks {
		c.walkLimit = true
		return xobjects, false
	}
	out := xobjects
	changed := false
	for _, key := range xobjects.Keys() {
		form, ok := core.GetStream(xobjects.Get(key))
		if !ok {
			continue
		}
		if form == orig {
			out = c.privateDict(out)
			out.Set(key, clone)
			changed = true
			continue
		}
		if form == clone || c.rewired[form] {
			if existing := c.cloneOf(form); existing != form {
				out = c.privateDict(out)
				out.Set(key, existing)
			}
			changed = true
			continue
		}
		if visiting[form] {
			c.cyclic = true
			continue
		}
		if c.noRewire[formSearch{form: form, depth: depth}] {
			continue
		}
		cur := c.current(form)
		res, ok := core.GetDict(cur.Get("Resources"))
		if !ok {
			continue
		}
		subXObjects, ok := core.GetDict(res.Get("XObject"))
		if !ok {
			continue
		}
		visiting[form] = true
		newSub, subChanged := c.rewireXObjects(subXObjects, orig, clone, depth+1, visiting)
		delete(visiting, form)
		if !subChanged {
			c.noRewire[formSearch{form: form, depth: depth}] = true
			continue
		}
		formClone := c.cloneOf(cur)
		newRes := c.privateDict(res)
		newRes.Set("XObject", newSub)
		formClone.Set("Resources", newRes)
		c.rewired[form] = true
		c.rewired[formClone] = true
		out = c.privateDict(out)
		out.Set(key, formClone)
		changed = true
	}
	return out, changed
}

func markArrayText(marks *extractor.TextMarkArray) string {
	text := ""
	for _, mark := range marks.Elements() {
		text += mark.Text
	}
	return text
}

type regexMatcher struct{ term RedactionTerm }

func groupByText(matches []*matchedIndex) []*targetMap {
	byText := make(map[string][][]int)
	targets := []*targetMap{}
	for _, m := range matches {
		text := m.text
		span := []int{m.start, m.end}
		if prev, found := byText[text]; found {
			byText[text] = append(prev, span)
		} else {
			byText[text] = [][]int{span}
		}
	}
	for text, ranges := range byText {
		t := &targetMap{text: text, ranges: ranges}
		targets = append(targets, t)
	}
	return targets
}

func (r *Redactor) redactPage(ops *contentstream.ContentStreamOperations, res *model.PdfPageResources) ([]matchedBBox, *contentstream.ContentStreamOperations, []editFailure, []*editTarget, error) {
	pageText, matchSets, newlines, err := findMatches(r.allTerms(), ops, res)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	ops = pageText.GetContentStreamOps()
	marks := pageText.Marks()
	boxes := []matchedBBox{}
	bySource := make(map[core.PdfObject][]localSpanMarks)
	targetMaps := []*targetMap{}
	for _, ms := range matchSets {
		ms = splitOverlaps(ms, newlines)
		ts := groupByText(ms)
		targetMaps = append(targetMaps, ts...)
	}
	pruneNestedTargets(targetMaps)
	for _, tm := range targetMaps {
		text := tm.text
		ranges := tm.ranges
		targetBoxes := []matchedBBox{}
		for _, rng := range ranges {
			spanMarks, box, matchErr := locateMatch(rng, marks, text)
			if matchErr != nil {
				return nil, nil, nil, nil, matchErr
			}
			parts := splitByTextObject(spanMarks)
			for i, part := range parts {
				span := localSpanMarks{marks: part, idx: i, term: text}
				obj, _ := firstDirectObject(part)
				if existing, ok := bySource[obj]; ok {
					bySource[obj] = append(existing, span)
				} else {
					bySource[obj] = []localSpanMarks{span}
				}
			}
			targetBoxes = append(targetBoxes, box)
		}
		boxes = append(boxes, targetBoxes...)
	}
	editTargets := []*editTarget{{ops: ops}}
	for _, form := range pageText.FormContents() {
		editTargets = append(editTargets, &editTarget{ops: form.Operations, form: form.Form, name: form.Name})
	}
	failures := applyEdits(editTargets, bySource, r.lookup)
	var formTargets []*editTarget
	for _, t := range editTargets {
		if t.form != nil {
			formTargets = append(formTargets, t)
		}
	}
	sort.Slice(failures, func(i, j int) bool {
		if failures[i].term != failures[j].term {
			return failures[i].term < failures[j].term
		}
		return failures[i].reason < failures[j].reason
	})
	return boxes, ops, failures, formTargets, nil
}

const maxFormWalks = 100000

func encodeForFont(s string, font *model.PdfFont) []byte {
	codes, numMisses := font.StringToCharcodeBytes(s)
	if numMisses != 0 {
		common.Log.Debug("WARN: some runes could not be encoded.\n\t%s -> %v", s, codes)
	}
	return codes
}

var StandardRegexPatterns = []string{RegexSSN, RegexCreditCard, RegexEmail, RegexPhone, RegexIPv4, RegexIPv6, RegexPassport, RegexDriversLicense, RegexDate, RegexPostalCode, RegexMAC, RegexBankRouting, RegexIBAN, RegexAWSAccessKeyID, RegexAWSSecretAccessKey, RegexGoogleAPIKey, RegexJWT, RegexCardExpirationDate, RegexSWIFTCode, RegexStripeAPIKey}

func failuresForPaint(paint formPaint, reason string) []RedactionFailure {
	if len(paint.target.terms) == 0 {
		return []RedactionFailure{{Page: paint.page, Reason: reason}}
	}
	out := make([]RedactionFailure, 0, len(paint.target.terms))
	for _, term := range paint.target.terms {
		out = append(out, RedactionFailure{Page: paint.page, Term: term, Reason: reason})
	}
	return out
}

const (
	RegexSSN                = "(?i)\\b\\d{3}-\\d{2}-\\d{4}\\b"
	RegexCreditCard         = "\\b(?:\\d[ -]*?){13,16}\\b"
	RegexEmail              = "\\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\\.[A-Za-z]{2,}\\b"
	RegexPhone              = "(?:\\+1[-.\\s]?)?\\(?\\d{3}\\)?[-.\\s]?\\d{3}[-.\\s]?\\d{4}"
	RegexIPv4               = "\\b(?:(?:25[0-5]|2[0-4]\\d|1\\d{2}|[1-9]?\\d)\\.){3}(?:25[0-5]|2[0-4]\\d|1\\d{2}|[1-9]?\\d)\\b"
	RegexIPv6               = "\\b([0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}\\b"
	RegexPassport           = "\\b[A-Z0-9]{6,9}\\b"
	RegexDriversLicense     = "\\b[A-Z0-9]{5,15}\\b"
	RegexDate               = "\\b(?:\\d{1,2}[/.-]\\d{1,2}[/.-]\\d{2,4}|\\d{4}[-/]\\d{2}[-/]\\d{2})\\b"
	RegexPostalCode         = "\\b\\d{5}(?:-\\d{4})?\\b"
	RegexMAC                = "\\b(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}\\b"
	RegexBankRouting        = "\\b\\d{9}\\b"
	RegexIBAN               = "\\b[A-Z]{2}\\d{2}[A-Z0-9]{11,30}\\b"
	RegexAWSAccessKeyID     = "\\bAKIA[0-9A-Z]{16}\\b"
	RegexAWSSecretAccessKey = "\\b[0-9a-zA-Z/+]{40}\\b"
	RegexGoogleAPIKey       = "\\bAIza[0-9A-Za-z\\-_]{35}\\b"
	RegexJWT                = "\\beyJ[A-Za-z0-9_\\-]+\\.eyJ[A-Za-z0-9_\\-]+\\.?[A-Za-z0-9_\\-]*\\b"
	RegexCardExpirationDate = "\\b(0[1-9]|1[0-2])/(?:\\d{2}|\\d{4})\\b"
	RegexSWIFTCode          = "\\b[A-Z]{4}[A-Z]{2}[A-Z0-9]{2}([A-Z0-9]{3})?\\b"
	RegexStripeAPIKey       = "sk_live_[0-9a-zA-Z]{24}"
)

// RedactRectanglePropsNew return a new pointer to a default RectangleProps object.
func RedactRectanglePropsNew() *RectangleProps {
	return &RectangleProps{FillColor: creator.ColorBlack, BorderWidth: 0.0, FillOpacity: 1.0}
}

func glyphAdvance(font *model.PdfFont, code textencoding.CharCode, ch string, mark extractor.TextMark) (float64, error) {
	metrics, ok := font.GetCharMetrics(code)
	if !ok {
		return 0.0, fmt.Errorf("no glyph metrics for code %d in font %s", code, font)
	}
	width := metrics.Wx / 1000
	if font.Subtype() == "Type3" {
		width = metrics.Wx
	}
	wordSpace := 0.0
	if runes := []rune(ch); len(runes) == 1 && runes[0] == ' ' {
		wordSpace = mark.Tw
	}
	return (width*mark.Tfs + mark.Tc + wordSpace) * mark.Th / 100, nil
}

// RectangleProps defines properties of the redaction rectangle to be drawn.
type RectangleProps struct {
	FillColor   creator.Color
	BorderWidth float64
	FillOpacity float64
}

func firstDirectObject(marks *extractor.TextMarkArray) (core.PdfObject, int) {
	var obj core.PdfObject
	idx := -1
	for i, m := range marks.Elements() {
		obj = m.DirectObject
		idx = i
		if obj != nil {
			break
		}
	}
	return obj, idx
}

func containedRanges(m *matchedIndex, ranges [][]int) (bool, [][]int) {
	contained := [][]int{}
	for _, r := range ranges {
		if m.start < r[0] && m.end > r[1] {
			contained = append(contained, r)
		}
	}
	return len(contained) > 0, contained
}

func splitByTextObject(marks *extractor.TextMarkArray) []*extractor.TextMarkArray {
	elems := marks.Elements()
	n := len(elems)
	var prevObj core.PdfObject
	groups := []*extractor.TextMarkArray{}
	cur := &extractor.TextMarkArray{}
	idx := -1
	for i, el := range elems {
		obj := el.DirectObject
		idx = el.Index
		if obj == nil {
			adj := neighbourIndex(marks, i)
			if prevObj != nil {
				if adj == -1 || adj > i {
					groups = append(groups, cur)
					cur = &extractor.TextMarkArray{}
				}
			}
		} else if prevObj == nil {
			if idx == 0 && i > 0 {
				groups = append(groups, cur)
				cur = &extractor.TextMarkArray{}
			}
		} else {
			if obj != prevObj {
				groups = append(groups, cur)
				cur = &extractor.TextMarkArray{}
			}
		}
		prevObj = obj
		cur.Append(el)
		if i == (n - 1) {
			groups = append(groups, cur)
		}
	}
	return groups
}

func placeholdersToReplacements(phs []placeHolders) []replacement {
	out := []replacement{}
	for _, ph := range phs {
		offsets := ph.offsets
		text := ph.text
		adj := ph.adj
		for _, off := range offsets {
			r := replacement{text: text, adj: adj, off: off}
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].off < out[j].off })
	return out
}

func (m *regexMatcher) match(s string) ([]*matchedIndex, error) {
	pattern := m.term.Pattern
	if pattern == nil {
		return nil, errors.New("no pattern compiled")
	}
	var (
		locs = pattern.FindAllStringIndex(s, -1)
		out  []*matchedIndex
	)
	for _, loc := range locs {
		out = append(out, &matchedIndex{start: loc[0], end: loc[1], text: s[loc[0]:loc[1]]})
	}
	return out, nil
}

func shallowCloneStream(src *core.PdfObjectStream) *core.PdfObjectStream {
	dst := &core.PdfObjectStream{}
	dst.PdfObjectDictionary = core.MakeDict()
	for _, key := range src.PdfObjectDictionary.Keys() {
		dst.PdfObjectDictionary.Set(key, src.PdfObjectDictionary.Get(key))
	}
	dst.Stream = src.Stream
	return dst
}

func newPageFormClones() *pageFormClones {
	return &pageFormClones{clones: map[*core.PdfObjectStream]*core.PdfObjectStream{}, isClone: map[*core.PdfObjectStream]bool{}, privateDicts: map[*core.PdfObjectDictionary]bool{}, noRewire: map[formSearch]bool{}, rewired: map[*core.PdfObjectStream]bool{}}
}

// RedactionOptions is a collection of RedactionTerm objects.
type RedactionOptions struct {
	Terms               []RedactionTerm
	UseStandardPatterns bool

	// DisableVerification skips the closing verification pass, which
	// re-extracts every page that had matches and reports terms that are still
	// extractable. Disabling it saves roughly one extraction pass per matched
	// page but removes that safety check; failures detected while editing are
	// still reported. Leaving verification enabled is strongly recommended.
	DisableVerification bool
}
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

// Redact executes the redact operation on a pdf file and updates the content streams of all pages of the file.
// Matched text is physically removed from the page content streams and from the content streams of the form
// XObjects it is located in, and the matched areas are covered with redaction rectangles.
// Matching, removal and the closing verification all operate on the text extractable from the page and
// form XObject content streams; text in other sources (such as annotation appearance streams) is not
// matched, removed or covered.
// If one or more matches could not be physically removed, Redact returns an *IncompleteRedactionError
// listing them; the rest of the document is still processed and can be written, but it must not be
// treated as safely redacted.
func (r *Redactor) Redact() error {
	numPages, err := r.reader.GetNumPages()
	if err != nil {
		return fmt.Errorf("failed to get the number of Pages")
	}
	if r.opts.UseStandardPatterns {
		for _, pattern := range StandardRegexPatterns {
			re, compileErr := regexp.Compile(pattern)
			if compileErr != nil {
				return compileErr
			}
			r.extraTerms = append(r.extraTerms, RedactionTerm{Pattern: re})
		}
	}
	fillColor := r.rectProps.FillColor
	borderWidth := r.rectProps.BorderWidth
	fillOpacity := r.rectProps.FillOpacity
	type pageVerification struct {
		page int
		ops  *contentstream.ContentStreamOperations
		res  *model.PdfPageResources
	}
	var failures []RedactionFailure
	var formEdits []pageFormEdits
	var verifs []pageVerification
	for pageNum := 1; pageNum <= numPages; pageNum++ {
		page, err := r.reader.GetPage(pageNum)
		if err != nil {
			return err
		}
		ex, err := extractor.New(page)
		if err != nil {
			return err
		}
		pageText, _, _, err := ex.ExtractPageText()
		if err != nil {
			return err
		}
		ops := pageText.GetContentStreamOps()
		boxes, newOps, editFailures, formTargets, err := r.redactPage(ops, page.Resources)
		if err != nil {
			return err
		}
		if len(formTargets) > 0 {
			formEdits = append(formEdits, pageFormEdits{page: pageNum, res: page.Resources, targets: formTargets})
		}
		for _, f := range editFailures {
			failures = append(failures, RedactionFailure{Page: pageNum, Term: f.term, Reason: f.reason})
		}
		if len(boxes) > 0 && !r.opts.DisableVerification {
			verifs = append(verifs, pageVerification{page: pageNum, ops: newOps, res: page.Resources})
		}
		nop := contentstream.ContentStreamOperation{Operand: "n"}
		*newOps = append(*newOps, &nop)
		if setErr := page.SetContentStreams([]string{newOps.String()}, core.NewFlateEncoder()); setErr != nil {
			return setErr
		}
		mediaBox, err := page.GetMediaBox()
		if err != nil {
			return err
		}
		if page.MediaBox == nil {
			page.MediaBox = mediaBox
		}
		if addErr := r.creator.AddPage(page); addErr != nil {
			return addErr
		}
		sort.Slice(boxes, func(i, j int) bool { return boxes[i].text < boxes[j].text })
		pageHeight := mediaBox.Ury
		for _, box := range boxes {
			bb := box.box
			rect := r.creator.NewRectangle(bb.Llx, pageHeight-bb.Lly, bb.Urx-bb.Llx, -(bb.Ury - bb.Lly))
			rect.SetFillColor(fillColor)
			rect.SetBorderWidth(borderWidth)
			rect.SetFillOpacity(fillOpacity)
			if drawErr := r.creator.Draw(rect); drawErr != nil {
				return drawErr
			}
		}
	}
	r.creator.SetOutlineTree(r.reader.GetOutlineTree())
	failures = append(failures, rewriteFormStreams(formEdits)...)
	if !r.opts.DisableVerification {
		key := func(page int, term string) string { return fmt.Sprintf("%d\x00%s", page, term) }
		seen := make(map[string]bool, len(failures))
		for _, f := range failures {
			seen[key(f.Page, f.Term)] = true
		}
		terms := r.allTerms()
		for _, v := range verifs {
			found, verifyErr := verifyRedaction(terms, v.ops, v.res)
			if verifyErr != nil {
				failures = append(failures, RedactionFailure{Page: v.page, Reason: fmt.Sprintf("could not verify the redaction: %v", verifyErr)})
				continue
			}
			for _, fail := range found {
				k := key(v.page, fail.term)
				if seen[k] {
					continue
				}
				seen[k] = true
				failures = append(failures, RedactionFailure{Page: v.page, Term: fail.term, Reason: fail.reason})
			}
		}
	}
	if len(failures) > 0 {
		return &IncompleteRedactionError{Failures: failures}
	}
	return nil
}

func tjAdjustment(advance, fontSize, hScale float64) (float64, error) {
	scaled := fontSize * hScale / 100
	if scaled == 0 {
		if advance != 0 {
			return 0.0, fmt.Errorf("advance %g of zero-size text cannot be reproduced by a TJ adjustment", advance)
		}
		return 0.0, nil
	}
	return -1000 * advance / scaled, nil
}

func decodeText(obj core.PdfObject, font *model.PdfFont) (string, error) {
	_, strs, err := decodeOperand(obj, font)
	if err != nil {
		return "", err
	}
	return strings.Join(strs, ""), nil
}

// IncompleteRedactionError is returned by Redact when one or more matched
// terms could not be physically removed from the underlying content streams.
// The original matches are still covered by redaction rectangles and the
// remaining matches are processed, but the text of the reported failures is
// (or may be) still present in the output file, so the result must not be
// treated as safely redacted.
type IncompleteRedactionError struct{ Failures []RedactionFailure }

const maxFormDepth = 20

func reencodeStream(s *core.PdfObjectStream, ops *contentstream.ContentStreamOperations) error {
	enc := core.NewFlateEncoder()
	data, err := enc.EncodeBytes([]byte(ops.String()))
	if err != nil {
		return err
	}
	s.Stream = data
	dict := enc.MakeStreamDict()
	s.PdfObjectDictionary.Set("Filter", dict.Get("Filter"))
	if parms := dict.Get("DecodeParms"); parms != nil {
		s.PdfObjectDictionary.Set("DecodeParms", parms)
	} else {
		s.PdfObjectDictionary.Remove("DecodeParms")
	}
	s.PdfObjectDictionary.Set("Length", core.MakeInteger(int64(len(data))))
	return nil
}

func failuresForSpans(spans []localSpanMarks, reason string) []editFailure {
	_, terms := groupByTerm(spans)
	out := make([]editFailure, 0, len(terms))
	for _, term := range terms {
		out = append(out, editFailure{term: term, reason: reason})
	}
	return out
}

type editTarget struct {
	ops       *contentstream.ContentStreamOperations
	form      *model.XObjectForm
	name      string
	edited    bool
	terms     []string
	editedOps map[*contentstream.ContentStreamOperation]bool
}

func findTextOperation(ops *contentstream.ContentStreamOperations, PdfObj core.PdfObject) (*contentstream.ContentStreamOperation, int, bool) {
	for i, op := range *ops {
		operand := op.Operand
		if operand == "Tj" {
			if len(op.Params) < 1 {
				continue
			}
			obj := core.TraceToDirectObject(op.Params[0])
			if obj == PdfObj {
				return op, i, true
			}
		} else if operand == "TJ" {
			if len(op.Params) < 1 {
				continue
			}
			arr, ok := core.GetArray(op.Params[0])
			if !ok {
				continue
			}
			for _, el := range arr.Elements() {
				if el == PdfObj {
					return op, i, true
				}
			}
		} else if operand == "\"" {
			if len(op.Params) < 3 {
				continue
			}
			obj := core.TraceToDirectObject(op.Params[2])
			if obj == PdfObj {
				return op, i, true
			}
		} else if operand == "'" {
			if len(op.Params) < 1 {
				continue
			}
			obj := core.TraceToDirectObject(op.Params[0])
			if obj == PdfObj {
				return op, i, true
			}
		}
	}
	return nil, -1, false
}

// Redactor represents a Redactor object.
type Redactor struct {
	reader     *model.PdfReader
	opts       *RedactionOptions
	creator    *creator.Creator
	rectProps  *RectangleProps
	extraTerms []RedactionTerm
	lookup     targetOpLookup
}

// WriteToFile writes the redacted document to file specified by `outputPath`.
func (r *Redactor) WriteToFile(outputPath string) error {
	if err := r.creator.WriteToFile(outputPath); err != nil {
		return fmt.Errorf("failed to write the output file")
	}
	return nil
}

type editFailure struct {
	term   string
	reason string
}

func (c *pageFormClones) cloneOf(s *core.PdfObjectStream) *core.PdfObjectStream {
	if c.isClone[s] {
		return s
	}
	if existing, ok := c.clones[s]; ok {
		return existing
	}
	clone := shallowCloneStream(s)
	c.clones[s] = clone
	c.isClone[clone] = true
	c.privateDicts[clone.PdfObjectDictionary] = true
	return clone
}

func operationString(op *contentstream.ContentStreamOperation) string {
	ops := contentstream.ContentStreamOperations{op}
	return ops.String()
}

func splitAroundRanges(m *matchedIndex, ranges [][]int) []*matchedIndex {
	out := []*matchedIndex{}
	base := m.start
	cur := base
	text := m.text
	pos := 0
	for _, r := range ranges {
		rs := r[0] - base
		if pos >= rs {
			continue
		}
		piece := text[pos:rs]
		mi := &matchedIndex{text: piece, start: cur, end: r[0]}
		if len(strings.TrimSpace(piece)) != 0 {
			out = append(out, mi)
		}
		pos = r[1] - base
		cur = base + pos
	}
	tail := text[pos:]
	last := &matchedIndex{text: tail, start: cur, end: m.end}
	if len(strings.TrimSpace(tail)) != 0 {
		out = append(out, last)
	}
	return out
}

type placeHolders struct {
	offsets []int
	text    string
	adj     float64
}

func rewriteTJ(op *contentstream.ContentStreamOperation, obj core.PdfObject, spans []localSpanMarks) error {
	arr, ok := core.GetArray(op.Params[0])
	out := []core.PdfObject{}
	if !ok {
		common.Log.Debug("ERROR: TJ op=%s GetArrayVal failed", op)
		return fmt.Errorf("spanMarks.BBox has no bounding box. spanMarks=%s", op)
	}
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
			for _, el := range arr.Elements() {
				if el == obj {
					out = append(out, parts...)
				} else {
					out = append(out, el)
				}
			}
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
			for _, el := range arr.Elements() {
				if el == obj {
					out = append(out, items...)
				} else {
					out = append(out, el)
				}
			}
		}
		op.Params[0] = core.MakeArray(out...)
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
		for _, el := range arr.Elements() {
			if el == obj {
				out = append(out, items...)
			} else {
				out = append(out, el)
			}
		}
		op.Params[0] = core.MakeArray(out...)
	}
	return nil
}

type pageFormEdits struct {
	page    int
	res     *model.PdfPageResources
	targets []*editTarget
}

func (c *pageFormClones) rewireFormReferences(res *model.PdfPageResources, orig, clone *core.PdfObjectStream) (string, bool) {
	if res == nil || res.XObject == nil {
		return "the page has no XObject resources", false
	}
	xobjects, ok := core.TraceToDirectObject(res.XObject).(*core.PdfObjectDictionary)
	if !ok {
		return "the page XObject resources are not a dictionary", false
	}
	clear(c.noRewire)
	clear(c.rewired)
	c.walks = 0
	c.tooDeep, c.cyclic, c.walkLimit = false, false, false
	newXObjects, changed := c.rewireXObjects(xobjects, orig, clone, 0, map[*core.PdfObjectStream]bool{})
	if c.walkLimit {
		return "the page paints more forms than are walked for one edit, so not every reference to it could be rewired", false
	}
	if !changed {
		return c.unreferencedReason(), false
	}
	res.XObject = newXObjects
	return "", true
}

type formSearch struct {
	form  *core.PdfObjectStream
	depth int
}
