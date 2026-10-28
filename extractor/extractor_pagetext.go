package extractor

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

func (pt *PageText) makeGridText(marks []*textMark) string {
	if len(marks) == 0 {
		return ""
	}
	words := makeTextWords(marks, pt.pageSize, false)
	if len(words) == 0 {
		return ""
	}
	charWidth := medianCharWidth(words)
	if charWidth <= 0 {
		charWidth = defaultCharWidth
	}
	gapThreshold := charWidth * gapFactor
	words2 := makeGridWords(words)
	if len(words2) == 0 {
		return ""
	}
	minX := minLlx(words2)
	rows := groupGridRows(words2, charWidth)
	spacing := rowSpacing(rows, charWidth)
	return renderGrid(rows, minX, gapThreshold, spacing)
}

// FormContents returns the form XObjects painted on the page, directly or
// nested inside other forms, along with their parsed content stream
// operations. Every paint of a form shares one parse, so each form appears
// once regardless of how many times, or under how many resource names, it is
// painted.
func (pt *PageText) FormContents() []FormContent { return pt.forms }

func (e *Extractor) isTextOnly() bool {
	if e.collectPaths {
		return false
	}
	if e.options == nil {
		return false
	}
	return e.options.ExtractionMode == ExtractionModeGrid
}

// Marks returns the TextMark collection for a page. It represents all the text on the page.
func (pt PageText) Marks() *TextMarkArray { return &TextMarkArray{marks: pt.viewMarks} }

// GridText returns the page text laid out on a fixed-width character grid
// using each word's spatial position. Multi-column documents and tables
// retain their visual column alignment, similar to the output of
// `pdftotext -layout`. Available in any extraction mode.
//
// In ExtractionModeGrid the grid layout is the same string returned by Text(),
// so the cached viewText is reused — including after ApplyArea has narrowed
// the scope, even if the filtered area produced an empty result. In other
// modes the grid is computed on demand from the page's full mark set.
func (pt PageText) GridText() string {
	if pt.options.mode == ExtractionModeGrid {
		return pt.text
	}
	return pt.makeGridText(pt.marks)
}

func makeTextPage(marks []*textMark, pageSize model.PdfRectangle, rulings rulingList, tilings []gridTiling, noBreaks bool) paraList {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("makeTextPage: %d elements pageSize=%.2f", len(marks), pageSize)
	}
	if len(marks) == 0 {
		return nil
	}
	words := makeTextWords(marks, pageSize, false)
	if len(words) == 0 {
		return nil
	}
	rulings.log("makeTextPage")
	vertRulings, horzRulings := rulings.vertsHorzs()
	bag := makeWordBag(words, pageSize.Ury, vertRulings, horzRulings)
	bags := splitWordBag(bag, pageSize.Ury, vertRulings, horzRulings, noBreaks)
	bags = mergeWordBags(bags)
	paras := make(paraList, 0, len(bags))
	for _, bag2 := range bags {
		para := bag2.arrangeText(noBreaks)
		if para != nil {
			paras = append(paras, para)
		}
	}
	if len(paras) >= minTableParas {
		paras = paras.extractTables(tilings)
	}
	paras.sortReadingOrder()
	paras.sortTopoOrder()
	paras.log("sorted in reading order")
	return paras
}

// FormContent holds a form XObject painted on the page together with its
// parsed content stream operations. The DirectObject fields of the text marks
// that originate from a form reference PdfObjects inside Operations, so
// consumers that rewrite content streams (e.g. the redactor) must edit
// Operations and re-encode Form rather than the page content stream.
// Marks merged from annotation appearance streams (extraction with
// Options.IncludeAnnotations) are not covered: the objects they reference
// belong to appearance stream parses that are not reported here.
type FormContent struct {
	// Name is the resource name the form was first painted under.
	Name string

	// Form is the form XObject. Its underlying stream primitive is shared
	// with every other page or form that references the same XObject.
	Form *model.XObjectForm

	// Operations is the parse of the form's content stream that the merged
	// text marks reference.
	Operations *contentstream.ContentStreamOperations
}

