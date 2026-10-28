// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"os"
	"text/template/parse"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/model"
)

var backgroundAttrs = map[string]attrApplyFunc{"fill-color": colorAttr(func(background *Background, fillColor Color) { background.FillColor = fillColor }), "border-color": colorAttr(func(background *Background, borderColor Color) { background.BorderColor = borderColor }), "border-size": floatAttr(func(background *Background, borderSize float64) { background.BorderSize = borderSize }), "border-radius": borderRadiusAttr(func(background *Background, topLeft, topRight, bottomRight, bottomLeft float64) {
	background.SetBorderRadius(topLeft, topRight, bottomLeft, bottomRight)
}), "border-top-left-radius": floatAttr(func(background *Background, borderRadiusTopLeft float64) {
	background.BorderRadiusTopLeft = borderRadiusTopLeft
}), "border-top-right-radius": floatAttr(func(background *Background, borderRadiusTopRight float64) {
	background.BorderRadiusTopRight = borderRadiusTopRight
}), "border-bottom-left-radius": floatAttr(func(background *Background, borderRadiusBottomLeft float64) {
	background.BorderRadiusBottomLeft = borderRadiusBottomLeft
}), "border-bottom-right-radius": floatAttr(func(background *Background, borderRadiusBottomRight float64) {
	background.BorderRadiusBottomRight = borderRadiusBottomRight
})}

func addEscapeToTemplate(node parse.Node) {
	switch listNode := node.(type) {
	case *parse.ListNode:
		if listNode == nil {
			return
		}
		for _, node := range listNode.Nodes {
			addEscapeToTemplate(node)
		}
	case *parse.ActionNode:
		addEscapeToPipe(listNode.Pipe)
	case *parse.IfNode:
		addEscapeToTemplate(listNode.List)
		addEscapeToTemplate(listNode.ElseList)
	case *parse.RangeNode:
		addEscapeToTemplate(listNode.List)
		addEscapeToTemplate(listNode.ElseList)
	case *parse.WithNode:
		addEscapeToTemplate(listNode.List)
		addEscapeToTemplate(listNode.ElseList)
	}
}

// RotatedSize returns the width and height of the rotated block.
func (b *Block) RotatedSize() (float64, float64) {
	_, _, width, height := rotatedBounds(b.width, b.height, b.angle)
	return width, height
}

// GraphicSVG represents a drawable graphic SVG.
// It is used to render the graphic SVG components using a creator instance.
type GraphicSVG struct {
	taggedDrawable
	root        *GraphicSVGElement
	positioning Positioning
	xPos        float64
	yPos        float64
	margins     Margins
}

func newImageFromFile(name string) (*Image, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	img, err := model.ImageHandling.Read(file)
	if err != nil {
		common.Log.Error("Error loading image: %s", err)
		return nil, err
	}
	return newImage(img)
}

// SetIndent sets the left offset of the list when nested into another list.
func (li *List) SetIndent(indent float64) { li.indent = indent; li.defaultIndent = false }

// NewRectangle creates a new rectangle with the left corner at (`x`, `y`),
// having the specified width and height.
// NOTE: In relative positioning mode, `x` and `y` are calculated using the
// current context. Furthermore, when the fit mode is set to fill the available
// space, the rectangle is scaled so that it occupies the entire context width
// while maintaining the original aspect ratio.
func (c *Creator) NewRectangle(x, y, width, height float64) *Rectangle {
	return newRectangle(x, y, width, height)
}
