package model

import (
	"bytes"
	"errors"
	"fmt"
	_ "image/gif"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/core/security/crypt"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
	"github.com/OpenMellon/openpdf/model/sigutil"
)

func newAppenderSnapshotReader(r io.ReaderAt, n int64, opts *ReaderOpts, ok bool) (*PdfReader, error) {
	readerOpts := NewReaderOpts()
	if opts != nil {
		*readerOpts = *opts
	}
	readerOpts.LazyLoad = true
	return newPdfReaderTracked(r, n, readerOpts, ok, true)
}

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

// NewStandardPdfOutputIntent creates a new standard PdfOutputIntent.
func NewStandardPdfOutputIntent(outputCondition, outputConditionIdentifier, registryName string, destOutputProfile []byte, colorComponents int) *PdfOutputIntent {
	return &PdfOutputIntent{Type: "OutputIntent", OutputCondition: outputCondition, OutputConditionIdentifier: outputConditionIdentifier, RegistryName: registryName, DestOutputProfile: destOutputProfile, ColorComponents: colorComponents, primitive: core.MakeDict()}
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

func newVRIFromDict(dict *core.PdfObjectDictionary) *VRI {
	tu, _ := core.GetString(dict.Get("TU"))
	ts, _ := core.GetString(dict.Get("TS"))
	return &VRI{Cert: streamsFromObject(dict.Get("Cert")), OCSP: streamsFromObject(dict.Get("OCSP")), CRL: streamsFromObject(dict.Get("CRL")), TU: tu, TS: ts}
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

// NewCustomPdfOutputIntent creates a new custom PdfOutputIntent.
func NewCustomPdfOutputIntent(outputCondition, outputConditionIdentifier, info string, destOutputProfile []byte, colorComponents int) *PdfOutputIntent {
	return &PdfOutputIntent{Type: "OutputIntent", OutputCondition: outputCondition, OutputConditionIdentifier: outputConditionIdentifier, Info: info, DestOutputProfile: destOutputProfile, primitive: core.MakeDict(), ColorComponents: colorComponents}
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

// NewDSS returns a new DSS dictionary.
func NewDSS() *DSS {
	return &DSS{primitive: core.MakeIndirectObject(core.MakeDict()), VRI: map[string]*VRI{}}
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

// NewReaderForText makes a new PdfReader for an input PDF content string. For use in testing.
func NewReaderForText(txt string) *PdfReader {
	return &PdfReader{traversed: map[core.PdfObject]struct{}{}, modelManager: newModelManager(), parser: core.NewParserFromString(txt)}
}
