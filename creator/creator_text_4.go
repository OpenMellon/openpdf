package creator

import (
	"errors"
	"slices"
	"strings"
	"unicode"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// TextChunk represents a chunk of text along with a particular style.
type TextChunk struct {
	taggedDrawable

	// The text that is being rendered in the PDF.
	Text string

	// The style of the text being rendered.
	Style       TextStyle
	annotations []*model.PdfAnnotation
	lineBreaks  []bool

	// The vertical alignment of the text chunk.
	VerticalAlignment TextVerticalAlignment
	textExpansion     *string
	actualText        *string
	altText           *string
}

// Highlight adds a highlight annotation to the text chunk with the specified color and alpha value.
func (tec *TextChunk) Highlight(color Color, alpha float64) *model.PdfAnnotation {
	r, g, b := color.ToRGB()
	pdfAnnotationHighlight := model.NewPdfAnnotationHighlight()
	pdfAnnotationHighlight.C = core.MakeArrayFromFloats([]float64{r, g, b})
	pdfAnnotationHighlight.CA = core.MakeFloat(alpha)
	tec.AddAnnotation(pdfAnnotationHighlight.PdfAnnotation)
	return pdfAnnotationHighlight.PdfAnnotation
}

// WrappedHeight returns the total height the chunk would occupy when wrapped
// to the given width: number of wrapped lines × Height().
func (tec *TextChunk) WrappedHeight(width float64) (float64, error) {
	strs, err := tec.Wrap(width)
	if err != nil {
		return 0, err
	}
	return float64(len(strs)) * tec.Height(), nil
}

// NewTextChunk returns a new text chunk instance.
func NewTextChunk(text string, style TextStyle) *TextChunk {
	return &TextChunk{Text: text, Style: style, VerticalAlignment: TextVerticalAlignmentBaseline, annotations: []*model.PdfAnnotation{}, lineBreaks: []bool{}, taggedDrawable: taggedDrawable{structType: model.StructureTypeSpan}}
}

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

// SetAltText sets the alternative text for the text chunk.
func (tec *TextChunk) SetAltText(text string) { tec.altText = &text }

// SetActualText sets the actual text for the text chunk.
func (tec *TextChunk) SetActualText(text string) { tec.actualText = &text }

// SetTextExpansion sets the text expansion for the text chunk.
func (tec *TextChunk) SetTextExpansion(text string) { tec.textExpansion = &text }

// SetAnnotation sets an annotation on a TextChunk,
// this will replace any existing annotation that has been set.
// Supplying a nil value as parameter would clear all the annotations.
func (tec *TextChunk) SetAnnotation(annotation *model.PdfAnnotation) {
	if annotation == nil {
		tec.ClearAnnotations()
	} else {
		tec.annotations = []*model.PdfAnnotation{annotation}
	}
}

// Wrap wraps the text of the chunk into lines based on its style and the
// specified width.
func (tec *TextChunk) Wrap(width float64) ([]string, error) {
	if int(width) <= 0 {
		return []string{tec.Text}, nil
	}
	var strs []string
	var rs []rune
	var value float64
	var floats []float64
	style := tec.Style
	isTextWriteDirectionLTR := core.IsTextWriteDirectionLTR(tec.Text)
	for _, r := range tec.Text {
		if r == '\u000A' {
			formatWriteDirectionLTR := core.FormatWriteDirectionLTR(string(rs), isTextWriteDirectionLTR)
			strs = append(strs, strings.TrimRightFunc(formatWriteDirectionLTR, unicode.IsSpace)+string(r))
			rs = nil
			value = 0
			floats = nil
			continue
		}
		ok2 := r == ' '
		runeMetrics, ok := style.Font.GetRuneMetrics(r)
		if !ok {
			common.Log.Debug("ERROR: Rune char metrics not found! rune=0x%04x=%c font=%s %#q", r, r, style.Font.BaseFont(), style.Font.Subtype())
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Font: %#v", style.Font)
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Encoder: %#v", style.Font.Encoder())
			}
			return nil, errors.New("glyph char metrics missing")
		}
		fontSize := style.FontSize * runeMetrics.Wx
		fontSize2 := fontSize
		if !ok2 {
			fontSize2 = fontSize + style.CharSpacing*1000.0
		}
		if value+fontSize > width*1000.0 {
			n := -1
			if !ok2 {
				for i, r := range slices.Backward(rs) {
					if r == ' ' {
						n = i
						break
					}
				}
			}
			formatWriteDirectionLTR := string(rs)
			if n > 0 {
				formatWriteDirectionLTR = string(rs[0 : n+1])
				rs = append(rs[n+1:], r)
				floats = append(floats[n+1:], fontSize2)
				value = 0
				for _, float := range floats {
					value += float
				}
			} else {
				if ok2 {
					rs = []rune{}
					floats = []float64{}
					value = 0
				} else {
					rs = []rune{r}
					floats = []float64{fontSize2}
					value = fontSize2
				}
			}
			formatWriteDirectionLTR = core.FormatWriteDirectionLTR(formatWriteDirectionLTR, isTextWriteDirectionLTR)
			strs = append(strs, strings.TrimRightFunc(formatWriteDirectionLTR, unicode.IsSpace))
		} else {
			rs = append(rs, r)
			value += fontSize2
			floats = append(floats, fontSize2)
		}
	}
	if len(rs) > 0 {
		formatWriteDirectionLTR := string(rs)
		formatWriteDirectionLTR = core.FormatWriteDirectionLTR(formatWriteDirectionLTR, isTextWriteDirectionLTR)
		strs = append(strs, formatWriteDirectionLTR)
	}
	return strs, nil
}

