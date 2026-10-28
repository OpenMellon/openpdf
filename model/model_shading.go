package model

import (
	"errors"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// PdfShadingType2 is an Axial shading.
type PdfShadingType2 struct {
	*PdfShading
	Coords   *core.PdfObjectArray
	Domain   *core.PdfObjectArray
	Function []PdfFunction
	Extend   *core.PdfObjectArray
}

// NewPdfShadingType2 creates an empty shading type 2 dictionary.
func NewPdfShadingType2() *PdfShadingType2 {
	shading2 := &PdfShadingType2{}
	shading2.PdfShading = &PdfShading{}
	shading2.PdfShading.primitive = core.MakeIndirectObject(core.MakeDict())
	shading2.PdfShading.context = shading2
	return shading2
}

// ToPdfObject returns the PDF representation of the shading dictionary.
func (s *PdfShadingType2) ToPdfObject() core.PdfObject {
	s.PdfShading.ToPdfObject()
	shadingDict, err := s.getShadingDict()
	if err != nil {
		common.Log.Error("Unable to access shading dict")
		return nil
	}
	if shadingDict == nil {
		common.Log.Error("Shading dict is nil")
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

func newPdfShadingType2FromDict(dict *core.PdfObjectDictionary) (*PdfShadingType2, error) {
	shading2 := PdfShadingType2{}
	coords := dict.Get("Coords")
	if coords == nil {
		common.Log.Debug("Required attribute missing:  Coords")
		return nil, ErrRequiredAttributeMissing
	}
	arr, ok := coords.(*core.PdfObjectArray)
	if !ok {
		common.Log.Debug("Coords not an array (got %T)", coords)
		return nil, errors.New("type check error")
	}
	if arr.Len() != 4 {
		common.Log.Debug("Coords length not 4 (got %d)", arr.Len())
		return nil, errors.New("invalid attribute")
	}
	shading2.Coords = arr
	if domain := dict.Get("Domain"); domain != nil {
		domain = core.TraceToDirectObject(domain)
		arr, ok := domain.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Domain not an array (got %T)", domain)
			return nil, errors.New("type check error")
		}
		shading2.Domain = arr
	}
	coords = dict.Get("Function")
	if coords == nil {
		common.Log.Debug("Required attribute missing:  Function")
		return nil, ErrRequiredAttributeMissing
	}
	shading2.Function = []PdfFunction{}
	if arr, ok := coords.(*core.PdfObjectArray); ok {
		for _, obj := range arr.Elements() {
			function, err := newPdfFunctionFromPdfObject(obj)
			if err != nil {
				common.Log.Debug("Error parsing function: %v", err)
				return nil, err
			}
			shading2.Function = append(shading2.Function, function)
		}
	} else {
		function, err := newPdfFunctionFromPdfObject(coords)
		if err != nil {
			common.Log.Debug("Error parsing function: %v", err)
			return nil, err
		}
		shading2.Function = append(shading2.Function, function)
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
		shading2.Extend = arr
	}
	return &shading2, nil
}

// PdfShading represents a shading dictionary. There are 7 types of shading,
// indicatedby the shading type variable:
// 1: Function-based shading.
// 2: Axial shading.
// 3: Radial shading.
// 4: Free-form Gouraud-shaded triangle mesh.
// 5: Lattice-form Gouraud-shaded triangle mesh.
// 6: Coons patch mesh.
// 7: Tensor-product patch mesh.
// types 4-7 are contained in a stream object, where the dictionary is given by the stream dictionary.
type PdfShading struct {
	ShadingType *core.PdfObjectInteger
	ColorSpace  PdfColorspace
	Background  *core.PdfObjectArray
	BBox        *PdfRectangle
	AntiAlias   *core.PdfObjectBool
	context     PdfModel
	primitive   core.PdfObject
}

func newPdfShadingFromPdfObject(obj core.PdfObject) (*PdfShading, error) {
	shading := &PdfShading{}
	var dict *core.PdfObjectDictionary
	if indirect, ok := core.GetIndirect(obj); ok {
		shading.primitive = indirect
		dict2, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
		if !ok {
			common.Log.Debug("Object not a dictionary type")
			return nil, core.ErrTypeError
		}
		dict = dict2
	} else if stream, ok := core.GetStream(obj); ok {
		shading.primitive = stream
		dict = stream.PdfObjectDictionary
	} else if dict2, ok := core.GetDict(obj); ok {
		shading.primitive = dict2
		dict = dict2
	} else {
		common.Log.Debug("Object type unexpected (%T)", obj)
		return nil, core.ErrTypeError
	}
	if dict == nil {
		common.Log.Debug("Dictionary missing")
		return nil, errors.New("dict missing")
	}
	obj = dict.Get("ShadingType")
	if obj == nil {
		common.Log.Debug("Required shading type missing")
		return nil, ErrRequiredAttributeMissing
	}
	obj = core.TraceToDirectObject(obj)
	num, ok := obj.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("Invalid type for shading type (%T)", obj)
		return nil, core.ErrTypeError
	}
	if *num < 1 || *num > 7 {
		common.Log.Debug("Invalid shading type, not 1-7 (got %d)", *num)
		return nil, core.ErrTypeError
	}
	shading.ShadingType = num
	obj = dict.Get("ColorSpace")
	if obj == nil {
		common.Log.Debug("Required ColorSpace entry missing")
		return nil, ErrRequiredAttributeMissing
	}
	cs, err := NewPdfColorspaceFromPdfObject(obj)
	if err != nil {
		common.Log.Debug("Failed loading colorspace: %v", err)
		return nil, err
	}
	shading.ColorSpace = cs
	obj = dict.Get("Background")
	if obj != nil {
		obj = core.TraceToDirectObject(obj)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Background should be specified by an array (got %T)", obj)
			return nil, core.ErrTypeError
		}
		shading.Background = arr
	}
	obj = dict.Get("BBox")
	if obj != nil {
		obj = core.TraceToDirectObject(obj)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Background should be specified by an array (got %T)", obj)
			return nil, core.ErrTypeError
		}
		rect, err := NewPdfRectangle(*arr)
		if err != nil {
			common.Log.Debug("BBox error: %v", err)
			return nil, err
		}
		shading.BBox = rect
	}
	obj = dict.Get("AntiAlias")
	if obj != nil {
		obj = core.TraceToDirectObject(obj)
		boolObj, ok := obj.(*core.PdfObjectBool)
		if !ok {
			common.Log.Debug("AntiAlias invalid type, should be bool (got %T)", obj)
			return nil, core.ErrTypeError
		}
		shading.AntiAlias = boolObj
	}
	switch *num {
	case 1:
		shadingType1, err := newPdfShadingType1FromDict(dict)
		if err != nil {
			return nil, err
		}
		shadingType1.PdfShading = shading
		shading.context = shadingType1
		return shading, nil
	case 2:
		shadingType2, err := newPdfShadingType2FromDict(dict)
		if err != nil {
			return nil, err
		}
		shadingType2.PdfShading = shading
		shading.context = shadingType2
		return shading, nil
	case 3:
		shadingType3, err := newPdfShadingType3FromDict(dict)
		if err != nil {
			return nil, err
		}
		shadingType3.PdfShading = shading
		shading.context = shadingType3
		return shading, nil
	case 4:
		shadingType4, err := newPdfShadingType4FromDict(dict)
		if err != nil {
			return nil, err
		}
		shadingType4.PdfShading = shading
		shading.context = shadingType4
		return shading, nil
	case 5:
		shadingType5, err := newPdfShadingType5FromDict(dict)
		if err != nil {
			return nil, err
		}
		shadingType5.PdfShading = shading
		shading.context = shadingType5
		return shading, nil
	case 6:
		shadingType6, err := newPdfShadingType6FromDict(dict)
		if err != nil {
			return nil, err
		}
		shadingType6.PdfShading = shading
		shading.context = shadingType6
		return shading, nil
	case 7:
		shadingType7, err := newPdfShadingType7FromDict(dict)
		if err != nil {
			return nil, err
		}
		shadingType7.PdfShading = shading
		shading.context = shadingType7
		return shading, nil
	}
	return nil, errors.New("unknown shading type")
}

