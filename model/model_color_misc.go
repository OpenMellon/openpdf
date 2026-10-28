package model

import (
	_ "image/gif"
	"math"

	"github.com/OpenMellon/openpdf/core"
)

// Colorspace returns a Pattern colorspace whose underlying colorspace, if any,
// is derived from the underlying color.
func (p *PdfColorPattern) Colorspace() PdfColorspace {
	specialPatternCS := NewPdfColorspaceSpecialPattern()
	if p.Color != nil {
		specialPatternCS.UnderlyingCS = p.Color.Colorspace()
	}
	return specialPatternCS
}

// NewPdfColorDeviceCMYK returns a new CMYK32 color.
func NewPdfColorDeviceCMYK(c, m, y, k float64) *PdfColorDeviceCMYK {
	deviceCMYKColor := PdfColorDeviceCMYK{c, m, y, k}
	return &deviceCMYKColor
}

// PdfColorPattern represents a pattern color.
type PdfColorPattern struct {
	Color       PdfColor
	PatternName core.PdfObjectName
}

// B returns the value of the B component of the color.
func (c *PdfColorCalRGB) B() float64 { return c[1] }

// GetNumComponents returns the number of color components (4 for CMYK32).
func (c *PdfColorDeviceCMYK) GetNumComponents() int { return 4 }

// Colorspace returns a best-effort, parameterless CalGray colorspace. The
// authoritative parameterized colorspace is tracked by the graphics state.
func (c *PdfColorCalGray) Colorspace() PdfColorspace { return NewPdfColorspaceCalGray() }

// NewPdfColorDeviceRGB returns a new PdfColorDeviceRGB based on the r,g,b component values.
func NewPdfColorDeviceRGB(r, g, b float64) *PdfColorDeviceRGB {
	deviceRGBColor := PdfColorDeviceRGB{r, g, b}
	return &deviceRGBColor
}

// C returns the value of the C component of the color.
func (c *PdfColorCalRGB) C() float64 { return c[2] }

// PdfColorCalGray represents a CalGray colorspace.
type PdfColorCalGray float64

// Components returns the component values of the underlying color, or nil if
// the pattern has no underlying color (e.g. an uncolored tiling pattern).
func (p *PdfColorPattern) Components() []float64 {
	if p.Color == nil {
		return nil
	}
	return p.Color.Components()
}

