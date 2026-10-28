package model

import (
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// PdfShadingType3 is a Radial shading.
type PdfShadingType3 struct {
	*PdfShading
	Coords   *core.PdfObjectArray
	Domain   *core.PdfObjectArray
	Function []PdfFunction
	Extend   *core.PdfObjectArray
}

// NewPdfShadingType3 creates an empty shading type 3 dictionary.
func NewPdfShadingType3() *PdfShadingType3 {
	shading3 := &PdfShadingType3{}
	shading3.PdfShading = &PdfShading{}
	shading3.PdfShading.primitive = core.MakeIndirectObject(core.MakeDict())
	shading3.PdfShading.context = shading3
	return shading3
}

func newPdfShadingType3FromDict(dict *core.PdfObjectDictionary) (*PdfShadingType3, error) {
	shading3 := PdfShadingType3{}
	coords := dict.Get("Coords")
	if coords == nil {
		common.Log.Debug("Required attribute missing: Coords")
		return nil, ErrRequiredAttributeMissing
	}
	arr, ok := coords.(*core.PdfObjectArray)
	if !ok {
		common.Log.Debug("Coords not an array (got %T)", coords)
		return nil, core.ErrTypeError
	}
	if arr.Len() != 6 {
		common.Log.Debug("Coords length not 6 (got %d)", arr.Len())
		return nil, ErrInvalidAttribute
	}
	shading3.Coords = arr
	if domain := dict.Get("Domain"); domain != nil {
		domain = core.TraceToDirectObject(domain)
		arr, ok := domain.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Domain not an array (got %T)", domain)
			return nil, core.ErrTypeError
		}
		shading3.Domain = arr
	}
	coords = dict.Get("Function")
	if coords == nil {
		common.Log.Debug("Required attribute missing:  Function")
		return nil, ErrRequiredAttributeMissing
	}
	shading3.Function = []PdfFunction{}
	if arr, ok := coords.(*core.PdfObjectArray); ok {
		for _, obj := range arr.Elements() {
			function, err := newPdfFunctionFromPdfObject(obj)
			if err != nil {
				common.Log.Debug("Error parsing function: %v", err)
				return nil, err
			}
			shading3.Function = append(shading3.Function, function)
		}
	} else {
		function, err := newPdfFunctionFromPdfObject(coords)
		if err != nil {
			common.Log.Debug("Error parsing function: %v", err)
			return nil, err
		}
		shading3.Function = append(shading3.Function, function)
	}
	if extend := dict.Get("Extend"); extend != nil {
		extend = core.TraceToDirectObject(extend)
		arr, ok := extend.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Matrix not an array (got %T)", extend)
			return nil, core.ErrTypeError
		}
		if arr.Len() != 2 {
			common.Log.Debug("Extend length not 2 (got %d)", arr.Len())
			return nil, ErrInvalidAttribute
		}
		shading3.Extend = arr
	}
	return &shading3, nil
}

// ToPdfObject returns the PDF representation of the shading dictionary.
func (s *PdfShadingType3) ToPdfObject() core.PdfObject {
	s.PdfShading.ToPdfObject()
	shadingDict, err := s.getShadingDict()
	if err != nil {
		common.Log.Error("Unable to access shading dict")
		return nil
	}
	if s.Coords != nil {
		shadingDict.Set("Coords", s.Coords)
	}
	if s.Domain != nil {
		shadingDict.Set("Domain", s.Domain)
	}
	if s.Function != nil {
		if len(s.Function) == 1 {
			shadingDict.Set("Function", s.Function[0].ToPdfObject())
		} else {
			array := core.MakeArray()
			for _, function := range s.Function {
				array.Append(function.ToPdfObject())
			}
			shadingDict.Set("Function", array)
		}
	}
	if s.Extend != nil {
		shadingDict.Set("Extend", s.Extend)
	}
	return s.primitive
}

// PdfShadingType5 is a Lattice-form Gouraud-shaded triangle mesh.
type PdfShadingType5 struct {
	*PdfShading
	BitsPerCoordinate *core.PdfObjectInteger
	BitsPerComponent  *core.PdfObjectInteger
	VerticesPerRow    *core.PdfObjectInteger
	Decode            *core.PdfObjectArray
	Function          []PdfFunction
}

