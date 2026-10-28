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

// EnableTextShaping enables or disables advanced text shaping for the paragraph.
//
// This only has effect if the fonts used in the paragraph support text shaping (OTF fonts).
// Advanced text shaping is useful for rendering complex scripts, ligatures,
// and proper character positioning.
//
// By default, text shaping is disabled.
func (sp *StyledParagraph) EnableTextShaping(enable bool) { sp.enableShaping = enable }

func (sp *StyledParagraph) addLine(chunks []*TextChunk) bool {
	if sp.maxLines > 0 && len(sp.lines)+1 >= sp.maxLines {
		if len(chunks) > 0 {
			sp.lines = append(sp.lines, chunks)
		}
		return false
	}
	sp.lines = append(sp.lines, chunks)
	return true
}

// SetText replaces all the text of the paragraph with the specified one.
func (sp *StyledParagraph) SetText(text string) *TextChunk {
	sp.Reset()
	return sp.Append(text)
}

func (sp *StyledParagraph) getTextLineWidth(chunks []*TextChunk) float64 {
	var charSpacing float64
	n := len(chunks)
	for i, chunk := range chunks {
		style := &chunk.Style
		textLen := len(chunk.Text)
		for j, r := range chunk.Text {
			if r == '\u000A' {
				continue
			}
			runeMetrics, ok := style.Font.GetRuneMetrics(r)
			if !ok {
				common.Log.Debug("Rune char metrics not found! %v\n", r)
				return -1
			}
			charSpacing += style.FontSize * runeMetrics.Wx * style.horizontalScale()
			if r != ' ' && (i != n-1 || j != textLen-1) {
				charSpacing += style.CharSpacing * 1000.0
			}
		}
	}
	return charSpacing
}

// SetMaxLines sets the maximum number of lines to be drawn.
func (sp *StyledParagraph) SetMaxLines(maxLines int) { sp.maxLines = maxLines }

// GetTextVerticalAlignment returns the vertical alignment of the text within
// the bounds of the paragraph.
func (sp *StyledParagraph) GetTextVerticalAlignment() TextVerticalAlignment {
	return sp.verticalAlignment
}

// GetMargins returns the Paragraph's margins: left, right, top, bottom.
func (sp *StyledParagraph) GetMargins() (float64, float64, float64, float64) {
	return sp.margins.Left, sp.margins.Right, sp.margins.Top, sp.margins.Bottom
}

// GetChunks returns the text chunks that compose the paragraph. The returned
// slice references the paragraph's internal chunks; modifying its elements
// modifies the paragraph.
func (sp *StyledParagraph) GetChunks() []*TextChunk { return sp.chunks }

// AddInternalLink adds a new internal link to the paragraph.
// The text parameter represents the text that is displayed.
// The user is taken to the specified page, at the specified x and y
// coordinates. Position 0, 0 is at the top left of the page.
// The zoom of the destination page is controlled with the zoom
// parameter. Pass in 0 to keep the current zoom value.
func (sp *StyledParagraph) AddInternalLink(text string, page int64, x, y, zoom float64) *TextChunk {
	textChunk := NewTextChunk(text, sp.linkStyle)
	textChunk.AddAnnotation(newInternalLinkAnnotation(page-1, x, y, zoom, ""))
	return sp.appendChunk(textChunk)
}

// SetMargins sets the Paragraph's margins.
func (sp *StyledParagraph) SetMargins(left, right, top, bottom float64) {
	sp.margins.Left = left
	sp.margins.Right = right
	sp.margins.Top = top
	sp.margins.Bottom = bottom
}

// GetTextOverflow returns the behavior applied to paragraph text which does
// not fit in the available space.
func (sp *StyledParagraph) GetTextOverflow() TextOverflow { return sp.textOverflow }

// GetArtifact returns the artifact associated with the paragraph, if any.
func (sp *StyledParagraph) GetArtifact() *model.Artifact { return sp.artifact }

// Height returns the height of the Paragraph. The height is calculated based on the input text and how it is wrapped
// within the container. Does not include Margins.
func (sp *StyledParagraph) Height() float64 {
	sp.wrapText()
	var height float64
	for _, line := range sp.lines {
		var height2 float64
		for _, chunk := range line {
			fontSize := chunk.Style.effectiveLineHeight(sp.lineHeight) * chunk.Style.FontSize
			if fontSize > height2 {
				height2 = fontSize
			}
		}
		height += height2
	}
	return height
}

