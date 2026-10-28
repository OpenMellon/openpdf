package model

import (
	"bytes"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"fmt"
	_ "image/gif"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/core/security/crypt"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/internal/timeutils"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
)

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

// NewCustomPdfOutputIntent creates a new custom PdfOutputIntent.
func NewCustomPdfOutputIntent(outputCondition, outputConditionIdentifier, info string, destOutputProfile []byte, colorComponents int) *PdfOutputIntent {
	return &PdfOutputIntent{Type: "OutputIntent", OutputCondition: outputCondition, OutputConditionIdentifier: outputConditionIdentifier, Info: info, DestOutputProfile: destOutputProfile, primitive: core.MakeDict(), ColorComponents: colorComponents}
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

func decodeDSSStream(stream *core.PdfObjectStream) ([]byte, bool) {
	data, err := core.DecodeStream(stream)
	if err != nil {
		common.Log.Debug("ERROR: skipping undecodable DSS stream: %v", err)
		return nil, false
	}
	return data, true
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

func crlEvidenceFromParsed(crls []*parsedCRL) []crlEvidence {
	crls2 := make([]crlEvidence, 0, len(crls))
	for _, crl := range crls {
		crls2 = append(crls2, crlEvidence{parsedCRL: crl})
	}
	return crls2
}

// ContentStreamWrapper wraps the Page's contentstream into q ... Q blocks.
type ContentStreamWrapper interface{ WrapContentStream(page *PdfPage) error }

// SetPdfCreator sets the Creator attribute of the output PDF.
func SetPdfCreator(creator string) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	pdfCreator = creator
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

func decodeStreams(streams []*core.PdfObjectStream) [][]byte {
	dataList := make([][]byte, 0, len(streams))
	for _, stream := range streams {
		if dssStream, ok := decodeDSSStream(stream); ok {
			dataList = append(dataList, dssStream)
		}
	}
	return dataList
}

// ToPdfObject converts date to a PDF string object.
func (d *PdfDate) ToPdfObject() core.PdfObject {
	s := fmt.Sprintf("D:%.4d%.2d%.2d%.2d%.2d%.2d%c%.2d'%.2d'", d.year, d.month, d.day, d.hour, d.minute, d.second, d.utOffsetSign, d.utOffsetHours, d.utOffsetMinutes)
	return core.MakeString(s)
}

// NewPdfDateFromTime will create a PdfDate based on the given time
func NewPdfDateFromTime(timeObj time.Time) (PdfDate, error) {
	format := timeObj.Format("-07:00")
	n, _ := strconv.ParseInt(format[1:3], 10, 32)
	count, _ := strconv.ParseInt(format[4:6], 10, 32)
	return PdfDate{year: int64(timeObj.Year()), month: int64(timeObj.Month()), day: int64(timeObj.Day()), hour: int64(timeObj.Hour()), minute: int64(timeObj.Minute()), second: int64(timeObj.Second()), utOffsetSign: format[0], utOffsetHours: n, utOffsetMinutes: count}, nil
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

func getPdfCreator() string {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	if len(pdfCreator) > 0 {
		return pdfCreator
	}
	return "OpenPDF - https://github.com/OpenMellon/openpdf"
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

// SetPdfCreationDate sets the CreationDate attribute of the output PDF.
func SetPdfCreationDate(creationDate time.Time) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	pdfCreationDate = creationDate
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

func crlIsCurrent(thisUpdate, nextUpdate time.Time, cert *x509.Certificate, t time.Time, online bool) bool {
	if thisUpdate.After(cert.NotAfter) {
		return false
	}
	if nextUpdate.IsZero() {
		return online && time.Since(thisUpdate) <= maxCRLAge
	}
	return !t.After(nextUpdate)
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

func getCreationDate() time.Time {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	return pdfCreationDate
}

func decodedTextString(str *core.PdfObjectString) string {
	data := str.Bytes()
	if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
		return str.Decoded()
	}
	return str.String()
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

// NewPdfDate returns a new PdfDate object from a PDF date string (see 7.9.4 Dates).
// format: "D: YYYYMMDDHHmmSSOHH'mm"
func NewPdfDate(dateStr string) (PdfDate, error) {
	pdfTime, err := timeutils.ParsePdfTime(dateStr)
	if err != nil {
		return PdfDate{}, err
	}
	return NewPdfDateFromTime(pdfTime)
}

const (
	crlReasonCertificateHold = 6
	crlReasonRemoveFromCRL   = 8
)
