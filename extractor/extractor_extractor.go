package extractor

import (
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

// Extractor stores and offers functionality for extracting content from PDF pages.
type Extractor struct {
	contents          string
	resources         *model.PdfPageResources
	mediaBox          model.PdfRectangle
	cropBox           *model.PdfRectangle
	structParentsKey  int
	fonts             map[string]fontEntry
	parsedForms       map[*core.PdfObjectStream]parsedForm
	formResults       map[formCacheKey]textResult
	annotationResults map[string]textResult
	formExtractions   int
	fontClock         int64
	options           *Options
	structTreeRoot    *model.StructTreeRoot
	pageObject        core.PdfObject
	annotations       []*model.PdfAnnotation
	collectPaths      bool
}

// ExtractStrokePaths processes and extracts all stroke paths in content streams.
// Path geometry is collected regardless of the configured ExtractionMode, so
// callers using ExtractionModeGrid still get stroke paths from this entry point.
func (e *Extractor) ExtractStrokePaths() ([]StrokePath, error) {
	saved := e.collectPaths
	e.collectPaths = true
	defer func() { e.collectPaths = saved }()
	pageText, _, _, err := e.ExtractPageText()
	if err != nil {
		return nil, err
	}
	paths := []StrokePath{}
	for _, stroke := range pageText.strokes {
		sp := StrokePath{Color: stroke.Color, Points: []transform.Point{}}
		for _, sub := range stroke.subpaths {
			sp.Points = append(sp.Points, sub.points...)
		}
		paths = append(paths, sp)
	}
	return paths, nil
}

// ExtractPageImages returns the image contents of the page extractor, including data
// and position, size information for each image.
// A set of options to control page image extraction can be passed in. The options
// parameter can be nil for the default options. By default, inline stencil masks
// are not extracted.
func (e *Extractor) ExtractPageImages(options *ImageExtractOptions) (*PageImages, error) {
	ctx := &imageExtractContext{options: options}
	err := ctx.extractContentStreamImages(e.contents, e.resources, nil)
	if err != nil {
		return nil, err
	}
	return &PageImages{Images: ctx.images}, nil
}
