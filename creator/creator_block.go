package creator

import (
	"fmt"
	"io"
	"math"
	"slices"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

// Block contains a portion of PDF Page contents. It has a width and a position and can
// be placed anywhere on a Page.  It can even contain a whole Page, and is used in the creator
// where each Drawable object can output one or more blocks, each representing content for separate pages
// (typically needed when Page breaks occur).
type Block struct {
	contents    *contentstream.ContentStreamOperations
	resources   *model.PdfPageResources
	positioning Positioning
	xPos, yPos  float64
	width       float64
	height      float64
	angle       float64
	margins     Margins
	annotations []*model.PdfAnnotation
	artifact    *model.Artifact
	background  *Background
}

// This method is not supported by Block component and exists solely to satisfy the Drawable interface.
func (b *Block) SetStructureType(structureType model.StructureType) {}

// DrawTemplate renders the template provided through the specified reader,
// using the specified `data` and `options`.
// Creator templates are first executed as text/template *Template instances,
// so the specified `data` is inserted within the template.
// The second phase of processing is actually parsing the template, translating
// it into creator components and rendering them using the provided options.
// Both the `data` and `options` parameters can be nil.
func (b *Block) DrawTemplate(c *Creator, r io.Reader, data any, options *TemplateOptions) error {
	return drawTemplate(c, r, data, options, b)
}

// RotatedSize returns the width and height of the rotated block.
func (b *Block) RotatedSize() (float64, float64) {
	_, _, width, height := rotatedBounds(b.width, b.height, b.angle)
	return width, height
}

// GeneratePageBlocks draws the block contents on a template Page block.
// Implements the Drawable interface.
func (b *Block) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	identityMatrix := transform.IdentityMatrix()
	width, height := b.Width(), b.Height()
	if b.positioning.IsRelative() {
		identityMatrix = identityMatrix.Translate(ctx.X, ctx.PageHeight-ctx.Y-height)
	} else {
		identityMatrix = identityMatrix.Translate(b.xPos, ctx.PageHeight-b.yPos-height)
	}
	y := height
	if b.angle != 0 {
		identityMatrix = identityMatrix.Translate(width/2, height/2).Rotate(b.angle*math.Pi/180.0).Translate(-width/2, -height/2)
		_, y = b.RotatedSize()
	}
	if b.positioning.IsRelative() {
		ctx.Y += y
	}
	contentCreator := contentstream.NewContentCreator()
	contentCreator.Add_cm(identityMatrix[0], identityMatrix[1], identityMatrix[3], identityMatrix[4], identityMatrix[6], identityMatrix[7])
	duplicate := b.duplicate()
	if b.background != nil {
		resources := *b.resources
		if pdfObjectDictionary, ok := b.resources.Pattern.(*core.PdfObjectDictionary); ok {
			resources.Pattern = core.MakeDict().Merge(pdfObjectDictionary)
		}
		if pdfObjectDictionary, ok := b.resources.Shading.(*core.PdfObjectDictionary); ok {
			resources.Shading = core.MakeDict().Merge(pdfObjectDictionary)
		}
		if pdfObjectDictionary, ok := b.resources.ExtGState.(*core.PdfObjectDictionary); ok {
			resources.ExtGState = core.MakeDict().Merge(pdfObjectDictionary)
		}
		duplicate.resources = &resources
		background, err := renderBackground(b.background, width, height)
		if err != nil {
			return nil, ctx, err
		}
		contentStreamOperations := &contentstream.ContentStreamOperations{}
		if err := mergeContentStreams(contentStreamOperations, duplicate.resources, background.contents, background.resources); err != nil {
			return nil, ctx, err
		}
		*duplicate.contents = append(*contentStreamOperations, *duplicate.contents...)
	}
	contentStreamOperations := append(*contentCreator.Operations(), *duplicate.contents...)
	contentStreamOperations.WrapIfNeeded()
	duplicate.contents = &contentStreamOperations
	if b.artifact != nil {
		contentCreator := contentstream.NewContentCreator()
		if m := b.artifact.GenerateMap(); len(m) > 0 {
			contentCreator.Add_BDC(*core.MakeName("Artifact"), m)
		} else {
			contentCreator.Add_BMC(*core.MakeName("Artifact"))
		}
		contentStreamOperations := append(*contentCreator.Operations(), *duplicate.contents...)
		contentCreator2 := contentstream.NewContentCreator()
		contentCreator2.Add_EMC()
		contentStreamOperations = append(contentStreamOperations, *contentCreator2.Operations()...)
		contentStreamOperations.WrapIfNeeded()
		duplicate.contents = &contentStreamOperations
	}
	for _, annotation := range b.annotations {
		arr, ok := core.GetArray(annotation.Rect)
		if !ok || arr.Len() != 4 {
			common.Log.Debug("WARN: invalid annotation Rect field: %v. Output may be incorrect.", annotation.Rect)
			continue
		}
		pdfRectangle, err := model.NewPdfRectangle(*arr)
		if err != nil {
			common.Log.Debug("WARN: could not parse annotation Rect field: %v. Output may be incorrect.", err)
			continue
		}
		pdfRectangle.Transform(identityMatrix)
		annotation.Rect = pdfRectangle.ToPdfObject()
	}
	return []*Block{duplicate}, ctx, nil
}

