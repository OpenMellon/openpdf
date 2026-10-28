package extractor

import (
	"errors"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

func (e *Extractor) isTextOnly() bool {
	if e.collectPaths {
		return false
	}
	if e.options == nil {
		return false
	}
	return e.options.ExtractionMode == ExtractionModeGrid
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

func (e *Extractor) extractPageText(contents string, resources *model.PdfPageResources, ctm transform.Matrix, formDepth int, isAnnotation bool, gs *contentstream.GraphicsState) (*PageText, int, int, error) {
	contentStreamParser, err := contentstream.NewContentStreamParser(contents).Parse()
	if err != nil {
		common.Log.Debug("ERROR: extractPageText parse failed. err=%v", err)
		pageText := &PageText{pageSize: e.mediaBox, structTreeRoot: e.structTreeRoot, pageObject: e.pageObject}
		return pageText, 0, 0, err
	}
	return e.extractPageTextFromOps(contentStreamParser, resources, ctm, formDepth, isAnnotation, gs)
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

// ExtractFonts returns all font information from the page extractor, including
// font name, font type, the raw data of the embedded font file (if embedded), font descriptor and more.
//
// The argument `previousPageFonts` is used when trying to build a complete font catalog for multiple pages or the entire document.
// The entries from `previousPageFonts` are added to the returned result unless already included in the page, i.e. no duplicate entries.
//
// NOTE: If previousPageFonts is nil, all fonts from the page will be returned. Use it when building up a full list of fonts for a document or page range.
func (e *Extractor) ExtractFonts(previousPageFonts *PageFonts) (*PageFonts, error) {
	fonts := PageFonts{}
	err := fonts.extractPageResourcesToFont(e.resources)
	if err != nil {
		return nil, err
	}
	if previousPageFonts != nil {
		for _, font := range previousPageFonts.Fonts {
			if !containsFont(fonts.Fonts, font.FontName) {
				fonts.Fonts = append(fonts.Fonts, font)
			}
		}
	}
	return &PageFonts{Fonts: fonts.Fonts}, nil
}
