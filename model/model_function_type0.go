package model

import (
	"errors"
	_ "image/gif"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/sampling"
)

func (f *PdfFunctionType0) processSamples() error {
	resampleBytes := sampling.ResampleBytes(f.rawData, f.BitsPerSample)
	f.samples = resampleBytes
	return nil
}

func newPdfFunctionType0FromStream(stream *core.PdfObjectStream) (*PdfFunctionType0, error) {
	fn0 := &PdfFunctionType0{}
	fn0.container = stream
	dict := stream.PdfObjectDictionary
	arr, ok := core.TraceToDirectObject(dict.Get("Domain")).(*core.PdfObjectArray)
	if !ok {
		common.Log.Error("Domain not specified")
		return nil, errors.New("required attribute missing or invalid")
	}
	if arr.Len() < 0 || arr.Len()%2 != 0 {
		common.Log.Error("Domain invalid")
		return nil, errors.New("invalid domain range")
	}
	fn0.NumInputs = arr.Len() / 2
	floats, err := arr.ToFloat64Array()
	if err != nil {
		return nil, err
	}
	fn0.Domain = floats
	arr, ok = core.TraceToDirectObject(dict.Get("Range")).(*core.PdfObjectArray)
	if !ok {
		common.Log.Error("Range not specified")
		return nil, errors.New("required attribute missing or invalid")
	}
	if arr.Len() < 0 || arr.Len()%2 != 0 {
		return nil, errors.New("invalid range")
	}
	fn0.NumOutputs = arr.Len() / 2
	floats2, err := arr.ToFloat64Array()
	if err != nil {
		return nil, err
	}
	fn0.Range = floats2
	arr, ok = core.TraceToDirectObject(dict.Get("Size")).(*core.PdfObjectArray)
	if !ok {
		common.Log.Error("Size not specified")
		return nil, errors.New("required attribute missing or invalid")
	}
	nums, err := arr.ToIntegerArray()
	if err != nil {
		return nil, err
	}
	if len(nums) != fn0.NumInputs {
		common.Log.Error("Table size not matching number of inputs")
		return nil, errors.New("range check")
	}
	fn0.Size = nums
	num, ok := core.TraceToDirectObject(dict.Get("BitsPerSample")).(*core.PdfObjectInteger)
	if !ok {
		common.Log.Error("BitsPerSample not specified")
		return nil, errors.New("required attribute missing or invalid")
	}
	if *num != 1 && *num != 2 && *num != 4 && *num != 8 && *num != 12 && *num != 16 && *num != 24 && *num != 32 {
		common.Log.Error("Bits per sample outside range (%d)", *num)
		return nil, errors.New("range check")
	}
	fn0.BitsPerSample = int(*num)
	fn0.Order = 1
	num2, ok := core.TraceToDirectObject(dict.Get("Order")).(*core.PdfObjectInteger)
	if ok {
		if *num2 != 1 && *num2 != 3 {
			common.Log.Error("Invalid order (%d)", *num2)
			return nil, errors.New("range check")
		}
		fn0.Order = int(*num2)
	}
	arr, ok = core.TraceToDirectObject(dict.Get("Encode")).(*core.PdfObjectArray)
	if ok {
		floats, err := arr.ToFloat64Array()
		if err != nil {
			return nil, err
		}
		fn0.Encode = floats
	}
	arr, ok = core.TraceToDirectObject(dict.Get("Decode")).(*core.PdfObjectArray)
	if ok {
		floats, err := arr.ToFloat64Array()
		if err != nil {
			return nil, err
		}
		fn0.Decode = floats
	}
	data, err := core.DecodeStream(stream)
	if err != nil {
		return nil, err
	}
	fn0.rawData = data
	return fn0, nil
}

