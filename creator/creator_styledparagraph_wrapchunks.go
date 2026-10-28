// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
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

// SetBorderColor sets the border color.
func (pbc *PolyBezierCurve) SetBorderColor(color Color) { pbc.curve.BorderColor = toPdfColor(color) }

// SetMargins sets the margins of the paragraph.
func (li *List) SetMargins(left, right, top, bottom float64) {
	li.margins.Left = left
	li.margins.Right = right
	li.margins.Top = top
	li.margins.Bottom = bottom
}