// Height returns the height of a single line of this chunk: FontSize × effective
// line height. When Style.LineHeight is 0 (the default), the effective line
// height is 1.0. To account for the line height set on a containing paragraph,
// callers should either propagate it into Style.LineHeight or multiply the
// result accordingly.
func (tec *TextChunk) Height() float64 {
	return tec.Style.FontSize * tec.Style.effectiveLineHeight(1.0)
}

// AddAnnotation adds an annotation on a TextChunk.
func (tec *TextChunk) AddAnnotation(annotation *model.PdfAnnotation) {
	if annotation == nil {
		return
	}
	tec.annotations = append(tec.annotations, annotation)
}

// Fit fits the chunk into the specified bounding box, cropping off the
// remainder in a new chunk, if it exceeds the specified dimensions.
// The line height is taken from Style.LineHeight when set (> 0); otherwise a
// multiplier of 1.0 is assumed, matching the historical behavior. To account
// for a line height applied at the paragraph level, propagate it into
// Style.LineHeight before calling Fit.
func (tec *TextChunk) Fit(width, height float64) (*TextChunk, error) {
	strs, err := tec.Wrap(width)
	if err != nil {
		return nil, err
	}
	height2 := tec.Height()
	if height2 <= 0 {
		return nil, errors.New("text chunk fit: non-positive effective line height")
	}
	height3 := int(height / height2)
	if height3 >= len(strs) {
		return nil, nil
	}
	fresh := "\n"
	tec.Text = strings.ReplaceAll(strings.Join(strs[:height3], " "), fresh+" ", fresh)
	text := strings.ReplaceAll(strings.Join(strs[height3:], " "), fresh+" ", fresh)
	return NewTextChunk(text, tec.Style), nil
}

// Width returns the width of the specified text chunk.
func (tec *TextChunk) Width() float64 {
	var (
		value float64
		style = tec.Style
	)
	for _, r := range tec.Text {
		runeMetrics, ok := style.Font.GetRuneMetrics(r)
		if !ok {
			common.Log.Debug("ERROR: Rune char metrics not found! rune=0x%04x=%c font=%s %#q", r, r, style.Font.BaseFont(), style.Font.Subtype())
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Font: %#v", style.Font)
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Encoder: %#v", style.Font.Encoder())
			}
		}
		fontSize := style.FontSize * runeMetrics.Wx
		fontSize2 := fontSize
		if r != ' ' {
			fontSize2 = fontSize + style.CharSpacing*1000.0
		}
		value += fontSize2
	}
	return value / 1000.0
}

func (tec *TextChunk) clone() *TextChunk {
	clone := *tec
	clone.annotations = copyAnnotations(tec.annotations)
	return &clone
}

func parseTextChunkTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseTextChunk(node, nil)
}

// ClearAnnotations clears any existing annotations.
func (tec *TextChunk) ClearAnnotations() { tec.annotations = []*model.PdfAnnotation{} }

