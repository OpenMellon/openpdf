package core

import (
	"errors"
	"fmt"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/ccittfax"
	"github.com/OpenMellon/openpdf/internal/imageutil"
)

// CCITTFaxEncoder implements Group3 and Group4 facsimile (fax) encoder/decoder.
type CCITTFaxEncoder struct {
	K                      int
	EndOfLine              bool
	EncodedByteAlign       bool
	Columns                int
	Rows                   int
	EndOfBlock             bool
	BlackIs1               bool
	DamagedRowsBeforeError int
	limits                 decodeLimits
}

func newCCITTFaxEncoderFromStream(streamObj *PdfObjectStream, decodeParams *PdfObjectDictionary) (*CCITTFaxEncoder, error) {
	ccittFaxEncoder := NewCCITTFaxEncoder()
	pdfObjectDictionary := streamObj.PdfObjectDictionary
	if pdfObjectDictionary == nil {
		return ccittFaxEncoder, nil
	}
	if decodeParams == nil {
		toDirectObject := TraceToDirectObject(pdfObjectDictionary.Get("DecodeParms"))
		if toDirectObject != nil {
			switch v := toDirectObject.(type) {
			case *PdfObjectDictionary:
				decodeParams = v
			case *PdfObjectArray:
				if v.Len() == 1 {
					if dict, ok := GetDict(v.Get(0)); ok {
						decodeParams = dict
					}
				}
			default:
				common.Log.Error("DecodeParms not a dictionary %#v", toDirectObject)
				return nil, errors.New("invalid DecodeParms")
			}
		}
		if decodeParams == nil {
			common.Log.Error("DecodeParms not specified %#v", toDirectObject)
			return nil, errors.New("invalid DecodeParms")
		}
	}
	if kN, err := GetNumberAsInt64(decodeParams.Get("K")); err == nil {
		ccittFaxEncoder.K = int(kN)
	}
	if columns, err := GetNumberAsInt64(decodeParams.Get("Columns")); err == nil {
		ccittFaxEncoder.Columns = int(columns)
	} else {
		ccittFaxEncoder.Columns = 1728
	}
	if blackIs1, err := GetNumberAsInt64(decodeParams.Get("BlackIs1")); err == nil {
		ccittFaxEncoder.BlackIs1 = blackIs1 > 0
	} else {
		if blackIs12, ok := GetBoolVal(decodeParams.Get("BlackIs1")); ok {
			ccittFaxEncoder.BlackIs1 = blackIs12
		} else {
			if decode, hasDecode := GetArray(decodeParams.Get("Decode")); hasDecode {
				nums, err2 := decode.ToIntegerArray()
				if err2 == nil {
					ccittFaxEncoder.BlackIs1 = nums[0] == 1 && nums[1] == 0
				}
			}
		}
	}
	if encodedByteAlign, err := GetNumberAsInt64(decodeParams.Get("EncodedByteAlign")); err == nil {
		ccittFaxEncoder.EncodedByteAlign = encodedByteAlign > 0
	} else {
		if byteAlign, ok := GetBoolVal(decodeParams.Get("EncodedByteAlign")); ok {
			ccittFaxEncoder.EncodedByteAlign = byteAlign
		}
	}
	if endOfLine, err := GetNumberAsInt64(decodeParams.Get("EndOfLine")); err == nil {
		ccittFaxEncoder.EndOfLine = endOfLine > 0
	} else {
		if endOfLine2, ok := GetBoolVal(decodeParams.Get("EndOfLine")); ok {
			ccittFaxEncoder.EndOfLine = endOfLine2
		}
	}
	if rows, err := GetNumberAsInt64(decodeParams.Get("Rows")); err == nil {
		ccittFaxEncoder.Rows = int(rows)
	}
	ccittFaxEncoder.EndOfBlock = true
	if endOfBlock, err := GetNumberAsInt64(decodeParams.Get("EndOfBlock")); err == nil {
		ccittFaxEncoder.EndOfBlock = endOfBlock > 0
	} else {
		if endOfBlock2, ok := GetBoolVal(decodeParams.Get("EndOfBlock")); ok {
			ccittFaxEncoder.EndOfBlock = endOfBlock2
		}
	}
	if damagedRowsBeforeError, err := GetNumberAsInt64(decodeParams.Get("DamagedRowsBeforeError")); err != nil {
		ccittFaxEncoder.DamagedRowsBeforeError = int(damagedRowsBeforeError)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("decode params: %s", decodeParams.String())
	}
	return ccittFaxEncoder, nil
}

