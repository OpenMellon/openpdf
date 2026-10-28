// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package redactor

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/creator"
	"github.com/OpenMellon/openpdf/extractor"
	"github.com/OpenMellon/openpdf/model"
)

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
			if slices.Contains(arr.Elements(), PdfObj) {
				return op, i, true
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