// Evaluate runs the function on the passed in slice and returns the results.
func (f *PdfFunctionType0) Evaluate(x []float64) ([]float64, error) {
	if len(x) != f.NumInputs {
		common.Log.Error("Number of inputs not matching what is needed")
		return nil, errors.New("range check error")
	}
	if f.samples == nil {
		err := f.processSamples()
		if err != nil {
			return nil, err
		}
	}
	floats := f.Encode
	if floats == nil {
		floats = []float64{}
		for i := 0; i < len(f.Size); i++ {
			floats = append(floats, 0)
			floats = append(floats, float64(f.Size[i]-1))
		}
	}
	floats2 := f.Decode
	if floats2 == nil {
		floats2 = f.Range
	}
	var nums, nums2 [maxFunctionInputs]int
	var floats3 [maxFunctionInputs]float64
	nums3, nums4, floats4 := nums[:], nums2[:], floats3[:]
	if f.NumInputs > maxFunctionInputs {
		nums3 = make([]int, f.NumInputs)
		nums4 = make([]int, f.NumInputs)
		floats4 = make([]float64, f.NumInputs)
	}
	for i := 0; i < f.NumInputs; i++ {
		v := math.Min(math.Max(x[i], f.Domain[2*i]), f.Domain[2*i+1])
		linearInterpolate := imageutil.LinearInterpolate(v, f.Domain[2*i], f.Domain[2*i+1], floats[2*i], floats[2*i+1])
		val := math.Min(math.Max(linearInterpolate, 0), float64(f.Size[i]-1))
		floor := max(min(int(math.Floor(val)), f.Size[i]-2), 0)
		nums3[i] = floor
		nums4[i] = floor
		if f.Size[i] > 1 {
			nums4[i] = floor + 1
		}
		floats4[i] = val - float64(floor)
	}
	var nums5 [maxFunctionInputs]int
	nums6 := nums5[:]
	if f.NumInputs > maxFunctionInputs {
		nums6 = make([]int, f.NumInputs)
	}
	n, count := 0, 1
	for i := 0; i < f.NumInputs; i++ {
		nums6[i] = count
		n += nums3[i] * count
		count *= f.Size[i]
	}
	var nums7 [maxSampledInputs]int
	nums8 := nums7[:0]
	for i := 0; i < f.NumInputs; i++ {
		if nums4[i] == nums3[i] || floats4[i] == 0 {
			continue
		}
		if len(nums8) == maxSampledInputs {
			if floats4[i] >= 0.5 {
				n += nums6[i]
			}
			continue
		}
		nums8 = append(nums8, i)
	}
	v := float64(uint64(1)<<uint(f.BitsPerSample)) - 1
	floats5 := make([]float64, f.NumOutputs)
	for i := 0; i < 1<<uint(len(nums8)); i++ {
		val := 1.0
		count := n
		for j, num := range nums8 {
			if i&(1<<uint(j)) != 0 {
				count += nums6[num]
				val *= floats4[num]
			} else {
				val *= 1 - floats4[num]
			}
		}
		if val == 0 {
			continue
		}
		num := count * f.NumOutputs
		for i := 0; i < f.NumOutputs; i++ {
			size := num + i
			if size < 0 || size >= len(f.samples) {
				common.Log.Debug("WARN: not enough input samples to determine output values. Output may be incorrect.")
				continue
			}
			floats5[i] += val * imageutil.LinearInterpolate(float64(f.samples[size]), 0, v, floats2[2*i], floats2[2*i+1])
		}
	}
	return clampToRange(floats5, f.Range), nil
}

// PdfFunctionType0 uses a sequence of sample values (contained in a stream) to provide an approximation
// for functions whose domains and ranges are bounded. The samples are organized as an m-dimensional
// table in which each entry has n components
type PdfFunctionType0 struct {
	Domain        []float64
	Range         []float64
	NumInputs     int
	NumOutputs    int
	Size          []int
	BitsPerSample int
	Order         int
	Encode        []float64
	Decode        []float64
	rawData       []byte
	samples       []uint32
	container     *core.PdfObjectStream
}

// ToPdfObject returns the PDF representation of the function.
func (f *PdfFunctionType0) ToPdfObject() core.PdfObject {
	if f.container == nil {
		f.container = &core.PdfObjectStream{}
	}
	dict := core.MakeDict()
	dict.Set("FunctionType", core.MakeInteger(0))
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
	arr3 := &core.PdfObjectArray{}
	for _, size := range f.Size {
		arr3.Append(core.MakeInteger(int64(size)))
	}
	dict.Set("Size", arr3)
	dict.Set("BitsPerSample", core.MakeInteger(int64(f.BitsPerSample)))
	if f.Order != 1 {
		dict.Set("Order", core.MakeInteger(int64(f.Order)))
	}
	dict.Set("Length", core.MakeInteger(int64(len(f.rawData))))
	f.container.Stream = f.rawData
	f.container.PdfObjectDictionary = dict
	return f.container
}