// AssociateAnnotationWithStructure associates the given annotation with the text chunk's structure element.
// This is necessary for proper tag structure when using annotations in tagged PDFs.
func (tec *TextChunk) AssociateAnnotationWithStructure(annotation *model.PdfAnnotation) {
	if tec.tagInfo == nil {
		tec.tagInfo = model.NewStructureTagInfo()
		tec.tagInfo.StructureType = model.StructureTypeSpan
	}
	containingPdfObject := annotation.GetContainingPdfObject()
	tec.tagInfo.SetObjAttrib(containingPdfObject)
	if dict, ok := core.GetDict(containingPdfObject); ok {
		dict.Set("StructParent", core.MakeInteger(tec.tagInfo.Mcid))
	}
}

var textChunkAttrs = map[string]attrApplyFunc{"color": colorAttr(func(chunk *TextChunk, color Color) { chunk.Style.Color = color }), "outline-color": colorAttr(func(chunk *TextChunk, outlineColor Color) { chunk.Style.OutlineColor = outlineColor }), "font": fontAttr(func(chunk *TextChunk, font *model.PdfFont) { chunk.Style.Font = font }), "font-size": floatAttr(func(chunk *TextChunk, fontSize float64) { chunk.Style.FontSize = fontSize }), "line-height": floatAttr(func(chunk *TextChunk, lineHeight float64) { chunk.Style.LineHeight = lineHeight }), "outline-size": floatAttr(func(chunk *TextChunk, outlineSize float64) { chunk.Style.OutlineSize = outlineSize }), "character-spacing": floatAttr(func(chunk *TextChunk, charSpacing float64) { chunk.Style.CharSpacing = charSpacing }), "horizontal-scaling": floatAttr(func(chunk *TextChunk, horizontalScaling float64) { chunk.Style.HorizontalScaling = horizontalScaling }), "rendering-mode": enumAttr(textRenderingModeValues, func(chunk *TextChunk, renderingMode TextRenderingMode) { chunk.Style.RenderingMode = renderingMode }), "underline": boolAttr(func(chunk *TextChunk, underline bool) { chunk.Style.Underline = underline }), "underline-color": colorAttr(func(chunk *TextChunk, color Color) { chunk.Style.UnderlineStyle.Color = color }), "underline-offset": floatAttr(func(chunk *TextChunk, offset float64) { chunk.Style.UnderlineStyle.Offset = offset }), "underline-thickness": floatAttr(func(chunk *TextChunk, thickness float64) { chunk.Style.UnderlineStyle.Thickness = thickness }), "text-rise": floatAttr(func(chunk *TextChunk, textRise float64) { chunk.Style.TextRise = textRise }), "link": nil}

// TextVerticalAlignment controls the vertical position of the text
// in a styled paragraph.
type TextVerticalAlignment int

const (
	TextVerticalAlignmentBaseline TextVerticalAlignment = iota
	TextVerticalAlignmentCenter
	TextVerticalAlignmentBottom
	TextVerticalAlignmentTop
)

// TextDecorationLineStyle represents the style of lines used to decorate
// a text chunk (e.g. underline).
type TextDecorationLineStyle struct {
	// Color represents the color of the line (default: the color of the text).
	Color Color

	// Offset represents the vertical offset of the line (default: 1).
	Offset float64

	// Thickness represents the thickness of the line (default: 1).
	Thickness float64
}

// TextRenderingMode determines whether showing text shall cause glyph
// outlines to be stroked, filled, used as a clipping boundary, or some
// combination of the three.
// See section 9.3 "Text State Parameters and Operators" and
// Table 106 (pp. 254-255 PDF32000_2008).
type TextRenderingMode int

const (
	TextRenderingModeFill TextRenderingMode = iota
	TextRenderingModeStroke
	TextRenderingModeFillStroke
	TextRenderingModeInvisible
	TextRenderingModeFillClip
	TextRenderingModeStrokeClip
	TextRenderingModeFillStrokeClip
	TextRenderingModeClip
)

var styledTextTags = map[string]struct{}{"paragraph": {}, "text-chunk": {}, "chapter-heading": {}, "list-marker": {}}

