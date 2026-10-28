package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	_ "image/gif"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model/mdp"
)

const (
	TrappedUnknown PdfInfoTrapped = "Unknown"
	TrappedTrue    PdfInfoTrapped = "True"
	TrappedFalse   PdfInfoTrapped = "False"
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

func getPdfTitle() string {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	return pdfTitle
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

func getPdfSubject() string { metadataMu.Lock(); defer metadataMu.Unlock(); return pdfSubject }

// VariableText contains the common attributes of a variable text.
// The VariableText is typically not used directly, but is can encapsulate by PdfField
// See section 12.7.3.3 "Variable Text" and Table 222 (pp. 434-436 PDF32000_2008).
type VariableText struct {
	DA *core.PdfObjectString
	Q  *core.PdfObjectInteger
	DS *core.PdfObjectString
	RV core.PdfObject
}

// SetPdfSubject sets the Subject attribute of the output PDF.
func SetPdfSubject(subject string) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	pdfSubject = subject
}

// Write outputs the object as it is to be written to file.
func (p *PdfTransformParamsDocMDP) Write() []byte { return p.ToPdfObject().Write() }

var symbolicFontEncodings = map[string]string{"Symbol": "SymbolEncoding", "ZapfDingbats": "ZapfDingbatsEncoding"}

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

const trailerOverheadBytes = 30

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

var (
	type1EncodingDictRegexp  = regexp.MustCompile("\\d+ dict\\s+(dup\\s+)?begin")
	type1DefLineRegexp       = regexp.MustCompile("^\\s*/(\\S+?)\\s+(.+?)\\s+def\\s*$")
	type1EncodingEntryRegexp = regexp.MustCompile("^\\s*dup\\s+(\\d+)\\s*/(\\w+?)(?:\\.\\d+)?\\s+put$")
	type1EncodingMarker      = "/Encoding 256 array"
	type1ReadonlyDefMarker   = "readonly def"
	type1EexecMarker         = "currentfile eexec"
)

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

// PdfTransformParamsDocMDP represents a transform parameters dictionary for the DocMDP method and is used to detect
// modifications relative to a signature field that is signed by the author of a document.
// (Section 12.8.2.2, Table 254 - Entries in the DocMDP transform parameters dictionary p. 471 in PDF32000_2008).
type PdfTransformParamsDocMDP struct {
	Type *core.PdfObjectName
	P    *core.PdfObjectInteger
	V    *core.PdfObjectName
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

func newVRIFromDict(dict *core.PdfObjectDictionary) *VRI {
	tu, _ := core.GetString(dict.Get("TU"))
	ts, _ := core.GetString(dict.Get("TS"))
	return &VRI{Cert: streamsFromObject(dict.Get("Cert")), OCSP: streamsFromObject(dict.Get("OCSP")), CRL: streamsFromObject(dict.Get("CRL")), TU: tu, TS: ts}
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

// String returns a string representation of PdfTransformParamsDocMDP.
func (p *PdfTransformParamsDocMDP) String() string {
	return fmt.Sprintf("%s P: %s V: %s", p.Type, p.P, p.V)
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

var (
	TabOrderRow       TabOrderType = "R"
	TabOrderColumn    TabOrderType = "C"
	TabOrderStructure TabOrderType = "S"
)

// SetPdfTitle sets the Title attribute of the output PDF.
func SetPdfTitle(title string) { metadataMu.Lock(); defer metadataMu.Unlock(); pdfTitle = title }

// NewPdfTransformParamsDocMDP create a PdfTransformParamsDocMDP with the specific permissions.
func NewPdfTransformParamsDocMDP(permission mdp.DocMDPPermission) *PdfTransformParamsDocMDP {
	return &PdfTransformParamsDocMDP{Type: core.MakeName("TransformParams"), P: core.MakeInteger(int64(permission)), V: core.MakeName("1.2")}
}

// Direct returns the object itself; the transform parameters are always a direct object.
func (p *PdfTransformParamsDocMDP) Direct() core.PdfObject { return p }

// ToPdfObject implements interface PdfModel.
func (p *PdfTransformParamsDocMDP) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	dict.SetIfNotNil("Type", p.Type)
	dict.SetIfNotNil("V", p.V)
	dict.SetIfNotNil("P", p.P)
	return dict
}

// Tab order types.
type TabOrderType string

func unicodeCategory(n rune) string {
	for s, rangeTable := range unicode.Categories {
		if len(s) == 2 && unicode.Is(rangeTable, n) {
			return s
		}
	}
	return "Cn"
}
