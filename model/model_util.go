package model

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"fmt"
	_ "image/gif"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
	"github.com/OpenMellon/unitype"
	"github.com/unidoc/typesetting/shaping"
)

func hasFieldKeys(dict *core.PdfObjectDictionary) bool {
	for _, name := range dict.Keys() {
		if _, ok := fieldDictKeys[name.String()]; ok {
			return true
		}
	}
	return false
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
		if slices.ContainsFunc(documentSigningEKUOIDs, unknownExtKeyUsage.Equal) {
			return true
		}
	}
	return false
}

func hasExtKeyUsage(cert *x509.Certificate, usage x509.ExtKeyUsage) bool {
	return slices.Contains(cert.ExtKeyUsage, usage)
}

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

func setAnnotationsAppearanceState(field *PdfField, obj core.PdfObject) {
	for _, annotation := range field.Annotations {
		annotation.AS = obj
		annotation.ToPdfObject()
	}
}

// SetPdfProducer sets the Producer attribute of the output PDF.
func SetPdfProducer(producer string) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	pdfProducer = producer
}

// SetPdfSubject sets the Subject attribute of the output PDF.
func SetPdfSubject(subject string) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	pdfSubject = subject
}

// SetPdfTitle sets the Title attribute of the output PDF.
func SetPdfTitle(title string) { metadataMu.Lock(); defer metadataMu.Unlock(); pdfTitle = title }

// SetPdfAuthor sets the Author attribute of the output PDF.
func SetPdfAuthor(author string) { metadataMu.Lock(); defer metadataMu.Unlock(); pdfAuthor = author }

// SetPdfCreator sets the Creator attribute of the output PDF.
func SetPdfCreator(creator string) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	pdfCreator = creator
}

// SetPdfCreationDate sets the CreationDate attribute of the output PDF.
func SetPdfCreationDate(creationDate time.Time) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	pdfCreationDate = creationDate
}

// SetPdfKeywords sets the Keywords attribute of the output PDF.
func SetPdfKeywords(keywords string) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	pdfKeywords = keywords
}

// SetPdfModifiedDate sets the ModDate attribute of the output PDF.
func SetPdfModifiedDate(modifiedDate time.Time) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	pdfModifiedDate = modifiedDate
}

// GetAlphabet returns a map of the runes in `text` and their frequencies.
func GetAlphabet(text string) map[rune]int {
	table := map[rune]int{}
	for _, n := range text {
		table[n]++
	}
	return table
}

func getPdfProducer() string {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	if len(pdfProducer) > 0 {
		return pdfProducer
	}
	return fmt.Sprintf("OpenPDF v%s - https://github.com/OpenMellon/openpdf", libraryVersion())
}

func getPdfTitle() string {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	return pdfTitle
}

func getPdfSubject() string { metadataMu.Lock(); defer metadataMu.Unlock(); return pdfSubject }

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

func getPdfAuthor() string { metadataMu.Lock(); defer metadataMu.Unlock(); return pdfAuthor }

func getPdfCreator() string {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	if len(pdfCreator) > 0 {
		return pdfCreator
	}
	return "OpenPDF - https://github.com/OpenMellon/openpdf"
}

func getCreationDate() time.Time {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	return pdfCreationDate
}

func getPdfKeywords() string {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	return pdfKeywords
}

func getModifiedDate() time.Time {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	return pdfModifiedDate
}

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

var predefinedEncodingNames = map[string]struct{}{"WinAnsiEncoding": {}, "MacRomanEncoding": {}, "MacExpertEncoding": {}, "StandardEncoding": {}}

const (
	RC4_128bit = EncryptionAlgorithm(iota)
	AES_128bit
	AES_256bit
)

func prependSubsetTag(s, s2 string) string {
	if strings.Contains(s, "+") {
		split := strings.Split(s, "+")
		if len(split) == 2 {
			s = split[1]
		}
	}
	return s2 + "+" + s
}

func sortedUniqueUint16(vals []uint16) []uint16 { slices.Sort(vals); return slices.Compact(vals) }

func sortedUniqueGlyphIndexes(gids []unitype.GlyphIndex) []unitype.GlyphIndex {
	slices.Sort(gids)
	return slices.Compact(gids)
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

func removeLazyTempFile(tempFile string) {
	if tempFile == "" {
		return
	}
	if err := os.Remove(filepath.Clean(tempFile)); err != nil {
		common.Log.Debug("Failed removing lazy temp file %q: %v", tempFile, err)
	}
}

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

func resolveColorspaceWithICC(colorSpace PdfColorspace, img *Image) PdfColorspace {
	if len(img.iccProfile) > 0 {
		if iccBasedColorspace := newICCBasedColorspaceFromProfile(img.iccProfile, colorSpace); iccBasedColorspace != nil {
			return iccBasedColorspace
		}
	}
	return colorSpace
}

func sha1Digest(data []byte) ([]byte, error) {
	digest := sha1.New()
	if _, err := io.Copy(digest, bytes.NewReader(data)); err != nil {
		return nil, err
	}
	return digest.Sum(nil), nil
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

func runesToGIDs(runes []rune, chars map[rune]fonts.GID) []uint16 {
	vals := make([]uint16, 0, len(runes))
	for _, n := range runes {
		if char, ok := chars[n]; ok {
			vals = append(vals, uint16(char))
		}
	}
	return vals
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

var (
	type1EncodingDictRegexp  = regexp.MustCompile("\\d+ dict\\s+(dup\\s+)?begin")
	type1DefLineRegexp       = regexp.MustCompile("^\\s*/(\\S+?)\\s+(.+?)\\s+def\\s*$")
	type1EncodingEntryRegexp = regexp.MustCompile("^\\s*dup\\s+(\\d+)\\s*/(\\w+?)(?:\\.\\d+)?\\s+put$")
	type1EncodingMarker      = "/Encoding 256 array"
	type1ReadonlyDefMarker   = "readonly def"
	type1EexecMarker         = "currentfile eexec"
)

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
