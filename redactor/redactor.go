// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package redactor

import (
	"errors"
	"fmt"
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
	var text strings.Builder
	for _, mark := range marks.Elements() {
		text.WriteString(mark.Text)
	}
	return text.String()
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