// List returns all the list objects detected on the page.
// It detects all the bullet point Lists from a given pdf page and builds a slice of bullet list objects.
// A given bullet list object has a tree structure.
// Each bullet point list is extracted with the text content it contains and all the sub lists found under it as children in the tree.
// The rest content of the pdf is ignored and only text in the bullet point lists are extracted.
// The list extraction is done in two ways.
// 1. If the document is tagged then the lists are extracted using the tags provided in the document.
// 2. Otherwise the bullet lists are extracted from the raw text using regex matching.
// By default the document tag is used if available.
// However this can be disabled using `DisableDocumentTags` in the `Options` object.
// Sometimes disabling document tags option might give a better bullet list extraction if the document was tagged incorrectly.
//
//	    options := &Options{
//		     DisableDocumentTags: false, // this means use document tag if available
//	    }
//	    ex, err := NewWithOptions(page, options)
//	    // handle error
//	    pageText, _, _, err := ex.ExtractPageText()
//	    // handle error
//	    lists := pageText.List()
//	    txt := lists.Text()
func (pt PageText) List() lists {
	useTags := !pt.options.disableDocumentTags
	paras := pt.getParagraphs()
	items := paras.list()
	if pt.structTreeRoot != nil && useTags {
		byMCID := groupLinesByMCID(&paras)
		if len(pt.structTreeRoot.K) == 0 {
			common.Log.Debug("List: structTreeRoot doesn't have any content, using text matching method instead.")
			return items
		}
		items = buildList(pt.structTreeRoot, byMCID, pt.pageObject)
	}
	return items
}

// ExtractPageText returns the text contents of `e` (an Extractor for a page) as a PageText.
// TODO(peterwilliams97): The stats complicate this function signature and aren't very useful.
//
//	Replace with a function like Extract() (*PageText, error)
func (e *Extractor) ExtractPageText() (*PageText, int, int, error) {
	pageText, numChars, numMisparsed, err := e.extractPageText(e.contents, e.resources, transform.IdentityMatrix(), 0, false, nil)
	if err != nil && !errors.Is(err, model.ErrColorOutOfRange) {
		return nil, 0, 0, err
	}
	if e.options != nil {
		pageText.options.mode = e.options.ExtractionMode
		pageText.options.disableDocumentTags = e.options.DisableDocumentTags
		pageText.options.disableDehyphenation = e.options.DisableDehyphenation
	}
	pageText.computeViews()
	if e.options != nil {
		if e.options.ApplyCropBox && e.cropBox != nil {
			pageText.ApplyArea(*e.cropBox)
		}
	}
	return pageText, numChars, numMisparsed, nil
}
func (pt PageText) marksText() string { return pt.cachedMarksText }

// ApplyArea processes the page text only within the specified area `bbox`.
// Each time ApplyArea is called, it updates the result set in `pt`.
// Can be called multiple times in a row with different bounding boxes.
func (pt *PageText) ApplyArea(bbox model.PdfRectangle) {
	inArea := make([]*textMark, 0, len(pt.marks))
	for _, mark := range pt.marks {
		if rectsOverlap(mark.bbox(), bbox) {
			inArea = append(inArea, mark)
		}
	}
	if pt.options.mode == ExtractionModeGrid {
		pt.text = pt.makeGridText(inArea)
		return
	}
	var paras paraList
	var plain strings.Builder
	numInArea := len(inArea)
	for angle := 0; angle < 360 && numInArea > 0; angle += 90 {
		angleMarks := make([]*textMark, 0, len(inArea)-numInArea)
		for _, inArea2 := range inArea {
			if inArea2.orientation == angle {
				angleMarks = append(angleMarks, inArea2)
			}
		}
		if len(angleMarks) > 0 {
			if pt.options.mode == ExtractionModePlain {
				plain.WriteString(makeSimpleText(angleMarks, pt.pageSize))
			} else {
				angleParas := makeTextPage(angleMarks, pt.pageSize, nil, nil, pt.options.mode == ExtractionModeLayoutNoBreaks)
				paras = append(paras, angleParas...)
			}
			numInArea -= len(angleMarks)
		}
	}
	if pt.options.mode == ExtractionModePlain {
		pt.text = plain.String()
	} else {
		buf := new(bytes.Buffer)
		ctx := &textContext{disableDehyphenation: pt.options.disableDehyphenation}
		paras.writeText(buf, ctx)
		pt.text = buf.String()
		pt.cachedMarksText = pt.text
		pt.viewMarks = paras.toTextMarks(ctx)
		pt.tables = paras.tables(ctx)
	}
}

// Tables returns the tables extracted from the page.
func (pt PageText) Tables() []TextTable {
	if debugTables {
		common.Log.Info("Tables: %d", len(pt.tables))
	}
	return pt.tables
}

func (e *Extractor) extractPageText(contents string, resources *model.PdfPageResources, ctm transform.Matrix, formDepth int, isAnnotation bool, gs *contentstream.GraphicsState) (*PageText, int, int, error) {
	contentStreamParser, err := contentstream.NewContentStreamParser(contents).Parse()
	if err != nil {
		common.Log.Debug("ERROR: extractPageText parse failed. err=%v", err)
		pageText := &PageText{pageSize: e.mediaBox, structTreeRoot: e.structTreeRoot, pageObject: e.pageObject}
		return pageText, 0, 0, err
	}
	return e.extractPageTextFromOps(contentStreamParser, resources, ctm, formDepth, isAnnotation, gs)
}

