package model

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	_ "image/gif"
	"math"
	"strings"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/license"
	"github.com/OpenMellon/openpdf/internal/transform"
)

var predefinedEncodingNames = map[string]struct{}{"WinAnsiEncoding": {}, "MacRomanEncoding": {}, "MacExpertEncoding": {}, "StandardEncoding": {}}

func randomIDWithPrefix(s string) (string, error) {
	var buf bytes.Buffer
	buf.WriteString(s)
	data := make([]byte, 8+16)
	unixNano := time.Now().UTC().UnixNano()
	binary.BigEndian.PutUint64(data, uint64(unixNano))
	_, err := rand.Read(data[8:])
	if err != nil {
		return "", err
	}
	buf.WriteString(hex.EncodeToString(data))
	return buf.String(), nil
}

// Height returns the height of `rect`.
func (r *PdfRectangle) Height() float64 { return math.Abs(r.Ury - r.Lly) }

// Quadding is the text alignment value derived from a field's /Q entry.
type Quadding int

// PrintScaling represents the page scaling option that shall be selected
// when a print dialog is displayed for this document.
type PrintScaling string

const (
	RC4_128bit = EncryptionAlgorithm(iota)
	AES_128bit
	AES_256bit
)

// GetContainingPdfObject returns the container of the pattern object (indirect object).
func (p *PdfPattern) GetContainingPdfObject() core.PdfObject { return p.primitive }

// PdfRectangle is a definition of a rectangle.
type PdfRectangle struct {
	Llx float64
	Lly float64
	Urx float64
	Ury float64
}

func (p *PdfPattern) getDict() *core.PdfObjectDictionary {
	if indirect, ok := p.primitive.(*core.PdfIndirectObject); ok {
		dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
		if !ok {
			return nil
		}
		return dict
	} else if stream, ok := p.primitive.(*core.PdfObjectStream); ok {
		return stream.PdfObjectDictionary
	} else {
		common.Log.Debug("Trying to access pattern dictionary of invalid object type (%T)", p.primitive)
		return nil
	}
}

// IsShading specifies if the pattern is a shading pattern.
func (p *PdfPattern) IsShading() bool { return p.PatternType == 2 }

// ReaderToWriterOpts options used to generate a PdfWriter.
type ReaderToWriterOpts struct {
	SkipAcroForm          bool
	SkipInfo              bool
	SkipNameDictionary    bool
	SkipNamedDests        bool
	SkipOCProperties      bool
	SkipOutlines          bool
	SkipPageLabels        bool
	SkipRotation          bool
	SkipMetadata          bool
	SkipMarkInfo          bool
	SkipViewerPreferences bool
	SkipLanguage          bool
	SkipPieceInfo         bool

	// SkipObjectCopy bypasses the writer's defensive deep copy of the source
	// object graph (see PdfWriter.SetSkipObjectCopy). It reduces memory use and
	// write time for large documents, but consumes the source: the reader and
	// its objects MUST NOT be used after the resulting writer's Write call.
	SkipObjectCopy      bool
	PageProcessCallback PageProcessCallback
}

// IsTiling specifies if the pattern is a tiling pattern.
func (p *PdfPattern) IsTiling() bool { return p.PatternType == 1 }

// Width returns the width of `rect`.
func (r *PdfRectangle) Width() float64 { return math.Abs(r.Urx - r.Llx) }

func prependSubsetTag(s, s2 string) string {
	if strings.Contains(s, "+") {
		split := strings.Split(s, "+")
		if len(split) == 2 {
			s = split[1]
		}
	}
	return s2 + "+" + s
}

// Permissions specify a permissions dictionary (PDF 1.5).
// (Section 12.8.4, Table 258 - Entries in a permissions dictionary p. 477 in PDF32000_2008).
type Permissions struct {
	DocMDP    *PdfSignature
	primitive *core.PdfObjectDictionary
}

func newPdfPatternFromPdfObject(obj core.PdfObject) (*PdfPattern, error) {
	pattern := &PdfPattern{}
	var dict *core.PdfObjectDictionary
	if indirect, ok := core.GetIndirect(obj); ok {
		pattern.primitive = indirect
		dict2, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
		if !ok {
			common.Log.Debug("Pattern indirect object not containing dictionary (got %T)", indirect.PdfObject)
			return nil, core.ErrTypeError
		}
		dict = dict2
	} else if stream, ok := core.GetStream(obj); ok {
		pattern.primitive = stream
		dict = stream.PdfObjectDictionary
	} else {
		common.Log.Debug("Pattern not an indirect object or stream. %T", obj)
		return nil, core.ErrTypeError
	}
	patternType := dict.Get("PatternType")
	if patternType == nil {
		common.Log.Debug("Pdf Pattern not containing PatternType")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok := patternType.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("Pattern type not an integer (got %T)", patternType)
		return nil, core.ErrTypeError
	}
	if *num != 1 && *num != 2 {
		common.Log.Debug("Pattern type != 1/2 (got %d)", *num)
		return nil, core.ErrRangeError
	}
	pattern.PatternType = int64(*num)
	switch *num {
	case 1:
		tilingPattern, err := newPdfTilingPatternFromDict(dict)
		if err != nil {
			return nil, err
		}
		tilingPattern.PdfPattern = pattern
		pattern.context = tilingPattern
		return pattern, nil
	case 2:
		shadingPattern, err := newPdfShadingPatternFromDict(dict)
		if err != nil {
			return nil, err
		}
		shadingPattern.PdfPattern = pattern
		pattern.context = shadingPattern
		return pattern, nil
	}
	return nil, errors.New("unknown pattern")
}

