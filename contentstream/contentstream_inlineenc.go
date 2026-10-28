package contentstream

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"image/jpeg"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// GetEncoder returns the encoder of the inline image.
func (img *ContentStreamInlineImage) GetEncoder() (core.StreamEncoder, error) {
	return newEncoderFromInlineImage(img)
}

func newDCTEncoderFromInlineImage(img *ContentStreamInlineImage) (*core.DCTEncoder, error) {
	v := core.NewDCTEncoder()
	v2 := bytes.NewReader(img.data)
	v3, err := jpeg.DecodeConfig(v2)
	if err != nil {
		common.Log.Debug("Error decoding file: %s", err)
		return nil, err
	}
	switch v3.ColorModel {
	case color.RGBAModel:
		v.BitsPerComponent = 8
		v.ColorComponents = 3
	case color.RGBA64Model:
		v.BitsPerComponent = 16
		v.ColorComponents = 3
	case color.GrayModel:
		v.BitsPerComponent = 8
		v.ColorComponents = 1
	case color.Gray16Model:
		v.BitsPerComponent = 16
		v.ColorComponents = 1
	case color.CMYKModel:
		v.BitsPerComponent = 8
		v.ColorComponents = 4
	case color.YCbCrModel:
		v.BitsPerComponent = 8
		v.ColorComponents = 3
	default:
		return nil, errors.New("unsupported color model")
	}
	v.Width = v3.Width
	v.Height = v3.Height
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("DCT Encoder: %+v", v)
	}
	return v, nil
}

func newMultiEncoderFromInlineImage(img *ContentStreamInlineImage) (*core.MultiEncoder, error) {
	v := core.NewMultiEncoder()
	var dict *core.PdfObjectDictionary
	var objs []core.PdfObject
	if obj := img.DecodeParms; obj != nil {
		dict2, ok := obj.(*core.PdfObjectDictionary)
		if ok {
			dict = dict2
		}
		arr, ok2 := obj.(*core.PdfObjectArray)
		if ok2 {
			for _, obj2 := range arr.Elements() {
				if dict3, ok3 := obj2.(*core.PdfObjectDictionary); ok3 {
					objs = append(objs, dict3)
				} else {
					objs = append(objs, nil)
				}
			}
		}
	}
	obj := img.Filter
	if obj == nil {
		return nil, fmt.Errorf("filter missing")
	}
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("multi filter can only be made from array")
	}
	for n, obj2 := range arr.Elements() {
		name, ok2 := obj2.(*core.PdfObjectName)
		if !ok2 {
			return nil, fmt.Errorf("multi filter array element not a name")
		}
		var obj3 core.PdfObject
		if dict != nil {
			obj3 = dict
		} else {
			if len(objs) > 0 {
				if n >= len(objs) {
					return nil, fmt.Errorf("missing elements in decode params array")
				}
				obj3 = objs[n]
			}
		}
		var dict2 *core.PdfObjectDictionary
		if dict3, ok3 := obj3.(*core.PdfObjectDictionary); ok3 {
			dict2 = dict3
		}
		switch *name {
		case core.StreamEncodingFilterNameFlate, "Fl":
			v2, err := newFlateEncoderFromInlineImage(img, dict2)
			if err != nil {
				return nil, err
			}
			v.AddEncoder(v2)
		case core.StreamEncodingFilterNameLZW:
			v2, err := newLZWEncoderFromInlineImage(img, dict2)
			if err != nil {
				return nil, err
			}
			v.AddEncoder(v2)
		case core.StreamEncodingFilterNameASCIIHex:
			v2 := core.NewASCIIHexEncoder()
			v.AddEncoder(v2)
		case core.StreamEncodingFilterNameASCII85, "A85":
			v2 := core.NewASCII85Encoder()
			v.AddEncoder(v2)
		default:
			common.Log.Error("Unsupported filter %s", *name)
			return nil, fmt.Errorf("invalid filter in multi filter array")
		}
	}
	return v, nil
}

func autoConvertRGBToGray(objs []core.PdfObject) (core.PdfObject, error) {
	common.Log.Debug("Auto convert color to grayscale, you can turn off this by setting RelaxedMode to false")
	if len(objs) != 3 {
		return nil, errInvalidNumberOfParams
	}
	floats, err := core.GetNumbersAsFloat(objs)
	if err != nil {
		return nil, err
	}
	n := int(math.Round(floats[0] * 255))
	n2 := int(math.Round(floats[1] * 255))
	n3 := int(math.Round(floats[2] * 255))
	n4 := core.RGBToGrayscale(n, n2, n3)
	return core.MakeFloat(float64(n4) / 255.0), nil
}

