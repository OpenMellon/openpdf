// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"encoding/xml"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

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

// NewCurve returns new instance of Curve between points (x1,y1) and (x2, y2) with control point (cx,cy).
func (c *Creator) NewCurve(x1, y1, cx, cy, x2, y2 float64) *Curve {
	return newCurve(x1, y1, cx, cy, x2, y2)
}

// NewTextChunk returns a new text chunk instance.
func NewTextChunk(text string, style TextStyle) *TextChunk {
	return &TextChunk{Text: text, Style: style, VerticalAlignment: TextVerticalAlignmentBaseline, annotations: []*model.PdfAnnotation{}, lineBreaks: []bool{}, taggedDrawable: taggedDrawable{structType: model.StructureTypeSpan}}
}

// CurvePolygon represents a curve polygon shape.
// Implements the Drawable interface and can be drawn on PDF using the Creator.
type CurvePolygon struct {
	taggedDrawable
	polygon       *contentdraw.CurvePolygon
	fillOpacity   float64
	borderOpacity float64
	fillColor     Color
}

const (
	TextVerticalAlignmentBaseline TextVerticalAlignment = iota
	TextVerticalAlignmentCenter
	TextVerticalAlignmentBottom
	TextVerticalAlignmentTop
)

// SetMargins sets the margins of the line.
// NOTE: line margins are only applied if relative positioning is used.
func (l *Line) SetMargins(left, right, top, bottom float64) {
	l.margins.Left = left
	l.margins.Right = right
	l.margins.Top = top
	l.margins.Bottom = bottom
}

// ScaleToHeight scales the ellipse to the specified height. The width of
// the ellipse is scaled so that the aspect ratio is maintained.
func (e *Ellipse) ScaleToHeight(h float64) {
	width := e.width / e.height
	e.height = h
	e.width = h * width
}

func parsePageBreakTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parsePageBreak(node)
}

var newlineWhitespaceRe = regexp.MustCompile("[ \\t\\r\\n]*\\n[ \\t\\r\\n]*")

func hasTextChunk(v any) bool {
	switch templateText2 := v.(type) {
	case *templateText:
		return !isBlank(templateText2.text)
	case *templateNode:
		switch templateText2.start.Name.Local {
		case styleElementName:
			return false
		case "text-chunk":
			return true
		}
		if slices.ContainsFunc(templateText2.childValues, hasTextChunk) {
			return true
		}
	}
	return false
}

func newList(style TextStyle) *List {
	return &List{marker: TextChunk{Text: "• ", Style: style}, indent: 0, defaultIndent: true, positioning: PositionRelative, textStyle: style, taggedDrawable: taggedDrawable{structType: model.StructureTypeList}}
}

func (tc *TableCell) cloneProps(content VectorDrawable) *TableCell {
	props := *tc
	props.content = content
	return &props
}

// Width returns the current page width.
func (c *Creator) Width() float64 { return c.pageWidth }

// GetIndent get the cell's left indent.
func (tc *TableCell) GetIndent() float64 { return tc.indent }

