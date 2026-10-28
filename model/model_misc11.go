package model

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"fmt"
	_ "image/gif"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
	"github.com/OpenMellon/typesetting/shaping"
	"github.com/OpenMellon/unitype"
)

func sortedUniqueUint16(vals []uint16) []uint16 { slices.Sort(vals); return slices.Compact(vals) }

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

// StandardApplier is the interface that performs optimization of the whole PDF document.
// As a result an input document is being changed by the optimizer.
// The writer than takes back all it's parts and overwrites it.
// NOTE: This implementation is in experimental development state.
//
//	Keep in mind that it might change in the subsequent minor versions.
type StandardApplier interface {
	ApplyStandard(document *docutil.Document) error
}

// RevocationStatus is the outcome of a revocation check for a signature's
// certificate chain.
type RevocationStatus int

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

func removeLazyTempFile(tempFile string) {
	if tempFile == "" {
		return
	}
	if err := os.Remove(filepath.Clean(tempFile)); err != nil {
		common.Log.Debug("Failed removing lazy temp file %q: %v", tempFile, err)
	}
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

// NewStandardPdfOutputIntent creates a new standard PdfOutputIntent.
func NewStandardPdfOutputIntent(outputCondition, outputConditionIdentifier, registryName string, destOutputProfile []byte, colorComponents int) *PdfOutputIntent {
	return &PdfOutputIntent{Type: "OutputIntent", OutputCondition: outputCondition, OutputConditionIdentifier: outputConditionIdentifier, RegistryName: registryName, DestOutputProfile: destOutputProfile, ColorComponents: colorComponents, primitive: core.MakeDict()}
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

func sha1Digest(data []byte) ([]byte, error) {
	digest := sha1.New()
	if _, err := io.Copy(digest, bytes.NewReader(data)); err != nil {
		return nil, err
	}
	return digest.Sum(nil), nil
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
	slices.Sort(runes)
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

const (
	RelationshipSource FileRelationship = iota
	RelationshipData
	RelationshipAlternative
	RelationshipSupplement
	RelationshipUnspecified
)

// RevocationCheck selects the revocation data sources consulted during
// signature validation.
type RevocationCheck int

const revocationUnknownMessage = "revocation status unknown: no usable revocation data for the certificate chain"

// StdFontName represents name of a standard font.
type StdFontName = fonts.StdFontName

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

func sortedUniqueGlyphIndexes(gids []unitype.GlyphIndex) []unitype.GlyphIndex {
	slices.Sort(gids)
	return slices.Compact(gids)
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
