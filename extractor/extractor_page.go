package extractor

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// PageTextOptions holds various options available in extraction process.
type PageTextOptions struct {
	disableDocumentTags  bool
	mode                 ExtractionMode
	disableDehyphenation bool
}

// PageImages represents extracted images on a PDF page with spatial information:
// display position and size.
type PageImages struct{ Images []ImageMark }

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

// Text returns the extracted page text.
func (pt PageText) Text() string { return pt.text }

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

func (pt *PageText) getParagraphs() paraList {
	var rulings rulingList
	if useStrokeRulings {
		strokeRulings := makeStrokeRulings(pt.strokes)
		rulings = append(rulings, strokeRulings...)
	}
	if useFillRulings {
		fillRulings := makeFillRulings(pt.fills)
		rulings = append(rulings, fillRulings...)
	}
	rulings, tilings := rulings.toTilings()
	var paras paraList
	numMarks := len(pt.marks)
	for angle := 0; angle < 360 && numMarks > 0; angle += 90 {
		orientMarks := make([]*textMark, 0, len(pt.marks)-numMarks)
		for _, mark := range pt.marks {
			if mark.orientation == angle {
				orientMarks = append(orientMarks, mark)
			}
		}
		if len(orientMarks) > 0 {
			orientParas := makeTextPage(orientMarks, pt.pageSize, rulings, tilings, pt.options.mode == ExtractionModeLayoutNoBreaks)
			paras = append(paras, orientParas...)
			numMarks -= len(orientMarks)
		}
	}
	return paras
}

// PageFonts represents extracted fonts on a PDF page.
type PageFonts struct{ Fonts []Font }

func (pf *PageFonts) extractPageResourcesToFont(resources *model.PdfPageResources) error {
	if resources.Font == nil {
		return errors.New(msgNoFontFound)
	}
	fontDict, ok := core.GetDict(resources.Font)
	if !ok {
		return errors.New(msgFontConvert)
	}
	for _, key := range fontDict.Keys() {
		var (
			hasToUnicode = true
			data         []byte
			fileName     string
		)
		fontObj, ok2 := resources.GetFontByName(key)
		if !ok2 {
			return errors.New(msgFontPropsNotFound)
		}
		pdfFont, err := model.NewPdfFontFromPdfObject(fontObj)
		if err != nil {
			return err
		}
		descriptor := pdfFont.FontDescriptor()
		fontName := pdfFont.FontDescriptor().FontName.String()
		subtype := pdfFont.Subtype()
		if containsFont(pf.Fonts, fontName) {
			continue
		}
		if len(pdfFont.ToUnicode()) == 0 {
			hasToUnicode = false
		}
		if descriptor.FontFile != nil {
			if stream, ok3 := core.GetStream(descriptor.FontFile); ok3 {
				data, err = core.DecodeStream(stream)
				if err != nil {
					return err
				}
				fileName = fontName + ".pfb"
			}
		} else if descriptor.FontFile2 != nil {
			if stream, ok3 := core.GetStream(descriptor.FontFile2); ok3 {
				data, err = core.DecodeStream(stream)
				if err != nil {
					return err
				}
				fileName = fontName + ".ttf"
			}
		} else if descriptor.FontFile3 != nil {
			if stream, ok3 := core.GetStream(descriptor.FontFile3); ok3 {
				data, err = core.DecodeStream(stream)
				if err != nil {
					return err
				}
				fileName = fontName + ".cff"
			}
		}
		if len(fileName) < 1 {
			common.Log.Debug(msgFontStreamInvalid)
		}
		font := Font{FontName: fontName, PdfFont: pdfFont, IsCID: pdfFont.IsCID(), IsSimple: pdfFont.IsSimple(), ToUnicode: hasToUnicode, FontType: subtype, FontData: data, FontFileName: fileName, FontDescriptor: descriptor}
		pf.Fonts = append(pf.Fonts, font)
	}
	return nil
}