// ScaleToWidth scales the Block to a specified width, maintaining the same aspect ratio.
func (b *Block) ScaleToWidth(w float64) { sx := w / b.width; b.Scale(sx, sx) }

// This method is not supported by Block component and exists solely to satisfy the Drawable interface.
func (b *Block) SetMarkedContentID(id int64) {}

// NewBlockFromPage creates a Block from a PDF Page.  Useful for loading template pages as blocks
// from a PDF document and additional content with the creator.
func NewBlockFromPage(page *model.PdfPage) (*Block, error) {
	block := &Block{}
	allContentStreams, err := page.GetAllContentStreams()
	if err != nil {
		return nil, err
	}
	contentStreamParser := contentstream.NewContentStreamParser(allContentStreams)
	contents, err := contentStreamParser.Parse()
	if err != nil {
		return nil, err
	}
	contents.WrapIfNeeded()
	block.contents = contents
	if page.Resources != nil {
		block.resources = page.Resources
	} else {
		block.resources = model.NewPdfPageResources()
	}
	mediaBox, err := page.GetMediaBox()
	if err != nil {
		return nil, err
	}
	if mediaBox.Llx != 0 || mediaBox.Lly != 0 {
		block.translate(-mediaBox.Llx, mediaBox.Lly)
	}
	block.width = mediaBox.Urx - mediaBox.Llx
	block.height = mediaBox.Ury - mediaBox.Lly
	if page.Rotate != nil {
		block.angle = -float64(*page.Rotate)
	}
	return block, nil
}

func (b *Block) drawToPage(page *model.PdfPage) error {
	contentStreamOperations := &contentstream.ContentStreamOperations{}
	if page.Resources == nil {
		page.Resources = model.NewPdfPageResources()
	}
	err := mergeContentStreams(contentStreamOperations, page.Resources, b.contents, b.resources)
	if err != nil {
		return err
	}
	if err = mergeColorspaces(b.resources, page.Resources); err != nil {
		return err
	}
	if err = page.AppendContentBytes(contentStreamOperations.Bytes(), true); err != nil {
		return err
	}
	for _, annotation := range b.annotations {
		page.AddAnnotation(annotation)
	}
	return nil
}

func (b *Block) addContents(contentStreamOperations *contentstream.ContentStreamOperations) {
	*b.contents = append(*b.contents, *contentStreamOperations...)
}

func (b *Block) addWrappedContents(contentStreamOperations *contentstream.ContentStreamOperations) {
	b.contents.WrapIfNeeded()
	contentStreamOperations.WrapIfNeeded()
	*b.contents = append(*b.contents, *contentStreamOperations...)
}

