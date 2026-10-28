package model

import (
	"encoding/asn1"
	"errors"
	_ "image/gif"
	"io"
	"sync"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
)

func newModelManager() *modelManager {
	manager := modelManager{}
	manager.primitiveByModel = map[PdfModel]core.PdfObject{}
	manager.modelByPrimitive = map[core.PdfObject]PdfModel{}
	manager.primitiveLock = sync.RWMutex{}
	manager.modelLock = sync.RWMutex{}
	return &manager
}

var _ pdfFont = (*pdfFontSimple)(nil)

func getModifiedDate() time.Time {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	return pdfModifiedDate
}

var _ pdfFont = (*pdfFontType0)(nil)

// PdfModel is a higher level PDF construct which can be collapsed into a PdfObject.
// Each PdfModel has an underlying PdfObject and vice versa (one-to-one).
// Under normal circumstances there should only be one copy of each.
// Copies can be made, but care must be taken to do it properly.
type PdfModel interface {
	ToPdfObject() core.PdfObject
	GetContainingPdfObject() core.PdfObject
}

var _ pdfFont = (*pdfFontType3)(nil)

const maxGapBytes = 2

// Reset sets the multi font encoder to its initial state.
func (e *MultipleFontEncoder) Reset() { e.CurrentFont = e.fontList[0] }

func parseMatte(arr *core.PdfObjectArray) (float64, error) {
	floats, err := arr.ToFloat64Array()
	if err != nil {
		common.Log.Debug("Bad Matte array: matte=%s err=%v", arr, err)
	}
	switch len(floats) {
	case 1:
		return floats[0], nil
	case 3:
		deviceRGBCS := PdfColorspaceDeviceRGB{}
		col, err := deviceRGBCS.ColorFromFloats(floats)
		if err != nil {
			return 0.0, err
		}
		return col.(*PdfColorDeviceRGB).ToGray().Val(), nil
	case 4:
		deviceCMYKCS := PdfColorspaceDeviceCMYK{}
		col, err := deviceCMYKCS.ColorFromFloats(floats)
		if err != nil {
			return 0.0, err
		}
		colorToRGB, err := deviceCMYKCS.ColorToRGB(col.(*PdfColorDeviceCMYK))
		if err != nil {
			return 0.0, err
		}
		return colorToRGB.(*PdfColorDeviceRGB).ToGray().Val(), nil
	}
	err = errors.New("bad Matte color")
	common.Log.Error("toGray: matte=%s err=%v", arr, err)
	return 0.0, err
}

type modelManager struct {
	primitiveByModel map[PdfModel]core.PdfObject
	modelByPrimitive map[core.PdfObject]PdfModel
	primitiveLock    sync.RWMutex
	modelLock        sync.RWMutex
}

const maxBitsPerComponent = 16

// Register registers (caches) a model to primitive object relationship.
func (m *modelManager) Register(primitive core.PdfObject, model PdfModel) {
	m.primitiveLock.Lock()
	defer m.primitiveLock.Unlock()
	m.modelLock.Lock()
	defer m.modelLock.Unlock()
	m.primitiveByModel[model] = primitive
	m.modelByPrimitive[primitive] = model
}

func matchesAuthorityCertIssuer(data, rawIssuer []byte) bool {
	var rawValues []asn1.RawValue
	if data2, err := asn1.Unmarshal(data, &rawValues); err != nil || len(data2) > 0 {
		return false
	}
	for _, rawValue := range rawValues {
		if rawValue.Class == asn1.ClassContextSpecific && rawValue.Tag == 4 && equalDistinguishedNames(rawValue.Bytes, rawIssuer) {
			return true
		}
	}
	return false
}

// GetPrimitiveFromModel returns the primitive object corresponding to the input `model`.
func (m *modelManager) GetPrimitiveFromModel(model PdfModel) core.PdfObject {
	m.primitiveLock.RLock()
	defer m.primitiveLock.RUnlock()
	primitiveByModel, ok := m.primitiveByModel[model]
	if !ok {
		return nil
	}
	return primitiveByModel
}