// GetFilterName returns the name of the encoding filter.
func (ccitt *CCITTFaxEncoder) GetFilterName() string { return StreamEncodingFilterNameCCITTFax }

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (ccitt *CCITTFaxEncoder) MakeDecodeParams() PdfObject {
	dict := MakeDict()
	dict.Set("K", MakeInteger(int64(ccitt.K)))
	dict.Set("Columns", MakeInteger(int64(ccitt.Columns)))
	if ccitt.BlackIs1 {
		dict.Set("BlackIs1", MakeBool(ccitt.BlackIs1))
	}
	if ccitt.EncodedByteAlign {
		dict.Set("EncodedByteAlign", MakeBool(ccitt.EncodedByteAlign))
	}
	if ccitt.EndOfLine && ccitt.K >= 0 {
		dict.Set("EndOfLine", MakeBool(ccitt.EndOfLine))
	}
	if ccitt.Rows != 0 && !ccitt.EndOfBlock {
		dict.Set("Rows", MakeInteger(int64(ccitt.Rows)))
	}
	if !ccitt.EndOfBlock {
		dict.Set("EndOfBlock", MakeBool(ccitt.EndOfBlock))
	}
	if ccitt.DamagedRowsBeforeError != 0 {
		dict.Set("DamagedRowsBeforeError", MakeInteger(int64(ccitt.DamagedRowsBeforeError)))
	}
	return dict
}

// NewCCITTFaxEncoder makes a new CCITTFax encoder.
func NewCCITTFaxEncoder() *CCITTFaxEncoder { return &CCITTFaxEncoder{Columns: 1728, EndOfBlock: true} }

// UpdateParams updates the parameter values of the encoder.
func (ccitt *CCITTFaxEncoder) UpdateParams(params *PdfObjectDictionary) {
	if kN, err := GetNumberAsInt64(params.Get("K")); err == nil {
		ccitt.K = int(kN)
	}
	if columns, err := GetNumberAsInt64(params.Get("Columns")); err == nil {
		ccitt.Columns = int(columns)
	} else if columns, err = GetNumberAsInt64(params.Get("Width")); err == nil {
		ccitt.Columns = int(columns)
	}
	if blackIs1, err := GetNumberAsInt64(params.Get("BlackIs1")); err == nil {
		ccitt.BlackIs1 = blackIs1 > 0
	} else {
		if blackIs12, ok := GetBoolVal(params.Get("BlackIs1")); ok {
			ccitt.BlackIs1 = blackIs12
		} else {
			if decode, hasDecode := GetArray(params.Get("Decode")); hasDecode {
				nums, err2 := decode.ToIntegerArray()
				if err2 == nil {
					ccitt.BlackIs1 = nums[0] == 1 && nums[1] == 0
				}
			}
		}
	}
	if encodedByteAlign, err := GetNumberAsInt64(params.Get("EncodedByteAlign")); err == nil {
		ccitt.EncodedByteAlign = encodedByteAlign > 0
	} else {
		if byteAlign, ok := GetBoolVal(params.Get("EncodedByteAlign")); ok {
			ccitt.EncodedByteAlign = byteAlign
		}
	}
	if endOfLine, err := GetNumberAsInt64(params.Get("EndOfLine")); err == nil {
		ccitt.EndOfLine = endOfLine > 0
	} else {
		if endOfLine2, ok := GetBoolVal(params.Get("EndOfLine")); ok {
			ccitt.EndOfLine = endOfLine2
		}
	}
	if rows, err := GetNumberAsInt64(params.Get("Rows")); err == nil {
		ccitt.Rows = int(rows)
	} else if rows, err = GetNumberAsInt64(params.Get("Height")); err == nil {
		ccitt.Rows = int(rows)
	}
	if endOfBlock, err := GetNumberAsInt64(params.Get("EndOfBlock")); err == nil {
		ccitt.EndOfBlock = endOfBlock > 0
	} else {
		if endOfBlock2, ok := GetBoolVal(params.Get("EndOfBlock")); ok {
			ccitt.EndOfBlock = endOfBlock2
		}
	}
	if damagedRowsBeforeError, err := GetNumberAsInt64(params.Get("DamagedRowsBeforeError")); err != nil {
		ccitt.DamagedRowsBeforeError = int(damagedRowsBeforeError)
	}
}

