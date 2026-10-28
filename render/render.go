// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package render

import (
	"errors"
	"fmt"
	"image"

	"github.com/OpenMellon/openpdf/annotator"
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/streamlimits"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/render/internal/context"
)

func (r *renderer) noteDecodeLimit(err error) {
	if r.decodeLimitErr == nil && errors.Is(err, core.ErrDecodedSizeLimitExceeded) {
		r.decodeLimitErr = err
	}
}

func (r *renderer) renderContentStream(ctx context.Context, content string, res *model.PdfPageResources) error {
	return r.renderContentStreamWithBase(ctx, content, res, ctx.Matrix())
}

func (r *renderer) renderPage(ctx context.Context, page *model.PdfPage, matrix transform.Matrix, skipFlattening bool) error {
	r.limits = streamlimits.Walker{MinRepeatCost: streamlimits.RenderRepeatCost}
	r.decodeLimitErr = nil
	if !skipFlattening {
		opts := model.FieldFlattenOpts{AnnotFilterFunc: func(annot *model.PdfAnnotation) bool {
			switch annot.GetContext().(type) {
			case *model.PdfAnnotationLine:
				return true
			case *model.PdfAnnotationSquare:
				return true
			case *model.PdfAnnotationCircle:
				return true
			case *model.PdfAnnotationPolygon:
				return true
			case *model.PdfAnnotationPolyLine:
				return true
			}
			return false
		}}
		appearance := annotator.FieldAppearance{}
		err := page.FlattenFieldsWithOpts(appearance, &opts)
		if err != nil {
			r.noteDecodeLimit(err)
			common.Log.Debug("Error during annotation flattening %v", err)
		}
	}
	content, err := page.GetAllContentStreams()
	if err != nil {
		return err
	}
	matrix = matrix.Round(0.01)
	if m := matrix; !m.Identity() {
		content = fmt.Sprintf("%.2f %.2f %.2f %.2f %.2f %.2f cm %s", m[0], m[1], m[3], m[4], m[6], m[7], content)
	}
	ctx.Translate(0, float64(ctx.Height()))
	ctx.Scale(1, -1)
	ctx.Push()
	ctx.SetRGBA(1, 1, 1, 1)
	ctx.DrawRectangle(0, 0, float64(ctx.Width()), float64(ctx.Height()))
	ctx.Fill()
	ctx.Pop()
	ctx.SetLineWidth(1.0)
	ctx.SetRGBA(0, 0, 0, 1)
	return r.renderContentStreamWithBase(ctx, content, page.Resources, ctx.Matrix().Mult(matrix))
}

type renderer struct {
	scale           float64
	fontByName      map[string]*context.TextFont
	textFontByDict  map[*core.PdfObjectDictionary]*context.TextFont
	pdfFontByDict   map[*core.PdfObjectDictionary]*model.PdfFont
	baseMatrix      transform.Matrix
	tilingCache     map[tilingCellKey]*image.RGBA
	tilingDepth     int
	softMaskDepth   int
	limits          streamlimits.Walker
	colorConverters map[*core.PdfObjectDictionary]context.ColorConverter
	decodeLimitErr  error
}

var (
	errTypeCheck           = errors.New("type check error")
	errRangeCheck          = errors.New("range check error")
	errSoftMaskBudget      = errors.New("page nested stream budget spent")
	errNestedStreamRefused = errors.New("nested stream refused")
)

func (r *renderer) renderContentStreamWithBase(ctx context.Context, content string, res *model.PdfPageResources, matrix transform.Matrix) error {
	ops, err := contentstream.NewContentStreamParser(content).Parse()
	if err != nil {
		return err
	}
	return r.renderOperations(ctx, ops, res, matrix)
}