// GetModelFromPrimitive returns the model corresponding to the `primitive` PdfObject.
func (m *modelManager) GetModelFromPrimitive(primitive core.PdfObject) PdfModel {
	m.modelLock.RLock()
	defer m.modelLock.RUnlock()
	model, ok := m.modelByPrimitive[primitive]
	if !ok {
		return nil
	}
	return model
}

// multiFontEncoder implements a an Encoder that holds a list of fonts provided.
type MultipleFontEncoder struct {
	fontList    []*PdfFont
	CurrentFont *PdfFont
}

var _ pdfFont = (*pdfCIDFontType0)(nil)

type linearizationLayout struct {
	firstSection    []int64
	mainSection     []int64
	sharedObjs      []int64
	orderedObjs     []core.PdfObject
	firstShared     []int64
	pageObjs        [][]int64
	pageShared      [][]int64
	mainShared      []int64
	pageCount       int
	firstPageObjNum int64
	maxObjNum       int64
}

// LinearizationOptions configures linearized ("Fast Web View") output.
type LinearizationOptions struct {
	// UseObjectStreams toggles object-stream packaging for compressible
	// main-section objects, with xref STREAMS replacing the textual
	// xref tables. Eligible objects are indirect dictionaries that are
	// not pages, page-tree nodes, the catalog, an encryption dict, an
	// existing ObjectStreams / XRef container, or a signature dict.
	// The first-page section is kept uncompressed so page 1 can be
	// displayed without decoding any container; this matches what other
	// linearization-aware producers do.
	UseObjectStreams bool
}

// SetPdfModifiedDate sets the ModDate attribute of the output PDF.
func SetPdfModifiedDate(modifiedDate time.Time) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	pdfModifiedDate = modifiedDate
}