func newEncoderFromInlineImage(img *ContentStreamInlineImage) (core.StreamEncoder, error) {
	if img.Filter == nil {
		return core.NewRawEncoder(), nil
	}
	name, ok := img.Filter.(*core.PdfObjectName)
	if !ok {
		arr, ok2 := img.Filter.(*core.PdfObjectArray)
		if !ok2 {
			return nil, fmt.Errorf("filter not a Name or Array object")
		}
		if arr.Len() == 0 {
			return core.NewRawEncoder(), nil
		}
		if arr.Len() != 1 {
			v, err := newMultiEncoderFromInlineImage(img)
			if err != nil {
				common.Log.Error("Failed creating multi encoder: %v", err)
				return nil, err
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Multi enc: %s\n", v)
			}
			return v, nil
		}
		obj := arr.Get(0)
		name, ok2 = obj.(*core.PdfObjectName)
		if !ok2 {
			return nil, fmt.Errorf("filter array member not a Name object")
		}
	}
	switch *name {
	case "AHx", "ASCIIHexDecode":
		return core.NewASCIIHexEncoder(), nil
	case "A85", "ASCII85Decode":
		return core.NewASCII85Encoder(), nil
	case "DCT", "DCTDecode":
		return newDCTEncoderFromInlineImage(img)
	case "Fl", "FlateDecode":
		return newFlateEncoderFromInlineImage(img, nil)
	case "LZW", "LZWDecode":
		return newLZWEncoderFromInlineImage(img, nil)
	case "CCF", "CCITTFaxDecode":
		return core.NewCCITTFaxEncoder(), nil
	case "RL", "RunLengthDecode":
		return core.NewRunLengthEncoder(), nil
	default:
		common.Log.Debug("Unsupported inline image encoding filter name : %s", *name)
		return nil, errors.New("unsupported inline encoding method")
	}
}

func newIndexedColorspaceFromArray(obj core.PdfObject) (model.PdfColorspace, error) {
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		common.Log.Debug("Error: Invalid indexed cs not in array (%#v)", obj)
		return nil, errors.New("type check error")
	}
	if arr.Len() != 4 {
		common.Log.Debug("Error: Invalid cs array, length != 4 (%d)", arr.Len())
		return nil, errors.New("range check error")
	}
	name, ok := arr.Get(0).(*core.PdfObjectName)
	if !ok {
		common.Log.Debug("Error: Invalid cs array first element not a name (array: %#v)", *arr)
		return nil, errors.New("type check error")
	}
	if *name != "I" && *name != "Indexed" {
		common.Log.Debug("Error: Invalid cs array first element != I (got: %v)", *name)
		return nil, errors.New("range check error")
	}
	name, ok = arr.Get(1).(*core.PdfObjectName)
	if !ok {
		common.Log.Debug("Error: Invalid cs array 2nd element not a name (array: %#v)", *arr)
		return nil, errors.New("type check error")
	}
	if *name != "G" && *name != "RGB" && *name != "CMYK" && *name != "DeviceGray" && *name != "DeviceRGB" && *name != "DeviceCMYK" {
		common.Log.Debug("Error: Invalid cs array 2nd element != G/RGB/CMYK (got: %v)", *name)
		return nil, errors.New("range check error")
	}
	s := ""
	switch *name {
	case "G", "DeviceGray":
		s = "DeviceGray"
	case "RGB", "DeviceRGB":
		s = "DeviceRGB"
	case "CMYK", "DeviceCMYK":
		s = "DeviceCMYK"
	}
	arr2 := core.MakeArray(core.MakeName("Indexed"), core.MakeName(s), arr.Get(2), arr.Get(3))
	return model.NewPdfColorspaceFromPdfObject(arr2)
}