// DecodeStream decodes the stream containing CCITTFax encoded image data.
func (ccitt *CCITTFaxEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return ccitt.DecodeBytes(streamObj.Stream)
}

// EncodeBytes encodes the image data using either Group3 or Group4 CCITT facsimile (fax) encoding.
// `data` is expected to be 1 color component, 1 bit per component. It is also valid to provide 8 BPC, 1 CC image like
// a standard go image Gray data.
func (ccitt *CCITTFaxEncoder) EncodeBytes(data []byte) ([]byte, error) {
	var gray imageutil.Gray
	switch len(data) {
	case ccitt.Rows * ccitt.Columns:
		image, err := imageutil.NewImage(ccitt.Columns, ccitt.Rows, 8, 1, data, nil, nil)
		if err != nil {
			return nil, err
		}
		gray = image.(imageutil.Gray)
	case (ccitt.Columns * ccitt.Rows) + 7>>3:
		image, err := imageutil.NewImage(ccitt.Columns, ccitt.Rows, 1, 1, data, nil, nil)
		if err != nil {
			return nil, err
		}
		monochrome := image.(*imageutil.Monochrome)
		if err = monochrome.AddPadding(); err != nil {
			return nil, err
		}
		gray = monochrome
	default:
		if len(data) < imageutil.BytesPerLine(ccitt.Columns, 1, 1)*ccitt.Rows {
			return nil, errors.New("provided invalid input data")
		}
		image, err := imageutil.NewImage(ccitt.Columns, ccitt.Rows, 1, 1, data, nil, nil)
		if err != nil {
			return nil, err
		}
		monochrome := image.(*imageutil.Monochrome)
		gray = monochrome
	}
	datas := make([][]byte, ccitt.Rows)
	for i := 0; i < ccitt.Rows; i++ {
		row := make([]byte, ccitt.Columns)
		for j := 0; j < ccitt.Columns; j++ {
			px := gray.GrayAt(j, i)
			row[j] = px.Y >> 7
		}
		datas[i] = row
	}
	encoder := &ccittfax.Encoder{K: ccitt.K, Columns: ccitt.Columns, EndOfLine: ccitt.EndOfLine, EndOfBlock: ccitt.EndOfBlock, BlackIs1: ccitt.BlackIs1, DamagedRowsBeforeError: ccitt.DamagedRowsBeforeError, Rows: ccitt.Rows, EncodedByteAlign: ccitt.EncodedByteAlign}
	return encoder.Encode(datas), nil
}

// DecodeBytes decodes the CCITTFax encoded image data.
func (ccitt *CCITTFaxEncoder) DecodeBytes(encoded []byte) ([]byte, error) {
	if ccitt.Columns < 1 || ccitt.Columns > maxCCITTColumns {
		return nil, fmt.Errorf("%w: CCITTFax Columns %d outside [1, %d]", ErrRangeError, ccitt.Columns, maxCCITTColumns)
	}
	if ccitt.Rows < 0 {
		return nil, fmt.Errorf("%w: CCITTFax Rows %d is negative", ErrRangeError, ccitt.Rows)
	}
	decoder, err := ccittfax.NewDecoder(encoded, ccittfax.DecodeOptions{Columns: ccitt.Columns, Rows: ccitt.Rows, K: ccitt.K, EncodedByteAligned: ccitt.EncodedByteAlign, BlackIsOne: ccitt.BlackIs1, EndOfBlock: ccitt.EndOfBlock, EndOfLine: ccitt.EndOfLine, DamagedRowsBeforeError: ccitt.DamagedRowsBeforeError})
	if err != nil {
		return nil, err
	}
	all, err := ccitt.limits.readAll(StreamEncodingFilterNameCCITTFax, decoder)
	if err != nil {
		ReleaseBytes(all)
		return nil, err
	}
	ccitt.limits.charge(int64(len(all)))
	return all, nil
}

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
func (ccitt *CCITTFaxEncoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName(ccitt.GetFilterName()))
	dict.SetIfNotNil("DecodeParms", ccitt.MakeDecodeParams())
	return dict
}

func (ccitt *CCITTFaxEncoder) setDecodeLimits(limits decodeLimits) { ccitt.limits = limits }
