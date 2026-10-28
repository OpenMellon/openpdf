package model

import (
	"bytes"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"fmt"
	_ "image/gif"
	"io"
	"math"
	"math/bits"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unicode"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/internal/lazystream"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
	"github.com/unidoc/typesetting/font/opentype"
	"golang.org/x/xerrors"
)

func unicodeCategory(n rune) string {
	for s, rangeTable := range unicode.Categories {
		if len(s) == 2 && unicode.Is(rangeTable, n) {
			return s
		}
	}
	return "Cn"
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

func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

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

func bitsRequired(n uint32) uint16 {
	if n == 0 {
		return 1
	}
	return uint16(bits.Len32(n))
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

func cffTable(loader *opentype.Loader) ([]byte, error) {
	mustNewTag := opentype.MustNewTag("CFF ")
	rawTable, err := loader.RawTable(mustNewTag)
	if err != nil {
		return nil, fmt.Errorf("CFF table not found: %w", err)
	}
	return rawTable, nil
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

func clampToRange(floats, floats2 []float64) []float64 {
	if len(floats2) != 2*len(floats) {
		return floats
	}
	for i := range floats {
		floats[i] = math.Min(math.Max(floats[i], floats2[2*i]), floats2[2*i+1])
	}
	return floats
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

func decodeStreams(streams []*core.PdfObjectStream) [][]byte {
	dataList := make([][]byte, 0, len(streams))
	for _, stream := range streams {
		if dssStream, ok := decodeDSSStream(stream); ok {
			dataList = append(dataList, dssStream)
		}
	}
	return dataList
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

var ErrColorOutOfRange = errors.New("color out of range")

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

var documentSigningEKUOIDs = []asn1.ObjectIdentifier{{1, 3, 6, 1, 5, 5, 7, 3, 36}, {1, 3, 6, 1, 4, 1, 311, 10, 3, 12}, {1, 2, 840, 113583, 1, 1, 5}}

func encryptedWriterCatalog(writer *PdfWriter) *core.PdfIndirectObject {
	if writer.crypter == nil {
		return nil
	}
	return writer.encryptObj
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

func flattenFieldTree(field *PdfField) []*PdfField {
	fields := []*PdfField{field}
	for _, kid := range field.Kids {
		fields = append(fields, flattenFieldTree(kid)...)
	}
	return fields
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

func firstPageHintOffset(layout *linearizationLayout, table map[int64]int64, n, count int64) (uint32, error) {
	if len(layout.firstShared) == 0 {
		return 0, nil
	}
	return adjustHintOffset(table[layout.firstShared[0]], n, count)
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