// GetArtifact returns the artifact associated with the block, if any.
func (b *Block) GetArtifact() *model.Artifact { return b.artifact }

// MarkAsArtifact marks the block content as an artifact of the specified type.
//
// All of the block's content is wrapped in an /Artifact marked content sequence
// and the block is excluded from the structure tree. This is used for content
// such as running headers and footers that must not be part of the logical
// structure of a tagged PDF.
func (b *Block) MarkAsArtifact(artifactType model.ArtifactType) *model.Artifact {
	b.artifact = &model.Artifact{Type: artifactType}
	return b.artifact
}

// SetBackground sets the background properties of the block. The background is
// drawn behind the block content, covering the block bounds.
func (b *Block) SetBackground(background *Background) { b.background = background }

// Width returns the Block's width.
func (b *Block) Width() float64 { return b.width }

// DrawWithContext draws the Block using the specified drawing context.
func (b *Block) DrawWithContext(d Drawable, ctx DrawContext) error {
	pageBlocks, _, err := d.GeneratePageBlocks(ctx)
	if err != nil {
		return err
	}
	if len(pageBlocks) != 1 {
		return ErrContentNotFit
	}
	for _, pageBlock := range pageBlocks {
		if err := b.mergeBlocks(pageBlock); err != nil {
			return err
		}
	}
	return nil
}

// This method is not supported by Block component and exists solely to satisfy the Drawable interface.
func (b *Block) GenerateKDict() (*model.KDict, error) { return nil, nil }

// Angle returns the block rotation angle in degrees.
func (b *Block) Angle() float64 { return b.angle }

// NewBlock creates a new Block with specified width and height.
func NewBlock(width float64, height float64) *Block {
	block := &Block{}
	block.contents = &contentstream.ContentStreamOperations{}
	block.resources = model.NewPdfPageResources()
	block.width = width
	block.height = height
	return block
}

// RemoveArtifact removes the artifact associated with the block, if any.
func (b *Block) RemoveArtifact() { b.artifact = nil }

func (b *Block) mergeBlocks(block *Block) error {
	err := mergeContentStreams(b.contents, b.resources, block.contents, block.resources)
	if err != nil {
		return err
	}
	for _, annotation := range block.annotations {
		b.AddAnnotation(annotation)
	}
	return nil
}

func (b *Block) setOpacity(val float64, val2 float64) (string, error) {
	if (val < 0 || val >= 1.0) && (val2 < 0 || val2 >= 1.0) {
		return "", nil
	}
	n := 0
	setOpacity := fmt.Sprintf("GS%d", n)
	for b.resources.HasExtGState(core.PdfObjectName(setOpacity)) {
		n++
		setOpacity = fmt.Sprintf("GS%d", n)
	}
	dict := core.MakeDict()
	if val >= 0 && val < 1.0 {
		dict.Set("ca", core.MakeFloat(val))
	}
	if val2 >= 0 && val2 < 1.0 {
		dict.Set("CA", core.MakeFloat(val2))
	}
	err := b.resources.AddExtGState(core.PdfObjectName(setOpacity), dict)
	if err != nil {
		return "", err
	}
	return setOpacity, nil
}

// SetPos sets the Block's positioning to absolute mode with the specified coordinates.
func (b *Block) SetPos(x, y float64) { b.positioning = PositionAbsolute; b.xPos = x; b.yPos = y }

// This method is not supported by Block component and exists solely to satisfy the Drawable interface.
func (b *Block) SetStructPageNumber(pageNumber *int64) {}

// ValidateTemplate checks the parsed template against the specified `data`
// without drawing anything, reporting unknown tags, invalid nesting and, in
// strict mode, attribute errors. The `data` parameter can be nil.
func (b *Block) ValidateTemplate(c *Creator, pt *ParsedTemplate, data any) error {
	return pt.validateWith(c, b, data)
}

