package model

import (
	"errors"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// NewXObjectForm creates a brand new XObject Form. Creates a new underlying PDF object stream primitive.
func NewXObjectForm() *XObjectForm {
	form := &XObjectForm{}
	stream := &core.PdfObjectStream{}
	stream.PdfObjectDictionary = core.MakeDict()
	form.primitive = stream
	return form
}

// NewXObjectFormFromStream builds the Form XObject from a stream object.
// TODO: Should this be exposed? Consider different access points.
func NewXObjectFormFromStream(stream *core.PdfObjectStream) (*XObjectForm, error) {
	form := &XObjectForm{}
	form.primitive = stream
	dict := stream.PdfObjectDictionary
	encoder, err := core.NewEncoderFromStream(stream)
	if err != nil {
		return nil, err
	}
	form.Filter = encoder
	if subtype := dict.Get("Subtype"); subtype != nil {
		name, ok := subtype.(*core.PdfObjectName)
		if !ok {
			return nil, errors.New("type error")
		}
		if *name != "Form" {
			common.Log.Debug("Invalid form subtype")
			return nil, errors.New("invalid form subtype")
		}
	}
	if formType := dict.Get("FormType"); formType != nil {
		form.FormType = formType
	}
	if bBox := dict.Get("BBox"); bBox != nil {
		form.BBox = bBox
	}
	if matrix := dict.Get("Matrix"); matrix != nil {
		form.Matrix = matrix
	}
	if resources := dict.Get("Resources"); resources != nil {
		resources = core.TraceToDirectObject(resources)
		dict, ok := resources.(*core.PdfObjectDictionary)
		if !ok {
			common.Log.Debug("Invalid XObject Form Resources object, pointing to non-dictionary")
			return nil, core.ErrTypeError
		}
		resources, err := NewPdfPageResourcesFromDict(dict)
		if err != nil {
			common.Log.Debug("Failed getting form resources")
			return nil, err
		}
		form.Resources = resources
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Form resources: %#v", form.Resources)
		}
	}
	form.Group = dict.Get("Group")
	form.Ref = dict.Get("Ref")
	form.MetaData = dict.Get("MetaData")
	form.PieceInfo = dict.Get("PieceInfo")
	form.LastModified = dict.Get("LastModified")
	form.StructParent = dict.Get("StructParent")
	form.StructParents = dict.Get("StructParents")
	form.OPI = dict.Get("OPI")
	form.OC = dict.Get("OC")
	form.Name = dict.Get("Name")
	form.Stream = stream.Stream
	return form, nil
}

// ToPdfObject returns a stream object.
func (x *XObjectForm) ToPdfObject() core.PdfObject {
	stream := x.primitive
	dict := stream.PdfObjectDictionary
	if x.Filter != nil {
		dict = x.Filter.MakeStreamDict()
		stream.PdfObjectDictionary = dict
	}
	dict.Set("Type", core.MakeName("XObject"))
	dict.Set("Subtype", core.MakeName("Form"))
	dict.SetIfNotNil("FormType", x.FormType)
	dict.SetIfNotNil("BBox", x.BBox)
	dict.SetIfNotNil("Matrix", x.Matrix)
	if x.Resources != nil {
		dict.SetIfNotNil("Resources", x.Resources.ToPdfObject())
	}
	dict.SetIfNotNil("Group", x.Group)
	dict.SetIfNotNil("Ref", x.Ref)
	dict.SetIfNotNil("MetaData", x.MetaData)
	dict.SetIfNotNil("PieceInfo", x.PieceInfo)
	dict.SetIfNotNil("LastModified", x.LastModified)
	dict.SetIfNotNil("StructParent", x.StructParent)
	dict.SetIfNotNil("StructParents", x.StructParents)
	dict.SetIfNotNil("OPI", x.OPI)
	dict.SetIfNotNil("OC", x.OC)
	dict.SetIfNotNil("Name", x.Name)
	dict.Set("Length", core.MakeInteger(int64(len(x.Stream))))
	stream.Stream = x.Stream
	return stream
}

// XObjectForm (Table 95 in 8.10.2).
type XObjectForm struct {
	Filter        core.StreamEncoder
	FormType      core.PdfObject
	BBox          core.PdfObject
	Matrix        core.PdfObject
	Resources     *PdfPageResources
	Group         core.PdfObject
	Ref           core.PdfObject
	MetaData      core.PdfObject
	PieceInfo     core.PdfObject
	LastModified  core.PdfObject
	StructParent  core.PdfObject
	StructParents core.PdfObject
	OPI           core.PdfObject
	OC            core.PdfObject
	Name          core.PdfObject

	// Stream data.
	Stream    []byte
	primitive *core.PdfObjectStream
}

// SetContentStream updates the content stream with specified encoding.
// If encoding is null, will use the xform.Filter object, or plain Flate when
// that filter cannot encode (such as Flate with a predictor), or Raw encoding
// if not set.
func (x *XObjectForm) SetContentStream(content []byte, encoder core.StreamEncoder) error {
	data := content
	ok := encoder == nil && x.Filter != nil
	if encoder == nil {
		if x.Filter != nil {
			encoder = x.Filter
		} else {
			encoder = core.NewRawEncoder()
		}
	}
	encodeBytes, err := encoder.EncodeBytes(data)
	if err != nil && ok {
		common.Log.Debug("Form filter cannot encode (%v); using plain Flate", err)
		encoder = core.NewFlateEncoder()
		encodeBytes, err = encoder.EncodeBytes(data)
	}
	if err != nil {
		return err
	}
	data = encodeBytes
	x.Stream = data
	x.Filter = encoder
	return nil
}

// GetContentStream returns the XObject Form's content stream.
func (x *XObjectForm) GetContentStream() ([]byte, error) {
	stream, err := core.DecodeStream(x.primitive)
	if err != nil {
		return nil, err
	}
	return stream, nil
}

// GetContainingPdfObject returns the XObject Form's containing object (indirect object).
func (x *XObjectForm) GetContainingPdfObject() core.PdfObject { return x.primitive }
