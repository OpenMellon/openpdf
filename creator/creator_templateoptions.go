// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"io"
	"reflect"
	"text/template"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	chartrender "github.com/OpenMellon/unichart/render"
)

// TemplateOptions contains options and resources to use when rendering
// a template with a Creator instance.
// All the resources in the map fields can be referenced by their
// name/key in the template which is rendered using the options instance.
type TemplateOptions struct {
	// HelperFuncMap is used to define functions which can be accessed
	// inside the rendered templates by their assigned names.
	HelperFuncMap template.FuncMap

	// SubtemplateMap contains templates which can be rendered alongside
	// the main template. They can be accessed using their assigned names
	// in the main template or in the other subtemplates.
	// Subtemplates defined inside the subtemplates specified in the map
	// can be accessed directly.
	// All resources available to the main template are also available
	// to the subtemplates.
	SubtemplateMap map[string]io.Reader

	// FontMap contains pre-loaded fonts which can be accessed
	// inside the rendered templates by their assigned names.
	// The assigned names must not contain commas, as the font
	// attribute is interpreted as a comma-separated fallback list.
	FontMap map[string]*model.PdfFont

	// ImageMap contains pre-loaded images which can be accessed
	// inside the rendered templates by their assigned names.
	ImageMap map[string]*model.Image

	// ColorMap contains colors which can be accessed
	// inside the rendered templates by their assigned names.
	ColorMap map[string]Color

	// ChartMap contains charts which can be accessed
	// inside the rendered templates by their assigned names.
	ChartMap map[string]chartrender.ChartRenderable

	// DisableAutoEscape turns off automatic XML-escaping of template action
	// output.
	//
	// By default, the output of every template action is XML-escaped so that
	// values coming from the template data cannot break the XML stream (e.g. a
	// bare `&`) or inject their own tags. The `raw` helper opts a single action
	// out of escaping; it is honored only as the final command of the action's
	// pipeline (optionally wrapped in parentheses), so `{{raw .v}}` and
	// `{{(raw .v)}}` are unescaped, while `{{raw .v | printf "%s"}}` and
	// `{{printf "[%s]" (raw .v)}}` are still escaped.
	//
	// Set this to true to restore the previous behavior, where action output is
	// written to the XML stream verbatim. This is an upgrade escape hatch for
	// templates whose data supplies pre-escaped XML (for example the "&#xA;"
	// newline idiom) or intentionally emits markup. When disabled, `raw` is not
	// reserved and a HelperFuncMap entry of that name is used as provided.
	DisableAutoEscape bool

	// Strict makes the processor return an error, located at the template file
	// and line:column, on an attribute parse failure, unknown attribute or tag,
	// invalid nesting, invalid table structure, inapplicable attribute or
	// unresolved image or chart resource. Execution also fails when an action
	// references a key missing from map data, including in `{{if .Key}}` and
	// `{{with .Key}}`; read optional keys with `{{with index . "Key"}}`.
	//
	// When false (the default), these problems are logged and skipped, a bad
	// attribute value falls back to its default, and a missing key renders as
	// empty text. With DisableAutoEscape it writes text/template's `<no value>`
	// into the XML, which fails to parse.
	Strict bool

	// FileName sets the template name used in error locations. Use it when the
	// template is read from a buffer or embedded filesystem, which carry no
	// file name of their own.
	FileName string
}

const (
	pxToPt             = 0.72
	cmToPt             = 28.3464
	mmToPt             = cmToPt / 10
	bezierCircleKappa  = 0.551784
	svgDPI             = 96
	defaultSVGFontSize = 16.0
)

// TextAlignment options for paragraph.
type TextAlignment int

// SetFontColor sets the font color for the paragraph.
func (sp *StyledParagraph) SetFontColor(color Color) {
	sp.defaultStyle.Color = color
	for _, chunk := range sp.chunks {
		chunk.Style.Color = color
	}
}

func callSetter(value reflect.Value, i any, values2 ...reflect.Value) []reflect.Value {
	if i == nil {
		return nil
	}
	typ := value.Type()
	values := make([]reflect.Value, 0, len(values2)+1)
	values = append(values, argOrZero(reflect.ValueOf(i), typ.In(0)))
	for i, argOrZero2 := range values2 {
		values = append(values, argOrZero(argOrZero2, typ.In(i+1)))
	}
	return value.Call(values)
}

func mergeColorspaces(resources, resources2 *model.PdfPageResources) error {
	colorspaces, _ := resources.GetColorspaces()
	if colorspaces != nil && len(colorspaces.Colorspaces) > 0 {
		for colorspaceKey, colorspace := range colorspaces.Colorspaces {
			name := *core.MakeName(colorspaceKey)
			if resources2.HasColorspaceByName(name) {
				continue
			}
			err := resources2.SetColorspaceByName(name, colorspace)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// ColorPoint is a pair of Color and a relative point where the color
// would be rendered.
type ColorPoint struct {
	color Color
	point float64
}

// Width is not used. The list component is designed to fill into the available
// width depending on the context. Returns 0.
func (li *List) Width() float64 { return 0 }

// SetHeight sets the custom height for the row.
func (gr *GridRow) SetHeight(h float64) { gr.height = h }

// SetOpacity sets opacity for border in range 0-1.
func (bo *border) SetOpacity(opacity float64) { bo.opacity = opacity }

// GridRow defines a row which can contain cells.
type GridRow struct {
	cells         []*GridCell
	height        float64
	contentHeight float64
	index         int
	grid          *Grid
	tagInfo       *model.StructureTagInfo
	section       GridRowSection
}

func (li *List) markerWidth() float64 {
	var markerWidth float64
	for _, item := range li.items {
		para := StyledParagraph{chunks: []*TextChunk{&item.marker}}
		markerWidth2 := para.getTextWidth() / 1000.0
		if markerWidth < markerWidth2 {
			markerWidth = markerWidth2
		}
	}
	return markerWidth
}

// Context returns the current drawing context.
func (c *Creator) Context() DrawContext { return c.context }

// SetHorizontalAlignment sets the cell's horizontal alignment of content.
// Can be one of:
// - CellHorizontalAlignmentLeft
// - CellHorizontalAlignmentCenter
// - CellHorizontalAlignmentRight
func (tc *TableCell) SetHorizontalAlignment(halign CellHorizontalAlignment) {
	tc.horizontalAlignment = halign
}