// AddHighlightedText adds a new highlighted text to the paragraph.
func (sp *StyledParagraph) AddHighlightedText(text string, color Color, alpha float64) *TextChunk {
	textChunk := NewTextChunk(text, sp.defaultStyle)
	textChunk.Highlight(color, alpha)
	return sp.appendChunk(textChunk)
}

func (sp *StyledParagraph) wrapText() error { return sp.wrapChunks(true) }

// dropCapsActive reports whether the paragraph renders a drop cap.
func (sp *StyledParagraph) dropCapsActive() bool {
	return len(sp.chunks) > 0 && sp.dropCaps != nil && sp.dropCaps.Type != DropCapsNone && sp.dropCapsEnabled
}

// GetPositioning returns the positioning mode (relative or absolute) of the
// paragraph.
func (sp *StyledParagraph) GetPositioning() Positioning { return sp.positioning }

// SetTextVerticalAlignment sets the vertical alignment of the text within the
// bounds of the styled paragraph.
//
// Note: Currently Styled Paragraph doesn't support TextVerticalAlignmentBottom
// as that option only used for aligning text chunks.
//
// In order to change the vertical alignment of individual text chunks, use TextChunk.VerticalAlignment.
func (sp *StyledParagraph) SetTextVerticalAlignment(align TextVerticalAlignment) {
	sp.verticalAlignment = align
}

// GetPos returns the absolute coordinates (x, y) of the paragraph. The
// coordinates are only meaningful when the paragraph uses absolute
// positioning (see GetPositioning).
func (sp *StyledParagraph) GetPos() (float64, float64) { return sp.xPos, sp.yPos }

// GetLineHeight returns the default line height multiplier of the paragraph.
func (sp *StyledParagraph) GetLineHeight() float64 { return sp.lineHeight }

// SetFontSize sets the font size for the paragraph.
func (sp *StyledParagraph) SetFontSize(fontSize float64) {
	sp.defaultStyle.FontSize = fontSize
	for _, chunk := range sp.chunks {
		chunk.Style.FontSize = fontSize
	}
}

// AddExternalLinkWithTag adds a new external link to the paragraph with proper tagging for accessibility.
// The text parameter represents the text that is displayed and the url parameter sets the destination.
// The options parameter contains accessibility properties like tooltip, altText, and mcid.
func (sp *StyledParagraph) AddExternalLinkWithTag(text, url string, options LinkTagOptions) (*TextChunk, *model.KDict) {
	uriLinkAnnotation := newURILinkAnnotation(url, options.Tooltip)
	accessibleLinkChunk, kDict, err := sp.createAccessibleLinkChunk(text, uriLinkAnnotation, options)
	if err != nil {
		common.Log.Error("Failed to add external link with tag: %v", err)
		return nil, nil
	}
	return accessibleLinkChunk, kDict
}

// SetPos sets absolute positioning with specified coordinates.
func (sp *StyledParagraph) SetPos(x, y float64) {
	sp.positioning = PositionAbsolute
	sp.xPos = x
	sp.yPos = y
}

// IsWrapEnabled returns whether line wrapping is enabled for the paragraph.
func (sp *StyledParagraph) IsWrapEnabled() bool { return sp.enableWrap }

