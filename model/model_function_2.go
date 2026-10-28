package model

import (
	"errors"
	_ "image/gif"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/imageutil"
)

// PdfFunctionType3 defines stitching of the subdomains of several 1-input functions to produce
// a single new 1-input function.
type PdfFunctionType3 struct {
	Domain    []float64
	Range     []float64
	Functions []PdfFunction
	Bounds    []float64
	Encode    []float64
	container *core.PdfIndirectObject
}

// ToPdfObject returns the PDF representation of the function.
func (f *PdfFunctionType3) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	dict.Set("FunctionType", core.MakeInteger(3))
	arr := &core.PdfObjectArray{}
	for _, domain := range f.Domain {
		arr.Append(core.MakeFloat(domain))
	}
	dict.Set("Domain", arr)
	if f.Range != nil {
		arr := &core.PdfObjectArray{}
		for _, v := range f.Range {
			arr.Append(core.MakeFloat(v))
		}
		dict.Set("Range", arr)
	}
	if f.Functions != nil {
		arr := &core.PdfObjectArray{}
		for _, function := range f.Functions {
			arr.Append(function.ToPdfObject())
		}
		dict.Set("Functions", arr)
	}
	if f.Bounds != nil {
		arr := &core.PdfObjectArray{}
		for _, bound := range f.Bounds {
			arr.Append(core.MakeFloat(bound))
		}
		dict.Set("Bounds", arr)
	}
	if f.Encode != nil {
		arr := &core.PdfObjectArray{}
		for _, v := range f.Encode {
			arr.Append(core.MakeFloat(v))
		}
		dict.Set("Encode", arr)
	}
	if f.container != nil {
		f.container.PdfObject = dict
		return f.container
	}
	return dict
}

func newPdfFunctionType3FromPdfObject(obj core.PdfObject) (*PdfFunctionType3, error) {
	fn3 := &PdfFunctionType3{}
	var dict *core.PdfObjectDictionary
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		dict2, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
		if !ok {
			return nil, errors.New("type check error")
		}
		fn3.container = indirect
		dict = dict2
	} else if dict2, ok := obj.(*core.PdfObjectDictionary); ok {
		dict = dict2
	} else {
		return nil, errors.New("type check error")
	}
	arr, ok := core.TraceToDirectObject(dict.Get("Domain")).(*core.PdfObjectArray)
	if !ok {
		common.Log.Error("Domain not specified")
		return nil, errors.New("required attribute missing or invalid")
	}
	if arr.Len() != 2 {
		common.Log.Error("Domain invalid")
		return nil, errors.New("invalid domain range")
	}
	floats, err := arr.ToFloat64Array()
	if err != nil {
		return nil, err
	}
	fn3.Domain = floats
	arr, ok = core.TraceToDirectObject(dict.Get("Range")).(*core.PdfObjectArray)
	if ok {
		if arr.Len() < 0 || arr.Len()%2 != 0 {
			return nil, errors.New("invalid range")
		}
		floats, err := arr.ToFloat64Array()
		if err != nil {
			return nil, err
		}
		fn3.Range = floats
	}
	arr, ok = core.TraceToDirectObject(dict.Get("Functions")).(*core.PdfObjectArray)
	if !ok {
		common.Log.Error("Functions not specified")
		return nil, errors.New("required attribute missing or invalid")
	}
	fn3.Functions = []PdfFunction{}
	for _, obj := range arr.Elements() {
		function, err := newPdfFunctionFromPdfObject(obj)
		if err != nil {
			return nil, err
		}
		fn3.Functions = append(fn3.Functions, function)
	}
	arr, ok = core.TraceToDirectObject(dict.Get("Bounds")).(*core.PdfObjectArray)
	if !ok {
		common.Log.Error("Bounds not specified")
		return nil, errors.New("required attribute missing or invalid")
	}
	floats2, err := arr.ToFloat64Array()
	if err != nil {
		return nil, err
	}
	fn3.Bounds = floats2
	if len(fn3.Bounds) != len(fn3.Functions)-1 {
		common.Log.Error("Bounds (%d) and num functions (%d) not matching", len(fn3.Bounds), len(fn3.Functions))
		return nil, errors.New("range check")
	}
	arr, ok = core.TraceToDirectObject(dict.Get("Encode")).(*core.PdfObjectArray)
	if !ok {
		common.Log.Error("Encode not specified")
		return nil, errors.New("required attribute missing or invalid")
	}
	floats3, err := arr.ToFloat64Array()
	if err != nil {
		return nil, err
	}
	fn3.Encode = floats3
	if len(fn3.Encode) != 2*len(fn3.Functions) {
		common.Log.Error("Len encode (%d) and num functions (%d) not matching up", len(fn3.Encode), len(fn3.Functions))
		return nil, errors.New("range check")
	}
	return fn3, nil
}

// Evaluate runs the function on the passed in slice and returns the results.
func (f *PdfFunctionType3) Evaluate(x []float64) ([]float64, error) {
	if len(x) != 1 {
		common.Log.Error("Only one input allowed")
		return nil, errors.New("range check")
	}
	if len(f.Functions) == 0 {
		common.Log.Error("No subfunctions defined")
		return nil, errors.New("range check")
	}
	if len(f.Domain) < 2 {
		common.Log.Error("Domain underspecified")
		return nil, errors.New("range check")
	}
	if len(f.Bounds) != len(f.Functions)-1 {
		common.Log.Error("Bounds (%d) and num functions (%d) not matching", len(f.Bounds), len(f.Functions))
		return nil, errors.New("range check")
	}
	if len(f.Encode) != 2*len(f.Functions) {
		common.Log.Error("Encode (%d) and num functions (%d) not matching", len(f.Encode), len(f.Functions))
		return nil, errors.New("range check")
	}
	v := math.Min(math.Max(x[0], f.Domain[0]), f.Domain[1])
	n := 0
	for n < len(f.Bounds) && v >= f.Bounds[n] {
		n++
	}
	domain := f.Domain[0]
	if n > 0 {
		domain = f.Bounds[n-1]
	}
	val := f.Domain[1]
	if n < len(f.Bounds) {
		val = f.Bounds[n]
	}
	linearInterpolate := imageutil.LinearInterpolate(v, domain, val, f.Encode[2*n], f.Encode[2*n+1])
	evaluate, err := f.Functions[n].Evaluate([]float64{linearInterpolate})
	if err != nil {
		return nil, err
	}
	return clampToRange(evaluate, f.Range), nil
}
