package creator

import (
	"errors"
	"fmt"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/ps"
)

// FunctionBasedShading holds data for rendering a function-based shading
// gradient (shading type 1). The color at a point comes from a PostScript
// calculator program rather than from color stops along an axis, so it can
// vary in two dimensions.
type FunctionBasedShading struct {
	base        *shading
	psCode      string
	boundingBox *model.PdfRectangle
}

// ToPdfShadingPattern generates a new model.PdfShadingPatternType1 object. It
// returns an error when the PostScript program cannot be parsed or does not
// produce a usable number of color components, rather than a pattern with no
// shading in it.
func (fbs *FunctionBasedShading) ToPdfShadingPattern() (*model.PdfShadingPatternType1, error) {
	shadingModel, err := fbs.shadingModel()
	if err != nil {
		return nil, err
	}
	pdfShadingPatternType1 := model.NewPdfShadingPatternType1()
	pdfShadingPatternType1.Shading = shadingModel
	return pdfShadingPatternType1, nil
}

func newFunctionBasedShading(psCode string) *FunctionBasedShading {
	return &FunctionBasedShading{base: &shading{antiAlias: false}, psCode: psCode, boundingBox: &model.PdfRectangle{}}
}

func (fbs *FunctionBasedShading) pdfFunction() (model.PdfFunction, model.PdfColorspace, error) {
	if fbs.psCode == "" {
		return nil, nil, errors.New("no PostScript program specified")
	}
	program, err := ps.NewPSParser([]byte(fbs.psCode)).Parse()
	if err != nil {
		return nil, nil, fmt.Errorf("failed parsing PostScript program: %v", err)
	}
	functionType4 := &model.PdfFunctionType4{Domain: functionDomain, Program: program}
	value := (functionDomain[0] + functionDomain[1]) / 2
	value2 := (functionDomain[2] + functionDomain[3]) / 2
	floats2, err := functionType4.Evaluate([]float64{value, value2})
	if err != nil {
		return nil, nil, fmt.Errorf("failed evaluating PostScript program: %v", err)
	}
	colorspace, err := colorspaceForComponents(len(floats2))
	if err != nil {
		return nil, nil, err
	}
	floats := make([]float64, 0, 2*len(floats2))
	for range floats2 {
		floats = append(floats, 0, 1)
	}
	return &model.PdfFunctionType4{Domain: functionDomain, Range: floats, Program: program}, colorspace, nil
}

func (fbs *FunctionBasedShading) shadingModel() (*model.PdfShadingType1, error) {
	pdfFunction, colorSpace, err := fbs.pdfFunction()
	if err != nil {
		return nil, err
	}
	pdfShadingType1 := model.NewPdfShadingType1()
	pdfShadingType1.ShadingType = core.MakeInteger(1)
	pdfShadingType1.ColorSpace = colorSpace
	pdfShadingType1.AntiAlias = core.MakeBool(fbs.base.antiAlias)
	pdfShadingType1.Domain = core.MakeArrayFromFloats(functionDomain)
	pdfShadingType1.Matrix = core.MakeArrayFromFloats([]float64{fbs.boundingBox.Width(), 0, 0, fbs.boundingBox.Height(), fbs.boundingBox.Llx, fbs.boundingBox.Lly})
	pdfShadingType1.Function = []model.PdfFunction{pdfFunction}
	return pdfShadingType1, nil
}

// ToRGB implements interface Color.
// Note: It's not directly used since shading color works differently than regular color.
func (fbs *FunctionBasedShading) ToRGB() (float64, float64, float64) { return 0, 0, 0 }

// AddPatternResource adds pattern dictionary inside the resources dictionary.
func (fbs *FunctionBasedShading) AddPatternResource(block *Block) (name core.PdfObjectName, err error) {
	pdfShadingPattern, err := fbs.ToPdfShadingPattern()
	if err != nil {
		return "", err
	}
	return addShadingPatternResource(block, pdfShadingPattern.ToPdfObject())
}

// SetBoundingBox set gradient color bounding box where the gradient would be rendered.
func (fbs *FunctionBasedShading) SetBoundingBox(x, y, width, height float64) {
	fbs.boundingBox = &model.PdfRectangle{Llx: x, Lly: y, Urx: x + width, Ury: y + height}
}

// AddShadingResource adds shading dictionary inside the resources dictionary.
func (fbs *FunctionBasedShading) AddShadingResource(block *Block) (name core.PdfObjectName, err error) {
	shadingModel, err := fbs.shadingModel()
	if err != nil {
		return "", err
	}
	return addShadingResource(block, shadingModel.ToPdfObject())
}

// SetAntiAlias enables anti alias config.
//
// Anti alias is disabled by default.
func (fbs *FunctionBasedShading) SetAntiAlias(enable bool) { fbs.base.SetAntiAlias(enable) }

var functionDomain = []float64{0, 1, 0, 1}
