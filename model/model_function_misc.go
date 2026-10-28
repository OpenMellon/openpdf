package model

import (
	"errors"
	_ "image/gif"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/ps"
)

// PdfFunctionType2 defines an exponential interpolation of one input value and n
// output values:
//
//	f(x) = y_0, ..., y_(n-1)
//
// y_j = C0_j + x^N * (C1_j - C0_j); for 0 <= j < n
// When N=1 ; linear interpolation between C0 and C1.
type PdfFunctionType2 struct {
	Domain    []float64
	Range     []float64
	C0        []float64
	C1        []float64
	N         float64
	container *core.PdfIndirectObject
}

// PdfFunctionType4 is a Postscript calculator functions.
type PdfFunctionType4 struct {
	Domain    []float64
	Range     []float64
	Program   *ps.PSProgram
	executor  *ps.PSExecutor
	rawData   []byte
	container *core.PdfObjectStream
}

// ToPdfObject returns the PDF representation of the function.
func (f *PdfFunctionType2) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	dict.Set("FunctionType", core.MakeInteger(2))
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
	if f.C0 != nil {
		arr := &core.PdfObjectArray{}
		for _, v := range f.C0 {
			arr.Append(core.MakeFloat(v))
		}
		dict.Set("C0", arr)
	}
	if f.C1 != nil {
		arr := &core.PdfObjectArray{}
		for _, v := range f.C1 {
			arr.Append(core.MakeFloat(v))
		}
		dict.Set("C1", arr)
	}
	dict.Set("N", core.MakeFloat(f.N))
	if f.container != nil {
		f.container.PdfObject = dict
		return f.container
	}
	return dict
}

func newPdfFunctionFromPdfObject(obj core.PdfObject) (PdfFunction, error) {
	obj = core.ResolveReference(obj)
	if stream, ok := obj.(*core.PdfObjectStream); ok {
		dict := stream.PdfObjectDictionary
		num, ok := dict.Get("FunctionType").(*core.PdfObjectInteger)
		if !ok {
			common.Log.Error("FunctionType number missing")
			return nil, errors.New("invalid parameter or missing")
		}
		if *num == 0 {
			return newPdfFunctionType0FromStream(stream)
		} else if *num == 4 {
			return newPdfFunctionType4FromStream(stream)
		} else {
			return nil, errors.New("invalid function type")
		}
	} else if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
		if !ok {
			common.Log.Error("Function Indirect object not containing dictionary")
			return nil, errors.New("invalid parameter or missing")
		}
		num, ok := dict.Get("FunctionType").(*core.PdfObjectInteger)
		if !ok {
			common.Log.Error("FunctionType number missing")
			return nil, errors.New("invalid parameter or missing")
		}
		if *num == 2 {
			return newPdfFunctionType2FromPdfObject(indirect)
		} else if *num == 3 {
			return newPdfFunctionType3FromPdfObject(indirect)
		} else {
			return nil, errors.New("invalid function type")
		}
	} else if dict, ok := obj.(*core.PdfObjectDictionary); ok {
		num, ok := dict.Get("FunctionType").(*core.PdfObjectInteger)
		if !ok {
			common.Log.Error("FunctionType number missing")
			return nil, errors.New("invalid parameter or missing")
		}
		if *num == 2 {
			return newPdfFunctionType2FromPdfObject(dict)
		} else if *num == 3 {
			return newPdfFunctionType3FromPdfObject(dict)
		} else {
			return nil, errors.New("invalid function type")
		}
	} else {
		common.Log.Debug("Function Type error: %#v", obj)
		return nil, errors.New("type error")
	}
}

// Evaluate runs the function on the passed in slice and returns the results.
func (f *PdfFunctionType2) Evaluate(x []float64) ([]float64, error) {
	if len(x) != 1 {
		common.Log.Error("Only one input allowed")
		return nil, errors.New("range check")
	}
	floats := []float64{0.0}
	if f.C0 != nil {
		floats = f.C0
	}
	floats2 := []float64{1.0}
	if f.C1 != nil {
		floats2 = f.C1
	}
	x = clampToDomain(x, f.Domain)
	var floats3 []float64
	for i := 0; i < len(floats); i++ {
		v := floats[i] + math.Pow(x[0], f.N)*(floats2[i]-floats[i])
		floats3 = append(floats3, v)
	}
	return clampToRange(floats3, f.Range), nil
}

// Evaluate runs the function. Input is [x1 x2 x3].
func (f *PdfFunctionType4) Evaluate(xVec []float64) ([]float64, error) {
	if f.executor == nil {
		f.executor = ps.NewPSExecutor(f.Program)
	}
	xVec = clampToDomain(xVec, f.Domain)
	var psObjects []ps.PSObject
	for _, f := range xVec {
		psObjects = append(psObjects, ps.MakeReal(f))
	}
	execute, err := f.executor.Execute(psObjects)
	if err != nil {
		return nil, err
	}
	psObjectArrayToFloat64Array, err := ps.PSObjectArrayToFloat64Array(execute)
	if err != nil {
		return nil, err
	}
	return clampToRange(psObjectArrayToFloat64Array, f.Range), nil
}

