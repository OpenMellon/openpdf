package extractor

import (
	"fmt"

	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

type formProcessingError struct {
	name string
	err  error
}

func (fpe *formProcessingError) Error() string {
	return fmt.Sprintf("form %s: %v", fpe.name, fpe.err)
}

func (fpe *formProcessingError) Unwrap() error { return fpe.err }

type inlineImageKey struct {
	image     *contentstream.ContentStreamInlineImage
	resources *model.PdfPageResources
}

type formCacheKey struct {
	stream      *core.PdfObjectStream
	ctm         transform.Matrix
	resources   *model.PdfPageResources
	strokeColor colorStateKey
	fillColor   colorStateKey
}

func makeFormCacheKey(stream *core.PdfObjectStream, ctm transform.Matrix, resources *model.PdfPageResources, gs contentstream.GraphicsState) formCacheKey {
	return formCacheKey{stream: stream, ctm: ctm, resources: resources, strokeColor: newColorStateKey(gs.ColorspaceStroking, gs.ColorStroking), fillColor: newColorStateKey(gs.ColorspaceNonStroking, gs.ColorNonStroking)}
}

type parsedForm struct {
	ops  *contentstream.ContentStreamOperations
	form *model.XObjectForm
}

type parsedImageStream struct {
	ops       *contentstream.ContentStreamOperations
	resources *model.PdfPageResources
	matrix    transform.Matrix
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