func (sp *StyledParagraph) wrapChunks(ok2 bool) error {
	if !sp.enableWrap || int(sp.width) <= 0 {
		sp.lines = [][]*TextChunk{sp.chunks}
		return nil
	}
	for _, chunk := range sp.chunks {
		if chunk.Style.HorizontalScaling == 0 {
			chunk.Style.HorizontalScaling = DefaultHorizontalScaling
		}
	}
	if sp.enableWordWrap {
		sp.wrapWordChunks()
	}
	if sp.dropCaps != nil && len(sp.chunks) > 0 && len(sp.chunks[0].Text) > 0 {
		chunk := sp.chunks[0]
		dropCaps := sp.dropCaps
		if !core.IsTextWriteDirectionLTR(chunk.Text) {
			common.Log.Debug("Drop caps not supported for RTL text")
			sp.dropCaps = nil
		} else if dropCaps.Type != DropCapsNone && !sp.dropCapsEnabled {
			var chunk2 *TextChunk
			switch dropCaps.Scope {
			case DropCapsFirstCharacter:
				text := []rune(chunk.Text)
				if len(text) > 0 {
					chunk2 = chunk.clone()
					chunk2.Text = string(text[0])
					if dropCaps.Style != nil {
						chunk2.Style = *dropCaps.Style
					}
					if chunk2.Style.HorizontalScaling == 0 {
						chunk2.Style.HorizontalScaling = DefaultHorizontalScaling
					}
					if len(text) > 1 {
						chunk2 := chunk.clone()
						chunk2.Text = string(text[1:])
						if chunk2.Style.HorizontalScaling == 0 {
							chunk2.Style.HorizontalScaling = DefaultHorizontalScaling
						}
						sp.chunks[0] = chunk2
					}
				}
			case DropCapsFirstWord:
				text := chunk.Text
				indexAny := strings.IndexAny(text, " \t\n")
				chunk2 = chunk.clone()
				chunk3 := chunk.clone()
				if indexAny > 0 {
					chunk2.Text = text[:indexAny]
					chunk3.Text = strings.TrimSpace(text[indexAny:])
				} else {
					chunk2.Text = text
					chunk3.Text = ""
				}
				if dropCaps.Style != nil {
					chunk2.Style = *dropCaps.Style
				}
				if chunk2.Style.HorizontalScaling == 0 {
					chunk2.Style.HorizontalScaling = DefaultHorizontalScaling
				}
				if chunk3.Style.HorizontalScaling == 0 {
					chunk3.Style.HorizontalScaling = DefaultHorizontalScaling
				}
				sp.chunks[0] = chunk3
			default:
				text := []rune(chunk.Text)
				if len(text) > 0 {
					chunk2 = chunk.clone()
					chunk2.Text = string(text[0])
					if dropCaps.Style != nil {
						chunk2.Style = *dropCaps.Style
					}
					if chunk2.Style.HorizontalScaling == 0 {
						chunk2.Style.HorizontalScaling = DefaultHorizontalScaling
					}
					if len(text) > 1 {
						chunk2 := chunk.clone()
						chunk2.Text = string(text[1:])
						if chunk2.Style.HorizontalScaling == 0 {
							chunk2.Style.HorizontalScaling = DefaultHorizontalScaling
						}
						sp.chunks[0] = chunk2
					}
				}
			}
			if chunk2 != nil {
				numLines := dropCaps.NumLines
				if numLines <= 0 {
					numLines = 3
				}
				var font *model.PdfFont
				var fontSize float64
				if len(sp.chunks) > 0 {
					font = sp.chunks[0].Style.Font
					fontSize = sp.chunks[0].Style.FontSize
				} else if sp.defaultStyle.Font != nil {
					font = sp.defaultStyle.Font
					fontSize = sp.defaultStyle.FontSize
				} else {
					font = chunk2.Style.Font
					fontSize = chunk2.Style.FontSize
				}
				fontMetrics := newFontMetrics(font, fontSize)
				capHeight := fontMetrics.capHeight
				value := fontSize
				capHeight2 := capHeight + value
				if capHeight2 > value {
					value = capHeight2
				}
				capHeight3 := (value * float64(numLines)) - capHeight
				if capHeight3 <= 0 {
					capHeight3 = fontSize * float64(numLines)
				}
				chunk2.Style.FontSize = capHeight3
				sp.dropCapsEnabled = true
				chunks := sp.chunks
				sp.chunks = []*TextChunk{chunk2}
				sp.chunks = append(sp.chunks, chunks...)
			}
		}
	}
	sp.lines = [][]*TextChunk{}
	var chunks []*TextChunk
	var value float64
	f := unicode.IsSpace
	if !ok2 {
		f = func(rune) bool { return false }
	}
	value2 := roundTo(sp.width*1000.0, 0.000001)
	n := 0
	i2 := 0
	var chunk *TextChunk
	if len(sp.chunks) > 0 && sp.dropCaps != nil {
		dropCaps := sp.dropCaps
		if dropCaps.Type != DropCapsNone && sp.dropCapsEnabled {
			chunk = sp.chunks[0]
			i2 = 1
		}
	}
	for i := i2; i < len(sp.chunks); i++ {
		chunk2 := sp.chunks[i]
		style := chunk2.Style
		annotations := chunk2.annotations
		verticalAlignment := chunk2.VerticalAlignment
		var (
			rs     []rune
			floats []float64
		)
		isTextWriteDirectionLTR := core.IsTextWriteDirectionLTR(chunk2.Text)
		for _, r := range chunk2.Text {
			if r == '\u000A' {
				if !ok2 {
					rs = append(rs, r)
				}
				chunks = append(chunks, &TextChunk{taggedDrawable: chunk2.taggedDrawable, Text: strings.TrimRightFunc(string(rs), f), Style: style, annotations: copyAnnotations(annotations), VerticalAlignment: verticalAlignment, textExpansion: chunk2.textExpansion, actualText: chunk2.actualText, altText: chunk2.altText})
				if line := sp.addLine(chunks); !line {
					return nil
				}
				n++
				chunks = nil
				value = 0
				rs = nil
				floats = nil
				continue
			}
			ok3 := r == ' '
			runeMetrics, ok := style.Font.GetRuneMetrics(r)
			if runeMetrics.Wx == 0 && style.MultiFont != nil || style.MultiFont != nil && !ok {
				runeMetrics, ok = style.MultiFont.GetRuneMetrics(r)
			}
			if !ok {
				common.Log.Debug("Rune char metrics not found! %v\n", r)
				return errors.New("glyph char metrics missing")
			}
			value3 := style.FontSize * runeMetrics.Wx * style.horizontalScale()
			value4 := value3
			if !ok3 {
				value4 = value3 + style.CharSpacing*1000.0
			}
			value5 := value2
			if chunk != nil {
				dropCaps := sp.dropCaps
				var charSpacing float64
				text := []rune(chunk.Text)
				for i, r := range text {
					runeMetrics, ok := chunk.Style.Font.GetRuneMetrics(r)
					if ok {
						charSpacing += chunk.Style.FontSize * runeMetrics.Wx * chunk.Style.horizontalScale() / 1000.0
						if i < len(text)-1 {
							charSpacing += chunk.Style.CharSpacing
						}
					}
				}
				gap := dropCaps.Gap
				if gap <= 0 {
					gap = 5.0
				}
				switch dropCaps.Type {
				case DropCapsDrop:
					if n < dropCaps.NumLines {
						value5 = value2 - (charSpacing * 1000.0) - (gap * 1000.0)
					}
				case DropCapsInline:
					value5 = value2 - (charSpacing * 1000.0) - (gap * 1000.0)
				}
			}
			if value+value3 > value5 {
				n2 := -1
				if !ok3 {
					for i, r := range slices.Backward(rs) {
						if r == ' ' {
							n2 = i
							break
						}
					}
				}
				if sp.enableWordWrap {
					chunksLen := len(chunks)
					if chunksLen > 0 {
						chunks[chunksLen-1].Text = strings.TrimRightFunc(chunks[chunksLen-1].Text, f)
						sp.lines = append(sp.lines, chunks)
						chunks = []*TextChunk{}
					}
					rs = append(rs, r)
					floats = append(floats, value4)
					if n2 >= 0 {
						rs = rs[n2+1:]
						floats = floats[n2+1:]
					}
					value = 0
					for _, float := range floats {
						value += float
					}
					if value > value2 {
						formatWriteDirectionLTR := string(rs[:len(rs)-1])
						if !sp.enableShaping {
							formatWriteDirectionLTR = core.FormatWriteDirectionLTR(formatWriteDirectionLTR, isTextWriteDirectionLTR)
						}
						if !ok2 && ok3 {
							formatWriteDirectionLTR += " "
						}
						chunks = append(chunks, &TextChunk{taggedDrawable: chunk2.taggedDrawable, Text: strings.TrimRightFunc(formatWriteDirectionLTR, f), Style: style, annotations: copyAnnotations(annotations), VerticalAlignment: verticalAlignment, textExpansion: chunk2.textExpansion, actualText: chunk2.actualText, altText: chunk2.altText})
						if line := sp.addLine(chunks); !line {
							return nil
						}
						n++
						chunks = []*TextChunk{}
						rs = []rune{r}
						floats = []float64{value4}
						value = value4
					}
					continue
				}
				formatWriteDirectionLTR := string(rs)
				if n2 >= 0 {
					formatWriteDirectionLTR = string(rs[0 : n2+1])
					rs = rs[n2+1:]
					rs = append(rs, r)
					floats = floats[n2+1:]
					floats = append(floats, value4)
					value = 0
					for _, float := range floats {
						value += float
					}
				} else {
					if ok3 {
						value = 0
						rs = []rune{}
						floats = []float64{}
					} else {
						value = value4
						rs = []rune{r}
						floats = []float64{value4}
					}
				}
				if !sp.enableShaping {
					formatWriteDirectionLTR = core.FormatWriteDirectionLTR(formatWriteDirectionLTR, isTextWriteDirectionLTR)
				}
				if !ok2 && ok3 {
					formatWriteDirectionLTR += " "
				}
				chunks = append(chunks, &TextChunk{taggedDrawable: chunk2.taggedDrawable, Text: strings.TrimRightFunc(formatWriteDirectionLTR, f), Style: style, annotations: copyAnnotations(annotations), VerticalAlignment: verticalAlignment, textExpansion: chunk2.textExpansion, actualText: chunk2.actualText, altText: chunk2.altText})
				if line := sp.addLine(chunks); !line {
					return nil
				}
				n++
				chunks = []*TextChunk{}
			} else {
				value += value4
				rs = append(rs, r)
				floats = append(floats, value4)
			}
		}
		if len(rs) > 0 {
			formatWriteDirectionLTR := string(rs)
			if !sp.enableShaping {
				formatWriteDirectionLTR = core.FormatWriteDirectionLTR(formatWriteDirectionLTR, isTextWriteDirectionLTR)
			}
			chunks = append(chunks, &TextChunk{taggedDrawable: chunk2.taggedDrawable, Text: formatWriteDirectionLTR, Style: style, annotations: copyAnnotations(annotations), VerticalAlignment: verticalAlignment, textExpansion: chunk2.textExpansion, actualText: chunk2.actualText, altText: chunk2.altText})
		}
	}
	if len(chunks) > 0 {
		if line := sp.addLine(chunks); !line {
			return nil
		}
		n++
	}
	if chunk != nil {
		if len(sp.lines) > 0 {
			sp.lines[0] = append([]*TextChunk{chunk}, sp.lines[0]...)
		} else {
			sp.lines = append(sp.lines, []*TextChunk{chunk})
		}
	}
	return nil
}