func (p *linearizedPlan) emit(w io.Writer) error {
	for _, data := range [][]byte{p.header, p.linDict, p.firstXref, p.hintStream, p.firstSection, p.mainSection, p.mainXref} {
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}

// GetRuneMetrics iterates through each font in the list of fonts the returns the fonts.CharMetrics from working font.
func (e *MultipleFontEncoder) GetRuneMetrics(r rune) (fonts.CharMetrics, bool) {
	currentFont := e.CurrentFont
	runeMetrics, ok := currentFont.GetRuneMetrics(r)
	for i := 1; i < len(e.fontList) && runeMetrics.Wx == 0; i++ {
		currentFont = e.fontList[i]
		runeMetrics, ok = currentFont.GetRuneMetrics(r)
	}
	return runeMetrics, ok
}

const (
	_ PdfOutputIntentType = iota
	PdfOutputIntentTypeA1
	PdfOutputIntentTypeA2
	PdfOutputIntentTypeA3
	PdfOutputIntentTypeA4
	PdfOutputIntentTypeX
)

const maxCRLAge = 24 * time.Hour

// Encoder iterates through the list of fonts and returns a working encoder
func (e *MultipleFontEncoder) Encoder(rn rune) (textencoding.TextEncoder, bool) {
	currentFont := e.CurrentFont
	encoder := currentFont.Encoder()
	_, ok := encoder.RuneToCharcode(rn)
	for i := 1; i < len(e.fontList) && !ok; i++ {
		currentFont = e.fontList[i]
		encoder = currentFont.Encoder()
		_, ok = encoder.RuneToCharcode(rn)
		e.CurrentFont = currentFont
	}
	return encoder, ok
}

func mergeFontDescriptor(fontDescriptor, std14Descriptor *PdfFontDescriptor) {
	if fontDescriptor == nil || std14Descriptor == nil {
		return
	}
	for _, st := range []struct{ dst, src *core.PdfObject }{{&fontDescriptor.FontName, &std14Descriptor.FontName}, {&fontDescriptor.FontFamily, &std14Descriptor.FontFamily}, {&fontDescriptor.FontWeight, &std14Descriptor.FontWeight}, {&fontDescriptor.Flags, &std14Descriptor.Flags}, {&fontDescriptor.FontBBox, &std14Descriptor.FontBBox}, {&fontDescriptor.ItalicAngle, &std14Descriptor.ItalicAngle}, {&fontDescriptor.Ascent, &std14Descriptor.Ascent}, {&fontDescriptor.Descent, &std14Descriptor.Descent}, {&fontDescriptor.CapHeight, &std14Descriptor.CapHeight}, {&fontDescriptor.XHeight, &std14Descriptor.XHeight}, {&fontDescriptor.StemV, &std14Descriptor.StemV}, {&fontDescriptor.StemH, &std14Descriptor.StemH}} {
		if *st.dst == nil {
			*st.dst = *st.src
		}
	}
}

type linearizedPlan struct {
	header             []byte
	linDict            []byte
	firstXref          []byte
	firstSection       []byte
	hintStream         []byte
	mainSection        []byte
	mainXref           []byte
	linDictOffset      int64
	firstXrefOffset    int64
	hintOffset         int64
	mainXrefOffset     int64
	mainXrefFirstEntry int64
	firstPageEnd       int64
	fileLength         int64
}

// GetColorGlyphInfoByRune iterates through each font in the list of fonts and returns the color glyph info from working font.
func (e *MultipleFontEncoder) GetColorGlyphInfoByRune(r rune) *ColorGlyphInfo {
	otfType := e.CurrentFont.GetOTFType()
	if otfType != nil {
		colorGlyphInfoByRune := GetColorGlyphInfoByRune(otfType, r)
		if colorGlyphInfoByRune != nil {
			return colorGlyphInfoByRune
		}
	}
	for _, font := range e.fontList {
		otfType = font.GetOTFType()
		if otfType != nil {
			colorGlyphInfoByRune := GetColorGlyphInfoByRune(otfType, r)
			if colorGlyphInfoByRune != nil {
				e.CurrentFont = font
				return colorGlyphInfoByRune
			}
		}
	}
	return nil
}

const maxLinearizationIterations = 16

// GetColorGlyphInfoByText iterates through each font in the list of fonts and returns the color glyph info from working font.
// This handles multi-rune sequences like ZWJ emoji (e.g., "🙋‍♂️").
func (e *MultipleFontEncoder) GetColorGlyphInfoByText(text string) *ColorGlyphInfo {
	otfType := e.CurrentFont.GetOTFType()
	if otfType != nil {
		colorGlyphInfoByText := GetColorGlyphInfoByText(otfType, text)
		if colorGlyphInfoByText != nil {
			return colorGlyphInfoByText
		}
	}
	for _, font := range e.fontList {
		otfType = font.GetOTFType()
		if otfType != nil {
			colorGlyphInfoByText := GetColorGlyphInfoByText(otfType, text)
			if colorGlyphInfoByText != nil {
				e.CurrentFont = font
				return colorGlyphInfoByText
			}
		}
	}
	return nil
}

func measureXrefTable(writer *PdfWriter, nums []int64, n int64, count int64, ok bool, found bool) int {
	xrefTable, _ := buildXrefTable(writer, nums, map[int64]int64{}, n, count, ok, 0, found)
	return len(xrefTable)
}

const linearizationDictObjNum2 = 1

var (
	metadataMu      sync.Mutex
	pdfAuthor       = ""
	pdfCreationDate time.Time
	pdfCreator      = ""
	pdfKeywords     = ""
	pdfModifiedDate time.Time
	pdfProducer     = ""
	pdfSubject      = ""
	pdfTitle        = ""
)

// NewMultipleFontEncoder returns instantiates a new *MultipleFontEncoder
func NewMultipleFontEncoder(fonts []*PdfFont) *MultipleFontEncoder {
	return &MultipleFontEncoder{fontList: fonts, CurrentFont: fonts[0]}
}

const (
	maxFunctionInputs = 8
	maxSampledInputs  = 8
)

var _ pdfFont = (*pdfCIDFontType2)(nil)