// LinkTagOptions represents optional parameters for tagged link annotations,
// providing flexibility for adding accessibility features.
type LinkTagOptions struct {
	// MCID is the marked content identifier for structure tree association
	MCID int64

	// AltText is alternative text for screen readers (only used if different from visible text)
	AltText string

	// Tooltip sets the annotation's Contents field (tooltip on hover)
	Tooltip string
}

func linkTextStyle(font *model.PdfFont) TextStyle {
	return TextStyle{Color: ColorRGBFrom8bit(0, 0, 238), Font: font, FontSize: 10, OutlineSize: 1, HorizontalScaling: DefaultHorizontalScaling, UnderlineStyle: TextDecorationLineStyle{Offset: 1, Thickness: 1}}
}

// DropCapsOptions contains configuration options for drop caps rendering.
type DropCapsOptions struct {
	// Type specifies the drop caps type (Drop or Inline).
	Type DropCapsType

	// Scope defines what portion of the paragraph text gets drop caps treatment.
	// Default: DropCapsFirstCharacter
	Scope DropCapsScope

	// NumLines specifies how many lines the drop cap should span.
	// For DropCapsDrop: how many lines flow beside before continuing below.
	// For DropCapsInline: affects the size of the drop cap character.
	// Default: 3 lines.
	NumLines int

	// Gap specifies the horizontal distance between the drop cap and the text.
	// Measured in points. Default: 5.0 points.
	Gap float64

	// Style specifies the text style to apply to the drop cap character(s).
	// This allows customization of font, color, outline, and other text properties.
	// If nil, the drop cap will inherit the style from the first chunk of text.
	// Note: The FontSize in this style will be overridden by the calculated size
	// based on NumLines, but other properties like Color, Font, OutlineColor,
	// CharSpacing, etc. will be applied.
	Style *TextStyle
}

// DropCapsType represents the type of drop caps to apply to a paragraph.
// Note: Drop caps are currently not supported for RTL (right-to-left) text and will be
// automatically disabled with a debug log if applied to RTL content.
type DropCapsType int

const (
	DropCapsNone DropCapsType = iota
	DropCapsDrop
	DropCapsInline
)

type graphemeCluster struct {
	runes []rune
	text  string
}

// TextStyle is a collection of properties that can be assigned to a text chunk.
type TextStyle struct {
	// Color represents the color of the text.
	Color Color

	// OutlineColor represents the color of the text outline.
	OutlineColor Color

	// MultiFont represents an encoder that accepts multiple fonts and selects the correct font for encoding.
	MultiFont *model.MultipleFontEncoder

	// Font represents the font the text will use.
	Font *model.PdfFont

	// FontSize represents the size of the font.
	FontSize float64

	// LineHeight is the line height multiplier applied to FontSize when this
	// chunk contributes to a line's vertical advance. A value of 0 (default)
	// means the chunk inherits the line-height setting of its containing
	// paragraph (default 1.0).
	LineHeight float64

	// OutlineSize represents the thickness of the text outline.
	OutlineSize float64

	// CharSpacing represents the character spacing.
	CharSpacing float64

	// HorizontalScaling represents the percentage to horizontally scale
	// characters by (default: 100). Values less than 100 will result in
	// narrower text while values greater than 100 will result in wider text.
	HorizontalScaling float64

	// RenderingMode represents the rendering mode.
	RenderingMode TextRenderingMode

	// Underline specifies if the text chunk is underlined.
	Underline bool

	// UnderlineStyle represents the style of the line used to underline text.
	UnderlineStyle TextDecorationLineStyle

	// TextRise specifies a vertical adjustment for text. It is useful for
	// drawing subscripts/superscripts. A positive text rise value will
	// produce superscript text, while a negative one will result in
	// subscript text.
	TextRise float64
}

func defaultTextStyle(font *model.PdfFont) TextStyle {
	return TextStyle{Color: ColorRGBFrom8bit(0, 0, 0), Font: font, FontSize: 10, OutlineSize: 1, HorizontalScaling: DefaultHorizontalScaling, UnderlineStyle: TextDecorationLineStyle{Offset: 1, Thickness: 1}}
}

func (ts *TextStyle) effectiveLineHeight(effectiveLineHeight float64) float64 {
	if ts.LineHeight > 0 {
		return ts.LineHeight
	}
	return effectiveLineHeight
}

func (ts *TextStyle) horizontalScale() float64 { return ts.HorizontalScaling / 100 }