const (
	QuaddingLeft     Quadding = 0
	QuaddingCentered Quadding = 1
	QuaddingRight    Quadding = 2
)

// GetAsShadingPattern returns a shading pattern. Check with IsShading() prior to using this.
func (p *PdfPattern) GetAsShadingPattern() *PdfShadingPattern {
	return p.context.(*PdfShadingPattern)
}

// SetPdfProducer sets the Producer attribute of the output PDF.
func SetPdfProducer(producer string) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	pdfProducer = producer
}

// A PdfPattern can represent a Pattern, either a tiling pattern or a shading pattern.
// Note that all patterns shall be treated as colours; a Pattern colour space shall be established with the CS or cs
// operator just like other colour spaces, and a particular pattern shall be installed as the current colour with the
// SCN or scn operator.
type PdfPattern struct {
	// Type: Pattern
	PatternType int64
	context     PdfModel
	primitive   core.PdfObject
}

// GetContext returns a reference to the subpattern entry: either PdfTilingPattern or PdfShadingPattern.
func (p *PdfPattern) GetContext() PdfModel { return p.context }

// ToPdfObject converts rectangle to a PDF object.
func (r *PdfRectangle) ToPdfObject() core.PdfObject {
	return core.MakeArray(core.MakeFloat(r.Llx), core.MakeFloat(r.Lly), core.MakeFloat(r.Urx), core.MakeFloat(r.Ury))
}

// GetContainingPdfObject implements interface PdfModel.
func (p *Permissions) GetContainingPdfObject() core.PdfObject { return p.primitive }

// GetAsTilingPattern returns a tiling pattern. Check with IsTiling() prior to using this.
func (p *PdfPattern) GetAsTilingPattern() *PdfTilingPattern {
	return p.context.(*PdfTilingPattern)
}

// NewPermissions returns a new permissions object.
func NewPermissions(docMdp *PdfSignature) *Permissions {
	perms := Permissions{}
	perms.DocMDP = docMdp
	dict := core.MakeDict()
	dict.Set("DocMDP", docMdp.ToPdfObject())
	perms.primitive = dict
	return &perms
}

// Transform rectangle with the supplied matrix.
func (r *PdfRectangle) Transform(transformMatrix transform.Matrix) {
	r.Llx, r.Lly = transformMatrix.Transform(r.Llx, r.Lly)
	r.Urx, r.Ury = transformMatrix.Transform(r.Urx, r.Ury)
	r.Normalize()
}

// Normalize swaps (Llx,Urx) if Urx < Llx, and (Lly,Ury) if Ury < Lly.
func (r *PdfRectangle) Normalize() {
	if r.Llx > r.Urx {
		r.Llx, r.Urx = r.Urx, r.Llx
	}
	if r.Lly > r.Ury {
		r.Lly, r.Ury = r.Ury, r.Lly
	}
}

// ToPdfObject returns the PDF representation of the pattern.
func (p *PdfPattern) ToPdfObject() core.PdfObject {
	dict := p.getDict()
	dict.Set("Type", core.MakeName("Pattern"))
	dict.Set("PatternType", core.MakeInteger(p.PatternType))
	return p.primitive
}

// NewPdfRectangle creates a PDF rectangle object based on an input array of 4 integers.
// Defining the lower left (LL) and upper right (UR) corners with
// floating point numbers.
func NewPdfRectangle(arr core.PdfObjectArray) (*PdfRectangle, error) {
	rect := PdfRectangle{}
	if arr.Len() != 4 {
		return nil, errors.New("invalid rectangle array, len != 4")
	}
	var err error
	rect.Llx, err = core.GetNumberAsFloat(arr.Get(0))
	if err != nil {
		return nil, err
	}
	rect.Lly, err = core.GetNumberAsFloat(arr.Get(1))
	if err != nil {
		return nil, err
	}
	rect.Urx, err = core.GetNumberAsFloat(arr.Get(2))
	if err != nil {
		return nil, err
	}
	rect.Ury, err = core.GetNumberAsFloat(arr.Get(3))
	if err != nil {
		return nil, err
	}
	return &rect, nil
}

// ToPdfObject implements interface PdfModel.
func (p *Permissions) ToPdfObject() core.PdfObject { return p.primitive }