// GetContainingPdfObject returns the container of the shading object (indirect object).
func (s *PdfShading) GetContainingPdfObject() core.PdfObject { return s.primitive }

// SetContext set the sub annotation (context).
func (s *PdfShading) SetContext(ctx PdfModel) { s.context = ctx }

// ToPdfObject returns the PDF representation of the shading dictionary.
func (s *PdfShading) ToPdfObject() core.PdfObject {
	obj := s.primitive
	shadingDict, err := s.getShadingDict()
	if err != nil {
		common.Log.Error("Unable to access shading dict")
		return nil
	}
	if s.ShadingType != nil {
		shadingDict.Set("ShadingType", s.ShadingType)
	}
	if s.ColorSpace != nil {
		shadingDict.Set("ColorSpace", s.ColorSpace.ToPdfObject())
	}
	if s.Background != nil {
		shadingDict.Set("Background", s.Background)
	}
	if s.BBox != nil {
		shadingDict.Set("BBox", s.BBox.ToPdfObject())
	}
	if s.AntiAlias != nil {
		shadingDict.Set("AntiAlias", s.AntiAlias)
	}
	return obj
}

func (s *PdfShading) getShadingDict() (*core.PdfObjectDictionary, error) {
	obj := s.primitive
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
		if !ok {
			return nil, core.ErrTypeError
		}
		return dict, nil
	} else if stream, ok := obj.(*core.PdfObjectStream); ok {
		return stream.PdfObjectDictionary, nil
	} else if dict, ok := obj.(*core.PdfObjectDictionary); ok {
		return dict, nil
	} else {
		common.Log.Debug("Unable to access shading dictionary")
		return nil, core.ErrTypeError
	}
}