// String returns a string describing `pt`.
func (pt PageText) String() string {
	header := fmt.Sprintf("PageText: %d elements", len(pt.marks))
	parts := []string{"-" + header}
	for _, mark := range pt.marks {
		parts = append(parts, mark.String())
	}
	parts = append(parts, "+"+header)
	return strings.Join(parts, "\n")
}

// GetContentStreamOps returns the contentStreamOps field of `pt`.
func (pt *PageText) GetContentStreamOps() *contentstream.ContentStreamOperations {
	return pt.ops
}

func (pt *PageText) appendFormContent(name string, form *model.XObjectForm, ops *contentstream.ContentStreamOperations) {
	if form == nil || ops == nil {
		return
	}
	for _, f := range pt.forms {
		if f.Operations == ops {
			return
		}
	}
	pt.forms = append(pt.forms, FormContent{Name: name, Form: form, Operations: ops})
}

// ExtractTextWithStats works like ExtractText but returns the number of characters in the output
// (`numChars`) and the number of characters that were not decoded (`numMisses`).
func (e *Extractor) ExtractTextWithStats() (text string, numChars int, numMisses int, err error) {
	pageText, numChars, numMisses, err := e.ExtractPageText()
	if err != nil {
		return "", numChars, numMisses, err
	}
	return pageText.Text(), numChars, numMisses, nil
}

// ExtractText processes and extracts all text data in content streams and returns as a string.
// It takes into account character encodings in the PDF file, which are decoded by
// CharcodeBytesToUnicode.
// Characters that can't be decoded are replaced with MissingCodeRune ('\ufffd' = �).
func (e *Extractor) ExtractText() (string, error) {
	textWithStats, _, _, err := e.ExtractTextWithStats()
	return textWithStats, err
}

// PageText represents the layout of text on a device page.
type PageText struct {
	marks           []*textMark
	text            string
	cachedMarksText string
	viewMarks       []TextMark
	tables          []TextTable
	pageSize        model.PdfRectangle
	strokes         []pathSection
	fills           []pathSection
	structTreeRoot  *model.StructTreeRoot
	pageObject      core.PdfObject
	ops             *contentstream.ContentStreamOperations
	forms           []FormContent
	options         PageTextOptions
}

// Text returns the extracted page text.
func (pt PageText) Text() string { return pt.text }

func makeSimpleText(marks []*textMark, pageSize model.PdfRectangle) string {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("makeSimpleText: %d elements pageSize=%.2f", len(marks), pageSize)
	}
	out := ""
	if len(marks) == 0 {
		return out
	}
	words := makeTextWords(marks, pageSize, true)
	if len(words) == 0 {
		return out
	}
	prevDepth := 0.0
	first := true
	line := ""
	for _, word := range words {
		fontSize := word.fontSize
		if fontSize > minFontSize {
			fontSize = minFontSize
		}
		if (word.depth-prevDepth > lineMergeFactor*fontSize && prevDepth != 0.0) || (prevDepth-word.depth > fontSize*10) {
			bidi := reorderBidi([]rune(line))
			line = bidi.text
			line += "\n"
			out += line
			line = ""
		} else {
			if !first {
				line += " "
			}
		}
		line += word.text
		first = false
		prevDepth = word.depth
	}
	if line != "" {
		bidi := reorderBidi([]rune(line))
		line = bidi.text
		line += "\n"
		out += line
	}
	return out
}

func (pt *PageText) computeViews() {
	if pt.options.mode == ExtractionModePlain {
		pt.text = pt.getText()
		return
	}
	if pt.options.mode == ExtractionModeGrid {
		pt.text = pt.makeGridText(pt.marks)
		return
	}
	paras := pt.getParagraphs()
	buffer := new(bytes.Buffer)
	ctx := &textContext{disableDehyphenation: pt.options.disableDehyphenation}
	paras.writeText(buffer, ctx)
	pt.text = buffer.String()
	pt.cachedMarksText = pt.text
	pt.viewMarks = paras.toTextMarks(ctx)
	pt.tables = paras.tables(ctx)
	if debugTables {
		common.Log.Info("computeViews: tables=%d", len(pt.tables))
	}
}

func (pt *PageText) getText() string {
	var out strings.Builder
	numMarks := len(pt.marks)
	for i := 0; i < 360 && numMarks > 0; i += 90 {
		angleMarks := make([]*textMark, 0, len(pt.marks)-numMarks)
		for _, mark := range pt.marks {
			if mark.orientation == i {
				angleMarks = append(angleMarks, mark)
			}
		}
		if len(angleMarks) > 0 {
			out.WriteString(makeSimpleText(angleMarks, pt.pageSize))
			numMarks -= len(angleMarks)
		}
	}
	return out.String()
}