func newLZWEncoderFromInlineImage(img *ContentStreamInlineImage, dict *core.PdfObjectDictionary) (*core.LZWEncoder, error) {
	v := core.NewLZWEncoder()
	if dict == nil {
		if img.DecodeParms != nil {
			dict2, ok := core.GetDict(img.DecodeParms)
			if !ok {
				common.Log.Debug("Error: DecodeParms not a dictionary (%T)", img.DecodeParms)
				return nil, fmt.Errorf("invalid DecodeParms")
			}
			dict = dict2
		}
	}
	if dict == nil {
		return v, nil
	}
	obj := dict.Get("EarlyChange")
	if obj != nil {
		num, ok := obj.(*core.PdfObjectInteger)
		if !ok {
			common.Log.Debug("Error: EarlyChange specified but not numeric (%T)", obj)
			return nil, fmt.Errorf("invalid EarlyChange")
		}
		if *num != 0 && *num != 1 {
			return nil, fmt.Errorf("invalid EarlyChange value (not 0 or 1)")
		}
		v.EarlyChange = int(*num)
	} else {
		v.EarlyChange = 1
	}
	obj = dict.Get("Predictor")
	if obj != nil {
		num, ok := obj.(*core.PdfObjectInteger)
		if !ok {
			common.Log.Debug("Error: Predictor specified but not numeric (%T)", obj)
			return nil, fmt.Errorf("invalid Predictor")
		}
		v.Predictor = int(*num)
	}
	obj = dict.Get("BitsPerComponent")
	if obj != nil {
		num, ok := obj.(*core.PdfObjectInteger)
		if !ok {
			common.Log.Debug("ERROR: Invalid BitsPerComponent")
			return nil, fmt.Errorf("invalid BitsPerComponent")
		}
		v.BitsPerComponent = int(*num)
	}
	if v.Predictor > 1 {
		v.Columns = 1
		obj = dict.Get("Columns")
		if obj != nil {
			num, ok := obj.(*core.PdfObjectInteger)
			if !ok {
				return nil, fmt.Errorf("predictor column invalid")
			}
			v.Columns = int(*num)
		}
		v.Colors = 1
		obj = dict.Get("Colors")
		if obj != nil {
			num, ok := obj.(*core.PdfObjectInteger)
			if !ok {
				return nil, fmt.Errorf("predictor colors not an integer")
			}
			v.Colors = int(*num)
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("decode params: %s", dict.String())
	}
	return v, nil
}

func newFlateEncoderFromInlineImage(img *ContentStreamInlineImage, dict *core.PdfObjectDictionary) (*core.FlateEncoder, error) {
	v := core.NewFlateEncoder()
	if img.imageBase != nil {
		v.SetImage(img.imageBase)
	}
	if dict == nil {
		obj := img.DecodeParms
		if obj != nil {
			dict2, ok := core.GetDict(obj)
			if !ok {
				common.Log.Debug("Error: DecodeParms not a dictionary (%T)", obj)
				return nil, fmt.Errorf("invalid DecodeParms")
			}
			dict = dict2
		}
	}
	if dict == nil {
		return v, nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("decode params: %s", dict.String())
	}
	obj := dict.Get("Predictor")
	if obj == nil {
		common.Log.Debug("Error: Predictor missing from DecodeParms - Continue with default (1)")
	} else {
		num, ok := obj.(*core.PdfObjectInteger)
		if !ok {
			common.Log.Debug("Error: Predictor specified but not numeric (%T)", obj)
			return nil, fmt.Errorf("invalid Predictor")
		}
		v.Predictor = int(*num)
	}
	obj = dict.Get("BitsPerComponent")
	if obj != nil {
		num, ok := obj.(*core.PdfObjectInteger)
		if !ok {
			common.Log.Debug("ERROR: Invalid BitsPerComponent")
			return nil, fmt.Errorf("invalid BitsPerComponent")
		}
		v.BitsPerComponent = int(*num)
	}
	if v.Predictor > 1 {
		v.Columns = 1
		obj = dict.Get("Columns")
		if obj != nil {
			num, ok := obj.(*core.PdfObjectInteger)
			if !ok {
				return nil, fmt.Errorf("predictor column invalid")
			}
			v.Columns = int(*num)
		}
		v.Colors = 1
		obj2 := dict.Get("Colors")
		if obj2 != nil {
			num, ok := obj2.(*core.PdfObjectInteger)
			if !ok {
				return nil, fmt.Errorf("predictor colors not an integer")
			}
			v.Colors = int(*num)
		}
	}
	return v, nil
}
