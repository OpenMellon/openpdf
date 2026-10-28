package model

import (
	_ "image/gif"
	"slices"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
	typefont "github.com/unidoc/typesetting/font"
)

// ColorGlyphInfo represents information about a color glyph.
// This supports multiple color glyph formats found in modern fonts:
// - SVG-based color glyphs (emoji fonts)
// - Bitmap-based color glyphs (PNG, JPG, TIFF formats)
// - Future support for layered color glyphs (COLR/CPAL tables)
type ColorGlyphInfo struct {
	GlyphID      typefont.GID
	GlyphType    ColorGlyphType
	SVGData      []byte
	BitmapData   []byte
	BitmapFormat typefont.BitmapFormat
	BitmapWidth  int
	BitmapHeight int
	OutlineData  typefont.GlyphOutline
}

// ColorGlyphType indicates the type of color glyph data available.
// Different font formats use different approaches to store color information.
type ColorGlyphType int

// GetImageData returns the raw image data that can be used to create an image file.
// For SVG glyphs, returns the SVG source. For bitmap glyphs, returns the raw image data.
// Returns the data, MIME type, and file extension.
func (g *ColorGlyphInfo) GetImageData() (data []byte, s string, s2 string) {
	switch g.GlyphType {
	case ColorGlyphTypeSVG:
		if len(g.SVGData) > 0 {
			return g.SVGData, "image/svg+xml", ".svg"
		}
	case ColorGlyphTypeBitmap:
		if len(g.BitmapData) > 0 {
			switch g.BitmapFormat {
			case typefont.PNG:
				return g.BitmapData, "image/png", ".png"
			case typefont.JPG:
				return g.BitmapData, "image/jpeg", ".jpg"
			case typefont.TIFF:
				return g.BitmapData, "image/tiff", ".tiff"
			case typefont.BlackAndWhite:
				return g.BitmapData, "image/png", ".png"
			}
		}
	}
	return nil, "", ""
}

// GetColorGlyphInfoByRune retrieves color glyph information for a rune from an OTF font
func GetColorGlyphInfoByRune(otf *fonts.OtfType, r rune) *ColorGlyphInfo {
	if otf.Face == nil {
		return nil
	}
	if char, ok := otf.Chars[r]; ok {
		if colorGlyphInfo, ok := GetColorGlyphInfo(otf, typefont.GID(char)); ok {
			return colorGlyphInfo
		} else {
			common.Log.Debug("Glyph %d for rune %c has no color information", char, r)
		}
	} else {
		common.Log.Debug("No glyph found for rune %c (U+%04X)", r, r)
	}
	return nil
}

const (
	ColorGlyphTypeSVG ColorGlyphType = iota
	ColorGlyphTypeBitmap
	ColorGlyphTypeOutline
)

// GetColorGlyphInfo retrieves color glyph information for a given glyph ID
func GetColorGlyphInfo(otf *fonts.OtfType, glyphID typefont.GID) (*ColorGlyphInfo, bool) {
	if otf.Face == nil {
		return nil, false
	}
	glyphData := otf.Face.GlyphData(glyphID)
	if glyphData == nil {
		return nil, false
	}
	glyphInfo := &ColorGlyphInfo{GlyphID: glyphID}
	switch v := glyphData.(type) {
	case typefont.GlyphSVG:
		glyphInfo.GlyphType = ColorGlyphTypeSVG
		glyphInfo.SVGData = v.Source
		return glyphInfo, true
	case typefont.GlyphBitmap:
		glyphInfo.GlyphType = ColorGlyphTypeBitmap
		glyphInfo.BitmapData = v.Data
		glyphInfo.BitmapFormat = v.Format
		glyphInfo.BitmapWidth = v.Width
		glyphInfo.BitmapHeight = v.Height
		return glyphInfo, true
	default:
		return nil, false
	}
}

// GetColorGlyphFormat returns a string description of the bitmap format
func (g *ColorGlyphInfo) GetColorGlyphFormat() string {
	switch g.GlyphType {
	case ColorGlyphTypeSVG:
		return "SVG"
	case ColorGlyphTypeBitmap:
		switch g.BitmapFormat {
		case typefont.PNG:
			return "PNG"
		case typefont.JPG:
			return "JPEG"
		case typefont.TIFF:
			return "TIFF"
		case typefont.BlackAndWhite:
			return "Monochrome Bitmap"
		default:
			return "Unknown Bitmap"
		}
	default:
		return "Unknown"
	}
}

// GetImageSize returns the image dimensions if available.
// For SVG glyphs, this may return 0,0 as SVG is scalable.
func (g *ColorGlyphInfo) GetImageSize() (n, count int) {
	switch g.GlyphType {
	case ColorGlyphTypeSVG:
		return 0, 0
	case ColorGlyphTypeBitmap:
		return g.BitmapWidth, g.BitmapHeight
	}
	return 0, 0
}

// GetColorGlyphInfoByText retrieves color glyph information for a text string from an OTF font.
// This function handles multi-rune sequences like ZWJ emoji by using text shaping to find the
// appropriate glyph. Text can be a single rune or a multi-rune sequence (e.g., "🙋‍♂️").
func GetColorGlyphInfoByText(otf *fonts.OtfType, text string) *ColorGlyphInfo {
	if otf.Face == nil {
		return nil
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	if len(runes) == 1 {
		return GetColorGlyphInfoByRune(otf, runes[0])
	}
	shapeTextLine := textencoding.ShapeTextLine(otf.Face, text, 10.0)
	if len(shapeTextLine) == 0 {
		return nil
	}
	for _, output := range shapeTextLine {
		if len(output.Glyphs) == 0 {
			continue
		}
		if len(output.Glyphs) > 1 {
			if slices.Contains(runes, 0x200D) {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Font does not have a combined glyph for ZWJ sequence '%s' (got %d glyphs instead of 1). Falling back.", text, len(output.Glyphs))
				}
				if len(runes) >= 2 && runes[1] >= 0x1F3FB && runes[1] <= 0x1F3FF {
					s := string(runes[:2])
					shapeTextLine := textencoding.ShapeTextLine(otf.Face, s, 10.0)
					for _, output := range shapeTextLine {
						if len(output.Glyphs) == 1 {
							if colorGlyphInfo, ok := GetColorGlyphInfo(otf, output.Glyphs[0].GlyphID); ok {
								return colorGlyphInfo
							}
						}
					}
				}
				return GetColorGlyphInfoByRune(otf, runes[0])
			}
		}
		glyphID := output.Glyphs[0].GlyphID
		if colorGlyphInfo, ok := GetColorGlyphInfo(otf, glyphID); ok {
			return colorGlyphInfo
		}
	}
	return nil
}
