package model

import (
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// PdfShadingPatternType2 is shading patterns that will use a Type 2 shading pattern (Axial).
type PdfShadingPatternType2 struct {
	*PdfPattern
	Shading   *PdfShadingType2
	Matrix    *core.PdfObjectArray
	ExtGState core.PdfObject
}

// ToPdfObject returns the PDF representation of the shading pattern.
func (sp *PdfShadingPatternType1) ToPdfObject() core.PdfObject {
	sp.PdfPattern.ToPdfObject()
	dict := sp.getDict()
	if sp.Shading != nil {
		dict.Set("Shading", sp.Shading.ToPdfObject())
	}
	if sp.Matrix != nil {
		dict.Set("Matrix", sp.Matrix)
	}
	if sp.ExtGState != nil {
		dict.Set("ExtGState", sp.ExtGState)
	}
	return sp.primitive
}

func newPdfShadingPatternFromDict(dict *core.PdfObjectDictionary) (*PdfShadingPattern, error) {
	shadingPattern := &PdfShadingPattern{}
	shading := dict.Get("Shading")
	if shading == nil {
		common.Log.Debug("Shading missing")
		return nil, ErrRequiredAttributeMissing
	}
	shading2, err := newPdfShadingFromPdfObject(shading)
	if err != nil {
		common.Log.Debug("Error loading shading: %v", err)
		return nil, err
	}
	shadingPattern.Shading = shading2
	if matrix := dict.Get("Matrix"); matrix != nil {
		arr, ok := core.TraceToDirectObject(matrix).(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Matrix not an array (got %T)", matrix)
			return nil, core.ErrTypeError
		}
		shadingPattern.Matrix = arr
	}
	if extGState := dict.Get("ExtGState"); extGState != nil {
		shadingPattern.ExtGState = extGState
	}
	return shadingPattern, nil
}

// PdfShadingPatternType3 is shading patterns that will use a Type 3 shading pattern (Radial).
type PdfShadingPatternType3 struct {
	*PdfPattern
	Shading   *PdfShadingType3
	Matrix    *core.PdfObjectArray
	ExtGState core.PdfObject
}

// ToPdfObject returns the PDF representation of the shading pattern.
func (sp *PdfShadingPattern) ToPdfObject() core.PdfObject {
	sp.PdfPattern.ToPdfObject()
	dict := sp.getDict()
	if sp.Shading != nil {
		dict.Set("Shading", sp.Shading.ToPdfObject())
	}
	if sp.Matrix != nil {
		dict.Set("Matrix", sp.Matrix)
	}
	if sp.ExtGState != nil {
		dict.Set("ExtGState", sp.ExtGState)
	}
	return sp.primitive
}

// ToPdfObject returns the PDF representation of the shading pattern.
func (sp *PdfShadingPatternType2) ToPdfObject() core.PdfObject {
	sp.PdfPattern.ToPdfObject()
	dict := sp.getDict()
	if sp.Shading != nil {
		dict.Set("Shading", sp.Shading.ToPdfObject())
	}
	if sp.Matrix != nil {
		dict.Set("Matrix", sp.Matrix)
	}
	if sp.ExtGState != nil {
		dict.Set("ExtGState", sp.ExtGState)
	}
	return sp.primitive
}

// NewPdfShadingPatternType1 creates an empty shading pattern type 1 object.
func NewPdfShadingPatternType1() *PdfShadingPatternType1 {
	shadingPatternType1 := &PdfShadingPatternType1{}
	shadingPatternType1.Matrix = core.MakeArrayFromIntegers([]int{1, 0, 0, 1, 0, 0})
	shadingPatternType1.PdfPattern = &PdfPattern{}
	shadingPatternType1.PdfPattern.PatternType = int64(*core.MakeInteger(2))
	shadingPatternType1.PdfPattern.context = shadingPatternType1
	shadingPatternType1.PdfPattern.primitive = core.MakeIndirectObject(core.MakeDict())
	return shadingPatternType1
}

// NewPdfShadingPatternType2 creates an empty shading pattern type 2 object.
func NewPdfShadingPatternType2() *PdfShadingPatternType2 {
	shadingPatternType2 := &PdfShadingPatternType2{}
	shadingPatternType2.Matrix = core.MakeArrayFromIntegers([]int{1, 0, 0, 1, 0, 0})
	shadingPatternType2.PdfPattern = &PdfPattern{}
	shadingPatternType2.PdfPattern.PatternType = int64(*core.MakeInteger(2))
	shadingPatternType2.PdfPattern.context = shadingPatternType2
	shadingPatternType2.PdfPattern.primitive = core.MakeIndirectObject(core.MakeDict())
	return shadingPatternType2
}

// ToPdfObject returns the PDF representation of the shading pattern.
func (sp *PdfShadingPatternType3) ToPdfObject() core.PdfObject {
	sp.PdfPattern.ToPdfObject()
	dict := sp.getDict()
	if sp.Shading != nil {
		dict.Set("Shading", sp.Shading.ToPdfObject())
	}
	if sp.Matrix != nil {
		dict.Set("Matrix", sp.Matrix)
	}
	if sp.ExtGState != nil {
		dict.Set("ExtGState", sp.ExtGState)
	}
	return sp.primitive
}

// PdfShadingPattern is a Shading patterns that provide a smooth transition between colors across an area to be painted,
// i.e. color(x,y) = f(x,y) at each point.
// It is a type 2 pattern (PatternType = 2).
type PdfShadingPattern struct {
	*PdfPattern
	Shading   *PdfShading
	Matrix    *core.PdfObjectArray
	ExtGState core.PdfObject
}

// NewPdfShadingPatternType3 creates an empty shading pattern type 3 object.
func NewPdfShadingPatternType3() *PdfShadingPatternType3 {
	shadingPatternType3 := &PdfShadingPatternType3{}
	shadingPatternType3.Matrix = core.MakeArrayFromIntegers([]int{1, 0, 0, 1, 0, 0})
	shadingPatternType3.PdfPattern = &PdfPattern{}
	shadingPatternType3.PdfPattern.PatternType = int64(*core.MakeInteger(2))
	shadingPatternType3.PdfPattern.context = shadingPatternType3
	shadingPatternType3.PdfPattern.primitive = core.MakeIndirectObject(core.MakeDict())
	return shadingPatternType3
}

// PdfShadingPatternType1 is shading patterns that will use a Type 1 shading pattern (Function-based).
type PdfShadingPatternType1 struct {
	*PdfPattern
	Shading   *PdfShadingType1
	Matrix    *core.PdfObjectArray
	ExtGState core.PdfObject
}