// GetContext returns a reference to the subshading entry as represented by PdfShadingType1-7.
func (s *PdfShading) GetContext() PdfModel { return s.context }

// PdfShadingType1 is a Function-based shading.
type PdfShadingType1 struct {
	*PdfShading
	Domain   *core.PdfObjectArray
	Matrix   *core.PdfObjectArray
	Function []PdfFunction
}

// ToPdfObject returns the PDF representation of the shading dictionary.
func (s *PdfShadingType1) ToPdfObject() core.PdfObject {
	s.PdfShading.ToPdfObject()
	shadingDict, err := s.getShadingDict()
	if err != nil {
		common.Log.Error("Unable to access shading dict")
		return nil
	}
	if s.Domain != nil {
		shadingDict.Set("Domain", s.Domain)
	}
	if s.Matrix != nil {
		shadingDict.Set("Matrix", s.Matrix)
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

// NewPdfShadingType1 creates an empty shading type 1 dictionary.
func NewPdfShadingType1() *PdfShadingType1 {
	shading1 := &PdfShadingType1{}
	shading1.PdfShading = &PdfShading{}
	shading1.PdfShading.primitive = core.MakeIndirectObject(core.MakeDict())
	shading1.PdfShading.context = shading1
	return shading1
}

func newPdfShadingType1FromDict(dict *core.PdfObjectDictionary) (*PdfShadingType1, error) {
	shading1 := PdfShadingType1{}
	if domain := dict.Get("Domain"); domain != nil {
		domain = core.TraceToDirectObject(domain)
		arr, ok := domain.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Domain not an array (got %T)", domain)
			return nil, errors.New("type check error")
		}
		shading1.Domain = arr
	}
	if matrix := dict.Get("Matrix"); matrix != nil {
		matrix = core.TraceToDirectObject(matrix)
		arr, ok := matrix.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Matrix not an array (got %T)", matrix)
			return nil, errors.New("type check error")
		}
		shading1.Matrix = arr
	}
	function := dict.Get("Function")
	if function == nil {
		common.Log.Debug("Required attribute missing:  Function")
		return nil, ErrRequiredAttributeMissing
	}
	shading1.Function = []PdfFunction{}
	if arr, ok := function.(*core.PdfObjectArray); ok {
		for _, obj := range arr.Elements() {
			function, err := newPdfFunctionFromPdfObject(obj)
			if err != nil {
				common.Log.Debug("Error parsing function: %v", err)
				return nil, err
			}
			shading1.Function = append(shading1.Function, function)
		}
	} else {
		fn, err := newPdfFunctionFromPdfObject(function)
		if err != nil {
			common.Log.Debug("Error parsing function: %v", err)
			return nil, err
		}
		shading1.Function = append(shading1.Function, fn)
	}
	return &shading1, nil
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
