package creator

import (
	"strconv"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

type shading struct {
	backgroundColor Color
	antiAlias       bool
	extends         []bool
	colorPoints     []*ColorPoint
}

func addShadingResource(block *Block, shadingObj core.PdfObject) (core.PdfObjectName, error) {
	i := 1
	keyName := core.PdfObjectName("Sh" + strconv.Itoa(i))
	for block.resources.HasShadingByName(keyName) {
		i++
		keyName = core.PdfObjectName("Sh" + strconv.Itoa(i))
	}
	if err := block.resources.SetShadingByName(keyName, shadingObj); err != nil {
		return "", err
	}
	return keyName, nil
}

// SetExtends specifies whether to extend the shading beyond the starting and ending points.
//
// Text extends is set to `[]bool{false, false}` by default.
func (s *shading) SetExtends(start bool, end bool) { s.extends = []bool{start, end} }

func addShadingPatternResource(block *Block, pattern core.PdfObject) (core.PdfObjectName, error) {
	i := 1
	keyName := core.PdfObjectName("P" + strconv.Itoa(i))
	for block.resources.HasPatternByName(keyName) {
		i++
		keyName = core.PdfObjectName("P" + strconv.Itoa(i))
	}
	if err := block.resources.SetPatternByName(keyName, pattern); err != nil {
		return "", err
	}
	return keyName, nil
}

// AddColorStop add color stop information for rendering gradient.
func (s *shading) AddColorStop(color Color, point float64) {
	s.colorPoints = append(s.colorPoints, newColorPoint(color, point))
}

// SetAntiAlias enables anti alias config.
//
// Anti alias is disabled by default.
func (s *shading) SetAntiAlias(enable bool) { s.antiAlias = enable }

func (s *shading) generatePdfFunctions() []model.PdfFunction {
	if len(s.colorPoints) == 0 {
		return nil
	} else if len(s.colorPoints) <= 2 {
		r, g, b := s.colorPoints[0].color.ToRGB()
		r2, g2, b2 := s.colorPoints[len(s.colorPoints)-1].color.ToRGB()
		return []model.PdfFunction{&model.PdfFunctionType2{Domain: []float64{0.0, 1.0}, Range: []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}, N: 1, C0: []float64{r, g, b}, C1: []float64{r2, g2, b2}}}
	} else {
		functions := []model.PdfFunction{}
		floats := []float64{}
		for i := 0; i < len(s.colorPoints)-1; i++ {
			r, g, b := s.colorPoints[i].color.ToRGB()
			r2, g2, b2 := s.colorPoints[i+1].color.ToRGB()
			functionType2 := &model.PdfFunctionType2{Domain: []float64{0.0, 1.0}, Range: []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}, N: 1, C0: []float64{r, g, b}, C1: []float64{r2, g2, b2}}
			functions = append(functions, functionType2)
			if i > 0 {
				floats = append(floats, s.colorPoints[i].point)
			}
		}
		encode := []float64{}
		for range functions {
			encode = append(encode, []float64{0.0, 1.0}...)
		}
		return []model.PdfFunction{&model.PdfFunctionType3{Domain: []float64{0.0, 1.0}, Range: []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}, Functions: functions, Bounds: floats, Encode: encode}}
	}
}

// SetBackgroundColor set background color of the shading area.
//
// By default the background color is set to white.
func (s *shading) SetBackgroundColor(backgroundColor Color) { s.backgroundColor = backgroundColor }
