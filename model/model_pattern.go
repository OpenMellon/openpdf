package model

import (
	"errors"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// A PdfPattern can represent a Pattern, either a tiling pattern or a shading pattern.
// Note that all patterns shall be treated as colours; a Pattern colour space shall be established with the CS or cs
// operator just like other colour spaces, and a particular pattern shall be installed as the current colour with the
// SCN or scn operator.
type PdfPattern struct {
	// Type: Pattern
	PatternType int64
	context     PdfModel
	primitive   core.PdfObject
}

// GetContainingPdfObject returns the container of the pattern object (indirect object).
func (p *PdfPattern) GetContainingPdfObject() core.PdfObject { return p.primitive }

func (p *PdfPattern) getDict() *core.PdfObjectDictionary {
	if indirect, ok := p.primitive.(*core.PdfIndirectObject); ok {
		dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
		if !ok {
			return nil
		}
		return dict
	} else if stream, ok := p.primitive.(*core.PdfObjectStream); ok {
		return stream.PdfObjectDictionary
	} else {
		common.Log.Debug("Trying to access pattern dictionary of invalid object type (%T)", p.primitive)
		return nil
	}
}

// IsShading specifies if the pattern is a shading pattern.
func (p *PdfPattern) IsShading() bool { return p.PatternType == 2 }

// IsTiling specifies if the pattern is a tiling pattern.
func (p *PdfPattern) IsTiling() bool { return p.PatternType == 1 }

func newPdfPatternFromPdfObject(obj core.PdfObject) (*PdfPattern, error) {
	pattern := &PdfPattern{}
	var dict *core.PdfObjectDictionary
	if indirect, ok := core.GetIndirect(obj); ok {
		pattern.primitive = indirect
		dict2, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
		if !ok {
			common.Log.Debug("Pattern indirect object not containing dictionary (got %T)", indirect.PdfObject)
			return nil, core.ErrTypeError
		}
		dict = dict2
	} else if stream, ok := core.GetStream(obj); ok {
		pattern.primitive = stream
		dict = stream.PdfObjectDictionary
	} else {
		common.Log.Debug("Pattern not an indirect object or stream. %T", obj)
		return nil, core.ErrTypeError
	}
	patternType := dict.Get("PatternType")
	if patternType == nil {
		common.Log.Debug("Pdf Pattern not containing PatternType")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok := patternType.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("Pattern type not an integer (got %T)", patternType)
		return nil, core.ErrTypeError
	}
	if *num != 1 && *num != 2 {
		common.Log.Debug("Pattern type != 1/2 (got %d)", *num)
		return nil, core.ErrRangeError
	}
	pattern.PatternType = int64(*num)
	switch *num {
	case 1:
		tilingPattern, err := newPdfTilingPatternFromDict(dict)
		if err != nil {
			return nil, err
		}
		tilingPattern.PdfPattern = pattern
		pattern.context = tilingPattern
		return pattern, nil
	case 2:
		shadingPattern, err := newPdfShadingPatternFromDict(dict)
		if err != nil {
			return nil, err
		}
		shadingPattern.PdfPattern = pattern
		pattern.context = shadingPattern
		return pattern, nil
	}
	return nil, errors.New("unknown pattern")
}

// GetAsShadingPattern returns a shading pattern. Check with IsShading() prior to using this.
func (p *PdfPattern) GetAsShadingPattern() *PdfShadingPattern {
	return p.context.(*PdfShadingPattern)
}

// GetContext returns a reference to the subpattern entry: either PdfTilingPattern or PdfShadingPattern.
func (p *PdfPattern) GetContext() PdfModel { return p.context }

// GetAsTilingPattern returns a tiling pattern. Check with IsTiling() prior to using this.
func (p *PdfPattern) GetAsTilingPattern() *PdfTilingPattern {
	return p.context.(*PdfTilingPattern)
}

// ToPdfObject returns the PDF representation of the pattern.
func (p *PdfPattern) ToPdfObject() core.PdfObject {
	dict := p.getDict()
	dict.Set("Type", core.MakeName("Pattern"))
	dict.Set("PatternType", core.MakeInteger(p.PatternType))
	return p.primitive
}

// SetContext sets the sub pattern (context).  Either PdfTilingPattern or PdfShadingPattern.
func (p *PdfPattern) SetContext(ctx PdfModel) { p.context = ctx }

// PdfShadingPattern is a Shading patterns that provide a smooth transition between colors across an area to be painted,
// i.e. color(x,y) = f(x,y) at each point.
// It is a type 2 pattern (PatternType = 2).
type PdfShadingPattern struct {
	*PdfPattern
	Shading   *PdfShading
	Matrix    *core.PdfObjectArray
	ExtGState core.PdfObject
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

// PdfShadingPatternType3 is shading patterns that will use a Type 3 shading pattern (Radial).
type PdfShadingPatternType3 struct {
	*PdfPattern
	Shading   *PdfShadingType3
	Matrix    *core.PdfObjectArray
	ExtGState core.PdfObject
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