func newPdfShadingType5FromDict(dict *core.PdfObjectDictionary) (*PdfShadingType5, error) {
	shading5 := PdfShadingType5{}
	bitsPerCoordinate := dict.Get("BitsPerCoordinate")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerCoordinate")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok := bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerCoordinate not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading5.BitsPerCoordinate = num
	bitsPerCoordinate = dict.Get("BitsPerComponent")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerComponent")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok = bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerComponent not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading5.BitsPerComponent = num
	bitsPerCoordinate = dict.Get("VerticesPerRow")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: VerticesPerRow")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok = bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("VerticesPerRow not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading5.VerticesPerRow = num
	bitsPerCoordinate = dict.Get("Decode")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: Decode")
		return nil, ErrRequiredAttributeMissing
	}
	arr, ok := bitsPerCoordinate.(*core.PdfObjectArray)
	if !ok {
		common.Log.Debug("Decode not an array (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading5.Decode = arr
	if function := dict.Get("Function"); function != nil {
		shading5.Function = []PdfFunction{}
		if arr, ok := function.(*core.PdfObjectArray); ok {
			for _, obj := range arr.Elements() {
				function, err := newPdfFunctionFromPdfObject(obj)
				if err != nil {
					common.Log.Debug("Error parsing function: %v", err)
					return nil, err
				}
				shading5.Function = append(shading5.Function, function)
			}
		} else {
			fn, err := newPdfFunctionFromPdfObject(function)
			if err != nil {
				common.Log.Debug("Error parsing function: %v", err)
				return nil, err
			}
			shading5.Function = append(shading5.Function, fn)
		}
	}
	return &shading5, nil
}

// ToPdfObject returns the PDF representation of the shading dictionary.
func (s *PdfShadingType5) ToPdfObject() core.PdfObject {
	s.PdfShading.ToPdfObject()
	shadingDict, err := s.getShadingDict()
	if err != nil {
		common.Log.Error("Unable to access shading dict")
		return nil
	}
	if s.BitsPerCoordinate != nil {
		shadingDict.Set("BitsPerCoordinate", s.BitsPerCoordinate)
	}
	if s.BitsPerComponent != nil {
		shadingDict.Set("BitsPerComponent", s.BitsPerComponent)
	}
	if s.VerticesPerRow != nil {
		shadingDict.Set("VerticesPerRow", s.VerticesPerRow)
	}
	if s.Decode != nil {
		shadingDict.Set("Decode", s.Decode)
	}
	if s.Function != nil {
		if len(s.Function) == 1 {
			shadingDict.Set("Function", s.Function[0].ToPdfObject())
		} else {
			array := core.MakeArray()
			for _, function := range s.Function {
				array.Append(function.ToPdfObject())
			}
			shadingDict.Set("Function", array)
		}
	}
	return s.primitive
}

// PdfShadingType7 is a Tensor-product patch mesh.
type PdfShadingType7 struct {
	*PdfShading
	BitsPerCoordinate *core.PdfObjectInteger
	BitsPerComponent  *core.PdfObjectInteger
	BitsPerFlag       *core.PdfObjectInteger
	Decode            *core.PdfObjectArray
	Function          []PdfFunction
}

func newPdfShadingType7FromDict(dict *core.PdfObjectDictionary) (*PdfShadingType7, error) {
	shading7 := PdfShadingType7{}
	bitsPerCoordinate := dict.Get("BitsPerCoordinate")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerCoordinate")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok := bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerCoordinate not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading7.BitsPerCoordinate = num
	bitsPerCoordinate = dict.Get("BitsPerComponent")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerComponent")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok = bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerComponent not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading7.BitsPerComponent = num
	bitsPerCoordinate = dict.Get("BitsPerFlag")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerFlag")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok = bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerFlag not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading7.BitsPerFlag = num
	bitsPerCoordinate = dict.Get("Decode")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: Decode")
		return nil, ErrRequiredAttributeMissing
	}
	arr, ok := bitsPerCoordinate.(*core.PdfObjectArray)
	if !ok {
		common.Log.Debug("Decode not an array (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading7.Decode = arr
	if function := dict.Get("Function"); function != nil {
		shading7.Function = []PdfFunction{}
		if arr, ok := function.(*core.PdfObjectArray); ok {
			for _, obj := range arr.Elements() {
				function, err := newPdfFunctionFromPdfObject(obj)
				if err != nil {
					common.Log.Debug("Error parsing function: %v", err)
					return nil, err
				}
				shading7.Function = append(shading7.Function, function)
			}
		} else {
			fn, err := newPdfFunctionFromPdfObject(function)
			if err != nil {
				common.Log.Debug("Error parsing function: %v", err)
				return nil, err
			}
			shading7.Function = append(shading7.Function, fn)
		}
	}
	return &shading7, nil
}

// ToPdfObject returns the PDF representation of the shading dictionary.
func (s *PdfShadingType7) ToPdfObject() core.PdfObject {
	s.PdfShading.ToPdfObject()
	shadingDict, err := s.getShadingDict()
	if err != nil {
		common.Log.Error("Unable to access shading dict")
		return nil
	}
	if s.BitsPerCoordinate != nil {
		shadingDict.Set("BitsPerCoordinate", s.BitsPerCoordinate)
	}
	if s.BitsPerComponent != nil {
		shadingDict.Set("BitsPerComponent", s.BitsPerComponent)
	}
	if s.BitsPerFlag != nil {
		shadingDict.Set("BitsPerFlag", s.BitsPerFlag)
	}
	if s.Decode != nil {
		shadingDict.Set("Decode", s.Decode)
	}
	if s.Function != nil {
		if len(s.Function) == 1 {
			shadingDict.Set("Function", s.Function[0].ToPdfObject())
		} else {
			array := core.MakeArray()
			for _, function := range s.Function {
				array.Append(function.ToPdfObject())
			}
			shadingDict.Set("Function", array)
		}
	}
	return s.primitive
}

// PdfShadingType6 is a Coons patch mesh.
type PdfShadingType6 struct {
	*PdfShading
	BitsPerCoordinate *core.PdfObjectInteger
	BitsPerComponent  *core.PdfObjectInteger
	BitsPerFlag       *core.PdfObjectInteger
	Decode            *core.PdfObjectArray
	Function          []PdfFunction
}

func newPdfShadingType6FromDict(dict *core.PdfObjectDictionary) (*PdfShadingType6, error) {
	shading6 := PdfShadingType6{}
	bitsPerCoordinate := dict.Get("BitsPerCoordinate")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerCoordinate")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok := bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerCoordinate not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading6.BitsPerCoordinate = num
	bitsPerCoordinate = dict.Get("BitsPerComponent")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerComponent")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok = bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerComponent not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading6.BitsPerComponent = num
	bitsPerCoordinate = dict.Get("BitsPerFlag")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerFlag")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok = bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerFlag not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading6.BitsPerFlag = num
	bitsPerCoordinate = dict.Get("Decode")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: Decode")
		return nil, ErrRequiredAttributeMissing
	}
	arr, ok := bitsPerCoordinate.(*core.PdfObjectArray)
	if !ok {
		common.Log.Debug("Decode not an array (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading6.Decode = arr
	if function := dict.Get("Function"); function != nil {
		shading6.Function = []PdfFunction{}
		if arr, ok := function.(*core.PdfObjectArray); ok {
			for _, obj := range arr.Elements() {
				function, err := newPdfFunctionFromPdfObject(obj)
				if err != nil {
					common.Log.Debug("Error parsing function: %v", err)
					return nil, err
				}
				shading6.Function = append(shading6.Function, function)
			}
		} else {
			fn, err := newPdfFunctionFromPdfObject(function)
			if err != nil {
				common.Log.Debug("Error parsing function: %v", err)
				return nil, err
			}
			shading6.Function = append(shading6.Function, fn)
		}
	}
	return &shading6, nil
}

// ToPdfObject returns the PDF representation of the shading dictionary.
func (s *PdfShadingType6) ToPdfObject() core.PdfObject {
	s.PdfShading.ToPdfObject()
	shadingDict, err := s.getShadingDict()
	if err != nil {
		common.Log.Error("Unable to access shading dict")
		return nil
	}
	if s.BitsPerCoordinate != nil {
		shadingDict.Set("BitsPerCoordinate", s.BitsPerCoordinate)
	}
	if s.BitsPerComponent != nil {
		shadingDict.Set("BitsPerComponent", s.BitsPerComponent)
	}
	if s.BitsPerFlag != nil {
		shadingDict.Set("BitsPerFlag", s.BitsPerFlag)
	}
	if s.Decode != nil {
		shadingDict.Set("Decode", s.Decode)
	}
	if s.Function != nil {
		if len(s.Function) == 1 {
			shadingDict.Set("Function", s.Function[0].ToPdfObject())
		} else {
			array := core.MakeArray()
			for _, function := range s.Function {
				array.Append(function.ToPdfObject())
			}
			shadingDict.Set("Function", array)
		}
	}
	return s.primitive
}

// PdfShadingPatternType2 is shading patterns that will use a Type 2 shading pattern (Axial).
type PdfShadingPatternType2 struct {
	*PdfPattern
	Shading   *PdfShadingType2
	Matrix    *core.PdfObjectArray
	ExtGState core.PdfObject
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

// PdfShadingPatternType1 is shading patterns that will use a Type 1 shading pattern (Function-based).
type PdfShadingPatternType1 struct {
	*PdfPattern
	Shading   *PdfShadingType1
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
