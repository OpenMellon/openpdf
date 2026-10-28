package model

import (
	"errors"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// SetSubtype sets the Subtype S for given PdfOutputIntent.
func (o *PdfOutputIntent) SetSubtype(subtype PdfOutputIntentType) error {
	if !subtype.IsValid() {
		return errors.New("provided invalid OutputIntent SubType")
	}
	o.S = subtype
	return nil
}

// PdfOutputIntent provides a means for matching the color characteristics of a PDF document with
// those of a target output device.
// Multiple PdfOutputIntents allows the production process to be customized to the expected workflow and the specific
// tools available.
type PdfOutputIntent struct {
	// Type is an optional PDF object that this dictionary describes.
	// If present, must be OutputIntent for an output intent dictionary.
	Type string

	// S defines the OutputIntent subtype which should match the standard used in given document i.e:
	// for PDF/X use PdfOutputIntentTypeX.
	S PdfOutputIntentType

	// OutputCondition is an optional field that is identifying the intended output device or production condition in
	// human-readable form. This is preferred method of defining such a string for presentation to the user.
	OutputCondition string

	// OutputConditionIdentifier is a required field identifying the intended output device or production condition in
	// human or machine-readable form. If human-readable, this string may be used
	// in lieu of an OutputCondition for presentation to the user.
	// A typical value for this entry would be the name of a production condition  maintained
	// in  an  industry-standard registry such  as the ICC Characterization Data Registry
	// If the intended production condition is not a recognized standard, the value Custom is recommended for this entry.
	// the DestOutputProfile entry defines the ICC profile, and the Info entry is used for further
	// human-readable identification.
	OutputConditionIdentifier string

	// RegistryName is an optional string field (conventionally URI) identifying the registry in which the condition
	// designated by OutputConditionIdentifier is defined.
	RegistryName string

	// Info is a required field if OutputConditionIdentifier does not specify a standard production condition.
	// A human-readable text string containing additional information  or comments about intended
	// target device or production condition.
	Info string

	// DestOutputProfile is required if OutputConditionIdentifier does not specify a standard production condition.
	// It is an ICC profile stream defining the transformation from the PDF document's source colors to output device colorants.
	DestOutputProfile []byte

	// ColorComponents is the number of color components supported by given output profile.
	ColorComponents int
	primitive       *core.PdfObjectDictionary
}

// IsValid checks if the given pdf output intent type is valid.
func (t PdfOutputIntentType) IsValid() bool {
	return t >= PdfOutputIntentTypeA1 && t <= PdfOutputIntentTypeX
}

// NewPdfOutputIntentFromPdfObject creates a new PdfOutputIntent from the input core.PdfObject.
func NewPdfOutputIntentFromPdfObject(object core.PdfObject) (*PdfOutputIntent, error) {
	outputIntent := &PdfOutputIntent{}
	if err := outputIntent.ParsePdfObject(object); err != nil {
		return nil, err
	}
	return outputIntent, nil
}

// ToPdfObject implements model.PdfModel interface.
func (o *PdfOutputIntent) ToPdfObject() core.PdfObject {
	if o.primitive == nil {
		o.primitive = core.MakeDict()
	}
	dict := o.primitive
	if o.Type != "" {
		dict.Set("Type", core.MakeName(o.Type))
	}
	dict.Set("S", core.MakeName(o.S.String()))
	if o.OutputCondition != "" {
		dict.Set("OutputCondition", core.MakeString(o.OutputCondition))
	}
	dict.Set("OutputConditionIdentifier", core.MakeString(o.OutputConditionIdentifier))
	dict.Set("RegistryName", core.MakeString(o.RegistryName))
	if o.Info != "" {
		dict.Set("Info", core.MakeString(o.Info))
	}
	if len(o.DestOutputProfile) != 0 {
		stream, err := core.MakeStream(o.DestOutputProfile, core.NewFlateEncoder())
		if err != nil {
			common.Log.Error("MakeStream DestOutputProfile failed: %v", err)
		}
		stream.PdfObjectDictionary.Set("N", core.MakeInteger(int64(o.ColorComponents)))
		floats := make([]float64, o.ColorComponents*2)
		for i := 0; i < o.ColorComponents*2; i++ {
			f := 0.0
			if i%2 != 0 {
				f = 1.0
			}
			floats[i] = f
		}
		stream.PdfObjectDictionary.Set("Range", core.MakeArrayFromFloats(floats))
		dict.Set("DestOutputProfile", stream)
	}
	return dict
}

// String returns string value of output intent for given type
// ISO_19005-2 6.2.3: GTS_PDFA1 value should be used for PDF/A-1, A-2 and A-3 at least
func (t PdfOutputIntentType) String() string {
	switch t {
	case PdfOutputIntentTypeA1:
		return "GTS_PDFA1"
	case PdfOutputIntentTypeA2:
		return "GTS_PDFA1"
	case PdfOutputIntentTypeA3:
		return "GTS_PDFA1"
	case PdfOutputIntentTypeA4:
		return "GTS_PDFA1"
	case PdfOutputIntentTypeX:
		return "GTS_PDFX"
	default:
		return "UNDEFINED"
	}
}

// ParsePdfObject parses input pdf object into given output intent.
func (o *PdfOutputIntent) ParsePdfObject(object core.PdfObject) error {
	dict, ok := core.GetDict(object)
	if !ok {
		common.Log.Error("Unknown type: %T for output intent", object)
		return errors.New("unknown pdf object type for output intent")
	}
	o.primitive = dict
	o.Type, _ = dict.GetString("Type")
	s, ok := dict.GetString("S")
	if ok {
		switch s {
		case "GTS_PDFA1":
			o.S = PdfOutputIntentTypeA1
		case "GTS_PDFA2":
			o.S = PdfOutputIntentTypeA2
		case "GTS_PDFA3":
			o.S = PdfOutputIntentTypeA3
		case "GTS_PDFA4":
			o.S = PdfOutputIntentTypeA4
		case "GTS_PDFX":
			o.S = PdfOutputIntentTypeX
		}
	}
	o.OutputCondition, _ = dict.GetString("OutputCondition")
	o.OutputConditionIdentifier, _ = dict.GetString("OutputConditionIdentifier")
	o.RegistryName, _ = dict.GetString("RegistryName")
	o.Info, _ = dict.GetString("Info")
	if destOutputProfile, ok := core.GetStream(dict.Get("DestOutputProfile")); ok {
		o.ColorComponents, _ = core.GetIntVal(destOutputProfile.Get("N"))
		stream, err := core.DecodeStream(destOutputProfile)
		if err != nil {
			return err
		}
		o.DestOutputProfile = stream
	}
	return nil
}

// PdfOutputIntentType is the subtype of the given PdfOutputIntent.
type PdfOutputIntentType int

// GetContainingPdfObject implements model.PdfModel interface.
func (o *PdfOutputIntent) GetContainingPdfObject() core.PdfObject { return o.primitive }