type preserializedObject struct {
	objNum int64
	genNum int64
	obj    core.PdfObject
	data   []byte
	offset int64
}

// SetContext sets the sub pattern (context).  Either PdfTilingPattern or PdfShadingPattern.
func (p *PdfPattern) SetContext(ctx PdfModel) { p.context = ctx }

func getPdfProducer() string {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	licenseKey := license.GetLicenseKey()
	if len(pdfProducer) > 0 && (licenseKey.IsLicensed() || licenseCheckDisabled) {
		return pdfProducer
	}
	return fmt.Sprintf("OpenPDF v%s (%s) - https://github.com/OpenMellon/openpdf", libraryVersion(), licenseKey.TypeToString())
}

// ToWriter creates a new writer from the current reader, based on the specified options.
// If no options are provided, all reader properties are copied to the writer.
func (r *PdfReader) ToWriter(opts *ReaderToWriterOpts) (*PdfWriter, error) {
	writer := NewPdfWriter()
	writer.SetFileName(r.fileName)
	if opts == nil {
		opts = &ReaderToWriterOpts{}
	}
	writer.SetSkipObjectCopy(opts.SkipObjectCopy)
	numPages, err := r.GetNumPages()
	if err != nil {
		common.Log.Debug("ERROR: %v", err)
		return nil, err
	}
	for i := 1; i <= numPages; i++ {
		page, err := r.GetPage(i)
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
			return nil, err
		}
		if opts.PageProcessCallback != nil {
			err = opts.PageProcessCallback(i, page)
			if err != nil {
				common.Log.Debug("ERROR: %v", err)
				return nil, err
			}
		}
		err = writer.AddPage(page)
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
			return nil, err
		}
	}
	writer.version = r.PdfVersion()
	if !opts.SkipInfo {
		pdfInfo, err := r.GetPdfInfo()
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
		} else {
			writer.infoObj.PdfObject = pdfInfo.ToPdfObject()
		}
	}
	if !opts.SkipMetadata {
		if metadata := r.catalog.Get("Metadata"); metadata != nil {
			if err := writer.SetCatalogMetadata(metadata); err != nil {
				return nil, err
			}
		}
	}
	if !opts.SkipMarkInfo {
		if catalogMarkInfo, ok := r.GetCatalogMarkInfo(); ok {
			if err := writer.SetCatalogMarkInfo(catalogMarkInfo); err != nil {
				return nil, err
			}
		}
	}
	if !opts.SkipAcroForm {
		err := writer.SetForms(r.AcroForm)
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
			return nil, err
		}
	}
	if !opts.SkipViewerPreferences {
		if catalogViewerPreferences, ok := r.GetCatalogViewerPreferences(); ok {
			if err := writer.SetCatalogViewerPreferences(catalogViewerPreferences); err != nil {
				return nil, err
			}
		}
	}
	if !opts.SkipLanguage {
		if catalogLanguage, ok := r.GetCatalogLanguage(); ok {
			if err := writer.SetCatalogLanguage(catalogLanguage); err != nil {
				return nil, err
			}
		}
	}
	if !opts.SkipPieceInfo {
		if pieceInfo, ok := r.GetPieceInfo(); ok {
			if err := writer.SetPieceInfo(pieceInfo); err != nil {
				return nil, err
			}
		}
	}
	if !opts.SkipOutlines {
		writer.AddOutlineTree(r.GetOutlineTree())
	}
	if !opts.SkipOCProperties {
		ocProperties, err := r.GetOCProperties()
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
		} else {
			err = writer.SetOCProperties(ocProperties)
			if err != nil {
				common.Log.Debug("ERROR: %v", err)
			}
		}
	}
	if !opts.SkipPageLabels {
		pageLabels, err := r.GetPageLabels()
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
		} else {
			err = writer.SetPageLabels(pageLabels)
			if err != nil {
				common.Log.Debug("ERROR: %v", err)
			}
		}
	}
	if !opts.SkipNamedDests {
		namedDestinations, err := r.GetNamedDestinations()
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
		} else {
			err = writer.SetNamedDestinations(namedDestinations)
			if err != nil {
				common.Log.Debug("ERROR: %v", err)
			}
		}
	}
	if !opts.SkipNameDictionary {
		nameDictionary, err := r.GetNameDictionary()
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
		} else {
			err = writer.SetNameDictionary(nameDictionary)
			if err != nil {
				common.Log.Debug("ERROR: %v", err)
			}
		}
	}
	catalogStructTreeRoot, ok := r.GetCatalogStructTreeRoot()
	if ok {
		err := writer.SetCatalogStructTreeRoot(catalogStructTreeRoot)
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
		}
	}
	if !opts.SkipRotation && r.Rotate != nil {
		if err := writer.SetRotation(*r.Rotate); err != nil {
			common.Log.Debug("ERROR: %v", err)
		}
	}
	return &writer, nil
}
