package model

import (
	_ "image/gif"
	"math"
	"slices"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
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

// ColorGlyphType indicates the type of color glyph data available.
// Different font formats use different approaches to store color information.
type ColorGlyphType int

const (
	ColorGlyphTypeSVG ColorGlyphType = iota
	ColorGlyphTypeBitmap
	ColorGlyphTypeOutline
)

// PdfColorPattern represents a pattern color.
type PdfColorPattern struct {
	Color       PdfColor
	PatternName core.PdfObjectName
}

// Colorspace returns a Pattern colorspace whose underlying colorspace, if any,
// is derived from the underlying color.
func (p *PdfColorPattern) Colorspace() PdfColorspace {
	specialPatternCS := NewPdfColorspaceSpecialPattern()
	if p.Color != nil {
		specialPatternCS.UnderlyingCS = p.Color.Colorspace()
	}
	return specialPatternCS
}

// Components returns the component values of the underlying color, or nil if
// the pattern has no underlying color (e.g. an uncolored tiling pattern).
func (p *PdfColorPattern) Components() []float64 {
	if p.Color == nil {
		return nil
	}
	return p.Color.Components()
}

// NewPdfColorPattern returns an empty color pattern.
func NewPdfColorPattern() *PdfColorPattern { patternColor := &PdfColorPattern{}; return patternColor }

// PdfColorDeviceCMYK is a CMYK32 color, where each component is defined in the range 0.0 - 1.0 where 1.0 is the primary intensity.
type PdfColorDeviceCMYK [4]float64

// NewPdfColorDeviceCMYK returns a new CMYK32 color.
func NewPdfColorDeviceCMYK(c, m, y, k float64) *PdfColorDeviceCMYK {
	deviceCMYKColor := PdfColorDeviceCMYK{c, m, y, k}
	return &deviceCMYKColor
}

// GetNumComponents returns the number of color components (4 for CMYK32).
func (c *PdfColorDeviceCMYK) GetNumComponents() int { return 4 }

// Components returns the color's component values: C, M, Y, K.
func (c *PdfColorDeviceCMYK) Components() []float64 {
	return []float64{c[0], c[1], c[2], c[3]}
}

// C returns the value of the cyan component of the color.
func (c *PdfColorDeviceCMYK) C() float64 { return c[0] }

// ToInteger convert to an integer format.
func (c *PdfColorDeviceCMYK) ToInteger(bits int) [4]uint32 {
	f := math.Pow(2, float64(bits)) - 1
	return [4]uint32{uint32(f * c.C()), uint32(f * c.M()), uint32(f * c.Y()), uint32(f * c.K())}
}

// Colorspace returns the DeviceCMYK colorspace.
func (c *PdfColorDeviceCMYK) Colorspace() PdfColorspace { return NewPdfColorspaceDeviceCMYK() }

// M returns the value of the magenta component of the color.
func (c *PdfColorDeviceCMYK) M() float64 { return c[1] }

// K returns the value of the key component of the color.
func (c *PdfColorDeviceCMYK) K() float64 { return c[3] }

// Y returns the value of the yellow component of the color.
func (c *PdfColorDeviceCMYK) Y() float64 { return c[2] }

// PdfColorCalRGB represents a color in the Colorimetric CIE RGB colorspace.
// A, B, C components
// Each component is defined in the range 0.0 - 1.0 where 1.0 is the primary intensity.
type PdfColorCalRGB [3]float64

// B returns the value of the B component of the color.
func (c *PdfColorCalRGB) B() float64 { return c[1] }

// C returns the value of the C component of the color.
func (c *PdfColorCalRGB) C() float64 { return c[2] }

// ToInteger convert to an integer format.
func (c *PdfColorCalRGB) ToInteger(bits int) [3]uint32 {
	f := math.Pow(2, float64(bits)) - 1
	return [3]uint32{uint32(f * c.A()), uint32(f * c.B()), uint32(f * c.C())}
}

// Colorspace returns a best-effort, parameterless CalRGB colorspace. The
// authoritative parameterized colorspace is tracked by the graphics state.
func (c *PdfColorCalRGB) Colorspace() PdfColorspace { return NewPdfColorspaceCalRGB() }

// Components returns the color's component values: A, B, C.
func (c *PdfColorCalRGB) Components() []float64 { return []float64{c[0], c[1], c[2]} }

// NewPdfColorCalRGB returns a new CalRBG color.
func NewPdfColorCalRGB(a, b, c float64) *PdfColorCalRGB {
	calRGBColor := PdfColorCalRGB{a, b, c}
	return &calRGBColor
}

// A returns the value of the A component of the color.
func (c *PdfColorCalRGB) A() float64 { return c[0] }

// GetNumComponents returns the number of color components (3 for CalRGB).
func (c *PdfColorCalRGB) GetNumComponents() int { return 3 }

// PdfColorCalGray represents a CalGray colorspace.
type PdfColorCalGray float64

// Colorspace returns a best-effort, parameterless CalGray colorspace. The
// authoritative parameterized colorspace is tracked by the graphics state.
func (c *PdfColorCalGray) Colorspace() PdfColorspace { return NewPdfColorspaceCalGray() }

// NewPdfColorCalGray returns a new CalGray color.
func NewPdfColorCalGray(grayVal float64) *PdfColorCalGray {
	calGrayColor := PdfColorCalGray(grayVal)
	return &calGrayColor
}

// GetNumComponents returns the number of color components (1 for CalGray).
func (c *PdfColorCalGray) GetNumComponents() int { return 1 }

// Components returns the color's component values: the single gray value.
func (c *PdfColorCalGray) Components() []float64 { return []float64{float64(*c)} }

// ToInteger convert to an integer format.
func (c *PdfColorCalGray) ToInteger(bits int) uint32 {
	f := math.Pow(2, float64(bits)) - 1
	return uint32(f * c.Val())
}

// Val returns the value of the color.
func (c *PdfColorCalGray) Val() float64 { return float64(*c) }

// PdfColorDeviceRGB represents a color in DeviceRGB colorspace with R, G, B components, where component is
// defined in the range 0.0 - 1.0 where 1.0 is the primary intensity.
type PdfColorDeviceRGB [3]float64

// NewPdfColorDeviceRGB returns a new PdfColorDeviceRGB based on the r,g,b component values.
func NewPdfColorDeviceRGB(r, g, b float64) *PdfColorDeviceRGB {
	deviceRGBColor := PdfColorDeviceRGB{r, g, b}
	return &deviceRGBColor
}

// G returns the value of the green component of the color.
func (c *PdfColorDeviceRGB) G() float64 { return c[1] }

// Colorspace returns the DeviceRGB colorspace.
func (c *PdfColorDeviceRGB) Colorspace() PdfColorspace { return NewPdfColorspaceDeviceRGB() }

// ToInteger convert to an integer format.
func (c *PdfColorDeviceRGB) ToInteger(bits int) [3]uint32 {
	f := math.Pow(2, float64(bits)) - 1
	return [3]uint32{uint32(f * c.R()), uint32(f * c.G()), uint32(f * c.B())}
}

// Components returns the color's component values: R, G, B.
func (c *PdfColorDeviceRGB) Components() []float64 {
	return []float64{c[0], c[1], c[2]}
}

// ToGray returns a PdfColorDeviceGray color based on the current RGB color.
func (c *PdfColorDeviceRGB) ToGray() *PdfColorDeviceGray {
	f := 0.3*c.R() + 0.59*c.G() + 0.11*c.B()
	f = math.Min(math.Max(f, 0.0), 1.0)
	return NewPdfColorDeviceGray(f)
}

// B returns the value of the blue component of the color.
func (c *PdfColorDeviceRGB) B() float64 { return c[2] }

// GetNumComponents returns the number of color components (3 for RGB).
func (c *PdfColorDeviceRGB) GetNumComponents() int { return 3 }

// R returns the value of the red component of the color.
func (c *PdfColorDeviceRGB) R() float64 { return c[0] }

// PdfColorPatternType1 represents a color shading pattern type 1 (Function-based).
type PdfColorPatternType1 struct {
	Color       PdfColor
	PatternName core.PdfObjectName
}

// Colorspace returns a Pattern colorspace whose underlying colorspace, if any,
// is derived from the underlying color.
func (p *PdfColorPatternType1) Colorspace() PdfColorspace {
	specialPatternCS := NewPdfColorspaceSpecialPattern()
	if p.Color != nil {
		specialPatternCS.UnderlyingCS = p.Color.Colorspace()
	}
	return specialPatternCS
}

// NewPdfColorPatternType1 returns an empty color shading pattern type 1 (Function-based).
func NewPdfColorPatternType1() *PdfColorPatternType1 {
	patternType1Color := &PdfColorPatternType1{}
	return patternType1Color
}

// Components returns the component values of the underlying color, or nil if
// the pattern has no underlying color.
func (p *PdfColorPatternType1) Components() []float64 {
	if p.Color == nil {
		return nil
	}
	return p.Color.Components()
}

// PdfColorDeviceGray represents a grayscale color value that shall be represented by a single number in the
// range 0.0 to 1.0 where 0.0 corresponds to black and 1.0 to white.
type PdfColorDeviceGray float64

// Components returns the color's component values: the single gray value.
func (c *PdfColorDeviceGray) Components() []float64 { return []float64{float64(*c)} }

// NewPdfColorDeviceGray returns a new grayscale color based on an input grayscale float value in range [0-1].
func NewPdfColorDeviceGray(grayVal float64) *PdfColorDeviceGray {
	deviceGrayColor := PdfColorDeviceGray(grayVal)
	return &deviceGrayColor
}

// ToInteger convert to an integer format.
func (c *PdfColorDeviceGray) ToInteger(bits int) uint32 {
	f := math.Pow(2, float64(bits)) - 1
	return uint32(f * c.Val())
}

// Val returns the color value.
func (c *PdfColorDeviceGray) Val() float64 { return float64(*c) }

// GetNumComponents returns the number of color components (1 for grayscale).
func (c *PdfColorDeviceGray) GetNumComponents() int { return 1 }

// Colorspace returns the DeviceGray colorspace.
func (c *PdfColorDeviceGray) Colorspace() PdfColorspace { return NewPdfColorspaceDeviceGray() }

// PdfColorPatternType2 represents a color shading pattern type 2 (Axial).
type PdfColorPatternType2 struct {
	Color       PdfColor
	PatternName core.PdfObjectName
}

// Colorspace returns a Pattern colorspace whose underlying colorspace, if any,
// is derived from the underlying color.
func (p *PdfColorPatternType2) Colorspace() PdfColorspace {
	specialPatternCS := NewPdfColorspaceSpecialPattern()
	if p.Color != nil {
		specialPatternCS.UnderlyingCS = p.Color.Colorspace()
	}
	return specialPatternCS
}

// NewPdfColorPatternType2 returns an empty color shading pattern type 2 (Axial).
func NewPdfColorPatternType2() *PdfColorPatternType2 {
	patternType2Color := &PdfColorPatternType2{}
	return patternType2Color
}

// Components returns the component values of the underlying color, or nil if
// the pattern has no underlying color.
func (p *PdfColorPatternType2) Components() []float64 {
	if p.Color == nil {
		return nil
	}
	return p.Color.Components()
}

// PdfColorPatternType3 represents a color shading pattern type 3 (Radial).
type PdfColorPatternType3 struct {
	Color       PdfColor
	PatternName core.PdfObjectName
}

// Colorspace returns a Pattern colorspace whose underlying colorspace, if any,
// is derived from the underlying color.
func (p *PdfColorPatternType3) Colorspace() PdfColorspace {
	specialPatternCS := NewPdfColorspaceSpecialPattern()
	if p.Color != nil {
		specialPatternCS.UnderlyingCS = p.Color.Colorspace()
	}
	return specialPatternCS
}

// NewPdfColorPatternType3 returns an empty color shading pattern type 3 (Radial).
func NewPdfColorPatternType3() *PdfColorPatternType3 {
	patternType3Color := &PdfColorPatternType3{}
	return patternType3Color
}

// Components returns the component values of the underlying color, or nil if
// the pattern has no underlying color.
func (p *PdfColorPatternType3) Components() []float64 {
	if p.Color == nil {
		return nil
	}
	return p.Color.Components()
}

// PdfColorLab represents a color in the L*, a*, b* 3 component colorspace.
// Each component is defined in the range 0.0 - 1.0 where 1.0 is the primary intensity.
type PdfColorLab [3]float64

// Colorspace returns a best-effort, parameterless Lab colorspace. The
// authoritative parameterized colorspace is tracked by the graphics state.
func (c *PdfColorLab) Colorspace() PdfColorspace { return NewPdfColorspaceLab() }

// B returns the value of the B component of the color.
func (c *PdfColorLab) B() float64 { return c[2] }

// GetNumComponents returns the number of color components (3 for Lab).
func (c *PdfColorLab) GetNumComponents() int { return 3 }

// A returns the value of the A component of the color.
func (c *PdfColorLab) A() float64 { return c[1] }

// ToInteger convert to an integer format.
func (c *PdfColorLab) ToInteger(bits int) [3]uint32 {
	f := math.Pow(2, float64(bits)) - 1
	return [3]uint32{uint32(f * c.L()), uint32(f * c.A()), uint32(f * c.B())}
}

// NewPdfColorLab returns a new Lab color.
func NewPdfColorLab(l, a, b float64) *PdfColorLab { labColor := PdfColorLab{l, a, b}; return &labColor }

// L returns the value of the L component of the color.
func (c *PdfColorLab) L() float64 { return c[0] }

// Components returns the color's component values: L, A, B.
func (c *PdfColorLab) Components() []float64 { return []float64{c[0], c[1], c[2]} }
