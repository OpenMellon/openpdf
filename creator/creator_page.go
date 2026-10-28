package creator

import (
	"errors"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

type pageTransformations struct {
	matrix *transform.Matrix
	flipH  bool
	flipV  bool
}

func (pt *pageTransformations) applyFlip(page *model.PdfPage) error {
	flipH, flipV := pt.flipH, pt.flipV
	if !flipH && !flipV {
		return nil
	}
	if page == nil {
		return errors.New("no page active")
	}
	mediaBox, err := page.GetMediaBox()
	if err != nil {
		return err
	}
	width, height := mediaBox.Width(), mediaBox.Height()
	rotate, err := page.GetRotate()
	if err != nil {
		common.Log.Debug("ERROR: %s - ignoring and assuming no rotation\n", err.Error())
	}
	if isRightAngle := rotate%360 != 0 && rotate%90 == 0; isRightAngle {
		if angle := (360 + rotate%360) % 360; angle == 90 || angle == 270 {
			flipH, flipV = flipV, flipH
		}
	}
	sx, tx := 1.0, 0.0
	if flipH {
		sx, tx = -1.0, -width
	}
	sy, ty := 1.0, 0.0
	if flipV {
		sy, ty = -1.0, -height
	}
	cc := contentstream.NewContentCreator().Scale(sx, sy).Translate(tx, ty)
	stream, err := core.MakeStream(cc.Bytes(), core.NewFlateEncoder())
	if err != nil {
		return err
	}
	array := core.MakeArray(stream)
	array.Append(page.GetContentStreamObjs()...)
	page.Contents = array
	return nil
}

func (pt *pageTransformations) transformBlock(block *Block) {
	if pt.matrix != nil {
		block.transform(*pt.matrix)
	}
}

func (pt *pageTransformations) transformPage(page *model.PdfPage) error {
	if err := pt.applyFlip(page); err != nil {
		return err
	}
	return nil
}

// PageBreak represents a page break for a chapter.
type PageBreak struct{}

func parsePageBreakTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parsePageBreak(node)
}

// Width returns 0. A page break has no width; the method exists so PageBreak
// satisfies the VectorDrawable interface and can be added to a Division.
func (pb *PageBreak) Width() float64 { return 0 }

// This method is not supported by PageBreak component and exists solely to satisfy the Drawable interface.
func (pb *PageBreak) SetStructureType(structureType model.StructureType) {}

// Height returns 0. A page break has no height; the method exists so PageBreak
// satisfies the VectorDrawable interface and can be added to a Division.
func (pb *PageBreak) Height() float64 { return 0 }

// GeneratePageBlocks generates a page break block.
func (pb *PageBreak) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	blocks := []*Block{NewBlock(ctx.PageWidth, ctx.PageHeight-ctx.Y), NewBlock(ctx.PageWidth, ctx.PageHeight)}
	ctx.Page++
	ctx2 := ctx
	ctx2.Y = ctx.Margins.Top
	ctx2.X = ctx.Margins.Left
	ctx2.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom
	ctx2.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right
	ctx = ctx2
	return blocks, ctx, nil
}

// This method is not supported by PageBreak component and exists solely to satisfy the Drawable interface.
func (pb *PageBreak) GenerateKDict() (*model.KDict, error) { return nil, nil }

func newPageBreak() *PageBreak { return &PageBreak{} }

// This method is not supported by PageBreak component and exists solely to satisfy the Drawable interface.
func (pb *PageBreak) SetMarkedContentID(id int64) {}

// This method is not supported by PageBreak component and exists solely to satisfy the Drawable interface.
func (pb *PageBreak) SetStructPageNumber(pageNumber *int64) {}

// PageFinalizeFunctionArgs holds the input arguments provided to the page
// finalize callback function which can be set using Creator.PageFinalize.
type PageFinalizeFunctionArgs struct {
	PageNum    int
	PageWidth  float64
	PageHeight float64
	TOCPages   int
	TotalPages int

	// Chapter describes the chapter the current page belongs to, resolved via
	// Creator.ChapterForPage. It is nil on pages that precede the first chapter.
	Chapter *ChapterInfo
}

// PageSize represents the page size as a 2 element array representing the width and height in PDF document units (points).
type PageSize [2]float64

var (
	PageSizeA3     = PageSize{297 * PPMM, 420 * PPMM}
	PageSizeA4     = PageSize{210 * PPMM, 297 * PPMM}
	PageSizeA5     = PageSize{148 * PPMM, 210 * PPMM}
	PageSizeLetter = PageSize{8.5 * PPI, 11 * PPI}
	PageSizeLegal  = PageSize{8.5 * PPI, 14 * PPI}
)