// RenderTemplate draws the parsed template into the block using the specified
// creator and `data`. The handle comes from ParseTemplate and can be rendered
// repeatedly with different data. The `data` parameter can be nil.
func (b *Block) RenderTemplate(c *Creator, pt *ParsedTemplate, data any) error {
	return pt.renderInto(c, b, data)
}

func (b *Block) isEmpty() bool { return b.contents == nil || len(*b.contents) == 0 }

// Draw draws the drawable d on the block.
// Note that the drawable must not wrap, i.e. only return one block. Otherwise an error is returned.
func (b *Block) Draw(d Drawable) error {
	ctx := DrawContext{}
	ctx.Width = b.width
	ctx.Height = b.height
	ctx.PageWidth = b.width
	ctx.PageHeight = b.height
	ctx.X = 0
	ctx.Y = 0
	pageBlocks, _, err := d.GeneratePageBlocks(ctx)
	if err != nil {
		return err
	}
	if len(pageBlocks) != 1 {
		return ErrContentNotFit
	}
	for _, pageBlock := range pageBlocks {
		if err := b.mergeBlocks(pageBlock); err != nil {
			return err
		}
	}
	return nil
}

// GetMargins returns the Block's margins: left, right, top, bottom.
func (b *Block) GetMargins() (float64, float64, float64, float64) {
	return b.margins.Left, b.margins.Right, b.margins.Top, b.margins.Bottom
}

// SetMargins sets the Block's left, right, top, bottom, margins.
func (b *Block) SetMargins(left, right, top, bottom float64) {
	b.margins.Left = left
	b.margins.Right = right
	b.margins.Top = top
	b.margins.Bottom = bottom
}

func (b *Block) translate(tx, ty float64) {
	operations := contentstream.NewContentCreator().Translate(tx, -ty).Operations()
	*b.contents = append(*operations, *b.contents...)
	b.contents.WrapIfNeeded()
}

// SetAngle sets the rotation angle in degrees.
func (b *Block) SetAngle(angleDeg float64) { b.angle = angleDeg }

func (b *Block) addContentsByString(contentStr string) error {
	contentStreamParser := contentstream.NewContentStreamParser(contentStr)
	contentStreamOperations, err := contentStreamParser.Parse()
	if err != nil {
		return err
	}
	b.contents.WrapIfNeeded()
	contentStreamOperations.WrapIfNeeded()
	*b.contents = append(*b.contents, *contentStreamOperations...)
	return nil
}

func (b *Block) transform(matrix transform.Matrix) {
	operations := contentstream.NewContentCreator().Add_cm(matrix[0], matrix[1], matrix[3], matrix[4], matrix[6], matrix[7]).Operations()
	*b.contents = append(*operations, *b.contents...)
	b.contents.WrapIfNeeded()
}

// AddAnnotation adds an annotation to the current block.
// The annotation will be added to the page the block will be rendered on.
func (b *Block) AddAnnotation(annotation *model.PdfAnnotation) {
	if slices.Contains(b.annotations, annotation) {
		return
	}
	b.annotations = append(b.annotations, annotation)
}

// ScaleToHeight scales the Block to a specified height, maintaining the same aspect ratio.
func (b *Block) ScaleToHeight(h float64) { sx := h / b.height; b.Scale(sx, sx) }

// Height returns the Block's height.
func (b *Block) Height() float64 { return b.height }

// Scale block by specified factors in the x and y directions.
func (b *Block) Scale(sx, sy float64) {
	operations := contentstream.NewContentCreator().Scale(sx, sy).Operations()
	*b.contents = append(*operations, *b.contents...)
	b.contents.WrapIfNeeded()
	b.width *= sx
	b.height *= sy
}

func (b *Block) duplicate() *Block {
	block := &Block{}
	*block = *b
	contentStreamOperations := contentstream.ContentStreamOperations{}
	contentStreamOperations = append(contentStreamOperations, *b.contents...)
	block.contents = &contentStreamOperations
	return block
}
