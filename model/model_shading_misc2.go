package model

import (
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// ToPdfObject returns the PDF representation of the shading dictionary.
func (s *PdfShadingType4) ToPdfObject() core.PdfObject {
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

// NewPdfShadingType3 creates an empty shading type 3 dictionary.
func NewPdfShadingType3() *PdfShadingType3 {
	shading3 := &PdfShadingType3{}
	shading3.PdfShading = &PdfShading{}
	shading3.PdfShading.primitive = core.MakeIndirectObject(core.MakeDict())
	shading3.PdfShading.context = shading3
	return shading3
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

// PdfShadingType4 is a Free-form Gouraud-shaded triangle mesh.
type PdfShadingType4 struct {
	*PdfShading
	BitsPerCoordinate *core.PdfObjectInteger
	BitsPerComponent  *core.PdfObjectInteger
	BitsPerFlag       *core.PdfObjectInteger
	Decode            *core.PdfObjectArray
	Function          []PdfFunction
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

func newPdfShadingType4FromDict(dict *core.PdfObjectDictionary) (*PdfShadingType4, error) {
	shading4 := PdfShadingType4{}
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
	shading4.BitsPerCoordinate = num
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
	shading4.BitsPerComponent = num
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
	shading4.BitsPerFlag = num
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
	shading4.Decode = arr
	if function := dict.Get("Function"); function != nil {
		shading4.Function = []PdfFunction{}
		if arr, ok := function.(*core.PdfObjectArray); ok {
			for _, obj := range arr.Elements() {
				function, err := newPdfFunctionFromPdfObject(obj)
				if err != nil {
					common.Log.Debug("Error parsing function: %v", err)
					return nil, err
				}
				shading4.Function = append(shading4.Function, function)
			}
		} else {
			fn, err := newPdfFunctionFromPdfObject(function)
			if err != nil {
				common.Log.Debug("Error parsing function: %v", err)
				return nil, err
			}
			shading4.Function = append(shading4.Function, fn)
		}
	}
	return &shading4, nil
}

// PdfShadingType3 is a Radial shading.
type PdfShadingType3 struct {
	*PdfShading
	Coords   *core.PdfObjectArray
	Domain   *core.PdfObjectArray
	Function []PdfFunction
	Extend   *core.PdfObjectArray
}
