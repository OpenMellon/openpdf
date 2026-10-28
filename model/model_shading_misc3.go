package model

import (
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

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

// PdfShadingType7 is a Tensor-product patch mesh.
type PdfShadingType7 struct {
	*PdfShading
	BitsPerCoordinate *core.PdfObjectInteger
	BitsPerComponent  *core.PdfObjectInteger
	BitsPerFlag       *core.PdfObjectInteger
	Decode            *core.PdfObjectArray
	Function          []PdfFunction
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
