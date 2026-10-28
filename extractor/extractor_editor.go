package extractor

import (
	"fmt"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// Editor represents a document editor object
type Editor struct{ reader *model.PdfReader }

// Search searches the pages specified by `pages`.
func (e *Editor) Search(pattern string, pages []int) (map[int]Match, error) {
	matches, _, err := e.getMatches(pattern, pages)
	return matches, err
}

// Replace takes a pattern and replaces all the texts that much the pattern with `replacement`.
func (e *Editor) Replace(pattern string, replacement string, pages []int) error {
	matches := map[int]Match{}
	for _, pageNum := range pages {
		pdfPage, err := e.reader.GetPage(pageNum)
		if err != nil {
			return err
		}
		ex, err := New(pdfPage)
		if err != nil {
			return err
		}
		pageText, _, _, err := ex.ExtractPageText()
		if err != nil {
			return err
		}
		contents := ""
		marksText := pageText.marksText()
		allMatches, err := findAllMatches(pattern, marksText)
		if err != nil {
			return err
		}
		marks := pageText.Marks()
		boxes := []Box{}
		for _, loc := range allMatches {
			matchMarks, box, err2 := getMatchMarks(loc, marks, pattern)
			if err2 != nil {
				return err2
			}
			objMarks := groupMarksByObject(matchMarks)
			boxes = append(boxes, box)
			err2 = replaceTextMarks(objMarks, replacement)
			if err2 != nil {
				return err2
			}
		}
		contents = pageText.GetContentStreamOps().String()
		match := Match{Pattern: pattern, Indexes: allMatches, Locations: boxes}
		pdfPage.SetContentStreams([]string{contents}, core.NewFlateEncoder())
		matches[pageNum] = match
	}
	return nil
}

// NewEditor returns a new Editor object
func NewEditor(reader *model.PdfReader) *Editor { return &Editor{reader: reader} }

func (e *Editor) getMatches(pattern string, pages []int) (map[int]Match, map[int][]*TextMarkArray, error) {
	matches := map[int]Match{}
	marksByPage := map[int][]*TextMarkArray{}
	for _, pageNum := range pages {
		page, err := e.reader.GetPage(pageNum)
		if err != nil {
			return nil, nil, err
		}
		ex, err := New(page)
		if err != nil {
			return nil, nil, err
		}
		pageText, _, _, err := ex.ExtractPageText()
		if err != nil {
			return nil, nil, err
		}
		marksText := pageText.marksText()
		allMatches, err := findAllMatches(pattern, marksText)
		if err != nil {
			return nil, nil, err
		}
		if len(allMatches) == 0 {
			common.Log.Info("No match found for %s on page %d", pattern, pageNum)
		}
		marks := pageText.Marks()
		boxes := []Box{}
		for _, allMatch := range allMatches {
			_, box, err2 := getMatchMarks(allMatch, marks, pattern)
			if err2 != nil {
				return nil, nil, err2
			}
			boxes = append(boxes, box)
		}
		match := Match{Pattern: pattern, Indexes: allMatches, Locations: boxes}
		matches[pageNum] = match
	}
	return matches, marksByPage, nil
}

// WriteToFile writes the edited content to `outputPath`.
func (e *Editor) WriteToFile(outputPath string) error {
	writer, err := e.reader.ToWriter(nil)
	if err != nil {
		return fmt.Errorf("failed to convert to writer %v", err)
	}
	writer.WriteToFile(outputPath)
	return nil
}
