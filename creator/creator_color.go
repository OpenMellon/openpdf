package creator

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/model"
)

// Color interface represents colors in the PDF creator.
type Color interface {
	ToRGB() (float64, float64, float64)
}

// ColorRGBFromHex converts color hex code to rgb color for using with creator.
// NOTE: If there is a problem interpreting the string, then will use black color and log a debug message.
// Example hex code: #ffffff -> (1,1,1) white.
func ColorRGBFromHex(hexStr string) Color {
	colorRGB := rgbColor{}
	if (len(hexStr) != 4 && len(hexStr) != 7) || hexStr[0] != '#' {
		common.Log.Debug("Invalid hex code: %s", hexStr)
		return colorRGB
	}
	var red, green, blue int
	if len(hexStr) == 4 {
		var redShort, greenShort, blueShort int
		n, err := fmt.Sscanf(hexStr, "#%1x%1x%1x", &redShort, &greenShort, &blueShort)
		if err != nil {
			common.Log.Debug("Invalid hex code: %s, error: %v", hexStr, err)
			return colorRGB
		}
		if n != 3 {
			common.Log.Debug("Invalid hex code: %s", hexStr)
			return colorRGB
		}
		red = redShort*16 + redShort
		green = greenShort*16 + greenShort
		blue = blueShort*16 + blueShort
	} else {
		n, err := fmt.Sscanf(hexStr, "#%2x%2x%2x", &red, &green, &blue)
		if err != nil {
			common.Log.Debug("Invalid hex code: %s", hexStr)
			return colorRGB
		}
		if n != 3 {
			common.Log.Debug("Invalid hex code: %s, n != 3 (%d)", hexStr, n)
			return colorRGB
		}
	}
	r := float64(red) / 255.0
	g := float64(green) / 255.0
	b := float64(blue) / 255.0
	colorRGB.r = r
	colorRGB.g = g
	colorRGB.b = b
	return colorRGB
}

// ColorRGBFrom8bit creates a Color from 8-bit (0-255) r,g,b values.
// Example:
//
//	red := ColorRGBFrom8Bit(255, 0, 0)
func ColorRGBFrom8bit(r, g, b byte) Color {
	return rgbColor{r: float64(r) / 255.0, g: float64(g) / 255.0, b: float64(b) / 255.0}
}

func colorspaceForComponents(n int) (model.PdfColorspace, error) {
	switch n {
	case 1:
		return model.NewPdfColorspaceDeviceGray(), nil
	case 3:
		return model.NewPdfColorspaceDeviceRGB(), nil
	case 4:
		return model.NewPdfColorspaceDeviceCMYK(), nil
	}
	return nil, fmt.Errorf("program returns %d values, expected 1, 3 or 4 color components", n)
}

// ColorCMYKFrom8bit creates a Color from c,m,y,k values (0-100).
// Example:
//
//	red := ColorCMYKFrom8Bit(0, 100, 100, 0)
func ColorCMYKFrom8bit(c, m, y, k byte) Color {
	return cmykColor{c: math.Min(float64(c), 100) / 100.0, m: math.Min(float64(m), 100) / 100.0, y: math.Min(float64(y), 100) / 100.0, k: math.Min(float64(k), 100) / 100.0}
}

// ColorRGBFromArithmetic creates a Color from arithmetic color values (0-1).
// Example:
//
//	green := ColorRGBFromArithmetic(0.0, 1.0, 0.0)
func ColorRGBFromArithmetic(r, g, b float64) Color {
	return rgbColor{r: math.Max(math.Min(r, 1.0), 0.0), g: math.Max(math.Min(g, 1.0), 0.0), b: math.Max(math.Min(b, 1.0), 0.0)}
}

// ColorGrayFrom8bit creates a Color from 8-bit (0-255) gray values.
// Example:
//
//	gray := ColorGrayFrom8bit(255, 0, 0)
func ColorGrayFrom8bit(g byte) Color { return grayColor{float64(g) / 255.0} }

// ColorGrayFromArithmetic creates a Color from a grayscale value (0-1).
// Example:
//
//	gray := ColorGrayFromArithmetic(0.7)
func ColorGrayFromArithmetic(g float64) Color { return grayColor{g} }

func parseColorComponent(s string) (float64, error) {
	s = strings.TrimSpace(s)
	var colorComponent float64
	if before, ok := strings.CutSuffix(s, "%"); ok {
		float, err := strconv.ParseFloat(before, 64)
		if err != nil {
			return 0, err
		}
		colorComponent = (float * 255.0) / 100.0
	} else {
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, err
		}
		colorComponent = float64(n)
	}
	return colorComponent, nil
}

