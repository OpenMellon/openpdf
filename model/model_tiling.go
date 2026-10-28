package model

import (
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// GetContentStream returns the pattern cell's content stream
func (p *PdfTilingPattern) GetContentStream() ([]byte, error) {
	contentStream, _, err := p.GetContentStreamWithEncoder()
	return contentStream, err
}

// SetContentStream sets the pattern cell's content stream.
func (p *PdfTilingPattern) SetContentStream(content []byte, encoder core.StreamEncoder) error {
	stream, ok := p.primitive.(*core.PdfObjectStream)
	if !ok {
		common.Log.Debug("Tiling pattern container not a stream (got %T)", p.primitive)
		return core.ErrTypeError
	}
	if encoder == nil {
		if stream.Get("Filter") != nil {
			encoder = core.NewFlateEncoder()
		} else {
			encoder = core.NewRawEncoder()
		}
	}
	encodeBytes, err := encoder.EncodeBytes(content)
	if err != nil {
		return err
	}
	dict := stream.PdfObjectDictionary
	dict.Remove("Filter")
	dict.Remove("DecodeParms")
	streamDict := encoder.MakeStreamDict()
	dict.Merge(streamDict)
	dict.Set("Length", core.MakeInteger(int64(len(encodeBytes))))
	stream.Stream = encodeBytes
	return nil
}

func newPdfTilingPatternFromDict(dict *core.PdfObjectDictionary) (*PdfTilingPattern, error) {
	tilingPattern := &PdfTilingPattern{}
	paintType := dict.Get("PaintType")
	if paintType == nil {
		common.Log.Debug("PaintType missing")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok := paintType.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("PaintType not an integer (got %T)", paintType)
		return nil, core.ErrTypeError
	}
	tilingPattern.PaintType = num
	paintType = dict.Get("TilingType")
	if paintType == nil {
		common.Log.Debug("TilingType missing")
		return nil, ErrRequiredAttributeMissing
	}
	num2, ok := paintType.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("TilingType not an integer (got %T)", paintType)
		return nil, core.ErrTypeError
	}
	tilingPattern.TilingType = num2
	paintType = dict.Get("BBox")
	if paintType == nil {
		common.Log.Debug("BBox missing")
		return nil, ErrRequiredAttributeMissing
	}
	paintType = core.TraceToDirectObject(paintType)
	arr, ok := paintType.(*core.PdfObjectArray)
	if !ok {
		common.Log.Debug("BBox should be specified by an array (got %T)", paintType)
		return nil, core.ErrTypeError
	}
	rect, err := NewPdfRectangle(*arr)
	if err != nil {
		common.Log.Debug("BBox error: %v", err)
		return nil, err
	}
	tilingPattern.BBox = rect
	paintType = dict.Get("XStep")
	if paintType == nil {
		common.Log.Debug("XStep missing")
		return nil, ErrRequiredAttributeMissing
	}
	f, err := core.GetNumberAsFloat(paintType)
	if err != nil {
		common.Log.Debug("Error getting XStep as float: %v", f)
		return nil, err
	}
	tilingPattern.XStep = core.MakeFloat(f)
	paintType = dict.Get("YStep")
	if paintType == nil {
		common.Log.Debug("YStep missing")
		return nil, ErrRequiredAttributeMissing
	}
	v, err := core.GetNumberAsFloat(paintType)
	if err != nil {
		common.Log.Debug("Error getting YStep as float: %v", v)
		return nil, err
	}
	tilingPattern.YStep = core.MakeFloat(v)
	paintType = dict.Get("Resources")
	if paintType == nil {
		common.Log.Debug("Resources missing")
		return nil, ErrRequiredAttributeMissing
	}
	dict2, ok := core.TraceToDirectObject(paintType).(*core.PdfObjectDictionary)
	if !ok {
		return nil, fmt.Errorf("invalid resource dictionary (%T)", paintType)
	}
	resources, err := NewPdfPageResourcesFromDict(dict2)
	if err != nil {
		return nil, err
	}
	tilingPattern.Resources = resources
	if matrix := dict.Get("Matrix"); matrix != nil {
		arr, ok := core.TraceToDirectObject(matrix).(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Matrix not an array (got %T)", matrix)
			return nil, core.ErrTypeError
		}
		tilingPattern.Matrix = arr
	}
	return tilingPattern, nil
}

// IsColored specifies if the pattern is colored.
func (p *PdfTilingPattern) IsColored() bool {
	if p.PaintType != nil && *p.PaintType == 1 {
		return true
	}
	return false
}

// GetContentStreamWithEncoder returns the pattern cell's content stream and its encoder
func (p *PdfTilingPattern) GetContentStreamWithEncoder() ([]byte, core.StreamEncoder, error) {
	stream, ok := p.primitive.(*core.PdfObjectStream)
	if !ok {
		common.Log.Debug("Tiling pattern container not a stream (got %T)", p.primitive)
		return nil, nil, core.ErrTypeError
	}
	data, err := core.DecodeStream(stream)
	if err != nil {
		common.Log.Debug("Failed decoding stream, err: %v", err)
		return nil, nil, err
	}
	encoder, err := core.NewEncoderFromStream(stream)
	if err != nil {
		common.Log.Debug("Failed finding decoding encoder: %v", err)
		return nil, nil, err
	}
	return data, encoder, nil
}

// ToPdfObject returns the PDF representation of the tiling pattern.
func (p *PdfTilingPattern) ToPdfObject() core.PdfObject {
	p.PdfPattern.ToPdfObject()
	dict := p.getDict()
	if p.PaintType != nil {
		dict.Set("PaintType", p.PaintType)
	}
	if p.TilingType != nil {
		dict.Set("TilingType", p.TilingType)
	}
	if p.BBox != nil {
		dict.Set("BBox", p.BBox.ToPdfObject())
	}
	if p.XStep != nil {
		dict.Set("XStep", p.XStep)
	}
	if p.YStep != nil {
		dict.Set("YStep", p.YStep)
	}
	if p.Resources != nil {
		dict.Set("Resources", p.Resources.ToPdfObject())
	}
	if p.Matrix != nil {
		dict.Set("Matrix", p.Matrix)
	}
	return p.primitive
}

// PdfTilingPattern is a Tiling pattern that consists of repetitions of a pattern cell with defined intervals.
// It is a type 1 pattern. (PatternType = 1).
// A tiling pattern is represented by a stream object, where the stream content is
// a content stream that describes the pattern cell.
type PdfTilingPattern struct {
	*PdfPattern
	PaintType  *core.PdfObjectInteger
	TilingType *core.PdfObjectInteger
	BBox       *PdfRectangle
	XStep      *core.PdfObjectFloat
	YStep      *core.PdfObjectFloat
	Resources  *PdfPageResources
	Matrix     *core.PdfObjectArray
}
