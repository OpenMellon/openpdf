// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package model provides an interface for working with high-level objects (models) in PDF files, including
// reading and writing documents.
//
// The document structure of a PDF is constructed of a hierarchy of data models, representing a tree
// of information starting from the Document catalog (Figure 5 p. 80).
// It is based on the core package which handles core functionality such as file i/o, parsing and
// handling of primitive PDF objects (core.PdfObject).
//
// As an example of the interface, the following snippet can read the PDF and output the number of pages:
//
//	f, err := os.Open(inputPath)
//		if err != nil {
//		return nil, err
//	}
//	defer f.Close()
//	pdfReader, err := openpdf.NewPdfReader(f)
//	if err != nil {
//		fmt.Printf("Failed to read PDF file: %v\n", err)
//		os.Exit(1)
//	}
//	numPages, err := pdfReader.GetNumPages()
//	if err != nil {
//		fmt.Printf("Failed to get number of pages: %v\n", err)
//		os.Exit(1)
//	}
//	fmt.Printf("The PDF file has %d pages\n", numPages)
//
// For more examples, see the examples directory of the repository
package model

import (
	"bufio"
	"bytes"
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/png"
	"io"
	"math"
	"math/big"
	"math/bits"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/OpenMellon/freetype"
	"github.com/OpenMellon/freetype/truetype"
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/core/security"
	"github.com/OpenMellon/openpdf/core/security/crypt"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/imageutil/iccextract"
	"github.com/OpenMellon/openpdf/internal/lazystream"
	"github.com/OpenMellon/openpdf/internal/license"
	"github.com/OpenMellon/openpdf/internal/sampling"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/internal/timeutils"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/internal/uuid"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
	"github.com/OpenMellon/openpdf/model/internal/icctransform"
	"github.com/OpenMellon/openpdf/model/internal/sigattr"
	"github.com/OpenMellon/openpdf/model/mdp"
	"github.com/OpenMellon/openpdf/model/sigutil"
	"github.com/OpenMellon/openpdf/ps"
	"github.com/OpenMellon/pkcs7"
	"github.com/OpenMellon/timestamp"
	typefont "github.com/OpenMellon/typesetting/font"
	"github.com/OpenMellon/typesetting/font/opentype"
	"github.com/OpenMellon/typesetting/shaping"
	"github.com/OpenMellon/unitype"
	"github.com/h2non/filetype"
	"golang.org/x/crypto/ocsp"
	imagefont "golang.org/x/image/font"
	"golang.org/x/xerrors"
)

func (r *PdfReader) loadFromParser(parser *core.PdfParser) (*PdfReader, error) {
	r.parser = parser
	ok, err := r.IsEncrypted()
	if err != nil {
		return nil, err
	}
	if !ok {
		err = r.loadStructure()
		if err != nil {
			return nil, err
		}
	} else if r.autoDecrypt {
		var ok bool
		if crypter := parser.GetCrypter(); crypter != nil && crypter.IsPublicKey() {
			if r.opts.Certificate == nil || r.opts.PrivateKey == nil {
				return nil, core.ErrCertificateRequired
			}
			ok, err = r.DecryptWithCertificate(r.opts.Certificate, r.opts.PrivateKey)
		} else {
			ok, err = r.Decrypt([]byte(r.opts.Password))
		}
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("unable to decrypt password protected file - need to specify pass to Decrypt")
		}
	}
	r.revisionCache = make(map[*PdfReader]*PdfReader)
	r.revisions = make([]*PdfReader, parser.GetRevisionNumber())
	return r, nil
}

// ToJBIG2Image converts current image to the core.JBIG2Image.
func (img *Image) ToJBIG2Image() (*core.JBIG2Image, error) {
	img2, err := img.ToGoImage()
	if err != nil {
		return nil, err
	}
	return core.GoImageToJBIG2(img2, core.JB2ImageAutoThreshold)
}

// ViewClip returns the value of the viewClip.
func (vp *ViewerPreferences) ViewClip() PageBoundary { return vp.viewClip }

func signatureContentsDigestHex(sig *PdfSignature) (string, error) {
	if sig.Contents == nil {
		return "", errors.New("signature Contents not set")
	}
	data, err := sha1Digest(sig.Contents.Bytes())
	if err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(data)), nil
}

// Colorspace returns a Pattern colorspace whose underlying colorspace, if any,
// is derived from the underlying color.
func (p *PdfColorPattern) Colorspace() PdfColorspace {
	specialPatternCS := NewPdfColorspaceSpecialPattern()
	if p.Color != nil {
		specialPatternCS.UnderlyingCS = p.Color.Colorspace()
	}
	return specialPatternCS
}

// ColorFromPdfObjects returns a new PdfColor based on the input slice of color
// components. The slice should contain three PdfObjectFloat elements representing
// the A, B and C components of the color.
func (cs *PdfColorspaceCalRGB) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) != 3 {
		return nil, errors.New("range check")
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objects)
	if err != nil {
		return nil, err
	}
	return cs.ColorFromFloats(numbersAsFloat)
}

// NewPdfAnnotationRedact returns a new redact annotation.
func NewPdfAnnotationRedact() *PdfAnnotationRedact {
	annot := NewPdfAnnotation()
	redact := &PdfAnnotationRedact{}
	redact.PdfAnnotation = annot
	redact.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(redact)
	return redact
}

func (r *PdfReader) loadOutlines() (*PdfOutlineTreeNode, error) {
	if r.parser.GetCrypter() != nil && !r.parser.IsAuthenticated() {
		return nil, fmt.Errorf("file need to be decrypted first")
	}
	catalog := r.catalog
	outlines := catalog.Get("Outlines")
	if outlines == nil {
		return nil, nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("-Has outlines")
	}
	obj := core.ResolveReference(outlines)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Outline root: %v", obj)
	}
	if ok := core.IsNullObject(obj); ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Outline root is null - no outlines")
		}
		return nil, nil
	}
	indirect, ok := obj.(*core.PdfIndirectObject)
	if !ok {
		if _, ok := core.GetDict(obj); !ok {
			common.Log.Debug("Invalid outline root - skipping")
			return nil, nil
		}
		common.Log.Debug("Outline root is a dict. Should be an indirect object")
		indirect = core.MakeIndirectObject(obj)
	}
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return nil, errors.New("outline indirect object should contain a dictionary")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Outline root dict: %v", dict)
	}
	outlineTree, _, err := r.buildOutlineTree(indirect, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Resulting outline tree: %v", outlineTree)
	}
	return outlineTree, nil
}

// GetEncryptionMethod returns a descriptive information string about the encryption method used.
func (r *PdfReader) GetEncryptionMethod() string {
	crypter := r.parser.GetCrypter()
	return crypter.String()
}

func (f *pdfFontSimple) getFontEncoding() (s string, glyphByCode map[textencoding.CharCode]textencoding.GlyphName, err error) {
	s = "StandardEncoding"
	if symbolicFontEncoding, ok := symbolicFontEncodings[f.baseFont]; ok {
		s = symbolicFontEncoding
	} else if f.fontFlags()&fontFlagSymbolic != 0 {
		for s2, symbolicFontEncoding := range symbolicFontEncodings {
			if strings.Contains(f.baseFont, s2) {
				s = symbolicFontEncoding
				break
			}
		}
	}
	if f.Encoding == nil {
		return s, nil, nil
	}
	switch v := f.Encoding.(type) {
	case *core.PdfObjectName:
		return string(*v), nil, nil
	case *core.PdfObjectDictionary:
		baseEncoding, ok := core.GetName(v.Get("BaseEncoding"))
		if ok {
			s = baseEncoding.String()
		}
		if differences := v.Get("Differences"); differences != nil {
			differencesArr, ok := core.GetArray(differences)
			if !ok {
				common.Log.Debug("ERROR: Bad font encoding dict=%+v Differences=%T", v, v.Get("Differences"))
				return "", nil, core.ErrTypeError
			}
			glyphByCode, err = textencoding.FromFontDifferences(differencesArr)
		}
		return s, glyphByCode, err
	default:
		common.Log.Debug("ERROR: Encoding not a name or dict (%T) %s", f.Encoding, f.Encoding)
		return "", nil, core.ErrTypeError
	}
}

// SetNumCopies sets the value of the numCopies.
func (vp *ViewerPreferences) SetNumCopies(numCopies int) { vp.numCopies = numCopies }

func adjustHintOffset(offset, n, count int64) (uint32, error) {
	num := offset
	if offset >= n {
		num = offset - count
	}
	if num < 0 || num > math.MaxUint32 {
		return 0, fmt.Errorf("hint table offset %d cannot be encoded in 32 bits (file exceeds 4 GB)", num)
	}
	return uint32(num), nil
}

func (w *PdfWriter) setCatalogVersion() {
	w.catalog.Set("Version", core.MakeName(fmt.Sprintf("%d.%d", w.version.Major, w.version.Minor)))
}

// NewOutline returns a new outline instance.
func NewOutline() *Outline { return &Outline{} }

// GetContext returns the PdfField context which is the more specific field data type, e.g. PdfFieldButton
// for a button field.
func (f *PdfField) GetContext() PdfModel { return f.context }

func (img Image) getBase() imageutil.ImageBase {
	return imageutil.NewImageBase(int(img.Width), int(img.Height), int(img.BitsPerComponent), img.ColorComponents, img.Data, img.alpha, img.decode)
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationPrinterMark) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("PrinterMark"))
	dict.SetIfNotNil("MN", a.MN)
	return indirect
}
func sortedUniqueUint16(vals []uint16) []uint16 { slices.Sort(vals); return slices.Compact(vals) }

// CompliancePdfReader is a wrapper over PdfReader that is used for verifying if the input Pdf document matches the
// compliance rules of standards like PDF/A.
// NOTE: This implementation is in experimental development state.
//
//	Keep in mind that it might change in the subsequent minor versions.
type CompliancePdfReader struct {
	*PdfReader
	parserMetadata core.ParserMetadata
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationProjection) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	return indirect
}

// NewPdfSignatureReferenceDocMDP returns PdfSignatureReference for the transformParams.
func NewPdfSignatureReferenceDocMDP(transformParams *PdfTransformParamsDocMDP) *PdfSignatureReference {
	return &PdfSignatureReference{Type: core.MakeName("SigRef"), TransformMethod: core.MakeName("DocMDP"), TransformParams: transformParams.ToPdfObject()}
}

func widgetMKProperties(widget *PdfAnnotationWidget) (col, col2 PdfColor, s string, n int) {
	if widget == nil || widget.MK == nil {
		return
	}
	dict, ok := core.GetDict(widget.MK)
	if !ok {
		return
	}
	if bc, ok := core.GetArray(dict.Get("BC")); ok {
		col = colorFromArray(bc)
	}
	if bg, ok := core.GetArray(dict.Get("BG")); ok {
		col2 = colorFromArray(bg)
	}
	if ca, ok := core.GetString(dict.Get("CA")); ok {
		s = ca.Decoded()
	}
	if r, ok := core.GetInt(dict.Get("R")); ok {
		n = int(*r)
	}
	return
}

func (f *pdfFontType0) subsetRegistered() error {
	switch v := f.DescendantFont.context.(type) {
	case *pdfCIDFontType2:
		return f.subsetTrueType(v)
	case *pdfCIDFontType0:
		return f.subsetCFF(v)
	default:
		common.Log.Debug("Font not supported for subsetting %T", f.DescendantFont)
		return nil
	}
}

// NewXObjectImageFromImageLazy creates a new XObject Image from an image object
// with default options. If encoder is nil, uses raw encoding (none).
// If lazy is true, then lazy mode is enabled for XObject.
// Lazy mode allows to reduce memory usage with the help of temporary files.
func NewXObjectImageFromImageLazy(img *Image, cs PdfColorspace, encoder core.StreamEncoder, lazy bool) (*XObjectImage, error) {
	xObjectImage := NewXObjectImage()
	if lazy {
		updateXObjectImage, err := UpdateXObjectImageFromImage(xObjectImage, img, cs, encoder)
		if err != nil {
			return nil, err
		}
		if err := updateXObjectImage.MakeLazy(); err != nil {
			return nil, err
		}
		return updateXObjectImage, nil
	}
	return UpdateXObjectImageFromImage(xObjectImage, img, cs, encoder)
}

// NewPdfColorDeviceCMYK returns a new CMYK32 color.
func NewPdfColorDeviceCMYK(c, m, y, k float64) *PdfColorDeviceCMYK {
	deviceCMYKColor := PdfColorDeviceCMYK{c, m, y, k}
	return &deviceCMYKColor
}

// GetBorderWidth returns the border style's width.
func (b *PdfBorderStyle) GetBorderWidth() float64 {
	if b.W == nil {
		return 1
	}
	return *b.W
}

// AppendContentBytes creates a PDF stream from `cs` and appends it to the
// array of streams specified by the pages's Contents entry.
// If `wrapContents` is true, the content stream of the page is wrapped using
// a `q/Q` operator pair, so that its state does not affect the appended
// content stream.
func (p *PdfPage) AppendContentBytes(cs []byte, wrapContents bool) error {
	contentStreamObjs := p.GetContentStreamObjs()
	wrapContents = wrapContents && len(contentStreamObjs) > 0
	flateEncoder := core.NewFlateEncoder()
	array := core.MakeArray()
	if wrapContents {
		stream, err := core.MakeStream([]byte("q\n"), flateEncoder)
		if err != nil {
			return err
		}
		array.Append(stream)
	}
	array.Append(contentStreamObjs...)
	if wrapContents {
		stream, err := core.MakeStream([]byte("\nQ\n"), flateEncoder)
		if err != nil {
			return err
		}
		array.Append(stream)
	}
	stream, err := core.MakeStream(cs, flateEncoder)
	if err != nil {
		return err
	}
	array.Append(stream)
	p.Contents = array
	return nil
}

var ImageHandling ImageHandler = DefaultImageHandler{}

func (a *PdfAnnotation) String() string {
	s := ""
	indirect, ok := a.ToPdfObject().(*core.PdfIndirectObject)
	if ok {
		s = fmt.Sprintf("%T: %s", a.context, indirect.PdfObject.String())
	}
	return s
}

// IsTerminal returns true for terminal fields, false otherwise.
// Terminal fields are fields whose descendants are only widget annotations.
func (f *PdfField) IsTerminal() bool { return len(f.Kids) == 0 }

func (a *PdfAppender) hasPriorSignature() bool {
	if a.roReader.AcroForm == nil {
		return false
	}
	for _, signatureField := range a.roReader.AcroForm.signatureFields() {
		if signatureField.V != nil {
			return true
		}
	}
	return false
}

// ToPdfObject converts colorspace to a PDF object. [/Indexed base hival lookup]
func (cs *PdfColorspaceSpecialIndexed) ToPdfObject() core.PdfObject {
	array := core.MakeArray(core.MakeName("Indexed"))
	array.Append(cs.Base.ToPdfObject())
	array.Append(core.MakeInteger(int64(cs.HiVal)))
	array.Append(cs.Lookup)
	if cs.container != nil {
		cs.container.PdfObject = array
		return cs.container
	}
	return array
}

func (r *PdfReader) newPdfActionThreadFromDict(dict *core.PdfObjectDictionary) (*PdfActionThread, error) {
	filespec, err := newPdfFilespecFromPdfObject(dict.Get("F"))
	if err != nil {
		return nil, err
	}
	return &PdfActionThread{D: dict.Get("D"), B: dict.Get("B"), F: filespec}, nil
}

var predefinedEncodingNames = map[string]struct{}{"WinAnsiEncoding": {}, "MacRomanEncoding": {}, "MacExpertEncoding": {}, "StandardEncoding": {}}

// NewXObjectImageFromImage creates a new XObject Image from an image object
// with default options. If encoder is nil, uses raw encoding (none).
func NewXObjectImageFromImage(img *Image, cs PdfColorspace, encoder core.StreamEncoder) (*XObjectImage, error) {
	xObjectImage := NewXObjectImage()
	return UpdateXObjectImageFromImage(xObjectImage, img, cs, encoder)
}

// ToPdfObject sets the common field elements.
// Note: Call the more field context's ToPdfObject to set both the generic and
// non-generic information.
func (f *PdfField) ToPdfObject() core.PdfObject {
	indirect := f.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	array := core.MakeArray()
	for _, kid := range f.Kids {
		array.Append(kid.ToPdfObject())
	}
	for _, annotation := range f.Annotations {
		if annotation.primitive != f.primitive {
			array.Append(annotation.GetContext().ToPdfObject())
		}
	}
	if f.Parent != nil {
		dict.SetIfNotNil("Parent", f.Parent.GetContainingPdfObject())
	}
	if array.Len() > 0 {
		dict.Set("Kids", array)
	}
	dict.SetIfNotNil("FT", f.FT)
	dict.SetIfNotNil("T", f.T)
	dict.SetIfNotNil("TU", f.TU)
	dict.SetIfNotNil("TM", f.TM)
	dict.SetIfNotNil("Ff", f.Ff)
	dict.SetIfNotNil("V", f.V)
	dict.SetIfNotNil("DV", f.DV)
	dict.SetIfNotNil("AA", f.AA)
	if f.VariableText != nil {
		dict.SetIfNotNil("DA", f.VariableText.DA)
		dict.SetIfNotNil("Q", f.VariableText.Q)
		dict.SetIfNotNil("DS", f.VariableText.DS)
		dict.SetIfNotNil("RV", f.VariableText.RV)
	}
	return indirect
}

// GetCatalogMetadata gets the catalog defined XMP Metadata.
func (r *PdfReader) GetCatalogMetadata() (core.PdfObject, bool) {
	if r.catalog == nil {
		return nil, false
	}
	metadata := r.catalog.Get("Metadata")
	return metadata, metadata != nil
}

func (w lazyStreamWriter) Write(p []byte) (int, error) {
	w.writer.writeBytes(p)
	if w.writer.writeErr != nil {
		return 0, w.writer.writeErr
	}
	return len(p), nil
}

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

// SubsetRegistered subsets the font to only the glyphs that have been registered by the encoder.
//
// NOTE: This only works on fonts that support subsetting. For unsupported fonts this is a no-op, although a debug
// message is emitted.  Currently supported fonts are embedded Truetype CID fonts (type 0).
//
// NOTE: Make sure to call this soon before writing (once all needed runes have been registered).
// If using package creator, use its EnableFontSubsetting method instead.
func (f *PdfFont) SubsetRegistered() error {
	switch v := f.context.(type) {
	case *pdfFontType0:
		err := v.subsetRegistered()
		if err != nil {
			common.Log.Debug("Subset error: %v", err)
			return err
		}
		if v.primitive != nil {
			if v.encoder != nil {
				v.encoder.ToPdfObject()
			}
			v.ToPdfObject()
		}
	default:
		common.Log.Debug("Font %T does not support subsetting", v)
	}
	return nil
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationTrapNet) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("TrapNet"))
	return indirect
}

// ImageToRGB convert 1-component grayscale data to 3-component RGB.
func (cs *PdfColorspaceDeviceGray) ImageToRGB(img Image) (Image, error) {
	if img.ColorComponents != 1 {
		return img, errors.New("the provided image is not gray scale")
	}
	img2, err := imageutil.NewImage(int(img.Width), int(img.Height), int(img.BitsPerComponent), img.ColorComponents, img.Data, img.alpha, img.decode)
	if err != nil {
		return img, err
	}
	convert, err := imageutil.NRGBAConverter.Convert(img2)
	if err != nil {
		return img, err
	}
	img3 := imageFromImageBase(convert.Base())
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("DeviceGray -> RGB")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("samples: %v", img.Data)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("RGB samples: %v", img3.Data)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("%v -> %v", img, img3)
	}
	return img3, nil
}

func newPdfFieldChoiceFromDict(dict *core.PdfObjectDictionary) (*PdfFieldChoice, error) {
	choiceField := &PdfFieldChoice{}
	choiceField.Opt, _ = core.GetArray(dict.Get("Opt"))
	choiceField.TI, _ = core.GetInt(dict.Get("TI"))
	choiceField.I, _ = core.GetArray(dict.Get("I"))
	return choiceField, nil
}

func (r *PdfReader) newPdfAnnotationStampFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationStamp, error) {
	stamp := PdfAnnotationStamp{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	stamp.PdfAnnotationMarkup = markup
	stamp.Name = dict.Get("Name")
	return &stamp, nil
}

// FieldFlattenOpts defines a set of options which can be used to configure
// the field flattening process.
type FieldFlattenOpts struct {
	// FilterFunc allows filtering the form fields used in the flattening
	// process. If the filter function returns true, the field is flattened,
	// otherwise it is skipped.
	// If a non-terminal field is discarded, all of its children (the fields
	// present in the Kids array) are discarded as well.
	// Non-terminal fields are kept in the AcroForm if one or more of their
	// child fields have not been selected for flattening.
	// If a filter function is not provided, all form fields are flattened.
	FilterFunc FieldFilterFunc

	// AnnotFilterFunc allows filtering the annotations in the flattening
	// process. If the filter function returns true, the annotation is flattened,
	// otherwise it is skipped.
	// Widget (form field) annotations are not subject to this filter: the
	// page-level flattening path always flattens them, and the reader-level
	// path selects them via FilterFunc.
	AnnotFilterFunc AnnotFilterFunc
}

func (p *PdfPage) getParentResources() (*PdfPageResources, error) {
	parent := p.Parent
	for parent != nil {
		parentDict, ok := core.GetDict(parent)
		if !ok {
			common.Log.Debug("ERROR: invalid parent node")
			return nil, errors.New("invalid parent object")
		}
		if resources := parentDict.Get("Resources"); resources != nil {
			resourcesDict, ok := core.GetDict(resources)
			if !ok {
				return nil, errors.New("invalid resource dict")
			}
			resources, err := NewPdfPageResourcesFromDict(resourcesDict)
			if err != nil {
				return nil, err
			}
			return resources, nil
		}
		parent = parentDict.Get("Parent")
	}
	return nil, nil
}

func newModelManager() *modelManager {
	manager := modelManager{}
	manager.primitiveByModel = map[PdfModel]core.PdfObject{}
	manager.modelByPrimitive = map[core.PdfObject]PdfModel{}
	manager.primitiveLock = sync.RWMutex{}
	manager.modelLock = sync.RWMutex{}
	return &manager
}

// ColorToRGB converts a CalRGB color to an RGB color.
func (cs *PdfColorspaceCalRGB) ColorToRGB(color PdfColor) (PdfColor, error) {
	calRGBColor, ok := color.(*PdfColorCalRGB)
	if !ok {
		common.Log.Debug("Input color not cal rgb")
		return nil, errors.New("type check error")
	}
	f := calRGBColor.A()
	v := calRGBColor.B()
	val := calRGBColor.C()
	X := cs.Matrix[0]*math.Pow(f, cs.Gamma[0]) + cs.Matrix[3]*math.Pow(v, cs.Gamma[1]) + cs.Matrix[6]*math.Pow(val, cs.Gamma[2])
	Y := cs.Matrix[1]*math.Pow(f, cs.Gamma[0]) + cs.Matrix[4]*math.Pow(v, cs.Gamma[1]) + cs.Matrix[7]*math.Pow(val, cs.Gamma[2])
	Z := cs.Matrix[2]*math.Pow(f, cs.Gamma[0]) + cs.Matrix[5]*math.Pow(v, cs.Gamma[1]) + cs.Matrix[8]*math.Pow(val, cs.Gamma[2])
	x := 3.240479*X + -1.537150*Y + -0.498535*Z
	y := -0.969256*X + 1.875992*Y + 0.041556*Z
	y2 := 0.055648*X + -0.204043*Y + 1.057311*Z
	x = math.Min(math.Max(x, 0), 1.0)
	y = math.Min(math.Max(y, 0), 1.0)
	y2 = math.Min(math.Max(y2, 0), 1.0)
	return NewPdfColorDeviceRGB(x, y, y2), nil
}

// Encrypt encrypts the output file with a specified user/owner password.
// An empty owner password is replaced by the user password, so the user
// password then grants full permissions regardless of options.Permissions.
//
// With AES_256bit the password bytes are interpreted as UTF-8 and prepared
// with the SASLprep profile (RFC 4013) before hashing, as required by
// ISO 32000-2; an error is returned if a password contains characters the
// profile prohibits (control characters, bidi violations, ...) or consists
// only of characters it removes. Input that is not valid UTF-8 is used as
// given without preparation. With the other algorithms the password bytes
// are used as given.
func (w *PdfWriter) Encrypt(userPass, ownerPass []byte, options *EncryptOptions) error {
	algo := RC4_128bit
	perms := security.PermOwner
	if options != nil {
		algo = options.Algorithm
		perms = options.Permissions
	}
	cryptFilter, err := newCryptFilter(algo)
	if err != nil {
		return err
	}
	pdfCryptNewEncrypt, encryptInfo, err := core.PdfCryptNewEncrypt(cryptFilter, userPass, ownerPass, perms)
	if err != nil {
		return err
	}
	w.setEncryption(pdfCryptNewEncrypt, encryptInfo)
	return nil
}

// PdfColorPattern represents a pattern color.
type PdfColorPattern struct {
	Color       PdfColor
	PatternName core.PdfObjectName
}

func (r *PdfReader) newPdfAnnotationLineFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationLine, error) {
	line := PdfAnnotationLine{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	line.PdfAnnotationMarkup = markup
	line.L = dict.Get("L")
	line.BS = dict.Get("BS")
	line.LE = dict.Get("LE")
	line.IC = dict.Get("IC")
	line.LL = dict.Get("LL")
	line.LLE = dict.Get("LLE")
	line.Cap = dict.Get("Cap")
	line.IT = dict.Get("IT")
	line.LLO = dict.Get("LLO")
	line.CP = dict.Get("CP")
	line.Measure = dict.Get("Measure")
	line.CO = dict.Get("CO")
	return &line, nil
}
func libraryVersion() string { return common.Version }

// RunesToCharcodeBytes maps the provided runes to charcode bytes and it
// returns the resulting slice of bytes, along with the number of runes which
// could not be converted. If the number of misses is 0, all runes were
// successfully converted.
func (f *PdfFont) RunesToCharcodeBytes(data []rune) ([]byte, int) {
	var encs []textencoding.TextEncoder
	var cMapEncoder textencoding.CMapEncoder
	if toUnicodeCMap := f.baseFields().toUnicodeCMap; toUnicodeCMap != nil {
		cMapEncoder = textencoding.NewCMapEncoder("", nil, toUnicodeCMap)
	}
	encoder := f.Encoder()
	if encoder != nil {
		switch v := encoder.(type) {
		case textencoding.SimpleEncoder:
			baseName := v.BaseName()
			if _, ok := predefinedEncodingNames[baseName]; ok {
				encs = append(encs, encoder)
			}
		}
	}
	if len(encs) == 0 {
		if f.baseFields().toUnicodeCMap != nil {
			encs = append(encs, cMapEncoder)
		}
		if encoder != nil {
			encs = append(encs, encoder)
		}
	}
	var buf bytes.Buffer
	var n int
	for _, count := range data {
		var ok bool
		for _, enc := range encs {
			if data := enc.Encode(string(count)); len(data) > 0 {
				buf.Write(data)
				ok = true
				break
			}
		}
		if !ok {
			common.Log.Debug("ERROR: failed to map rune `%+q` to charcode", count)
			n++
		}
	}
	if n != 0 {
		common.Log.Debug("ERROR: could not convert all runes to charcodes.\n"+"\tnumRunes=%d numMisses=%d\n"+"\tfont=%s encoders=%+v", len(data), n, f, encs)
	}
	return buf.Bytes(), n
}
func newNames() *Names { return &Names{container: core.MakeIndirectObject(core.MakeDict())} }

// Height returns the height of `rect`.
func (r *PdfRectangle) Height() float64 { return math.Abs(r.Ury - r.Lly) }

// PdfAnnotationSquare represents Square annotations.
// (Section 12.5.6.8).
type PdfAnnotationSquare struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	BS core.PdfObject
	IC core.PdfObject
	BE core.PdfObject
	RD core.PdfObject
}

var _ pdfFont = (*pdfFontSimple)(nil)

// NewLTV returns a new LTV client.
func NewLTV(appender *PdfAppender) (*LTV, error) {
	dss := appender.Reader.DSS
	if dss == nil {
		dss = NewDSS()
	}
	if err := dss.GenerateHashMaps(); err != nil {
		return nil, err
	}
	return &LTV{CertClient: sigutil.NewCertClient(), OCSPClient: sigutil.NewOCSPClient(), CRLClient: sigutil.NewCRLClient(), SkipExisting: true, appender: appender, dss: dss}, nil
}

// PdfShadingPatternType2 is shading patterns that will use a Type 2 shading pattern (Axial).
type PdfShadingPatternType2 struct {
	*PdfPattern
	Shading   *PdfShadingType2
	Matrix    *core.PdfObjectArray
	ExtGState core.PdfObject
}

func getModifiedDate() time.Time {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	return pdfModifiedDate
}

// GetContainingPdfObject implements interface PdfModel.
func (f *PdfFilespec) GetContainingPdfObject() core.PdfObject { return f.primitive }

var (
	StructureTypeSpan               StructureType = "Span"
	StructureTypeQuote              StructureType = "Quote"
	StructureTypeNote               StructureType = "Note"
	StructureTypeReference          StructureType = "Reference"
	StructureTypeBibliography       StructureType = "BibEntry"
	StructureTypeCode               StructureType = "Code"
	StructureTypeLink               StructureType = "Link"
	StructureTypeAnnot              StructureType = "Annot"
	StructureTypeRuby               StructureType = "Ruby"
	StructureTypeWarichu            StructureType = "Warichu"
	StructureTypeRubyBase           StructureType = "RB"
	StructureTypeRubyText           StructureType = "RT"
	StructureTypeRubyPunctuation    StructureType = "RP"
	StructureTypeWarichuText        StructureType = "WT"
	StructureTypeWarichuPunctuation StructureType = "WP"
	StructureTypeFigure             StructureType = "Figure"
	StructureTypeFormula            StructureType = "Formula"
	StructureTypeForm               StructureType = "Form"
	StructureTypeDocumentFragment   StructureType = "DocumentFragment"
	StructureTypeAside              StructureType = "Aside"
	StructureTypeTitle              StructureType = "Title"
	StructureTypeFENote             StructureType = "FENote"
	StructureTypeSub                StructureType = "Sub"
	StructureTypeEm                 StructureType = "Em"
	StructureTypeStrong             StructureType = "Strong"
)

// GetOCProperties returns the optional content properties PdfObject.
func (r *PdfReader) GetOCProperties() (core.PdfObject, error) {
	catalog := r.catalog
	ocProperties := catalog.Get("OCProperties")
	ocProperties = core.ResolveReference(ocProperties)
	if !r.lazy {
		err := r.traverseObjectData(ocProperties)
		if err != nil {
			return nil, err
		}
	}
	return ocProperties, nil
}

func (t *trustEvaluator) trustedSignatureTimestamp(p7 *pkcs7.PKCS7, certs []*x509.Certificate, evidence revocationEvidence) (time.Time, []string, []string) {
	if p7 == nil || len(p7.Signers) != 1 {
		return time.Time{}, nil, nil
	}
	signer := p7.Signers[0]
	var strs []string
	for _, unauthenticatedAttribute := range signer.UnauthenticatedAttributes {
		if !unauthenticatedAttribute.Type.Equal(pkcs7.OIDAttributeTimeStampToken) {
			continue
		}
		verifySignatureTimestamp, strs2, err := t.verifySignatureTimestamp(unauthenticatedAttribute.Value.Bytes, signer.EncryptedDigest, p7.Certificates, certs, evidence)
		if err != nil {
			strs = append(strs, err.Error())
			continue
		}
		return verifySignatureTimestamp, strs2, nil
	}
	return time.Time{}, nil, strs
}

// GetCatalogStructTreeRoot gets the catalog StructTreeRoot object.
func (r *PdfReader) GetCatalogStructTreeRoot() (core.PdfObject, bool) {
	structTreeRoot := core.ResolveReference(r.catalog.Get("StructTreeRoot"))
	if structTreeRoot == nil {
		return nil, false
	}
	if !r.lazy {
		err := r.traverseObjectData(structTreeRoot)
		if err != nil {
			common.Log.Debug("ERROR: Failed to traverse StructTreeRoot object (%s)", err)
			return nil, false
		}
	}
	return structTreeRoot, true
}

// NewCompositePdfFontFile loads a font file (TTF or OTF) and returns a composite PdfFont.
// This is the recommended way to load fonts for full Unicode support, text shaping,
// and color glyph (emoji) capabilities.
func NewCompositePdfFontFile(filePath string) (*PdfFont, error) {
	open, err := os.Open(filePath)
	if err != nil {
		common.Log.Debug("ERROR: reading font file: %v", err)
		return nil, err
	}
	defer open.Close()
	return NewCompositePdfFont(open)
}

// PdfPageResources is a Page resources model.
// Implements PdfModel.
type PdfPageResources struct {
	ExtGState   core.PdfObject
	ColorSpace  core.PdfObject
	Pattern     core.PdfObject
	Shading     core.PdfObject
	XObject     core.PdfObject
	Font        core.PdfObject
	ProcSet     core.PdfObject
	Properties  core.PdfObject
	primitive   *core.PdfObjectDictionary
	colorspaces *PdfPageResourcesColorspaces
}

func newPdfColorspaceICCBasedFromPdfObject(obj core.PdfObject) (*PdfColorspaceICCBased, error) {
	iccBasedCS := &PdfColorspaceICCBased{}
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		iccBasedCS.container = indirect
	}
	obj = core.TraceToDirectObject(obj)
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("type error")
	}
	if arr.Len() != 2 {
		return nil, fmt.Errorf("invalid ICCBased colorspace")
	}
	obj = core.TraceToDirectObject(arr.Get(0))
	name, ok := obj.(*core.PdfObjectName)
	if !ok {
		return nil, fmt.Errorf("ICCBased name not a Name object")
	}
	if *name != "ICCBased" {
		return nil, fmt.Errorf("not an ICCBased colorspace")
	}
	obj = arr.Get(1)
	stream, ok := core.GetStream(obj)
	if !ok {
		common.Log.Error("ICCBased not pointing to stream: %T", obj)
		return nil, fmt.Errorf("ICCBased stream invalid")
	}
	dict := stream.PdfObjectDictionary
	num, ok := dict.Get("N").(*core.PdfObjectInteger)
	if !ok {
		return nil, fmt.Errorf("ICCBased missing N from stream dict")
	}
	if *num != 1 && *num != 3 && *num != 4 {
		return nil, fmt.Errorf("ICCBased colorspace invalid N (not 1,3,4)")
	}
	iccBasedCS.N = int(*num)
	if alternate := dict.Get("Alternate"); alternate != nil {
		cs, err := NewPdfColorspaceFromPdfObject(alternate)
		if err != nil {
			return nil, err
		}
		iccBasedCS.Alternate = cs
	}
	if obj := dict.Get("Range"); obj != nil {
		obj = core.TraceToDirectObject(obj)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			return nil, fmt.Errorf("ICCBased Range not an array")
		}
		if arr.Len() != 2*iccBasedCS.N {
			return nil, fmt.Errorf("ICCBased Range wrong number of elements")
		}
		floats, err := arr.GetAsFloat64Slice()
		if err != nil {
			return nil, err
		}
		iccBasedCS.Range = floats
	} else {
		iccBasedCS.Range = make([]float64, 2*iccBasedCS.N)
		for i := 0; i < iccBasedCS.N; i++ {
			iccBasedCS.Range[2*i] = 0.0
			iccBasedCS.Range[2*i+1] = 1.0
		}
	}
	if metadata := dict.Get("Metadata"); metadata != nil {
		stream, ok := metadata.(*core.PdfObjectStream)
		if !ok {
			return nil, fmt.Errorf("ICCBased Metadata not a stream")
		}
		iccBasedCS.Metadata = stream
	}
	data, err := core.DecodeStream(stream)
	if err != nil {
		return nil, err
	}
	iccBasedCS.Data = data
	iccBasedCS.stream = stream
	return iccBasedCS, nil
}

// ToPdfObject returns an indirect object containing the signature field dictionary.
func (f *PdfFieldSignature) ToPdfObject() core.PdfObject {
	if f.PdfAnnotationWidget != nil {
		f.PdfAnnotationWidget.ToPdfObject()
	}
	f.PdfField.ToPdfObject()
	indirect := f.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("FT", core.MakeName("Sig"))
	dict.SetIfNotNil("Lock", f.Lock)
	dict.SetIfNotNil("SV", f.SV)
	if f.V != nil {
		dict.SetIfNotNil("V", f.V.ToPdfObject())
	}
	return indirect
}

// PdfActionImportData represents a importData action.
type PdfActionImportData struct {
	*PdfAction
	F *PdfFilespec
}

func newPdfShadingType7FromDict(dict *core.PdfObjectDictionary) (*PdfShadingType7, error) {
	shading7 := PdfShadingType7{}
	bitsPerCoordinate := dict.Get("BitsPerCoordinate")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerCoordinate")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok := bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerCoordinate not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading7.BitsPerCoordinate = num
	bitsPerCoordinate = dict.Get("BitsPerComponent")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerComponent")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok = bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerComponent not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading7.BitsPerComponent = num
	bitsPerCoordinate = dict.Get("BitsPerFlag")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerFlag")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok = bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerFlag not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading7.BitsPerFlag = num
	bitsPerCoordinate = dict.Get("Decode")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: Decode")
		return nil, ErrRequiredAttributeMissing
	}
	arr, ok := bitsPerCoordinate.(*core.PdfObjectArray)
	if !ok {
		common.Log.Debug("Decode not an array (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading7.Decode = arr
	if function := dict.Get("Function"); function != nil {
		shading7.Function = []PdfFunction{}
		if arr, ok := function.(*core.PdfObjectArray); ok {
			for _, obj := range arr.Elements() {
				function, err := newPdfFunctionFromPdfObject(obj)
				if err != nil {
					common.Log.Debug("Error parsing function: %v", err)
					return nil, err
				}
				shading7.Function = append(shading7.Function, function)
			}
		} else {
			fn, err := newPdfFunctionFromPdfObject(function)
			if err != nil {
				common.Log.Debug("Error parsing function: %v", err)
				return nil, err
			}
			shading7.Function = append(shading7.Function, fn)
		}
	}
	return &shading7, nil
}

func newPdfFontSimpleFromCommon(fc *fontCommon) *pdfFontSimple {
	return &pdfFontSimple{fontCommon: *fc}
}

// NewImageFromGoImage creates a new NRGBA32 OpenPDF Image from a golang Image.
// If `goimg` is grayscale (*goimage.Gray8) then calls NewGrayImageFromGoImage instead.
func (h DefaultImageHandler) NewImageFromGoImage(goimg image.Image) (*Image, error) {
	fromGoImage, err := imageutil.FromGoImage(goimg)
	if err != nil {
		return nil, err
	}
	img := imageFromImageBase(fromGoImage.Base())
	return &img, nil
}

func newPdfCIDFontType0FromDict(dict *core.PdfObjectDictionary, fc *fontCommon) (*pdfCIDFontType0, error) {
	if fc.subtype != "CIDFontType0" {
		common.Log.Debug("ERROR: Font SubType != CIDFontType0. font=%s", fc)
		return nil, core.ErrRangeError
	}
	cidFontType0 := newPdfCIDFontType0FromCommon(fc)
	cidSystemInfo, ok := core.GetDict(dict.Get("CIDSystemInfo"))
	if !ok {
		common.Log.Debug("ERROR: CIDSystemInfo (Required) missing. font=%s", fc)
		return nil, ErrRequiredAttributeMissing
	}
	cidFontType0.CIDSystemInfo = cidSystemInfo
	cidFontType0.DW = dict.Get("DW")
	cidFontType0.W = dict.Get("W")
	cidFontType0.DW2 = dict.Get("DW2")
	cidFontType0.W2 = dict.Get("W2")
	cidFontType0.defaultWidth = 1000.0
	if f, err := core.GetNumberAsFloat(cidFontType0.DW); err == nil {
		cidFontType0.defaultWidth = f
	}
	cidWidths, err := parseCIDWidths(cidFontType0.W)
	if err != nil {
		return nil, err
	}
	if cidWidths == nil {
		cidWidths = map[textencoding.CharCode]float64{}
	}
	cidFontType0.charWidths = cidWidths
	return cidFontType0, nil
}

func (c fontCommon) asPdfObjectDictionary(s string) *core.PdfObjectDictionary {
	if s != "" && c.subtype != "" && s != c.subtype {
		common.Log.Debug("ERROR: asPdfObjectDictionary. Overriding subtype to %#q %s", s, c)
	} else if s == "" && c.subtype == "" {
		common.Log.Debug("ERROR: asPdfObjectDictionary no subtype. font=%s", c)
	} else if c.subtype == "" {
		c.subtype = s
	}
	dict := core.MakeDict()
	dict.Set("Type", core.MakeName("Font"))
	dict.Set("BaseFont", core.MakeName(c.baseFont))
	dict.Set("Subtype", core.MakeName(c.subtype))
	if c.fontDescriptor != nil {
		dict.Set("FontDescriptor", c.fontDescriptor.ToPdfObject())
	}
	if c.toUnicode != nil {
		dict.Set("ToUnicode", c.toUnicode)
	} else if c.toUnicodeCMap != nil {
		stream, err := c.toUnicodeCMap.Stream()
		if err != nil {
			common.Log.Debug("WARN: could not get CMap stream. err=%v", err)
		} else {
			dict.Set("ToUnicode", stream)
		}
	}
	return dict
}

// ToPdfObject returns a PDF object representation of the outline destination.
func (d OutlineDest) ToPdfObject() core.PdfObject {
	if (d.PageObj == nil && d.Page < 0) || d.Mode == "" {
		return core.MakeNull()
	}
	array := core.MakeArray()
	if d.PageObj != nil {
		array.Append(d.PageObj)
	} else {
		array.Append(core.MakeInteger(d.Page))
	}
	array.Append(core.MakeName(d.Mode))
	switch d.Mode {
	case "Fit", "FitB":
	case "FitH", "FitBH":
		array.Append(core.MakeFloat(d.Y))
	case "FitV", "FitBV":
		array.Append(core.MakeFloat(d.X))
	case "XYZ":
		array.Append(core.MakeFloat(d.X))
		array.Append(core.MakeFloat(d.Y))
		array.Append(core.MakeFloat(d.Zoom))
	default:
		array.Set(1, core.MakeName("Fit"))
	}
	return array
}

func newPdfColorspaceDeviceNFromPdfObject(obj core.PdfObject) (*PdfColorspaceDeviceN, error) {
	deviceNCS := NewPdfColorspaceDeviceN()
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		deviceNCS.container = indirect
	}
	obj = core.TraceToDirectObject(obj)
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("deviceN CS: Invalid object")
	}
	if arr.Len() != 4 && arr.Len() != 5 {
		return nil, fmt.Errorf("deviceN CS: Incorrect array length")
	}
	obj = arr.Get(0)
	name, ok := obj.(*core.PdfObjectName)
	if !ok {
		return nil, fmt.Errorf("deviceN CS: invalid family name")
	}
	if *name != "DeviceN" {
		return nil, fmt.Errorf("deviceN CS: wrong family name")
	}
	obj = arr.Get(1)
	obj = core.TraceToDirectObject(obj)
	arr2, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("deviceN CS: Invalid names array")
	}
	deviceNCS.ColorantNames = arr2
	obj = arr.Get(2)
	cs, err := NewPdfColorspaceFromPdfObject(obj)
	if err != nil {
		return nil, err
	}
	deviceNCS.AlternateSpace = cs
	function, err := newPdfFunctionFromPdfObject(arr.Get(3))
	if err != nil {
		return nil, err
	}
	deviceNCS.TintTransform = function
	if arr.Len() == 5 {
		colorspaceDeviceNAttributes, err := newPdfColorspaceDeviceNAttributesFromPdfObject(arr.Get(4))
		if err != nil {
			return nil, err
		}
		deviceNCS.Attributes = colorspaceDeviceNAttributes
	}
	return deviceNCS, nil
}

// NewPdfFontFromTTF loads a TTF font and returns a PdfFont type that can be
// used in text styling functions.
// Uses a WinAnsiTextEncoder and loads only character codes 32-255.
// NOTE: For composite fonts such as used in symbolic languages, use NewCompositePdfFont.
func NewPdfFontFromTTF(r io.ReadSeeker) (*PdfFont, error) {
	const defaultFirstChar = textencoding.CharCode(32)
	const defaultLastChar = textencoding.CharCode(255)
	all, err := io.ReadAll(r)
	if err != nil {
		common.Log.Debug("ERROR: Unable to read font contents: %v", err)
		return nil, err
	}
	if fonts.IsTTC(all) {
		all, err = fonts.SfntFromTTC(all, 0)
		if err != nil {
			common.Log.Debug("ERROR: extracting font from TTC: %v", err)
			return nil, err
		}
	}
	ttfParse, err := fonts.TtfParse(bytes.NewReader(all))
	if err != nil {
		common.Log.Debug("ERROR: loading TTF font: %v", err)
		return nil, err
	}
	font := &pdfFontSimple{charWidths: make(map[textencoding.CharCode]float64), fontCommon: fontCommon{subtype: "TrueType"}}
	font.encoder = textencoding.NewWinAnsiEncoder()
	font.baseFont = ttfParse.PostScriptName
	font.FirstChar = core.MakeInteger(int64(defaultFirstChar))
	font.LastChar = core.MakeInteger(int64(defaultLastChar))
	f := 1000.0 / float64(ttfParse.UnitsPerEm)
	if len(ttfParse.Widths) <= 0 {
		return nil, errors.New("ERROR: Missing required attribute (Widths)")
	}
	v := f * float64(ttfParse.Widths[0])
	floats := make([]float64, 0, defaultLastChar-defaultFirstChar+1)
	for i := defaultFirstChar; i <= defaultLastChar; i++ {
		charcodeToRune, ok := font.Encoder().CharcodeToRune(i)
		if !ok {
			common.Log.Debug("Rune not found (code: %d)", i)
			floats = append(floats, v)
			continue
		}
		char, isChar := ttfParse.Chars[charcodeToRune]
		if !isChar {
			common.Log.Debug("Rune not in TTF Chars")
			floats = append(floats, v)
			continue
		}
		v := f * float64(ttfParse.Widths[char])
		floats = append(floats, v)
	}
	font.Widths = core.MakeIndirectObject(core.MakeArrayFromFloats(floats))
	if len(floats) < int(defaultLastChar-defaultFirstChar+1) {
		common.Log.Debug("ERROR: Invalid length of widths, %d < %d", len(floats), 255-32+1)
		return nil, core.ErrRangeError
	}
	for i := defaultFirstChar; i <= defaultLastChar; i++ {
		font.charWidths[i] = floats[i-defaultFirstChar]
	}
	font.Encoding = core.MakeName("WinAnsiEncoding")
	fontDescriptor := newPdfFontDescriptorFromTtf(&ttfParse, v, fontFlagNonsymbolic)
	stream, err := core.MakeStream(all, core.NewFlateEncoder())
	if err != nil {
		common.Log.Debug("ERROR: Unable to make stream: %v", err)
		return nil, err
	}
	stream.PdfObjectDictionary.Set("Length1", core.MakeInteger(int64(len(all))))
	fontDescriptor.FontFile2 = stream
	font.fontDescriptor = fontDescriptor
	font2 := &PdfFont{context: font}
	return font2, nil
}

func (t *trustEvaluator) vriCertificates(s string) []*x509.Certificate {
	if t.reader.DSS == nil {
		return nil
	}
	vri := t.reader.DSS.VRI[s]
	if vri == nil {
		return nil
	}
	return parseCertificates(vri.Cert)
}

// DecodeArray returns the component range values for the DeviceN colorspace.
// [0 1.0 0 1.0 ...] for each color component.
func (cs *PdfColorspaceDeviceN) DecodeArray() []float64 {
	var floats []float64
	for i := 0; i < cs.GetNumComponents(); i++ {
		floats = append(floats, 0.0, 1.0)
	}
	return floats
}

// AddCustomInfo adds a custom info into document info dictionary.
func (i *PdfInfo) AddCustomInfo(name string, value string) error {
	if i.dict == nil {
		i.dict = core.MakeDict()
	}
	if _, ok := infoDictKeys[name]; ok {
		return fmt.Errorf("cannot use standard info key %s as custom field key", name)
	}
	i.dict.SetIfNotNil(*core.MakeName(name), core.MakeString(value))
	return nil
}

// SetName sets the `Name` field of the signature.
func (s *PdfSignature) SetName(name string) { s.Name = core.MakeEncodedString(name, true) }

// PdfActionHide represents a hide action.
type PdfActionHide struct {
	*PdfAction
	T core.PdfObject
	H core.PdfObject
}

// HasFontByName checks whether a font is defined by the specified keyName.
func (r *PdfPageResources) HasFontByName(keyName core.PdfObjectName) bool {
	_, ok := r.GetFontByName(keyName)
	return ok
}

// NewPdfColorspaceDeviceGray returns a new grayscale colorspace.
func NewPdfColorspaceDeviceGray() *PdfColorspaceDeviceGray { return &PdfColorspaceDeviceGray{} }

// GetNumComponents returns the number of color components of the colorspace device.
// Returns 3 for a CalRGB device.
func (cs *PdfColorspaceCalRGB) GetNumComponents() int { return 3 }

// PdfAnnotationTrapNet represents TrapNet annotations.
// (Section 12.5.6.21).
type PdfAnnotationTrapNet struct{ *PdfAnnotation }

func (r *PdfReader) newPdfAnnotationMovieFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationMovie, error) {
	movie := PdfAnnotationMovie{}
	movie.T = dict.Get("T")
	movie.Movie = dict.Get("Movie")
	movie.A = dict.Get("A")
	return &movie, nil
}
func (f *pdfFontType3) baseFields() *fontCommon { return &f.fontCommon }

// CanPassThroughJPEG reports whether the image still holds the original JPEG
// bytes of an 8-bit grayscale or RGB source, which a DCT encoder (including
// the default when no encoder is set) embeds unchanged instead of decoding
// and re-encoding.
func (img *Image) CanPassThroughJPEG() bool { return img.canPassThroughJPEG() }

func hasFieldKeys(dict *core.PdfObjectDictionary) bool {
	for _, name := range dict.Keys() {
		if _, ok := fieldDictKeys[name.String()]; ok {
			return true
		}
	}
	return false
}

func isWhitespaceGap(parser *core.PdfParser, n, count int64) bool {
	if count-n <= 0 || count-n > maxGapBytes {
		return false
	}
	bytesAt, err := parser.ReadBytesAt(n, count-n)
	if err != nil {
		return false
	}
	for _, b := range bytesAt {
		if !core.IsWhiteSpace(b) {
			return false
		}
	}
	return true
}

// NewCustomPdfOutputIntent creates a new custom PdfOutputIntent.
func NewCustomPdfOutputIntent(outputCondition, outputConditionIdentifier, info string, destOutputProfile []byte, colorComponents int) *PdfOutputIntent {
	return &PdfOutputIntent{Type: "OutputIntent", OutputCondition: outputCondition, OutputConditionIdentifier: outputConditionIdentifier, Info: info, DestOutputProfile: destOutputProfile, primitive: core.MakeDict(), ColorComponents: colorComponents}
}

// ColorFromPdfObjects returns a new PdfColor based on the input slice of color
// component PDF objects.
func (cs *PdfColorspaceICCBased) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if cs.Alternate == nil {
		if cs.N == 1 {
			deviceGrayCS := NewPdfColorspaceDeviceGray()
			return deviceGrayCS.ColorFromPdfObjects(objects)
		} else if cs.N == 3 {
			deviceRGBCS := NewPdfColorspaceDeviceRGB()
			return deviceRGBCS.ColorFromPdfObjects(objects)
		} else if cs.N == 4 {
			deviceCMYKCS := NewPdfColorspaceDeviceCMYK()
			return deviceCMYKCS.ColorFromPdfObjects(objects)
		} else {
			return nil, errors.New("ICC Based colorspace missing alternative")
		}
	}
	return cs.Alternate.ColorFromPdfObjects(objects)
}

// SetSubtype sets the Subtype S for given PdfOutputIntent.
func (o *PdfOutputIntent) SetSubtype(subtype PdfOutputIntentType) error {
	if !subtype.IsValid() {
		return errors.New("provided invalid OutputIntent SubType")
	}
	o.S = subtype
	return nil
}
func (f *pdfCIDFontType2) getFontDescriptor() *PdfFontDescriptor { return f.fontDescriptor }

// GetNumComponents returns the number of color components.
func (cs *PdfColorspaceICCBased) GetNumComponents() int { return cs.N }

// Quadding is the text alignment value derived from a field's /Q entry.
type Quadding int

// NewEmbeddedFile constructs a new EmbeddedFile object from supplied file.
// The file type of the file would be detected automatically.
func NewEmbeddedFile(path string) (*EmbeddedFile, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	match, err := filetype.Match(file)
	if err != nil {
		common.Log.Debug("Failed to detect file type: %v", err)
	}
	data := md5.Sum(file)
	file2 := &EmbeddedFile{Name: filepath.Base(path), Content: file, FileType: match.MIME.Value, Hash: hex.EncodeToString(data[:])}
	return file2, nil
}

// GetNumComponents returns the number of color components of the underlying
// colorspace device. Returns 0 if there is no underlying colorspace (e.g. an
// uncolored pattern).
func (cs *PdfColorspaceSpecialPattern) GetNumComponents() int {
	if cs.UnderlyingCS == nil {
		return 0
	}
	return cs.UnderlyingCS.GetNumComponents()
}

func parseDefaultAppearance(s string) (s2 string, f float64, col PdfColor) {
	if s == "" {
		return s2, f, col
	}
	var strs []string
	fn := func(n int) ([]string, bool) {
		if len(strs) < n {
			return nil, false
		}
		strs2 := append([]string(nil), strs[len(strs)-n:]...)
		strs = strs[:len(strs)-n]
		return strs2, true
	}
	fn2 := func(strs []string) ([]float64, bool) {
		floats := make([]float64, len(strs))
		for i, str := range strs {
			f, err := strconv.ParseFloat(str, 64)
			if err != nil {
				return nil, false
			}
			floats[i] = f
		}
		return floats, true
	}
	for _, field := range strings.Fields(s) {
		switch field {
		case "Tf":
			strs, ok := fn(2)
			if !ok {
				continue
			}
			if strings.HasPrefix(strs[0], "/") {
				s2 = strs[0][1:]
			}
			if v, err := strconv.ParseFloat(strs[1], 64); err == nil {
				f = v
			}
		case "g", "G":
			strs, ok := fn(1)
			if !ok {
				continue
			}
			floats, ok := fn2(strs)
			if !ok {
				continue
			}
			if col2, err := NewPdfColorspaceDeviceGray().ColorFromFloats(floats); err == nil {
				col = col2
			}
		case "rg", "RG":
			strs, ok := fn(3)
			if !ok {
				continue
			}
			floats, ok := fn2(strs)
			if !ok {
				continue
			}
			if col2, err := NewPdfColorspaceDeviceRGB().ColorFromFloats(floats); err == nil {
				col = col2
			}
		case "k", "K":
			strs, ok := fn(4)
			if !ok {
				continue
			}
			floats, ok := fn2(strs)
			if !ok {
				continue
			}
			if col2, err := NewPdfColorspaceDeviceCMYK().ColorFromFloats(floats); err == nil {
				col = col2
			}
		default:
			strs = append(strs, field)
		}
	}
	return s2, f, col
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationStamp) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Stamp"))
	dict.SetIfNotNil("Name", a.Name)
	return indirect
}

const (
	TrappedUnknown PdfInfoTrapped = "Unknown"
	TrappedTrue    PdfInfoTrapped = "True"
	TrappedFalse   PdfInfoTrapped = "False"
)

func newPdfCIDFontType2FromTtf(ttf fonts.TtfType, data []byte, otf *fonts.OtfType) (PdfFont, error) {
	font := &pdfCIDFontType2{fontCommon: fontCommon{subtype: "CIDFontType2"}, CIDToGIDMap: core.MakeName("Identity")}
	if len(ttf.Widths) <= 0 {
		return PdfFont{}, errors.New("ERROR: Missing required attribute (Widths)")
	}
	f := 1000.0 / float64(ttf.UnitsPerEm)
	v := f * float64(ttf.Widths[0])
	table, numByV := scaleGlyphWidths(ttf.Chars, ttf.Widths, ttf.UnitsPerEm)
	font.runeToGID = table
	font.DW = core.MakeInteger(int64(v))
	v2 := fonts.GID(len(ttf.Widths))
	widthsArray := buildWidthsArray(numByV, uint16(v2))
	font.W = core.MakeIndirectObject(widthsArray)
	dict := core.MakeDict()
	dict.Set("Ordering", core.MakeString("Identity"))
	dict.Set("Registry", core.MakeString("Adobe"))
	dict.Set("Supplement", core.MakeInteger(0))
	font.CIDSystemInfo = dict
	fontDescriptor := newPdfFontDescriptorFromTtf(&ttf, v, fontFlagSymbolic)
	stream, err := core.MakeStream(data, core.NewFlateEncoder())
	if err != nil {
		common.Log.Debug("ERROR: Unable to make stream: %v", err)
		return PdfFont{}, err
	}
	stream.PdfObjectDictionary.Set("Length1", core.MakeInteger(int64(len(data))))
	fontDescriptor.FontFile2 = stream
	font.baseFont = ttf.PostScriptName
	font.fontDescriptor = fontDescriptor
	var enc textencoding.TextEncoder
	if otf != nil {
		font.SetOtfType(otf)
		enc = otf.NewEncoder()
	} else {
		enc = ttf.NewEncoder()
	}
	font2 := pdfFontType0{fontCommon: fontCommon{subtype: "Type0", baseFont: ttf.PostScriptName}, DescendantFont: &PdfFont{context: font}, Encoding: core.MakeName("Identity-H"), encoder: enc}
	if len(ttf.Chars) > 0 {
		textByCode := make(map[cmap.CharCode]string, len(ttf.Chars))
		for n, char := range ttf.Chars {
			code := cmap.CharCode(char)
			s := string(n)
			if s2, ok := textByCode[code]; !ok || (ok && len(s2) > 0 && s2 > s) {
				textByCode[code] = s
			}
		}
		font2.toUnicodeCMap = cmap.NewToUnicodeCMap(textByCode)
	}
	font3 := PdfFont{context: &font2}
	return font3, nil
}

func (r *PdfReader) newPdfAnnotationLinkFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationLink, error) {
	link := PdfAnnotationLink{}
	link.A = dict.Get("A")
	link.Dest = dict.Get("Dest")
	link.H = dict.Get("H")
	link.PA = dict.Get("PA")
	link.QuadPoints = dict.Get("QuadPoints")
	link.BS = dict.Get("BS")
	return &link, nil
}

// B returns the value of the B component of the color.
func (c *PdfColorCalRGB) B() float64 { return c[1] }

// PdfShadingType2 is an Axial shading.
type PdfShadingType2 struct {
	*PdfShading
	Coords   *core.PdfObjectArray
	Domain   *core.PdfObjectArray
	Function []PdfFunction
	Extend   *core.PdfObjectArray
}

// NewPdfColorspaceICCBased returns a new ICCBased colorspace object.
func NewPdfColorspaceICCBased(N int) (*PdfColorspaceICCBased, error) {
	iccBasedCS := &PdfColorspaceICCBased{}
	if N != 1 && N != 3 && N != 4 {
		return nil, fmt.Errorf("invalid N (1/3/4)")
	}
	iccBasedCS.N = N
	return iccBasedCS, nil
}

// PdfStructureNamespace represents a namespace dictionary as defined in
// ISO 32000-2 14.7.4.2, table 356.
type PdfStructureNamespace struct {
	uri             string
	schema          core.PdfObject
	roleMapNS       *core.PdfObjectDictionary
	indirect        *core.PdfIndirectObject
	roleMapIndirect *core.PdfIndirectObject
}

func (r *PdfReader) newPdfAnnotationWidgetFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationWidget, error) {
	widget := PdfAnnotationWidget{}
	widget.H = dict.Get("H")
	widget.MK = dict.Get("MK")
	widget.A = dict.Get("A")
	widget.AA = dict.Get("AA")
	widget.BS = dict.Get("BS")
	widget.Parent = dict.Get("Parent")
	return &widget, nil
}

// GetNumComponents returns the number of color components (4 for CMYK32).
func (c *PdfColorDeviceCMYK) GetNumComponents() int { return 4 }

func getASCIISections(data []byte) (s, s2 string, err error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("getASCIISections: %d ", len(data))
	}
	index := type1EncodingDictRegexp.FindIndex(data)
	if index == nil {
		common.Log.Debug("ERROR: getASCIISections. No dict.")
		return "", "", core.ErrTypeError
	}
	n := index[1]
	count := strings.Index(string(data[n:]), type1EncodingMarker)
	if count < 0 {
		s = string(data[n:])
		return s, "", nil
	}
	num := n + count
	s = string(data[n:num])
	size := num
	count = strings.Index(string(data[size:]), type1ReadonlyDefMarker)
	if count < 0 {
		common.Log.Debug("ERROR: getASCIISections. err=%v", err)
		return "", "", core.ErrTypeError
	}
	length := size + count
	s2 = string(data[size:length])
	return s, s2, nil
}

// GetContainingPdfObject gets the primitive used to parse the color space.
func (cs *PdfColorspaceICCBased) GetContainingPdfObject() core.PdfObject { return cs.stream }

func newPdfShadingType6FromDict(dict *core.PdfObjectDictionary) (*PdfShadingType6, error) {
	shading6 := PdfShadingType6{}
	bitsPerCoordinate := dict.Get("BitsPerCoordinate")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerCoordinate")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok := bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerCoordinate not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading6.BitsPerCoordinate = num
	bitsPerCoordinate = dict.Get("BitsPerComponent")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerComponent")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok = bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerComponent not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading6.BitsPerComponent = num
	bitsPerCoordinate = dict.Get("BitsPerFlag")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerFlag")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok = bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerFlag not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading6.BitsPerFlag = num
	bitsPerCoordinate = dict.Get("Decode")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: Decode")
		return nil, ErrRequiredAttributeMissing
	}
	arr, ok := bitsPerCoordinate.(*core.PdfObjectArray)
	if !ok {
		common.Log.Debug("Decode not an array (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading6.Decode = arr
	if function := dict.Get("Function"); function != nil {
		shading6.Function = []PdfFunction{}
		if arr, ok := function.(*core.PdfObjectArray); ok {
			for _, obj := range arr.Elements() {
				function, err := newPdfFunctionFromPdfObject(obj)
				if err != nil {
					common.Log.Debug("Error parsing function: %v", err)
					return nil, err
				}
				shading6.Function = append(shading6.Function, function)
			}
		} else {
			fn, err := newPdfFunctionFromPdfObject(function)
			if err != nil {
				common.Log.Debug("Error parsing function: %v", err)
				return nil, err
			}
			shading6.Function = append(shading6.Function, fn)
		}
	}
	return &shading6, nil
}

// PdfAnnotation represents an annotation in PDF (section 12.5 p. 389).
type PdfAnnotation struct {
	context      PdfModel
	Rect         core.PdfObject
	Contents     core.PdfObject
	P            core.PdfObject
	NM           core.PdfObject
	M            core.PdfObject
	F            core.PdfObject
	AP           core.PdfObject
	AS           core.PdfObject
	Border       core.PdfObject
	C            core.PdfObject
	StructParent core.PdfObject
	OC           core.PdfObject
	primitive    *core.PdfIndirectObject
}

func newPdfFieldButtonFromDict(dict *core.PdfObjectDictionary) (*PdfFieldButton, error) {
	buttonField := &PdfFieldButton{}
	buttonField.PdfField = NewPdfField()
	buttonField.SetContext(buttonField)
	buttonField.Opt, _ = core.GetArray(dict.Get("Opt"))
	widget := NewPdfAnnotationWidget()
	widget.A, _ = core.GetDict(dict.Get("A"))
	widget.AP, _ = core.GetDict(dict.Get("AP"))
	widget.SetContext(buttonField)
	buttonField.Annotations = append(buttonField.Annotations, widget)
	return buttonField, nil
}

// ToPdfObject converts the structure tree root to a PDF object.
func (r *StructTreeRoot) ToPdfObject() core.PdfObject {
	indirect := r.container
	if indirect == nil {
		indirect = &core.PdfIndirectObject{}
		indirect.PdfObject = core.MakeDict()
	}
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	var obj core.PdfObject
	if len(r.K) == 1 {
		obj = core.MakeIndirectObject(r.K[0].ToPdfObject())
	} else {
		array := core.MakeArray()
		for _, K := range r.K {
			array.Append(core.MakeIndirectObject(K.ToPdfObject()))
		}
		obj = array
	}
	var (
		objs      = []core.PdfObject{}
		objsByObj = map[core.PdfObject][]core.PdfObject{}
		objByKey  = map[string]core.PdfObject{}
	)
	collectStructureElements(obj, indirect, objsByObj, objByKey, &objs)
	dict.Set("Type", core.MakeName("StructTreeRoot"))
	dict.Set("K", obj)
	if r.IDTree != nil {
		dict.Set("IDTree", core.MakeIndirectObject(r.IDTree.ToPdfObject()))
	} else if len(objByKey) > 0 {
		array := core.MakeArray()
		strs := make([]string, 0, len(objByKey))
		for s := range objByKey {
			strs = append(strs, s)
		}
		sort.Strings(strs)
		for _, str := range strs {
			array.Append(core.MakeString(str))
			array.Append(objByKey[str])
		}
		r.IDTree = &IDTree{Names: array, Limits: core.MakeArray(core.MakeString(strs[0]), core.MakeString(strs[len(strs)-1]))}
		dict.Set("IDTree", core.MakeIndirectObject(r.IDTree.ToPdfObject()))
	}
	if r.ParentTree != nil {
		dict.Set("ParentTree", core.MakeIndirectObject(r.ParentTree))
	} else if len(objsByObj) > 0 || len(objs) > 0 {
		array := core.MakeArray()
		n := 0
		for obj, objs := range objsByObj {
			arr := core.MakeArray()
			for _, obj := range objs {
				arr.Append(obj)
			}
			integer := core.MakeInteger(int64(n))
			array.Append(integer)
			array.Append(core.MakeIndirectObject(arr))
			if indirect, ok := core.GetIndirect(obj); ok {
				if dict, ok := core.GetDict(indirect.PdfObject); ok {
					structParents := dict.Get("StructParents")
					if structParents != nil {
						if count, ok := core.GetIntVal(structParents); ok {
							if n < count {
								dict.Set("StructParents", integer)
							}
						}
					} else {
						dict.Set("StructParents", integer)
					}
				}
			}
			n++
		}
		fn := func(obj core.PdfObject, dict *core.PdfObjectDictionary) bool {
			dict2 := dict
			if obj := dict.Get("Type"); obj.String() == "OBJR" {
				if obj := dict.Get("Obj"); obj != nil {
					if dict3, ok := core.GetDict(obj); ok {
						dict2 = dict3
					}
				}
			}
			obj2 := dict2.Get("Type")
			if obj2 != nil && obj2.String() == "Annot" {
				integer := core.MakeInteger(int64(n))
				dict2.Set("StructParent", integer)
				array.Append(integer)
				array.Append(obj)
				n++
				return true
			}
			return false
		}
		for _, obj := range objs {
			if dict, ok := core.GetDict(obj); ok {
				if s := dict.Get("S"); s != nil {
					if s2, ok := core.GetNameVal(s); ok && (s2 == string(StructureTypeLink) || s2 == string(StructureTypeAnnot)) {
						if k := dict.Get("K"); k != nil {
							if kArr, ok := core.GetArray(k); ok {
								for _, obj2 := range kArr.Elements() {
									if dict, ok := core.GetDict(obj2); ok {
										fn(obj, dict)
									}
								}
							} else if kDict, ok := core.GetDict(k); ok {
								fn(obj, kDict)
							}
						}
					}
				}
			}
		}
		r.ParentTree = core.MakeDict()
		r.ParentTree.Set("Nums", array)
		r.ParentTreeNextKey = int64(n)
		dict.Set("ParentTree", core.MakeIndirectObject(r.ParentTree))
	}
	dict.Set("ParentTreeNextKey", core.MakeInteger(r.ParentTreeNextKey))
	if r.RoleMap != nil {
		dict.Set("RoleMap", r.RoleMap)
	}
	if r.ClassMap != nil {
		dict.Set("ClassMap", r.ClassMap)
	}
	array := core.MakeArray()
	for _, namespace := range r.Namespaces {
		if namespace == nil {
			continue
		}
		array.Append(namespace.ToPdfObject())
	}
	if array.Len() > 0 {
		dict.Set("Namespaces", array)
	} else {
		dict.Remove("Namespaces")
	}
	return indirect
}

// NewEmbeddedFileFromObject construct a new EmbeddedFile from supplied object.
func NewEmbeddedFileFromObject(obj core.PdfObject) (*EmbeddedFile, error) {
	obj2 := core.TraceToDirectObject(obj)
	dict, ok := obj2.(*core.PdfObjectDictionary)
	if !ok {
		return nil, errors.New("invalid object")
	}
	f := core.TraceToDirectObject(dict.Get("F"))
	if f == nil {
		return nil, errors.New("invalid object structure")
	}
	stream, ok := f.(*core.PdfObjectStream)
	if !ok {
		return nil, errors.New("invalid stream")
	}
	dict2 := stream.PdfObjectDictionary
	params := dict2.Get("Params")
	if params == nil {
		return nil, errors.New("params object not available")
	}
	dict3, ok := params.(*core.PdfObjectDictionary)
	if !ok {
		return nil, errors.New("invalid Params object")
	}
	s := ""
	checkSum := dict3.Get("CheckSum")
	if checkSum != nil {
		s = checkSum.(*core.PdfObjectString).Str()
	}
	data, err := core.DecodeStream(stream)
	if err != nil {
		return nil, err
	}
	file := &EmbeddedFile{Content: data, Hash: s}
	return file, nil
}

// SetPatternByName sets a pattern resource specified by keyName.
func (r *PdfPageResources) SetPatternByName(keyName core.PdfObjectName, pattern core.PdfObject) error {
	if r.Pattern == nil {
		r.Pattern = core.MakeDict()
	}
	patternDict, ok := core.GetDict(r.Pattern)
	if !ok {
		return core.ErrTypeError
	}
	patternDict.Set(keyName, pattern)
	return nil
}

func debugLogObject(obj core.PdfObject) {
	common.Log.Debug("obj: %T %s", obj, obj.String())
	if stream, ok := obj.(*core.PdfObjectStream); ok {
		data, err := core.DecodeStream(stream)
		if err != nil {
			common.Log.Debug("Error: %v", err)
			return
		}
		common.Log.Debug("Decoded: %s", data)
	} else if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		common.Log.Debug("%T %v", indirect.PdfObject, indirect.PdfObject)
		common.Log.Debug("%s", indirect.PdfObject.String())
	}
}

// ToPdfOutline returns a low level PdfOutline object, based on the current
// instance.
func (o *Outline) ToPdfOutline() *PdfOutline {
	outline := NewPdfOutline()
	var items []*PdfOutlineItem
	var n int64
	var item *PdfOutlineItem
	for _, entry := range o.Entries {
		item2, count := entry.ToPdfOutlineItem()
		item2.Parent = &outline.PdfOutlineTreeNode
		if item != nil {
			item.Next = &item2.PdfOutlineTreeNode
			item2.Prev = &item.PdfOutlineTreeNode
		}
		items = append(items, item2)
		n += count
		item = item2
	}
	count := int64(len(items))
	n += count
	if count > 0 {
		outline.First = &items[0].PdfOutlineTreeNode
		outline.Last = &items[count-1].PdfOutlineTreeNode
		outline.Count = &n
	}
	return outline
}

// GetPageDict converts the Page to a PDF object dictionary.
func (p *PdfPage) GetPageDict() *core.PdfObjectDictionary {
	pageDict := p.pageDict
	pageDict.Clear()
	pageDict.Set("Type", core.MakeName("Page"))
	pageDict.Set("Parent", p.Parent)
	if p.LastModified != nil {
		pageDict.Set("LastModified", p.LastModified.ToPdfObject())
	}
	if p.Resources != nil {
		pageDict.Set("Resources", p.Resources.ToPdfObject())
	}
	if p.CropBox != nil {
		pageDict.Set("CropBox", p.CropBox.ToPdfObject())
	}
	if p.MediaBox != nil {
		pageDict.Set("MediaBox", p.MediaBox.ToPdfObject())
	}
	if p.BleedBox != nil {
		pageDict.Set("BleedBox", p.BleedBox.ToPdfObject())
	}
	if p.TrimBox != nil {
		pageDict.Set("TrimBox", p.TrimBox.ToPdfObject())
	}
	if p.ArtBox != nil {
		pageDict.Set("ArtBox", p.ArtBox.ToPdfObject())
	}
	pageDict.SetIfNotNil("BoxColorInfo", p.BoxColorInfo)
	pageDict.SetIfNotNil("Contents", p.Contents)
	if p.Rotate != nil {
		pageDict.Set("Rotate", core.MakeInteger(*p.Rotate))
	}
	pageDict.SetIfNotNil("Group", p.Group)
	pageDict.SetIfNotNil("Thumb", p.Thumb)
	pageDict.SetIfNotNil("B", p.B)
	pageDict.SetIfNotNil("Dur", p.Dur)
	pageDict.SetIfNotNil("Trans", p.Trans)
	pageDict.SetIfNotNil("AA", p.AA)
	pageDict.SetIfNotNil("Metadata", p.Metadata)
	pageDict.SetIfNotNil("PieceInfo", p.PieceInfo)
	pageDict.SetIfNotNil("StructParents", p.StructParents)
	pageDict.SetIfNotNil("ID", p.ID)
	pageDict.SetIfNotNil("PZ", p.PZ)
	pageDict.SetIfNotNil("SeparationInfo", p.SeparationInfo)
	pageDict.SetIfNotNil("Tabs", p.Tabs)
	pageDict.SetIfNotNil("TemplateInstantiated", p.TemplateInstantiated)
	pageDict.SetIfNotNil("PresSteps", p.PresSteps)
	pageDict.SetIfNotNil("UserUnit", p.UserUnit)
	pageDict.SetIfNotNil("VP", p.VP)
	if annotationsSnapshot := p.annotationsSnapshot(); annotationsSnapshot != nil {
		array := core.MakeArray()
		for _, annot := range annotationsSnapshot {
			if model := annot.GetContext(); model != nil {
				array.Append(model.ToPdfObject())
			} else {
				array.Append(annot.ToPdfObject())
			}
		}
		if array.Len() > 0 {
			pageDict.Set("Annots", array)
		}
	} else if p.Annots != nil {
		pageDict.SetIfNotNil("Annots", p.Annots)
	}
	return pageDict
}

func (a *PdfAppender) updateObjectsDeep(obj core.PdfObject, set map[core.PdfObject]struct{}) {
	if set == nil {
		set = map[core.PdfObject]struct{}{}
	}
	if _, ok := set[obj]; ok || obj == nil {
		return
	}
	set[obj] = struct{}{}
	err := core.ResolveReferencesDeep(obj, a.traversed)
	if err != nil {
		common.Log.Debug("ERROR: %v", err)
	}
	switch v := obj.(type) {
	case *core.PdfIndirectObject:
		switch {
		case v.GetParser() == a.roReader.parser:
			return
		case v.GetParser() == a.Reader.parser:
			indirectObjectByNumber, _ := a.roReader.GetIndirectObjectByNumber(int(v.ObjectNumber))
			indirect, ok := indirectObjectByNumber.(*core.PdfIndirectObject)
			if ok && indirect != nil {
				s := string(v.PdfObject.Write())
				if indirect.PdfObject != v.PdfObject && string(indirect.PdfObject.Write()) != s {
					if strings.Contains(s, "/Sig") && strings.Contains(s, "/Subtype") {
						return
					}
					a.addNewObject(obj)
					a.objNumMap[obj] = v.ObjectNumber
				}
			}
		default:
			a.addNewObject(obj)
		}
		a.updateObjectsDeep(v.PdfObject, set)
	case *core.PdfObjectArray:
		for _, obj := range v.Elements() {
			a.updateObjectsDeep(obj, set)
		}
	case *core.PdfObjectDictionary:
		for _, name := range v.Keys() {
			a.updateObjectsDeep(v.Get(name), set)
		}
	case *core.PdfObjectStreams:
		if v.GetParser() != a.roReader.parser {
			for _, obj := range v.Elements() {
				a.updateObjectsDeep(obj, set)
			}
		}
	case *core.PdfObjectStream:
		switch {
		case v.GetParser() == a.roReader.parser:
			return
		case v.GetParser() == a.Reader.parser:
			if lookupByReference, err := a.roReader.parser.LookupByReference(v.PdfObjectReference); err == nil {
				var ok bool
				if lookupByReferenceStream, isLookupByReferenceStream := core.GetStream(lookupByReference); isLookupByReferenceStream && bytes.Equal(lookupByReferenceStream.Stream, v.Stream) {
					ok = true
				}
				if lookupByReferenceDict, isLookupByReferenceDict := core.GetDict(lookupByReference); ok && isLookupByReferenceDict {
					ok = string(lookupByReferenceDict.Write()) == string(v.PdfObjectDictionary.Write())
				}
				if ok {
					return
				}
			}
			if v.ObjectNumber != 0 {
				a.objNumMap[obj] = v.ObjectNumber
			}
		default:
			if _, ok := a.hasNewObject[obj]; !ok {
				a.addNewObject(obj)
			}
		}
		a.updateObjectsDeep(v.PdfObjectDictionary, set)
	}
}

func newPdfColorspaceLabFromPdfObject(obj core.PdfObject) (*PdfColorspaceLab, error) {
	labCS := NewPdfColorspaceLab()
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		labCS.container = indirect
	}
	obj = core.TraceToDirectObject(obj)
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("type error")
	}
	if arr.Len() != 2 {
		return nil, fmt.Errorf("invalid CalRGB colorspace")
	}
	obj = core.TraceToDirectObject(arr.Get(0))
	name, ok := obj.(*core.PdfObjectName)
	if !ok {
		return nil, fmt.Errorf("lab name not a Name object")
	}
	if *name != "Lab" {
		return nil, fmt.Errorf("not a Lab colorspace")
	}
	obj = core.TraceToDirectObject(arr.Get(1))
	dict, ok := obj.(*core.PdfObjectDictionary)
	if !ok {
		return nil, fmt.Errorf("colorspace dictionary missing or invalid")
	}
	obj = dict.Get("WhitePoint")
	obj = core.TraceToDirectObject(obj)
	arr2, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("lab Invalid WhitePoint")
	}
	if arr2.Len() != 3 {
		return nil, fmt.Errorf("lab: Invalid WhitePoint array")
	}
	floats, err := arr2.GetAsFloat64Slice()
	if err != nil {
		return nil, err
	}
	labCS.WhitePoint = floats
	obj = dict.Get("BlackPoint")
	if obj != nil {
		obj = core.TraceToDirectObject(obj)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			return nil, fmt.Errorf("lab: Invalid BlackPoint")
		}
		if arr.Len() != 3 {
			return nil, fmt.Errorf("lab: Invalid BlackPoint array")
		}
		floats, err := arr.GetAsFloat64Slice()
		if err != nil {
			return nil, err
		}
		labCS.BlackPoint = floats
	}
	obj = dict.Get("Range")
	if obj != nil {
		obj = core.TraceToDirectObject(obj)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			common.Log.Error("Range type error")
			return nil, fmt.Errorf("lab: Type error")
		}
		if arr.Len() != 4 {
			common.Log.Error("Range range error")
			return nil, fmt.Errorf("lab: Range error")
		}
		floats, err := arr.GetAsFloat64Slice()
		if err != nil {
			return nil, err
		}
		labCS.Range = floats
	}
	return labCS, nil
}

// CharcodeBytesToUnicode converts PDF character codes `data` to a Go unicode string.
//
// 9.10 Extraction of Text Content (page 292)
// The process of finding glyph descriptions in OpenType fonts by a conforming reader shall be the following:
//   - For Type 1 fonts using “CFF” tables, the process shall be as described in 9.6.6.2, "Encodings
//     for Type 1 Fonts".
//   - For TrueType fonts using “glyf” tables, the process shall be as described in 9.6.6.4,
//     "Encodings for TrueType Fonts". Since this process sometimes produces ambiguous results,
//     conforming writers, instead of using a simple font, shall use a Type 0 font with an Identity-H
//     encoding and use the glyph indices as character codes, as described following Table 118.
func (f *PdfFont) CharcodeBytesToUnicode(data []byte) (string, int, int) {
	charcodesToStrings, _, n := f.CharcodesToStrings(f.BytesToCharcodes(data), "")
	runes := []rune(strings.Join(charcodesToStrings, ""))
	expandLigatures := textencoding.ExpandLigatures(runes)
	return expandLigatures, utf8.RuneCountInString(expandLigatures), n
}

// NewPdfColorspaceDeviceCMYK returns a new CMYK32 colorspace object.
func NewPdfColorspaceDeviceCMYK() *PdfColorspaceDeviceCMYK { return &PdfColorspaceDeviceCMYK{} }

// ArtifactSubtype represents the subtype of an artifact in a PDF document.
type ArtifactSubtype string

// FileRelationship represents a attachment file relationship type.
type (
	FileRelationship int
	ocspKey          struct{ responseHash, certHash, issuerHash [32]byte }
)

// AcroFormRepairOptions contains options for rebuilding the AcroForm.
type AcroFormRepairOptions struct{}

func (t *trustEvaluator) checkChainRevocation(trustChain []*x509.Certificate, validatedAt time.Time, ok bool, evidence revocationEvidence) chainRevocation {
	revocation := chainRevocation{status: RevocationStatusGood}
	for i := 0; i+1 < len(trustChain); i++ {
		cert, cert2 := trustChain[i], trustChain[i+1]
		checkCertRevocation := t.checkCertRevocation(cert, cert2, validatedAt, ok, evidence)
		if !checkCertRevocation.settled() && t.opts.Revocation == RevocationCheckOnline {
			var strs []string
			checkCertRevocation, strs = t.checkOnline(checkCertRevocation, cert, cert2, validatedAt, ok, evidence)
			revocation.warnings = append(revocation.warnings, strs...)
		}
		if i == 0 {
			revocation.leaf = checkCertRevocation
		}
		switch checkCertRevocation.status {
		case RevocationStatusRevoked:
			revocation.status = RevocationStatusRevoked
			revocation.revokedCert = cert
			revocation.revokedAt = checkCertRevocation.revokedAt
			return revocation
		case RevocationStatusUnknown:
			revocation.status = RevocationStatusUnknown
		}
	}
	return revocation
}

func newPdfReaderTracked(r io.ReaderAt, size int64, opts *ReaderOpts, ok, found bool, s string) (*PdfReader, error) {
	readerCore, err := newPdfReaderCore(r, opts, ok, found, s)
	if err != nil {
		return nil, err
	}
	opts = readerCore.opts
	var parser *core.PdfParser
	if !readerCore.complianceMode {
		opts2 := core.ParserOpts{RelaxedMode: opts.RelaxedMode, AutoRepairXrefTable: opts.ParserAutoRepair, MaxDecodedStreamSize: opts.MaxDecodedStreamSize, MaxDecodedTotalSize: opts.MaxDecodedTotalSize}
		parser, err = core.NewParserWithOptsAt(r, size, opts2)
	} else {
		parser, err = core.NewCompliancePdfParserAt(r, size)
		if err == nil && (opts.MaxDecodedStreamSize != 0 || opts.MaxDecodedTotalSize != 0) {
			parser.Opts = &core.ParserOpts{MaxDecodedStreamSize: opts.MaxDecodedStreamSize, MaxDecodedTotalSize: opts.MaxDecodedTotalSize}
		}
	}
	if err != nil {
		return nil, err
	}
	return readerCore.loadFromParser(parser)
}

func (p *PdfPage) getAnnotationsLocked() ([]*PdfAnnotation, error) {
	if p.annotations != nil {
		return p.annotations, nil
	}
	if p.Annots == nil {
		p.annotations = []*PdfAnnotation{}
		return nil, nil
	}
	if p.reader == nil {
		p.annotations = []*PdfAnnotation{}
		return nil, nil
	}
	annotations, err := p.reader.loadAnnotations(p.Annots)
	if err != nil {
		return nil, err
	}
	if annotations == nil {
		p.annotations = []*PdfAnnotation{}
	}
	p.annotations = annotations
	return p.annotations, nil
}

var _ pdfFont = (*pdfFontType0)(nil)

// Colorspace returns a best-effort, parameterless CalGray colorspace. The
// authoritative parameterized colorspace is tracked by the graphics state.
func (c *PdfColorCalGray) Colorspace() PdfColorspace { return NewPdfColorspaceCalGray() }

func (w *PdfWriter) addObjects(obj core.PdfObject) error {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Adding objects!")
	}
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Indirect")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("- %s (%p)", obj, indirect)
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("- %s", indirect.PdfObject)
		}
		if w.addObject(indirect) {
			err := w.addObjects(indirect.PdfObject)
			if err != nil {
				return err
			}
		}
		return nil
	}
	if stream, ok := obj.(*core.PdfObjectStream); ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Stream")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("- %s %p", obj, obj)
		}
		if w.addObject(stream) {
			err := w.addObjects(stream.PdfObjectDictionary)
			if err != nil {
				return err
			}
		}
		return nil
	}
	if dict, ok := obj.(*core.PdfObjectDictionary); ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Dict")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("- %s", obj)
		}
		for _, name := range dict.Keys() {
			obj := dict.Get(name)
			if ref, ok := obj.(*core.PdfObjectReference); ok {
				obj = ref.Resolve()
				dict.Set(name, obj)
			}
			if name != "Parent" {
				if err := w.addObjects(obj); err != nil {
					return err
				}
			} else {
				if _, ok := obj.(*core.PdfObjectNull); ok {
					continue
				}
				if ok := w.hasObject(obj); !ok {
					common.Log.Debug("Parent obj not added yet!! %T %p %v", obj, obj, obj)
					w.missingParents[obj] = append(w.missingParents[obj], dict)
				}
			}
		}
		return nil
	}
	if arr, ok := obj.(*core.PdfObjectArray); ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Array")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("- %s", obj)
		}
		if arr == nil {
			return errors.New("array is nil")
		}
		for i, obj := range arr.Elements() {
			if ref, ok := obj.(*core.PdfObjectReference); ok {
				obj = ref.Resolve()
				arr.Set(i, obj)
			}
			if err := w.addObjects(obj); err != nil {
				return err
			}
		}
		return nil
	}
	if _, ok := obj.(*core.PdfObjectReference); ok {
		common.Log.Debug("ERROR: Cannot be a reference - got %#v!", obj)
		return errors.New("reference not allowed")
	}
	return nil
}

// PdfBorderStyle represents a border style dictionary (12.5.4 Border Styles p. 394).
type PdfBorderStyle struct {
	W         *float64
	S         *BorderStyle
	D         *[]int
	container core.PdfObject
}

// NewPdfAnnotationSquiggly returns a new text squiggly annotation.
func NewPdfAnnotationSquiggly() *PdfAnnotationSquiggly {
	annot := NewPdfAnnotation()
	squiggly := &PdfAnnotationSquiggly{}
	squiggly.PdfAnnotation = annot
	squiggly.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(squiggly)
	return squiggly
}

type pdfFontSimple struct {
	fontCommon
	primitive       *core.PdfIndirectObject
	charWidths      map[textencoding.CharCode]float64
	encoder         textencoding.TextEncoder
	std14Encoder    textencoding.TextEncoder
	std14Descriptor *PdfFontDescriptor

	// Encoding is subject to limitations that are described in 9.6.6, "Character Encoding".
	// BaseFont is derived differently.
	FirstChar   core.PdfObject
	LastChar    core.PdfObject
	Widths      core.PdfObject
	Encoding    core.PdfObject
	fontMetrics *fonts.RuneCharSafeMap
}

// ColorToRGB converts gray -> rgb for a single color component.
func (cs *PdfColorspaceDeviceGray) ColorToRGB(color PdfColor) (PdfColor, error) {
	deviceGrayColor, ok := color.(*PdfColorDeviceGray)
	if !ok {
		common.Log.Debug("Input color not device gray %T", color)
		return nil, errors.New("type check error")
	}
	return NewPdfColorDeviceRGB(float64(*deviceGrayColor), float64(*deviceGrayColor), float64(*deviceGrayColor)), nil
}

// GetNamedDestinations returns the Dests entry in the PDF catalog.
// See section 12.3.2.3 "Named Destinations" (p. 367 PDF32000_2008).
func (r *PdfReader) GetNamedDestinations() (core.PdfObject, error) {
	dests := core.ResolveReference(r.catalog.Get("Dests"))
	if dests == nil {
		return nil, nil
	}
	if !r.lazy {
		err := r.traverseObjectData(dests)
		if err != nil {
			return nil, err
		}
	}
	return dests, nil
}

// PdfColorspaceCalRGB stores A, B, C components
type PdfColorspaceCalRGB struct {
	WhitePoint []float64
	BlackPoint []float64
	Gamma      []float64
	Matrix     []float64
	dict       *core.PdfObjectDictionary
	container  *core.PdfIndirectObject
}

func (w *PdfWriter) excludedFromObjectStream(obj core.PdfObject) bool {
	if w.encryptObj != nil && obj == w.encryptObj {
		return true
	}
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		if _, ok := indirect.PdfObject.(*pdfSignDictionary); ok {
			return true
		}
	}
	return false
}

// NewCompliancePdfReader creates a PdfReader that scans the file for the
// extra metadata needed by PDF/A or PDF/X compliance checks. The source's
// byte length is auto-detected; for exotic io.ReaderAt sources use
// NewCompliancePdfReaderAt.
// NOTE: This implementation is in experimental development state and may
// change in subsequent minor versions.
func NewCompliancePdfReader(r io.ReaderAt) (*CompliancePdfReader, error) {
	sizeOfReaderAt, err := core.SizeOfReaderAt(r)
	if err != nil {
		return nil, err
	}
	return NewCompliancePdfReaderAt(r, sizeOfReaderAt)
}

// ToPdfObject implements interface PdfModel.
func (r *PdfSignatureReference) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	dict.SetIfNotNil("Type", r.Type)
	dict.SetIfNotNil("TransformMethod", r.TransformMethod)
	dict.SetIfNotNil("TransformParams", r.TransformParams)
	dict.SetIfNotNil("Data", r.Data)
	dict.SetIfNotNil("DigestMethod", r.DigestMethod)
	return dict
}

// GetContentStream returns the pattern cell's content stream
func (p *PdfTilingPattern) GetContentStream() ([]byte, error) {
	contentStream, _, err := p.GetContentStreamWithEncoder()
	return contentStream, err
}

// LTV represents an LTV (Long-Term Validation) client. It is used to LTV
// enable signatures by adding validation and revocation data (certificate,
// OCSP and CRL information) to the DSS dictionary of a PDF document.
//
// LTV is added through the DSS by:
//   - Adding certificates, OCSP and CRL information in the global scope of the
//     DSS. The global data is used for validating any of the signatures present
//     in the document.
//   - Adding certificates, OCSP and CRL information for a single signature,
//     through an entry in the VRI dictionary of the DSS. The added data is used
//     for validating that particular signature only. This is the recommended
//     method for adding validation data for a signature. However, this is not
//     possible in the same revision the signature is applied. Validation data
//     for a signature is added based on the Contents entry of the signature,
//     which is known only after the revision is written. Even if the Contents
//     are known (e.g. when signing externally), updating the DSS at that point
//     would invalidate the calculated signature. As a result, if adding LTV
//     in the same revision is a requirement, use the first method.
//     See LTV.EnableChain.
//
// The client applies both methods, when possible.
//
// If `LTV.SkipExisting` is set to true (the default), validations are
// not added for signatures which are already present in the VRI entry of the
// document's DSS dictionary.
type LTV struct {
	// CertClient is the client used to retrieve certificates.
	CertClient *sigutil.CertClient

	// OCSPClient is the client used to retrieve OCSP validation information.
	OCSPClient *sigutil.OCSPClient

	// CRLClient is the client used to retrieve CRL validation information.
	CRLClient *sigutil.CRLClient

	// SkipExisting specifies whether existing signature validations
	// should be skipped.
	SkipExisting bool
	appender     *PdfAppender
	dss          *DSS
}

// ColorFromFloats returns a new PdfColor based on the input slice of color
// components.
func (cs *PdfColorspaceICCBased) ColorFromFloats(vals []float64) (PdfColor, error) {
	if cs.Alternate == nil {
		if cs.N == 1 {
			deviceGrayCS := NewPdfColorspaceDeviceGray()
			return deviceGrayCS.ColorFromFloats(vals)
		} else if cs.N == 3 {
			deviceRGBCS := NewPdfColorspaceDeviceRGB()
			return deviceRGBCS.ColorFromFloats(vals)
		} else if cs.N == 4 {
			deviceCMYKCS := NewPdfColorspaceDeviceCMYK()
			return deviceCMYKCS.ColorFromFloats(vals)
		} else {
			return nil, errors.New("ICC Based colorspace missing alternative")
		}
	}
	return cs.Alternate.ColorFromFloats(vals)
}

func (r *PdfReader) loadAnnotations(annots core.PdfObject) ([]*PdfAnnotation, error) {
	annotsArr, ok := core.GetArray(annots)
	if !ok {
		return nil, fmt.Errorf("Annots not an array")
	}
	var annots2 []*PdfAnnotation
	for _, obj := range annotsArr.Elements() {
		obj = core.ResolveReference(obj)
		if _, ok := obj.(*core.PdfObjectNull); ok {
			continue
		}
		dict, ok := obj.(*core.PdfObjectDictionary)
		indirect, isIndirect := obj.(*core.PdfIndirectObject)
		if ok {
			indirect = &core.PdfIndirectObject{}
			indirect.PdfObject = dict
		} else {
			if !isIndirect {
				return nil, fmt.Errorf("annotation not in an indirect object")
			}
		}
		annot, err := r.newPdfAnnotationFromIndirectObject(indirect)
		if err != nil {
			return nil, err
		}
		switch v := annot.GetContext().(type) {
		case *PdfAnnotationWidget:
			for _, allField := range r.AcroForm.AllFields() {
				if allField.primitive == v.Parent {
					v.field = allField
					break
				}
			}
		}
		if annot != nil {
			annots2 = append(annots2, annot)
		}
	}
	return annots2, nil
}

// PdfFieldButton represents a button field which includes push buttons, checkboxes, and radio buttons.
type PdfFieldButton struct {
	*PdfField
	Opt         *core.PdfObjectArray
	buttonImage *Image
}

func (r *PdfReader) loadAction(a core.PdfObject) (*PdfAction, error) {
	if aIndirect, ok := core.GetIndirect(a); ok {
		action, err := r.newPdfActionFromIndirectObject(aIndirect)
		if err != nil {
			return nil, err
		}
		return action, nil
	} else if !core.IsNullObject(a) {
		return nil, errors.New("action should point to an indirect object")
	}
	return nil, nil
}

func (w *PdfWriter) prepareLinearizedDocument() error {
	if err := w.checkLicense(); err != nil {
		return err
	}
	if err := w.writeOutlines(); err != nil {
		return err
	}
	if err := w.writeAcroFormFields(); err != nil {
		return err
	}
	if err := w.writeNamesDictionary(); err != nil {
		return err
	}
	w.checkPendingObjects()
	if err := w.writeOutputIntents(); err != nil {
		return err
	}
	w.setCatalogVersion()
	w.copyObjects()
	w.refreshPagesAfterCopy()
	if err := w.optimize(); err != nil {
		return err
	}
	if err := w.optimizeDocument(); err != nil {
		return err
	}
	return nil
}

// SetSchema sets the namespace schema, a reference to a file containing a
// schema for the namespace (ISO 32000-2 14.7.4.2, table 356).
func (ns *PdfStructureNamespace) SetSchema(schema core.PdfObject) { ns.schema = schema }

func (p *PdfPage) flattenFieldsWithOpts(appgen FieldAppearanceGenerator, fieldFlattenOpts *FieldFlattenOpts, set map[*PdfAnnotation]bool) error {
	var annots []*PdfAnnotation
	if appgen != nil {
		if err := appgen.WrapContentStream(p); err != nil {
			return err
		}
	}
	annotations, err := p.GetAnnotations()
	if err != nil {
		return err
	}
	for _, annotation := range annotations {
		ok, found := set[annotation]
		if !found && fieldFlattenOpts.AnnotFilterFunc != nil {
			if _, valid := annotation.GetContext().(*PdfAnnotationWidget); !valid {
				found = fieldFlattenOpts.AnnotFilterFunc(annotation)
			}
		}
		if !found {
			annots = append(annots, annotation)
			continue
		}
		switch annotation.GetContext().(type) {
		case *PdfAnnotationPopup:
			continue
		case *PdfAnnotationLink:
			continue
		case *PdfAnnotationProjection:
			continue
		}
		form, rect, err := appearanceFormAndRect(annotation)
		if err != nil {
			if !ok {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Field without V -> annotation without appearance stream - skipping over")
				}
				continue
			}
			common.Log.Debug("ERROR Annotation without appearance stream, err : %v - skipping over", err)
			continue
		}
		if form == nil {
			continue
		}
		generateXObjectName := p.Resources.GenerateXObjectName()
		p.Resources.SetXObjectFormByName(generateXObjectName, form)
		rect2, done, err := formBBox(form)
		if err != nil {
			common.Log.Debug("Error converting appearance Matrix, skipping xform bbox adjustment: %v", err)
		} else {
			identityMatrix := transform.IdentityMatrix()
			identityMatrix = identityMatrix.Translate(-rect2.Llx, -rect2.Lly)
			if done {
				f := 1.0
				if rect2.Width() > 0 {
					f = math.Round(rect.Width() / rect2.Width())
				}
				v := 1.0
				if rect2.Height() > 0 {
					v = math.Round(rect.Height() / rect2.Height())
				}
				identityMatrix = identityMatrix.Scale(f, v)
			}
			rect.Transform(identityMatrix)
		}
		f := math.Min(rect.Llx, rect.Urx)
		v := math.Min(rect.Lly, rect.Ury)
		var strs []string
		strs = append(strs, "q")
		strs = append(strs, fmt.Sprintf("%.6f %.6f %.6f %.6f %.6f %.6f cm", 1.0, 0.0, 0.0, 1.0, f, v))
		strs = append(strs, fmt.Sprintf("/%s Do", generateXObjectName.String()))
		strs = append(strs, "Q")
		join := strings.Join(strs, "\n")
		err = p.AppendContentStream(join)
		if err != nil {
			return err
		}
		if form.Resources != nil {
			fontDict, ok := core.GetDict(form.Resources.Font)
			if ok {
				for _, name := range fontDict.Keys() {
					if !p.Resources.HasFontByName(name) {
						p.Resources.SetFontByName(name, fontDict.Get(name))
					}
				}
			}
		}
	}
	if len(annots) > 0 {
		p.annotations = annots
	} else {
		p.annotations = []*PdfAnnotation{}
	}
	return nil
}

// SetType sets the field button's type.  Can be one of:
// - PdfFieldButtonPush for push button fields
// - PdfFieldButtonCheckbox for checkbox fields
// - PdfFieldButtonRadio for radio button fields
// This sets the field's flag appropriately.
func (f *PdfFieldButton) SetType(btype ButtonType) {
	n := uint32(0)
	if f.Ff != nil {
		n = uint32(*f.Ff)
	}
	switch btype {
	case ButtonTypePush:
		n |= FieldFlagPushbutton.Mask()
	case ButtonTypeRadio:
		n |= FieldFlagRadio.Mask()
	}
	f.Ff = core.MakeInteger(int64(n))
}

// NewStandard14FontMustCompile returns the standard 14 font named `basefont` as a *PdfFont.
// If `basefont` is one of the 14 Standard14Font values defined above then NewStandard14FontMustCompile
// is guaranteed to succeed.
func NewStandard14FontMustCompile(basefont StdFontName) *PdfFont {
	standard14Font, err := NewStandard14Font(basefont)
	if err != nil {
		panic(fmt.Errorf("invalid Standard14Font %#q", basefont))
	}
	return standard14Font
}

// GetShadingByName gets the shading specified by keyName. Returns nil if not existing.
// The bool flag indicated whether it was found or not.
func (r *PdfPageResources) GetShadingByName(keyName core.PdfObjectName) (*PdfShading, bool) {
	if r.Shading == nil {
		return nil, false
	}
	dict, ok := core.TraceToDirectObject(r.Shading).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Invalid Shading entry - not a dict (got %T)", r.Shading)
		return nil, false
	}
	if obj := dict.Get(keyName); obj != nil {
		shading, err := newPdfShadingFromPdfObject(obj)
		if err != nil {
			common.Log.Debug("ERROR: failed to load pdf shading: %v", err)
			return nil, false
		}
		return shading, true
	}
	return nil, false
}

func (w *PdfWriter) setDocumentIDs(id0, s string) {
	w.ids = core.MakeArray(core.MakeHexString(id0), core.MakeHexString(s))
}

func (r *PdfReader) newPdfAnnotationWatermarkFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationWatermark, error) {
	watermark := PdfAnnotationWatermark{}
	watermark.FixedPrint = dict.Get("FixedPrint")
	return &watermark, nil
}

// NewPdfActionSetOCGState returns a new "named" action.
func NewPdfActionSetOCGState() *PdfActionSetOCGState {
	action := NewPdfAction()
	setOCGStateAction := &PdfActionSetOCGState{}
	setOCGStateAction.PdfAction = action
	action.SetContext(setOCGStateAction)
	return setOCGStateAction
}

// AddCerts adds certificates to DSS.
func (d *DSS) AddCerts(certs [][]byte) ([]*core.PdfObjectStream, error) {
	return d.add(&d.Certs, d.certsMap, certs)
}

func newPdfColorspaceSpecialSeparationFromPdfObject(obj core.PdfObject) (*PdfColorspaceSpecialSeparation, error) {
	specialSeparationCS := NewPdfColorspaceSpecialSeparation()
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		specialSeparationCS.container = indirect
	}
	obj = core.TraceToDirectObject(obj)
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("separation CS: Invalid object")
	}
	if arr.Len() != 4 {
		return nil, fmt.Errorf("separation CS: Incorrect array length")
	}
	obj = arr.Get(0)
	name, ok := obj.(*core.PdfObjectName)
	if !ok {
		return nil, fmt.Errorf("separation CS: invalid family name")
	}
	if *name != "Separation" {
		return nil, fmt.Errorf("separation CS: wrong family name")
	}
	obj = arr.Get(1)
	name, ok = obj.(*core.PdfObjectName)
	if !ok {
		return nil, fmt.Errorf("separation CS: Invalid colorant name")
	}
	specialSeparationCS.ColorantName = name
	obj = arr.Get(2)
	cs, err := NewPdfColorspaceFromPdfObject(obj)
	if err != nil {
		return nil, err
	}
	specialSeparationCS.AlternateSpace = cs
	function, err := newPdfFunctionFromPdfObject(arr.Get(3))
	if err != nil {
		return nil, err
	}
	specialSeparationCS.TintTransform = function
	return specialSeparationCS, nil
}

func (f *pdfFontType0) subsetTrueType(font *pdfCIDFontType2) error {
	if font == nil {
		return nil
	}
	if font.fontDescriptor == nil {
		common.Log.Debug("Missing font descriptor")
		return nil
	}
	if f.encoder == nil {
		common.Log.Debug("No encoder - subsetting ignored")
		return nil
	}
	fontFile2Stream, ok := core.GetStream(font.fontDescriptor.FontFile2)
	if !ok {
		common.Log.Debug("Embedded font object not found -- ABORT subsetting")
		return errors.New("fontfile2 not found")
	}
	stream, err := core.DecodeStream(fontFile2Stream)
	if err != nil {
		common.Log.Debug("Decode error: %v", err)
		return err
	}
	font2, err := unitype.Parse(bytes.NewReader(stream))
	if err != nil {
		common.Log.Debug("Error parsing %d byte font", len(fontFile2Stream.Stream))
		return err
	}
	var runes []rune
	var gids []unitype.GlyphIndex
	switch v := f.encoder.(type) {
	case textencoding.GIDRegistry:
		registeredGIDs := v.RegisteredGIDs()
		if len(registeredGIDs) == 0 {
			common.Log.Debug("No registered GIDs - skip subsetting")
			return nil
		}
		gids = make([]unitype.GlyphIndex, len(registeredGIDs))
		for i, registeredGID := range registeredGIDs {
			gids[i] = unitype.GlyphIndex(registeredGID)
		}
	case textencoding.RuneRegistry:
		runes = v.RegisteredRunes()
		gids = font2.LookupRunes(runes)
		v.SubsetRegistered()
	case textencoding.SimpleEncoder:
		charcodes := v.Charcodes()
		for _, charcode := range charcodes {
			charcodeToRune, ok := v.CharcodeToRune(charcode)
			if !ok {
				common.Log.Debug("ERROR: unable convert charcode to rune: %d", charcode)
				continue
			}
			runes = append(runes, charcodeToRune)
		}
		gids = font2.LookupRunes(runes)
	default:
		return fmt.Errorf("unsupported encoder for subsetting: %T", f.encoder)
	}
	vals := shapedGlyphIDs(f.encoder)
	for _, val := range vals {
		gids = append(gids, unitype.GlyphIndex(val))
	}
	gids = sortedUniqueGlyphIndexes(gids)
	subsetKeepIndices, err := font2.SubsetKeepIndices(gids)
	if err != nil {
		common.Log.Debug("ERROR: %v", err)
		return err
	}
	var buf bytes.Buffer
	err = subsetKeepIndices.Write(&buf)
	if err != nil {
		common.Log.Debug("ERROR: %v", err)
		return err
	}
	if runes != nil {
		f.updateSubsetToUnicode(runes, vals)
	} else {
		f.filterSubsetToUnicode(gids)
	}
	registerGlyphWidthsFromGlyphIndexes(font.W, gids)
	registerOtfGlyphWidths(font.W, vals, font.GetOtfType())
	fontFile2Stream, err = core.MakeStream(buf.Bytes(), core.NewFlateEncoder())
	if err != nil {
		common.Log.Debug("ERROR: %v", err)
		return err
	}
	fontFile2Stream.Set("Length1", core.MakeInteger(int64(buf.Len())))
	if stream, ok := core.GetStream(font.fontDescriptor.FontFile2); ok {
		*stream = *fontFile2Stream
	} else {
		font.fontDescriptor.FontFile2 = fontFile2Stream
	}
	applySubsetTag(f, &font.baseFont, font.fontDescriptor, buf.Bytes())
	return nil
}

// PdfShading represents a shading dictionary. There are 7 types of shading,
// indicatedby the shading type variable:
// 1: Function-based shading.
// 2: Axial shading.
// 3: Radial shading.
// 4: Free-form Gouraud-shaded triangle mesh.
// 5: Lattice-form Gouraud-shaded triangle mesh.
// 6: Coons patch mesh.
// 7: Tensor-product patch mesh.
// types 4-7 are contained in a stream object, where the dictionary is given by the stream dictionary.
type PdfShading struct {
	ShadingType *core.PdfObjectInteger
	ColorSpace  PdfColorspace
	Background  *core.PdfObjectArray
	BBox        *PdfRectangle
	AntiAlias   *core.PdfObjectBool
	context     PdfModel
	primitive   core.PdfObject
}

func (w *PdfWriter) updateObjectNumbers() {
	objNumOffset := w.ObjNumOffset
	n := 0
	for _, object := range w.objects {
		count := int64(n + 1 + objNumOffset)
		ok := true
		if w.appendMode {
			if objNumMap, isObjNumMap := w.objNumMap[object]; isObjNumMap {
				count = objNumMap
				ok = false
			}
		}
		switch v := object.(type) {
		case *core.PdfIndirectObject:
			v.ObjectNumber = count
			v.GenerationNumber = 0
		case *core.PdfObjectStream:
			v.ObjectNumber = count
			v.GenerationNumber = 0
		case *core.PdfObjectStreams:
			v.ObjectNumber = count
			v.GenerationNumber = 0
		case *core.PdfObjectReference:
			v.ObjectNumber = count
			v.GenerationNumber = 0
		case *core.PdfObjectDictionary, *core.PdfObjectString:
		default:
			common.Log.Debug("ERROR: Unknown type %T - skipping", v)
			continue
		}
		if ok {
			n++
		}
	}
	fn := func(obj core.PdfObject) int64 {
		switch v := obj.(type) {
		case *core.PdfIndirectObject:
			return v.ObjectNumber
		case *core.PdfObjectStream:
			return v.ObjectNumber
		case *core.PdfObjectStreams:
			return v.ObjectNumber
		case *core.PdfObjectReference:
			return v.ObjectNumber
		}
		return 0
	}
	sort.SliceStable(w.objects, func(n, count int) bool { return fn(w.objects[n]) < fn(w.objects[count]) })
}

func (f *pdfFontType0) filterSubsetToUnicode(gids []unitype.GlyphIndex) {
	if f.toUnicodeCMap == nil {
		return
	}
	textByCode := make(map[cmap.CharCode]string, len(gids))
	for _, gid := range gids {
		code := cmap.CharCode(gid)
		if charcodeToUnicode, ok := f.toUnicodeCMap.CharcodeToUnicode(code); ok {
			textByCode[code] = charcodeToUnicode
		}
	}
	f.toUnicodeCMap = cmap.NewToUnicodeCMap(textByCode)
	if identityEncoder, ok := f.encoder.(*textencoding.IdentityEncoder); ok {
		identityEncoder.SetToUnicodeCMap(f.toUnicodeCMap)
	}
}

// NewPdfAnnotationCaret returns a new caret annotation.
func NewPdfAnnotationCaret() *PdfAnnotationCaret {
	annot := NewPdfAnnotation()
	caret := &PdfAnnotationCaret{}
	caret.PdfAnnotation = annot
	caret.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(caret)
	return caret
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionURI) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeURI)))
	dict.SetIfNotNil("URI", a.URI)
	dict.SetIfNotNil("IsMap", a.IsMap)
	return indirect
}

// RegisterLigatureMapping records the ToUnicode mapping for a shaped run so the
// emitted glyphs are extractable as text and kept through subsetting. It is the
// ToUnicode counterpart of ShapeText: ShapeText registers glyphs with the
// encoder for subsetting, while this records what text those glyphs represent
// and also registers their glyph IDs. Callers driving their own shaper should
// call both; PdfFont.ShapeText followed by this method (as the creator does) is
// the supported pairing.
func (f *PdfFont) RegisterLigatureMapping(shapingOutput shaping.Output, text string) error {
	if f == nil {
		return errors.New("font is nil")
	}
	runes := []rune(text)
	gidEncoder, _ := f.Encoder().(textencoding.GIDEncoder)
	v, _ := f.Encoder().(interface{ RegisterShapedGID(n uint16) })
	textByCode := make(map[cmap.CharCode]string)
	code := make(map[cmap.CharCode]string)
	n := -1
	for _, glyph := range shapingOutput.Glyphs {
		if glyph.GlyphID > 0xffff {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("RegisterLigatureMapping: glyph ID %d overflows uint16 - skipping", glyph.GlyphID)
			}
			continue
		}
		ok := glyph.TextIndex() != n
		n = glyph.TextIndex()
		glyphID := uint16(glyph.GlyphID)
		if v != nil {
			v.RegisterShapedGID(glyphID)
		}
		code2 := textencoding.CharCode(textencoding.GID(glyphID))
		if gidEncoder != nil {
			if gidToCharcode, ok := gidEncoder.GIDToCharcode(textencoding.GID(glyphID)); ok {
				code2 = gidToCharcode
			}
		}
		code3 := cmap.CharCode(code2)
		s := shapedGlyphText(runes, glyph)
		if s == "" {
			continue
		}
		if glyph.GlyphsCount() == 1 {
			textByCode[code3] = s
		} else if ok {
			if _, ok := code[code3]; !ok {
				code[code3] = s
			}
		}
	}
	switch v := f.context.(type) {
	case *pdfFontType0:
		v.applyLigatureToUnicode(textByCode, code)
	default:
	}
	return nil
}

// ImageToRGB returns an error since an image cannot be defined in a pattern colorspace.
func (cs *PdfColorspaceSpecialPattern) ImageToRGB(img Image) (Image, error) {
	common.Log.Debug("Error: Image cannot be specified in Pattern colorspace")
	return img, errors.New("invalid colorspace for image (pattern)")
}

func filterFieldsRecursive(fields *[]*PdfField, filter FieldFilterFunc, ok bool) []*PdfField {
	if fields == nil {
		return nil
	}
	fields2 := *fields
	if len(*fields) == 0 {
		return nil
	}
	fields3 := fields2[:0]
	if filter == nil {
		filter = func(*PdfField) bool { return true }
	}
	var fields4 []*PdfField
	for _, field := range fields2 {
		found := filter(field)
		if found {
			fields4 = append(fields4, field)
			if len(field.Kids) > 0 {
				fields4 = append(fields4, filterFieldsRecursive(&field.Kids, filter, ok)...)
			}
		}
		if !ok || !found || len(field.Kids) > 0 {
			fields3 = append(fields3, field)
		}
	}
	*fields = fields3
	return fields4
}

// NewPdfColorDeviceRGB returns a new PdfColorDeviceRGB based on the r,g,b component values.
func NewPdfColorDeviceRGB(r, g, b float64) *PdfColorDeviceRGB {
	deviceRGBColor := PdfColorDeviceRGB{r, g, b}
	return &deviceRGBColor
}

// BorderEffect represents a border effect (Table 167 p. 395).
type BorderEffect int

// ToPdfObject converts the pdfFontSimple to its PDF representation for outputting.
func (f *pdfFontSimple) ToPdfObject() core.PdfObject {
	if f.primitive == nil {
		f.primitive = &core.PdfIndirectObject{}
	}
	dict := f.baseFields().asPdfObjectDictionary("")
	f.primitive.PdfObject = dict
	if f.FirstChar != nil {
		dict.Set("FirstChar", f.FirstChar)
	}
	if f.LastChar != nil {
		dict.Set("LastChar", f.LastChar)
	}
	if f.Widths != nil {
		dict.Set("Widths", f.Widths)
	}
	if f.Encoding != nil {
		dict.Set("Encoding", f.Encoding)
	} else if f.encoder != nil {
		obj := f.encoder.ToPdfObject()
		if obj != nil {
			dict.Set("Encoding", obj)
		}
	}
	return f.primitive
}

// GetRefObject returns the reference object of the KValue.
func (kv *KValue) GetRefObject() core.PdfObject { return kv.refObject }

func (p *PdfPage) annotationsSnapshot() []*PdfAnnotation {
	if p.mu != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
	}
	return p.annotations
}

func parseByteRange(arr *core.PdfObjectArray, n int64) ([]int64, error) {
	arrLen := arr.Len()
	if arrLen < 2 || arrLen%2 != 0 {
		return nil, fmt.Errorf("invalid ByteRange length %d", arrLen)
	}
	nums := make([]int64, 0, arrLen)
	for i := 0; i < arrLen; i++ {
		n, err := core.GetNumberAsInt64(arr.Get(i))
		if err != nil {
			return nil, fmt.Errorf("invalid ByteRange entry %d: %v", i, err)
		}
		if n < 0 {
			return nil, fmt.Errorf("negative ByteRange entry %d", i)
		}
		nums = append(nums, n)
	}
	for i := 0; i < arrLen; i += 2 {
		if nums[i] > n || nums[i+1] > n-nums[i] {
			return nil, fmt.Errorf("ByteRange pair (%d, %d) outside of file", nums[i], nums[i+1])
		}
	}
	return nums, nil
}

// ColorToRGB converts an Indexed color to an RGB color.
func (cs *PdfColorspaceSpecialIndexed) ColorToRGB(color PdfColor) (PdfColor, error) {
	if cs.Base == nil {
		return nil, errors.New("indexed base colorspace undefined")
	}
	return cs.Base.ColorToRGB(color)
}

// PrintScaling represents the page scaling option that shall be selected
// when a print dialog is displayed for this document.
type PrintScaling string

// AppendContentStream adds content stream by string.  Appends to the last
// contentstream instance if many.
func (p *PdfPage) AppendContentStream(contentStr string) error {
	contentStreams, err := p.GetContentStreams()
	if err != nil {
		return err
	}
	if len(contentStreams) == 0 {
		contentStreams = []string{contentStr}
		return p.SetContentStreams(contentStreams, core.NewFlateEncoder())
	}
	var buf bytes.Buffer
	buf.WriteString(contentStreams[len(contentStreams)-1])
	buf.WriteString("\n")
	buf.WriteString(contentStr)
	contentStreams[len(contentStreams)-1] = buf.String()
	return p.SetContentStreams(contentStreams, core.NewFlateEncoder())
}

// ToPdfObject returns the PDF representation of the shading pattern.
func (sp *PdfShadingPatternType1) ToPdfObject() core.PdfObject {
	sp.PdfPattern.ToPdfObject()
	dict := sp.getDict()
	if sp.Shading != nil {
		dict.Set("Shading", sp.Shading.ToPdfObject())
	}
	if sp.Matrix != nil {
		dict.Set("Matrix", sp.Matrix)
	}
	if sp.ExtGState != nil {
		dict.Set("ExtGState", sp.ExtGState)
	}
	return sp.primitive
}

// LinearizationDictObjNum returns the object number of the linearization
// parameter dictionary (the first indirect object after the header), or 0 if
// the PDF does not begin with one. Populated even when IsLinearized() is false
// (e.g. the dictionary was superseded by a later incremental revision).
func (r *PdfReader) LinearizationDictObjNum() int {
	return r.parser.LinearizationDictObjNum()
}

// C returns the value of the C component of the color.
func (c *PdfColorCalRGB) C() float64 { return c[2] }

// StructureType represents a standard structure type in a tagged PDF document.
type StructureType string

// ToPdfObject returns a PDF object representation of the outline.
func (o *Outline) ToPdfObject() core.PdfObject { return o.ToPdfOutline().ToPdfObject() }

// ToPdfObject returns the PDF representation of the shading dictionary.
func (s *PdfShadingType1) ToPdfObject() core.PdfObject {
	s.PdfShading.ToPdfObject()
	shadingDict, err := s.getShadingDict()
	if err != nil {
		common.Log.Error("Unable to access shading dict")
		return nil
	}
	if s.Domain != nil {
		shadingDict.Set("Domain", s.Domain)
	}
	if s.Matrix != nil {
		shadingDict.Set("Matrix", s.Matrix)
	}
	if s.Function != nil {
		if len(s.Function) == 1 {
			shadingDict.Set("Function", s.Function[0].ToPdfObject())
		} else {
			array := core.MakeArray()
			for _, function := range s.Function {
				array.Append(function.ToPdfObject())
			}
			shadingDict.Set("Function", array)
		}
	}
	return s.primitive
}

func (r *PdfReader) newPdfSignatureFromIndirect(indirect *core.PdfIndirectObject) (*PdfSignature, error) {
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Signature container not containing a dictionary")
		return nil, ErrTypeCheck
	}
	if sig, ok := r.modelManager.GetModelFromPrimitive(indirect).(*PdfSignature); ok {
		return sig, nil
	}
	sig := &PdfSignature{}
	sig.primitive = indirect
	sig.Type, _ = core.GetName(dict.Get("Type"))
	sig.Filter, ok = core.GetName(dict.Get("Filter"))
	if !ok {
		common.Log.Error("ERROR: Signature Filter attribute invalid or missing")
		return nil, ErrInvalidAttribute
	}
	sig.SubFilter, _ = core.GetName(dict.Get("SubFilter"))
	sig.Contents, ok = core.GetString(dict.Get("Contents"))
	if !ok {
		common.Log.Error("ERROR: Signature contents missing")
		return nil, ErrInvalidAttribute
	}
	if reference, ok := core.GetArray(dict.Get("Reference")); ok {
		sig.Reference = core.MakeArray()
		for _, obj := range reference.Elements() {
			dict, ok := core.GetDict(obj)
			if !ok {
				common.Log.Error("ERROR: Reference contents invalidated")
				return nil, ErrInvalidAttribute
			}
			signatureReference, err := r.newPdfSignatureReferenceFromDict(dict)
			if err != nil {
				return nil, err
			}
			sig.Reference.Append(signatureReference.ToPdfObject())
		}
	}
	sig.Cert = dict.Get("Cert")
	sig.ByteRange, _ = core.GetArray(dict.Get("ByteRange"))
	sig.Changes, _ = core.GetArray(dict.Get("Changes"))
	sig.Name, _ = core.GetString(dict.Get("Name"))
	sig.M, _ = core.GetString(dict.Get("M"))
	sig.Location, _ = core.GetString(dict.Get("Location"))
	sig.Reason, _ = core.GetString(dict.Get("Reason"))
	sig.ContactInfo, _ = core.GetString(dict.Get("ContactInfo"))
	sig.R, _ = core.GetInt(dict.Get("R"))
	sig.V, _ = core.GetInt(dict.Get("V"))
	sig.PropBuild, _ = core.GetDict(dict.Get("Prop_Build"))
	sig.PropAuthTime, _ = core.GetInt(dict.Get("Prop_AuthTime"))
	sig.PropAuthType, _ = core.GetName(dict.Get("Prop_AuthType"))
	r.modelManager.Register(indirect, sig)
	return sig, nil
}

// GetAction returns the PDF action for the annotation link.
func (a *PdfAnnotationLink) GetAction() (*PdfAction, error) {
	if a.action != nil {
		return a.action, nil
	}
	if a.A == nil {
		return nil, nil
	}
	if a.reader == nil {
		return nil, nil
	}
	action, err := a.reader.loadAction(a.A)
	if err != nil {
		return nil, err
	}
	a.action = action
	return a.action, nil
}

// SetDecode sets the decode array: a (min, max) pair per color component.
// For gray images the pairs are in normalized component units, so that
// [0 1] is the identity and [1 0] inverts; for an Indexed image the index
// range is expressed as a fraction of 2^BitsPerComponent-1, as
// XObjectImage.ToImage produces it. Lab and colour-managed ICCBased images
// take their component ranges as is. DeviceRGB, DeviceCMYK and ICCBased
// images without a managed transform hand the array unscaled to the device
// sample readers, and the producers differ: XObjectImage.ToImage stores the
// PDF's values, inline images store sample units (0 to 2^BitsPerComponent-1)
// as those readers expect; unifying the two is pending.
func (img *Image) SetDecode(decode []float64) { img.decode = decode }

// HasExtGState checks if ExtGState name is available.
func (p *PdfPage) HasExtGState(name core.PdfObjectName) bool {
	if p.Resources == nil {
		return false
	}
	if p.Resources.ExtGState == nil {
		return false
	}
	dict, ok := core.TraceToDirectObject(p.Resources.ExtGState).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("Expected ExtGState dictionary is not a dictionary: %v", core.TraceToDirectObject(p.Resources.ExtGState))
		return false
	}
	obj := dict.Get(name)
	found := obj != nil
	return found
}

const (
	RC4_128bit = EncryptionAlgorithm(iota)
	AES_128bit
	AES_256bit
)

func newPdfFontType0FromCommon(fc *fontCommon) *pdfFontType0 { return &pdfFontType0{fontCommon: *fc} }

func (p *PdfPage) setContainer(indirect *core.PdfIndirectObject) {
	indirect.PdfObject = p.pageDict
	p.primitive = indirect
}
func (cs *PdfColorspaceSpecialIndexed) String() string { return "Indexed" }

// HasColorspaceByName checks if the colorspace with the specified name exists in the page resources.
func (r *PdfPageResources) HasColorspaceByName(keyName core.PdfObjectName) bool {
	colorspaces, err := r.GetColorspaces()
	if err != nil {
		common.Log.Debug("ERROR getting colorsprace: %v", err)
		return false
	}
	if colorspaces == nil {
		return false
	}
	_, ok := colorspaces.Colorspaces[string(keyName)]
	return ok
}

func (f *fontFile) loadFromSegments(data, data2 []byte) error {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("loadFromSegments: %d %d", len(data), len(data2))
	}
	err := f.parseASCIIPart(data)
	if err != nil {
		return err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("fontfile=%s", f)
	}
	if len(data2) == 0 {
		return nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("fontfile=%s", f)
	}
	return nil
}

func newPdfFontSimpleFromDict(dict *core.PdfObjectDictionary, fc *fontCommon, enc textencoding.TextEncoder) (*pdfFontSimple, error) {
	fontSimple := newPdfFontSimpleFromCommon(fc)
	fontSimple.std14Encoder = enc
	if enc == nil || dict.Get("Widths") != nil {
		firstChar := dict.Get("FirstChar")
		if firstChar == nil {
			firstChar = core.MakeInteger(0)
		}
		fontSimple.FirstChar = firstChar
		n, ok := core.GetIntVal(firstChar)
		if !ok {
			common.Log.Debug("ERROR: Invalid FirstChar type (%T)", firstChar)
			return nil, core.ErrTypeError
		}
		code := textencoding.CharCode(n)
		firstChar = dict.Get("LastChar")
		if firstChar == nil {
			firstChar = core.MakeInteger(255)
		}
		fontSimple.LastChar = firstChar
		n, ok = core.GetIntVal(firstChar)
		if !ok {
			common.Log.Debug("ERROR: Invalid LastChar type (%T)", firstChar)
			return nil, core.ErrTypeError
		}
		code2 := textencoding.CharCode(n)
		fontSimple.charWidths = make(map[textencoding.CharCode]float64)
		firstChar = dict.Get("Widths")
		if firstChar != nil {
			fontSimple.Widths = firstChar
			firstCharArr, ok := core.GetArray(firstChar)
			if !ok {
				common.Log.Debug("ERROR: Widths attribute != array (%T)", firstChar)
				return nil, core.ErrTypeError
			}
			floats, err := firstCharArr.ToFloat64Array()
			if err != nil {
				common.Log.Debug("ERROR: converting widths to array")
				return nil, err
			}
			if len(floats) != int(code2-code+1) {
				common.Log.Debug("ERROR: Invalid widths length != %d (%d)", code2-code+1, len(floats))
				return nil, core.ErrRangeError
			}
			for i, float := range floats {
				fontSimple.charWidths[code+textencoding.CharCode(i)] = float
			}
		}
	}
	fontSimple.Encoding = core.TraceToDirectObject(dict.Get("Encoding"))
	return fontSimple, nil
}

// HasXObjectByName checks if an XObject with a specified keyName is defined.
func (r *PdfPageResources) HasXObjectByName(keyName core.PdfObjectName) bool {
	xObjectByName, _ := r.GetXObjectByName(keyName)
	return xObjectByName != nil
}

// GetPageAsIndirectObject returns the page as a dictionary within an PdfIndirectObject.
func (p *PdfPage) GetPageAsIndirectObject() *core.PdfIndirectObject { return p.primitive }

func newPdfFontSimpleFromStdFont(stdFont fonts.StdFont) pdfFontSimple {
	descriptor := stdFont.Descriptor()
	return pdfFontSimple{fontCommon: fontCommon{subtype: "Type1", baseFont: stdFont.Name()}, fontMetrics: stdFont.GetMetricsTable(), std14Descriptor: &PdfFontDescriptor{FontName: core.MakeName(string(descriptor.Name)), FontFamily: core.MakeName(descriptor.Family), FontWeight: core.MakeFloat(float64(descriptor.Weight)), Flags: core.MakeInteger(int64(descriptor.Flags)), FontBBox: core.MakeArrayFromFloats(descriptor.BBox[:]), ItalicAngle: core.MakeFloat(descriptor.ItalicAngle), Ascent: core.MakeFloat(descriptor.Ascent), Descent: core.MakeFloat(descriptor.Descent), CapHeight: core.MakeFloat(descriptor.CapHeight), XHeight: core.MakeFloat(descriptor.XHeight), StemV: core.MakeFloat(descriptor.StemV), StemH: core.MakeFloat(descriptor.StemH)}, std14Encoder: stdFont.Encoder()}
}

// PdfAction represents an action in PDF (section 12.6 p. 412).
type PdfAction struct {
	context   PdfModel
	Type      core.PdfObject
	S         core.PdfObject
	Next      core.PdfObject
	primitive *core.PdfIndirectObject
}

// AddPage adds a page to the PDF file. The new page should be an indirect object.
func (w *PdfWriter) AddPage(page *PdfPage) error {
	const trackNameAddPage = "model:PdfWriter.AddPage"
	obj := pageToPdfObjectLicensed(page)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("==========")
		common.Log.Trace("Appending to page list %T", obj)
	}
	indirect, ok := core.GetIndirect(obj)
	if !ok {
		return errors.New("page should be an indirect object")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("%s", indirect)
		common.Log.Trace("%s", indirect.PdfObject)
	}
	dict, ok := core.GetDict(indirect.PdfObject)
	if !ok {
		return errors.New("page object should be a dictionary")
	}
	name, ok := core.GetName(dict.Get("Type"))
	if !ok {
		return fmt.Errorf("page should have a Type key with a value of type name (%T)", dict.Get("Type"))
	}
	if name.String() != "Page" {
		return errors.New("field Type != Page (Required)")
	}
	names := []core.PdfObjectName{"Resources", "MediaBox", "CropBox", "Rotate"}
	parent, isParent := core.GetIndirect(dict.Get("Parent"))
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Page Parent: %T (%v)", dict.Get("Parent"), isParent)
	}
	for isParent {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Page Parent: %T", parent)
		}
		dict2, isDict := core.GetDict(parent.PdfObject)
		if !isDict {
			return errors.New("invalid Parent object")
		}
		for _, name := range names {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Field %s", name)
			}
			if dict.Get(name) != nil {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("- page has already")
				}
				continue
			}
			if obj := dict2.Get(name); obj != nil {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Inheriting field %s", name)
				}
				dict.Set(name, obj)
			}
		}
		parent, isParent = core.GetIndirect(dict2.Get("Parent"))
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Next parent: %T", dict2.Get("Parent"))
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Traversal done")
	}
	dict.Set("Parent", w.pages)
	indirect.PdfObject = dict
	dict2, ok := core.GetDict(w.pages.PdfObject)
	if !ok {
		return errors.New("invalid Pages obj (not a dict)")
	}
	kids, ok := core.GetArray(dict2.Get("Kids"))
	if !ok {
		return errors.New("invalid Pages Kids obj (not an array)")
	}
	kids.Append(indirect)
	w.pageDicts[dict] = struct{}{}
	w.pageObjs = append(w.pageObjs, indirect)
	count, ok := core.GetInt(dict2.Get("Count"))
	if !ok {
		return errors.New("invalid Pages Count object (not an integer)")
	}
	*count = *count + 1
	if page.reader == nil {
		err := license.Track(w.trackID, trackNameAddPage, w.fileName)
		if err != nil {
			return err
		}
	} else {
		err := license.Track(page.reader.trackID, trackNameAddPage, page.reader.fileName)
		if err != nil {
			return err
		}
	}
	w.addObject(indirect)
	err := w.addObjects(dict)
	if err != nil {
		return err
	}
	return nil
}

// GetContainingPdfObject returns the container of the pattern object (indirect object).
func (p *PdfPattern) GetContainingPdfObject() core.PdfObject { return p.primitive }

func (r *PdfReader) newPdfAnnotationCaretFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationCaret, error) {
	caret := PdfAnnotationCaret{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	caret.PdfAnnotationMarkup = markup
	caret.RD = dict.Get("RD")
	caret.Sy = dict.Get("Sy")
	return &caret, nil
}

// ColorFromFloats returns a new PdfColor based on the input slice of color
// components. The slice should contain a single element between 0 and 1.
func (cs *PdfColorspaceCalGray) ColorFromFloats(vals []float64) (PdfColor, error) {
	if len(vals) != 1 {
		return nil, errors.New("range check")
	}
	val := vals[0]
	if val < 0.0 || val > 1.0 {
		common.Log.Debug("color out of CS range: ", val)
		return nil, ErrColorOutOfRange
	}
	calGrayColor := NewPdfColorCalGray(val)
	return calGrayColor, nil
}

// PdfAcroForm represents the AcroForm dictionary used for representation of form data in PDF.
type PdfAcroForm struct {
	Fields          *[]*PdfField
	NeedAppearances *core.PdfObjectBool
	SigFlags        *core.PdfObjectInteger
	CO              *core.PdfObjectArray
	DR              *PdfPageResources
	DA              *core.PdfObjectString
	Q               *core.PdfObjectInteger
	XFA             core.PdfObject

	// ADBEEchoSign extra objects from Adobe Acrobat, causing signature invalid if not exists.
	ADBEEchoSign core.PdfObject
	primitive    *core.PdfIndirectObject
	direct       bool
}
type pdfCIDFontType2 struct {
	fontCommon
	primitive *core.PdfIndirectObject
	encoder   textencoding.TextEncoder

	// Table 117 – Entries in a CIDFont dictionary (page 269)
	// Dictionary that defines the character collection of the CIDFont (required).
	// See Table 116.
	CIDSystemInfo *core.PdfObjectDictionary

	// Glyph metrics fields (optional).
	DW  core.PdfObject
	W   core.PdfObject
	DW2 core.PdfObject
	W2  core.PdfObject

	// CIDs to glyph indices mapping (optional).
	CIDToGIDMap  core.PdfObject
	charWidths   map[textencoding.CharCode]float64
	defaultWidth float64
	runeToGID    map[rune]int
	otfType      *fonts.OtfType
}

// NewXObjectImage returns a new XObjectImage.
func NewXObjectImage() *XObjectImage {
	xobj := &XObjectImage{}
	stream := &core.PdfObjectStream{}
	stream.PdfObjectDictionary = core.MakeDict()
	xobj.primitive = stream
	return xobj
}

func shapedGlyphText(runes []rune, glyph shaping.Glyph) string {
	textIndex := glyph.TextIndex()
	n := textIndex + glyph.RunesCount()
	if textIndex < 0 || n > len(runes) || textIndex >= n {
		return ""
	}
	s := string(runes[textIndex:n])
	if glyphToRune, ok := textencoding.GlyphToRune(textencoding.GlyphName(s)); ok {
		return string(glyphToRune)
	}
	return s
}

// SetKDict sets the KDict for the KValue.
func (kv *KValue) SetKDict(kDict *KDict) { kv.Clear(); kv.kDict = kDict }

// NewPdfAnnotationPolygon returns a new polygon annotation.
func NewPdfAnnotationPolygon() *PdfAnnotationPolygon {
	annot := NewPdfAnnotation()
	polygon := &PdfAnnotationPolygon{}
	polygon.PdfAnnotation = annot
	polygon.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(polygon)
	return polygon
}

// ToPdfObject implements interface PdfModel.
func (f *EmbeddedFile) ToPdfObject() core.PdfObject {
	flateEncoder := core.NewFlateEncoder()
	stream, err := core.MakeStream(f.Content, flateEncoder)
	if err != nil {
		common.Log.Debug("Failed to create embedded file stream: %v", err)
		return nil
	}
	dict := stream.PdfObjectDictionary
	dict.Set("Type", core.MakeName("EmbeddedFile"))
	dict.Set("Subtype", core.MakeEncodedString("text/plain", true))
	dict2 := core.MakeDict()
	dict2.Set("CheckSum", core.MakeString(f.Hash[:]))
	dict2.Set("Size", core.MakeInteger(int64(len(f.Content))))
	creationTime := f.CreationTime
	if creationTime.IsZero() {
		creationTime = time.Now()
	}
	modTime := f.ModTime
	if modTime.IsZero() {
		modTime = creationTime
	}
	date, err := NewPdfDateFromTime(creationTime)
	if err != nil {
		common.Log.Debug("Failed to create embedded file creation date: %v", err)
		return nil
	}
	date2, err := NewPdfDateFromTime(modTime)
	if err != nil {
		common.Log.Debug("Failed to create embedded file creation date: %v", err)
		return nil
	}
	dict2.Set("CreationDate", date.ToPdfObject())
	dict2.Set("ModDate", date2.ToPdfObject())
	dict.Set("Params", dict2)
	dict3 := core.MakeDict()
	dict3.Set(*core.MakeName("F"), stream)
	return dict3
}

// GetMediaBox gets the inheritable media box value, either from the page
// or a higher up page/pages struct.
func (p *PdfPage) GetMediaBox() (*PdfRectangle, error) {
	if p.MediaBox != nil {
		return p.MediaBox, nil
	}
	parent := p.Parent
	for parent != nil {
		parentDict, ok := core.GetDict(parent)
		if !ok {
			return nil, errors.New("invalid parent objects dictionary")
		}
		if mediaBox := parentDict.Get("MediaBox"); mediaBox != nil {
			mediaBoxArr, ok := core.GetArray(mediaBox)
			if !ok {
				return nil, errors.New("invalid media box")
			}
			rect, err := NewPdfRectangle(*mediaBoxArr)
			if err != nil {
				return nil, err
			}
			return rect, nil
		}
		parent = parentDict.Get("Parent")
	}
	return nil, errors.New("media box not defined")
}

func (r *PdfReader) newPdfActionGoTo3DViewFromDict(dict *core.PdfObjectDictionary) (*PdfActionGoTo3DView, error) {
	return &PdfActionGoTo3DView{TA: dict.Get("TA"), V: dict.Get("V")}, nil
}

// ToPdfObject returns the text field dictionary within an indirect object (container).
func (f *PdfFieldText) ToPdfObject() core.PdfObject {
	f.PdfField.ToPdfObject()
	indirect := f.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.Set("FT", core.MakeName("Tx"))
	if f.DA != nil {
		dict.Set("DA", f.DA)
	}
	if f.Q != nil {
		dict.Set("Q", f.Q)
	}
	if f.DS != nil {
		dict.Set("DS", f.DS)
	}
	if f.RV != nil {
		dict.Set("RV", f.RV)
	}
	if f.MaxLen != nil {
		dict.Set("MaxLen", f.MaxLen)
	}
	return indirect
}

func (img *Image) canPassThroughJPEG() bool {
	return img.encodedFormat == "jpeg" && len(img.encodedData) > 0 && img.BitsPerComponent == 8 && (img.ColorComponents == 1 || img.ColorComponents == 3)
}

func (w *PdfWriter) optimizeDocument() error {
	if w.standardApplier == nil {
		return nil
	}
	infoObjDict, ok := core.GetDict(w.infoObj)
	if !ok {
		return errors.New("an info object is not a dictionary")
	}
	document := docutil.Document{ID: [2]string{w.id0, w.id1}, Version: w.version, Objects: w.objects, Info: infoObjDict, Crypt: w.crypter, UseHashBasedID: w.useHashBasedID}
	if err := w.standardApplier.ApplyStandard(&document); err != nil {
		return err
	}
	w.id0, w.id1 = document.ID[0], document.ID[1]
	w.version = document.Version
	w.objects = document.Objects
	w.infoObj.PdfObject = document.Info
	w.useHashBasedID = document.UseHashBasedID
	w.crypter = document.Crypt
	set := make(map[core.PdfObject]struct{}, len(w.objects))
	for _, object := range w.objects {
		set[object] = struct{}{}
	}
	w.hasObjects = set
	return nil
}

// PdfRectangle is a definition of a rectangle.
type PdfRectangle struct {
	Llx float64
	Lly float64
	Urx float64
	Ury float64
}
type pdfFontType0 struct {
	fontCommon
	primitive      *core.PdfIndirectObject
	encoder        textencoding.TextEncoder
	Encoding       core.PdfObject
	DescendantFont *PdfFont
	predefinedCMap *cmap.CMap
}

// Write outputs the object as it is to be written to file.
func (d *pdfSignDictionary) Write() []byte {
	d.contentsOffsetStart = 0
	d.contentsOffsetEnd = 0
	d.byteRangeOffsetStart = 0
	d.byteRangeOffsetEnd = 0
	buffer := bytes.NewBuffer(nil)
	buffer.WriteString("<<")
	for _, name := range d.Keys() {
		obj := d.Get(name)
		switch name {
		case "ByteRange":
			buffer.Write(name.Write())
			buffer.WriteString(" ")
			d.byteRangeOffsetStart = buffer.Len()
			buffer.Write(obj.Write())
			buffer.WriteString(" ")
			d.byteRangeOffsetEnd = buffer.Len() - 1
		case "Contents":
			buffer.Write(name.Write())
			buffer.WriteString(" ")
			d.contentsOffsetStart = buffer.Len()
			buffer.Write(obj.Write())
			buffer.WriteString(" ")
			d.contentsOffsetEnd = buffer.Len() - 1
		default:
			buffer.Write(name.Write())
			buffer.WriteString(" ")
			buffer.Write(obj.Write())
		}
	}
	buffer.WriteString(">>")
	return buffer.Bytes()
}

func (cs *PdfColorspaceDeviceGray) String() string {
	return "DeviceGray"
}

// NewPdfOutlineTree returns an initialized PdfOutline tree.
func NewPdfOutlineTree() *PdfOutline {
	outline := NewPdfOutline()
	outline.context = &outline
	return outline
}

// GetSamples converts the raw byte slice into samples which are stored in a uint32 bit array.
// Each sample is represented by BitsPerComponent consecutive bits in the raw data.
// NOTE: The method resamples the image byte data before returning the result and
// this could lead to high memory usage, especially on large images. It should
// be avoided, when possible. It is recommended to access the Data field of the
// image directly or use the ColorAt method to extract individual pixels.
func (img *Image) GetSamples() []uint32 {
	resampleBytes := sampling.ResampleBytes(img.Data, int(img.BitsPerComponent))
	if img.BitsPerComponent < 8 {
		resampleBytes = img.samplesTrimPadding(resampleBytes)
	}
	n := int(img.Width) * int(img.Height) * img.ColorComponents
	if len(resampleBytes) < n {
		common.Log.Debug("Error: Too few samples (got %d, expecting %d)", len(resampleBytes), n)
		return resampleBytes
	} else if len(resampleBytes) > n {
		common.Log.Debug("Error: Too many samples (got %d, expecting %d", len(resampleBytes), n)
		resampleBytes = resampleBytes[:n]
	}
	return resampleBytes
}

// ImageToRGB converts an Image in a given PdfColorspace to an RGB image.
func (cs *PdfColorspaceDeviceN) ImageToRGB(img Image) (Image, error) {
	reader := sampling.NewReader(img.getBase())
	imageBase := imageutil.NewImageBase(int(img.Width), int(img.Height), int(img.BitsPerComponent), img.ColorComponents, nil, img.alpha, img.decode)
	writer := sampling.NewWriter(imageBase)
	f := math.Pow(2, float64(img.BitsPerComponent)) - 1
	numComponents := cs.GetNumComponents()
	nums := make([]uint32, numComponents)
	floats := make([]float64, numComponents)
	for {
		err := reader.ReadSamples(nums)
		if err == io.EOF {
			break
		} else if err != nil {
			return img, err
		}
		for i := 0; i < numComponents; i++ {
			v := float64(nums[i]) / f
			floats[i] = v
		}
		evaluate, err := cs.TintTransform.Evaluate(floats)
		if err != nil {
			return img, err
		}
		for _, v := range evaluate {
			v = math.Min(math.Max(0, v), 1.0)
			if err = writer.WriteSample(uint32(v * f)); err != nil {
				return img, err
			}
		}
	}
	return cs.AlternateSpace.ImageToRGB(imageFromImageBase(&imageBase))
}

func (c fontCommon) isCIDFont() bool {
	if c.subtype == "" {
		common.Log.Debug("ERROR: isCIDFont. context is nil. font=%s", c)
	}
	ok := false
	switch c.subtype {
	case "Type0", "CIDFontType0", "CIDFontType2":
		ok = true
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("isCIDFont: isCID=%t font=%s", ok, c)
	}
	return ok
}

// PdfShadingType7 is a Tensor-product patch mesh.
type PdfShadingType7 struct {
	*PdfShading
	BitsPerCoordinate *core.PdfObjectInteger
	BitsPerComponent  *core.PdfObjectInteger
	BitsPerFlag       *core.PdfObjectInteger
	Decode            *core.PdfObjectArray
	Function          []PdfFunction
}

// NewPdfInfoFromObject creates a new PdfInfo from the input core.PdfObject.
func NewPdfInfoFromObject(obj core.PdfObject) (*PdfInfo, error) {
	var info PdfInfo
	dict, ok := obj.(*core.PdfObjectDictionary)
	if !ok {
		return nil, fmt.Errorf("invalid pdf object type: %T", obj)
	}
	for _, name := range dict.Keys() {
		switch name {
		case "Title":
			info.Title, _ = core.GetString(dict.Get("Title"))
		case "Author":
			info.Author, _ = core.GetString(dict.Get("Author"))
		case "Subject":
			info.Subject, _ = core.GetString(dict.Get("Subject"))
		case "Keywords":
			info.Keywords, _ = core.GetString(dict.Get("Keywords"))
		case "Creator":
			info.Creator, _ = core.GetString(dict.Get("Creator"))
		case "Producer":
			info.Producer, _ = core.GetString(dict.Get("Producer"))
		case "Trapped":
			info.Trapped, _ = core.GetName(dict.Get("Trapped"))
		case "CreationDate":
			if creationDate, ok := core.GetString(dict.Get("CreationDate")); ok && creationDate.String() != "" {
				date, err := NewPdfDate(creationDate.String())
				if err != nil {
					return nil, fmt.Errorf("invalid CreationDate field: %w", err)
				}
				info.CreationDate = &date
			}
		case "ModDate":
			if modDate, ok := core.GetString(dict.Get("ModDate")); ok && modDate.String() != "" {
				date, err := NewPdfDate(modDate.String())
				if err != nil {
					return nil, fmt.Errorf("invalid ModDate field: %w", err)
				}
				info.ModifiedDate = &date
			}
		default:
			str, _ := core.GetString(dict.Get(name))
			if info.dict == nil {
				info.dict = core.MakeDict()
			}
			info.dict.Set(name, str)
		}
	}
	return &info, nil
}

func (c certRevocation) settled() bool {
	return c.status == RevocationStatusGood || c.status == RevocationStatusRevoked && !c.onHold
}

func (t *trustEvaluator) evaluateRevocation(result *SignatureValidationResult, ok bool, evidence revocationEvidence) {
	result.RevocationStatus = RevocationStatusUnknown
	if len(result.TrustChain) < 2 {
		t.reportUnknownRevocation(result, "revocation status unknown: the signer certificate is a trust root and has no issuer to ask")
		return
	}
	checkChainRevocation := t.checkChainRevocation(result.TrustChain, result.ValidatedAt, ok, evidence)
	result.IsOcspFound = checkChainRevocation.leaf.ocspFound
	result.IsCrlFound = checkChainRevocation.leaf.crlFound
	result.RevocationStatus = checkChainRevocation.status
	switch checkChainRevocation.status {
	case RevocationStatusRevoked:
		result.RevocationTime = checkChainRevocation.revokedAt
		result.IsTrusted = false
		result.TrustErrors = append(result.TrustErrors, fmt.Sprintf("certificate %q revoked at %s", checkChainRevocation.revokedCert.Subject.String(), checkChainRevocation.revokedAt.UTC().Format(time.RFC3339)))
	case RevocationStatusGood:
		result.TrustWarnings = append(result.TrustWarnings, checkChainRevocation.warnings...)
	default:
		t.reportUnknownRevocation(result, append(checkChainRevocation.warnings, revocationUnknownMessage)...)
	}
}

// SignatureHandler interface defines the common functionality for PDF signature handlers, which
// need to be capable of validating digital signatures and signing PDF documents.
type SignatureHandler interface {
	// IsApplicable checks if a given signature dictionary `sig` is applicable for the signature handler.
	// For example a signature of type `adbe.pkcs7.detached` might not fit for a rsa.sha1 handler.
	IsApplicable(sig *PdfSignature) bool

	// Validate validates a PDF signature against a given digest (hash) such as that determined
	// for an input file. Returns validation results.
	Validate(sig *PdfSignature, hasher Hasher) (SignatureValidationResult, error)

	// InitSignature prepares the signature dictionary for signing. This involves setting all
	// necessary fields, and also allocating sufficient space to the Contents so that the
	// finalized signature can be inserted once the hash is calculated.
	InitSignature(sig *PdfSignature) error

	// NewDigest creates a new digest/hasher based on the signature dictionary and handler.
	NewDigest(sig *PdfSignature) (Hasher, error)

	// Sign receives the hash `digest` (for example hash of an input file), and signs based
	// on the signature dictionary `sig` and applies the signature data to the signature
	// dictionary Contents field.
	Sign(sig *PdfSignature, hasher Hasher) error
}

func flattenFieldTree(field *PdfField) []*PdfField {
	fields := []*PdfField{field}
	for _, kid := range field.Kids {
		fields = append(fields, flattenFieldTree(kid)...)
	}
	return fields
}

// HasXObjectByName checks if has XObject resource by name.
func (p *PdfPage) HasXObjectByName(name core.PdfObjectName) bool {
	dict, ok := p.Resources.XObject.(*core.PdfObjectDictionary)
	if !ok {
		return false
	}
	if obj := dict.Get(name); obj != nil {
		return true
	}
	return false
}

// NewPdfColorspaceSpecialIndexed returns a new Indexed color.
func NewPdfColorspaceSpecialIndexed() *PdfColorspaceSpecialIndexed {
	return &PdfColorspaceSpecialIndexed{HiVal: 255}
}

// SetDSS sets the DSS dictionary (ETSI TS 102 778-4 V1.1.1) of the current
// document revision.
func (a *PdfAppender) SetDSS(dss *DSS) {
	if dss != nil {
		a.updateObjectsDeep(dss.ToPdfObject(), nil)
	}
	a.dss = dss
}

var (
	ArtifactAttachPositionTop    ArtifactAttachPosition = "Top"
	ArtifactAttachPositionBottom ArtifactAttachPosition = "Bottom"
	ArtifactAttachPositionLeft   ArtifactAttachPosition = "Left"
	ArtifactAttachPositionRight  ArtifactAttachPosition = "Right"
)

func validateStencilMaskEncoder(encoder core.StreamEncoder) error {
	err := errors.New("DCT encoding cannot be used for 1-bit stencil masks")
	err2 := errors.New("predictor-based encoding cannot be used for 1-bit stencil masks")
	switch v := encoder.(type) {
	case *core.DCTEncoder:
		return err
	case *core.FlateEncoder:
		if v.Predictor > 1 {
			return err2
		}
	case *core.LZWEncoder:
		if v.Predictor > 1 {
			return err2
		}
	case *core.MultiEncoder:
		if strings.Contains(v.GetFilterName(), core.StreamEncodingFilterNameDCT) {
			return err
		}
		decodeParams := v.MakeDecodeParams()
		var dicts []*core.PdfObjectDictionary
		if decodeParamsDict, ok := core.GetDict(decodeParams); ok {
			dicts = append(dicts, decodeParamsDict)
		} else if decodeParamsArr, ok := core.GetArray(decodeParams); ok {
			for _, obj := range decodeParamsArr.Elements() {
				if dict, ok := core.GetDict(obj); ok {
					dicts = append(dicts, dict)
				}
			}
		}
		for _, dict := range dicts {
			if predictor, ok := core.GetIntVal(dict.Get("Predictor")); ok && predictor > 1 {
				return err2
			}
		}
	}
	return nil
}

// PdfColorCalGray represents a CalGray colorspace.
type PdfColorCalGray float64

// PdfFont represents an underlying font structure which can be of type:
// - Type0
// - Type1
// - TrueType
// etc.
type PdfFont struct{ context pdfFont }

// Field returns the parent form field of the widget annotation, if one exists.
// NOTE: the method returns nil if the parent form field has not been parsed.
func (a *PdfAnnotationWidget) Field() *PdfField { return a.field }

func getPdfAuthor() string { metadataMu.Lock(); defer metadataMu.Unlock(); return pdfAuthor }

func decodeDSSStream(stream *core.PdfObjectStream) ([]byte, bool) {
	data, err := core.DecodeStream(stream)
	if err != nil {
		common.Log.Debug("ERROR: skipping undecodable DSS stream: %v", err)
		return nil, false
	}
	return data, true
}

// AddImageResource adds an image to the XObject resources.
func (p *PdfPage) AddImageResource(name core.PdfObjectName, ximg *XObjectImage) error {
	var dict *core.PdfObjectDictionary
	if p.Resources.XObject == nil {
		dict = core.MakeDict()
		p.Resources.XObject = dict
	} else {
		var ok bool
		dict, ok = p.Resources.XObject.(*core.PdfObjectDictionary)
		if !ok {
			return errors.New("invalid xres dict type")
		}
	}
	dict.Set(name, ximg.ToPdfObject())
	return nil
}

// NewDSS returns a new DSS dictionary.
func NewDSS() *DSS {
	return &DSS{primitive: core.MakeIndirectObject(core.MakeDict()), VRI: map[string]*VRI{}}
}

func hasAcceptableKeyUsage(cert *x509.Certificate) bool {
	if len(cert.ExtKeyUsage) == 0 && len(cert.UnknownExtKeyUsage) == 0 {
		return true
	}
	for _, usage := range []x509.ExtKeyUsage{x509.ExtKeyUsageAny, x509.ExtKeyUsageEmailProtection, x509.ExtKeyUsageCodeSigning} {
		if hasExtKeyUsage(cert, usage) {
			return true
		}
	}
	for _, unknownExtKeyUsage := range cert.UnknownExtKeyUsage {
		for _, documentSigningEKUOID := range documentSigningEKUOIDs {
			if unknownExtKeyUsage.Equal(documentSigningEKUOID) {
				return true
			}
		}
	}
	return false
}

// ToPdfObject returns the PDF representation of the shading dictionary.
func (s *PdfShadingType4) ToPdfObject() core.PdfObject {
	s.PdfShading.ToPdfObject()
	shadingDict, err := s.getShadingDict()
	if err != nil {
		common.Log.Error("Unable to access shading dict")
		return nil
	}
	if s.BitsPerCoordinate != nil {
		shadingDict.Set("BitsPerCoordinate", s.BitsPerCoordinate)
	}
	if s.BitsPerComponent != nil {
		shadingDict.Set("BitsPerComponent", s.BitsPerComponent)
	}
	if s.BitsPerFlag != nil {
		shadingDict.Set("BitsPerFlag", s.BitsPerFlag)
	}
	if s.Decode != nil {
		shadingDict.Set("Decode", s.Decode)
	}
	if s.Function != nil {
		if len(s.Function) == 1 {
			shadingDict.Set("Function", s.Function[0].ToPdfObject())
		} else {
			array := core.MakeArray()
			for _, function := range s.Function {
				array.Append(function.ToPdfObject())
			}
			shadingDict.Set("Function", array)
		}
	}
	return s.primitive
}

type parsedCRL struct {
	*x509.RevocationList
	number *big.Int
	idp    *issuingDistributionPoint
	idpRaw []byte
	memo   map[[32]byte]bool
}

// ColorGlyphInfo represents information about a color glyph.
// This supports multiple color glyph formats found in modern fonts:
// - SVG-based color glyphs (emoji fonts)
// - Bitmap-based color glyphs (PNG, JPG, TIFF formats)
// - Future support for layered color glyphs (COLR/CPAL tables)
type ColorGlyphInfo struct {
	GlyphID      typefont.GID
	GlyphType    ColorGlyphType
	SVGData      []byte
	BitmapData   []byte
	BitmapFormat typefont.BitmapFormat
	BitmapWidth  int
	BitmapHeight int
	OutlineData  typefont.GlyphOutline
}

// PdfFilespec represents a file specification which can either refer to an external or embedded file.
type PdfFilespec struct {
	Type core.PdfObject
	FS   core.PdfObject
	F    core.PdfObject
	UF   core.PdfObject

	// DOS, Mac and Unix are obsolete as of PDF 2.0 (deprecated by PDF 1.7); the F and UF entries
	// should be used instead. They are still read and written here because they may appear in
	// existing files produced by older writers.
	DOS            core.PdfObject
	Mac            core.PdfObject
	Unix           core.PdfObject
	ID             core.PdfObject
	V              core.PdfObject
	EF             core.PdfObject
	RF             core.PdfObject
	Desc           core.PdfObject
	CI             core.PdfObject
	AFRelationship core.PdfObject
	primitive      core.PdfObject
}

// SetRefObject sets the reference object for the KValue.
func (kv *KValue) SetRefObject(refObject core.PdfObject) {
	kv.Clear()
	kv.refObject = refObject
}

// WriteToFile writes the Appender output to file specified by path.
func (a *PdfAppender) WriteToFile(outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()
	return a.Write(file)
}

// NewPdfAnnotationInk returns a new ink annotation.
func NewPdfAnnotationInk() *PdfAnnotationInk {
	annot := NewPdfAnnotation()
	ink := &PdfAnnotationInk{}
	ink.PdfAnnotation = annot
	ink.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(ink)
	return ink
}

func padBytes(data []byte, n int) []byte {
	if len(data) >= n {
		return data
	}
	data2 := make([]byte, n)
	count := copy(data2, data)
	for i := count; i < n; i++ {
		data2[i] = ' '
	}
	return data2
}

// NewPdfAnnotationFreeText returns a new free text annotation.
func NewPdfAnnotationFreeText() *PdfAnnotationFreeText {
	annot := NewPdfAnnotation()
	freeText := &PdfAnnotationFreeText{}
	freeText.PdfAnnotation = annot
	freeText.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(freeText)
	return freeText
}

// Decode returns the image's decode array ([Dmin Dmax ...] values for each
// color component), or nil when the default sample mapping applies. The
// returned slice must not be mutated; use SetDecode to change it.
func (img *Image) Decode() []float64 { return img.decode }

// GetContainingPdfObject returns the container of the outline tree node (indirect object).
func (n *PdfOutlineTreeNode) GetContainingPdfObject() core.PdfObject {
	return n.GetContext().GetContainingPdfObject()
}
func (cs *PdfColorspaceLab) String() string { return "Lab" }

// ColorFromPdfObjects loads the color from PDF objects.
// The first objects (if present) represent the color in underlying colorspace.  The last one represents
// the name of the pattern.
func (cs *PdfColorspaceSpecialPattern) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) < 1 {
		return nil, errors.New("invalid number of parameters")
	}
	patternColor := &PdfColorPattern{}
	name, ok := objects[len(objects)-1].(*core.PdfObjectName)
	if !ok {
		common.Log.Debug("Pattern name not a name (got %T)", objects[len(objects)-1])
		return nil, ErrTypeCheck
	}
	patternColor.PatternName = *name
	if len(objects) > 1 {
		objs := objects[0 : len(objects)-1]
		if cs.UnderlyingCS == nil {
			common.Log.Debug("Pattern color with defined color components but underlying cs missing")
			return nil, errors.New("underlying CS not defined")
		}
		col, err := cs.UnderlyingCS.ColorFromPdfObjects(objs)
		if err != nil {
			common.Log.Debug("ERROR: Unable to convert color via underlying cs: %v", err)
			return nil, err
		}
		patternColor.Color = col
	}
	return patternColor, nil
}

func parseCertificates(cert []*core.PdfObjectStream) []*x509.Certificate {
	var certs []*x509.Certificate
	for _, stream := range decodeStreams(cert) {
		certificate, err := x509.ParseCertificate(stream)
		if err != nil {
			common.Log.Debug("ERROR: skipping unparsable DSS certificate: %v", err)
			continue
		}
		certs = append(certs, certificate)
	}
	return certs
}
func (f *pdfCIDFontType2) baseFields() *fontCommon { return &f.fontCommon }

// GetSubFilter returns SubFilter value or empty string.
func (d *pdfSignDictionary) GetSubFilter() string {
	subFilter := d.Get("SubFilter")
	if subFilter == nil {
		return ""
	}
	if s, ok := core.GetNameVal(subFilter); ok {
		return s
	}
	return ""
}

func parseType1Encoding(s string) (map[textencoding.CharCode]textencoding.GlyphName, error) {
	split := strings.Split(s, "\n")
	glyphByCode := make(map[textencoding.CharCode]textencoding.GlyphName)
	for _, s := range split {
		stringSubmatch := type1EncodingEntryRegexp.FindStringSubmatch(s)
		if stringSubmatch == nil {
			continue
		}
		s2, s3 := stringSubmatch[1], stringSubmatch[2]
		atoi, err := strconv.Atoi(s2)
		if err != nil {
			common.Log.Debug("ERROR: Bad encoding line. %q", s)
			return nil, core.ErrTypeError
		}
		glyphByCode[textencoding.CharCode(atoi)] = textencoding.GlyphName(s3)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("getEncodings: keyValues=%#v", glyphByCode)
	}
	return glyphByCode, nil
}

func (f *PdfFilespec) getDict() *core.PdfObjectDictionary {
	if indirect, ok := f.primitive.(*core.PdfIndirectObject); ok {
		dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
		if !ok {
			return nil
		}
		return dict
	} else if dict, ok := f.primitive.(*core.PdfObjectDictionary); ok {
		return dict
	} else {
		common.Log.Debug("Trying to access Filespec dictionary of invalid object type (%T)", f.primitive)
		return nil
	}
}

// PageBoundary represents the name of the page boundary representing
// the visible area.
type PageBoundary string

// IDTree represents the ID tree dictionary where the format of the content
// is using Name Trees as described in chapter 7.9.6.
type IDTree struct {
	// Limits shall be an array of two strings, that shall specify the (lexically) least and greatest keys included in the Names array.
	Limits *core.PdfObjectArray

	// Names shall be an array of the form
	//
	// [ key1 value1 key2 value2 … keyn valuen]
	//
	// where each keyi shall be a string and the corresponding valuei shall be the object
	// associated with that key. The keys shall be sorted in lexical order, as described below.
	Names *core.PdfObjectArray

	// Kids Shall be an array of indirect references to the immediate children of this node.
	Kids []*IDTree
}

// GenerateHashMaps generates DSS hashmaps for Certificates, OCSPs and CRLs to make sure they are unique.
func (d *DSS) GenerateHashMaps() error {
	generateHashMap, err := d.generateHashMap(d.Certs)
	if err != nil {
		return err
	}
	streamByKey, err := d.generateHashMap(d.OCSPs)
	if err != nil {
		return err
	}
	key, err := d.generateHashMap(d.CRLs)
	if err != nil {
		return err
	}
	d.certsMap = generateHashMap
	d.ocspsMap = streamByKey
	d.crlsMap = key
	return nil
}
func (cs *PdfColorspaceICCBased) String() string { return "ICCBased" }
func (w *PdfWriter) getPdfVersion() string {
	return fmt.Sprintf("%d.%d", w.version.Major, w.version.Minor)
}

// PdfFontDescriptor specifies metrics and other attributes of a font and can refer to a FontFile
// for embedded fonts.
// 9.8 Font Descriptors (page 281)
type PdfFontDescriptor struct {
	FontName     core.PdfObject
	FontFamily   core.PdfObject
	FontStretch  core.PdfObject
	FontWeight   core.PdfObject
	Flags        core.PdfObject
	FontBBox     core.PdfObject
	ItalicAngle  core.PdfObject
	Ascent       core.PdfObject
	Descent      core.PdfObject
	Leading      core.PdfObject
	CapHeight    core.PdfObject
	XHeight      core.PdfObject
	StemV        core.PdfObject
	StemH        core.PdfObject
	AvgWidth     core.PdfObject
	MaxWidth     core.PdfObject
	MissingWidth core.PdfObject
	FontFile     core.PdfObject
	FontFile2    core.PdfObject
	FontFile3    core.PdfObject
	CharSet      core.PdfObject
	flags        int
	missingWidth float64
	*fontFile
	ttf *fonts.TtfType

	// Additional entries for CIDFonts
	Style     core.PdfObject
	Lang      core.PdfObject
	FD        core.PdfObject
	CIDSet    core.PdfObject
	primitive *core.PdfIndirectObject
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

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationWidget) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	if a.building {
		return indirect
	}
	a.building = true
	dict.SetIfNotNil("Subtype", core.MakeName("Widget"))
	dict.SetIfNotNil("H", a.H)
	dict.SetIfNotNil("MK", a.MK)
	dict.SetIfNotNil("A", a.A)
	dict.SetIfNotNil("AA", a.AA)
	dict.SetIfNotNil("BS", a.BS)
	parent := a.Parent
	if a.field != nil {
		if a.field.primitive == a.primitive {
			a.field.ToPdfObject()
		}
		parent = a.field.GetContainingPdfObject()
	}
	if parent != indirect {
		dict.SetIfNotNil("Parent", parent)
	}
	a.building = false
	return indirect
}

func buildXrefStream(plan *xrefStreamPlan) ([]byte, error) {
	var buf bytes.Buffer
	array := core.MakeArray()
	binary.Write(&buf, binary.BigEndian, byte(0))
	binary.Write(&buf, binary.BigEndian, uint32(0))
	binary.Write(&buf, binary.BigEndian, uint16(0xFFFF))
	array.Append(core.MakeInteger(0), core.MakeInteger(1))
	entries := append([]xrefEntry(nil), plan.entries...)
	for i := 1; i < len(entries); i++ {
		entry := entries[i]
		n := i - 1
		for n >= 0 && entries[n].objNum > entry.objNum {
			entries[n+1] = entries[n]
			n--
		}
		entries[n+1] = entry
	}
	for i := 0; i < len(entries); {
		objNum := entries[i].objNum
		n := i + 1
		for n < len(entries) && entries[n].objNum == entries[n-1].objNum+1 {
			n++
		}
		array.Append(core.MakeInteger(objNum), core.MakeInteger(int64(n-i)))
		for j := i; j < n; j++ {
			entry := entries[j]
			binary.Write(&buf, binary.BigEndian, entry.entryType)
			binary.Write(&buf, binary.BigEndian, entry.offset)
			binary.Write(&buf, binary.BigEndian, entry.genOrIndex)
		}
		i = n
	}
	stream, err := core.MakeStream(buf.Bytes(), core.NewFlateEncoder())
	if err != nil {
		return nil, fmt.Errorf("xref stream encode: %w", err)
	}
	stream.ObjectNumber = plan.objNum
	dict := stream.PdfObjectDictionary
	dict.Set("Type", core.MakeName("XRef"))
	dict.Set("W", core.MakeArray(core.MakeInteger(1), core.MakeInteger(4), core.MakeInteger(2)))
	dict.Set("Index", array)
	dict.Set("Size", core.MakeInteger(plan.size))
	if plan.root != nil {
		dict.Set("Root", plan.root)
	}
	if plan.info != nil {
		dict.Set("Info", plan.info)
	}
	if plan.encrypt != nil {
		dict.Set("Encrypt", plan.encrypt)
	}
	if plan.id != nil {
		dict.Set("ID", plan.id)
	}
	if plan.hasPrev {
		dict.Set("Prev", core.MakeInteger(plan.prev))
	}
	var buf2 bytes.Buffer
	fmt.Fprintf(&buf2, "%d 0 obj\n", plan.objNum)
	buf2.Write(dict.Write())
	buf2.WriteString("\nstream\n")
	buf2.Write(stream.Stream)
	buf2.WriteString("\nendstream\nendobj\n")
	return buf2.Bytes(), nil
}

// SetPickTrayByPDFSize sets the value of the pickTrayByPDFSize flag.
func (vp *ViewerPreferences) SetPickTrayByPDFSize(pickTrayByPDFSize bool) {
	vp.pickTrayByPDFSize = &pickTrayByPDFSize
}

// PdfField contains the common attributes of a form field. The context object contains the specific field data
// which can represent a button, text, choice or signature.
// The PdfField is typically not used directly, but is encapsulated by the more specific field types such as
// PdfFieldButton etc (i.e. the context attribute).
type PdfField struct {
	context      PdfModel
	primitive    *core.PdfIndirectObject
	Parent       *PdfField
	Annotations  []*PdfAnnotationWidget
	Kids         []*PdfField
	FT           *core.PdfObjectName
	T            *core.PdfObjectString
	TU           *core.PdfObjectString
	TM           *core.PdfObjectString
	Ff           *core.PdfObjectInteger
	V            core.PdfObject
	DV           core.PdfObject
	AA           core.PdfObject
	VariableText *VariableText
}

// RemovePage removes a page by number.
func (a *PdfAppender) RemovePage(pageNum int) {
	n := pageNum - 1
	a.pages = append(a.pages[0:n], a.pages[pageNum:]...)
}

func (cs *PdfColorspaceDeviceCMYK) String() string {
	return "DeviceCMYK"
}

// GetCharMetrics returns the char metrics for character code `code`.
// How it works:
//  1. It calls the GetCharMetrics function for the underlying font, either a simple font or
//     a Type0 font. The underlying font GetCharMetrics() functions do direct charcode ➞  metrics
//     mappings.
//  2. If the underlying font's GetCharMetrics() doesn't have a CharMetrics for `code` then a
//     a CharMetrics with the FontDescriptor's /MissingWidth is returned.
//  3. If there is no /MissingWidth then a failure is returned.
//
// TODO(peterwilliams97) There is nothing callers can do if no CharMetrics are found so we might as
// well give them 0 width. There is no need for the bool return.
//
// TODO(gunnsth): Reconsider whether needed or if can map via GlyphName.
func (f *PdfFont) GetCharMetrics(code textencoding.CharCode) (CharMetrics, bool) {
	var metrics fonts.CharMetrics
	switch v := f.context.(type) {
	case *pdfFontSimple:
		if charMetrics, ok := v.GetCharMetrics(code); ok {
			return charMetrics, ok
		}
	case *pdfFontType0:
		if charMetrics, ok := v.GetCharMetrics(code); ok {
			return charMetrics, ok
		}
	case *pdfCIDFontType0:
		if charMetrics, ok := v.GetCharMetrics(code); ok {
			return charMetrics, ok
		}
	case *pdfCIDFontType2:
		if charMetrics, ok := v.GetCharMetrics(code); ok {
			return charMetrics, ok
		}
	case *pdfFontType3:
		if charMetrics, ok := v.GetCharMetrics(code); ok {
			return charMetrics, ok
		}
	default:
		common.Log.Debug("ERROR: GetCharMetrics not implemented for font type=%T.", f.context)
		return metrics, false
	}
	if fontDescriptor, err := f.GetFontDescriptor(); err == nil && fontDescriptor != nil {
		return fonts.CharMetrics{Wx: fontDescriptor.missingWidth}, true
	}
	common.Log.Debug("GetCharMetrics: No metrics for font=%s", f)
	return metrics, false
}

// SetFontByName sets the font specified by keyName to the given object.
func (r *PdfPageResources) SetFontByName(keyName core.PdfObjectName, obj core.PdfObject) error {
	if r.Font == nil {
		r.Font = core.MakeDict()
	}
	dict, ok := core.TraceToDirectObject(r.Font).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Font not a dictionary! (got %T)", core.TraceToDirectObject(r.Font))
		return core.ErrTypeError
	}
	dict.Set(keyName, obj)
	return nil
}

// PdfAnnotationPolyLine represents PolyLine annotations.
// (Section 12.5.6.9).
type PdfAnnotationPolyLine struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	Vertices core.PdfObject
	LE       core.PdfObject
	BS       core.PdfObject
	IC       core.PdfObject
	BE       core.PdfObject
	IT       core.PdfObject
	Measure  core.PdfObject
}

// Insert adds a top level outline item in the outline,
// at the specified index.
func (o *Outline) Insert(index uint, item *OutlineItem) {
	n := uint(len(o.Entries))
	if index > n {
		index = n
	}
	o.Entries = append(o.Entries[:index], append([]*OutlineItem{item}, o.Entries[index:]...)...)
}

// GetXObjectFormByName returns the XObjectForm with the specified name from the
// page resources, if it exists.
func (r *PdfPageResources) GetXObjectFormByName(keyName core.PdfObjectName) (*XObjectForm, error) {
	xObjectByName, xObjectType := r.GetXObjectByName(keyName)
	if xObjectByName == nil {
		return nil, nil
	}
	if xObjectType != XObjectTypeForm {
		return nil, errors.New("not a form")
	}
	xObjectForm, err := NewXObjectFormFromStream(xObjectByName)
	if err != nil {
		return nil, err
	}
	return xObjectForm, nil
}

func (r *PdfReader) newPdfAnnotationCircleFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationCircle, error) {
	circle := PdfAnnotationCircle{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	circle.PdfAnnotationMarkup = markup
	circle.BS = dict.Get("BS")
	circle.IC = dict.Get("IC")
	circle.BE = dict.Get("BE")
	circle.RD = dict.Get("RD")
	return &circle, nil
}

// ColorFromPdfObjects returns a new PdfColor based on the input slice of color
// components. The slice should contain a single PdfObjectFloat element in
// range 0-1.
func (cs *PdfColorspaceCalGray) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) != 1 {
		return nil, errors.New("range check")
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objects)
	if err != nil {
		return nil, err
	}
	return cs.ColorFromFloats(numbersAsFloat)
}

// OutlineDest represents the destination of an outline item.
// It holds the page and the position on the page an outline item points to.
type OutlineDest struct {
	PageObj *core.PdfIndirectObject `json:"-"`
	Page    int64                   `json:"page"`
	Mode    string                  `json:"mode"`
	X       float64                 `json:"x"`
	Y       float64                 `json:"y"`
	Zoom    float64                 `json:"zoom"`
}

// BorderStyle defines border type, typically used for annotations.
type BorderStyle int

// GetCharMetrics returns the character metrics for the specified character code.  A bool flag is
// returned to indicate whether or not the entry was found in the glyph to charcode mapping.
// How it works:
//  1. Return a value the /Widths array (charWidths) if there is one.
//  2. If the font has the same name as a standard 14 font then return width=250.
//  3. Otherwise return no match and let the caller substitute a default.
func (f pdfFontSimple) GetCharMetrics(code textencoding.CharCode) (fonts.CharMetrics, bool) {
	if charWidth, ok := f.charWidths[code]; ok {
		return fonts.CharMetrics{Wx: charWidth}, true
	}
	if fonts.IsStdFont(fonts.StdFontName(f.baseFont)) {
		return fonts.CharMetrics{Wx: 250}, true
	}
	return fonts.CharMetrics{}, false
}

// PdfAnnotationWidget represents Widget annotations.
// Note: Widget annotations are used to display form fields.
// (Section 12.5.6.19).
type PdfAnnotationWidget struct {
	*PdfAnnotation
	H        core.PdfObject
	MK       core.PdfObject
	A        core.PdfObject
	AA       core.PdfObject
	BS       core.PdfObject
	Parent   core.PdfObject
	field    *PdfField
	building bool
}

// SetSamples convert samples to byte-data and sets for the image.
// NOTE: The method resamples the data and this could lead to high memory usage,
// especially on large images. It should be used only when it is not possible
// to work with the image byte data directly.
func (img *Image) SetSamples(samples []uint32) {
	if img.BitsPerComponent < 8 {
		samples = img.samplesAddPadding(samples)
	}
	resampleUint32 := sampling.ResampleUint32(samples, int(img.BitsPerComponent), 8)
	acquireBytes := core.AcquireBytes(len(resampleUint32))
	for i, n := range resampleUint32 {
		acquireBytes[i] = byte(n)
	}
	img.Data = acquireBytes
	img.clearSourceMetadata()
}

// PdfModel is a higher level PDF construct which can be collapsed into a PdfObject.
// Each PdfModel has an underlying PdfObject and vice versa (one-to-one).
// Under normal circumstances there should only be one copy of each.
// Copies can be made, but care must be taken to do it properly.
type PdfModel interface {
	ToPdfObject() core.PdfObject
	GetContainingPdfObject() core.PdfObject
}

// ColorFromPdfObjects returns a new PdfColor based on the input slice of color
// components. The slice should contain a single PdfObjectFloat element.
func (cs *PdfColorspaceSpecialSeparation) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) != 1 {
		return nil, errors.New("range check")
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objects)
	if err != nil {
		return nil, err
	}
	return cs.ColorFromFloats(numbersAsFloat)
}

// Components returns the component values of the underlying color, or nil if
// the pattern has no underlying color (e.g. an uncolored tiling pattern).
func (p *PdfColorPattern) Components() []float64 {
	if p.Color == nil {
		return nil
	}
	return p.Color.Components()
}

// FlattenFieldsWithOpts flattens the form field widgets and annotations of the
// page using the provided field appearance generator and the specified options.
// If opts is nil, all annotations on the page are flattened.
// Widget (form field) annotations are always flattened. Other annotations are
// flattened only when they pass opts.AnnotFilterFunc; when no filter is
// provided, every annotation is flattened. Annotations that are not flattened
// are preserved on the page.
func (p *PdfPage) FlattenFieldsWithOpts(appgen FieldAppearanceGenerator, opts *FieldFlattenOpts) error {
	if opts == nil {
		opts = &FieldFlattenOpts{}
	}
	annotations, err := p.GetAnnotations()
	if err != nil {
		return err
	}
	set := map[*PdfAnnotation]bool{}
	for _, annotation := range annotations {
		_, ok := annotation.GetContext().(*PdfAnnotationWidget)
		if ok || opts.AnnotFilterFunc == nil || opts.AnnotFilterFunc(annotation) {
			set[annotation] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	fieldFlattenOpts := *opts
	fieldFlattenOpts.AnnotFilterFunc = nil
	return p.flattenFieldsWithOpts(appgen, &fieldFlattenOpts, set)
}

// SetHideWindowUI sets the value of the hideWindowUI flag.
func (vp *ViewerPreferences) SetHideWindowUI(hideWindowUI bool) { vp.hideWindowUI = &hideWindowUI }

// Flags returns the field flags for the field accounting for any inherited flags.
func (f *PdfField) Flags() FieldFlag {
	var fieldFlag FieldFlag
	ok, err := f.inherit(func(field *PdfField) bool {
		if field.Ff != nil {
			fieldFlag = FieldFlag(*field.Ff)
			return true
		}
		return false
	})
	if err != nil {
		common.Log.Debug("Error evaluating flags via inheritance: %v", err)
	}
	if !ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("No field flags found - assume clear")
		}
	}
	return fieldFlag
}

// SignatureHandlerDocMDP extends SignatureHandler with the ValidateWithOpts method for checking the DocMDP policy.
type SignatureHandlerDocMDP interface {
	SignatureHandler

	// ValidateWithOpts validates a PDF signature by checking PdfReader or PdfParser
	// ValidateWithOpts shall contain Validate call
	ValidateWithOpts(sig *PdfSignature, hasher Hasher, signatureHandlerDocMDPParams SignatureHandlerDocMDPParams) (SignatureValidationResult, error)
}

// FullName returns the full name of the field as in rootname.parentname.partialname.
func (f *PdfField) FullName() (string, error) {
	var buf bytes.Buffer
	strs := []string{}
	if f.T != nil {
		strs = append(strs, f.T.Decoded())
	}
	set := map[*PdfField]bool{}
	set[f] = true
	parent := f.Parent
	for parent != nil {
		if _, ok := set[parent]; ok {
			return buf.String(), errors.New("recursive traversal")
		}
		if parent.T == nil {
			return buf.String(), errors.New("field partial name (T) not specified")
		}
		strs = append(strs, parent.T.Decoded())
		set[parent] = true
		parent = parent.Parent
	}
	for i := len(strs) - 1; i >= 0; i-- {
		buf.WriteString(strs[i])
		if i > 0 {
			buf.WriteString(".")
		}
	}
	return buf.String(), nil
}

// ToPdfObject returns the PDF representation of the colorspace.
func (cs *PdfColorspaceDeviceCMYK) ToPdfObject() core.PdfObject {
	return core.MakeName("DeviceCMYK")
}

// UpdatePage updates the `page` in the new revision if it has changed.
func (a *PdfAppender) UpdatePage(page *PdfPage) {
	a.updateObjectsDeep(page.ToPdfObject(), nil)
}

func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationScreen) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("Screen"))
	dict.SetIfNotNil("T", a.T)
	dict.SetIfNotNil("MK", a.MK)
	dict.SetIfNotNil("A", a.A)
	dict.SetIfNotNil("AA", a.AA)
	return indirect
}

func (f *PdfField) resolveMaxLen() int {
	var n int
	_, _ = f.inherit(func(field *PdfField) bool {
		textField, ok := field.GetContext().(*PdfFieldText)
		if !ok || textField.MaxLen == nil {
			return false
		}
		n = int(*textField.MaxLen)
		return true
	})
	return n
}

// PdfActionMovie represents a movie action.
type PdfActionMovie struct {
	*PdfAction
	Annotation core.PdfObject
	T          core.PdfObject
	Operation  core.PdfObject
}

// SetContentStreams sets the content streams based on a string array. Will make
// 1 object stream for each string and reference from the page Contents.
// Each stream will be encoded using the encoding specified by the StreamEncoder,
// if empty, will use identity encoding (raw data).
func (p *PdfPage) SetContentStreams(cStreams []string, encoder core.StreamEncoder) error {
	if len(cStreams) == 0 {
		p.Contents = nil
		return nil
	}
	if encoder == nil {
		encoder = core.NewRawEncoder()
	}
	var streams []*core.PdfObjectStream
	for _, cStream := range cStreams {
		stream := &core.PdfObjectStream{}
		streamDict := encoder.MakeStreamDict()
		encodeBytes, err := encoder.EncodeBytes([]byte(cStream))
		if err != nil {
			return err
		}
		streamDict.Set("Length", core.MakeInteger(int64(len(encodeBytes))))
		stream.PdfObjectDictionary = streamDict
		stream.Stream = encodeBytes
		streams = append(streams, stream)
	}
	if len(streams) == 1 {
		p.Contents = streams[0]
	} else {
		array := core.MakeArray()
		for _, stream := range streams {
			array.Append(stream)
		}
		p.Contents = array
	}
	return nil
}

func getPdfTitle() string {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	return pdfTitle
}

// GetNumComponents returns the number of color components of the colorspace device.
// Returns 4 for a CMYK32 device.
func (cs *PdfColorspaceDeviceCMYK) GetNumComponents() int { return 4 }

// PdfActionGoToR represents a GoToR action.
type PdfActionGoToR struct {
	*PdfAction
	F         *PdfFilespec
	D         core.PdfObject
	NewWindow core.PdfObject
}

// SetAction sets the PDF action for the annotation link.
func (a *PdfAnnotationLink) SetAction(action *PdfAction) {
	a.action = action
	if action == nil {
		a.A = nil
	}
}

func buildSharedObjectHintTable(layout *linearizationLayout, preObjByNum map[int64]preserializedObject, n, count int64) (*core.SharedObjectHintTable, error) {
	firstShared := layout.firstShared
	mainShared := layout.mainShared
	if len(firstShared) == 0 {
		return &core.SharedObjectHintTable{BitsPerGroupIdentifier: 1, BitsPerSharedGroupLengthDelta: 1}, nil
	}
	nums := make([]int64, 0, len(firstShared)+len(mainShared))
	nums = append(nums, firstShared...)
	nums = append(nums, mainShared...)
	var num uint32
	size := nums[0]
	if len(mainShared) > 0 {
		num = uint32(mainShared[0])
		size = mainShared[0]
	}
	length, err := adjustHintOffset(preObjByNum[size].offset, n, count)
	if err != nil {
		return nil, err
	}
	var length2 uint32
	var length3 uint32
	for i, n := range nums {
		length4 := uint32(len(preObjByNum[n].data))
		if i == 0 || length4 < length2 {
			length2 = length4
		}
		if length4 > length3 {
			length3 = length4
		}
	}
	sharedObjectHintTable := &core.SharedObjectHintTable{FirstSharedObjNum: num, FirstSharedOffset: length, NumSharedFirstPage: uint32(len(firstShared)), NumSharedTotal: uint32(len(nums)), BitsPerGroupIdentifier: 1, LeastSharedGroupLength: length2, BitsPerSharedGroupLengthDelta: bitsRequired(length3 - length2)}
	sharedObjectHintTable.Entries = make([]core.SharedObjectEntry, len(nums))
	for i, n := range nums {
		length3 := uint32(len(preObjByNum[n].data))
		sharedObjectHintTable.Entries[i] = core.SharedObjectEntry{GroupLengthDelta: length3 - length2, SignaturePresent: false, ObjectsInGroup: 1}
	}
	return sharedObjectHintTable, nil
}

// PdfAnnotationInk represents Ink annotations.
// (Section 12.5.6.13).
type PdfAnnotationInk struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	InkList core.PdfObject
	BS      core.PdfObject
}

// ToPdfObject converts the K dictionary to a PDF object.
func (k *KDict) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	if k.ID != nil {
		dict.Set("ID", k.ID)
	}
	if k.K != nil {
		dict.Set("K", k.K)
	} else if len(k.children) > 0 {
		if len(k.children) == 1 {
			k.K = k.children[0].ToPdfObject()
		} else {
			array := core.MakeArray()
			for _, children := range k.children {
				array.Append(children.ToPdfObject())
			}
			k.K = array
		}
		dict.Set("K", k.K)
	}
	if k.S != nil {
		dict.Set("S", k.S)
	}
	if k.P != nil {
		dict.Set("P", k.P)
	}
	if k.Pg != nil {
		dict.Set("Pg", k.Pg)
	}
	if k.C != nil {
		dict.Set("C", k.C)
	}
	if k.R != nil {
		dict.Set("R", k.R)
	}
	if k.T != nil {
		dict.Set("T", k.T)
	}
	if k.Lang != nil {
		dict.Set("Lang", k.Lang)
	}
	if k.Alt != nil {
		dict.Set("Alt", k.Alt)
	}
	if k.E != nil {
		dict.Set("E", k.E)
	}
	if k.A != nil {
		dict.Set("A", k.A)
	} else if k.boundingBox != nil {
		array := core.MakeArrayFromFloats([]float64{k.boundingBox.Llx, k.boundingBox.Lly, k.boundingBox.Urx, k.boundingBox.Ury})
		dict2 := core.MakeDict()
		dict2.Set("BBox", array)
		dict2.Set("O", core.MakeName("Layout"))
		dict.Set("A", core.MakeIndirectObject(dict2))
	}
	if k.ActualText != nil {
		dict.Set("ActualText", k.ActualText)
	}
	if k.E != nil {
		dict.Set("E", k.E)
	}
	if k.Alt != nil {
		dict.Set("Alt", k.Alt)
	}
	if k.NS != nil {
		dict.Set("NS", k.NS.ToPdfObject())
	}
	if k.AF != nil {
		dict.Set("AF", k.AF)
	}
	return dict
}

// NewPdfColorCalGray returns a new CalGray color.
func NewPdfColorCalGray(grayVal float64) *PdfColorCalGray {
	calGrayColor := PdfColorCalGray(grayVal)
	return &calGrayColor
}

// PdfVersion returns version of the PDF file.
func (r *PdfReader) PdfVersion() core.Version { return r.parser.PdfVersion() }

func (w *PdfWriter) writeLinearized(writer io.Writer) error {
	if err := w.prepareLinearizedDocument(); err != nil {
		return err
	}
	if len(w.pageObjs) == 0 {
		return fmt.Errorf("linearization: cannot linearize a document with no pages")
	}
	ok := w.linearizationOptions.UseObjectStreams
	w.adjustXRefAffectedVersion(ok)
	w.updateObjectNumbers()
	layout, err := w.computeLinearizationLayout()
	if err != nil {
		return err
	}
	reserveLinearizationObjectNumbers := w.reserveLinearizationObjectNumbers(layout)
	var objStm *objectStreamsState
	if ok {
		objStm, err = w.setupObjectStreams(layout)
		if err != nil {
			return err
		}
	}
	serializeForLinearization, err := w.serializeForLinearization(layout)
	if err != nil {
		return err
	}
	w.ensureLinearizedDocumentIDs(serializeForLinearization)
	plan, err := planLinearization(w, layout, serializeForLinearization, reserveLinearizationObjectNumbers, objStm)
	if err != nil {
		return err
	}
	w2 := bufio.NewWriter(writer)
	if err := plan.emit(w2); err != nil {
		return err
	}
	return w2.Flush()
}

func streamsToArray(streams []*core.PdfObjectStream) *core.PdfObjectArray {
	if len(streams) == 0 {
		return nil
	}
	objs := make([]core.PdfObject, 0, len(streams))
	for _, stream := range streams {
		objs = append(objs, stream)
	}
	return core.MakeArray(objs...)
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionRendition) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeRendition)))
	dict.SetIfNotNil("R", a.R)
	dict.SetIfNotNil("AN", a.AN)
	dict.SetIfNotNil("OP", a.OP)
	dict.SetIfNotNil("JS", a.JS)
	return indirect
}

// NonFullScreenPageMode returns the value of the nonFullScreenPageMode.
func (vp *ViewerPreferences) NonFullScreenPageMode() NonFullScreenPageMode {
	return vp.nonFullScreenPageMode
}

// NewPdfActionJavaScript returns a new "javaScript" action.
func NewPdfActionJavaScript() *PdfActionJavaScript {
	action := NewPdfAction()
	javaScriptAction := &PdfActionJavaScript{}
	javaScriptAction.PdfAction = action
	action.SetContext(javaScriptAction)
	return javaScriptAction
}

func newWidgetProperties(widget *PdfAnnotationWidget) WidgetProperties {
	var widgetProperties WidgetProperties
	widgetProperties.BorderColor, widgetProperties.BackgroundColor, widgetProperties.Caption, widgetProperties.Rotation = widgetMKProperties(widget)
	if widget == nil || widget.PdfAnnotation == nil {
		return widgetProperties
	}
	if rectArr, ok := core.GetArray(widget.Rect); ok {
		if rect, err := NewPdfRectangle(*rectArr); err == nil {
			widgetProperties.Rect = rect
		}
	}
	if name, ok := core.GetName(widget.AS); ok {
		widgetProperties.AppearanceState = string(*name)
	}
	return widgetProperties
}

type pdfFont interface {
	fonts.Font

	// ToPdfObject returns a PDF representation of the font and implements interface Model.
	ToPdfObject() core.PdfObject
	getFontDescriptor() *PdfFontDescriptor
	baseFields() *fontCommon
}

// FlattenFieldsWithOpts flattens the AcroForm fields of the reader using the
// provided field appearance generator and the specified options. If no options
// are specified, all form fields are flattened.
// If a filter function is provided using the opts parameter, only the filtered
// fields are flattened. Otherwise, all form fields are flattened.
// At the end of the process, the AcroForm contains all the fields which were
// not flattened. If all fields are flattened, the reader's AcroForm field
// is set to nil.
func (r *PdfReader) FlattenFieldsWithOpts(appgen FieldAppearanceGenerator, opts *FieldFlattenOpts) error {
	return r.flattenFieldsWithOpts(false, appgen, opts)
}

// PdfBorderEffect represents a PDF border effect.
type PdfBorderEffect struct {
	S *BorderEffect
	I *float64
}

var _ pdfFont = (*pdfFontType3)(nil)

// ColorFromFloats returns a new PdfColor based on the input slice of color
// components. The slice should contain three elements representing the
// red, green and blue components of the color. The values of the elements
// should be between 0 and 1.
func (cs *PdfColorspaceDeviceRGB) ColorFromFloats(vals []float64) (PdfColor, error) {
	if len(vals) != 3 {
		return nil, errors.New("range check")
	}
	val := vals[0]
	if val < 0.0 || val > 1.0 {
		common.Log.Debug("color out of CS range: ", val)
		return nil, ErrColorOutOfRange
	}
	f := vals[1]
	if f < 0.0 || f > 1.0 {
		common.Log.Debug("color out of CS range: ", val)
		return nil, ErrColorOutOfRange
	}
	v := vals[2]
	if v < 0.0 || v > 1.0 {
		common.Log.Debug("color out of CS range: ", val)
		return nil, ErrColorOutOfRange
	}
	deviceRGBColor := NewPdfColorDeviceRGB(val, f, v)
	return deviceRGBColor, nil
}

func (r *PdfReader) newPdfActionMovieFromDict(dict *core.PdfObjectDictionary) (*PdfActionMovie, error) {
	return &PdfActionMovie{Annotation: dict.Get("Annotation"), T: dict.Get("T"), Operation: dict.Get("Operation")}, nil
}

// Clear clears the KValue.
func (kv *KValue) Clear() { kv.kDict = nil; kv.refObject = nil; kv.mcid = nil }

// Items returns all children outline items.
func (o *Outline) Items() []*OutlineItem { return o.Entries }

// GetContainingPdfObject returns the containing object for the PdfField, i.e. an indirect object
// containing the field dictionary.
func (f *PdfField) GetContainingPdfObject() core.PdfObject { return f.primitive }

// DefaultImageHandler is the default implementation of the ImageHandler using the standard go library.
type DefaultImageHandler struct{}

// NewPdfActionSound returns a new "sound" action.
func NewPdfActionSound() *PdfActionSound {
	action := NewPdfAction()
	soundAction := &PdfActionSound{}
	soundAction.PdfAction = action
	action.SetContext(soundAction)
	return soundAction
}

// ImageToRGB converts an image in CMYK32 colorspace to an RGB image.
func (cs *PdfColorspaceDeviceCMYK) ImageToRGB(img Image) (Image, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("CMYK32 -> RGB")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Image BPC: %d, Color components: %d", img.BitsPerComponent, img.ColorComponents)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Len data: %d", len(img.Data))
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Height: %d, Width: %d", img.Height, img.Width)
	}
	img2, err := imageutil.NewImage(int(img.Width), int(img.Height), int(img.BitsPerComponent), img.ColorComponents, img.Data, img.alpha, img.decode)
	if err != nil {
		return Image{}, err
	}
	convert, err := imageutil.NRGBAConverter.Convert(img2)
	if err != nil {
		return Image{}, err
	}
	return imageFromImageBase(convert.Base()), nil
}

func (a *PdfAppender) addNewObject(obj core.PdfObject) {
	if _, ok := a.hasNewObject[obj]; !ok {
		a.newObjects = append(a.newObjects, obj)
		a.hasNewObject[obj] = struct{}{}
	}
}

func buildMainXrefStream(writer *PdfWriter, mainXrefObjNum int64, nums []int64, preObjByNum map[int64]preserializedObject, n int64, hintOffset int64, linDictOffset int64, mainXrefOffset int64, firstXrefOffset int64, count int64, objStm *objectStreamsState) ([]byte, error) {
	table := map[int64]int{}
	for i, packedObjNum := range objStm.packedObjNums {
		table[packedObjNum] = i
	}
	entries := make([]xrefEntry, 0, len(nums))
	for _, num := range nums {
		if n, ok := table[num]; ok {
			entries = append(entries, xrefEntry{objNum: num, entryType: 2, offset: uint32(objStm.streamObjNum), genOrIndex: uint16(n)})
			continue
		}
		var count int64
		switch num {
		case linearizationDictObjNum2:
			count = linDictOffset
		case n:
			count = hintOffset
		case objStm.firstXrefObjNum:
			count = firstXrefOffset
		case objStm.mainXrefObjNum:
			count = mainXrefOffset
		default:
			preObj, ok := preObjByNum[num]
			if !ok {
				return nil, fmt.Errorf("main xref stream: missing offset for obj %d", num)
			}
			count = preObj.offset
		}
		if count > math.MaxUint32 {
			return nil, fmt.Errorf("xref stream: object %d offset %d exceeds 4 GB (xref-stream offset field is 4 bytes)", num, count)
		}
		entries = append(entries, xrefEntry{objNum: num, entryType: 1, offset: uint32(count), genOrIndex: 0})
	}
	return buildXrefStream(&xrefStreamPlan{objNum: mainXrefObjNum, entries: entries, size: count + 1, hasPrev: false, root: writer.root, info: writer.infoObj, encrypt: encryptedWriterCatalog(writer), id: writer.ids})
}

// GetContainingPdfObject implements interface PdfModel.
func (r *PdfSignatureReference) GetContainingPdfObject() core.PdfObject { return r.primitive }

// NewPdfAcroForm returns a new PdfAcroForm with an initialized container (indirect object).
func NewPdfAcroForm() *PdfAcroForm {
	return &PdfAcroForm{Fields: &[]*PdfField{}, primitive: core.MakeIndirectObject(core.MakeDict())}
}

// ApplyStandard is used to apply changes required on the document to match the rules required by the input standard.
// The writer's content would be changed after all the document parts are already established during the Write method.
// A good example of the StandardApplier could be a PDF/A Profile (i.e.: pdfa.Profile1A). In such a case PdfWriter would
// set up all rules required by that Profile.
func (w *PdfWriter) ApplyStandard(optimizer StandardApplier) { w.standardApplier = optimizer }

// IsSimple returns true if `font` is a simple font.
func (f *PdfFont) IsSimple() bool { _, ok := f.context.(*pdfFontSimple); return ok }

// IsRadio returns true if the button field represents a radio button, false otherwise.
func (f *PdfFieldButton) IsRadio() bool { return f.GetType() == ButtonTypeRadio }

func (r *PdfReader) newPdfAnnotationStrikeOut(dict *core.PdfObjectDictionary) (*PdfAnnotationStrikeOut, error) {
	strikeOut := PdfAnnotationStrikeOut{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	strikeOut.PdfAnnotationMarkup = markup
	strikeOut.QuadPoints = dict.Get("QuadPoints")
	return &strikeOut, nil
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionGoToE) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeGoToE)))
	if a.F != nil {
		dict.Set("F", a.F.ToPdfObject())
	}
	dict.SetIfNotNil("D", a.D)
	dict.SetIfNotNil("NewWindow", a.NewWindow)
	dict.SetIfNotNil("T", a.T)
	return indirect
}

func (a *PdfAnnotationMarkup) appendToPdfDictionary(dict *core.PdfObjectDictionary) {
	dict.SetIfNotNil("T", a.T)
	if a.Popup != nil {
		dict.Set("Popup", a.Popup.ToPdfObject())
	}
	dict.SetIfNotNil("CA", a.CA)
	dict.SetIfNotNil("RC", a.RC)
	dict.SetIfNotNil("CreationDate", a.CreationDate)
	dict.SetIfNotNil("IRT", a.IRT)
	dict.SetIfNotNil("Subj", a.Subj)
	dict.SetIfNotNil("RT", a.RT)
	dict.SetIfNotNil("IT", a.IT)
	dict.SetIfNotNil("ExData", a.ExData)
}

func (r *PdfReader) newPdfAnnotationFreeTextFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationFreeText, error) {
	freeText := PdfAnnotationFreeText{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	freeText.PdfAnnotationMarkup = markup
	freeText.DA = dict.Get("DA")
	freeText.Q = dict.Get("Q")
	freeText.RC = dict.Get("RC")
	freeText.DS = dict.Get("DS")
	freeText.CL = dict.Get("CL")
	freeText.IT = dict.Get("IT")
	freeText.BE = dict.Get("BE")
	freeText.RD = dict.Get("RD")
	freeText.BS = dict.Get("BS")
	freeText.LE = dict.Get("LE")
	return &freeText, nil
}

// NewPdfActionGoTo returns a new "go to" action.
func NewPdfActionGoTo() *PdfActionGoTo {
	action := NewPdfAction()
	goToAction := &PdfActionGoTo{}
	goToAction.PdfAction = action
	action.SetContext(goToAction)
	return goToAction
}

// IsFitWindow returns the value of the fitWindow flag.
func (vp *ViewerPreferences) IsFitWindow() bool {
	if vp.fitWindow == nil {
		return false
	}
	return *vp.fitWindow
}

// GetRuneMetrics returns the character metrics for the rune.
// A bool flag is returned to indicate whether or not the entry was found.
func (f pdfFontSimple) GetRuneMetrics(r rune) (fonts.CharMetrics, bool) {
	if f.fontMetrics != nil {
		metrics, ok := f.fontMetrics.Read(r)
		if ok {
			return metrics, true
		}
	}
	encoder := f.Encoder()
	if encoder == nil {
		common.Log.Debug("No encoder for fonts=%s", f)
		return fonts.CharMetrics{}, false
	}
	runeToCharcode, ok := encoder.RuneToCharcode(r)
	if !ok {
		if r != ' ' {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("No charcode for rune=%v font=%s", r, f)
			}
		}
		return fonts.CharMetrics{}, false
	}
	charMetrics, isCharMetrics := f.GetCharMetrics(runeToCharcode)
	return charMetrics, isCharMetrics
}

// SetFillImage attach a model.Image to push button.
func (f *PdfFieldButton) SetFillImage(image *Image) {
	if f.IsPush() {
		f.buttonImage = image
	}
}

// SetHideMenubar sets the value of the hideMenubar flag.
func (vp *ViewerPreferences) SetHideMenubar(hideMenubar bool) { vp.hideMenubar = &hideMenubar }

// SetDate sets the `M` field of the signature.
func (s *PdfSignature) SetDate(date time.Time, format string) {
	if format == "" {
		format = "D:20060102150405-07'00'"
	}
	s.M = core.MakeString(date.Format(format))
}

func newPdfShadingFromPdfObject(obj core.PdfObject) (*PdfShading, error) {
	shading := &PdfShading{}
	var dict *core.PdfObjectDictionary
	if indirect, ok := core.GetIndirect(obj); ok {
		shading.primitive = indirect
		dict2, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
		if !ok {
			common.Log.Debug("Object not a dictionary type")
			return nil, core.ErrTypeError
		}
		dict = dict2
	} else if stream, ok := core.GetStream(obj); ok {
		shading.primitive = stream
		dict = stream.PdfObjectDictionary
	} else if dict2, ok := core.GetDict(obj); ok {
		shading.primitive = dict2
		dict = dict2
	} else {
		common.Log.Debug("Object type unexpected (%T)", obj)
		return nil, core.ErrTypeError
	}
	if dict == nil {
		common.Log.Debug("Dictionary missing")
		return nil, errors.New("dict missing")
	}
	obj = dict.Get("ShadingType")
	if obj == nil {
		common.Log.Debug("Required shading type missing")
		return nil, ErrRequiredAttributeMissing
	}
	obj = core.TraceToDirectObject(obj)
	num, ok := obj.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("Invalid type for shading type (%T)", obj)
		return nil, core.ErrTypeError
	}
	if *num < 1 || *num > 7 {
		common.Log.Debug("Invalid shading type, not 1-7 (got %d)", *num)
		return nil, core.ErrTypeError
	}
	shading.ShadingType = num
	obj = dict.Get("ColorSpace")
	if obj == nil {
		common.Log.Debug("Required ColorSpace entry missing")
		return nil, ErrRequiredAttributeMissing
	}
	cs, err := NewPdfColorspaceFromPdfObject(obj)
	if err != nil {
		common.Log.Debug("Failed loading colorspace: %v", err)
		return nil, err
	}
	shading.ColorSpace = cs
	obj = dict.Get("Background")
	if obj != nil {
		obj = core.TraceToDirectObject(obj)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Background should be specified by an array (got %T)", obj)
			return nil, core.ErrTypeError
		}
		shading.Background = arr
	}
	obj = dict.Get("BBox")
	if obj != nil {
		obj = core.TraceToDirectObject(obj)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Background should be specified by an array (got %T)", obj)
			return nil, core.ErrTypeError
		}
		rect, err := NewPdfRectangle(*arr)
		if err != nil {
			common.Log.Debug("BBox error: %v", err)
			return nil, err
		}
		shading.BBox = rect
	}
	obj = dict.Get("AntiAlias")
	if obj != nil {
		obj = core.TraceToDirectObject(obj)
		boolObj, ok := obj.(*core.PdfObjectBool)
		if !ok {
			common.Log.Debug("AntiAlias invalid type, should be bool (got %T)", obj)
			return nil, core.ErrTypeError
		}
		shading.AntiAlias = boolObj
	}
	switch *num {
	case 1:
		shadingType1, err := newPdfShadingType1FromDict(dict)
		if err != nil {
			return nil, err
		}
		shadingType1.PdfShading = shading
		shading.context = shadingType1
		return shading, nil
	case 2:
		shadingType2, err := newPdfShadingType2FromDict(dict)
		if err != nil {
			return nil, err
		}
		shadingType2.PdfShading = shading
		shading.context = shadingType2
		return shading, nil
	case 3:
		shadingType3, err := newPdfShadingType3FromDict(dict)
		if err != nil {
			return nil, err
		}
		shadingType3.PdfShading = shading
		shading.context = shadingType3
		return shading, nil
	case 4:
		shadingType4, err := newPdfShadingType4FromDict(dict)
		if err != nil {
			return nil, err
		}
		shadingType4.PdfShading = shading
		shading.context = shadingType4
		return shading, nil
	case 5:
		shadingType5, err := newPdfShadingType5FromDict(dict)
		if err != nil {
			return nil, err
		}
		shadingType5.PdfShading = shading
		shading.context = shadingType5
		return shading, nil
	case 6:
		shadingType6, err := newPdfShadingType6FromDict(dict)
		if err != nil {
			return nil, err
		}
		shadingType6.PdfShading = shading
		shading.context = shadingType6
		return shading, nil
	case 7:
		shadingType7, err := newPdfShadingType7FromDict(dict)
		if err != nil {
			return nil, err
		}
		shadingType7.PdfShading = shading
		shading.context = shadingType7
		return shading, nil
	}
	return nil, errors.New("unknown shading type")
}

var (
	ErrRequiredAttributeMissing = errors.New("required attribute missing")
	ErrInvalidAttribute         = errors.New("invalid attribute")
	ErrTypeCheck                = errors.New("type check")
	errRangeCheck               = errors.New("range check error")
	ErrEncrypted                = errors.New("file needs to be decrypted first")
	ErrNoFont                   = errors.New("font not defined")
	ErrFontNotSupported         = xerrors.Errorf("unsupported font (%w)", core.ErrNotSupported)
	ErrType1CFontNotSupported   = xerrors.Errorf("Type1C fonts are not currently supported (%w)", core.ErrNotSupported)
	ErrType3FontNotSupported    = xerrors.Errorf("Type3 fonts are not currently supported (%w)", core.ErrNotSupported)
	ErrTTCmapNotSupported       = xerrors.Errorf("unsupported TrueType cmap format (%w)", core.ErrNotSupported)
	ErrSignNotEnoughSpace       = xerrors.Errorf("insufficient space allocated for the signature contents")
	ErrSignNoCertificates       = xerrors.Errorf("could not retrieve certificate chain")
	ErrNoCertClient             = xerrors.Errorf("cert client is not set")
	ErrNoCrlClient              = xerrors.Errorf("crl client is not set")
	ErrNoOcspClient             = xerrors.Errorf("ocsp client is not set")
)

func newPdfFontType3FromDict(dict *core.PdfObjectDictionary, fc *fontCommon) (*pdfFontType3, error) {
	fontType3 := newPdfFontType3FromCommon(fc)
	firstChar := dict.Get("FirstChar")
	if firstChar == nil {
		firstChar = core.MakeInteger(0)
	}
	fontType3.FirstChar = firstChar
	n, ok := core.GetIntVal(firstChar)
	if !ok {
		common.Log.Debug("ERROR: Invalid FirstChar type (%T)", firstChar)
		return nil, core.ErrTypeError
	}
	code := textencoding.CharCode(n)
	firstChar = dict.Get("LastChar")
	if firstChar == nil {
		firstChar = core.MakeInteger(255)
	}
	fontType3.LastChar = firstChar
	n, ok = core.GetIntVal(firstChar)
	if !ok {
		common.Log.Debug("ERROR: Invalid LastChar type (%T)", firstChar)
		return nil, core.ErrTypeError
	}
	code2 := textencoding.CharCode(n)
	firstChar = dict.Get("Resources")
	if firstChar != nil {
		fontType3.Resources = firstChar
	}
	firstChar = dict.Get("CharProcs")
	if firstChar == nil {
		common.Log.Debug("ERROR: Invalid CharProcs (%v)", firstChar)
		return nil, core.ErrNotSupported
	}
	fontType3.CharProcs = firstChar
	firstChar = dict.Get("FontMatrix")
	if firstChar == nil {
		common.Log.Debug("ERROR: Invalid FontMatrix (%v)", firstChar)
		return nil, core.ErrNotSupported
	}
	fontType3.FontMatrix = firstChar
	fontType3.charWidths = make(map[textencoding.CharCode]float64)
	firstChar = dict.Get("Widths")
	if firstChar != nil {
		fontType3.Widths = firstChar
		firstCharArr, ok := core.GetArray(firstChar)
		if !ok {
			common.Log.Debug("ERROR: Widths attribute != array (%T)", firstChar)
			return nil, core.ErrTypeError
		}
		floats, err := firstCharArr.ToFloat64Array()
		if err != nil {
			common.Log.Debug("ERROR: converting widths to array")
			return nil, err
		}
		if len(floats) != int(code2-code+1) {
			common.Log.Debug("ERROR: Invalid widths length != %d (%d)", code2-code+1, len(floats))
			return nil, core.ErrRangeError
		}
		fontMatrixArr, ok := core.GetArray(fontType3.FontMatrix)
		if !ok {
			common.Log.Debug("ERROR: FontMatrix attribute != array (%T)", fontMatrixArr)
			return nil, err
		}
		floats2, err := fontMatrixArr.ToFloat64Array()
		if err != nil {
			common.Log.Debug("ERROR: converting FontMatrix to array")
			return nil, err
		}
		matrix := transform.NewMatrix(floats2[0], floats2[1], floats2[2], floats2[3], floats2[4], floats2[5])
		for i, float := range floats {
			f, _ := matrix.Transform(float, float)
			fontType3.charWidths[code+textencoding.CharCode(i)] = f
		}
	}
	fontType3.Encoding = core.TraceToDirectObject(dict.Get("Encoding"))
	toUnicode := dict.Get("ToUnicode")
	if toUnicode != nil {
		fontType3.toUnicode = core.TraceToDirectObject(toUnicode)
		cm, err := toUnicodeCMapFromObject(fontType3.toUnicode, &fontType3.fontCommon)
		if err != nil {
			return nil, err
		}
		fontType3.toUnicodeCMap = cm
	}
	if toUnicodeCMap := fontType3.toUnicodeCMap; toUnicodeCMap != nil {
		fontType3.encoder = textencoding.NewCMapEncoder("", nil, toUnicodeCMap)
	} else {
		fontType3.encoder = textencoding.NewPdfDocEncoder()
	}
	return fontType3, nil
}

// ColorGlyphType indicates the type of color glyph data available.
// Different font formats use different approaches to store color information.
type ColorGlyphType int

// ToPdfObject returns the PDF representation of the colorspace.
func (cs *PdfColorspaceDeviceRGB) ToPdfObject() core.PdfObject { return core.MakeName("DeviceRGB") }

const (
	NonFullScreenPageModeUseNone     NonFullScreenPageMode = "UseNone"
	NonFullScreenPageModeUseOutlines NonFullScreenPageMode = "UseOutlines"
	NonFullScreenPageModeUseThumbs   NonFullScreenPageMode = "UseThumbs"
	NonFullScreenPageModeUseOC       NonFullScreenPageMode = "UseOC"
	DirectionL2R                     Direction             = "L2R"
	DirectionR2L                     Direction             = "R2L"
	PageBoundaryMediaBox             PageBoundary          = "MediaBox"
	PageBoundaryCropBox              PageBoundary          = "CropBox"
	PageBoundaryBleedBox             PageBoundary          = "BleedBox"
	PageBoundaryTrimBox              PageBoundary          = "TrimBox"
	PageBoundaryArtBox               PageBoundary          = "ArtBox"
	PrintScalingNone                 PrintScaling          = "None"
	PrintScalingAppDefault           PrintScaling          = "AppDefault"
	DuplexNone                       Duplex                = "none"
	DuplexSimplex                    Duplex                = "Simplex"
	DuplexFlipShortEdge              Duplex                = "DuplexFlipShortEdge"
	DuplexFlipLongEdge               Duplex                = "DuplexFlipLongEdge"
)

func planLinearizationPass(writer *PdfWriter, layout *linearizationLayout, preObjByNum map[int64]preserializedObject, n int64, objStm *objectStreamsState, data []byte, count int, nums []int64, nums2 []int64, num int64, size int, length int, length2 int) (*linearizedPlan, planSizes, error) {
	plan := &linearizedPlan{header: append([]byte(nil), data...)}
	length3 := int64(len(data))
	plan.linDictOffset = length3
	length3 += int64(count)
	plan.firstXrefOffset = length3
	var length4 int
	if objStm == nil {
		const placeholderOffset = int64(9999999999)
		length4 = measureXrefTable(writer, nums, num, placeholderOffset, true, false)
	} else {
		length4 = length
		if length4 < 0 {
			length4 = 256 + 7*len(nums)/2
		}
	}
	length3 += int64(length4)
	plan.hintOffset = length3
	length5 := size
	if length5 < 0 {
		length5 = 256 + 32*len(layout.firstSection) + 16*len(layout.sharedObjs)
	}
	length3 += int64(length5)
	length6 := length3
	table := make(map[int64]int64, len(layout.firstSection))
	for _, firstSection := range layout.firstSection {
		preObj, ok := preObjByNum[firstSection]
		if !ok {
			return nil, planSizes{}, fmt.Errorf("plan: missing serialized object %d", firstSection)
		}
		preObj.offset = length3
		preObjByNum[firstSection] = preObj
		table[firstSection] = length3
		length3 += int64(len(preObj.data))
	}
	plan.firstPageEnd = length3
	length7 := length3
	for _, mainSection := range layout.mainSection {
		preObj, ok := preObjByNum[mainSection]
		if !ok {
			return nil, planSizes{}, fmt.Errorf("plan: missing serialized object %d", mainSection)
		}
		preObj.offset = length3
		preObjByNum[mainSection] = preObj
		length3 += int64(len(preObj.data))
	}
	plan.mainXrefOffset = length3
	var data2 []byte
	var length8 int64
	if objStm == nil {
		data2, length8 = buildMainXrefTable(writer, nums2, num, preObjByNum, n, plan.hintOffset, plan.mainXrefOffset, plan.linDictOffset, plan.firstXrefOffset)
	} else {
		length9 := length2
		if length9 < 0 {
			length9 = 256 + 7*len(nums2)/2
		}
		data2 = make([]byte, length9)
		length8 = plan.mainXrefOffset
	}
	plan.mainXref = data2
	plan.mainXrefFirstEntry = length8
	length3 += int64(len(data2))
	if objStm != nil {
		length3 += int64(trailerOverheadBytes)
	}
	plan.fileLength = length3
	hintStream, length9, err := buildHintStream(layout, table, preObjByNum, plan.hintOffset, int64(length5))
	if err != nil {
		return nil, planSizes{}, err
	}
	hintStreamObject, err := buildHintStreamObject(n, hintStream, length9)
	if err != nil {
		return nil, planSizes{}, err
	}
	plan.hintStream = hintStreamObject
	length10 := len(hintStreamObject)
	length11 := length4
	length12 := len(data2)
	if objStm != nil {
		length13 := len(data2)
		firstXrefStream, err := buildFirstXrefStream(writer, objStm.firstXrefObjNum, nums, preObjByNum, n, plan.hintOffset, plan.linDictOffset, plan.firstXrefOffset, plan.mainXrefOffset, num, objStm)
		if err != nil {
			return nil, planSizes{}, err
		}
		plan.firstXref = padBytes(firstXrefStream, length4)
		length11 = len(plan.firstXref)
		mainXrefStream, err := buildMainXrefStream(writer, objStm.mainXrefObjNum, nums2, preObjByNum, n, plan.hintOffset, plan.linDictOffset, plan.mainXrefOffset, plan.firstXrefOffset, num, objStm)
		if err != nil {
			return nil, planSizes{}, err
		}
		plan.mainXref = padBytes(mainXrefStream, length13)
		length12 = len(plan.mainXref)
	}
	sizes := planSizes{hintLen: length10, firstXrefLen: length11, mainXrefLen: length12}
	if length10 != length5 || length11 != length4 || length12 != len(data2) {
		return plan, sizes, nil
	}
	if err := finalizeLinearizationDict(writer, plan, convergedPlanInputs{layout: layout, objects: preObjByNum, objStreams: objStm, hintObjNum: n, firstXrefObjs: nums, trailerSize: num, linDictLen: count, firstXrefLen: length4, hintLen: length10, xrefFirstEntryOffset: length8, firstSectionStart: length6, mainSectionStart: length7}); err != nil {
		return nil, planSizes{}, err
	}
	return plan, sizes, nil
}

type trustEvaluator struct {
	reader            *PdfReader
	opts              *SignatureValidationOpts
	dssCerts          []*x509.Certificate
	dssCertsLoaded    bool
	dssEvidence       revocationEvidence
	dssEvidenceLoaded bool
	crlByHash         map[[32]byte]*parsedCRL
	crlByStream       map[*core.PdfObjectStream]*parsedCRL
	ocspCache         map[ocspKey]*ocsp.Response
	fetchCache        map[fetchKey]fetchResult
}

// IsEncrypted returns true if the PDF file is encrypted.
func (r *PdfReader) IsEncrypted() (bool, error) { return r.parser.IsEncrypted() }

// GetContext returns the annotation context which contains the specific type-dependent context.
// The context represents the subannotation.
func (a *PdfAnnotation) GetContext() PdfModel {
	if a == nil {
		return nil
	}
	return a.context
}

func buildLinearizationDict(n int64, fileLength int64, hintOffset int64, count int64, firstPageEnd int64, pageCount int, xrefFirstEntryOffset int64, firstPageObjNum int64) []byte {
	s := fmt.Sprintf("%d 0 obj\n<</Linearized 1/L %d/H[%d %d]/O %d/E %d/N %d/T %d", n, fileLength, hintOffset, count, firstPageObjNum, firstPageEnd, pageCount, xrefFirstEntryOffset)
	s2 := fmt.Sprintf("%d 0 obj\n<</Linearized 1/L %010d/H[%010d %010d]/O %010d/E %010d/N %010d/T %010d", n, fileLength, hintOffset, count, firstPageObjNum, firstPageEnd, pageCount, xrefFirstEntryOffset)
	num := len(s2) - len(s)
	if num < 0 {
		num = 0
	}
	var buf bytes.Buffer
	buf.WriteString(s)
	for i := 0; i < num; i++ {
		buf.WriteByte(' ')
	}
	buf.WriteString(">>\nendobj\n")
	return buf.Bytes()
}

// ToPdfObject returns the PdfFontDescriptor as a PDF dictionary inside an indirect object.
func (d *PdfFontDescriptor) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	if d.primitive == nil {
		d.primitive = &core.PdfIndirectObject{}
	}
	d.primitive.PdfObject = dict
	dict.Set("Type", core.MakeName("FontDescriptor"))
	if d.FontName != nil {
		dict.Set("FontName", d.FontName)
	}
	if d.FontFamily != nil {
		dict.Set("FontFamily", d.FontFamily)
	}
	if d.FontStretch != nil {
		dict.Set("FontStretch", d.FontStretch)
	}
	if d.FontWeight != nil {
		dict.Set("FontWeight", d.FontWeight)
	}
	if d.Flags != nil {
		dict.Set("Flags", d.Flags)
	}
	if d.FontBBox != nil {
		dict.Set("FontBBox", d.FontBBox)
	}
	if d.ItalicAngle != nil {
		dict.Set("ItalicAngle", d.ItalicAngle)
	}
	if d.Ascent != nil {
		dict.Set("Ascent", d.Ascent)
	}
	if d.Descent != nil {
		dict.Set("Descent", d.Descent)
	}
	if d.Leading != nil {
		dict.Set("Leading", d.Leading)
	}
	if d.CapHeight != nil {
		dict.Set("CapHeight", d.CapHeight)
	}
	if d.XHeight != nil {
		dict.Set("XHeight", d.XHeight)
	}
	if d.StemV != nil {
		dict.Set("StemV", d.StemV)
	}
	if d.StemH != nil {
		dict.Set("StemH", d.StemH)
	}
	if d.AvgWidth != nil {
		dict.Set("AvgWidth", d.AvgWidth)
	}
	if d.MaxWidth != nil {
		dict.Set("MaxWidth", d.MaxWidth)
	}
	if d.MissingWidth != nil {
		dict.Set("MissingWidth", d.MissingWidth)
	}
	if d.FontFile != nil {
		dict.Set("FontFile", d.FontFile)
	}
	if d.FontFile2 != nil {
		dict.Set("FontFile2", d.FontFile2)
	}
	if d.FontFile3 != nil {
		dict.Set("FontFile3", d.FontFile3)
	}
	if d.CharSet != nil {
		dict.Set("CharSet", d.CharSet)
	}
	if d.Style != nil {
		dict.Set("FontName", d.FontName)
	}
	if d.Lang != nil {
		dict.Set("Lang", d.Lang)
	}
	if d.FD != nil {
		dict.Set("FD", d.FD)
	}
	if d.CIDSet != nil {
		dict.Set("CIDSet", d.CIDSet)
	}
	return d.primitive
}

func (a *PdfAcroForm) filteredFields(filterFunc FieldFilterFunc, ok bool) []*PdfField {
	if a == nil {
		return nil
	}
	return filterFieldsRecursive(a.Fields, filterFunc, ok)
}

func (t *trustEvaluator) embeddedRevocationEvidence(p7 *pkcs7.PKCS7, s string) revocationEvidence {
	var evidence revocationEvidence
	if p7 != nil {
		var revocationInfoArchival sigattr.RevocationInfoArchival
		if err := p7.UnmarshalSignedAttribute(pkcs7.OIDAttributeAdobeRevocation, &revocationInfoArchival); err == nil {
			dataList := make([][]byte, 0, len(revocationInfoArchival.Ocsp))
			for _, rawValue := range revocationInfoArchival.Ocsp {
				dataList = append(dataList, rawValue.FullBytes)
			}
			evidence.ocsps = ocspEvidenceFromDER(dataList)
			var crls []*parsedCRL
			for _, crl := range revocationInfoArchival.Crl {
				if crl2 := t.parseCRL(crl.FullBytes); crl2 != nil {
					crls = append(crls, crl2)
				}
			}
			evidence.crls = crlEvidenceFromParsed(crls)
		}
	}
	dss := t.reader.DSS
	if dss == nil {
		return evidence
	}
	if vri := dss.VRI[s]; vri != nil {
		evidence.ocsps = append(evidence.ocsps, ocspEvidenceFromDER(decodeStreams(vri.OCSP))...)
		evidence.crls = append(evidence.crls, crlEvidenceFromParsed(t.streamCRLs(vri.CRL))...)
	}
	if !t.dssEvidenceLoaded {
		t.dssEvidenceLoaded = true
		t.dssEvidence = revocationEvidence{ocsps: ocspEvidenceFromDER(decodeStreams(dss.OCSPs)), crls: crlEvidenceFromParsed(t.streamCRLs(dss.CRLs))}
	}
	evidence.ocsps = append(evidence.ocsps, t.dssEvidence.ocsps...)
	evidence.crls = append(evidence.crls, t.dssEvidence.crls...)
	return evidence
}

// FieldFlag represents form field flags. Some of the flags can apply to all types of fields whereas other
// flags are specific.
type FieldFlag uint32

// NewPdfAnnotationSound returns a new sound annotation.
func NewPdfAnnotationSound() *PdfAnnotationSound {
	annot := NewPdfAnnotation()
	sound := &PdfAnnotationSound{}
	sound.PdfAnnotation = annot
	sound.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(sound)
	return sound
}

func (r *PdfReader) loadDSS() (*DSS, error) {
	if r.parser.GetCrypter() != nil && !r.parser.IsAuthenticated() {
		return nil, fmt.Errorf("file need to be decrypted first")
	}
	dss := r.catalog.Get("DSS")
	if dss == nil {
		return nil, nil
	}
	dssIndirect, _ := core.GetIndirect(dss)
	dss = core.TraceToDirectObject(dss)
	switch v := dss.(type) {
	case *core.PdfObjectNull:
		return nil, nil
	case *core.PdfObjectDictionary:
		return newDSSFromDict(dssIndirect, v)
	}
	return nil, fmt.Errorf("invalid DSS entry %T", dss)
}

// SetXObjectImageByNameLazy adds the provided XObjectImage to the page resources.
// The added XObjectImage is identified by the specified name.
func (r *PdfPageResources) SetXObjectImageByNameLazy(keyName core.PdfObjectName, ximg *XObjectImage, lazy bool) error {
	stream := ximg.ToPdfObject().(*core.PdfObjectStream)
	if lazy {
		stream.MakeLazy()
	}
	err := r.SetXObjectByName(keyName, stream)
	return err
}

// PdfActionThread represents a thread action.
type PdfActionThread struct {
	*PdfAction
	F *PdfFilespec
	D core.PdfObject
	B core.PdfObject
}

var ErrColorOutOfRange = errors.New("color out of range")

// Colorspace returns a Pattern colorspace whose underlying colorspace, if any,
// is derived from the underlying color.
func (p *PdfColorPatternType1) Colorspace() PdfColorspace {
	specialPatternCS := NewPdfColorspaceSpecialPattern()
	if p.Color != nil {
		specialPatternCS.UnderlyingCS = p.Color.Colorspace()
	}
	return specialPatternCS
}

// GetContainingPdfObject returns the container of the outline item (indirect object).
func (o *PdfOutlineItem) GetContainingPdfObject() core.PdfObject { return o.primitive }

type ocspAnswer struct {
	*ocsp.Response
	online     bool
	authorised bool
}

// NewPdfReaderAt is the explicit-size form of NewPdfReader. Use this when
// the source's byte length cannot be auto-detected — e.g. a custom io.ReaderAt
// that doesn't implement Size/Seek/Stat.
func NewPdfReaderAt(r io.ReaderAt, size int64) (*PdfReader, error) {
	const trackNameNewPdfReader = "model:NewPdfReader"
	return newPdfReaderTracked(r, size, &ReaderOpts{}, false, false, trackNameNewPdfReader)
}

func imageFromImageBase(base *imageutil.ImageBase) (img Image) {
	img.Width = int64(base.Width)
	img.Height = int64(base.Height)
	img.BitsPerComponent = int64(base.BitsPerComponent)
	img.ColorComponents = base.ColorComponents
	img.Data = base.Data
	img.decode = base.Decode
	img.alpha = base.Alpha
	return img
}

// PdfActionURI represents an URI action.
type PdfActionURI struct {
	*PdfAction
	URI   core.PdfObject
	IsMap core.PdfObject
}
type crossReference struct {
	Type int

	// Type 1
	Offset     int64
	Generation int64

	// Type 2
	ObjectNumber int
	Index        int
}
type xrefStreamPlan struct {
	objNum  int64
	entries []xrefEntry
	size    int64
	prev    int64
	hasPrev bool
	root    *core.PdfIndirectObject
	info    *core.PdfIndirectObject
	encrypt *core.PdfIndirectObject
	id      *core.PdfObjectArray
}

// NewPdfActionNamed returns a new "named" action.
func NewPdfActionNamed() *PdfActionNamed {
	action := NewPdfAction()
	namedAction := &PdfActionNamed{}
	namedAction.PdfAction = action
	action.SetContext(namedAction)
	return namedAction
}

// GetNumComponents returns the number of input color components, i.e. that are input to the tint transform.
func (cs *PdfColorspaceDeviceN) GetNumComponents() int { return cs.ColorantNames.Len() }

// StandardImplementer is an interface that defines specified PDF standards like PDF/A-1A (pdfa.Profile1A)
// NOTE: This implementation is in experimental development state.
//
//	Keep in mind that it might change in the subsequent minor versions.
type StandardImplementer interface {
	StandardValidator
	StandardApplier

	// StandardName gets the human-readable name of the standard.
	StandardName() string
}

func bitsRequired(n uint32) uint16 {
	if n == 0 {
		return 1
	}
	return uint16(bits.Len32(n))
}

// SetFileName sets the pdf writer file name for metered usage tracker.
func (w *PdfWriter) SetFileName(name string) { w.fileName = name }

func (f *PdfFunctionType0) processSamples() error {
	resampleBytes := sampling.ResampleBytes(f.rawData, f.BitsPerSample)
	f.samples = resampleBytes
	return nil
}

// NewPdfColorspaceSpecialPattern returns a new pattern color.
func NewPdfColorspaceSpecialPattern() *PdfColorspaceSpecialPattern {
	return &PdfColorspaceSpecialPattern{}
}

// PdfActionSubmitForm represents a submitForm action.
type PdfActionSubmitForm struct {
	*PdfAction
	F      *PdfFilespec
	Fields core.PdfObject
	Flags  core.PdfObject
}

// EncryptForRecipients encrypts the document with the public-key security
// handler (/Filter /Adobe.PubSec) for the given recipient certificates, which
// must carry RSA public keys. Recipients open the document with their
// certificate and private key (PdfReader.DecryptWithCertificate or
// ReaderOpts); there is no password. All recipients get the permissions in
// options; granting every permission (security.PermOwner) makes them
// owner-equivalent. The cipher is AES_256bit (V5, PDF 2.0) unless options
// names AES_128bit (V4, PDF 1.5); RC4 is not available with this handler. A
// higher version already set on the writer is kept.
func (w *PdfWriter) EncryptForRecipients(recipients []*x509.Certificate, options *EncryptOptions) error {
	algo := AES_256bit
	perms := security.PermOwner
	if options != nil {
		if options.Algorithm != RC4_128bit {
			algo = options.Algorithm
		}
		perms = options.Permissions
	}
	cryptFilter, err := newCryptFilter(algo)
	if err != nil {
		return err
	}
	pdfCryptNewEncryptPubSec, encryptInfo, err := core.PdfCryptNewEncryptPubSec(cryptFilter, recipients, perms)
	if err != nil {
		return err
	}
	if w.version.Major > encryptInfo.Major || (w.version.Major == encryptInfo.Major && w.version.Minor > encryptInfo.Minor) {
		encryptInfo.Version = w.version
	}
	w.setEncryption(pdfCryptNewEncryptPubSec, encryptInfo)
	return nil
}

// PdfAnnotationPopup represents Popup annotations.
// (Section 12.5.6.14).
type PdfAnnotationPopup struct {
	*PdfAnnotation
	Parent core.PdfObject
	Open   core.PdfObject
}

// PdfActionResetForm represents a resetForm action.
type PdfActionResetForm struct {
	*PdfAction
	Fields core.PdfObject
	Flags  core.PdfObject
}

// FieldFilterFunc represents a PDF field filtering function. If the function
// returns true, the PDF field is kept, otherwise it is discarded.
type FieldFilterFunc func(*PdfField) bool

// ToPdfObject implements interface PdfModel.
func (a *PdfActionImportData) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeImportData)))
	if a.F != nil {
		dict.Set("F", a.F.ToPdfObject())
	}
	return indirect
}

type xrefEntry struct {
	objNum     int64
	entryType  byte
	offset     uint32
	genOrIndex uint16
}

// PartialName returns the partial name of the field.
func (f *PdfField) PartialName() string {
	s := ""
	if f.T != nil {
		s = f.T.Decoded()
	} else {
		common.Log.Debug("Field missing T field (incompatible)")
	}
	return s
}

// IsHideMenubar returns the value of the hideMenubar flag.
func (vp *ViewerPreferences) IsHideMenubar() bool {
	if vp.hideMenubar == nil {
		return false
	}
	return *vp.hideMenubar
}

// PdfColorDeviceCMYK is a CMYK32 color, where each component is defined in the range 0.0 - 1.0 where 1.0 is the primary intensity.
type PdfColorDeviceCMYK [4]float64

// PdfActionRendition represents a Rendition action.
type PdfActionRendition struct {
	*PdfAction
	R  core.PdfObject
	AN core.PdfObject
	OP core.PdfObject
	JS core.PdfObject
}

// GetAllContentStreams gets all the content streams for a page as one string.
func (p *PdfPage) GetAllContentStreams() (string, error) {
	contentStreams, err := p.GetContentStreams()
	if err != nil {
		return "", err
	}
	return strings.Join(contentStreams, " "), nil
}

var (
	ArtifactTypeNone       ArtifactType = ""
	ArtifactTypePagination ArtifactType = "Pagination"
	ArtifactTypeLayout     ArtifactType = "Layout"
	ArtifactTypePage       ArtifactType = "Page"
	ArtifactTypeBackground ArtifactType = "Background"
)

// ImageToRGB converts CalRGB colorspace image to RGB and returns the result.
func (cs *PdfColorspaceCalRGB) ImageToRGB(img Image) (Image, error) {
	reader := sampling.NewReader(img.getBase())
	imageBase := imageutil.NewImageBase(int(img.Width), int(img.Height), int(img.BitsPerComponent), 3, nil, nil, nil)
	writer := sampling.NewWriter(imageBase)
	f := math.Pow(2, float64(img.BitsPerComponent)) - 1
	nums := make([]uint32, 3)
	var (
		err                  error
		v, val, x, y, y2, y3 float64
	)
	for {
		err = reader.ReadSamples(nums)
		if err == io.EOF {
			break
		} else if err != nil {
			return img, err
		}
		v = float64(nums[0]) / f
		val = float64(nums[1]) / f
		x = float64(nums[2]) / f
		y = cs.Matrix[0]*math.Pow(v, cs.Gamma[0]) + cs.Matrix[3]*math.Pow(val, cs.Gamma[1]) + cs.Matrix[6]*math.Pow(x, cs.Gamma[2])
		y2 = cs.Matrix[1]*math.Pow(v, cs.Gamma[0]) + cs.Matrix[4]*math.Pow(val, cs.Gamma[1]) + cs.Matrix[7]*math.Pow(x, cs.Gamma[2])
		y3 = cs.Matrix[2]*math.Pow(v, cs.Gamma[0]) + cs.Matrix[5]*math.Pow(val, cs.Gamma[1]) + cs.Matrix[8]*math.Pow(x, cs.Gamma[2])
		v = 3.240479*y + -1.537150*y2 + -0.498535*y3
		val = -0.969256*y + 1.875992*y2 + 0.041556*y3
		x = 0.055648*y + -0.204043*y2 + 1.057311*y3
		v = math.Min(math.Max(v, 0), 1.0)
		val = math.Min(math.Max(val, 0), 1.0)
		x = math.Min(math.Max(x, 0), 1.0)
		nums[0] = uint32(v * f)
		nums[1] = uint32(val * f)
		nums[2] = uint32(x * f)
		if err = writer.WriteSamples(nums); err != nil {
			return img, err
		}
	}
	return imageFromImageBase(&imageBase), nil
}

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

func getPdfSubject() string { metadataMu.Lock(); defer metadataMu.Unlock(); return pdfSubject }
func (r *PdfReader) newPdfAnnotationSquigglyFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationSquiggly, error) {
	squiggly := PdfAnnotationSquiggly{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	squiggly.PdfAnnotationMarkup = markup
	squiggly.QuadPoints = dict.Get("QuadPoints")
	return &squiggly, nil
}

// IsShading specifies if the pattern is a shading pattern.
func (p *PdfPattern) IsShading() bool { return p.PatternType == 2 }

func newNamesFromPdfObject(names core.PdfObject) *Names {
	names2 := newNames()
	dict := names.Direct().(*core.PdfObjectDictionary)
	if dests := dict.Get("Dests"); dests != nil {
		names2.Dests = dests.Direct().(*core.PdfObjectDictionary)
	}
	if ap := dict.Get("AP"); ap != nil {
		names2.AP = ap.Direct().(*core.PdfObjectDictionary)
	}
	if javaScript := dict.Get("JavaScript"); javaScript != nil {
		names2.JavaScript = javaScript.Direct().(*core.PdfObjectDictionary)
	}
	if pages := dict.Get("Pages"); pages != nil {
		names2.Pages = pages.Direct().(*core.PdfObjectDictionary)
	}
	if templates := dict.Get("Templates"); templates != nil {
		names2.Templates = templates.Direct().(*core.PdfObjectDictionary)
	}
	if ids := dict.Get("IDS"); ids != nil {
		names2.IDS = ids.Direct().(*core.PdfObjectDictionary)
	}
	if urls := dict.Get("URLS"); urls != nil {
		names2.URLS = urls.Direct().(*core.PdfObjectDictionary)
	}
	if embeddedFiles := dict.Get("EmbeddedFiles"); embeddedFiles != nil {
		names2.EmbeddedFiles = embeddedFiles.Direct().(*core.PdfObjectDictionary)
	}
	if alternatePresentations := dict.Get("AlternatePresentations"); alternatePresentations != nil {
		names2.AlternatePresentations = alternatePresentations.Direct().(*core.PdfObjectDictionary)
	}
	if renditions := dict.Get("Renditions"); renditions != nil {
		names2.Renditions = renditions.Direct().(*core.PdfObjectDictionary)
	}
	return names2
}

// GetXObjectByName returns the XObject with the specified keyName and the object type.
func (r *PdfPageResources) GetXObjectByName(keyName core.PdfObjectName) (*core.PdfObjectStream, XObjectType) {
	if r.XObject == nil {
		return nil, XObjectTypeUndefined
	}
	dict, ok := core.TraceToDirectObject(r.XObject).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: XObject not a dictionary! (got %T)", core.TraceToDirectObject(r.XObject))
		return nil, XObjectTypeUndefined
	}
	if obj := dict.Get(keyName); obj != nil {
		stream, ok := core.GetStream(obj)
		if !ok {
			common.Log.Debug("XObject not pointing to a stream %T", obj)
			return nil, XObjectTypeUndefined
		}
		dict := stream.PdfObjectDictionary
		name, ok := core.TraceToDirectObject(dict.Get("Subtype")).(*core.PdfObjectName)
		if !ok {
			common.Log.Debug("XObject Subtype not a Name, dict: %s", dict.String())
			return nil, XObjectTypeUndefined
		}
		if *name == "Image" {
			return stream, XObjectTypeImage
		} else if *name == "Form" {
			return stream, XObjectTypeForm
		} else if *name == "PS" {
			return stream, XObjectTypePS
		} else {
			common.Log.Debug("XObject Subtype not known (%s)", *name)
			return nil, XObjectTypeUndefined
		}
	} else {
		return nil, XObjectTypeUndefined
	}
}

// IsLinearized reports whether the underlying PDF is linearized
// ("fast web view") as defined in ISO 32000-2 Annex F.
func (r *PdfReader) IsLinearized() bool { return r.parser.IsLinearized() }

// GetContainingPdfObject implements interface PdfModel.
func (s *PdfSignature) GetContainingPdfObject() core.PdfObject { return s.primitive }

// PdfActionTrans represents a trans action.
type PdfActionTrans struct {
	*PdfAction
	Trans core.PdfObject
}
type issuingDistributionPoint struct {
	DistributionPoint          distributionPointName `asn1:"optional,tag:0"`
	OnlyContainsUserCerts      bool                  `asn1:"optional,tag:1"`
	OnlyContainsCACerts        bool                  `asn1:"optional,tag:2"`
	OnlySomeReasons            asn1.BitString        `asn1:"optional,tag:3"`
	IndirectCRL                bool                  `asn1:"optional,tag:4"`
	OnlyContainsAttributeCerts bool                  `asn1:"optional,tag:5"`
}

// ToPdfObject implements interface PdfModel.
func (b *PdfBorderStyle) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	if b.container != nil {
		if indirect, ok := b.container.(*core.PdfIndirectObject); ok {
			indirect.PdfObject = dict
		}
	}
	dict.Set("Subtype", core.MakeName("Border"))
	if b.W != nil {
		dict.Set("W", core.MakeFloat(*b.W))
	}
	if b.S != nil {
		dict.Set("S", core.MakeName(b.S.GetPdfName()))
	}
	if b.D != nil {
		dict.Set("D", core.MakeArrayFromIntegers(*b.D))
	}
	if b.container != nil {
		return b.container
	}
	return dict
}

// CharMetrics represents width and height metrics of a glyph.
type CharMetrics = fonts.CharMetrics

func (t *trustEvaluator) sharedCertificates(certs []*x509.Certificate) []*x509.Certificate {
	certs2 := append([]*x509.Certificate(nil), certs...)
	certs2 = append(certs2, t.dssCertificates()...)
	return append(certs2, t.opts.Intermediates...)
}

// GetNumComponents returns the number of color components of the colorspace device.
// Returns 3 for an RGB device.
func (cs *PdfColorspaceDeviceRGB) GetNumComponents() int { return 3 }

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

func newPdfColorspaceCalRGBFromPdfObject(obj core.PdfObject) (*PdfColorspaceCalRGB, error) {
	calRGBCS := NewPdfColorspaceCalRGB()
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		calRGBCS.container = indirect
	}
	obj = core.TraceToDirectObject(obj)
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("type error")
	}
	if arr.Len() != 2 {
		return nil, fmt.Errorf("invalid CalRGB colorspace")
	}
	obj = core.TraceToDirectObject(arr.Get(0))
	name, ok := obj.(*core.PdfObjectName)
	if !ok {
		return nil, fmt.Errorf("CalRGB name not a Name object")
	}
	if *name != "CalRGB" {
		return nil, fmt.Errorf("not a CalRGB colorspace")
	}
	obj = core.TraceToDirectObject(arr.Get(1))
	dict, ok := obj.(*core.PdfObjectDictionary)
	if !ok {
		return nil, fmt.Errorf("CalRGB name not a Name object")
	}
	obj = dict.Get("WhitePoint")
	obj = core.TraceToDirectObject(obj)
	arr2, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("CalRGB: Invalid WhitePoint")
	}
	if arr2.Len() != 3 {
		return nil, fmt.Errorf("CalRGB: Invalid WhitePoint array")
	}
	floats, err := arr2.GetAsFloat64Slice()
	if err != nil {
		return nil, err
	}
	calRGBCS.WhitePoint = floats
	obj = dict.Get("BlackPoint")
	if obj != nil {
		obj = core.TraceToDirectObject(obj)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			return nil, fmt.Errorf("CalRGB: Invalid BlackPoint")
		}
		if arr.Len() != 3 {
			return nil, fmt.Errorf("CalRGB: Invalid BlackPoint array")
		}
		floats, err := arr.GetAsFloat64Slice()
		if err != nil {
			return nil, err
		}
		calRGBCS.BlackPoint = floats
	}
	obj = dict.Get("Gamma")
	if obj != nil {
		obj = core.TraceToDirectObject(obj)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			return nil, fmt.Errorf("CalRGB: Invalid Gamma")
		}
		if arr.Len() != 3 {
			return nil, fmt.Errorf("CalRGB: Invalid Gamma array")
		}
		floats, err := arr.GetAsFloat64Slice()
		if err != nil {
			return nil, err
		}
		calRGBCS.Gamma = floats
	}
	obj = dict.Get("Matrix")
	if obj != nil {
		obj = core.TraceToDirectObject(obj)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			return nil, fmt.Errorf("CalRGB: Invalid Matrix")
		}
		if arr.Len() != 9 {
			common.Log.Error("Matrix array: %s", arr.String())
			return nil, fmt.Errorf("CalRGB: Invalid Matrix array")
		}
		floats, err := arr.GetAsFloat64Slice()
		if err != nil {
			return nil, err
		}
		calRGBCS.Matrix = floats
	}
	return calRGBCS, nil
}

// ToInteger convert to an integer format.
func (c *PdfColorCalRGB) ToInteger(bits int) [3]uint32 {
	f := math.Pow(2, float64(bits)) - 1
	return [3]uint32{uint32(f * c.A()), uint32(f * c.B()), uint32(f * c.C())}
}

// Clear clears flag fl from the flag and returns the resulting flag.
func (f FieldFlag) Clear(fl FieldFlag) FieldFlag { return FieldFlag(f.Mask() &^ fl.Mask()) }

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

var newlineRegexp = regexp.MustCompile("[\\n\\r]+")

// PdfAnnotationCaret represents Caret annotations.
// (Section 12.5.6.11).
type PdfAnnotationCaret struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	RD core.PdfObject
	Sy core.PdfObject
}

// NewPdfShadingType2 creates an empty shading type 2 dictionary.
func NewPdfShadingType2() *PdfShadingType2 {
	shading2 := &PdfShadingType2{}
	shading2.PdfShading = &PdfShading{}
	shading2.PdfShading.primitive = core.MakeIndirectObject(core.MakeDict())
	shading2.PdfShading.context = shading2
	return shading2
}

const maxGapBytes = 2

func newPdfShadingPatternFromDict(dict *core.PdfObjectDictionary) (*PdfShadingPattern, error) {
	shadingPattern := &PdfShadingPattern{}
	shading := dict.Get("Shading")
	if shading == nil {
		common.Log.Debug("Shading missing")
		return nil, ErrRequiredAttributeMissing
	}
	shading2, err := newPdfShadingFromPdfObject(shading)
	if err != nil {
		common.Log.Debug("Error loading shading: %v", err)
		return nil, err
	}
	shadingPattern.Shading = shading2
	if matrix := dict.Get("Matrix"); matrix != nil {
		arr, ok := core.TraceToDirectObject(matrix).(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Matrix not an array (got %T)", matrix)
			return nil, core.ErrTypeError
		}
		shadingPattern.Matrix = arr
	}
	if extGState := dict.Get("ExtGState"); extGState != nil {
		shadingPattern.ExtGState = extGState
	}
	return shadingPattern, nil
}

func (w *PdfWriter) canSkipCopy() bool {
	if w.appendMode {
		return false
	}
	if !w.skipObjectCopy {
		if w.crypter != nil || w.optimizer != nil || w.standardApplier != nil {
			return false
		}
	}
	return !w.hasOrphanPages()
}

// VariableText contains the common attributes of a variable text.
// The VariableText is typically not used directly, but is can encapsulate by PdfField
// See section 12.7.3.3 "Variable Text" and Table 222 (pp. 434-436 PDF32000_2008).
type VariableText struct {
	DA *core.PdfObjectString
	Q  *core.PdfObjectInteger
	DS *core.PdfObjectString
	RV core.PdfObject
}

func (t *trustEvaluator) streamCRLs(crLs []*core.PdfObjectStream) []*parsedCRL {
	var crls []*parsedCRL
	for _, crL := range crLs {
		crlByStream, ok := t.crlByStream[crL]
		if !ok {
			if dssStream, ok := decodeDSSStream(crL); ok {
				crlByStream = t.parseCRL(dssStream)
			}
			if t.crlByStream == nil {
				t.crlByStream = map[*core.PdfObjectStream]*parsedCRL{}
			}
			t.crlByStream[crL] = crlByStream
		}
		if crlByStream != nil {
			crls = append(crls, crlByStream)
		}
	}
	return crls
}

// ToPdfObject converts the K dictionary to a PDF object.
func (kv *KValue) ToPdfObject() core.PdfObject {
	if kv.kDict != nil {
		return core.MakeIndirectObject(kv.kDict.ToPdfObject())
	}
	if kv.refObject != nil {
		return kv.refObject
	}
	if kv.mcid != nil {
		return core.MakeInteger(int64(*kv.mcid))
	}
	return nil
}

// ToPdfObject returns the PDF representation of the page resources.
func (r *PdfPageResources) ToPdfObject() core.PdfObject {
	dict := r.primitive
	dict.SetIfNotNil("ExtGState", r.ExtGState)
	if r.colorspaces != nil {
		r.ColorSpace = r.colorspaces.ToPdfObject()
	}
	dict.SetIfNotNil("ColorSpace", r.ColorSpace)
	dict.SetIfNotNil("Pattern", r.Pattern)
	dict.SetIfNotNil("Shading", r.Shading)
	dict.SetIfNotNil("XObject", r.XObject)
	dict.SetIfNotNil("Font", r.Font)
	dict.SetIfNotNil("ProcSet", r.ProcSet)
	dict.SetIfNotNil("Properties", r.Properties)
	return dict
}

// GetRuneMetrics returns the character metrics for the specified rune.
// A bool flag is returned to indicate whether or not the entry was found.
func (f pdfFontType0) GetRuneMetrics(r rune) (fonts.CharMetrics, bool) {
	if f.DescendantFont == nil {
		common.Log.Debug("ERROR: No descendant. font=%s", f)
		return fonts.CharMetrics{}, false
	}
	return f.DescendantFont.GetRuneMetrics(r)
}

type chainRevocation struct {
	status      RevocationStatus
	revokedCert *x509.Certificate
	revokedAt   time.Time
	leaf        certRevocation
	warnings    []string
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationPolyLine) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("PolyLine"))
	dict.SetIfNotNil("Vertices", a.Vertices)
	dict.SetIfNotNil("LE", a.LE)
	dict.SetIfNotNil("BS", a.BS)
	dict.SetIfNotNil("IC", a.IC)
	dict.SetIfNotNil("BE", a.BE)
	dict.SetIfNotNil("IT", a.IT)
	dict.SetIfNotNil("Measure", a.Measure)
	return indirect
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationSquare) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	if a.PdfAnnotationMarkup != nil {
		a.appendToPdfDictionary(dict)
	}
	dict.SetIfNotNil("Subtype", core.MakeName("Square"))
	dict.SetIfNotNil("BS", a.BS)
	dict.SetIfNotNil("IC", a.IC)
	dict.SetIfNotNil("BE", a.BE)
	dict.SetIfNotNil("RD", a.RD)
	return indirect
}

// ArtifactType represents the type of an artifact in a PDF document.
type ArtifactType string

// NewPdfShadingType3 creates an empty shading type 3 dictionary.
func NewPdfShadingType3() *PdfShadingType3 {
	shading3 := &PdfShadingType3{}
	shading3.PdfShading = &PdfShading{}
	shading3.PdfShading.primitive = core.MakeIndirectObject(core.MakeDict())
	shading3.PdfShading.context = shading3
	return shading3
}

// ToPdfObject returns the PDF representation of the colorspace.
func (r *PdfPageResourcesColorspaces) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	for _, name := range r.Names {
		dict.Set(core.PdfObjectName(name), r.Colorspaces[name].ToPdfObject())
	}
	if r.container != nil {
		r.container.PdfObject = dict
		return r.container
	}
	return dict
}

// NewPdfAnnotationPrinterMark returns a new printermark annotation.
func NewPdfAnnotationPrinterMark() *PdfAnnotationPrinterMark {
	annot := NewPdfAnnotation()
	printerMark := &PdfAnnotationPrinterMark{}
	printerMark.PdfAnnotation = annot
	annot.SetContext(printerMark)
	return printerMark
}

// Names represents a PDF name tree.
//
// Ref: PDF32000_2008 chapter 7.7.4.
type Names struct {
	container *core.PdfIndirectObject

	// Dests is a name tree mapping name string to destinations.
	Dests *core.PdfObjectDictionary

	// AP is a name tree mapping name strings to annotation appearance streams.
	AP *core.PdfObjectDictionary

	// JavaScript is a name tree mapping name strings to JavaScript actions.
	JavaScript *core.PdfObjectDictionary

	// Pages is a name tree mapping name strings to visible pages for use in interactive forms.
	Pages *core.PdfObjectDictionary

	// Templates is a name tree mapping name strings to invisible (template) pages for use in interactive forms.
	Templates *core.PdfObjectDictionary

	// IDS is a name tree mapping digital identifies to Web Capture content sets.
	IDS *core.PdfObjectDictionary

	// URLS is a name tree mapping URLs to Web Capture content sets.
	URLS *core.PdfObjectDictionary

	// EmbeddedFiles is a name tree mapping name strings to file specifications for embedded file streams.
	EmbeddedFiles *core.PdfObjectDictionary

	// AlternatePresentations is a name tree mapping name strings to alternate presentations.
	AlternatePresentations *core.PdfObjectDictionary

	// Renditions is a name tree mapping name strings (which shall have Unicode encoding) to rendition objects.
	Renditions *core.PdfObjectDictionary
}

// ColorFromFloats returns a new PdfColor based on input color components.
func (cs *PdfColorspaceDeviceN) ColorFromFloats(vals []float64) (PdfColor, error) {
	if len(vals) != cs.GetNumComponents() {
		return nil, errors.New("range check")
	}
	evaluate, err := cs.TintTransform.Evaluate(vals)
	if err != nil {
		return nil, err
	}
	col, err := cs.AlternateSpace.ColorFromFloats(evaluate)
	if err != nil {
		return nil, err
	}
	return col, nil
}

// Components returns the color's component values: the single gray value.
func (c *PdfColorDeviceGray) Components() []float64 { return []float64{float64(*c)} }

// ToPdfObject converts the PdfFont object to its PDF representation.
func (f *PdfFont) ToPdfObject() core.PdfObject {
	if f.context == nil {
		common.Log.Debug("ERROR: font context is nil")
		return core.MakeNull()
	}
	return f.context.ToPdfObject()
}

// PdfColorspaceDeviceCMYK represents a CMYK32 colorspace.
type PdfColorspaceDeviceCMYK struct{}

// FieldAppearanceGenerator generates appearance stream for a given field.
type FieldAppearanceGenerator interface {
	ContentStreamWrapper
	GenerateAppearanceDict(acroForm *PdfAcroForm, field *PdfField, widget *PdfAnnotationWidget) (*core.PdfObjectDictionary, error)
}

// StandardApplier is the interface that performs optimization of the whole PDF document.
// As a result an input document is being changed by the optimizer.
// The writer than takes back all it's parts and overwrites it.
// NOTE: This implementation is in experimental development state.
//
//	Keep in mind that it might change in the subsequent minor versions.
type StandardApplier interface {
	ApplyStandard(document *docutil.Document) error
}

func newAppenderSnapshotReader(r io.ReaderAt, n int64, opts *ReaderOpts, ok bool) (*PdfReader, error) {
	readerOpts := NewReaderOpts()
	if opts != nil {
		*readerOpts = *opts
	}
	readerOpts.LazyLoad = true
	const trackNameAppenderSnapshot = "model:appenderSnapshot"
	return newPdfReaderTracked(r, n, readerOpts, ok, true, trackNameAppenderSnapshot)
}

// FieldValue is the fully qualified name of a terminal (leaf) form field
// paired with its raw value object.
type FieldValue struct {
	// Name is the fully qualified field name, e.g. "form.address.city".
	Name string

	// Value is the field's /V entry resolved to a direct object (a string,
	// name or array for regular fields), or nil when the field has no value.
	Value core.PdfObject

	// Field is the terminal field the value belongs to.
	Field *PdfField
}

func crlEvidenceFromParsed(crls []*parsedCRL) []crlEvidence {
	crls2 := make([]crlEvidence, 0, len(crls))
	for _, crl := range crls {
		crls2 = append(crls2, crlEvidence{parsedCRL: crl})
	}
	return crls2
}

// RevocationStatus is the outcome of a revocation check for a signature's
// certificate chain.
type RevocationStatus int

// Reset sets the multi font encoder to its initial state.
func (e *MultipleFontEncoder) Reset() { e.CurrentFont = e.fontList[0] }

// SetPdfSubject sets the Subject attribute of the output PDF.
func SetPdfSubject(subject string) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	pdfSubject = subject
}

// ToPdfObject recursively builds the Outline tree PDF object.
func (o *PdfOutlineItem) ToPdfObject() core.PdfObject {
	indirect := o.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.Set("Title", o.Title)
	if o.A != nil {
		dict.Set("A", o.A)
	}
	if se := dict.Get("SE"); se != nil {
		dict.Remove("SE")
	}
	if o.C != nil {
		dict.Set("C", o.C)
	}
	if o.Dest != nil {
		dict.Set("Dest", o.Dest)
	}
	if o.F != nil {
		dict.Set("F", o.F)
	}
	if o.Count != nil {
		dict.Set("Count", core.MakeInteger(*o.Count))
	}
	if o.Next != nil {
		dict.Set("Next", o.Next.ToPdfObject())
	}
	if o.First != nil {
		dict.Set("First", o.First.ToPdfObject())
	}
	if o.Prev != nil {
		dict.Set("Prev", o.Prev.GetContext().GetContainingPdfObject())
	}
	if o.Last != nil {
		dict.Set("Last", o.Last.GetContext().GetContainingPdfObject())
	}
	if o.Parent != nil {
		dict.Set("Parent", o.Parent.GetContext().GetContainingPdfObject())
	}
	return indirect
}

func newPdfFilespecFromPdfObject(obj core.PdfObject) (*PdfFilespec, error) {
	if obj == nil {
		return nil, nil
	}
	return NewPdfFilespecFromObj(obj)
}

// GetFontByName gets the font specified by keyName. Returns the PdfObject which
// the entry refers to. Returns a bool value indicating whether or not the entry was found.
func (r *PdfPageResources) GetFontByName(keyName core.PdfObjectName) (core.PdfObject, bool) {
	if r.Font == nil {
		return nil, false
	}
	dict, ok := core.TraceToDirectObject(r.Font).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Font not a dictionary! (got %T)", core.TraceToDirectObject(r.Font))
		return nil, false
	}
	if obj := dict.Get(keyName); obj != nil {
		return obj, true
	}
	return nil, false
}

// RemoveKChild removes a specific KDict child from the kChildren slice.
func (k *KDict) RemoveKChild(kChild *KDict) bool {
	for i, children := range k.children {
		if children.kDict == kChild {
			k.children = append(k.children[:i], k.children[i+1:]...)
			return true
		}
	}
	return false
}

func (f *PdfField) inheritedValue() core.PdfObject {
	var obj core.PdfObject
	f.inherit(func(field *PdfField) bool { obj = field.V; return obj != nil })
	return obj
}

// IsValid checks if the given pdf output intent type is valid.
func (t PdfOutputIntentType) IsValid() bool {
	return t >= PdfOutputIntentTypeA1 && t <= PdfOutputIntentTypeX
}

func applySubsetTag(font *pdfFontType0, s *string, fontDescriptor *PdfFontDescriptor, data []byte) {
	s2 := subsetTag(data)
	if len(font.baseFont) > 0 {
		font.baseFont = prependSubsetTag(font.baseFont, s2)
	}
	if len(*s) > 0 {
		*s = prependSubsetTag(*s, s2)
	}
	if len(font.name) > 0 {
		font.name = prependSubsetTag(font.name, s2)
	}
	if fontDescriptor != nil {
		fontNameName, ok := core.GetName(fontDescriptor.FontName)
		if ok && len(fontNameName.String()) > 0 {
			fontDescriptor.FontName = core.MakeName(prependSubsetTag(fontNameName.String(), s2))
		}
	}
}

// GetContentStreams returns the content stream as an array of strings.
func (p *PdfPage) GetContentStreams() ([]string, error) {
	contentStreamObjs := p.GetContentStreamObjs()
	var strs []string
	for _, contentStreamObj := range contentStreamObjs {
		s, err := pdfObjectToString(contentStreamObj)
		if err != nil {
			return nil, err
		}
		strs = append(strs, s)
	}
	return strs, nil
}

func (d *DSS) add(streams *[]*core.PdfObjectStream, streamByKey map[string]*core.PdfObjectStream, dataList [][]byte) ([]*core.PdfObjectStream, error) {
	streams2 := make([]*core.PdfObjectStream, 0, len(dataList))
	for _, data := range dataList {
		data2, err := sha1Digest(data)
		if err != nil {
			return nil, err
		}
		stream, ok := streamByKey[string(data2)]
		if !ok {
			stream, err = core.MakeStream(data, core.NewRawEncoder())
			if err != nil {
				return nil, err
			}
			streamByKey[string(data2)] = stream
			*streams = append(*streams, stream)
		}
		streams2 = append(streams2, stream)
	}
	return streams2, nil
}

// Colorspace returns a Pattern colorspace whose underlying colorspace, if any,
// is derived from the underlying color.
func (p *PdfColorPatternType2) Colorspace() PdfColorspace {
	specialPatternCS := NewPdfColorspaceSpecialPattern()
	if p.Color != nil {
		specialPatternCS.UnderlyingCS = p.Color.Colorspace()
	}
	return specialPatternCS
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

// SetCatalogMarkInfo sets the catalog MarkInfo dictionary.
func (w *PdfWriter) SetCatalogMarkInfo(info core.PdfObject) error {
	if info == nil {
		w.catalog.Remove("MarkInfo")
		return nil
	}
	if ref, ok := info.(*core.PdfObjectReference); ok {
		info = ref.Resolve()
		if info == nil {
			w.catalog.Remove("MarkInfo")
			return nil
		}
	}
	if _, ok := info.(*core.PdfIndirectObject); ok {
		w.addObject(info)
	}
	w.catalog.Set("MarkInfo", info)
	return nil
}

func (w *PdfWriter) checkLicense() error {
	licenseKey := license.GetLicenseKey()
	if (licenseKey == nil || !licenseKey.IsLicensed()) && !licenseCheckDisabled {
		fmt.Printf("Unlicensed copy of UniPDF\n")
		fmt.Println("")
		return errors.New("unipdf license code required")
	}
	return nil
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationSound) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Sound"))
	dict.SetIfNotNil("Sound", a.Sound)
	dict.SetIfNotNil("Name", a.Name)
	return indirect
}

// ToPdfObject implements interface PdfModel.
func (s *PdfSignature) ToPdfObject() core.PdfObject {
	indirect := s.primitive
	var dict *core.PdfObjectDictionary
	if signDict, ok := indirect.PdfObject.(*pdfSignDictionary); ok {
		dict = signDict.PdfObjectDictionary
	} else {
		dict = indirect.PdfObject.(*core.PdfObjectDictionary)
	}
	dict.SetIfNotNil("Type", s.Type)
	dict.SetIfNotNil("Filter", s.Filter)
	dict.SetIfNotNil("SubFilter", s.SubFilter)
	dict.SetIfNotNil("ByteRange", s.ByteRange)
	dict.SetIfNotNil("Contents", s.Contents)
	dict.SetIfNotNil("Cert", s.Cert)
	dict.SetIfNotNil("Name", s.Name)
	dict.SetIfNotNil("Reason", s.Reason)
	dict.SetIfNotNil("M", s.M)
	dict.SetIfNotNil("Reference", s.Reference)
	dict.SetIfNotNil("Changes", s.Changes)
	dict.SetIfNotNil("ContactInfo", s.ContactInfo)
	dict.SetIfNotNil("Location", s.Location)
	return indirect
}

// NewPdfColorspaceCalGray returns a new CalGray colorspace object.
func NewPdfColorspaceCalGray() *PdfColorspaceCalGray {
	calGrayCS := &PdfColorspaceCalGray{}
	calGrayCS.BlackPoint = []float64{0.0, 0.0, 0.0}
	calGrayCS.Gamma = 1
	return calGrayCS
}

// RemveTabOrder removes the tab order for the page.
func (p *PdfPage) RemoveTabOrder() { p.Tabs = nil }

// NewPdfReader returns a new PdfReader for the given io.ReaderAt source.
// The source's byte length is auto-detected via Size() / Seek() / Stat() —
// *os.File, *bytes.Reader, and *io.SectionReader all work without a manual
// size argument. For exotic io.ReaderAt sources that can't be auto-sized,
// use NewPdfReaderAt.
//
// Immediately loads and traverses the PDF structure including pages and page
// contents (if not encrypted). Loads entire document structure into memory.
// Alternatively, a lazy-loading reader can be created with NewPdfReaderLazy
// which loads references from disk on demand.
func NewPdfReader(r io.ReaderAt) (*PdfReader, error) {
	sizeOfReaderAt, err := core.SizeOfReaderAt(r)
	if err != nil {
		return nil, err
	}
	return NewPdfReaderAt(r, sizeOfReaderAt)
}

// PdfAnnotationStamp represents Stamp annotations.
// (Section 12.5.6.12).
type PdfAnnotationStamp struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	Name core.PdfObject
}

// GetContainingPdfObject returns the container of the image object (indirect object).
func (x *XObjectImage) GetContainingPdfObject() core.PdfObject { return x.primitive }

// Encoder returns the font's text encoder.
func (f pdfCIDFontType0) Encoder() textencoding.TextEncoder { return f.encoder }

func (r *PdfReader) newPdfActionSubmitFormFromDict(dict *core.PdfObjectDictionary) (*PdfActionSubmitForm, error) {
	filespec, err := newPdfFilespecFromPdfObject(dict.Get("F"))
	if err != nil {
		return nil, err
	}
	return &PdfActionSubmitForm{F: filespec, Fields: dict.Get("Fields"), Flags: dict.Get("Flags")}, nil
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationText) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	if a.PdfAnnotationMarkup != nil {
		a.appendToPdfDictionary(dict)
	}
	dict.SetIfNotNil("Subtype", core.MakeName("Text"))
	dict.SetIfNotNil("Open", a.Open)
	dict.SetIfNotNil("Name", a.Name)
	dict.SetIfNotNil("State", a.State)
	dict.SetIfNotNil("StateModel", a.StateModel)
	return indirect
}

// SetViewClip sets the value of the viewClip.
func (vp *ViewerPreferences) SetViewClip(viewClip PageBoundary) { vp.viewClip = viewClip }

func (t *trustEvaluator) parseCRL(data []byte) *parsedCRL {
	sum256 := sha256.Sum256(data)
	if crlByHash, ok := t.crlByHash[sum256]; ok {
		return crlByHash
	}
	crl := parseCRL(data)
	if t.crlByHash == nil {
		t.crlByHash = map[[32]byte]*parsedCRL{}
	}
	t.crlByHash[sum256] = crl
	return crl
}

// NewPdfColorDeviceGray returns a new grayscale color based on an input grayscale float value in range [0-1].
func NewPdfColorDeviceGray(grayVal float64) *PdfColorDeviceGray {
	deviceGrayColor := PdfColorDeviceGray(grayVal)
	return &deviceGrayColor
}

// GetVersion gets the document version.
func (w *PdfWriter) GetVersion() core.Version { return w.version }

// ColorFromPdfObjects returns a new PdfColor based on input color components. The input PdfObjects should
// be numeric.
func (cs *PdfColorspaceDeviceN) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) != cs.GetNumComponents() {
		return nil, errors.New("range check")
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objects)
	if err != nil {
		return nil, err
	}
	return cs.ColorFromFloats(numbersAsFloat)
}

// ShapeText shapes `text` with HarfBuzz and registers the resulting glyphs with
// the font's encoder so that subsetting keeps them (ligatures and contextual
// forms included). It returns nil for fonts without an OpenType encoder.
func (f *PdfFont) ShapeText(text string, fontSize float64) shaping.Line {
	openTypeFontEncoder, ok := f.Encoder().(*textencoding.OpenTypeFontEncoder)
	if !ok {
		return nil
	}
	return openTypeFontEncoder.ShapeText(text, fontSize)
}

// SetLinearizationOptions sets the options that apply when linearized output
// is enabled via SetLinearized.
func (w *PdfWriter) SetLinearizationOptions(opts LinearizationOptions) { w.linearizationOptions = opts }

func (r *PdfReader) newPdfPageFromDict(dict *core.PdfObjectDictionary) (*PdfPage, error) {
	page := NewPdfPage()
	page.pageDict = dict
	page.originalDict = dict.ShallowCopy()
	name, ok := dict.Get("Type").(*core.PdfObjectName)
	if !ok {
		return nil, errors.New("missing/invalid Page dictionary Type")
	}
	if *name != "Page" {
		return nil, errors.New("page dictionary Type != Page")
	}
	if parent := dict.Get("Parent"); parent != nil {
		page.Parent = parent
	}
	if lastModified := dict.Get("LastModified"); lastModified != nil {
		lastModifiedStr, ok := core.GetString(lastModified)
		if !ok {
			return nil, errors.New("page dictionary LastModified != string")
		}
		date, err := NewPdfDate(lastModifiedStr.Str())
		if err != nil {
			return nil, err
		}
		page.LastModified = &date
	}
	if resources := dict.Get("Resources"); resources != nil && !core.IsNullObject(resources) {
		resourcesDict, ok := core.GetDict(resources)
		if !ok {
			return nil, fmt.Errorf("invalid resource dictionary (%T)", resources)
		}
		var err error
		page.Resources, err = NewPdfPageResourcesFromDict(resourcesDict)
		if err != nil {
			return nil, err
		}
	} else {
		parentResources, err := page.getParentResources()
		if err != nil {
			return nil, err
		}
		if parentResources == nil {
			parentResources = NewPdfPageResources()
		}
		page.Resources = parentResources
	}
	if mediaBox := dict.Get("MediaBox"); mediaBox != nil {
		mediaBoxArr, ok := core.GetArray(mediaBox)
		if !ok {
			return nil, errors.New("page MediaBox not an array")
		}
		var err error
		page.MediaBox, err = NewPdfRectangle(*mediaBoxArr)
		if err != nil {
			return nil, err
		}
	}
	if cropBox := dict.Get("CropBox"); cropBox != nil {
		cropBoxArr, ok := core.GetArray(cropBox)
		if !ok {
			return nil, errors.New("page CropBox not an array")
		}
		var err error
		page.CropBox, err = NewPdfRectangle(*cropBoxArr)
		if err != nil {
			return nil, err
		}
	}
	if bleedBox := dict.Get("BleedBox"); bleedBox != nil {
		bleedBoxArr, ok := core.GetArray(bleedBox)
		if !ok {
			return nil, errors.New("page BleedBox not an array")
		}
		var err error
		page.BleedBox, err = NewPdfRectangle(*bleedBoxArr)
		if err != nil {
			return nil, err
		}
	}
	if trimBox := dict.Get("TrimBox"); trimBox != nil {
		trimBoxArr, ok := core.GetArray(trimBox)
		if !ok {
			return nil, errors.New("page TrimBox not an array")
		}
		var err error
		page.TrimBox, err = NewPdfRectangle(*trimBoxArr)
		if err != nil {
			return nil, err
		}
	}
	if artBox := dict.Get("ArtBox"); artBox != nil {
		artBoxArr, ok := core.GetArray(artBox)
		if !ok {
			return nil, errors.New("page ArtBox not an array")
		}
		var err error
		page.ArtBox, err = NewPdfRectangle(*artBoxArr)
		if err != nil {
			return nil, err
		}
	}
	if boxColorInfo := dict.Get("BoxColorInfo"); boxColorInfo != nil {
		page.BoxColorInfo = boxColorInfo
	}
	if contents := dict.Get("Contents"); contents != nil {
		page.Contents = contents
	}
	if rotate := dict.Get("Rotate"); rotate != nil {
		n, err := core.GetNumberAsInt64(rotate)
		if err != nil {
			return nil, errors.New("invalid Page Rotate object")
		}
		page.Rotate = &n
	}
	if group := dict.Get("Group"); group != nil {
		page.Group = group
	}
	if thumb := dict.Get("Thumb"); thumb != nil {
		page.Thumb = thumb
	}
	if b := dict.Get("B"); b != nil {
		page.B = b
	}
	if dur := dict.Get("Dur"); dur != nil {
		page.Dur = dur
	}
	if trans := dict.Get("Trans"); trans != nil {
		page.Trans = trans
	}
	if aa := dict.Get("AA"); aa != nil {
		page.AA = aa
	}
	if metadata := dict.Get("Metadata"); metadata != nil {
		page.Metadata = metadata
	}
	if pieceInfo := dict.Get("PieceInfo"); pieceInfo != nil {
		page.PieceInfo = pieceInfo
	}
	if structParents := dict.Get("StructParents"); structParents != nil {
		page.StructParents = structParents
	}
	if id := dict.Get("ID"); id != nil {
		page.ID = id
	}
	if pz := dict.Get("PZ"); pz != nil {
		page.PZ = pz
	}
	if separationInfo := dict.Get("SeparationInfo"); separationInfo != nil {
		page.SeparationInfo = separationInfo
	}
	if tabs := dict.Get("Tabs"); tabs != nil {
		page.Tabs = tabs
	}
	if templateInstantiated := dict.Get("TemplateInstantiated"); templateInstantiated != nil {
		page.TemplateInstantiated = templateInstantiated
	}
	if presSteps := dict.Get("PresSteps"); presSteps != nil {
		page.PresSteps = presSteps
	}
	if userUnit := dict.Get("UserUnit"); userUnit != nil {
		page.UserUnit = userUnit
	}
	if vp := dict.Get("VP"); vp != nil {
		page.VP = vp
	}
	if annots := dict.Get("Annots"); annots != nil {
		page.Annots = annots
	}
	page.reader = r
	return page, nil
}

// NewPdfSignature creates a new PdfSignature object.
func NewPdfSignature(handler SignatureHandler) *PdfSignature {
	sig := &PdfSignature{Type: core.MakeName("Sig"), Handler: handler}
	signDict := &pdfSignDictionary{PdfObjectDictionary: core.MakeDict(), handler: &handler, signature: sig}
	sig.primitive = core.MakeIndirectObject(signDict)
	return sig
}

// EncodedData returns the original, undecoded source bytes retained for
// direct embedding (currently JPEG only), or nil if unavailable. The returned
// slice must not be mutated.
func (img *Image) EncodedData() []byte { return img.encodedData }

// ContentStreamWrapper wraps the Page's contentstream into q ... Q blocks.
type ContentStreamWrapper interface{ WrapContentStream(page *PdfPage) error }

// AllFields returns a flattened list of all fields in the form.
func (a *PdfAcroForm) AllFields() []*PdfField {
	if a == nil {
		return nil
	}
	var fields []*PdfField
	if a.Fields != nil {
		for _, field := range *a.Fields {
			fields = append(fields, flattenFieldTree(field)...)
		}
	}
	return fields
}

// SetDuplex sets the value of the duplex.
func (vp *ViewerPreferences) SetDuplex(duplex Duplex) { vp.duplex = duplex }

// AddExtension adds the specified extension to the Extensions dictionary.
// See section 7.1.2 "Extensions Dictionary" (pp. 108-109 PDF32000_2008).
func (w *PdfWriter) AddExtension(extName, baseVersion string, extLevel int) {
	extensions, ok := core.GetDict(w.catalog.Get("Extensions"))
	if !ok {
		extensions = core.MakeDict()
		w.catalog.Set("Extensions", extensions)
	}
	dict, ok := core.GetDict(extensions.Get(core.PdfObjectName(extName)))
	if !ok {
		dict = core.MakeDict()
		extensions.Set(core.PdfObjectName(extName), dict)
	}
	if s, _ := core.GetNameVal(dict.Get("BaseVersion")); s != baseVersion {
		dict.Set("BaseVersion", core.MakeName(baseVersion))
	}
	if extensionLevel, _ := core.GetIntVal(dict.Get("ExtensionLevel")); extensionLevel != extLevel {
		dict.Set("ExtensionLevel", core.MakeInteger(int64(extLevel)))
	}
}

// Write outputs the object as it is to be written to file.
func (p *PdfTransformParamsDocMDP) Write() []byte { return p.ToPdfObject().Write() }

func newPdfFontSimpleFromStdName(basefont StdFontName) (pdfFontSimple, error) {
	stdFontByName, ok := fonts.NewStdFontByName(basefont)
	if !ok {
		return pdfFontSimple{}, ErrFontNotSupported
	}
	fontSimple := newPdfFontSimpleFromStdFont(stdFontByName)
	return fontSimple, nil
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationLink) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("Link"))
	if a.action != nil && a.action.context != nil {
		dict.Set("A", a.action.context.ToPdfObject())
	} else if a.A != nil {
		dict.Set("A", a.A)
	}
	dict.SetIfNotNil("Dest", a.Dest)
	dict.SetIfNotNil("H", a.H)
	dict.SetIfNotNil("PA", a.PA)
	dict.SetIfNotNil("QuadPoints", a.QuadPoints)
	dict.SetIfNotNil("BS", a.BS)
	return indirect
}

func (l *LTV) getOCSPs(certs []*x509.Certificate, certByKey map[string]*x509.Certificate) ([][]byte, error) {
	dataList := make([][]byte, 0, len(certs))
	for _, cert := range certs {
		for _, ocspServer := range cert.OCSPServer {
			if l.CertClient.IsCA(cert) {
				continue
			}
			cert2, ok := certByKey[cert.Issuer.CommonName]
			if !ok {
				common.Log.Debug("WARN: Skipping OCSP request: issuer certificate not found")
				continue
			}
			_, data, err := l.OCSPClient.MakeRequest(ocspServer, cert, cert2)
			if err != nil {
				common.Log.Debug("WARN: OCSP request error: %v", err)
				continue
			}
			dataList = append(dataList, data)
		}
	}
	return dataList, nil
}

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

const (
	RevocationStatusNotChecked RevocationStatus = iota
	RevocationStatusUnknown
	RevocationStatusGood
	RevocationStatusRevoked
)

const (
	RevocationCheckNone RevocationCheck = iota
	RevocationCheckOffline
	RevocationCheckOnline
)

type fontCommon struct {
	baseFont       string
	subtype        string
	name           string
	toUnicode      core.PdfObject
	toUnicodeCMap  *cmap.CMap
	fontDescriptor *PdfFontDescriptor
	objectNumber   int64
}

// Colorspace returns a best-effort, parameterless CalRGB colorspace. The
// authoritative parameterized colorspace is tracked by the graphics state.
func (c *PdfColorCalRGB) Colorspace() PdfColorspace { return NewPdfColorspaceCalRGB() }

func (w *PdfWriter) writeOutlines() error {
	if w.outlineTree == nil {
		return nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("OutlineTree: %+v", w.outlineTree)
	}
	obj := w.outlineTree.ToPdfObject()
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Outlines: %+v (%T, p:%p)", obj, obj, obj)
	}
	w.catalog.Set("Outlines", obj)
	err := w.addObjects(obj)
	if err != nil {
		return err
	}
	return nil
}

// Components returns the color's component values: C, M, Y, K.
func (c *PdfColorDeviceCMYK) Components() []float64 {
	return []float64{c[0], c[1], c[2], c[3]}
}

// NewXObjectForm creates a brand new XObject Form. Creates a new underlying PDF object stream primitive.
func NewXObjectForm() *XObjectForm {
	form := &XObjectForm{}
	stream := &core.PdfObjectStream{}
	stream.PdfObjectDictionary = core.MakeDict()
	form.primitive = stream
	return form
}

// Colorspace returns a Pattern colorspace whose underlying colorspace, if any,
// is derived from the underlying color.
func (p *PdfColorPatternType3) Colorspace() PdfColorspace {
	specialPatternCS := NewPdfColorspaceSpecialPattern()
	if p.Color != nil {
		specialPatternCS.UnderlyingCS = p.Color.Colorspace()
	}
	return specialPatternCS
}

// ColorAt returns the color of the image pixel specified by the x and y coordinates.
func (img *Image) ColorAt(x, y int) (color.Color, error) {
	bytesPerLine := imageutil.BytesPerLine(int(img.Width), int(img.BitsPerComponent), img.ColorComponents)
	switch img.ColorComponents {
	case 1:
		return imageutil.ColorAtGrayscale(x, y, int(img.BitsPerComponent), bytesPerLine, img.Data, img.decode)
	case 3:
		return imageutil.ColorAtNRGBA(x, y, int(img.Width), bytesPerLine, int(img.BitsPerComponent), img.Data, img.alpha, img.decode)
	case 4:
		return imageutil.ColorAtCMYK(x, y, int(img.Width), img.Data, img.decode)
	}
	common.Log.Debug("ERROR: unsupported image. %d components, %d bits per component", img.ColorComponents, img.BitsPerComponent)
	return nil, errors.New("unsupported image colorspace")
}

func newPdfColorspaceDeviceNAttributesFromPdfObject(obj core.PdfObject) (*PdfColorspaceDeviceNAttributes, error) {
	deviceNAttributesCS := &PdfColorspaceDeviceNAttributes{}
	var dict *core.PdfObjectDictionary
	switch v := obj.(type) {
	case *core.PdfIndirectObject:
		deviceNAttributesCS.container = v
		var ok bool
		dict, ok = v.PdfObject.(*core.PdfObjectDictionary)
		if !ok {
			common.Log.Error("DeviceN attribute type error")
			return nil, errors.New("type error")
		}
	case *core.PdfObjectDictionary:
		dict = v
	case *core.PdfObjectReference:
		resolve := v.Resolve()
		return newPdfColorspaceDeviceNAttributesFromPdfObject(resolve)
	default:
		common.Log.Error("DeviceN attribute type error")
		return nil, errors.New("type error")
	}
	if subtype := dict.Get("Subtype"); subtype != nil {
		name, ok := core.TraceToDirectObject(subtype).(*core.PdfObjectName)
		if !ok {
			common.Log.Error("DeviceN attribute Subtype type error")
			return nil, errors.New("type error")
		}
		deviceNAttributesCS.Subtype = name
	}
	if colorants := dict.Get("Colorants"); colorants != nil {
		deviceNAttributesCS.Colorants = colorants
	}
	if process := dict.Get("Process"); process != nil {
		deviceNAttributesCS.Process = process
	}
	if mixingHints := dict.Get("MixingHints"); mixingHints != nil {
		deviceNAttributesCS.MixingHints = mixingHints
	}
	return deviceNAttributesCS, nil
}

func (a *PdfAcroForm) fillImageWithAppearance(fieldImageProvider FieldImageProvider, appGen FieldAppearanceGenerator) error {
	if a == nil {
		return nil
	}
	fieldImageValues, err := fieldImageProvider.FieldImageValues()
	if err != nil {
		return err
	}
	for _, allField := range a.AllFields() {
		partialName := allField.PartialName()
		var img *Image
		var ok bool
		if fullName, err := allField.FullName(); err == nil {
			img, ok = fieldImageValues[fullName]
		}
		if !ok {
			img, ok = fieldImageValues[partialName]
		}
		if !ok {
			common.Log.Debug("WARN: form field %s not found in the provider. Skipping.", partialName)
			continue
		}
		switch v := allField.GetContext().(type) {
		case *PdfFieldButton:
			if v.IsPush() {
				v.SetFillImage(img)
			}
		}
		if appGen == nil {
			continue
		}
		for _, annotation := range allField.Annotations {
			generateAppearanceDict, err := appGen.GenerateAppearanceDict(a, allField, annotation)
			if err != nil {
				return err
			}
			annotation.AP = generateAppearanceDict
			annotation.ToPdfObject()
		}
	}
	return nil
}

// ToPdfObject returns the PDF representation of the outline tree node.
func (n *PdfOutlineTreeNode) ToPdfObject() core.PdfObject {
	return n.GetContext().ToPdfObject()
}

// SetPdfCreator sets the Creator attribute of the output PDF.
func SetPdfCreator(creator string) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	pdfCreator = creator
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotation3D) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("3D"))
	dict.SetIfNotNil("3DD", a.T3DD)
	dict.SetIfNotNil("3DV", a.T3DV)
	dict.SetIfNotNil("3DA", a.T3DA)
	dict.SetIfNotNil("3DI", a.T3DI)
	dict.SetIfNotNil("3DB", a.T3DB)
	return indirect
}

// GetRuneMetrics returns the char metrics for a rune.
// TODO(peterwilliams97) There is nothing callers can do if no CharMetrics are found so we might as
// well give them 0 width. There is no need for the bool return.
func (f *PdfFont) GetRuneMetrics(r rune) (CharMetrics, bool) {
	actualFont := f.actualFont()
	if actualFont == nil {
		common.Log.Debug("ERROR: GetGlyphCharMetrics Not implemented for font type=%#T", f.context)
		return fonts.CharMetrics{}, false
	}
	if runeMetrics, ok := actualFont.GetRuneMetrics(r); ok {
		return runeMetrics, true
	}
	if fontDescriptor, err := f.GetFontDescriptor(); err == nil && fontDescriptor != nil {
		return fonts.CharMetrics{Wx: fontDescriptor.missingWidth}, true
	}
	common.Log.Debug("GetGlyphCharMetrics: No metrics for font=%s", f)
	return fonts.CharMetrics{}, false
}

// GetPageIndirectObject returns the indirect object of page for the specified page number.
func (w *PdfWriter) GetPageIndirectObject(pageNum int) (*core.PdfIndirectObject, error) {
	if pageNum < 0 || pageNum >= len(w.pageObjs) {
		return nil, errors.New("page number out of range")
	}
	return w.pageObjs[pageNum], nil
}

func newPdfFontType3FromCommon(fc *fontCommon) *pdfFontType3 { return &pdfFontType3{fontCommon: *fc} }

// CharcodesToStrings returns the unicode strings corresponding to `charcodes`.
// The int returns are the number of strings and the number of unconvereted codes.
// NOTE: The number of strings returned is equal to the number of charcodes
func (f *PdfFont) CharcodesToStrings(charcodes []textencoding.CharCode, replacementText string) ([]string, int, int) {
	baseFields := f.baseFields()
	strs := make([]string, 0, len(charcodes))
	n := 0
	encoder := f.Encoder()
	ok := baseFields.toUnicodeCMap != nil && f.IsSimple() && f.Subtype() == "TrueType" && !strings.Contains(baseFields.toUnicodeCMap.Name(), "Identity-")
	if !ok && encoder != nil {
		switch v := encoder.(type) {
		case textencoding.SimpleEncoder:
			baseName := v.BaseName()
			if _, ok := predefinedEncodingNames[baseName]; ok {
				for _, charcode := range charcodes {
					if charcodeToRune, ok := encoder.CharcodeToRune(charcode); ok {
						strs = append(strs, string(charcodeToRune))
					} else {
						common.Log.Debug("ERROR: No rune. code=0x%04x charcodes=[% 04x] CID=%t\n"+"\tfont=%s\n\tencoding=%s", charcode, charcodes, baseFields.isCIDFont(), f, encoder)
						n++
						strs = append(strs, cmap.MissingCodeString)
					}
				}
				return strs, len(strs), n
			}
		}
	}
	gidRegistry, _ := encoder.(textencoding.GIDRegistry)
	_, found := encoder.(*textencoding.IdentityEncoder)
	for _, charcode := range charcodes {
		if baseFields.toUnicodeCMap != nil {
			if charcodeToUnicode, ok := baseFields.toUnicodeCMap.CharcodeToUnicode(cmap.CharCode(charcode)); ok {
				lastRuneInString, _ := utf8.DecodeLastRuneInString(charcodeToUnicode)
				s := unicodeCategory(lastRuneInString)
				if s != "Co" {
					if gidRegistry != nil {
						gidRegistry.RegisterGID(textencoding.GID(charcode))
					}
					strs = append(strs, charcodeToUnicode)
					continue
				}
				common.Log.Debug("ERROR: ToUnicode Cmap has incorrect mapping. code=0x%04x is mapped to an invalid code point %s", charcode, charcodeToUnicode)
			}
		}
		if encoder != nil {
			if charcodeToRune, ok := encoder.CharcodeToRune(charcode); ok {
				s := unicodeCategory(charcodeToRune)
				if s != "Co" {
					strs = append(strs, string(charcodeToRune))
					continue
				}
				common.Log.Debug("ERROR: encoder failed. code=0x%04x is decoded to an invalid run %s", charcode, string(charcodeToRune))
			}
			if found {
				if n := rune(charcode); unicodeCategory(n) != "Co" {
					strs = append(strs, string(n))
					continue
				}
			}
		}
		if replacementText != "" {
			strs = append(strs, replacementText)
		} else {
			common.Log.Debug("ERROR: No rune. code=0x%04x charcodes=[% 04x] CID=%t\n"+"\tfont=%s\n\tencoding=%s", charcode, charcodes, baseFields.isCIDFont(), f, encoder)
			n++
			strs = append(strs, cmap.MissingCodeString)
		}
	}
	if n != 0 {
		common.Log.Debug("ERROR: Couldn't convert to unicode. Using input.\n"+"\tnumChars=%d numMisses=%d\n"+"\tfont=%s", len(charcodes), n, f)
	}
	return strs, len(strs), n
}

// CharcodesToUnicode converts the character codes `charcodes` to a slice of runes.
// How it works:
//  1. Use the ToUnicode CMap if there is one.
//  2. Use the underlying font's encoding.
func (f *PdfFont) CharcodesToUnicode(charcodes []textencoding.CharCode) []rune {
	charcodesToStrings, _, _ := f.CharcodesToStrings(charcodes, "")
	return []rune(strings.Join(charcodesToStrings, ""))
}

type distributionPointName struct {
	FullName []asn1.RawValue `asn1:"optional,tag:0"`

	// RelativeName is nameRelativeToCRLIssuer, a single
	// RelativeDistinguishedName; only its presence matters.
	RelativeName asn1.RawValue `asn1:"optional,tag:1"`
}

// ToPdfOutlineItem returns a low level PdfOutlineItem object,
// based on the current instance.
func (o *OutlineItem) ToPdfOutlineItem() (*PdfOutlineItem, int64) {
	item := NewPdfOutlineItem()
	item.Title = core.MakeEncodedString(o.Title, true)
	item.Dest = o.Dest.ToPdfObject()
	var items []*PdfOutlineItem
	var n int64
	var item2 *PdfOutlineItem
	for _, entry := range o.Entries {
		item3, count := entry.ToPdfOutlineItem()
		item3.Parent = &item.PdfOutlineTreeNode
		if item2 != nil {
			item2.Next = &item3.PdfOutlineTreeNode
			item3.Prev = &item2.PdfOutlineTreeNode
		}
		items = append(items, item3)
		n += count
		item2 = item3
	}
	count := len(items)
	n += int64(count)
	if count > 0 {
		item.First = &items[0].PdfOutlineTreeNode
		item.Last = &items[count-1].PdfOutlineTreeNode
		item.Count = &n
	}
	return item, n
}

func isBinaryData(data []byte) bool {
	if len(data) < 4 {
		return true
	}
	for i := range data[:4] {
		n := rune(i)
		if !unicode.Is(unicode.ASCII_Hex_Digit, n) && !unicode.IsSpace(n) {
			return true
		}
	}
	return false
}

// AddContentStreamByString adds content stream by string. Puts the content
// string into a stream object and points the content stream towards it.
func (p *PdfPage) AddContentStreamByString(contentStr string) error {
	stream, err := core.MakeStream([]byte(contentStr), core.NewFlateEncoder())
	if err != nil {
		return err
	}
	if p.Contents == nil {
		p.Contents = stream
	} else {
		obj := core.TraceToDirectObject(p.Contents)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			arr = core.MakeArray(obj)
		}
		arr.Append(stream)
		p.Contents = arr
	}
	return nil
}

// NewPdfAnnotationSquare returns a new square annotation.
func NewPdfAnnotationSquare() *PdfAnnotationSquare {
	annot := NewPdfAnnotation()
	square := &PdfAnnotationSquare{}
	square.PdfAnnotation = annot
	square.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(square)
	return square
}

// SetSkipObjectCopy controls whether Write skips the defensive deep copy of the
// source object graph.
//
// By default, when a mutating pipeline stage is present (optimizer, encryption,
// PDF/A conversion), the writer deep-copies source objects so those stages don't
// modify the originating PdfReader's objects. That copy is the single largest
// source of memory/time overhead when writing large documents.
//
// When skip is true the copy is bypassed and those stages mutate the source
// objects in place. This significantly reduces memory use and write time, but
// the source objects (and the PdfReader they came from) are consumed: they MUST
// NOT be read or written again after Write. Use this only for one-shot
// load -> (optimize/encrypt) -> write -> discard pipelines.
//
// The copy is still performed regardless of this setting in append mode and when
// orphan pages require it (see canSkipCopy), as those need the copy for
// correctness rather than source protection.
func (w *PdfWriter) SetSkipObjectCopy(skip bool) { w.skipObjectCopy = skip }

func newPdfOutlineFromIndirectObject(indirect *core.PdfIndirectObject) (*PdfOutline, error) {
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return nil, fmt.Errorf("outline object not a dictionary")
	}
	outline := NewPdfOutline()
	if obj := dict.Get("Type"); obj != nil {
		name, ok := obj.(*core.PdfObjectName)
		if ok {
			if *name != "Outlines" {
				common.Log.Debug("ERROR Type != Outlines (%s)", *name)
			}
		}
	}
	if count := dict.Get("Count"); count != nil {
		n, err := core.GetNumberAsInt64(count)
		if err != nil {
			return nil, err
		}
		outline.Count = &n
	}
	return outline, nil
}

// IsTiling specifies if the pattern is a tiling pattern.
func (p *PdfPattern) IsTiling() bool { return p.PatternType == 1 }

// NewXObjectImageFromStream builds the image xobject from a stream object.
// An image dictionary is the dictionary portion of a stream object representing an image XObject.
func NewXObjectImageFromStream(stream *core.PdfObjectStream) (*XObjectImage, error) {
	xobj := &XObjectImage{}
	xobj.primitive = stream
	dict := stream.PdfObjectDictionary
	encoder, err := core.NewEncoderFromStream(stream)
	if err != nil {
		return nil, err
	}
	xobj.Filter = encoder
	if width := core.TraceToDirectObject(dict.Get("Width")); width != nil {
		num, ok := width.(*core.PdfObjectInteger)
		if !ok {
			return nil, errors.New("invalid image width object")
		}
		n := int64(*num)
		xobj.Width = &n
	} else {
		return nil, errors.New("width missing")
	}
	if height := core.TraceToDirectObject(dict.Get("Height")); height != nil {
		num, ok := height.(*core.PdfObjectInteger)
		if !ok {
			return nil, errors.New("invalid image height object")
		}
		n := int64(*num)
		xobj.Height = &n
	} else {
		return nil, errors.New("height missing")
	}
	if colorSpace := core.TraceToDirectObject(dict.Get("ColorSpace")); colorSpace != nil {
		cs, err := NewPdfColorspaceFromPdfObject(colorSpace)
		if err != nil {
			return nil, err
		}
		xobj.ColorSpace = cs
	} else {
		common.Log.Debug("XObject Image colorspace not specified - assuming 1 color component - DeviceGray")
		xobj.ColorSpace = NewPdfColorspaceDeviceGray()
	}
	if bitsPerComponent := core.TraceToDirectObject(dict.Get("BitsPerComponent")); bitsPerComponent != nil {
		num, ok := bitsPerComponent.(*core.PdfObjectInteger)
		if !ok {
			return nil, errors.New("invalid image height object")
		}
		n := int64(*num)
		xobj.BitsPerComponent = &n
	}
	xobj.Intent = dict.Get("Intent")
	xobj.ImageMask = dict.Get("ImageMask")
	xobj.Mask = dict.Get("Mask")
	xobj.Decode = dict.Get("Decode")
	xobj.Interpolate = dict.Get("Interpolate")
	xobj.Alternatives = dict.Get("Alternatives")
	xobj.SMask = dict.Get("SMask")
	xobj.SMaskInData = dict.Get("SMaskInData")
	xobj.Matte = dict.Get("Matte")
	xobj.Name = dict.Get("Name")
	xobj.StructParent = dict.Get("StructParent")
	xobj.ID = dict.Get("ID")
	xobj.OPI = dict.Get("OPI")
	xobj.Metadata = dict.Get("Metadata")
	xobj.OC = dict.Get("OC")
	xobj.Stream = stream.Stream
	return xobj, nil
}

// IsColored specifies if the pattern is colored.
func (p *PdfTilingPattern) IsColored() bool {
	if p.PaintType != nil && *p.PaintType == 1 {
		return true
	}
	return false
}

// NewPdfAnnotationRichMedia returns a new rich media annotation.
func NewPdfAnnotationRichMedia() *PdfAnnotationRichMedia {
	annot := NewPdfAnnotation()
	richMedia := &PdfAnnotationRichMedia{}
	richMedia.PdfAnnotation = annot
	annot.SetContext(richMedia)
	return richMedia
}

// ImageToRGB converts image in CalGray color space to RGB (A, B, C -> X, Y, Z).
func (cs *PdfColorspaceCalGray) ImageToRGB(img Image) (Image, error) {
	reader := sampling.NewReader(img.getBase())
	imageBase := imageutil.NewImageBase(int(img.Width), int(img.Height), int(img.BitsPerComponent), 3, nil, nil, nil)
	writer := sampling.NewWriter(imageBase)
	f := math.Pow(2, float64(img.BitsPerComponent)) - 1
	nums := make([]uint32, 3)
	var (
		n                         uint32
		ANorm, X, Y, Z, v, val, x float64
		err                       error
	)
	for {
		n, err = reader.ReadSample()
		if err == io.EOF {
			break
		} else if err != nil {
			return img, err
		}
		ANorm = float64(n) / f
		X = cs.WhitePoint[0] * math.Pow(ANorm, cs.Gamma)
		Y = cs.WhitePoint[1] * math.Pow(ANorm, cs.Gamma)
		Z = cs.WhitePoint[2] * math.Pow(ANorm, cs.Gamma)
		v = 3.240479*X + -1.537150*Y + -0.498535*Z
		val = -0.969256*X + 1.875992*Y + 0.041556*Z
		x = 0.055648*X + -0.204043*Y + 1.057311*Z
		v = math.Min(math.Max(v, 0), 1.0)
		val = math.Min(math.Max(val, 0), 1.0)
		x = math.Min(math.Max(x, 0), 1.0)
		nums[0] = uint32(v * f)
		nums[1] = uint32(val * f)
		nums[2] = uint32(x * f)
		if err = writer.WriteSamples(nums); err != nil {
			return img, err
		}
	}
	return imageFromImageBase(&imageBase), nil
}

// NewPdfActionThread returns a new "thread" action.
func NewPdfActionThread() *PdfActionThread {
	action := NewPdfAction()
	threadAction := &PdfActionThread{}
	threadAction.PdfAction = action
	action.SetContext(threadAction)
	return threadAction
}

// GetContainingPdfObject returns the container of the shading object (indirect object).
func (s *PdfShading) GetContainingPdfObject() core.PdfObject { return s.primitive }

// WatermarkImageOptions contains options for configuring the image watermark process.
type WatermarkImageOptions struct {
	Alpha               float64
	FitToWidth          bool
	PreserveAspectRatio bool
	Angle               float64
}

// PdfColorspaceDeviceN represents a DeviceN color space. DeviceN color spaces are similar to Separation color
// spaces, except they can contain an arbitrary number of color components.
/*
	Format: [/DeviceN names alternateSpace tintTransform]
        or: [/DeviceN names alternateSpace tintTransform attributes]
*/
type PdfColorspaceDeviceN struct {
	ColorantNames  *core.PdfObjectArray
	AlternateSpace PdfColorspace
	TintTransform  PdfFunction
	Attributes     *PdfColorspaceDeviceNAttributes
	container      *core.PdfIndirectObject
}

func (w *PdfWriter) adjustXRefAffectedVersion(ok bool) {
	if ok && w.version.Major == 1 && w.version.Minor < 5 {
		w.version.Minor = 5
	}
}

func (r *PdfReader) newPdfOutlineItemFromIndirectObject(indirect *core.PdfIndirectObject) (*PdfOutlineItem, error) {
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return nil, fmt.Errorf("outline object not a dictionary")
	}
	item := NewPdfOutlineItem()
	title := dict.Get("Title")
	if title == nil {
		return nil, fmt.Errorf("missing Title from Outline Item (required)")
	}
	titleStr, isTitleStr := core.GetString(title)
	if !isTitleStr {
		return nil, fmt.Errorf("title not a string (%T)", title)
	}
	item.Title = titleStr
	if count := dict.Get("Count"); count != nil {
		num, ok := count.(*core.PdfObjectInteger)
		if !ok {
			return nil, fmt.Errorf("count not an integer (%T)", count)
		}
		n := int64(*num)
		item.Count = &n
	}
	if dest := dict.Get("Dest"); dest != nil {
		item.Dest = core.ResolveReference(dest)
		if !r.lazy {
			err := r.traverseObjectData(item.Dest)
			if err != nil {
				return nil, err
			}
		}
	}
	if a := dict.Get("A"); a != nil {
		item.A = core.ResolveReference(a)
		if !r.lazy {
			err := r.traverseObjectData(item.A)
			if err != nil {
				return nil, err
			}
		}
	}
	if se := dict.Get("SE"); se != nil {
		item.SE = nil
	}
	if c := dict.Get("C"); c != nil {
		item.C = core.ResolveReference(c)
	}
	if f := dict.Get("F"); f != nil {
		item.F = core.ResolveReference(f)
	}
	return item, nil
}

// NewPdfActionResetForm returns a new "reset form" action.
func NewPdfActionResetForm() *PdfActionResetForm {
	action := NewPdfAction()
	resetFormAction := &PdfActionResetForm{}
	resetFormAction.PdfAction = action
	action.SetContext(resetFormAction)
	return resetFormAction
}

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

func (t *trustEvaluator) reportUnknownRevocation(result *SignatureValidationResult, strs ...string) {
	if t.opts.RequireRevocationStatus {
		result.TrustErrors = append(result.TrustErrors, strs...)
		result.IsTrusted = false
		return
	}
	result.TrustWarnings = append(result.TrustWarnings, strs...)
}

// PdfColorspaceICCBased format [/ICCBased stream]
//
// The stream shall contain the ICC profile.
// A conforming reader shall support ICC.1:2004:10 as required by PDF 1.7, which will enable it
// to properly render all embedded ICC profiles regardless of the PDF version
//
// In the current implementation, we rely on the alternative colormap provided.
type PdfColorspaceICCBased struct {
	N         int
	Alternate PdfColorspace

	// If omitted ICC not supported: then use DeviceGray,
	// DeviceRGB or DeviceCMYK for N=1,3,4 respectively.
	Range          []float64
	Metadata       *core.PdfObjectStream
	Data           []byte
	container      *core.PdfIndirectObject
	stream         *core.PdfObjectStream
	iccTransform   *icctransform.Transform
	transformOnce  sync.Once
	transformReady bool
}

// GetNumComponents returns the number of color components (1 for CalGray).
func (c *PdfColorCalGray) GetNumComponents() int { return 1 }

// ParserMetadata gets the parser  metadata.
func (r *CompliancePdfReader) ParserMetadata() core.ParserMetadata {
	if r.parserMetadata == (core.ParserMetadata{}) {
		r.parserMetadata, _ = r.parser.ParserMetadata()
	}
	return r.parserMetadata
}

type convergedPlanInputs struct {
	layout               *linearizationLayout
	objects              map[int64]preserializedObject
	objStreams           *objectStreamsState
	hintObjNum           int64
	firstXrefObjs        []int64
	trailerSize          int64
	linDictLen           int
	firstXrefLen         int
	hintLen              int
	xrefFirstEntryOffset int64
	firstSectionStart    int64
	mainSectionStart     int64
}

// SetPageNumber sets the page number.
func (k *KDict) SetPageNumber(pageNumber int64) { k.pageNumber = pageNumber }

// SetImageHandler sets the image handler used by the package.
func SetImageHandler(imgHandling ImageHandler) { ImageHandling = imgHandling }

type modelManager struct {
	primitiveByModel map[PdfModel]core.PdfObject
	modelByPrimitive map[core.PdfObject]PdfModel
	primitiveLock    sync.RWMutex
	modelLock        sync.RWMutex
}

// AlphaData returns the image's alpha channel data (stored with the image's
// bits per component and a single color component), or nil if the image has
// none. The returned slice must not be mutated; use SetAlpha or AlphaMap to
// change the alpha channel.
func (img *Image) AlphaData() []byte { return img.alpha }

func ocspResponderAuthorizedByIssuer(resp *ocsp.Response, cert, cert2 *x509.Certificate) bool {
	certificate := resp.Certificate
	if certificate == nil || certificate.Equal(cert2) {
		return true
	}
	if certificate.Equal(cert) || !hasExtKeyUsage(certificate, x509.ExtKeyUsageOCSPSigning) {
		return false
	}
	return !resp.ProducedAt.Before(certificate.NotBefore) && !resp.ProducedAt.After(certificate.NotAfter)
}

const maxBitsPerComponent = 16

// NewViewerPreferences returns a new ViewerPreferences object with
// default empty values.
func NewViewerPreferences() *ViewerPreferences { return &ViewerPreferences{} }

// ColorFromFloats returns a new PdfColor based on the input slice of color
// components. The slice should contain a single element.
func (cs *PdfColorspaceSpecialIndexed) ColorFromFloats(vals []float64) (PdfColor, error) {
	if len(vals) != 1 {
		return nil, errors.New("range check")
	}
	N := cs.Base.GetNumComponents()
	n := int(vals[0]) * N
	if n < 0 || (n+N-1) >= len(cs.lookupTable) {
		common.Log.Debug("color out of CS range: ", n)
		return nil, ErrColorOutOfRange
	}
	data := cs.lookupTable[n : n+N]
	var floats []float64
	for _, b := range data {
		floats = append(floats, float64(b)/255.0)
	}
	col, err := cs.Base.ColorFromFloats(floats)
	if err != nil {
		return nil, err
	}
	return col, nil
}

func removeLazyTempFile(tempFile string) {
	if tempFile == "" {
		return
	}
	if err := os.Remove(filepath.Clean(tempFile)); err != nil {
		common.Log.Debug("Failed removing lazy temp file %q: %v", tempFile, err)
	}
}

func findRevocationEntry(crls []crlEvidence, cert *x509.Certificate, t time.Time) *x509.RevocationListEntry {
	for _, crl := range crls {
		entryFor := crl.entryFor(cert)
		if entryFor == nil || entryFor.ReasonCode == crlReasonCertificateHold || entryFor.ReasonCode == crlReasonRemoveFromCRL {
			continue
		}
		if !entryFor.RevocationTime.After(t) {
			return entryFor
		}
	}
	return nil
}

// ColorFromFloats returns a new PdfColor based on the input slice of color
// components. The slice should contain a single element between 0 and 1.
func (cs *PdfColorspaceDeviceGray) ColorFromFloats(vals []float64) (PdfColor, error) {
	if len(vals) != 1 {
		return nil, errors.New("range check")
	}
	val := vals[0]
	if val < 0.0 || val > 1.0 {
		common.Log.Debug("Incompatibility: Range outside [0,1]")
	}
	if val < 0.0 {
		val = 0.0
	} else if val > 1.0 {
		val = 1.0
	}
	return NewPdfColorDeviceGray(val), nil
}

// Mask returns the uin32 bitmask for the specific flag.
func (f FieldFlag) Mask() uint32 { return uint32(f) }

var symbolicFontEncodings = map[string]string{"Symbol": "SymbolEncoding", "ZapfDingbats": "ZapfDingbatsEncoding"}

// NewPdfAnnotationPolyLine returns a new polyline annotation.
func NewPdfAnnotationPolyLine() *PdfAnnotationPolyLine {
	annot := NewPdfAnnotation()
	polyLine := &PdfAnnotationPolyLine{}
	polyLine.PdfAnnotation = annot
	polyLine.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(polyLine)
	return polyLine
}

func (img *Image) clearSourceMetadata() {
	img.iccProfile = nil
	img.encodedData = nil
	img.encodedFormat = ""
}

func subsetTag(data []byte) string {
	const (
		subsetTagAlphabet    = "QWERTYUIOPASDFGHJKLZXCVBNM"
		subsetTagAlphabetLen = len(subsetTagAlphabet)
	)
	sum256 := sha256.Sum256(data)
	var data2 [6]byte
	for i := 0; i < 6; i++ {
		data2[i] = subsetTagAlphabet[int(sum256[i])%subsetTagAlphabetLen]
	}
	return string(data2[:])
}

// GetContainingPdfObject implements interface PdfModel.
func (a *PdfAnnotation) GetContainingPdfObject() core.PdfObject { return a.primitive }

func hasExtKeyUsage(cert *x509.Certificate, usage x509.ExtKeyUsage) bool {
	for _, extKeyUsage := range cert.ExtKeyUsage {
		if extKeyUsage == usage {
			return true
		}
	}
	return false
}

type (
	ocspEvidence struct {
		raw    []byte
		digest [32]byte
		online bool
	}
	crlEvidence struct {
		*parsedCRL
		online bool
	}
)

// Width returns the width of `rect`.
func (r *PdfRectangle) Width() float64 { return math.Abs(r.Urx - r.Llx) }

// NewPdfActionGoToR returns a new "go to remote" action.
func NewPdfActionGoToR() *PdfActionGoToR {
	action := NewPdfAction()
	goToRAction := &PdfActionGoToR{}
	goToRAction.PdfAction = action
	action.SetContext(goToRAction)
	return goToRAction
}

// Register registers (caches) a model to primitive object relationship.
func (m *modelManager) Register(primitive core.PdfObject, model PdfModel) {
	m.primitiveLock.Lock()
	defer m.primitiveLock.Unlock()
	m.modelLock.Lock()
	defer m.modelLock.Unlock()
	m.primitiveByModel[model] = primitive
	m.modelByPrimitive[primitive] = model
}

func buildFirstXrefTable(writer *PdfWriter, firstXrefObjs []int64, objects map[int64]preserializedObject, hintObjNum int64, hintOffset int64, mainXrefOffset int64, linDictOffset int64, trailerSize int64) []byte {
	table := map[int64]int64{}
	for _, firstXrefObj := range firstXrefObjs {
		switch firstXrefObj {
		case linearizationDictObjNum2:
			table[firstXrefObj] = linDictOffset
		case hintObjNum:
			table[firstXrefObj] = hintOffset
		default:
			table[firstXrefObj] = objects[firstXrefObj].offset
		}
	}
	xrefTable, _ := buildXrefTable(writer, firstXrefObjs, table, trailerSize, mainXrefOffset, true, 0, false)
	return xrefTable
}

func buildObjectStreamBytes(n int64, objStreams *core.PdfObjectStreams, crypter *core.PdfCrypt) ([]byte, error) {
	var buf bytes.Buffer
	var buf2 bytes.Buffer
	var count int64
	for _, obj := range objStreams.Elements() {
		indirect, ok := obj.(*core.PdfIndirectObject)
		if !ok {
			return nil, fmt.Errorf("object stream %d: contains non-indirect %T", n, obj)
		}
		data := append(indirect.PdfObject.Write(), ' ')
		fmt.Fprintf(&buf, "%d %d ", indirect.ObjectNumber, count)
		buf2.Write(data)
		count += int64(len(data))
	}
	data := append(buf.Bytes(), buf2.Bytes()...)
	stream, err := core.MakeStream(data, core.NewFlateEncoder())
	if err != nil {
		return nil, fmt.Errorf("object stream %d: encode: %w", n, err)
	}
	stream.ObjectNumber = n
	stream.PdfObjectDictionary.Set("Type", core.MakeName("ObjStm"))
	stream.PdfObjectDictionary.Set("N", core.MakeInteger(int64(objStreams.Len())))
	stream.PdfObjectDictionary.Set("First", core.MakeInteger(int64(buf.Len())))
	if crypter != nil {
		if err := crypter.Encrypt(stream, n, 0); err != nil {
			return nil, fmt.Errorf("object stream %d: encrypt: %w", n, err)
		}
	}
	var buf3 bytes.Buffer
	fmt.Fprintf(&buf3, "%d 0 obj\n", n)
	buf3.Write(stream.PdfObjectDictionary.Write())
	buf3.WriteString("\nstream\n")
	buf3.Write(stream.Stream)
	buf3.WriteString("\nendstream\nendobj\n")
	return buf3.Bytes(), nil
}

func cffTable(loader *opentype.Loader) ([]byte, error) {
	mustNewTag := opentype.MustNewTag("CFF ")
	rawTable, err := loader.RawTable(mustNewTag)
	if err != nil {
		return nil, fmt.Errorf("CFF table not found: %w", err)
	}
	return rawTable, nil
}

// SetContext set the sub annotation (context).
func (s *PdfShading) SetContext(ctx PdfModel) { s.context = ctx }

func prependSubsetTag(s, s2 string) string {
	if strings.Contains(s, "+") {
		split := strings.Split(s, "+")
		if len(split) == 2 {
			s = split[1]
		}
	}
	return s2 + "+" + s
}

// Colorspace returns a best-effort, parameterless Lab colorspace. The
// authoritative parameterized colorspace is tracked by the graphics state.
func (c *PdfColorLab) Colorspace() PdfColorspace { return NewPdfColorspaceLab() }

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

// SignatureValidationResult defines the response from the signature validation handler.
type SignatureValidationResult struct {
	// List of errors when validating the signature.
	Errors   []string
	IsSigned bool

	// IsVerified is true when the signature value matches the signed
	// document bytes under the signer's public key. It is an integrity
	// verdict only: the key comes from the certificate embedded in the
	// document, so a self-signed certificate under any name verifies. It
	// says nothing about who the signer is; see IsTrusted for that.
	IsVerified bool

	// IsTrusted is true when the signer certificate chains to one of the
	// trust roots given in SignatureValidationOpts at the validation time and
	// may sign documents:
	//   - its key usage permits digital signatures;
	//   - its extended key usage, if any, includes anyExtendedKeyUsage,
	//     emailProtection, codeSigning or a document signing usage (RFC 9336,
	//     Microsoft or Adobe), so a TLS-only certificate is not trusted;
	//   - every intermediate CA that has an extended key usage allows the same.
	// For a document timestamp the signer and every CA of its chain, the root
	// included, must allow time stamping instead. It is only ever set by
	// PdfReader.ValidateSignaturesWithOpts; without roots it stays false and
	// TrustErrors says why.
	IsTrusted bool

	// TrustErrors lists the reasons IsTrusted is false. It is empty when
	// IsTrusted is true.
	TrustErrors []string

	// TrustWarnings lists problems met while establishing trust that did
	// not change the verdict, such as an unreachable revocation endpoint or
	// an unknown revocation status that was not required.
	TrustWarnings []string

	// TrustChain is the verified path from the signer certificate to the
	// trust root, nil when no such path exists. It stays set when a
	// revocation check later cleared IsTrusted, so the revoked chain can be
	// inspected.
	TrustChain []*x509.Certificate

	// ValidatedAt is the instant the certificate chain was evaluated at.
	// Zero when the evaluation stopped before the chain was checked, with
	// the reason in TrustErrors: no roots, failed integrity, no signer
	// certificate, or a key usage that rules the signer out.
	ValidatedAt time.Time

	// RevocationStatus is the outcome of the revocation check selected by
	// SignatureValidationOpts.Revocation.
	RevocationStatus RevocationStatus

	// RevocationTime is when the chain certificate was revoked. Set only
	// when RevocationStatus is RevocationStatusRevoked.
	RevocationTime time.Time
	Fields         []*PdfField
	Name           string
	Date           PdfDate
	Reason         string
	Location       string
	ContactInfo    string
	DiffResults    *mdp.DiffResults

	// IsCrlFound and IsOcspFound report whether a CRL, respectively an OCSP
	// response, covering the signer certificate at the validation time was
	// found and checked. When no revocation check ran, because none was
	// requested or the chain was not trusted, they keep the handler's
	// value: the PAdES handler sets them to whether the signature carries a
	// revocation attribute at all.
	IsCrlFound  bool
	IsOcspFound bool

	// IsCoveringEntireFile is true if the signature's ByteRange, plus the
	// /Contents gap it excludes, spans the whole underlying file — no bytes
	// were appended after this signature was applied.
	IsCoveringEntireFile bool

	// IsCoveringEntireRevision is true if the signature covers everything up
	// to the end of the document revision it was applied in, even when later
	// incremental-update revisions were appended afterward (the common, valid
	// incremental-signing case). IsCoveringEntireFile implies it.
	IsCoveringEntireRevision bool

	// RevisionsAfterSignature is the number of incremental-update revisions
	// appended to the file after the revision covered by this signature.
	// Meaningful only when IsCoveringEntireRevision is true: zero then means
	// the signature covers the latest revision, and the field stays zero when
	// no revision is covered at all.
	RevisionsAfterSignature int

	// ContentsGapOnly is true if the only bytes excluded from the signed
	// range are exactly the signature's own /Contents hex string, including
	// its <> delimiters. False indicates the ByteRange excludes something
	// else too — a red flag that unsigned content may be hiding inside the
	// nominally signed span rather than only appended at the end of the
	// file. It is also false for unusual but benign layouts (a ByteRange not
	// starting at offset 0, or a non-hexadecimal /Contents).
	ContentsGapOnly  bool
	byteRangeChecked bool

	// Certificates is the certificate chain embedded in the signature, with
	// the signer's certificate first. Handlers set it to the chain they
	// verified the signature with; otherwise it is filled best-effort from
	// PdfSignature.GetCerts and stays nil when no chain could be extracted.
	// Only a handler-reported chain is used for trust (see TrustChain).
	Certificates []*x509.Certificate

	// GeneralizedTime is the time at which the time-stamp token has been created by the TSA (RFC 3161).
	GeneralizedTime time.Time
}

// ViewArea returns the value of the viewArea.
func (vp *ViewerPreferences) ViewArea() PageBoundary { return vp.viewArea }

// AddExtGState adds a graphics state to the XObject resources.
func (p *PdfPage) AddExtGState(name core.PdfObjectName, egs *core.PdfObjectDictionary) error {
	if p.Resources == nil {
		p.Resources = NewPdfPageResources()
	}
	if p.Resources.ExtGState == nil {
		p.Resources.ExtGState = core.MakeDict()
	}
	dict, ok := core.TraceToDirectObject(p.Resources.ExtGState).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("Expected ExtGState dictionary is not a dictionary: %v", core.TraceToDirectObject(p.Resources.ExtGState))
		return errors.New("type check error")
	}
	dict.Set(name, egs)
	return nil
}

func (r *PdfReader) newPdfAnnotationPrinterMarkFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationPrinterMark, error) {
	printerMark := PdfAnnotationPrinterMark{}
	printerMark.MN = dict.Get("MN")
	return &printerMark, nil
}

func buildFirstXrefStream(writer *PdfWriter, firstXrefObjNum int64, nums []int64, preObjByNum map[int64]preserializedObject, n int64, hintOffset int64, linDictOffset int64, firstXrefOffset int64, mainXrefOffset int64, count int64, objStm *objectStreamsState) ([]byte, error) {
	entries := make([]xrefEntry, 0, len(nums))
	for _, num := range nums {
		var count int64
		switch num {
		case linearizationDictObjNum2:
			count = linDictOffset
		case n:
			count = hintOffset
		case objStm.firstXrefObjNum:
			count = firstXrefOffset
		default:
			preObj, ok := preObjByNum[num]
			if !ok {
				return nil, fmt.Errorf("first xref stream: missing offset for obj %d", num)
			}
			count = preObj.offset
		}
		if count > math.MaxUint32 {
			return nil, fmt.Errorf("xref stream: object %d offset %d exceeds 4 GB (xref-stream offset field is 4 bytes)", num, count)
		}
		entries = append(entries, xrefEntry{objNum: num, entryType: 1, offset: uint32(count), genOrIndex: 0})
	}
	return buildXrefStream(&xrefStreamPlan{objNum: firstXrefObjNum, entries: entries, size: count + 1, prev: mainXrefOffset, hasPrev: true, root: writer.root, info: writer.infoObj, encrypt: encryptedWriterCatalog(writer), id: writer.ids})
}

// GetImageData returns the raw image data that can be used to create an image file.
// For SVG glyphs, returns the SVG source. For bitmap glyphs, returns the raw image data.
// Returns the data, MIME type, and file extension.
func (g *ColorGlyphInfo) GetImageData() (data []byte, s string, s2 string) {
	switch g.GlyphType {
	case ColorGlyphTypeSVG:
		if len(g.SVGData) > 0 {
			return g.SVGData, "image/svg+xml", ".svg"
		}
	case ColorGlyphTypeBitmap:
		if len(g.BitmapData) > 0 {
			switch g.BitmapFormat {
			case typefont.PNG:
				return g.BitmapData, "image/png", ".png"
			case typefont.JPG:
				return g.BitmapData, "image/jpeg", ".jpg"
			case typefont.TIFF:
				return g.BitmapData, "image/tiff", ".tiff"
			case typefont.BlackAndWhite:
				return g.BitmapData, "image/png", ".png"
			}
		}
	}
	return nil, "", ""
}

// ColorFromPdfObjects returns a new PdfColor based on the input slice of color
// components. The slice should contain a single PdfObjectFloat element.
func (cs *PdfColorspaceSpecialIndexed) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) != 1 {
		return nil, errors.New("range check")
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objects)
	if err != nil {
		return nil, err
	}
	return cs.ColorFromFloats(numbersAsFloat)
}

func (w *PdfWriter) setDocInfo(obj core.PdfObject) {
	if w.hasObject(w.infoObj) {
		delete(w.hasObjects, w.infoObj)
		delete(w.traversed, w.infoObj)
		for i, object := range w.objects {
			if object == w.infoObj {
				copy(w.objects[i:], w.objects[i+1:])
				w.objects[len(w.objects)-1] = nil
				w.objects = w.objects[:len(w.objects)-1]
				break
			}
		}
	}
	indirect := core.PdfIndirectObject{}
	indirect.PdfObject = obj
	w.infoObj = &indirect
	w.addObject(&indirect)
}

func (h holdReport) precedes(thisUpdate time.Time, crl *parsedCRL) bool {
	if h.crl != nil && crl != nil {
		return crl.newerThan(h.crl)
	}
	return thisUpdate.After(h.thisUpdate)
}

// Components returns the color's component values: A, B, C.
func (c *PdfColorCalRGB) Components() []float64 { return []float64{c[0], c[1], c[2]} }

// FieldImageProvider provides fields images for specified fields.
type FieldImageProvider interface {
	FieldImageValues() (map[string]*Image, error)
}

// ToInteger convert to an integer format.
func (c *PdfColorDeviceGray) ToInteger(bits int) uint32 {
	f := math.Pow(2, float64(bits)) - 1
	return uint32(f * c.Val())
}

// B returns the value of the B component of the color.
func (c *PdfColorLab) B() float64 { return c[2] }

// ToPdfObject returns colorspace in a PDF object format [name stream]
func (cs *PdfColorspaceICCBased) ToPdfObject() core.PdfObject {
	arr := &core.PdfObjectArray{}
	arr.Append(core.MakeName("ICCBased"))
	var stream *core.PdfObjectStream
	if cs.stream != nil {
		stream = cs.stream
	} else {
		stream = &core.PdfObjectStream{}
	}
	dict := core.MakeDict()
	dict.Set("N", core.MakeInteger(int64(cs.N)))
	if cs.Alternate != nil {
		dict.Set("Alternate", cs.Alternate.ToPdfObject())
	}
	if cs.Metadata != nil {
		dict.Set("Metadata", cs.Metadata)
	}
	if cs.Range != nil {
		var objs []core.PdfObject
		for _, f := range cs.Range {
			objs = append(objs, core.MakeFloat(f))
		}
		dict.Set("Range", core.MakeArray(objs...))
	}
	dict.Set("Length", core.MakeInteger(int64(len(cs.Data))))
	stream.Stream = cs.Data
	stream.PdfObjectDictionary = dict
	arr.Append(stream)
	if cs.container != nil {
		cs.container.PdfObject = arr
		return cs.container
	}
	return arr
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

func collectPageResourceObjects(page *PdfPage) map[core.PdfObjectName]core.PdfObject {
	objByName := make(map[core.PdfObjectName]core.PdfObject)
	if page.Resources == nil {
		return objByName
	}
	if page.Resources.Font != nil {
		if fontDict, ok := core.GetDict(page.Resources.Font); ok {
			for _, name := range fontDict.Keys() {
				objByName[name] = fontDict.Get(name)
			}
		}
	}
	if page.Resources.ExtGState != nil {
		if extGStateDict, ok := core.GetDict(page.Resources.ExtGState); ok {
			for _, name := range extGStateDict.Keys() {
				objByName[name] = extGStateDict.Get(name)
			}
		}
	}
	if page.Resources.XObject != nil {
		if xObjectDict, ok := core.GetDict(page.Resources.XObject); ok {
			for _, name := range xObjectDict.Keys() {
				objByName[name] = xObjectDict.Get(name)
			}
		}
	}
	if page.Resources.Pattern != nil {
		if patternDict, ok := core.GetDict(page.Resources.Pattern); ok {
			for _, name := range patternDict.Keys() {
				objByName[name] = patternDict.Get(name)
			}
		}
	}
	if page.Resources.Shading != nil {
		if shadingDict, ok := core.GetDict(page.Resources.Shading); ok {
			for _, name := range shadingDict.Keys() {
				objByName[name] = shadingDict.Get(name)
			}
		}
	}
	if page.Resources.ProcSet != nil {
		if procSetDict, ok := core.GetDict(page.Resources.ProcSet); ok {
			for _, name := range procSetDict.Keys() {
				objByName[name] = procSetDict.Get(name)
			}
		}
	}
	if page.Resources.Properties != nil {
		if propertiesDict, ok := core.GetDict(page.Resources.Properties); ok {
			for _, name := range propertiesDict.Keys() {
				objByName[name] = propertiesDict.Get(name)
			}
		}
	}
	return objByName
}

// GetParamsDict returns *core.PdfObjectDictionary with a set of basic image parameters.
func (img *Image) GetParamsDict() *core.PdfObjectDictionary {
	dict := core.MakeDict()
	dict.Set("Width", core.MakeInteger(img.Width))
	dict.Set("Height", core.MakeInteger(img.Height))
	dict.Set("ColorComponents", core.MakeInteger(int64(img.ColorComponents)))
	dict.Set("BitsPerComponent", core.MakeInteger(img.BitsPerComponent))
	return dict
}

func buildHintStream(layout *linearizationLayout, table map[int64]int64, preObjByNum map[int64]preserializedObject, hintOffset, n int64) ([]byte, int64, error) {
	pageOffsetHintTable, err := buildPageOffsetHintTable(layout, table, preObjByNum, hintOffset, n)
	if err != nil {
		return nil, 0, err
	}
	sharedObjectHintTable, err := buildSharedObjectHintTable(layout, preObjByNum, hintOffset, n)
	if err != nil {
		return nil, 0, err
	}
	return core.EncodeMandatoryHintStream(pageOffsetHintTable, sharedObjectHintTable)
}

func buildXrefTable(writer *PdfWriter, nums []int64, table map[int64]int64, n int64, count int64, ok bool, num int64, found bool) ([]byte, int64) {
	var buf bytes.Buffer
	buf.WriteString("xref\r\n")
	size := int64(-1)
	if found {
		buf.WriteString("0 1\r\n")
		size = int64(buf.Len())
		buf.WriteString("0000000000 65535 f\r\n")
	}
	for i := 0; i < len(nums); {
		n := nums[i]
		length := i + 1
		for length < len(nums) && nums[length] == nums[length-1]+1 {
			length++
		}
		length2 := length - i
		fmt.Fprintf(&buf, "%d %d\r\n", n, length2)
		for j := i; j < length; j++ {
			if size < 0 {
				size = int64(buf.Len())
			}
			fmt.Fprintf(&buf, "%010d 00000 n\r\n", table[nums[j]])
		}
		i = length
	}
	if size < 0 {
		size = 0
	}
	buf.WriteString("trailer\r\n")
	writeTrailerDict(&buf, writer, n+1, count, ok)
	fmt.Fprintf(&buf, "\r\nstartxref\r\n%010d\r\n%%%%EOF\r\n", num)
	return buf.Bytes(), size
}

// PdfActionNamed represents a named action.
type PdfActionNamed struct {
	*PdfAction
	N core.PdfObject
}

func (w *PdfWriter) flushWriter() error {
	if w.writeErr == nil {
		w.writeErr = w.bufWriter.Flush()
	}
	return w.writeErr
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

// ToGoTime returns the date in time.Time format.
func (d PdfDate) ToGoTime() time.Time {
	n := int(d.utOffsetHours*60*60 + d.utOffsetMinutes*60)
	switch d.utOffsetSign {
	case '-':
		n = -n
	case 'Z':
		n = 0
	}
	s := fmt.Sprintf("UTC%c%.2d%.2d", d.utOffsetSign, d.utOffsetHours, d.utOffsetMinutes)
	fixedZone := time.FixedZone(s, n)
	return time.Date(int(d.year), time.Month(d.month), int(d.day), int(d.hour), int(d.minute), int(d.second), 0, fixedZone)
}

func (w *PdfWriter) copyObject(obj core.PdfObject, objByObj map[core.PdfObject]core.PdfObject, set map[core.PdfObject]struct{}, ok bool) core.PdfObject {
	found := !w.appendMode && set != nil
	if obj2, isObjByObj := objByObj[obj]; isObjByObj {
		if found && !ok {
			delete(set, obj)
		}
		return obj2
	}
	if obj == nil {
		null := core.MakeNull()
		return null
	}
	obj2 := obj
	switch v := obj.(type) {
	case *core.PdfObjectArray:
		array := core.MakeArray()
		obj2 = array
		objByObj[obj] = obj2
		for _, obj := range v.Elements() {
			array.Append(w.copyObject(obj, objByObj, set, ok))
		}
	case *core.PdfObjectStreams:
		objStreams := &core.PdfObjectStreams{PdfObjectReference: v.PdfObjectReference}
		obj2 = objStreams
		objByObj[obj] = obj2
		for _, obj := range v.Elements() {
			objStreams.Append(w.copyObject(obj, objByObj, set, ok))
		}
	case *core.PdfObjectStream:
		stream := &core.PdfObjectStream{Stream: v.Stream, PdfObjectReference: v.PdfObjectReference, Lazy: v.Lazy, TempFile: v.TempFile}
		if v.Lazy {
			if w.lazySources == nil {
				w.lazySources = map[*core.PdfObjectStream]*core.PdfObjectStream{}
			}
			w.lazySources[stream] = v
		}
		obj2 = stream
		objByObj[obj] = obj2
		stream.PdfObjectDictionary = w.copyObject(v.PdfObjectDictionary, objByObj, set, ok).(*core.PdfObjectDictionary)
	case *core.PdfObjectDictionary:
		var done bool
		if found && !ok {
			if s, _ := core.GetNameVal(v.Get("Type")); s == "Page" {
				_, valid := w.pageDicts[v]
				ok = !valid
				done = ok
			}
		}
		dict := core.MakeDict()
		obj2 = dict
		objByObj[obj] = obj2
		for _, name := range v.Keys() {
			dict.Set(name, w.copyObject(v.Get(name), objByObj, set, ok))
		}
		if done {
			obj2 = core.MakeNull()
			ok = false
		}
	case *core.PdfIndirectObject:
		indirect := &core.PdfIndirectObject{PdfObjectReference: v.PdfObjectReference}
		obj2 = indirect
		objByObj[obj] = obj2
		indirect.PdfObject = w.copyObject(v.PdfObject, objByObj, set, ok)
	case *core.PdfObjectString:
		if w.crypter != nil {
			str := *v
			obj2 = &str
			objByObj[obj] = obj2
		} else {
			obj2 = obj
		}
	case *core.PdfObjectName, *core.PdfObjectNull, *core.PdfObjectInteger, *core.PdfObjectFloat, *core.PdfObjectBool:
		obj2 = obj
	case *core.PdfObjectReference:
		ref := *v
		obj2 = &ref
		objByObj[obj] = obj2
	case *pdfSignDictionary:
		signDict := &pdfSignDictionary{PdfObjectDictionary: core.MakeDict(), handler: v.handler, signature: v.signature}
		obj2 = signDict
		objByObj[obj] = obj2
		for _, name := range v.Keys() {
			signDict.Set(name, w.copyObject(v.Get(name), objByObj, set, ok))
		}
	default:
		common.Log.Info("TODO(a5i): implement copyObject for %+v", obj)
	}
	if found && ok {
		set[obj] = struct{}{}
	}
	return obj2
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

// PdfAnnotationMovie represents Movie annotations.
// (Section 12.5.6.17).
type PdfAnnotationMovie struct {
	*PdfAnnotation
	T     core.PdfObject
	Movie core.PdfObject
	A     core.PdfObject
}

// GetColorGlyphInfoByRune retrieves color glyph information for a rune from an OTF font
func GetColorGlyphInfoByRune(otf *fonts.OtfType, r rune) *ColorGlyphInfo {
	if otf.Face == nil {
		return nil
	}
	if char, ok := otf.Chars[r]; ok {
		if colorGlyphInfo, ok := GetColorGlyphInfo(otf, typefont.GID(char)); ok {
			return colorGlyphInfo
		} else {
			common.Log.Debug("Glyph %d for rune %c has no color information", char, r)
		}
	} else {
		common.Log.Debug("No glyph found for rune %c (U+%04X)", r, r)
	}
	return nil
}

// NewOutlineBookmark returns an initialized PdfOutlineItem for a given bookmark title and page.
func NewOutlineBookmark(title string, page *core.PdfIndirectObject) *PdfOutlineItem {
	item := PdfOutlineItem{primitive: core.MakeIndirectObject(core.MakeDict())}
	item.context = &item
	item.Title = core.MakeString(title)
	array := core.MakeArray()
	array.Append(page)
	array.Append(core.MakeName("Fit"))
	item.Dest = array
	return &item
}

// Size returns the width and the height of the page. The method reports
// the page dimensions as displayed by a PDF viewer (i.e. page rotation is
// taken into account).
func (p *PdfPage) Size() (float64, float64, error) {
	mediaBox, err := p.GetMediaBox()
	if err != nil {
		return 0, 0, err
	}
	width, height := mediaBox.Width(), mediaBox.Height()
	rotate, err := p.GetRotate()
	if err != nil {
		common.Log.Debug("ERROR: %s - ignoring and assuming no rotation\n", err.Error())
	}
	if n := rotate; n%360 != 0 && n%90 == 0 {
		if count := (360 + n%360) % 360; count == 90 || count == 270 {
			width, height = height, width
		}
	}
	return width, height, nil
}

// GetNameDictionary returns the Names entry in the PDF catalog.
// See section 7.7.4 "Name Dictionary" (p. 80 PDF32000_2008).
func (r *PdfReader) GetNameDictionary() (core.PdfObject, error) {
	names := core.ResolveReference(r.catalog.Get("Names"))
	if names == nil {
		return nil, nil
	}
	if !r.lazy {
		err := r.traverseObjectData(names)
		if err != nil {
			return nil, err
		}
	}
	return names, nil
}

func canStoreInObjectStream(obj core.PdfObject, writer *PdfWriter) bool {
	indirect, ok := obj.(*core.PdfIndirectObject)
	if !ok {
		return false
	}
	if obj == writer.encryptObj {
		return false
	}
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return false
	}
	if _, ok := indirect.PdfObject.(*pdfSignDictionary); ok {
		return false
	}
	if name, _ := dict.Get("Type").(*core.PdfObjectName); name != nil {
		switch string(*name) {
		case "Page", "Pages", "Catalog", "XRef", "ObjStm":
			return false
		}
	}
	return true
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionResetForm) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeResetForm)))
	dict.SetIfNotNil("Fields", a.Fields)
	dict.SetIfNotNil("Flags", a.Flags)
	return indirect
}

// Has checks if flag fl is set in flag and returns true if so, false otherwise.
func (f FieldFlag) Has(fl FieldFlag) bool { return (f.Mask() & fl.Mask()) > 0 }

// GetNumComponents returns the number of color components of the colorspace device.
// Returns 1 for a grayscale device.
func (cs *PdfColorspaceDeviceGray) GetNumComponents() int { return 1 }

func (r *PdfReader) newPdfActionResetFormFromDict(dict *core.PdfObjectDictionary) (*PdfActionResetForm, error) {
	return &PdfActionResetForm{Fields: dict.Get("Fields"), Flags: dict.Get("Flags")}, nil
}

// multiFontEncoder implements a an Encoder that holds a list of fonts provided.
type MultipleFontEncoder struct {
	fontList    []*PdfFont
	CurrentFont *PdfFont
}

func (w *PdfWriter) AttachFile(file *EmbeddedFile) error {
	names := w.names
	if names == nil {
		names = newNames()
	}
	err := names.addEmbeddedFile(file)
	if err != nil {
		return err
	}
	w.names = names
	return nil
}

// PdfAppender appends new PDF content to an existing PDF document via incremental updates.
type PdfAppender struct {
	readSeeker     io.ReadSeeker
	parser         *core.PdfParser
	roReader       *PdfReader
	Reader         *PdfReader
	pages          []*PdfPage
	acroForm       *PdfAcroForm
	dss            *DSS
	perms          *Permissions
	xrefs          core.XrefTable
	xrefOffset     int64
	greatestObjNum int
	newObjects     []core.PdfObject
	hasNewObject   map[core.PdfObject]struct{}
	objNumMap      map[core.PdfObject]int64
	skipObjects    map[core.PdfObject]struct{}
	traversed      map[core.PdfObject]struct{}
	originalSize   int64
	written        bool
	password       string
	encryptOptions *EncryptOptions
	docInfo        *PdfInfo
}

// SetPrintScaling sets the value of the printScaling.
func (vp *ViewerPreferences) SetPrintScaling(printScaling PrintScaling) {
	vp.printScaling = printScaling
}
func (f *pdfCIDFontType0) getFontDescriptor() *PdfFontDescriptor { return f.fontDescriptor }
func (f *pdfFontSimple) updateStandard14Font() {
	enc, ok := f.Encoder().(textencoding.SimpleEncoder)
	if !ok {
		common.Log.Error("Wrong encoder type: %T. font=%s.", f.Encoder(), f)
		return
	}
	charcodes := enc.Charcodes()
	f.charWidths = make(map[textencoding.CharCode]float64, len(charcodes))
	for _, charcode := range charcodes {
		charcodeToRune, _ := enc.CharcodeToRune(charcode)
		metrics, _ := f.fontMetrics.Read(charcodeToRune)
		f.charWidths[charcode] = metrics.Wx
	}
}

// AddWatermarkText adds a text watermark to the page.
func (p *PdfPage) AddWatermarkText(text string, opt WatermarkTextOptions) error {
	if text == "" {
		return errors.New("no text set")
	}
	if opt.FontPath == "" {
		return errors.New("no font path set")
	}
	if opt.Alpha == 0 {
		opt.Alpha = 0.5
	}
	if opt.FontSize == 0 {
		opt.FontSize = 20.0
	}
	if opt.FontColor == nil {
		opt.FontColor = color.Black
	}
	generateImage, err := p.generateImage(text, opt.FontSize, opt.FontPath, opt.FontColor)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(bytes.NewReader(generateImage))
	if err != nil {
		return err
	}
	img2, err := ImageHandling.NewImageFromGoImage(img)
	if err != nil {
		return err
	}
	xObjectImage, err := NewXObjectImageFromImage(img2, nil, core.NewFlateEncoder())
	if err != nil {
		return err
	}
	watermarkImageOptions := WatermarkImageOptions{Alpha: opt.Alpha, FitToWidth: false, PreserveAspectRatio: true, Angle: opt.Angle}
	return p.AddWatermarkImage(xObjectImage, watermarkImageOptions)
}

// ToPdfObject converts the namespace to a PDF object. The returned object is
// stable: repeated calls return the same indirect object, so multiple
// structure elements may reference one namespace.
func (ns *PdfStructureNamespace) ToPdfObject() core.PdfObject {
	if ns.indirect == nil {
		ns.indirect = core.MakeIndirectObject(core.MakeDict())
	}
	dict := ns.indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.Set("Type", core.MakeName("Namespace"))
	dict.Set("NS", core.MakeString(ns.uri))
	if ns.schema != nil {
		dict.Set("Schema", ns.schema)
	} else {
		dict.Remove("Schema")
	}
	if ns.roleMapNS != nil {
		if ns.roleMapIndirect == nil {
			ns.roleMapIndirect = core.MakeIndirectObject(ns.roleMapNS)
		} else {
			ns.roleMapIndirect.PdfObject = ns.roleMapNS
		}
		dict.Set("RoleMapNS", ns.roleMapIndirect)
	} else {
		dict.Remove("RoleMapNS")
	}
	return ns.indirect
}

func shapedGlyphIDs(encoder textencoding.TextEncoder) []uint16 {
	shapedGlyphRegistry, ok := encoder.(textencoding.ShapedGlyphRegistry)
	if !ok {
		return nil
	}
	registeredShapedGIDs := shapedGlyphRegistry.RegisteredShapedGIDs()
	vals := make([]uint16, len(registeredShapedGIDs))
	for i, registeredShapedGID := range registeredShapedGIDs {
		vals[i] = uint16(registeredShapedGID)
	}
	return vals
}

func newPdfPageResourcesColorspacesFromPdfObject(colorSpace core.PdfObject) (*PdfPageResourcesColorspaces, error) {
	pageResourcesColorspaces := &PdfPageResourcesColorspaces{}
	if indirect, ok := colorSpace.(*core.PdfIndirectObject); ok {
		pageResourcesColorspaces.container = indirect
		colorSpace = indirect.PdfObject
	}
	colorSpaceDict, ok := core.GetDict(colorSpace)
	if !ok {
		return nil, errors.New("CS attribute type error")
	}
	pageResourcesColorspaces.Names = []string{}
	pageResourcesColorspaces.Colorspaces = map[string]PdfColorspace{}
	for _, name := range colorSpaceDict.Keys() {
		obj := colorSpaceDict.Get(name)
		pageResourcesColorspaces.Names = append(pageResourcesColorspaces.Names, string(name))
		cs, err := NewPdfColorspaceFromPdfObject(obj)
		if err != nil {
			return nil, err
		}
		pageResourcesColorspaces.Colorspaces[string(name)] = cs
	}
	return pageResourcesColorspaces, nil
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

// ToPdfObject converts PdfAcroForm to a PdfObject, i.e. an indirect object containing the
// AcroForm dictionary.
func (a *PdfAcroForm) ToPdfObject() core.PdfObject {
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	if a.Fields != nil {
		arr := core.PdfObjectArray{}
		for _, field := range *a.Fields {
			model := field.GetContext()
			if model != nil {
				arr.Append(model.ToPdfObject())
			} else {
				arr.Append(field.ToPdfObject())
			}
		}
		dict.Set("Fields", &arr)
	}
	if a.NeedAppearances != nil {
		dict.Set("NeedAppearances", a.NeedAppearances)
	} else {
		if needAppearances := dict.Get("NeedAppearances"); needAppearances != nil {
			dict.Remove("NeedAppearances")
		}
	}
	if a.SigFlags != nil {
		dict.Set("SigFlags", a.SigFlags)
	}
	if a.CO != nil {
		dict.Set("CO", a.CO)
	}
	if a.DR != nil {
		dict.Set("DR", a.DR.ToPdfObject())
	}
	if a.DA != nil {
		dict.Set("DA", a.DA)
	}
	if a.Q != nil {
		dict.Set("Q", a.Q)
	}
	if a.XFA != nil {
		dict.Set("XFA", a.XFA)
	}
	if a.ADBEEchoSign != nil {
		dict.Set("ADBE_EchoSign", a.ADBEEchoSign)
	}
	return indirect
}

// Artifact represents a PDF artifact, which is a non-visual element
// that provides additional information about the document structure or content.
type Artifact struct {
	// Type of the artifact.
	Type ArtifactType

	// BBox of the artifact.
	//
	// Optional; required for background artifacts.
	//
	// An array of four numbers in default user space units giving the coordinates
	// of the left, bottom, right, and top edges, respectively, of the artifact’s
	// bounding box (the rectangle that completely encloses its visible extent).
	BBox *PdfRectangle

	// Attached positions for the artifact.
	//
	// Optional; pagination and full-page background artifacts only)
	//
	// An array of name objects containing one to four of the names Top, Bottom, Left, and
	// Right, specifying the edges of the page, if any, to which the artifact is
	// logically attached. Page edges shall be defined by the page’s crop box.
	// The ordering of names within the array is immaterial. Including both Left and Right or both Top and Bottom
	// indicates a full-width or full-height artifact, respectively.
	//
	// Use of this entry for background artifacts shall be limited to full-page artifacts.
	// Background artifacts that are not full-page take their dimensions from their parent structural element.
	AttachPositions []ArtifactAttachPosition

	// Subtype of the artifact, if applicable.
	//
	// The subtype of the artifact. This entry should appear
	// only when the Type entry has a value of Pagination.
	//
	// Several standard values are defined in `model.ArtifactSubtype` type.
	//
	// Additional values may be specified for this entry.
	Subtype string
}

// PdfAnnotationRedact represents Redact annotations.
// (Section 12.5.6.23).
type PdfAnnotationRedact struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	QuadPoints  core.PdfObject
	IC          core.PdfObject
	RO          core.PdfObject
	OverlayText core.PdfObject
	Repeat      core.PdfObject
	DA          core.PdfObject
	Q           core.PdfObject
}

// GetCharMetrics returns the char metrics for character code `code`.
func (f pdfCIDFontType0) GetCharMetrics(code textencoding.CharCode) (fonts.CharMetrics, bool) {
	defaultWidth := f.defaultWidth
	if charWidth, ok := f.charWidths[code]; ok {
		defaultWidth = charWidth
	}
	return fonts.CharMetrics{Wx: defaultWidth}, true
}

func NewViewerPreferencesFromPdfObject(obj core.PdfObject) (*ViewerPreferences, error) {
	obj2 := core.ResolveReference(obj)
	dict, ok := core.GetDict(obj2)
	if !ok {
		return nil, fmt.Errorf("existing viewer preferences is not a dictionary")
	}
	viewerPreferences := NewViewerPreferences()
	if hideToolbar := dict.Get("HideToolbar"); hideToolbar != nil {
		if hideToolbarBool, ok := core.GetBool(hideToolbar); ok {
			viewerPreferences.hideToolbar = (*bool)(hideToolbarBool)
		}
	}
	if hideMenubar := dict.Get("HideMenubar"); hideMenubar != nil {
		if hideMenubarBool, ok := core.GetBool(hideMenubar); ok {
			viewerPreferences.hideMenubar = (*bool)(hideMenubarBool)
		}
	}
	if hideWindowUI := dict.Get("HideWindowUI"); hideWindowUI != nil {
		if hideWindowUIBool, ok := core.GetBool(hideWindowUI); ok {
			viewerPreferences.hideWindowUI = (*bool)(hideWindowUIBool)
		}
	}
	if fitWindow := dict.Get("FitWindow"); fitWindow != nil {
		if fitWindowBool, ok := core.GetBool(fitWindow); ok {
			viewerPreferences.fitWindow = (*bool)(fitWindowBool)
		}
	}
	if centerWindow := dict.Get("CenterWindow"); centerWindow != nil {
		if centerWindowBool, ok := core.GetBool(centerWindow); ok {
			viewerPreferences.centerWindow = (*bool)(centerWindowBool)
		}
	}
	if displayDocTitle := dict.Get("DisplayDocTitle"); displayDocTitle != nil {
		if displayDocTitleBool, ok := core.GetBool(displayDocTitle); ok {
			viewerPreferences.displayDocTitle = (*bool)(displayDocTitleBool)
		}
	}
	if nonFullScreenPageMode := dict.Get("NonFullScreenPageMode"); nonFullScreenPageMode != nil {
		if nonFullScreenPageModeName, ok := core.GetName(nonFullScreenPageMode); ok {
			viewerPreferences.nonFullScreenPageMode = NonFullScreenPageMode(*nonFullScreenPageModeName)
		}
	}
	if direction := dict.Get("Direction"); direction != nil {
		if directionName, ok := core.GetName(direction); ok {
			viewerPreferences.direction = Direction(*directionName)
		}
	}
	if viewArea := dict.Get("ViewArea"); viewArea != nil {
		if viewAreaName, ok := core.GetName(viewArea); ok {
			viewerPreferences.viewArea = PageBoundary(*viewAreaName)
		}
	}
	if viewClip := dict.Get("ViewClip"); viewClip != nil {
		if viewClipName, ok := core.GetName(viewClip); ok {
			viewerPreferences.viewClip = PageBoundary(*viewClipName)
		}
	}
	if printArea := dict.Get("PrintArea"); printArea != nil {
		if printAreaName, ok := core.GetName(printArea); ok {
			viewerPreferences.printArea = PageBoundary(*printAreaName)
		}
	}
	if printClip := dict.Get("PrintClip"); printClip != nil {
		if printClipName, ok := core.GetName(printClip); ok {
			viewerPreferences.printClip = PageBoundary(*printClipName)
		}
	}
	if printScaling := dict.Get("PrintScaling"); printScaling != nil {
		if printScalingName, ok := core.GetName(printScaling); ok {
			viewerPreferences.printScaling = PrintScaling(*printScalingName)
		}
	}
	if duplex := dict.Get("Duplex"); duplex != nil {
		if duplexName, ok := core.GetName(duplex); ok {
			viewerPreferences.duplex = Duplex(*duplexName)
		}
	}
	if pickTrayByPDFSize := dict.Get("PickTrayByPDFSize"); pickTrayByPDFSize != nil {
		if pickTrayByPDFSizeBool, ok := core.GetBool(pickTrayByPDFSize); ok {
			viewerPreferences.pickTrayByPDFSize = (*bool)(pickTrayByPDFSizeBool)
		}
	}
	if printPageRange := dict.Get("PrintPageRange"); printPageRange != nil {
		if printPageRangeArr, ok := core.GetArray(printPageRange); ok {
			viewerPreferences.printPageRange = make([]int, printPageRangeArr.Len())
			for i := range viewerPreferences.printPageRange {
				if obj := printPageRangeArr.Get(i); obj != nil {
					if num, ok := core.GetInt(obj); ok {
						viewerPreferences.printPageRange[i] = int(*num)
					}
				}
			}
		}
	}
	if numCopies := dict.Get("NumCopies"); numCopies != nil {
		if numCopiesInt, ok := core.GetInt(numCopies); ok {
			viewerPreferences.numCopies = int(*numCopiesInt)
		}
	}
	return viewerPreferences, nil
}

func (c *parsedCRL) signedBy(cert *x509.Certificate, data [32]byte) bool {
	ok, found := c.memo[data]
	if !found {
		ok = c.CheckSignatureFrom(cert) == nil
		if c.memo == nil {
			c.memo = map[[32]byte]bool{}
		}
		c.memo[data] = ok
	}
	return ok
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationPolygon) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Polygon"))
	dict.SetIfNotNil("Vertices", a.Vertices)
	dict.SetIfNotNil("LE", a.LE)
	dict.SetIfNotNil("BS", a.BS)
	dict.SetIfNotNil("IC", a.IC)
	dict.SetIfNotNil("BE", a.BE)
	dict.SetIfNotNil("IT", a.IT)
	dict.SetIfNotNil("Measure", a.Measure)
	return indirect
}

// NewCompositePdfFont loads a font (TTF or OTF) from a reader and returns a composite PdfFont.
// This is the recommended way to load fonts for full Unicode support, text shaping,
// and color glyph (emoji) capabilities.
func NewCompositePdfFont(r io.ReadSeeker) (*PdfFont, error) {
	all, err := io.ReadAll(r)
	if err != nil {
		common.Log.Debug("ERROR: Unable to read font contents: %v", err)
		return nil, err
	}
	otfParse, err := fonts.OtfParse(bytes.NewReader(all))
	if err != nil {
		return nil, err
	}
	if otfParse.OutlineType == fonts.OTFOutlineTypeTrueType {
		cidFontType2, err := newPdfCIDFontType2FromTtf(*otfParse.TtfType, all, &otfParse)
		if err != nil {
			return nil, err
		}
		return &cidFontType2, nil
	}
	font := &pdfCIDFontType0{fontCommon: fontCommon{subtype: "CIDFontType0"}, charWidths: make(map[textencoding.CharCode]float64), CIDToGIDMap: core.MakeName("Identity")}
	font.SetOtfType(&otfParse)
	font.baseFont = otfParse.PostScriptName
	if font.baseFont == "" {
		font.baseFont = "UnknownOTFFont"
	}
	f := 1000.0 / float64(otfParse.UnitsPerEm)
	v := 1000.0
	if otfParse.UnitsPerEm == 0 {
		f = 1.0
	}
	if len(otfParse.Widths) > 0 {
		v = f * float64(otfParse.Widths[0])
	}
	table, numByV := scaleGlyphWidths(otfParse.Chars, otfParse.Widths, otfParse.UnitsPerEm)
	font.runeToGID = table
	font.DW = core.MakeInteger(int64(v))
	v2 := fonts.GID(len(otfParse.Widths))
	widthsArray := buildWidthsArray(numByV, uint16(v2))
	font.W = core.MakeIndirectObject(widthsArray)
	n := fontFlagNonsymbolic
	if otfParse.OutlineType != fonts.OTFOutlineTypePostScript {
		n = fontFlagSymbolic
	}
	fontDescriptor := newPdfFontDescriptorFromTtf(otfParse.TtfType, v, n)
	fontDescriptor.FontName = core.MakeName(font.baseFont)
	if otfParse.Bold {
		if flagsInt, ok := core.GetInt(fontDescriptor.Flags); ok {
			fontDescriptor.Flags = core.MakeInteger(int64(*flagsInt) | int64(fontFlagForceBold))
		}
	}
	data, err := cffTable(otfParse.Loader)
	if err == nil {
		stream, err := core.MakeStream(data, core.NewFlateEncoder())
		if err != nil {
			common.Log.Debug("ERROR: Unable to make stream: %v", err)
			return nil, err
		}
		stream.PdfObjectDictionary.Set("Subtype", core.MakeName("CIDFontType0C"))
		fontDescriptor.FontFile3 = stream
	} else {
		stream, err := core.MakeStream(all, core.NewFlateEncoder())
		if err != nil {
			common.Log.Debug("ERROR: Unable to make stream: %v", err)
			return nil, err
		}
		stream.PdfObjectDictionary.Set("Subtype", core.MakeName("OpenType"))
		fontDescriptor.FontFile3 = stream
	}
	dict := core.MakeDict()
	dict.Set("Registry", core.MakeString("Adobe"))
	dict.Set("Ordering", core.MakeString("Identity"))
	dict.Set("Supplement", core.MakeInteger(0))
	font.CIDSystemInfo = dict
	if len(otfParse.Widths) > 0 && len(otfParse.Chars) > 0 {
		for _, char := range otfParse.Chars {
			if int(char) < len(otfParse.Widths) {
				v := f * float64(otfParse.Widths[char])
				font.charWidths[textencoding.CharCode(char)] = v
			}
		}
	}
	font.fontDescriptor = fontDescriptor
	font2 := &pdfFontType0{fontCommon: fontCommon{subtype: "Type0", baseFont: otfParse.PostScriptName}, Encoding: core.MakeName("Identity-H"), encoder: otfParse.NewEncoder()}
	font2.baseFont = font.baseFont + "-Identity-H"
	if len(otfParse.Chars) > 0 {
		font2.toUnicodeCMap = cmapFromGlyphMap(otfParse.Chars)
	}
	font3 := &PdfFont{context: font}
	font2.DescendantFont = font3
	font4 := &PdfFont{context: font2}
	return font4, nil
}

// ToPdfObject converts the pdfCIDFontType0 to a PDF representation.
func (f *pdfCIDFontType0) ToPdfObject() core.PdfObject {
	if f.primitive == nil {
		f.primitive = &core.PdfIndirectObject{}
	}
	dict := f.baseFields().asPdfObjectDictionary("CIDFontType0")
	f.primitive.PdfObject = dict
	if f.CIDSystemInfo != nil {
		dict.Set("CIDSystemInfo", f.CIDSystemInfo)
	}
	if f.DW != nil {
		dict.Set("DW", f.DW)
	}
	if f.DW2 != nil {
		dict.Set("DW2", f.DW2)
	}
	if f.W != nil {
		dict.Set("W", f.W)
	}
	if f.W2 != nil {
		dict.Set("W2", f.W2)
	}
	if f.fontDescriptor != nil {
		dict.Set("FontDescriptor", f.fontDescriptor.ToPdfObject())
	}
	if f.CIDToGIDMap != nil {
		dict.Set("CIDToGIDMap", f.CIDToGIDMap)
	}
	return f.primitive
}

var _ pdfFont = (*pdfCIDFontType0)(nil)

// GetXObjectImageByName returns the XObjectImage with the specified name from the
// page resources, if it exists.
func (r *PdfPageResources) GetXObjectImageByName(keyName core.PdfObjectName) (*XObjectImage, error) {
	xObjectByName, xObjectType := r.GetXObjectByName(keyName)
	if xObjectByName == nil {
		return nil, nil
	}
	if xObjectType != XObjectTypeImage {
		return nil, errors.New("not an image")
	}
	xObjectImage, err := NewXObjectImageFromStream(xObjectByName)
	if err != nil {
		return nil, err
	}
	return xObjectImage, nil
}

func (w *PdfWriter) snapshotObjectNumbers() func() {
	type savedNum struct {
		obj    core.PdfObject
		objNum int64
		genNum int64
	}
	savedNums := make([]savedNum, 0, len(w.objects))
	for _, object := range w.objects {
		switch v := object.(type) {
		case *core.PdfIndirectObject:
			savedNums = append(savedNums, savedNum{v, v.ObjectNumber, v.GenerationNumber})
		case *core.PdfObjectStream:
			savedNums = append(savedNums, savedNum{v, v.ObjectNumber, v.GenerationNumber})
		case *core.PdfObjectStreams:
			savedNums = append(savedNums, savedNum{v, v.ObjectNumber, v.GenerationNumber})
		case *core.PdfObjectReference:
			savedNums = append(savedNums, savedNum{v, v.ObjectNumber, v.GenerationNumber})
		}
	}
	return func() {
		for _, savedNum := range savedNums {
			switch v := savedNum.obj.(type) {
			case *core.PdfIndirectObject:
				v.ObjectNumber, v.GenerationNumber = savedNum.objNum, savedNum.genNum
			case *core.PdfObjectStream:
				v.ObjectNumber, v.GenerationNumber = savedNum.objNum, savedNum.genNum
			case *core.PdfObjectStreams:
				v.ObjectNumber, v.GenerationNumber = savedNum.objNum, savedNum.genNum
			case *core.PdfObjectReference:
				v.ObjectNumber, v.GenerationNumber = savedNum.objNum, savedNum.genNum
			}
		}
	}
}

const trailerOverheadBytes = 30

// SetBoundingBox sets the bounding box in the attribute object.
func (k *KDict) SetBoundingBox(x, y, width, height float64) {
	k.boundingBox = &PdfRectangle{Llx: x, Lly: y, Urx: x + width, Ury: y + height}
}

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

func (f *pdfFontType0) applyLigatureToUnicode(textByCode, code map[cmap.CharCode]string) {
	if len(textByCode) == 0 && len(code) == 0 {
		return
	}
	if f.toUnicodeCMap == nil {
		f.toUnicodeCMap = cmap.NewToUnicodeCMap(map[cmap.CharCode]string{})
	}
	f.toUnicodeCMap.MergeCodeToUnicode(code, false)
	f.toUnicodeCMap.MergeCodeToUnicode(textByCode, true)
	if identityEncoder, ok := f.encoder.(*textencoding.IdentityEncoder); ok {
		identityEncoder.SetToUnicodeCMap(f.toUnicodeCMap)
	}
}

func newPdfShadingType5FromDict(dict *core.PdfObjectDictionary) (*PdfShadingType5, error) {
	shading5 := PdfShadingType5{}
	bitsPerCoordinate := dict.Get("BitsPerCoordinate")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerCoordinate")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok := bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerCoordinate not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading5.BitsPerCoordinate = num
	bitsPerCoordinate = dict.Get("BitsPerComponent")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerComponent")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok = bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerComponent not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading5.BitsPerComponent = num
	bitsPerCoordinate = dict.Get("VerticesPerRow")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: VerticesPerRow")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok = bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("VerticesPerRow not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading5.VerticesPerRow = num
	bitsPerCoordinate = dict.Get("Decode")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: Decode")
		return nil, ErrRequiredAttributeMissing
	}
	arr, ok := bitsPerCoordinate.(*core.PdfObjectArray)
	if !ok {
		common.Log.Debug("Decode not an array (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading5.Decode = arr
	if function := dict.Get("Function"); function != nil {
		shading5.Function = []PdfFunction{}
		if arr, ok := function.(*core.PdfObjectArray); ok {
			for _, obj := range arr.Elements() {
				function, err := newPdfFunctionFromPdfObject(obj)
				if err != nil {
					common.Log.Debug("Error parsing function: %v", err)
					return nil, err
				}
				shading5.Function = append(shading5.Function, function)
			}
		} else {
			fn, err := newPdfFunctionFromPdfObject(function)
			if err != nil {
				common.Log.Debug("Error parsing function: %v", err)
				return nil, err
			}
			shading5.Function = append(shading5.Function, fn)
		}
	}
	return &shading5, nil
}

func collectIndirectObjects(indirect *core.PdfIndirectObject) []*core.PdfIndirectObject {
	var indirects []*core.PdfIndirectObject
	if indirect == nil {
		return indirects
	}
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return indirects
	}
	name, _ := dict.Get("Type").(*core.PdfObjectName)
	if name != nil && string(*name) == "Page" {
		indirects = append(indirects, indirect)
		return indirects
	}
	arr, ok := dict.Get("Kids").(*core.PdfObjectArray)
	if !ok {
		return indirects
	}
	for _, obj := range arr.Elements() {
		if indirect, ok := obj.(*core.PdfIndirectObject); ok {
			indirects = append(indirects, collectIndirectObjects(indirect)...)
		}
	}
	return indirects
}

// ToPdfObject returns the PDF representation of the colorspace.
func (cs *PdfColorspaceSpecialPattern) ToPdfObject() core.PdfObject {
	if cs.UnderlyingCS == nil {
		return core.MakeName("Pattern")
	}
	array := core.MakeArray(core.MakeName("Pattern"))
	array.Append(cs.UnderlyingCS.ToPdfObject())
	if cs.container != nil {
		cs.container.PdfObject = array
		return cs.container
	}
	return array
}

// NewPdfAnnotation returns an initialized generic PDF annotation model.
func NewPdfAnnotation() *PdfAnnotation {
	annot := &PdfAnnotation{}
	annot.primitive = core.MakeIndirectObject(core.MakeDict())
	return annot
}

// GetCatalogMarkInfo gets catalog MarkInfo object.
func (r *PdfReader) GetCatalogMarkInfo() (core.PdfObject, bool) {
	if r.catalog == nil {
		return nil, false
	}
	markInfo := r.catalog.Get("MarkInfo")
	return markInfo, markInfo != nil
}

func parseType1Dictionary(s string) map[string]string {
	split := newlineRegexp.Split(s, -1)
	textByKey := map[string]string{}
	for _, s := range split {
		stringSubmatch := type1DefLineRegexp.FindStringSubmatch(s)
		if stringSubmatch == nil {
			continue
		}
		s, s2 := stringSubmatch[1], stringSubmatch[2]
		textByKey[s] = s2
	}
	return textByKey
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationHighlight) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Highlight"))
	dict.SetIfNotNil("QuadPoints", a.QuadPoints)
	return indirect
}

func parseCRL(data []byte) *parsedCRL {
	revocationList, err := x509.ParseRevocationList(data)
	if err != nil {
		return nil
	}
	crl := &parsedCRL{RevocationList: revocationList}
	for _, extension := range revocationList.Extensions {
		switch {
		case extension.Id.Equal(oidAuthorityKeyIdentifier), extension.Id.Equal(oidCRLNumber):
		case extension.Id.Equal(oidDeltaCRLIndicator):
			if crl.number != nil {
				return nil
			}
			if data, err := asn1.Unmarshal(extension.Value, &crl.number); err != nil || len(data) > 0 || crl.number.Sign() < 0 {
				return nil
			}
		case extension.Id.Equal(oidIssuingDistributionPoint):
			if crl.idp != nil {
				return nil
			}
			crl.idp, crl.idpRaw = &issuingDistributionPoint{}, extension.Value
			if data, err := asn1.Unmarshal(extension.Value, crl.idp); err != nil || len(data) > 0 {
				return nil
			}
		default:
			if extension.Critical {
				return nil
			}
		}
	}
	ok := crl.idp != nil && crl.idp.IndirectCRL
	for _, revokedCertificateEntry := range revocationList.RevokedCertificateEntries {
		for _, extension := range revokedCertificateEntry.Extensions {
			switch {
			case extension.Id.Equal(oidCertificateIssuer):
				if !ok {
					return nil
				}
			case extension.Id.Equal(oidCRLReason), extension.Id.Equal(oidInvalidityDate):
			default:
				if extension.Critical {
					return nil
				}
			}
		}
	}
	return crl
}

var (
	type1EncodingDictRegexp  = regexp.MustCompile("\\d+ dict\\s+(dup\\s+)?begin")
	type1DefLineRegexp       = regexp.MustCompile("^\\s*/(\\S+?)\\s+(.+?)\\s+def\\s*$")
	type1EncodingEntryRegexp = regexp.MustCompile("^\\s*dup\\s+(\\d+)\\s*/(\\w+?)(?:\\.\\d+)?\\s+put$")
	type1EncodingMarker      = "/Encoding 256 array"
	type1ReadonlyDefMarker   = "readonly def"
	type1EexecMarker         = "currentfile eexec"
)

// ValidateLinearization validates the PDF's linearization structure against
// ISO 32000-2 Annex F, returning a slice of violations (empty if it passes).
// Returns [core.LinErrNotLinearized] if the PDF is not linearized.
func (r *PdfReader) ValidateLinearization() ([]core.LinearizationViolation, error) {
	return r.parser.ValidateLinearization()
}

// PdfShadingPatternType3 is shading patterns that will use a Type 3 shading pattern (Radial).
type PdfShadingPatternType3 struct {
	*PdfPattern
	Shading   *PdfShadingType3
	Matrix    *core.PdfObjectArray
	ExtGState core.PdfObject
}

// ToPdfObject returns the PDF representation of the shading pattern.
func (sp *PdfShadingPattern) ToPdfObject() core.PdfObject {
	sp.PdfPattern.ToPdfObject()
	dict := sp.getDict()
	if sp.Shading != nil {
		dict.Set("Shading", sp.Shading.ToPdfObject())
	}
	if sp.Matrix != nil {
		dict.Set("Matrix", sp.Matrix)
	}
	if sp.ExtGState != nil {
		dict.Set("ExtGState", sp.ExtGState)
	}
	return sp.primitive
}

// EnableChain adds the specified certificate chain and validation data (OCSP
// and CRL information) for it to the global scope of the document DSS. The
// added data is used for validating any of the signatures present in the
// document. The LTV client attempts to build the certificate chain up to a
// trusted root by downloading any missing certificates.
func (l *LTV) EnableChain(chain []*x509.Certificate) error { return l.enable(nil, chain, "") }

// PdfShadingType6 is a Coons patch mesh.
type PdfShadingType6 struct {
	*PdfShading
	BitsPerCoordinate *core.PdfObjectInteger
	BitsPerComponent  *core.PdfObjectInteger
	BitsPerFlag       *core.PdfObjectInteger
	Decode            *core.PdfObjectArray
	Function          []PdfFunction
}

func (r *PdfReader) newPdfActionSoundFromDict(dict *core.PdfObjectDictionary) (*PdfActionSound, error) {
	return &PdfActionSound{Sound: dict.Get("Sound"), Volume: dict.Get("Volume"), Synchronous: dict.Get("Synchronous"), Repeat: dict.Get("Repeat"), Mix: dict.Get("Mix")}, nil
}

// SetMCID sets the MCID for the KValue.
func (kv *KValue) SetMCID(mcid int) { kv.Clear(); kv.mcid = &mcid }

func collectReachableObjectNumbers(indirect *core.PdfIndirectObject, numByObj map[core.PdfObject]int64) map[int64]struct{} {
	set := map[int64]struct{}{}
	set2 := map[core.PdfObject]struct{}{}
	var fn func(obj core.PdfObject)
	fn = func(obj core.PdfObject) {
		if obj == nil {
			return
		}
		if _, ok := set2[obj]; ok {
			return
		}
		set2[obj] = struct{}{}
		if n, ok := numByObj[obj]; ok {
			set[n] = struct{}{}
		}
		switch v := obj.(type) {
		case *core.PdfIndirectObject:
			fn(v.PdfObject)
		case *core.PdfObjectStream:
			if v.PdfObjectDictionary != nil {
				fn(v.PdfObjectDictionary)
			}
		case *core.PdfObjectDictionary:
			for _, name := range v.Keys() {
				if name == "Parent" {
					continue
				}
				fn(v.Get(name))
			}
		case *core.PdfObjectArray:
			for _, obj := range v.Elements() {
				fn(obj)
			}
		}
	}
	fn(indirect)
	return set
}

// SetRoleMapNS sets the namespace role map.
// Keys are structure type names in this namespace. Values are either a name
// (target type in the default standard namespace) or a two element array of
// [name, namespace dictionary reference].
func (ns *PdfStructureNamespace) SetRoleMapNS(roleMap *core.PdfObjectDictionary) {
	ns.roleMapNS = roleMap
}

// NewPdfActionLaunch returns a new "launch" action.
func NewPdfActionLaunch() *PdfActionLaunch {
	action := NewPdfAction()
	launchAction := &PdfActionLaunch{}
	launchAction.PdfAction = action
	action.SetContext(launchAction)
	return launchAction
}

func newPdfFontType0FromDict(dict *core.PdfObjectDictionary, fc *fontCommon) (*pdfFontType0, error) {
	descendantFonts, ok := core.GetArray(dict.Get("DescendantFonts"))
	if !ok {
		common.Log.Debug("ERROR: Invalid DescendantFonts - not an array %s", fc)
		return nil, core.ErrRangeError
	}
	if descendantFonts.Len() != 1 {
		common.Log.Debug("ERROR: Array length != 1 (%d)", descendantFonts.Len())
		return nil, core.ErrRangeError
	}
	font, err := newPdfFontFromPdfObjectInternal(descendantFonts.Get(0), false)
	if err != nil {
		common.Log.Debug("ERROR: Failed loading descendant font: err=%v %s", err, fc)
		return nil, err
	}
	encoding := dict.Get("Encoding")
	s := ""
	fontType0 := newPdfFontType0FromCommon(fc)
	fontType0.DescendantFont = font
	switch v := encoding.(type) {
	case *core.PdfObjectName:
		s, ok = core.GetNameVal(encoding)
		if ok {
			if s == "Identity-H" || s == "Identity-V" {
				identityTextEncoder := textencoding.NewIdentityTextEncoder(s)
				identityTextEncoder.SetToUnicodeCMap(fc.toUnicodeCMap)
				if cidToGIDMap := cidToGIDMapFromObject(font.GetCIDToGIDMapObject()); cidToGIDMap != nil {
					identityTextEncoder.SetCIDToGIDMap(cidToGIDMap)
				}
				fontType0.encoder = identityTextEncoder
			} else if cmap.IsPredefinedCMap(s) {
				fontType0.predefinedCMap, err = cmap.LoadPredefinedCMap(s)
				if err != nil {
					common.Log.Debug("WARN: could not load predefined CMap %s: %v", s, err)
				}
			} else {
				common.Log.Debug("Unhandled cmap %q", s)
			}
		}
	case *core.PdfObjectStream:
		if fontType0.toUnicode == nil {
			cidSystemInfo, err := cmap.NewCIDSystemInfo(v.PdfObjectDictionary.Get("CIDSystemInfo"))
			if err != nil {
				common.Log.Debug("Unable to get CIDSystemInfo: %v", err)
			}
			s := fmt.Sprintf("%s-%s-UCS2", cidSystemInfo.Registry, cidSystemInfo.Ordering)
			if cmap.IsPredefinedCMap(s) {
				fontType0.predefinedCMap, err = cmap.LoadPredefinedCMap(s)
				if err != nil {
					common.Log.Debug("WARN: could not load predefined CMap %s: %v", s, err)
				}
			} else {
				s = v.PdfObjectDictionary.Get("CMapName").String()
				stream, err := core.DecodeStream(v)
				if err != nil {
					common.Log.Debug("Unable to decode stream: %v", err)
					return fontType0, err
				}
				if ok := s == "OneByteIdentityH" || s == "OneByteIdentityV"; ok {
					fontType0.predefinedCMap, err = cmap.LoadCmapFromData(stream, ok)
					if err != nil {
						common.Log.Debug("Unable to load CMap from stream: %v -- %v", err, string(stream))
						return fontType0, err
					}
					textByCode := make(map[cmap.CharCode]string, 256)
					for i := 0x00; i <= 0xFF; i++ {
						textByCode[cmap.CharCode(i)] = string(rune(i))
					}
					fontType0.toUnicodeCMap = cmap.NewToUnicodeCMap(textByCode)
				}
			}
		}
	}
	if toUnicodeCMap := font.baseFields().toUnicodeCMap; toUnicodeCMap != nil {
		if name := toUnicodeCMap.Name(); name == "Adobe-CNS1-UCS2" || name == "Adobe-GB1-UCS2" || name == "Adobe-Japan1-UCS2" || name == "Adobe-Korea1-UCS2" {
			fontType0.encoder = textencoding.NewCMapEncoder(s, fontType0.predefinedCMap, toUnicodeCMap)
		}
	}
	return fontType0, nil
}

// SetLinearized toggles linearized output for the next Write call. When
// enabled, the writer rearranges the document into the layout described by
// ISO 32000-1 Annex F: header → linearization parameter dictionary → first
// xref → hint stream → first-page section → remaining objects → main xref.
func (w *PdfWriter) SetLinearized(enabled bool) { w.linearized = enabled }

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

func (r *PdfReader) flattenFieldsWithOpts(allannots bool, appgen FieldAppearanceGenerator, opts *FieldFlattenOpts) error {
	if opts == nil {
		opts = &FieldFlattenOpts{}
	}
	var ok bool
	set := map[*PdfAnnotation]bool{}
	{
		var fields []*PdfField
		acroForm := r.AcroForm
		if acroForm != nil {
			if opts.FilterFunc != nil {
				fields = acroForm.filteredFields(opts.FilterFunc, true)
				ok = acroForm.Fields != nil && len(*acroForm.Fields) > 0
			} else {
				fields = acroForm.AllFields()
			}
		}
		for _, field := range fields {
			if len(field.Annotations) < 1 {
				common.Log.Debug("No annotation found for: %v, look into Kids Object", field.PartialName())
				for i, kid := range field.Kids {
					for _, annotation := range kid.Annotations {
						set[annotation.PdfAnnotation] = field.V != nil
						if kid.V == nil {
							kid.V = field.V
						}
						if kid.T == nil {
							kid.T = core.MakeString(fmt.Sprintf("%s#%d", field.PartialName(), i))
						}
						if appgen != nil {
							generateAppearanceDict, err := appgen.GenerateAppearanceDict(acroForm, kid, annotation)
							if err != nil {
								return err
							}
							annotation.AP = generateAppearanceDict
						}
					}
				}
			}
			for _, annotation := range field.Annotations {
				set[annotation.PdfAnnotation] = field.V != nil
				if appgen != nil {
					generateAppearanceDict, err := appgen.GenerateAppearanceDict(acroForm, field, annotation)
					if err != nil {
						return err
					}
					annotation.AP = generateAppearanceDict
				}
			}
		}
	}
	if allannots {
		for _, page := range r.PageList {
			annotations, err := page.GetAnnotations()
			if err != nil {
				return err
			}
			for _, annotation := range annotations {
				set[annotation] = true
			}
		}
	}
	for _, page := range r.PageList {
		err := page.flattenFieldsWithOpts(appgen, opts, set)
		if err != nil {
			return err
		}
	}
	if !ok {
		r.AcroForm = nil
	}
	return nil
}

// SetAlpha sets the alpha layer for the image.
func (img *Image) SetAlpha(alpha []byte) { img.alpha = alpha }

func decodeStreams(streams []*core.PdfObjectStream) [][]byte {
	dataList := make([][]byte, 0, len(streams))
	for _, stream := range streams {
		if dssStream, ok := decodeDSSStream(stream); ok {
			dataList = append(dataList, dssStream)
		}
	}
	return dataList
}

// GetPageLabels returns the PageLabels entry in the PDF catalog.
// See section 12.4.2 "Page Labels" (p. 382 PDF32000_2008).
func (r *PdfReader) GetPageLabels() (core.PdfObject, error) {
	pageLabels := core.ResolveReference(r.catalog.Get("PageLabels"))
	if pageLabels == nil {
		return nil, nil
	}
	if !r.lazy {
		err := r.traverseObjectData(pageLabels)
		if err != nil {
			return nil, err
		}
	}
	return pageLabels, nil
}

// PdfOutlineTreeNode contains common fields used by the outline and outline
// item objects.
type PdfOutlineTreeNode struct {
	context interface{}
	First   *PdfOutlineTreeNode
	Last    *PdfOutlineTreeNode
}
type pdfSignDictionary struct {
	*core.PdfObjectDictionary
	handler              *SignatureHandler
	signature            *PdfSignature
	fileOffset           int64
	contentsOffsetStart  int
	contentsOffsetEnd    int
	byteRangeOffsetStart int
	byteRangeOffsetEnd   int
}

func (r *PdfReader) newPdfAnnotationPopupFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationPopup, error) {
	popup := PdfAnnotationPopup{}
	popup.Parent = dict.Get("Parent")
	popup.Open = dict.Get("Open")
	return &popup, nil
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationCaret) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Caret"))
	dict.SetIfNotNil("RD", a.RD)
	dict.SetIfNotNil("Sy", a.Sy)
	return indirect
}

// ToPdfObject returns the PDF representation of the shading pattern.
func (sp *PdfShadingPatternType2) ToPdfObject() core.PdfObject {
	sp.PdfPattern.ToPdfObject()
	dict := sp.getDict()
	if sp.Shading != nil {
		dict.Set("Shading", sp.Shading.ToPdfObject())
	}
	if sp.Matrix != nil {
		dict.Set("Matrix", sp.Matrix)
	}
	if sp.ExtGState != nil {
		dict.Set("ExtGState", sp.ExtGState)
	}
	return sp.primitive
}

const (
	BorderEffectNoEffect BorderEffect = iota
	BorderEffectCloudy   BorderEffect = iota
)

// NewPdfAction returns an initialized generic PDF action model.
func NewPdfAction() *PdfAction {
	action := &PdfAction{}
	action.primitive = core.MakeIndirectObject(core.MakeDict())
	return action
}

// XObjectImage (Table 89 in 8.9.5.1).
// Implements PdfModel interface.
type XObjectImage struct {
	// ColorSpace       PdfObject
	Width            *int64
	Height           *int64
	ColorSpace       PdfColorspace
	BitsPerComponent *int64
	Filter           core.StreamEncoder
	Intent           core.PdfObject
	ImageMask        core.PdfObject
	Mask             core.PdfObject
	Matte            core.PdfObject
	Decode           core.PdfObject
	Interpolate      core.PdfObject
	Alternatives     core.PdfObject
	SMask            core.PdfObject
	SMaskInData      core.PdfObject
	Name             core.PdfObject
	StructParent     core.PdfObject
	ID               core.PdfObject
	OPI              core.PdfObject
	Metadata         core.PdfObject
	OC               core.PdfObject
	Stream           []byte
	primitive        *core.PdfObjectStream
	building         bool
}

// IsCID returns true if the underlying font is CID.
func (f *PdfFont) IsCID() bool { return f.baseFields().isCIDFont() }

// IsHideToolbar returns the value of the hideToolbar flag.
func (vp *ViewerPreferences) IsHideToolbar() bool {
	if vp.hideToolbar == nil {
		return false
	}
	return *vp.hideToolbar
}

func (f *pdfFontType0) bytesToCharcodes(data []byte) ([]textencoding.CharCode, bool) {
	if f.predefinedCMap == nil {
		return nil, false
	}
	bytesToCharcodes, ok := f.predefinedCMap.BytesToCharcodes(data)
	if !ok {
		return nil, false
	}
	codes := make([]textencoding.CharCode, len(bytesToCharcodes))
	for i, bytesToCharcode := range bytesToCharcodes {
		codes[i] = textencoding.CharCode(bytesToCharcode)
	}
	return codes, true
}

func (f *pdfFontSimple) getFontDescriptor() *PdfFontDescriptor {
	if fontDescriptor := f.fontDescriptor; fontDescriptor != nil {
		return fontDescriptor
	}
	return f.std14Descriptor
}

// ImageHandler interface implements common image loading and processing tasks.
// Implementing as an interface allows for the possibility to use non-standard libraries for faster
// loading and processing of images.
type ImageHandler interface {
	// Read any image type and load into a new Image object.
	Read(r io.Reader) (*Image, error)

	// NewImageFromGoImage loads a NRGBA32 OpenPDF Image from a standard Go image structure.
	NewImageFromGoImage(img image.Image) (*Image, error)

	// NewGrayImageFromGoImage loads a grayscale OpenPDF Image from a standard Go image structure.
	NewGrayImageFromGoImage(img image.Image) (*Image, error)

	// Compress an image.
	Compress(img *Image, n int64) (*Image, error)
}

// G returns the value of the green component of the color.
func (c *PdfColorDeviceRGB) G() float64 { return c[1] }

func (r *PdfReader) newPdfAnnotationFileAttachmentFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationFileAttachment, error) {
	fileAttachment := PdfAnnotationFileAttachment{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	fileAttachment.PdfAnnotationMarkup = markup
	fileAttachment.FS = dict.Get("FS")
	fileAttachment.Name = dict.Get("Name")
	return &fileAttachment, nil
}

func colorFromArray(arr *core.PdfObjectArray) PdfColor {
	if arr == nil {
		return nil
	}
	floats := make([]float64, 0, arr.Len())
	for _, obj := range arr.Elements() {
		f, err := core.GetNumberAsFloat(obj)
		if err != nil {
			return nil
		}
		floats = append(floats, f)
	}
	switch len(floats) {
	case 1:
		col, err := NewPdfColorspaceDeviceGray().ColorFromFloats(floats)
		if err != nil {
			return nil
		}
		return col
	case 3:
		col, err := NewPdfColorspaceDeviceRGB().ColorFromFloats(floats)
		if err != nil {
			return nil
		}
		return col
	case 4:
		col, err := NewPdfColorspaceDeviceCMYK().ColorFromFloats(floats)
		if err != nil {
			return nil
		}
		return col
	}
	return nil
}

// GetContainingPdfObject implements interface PdfModel.
func (a *PdfAction) GetContainingPdfObject() core.PdfObject { return a.primitive }

func (s RevocationStatus) String() string {
	switch s {
	case RevocationStatusUnknown:
		return "unknown"
	case RevocationStatusGood:
		return "good"
	case RevocationStatusRevoked:
		return "revoked"
	}
	return "not checked"
}

// PdfAnnotationFreeText represents FreeText annotations.
// (Section 12.5.6.6).
type PdfAnnotationFreeText struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	DA core.PdfObject
	Q  core.PdfObject
	RC core.PdfObject
	DS core.PdfObject
	CL core.PdfObject
	IT core.PdfObject
	BE core.PdfObject
	RD core.PdfObject
	BS core.PdfObject
	LE core.PdfObject
}

func (w *PdfWriter) renumberForObjectStreams(layout *linearizationLayout, nums, nums2 []int64) (n, count, num int64) {
	var linearizationDictObjNum int64 = linearizationDictObjNum2
	for _, firstSection := range layout.firstSection {
		if firstSection > linearizationDictObjNum {
			linearizationDictObjNum = firstSection
		}
	}
	if layout.maxObjNum > linearizationDictObjNum {
		linearizationDictObjNum = layout.maxObjNum
	}
	size := linearizationDictObjNum + 1
	table := map[int64]int64{}
	for _, n := range nums2 {
		table[n] = size
		size++
	}
	n = size
	size++
	count = size
	size++
	num = size
	size++
	for _, n := range nums {
		table[n] = size
		size++
	}
	for _, object := range w.objects {
		n, _ := objectNumbers(object)
		if count, ok := table[n]; ok {
			switch v := object.(type) {
			case *core.PdfIndirectObject:
				v.ObjectNumber = count
			case *core.PdfObjectStream:
				v.ObjectNumber = count
			case *core.PdfObjectStreams:
				v.ObjectNumber = count
			}
		}
	}
	for i, mainSection := range layout.mainSection {
		if n, ok := table[mainSection]; ok {
			layout.mainSection[i] = n
		}
	}
	for i, mainShared := range layout.mainShared {
		if n, ok := table[mainShared]; ok {
			layout.mainShared[i] = n
		}
	}
	for i := range layout.pageObjs {
		for j, pageObj := range layout.pageObjs[i] {
			if n, ok := table[pageObj]; ok {
				layout.pageObjs[i][j] = n
			}
		}
	}
	return n, count, num
}

func (r *PdfReader) resolveReference(ref *core.PdfObjectReference) (core.PdfObject, bool, error) {
	if cachedObject, ok := r.parser.LoadCachedObject(int(ref.ObjectNumber)); ok {
		return cachedObject, true, nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Reader Lookup ref: %s", ref)
	}
	lookupByReference, err := r.parser.LookupByReference(*ref)
	if err != nil {
		return nil, false, err
	}
	r.parser.StoreCachedObject(int(ref.ObjectNumber), lookupByReference)
	return lookupByReference, false, nil
}

func getPdfKeywords() string {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	return pdfKeywords
}

func newPdfStructureNamespaceFromObject(obj core.PdfObject) (*PdfStructureNamespace, bool) {
	dict, ok := core.GetDict(obj)
	if !ok {
		return nil, false
	}
	ns, ok := core.GetString(dict.Get("NS"))
	if !ok {
		return nil, false
	}
	structureNamespace := &PdfStructureNamespace{uri: ns.Decoded()}
	if indirect, ok := core.GetIndirect(obj); ok {
		structureNamespace.indirect = indirect
	}
	structureNamespace.schema = dict.Get("Schema")
	if roleMapNS, ok := core.GetDict(dict.Get("RoleMapNS")); ok {
		structureNamespace.roleMapNS = roleMapNS
	}
	return structureNamespace, true
}

// SetPdfModifiedDate sets the ModDate attribute of the output PDF.
func SetPdfModifiedDate(modifiedDate time.Time) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	pdfModifiedDate = modifiedDate
}

func (c fontCommon) coreString() string {
	s := ""
	if c.fontDescriptor != nil {
		s = c.fontDescriptor.String()
	}
	return fmt.Sprintf("%#q %#q %q obj=%d ToUnicode=%t flags=0x%0x %s", c.subtype, c.baseFont, c.name, c.objectNumber, c.toUnicode != nil, c.fontFlags(), s)
}

func setAnnotationsAppearanceState(field *PdfField, obj core.PdfObject) {
	for _, annotation := range field.Annotations {
		annotation.AS = obj
		annotation.ToPdfObject()
	}
}

// IsHideWindowUI returns the value of the hideWindowUI flag.
func (vp *ViewerPreferences) IsHideWindowUI() bool {
	if vp.hideWindowUI == nil {
		return false
	}
	return *vp.hideWindowUI
}

func objectNumbers(obj core.PdfObject) (int64, int64) {
	switch v := obj.(type) {
	case *core.PdfIndirectObject:
		return v.ObjectNumber, v.GenerationNumber
	case *core.PdfObjectStream:
		return v.ObjectNumber, v.GenerationNumber
	case *core.PdfObjectStreams:
		return v.ObjectNumber, v.GenerationNumber
	}
	return 0, 0
}

// GetStructParentsKey returns the StructParents key.
// If not set, returns -1.
func (p *PdfPage) GetStructParentsKey() int {
	if n, ok := core.GetIntVal(p.StructParents); ok {
		return n
	}
	return -1
}

// PdfAnnotationWatermark represents Watermark annotations.
// (Section 12.5.6.22).
type PdfAnnotationWatermark struct {
	*PdfAnnotation
	FixedPrint core.PdfObject
}

// NewPdfShadingPatternType1 creates an empty shading pattern type 1 object.
func NewPdfShadingPatternType1() *PdfShadingPatternType1 {
	shadingPatternType1 := &PdfShadingPatternType1{}
	shadingPatternType1.Matrix = core.MakeArrayFromIntegers([]int{1, 0, 0, 1, 0, 0})
	shadingPatternType1.PdfPattern = &PdfPattern{}
	shadingPatternType1.PdfPattern.PatternType = int64(*core.MakeInteger(2))
	shadingPatternType1.PdfPattern.context = shadingPatternType1
	shadingPatternType1.PdfPattern.primitive = core.MakeIndirectObject(core.MakeDict())
	return shadingPatternType1
}

// NewKValue creates a new K value object.
func NewKValue() *KValue { return &KValue{} }

// WatermarkTextOptions contains options for configuring the text watermark process.
type WatermarkTextOptions struct {
	// opacity, 0.5 by default.
	Alpha float64

	// font size, 20 by default.
	FontSize float64

	// it is mandatory to set FontPath to the path of .ttf font file you would like to use for drawing.
	FontPath string

	// font color, black by default.
	FontColor color.Color

	// rotation angle in degrees, 0 by default.
	Angle float64
}

// NewPdfShadingPatternType2 creates an empty shading pattern type 2 object.
func NewPdfShadingPatternType2() *PdfShadingPatternType2 {
	shadingPatternType2 := &PdfShadingPatternType2{}
	shadingPatternType2.Matrix = core.MakeArrayFromIntegers([]int{1, 0, 0, 1, 0, 0})
	shadingPatternType2.PdfPattern = &PdfPattern{}
	shadingPatternType2.PdfPattern.PatternType = int64(*core.MakeInteger(2))
	shadingPatternType2.PdfPattern.context = shadingPatternType2
	shadingPatternType2.PdfPattern.primitive = core.MakeIndirectObject(core.MakeDict())
	return shadingPatternType2
}

// Permissions specify a permissions dictionary (PDF 1.5).
// (Section 12.8.4, Table 258 - Entries in a permissions dictionary p. 477 in PDF32000_2008).
type Permissions struct {
	DocMDP    *PdfSignature
	primitive *core.PdfObjectDictionary
}

// AddChild adds a child object.
func (k *KDict) AddChild(kv *KValue) { k.children = append(k.children, kv) }

func (w *PdfWriter) optimize() error {
	if w.optimizer == nil {
		return nil
	}
	var err error
	w.objects, err = w.optimizer.Optimize(w.objects)
	if err != nil {
		return err
	}
	set := make(map[core.PdfObject]struct{}, len(w.objects))
	for _, object := range w.objects {
		set[object] = struct{}{}
	}
	w.hasObjects = set
	return nil
}

func (t *trustEvaluator) checkOnline(revocation certRevocation, cert, cert2 *x509.Certificate, t2 time.Time, ok bool, evidence revocationEvidence) (certRevocation, []string) {
	var strs []string
	evidence = revocationEvidence{ocsps: append([]ocspEvidence(nil), evidence.ocsps...), crls: append([]crlEvidence(nil), evidence.crls...)}
	ocspClient := t.opts.OCSPClient
	if ocspClient == nil {
		ocspClient = sigutil.NewOCSPClient()
	}
	sum256, data := sha256.Sum256(cert.Raw), sha256.Sum256(cert2.Raw)
	for _, ocspServer := range cert.OCSPServer {
		if revocation.settled() {
			return revocation, strs
		}
		if !isHTTPURL(ocspServer) {
			continue
		}
		fetch := t.fetch(fetchKey{url: ocspServer, certHash: sum256, issuerHash: data}, func() fetchResult {
			_, data, err := ocspClient.MakeRequest(ocspServer, cert, cert2)
			return fetchResult{body: data, err: err}
		})
		if fetch.err != nil {
			strs = append(strs, fmt.Sprintf("OCSP request to %s failed: %v", ocspServer, fetch.err))
			continue
		}
		evidence.ocsps = append(evidence.ocsps, newOCSPEvidence(fetch.body, true))
		revocation = t.checkCertRevocation(cert, cert2, t2, ok, evidence)
	}
	crlClient := t.opts.CRLClient
	if crlClient == nil {
		crlClient = sigutil.NewCRLClient()
	}
	for _, crlDistributionPoint := range cert.CRLDistributionPoints {
		if revocation.settled() {
			return revocation, strs
		}
		if !isHTTPURL(crlDistributionPoint) {
			continue
		}
		fetch := t.fetch(fetchKey{url: crlDistributionPoint}, func() fetchResult {
			request, err := crlClient.MakeRequest(crlDistributionPoint, cert)
			if err != nil {
				return fetchResult{err: err}
			}
			return fetchResult{crl: t.parseCRL(request)}
		})
		if fetch.err != nil {
			strs = append(strs, fmt.Sprintf("CRL request to %s failed: %v", crlDistributionPoint, fetch.err))
			continue
		}
		if fetch.crl == nil {
			strs = append(strs, fmt.Sprintf("CRL from %s is not a valid CRL", crlDistributionPoint))
			continue
		}
		evidence.crls = append(evidence.crls, crlEvidence{parsedCRL: fetch.crl, online: true})
		revocation = t.checkCertRevocation(cert, cert2, t2, ok, evidence)
	}
	return revocation, strs
}

// PdfSignature represents a PDF signature dictionary and is used for signing via form signature fields.
// (Section 12.8, Table 252 - Entries in a signature dictionary p. 475 in PDF32000_2008).
type PdfSignature struct {
	Handler   SignatureHandler
	primitive *core.PdfIndirectObject

	// Type: Sig/DocTimeStamp
	Type         *core.PdfObjectName
	Filter       *core.PdfObjectName
	SubFilter    *core.PdfObjectName
	Contents     *core.PdfObjectString
	Cert         core.PdfObject
	ByteRange    *core.PdfObjectArray
	Reference    *core.PdfObjectArray
	Changes      *core.PdfObjectArray
	Name         *core.PdfObjectString
	M            *core.PdfObjectString
	Location     *core.PdfObjectString
	Reason       *core.PdfObjectString
	ContactInfo  *core.PdfObjectString
	R            *core.PdfObjectInteger
	V            *core.PdfObjectInteger
	PropBuild    *core.PdfObjectDictionary
	PropAuthTime *core.PdfObjectInteger
	PropAuthType *core.PdfObjectName
}

// HasPatternByName checks whether a pattern object is defined by the specified keyName.
func (r *PdfPageResources) HasPatternByName(keyName core.PdfObjectName) bool {
	_, ok := r.GetPatternByName(keyName)
	return ok
}

// ImageToRGB convert an indexed image to RGB.
func (cs *PdfColorspaceSpecialIndexed) ImageToRGB(img Image) (Image, error) {
	N := cs.Base.GetNumComponents()
	if N < 1 {
		return Image{}, fmt.Errorf("bad base colorspace NumComponents=%d", N)
	}
	n := len(cs.lookupTable) / N
	if n < 1 {
		imageBase := imageutil.NewImageBase(int(img.Width), int(img.Height), 8, N, nil, img.alpha, img.decode)
		return cs.Base.ImageToRGB(imageFromImageBase(&imageBase))
	}
	imageBase := imageutil.NewImageBase(n, 1, 8, N, nil, nil, img.decode)
	writer := sampling.NewWriter(imageBase)
	for i := 0; i < n*N; i++ {
		if err := writer.WriteSample(uint32(cs.lookupTable[i])); err != nil {
			return img, err
		}
	}
	imageToRGB, err := cs.Base.ImageToRGB(imageFromImageBase(&imageBase))
	if err != nil {
		return img, err
	}
	base := imageToRGB.getBase()
	reader := sampling.NewReader(base)
	numsList := make([][3]uint32, n)
	for i := 0; i < n; i++ {
		for j := 0; j < 3; j++ {
			sample, err := reader.ReadSample()
			if err != nil {
				return img, err
			}
			numsList[i][j] = sample
		}
	}
	base2 := imageutil.NewImageBase(int(img.Width), int(img.Height), base.BitsPerComponent, 3, nil, img.alpha, base.Decode)
	r := sampling.NewReader(img.getBase())
	w := sampling.NewWriter(base2)
	count := n - 1
	var (
		num  uint32
		err2 error
	)
	for {
		num, err2 = r.ReadSample()
		if err2 == io.EOF {
			break
		} else if err2 != nil {
			return img, err2
		}
		size := int(num)
		if size > count {
			size = count
		}
		if err := w.WriteSamples(numsList[size][:]); err != nil {
			return img, err
		}
	}
	return imageFromImageBase(&base2), nil
}

// NewPdfAnnotation3D returns a new 3d annotation.
func NewPdfAnnotation3D() *PdfAnnotation3D {
	annot := NewPdfAnnotation()
	annot3D := &PdfAnnotation3D{}
	annot3D.PdfAnnotation = annot
	annot.SetContext(annot3D)
	return annot3D
}

// ToPdfObject returns a stream object.
func (x *XObjectImage) ToPdfObject() core.PdfObject {
	stream := x.primitive
	if x.building {
		return stream
	}
	dict := stream.PdfObjectDictionary
	if x.Filter != nil {
		dict = x.Filter.MakeStreamDict()
		stream.PdfObjectDictionary = dict
	}
	dict.Set("Type", core.MakeName("XObject"))
	dict.Set("Subtype", core.MakeName("Image"))
	dict.Set("Width", core.MakeInteger(*x.Width))
	dict.Set("Height", core.MakeInteger(*x.Height))
	if x.BitsPerComponent != nil {
		dict.Set("BitsPerComponent", core.MakeInteger(*x.BitsPerComponent))
	}
	if x.ColorSpace != nil {
		dict.SetIfNotNil("ColorSpace", x.ColorSpace.ToPdfObject())
	}
	dict.SetIfNotNil("Intent", x.Intent)
	dict.SetIfNotNil("ImageMask", x.ImageMask)
	dict.SetIfNotNil("Mask", x.Mask)
	ok := dict.Get("Decode") != nil
	if x.Decode == nil && ok {
		dict.Remove("Decode")
	} else if x.Decode != nil {
		dict.Set("Decode", x.Decode)
	}
	dict.SetIfNotNil("Interpolate", x.Interpolate)
	dict.SetIfNotNil("Alternatives", x.Alternatives)
	dict.SetIfNotNil("SMask", x.SMask)
	dict.SetIfNotNil("SMaskInData", x.SMaskInData)
	dict.SetIfNotNil("Matte", x.Matte)
	dict.SetIfNotNil("Name", x.Name)
	dict.SetIfNotNil("StructParent", x.StructParent)
	dict.SetIfNotNil("ID", x.ID)
	dict.SetIfNotNil("OPI", x.OPI)
	dict.SetIfNotNil("Metadata", x.Metadata)
	dict.SetIfNotNil("OC", x.OC)
	dict.Set("Length", core.MakeInteger(int64(len(x.Stream))))
	stream.Stream = x.Stream
	x.building = true
	return stream
}

// SetPdfAuthor sets the Author attribute of the output PDF.
func SetPdfAuthor(author string) { metadataMu.Lock(); defer metadataMu.Unlock(); pdfAuthor = author }

// SetDocInfo sets the document /Info metadata.
// This will overwrite any globally declared document info.
func (a *PdfAppender) SetDocInfo(info *PdfInfo) { a.docInfo = info }

func (r *PdfReader) newPdfAnnotationRedactFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationRedact, error) {
	redact := PdfAnnotationRedact{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	redact.PdfAnnotationMarkup = markup
	redact.QuadPoints = dict.Get("QuadPoints")
	redact.IC = dict.Get("IC")
	redact.RO = dict.Get("RO")
	redact.OverlayText = dict.Get("OverlayText")
	redact.Repeat = dict.Get("Repeat")
	redact.DA = dict.Get("DA")
	redact.Q = dict.Get("Q")
	return &redact, nil
}

func inheritReaderOpts(reader *PdfReader, opts *ReaderOpts) *ReaderOpts {
	if reader.parser == nil || reader.parser.Opts == nil {
		return opts
	}
	readerOpts := NewReaderOpts()
	if opts != nil {
		*readerOpts = *opts
	}
	if readerOpts.MaxDecodedStreamSize == 0 {
		readerOpts.MaxDecodedStreamSize = reader.parser.Opts.MaxDecodedStreamSize
	}
	if readerOpts.MaxDecodedTotalSize == 0 {
		readerOpts.MaxDecodedTotalSize = reader.parser.Opts.MaxDecodedTotalSize
	}
	return readerOpts
}

// AddKChild adds a child K dictionary object.
func (k *KDict) AddKChild(kChild *KDict) {
	k.children = append(k.children, &KValue{kDict: kChild})
}

// SetHideToolbar sets the value of the hideToolbar flag.
func (vp *ViewerPreferences) SetHideToolbar(hideToolbar bool) { vp.hideToolbar = &hideToolbar }

// NewPdfFontFromTTFFile loads a TTF font file and returns a PdfFont type
// that can be used in text styling functions.
// Uses a WinAnsiTextEncoder and loads only character codes 32-255.
// NOTE: For composite fonts such as used in symbolic languages, use NewCompositePdfFontFile.
func NewPdfFontFromTTFFile(filePath string) (*PdfFont, error) {
	open, err := os.Open(filePath)
	if err != nil {
		common.Log.Debug("ERROR: reading TTF font file: %v", err)
		return nil, err
	}
	defer open.Close()
	return NewPdfFontFromTTF(open)
}

// Subtype returns the font's "Subtype" field.
func (f *PdfFont) Subtype() string {
	subtype := f.baseFields().subtype
	if font, ok := f.context.(*pdfFontType0); ok {
		subtype = subtype + ":" + font.DescendantFont.Subtype()
	}
	return subtype
}

func pageToPdfObjectLicensed(page *PdfPage) core.PdfObject {
	licenseKey := license.GetLicenseKey()
	if licenseKey != nil && licenseKey.IsLicensed() {
		return page.ToPdfObject()
	}
	name := core.PdfObjectName("UF1")
	if !page.Resources.HasFontByName(name) {
		page.Resources.SetFontByName(name, DefaultFont().ToPdfObject())
	}
	var strs []string
	strs = append(strs, "q")
	strs = append(strs, "BT")
	strs = append(strs, fmt.Sprintf("/%s 14 Tf", name.String()))
	strs = append(strs, "1 0 0 rg")
	strs = append(strs, "10 10 Td")
	s := "Unlicensed OpenPDF"
	strs = append(strs, fmt.Sprintf("(%s) Tj", s))
	strs = append(strs, "ET")
	strs = append(strs, "Q")
	join := strings.Join(strs, "\n")
	page.AddContentStreamByString(join)
	return page.ToPdfObject()
}

// DecodeArray returns an empty slice as there are no components associated with pattern colorspace.
func (cs *PdfColorspaceSpecialPattern) DecodeArray() []float64 { return []float64{} }

func clampToRange(floats, floats2 []float64) []float64 {
	if len(floats2) != 2*len(floats) {
		return floats
	}
	for i := range floats {
		floats[i] = math.Min(math.Max(floats[i], floats2[2*i]), floats2[2*i+1])
	}
	return floats
}

// PrintArea returns the value of the printArea.
func (vp *ViewerPreferences) PrintArea() PageBoundary { return vp.printArea }

// MakeLazy prepares the XObject's stream object and moves the encoded stream
// data into a temporary file, reducing memory usage. The stream dictionary is
// frozen by this call: field changes made afterwards are not reflected in the
// written object, so attach entries such as SMask or Mask before calling it.
func (x *XObjectImage) MakeLazy() error {
	x.ToPdfObject()
	if err := x.primitive.MakeLazy(); err != nil {
		return err
	}
	x.Stream = nil
	return nil
}

func (a *PdfAcroForm) fill(provider FieldValueProvider, appGen FieldAppearanceGenerator) error {
	if a == nil {
		return nil
	}
	fieldValues, err := provider.FieldValues()
	if err != nil {
		return err
	}
	for _, allField := range a.AllFields() {
		partialName := allField.PartialName()
		var obj core.PdfObject
		var ok bool
		if fullName, err := allField.FullName(); err == nil {
			obj, ok = fieldValues[fullName]
		}
		if !ok {
			obj, ok = fieldValues[partialName]
		}
		if !ok {
			common.Log.Debug("WARN: form field %s not found in the provider. Skipping.", partialName)
			continue
		}
		if err := setFieldValue(allField, obj); err != nil {
			return err
		}
		if appGen == nil {
			continue
		}
		for _, widget := range fieldWidgets(allField) {
			generateAppearanceDict, err := appGen.GenerateAppearanceDict(a, allField, widget)
			if err != nil {
				return err
			}
			widget.AP = generateAppearanceDict
			widget.ToPdfObject()
		}
	}
	return nil
}

// DecryptWithCertificate decrypts a document encrypted with a public-key
// security handler (/Filter /Adobe.PubSec) using a recipient's certificate and
// private key (an *rsa.PrivateKey), then loads the document structure. It
// returns security.ErrNotRecipient when the certificate is not among the
// recipients, security.ErrRecipientNotOpened when the recipient's entry does
// not open with the key, and core.ErrPasswordRequired when the document uses
// the standard security handler.
func (r *PdfReader) DecryptWithCertificate(cert *x509.Certificate, key crypto.PrivateKey) (bool, error) {
	ok, err := r.parser.DecryptWithCertificate(cert, key)
	if err != nil {
		return ok, err
	}
	if !ok {
		return false, nil
	}
	err = r.loadStructure()
	if err != nil {
		common.Log.Debug("ERROR: Fail to load structure (%s)", err)
		return false, err
	}
	return true, nil
}

func (a *PdfAppender) replaceObject(obj, obj2 core.PdfObject) {
	switch v := obj.(type) {
	case *core.PdfIndirectObject:
		a.objNumMap[obj2] = v.ObjectNumber
	case *core.PdfObjectStream:
		a.objNumMap[obj2] = v.ObjectNumber
	}
}

// ImageToRGB converts an image with samples in Separation CS to an image with samples specified in
// DeviceRGB CS.
func (cs *PdfColorspaceSpecialSeparation) ImageToRGB(img Image) (Image, error) {
	reader := sampling.NewReader(img.getBase())
	imageBase := imageutil.NewImageBase(int(img.Width), int(img.Height), int(img.BitsPerComponent), cs.AlternateSpace.GetNumComponents(), nil, img.alpha, nil)
	writer := sampling.NewWriter(imageBase)
	f := math.Pow(2, float64(img.BitsPerComponent)) - 1
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Separation color space -> ToRGB conversion")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("TintTransform: %+v", cs.TintTransform)
	}
	array := cs.AlternateSpace.DecodeArray()
	var (
		n   uint32
		err error
	)
	for {
		n, err = reader.ReadSample()
		if err == io.EOF {
			break
		}
		if err != nil {
			return img, err
		}
		v := float64(n) / f
		evaluate, err := cs.TintTransform.Evaluate([]float64{v})
		if err != nil {
			return img, err
		}
		for i, v := range evaluate {
			linearInterpolate := imageutil.LinearInterpolate(v, array[i*2], array[i*2+1], 0, 1)
			if err = writer.WriteSample(uint32(linearInterpolate * f)); err != nil {
				return img, err
			}
		}
	}
	return cs.AlternateSpace.ImageToRGB(imageFromImageBase(&imageBase))
}

// NewPdfFieldSignature returns an initialized signature field.
func NewPdfFieldSignature(signature *PdfSignature) *PdfFieldSignature {
	signatureField := &PdfFieldSignature{}
	signatureField.PdfField = NewPdfField()
	signatureField.SetContext(signatureField)
	signatureField.PdfAnnotationWidget = NewPdfAnnotationWidget()
	signatureField.PdfAnnotationWidget.SetContext(signatureField)
	signatureField.PdfAnnotationWidget.primitive = signatureField.primitive
	signatureField.T = core.MakeString("")
	signatureField.F = core.MakeInteger(132)
	signatureField.V = signature
	return signatureField
}

// SetNamespace sets the PDF 2.0 structure namespace of this element.
func (k *KDict) SetNamespace(ns *PdfStructureNamespace) { k.NS = ns }

func buildMainXrefTable(writer *PdfWriter, nums []int64, n int64, preObjByNum map[int64]preserializedObject, count int64, hintOffset int64, mainXrefOffset int64, linDictOffset int64, firstXrefOffset int64) ([]byte, int64) {
	table := map[int64]int64{}
	for _, num := range nums {
		switch num {
		case linearizationDictObjNum2:
			table[num] = linDictOffset
		case count:
			table[num] = hintOffset
		default:
			table[num] = preObjByNum[num].offset
		}
	}
	xrefTable, num := buildXrefTable(writer, nums, table, n, 0, false, firstXrefOffset, true)
	size := mainXrefOffset + num
	return xrefTable, size
}

// Release returns the image's data buffers to the shared byte pool.
// After Release, Image.Data and the alpha channel are nil and the Image
// must not be used further.
//
// The pool classifies buffers by capacity, not origin: buffers whose cap
// exactly matches a bucket size are accepted for reuse regardless of
// whether they came from AcquireBytes or from a plain make; buffers whose
// cap does not match are dropped. In practice this means Release is safe
// to call on any Image, but callers that assign Image.Data via make and
// happen to hit a bucket size will hand that slab to the pool — the
// "must not be used further" rule is what keeps that safe.
//
// Release is optional. Callers that don't invoke it pay normal GC cost.
func (img *Image) Release() {
	if img == nil {
		return
	}
	if img.Data != nil {
		core.ReleaseBytes(img.Data)
		img.Data = nil
	}
	if img.alpha != nil {
		core.ReleaseBytes(img.alpha)
		img.alpha = nil
	}
}

// NewPdfColorspaceLab returns a new Lab colorspace object.
func NewPdfColorspaceLab() *PdfColorspaceLab {
	labCS := &PdfColorspaceLab{}
	labCS.BlackPoint = []float64{0.0, 0.0, 0.0}
	labCS.Range = []float64{-100, 100, -100, 100}
	return labCS
}

func (w *PdfWriter) writeTrailer(n int) {
	w.writeString("xref\r\n")
	for i := 0; i <= n; {
		for ; i <= n; i++ {
			xref, ok := w.crossReferences[i]
			if ok && (!w.appendMode || w.appendMode && (xref.Type == 1 && xref.Offset >= w.originalSize || xref.Type == 0)) {
				break
			}
		}
		var count int
		for count = i + 1; count <= n; count++ {
			xref, ok := w.crossReferences[count]
			if ok && (!w.appendMode || w.appendMode && (xref.Type == 1 && xref.Offset > w.originalSize)) {
				continue
			}
			break
		}
		s := fmt.Sprintf("%d %d\r\n", i, count-i)
		w.writeString(s)
		for j := i; j < count; j++ {
			xref := w.crossReferences[j]
			switch xref.Type {
			case 0:
				s = fmt.Sprintf("%.10d %.5d f\r\n", 0, 65535)
				w.writeString(s)
			case 1:
				s = fmt.Sprintf("%.10d %.5d n\r\n", xref.Offset, 0)
				w.writeString(s)
			}
		}
		i = count + 1
	}
	dict := core.MakeDict()
	dict.Set("Info", w.infoObj)
	dict.Set("Root", w.root)
	dict.Set("Size", core.MakeInteger(int64(n+1)))
	if w.appendMode && w.prevXrefOffset > 0 {
		dict.Set("Prev", core.MakeInteger(w.prevXrefOffset))
	}
	if w.crypter != nil {
		dict.Set("Encrypt", w.encryptObj)
	}
	if w.ids == nil && w.id0 != "" && w.id1 != "" {
		w.ids = core.MakeArray(core.MakeHexString(w.id0), core.MakeHexString(w.id1))
	}
	if w.ids != nil {
		dict.Set("ID", w.ids)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Ids: %s", w.ids)
		}
	}
	w.writeString("trailer\n")
	w.writeBytes(dict.Write())
	w.writeString("\n")
}

// GetPatternByName gets the pattern specified by keyName. Returns nil if not existing.
// The bool flag indicated whether it was found or not.
func (r *PdfPageResources) GetPatternByName(keyName core.PdfObjectName) (*PdfPattern, bool) {
	if r.Pattern == nil {
		return nil, false
	}
	dict, ok := core.TraceToDirectObject(r.Pattern).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Invalid Pattern entry - not a dict (got %T)", r.Pattern)
		return nil, false
	}
	if obj := dict.Get(keyName); obj != nil {
		pattern, err := newPdfPatternFromPdfObject(obj)
		if err != nil {
			common.Log.Debug("ERROR: failed to load pdf pattern: %v", err)
			return nil, false
		}
		return pattern, true
	}
	return nil, false
}

// GetCharMetrics returns the char metrics for character code `code`.
func (f pdfFontType0) GetCharMetrics(code textencoding.CharCode) (fonts.CharMetrics, bool) {
	if f.DescendantFont == nil {
		common.Log.Debug("ERROR: No descendant. font=%s", f)
		return fonts.CharMetrics{}, false
	}
	return f.DescendantFont.GetCharMetrics(code)
}

// SetXObjectByName adds the XObject from the passed in stream to the page resources.
// The added XObject is identified by the specified name.
func (r *PdfPageResources) SetXObjectByName(keyName core.PdfObjectName, stream *core.PdfObjectStream) error {
	if r.XObject == nil {
		r.XObject = core.MakeDict()
	}
	obj := core.TraceToDirectObject(r.XObject)
	dict, ok := obj.(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("Invalid XObject, got %T/%T", r.XObject, obj)
		return errors.New("type check error")
	}
	dict.Set(keyName, stream)
	return nil
}

// ToPdfObject returns the PDF representation of the shading dictionary.
func (s *PdfShadingType2) ToPdfObject() core.PdfObject {
	s.PdfShading.ToPdfObject()
	shadingDict, err := s.getShadingDict()
	if err != nil {
		common.Log.Error("Unable to access shading dict")
		return nil
	}
	if shadingDict == nil {
		common.Log.Error("Shading dict is nil")
		return nil
	}
	if s.Coords != nil {
		shadingDict.Set("Coords", s.Coords)
	}
	if s.Domain != nil {
		shadingDict.Set("Domain", s.Domain)
	}
	if s.Function != nil {
		if len(s.Function) == 1 {
			shadingDict.Set("Function", s.Function[0].ToPdfObject())
		} else {
			array := core.MakeArray()
			for _, function := range s.Function {
				array.Append(function.ToPdfObject())
			}
			shadingDict.Set("Function", array)
		}
	}
	if s.Extend != nil {
		shadingDict.Set("Extend", s.Extend)
	}
	return s.primitive
}

type fetchResult struct {
	body []byte
	crl  *parsedCRL
	err  error
}

// AddFont adds a font dictionary to the Font resources.
func (p *PdfPage) AddFont(name core.PdfObjectName, font core.PdfObject) error {
	if p.Resources == nil {
		p.Resources = NewPdfPageResources()
	}
	if p.Resources.Font == nil {
		p.Resources.Font = core.MakeDict()
	}
	dict, ok := core.TraceToDirectObject(p.Resources.Font).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("Expected font dictionary is not a dictionary: %v", core.TraceToDirectObject(p.Resources.Font))
		return errors.New("type check error")
	}
	dict.Set(name, font)
	return nil
}

func (r *PdfReader) newPdfAnnotationScreenFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationScreen, error) {
	screen := PdfAnnotationScreen{}
	screen.T = dict.Get("T")
	screen.MK = dict.Get("MK")
	screen.A = dict.Get("A")
	screen.AA = dict.Get("AA")
	return &screen, nil
}

// NewPdfOutputIntentFromPdfObject creates a new PdfOutputIntent from the input core.PdfObject.
func NewPdfOutputIntentFromPdfObject(object core.PdfObject) (*PdfOutputIntent, error) {
	outputIntent := &PdfOutputIntent{}
	if err := outputIntent.ParsePdfObject(object); err != nil {
		return nil, err
	}
	return outputIntent, nil
}

// GetAlphabet returns a map of the runes in `text` and their frequencies.
func GetAlphabet(text string) map[rune]int {
	table := map[rune]int{}
	for _, n := range text {
		table[n]++
	}
	return table
}

// ToPdfObject returns the PDF representation of the colorspace.
func (cs *PdfColorspaceSpecialSeparation) ToPdfObject() core.PdfObject {
	array := core.MakeArray(core.MakeName("Separation"))
	array.Append(cs.ColorantName)
	array.Append(cs.AlternateSpace.ToPdfObject())
	array.Append(cs.TintTransform.ToPdfObject())
	if cs.container != nil {
		cs.container.PdfObject = array
		return cs.container
	}
	return array
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

func newPdfFontDescriptorFromPdfObject(obj core.PdfObject) (*PdfFontDescriptor, error) {
	desc := &PdfFontDescriptor{}
	obj = core.ResolveReference(obj)
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		desc.primitive = indirect
		obj = indirect.PdfObject
	}
	dict, ok := core.GetDict(obj)
	if !ok {
		common.Log.Debug("ERROR: FontDescriptor not given by a dictionary (%T)", obj)
		return nil, core.ErrTypeError
	}
	if fontName := dict.Get("FontName"); fontName != nil {
		desc.FontName = fontName
	} else {
		common.Log.Debug("Incompatibility: FontName (Required) missing")
	}
	fontNameName, _ := core.GetName(desc.FontName)
	if obj := dict.Get("Type"); obj != nil {
		name, ok := obj.(*core.PdfObjectName)
		if !ok || string(*name) != "FontDescriptor" {
			common.Log.Debug("Incompatibility: Font descriptor Type invalid (%T) font=%q %T", obj, fontNameName, desc.FontName)
		}
	} else {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Incompatibility: Type (Required) missing. font=%q %T", fontNameName, desc.FontName)
		}
	}
	desc.FontFamily = dict.Get("FontFamily")
	desc.FontStretch = dict.Get("FontStretch")
	desc.FontWeight = dict.Get("FontWeight")
	desc.Flags = dict.Get("Flags")
	desc.FontBBox = dict.Get("FontBBox")
	desc.ItalicAngle = dict.Get("ItalicAngle")
	desc.Ascent = dict.Get("Ascent")
	desc.Descent = dict.Get("Descent")
	desc.Leading = dict.Get("Leading")
	desc.CapHeight = dict.Get("CapHeight")
	desc.XHeight = dict.Get("XHeight")
	desc.StemV = dict.Get("StemV")
	desc.StemH = dict.Get("StemH")
	desc.AvgWidth = dict.Get("AvgWidth")
	desc.MaxWidth = dict.Get("MaxWidth")
	desc.MissingWidth = dict.Get("MissingWidth")
	desc.FontFile = dict.Get("FontFile")
	desc.FontFile2 = dict.Get("FontFile2")
	desc.FontFile3 = dict.Get("FontFile3")
	desc.CharSet = dict.Get("CharSet")
	desc.Style = dict.Get("Style")
	desc.Lang = dict.Get("Lang")
	desc.FD = dict.Get("FD")
	desc.CIDSet = dict.Get("CIDSet")
	if desc.Flags != nil {
		if n, ok := core.GetIntVal(desc.Flags); ok {
			desc.flags = n
		}
	}
	if desc.MissingWidth != nil {
		if f, err := core.GetNumberAsFloat(desc.MissingWidth); err == nil {
			desc.missingWidth = f
		}
	}
	if desc.FontFile != nil {
		file, err := newFontFileFromPdfObject(desc.FontFile)
		if err != nil {
			return desc, err
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("fontFile=%s", file)
		}
		desc.fontFile = file
	}
	if desc.FontFile2 != nil {
		fontFile2, err := fonts.NewFontFile2FromPdfObject(desc.FontFile2)
		if err != nil {
			return desc, err
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("fontFile2=%s", fontFile2.String())
		}
		desc.ttf = &fontFile2
	}
	return desc, nil
}

// PdfColorspaceDeviceNAttributes contains additional information about the components of colour space that
// conforming readers may use. Conforming readers need not use the alternateSpace and tintTransform parameters,
// and may instead use a custom blending algorithms, along with other information provided in the attributes
// dictionary if present.
type PdfColorspaceDeviceNAttributes struct {
	Subtype     *core.PdfObjectName
	Colorants   core.PdfObject
	Process     core.PdfObject
	MixingHints core.PdfObject
	container   *core.PdfIndirectObject
}

func newPdfCIDFontType0FromCommon(fc *fontCommon) *pdfCIDFontType0 {
	return &pdfCIDFontType0{fontCommon: *fc}
}

// NewPdfOutlineItem returns an initialized PdfOutlineItem.
func NewPdfOutlineItem() *PdfOutlineItem {
	item := &PdfOutlineItem{primitive: core.MakeIndirectObject(core.MakeDict())}
	item.context = item
	return item
}

// ToPdfObject returns the PDF representation of the shading dictionary.
func (s *PdfShadingType5) ToPdfObject() core.PdfObject {
	s.PdfShading.ToPdfObject()
	shadingDict, err := s.getShadingDict()
	if err != nil {
		common.Log.Error("Unable to access shading dict")
		return nil
	}
	if s.BitsPerCoordinate != nil {
		shadingDict.Set("BitsPerCoordinate", s.BitsPerCoordinate)
	}
	if s.BitsPerComponent != nil {
		shadingDict.Set("BitsPerComponent", s.BitsPerComponent)
	}
	if s.VerticesPerRow != nil {
		shadingDict.Set("VerticesPerRow", s.VerticesPerRow)
	}
	if s.Decode != nil {
		shadingDict.Set("Decode", s.Decode)
	}
	if s.Function != nil {
		if len(s.Function) == 1 {
			shadingDict.Set("Function", s.Function[0].ToPdfObject())
		} else {
			array := core.MakeArray()
			for _, function := range s.Function {
				array.Append(function.ToPdfObject())
			}
			shadingDict.Set("Function", array)
		}
	}
	return s.primitive
}

// ToPdfObject converts date to a PDF string object.
func (d *PdfDate) ToPdfObject() core.PdfObject {
	s := fmt.Sprintf("D:%.4d%.2d%.2d%.2d%.2d%.2d%c%.2d'%.2d'", d.year, d.month, d.day, d.hour, d.minute, d.second, d.utOffsetSign, d.utOffsetHours, d.utOffsetMinutes)
	return core.MakeString(s)
}

const (
	ColorGlyphTypeSVG ColorGlyphType = iota
	ColorGlyphTypeBitmap
	ColorGlyphTypeOutline
)

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

func newFontCommonFromPdfObject(obj core.PdfObject) (*core.PdfObjectDictionary, *fontCommon, error) {
	fc := &fontCommon{}
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		fc.objectNumber = indirect.ObjectNumber
	}
	dict, ok := core.GetDict(obj)
	if !ok {
		common.Log.Debug("ERROR: Font not given by a dictionary (%T)", obj)
		return nil, nil, ErrFontNotSupported
	}
	s, ok := core.GetNameVal(dict.Get("Type"))
	if !ok {
		common.Log.Debug("ERROR: Font Incompatibility. Type (Required) missing")
		return nil, nil, ErrRequiredAttributeMissing
	}
	if s != "Font" {
		common.Log.Debug("ERROR: Font Incompatibility. Type=%q. Should be %q.", s, "Font")
		return nil, nil, core.ErrTypeError
	}
	subtype, ok := core.GetNameVal(dict.Get("Subtype"))
	if !ok {
		common.Log.Debug("ERROR: Font Incompatibility. Subtype (Required) missing")
		return nil, nil, ErrRequiredAttributeMissing
	}
	fc.subtype = subtype
	name, ok := core.GetNameVal(dict.Get("Name"))
	if ok {
		fc.name = name
	}
	toUnicode := dict.Get("ToUnicode")
	if toUnicode != nil {
		fc.toUnicode = core.TraceToDirectObject(toUnicode)
		cm, err := toUnicodeCMapFromObject(fc.toUnicode, fc)
		if err != nil {
			return dict, fc, err
		}
		fc.toUnicodeCMap = cm
	} else if subtype == "CIDFontType0" || subtype == "CIDFontType2" {
		cidSystemInfo, err := cmap.NewCIDSystemInfo(dict.Get("CIDSystemInfo"))
		if err != nil {
			return dict, fc, err
		}
		s := fmt.Sprintf("%s-%s-UCS2", cidSystemInfo.Registry, cidSystemInfo.Ordering)
		if cmap.IsPredefinedCMap(s) {
			fc.toUnicodeCMap, err = cmap.LoadPredefinedCMap(s)
			if err != nil {
				common.Log.Debug("WARN: could not load predefined CMap %s: %v", s, err)
			}
		}
	}
	fontDescriptor := dict.Get("FontDescriptor")
	if fontDescriptor != nil {
		desc, err := newPdfFontDescriptorFromPdfObject(fontDescriptor)
		if err != nil {
			common.Log.Debug("ERROR: Bad font descriptor. err=%v", err)
			return dict, fc, err
		}
		fc.fontDescriptor = desc
	}
	if subtype != "Type3" {
		baseFont, ok := core.GetNameVal(dict.Get("BaseFont"))
		if !ok {
			common.Log.Debug("ERROR: Font Incompatibility. BaseFont (Required) missing")
			return dict, fc, ErrRequiredAttributeMissing
		}
		fc.baseFont = baseFont
	}
	return dict, fc, nil
}

// NewPdfReaderWithOptsAt is the explicit-size form of NewPdfReaderWithOpts.
func NewPdfReaderWithOptsAt(r io.ReaderAt, size int64, opts *ReaderOpts) (*PdfReader, error) {
	const trackNameNewPdfReaderWithOpts = "model:NewPdfReaderWithOpts"
	return newPdfReaderTracked(r, size, opts, true, false, trackNameNewPdfReaderWithOpts)
}

func (c *parsedCRL) covers(cert *x509.Certificate) bool {
	idp := c.idp
	if idp == nil {
		return true
	}
	if idp.OnlyContainsAttributeCerts || idp.OnlySomeReasons.BitLength > 0 {
		return false
	}
	ok := cert.BasicConstraintsValid && cert.IsCA
	if idp.OnlyContainsUserCerts && ok || idp.OnlyContainsCACerts && !ok {
		return false
	}
	if len(idp.DistributionPoint.RelativeName.FullBytes) > 0 {
		return false
	}
	if len(idp.DistributionPoint.FullName) == 0 {
		return true
	}
	for _, fullName := range idp.DistributionPoint.FullName {
		if fullName.Class != asn1.ClassContextSpecific || fullName.Tag != 6 {
			continue
		}
		for _, crlDistributionPoint := range cert.CRLDistributionPoints {
			if string(fullName.Bytes) == crlDistributionPoint {
				return true
			}
		}
	}
	return false
}

// NewPdfDateFromTime will create a PdfDate based on the given time
func NewPdfDateFromTime(timeObj time.Time) (PdfDate, error) {
	format := timeObj.Format("-07:00")
	n, _ := strconv.ParseInt(format[1:3], 10, 32)
	count, _ := strconv.ParseInt(format[4:6], 10, 32)
	return PdfDate{year: int64(timeObj.Year()), month: int64(timeObj.Month()), day: int64(timeObj.Day()), hour: int64(timeObj.Hour()), minute: int64(timeObj.Minute()), second: int64(timeObj.Second()), utOffsetSign: format[0], utOffsetHours: n, utOffsetMinutes: count}, nil
}

func (r *PdfReader) newPdfActionHideFromDict(dict *core.PdfObjectDictionary) (*PdfActionHide, error) {
	return &PdfActionHide{T: dict.Get("T"), H: dict.Get("H")}, nil
}

// Sign signs a specific page with a digital signature.
// The signature field parameter must have a valid signature dictionary
// specified by its V field.
func (a *PdfAppender) Sign(pageNum int, field *PdfFieldSignature) error {
	if field == nil {
		return errors.New("signature field cannot be nil")
	}
	sig := field.V
	if sig == nil {
		return errors.New("signature dictionary cannot be nil")
	}
	n := pageNum - 1
	if n < 0 || n > len(a.pages)-1 {
		return fmt.Errorf("page %d not found", pageNum)
	}
	page := a.Reader.PageList[n]
	field.P = page.ToPdfObject()
	if field.T == nil || field.T.String() == "" {
		field.T = core.MakeString(fmt.Sprintf("Signature %d", pageNum))
	}
	page.AddAnnotation(field.PdfAnnotation)
	if a.acroForm == a.roReader.AcroForm {
		a.acroForm = a.Reader.AcroForm
	}
	acroForm := a.acroForm
	if acroForm == nil {
		acroForm = NewPdfAcroForm()
	}
	acroForm.SigFlags = core.MakeInteger(3)
	if acroForm.NeedAppearances != nil {
		acroForm.NeedAppearances = nil
	}
	fields := append(acroForm.AllFields(), field.PdfField)
	acroForm.Fields = &fields
	a.ReplaceAcroForm(acroForm)
	a.UpdatePage(page)
	a.pages[n] = page
	if _, ok := field.V.GetDocMDPPermission(); ok {
		a.perms = NewPermissions(field.V)
	}
	return nil
}

// DecodeArray returns the range of color component values in CalGray colorspace.
func (cs *PdfColorspaceCalGray) DecodeArray() []float64 { return []float64{0.0, 1.0} }

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

// DecodeArray returns the range of color component values in DeviceRGB colorspace.
func (cs *PdfColorspaceDeviceRGB) DecodeArray() []float64 {
	return []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}
}

func decryptEexec(data []byte) []byte {
	const eexecMultiplier = 52845
	const eexecAddend = 22719
	n := 55665
	for _, b := range data[:4] {
		n = (int(b)+n)*eexecMultiplier + eexecAddend
	}
	data2 := make([]byte, len(data)-4)
	for i, b := range data[4:] {
		data2[i] = byte(int(b) ^ n>>8)
		n = (int(b)+n)*eexecMultiplier + eexecAddend
	}
	return data2
}

// PdfFieldText represents a text field where user can enter text.
type PdfFieldText struct {
	*PdfField
	DA     *core.PdfObjectString
	Q      *core.PdfObjectInteger
	DS     *core.PdfObjectString
	RV     core.PdfObject
	MaxLen *core.PdfObjectInteger
}

func (w *PdfWriter) setupObjectStreams(layout *linearizationLayout) (*objectStreamsState, error) {
	objByNum := map[int64]core.PdfObject{}
	for _, object := range w.objects {
		n, _ := objectNumbers(object)
		if n != 0 {
			objByNum[n] = object
		}
	}
	nums, nums2 := partitionObjectNumbers(w, layout, objByNum)
	renumberForObjectStreams, n, count := w.renumberForObjectStreams(layout, nums, nums2)
	objByNum = map[int64]core.PdfObject{}
	for _, object := range w.objects {
		n, _ := objectNumbers(object)
		if n != 0 {
			objByNum[n] = object
		}
	}
	nums3 := make([]int64, 0, len(nums))
	indirectByNum := map[int64]*core.PdfIndirectObject{}
	for _, object := range w.objects {
		num, _ := objectNumbers(object)
		if num <= count {
			continue
		}
		if indirect, ok := object.(*core.PdfIndirectObject); ok {
			nums3 = append(nums3, num)
			indirectByNum[num] = indirect
		}
	}
	sortInt64s(nums3)
	if len(nums3) > 0xFFFF {
		return nil, fmt.Errorf("linearization: %d compressible objects exceed the single object-stream index limit (65535); object-stream splitting is not yet supported", len(nums3))
	}
	indirects := make([]*core.PdfIndirectObject, 0, len(nums3))
	for _, num := range nums3 {
		indirects = append(indirects, indirectByNum[num])
	}
	objectStreams := core.MakeObjectStreams()
	for _, indirect := range indirects {
		objectStreams.Append(indirect)
	}
	objectStreams.ObjectNumber = renumberForObjectStreams
	set := map[int64]struct{}{}
	for _, num := range nums3 {
		set[num] = struct{}{}
	}
	nums4 := make([]int64, 0, len(layout.mainSection))
	for _, mainSection := range layout.mainSection {
		if _, ok := set[mainSection]; ok {
			continue
		}
		nums4 = append(nums4, mainSection)
	}
	nums4 = append(nums4, renumberForObjectStreams)
	layout.mainSection = nums4
	objByNum = map[int64]core.PdfObject{}
	for _, object := range w.objects {
		n, _ := objectNumbers(object)
		if n != 0 {
			objByNum[n] = object
		}
	}
	objs := make([]core.PdfObject, 0, len(layout.firstSection)+len(layout.mainSection))
	for _, firstSection := range layout.firstSection {
		if obj, ok := objByNum[firstSection]; ok {
			objs = append(objs, obj)
		}
	}
	for _, mainSection := range layout.mainSection {
		if mainSection == renumberForObjectStreams {
			objs = append(objs, objectStreams)
			continue
		}
		if obj, ok := objByNum[mainSection]; ok {
			objs = append(objs, obj)
		}
	}
	layout.orderedObjs = objs
	w.objects = append(w.objects, objectStreams)
	return &objectStreamsState{members: indirects, streams: objectStreams, streamObjNum: renumberForObjectStreams, mainXrefObjNum: n, firstXrefObjNum: count, packedObjNums: nums3}, nil
}

func (w *PdfWriter) addObject(obj core.PdfObject) bool {
	ok := w.hasObject(obj)
	if !ok {
		err := core.ResolveReferencesDeep(obj, w.traversed)
		if err != nil {
			common.Log.Debug("ERROR: %v - skipping", err)
		}
		w.objects = append(w.objects, obj)
		w.hasObjects[obj] = struct{}{}
		return true
	}
	return false
}

// NewStructTreeRoot creates a new structure tree root dictionary.
func NewStructTreeRoot() *StructTreeRoot {
	return &StructTreeRoot{K: []*KDict{}, RoleMap: core.MakeDict(), ParentTreeNextKey: 0}
}

func buildWidthsArray(numByV map[fonts.GID]int, n uint16) *core.PdfObjectArray {
	arr := &core.PdfObjectArray{}
	v := fonts.GID(n)
	for i := fonts.GID(0); i < v; {
		n, ok := numByV[i]
		if !ok {
			i++
			continue
		}
		v2 := i
		for i := v2 + 1; i < v; i++ {
			if count, ok := numByV[i]; !ok || n != count {
				break
			}
			v2 = i
		}
		arr.Append(core.MakeInteger(int64(i)))
		arr.Append(core.MakeInteger(int64(v2)))
		arr.Append(core.MakeInteger(int64(n)))
		i = v2 + 1
	}
	return arr
}

// ToPdfObject returns colorspace in a PDF object format [name dictionary]
func (cs *PdfColorspaceLab) ToPdfObject() core.PdfObject {
	array := core.MakeArray()
	array.Append(core.MakeName("Lab"))
	dict := core.MakeDict()
	if cs.WhitePoint != nil {
		array := core.MakeArray(core.MakeFloat(cs.WhitePoint[0]), core.MakeFloat(cs.WhitePoint[1]), core.MakeFloat(cs.WhitePoint[2]))
		dict.Set("WhitePoint", array)
	} else {
		common.Log.Error("Lab: Missing WhitePoint (Required)")
	}
	if cs.BlackPoint != nil {
		array := core.MakeArray(core.MakeFloat(cs.BlackPoint[0]), core.MakeFloat(cs.BlackPoint[1]), core.MakeFloat(cs.BlackPoint[2]))
		dict.Set("BlackPoint", array)
	}
	if cs.Range != nil {
		array := core.MakeArray(core.MakeFloat(cs.Range[0]), core.MakeFloat(cs.Range[1]), core.MakeFloat(cs.Range[2]), core.MakeFloat(cs.Range[3]))
		dict.Set("Range", array)
	}
	array.Append(dict)
	if cs.container != nil {
		cs.container.PdfObject = array
		return cs.container
	}
	return array
}

// NewPdfAppender creates a new Pdf appender from a Pdf reader. The appender
// performs incremental writes over the original bytes and therefore requires
// a seekable source — the reader must have been constructed from a source
// that implements both io.ReadSeeker and io.ReaderAt (e.g. *os.File,
// *bytes.Reader).
func NewPdfAppender(reader *PdfReader) (*PdfAppender, error) {
	if reader.readSeeker == nil {
		return nil, errors.New("appender requires the reader's source to also implement io.ReadSeeker (e.g. *os.File, *bytes.Reader)")
	}
	r, ok := reader.readSeeker.(io.ReaderAt)
	if !ok {
		return nil, errors.New("appender requires a source that implements both io.ReadSeeker and io.ReaderAt")
	}
	appender := &PdfAppender{readSeeker: reader.readSeeker, Reader: reader, parser: reader.parser, traversed: reader.traversed}
	seek, err := appender.readSeeker.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	appender.originalSize = seek
	if _, err = appender.readSeeker.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	appender.roReader, err = newAppenderSnapshotReader(r, seek, inheritReaderOpts(reader, nil), false)
	if err != nil {
		return nil, err
	}
	for _, objectNum := range appender.Reader.GetObjectNums() {
		if appender.greatestObjNum < objectNum {
			appender.greatestObjNum = objectNum
		}
	}
	appender.xrefs = appender.parser.GetXrefTable()
	appender.xrefOffset = appender.parser.GetXrefOffset()
	appender.pages = append(appender.pages, appender.roReader.PageList...)
	appender.hasNewObject = make(map[core.PdfObject]struct{})
	appender.objNumMap = make(map[core.PdfObject]int64)
	appender.skipObjects = make(map[core.PdfObject]struct{})
	appender.acroForm = appender.roReader.AcroForm
	appender.dss = appender.roReader.DSS
	return appender, nil
}

func (w *PdfWriter) serializeForLinearization(layout *linearizationLayout) ([]preserializedObject, error) {
	preObjs := make([]preserializedObject, 0, len(layout.orderedObjs))
	for _, orderedObj := range layout.orderedObjs {
		n, count := objectNumbers(orderedObj)
		if n == 0 {
			return nil, fmt.Errorf("linearization: object %T has no assigned object number", orderedObj)
		}
		if w.crypter != nil && orderedObj != w.encryptObj {
			if err := w.crypter.Encrypt(orderedObj, n, count); err != nil {
				return nil, fmt.Errorf("linearization: encrypt obj %d: %w", n, err)
			}
		}
		data, err := serializeObject(int(n), orderedObj, w.crypter)
		if err != nil {
			return nil, err
		}
		if stream, ok := orderedObj.(*core.PdfObjectStream); ok && stream.Lazy {
			w.lazyStreams = append(w.lazyStreams, stream)
		}
		preObjs = append(preObjs, preserializedObject{objNum: n, genNum: count, obj: orderedObj, data: data})
	}
	return preObjs, nil
}

const (
	QuaddingLeft     Quadding = 0
	QuaddingCentered Quadding = 1
	QuaddingRight    Quadding = 2
)

func (r *PdfReader) newPdfAnnotationFromIndirectObject(indirect *core.PdfIndirectObject) (*PdfAnnotation, error) {
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return nil, fmt.Errorf("annotation indirect object not containing a dictionary")
	}
	if model := r.modelManager.GetModelFromPrimitive(dict); model != nil {
		annot, ok := model.(*PdfAnnotation)
		if !ok {
			return nil, fmt.Errorf("cached model not a PDF annotation")
		}
		return annot, nil
	}
	annot := &PdfAnnotation{}
	annot.primitive = indirect
	r.modelManager.Register(dict, annot)
	if obj := dict.Get("Type"); obj != nil {
		name, ok := obj.(*core.PdfObjectName)
		if !ok {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Incompatibility! Invalid type of Type (%T) - should be Name", obj)
			}
		} else {
			if *name != "Annot" {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Unsuspected Type != Annot (%s)", *name)
				}
			}
		}
	}
	if rect := dict.Get("Rect"); rect != nil {
		annot.Rect = rect
	}
	if contents := dict.Get("Contents"); contents != nil {
		annot.Contents = contents
	}
	if p := dict.Get("P"); p != nil {
		annot.P = p
	}
	if nm := dict.Get("NM"); nm != nil {
		annot.NM = nm
	}
	if m := dict.Get("M"); m != nil {
		annot.M = m
	}
	if f := dict.Get("F"); f != nil {
		annot.F = f
	}
	if ap := dict.Get("AP"); ap != nil {
		annot.AP = ap
	}
	if as := dict.Get("AS"); as != nil {
		annot.AS = as
	}
	if border := dict.Get("Border"); border != nil {
		annot.Border = border
	}
	if c := dict.Get("C"); c != nil {
		annot.C = c
	}
	if structParent := dict.Get("StructParent"); structParent != nil {
		annot.StructParent = structParent
	}
	if oc := dict.Get("OC"); oc != nil {
		annot.OC = oc
	}
	subtype := dict.Get("Subtype")
	if subtype == nil {
		common.Log.Debug("WARNING: Compatibility issue - annotation Subtype missing - assuming no subtype")
		annot.context = nil
		return annot, nil
	}
	name, found := subtype.(*core.PdfObjectName)
	if !found {
		common.Log.Debug("ERROR: Invalid Subtype object type != name (%T)", subtype)
		return nil, fmt.Errorf("invalid Subtype object type != name (%T)", subtype)
	}
	switch *name {
	case "Text":
		text, err := r.newPdfAnnotationTextFromDict(dict)
		if err != nil {
			return nil, err
		}
		text.PdfAnnotation = annot
		annot.context = text
		return annot, nil
	case "Link":
		link, err := r.newPdfAnnotationLinkFromDict(dict)
		if err != nil {
			return nil, err
		}
		link.PdfAnnotation = annot
		annot.context = link
		return annot, nil
	case "FreeText":
		freeText, err := r.newPdfAnnotationFreeTextFromDict(dict)
		if err != nil {
			return nil, err
		}
		freeText.PdfAnnotation = annot
		annot.context = freeText
		return annot, nil
	case "Line":
		line, err := r.newPdfAnnotationLineFromDict(dict)
		if err != nil {
			return nil, err
		}
		line.PdfAnnotation = annot
		annot.context = line
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("LINE ANNOTATION: annot (%T): %+v\n", annot, annot)
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("LINE ANNOTATION: ctx (%T): %+v\n", line, line)
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("LINE ANNOTATION Markup: ctx (%T): %+v\n", line.PdfAnnotationMarkup, line.PdfAnnotationMarkup)
		}
		return annot, nil
	case "Square":
		square, err := r.newPdfAnnotationSquareFromDict(dict)
		if err != nil {
			return nil, err
		}
		square.PdfAnnotation = annot
		annot.context = square
		return annot, nil
	case "Circle":
		circle, err := r.newPdfAnnotationCircleFromDict(dict)
		if err != nil {
			return nil, err
		}
		circle.PdfAnnotation = annot
		annot.context = circle
		return annot, nil
	case "Polygon":
		polygon, err := r.newPdfAnnotationPolygonFromDict(dict)
		if err != nil {
			return nil, err
		}
		polygon.PdfAnnotation = annot
		annot.context = polygon
		return annot, nil
	case "PolyLine":
		polyLine, err := r.newPdfAnnotationPolyLineFromDict(dict)
		if err != nil {
			return nil, err
		}
		polyLine.PdfAnnotation = annot
		annot.context = polyLine
		return annot, nil
	case "Highlight":
		highlight, err := r.newPdfAnnotationHighlightFromDict(dict)
		if err != nil {
			return nil, err
		}
		highlight.PdfAnnotation = annot
		annot.context = highlight
		return annot, nil
	case "Underline":
		underline, err := r.newPdfAnnotationUnderlineFromDict(dict)
		if err != nil {
			return nil, err
		}
		underline.PdfAnnotation = annot
		annot.context = underline
		return annot, nil
	case "Squiggly":
		squiggly, err := r.newPdfAnnotationSquigglyFromDict(dict)
		if err != nil {
			return nil, err
		}
		squiggly.PdfAnnotation = annot
		annot.context = squiggly
		return annot, nil
	case "StrikeOut":
		strikeOut, err := r.newPdfAnnotationStrikeOut(dict)
		if err != nil {
			return nil, err
		}
		strikeOut.PdfAnnotation = annot
		annot.context = strikeOut
		return annot, nil
	case "Caret":
		caret, err := r.newPdfAnnotationCaretFromDict(dict)
		if err != nil {
			return nil, err
		}
		caret.PdfAnnotation = annot
		annot.context = caret
		return annot, nil
	case "Stamp":
		stamp, err := r.newPdfAnnotationStampFromDict(dict)
		if err != nil {
			return nil, err
		}
		stamp.PdfAnnotation = annot
		annot.context = stamp
		return annot, nil
	case "Ink":
		ink, err := r.newPdfAnnotationInkFromDict(dict)
		if err != nil {
			return nil, err
		}
		ink.PdfAnnotation = annot
		annot.context = ink
		return annot, nil
	case "Popup":
		popup, err := r.newPdfAnnotationPopupFromDict(dict)
		if err != nil {
			return nil, err
		}
		popup.PdfAnnotation = annot
		annot.context = popup
		return annot, nil
	case "FileAttachment":
		fileAttachment, err := r.newPdfAnnotationFileAttachmentFromDict(dict)
		if err != nil {
			return nil, err
		}
		fileAttachment.PdfAnnotation = annot
		annot.context = fileAttachment
		return annot, nil
	case "Sound":
		sound, err := r.newPdfAnnotationSoundFromDict(dict)
		if err != nil {
			return nil, err
		}
		sound.PdfAnnotation = annot
		annot.context = sound
		return annot, nil
	case "RichMedia":
		richMedia, err := r.newPdfAnnotationRichMediaFromDict(dict)
		if err != nil {
			return nil, err
		}
		richMedia.PdfAnnotation = annot
		annot.context = richMedia
		return annot, nil
	case "Movie":
		movie, err := r.newPdfAnnotationMovieFromDict(dict)
		if err != nil {
			return nil, err
		}
		movie.PdfAnnotation = annot
		annot.context = movie
		return annot, nil
	case "Screen":
		screen, err := r.newPdfAnnotationScreenFromDict(dict)
		if err != nil {
			return nil, err
		}
		screen.PdfAnnotation = annot
		annot.context = screen
		return annot, nil
	case "Widget":
		widget, err := r.newPdfAnnotationWidgetFromDict(dict)
		if err != nil {
			return nil, err
		}
		widget.PdfAnnotation = annot
		annot.context = widget
		return annot, nil
	case "PrinterMark":
		printerMark, err := r.newPdfAnnotationPrinterMarkFromDict(dict)
		if err != nil {
			return nil, err
		}
		printerMark.PdfAnnotation = annot
		annot.context = printerMark
		return annot, nil
	case "TrapNet":
		trapNet, err := r.newPdfAnnotationTrapNetFromDict(dict)
		if err != nil {
			return nil, err
		}
		trapNet.PdfAnnotation = annot
		annot.context = trapNet
		return annot, nil
	case "Watermark":
		watermark, err := r.newPdfAnnotationWatermarkFromDict(dict)
		if err != nil {
			return nil, err
		}
		watermark.PdfAnnotation = annot
		annot.context = watermark
		return annot, nil
	case "3D":
		annot3D, err := r.newPdfAnnotation3DFromDict(dict)
		if err != nil {
			return nil, err
		}
		annot3D.PdfAnnotation = annot
		annot.context = annot3D
		return annot, nil
	case "Projection":
		projection, err := r.newPdfAnnotationProjectionFromDict(dict)
		if err != nil {
			return nil, err
		}
		projection.PdfAnnotation = annot
		annot.context = projection
		return annot, nil
	case "Redact":
		redact, err := r.newPdfAnnotationRedactFromDict(dict)
		if err != nil {
			return nil, err
		}
		redact.PdfAnnotation = annot
		annot.context = redact
		return annot, nil
	}
	common.Log.Debug("ERROR: Ignoring unknown annotation: %s", *name)
	return nil, nil
}

// FillWithAppearance populates `form` with values provided by `provider`.
// If not nil, `appGen` is used to generate appearance dictionaries for the
// field annotations, based on the specified settings. Otherwise, appearance
// generation is skipped.
// e.g.: appGen := annotator.FieldAppearance{OnlyIfMissing: true, RegenerateTextFields: true}
// NOTE: In next major version this functionality will be part of Fill. (v4)
func (a *PdfAcroForm) FillWithAppearance(provider FieldValueProvider, appGen FieldAppearanceGenerator) error {
	err := a.fill(provider, appGen)
	if err != nil {
		return err
	}
	if _, ok := provider.(FieldImageProvider); ok {
		err = a.fillImageWithAppearance(provider.(FieldImageProvider), appGen)
	}
	return err
}

func (p *linearizedPlan) emit(w io.Writer) error {
	for _, data := range [][]byte{p.header, p.linDict, p.firstXref, p.hintStream, p.firstSection, p.mainSection, p.mainXref} {
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}

// NewPdfPage returns a new PDF page.
func NewPdfPage() *PdfPage {
	page := PdfPage{}
	page.mu = &sync.Mutex{}
	page.pageDict = core.MakeDict()
	page.Resources = NewPdfPageResources()
	indirect := core.PdfIndirectObject{}
	indirect.PdfObject = page.pageDict
	page.primitive = &indirect
	page.originalDict = page.pageDict.ShallowCopy()
	return &page
}

// NewPdfColorPattern returns an empty color pattern.
func NewPdfColorPattern() *PdfColorPattern { patternColor := &PdfColorPattern{}; return patternColor }

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationFileAttachment) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("FileAttachment"))
	dict.SetIfNotNil("FS", a.FS)
	dict.SetIfNotNil("Name", a.Name)
	return indirect
}

// ToPdfObject returns the PDF representation of the shading dictionary.
func (s *PdfShadingType7) ToPdfObject() core.PdfObject {
	s.PdfShading.ToPdfObject()
	shadingDict, err := s.getShadingDict()
	if err != nil {
		common.Log.Error("Unable to access shading dict")
		return nil
	}
	if s.BitsPerCoordinate != nil {
		shadingDict.Set("BitsPerCoordinate", s.BitsPerCoordinate)
	}
	if s.BitsPerComponent != nil {
		shadingDict.Set("BitsPerComponent", s.BitsPerComponent)
	}
	if s.BitsPerFlag != nil {
		shadingDict.Set("BitsPerFlag", s.BitsPerFlag)
	}
	if s.Decode != nil {
		shadingDict.Set("Decode", s.Decode)
	}
	if s.Function != nil {
		if len(s.Function) == 1 {
			shadingDict.Set("Function", s.Function[0].ToPdfObject())
		} else {
			array := core.MakeArray()
			for _, function := range s.Function {
				array.Append(function.ToPdfObject())
			}
			shadingDict.Set("Function", array)
		}
	}
	return s.primitive
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationRedact) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Redact"))
	dict.SetIfNotNil("QuadPoints", a.QuadPoints)
	dict.SetIfNotNil("IC", a.IC)
	dict.SetIfNotNil("RO", a.RO)
	dict.SetIfNotNil("OverlayText", a.OverlayText)
	dict.SetIfNotNil("Repeat", a.Repeat)
	dict.SetIfNotNil("DA", a.DA)
	dict.SetIfNotNil("Q", a.Q)
	return indirect
}

func (p *PdfPage) removeXObj(dict *core.PdfObjectDictionary, name core.PdfObjectName) {
	dict.Remove(name)
	flateEncoder := core.NewFlateEncoder()
	contentStreamObjs := p.GetContentStreamObjs()
	array := core.MakeArray()
	for _, contentStreamObj := range contentStreamObjs {
		s, _ := pdfObjectToString(contentStreamObj)
		if strings.Contains(s, name.String()) {
			index := strings.Index(s, name.String())
			lastIndex := strings.LastIndex(s[:index], "q")
			n := strings.Index(s[index:], "Q") + index + 1
			if lastIndex >= 0 && n >= 0 {
				var builder strings.Builder
				builder.WriteString(s[:lastIndex])
				builder.WriteString(s[n:])
				stream, err := core.MakeStream([]byte(builder.String()), flateEncoder)
				if err == nil {
					array.Append(stream)
					continue
				}
			}
		}
		array.Append(contentStreamObj)
	}
	p.Contents = array
}

func (r *PdfReader) newPdfActionGotoFromDict(dict *core.PdfObjectDictionary) (*PdfActionGoTo, error) {
	return &PdfActionGoTo{D: dict.Get("D")}, nil
}

// NewPdfActionTrans returns a new "trans" action.
func NewPdfActionTrans() *PdfActionTrans {
	action := NewPdfAction()
	transAction := &PdfActionTrans{}
	transAction.PdfAction = action
	action.SetContext(transAction)
	return transAction
}

func findHoldReport(answers []ocspAnswer, crls []crlEvidence, cert *x509.Certificate, t time.Time) (holdReport, bool) {
	var report holdReport
	ok := false
	fn := func(report2 holdReport) {
		if !ok || report.precedes(report2.thisUpdate, report2.crl) {
			report, ok = report2, true
		}
	}
	for _, answer := range answers {
		if answer.Status == ocsp.Revoked && answer.RevocationReason == ocsp.CertificateHold && !answer.RevokedAt.After(t) {
			fn(holdReport{revokedAt: answer.RevokedAt, thisUpdate: answer.ThisUpdate})
		}
	}
	for _, crl := range crls {
		if entryFor := crl.entryFor(cert); entryFor != nil && entryFor.ReasonCode == crlReasonCertificateHold && !entryFor.RevocationTime.After(t) {
			fn(holdReport{revokedAt: entryFor.RevocationTime, thisUpdate: crl.ThisUpdate, crl: crl.parsedCRL})
		}
	}
	return report, ok
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

// GetContainingPdfObject returns the container of the DSS (indirect object).
func (d *DSS) GetContainingPdfObject() core.PdfObject { return d.primitive }

// AddMCIDChild adds a child MCID object.
func (k *KDict) AddMCIDChild(mcid int) {
	k.children = append(k.children, &KValue{mcid: &mcid})
}

var (
	CourierName              = fonts.CourierName
	CourierBoldName          = fonts.CourierBoldName
	CourierObliqueName       = fonts.CourierObliqueName
	CourierBoldObliqueName   = fonts.CourierBoldObliqueName
	HelveticaName            = fonts.HelveticaName
	HelveticaBoldName        = fonts.HelveticaBoldName
	HelveticaObliqueName     = fonts.HelveticaObliqueName
	HelveticaBoldObliqueName = fonts.HelveticaBoldObliqueName
	SymbolName               = fonts.SymbolName
	ZapfDingbatsName         = fonts.ZapfDingbatsName
	TimesRomanName           = fonts.TimesRomanName
	TimesBoldName            = fonts.TimesBoldName
	TimesItalicName          = fonts.TimesItalicName
	TimesBoldItalicName      = fonts.TimesBoldItalicName
)

// NewPdfActionHide returns a new "hide" action.
func NewPdfActionHide() *PdfActionHide {
	action := NewPdfAction()
	hideAction := &PdfActionHide{}
	hideAction.PdfAction = action
	action.SetContext(hideAction)
	return hideAction
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
		floor := int(math.Floor(val))
		if floor > f.Size[i]-2 {
			floor = f.Size[i] - 2
		}
		if floor < 0 {
			floor = 0
		}
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

// SetOptimizer sets the optimizer to optimize PDF before writing.
func (w *PdfWriter) SetOptimizer(optimizer Optimizer) { w.optimizer = optimizer }

// ToPdfObject implements interface PdfModel.
// Note: Call the sub-annotation's ToPdfObject to set both the generic and non-generic information.
func (a *PdfAnnotation) ToPdfObject() core.PdfObject {
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.Clear()
	dict.Set("Type", core.MakeName("Annot"))
	dict.SetIfNotNil("Rect", a.Rect)
	dict.SetIfNotNil("Contents", a.Contents)
	dict.SetIfNotNil("P", a.P)
	dict.SetIfNotNil("NM", a.NM)
	dict.SetIfNotNil("M", a.M)
	dict.SetIfNotNil("F", a.F)
	dict.SetIfNotNil("AP", a.AP)
	dict.SetIfNotNil("AS", a.AS)
	dict.SetIfNotNil("Border", a.Border)
	dict.SetIfNotNil("C", a.C)
	dict.SetIfNotNil("StructParent", a.StructParent)
	dict.SetIfNotNil("OC", a.OC)
	return indirect
}

// Direction returns the value of the direction.
func (vp *ViewerPreferences) Direction() Direction { return vp.direction }

func newPdfShadingType3FromDict(dict *core.PdfObjectDictionary) (*PdfShadingType3, error) {
	shading3 := PdfShadingType3{}
	coords := dict.Get("Coords")
	if coords == nil {
		common.Log.Debug("Required attribute missing: Coords")
		return nil, ErrRequiredAttributeMissing
	}
	arr, ok := coords.(*core.PdfObjectArray)
	if !ok {
		common.Log.Debug("Coords not an array (got %T)", coords)
		return nil, core.ErrTypeError
	}
	if arr.Len() != 6 {
		common.Log.Debug("Coords length not 6 (got %d)", arr.Len())
		return nil, ErrInvalidAttribute
	}
	shading3.Coords = arr
	if domain := dict.Get("Domain"); domain != nil {
		domain = core.TraceToDirectObject(domain)
		arr, ok := domain.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Domain not an array (got %T)", domain)
			return nil, core.ErrTypeError
		}
		shading3.Domain = arr
	}
	coords = dict.Get("Function")
	if coords == nil {
		common.Log.Debug("Required attribute missing:  Function")
		return nil, ErrRequiredAttributeMissing
	}
	shading3.Function = []PdfFunction{}
	if arr, ok := coords.(*core.PdfObjectArray); ok {
		for _, obj := range arr.Elements() {
			function, err := newPdfFunctionFromPdfObject(obj)
			if err != nil {
				common.Log.Debug("Error parsing function: %v", err)
				return nil, err
			}
			shading3.Function = append(shading3.Function, function)
		}
	} else {
		function, err := newPdfFunctionFromPdfObject(coords)
		if err != nil {
			common.Log.Debug("Error parsing function: %v", err)
			return nil, err
		}
		shading3.Function = append(shading3.Function, function)
	}
	if extend := dict.Get("Extend"); extend != nil {
		extend = core.TraceToDirectObject(extend)
		arr, ok := extend.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Matrix not an array (got %T)", extend)
			return nil, core.ErrTypeError
		}
		if arr.Len() != 2 {
			common.Log.Debug("Extend length not 2 (got %d)", arr.Len())
			return nil, ErrInvalidAttribute
		}
		shading3.Extend = arr
	}
	return &shading3, nil
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationLine) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Line"))
	dict.SetIfNotNil("L", a.L)
	dict.SetIfNotNil("BS", a.BS)
	dict.SetIfNotNil("LE", a.LE)
	dict.SetIfNotNil("IC", a.IC)
	dict.SetIfNotNil("LL", a.LL)
	dict.SetIfNotNil("LLE", a.LLE)
	dict.SetIfNotNil("Cap", a.Cap)
	dict.SetIfNotNil("IT", a.IT)
	dict.SetIfNotNil("LLO", a.LLO)
	dict.SetIfNotNil("CP", a.CP)
	dict.SetIfNotNil("Measure", a.Measure)
	dict.SetIfNotNil("CO", a.CO)
	return indirect
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

// GetNumComponents returns the number of color components of the colorspace device.
// Returns 1 for a CalGray device.
func (cs *PdfColorspaceCalGray) GetNumComponents() int { return 1 }

// PdfShadingType4 is a Free-form Gouraud-shaded triangle mesh.
type PdfShadingType4 struct {
	*PdfShading
	BitsPerCoordinate *core.PdfObjectInteger
	BitsPerComponent  *core.PdfObjectInteger
	BitsPerFlag       *core.PdfObjectInteger
	Decode            *core.PdfObjectArray
	Function          []PdfFunction
}

// PdfWriter handles outputing PDF content.
type PdfWriter struct {
	root                 *core.PdfIndirectObject
	pages                *core.PdfIndirectObject
	pageDicts            map[core.PdfObject]struct{}
	pageObjs             []*core.PdfIndirectObject
	objects              []core.PdfObject
	hasObjects           map[core.PdfObject]struct{}
	spareObjs            []*core.PdfIndirectObject
	outlineTree          *PdfOutlineTreeNode
	lazySources          map[*core.PdfObjectStream]*core.PdfObjectStream
	lazyStreams          []*core.PdfObjectStream
	catalog              *core.PdfObjectDictionary
	spareObjects         []core.PdfObject
	infoObj              *core.PdfIndirectObject
	bufWriter            *bufio.Writer
	writePos             int64
	writeErr             error
	crypter              *core.PdfCrypt
	encryptDict          *core.PdfObjectDictionary
	encryptObj           *core.PdfIndirectObject
	ids                  *core.PdfObjectArray
	version              core.Version
	useObjectStreams     *bool
	missingParents       map[core.PdfObject][]*core.PdfObjectDictionary
	acroForm             *PdfAcroForm
	names                *Names
	optimizer            Optimizer
	standardApplier      StandardApplier
	skipObjectCopy       bool
	crossReferences      map[int]crossReference
	startOffset          int64
	ObjNumOffset         int
	appendMode           bool
	prevXrefs            core.XrefTable
	prevXrefOffset       int64
	originalSize         int64
	objNumMap            map[core.PdfObject]int64
	traversed            map[core.PdfObject]struct{}
	trackID              string
	fileName             string
	outputIntents        []*PdfOutputIntent
	useHashBasedID       bool
	id0, id1             string
	linearized           bool
	linearizationOptions LinearizationOptions
	written              bool
	writeFailed          bool
	copied               bool
	encrypted            bool
}

// FieldProperties is a flat, language-friendly snapshot of a form field's
// commonly used attributes. Properties are resolved with parent inheritance
// applied (Ff, V, DV, DA, Q, MaxLen), so callers do not need to walk the
// field tree.
//
// AcroForm-level /DA and /Q defaults are intentionally not consulted in this
// version; most production forms set DA at the field level.
type FieldProperties struct {
	// Identity.
	FullName      string
	PartialName   string
	AlternateName string
	MappingName   string
	Type          FieldType

	// Value.
	Value        string
	DefaultValue string

	// Common flags (resolved with inheritance).
	ReadOnly bool
	Required bool
	NoExport bool

	// Text-field flags.
	Multiline       bool
	Password        bool
	FileSelect      bool
	DoNotScroll     bool
	Comb            bool
	RichText        bool
	DoNotSpellCheck bool

	// Choice-field flags.
	Combo             bool
	Edit              bool
	Sort              bool
	MultiSelect       bool
	CommitOnSelChange bool

	// Button-field flags.
	PushButton     bool
	Radio          bool
	NoToggleToOff  bool
	RadiosInUnison bool

	// Variable-text properties (parsed from inherited DA).
	FontName  string
	FontSize  float64
	TextColor PdfColor
	Alignment Quadding

	// Text-specific.
	MaxLength int

	// Choice-specific.
	Options []string

	// Widget appearance characteristics, taken from /MK on the first widget
	// annotation. Nil colors / empty strings when /MK is absent. For
	// multi-widget fields callers can use Widgets to inspect each one.
	BorderColor     PdfColor
	BackgroundColor PdfColor
	Caption         string
	Rotation        int

	// Per-widget snapshots for fields with more than one annotation
	// (radio groups, multi-page widgets, etc.).
	Widgets []WidgetProperties
}

// PdfReader represents a PDF file reader. It is a frontend to the lower level parsing mechanism and provides
// a higher level access to work with PDF structure and information, such as the page structure etc.
type PdfReader struct {
	parser         *core.PdfParser
	root           core.PdfObject
	pagesObj       *core.PdfIndirectObject
	pagesDict      *core.PdfObjectDictionary
	pageObjs       []*core.PdfIndirectObject
	PageList       []*PdfPage
	pageCount      int
	catalog        *core.PdfObjectDictionary
	outlineTree    *PdfOutlineTreeNode
	AcroForm       *PdfAcroForm
	DSS            *DSS
	Rotate         *int64
	perms          *Permissions
	revisionCache  map[*PdfReader]*PdfReader
	revisions      []*PdfReader
	modelManager   *modelManager
	lazy           bool
	skipOutlines   bool
	skipPerms      bool
	traversed      map[core.PdfObject]struct{}
	mu             sync.Mutex
	readSeeker     io.ReadSeeker
	trackID        string
	complianceMode bool
	fileName       string
	opts           *ReaderOpts
	autoDecrypt    bool
}

// SetFilter sets compression filter. Decodes with current filter sets and
// encodes the data with the new filter.
func (x *XObjectImage) SetFilter(encoder core.StreamEncoder) error {
	stream := x.Stream
	data, err := x.Filter.DecodeBytes(stream)
	if err != nil {
		return err
	}
	x.Filter = encoder
	encoder.UpdateParams(x.getParamsDict())
	stream, err = encoder.EncodeBytes(data)
	if err != nil {
		return err
	}
	x.Stream = stream
	return nil
}

// ToPdfObject returns a PDF object representation of the ViewerPreferences.
func (vp *ViewerPreferences) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	if vp.hideToolbar != nil {
		dict.Set("HideToolbar", core.MakeBool(*vp.hideToolbar))
	}
	if vp.hideMenubar != nil {
		dict.Set("HideMenubar", core.MakeBool(*vp.hideMenubar))
	}
	if vp.hideWindowUI != nil {
		dict.Set("HideWindowUI", core.MakeBool(*vp.hideWindowUI))
	}
	if vp.fitWindow != nil {
		dict.Set("FitWindow", core.MakeBool(*vp.fitWindow))
	}
	if vp.centerWindow != nil {
		dict.Set("CenterWindow", core.MakeBool(*vp.centerWindow))
	}
	if vp.displayDocTitle != nil {
		dict.Set("DisplayDocTitle", core.MakeBool(*vp.displayDocTitle))
	}
	if vp.nonFullScreenPageMode != "" {
		dict.Set("NonFullScreenPageMode", core.MakeName(string(vp.nonFullScreenPageMode)))
	}
	if vp.direction != "" {
		dict.Set("Direction", core.MakeName(string(vp.direction)))
	}
	if vp.viewArea != "" {
		dict.Set("ViewArea", core.MakeName(string(vp.viewArea)))
	}
	if vp.viewClip != "" {
		dict.Set("ViewClip", core.MakeName(string(vp.viewClip)))
	}
	if vp.printArea != "" {
		dict.Set("PrintArea", core.MakeName(string(vp.printArea)))
	}
	if vp.printClip != "" {
		dict.Set("PrintClip", core.MakeName(string(vp.printClip)))
	}
	if vp.printScaling != "" {
		dict.Set("PrintScaling", core.MakeName(string(vp.printScaling)))
	}
	if vp.duplex != "" {
		dict.Set("Duplex", core.MakeName(string(vp.duplex)))
	}
	if vp.pickTrayByPDFSize != nil {
		dict.Set("PickTrayByPDFSize", core.MakeBool(*vp.pickTrayByPDFSize))
	}
	if vp.printPageRange != nil {
		dict.Set("PrintPageRange", core.MakeArrayFromIntegers(vp.printPageRange))
	}
	if vp.numCopies != 0 {
		dict.Set("NumCopies", core.MakeInteger(int64(vp.numCopies)))
	}
	return dict
}

// GetCatalogViewerPreferences gets catalog ViewerPreferences object.
func (r *PdfReader) GetCatalogViewerPreferences() (core.PdfObject, bool) {
	if r.catalog == nil {
		return nil, false
	}
	viewerPreferences := r.catalog.Get("ViewerPreferences")
	return viewerPreferences, viewerPreferences != nil
}

// PdfAnnotationUnderline represents Underline annotations.
// (Section 12.5.6.10).
type PdfAnnotationUnderline struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	QuadPoints core.PdfObject
}

// WriteLinearized writes a linearized PDF to the given WriteSeeker. This is
// equivalent to SetLinearized(true) followed by Write — the WriteSeeker is
// accepted so a future zero-copy variant can patch in place; v1 buffers
// internally regardless.
//
// Memory: v1 holds the document in memory roughly three times over at peak —
// the live object graph, the per-object serialized bytes
// (serializeForLinearization), and the assembled first/main section buffers
// during plan convergence. Peak RSS is therefore on the order of 3x the
// output file size. This is a known v1 limitation; the io.WriteSeeker
// signature reserves a future streaming/zero-copy path.
func (w *PdfWriter) WriteLinearized(ws io.WriteSeeker) error {
	w.linearized = true
	return w.Write(ws)
}

// ToPdfObject generates a PdfObject representation of the Names struct.
func (n *Names) ToPdfObject() core.PdfObject {
	indirect := n.container
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("Invalid object")
		return indirect
	}
	if n.EmbeddedFiles != nil {
		dict.Set("EmbeddedFiles", core.MakeIndirectObject(n.EmbeddedFiles))
	}
	return indirect
}

// Insert adds an outline item as a child of the current outline item,
// at the specified index.
func (o *OutlineItem) Insert(index uint, item *OutlineItem) {
	n := uint(len(o.Entries))
	if index > n {
		index = n
	}
	o.Entries = append(o.Entries[:index], append([]*OutlineItem{item}, o.Entries[index:]...)...)
}

// ToPdfObject returns the PDF representation of the shading pattern.
func (sp *PdfShadingPatternType3) ToPdfObject() core.PdfObject {
	sp.PdfPattern.ToPdfObject()
	dict := sp.getDict()
	if sp.Shading != nil {
		dict.Set("Shading", sp.Shading.ToPdfObject())
	}
	if sp.Matrix != nil {
		dict.Set("Matrix", sp.Matrix)
	}
	if sp.ExtGState != nil {
		dict.Set("ExtGState", sp.ExtGState)
	}
	return sp.primitive
}

// SetDisplayDocTitle sets the value of the displayDocTitle flag.
func (vp *ViewerPreferences) SetDisplayDocTitle(displayDocTitle bool) {
	vp.displayDocTitle = &displayDocTitle
}

// SetOCProperties sets the optional content properties.
func (w *PdfWriter) SetOCProperties(ocProperties core.PdfObject) error {
	catalog := w.catalog
	if ocProperties != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Setting OC Properties...")
		}
		catalog.Set("OCProperties", ocProperties)
		return w.addObjects(ocProperties)
	}
	return nil
}

// NewOutlineDest returns a new outline destination which can be used
// with outline items.
func NewOutlineDest(page int64, x, y float64) OutlineDest {
	return OutlineDest{Page: page, Mode: "XYZ", X: x, Y: y}
}

var fieldDictKeys = map[string]struct{}{"FT": {}, "Kids": {}, "T": {}, "TU": {}, "TM": {}, "Ff": {}, "V": {}, "DV": {}, "AA": {}, "DA": {}, "Q": {}, "DS": {}, "RV": {}}

// PdfShadingType1 is a Function-based shading.
type PdfShadingType1 struct {
	*PdfShading
	Domain   *core.PdfObjectArray
	Matrix   *core.PdfObjectArray
	Function []PdfFunction
}

// ReaderOpts defines options for creating PdfReader instances.
type ReaderOpts struct {
	// Password password of the PDF file encryption.
	// Default: empty ("").
	Password string

	// Certificate and PrivateKey identify the recipient when the document
	// is encrypted with a public-key security handler (/Filter
	// /Adobe.PubSec). Both must be set, and PrivateKey must be an
	// *rsa.PrivateKey (RSA key transport is the only recipient type
	// supported). They are ignored for documents using the standard
	// (password) security handler, so one ReaderOpts can open either kind.
	Certificate *x509.Certificate
	PrivateKey  crypto.PrivateKey

	// LazyLoad set if the PDF file would be loaded using lazy-loading mode.
	// Default: true.
	LazyLoad bool

	// ComplianceMode set if parsed PDF file should contain meta information for the verifiers of the compliance standards like PDF/A.
	ComplianceMode bool

	// RelaxedMode indicates whether the parser should operate in a relaxed mode, ignoring certain insignificant errors and continuing to parse the PDF file.
	// For example, it skips AcroForm loading errors and proceeds with the rest of the file if no form-related processing is required.
	RelaxedMode bool

	// ParserAutoRepair indicates whether the parser should attempt to automatically repair the file if it is found to be corrupted or malformed.
	ParserAutoRepair bool

	// MaxDecodedStreamSize is the maximum number of bytes a single stream, or a single filter stage
	// of a filter chain, may decode to. 0 selects core.DefaultMaxDecodedStreamSize (256 MiB unless
	// changed with core.SetDefaultMaxDecodedStreamSize).
	// Decoding past the limit fails with core.ErrDecodedSizeLimitExceeded.
	// With ComplianceMode the limits apply once the cross-reference data is loaded; the streams
	// read while loading it use core.DefaultMaxDecodedStreamSize. See core.ParserOpts for the
	// decoders the limits do not cover.
	MaxDecodedStreamSize int64

	// MaxDecodedTotalSize is the maximum number of bytes all stream decodes of the document may
	// produce together, repeated decodes of the same stream included. See
	// core.ParserOpts.MaxDecodedTotalSize. 0 disables the limit.
	MaxDecodedTotalSize int64
}

// NewPdfColorCalRGB returns a new CalRBG color.
func NewPdfColorCalRGB(a, b, c float64) *PdfColorCalRGB {
	calRGBColor := PdfColorCalRGB{a, b, c}
	return &calRGBColor
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionHide) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeHide)))
	dict.SetIfNotNil("T", a.T)
	dict.SetIfNotNil("H", a.H)
	return indirect
}

// NewPdfActionSubmitForm returns a new "submit form" action.
func NewPdfActionSubmitForm() *PdfActionSubmitForm {
	action := NewPdfAction()
	submitFormAction := &PdfActionSubmitForm{}
	submitFormAction.PdfAction = action
	action.SetContext(submitFormAction)
	return submitFormAction
}

func (r *PdfReader) newPdfActionGotoEFromDict(dict *core.PdfObjectDictionary) (*PdfActionGoToE, error) {
	filespec, err := newPdfFilespecFromPdfObject(dict.Get("F"))
	if err != nil {
		return nil, err
	}
	return &PdfActionGoToE{D: dict.Get("D"), NewWindow: dict.Get("NewWindow"), T: dict.Get("T"), F: filespec}, nil
}

func (r *SignatureValidationResult) copyCoverageFrom(result *SignatureValidationResult) {
	r.IsCoveringEntireFile = result.IsCoveringEntireFile
	r.IsCoveringEntireRevision = result.IsCoveringEntireRevision
	r.RevisionsAfterSignature = result.RevisionsAfterSignature
	r.ContentsGapOnly = result.ContentsGapOnly
	r.byteRangeChecked = result.byteRangeChecked
}

// C returns the value of the cyan component of the color.
func (c *PdfColorDeviceCMYK) C() float64 { return c[0] }

func (cs *PdfColorspaceICCBased) ImageToRGB(img Image) (Image, error) {
	cs.buildTransform()
	if cs.transformReady && img.ColorComponents == 3 {
		imageToRGBManaged, err := cs.imageToRGBManaged(img)
		if err == nil {
			return imageToRGBManaged, nil
		}
		common.Log.Debug("ICCBased colour-managed conversion failed (%v); using alternate", err)
	}
	if cs.Alternate == nil {
		common.Log.Debug("ICC Based colorspace missing alternative")
		if cs.N == 1 {
			common.Log.Debug("ICC Based colorspace missing alternative - using DeviceGray (N=1)")
			deviceGrayCS := NewPdfColorspaceDeviceGray()
			return deviceGrayCS.ImageToRGB(img)
		} else if cs.N == 3 {
			common.Log.Debug("ICC Based colorspace missing alternative - using DeviceRGB (N=3)")
			return img, nil
		} else if cs.N == 4 {
			common.Log.Debug("ICC Based colorspace missing alternative - using DeviceCMYK (N=4)")
			deviceCMYKCS := NewPdfColorspaceDeviceCMYK()
			return deviceCMYKCS.ImageToRGB(img)
		} else {
			return img, errors.New("ICC Based colorspace missing alternative")
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("ICC Based colorspace with alternative: %#v", cs)
	}
	imageToRGB, err := cs.Alternate.ImageToRGB(img)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("ICC Input image: %+v", img)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("ICC Output image: %+v", imageToRGB)
	}
	return imageToRGB, err
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationPopup) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("Popup"))
	dict.SetIfNotNil("Parent", a.Parent)
	dict.SetIfNotNil("Open", a.Open)
	return indirect
}

// SetDocInfo set document info.
// This will overwrite any globally declared document info.
func (w *PdfWriter) SetDocInfo(info *PdfInfo) { w.setDocInfo(info.ToPdfObject()) }

// ToGoImage converts the OpenPDF Image to a golang Image structure.
func (img *Image) ToGoImage() (image.Image, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Converting to go image")
	}
	img2, err := imageutil.NewImage(int(img.Width), int(img.Height), int(img.BitsPerComponent), img.ColorComponents, img.Data, img.alpha, img.decode)
	if err != nil {
		return nil, err
	}
	return img2, nil
}

func (w *PdfWriter) finishLazyStreams(err error) {
	w.writeFailed = err != nil
	if err == nil {
		for _, lazyStream := range w.lazyStreams {
			removeLazyTempFile(lazyStream.TempFile)
			lazyStream.TempFile = ""
			if lazySource, ok := w.lazySources[lazyStream]; ok {
				lazySource.TempFile = ""
			}
		}
	} else if w.crypter != nil {
		lazystream.Drop(w.crypter)
	}
	w.lazySources, w.lazyStreams = nil, nil
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

// ValidateLinearizationTrailerID runs only the linearization /ID consistency
// check (first-xref trailer vs main trailer), without the full Annex F battery.
// Returns nil if the PDF is not linearized or the /ID values match.
func (r *PdfReader) ValidateLinearizationTrailerID() *core.LinearizationViolation {
	return r.parser.ValidateLinearizationTrailerID()
}

// ColorToRGB only converts color used with uncolored patterns (defined in underlying colorspace).  Does not go into the
// pattern objects and convert those.  If that is desired, needs to be done separately.  See for example
// grayscale conversion example in examples.
func (cs *PdfColorspaceSpecialPattern) ColorToRGB(color PdfColor) (PdfColor, error) {
	patternColor, ok := color.(*PdfColorPattern)
	if !ok {
		common.Log.Debug("Color not pattern (got %T)", color)
		return nil, ErrTypeCheck
	}
	if patternColor.Color == nil {
		return color, nil
	}
	if cs.UnderlyingCS == nil {
		return nil, errors.New("underlying CS not defined")
	}
	return cs.UnderlyingCS.ColorToRGB(patternColor.Color)
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

// DetermineColorspaceNameFromPdfObject determines PDF colorspace from a PdfObject.  Returns the colorspace name and
// an error on failure. If the colorspace was not found, will return an empty string.
func DetermineColorspaceNameFromPdfObject(obj core.PdfObject) (core.PdfObjectName, error) {
	var name *core.PdfObjectName
	var arr *core.PdfObjectArray
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		if arr2, ok := indirect.PdfObject.(*core.PdfObjectArray); ok {
			arr = arr2
		} else if name2, ok := indirect.PdfObject.(*core.PdfObjectName); ok {
			name = name2
		}
	} else if arr2, ok := obj.(*core.PdfObjectArray); ok {
		arr = arr2
	} else if name2, ok := obj.(*core.PdfObjectName); ok {
		name = name2
	}
	if name != nil {
		switch *name {
		case "DeviceGray", "DeviceRGB", "DeviceCMYK":
			return *name, nil
		case "Pattern":
			return *name, nil
		}
	}
	if arr != nil && arr.Len() > 0 {
		if name, ok := arr.Get(0).(*core.PdfObjectName); ok {
			switch *name {
			case "DeviceGray", "DeviceRGB", "DeviceCMYK":
				if arr.Len() == 1 {
					return *name, nil
				}
			case "CalGray", "CalRGB", "Lab":
				return *name, nil
			case "ICCBased", "Pattern", "Indexed":
				return *name, nil
			case "Separation", "DeviceN":
				return *name, nil
			}
		}
	}
	return "", nil
}

// GetPdfInfo returns the PDF info dictionary.
func (r *PdfReader) GetPdfInfo() (*PdfInfo, error) {
	trailer, err := r.GetTrailer()
	if err != nil {
		return nil, err
	}
	var dict *core.PdfObjectDictionary
	info := trailer.Get("Info")
	switch v := info.(type) {
	case *core.PdfObjectReference:
		ref := v
		info, err = r.GetIndirectObjectByNumber(int(ref.ObjectNumber))
		info = core.TraceToDirectObject(info)
		if err != nil {
			return nil, err
		}
		dict, _ = info.(*core.PdfObjectDictionary)
	case *core.PdfObjectDictionary:
		dict = v
	}
	if dict == nil {
		return nil, errors.New("info dictionary not present")
	}
	info2, err := NewPdfInfoFromObject(dict)
	if err != nil {
		return nil, err
	}
	return info2, nil
}

// GetContainingPdfObject returns the container of the resources object (indirect object).
func (r *PdfPageResources) GetContainingPdfObject() core.PdfObject { return r.primitive }

// NS returns the namespace URI.
func (ns *PdfStructureNamespace) NS() string { return ns.uri }

func (img *Image) resampleLowBits(nums []uint32) {
	bytesPerLine := imageutil.BytesPerLine(int(img.Width), int(img.BitsPerComponent), img.ColorComponents)
	acquireBytes := core.AcquireBytes(bytesPerLine * int(img.Height))
	clear(acquireBytes)
	n := img.ColorComponents * int(img.Width)
	var (
		count, num int
		size       uint32
	)
	for i := 0; i < int(img.Height); i++ {
		num = i * bytesPerLine
		b := uint8(8)
		for i := 0; i < n; i++ {
			if count >= len(nums) {
				break
			}
			size = nums[count]
			b -= uint8(img.BitsPerComponent)
			acquireBytes[num] |= byte(size) << b
			if b == 0 {
				b = 8
				num++
			}
			count++
		}
	}
	img.Data = acquireBytes
}
func (cs *PdfColorspaceCalGray) String() string { return "CalGray" }

type pdfFontType3 struct {
	fontCommon
	primitive *core.PdfIndirectObject

	// These fields are specific to Type 3 fonts.
	CharProcs  core.PdfObject
	Encoding   core.PdfObject
	FontBBox   core.PdfObject
	FontMatrix core.PdfObject
	FirstChar  core.PdfObject
	LastChar   core.PdfObject
	Widths     core.PdfObject
	Resources  core.PdfObject
	charWidths map[textencoding.CharCode]float64
	encoder    textencoding.TextEncoder
}

var documentSigningEKUOIDs = []asn1.ObjectIdentifier{{1, 3, 6, 1, 5, 5, 7, 3, 36}, {1, 3, 6, 1, 4, 1, 311, 10, 3, 12}, {1, 2, 840, 113583, 1, 1, 5}}

// NewPdfReaderFromParser builds a PdfReader on top of a pre-built parser.
// Useful for sharing a single parser across goroutines — the parser's
// internal sync.Map cache and mutexes make concurrent reader.GetPage / page
// content access safe.
//
// The resulting reader has no io.ReadSeeker source, so it cannot be passed to
// NewPdfAppender (incremental signing/updates require a seekable source — use
// NewPdfReader for that). Reading existing signatures is supported, since the
// parser reads raw bytes via ReadBytesAt. Encrypted PDFs are not supported.
func NewPdfReaderFromParser(parser *core.PdfParser) (*PdfReader, error) {
	const trackNameNewPdfReaderFromParser = "model:NewPdfReaderFromParser"
	randomID, err := randomIDWithPrefix("r")
	if err != nil {
		return nil, err
	}
	if err := license.Track(randomID, trackNameNewPdfReaderFromParser, ""); err != nil {
		return nil, err
	}
	reader := &PdfReader{parser: parser, traversed: map[core.PdfObject]struct{}{}, modelManager: newModelManager(), lazy: false, opts: NewReaderOpts(), trackID: randomID}
	ok, err := reader.IsEncrypted()
	if err != nil {
		return nil, err
	}
	if ok {
		return nil, errors.New("encrypted PDFs are not supported via NewPdfReaderFromParser")
	}
	if err := reader.loadStructure(); err != nil {
		return nil, err
	}
	reader.revisionCache = make(map[*PdfReader]*PdfReader)
	reader.revisions = make([]*PdfReader, parser.GetRevisionNumber())
	return reader, nil
}

// Equals reports whether other is a DocMDP transform parameters object with the same contents.
func (p *PdfTransformParamsDocMDP) Equals(other core.PdfObject) bool {
	transformParamsDocMDP, ok := other.(*PdfTransformParamsDocMDP)
	if !ok {
		return false
	}
	if p == nil || transformParamsDocMDP == nil {
		return p == nil && transformParamsDocMDP == nil
	}
	return p.ToPdfObject().Equals(transformParamsDocMDP.ToPdfObject())
}
func (cs *PdfColorspaceSpecialSeparation) String() string { return "Separation" }

// NewPdfAnnotationWatermark returns a new watermark annotation.
func NewPdfAnnotationWatermark() *PdfAnnotationWatermark {
	annot := NewPdfAnnotation()
	watermark := &PdfAnnotationWatermark{}
	watermark.PdfAnnotation = annot
	annot.SetContext(watermark)
	return watermark
}

// ButtonType represents the subtype of a button field, can be one of:
// - Checkbox (ButtonTypeCheckbox)
// - PushButton (ButtonTypePushButton)
// - RadioButton (ButtonTypeRadioButton)
type ButtonType int

// ValidateSignatures validates the integrity of the digital signatures in
// the document. No certificate trust is established: every result carries
// IsTrusted false. Use ValidateSignaturesWithOpts with trust roots to decide
// whether the signers are trusted.
func (r *PdfReader) ValidateSignatures(handlers []SignatureHandler) ([]SignatureValidationResult, error) {
	return r.ValidateSignaturesWithOpts(handlers, nil)
}

// PdfColorDeviceGray represents a grayscale color value that shall be represented by a single number in the
// range 0.0 to 1.0 where 0.0 corresponds to black and 1.0 to white.
type PdfColorDeviceGray float64

// AddAnnotation appends `annot` to the list of page annotations.
func (p *PdfPage) AddAnnotation(annot *PdfAnnotation) {
	if p.mu != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
	}
	if p.annotations == nil {
		p.getAnnotationsLocked()
	}
	p.annotations = append(p.annotations, annot)
}

// Components returns the color's component values: the single gray value.
func (c *PdfColorCalGray) Components() []float64 { return []float64{float64(*c)} }

// Encoder returns the font's text encoder.
func (f pdfCIDFontType2) Encoder() textencoding.TextEncoder { return f.encoder }

// NewPdfAnnotationCircle returns a new circle annotation.
func NewPdfAnnotationCircle() *PdfAnnotationCircle {
	annot := NewPdfAnnotation()
	circle := &PdfAnnotationCircle{}
	circle.PdfAnnotation = annot
	circle.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(circle)
	return circle
}

// NewPdfPageResourcesColorspaces returns a new PdfPageResourcesColorspaces object.
func NewPdfPageResourcesColorspaces() *PdfPageResourcesColorspaces {
	pageResourcesColorspaces := &PdfPageResourcesColorspaces{}
	pageResourcesColorspaces.Names = []string{}
	pageResourcesColorspaces.Colorspaces = map[string]PdfColorspace{}
	pageResourcesColorspaces.container = &core.PdfIndirectObject{}
	return pageResourcesColorspaces
}

func (t *trustEvaluator) dssCertificates() []*x509.Certificate {
	if !t.dssCertsLoaded && t.reader.DSS != nil {
		t.dssCertsLoaded = true
		t.dssCerts = parseCertificates(t.reader.DSS.Certs)
	}
	return t.dssCerts
}

// ColorFromFloats returns a new PdfColor based on the input slice of color
// components. The slice should contain three elements representing the
// L (range 0-100), A (range -100-100) and B (range -100-100) components of
// the color.
func (cs *PdfColorspaceLab) ColorFromFloats(vals []float64) (PdfColor, error) {
	if len(vals) != 3 {
		return nil, errors.New("range check")
	}
	val := vals[0]
	if val < 0.0 || val > 100.0 {
		common.Log.Debug("L out of range (got %v should be 0-100)", val)
		return nil, ErrColorOutOfRange
	}
	f := vals[1]
	v := float64(-100)
	x := float64(100)
	if len(cs.Range) > 1 {
		v = cs.Range[0]
		x = cs.Range[1]
	}
	if f < v || f > x {
		common.Log.Debug("A out of range (got %v; range %v to %v)", f, v, x)
		return nil, ErrColorOutOfRange
	}
	y := vals[2]
	y2 := float64(-100)
	y3 := float64(100)
	if len(cs.Range) > 3 {
		y2 = cs.Range[2]
		y3 = cs.Range[3]
	}
	if y < y2 || y > y3 {
		common.Log.Debug("b out of range (got %v; range %v to %v)", y, y2, y3)
		return nil, ErrColorOutOfRange
	}
	labColor := NewPdfColorLab(val, f, y)
	return labColor, nil
}

// GetCatalogVersion gets the catalog /Version entry. Per ISO 32000-2 §7.7.2
// (Table 29) this entry supersedes the file header version only when it is
// later than the header; callers must apply that max rule themselves (this
// accessor just returns the raw entry).
func (r *PdfReader) GetCatalogVersion() (core.PdfObject, bool) {
	if r.catalog == nil {
		return nil, false
	}
	version := r.catalog.Get("Version")
	return version, version != nil
}

func ocspEvidenceFromDER(dataList [][]byte) []ocspEvidence {
	ocsps := make([]ocspEvidence, 0, len(dataList))
	for _, data := range dataList {
		ocsps = append(ocsps, newOCSPEvidence(data, false))
	}
	return ocsps
}

// GetAsShadingPattern returns a shading pattern. Check with IsShading() prior to using this.
func (p *PdfPattern) GetAsShadingPattern() *PdfShadingPattern {
	return p.context.(*PdfShadingPattern)
}

// ToPdfObject returns the PDF representation of the shading dictionary.
func (s *PdfShadingType3) ToPdfObject() core.PdfObject {
	s.PdfShading.ToPdfObject()
	shadingDict, err := s.getShadingDict()
	if err != nil {
		common.Log.Error("Unable to access shading dict")
		return nil
	}
	if s.Coords != nil {
		shadingDict.Set("Coords", s.Coords)
	}
	if s.Domain != nil {
		shadingDict.Set("Domain", s.Domain)
	}
	if s.Function != nil {
		if len(s.Function) == 1 {
			shadingDict.Set("Function", s.Function[0].ToPdfObject())
		} else {
			array := core.MakeArray()
			for _, function := range s.Function {
				array.Append(function.ToPdfObject())
			}
			shadingDict.Set("Function", array)
		}
	}
	if s.Extend != nil {
		shadingDict.Set("Extend", s.Extend)
	}
	return s.primitive
}

func newPdfColorspaceCalGrayFromPdfObject(obj core.PdfObject) (*PdfColorspaceCalGray, error) {
	calGrayCS := NewPdfColorspaceCalGray()
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		calGrayCS.container = indirect
	}
	obj = core.TraceToDirectObject(obj)
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("type error")
	}
	if arr.Len() != 2 {
		return nil, fmt.Errorf("invalid CalGray colorspace")
	}
	obj = core.TraceToDirectObject(arr.Get(0))
	name, ok := obj.(*core.PdfObjectName)
	if !ok {
		return nil, fmt.Errorf("CalGray name not a Name object")
	}
	if *name != "CalGray" {
		return nil, fmt.Errorf("not a CalGray colorspace")
	}
	obj = core.TraceToDirectObject(arr.Get(1))
	dict, ok := obj.(*core.PdfObjectDictionary)
	if !ok {
		return nil, fmt.Errorf("CalGray dict not a Dictionary object")
	}
	obj = dict.Get("WhitePoint")
	obj = core.TraceToDirectObject(obj)
	arr2, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("CalGray: Invalid WhitePoint")
	}
	if arr2.Len() != 3 {
		return nil, fmt.Errorf("CalGray: Invalid WhitePoint array")
	}
	floats, err := arr2.GetAsFloat64Slice()
	if err != nil {
		return nil, err
	}
	calGrayCS.WhitePoint = floats
	obj = dict.Get("BlackPoint")
	if obj != nil {
		obj = core.TraceToDirectObject(obj)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			return nil, fmt.Errorf("CalGray: Invalid BlackPoint")
		}
		if arr.Len() != 3 {
			return nil, fmt.Errorf("CalGray: Invalid BlackPoint array")
		}
		floats, err := arr.GetAsFloat64Slice()
		if err != nil {
			return nil, err
		}
		calGrayCS.BlackPoint = floats
	}
	obj = dict.Get("Gamma")
	if obj != nil {
		obj = core.TraceToDirectObject(obj)
		f, err := core.GetNumberAsFloat(obj)
		if err != nil {
			return nil, fmt.Errorf("CalGray: gamma not a number")
		}
		calGrayCS.Gamma = f
	}
	return calGrayCS, nil
}

// GetDocMDPPermission returns the DocMDP level of the restrictions
func (s *PdfSignature) GetDocMDPPermission() (mdp.DocMDPPermission, bool) {
	for _, obj := range s.Reference.Elements() {
		if dict, ok := core.GetDict(obj); ok {
			if transformMethod, ok := core.GetNameVal(dict.Get("TransformMethod")); ok && transformMethod == "DocMDP" {
				if transformParams, ok := core.GetDict(dict.Get("TransformParams")); ok {
					if P, ok := core.GetIntVal(transformParams.Get("P")); ok {
						return mdp.DocMDPPermission(P), true
					}
				}
			}
		}
	}
	return 0, false
}

// PdfTransformParamsDocMDP represents a transform parameters dictionary for the DocMDP method and is used to detect
// modifications relative to a signature field that is signed by the author of a document.
// (Section 12.8.2.2, Table 254 - Entries in the DocMDP transform parameters dictionary p. 471 in PDF32000_2008).
type PdfTransformParamsDocMDP struct {
	Type *core.PdfObjectName
	P    *core.PdfObjectInteger
	V    *core.PdfObjectName
}

// String returns a string that describes `base`.
func (c fontCommon) String() string { return fmt.Sprintf("FONT{%s}", c.coreString()) }

// Enable LTV enables the specified signature. The signing certificate
// chain is extracted from the signature dictionary. Optionally, additional
// certificates can be specified through the `extraCerts` parameter.
// The LTV client attempts to build the certificate chain up to a trusted root
// by downloading any missing certificates.
func (l *LTV) Enable(sig *PdfSignature, extraCerts []*x509.Certificate) error {
	if err := l.validateSig(sig); err != nil {
		return err
	}
	s, err := signatureContentsDigestHex(sig)
	if err != nil {
		return err
	}
	if _, ok := l.dss.VRI[s]; ok && l.SkipExisting {
		return nil
	}
	certs, err := sig.GetCerts()
	if err != nil {
		return err
	}
	return l.enable(certs, extraCerts, s)
}

// Hasher is the interface that wraps the basic Write method.
type Hasher interface {
	Write(data []byte) (n int, err error)
}

// SetColorspaceByName adds the provided colorspace to the page resources.
func (r *PdfPageResources) SetColorspaceByName(keyName core.PdfObjectName, cs PdfColorspace) error {
	colorspaces, err := r.GetColorspaces()
	if err != nil {
		common.Log.Debug("ERROR getting colorsprace: %v", err)
		return err
	}
	if colorspaces == nil {
		colorspaces = NewPdfPageResourcesColorspaces()
		r.SetColorSpace(colorspaces)
	}
	colorspaces.Set(keyName, cs)
	return nil
}

// PdfActionLaunch represents a launch action.
type PdfActionLaunch struct {
	*PdfAction
	F         *PdfFilespec
	Win       core.PdfObject
	Mac       core.PdfObject
	Unix      core.PdfObject
	NewWindow core.PdfObject
}
type certRevocation struct {
	status    RevocationStatus
	revokedAt time.Time
	onHold    bool
	ocspFound bool
	crlFound  bool
}

func (r *PdfReader) loadPerms() (*Permissions, error) {
	if perms := r.catalog.Get("Perms"); perms != nil {
		if permsDict, ok := core.GetDict(perms); ok {
			docMDP := permsDict.Get("DocMDP")
			if docMDP == nil {
				return nil, nil
			}
			if docMDPIndirect, ok := core.GetIndirect(docMDP); ok {
				sig, err := r.newPdfSignatureFromIndirect(docMDPIndirect)
				if err != nil {
					return nil, err
				}
				return NewPermissions(sig), nil
			}
			return nil, fmt.Errorf("invalid DocMDP entry")
		}
		return nil, fmt.Errorf("invalid Perms entry")
	}
	return nil, nil
}

const (
	FieldFlagClear             FieldFlag = 0
	FieldFlagReadOnly          FieldFlag = 1
	FieldFlagRequired          FieldFlag = (1 << 1)
	FieldFlagNoExport          FieldFlag = (2 << 1)
	FieldFlagNoToggleToOff     FieldFlag = (1 << 14)
	FieldFlagRadio             FieldFlag = (1 << 15)
	FieldFlagPushbutton        FieldFlag = (1 << 16)
	FieldFlagRadiosInUnision   FieldFlag = (1 << 25)
	FieldFlagMultiline         FieldFlag = (1 << 12)
	FieldFlagPassword          FieldFlag = (1 << 13)
	FieldFlagFileSelect        FieldFlag = (1 << 20)
	FieldFlagDoNotScroll       FieldFlag = (1 << 23)
	FieldFlagComb              FieldFlag = (1 << 24)
	FieldFlagRichText          FieldFlag = (1 << 26)
	FieldFlagDoNotSpellCheck   FieldFlag = (1 << 22)
	FieldFlagCombo             FieldFlag = (1 << 17)
	FieldFlagEdit              FieldFlag = (1 << 18)
	FieldFlagSort              FieldFlag = (1 << 19)
	FieldFlagMultiSelect       FieldFlag = (1 << 21)
	FieldFlagCommitOnSelChange FieldFlag = (1 << 27)
)

// Write writes the Appender output to io.Writer.
// It can only be called once and further invocations will result in an error.
func (a *PdfAppender) Write(w io.Writer) error {
	if a.written {
		return errors.New("appender write can only be invoked once")
	}
	writer := NewPdfWriter()
	pagesDict, ok := core.GetDict(writer.pages)
	if !ok {
		return errors.New("invalid Pages obj (not a dict)")
	}
	arr, ok := pagesDict.Get("Kids").(*core.PdfObjectArray)
	if !ok {
		return errors.New("invalid Pages Kids obj (not an array)")
	}
	num, ok := pagesDict.Get("Count").(*core.PdfObjectInteger)
	if !ok {
		return errors.New("invalid Pages Count object (not an integer)")
	}
	parser := a.roReader.parser
	trailer := parser.GetTrailer()
	if trailer == nil {
		return errors.New("missing trailer")
	}
	root, ok := core.GetIndirect(trailer.Get("Root"))
	if !ok {
		return errors.New("catalog container not found")
	}
	rootDict, ok := core.GetDict(root)
	if !ok {
		common.Log.Debug("ERROR: Missing catalog: (root %q) (trailer %s)", root, trailer)
		return errors.New("missing catalog")
	}
	found := false
	for _, signatureField := range a.roReader.AcroForm.signatureFields() {
		if signatureField.Lock != nil {
			found = true
			break
		}
	}
	if found {
		writer.root = root
	}
	for _, name := range rootDict.Keys() {
		if writer.catalog.Get(name) == nil {
			obj := rootDict.Get(name)
			writer.catalog.Set(name, obj)
		}
	}
	if a.acroForm != nil {
		if a.acroForm.direct {
			if obj := a.acroForm.ToPdfObject().Direct(); !core.IsNullObject(obj) {
				writer.catalog.Set("AcroForm", obj)
				a.updateObjectsDeep(obj, nil)
			} else {
				common.Log.Debug("Unable to trace AcroForm object, failed to add AcroForm.")
			}
		} else {
			writer.catalog.Set("AcroForm", a.acroForm.ToPdfObject())
			a.updateObjectsDeep(a.acroForm.ToPdfObject(), nil)
		}
	}
	if a.dss != nil {
		a.updateObjectsDeep(a.dss.ToPdfObject(), nil)
		writer.catalog.Set("DSS", a.dss.GetContainingPdfObject())
	}
	if a.perms != nil {
		writer.catalog.Set("Perms", a.perms.ToPdfObject())
		a.updateObjectsDeep(a.perms.ToPdfObject(), nil)
	}
	if writer.version.Major < 2 {
		writer.AddExtension("ESIC", "1.7", 5)
		writer.AddExtension("ADBE", "1.7", 8)
	}
	if info, ok := core.GetDict(trailer.Get("Info")); ok {
		if infoObjDict, ok := core.GetDict(writer.infoObj); ok {
			for _, name := range info.Keys() {
				if infoObjDict.Get(name) == nil {
					infoObjDict.Set(name, info.Get(name))
				}
			}
		}
	}
	if a.docInfo != nil {
		writer.infoObj = core.MakeIndirectObject(a.docInfo.ToPdfObject())
	}
	a.updateObjectsDeep(writer.infoObj, nil)
	a.updateObjectsDeep(writer.root, nil)
	done := false
	if len(a.roReader.PageList) != len(a.pages) {
		done = true
	} else {
		for i := range a.roReader.PageList {
			switch a.pages[i] {
			case a.roReader.PageList[i]:
			case a.Reader.PageList[i]:
			default:
				done = true
			}
			if done {
				break
			}
		}
	}
	if done {
		a.updateObjectsDeep(writer.pages, nil)
	} else {
		a.skipObjects[writer.pages] = struct{}{}
	}
	writer.pages.ObjectNumber = a.Reader.pagesObj.ObjectNumber
	a.objNumMap[writer.pages] = a.Reader.pagesObj.ObjectNumber
	names := []core.PdfObjectName{"Resources", "MediaBox", "CropBox", "Rotate"}
	for _, page := range a.pages {
		obj := page.ToPdfObject()
		*num = *num + 1
		if indirect, ok := obj.(*core.PdfIndirectObject); ok && indirect.GetParser() == a.roReader.parser {
			arr.Append(&indirect.PdfObjectReference)
			continue
		}
		if dict, isDict := core.GetDict(obj); isDict {
			indirect, isIndirect := dict.Get("Parent").(*core.PdfIndirectObject)
			for isIndirect {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Page Parent: %T", indirect)
				}
				dict2, isDict := indirect.PdfObject.(*core.PdfObjectDictionary)
				if !isDict {
					return errors.New("invalid Parent object")
				}
				for _, name := range names {
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("Field %s", name)
					}
					if obj := dict.Get(name); obj != nil {
						if common.Log.IsLogLevel(common.LogLevelTrace) {
							common.Log.Trace("- page has already")
						}
						if len(page.originalDict.Keys()) > 0 && !done {
							originalDict := page.originalDict
							if obj2 := originalDict.Get(name); obj2 != nil {
								if obj != obj2 {
									if common.Log.IsLogLevel(common.LogLevelTrace) {
										common.Log.Trace("Inheriting original field %s, %T", name, obj2)
									}
									dict.Set(name, obj2)
								}
							}
						}
						continue
					}
					if obj := dict2.Get(name); obj != nil {
						if common.Log.IsLogLevel(common.LogLevelTrace) {
							common.Log.Trace("Inheriting field %s", name)
						}
						dict.Set(name, obj)
					}
				}
				indirect, isIndirect = dict2.Get("Parent").(*core.PdfIndirectObject)
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Next parent: %T", dict2.Get("Parent"))
				}
			}
			if done {
				dict.Set("Parent", writer.pages)
			}
		}
		a.updateObjectsDeep(obj, nil)
		arr.Append(obj)
	}
	if _, err := a.readSeeker.Seek(0, io.SeekStart); err != nil {
		return err
	}
	writerBySignatureHandler := make(map[SignatureHandler]io.Writer)
	array := core.MakeArray()
	for _, newObject := range a.newObjects {
		if newObjectIndirect, ok := core.GetIndirect(newObject); ok {
			if signDict, ok := newObjectIndirect.PdfObject.(*pdfSignDictionary); ok {
				handler := *signDict.handler
				var err error
				writerBySignatureHandler[handler], err = handler.NewDigest(signDict.signature)
				if err != nil {
					return err
				}
				array.Append(core.MakeInteger(0xfffff), core.MakeInteger(0xfffff))
			}
		}
	}
	if array.Len() > 0 {
		array.Append(core.MakeInteger(0xfffff), core.MakeInteger(0xfffff))
	}
	for _, newObject := range a.newObjects {
		if newObjectIndirect, ok := core.GetIndirect(newObject); ok {
			if signDict, ok := newObjectIndirect.PdfObject.(*pdfSignDictionary); ok {
				signDict.Set("ByteRange", array)
			}
		}
	}
	flag := len(writerBySignatureHandler) > 0
	var readSeeker io.Reader = a.readSeeker
	if flag {
		ws := make([]io.Writer, 0, len(writerBySignatureHandler))
		for _, w := range writerBySignatureHandler {
			ws = append(ws, w)
		}
		readSeeker = io.TeeReader(a.readSeeker, io.MultiWriter(ws...))
	}
	n, err := io.Copy(w, readSeeker)
	if err != nil {
		return err
	}
	if len(a.newObjects) == 0 {
		return nil
	}
	if flag && a.roReader.parser.IsLinearized() {
		if n := a.roReader.parser.LinearizationDictObjNum(); n > 0 && !a.hasPriorSignature() {
			indirectObject := core.MakeIndirectObject(core.MakeDict())
			indirectObject.ObjectNumber = int64(n)
			a.objNumMap[indirectObject] = int64(n)
			a.addNewObject(indirectObject)
		}
	}
	writer.startOffset = n
	writer.ObjNumOffset = a.greatestObjNum
	writer.appendMode = true
	writer.prevXrefs = a.xrefs
	writer.prevXrefOffset = a.xrefOffset
	writer.originalSize = a.originalSize
	writer.version = a.roReader.PdfVersion()
	writer.objNumMap = a.objNumMap
	writer.crypter = a.parser.GetCrypter()
	writer.encryptObj = a.parser.GetEncryptObj()
	xrefType := a.parser.GetXrefType()
	if xrefType != nil {
		ok := *xrefType == core.XrefTypeObjectStream
		writer.useObjectStreams = &ok
	}
	writer.hasObjects = map[core.PdfObject]struct{}{}
	writer.objects = []core.PdfObject{}
	for _, newObject := range a.newObjects {
		if _, ok := a.skipObjects[newObject]; ok {
			continue
		}
		writer.addObject(newObject)
	}
	w2 := w
	if flag {
		w2 = bytes.NewBuffer(nil)
	}
	if a.password != "" && writer.crypter == nil {
		if err := writer.Encrypt([]byte(a.password), []byte(a.password), a.encryptOptions); err != nil {
			return err
		}
	}
	if id := trailer.Get("ID"); id != nil {
		if idArr, ok := core.GetArray(id); ok {
			writer.ids = idArr
		}
	}
	if err := writer.Write(w2); err != nil {
		return err
	}
	if flag {
		data := w2.(*bytes.Buffer).Bytes()
		array := core.MakeArray()
		var signDicts []*pdfSignDictionary
		var count int64
		for _, object := range writer.objects {
			if indirect, ok := core.GetIndirect(object); ok {
				if signDict, ok := indirect.PdfObject.(*pdfSignDictionary); ok {
					signDicts = append(signDicts, signDict)
					num := signDict.fileOffset + int64(signDict.contentsOffsetStart)
					array.Append(core.MakeInteger(count), core.MakeInteger(num-count))
					count = signDict.fileOffset + int64(signDict.contentsOffsetEnd)
				}
			}
		}
		array.Append(core.MakeInteger(count), core.MakeInteger(n+int64(len(data))-count))
		data2 := array.Write()
		for _, signDict := range signDicts {
			count := int(signDict.fileOffset - n)
			for byteRangeOffsetStart := signDict.byteRangeOffsetStart; byteRangeOffsetStart < signDict.byteRangeOffsetEnd; byteRangeOffsetStart++ {
				data[count+byteRangeOffsetStart] = ' '
			}
			data3 := data[count+signDict.byteRangeOffsetStart : count+signDict.byteRangeOffsetEnd]
			copy(data3, data2)
		}
		var num int
		for _, signDict := range signDicts {
			size := int(signDict.fileOffset - n)
			data2 := data[num : size+signDict.contentsOffsetStart]
			handler := *signDict.handler
			writerBySignatureHandler[handler].Write(data2)
			num = size + signDict.contentsOffsetEnd
		}
		for _, signDict := range signDicts {
			data2 := data[num:]
			handler := *signDict.handler
			writerBySignatureHandler[handler].Write(data2)
		}
		for _, signDict := range signDicts {
			count := int(signDict.fileOffset - n)
			handler := *signDict.handler
			w := writerBySignatureHandler[handler]
			if err := handler.Sign(signDict.signature, w); err != nil {
				return err
			}
			signDict.signature.ByteRange = array
			data3 := signDict.signature.Contents.Write()
			for byteRangeOffsetStart := signDict.byteRangeOffsetStart; byteRangeOffsetStart < signDict.byteRangeOffsetEnd; byteRangeOffsetStart++ {
				data[count+byteRangeOffsetStart] = ' '
			}
			for contentsOffsetStart := signDict.contentsOffsetStart; contentsOffsetStart < signDict.contentsOffsetEnd; contentsOffsetStart++ {
				data[count+contentsOffsetStart] = ' '
			}
			data4 := data[count+signDict.byteRangeOffsetStart : count+signDict.byteRangeOffsetEnd]
			copy(data4, data2)
			data4 = data[count+signDict.contentsOffsetStart : count+signDict.contentsOffsetEnd]
			copy(data4, data3)
		}
		buffer := bytes.NewBuffer(data)
		_, err = io.Copy(w, buffer)
		if err != nil {
			return err
		}
	}
	a.written = true
	return nil
}

// KDict represents a K dictionary object.
type KDict struct {
	// The structure type, a name object identifying the nature of the
	// structure element and its role within the document,
	// such as a chapter, paragraph, or footnote
	S core.PdfObject

	// The structure element that is the immediate parent of this one
	// in the structure hierarchy.
	P core.PdfObject

	// The element identifier, a byte string designating this structure element.
	ID *core.PdfObjectString

	// A page object representing a page on which some or all of the content
	// items designated by the K entry shall be rendered.
	Pg core.PdfObject

	// The children of this structure element.
	K core.PdfObject

	// A single attribute object or array of attribute objects associated
	// with this structure element.
	A core.PdfObject

	// An attribute class name or array of class names associated with
	// this structure element.
	C core.PdfObject

	// The current revision number of this structure element
	R *core.PdfObjectInteger

	// The title of the structure element, a text string representing
	// it in human-readable form.
	T *core.PdfObjectString

	// A language identifier specifying the natural language for all text
	// in the structure element except where overridden by
	// language specifications for nested structure elements or marked content.
	Lang *core.PdfObjectString

	// An alternate description of the structure element and its
	// children in human-readable form, which is useful when extracting
	// the document’s contents in support of accessibility to users with
	// disabilities or for other purposes.
	Alt *core.PdfObjectString

	// The expanded form of an abbreviation.
	E *core.PdfObjectString

	// Text that is an exact replacement for the structure element and its children.
	ActualText *core.PdfObjectString

	// NS is the namespace this structure element's type belongs to (PDF 2.0).
	NS *PdfStructureNamespace

	// AF is an array of associated file specification dictionaries (PDF 2.0).
	AF          *core.PdfObjectArray
	children    []*KValue
	pageNumber  int64
	boundingBox *PdfRectangle
}

func getPdfCreator() string {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	if len(pdfCreator) > 0 {
		return pdfCreator
	}
	return "OpenPDF - https://github.com/OpenMellon/openpdf"
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

func (w *PdfWriter) ensureLinearizedDocumentIDs(preObjs []preserializedObject) {
	if w.ids != nil {
		return
	}
	if w.id0 != "" && w.id1 != "" {
		w.ids = core.MakeArray(core.MakeHexString(w.id0), core.MakeHexString(w.id1))
		return
	}
	digest := md5.New()
	for _, preObj := range preObjs {
		digest.Write(preObj.data)
	}
	data := digest.Sum(nil)
	w.id0 = hex.EncodeToString(data[:8])
	w.id1 = hex.EncodeToString(data[8:])
	w.ids = core.MakeArray(core.MakeHexString(w.id0), core.MakeHexString(w.id1))
}

// SetPdfKeywords sets the Keywords attribute of the output PDF.
func SetPdfKeywords(keywords string) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	pdfKeywords = keywords
}

func iccProfileNumComponents(data []byte) int {
	if len(data) < 128 {
		return 0
	}
	switch string(data[16:20]) {
	case "GRAY":
		return 1
	case "RGB ":
		return 3
	case "CMYK":
		return 4
	default:
		return 0
	}
}

func findChainWithValidIntermediates(certsList [][]*x509.Certificate, fn func(*x509.Certificate) bool) []*x509.Certificate {
	for _, certs := range certsList {
		ok := true
		for i := 1; i < len(certs)-1; i++ {
			if !fn(certs[i]) {
				ok = false
				break
			}
		}
		if ok {
			return certs
		}
	}
	return nil
}

// NewPdfColorPatternType3 returns an empty color shading pattern type 3 (Radial).
func NewPdfColorPatternType3() *PdfColorPatternType3 {
	patternType3Color := &PdfColorPatternType3{}
	return patternType3Color
}

func (r *PdfReader) newPdfActionJavaScriptFromDict(dict *core.PdfObjectDictionary) (*PdfActionJavaScript, error) {
	return &PdfActionJavaScript{JS: dict.Get("JS")}, nil
}

func (h holdReport) lifted(answers []ocspAnswer, crls []crlEvidence, cert *x509.Certificate, t time.Time) bool {
	fn := func(t2 time.Time) bool { return t.IsZero() || !t2.After(t) }
	for _, answer := range answers {
		if answer.Status == ocsp.Good && answer.authorised && fn(answer.ThisUpdate) && !answer.ThisUpdate.After(cert.NotAfter) && h.precedes(answer.ThisUpdate, nil) {
			return true
		}
	}
	for _, crl := range crls {
		if !fn(crl.ThisUpdate) || crl.ThisUpdate.After(cert.NotAfter) || !h.precedes(crl.ThisUpdate, crl.parsedCRL) {
			continue
		}
		entryFor := crl.entryFor(cert)
		if entryFor != nil && entryFor.ReasonCode == crlReasonRemoveFromCRL {
			return true
		}
		if entryFor == nil && !crl.isDelta() && crl.covers(cert) {
			return true
		}
	}
	return false
}

// Colorspace returns the DeviceRGB colorspace.
func (c *PdfColorDeviceRGB) Colorspace() PdfColorspace { return NewPdfColorspaceDeviceRGB() }

const (
	_ PdfOutputIntentType = iota
	PdfOutputIntentTypeA1
	PdfOutputIntentTypeA2
	PdfOutputIntentTypeA3
	PdfOutputIntentTypeA4
	PdfOutputIntentTypeX
)

// SetCatalogViewerPreferences sets the catalog ViewerPreferences dictionary.
func (w *PdfWriter) SetCatalogViewerPreferences(pref core.PdfObject) error {
	if pref == nil {
		w.catalog.Remove("ViewerPreferences")
		return nil
	}
	if ref, ok := pref.(*core.PdfObjectReference); ok {
		pref = ref.Resolve()
		if pref == nil {
			w.catalog.Remove("ViewerPreferences")
			return nil
		}
	}
	if _, ok := pref.(*core.PdfIndirectObject); ok {
		w.addObject(pref)
	}
	w.catalog.Set("ViewerPreferences", pref)
	return nil
}

// GetChildren returns the children of the K dictionary object.
func (k *KDict) GetChildren() []*KValue { return k.children }

// SetForms sets the Acroform for a PDF file.
func (w *PdfWriter) SetForms(form *PdfAcroForm) error { w.acroForm = form; return nil }

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

// SetBorderWidth sets the style's border width.
func (b *PdfBorderStyle) SetBorderWidth(width float64) { b.W = &width }

// NewPdfActionGoTo3DView returns a new "goTo3DView" action.
func NewPdfActionGoTo3DView() *PdfActionGoTo3DView {
	action := NewPdfAction()
	goTo3DViewAction := &PdfActionGoTo3DView{}
	goTo3DViewAction.PdfAction = action
	action.SetContext(goTo3DViewAction)
	return goTo3DViewAction
}

// NewPdfColorspaceCalRGB returns a new CalRGB colorspace object.
func NewPdfColorspaceCalRGB() *PdfColorspaceCalRGB {
	calRGBCS := &PdfColorspaceCalRGB{}
	calRGBCS.BlackPoint = []float64{0.0, 0.0, 0.0}
	calRGBCS.Gamma = []float64{1.0, 1.0, 1.0}
	calRGBCS.Matrix = []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	return calRGBCS
}

// GetFontDescriptor returns the font descriptor for `font`.
func (f PdfFont) GetFontDescriptor() (*PdfFontDescriptor, error) {
	return f.context.getFontDescriptor(), nil
}

// NewPdfFunctionFromPdfObject loads a PDF function from a PdfObject (a stream for
// Type 0 and Type 4 functions, or a dictionary for Type 2 and Type 3).
func NewPdfFunctionFromPdfObject(obj core.PdfObject) (PdfFunction, error) {
	return newPdfFunctionFromPdfObject(obj)
}

// SetPieceInfo sets the page-piece dictionary associated with the document.
func (w *PdfWriter) SetPieceInfo(pieceInfo core.PdfObject) error {
	catalog := w.catalog
	if pieceInfo != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Setting PieceInfo...")
		}
		catalog.Set("PieceInfo", pieceInfo)
		return w.addObjects(pieceInfo)
	}
	return nil
}

func (w *PdfWriter) refreshPagesAfterCopy() {
	if w.root == nil {
		return
	}
	dict, ok := w.root.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return
	}
	indirect, _ := dict.Get("Pages").(*core.PdfIndirectObject)
	if indirect == nil {
		return
	}
	w.pages = indirect
	w.pageObjs = collectIndirectObjects(indirect)
}

func parseCIDWidths(obj core.PdfObject) (map[textencoding.CharCode]float64, error) {
	if obj == nil {
		return nil, nil
	}
	arr, ok := core.GetArray(obj)
	if !ok {
		return nil, nil
	}
	valByCode := map[textencoding.CharCode]float64{}
	arrLen := arr.Len()
	for i := 0; i < arrLen-1; i++ {
		obj := core.TraceToDirectObject(arr.Get(i))
		n, ok := core.GetIntVal(obj)
		if !ok {
			return nil, fmt.Errorf("bad font W obj0: i=%d %#v", i, obj)
		}
		i++
		if i > arrLen-1 {
			return nil, fmt.Errorf("bad font W array: arr2=%+v", arr)
		}
		obj2 := core.TraceToDirectObject(arr.Get(i))
		switch obj2.(type) {
		case *core.PdfObjectArray:
			arr, _ := core.GetArray(obj2)
			if floats, err := arr.ToFloat64Array(); err == nil {
				for i := 0; i < len(floats); i++ {
					valByCode[textencoding.CharCode(n+i)] = floats[i]
				}
			} else {
				return nil, fmt.Errorf("bad font W array obj1: i=%d %#v", i, obj2)
			}
		case *core.PdfObjectInteger:
			count, ok := core.GetIntVal(obj2)
			if !ok {
				return nil, fmt.Errorf("bad font W int obj1: i=%d %#v", i, obj2)
			}
			i++
			if i > arrLen-1 {
				return nil, fmt.Errorf("bad font W array: arr2=%+v", arr)
			}
			obj := arr.Get(i)
			f, err := core.GetNumberAsFloat(obj)
			if err != nil {
				return nil, fmt.Errorf("bad font W int obj2: i=%d %#v", i, obj)
			}
			for i := n; i <= count; i++ {
				valByCode[textencoding.CharCode(i)] = f
			}
		default:
			return nil, fmt.Errorf("bad font W obj1 type: i=%d %#v", i, obj2)
		}
	}
	return valByCode, nil
}

// ReplaceAcroForm replaces the acrobat form. It appends a new form to the Pdf which
// replaces the original AcroForm.
func (a *PdfAppender) ReplaceAcroForm(acroForm *PdfAcroForm) {
	if acroForm != nil {
		a.updateObjectsDeep(acroForm.ToPdfObject(), nil)
	}
	a.acroForm = acroForm
}

// GetColorGlyphInfo retrieves color glyph information for a given glyph ID
func GetColorGlyphInfo(otf *fonts.OtfType, glyphID typefont.GID) (*ColorGlyphInfo, bool) {
	if otf.Face == nil {
		return nil, false
	}
	glyphData := otf.Face.GlyphData(glyphID)
	if glyphData == nil {
		return nil, false
	}
	glyphInfo := &ColorGlyphInfo{GlyphID: glyphID}
	switch v := glyphData.(type) {
	case typefont.GlyphSVG:
		glyphInfo.GlyphType = ColorGlyphTypeSVG
		glyphInfo.SVGData = v.Source
		return glyphInfo, true
	case typefont.GlyphBitmap:
		glyphInfo.GlyphType = ColorGlyphTypeBitmap
		glyphInfo.BitmapData = v.Data
		glyphInfo.BitmapFormat = v.Format
		glyphInfo.BitmapWidth = v.Width
		glyphInfo.BitmapHeight = v.Height
		return glyphInfo, true
	default:
		return nil, false
	}
}

// NewStandardPdfOutputIntent creates a new standard PdfOutputIntent.
func NewStandardPdfOutputIntent(outputCondition, outputConditionIdentifier, registryName string, destOutputProfile []byte, colorComponents int) *PdfOutputIntent {
	return &PdfOutputIntent{Type: "OutputIntent", OutputCondition: outputCondition, OutputConditionIdentifier: outputConditionIdentifier, RegistryName: registryName, DestOutputProfile: destOutputProfile, ColorComponents: colorComponents, primitive: core.MakeDict()}
}

// PdfColorPatternType2 represents a color shading pattern type 2 (Axial).
type PdfColorPatternType2 struct {
	Color       PdfColor
	PatternName core.PdfObjectName
}

// NewPdfActionRendition returns a new "rendition" action.
func NewPdfActionRendition() *PdfActionRendition {
	action := NewPdfAction()
	renditionAction := &PdfActionRendition{}
	renditionAction.PdfAction = action
	action.SetContext(renditionAction)
	return renditionAction
}

// ICCProfile returns the raw ICC colour profile preserved from the source
// image, or nil if the source carried none. The returned slice must not be
// mutated.
func (img *Image) ICCProfile() []byte { return img.iccProfile }

func writeLazyStream(w io.Writer, n int, stream *core.PdfObjectStream, crypter *core.PdfCrypt) error {
	if stream.TempFile == "" {
		return fmt.Errorf("lazy stream %d: already written (lazy streams are consumed by the first write)", n)
	}
	if crypter != nil {
		take, ok := lazystream.Take(crypter, stream)
		if !ok {
			return fmt.Errorf("lazy stream %d: not encrypted by this writer", n)
		}
		if take != nil {
			_, err := w.Write(take)
			return err
		}
	}
	clean := filepath.Clean(stream.TempFile)
	open, err := os.Open(clean)
	if err != nil {
		return fmt.Errorf("lazy stream %d: temp file %q is not available (lazy streams are consumed by the first write): %w", n, clean, err)
	}
	data := make([]byte, 64*1024)
	for {
		count, err := open.Read(data)
		if count > 0 {
			if _, err := w.Write(data[:count]); err != nil {
				open.Close()
				return err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			open.Close()
			return fmt.Errorf("lazy stream %d: reading temp file %q: %w", n, clean, err)
		}
	}
	open.Close()
	return nil
}

func updateSignatureByteRangeOffsets(preObjByNum map[int64]preserializedObject) {
	for _, preObj := range preObjByNum {
		indirect, ok := preObj.obj.(*core.PdfIndirectObject)
		if !ok {
			continue
		}
		signDict, ok := indirect.PdfObject.(*pdfSignDictionary)
		if !ok {
			continue
		}
		s := fmt.Sprintf("%d 0 obj\n", preObj.objNum)
		signDict.fileOffset = preObj.offset + int64(len(s))
	}
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

// ToPdfObject returns the PDF representation of the VRI dictionary.
func (v *VRI) ToPdfObject() *core.PdfObjectDictionary {
	dict := core.MakeDict()
	dict.SetIfNotNil(core.PdfObjectName("Cert"), streamsToArray(v.Cert))
	dict.SetIfNotNil(core.PdfObjectName("OCSP"), streamsToArray(v.OCSP))
	dict.SetIfNotNil(core.PdfObjectName("CRL"), streamsToArray(v.CRL))
	dict.SetIfNotNil("TU", v.TU)
	dict.SetIfNotNil("TS", v.TS)
	return dict
}

// GetExtGState gets the ExtGState specified by keyName. Returns a bool
// indicating whether it was found or not.
func (r *PdfPageResources) GetExtGState(keyName core.PdfObjectName) (core.PdfObject, bool) {
	if r.ExtGState == nil {
		return nil, false
	}
	dict, ok := core.TraceToDirectObject(r.ExtGState).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Invalid ExtGState entry - not a dict (got %T)", r.ExtGState)
		return nil, false
	}
	if obj := dict.Get(keyName); obj != nil {
		return obj, true
	}
	return nil, false
}

// SetNameDictionary sets the Names entry in the PDF catalog.
// See section 7.7.4 "Name Dictionary" (p. 80 PDF32000_2008).
func (w *PdfWriter) SetNameDictionary(names core.PdfObject) error {
	if names == nil {
		return nil
	}
	w.names = newNamesFromPdfObject(names)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Setting catalog Names...")
	}
	w.catalog.Set("Names", names)
	return w.addObjects(names)
}

// GetCapHeight returns the CapHeight of the font `descriptor`.
func (d *PdfFontDescriptor) GetCapHeight() (float64, error) {
	return core.GetNumberAsFloat(d.CapHeight)
}

// SetEncoder sets the encoding for the underlying font.
func (f *pdfFontSimple) SetEncoder(encoder textencoding.SimpleEncoder) { f.encoder = encoder }

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationWatermark) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("Watermark"))
	dict.SetIfNotNil("FixedPrint", a.FixedPrint)
	return indirect
}

// PdfShadingPattern is a Shading patterns that provide a smooth transition between colors across an area to be painted,
// i.e. color(x,y) = f(x,y) at each point.
// It is a type 2 pattern (PatternType = 2).
type PdfShadingPattern struct {
	*PdfPattern
	Shading   *PdfShading
	Matrix    *core.PdfObjectArray
	ExtGState core.PdfObject
}

// SignatureHandlerDocMDPParams describe the specific parameters for the SignatureHandlerEx
// These parameters describe how to check the difference between revisions.
// Revisions of the document get from the PdfParser.
type SignatureHandlerDocMDPParams struct {
	Parser     *core.PdfParser
	DiffPolicy mdp.DiffPolicy
}

// GetRuneMetrics returns the character metrics for the specified rune.
// A bool flag is returned to indicate whether or not the entry was found.
func (f pdfCIDFontType2) GetRuneMetrics(r rune) (fonts.CharMetrics, bool) {
	runeToGID, ok := f.runeToGID[r]
	if !ok {
		num, ok := core.GetInt(f.DW)
		if !ok {
			return fonts.CharMetrics{}, false
		}
		runeToGID = int(*num)
	}
	return fonts.CharMetrics{Wx: float64(runeToGID)}, true
}

// NewPdfAnnotationLine returns a new line annotation.
func NewPdfAnnotationLine() *PdfAnnotationLine {
	annot := NewPdfAnnotation()
	line := &PdfAnnotationLine{}
	line.PdfAnnotation = annot
	line.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(line)
	return line
}

func (r *PdfReader) lookupPageByObject(obj core.PdfObject) (*PdfPage, error) {
	return nil, errors.New("page not found")
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

// NumCopies returns the value of the numCopies.
func (vp *ViewerPreferences) NumCopies() int { return vp.numCopies }

// SetPdfProducer sets the Producer attribute of the output PDF.
func SetPdfProducer(producer string) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	pdfProducer = producer
}

// PdfColorspaceDeviceGray represents a grayscale colorspace.
type PdfColorspaceDeviceGray struct{}

// ToPdfObject returns the PDF representation of the DSS dictionary.
func (d *DSS) ToPdfObject() core.PdfObject {
	dict := d.primitive.PdfObject.(*core.PdfObjectDictionary)
	dict.Clear()
	dict2 := core.MakeDict()
	for s, vri := range d.VRI {
		dict2.Set(*core.MakeName(s), vri.ToPdfObject())
	}
	dict.SetIfNotNil("Certs", streamsToArray(d.Certs))
	dict.SetIfNotNil("OCSPs", streamsToArray(d.OCSPs))
	dict.SetIfNotNil("CRLs", streamsToArray(d.CRLs))
	dict.Set("VRI", dict2)
	return d.primitive
}

// ToOutlineTree returns a low level PdfOutlineTreeNode object, based on
// the current instance.
func (o *Outline) ToOutlineTree() *PdfOutlineTreeNode {
	return &o.ToPdfOutline().PdfOutlineTreeNode
}

// ColorToRGB converts a color in Separation colorspace to RGB colorspace.
func (cs *PdfColorspaceSpecialSeparation) ColorToRGB(color PdfColor) (PdfColor, error) {
	if cs.AlternateSpace == nil {
		return nil, errors.New("alternate colorspace undefined")
	}
	return cs.AlternateSpace.ColorToRGB(color)
}

// Set applies flag fl to the flag's bitmask and returns the combined flag.
func (f FieldFlag) Set(fl FieldFlag) FieldFlag { return FieldFlag(f.Mask() | fl.Mask()) }

func setFieldValue(field *PdfField, obj core.PdfObject) error {
	switch field.GetContext().(type) {
	case *PdfFieldText:
		switch v := obj.(type) {
		case *core.PdfObjectName:
			name := v
			common.Log.Debug("Unexpected: Got V as name -> converting to string '%s'", name.String())
			field.V = core.MakeEncodedString(v.String(), true)
		case *core.PdfObjectString:
			field.V = core.MakeEncodedString(decodedTextString(v), true)
		default:
			common.Log.Debug("ERROR: Unsupported text field V type: %T (%#v)", v, v)
		}
	case *PdfFieldButton:
		switch obj.(type) {
		case *core.PdfObjectName:
			if len(obj.String()) > 0 {
				field.V = obj
				setAnnotationsAppearanceState(field, obj)
			}
		case *core.PdfObjectString:
			if len(obj.String()) > 0 {
				field.V = core.MakeName(obj.String())
				setAnnotationsAppearanceState(field, field.V)
			}
		default:
			common.Log.Debug("ERROR: UNEXPECTED %s -> %v", field.PartialName(), obj)
			field.V = obj
		}
	case *PdfFieldChoice:
		switch obj.(type) {
		case *core.PdfObjectName:
			if len(obj.String()) > 0 {
				field.V = core.MakeString(obj.String())
				setAnnotationsAppearanceState(field, obj)
			}
		case *core.PdfObjectString:
			if len(obj.String()) > 0 {
				field.V = obj
				setAnnotationsAppearanceState(field, core.MakeName(obj.String()))
			}
		case *core.PdfObjectArray:
			field.V = obj
		default:
			common.Log.Debug("ERROR: UNEXPECTED %s -> %v", field.PartialName(), obj)
			field.V = obj
		}
	case *PdfFieldSignature:
		common.Log.Debug("Signature field %s: value not fillable - ignoring %v", field.PartialName(), obj)
	}
	return nil
}

// GetDSS gets the DSS dictionary (ETSI TS 102 778-4 V1.1.1) of the current
// document revision.
func (a *PdfAppender) GetDSS() (dss *DSS) { return a.dss }

// FlattenFields flattens the form fields and annotations for the PDF loaded in `pdf` and makes
// non-editable.
// Looks up all widget annotations corresponding to form fields and flattens them by drawing the content
// through the content stream rather than annotations.
// References to flattened annotations will be removed from Page Annots array. For fields the AcroForm entry
// will be emptied.
// When `allannots` is true, all annotations will be flattened. Keep false if want to keep non-form related
// annotations intact.
// When `appgen` is not nil, it will be used to generate appearance streams for the field annotations.
func (r *PdfReader) FlattenFields(allannots bool, appgen FieldAppearanceGenerator) error {
	return r.flattenFieldsWithOpts(allannots, appgen, nil)
}

// A returns the value of the A component of the color.
func (c *PdfColorCalRGB) A() float64 { return c[0] }

// GetType returns the button field type which returns one of the following
// - PdfFieldButtonPush for push button fields
// - PdfFieldButtonCheckbox for checkbox fields
// - PdfFieldButtonRadio for radio button fields
func (f *PdfFieldButton) GetType() ButtonType {
	buttonType := ButtonTypeCheckbox
	if f.Ff != nil {
		if (uint32(*f.Ff) & FieldFlagPushbutton.Mask()) > 0 {
			buttonType = ButtonTypePush
		} else if (uint32(*f.Ff) & FieldFlagRadio.Mask()) > 0 {
			buttonType = ButtonTypeRadio
		}
	}
	return buttonType
}

// GetIndirectObjectByNumber retrieves and returns a specific PdfObject by object number.
func (r *PdfReader) GetIndirectObjectByNumber(number int) (core.PdfObject, error) {
	lookupByNumber, err := r.parser.LookupByNumber(number)
	return lookupByNumber, err
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationSquiggly) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Squiggly"))
	dict.SetIfNotNil("QuadPoints", a.QuadPoints)
	return indirect
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAction) ToPdfObject() core.PdfObject {
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.Clear()
	dict.Set("Type", core.MakeName("Action"))
	dict.SetIfNotNil("S", a.S)
	dict.SetIfNotNil("Next", a.Next)
	return indirect
}

// DecodeArray returns the range of color component values in CalRGB colorspace.
func (cs *PdfColorspaceCalRGB) DecodeArray() []float64 {
	return []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}
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

// NewPdfActionImportData returns a new "import data" action.
func NewPdfActionImportData() *PdfActionImportData {
	action := NewPdfAction()
	importDataAction := &PdfActionImportData{}
	importDataAction.PdfAction = action
	action.SetContext(importDataAction)
	return importDataAction
}

// SetPage directly sets the page object.
func (k *KDict) SetPage(page *core.PdfIndirectObject) { k.Pg = page }

const (
	fontFlagFixedPitch  = 0x00001
	fontFlagSerif       = 0x00002
	fontFlagSymbolic    = 0x00004
	fontFlagScript      = 0x00008
	fontFlagNonsymbolic = 0x00020
	fontFlagItalic      = 0x00040
	fontFlagAllCap      = 0x10000
	fontFlagSmallCap    = 0x20000
	fontFlagForceBold   = 0x40000
)

// RemoveWatermarkImage removes watermark.
// If you don't know exact name of watermark PDF object, pass empty string as an argument.
func (p *PdfPage) RemoveWatermarkImage(name string) error {
	if p.Resources != nil && p.Resources.XObject != nil {
		if xObjectDict, ok := core.GetDict(p.Resources.XObject); ok {
			for _, name2 := range xObjectDict.Keys() {
				if name == name2.String() {
					p.removeXObj(xObjectDict, name2)
					continue
				}
				stream, ok := core.GetStream(xObjectDict.Get(name2))
				if !ok {
					continue
				}
				pieceInfo, ok := core.GetDict(stream.Get("PieceInfo"))
				if !ok {
					continue
				}
				adbeCompoundType, ok := core.GetDict(pieceInfo.Get("ADBE_CompoundType"))
				if !ok {
					continue
				}
				if adbeCompoundType.Get("Private") != nil && adbeCompoundType.Get("Private").String() == "Watermark" {
					p.removeXObj(xObjectDict, name2)
				}
			}
		}
	}
	annotations, err := p.GetAnnotations()
	if err == nil {
		annots := make([]*PdfAnnotation, 0)
		for _, annotation := range annotations {
			switch annotation.GetContext().(type) {
			case *PdfAnnotationWatermark:
			default:
				annots = append(annots, annotation)
			}
		}
		p.SetAnnotations(annots)
	}
	return nil
}

// PdfAnnotationSquiggly represents Squiggly annotations.
// (Section 12.5.6.10).
type PdfAnnotationSquiggly struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	QuadPoints core.PdfObject
}

// GetObjectNums returns the object numbers of the PDF objects in the file
// Numbered objects are either indirect objects or stream objects.
// e.g. objNums := pdfReader.GetObjectNums()
// The underlying objects can then be accessed with
// pdfReader.GetIndirectObjectByNumber(objNums[0]) for the first available object.
func (r *PdfReader) GetObjectNums() []int { return r.parser.GetObjectNums() }

// GetCerts returns the certificate chain embedded in the signature, with the
// signer's certificate first. For PKCS#7 based SubFilters the signer is the
// certificate named by the SignerInfo; the /Cert entry lists it first already.
// When a SubFilter offers several sources, the next one is tried if the
// previous fails, and an error is returned only when none yields a chain.
func (s *PdfSignature) GetCerts() ([]*x509.Certificate, error) {
	var fns []func() ([]*x509.Certificate, error)
	switch s2, _ := core.GetNameVal(s.SubFilter); s2 {
	case "adbe.pkcs7.detached", "ETSI.CAdES.detached":
		fns = append(fns, s.extractChainFromPKCS7, s.extractChainFromCert)
	case "adbe.x509.rsa_sha1":
		fns = append(fns, s.extractChainFromCert)
	case "ETSI.RFC3161":
		fns = append(fns, s.extractChainFromPKCS7)
	default:
		return nil, fmt.Errorf("unsupported SubFilter: %s", s2)
	}
	var err error
	for _, fn := range fns {
		certs, certsErr := fn()
		if certsErr != nil {
			if err == nil {
				err = certsErr
			}
			continue
		}
		if len(certs) > 0 {
			return certs, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, ErrSignNoCertificates
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

func (r *PdfReader) newPdfAnnotationUnderlineFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationUnderline, error) {
	underline := PdfAnnotationUnderline{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	underline.PdfAnnotationMarkup = markup
	underline.QuadPoints = dict.Get("QuadPoints")
	return &underline, nil
}

// LinearizationInfo returns linearization parameters parsed from the PDF, or
// nil if the PDF is not linearized.
func (r *PdfReader) LinearizationInfo() *core.LinearizationInfo {
	return r.parser.LinearizationInfo()
}

// ToPdfObject recursively builds the Outline tree PDF object.
func (o *PdfOutline) ToPdfObject() core.PdfObject {
	indirect := o.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.Set("Type", core.MakeName("Outlines"))
	if o.First != nil {
		dict.Set("First", o.First.ToPdfObject())
	}
	if o.Last != nil {
		dict.Set("Last", o.Last.GetContext().GetContainingPdfObject())
	}
	if o.Parent != nil {
		dict.Set("Parent", o.Parent.GetContext().GetContainingPdfObject())
	}
	if o.Count != nil {
		dict.Set("Count", core.MakeInteger(*o.Count))
	}
	return indirect
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

// NewPdfAnnotationScreen returns a new screen annotation.
func NewPdfAnnotationScreen() *PdfAnnotationScreen {
	annot := NewPdfAnnotation()
	screen := &PdfAnnotationScreen{}
	screen.PdfAnnotation = annot
	annot.SetContext(screen)
	return screen
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

// ColorToRGB converts a ICCBased color to an RGB color.
func (cs *PdfColorspaceICCBased) ColorToRGB(color PdfColor) (PdfColor, error) {
	cs.buildTransform()
	if cs.transformReady {
		if components := color.Components(); len(components) == 3 {
			convert, f, v := cs.iccTransform.Convert(components[0], components[1], components[2])
			return NewPdfColorDeviceRGB(convert, f, v), nil
		}
	}
	if cs.Alternate == nil {
		common.Log.Debug("ICC Based colorspace missing alternative")
		if cs.N == 1 {
			common.Log.Debug("ICC Based colorspace missing alternative - using DeviceGray (N=1)")
			deviceGrayCS := NewPdfColorspaceDeviceGray()
			return deviceGrayCS.ColorToRGB(color)
		} else if cs.N == 3 {
			common.Log.Debug("ICC Based colorspace missing alternative - using DeviceRGB (N=3)")
			return color, nil
		} else if cs.N == 4 {
			common.Log.Debug("ICC Based colorspace missing alternative - using DeviceCMYK (N=4)")
			deviceCMYKCS := NewPdfColorspaceDeviceCMYK()
			return deviceCMYKCS.ColorToRGB(color)
		} else {
			return nil, errors.New("ICC Based colorspace missing alternative")
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("ICC Based colorspace with alternative: %#v", cs)
	}
	return cs.Alternate.ColorToRGB(color)
}

// PdfAnnotationSound represents Sound annotations.
// (Section 12.5.6.16).
type PdfAnnotationSound struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	Sound core.PdfObject
	Name  core.PdfObject
}

// GetColorGlyphFormat returns a string description of the bitmap format
func (g *ColorGlyphInfo) GetColorGlyphFormat() string {
	switch g.GlyphType {
	case ColorGlyphTypeSVG:
		return "SVG"
	case ColorGlyphTypeBitmap:
		switch g.BitmapFormat {
		case typefont.PNG:
			return "PNG"
		case typefont.JPG:
			return "JPEG"
		case typefont.TIFF:
			return "TIFF"
		case typefont.BlackAndWhite:
			return "Monochrome Bitmap"
		default:
			return "Unknown Bitmap"
		}
	default:
		return "Unknown"
	}
}

// HasExtGState checks whether a font is defined by the specified keyName.
func (r *PdfPageResources) HasExtGState(keyName core.PdfObjectName) bool {
	_, ok := r.GetFontByName(keyName)
	return ok
}

// RemoveChildAt removes a child at the specified index from the kChildren slice.
func (k *KDict) RemoveChildAt(index int) bool {
	if index < 0 || index >= len(k.children) {
		return false
	}
	k.children = append(k.children[:index], k.children[index+1:]...)
	return true
}

func (r *PdfReader) newPdfAnnotationInkFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationInk, error) {
	ink := PdfAnnotationInk{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	ink.PdfAnnotationMarkup = markup
	ink.InkList = dict.Get("InkList")
	ink.BS = dict.Get("BS")
	return &ink, nil
}

// PdfActionType represents an action type in PDF (section 12.6.4 p. 417).
type PdfActionType string

// ToInteger convert to an integer format.
func (c *PdfColorCalGray) ToInteger(bits int) uint32 {
	f := math.Pow(2, float64(bits)) - 1
	return uint32(f * c.Val())
}

func buildPageOffsetHintTable(layout *linearizationLayout, table map[int64]int64, preObjByNum map[int64]preserializedObject, n, count int64) (*core.PageOffsetHintTable, error) {
	if layout.pageCount <= 0 {
		return nil, fmt.Errorf("hint encoder: numPages=%d", layout.pageCount)
	}
	nums := make([]uint32, layout.pageCount)
	nums2 := make([]uint32, layout.pageCount)
	for i := 0; i < layout.pageCount; i++ {
		var nums3 []int64
		if i < len(layout.pageObjs) {
			nums3 = layout.pageObjs[i]
		}
		if i == 0 && len(nums3) == 0 {
			nums3 = layout.firstShared
		}
		nums[i] = uint32(len(nums3))
		for _, num := range nums3 {
			nums2[i] += uint32(len(preObjByNum[num].data))
		}
	}
	num := nums[0]
	size := nums[0]
	for _, length := range nums {
		if length < num {
			num = length
		}
		if length > size {
			size = length
		}
	}
	length := nums2[0]
	length2 := nums2[0]
	for _, length3 := range nums2 {
		if length3 < length {
			length = length3
		}
		if length3 > length2 {
			length2 = length3
		}
	}
	length3, err := firstPageHintOffset(layout, table, n, count)
	if err != nil {
		return nil, err
	}
	table2 := make(map[int64]uint32, len(layout.firstShared)+len(layout.mainShared))
	length4 := uint32(0)
	for _, firstShared := range layout.firstShared {
		if _, ok := table2[firstShared]; !ok {
			table2[firstShared] = length4
			length4++
		}
	}
	for _, mainShared := range layout.mainShared {
		if _, ok := table2[mainShared]; !ok {
			table2[mainShared] = length4
			length4++
		}
	}
	numsList := make([][]uint32, layout.pageCount)
	var length5, length6 uint32
	for i := 1; i < layout.pageCount; i++ {
		if i >= len(layout.pageShared) {
			continue
		}
		nums := make([]uint32, 0, len(layout.pageShared[i]))
		for _, pageShared := range layout.pageShared[i] {
			length7, ok := table2[pageShared]
			if !ok {
				continue
			}
			nums = append(nums, length7)
			if length7 > length6 {
				length6 = length7
			}
		}
		numsList[i] = nums
		if uint32(len(nums)) > length5 {
			length5 = uint32(len(nums))
		}
	}
	pageOffsetHintTable := &core.PageOffsetHintTable{LeastObjectsInPage: num, FirstPageOffset: length3, BitsPerObjectCountDelta: bitsRequired(size - num), LeastPageLength: length, BitsPerPageLengthDelta: bitsRequired(length2 - length), LeastContentStreamOffset: 0, BitsPerContentStreamOffsetDelta: 1, LeastContentStreamLength: 0, BitsPerContentStreamLengthDelta: 1, BitsPerNumSharedObjects: bitsRequired(length5), BitsPerSharedIdentifier: bitsRequired(length6), BitsPerSharedNumerator: 1, SharedDenominator: 1}
	pageOffsetHintTable.PerPage = make([]core.PageOffsetEntry, layout.pageCount)
	for i := range pageOffsetHintTable.PerPage {
		nums3 := numsList[i]
		nums4 := make([]uint32, len(nums3))
		pageOffsetHintTable.PerPage[i] = core.PageOffsetEntry{ObjectCountDelta: nums[i] - num, PageLengthDelta: nums2[i] - length, NumSharedObjects: uint32(len(nums3)), SharedObjectIdentifiers: nums3, SharedObjectNumerators: nums4}
	}
	return pageOffsetHintTable, nil
}

// IsCenterWindow returns the value of the centerWindow flag.
func (vp *ViewerPreferences) IsCenterWindow() bool {
	if vp.centerWindow == nil {
		return false
	}
	return *vp.centerWindow
}

func (w *PdfWriter) checkCrossReferenceStream() bool {
	ok := w.version.Major > 1 || (w.version.Major == 1 && w.version.Minor > 4)
	if w.useObjectStreams != nil {
		ok = *w.useObjectStreams
	}
	return ok
}

// ColorToRGB converts a CMYK32 color to an RGB color.
func (cs *PdfColorspaceDeviceCMYK) ColorToRGB(color PdfColor) (PdfColor, error) {
	deviceCMYKColor, ok := color.(*PdfColorDeviceCMYK)
	if !ok {
		common.Log.Debug("Input color not device cmyk")
		return nil, errors.New("type check error")
	}
	f := deviceCMYKColor.C()
	v := deviceCMYKColor.M()
	val := deviceCMYKColor.Y()
	x := deviceCMYKColor.K()
	f = f*(1-x) + x
	v = v*(1-x) + x
	val = val*(1-x) + x
	y := 1 - f
	y2 := 1 - v
	y3 := 1 - val
	return NewPdfColorDeviceRGB(y, y2, y3), nil
}

// GetNumComponents returns the number of color components (3 for CalRGB).
func (c *PdfColorCalRGB) GetNumComponents() int { return 3 }

// GetStandardApplier gets currently used StandardApplier..
func (w *PdfWriter) GetStandardApplier() StandardApplier { return w.standardApplier }

// NewPdfStructureNamespace creates a new namespace dictionary with the given URI.
func NewPdfStructureNamespace(uri string) *PdfStructureNamespace {
	return &PdfStructureNamespace{uri: uri}
}

// NewPdfColorspaceSpecialSeparation returns a new separation color.
func NewPdfColorspaceSpecialSeparation() *PdfColorspaceSpecialSeparation {
	specialSeparationCS := &PdfColorspaceSpecialSeparation{}
	return specialSeparationCS
}

// Outline represents a PDF outline dictionary (Table 152 - p. 376).
// Currently, the Outline object can only be used to construct PDF outlines.
type Outline struct {
	Entries []*OutlineItem `json:"entries,omitempty"`
}

func isContentsGapOnly(parser *core.PdfParser, nums []int64, str *core.PdfObjectString) bool {
	if len(nums) != 4 || str == nil {
		return false
	}
	if nums[0] != 0 {
		return false
	}
	n := nums[0] + nums[1]
	num := nums[2]
	data := str.Bytes()
	count := num - n
	if count < 2 || count > int64(3*len(data)+64) {
		return false
	}
	bytesAt, err := parser.ReadBytesAt(n, count)
	if err != nil {
		return false
	}
	if bytesAt[0] != '<' || bytesAt[len(bytesAt)-1] != '>' {
		return false
	}
	data2 := make([]byte, 0, len(bytesAt)-2)
	for _, b := range bytesAt[1 : len(bytesAt)-1] {
		switch {
		case core.IsWhiteSpace(b):
		case '0' <= b && b <= '9', 'a' <= b && b <= 'f', 'A' <= b && b <= 'F':
			data2 = append(data2, b)
		default:
			return false
		}
	}
	if len(data2)%2 != 0 {
		data2 = append(data2, '0')
	}
	data3, err := hex.DecodeString(string(data2))
	if err != nil {
		return false
	}
	return bytes.Equal(data3, data)
}

// VRI represents a Validation-Related Information dictionary.
// The VRI dictionary contains validation data in the form of
// certificates, OCSP and CRL information, for a single signature.
// See ETSI TS 102 778-4 V1.1.1 for more information.
type VRI struct {
	Cert []*core.PdfObjectStream
	OCSP []*core.PdfObjectStream
	CRL  []*core.PdfObjectStream
	TU   *core.PdfObjectString
	TS   *core.PdfObjectString
}

// GetOutlineTree returns the outline tree.
func (r *PdfReader) GetOutlineTree() *PdfOutlineTreeNode { return r.outlineTree }

// ToPdfObject returns a PdfObject representation of PdfColorspaceDeviceNAttributes as a PdfObjectDictionary directly
// or indirectly within an indirect object container.
func (cs *PdfColorspaceDeviceNAttributes) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	if cs.Subtype != nil {
		dict.Set("Subtype", cs.Subtype)
	}
	dict.SetIfNotNil("Colorants", cs.Colorants)
	dict.SetIfNotNil("Process", cs.Process)
	dict.SetIfNotNil("MixingHints", cs.MixingHints)
	if cs.container != nil {
		cs.container.PdfObject = dict
		return cs.container
	}
	return dict
}

// NewPdfColorspaceDeviceRGB returns a new RGB colorspace object.
func NewPdfColorspaceDeviceRGB() *PdfColorspaceDeviceRGB { return &PdfColorspaceDeviceRGB{} }

func (w *PdfWriter) reserveLinearizationObjectNumbers(layout *linearizationLayout) int64 {
	table := make(map[int64]int64, len(layout.firstSection)+len(layout.mainSection))
	n := int64(2)
	for _, firstSection := range layout.firstSection {
		table[firstSection] = n
		n++
	}
	count := n
	n++
	for _, mainSection := range layout.mainSection {
		table[mainSection] = n
		n++
	}
	fn := func(n int64) int64 {
		if count, ok := table[n]; ok {
			return count
		}
		return n
	}
	for _, object := range w.objects {
		switch v := object.(type) {
		case *core.PdfIndirectObject:
			v.ObjectNumber = fn(v.ObjectNumber)
		case *core.PdfObjectStream:
			v.ObjectNumber = fn(v.ObjectNumber)
		case *core.PdfObjectStreams:
			v.ObjectNumber = fn(v.ObjectNumber)
		}
	}
	for i := range layout.firstSection {
		layout.firstSection[i] = fn(layout.firstSection[i])
	}
	for i := range layout.mainSection {
		layout.mainSection[i] = fn(layout.mainSection[i])
	}
	for i := range layout.sharedObjs {
		layout.sharedObjs[i] = fn(layout.sharedObjs[i])
	}
	for i := range layout.firstShared {
		layout.firstShared[i] = fn(layout.firstShared[i])
	}
	for i := range layout.pageObjs {
		for j := range layout.pageObjs[i] {
			layout.pageObjs[i][j] = fn(layout.pageObjs[i][j])
		}
	}
	for i := range layout.mainShared {
		layout.mainShared[i] = fn(layout.mainShared[i])
	}
	for i := range layout.pageShared {
		for j := range layout.pageShared[i] {
			layout.pageShared[i][j] = fn(layout.pageShared[i][j])
		}
	}
	layout.firstPageObjNum = fn(layout.firstPageObjNum)
	layout.maxObjNum = count
	return count
}

func newPdfReaderCore(r io.ReaderAt, opts *ReaderOpts, autoDecrypt, ok bool, s string) (*PdfReader, error) {
	if opts == nil {
		opts = NewReaderOpts()
	}
	s2 := ""
	if file, ok := r.(*os.File); ok {
		s2 = file.Name()
	}
	var rs io.ReadSeeker
	if rs2, ok := r.(io.ReadSeeker); ok {
		rs = rs2
	}
	opts2 := *opts
	reader := &PdfReader{readSeeker: rs, traversed: map[core.PdfObject]struct{}{}, modelManager: newModelManager(), lazy: opts.LazyLoad, complianceMode: opts.ComplianceMode, skipOutlines: ok, skipPerms: ok, autoDecrypt: autoDecrypt, opts: &opts2, fileName: s2}
	randomID, err := randomIDWithPrefix("r")
	if err != nil {
		return nil, err
	}
	err = license.Track(randomID, s, reader.fileName)
	if err != nil {
		return nil, err
	}
	reader.trackID = randomID
	return reader, nil
}

func newOutlineDestFromPdfObject(obj core.PdfObject, reader *PdfReader) (*OutlineDest, error) {
	arr, ok := core.GetArray(obj)
	if !ok {
		return nil, errors.New("outline destination object must be an array")
	}
	arrLen := arr.Len()
	if arrLen < 2 {
		return nil, fmt.Errorf("invalid outline destination array length: %d", arrLen)
	}
	dest := &OutlineDest{Mode: "Fit"}
	obj2 := arr.Get(0)
	if indirect, ok := core.GetIndirect(obj2); ok {
		if _, n, err := reader.PageFromIndirectObject(indirect); err == nil {
			dest.Page = int64(n - 1)
		} else {
			common.Log.Debug("WARN: could not get page index for page %+v", indirect)
		}
		dest.PageObj = indirect
	} else if n, ok := core.GetIntVal(obj2); ok {
		if n >= 0 && n < len(reader.PageList) {
			dest.PageObj = reader.PageList[n].GetPageAsIndirectObject()
		} else {
			common.Log.Debug("WARN: could not get page container for page %d", n)
		}
		dest.Page = int64(n)
	} else {
		return nil, fmt.Errorf("invalid outline destination page: %T", obj2)
	}
	s, ok := core.GetNameVal(arr.Get(1))
	if !ok {
		common.Log.Debug("invalid outline destination magnification mode: %v", arr.Get(1))
		return dest, nil
	}
	switch s {
	case "Fit", "FitB":
	case "FitH", "FitBH":
		if arrLen > 2 {
			dest.Y, _ = core.GetNumberAsFloat(core.TraceToDirectObject(arr.Get(2)))
		}
	case "FitV", "FitBV":
		if arrLen > 2 {
			dest.X, _ = core.GetNumberAsFloat(core.TraceToDirectObject(arr.Get(2)))
		}
	case "XYZ":
		if arrLen > 4 {
			dest.X, _ = core.GetNumberAsFloat(core.TraceToDirectObject(arr.Get(2)))
			dest.Y, _ = core.GetNumberAsFloat(core.TraceToDirectObject(arr.Get(3)))
			dest.Zoom, _ = core.GetNumberAsFloat(core.TraceToDirectObject(arr.Get(4)))
		}
	default:
		s = "Fit"
	}
	dest.Mode = s
	return dest, nil
}

// AddAssociatedFile adds an associated file specification to this element (PDF 2.0).
func (k *KDict) AddAssociatedFile(filespec core.PdfObject) {
	if k.AF == nil {
		k.AF = core.MakeArray()
	}
	k.AF.Append(filespec)
}

// SetPdfCreationDate sets the CreationDate attribute of the output PDF.
func SetPdfCreationDate(creationDate time.Time) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	pdfCreationDate = creationDate
}

// DecodeArray returns the range of color component values in the ICCBased colorspace.
func (cs *PdfColorspaceICCBased) DecodeArray() []float64 { return cs.Range }

// ImageToGray returns a new grayscale image based on the passed in RGB image.
func (cs *PdfColorspaceDeviceRGB) ImageToGray(img Image) (Image, error) {
	if img.ColorComponents != 3 {
		return img, errors.New("provided image is not a DeviceRGB")
	}
	img2, err := imageutil.NewImage(int(img.Width), int(img.Height), int(img.BitsPerComponent), img.ColorComponents, img.Data, img.alpha, img.decode)
	if err != nil {
		return img, err
	}
	convert, err := imageutil.GrayConverter.Convert(img2)
	if err != nil {
		return img, err
	}
	return imageFromImageBase(convert.Base()), nil
}

// NewPdfOutline returns an initialized PdfOutline.
func NewPdfOutline() *PdfOutline {
	outline := &PdfOutline{primitive: core.MakeIndirectObject(core.MakeDict())}
	outline.context = outline
	return outline
}

const maxCRLAge = 24 * time.Hour

// GetContext returns a reference to the subpattern entry: either PdfTilingPattern or PdfShadingPattern.
func (p *PdfPattern) GetContext() PdfModel { return p.context }

type holdReport struct {
	revokedAt  time.Time
	thisUpdate time.Time
	crl        *parsedCRL
}

// ToPdfObject returns the choice field dictionary within an indirect object (container).
func (f *PdfFieldChoice) ToPdfObject() core.PdfObject {
	f.PdfField.ToPdfObject()
	indirect := f.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.Set("FT", core.MakeName("Ch"))
	if f.Opt != nil {
		dict.Set("Opt", f.Opt)
	}
	if f.TI != nil {
		dict.Set("TI", f.TI)
	}
	if f.I != nil {
		dict.Set("I", f.I)
	}
	return indirect
}

func newVRIFromDict(dict *core.PdfObjectDictionary) *VRI {
	tu, _ := core.GetString(dict.Get("TU"))
	ts, _ := core.GetString(dict.Get("TS"))
	return &VRI{Cert: streamsFromObject(dict.Get("Cert")), OCSP: streamsFromObject(dict.Get("OCSP")), CRL: streamsFromObject(dict.Get("CRL")), TU: tu, TS: ts}
}
func (f *pdfFontType0) baseFields() *fontCommon { return &f.fontCommon }
func registerGlyphWidthsFromGlyphIndexes(w core.PdfObject, gids []unitype.GlyphIndex) {
	var n uint16
	for _, gid := range gids {
		if uint16(gid) > n {
			n = uint16(gid)
		}
	}
	count := n + 1
	cidWidths, err := parseCIDWidths(w)
	if err != nil || cidWidths == nil {
		return
	}
	numByV := make(map[fonts.GID]int, len(cidWidths))
	for code, cidWidth := range cidWidths {
		v := fonts.GID(code)
		if v < fonts.GID(count) {
			numByV[v] = int(cidWidth)
		}
	}
	widthsArray := buildWidthsArray(numByV, count)
	if wIndirect, ok := core.GetIndirect(w); ok {
		wIndirect.PdfObject = widthsArray
	}
}

func (w *PdfWriter) writeXRefStreams(n int, count int64) error {
	num := n + 1
	w.crossReferences[num] = crossReference{Type: 1, ObjectNumber: num, Offset: count}
	buffer := bytes.NewBuffer(nil)
	array := core.MakeArray()
	for i := 0; i <= n; {
		for ; i <= n; i++ {
			xref, ok := w.crossReferences[i]
			if ok && (!w.appendMode || w.appendMode && (xref.Type == 1 && xref.Offset >= w.originalSize || xref.Type == 0)) {
				break
			}
		}
		var count int
		for count = i + 1; count <= n; count++ {
			xref, ok := w.crossReferences[count]
			if ok && (!w.appendMode || w.appendMode && (xref.Type == 1 && xref.Offset > w.originalSize)) {
				continue
			}
			break
		}
		array.Append(core.MakeInteger(int64(i)), core.MakeInteger(int64(count-i)))
		for j := i; j < count; j++ {
			xref := w.crossReferences[j]
			switch xref.Type {
			case 0:
				binary.Write(buffer, binary.BigEndian, byte(0))
				binary.Write(buffer, binary.BigEndian, uint32(0))
				binary.Write(buffer, binary.BigEndian, uint16(0xFFFF))
			case 1:
				binary.Write(buffer, binary.BigEndian, byte(1))
				binary.Write(buffer, binary.BigEndian, uint32(xref.Offset))
				binary.Write(buffer, binary.BigEndian, uint16(xref.Generation))
			case 2:
				binary.Write(buffer, binary.BigEndian, byte(2))
				binary.Write(buffer, binary.BigEndian, uint32(xref.ObjectNumber))
				binary.Write(buffer, binary.BigEndian, uint16(xref.Index))
			}
		}
		i = count + 1
	}
	stream, err := core.MakeStream(buffer.Bytes(), core.NewFlateEncoder())
	if err != nil {
		return err
	}
	stream.ObjectNumber = int64(num)
	stream.PdfObjectDictionary.Set("Type", core.MakeName("XRef"))
	stream.PdfObjectDictionary.Set("W", core.MakeArray(core.MakeInteger(1), core.MakeInteger(4), core.MakeInteger(2)))
	stream.PdfObjectDictionary.Set("Index", array)
	stream.PdfObjectDictionary.Set("Size", core.MakeInteger(int64(num)))
	stream.PdfObjectDictionary.Set("Info", w.infoObj)
	stream.PdfObjectDictionary.Set("Root", w.root)
	if w.appendMode && w.prevXrefOffset > 0 {
		stream.PdfObjectDictionary.Set("Prev", core.MakeInteger(w.prevXrefOffset))
	}
	if w.crypter != nil {
		stream.Set("Encrypt", w.encryptObj)
	}
	if w.ids == nil && w.id0 != "" && w.id1 != "" {
		w.ids = core.MakeArray(core.MakeHexString(w.id0), core.MakeHexString(w.id1))
	}
	if w.ids != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Ids: %s", w.ids)
		}
		stream.Set("ID", w.ids)
	}
	return w.writeObject(int(stream.ObjectNumber), stream)
}

// NewPdfReaderLazy creates a new PdfReader in lazy-loading mode. Objects are
// loaded into memory only when needed. The source's byte length is
// auto-detected; for exotic io.ReaderAt sources use NewPdfReaderLazyAt.
func NewPdfReaderLazy(r io.ReaderAt) (*PdfReader, error) {
	sizeOfReaderAt, err := core.SizeOfReaderAt(r)
	if err != nil {
		return nil, err
	}
	return NewPdfReaderLazyAt(r, sizeOfReaderAt)
}

// NewStructTreeRootFromPdfObject creates a new structure tree root from a PDF object.
func NewStructTreeRootFromPdfObject(obj core.PdfObject) (*StructTreeRoot, error) {
	obj2 := core.ResolveReference(obj)
	dict, ok := core.GetDict(obj2)
	if !ok {
		return nil, fmt.Errorf("existing structure tree root is not a dictionary")
	}
	root := &StructTreeRoot{}
	root.container = core.MakeIndirectObject(core.MakeDict())
	if k := dict.Get("K"); k != nil {
		obj := core.ResolveReference(k)
		err := core.ResolveReferencesDeep(obj, nil)
		if err != nil {
			common.Log.Debug("Error resolving K object: %v", err)
		}
		root.K = []*KDict{}
		if arr, ok := core.GetArray(obj); ok {
			for i := 0; i < arr.Len(); i++ {
				obj := arr.Get(i)
				kDict, err := NewKDictFromPdfObject(obj)
				if err != nil {
					return nil, err
				}
				root.K = append(root.K, kDict)
			}
		} else {
			kDict, err := NewKDictFromPdfObject(obj)
			if err != nil {
				return nil, err
			}
			root.K = append(root.K, kDict)
		}
	}
	if idTree := dict.Get("IDTree"); idTree != nil {
		root.IDTree = newIDTreeFromPdfObject(idTree)
	}
	if parentTree := dict.Get("ParentTree"); parentTree != nil {
		obj := core.ResolveReference(parentTree)
		if dict, ok := core.GetDict(obj); ok {
			root.ParentTree = dict
		}
	}
	if parentTreeNextKey := dict.Get("ParentTreeNextKey"); parentTreeNextKey != nil {
		_, ok := core.GetInt(parentTreeNextKey)
		if ok {
			n, err := core.GetNumberAsInt64(parentTreeNextKey)
			if err != nil {
				return nil, err
			}
			root.ParentTreeNextKey = n
		}
	}
	if roleMap := dict.Get("RoleMap"); roleMap != nil {
		switch v := roleMap.(type) {
		case *core.PdfIndirectObject:
			if dict, ok := core.GetDict(v.PdfObject); ok {
				root.RoleMap = dict
			}
		case *core.PdfObjectDictionary:
			root.RoleMap = v
		case *core.PdfObjectString:
			root.RoleMap = v
		}
	}
	if classMap := dict.Get("ClassMap"); classMap != nil {
		if classMapDict, ok := core.GetDict(classMap); ok {
			root.ClassMap = classMapDict
		}
	}
	if namespaces, ok := core.GetArray(dict.Get("Namespaces")); ok {
		for _, obj := range namespaces.Elements() {
			if structureNamespace, ok := newPdfStructureNamespaceFromObject(obj); ok {
				root.Namespaces = append(root.Namespaces, structureNamespace)
			}
		}
	}
	return root, nil
}

const (
	StructureNamespacePDF2   = "http://iso.org/pdf2/ssn"
	StructureNamespacePDF17  = "http://iso.org/pdf/ssn"
	StructureNamespaceMathML = "http://www.w3.org/1998/Math/MathML"
)

func (w *PdfWriter) computeLinearizationLayout() (*linearizationLayout, error) {
	if len(w.pageObjs) == 0 {
		return nil, fmt.Errorf("linearization layout: no pages")
	}
	numByObj := make(map[core.PdfObject]int64, len(w.objects))
	objByNum := make(map[int64]core.PdfObject, len(w.objects))
	for _, object := range w.objects {
		n, _ := objectNumbers(object)
		if n == 0 {
			continue
		}
		numByObj[object] = n
		objByNum[n] = object
	}
	sets := make([]map[int64]struct{}, len(w.pageObjs))
	for i, pageObj := range w.pageObjs {
		sets[i] = collectReachableObjectNumbers(pageObj, numByObj)
	}
	set := map[int64]struct{}{}
	fn := func(obj core.PdfObject) {
		if obj == nil {
			return
		}
		if n, ok := numByObj[obj]; ok {
			set[n] = struct{}{}
		}
	}
	fn(w.root)
	fn(w.pages)
	if w.encryptObj != nil {
		fn(w.encryptObj)
	}
	if w.infoObj != nil {
		fn(w.infoObj)
	}
	if w.acroForm != nil {
		if indirect, ok := w.catalog.Get("AcroForm").(*core.PdfIndirectObject); ok && indirect != nil {
			fn(indirect)
		}
	}
	if indirect, ok := w.catalog.Get("Outlines").(*core.PdfIndirectObject); ok && indirect != nil {
		fn(indirect)
	}
	if indirect, ok := w.catalog.Get("Names").(*core.PdfIndirectObject); ok && indirect != nil {
		fn(indirect)
	}
	set2 := sets[0]
	set3 := map[int64]struct{}{}
	sets2 := make([]map[int64]struct{}, len(sets))
	for i := range sets2 {
		sets2[i] = map[int64]struct{}{}
	}
	set4 := map[int64]struct{}{}
	table := make(map[int64]int, len(objByNum))
	table2 := make(map[int64]int)
	for i := 1; i < len(sets); i++ {
		for n := range sets[i] {
			table[n]++
			if table[n] == 1 {
				table2[n] = i
			}
		}
	}
	for n := range objByNum {
		_, ok := set2[n]
		_, found := set[n]
		count := table[n]
		switch {
		case found:
		case ok:
		case count > 1:
			set3[n] = struct{}{}
		case count == 1:
			sets2[table2[n]][n] = struct{}{}
		default:
			set4[n] = struct{}{}
		}
	}
	table3 := make(map[int64]int, len(objByNum))
	for i := 0; i < len(sets); i++ {
		for n := range sets[i] {
			table3[n]++
		}
	}
	n := numByObj[w.pageObjs[0]]
	nums := make([]int64, 0, len(set))
	set5 := map[int64]struct{}{}
	fn2 := func(n int64) {
		if n == 0 {
			return
		}
		if _, ok := set[n]; !ok {
			return
		}
		if _, ok := set2[n]; ok {
			return
		}
		if _, ok := set5[n]; ok {
			return
		}
		if _, ok := objByNum[n]; !ok {
			return
		}
		set5[n] = struct{}{}
		nums = append(nums, n)
	}
	if w.root != nil {
		fn2(numByObj[w.root])
	}
	if w.pages != nil {
		fn2(numByObj[w.pages])
	}
	if w.infoObj != nil {
		fn2(numByObj[w.infoObj])
	}
	for _, object := range w.objects {
		n, _ := objectNumbers(object)
		fn2(n)
	}
	nums2 := []int64{n}
	set6 := map[int64]struct{}{n: {}}
	for _, object := range w.objects {
		count, _ := objectNumbers(object)
		if count == n {
			continue
		}
		if _, ok := set2[count]; !ok {
			continue
		}
		if _, ok := set5[count]; ok {
			continue
		}
		if _, ok := set6[count]; ok {
			continue
		}
		set6[count] = struct{}{}
		nums2 = append(nums2, count)
	}
	nums3 := make([]int64, 0, len(nums)+len(nums2))
	nums3 = append(nums3, nums...)
	nums3 = append(nums3, nums2...)
	nums4 := append([]int64(nil), nums2...)
	numsList := make([][]int64, len(w.pageObjs))
	numsList[0] = append([]int64(nil), nums4...)
	table4 := make(map[int64]int)
	for i := 1; i < len(sets2); i++ {
		for n := range sets2[i] {
			table4[n] = i
		}
	}
	sets3 := make([]map[int64]struct{}, len(w.pageObjs))
	for i := 1; i < len(w.pageObjs); i++ {
		n := numByObj[w.pageObjs[i]]
		numsList[i] = []int64{n}
		sets3[i] = map[int64]struct{}{n: {}}
	}
	for _, object := range w.objects {
		n, _ := objectNumbers(object)
		count, ok := table4[n]
		if !ok {
			continue
		}
		if _, ok := sets3[count][n]; ok {
			continue
		}
		sets3[count][n] = struct{}{}
		numsList[count] = append(numsList[count], n)
	}
	nums5 := make([]int64, 0, len(set3))
	for _, object := range w.objects {
		n, _ := objectNumbers(object)
		if _, ok := set3[n]; ok {
			nums5 = append(nums5, n)
		}
	}
	nums6 := make([]int64, 0, len(nums5)+len(set4)+len(objByNum))
	nums6 = append(nums6, nums5...)
	for i := 1; i < len(w.pageObjs); i++ {
		nums6 = append(nums6, numsList[i]...)
	}
	for _, object := range w.objects {
		n, _ := objectNumbers(object)
		if _, ok := set4[n]; ok {
			nums6 = append(nums6, n)
		}
	}
	nums7 := make([]int64, 0, len(nums2)+len(nums5))
	nums7 = append(nums7, nums2...)
	nums7 = append(nums7, nums5...)
	set7 := make(map[int64]struct{}, len(nums7))
	for _, num := range nums7 {
		set7[num] = struct{}{}
	}
	list := make([][]int64, len(w.pageObjs))
	for i := range list {
		var nums8 []int64
		for _, num := range nums7 {
			if _, ok := sets[i][num]; !ok {
				continue
			}
			if table3[num] <= 1 {
				continue
			}
			nums8 = append(nums8, num)
		}
		list[i] = nums8
	}
	nums8 := append([]int64(nil), nums7...)
	objs := make([]core.PdfObject, 0, len(nums3)+len(nums6))
	for _, num := range nums3 {
		objs = append(objs, objByNum[num])
	}
	for _, num := range nums6 {
		objs = append(objs, objByNum[num])
	}
	return &linearizationLayout{firstSection: nums3, mainSection: nums6, sharedObjs: nums8, orderedObjs: objs, firstShared: nums4, pageObjs: numsList, pageShared: list, mainShared: nums5, pageCount: len(w.pageObjs), firstPageObjNum: n}, nil
}

// NewStandard14Font returns the standard 14 font named `basefont` as a *PdfFont, or an error if it
// `basefont` is not one of the standard 14 font names.
func NewStandard14Font(basefont StdFontName) (*PdfFont, error) {
	fontSimple, err := newPdfFontSimpleFromStdName(basefont)
	if err != nil {
		return nil, err
	}
	if basefont != SymbolName && basefont != ZapfDingbatsName {
		fontSimple.encoder = textencoding.NewWinAnsiEncoder()
	}
	return &PdfFont{context: &fontSimple}, nil
}

func resolveColorspaceWithICC(colorSpace PdfColorspace, img *Image) PdfColorspace {
	if len(img.iccProfile) > 0 {
		if iccBasedColorspace := newICCBasedColorspaceFromProfile(img.iccProfile, colorSpace); iccBasedColorspace != nil {
			return iccBasedColorspace
		}
	}
	return colorSpace
}

func registerOtfGlyphWidths(w core.PdfObject, vals []uint16, otf *fonts.OtfType) {
	if otf == nil || len(vals) == 0 || len(otf.Widths) == 0 {
		return
	}
	cidWidths, err := parseCIDWidths(w)
	if err != nil {
		return
	}
	if cidWidths == nil {
		cidWidths = map[textencoding.CharCode]float64{}
	}
	f := 1000.0 / float64(otf.UnitsPerEm)
	if otf.UnitsPerEm == 0 {
		f = 1.0
	}
	numByV := make(map[fonts.GID]int, len(cidWidths)+len(vals))
	var n uint16
	for code, cidWidth := range cidWidths {
		numByV[fonts.GID(code)] = int(cidWidth)
		if uint16(code) > n {
			n = uint16(code)
		}
	}
	ok := false
	for _, val := range vals {
		if _, ok := numByV[fonts.GID(val)]; ok {
			continue
		}
		width := otf.Widths[len(otf.Widths)-1]
		if int(val) < len(otf.Widths) {
			width = otf.Widths[val]
		}
		numByV[fonts.GID(val)] = int(f * float64(width))
		if val > n {
			n = val
		}
		ok = true
	}
	if !ok {
		return
	}
	widthsArray := buildWidthsArray(numByV, n+1)
	if wIndirect, ok := core.GetIndirect(w); ok {
		wIndirect.PdfObject = widthsArray
	}
}

// EnableByName LTV enables the signature dictionary of the PDF AcroForm
// field identified the specified name. The signing certificate chain is
// extracted from the signature dictionary. Optionally, additional certificates
// can be specified through the `extraCerts` parameter. The LTV client attempts
// to build the certificate chain up to a trusted root by downloading any
// missing certificates.
func (l *LTV) EnableByName(name string, extraCerts []*x509.Certificate) error {
	acroForm := l.appender.roReader.AcroForm
	for _, allField := range acroForm.AllFields() {
		signatureField, _ := allField.GetContext().(*PdfFieldSignature)
		if signatureField == nil {
			continue
		}
		if partialName := signatureField.PartialName(); partialName != name {
			continue
		}
		return l.Enable(signatureField.V, extraCerts)
	}
	return nil
}

// SetXObjectFormByName adds the provided XObjectForm to the page resources.
// The added XObjectForm is identified by the specified name.
func (r *PdfPageResources) SetXObjectFormByName(keyName core.PdfObjectName, xform *XObjectForm) error {
	stream := xform.ToPdfObject().(*core.PdfObjectStream)
	err := r.SetXObjectByName(keyName, stream)
	return err
}

// AddOutlineTree adds outlines to a PDF file.
func (w *PdfWriter) AddOutlineTree(outlineTree *PdfOutlineTreeNode) {
	w.outlineTree = outlineTree
}

// ToPdfObject returns the PDF representation of the colorspace.
func (cs *PdfColorspaceDeviceGray) ToPdfObject() core.PdfObject {
	return core.MakeName("DeviceGray")
}

// GetCIDToGIDMapObject returns the underlying CIDToGIDMap object if the font
// type is CIDFontType2. For a Type0 font it delegates to the descendant CIDFont.
func (f *PdfFont) GetCIDToGIDMapObject() core.PdfObject {
	switch v := f.context.(type) {
	case *pdfCIDFontType2:
		return v.CIDToGIDMap
	case *pdfFontType0:
		if v.DescendantFont != nil {
			return v.DescendantFont.GetCIDToGIDMapObject()
		}
	}
	return nil
}

// GetPdfName returns the PDF name used to indicate the border style.
// (Table 166 p. 395).
func (b *BorderStyle) GetPdfName() string {
	switch *b {
	case BorderStyleSolid:
		return "S"
	case BorderStyleDashed:
		return "D"
	case BorderStyleBeveled:
		return "B"
	case BorderStyleInset:
		return "I"
	case BorderStyleUnderline:
		return "U"
	}
	return ""
}

// ToPdfObject returns a *PdfIndirectObject containing a *PdfObjectArray representation of the DeviceN colorspace.
/*
	Format: [/DeviceN names alternateSpace tintTransform]
	    or: [/DeviceN names alternateSpace tintTransform attributes]
*/
func (cs *PdfColorspaceDeviceN) ToPdfObject() core.PdfObject {
	array := core.MakeArray(core.MakeName("DeviceN"))
	array.Append(cs.ColorantNames)
	array.Append(cs.AlternateSpace.ToPdfObject())
	array.Append(cs.TintTransform.ToPdfObject())
	if cs.Attributes != nil {
		array.Append(cs.Attributes.ToPdfObject())
	}
	if cs.container != nil {
		cs.container.PdfObject = array
		return cs.container
	}
	return array
}

// PdfFunction interface represents the common methods of a function in PDF.
type PdfFunction interface {
	Evaluate([]float64) ([]float64, error)
	ToPdfObject() core.PdfObject
}

// Encoder returns the font's text encoder.
func (f pdfFontType0) Encoder() textencoding.TextEncoder { return f.encoder }

// AlphaMap performs mapping of alpha data for transformations. Allows custom filtering of alpha data etc.
func (img *Image) AlphaMap(mapFunc AlphaMapFunc) {
	for i, alpha := range img.alpha {
		img.alpha[i] = mapFunc(alpha)
	}
}

func stringsFromArray(opt *core.PdfObjectArray) []string {
	strs := make([]string, 0, opt.Len())
	for _, obj := range opt.Elements() {
		if str, ok := core.GetString(obj); ok {
			strs = append(strs, str.Decoded())
			continue
		}
		if arr, ok := core.GetArray(obj); ok && arr.Len() >= 1 {
			if str, ok := core.GetString(arr.Get(0)); ok {
				strs = append(strs, str.Decoded())
				continue
			}
		}
		strs = append(strs, "")
	}
	return strs
}

// ToInteger convert to an integer format.
func (c *PdfColorDeviceCMYK) ToInteger(bits int) [4]uint32 {
	f := math.Pow(2, float64(bits)) - 1
	return [4]uint32{uint32(f * c.C()), uint32(f * c.M()), uint32(f * c.Y()), uint32(f * c.K())}
}

// GetOtfType returns the OTF type information if available.
func (f *pdfCIDFontType2) GetOtfType() *fonts.OtfType { return f.otfType }

// SetShadingByName sets a shading resource specified by keyName.
func (r *PdfPageResources) SetShadingByName(keyName core.PdfObjectName, shadingObj core.PdfObject) error {
	if r.Shading == nil {
		r.Shading = core.MakeDict()
	}
	shadingDict, ok := core.GetDict(r.Shading)
	if !ok {
		return core.ErrTypeError
	}
	shadingDict.Set(keyName, shadingObj)
	return nil
}

// Set sets the colorspace corresponding to key. Add to Names if not set.
func (r *PdfPageResourcesColorspaces) Set(key core.PdfObjectName, val PdfColorspace) {
	if _, ok := r.Colorspaces[string(key)]; !ok {
		r.Names = append(r.Names, string(key))
	}
	r.Colorspaces[string(key)] = val
}

var licenseCheckDisabled = false

// BytesToCharcodes converts the bytes in a PDF string to character codes.
func (f *PdfFont) BytesToCharcodes(data []byte) []textencoding.CharCode {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("BytesToCharcodes: data=[% 02x]=%#q", data, data)
	}
	if font, ok := f.context.(*pdfFontType0); ok && font.predefinedCMap != nil {
		if bytesToCharcodes, ok := font.bytesToCharcodes(data); ok {
			return bytesToCharcodes
		}
	}
	var (
		codes      = make([]textencoding.CharCode, 0, len(data)+len(data)%2)
		baseFields = f.baseFields()
	)
	if baseFields.toUnicodeCMap != nil {
		if bytesToCharcodes, ok := baseFields.toUnicodeCMap.BytesToCharcodes(data); ok {
			for _, bytesToCharcode := range bytesToCharcodes {
				codes = append(codes, textencoding.CharCode(bytesToCharcode))
			}
			return codes
		}
	}
	if baseFields.isCIDFont() {
		if len(data) == 1 {
			data = []byte{0, data[0]}
		}
		if len(data)%2 != 0 {
			common.Log.Debug("ERROR: Padding data=%+v to even length", data)
			data = append(data, 0)
		}
		for i := 0; i < len(data); i += 2 {
			n := uint16(data[i])<<8 | uint16(data[i+1])
			codes = append(codes, textencoding.CharCode(n))
		}
	} else {
		for _, b := range data {
			codes = append(codes, textencoding.CharCode(b))
		}
	}
	return codes
}

// PdfColor interface represents a generic color in PDF.
type PdfColor interface {
	// Components returns the color's component values in its own colorspace.
	Components() []float64

	// Colorspace returns the colorspace family this color belongs to.
	// For Cal/Lab colors this is a best-effort, parameterless instance; the
	// authoritative parameterized colorspace is tracked by the graphics state.
	Colorspace() PdfColorspace
}

// GetNumComponents returns the number of color components (3 for Lab).
func (c *PdfColorLab) GetNumComponents() int { return 3 }

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

// NonFullScreenPageMode represents the document’s page mode when exiting
// full-screen mode.
type NonFullScreenPageMode string

// Encoder returns the font's text encoder.
func (f *pdfFontSimple) Encoder() textencoding.TextEncoder {
	if f.encoder != nil {
		return f.encoder
	}
	if f.std14Encoder != nil {
		return f.std14Encoder
	}
	simpleTextEncoder, _ := textencoding.NewSimpleTextEncoder("StandardEncoding", nil)
	return simpleTextEncoder
}

func encodeImageData(encoder core.StreamEncoder, img *Image) ([]byte, error) {
	if _, ok := encoder.(*core.DCTEncoder); ok && img.canPassThroughJPEG() {
		return img.encodedData, nil
	}
	return encoder.EncodeBytes(img.Data)
}

// Fill populates `form` with values provided by `provider`.
func (a *PdfAcroForm) Fill(provider FieldValueProvider) error { return a.fill(provider, nil) }

var (
	oidAuthorityKeyIdentifier = asn1.ObjectIdentifier{2, 5, 29, 35}
	oidCRLNumber              = asn1.ObjectIdentifier{2, 5, 29, 20}
	oidCRLReason              = asn1.ObjectIdentifier{2, 5, 29, 21}
	oidInvalidityDate         = asn1.ObjectIdentifier{2, 5, 29, 24}
)

// NewPdfFilespecFromObj creates and returns a new PdfFilespec object.
func NewPdfFilespecFromObj(obj core.PdfObject) (*PdfFilespec, error) {
	filespec := &PdfFilespec{}
	var dict *core.PdfObjectDictionary
	if indirect, ok := core.GetIndirect(obj); ok {
		filespec.primitive = indirect
		dict2, ok := core.GetDict(indirect.PdfObject)
		if !ok {
			common.Log.Debug("Object not a dictionary type")
			return nil, core.ErrTypeError
		}
		dict = dict2
	} else if dict2, ok := core.GetDict(obj); ok {
		filespec.primitive = dict2
		dict = dict2
	} else {
		common.Log.Debug("Object type unexpected (%T)", obj)
		return nil, core.ErrTypeError
	}
	if dict == nil {
		common.Log.Debug("Dictionary missing")
		return nil, errors.New("dict missing")
	}
	if obj := dict.Get("Type"); obj != nil {
		name, ok := obj.(*core.PdfObjectName)
		if !ok {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Incompatibility! Invalid type of Type (%T) - should be Name", obj)
			}
		} else {
			if *name != "Filespec" {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Unsuspected Type != Filespec (%s)", *name)
				}
			}
		}
	}
	if fs := dict.Get("FS"); fs != nil {
		filespec.FS = fs
	}
	if f := dict.Get("F"); f != nil {
		filespec.F = f
	}
	if uf := dict.Get("UF"); uf != nil {
		filespec.UF = uf
	}
	if dos := dict.Get("DOS"); dos != nil {
		filespec.DOS = dos
	}
	if mac := dict.Get("Mac"); mac != nil {
		filespec.Mac = mac
	}
	if unix := dict.Get("Unix"); unix != nil {
		filespec.Unix = unix
	}
	if id := dict.Get("ID"); id != nil {
		filespec.ID = id
	}
	if v := dict.Get("V"); v != nil {
		filespec.V = v
	}
	if ef := dict.Get("EF"); ef != nil {
		filespec.EF = ef
	}
	if rf := dict.Get("RF"); rf != nil {
		filespec.RF = rf
	}
	if desc := dict.Get("Desc"); desc != nil {
		filespec.Desc = desc
	}
	if ci := dict.Get("CI"); ci != nil {
		filespec.CI = ci
	}
	if afRelationship := dict.Get("AFRelationship"); afRelationship != nil {
		filespec.AFRelationship = afRelationship
	}
	return filespec, nil
}

// FieldType is the resolved kind of form field.
type FieldType int

func (f *pdfFontType0) updateSubsetToUnicode(runes []rune, vals []uint16) {
	if f.toUnicodeCMap == nil {
		return
	}
	toUnicodeCMap := f.toUnicodeCMap
	textByCode := make(map[cmap.CharCode]string, len(runes)+len(vals))
	for _, n := range runes {
		runeToCharcode, ok := f.encoder.RuneToCharcode(n)
		if !ok {
			continue
		}
		textByCode[cmap.CharCode(runeToCharcode)] = string(n)
	}
	for _, val := range vals {
		code := cmap.CharCode(val)
		if _, ok := textByCode[code]; ok {
			continue
		}
		if charcodeToUnicode, ok := toUnicodeCMap.CharcodeToUnicode(code); ok {
			textByCode[code] = charcodeToUnicode
		}
	}
	f.toUnicodeCMap = cmap.NewToUnicodeCMap(textByCode)
}

func sha1Digest(data []byte) ([]byte, error) {
	digest := sha1.New()
	if _, err := io.Copy(digest, bytes.NewReader(data)); err != nil {
		return nil, err
	}
	return digest.Sum(nil), nil
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

func (t *trustEvaluator) fetch(key fetchKey, fn func() fetchResult) fetchResult {
	if fetchCache, ok := t.fetchCache[key]; ok {
		return fetchCache
	}
	result := fn()
	if t.fetchCache == nil {
		t.fetchCache = map[fetchKey]fetchResult{}
	}
	t.fetchCache[key] = result
	return result
}

// GetOTFType checks if the font is an OTF font and returns the OTF type if it is
func (f *PdfFont) GetOTFType() *fonts.OtfType {
	if f == nil {
		return nil
	}
	if font, ok := f.context.(*pdfFontType0); ok {
		if descendantFont := font.DescendantFont; descendantFont != nil {
			if font, ok := descendantFont.context.(*pdfCIDFontType0); ok {
				if otfType := font.GetOtfType(); otfType != nil {
					return otfType
				}
			}
			if font, ok := descendantFont.context.(*pdfCIDFontType2); ok {
				if otfType := font.GetOtfType(); otfType != nil {
					return otfType
				}
			}
		}
	}
	return nil
}

// String returns a human readable description of `fontfile`.
func (f *fontFile) String() string {
	s := "[None]"
	if f.encoder != nil {
		s = f.encoder.String()
	}
	return fmt.Sprintf("FONTFILE{%#q encoder=%s}", f.name, s)
}

// GetFillImage get attached model.Image in push button.
func (f *PdfFieldButton) GetFillImage() *Image {
	if f.IsPush() {
		return f.buttonImage
	}
	return nil
}

// DecodeArray returns the range of color component values in DeviceCMYK colorspace.
func (cs *PdfColorspaceDeviceCMYK) DecodeArray() []float64 {
	return []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0, 0.0, 1.0}
}

// SetDirection sets the value of the direction.
func (vp *ViewerPreferences) SetDirection(direction Direction) { vp.direction = direction }

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

// Val returns the value of the color.
func (c *PdfColorCalGray) Val() float64 { return float64(*c) }

// GetColorspaceByName returns the colorspace with the specified name from the page resources.
func (r *PdfPageResources) GetColorspaceByName(keyName core.PdfObjectName) (PdfColorspace, bool) {
	colorspaces, err := r.GetColorspaces()
	if err != nil {
		common.Log.Debug("ERROR getting colorsprace: %v", err)
		return nil, false
	}
	if colorspaces == nil {
		return nil, false
	}
	colorspace, ok := colorspaces.Colorspaces[string(keyName)]
	if !ok {
		return nil, false
	}
	return colorspace, true
}

func cmapFromGlyphMap(chars map[rune]fonts.GID) *cmap.CMap {
	if len(chars) == 0 {
		return nil
	}
	textByCode := make(map[cmap.CharCode]string, len(chars))
	for n, char := range chars {
		code := cmap.CharCode(char)
		s := string(n)
		if s2, ok := textByCode[code]; !ok || (ok && len(s2) > 0 && s2 > s) {
			textByCode[code] = s
		}
	}
	return cmap.NewToUnicodeCMap(textByCode)
}

func (r *PdfReader) newPdfAnnotationRichMediaFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationRichMedia, error) {
	richMedia := &PdfAnnotationRichMedia{}
	richMedia.RichMediaSettings = dict.Get("RichMediaSettings")
	richMedia.RichMediaContent = dict.Get("RichMediaContent")
	return richMedia, nil
}

func (r *PdfReader) newPdfAnnotationTextFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationText, error) {
	text := PdfAnnotationText{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	text.PdfAnnotationMarkup = markup
	text.Open = dict.Get("Open")
	text.Name = dict.Get("Name")
	text.State = dict.Get("State")
	text.StateModel = dict.Get("StateModel")
	return &text, nil
}

func newDSSFromDict(indirect *core.PdfIndirectObject, dict *core.PdfObjectDictionary) (*DSS, error) {
	if indirect == nil {
		indirect = core.MakeIndirectObject(nil)
	}
	indirect.PdfObject = core.MakeDict()
	vriByKey := map[string]*VRI{}
	if vri, ok := core.GetDict(dict.Get("VRI")); ok {
		for _, name := range vri.Keys() {
			if dict, ok := core.GetDict(vri.Get(name)); ok {
				vriByKey[strings.ToUpper(name.String())] = newVRIFromDict(dict)
			}
		}
	}
	return &DSS{Certs: streamsFromObject(dict.Get("Certs")), OCSPs: streamsFromObject(dict.Get("OCSPs")), CRLs: streamsFromObject(dict.Get("CRLs")), VRI: vriByKey, primitive: indirect}, nil
}

func newICCBasedColorspaceFromProfile(iccProfile []byte, cs PdfColorspace) *PdfColorspaceICCBased {
	if cs == nil {
		return nil
	}
	numComponents := cs.GetNumComponents()
	if numComponents != 1 && numComponents != 3 && numComponents != 4 {
		return nil
	}
	if iccProfileNumComponents(iccProfile) != numComponents {
		return nil
	}
	iccBasedCS, err := NewPdfColorspaceICCBased(numComponents)
	if err != nil {
		return nil
	}
	iccBasedCS.Data = iccProfile
	iccBasedCS.Alternate = cs
	return iccBasedCS
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionJavaScript) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeJavaScript)))
	dict.SetIfNotNil("JS", a.JS)
	return indirect
}

// GetEmojiImageSize returns the dimensions of an emoji image if available.
// For SVG emojis, this may return 0,0 as SVG is scalable.
func (f *PdfFont) GetEmojiImageSize(r rune) (n, count int, err error) {
	otfType := f.GetOTFType()
	if otfType == nil {
		return 0, 0, fmt.Errorf("font does not support OTF color glyphs")
	}
	colorGlyphInfoByRune := GetColorGlyphInfoByRune(otfType, r)
	if colorGlyphInfoByRune == nil {
		return 0, 0, fmt.Errorf("no color glyph information found for rune %c (U+%04X)", r, r)
	}
	n, count = colorGlyphInfoByRune.GetImageSize()
	return n, count, nil
}

// GetTrailer returns the PDF's trailer dictionary.
func (r *PdfReader) GetTrailer() (*core.PdfObjectDictionary, error) {
	trailer := r.parser.GetTrailer()
	if trailer == nil {
		return nil, errors.New("trailer missing")
	}
	return trailer, nil
}

// StandardValidator is the interface that is used for the PDF StandardImplementer validation for the PDF document.
// It is using a CompliancePdfReader which is expected to give more Metadata during reading process.
// NOTE: This implementation is in experimental development state.
//
//	Keep in mind that it might change in the subsequent minor versions.
type StandardValidator interface {
	// ValidateStandard checks if the input reader
	ValidateStandard(compliancePdfReader *CompliancePdfReader) error
}

func (r *PdfReader) newPdfActionImportDataFromDict(dict *core.PdfObjectDictionary) (*PdfActionImportData, error) {
	filespec, err := newPdfFilespecFromPdfObject(dict.Get("F"))
	if err != nil {
		return nil, err
	}
	return &PdfActionImportData{F: filespec}, nil
}

func (r *PdfReader) newPdfAnnotationMarkupFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationMarkup, error) {
	markup := &PdfAnnotationMarkup{}
	if t := dict.Get("T"); t != nil {
		markup.T = t
	}
	if popup := dict.Get("Popup"); popup != nil {
		indirect, ok := popup.(*core.PdfIndirectObject)
		if !ok {
			if _, ok := popup.(*core.PdfObjectNull); !ok {
				return nil, errors.New("popup should point to an indirect object")
			}
		} else {
			annot, err := r.newPdfAnnotationFromIndirectObject(indirect)
			if err != nil {
				return nil, err
			}
			if annot != nil {
				popup, ok := annot.context.(*PdfAnnotationPopup)
				if !ok {
					return nil, errors.New("object not referring to a popup annotation")
				}
				markup.Popup = popup
			}
		}
	}
	if ca := dict.Get("CA"); ca != nil {
		markup.CA = ca
	}
	if rc := dict.Get("RC"); rc != nil {
		markup.RC = rc
	}
	if creationDate := dict.Get("CreationDate"); creationDate != nil {
		markup.CreationDate = creationDate
	}
	if irt := dict.Get("IRT"); irt != nil {
		markup.IRT = irt
	}
	if subj := dict.Get("Subj"); subj != nil {
		markup.Subj = subj
	}
	if rt := dict.Get("RT"); rt != nil {
		markup.RT = rt
	}
	if it := dict.Get("IT"); it != nil {
		markup.IT = it
	}
	if exData := dict.Get("ExData"); exData != nil {
		markup.ExData = exData
	}
	return markup, nil
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionThread) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeThread)))
	if a.F != nil {
		dict.Set("F", a.F.ToPdfObject())
	}
	dict.SetIfNotNil("D", a.D)
	dict.SetIfNotNil("B", a.B)
	return indirect
}

func finalizeLinearizationDict(writer *PdfWriter, plan *linearizedPlan, inputs convergedPlanInputs) error {
	plan.linDict = buildLinearizationDict(linearizationDictObjNum2, plan.fileLength, plan.hintOffset, int64(inputs.hintLen), plan.firstPageEnd, inputs.layout.pageCount, inputs.xrefFirstEntryOffset, inputs.layout.firstPageObjNum)
	if len(plan.linDict) != inputs.linDictLen {
		return fmt.Errorf("plan: linearization dict size changed (placeholder=%d, final=%d)", inputs.linDictLen, len(plan.linDict))
	}
	if inputs.objStreams == nil {
		plan.firstXref = buildFirstXrefTable(writer, inputs.firstXrefObjs, inputs.objects, inputs.hintObjNum, plan.hintOffset, plan.mainXrefOffset, plan.linDictOffset, inputs.trailerSize)
		if len(plan.firstXref) != inputs.firstXrefLen {
			return fmt.Errorf("plan: first xref size changed (placeholder=%d, final=%d)", inputs.firstXrefLen, len(plan.firstXref))
		}
	}
	var buf bytes.Buffer
	for _, firstSection := range inputs.layout.firstSection {
		buf.Write(inputs.objects[firstSection].data)
	}
	plan.firstSection = buf.Bytes()
	if int64(len(plan.firstSection)) != plan.firstPageEnd-inputs.firstSectionStart {
		return fmt.Errorf("plan: first section byte count mismatch")
	}
	var buf2 bytes.Buffer
	for _, mainSection := range inputs.layout.mainSection {
		buf2.Write(inputs.objects[mainSection].data)
	}
	plan.mainSection = buf2.Bytes()
	if int64(len(plan.mainSection)) != plan.mainXrefOffset-inputs.mainSectionStart {
		return fmt.Errorf("plan: main section byte count mismatch")
	}
	if inputs.objStreams != nil {
		s := fmt.Sprintf("startxref\r\n%010d\r\n%%%%EOF\r\n", plan.firstXrefOffset)
		plan.mainXref = append(plan.mainXref, []byte(s)...)
	}
	return nil
}

// SetCatalogStructTreeRoot sets the catalog struct tree root object.
func (w *PdfWriter) SetCatalogStructTreeRoot(tree core.PdfObject) error {
	if tree == nil {
		w.catalog.Remove("StructTreeRoot")
		return nil
	}
	w.catalog.Set("StructTreeRoot", tree)
	return w.addObjects(tree)
}

// ToPdfObject converts the pdfCIDFontType2 to a PDF representation.
func (f *pdfCIDFontType2) ToPdfObject() core.PdfObject {
	if f.primitive == nil {
		f.primitive = &core.PdfIndirectObject{}
	}
	dict := f.baseFields().asPdfObjectDictionary("CIDFontType2")
	f.primitive.PdfObject = dict
	if f.CIDSystemInfo != nil {
		dict.Set("CIDSystemInfo", f.CIDSystemInfo)
	}
	if f.DW != nil {
		dict.Set("DW", f.DW)
	}
	if f.DW2 != nil {
		dict.Set("DW2", f.DW2)
	}
	if f.W != nil {
		dict.Set("W", f.W)
	}
	if f.W2 != nil {
		dict.Set("W2", f.W2)
	}
	if f.CIDToGIDMap != nil {
		dict.Set("CIDToGIDMap", f.CIDToGIDMap)
	}
	return f.primitive
}

// SignWithAppearance signs a specific page with a digital signature, like
// Sign, and generates the widget appearance of `field` using `appGen`.
// Set the signature dictionary entries displayed by the generated appearance
// (e.g. Name, M, Reason, Location) before calling. An appearance the caller
// already attached to the field widget is honored according to the `appGen`
// implementation (the default annotator.FieldAppearance leaves it untouched).
// If `appGen` is nil, it behaves exactly like Sign.
func (a *PdfAppender) SignWithAppearance(pageNum int, field *PdfFieldSignature, appGen FieldAppearanceGenerator) error {
	if field == nil {
		return errors.New("signature field cannot be nil")
	}
	if field.V == nil {
		return errors.New("signature dictionary cannot be nil")
	}
	if pageNum < 1 || pageNum > len(a.pages) {
		return fmt.Errorf("page %d not found", pageNum)
	}
	if appGen != nil {
		if field.PdfAnnotationWidget == nil {
			return errors.New("signature field has no widget annotation")
		}
		if a.acroForm == a.roReader.AcroForm {
			a.acroForm = a.Reader.AcroForm
		}
		acroForm := a.acroForm
		if acroForm == nil {
			acroForm = NewPdfAcroForm()
			a.acroForm = acroForm
		}
		generateAppearanceDict, err := appGen.GenerateAppearanceDict(acroForm, field.PdfField, field.PdfAnnotationWidget)
		if err != nil {
			return err
		}
		if generateAppearanceDict != nil {
			field.PdfAnnotationWidget.AP = generateAppearanceDict
		}
	}
	return a.Sign(pageNum, field)
}

// ToPdfObject returns colorspace in a PDF object format [name dictionary]
func (cs *PdfColorspaceCalRGB) ToPdfObject() core.PdfObject {
	arr := &core.PdfObjectArray{}
	arr.Append(core.MakeName("CalRGB"))
	dict := core.MakeDict()
	if cs.WhitePoint != nil {
		array := core.MakeArray(core.MakeFloat(cs.WhitePoint[0]), core.MakeFloat(cs.WhitePoint[1]), core.MakeFloat(cs.WhitePoint[2]))
		dict.Set("WhitePoint", array)
	} else {
		common.Log.Error("CalRGB: Missing WhitePoint (Required)")
	}
	if cs.BlackPoint != nil {
		array := core.MakeArray(core.MakeFloat(cs.BlackPoint[0]), core.MakeFloat(cs.BlackPoint[1]), core.MakeFloat(cs.BlackPoint[2]))
		dict.Set("BlackPoint", array)
	}
	if cs.Gamma != nil {
		array := core.MakeArray(core.MakeFloat(cs.Gamma[0]), core.MakeFloat(cs.Gamma[1]), core.MakeFloat(cs.Gamma[2]))
		dict.Set("Gamma", array)
	}
	if cs.Matrix != nil {
		array := core.MakeArray(core.MakeFloat(cs.Matrix[0]), core.MakeFloat(cs.Matrix[1]), core.MakeFloat(cs.Matrix[2]), core.MakeFloat(cs.Matrix[3]), core.MakeFloat(cs.Matrix[4]), core.MakeFloat(cs.Matrix[5]), core.MakeFloat(cs.Matrix[6]), core.MakeFloat(cs.Matrix[7]), core.MakeFloat(cs.Matrix[8]))
		dict.Set("Matrix", array)
	}
	arr.Append(dict)
	if cs.container != nil {
		cs.container.PdfObject = arr
		return cs.container
	}
	return arr
}

func encryptedWriterCatalog(writer *PdfWriter) *core.PdfIndirectObject {
	if writer.crypter == nil {
		return nil
	}
	return writer.encryptObj
}

// PickTrayByPDFSize returns the value of the pickTrayByPDFSize flag.
func (vp *ViewerPreferences) PickTrayByPDFSize() bool {
	if vp.pickTrayByPDFSize == nil {
		return false
	}
	return *vp.pickTrayByPDFSize
}

// Val returns the color value.
func (c *PdfColorDeviceGray) Val() float64 { return float64(*c) }

func (l *LTV) buildCertChain(certs, certs2 []*x509.Certificate) ([]*x509.Certificate, map[string]*x509.Certificate, error) {
	certByKey := map[string]*x509.Certificate{}
	for _, cert := range certs {
		certByKey[cert.Subject.CommonName] = cert
	}
	certs3 := certs
	for _, cert := range certs2 {
		commonName := cert.Subject.CommonName
		if _, ok := certByKey[commonName]; ok {
			continue
		}
		certByKey[commonName] = cert
		certs3 = append(certs3, cert)
	}
	if len(certs3) == 0 {
		return nil, nil, ErrSignNoCertificates
	}
	var err error
	for cert := certs3[0]; cert != nil && !l.CertClient.IsCA(cert); {
		cert2, ok := certByKey[cert.Issuer.CommonName]
		if !ok {
			if cert2, err = l.CertClient.GetIssuer(cert); err != nil {
				common.Log.Debug("WARN: Could not retrieve certificate issuer: %v", err)
				break
			}
			certByKey[cert.Issuer.CommonName] = cert2
			certs3 = append(certs3, cert2)
		}
		cert = cert2
	}
	return certs3, certByKey, nil
}

// NewPdfShadingPatternType3 creates an empty shading pattern type 3 object.
func NewPdfShadingPatternType3() *PdfShadingPatternType3 {
	shadingPatternType3 := &PdfShadingPatternType3{}
	shadingPatternType3.Matrix = core.MakeArrayFromIntegers([]int{1, 0, 0, 1, 0, 0})
	shadingPatternType3.PdfPattern = &PdfPattern{}
	shadingPatternType3.PdfPattern.PatternType = int64(*core.MakeInteger(2))
	shadingPatternType3.PdfPattern.context = shadingPatternType3
	shadingPatternType3.PdfPattern.primitive = core.MakeIndirectObject(core.MakeDict())
	return shadingPatternType3
}

// AllFieldValues returns the value of every terminal (leaf) field of the
// form, in document order. Values are returned verbatim: text as the stored
// PDF text string, check box and radio button states as names, multi-select
// choice values as arrays. A field without a value of its own inherits the
// value of its nearest ancestor that has one. Non-terminal fields are
// omitted, as are fields whose fully qualified name cannot be determined.
// One entry is returned per name: widget kids that carry field entries load
// as unnamed child fields and share their parent's name, and the first of
// them with a value supplies it.
func (a *PdfAcroForm) AllFieldValues() []FieldValue {
	var fieldValues []FieldValue
	numByKey := map[string]int{}
	for _, allField := range a.AllFields() {
		if !allField.IsTerminal() {
			continue
		}
		fullName, err := allField.FullName()
		if err != nil || fullName == "" {
			common.Log.Debug("WARN: skipping form field %q with unresolvable name: %v", fullName, err)
			continue
		}
		obj := core.TraceToDirectObject(allField.inheritedValue())
		if n, ok := numByKey[fullName]; ok {
			if fieldValues[n].Value == nil && obj != nil {
				fieldValues[n].Value = obj
				fieldValues[n].Field = allField
			}
			continue
		}
		numByKey[fullName] = len(fieldValues)
		fieldValues = append(fieldValues, FieldValue{Name: fullName, Value: obj, Field: allField})
	}
	return fieldValues
}

// CustomKeys returns all custom info keys as list.
func (i *PdfInfo) CustomKeys() []string {
	if i.dict == nil {
		return nil
	}
	strs := make([]string, len(i.dict.Keys()))
	for _, name := range i.dict.Keys() {
		strs = append(strs, name.String())
	}
	return strs
}

// ColorFromPdfObjects returns a new PdfColor based on the input slice of color
// components. The slice should contain three PdfObjectFloat elements representing
// the L, A and B components of the color.
func (cs *PdfColorspaceLab) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) != 3 {
		return nil, errors.New("range check")
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objects)
	if err != nil {
		return nil, err
	}
	return cs.ColorFromFloats(numbersAsFloat)
}

func toUnicodeCMapFromObject(toUnicode core.PdfObject, fc *fontCommon) (*cmap.CMap, error) {
	toUnicodeStream, ok := core.GetStream(toUnicode)
	if !ok {
		common.Log.Debug("ERROR: toUnicodeToCmap: Not a stream (%T)", toUnicode)
		return nil, core.ErrTypeError
	}
	stream, err := core.DecodeStream(toUnicodeStream)
	if err != nil {
		return nil, err
	}
	cm, err := cmap.LoadCmapFromData(stream, !fc.isCIDFont())
	if err != nil {
		common.Log.Debug("ERROR: ObjectNumber=%d err=%v", toUnicodeStream.ObjectNumber, err)
	}
	return cm, err
}

func (t *trustEvaluator) evaluate(sig *PdfSignature, result *SignatureValidationResult, certs []*x509.Certificate) {
	result.IsTrusted = false
	result.TrustChain = nil
	result.TrustErrors = nil
	result.TrustWarnings = nil
	result.ValidatedAt = time.Time{}
	result.RevocationStatus = RevocationStatusNotChecked
	result.RevocationTime = time.Time{}
	if t.opts == nil || t.opts.Roots == nil {
		result.TrustErrors = append(result.TrustErrors, noTrustRootsMessage)
		return
	}
	if !result.IsVerified {
		result.TrustErrors = append(result.TrustErrors, "signature integrity not verified")
		return
	}
	if len(certs) == 0 {
		result.TrustErrors = append(result.TrustErrors, "signer certificate not identified by the signature handler")
		return
	}
	cert := certs[0]
	if cert.KeyUsage != 0 && cert.KeyUsage&(x509.KeyUsageDigitalSignature|x509.KeyUsageContentCommitment) == 0 {
		result.TrustErrors = append(result.TrustErrors, "signer certificate key usage does not permit digital signatures")
		return
	}
	s, _ := core.GetNameVal(sig.SubFilter)
	ok := s == "ETSI.RFC3161"
	if ok {
		if !hasExtKeyUsage(cert, x509.ExtKeyUsageTimeStamping) {
			result.TrustErrors = append(result.TrustErrors, "timestamp certificate lacks the time stamping extended key usage")
			return
		}
	} else if !hasAcceptableKeyUsage(cert) {
		result.TrustErrors = append(result.TrustErrors, "signer certificate extended key usage does not permit document signing")
		return
	}
	var p7 *pkcs7.PKCS7
	if sig.Contents != nil {
		p7, _ = pkcs7.Parse(sig.Contents.Bytes())
	}
	s2, _ := signatureContentsDigestHex(sig)
	vriCertificates := t.vriCertificates(s2)
	var evidence revocationEvidence
	if t.opts.Revocation != RevocationCheckNone {
		evidence = t.embeddedRevocationEvidence(p7, s2)
	}
	var found bool
	result.ValidatedAt, found = t.validationTime(p7, result, vriCertificates, evidence)
	usage := x509.ExtKeyUsageAny
	if ok {
		usage = x509.ExtKeyUsageTimeStamping
	}
	verify, err := cert.Verify(x509.VerifyOptions{Roots: t.opts.Roots, Intermediates: t.intermediatePool(vriCertificates, certs[1:]), CurrentTime: result.ValidatedAt, KeyUsages: []x509.ExtKeyUsage{usage}})
	if err != nil {
		result.TrustErrors = append(result.TrustErrors, err.Error())
		return
	}
	certs2 := verify[0]
	if !ok {
		if certs2 = findChainWithValidIntermediates(verify, hasAcceptableKeyUsage); certs2 == nil {
			result.TrustErrors = append(result.TrustErrors, "intermediate CA certificate extended key usage does not permit document signing")
			return
		}
	}
	result.TrustChain = certs2
	result.IsTrusted = true
	if t.opts.Revocation != RevocationCheckNone {
		t.evaluateRevocation(result, found, evidence)
	}
}

// SetReason sets the `Reason` field of the signature.
func (s *PdfSignature) SetReason(reason string) {
	s.Reason = core.MakeEncodedString(reason, true)
}

// ColorToRGB converts a Lab color to an RGB color.
func (cs *PdfColorspaceLab) ColorToRGB(color PdfColor) (PdfColor, error) {
	fn := func(f float64) float64 {
		if f >= 6.0/29 {
			return f * f * f
		}
		return 108.0 / 841 * (f - 4.0/29.0)
	}
	labColor, ok := color.(*PdfColorLab)
	if !ok {
		common.Log.Debug("input color not lab")
		return nil, errors.New("type check error")
	}
	LStar := labColor.L()
	AStar := labColor.A()
	BStar := labColor.B()
	L := (LStar+16)/116 + AStar/500
	M := (LStar + 16) / 116
	N := (LStar+16)/116 - BStar/200
	X := cs.WhitePoint[0] * fn(L)
	Y := cs.WhitePoint[1] * fn(M)
	Z := cs.WhitePoint[2] * fn(N)
	f := 3.240479*X + -1.537150*Y + -0.498535*Z
	v := -0.969256*X + 1.875992*Y + 0.041556*Z
	val := 0.055648*X + -0.204043*Y + 1.057311*Z
	f = math.Min(math.Max(f, 0), 1.0)
	v = math.Min(math.Max(v, 0), 1.0)
	val = math.Min(math.Max(val, 0), 1.0)
	return NewPdfColorDeviceRGB(f, v, val), nil
}

// Duplex represents the paper handling option that shall be used when printing
// the file from the print dialog.
type Duplex string

func (f *PdfFont) baseFields() *fontCommon {
	if f.context == nil {
		common.Log.Debug("ERROR: baseFields. context is nil.")
		return nil
	}
	return f.context.baseFields()
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

// NewPdfPageResources returns a new PdfPageResources object.
func NewPdfPageResources() *PdfPageResources {
	resources := &PdfPageResources{}
	resources.primitive = core.MakeDict()
	return resources
}

// ToPdfObject converts rectangle to a PDF object.
func (r *PdfRectangle) ToPdfObject() core.PdfObject {
	return core.MakeArray(core.MakeFloat(r.Llx), core.MakeFloat(r.Lly), core.MakeFloat(r.Urx), core.MakeFloat(r.Ury))
}

// ToPdfObject converts the ID tree to a PDF object.
func (t *IDTree) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	if t.Names != nil && t.Names.Len() > 0 {
		dict.Set("Names", t.Names)
		dict.Set("Limits", t.Limits)
	}
	if len(t.Kids) > 0 {
		array := core.MakeArray()
		for _, kid := range t.Kids {
			array.Append(kid.ToPdfObject())
		}
		dict2 := core.MakeDict()
		dict2.Set("Kids", array)
		dict.Set("Kids", dict2)
	}
	return dict
}

// PdfActionSound represents a sound action.
type PdfActionSound struct {
	*PdfAction
	Sound       core.PdfObject
	Volume      core.PdfObject
	Synchronous core.PdfObject
	Repeat      core.PdfObject
	Mix         core.PdfObject
}

// PdfShadingType5 is a Lattice-form Gouraud-shaded triangle mesh.
type PdfShadingType5 struct {
	*PdfShading
	BitsPerCoordinate *core.PdfObjectInteger
	BitsPerComponent  *core.PdfObjectInteger
	VerticesPerRow    *core.PdfObjectInteger
	Decode            *core.PdfObjectArray
	Function          []PdfFunction
}

// ToPdfObject convert PdfInfo to pdf object.
func (i *PdfInfo) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	dict.SetIfNotNil("Title", i.Title)
	dict.SetIfNotNil("Author", i.Author)
	dict.SetIfNotNil("Subject", i.Subject)
	dict.SetIfNotNil("Keywords", i.Keywords)
	dict.SetIfNotNil("Creator", i.Creator)
	dict.SetIfNotNil("Producer", i.Producer)
	dict.SetIfNotNil("Trapped", i.Trapped)
	if i.CreationDate != nil {
		dict.SetIfNotNil("CreationDate", i.CreationDate.ToPdfObject())
	}
	if i.ModifiedDate != nil {
		dict.SetIfNotNil("ModDate", i.ModifiedDate.ToPdfObject())
	}
	for _, name := range i.dict.Keys() {
		dict.SetIfNotNil(name, i.dict.Get(name))
	}
	return dict
}

// PdfAnnotationText represents Text annotations.
// (Section 12.5.6.4 p. 402).
type PdfAnnotationText struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	Open       core.PdfObject
	Name       core.PdfObject
	State      core.PdfObject
	StateModel core.PdfObject
}

// ColorFromPdfObjects gets the color from a series of pdf objects (4 for cmyk).
func (cs *PdfColorspaceDeviceCMYK) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) != 4 {
		return nil, errors.New("range check")
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objects)
	if err != nil {
		return nil, err
	}
	return cs.ColorFromFloats(numbersAsFloat)
}

// GetPageNumber returns the page number that has been assigned to the K object.
func (k *KDict) GetPageNumber() int64 { return k.pageNumber }

// ColorFromFloats returns a new PdfColor based on the input slice of color
// components. The slice should contain a single element.
func (cs *PdfColorspaceSpecialSeparation) ColorFromFloats(vals []float64) (PdfColor, error) {
	if len(vals) != 1 {
		return nil, errors.New("range check")
	}
	val := vals[0]
	floats := []float64{val}
	evaluate, err := cs.TintTransform.Evaluate(floats)
	if err != nil {
		common.Log.Debug("Error, failed to evaluate: %v", err)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Tint transform: %+v", cs.TintTransform)
		}
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Processing ColorFromFloats(%+v) on AlternateSpace: %#v", evaluate, cs.AlternateSpace)
	}
	col, err := cs.AlternateSpace.ColorFromFloats(evaluate)
	if err != nil {
		common.Log.Debug("Error, failed to evaluate in alternate space: %v", err)
		return nil, err
	}
	return col, nil
}

// GenerateKDict generates K Dictionary based on existing Annotation type.
func (a *PdfAnnotation) GenerateKDict() *KDict {
	if a == nil {
		return nil
	}
	kDictionary := NewKDictionary()
	kDictionary.S = core.MakeName(string(StructureTypeAnnot))
	kDictionary.AddRefChild(a.primitive)
	return kDictionary
}

// GetNumPages returns the number of pages in the document.
func (r *PdfReader) GetNumPages() (int, error) {
	if r.parser.GetCrypter() != nil && !r.parser.IsAuthenticated() {
		return 0, fmt.Errorf("file need to be decrypted first")
	}
	return len(r.pageObjs), nil
}

func (p *PdfPage) loadFont(s string) (*truetype.Font, error) {
	file, err := os.ReadFile(s)
	if err != nil {
		return nil, err
	}
	font, err := freetype.ParseFont(file)
	if err != nil {
		return nil, err
	}
	return font, nil
}

// AddPages adds pages to be appended to the end of the source PDF.
func (a *PdfAppender) AddPages(pages ...*PdfPage) {
	for _, page := range pages {
		page = page.Duplicate()
		pageToPdfObjectLicensed(page)
		a.pages = append(a.pages, page)
	}
}

// PdfColorspaceDeviceRGB represents an RGB colorspace.
type PdfColorspaceDeviceRGB struct{}

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

func (t *trustEvaluator) validationTime(p7 *pkcs7.PKCS7, result *SignatureValidationResult, certs []*x509.Certificate, evidence revocationEvidence) (t2 time.Time, ok bool) {
	if !t.opts.ValidationTime.IsZero() {
		return t.opts.ValidationTime, false
	}
	t2, strs, strs2 := t.trustedSignatureTimestamp(p7, certs, evidence)
	if !t2.IsZero() {
		result.TrustWarnings = append(result.TrustWarnings, strs...)
		return t2, false
	}
	for _, str := range strs2 {
		result.TrustWarnings = append(result.TrustWarnings, "signature timestamp ignored: "+str)
	}
	return time.Now(), true
}
func (f *pdfFontSimple) baseFields() *fontCommon { return &f.fontCommon }
func (w *PdfWriter) writeObjectsInStreams(set map[core.PdfObject]bool) error {
	for _, object := range w.objects {
		if ok := set[object]; ok {
			continue
		}
		n := int64(0)
		switch v := object.(type) {
		case *core.PdfIndirectObject:
			n = v.ObjectNumber
		case *core.PdfObjectStream:
			n = v.ObjectNumber
		case *core.PdfObjectStreams:
			n = v.ObjectNumber
		case *core.PdfObjectDictionary, *core.PdfObjectString:
		default:
			common.Log.Debug("ERROR: Unsupported type in writer objects: %T (type %T)", object, v)
			return ErrTypeCheck
		}
		_, ok := object.(*core.PdfObjectStreams)
		if w.crypter != nil && object != w.encryptObj && !ok {
			w.encrypted = true
			err := w.crypter.Encrypt(object, n, 0)
			if err != nil {
				common.Log.Debug("ERROR: Failed encrypting (%s)", err)
				return err
			}
		}
		if err := w.writeObject(int(n), object); err != nil {
			return err
		}
	}
	return nil
}

// SetViewArea sets the value of the viewArea.
func (vp *ViewerPreferences) SetViewArea(viewArea PageBoundary) { vp.viewArea = viewArea }

func (r *PdfReader) newPdfFieldSignatureFromDict(dict *core.PdfObjectDictionary) (*PdfFieldSignature, error) {
	signatureField := &PdfFieldSignature{}
	v, ok := core.GetIndirect(dict.Get("V"))
	if ok {
		var err error
		signatureField.V, err = r.newPdfSignatureFromIndirect(v)
		if err != nil {
			return nil, err
		}
	}
	signatureField.Lock, _ = core.GetIndirect(dict.Get("Lock"))
	signatureField.SV, _ = core.GetIndirect(dict.Get("SV"))
	return signatureField, nil
}

// SetVersion sets the PDF version of the output file.
func (w *PdfWriter) SetVersion(majorVersion, minorVersion int) {
	w.version.Major = majorVersion
	w.version.Minor = minorVersion
}

// SetPrintArea sets the value of the printArea.
func (vp *ViewerPreferences) SetPrintArea(printArea PageBoundary) { vp.printArea = printArea }

func appearanceFormAndRect(annot *PdfAnnotation) (*XObjectForm, *PdfRectangle, error) {
	dict, ok := core.GetDict(annot.AP)
	if !ok {
		return nil, nil, errors.New("field missing AP dictionary")
	}
	if dict == nil {
		return nil, nil, nil
	}
	rectArr, ok := core.GetArray(annot.Rect)
	if !ok || rectArr.Len() != 4 {
		return nil, nil, errors.New("rect invalid")
	}
	rect, err := NewPdfRectangle(*rectArr)
	if err != nil {
		return nil, nil, err
	}
	n := core.TraceToDirectObject(dict.Get("N"))
	switch v := n.(type) {
	case *core.PdfObjectStream:
		stream := v
		xObjectForm, err := NewXObjectFormFromStream(stream)
		return xObjectForm, rect, err
	case *core.PdfObjectDictionary:
		dict := v
		if dict == nil {
			common.Log.Debug("ERROR: Failed to get appearance. Dictionary is nil")
			return nil, nil, nil
		}
		var obj core.PdfObject
		name, ok := core.GetName(annot.AS)
		if ok {
			obj = dict.Get(*name)
		} else {
			dict2 := annot.primitive.PdfObject.(*core.PdfObjectDictionary)
			if dict2 == nil {
				common.Log.Debug("ERROR: Failed to get appearance from value.")
				return nil, nil, nil
			}
			if v := dict2.Get("V"); v != nil {
				obj = dict.Get(core.PdfObjectName(v.String()))
			}
		}
		if obj == nil {
			common.Log.Debug("ERROR: AS state not specified in AP dict - ignoring")
			return nil, nil, nil
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			common.Log.Debug("ERROR: Unable to access appearance stream for %v", name)
			return nil, nil, errors.New("stream missing")
		}
		xObjectForm, err := NewXObjectFormFromStream(stream)
		return xObjectForm, rect, err
	}
	common.Log.Debug("Invalid type for N: %T", n)
	return nil, nil, errors.New("type check error")
}

// GetCharMetrics returns the char metrics for character code `code`.
func (f pdfFontType3) GetCharMetrics(code textencoding.CharCode) (fonts.CharMetrics, bool) {
	if charWidth, ok := f.charWidths[code]; ok {
		return fonts.CharMetrics{Wx: charWidth}, true
	}
	if fonts.IsStdFont(fonts.StdFontName(f.baseFont)) {
		return fonts.CharMetrics{Wx: 250}, true
	}
	return fonts.CharMetrics{}, false
}

// DecodeArray returns the component range values for the Indexed colorspace.
func (cs *PdfColorspaceSpecialIndexed) DecodeArray() []float64 {
	return []float64{0, float64(cs.HiVal)}
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

// String returns a string that describes `font`.
func (f *PdfFont) String() string {
	s := ""
	if f.context.Encoder() != nil {
		s = f.context.Encoder().String()
	}
	return fmt.Sprintf("FONT{%T %s %s}", f.context, f.baseFields().coreString(), s)
}

func planLinearization(writer *PdfWriter, layout *linearizationLayout, preObjs []preserializedObject, n int64, objStm *objectStreamsState) (*linearizedPlan, error) {
	if len(preObjs) == 0 {
		return nil, fmt.Errorf("linearization plan: nothing to serialize")
	}
	data := []byte(fmt.Sprintf("\x25\x25\120\104\106\x2d\045\x64\056\x25\x64\x0a\x25\x25\342\xe3\xcf\323\012", writer.version.Major, writer.version.Minor))
	preObjByNum := make(map[int64]preserializedObject, len(preObjs))
	for _, preObj := range preObjs {
		preObjByNum[preObj.objNum] = preObj
	}
	linearizationDict := buildLinearizationDict(linearizationDictObjNum2, 0, 0, 0, 0, 0, 0, 0)
	count := len(linearizationDict)
	nums := append([]int64{linearizationDictObjNum2}, layout.firstSection...)
	nums = append(nums, n)
	if objStm != nil {
		nums = append(nums, objStm.firstXrefObjNum)
	}
	sort.Slice(nums, func(n, count int) bool { return nums[n] < nums[count] })
	nums2 := append([]int64(nil), layout.mainSection...)
	if objStm != nil {
		nums2 = append(nums2, objStm.mainXrefObjNum)
		nums2 = append(nums2, objStm.firstXrefObjNum)
		nums2 = append(nums2, objStm.packedObjNums...)
	}
	sort.Slice(nums2, func(n, count int) bool { return nums2[n] < nums2[count] })
	num := nums[len(nums)-1]
	if len(nums2) > 0 && nums2[len(nums2)-1] > num {
		num = nums2[len(nums2)-1]
	}
	size := -1
	length := -1
	length2 := -1
	for i := 0; i < maxLinearizationIterations; i++ {
		plan, sizes, err := planLinearizationPass(writer, layout, preObjByNum, n, objStm, data, count, nums, nums2, num, size, length, length2)
		if err != nil {
			return nil, err
		}
		if sizes.hintLen == size && sizes.firstXrefLen == length && sizes.mainXrefLen == length2 {
			updateSignatureByteRangeOffsets(preObjByNum)
			return plan, nil
		}
		size = sizes.hintLen
		length = sizes.firstXrefLen
		length2 = sizes.mainXrefLen
	}
	return nil, fmt.Errorf("linearization plan: variable-length sections did not stabilize in %d iterations", maxLinearizationIterations)
}

const (
	XObjectTypeUndefined XObjectType = iota
	XObjectTypeImage
	XObjectTypeForm
	XObjectTypePS
	XObjectTypeUnknown
)

// Optimizer is the interface that performs optimization of PDF object structure for output writing.
//
// Optimize receives a slice of input `objects`, performs optimization, including removing, replacing objects and
// output the optimized slice of objects.
type Optimizer interface {
	Optimize(objs []core.PdfObject) ([]core.PdfObject, error)
}

func (r *PdfReader) newPdfActionSetOCGStateFromDict(dict *core.PdfObjectDictionary) (*PdfActionSetOCGState, error) {
	return &PdfActionSetOCGState{State: dict.Get("State"), PreserveRB: dict.Get("PreserveRB")}, nil
}

// ToInteger convert to an integer format.
func (c *PdfColorDeviceRGB) ToInteger(bits int) [3]uint32 {
	f := math.Pow(2, float64(bits)) - 1
	return [3]uint32{uint32(f * c.R()), uint32(f * c.G()), uint32(f * c.B())}
}

// ToUnicode returns the name of the font's "ToUnicode" field if there is one, or "" if there isn't.
func (f *PdfFont) ToUnicode() string {
	if f.baseFields().toUnicodeCMap == nil {
		return ""
	}
	return f.baseFields().toUnicodeCMap.Name()
}

// GetRevisionNumber returns the version of the current Pdf document
func (r *PdfReader) GetRevisionNumber() int { return r.parser.GetRevisionNumber() }

// ToPdfObject returns the button field dictionary within an indirect object.
func (f *PdfFieldButton) ToPdfObject() core.PdfObject {
	f.PdfField.ToPdfObject()
	indirect := f.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.Set("FT", core.MakeName("Btn"))
	if f.Opt != nil {
		dict.Set("Opt", f.Opt)
	}
	return indirect
}

// NewPdfAnnotationText returns a new text annotation.
func NewPdfAnnotationText() *PdfAnnotationText {
	annot := NewPdfAnnotation()
	text := &PdfAnnotationText{}
	text.PdfAnnotation = annot
	text.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(text)
	return text
}

func newCryptFilter(algo EncryptionAlgorithm) (crypt.Filter, error) {
	switch algo {
	case RC4_128bit:
		return crypt.NewFilterV2(16), nil
	case AES_128bit:
		return crypt.NewFilterAESV2(), nil
	case AES_256bit:
		return crypt.NewFilterAESV3(), nil
	}
	return nil, fmt.Errorf("unsupported algorithm: %v", algo)
}

// PdfAnnotationPolygon represents Polygon annotations.
// (Section 12.5.6.9).
type PdfAnnotationPolygon struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	Vertices core.PdfObject
	LE       core.PdfObject
	BS       core.PdfObject
	IC       core.PdfObject
	BE       core.PdfObject
	IT       core.PdfObject
	Measure  core.PdfObject
}

// AddKDict adds a K dictionary object to the structure tree root.
func (r *StructTreeRoot) AddKDict(k *KDict) { r.K = append(r.K, k) }

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

func (r *PdfReader) traverseObjectData(obj core.PdfObject) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return core.ResolveReferencesDeep(obj, r.traversed)
}

// NewPdfColorPatternType2 returns an empty color shading pattern type 2 (Axial).
func NewPdfColorPatternType2() *PdfColorPatternType2 {
	patternType2Color := &PdfColorPatternType2{}
	return patternType2Color
}

// String returns a string representation of PdfTransformParamsDocMDP.
func (p *PdfTransformParamsDocMDP) String() string {
	return fmt.Sprintf("%s P: %s V: %s", p.Type, p.P, p.V)
}

// PdfActionGoTo represents a GoTo action.
type PdfActionGoTo struct {
	*PdfAction
	D core.PdfObject
}

// GetCharMetrics returns the char metrics for character code `code`.
func (f pdfCIDFontType2) GetCharMetrics(code textencoding.CharCode) (fonts.CharMetrics, bool) {
	if charWidth, ok := f.charWidths[code]; ok {
		return fonts.CharMetrics{Wx: charWidth}, true
	}
	n := rune(code)
	runeToGID, ok := f.runeToGID[n]
	if !ok {
		runeToGID = int(f.defaultWidth)
	}
	return fonts.CharMetrics{Wx: float64(runeToGID)}, true
}

func (img *Image) samplesAddPadding(samples []uint32) []uint32 {
	n := imageutil.BytesPerLine(int(img.Width), int(img.BitsPerComponent), img.ColorComponents) * (8 / int(img.BitsPerComponent))
	count := n * int(img.Height)
	if len(samples) == count {
		return samples
	}
	nums := make([]uint32, count)
	num := int(img.Width) * img.ColorComponents
	for i := 0; i < int(img.Height); i++ {
		size := i * int(img.Width)
		length := i * n
		for i := 0; i < num; i++ {
			nums[length+i] = samples[size+i]
		}
	}
	return nums
}

// GetContentStream returns the XObject Form's content stream.
func (x *XObjectForm) GetContentStream() ([]byte, error) {
	stream, err := core.DecodeStream(x.primitive)
	if err != nil {
		return nil, err
	}
	return stream, nil
}

// XObjectType represents the type of an XObject.
type XObjectType int

func crlIsCurrent(thisUpdate, nextUpdate time.Time, cert *x509.Certificate, t time.Time, online bool) bool {
	if thisUpdate.After(cert.NotAfter) {
		return false
	}
	if nextUpdate.IsZero() {
		return online && time.Since(thisUpdate) <= maxCRLAge
	}
	return !t.After(nextUpdate)
}

func newFontFileFromPdfObject(obj core.PdfObject) (*fontFile, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("newFontFileFromPdfObject: obj=%s", obj)
	}
	file := &fontFile{}
	obj = core.TraceToDirectObject(obj)
	stream, ok := obj.(*core.PdfObjectStream)
	if !ok {
		common.Log.Debug("ERROR: FontFile must be a stream (%T)", obj)
		return nil, core.ErrTypeError
	}
	dict := stream.PdfObjectDictionary
	data, err := core.DecodeStream(stream)
	if err != nil {
		return nil, err
	}
	subtype, ok := core.GetNameVal(dict.Get("Subtype"))
	if !ok {
		file.subtype = subtype
		if subtype == "Type1C" {
			common.Log.Debug("Type1C fonts are currently not supported")
			return nil, ErrType1CFontNotSupported
		}
	}
	length1, _ := core.GetIntVal(dict.Get("Length1"))
	length2, _ := core.GetIntVal(dict.Get("Length2"))
	if length1 > len(data) {
		length1 = len(data)
	}
	if length1+length2 > len(data) {
		length2 = len(data) - length1
	}
	data2 := data[:length1]
	var data3 []byte
	if length2 > 0 {
		data3 = data[length1 : length1+length2]
	}
	if length1 > 0 && length2 > 0 {
		err := file.loadFromSegments(data2, data3)
		if err != nil {
			return nil, err
		}
	}
	return file, nil
}

// Read reads an image and loads into a new Image object with an RGB
// colormap and 8 bits per component.
//
// Read buffers the whole stream so it can decode the pixels and also retain the
// original encoded bytes and any embedded ICC profile on the returned Image
// (EncodedData/EncodedFormat/ICCProfile). This lets the profile be preserved
// and JPEG sources be embedded without a lossy re-encode. The retained data is
// cleared by pixel-mutating operations.
func (h DefaultImageHandler) Read(reader io.Reader) (*Image, error) {
	all, err := io.ReadAll(reader)
	if err != nil {
		common.Log.Debug("Error reading image data: %s", err)
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(all))
	if err != nil {
		common.Log.Debug("Error decoding file: %s", err)
		return nil, err
	}
	img2, err := h.NewImageFromGoImage(img)
	if err != nil {
		return nil, err
	}
	extract, format := iccextract.Extract(all)
	if format == iccextract.FormatJPEG {
		img2.encodedData = all
		img2.encodedFormat = string(format)
	}
	img2.iccProfile = extract
	return img2, nil
}

func newPdfShadingType2FromDict(dict *core.PdfObjectDictionary) (*PdfShadingType2, error) {
	shading2 := PdfShadingType2{}
	coords := dict.Get("Coords")
	if coords == nil {
		common.Log.Debug("Required attribute missing:  Coords")
		return nil, ErrRequiredAttributeMissing
	}
	arr, ok := coords.(*core.PdfObjectArray)
	if !ok {
		common.Log.Debug("Coords not an array (got %T)", coords)
		return nil, errors.New("type check error")
	}
	if arr.Len() != 4 {
		common.Log.Debug("Coords length not 4 (got %d)", arr.Len())
		return nil, errors.New("invalid attribute")
	}
	shading2.Coords = arr
	if domain := dict.Get("Domain"); domain != nil {
		domain = core.TraceToDirectObject(domain)
		arr, ok := domain.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Domain not an array (got %T)", domain)
			return nil, errors.New("type check error")
		}
		shading2.Domain = arr
	}
	coords = dict.Get("Function")
	if coords == nil {
		common.Log.Debug("Required attribute missing:  Function")
		return nil, ErrRequiredAttributeMissing
	}
	shading2.Function = []PdfFunction{}
	if arr, ok := coords.(*core.PdfObjectArray); ok {
		for _, obj := range arr.Elements() {
			function, err := newPdfFunctionFromPdfObject(obj)
			if err != nil {
				common.Log.Debug("Error parsing function: %v", err)
				return nil, err
			}
			shading2.Function = append(shading2.Function, function)
		}
	} else {
		function, err := newPdfFunctionFromPdfObject(coords)
		if err != nil {
			common.Log.Debug("Error parsing function: %v", err)
			return nil, err
		}
		shading2.Function = append(shading2.Function, function)
	}
	if extend := dict.Get("Extend"); extend != nil {
		extend = core.TraceToDirectObject(extend)
		arr, ok := extend.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Matrix not an array (got %T)", extend)
			return nil, core.ErrTypeError
		}
		if arr.Len() != 2 {
			common.Log.Debug("Extend length not 2 (got %d)", arr.Len())
			return nil, ErrInvalidAttribute
		}
		shading2.Extend = arr
	}
	return &shading2, nil
}

// Encoder returns the font's text encoder.
func (f *PdfFont) Encoder() textencoding.TextEncoder {
	actualFont := f.actualFont()
	if actualFont == nil {
		common.Log.Debug("ERROR: Encoder not implemented for font type=%#T", f.context)
		return nil
	}
	return actualFont.Encoder()
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionSound) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeSound)))
	dict.SetIfNotNil("Sound", a.Sound)
	dict.SetIfNotNil("Volume", a.Volume)
	dict.SetIfNotNil("Synchronous", a.Synchronous)
	dict.SetIfNotNil("Repeat", a.Repeat)
	dict.SetIfNotNil("Mix", a.Mix)
	return indirect
}

// Components returns the color's component values: R, G, B.
func (c *PdfColorDeviceRGB) Components() []float64 {
	return []float64{c[0], c[1], c[2]}
}

// NewPdfAnnotationStamp returns a new stamp annotation.
func NewPdfAnnotationStamp() *PdfAnnotationStamp {
	annot := NewPdfAnnotation()
	stamp := &PdfAnnotationStamp{}
	stamp.PdfAnnotation = annot
	stamp.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(stamp)
	return stamp
}

// ToPdfObject converts the font to a PDF representation.
func (f *pdfFontType0) ToPdfObject() core.PdfObject {
	if f.primitive == nil {
		f.primitive = &core.PdfIndirectObject{}
	}
	dict := f.baseFields().asPdfObjectDictionary("Type0")
	f.primitive.PdfObject = dict
	if f.Encoding != nil {
		dict.Set("Encoding", f.Encoding)
	} else if f.encoder != nil {
		dict.Set("Encoding", f.encoder.ToPdfObject())
	}
	if f.DescendantFont != nil {
		dict.Set("DescendantFonts", core.MakeArray(f.DescendantFont.ToPdfObject()))
	}
	return f.primitive
}

// Direction represents the predominant reading order for text.
type Direction string

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

// GetOutlines returns a high-level Outline object, based on the outline tree
// of the reader.
func (r *PdfReader) GetOutlines() (*Outline, error) {
	if r == nil {
		return nil, errors.New("cannot create outline from nil reader")
	}
	outlineTree := r.GetOutlineTree()
	if outlineTree == nil {
		return nil, errors.New("the specified reader does not have an outline tree")
	}
	var fn func(node *PdfOutlineTreeNode, items *[]*OutlineItem)
	fn = func(node *PdfOutlineTreeNode, items *[]*OutlineItem) {
		if node == nil {
			return
		}
		if node.context == nil {
			common.Log.Debug("ERROR: missing outline entry context")
			return
		}
		var item *OutlineItem
		if item2, ok := node.context.(*PdfOutlineItem); ok {
			dest := item2.Dest
			if (dest == nil || core.IsNullObject(dest)) && item2.A != nil {
				if dict, ok := core.GetDict(item2.A); ok {
					if d, ok := core.GetArray(dict.Get("D")); ok {
						dest = d
					} else {
						d, ok := core.GetString(dict.Get("D"))
						if !ok {
							return
						}
						ref, ok := r.catalog.Get("Names").(*core.PdfObjectReference)
						if !ok {
							return
						}
						lookupByReference, err := r.parser.LookupByReference(*ref)
						if err != nil {
							common.Log.Debug("ERROR: Failed to read names reference (%s)", err.Error())
							return
						}
						indirect, ok := lookupByReference.(*core.PdfIndirectObject)
						if !ok {
							return
						}
						set := map[core.PdfObject]struct{}{}
						err = r.buildNameNodes(indirect, set)
						if err != nil {
							common.Log.Debug("ERROR: Failed to build name nodes (%s)", err.Error())
							return
						}
						for obj := range set {
							dict, ok := core.GetDict(obj)
							if !ok {
								continue
							}
							names, ok := core.GetArray(dict.Get("Names"))
							if !ok {
								continue
							}
							for i, obj := range names.Elements() {
								switch obj.(type) {
								case *core.PdfObjectString:
									if obj.String() == d.String() {
										if obj := names.Get(i + 1); obj != nil {
											if dict, ok := core.GetDict(obj); ok {
												dest = dict.Get("D")
												break
											}
										}
									}
								}
							}
						}
					}
				}
			}
			var dest2 OutlineDest
			if dest != nil && !core.IsNullObject(dest) {
				if outlineDest, err := newOutlineDestFromPdfObject(dest, r); err == nil {
					dest2 = *outlineDest
				} else {
					common.Log.Debug("WARN: could not parse outline dest (%v): %v", dest, err)
				}
			}
			item = NewOutlineItem(item2.Title.Decoded(), dest2)
			*items = append(*items, item)
			if item2.Next != nil {
				fn(item2.Next, items)
			}
		}
		if node.First != nil {
			if item != nil {
				items = &item.Entries
			}
			fn(node.First, items)
		}
	}
	outline := NewOutline()
	fn(outlineTree, &outline.Entries)
	return outline, nil
}

// PdfPage represents a page in a PDF document. (7.7.3.3 - Table 30).
type PdfPage struct {
	Parent               core.PdfObject
	LastModified         *PdfDate
	Resources            *PdfPageResources
	CropBox              *PdfRectangle
	MediaBox             *PdfRectangle
	BleedBox             *PdfRectangle
	TrimBox              *PdfRectangle
	ArtBox               *PdfRectangle
	BoxColorInfo         core.PdfObject
	Contents             core.PdfObject
	Rotate               *int64
	Group                core.PdfObject
	Thumb                core.PdfObject
	B                    core.PdfObject
	Dur                  core.PdfObject
	Trans                core.PdfObject
	AA                   core.PdfObject
	Metadata             core.PdfObject
	PieceInfo            core.PdfObject
	StructParents        core.PdfObject
	ID                   core.PdfObject
	PZ                   core.PdfObject
	SeparationInfo       core.PdfObject
	Tabs                 core.PdfObject
	TemplateInstantiated core.PdfObject
	PresSteps            core.PdfObject
	UserUnit             core.PdfObject
	VP                   core.PdfObject
	Annots               core.PdfObject
	annotations          []*PdfAnnotation
	mu                   *sync.Mutex
	pageDict             *core.PdfObjectDictionary
	primitive            *core.PdfIndirectObject
	originalDict         *core.PdfObjectDictionary
	reader               *PdfReader
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationStrikeOut) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("StrikeOut"))
	dict.SetIfNotNil("QuadPoints", a.QuadPoints)
	return indirect
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionLaunch) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeLaunch)))
	if a.F != nil {
		dict.Set("F", a.F.ToPdfObject())
	}
	dict.SetIfNotNil("Win", a.Win)
	dict.SetIfNotNil("Mac", a.Mac)
	dict.SetIfNotNil("Unix", a.Unix)
	dict.SetIfNotNil("NewWindow", a.NewWindow)
	return indirect
}

// ColorFromFloats returns a new PdfColor based on the input slice of color
// components.
func (cs *PdfColorspaceSpecialPattern) ColorFromFloats(vals []float64) (PdfColor, error) {
	if cs.UnderlyingCS == nil {
		return nil, errors.New("underlying CS not specified")
	}
	return cs.UnderlyingCS.ColorFromFloats(vals)
}

func (a *PdfAppender) mergeResources(obj, obj2 core.PdfObject, nameByName map[core.PdfObjectName]core.PdfObjectName) core.PdfObject {
	if obj2 == nil && obj == nil {
		return nil
	}
	if obj2 == nil {
		return obj
	}
	dict, ok := core.GetDict(obj2)
	if !ok {
		return obj
	}
	if obj == nil {
		dict2 := core.MakeDict()
		dict2.Merge(dict)
		return obj2
	}
	dict2, ok := core.GetDict(obj)
	if !ok {
		common.Log.Error("Error resource is not a dictionary")
		dict2 = core.MakeDict()
	}
	for _, name := range dict.Keys() {
		if name2, ok := nameByName[name]; ok {
			dict2.Set(name2, dict.Get(name))
		} else {
			dict2.Set(name, dict.Get(name))
		}
	}
	return dict2
}

// NewStandard14FontWithEncoding returns the standard 14 font named `basefont` as a *PdfFont and
// a TextEncoder that encodes all the runes in `alphabet`, or an error if this is not possible.
// An error can occur if `basefont` is not one the standard 14 font names.
func NewStandard14FontWithEncoding(basefont StdFontName, alphabet map[rune]int) (*PdfFont, textencoding.SimpleEncoder, error) {
	fontSimple, err := newPdfFontSimpleFromStdName(basefont)
	if err != nil {
		return nil, nil, err
	}
	enc, ok := fontSimple.Encoder().(textencoding.SimpleEncoder)
	if !ok {
		return nil, nil, fmt.Errorf("only simple encoding is supported, got %T", fontSimple.Encoder())
	}
	glyphByNum := make(map[rune]textencoding.GlyphName)
	for n := range alphabet {
		if _, ok := enc.RuneToCharcode(n); !ok {
			_, ok := fontSimple.fontMetrics.Read(n)
			if !ok {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("rune %#x=%q not in the font", n, n)
				}
				continue
			}
			runeToGlyph, ok := textencoding.RuneToGlyph(n)
			if !ok {
				common.Log.Debug("no glyph for rune %#x=%q", n, n)
				continue
			}
			if len(glyphByNum) >= 255 {
				return nil, nil, errors.New("too many characters for simple encoding")
			}
			glyphByNum[n] = runeToGlyph
		}
	}
	var (
		codes  []textencoding.CharCode
		codes2 []textencoding.CharCode
	)
	for i := textencoding.CharCode(1); i <= 0xff; i++ {
		charcodeToRune, ok := enc.CharcodeToRune(i)
		if !ok {
			codes = append(codes, i)
			continue
		}
		if _, ok = alphabet[charcodeToRune]; !ok {
			codes2 = append(codes2, i)
		}
	}
	codes3 := append(codes, codes2...)
	if len(codes3) < len(glyphByNum) {
		return nil, nil, fmt.Errorf("need to encode %d runes, but have only %d slots", len(glyphByNum), len(codes3))
	}
	runes := make([]rune, 0, len(glyphByNum))
	for n := range glyphByNum {
		runes = append(runes, n)
	}
	sort.Slice(runes, func(n, count int) bool { return runes[n] < runes[count] })
	glyphByCode := make(map[textencoding.CharCode]textencoding.GlyphName, len(runes))
	for _, n := range runes {
		code := codes3[0]
		codes3 = codes3[1:]
		glyphByCode[code] = glyphByNum[n]
	}
	enc = textencoding.ApplyDifferences(enc, glyphByCode)
	fontSimple.SetEncoder(enc)
	return &PdfFont{context: &fontSimple}, enc, nil
}

func (w *PdfWriter) seekByName(obj core.PdfObject, strs []string, s string) ([]core.PdfObject, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Seek by name.. %T", obj)
	}
	var objs []core.PdfObject
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		return w.seekByName(indirect.PdfObject, strs, s)
	}
	if stream, ok := obj.(*core.PdfObjectStream); ok {
		return w.seekByName(stream.PdfObjectDictionary, strs, s)
	}
	if dict, ok := obj.(*core.PdfObjectDictionary); ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Dict")
		}
		for _, name := range dict.Keys() {
			obj := dict.Get(name)
			if string(name) == s {
				objs = append(objs, obj)
			}
			for _, str := range strs {
				if string(name) == str {
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("Follow key %s", str)
					}
					seekByName, err := w.seekByName(obj, strs, s)
					if err != nil {
						return objs, err
					}
					objs = append(objs, seekByName...)
					break
				}
			}
		}
		return objs, nil
	}
	return objs, nil
}

// ToPdfObject return the CalGray colorspace as a PDF object (name dictionary).
func (cs *PdfColorspaceCalGray) ToPdfObject() core.PdfObject {
	arr := &core.PdfObjectArray{}
	arr.Append(core.MakeName("CalGray"))
	dict := core.MakeDict()
	if cs.WhitePoint != nil {
		dict.Set("WhitePoint", core.MakeArray(core.MakeFloat(cs.WhitePoint[0]), core.MakeFloat(cs.WhitePoint[1]), core.MakeFloat(cs.WhitePoint[2])))
	} else {
		common.Log.Error("CalGray: Missing WhitePoint (Required)")
	}
	if cs.BlackPoint != nil {
		dict.Set("BlackPoint", core.MakeArray(core.MakeFloat(cs.BlackPoint[0]), core.MakeFloat(cs.BlackPoint[1]), core.MakeFloat(cs.BlackPoint[2])))
	}
	dict.Set("Gamma", core.MakeFloat(cs.Gamma))
	arr.Append(dict)
	if cs.container != nil {
		cs.container.PdfObject = arr
		return cs.container
	}
	return arr
}

// SetOtfType sets the OTF type information.
func (f *pdfCIDFontType2) SetOtfType(otfType *fonts.OtfType) { f.otfType = otfType }

// NewPdfReaderWithOpts creates a new PdfReader for the given io.ReaderAt
// source with the supplied options. If opts is nil, the defaults from
// NewReaderOpts are used. The source's byte length is auto-detected; for
// exotic io.ReaderAt sources use NewPdfReaderWithOptsAt.
func NewPdfReaderWithOpts(r io.ReaderAt, opts *ReaderOpts) (*PdfReader, error) {
	sizeOfReaderAt, err := core.SizeOfReaderAt(r)
	if err != nil {
		return nil, err
	}
	return NewPdfReaderWithOptsAt(r, sizeOfReaderAt, opts)
}

// ArtifactAttachPosition specifies the position where an artifact is attached to a page.
type ArtifactAttachPosition string

// NewPdfActionMovie returns a new "movie" action.
func NewPdfActionMovie() *PdfActionMovie {
	action := NewPdfAction()
	movieAction := &PdfActionMovie{}
	movieAction.PdfAction = action
	action.SetContext(movieAction)
	return movieAction
}

// NewPdfAnnotationFileAttachment returns a new file attachment annotation.
func NewPdfAnnotationFileAttachment() *PdfAnnotationFileAttachment {
	annot := NewPdfAnnotation()
	fileAttachment := &PdfAnnotationFileAttachment{}
	fileAttachment.PdfAnnotation = annot
	fileAttachment.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(fileAttachment)
	return fileAttachment
}

// ToPdfObject returns a PDF object representation of the outline item.
func (o *OutlineItem) ToPdfObject() core.PdfObject {
	item, _ := o.ToPdfOutlineItem()
	return item.ToPdfObject()
}

func (r *PdfReader) newPdfAnnotationProjectionFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationProjection, error) {
	projection := &PdfAnnotationProjection{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	projection.PdfAnnotationMarkup = markup
	return projection, nil
}

// PrintPdfObjects is a helper function printing all objects in a given PDF file
// It can be useful while debugging. Use os.Stdout as w io.Writer to print to console.
func (r *PdfReader) PrintPdfObjects(w io.Writer) error {
	objectNums := r.GetObjectNums()
	fmt.Fprintf(w, "%d PDF objects:\n", len(objectNums))
	for _, objectNum := range objectNums {
		fmt.Fprintln(w, "=========================================================")
		if err := r.PrintPdfObject(w, objectNum); err != nil {
			return err
		}
	}
	return nil
}

func (w *PdfWriter) writeString(s string) {
	if w.writeErr != nil {
		return
	}
	writeString, err := w.bufWriter.WriteString(s)
	w.writePos += int64(writeString)
	w.writeErr = err
}

func thresholdImage(img *Image, b uint8) (*Image, error) {
	img2, err := img.ToGoImage()
	if err != nil {
		return nil, err
	}
	imgToGray := imageutil.ImgToGray(img2)
	bounds := imgToGray.Bounds()
	n, count := bounds.Dx(), bounds.Dy()
	num := (n + 7) / 8
	data := make([]byte, num*count)
	for i := 0; i < count; i++ {
		size := i * num
		for j := 0; j < n; j++ {
			if imgToGray.GrayAt(bounds.Min.X+j, bounds.Min.Y+i).Y >= b {
				data[size+j/8] |= 0x80 >> uint(j%8)
			}
		}
	}
	return &Image{Width: int64(n), Height: int64(count), BitsPerComponent: 1, ColorComponents: 1, Data: data}, nil
}

// String returns a string representation of the field.
func (f *PdfField) String() string {
	if indirect, ok := f.ToPdfObject().(*core.PdfIndirectObject); ok {
		return fmt.Sprintf("%T: %s", f.context, indirect.PdfObject.String())
	}
	return ""
}

func buildHintStreamObject(n int64, data []byte, count int64) ([]byte, error) {
	return core.BuildHintStreamObject(n, data, count)
}

func (w *PdfWriter) writeNamesDictionary() error {
	if w.names == nil {
		return nil
	}
	obj := w.names.ToPdfObject()
	w.catalog.Set("Names", obj)
	err := w.addObjects(obj)
	if err != nil {
		return err
	}
	return nil
}

// PdfDate represents a date, which is a PDF string of the form:
// (D:YYYYMMDDHHmmSSOHH'mm)
type PdfDate struct {
	year            int64
	month           int64
	day             int64
	hour            int64
	minute          int64
	second          int64
	utOffsetSign    byte
	utOffsetHours   int64
	utOffsetMinutes int64
}

func (w *PdfWriter) copyObjects() {
	w.copied = true
	objByObj := make(map[core.PdfObject]core.PdfObject)
	objs := make([]core.PdfObject, 0, len(w.objects))
	set := make(map[core.PdfObject]struct{}, len(w.objects))
	set2 := make(map[core.PdfObject]struct{})
	for _, object := range w.objects {
		copyObject := w.copyObject(object, objByObj, set2, false)
		if _, ok := set2[object]; ok {
			continue
		}
		objs = append(objs, copyObject)
		set[copyObject] = struct{}{}
	}
	w.objects = objs
	w.hasObjects = set
	w.infoObj = w.copyObject(w.infoObj, objByObj, nil, false).(*core.PdfIndirectObject)
	w.root = w.copyObject(w.root, objByObj, nil, false).(*core.PdfIndirectObject)
	if w.encryptObj != nil {
		w.encryptObj = w.copyObject(w.encryptObj, objByObj, nil, false).(*core.PdfIndirectObject)
	}
	if w.appendMode {
		numByObj := make(map[core.PdfObject]int64)
		for obj, objNumMap := range w.objNumMap {
			if obj2, ok := objByObj[obj]; ok {
				numByObj[obj2] = objNumMap
			} else {
				common.Log.Debug("ERROR: append mode - object copy not in map")
			}
		}
		w.objNumMap = numByObj
	}
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationInk) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Ink"))
	dict.SetIfNotNil("InkList", a.InkList)
	dict.SetIfNotNil("BS", a.BS)
	return indirect
}

// ToPdfObject returns the PDF representation of the shading dictionary.
func (s *PdfShadingType6) ToPdfObject() core.PdfObject {
	s.PdfShading.ToPdfObject()
	shadingDict, err := s.getShadingDict()
	if err != nil {
		common.Log.Error("Unable to access shading dict")
		return nil
	}
	if s.BitsPerCoordinate != nil {
		shadingDict.Set("BitsPerCoordinate", s.BitsPerCoordinate)
	}
	if s.BitsPerComponent != nil {
		shadingDict.Set("BitsPerComponent", s.BitsPerComponent)
	}
	if s.BitsPerFlag != nil {
		shadingDict.Set("BitsPerFlag", s.BitsPerFlag)
	}
	if s.Decode != nil {
		shadingDict.Set("Decode", s.Decode)
	}
	if s.Function != nil {
		if len(s.Function) == 1 {
			shadingDict.Set("Function", s.Function[0].ToPdfObject())
		} else {
			array := core.MakeArray()
			for _, function := range s.Function {
				array.Append(function.ToPdfObject())
			}
			shadingDict.Set("Function", array)
		}
	}
	return s.primitive
}

func (r *PdfReader) newPdfAnnotationPolygonFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationPolygon, error) {
	polygon := PdfAnnotationPolygon{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	polygon.PdfAnnotationMarkup = markup
	polygon.Vertices = dict.Get("Vertices")
	polygon.LE = dict.Get("LE")
	polygon.BS = dict.Get("BS")
	polygon.IC = dict.Get("IC")
	polygon.BE = dict.Get("BE")
	polygon.IT = dict.Get("IT")
	polygon.Measure = dict.Get("Measure")
	return &polygon, nil
}

// GetContainingPdfObject implements interface PdfModel.
func (p *Permissions) GetContainingPdfObject() core.PdfObject { return p.primitive }

// GetNumComponents returns the number of color components (1 for Indexed).
func (cs *PdfColorspaceSpecialIndexed) GetNumComponents() int { return 1 }

// SetColorSpace sets `r` colorspace object to `colorspace`.
func (r *PdfPageResources) SetColorSpace(colorspace *PdfPageResourcesColorspaces) {
	r.colorspaces = colorspace
}

// GetPage returns the PdfPage model for the specified page number.
func (r *PdfReader) GetPage(pageNumber int) (*PdfPage, error) {
	if r.parser.GetCrypter() != nil && !r.parser.IsAuthenticated() {
		return nil, fmt.Errorf("file needs to be decrypted first")
	}
	if len(r.pageObjs) < pageNumber {
		return nil, errors.New("invalid page number (page count too short)")
	}
	n := pageNumber - 1
	if n < 0 {
		return nil, fmt.Errorf("page numbering must start at 1")
	}
	page := r.PageList[n]
	return page, nil
}

// NewPdfField returns an initialized PdfField.
func NewPdfField() *PdfField { return &PdfField{primitive: core.MakeIndirectObject(core.MakeDict())} }

var infoDictKeys = map[string]struct{}{"Title": {}, "Author": {}, "Subject": {}, "Keywords": {}, "Creator": {}, "Producer": {}, "Trapped": {}, "CreationDate": {}, "ModDate": {}}

func (w *PdfWriter) writeBytes(data []byte) {
	if w.writeErr != nil {
		return
	}
	n, err := w.bufWriter.Write(data)
	w.writePos += int64(n)
	w.writeErr = err
}

func (cs *PdfColorspaceICCBased) imageToRGBManaged(img Image) (Image, error) {
	floats := img.decode
	if len(floats) != 6 {
		floats = cs.Range
	}
	if len(floats) != 6 {
		floats = []float64{0, 1, 0, 1, 0, 1}
	}
	imageBase := imageutil.NewImageBase(int(img.Width), int(img.Height), int(img.BitsPerComponent), 3, nil, img.alpha, nil)
	f := math.Pow(2, float64(img.BitsPerComponent)) - 1
	var floats2, floats3, floats4 []float64
	if img.BitsPerComponent <= maxBitsPerComponent {
		n := int(f) + 1
		floats2 = make([]float64, n)
		floats3 = make([]float64, n)
		floats4 = make([]float64, n)
		for i := 0; i < n; i++ {
			v := float64(i) / f
			floats2[i] = cs.iccTransform.LinearizeChannel(0, imageutil.LinearInterpolate(v, 0.0, 1.0, floats[0], floats[1]))
			floats3[i] = cs.iccTransform.LinearizeChannel(1, imageutil.LinearInterpolate(v, 0.0, 1.0, floats[2], floats[3]))
			floats4[i] = cs.iccTransform.LinearizeChannel(2, imageutil.LinearInterpolate(v, 0.0, 1.0, floats[4], floats[5]))
		}
	}
	base := img.getBase()
	if floats2 != nil && img.BitsPerComponent == 8 && base.BytesPerLine == int(img.Width)*3 && imageBase.BytesPerLine == int(img.Width)*3 && len(base.Data) >= imageBase.BytesPerLine*int(img.Height) {
		data := imageBase.Data
		data2 := base.Data
		n := int(img.Width) * int(img.Height)
		for i := 0; i < n; i++ {
			n := i * 3
			encodeLinear, v, val := cs.iccTransform.EncodeLinear(floats2[data2[n]], floats3[data2[n+1]], floats4[data2[n+2]])
			data[n] = byte(uint32(encodeLinear * f))
			data[n+1] = byte(uint32(v * f))
			data[n+2] = byte(uint32(val * f))
		}
		return imageFromImageBase(&imageBase), nil
	}
	reader := sampling.NewReader(base)
	writer := sampling.NewWriter(imageBase)
	nums := make([]uint32, 3)
	var err error
	for {
		err = reader.ReadSamples(nums)
		if err == io.EOF {
			break
		} else if err != nil {
			return img, err
		}
		var v, val, x float64
		if floats2 != nil {
			v, val, x = cs.iccTransform.EncodeLinear(floats2[nums[0]], floats3[nums[1]], floats4[nums[2]])
		} else {
			linearInterpolate := imageutil.LinearInterpolate(float64(nums[0])/f, 0.0, 1.0, floats[0], floats[1])
			y := imageutil.LinearInterpolate(float64(nums[1])/f, 0.0, 1.0, floats[2], floats[3])
			y2 := imageutil.LinearInterpolate(float64(nums[2])/f, 0.0, 1.0, floats[4], floats[5])
			v, val, x = cs.iccTransform.Convert(linearInterpolate, y, y2)
		}
		nums[0] = uint32(v * f)
		nums[1] = uint32(val * f)
		nums[2] = uint32(x * f)
		if err = writer.WriteSamples(nums); err != nil {
			return img, err
		}
	}
	return imageFromImageBase(&imageBase), nil
}

func newPdfColorspaceSpecialIndexedFromPdfObject(obj core.PdfObject) (*PdfColorspaceSpecialIndexed, error) {
	specialIndexedCS := NewPdfColorspaceSpecialIndexed()
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		specialIndexedCS.container = indirect
	}
	obj = core.TraceToDirectObject(obj)
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("type error")
	}
	if arr.Len() != 4 {
		return nil, fmt.Errorf("indexed CS: invalid array length")
	}
	obj = arr.Get(0)
	name, ok := obj.(*core.PdfObjectName)
	if !ok {
		return nil, fmt.Errorf("indexed CS: invalid name")
	}
	if *name != "Indexed" {
		return nil, fmt.Errorf("indexed CS: wrong name")
	}
	obj = arr.Get(1)
	determineColorspaceName, err := DetermineColorspaceNameFromPdfObject(obj)
	if err != nil {
		return nil, err
	}
	if determineColorspaceName == "Indexed" || determineColorspaceName == "Pattern" {
		common.Log.Debug("Error: Indexed colorspace cannot have Indexed/Pattern CS as base (%v)", determineColorspaceName)
		return nil, errRangeCheck
	}
	cs, err := NewPdfColorspaceFromPdfObject(obj)
	if err != nil {
		return nil, err
	}
	specialIndexedCS.Base = cs
	obj = arr.Get(2)
	n, err := core.GetNumberAsInt64(obj)
	if err != nil {
		return nil, err
	}
	if n > 255 {
		return nil, fmt.Errorf("indexed CS: Invalid hival")
	}
	specialIndexedCS.HiVal = int(n)
	obj = arr.Get(3)
	specialIndexedCS.Lookup = obj
	obj = core.TraceToDirectObject(obj)
	var data []byte
	if str, ok := obj.(*core.PdfObjectString); ok {
		data = str.Bytes()
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Indexed string color data: % d", data)
		}
	} else if stream, ok := obj.(*core.PdfObjectStream); ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Indexed stream: %s", obj.String())
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Encoded (%d) : %# x", len(stream.Stream), stream.Stream)
		}
		data2, err := core.DecodeStream(stream)
		if err != nil {
			return nil, err
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Decoded (%d) : % X", len(data2), data2)
		}
		data = data2
	} else {
		common.Log.Debug("Type: %T", obj)
		return nil, fmt.Errorf("indexed CS: Invalid table format")
	}
	if len(data) < specialIndexedCS.Base.GetNumComponents()*(specialIndexedCS.HiVal+1) {
		common.Log.Debug("PDF Incompatibility: Index stream too short")
		common.Log.Debug("Fail, len(data): %d, components: %d, hiVal: %d", len(data), specialIndexedCS.Base.GetNumComponents(), specialIndexedCS.HiVal)
	} else {
		data = data[:specialIndexedCS.Base.GetNumComponents()*(specialIndexedCS.HiVal+1)]
	}
	specialIndexedCS.lookupTable = data
	return specialIndexedCS, nil
}

// A returns the value of the A component of the color.
func (c *PdfColorLab) A() float64 { return c[1] }

func ocspResponderAuthorized(resp *ocsp.Response, cert *x509.Certificate) bool {
	certificate := resp.Certificate
	if certificate == nil || certificate.Equal(cert) {
		return true
	}
	if resp.ThisUpdate.After(resp.ProducedAt) {
		return false
	}
	return resp.NextUpdate.IsZero() || !resp.NextUpdate.After(certificate.NotAfter)
}

// PrintScaling returns the value of the printScaling.
func (vp *ViewerPreferences) PrintScaling() PrintScaling { return vp.printScaling }

// ColorFromPdfObjects gets the color from a series of pdf objects (3 for rgb).
func (cs *PdfColorspaceDeviceRGB) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) != 3 {
		return nil, errors.New("range check")
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objects)
	if err != nil {
		return nil, err
	}
	return cs.ColorFromFloats(numbersAsFloat)
}

var (
	StructureTypeParagraph       StructureType = "P"
	StructureTypeHeader          StructureType = "H"
	StructureTypeHeading1        StructureType = "H1"
	StructureTypeHeading2        StructureType = "H2"
	StructureTypeHeading3        StructureType = "H3"
	StructureTypeHeading4        StructureType = "H4"
	StructureTypeHeading5        StructureType = "H5"
	StructureTypeHeading6        StructureType = "H6"
	StructureTypeList            StructureType = "L"
	StructureTypeListItem        StructureType = "LI"
	StructureTypeLabel           StructureType = "Lbl"
	StructureTypeListBody        StructureType = "LBody"
	StructureTypeTable           StructureType = "Table"
	StructureTypeTableRow        StructureType = "TR"
	StructureTypeTableHeaderCell StructureType = "TH"
	StructureTypeTableData       StructureType = "TD"
	StructureTypeTableHead       StructureType = "THead"
	StructureTypeTableBody       StructureType = "TBody"
	StructureTypeTableFooter     StructureType = "TFoot"
)

// ColorToRGB verifies that the input color is an RGB color. Method exists in
// order to satisfy the PdfColorspace interface.
func (cs *PdfColorspaceDeviceRGB) ColorToRGB(color PdfColor) (PdfColor, error) {
	deviceRGBColor, ok := color.(*PdfColorDeviceRGB)
	if !ok {
		common.Log.Debug("Input color not device RGB")
		return nil, errors.New("type check error")
	}
	return deviceRGBColor, nil
}

// FirstXrefTrailer returns the trailer dictionary of the first xref table
// (the one immediately after the linearization parameter dictionary) for a
// linearized PDF. Returns nil if the PDF is not linearized, or an error if
// the first xref cannot be located.
func (r *PdfReader) FirstXrefTrailer() (*core.PdfObjectDictionary, error) {
	return r.parser.FirstXrefTrailer()
}

// PdfColorspaceSpecialSeparation is a Separation colorspace.
// At the moment the colour space is set to a Separation space, the conforming reader shall determine whether the
// device has an available colorant (e.g. dye) corresponding to the name of the requested space. If so, the conforming
// reader shall ignore the alternateSpace and tintTransform parameters; subsequent painting operations within the
// space shall apply the designated colorant directly, according to the tint values supplied.
//
// Format: [/Separation name alternateSpace tintTransform]
type PdfColorspaceSpecialSeparation struct {
	ColorantName   *core.PdfObjectName
	AlternateSpace PdfColorspace
	TintTransform  PdfFunction
	container      *core.PdfIndirectObject
}

var (
	oidDeltaCRLIndicator        = asn1.ObjectIdentifier{2, 5, 29, 27}
	oidIssuingDistributionPoint = asn1.ObjectIdentifier{2, 5, 29, 28}
	oidCertificateIssuer        = asn1.ObjectIdentifier{2, 5, 29, 29}
)

func (w *PdfWriter) hasOrphanPages() bool {
	for _, object := range w.objects {
		indirect, ok := object.(*core.PdfIndirectObject)
		if !ok {
			continue
		}
		dict, ok := core.GetDict(indirect.PdfObject)
		if !ok {
			continue
		}
		if s, _ := core.GetNameVal(dict.Get("Type")); s != "Page" {
			continue
		}
		if _, ok := w.pageDicts[dict]; !ok {
			return true
		}
	}
	return false
}

func (c fontCommon) fontFlags() int {
	if c.fontDescriptor == nil {
		return 0
	}
	return c.fontDescriptor.flags
}

// NewBorderStyle returns an initialized PdfBorderStyle.
func NewBorderStyle() *PdfBorderStyle { borderStyle := &PdfBorderStyle{}; return borderStyle }

// NewPdfAnnotationLink returns a new link annotation.
func NewPdfAnnotationLink() *PdfAnnotationLink {
	annot := NewPdfAnnotation()
	link := &PdfAnnotationLink{}
	link.PdfAnnotation = annot
	annot.SetContext(link)
	return link
}

// GetPreviousRevision returns the previous revision of PdfReader for the Pdf document
func (r *PdfReader) GetPreviousRevision() (*PdfReader, error) {
	if r.parser.GetRevisionNumber() == 0 {
		return nil, errors.New("previous version is not exist")
	}
	if revisionCache, ok := r.revisionCache[r]; ok {
		return revisionCache, nil
	}
	previousRevisionParser, err := r.parser.GetPreviousRevisionParser()
	if err != nil {
		return nil, err
	}
	readerCore, err := newPdfReaderCore(previousRevisionParser.GetReaderAt(), r.opts, r.autoDecrypt, r.skipOutlines && r.skipPerms, "model:GetPreviousRevision")
	if err != nil {
		return nil, err
	}
	if readerCore, err = readerCore.loadFromParser(previousRevisionParser); err != nil {
		return nil, err
	}
	r.revisions[r.parser.GetRevisionNumber()-1] = readerCore
	r.revisionCache[r] = readerCore
	readerCore.revisionCache = r.revisionCache
	return readerCore, nil
}

func (w *PdfWriter) makeOffSetReference(n int64) {
	s := fmt.Sprintf("startxref\n%d\n", n)
	w.writeString(s)
	w.writeString("%%EOF\n")
}

// ToInteger convert to an integer format.
func (c *PdfColorLab) ToInteger(bits int) [3]uint32 {
	f := math.Pow(2, float64(bits)) - 1
	return [3]uint32{uint32(f * c.L()), uint32(f * c.A()), uint32(f * c.B())}
}

// GetStructRoot gets the StructTreeRoot object
func (p *PdfPage) GetStructTreeRoot() (*core.PdfObject, bool) {
	catalogStructTreeRoot, ok := p.reader.GetCatalogStructTreeRoot()
	return &catalogStructTreeRoot, ok
}

// GetEmojiImageData extracts image data from an emoji rune if the font supports it.
// Returns the raw image data, MIME type, and file extension.
// This is a convenience method that combines OTF font detection and color glyph extraction.
func (f *PdfFont) GetEmojiImageData(r rune) (data []byte, s string, s2 string, err error) {
	otfType := f.GetOTFType()
	if otfType == nil {
		return nil, "", "", fmt.Errorf("font does not support OTF color glyphs")
	}
	colorGlyphInfoByRune := GetColorGlyphInfoByRune(otfType, r)
	if colorGlyphInfoByRune == nil {
		return nil, "", "", fmt.Errorf("no color glyph information found for rune %c (U+%04X)", r, r)
	}
	data, s, s2 = colorGlyphInfoByRune.GetImageData()
	if data == nil {
		return nil, "", "", fmt.Errorf("no image data available for rune %c (U+%04X)", r, r)
	}
	return data, s, s2, nil
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
func (cs *PdfColorspaceDeviceRGB) String() string { return "DeviceRGB" }

// PageFromIndirectObject returns the PdfPage and page number for a given indirect object.
func (r *PdfReader) PageFromIndirectObject(ind *core.PdfIndirectObject) (*PdfPage, int, error) {
	if len(r.PageList) != len(r.pageObjs) {
		return nil, 0, errors.New("page list invalid")
	}
	for i, pageObj := range r.pageObjs {
		if pageObj == ind {
			return r.PageList[i], i + 1, nil
		}
	}
	return nil, 0, errors.New("page not found")
}

func scaleGlyphWidths(chars map[rune]fonts.GID, widths []uint16, unitsPerEm uint16) (map[rune]int, map[fonts.GID]int) {
	f := 1000.0 / float64(unitsPerEm)
	if unitsPerEm == 0 {
		f = 1.0
	}
	table := make(map[rune]int)
	numByV := make(map[fonts.GID]int)
	v := fonts.GID(len(widths))
	for n, char := range chars {
		if v == 0 || char >= v {
			continue
		}
		count := int(f * float64(widths[char]))
		table[n] = count
		numByV[char] = count
	}
	return table, numByV
}

// GetAsTilingPattern returns a tiling pattern. Check with IsTiling() prior to using this.
func (p *PdfPattern) GetAsTilingPattern() *PdfTilingPattern {
	return p.context.(*PdfTilingPattern)
}

// NewPdfColorLab returns a new Lab color.
func NewPdfColorLab(l, a, b float64) *PdfColorLab { labColor := PdfColorLab{l, a, b}; return &labColor }

// NewPermissions returns a new permissions object.
func NewPermissions(docMdp *PdfSignature) *Permissions {
	perms := Permissions{}
	perms.DocMDP = docMdp
	dict := core.MakeDict()
	dict.Set("DocMDP", docMdp.ToPdfObject())
	perms.primitive = dict
	return &perms
}

func (t *trustEvaluator) checkCertRevocation(cert, cert2 *x509.Certificate, t2 time.Time, ok bool, evidence revocationEvidence) certRevocation {
	sum256, data := sha256.Sum256(cert.Raw), sha256.Sum256(cert2.Raw)
	var answers []ocspAnswer
	numByResp := map[*ocsp.Response]int{}
	for _, ocspResp := range evidence.ocsps {
		resp := t.parseOCSP(ocspKey{responseHash: ocspResp.digest, certHash: sum256, issuerHash: data}, ocspResp.raw, cert, cert2)
		if resp == nil {
			continue
		}
		if n, ok := numByResp[resp]; ok {
			answers[n].online = answers[n].online || ocspResp.online
			continue
		}
		if !ocspResponderAuthorizedByIssuer(resp, cert, cert2) {
			continue
		}
		numByResp[resp] = len(answers)
		answers = append(answers, ocspAnswer{Response: resp, online: ocspResp.online, authorised: ocspResponderAuthorized(resp, cert2)})
	}
	var crls []crlEvidence
	numByCrl := map[*parsedCRL]int{}
	for _, crl := range evidence.crls {
		if n, ok := numByCrl[crl.parsedCRL]; ok {
			crls[n].online = crls[n].online || crl.online
			continue
		}
		if !equalDistinguishedNames(crl.RawIssuer, cert.RawIssuer) || !crl.signedBy(cert2, data) {
			continue
		}
		numByCrl[crl.parsedCRL] = len(crls)
		crls = append(crls, crl)
	}
	for _, answer := range answers {
		if answer.Status == ocsp.Revoked && answer.RevocationReason != ocsp.CertificateHold && !answer.RevokedAt.After(t2) {
			return certRevocation{status: RevocationStatusRevoked, revokedAt: answer.RevokedAt, ocspFound: true}
		}
	}
	if revocationEntry := findRevocationEntry(crls, cert, t2); revocationEntry != nil {
		return certRevocation{status: RevocationStatusRevoked, revokedAt: revocationEntry.RevocationTime, crlFound: true}
	}
	if report, isReport := findHoldReport(answers, crls, cert, t2); isReport {
		t3 := t2
		if ok {
			t3 = time.Time{}
		}
		switch {
		case report.lifted(answers, crls, cert, t3):
		case !ok && report.thisUpdate.Before(t2) && report.lifted(answers, crls, cert, time.Time{}):
			return certRevocation{status: RevocationStatusUnknown, ocspFound: report.crl == nil, crlFound: report.crl != nil}
		default:
			return certRevocation{status: RevocationStatusRevoked, revokedAt: report.revokedAt, onHold: true, ocspFound: report.crl == nil, crlFound: report.crl != nil}
		}
	}
	revocation := certRevocation{status: RevocationStatusUnknown}
	for _, answer := range answers {
		if answer.Status == ocsp.Unknown || answer.Status == ocsp.Revoked && !answer.RevokedAt.After(t2) {
			continue
		}
		if answer.authorised && crlIsCurrent(answer.ThisUpdate, answer.NextUpdate, cert, t2, answer.online) {
			revocation.status, revocation.ocspFound = RevocationStatusGood, true
		}
	}
	if anyCRLCovers(crls, cert, t2) {
		revocation.status, revocation.crlFound = RevocationStatusGood, true
	}
	return revocation
}

func getCreationDate() time.Time {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	return pdfCreationDate
}

// NewPdfShadingType1 creates an empty shading type 1 dictionary.
func NewPdfShadingType1() *PdfShadingType1 {
	shading1 := &PdfShadingType1{}
	shading1.PdfShading = &PdfShading{}
	shading1.PdfShading.primitive = core.MakeIndirectObject(core.MakeDict())
	shading1.PdfShading.context = shading1
	return shading1
}

// GetPdfVersion gets the version of the PDF used within this document.
func (w *PdfWriter) GetPdfVersion() string { return w.getPdfVersion() }

// PdfAnnotationStrikeOut represents StrikeOut annotations.
// (Section 12.5.6.10).
type PdfAnnotationStrikeOut struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	QuadPoints core.PdfObject
}

// AddExtGState add External Graphics State (GState). The gsDict can be specified
// either directly as a dictionary or an indirect object containing a dictionary.
func (r *PdfPageResources) AddExtGState(gsName core.PdfObjectName, gsDict core.PdfObject) error {
	if r.ExtGState == nil {
		r.ExtGState = core.MakeDict()
	}
	extGState := r.ExtGState
	dict, ok := core.TraceToDirectObject(extGState).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ExtGState type error (got %T/%T)", extGState, core.TraceToDirectObject(extGState))
		return core.ErrTypeError
	}
	dict.Set(gsName, gsDict)
	return nil
}

// ImageToRGB converts Lab colorspace image to RGB and returns the result.
func (cs *PdfColorspaceLab) ImageToRGB(img Image) (Image, error) {
	fn := func(f float64) float64 {
		if f >= 6.0/29 {
			return f * f * f
		}
		return 108.0 / 841 * (f - 4.0/29.0)
	}
	floats := img.decode
	if len(floats) != 6 {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Image - Lab Decode range != 6... use [0 100 amin amax bmin bmax] default decode array")
		}
		floats = cs.DecodeArray()
	}
	reader := sampling.NewReader(img.getBase())
	imageBase := imageutil.NewImageBase(int(img.Width), int(img.Height), int(img.BitsPerComponent), 3, nil, img.alpha, img.decode)
	writer := sampling.NewWriter(imageBase)
	f := math.Pow(2, float64(img.BitsPerComponent)) - 1
	nums := make([]uint32, 3)
	var (
		err                                     error
		Ls, As, Bs, L, M, N, X, Y, Z, v, val, x float64
	)
	for {
		err = reader.ReadSamples(nums)
		if err == io.EOF {
			break
		} else if err != nil {
			return img, err
		}
		Ls = float64(nums[0]) / f
		As = float64(nums[1]) / f
		Bs = float64(nums[2]) / f
		Ls = imageutil.LinearInterpolate(Ls, 0.0, 1.0, floats[0], floats[1])
		As = imageutil.LinearInterpolate(As, 0.0, 1.0, floats[2], floats[3])
		Bs = imageutil.LinearInterpolate(Bs, 0.0, 1.0, floats[4], floats[5])
		L = (Ls+16)/116 + As/500
		M = (Ls + 16) / 116
		N = (Ls+16)/116 - Bs/200
		X = cs.WhitePoint[0] * fn(L)
		Y = cs.WhitePoint[1] * fn(M)
		Z = cs.WhitePoint[2] * fn(N)
		v = 3.240479*X + -1.537150*Y + -0.498535*Z
		val = -0.969256*X + 1.875992*Y + 0.041556*Z
		x = 0.055648*X + -0.204043*Y + 1.057311*Z
		v = math.Min(math.Max(v, 0), 1.0)
		val = math.Min(math.Max(val, 0), 1.0)
		x = math.Min(math.Max(x, 0), 1.0)
		nums[0] = uint32(v * f)
		nums[1] = uint32(val * f)
		nums[2] = uint32(x * f)
		if err = writer.WriteSamples(nums); err != nil {
			return img, err
		}
	}
	return imageFromImageBase(&imageBase), nil
}

// EncodedFormat returns the container format of the bytes returned by
// EncodedData ("jpeg"), or "" when none are retained.
func (img *Image) EncodedFormat() string { return img.encodedFormat }

// GetXObjectByName gets XObject by name.
func (p *PdfPage) GetXObjectByName(name core.PdfObjectName) (core.PdfObject, bool) {
	dict, ok := p.Resources.XObject.(*core.PdfObjectDictionary)
	if !ok {
		return nil, false
	}
	if obj := dict.Get(name); obj != nil {
		return obj, true
	}
	return nil, false
}

func (n *Names) addEmbeddedFile(file *EmbeddedFile) error {
	if n.EmbeddedFiles == nil {
		n.EmbeddedFiles = core.MakeDict()
		n.EmbeddedFiles.Set("Names", core.MakeArray())
	}
	filespec := NewPdfFileSpecFromEmbeddedFile(file)
	names := n.EmbeddedFiles.Get("Names")
	arr, ok := names.(*core.PdfObjectArray)
	if !ok {
		return errors.New("Invalid Names array")
	}
	type FileSpecMap struct {
		name     string
		filespec *PdfFilespec
	}
	fileSpecMaps := []FileSpecMap{}
	for i := 0; i < len(arr.Elements()); i += 2 {
		if i%2 == 0 {
			obj := arr.Get(i)
			if obj != nil {
				str := obj.(*core.PdfObjectString)
				obj := arr.Get(i + 1)
				filespec, err := NewPdfFilespecFromObj(obj)
				if err != nil {
					return err
				}
				fileSpecMaps = append(fileSpecMaps, FileSpecMap{name: str.String(), filespec: filespec})
			}
		}
	}
	fileSpecMaps = append(fileSpecMaps, FileSpecMap{name: file.Name, filespec: filespec})
	sort.Slice(fileSpecMaps, func(n, count int) bool { return fileSpecMaps[n].name < fileSpecMaps[count].name })
	arr = core.MakeArray()
	for _, fileSpecMap := range fileSpecMaps {
		arr.Append(core.MakeString(fileSpecMap.name))
		arr.Append(fileSpecMap.filespec.ToPdfObject())
	}
	n.EmbeddedFiles.Set("Names", arr)
	return nil
}

// RemoveStructParentsKey removes the StructParents key.
func (p *PdfPage) RemoveStructParentsKey() { p.StructParents = nil }

func sortInt64s(nums []int64) {
	for i := 1; i < len(nums); i++ {
		num := nums[i]
		n := i - 1
		for n >= 0 && nums[n] > num {
			nums[n+1] = nums[n]
			n--
		}
		nums[n+1] = num
	}
}

// GetCustomInfo returns a custom info value for the specified name.
func (i *PdfInfo) GetCustomInfo(name string) *core.PdfObjectString {
	var str *core.PdfObjectString
	if i.dict == nil {
		return str
	}
	if str2, ok := i.dict.Get(*core.MakeName(name)).(*core.PdfObjectString); ok {
		str = str2
	}
	return str
}

func (f *pdfFontType0) subsetCFF(font *pdfCIDFontType0) error {
	if font == nil {
		return nil
	}
	otfType := font.GetOtfType()
	if otfType == nil || otfType.Loader == nil {
		common.Log.Debug("No OTF data for CFF subsetting")
		return nil
	}
	if font.fontDescriptor == nil {
		common.Log.Debug("Missing font descriptor for CFF subsetting")
		return nil
	}
	if f.encoder == nil {
		common.Log.Debug("No encoder - CFF subsetting ignored")
		return nil
	}
	data, err := cffTable(otfType.Loader)
	if err != nil {
		common.Log.Debug("Unable to get CFF table: %v", err)
		return nil
	}
	var runes []rune
	var vals []uint16
	switch v := f.encoder.(type) {
	case textencoding.GIDRegistry:
		for _, registeredGID := range v.RegisteredGIDs() {
			vals = append(vals, uint16(registeredGID))
		}
		if len(vals) == 0 {
			common.Log.Debug("No registered GIDs - skip CFF subsetting")
			return nil
		}
	case textencoding.RuneRegistry:
		runes = v.RegisteredRunes()
		vals = runesToGIDs(runes, otfType.Chars)
		v.SubsetRegistered()
	default:
		common.Log.Debug("Unsupported encoder for CFF subsetting: %T", f.encoder)
		return nil
	}
	vals2 := shapedGlyphIDs(f.encoder)
	vals = append(vals, vals2...)
	vals = sortedUniqueUint16(vals)
	subsetCFF, err := fonts.SubsetCFF(data, vals)
	if err != nil {
		common.Log.Debug("CFF subset error: %v", err)
		return err
	}
	stream, err := core.MakeStream(subsetCFF, core.NewFlateEncoder())
	if err != nil {
		common.Log.Debug("ERROR: %v", err)
		return err
	}
	stream.Set("Subtype", core.MakeName("CIDFontType0C"))
	if fontFile3Stream, ok := core.GetStream(font.fontDescriptor.FontFile3); ok {
		*fontFile3Stream = *stream
	} else {
		font.fontDescriptor.FontFile3 = stream
	}
	if runes != nil {
		f.updateSubsetToUnicode(runes, vals2)
	} else {
		gids := make([]unitype.GlyphIndex, len(vals))
		for i, val := range vals {
			gids[i] = unitype.GlyphIndex(val)
		}
		f.filterSubsetToUnicode(gids)
	}
	registerGlyphWidthsFromGIDs(font.W, vals)
	registerOtfGlyphWidths(font.W, vals2, otfType)
	applySubsetTag(f, &font.baseFont, font.fontDescriptor, subsetCFF)
	return nil
}

const (
	ActionTypeGoTo        PdfActionType = "GoTo"
	ActionTypeGoTo3DView  PdfActionType = "GoTo3DView"
	ActionTypeGoToE       PdfActionType = "GoToE"
	ActionTypeGoToR       PdfActionType = "GoToR"
	ActionTypeHide        PdfActionType = "Hide"
	ActionTypeImportData  PdfActionType = "ImportData"
	ActionTypeJavaScript  PdfActionType = "JavaScript"
	ActionTypeLaunch      PdfActionType = "Launch"
	ActionTypeMovie       PdfActionType = "Movie"
	ActionTypeNamed       PdfActionType = "Named"
	ActionTypeRendition   PdfActionType = "Rendition"
	ActionTypeResetForm   PdfActionType = "ResetForm"
	ActionTypeSetOCGState PdfActionType = "SetOCGState"
	ActionTypeSound       PdfActionType = "Sound"
	ActionTypeSubmitForm  PdfActionType = "SubmitForm"
	ActionTypeThread      PdfActionType = "Thread"
	ActionTypeTrans       PdfActionType = "Trans"
	ActionTypeURI         PdfActionType = "URI"
)

// PdfAnnotationLine represents Line annotations.
// (Section 12.5.6.7).
type PdfAnnotationLine struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	L       core.PdfObject
	BS      core.PdfObject
	LE      core.PdfObject
	IC      core.PdfObject
	LL      core.PdfObject
	LLE     core.PdfObject
	Cap     core.PdfObject
	IT      core.PdfObject
	LLO     core.PdfObject
	CP      core.PdfObject
	Measure core.PdfObject
	CO      core.PdfObject
}
type objectStreamsState struct {
	members         []*core.PdfIndirectObject
	packedObjNums   []int64
	streams         *core.PdfObjectStreams
	streamObjNum    int64
	mainXrefObjNum  int64
	firstXrefObjNum int64
}

// SetTabOrder sets the tab order for the page.
func (p *PdfPage) SetTabOrder(order TabOrderType) { p.Tabs = core.MakeName(string(order)) }

// GetNumComponents returns the number of color components (1 for grayscale).
func (c *PdfColorDeviceGray) GetNumComponents() int { return 1 }

// GetNumComponents returns the number of color components (1 for Separation).
func (cs *PdfColorspaceSpecialSeparation) GetNumComponents() int { return 1 }

// PdfAnnotationProjection represents Projection annotations.
type PdfAnnotationProjection struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
}

// GetContext returns the context of the outline tree node, which is either a
// *PdfOutline or a *PdfOutlineItem. The method returns nil for uninitialized
// tree nodes.
func (n *PdfOutlineTreeNode) GetContext() PdfModel {
	if outline, ok := n.context.(*PdfOutline); ok {
		return outline
	}
	if item, ok := n.context.(*PdfOutlineItem); ok {
		return item
	}
	common.Log.Debug("ERROR Invalid outline tree node item")
	return nil
}

// PdfActionSetOCGState represents a SetOCGState action.
type PdfActionSetOCGState struct {
	*PdfAction
	State      core.PdfObject
	PreserveRB core.PdfObject
}

// Transform rectangle with the supplied matrix.
func (r *PdfRectangle) Transform(transformMatrix transform.Matrix) {
	r.Llx, r.Lly = transformMatrix.Transform(r.Llx, r.Lly)
	r.Urx, r.Ury = transformMatrix.Transform(r.Urx, r.Ury)
	r.Normalize()
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionSubmitForm) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeSubmitForm)))
	if a.F != nil {
		dict.Set("F", a.F.ToPdfObject())
	}
	dict.SetIfNotNil("Fields", a.Fields)
	dict.SetIfNotNil("Flags", a.Flags)
	return indirect
}

func runesToGIDs(runes []rune, chars map[rune]fonts.GID) []uint16 {
	vals := make([]uint16, 0, len(runes))
	for _, n := range runes {
		if char, ok := chars[n]; ok {
			vals = append(vals, uint16(char))
		}
	}
	return vals
}

func (c *parsedCRL) newerThan(crl *parsedCRL) bool {
	if c.Number != nil && crl.Number != nil && bytes.Equal(c.idpRaw, crl.idpRaw) {
		return c.Number.Cmp(crl.Number) > 0
	}
	return c.ThisUpdate.After(crl.ThisUpdate)
}

func (r *PdfReader) newPdfActionNamedFromDict(dict *core.PdfObjectDictionary) (*PdfActionNamed, error) {
	return &PdfActionNamed{N: dict.Get("N")}, nil
}

// NewPdfFileSpecFromEmbeddedFile construct a new PdfFileSpec that contains an embedded file.
func NewPdfFileSpecFromEmbeddedFile(file *EmbeddedFile) *PdfFilespec {
	filespec := &PdfFilespec{}
	filespec.primitive = core.MakeIndirectObject(core.MakeDict())
	filespec.Desc = core.MakeString(file.Description)
	filespec.EF = file.ToPdfObject()
	filespec.F = core.MakeString(file.Name)
	filespec.UF = core.MakeEncodedString(file.Name, true)
	s := "Unspecified"
	switch file.Relationship {
	case RelationshipSource:
		s = "Source"
	case RelationshipData:
		s = "Data"
	case RelationshipAlternative:
		s = "Alternative"
	case RelationshipSupplement:
		s = "Supplement"
	}
	filespec.AFRelationship = core.MakeName(s)
	return filespec
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionGoTo) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeGoTo)))
	dict.SetIfNotNil("D", a.D)
	return indirect
}

func (l *LTV) enable(certs, chain []*x509.Certificate, s string) error {
	certChain, certByKey, err := l.buildCertChain(certs, chain)
	if err != nil {
		return err
	}
	dataList, err := l.getCerts(certChain)
	if err != nil {
		return err
	}
	var list, list2 [][]byte
	if l.OCSPClient != nil {
		list, err = l.getOCSPs(certChain, certByKey)
		if err != nil {
			return err
		}
	}
	if l.CRLClient != nil {
		list2, err = l.getCRLs(certChain)
		if err != nil {
			return err
		}
	}
	dss := l.dss
	addCerts, err := dss.AddCerts(dataList)
	if err != nil {
		return err
	}
	addOCSPs, err := dss.AddOCSPs(list)
	if err != nil {
		return err
	}
	addCRLs, err := dss.AddCRLs(list2)
	if err != nil {
		return err
	}
	if s != "" {
		dss.VRI[s] = &VRI{Cert: addCerts, OCSP: addOCSPs, CRL: addCRLs}
	}
	l.appender.SetDSS(dss)
	return nil
}

const (
	BorderStyleSolid     BorderStyle = iota
	BorderStyleDashed    BorderStyle = iota
	BorderStyleBeveled   BorderStyle = iota
	BorderStyleInset     BorderStyle = iota
	BorderStyleUnderline BorderStyle = iota
)

// PdfAnnotationHighlight represents Highlight annotations.
// (Section 12.5.6.10).
type PdfAnnotationHighlight struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	QuadPoints core.PdfObject
}

func (r *PdfReader) newPdfSignatureReferenceFromDict(dict *core.PdfObjectDictionary) (*PdfSignatureReference, error) {
	if signatureReference, ok := r.modelManager.GetModelFromPrimitive(dict).(*PdfSignatureReference); ok {
		return signatureReference, nil
	}
	signatureReference := &PdfSignatureReference{primitive: dict, Data: dict.Get("Data")}
	var ok bool
	signatureReference.Type, _ = core.GetName(dict.Get("Type"))
	signatureReference.TransformMethod, ok = core.GetName(dict.Get("TransformMethod"))
	if !ok {
		common.Log.Error("ERROR: Signature Reference TransformMethod attribute invalid or missing")
		return nil, ErrInvalidAttribute
	}
	signatureReference.TransformParams, _ = core.GetDict(dict.Get("TransformParams"))
	signatureReference.DigestMethod, _ = core.GetName(dict.Get("DigestMethod"))
	return signatureReference, nil
}

// FontDescriptor returns font's PdfFontDescriptor. This may be a builtin descriptor for standard 14
// fonts but must be an explicit descriptor for other fonts.
func (f *PdfFont) FontDescriptor() *PdfFontDescriptor {
	if f.baseFields().fontDescriptor != nil {
		return f.baseFields().fontDescriptor
	}
	if fontDescriptor := f.context.getFontDescriptor(); fontDescriptor != nil {
		return fontDescriptor
	}
	common.Log.Error("All fonts have a Descriptor. font=%s", f)
	return nil
}

func equalDistinguishedNames(data, rawIssuer []byte) bool {
	if bytes.Equal(data, rawIssuer) {
		return true
	}
	var rdnSequence, sequence pkix.RDNSequence
	if data2, err := asn1.Unmarshal(data, &rdnSequence); err != nil || len(data2) > 0 {
		return false
	}
	if data, err := asn1.Unmarshal(rawIssuer, &sequence); err != nil || len(data) > 0 || len(rdnSequence) != len(sequence) {
		return false
	}
	for i := range rdnSequence {
		if len(rdnSequence[i]) != len(sequence[i]) {
			return false
		}
		for j := range rdnSequence[i] {
			attributeTypeAndValue, value := rdnSequence[i][j], sequence[i][j]
			if !attributeTypeAndValue.Type.Equal(value.Type) {
				return false
			}
			s, ok := attributeTypeAndValue.Value.(string)
			s2, found := value.Value.(string)
			if !ok || !found {
				if !reflect.DeepEqual(attributeTypeAndValue.Value, value.Value) {
					return false
				}
				continue
			}
			if !strings.EqualFold(strings.Join(strings.Fields(s), " "), strings.Join(strings.Fields(s2), " ")) {
				return false
			}
		}
	}
	return true
}

func newPdfShadingType4FromDict(dict *core.PdfObjectDictionary) (*PdfShadingType4, error) {
	shading4 := PdfShadingType4{}
	bitsPerCoordinate := dict.Get("BitsPerCoordinate")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerCoordinate")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok := bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerCoordinate not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading4.BitsPerCoordinate = num
	bitsPerCoordinate = dict.Get("BitsPerComponent")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerComponent")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok = bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerComponent not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading4.BitsPerComponent = num
	bitsPerCoordinate = dict.Get("BitsPerFlag")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: BitsPerFlag")
		return nil, ErrRequiredAttributeMissing
	}
	num, ok = bitsPerCoordinate.(*core.PdfObjectInteger)
	if !ok {
		common.Log.Debug("BitsPerFlag not an integer (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading4.BitsPerFlag = num
	bitsPerCoordinate = dict.Get("Decode")
	if bitsPerCoordinate == nil {
		common.Log.Debug("Required attribute missing: Decode")
		return nil, ErrRequiredAttributeMissing
	}
	arr, ok := bitsPerCoordinate.(*core.PdfObjectArray)
	if !ok {
		common.Log.Debug("Decode not an array (got %T)", bitsPerCoordinate)
		return nil, core.ErrTypeError
	}
	shading4.Decode = arr
	if function := dict.Get("Function"); function != nil {
		shading4.Function = []PdfFunction{}
		if arr, ok := function.(*core.PdfObjectArray); ok {
			for _, obj := range arr.Elements() {
				function, err := newPdfFunctionFromPdfObject(obj)
				if err != nil {
					common.Log.Debug("Error parsing function: %v", err)
					return nil, err
				}
				shading4.Function = append(shading4.Function, function)
			}
		} else {
			fn, err := newPdfFunctionFromPdfObject(function)
			if err != nil {
				common.Log.Debug("Error parsing function: %v", err)
				return nil, err
			}
			shading4.Function = append(shading4.Function, fn)
		}
	}
	return &shading4, nil
}

// SetOpenAction sets the OpenAction in the PDF catalog.
// The value shall be either an array defining a destination (12.3.2 "Destinations" PDF32000_2008),
// or an action dictionary representing an action (12.6 "Actions" PDF32000_2008).
func (w *PdfWriter) SetOpenAction(dest core.PdfObject) error {
	if dest == nil || core.IsNullObject(dest) {
		return nil
	}
	w.catalog.Set("OpenAction", dest)
	return w.addObjects(dest)
}

// ToPdfObject returns the PDF representation of the shading dictionary.
func (s *PdfShading) ToPdfObject() core.PdfObject {
	obj := s.primitive
	shadingDict, err := s.getShadingDict()
	if err != nil {
		common.Log.Error("Unable to access shading dict")
		return nil
	}
	if s.ShadingType != nil {
		shadingDict.Set("ShadingType", s.ShadingType)
	}
	if s.ColorSpace != nil {
		shadingDict.Set("ColorSpace", s.ColorSpace.ToPdfObject())
	}
	if s.Background != nil {
		shadingDict.Set("Background", s.Background)
	}
	if s.BBox != nil {
		shadingDict.Set("BBox", s.BBox.ToPdfObject())
	}
	if s.AntiAlias != nil {
		shadingDict.Set("AntiAlias", s.AntiAlias)
	}
	return obj
}

// SignerCertificate returns the signer's (end-entity) certificate: the first
// entry of Certificates, or nil when the chain is empty.
func (r SignatureValidationResult) SignerCertificate() *x509.Certificate {
	if len(r.Certificates) == 0 {
		return nil
	}
	return r.Certificates[0]
}

// SetContext sets the specific fielddata type, e.g. would be PdfFieldButton for a button field.
func (f *PdfField) SetContext(ctx PdfModel)    { f.context = ctx }
func (cs *PdfColorspaceCalRGB) String() string { return "CalRGB" }
func (f *PdfField) resolveDA() string {
	var s string
	_, _ = f.inherit(func(field *PdfField) bool {
		if field.VariableText != nil && field.VariableText.DA != nil {
			s = field.VariableText.DA.Decoded()
			return true
		}
		return false
	})
	return s
}

// DefaultFont returns the default font, which is currently the built in Helvetica.
func DefaultFont() *PdfFont {
	stdFontByName, ok := fonts.NewStdFontByName(HelveticaName)
	if !ok {
		panic("Helvetica should always be available")
	}
	fontSimple := newPdfFontSimpleFromStdFont(stdFontByName)
	return &PdfFont{context: &fontSimple}
}

func (w *PdfWriter) writeAcroFormFields() error {
	if w.acroForm == nil {
		return nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Writing acro forms")
	}
	obj := w.acroForm.ToPdfObject()
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("AcroForm: %+v", obj)
	}
	w.catalog.Set("AcroForm", obj)
	err := w.addObjects(obj)
	if err != nil {
		return err
	}
	return nil
}

// GetXHeight returns the XHeight of the font `descriptor`.
func (d *PdfFontDescriptor) GetXHeight() (float64, error) {
	return core.GetNumberAsFloat(d.XHeight)
}

// NewPdfAnnotationMovie returns a new movie annotation.
func NewPdfAnnotationMovie() *PdfAnnotationMovie {
	annot := NewPdfAnnotation()
	movie := &PdfAnnotationMovie{}
	movie.PdfAnnotation = annot
	annot.SetContext(movie)
	return movie
}

func (r *PdfReader) newPdfActionRenditionFromDict(dict *core.PdfObjectDictionary) (*PdfActionRendition, error) {
	return &PdfActionRendition{R: dict.Get("R"), AN: dict.Get("AN"), OP: dict.Get("OP"), JS: dict.Get("JS")}, nil
}

// NewPdfPageResourcesFromDict creates and returns a new PdfPageResources object
// from the input dictionary.
func NewPdfPageResourcesFromDict(dict *core.PdfObjectDictionary) (*PdfPageResources, error) {
	resources := NewPdfPageResources()
	if extGState := dict.Get("ExtGState"); extGState != nil {
		resources.ExtGState = extGState
	}
	if colorSpace := dict.Get("ColorSpace"); colorSpace != nil && !core.IsNullObject(colorSpace) {
		resources.ColorSpace = colorSpace
	}
	if pattern := dict.Get("Pattern"); pattern != nil {
		resources.Pattern = pattern
	}
	if shading := dict.Get("Shading"); shading != nil {
		resources.Shading = shading
	}
	if xObject := dict.Get("XObject"); xObject != nil {
		resources.XObject = xObject
	}
	if font := core.ResolveReference(dict.Get("Font")); font != nil {
		resources.Font = font
	}
	if procSet := dict.Get("ProcSet"); procSet != nil {
		resources.ProcSet = procSet
	}
	if properties := dict.Get("Properties"); properties != nil {
		resources.Properties = properties
	}
	return resources, nil
}

// SetFitWindow sets the value of the fitWindow flag.
func (vp *ViewerPreferences) SetFitWindow(fitWindow bool) { vp.fitWindow = &fitWindow }

// AcroFormNeedsRepair returns true if the document contains widget annotations
// linked to fields which are not referenced in the AcroForm. The AcroForm can
// be repaired using the RepairAcroForm method of the reader.
func (r *PdfReader) AcroFormNeedsRepair() (bool, error) {
	var fields []*PdfField
	if r.AcroForm != nil {
		fields = r.AcroForm.AllFields()
	}
	set := make(map[*PdfField]struct{}, len(fields))
	for _, field := range fields {
		set[field] = struct{}{}
	}
	for _, page := range r.PageList {
		annotations, err := page.GetAnnotations()
		if err != nil {
			return false, err
		}
		for _, annotation := range annotations {
			widget, ok := annotation.GetContext().(*PdfAnnotationWidget)
			if !ok {
				continue
			}
			field := widget.Field()
			if field == nil {
				return true, nil
			}
			if _, ok := set[field]; !ok {
				return true, nil
			}
		}
	}
	return false, nil
}

func formBBox(form *XObjectForm) (*PdfRectangle, bool, error) {
	if arr, ok := form.BBox.(*core.PdfObjectArray); ok {
		rect, err := NewPdfRectangle(*arr)
		if err != nil {
			return nil, false, err
		}
		if arr, ok := form.Matrix.(*core.PdfObjectArray); ok {
			floats, err := arr.ToFloat64Array()
			if err != nil {
				return nil, false, err
			}
			identityMatrix := transform.IdentityMatrix()
			if len(floats) == 6 {
				identityMatrix = transform.NewMatrix(floats[0], floats[1], floats[2], floats[3], floats[4], floats[5])
			}
			rect.Transform(identityMatrix)
			return rect, true, nil
		}
		return rect, false, nil
	}
	return nil, false, errors.New("invalid appearance BBox type")
}

// GetImageSize returns the image dimensions if available.
// For SVG glyphs, this may return 0,0 as SVG is scalable.
func (g *ColorGlyphInfo) GetImageSize() (n, count int) {
	switch g.GlyphType {
	case ColorGlyphTypeSVG:
		return 0, 0
	case ColorGlyphTypeBitmap:
		return g.BitmapWidth, g.BitmapHeight
	}
	return 0, 0
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

// SetPrintClip sets the value of the printClip.
func (vp *ViewerPreferences) SetPrintClip(printClip PageBoundary) { vp.printClip = printClip }

// GetPerms returns the Permissions dictionary
func (r *PdfReader) GetPerms() *Permissions { return r.perms }

// ToPdfObject implements interface PdfModel.
func (f *PdfFilespec) ToPdfObject() core.PdfObject {
	dict := f.getDict()
	dict.Clear()
	dict.Set("Type", core.MakeName("Filespec"))
	dict.SetIfNotNil("FS", f.FS)
	dict.SetIfNotNil("F", f.F)
	dict.SetIfNotNil("UF", f.UF)
	dict.SetIfNotNil("DOS", f.DOS)
	dict.SetIfNotNil("Mac", f.Mac)
	dict.SetIfNotNil("Unix", f.Unix)
	dict.SetIfNotNil("ID", f.ID)
	dict.SetIfNotNil("V", f.V)
	dict.SetIfNotNil("EF", f.EF)
	dict.SetIfNotNil("RF", f.RF)
	dict.SetIfNotNil("Desc", f.Desc)
	dict.SetIfNotNil("CI", f.CI)
	dict.SetIfNotNil("AFRelationship", f.AFRelationship)
	return f.primitive
}

// Colorspace returns the DeviceCMYK colorspace.
func (c *PdfColorDeviceCMYK) Colorspace() PdfColorspace { return NewPdfColorspaceDeviceCMYK() }

// NewPdfAnnotationUnderline returns a new text underline annotation.
func NewPdfAnnotationUnderline() *PdfAnnotationUnderline {
	annot := NewPdfAnnotation()
	underline := &PdfAnnotationUnderline{}
	underline.PdfAnnotation = annot
	underline.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(underline)
	return underline
}

func (w *PdfWriter) mapObjectStreams(ok bool) (map[core.PdfObject]bool, bool) {
	set := make(map[core.PdfObject]bool)
	objs := make([]core.PdfObject, 0, len(w.objects))
	for _, object := range w.objects {
		objStreams, isObjStreams := object.(*core.PdfObjectStreams)
		if !isObjStreams {
			objs = append(objs, object)
			continue
		}
		n := 0
		for _, obj := range objStreams.Elements() {
			if w.excludedFromObjectStream(obj) {
				continue
			}
			set[obj] = true
			if indirect, ok := obj.(*core.PdfIndirectObject); ok {
				set[indirect.PdfObject] = true
			}
			n++
		}
		if n == 0 {
			delete(w.hasObjects, object)
			continue
		}
		ok = true
		objs = append(objs, object)
	}
	w.objects = objs
	return set, ok
}

// NewPdfReaderFromFile creates a new PdfReader from the specified PDF file.
// If ReaderOpts is nil it will be set to default value from NewReaderOpts.
func NewPdfReaderFromFile(pdfFile string, opts *ReaderOpts) (*PdfReader, *os.File, error) {
	const trackNameNewPdfReaderFromFile = "model:NewPdfReaderFromFile"
	open, err := os.Open(pdfFile)
	if err != nil {
		return nil, nil, err
	}
	stat, err := open.Stat()
	if err != nil {
		open.Close()
		return nil, nil, err
	}
	readerTracked, err := newPdfReaderTracked(open, stat.Size(), opts, true, false, trackNameNewPdfReaderFromFile)
	if err != nil {
		open.Close()
		return nil, nil, err
	}
	readerTracked.fileName = pdfFile
	return readerTracked, open, nil
}

func (r *PdfReader) newPdfActionURIFromDict(dict *core.PdfObjectDictionary) (*PdfActionURI, error) {
	return &PdfActionURI{URI: dict.Get("URI"), IsMap: dict.Get("IsMap")}, nil
}

// String returns a string describing the font descriptor.
func (d *PdfFontDescriptor) String() string {
	var strs []string
	if d.FontName != nil {
		strs = append(strs, d.FontName.String())
	}
	if d.FontFamily != nil {
		strs = append(strs, d.FontFamily.String())
	}
	if d.fontFile != nil {
		strs = append(strs, d.fontFile.String())
	}
	if d.ttf != nil {
		strs = append(strs, d.ttf.String())
	}
	strs = append(strs, fmt.Sprintf("FontFile3=%t", d.FontFile3 != nil))
	return fmt.Sprintf("FONT_DESCRIPTOR{%s}", strings.Join(strs, ", "))
}

func (f *pdfFontType0) getFontDescriptor() *PdfFontDescriptor {
	if f.fontDescriptor == nil && f.DescendantFont != nil {
		return f.DescendantFont.FontDescriptor()
	}
	return f.fontDescriptor
}

func (a *PdfAcroForm) signatureFields() []*PdfFieldSignature {
	var signatureFields []*PdfFieldSignature
	for _, allField := range a.AllFields() {
		switch v := allField.GetContext().(type) {
		case *PdfFieldSignature:
			signatureField := v
			signatureFields = append(signatureFields, signatureField)
		}
	}
	return signatureFields
}

// SetAnnotations sets the annotations list.
func (p *PdfPage) SetAnnotations(annotations []*PdfAnnotation) {
	if p.mu != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
	}
	p.annotations = annotations
}

// PdfAnnotationRichMedia represents Rich Media annotations.
type PdfAnnotationRichMedia struct {
	*PdfAnnotation
	RichMediaSettings core.PdfObject
	RichMediaContent  core.PdfObject
}

// NewStructureTagInfo creates a new StructureTagInfo with default values.
func NewStructureTagInfo() *StructureTagInfo {
	return &StructureTagInfo{Mcid: 0, StructureType: StructureTypeUnknown}
}

// PdfAnnotationLink represents Link annotations.
// (Section 12.5.6.5 p. 403).
type PdfAnnotationLink struct {
	*PdfAnnotation
	A          core.PdfObject
	Dest       core.PdfObject
	H          core.PdfObject
	PA         core.PdfObject
	QuadPoints core.PdfObject
	BS         core.PdfObject
	action     *PdfAction
	reader     *PdfReader
}

// WidgetProperties describes appearance characteristics for a single
// widget annotation of a form field.
type WidgetProperties struct {
	Rect            *PdfRectangle
	BorderColor     PdfColor
	BackgroundColor PdfColor
	Caption         string
	Rotation        int
	AppearanceState string
}

// AddWatermarkImage adds an image watermark to the page.
func (p *PdfPage) AddWatermarkImage(ximg *XObjectImage, opt WatermarkImageOptions) error {
	mediaBox, err := p.GetMediaBox()
	if err != nil {
		return err
	}
	f := mediaBox.Urx - mediaBox.Llx
	v := mediaBox.Ury - mediaBox.Lly
	val := opt.Angle * math.Pi / 180
	width := float64(*ximg.Width)
	x := (f - width*math.Cos(val)) / 2
	if opt.FitToWidth {
		width = f
		x = 0
	}
	y := v
	y2 := float64(0)
	if opt.PreserveAspectRatio {
		y = width * float64(*ximg.Height) / float64(*ximg.Width)
		y2 = (v - y) / 2
		y2 -= float64(*ximg.Width) / 2 * math.Sin(val)
	}
	if p.Resources == nil {
		p.Resources = NewPdfPageResources()
	}
	xObjectForm := NewXObjectForm()
	xObjectForm.BBox = core.MakeArrayFromFloats([]float64{0, 0, width, y})
	xObjectForm.Resources = NewPdfPageResources()
	n := 0
	name := core.PdfObjectName(fmt.Sprintf("Imw%d", n))
	for xObjectForm.Resources.HasXObjectByName(name) {
		n++
		name = core.PdfObjectName(fmt.Sprintf("Imw%d", n))
	}
	err = xObjectForm.Resources.SetXObjectImageByName(name, ximg)
	if err != nil {
		return err
	}
	n = 0
	name2 := core.PdfObjectName(fmt.Sprintf("GS%d", n))
	for xObjectForm.Resources.HasExtGState(name2) {
		n++
		name2 = core.PdfObjectName(fmt.Sprintf("GS%d", n))
	}
	dict := core.MakeDict()
	dict.Set("BM", core.MakeName("Normal"))
	dict.Set("CA", core.MakeFloat(opt.Alpha))
	dict.Set("ca", core.MakeFloat(opt.Alpha))
	err = xObjectForm.Resources.AddExtGState(name2, dict)
	if err != nil {
		return err
	}
	s := fmt.Sprintf("q\n"+"/%s gs\n"+"%.0f 0 0 %.0f %.4f %.4f cm\n"+"/%s Do\n"+"Q", name2, width, y, 0.0, 0.0, name)
	xObjectForm.SetContentStream([]byte(s), nil)
	rotate := transform.IdentityMatrix().Rotate(val)
	floats := []float64{rotate[0], rotate[1], rotate[3], rotate[4], rotate[6], rotate[7]}
	xObjectForm.Matrix = core.MakeArrayFromFloats(floats)
	generateXObjectName := p.Resources.GenerateXObjectName()
	err = p.Resources.SetXObjectFormByName(generateXObjectName, xObjectForm)
	if err != nil {
		return err
	}
	s2 := fmt.Sprintf("q\n"+"%.0f 0 0 %.0f %.4f %.4f cm\n"+"/%s Do\n"+"Q", 1.0, 1.0, x, y2, generateXObjectName)
	return p.AppendContentBytes([]byte(s2), true)
}

func newPdfBorderStyleFromPdfObject(obj core.PdfObject) (*PdfBorderStyle, error) {
	borderStyle := &PdfBorderStyle{}
	borderStyle.container = obj
	var dict *core.PdfObjectDictionary
	obj = core.TraceToDirectObject(obj)
	dict, ok := obj.(*core.PdfObjectDictionary)
	if !ok {
		return nil, errors.New("type check")
	}
	if obj := dict.Get("Type"); obj != nil {
		name, ok := obj.(*core.PdfObjectName)
		if !ok {
			common.Log.Debug("Incompatibility with Type not a name object: %T", obj)
		} else {
			if *name != "Border" {
				common.Log.Debug("Warning, Type != Border: %s", *name)
			}
		}
	}
	if w := dict.Get("W"); w != nil {
		f, err := core.GetNumberAsFloat(w)
		if err != nil {
			common.Log.Debug("Error retrieving W: %v", err)
			return nil, err
		}
		borderStyle.W = &f
	}
	if s := dict.Get("S"); s != nil {
		name, ok := s.(*core.PdfObjectName)
		if !ok {
			return nil, errors.New("border S not a name object")
		}
		var style BorderStyle
		switch *name {
		case "S":
			style = BorderStyleSolid
		case "D":
			style = BorderStyleDashed
		case "B":
			style = BorderStyleBeveled
		case "I":
			style = BorderStyleInset
		case "U":
			style = BorderStyleUnderline
		default:
			common.Log.Debug("Invalid style name %s", *name)
			return nil, errors.New("style type range check")
		}
		borderStyle.S = &style
	}
	if d := dict.Get("D"); d != nil {
		arr, ok := d.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Border D dash not an array: %T", d)
			return nil, errors.New("border D type check error")
		}
		nums, err := arr.ToIntegerArray()
		if err != nil {
			common.Log.Debug("Border D Problem converting to integer array: %v", err)
			return nil, err
		}
		borderStyle.D = &nums
	}
	return borderStyle, nil
}

func streamsFromObject(obj core.PdfObject) []*core.PdfObjectStream {
	if obj == nil {
		return nil
	}
	arr, ok := core.GetArray(obj)
	if !ok || arr.Len() == 0 {
		return nil
	}
	streams := make([]*core.PdfObjectStream, 0, arr.Len())
	for _, obj := range arr.Elements() {
		if stream, ok := core.GetStream(obj); ok {
			streams = append(streams, stream)
		}
	}
	return streams
}

// GetOtfType returns the OTF type information if available
func (f *pdfCIDFontType0) GetOtfType() *fonts.OtfType { return f.otfType }

// SetImage updates XObject Image with new image data.
//
// If `img` was loaded from a JPEG and the current filter is a DCTEncoder, the
// original JPEG bytes are embedded unchanged (filter settings such as quality
// are not applied); set a non-DCT filter to force a re-encode. When `cs` is nil
// and `img` carries a compatible embedded ICC profile, the colorspace is an
// ICCBased wrapping the device colorspace.
func (x *XObjectImage) SetImage(img *Image, cs PdfColorspace) error {
	x.Filter.UpdateParams(img.GetParamsDict())
	data, err := encodeImageData(x.Filter, img)
	if err != nil {
		return err
	}
	x.Stream = data
	width := img.Width
	x.Width = &width
	height := img.Height
	x.Height = &height
	bitsPerComponent := img.BitsPerComponent
	x.BitsPerComponent = &bitsPerComponent
	if cs == nil {
		if img.ColorComponents == 1 {
			x.ColorSpace = NewPdfColorspaceDeviceGray()
		} else if img.ColorComponents == 3 {
			x.ColorSpace = NewPdfColorspaceDeviceRGB()
		} else if img.ColorComponents == 4 {
			x.ColorSpace = NewPdfColorspaceDeviceCMYK()
		} else {
			return errors.New("colorspace undefined")
		}
		x.ColorSpace = resolveColorspaceWithICC(x.ColorSpace, img)
	} else {
		x.ColorSpace = cs
	}
	return nil
}

// NewXObjectImageStencilMask creates a 1-bit stencil mask XObject
// (/ImageMask true) from `img`, suitable for use as the /Mask entry of a base
// image XObject (explicit masking, PDF 32000-1:2008 8.9.6).
//
// The mask follows soft-mask conventions: white (high) samples mark areas of
// the base image that are painted, black (low) samples mark areas that are
// masked out (a Decode array maps them to the stencil semantics of 8.9.6.4).
// The optional `threshold` selects the grayscale luminance (0-255) at or
// above which a sample counts as white; when omitted, an image with more than
// one bit per component is binarized using a histogram-based auto threshold
// (see Image.ConvertToBinary). An image that is already 1 bit per component
// is used bit-for-bit (its decode array, if any, is honored) and `threshold`
// is ignored. `img` itself is left unmodified.
//
// If `encoder` is nil, flate encoding is used. DCT encoding cannot represent
// 1-bit data, and predictor-based flate/LZW encoding does not support the
// packed 1-bit rows; both are rejected.
func NewXObjectImageStencilMask(img *Image, encoder core.StreamEncoder, threshold ...uint8) (*XObjectImage, error) {
	if img == nil {
		return nil, errors.New("image is nil")
	}
	if len(threshold) > 1 {
		return nil, fmt.Errorf("expected at most one threshold value, got %d", len(threshold))
	}
	if encoder == nil {
		encoder = core.NewFlateEncoder()
	} else if err := validateStencilMaskEncoder(encoder); err != nil {
		return nil, err
	}
	img2 := *img
	nums := []int{1, 0}
	if img2.ColorComponents == 1 && img2.BitsPerComponent == 1 {
		if len(img2.decode) == 2 && img2.decode[0] > img2.decode[1] {
			nums = []int{0, 1}
		}
	} else if len(threshold) == 1 {
		img3, err := thresholdImage(&img2, threshold[0])
		if err != nil {
			return nil, err
		}
		img2 = *img3
	} else if err := img2.ConvertToBinary(); err != nil {
		return nil, err
	}
	encoder.UpdateParams(img2.GetParamsDict())
	encodeBytes, err := encoder.EncodeBytes(img2.Data)
	if err != nil {
		common.Log.Debug("Error with encoding: %v", err)
		return nil, err
	}
	xObjectImage := NewXObjectImage()
	xObjectImage.Width = &img2.Width
	xObjectImage.Height = &img2.Height
	xObjectImage.BitsPerComponent = &img2.BitsPerComponent
	xObjectImage.ImageMask = core.MakeBool(true)
	xObjectImage.Decode = core.MakeArrayFromIntegers(nums)
	xObjectImage.Filter = encoder
	xObjectImage.Stream = encodeBytes
	return xObjectImage, nil
}

func validateImageMatte(xobj *XObjectImage) error {
	if xobj.SMask == nil {
		return nil
	}
	stream, ok := xobj.SMask.(*core.PdfObjectStream)
	if !ok {
		common.Log.Debug("SMask is not *PdfObjectStream")
		return core.ErrTypeError
	}
	dict := stream.PdfObjectDictionary
	matte := dict.Get("Matte")
	if matte == nil {
		return nil
	}
	f, err := parseMatte(matte.(*core.PdfObjectArray))
	if err != nil {
		return err
	}
	array := core.MakeArrayFromFloats([]float64{f})
	dict.SetIfNotNil("Matte", array)
	return nil
}

// GetContainingPdfObject returns the page as a dictionary within an PdfIndirectObject.
func (p *PdfPage) GetContainingPdfObject() core.PdfObject { return p.primitive }

const noTrustRootsMessage = "no trust roots configured"

type lazyStreamWriter struct{ writer *PdfWriter }

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

const maxLinearizationIterations = 16

// NewPdfColorspaceFromPdfObject loads a PdfColorspace from a PdfObject.  Returns an error if there is
// a failure in loading.
func NewPdfColorspaceFromPdfObject(obj core.PdfObject) (PdfColorspace, error) {
	if obj == nil {
		return nil, nil
	}
	var indirect *core.PdfIndirectObject
	var name *core.PdfObjectName
	var arr *core.PdfObjectArray
	if indirect2, ok := obj.(*core.PdfIndirectObject); ok {
		indirect = indirect2
	}
	obj = core.TraceToDirectObject(obj)
	switch v := obj.(type) {
	case *core.PdfObjectArray:
		arr = v
	case *core.PdfObjectName:
		name = v
	}
	if name != nil {
		switch *name {
		case "DeviceGray":
			return NewPdfColorspaceDeviceGray(), nil
		case "DeviceRGB":
			return NewPdfColorspaceDeviceRGB(), nil
		case "DeviceCMYK":
			return NewPdfColorspaceDeviceCMYK(), nil
		case "Pattern":
			return NewPdfColorspaceSpecialPattern(), nil
		default:
			common.Log.Debug("ERROR: Unknown colorspace %s", *name)
			return nil, errRangeCheck
		}
	}
	if arr != nil && arr.Len() > 0 {
		var obj core.PdfObject = indirect
		if indirect == nil {
			obj = arr
		}
		if name, ok := core.GetName(arr.Get(0)); ok {
			switch name.String() {
			case "DeviceGray":
				if arr.Len() == 1 {
					return NewPdfColorspaceDeviceGray(), nil
				}
			case "DeviceRGB":
				if arr.Len() == 1 {
					return NewPdfColorspaceDeviceRGB(), nil
				}
			case "DeviceCMYK":
				if arr.Len() == 1 {
					return NewPdfColorspaceDeviceCMYK(), nil
				}
			case "CalGray":
				return newPdfColorspaceCalGrayFromPdfObject(obj)
			case "CalRGB":
				return newPdfColorspaceCalRGBFromPdfObject(obj)
			case "Lab":
				return newPdfColorspaceLabFromPdfObject(obj)
			case "ICCBased":
				return newPdfColorspaceICCBasedFromPdfObject(obj)
			case "Pattern":
				return newPdfColorspaceSpecialPatternFromPdfObject(obj)
			case "Indexed":
				return newPdfColorspaceSpecialIndexedFromPdfObject(obj)
			case "Separation":
				return newPdfColorspaceSpecialSeparationFromPdfObject(obj)
			case "DeviceN":
				return newPdfColorspaceDeviceNFromPdfObject(obj)
			default:
				common.Log.Debug("Array with invalid name: %s", *name)
			}
		}
	}
	common.Log.Debug("PDF File Error: Colorspace type error: %s", obj.String())
	return nil, ErrTypeCheck
}

// AddOCSPs adds OCSPs to DSS.
func (d *DSS) AddOCSPs(ocsps [][]byte) ([]*core.PdfObjectStream, error) {
	return d.add(&d.OCSPs, d.ocspsMap, ocsps)
}

// NewComponentKObj builds the component's K dictionary for the given structure
// type, seeding its page number when known so descendants without their own page
// inherit it (required for ParentTree reachability).
func (t *StructureTagInfo) NewComponentKObj(structureType StructureType) *KDict {
	kDictionary := NewKDictionary()
	kDictionary.S = core.MakeName(string(structureType))
	if t.StructPageNumber != nil {
		kDictionary.SetPageNumber(*t.StructPageNumber)
	}
	return kDictionary
}

// SetXObjectImageByName adds the provided XObjectImage to the page resources.
// The added XObjectImage is identified by the specified name.
func (r *PdfPageResources) SetXObjectImageByName(keyName core.PdfObjectName, ximg *XObjectImage) error {
	stream := ximg.ToPdfObject().(*core.PdfObjectStream)
	err := r.SetXObjectByName(keyName, stream)
	return err
}

// SetPrintPageRange sets the value of the printPageRange.
func (vp *ViewerPreferences) SetPrintPageRange(printPageRange []int) {
	vp.printPageRange = printPageRange
}

func (cs *PdfColorspaceICCBased) buildTransform() {
	cs.transformOnce.Do(func() {
		if cs.N != 3 || len(cs.Data) < 128 {
			return
		}
		xform, err := icctransform.NewTransform(cs.Data)
		if err != nil {
			common.Log.Debug("ICCBased: profile not colour-managed (%v); using alternate", err)
			return
		}
		cs.iccTransform = xform
		cs.transformReady = true
	})
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionTrans) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeTrans)))
	dict.SetIfNotNil("Trans", a.Trans)
	return indirect
}

// GetRuneMetrics returns the character metrics for the specified rune.
// A bool flag is returned to indicate whether or not the entry was found.
func (f pdfCIDFontType0) GetRuneMetrics(r rune) (fonts.CharMetrics, bool) {
	runeToGID, ok := f.runeToGID[r]
	if !ok {
		num, ok := core.GetInt(f.DW)
		if !ok {
			return fonts.CharMetrics{}, false
		}
		runeToGID = int(*num)
	}
	return fonts.CharMetrics{Wx: float64(runeToGID)}, true
}

func anyCRLCovers(crls []crlEvidence, cert *x509.Certificate, t time.Time) bool {
	for _, crl := range crls {
		if crl.isDelta() || !crl.covers(cert) {
			continue
		}
		if crlIsCurrent(crl.ThisUpdate, crl.NextUpdate, cert, t, crl.online) {
			return true
		}
		if crl.Number == nil || crl.ThisUpdate.After(cert.NotAfter) {
			continue
		}
		for _, crl2 := range crls {
			if crl2.isDelta() && crl2.number.Cmp(crl.Number) <= 0 && crl2.newerThan(crl.parsedCRL) && bytes.Equal(crl2.idpRaw, crl.idpRaw) && crlIsCurrent(crl2.ThisUpdate, crl2.NextUpdate, cert, t, crl2.online) {
				return true
			}
		}
	}
	return false
}

func (f *fontFile) parseASCIIPart(data []byte) error {
	if len(data) < 2 || string(data[:2]) != "%!" {
		return errors.New("invalid start of ASCII segment")
	}
	asciiSections, s, err := getASCIISections(data)
	if err != nil {
		return err
	}
	type1Dictionary := parseType1Dictionary(asciiSections)
	f.name = type1Dictionary["FontName"]
	if f.name == "" {
		common.Log.Debug(" FontFile has no /FontName")
	}
	if s != "" {
		type1Encoding, err := parseType1Encoding(s)
		if err != nil {
			return err
		}
		customSimpleTextEncoder, err := textencoding.NewCustomSimpleTextEncoder(type1Encoding, nil)
		if err != nil {
			common.Log.Debug("ERROR :UNKNOWN GLYPH: err=%v", err)
			return nil
		}
		f.encoder = customSimpleTextEncoder
	}
	return nil
}

// PdfFieldSignature signature field represents digital signatures and optional data for authenticating
// the name of the signer and verifying document contents.
type PdfFieldSignature struct {
	*PdfField
	*PdfAnnotationWidget
	V    *PdfSignature
	Lock *core.PdfIndirectObject
	SV   *core.PdfIndirectObject
}

// ToGray returns a PdfColorDeviceGray color based on the current RGB color.
func (c *PdfColorDeviceRGB) ToGray() *PdfColorDeviceGray {
	f := 0.3*c.R() + 0.59*c.G() + 0.11*c.B()
	f = math.Min(math.Max(f, 0.0), 1.0)
	return NewPdfColorDeviceGray(f)
}

type fontFile struct {
	name    string
	subtype string
	encoder textencoding.SimpleEncoder
}

// PdfAnnotationScreen represents Screen annotations.
// (Section 12.5.6.18).
type PdfAnnotationScreen struct {
	*PdfAnnotation
	T  core.PdfObject
	MK core.PdfObject
	A  core.PdfObject
	AA core.PdfObject
}

// SetStructParentsKey sets the StructParents key.
func (p *PdfPage) SetStructParentsKey(key int) {
	if key == -1 {
		p.StructParents = nil
	} else {
		p.StructParents = core.MakeInteger(int64(key))
	}
}

// GetNumComponents returns the number of color components of the colorspace device.
// Returns 3 for a Lab device.
func (cs *PdfColorspaceLab) GetNumComponents() int { return 3 }

// UpdateXObjectImageFromImage creates a new XObject Image from an
// Image object `img` and default masks from xobjIn.
// The default masks are overridden if img.hasAlpha
// If `encoder` is nil, uses raw encoding (none).
//
// If `img` was loaded from a JPEG (via ImageHandling.Read) and `encoder` is a
// DCTEncoder (including the default chosen when encoder is nil), the original
// JPEG bytes are embedded unchanged; encoder settings such as quality are not
// applied. Supply a non-DCT encoder to force a re-encode.
//
// When `cs` is nil and `img` carries a compatible embedded ICC profile, the
// colorspace is an ICCBased wrapping the device colorspace; pass an explicit
// `cs` to override.
func UpdateXObjectImageFromImage(xobjIn *XObjectImage, img *Image, cs PdfColorspace, encoder core.StreamEncoder) (*XObjectImage, error) {
	if encoder == nil {
		if img.canPassThroughJPEG() {
			encoder = core.NewDCTEncoder()
		} else {
			var err error
			encoder, err = img.getSuitableEncoder()
			if err != nil {
				common.Log.Debug("Failure on finding suitable image encoder, fallback to RawEncoder: %v", err)
				encoder = core.NewRawEncoder()
			}
		}
	}
	encoder.UpdateParams(img.GetParamsDict())
	data, err := encodeImageData(encoder, img)
	if err != nil {
		common.Log.Debug("Error with encoding: %v", err)
		return nil, err
	}
	xObjectImage := NewXObjectImage()
	width := img.Width
	height := img.Height
	xObjectImage.Width = &width
	xObjectImage.Height = &height
	bitsPerComponent := img.BitsPerComponent
	xObjectImage.BitsPerComponent = &bitsPerComponent
	xObjectImage.Filter = encoder
	xObjectImage.Stream = data
	if cs == nil {
		if img.ColorComponents == 1 {
			xObjectImage.ColorSpace = NewPdfColorspaceDeviceGray()
			if img.BitsPerComponent == 16 {
				switch encoder.(type) {
				case *core.DCTEncoder:
					xObjectImage.ColorSpace = NewPdfColorspaceDeviceRGB()
					bitsPerComponent = 8
					xObjectImage.BitsPerComponent = &bitsPerComponent
				}
			}
		} else if img.ColorComponents == 3 {
			xObjectImage.ColorSpace = NewPdfColorspaceDeviceRGB()
		} else if img.ColorComponents == 4 {
			switch encoder.(type) {
			case *core.DCTEncoder:
				xObjectImage.ColorSpace = NewPdfColorspaceDeviceRGB()
			default:
				xObjectImage.ColorSpace = NewPdfColorspaceDeviceCMYK()
			}
		} else {
			return nil, errors.New("colorspace undefined")
		}
		xObjectImage.ColorSpace = resolveColorspaceWithICC(xObjectImage.ColorSpace, img)
	} else {
		xObjectImage.ColorSpace = cs
	}
	if len(img.alpha) != 0 {
		xobj := NewXObjectImage()
		xobj.Filter = encoder
		encodeBytes, err := encoder.EncodeBytes(img.alpha)
		if err != nil {
			common.Log.Debug("Error with encoding: %v", err)
			return nil, err
		}
		xobj.Stream = encodeBytes
		xobj.BitsPerComponent = xObjectImage.BitsPerComponent
		xobj.Width = &img.Width
		xobj.Height = &img.Height
		xobj.ColorSpace = NewPdfColorspaceDeviceGray()
		xObjectImage.SMask = xobj.ToPdfObject()
	} else {
		xObjectImage.SMask = xobjIn.SMask
		xObjectImage.ImageMask = xobjIn.ImageMask
		if xObjectImage.ColorSpace.GetNumComponents() == 1 {
			validateImageMatte(xObjectImage)
		}
	}
	return xObjectImage, nil
}

// EmbeddedFile represents an embedded file.
type EmbeddedFile struct {
	Name         string
	Content      []byte
	FileType     string
	Description  string
	Relationship FileRelationship
	Hash         string
	CreationTime time.Time
	ModTime      time.Time
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationUnderline) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Underline"))
	dict.SetIfNotNil("QuadPoints", a.QuadPoints)
	return indirect
}

func (r *PdfReader) buildNameNodes(indirect *core.PdfIndirectObject, set map[core.PdfObject]struct{}) error {
	if indirect == nil {
		return nil
	}
	if _, ok := set[indirect]; ok {
		common.Log.Debug("Cyclic recursion, skipping (%v)", indirect.ObjectNumber)
		return nil
	}
	set[indirect] = struct{}{}
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return errors.New("node not a dictionary")
	}
	if dests, ok := core.GetDict(dict.Get("Dests")); ok {
		kids, ok := core.GetArray(dests.Get("Kids"))
		if !ok {
			return errors.New("Invalid Kids array object")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Kids: %s", kids)
		}
		for i, obj := range kids.Elements() {
			indirect, ok := core.GetIndirect(obj)
			if !ok {
				common.Log.Debug("ERROR: child not indirect object - (%s)", indirect)
				return errors.New("child not indirect object")
			}
			kids.Set(i, indirect)
			err := r.buildNameNodes(indirect, set)
			if err != nil {
				return err
			}
		}
	}
	if dictDict, ok := core.GetDict(dict); ok {
		if !core.IsNullObject(dictDict.Get("Kids")) {
			if kids, ok := core.GetArray(dictDict.Get("Kids")); ok {
				for i, obj := range kids.Elements() {
					if indirect, ok := core.GetIndirect(obj); ok {
						kids.Set(i, indirect)
						err := r.buildNameNodes(indirect, set)
						if err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

// ViewerPreferences represents the viewer preferences of a PDF document.
type ViewerPreferences struct {
	hideToolbar           *bool
	hideMenubar           *bool
	hideWindowUI          *bool
	fitWindow             *bool
	centerWindow          *bool
	displayDocTitle       *bool
	nonFullScreenPageMode NonFullScreenPageMode
	direction             Direction
	viewArea              PageBoundary
	viewClip              PageBoundary
	printArea             PageBoundary
	printClip             PageBoundary
	printScaling          PrintScaling
	duplex                Duplex
	pickTrayByPDFSize     *bool
	printPageRange        []int
	numCopies             int
}

// PdfInfo holds document information that will overwrite
// document information global variables defined above.
type PdfInfo struct {
	Title        *core.PdfObjectString
	Author       *core.PdfObjectString
	Subject      *core.PdfObjectString
	Keywords     *core.PdfObjectString
	Creator      *core.PdfObjectString
	Producer     *core.PdfObjectString
	CreationDate *PdfDate
	ModifiedDate *PdfDate
	Trapped      *core.PdfObjectName
	dict         *core.PdfObjectDictionary
}

// PdfAnnotationPrinterMark represents PrinterMark annotations.
// (Section 12.5.6.20).
type PdfAnnotationPrinterMark struct {
	*PdfAnnotation
	MN core.PdfObject
}

func (c *parsedCRL) isDelta() bool { return c.number != nil }

// Duplex returns the value of the duplex.
func (vp *ViewerPreferences) Duplex() Duplex { return vp.duplex }

// NewPdfActionURI returns a new "Uri" action.
func NewPdfActionURI() *PdfActionURI {
	action := NewPdfAction()
	uriAction := &PdfActionURI{}
	uriAction.PdfAction = action
	action.SetContext(uriAction)
	return uriAction
}

func writeTrailerDict(buf *bytes.Buffer, writer *PdfWriter, n int64, count int64, ok bool) {
	buf.WriteString("<<")
	fmt.Fprintf(buf, "/Size %d", n)
	if writer.root != nil {
		fmt.Fprintf(buf, "/Root %d 0 R", writer.root.ObjectNumber)
	}
	if writer.infoObj != nil {
		fmt.Fprintf(buf, "/Info %d 0 R", writer.infoObj.ObjectNumber)
	}
	if ok {
		fmt.Fprintf(buf, "/Prev %010d", count)
	}
	if writer.crypter != nil && writer.encryptObj != nil {
		fmt.Fprintf(buf, "/Encrypt %d 0 R", writer.encryptObj.ObjectNumber)
	}
	if writer.ids == nil && writer.id0 != "" && writer.id1 != "" {
		writer.ids = core.MakeArray(core.MakeHexString(writer.id0), core.MakeHexString(writer.id1))
	}
	if writer.ids != nil {
		buf.WriteString("/ID")
		buf.Write(writer.ids.Write())
	}
	buf.WriteString(">>")
}

func newIDTreeFromPdfObject(obj core.PdfObject) *IDTree {
	obj = core.ResolveReference(obj)
	array := core.MakeArray()
	arr := core.MakeArray()
	idTrees := []*IDTree{}
	if dict, ok := core.GetDict(obj); ok {
		if names := dict.Get("Names"); names != nil {
			names = core.ResolveReference(names)
			if namesArr, ok := core.GetArray(names); ok {
				for _, obj := range namesArr.Elements() {
					array.Append(obj)
				}
			}
		}
		if limits := dict.Get("Limits"); limits != nil {
			limits = core.ResolveReference(limits)
			if limitsArr, ok := core.GetArray(limits); ok {
				for i := 0; i < 2; i++ {
					arr.Append(limitsArr.Get(i))
				}
			}
		}
		if kids := dict.Get("Kids"); kids != nil {
			kids = core.ResolveReference(kids)
			if kidsArr, ok := core.GetArray(kids); ok {
				for _, obj := range kidsArr.Elements() {
					idTree := newIDTreeFromPdfObject(obj)
					idTrees = append(idTrees, idTree)
				}
			}
		}
	}
	idTree := &IDTree{Names: array, Limits: arr}
	if len(idTrees) > 0 {
		idTree.Kids = idTrees
	}
	return idTree
}

var (
	TabOrderRow       TabOrderType = "R"
	TabOrderColumn    TabOrderType = "C"
	TabOrderStructure TabOrderType = "S"
)

// UpdateObject marks `obj` as updated and to be included in the following revision.
func (a *PdfAppender) UpdateObject(obj core.PdfObject) {
	a.replaceObject(obj, obj)
	if _, ok := a.hasNewObject[obj]; !ok {
		a.newObjects = append(a.newObjects, obj)
		a.hasNewObject[obj] = struct{}{}
	}
}

// PdfActionGoToE represents a GoToE action.
type PdfActionGoToE struct {
	*PdfAction
	F         *PdfFilespec
	D         core.PdfObject
	NewWindow core.PdfObject
	T         core.PdfObject
}

// NewPdfColorPatternType1 returns an empty color shading pattern type 1 (Function-based).
func NewPdfColorPatternType1() *PdfColorPatternType1 {
	patternType1Color := &PdfColorPatternType1{}
	return patternType1Color
}

// DecodeArray returns the range of color component values in DeviceGray colorspace.
func (cs *PdfColorspaceDeviceGray) DecodeArray() []float64 { return []float64{0, 1.0} }

func (f PdfFont) actualFont() pdfFont {
	if f.context == nil {
		common.Log.Debug("ERROR: actualFont. context is nil. font=%s", f)
	}
	return f.context
}

// GenerateRandomID generates a random ID for the K dictionary object.
func (k *KDict) GenerateRandomID() string {
	id, err := uuid.NewUUID()
	if err != nil {
		common.Log.Debug("Error generating UUID: %v", err)
	}
	if id != uuid.Nil {
		s := id.String()
		k.ID = core.MakeString(s)
		return s
	}
	return ""
}

// GetContainingPdfObject returns the container of the outline (indirect object).
func (o *PdfOutline) GetContainingPdfObject() core.PdfObject { return o.primitive }

// MergePageWith appends page content to source Pdf file page content.
func (a *PdfAppender) MergePageWith(pageNum int, page *PdfPage) error {
	n := pageNum - 1
	var page2 *PdfPage
	for i, page3 := range a.pages {
		if i == n {
			page2 = page3
		}
	}
	if page2 == nil {
		return fmt.Errorf("ERROR: Page dictionary %d not found in the source document", pageNum)
	}
	if page2.primitive != nil && page2.primitive.GetParser() == a.roReader.parser {
		page2 = page2.Duplicate()
		a.pages[n] = page2
	}
	page = page.Duplicate()
	pageToPdfObjectLicensed(page)
	pageResourceObjects := collectPageResourceObjects(page2)
	objByName := collectPageResourceObjects(page)
	nameByName := make(map[core.PdfObjectName]core.PdfObjectName)
	for name := range objByName {
		if _, ok := pageResourceObjects[name]; ok {
			for i := 1; true; i++ {
				name2 := core.PdfObjectName(string(name) + strconv.Itoa(i))
				if _, ok := pageResourceObjects[name2]; !ok {
					nameByName[name] = name2
					break
				}
			}
		}
	}
	contentStreams, err := page.GetContentStreams()
	if err != nil {
		return err
	}
	strs, err := page2.GetContentStreams()
	if err != nil {
		return err
	}
	for i, contentStream := range contentStreams {
		for name, name2 := range nameByName {
			contentStream = strings.ReplaceAll(contentStream, "/"+string(name), "/"+string(name2))
		}
		contentStreams[i] = contentStream
	}
	strs = append(strs, contentStreams...)
	if err := page2.SetContentStreams(strs, core.NewFlateEncoder()); err != nil {
		return err
	}
	page2.annotations = append(page2.annotations, page.annotations...)
	if page2.Resources == nil {
		page2.Resources = NewPdfPageResources()
	}
	if page.Resources != nil {
		page2.Resources.Font = a.mergeResources(page2.Resources.Font, page.Resources.Font, nameByName)
		page2.Resources.XObject = a.mergeResources(page2.Resources.XObject, page.Resources.XObject, nameByName)
		page2.Resources.Properties = a.mergeResources(page2.Resources.Properties, page.Resources.Properties, nameByName)
		if page2.Resources.ProcSet == nil {
			page2.Resources.ProcSet = page.Resources.ProcSet
		}
		page2.Resources.Shading = a.mergeResources(page2.Resources.Shading, page.Resources.Shading, nameByName)
		page2.Resources.ExtGState = a.mergeResources(page2.Resources.ExtGState, page.Resources.ExtGState, nameByName)
	}
	mediaBox, err := page2.GetMediaBox()
	if err != nil {
		return err
	}
	rect, err := page.GetMediaBox()
	if err != nil {
		return err
	}
	var ok bool
	if mediaBox.Llx > rect.Llx {
		mediaBox.Llx = rect.Llx
		ok = true
	}
	if mediaBox.Lly > rect.Lly {
		mediaBox.Lly = rect.Lly
		ok = true
	}
	if mediaBox.Urx < rect.Urx {
		mediaBox.Urx = rect.Urx
		ok = true
	}
	if mediaBox.Ury < rect.Ury {
		mediaBox.Ury = rect.Ury
		ok = true
	}
	if ok {
		page2.MediaBox = mediaBox
	}
	return nil
}

// NewPdfFontFromPdfObject loads a PdfFont from the dictionary `fontObj`.  If there is a problem an
// error is returned.
func NewPdfFontFromPdfObject(fontObj core.PdfObject) (*PdfFont, error) {
	return newPdfFontFromPdfObjectInternal(fontObj, true)
}

// NewCompliancePdfReaderAt is the explicit-size form of NewCompliancePdfReader.
func NewCompliancePdfReaderAt(r io.ReaderAt, size int64) (*CompliancePdfReader, error) {
	const trackNameNewCompliancePdfReader = "model:NewCompliancePdfReader"
	readerTracked, err := newPdfReaderTracked(r, size, &ReaderOpts{ComplianceMode: true}, false, false, trackNameNewCompliancePdfReader)
	if err != nil {
		return nil, err
	}
	return &CompliancePdfReader{PdfReader: readerTracked}, nil
}

// GetCatalogLanguage gets catalog Language object.
func (r *PdfReader) GetCatalogLanguage() (core.PdfObject, bool) {
	if r.catalog == nil {
		return nil, false
	}
	lang := r.catalog.Get("Lang")
	return lang, lang != nil
}

func (s *PdfShading) getShadingDict() (*core.PdfObjectDictionary, error) {
	obj := s.primitive
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
		if !ok {
			return nil, core.ErrTypeError
		}
		return dict, nil
	} else if stream, ok := obj.(*core.PdfObjectStream); ok {
		return stream.PdfObjectDictionary, nil
	} else if dict, ok := obj.(*core.PdfObjectDictionary); ok {
		return dict, nil
	} else {
		common.Log.Debug("Unable to access shading dictionary")
		return nil, core.ErrTypeError
	}
}

// PdfOutputIntentType is the subtype of the given PdfOutputIntent.
type PdfOutputIntentType int

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationCircle) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("Circle"))
	dict.SetIfNotNil("BS", a.BS)
	dict.SetIfNotNil("IC", a.IC)
	dict.SetIfNotNil("BE", a.BE)
	dict.SetIfNotNil("RD", a.RD)
	return indirect
}

// PageProcessCallback callback function used in page loading
// that could be used to modify the page content.
//
// If an error is returned, the `ToWriter` process would fail.
type PageProcessCallback func(n int, page *PdfPage) error

// PdfOutline represents a PDF outline dictionary (Table 152 - p. 376).
type PdfOutline struct {
	PdfOutlineTreeNode
	Parent    *PdfOutlineTreeNode
	Count     *int64
	primitive *core.PdfIndirectObject
}

// NewPdfColorspaceDeviceN returns an initialized PdfColorspaceDeviceN.
func NewPdfColorspaceDeviceN() *PdfColorspaceDeviceN {
	deviceNCS := &PdfColorspaceDeviceN{}
	return deviceNCS
}

// PdfColorDeviceRGB represents a color in DeviceRGB colorspace with R, G, B components, where component is
// defined in the range 0.0 - 1.0 where 1.0 is the primary intensity.
type PdfColorDeviceRGB [3]float64

// SetPdfTitle sets the Title attribute of the output PDF.
func SetPdfTitle(title string) { metadataMu.Lock(); defer metadataMu.Unlock(); pdfTitle = title }

// Image interface is a basic representation of an image used in PDF.
// The colorspace is not specified, but must be known when handling the image.
type Image struct {
	Width            int64
	Height           int64
	BitsPerComponent int64
	ColorComponents  int
	Data             []byte
	alpha            []byte
	decode           []float64
	iccProfile       []byte
	encodedData      []byte
	encodedFormat    string
}

// PdfColorPatternType3 represents a color shading pattern type 3 (Radial).
type PdfColorPatternType3 struct {
	Color       PdfColor
	PatternName core.PdfObjectName
}

func (r *PdfReader) buildOutlineTree(obj core.PdfObject, node *PdfOutlineTreeNode, node2 *PdfOutlineTreeNode, set map[core.PdfObject]struct{}) (*PdfOutlineTreeNode, *PdfOutlineTreeNode, error) {
	if set == nil {
		set = map[core.PdfObject]struct{}{}
	}
	set[obj] = struct{}{}
	indirect, ok := obj.(*core.PdfIndirectObject)
	if !ok {
		return nil, nil, fmt.Errorf("outline container not an indirect object %T", obj)
	}
	dict, isDict := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !isDict {
		return nil, nil, errors.New("not a dictionary object")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("build outline tree: dict: %v (%v) p: %p", dict, indirect, indirect)
	}
	if title := dict.Get("Title"); title != nil {
		item, err := r.newPdfOutlineItemFromIndirectObject(indirect)
		if err != nil {
			return nil, nil, err
		}
		item.Parent = node
		item.Prev = node2
		first := core.ResolveReference(dict.Get("First"))
		if _, ok := set[first]; first != nil && first != indirect && !ok {
			if !core.IsNullObject(first) {
				outlineTree, node, err := r.buildOutlineTree(first, &item.PdfOutlineTreeNode, nil, set)
				if err != nil {
					common.Log.Debug("DEBUG: could not build outline item tree: %v. Skipping node children.", err)
				} else {
					item.First = outlineTree
					item.Last = node
				}
			}
		}
		next := core.ResolveReference(dict.Get("Next"))
		if _, ok := set[next]; next != nil && next != indirect && !ok {
			if !core.IsNullObject(next) {
				outlineTree, node2, err := r.buildOutlineTree(next, node, &item.PdfOutlineTreeNode, set)
				if err != nil {
					common.Log.Debug("DEBUG: could not build outline tree for Next node: %v. Skipping node.", err)
				} else {
					item.Next = outlineTree
					return &item.PdfOutlineTreeNode, node2, nil
				}
			}
		}
		return &item.PdfOutlineTreeNode, &item.PdfOutlineTreeNode, nil
	}
	outline, err := newPdfOutlineFromIndirectObject(indirect)
	if err != nil {
		return nil, nil, err
	}
	outline.Parent = node
	if first := dict.Get("First"); first != nil {
		first = core.ResolveReference(first)
		if _, ok := set[first]; first != nil && first != indirect && !ok {
			obj := core.TraceToDirectObject(first)
			if _, ok := obj.(*core.PdfObjectNull); !ok && obj != nil {
				outlineTree, node, err := r.buildOutlineTree(first, &outline.PdfOutlineTreeNode, nil, set)
				if err != nil {
					common.Log.Debug("DEBUG: could not build outline tree: %v. Skipping node children.", err)
				} else {
					outline.First = outlineTree
					outline.Last = node
				}
			}
		}
	}
	return &outline.PdfOutlineTreeNode, &outline.PdfOutlineTreeNode, nil
}

var (
	ArtifactSubtypeHeader    ArtifactSubtype = "Header"
	ArtifactSubtypeFooter    ArtifactSubtype = "Footer"
	ArtifactSubtypeWatermark ArtifactSubtype = "Watermark"
)

const (
	FieldTypeUnknown FieldType = iota
	FieldTypeText
	FieldTypeButton
	FieldTypeChoice
	FieldTypeSignature
)

// B returns the value of the blue component of the color.
func (c *PdfColorDeviceRGB) B() float64 { return c[2] }

func cidToGIDMapFromObject(obj core.PdfObject) []uint16 {
	stream, ok := core.GetStream(obj)
	if !ok {
		return nil
	}
	data, err := core.DecodeStream(stream)
	if err != nil {
		common.Log.Debug("WARN: could not decode CIDToGIDMap stream: %v", err)
		return nil
	}
	if len(data) == 0 || len(data)%2 != 0 {
		common.Log.Debug("WARN: invalid CIDToGIDMap stream length %d; using Identity", len(data))
		return nil
	}
	n := len(data) / 2
	if n > 65536 {
		n = 65536
	}
	vals := make([]uint16, n)
	for i := 0; i < n; i++ {
		vals[i] = uint16(data[2*i])<<8 | uint16(data[2*i+1])
	}
	return vals
}

// GetColorGlyphInfoByText retrieves color glyph information for a text string from an OTF font.
// This function handles multi-rune sequences like ZWJ emoji by using text shaping to find the
// appropriate glyph. Text can be a single rune or a multi-rune sequence (e.g., "🙋‍♂️").
func GetColorGlyphInfoByText(otf *fonts.OtfType, text string) *ColorGlyphInfo {
	if otf.Face == nil {
		return nil
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	if len(runes) == 1 {
		return GetColorGlyphInfoByRune(otf, runes[0])
	}
	shapeTextLine := textencoding.ShapeTextLine(otf.Face, text, 10.0)
	if len(shapeTextLine) == 0 {
		return nil
	}
	for _, output := range shapeTextLine {
		if len(output.Glyphs) == 0 {
			continue
		}
		if len(output.Glyphs) > 1 {
			if slices.Contains(runes, 0x200D) {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Font does not have a combined glyph for ZWJ sequence '%s' (got %d glyphs instead of 1). Falling back.", text, len(output.Glyphs))
				}
				if len(runes) >= 2 && runes[1] >= 0x1F3FB && runes[1] <= 0x1F3FF {
					s := string(runes[:2])
					shapeTextLine := textencoding.ShapeTextLine(otf.Face, s, 10.0)
					for _, output := range shapeTextLine {
						if len(output.Glyphs) == 1 {
							if colorGlyphInfo, ok := GetColorGlyphInfo(otf, output.Glyphs[0].GlyphID); ok {
								return colorGlyphInfo
							}
						}
					}
				}
				return GetColorGlyphInfoByRune(otf, runes[0])
			}
		}
		glyphID := output.Glyphs[0].GlyphID
		if colorGlyphInfo, ok := GetColorGlyphInfo(otf, glyphID); ok {
			return colorGlyphInfo
		}
	}
	return nil
}

// PdfShadingType3 is a Radial shading.
type PdfShadingType3 struct {
	*PdfShading
	Coords   *core.PdfObjectArray
	Domain   *core.PdfObjectArray
	Function []PdfFunction
	Extend   *core.PdfObjectArray
}

// DecodeArray returns the range of color component values in the Lab colorspace.
func (cs *PdfColorspaceLab) DecodeArray() []float64 {
	floats := []float64{0, 100}
	if cs != nil && cs.Range != nil && len(cs.Range) == 4 {
		floats = append(floats, cs.Range...)
	} else {
		floats = append(floats, -100, 100, -100, 100)
	}
	return floats
}

// Schema returns the namespace schema, a reference to a file containing a
// schema for the namespace (ISO 32000-2 14.7.4.2, table 356). Nil if unset.
func (ns *PdfStructureNamespace) Schema() core.PdfObject { return ns.schema }

func firstPageHintOffset(layout *linearizationLayout, table map[int64]int64, n, count int64) (uint32, error) {
	if len(layout.firstShared) == 0 {
		return 0, nil
	}
	return adjustHintOffset(table[layout.firstShared[0]], n, count)
}

const (
	RelationshipSource FileRelationship = iota
	RelationshipData
	RelationshipAlternative
	RelationshipSupplement
	RelationshipUnspecified
)

// IsCheckbox returns true if the button field represents a checkbox, false otherwise.
func (f *PdfFieldButton) IsCheckbox() bool { return f.GetType() == ButtonTypeCheckbox }

func newPdfFontDescriptorFromTtf(ttf *fonts.TtfType, f float64, n int) *PdfFontDescriptor {
	v := 1000.0 / float64(ttf.UnitsPerEm)
	if ttf.UnitsPerEm == 0 {
		v = 1.0
	}
	desc := &PdfFontDescriptor{FontName: core.MakeName(ttf.PostScriptName), Ascent: core.MakeFloat(v * float64(ttf.TypoAscender)), Descent: core.MakeFloat(v * float64(ttf.TypoDescender)), CapHeight: core.MakeFloat(v * float64(ttf.CapHeight)), FontBBox: core.MakeArrayFromFloats([]float64{v * float64(ttf.Xmin), v * float64(ttf.Ymin), v * float64(ttf.Xmax), v * float64(ttf.Ymax)}), ItalicAngle: core.MakeFloat(ttf.ItalicAngle), MissingWidth: core.MakeFloat(f)}
	if ttf.Bold {
		desc.StemV = core.MakeInteger(120)
	} else {
		desc.StemV = core.MakeInteger(70)
	}
	count := n
	if ttf.IsFixedPitch {
		count |= fontFlagFixedPitch
	}
	if ttf.ItalicAngle != 0 {
		count |= fontFlagItalic
	}
	desc.Flags = core.MakeInteger(int64(count))
	return desc
}

func newPdfFontFromPdfObjectInternal(fontObj core.PdfObject, ok bool) (*PdfFont, error) {
	dict, fc, err := newFontCommonFromPdfObject(fontObj)
	if dict != nil {
		fixFontWidthsRange(dict)
	}
	if err != nil {
		if err == ErrType1CFontNotSupported {
			fontSimple, fontSimpleErr := newPdfFontSimpleFromDict(dict, fc, nil)
			if fontSimpleErr != nil {
				common.Log.Debug("ERROR: While loading simple font: font=%s err=%v", fc, fontSimpleErr)
				return nil, err
			}
			return &PdfFont{context: fontSimple}, err
		}
		return nil, err
	}
	font := &PdfFont{}
	switch fc.subtype {
	case "Type0":
		if !ok {
			common.Log.Debug("ERROR: Loading type0 not allowed. font=%s", fc)
			return nil, errors.New("cyclical type0 loading")
		}
		fontType0, err := newPdfFontType0FromDict(dict, fc)
		if err != nil {
			common.Log.Debug("ERROR: While loading Type0 font. font=%s err=%v", fc, err)
			return nil, err
		}
		font.context = fontType0
	case "Type1", "MMType1", "TrueType":
		var font2 *pdfFontSimple
		stdFontByName, ok := fonts.NewStdFontByName(fonts.StdFontName(fc.baseFont))
		if ok {
			fontSimple := newPdfFontSimpleFromStdFont(stdFontByName)
			font2, err = newPdfFontSimpleFromDict(dict, fc, fontSimple.std14Encoder)
			if err != nil {
				common.Log.Debug("ERROR: Bad Standard14\n\tfont=%s\n\tstd=%+v", fc, fontSimple)
				return nil, err
			}
			font2.fontMetrics = fontSimple.fontMetrics
			if font2.Widths == nil {
				font2.charWidths = fontSimple.charWidths
			}
			if font2.std14Descriptor == nil {
				font2.std14Descriptor = fontSimple.std14Descriptor
			}
			mergeFontDescriptor(font2.fontDescriptor, fontSimple.std14Descriptor)
		} else {
			font2, err = newPdfFontSimpleFromDict(dict, fc, nil)
			if err != nil {
				common.Log.Debug("ERROR: While loading simple font: font=%s err=%v", fc, err)
				return nil, err
			}
		}
		err = font2.addEncoding()
		if err != nil {
			return nil, err
		}
		if ok && font2.Widths == nil {
			font2.updateStandard14Font()
		}
		if ok && font2.encoder == nil && font2.std14Encoder == nil {
			common.Log.Error("simplefont=%s", font2)
			common.Log.Error("fnt=%+v", stdFontByName)
		}
		if len(font2.charWidths) == 0 {
			common.Log.Debug("ERROR: No widths. font=%s", font2)
		}
		font.context = font2
	case "Type3":
		fontType3, err := newPdfFontType3FromDict(dict, fc)
		if err != nil {
			common.Log.Debug("ERROR: While loading type3 font: %v", err)
			return nil, err
		}
		font.context = fontType3
	case "CIDFontType0":
		cidFontType0, err := newPdfCIDFontType0FromDict(dict, fc)
		if err != nil {
			common.Log.Debug("ERROR: While loading cid font type0 font: %v", err)
			return nil, err
		}
		font.context = cidFontType0
	case "CIDFontType2":
		cidFontType2, err := newPdfCIDFontType2FromDict(dict, fc)
		if err != nil {
			common.Log.Debug("ERROR: While loading cid font type2 font. font=%s err=%v", fc, err)
			return nil, err
		}
		font.context = cidFontType2
	default:
		common.Log.Debug("ERROR: Unsupported font type: font=%s", fc)
		return nil, fmt.Errorf("unsupported font type: font=%s", fc)
	}
	return font, nil
}

func (img *Image) getSuitableEncoder() (core.StreamEncoder, error) {
	var (
		width, height = int(img.Width), int(img.Height)
		set           = make(map[string]bool)
		ok            = true
		found         = false
		fn            = func() *core.DCTEncoder { return core.NewDCTEncoder() }
		fn2           = func() *core.DCTEncoder {
			dctEncoder := core.NewDCTEncoder()
			dctEncoder.BitsPerComponent = 16
			return dctEncoder
		}
	)
	for i := 0; i < height; i++ {
		for j := 0; j < width; j++ {
			colorAt, err := img.ColorAt(j, i)
			if err != nil {
				return nil, err
			}
			rgba, n, count, num := colorAt.RGBA()
			if ok && (rgba != n || rgba != count || n != count) {
				ok = false
			}
			if !found {
				switch colorAt.(type) {
				case color.NRGBA:
					found = num > 0
				}
			}
			set[fmt.Sprintf("%d,%d,%d", rgba, n, count)] = true
			if len(set) > 2 && found {
				return fn2(), nil
			}
		}
	}
	if found || len(img.alpha) > 0 {
		return core.NewFlateEncoder(), nil
	}
	if len(set) <= 2 {
		err := img.ConvertToBinary()
		if err != nil {
			return nil, err
		}
		return core.NewJBIG2Encoder(), nil
	}
	if ok {
		return fn(), nil
	}
	if img.ColorComponents == 1 {
		if img.BitsPerComponent == 1 {
			return core.NewJBIG2Encoder(), nil
		} else if img.BitsPerComponent == 8 {
			dctEncoder := core.NewDCTEncoder()
			dctEncoder.ColorComponents = 1
			return dctEncoder, nil
		}
	} else if img.ColorComponents == 3 {
		if img.BitsPerComponent == 8 {
			return fn(), nil
		} else if img.BitsPerComponent == 16 {
			return fn2(), nil
		}
	} else if img.ColorComponents == 4 {
		dctEncoder := fn2()
		dctEncoder.ColorComponents = 4
		return dctEncoder, nil
	}
	return fn2(), nil
}

// DecodeArray returns the component range values for the Separation colorspace.
func (cs *PdfColorspaceSpecialSeparation) DecodeArray() []float64 { return []float64{0, 1.0} }

func (w *PdfWriter) setWriter(writer io.Writer) {
	w.writePos = w.startOffset
	w.writeErr = nil
	w.bufWriter = bufio.NewWriter(writer)
}

func (w *PdfWriter) writeOutputIntents() error {
	if len(w.outputIntents) == 0 {
		return nil
	}
	objs := make([]core.PdfObject, len(w.outputIntents))
	for i, outputIntent := range w.outputIntents {
		obj := outputIntent.ToPdfObject()
		objs[i] = core.MakeIndirectObject(obj)
	}
	indirectObject := core.MakeIndirectObject(core.MakeArray(objs...))
	w.catalog.Set("OutputIntents", indirectObject)
	if err := w.addObjects(indirectObject); err != nil {
		return err
	}
	return nil
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionGoToR) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeGoToR)))
	if a.F != nil {
		dict.Set("F", a.F.ToPdfObject())
	}
	dict.SetIfNotNil("D", a.D)
	dict.SetIfNotNil("NewWindow", a.NewWindow)
	return indirect
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionGoTo3DView) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeGoTo3DView)))
	dict.SetIfNotNil("TA", a.TA)
	dict.SetIfNotNil("V", a.V)
	return indirect
}

func (t *trustEvaluator) intermediatePool(certs, certs2 []*x509.Certificate) *x509.CertPool {
	certPool := x509.NewCertPool()
	for _, cert := range certs2 {
		certPool.AddCert(cert)
	}
	for _, sharedCertificate := range t.sharedCertificates(certs) {
		certPool.AddCert(sharedCertificate)
	}
	return certPool
}

func (r SignatureValidationResult) String() string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Name: %s\n", r.Name)
	if r.Date.year > 0 {
		fmt.Fprintf(&buf, "Date: %s\n", r.Date.ToGoTime().String())
	} else {
		buf.WriteString("Date not specified\n")
	}
	if len(r.Reason) > 0 {
		fmt.Fprintf(&buf, "Reason: %s\n", r.Reason)
	} else {
		buf.WriteString("No reason specified\n")
	}
	if len(r.Location) > 0 {
		fmt.Fprintf(&buf, "Location: %s\n", r.Location)
	} else {
		buf.WriteString("Location not specified\n")
	}
	if len(r.ContactInfo) > 0 {
		fmt.Fprintf(&buf, "Contact Info: %s\n", r.ContactInfo)
	} else {
		buf.WriteString("Contact info not specified\n")
	}
	fmt.Fprintf(&buf, "Fields: %d\n", len(r.Fields))
	if r.IsSigned {
		buf.WriteString("Signed: Document is signed\n")
	} else {
		buf.WriteString("Signed: Not signed\n")
	}
	if r.IsVerified {
		buf.WriteString("Signature validation: Is valid\n")
	} else {
		buf.WriteString("Signature validation: Is invalid\n")
	}
	if r.IsTrusted {
		fmt.Fprintf(&buf, "Trusted: Certificate is trusted (chain of %d certificate(s))\n", len(r.TrustChain))
	} else {
		buf.WriteString("Trusted: Untrusted certificate\n")
	}
	for _, trustError := range r.TrustErrors {
		fmt.Fprintf(&buf, "\t%s\n", trustError)
	}
	for _, trustWarning := range r.TrustWarnings {
		fmt.Fprintf(&buf, "Trust warning: %s\n", trustWarning)
	}
	if !r.ValidatedAt.IsZero() {
		fmt.Fprintf(&buf, "Validated at: %s\n", r.ValidatedAt.UTC().Format(time.RFC3339))
	}
	if r.RevocationStatus != RevocationStatusNotChecked {
		fmt.Fprintf(&buf, "Revocation status: %s\n", r.RevocationStatus)
	}
	if !r.RevocationTime.IsZero() {
		fmt.Fprintf(&buf, "Revocation time: %s\n", r.RevocationTime.UTC().Format(time.RFC3339))
	}
	if signerCertificate := r.SignerCertificate(); signerCertificate != nil {
		fmt.Fprintf(&buf, "Certificate: %s\n", signerCertificate.Subject.String())
		fmt.Fprintf(&buf, "Certificate issuer: %s\n", signerCertificate.Issuer.String())
		fmt.Fprintf(&buf, "Certificate validity: %s - %s\n", signerCertificate.NotBefore.UTC().Format(time.RFC3339), signerCertificate.NotAfter.UTC().Format(time.RFC3339))
		fmt.Fprintf(&buf, "Certificate chain: %d certificate(s)\n", len(r.Certificates))
	} else {
		buf.WriteString("Certificate: not available\n")
	}
	if !r.GeneralizedTime.IsZero() {
		fmt.Fprintf(&buf, "GeneralizedTime: %s\n", r.GeneralizedTime.String())
	}
	if r.byteRangeChecked {
		if r.IsCoveringEntireFile {
			buf.WriteString("Coverage: Entire file is covered by the signature\n")
		} else if r.IsCoveringEntireRevision {
			fmt.Fprintf(&buf, "Coverage: Entire revision is covered by the signature (%d revision(s) appended after signing)\n", r.RevisionsAfterSignature)
		} else {
			buf.WriteString("Coverage: Signed range stops before the end of its revision\n")
		}
		if r.ContentsGapOnly {
			buf.WriteString("ByteRange gap: matches /Contents\n")
		} else {
			buf.WriteString("ByteRange gap: does not exactly match /Contents\n")
		}
	}
	if r.DiffResults != nil {
		fmt.Fprintf(&buf, "diff is permitted: %v\n", r.DiffResults.IsPermitted())
		if len(r.DiffResults.Warnings) > 0 {
			buf.WriteString("MDP warnings:\n")
			for _, warning := range r.DiffResults.Warnings {
				fmt.Fprintf(&buf, "\t%s\n", warning)
			}
		}
		if len(r.DiffResults.Errors) > 0 {
			buf.WriteString("MDP errors:\n")
			for _, diffResult := range r.DiffResults.Errors {
				fmt.Fprintf(&buf, "\t%s\n", diffResult)
			}
		}
	}
	if r.IsCrlFound {
		buf.WriteString("Revocation data: CRL found\n")
	} else {
		buf.WriteString("Revocation data: CRL not found\n")
	}
	if r.IsOcspFound {
		buf.WriteString("Revocation data: OCSP found\n")
	} else {
		buf.WriteString("Revocation data: OCSP not found\n")
	}
	return buf.String()
}

// GetDescent returns the Descent of the font `descriptor`.
func (d *PdfFontDescriptor) GetDescent() (float64, error) {
	return core.GetNumberAsFloat(d.Descent)
}

// GetContext returns the action context which contains the specific type-dependent context.
// The context represents the subaction.
func (a *PdfAction) GetContext() PdfModel {
	if a == nil {
		return nil
	}
	return a.context
}

func collectStructureElements(obj core.PdfObject, obj2 core.PdfObject, objsByObj map[core.PdfObject][]core.PdfObject, objByKey map[string]core.PdfObject, objs *[]core.PdfObject) {
	var indirect *core.PdfIndirectObject
	if indirect2, ok := core.GetIndirect(obj); ok {
		indirect = indirect2
		obj = indirect2.PdfObject
	}
	switch v := obj.(type) {
	case *core.PdfObjectDictionary:
		if v.Get("S") == nil {
			return
		}
		v.Set("P", obj2)
		if pg := v.Get("Pg"); pg != nil {
			if pgIndirect, ok := core.GetIndirect(pg); ok && pgIndirect != nil && pgIndirect.PdfObject != nil {
				objsByObj[pg] = append(objsByObj[pg], indirect)
			}
		}
		if s := v.Get("S"); s != nil {
			if s2, ok := core.GetNameVal(s); ok {
				if s2 == string(StructureTypeLink) || s2 == string(StructureTypeAnnot) {
					if k := v.Get("K"); k != nil {
						if kArr, ok := core.GetArray(k); ok && kArr.Len() == 2 {
							ok := false
							found := false
							for _, obj := range kArr.Elements() {
								if dict, isDict := core.GetDict(obj); isDict {
									if _, valid := core.GetName(dict.Get("Type")); valid {
										found = true
									}
								} else if _, found := core.GetInt(obj); found {
									ok = true
								}
							}
							if ok && found {
								*objs = append(*objs, indirect)
							}
						} else if kDict, ok := core.GetDict(k); ok {
							if _, ok := core.GetName(kDict.Get("Type")); ok {
								*objs = append(*objs, indirect)
							}
						}
					}
				}
			}
		}
		if id := v.Get("ID"); id != nil {
			objByKey[id.String()] = obj
		}
		if k := v.Get("K"); k != nil {
			collectStructureElements(k, indirect, objsByObj, objByKey, objs)
		}
	case *core.PdfObjectArray:
		for _, obj3 := range v.Elements() {
			collectStructureElements(obj3, obj2, objsByObj, objByKey, objs)
		}
	default:
	}
}

// PdfColorspaceLab is a L*, a*, b* 3 component colorspace.
type PdfColorspaceLab struct {
	WhitePoint []float64
	BlackPoint []float64
	Range      []float64
	container  *core.PdfIndirectObject
}

// Colorspace returns the DeviceGray colorspace.
func (c *PdfColorDeviceGray) Colorspace() PdfColorspace { return NewPdfColorspaceDeviceGray() }

// HasShadingByName checks whether a shading is defined by the specified keyName.
func (r *PdfPageResources) HasShadingByName(keyName core.PdfObjectName) bool {
	_, ok := r.GetShadingByName(keyName)
	return ok
}

func newPdfShadingType1FromDict(dict *core.PdfObjectDictionary) (*PdfShadingType1, error) {
	shading1 := PdfShadingType1{}
	if domain := dict.Get("Domain"); domain != nil {
		domain = core.TraceToDirectObject(domain)
		arr, ok := domain.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Domain not an array (got %T)", domain)
			return nil, errors.New("type check error")
		}
		shading1.Domain = arr
	}
	if matrix := dict.Get("Matrix"); matrix != nil {
		matrix = core.TraceToDirectObject(matrix)
		arr, ok := matrix.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Matrix not an array (got %T)", matrix)
			return nil, errors.New("type check error")
		}
		shading1.Matrix = arr
	}
	function := dict.Get("Function")
	if function == nil {
		common.Log.Debug("Required attribute missing:  Function")
		return nil, ErrRequiredAttributeMissing
	}
	shading1.Function = []PdfFunction{}
	if arr, ok := function.(*core.PdfObjectArray); ok {
		for _, obj := range arr.Elements() {
			function, err := newPdfFunctionFromPdfObject(obj)
			if err != nil {
				common.Log.Debug("Error parsing function: %v", err)
				return nil, err
			}
			shading1.Function = append(shading1.Function, function)
		}
	} else {
		fn, err := newPdfFunctionFromPdfObject(function)
		if err != nil {
			common.Log.Debug("Error parsing function: %v", err)
			return nil, err
		}
		shading1.Function = append(shading1.Function, fn)
	}
	return &shading1, nil
}

// PdfActionJavaScript represents a javaScript action.
type PdfActionJavaScript struct {
	*PdfAction
	JS core.PdfObject
}

func (cs *PdfColorspaceSpecialPattern) String() string { return "Pattern" }

// GetMCID returns the MCID of the KValue.
func (kv *KValue) GetMCID() *int { return kv.mcid }

// ToPdfObject implements interface PdfModel.
func (a *PdfActionNamed) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeNamed)))
	dict.SetIfNotNil("N", a.N)
	return indirect
}

func (a *Artifact) GenerateMap() map[string]core.PdfObject {
	objByKey := make(map[string]core.PdfObject)
	if a.Type != ArtifactTypeNone {
		objByKey["Type"] = core.MakeName(string(a.Type))
	}
	if a.BBox != nil {
		objByKey["BBox"] = core.MakeArrayFromFloats([]float64{a.BBox.Llx, a.BBox.Lly, a.BBox.Urx, a.BBox.Ury})
	}
	if len(a.AttachPositions) > 0 {
		var objs []core.PdfObject
		for _, attachPosition := range a.AttachPositions {
			objs = append(objs, core.MakeName(string(attachPosition)))
		}
		objByKey["Attach"] = core.MakeArray(objs...)
	}
	if a.Subtype != "" {
		objByKey["Subtype"] = core.MakeName(a.Subtype)
	}
	return objByKey
}

// GetOutlinesFlattened returns a flattened list of tree nodes and titles.
// NOTE: for most use cases, it is recommended to use the high-level GetOutlines
// method instead, which also provides information regarding the destination
// of the outline items.
func (r *PdfReader) GetOutlinesFlattened() ([]*PdfOutlineTreeNode, []string, error) {
	var nodes []*PdfOutlineTreeNode
	var strs []string
	var fn func(*PdfOutlineTreeNode, *[]*PdfOutlineTreeNode, *[]string, int)
	fn = func(node *PdfOutlineTreeNode, nodes *[]*PdfOutlineTreeNode, strs *[]string, n int) {
		if node == nil {
			return
		}
		if node.context == nil {
			common.Log.Debug("ERROR: Missing node.context")
			return
		}
		item, ok := node.context.(*PdfOutlineItem)
		if ok {
			*nodes = append(*nodes, &item.PdfOutlineTreeNode)
			s := strings.Repeat(" ", n*2) + item.Title.Decoded()
			*strs = append(*strs, s)
		}
		if node.First != nil {
			s := strings.Repeat(" ", n*2) + "+"
			*strs = append(*strs, s)
			fn(node.First, nodes, strs, n+1)
		}
		if ok && item.Next != nil {
			fn(item.Next, nodes, strs, n)
		}
	}
	fn(r.outlineTree, &nodes, &strs, 0)
	return nodes, strs, nil
}

func (r *PdfReader) newPdfAcroFormFromDict(indirect *core.PdfIndirectObject, dict *core.PdfObjectDictionary) (*PdfAcroForm, error) {
	acroForm := NewPdfAcroForm()
	if indirect != nil {
		acroForm.primitive = indirect
		indirect.PdfObject = core.MakeDict()
	}
	if fields := dict.Get("Fields"); fields != nil && !core.IsNullObject(fields) {
		fieldsArr, ok := core.GetArray(fields)
		if !ok {
			return nil, fmt.Errorf("fields not an array (%T)", fields)
		}
		var fields []*PdfField
		for _, obj := range fieldsArr.Elements() {
			indirect, ok := core.GetIndirect(obj)
			if !ok {
				if _, ok := obj.(*core.PdfObjectNull); ok {
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("Skipping over null field")
					}
					continue
				}
				common.Log.Debug("Field not contained in indirect object %T", obj)
				return nil, fmt.Errorf("field not in an indirect object")
			}
			field, err := r.newPdfFieldFromIndirectObject(indirect, nil)
			if err != nil {
				return nil, err
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("AcroForm Field: %+v", *field)
			}
			fields = append(fields, field)
		}
		acroForm.Fields = &fields
	}
	if needAppearances := dict.Get("NeedAppearances"); needAppearances != nil {
		needAppearancesBool, ok := core.GetBool(needAppearances)
		if ok {
			acroForm.NeedAppearances = needAppearancesBool
		} else {
			common.Log.Debug("ERROR: NeedAppearances invalid (got %T)", needAppearances)
		}
	}
	if sigFlags := dict.Get("SigFlags"); sigFlags != nil {
		sigFlagsInt, ok := core.GetInt(sigFlags)
		if ok {
			acroForm.SigFlags = sigFlagsInt
		} else {
			common.Log.Debug("ERROR: SigFlags invalid (got %T)", sigFlags)
		}
	}
	if co := dict.Get("CO"); co != nil {
		coArr, ok := core.GetArray(co)
		if ok {
			acroForm.CO = coArr
		} else {
			common.Log.Debug("ERROR: CO invalid (got %T)", co)
		}
	}
	if dr := dict.Get("DR"); dr != nil {
		if drDict, ok := core.GetDict(dr); ok {
			resources, err := NewPdfPageResourcesFromDict(drDict)
			if err != nil {
				common.Log.Error("Invalid DR: %v", err)
				return nil, err
			}
			acroForm.DR = resources
		} else {
			common.Log.Debug("ERROR: DR invalid (got %T)", dr)
		}
	}
	if da := dict.Get("DA"); da != nil {
		daStr, ok := core.GetString(da)
		if ok {
			acroForm.DA = daStr
		} else {
			common.Log.Debug("ERROR: DA invalid (got %T)", da)
		}
	}
	if q := dict.Get("Q"); q != nil {
		qInt, ok := core.GetInt(q)
		if ok {
			acroForm.Q = qInt
		} else {
			common.Log.Debug("ERROR: Q invalid (got %T)", q)
		}
	}
	if xfa := dict.Get("XFA"); xfa != nil {
		acroForm.XFA = xfa
	}
	if adbeEchoSign := dict.Get("ADBE_EchoSign"); adbeEchoSign != nil {
		acroForm.ADBEEchoSign = adbeEchoSign
	}
	acroForm.ToPdfObject()
	return acroForm, nil
}

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

func (w *PdfWriter) setHashIDs(digest hash.Hash) error {
	data := digest.Sum(nil)
	if w.id0 == "" {
		w.id0 = hex.EncodeToString(data[:8])
	}
	w.setDocumentIDs(w.id0, hex.EncodeToString(data[8:]))
	return nil
}

// NewPdfActionGoToE returns a new "go to embedded" action.
func NewPdfActionGoToE() *PdfActionGoToE {
	action := NewPdfAction()
	goToEAction := &PdfActionGoToE{}
	goToEAction.PdfAction = action
	action.SetContext(goToEAction)
	return goToEAction
}

// GetRuneMetrics returns the character metrics for the specified rune.
// A bool flag is returned to indicate whether or not the entry was found.
func (f pdfFontType3) GetRuneMetrics(r rune) (fonts.CharMetrics, bool) {
	encoder := f.Encoder()
	if encoder == nil {
		common.Log.Debug("No encoder for fonts=%s", f)
		return fonts.CharMetrics{}, false
	}
	runeToCharcode, ok := encoder.RuneToCharcode(r)
	if !ok {
		if r != ' ' {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("No charcode for rune=%v font=%s", r, f)
			}
		}
		return fonts.CharMetrics{}, false
	}
	charMetrics, isCharMetrics := f.GetCharMetrics(runeToCharcode)
	return charMetrics, isCharMetrics
}

// ReplacePage replaces the original page to a new page.
func (a *PdfAppender) ReplacePage(pageNum int, page *PdfPage) {
	n := pageNum - 1
	for i := range a.pages {
		if i == n {
			duplicate := page.Duplicate()
			pageToPdfObjectLicensed(duplicate)
			a.pages[i] = duplicate
		}
	}
}

func (s *PdfSignature) extractChainFromPKCS7() ([]*x509.Certificate, error) {
	if s.Contents == nil {
		return nil, errors.New("signature Contents not set")
	}
	p7, err := pkcs7.Parse(s.Contents.Bytes())
	if err != nil {
		return nil, err
	}
	if len(p7.Certificates) == 0 {
		return nil, nil
	}
	onlySigner := p7.GetOnlySigner()
	if onlySigner == nil {
		return nil, errors.New("signer certificate not identified in PKCS#7 package")
	}
	certs := make([]*x509.Certificate, 0, len(p7.Certificates))
	certs = append(certs, onlySigner)
	for _, certificate := range p7.Certificates {
		if certificate != onlySigner {
			certs = append(certs, certificate)
		}
	}
	return certs, nil
}

// StringToCharcodeBytes maps the provided string runes to charcode bytes and
// it returns the resulting slice of bytes, along with the number of runes
// which could not be converted. If the number of misses is 0, all string runes
// were successfully converted.
func (f *PdfFont) StringToCharcodeBytes(str string) ([]byte, int) {
	return f.RunesToCharcodeBytes([]rune(str))
}

func newPdfCIDFontType2FromDict(dict *core.PdfObjectDictionary, fc *fontCommon) (*pdfCIDFontType2, error) {
	if fc.subtype != "CIDFontType2" {
		common.Log.Debug("ERROR: Font SubType != CIDFontType2. font=%s", fc)
		return nil, core.ErrRangeError
	}
	cidFontType2 := newPdfCIDFontType2FromCommon(fc)
	cidSystemInfo, ok := core.GetDict(dict.Get("CIDSystemInfo"))
	if !ok {
		common.Log.Debug("ERROR: CIDSystemInfo (Required) missing. font=%s", fc)
		return nil, ErrRequiredAttributeMissing
	}
	cidFontType2.CIDSystemInfo = cidSystemInfo
	cidFontType2.DW = dict.Get("DW")
	cidFontType2.W = dict.Get("W")
	cidFontType2.DW2 = dict.Get("DW2")
	cidFontType2.W2 = dict.Get("W2")
	cidFontType2.CIDToGIDMap = dict.Get("CIDToGIDMap")
	cidFontType2.defaultWidth = 1000.0
	if f, err := core.GetNumberAsFloat(cidFontType2.DW); err == nil {
		cidFontType2.defaultWidth = f
	}
	cidWidths, err := parseCIDWidths(cidFontType2.W)
	if err != nil {
		return nil, err
	}
	if cidWidths == nil {
		cidWidths = map[textencoding.CharCode]float64{}
	}
	cidFontType2.charWidths = cidWidths
	return cidFontType2, nil
}
func (f *pdfFontType3) getFontDescriptor() *PdfFontDescriptor { return f.fontDescriptor }

// SetRotation sets the rotation of all pages added to writer. The rotation is
// specified in degrees and must be a multiple of 90.
// The Rotate field of individual pages has priority over the global rotation.
func (w *PdfWriter) SetRotation(rotate int64) error {
	pagesDict, ok := core.GetDict(w.pages)
	if !ok {
		return ErrTypeCheck
	}
	pagesDict.Set("Rotate", core.MakeInteger(rotate))
	return nil
}

// GetProperties returns a snapshot of the field's resolved properties,
// including parent-inherited values for V, DV, Ff, DA, Q, MaxLen. The
// returned pointer is freshly allocated; mutating it does not affect the
// underlying field.
//
// AcroForm-level /DA and /Q defaults are not consulted.
func (f *PdfField) GetProperties() *FieldProperties {
	if f == nil {
		return nil
	}
	fieldProperties := &FieldProperties{PartialName: f.PartialName(), Type: f.FieldType()}
	if fullName, err := f.FullName(); err == nil {
		fieldProperties.FullName = fullName
	}
	if f.TU != nil {
		fieldProperties.AlternateName = f.TU.Decoded()
	}
	if f.TM != nil {
		fieldProperties.MappingName = f.TM.Decoded()
	}
	fieldProperties.Value = f.resolveValueString(false)
	fieldProperties.DefaultValue = f.resolveValueString(true)
	flags := f.Flags()
	fieldProperties.ReadOnly = flags&FieldFlagReadOnly > 0
	fieldProperties.Required = flags&FieldFlagRequired > 0
	fieldProperties.NoExport = flags&FieldFlagNoExport > 0
	fieldProperties.Multiline = flags&FieldFlagMultiline > 0
	fieldProperties.Password = flags&FieldFlagPassword > 0
	fieldProperties.FileSelect = flags&FieldFlagFileSelect > 0
	fieldProperties.DoNotScroll = flags&FieldFlagDoNotScroll > 0
	fieldProperties.Comb = flags&FieldFlagComb > 0
	fieldProperties.RichText = flags&FieldFlagRichText > 0
	fieldProperties.DoNotSpellCheck = flags&FieldFlagDoNotSpellCheck > 0
	fieldProperties.Combo = flags&FieldFlagCombo > 0
	fieldProperties.Edit = flags&FieldFlagEdit > 0
	fieldProperties.Sort = flags&FieldFlagSort > 0
	fieldProperties.MultiSelect = flags&FieldFlagMultiSelect > 0
	fieldProperties.CommitOnSelChange = flags&FieldFlagCommitOnSelChange > 0
	fieldProperties.PushButton = flags&FieldFlagPushbutton > 0
	fieldProperties.Radio = flags&FieldFlagRadio > 0
	fieldProperties.NoToggleToOff = flags&FieldFlagNoToggleToOff > 0
	fieldProperties.RadiosInUnison = flags&FieldFlagRadiosInUnision > 0
	fieldProperties.FontName, fieldProperties.FontSize, fieldProperties.TextColor = parseDefaultAppearance(f.resolveDA())
	fieldProperties.Alignment = f.resolveQ()
	fieldProperties.MaxLength = f.resolveMaxLen()
	if choiceField, ok := f.GetContext().(*PdfFieldChoice); ok && choiceField.Opt != nil {
		fieldProperties.Options = stringsFromArray(choiceField.Opt)
	}
	widgets := fieldWidgets(f)
	if len(widgets) > 0 {
		fieldProperties.BorderColor, fieldProperties.BackgroundColor, fieldProperties.Caption, fieldProperties.Rotation = widgetMKProperties(widgets[0])
		fieldProperties.Widgets = make([]WidgetProperties, 0, len(widgets))
		for _, widget := range widgets {
			fieldProperties.Widgets = append(fieldProperties.Widgets, newWidgetProperties(widget))
		}
	}
	return fieldProperties
}

// NewPdfAppenderWithOpts creates a new Pdf appender from a Pdf reader with
// options. See NewPdfAppender for the seekable-source requirement.
func NewPdfAppenderWithOpts(reader *PdfReader, opts *ReaderOpts, encryptOptions *EncryptOptions) (*PdfAppender, error) {
	if reader.readSeeker == nil {
		return nil, errors.New("appender requires the reader's source to also implement io.ReadSeeker (e.g. *os.File, *bytes.Reader)")
	}
	r, ok := reader.readSeeker.(io.ReaderAt)
	if !ok {
		return nil, errors.New("appender requires a source that implements both io.ReadSeeker and io.ReaderAt")
	}
	appender := &PdfAppender{readSeeker: reader.readSeeker, Reader: reader, parser: reader.parser, traversed: reader.traversed}
	seek, err := appender.readSeeker.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	appender.originalSize = seek
	if _, err = appender.readSeeker.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	appender.roReader, err = newAppenderSnapshotReader(r, seek, inheritReaderOpts(reader, opts), true)
	if err != nil {
		return nil, err
	}
	for _, objectNum := range appender.Reader.GetObjectNums() {
		if appender.greatestObjNum < objectNum {
			appender.greatestObjNum = objectNum
		}
	}
	appender.xrefs = appender.parser.GetXrefTable()
	appender.xrefOffset = appender.parser.GetXrefOffset()
	appender.pages = append(appender.pages, appender.roReader.PageList...)
	appender.hasNewObject = make(map[core.PdfObject]struct{})
	appender.objNumMap = make(map[core.PdfObject]int64)
	appender.skipObjects = make(map[core.PdfObject]struct{})
	appender.acroForm = appender.roReader.AcroForm
	appender.dss = appender.roReader.DSS
	if opts != nil {
		appender.password = opts.Password
	}
	if encryptOptions != nil {
		appender.encryptOptions = encryptOptions
	}
	return appender, nil
}

// SetCatalogLanguage sets the catalog language.
func (w *PdfWriter) SetCatalogLanguage(lang core.PdfObject) error {
	if lang == nil {
		w.catalog.Remove("Lang")
		return nil
	}
	if _, ok := lang.(*core.PdfIndirectObject); ok {
		w.addObject(lang)
	}
	w.catalog.Set("Lang", lang)
	return nil
}

func newOCSPEvidence(body []byte, ok bool) ocspEvidence {
	return ocspEvidence{raw: body, digest: sha256.Sum256(body), online: ok}
}

// PrintPdfObject is a helper function printing object with a given number in a PDF file.
// It can be useful while debugging. Use os.Stdout as w io.Writer to print to console.
// If objNum is -1, then it prints trailer of the PDF file.
func (r *PdfReader) PrintPdfObject(w io.Writer, objNum int) error {
	if objNum == -1 {
		trailer, err := r.GetTrailer()
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "Trailer: %s\n", trailer.String())
		return nil
	}
	indirectObjectByNumber, err := r.GetIndirectObjectByNumber(objNum)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Object %d: %s\n", objNum, indirectObjectByNumber.String())
	if stream, ok := indirectObjectByNumber.(*core.PdfObjectStream); ok {
		data, err := core.DecodeStream(stream)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "Decoded:\n%s", data)
	} else if indirect, ok := indirectObjectByNumber.(*core.PdfIndirectObject); ok {
		fmt.Fprintf(w, "%T\n", indirect.PdfObject)
		fmt.Fprintf(w, "%s\n", indirect.PdfObject.String())
	}
	return nil
}

// AlphaMapFunc represents a alpha mapping function: byte -> byte. Can be used for
// thresholding the alpha channel, i.e. setting all alpha values below threshold to transparent.
type AlphaMapFunc func(b byte) byte

// Normalize swaps (Llx,Urx) if Urx < Llx, and (Lly,Ury) if Ury < Lly.
func (r *PdfRectangle) Normalize() {
	if r.Llx > r.Urx {
		r.Llx, r.Urx = r.Urx, r.Llx
	}
	if r.Lly > r.Ury {
		r.Lly, r.Ury = r.Ury, r.Lly
	}
}

// NewReaderOpts generates a default `ReaderOpts` instance.
func NewReaderOpts() *ReaderOpts { return &ReaderOpts{Password: "", LazyLoad: true} }

// NewPdfAnnotationStrikeOut returns a new text strikeout annotation.
func NewPdfAnnotationStrikeOut() *PdfAnnotationStrikeOut {
	annot := NewPdfAnnotation()
	strikeOut := &PdfAnnotationStrikeOut{}
	strikeOut.PdfAnnotation = annot
	strikeOut.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(strikeOut)
	return strikeOut
}

// NewKDictionary creates a new K dictionary object.
func NewKDictionary() *KDict { return &KDict{children: make([]*KValue, 0), pageNumber: -1} }

// SetPageLabels sets the PageLabels entry in the PDF catalog.
// See section 12.4.2 "Page Labels" (p. 382 PDF32000_2008).
func (w *PdfWriter) SetPageLabels(pageLabels core.PdfObject) error {
	if pageLabels == nil {
		return nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Setting catalog PageLabels...")
	}
	w.catalog.Set("PageLabels", pageLabels)
	return w.addObjects(pageLabels)
}

// PdfColorPatternType1 represents a color shading pattern type 1 (Function-based).
type PdfColorPatternType1 struct {
	Color       PdfColor
	PatternName core.PdfObjectName
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationMovie) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("Movie"))
	dict.SetIfNotNil("T", a.T)
	dict.SetIfNotNil("Movie", a.Movie)
	dict.SetIfNotNil("A", a.A)
	return indirect
}

// IsPush returns true if the button field represents a push button, false otherwise.
func (f *PdfFieldButton) IsPush() bool { return f.GetType() == ButtonTypePush }

// SetNonFullScreenPageMode sets the value of the nonFullScreenPageMode.
func (vp *ViewerPreferences) SetNonFullScreenPageMode(nonFullScreenPageMode NonFullScreenPageMode) {
	vp.nonFullScreenPageMode = nonFullScreenPageMode
}

// AnnotFilterFunc represents a PDF annotation filtering function. If the function
// returns true, the annotation is kept, otherwise it is discarded.
type AnnotFilterFunc func(*PdfAnnotation) bool

// IsColorManaged reports whether the embedded profile is interpreted, so that
// ImageToRGB and ColorToRGB convert through it rather than through the
// alternate colorspace. Only 3-component matrix/TRC RGB profiles are
// interpreted.
func (cs *PdfColorspaceICCBased) IsColorManaged() bool {
	cs.buildTransform()
	return cs.transformReady && cs.N == 3
}

// PdfSignatureReference represents a PDF signature reference dictionary and is used for signing via form signature fields.
// (Section 12.8.1, Table 253 - Entries in a signature reference dictionary p. 469 in PDF32000_2008).
type PdfSignatureReference struct {
	primitive       *core.PdfObjectDictionary
	Type            *core.PdfObjectName
	TransformMethod *core.PdfObjectName
	TransformParams core.PdfObject
	Data            core.PdfObject
	DigestMethod    *core.PdfObjectName
}

func (r *PdfReader) loadForms() (*PdfAcroForm, error) {
	if r.parser.GetCrypter() != nil && !r.parser.IsAuthenticated() {
		return nil, fmt.Errorf("file need to be decrypted first")
	}
	catalog := r.catalog
	acroForm := catalog.Get("AcroForm")
	if acroForm == nil {
		return nil, nil
	}
	acroFormIndirect, ok := core.GetIndirect(acroForm)
	acroForm = core.TraceToDirectObject(acroForm)
	if core.IsNullObject(acroForm) {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Acroform is a null object (empty)\n")
		}
		return nil, nil
	}
	acroFormDict, isAcroFormDict := core.GetDict(acroForm)
	if !isAcroFormDict {
		common.Log.Debug("Invalid AcroForm entry %T", acroForm)
		common.Log.Debug("Does not have forms")
		if r.opts.RelaxedMode {
			common.Log.Debug("RelaxedMode enabled. Ignoring AcroForm loading errors")
			return nil, nil
		}
		return nil, fmt.Errorf("invalid acroform entry %T", acroForm)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Has Acro forms")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Traverse the Acroforms structure")
	}
	if !r.lazy {
		err := r.traverseObjectData(acroFormDict)
		if err != nil {
			common.Log.Debug("ERROR: Unable to traverse AcroForms (%s)", err)
			return nil, err
		}
	}
	form, err := r.newPdfAcroFormFromDict(acroFormIndirect, acroFormDict)
	if err != nil {
		return nil, err
	}
	form.direct = !ok
	return form, nil
}

func fixFontWidthsRange(dict *core.PdfObjectDictionary) {
	widths, ok := core.GetArray(dict.Get("Widths"))
	firstChar, isFirstChar := core.GetIntVal(dict.Get("FirstChar"))
	lastChar, isLastChar := core.GetIntVal(dict.Get("LastChar"))
	if ok && isFirstChar && isLastChar {
		widthsLen := widths.Len()
		if widthsLen != lastChar-firstChar+1 {
			common.Log.Debug("Unexpected Widths length: %v, LastChar: %v", widthsLen, lastChar)
			num := core.PdfObjectInteger(firstChar + widthsLen - 1)
			dict.Set("LastChar", &num)
		}
	}
}

// BaseFont returns the font's "BaseFont" field.
func (f *PdfFont) BaseFont() string { return f.baseFields().baseFont }

func measureXrefTable(writer *PdfWriter, nums []int64, n int64, count int64, ok bool, found bool) int {
	xrefTable, _ := buildXrefTable(writer, nums, map[int64]int64{}, n, count, ok, 0, found)
	return len(xrefTable)
}

// SetLocation sets the `Location` field of the signature.
func (s *PdfSignature) SetLocation(location string) {
	s.Location = core.MakeEncodedString(location, true)
}

// CheckAccessRights checks access rights and permissions for a specified password.  If either user/owner
// password is specified,  full rights are granted, otherwise the access rights are specified by the
// Permissions flag.
//
// The bool flag indicates that the user can access and view the file.
// The AccessPermissions shows what access the user has for editing etc.
// An error is returned if there was a problem performing the authentication.
//
// For AES-256 the password is prepared as described on Decrypt.
func (r *PdfReader) CheckAccessRights(password []byte) (bool, security.Permissions, error) {
	return r.parser.CheckAccessRights(password)
}

func decodedTextString(str *core.PdfObjectString) string {
	data := str.Bytes()
	if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
		return str.Decoded()
	}
	return str.String()
}

func (r *PdfReader) newPdfAnnotationPolyLineFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationPolyLine, error) {
	polyLine := PdfAnnotationPolyLine{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	polyLine.PdfAnnotationMarkup = markup
	polyLine.Vertices = dict.Get("Vertices")
	polyLine.LE = dict.Get("LE")
	polyLine.BS = dict.Get("BS")
	polyLine.IC = dict.Get("IC")
	polyLine.BE = dict.Get("BE")
	polyLine.IT = dict.Get("IT")
	polyLine.Measure = dict.Get("Measure")
	return &polyLine, nil
}

func (l *LTV) getCerts(certs []*x509.Certificate) ([][]byte, error) {
	dataList := make([][]byte, 0, len(certs))
	for _, cert := range certs {
		dataList = append(dataList, cert.Raw)
	}
	return dataList, nil
}

// ToPdfObject converts the PdfPage to a dictionary within an indirect object container.
func (p *PdfPage) ToPdfObject() core.PdfObject {
	indirect := p.primitive
	p.GetPageDict()
	return indirect
}

// GetAscent returns the Ascent of the font `descriptor`.
func (d *PdfFontDescriptor) GetAscent() (float64, error) {
	return core.GetNumberAsFloat(d.Ascent)
}

// PrintClip returns the value of the printClip.
func (vp *ViewerPreferences) PrintClip() PageBoundary { return vp.printClip }

// ColorFromFloats returns a new PdfColorDevice based on the input slice of
// color components. The slice should contain four elements representing the
// cyan, magenta, yellow and key components of the color. The values of the
// elements should be between 0 and 1.
func (cs *PdfColorspaceDeviceCMYK) ColorFromFloats(vals []float64) (PdfColor, error) {
	if len(vals) != 4 {
		return nil, errors.New("range check")
	}
	val := vals[0]
	if val < 0.0 || val > 1.0 {
		common.Log.Debug("color out of CS range: ", val)
		return nil, ErrColorOutOfRange
	}
	f := vals[1]
	if f < 0.0 || f > 1.0 {
		common.Log.Debug("color out of CS range: ", f)
		return nil, ErrColorOutOfRange
	}
	v := vals[2]
	if v < 0.0 || v > 1.0 {
		common.Log.Debug("color out of CS range: ", v)
		return nil, ErrColorOutOfRange
	}
	x := vals[3]
	if x < 0.0 || x > 1.0 {
		common.Log.Debug("color out of CS range: ", x)
		return nil, ErrColorOutOfRange
	}
	deviceCMYKColor := NewPdfColorDeviceCMYK(val, f, v, x)
	return deviceCMYKColor, nil
}

// M returns the value of the magenta component of the color.
func (c *PdfColorDeviceCMYK) M() float64 { return c[1] }

func (r *PdfReader) newPdfActionGotoRFromDict(dict *core.PdfObjectDictionary) (*PdfActionGoToR, error) {
	filespec, err := newPdfFilespecFromPdfObject(dict.Get("F"))
	if err != nil {
		return nil, err
	}
	return &PdfActionGoToR{D: dict.Get("D"), NewWindow: dict.Get("NewWindow"), F: filespec}, nil
}

// Add appends a top level outline item to the outline.
func (o *Outline) Add(item *OutlineItem) { o.Entries = append(o.Entries, item) }

var (
	StructureTypeUnknown       StructureType = ""
	StructureTypeDocument      StructureType = "Document"
	StructureTypePart          StructureType = "Part"
	StructureTypeArticle       StructureType = "Art"
	StructureTypeSection       StructureType = "Sect"
	StructureTypeDivision      StructureType = "Div"
	StructureTypeBlockQuote    StructureType = "BlockQuote"
	StructureTypeCaption       StructureType = "Caption"
	StructureTypeTOC           StructureType = "TOC"
	StructureTypeTOCI          StructureType = "TOCI"
	StructureTypeIndex         StructureType = "Index"
	StructureTypeNonStructural StructureType = "NonStruct"
	StructureTypePrivate       StructureType = "Private"
)

func (r *PdfReader) newPdfFieldFromIndirectObject(indirect *core.PdfIndirectObject, field *PdfField) (*PdfField, error) {
	if field, ok := r.modelManager.GetModelFromPrimitive(indirect).(*PdfField); ok {
		return field, nil
	}
	indirectDict, ok := core.GetDict(indirect)
	if !ok {
		return nil, fmt.Errorf("PdfField indirect object not containing a dictionary")
	}
	field2 := NewPdfField()
	field2.primitive = indirect
	field2.primitive.PdfObject = indirectDict
	if ft, ok := core.GetName(indirectDict.Get("FT")); ok {
		field2.FT = ft
	}
	if field != nil {
		field2.Parent = field
	}
	field2.T, _ = indirectDict.Get("T").(*core.PdfObjectString)
	field2.TU, _ = indirectDict.Get("TU").(*core.PdfObjectString)
	field2.TM, _ = indirectDict.Get("TM").(*core.PdfObjectString)
	field2.Ff, _ = indirectDict.Get("Ff").(*core.PdfObjectInteger)
	field2.V = indirectDict.Get("V")
	field2.DV = indirectDict.Get("DV")
	field2.AA = indirectDict.Get("AA")
	if DA := indirectDict.Get("DA"); DA != nil {
		DA, _ := core.GetString(DA)
		field2.VariableText = &VariableText{DA: DA}
		Q, _ := indirectDict.Get("Q").(*core.PdfObjectInteger)
		DS, _ := indirectDict.Get("DS").(*core.PdfObjectString)
		RV := indirectDict.Get("RV")
		field2.VariableText.Q = Q
		field2.VariableText.DS = DS
		field2.VariableText.RV = RV
	}
	name := field2.FT
	if name == nil && field != nil {
		name = field.FT
	}
	if name != nil {
		switch *name {
		case "Tx":
			fieldText, err := newPdfFieldTextFromDict(indirectDict)
			if err != nil {
				return nil, err
			}
			fieldText.PdfField = field2
			field2.context = fieldText
		case "Ch":
			fieldChoice, err := newPdfFieldChoiceFromDict(indirectDict)
			if err != nil {
				return nil, err
			}
			fieldChoice.PdfField = field2
			field2.context = fieldChoice
		case "Btn":
			fieldButton, err := newPdfFieldButtonFromDict(indirectDict)
			if err != nil {
				return nil, err
			}
			fieldButton.PdfField = field2
			field2.context = fieldButton
		case "Sig":
			signatureField, err := r.newPdfFieldSignatureFromDict(indirectDict)
			if err != nil {
				return nil, err
			}
			signatureField.PdfField = field2
			field2.context = signatureField
		default:
			common.Log.Debug("ERROR: Unsupported field type %s", *field2.FT)
			return nil, errors.New("unsupported field type")
		}
	}
	if subtype, ok := core.GetName(indirectDict.Get("Subtype")); ok {
		if *subtype == "Widget" {
			annot, err := r.newPdfAnnotationFromIndirectObject(indirect)
			if err != nil {
				return nil, err
			}
			widget, ok := annot.GetContext().(*PdfAnnotationWidget)
			if !ok {
				return nil, errors.New("invalid widget annotation")
			}
			widget.field = field2
			widget.Parent = field2.primitive
			field2.Annotations = append(field2.Annotations, widget)
			return field2, nil
		}
	}
	found := true
	if kids, isKids := core.GetArray(indirectDict.Get("Kids")); isKids {
		indirects := make([]*core.PdfIndirectObject, 0, kids.Len())
		for _, obj := range kids.Elements() {
			indirect, isIndirect := core.GetIndirect(obj)
			if !isIndirect {
				stream, ok := core.GetStream(obj)
				if ok && stream.PdfObjectDictionary != nil {
					s, ok := core.GetNameVal(stream.Get("Type"))
					if ok && s == "Metadata" {
						common.Log.Debug("ERROR: form field Kids array contains invalid Metadata stream. Skipping.")
						continue
					}
				}
				return nil, errors.New("not an indirect object (form field)")
			}
			indirectDict, isIndirectDict := core.GetDict(indirect)
			if !isIndirectDict {
				return nil, ErrTypeCheck
			}
			if found {
				found = !hasFieldKeys(indirectDict)
			}
			indirects = append(indirects, indirect)
		}
		for _, indirect := range indirects {
			if found {
				annot, err := r.newPdfAnnotationFromIndirectObject(indirect)
				if err != nil {
					common.Log.Debug("Error loading widget annotation for field: %v", err)
					return nil, err
				}
				widget, ok := annot.context.(*PdfAnnotationWidget)
				if !ok {
					return nil, ErrTypeCheck
				}
				widget.field = field2
				field2.Annotations = append(field2.Annotations, widget)
			} else {
				field3, err := r.newPdfFieldFromIndirectObject(indirect, field2)
				if err != nil {
					common.Log.Debug("Error loading child field: %v", err)
					return nil, err
				}
				field2.Kids = append(field2.Kids, field3)
			}
		}
	}
	return field2, nil
}

// NewPdfFilespec returns an initialized generic PDF filespec model.
func NewPdfFilespec() *PdfFilespec {
	filespec := &PdfFilespec{}
	filespec.primitive = core.MakeIndirectObject(core.MakeDict())
	return filespec
}

func (l *LTV) validateSig(sig *PdfSignature) error {
	if sig == nil || sig.Contents == nil || len(sig.Contents.Bytes()) == 0 {
		return fmt.Errorf("invalid signature field: %v", sig)
	}
	return nil
}

func (r *PdfReader) loadStructure() error {
	if r.parser.GetCrypter() != nil && !r.parser.IsAuthenticated() {
		return fmt.Errorf("file need to be decrypted first")
	}
	trailer := r.parser.GetTrailer()
	if trailer == nil {
		return fmt.Errorf("missing trailer")
	}
	ref, ok := trailer.Get("Root").(*core.PdfObjectReference)
	if !ok {
		return fmt.Errorf("invalid Root (trailer: %s)", trailer)
	}
	lookupByReference, err := r.parser.LookupByReference(*ref)
	if err != nil {
		common.Log.Debug("ERROR: Failed to read root element catalog: %s", err)
		return err
	}
	indirect, ok := lookupByReference.(*core.PdfIndirectObject)
	if !ok {
		common.Log.Debug("ERROR: Missing catalog: (root %q) (trailer %s)", lookupByReference, trailer)
		return errors.New("missing catalog")
	}
	dict, ok := (*indirect).PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Invalid catalog (%s)", indirect.PdfObject)
		return errors.New("invalid catalog")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Catalog: %s", dict)
	}
	ref2, ok := dict.Get("Pages").(*core.PdfObjectReference)
	if !ok {
		return errors.New("pages in catalog should be a reference")
	}
	obj, err := r.parser.LookupByReference(*ref2)
	if err != nil {
		common.Log.Debug("ERROR: Failed to read pages")
		return err
	}
	indirect2, ok := obj.(*core.PdfIndirectObject)
	if !ok {
		common.Log.Debug("ERROR: Pages object invalid")
		common.Log.Debug("op: %p", indirect2)
		return errors.New("pages object invalid")
	}
	dict2, ok := indirect2.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Pages object invalid (%s)", indirect2)
		return errors.New("pages object invalid")
	}
	count, ok := core.GetInt(dict2.Get("Count"))
	if !ok {
		common.Log.Debug("ERROR: Pages count object invalid")
		return errors.New("pages count invalid")
	}
	if _, ok = core.GetName(dict2.Get("Type")); !ok {
		common.Log.Debug("Pages dict Type field not set. Setting Type to Pages.")
		dict2.Set("Type", core.MakeName("Pages"))
	}
	if rotate, ok := core.GetInt(dict2.Get("Rotate")); ok {
		n := int64(*rotate)
		r.Rotate = &n
	}
	r.root = ref
	r.catalog = dict
	r.pagesDict = dict2
	r.pagesObj = indirect2
	r.pageCount = int(*count)
	r.pageObjs = []*core.PdfIndirectObject{}
	set := map[core.PdfObject]struct{}{}
	err = r.buildPageList(indirect2, nil, set)
	if err != nil {
		return err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("---")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("TOC")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Pages")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("%d: %s", len(r.pageObjs), r.pageObjs)
	}
	if !r.skipOutlines {
		r.outlineTree, err = r.loadOutlines()
		if err != nil {
			common.Log.Debug("ERROR: Failed to build outline tree (%s)", err)
			return err
		}
	}
	r.AcroForm, err = r.loadForms()
	if err != nil {
		return err
	}
	r.DSS, err = r.loadDSS()
	if err != nil {
		return err
	}
	if !r.skipPerms {
		r.perms, err = r.loadPerms()
		if err != nil {
			return err
		}
	}
	return nil
}

// FieldType returns the resolved field type, walking up to the parent when
// /FT is inherited.
func (f *PdfField) FieldType() FieldType {
	if f == nil {
		return FieldTypeUnknown
	}
	var s string
	_, _ = f.inherit(func(field *PdfField) bool {
		if field.FT == nil {
			return false
		}
		s = string(*field.FT)
		return true
	})
	switch s {
	case "Tx":
		return FieldTypeText
	case "Btn":
		return FieldTypeButton
	case "Ch":
		return FieldTypeChoice
	case "Sig":
		return FieldTypeSignature
	}
	return FieldTypeUnknown
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

// String returns the name of the colorspace (DeviceN).
func (cs *PdfColorspaceDeviceN) String() string { return "DeviceN" }

type pdfCIDFontType0 struct {
	fontCommon
	primitive *core.PdfIndirectObject
	encoder   textencoding.TextEncoder

	// Table 117 – Entries in a CIDFont dictionary (page 269)
	// (Required) Dictionary that defines the character collection of the CIDFont.
	// See Table 116.
	CIDSystemInfo *core.PdfObjectDictionary

	// Glyph metrics fields (optional).
	DW  core.PdfObject
	W   core.PdfObject
	DW2 core.PdfObject
	W2  core.PdfObject

	// CIDs to glyph indices mapping (optional).
	CIDToGIDMap  core.PdfObject
	charWidths   map[textencoding.CharCode]float64
	defaultWidth float64
	runeToGID    map[rune]int
	otfType      *fonts.OtfType
}

// K returns the value of the key component of the color.
func (c *PdfColorDeviceCMYK) K() float64 { return c[3] }

func fillSignatureCoverage(parser *core.PdfParser, nums []int64, contents *core.PdfObjectString, result *SignatureValidationResult) {
	result.byteRangeChecked = true
	n := nums[len(nums)-2] + nums[len(nums)-1]
	result.IsCoveringEntireFile = n == parser.Size()
	result.ContentsGapOnly = isContentsGapOnly(parser, nums, contents)
	revisionEndOffsets, err := parser.RevisionEndOffsets()
	if err != nil || len(revisionEndOffsets) == 0 {
		result.IsCoveringEntireRevision = result.IsCoveringEntireFile
		return
	}
	search := sort.Search(len(revisionEndOffsets), func(count int) bool { return revisionEndOffsets[count] >= n })
	if search == len(revisionEndOffsets) {
		result.IsCoveringEntireRevision = result.IsCoveringEntireFile || isWhitespaceGap(parser, revisionEndOffsets[len(revisionEndOffsets)-1], n)
		return
	}
	if n == revisionEndOffsets[search] {
		result.IsCoveringEntireRevision = true
		result.RevisionsAfterSignature = len(revisionEndOffsets) - 1 - search
		return
	}
	if isWhitespaceGap(parser, n, revisionEndOffsets[search]) {
		result.IsCoveringEntireRevision = true
		result.RevisionsAfterSignature = len(revisionEndOffsets) - 1 - search
		return
	}
	if search > 0 && isWhitespaceGap(parser, revisionEndOffsets[search-1], n) {
		result.IsCoveringEntireRevision = true
		result.RevisionsAfterSignature = len(revisionEndOffsets) - search
	}
}

// AddRefChild adds a child reference object.
func (k *KDict) AddRefChild(kChild core.PdfObject) {
	k.children = append(k.children, &KValue{refObject: kChild})
}

// Initialize initializes the PdfSignature.
func (s *PdfSignature) Initialize() error {
	if s.Handler == nil {
		return errors.New("signature handler cannot be nil")
	}
	return s.Handler.InitSignature(s)
}

// GetAttachedFiles retrieves all the attached files info and content.
func (r *PdfReader) GetAttachedFiles() ([]*EmbeddedFile, error) {
	files := []*EmbeddedFile{}
	nameDictionary, err := r.GetNameDictionary()
	if err != nil {
		return nil, err
	}
	if nameDictionary == nil {
		return files, nil
	}
	names := newNamesFromPdfObject(nameDictionary)
	if names.EmbeddedFiles == nil {
		return nil, nil
	}
	obj := names.EmbeddedFiles.Get("Names")
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, errors.New("Invalid Names array")
	}
	for i := 1; i < len(arr.Elements()); i += 2 {
		if i%2 != 0 {
			obj := arr.Get(i)
			filespec, err := NewPdfFilespecFromObj(obj)
			if err != nil {
				return nil, err
			}
			embeddedFile, err := NewEmbeddedFileFromObject(filespec.EF)
			if err != nil {
				return nil, err
			}
			str, ok := filespec.F.(*core.PdfObjectString)
			if ok {
				embeddedFile.Name = str.Str()
			}
			embeddedFile.Description = string(filespec.Desc.Write())
			embeddedFile.Relationship = RelationshipUnspecified
			if filespec.AFRelationship != nil {
				switch string(filespec.AFRelationship.Write()) {
				case "Source":
					embeddedFile.Relationship = RelationshipSource
				case "Data":
					embeddedFile.Relationship = RelationshipData
				case "Alternative":
					embeddedFile.Relationship = RelationshipAlternative
				case "Supplement":
					embeddedFile.Relationship = RelationshipSupplement
				default:
					embeddedFile.Relationship = RelationshipUnspecified
				}
			}
			files = append(files, embeddedFile)
		}
	}
	return files, nil
}

// NewReaderForText makes a new PdfReader for an input PDF content string. For use in testing.
func NewReaderForText(txt string) *PdfReader {
	return &PdfReader{traversed: map[core.PdfObject]struct{}{}, modelManager: newModelManager(), parser: core.NewParserFromString(txt)}
}

// NewPdfReaderLazyAt is the explicit-size form of NewPdfReaderLazy.
func NewPdfReaderLazyAt(r io.ReaderAt, size int64) (*PdfReader, error) {
	const trackNameNewPdfReaderLazy = "model:NewPdfReaderLazy"
	return newPdfReaderTracked(r, size, &ReaderOpts{LazyLoad: true}, false, false, trackNameNewPdfReaderLazy)
}

// FieldValueProvider provides field values from a data source such as FDF, JSON or any other.
type FieldValueProvider interface {
	FieldValues() (map[string]core.PdfObject, error)
}

// EnableAll LTV enables all signatures in the PDF document.
// The signing certificate chain is extracted from each signature dictionary.
// Optionally, additional certificates can be specified through the
// `extraCerts` parameter. The LTV client attempts to build the certificate
// chain up to a trusted root by downloading any missing certificates.
func (l *LTV) EnableAll(extraCerts []*x509.Certificate) error {
	acroForm := l.appender.roReader.AcroForm
	for _, allField := range acroForm.AllFields() {
		signatureField, _ := allField.GetContext().(*PdfFieldSignature)
		if signatureField == nil {
			continue
		}
		sig := signatureField.V
		if err := l.validateSig(sig); err != nil {
			common.Log.Debug("WARN: Skipping invalid signature field: %v", err)
		}
		if err := l.Enable(sig, extraCerts); err != nil {
			return err
		}
	}
	return nil
}

// IsDisplayDocTitle returns the value of the displayDocTitle flag.
func (vp *ViewerPreferences) IsDisplayDocTitle() bool {
	if vp.displayDocTitle == nil {
		return false
	}
	return *vp.displayDocTitle
}

// Y returns the value of the yellow component of the color.
func (c *PdfColorDeviceCMYK) Y() float64 { return c[2] }

// ToPdfObject implements interface PdfModel.
func (a *PdfActionMovie) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeMovie)))
	dict.SetIfNotNil("Annotation", a.Annotation)
	dict.SetIfNotNil("T", a.T)
	dict.SetIfNotNil("Operation", a.Operation)
	return indirect
}

// GetContainingPdfObject implements model.PdfModel interface.
func (o *PdfOutputIntent) GetContainingPdfObject() core.PdfObject { return o.primitive }

func (r *PdfReader) newPdfAnnotation3DFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotation3D, error) {
	annot3D := PdfAnnotation3D{}
	annot3D.T3DD = dict.Get("3DD")
	annot3D.T3DV = dict.Get("3DV")
	annot3D.T3DA = dict.Get("3DA")
	annot3D.T3DI = dict.Get("3DI")
	annot3D.T3DB = dict.Get("3DB")
	return &annot3D, nil
}

// PdfInfoTrapped specifies pdf trapped information.
type PdfInfoTrapped string

// L returns the value of the L component of the color.
func (c *PdfColorLab) L() float64 { return c[0] }

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationRichMedia) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("Subtype", core.MakeName("RichMedia"))
	dict.SetIfNotNil("RichMediaSettings", a.RichMediaSettings)
	dict.SetIfNotNil("RichMediaContent", a.RichMediaContent)
	return indirect
}

// WriteToFile writes the output PDF to file.
func (w *PdfWriter) WriteToFile(outputFilePath string) error {
	file, err := os.Create(outputFilePath)
	if err != nil {
		return err
	}
	defer file.Close()
	return w.Write(file)
}

// ToPdfObject returns the PDF representation of the pattern.
func (p *PdfPattern) ToPdfObject() core.PdfObject {
	dict := p.getDict()
	dict.Set("Type", core.MakeName("Pattern"))
	dict.Set("PatternType", core.MakeInteger(p.PatternType))
	return p.primitive
}

func (p *PdfPage) generateImage(text string, fontSize float64, fontPath string, fontColor color.Color) ([]byte, error) {
	rgba, n, count, _ := fontColor.RGBA()
	rgba2 := color.RGBA{uint8(rgba >> 8), uint8(n >> 8), uint8(count >> 8), 255}
	rgba3 := color.RGBA{0xff, 0xff, 0xff, 0x00}
	font, err := p.loadFont(fontPath)
	if err != nil {
		return nil, err
	}
	replaceAll := strings.ReplaceAll(text, "\t", "    ")
	split := strings.Split(replaceAll, "\n")
	f := 1.0
	for _, s := range split {
		if float64(len(s))*fontSize > f {
			f = float64(len(s)) * fontSize
		}
	}
	if f > p.MediaBox.Width() {
		f = p.MediaBox.Width()
	}
	uniform := image.NewUniform(rgba2)
	uniform2 := image.NewUniform(rgba3)
	rgba4 := image.NewRGBA(image.Rect(0, 0, int(f), int(fontSize*1.5*float64(len(split)))))
	draw.Draw(rgba4, rgba4.Bounds(), uniform2, image.Pt(0, 0), draw.Src)
	ctx := freetype.NewContext()
	ctx.SetDPI(72)
	ctx.SetFont(font)
	ctx.SetFontSize(fontSize)
	ctx.SetHinting(imagefont.HintingNone)
	ctx.SetClip(rgba4.Bounds())
	ctx.SetDst(rgba4)
	ctx.SetSrc(uniform)
	num := 50
	size := 10 + int(ctx.PointToFixed(fontSize)>>6)
	point26_6 := freetype.Pt(num, size)
	for _, s := range split {
		_, err = ctx.DrawString(strings.ReplaceAll(s, "\r", ""), point26_6)
		if err != nil {
			return nil, err
		}
		point26_6.Y += ctx.PointToFixed(fontSize * 1.5)
	}
	buf := new(bytes.Buffer)
	if err := png.Encode(buf, rgba4); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

const linearizationDictObjNum2 = 1

func newPdfFieldTextFromDict(dict *core.PdfObjectDictionary) (*PdfFieldText, error) {
	textField := &PdfFieldText{}
	textField.DA, _ = core.GetString(dict.Get("DA"))
	textField.Q, _ = core.GetInt(dict.Get("Q"))
	textField.DS, _ = core.GetString(dict.Get("DS"))
	textField.RV = dict.Get("RV")
	textField.MaxLen, _ = core.GetInt(dict.Get("MaxLen"))
	return textField, nil
}

// String returns a string representation of what flags are set.
func (f FieldFlag) String() string {
	s := ""
	if f == FieldFlagClear {
		s = "Clear"
		return s
	}
	if f&FieldFlagReadOnly > 0 {
		s += "|ReadOnly"
	}
	if f&FieldFlagRequired > 0 {
		s += "|Required"
	}
	if f&FieldFlagNoExport > 0 {
		s += "|NoExport"
	}
	if f&FieldFlagNoToggleToOff > 0 {
		s += "|NoToggleToOff"
	}
	if f&FieldFlagRadio > 0 {
		s += "|Radio"
	}
	if f&FieldFlagPushbutton > 0 {
		s += "|Pushbutton"
	}
	if f&FieldFlagRadiosInUnision > 0 {
		s += "|RadiosInUnision"
	}
	if f&FieldFlagMultiline > 0 {
		s += "|Multiline"
	}
	if f&FieldFlagPassword > 0 {
		s += "|Password"
	}
	if f&FieldFlagFileSelect > 0 {
		s += "|FileSelect"
	}
	if f&FieldFlagDoNotScroll > 0 {
		s += "|DoNotScroll"
	}
	if f&FieldFlagComb > 0 {
		s += "|Comb"
	}
	if f&FieldFlagRichText > 0 {
		s += "|RichText"
	}
	if f&FieldFlagDoNotSpellCheck > 0 {
		s += "|DoNotSpellCheck"
	}
	if f&FieldFlagCombo > 0 {
		s += "|Combo"
	}
	if f&FieldFlagEdit > 0 {
		s += "|Edit"
	}
	if f&FieldFlagSort > 0 {
		s += "|Sort"
	}
	if f&FieldFlagMultiSelect > 0 {
		s += "|MultiSelect"
	}
	if f&FieldFlagCommitOnSelChange > 0 {
		s += "|CommitOnSelChange"
	}
	return strings.Trim(s, "|")
}

func (w *PdfWriter) checkPendingObjects() {
	for obj, missingParent := range w.missingParents {
		if !w.hasObject(obj) {
			common.Log.Debug("WARN Pending object %+v %T (%p) never added for writing", obj, obj, obj)
			for _, dict := range missingParent {
				for _, name := range dict.Keys() {
					obj2 := dict.Get(name)
					if obj2 == obj {
						common.Log.Debug("Pending object found! and replaced with null")
						dict.Set(name, core.MakeNull())
						break
					}
				}
			}
		}
	}
}

// CopyFrom copies all the fields from another PdfAnnotation
func (a *PdfAnnotation) CopyFrom(another *PdfAnnotation) {
	if another == nil {
		return
	}
	a.Rect = another.Rect
	a.Contents = another.Contents
	a.P = another.P
	a.NM = another.NM
	a.M = another.M
	a.F = another.F
	a.AP = another.AP
	a.AS = another.AS
	a.Border = another.Border
	a.C = another.C
	a.StructParent = another.StructParent
	a.OC = another.OC
}

// OutlineItem represents a PDF outline item dictionary (Table 153 - pp. 376 - 377).
type OutlineItem struct {
	Title   string         `json:"title"`
	Dest    OutlineDest    `json:"dest"`
	Entries []*OutlineItem `json:"entries,omitempty"`
}

func clampToDomain(xVec, domain []float64) []float64 {
	if len(domain) != 2*len(xVec) {
		return xVec
	}
	floats := make([]float64, len(xVec))
	for i := range xVec {
		floats[i] = math.Min(math.Max(xVec[i], domain[2*i]), domain[2*i+1])
	}
	return floats
}

// Components returns the component values of the underlying color, or nil if
// the pattern has no underlying color.
func (p *PdfColorPatternType2) Components() []float64 {
	if p.Color == nil {
		return nil
	}
	return p.Color.Components()
}

// GetRevision returns the specific version of the PdfReader for the current Pdf document
func (r *PdfReader) GetRevision(revisionNumber int) (*PdfReader, error) {
	n := r.parser.GetRevisionNumber()
	if revisionNumber < 0 || revisionNumber > n {
		return nil, errors.New("wrong revision number")
	}
	if revisionNumber == n {
		return r, nil
	}
	if r.revisions[revisionNumber] != nil {
		return r.revisions[revisionNumber], nil
	}
	reader := r
	for i := n - 1; i >= revisionNumber; i-- {
		previousRevision, err := reader.GetPreviousRevision()
		if err != nil {
			return nil, err
		}
		r.revisions[i] = previousRevision
		reader = previousRevision
	}
	return reader, nil
}

func (x *XObjectImage) getParamsDict() *core.PdfObjectDictionary {
	dict := core.MakeDict()
	dict.Set("Width", core.MakeInteger(*x.Width))
	dict.Set("Height", core.MakeInteger(*x.Height))
	dict.Set("ColorComponents", core.MakeInteger(int64(x.ColorSpace.GetNumComponents())))
	dict.Set("BitsPerComponent", core.MakeInteger(*x.BitsPerComponent))
	return dict
}

// NewPdfAnnotationTrapNet returns a new trapnet annotation.
func NewPdfAnnotationTrapNet() *PdfAnnotationTrapNet {
	annot := NewPdfAnnotation()
	trapNet := &PdfAnnotationTrapNet{}
	trapNet.PdfAnnotation = annot
	annot.SetContext(trapNet)
	return trapNet
}

// PdfActionGoTo3DView represents a GoTo3DView action.
type PdfActionGoTo3DView struct {
	*PdfAction
	TA core.PdfObject
	V  core.PdfObject
}

// NewPdfAnnotationWidget returns an initialized annotation widget.
func NewPdfAnnotationWidget() *PdfAnnotationWidget {
	annot := NewPdfAnnotation()
	widget := &PdfAnnotationWidget{}
	widget.PdfAnnotation = annot
	annot.SetContext(widget)
	return widget
}

// PdfAnnotationCircle represents Circle annotations.
// (Section 12.5.6.8).
type PdfAnnotationCircle struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	BS core.PdfObject
	IC core.PdfObject
	BE core.PdfObject
	RD core.PdfObject
}

// PdfColorspaceSpecialIndexed is an indexed color space is a lookup table, where the input element
// is an index to the lookup table and the output is a color defined in the lookup table in the Base
// colorspace.
// [/Indexed base hival lookup]
type PdfColorspaceSpecialIndexed struct {
	Base        PdfColorspace
	HiVal       int
	Lookup      core.PdfObject
	lookupTable []byte
	container   *core.PdfIndirectObject
}

func decodedString(obj core.PdfObject) string {
	if str, ok := core.GetString(obj); ok {
		return str.Decoded()
	}
	if name, ok := core.GetName(obj); ok {
		return string(*name)
	}
	if arr, ok := core.GetArray(obj); ok {
		strs := make([]string, 0, arr.Len())
		for _, obj := range arr.Elements() {
			strs = append(strs, decodedString(obj))
		}
		return strings.Join(strs, ", ")
	}
	return ""
}

// Write writes out the PDF.
func (w *PdfWriter) Write(writer io.Writer) (err error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Write()")
	}
	if file, ok := writer.(*os.File); ok {
		w.SetFileName(file.Name())
	}
	defer func() {
		if v := recover(); v != nil {
			w.finishLazyStreams(fmt.Errorf("write panicked: %v", v))
			panic(v)
		}
		w.finishLazyStreams(err)
	}()
	if w.written && (w.encrypted || (w.copied && w.writeFailed)) {
		return errors.New("cannot write again with the same PdfWriter after encrypting or after a failed write that copied the objects; use a new PdfWriter")
	}
	w.written = true
	if w.linearized {
		return w.writeLinearized(writer)
	}
	err = w.checkLicense()
	if err != nil {
		return err
	}
	if err = w.writeOutlines(); err != nil {
		return err
	}
	if err = w.writeAcroFormFields(); err != nil {
		return err
	}
	if err = w.writeNamesDictionary(); err != nil {
		return err
	}
	w.checkPendingObjects()
	if err = w.writeOutputIntents(); err != nil {
		return err
	}
	w.setCatalogVersion()
	if w.canSkipCopy() {
		if !w.skipObjectCopy {
			defer w.snapshotObjectNumbers()()
		}
	} else {
		w.copyObjects()
	}
	if err = w.optimize(); err != nil {
		return err
	}
	if err = w.optimizeDocument(); err != nil {
		return err
	}
	var digest hash.Hash
	if w.useHashBasedID {
		digest = md5.New()
		writer = io.MultiWriter(digest, writer)
	}
	w.setWriter(writer)
	ok := w.checkCrossReferenceStream()
	mapObjectStreams, ok := w.mapObjectStreams(ok)
	w.adjustXRefAffectedVersion(ok)
	w.writeDocumentVersion()
	w.updateObjectNumbers()
	w.writeObjects()
	if err = w.writeObjectsInStreams(mapObjectStreams); err != nil {
		return err
	}
	writePos := w.writePos
	var n int
	for key := range w.crossReferences {
		if key > n {
			n = key
		}
	}
	if w.useHashBasedID {
		if err = w.setHashIDs(digest); err != nil {
			return err
		}
	}
	if ok {
		if err = w.writeXRefStreams(n, writePos); err != nil {
			return err
		}
	} else {
		w.writeTrailer(n)
	}
	w.makeOffSetReference(writePos)
	if err = w.flushWriter(); err != nil {
		return err
	}
	return nil
}

// StructureTagInfo represents a tagged structure in a PDF document that used by Drawable components.
type StructureTagInfo struct {
	// Marked Content ID, if applicable.
	Mcid int64

	// Structure type, a name object identifying the nature of the object and its role within the document.
	StructureType StructureType

	// AnnotObjs is used to reference annotation objects in structure elements
	AnnotObjs []core.PdfObject

	// Whether the component that has StructureTagInfo is tagged for accessibility.
	ApplyTag bool

	// Parent K object for the component structure that also act as a parent node.
	ParentKObj *KDict

	// The page number where the structure element for this component is located.
	StructPageNumber *int64

	// K object for the component.
	ComponentKObj *KDict
}

// GetContext returns a reference to the subshading entry as represented by PdfShadingType1-7.
func (s *PdfShading) GetContext() PdfModel { return s.context }

// SetICCProfile sets the raw ICC colour profile describing the image samples,
// as embedded by NewXObjectImageFromImage. It must match the current samples;
// nil clears the profile.
func (img *Image) SetICCProfile(profile []byte) { img.iccProfile = profile }

// NewPdfDate returns a new PdfDate object from a PDF date string (see 7.9.4 Dates).
// format: "D: YYYYMMDDHHmmSSOHH'mm"
func NewPdfDate(dateStr string) (PdfDate, error) {
	pdfTime, err := timeutils.ParsePdfTime(dateStr)
	if err != nil {
		return PdfDate{}, err
	}
	return NewPdfDateFromTime(pdfTime)
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

// NewPdfTransformParamsDocMDP create a PdfTransformParamsDocMDP with the specific permissions.
func NewPdfTransformParamsDocMDP(permission mdp.DocMDPPermission) *PdfTransformParamsDocMDP {
	return &PdfTransformParamsDocMDP{Type: core.MakeName("TransformParams"), P: core.MakeInteger(int64(permission)), V: core.MakeName("1.2")}
}

// NewPdfAnnotationHighlight returns a new text highlight annotation.
func NewPdfAnnotationHighlight() *PdfAnnotationHighlight {
	annot := NewPdfAnnotation()
	highlight := &PdfAnnotationHighlight{}
	highlight.PdfAnnotation = annot
	highlight.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(highlight)
	return highlight
}

// GetContainingPdfObject returns the container of the PdfAcroForm (indirect object).
func (a *PdfAcroForm) GetContainingPdfObject() core.PdfObject { return a.primitive }

// PdfAnnotationFileAttachment represents FileAttachment annotations.
// (Section 12.5.6.15).
type PdfAnnotationFileAttachment struct {
	*PdfAnnotation
	*PdfAnnotationMarkup
	FS   core.PdfObject
	Name core.PdfObject
}

// EncryptionAlgorithm is used in EncryptOptions to change the default algorithm used to encrypt the document.
type EncryptionAlgorithm int

// NewPdfAnnotationProjection returns a new projection annotation.
func NewPdfAnnotationProjection() *PdfAnnotationProjection {
	annot := NewPdfAnnotation()
	projection := &PdfAnnotationProjection{}
	projection.PdfAnnotation = annot
	projection.PdfAnnotationMarkup = &PdfAnnotationMarkup{}
	annot.SetContext(projection)
	return projection
}

// PdfColorspaceCalGray represents CalGray color space.
type PdfColorspaceCalGray struct {
	WhitePoint []float64
	BlackPoint []float64
	Gamma      float64
	container  *core.PdfIndirectObject
}

// GenerateKDict generates a K dictionary object from the StructureTagInfo.
func (t *StructureTagInfo) GenerateKDict() *KDict {
	kdict := &KDict{S: core.MakeName(string(t.StructureType))}
	if t.StructPageNumber != nil {
		kdict.SetPageNumber(*t.StructPageNumber)
	}
	if len(t.AnnotObjs) == 0 {
		kdict.K = core.MakeInteger(t.Mcid)
		return kdict
	}
	array := core.MakeArray()
	array.Append(core.MakeInteger(t.Mcid))
	for _, annotObj := range t.AnnotObjs {
		dict := core.MakeDict()
		dict.Set("Type", core.MakeName("OBJR"))
		dict.Set("Obj", annotObj)
		array.Append(dict)
	}
	kdict.K = array
	return kdict
}

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

// PdfPageResourcesColorspaces contains the colorspace in the PdfPageResources.
// Needs to have matching name and colorspace map entry. The Names define the order.
type PdfPageResourcesColorspaces struct {
	Names       []string
	Colorspaces map[string]PdfColorspace
	container   *core.PdfIndirectObject
}

func (t *trustEvaluator) parseOCSP(key ocspKey, raw []byte, cert, cert2 *x509.Certificate) *ocsp.Response {
	if ocspCache, ok := t.ocspCache[key]; ok {
		return ocspCache
	}
	responseForCert, err := ocsp.ParseResponseForCert(raw, cert, cert2)
	if err != nil {
		responseForCert = nil
	}
	if t.ocspCache == nil {
		t.ocspCache = map[ocspKey]*ocsp.Response{}
	}
	t.ocspCache[key] = responseForCert
	return responseForCert
}

// GetOptimizer returns current PDF optimizer.
func (w *PdfWriter) GetOptimizer() Optimizer { return w.optimizer }

// GetColorspaces loads PdfPageResourcesColorspaces from `r.ColorSpace` and returns an error if there
// is a problem loading. Once loaded, the same object is returned on multiple calls.
func (r *PdfPageResources) GetColorspaces() (*PdfPageResourcesColorspaces, error) {
	if r.colorspaces != nil {
		return r.colorspaces, nil
	}
	if r.ColorSpace == nil {
		return nil, nil
	}
	pageResourcesColorspaces, err := newPdfPageResourcesColorspacesFromPdfObject(r.ColorSpace)
	if err != nil {
		return nil, err
	}
	r.colorspaces = pageResourcesColorspaces
	return r.colorspaces, nil
}

// Encoder returns the font's text encoder.
func (f pdfFontType3) Encoder() textencoding.TextEncoder { return f.encoder }

func (r *PdfReader) newPdfAnnotationSoundFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationSound, error) {
	sound := PdfAnnotationSound{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	sound.PdfAnnotationMarkup = markup
	sound.Name = dict.Get("Name")
	sound.Sound = dict.Get("Sound")
	return &sound, nil
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionSetOCGState) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeSetOCGState)))
	dict.SetIfNotNil("State", a.State)
	dict.SetIfNotNil("PreserveRB", a.PreserveRB)
	return indirect
}

// Inspect inspects the object types, subtypes and content in the PDF file returning a map of
// object type to number of instances of each.
func (r *PdfReader) Inspect() (map[string]int, error) { return r.parser.Inspect() }

// PdfColorCalRGB represents a color in the Colorimetric CIE RGB colorspace.
// A, B, C components
// Each component is defined in the range 0.0 - 1.0 where 1.0 is the primary intensity.
type PdfColorCalRGB [3]float64

// NewKDictFromPdfObject creates a new K dictionary object from a PDF object.
func NewKDictFromPdfObject(obj core.PdfObject) (*KDict, error) {
	obj2 := core.ResolveReference(obj)
	if obj2 == nil {
		return nil, fmt.Errorf("K object is nil")
	}
	dict, ok := core.GetDict(obj2)
	if !ok {
		return nil, fmt.Errorf("K object is not a dictionary")
	}
	kdict := &KDict{}
	if s := dict.Get("S"); s != nil {
		kdict.S = s
	}
	if p := dict.Get("P"); p != nil {
		kdict.P = p
	}
	if id := dict.Get("ID"); id != nil {
		if idStr, ok := core.GetString(id); ok {
			kdict.ID = idStr
		}
	}
	if pg := dict.Get("Pg"); pg != nil {
		kdict.Pg = pg
	}
	if k := dict.Get("K"); k != nil {
		kdict.K = k
		switch v := k.(type) {
		case *core.PdfObjectArray:
			if kArr, ok := core.GetArray(k); ok {
				for _, obj := range kArr.Elements() {
					switch v := obj.(type) {
					case *core.PdfIndirectObject:
						kDict, err := NewKDictFromPdfObject(v)
						if err != nil {
							common.Log.Debug("Error creating K child: %v", err)
							continue
						}
						kdict.children = append(kdict.children, &KValue{kDict: kDict})
					case *core.PdfObjectInteger:
						if n, ok := core.GetIntVal(obj); ok {
							kdict.children = append(kdict.children, &KValue{mcid: &n})
						}
					case *core.PdfObjectDictionary:
						kdict.children = append(kdict.children, &KValue{refObject: obj})
					}
				}
			}
		case *core.PdfIndirectObject:
			kDict, err := NewKDictFromPdfObject(v)
			if err != nil {
				common.Log.Debug("Error creating K child: %v", err)
			}
			if kDict != nil {
				kdict.children = append(kdict.children, &KValue{kDict: kDict})
			}
		case *core.PdfObjectDictionary:
			kdict.children = append(kdict.children, &KValue{refObject: v})
		case *core.PdfObjectInteger:
			if n, ok := core.GetIntVal(k); ok {
				kdict.children = append(kdict.children, &KValue{mcid: &n})
			}
		}
	}
	if a := dict.Get("A"); a != nil {
		kdict.A = a
	}
	if c := dict.Get("C"); c != nil {
		kdict.C = c
	}
	if r := dict.Get("R"); r != nil {
		if rInt, ok := core.GetInt(r); ok {
			kdict.R = rInt
		}
	}
	if t := dict.Get("T"); t != nil {
		if str, ok := core.GetString(t); ok {
			kdict.T = str
		}
	}
	if lang := dict.Get("Lang"); lang != nil {
		if langStr, ok := core.GetString(lang); ok {
			kdict.Lang = langStr
		}
	}
	if alt := dict.Get("Alt"); alt != nil {
		if altStr, ok := core.GetString(alt); ok {
			kdict.Alt = altStr
		}
	}
	if e := dict.Get("E"); e != nil {
		if eStr, ok := core.GetString(e); ok {
			kdict.E = eStr
		}
	}
	if actualText := dict.Get("ActualText"); actualText != nil {
		if actualTextStr, ok := core.GetString(actualText); ok {
			kdict.ActualText = actualTextStr
		}
	}
	if ns := dict.Get("NS"); ns != nil {
		if structureNamespace, ok := newPdfStructureNamespaceFromObject(ns); ok {
			kdict.NS = structureNamespace
		}
	}
	if af, ok := core.GetArray(dict.Get("AF")); ok {
		kdict.AF = af
	}
	return kdict, nil
}

// SetCatalogMetadata sets the catalog metadata (XMP) stream object.
func (w *PdfWriter) SetCatalogMetadata(meta core.PdfObject) error {
	if meta == nil {
		w.catalog.Remove("Metadata")
		return nil
	}
	metaStream, ok := core.GetStream(meta)
	if !ok {
		return errors.New("catalog metadata must be a valid stream")
	}
	w.addObject(metaStream)
	w.catalog.Set("Metadata", metaStream)
	return nil
}

// ToPdfObject implements interface PdfModel.
func (p *Permissions) ToPdfObject() core.PdfObject { return p.primitive }

// NewOutlineItem returns a new outline item instance.
func NewOutlineItem(title string, dest OutlineDest) *OutlineItem {
	return &OutlineItem{Title: title, Dest: dest}
}

// RevocationCheck selects the revocation data sources consulted during
// signature validation.
type RevocationCheck int

const revocationUnknownMessage = "revocation status unknown: no usable revocation data for the certificate chain"

func (r *PdfReader) newPdfActionTransFromDict(dict *core.PdfObjectDictionary) (*PdfActionTrans, error) {
	return &PdfActionTrans{Trans: dict.Get("Trans")}, nil
}

func (w *PdfWriter) writeObject(n int, obj core.PdfObject) error {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Write obj #%d\n", n)
	}
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		w.crossReferences[n] = crossReference{Type: 1, Offset: w.writePos, Generation: indirect.GenerationNumber}
		s := fmt.Sprintf("%d 0 obj\n", n)
		if signDict, ok := indirect.PdfObject.(*pdfSignDictionary); ok {
			signDict.fileOffset = w.writePos + int64(len(s))
		}
		if indirect.PdfObject == nil {
			common.Log.Debug("Error: indirect object's PdfObject should never be nil - setting to PdfObjectNull")
			indirect.PdfObject = core.MakeNull()
		}
		s += string(indirect.PdfObject.Write())
		s += "\nendobj\n"
		w.writeString(s)
		return nil
	}
	if stream, ok := obj.(*core.PdfObjectStream); ok {
		w.crossReferences[n] = crossReference{Type: 1, Offset: w.writePos, Generation: stream.GenerationNumber}
		w.writeString(fmt.Sprintf("%d 0 obj\n", n))
		w.writeBytes(stream.PdfObjectDictionary.Write())
		w.writeString("\nstream\n")
		if stream.Lazy {
			if err := writeLazyStream(lazyStreamWriter{w}, n, stream, w.crypter); err != nil {
				return err
			}
			w.lazyStreams = append(w.lazyStreams, stream)
		} else {
			w.writeBytes(stream.Stream)
		}
		w.writeString("\nendstream\nendobj\n")
		return nil
	}
	if objStreams, ok := obj.(*core.PdfObjectStreams); ok {
		w.crossReferences[n] = crossReference{Type: 1, Offset: w.writePos, Generation: objStreams.GenerationNumber}
		s := fmt.Sprintf("%d 0 obj\n", n)
		var builder, builder2 strings.Builder
		var count int64
		num := 0
		for _, obj := range objStreams.Elements() {
			indirect, ok := obj.(*core.PdfIndirectObject)
			if !ok {
				common.Log.Debug("ERROR: Object streams N %d contains non indirect pdf object %v", n, obj)
				continue
			}
			if w.excludedFromObjectStream(indirect) {
				continue
			}
			data := append(indirect.PdfObject.Write(), ' ')
			builder2.Write(data)
			fmt.Fprintf(&builder, "%d %d ", indirect.ObjectNumber, count)
			w.crossReferences[int(indirect.ObjectNumber)] = crossReference{Type: 2, ObjectNumber: n, Index: num}
			num++
			count += int64(len(data))
		}
		flateEncoder := core.NewFlateEncoder()
		streamDict := flateEncoder.MakeStreamDict()
		streamDict.Set(core.PdfObjectName("Type"), core.MakeName("ObjStm"))
		streamDict.Set(core.PdfObjectName("N"), core.MakeInteger(int64(num)))
		builderLen := int64(builder.Len())
		streamDict.Set(core.PdfObjectName("First"), core.MakeInteger(builderLen))
		encodeBytes, _ := flateEncoder.EncodeBytes([]byte(builder.String() + builder2.String()))
		if w.crypter != nil {
			stream := &core.PdfObjectStream{PdfObjectDictionary: streamDict, Stream: encodeBytes}
			stream.ObjectNumber = int64(n)
			if err := w.crypter.Encrypt(stream, int64(n), 0); err != nil {
				return fmt.Errorf("failed encrypting object stream %d: %w", n, err)
			}
			encodeBytes = stream.Stream
		}
		size := int64(len(encodeBytes))
		streamDict.Set(core.PdfObjectName("Length"), core.MakeInteger(size))
		s += string(streamDict.Write())
		s += "\nstream\n"
		w.writeString(s)
		w.writeBytes(encodeBytes)
		w.writeString("\nendstream\nendobj\n")
		return nil
	}
	w.writeBytes(obj.Write())
	return nil
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

// SetCenterWindow sets the value of the centerWindow flag.
func (vp *ViewerPreferences) SetCenterWindow(centerWindow bool) { vp.centerWindow = &centerWindow }
func (f *pdfCIDFontType0) baseFields() *fontCommon              { return &f.fontCommon }

// GetContainingPdfObject returns the XObject Form's containing object (indirect object).
func (x *XObjectForm) GetContainingPdfObject() core.PdfObject { return x.primitive }

// Direct returns the object itself; the transform parameters are always a direct object.
func (p *PdfTransformParamsDocMDP) Direct() core.PdfObject { return p }

func (w *PdfWriter) writeObjects() {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Writing %d obj", len(w.objects))
	}
	w.crossReferences = make(map[int]crossReference)
	w.crossReferences[0] = crossReference{Type: 0, ObjectNumber: 0, Generation: 0xFFFF}
	if w.prevXrefs.ObjectMap != nil {
		for n, objectMap := range w.prevXrefs.ObjectMap {
			if n == 0 {
				continue
			}
			if objectMap.XType == core.XrefTypeObjectStream {
				xref := crossReference{Type: 2, ObjectNumber: objectMap.OsObjNumber, Index: objectMap.OsObjIndex}
				w.crossReferences[n] = xref
			}
			if objectMap.XType == core.XrefTypeTableEntry {
				xref := crossReference{Type: 1, ObjectNumber: objectMap.ObjectNumber, Offset: objectMap.Offset}
				w.crossReferences[n] = xref
			}
		}
	}
}

// SetContext sets the sub annotation (context).
func (a *PdfAnnotation) SetContext(ctx PdfModel) { a.context = ctx }

// GetAnnotations returns the list of page annotations for `page`. If not loaded attempts to load the
// annotations, otherwise returns the loaded list. Safe for concurrent use: the
// lazy load is guarded so the same page may be read from multiple goroutines.
func (p *PdfPage) GetAnnotations() ([]*PdfAnnotation, error) {
	if p.mu != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
	}
	return p.getAnnotationsLocked()
}

// Decrypt decrypts the PDF file with a specified password.  Also tries to
// decrypt with an empty password.  Returns true if successful,
// false otherwise.
//
// For AES-256 (R=5/6) encryption the password bytes are interpreted as UTF-8
// and prepared with the SASLprep profile (RFC 4013), as required by
// ISO 32000-2. If the prepared password does not authenticate, the raw bytes
// are tried as well. Input that is not valid UTF-8 is used as given without
// preparation. For RC4 and AES-128 the password bytes are used as given.
func (r *PdfReader) Decrypt(password []byte) (bool, error) {
	ok, err := r.parser.Decrypt(password)
	if err != nil {
		return ok, err
	}
	if !ok {
		return false, nil
	}
	err = r.loadStructure()
	if err != nil {
		common.Log.Debug("ERROR: Fail to load structure (%s)", err)
		return false, err
	}
	return true, nil
}

// NewMultipleFontEncoder returns instantiates a new *MultipleFontEncoder
func NewMultipleFontEncoder(fonts []*PdfFont) *MultipleFontEncoder {
	return &MultipleFontEncoder{fontList: fonts, CurrentFont: fonts[0]}
}

func (r *PdfReader) newPdfActionLaunchFromDict(dict *core.PdfObjectDictionary) (*PdfActionLaunch, error) {
	filespec, err := newPdfFilespecFromPdfObject(dict.Get("F"))
	if err != nil {
		return nil, err
	}
	return &PdfActionLaunch{Win: dict.Get("Win"), Mac: dict.Get("Mac"), Unix: dict.Get("Unix"), NewWindow: dict.Get("NewWindow"), F: filespec}, nil
}

// GenerateXObjectName generates an unused XObject name that can be used for
// adding new XObjects. Uses format XObj1, XObj2, ...
func (r *PdfPageResources) GenerateXObjectName() core.PdfObjectName {
	n := 1
	for {
		name := core.MakeName(fmt.Sprintf("XObj%d", n))
		if !r.HasXObjectByName(*name) {
			return *name
		}
		n++
	}
}

func (w *PdfWriter) setEncryption(crypter *core.PdfCrypt, encryptInfo *core.EncryptInfo) {
	w.crypter = crypter
	if encryptInfo.Major != 0 {
		w.SetVersion(encryptInfo.Major, encryptInfo.Minor)
	}
	w.encryptDict = encryptInfo.Encrypt
	w.id0, w.id1 = encryptInfo.ID0, encryptInfo.ID1
	if encryptObj := w.encryptObj; encryptObj != nil {
		delete(w.hasObjects, encryptObj)
		w.objects = slices.DeleteFunc(w.objects, func(obj core.PdfObject) bool { return obj == encryptObj })
	}
	indirectObject := core.MakeIndirectObject(encryptInfo.Encrypt)
	w.encryptObj = indirectObject
	w.addObject(indirectObject)
}

// KValue is a wrapper object to hold various type of K's children objects.
type KValue struct {
	kDict     *KDict
	refObject core.PdfObject
	mcid      *int
}

// RemoveChild removes a specific KValue child from the kChildren slice.
func (k *KDict) RemoveChild(kv *KValue) bool {
	for i, children := range k.children {
		if children == kv {
			k.children = append(k.children[:i], k.children[i+1:]...)
			return true
		}
	}
	return false
}

type planSizes struct {
	hintLen      int
	firstXrefLen int
	mainXrefLen  int
}
type preserializedObject struct {
	objNum int64
	genNum int64
	obj    core.PdfObject
	data   []byte
	offset int64
}

// Duplicate creates a duplicate page based on the current one and returns it.
func (p *PdfPage) Duplicate() *PdfPage {
	page := *p
	page.mu = &sync.Mutex{}
	page.pageDict = core.MakeDict()
	page.primitive = core.MakeIndirectObject(page.pageDict)
	page.originalDict = page.pageDict.ShallowCopy()
	return &page
}

func (d *DSS) generateHashMap(streams []*core.PdfObjectStream) (map[string]*core.PdfObjectStream, error) {
	streamByKey := map[string]*core.PdfObjectStream{}
	for _, stream := range streams {
		data, err := core.DecodeStream(stream)
		if err != nil {
			return nil, err
		}
		data2, err := sha1Digest(data)
		if err != nil {
			return nil, err
		}
		streamByKey[string(data2)] = stream
	}
	return streamByKey, nil
}

// SetContext sets the sub pattern (context).  Either PdfTilingPattern or PdfShadingPattern.
func (p *PdfPattern) SetContext(ctx PdfModel) { p.context = ctx }

// GetContentStreamObjs returns a slice of PDF objects containing the content
// streams of the page.
func (p *PdfPage) GetContentStreamObjs() []core.PdfObject {
	if p.Contents == nil {
		return nil
	}
	obj := core.TraceToDirectObject(p.Contents)
	if arr, ok := obj.(*core.PdfObjectArray); ok {
		return arr.Elements()
	}
	return []core.PdfObject{obj}
}

// Compress is yet to be implemented.
// Should be able to compress in terms of JPEG quality parameter,
// and DPI threshold (need to know bounding area dimensions).
func (h DefaultImageHandler) Compress(input *Image, quality int64) (*Image, error) {
	return input, nil
}

// CheckAccessRightsWithCertificate reports whether the certificate can open a
// public-key encrypted document and the permissions its recipient entry
// grants, without decrypting the document.
func (r *PdfReader) CheckAccessRightsWithCertificate(cert *x509.Certificate, key crypto.PrivateKey) (bool, security.Permissions, error) {
	return r.parser.CheckAccessRightsWithCertificate(cert, key)
}

// Add appends an outline item as a child of the current outline item.
func (o *OutlineItem) Add(item *OutlineItem) { o.Entries = append(o.Entries, item) }

// StdFontName represents name of a standard font.
type StdFontName = fonts.StdFontName

// DSS represents a Document Security Store dictionary.
// The DSS dictionary contains both global and signature specific validation
// information. The certificates and revocation data in the `Certs`, `OCSPs`,
// and `CRLs` fields can be used to validate any signature in the document.
// Additionally, the VRI entry contains validation data per signature.
// The keys in the VRI entry are calculated as upper(hex(sha1(sig.Contents))).
// The values are VRI dictionaries containing certificates and revocation
// information used for validating a single signature.
// See ETSI TS 102 778-4 V1.1.1 for more information.
type DSS struct {
	primitive *core.PdfIndirectObject
	Certs     []*core.PdfObjectStream
	OCSPs     []*core.PdfObjectStream
	CRLs      []*core.PdfObjectStream
	VRI       map[string]*VRI
	certsMap  map[string]*core.PdfObjectStream
	ocspsMap  map[string]*core.PdfObjectStream
	crlsMap   map[string]*core.PdfObjectStream
}

// PrintPageRange returns the value of the printPageRange.
func (vp *ViewerPreferences) PrintPageRange() []int { return vp.printPageRange }

// HasFontByName checks if has font resource by name.
func (p *PdfPage) HasFontByName(name core.PdfObjectName) bool {
	dict, ok := p.Resources.Font.(*core.PdfObjectDictionary)
	if !ok {
		return false
	}
	if obj := dict.Get(name); obj != nil {
		return true
	}
	return false
}

func (w *PdfWriter) hasObject(obj core.PdfObject) bool {
	_, ok := w.hasObjects[obj]
	return ok
}

func registerGlyphWidthsFromGIDs(w core.PdfObject, vals []uint16) {
	var n uint16
	for _, val := range vals {
		if val > n {
			n = val
		}
	}
	count := n + 1
	cidWidths, err := parseCIDWidths(w)
	if err != nil || cidWidths == nil {
		return
	}
	set := make(map[textencoding.CharCode]bool, len(vals))
	for _, val := range vals {
		set[textencoding.CharCode(val)] = true
	}
	numByV := make(map[fonts.GID]int, len(cidWidths))
	for code, cidWidth := range cidWidths {
		if code < textencoding.CharCode(count) && set[code] {
			numByV[fonts.GID(code)] = int(cidWidth)
		}
	}
	widthsArray := buildWidthsArray(numByV, count)
	if wIndirect, ok := core.GetIndirect(w); ok {
		wIndirect.PdfObject = widthsArray
	}
}

// ToPdfObject implements interface PdfModel.
func (p *PdfTransformParamsDocMDP) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	dict.SetIfNotNil("Type", p.Type)
	dict.SetIfNotNil("V", p.V)
	dict.SetIfNotNil("P", p.P)
	return dict
}

func sortedUniqueGlyphIndexes(gids []unitype.GlyphIndex) []unitype.GlyphIndex {
	slices.Sort(gids)
	return slices.Compact(gids)
}

func (img *Image) samplesTrimPadding(nums []uint32) []uint32 {
	n := img.ColorComponents * int(img.Width) * int(img.Height)
	if len(nums) == n {
		return nums
	}
	nums2 := make([]uint32, n)
	count := int(img.Width) * img.ColorComponents
	var num, size, length, length2 int
	bytesPerLine := imageutil.BytesPerLine(int(img.Width), int(img.BitsPerComponent), img.ColorComponents)
	for num = 0; num < int(img.Height); num++ {
		size = num * int(img.Width)
		length = num * bytesPerLine
		for length2 = 0; length2 < count; length2++ {
			nums2[size+length2] = nums[length+length2]
		}
	}
	return nums2
}

// PdfAnnotationMarkup represents additional fields for mark-up annotations.
// (Section 12.5.6.2 p. 399).
type PdfAnnotationMarkup struct {
	T            core.PdfObject
	Popup        *PdfAnnotationPopup
	CA           core.PdfObject
	RC           core.PdfObject
	CreationDate core.PdfObject
	IRT          core.PdfObject
	Subj         core.PdfObject
	RT           core.PdfObject
	IT           core.PdfObject
	ExData       core.PdfObject
}

// ValidateSignaturesWithOpts validates the digital signatures in the
// document: integrity through the matching handler, then certificate trust
// according to opts. A nil opts performs the integrity check only.
func (r *PdfReader) ValidateSignaturesWithOpts(handlers []SignatureHandler, opts *SignatureValidationOpts) ([]SignatureValidationResult, error) {
	if r.AcroForm == nil {
		return nil, nil
	}
	if r.AcroForm.Fields == nil {
		return nil, nil
	}
	type sigFieldPair struct {
		signature *PdfSignature
		field     *PdfField
		handler   SignatureHandler
	}
	var sigFieldPairs []*sigFieldPair
	for _, allField := range r.AcroForm.AllFields() {
		if allField.V == nil {
			continue
		}
		if dict, ok := core.GetDict(allField.V); ok {
			if s, ok := core.GetNameVal(dict.Get("Type")); ok && (s == "Sig" || s == "DocTimeStamp") {
				indirect, ok := core.GetIndirect(allField.V)
				if !ok {
					common.Log.Debug("ERROR: Signature container is nil")
					return nil, ErrTypeCheck
				}
				sig, err := r.newPdfSignatureFromIndirect(indirect)
				if err != nil {
					return nil, err
				}
				var signatureHandler SignatureHandler
				for _, handler := range handlers {
					if handler.IsApplicable(sig) {
						signatureHandler = handler
						break
					}
				}
				sigFieldPairs = append(sigFieldPairs, &sigFieldPair{signature: sig, field: allField, handler: signatureHandler})
			}
		}
	}
	evaluator := &trustEvaluator{reader: r, opts: opts}
	var results []SignatureValidationResult
	fn := func(sig *PdfSignature, result SignatureValidationResult, certs []*x509.Certificate) {
		evaluator.evaluate(sig, &result, certs)
		results = append(results, result)
	}
	for _, sigFieldPair := range sigFieldPairs {
		result := SignatureValidationResult{IsSigned: true, Fields: []*PdfField{sigFieldPair.field}}
		byteRange := sigFieldPair.signature.ByteRange
		var nums []int64
		var err error
		if byteRange != nil {
			nums, err = parseByteRange(byteRange, r.parser.Size())
			if err == nil {
				fillSignatureCoverage(r.parser, nums, sigFieldPair.signature.Contents, &result)
			}
		}
		if certs, err := sigFieldPair.signature.GetCerts(); err != nil {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("ERROR: unable to extract signature certificates: %v", err)
			}
		} else {
			result.Certificates = certs
		}
		if sigFieldPair.handler == nil {
			result.Errors = append(result.Errors, "handler not set")
			fn(sigFieldPair.signature, result, nil)
			continue
		}
		digest, digestErr := sigFieldPair.handler.NewDigest(sigFieldPair.signature)
		if digestErr != nil {
			result.Errors = append(result.Errors, "digest error", digestErr.Error())
			fn(sigFieldPair.signature, result, nil)
			continue
		}
		if byteRange == nil {
			result.Errors = append(result.Errors, "ByteRange not set")
			fn(sigFieldPair.signature, result, nil)
			continue
		}
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			fn(sigFieldPair.signature, result, nil)
			continue
		}
		for i := 0; i < len(nums); i += 2 {
			bytesAt, err := r.parser.ReadBytesAt(nums[i], nums[i+1])
			if err != nil {
				return nil, err
			}
			digest.Write(bytesAt)
		}
		var result2 SignatureValidationResult
		if signatureHandlerDocMDP, ok := sigFieldPair.handler.(SignatureHandlerDocMDP); ok {
			result2, digestErr = signatureHandlerDocMDP.ValidateWithOpts(sigFieldPair.signature, digest, SignatureHandlerDocMDPParams{Parser: r.parser})
		} else {
			result2, digestErr = sigFieldPair.handler.Validate(sigFieldPair.signature, digest)
		}
		if digestErr != nil {
			common.Log.Debug("ERROR: %v (%T) - skip", digestErr, sigFieldPair.handler)
			result2.Errors = append(result2.Errors, digestErr.Error())
		}
		result2.copyCoverageFrom(&result)
		certificates := result2.Certificates
		if result2.Certificates == nil {
			result2.Certificates = result.Certificates
		}
		result2.Name = sigFieldPair.signature.Name.Decoded()
		result2.Reason = sigFieldPair.signature.Reason.Decoded()
		if sigFieldPair.signature.M != nil {
			if date, err := NewPdfDate(sigFieldPair.signature.M.String()); err != nil {
				common.Log.Debug("ERROR: %v", err)
				result2.Errors = append(result2.Errors, err.Error())
			} else {
				result2.Date = date
			}
		}
		result2.ContactInfo = sigFieldPair.signature.ContactInfo.Decoded()
		result2.Location = sigFieldPair.signature.Location.Decoded()
		result2.Fields = result.Fields
		fn(sigFieldPair.signature, result2, certificates)
	}
	return results, nil
}

// AddNamespace adds a namespace dictionary to the structure tree root.
func (r *StructTreeRoot) AddNamespace(ns *PdfStructureNamespace) {
	r.Namespaces = append(r.Namespaces, ns)
}

// NewPdfWriter initializes a new PdfWriter.
func NewPdfWriter() PdfWriter {
	writer := PdfWriter{}
	writer.hasObjects = map[core.PdfObject]struct{}{}
	writer.objects = []core.PdfObject{}
	writer.missingParents = map[core.PdfObject][]*core.PdfObjectDictionary{}
	writer.traversed = map[core.PdfObject]struct{}{}
	writer.version.Major = 1
	writer.version.Minor = 3
	dict := core.MakeDict()
	sts := []struct {
		key   core.PdfObjectName
		value string
	}{{"Producer", getPdfProducer()}, {"Creator", getPdfCreator()}, {"Author", getPdfAuthor()}, {"Subject", getPdfSubject()}, {"Title", getPdfTitle()}, {"Keywords", getPdfKeywords()}}
	for _, st := range sts {
		if st.value != "" {
			dict.Set(st.key, core.MakeString(st.value))
		}
	}
	if creationDate := getCreationDate(); !creationDate.IsZero() {
		if date, err := NewPdfDateFromTime(creationDate); err == nil {
			dict.Set("CreationDate", date.ToPdfObject())
		}
	}
	if modifiedDate := getModifiedDate(); !modifiedDate.IsZero() {
		if date, err := NewPdfDateFromTime(modifiedDate); err == nil {
			dict.Set("ModDate", date.ToPdfObject())
		}
	}
	indirect := core.PdfIndirectObject{}
	indirect.PdfObject = dict
	writer.infoObj = &indirect
	writer.addObject(&indirect)
	indirect2 := core.PdfIndirectObject{}
	dict2 := core.MakeDict()
	dict2.Set("Type", core.MakeName("Catalog"))
	indirect2.PdfObject = dict2
	writer.root = &indirect2
	writer.addObject(writer.root)
	randomID, err := randomIDWithPrefix("w")
	if err != nil {
		common.Log.Debug("ERROR: %v", err)
	}
	writer.trackID = randomID
	indirect3 := core.PdfIndirectObject{}
	dict3 := core.MakeDict()
	dict3.Set("Type", core.MakeName("Pages"))
	arr := core.PdfObjectArray{}
	dict3.Set("Kids", &arr)
	dict3.Set("Count", core.MakeInteger(0))
	indirect3.PdfObject = dict3
	writer.pages = &indirect3
	writer.pageDicts = map[core.PdfObject]struct{}{}
	writer.pageObjs = []*core.PdfIndirectObject{}
	writer.addObject(writer.pages)
	dict2.Set("Pages", &indirect3)
	writer.catalog = dict2
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Catalog %s", indirect2)
	}
	return writer
}

// PdfOutlineItem represents an outline item dictionary (Table 153 - pp. 376 - 377).
type PdfOutlineItem struct {
	PdfOutlineTreeNode
	Title     *core.PdfObjectString
	Parent    *PdfOutlineTreeNode
	Prev      *PdfOutlineTreeNode
	Next      *PdfOutlineTreeNode
	Count     *int64
	Dest      core.PdfObject
	A         core.PdfObject
	SE        core.PdfObject
	C         core.PdfObject
	F         core.PdfObject
	primitive *core.PdfIndirectObject
}

// ImageToRGB returns the passed in image. Method exists in order to satisfy
// the PdfColorspace interface.
func (cs *PdfColorspaceDeviceRGB) ImageToRGB(img Image) (Image, error) { return img, nil }

// SetFlag sets the flag for the field.
func (f *PdfField) SetFlag(flag FieldFlag) { f.Ff = core.MakeInteger(int64(flag)) }

// Items returns all children outline items.
func (o *OutlineItem) Items() []*OutlineItem { return o.Entries }

type fetchKey struct {
	url        string
	certHash   [32]byte
	issuerHash [32]byte
}

const (
	maxFunctionInputs = 8
	maxSampledInputs  = 8
)

// SetNamedDestinations sets the Dests entry in the PDF catalog.
// See section 12.3.2.3 "Named Destinations" (p. 367 PDF32000_2008).
func (w *PdfWriter) SetNamedDestinations(dests core.PdfObject) error {
	if dests == nil {
		return nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Setting catalog Dests...")
	}
	w.catalog.Set("Dests", dests)
	return w.addObjects(dests)
}

// Tab order types.
type TabOrderType string

// ColorToRGB converts a DeviceN color to an RGB color.
func (cs *PdfColorspaceDeviceN) ColorToRGB(color PdfColor) (PdfColor, error) {
	if cs.AlternateSpace == nil {
		return nil, errors.New("DeviceN alternate space undefined")
	}
	return cs.AlternateSpace.ColorToRGB(color)
}

// NewEmbeddedFileFromContent construct a new EmbeddedFile from supplied file content.
func NewEmbeddedFileFromContent(content []byte) (*EmbeddedFile, error) {
	match, err := filetype.Match(content)
	if err != nil {
		common.Log.Debug("Failed to detect file type: %v", err)
	}
	data := md5.Sum(content)
	file := &EmbeddedFile{Name: "attachment", Content: content, FileType: match.MIME.Type, Hash: hex.EncodeToString(data[:])}
	return file, nil
}

// PdfShadingPatternType1 is shading patterns that will use a Type 1 shading pattern (Function-based).
type PdfShadingPatternType1 struct {
	*PdfPattern
	Shading   *PdfShadingType1
	Matrix    *core.PdfObjectArray
	ExtGState core.PdfObject
}

// RepairAcroForm attempts to rebuild the AcroForm fields using the widget
// annotations present in the document pages. Pass nil for the opts parameter
// in order to use the default options.
// NOTE: Currently, the opts parameter is declared in order to enable adding
// future options, but passing nil will always result in the default options
// being used.
func (r *PdfReader) RepairAcroForm(opts *AcroFormRepairOptions) error {
	var fields []*PdfField
	set := map[*core.PdfIndirectObject]struct{}{}
	for _, page := range r.PageList {
		annotations, err := page.GetAnnotations()
		if err != nil {
			return err
		}
		for _, annotation := range annotations {
			var field *PdfField
			switch v := annotation.GetContext().(type) {
			case *PdfAnnotationWidget:
				if v.field != nil {
					field = v.field
					break
				}
				if parentIndirect, ok := core.GetIndirect(v.Parent); ok {
					field, err = r.newPdfFieldFromIndirectObject(parentIndirect, nil)
					if err == nil {
						break
					}
					common.Log.Debug("WARN: could not parse form field %+v: %v", parentIndirect, err)
				}
				if v.primitive != nil {
					field, err = r.newPdfFieldFromIndirectObject(v.primitive, nil)
					if err == nil {
						break
					}
					common.Log.Debug("WARN: could not parse form field %+v: %v", v.primitive, err)
				}
			}
			if field == nil {
				continue
			}
			if _, ok := set[field.primitive]; ok {
				continue
			}
			set[field.primitive] = struct{}{}
			fields = append(fields, field)
		}
	}
	if len(fields) == 0 {
		return nil
	}
	if r.AcroForm == nil {
		r.AcroForm = NewPdfAcroForm()
	}
	r.AcroForm.Fields = &fields
	return nil
}

func (f *PdfField) resolveValueString(ok bool) string {
	var obj core.PdfObject
	_, _ = f.inherit(func(field *PdfField) bool {
		var obj2 core.PdfObject
		if ok {
			obj2 = field.DV
		} else {
			obj2 = field.V
		}
		if obj2 == nil {
			return false
		}
		obj = obj2
		return true
	})
	if obj != nil {
		return decodedString(obj)
	}
	if ok {
		return ""
	}
	for _, annotation := range f.Annotations {
		if annotation == nil || annotation.PdfAnnotation == nil || annotation.AS == nil {
			continue
		}
		if name, ok := core.GetName(annotation.AS); ok {
			return string(*name)
		}
	}
	return ""
}

// ToPdfObject implements interface PdfModel.
func (a *PdfAnnotationFreeText) ToPdfObject() core.PdfObject {
	a.PdfAnnotation.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	a.appendToPdfDictionary(dict)
	dict.SetIfNotNil("Subtype", core.MakeName("FreeText"))
	dict.SetIfNotNil("DA", a.DA)
	dict.SetIfNotNil("Q", a.Q)
	dict.SetIfNotNil("RC", a.RC)
	dict.SetIfNotNil("DS", a.DS)
	dict.SetIfNotNil("CL", a.CL)
	dict.SetIfNotNil("IT", a.IT)
	dict.SetIfNotNil("BE", a.BE)
	dict.SetIfNotNil("RD", a.RD)
	dict.SetIfNotNil("BS", a.BS)
	dict.SetIfNotNil("LE", a.LE)
	return indirect
}

// GetPieceInfo gets catalog piece info object.
func (r *PdfReader) GetPieceInfo() (core.PdfObject, bool) {
	if r.catalog == nil {
		return nil, false
	}
	pieceInfo := r.catalog.Get("PieceInfo")
	return pieceInfo, pieceInfo != nil
}

// EncryptOptions represents encryption options for an output PDF.
type EncryptOptions struct {
	// Permissions granted when the document is opened with the user password
	// (Encrypt) or to every recipient (EncryptForRecipients). The zero value
	// grants none; security.PermOwner grants all, as does a nil
	// EncryptOptions. Encrypt sets the reserved bits of /P itself.
	// EncryptForRecipients writes only bits 3-6 and 9-12, always sets bit 10
	// as PDF 2.0 requires, and makes recipients granted every permission
	// owner-equivalent (able to change the security settings).
	Permissions security.Permissions

	// Algorithm selects the cipher. The zero value is RC4_128bit, which
	// Encrypt uses as its default; EncryptForRecipients does not produce RC4
	// output and treats the zero value as AES_256bit.
	Algorithm EncryptionAlgorithm
}

func (c *parsedCRL) entryFor(cert *x509.Certificate) *x509.RevocationListEntry {
	if c.idp != nil && c.idp.OnlyContainsAttributeCerts {
		return nil
	}
	ok := true
	for i := range c.RevokedCertificateEntries {
		revokedCertificateEntry := &c.RevokedCertificateEntries[i]
		for _, extension := range revokedCertificateEntry.Extensions {
			if extension.Id.Equal(oidCertificateIssuer) {
				ok = matchesAuthorityCertIssuer(extension.Value, cert.RawIssuer)
			}
		}
		if ok && revokedCertificateEntry.SerialNumber.Cmp(cert.SerialNumber) == 0 {
			return revokedCertificateEntry
		}
	}
	return nil
}

func (r *PdfReader) newPdfActionFromIndirectObject(indirect *core.PdfIndirectObject) (*PdfAction, error) {
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return nil, fmt.Errorf("action indirect object not containing a dictionary")
	}
	if model := r.modelManager.GetModelFromPrimitive(dict); model != nil {
		action, ok := model.(*PdfAction)
		if !ok {
			return nil, fmt.Errorf("cached model not a PDF action")
		}
		return action, nil
	}
	action := &PdfAction{}
	action.primitive = indirect
	r.modelManager.Register(dict, action)
	if obj := dict.Get("Type"); obj != nil {
		name, ok := obj.(*core.PdfObjectName)
		if !ok {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Incompatibility! Invalid type of Type (%T) - should be Name", obj)
			}
		} else {
			if *name != "Action" {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Unsuspected Type != Action (%s)", *name)
				}
			}
			action.Type = name
		}
	}
	if next := dict.Get("Next"); next != nil {
		action.Next = next
	}
	if s := dict.Get("S"); s != nil {
		action.S = s
	}
	name, found := action.S.(*core.PdfObjectName)
	if !found {
		common.Log.Debug("ERROR: Invalid S object type != name (%T)", action.S)
		return nil, fmt.Errorf("invalid S object type != name (%T)", action.S)
	}
	typeAction := PdfActionType(name.String())
	switch typeAction {
	case ActionTypeGoTo:
		goToAction, err := r.newPdfActionGotoFromDict(dict)
		if err != nil {
			return nil, err
		}
		goToAction.PdfAction = action
		action.context = goToAction
		return action, nil
	case ActionTypeGoToR:
		goToRAction, err := r.newPdfActionGotoRFromDict(dict)
		if err != nil {
			return nil, err
		}
		goToRAction.PdfAction = action
		action.context = goToRAction
		return action, nil
	case ActionTypeGoToE:
		goToEAction, err := r.newPdfActionGotoEFromDict(dict)
		if err != nil {
			return nil, err
		}
		goToEAction.PdfAction = action
		action.context = goToEAction
		return action, nil
	case ActionTypeLaunch:
		launchAction, err := r.newPdfActionLaunchFromDict(dict)
		if err != nil {
			return nil, err
		}
		launchAction.PdfAction = action
		action.context = launchAction
		return action, nil
	case ActionTypeThread:
		threadAction, err := r.newPdfActionThreadFromDict(dict)
		if err != nil {
			return nil, err
		}
		threadAction.PdfAction = action
		action.context = threadAction
		return action, nil
	case ActionTypeURI:
		uriAction, err := r.newPdfActionURIFromDict(dict)
		if err != nil {
			return nil, err
		}
		uriAction.PdfAction = action
		action.context = uriAction
		return action, nil
	case ActionTypeSound:
		soundAction, err := r.newPdfActionSoundFromDict(dict)
		if err != nil {
			return nil, err
		}
		soundAction.PdfAction = action
		action.context = soundAction
		return action, nil
	case ActionTypeMovie:
		movieAction, err := r.newPdfActionMovieFromDict(dict)
		if err != nil {
			return nil, err
		}
		movieAction.PdfAction = action
		action.context = movieAction
		return action, nil
	case ActionTypeHide:
		hideAction, err := r.newPdfActionHideFromDict(dict)
		if err != nil {
			return nil, err
		}
		hideAction.PdfAction = action
		action.context = hideAction
		return action, nil
	case ActionTypeNamed:
		namedAction, err := r.newPdfActionNamedFromDict(dict)
		if err != nil {
			return nil, err
		}
		namedAction.PdfAction = action
		action.context = namedAction
		return action, nil
	case ActionTypeSubmitForm:
		submitFormAction, err := r.newPdfActionSubmitFormFromDict(dict)
		if err != nil {
			return nil, err
		}
		submitFormAction.PdfAction = action
		action.context = submitFormAction
		return action, nil
	case ActionTypeResetForm:
		resetFormAction, err := r.newPdfActionResetFormFromDict(dict)
		if err != nil {
			return nil, err
		}
		resetFormAction.PdfAction = action
		action.context = resetFormAction
		return action, nil
	case ActionTypeImportData:
		importDataAction, err := r.newPdfActionImportDataFromDict(dict)
		if err != nil {
			return nil, err
		}
		importDataAction.PdfAction = action
		action.context = importDataAction
		return action, nil
	case ActionTypeSetOCGState:
		setOCGStateAction, err := r.newPdfActionSetOCGStateFromDict(dict)
		if err != nil {
			return nil, err
		}
		setOCGStateAction.PdfAction = action
		action.context = setOCGStateAction
		return action, nil
	case ActionTypeRendition:
		renditionAction, err := r.newPdfActionRenditionFromDict(dict)
		if err != nil {
			return nil, err
		}
		renditionAction.PdfAction = action
		action.context = renditionAction
		return action, nil
	case ActionTypeTrans:
		transAction, err := r.newPdfActionTransFromDict(dict)
		if err != nil {
			return nil, err
		}
		transAction.PdfAction = action
		action.context = transAction
		return action, nil
	case ActionTypeGoTo3DView:
		goTo3DViewAction, err := r.newPdfActionGoTo3DViewFromDict(dict)
		if err != nil {
			return nil, err
		}
		goTo3DViewAction.PdfAction = action
		action.context = goTo3DViewAction
		return action, nil
	case ActionTypeJavaScript:
		javaScriptAction, err := r.newPdfActionJavaScriptFromDict(dict)
		if err != nil {
			return nil, err
		}
		javaScriptAction.PdfAction = action
		action.context = javaScriptAction
		return action, nil
	}
	common.Log.Debug("ERROR: Ignoring unknown action: %s", typeAction)
	return nil, nil
}

func (f *PdfField) resolveQ() Quadding {
	quadding := QuaddingLeft
	_, _ = f.inherit(func(field *PdfField) bool {
		if field.VariableText != nil && field.VariableText.Q != nil {
			quadding2 := Quadding(*field.VariableText.Q)
			switch quadding2 {
			case QuaddingLeft, QuaddingCentered, QuaddingRight:
				quadding = quadding2
			default:
				quadding = QuaddingLeft
			}
			return true
		}
		return false
	})
	return quadding
}

// GetKDict returns the KDict of the KValue.
func (kv *KValue) GetKDict() *KDict { return kv.kDict }

// Resample resamples the image data converting from current BitsPerComponent to a target BitsPerComponent
// value.  Sets the image's BitsPerComponent to the target value following resampling.
//
// For example, converting an 8-bit RGB image to 1-bit grayscale (common for scanned images):
//
//	// Convert RGB image to grayscale.
//	rgbColorSpace := pdf.NewPdfColorspaceDeviceRGB()
//	grayImage, err := rgbColorSpace.ImageToGray(rgbImage)
//	if err != nil {
//	  return err
//	}
//	// Resample as 1 bit.
//	grayImage.Resample(1)
func (img *Image) Resample(targetBitsPerComponent int64) {
	if img.BitsPerComponent == targetBitsPerComponent {
		return
	}
	img.clearSourceMetadata()
	samples := img.GetSamples()
	if targetBitsPerComponent < img.BitsPerComponent {
		n := img.BitsPerComponent - targetBitsPerComponent
		for i := range samples {
			samples[i] >>= uint(n)
		}
	} else if targetBitsPerComponent > img.BitsPerComponent {
		n := targetBitsPerComponent - img.BitsPerComponent
		for i := range samples {
			samples[i] <<= uint(n)
		}
	}
	img.BitsPerComponent = targetBitsPerComponent
	if img.BitsPerComponent < 8 {
		img.resampleLowBits(samples)
		return
	}
	bytesPerLine := imageutil.BytesPerLine(int(img.Width), int(img.BitsPerComponent), img.ColorComponents)
	n := int(img.Width) * img.ColorComponents
	acquireBytes := core.AcquireBytes(bytesPerLine * int(img.Height))
	clear(acquireBytes)
	for i := 0; i < int(img.Height); i++ {
		count := i * n
		num := count + n
		if num > len(samples) {
			num = len(samples)
		}
		resampleUint32 := sampling.ResampleUint32(samples[count:num], int(targetBitsPerComponent), 8)
		size := i * bytesPerLine
		for i, length := range resampleUint32 {
			if size+i >= len(acquireBytes) {
				break
			}
			acquireBytes[size+i] = byte(length)
		}
	}
	img.Data = acquireBytes
}

func (w *PdfWriter) writeDocumentVersion() {
	if w.appendMode {
		w.writeString("\n")
	} else {
		w.writeString(fmt.Sprintf("%%PDF-%d.%d\n", w.version.Major, w.version.Minor))
		w.writeString("%âãÏÓ\n")
	}
}

type revocationEvidence struct {
	ocsps []ocspEvidence
	crls  []crlEvidence
}

func serializeObject(n int, obj core.PdfObject, crypter *core.PdfCrypt) ([]byte, error) {
	var buf bytes.Buffer
	switch v := obj.(type) {
	case *core.PdfIndirectObject:
		fmt.Fprintf(&buf, "%d 0 obj\n", n)
		obj := v.PdfObject
		if obj == nil {
			obj = core.MakeNull()
		}
		buf.Write(obj.Write())
		buf.WriteString("\nendobj\n")
	case *core.PdfObjectStream:
		fmt.Fprintf(&buf, "%d 0 obj\n", n)
		buf.Write(v.PdfObjectDictionary.Write())
		buf.WriteString("\nstream\n")
		if v.Lazy {
			if err := writeLazyStream(&buf, n, v, crypter); err != nil {
				return nil, err
			}
		} else {
			buf.Write(v.Stream)
		}
		buf.WriteString("\nendstream\nendobj\n")
	case *core.PdfObjectStreams:
		objectStreamBytes, err := buildObjectStreamBytes(int64(n), v, crypter)
		if err != nil {
			return nil, err
		}
		buf.Write(objectStreamBytes)
	default:
		return nil, fmt.Errorf("serializeIndirectObject: unsupported type %T for object %d", obj, n)
	}
	return buf.Bytes(), nil
}

const (
	ButtonTypeCheckbox ButtonType = iota
	ButtonTypePush     ButtonType = iota
	ButtonTypeRadio    ButtonType = iota
)

func (t *trustEvaluator) verifySignatureTimestamp(data, encryptedDigest []byte, certificates, certs []*x509.Certificate, evidence revocationEvidence) (time.Time, []string, error) {
	ts, err := timestamp.Parse(data)
	if err != nil {
		return time.Time{}, nil, err
	}
	if !ts.HashAlgorithm.Available() {
		return time.Time{}, nil, fmt.Errorf("unsupported hash algorithm in token")
	}
	digest := ts.HashAlgorithm.New()
	digest.Write(encryptedDigest)
	if !bytes.Equal(digest.Sum(nil), ts.HashedMessage) {
		return time.Time{}, nil, fmt.Errorf("token does not cover this signature")
	}
	p7, err := pkcs7.Parse(data)
	if err != nil {
		return time.Time{}, nil, err
	}
	if len(p7.Certificates) == 0 {
		if len(p7.Signers) != 1 {
			return time.Time{}, nil, fmt.Errorf("token must have exactly one signer")
		}
		issuerAndSerialNumber := p7.Signers[0].IssuerAndSerialNumber
		err := fmt.Errorf("TSA certificate neither in the token nor in the document")
	findTSACertificate:
		for _, certs2 := range [][]*x509.Certificate{certificates, t.sharedCertificates(certs)} {
			for _, cert := range certs2 {
				if bytes.Equal(cert.RawIssuer, issuerAndSerialNumber.IssuerName.FullBytes) && cert.SerialNumber.Cmp(issuerAndSerialNumber.SerialNumber) == 0 {
					p7.Certificates = []*x509.Certificate{cert}
					if err = p7.Verify(); err == nil {
						break findTSACertificate
					}
				}
			}
		}
		if err != nil {
			return time.Time{}, nil, err
		}
	}
	onlySigner := p7.GetOnlySigner()
	if onlySigner == nil {
		return time.Time{}, nil, fmt.Errorf("token must have exactly one signer")
	}
	if !hasExtKeyUsage(onlySigner, x509.ExtKeyUsageTimeStamping) {
		return time.Time{}, nil, fmt.Errorf("TSA certificate lacks the time stamping extended key usage")
	}
	t2 := time.Now()
	verify, err := onlySigner.Verify(x509.VerifyOptions{Roots: t.opts.Roots, Intermediates: t.intermediatePool(certs, append(append([]*x509.Certificate(nil), p7.Certificates...), certificates...)), CurrentTime: t2, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping}})
	if err != nil {
		return time.Time{}, nil, err
	}
	certs2 := verify[0]
	if t.opts.Revocation == RevocationCheckNone {
		return ts.Time, nil, nil
	}
	checkChainRevocation := t.checkChainRevocation(certs2, t2, true, evidence)
	switch checkChainRevocation.status {
	case RevocationStatusRevoked:
		return time.Time{}, nil, fmt.Errorf("TSA certificate %q revoked at %s", checkChainRevocation.revokedCert.Subject.String(), checkChainRevocation.revokedAt.UTC().Format(time.RFC3339))
	case RevocationStatusUnknown:
		if t.opts.RequireRevocationStatus {
			return time.Time{}, nil, fmt.Errorf("TSA revocation status unknown")
		}
		return ts.Time, append(checkChainRevocation.warnings, "TSA revocation status unknown"), nil
	}
	return ts.Time, checkChainRevocation.warnings, nil
}

func (r *PdfReader) newPdfAnnotationSquareFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationSquare, error) {
	square := PdfAnnotationSquare{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	square.PdfAnnotationMarkup = markup
	square.BS = dict.Get("BS")
	square.IC = dict.Get("IC")
	square.BE = dict.Get("BE")
	square.RD = dict.Get("RD")
	return &square, nil
}

func newPdfCIDFontType2FromCommon(fc *fontCommon) *pdfCIDFontType2 {
	return &pdfCIDFontType2{fontCommon: *fc}
}

// AddCRLs adds CRLs to DSS.
func (d *DSS) AddCRLs(crls [][]byte) ([]*core.PdfObjectStream, error) {
	return d.add(&d.CRLs, d.crlsMap, crls)
}

// ColorFromPdfObjects returns a new PdfColor based on the input slice of color
// components. The slice should contain a single PdfObjectFloat element in
// range 0-1.
func (cs *PdfColorspaceDeviceGray) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) != 1 {
		return nil, errors.New("range check")
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objects)
	if err != nil {
		return nil, err
	}
	return cs.ColorFromFloats(numbersAsFloat)
}

// Components returns the component values of the underlying color, or nil if
// the pattern has no underlying color.
func (p *PdfColorPatternType1) Components() []float64 {
	if p.Color == nil {
		return nil
	}
	return p.Color.Components()
}

func (s *PdfSignature) extractChainFromCert() ([]*x509.Certificate, error) {
	var arr *core.PdfObjectArray
	switch v := core.TraceToDirectObject(s.Cert).(type) {
	case nil:
		return nil, nil
	case *core.PdfObjectString:
		arr = core.MakeArray(v)
	case *core.PdfObjectArray:
		arr = v
	default:
		return nil, fmt.Errorf("invalid signature certificate object type: %T", v)
	}
	var buf bytes.Buffer
	for _, obj := range arr.Elements() {
		str, ok := core.GetString(obj)
		if !ok {
			return nil, fmt.Errorf("invalid certificate object type in signature certificate chain: %T", obj)
		}
		if _, err := buf.Write(str.Bytes()); err != nil {
			return nil, err
		}
	}
	return x509.ParseCertificates(buf.Bytes())
}

func pdfObjectToString(obj core.PdfObject) (string, error) {
	obj = core.TraceToDirectObject(obj)
	switch v := obj.(type) {
	case *core.PdfObjectString:
		return v.Str(), nil
	case *core.PdfObjectStream:
		stream, err := core.DecodeStream(v)
		if err != nil {
			return "", err
		}
		return string(stream), nil
	}
	return "", fmt.Errorf("invalid content stream object holder (%T)", obj)
}

func getPdfProducer() string {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	licenseKey := license.GetLicenseKey()
	if len(pdfProducer) > 0 && (licenseKey.IsLicensed() || licenseCheckDisabled) {
		return pdfProducer
	}
	return fmt.Sprintf("OpenPDF v%s (%s) - https://github.com/OpenMellon/openpdf", libraryVersion(), licenseKey.TypeToString())
}

// StructTreeRoot represents the structure tree root dictionary.
// Reference: PDF documentation chapter 14.7 Logical Structure, table 322.
type StructTreeRoot struct {
	K                 []*KDict
	IDTree            *IDTree
	ParentTree        *core.PdfObjectDictionary
	ParentTreeNextKey int64
	RoleMap           core.PdfObject
	ClassMap          *core.PdfObjectDictionary

	// Namespaces are the namespace dictionaries used within the document
	// (ISO 32000-2, PDF 2.0). Nil for PDF 1.x structure trees.
	Namespaces []*PdfStructureNamespace
	container  *core.PdfIndirectObject
	ids        []uuid.UUID
}

// GetNumComponents returns the number of color components (3 for RGB).
func (c *PdfColorDeviceRGB) GetNumComponents() int { return 3 }

const (
	crlReasonCertificateHold = 6
	crlReasonRemoveFromCRL   = 8
)

// PdfFieldChoice represents a choice field which includes scrollable list boxes and combo boxes.
type PdfFieldChoice struct {
	*PdfField
	Opt *core.PdfObjectArray
	TI  *core.PdfObjectInteger
	I   *core.PdfObjectArray
}

// PdfAnnotation3D represents 3D annotations.
// (Section 13.6.2).
type PdfAnnotation3D struct {
	*PdfAnnotation
	T3DD core.PdfObject
	T3DV core.PdfObject
	T3DA core.PdfObject
	T3DI core.PdfObject
	T3DB core.PdfObject
}

// NewGrayImageFromGoImage creates a new grayscale OpenPDF Image from a golang Image.
func (h DefaultImageHandler) NewGrayImageFromGoImage(goimg image.Image) (*Image, error) {
	bounds := goimg.Bounds()
	img := &Image{Width: int64(bounds.Dx()), Height: int64(bounds.Dy()), ColorComponents: 1, BitsPerComponent: 8}
	switch v := goimg.(type) {
	case *image.Gray:
		if len(v.Pix) != bounds.Dx()*bounds.Dy() {
			convert, err := imageutil.GrayConverter.Convert(goimg)
			if err != nil {
				return nil, err
			}
			img.Data = convert.Pix()
		} else {
			img.Data = v.Pix
		}
	case *image.Gray16:
		img.BitsPerComponent = 16
		if len(v.Pix) != bounds.Dx()*bounds.Dy()*2 {
			convert, err := imageutil.Gray16Converter.Convert(goimg)
			if err != nil {
				return nil, err
			}
			img.Data = convert.Pix()
		} else {
			img.Data = v.Pix
		}
	case imageutil.Image:
		base := v.Base()
		if base.ColorComponents == 1 {
			img.BitsPerComponent = int64(base.BitsPerComponent)
			img.Data = base.Data
			return img, nil
		}
		convert, err := imageutil.GrayConverter.Convert(goimg)
		if err != nil {
			return nil, err
		}
		img.Data = convert.Pix()
	default:
		convert, err := imageutil.GrayConverter.Convert(goimg)
		if err != nil {
			return nil, err
		}
		img.Data = convert.Pix()
	}
	return img, nil
}

var _ pdfFont = (*pdfCIDFontType2)(nil)

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

func (l *LTV) getCRLs(certs []*x509.Certificate) ([][]byte, error) {
	dataList := make([][]byte, 0, len(certs))
	for _, cert := range certs {
		for _, crlDistributionPoint := range cert.CRLDistributionPoints {
			if l.CertClient.IsCA(cert) {
				continue
			}
			request, err := l.CRLClient.MakeRequest(crlDistributionPoint, cert)
			if err != nil {
				common.Log.Debug("WARN: CRL request error: %v", err)
				continue
			}
			dataList = append(dataList, request)
		}
	}
	return dataList, nil
}

// GetRotate gets the inheritable rotate value, either from the page
// or a higher up page/pages struct.
func (p *PdfPage) GetRotate() (int64, error) {
	if p.Rotate != nil {
		return *p.Rotate, nil
	}
	parent := p.Parent
	for parent != nil {
		parentDict, ok := core.GetDict(parent)
		if !ok {
			return 0, errors.New("invalid parent objects dictionary")
		}
		if rotate := parentDict.Get("Rotate"); rotate != nil {
			rotateInt, ok := core.GetInt(rotate)
			if !ok {
				return 0, errors.New("invalid rotate value")
			}
			if rotateInt != nil {
				return int64(*rotateInt), nil
			}
			return 0, errors.New("rotate value is nil")
		}
		parent = parentDict.Get("Parent")
	}
	return 0, errors.New("rotate not defined")
}

// ToPdfObject converts the font to a PDF representation.
func (f *pdfFontType3) ToPdfObject() core.PdfObject {
	if f.primitive == nil {
		f.primitive = &core.PdfIndirectObject{}
	}
	dict := f.baseFields().asPdfObjectDictionary("Type3")
	f.primitive.PdfObject = dict
	if f.FirstChar != nil {
		dict.Set("FirstChar", f.FirstChar)
	}
	if f.LastChar != nil {
		dict.Set("LastChar", f.LastChar)
	}
	if f.Widths != nil {
		dict.Set("Widths", f.Widths)
	}
	if f.Encoding != nil {
		dict.Set("Encoding", f.Encoding)
	} else if f.encoder != nil {
		obj := f.encoder.ToPdfObject()
		if obj != nil {
			dict.Set("Encoding", obj)
		}
	}
	if f.FontBBox != nil {
		dict.Set("FontBBox", f.FontBBox)
	}
	if f.FontMatrix != nil {
		dict.Set("FontMatirx", f.FontMatrix)
	}
	if f.CharProcs != nil {
		dict.Set("CharProcs", f.CharProcs)
	}
	if f.Resources != nil {
		dict.Set("Resources", f.Resources)
	}
	return f.primitive
}

// PdfColorLab represents a color in the L*, a*, b* 3 component colorspace.
// Each component is defined in the range 0.0 - 1.0 where 1.0 is the primary intensity.
type PdfColorLab [3]float64

// Components returns the color's component values: L, A, B.
func (c *PdfColorLab) Components() []float64 { return []float64{c[0], c[1], c[2]} }

// R returns the value of the red component of the color.
func (c *PdfColorDeviceRGB) R() float64 { return c[0] }

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

func unicodeCategory(n rune) string {
	for s, rangeTable := range unicode.Categories {
		if len(s) == 2 && unicode.Is(rangeTable, n) {
			return s
		}
	}
	return "Cn"
}

func (r *PdfReader) newPdfAnnotationHighlightFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationHighlight, error) {
	highlight := PdfAnnotationHighlight{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	highlight.PdfAnnotationMarkup = markup
	highlight.QuadPoints = dict.Get("QuadPoints")
	return &highlight, nil
}

// ColorFromFloats returns a new PdfColor based on the input slice of color
// components. The slice should contain three elements representing the
// A, B and C components of the color. The values of the elements should be
// between 0 and 1.
func (cs *PdfColorspaceCalRGB) ColorFromFloats(vals []float64) (PdfColor, error) {
	if len(vals) != 3 {
		return nil, errors.New("range check")
	}
	val := vals[0]
	if val < 0.0 || val > 1.0 {
		common.Log.Debug("color out of CS range: ", val)
		return nil, ErrColorOutOfRange
	}
	f := vals[1]
	if f < 0.0 || f > 1.0 {
		common.Log.Debug("color out of CS range: ", f)
		return nil, ErrColorOutOfRange
	}
	v := vals[2]
	if v < 0.0 || v > 1.0 {
		common.Log.Debug("color out of CS range: ", v)
		return nil, ErrColorOutOfRange
	}
	calRGBColor := NewPdfColorCalRGB(val, f, v)
	return calRGBColor, nil
}

// String implements interface PdfObject.
func (a *PdfAction) String() string {
	indirect, ok := a.ToPdfObject().(*core.PdfIndirectObject)
	if ok {
		return fmt.Sprintf("%T: %s", a.context, indirect.PdfObject.String())
	}
	return ""
}

// PdfColorspaceSpecialPattern is a Pattern colorspace.
// Can be defined either as /Pattern or with an underlying colorspace [/Pattern cs].
type PdfColorspaceSpecialPattern struct {
	UnderlyingCS PdfColorspace
	container    *core.PdfIndirectObject
}

func (r *PdfReader) newPdfAnnotationTrapNetFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationTrapNet, error) {
	trapNet := PdfAnnotationTrapNet{}
	return &trapNet, nil
}

// PdfColorspace interface defines the common methods of a PDF colorspace.
// The colorspace defines the data storage format for each color and color representation.
//
// Device based colorspace, specified by name
// - /DeviceGray
// - /DeviceRGB
// - /DeviceCMYK
//
// CIE based colorspace specified by [name, dictionary]
// - [/CalGray dict]
// - [/CalRGB dict]
// - [/Lab dict]
// - [/ICCBased dict]
//
// Special colorspaces
// - /Pattern
// - /Indexed
// - /Separation
// - /DeviceN
//
// Work is in progress to support all colorspaces. At the moment ICCBased color spaces fall back to the alternate
// colorspace which works OK in most cases. For full color support, will need fully featured ICC support.
type PdfColorspace interface {
	// String returns the PdfColorspace's name.
	String() string

	// ImageToRGB converts an Image in a given PdfColorspace to an RGB image.
	ImageToRGB(Image) (Image, error)

	// ColorToRGB converts a single color in a given PdfColorspace to an RGB color.
	ColorToRGB(col PdfColor) (PdfColor, error)

	// GetNumComponents returns the number of components in the PdfColorspace.
	GetNumComponents() int

	// ToPdfObject returns a PdfObject representation of the PdfColorspace.
	ToPdfObject() core.PdfObject

	// ColorFromPdfObjects returns a PdfColor in the given PdfColorspace from an array of PdfObject where each
	// PdfObject represents a numeric value.
	ColorFromPdfObjects(objs []core.PdfObject) (PdfColor, error)

	// ColorFromFloats returns a new PdfColor based on input color components for a given PdfColorspace.
	ColorFromFloats(floats []float64) (PdfColor, error)

	// DecodeArray returns the Decode array for the PdfColorSpace, i.e. the range of each component.
	DecodeArray() []float64
}

func (r *PdfReader) buildPageList(indirect *core.PdfIndirectObject, indirect2 *core.PdfIndirectObject, set map[core.PdfObject]struct{}) error {
	if indirect == nil {
		return nil
	}
	if _, ok := set[indirect]; ok {
		common.Log.Debug("Cyclic recursion, skipping (%v)", indirect.ObjectNumber)
		return nil
	}
	set[indirect] = struct{}{}
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return errors.New("node not a dictionary")
	}
	name, ok := (*dict).Get("Type").(*core.PdfObjectName)
	if !ok {
		if dict.Get("Kids") == nil {
			return errors.New("node missing Type (Required)")
		}
		common.Log.Debug("ERROR: node missing Type, but has Kids. Assuming Pages node.")
		name = core.MakeName("Pages")
		dict.Set("Type", name)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("buildPageList node type: %s (%+v)", *name, indirect)
	}
	if *name == "Page" {
		page, err := r.newPdfPageFromDict(dict)
		if err != nil {
			return err
		}
		page.setContainer(indirect)
		if indirect2 != nil {
			dict.Set("Parent", indirect2)
		}
		r.pageObjs = append(r.pageObjs, indirect)
		r.PageList = append(r.PageList, page)
		return nil
	}
	if *name != "Pages" {
		if r.parser.Opts != nil && r.parser.Opts.RelaxedMode {
			common.Log.Warning("Warning: Table of content containing non Page/Pages object! (%s)", name)
			return nil
		}
		common.Log.Debug("ERROR: Table of content containing non Page/Pages object! (%s)", name)
		return errors.New("table of content containing non Page/Pages object")
	}
	if indirect2 != nil {
		dict.Set("Parent", indirect2)
	}
	if !r.lazy {
		err := r.traverseObjectData(indirect)
		if err != nil {
			return err
		}
	}
	resolve, err := r.parser.Resolve(dict.Get("Kids"))
	if err != nil {
		common.Log.Debug("ERROR: Failed loading Kids object")
		return err
	}
	var arr *core.PdfObjectArray
	arr, ok = resolve.(*core.PdfObjectArray)
	if !ok {
		indirect, isIndirect := resolve.(*core.PdfIndirectObject)
		if !isIndirect {
			return errors.New("invalid Kids object")
		}
		arr, ok = indirect.PdfObject.(*core.PdfObjectArray)
		if !ok {
			return errors.New("invalid Kids indirect object")
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Kids: %s", arr)
	}
	for i, obj := range arr.Elements() {
		indirect2, ok := core.GetIndirect(obj)
		if !ok {
			common.Log.Debug("ERROR: Page not indirect object - (%s)", indirect2)
			return errors.New("page not indirect object")
		}
		arr.Set(i, indirect2)
		err = r.buildPageList(indirect2, indirect, set)
		if err != nil {
			return err
		}
	}
	return nil
}

// SignatureValidationOpts configures the certificate trust checks that
// PdfReader.ValidateSignaturesWithOpts performs after a handler has verified
// the integrity of a signature. Trust anchors always come from the caller;
// certificates embedded in the document are used as intermediates only.
type SignatureValidationOpts struct {
	// Roots is the set of trusted anchors. When nil no trust check is
	// performed and SignatureValidationResult.IsTrusted stays false. Use
	// the authorities you accept for document signing; the system pool
	// would make the holder of any TLS server certificate a trusted signer.
	Roots *x509.CertPool

	// Intermediates supplements the certificates embedded in the document
	// when building the chain from the signer to a root.
	Intermediates []*x509.Certificate

	// ValidationTime is the instant the chain and revocation data are
	// evaluated at. Zero selects the time asserted by a signature timestamp
	// token whose TSA chains to Roots, else the current time. A document
	// timestamp is judged at the current time, never at the time its own
	// token asserts. The signature dictionary's /M date is never used
	// because the signer chooses it; a caller who accepts it can pass it
	// here.
	ValidationTime time.Time

	// Revocation selects how the revocation status of the chain is
	// established. The default performs no revocation check.
	Revocation RevocationCheck

	// RequireRevocationStatus makes an unknown revocation status fail
	// trust, with the reason in TrustErrors. By default an unknown status
	// leaves IsTrusted unchanged and is reported in TrustWarnings.
	RequireRevocationStatus bool

	// CRLClient and OCSPClient perform the online lookups of
	// RevocationCheckOnline. nil selects the sigutil defaults, which use a
	// 5 second timeout per request.
	CRLClient  *sigutil.CRLClient
	OCSPClient *sigutil.OCSPClient
}

func partitionObjectNumbers(writer *PdfWriter, layout *linearizationLayout, objByNum map[int64]core.PdfObject) (nums, nums2 []int64) {
	for _, mainSection := range layout.mainSection {
		obj := objByNum[mainSection]
		if obj == nil {
			nums2 = append(nums2, mainSection)
			continue
		}
		if canStoreInObjectStream(obj, writer) {
			nums = append(nums, mainSection)
		} else {
			nums2 = append(nums2, mainSection)
		}
	}
	return nums, nums2
}

func newPdfColorspaceSpecialPatternFromPdfObject(obj core.PdfObject) (*PdfColorspaceSpecialPattern, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("New Pattern CS from obj: %s %T", obj.String(), obj)
	}
	specialPatternCS := NewPdfColorspaceSpecialPattern()
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		specialPatternCS.container = indirect
	}
	obj = core.TraceToDirectObject(obj)
	if name, ok := obj.(*core.PdfObjectName); ok {
		if *name != "Pattern" {
			return nil, fmt.Errorf("invalid name")
		}
		return specialPatternCS, nil
	}
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		common.Log.Error("Invalid Pattern CS Object: %#v", obj)
		return nil, fmt.Errorf("invalid Pattern CS object")
	}
	if arr.Len() != 1 && arr.Len() != 2 {
		common.Log.Error("Invalid Pattern CS array: %#v", arr)
		return nil, fmt.Errorf("invalid Pattern CS array")
	}
	obj = arr.Get(0)
	if name, ok := obj.(*core.PdfObjectName); ok {
		if *name != "Pattern" {
			common.Log.Error("Invalid Pattern CS array name: %#v", name)
			return nil, fmt.Errorf("invalid name")
		}
	}
	if arr.Len() > 1 {
		obj = arr.Get(1)
		obj = core.TraceToDirectObject(obj)
		cs, err := NewPdfColorspaceFromPdfObject(obj)
		if err != nil {
			return nil, err
		}
		specialPatternCS.UnderlyingCS = cs
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Returning Pattern with underlying cs: %T", specialPatternCS.UnderlyingCS)
	}
	return specialPatternCS, nil
}

func (f *pdfFontSimple) addEncoding() error {
	var (
		s           string
		glyphByCode map[textencoding.CharCode]textencoding.GlyphName
		enc         textencoding.SimpleEncoder
	)
	if f.Encoder() != nil {
		enc, ok := f.Encoder().(textencoding.SimpleEncoder)
		if ok && enc != nil {
			s = enc.BaseName()
		}
	}
	if f.Encoding != nil {
		fontEncoding, code, err := f.getFontEncoding()
		if err != nil {
			common.Log.Debug("ERROR: BaseFont=%q Subtype=%q Encoding=%s (%T) err=%v", f.baseFont, f.subtype, f.Encoding, f.Encoding, err)
			return err
		}
		if fontEncoding != "" {
			s = fontEncoding
		}
		glyphByCode = code
		enc, err = textencoding.NewSimpleTextEncoder(s, glyphByCode)
		if err != nil {
			return err
		}
	}
	if enc == nil {
		fontDescriptor := f.fontDescriptor
		if fontDescriptor != nil {
			switch f.subtype {
			case "Type1":
				if fontDescriptor.fontFile != nil && fontDescriptor.encoder != nil {
					common.Log.Debug("Using fontFile")
					enc = fontDescriptor.encoder
				}
			case "TrueType":
				if fontDescriptor.ttf != nil {
					common.Log.Debug("Using FontFile2")
					encoder, err := fontDescriptor.ttf.MakeEncoder()
					if err == nil {
						enc = encoder
					}
					if f.toUnicodeCMap == nil {
						f.toUnicodeCMap = fontDescriptor.ttf.MakeToUnicode()
					}
				}
			}
		}
	}
	if enc != nil {
		if glyphByCode != nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("differences=%+v font=%s", glyphByCode, f.baseFields())
			}
			enc = textencoding.ApplyDifferences(enc, glyphByCode)
		}
		f.SetEncoder(enc)
	}
	return nil
}

// NewPdfAnnotationPopup returns a new popup annotation.
func NewPdfAnnotationPopup() *PdfAnnotationPopup {
	annot := NewPdfAnnotation()
	popup := &PdfAnnotationPopup{}
	popup.PdfAnnotation = annot
	annot.SetContext(popup)
	return popup
}

// ColorToRGB converts a CalGray color to an RGB color.
func (cs *PdfColorspaceCalGray) ColorToRGB(color PdfColor) (PdfColor, error) {
	calGrayColor, ok := color.(*PdfColorCalGray)
	if !ok {
		common.Log.Debug("Input color not cal gray")
		return nil, errors.New("type check error")
	}
	ANorm := calGrayColor.Val()
	X := cs.WhitePoint[0] * math.Pow(ANorm, cs.Gamma)
	Y := cs.WhitePoint[1] * math.Pow(ANorm, cs.Gamma)
	Z := cs.WhitePoint[2] * math.Pow(ANorm, cs.Gamma)
	f := 3.240479*X + -1.537150*Y + -0.498535*Z
	v := -0.969256*X + 1.875992*Y + 0.041556*Z
	val := 0.055648*X + -0.204043*Y + 1.057311*Z
	f = math.Min(math.Max(f, 0), 1.0)
	v = math.Min(math.Max(v, 0), 1.0)
	val = math.Min(math.Max(val, 0), 1.0)
	return NewPdfColorDeviceRGB(f, v, val), nil
}

func (f *PdfField) inherit(fn func(*PdfField) bool) (bool, error) {
	set := map[*PdfField]bool{}
	ok := false
	field := f
	for field != nil {
		if _, ok := set[field]; ok {
			return false, errors.New("recursive traversal")
		}
		found := fn(field)
		if found {
			ok = true
			break
		}
		set[field] = true
		field = field.Parent
	}
	return ok, nil
}

// ToImage converts an object to an Image which can be transformed or saved out.
// The image data is decoded and the Image returned.
func (x *XObjectImage) ToImage() (*Image, error) {
	img := &Image{}
	if x.Height == nil {
		return nil, errors.New("height attribute missing")
	}
	img.Height = *x.Height
	if x.Width == nil {
		return nil, errors.New("width attribute missing")
	}
	img.Width = *x.Width
	if x.BitsPerComponent == nil {
		switch x.Filter.(type) {
		case *core.CCITTFaxEncoder, *core.JBIG2Encoder:
			img.BitsPerComponent = 1
		case *core.LZWEncoder, *core.RunLengthEncoder:
			img.BitsPerComponent = 8
		default:
			return nil, errors.New("bits per component missing")
		}
	} else {
		img.BitsPerComponent = *x.BitsPerComponent
	}
	img.ColorComponents = x.ColorSpace.GetNumComponents()
	x.primitive.Set("ColorComponents", core.MakeInteger(int64(img.ColorComponents)))
	stream, err := core.DecodeStream(x.primitive)
	if err != nil {
		return nil, err
	}
	img.Data = stream
	if x.Decode != nil {
		arr, ok := x.Decode.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Invalid Decode object")
			return nil, errors.New("invalid type")
		}
		floats, err := arr.ToFloat64Array()
		if err != nil {
			return nil, err
		}
		switch x.ColorSpace.(type) {
		case *PdfColorspaceSpecialIndexed:
			if normalizeIndexedDecode, ok := imageutil.NormalizeIndexedDecode(floats, int(img.BitsPerComponent)); ok {
				img.decode = normalizeIndexedDecode
			}
		case *PdfColorspaceDeviceCMYK:
			array := x.ColorSpace.DecodeArray()
			if array[0] == floats[0] && array[1] == floats[1] && array[2] == floats[2] && array[3] == floats[3] {
				img.decode = floats
			}
		default:
			img.decode = floats
		}
	}
	return img, nil
}

// ConvertToBinary converts current image into binary (bi-level) format.
// Binary images are composed of single bits per pixel (only black or white).
// If provided image has more color components, then it would be converted into binary image using
// histogram auto threshold function.
func (img *Image) ConvertToBinary() error {
	if img.ColorComponents == 1 && img.BitsPerComponent == 1 {
		return nil
	}
	img2, err := img.ToGoImage()
	if err != nil {
		return err
	}
	convert, err := imageutil.MonochromeConverter.Convert(img2)
	if err != nil {
		return err
	}
	img.Data = convert.Base().Data
	img.alpha, err = imageutil.ScaleAlphaToMonochrome(img.alpha, int(img.Width), int(img.Height))
	if err != nil {
		return err
	}
	img.BitsPerComponent = 1
	img.ColorComponents = 1
	img.decode = nil
	img.clearSourceMetadata()
	return nil
}

func fieldWidgets(field *PdfField) []*PdfAnnotationWidget {
	widgets := append([]*PdfAnnotationWidget(nil), field.Annotations...)
	if signatureField, ok := field.GetContext().(*PdfFieldSignature); ok && signatureField.PdfAnnotationWidget != nil {
		ok := false
		for _, widget := range widgets {
			if widget == signatureField.PdfAnnotationWidget {
				ok = true
				break
			}
		}
		if !ok {
			widgets = append(widgets, signatureField.PdfAnnotationWidget)
		}
	}
	return widgets
}

// Components returns the component values of the underlying color, or nil if
// the pattern has no underlying color.
func (p *PdfColorPatternType3) Components() []float64 {
	if p.Color == nil {
		return nil
	}
	return p.Color.Components()
}

// SetOtfType sets the OTF type information
func (f *pdfCIDFontType0) SetOtfType(otfType *fonts.OtfType) { f.otfType = otfType }

// SetContext sets the sub action (context).
func (a *PdfAction) SetContext(ctx PdfModel) { a.context = ctx }

// SetObjAttrib adds an object attribute to the structure tag info.
// This is commonly used to associate annotations with structure elements.
func (t *StructureTagInfo) SetObjAttrib(obj core.PdfObject) {
	if t.AnnotObjs == nil {
		t.AnnotObjs = make([]core.PdfObject, 0)
	}
	t.AnnotObjs = append(t.AnnotObjs, obj)
}