// ColorGrayFromHex converts color hex code to gray color for using with creator.
// NOTE: If there is a problem interpreting the string, then will use black color and log a debug message.
// Example hex code: #ff -> white.
func ColorGrayFromHex(hexStr string) Color {
	colorGray := grayColor{}
	if (len(hexStr) != 2 && len(hexStr) != 3) || hexStr[0] != '#' {
		common.Log.Debug("Invalid hex code: %s", hexStr)
		return colorGray
	}
	var gray int
	if len(hexStr) == 2 {
		var shortGray int
		n, err := fmt.Sscanf(hexStr, "#%1x", &shortGray)
		if err != nil {
			common.Log.Debug("Invalid hex code: %s, error: %v", hexStr, err)
			return colorGray
		}
		if n != 1 {
			common.Log.Debug("Invalid hex code: %s", hexStr)
			return colorGray
		}
		gray = shortGray*16 + shortGray
	} else {
		n, err := fmt.Sscanf(hexStr, "#%2x", &gray)
		if err != nil {
			common.Log.Debug("Invalid hex code: %s", hexStr)
			return colorGray
		}
		if n != 1 {
			common.Log.Debug("Invalid hex code: %s, n != 3 (%d)", hexStr, n)
			return colorGray
		}
	}
	colorGray.gray = float64(gray) / 255.0
	return colorGray
}

func colorAttr(i any) attrApplyFunc {
	value2 := validateSetter("colorAttr", i, 0, colorType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		color, err := proc.resolveColor(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
		}
		callSetter(value2, target, reflect.ValueOf(color))
		return nil
	}
}

var (
	ColorBlack  = ColorRGBFromArithmetic(0, 0, 0)
	ColorWhite  = ColorRGBFromArithmetic(1, 1, 1)
	ColorRed    = ColorRGBFromArithmetic(1, 0, 0)
	ColorGreen  = ColorRGBFromArithmetic(0, 1, 0)
	ColorBlue   = ColorRGBFromArithmetic(0, 0, 1)
	ColorYellow = ColorRGBFromArithmetic(1, 1, 0)
)

// ColorCMYKFromArithmetic creates a Color from arithmetic color values (0-1).
// Example:
//
//	green := ColorCMYKFromArithmetic(1.0, 0.0, 1.0, 0.0)
func ColorCMYKFromArithmetic(c, m, y, k float64) Color {
	return cmykColor{c: math.Max(math.Min(c, 1.0), 0.0), m: math.Max(math.Min(m, 1.0), 0.0), y: math.Max(math.Min(y, 1.0), 0.0), k: math.Max(math.Min(k, 1.0), 0.0)}
}

func validateColorKeyMask(nums []int, xObjectImage *model.XObjectImage) error {
	if xObjectImage.ColorSpace == nil {
		return errors.New("image has no colorspace")
	}
	if n := 2 * xObjectImage.ColorSpace.GetNumComponents(); len(nums) != n {
		return fmt.Errorf("color key mask needs %d values for colorspace %s, got %d", n, xObjectImage.ColorSpace.String(), len(nums))
	}
	if xObjectImage.BitsPerComponent == nil {
		return errors.New("image has no bits per component")
	}
	n := (1 << uint(*xObjectImage.BitsPerComponent)) - 1
	for _, n2 := range nums {
		if n2 > n {
			return fmt.Errorf("color key mask value %d out of range [0, %d]", n2, n)
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

func newColorPoint(color Color, point float64) *ColorPoint {
	return &ColorPoint{color: color, point: point}
}

// NewColorPoint creates a new color and point object for use in the gradient rendering process.
func NewColorPoint(color Color, point float64) *ColorPoint { return newColorPoint(color, point) }

type rgbColor struct{ r, g, b float64 }

func parseRGBColor(s string) (float64, float64, float64) {
	s2 := strings.TrimPrefix(s, "rgb(")
	s2 = strings.TrimSuffix(s2, ")")
	strs := strings.Split(s2, ",")
	if len(strs) != 3 {
		common.Log.Debug("Invalid rgb color specification: %s", s)
		return 0, 0, 0
	}
	var value, value2, value3 float64
	value, err := parseColorComponent(strs[0])
	if err != nil {
		common.Log.Debug("Invalid rgb color specification: %s", s)
		return 0, 0, 0
	}
	value2, err = parseColorComponent(strs[1])
	if err != nil {
		common.Log.Debug("Invalid rgb color specification: %s", s)
		return 0, 0, 0
	}
	value3, err = parseColorComponent(strs[2])
	if err != nil {
		common.Log.Debug("Invalid rgb color specification: %s", s)
		return 0, 0, 0
	}
	value4 := value / 255.0
	value5 := value2 / 255.0
	value6 := value3 / 255.0
	return value4, value5, value6
}

func (rc rgbColor) ToRGB() (float64, float64, float64) {
	return rc.r, rc.g, rc.b
}

type cmykColor struct{ c, m, y, k float64 }

func (cc cmykColor) ToRGB() (float64, float64, float64) {
	k := cc.k
	return 1 - (cc.c*(1-k) + k), 1 - (cc.m*(1-k) + k), 1 - (cc.y*(1-k) + k)
}

type grayColor struct{ gray float64 }

func (grc grayColor) ToRGB() (float64, float64, float64) {
	return grc.gray, grc.gray, grc.gray
}