// NewPdfColorCalGray returns a new CalGray color.
func NewPdfColorCalGray(grayVal float64) *PdfColorCalGray {
	calGrayColor := PdfColorCalGray(grayVal)
	return &calGrayColor
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

// PdfColorDeviceCMYK is a CMYK32 color, where each component is defined in the range 0.0 - 1.0 where 1.0 is the primary intensity.
type PdfColorDeviceCMYK [4]float64

// ToInteger convert to an integer format.
func (c *PdfColorCalRGB) ToInteger(bits int) [3]uint32 {
	f := math.Pow(2, float64(bits)) - 1
	return [3]uint32{uint32(f * c.A()), uint32(f * c.B()), uint32(f * c.C())}
}

// Components returns the color's component values: the single gray value.
func (c *PdfColorDeviceGray) Components() []float64 { return []float64{float64(*c)} }

// Colorspace returns a Pattern colorspace whose underlying colorspace, if any,
// is derived from the underlying color.
func (p *PdfColorPatternType2) Colorspace() PdfColorspace {
	specialPatternCS := NewPdfColorspaceSpecialPattern()
	if p.Color != nil {
		specialPatternCS.UnderlyingCS = p.Color.Colorspace()
	}
	return specialPatternCS
}

// NewPdfColorDeviceGray returns a new grayscale color based on an input grayscale float value in range [0-1].
func NewPdfColorDeviceGray(grayVal float64) *PdfColorDeviceGray {
	deviceGrayColor := PdfColorDeviceGray(grayVal)
	return &deviceGrayColor
}

// Colorspace returns a best-effort, parameterless CalRGB colorspace. The
// authoritative parameterized colorspace is tracked by the graphics state.
func (c *PdfColorCalRGB) Colorspace() PdfColorspace { return NewPdfColorspaceCalRGB() }

// Components returns the color's component values: C, M, Y, K.
func (c *PdfColorDeviceCMYK) Components() []float64 {
	return []float64{c[0], c[1], c[2], c[3]}
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

// GetNumComponents returns the number of color components (1 for CalGray).
func (c *PdfColorCalGray) GetNumComponents() int { return 1 }

// Colorspace returns a best-effort, parameterless Lab colorspace. The
// authoritative parameterized colorspace is tracked by the graphics state.
func (c *PdfColorLab) Colorspace() PdfColorspace { return NewPdfColorspaceLab() }

// Components returns the color's component values: A, B, C.
func (c *PdfColorCalRGB) Components() []float64 { return []float64{c[0], c[1], c[2]} }

// ToInteger convert to an integer format.
func (c *PdfColorDeviceGray) ToInteger(bits int) uint32 {
	f := math.Pow(2, float64(bits)) - 1
	return uint32(f * c.Val())
}

// B returns the value of the B component of the color.
func (c *PdfColorLab) B() float64 { return c[2] }

// G returns the value of the green component of the color.
func (c *PdfColorDeviceRGB) G() float64 { return c[1] }

func colorFromArray(arr *core.PdfObjectArray) PdfColor {
	if arr == nil {
		return nil
	}
	floats := make([]float64, 0, arr.Len())
	for _, obj := range arr.Elements() {
		f, err := core.GetNumberAsFloat(obj)
		if err != nil {
			return nil
		}
		floats = append(floats, f)
	}
	switch len(floats) {
	case 1:
		col, err := NewPdfColorspaceDeviceGray().ColorFromFloats(floats)
		if err != nil {
			return nil
		}
		return col
	case 3:
		col, err := NewPdfColorspaceDeviceRGB().ColorFromFloats(floats)
		if err != nil {
			return nil
		}
		return col
	case 4:
		col, err := NewPdfColorspaceDeviceCMYK().ColorFromFloats(floats)
		if err != nil {
			return nil
		}
		return col
	}
	return nil
}

// NewPdfColorPattern returns an empty color pattern.
func NewPdfColorPattern() *PdfColorPattern { patternColor := &PdfColorPattern{}; return patternColor }

// NewPdfColorCalRGB returns a new CalRBG color.
func NewPdfColorCalRGB(a, b, c float64) *PdfColorCalRGB {
	calRGBColor := PdfColorCalRGB{a, b, c}
	return &calRGBColor
}

// C returns the value of the cyan component of the color.
func (c *PdfColorDeviceCMYK) C() float64 { return c[0] }

// PdfColorDeviceGray represents a grayscale color value that shall be represented by a single number in the
// range 0.0 to 1.0 where 0.0 corresponds to black and 1.0 to white.
type PdfColorDeviceGray float64

// Components returns the color's component values: the single gray value.
func (c *PdfColorCalGray) Components() []float64 { return []float64{float64(*c)} }

// NewPdfColorPatternType3 returns an empty color shading pattern type 3 (Radial).
func NewPdfColorPatternType3() *PdfColorPatternType3 {
	patternType3Color := &PdfColorPatternType3{}
	return patternType3Color
}

// Colorspace returns the DeviceRGB colorspace.
func (c *PdfColorDeviceRGB) Colorspace() PdfColorspace { return NewPdfColorspaceDeviceRGB() }

// PdfColorPatternType2 represents a color shading pattern type 2 (Axial).
type PdfColorPatternType2 struct {
	Color       PdfColor
	PatternName core.PdfObjectName
}

// A returns the value of the A component of the color.
func (c *PdfColorCalRGB) A() float64 { return c[0] }

// ToInteger convert to an integer format.
func (c *PdfColorCalGray) ToInteger(bits int) uint32 {
	f := math.Pow(2, float64(bits)) - 1
	return uint32(f * c.Val())
}

// GetNumComponents returns the number of color components (3 for CalRGB).
func (c *PdfColorCalRGB) GetNumComponents() int { return 3 }

// ToInteger convert to an integer format.
func (c *PdfColorDeviceCMYK) ToInteger(bits int) [4]uint32 {
	f := math.Pow(2, float64(bits)) - 1
	return [4]uint32{uint32(f * c.C()), uint32(f * c.M()), uint32(f * c.Y()), uint32(f * c.K())}
}

// PdfColor interface represents a generic color in PDF.
type PdfColor interface {
	// Components returns the color's component values in its own colorspace.
	Components() []float64

	// Colorspace returns the colorspace family this color belongs to.
	// For Cal/Lab colors this is a best-effort, parameterless instance; the
	// authoritative parameterized colorspace is tracked by the graphics state.
	Colorspace() PdfColorspace
}

// GetNumComponents returns the number of color components (3 for Lab).
func (c *PdfColorLab) GetNumComponents() int { return 3 }

// Val returns the value of the color.
func (c *PdfColorCalGray) Val() float64 { return float64(*c) }

// Val returns the color value.
func (c *PdfColorDeviceGray) Val() float64 { return float64(*c) }

// ToInteger convert to an integer format.
func (c *PdfColorDeviceRGB) ToInteger(bits int) [3]uint32 {
	f := math.Pow(2, float64(bits)) - 1
	return [3]uint32{uint32(f * c.R()), uint32(f * c.G()), uint32(f * c.B())}
}

// NewPdfColorPatternType2 returns an empty color shading pattern type 2 (Axial).
func NewPdfColorPatternType2() *PdfColorPatternType2 {
	patternType2Color := &PdfColorPatternType2{}
	return patternType2Color
}

// Components returns the color's component values: R, G, B.
func (c *PdfColorDeviceRGB) Components() []float64 {
	return []float64{c[0], c[1], c[2]}
}

// A returns the value of the A component of the color.
func (c *PdfColorLab) A() float64 { return c[1] }

// ToInteger convert to an integer format.
func (c *PdfColorLab) ToInteger(bits int) [3]uint32 {
	f := math.Pow(2, float64(bits)) - 1
	return [3]uint32{uint32(f * c.L()), uint32(f * c.A()), uint32(f * c.B())}
}

// NewPdfColorLab returns a new Lab color.
func NewPdfColorLab(l, a, b float64) *PdfColorLab { labColor := PdfColorLab{l, a, b}; return &labColor }

// GetNumComponents returns the number of color components (1 for grayscale).
func (c *PdfColorDeviceGray) GetNumComponents() int { return 1 }

// Colorspace returns the DeviceCMYK colorspace.
func (c *PdfColorDeviceCMYK) Colorspace() PdfColorspace { return NewPdfColorspaceDeviceCMYK() }

// ToGray returns a PdfColorDeviceGray color based on the current RGB color.
func (c *PdfColorDeviceRGB) ToGray() *PdfColorDeviceGray {
	f := 0.3*c.R() + 0.59*c.G() + 0.11*c.B()
	f = math.Min(math.Max(f, 0.0), 1.0)
	return NewPdfColorDeviceGray(f)
}

// NewPdfColorPatternType1 returns an empty color shading pattern type 1 (Function-based).
func NewPdfColorPatternType1() *PdfColorPatternType1 {
	patternType1Color := &PdfColorPatternType1{}
	return patternType1Color
}

// PdfColorDeviceRGB represents a color in DeviceRGB colorspace with R, G, B components, where component is
// defined in the range 0.0 - 1.0 where 1.0 is the primary intensity.
type PdfColorDeviceRGB [3]float64

// PdfColorPatternType3 represents a color shading pattern type 3 (Radial).
type PdfColorPatternType3 struct {
	Color       PdfColor
	PatternName core.PdfObjectName
}

// B returns the value of the blue component of the color.
func (c *PdfColorDeviceRGB) B() float64 { return c[2] }

// Colorspace returns the DeviceGray colorspace.
func (c *PdfColorDeviceGray) Colorspace() PdfColorspace { return NewPdfColorspaceDeviceGray() }

// PdfColorPatternType1 represents a color shading pattern type 1 (Function-based).
type PdfColorPatternType1 struct {
	Color       PdfColor
	PatternName core.PdfObjectName
}

// M returns the value of the magenta component of the color.
func (c *PdfColorDeviceCMYK) M() float64 { return c[1] }

// K returns the value of the key component of the color.
func (c *PdfColorDeviceCMYK) K() float64 { return c[3] }

// Y returns the value of the yellow component of the color.
func (c *PdfColorDeviceCMYK) Y() float64 { return c[2] }

// L returns the value of the L component of the color.
func (c *PdfColorLab) L() float64 { return c[0] }

// Components returns the component values of the underlying color, or nil if
// the pattern has no underlying color.
func (p *PdfColorPatternType2) Components() []float64 {
	if p.Color == nil {
		return nil
	}
	return p.Color.Components()
}

// PdfColorCalRGB represents a color in the Colorimetric CIE RGB colorspace.
// A, B, C components
// Each component is defined in the range 0.0 - 1.0 where 1.0 is the primary intensity.
type PdfColorCalRGB [3]float64

// Components returns the component values of the underlying color, or nil if
// the pattern has no underlying color.
func (p *PdfColorPatternType1) Components() []float64 {
	if p.Color == nil {
		return nil
	}
	return p.Color.Components()
}

// GetNumComponents returns the number of color components (3 for RGB).
func (c *PdfColorDeviceRGB) GetNumComponents() int { return 3 }

// PdfColorLab represents a color in the L*, a*, b* 3 component colorspace.
// Each component is defined in the range 0.0 - 1.0 where 1.0 is the primary intensity.
type PdfColorLab [3]float64

// Components returns the color's component values: L, A, B.
func (c *PdfColorLab) Components() []float64 { return []float64{c[0], c[1], c[2]} }

// R returns the value of the red component of the color.
func (c *PdfColorDeviceRGB) R() float64 { return c[0] }

// Components returns the component values of the underlying color, or nil if
// the pattern has no underlying color.
func (p *PdfColorPatternType3) Components() []float64 {
	if p.Color == nil {
		return nil
	}
	return p.Color.Components()
}