var styledParagraphAttrs = map[string]attrApplyFunc{"text-align": enumAttr(textAlignmentValues, func(para *StyledParagraph, align TextAlignment) { para.SetTextAlignment(align) }), "vertical-text-align": enumAttr(textVerticalAlignmentValues, func(para *StyledParagraph, align TextVerticalAlignment) { para.SetTextVerticalAlignment(align) }), "line-height": floatAttr(func(para *StyledParagraph, lineheight float64) { para.SetLineHeight(lineheight) }), "margin": marginAttr(func(para *StyledParagraph, margins Margins) {
	para.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
}), "enable-wrap": boolAttr(func(para *StyledParagraph, enableWrap bool) { para.SetEnableWrap(enableWrap) }), "enable-word-wrap": boolAttr(func(para *StyledParagraph, val bool) { para.EnableWordWrap(val) }), "text-overflow": enumAttr(textOverflowValues, func(para *StyledParagraph, textOverflow TextOverflow) { para.SetTextOverflow(textOverflow) }), "x": floatAttr(func(para *StyledParagraph, x float64) { para.SetPos(x, para.yPos) }), "y": floatAttr(func(para *StyledParagraph, y float64) { para.SetPos(para.xPos, y) }), "angle": floatAttr(func(para *StyledParagraph, angle float64) { para.SetAngle(angle) })}

// SetStyle sets paragraph style for all chunks.
func (sp *StyledParagraph) SetStyle(style TextStyle) {
	sp.defaultStyle = style
	for _, chunk := range sp.chunks {
		chunk.Style = style
	}
}