func drawImageBlock(block *Block, img *Image, imageBlock DrawContext) (DrawContext, error) {
	imageBlock2 := imageBlock
	n := 1
	keyName := core.PdfObjectName(fmt.Sprintf("Img%d", n))
	for block.resources.HasXObjectByName(keyName) {
		n++
		keyName = core.PdfObjectName(fmt.Sprintf("Img%d", n))
	}
	err := block.resources.SetXObjectImageByNameLazy(keyName, img.xobj, img.lazy)
	if err != nil {
		return imageBlock, err
	}
	n2 := 0
	gsName := core.PdfObjectName(fmt.Sprintf("GS%d", n2))
	for block.resources.HasExtGState(gsName) {
		n2++
		gsName = core.PdfObjectName(fmt.Sprintf("GS%d", n2))
	}
	dict := core.MakeDict()
	dict.Set("BM", core.MakeName("Normal"))
	if img.opacity < 1.0 {
		dict.Set("CA", core.MakeFloat(img.opacity))
		dict.Set("ca", core.MakeFloat(img.opacity))
	}
	err = block.resources.AddExtGState(gsName, core.MakeIndirectObject(dict))
	if err != nil {
		return imageBlock, err
	}
	width := img.Width()
	height := img.Height()
	_, y := img.rotatedSize()
	x := imageBlock.X
	ty := imageBlock.PageHeight - imageBlock.Y - height
	if img.positioning.IsRelative() {
		ty -= (y - height) / 2
		switch img.horizontalAlignment {
		case HorizontalAlignmentCenter:
			x += (imageBlock.Width - width) / 2
		case HorizontalAlignmentRight:
			x = imageBlock.PageWidth - imageBlock.Margins.Right - img.margins.Right - width
		}
	}
	angle := img.angle
	if img.artifact == nil && img.tagInfo != nil && img.tagInfo.ComponentKObj != nil {
		width2, height2 := img.rotatedSize()
		x2 := x + width/2
		ty2 := ty + height/2
		img.tagInfo.ComponentKObj.SetBoundingBox(x2-width2/2, ty2-height2/2, width2, height2)
	}
	contentCreator := contentstream.NewContentCreator()
	if img.artifact == nil {
		if img.tagInfo != nil {
			contentCreator.Add_BDC(*core.MakeName(string(img.tagInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(img.tagInfo.Mcid)})
		}
	} else {
		m := img.artifact.GenerateMap()
		if len(m) > 0 {
			contentCreator.Add_BDC(*core.MakeName(string("Artifact")), m)
		} else {
			contentCreator.Add_BMC(*core.MakeName(string("Artifact")))
		}
	}
	contentCreator.Add_gs(gsName)
	contentCreator.Translate(x, ty)
	if angle != 0 {
		contentCreator.Translate(width/2, height/2)
		contentCreator.RotateDeg(angle)
		contentCreator.Translate(-width/2, -height/2)
	}
	contentCreator.Scale(width, height).Add_Do(keyName)
	if img.tagInfo != nil || img.artifact != nil {
		contentCreator.Add_EMC()
	}
	operations := contentCreator.Operations()
	operations.WrapIfNeeded()
	block.addWrappedContents(operations)
	if img.positioning.IsRelative() {
		imageBlock.Y += y
		imageBlock.Height -= y
		return imageBlock, nil
	}
	return imageBlock2, nil
}

func isVariationSelector(r rune) bool {
	return (r >= 0xFE00 && r <= 0xFE0F) || (r >= 0xE0100 && r <= 0xE01EF)
}

// NewCell makes a new cell and inserts it into the table at the current position.
func (t *Table) NewCell() *TableCell { return t.MultiCell(1, 1) }

func (t *Table) split(ctx DrawContext, value float64) (head, tail *Table) {
	headerCut := t.maxFittingRows(ctx.Width, ctx.Height)
	if !t.enablePageWrap && t.Height()+t.margins.Top+t.margins.Bottom <= value {
		return nil, t
	}
	if withPartialRow := t.withPartialRow(headerCut, ctx, value); withPartialRow != nil {
		return withPartialRow.splitAtRow(headerCut + 1)
	}
	return t.splitAtRow(t.headerCut(headerCut))
}

// SetExtends specifies whether to extend the shading beyond the starting and ending points.
//
// Text extends is set to `[]bool{false, false}` by default.
func (s *shading) SetExtends(start bool, end bool) { s.extends = []bool{start, end} }
func parseFloat64(s string) (float64, error)       { return strconv.ParseFloat(s, 64) }

// SetAngle sets Image rotation angle in degrees.
func (im *Image) SetAngle(angle float64) { im.angle = angle }

func newFunctionBasedShading(psCode string) *FunctionBasedShading {
	return &FunctionBasedShading{base: &shading{antiAlias: false}, psCode: psCode, boundingBox: &model.PdfRectangle{}}
}
func decoderInputPos(decoder *xml.Decoder) (int, int) { return decoder.InputPos() }

// Margins returns the margins of the list: left, right, top, bottom.
func (li *List) Margins() (float64, float64, float64, float64) {
	return li.margins.Left, li.margins.Right, li.margins.Top, li.margins.Bottom
}