// NewPdfFunctionFromPdfObject loads a PDF function from a PdfObject (a stream for
// Type 0 and Type 4 functions, or a dictionary for Type 2 and Type 3).
func NewPdfFunctionFromPdfObject(obj core.PdfObject) (PdfFunction, error) {
	return newPdfFunctionFromPdfObject(obj)
}

// PdfFunction interface represents the common methods of a function in PDF.
type PdfFunction interface {
	Evaluate([]float64) ([]float64, error)
	ToPdfObject() core.PdfObject
}

func newPdfFunctionType4FromStream(stream *core.PdfObjectStream) (*PdfFunctionType4, error) {
	fn4 := &PdfFunctionType4{}
	fn4.container = stream
	dict := stream.PdfObjectDictionary
	arr, ok := core.TraceToDirectObject(dict.Get("Domain")).(*core.PdfObjectArray)
	if !ok {
		common.Log.Error("Domain not specified")
		return nil, errors.New("required attribute missing or invalid")
	}
	if arr.Len()%2 != 0 {
		common.Log.Error("Domain invalid")
		return nil, errors.New("invalid domain range")
	}
	floats, err := arr.ToFloat64Array()
	if err != nil {
		return nil, err
	}
	fn4.Domain = floats
	arr, ok = core.TraceToDirectObject(dict.Get("Range")).(*core.PdfObjectArray)
	if ok {
		if arr.Len() < 0 || arr.Len()%2 != 0 {
			return nil, errors.New("invalid range")
		}
		floats, err := arr.ToFloat64Array()
		if err != nil {
			return nil, err
		}
		fn4.Range = floats
	}
	data, err := core.DecodeStream(stream)
	if err != nil {
		return nil, err
	}
	fn4.rawData = data
	psParser := ps.NewPSParser(data)
	psProgram, err := psParser.Parse()
	if err != nil {
		return nil, err
	}
	fn4.Program = psProgram
	return fn4, nil
}

func newPdfFunctionType2FromPdfObject(obj core.PdfObject) (*PdfFunctionType2, error) {
	fn2 := &PdfFunctionType2{}
	var dict *core.PdfObjectDictionary
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		dict2, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
		if !ok {
			return nil, errors.New("type check error")
		}
		fn2.container = indirect
		dict = dict2
	} else if dict2, ok := obj.(*core.PdfObjectDictionary); ok {
		dict = dict2
	} else {
		return nil, errors.New("type check error")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("FUNC2: %s", dict.String())
	}
	arr, ok := core.TraceToDirectObject(dict.Get("Domain")).(*core.PdfObjectArray)
	if !ok {
		common.Log.Error("Domain not specified")
		return nil, errors.New("required attribute missing or invalid")
	}
	if arr.Len() < 0 || arr.Len()%2 != 0 {
		common.Log.Error("Domain range invalid")
		return nil, errors.New("invalid domain range")
	}
	floats, err := arr.ToFloat64Array()
	if err != nil {
		return nil, err
	}
	fn2.Domain = floats
	arr, ok = core.TraceToDirectObject(dict.Get("Range")).(*core.PdfObjectArray)
	if ok {
		if arr.Len() < 0 || arr.Len()%2 != 0 {
			return nil, errors.New("invalid range")
		}
		floats, err := arr.ToFloat64Array()
		if err != nil {
			return nil, err
		}
		fn2.Range = floats
	}
	arr, ok = core.TraceToDirectObject(dict.Get("C0")).(*core.PdfObjectArray)
	if ok {
		floats, err := arr.ToFloat64Array()
		if err != nil {
			return nil, err
		}
		fn2.C0 = floats
	}
	arr, ok = core.TraceToDirectObject(dict.Get("C1")).(*core.PdfObjectArray)
	if ok {
		floats, err := arr.ToFloat64Array()
		if err != nil {
			return nil, err
		}
		fn2.C1 = floats
	}
	if len(fn2.C0) != len(fn2.C1) {
		common.Log.Error("C0 and C1 not matching")
		return nil, core.ErrRangeError
	}
	N, err := core.GetNumberAsFloat(core.TraceToDirectObject(dict.Get("N")))
	if err != nil {
		common.Log.Error("N missing or invalid, dict: %s", dict.String())
		return nil, err
	}
	fn2.N = N
	return fn2, nil
}

// ToPdfObject returns the PDF representation of the function.
func (f *PdfFunctionType4) ToPdfObject() core.PdfObject {
	stream := f.container
	if stream == nil {
		f.container = &core.PdfObjectStream{}
		stream = f.container
	}
	dict := core.MakeDict()
	dict.Set("FunctionType", core.MakeInteger(4))
	arr := &core.PdfObjectArray{}
	for _, domain := range f.Domain {
		arr.Append(core.MakeFloat(domain))
	}
	dict.Set("Domain", arr)
	arr2 := &core.PdfObjectArray{}
	for _, v := range f.Range {
		arr2.Append(core.MakeFloat(v))
	}
	dict.Set("Range", arr2)
	if f.rawData == nil && f.Program != nil {
		f.rawData = []byte(f.Program.String())
	}
	dict.Set("Length", core.MakeInteger(int64(len(f.rawData))))
	stream.Stream = f.rawData
	stream.PdfObjectDictionary = dict
	return stream
}
