// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package pdfa provides abstraction to optimize and verify documents with respect to the PDF/A standards.
// NOTE: This implementation is in experimental development state.
//
//	Keep in mind that it might change in the subsequent minor versions.
package pdfa

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/timeutils"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/colorprofile"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
	"github.com/OpenMellon/openpdf/model/xmputil"
	"github.com/OpenMellon/openpdf/model/xmputil/pdfaextension"
	"github.com/adrg/sysfont"
)

func verifyFonts(r *model.CompliancePdfReader, std standardType, clause string) (violations []ViolatedRule) {
	var cidSystemInfoReported, cidToGIDMapReported, cMapsEmbeddedReported, cMapWModeReported, fontEmbeddedReported, symbolicTrueTypeEncodingReported, toUnicodeReported bool
	allReported := func() bool {
		return cidSystemInfoReported && cidToGIDMapReported && cMapsEmbeddedReported && cMapWModeReported && fontEmbeddedReported && symbolicTrueTypeEncodingReported && toUnicodeReported
	}
	cmaps := map[*core.PdfObjectStream]*cmap.CMap{}
	streamData := map[*core.PdfObjectStream][]byte{}
	fontByObj := map[core.PdfObject]*model.PdfFont{}
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		dict, ok := core.GetDict(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(dict.Get("Type"))
		if !ok {
			continue
		}
		if *typeName != "Font" {
			continue
		}
		font, err := model.NewPdfFontFromPdfObject(dict)
		if err != nil {
			common.Log.Debug("getting font from object failed: %v", err)
			continue
		}
		fontByObj[dict] = font
	}
	for _, page := range r.PageList {
		contentStreams, err := page.GetContentStreams()
		if err != nil {
			common.Log.Debug("Getting page content streams failed")
			continue
		}
		for _, contentStream := range contentStreams {
			contentStreamParser := contentstream.NewContentStreamParser(contentStream)
			ops, err := contentStreamParser.Parse()
			if err != nil {
				common.Log.Debug("parsing contentstream failed: %v", err)
				continue
			}
			var invisibleText bool
			for _, op := range *ops {
				if op.Operand != "Tr" {
					continue
				}
				if len(op.Params) != 1 {
					common.Log.Debug("invalid number of parameters for the 'Tr' operand, expected '1' but is: '%d'", len(op.Params))
					continue
				}
				n, ok := core.GetIntVal(op.Params[0])
				if !ok {
					common.Log.Debug("rendering mode is not an integer")
					continue
				}
				if n == 3 {
					invisibleText = true
					break
				}
			}
			for _, op := range *ops {
				if op.Operand != "Tf" {
					continue
				}
				if len(op.Params) != 2 {
					common.Log.Debug("invalid number of parameters for the 'Tf' operand, expected '2' is: '%d'", len(op.Params))
					continue
				}
				name, ok := core.GetName(op.Params[0])
				if !ok {
					common.Log.Debug("ERROR: Tf op=%s GetNameVal failed", op)
					continue
				}
				fontByName, ok2 := page.Resources.GetFontByName(*name)
				if !ok2 {
					common.Log.Debug("font not found")
					continue
				}
				fontByDict, ok := core.GetDict(fontByName)
				if !ok {
					common.Log.Debug("font dict not found")
					continue
				}
				font, ok := fontByObj[fontByDict]
				if !ok {
					var err error
					font, err = model.NewPdfFontFromPdfObject(fontByDict)
					if err != nil {
						common.Log.Debug("getting font from object: %v", err)
						continue
					}
					fontByObj[fontByDict] = font
				}
				if !cidSystemInfoReported {
					rule := verifyCIDSystemInfo(fontByDict, streamData, cmaps, clause)
					if rule != noViolation {
						violations = append(violations, rule)
						cidSystemInfoReported = true
						if allReported() {
							return violations
						}
					}
				}
				if !cidToGIDMapReported {
					rule := verifyCIDToGIDMap(fontByDict, clause)
					if rule != noViolation {
						violations = append(violations, rule)
						cidToGIDMapReported = true
						if allReported() {
							return violations
						}
					}
				}
				if !cMapsEmbeddedReported {
					rule := verifyCMapsEmbedded(fontByDict, streamData, cmaps, clause)
					if rule != noViolation {
						violations = append(violations, rule)
						cMapsEmbeddedReported = true
						if allReported() {
							return violations
						}
					}
				}
				if !cMapWModeReported {
					rule := verifyCMapWMode(fontByDict, streamData, cmaps, clause)
					if rule != noViolation {
						violations = append(violations, rule)
						cMapWModeReported = true
						if allReported() {
							return violations
						}
					}
				}
				if !fontEmbeddedReported {
					rule := verifyFontEmbedded(font, fontByDict, invisibleText, clause)
					if rule != noViolation {
						fontEmbeddedReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
				if !symbolicTrueTypeEncodingReported {
					rule := verifySymbolicTrueTypeEncoding(font, fontByDict, clause)
					if rule != noViolation {
						symbolicTrueTypeEncodingReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
				if !toUnicodeReported && (std.conformance == "A" || std.conformance == "U" || std.part == 4) {
					rule := verifyToUnicode(fontByDict, streamData, cmaps, clause)
					if rule != noViolation {
						toUnicodeReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
			}
		}
	}
	return violations
}

func verifyLinearized(r *model.CompliancePdfReader, clause string) ViolatedRule {
	if !r.IsLinearized() {
		return noViolation
	}
	if linearizationViolation := r.ValidateLinearizationTrailerID(); linearizationViolation != nil {
		return newViolatedRule(clause, "linearized PDF: "+linearizationViolation.Message)
	}
	return noViolation
}

// Conformance gets the PDF/A conformance.
func (p *profile2) Conformance() string { return p.standard.conformance }

func verifyCMapWMode(dict *core.PdfObjectDictionary, streamData map[*core.PdfObjectStream][]byte, cmaps map[*core.PdfObjectStream]*cmap.CMap, clause string) ViolatedRule {
	s := clause + ".3-4"
	const (
		detail = "For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream."
	)
	var str string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		str = subtypeName.String()
	}
	if str != "Type0" {
		return noViolation
	}
	encoding := dict.Get("Encoding")
	if _, ok := core.GetName(encoding); ok {
		return noViolation
	}
	encodingStream, ok := core.GetStream(encoding)
	if !ok {
		return newViolatedRule(s, detail)
	}
	cm, err := loadCMap(encodingStream, streamData, cmaps)
	if err != nil {
		return newViolatedRule(s, detail)
	}
	wMode, ok2 := core.GetIntVal(encodingStream.Get("WMode"))
	n, found := cm.WMode()
	if ok2 && found {
		if n != wMode {
			return newViolatedRule(s, detail)
		}
	}
	if (ok2 && !found) || (!ok2 && found) {
		return newViolatedRule(s, detail)
	}
	return noViolation
}

type xmpPropertyType struct {
	Array xmpArrayKind
	Base  xmpBaseType
}

func trimmedEqual(a, b string) bool { return strings.TrimSpace(a) == strings.TrimSpace(b) }

// Profile is the model.StandardImplementer enhanced by the information about the profile conformance level.
type Profile interface {
	model.StandardImplementer
	Conformance() string
	Part() int
}

func isRDFOrXMLName(name xml.Name) bool {
	return name.Space == rdfNamespace || name.Space == xmlNamespace || name.Space == "" || name.Space == "xmlns"
}

// VerificationError is the PDF/A verification error structure, that contains all violated rules.
type VerificationError struct {
	// ViolatedRules are the rules that were violated during error verification.
	ViolatedRules []ViolatedRule

	// ConformanceLevel defines the standard on verification failed.
	ConformanceLevel int

	// ConformanceVariant is the standard variant used on verification.
	ConformanceVariant string
}

func verifyEmbeddedFilesPDFA4(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	objectNums := r.GetObjectNums()
	for _, objectNum := range objectNums {
		obj, err := r.GetIndirectObjectByNumber(objectNum)
		if err != nil {
			continue
		}
		dict, ok := core.GetDict(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(dict.Get("Type"))
		if !ok {
			continue
		}
		if typeName.String() != "Filespec" {
			continue
		}
		filespec, err := model.NewPdfFilespecFromObj(dict)
		if err != nil {
			continue
		}
		if filespec.EF != nil {
			if filespec.F == nil || filespec.UF == nil {
				violations = append(violations, newViolatedRule("6.9-2", "The file specification dictionary for an embedded file shall contain the F and UF keys."))
				break
			}
			if filespec.AFRelationship == nil {
				violations = append(violations, newViolatedRule("6.9-4", "In order to enable identification of the relationship between the file specification dictionary and the content that is referring to it, a new (required) key has been defined and its presence (in the dictionary) is required."))
				break
			}
		}
	}
	return violations
}

// StandardName gets the name of the standard.
func (p *profile2) StandardName() string { return fmt.Sprintf("PDF/A-2%s", p.standard.conformance) }

func verifyCMapWModePDFA1(dict *core.PdfObjectDictionary, streamData map[*core.PdfObjectStream][]byte, cmaps map[*core.PdfObjectStream]*cmap.CMap) ViolatedRule {
	const (
		ruleNo = "6.3.3-4"
		detail = "For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream."
	)
	var s string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		s = subtypeName.String()
	}
	if s != "Type0" {
		return noViolation
	}
	encoding := dict.Get("Encoding")
	if _, ok := core.GetName(encoding); ok {
		return noViolation
	}
	encodingStream, ok := core.GetStream(encoding)
	if !ok {
		return newViolatedRule(ruleNo, detail)
	}
	cm, err := loadCMap(encodingStream, streamData, cmaps)
	if err != nil {
		return newViolatedRule(ruleNo, detail)
	}
	wMode, ok2 := core.GetIntVal(encodingStream.Get("WMode"))
	n, found := cm.WMode()
	if ok2 && found {
		if n != wMode {
			return newViolatedRule(ruleNo, detail)
		}
	}
	if (ok2 && !found) || (!ok2 && found) {
		return newViolatedRule(ruleNo, detail)
	}
	return noViolation
}

// ValidateStandard checks if provided input CompliancePdfReader matches rules that conforms PDF/A-1 standard.
func (p *profile1) ValidateStandard(r *model.CompliancePdfReader) error {
	verificationError := VerificationError{ConformanceLevel: p.standard.part, ConformanceVariant: p.standard.conformance}
	if rule := verifyHeaderPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyHeaderBinaryComment(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyTrailer(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyNoDataAfterEOF(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyLinearizedPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyXrefSubsectionsPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := stubDocumentInfoPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyHexStrings(r, "6.1.6"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyStreamsPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := stubIndirectObjectsPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyFiltersPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyEmbeddedFilesPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyImplementationLimitsPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyOptionalContentPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyColorspacesPDFA1(r, false); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyXObjectsPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyFormXObjectsPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyReferenceXObjectsPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyPostScriptXObjectsPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := stubClause628PDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyContentStreamOperatorsPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyFontDictionariesPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyFontsPDFA1(r, p.standard); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyExtGStatePDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyXObjectSMaskPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyTransparencyGroupPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyAnnotationsPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyActionsPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyFieldActionsPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyCatalogAdditionalActionsPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyMetadataPDFA1(r, p.standard, false); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if p.standard == standard1A() {
		if rules := verifyLogicalStructurePDFA1A(r); len(rules) != 0 {
			verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
		}
	}
	if rules := verifyInteractiveFormsPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if len(verificationError.ViolatedRules) > 0 {
		sort.Slice(verificationError.ViolatedRules, func(i, j int) bool {
			return verificationError.ViolatedRules[i].RuleNo < verificationError.ViolatedRules[j].RuleNo
		})
		return verificationError
	}
	return nil
}

func resolveItemText(s string, contentKind xmpContentKind, ok bool) (string, bool) {
	switch contentKind {
	case xmpContentValue:
		return s, false
	case xmpContentStruct:
		return "", true
	}
	return s, ok
}

// NewProfile4 creates a new Profile4 with given options.
func NewProfile4(options *Profile4Options) *Profile4 {
	if options == nil {
		options = DefaultProfile4Options()
	}
	setProfile4OptionDefaults(options)
	return &Profile4{profile4{options: *options, standard: standard4()}}
}

func parseItemContent(dec *xml.Decoder, start xml.StartElement) (s string, contentKind xmpContentKind, err error) {
	switch {
	case start.Name.Space == rdfNamespace && start.Name.Local == "value":
		s, _, err = readContent(dec)
		contentKind = xmpContentValue
	case start.Name.Space == rdfNamespace && start.Name.Local == "Description":
		s, contentKind, err = readContent(dec)
	default:
		contentKind = xmpContentStruct
		_, _, err = readContent(dec)
	}
	if err != nil {
		return s, contentKind, err
	}
	return s, contentKind, skipElement(dec, 0)
}

// DefaultProfile1Options are the default options for the Profile1.
func DefaultProfile1Options() *Profile1Options {
	return &Profile1Options{Now: time.Now, Xmp: XmpOptions{MarshalIndent: "\t"}}
}

func extensionPropertyTypes(schemas []pdfaextension.Schema) map[string]map[string]xmpPropertyType {
	propTypeBySByS := map[string]map[string]xmpPropertyType{}
	for _, schema := range schemas {
		for _, property := range schema.Property {
			if property.Name == "" {
				continue
			}
			if _, ok := propTypeBySByS[schema.NamespaceURI]; !ok {
				propTypeBySByS[schema.NamespaceURI] = map[string]xmpPropertyType{}
			}
			propTypeBySByS[schema.NamespaceURI][property.Name] = parseValueType(string(property.ValueType))
		}
	}
	return propTypeBySByS
}

var _ Profile = (*Profile4)(nil)

func fixXObjects(doc *docutil.Document, part int) error {
	seen := map[*core.PdfObjectStream]struct{}{}
	for _, object := range doc.Objects {
		objectStream, ok := core.GetStream(object)
		if !ok {
			continue
		}
		if _, ok = seen[objectStream]; ok {
			continue
		}
		seen[objectStream] = struct{}{}
		subTypeName, ok := core.GetName(objectStream.Get("SubType"))
		if !ok {
			continue
		}
		if objectStream.Get("Ref") != nil {
			objectStream.Remove("Ref")
		}
		if subTypeName.String() == "PS" {
			objectStream.Remove("PS")
			continue
		}
		if subTypeName.String() == "Form" {
			if objectStream.Get("OPI") != nil {
				objectStream.Remove("OPI")
			}
			if objectStream.Get("PS") != nil {
				objectStream.Remove("PS")
			}
			if subtype2 := objectStream.Get("Subtype2"); subtype2 != nil {
				if subtype2Name, ok := core.GetName(subtype2); ok && *subtype2Name == "PS" {
					objectStream.Remove("Subtype2")
				}
			}
			continue
		}
		if subTypeName.String() == "Image" {
			interpolate, ok := core.GetBool(objectStream.Get("Interpolate"))
			if ok && bool(*interpolate) {
				objectStream.Set("Interpolate", core.MakeBool(false))
			}
			if part == 2 {
				if objectStream.Get("OPI") != nil {
					objectStream.Remove("OPI")
				}
			}
			if objectStream.Get("Alternates") != nil {
				objectStream.Remove("Alternates")
			}
			continue
		}
	}
	return nil
}

func verifyContentStreamOperatorsPDFA1(r *model.CompliancePdfReader) ViolatedRule {
	for _, page := range r.PageList {
		contentStreams, err := page.GetContentStreams()
		if err != nil {
			continue
		}
		for _, contentStream := range contentStreams {
			contentStreamParser := contentstream.NewContentStreamParser(contentStream)
			_, err = contentStreamParser.Parse()
			if err != nil {
				return newViolatedRule("6.2.10-1", "A content stream shall not contain any operators not defined in PDF Reference even if such operators are bracketed by the BX/EX compatibility operators.")
			}
		}
	}
	return noViolation
}

// Validate checks if provided input document reader matches given PDF/A profile.
func Validate(d *model.CompliancePdfReader, profile Profile) error {
	return profile.ValidateStandard(d)
}

func verifyActionsPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_6_1_1, reported6_6_1_2 bool
	allReported := func() bool { return reported6_6_1_1 && reported6_6_1_2 }
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			common.Log.Debug("Getting object with number %d failed: %v", n, err)
			continue
		}
		dict, ok := core.GetDict(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(dict.Get("Type"))
		if !ok {
			continue
		}
		if *typeName != "Action" {
			continue
		}
		sName, ok := core.GetName(dict.Get("S"))
		if !ok {
			if !reported6_6_1_1 {
				violations = append(violations, newViolatedRule("6.6.1-1", "The Launch, Sound, Movie, ResetForm, ImportData and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and no-op actions shall not be permitted. The Hide action shall not be permitted."))
				reported6_6_1_1 = true
				if allReported() {
					return violations
				}
			}
			continue
		}
		switch model.PdfActionType(*sName) {
		case model.ActionTypeLaunch, model.ActionTypeSound, model.ActionTypeMovie, model.ActionTypeResetForm, model.ActionTypeImportData, model.ActionTypeJavaScript:
			if !reported6_6_1_1 {
				violations = append(violations, newViolatedRule("6.6.1-1", "The Launch, Sound, Movie, ResetForm, ImportData and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and no-op actions shall not be permitted. The Hide action shall not be permitted."))
				reported6_6_1_1 = true
				if allReported() {
					return violations
				}
			}
			continue
		case model.ActionTypeNamed:
			if !reported6_6_1_2 {
				nName, ok := core.GetName(dict.Get("N"))
				if !ok {
					violations = append(violations, newViolatedRule("6.6.1-2", "Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted."))
					reported6_6_1_2 = true
					if allReported() {
						return violations
					}
					continue
				}
				switch *nName {
				case "NextPage", "PrevPage", "FirstPage", "LastPage":
				default:
					violations = append(violations, newViolatedRule("6.6.1-2", "Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted."))
					reported6_6_1_2 = true
					if allReported() {
						return violations
					}
					continue
				}
			}
		}
	}
	return violations
}

func firstOutputIntentDict(doc *docutil.Document) (*core.PdfObjectDictionary, bool) {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return nil, false
	}
	outputIntentsArray, ok := core.GetArray(catalog.Object.Get("OutputIntents"))
	if !ok {
		return nil, false
	}
	if outputIntentsArray.Len() == 0 {
		return nil, false
	}
	return core.GetDict(outputIntentsArray.Get(0))
}

func fixInteractiveForm(doc *docutil.Document) error {
	for _, object := range doc.Objects {
		objectDict, ok := core.GetDict(object)
		if !ok {
			continue
		}
		typeObj := objectDict.Get("Type")
		if typeObj == nil {
			continue
		}
		if typeName, ok := core.GetName(typeObj); ok && typeName.String() != "AcroForm" {
			continue
		}
		needAppearances, ok2 := core.GetBool(objectDict.Get("NeedAppearances"))
		if ok2 && bool(*needAppearances) {
			objectDict.Set("NeedAppearances", core.MakeBool(false))
		}
		if objectDict.Get("XFA") != nil {
			objectDict.Remove("XFA")
		}
	}
	catalog, ok := doc.FindCatalog()
	if !ok {
		return errors.New("catalog not found")
	}
	if catalog.Object.Get("NeedsRendering") != nil {
		catalog.Object.Remove("NeedsRendering")
	}
	return nil
}

func newColorspaceOptimizers(cmykDefault bool, std standardType) (pageColorspaceOptimizeFunc, documentColorspaceOptimizeFunc) {
	var hasRGB, hasGray, hasCMYK bool
	optimizePage := func(doc *docutil.Document, page *docutil.Page, imgs []*docutil.Image) error {
		hasGray = true
		for _, img := range imgs {
			switch img.Colorspace {
			case "DeviceGray":
				hasGray = true
			case "DeviceRGB":
				hasRGB = true
			case "DeviceCMYK":
				hasCMYK = true
			}
		}
		contents, ok := page.GetContents()
		if !ok {
			return nil
		}
		for _, content := range contents {
			data, err := content.GetData()
			if err != nil {
				continue
			}
			contentStreamParser := contentstream.NewContentStreamParser(string(data))
			ops, err := contentStreamParser.Parse()
			if err != nil {
				continue
			}
			for _, op := range *ops {
				switch op.Operand {
				case "G", "g":
					hasGray = true
				case "RG", "rg":
					hasRGB = true
				case "K", "k":
					hasCMYK = true
				case "CS", "cs":
					if len(op.Params) == 0 {
						continue
					}
					name, ok := core.GetName(op.Params[0])
					if !ok {
						continue
					}
					switch name.String() {
					case "RGB", "DeviceRGB":
						hasRGB = true
					case "G", "DeviceGray":
						hasGray = true
					case "CMYK", "DeviceCMYK":
						hasCMYK = true
					}
				}
			}
		}
		streams := page.FindXObjectForms()
		for _, stream := range streams {
			contentStreamParser := contentstream.NewContentStreamParser(string(stream.Stream))
			ops, err := contentStreamParser.Parse()
			if err != nil {
				continue
			}
			for _, op := range *ops {
				switch op.Operand {
				case "G", "g":
					hasGray = true
				case "RG", "rg":
					hasRGB = true
				case "K", "k":
					hasCMYK = true
				case "CS", "cs":
					if len(op.Params) == 0 {
						continue
					}
					name, ok := core.GetName(op.Params[0])
					if !ok {
						continue
					}
					switch name.String() {
					case "RGB", "DeviceRGB":
						hasRGB = true
					case "G", "DeviceGray":
						hasGray = true
					case "CMYK", "DeviceCMYK":
						hasCMYK = true
					}
				}
			}
			annotsArray, ok := core.GetArray(page.Object.Get("Annots"))
			if !ok {
				return nil
			}
			for _, obj := range annotsArray.Elements() {
				dict, ok := core.GetDict(obj)
				if !ok {
					continue
				}
				c := dict.Get("C")
				if c == nil {
					continue
				}
				cArray, ok := core.GetArray(c)
				if !ok {
					continue
				}
				switch cArray.Len() {
				case 0:
				case 1:
					hasGray = true
				case 3:
					hasRGB = true
				case 4:
					hasCMYK = true
				}
			}
		}
		return nil
	}
	optimizeDocument := func(doc *docutil.Document, imgs []*docutil.Image) error {
		catalog, ok := doc.FindCatalog()
		if !ok {
			return nil
		}
		outputIntents, ok := catalog.GetOutputIntents()
		if ok && outputIntents.Len() > 0 {
			return nil
		}
		if !ok {
			outputIntents = catalog.NewOutputIntents()
		}
		if !(hasRGB || hasCMYK || hasGray) {
			return nil
		}
		defer catalog.SetOutputIntents(outputIntents)
		if hasRGB && !hasCMYK && !hasGray {
			return addSRGBOutputIntent(doc, std, outputIntents)
		}
		if hasCMYK && !hasRGB && !hasGray {
			return addCMYKOutputIntent(std, outputIntents)
		}
		if hasGray && !hasRGB && !hasCMYK {
			return addOutputIntent(std, outputIntents)
		}
		if (hasRGB && hasCMYK) || (hasRGB && hasGray) || (hasCMYK && hasGray) {
			if err := convertImagesColorspace(imgs, cmykDefault); err != nil {
				return err
			}
			if err := convertPageContentColors(doc, cmykDefault); err != nil {
				return err
			}
			if err := optimizeFormXObjectColors(doc, cmykDefault); err != nil {
				return err
			}
			if err := fixAnnotationColors(doc, cmykDefault); err != nil {
				return err
			}
			if cmykDefault {
				return addCMYKOutputIntent(std, outputIntents)
			}
			return addSRGBOutputIntent(doc, std, outputIntents)
		}
		return nil
	}
	return optimizePage, optimizeDocument
}

func verifySymbolicTrueTypeEncoding(font *model.PdfFont, dict *core.PdfObjectDictionary, clause string) ViolatedRule {
	s := clause + ".6-3"
	const (
		detail = "All symbolic TrueType fonts shall not specify an Encoding entry in the font dictionary."
	)
	var str string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		str = subtypeName.String()
	}
	if str != "TrueType" {
		return noViolation
	}
	pdfFontDescriptor := font.FontDescriptor()
	n, ok := core.GetIntVal(pdfFontDescriptor.Flags)
	if !ok {
		common.Log.Debug("flags not defined for the font descriptor")
		return newViolatedRule(s, detail)
	}
	count := (uint32(n) >> 3) & 1
	found := count != 0
	if !found {
		return noViolation
	}
	if dict.Get("Encoding") != nil {
		return newViolatedRule(s, detail)
	}
	return noViolation
}

func verifyCIDSetPDFA1(font *model.PdfFont, dict *core.PdfObjectDictionary) ViolatedRule {
	const (
		ruleNo = "6.3.5-3"
		detail = "For all CIDFont subsets referenced within a conforming file, the font descriptor dictionary shall include a CIDSet stream identifying which CIDs are present in the embedded CIDFont file, as described in PDF Reference Table 5.20."
	)
	var s string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		s = subtypeName.String()
	}
	switch s {
	case "CIDFontType0", "CIDFontType2":
		pdfFontDescriptor := font.FontDescriptor()
		if pdfFontDescriptor.CIDSet == nil {
			return newViolatedRule(ruleNo, detail)
		}
		return noViolation
	default:
		return noViolation
	}
}

// StandardName gets the name of the standard.
func (p *profile1) StandardName() string { return fmt.Sprintf("PDF/A-1%s", p.standard.conformance) }

func verifyHeaderPDFA4(r *model.CompliancePdfReader) ViolatedRule {
	if r.ParserMetadata().HeaderPosition() != 0 {
		return newViolatedRule("6.1.2-1", "header position is not at the first byte")
	}
	if r.PdfVersion().Major != 2 {
		return newViolatedRule("6.1.2-1", "File header starts at non-zero offset or does not match the pattern %PDF-2.n, where 'n' is a single digit number between 0 and 9")
	}
	if r.PdfVersion().Minor < 0 || r.PdfVersion().Minor > 9 {
		return newViolatedRule("6.1.2-1", "File header starts at non-zero offset or does not match the pattern %PDF-2.n, where 'n' is a single digit number between 0 and 9")
	}
	return noViolation
}

func isResourceParseType(start xml.StartElement) bool {
	for _, attr := range start.Attr {
		if attr.Name.Space == rdfNamespace && attr.Name.Local == "parseType" && attr.Value == "Resource" {
			return true
		}
		if !isRDFOrXMLName(attr.Name) {
			return true
		}
	}
	return false
}

// Profile3Options are the options that changes the way how optimizer may try to adapt document into PDF/A standard.
type Profile3Options struct {
	// CMYKDefaultColorSpace is an option that refers PDF/A
	CMYKDefaultColorSpace bool

	// Now is a function that returns current time.
	Now func() time.Time

	// Xmp is the xmp options information.
	Xmp XmpOptions
}

func setProfile3OptionDefaults(profile3Options *Profile3Options) {
	if profile3Options.Now == nil {
		profile3Options.Now = time.Now
	}
}

// Profile3U is the implementation of the PDF/A-3U standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type Profile3U struct{ profile3 }

func stubClause628PDFA1(r *model.CompliancePdfReader) ViolatedRule { return noViolation }
func verifyAnnotationsPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_5_2_1, reported6_5_3_1, reported6_5_3_2, reported6_5_3_4, reported6_5_3_3, reported6_6_1_3, reported6_6_2_1 bool
	allReported := func() bool {
		return reported6_5_2_1 && reported6_5_3_1 && reported6_5_3_2 && reported6_5_3_4 && reported6_5_3_3 && reported6_6_1_3 && reported6_6_2_1
	}
	for _, page := range r.PageList {
		annotations, err := page.GetAnnotations()
		if err != nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("loading annotations failed: %v", err)
			}
			continue
		}
		for _, annotation := range annotations {
			if !reported6_5_2_1 {
				switch annotation.GetContext().(type) {
				case *model.PdfAnnotationFileAttachment, *model.PdfAnnotationSound, *model.PdfAnnotationMovie, nil:
					violations = append(violations, newViolatedRule("6.5.2-1", "Annotation types not defined in PDF Reference shall not be permitted. Additionally, the FileAttachment, Sound and Movie types shall not be permitted."))
					reported6_5_2_1 = true
					if allReported() {
						return violations
					}
				}
			}
			dict, ok := core.GetDict(annotation.GetContainingPdfObject())
			if !ok {
				continue
			}
			if !reported6_5_3_1 {
				ca, ok := core.GetFloatVal(dict.Get("CA"))
				if ok && ca != 1.0 {
					violations = append(violations, newViolatedRule("6.5.3-1", "An annotation dictionary shall not contain the CA key with a value other than 1.0."))
					reported6_5_3_1 = true
					if allReported() {
						return violations
					}
				}
			}
			if !reported6_5_3_2 {
				f, ok := core.GetIntVal(dict.Get("F"))
				if !(ok && f&4 == 4 && f&1 == 0 && f&2 == 0 && f&32 == 0) {
					violations = append(violations, newViolatedRule("6.5.3-2", "An annotation dictionary shall contain the F key. The F key's Print flag bit shall be set to 1 and its Hidden, Invisible and NoView flag bits shall be set to 0."))
					reported6_5_3_2 = true
					if allReported() {
						return violations
					}
				}
			}
			if !reported6_5_3_4 {
				apDict, ok := core.GetDict(dict.Get("AP"))
				if ok {
					n := apDict.Get("N")
					if n == nil || len(apDict.Keys()) > 1 {
						violations = append(violations, newViolatedRule("6.5.3-4", "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key. If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary; otherwise the value of the N key shall be an appearance stream."))
						reported6_5_3_4 = true
						if allReported() {
							return violations
						}
						continue
					}
					_, ok := annotation.GetContext().(*model.PdfAnnotationWidget)
					if ok {
						ftName, ok := core.GetName(dict.Get("FT"))
						if ok && *ftName == "Btn" {
							if _, ok := core.GetDict(n); !ok {
								violations = append(violations, newViolatedRule("6.5.3-4", "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key. If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary; otherwise the value of the N key shall be an appearance stream."))
								reported6_5_3_4 = true
								if allReported() {
									return violations
								}
								continue
							}
						}
					}
					_, ok2 := core.GetStream(n)
					if !ok2 {
						violations = append(violations, newViolatedRule("6.5.3-4", "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key. If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary; otherwise the value of the N key shall be an appearance stream."))
						reported6_5_3_4 = true
						if allReported() {
							return violations
						}
						continue
					}
				}
			}
			if !reported6_5_3_3 {
				if dict.Get("C") != nil || dict.Get("IC") != nil {
					dict, ok := firstOutputIntentDictFromReader(r)
					if !ok {
						violations = append(violations, newViolatedRule("6.5.3-3", "An annotation dictionary shall not contain the C array or the IC array unless the color space of the DestOutputProfile in the PDF/A-1 OutputIntent dictionary, defined in 6.2.2, is RGB."))
						reported6_5_3_3 = true
						if allReported() {
							return violations
						}
					} else {
						n, ok := core.GetIntVal(dict.Get("N"))
						if !ok || n != 3 {
							violations = append(violations, newViolatedRule("6.5.3-3", "An annotation dictionary shall not contain the C array or the IC array unless the color space of the DestOutputProfile in the PDF/A-1 OutputIntent dictionary, defined in 6.2.2, is RGB."))
							reported6_5_3_3 = true
							if allReported() {
								return violations
							}
						}
					}
				}
			}
			pdfAnnotationWidget, found := annotation.GetContext().(*model.PdfAnnotationWidget)
			if !found {
				continue
			}
			if !reported6_6_1_3 {
				if pdfAnnotationWidget.A != nil {
					violations = append(violations, newViolatedRule("6.6.1-3", "A Widget annotation dictionary shall not include an A entry."))
					reported6_6_1_3 = true
					if allReported() {
						return violations
					}
				}
			}
			if !reported6_6_2_1 {
				if pdfAnnotationWidget.AA != nil {
					violations = append(violations, newViolatedRule("6.6.2-1", "A Widget annotation dictionary shall not include an AA entry for an additional-actions dictionary."))
					reported6_6_2_1 = true
					if allReported() {
						return violations
					}
				}
			}
		}
	}
	return violations
}

func setLastModified(doc *docutil.Document, t time.Time) error {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return nil
	}
	date, _ := model.NewPdfDateFromTime(t)
	if catalog.Object.Get("PieceInfo") == nil {
		dictMap := core.MakeDictMap(map[string]core.PdfObject{"LastModified": date.ToPdfObject()})
		catalog.Object.Set("PieceInfo", dictMap)
	}
	dict := core.MakeDict()
	dict.SetIfNotNil("ModDate", date.ToPdfObject())
	doc.Info = dict
	return nil
}

var _ Profile = (*Profile2B)(nil)

func addSRGBOutputIntent(doc *docutil.Document, std standardType, intents *docutil.OutputIntents) error {
	var (
		intent *model.PdfOutputIntent
		err    error
	)
	if doc.Version.Minor <= 7 {
		intent, err = colorprofile.NewSRGBv2OutputIntent(std.outputIntentSubtype())
	} else {
		intent, err = colorprofile.NewSRGBv4OutputIntent(std.outputIntentSubtype())
	}
	if err != nil {
		return err
	}
	if err = intents.Add(intent.ToPdfObject()); err != nil {
		return err
	}
	return nil
}

// StandardName gets the name of the standard.
func (p *profile4) StandardName() string { return fmt.Sprintf("PDF/A-4%s", p.standard.conformance) }

// Part gets the PDF/A version level.
func (p *profile1) Part() int { return p.standard.part }

type profile1 struct {
	standard standardType
	options  Profile1Options
}

// StandardName gets the name of the standard.
func (p *profile3) StandardName() string { return fmt.Sprintf("PDF/A-3%s", p.standard.conformance) }

func verifyInfoMatchesXMP(info *model.PdfInfo, xmp *xmputil.Document) (violations []ViolatedRule) {
	pdfInfo, ok := xmp.GetPdfInfo()
	if !ok || pdfInfo.InfoDict == nil {
		return nil
	}
	pdfInfoFromObject, err := model.NewPdfInfoFromObject(pdfInfo.InfoDict)
	if err != nil {
		return nil
	}
	for _, infoKeyRule := range infoKeyRules {
		if !infoEntryMatches(infoKeyRule.Key, info, pdfInfoFromObject) {
			violations = append(violations, newViolatedRule(infoKeyRule.RuleNo, infoXMPEquivalenceDetail(infoKeyRule.Key)))
		}
	}
	return violations
}

func verifyFormXObjectsPDFA1(r *model.CompliancePdfReader) (rule ViolatedRule) {
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(stream.Get("Type"))
		if !ok {
			continue
		}
		if *typeName != "XObject" {
			continue
		}
		subtype2Name, ok := core.GetName(stream.Get("Subtype2"))
		if !ok {
			continue
		}
		if *subtype2Name == "PS" {
			return newViolatedRule("6.2.5-1", "A form XObject dictionary shall not contain the Subtype2 key with a value of PS or the PS key.")
		}
		if stream.Get("PS") != nil {
			return newViolatedRule("6.2.5-1", "A form XObject dictionary shall not contain the Subtype2 key with a value of PS or the PS key.")
		}
	}
	return rule
}

// DefaultProfile4Options the default options for the Profile4.
func DefaultProfile4Options() *Profile4Options {
	return &Profile4Options{Now: time.Now, Xmp: XmpOptions{MarshalIndent: "\t"}}
}

// NewProfile3U creates a new Profile3U with the given options.
func NewProfile3U(options *Profile3Options) *Profile3U {
	if options == nil {
		options = DefaultProfile3Options()
	}
	setProfile3OptionDefaults(options)
	return &Profile3U{profile3{options: *options, standard: standard3U()}}
}

// Profile3B is the implementation of the PDF/A-3B standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type Profile3B struct{ profile3 }

// Conformance gets the PDF/A conformance.
func (p *profile1) Conformance() string { return p.standard.conformance }

func parseDescriptionProperties(dec *xml.Decoder) []xmpPropertyValue {
	var props []xmpPropertyValue
	for {
		tok, err := dec.Token()
		if err != nil {
			return props
		}
		switch tok := tok.(type) {
		case xml.EndElement:
			return props
		case xml.StartElement:
			prop, err := parsePropertyElement(dec, tok)
			if err != nil {
				return props
			}
			props = append(props, prop)
		}
	}
}

const (
	xmpArrayNone xmpArrayKind = iota
	xmpArraySeq
	xmpArrayBag
	xmpArrayAlt
	xmpArrayLangAlt
)

func verifyOutputIntentPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return violations
	}
	rule := newViolatedRule("6.2.2-1", "A PDF/A-1 OutputIntent is an OutputIntent dictionary, as defined by PDF Reference 9.10.4, that is included in the file's OutputIntents array and has GTS_PDFA1 as the value of its S key and a valid ICC profile stream as the value its DestOutputProfile key.")
	outputIntentsArray, ok := core.GetArray(dict.Get("OutputIntents"))
	if !ok {
		violations = append(violations, rule)
		return violations
	}
	rule2 := newViolatedRule("6.2.2-2", "If a file's OutputIntents array contains more than one entry, then all entries that contain a DestOutputProfile key shall have as the value of that key the same indirect object, which shall be a valid ICC profile stream.")
	if outputIntentsArray.Len() > 1 {
		seen := map[*core.PdfObjectDictionary]struct{}{}
		for n := 0; n < outputIntentsArray.Len(); n++ {
			dict, ok := core.GetDict(outputIntentsArray.Get(n))
			if !ok {
				violations = append(violations, rule)
				return violations
			}
			if n == 0 {
				seen[dict] = struct{}{}
				continue
			}
			if _, ok := seen[dict]; !ok {
				violations = append(violations, rule2)
				break
			}
		}
	} else if outputIntentsArray.Len() == 0 {
		violations = append(violations, rule)
		return violations
	}
	dict2, ok := core.GetDict(outputIntentsArray.Get(0))
	if !ok {
		violations = append(violations, rule)
		return violations
	}
	if sName, ok := core.GetName(dict2.Get("S")); !ok || (*sName) != "GTS_PDFA1" {
		violations = append(violations, rule)
		return violations
	}
	outputIntent, err := model.NewPdfOutputIntentFromPdfObject(dict2)
	if err != nil {
		common.Log.Debug("parsing output intent failed: %v", err)
		return violations
	}
	profileHeader, err := colorprofile.ParseHeader(outputIntent.DestOutputProfile)
	if err != nil {
		common.Log.Debug("parsing colorprofile header failed: %v", err)
		return violations
	}
	if (profileHeader.DeviceClass == colorprofile.DeviceClassPRTR || profileHeader.DeviceClass == colorprofile.DeviceClassMNTR) && (profileHeader.ColorSpace == colorprofile.ColorSpaceRGB || profileHeader.ColorSpace == colorprofile.ColorSpaceCMYK || profileHeader.ColorSpace == colorprofile.ColorSpaceGRAY) {
		return violations
	}
	violations = append(violations, rule)
	return violations
}

func verifyXObjectsPDFA23(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_2_8_1, reported6_2_9_1, reported6_2_9_1_2, reported6_2_9_2, reported6_2_8_3, reported6_2_9_3, reported6_2_8_2 bool
	seen := map[*core.PdfObjectStream]struct{}{}
	for _, n := range r.GetObjectNums() {
		if reported6_2_8_1 && reported6_2_9_1 && reported6_2_8_3 && reported6_2_9_1_2 && reported6_2_9_2 && reported6_2_9_3 && reported6_2_8_2 {
			return violations
		}
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		if _, ok = seen[stream]; ok {
			continue
		}
		seen[stream] = struct{}{}
		subTypeName, ok := core.GetName(stream.Get("SubType"))
		if !ok {
			continue
		}
		if !reported6_2_9_2 {
			if stream.Get("Ref") != nil {
				violations = append(violations, newViolatedRule("6.2.9-2", "A conforming file shall not contain any reference XObjects."))
				reported6_2_9_2 = true
			}
		}
		if subTypeName.String() == "PS" {
			if !reported6_2_9_3 {
				violations = append(violations, newViolatedRule("6.2.9-3", "A conforming file shall not contain any PostScript XObjects."))
				reported6_2_9_3 = true
				continue
			}
		}
		if subTypeName.String() == "Form" {
			if reported6_2_9_1 && reported6_2_9_1_2 && reported6_2_9_2 {
				continue
			}
			if !reported6_2_9_1 && stream.Get("OPI") != nil {
				violations = append(violations, newViolatedRule("6.2.9-1", "A form XObject dictionary shall not contain the OPI key."))
				reported6_2_9_1 = true
			}
			if !reported6_2_9_1_2 {
				if stream.Get("PS") != nil {
					reported6_2_9_1_2 = true
				}
				if subtype2 := stream.Get("Subtype2"); subtype2 != nil && !reported6_2_9_1_2 {
					if subtype2Name, ok := core.GetName(subtype2); ok && *subtype2Name == "PS" {
						reported6_2_9_1_2 = true
					}
				}
				if reported6_2_9_1_2 {
					violations = append(violations, newViolatedRule("6.2.9-1", "A form XObject dictionary shall not contain the Subtype2 key with a value of PS and the PS key."))
				}
			}
			continue
		}
		if subTypeName.String() != "Image" {
			continue
		}
		if !reported6_2_8_1 && stream.Get("Alternates") != nil {
			violations = append(violations, newViolatedRule("6.2.8-1", "An Image dictionary shall not contain the Alternates key."))
			reported6_2_8_1 = true
		}
		if !reported6_2_8_2 && stream.Get("OPI") != nil {
			violations = append(violations, newViolatedRule("6.2.8-2", "An Image dictionary shall not contain the OPI key."))
			reported6_2_8_2 = true
		}
		if !reported6_2_8_3 && stream.Get("Interpolate") != nil {
			interpolate, ok := core.GetBool(stream.Get("Interpolate"))
			if ok && bool(*interpolate) {
				continue
			}
			violations = append(violations, newViolatedRule("6.2.8-3", "If an Image dictionary contains the Interpolate key, its value shall be false."))
			reported6_2_8_3 = true
		}
	}
	return violations
}

func parseXMPProperties(data []byte) []xmpPropertyValue {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false
	var props []xmpPropertyValue
	for {
		tok, err := decoder.Token()
		if err != nil {
			return props
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Space != rdfNamespace || start.Name.Local != "Description" {
			continue
		}
		for _, attr := range start.Attr {
			if isRDFOrXMLName(attr.Name) {
				continue
			}
			props = append(props, xmpPropertyValue{Namespace: attr.Name.Space, Name: attr.Name.Local, Text: attr.Value})
		}
		props = append(props, parseDescriptionProperties(decoder)...)
	}
}

func verifyInteractiveFormsPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		dict, ok := core.GetDict(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(dict.Get("Type"))
		if !ok {
			continue
		}
		if typeName.String() != "AcroForm" {
			continue
		}
		needAppearances, ok := core.GetBool(dict.Get("NeedAppearances"))
		if !ok {
			return violations
		}
		if bool(*needAppearances) {
			violations = append(violations, newViolatedRule("6.9-1", "The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false."))
		}
	}
	return violations
}

func verifyToUnicode(dict *core.PdfObjectDictionary, streamData map[*core.PdfObjectStream][]byte, cmaps map[*core.PdfObjectStream]*cmap.CMap, clause string) ViolatedRule {
	s := clause + ".7-1"
	const (
		detail = "The font dictionary shall include a ToUnicode entry whose value is a CMap stream object that maps character codes to Unicode values, as described in PDF Reference 5.9, unless the font meets any of the following three conditions:\n - fonts that use the predefined encodings MacRomanEncoding, MacExpertEncoding or WinAnsiEncoding, or that use the predefined Identity-H or Identity-V CMaps;\n - Type 1 fonts whose character names are taken from the Adobe standard Latin character set or the set of named characters in the Symbol font, as defined in PDF Reference Appendix D;\n - Type 0 fonts whose descendant CIDFont uses the Adobe-GB1, Adobe-CNS1, Adobe-Japan1 or Adobe-Korea1 character collections."
	)
	toUnicodeStream, ok := core.GetStream(dict.Get("ToUnicode"))
	if ok {
		_, err := loadCMap(toUnicodeStream, streamData, cmaps)
		if err != nil {
			return newViolatedRule(s, detail)
		}
		return noViolation
	}
	subtypeName, ok := core.GetName(dict.Get("Subtype"))
	if !ok {
		return newViolatedRule(s, detail)
	}
	switch subtypeName.String() {
	case "Type1":
		return noViolation
	}
	return newViolatedRule(s, detail)
}

// ApplyStandard tries to change the content of the writer to match the PDF/A-4 standard.
// Implements model.StandardApplier.
func (p *profile4) ApplyStandard(document *docutil.Document) (err error) {
	ensureMinimumPDF2Version(document, 0)
	if err = setLastModified(document, p.options.Now()); err != nil {
		return err
	}
	if err = setCatalogVersion(document); err != nil {
		return err
	}
	optimizePage, optimizeDocument := newColorspaceOptimizers(p.options.CMYKDefaultColorSpace, p.standard)
	err = optimizeColorspaces(document, []pageColorspaceOptimizeFunc{optimizePage}, []documentColorspaceOptimizeFunc{optimizeDocument})
	if err != nil {
		return err
	}
	ensureDocumentID(document)
	if err = fixPermissions(document); err != nil {
		return err
	}
	if err = fixXObjects(document, p.standard.part); err != nil {
		return err
	}
	if err = fixAnnotationsPDFA23(document); err != nil {
		return err
	}
	if err = fixExtGStatePDFA23(document); err != nil {
		return err
	}
	if err = embedFonts(document); err != nil {
		return err
	}
	if err = fixInteractiveForm(document); err != nil {
		return err
	}
	if err = fixActionsPDFA4(document, p.standard); err != nil {
		return err
	}
	if err = filterCatalogAdditionalActions(document); err != nil {
		return err
	}
	if err := updateXMPMetadataPDFA4(document, p.standard, p.options.Xmp); err != nil {
		return err
	}
	if err = removeAlternatePresentations(document); err != nil {
		return err
	}
	if err = removeRequirements(document); err != nil {
		return err
	}
	return nil
}

func validateXMPProperties(data []byte, schemas []pdfaextension.Schema, useOverrides bool) bool {
	propTypeBySByS := extensionPropertyTypes(schemas)
	for _, prop := range parseXMPProperties(data) {
		propType, ok := lookupPropertyType(prop, propTypeBySByS, useOverrides)
		if !ok {
			continue
		}
		if !propertyMatchesType(prop, propType) {
			return false
		}
	}
	return true
}
func standard4F() standardType { return standardType{part: 4, conformance: "F"} }

// Profile1B is the implementation of the PDF/A-1B standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type Profile1B struct{ profile1 }

func verifyFieldActionsPDFA23(r *model.CompliancePdfReader) (rule ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return noViolation
	}
	acroFormDict, ok := core.GetDict(dict.Get("AcroForm"))
	if !ok {
		return noViolation
	}
	fieldsArray, ok := core.GetArray(acroFormDict.Get("Fields"))
	if !ok {
		return noViolation
	}
	for n := 0; n < fieldsArray.Len(); n++ {
		dict, ok := core.GetDict(fieldsArray.Get(n))
		if !ok {
			continue
		}
		if dict.Get("A") != nil {
			return newViolatedRule("6.4.1-2", "A Field dictionary shall not contain the A or AA keys.")
		}
		if dict.Get("AA") != nil {
			return newViolatedRule("6.4.1-2", "A Field dictionary shall not contain the A or AA keys.")
		}
	}
	return noViolation
}

func substringBetween(s string, start string, end string) (string, bool) {
	n := strings.Index(s, start)
	if n == -1 {
		return "", false
	}
	n += len(start)
	count := strings.Index(s[n:], end)
	if count == -1 {
		return "", false
	}
	count = n + count
	return s[n:count], true
}

func verifyEmbeddedFilesPDFA3(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	objectNums := r.GetObjectNums()
	for _, objectNum := range objectNums {
		obj, err := r.GetIndirectObjectByNumber(objectNum)
		if err != nil {
			continue
		}
		dict, ok := core.GetDict(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(dict.Get("Type"))
		if !ok {
			continue
		}
		if typeName.String() != "Filespec" {
			continue
		}
		filespec, err := model.NewPdfFilespecFromObj(dict)
		if err != nil {
			continue
		}
		if filespec.EF != nil {
			if filespec.F == nil || filespec.UF == nil {
				violations = append(violations, newViolatedRule("6.8-2", "The file specification dictionary for an embedded file shall contain the F and UF keys."))
				break
			}
			if filespec.AFRelationship == nil {
				violations = append(violations, newViolatedRule("6.8-3", "In order to enable identification of the relationship between the file specification dictionary and the content that is referring to it, a new (required) key has been defined and its presence (in the dictionary) is required."))
				break
			}
		}
	}
	return violations
}

func removeImageSoftMasks(doc *docutil.Document, page *docutil.Page, imgs []*docutil.Image) error {
	for _, img := range imgs {
		if img.SMask == nil {
			continue
		}
		xObjectImage, err := model.NewXObjectImageFromStream(img.Stream)
		if err != nil {
			return err
		}
		image, err := xObjectImage.ToImage()
		if err != nil {
			return err
		}
		image2, err := image.ToGoImage()
		if err != nil {
			return err
		}
		img2, err := imageutil.RGBAConverter.Convert(image2)
		if err != nil {
			return err
		}
		imageBase := img2.Base()
		image3 := &model.Image{Width: int64(imageBase.Width), Height: int64(imageBase.Height), BitsPerComponent: int64(imageBase.BitsPerComponent), ColorComponents: imageBase.ColorComponents, Data: imageBase.Data}
		image3.SetDecode(imageBase.Decode)
		image3.SetAlpha(imageBase.Alpha)
		if err = xObjectImage.SetImage(image3, nil); err != nil {
			return err
		}
		xObjectImage.SMask = core.MakeNull()
		var obj core.PdfObject
		n := -1
		for n, obj = range doc.Objects {
			if obj == img.SMask.Stream {
				break
			}
		}
		if n != -1 {
			doc.Objects = append(doc.Objects[:n], doc.Objects[n+1:]...)
		}
		img.SMask = nil
		xObjectImage.ToPdfObject()
	}
	return nil
}

// NewProfile2B creates a new Profile2B with the given options.
func NewProfile2B(options *Profile2Options) *Profile2B {
	if options == nil {
		options = DefaultProfile2Options()
	}
	setProfile2OptionDefaults(options)
	return &Profile2B{profile2{options: *options, standard: standard2B()}}
}

// Part gets the PDF/A version level.
func (p *profile4) Part() int { return p.standard.part }

// XmpOptions are the options used by the optimization of the XMP metadata.
type XmpOptions struct {
	// Copyright information.
	Copyright string

	// OriginalDocumentID is the original document identifier.
	// By default, if this field is empty the value is extracted from the XMP Metadata or generated UUID.
	OriginalDocumentID string

	// DocumentID is the original document identifier.
	// By default, if this field is empty the value is extracted from the XMP Metadata or generated UUID.
	DocumentID string

	// InstanceID is the original document identifier.
	// By default, if this field is empty the value is set to generated UUID.
	InstanceID string

	// NewDocumentVersion is a flag that defines if a document was overwritten.
	// If the new document was created this should be true. On changing given document file, and overwriting it it should be true.
	NewDocumentVersion bool

	// MarshalIndent defines marshaling indent of the XMP metadata.
	MarshalIndent string

	// MarshalPrefix defines marshaling prefix of the XMP metadata.
	MarshalPrefix string
}

func verifyActionsPDFA4(r *model.CompliancePdfReader, std standardType) (violations []ViolatedRule) {
	var reported6_6_1_1, reported6_6_1_2 bool
	allReported := func() bool { return reported6_6_1_1 && reported6_6_1_2 }
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			common.Log.Debug("Getting object with number %d failed: %v", n, err)
			continue
		}
		dict, ok := core.GetDict(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(dict.Get("Type"))
		if !ok {
			continue
		}
		if *typeName != "Action" {
			continue
		}
		sName, ok := core.GetName(dict.Get("S"))
		if !ok {
			if !reported6_6_1_1 {
				violations = append(violations, newViolatedRule("6.6.1-1", "The Launch, Sound, Movie, ResetForm, ImportData, Hide, Rendition, Trans, and JavaScript actions shall not be permitted. SetOCGState and GoTo3DView are allowed only in 4E profile. Additionally, the deprecated set-state and noop actions shall not be permitted."))
				reported6_6_1_1 = true
				if allReported() {
					return violations
				}
			}
			continue
		}
		switch model.PdfActionType(*sName) {
		case model.ActionTypeLaunch, model.ActionTypeSound, model.ActionTypeMovie, model.ActionTypeResetForm, model.ActionTypeImportData, model.ActionTypeJavaScript, model.ActionTypeHide, model.ActionTypeRendition, model.ActionTypeTrans:
			if !reported6_6_1_1 {
				violations = append(violations, newViolatedRule("6.6.1-1", "The Launch, Sound, Movie, ResetForm, ImportData, Hide, Rendition, Trans, and JavaScript actions shall not be permitted. SetOCGState and GoTo3DView are allowed only in 4E profile. Additionally, the deprecated set-state and noop actions shall not be permitted."))
				reported6_6_1_1 = true
				if allReported() {
					return violations
				}
			}
			continue
		case model.ActionTypeSetOCGState, model.ActionTypeGoTo3DView:
			if std.conformance != "E" {
				if !reported6_6_1_1 {
					violations = append(violations, newViolatedRule("6.6.1-1", "The Launch, Sound, Movie, ResetForm, ImportData, Hide, Rendition, Trans, and JavaScript actions shall not be permitted. SetOCGState and GoTo3DView are allowed only in 4E profile. Additionally, the deprecated set-state and noop actions shall not be permitted."))
					reported6_6_1_1 = true
					if allReported() {
						return violations
					}
				}
			}
			continue
		case model.ActionTypeNamed:
			if !reported6_6_1_2 {
				nName, ok := core.GetName(dict.Get("N"))
				if !ok {
					violations = append(violations, newViolatedRule("6.6.1-2", "Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted."))
					reported6_6_1_2 = true
					if allReported() {
						return violations
					}
					continue
				}
				switch *nName {
				case "NextPage", "PrevPage", "FirstPage", "LastPage":
				default:
					violations = append(violations, newViolatedRule("6.6.1-2", "Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted."))
					reported6_6_1_2 = true
					if allReported() {
						return violations
					}
					continue
				}
			}
		}
	}
	return violations
}

func removeAlternatePresentations(doc *docutil.Document) error {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return errors.New("catalog not found")
	}
	namesDict, ok := core.GetDict(catalog.Object.Get("Names"))
	if !ok {
		return nil
	}
	if namesDict.Get("AlternatePresentations") != nil {
		namesDict.Remove("AlternatePresentations")
	}
	return nil
}

var _ Profile = (*Profile3U)(nil)

func removeCatalogAdditionalActions(doc *docutil.Document) error {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return errors.New("catalog not found")
	}
	_, ok = core.GetDict(catalog.Object.Get("AA"))
	if !ok {
		return nil
	}
	catalog.Object.Remove("AA")
	return nil
}
func standard2B() standardType { return standardType{part: 2, conformance: "B"} }

var noViolation = ViolatedRule{}

func stubClause618PDFA23(r *model.CompliancePdfReader) []ViolatedRule { return nil }
func updateXMPMetadata(doc *docutil.Document, std standardType, xmpOptions XmpOptions) error {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return nil
	}
	var xmp *xmputil.Document
	metadata, ok := catalog.GetMetadata()
	if !ok {
		xmp = xmputil.NewDocument()
	} else {
		var err error
		xmp, err = xmputil.LoadDocument(metadata.Stream)
		if err != nil {
			return err
		}
	}
	pdfInfoOptions := xmputil.PdfInfoOptions{InfoDict: doc.Info, PdfVersion: fmt.Sprintf("%d.%d", doc.Version.Major, doc.Version.Minor), Copyright: xmpOptions.Copyright, Overwrite: true}
	markInfo, ok := catalog.GetMarkInfo()
	if ok {
		marked, ok := core.GetBool(markInfo.Get("Marked"))
		if ok && bool(*marked) {
			pdfInfoOptions.Marked = true
		}
	}
	if err := xmp.SetPdfInfo(&pdfInfoOptions); err != nil {
		return err
	}
	if err := xmp.SetPdfAID(std.part, std.conformance); err != nil {
		return err
	}
	mediaManagementOptions := xmputil.MediaManagementOptions{OriginalDocumentID: xmpOptions.OriginalDocumentID, DocumentID: xmpOptions.DocumentID, InstanceID: xmpOptions.InstanceID, NewDocumentID: !xmpOptions.NewDocumentVersion, ModifyComment: "Optimize document to PDF/A standard"}
	dict, ok := core.GetDict(doc.Info)
	if ok {
		if modDate, ok := core.GetString(dict.Get("ModDate")); ok && modDate.String() != "" {
			t, err := timeutils.ParsePdfTime(modDate.String())
			if err != nil {
				return fmt.Errorf("invalid ModDate field: %w", err)
			}
			mediaManagementOptions.ModifyDate = t
		}
	}
	if err := xmp.SetMediaManagement(&mediaManagementOptions); err != nil {
		return err
	}
	if err := xmp.SetPdfAExtension(); err != nil {
		return err
	}
	data, err := xmp.MarshalIndent(xmpOptions.MarshalPrefix, xmpOptions.MarshalIndent)
	if err != nil {
		return err
	}
	if err := catalog.SetMetadata(data); err != nil {
		return err
	}
	return nil
}

func setProfile4OptionDefaults(profile4Options *Profile4Options) {
	if profile4Options.Now == nil {
		profile4Options.Now = time.Now
	}
}

func verifyColorspacesPDFA1(r *model.CompliancePdfReader, checkOutputIntents bool) (violations []ViolatedRule) {
	var reportedUnused1, reportedUnused2, reported6_2_3_1, reported6_2_3_2, reported6_2_3_3, reported6_2_3_4, reported6_2_3_1_2 bool
	allReported := func() bool {
		return reportedUnused1 && reportedUnused2 && reported6_2_3_1 && reported6_2_3_2 && reported6_2_3_3 && reported6_2_3_4 && reported6_2_3_1_2
	}
	intent, ok := firstOutputIntent(r)
	var profileHeader colorprofile.ProfileHeader
	if ok {
		profileHeader, _ = colorprofile.ParseHeader(intent.DestOutputProfile)
	}
	var deviceColorViolation bool
	seen := map[core.PdfObject]struct{}{}
	var checkColorspace func(cs model.PdfColorspace) bool
	checkColorspace = func(cs model.PdfColorspace) bool {
		switch cs := cs.(type) {
		case *model.PdfColorspaceDeviceGray:
			if !reported6_2_3_4 {
				if !ok {
					deviceColorViolation = true
					violations = append(violations, newViolatedRule("6.2.3-4", "DeviceGray may be used only if the file has a PDF/A-1 OutputIntent."))
					reported6_2_3_4 = true
					if allReported() {
						return true
					}
				}
			}
		case *model.PdfColorspaceDeviceRGB:
			if !reported6_2_3_2 {
				if !ok || profileHeader.ColorSpace != colorprofile.ColorSpaceRGB {
					deviceColorViolation = true
					violations = append(violations, newViolatedRule("6.2.3-2", "DeviceRGB may be used only if the file has a PDF/A-1 OutputIntent that uses an RGB color space."))
					reported6_2_3_2 = true
					if allReported() {
						return true
					}
				}
			}
		case *model.PdfColorspaceDeviceCMYK:
			if !reported6_2_3_3 {
				if !ok || profileHeader.ColorSpace != colorprofile.ColorSpaceCMYK {
					deviceColorViolation = true
					violations = append(violations, newViolatedRule("6.2.3-3", "DeviceCMYK may be used only if the file has a PDF/A-1 OutputIntent that uses an CMYK color space."))
					reported6_2_3_3 = true
					if allReported() {
						return true
					}
				}
			}
		case *model.PdfColorspaceICCBased:
			if !reported6_2_3_1 || !reported6_2_3_1_2 {
				profileHeader, err := colorprofile.ParseHeader(cs.Data)
				if err != nil {
					common.Log.Debug("parsing ICCBased header failed: %v", err)
					violations = append(violations, func() ViolatedRule {
						return newViolatedRule("6.2.3-1", "All ICCBased color spaces shall be embedded as ICC profile streams as described in PDF Reference 4.5")
					}())
					reported6_2_3_1 = true
					if allReported() {
						return true
					}
				}
				if !reported6_2_3_1 {
					var badDeviceClass, badColorSpace bool
					switch profileHeader.DeviceClass {
					case colorprofile.DeviceClassPRTR, colorprofile.DeviceClassMNTR, colorprofile.DeviceClassSCNR, colorprofile.DeviceClassSPAC:
					default:
						badDeviceClass = true
					}
					switch profileHeader.ColorSpace {
					case colorprofile.ColorSpaceRGB, colorprofile.ColorSpaceCMYK, colorprofile.ColorSpaceGRAY, colorprofile.ColorSpaceLAB:
					default:
						badColorSpace = true
					}
					if badDeviceClass || badColorSpace {
						violations = append(violations, newViolatedRule("6.2.3-1", "All ICCBased color spaces shall be embedded as ICC profile streams as described in PDF Reference 4.5"))
						reported6_2_3_1 = true
						if allReported() {
							return true
						}
					}
				}
				if !reported6_2_3_1_2 {
					stream, _ := core.GetStream(cs.GetContainingPdfObject())
					if stream.Get("N") == nil || (cs.N == 1 && profileHeader.ColorSpace != colorprofile.ColorSpaceGRAY) || (cs.N == 3 && !(profileHeader.ColorSpace == colorprofile.ColorSpaceRGB || profileHeader.ColorSpace == colorprofile.ColorSpaceLAB)) || (cs.N == 4 && profileHeader.ColorSpace != colorprofile.ColorSpaceCMYK) {
						violations = append(violations, newViolatedRule("6.2.3-5", "If an uncalibrated color space is used in a file then that file shall contain a PDF/A-1 OutputIntent, as defined in 6.2.2."))
						reported6_2_3_1_2 = true
						if allReported() {
							return true
						}
					}
				}
			}
			if cs.Alternate != nil {
				return checkColorspace(cs.Alternate)
			}
		}
		return false
	}
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(stream.Get("Type"))
		if !ok || typeName.String() != "XObject" {
			continue
		}
		subtypeName, ok := core.GetName(stream.Get("Subtype"))
		if !ok {
			continue
		}
		seen[stream] = struct{}{}
		switch subtypeName.String() {
		case "Image":
			xObjectImage, err := model.NewXObjectImageFromStream(stream)
			if err != nil {
				continue
			}
			seen[stream] = struct{}{}
			if checkColorspace(xObjectImage.ColorSpace) {
				return violations
			}
		case "Form":
			groupDict, ok := core.GetDict(stream.Get("Group"))
			if !ok {
				continue
			}
			cs := groupDict.Get("CS")
			if cs == nil {
				continue
			}
			colorspace, err := model.NewPdfColorspaceFromPdfObject(cs)
			if err != nil {
				continue
			}
			if checkColorspace(colorspace) {
				return violations
			}
		}
	}
	for _, page := range r.PageList {
		contentStreams, err := page.GetContentStreams()
		if err != nil {
			continue
		}
		for _, contentStream := range contentStreams {
			ops, err := contentstream.NewContentStreamParser(contentStream).Parse()
			if err != nil {
				continue
			}
			for _, op := range *ops {
				if len(op.Params) > 1 {
					continue
				}
				switch op.Operand {
				case "BI":
					contentStreamInlineImage, ok := op.Params[0].(*contentstream.ContentStreamInlineImage)
					if !ok {
						continue
					}
					colorSpace, err := contentStreamInlineImage.GetColorSpace(page.Resources)
					if err != nil {
						continue
					}
					if checkColorspace(colorSpace) {
						return violations
					}
				case "Do":
					name, ok := core.GetName(op.Params[0])
					if !ok {
						continue
					}
					xObjectByName, xObjectType := page.Resources.GetXObjectByName(*name)
					if _, ok := seen[xObjectByName]; ok {
						continue
					}
					switch xObjectType {
					case model.XObjectTypeImage:
						xObjectImage, err := model.NewXObjectImageFromStream(xObjectByName)
						if err != nil {
							continue
						}
						seen[xObjectByName] = struct{}{}
						if checkColorspace(xObjectImage.ColorSpace) {
							return violations
						}
					case model.XObjectTypeForm:
						groupDict, ok := core.GetDict(xObjectByName.Get("Group"))
						if !ok {
							continue
						}
						csName, ok := core.GetName(groupDict.Get("CS"))
						if !ok {
							continue
						}
						colorspace, err := model.NewPdfColorspaceFromPdfObject(csName)
						if err != nil {
							continue
						}
						seen[xObjectByName] = struct{}{}
						if checkColorspace(colorspace) {
							return violations
						}
					}
				}
			}
		}
	}
	if !deviceColorViolation {
		return violations
	}
	if (profileHeader.DeviceClass == colorprofile.DeviceClassPRTR || profileHeader.DeviceClass == colorprofile.DeviceClassMNTR) && (profileHeader.ColorSpace == colorprofile.ColorSpaceRGB || profileHeader.ColorSpace == colorprofile.ColorSpaceCMYK || profileHeader.ColorSpace == colorprofile.ColorSpaceGRAY) {
		return violations
	}
	if !checkOutputIntents {
		return violations
	}
	dict, ok2 := catalogDict(r)
	if !ok2 {
		return violations
	}
	outputIntentsArray, ok2 := core.GetArray(dict.Get("OutputIntents"))
	if !ok2 {
		violations = append(violations, newViolatedRule("6.2.2-1", "A PDF/A-1 OutputIntent is an OutputIntent dictionary, as defined by PDF Reference 9.10.4, that is included in the file's OutputIntents array and has GTS_PDFA1 as the value of its S key and a valid ICC profile stream as the value its DestOutputProfile key."), newViolatedRule("6.2.2-2", "If a file's OutputIntents array contains more than one entry, then all entries that contain a DestOutputProfile key shall have as the value of that key the same indirect object, which shall be a valid ICC profile stream."))
		return violations
	}
	if outputIntentsArray.Len() > 1 {
		seen := map[*core.PdfObjectDictionary]struct{}{}
		for n := 0; n < outputIntentsArray.Len(); n++ {
			dict, ok := core.GetDict(outputIntentsArray.Get(n))
			if !ok {
				continue
			}
			if n == 0 {
				seen[dict] = struct{}{}
				continue
			}
			if _, ok := seen[dict]; !ok {
				violations = append(violations, newViolatedRule("6.2.2-2", "If a file's OutputIntents array contains more than one entry, then all entries that contain a DestOutputProfile key shall have as the value of that key the same indirect object, which shall be a valid ICC profile stream."))
				break
			}
		}
	}
	return violations
}

func parseArrayItems(dec *xml.Decoder) (items []xmpArrayItem, ok bool, err error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return items, ok, err
		}
		switch tok := tok.(type) {
		case xml.EndElement:
			return items, ok, nil
		case xml.StartElement:
			if tok.Name.Space != rdfNamespace || tok.Name.Local != "li" {
				if err := skipElement(dec, 0); err != nil {
					return items, ok, err
				}
				continue
			}
			for _, attr := range tok.Attr {
				if attr.Name.Space == xmlNamespace && attr.Name.Local == "lang" {
					ok = true
				}
			}
			item := xmpArrayItem{Struct: isResourceParseType(tok)}
			s, contentKind, err := readContent(dec)
			if err != nil {
				return items, ok, err
			}
			item.Text, item.Struct = resolveItemText(s, contentKind, item.Struct)
			items = append(items, item)
		}
	}
}

func convertContentStreamColors(res *model.PdfPageResources, ops *contentstream.ContentStreamOperations, cmykDefault bool) ([]byte, error) {
	var changed bool
	for _, op := range *ops {
	opSwitch:
		switch op.Operand {
		case "BI":
			contentStreamInlineImage, ok := op.Params[0].(*contentstream.ContentStreamInlineImage)
			if !ok {
				break
			}
			colorSpace, err := contentStreamInlineImage.GetColorSpace(res)
			if err != nil {
				return nil, err
			}
			switch colorSpace.(type) {
			case *model.PdfColorspaceDeviceCMYK:
				if cmykDefault {
					break opSwitch
				}
			case *model.PdfColorspaceDeviceGray:
			case *model.PdfColorspaceDeviceRGB:
				if !cmykDefault {
					break opSwitch
				}
			default:
				break opSwitch
			}
			changed = true
			image, err := contentStreamInlineImage.ToImage(res)
			if err != nil {
				return nil, err
			}
			image2, err := image.ToGoImage()
			if err != nil {
				return nil, err
			}
			if cmykDefault {
				image2, err = imageutil.CMYKConverter.Convert(image2)
			} else {
				image2, err = imageutil.NRGBAConverter.Convert(image2)
			}
			if err != nil {
				return nil, err
			}
			img, ok := image2.(imageutil.Image)
			if !ok {
				return nil, errors.New("image doesn't implement imageutil.Image")
			}
			imageBase := img.Base()
			image3 := model.Image{Width: int64(imageBase.Width), Height: int64(imageBase.Height), BitsPerComponent: int64(imageBase.BitsPerComponent), ColorComponents: imageBase.ColorComponents, Data: imageBase.Data}
			image3.SetDecode(imageBase.Decode)
			image3.SetAlpha(imageBase.Alpha)
			encoder, err := contentStreamInlineImage.GetEncoder()
			if err != nil {
				encoder = core.NewFlateEncoder()
			}
			inlineImage, err := contentstream.NewInlineImageFromImage(image3, encoder)
			if err != nil {
				return nil, err
			}
			op.Params[0] = inlineImage
		case "G", "g":
			if len(op.Params) != 1 {
				break
			}
			f, err := core.GetNumberAsFloat(op.Params[0])
			if err != nil {
				break
			}
			if cmykDefault {
				op.Params = []core.PdfObject{core.MakeFloat(0), core.MakeFloat(0), core.MakeFloat(0), core.MakeFloat(1 - f)}
				operand := "K"
				if op.Operand == "g" {
					operand = "k"
				}
				op.Operand = operand
			} else {
				op.Params = []core.PdfObject{core.MakeFloat(f), core.MakeFloat(f), core.MakeFloat(f)}
				operand := "RG"
				if op.Operand == "g" {
					operand = "rg"
				}
				op.Operand = operand
			}
			changed = true
		case "RG", "rg":
			if !cmykDefault {
				break
			}
			if len(op.Params) != 3 {
				break
			}
			nums, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				break
			}
			changed = true
			red, green, blue := nums[0], nums[1], nums[2]
			c, m, y, k := color.RGBToCMYK(uint8(red*255), uint8(green*255), uint8(255*blue))
			op.Params = []core.PdfObject{core.MakeFloat(float64(c) / 255), core.MakeFloat(float64(m) / 255), core.MakeFloat(float64(y) / 255), core.MakeFloat(float64(k) / 255)}
			operand := "K"
			if op.Operand == "rg" {
				operand = "k"
			}
			op.Operand = operand
		case "K", "k":
			if cmykDefault {
				break
			}
			if len(op.Params) != 4 {
				break
			}
			nums, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				break
			}
			cyan, magenta, yellow, black := nums[0], nums[1], nums[2], nums[3]
			red, green, blue := color.CMYKToRGB(uint8(255*cyan), uint8(255*magenta), uint8(255*yellow), uint8(255*black))
			op.Params = []core.PdfObject{core.MakeFloat(float64(red) / 255), core.MakeFloat(float64(green) / 255), core.MakeFloat(float64(blue) / 255)}
			operand := "RG"
			if op.Operand == "k" {
				operand = "rg"
			}
			op.Operand = operand
			changed = true
		}
	}
	if !changed {
		return nil, nil
	}
	contentCreator := contentstream.NewContentCreator()
	for _, op := range *ops {
		contentCreator.AddOperand(*op)
	}
	data := contentCreator.Bytes()
	return data, nil
}

// Part gets the PDF/A version level.
func (p *profile2) Part() int { return p.standard.part }

// Conformance gets the PDF/A conformance.
func (p *profile3) Conformance() string { return p.standard.conformance }

func verifyAnnotationsPDFA4(r *model.CompliancePdfReader, std standardType) (violations []ViolatedRule) {
	var reported6_3_1_1, reported6_3_2_1, reported6_3_2_2, reported6_3_2_3, reported6_3_3_2, reported6_4_1_1, reported6_4_1_1_2 bool
	allReported := func() bool {
		return reported6_3_1_1 && reported6_3_2_1 && reported6_3_2_2 && reported6_3_2_3 && reported6_3_3_2 && reported6_4_1_1 && reported6_4_1_1_2
	}
	for _, page := range r.PageList {
		annotations, err := page.GetAnnotations()
		if err != nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("loading annotations failed: %v", err)
			}
			continue
		}
		for _, annotation := range annotations {
			if !reported6_3_1_1 {
				switch annotation.GetContext().(type) {
				case *model.PdfAnnotationScreen, *model.PdfAnnotationSound, *model.PdfAnnotationMovie, nil:
					violations = append(violations, newViolatedRule("6.3.1-1", "Annotation types not defined in PDF Reference shall not be permitted. Additionally, the Sound, Screen and Movie types shall not be permitted."))
					reported6_3_1_1 = true
					if allReported() {
						return violations
					}
				case *model.PdfAnnotationFileAttachment:
					if std != standard4F() {
						violations = append(violations, newViolatedRule("6.3.1-1", "Annotation types not defined in PDF Reference shall not be permitted. Additionally, the FileAttachment, Sound, Screen and Movie types shall not be permitted."))
						reported6_3_1_1 = true
						if allReported() {
							return violations
						}
					}
				case *model.PdfAnnotation3D, *model.PdfAnnotationRichMedia:
					if std != standard4E() {
						violations = append(violations, newViolatedRule("6.3.1-1", "Annotation types not defined in PDF Reference shall not be permitted. Additionally, the 3D, RichMedia, Sound, Screen and Movie types shall not be permitted."))
						reported6_3_1_1 = true
						if allReported() {
							return violations
						}
					}
				}
			}
			dict, ok := core.GetDict(annotation.GetContainingPdfObject())
			if !ok {
				continue
			}
			_, found := annotation.GetContext().(*model.PdfAnnotationPopup)
			if !found && !reported6_3_2_1 {
				_, ok := core.GetIntVal(dict.Get("F"))
				if !ok {
					violations = append(violations, newViolatedRule("6.3.2-1", "Except for annotation dictionaries whose Subtype value is Popup, all annotation dictionaries shall contain the F key."))
					reported6_3_2_1 = true
					if allReported() {
						return violations
					}
				}
			}
			if !reported6_3_2_2 {
				f, ok := core.GetIntVal(dict.Get("F"))
				if ok && !(f&4 == 4 && f&1 == 0 && f&2 == 0 && f&32 == 0 && f&256 == 0) {
					violations = append(violations, newViolatedRule("6.3.2-2", "If present, the F key's Print flag bit shall be set to 1 and its Hidden, Invisible, ToggleNoView, and NoView flag bits shall be set to 0."))
					reported6_3_2_2 = true
					if allReported() {
						return violations
					}
				}
			}
			_, ok2 := annotation.GetContext().(*model.PdfAnnotationText)
			if ok2 && !reported6_3_2_3 {
				f, ok := core.GetIntVal(dict.Get("F"))
				if ok && !(f&8 == 8 && f&16 == 16) {
					violations = append(violations, newViolatedRule("6.3.2-3", "Text annotation has one of the flags NoZoom or NoRotate set to 0."))
					reported6_3_2_3 = true
					if allReported() {
						return violations
					}
				}
			}
			if !reported6_3_3_2 {
				apDict, ok := core.GetDict(dict.Get("AP"))
				if ok {
					n := apDict.Get("N")
					if n == nil || len(apDict.Keys()) > 1 {
						violations = append(violations, newViolatedRule("6.3.3-2", "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key. If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary; otherwise the value of the N key shall be an appearance stream."))
						reported6_3_3_2 = true
						if allReported() {
							return violations
						}
						continue
					}
					_, ok := annotation.GetContext().(*model.PdfAnnotationWidget)
					if ok {
						ftName, ok := core.GetName(dict.Get("FT"))
						if ok && *ftName == "Btn" {
							if _, ok := core.GetDict(n); !ok {
								violations = append(violations, newViolatedRule("6.3.3-2", "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key. If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary; otherwise the value of the N key shall be an appearance stream."))
								reported6_3_3_2 = true
								if allReported() {
									return violations
								}
								continue
							}
						}
					}
					_, ok2 := core.GetStream(n)
					if !ok2 {
						violations = append(violations, newViolatedRule("6.3.3-2", "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key. If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary; otherwise the value of the N key shall be an appearance stream."))
						reported6_3_3_2 = true
						if allReported() {
							return violations
						}
						continue
					}
				}
			}
			pdfAnnotationWidget, ok3 := annotation.GetContext().(*model.PdfAnnotationWidget)
			if !ok3 {
				continue
			}
			if !reported6_4_1_1 {
				if pdfAnnotationWidget.A != nil {
					violations = append(violations, newViolatedRule("6.4.1-1", "A Widget annotation dictionary shall not include an A entry."))
					reported6_4_1_1 = true
					if allReported() {
						return violations
					}
				}
			}
			if !reported6_4_1_1_2 {
				if pdfAnnotationWidget.AA != nil {
					violations = append(violations, newViolatedRule("6.4.1-1", "A Widget annotation dictionary shall not include an AA entry for an additional-actions dictionary."))
					reported6_4_1_1_2 = true
					if allReported() {
						return violations
					}
				}
			}
		}
	}
	return violations
}

func verifyStreamsPDFA23(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_1_7_1, reported6_1_7_3, reported6_1_7_4 bool
	if r.ParserMetadata().HasNonConformantStream() {
		violations = []ViolatedRule{newViolatedRule("6.1.7-2", "The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah) character sequence or by a single LINE FEED character. The endstream keyword shall be preceded by an EOL marker.")}
	}
	for _, n := range r.GetObjectNums() {
		obj, _ := r.GetIndirectObjectByNumber(n)
		if obj == nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		if !reported6_1_7_1 {
			length := stream.Get("Length")
			if length == nil {
				violations = append(violations, newViolatedRule("6.1.7-1", "no 'Length' key found in the stream object"))
				reported6_1_7_1 = true
			} else {
				n, ok := core.GetIntVal(length)
				if !ok {
					violations = append(violations, newViolatedRule("6.1.7-1", "stream 'Length' key should be an integer"))
					reported6_1_7_1 = true
				} else {
					if len(stream.Stream) != n {
						violations = append(violations, newViolatedRule("6.1.7-1", "stream object length value should match the size of the stream"))
						reported6_1_7_1 = true
					}
				}
			}
		}
		if !reported6_1_7_3 {
			if stream.Get("F") != nil {
				reported6_1_7_3 = true
				violations = append(violations, newViolatedRule("6.1.7-3", "stream object should not contain 'F', 'FFilter', or 'FDecodeParams' key"))
			}
			if stream.Get("FFilter") != nil && !reported6_1_7_3 {
				reported6_1_7_3 = true
				violations = append(violations, newViolatedRule("6.1.7-3", "stream object should not contain 'F', 'FFilter', or 'FDecodeParams' key"))
				continue
			}
			if stream.Get("FDecodeParams") != nil && !reported6_1_7_3 {
				reported6_1_7_3 = true
				violations = append(violations, newViolatedRule("6.1.7-3", "stream object should not contain 'F', 'FFilter', or 'FDecodeParams' key"))
				continue
			}
		}
		if !reported6_1_7_4 {
			filterName, ok := core.GetName(core.TraceToDirectObject(stream.Get("Filter")))
			if !ok {
				continue
			}
			if *filterName == core.StreamEncodingFilterNameLZW {
				reported6_1_7_4 = true
				violations = append(violations, newViolatedRule("6.1.7-4", "The LZWDecode filter shall not be permitted."))
			}
		}
	}
	return violations
}

var _ Profile = (*Profile1A)(nil)

func standard2A() standardType { return standardType{part: 2, conformance: "A"} }
func verifyStreamsPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_1_7_2, reported6_1_7_3, reported6_1_10_1 bool
	if r.ParserMetadata().HasNonConformantStream() {
		violations = []ViolatedRule{newViolatedRule("6.1.7-1", "The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah) character sequence or by a single LINE FEED character. The endstream keyword shall be preceded by an EOL marker.")}
	}
	for _, n := range r.GetObjectNums() {
		obj, _ := r.GetIndirectObjectByNumber(n)
		if obj == nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		if !reported6_1_7_2 {
			length := stream.Get("Length")
			if length == nil {
				violations = append(violations, newViolatedRule("6.1.7-2", "no 'Length' key found in the stream object"))
				reported6_1_7_2 = true
			} else {
				n, ok := core.GetIntVal(length)
				if !ok {
					violations = append(violations, newViolatedRule("6.1.7-2", "stream 'Length' key should be an integer"))
					reported6_1_7_2 = true
				} else {
					if len(stream.Stream) != n {
						violations = append(violations, newViolatedRule("6.1.7-2", "stream object length value should match the size of the stream"))
						reported6_1_7_2 = true
					}
				}
			}
		}
		if !reported6_1_7_3 {
			if stream.Get("F") != nil {
				reported6_1_7_3 = true
				violations = append(violations, newViolatedRule("6.1.7-3", "stream object should not contain 'F','FFilter','FDecodeParams' key"))
			}
			if stream.Get("FFilter") != nil && !reported6_1_7_3 {
				reported6_1_7_3 = true
				violations = append(violations, newViolatedRule("6.1.7-3", "stream object should not contain 'F','FFilter','FDecodeParams' key"))
				continue
			}
			if stream.Get("FDecodeParams") != nil && !reported6_1_7_3 {
				reported6_1_7_3 = true
				violations = append(violations, newViolatedRule("6.1.7-3", "stream object should not contain 'F','FFilter','FDecodeParams' key"))
				continue
			}
		}
		if !reported6_1_10_1 {
			filterName, ok := core.GetName(core.TraceToDirectObject(stream.Get("Filter")))
			if !ok {
				continue
			}
			if *filterName == core.StreamEncodingFilterNameLZW {
				reported6_1_10_1 = true
				violations = append(violations, newViolatedRule("6.1.10-1", "The LZWDecode filter shall not be permitted."))
			}
		}
	}
	return violations
}

func (s standardType) outputIntentSubtype() model.PdfOutputIntentType {
	switch s.part {
	case 1:
		return model.PdfOutputIntentTypeA1
	case 2:
		return model.PdfOutputIntentTypeA2
	case 3:
		return model.PdfOutputIntentTypeA3
	case 4:
		return model.PdfOutputIntentTypeA4
	default:
		return 0
	}
}

func addCMYKOutputIntent(std standardType, intents *docutil.OutputIntents) error {
	cmykIsoCoatedV2OutputIntent, err := colorprofile.NewCmykIsoCoatedV2OutputIntent(std.outputIntentSubtype())
	if err != nil {
		return err
	}
	if err = intents.Add(cmykIsoCoatedV2OutputIntent.ToPdfObject()); err != nil {
		return err
	}
	return nil
}

func ensureMinimumPDF2Version(doc *docutil.Document, minor int) {
	if doc.Version.Major < 2 {
		doc.Version.Major = 2
		doc.Version.Minor = 0
	}
	if doc.Version.Minor < minor {
		doc.Version.Minor = minor
	}
}

func verifyCMapsEmbedded(dict *core.PdfObjectDictionary, streamData map[*core.PdfObjectStream][]byte, cmaps map[*core.PdfObjectStream]*cmap.CMap, clause string) ViolatedRule {
	s := clause + ".3-3"
	const (
		detail = "All CMaps used within a PDF/A-2 file, except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118, shall be embedded in that file as described in ISO 32000-1:2008, 9.7.5."
	)
	var str string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		str = subtypeName.String()
	}
	if str != "Type0" {
		return noViolation
	}
	encoding := dict.Get("Encoding")
	if encodingName, ok := core.GetName(encoding); ok {
		switch encodingName.String() {
		case "Identity-H", "Identity-V":
			return noViolation
		default:
			return newViolatedRule(s, detail)
		}
	}
	encodingStream, ok := core.GetStream(encoding)
	if !ok {
		return newViolatedRule(s, detail)
	}
	_, err := loadCMap(encodingStream, streamData, cmaps)
	if err != nil {
		return newViolatedRule(s, detail)
	}
	return noViolation
}

func verifyHexStrings(r *model.CompliancePdfReader, clause string) (violations []ViolatedRule) {
	if r.ParserMetadata().HasOddLengthHexStrings() {
		violations = append(violations, newViolatedRule(clause+"-1", "hexadecimal strings should be of even size"))
	}
	if r.ParserMetadata().HasOddLengthHexStrings() {
		violations = append(violations, newViolatedRule(clause+"-2", "hexadecimal strings should contain only characters from range [0-9;A-F;a-f]"))
	}
	return violations
}

func verifySymbolicTrueTypeEncodingPDFA1(font *model.PdfFont, dict *core.PdfObjectDictionary) ViolatedRule {
	const (
		ruleNo = "6.3.7-2"
		detail = "All symbolic TrueType fonts shall not specify an Encoding entry in the font dictionary."
	)
	var s string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		s = subtypeName.String()
	}
	if s != "TrueType" {
		return noViolation
	}
	pdfFontDescriptor := font.FontDescriptor()
	n, ok := core.GetIntVal(pdfFontDescriptor.Flags)
	if !ok {
		common.Log.Debug("flags not defined for the font descriptor")
		return newViolatedRule(ruleNo, detail)
	}
	count := (uint32(n) >> 3) & 1
	found := count != 0
	if !found {
		return noViolation
	}
	if dict.Get("Encoding") != nil {
		return newViolatedRule(ruleNo, detail)
	}
	return noViolation
}
func (d *documentImages) hasOnlyDeviceCMYK() bool { return d.hasCMYK && !d.hasRGB && !d.hasGray }

// Profile1Options are the options that changes the way how optimizer may try to adapt document into PDF/A standard.
type Profile1Options struct {
	// CMYKDefaultColorSpace is an option that refers PDF/A-1
	CMYKDefaultColorSpace bool

	// Now is a function that returns current time.
	Now func() time.Time

	// Xmp is the xmp options information.
	Xmp XmpOptions
}

func verifyCatalogAdditionalActionsPDFA1(r *model.CompliancePdfReader) (rule ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return noViolation
	}
	if dict.Get("AA") != nil {
		return newViolatedRule("6.6.2-3", "The document catalog dictionary shall not include an AA entry for an additional-actions dictionary.")
	}
	return noViolation
}

func verifyFormXObjectsPDFA23(r *model.CompliancePdfReader) (rule ViolatedRule) {
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(stream.Get("Type"))
		if !ok {
			continue
		}
		if *typeName != "XObject" {
			continue
		}
		_, ok = core.GetName(stream.Get("OPI"))
		if ok {
			return newViolatedRule("6.2.9-1", "A form XObject dictionary shall not contain any of the following: - the OPI key; - the Subtype2 key with a value of PS; - the PS key.")
		}
		subtype2Name, ok := core.GetName(stream.Get("Subtype2"))
		if !ok {
			continue
		}
		if *subtype2Name == "PS" {
			return newViolatedRule("6.2.9-1", "A form XObject dictionary shall not contain any of the following: - the OPI key; - the Subtype2 key with a value of PS; - the PS key.")
		}
		if stream.Get("PS") != nil {
			return newViolatedRule("6.2.9-1", "A form XObject dictionary shall not contain any of the following: - the OPI key; - the Subtype2 key with a value of PS; - the PS key.")
		}
	}
	return rule
}

type imageInfo struct {
	ColorSpace       core.PdfObjectName
	BitsPerComponent int
	ColorComponents  int
	Width            int
	Height           int
	Stream           *core.PdfObjectStream
	otherColorspace  bool
}

// ValidateStandard checks if provided input CompliancePdfReader matches rules that conforms PDF/A-4 standard.
func (p *profile4) ValidateStandard(r *model.CompliancePdfReader) error {
	verificationError := VerificationError{ConformanceLevel: p.standard.part, ConformanceVariant: p.standard.conformance}
	if rule := verifyHeaderPDFA4(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyHeaderBinaryComment(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyTrailer(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyNoDataAfterEOF(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyXrefKeywordEOL(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyHexStrings(r, "6.1.5"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyStreamsPDFA4(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyPermissions(r, "6.1.11"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyContentStreamOperators(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyColorspaces(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyExtGState(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyXObjectsPDFA4(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyFormXObjectOPIPDFA4(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyFontDictionaries(r, "6.2.10.2"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyFonts(r, p.standard, "6.2.10"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyAnnotationsPDFA4(r, p.standard); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyInteractiveFormsPDFA4(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyActionsPDFA4(r, p.standard); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyCatalogAdditionalActionsPDFA4(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyMetadataPDFA4(r, p.standard, false); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyEmbeddedFilesPDFA4(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyOptionalContentPDFA4(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyAlternatePresentations(r, "6.11"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyRequirements(r, "6.12"); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if len(verificationError.ViolatedRules) > 0 {
		sort.Slice(verificationError.ViolatedRules, func(i, j int) bool {
			return verificationError.ViolatedRules[i].RuleNo < verificationError.ViolatedRules[j].RuleNo
		})
		return verificationError
	}
	return nil
}

func verifyEmbeddedFilesPDFA2(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	objectNums := r.GetObjectNums()
	for _, objectNum := range objectNums {
		obj, err := r.GetIndirectObjectByNumber(objectNum)
		if err != nil {
			continue
		}
		dict, ok := core.GetDict(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(dict.Get("Type"))
		if !ok {
			continue
		}
		if typeName.String() != "Filespec" {
			continue
		}
		filespec, err := model.NewPdfFilespecFromObj(dict)
		if err != nil {
			continue
		}
		if filespec.EF != nil {
			if filespec.F == nil || filespec.UF == nil {
				violations = append(violations, newViolatedRule("6.8-2", "The file specification dictionary for an embedded file shall contain the F and UF keys."))
				break
			}
			if filespec.AFRelationship == nil {
				violations = append(violations, newViolatedRule("6.8-3", "In order to enable identification of the relationship between the file specification dictionary and the content that is referring to it, a new (required) key has been defined and its presence (in the dictionary) is required."))
				break
			}
			embeddedFile, err := model.NewEmbeddedFileFromObject(filespec.EF)
			if err != nil {
				continue
			}
			if strings.ToLower(embeddedFile.FileType) != "application/pdf" {
				violations = append(violations, newViolatedRule("6.8-4", "All embedded files should be a PDF file that conform to PDF/A-1 or PDF/A-2."))
				break
			}
		}
	}
	return violations
}

// Conformance gets the PDF/A conformance.
func (p *profile4) Conformance() string { return p.standard.conformance }

func optimizeFormXObjectColors(doc *docutil.Document, cmykDefault bool) error {
	pages, ok := doc.GetPages()
	if !ok {
		return nil
	}
	for _, page := range pages {
		streams := page.FindXObjectForms()
		for _, stream := range streams {
			xObjectForm, err := model.NewXObjectFormFromStream(stream)
			if err != nil {
				return err
			}
			contentStream, err := xObjectForm.GetContentStream()
			if err != nil {
				return err
			}
			contentStreamParser := contentstream.NewContentStreamParser(string(contentStream))
			ops, err := contentStreamParser.Parse()
			if err != nil {
				return err
			}
			data, err := convertContentStreamColors(xObjectForm.Resources, ops, cmykDefault)
			if err != nil {
				return err
			}
			if len(data) == 0 {
				continue
			}
			if err = xObjectForm.SetContentStream(data, core.NewFlateEncoder()); err != nil {
				return err
			}
			xObjectForm.ToPdfObject()
		}
	}
	return nil
}

var _ Profile = (*Profile2U)(nil)

func verifyTrailer(r *model.CompliancePdfReader) ViolatedRule {
	trailer, err := r.PdfReader.GetTrailer()
	if err != nil {
		return newViolatedRule("6.1.3-1", "missing trailer in the document")
	}
	if trailer.Get("ID") == nil {
		return newViolatedRule("6.1.3-1", "The file trailer dictionary shall contain the 'ID' keyword")
	}
	if trailer.Get("Encrypt") != nil {
		return newViolatedRule("6.1.3-2", "The keyword 'Encrypt' shall not be used in the trailer dictionary. ")
	}
	return noViolation
}

var _ Profile = (*Profile1B)(nil)

func propertyMatchesType(prop xmpPropertyValue, propType xmpPropertyType) bool {
	if propType.Base == xmpBaseNone {
		return true
	}
	switch propType.Array {
	case xmpArrayNone:
		if prop.Array != xmpArrayNone {
			return false
		}
		if (propType.Base == xmpBaseOther) != prop.Struct {
			return false
		}
		if propType.Base == xmpBaseOther {
			return true
		}
		return valueMatchesBaseType(prop.Text, propType.Base)
	case xmpArrayLangAlt:
		return prop.Array == xmpArrayAlt || prop.Array == xmpArrayLangAlt
	default:
		if prop.Array != propType.Array {
			return false
		}
		for _, item := range prop.Items {
			if (propType.Base == xmpBaseOther) != item.Struct {
				return false
			}
			if propType.Base == xmpBaseOther {
				continue
			}
			if !valueMatchesBaseType(item.Text, propType.Base) {
				return false
			}
		}
		return true
	}
}

func firstOutputIntent(r *model.CompliancePdfReader) (*model.PdfOutputIntent, bool) {
	dict, ok := firstOutputIntentDictFromReader(r)
	if !ok {
		return nil, false
	}
	outputIntent, err := model.NewPdfOutputIntentFromPdfObject(dict)
	if err != nil {
		return nil, false
	}
	return outputIntent, true
}

func setProfile1OptionDefaults(profile1Options *Profile1Options) {
	if profile1Options.Now == nil {
		profile1Options.Now = time.Now
	}
}

func verifyXrefSubsectionsPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	parserMetadata := r.ParserMetadata()
	if parserMetadata.HasInvalidSubsectionHeader() {
		violations = append(violations, newViolatedRule("6.1.4-1", "n a cross reference subsection header the starting object number and the range shall be separated by a single SPACE character (20h)."))
	}
	if parserMetadata.HasInvalidSeparationAfterXRef() {
		violations = append(violations, newViolatedRule("6.1.4-2", "The xref keyword and the cross reference subsection header shall be separated by a single EOL marker."))
	}
	return violations
}

var _ Profile = (*Profile3B)(nil)

func (d *documentImages) hasOnlyDeviceGray() bool { return d.hasGray && !d.hasRGB && !d.hasCMYK }
func verifyOptionalContentPDFA23(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	checkConfigName := func(dict *core.PdfObjectDictionary, sList *[]string, rules *[]ViolatedRule) error {
		name := dict.Get("Name")
		if name == nil || len(name.String()) == 0 {
			*rules = append(*rules, newViolatedRule("6.9-1", "Each optional content configuration dictionary shall contain the Name key."))
		}
		for _, s := range *sList {
			if s == name.String() {
				*rules = append(*rules, newViolatedRule("6.9-2", "Each optional content configuration dictionary shall contain the Name key, whose value shall be unique amongst all optional content configuration dictionaries within the PDF/A-2 file."))
			} else {
				*sList = append(*sList, name.String())
			}
		}
		if dict.Get("AS") != nil {
			*rules = append(*rules, newViolatedRule("6.9-4", "The AS key shall not appear in any optional content configuration dictionary."))
		}
		return nil
	}
	dict, ok := catalogDict(r)
	if !ok {
		return violations
	}
	ocPropertiesDict, ok := core.GetDict(dict.Get("OCProperties"))
	if !ok {
		return violations
	}
	var sList []string
	dDict, ok := core.GetDict(ocPropertiesDict.Get("D"))
	if ok {
		checkConfigName(dDict, &sList, &violations)
	}
	configsArray, ok := core.GetArray(ocPropertiesDict.Get("Configs"))
	if ok {
		for n := 0; n < configsArray.Len(); n++ {
			dict, ok := core.GetDict(configsArray.Get(n))
			if !ok {
				continue
			}
			checkConfigName(dict, &sList, &violations)
		}
	}
	return violations
}

type imageModifications struct {
	colorspace *colorspaceModification
	encoder    core.StreamEncoder
}

func newViolatedRule(clause string, detail string) ViolatedRule {
	return ViolatedRule{RuleNo: clause, Detail: detail}
}

// String gets a string representation of the violated rule.
func (r ViolatedRule) String() string { return fmt.Sprintf("%s: %s", r.RuleNo, r.Detail) }

type xmpArrayKind uint8

func collectDocumentImages(objs []core.PdfObject) (*documentImages, error) {
	name := core.PdfObjectName("Subtype")
	seen := make(map[*core.PdfObjectStream]struct{})
	set := make(map[core.PdfObject]struct{})
	var (
		hasRGB, hasCMYK, hasGray bool
		imgInfos                 []*imageInfo
		err                      error
	)
	for _, obj := range objs {
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		if _, ok := seen[stream]; ok {
			continue
		}
		seen[stream] = struct{}{}
		obj2 := stream.PdfObjectDictionary.Get(name)
		obj2Name, ok := core.GetName(obj2)
		if !ok || string(*obj2Name) != "Image" {
			continue
		}
		if sMask := stream.PdfObjectDictionary.Get("SMask"); sMask != nil {
			set[sMask] = struct{}{}
		}
		imgInfo := imageInfo{BitsPerComponent: 8, Stream: stream}
		imgInfo.ColorSpace, err = model.DetermineColorspaceNameFromPdfObject(stream.PdfObjectDictionary.Get("ColorSpace"))
		if err != nil {
			return nil, err
		}
		if bitsPerComponent, ok := core.GetIntVal(stream.PdfObjectDictionary.Get("BitsPerComponent")); ok {
			imgInfo.BitsPerComponent = bitsPerComponent
		}
		if width, ok := core.GetIntVal(stream.PdfObjectDictionary.Get("Width")); ok {
			imgInfo.Width = width
		}
		if height, ok := core.GetIntVal(stream.PdfObjectDictionary.Get("Height")); ok {
			imgInfo.Height = height
		}
		switch imgInfo.ColorSpace {
		case "DeviceGray":
			hasGray = true
			imgInfo.ColorComponents = 1
		case "DeviceRGB":
			hasRGB = true
			imgInfo.ColorComponents = 3
		case "DeviceCMYK":
			hasCMYK = true
			imgInfo.ColorComponents = 4
		default:
			imgInfo.otherColorspace = true
		}
		imgInfos = append(imgInfos, &imgInfo)
	}
	if len(set) > 0 {
		if len(set) == len(imgInfos) {
			imgInfos = nil
		} else {
			imgInfos2 := make([]*imageInfo, len(imgInfos)-len(set))
			var n int
			for _, imgInfo := range imgInfos {
				if _, ok := set[imgInfo.Stream]; ok {
					continue
				}
				imgInfos2[n] = imgInfo
				n++
			}
			imgInfos = imgInfos2
		}
	}
	return &documentImages{hasRGB: hasRGB, hasCMYK: hasCMYK, hasGray: hasGray, softMasks: set, images: imgInfos}, nil
}
func standard1B() standardType { return standardType{part: 1, conformance: "B"} }
func standard3U() standardType { return standardType{part: 3, conformance: "U"} }
func verifyContentStreamOperators(r *model.CompliancePdfReader) ViolatedRule {
	for _, page := range r.PageList {
		contentStreams, err := page.GetContentStreams()
		if err != nil {
			continue
		}
		for _, contentStream := range contentStreams {
			contentStreamParser := contentstream.NewContentStreamParser(contentStream)
			_, err = contentStreamParser.Parse()
			if err != nil {
				return newViolatedRule("6.2.2-1", "A content stream shall not contain any operators not defined in PDF Reference even if such operators are bracketed by the BX/EX compatibility operators.")
			}
		}
	}
	return noViolation
}

// Profile4Options are the options that changes the way how optimizer may try to adapt document into PDF/A standard.
type Profile4Options struct {
	// CMYKDefaultColorSpace is an option that refers PDF/A
	CMYKDefaultColorSpace bool

	// Now is a function that returns current time.
	Now func() time.Time

	// Xmp is the xmp options information.
	Xmp XmpOptions
}

func verifyFontDictionariesPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_3_2_1, reported6_3_2_2, reported6_3_2_3, reported6_3_2_4, reported6_3_2_5, reported6_3_2_6, ok bool
	allReported := func() bool {
		return reported6_3_2_1 && reported6_3_2_2 && reported6_3_2_3 && reported6_3_2_4 && reported6_3_2_5 && reported6_3_2_6 && ok
	}
	for _, page := range r.PageList {
		if page.Resources == nil {
			continue
		}
		dict, ok := core.GetDict(page.Resources.Font)
		if !ok {
			continue
		}
		for _, name := range dict.Keys() {
			dict2, ok := core.GetDict(dict.Get(name))
			if !ok {
				if !reported6_3_2_1 {
					violations = append(violations, newViolatedRule("6.3.2-1", "All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5."))
					reported6_3_2_1 = true
					if allReported() {
						return violations
					}
				}
				continue
			}
			if typeName, ok := core.GetName(dict2.Get("Type")); !reported6_3_2_1 && (!ok || typeName.String() != "Font") {
				violations = append(violations, newViolatedRule("6.3.2-1", "Type - name - (Required) The type of PDF object that this dictionary describes; must be Font for a font dictionary."))
				reported6_3_2_1 = true
				if allReported() {
					return violations
				}
			}
			font, err := model.NewPdfFontFromPdfObject(dict2)
			if err != nil {
				continue
			}
			var s string
			if subtypeName, ok := core.GetName(dict2.Get("Subtype")); ok {
				s = subtypeName.String()
			}
			if !reported6_3_2_2 {
				switch s {
				case "Type0", "Type1", "MMType1", "TrueType", "CIDFontType0", "CIDFontType2":
				default:
					reported6_3_2_2 = true
					violations = append(violations, newViolatedRule("6.3.2-2", "Subtype - name - (Required) The type of font; must be \"Type1\" for Type 1 fonts, \"MMType1\" for multiple master fonts, \"TrueType\" for TrueType fonts \"Type3\" for Type 3 fonts, \"Type0\" for Type 0 fonts and \"CIDFontType0\" or \"CIDFontType2\" for CID fonts."))
					if allReported() {
						return violations
					}
				}
			}
			if !reported6_3_2_3 {
				if s != "Type3" {
					baseFontName, ok := core.GetName(dict2.Get("BaseFont"))
					if !ok || baseFontName.String() == "" {
						violations = append(violations, newViolatedRule("6.3.2-3", "BaseFont - name - (Required) The PostScript name of the font."))
						reported6_3_2_3 = true
						if allReported() {
							return violations
						}
					}
				}
			}
			if s != "Type1" {
				continue
			}
			found := fonts.IsStdFont(fonts.StdFontName(font.BaseFont()))
			if found {
				continue
			}
			firstChar, ok2 := core.GetIntVal(dict2.Get("FirstChar"))
			if !ok2 && !reported6_3_2_4 {
				violations = append(violations, newViolatedRule("6.3.2-4", "FirstChar - integer - (Required except for the standard 14 fonts) The first character code defined in the font's Widths array."))
				reported6_3_2_4 = true
				if allReported() {
					return violations
				}
			}
			lastChar, ok3 := core.GetIntVal(dict2.Get("LastChar"))
			if !ok3 && !reported6_3_2_5 {
				violations = append(violations, newViolatedRule("6.3.2-5", "LastChar - integer - (Required except for the standard 14 fonts) The last character code defined in the font's Widths array."))
				reported6_3_2_5 = true
				if allReported() {
					return violations
				}
			}
			if !reported6_3_2_6 {
				widthsArray, ok := core.GetArray(dict2.Get("Widths"))
				if !ok || !ok2 || !ok3 || widthsArray.Len() != lastChar-firstChar+1 {
					violations = append(violations, newViolatedRule("6.3.2-6", "Widths - array - (Required except for the standard 14 fonts; indirect reference preferred) An array of (LastChar − FirstChar + 1) widths."))
					reported6_3_2_6 = true
					if allReported() {
						return violations
					}
				}
			}
		}
	}
	return violations
}

func verifyInteractiveFormsPDFA23(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		dict, ok := core.GetDict(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(dict.Get("Type"))
		if !ok {
			continue
		}
		if typeName.String() != "AcroForm" {
			continue
		}
		needAppearances, ok := core.GetBool(dict.Get("NeedAppearances"))
		if ok && bool(*needAppearances) {
			violations = append(violations, newViolatedRule("6.4.1-3", "The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false."))
		}
		if dict.Get("XFA") != nil {
			violations = append(violations, newViolatedRule("6.4.2-1", "The document's interactive form dictionary that forms the value of the AcroForm key in the document's Catalog of a PDF/A-2 file, if present, shall not contain the XFA key."))
		}
	}
	dict, ok := catalogDict(r)
	if ok && dict.Get("NeedsRendering") != nil {
		violations = append(violations, newViolatedRule("6.4.2-2", "A document's Catalog shall not contain the NeedsRendering key."))
	}
	return violations
}

func verifyInteractiveFormsPDFA4(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		dict, ok := core.GetDict(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(dict.Get("Type"))
		if !ok {
			continue
		}
		if typeName.String() != "AcroForm" {
			continue
		}
		needAppearances, ok := core.GetBool(dict.Get("NeedAppearances"))
		if ok && bool(*needAppearances) {
			violations = append(violations, newViolatedRule("6.4.1-2", "The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false."))
		}
		if dict.Get("XFA") != nil {
			violations = append(violations, newViolatedRule("6.4.2-1", "The document's interactive form dictionary that forms the value of the AcroForm key in the document's Catalog of a PDF/A-2 file, if present, shall not contain the XFA key."))
		}
	}
	dict, ok := catalogDict(r)
	if ok && dict.Get("NeedsRendering") != nil {
		violations = append(violations, newViolatedRule("6.4.2-2", "A document's Catalog shall not contain the NeedsRendering key."))
	}
	return violations
}

func verifyFontDictionaries(r *model.CompliancePdfReader, clause string) (violations []ViolatedRule) {
	var reportedType, reportedSubtype, reportedBaseFont, reportedFirstChar, reportedLastChar, reportedWidths bool
	allReported := func() bool {
		return reportedType && reportedSubtype && reportedBaseFont && reportedFirstChar && reportedLastChar && reportedWidths
	}
	for _, page := range r.PageList {
		if page.Resources == nil {
			continue
		}
		dict, ok := core.GetDict(page.Resources.Font)
		if !ok {
			continue
		}
		for _, name := range dict.Keys() {
			dict2, ok := core.GetDict(dict.Get(name))
			if !ok {
				if !reportedType {
					violations = append(violations, newViolatedRule(clause+"-1", "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7."))
					reportedType = true
					if allReported() {
						return violations
					}
				}
				continue
			}
			if typeName, ok := core.GetName(dict2.Get("Type")); !reportedType && (!ok || typeName.String() != "Font") {
				violations = append(violations, newViolatedRule(clause+"-1", "Type - name - (Required) The type of PDF object that this dictionary describes; must be Font for a font dictionary."))
				reportedType = true
				if allReported() {
					return violations
				}
			}
			font, err := model.NewPdfFontFromPdfObject(dict2)
			if err != nil {
				continue
			}
			var s string
			if subtypeName, ok := core.GetName(dict2.Get("Subtype")); ok {
				s = subtypeName.String()
			}
			if !reportedSubtype {
				switch s {
				case "Type0", "Type1", "MMType1", "TrueType", "CIDFontType0", "CIDFontType2":
				default:
					reportedSubtype = true
					violations = append(violations, newViolatedRule(clause+"-2", "Subtype - name - (Required) The type of font; must be \"Type1\" for Type 1 fonts, \"MMType1\" for multiple master fonts, \"TrueType\" for TrueType fonts \"Type3\" for Type 3 fonts, \"Type0\" for Type 0 fonts and \"CIDFontType0\" or \"CIDFontType2\" for CID fonts."))
					if allReported() {
						return violations
					}
				}
			}
			if !reportedBaseFont {
				if s != "Type3" {
					baseFontName, ok := core.GetName(dict2.Get("BaseFont"))
					if !ok || baseFontName.String() == "" {
						violations = append(violations, newViolatedRule(clause+"-3", "BaseFont - name - (Required) The PostScript name of the font."))
						reportedBaseFont = true
						if allReported() {
							return violations
						}
					}
				}
			}
			if s != "Type1" {
				continue
			}
			found := fonts.IsStdFont(fonts.StdFontName(font.BaseFont()))
			if found {
				continue
			}
			firstChar, ok2 := core.GetIntVal(dict2.Get("FirstChar"))
			if !ok2 && !reportedFirstChar {
				violations = append(violations, newViolatedRule(clause+"-4", "FirstChar - integer - (Required except for the standard 14 fonts) The first character code defined in the font's Widths array."))
				reportedFirstChar = true
				if allReported() {
					return violations
				}
			}
			lastChar, ok3 := core.GetIntVal(dict2.Get("LastChar"))
			if !ok3 && !reportedLastChar {
				violations = append(violations, newViolatedRule(clause+"-5", "LastChar - integer - (Required except for the standard 14 fonts) The last character code defined in the font's Widths array."))
				reportedLastChar = true
				if allReported() {
					return violations
				}
			}
			if !reportedWidths {
				widthsArray, ok := core.GetArray(dict2.Get("Widths"))
				if !ok || !ok2 || !ok3 || widthsArray.Len() != lastChar-firstChar+1 {
					violations = append(violations, newViolatedRule(clause+"-6", "Widths - array - (Required except for the standard 14 fonts; indirect reference preferred) An array of (LastChar − FirstChar + 1) widths."))
					reportedWidths = true
					if allReported() {
						return violations
					}
				}
			}
		}
	}
	return violations
}

func verifyXrefKeywordEOL(r *model.CompliancePdfReader) ViolatedRule {
	parserMetadata := r.ParserMetadata()
	if parserMetadata.HasInvalidSeparationAfterXRef() {
		return newViolatedRule("6.1.4-2", "The xref keyword and the cross reference subsection header shall be separated by a single EOL marker.")
	}
	return noViolation
}

func verifyFieldActionsPDFA1(r *model.CompliancePdfReader) (rule ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return noViolation
	}
	acroFormDict, ok := core.GetDict(dict.Get("AcroForm"))
	if !ok {
		return noViolation
	}
	fieldsArray, ok := core.GetArray(acroFormDict.Get("Fields"))
	if !ok {
		return noViolation
	}
	for n := 0; n < fieldsArray.Len(); n++ {
		dict, ok := core.GetDict(fieldsArray.Get(n))
		if !ok {
			continue
		}
		if dict.Get("AA") != nil {
			return newViolatedRule("6.6.2-2", "A Field dictionary shall not include an AA entry for an additional-actions dictionary.")
		}
	}
	return noViolation
}

func verifyActionsPDFA23(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_5_1_1, reported6_5_1_2 bool
	allReported := func() bool { return reported6_5_1_1 && reported6_5_1_2 }
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			common.Log.Debug("Getting object with number %d failed: %v", n, err)
			continue
		}
		dict, ok := core.GetDict(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(dict.Get("Type"))
		if !ok {
			continue
		}
		if *typeName != "Action" {
			continue
		}
		sName, ok := core.GetName(dict.Get("S"))
		if !ok {
			if !reported6_5_1_1 {
				violations = append(violations, newViolatedRule("6.5.1-1", "The Launch, Sound, Movie, ResetForm, ImportData, Hide, SetOCGState, Rendition, Trans, GoTo3DView and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and noop actions shall not be permitted."))
				reported6_5_1_1 = true
				if allReported() {
					return violations
				}
			}
			continue
		}
		switch model.PdfActionType(*sName) {
		case model.ActionTypeLaunch, model.ActionTypeSound, model.ActionTypeMovie, model.ActionTypeResetForm, model.ActionTypeImportData, model.ActionTypeJavaScript, model.ActionTypeHide, model.ActionTypeSetOCGState, model.ActionTypeRendition, model.ActionTypeTrans, model.ActionTypeGoTo3DView:
			if !reported6_5_1_1 {
				violations = append(violations, newViolatedRule("6.5.1-1", "The Launch, Sound, Movie, ResetForm, ImportData, Hide, SetOCGState, Rendition, Trans, GoTo3DView and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and noop actions shall not be permitted."))
				reported6_5_1_1 = true
				if allReported() {
					return violations
				}
			}
			continue
		case model.ActionTypeNamed:
			if !reported6_5_1_2 {
				nName, ok := core.GetName(dict.Get("N"))
				if !ok {
					violations = append(violations, newViolatedRule("6.5.1-2", "Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted."))
					reported6_5_1_2 = true
					if allReported() {
						return violations
					}
					continue
				}
				switch *nName {
				case "NextPage", "PrevPage", "FirstPage", "LastPage":
				default:
					violations = append(violations, newViolatedRule("6.5.1-2", "Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted."))
					reported6_5_1_2 = true
					if allReported() {
						return violations
					}
					continue
				}
			}
		}
	}
	return violations
}

func verifyRequirements(r *model.CompliancePdfReader, clause string) (rule ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return noViolation
	}
	if dict.Get("Requirements") != nil {
		return newViolatedRule(clause+"-1", "The document catalog shall not contain the Requirements key.")
	}
	return noViolation
}

func verifyXObjectsPDFA4(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_2_7_1_1, reported6_2_8_1_1, reportedUnused2, reported6_2_8_2_1, reported6_2_7_1_3, reportedUnused1, reported6_2_7_1_2 bool
	seen := map[*core.PdfObjectStream]struct{}{}
	for _, n := range r.GetObjectNums() {
		if reported6_2_7_1_1 && reported6_2_8_1_1 && reported6_2_7_1_3 && reportedUnused2 && reported6_2_8_2_1 && reportedUnused1 && reported6_2_7_1_2 {
			return violations
		}
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		if _, ok = seen[stream]; ok {
			continue
		}
		seen[stream] = struct{}{}
		subTypeName, ok := core.GetName(stream.Get("SubType"))
		if !ok {
			continue
		}
		if !reported6_2_8_2_1 {
			if stream.Get("Ref") != nil {
				violations = append(violations, newViolatedRule("6.2.8.2-1", "A conforming file shall not contain any reference XObjects."))
				reported6_2_8_2_1 = true
			}
		}
		if subTypeName.String() == "Form" {
			if reported6_2_8_1_1 && reportedUnused2 && reported6_2_8_2_1 {
				continue
			}
			if !reported6_2_8_1_1 && stream.Get("OPI") != nil {
				violations = append(violations, newViolatedRule("6.2.8.1-1", "A form XObject dictionary shall not contain the OPI key."))
				reported6_2_8_1_1 = true
			}
			continue
		}
		if subTypeName.String() != "Image" {
			continue
		}
		if !reported6_2_7_1_1 && stream.Get("Alternates") != nil {
			violations = append(violations, newViolatedRule("6.2.7.1-1", "An Image dictionary shall not contain the Alternates key."))
			reported6_2_7_1_1 = true
		}
		if !reported6_2_7_1_2 && stream.Get("OPI") != nil {
			violations = append(violations, newViolatedRule("6.2.7.1-2", "An Image dictionary shall not contain the OPI key."))
			reported6_2_7_1_2 = true
		}
		if !reported6_2_7_1_3 && stream.Get("Interpolate") != nil {
			interpolate, ok := core.GetBool(stream.Get("Interpolate"))
			if ok && bool(*interpolate) {
				continue
			}
			violations = append(violations, newViolatedRule("6.2.7.1-3", "If an Image dictionary contains the Interpolate key, its value shall be false."))
			reported6_2_7_1_3 = true
		}
	}
	return violations
}

// ValidateStandard checks if provided input CompliancePdfReader matches rules that conforms PDF/A-3 standard.
func (p *profile3) ValidateStandard(r *model.CompliancePdfReader) error {
	verificationError := VerificationError{ConformanceLevel: p.standard.part, ConformanceVariant: p.standard.conformance}
	if rule := verifyHeaderPDFA23(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyHeaderBinaryComment(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyTrailer(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyNoDataAfterEOF(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyXrefKeywordEOL(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyHexStrings(r, "6.1.6"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyStreamsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := stubClause618PDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyLinearizedPDFA23(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyPermissions(r, "6.1.12"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyImplementationLimitsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyContentStreamOperators(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyColorspaces(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyExtGState(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := stubClause6xxPDFA23b(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyXObjectsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := stubClause6xxPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyFormXObjectsPDFA23(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyFontDictionaries(r, "6.2.11.2"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyFonts(r, p.standard, "6.2.11"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyAnnotationsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyInteractiveFormsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := stubClause6xxFormsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyFieldActionsPDFA23(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyActionsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyCatalogAdditionalActionsPDFA23(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyMetadataPDFA23(r, p.standard, false); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if p.standard == standard3A() {
		if rules := verifyLogicalStructurePDFA23(r); len(rules) != 0 {
			verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
		}
	}
	if rules := verifyEmbeddedFilesPDFA3(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyOptionalContentPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyAlternatePresentations(r, "6.10"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyRequirements(r, "6.11"); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if len(verificationError.ViolatedRules) > 0 {
		sort.Slice(verificationError.ViolatedRules, func(i, j int) bool {
			return verificationError.ViolatedRules[i].RuleNo < verificationError.ViolatedRules[j].RuleNo
		})
		return verificationError
	}
	return nil
}

func verifyColorspaces(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_2_4_3_4, reported6_2_4_3_2, reported6_2_4_3_3, reported6_2_4_2_1 bool
	allReported := func() bool { return reported6_2_4_3_4 && reported6_2_4_3_2 && reported6_2_4_3_3 && reported6_2_4_2_1 }
	intent, ok := firstOutputIntent(r)
	var profileHeader colorprofile.ProfileHeader
	if ok {
		profileHeader, _ = colorprofile.ParseHeader(intent.DestOutputProfile)
	}
	seen := map[core.PdfObject]struct{}{}
	var checkColorspace func(cs model.PdfColorspace) bool
	checkColorspace = func(cs model.PdfColorspace) bool {
		switch cs := cs.(type) {
		case *model.PdfColorspaceDeviceGray:
			if !reported6_2_4_3_4 {
				if !ok {
					violations = append(violations, newViolatedRule("6.2.4.3-4", "DeviceGray shall only be used if a device independent DefaultGray colour space has been set when the DeviceGray colour space is used, or if a PDF/A OutputIntent is present."))
					reported6_2_4_3_4 = true
					if allReported() {
						return true
					}
				}
			}
		case *model.PdfColorspaceDeviceRGB:
			if !reported6_2_4_3_2 {
				if !ok || profileHeader.ColorSpace != colorprofile.ColorSpaceRGB {
					violations = append(violations, newViolatedRule("6.2.4.3-2", "DeviceRGB shall only be used if a device independent DefaultRGB colour space has been set when the DeviceRGB colour space is used, or if the file has a PDF/A OutputIntent that contains an RGB destination profile."))
					reported6_2_4_3_2 = true
					if allReported() {
						return true
					}
				}
			}
		case *model.PdfColorspaceDeviceCMYK:
			if !reported6_2_4_3_3 {
				if !ok || profileHeader.ColorSpace != colorprofile.ColorSpaceCMYK {
					violations = append(violations, newViolatedRule("6.2.4.3-3", "DeviceCMYK shall only be used if a device independent DefaultCMYK colour space has been set or if a DeviceN-based DefaultCMYK colour space has been set when the DeviceCMYK colour space is used or the file has a PDF/A OutputIntent that contains a CMYK destination profile."))
					reported6_2_4_3_3 = true
					if allReported() {
						return true
					}
				}
			}
		case *model.PdfColorspaceICCBased:
			if !reported6_2_4_2_1 {
				profileHeader, err := colorprofile.ParseHeader(cs.Data)
				if err != nil {
					common.Log.Debug("parsing ICCBased header failed: %v", err)
					violations = append(violations, func() ViolatedRule {
						return newViolatedRule("6.2.4.2-1", "The profile that forms the stream of an ICCBased colour space shall conform to ICC.1:1998-09, ICC.1:2001-12, ICC.1:2003-09 or ISO 15076-1.")
					}())
					reported6_2_4_2_1 = true
					if allReported() {
						return true
					}
				}
				if !reported6_2_4_2_1 {
					var badDeviceClass, badColorSpace bool
					switch profileHeader.DeviceClass {
					case colorprofile.DeviceClassPRTR, colorprofile.DeviceClassMNTR, colorprofile.DeviceClassSCNR, colorprofile.DeviceClassSPAC:
					default:
						badDeviceClass = true
					}
					switch profileHeader.ColorSpace {
					case colorprofile.ColorSpaceRGB, colorprofile.ColorSpaceCMYK, colorprofile.ColorSpaceGRAY, colorprofile.ColorSpaceLAB:
					default:
						badColorSpace = true
					}
					if badDeviceClass || badColorSpace {
						violations = append(violations, newViolatedRule("6.2.4.2-1", "The profile that forms the stream of an ICCBased colour space shall conform to ICC.1:1998-09, ICC.1:2001-12, ICC.1:2003-09 or ISO 15076-1."))
						reported6_2_4_2_1 = true
						if allReported() {
							return true
						}
					}
				}
			}
			if cs.Alternate != nil {
				return checkColorspace(cs.Alternate)
			}
		}
		return false
	}
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(stream.Get("Type"))
		if !ok || typeName.String() != "XObject" {
			continue
		}
		subtypeName, ok := core.GetName(stream.Get("Subtype"))
		if !ok {
			continue
		}
		seen[stream] = struct{}{}
		switch subtypeName.String() {
		case "Image":
			xObjectImage, err := model.NewXObjectImageFromStream(stream)
			if err != nil {
				continue
			}
			seen[stream] = struct{}{}
			if checkColorspace(xObjectImage.ColorSpace) {
				return violations
			}
		case "Form":
			groupDict, ok := core.GetDict(stream.Get("Group"))
			if !ok {
				continue
			}
			cs := groupDict.Get("CS")
			if cs == nil {
				continue
			}
			colorspace, err := model.NewPdfColorspaceFromPdfObject(cs)
			if err != nil {
				continue
			}
			if checkColorspace(colorspace) {
				return violations
			}
		}
	}
	for _, page := range r.PageList {
		contentStreams, err := page.GetContentStreams()
		if err != nil {
			continue
		}
		for _, contentStream := range contentStreams {
			ops, err := contentstream.NewContentStreamParser(contentStream).Parse()
			if err != nil {
				continue
			}
			for _, op := range *ops {
				if len(op.Params) > 1 {
					continue
				}
				switch op.Operand {
				case "BI":
					contentStreamInlineImage, ok := op.Params[0].(*contentstream.ContentStreamInlineImage)
					if !ok {
						continue
					}
					colorSpace, err := contentStreamInlineImage.GetColorSpace(page.Resources)
					if err != nil {
						continue
					}
					if checkColorspace(colorSpace) {
						return violations
					}
				case "Do":
					name, ok := core.GetName(op.Params[0])
					if !ok {
						continue
					}
					xObjectByName, xObjectType := page.Resources.GetXObjectByName(*name)
					if _, ok := seen[xObjectByName]; ok {
						continue
					}
					switch xObjectType {
					case model.XObjectTypeImage:
						xObjectImage, err := model.NewXObjectImageFromStream(xObjectByName)
						if err != nil {
							continue
						}
						seen[xObjectByName] = struct{}{}
						if checkColorspace(xObjectImage.ColorSpace) {
							return violations
						}
					case model.XObjectTypeForm:
						groupDict, ok := core.GetDict(xObjectByName.Get("Group"))
						if !ok {
							continue
						}
						csName, ok := core.GetName(groupDict.Get("CS"))
						if !ok {
							continue
						}
						colorspace, err := model.NewPdfColorspaceFromPdfObject(csName)
						if err != nil {
							continue
						}
						seen[xObjectByName] = struct{}{}
						if checkColorspace(colorspace) {
							return violations
						}
					}
				}
			}
		}
	}
	return violations
}

func convertImagesColorspace(imgs []*docutil.Image, cmykDefault bool) error {
	detail := core.PdfObjectName("DeviceRGB")
	if cmykDefault {
		detail = "DeviceCMYK"
	}
	for _, img := range imgs {
		if img.Colorspace == detail {
			continue
		}
		xObjectImage, err := model.NewXObjectImageFromStream(img.Stream)
		if err != nil {
			return err
		}
		image, err := xObjectImage.ToImage()
		if err != nil {
			return err
		}
		image2, err := image.ToGoImage()
		if err != nil {
			return err
		}
		var cs model.PdfColorspace
		if cmykDefault {
			cs = model.NewPdfColorspaceDeviceCMYK()
			image2, err = imageutil.CMYKConverter.Convert(image2)
		} else {
			cs = model.NewPdfColorspaceDeviceRGB()
			image2, err = imageutil.NRGBAConverter.Convert(image2)
		}
		if err != nil {
			return err
		}
		img2, ok := image2.(imageutil.Image)
		if !ok {
			return errors.New("image doesn't implement imageutil.Image")
		}
		imageBase := img2.Base()
		image3 := &model.Image{Width: int64(imageBase.Width), Height: int64(imageBase.Height), BitsPerComponent: int64(imageBase.BitsPerComponent), ColorComponents: imageBase.ColorComponents, Data: imageBase.Data}
		image3.SetDecode(imageBase.Decode)
		image3.SetAlpha(imageBase.Alpha)
		if err = xObjectImage.SetImage(image3, cs); err != nil {
			return err
		}
		xObjectImage.ToPdfObject()
		img.ColorComponents = imageBase.ColorComponents
		img.Colorspace = detail
	}
	return nil
}

func verifyMetadataPDFA23(r *model.CompliancePdfReader, std standardType, unused bool) (violations []ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return []ViolatedRule{newViolatedRule("6.6.2.1-1", "catalog not found.")}
	}
	metadata := dict.Get("Metadata")
	if metadata == nil {
		return []ViolatedRule{newViolatedRule("6.6.2.1-1", "The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream")}
	}
	metadataStream, ok := core.GetStream(metadata)
	if !ok {
		return []ViolatedRule{newViolatedRule("6.6.2.1-1", "The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream")}
	}
	xmp, err := xmputil.LoadDocument(metadataStream.Stream)
	if err != nil {
		violations = []ViolatedRule{newViolatedRule("6.6.2.1-4", "All metadata streams present in the PDF shall conform to the XMP Specification. All content of all XMP packets shall be well-formed")}
		if xmputil.PacketEncoding(metadataStream.Stream) != xmputil.EncodingUTF8 {
			violations = append(violations, newViolatedRule("6.6.2.1-5", "All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8"))
		}
		return violations
	}
	if xmp.Encoding() != xmputil.EncodingUTF8 {
		violations = append(violations, newViolatedRule("6.6.2.1-5", "All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8"))
	}
	seen := map[string]struct{}{}
	for _, prop := range parseXMPProperties(xmp.Packet()) {
		if isPredefinedNamespace(prop.Namespace) {
			continue
		}
		seen[prop.Namespace] = struct{}{}
	}
	found := true
	pdfaExtensionSchemas, err := xmp.GetPdfaExtensionSchemas()
	if err != nil {
		found = false
	} else {
		for s := range seen {
			var ok bool
			for i := range pdfaExtensionSchemas {
				if s == pdfaExtensionSchemas[i].NamespaceURI {
					ok = true
					break
				}
			}
			if !ok {
				found = false
				break
			}
		}
	}
	if !found {
		violations = append(violations, newViolatedRule("6.6.2.3.1-1", "All properties specified in XMP form shall use either the predefined schemas defined in the XMP Specification, ISO 19005-1 or this part of ISO 19005, or any extension schemas that comply with 6.6.2.3.2."))
	}
	if !validateXMPProperties(xmp.Packet(), pdfaExtensionSchemas, false) {
		violations = append(violations, newViolatedRule("6.6.2.3.1-2", "The value of an XMP property does not have the type its schema declares for it."))
	}
	pdfAID, ok := xmp.GetPdfAID()
	if !ok {
		violations = append(violations, newViolatedRule("6.6.4-1", "The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema."))
	} else {
		if pdfAID.Part != std.part {
			violations = append(violations, newViolatedRule("6.6.4-2", "The value of pdfaid:part shall be the part number of ISO 19005 to which the file conforms."))
		}
		if std.conformance == "A" && pdfAID.Conformance != "A" {
			violations = append(violations, newViolatedRule("6.6.4-3", "A Level A conforming file shall specify the value of pdfaid:conformance as A. A Level B conforming file shall specify the value of pdfaid:conformance as B. A Level U conforming file shall specify the value of pdfaid:conformance as U."))
		} else if std.conformance == "U" && (pdfAID.Conformance != "A" && pdfAID.Conformance != "U") {
			violations = append(violations, newViolatedRule("6.6.4-3", "A Level A conforming file shall specify the value of pdfaid:conformance as A. A Level B conforming file shall specify the value of pdfaid:conformance as B. A Level U conforming file shall specify the value of pdfaid:conformance as U."))
		} else if std.conformance == "B" && (pdfAID.Conformance != "A" && pdfAID.Conformance != "B" && pdfAID.Conformance != "U") {
			violations = append(violations, newViolatedRule("6.6.4-3", "A Level A conforming file shall specify the value of pdfaid:conformance as A. A Level B conforming file shall specify the value of pdfaid:conformance as B. A Level U conforming file shall specify the value of pdfaid:conformance as U."))
		}
	}
	return violations
}

// Profile2A is the implementation of the PDF/A-2A standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type Profile2A struct{ profile2 }

func parsePropertyElement(dec *xml.Decoder, start xml.StartElement) (xmpPropertyValue, error) {
	prop := xmpPropertyValue{Namespace: start.Name.Space, Name: start.Name.Local}
	prop.Struct = isResourceParseType(start)
	for {
		tok, err := dec.Token()
		if err != nil {
			return prop, err
		}
		switch tok := tok.(type) {
		case xml.CharData:
			prop.Text = strings.TrimSpace(string(tok))
		case xml.StartElement:
			if tok.Name.Space == rdfNamespace && (tok.Name.Local == "Seq" || tok.Name.Local == "Bag" || tok.Name.Local == "Alt") {
				prop.Array = arrayKindFromName(tok.Name.Local)
				items, ok, err := parseArrayItems(dec)
				if err != nil {
					return prop, err
				}
				prop.Items = items
				if prop.Array == xmpArrayAlt && ok {
					prop.Array = xmpArrayLangAlt
				}
				if err := skipElement(dec, 0); err != nil {
					return prop, err
				}
				return prop, nil
			}
			s, contentKind, err := parseItemContent(dec, tok)
			if err != nil {
				return prop, err
			}
			prop.Text, prop.Struct = resolveItemText(s, contentKind, prop.Struct)
			return prop, nil
		case xml.EndElement:
			return prop, nil
		}
	}
}

type profile4 struct {
	standard standardType
	options  Profile4Options
}

func verifyFontsPDFA1(r *model.CompliancePdfReader, std standardType) (violations []ViolatedRule) {
	var cidSystemInfoReported, cidToGIDMapReported, cMapsEmbeddedReported, cMapWModeReported, fontEmbeddedReported, charSetReported, cidSetReported, nonSymbolicTrueTypeEncodingReported, symbolicTrueTypeEncodingReported, symbolicTrueTypeCmapReported, toUnicodeReported bool
	allReported := func() bool {
		return cidSystemInfoReported && cidToGIDMapReported && cMapsEmbeddedReported && cMapWModeReported && fontEmbeddedReported && charSetReported && cidSetReported && nonSymbolicTrueTypeEncodingReported && symbolicTrueTypeEncodingReported && symbolicTrueTypeCmapReported && toUnicodeReported
	}
	cmaps := map[*core.PdfObjectStream]*cmap.CMap{}
	streamData := map[*core.PdfObjectStream][]byte{}
	fontByObj := map[core.PdfObject]*model.PdfFont{}
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		dict, ok := core.GetDict(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(dict.Get("Type"))
		if !ok {
			continue
		}
		if *typeName != "Font" {
			continue
		}
		font, err := model.NewPdfFontFromPdfObject(dict)
		if err != nil {
			common.Log.Debug("getting font from object failed: %v", err)
			continue
		}
		fontByObj[dict] = font
	}
	for _, page := range r.PageList {
		contentStreams, err := page.GetContentStreams()
		if err != nil {
			common.Log.Debug("Getting page content streams failed")
			continue
		}
		for _, contentStream := range contentStreams {
			contentStreamParser := contentstream.NewContentStreamParser(contentStream)
			ops, err := contentStreamParser.Parse()
			if err != nil {
				common.Log.Debug("parsing contentstream failed: %v", err)
				continue
			}
			var invisibleText bool
			for _, op := range *ops {
				if op.Operand != "Tr" {
					continue
				}
				if len(op.Params) != 1 {
					common.Log.Debug("invalid number of parameters for the 'Tr' operand, expected '1' but is: '%d'", len(op.Params))
					continue
				}
				n, ok := core.GetIntVal(op.Params[0])
				if !ok {
					common.Log.Debug("rendering mode is not an integer")
					continue
				}
				if n == 3 {
					invisibleText = true
					break
				}
			}
			for _, op := range *ops {
				if op.Operand != "Tf" {
					continue
				}
				if len(op.Params) != 2 {
					common.Log.Debug("invalid number of parameters for the 'Tf' operand, expected '2' is: '%d'", len(op.Params))
					continue
				}
				name, ok := core.GetName(op.Params[0])
				if !ok {
					common.Log.Debug("ERROR: Tf op=%s GetNameVal failed", op)
					continue
				}
				fontByName, ok2 := page.Resources.GetFontByName(*name)
				if !ok2 {
					common.Log.Debug("font not found")
					continue
				}
				fontByDict, ok := core.GetDict(fontByName)
				if !ok {
					common.Log.Debug("font dict not found")
					continue
				}
				font, ok := fontByObj[fontByDict]
				if !ok {
					var err error
					font, err = model.NewPdfFontFromPdfObject(fontByDict)
					if err != nil {
						common.Log.Debug("getting font from object: %v", err)
						continue
					}
					fontByObj[fontByDict] = font
				}
				if !cidSystemInfoReported {
					rule := verifyCIDSystemInfoPDFA1(fontByDict, streamData, cmaps)
					if rule != noViolation {
						violations = append(violations, rule)
						cidSystemInfoReported = true
						if allReported() {
							return violations
						}
					}
				}
				if !cidToGIDMapReported {
					rule := verifyCIDToGIDMapPDFA1(fontByDict)
					if rule != noViolation {
						violations = append(violations, rule)
						cidToGIDMapReported = true
						if allReported() {
							return violations
						}
					}
				}
				if !cMapsEmbeddedReported {
					rule := verifyCMapsEmbeddedPDFA1(fontByDict, streamData, cmaps)
					if rule != noViolation {
						violations = append(violations, rule)
						cMapsEmbeddedReported = true
						if allReported() {
							return violations
						}
					}
				}
				if !cMapWModeReported {
					rule := verifyCMapWModePDFA1(fontByDict, streamData, cmaps)
					if rule != noViolation {
						violations = append(violations, rule)
						cMapWModeReported = true
						if allReported() {
							return violations
						}
					}
				}
				if !fontEmbeddedReported {
					rule := verifyFontEmbeddedPDFA1(font, fontByDict, invisibleText)
					if rule != noViolation {
						fontEmbeddedReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
				if !charSetReported {
					rule := verifyCharSetPDFA1(font, fontByDict)
					if rule != noViolation {
						charSetReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
				if !cidSetReported {
					rule := verifyCIDSetPDFA1(font, fontByDict)
					if rule != noViolation {
						cidSetReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
				if !nonSymbolicTrueTypeEncodingReported {
					rule := verifyNonSymbolicTrueTypeEncodingPDFA1(font, fontByDict)
					if rule != noViolation {
						nonSymbolicTrueTypeEncodingReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
				if !symbolicTrueTypeEncodingReported {
					rule := verifySymbolicTrueTypeEncodingPDFA1(font, fontByDict)
					if rule != noViolation {
						symbolicTrueTypeEncodingReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
				if !symbolicTrueTypeCmapReported {
					rule := verifySymbolicTrueTypeCmapPDFA1(font, fontByDict)
					if rule != noViolation {
						symbolicTrueTypeCmapReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
				if !toUnicodeReported && std.conformance == "A" {
					rule := verifyToUnicodePDFA1(fontByDict, streamData, cmaps)
					if rule != noViolation {
						toUnicodeReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
			}
		}
	}
	return violations
}

func embedFonts(doc *docutil.Document) error {
	dictByS := map[string]*core.PdfObjectDictionary{}
	finder := sysfont.NewFinder(&sysfont.FinderOpts{Extensions: []string{".ttf", ".ttc"}})
	seen := map[core.PdfObject]struct{}{}
	set := map[core.PdfObject]struct{}{}
	for _, object := range doc.Objects {
		objectDict, ok := core.GetDict(object)
		if !ok {
			continue
		}
		typeObj := objectDict.Get("Type")
		if typeObj == nil {
			continue
		}
		if typeName, ok := core.GetName(typeObj); ok && typeName.String() != "Font" {
			continue
		}
		if _, ok := seen[object]; ok {
			continue
		}
		font, err := model.NewPdfFontFromPdfObject(objectDict)
		if err != nil {
			common.Log.Debug("ERROR: could not load font from object")
			return err
		}
		if font.Encoder() != nil && (font.Encoder().String() == "Identity-H" || font.Encoder().String() == "Identity-V") {
			continue
		}
		if font.Subtype() == "CIDFontType2" {
			cidToGIDMapObject := font.GetCIDToGIDMapObject()
			if cidToGIDMapObject != nil {
				continue
			}
		}
		fontDescriptor, err := font.GetFontDescriptor()
		if err != nil {
			return err
		}
		if fontDescriptor != nil && (fontDescriptor.FontFile != nil || fontDescriptor.FontFile2 != nil || fontDescriptor.FontFile3 != nil) {
			continue
		}
		s := font.BaseFont()
		if s == "" {
			fontDescriptor, err := font.GetFontDescriptor()
			if err != nil {
				return fmt.Errorf("can't get the font name from font descriptor: %s", objectDict.String())
			}
			s = fontDescriptor.FontName.String()
			if s == "" {
				return fmt.Errorf("one of the font objects syntax is not valid - BaseFont undefined: %s", objectDict.String())
			}
		}
		dict, found := dictByS[s]
		if !found {
			if len(s) > 7 && s[6] == '+' {
				s = s[7:]
			}
			sList := []string{s, "Times New Roman", "Arial", "DejaVu Sans"}
			for _, s := range sList {
				common.Log.Debug("DEBUG: searching system font `%s`", s)
				if dict, found = dictByS[s]; found {
					break
				}
				font := finder.Match(s)
				if font == nil {
					common.Log.Debug("could not find font file %s", s)
					continue
				}
				pdfFontFromTTFFile, err := model.NewPdfFontFromTTFFile(font.Filename)
				if err != nil {
					return err
				}
				pdfFontDescriptor := pdfFontFromTTFFile.FontDescriptor()
				if pdfFontDescriptor.FontFile != nil {
					if _, found = set[pdfFontDescriptor.FontFile]; !found {
						doc.Objects = append(doc.Objects, pdfFontDescriptor.FontFile)
						set[pdfFontDescriptor.FontFile] = struct{}{}
					}
				}
				if pdfFontDescriptor.FontFile2 != nil {
					if _, found = set[pdfFontDescriptor.FontFile2]; !found {
						doc.Objects = append(doc.Objects, pdfFontDescriptor.FontFile2)
						set[pdfFontDescriptor.FontFile2] = struct{}{}
					}
				}
				if pdfFontDescriptor.FontFile3 != nil {
					if _, found = set[pdfFontDescriptor.FontFile3]; !found {
						doc.Objects = append(doc.Objects, pdfFontDescriptor.FontFile3)
						set[pdfFontDescriptor.FontFile3] = struct{}{}
					}
				}
				indObj, ok := pdfFontFromTTFFile.ToPdfObject().(*core.PdfIndirectObject)
				if !ok {
					common.Log.Debug("font is not an indirect object")
					continue
				}
				dict2, ok := indObj.PdfObject.(*core.PdfObjectDictionary)
				if !ok {
					common.Log.Debug("Font type is not an object dictionary")
					continue
				}
				dictByS[s] = dict2
				dict = dict2
				break
			}
			if dict == nil {
				common.Log.Debug("No matching font found for: %s", font.BaseFont())
				return errors.New("no matching font found in the system")
			}
		}
		for _, name := range dict.Keys() {
			objectDict.Set(name, dict.Get(name))
		}
		widths := dict.Get("Widths")
		if widths != nil {
			if _, found = set[widths]; !found {
				doc.Objects = append(doc.Objects, widths)
				set[widths] = struct{}{}
			}
		}
		seen[object] = struct{}{}
		fontDescriptorObj := objectDict.Get("FontDescriptor")
		if fontDescriptorObj != nil {
			doc.Objects = append(doc.Objects, fontDescriptorObj)
			set[fontDescriptorObj] = struct{}{}
		}
	}
	return nil
}

// DefaultProfile2Options are the default options for the Profile2.
func DefaultProfile2Options() *Profile2Options {
	return &Profile2Options{Now: time.Now, Xmp: XmpOptions{MarshalIndent: "\t"}}
}

func filterCatalogAdditionalActions(doc *docutil.Document) error {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return errors.New("catalog not found")
	}
	aaDict, ok := core.GetDict(catalog.Object.Get("AA"))
	if !ok {
		return nil
	}
	for _, name := range aaDict.Keys() {
		if name != "E" && name != "X" && name != "D" && name != "U" && name != "Fo" && name != "Bl" {
			aaDict.Remove(name)
		}
	}
	return nil
}
func (d *documentImages) hasOnlyDeviceRGB() bool { return d.hasRGB && !d.hasCMYK && !d.hasGray }
func arrayKindFromName(s string) xmpArrayKind {
	switch s {
	case "Seq":
		return xmpArraySeq
	case "Bag":
		return xmpArrayBag
	case "Alt":
		return xmpArrayAlt
	}
	return xmpArrayNone
}

var _ Profile = (*Profile2A)(nil)

func ensureStructTreeRoot(doc *docutil.Document) error {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return nil
	}
	_, ok = catalog.GetStructTreeRoot()
	if !ok {
		structTreeRoot := model.NewStructTreeRoot()
		indObj := structTreeRoot.ToPdfObject().(*core.PdfIndirectObject)
		dict := indObj.PdfObject.(*core.PdfObjectDictionary)
		catalog.SetStructTreeRoot(dict)
	}
	return nil
}

func verifyCIDToGIDMapPDFA1(dict *core.PdfObjectDictionary) ViolatedRule {
	const (
		ruleNo = "6.3.3-2"
		detail = "For all Type 2 CIDFonts that are used for rendering, the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in PDF Reference Table 5.13"
	)
	var s string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		s = subtypeName.String()
	}
	if s != "CIDFontType2" {
		return noViolation
	}
	if dict.Get("CIDToGIDMap") == nil {
		return newViolatedRule(ruleNo, detail)
	}
	return noViolation
}

func loadCMap(stream *core.PdfObjectStream, streamData map[*core.PdfObjectStream][]byte, cmaps map[*core.PdfObjectStream]*cmap.CMap) (*cmap.CMap, error) {
	cm, ok := cmaps[stream]
	if !ok {
		var err error
		data, ok := streamData[stream]
		if !ok {
			data, err = core.DecodeStream(stream)
			if err != nil {
				common.Log.Debug("decoding stream failed: %v", err)
				return nil, err
			}
			streamData[stream] = data
		}
		cm, err = cmap.LoadCmapFromData(data, false)
		if err != nil {
			return nil, err
		}
		cmaps[stream] = cm
	}
	return cm, nil
}
func standard3A() standardType { return standardType{part: 3, conformance: "A"} }

// DefaultProfile3Options the default options for the Profile3.
func DefaultProfile3Options() *Profile3Options {
	return &Profile3Options{Now: time.Now, Xmp: XmpOptions{MarshalIndent: "\t"}}
}

func verifyCatalogAdditionalActionsPDFA4(r *model.CompliancePdfReader) (rule ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return noViolation
	}
	aaDict, ok := core.GetDict(dict.Get("AA"))
	if !ok {
		return noViolation
	}
	for _, name := range aaDict.Keys() {
		if name != "E" && name != "X" && name != "D" && name != "U" && name != "Fo" && name != "Bl" {
			return newViolatedRule("6.6.3-1", "If a document catalog dictionary includes an AA entry, its value (which is an additional-actions dictionary) shall only contain keys from the following list: E, X, D, U, Fo, and Bl")
		}
	}
	return noViolation
}

func verifyNonSymbolicTrueTypeEncodingPDFA1(font *model.PdfFont, dict *core.PdfObjectDictionary) ViolatedRule {
	const (
		ruleNo = "6.3.7-1"
		detail = "All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary which is the value of the Encoding key in the Font dictionary. In addition, no non-symbolic TrueType font shall define a Differences array unless all of the glyph names in the Differences array are listed in the Adobe Glyph List and the embedded font program contains at least the Microsoft Unicode (3,1 – Platform ID=3, Encoding ID=1) encoding in the 'cmap' table."
	)
	var s string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		s = subtypeName.String()
	}
	if s != "TrueType" {
		return noViolation
	}
	pdfFontDescriptor := font.FontDescriptor()
	n, ok := core.GetIntVal(pdfFontDescriptor.Flags)
	if !ok {
		common.Log.Debug("flags not defined for the font descriptor")
		return newViolatedRule(ruleNo, detail)
	}
	found := (uint32(n) >> 3) != 0
	if found {
		return noViolation
	}
	encodingName, ok := core.GetName(dict.Get("Encoding"))
	if !ok {
		return newViolatedRule(ruleNo, detail)
	}
	switch encodingName.String() {
	case "MacRomanEncoding", "WinAnsiEncoding":
		return noViolation
	default:
		return newViolatedRule(ruleNo, detail)
	}
}

// Error implements error interface.
func (e VerificationError) Error() string {
	builder := strings.Builder{}
	builder.WriteString("Standard: ")
	builder.WriteString(fmt.Sprintf("PDF/A-%d%s", e.ConformanceLevel, e.ConformanceVariant))
	builder.WriteString(" Violated rules:\n")
	for i, violatedRule := range e.ViolatedRules {
		builder.WriteString(violatedRule.String())
		if i != len(e.ViolatedRules)-1 {
			builder.WriteRune('\n')
		}
	}
	return builder.String()
}

// Profile2U is the implementation of the PDF/A-2U standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type Profile2U struct{ profile2 }

func standard1A() standardType { return standardType{part: 1, conformance: "A"} }
func setInfoDates(info *model.PdfInfo, now func() time.Time) error {
	var pdfDate *model.PdfDate
	if info.CreationDate == nil {
		date, err := model.NewPdfDateFromTime(now())
		if err != nil {
			return err
		}
		pdfDate = &date
		info.CreationDate = pdfDate
	}
	if info.ModifiedDate == nil {
		if pdfDate != nil {
			date, err := model.NewPdfDateFromTime(now())
			if err != nil {
				return err
			}
			pdfDate = &date
		}
		info.ModifiedDate = pdfDate
	}
	return nil
}

// ValidateStandard checks if provided input CompliancePdfReader matches rules that conforms PDF/A-2 standard.
func (p *profile2) ValidateStandard(r *model.CompliancePdfReader) error {
	verificationError := VerificationError{ConformanceLevel: p.standard.part, ConformanceVariant: p.standard.conformance}
	if rule := verifyHeaderPDFA23(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyHeaderBinaryComment(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyTrailer(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyNoDataAfterEOF(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyXrefKeywordEOL(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyHexStrings(r, "6.1.6"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyStreamsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := stubClause618PDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyLinearizedPDFA23(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyPermissions(r, "6.1.12"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyImplementationLimitsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyContentStreamOperators(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyColorspaces(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyExtGState(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := stubClause6xxPDFA23b(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyXObjectsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := stubClause6xxPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyFormXObjectsPDFA23(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyFontDictionaries(r, "6.2.11.2"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyFonts(r, p.standard, "6.2.11"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyAnnotationsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyInteractiveFormsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := stubClause6xxFormsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyFieldActionsPDFA23(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyActionsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyCatalogAdditionalActionsPDFA23(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyMetadataPDFA23(r, p.standard, false); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if p.standard == standard2A() {
		if rules := verifyLogicalStructurePDFA23(r); len(rules) != 0 {
			verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
		}
	}
	if rules := verifyEmbeddedFilesPDFA2(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyOptionalContentPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyAlternatePresentations(r, "6.10"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyRequirements(r, "6.11"); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if len(verificationError.ViolatedRules) > 0 {
		sort.Slice(verificationError.ViolatedRules, func(i, j int) bool {
			return verificationError.ViolatedRules[i].RuleNo < verificationError.ViolatedRules[j].RuleNo
		})
		return verificationError
	}
	return nil
}

func updateInfoDates(doc *docutil.Document, now func() time.Time) error {
	info, err := model.NewPdfInfoFromObject(doc.Info)
	if err != nil {
		return err
	}
	if err := setInfoDates(info, now); err != nil {
		return err
	}
	doc.Info = info.ToPdfObject()
	return nil
}

func addOutputIntent(std standardType, intents *docutil.OutputIntents) error {
	isoCoatedV2Gray1CBasOutputIntent, err := colorprofile.NewISOCoatedV2Gray1CBasOutputIntent(std.outputIntentSubtype())
	if err != nil {
		return err
	}
	if err = intents.Add(isoCoatedV2Gray1CBasOutputIntent.ToPdfObject()); err != nil {
		return err
	}
	return nil
}

func catalogDict(r *model.CompliancePdfReader) (*core.PdfObjectDictionary, bool) {
	trailer, err := r.GetTrailer()
	if err != nil {
		common.Log.Debug("Cannot get document trailer: %v", err)
		return nil, false
	}
	ref, ok := trailer.Get("Root").(*core.PdfObjectReference)
	if !ok {
		common.Log.Debug("Cannot find document root")
		return nil, false
	}
	refDict, ok := core.GetDict(core.ResolveReference(ref))
	if !ok {
		common.Log.Debug("cannot resolve catalog dictionary")
		return nil, false
	}
	return refDict, true
}

func verifyCMapsEmbeddedPDFA1(dict *core.PdfObjectDictionary, streamData map[*core.PdfObjectStream][]byte, cmaps map[*core.PdfObjectStream]*cmap.CMap) ViolatedRule {
	const (
		ruleNo = "6.3.3-3"
		detail = "All CMaps used within a conforming file, except Identity-H and Identity-V, shall be embedded in that file as described in PDF Reference 5.6.4."
	)
	var s string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		s = subtypeName.String()
	}
	if s != "Type0" {
		return noViolation
	}
	encoding := dict.Get("Encoding")
	if encodingName, ok := core.GetName(encoding); ok {
		switch encodingName.String() {
		case "Identity-H", "Identity-V":
			return noViolation
		default:
			return newViolatedRule(ruleNo, detail)
		}
	}
	encodingStream, ok := core.GetStream(encoding)
	if !ok {
		return newViolatedRule(ruleNo, detail)
	}
	_, err := loadCMap(encodingStream, streamData, cmaps)
	if err != nil {
		return newViolatedRule(ruleNo, detail)
	}
	return noViolation
}

func firstOutputIntentDictFromReader(r *model.CompliancePdfReader) (*core.PdfObjectDictionary, bool) {
	dict, ok := catalogDict(r)
	if !ok {
		return nil, false
	}
	outputIntentsArray, ok := core.GetArray(dict.Get("OutputIntents"))
	if !ok {
		return nil, false
	}
	if outputIntentsArray.Len() == 0 {
		return nil, false
	}
	return core.GetDict(outputIntentsArray.Get(0))
}

// Profile1A is the implementation of the PDF/A-1A standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type (
	Profile1A              struct{ profile1 }
	colorspaceModification struct {
		converter imageutil.ColorConverter
		target    model.PdfColorspace
	}
)

func verifyFiltersPDFA1(r *model.CompliancePdfReader) ViolatedRule {
	for _, page := range r.PageList {
		contentStreamObjs := page.GetContentStreamObjs()
		for _, contentStreamObj := range contentStreamObjs {
			contentStreamObj = core.TraceToDirectObject(contentStreamObj)
			var s string
			switch contentStream := contentStreamObj.(type) {
			case *core.PdfObjectString:
				s = contentStream.Str()
			case *core.PdfObjectStream:
				filterName, ok := core.GetName(core.TraceToDirectObject(contentStream.Get("Filter")))
				if ok {
					if *filterName == core.StreamEncodingFilterNameLZW {
						return newViolatedRule("6.1.10-2", "The LZWDecode filter shall not be permitted.")
					}
				}
				data, err := core.DecodeStream(contentStream)
				if err != nil {
					common.Log.Debug("Err: %v", err)
					continue
				}
				s = string(data)
			default:
				common.Log.Debug("Invalid content stream object: %T", contentStreamObj)
				continue
			}
			contentStreamParser := contentstream.NewContentStreamParser(s)
			ops, err := contentStreamParser.Parse()
			if err != nil {
				common.Log.Debug("Invalid content stream: %v", err)
				continue
			}
			for _, op := range *ops {
				if !(op.Operand == "BI" && len(op.Params) == 1) {
					continue
				}
				contentStreamInlineImage, ok := op.Params[0].(*contentstream.ContentStreamInlineImage)
				if !ok {
					continue
				}
				encoder, err := contentStreamInlineImage.GetEncoder()
				if err != nil {
					common.Log.Debug("getting inline image encoder failed: %v", err)
					continue
				}
				if encoder.GetFilterName() == core.StreamEncodingFilterNameLZW {
					return newViolatedRule("6.1.10-2", "The LZWDecode filter shall not be permitted.")
				}
			}
		}
	}
	return noViolation
}

func verifyOptionalContentPDFA4(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	checkConfigName := func(dict *core.PdfObjectDictionary, sList *[]string, rules *[]ViolatedRule) error {
		name := dict.Get("Name")
		if name == nil || len(name.String()) == 0 {
			*rules = append(*rules, newViolatedRule("6.10-1", "Each optional content configuration dictionary shall contain the Name key."))
		}
		for _, s := range *sList {
			if s == name.String() {
				*rules = append(*rules, newViolatedRule("6.10-2", "Each optional content configuration dictionary shall contain the Name key, whose value shall be unique amongst all optional content configuration dictionaries within the PDF/A-2 file."))
			} else {
				*sList = append(*sList, name.String())
			}
		}
		return nil
	}
	dict, ok := catalogDict(r)
	if !ok {
		return violations
	}
	ocPropertiesDict, ok := core.GetDict(dict.Get("OCProperties"))
	if !ok {
		return violations
	}
	var sList []string
	dDict, ok := core.GetDict(ocPropertiesDict.Get("D"))
	if ok {
		checkConfigName(dDict, &sList, &violations)
	}
	configsArray, ok := core.GetArray(ocPropertiesDict.Get("Configs"))
	if ok {
		for n := 0; n < configsArray.Len(); n++ {
			dict, ok := core.GetDict(configsArray.Get(n))
			if !ok {
				continue
			}
			checkConfigName(dict, &sList, &violations)
		}
	}
	return violations
}
func standard4() standardType { return standardType{part: 4, conformance: ""} }

// NewProfile2U creates a new Profile2U with the given options.
func NewProfile2U(options *Profile2Options) *Profile2U {
	if options == nil {
		options = DefaultProfile2Options()
	}
	setProfile2OptionDefaults(options)
	return &Profile2U{profile2{options: *options, standard: standard2U()}}
}

func verifyCharSetPDFA1(font *model.PdfFont, dict *core.PdfObjectDictionary) ViolatedRule {
	const (
		ruleNo = "6.3.5-2"
		detail = "For all Type 1 font subsets referenced within a conforming file, the font descriptor dictionary shall include a CharSet string listing the character names defined in the font subset, as described in PDF Reference Table 5.18."
	)
	var s string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		s = subtypeName.String()
	}
	if s != "Type1" {
		return noViolation
	}
	if fonts.IsStdFont(fonts.StdFontName(font.BaseFont())) {
		return noViolation
	}
	pdfFontDescriptor := font.FontDescriptor()
	if pdfFontDescriptor.CharSet == nil {
		return newViolatedRule(ruleNo, detail)
	}
	return noViolation
}
func standard4E() standardType { return standardType{part: 4, conformance: "E"} }

var infoKeyRules = []struct {
	Key    string
	RuleNo string
}{{Key: "CreationDate", RuleNo: "6.7.3-1"}, {Key: "Title", RuleNo: "6.7.3-2"}, {Key: "Author", RuleNo: "6.7.3-3"}, {Key: "Subject", RuleNo: "6.7.3-4"}, {Key: "Keywords", RuleNo: "6.7.3-5"}, {Key: "Creator", RuleNo: "6.7.3-6"}, {Key: "Producer", RuleNo: "6.7.3-7"}, {Key: "ModDate", RuleNo: "6.7.3-8"}}

func verifyStreamsPDFA4(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_1_6_1_1, reported6_1_6_1_2, reported6_1_6_2_1 bool
	if r.ParserMetadata().HasNonConformantStream() {
		violations = []ViolatedRule{newViolatedRule("6.1.6.1-1", "The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah) character sequence or by a single LINE FEED character. The endstream keyword shall be preceded by an EOL marker.")}
	}
	for _, n := range r.GetObjectNums() {
		obj, _ := r.GetIndirectObjectByNumber(n)
		if obj == nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		if !reported6_1_6_1_1 {
			length := stream.Get("Length")
			if length == nil {
				violations = append(violations, newViolatedRule("6.1.6.1-1", "no 'Length' key found in the stream object"))
				reported6_1_6_1_1 = true
			} else {
				n, ok := core.GetIntVal(length)
				if !ok {
					violations = append(violations, newViolatedRule("6.1.6.1-1", "stream 'Length' key should be an integer"))
					reported6_1_6_1_1 = true
				} else {
					if len(stream.Stream) != n {
						violations = append(violations, newViolatedRule("6.1.6.1-1", "stream object length value should match the size of the stream"))
						reported6_1_6_1_1 = true
					}
				}
			}
		}
		if !reported6_1_6_1_2 {
			if stream.Get("F") != nil {
				reported6_1_6_1_2 = true
				violations = append(violations, newViolatedRule("6.1.6.1-2", "stream object should not contain 'F', 'FFilter', or 'FDecodeParams' key"))
			}
			if stream.Get("FFilter") != nil && !reported6_1_6_1_2 {
				reported6_1_6_1_2 = true
				violations = append(violations, newViolatedRule("6.1.6.1-2", "stream object should not contain 'F', 'FFilter', or 'FDecodeParams' key"))
				continue
			}
			if stream.Get("FDecodeParams") != nil && !reported6_1_6_1_2 {
				reported6_1_6_1_2 = true
				violations = append(violations, newViolatedRule("6.1.6.1-2", "stream object should not contain 'F', 'FFilter', or 'FDecodeParams' key"))
				continue
			}
		}
		if !reported6_1_6_2_1 {
			filterName, ok := core.GetName(core.TraceToDirectObject(stream.Get("Filter")))
			if !ok {
				continue
			}
			if *filterName == core.StreamEncodingFilterNameLZW {
				reported6_1_6_2_1 = true
				violations = append(violations, newViolatedRule("6.1.6.2-1", "The LZWDecode filter shall not be permitted."))
			}
		}
	}
	return violations
}

func lookupPropertyType(prop xmpPropertyValue, propTypeBySByS map[string]map[string]xmpPropertyType, useOverrides bool) (xmpPropertyType, bool) {
	if useOverrides {
		if propTypeByS, ok := predefinedPropertyOverrides[prop.Namespace]; ok {
			if propType, ok := propTypeByS[prop.Name]; ok {
				return propType, true
			}
		}
	}
	propTypeBySBySs := []map[string]map[string]xmpPropertyType{predefinedProperties, propTypeBySByS}
	if !isPredefinedNamespace(prop.Namespace) {
		propTypeBySBySs = []map[string]map[string]xmpPropertyType{propTypeBySByS, predefinedProperties}
	}
	for _, propTypeBySByS := range propTypeBySBySs {
		if propTypeByS, ok := propTypeBySByS[prop.Namespace]; ok {
			if propType, ok := propTypeByS[prop.Name]; ok {
				return propType, true
			}
		}
	}
	return xmpPropertyType{}, false
}

// ApplyStandard tries to change the content of the writer to match the PDF/A-2 standard.
// Implements model.StandardApplier.
func (p *profile2) ApplyStandard(document *docutil.Document) (err error) {
	ensureMinimumVersion(document, 7)
	if err = updateInfoDates(document, p.options.Now); err != nil {
		return err
	}
	if err = setCatalogVersion(document); err != nil {
		return err
	}
	optimizePage, optimizeDocument := newColorspaceOptimizers(p.options.CMYKDefaultColorSpace, p.standard)
	err = optimizeColorspaces(document, []pageColorspaceOptimizeFunc{optimizePage}, []documentColorspaceOptimizeFunc{optimizeDocument})
	if err != nil {
		return err
	}
	ensureDocumentID(document)
	if err = fixPermissions(document); err != nil {
		return err
	}
	if err = fixXObjects(document, p.standard.part); err != nil {
		return err
	}
	if err = fixAnnotationsPDFA23(document); err != nil {
		return err
	}
	if err = fixExtGStatePDFA23(document); err != nil {
		return err
	}
	if err = embedFonts(document); err != nil {
		return err
	}
	if err = fixInteractiveForm(document); err != nil {
		return err
	}
	if p.standard.conformance == "A" {
		setMarkInfoMarked(document)
	}
	if err = fixActions(document, p.standard.part); err != nil {
		return err
	}
	if err = removeCatalogAdditionalActions(document); err != nil {
		return err
	}
	if err := updateXMPMetadata(document, p.standard, p.options.Xmp); err != nil {
		return err
	}
	if p.standard == standard2A() {
		if err = ensureStructTreeRoot(document); err != nil {
			return err
		}
	}
	if err = fixOptionalContent(document); err != nil {
		return err
	}
	if err = removeAlternatePresentations(document); err != nil {
		return err
	}
	if err = removeRequirements(document); err != nil {
		return err
	}
	return nil
}

var predefinedNamespaces = map[string]struct{}{"http://purl.org/dc/elements/1.1/": {}, "http://ns.adobe.com/xap/1.0/": {}, "http://ns.adobe.com/xap/1.0/rights/": {}, "http://ns.adobe.com/xap/1.0/mm/": {}, "http://ns.adobe.com/xap/1.0/bj/": {}, "http://ns.adobe.com/xap/1.0/t/pg/": {}, "http://ns.adobe.com/xmp/1.0/DynamicMedia/": {}, "http://ns.adobe.com/pdf/1.3/": {}, "http://ns.adobe.com/photoshop/1.0/": {}, "http://ns.adobe.com/camera-raw-settings/1.0/": {}, "http://ns.adobe.com/exif/1.0/": {}, "http://ns.adobe.com/exif/1.0/aux/": {}, "http://ns.adobe.com/tiff/1.0/": {}, "http://ns.adobe.com/xap/1.0/g/": {}, "http://ns.adobe.com/xap/1.0/g/img/": {}, "http://ns.adobe.com/xap/1.0/sType/Dimensions#": {}, "http://ns.adobe.com/xap/1.0/sType/Font#": {}, "http://ns.adobe.com/xap/1.0/sType/Job#": {}, "http://ns.adobe.com/xap/1.0/sType/ResourceEvent#": {}, "http://ns.adobe.com/xap/1.0/sType/ResourceRef#": {}, "http://ns.adobe.com/xap/1.0/sType/Version#": {}, "http://ns.adobe.com/xmp/Identifier/qual/1.0/": {}, "http://www.aiim.org/pdfa/ns/id/": {}, "http://www.aiim.org/pdfa/ns/extension/": {}, "http://www.aiim.org/pdfa/ns/schema#": {}, "http://www.aiim.org/pdfa/ns/property#": {}, "http://www.aiim.org/pdfa/ns/type#": {}, "http://www.aiim.org/pdfa/ns/field#": {}}

func verifyLogicalStructurePDFA23(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	ok := true
	catalogMarkInfo, ok2 := r.GetCatalogMarkInfo()
	if !ok2 {
		ok = false
	} else {
		catalogMarkInfoDict, ok2 := core.GetDict(catalogMarkInfo)
		if ok2 {
			marked, ok2 := core.GetBool(catalogMarkInfoDict.Get("Marked"))
			if !ok2 || !bool(*marked) {
				ok = false
			}
		} else {
			ok = false
		}
	}
	if !ok {
		violations = append(violations, newViolatedRule("6.7.2.2-1", "The document catalog dictionary shall include a MarkInfo dictionary with a Marked entry in it, whose value shall be true"))
	}
	catalogStructTreeRoot, ok2 := r.GetCatalogStructTreeRoot()
	if !ok2 {
		violations = append(violations, newViolatedRule("6.7.3.3-1", "The logical structure of the conforming file shall be described by a structure hierarchy rooted in the StructTreeRoot entry of the document catalog dictionary, as described in PDF Reference 9.6."))
	}
	catalogStructTreeRootDict, ok2 := core.GetDict(catalogStructTreeRoot)
	if ok2 {
		roleMapName, ok := core.GetName(catalogStructTreeRootDict.Get("RoleMap"))
		if ok {
			roleMapDict, ok := core.GetDict(roleMapName)
			if ok {
				for _, name := range roleMapDict.Keys() {
					obj := roleMapDict.Get(name)
					if obj == nil {
						violations = append(violations, newViolatedRule("6.7.3.4-1", "All non-standard structure types shall be mapped to the nearest functionally equivalent standard type, as defined in PDF Reference 9.7.4, in the role map dictionary of the structure tree root."))
					}
				}
			}
		}
	}
	return violations
}

func verifyXObjectSMaskPDFA1(r *model.CompliancePdfReader) ViolatedRule {
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(stream.Get("Type"))
		if !ok {
			continue
		}
		if *typeName != "XObject" {
			continue
		}
		if stream.Get("SMask") != nil {
			return newViolatedRule("6.4-2", "An XObject dictionary shall not contain the SMask key.")
		}
	}
	return noViolation
}

func verifyCatalogAdditionalActionsPDFA23(r *model.CompliancePdfReader) (rule ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return noViolation
	}
	if dict.Get("AA") != nil {
		return newViolatedRule("6.5.2-1", "The document catalog shall not include an AA entry for an additional-actions dictionary.")
	}
	return noViolation
}

func fixActionsPDFA4(doc *docutil.Document, std standardType) error {
	for _, object := range doc.Objects {
		objectDict, ok := core.GetDict(object)
		if !ok {
			continue
		}
		typeObj := objectDict.Get("Type")
		if typeObj == nil {
			continue
		}
		if typeName, ok := core.GetName(typeObj); ok && typeName.String() != "Action" {
			continue
		}
		sName, ok2 := core.GetName(objectDict.Get("S"))
		if !ok2 {
			continue
		}
		switch model.PdfActionType(*sName) {
		case model.ActionTypeLaunch, model.ActionTypeSound, model.ActionTypeMovie, model.ActionTypeResetForm, model.ActionTypeImportData, model.ActionTypeJavaScript, model.ActionTypeHide, model.ActionTypeRendition, model.ActionTypeTrans:
			objectDict.Remove("S")
		case model.ActionTypeSetOCGState, model.ActionTypeGoTo3DView:
			if std.conformance != "E" {
				objectDict.Remove("S")
			}
		case model.ActionTypeNamed:
			nName, ok := core.GetName(objectDict.Get("N"))
			if !ok {
				continue
			}
			switch *nName {
			case "NextPage", "PrevPage", "FirstPage", "LastPage":
			default:
				objectDict.Remove("N")
			}
		}
	}
	return nil
}

func verifyTransparencyGroupPDFA1(r *model.CompliancePdfReader) ViolatedRule {
	seen := map[*core.PdfObjectStream]struct{}{}
	for _, page := range r.PageList {
		if page.Resources == nil && page.Contents == nil {
			continue
		}
		if pageDict := page.GetPageDict(); pageDict != nil {
			groupDict, ok := core.GetDict(pageDict.Get("Group"))
			if ok {
				if s := groupDict.Get("S"); s != nil {
					sName, ok := core.GetName(s)
					if ok && sName.String() == "Transparency" {
						return newViolatedRule("6.4-3", "A Group object with an S xObject with a value of Transparency shall not be included in a form XObject.\nA Group object with an S xObject with a value of Transparency shall not be included in a page dictionary.")
					}
				}
			}
		}
		if page.Resources != nil {
			if dict, ok := core.GetDict(page.Resources.XObject); ok {
				for _, name := range dict.Keys() {
					stream, ok := core.GetStream(dict.Get(name))
					if !ok {
						continue
					}
					if _, ok := seen[stream]; ok {
						continue
					}
					groupDict, ok := core.GetDict(stream.Get("Group"))
					if !ok {
						seen[stream] = struct{}{}
						continue
					}
					s := groupDict.Get("S")
					if s != nil {
						sName, ok := core.GetName(s)
						if ok && sName.String() == "Transparency" {
							return newViolatedRule("6.4-3", "A Group object with an S xObject with a value of Transparency shall not be included in a form XObject.\nA Group object with an S xObject with a value of Transparency shall not be included in a page dictionary.")
						}
					}
					seen[stream] = struct{}{}
					continue
				}
			}
		}
		if page.Contents != nil {
			contentStreams, err := page.GetContentStreams()
			if err != nil {
				continue
			}
			for _, contentStream := range contentStreams {
				ops, err := contentstream.NewContentStreamParser(contentStream).Parse()
				if err != nil {
					continue
				}
				for _, op := range *ops {
					if len(op.Params) == 0 {
						continue
					}
					name, ok := core.GetName(op.Params[0])
					if !ok {
						continue
					}
					xObjectByName, xObjectType := page.Resources.GetXObjectByName(*name)
					if xObjectType != model.XObjectTypeForm {
						continue
					}
					if _, ok := seen[xObjectByName]; ok {
						continue
					}
					groupDict, ok := core.GetDict(xObjectByName.Get("Group"))
					if !ok {
						seen[xObjectByName] = struct{}{}
						continue
					}
					s := groupDict.Get("S")
					if s != nil {
						sName, ok := core.GetName(s)
						if ok && sName.String() == "Transparency" {
							return newViolatedRule("6.4-3", "A Group object with an S xObject with a value of Transparency shall not be included in a form XObject.\nA Group object with an S xObject with a value of Transparency shall not be included in a page dictionary.")
						}
					}
					seen[xObjectByName] = struct{}{}
				}
			}
		}
	}
	return noViolation
}

type pageColorspaceOptimizeFunc func(doc *docutil.Document, page *docutil.Page, imgs []*docutil.Image) error

func valueMatchesBaseType(s string, baseType xmpBaseType) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	switch baseType {
	case xmpBaseText, xmpBaseOther:
		return true
	case xmpBaseInteger:
		_, err := strconv.Atoi(s)
		return err == nil
	case xmpBaseReal:
		_, err := strconv.ParseFloat(s, 64)
		return err == nil
	case xmpBaseBoolean:
		return s == "True" || s == "False"
	case xmpBaseRational:
		str, s2, ok := strings.Cut(s, "/")
		if !ok {
			return false
		}
		if _, err := strconv.Atoi(strings.TrimSpace(str)); err != nil {
			return false
		}
		_, err := strconv.Atoi(strings.TrimSpace(s2))
		return err == nil
	case xmpBaseDate:
		return isValidXMPDate(s)
	}
	return true
}

// Profile2Options are the options that changes the way how optimizer may try to adapt document into PDF/A standard.
type Profile2Options struct {
	// CMYKDefaultColorSpace is an option that refers PDF/A
	CMYKDefaultColorSpace bool

	// Now is a function that returns current time.
	Now func() time.Time

	// Xmp is the xmp options information.
	Xmp XmpOptions
}

func stubClause6xxPDFA23(r *model.CompliancePdfReader) (violations []ViolatedRule) { return violations }

// NewProfile1A creates a new Profile1A with given options.
func NewProfile1A(options *Profile1Options) *Profile1A {
	if options == nil {
		options = DefaultProfile1Options()
	}
	setProfile1OptionDefaults(options)
	return &Profile1A{profile1{options: *options, standard: standard1A()}}
}

func verifyCIDSystemInfoPDFA1(dict *core.PdfObjectDictionary, streamData map[*core.PdfObjectStream][]byte, cmaps map[*core.PdfObjectStream]*cmap.CMap) ViolatedRule {
	const (
		detail = "For any given composite (Type 0) font referenced within a conforming file, the CIDSystemInfo entries of its CIDFont and CMap dictionaries shall be compatible. In other words, the Registry and Ordering strings of the CIDSystemInfo dictionaries for that font shall be identical, unless the value of the Encoding key in the font dictionary is Identity-H or Identity-V."
		ruleNo = "6.3.3-1"
	)
	var s string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		s = subtypeName.String()
	}
	if s != "Type0" {
		return noViolation
	}
	encoding := dict.Get("Encoding")
	if encodingName, ok := core.GetName(encoding); ok {
		switch encodingName.String() {
		case "Identity-H", "Identity-V":
			return noViolation
		}
		cm, err := cmap.LoadPredefinedCMap(encodingName.String())
		if err != nil {
			return newViolatedRule(ruleNo, detail)
		}
		cidSystemInfo := cm.CIDSystemInfo()
		if cidSystemInfo.Ordering != cidSystemInfo.Registry {
			return newViolatedRule(ruleNo, detail)
		}
		return noViolation
	}
	encodingStream, ok := core.GetStream(encoding)
	if !ok {
		return newViolatedRule(ruleNo, detail)
	}
	cm, err := loadCMap(encodingStream, streamData, cmaps)
	if err != nil {
		return newViolatedRule(ruleNo, detail)
	}
	cidSystemInfo := cm.CIDSystemInfo()
	if cidSystemInfo.Ordering != cidSystemInfo.Registry {
		return newViolatedRule(ruleNo, detail)
	}
	return noViolation
}

func updateXMPMetadataPDFA4(doc *docutil.Document, std standardType, xmpOptions XmpOptions) error {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return nil
	}
	var xmp *xmputil.Document
	metadata, ok := catalog.GetMetadata()
	if !ok {
		xmp = xmputil.NewDocument()
	} else {
		var err error
		xmp, err = xmputil.LoadDocument(metadata.Stream)
		if err != nil {
			return err
		}
	}
	pdfInfoOptions := xmputil.PdfInfoOptions{InfoDict: doc.Info, PdfVersion: fmt.Sprintf("%d.%d", doc.Version.Major, doc.Version.Minor), Copyright: xmpOptions.Copyright, Overwrite: true}
	markInfo, ok := catalog.GetMarkInfo()
	if ok {
		marked, ok := core.GetBool(markInfo.Get("Marked"))
		if ok && bool(*marked) {
			pdfInfoOptions.Marked = true
		}
	}
	if err := xmp.SetPdfInfo(&pdfInfoOptions); err != nil {
		return err
	}
	mediaManagementOptions := xmputil.MediaManagementOptions{OriginalDocumentID: xmpOptions.OriginalDocumentID, DocumentID: xmpOptions.DocumentID, InstanceID: xmpOptions.InstanceID, NewDocumentID: !xmpOptions.NewDocumentVersion, ModifyComment: "Optimize document to PDF/A standard"}
	n := time.Now().Year()
	dict, ok := core.GetDict(doc.Info)
	if ok {
		if modDate, ok := core.GetString(dict.Get("ModDate")); ok && modDate.String() != "" {
			t, err := timeutils.ParsePdfTime(modDate.String())
			if err != nil {
				return fmt.Errorf("invalid ModDate field: %w", err)
			}
			mediaManagementOptions.ModifyDate = t
			n = t.Year()
		}
	}
	if err := xmp.SetPdfAIDWithRev(std.part, std.conformance, n); err != nil {
		return err
	}
	if err := xmp.SetMediaManagement(&mediaManagementOptions); err != nil {
		return err
	}
	if err := xmp.SetPdfAExtension(); err != nil {
		return err
	}
	data, err := xmp.MarshalIndent(xmpOptions.MarshalPrefix, xmpOptions.MarshalIndent)
	if err != nil {
		return err
	}
	if err := catalog.SetMetadata(data); err != nil {
		return err
	}
	return nil
}

func skipElement(dec *xml.Decoder, depth int) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			if depth == 0 {
				return nil
			}
			depth--
		}
	}
}

// NewProfile3B creates a new Profile3B with the given options.
func NewProfile3B(options *Profile3Options) *Profile3B {
	if options == nil {
		options = DefaultProfile3Options()
	}
	setProfile3OptionDefaults(options)
	return &Profile3B{profile3{options: *options, standard: standard3B()}}
}

func fixAnnotationsPDFA1(doc *docutil.Document) error {
	pages, ok := doc.GetPages()
	if !ok {
		return nil
	}
	for _, page := range pages {
		annotsArray, ok := core.GetArray(page.Object.Get("Annots"))
		if !ok {
			continue
		}
		for _, obj := range annotsArray.Elements() {
			obj = core.ResolveReference(obj)
			if _, ok := obj.(*core.PdfObjectNull); ok {
				continue
			}
			dict, ok := core.GetDict(obj)
			if !ok {
				continue
			}
			f, _ := core.GetIntVal(dict.Get("F"))
			f &= ^(1 << 0)
			f &= ^(1 << 1)
			f &= ^(1 << 5)
			f |= 1 << 2
			dict.Set("F", core.MakeInteger(int64(f)))
			found := false
			if subtype := dict.Get("Subtype"); subtype != nil {
				subtypeName, ok := core.GetName(subtype)
				if ok && subtypeName.String() == "Widget" {
					found = true
					if dict.Get("AA") != nil {
						dict.Remove("AA")
					}
				}
			}
			if dict.Get("C") != nil || dict.Get("IC") != nil {
				dict2, ok := firstOutputIntentDict(doc)
				if !ok {
					dict.Remove("C")
					dict.Remove("IC")
				} else {
					n, ok := core.GetIntVal(dict2.Get("N"))
					if !ok || n != 3 {
						dict.Remove("C")
						dict.Remove("IC")
					}
				}
			}
			apDict, ok := core.GetDict(dict.Get("AP"))
			if ok {
				n := apDict.Get("N")
				if n == nil {
					continue
				}
				if len(apDict.Keys()) > 1 {
					apDict.Clear()
					apDict.Set("N", n)
				}
				if found {
					ftName, ok := core.GetName(dict.Get("FT"))
					if ok && *ftName == "Btn" {
						continue
					}
				}
			}
		}
	}
	return nil
}

func verifyHeaderPDFA1(r *model.CompliancePdfReader) ViolatedRule {
	if r.ParserMetadata().HeaderPosition() != 0 {
		return newViolatedRule("6.1.2-1", "header position is not at the first byte")
	}
	return noViolation
}
func (d *documentImages) hasUncalibratedImages() bool { return d.hasRGB || d.hasCMYK || d.hasGray }
func ensureDocumentID(doc *docutil.Document) {
	if doc.ID[0] != "" && doc.ID[1] != "" {
		return
	}
	doc.UseHashBasedID = true
}

func (s standardType) String() string {
	return fmt.Sprintf("PDF/A-%d%s", s.part, s.conformance)
}

func verifyLinearizedPDFA23(r *model.CompliancePdfReader) ViolatedRule {
	return verifyLinearized(r, "6.1.11")
}

func verifyHeaderBinaryComment(r *model.CompliancePdfReader) ViolatedRule {
	bs := r.ParserMetadata().HeaderCommentBytes()
	if bs[0] > 127 && bs[1] > 127 && bs[2] > 127 && bs[3] > 127 {
		return noViolation
	}
	return newViolatedRule("6.1.2-2", "The file header line shall be immediately followed by a comment consisting of a % character followed by at\tleast four characters, each of whose encoded byte values shall have a decimal value greater than 127.")
}

// Part gets the PDF/A version level.
func (p *profile3) Part() int { return p.standard.part }

// Profile4 is the implementation of the PDF/A-4 standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type Profile4 struct{ profile4 }

func fixAnnotationsPDFA23(doc *docutil.Document) error {
	pages, ok := doc.GetPages()
	if !ok {
		return nil
	}
	for _, page := range pages {
		annotsArray, ok := core.GetArray(page.Object.Get("Annots"))
		if !ok {
			continue
		}
		for _, obj := range annotsArray.Elements() {
			obj = core.ResolveReference(obj)
			if _, ok := obj.(*core.PdfObjectNull); ok {
				continue
			}
			dict, ok := core.GetDict(obj)
			if !ok {
				continue
			}
			f, _ := core.GetIntVal(dict.Get("F"))
			f &= ^(1 << 0)
			f &= ^(1 << 1)
			f &= ^(1 << 5)
			f &= ^(1 << 8)
			f |= 1 << 2
			dict.Set("F", core.MakeInteger(int64(f)))
			found := false
			if subtype := dict.Get("Subtype"); subtype != nil {
				subtypeName, ok := core.GetName(subtype)
				if ok && subtypeName.String() == "Widget" {
					found = true
					if dict.Get("AA") != nil {
						dict.Remove("AA")
					}
					if dict.Get("A") != nil {
						dict.Remove("A")
					}
				}
				if ok && subtypeName.String() == "Text" {
					f, _ := core.GetIntVal(dict.Get("F"))
					f |= 1 << 3
					f |= 1 << 4
					dict.Set("F", core.MakeInteger(int64(f)))
				}
			}
			apDict, ok := core.GetDict(dict.Get("AP"))
			if ok {
				n := apDict.Get("N")
				if n == nil {
					continue
				}
				if len(apDict.Keys()) > 1 {
					apDict.Clear()
					apDict.Set("N", n)
				}
				if found {
					ftName, ok := core.GetName(dict.Get("FT"))
					if ok && *ftName == "Btn" {
						continue
					}
				}
			}
		}
	}
	return nil
}

func convertPageContentColors(doc *docutil.Document, cmykDefault bool) error {
	pages, ok := doc.GetPages()
	if !ok {
		return nil
	}
	for _, page := range pages {
		contents, ok := page.GetContents()
		if !ok {
			continue
		}
		var res *model.PdfPageResources
		resources, ok := page.GetResources()
		if ok {
			res, _ = model.NewPdfPageResourcesFromDict(resources)
		}
		for i, content := range contents {
			data, err := content.GetData()
			if err != nil {
				continue
			}
			contentStreamParser := contentstream.NewContentStreamParser(string(data))
			ops, err := contentStreamParser.Parse()
			if err != nil {
				continue
			}
			data2, err := convertContentStreamColors(res, ops, cmykDefault)
			if err != nil {
				return err
			}
			if data2 == nil {
				continue
			}
			if err = (&contents[i]).SetData(data2); err != nil {
				return err
			}
		}
	}
	return nil
}

func verifySymbolicTrueTypeCmapPDFA1(font *model.PdfFont, dict *core.PdfObjectDictionary) ViolatedRule {
	const (
		ruleNo = "6.3.7-3"
		detail = "Font programs' \"cmap\" tables for all symbolic TrueType fonts shall contain exactly one encoding."
	)
	var s string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		s = subtypeName.String()
	}
	if s != "TrueType" {
		return noViolation
	}
	pdfFontDescriptor := font.FontDescriptor()
	n, ok := core.GetIntVal(pdfFontDescriptor.Flags)
	if !ok {
		common.Log.Debug("flags not defined for the font descriptor")
		return newViolatedRule(ruleNo, detail)
	}
	found := (uint32(n) >> 3) != 0
	if !found {
		return noViolation
	}
	return noViolation
}

func setMarkInfoMarked(doc *docutil.Document) {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return
	}
	markInfo, ok := catalog.GetMarkInfo()
	if !ok {
		markInfo = core.MakeDict()
	}
	marked, ok := core.GetBool(markInfo.Get("Marked"))
	if !ok || !bool(*marked) {
		markInfo.Set("Marked", core.MakeBool(true))
		catalog.SetMarkInfo(markInfo)
	}
}

var predefinedProperties = map[string]map[string]xmpPropertyType{"http://iptc.org/std/Iptc4xmpCore/1.0/xmlns/": {"CountryCode": {xmpArrayNone, xmpBaseText}, "CreatorContactInfo": {xmpArrayNone, xmpBaseOther}, "IntellectualGenre": {xmpArrayNone, xmpBaseText}, "Location": {xmpArrayNone, xmpBaseText}, "Scene": {xmpArrayBag, xmpBaseText}, "SubjectCode": {xmpArrayBag, xmpBaseText}}, "http://ns.adobe.com/camera-raw-settings/1.0/": {"AutoBrightness": {xmpArrayNone, xmpBaseBoolean}, "AutoContrast": {xmpArrayNone, xmpBaseBoolean}, "AutoExposure": {xmpArrayNone, xmpBaseBoolean}, "AutoShadows": {xmpArrayNone, xmpBaseBoolean}, "BlueHue": {xmpArrayNone, xmpBaseInteger}, "BlueSaturation": {xmpArrayNone, xmpBaseInteger}, "Brightness": {xmpArrayNone, xmpBaseInteger}, "CameraProfile": {xmpArrayNone, xmpBaseText}, "ChromaticAberrationB": {xmpArrayNone, xmpBaseInteger}, "ChromaticAberrationR": {xmpArrayNone, xmpBaseInteger}, "ColorNoiseReduction": {xmpArrayNone, xmpBaseInteger}, "Contrast": {xmpArrayNone, xmpBaseInteger}, "CropAngle": {xmpArrayNone, xmpBaseReal}, "CropBottom": {xmpArrayNone, xmpBaseReal}, "CropHeight": {xmpArrayNone, xmpBaseReal}, "CropLeft": {xmpArrayNone, xmpBaseReal}, "CropRight": {xmpArrayNone, xmpBaseReal}, "CropTop": {xmpArrayNone, xmpBaseReal}, "CropUnits": {xmpArrayNone, xmpBaseInteger}, "CropWidth": {xmpArrayNone, xmpBaseReal}, "Exposure": {xmpArrayNone, xmpBaseReal}, "GreenHue": {xmpArrayNone, xmpBaseInteger}, "GreenSaturation": {xmpArrayNone, xmpBaseInteger}, "HasCrop": {xmpArrayNone, xmpBaseBoolean}, "HasSettings": {xmpArrayNone, xmpBaseBoolean}, "LuminanceSmoothing": {xmpArrayNone, xmpBaseInteger}, "RawFileName": {xmpArrayNone, xmpBaseText}, "RedHue": {xmpArrayNone, xmpBaseInteger}, "RedSaturation": {xmpArrayNone, xmpBaseInteger}, "Saturation": {xmpArrayNone, xmpBaseInteger}, "ShadowTint": {xmpArrayNone, xmpBaseInteger}, "Shadows": {xmpArrayNone, xmpBaseInteger}, "Sharpness": {xmpArrayNone, xmpBaseInteger}, "Temperature": {xmpArrayNone, xmpBaseInteger}, "Tint": {xmpArrayNone, xmpBaseInteger}, "ToneCurve": {xmpArraySeq, xmpBaseText}, "ToneCurveName": {xmpArrayNone, xmpBaseText}, "Version": {xmpArrayNone, xmpBaseText}, "VignetteAmount": {xmpArrayNone, xmpBaseInteger}, "VignetteMidpoint": {xmpArrayNone, xmpBaseInteger}, "WhiteBalance": {xmpArrayNone, xmpBaseText}}, "http://ns.adobe.com/exif/1.0/": {"ApertureValue": {xmpArrayNone, xmpBaseRational}, "BrightnessValue": {xmpArrayNone, xmpBaseRational}, "CFAPattern": {xmpArrayNone, xmpBaseOther}, "ColorSpace": {xmpArrayNone, xmpBaseInteger}, "ComponentsConfiguration": {xmpArraySeq, xmpBaseInteger}, "CompressedBitsPerPixel": {xmpArrayNone, xmpBaseRational}, "Contrast": {xmpArrayNone, xmpBaseInteger}, "CustomRendered": {xmpArrayNone, xmpBaseInteger}, "DateTimeDigitized": {xmpArrayNone, xmpBaseDate}, "DateTimeOriginal": {xmpArrayNone, xmpBaseDate}, "DeviceSettingDescription": {xmpArrayNone, xmpBaseOther}, "DigitalZoomRatio": {xmpArrayNone, xmpBaseRational}, "ExifVersion": {xmpArrayNone, xmpBaseText}, "ExposureBiasValue": {xmpArrayNone, xmpBaseRational}, "ExposureIndex": {xmpArrayNone, xmpBaseRational}, "ExposureMode": {xmpArrayNone, xmpBaseInteger}, "ExposureProgram": {xmpArrayNone, xmpBaseInteger}, "ExposureTime": {xmpArrayNone, xmpBaseRational}, "FNumber": {xmpArrayNone, xmpBaseRational}, "FileSource": {xmpArrayNone, xmpBaseInteger}, "Flash": {xmpArrayNone, xmpBaseOther}, "FlashEnergy": {xmpArrayNone, xmpBaseRational}, "FlashpixVersion": {xmpArrayNone, xmpBaseText}, "FocalLength": {xmpArrayNone, xmpBaseRational}, "FocalLengthIn35mmFilm": {xmpArrayNone, xmpBaseInteger}, "FocalPlaneResolutionUnit": {xmpArrayNone, xmpBaseInteger}, "FocalPlaneXResolution": {xmpArrayNone, xmpBaseRational}, "FocalPlaneYResolution": {xmpArrayNone, xmpBaseRational}, "GPSAltitude": {xmpArrayNone, xmpBaseRational}, "GPSAltitudeRef": {xmpArrayNone, xmpBaseInteger}, "GPSAreaInformation": {xmpArrayNone, xmpBaseText}, "GPSDOP": {xmpArrayNone, xmpBaseRational}, "GPSDestBearing": {xmpArrayNone, xmpBaseRational}, "GPSDestBearingRef": {xmpArrayNone, xmpBaseText}, "GPSDestDistance": {xmpArrayNone, xmpBaseRational}, "GPSDestDistanceRef": {xmpArrayNone, xmpBaseText}, "GPSDestLatitude": {xmpArrayNone, xmpBaseText}, "GPSDestLongitude": {xmpArrayNone, xmpBaseText}, "GPSDifferential": {xmpArrayNone, xmpBaseInteger}, "GPSImgDirection": {xmpArrayNone, xmpBaseRational}, "GPSImgDirectionRef": {xmpArrayNone, xmpBaseText}, "GPSLatitude": {xmpArrayNone, xmpBaseText}, "GPSLongitude": {xmpArrayNone, xmpBaseText}, "GPSMapDatum": {xmpArrayNone, xmpBaseText}, "GPSMeasureMode": {xmpArrayNone, xmpBaseInteger}, "GPSProcessingMethod": {xmpArrayNone, xmpBaseText}, "GPSSatellites": {xmpArrayNone, xmpBaseText}, "GPSSpeed": {xmpArrayNone, xmpBaseRational}, "GPSSpeedRef": {xmpArrayNone, xmpBaseText}, "GPSStatus": {xmpArrayNone, xmpBaseText}, "GPSTimeStamp": {xmpArrayNone, xmpBaseDate}, "GPSTrack": {xmpArrayNone, xmpBaseRational}, "GPSTrackRef": {xmpArrayNone, xmpBaseText}, "GPSVersionID": {xmpArrayNone, xmpBaseText}, "GainControl": {xmpArrayNone, xmpBaseInteger}, "ISOSpeedRatings": {xmpArraySeq, xmpBaseInteger}, "ImageUniqueID": {xmpArrayNone, xmpBaseText}, "LightSource": {xmpArrayNone, xmpBaseInteger}, "MaxApertureValue": {xmpArrayNone, xmpBaseRational}, "MeteringMode": {xmpArrayNone, xmpBaseInteger}, "OECF": {xmpArrayNone, xmpBaseOther}, "PixelXDimension": {xmpArrayNone, xmpBaseInteger}, "PixelYDimension": {xmpArrayNone, xmpBaseInteger}, "RelatedSoundFile": {xmpArrayNone, xmpBaseText}, "Saturation": {xmpArrayNone, xmpBaseInteger}, "SceneCaptureType": {xmpArrayNone, xmpBaseInteger}, "SceneType": {xmpArrayNone, xmpBaseInteger}, "SensingMethod": {xmpArrayNone, xmpBaseInteger}, "Sharpness": {xmpArrayNone, xmpBaseInteger}, "ShutterSpeedValue": {xmpArrayNone, xmpBaseRational}, "SpatialFrequencyResponse": {xmpArrayNone, xmpBaseOther}, "SpectralSensitivity": {xmpArrayNone, xmpBaseText}, "SubjectArea": {xmpArraySeq, xmpBaseInteger}, "SubjectDistance": {xmpArrayNone, xmpBaseRational}, "SubjectDistanceRange": {xmpArrayNone, xmpBaseInteger}, "SubjectLocation": {xmpArraySeq, xmpBaseInteger}, "UserComment": {xmpArrayLangAlt, xmpBaseText}, "WhiteBalance": {xmpArrayNone, xmpBaseInteger}}, "http://ns.adobe.com/exif/1.0/aux/": {"ApproximateFocusDistance": {xmpArrayNone, xmpBaseRational}, "Firmware": {xmpArrayNone, xmpBaseText}, "FlashCompensation": {xmpArrayNone, xmpBaseRational}, "ImageNumber": {xmpArrayNone, xmpBaseInteger}, "Lens": {xmpArrayNone, xmpBaseText}, "LensID": {xmpArrayNone, xmpBaseText}, "LensInfo": {xmpArrayNone, xmpBaseText}, "LensSerialNumber": {xmpArrayNone, xmpBaseText}, "OwnerName": {xmpArrayNone, xmpBaseText}, "SerialNumber": {xmpArrayNone, xmpBaseText}}, "http://ns.adobe.com/pdf/1.3/": {"Author": {xmpArrayNone, xmpBaseText}, "BaseURL": {xmpArrayNone, xmpBaseText}, "Copyright": {xmpArrayNone, xmpBaseText}, "CreationDate": {xmpArrayNone, xmpBaseDate}, "Creator": {xmpArrayNone, xmpBaseText}, "Keywords": {xmpArrayNone, xmpBaseText}, "Marked": {xmpArrayNone, xmpBaseBoolean}, "ModDate": {xmpArrayNone, xmpBaseDate}, "PDFVersion": {xmpArrayNone, xmpBaseText}, "Producer": {xmpArrayNone, xmpBaseText}, "Subject": {xmpArrayNone, xmpBaseText}, "Title": {xmpArrayNone, xmpBaseText}, "Trapped": {xmpArrayNone, xmpBaseBoolean}}, "http://ns.adobe.com/photoshop/1.0/": {"AuthorsPosition": {xmpArrayNone, xmpBaseText}, "CaptionWriter": {xmpArrayNone, xmpBaseText}, "Category": {xmpArrayNone, xmpBaseText}, "City": {xmpArrayNone, xmpBaseText}, "ColorMode": {xmpArrayNone, xmpBaseInteger}, "Country": {xmpArrayNone, xmpBaseText}, "Credit": {xmpArrayNone, xmpBaseText}, "DateCreated": {xmpArrayNone, xmpBaseDate}, "DocumentAncestors": {xmpArrayBag, xmpBaseOther}, "Headline": {xmpArrayNone, xmpBaseText}, "History": {xmpArrayNone, xmpBaseText}, "ICCProfile": {xmpArrayNone, xmpBaseText}, "Instructions": {xmpArrayNone, xmpBaseText}, "Source": {xmpArrayNone, xmpBaseText}, "State": {xmpArrayNone, xmpBaseText}, "SupplementalCategories": {xmpArrayBag, xmpBaseText}, "TextLayers": {xmpArraySeq, xmpBaseOther}, "TransmissionReference": {xmpArrayNone, xmpBaseText}, "Urgency": {xmpArrayNone, xmpBaseInteger}}, "http://ns.adobe.com/tiff/1.0/": {"Artist": {xmpArrayNone, xmpBaseText}, "BitsPerSample": {xmpArraySeq, xmpBaseInteger}, "Compression": {xmpArrayNone, xmpBaseInteger}, "Copyright": {xmpArrayLangAlt, xmpBaseText}, "DateTime": {xmpArrayNone, xmpBaseDate}, "ImageDescription": {xmpArrayLangAlt, xmpBaseText}, "ImageLength": {xmpArrayNone, xmpBaseInteger}, "ImageWidth": {xmpArrayNone, xmpBaseInteger}, "Make": {xmpArrayNone, xmpBaseText}, "Model": {xmpArrayNone, xmpBaseText}, "Orientation": {xmpArrayNone, xmpBaseInteger}, "PhotometricInterpretation": {xmpArrayNone, xmpBaseInteger}, "PlanarConfiguration": {xmpArrayNone, xmpBaseInteger}, "PrimaryChromaticities": {xmpArraySeq, xmpBaseRational}, "ReferenceBlackWhite": {xmpArraySeq, xmpBaseRational}, "ResolutionUnit": {xmpArrayNone, xmpBaseInteger}, "SamplesPerPixel": {xmpArrayNone, xmpBaseInteger}, "Software": {xmpArrayNone, xmpBaseText}, "TransferFunction": {xmpArraySeq, xmpBaseInteger}, "WhitePoint": {xmpArraySeq, xmpBaseRational}, "XResolution": {xmpArrayNone, xmpBaseRational}, "YCbCrCoefficients": {xmpArraySeq, xmpBaseRational}, "YCbCrPositioning": {xmpArrayNone, xmpBaseInteger}, "YCbCrSubSampling": {xmpArraySeq, xmpBaseInteger}, "YResolution": {xmpArrayNone, xmpBaseRational}}, "http://ns.adobe.com/xap/1.0/": {"Advisory": {xmpArrayBag, xmpBaseText}, "Author": {xmpArraySeq, xmpBaseText}, "BaseURL": {xmpArrayNone, xmpBaseText}, "CreateDate": {xmpArrayNone, xmpBaseDate}, "CreatorTool": {xmpArrayNone, xmpBaseText}, "Description": {xmpArrayLangAlt, xmpBaseText}, "Format": {xmpArrayNone, xmpBaseText}, "Identifier": {xmpArrayBag, xmpBaseText}, "Keywords": {xmpArrayNone, xmpBaseText}, "Label": {xmpArrayNone, xmpBaseText}, "MetadataDate": {xmpArrayNone, xmpBaseDate}, "ModifyDate": {xmpArrayNone, xmpBaseDate}, "Nickname": {xmpArrayNone, xmpBaseText}, "PageInfo": {xmpArrayNone, xmpBaseOther}, "Rating": {xmpArrayNone, xmpBaseReal}, "Thumbnails": {xmpArrayAlt, xmpBaseOther}, "Title": {xmpArrayLangAlt, xmpBaseText}}, "http://ns.adobe.com/xap/1.0/bj/": {"JobRef": {xmpArrayBag, xmpBaseOther}}, "http://ns.adobe.com/xap/1.0/mm/": {"DerivedFrom": {xmpArrayNone, xmpBaseOther}, "DocumentAncestors": {xmpArrayBag, xmpBaseOther}, "DocumentID": {xmpArrayNone, xmpBaseText}, "History": {xmpArraySeq, xmpBaseOther}, "Ingredients": {xmpArrayBag, xmpBaseOther}, "InstanceID": {xmpArrayNone, xmpBaseText}, "LastURL": {xmpArrayNone, xmpBaseText}, "ManageTo": {xmpArrayNone, xmpBaseText}, "ManageUI": {xmpArrayNone, xmpBaseText}, "ManagedFrom": {xmpArrayNone, xmpBaseOther}, "Manager": {xmpArrayNone, xmpBaseText}, "ManagerVariant": {xmpArrayNone, xmpBaseText}, "Manifest": {xmpArrayBag, xmpBaseOther}, "OriginalDocumentID": {xmpArrayNone, xmpBaseText}, "Pantry": {xmpArrayBag, xmpBaseOther}, "RenditionClass": {xmpArrayNone, xmpBaseText}, "RenditionOf": {xmpArrayNone, xmpBaseOther}, "RenditionParams": {xmpArrayNone, xmpBaseText}, "SaveID": {xmpArrayNone, xmpBaseInteger}, "VersionID": {xmpArrayNone, xmpBaseText}, "Versions": {xmpArraySeq, xmpBaseOther}}, "http://ns.adobe.com/xap/1.0/rights/": {"Certificate": {xmpArrayNone, xmpBaseText}, "Copyright": {xmpArrayLangAlt, xmpBaseText}, "Marked": {xmpArrayNone, xmpBaseBoolean}, "Owner": {xmpArrayBag, xmpBaseText}, "UsageTerms": {xmpArrayLangAlt, xmpBaseText}, "WebStatement": {xmpArrayNone, xmpBaseText}}, "http://ns.adobe.com/xap/1.0/t/pg/": {"Colorants": {xmpArraySeq, xmpBaseOther}, "Fonts": {xmpArrayBag, xmpBaseOther}, "MaxPageSize": {xmpArrayNone, xmpBaseOther}, "NPages": {xmpArrayNone, xmpBaseInteger}, "PlateNames": {xmpArraySeq, xmpBaseText}}, "http://ns.adobe.com/xmp/1.0/DynamicMedia/": {"Tracks": {xmpArrayBag, xmpBaseOther}, "absPeakAudioFilePath": {xmpArrayNone, xmpBaseText}, "album": {xmpArrayNone, xmpBaseText}, "altTapeName": {xmpArrayNone, xmpBaseText}, "altTimecode": {xmpArrayNone, xmpBaseOther}, "artist": {xmpArrayNone, xmpBaseText}, "audioChannelType": {xmpArrayNone, xmpBaseText}, "audioCompressor": {xmpArrayNone, xmpBaseText}, "audioSampleRate": {xmpArrayNone, xmpBaseInteger}, "audioSampleType": {xmpArrayNone, xmpBaseText}, "beatSpliceParams": {xmpArrayNone, xmpBaseOther}, "cameraAngle": {xmpArrayNone, xmpBaseText}, "cameraLabel": {xmpArrayNone, xmpBaseText}, "cameraModel": {xmpArrayNone, xmpBaseText}, "cameraMove": {xmpArrayNone, xmpBaseText}, "client": {xmpArrayNone, xmpBaseText}, "comment": {xmpArrayNone, xmpBaseText}, "composer": {xmpArrayNone, xmpBaseText}, "contributedMedia": {xmpArrayBag, xmpBaseOther}, "director": {xmpArrayNone, xmpBaseText}, "directorPhotography": {xmpArrayNone, xmpBaseText}, "discNumber": {xmpArrayNone, xmpBaseText}, "duration": {xmpArrayNone, xmpBaseOther}, "engineer": {xmpArrayNone, xmpBaseText}, "fileDataRate": {xmpArrayNone, xmpBaseRational}, "genre": {xmpArrayNone, xmpBaseText}, "good": {xmpArrayNone, xmpBaseBoolean}, "instrument": {xmpArrayNone, xmpBaseText}, "introTime": {xmpArrayNone, xmpBaseOther}, "key": {xmpArrayNone, xmpBaseText}, "logComment": {xmpArrayNone, xmpBaseText}, "loop": {xmpArrayNone, xmpBaseBoolean}, "lyrics": {xmpArrayNone, xmpBaseText}, "markers": {xmpArraySeq, xmpBaseOther}, "numberOfBeats": {xmpArrayNone, xmpBaseReal}, "outCue": {xmpArrayNone, xmpBaseOther}, "partOfCompilation": {xmpArrayNone, xmpBaseBoolean}, "projectName": {xmpArrayNone, xmpBaseText}, "projectRef": {xmpArrayNone, xmpBaseOther}, "pullDown": {xmpArrayNone, xmpBaseText}, "relativePeakAudioFilePath": {xmpArrayNone, xmpBaseText}, "relativeTimestamp": {xmpArrayNone, xmpBaseOther}, "releaseDate": {xmpArrayNone, xmpBaseDate}, "resampleParams": {xmpArrayNone, xmpBaseOther}, "scaleType": {xmpArrayNone, xmpBaseText}, "scene": {xmpArrayNone, xmpBaseText}, "shotDate": {xmpArrayNone, xmpBaseDate}, "shotDay": {xmpArrayNone, xmpBaseText}, "shotLocation": {xmpArrayNone, xmpBaseText}, "shotName": {xmpArrayNone, xmpBaseText}, "shotNumber": {xmpArrayNone, xmpBaseText}, "shotSize": {xmpArrayNone, xmpBaseText}, "speakerPlacement": {xmpArrayNone, xmpBaseText}, "startTimecode": {xmpArrayNone, xmpBaseOther}, "stretchMode": {xmpArrayNone, xmpBaseText}, "takeNumber": {xmpArrayNone, xmpBaseInteger}, "tapeName": {xmpArrayNone, xmpBaseText}, "tempo": {xmpArrayNone, xmpBaseReal}, "timeScaleParams": {xmpArrayNone, xmpBaseOther}, "timeSignature": {xmpArrayNone, xmpBaseText}, "trackNumber": {xmpArrayNone, xmpBaseInteger}, "videoAlphaMode": {xmpArrayNone, xmpBaseText}, "videoAlphaPremultipleColor": {xmpArrayNone, xmpBaseOther}, "videoAlphaUnityIsTransparent": {xmpArrayNone, xmpBaseBoolean}, "videoColorSpace": {xmpArrayNone, xmpBaseText}, "videoCompressor": {xmpArrayNone, xmpBaseText}, "videoFieldOrder": {xmpArrayNone, xmpBaseText}, "videoFrameRate": {xmpArrayNone, xmpBaseText}, "videoFrameSize": {xmpArrayNone, xmpBaseOther}, "videoPixelAspectRatio": {xmpArrayNone, xmpBaseRational}, "videoPixelDepth": {xmpArrayNone, xmpBaseText}}, "http://purl.org/dc/elements/1.1/": {"contributor": {xmpArrayBag, xmpBaseText}, "coverage": {xmpArrayNone, xmpBaseText}, "creator": {xmpArraySeq, xmpBaseText}, "date": {xmpArraySeq, xmpBaseDate}, "description": {xmpArrayLangAlt, xmpBaseText}, "format": {xmpArrayNone, xmpBaseText}, "identifier": {xmpArrayNone, xmpBaseText}, "language": {xmpArrayBag, xmpBaseText}, "publisher": {xmpArrayBag, xmpBaseText}, "relation": {xmpArrayBag, xmpBaseText}, "rights": {xmpArrayLangAlt, xmpBaseText}, "source": {xmpArrayNone, xmpBaseText}, "subject": {xmpArrayBag, xmpBaseText}, "title": {xmpArrayLangAlt, xmpBaseText}, "type": {xmpArrayBag, xmpBaseText}}, "http://www.aiim.org/pdfa/ns/id/": {"amd": {xmpArrayNone, xmpBaseText}, "conformance": {xmpArrayNone, xmpBaseText}, "corr": {xmpArrayNone, xmpBaseText}, "part": {xmpArrayNone, xmpBaseInteger}, "rev": {xmpArrayNone, xmpBaseText}}}

func verifyPostScriptXObjectsPDFA1(r *model.CompliancePdfReader) (rule ViolatedRule) {
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(stream.Get("Type"))
		if !ok {
			continue
		}
		if *typeName != "XObject" {
			continue
		}
		subtypeName, ok := core.GetName(stream.Get("Subtype"))
		if !ok {
			continue
		}
		if *subtypeName == "PS" {
			return newViolatedRule("6.2.7-1", "A conforming file shall not contain any PostScript XObjects.")
		}
	}
	return rule
}

var _ Profile = (*Profile4E)(nil)

func ensureMinimumVersion(doc *docutil.Document, minor int) {
	if doc.Version.Major == 0 {
		doc.Version.Major = 1
	}
	if doc.Version.Minor < minor {
		doc.Version.Minor = minor
	}
}

type xmpPropertyValue struct {
	Namespace string
	Name      string
	Array     xmpArrayKind

	// Text is the character data of a simple value.
	Text string

	// Items are the entries of an array value.
	Items []xmpArrayItem

	// Struct is set when the value is a structure rather than a simple value.
	Struct bool
}

func stubClause6xxFormsPDFA23(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	return violations
}

// Profile3A is the implementation of the PDF/A-3A standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type Profile3A struct{ profile3 }

func stubClause6xxPDFA23b(r *model.CompliancePdfReader) ViolatedRule { return noViolation }
func setProfile2OptionDefaults(profile2Options *Profile2Options) {
	if profile2Options.Now == nil {
		profile2Options.Now = time.Now
	}
}

func fixActions(doc *docutil.Document, part int) error {
	for _, object := range doc.Objects {
		objectDict, ok := core.GetDict(object)
		if !ok {
			continue
		}
		typeObj := objectDict.Get("Type")
		if typeObj == nil {
			continue
		}
		if typeName, ok := core.GetName(typeObj); ok && typeName.String() != "Action" {
			continue
		}
		sName, ok2 := core.GetName(objectDict.Get("S"))
		if !ok2 {
			continue
		}
		switch model.PdfActionType(*sName) {
		case model.ActionTypeLaunch, model.ActionTypeSound, model.ActionTypeMovie, model.ActionTypeResetForm, model.ActionTypeImportData, model.ActionTypeJavaScript:
			objectDict.Remove("S")
		case model.ActionTypeHide, model.ActionTypeSetOCGState, model.ActionTypeRendition, model.ActionTypeTrans, model.ActionTypeGoTo3DView:
			if part == 2 {
				objectDict.Remove("S")
			}
		case model.ActionTypeNamed:
			nName, ok := core.GetName(objectDict.Get("N"))
			if !ok {
				continue
			}
			switch *nName {
			case "NextPage", "PrevPage", "FirstPage", "LastPage":
			default:
				objectDict.Remove("N")
			}
		}
	}
	return nil
}

func verifyFontEmbeddedPDFA1(font *model.PdfFont, dict *core.PdfObjectDictionary, invisibleText bool) ViolatedRule {
	const (
		ruleNo = "6.3.4-1"
		detail = "The font programs for all fonts used within a conforming file shall be embedded within that file, as defined in PDF Reference 5.8, except when the fonts are used exclusively with text rendering mode 3."
	)
	if invisibleText {
		return noViolation
	}
	pdfFontDescriptor := font.FontDescriptor()
	var s string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		s = subtypeName.String()
	}
	switch s {
	case "Type1":
		if pdfFontDescriptor.FontFile == nil {
			return newViolatedRule(ruleNo, detail)
		}
	case "TrueType":
		if pdfFontDescriptor.FontFile2 == nil {
			return newViolatedRule(ruleNo, detail)
		}
	case "Type0", "Type3":
	default:
		if pdfFontDescriptor.FontFile3 == nil {
			return newViolatedRule(ruleNo, detail)
		}
	}
	return noViolation
}
func standard2U() standardType { return standardType{part: 2, conformance: "U"} }

// NewProfile1B creates a new Profile1B with the given options.
func NewProfile1B(options *Profile1Options) *Profile1B {
	if options == nil {
		options = DefaultProfile1Options()
	}
	setProfile1OptionDefaults(options)
	return &Profile1B{profile1{options: *options, standard: standard1B()}}
}

func verifyCIDToGIDMap(dict *core.PdfObjectDictionary, clause string) ViolatedRule {
	s := clause + ".3-2"
	const (
		detail = "ISO 32000-1:2008, 9.7.4, Table 117 requires that all embedded Type 2 CIDFonts in the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-1:2008, 9.7.4, Table 117."
	)
	var str string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		str = subtypeName.String()
	}
	if str != "CIDFontType2" {
		return noViolation
	}
	if dict.Get("CIDToGIDMap") == nil {
		return newViolatedRule(s, detail)
	}
	return noViolation
}

func verifyAlternatePresentations(r *model.CompliancePdfReader, clause string) (violations []ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return violations
	}
	namesDict, ok := core.GetDict(dict.Get("Names"))
	if !ok {
		return violations
	}
	if namesDict.Get("AlternatePresentations") != nil {
		violations = append(violations, newViolatedRule(clause+"-1", "There shall be no AlternatePresentations entry in the document's name dictionary."))
	}
	return violations
}

func fixExtGStatePDFA23(doc *docutil.Document) error {
	fixExtGState := func(dict *core.PdfObjectDictionary) error {
		if dict.Get("TR") != nil {
			common.Log.Debug("ExtGState object contains TR key")
			dict.Remove("TR")
		}
		tR2 := dict.Get("TR2")
		if tR2 != nil {
			s := tR2.String()
			if s != "Default" {
				common.Log.Debug("ExtGState object contains TR2 key with value other than Default")
				dict.Set("TR2", core.MakeName("Default"))
			}
		}
		if dict.Get("HTP") != nil {
			common.Log.Debug("ExtGState object contains HTP key")
			dict.Remove("HTP")
		}
		bm := dict.Get("BM")
		if bm != nil {
			bmName, ok := core.GetName(bm)
			if !ok {
				common.Log.Debug("ExtGState object 'BM' is not a Name")
				bmName = core.MakeName("")
			}
			s := bmName.String()
			switch s {
			case "Normal", "Compatible", "Multiply", "Screen", "Overlay", "Darken", "Lighten", "ColorDodge", "ColorBurn", "HardLight", "SoftLight", "Difference", "Exclusion", "Hue", "Saturation", "Color", "Luminosity":
			default:
				dict.Set("BM", core.MakeName("Normal"))
			}
		}
		return nil
	}
	pages, ok := doc.GetPages()
	if !ok {
		return nil
	}
	for _, page := range pages {
		resources, ok := page.GetResources()
		if !ok {
			continue
		}
		extGStateDict, ok2 := core.GetDict(resources.Get("ExtGState"))
		if !ok2 {
			return nil
		}
		names := extGStateDict.Keys()
		for _, name := range names {
			dict, ok := core.GetDict(extGStateDict.Get(name))
			if !ok {
				continue
			}
			err := fixExtGState(dict)
			if err != nil {
				continue
			}
		}
	}
	for _, page := range pages {
		contents, ok := page.GetContents()
		if !ok {
			return nil
		}
		for _, content := range contents {
			data, err := content.GetData()
			if err != nil {
				continue
			}
			contentStreamParser := contentstream.NewContentStreamParser(string(data))
			ops, err := contentStreamParser.Parse()
			if err != nil {
				continue
			}
			for _, op := range *ops {
				if len(op.Params) == 0 {
					continue
				}
				_, ok := core.GetName(op.Params[0])
				if !ok {
					continue
				}
				resourcesXObject, ok2 := page.GetResourcesXObject()
				if !ok2 {
					continue
				}
				for _, name := range resourcesXObject.Keys() {
					stream, ok := core.GetStream(resourcesXObject.Get(name))
					if !ok {
						continue
					}
					resourcesDict, ok := core.GetDict(stream.Get("Resources"))
					if !ok {
						continue
					}
					extGStateDict, ok := core.GetDict(resourcesDict.Get("ExtGState"))
					if !ok {
						continue
					}
					for _, name := range extGStateDict.Keys() {
						dict, ok := core.GetDict(extGStateDict.Get(name))
						if !ok {
							continue
						}
						err := fixExtGState(dict)
						if err != nil {
							continue
						}
					}
				}
			}
		}
	}
	return nil
}

// Profile4F is the implementation of the PDF/A-4F standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type Profile4F struct{ profile4 }

func isPredefinedNamespace(s string) bool { _, ok := predefinedNamespaces[s]; return ok }

type profile2 struct {
	standard standardType
	options  Profile2Options
}
type documentImages struct {
	hasRGB, hasCMYK, hasGray bool
	softMasks                map[core.PdfObject]struct{}
	images                   []*imageInfo
}

func fixInteractiveFormPDFA1(doc *docutil.Document) error {
	for _, object := range doc.Objects {
		objectDict, ok := core.GetDict(object)
		if !ok {
			continue
		}
		typeObj := objectDict.Get("Type")
		if typeObj == nil {
			continue
		}
		if typeName, ok := core.GetName(typeObj); ok && typeName.String() != "AcroForm" {
			continue
		}
		needAppearances, ok2 := core.GetBool(objectDict.Get("NeedAppearances"))
		if ok2 {
			if bool(*needAppearances) {
				objectDict.Set("NeedAppearances", core.MakeBool(false))
			}
		}
		a := objectDict.Get("A")
		if a != nil {
			objectDict.Remove("A")
		}
		fieldsArray, ok2 := core.GetArray(objectDict.Get("Fields"))
		if ok2 {
			for n := 0; n < fieldsArray.Len(); n++ {
				dict, ok := core.GetDict(fieldsArray.Get(n))
				if !ok {
					continue
				}
				if dict.Get("AA") != nil {
					dict.Remove("AA")
				}
			}
		}
	}
	return nil
}

func fixAnnotationColors(doc *docutil.Document, cmykDefault bool) error {
	pages, ok := doc.GetPages()
	if !ok {
		return nil
	}
	for _, page := range pages {
		annotsArray, ok := core.GetArray(page.Object.Get("Annots"))
		if !ok {
			continue
		}
		for _, obj := range annotsArray.Elements() {
			dict, ok := core.GetDict(obj)
			if !ok {
				continue
			}
			c := dict.Get("C")
			if c == nil {
				continue
			}
			cArray, ok := core.GetArray(c)
			if !ok {
				continue
			}
			asFloat64Slice, err := cArray.GetAsFloat64Slice()
			if err != nil {
				return err
			}
			switch cArray.Len() {
			case 0, 1:
				if cmykDefault {
					dict.Set("C", core.MakeArrayFromIntegers([]int{1, 1, 1, 1}))
				} else {
					dict.Set("C", core.MakeArrayFromIntegers([]int{1, 1, 1}))
				}
			case 3:
				if cmykDefault {
					c, m, y, k := color.RGBToCMYK(uint8(asFloat64Slice[0]*255), uint8(asFloat64Slice[1]*255), uint8(asFloat64Slice[2]*255))
					dict.Set("C", core.MakeArrayFromFloats([]float64{float64(c) / 255, float64(m) / 255, float64(y) / 255, float64(k) / 255}))
				}
			case 4:
				if !cmykDefault {
					red, green, blue := color.CMYKToRGB(uint8(asFloat64Slice[0]*255), uint8(asFloat64Slice[1]*255), uint8(asFloat64Slice[2]*255), uint8(asFloat64Slice[3]*255))
					dict.Set("C", core.MakeArrayFromFloats([]float64{float64(red) / 255, float64(green) / 255, float64(blue) / 255}))
				}
			}
		}
	}
	return nil
}

func verifyImplementationLimitsPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var (
		reported6_1_12_1, reported6_1_12_2, reported6_1_12_3, reported6_1_12_4, reported6_1_12_5, reported6_1_12_11, reported6_1_12_9 bool
		visit                                                                                                                         func(core.PdfObject)
	)
	visit = func(obj core.PdfObject) {
		switch v := obj.(type) {
		case *core.PdfObjectInteger:
			if !reported6_1_12_1 && (int64(*v) > math.MaxInt32 || int64(*v) < -math.MaxInt32) {
				violations = append(violations, newViolatedRule("6.1.12-1", "Largest Integer value is 2,147,483,647. Smallest integer value is -2,147,483,648."))
				reported6_1_12_1 = true
			}
		case *core.PdfObjectFloat:
			if !reported6_1_12_2 && (math.Abs(float64(*v)) > 32767.0) {
				violations = append(violations, newViolatedRule("6.1.12-2", "Absolute real value must be less than or equal to 32767.0."))
			}
		case *core.PdfObjectString:
			if !reported6_1_12_3 && len([]byte(v.Str())) > 65535 {
				violations = append(violations, newViolatedRule("6.1.12-3", "Maximum length of a string (in bytes) is 65535."))
				reported6_1_12_3 = true
			}
		case *core.PdfObjectName:
			if !reported6_1_12_4 && len([]byte(*v)) > 127 {
				violations = append(violations, newViolatedRule("6.1.12-4", "Maximum length of a name (in bytes) is 127."))
				reported6_1_12_4 = true
			}
		case *core.PdfObjectArray:
			if !reported6_1_12_5 && v.Len() > 8191 {
				violations = append(violations, newViolatedRule("6.1.12-5", "Maximum length of an array (in elements) is 8191."))
				reported6_1_12_5 = true
			}
			for _, obj := range v.Elements() {
				visit(obj)
			}
			if !reported6_1_12_9 && (v.Len() == 4 || v.Len() == 5) {
				name, ok := core.GetName(v.Get(0))
				if !ok {
					return
				}
				if *name != "DeviceN" {
					return
				}
				obj := v.Get(1)
				obj = core.TraceToDirectObject(obj)
				arr, ok := core.GetArray(obj)
				if !ok {
					return
				}
				if arr.Len() > 8 {
					violations = append(violations, newViolatedRule("6.1.12-9", "Maximum number of DeviceN components is 8."))
					reported6_1_12_9 = true
				}
			}
		case *core.PdfObjectDictionary:
			names := v.Keys()
			if !reported6_1_12_11 && len(names) > 4095 {
				violations = append(violations, newViolatedRule("6.1.12-11", "Maximum capacity of a dictionary (in entries) is 4095."))
				reported6_1_12_11 = true
			}
			for i, name := range names {
				visit(&names[i])
				visit(v.Get(name))
			}
		case *core.PdfObjectStream:
			visit(v.PdfObjectDictionary)
		case *core.PdfObjectStreams:
			for _, obj := range v.Elements() {
				visit(obj)
			}
		case *core.PdfObjectReference:
			visit(v.Resolve())
		}
	}
	objectNums := r.GetObjectNums()
	if len(objectNums) > 8388607 {
		violations = append(violations, newViolatedRule("6.1.12-7", "Maximum number of indirect objects in a PDF file is 8,388,607."))
	}
	for _, objectNum := range objectNums {
		obj, err := r.GetIndirectObjectByNumber(objectNum)
		if err != nil {
			continue
		}
		obj2 := core.TraceToDirectObject(obj)
		visit(obj2)
	}
	return violations
}

var predefinedPropertyOverrides = map[string]map[string]xmpPropertyType{"http://ns.adobe.com/photoshop/1.0/": {"SupplementalCategories": {xmpArrayNone, xmpBaseText}}}

func fixPermissions(doc *docutil.Document) error {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return errors.New("catalog not found")
	}
	permsDict, ok := core.GetDict(catalog.Object.Get("Perms"))
	if ok {
		dict := core.MakeDict()
		names := permsDict.Keys()
		for _, name := range names {
			if name.String() == "UR3" || name.String() == "DocMDP" {
				dict.Set(name, permsDict.Get(name))
			}
		}
		catalog.Object.Set("Perms", dict)
	}
	return nil
}

var _ Profile = (*Profile3A)(nil)

func readContent(dec *xml.Decoder) (s string, contentKind xmpContentKind, err error) {
	var builder, builder2 strings.Builder
	ok, found := false, false
	n, count := 0, 0
	for {
		tok, err := dec.Token()
		if err != nil {
			return builder.String(), contentKind, err
		}
		switch tok := tok.(type) {
		case xml.CharData:
			switch {
			case count > 0:
				builder2.Write(tok)
			case n == 0:
				builder.Write(tok)
			}
		case xml.StartElement:
			if tok.Name.Space == rdfNamespace && tok.Name.Local == "value" {
				ok = true
				count++
			} else if count == 0 {
				found = true
			}
			n++
		case xml.EndElement:
			if n == 0 {
				switch {
				case ok:
					return strings.TrimSpace(builder2.String()), xmpContentValue, nil
				case found:
					return "", xmpContentStruct, nil
				}
				return strings.TrimSpace(builder.String()), xmpContentText, nil
			}
			if count > 0 {
				count--
			}
			n--
		}
	}
}

func verifyImplementationLimitsPDFA23(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var (
		reported6_1_13_1, reported6_1_13_2, reported6_1_13_3, reported6_1_13_4, reported6_1_13_9 bool
		visit                                                                                    func(core.PdfObject)
	)
	visit = func(obj core.PdfObject) {
		switch v := obj.(type) {
		case *core.PdfObjectInteger:
			if !reported6_1_13_1 && (int64(*v) > math.MaxInt32 || int64(*v) < -math.MaxInt32) {
				violations = append(violations, newViolatedRule("6.1.13-1", "Largest Integer value is 2,147,483,647. Smallest integer value is -2,147,483,648."))
				reported6_1_13_1 = true
			}
		case *core.PdfObjectFloat:
			if !reported6_1_13_2 && (math.Abs(float64(*v)) > math.MaxFloat32) {
				violations = append(violations, newViolatedRule("6.1.13-2", "A conforming file shall not contain any real number outside the range of +/-3.403 x 10^38."))
			}
		case *core.PdfObjectString:
			if !reported6_1_13_3 && len([]byte(v.Str())) > 32767 {
				violations = append(violations, newViolatedRule("6.1.13-3", "Maximum length of a string (in bytes) is 32767."))
				reported6_1_13_3 = true
			}
		case *core.PdfObjectName:
			if !reported6_1_13_4 && len([]byte(*v)) > 127 {
				violations = append(violations, newViolatedRule("6.1.13-4", "Maximum length of a name (in bytes) is 127."))
				reported6_1_13_4 = true
			}
		case *core.PdfObjectArray:
			for _, obj := range v.Elements() {
				visit(obj)
			}
			if !reported6_1_13_9 && (v.Len() == 4 || v.Len() == 5) {
				name, ok := core.GetName(v.Get(0))
				if !ok {
					return
				}
				if *name != "DeviceN" {
					return
				}
				obj := v.Get(1)
				obj = core.TraceToDirectObject(obj)
				arr, ok := core.GetArray(obj)
				if !ok {
					return
				}
				if arr.Len() > 32 {
					violations = append(violations, newViolatedRule("6.1.13-9", "Maximum number of DeviceN components is 32."))
					reported6_1_13_9 = true
				}
			}
		case *core.PdfObjectDictionary:
			names := v.Keys()
			for i, name := range names {
				visit(&names[i])
				visit(v.Get(name))
			}
		case *core.PdfObjectStream:
			visit(v.PdfObjectDictionary)
		case *core.PdfObjectStreams:
			for _, obj := range v.Elements() {
				visit(obj)
			}
		case *core.PdfObjectReference:
			visit(v.Resolve())
		}
	}
	objectNums := r.GetObjectNums()
	if len(objectNums) > 8388607 {
		violations = append(violations, newViolatedRule("6.1.13-7", "Maximum number of indirect objects in a PDF file is 8,388,607."))
	}
	for _, objectNum := range objectNums {
		obj, err := r.GetIndirectObjectByNumber(objectNum)
		if err != nil {
			continue
		}
		obj2 := core.TraceToDirectObject(obj)
		visit(obj2)
	}
	return violations
}

func verifyMetadataPDFA1(r *model.CompliancePdfReader, std standardType, unused bool) (violations []ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return []ViolatedRule{newViolatedRule("6.7.2-1", "catalog not found.")}
	}
	metadata := dict.Get("Metadata")
	if metadata == nil {
		return []ViolatedRule{newViolatedRule("6.7.2-1", "no 'Metadata' key found in the document catalog."), newViolatedRule("6.7.3-1", "If [a document information dictionary appears in a document], then all of its entries that have analogous properties in predefined XMP schemas … shall also be embedded in the file in XMP form with equivalent values.")}
	}
	metadataStream, ok := core.GetStream(metadata)
	if !ok {
		return []ViolatedRule{newViolatedRule("6.7.2-2", "catalog 'Metadata' object is not a stream."), newViolatedRule("6.7.3-1", "If [a document information dictionary appears in a document], then all of its entries that have analogous properties in predefined XMP schemas … shall also be embedded in the file in XMP form with equivalent values.")}
	}
	if metadataStream.Get("Filter") != nil {
		violations = append(violations, newViolatedRule("6.7.2-2", "Metadata object stream dictionaries shall not contain the Filter key."))
	}
	xmp, err := xmputil.LoadDocument(metadataStream.Stream)
	if err != nil {
		return []ViolatedRule{newViolatedRule("6.7.9-1", "The metadata stream shall conform to XMP Specification and well formed PDFAExtension Schema for all extensions.")}
	}
	seen := map[string]struct{}{}
	for _, prop := range parseXMPProperties(xmp.Packet()) {
		if isPredefinedNamespace(prop.Namespace) {
			continue
		}
		seen[prop.Namespace] = struct{}{}
	}
	found := true
	pdfaExtensionSchemas, err := xmp.GetPdfaExtensionSchemas()
	if err != nil {
		found = false
	} else {
		for s := range seen {
			var ok bool
			for i := range pdfaExtensionSchemas {
				if s == pdfaExtensionSchemas[i].NamespaceURI {
					ok = true
					break
				}
			}
			if !ok {
				found = false
				break
			}
		}
	}
	if !found {
		violations = append(violations, newViolatedRule("6.7.9-2", "Properties specified in XMP form shall use either the predefined schemas defined in XMP Specification, or extension schemas that comply with XMP Specification."))
	}
	if !validateXMPProperties(xmp.Packet(), pdfaExtensionSchemas, true) {
		violations = append(violations, newViolatedRule("6.7.9-3", "The value of an XMP property does not have the type its schema declares for it."))
	}
	if pdfInfo, err := r.GetPdfInfo(); err == nil {
		violations = append(violations, verifyInfoMatchesXMP(pdfInfo, xmp)...)
	}
	pdfAID, ok := xmp.GetPdfAID()
	if !ok {
		violations = append(violations, newViolatedRule("6.7.11-1", "The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema."))
	} else {
		if pdfAID.Part != std.part {
			violations = append(violations, newViolatedRule("6.7.11-2", "The value of pdfaid:part shall be the part number of ISO 19005 to which the file conforms."))
		}
		if std.conformance == "A" && pdfAID.Conformance != "A" {
			violations = append(violations, newViolatedRule("6.7.11-3", "A Level A conforming file shall specify the value of pdfaid:conformance as A. A Level B conforming file shall specify the value of pdfaid:conformance as B."))
		} else if std.conformance == "B" && (pdfAID.Conformance != "A" && pdfAID.Conformance != "B") {
			violations = append(violations, newViolatedRule("6.7.11-3", "A Level A conforming file shall specify the value of pdfaid:conformance as A. A Level B conforming file shall specify the value of pdfaid:conformance as B."))
		}
	}
	return violations
}

func verifyAnnotationsPDFA23(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_3_1_1, reported6_3_2_1, reported6_3_2_2, reported6_3_2_3, reported6_3_3_2, reported6_4_1_1, reported6_4_1_1_2 bool
	allReported := func() bool {
		return reported6_3_1_1 && reported6_3_2_1 && reported6_3_2_2 && reported6_3_2_3 && reported6_3_3_2 && reported6_4_1_1 && reported6_4_1_1_2
	}
	for _, page := range r.PageList {
		annotations, err := page.GetAnnotations()
		if err != nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("loading annotations failed: %v", err)
			}
			continue
		}
		for _, annotation := range annotations {
			if !reported6_3_1_1 {
				switch annotation.GetContext().(type) {
				case *model.PdfAnnotationScreen, *model.PdfAnnotation3D, *model.PdfAnnotationSound, *model.PdfAnnotationMovie, nil:
					violations = append(violations, newViolatedRule("6.3.1-1", "Annotation types not defined in PDF Reference shall not be permitted. Additionally, the 3D, Sound, Screen and Movie types shall not be permitted."))
					reported6_3_1_1 = true
					if allReported() {
						return violations
					}
				}
			}
			dict, ok := core.GetDict(annotation.GetContainingPdfObject())
			if !ok {
				continue
			}
			_, found := annotation.GetContext().(*model.PdfAnnotationPopup)
			if !found && !reported6_3_2_1 {
				_, ok := core.GetIntVal(dict.Get("F"))
				if !ok {
					violations = append(violations, newViolatedRule("6.3.2-1", "Except for annotation dictionaries whose Subtype value is Popup, all annotation dictionaries shall contain the F key."))
					reported6_3_2_1 = true
					if allReported() {
						return violations
					}
				}
			}
			if !reported6_3_2_2 {
				f, ok := core.GetIntVal(dict.Get("F"))
				if ok && !(f&4 == 4 && f&1 == 0 && f&2 == 0 && f&32 == 0 && f&256 == 0) {
					violations = append(violations, newViolatedRule("6.3.2-2", "If present, the F key's Print flag bit shall be set to 1 and its Hidden, Invisible, ToggleNoView, and NoView flag bits shall be set to 0."))
					reported6_3_2_2 = true
					if allReported() {
						return violations
					}
				}
			}
			_, ok2 := annotation.GetContext().(*model.PdfAnnotationText)
			if ok2 && !reported6_3_2_3 {
				f, ok := core.GetIntVal(dict.Get("F"))
				if ok && !(f&8 == 8 && f&16 == 16) {
					violations = append(violations, newViolatedRule("6.3.2-3", "Text annotation has one of the flags NoZoom or NoRotate set to 0."))
					reported6_3_2_3 = true
					if allReported() {
						return violations
					}
				}
			}
			if !reported6_3_3_2 {
				apDict, ok := core.GetDict(dict.Get("AP"))
				if ok {
					n := apDict.Get("N")
					if n == nil || len(apDict.Keys()) > 1 {
						violations = append(violations, newViolatedRule("6.3.3-2", "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key. If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary; otherwise the value of the N key shall be an appearance stream."))
						reported6_3_3_2 = true
						if allReported() {
							return violations
						}
						continue
					}
					_, ok := annotation.GetContext().(*model.PdfAnnotationWidget)
					if ok {
						ftName, ok := core.GetName(dict.Get("FT"))
						if ok && *ftName == "Btn" {
							if _, ok := core.GetDict(n); !ok {
								violations = append(violations, newViolatedRule("6.3.3-2", "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key. If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary; otherwise the value of the N key shall be an appearance stream."))
								reported6_3_3_2 = true
								if allReported() {
									return violations
								}
								continue
							}
						}
					}
					_, ok2 := core.GetStream(n)
					if !ok2 {
						violations = append(violations, newViolatedRule("6.3.3-2", "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key. If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary; otherwise the value of the N key shall be an appearance stream."))
						reported6_3_3_2 = true
						if allReported() {
							return violations
						}
						continue
					}
				}
			}
			pdfAnnotationWidget, ok3 := annotation.GetContext().(*model.PdfAnnotationWidget)
			if !ok3 {
				continue
			}
			if !reported6_4_1_1 {
				if pdfAnnotationWidget.A != nil {
					violations = append(violations, newViolatedRule("6.4.1-1", "A Widget annotation dictionary shall not include an A entry."))
					reported6_4_1_1 = true
					if allReported() {
						return violations
					}
				}
			}
			if !reported6_4_1_1_2 {
				if pdfAnnotationWidget.AA != nil {
					violations = append(violations, newViolatedRule("6.4.1-1", "A Widget annotation dictionary shall not include an AA entry for an additional-actions dictionary."))
					reported6_4_1_1_2 = true
					if allReported() {
						return violations
					}
				}
			}
		}
	}
	return violations
}

const (
	xmpContentText xmpContentKind = iota
	xmpContentValue
	xmpContentStruct
)

func verifyFontEmbedded(font *model.PdfFont, dict *core.PdfObjectDictionary, invisibleText bool, clause string) ViolatedRule {
	s := clause + ".4-1"
	const (
		detail = "The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-1:2008, 9.9."
	)
	if invisibleText {
		return noViolation
	}
	pdfFontDescriptor := font.FontDescriptor()
	var str string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		str = subtypeName.String()
	}
	switch str {
	case "Type1":
		if pdfFontDescriptor.FontFile == nil {
			return newViolatedRule(s, detail)
		}
	case "TrueType":
		if pdfFontDescriptor.FontFile2 == nil {
			return newViolatedRule(s, detail)
		}
	case "Type0", "Type3":
	default:
		if pdfFontDescriptor.FontFile3 == nil {
			return newViolatedRule(s, detail)
		}
	}
	return noViolation
}

type documentColorspaceOptimizeFunc func(doc *docutil.Document, imgs []*docutil.Image) error

func verifyMetadataPDFA4(r *model.CompliancePdfReader, std standardType, unused bool) (violations []ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return []ViolatedRule{newViolatedRule("6.7.2.1-1", "catalog not found.")}
	}
	metadata := dict.Get("Metadata")
	if metadata == nil {
		return []ViolatedRule{newViolatedRule("6.7.2.1", "The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream")}
	}
	metadataStream, ok := core.GetStream(metadata)
	if !ok {
		return []ViolatedRule{newViolatedRule("6.7.2.1-1", "The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream")}
	}
	xmp, err := xmputil.LoadDocument(metadataStream.Stream)
	if err != nil {
		violations = []ViolatedRule{newViolatedRule("6.7.2.1-4", "All content of all XMP packets located in any metadata stream present in the PDF shall be well-formed")}
		if xmputil.PacketEncoding(metadataStream.Stream) != xmputil.EncodingUTF8 {
			violations = append(violations, newViolatedRule("6.7.2.1-5", "All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8"))
		}
		return violations
	}
	if xmp.Encoding() != xmputil.EncodingUTF8 {
		violations = append(violations, newViolatedRule("6.7.2.1-5", "All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8"))
	}
	pdfAID, ok := xmp.GetPdfAID()
	if !ok {
		violations = append(violations, newViolatedRule("6.7.3-1", "The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema."))
	} else {
		if pdfAID.Part != std.part {
			violations = append(violations, newViolatedRule("6.7.3-2", "The value of pdfaid:part shall be the part number of ISO 19005 to which the file conforms."))
		}
		if std.conformance == "" && pdfAID.Conformance != "" {
			violations = append(violations, newViolatedRule("6.7.3-3", "A conforming PDF/A-4 file shall specify the value of pdfaid:conformance as nil. A Level E conforming file shall specify the value of pdfaid:conformance as E. A Level F conforming file shall specify the value of pdfaid:conformance as F."))
		} else if std.conformance == "E" && pdfAID.Conformance != "E" {
			violations = append(violations, newViolatedRule("6.7.3-3", "A conforming PDF/A-4 file shall specify the value of pdfaid:conformance as nil. A Level E conforming file shall specify the value of pdfaid:conformance as E. A Level F conforming file shall specify the value of pdfaid:conformance as F."))
		} else if std.conformance == "F" && pdfAID.Conformance != "F" {
			violations = append(violations, newViolatedRule("6.7.3-3", "A conforming PDF/A-4 file shall specify the value of pdfaid:conformance as nil. A Level E conforming file shall specify the value of pdfaid:conformance as E. A Level F conforming file shall specify the value of pdfaid:conformance as F."))
		}
		if pdfAID.Rev < 1000 || pdfAID.Rev > 9999 {
			violations = append(violations, newViolatedRule("6.7.3-5", "The value of pdfaid:rev shall be the four digit year."))
		}
	}
	return violations
}

// NewProfile4E creates a new Profile4E with the given options.
func NewProfile4E(options *Profile4Options) *Profile4E {
	if options == nil {
		options = DefaultProfile4Options()
	}
	setProfile4OptionDefaults(options)
	return &Profile4E{profile4{options: *options, standard: standard4E()}}
}

var _ Profile = (*Profile4F)(nil)

func fixExtGStatePDFA1(doc *docutil.Document) error {
	fixExtGState := func(dict *core.PdfObjectDictionary) error {
		if sMask := dict.Get("SMask"); sMask != nil {
			dict.Set("SMask", core.MakeName("None"))
		}
		ca := dict.Get("CA")
		if ca != nil {
			f, err := core.GetNumberAsFloat(ca)
			if err != nil {
				common.Log.Debug("ExtGState object CA value is not a float: %v", err)
				f = 0
			}
			if f != 1.0 {
				dict.Set("CA", core.MakeFloat(1.0))
			}
		}
		ca = dict.Get("ca")
		if ca != nil {
			f, err := core.GetNumberAsFloat(ca)
			if err != nil {
				common.Log.Debug("ExtGState object 'ca' value is not a float: %v", err)
				f = 0
			}
			if f != 1.0 {
				dict.Set("ca", core.MakeFloat(1.0))
			}
		}
		bm := dict.Get("BM")
		if bm != nil {
			bmName, ok := core.GetName(bm)
			if !ok {
				common.Log.Debug("ExtGState object 'BM' is not a Name")
				bmName = core.MakeName("")
			}
			s := bmName.String()
			switch s {
			case "Normal", "Compatible":
			default:
				dict.Set("BM", core.MakeName("Normal"))
			}
		}
		tr := dict.Get("TR")
		if tr != nil {
			common.Log.Debug("ExtGState object contains TR key")
			dict.Remove("TR")
		}
		tR2 := dict.Get("TR2")
		if tR2 != nil {
			s := tR2.String()
			if s != "Default" {
				common.Log.Debug("ExtGState object contains TR2 key with value other than Default")
				dict.Set("TR2", core.MakeName("Default"))
			}
		}
		return nil
	}
	pages, ok := doc.GetPages()
	if !ok {
		return nil
	}
	for _, page := range pages {
		resources, ok := page.GetResources()
		if !ok {
			continue
		}
		extGStateDict, ok2 := core.GetDict(resources.Get("ExtGState"))
		if !ok2 {
			return nil
		}
		names := extGStateDict.Keys()
		for _, name := range names {
			dict, ok := core.GetDict(extGStateDict.Get(name))
			if !ok {
				continue
			}
			err := fixExtGState(dict)
			if err != nil {
				continue
			}
		}
	}
	for _, page := range pages {
		contents, ok := page.GetContents()
		if !ok {
			return nil
		}
		for _, content := range contents {
			data, err := content.GetData()
			if err != nil {
				continue
			}
			contentStreamParser := contentstream.NewContentStreamParser(string(data))
			ops, err := contentStreamParser.Parse()
			if err != nil {
				continue
			}
			for _, op := range *ops {
				if len(op.Params) == 0 {
					continue
				}
				_, ok := core.GetName(op.Params[0])
				if !ok {
					continue
				}
				resourcesXObject, ok2 := page.GetResourcesXObject()
				if !ok2 {
					continue
				}
				for _, name := range resourcesXObject.Keys() {
					stream, ok := core.GetStream(resourcesXObject.Get(name))
					if !ok {
						continue
					}
					resourcesDict, ok := core.GetDict(stream.Get("Resources"))
					if !ok {
						continue
					}
					extGStateDict, ok := core.GetDict(resourcesDict.Get("ExtGState"))
					if !ok {
						continue
					}
					for _, name := range extGStateDict.Keys() {
						dict, ok := core.GetDict(extGStateDict.Get(name))
						if !ok {
							continue
						}
						err := fixExtGState(dict)
						if err != nil {
							continue
						}
					}
				}
			}
		}
	}
	return nil
}

type xmpArrayItem struct {
	Text   string
	Struct bool
}

func isValidXMPDate(s string) bool {
	for _, str := range []string{"2006", "2006-01", "2006-01-02", "2006-01-02T15:04Z07:00", "2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05.999999999Z07:00", "2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02T15:04:05.999999999"} {
		if _, err := time.Parse(str, s); err == nil {
			return true
		}
	}
	return false
}

const (
	rdfNamespace = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	xmlNamespace = "http://www.w3.org/XML/1998/namespace"
)

func stubIndirectObjectsPDFA1(r *model.CompliancePdfReader) []ViolatedRule { return nil }
func fixOptionalContent(doc *docutil.Document) error {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return errors.New("catalog not found")
	}
	ocPropertiesDict, ok := core.GetDict(catalog.Object.Get("OCProperties"))
	if !ok {
		return nil
	}
	dDict, ok := core.GetDict(ocPropertiesDict.Get("D"))
	if ok {
		if dDict.Get("AS") != nil {
			dDict.Remove("AS")
		}
	}
	configsArray, ok := core.GetArray(ocPropertiesDict.Get("Configs"))
	if ok {
		for n := 0; n < configsArray.Len(); n++ {
			dict, ok := core.GetDict(configsArray.Get(n))
			if !ok {
				continue
			}
			if dict.Get("AS") != nil {
				dict.Remove("AS")
			}
		}
	}
	return nil
}

func verifyPermissions(r *model.CompliancePdfReader, clause string) (violations []ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return violations
	}
	permsDict, ok := core.GetDict(dict.Get("Perms"))
	if !ok {
		return violations
	}
	names := permsDict.Keys()
	for _, name := range names {
		if name.String() != "UR3" && name.String() != "DocMDP" {
			violations = append(violations, newViolatedRule("6.1.12-1", "No keys other than UR3 and DocMDP shall be present in a permissions dictionary."))
		}
	}
	return violations
}

func verifyExtGState(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_2_5_1, reported6_2_5_2, reported6_2_5_3, reported6_2_5_4, reported6_2_5_5, reported6_2_10_1, reported6_2_10_1_2 bool
	allReported := func() bool {
		return reported6_2_5_1 && reported6_2_5_2 && reported6_2_5_3 && reported6_2_5_4 && reported6_2_5_5 && reported6_2_10_1 && reported6_2_10_1_2
	}
	checkExtGState := func(dict *core.PdfObjectDictionary) bool {
		if !reported6_2_5_1 && dict.Get("TR") != nil {
			reported6_2_5_1 = true
			violations = append(violations, newViolatedRule("6.2.5-1", "An ExtGState dictionary shall not contain the TR key."))
		}
		if tR2 := dict.Get("TR2"); !reported6_2_5_2 && tR2 != nil {
			tR2Name, ok := core.GetName(tR2)
			if !ok || (ok && *tR2Name != "Default") {
				reported6_2_5_2 = true
				violations = append(violations, newViolatedRule("6.2.5-2", "An ExtGState dictionary shall not contain the TR2 key with a value other than Default."))
				if allReported() {
					return true
				}
			}
		}
		if !reported6_2_5_3 && dict.Get("HTP") != nil {
			reported6_2_5_3 = true
			violations = append(violations, newViolatedRule("6.2.5-3", "An ExtGState dictionary shall not contain the HTP key."))
		}
		htDict, ok := core.GetDict(dict.Get("HT"))
		if ok {
			if halftoneType := htDict.Get("HalftoneType"); !reported6_2_5_4 && halftoneType != nil {
				n, ok := core.GetInt(halftoneType)
				if !ok || (ok && !(*n == 1 || *n == 5)) {
					violations = append(violations, newViolatedRule(" 6.2.5-4", "All halftones in a conforming PDF/A-2 file shall have the value 1 or 5 for the HalftoneType key."))
					if allReported() {
						return true
					}
				}
			}
			if halftoneName := htDict.Get("HalftoneName"); !reported6_2_5_5 && halftoneName != nil {
				reported6_2_5_5 = true
				violations = append(violations, newViolatedRule("6.2.5-5", "Halftones in a conforming PDF/A-2 file shall not contain a HalftoneName key."))
				if allReported() {
					return true
				}
			}
		}
		_, ok2 := firstOutputIntent(r)
		var reported6_2_10_2 bool
		groupDict, ok := core.GetDict(dict.Get("Group"))
		if ok {
			_, ok := core.GetName(groupDict.Get("CS"))
			if ok {
				reported6_2_10_2 = true
			}
		}
		if bm := dict.Get("BM"); !reported6_2_10_1 && !reported6_2_10_1_2 && bm != nil {
			bmName, ok := core.GetName(bm)
			if ok {
				switch bmName.String() {
				case "Normal", "Compatible", "Multiply", "Screen", "Overlay", "Darken", "Lighten", "ColorDodge", "ColorBurn", "HardLight", "SoftLight", "Difference", "Exclusion", "Hue", "Saturation", "Color", "Luminosity":
				default:
					reported6_2_10_1 = true
					violations = append(violations, newViolatedRule("6.2.10-1", "Only blend modes that are specified in ISO 32000-1:2008 shall be used for the value of the BM key in an extended graphic state dictionary."))
					if allReported() {
						return true
					}
				}
				if bmName.String() != "Normal" && !ok2 && !reported6_2_10_2 {
					reported6_2_10_1_2 = true
					violations = append(violations, newViolatedRule("6.2.10-2", "If the document does not contain a PDF/A OutputIntent, then all Page objects that contain transparency shall include the Group key, and the attribute dictionary that forms the value of that Group key shall include a CS entry whose value shall be used as the default blending colour space."))
					if allReported() {
						return true
					}
				}
			}
		}
		if _, ok = core.GetDict(dict.Get("SMask")); !reported6_2_10_1_2 && ok && !ok2 && !reported6_2_10_2 {
			reported6_2_10_1_2 = true
			violations = append(violations, newViolatedRule("6.2.10-2", "If the document does not contain a PDF/A OutputIntent, then all Page objects that contain transparency shall include the Group key, and the attribute dictionary that forms the value of that Group key shall include a CS entry whose value shall be used as the default blending colour space."))
			if allReported() {
				return true
			}
		}
		if ca := dict.Get("CA"); !reported6_2_10_1_2 && ca != nil && !ok2 && !reported6_2_10_2 {
			f, err := core.GetNumberAsFloat(ca)
			if err == nil && f < 1.0 {
				reported6_2_10_1_2 = true
				violations = append(violations, newViolatedRule("6.2.10-2", "If the document does not contain a PDF/A OutputIntent, then all Page objects that contain transparency shall include the Group key, and the attribute dictionary that forms the value of that Group key shall include a CS entry whose value shall be used as the default blending colour space."))
				if allReported() {
					return true
				}
			}
		}
		if ca := dict.Get("ca"); !reported6_2_10_1_2 && ca != nil && !ok2 && !reported6_2_10_2 {
			f, err := core.GetNumberAsFloat(ca)
			if err == nil && f < 1.0 {
				reported6_2_10_1_2 = true
				violations = append(violations, newViolatedRule("6.2.10-2", "If the document does not contain a PDF/A OutputIntent, then all Page objects that contain transparency shall include the Group key, and the attribute dictionary that forms the value of that Group key shall include a CS entry whose value shall be used as the default blending colour space."))
				if allReported() {
					return true
				}
			}
		}
		return false
	}
	for _, page := range r.PageList {
		res := page.Resources
		if res == nil {
			continue
		}
		if res.ExtGState == nil {
			continue
		}
		dict, ok := core.GetDict(res.ExtGState)
		if !ok {
			continue
		}
		names := dict.Keys()
		for _, name := range names {
			dict2, ok := core.GetDict(dict.Get(name))
			if !ok {
				continue
			}
			if checkExtGState(dict2) {
				return violations
			}
		}
	}
	for _, page := range r.PageList {
		res := page.Resources
		if res == nil {
			continue
		}
		dict, ok := core.GetDict(res.XObject)
		if !ok {
			continue
		}
		for _, name := range dict.Keys() {
			stream, ok := core.GetStream(dict.Get(name))
			if !ok {
				continue
			}
			resourcesDict, ok := core.GetDict(stream.Get("Resources"))
			if !ok {
				continue
			}
			extGStateDict, ok := core.GetDict(resourcesDict.Get("ExtGState"))
			if !ok {
				continue
			}
			for _, name := range extGStateDict.Keys() {
				dict, ok := core.GetDict(extGStateDict.Get(name))
				if !ok {
					continue
				}
				if checkExtGState(dict) {
					return violations
				}
			}
		}
	}
	return violations
}

// ViolatedRule is the structure that defines violated PDF/A rule.
type ViolatedRule struct {
	RuleNo string
	Detail string
}

// Profile4E is the implementation of the PDF/A-4E standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type (
	Profile4E   struct{ profile4 }
	xmpBaseType uint8
)

func applyImageModifications(img *model.XObjectImage, imageModifications2 imageModifications) error {
	image, err := img.ToImage()
	if err != nil {
		return err
	}
	if imageModifications2.encoder != nil {
		img.Filter = imageModifications2.encoder
	}
	dict := core.MakeDict()
	dict.Set("Quality", core.MakeInteger(100))
	dict.Set("Predictor", core.MakeInteger(1))
	img.Decode = nil
	if err = img.SetImage(image, nil); err != nil {
		return err
	}
	img.ToPdfObject()
	return nil
}

func verifyToUnicodePDFA1(dict *core.PdfObjectDictionary, streamData map[*core.PdfObjectStream][]byte, cmaps map[*core.PdfObjectStream]*cmap.CMap) ViolatedRule {
	const (
		ruleNo = "6.3.8-1"
		detail = "The font dictionary shall include a ToUnicode entry whose value is a CMap stream object that maps character codes to Unicode values, as described in PDF Reference 5.9, unless the font meets any of the following three conditions:\n - fonts that use the predefined encodings MacRomanEncoding, MacExpertEncoding or WinAnsiEncoding, or that use the predefined Identity-H or Identity-V CMaps;\n - Type 1 fonts whose character names are taken from the Adobe standard Latin character set or the set of named characters in the Symbol font, as defined in PDF Reference Appendix D;\n - Type 0 fonts whose descendant CIDFont uses the Adobe-GB1, Adobe-CNS1, Adobe-Japan1 or Adobe-Korea1 character collections."
	)
	encoding := dict.Get("Encoding")
	if encodingName, ok := core.GetName(encoding); ok {
		if encodingName.String() == "Identity-H" || encodingName.String() == "Identity-V" || encodingName.String() == "MacRomanEncoding" || encodingName.String() == "MacExpertEncoding" || encodingName.String() == "WinAnsiEncoding" {
			return noViolation
		}
	}
	toUnicodeStream, ok := core.GetStream(dict.Get("ToUnicode"))
	if ok {
		_, err := loadCMap(toUnicodeStream, streamData, cmaps)
		if err != nil {
			return newViolatedRule(ruleNo, detail)
		}
		return noViolation
	}
	subtypeName, ok := core.GetName(dict.Get("Subtype"))
	if !ok {
		return newViolatedRule(ruleNo, detail)
	}
	switch subtypeName.String() {
	case "Type1":
		return noViolation
	}
	return newViolatedRule(ruleNo, detail)
}

// NewProfile2A creates a new Profile2A with given options.
func NewProfile2A(options *Profile2Options) *Profile2A {
	if options == nil {
		options = DefaultProfile2Options()
	}
	setProfile2OptionDefaults(options)
	return &Profile2A{profile2{options: *options, standard: standard2A()}}
}

func verifyReferenceXObjectsPDFA1(r *model.CompliancePdfReader) (rule ViolatedRule) {
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(stream.Get("Type"))
		if !ok {
			continue
		}
		if *typeName != "XObject" {
			continue
		}
		if stream.Get("Ref") != nil {
			return newViolatedRule("6.2.6-1", "A conforming file shall not contain any reference XObjects.")
		}
	}
	return rule
}

func verifyOptionalContentPDFA1(r *model.CompliancePdfReader) ViolatedRule {
	trailer, err := r.GetTrailer()
	if err != nil {
		common.Log.Debug("Cannot get document trailer: %v", err)
		return noViolation
	}
	ref, ok := trailer.Get("Root").(*core.PdfObjectReference)
	if !ok {
		common.Log.Debug("Cannot find document root")
		return noViolation
	}
	refDict, ok := core.GetDict(core.ResolveReference(ref))
	if !ok {
		common.Log.Debug("cannot resolve catalog dictionary")
		return noViolation
	}
	if refDict.Get("OCProperties") != nil {
		return newViolatedRule("6.1.13-1", "The document catalog dictionary shall not contain a key with the name OCProperties")
	}
	return noViolation
}
func standard3B() standardType { return standardType{part: 3, conformance: "B"} }

// NewProfile3A creates a new Profile3A with given options.
func NewProfile3A(options *Profile3Options) *Profile3A {
	if options == nil {
		options = DefaultProfile3Options()
	}
	setProfile3OptionDefaults(options)
	return &Profile3A{profile3{options: *options, standard: standard3A()}}
}

func verifyEmbeddedFilesPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	objectNums := r.GetObjectNums()
	for _, objectNum := range objectNums {
		obj, err := r.GetIndirectObjectByNumber(objectNum)
		if err != nil {
			continue
		}
		dict, ok := core.GetDict(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(dict.Get("Type"))
		if !ok {
			continue
		}
		if typeName.String() != "Filespec" {
			continue
		}
		if dict.Get("EF") != nil {
			violations = append(violations, newViolatedRule("6.1.11-1", "A file specification dictionary, as defined in PDF 3.10.2, shall not contain the EF key."))
			break
		}
	}
	dict, ok := catalogDict(r)
	if !ok {
		return violations
	}
	namesDict, ok := core.GetDict(dict.Get("Names"))
	if !ok {
		return violations
	}
	if namesDict.Get("EmbeddedFiles") != nil {
		violations = append(violations, newViolatedRule("6.1.11-2", "A file's name dictionary, as defined in PDF Reference 3.6.3, shall not contain the EmbeddedFiles key."))
	}
	return violations
}

func verifyXObjectsPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_2_4_1, reported6_2_4_2, reported6_2_5_1, reported6_2_6_1, reported6_2_4_3, reported6_2_7_1 bool
	seen := map[*core.PdfObjectStream]struct{}{}
	for _, n := range r.GetObjectNums() {
		if reported6_2_4_1 && reported6_2_4_2 && reported6_2_4_3 && reported6_2_5_1 && reported6_2_6_1 && reported6_2_7_1 {
			return violations
		}
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		if _, ok = seen[stream]; ok {
			continue
		}
		seen[stream] = struct{}{}
		subTypeName, ok := core.GetName(stream.Get("SubType"))
		if !ok {
			continue
		}
		if !reported6_2_6_1 {
			if stream.Get("Ref") != nil {
				violations = append(violations, newViolatedRule("6.2.6-1", "A conforming file shall not contain any reference XObjects."))
				reported6_2_6_1 = true
			}
		}
		if subTypeName.String() == "PS" {
			if !reported6_2_7_1 {
				violations = append(violations, newViolatedRule("6.2.7-1", "A conforming file shall not contain any PostScript XObjects."))
				reported6_2_7_1 = true
				continue
			}
		}
		if subTypeName.String() == "Form" {
			if reported6_2_4_2 && reported6_2_5_1 && reported6_2_6_1 {
				continue
			}
			if !reported6_2_4_2 && stream.Get("OPI") != nil {
				violations = append(violations, newViolatedRule("6.2.4-2", "An XObject dictionary (Image or Form) shall not contain the OPI key."))
				reported6_2_4_2 = true
			}
			if !reported6_2_5_1 {
				if stream.Get("PS") != nil {
					reported6_2_5_1 = true
				}
				if subtype2 := stream.Get("Subtype2"); subtype2 != nil && !reported6_2_5_1 {
					if subtype2Name, ok := core.GetName(subtype2); ok && *subtype2Name == "PS" {
						reported6_2_5_1 = true
					}
				}
				if reported6_2_5_1 {
					violations = append(violations, newViolatedRule("6.2.5-1", "A form XObject dictionary shall not contain the Subtype2 key with a value of PS or the PS key."))
				}
			}
			continue
		}
		if subTypeName.String() != "Image" {
			continue
		}
		if !reported6_2_4_1 && stream.Get("Alternates") != nil {
			violations = append(violations, newViolatedRule("6.2.4-1", "An Image dictionary shall not contain the Alternates key."))
			reported6_2_4_1 = true
		}
		if !reported6_2_4_3 && stream.Get("Interpolate") != nil {
			interpolate, ok := core.GetBool(stream.Get("Interpolate"))
			if ok && bool(*interpolate) {
				continue
			}
			violations = append(violations, newViolatedRule("6.2.4-3", "If an Image dictionary contains the Interpolate key, its value shall be false."))
			reported6_2_4_3 = true
		}
	}
	return violations
}

func verifyFormXObjectOPIPDFA4(r *model.CompliancePdfReader) (rule ViolatedRule) {
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(stream.Get("Type"))
		if !ok {
			continue
		}
		if *typeName != "XObject" {
			continue
		}
		_, ok = core.GetName(stream.Get("OPI"))
		if ok {
			return newViolatedRule("6.2.8.1-1", "A form XObject dictionary shall not contain an OPI key.")
		}
	}
	return rule
}

func infoEntryMatches(s string, info, info2 *model.PdfInfo) bool {
	switch s {
	case "CreationDate":
		if info.CreationDate == nil {
			return true
		}
		return info2.CreationDate != nil && info2.CreationDate.ToGoTime().Equal(info.CreationDate.ToGoTime())
	case "ModDate":
		if info.ModifiedDate == nil {
			return true
		}
		return info2.ModifiedDate != nil && info2.ModifiedDate.ToGoTime().Equal(info.ModifiedDate.ToGoTime())
	}
	var str, str2 *core.PdfObjectString
	switch s {
	case "Title":
		str, str2 = info.Title, info2.Title
	case "Author":
		str, str2 = info.Author, info2.Author
	case "Subject":
		str, str2 = info.Subject, info2.Subject
	case "Keywords":
		str, str2 = info.Keywords, info2.Keywords
	case "Creator":
		str, str2 = info.Creator, info2.Creator
	case "Producer":
		str, str2 = info.Producer, info2.Producer
	}
	if str == nil {
		return true
	}
	return str2 != nil && trimmedEqual(str2.String(), str.String())
}

// ApplyStandard tries to change the content of the writer to match the PDF/A-3 standard.
// Implements model.StandardApplier.
func (p *profile3) ApplyStandard(document *docutil.Document) (err error) {
	ensureMinimumVersion(document, 7)
	if err = updateInfoDates(document, p.options.Now); err != nil {
		return err
	}
	if err = setCatalogVersion(document); err != nil {
		return err
	}
	optimizePage, optimizeDocument := newColorspaceOptimizers(p.options.CMYKDefaultColorSpace, p.standard)
	err = optimizeColorspaces(document, []pageColorspaceOptimizeFunc{optimizePage}, []documentColorspaceOptimizeFunc{optimizeDocument})
	if err != nil {
		return err
	}
	ensureDocumentID(document)
	if err = fixPermissions(document); err != nil {
		return err
	}
	if err = fixXObjects(document, p.standard.part); err != nil {
		return err
	}
	if err = fixAnnotationsPDFA23(document); err != nil {
		return err
	}
	if err = fixExtGStatePDFA23(document); err != nil {
		return err
	}
	if err = embedFonts(document); err != nil {
		return err
	}
	if err = fixInteractiveForm(document); err != nil {
		return err
	}
	if p.standard.conformance == "A" {
		setMarkInfoMarked(document)
	}
	if err = fixActions(document, p.standard.part); err != nil {
		return err
	}
	if err = removeCatalogAdditionalActions(document); err != nil {
		return err
	}
	if err := updateXMPMetadata(document, p.standard, p.options.Xmp); err != nil {
		return err
	}
	if p.standard == standard3A() {
		if err = ensureStructTreeRoot(document); err != nil {
			return err
		}
	}
	if err = fixOptionalContent(document); err != nil {
		return err
	}
	if err = removeAlternatePresentations(document); err != nil {
		return err
	}
	if err = removeRequirements(document); err != nil {
		return err
	}
	return nil
}

func parseValueType(s string) xmpPropertyType {
	str := strings.TrimSpace(s)
	arrayKind := xmpArrayNone
	for _, v := range []struct {
		prefix string
		kind   xmpArrayKind
	}{{"Seq ", xmpArraySeq}, {"Bag ", xmpArrayBag}, {"Alt ", xmpArrayAlt}, {"Ordered array of ", xmpArraySeq}, {"Unordered array of ", xmpArrayBag}, {"Alternative array of ", xmpArrayAlt}} {
		if s, ok := strings.CutPrefix(str, v.prefix); ok {
			arrayKind, str = v.kind, strings.TrimSpace(s)
			break
		}
	}
	if strings.EqualFold(str, "Lang Alt") || strings.EqualFold(str, "Language Alternative") {
		return xmpPropertyType{Array: xmpArrayLangAlt, Base: xmpBaseText}
	}
	switch str {
	case "Integer":
		return xmpPropertyType{Array: arrayKind, Base: xmpBaseInteger}
	case "Real":
		return xmpPropertyType{Array: arrayKind, Base: xmpBaseReal}
	case "Boolean":
		return xmpPropertyType{Array: arrayKind, Base: xmpBaseBoolean}
	case "Date":
		return xmpPropertyType{Array: arrayKind, Base: xmpBaseDate}
	case "Rational":
		return xmpPropertyType{Array: arrayKind, Base: xmpBaseRational}
	case "Text", "ProperName", "AgentName", "GUID", "URI", "URL", "MIMEType", "Locale", "RenditionClass", "XPath", "Part", "Choice":
		return xmpPropertyType{Array: arrayKind, Base: xmpBaseText}
	case "":
		return xmpPropertyType{}
	}
	return xmpPropertyType{Array: arrayKind, Base: xmpBaseOther}
}

func removeRequirements(doc *docutil.Document) error {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return errors.New("catalog not found")
	}
	if catalog.Object.Get("Requirements") != nil {
		catalog.Object.Remove("Requirements")
	}
	return nil
}

func setCatalogVersion(doc *docutil.Document) error {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return errors.New("catalog not found")
	}
	catalog.SetVersion()
	return nil
}

func fixTransparencyGroups(doc *docutil.Document) error {
	pages, ok := doc.GetPages()
	if !ok {
		return nil
	}
	for _, page := range pages {
		streams := page.FindXObjectForms()
		for _, stream := range streams {
			groupDict, ok := core.GetDict(stream.Get("Group"))
			if ok {
				if s := groupDict.Get("S"); s != nil {
					sName, ok := core.GetName(s)
					if ok && sName.String() == "Transparency" {
						stream.Remove("Group")
					}
				}
			}
		}
		resourcesXObject, ok := page.GetResourcesXObject()
		if ok {
			groupDict, ok := core.GetDict(resourcesXObject.Get("Group"))
			if ok {
				s := groupDict.Get("S")
				if s != nil {
					sName, ok := core.GetName(s)
					if ok && sName.String() == "Transparency" {
						resourcesXObject.Remove("Group")
					}
				}
			}
		}
		groupDict, ok2 := core.GetDict(page.Object.Get("Group"))
		if ok2 {
			s := groupDict.Get("S")
			if s != nil {
				sName, ok := core.GetName(s)
				if ok && sName.String() == "Transparency" {
					page.Object.Remove("Group")
				}
			}
		}
	}
	return nil
}

// NewProfile4F creates a new Profile4F with the given options.
func NewProfile4F(options *Profile4Options) *Profile4F {
	if options == nil {
		options = DefaultProfile4Options()
	}
	setProfile4OptionDefaults(options)
	return &Profile4F{profile4{options: *options, standard: standard4F()}}
}

func optimizeColorspaces(doc *docutil.Document, optimizePages []pageColorspaceOptimizeFunc, optimizeDocuments []documentColorspaceOptimizeFunc) error {
	pages, ok := doc.GetPages()
	if !ok {
		return nil
	}
	var imgs []*docutil.Image
	for i, page := range pages {
		imgs2, err := page.FindXObjectImages()
		if err != nil {
			return err
		}
		for _, optimizePage := range optimizePages {
			if err = optimizePage(doc, &pages[i], imgs2); err != nil {
				return err
			}
		}
		imgs = append(imgs, imgs2...)
	}
	for _, optimizeDocument := range optimizeDocuments {
		if err := optimizeDocument(doc, imgs); err != nil {
			return err
		}
	}
	return nil
}

type standardType struct {
	part        int
	conformance string
}
type xmpContentKind uint8

func infoXMPEquivalenceDetail(s string) string {
	return "The value of the " + s + " entry from the document information dictionary, if present, " + "and its analogous XMP property shall be equivalent."
}

// ApplyStandard tries to change the content of the writer to match the PDF/A-1 standard.
// Implements model.StandardApplier.
func (p *profile1) ApplyStandard(document *docutil.Document) (err error) {
	ensureMinimumVersion(document, 4)
	if err = updateInfoDates(document, p.options.Now); err != nil {
		return err
	}
	if err = setCatalogVersion(document); err != nil {
		return err
	}
	optimizePage, optimizeDocument := newColorspaceOptimizers(p.options.CMYKDefaultColorSpace, p.standard)
	err = optimizeColorspaces(document, []pageColorspaceOptimizeFunc{removeImageSoftMasks, optimizePage}, []documentColorspaceOptimizeFunc{optimizeDocument})
	if err != nil {
		return err
	}
	ensureDocumentID(document)
	if err = fixXObjects(document, p.standard.part); err != nil {
		return err
	}
	if err = fixTransparencyGroups(document); err != nil {
		return err
	}
	if err = fixAnnotationsPDFA1(document); err != nil {
		return err
	}
	if err = fixExtGStatePDFA1(document); err != nil {
		return err
	}
	if err = embedFonts(document); err != nil {
		return err
	}
	if p.standard.conformance == "A" {
		setMarkInfoMarked(document)
	}
	if err = fixActions(document, p.standard.part); err != nil {
		return err
	}
	if err = removeCatalogAdditionalActions(document); err != nil {
		return err
	}
	if err := updateXMPMetadata(document, p.standard, p.options.Xmp); err != nil {
		return err
	}
	if p.standard == standard1A() {
		if err = ensureStructTreeRoot(document); err != nil {
			return err
		}
	}
	if err = fixInteractiveFormPDFA1(document); err != nil {
		return err
	}
	return nil
}

// Profile2B is the implementation of the PDF/A-2B standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type Profile2B struct{ profile2 }

func verifyExtGStatePDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_2_8_1, reported6_2_8_2, reported6_4_1, reported6_4_4, reported6_4_5, reported6_4_6 bool
	allReported := func() bool {
		return reported6_2_8_1 && reported6_2_8_2 && reported6_4_1 && reported6_4_4 && reported6_4_5 && reported6_4_6
	}
	checkExtGState := func(dict *core.PdfObjectDictionary) bool {
		if !reported6_2_8_1 && dict.Get("TR") != nil {
			reported6_2_8_1 = true
			violations = append(violations, newViolatedRule("6.2.8-1", "An ExtGState dictionary shall not contain the TR key."))
		}
		if tR2 := dict.Get("TR2"); !reported6_2_8_2 && tR2 != nil {
			tR2Name, ok := core.GetName(tR2)
			if !ok || (ok && *tR2Name != "Default") {
				reported6_2_8_2 = true
				violations = append(violations, newViolatedRule("6.2.8-2", "An ExtGState dictionary shall not contain the TR2 key with a value other than Default."))
				if allReported() {
					return true
				}
			}
		}
		if sMask := dict.Get("SMask"); !reported6_4_1 && sMask != nil {
			sMaskName, ok := core.GetName(sMask)
			if !ok || (ok && *sMaskName != "None") {
				reported6_4_1 = true
				violations = append(violations, newViolatedRule("6.4-1", "If an SMask key appears in an ExtGState dictionary, its value shall be None."))
				if allReported() {
					return true
				}
			}
		}
		if ca := dict.Get("CA"); !reported6_4_5 && ca != nil {
			f, err := core.GetNumberAsFloat(ca)
			if err == nil && f != 1.0 {
				reported6_4_5 = true
				violations = append(violations, newViolatedRule("6.4-5", "The following keys, if present in an ExtGState object, shall have the values shown: CA - 1.0."))
				if allReported() {
					return true
				}
			}
		}
		if ca := dict.Get("ca"); !reported6_4_6 && ca != nil {
			f, err := core.GetNumberAsFloat(ca)
			if err == nil && f != 1.0 {
				reported6_4_6 = true
				violations = append(violations, newViolatedRule("6.4-6", "The following keys, if present in an ExtGState object, shall have the values shown: ca - 1.0."))
				if allReported() {
					return true
				}
			}
		}
		if bm := dict.Get("BM"); !reported6_4_4 && bm != nil {
			bmName, ok := core.GetName(bm)
			if ok {
				switch bmName.String() {
				case "Normal", "Compatible":
				default:
					reported6_4_4 = true
					violations = append(violations, newViolatedRule("6.4-4", "The following keys, if present in an ExtGState object, shall have the values shown: BM - Normal or Compatible."))
					if allReported() {
						return true
					}
				}
			}
		}
		return false
	}
	for _, page := range r.PageList {
		res := page.Resources
		if res == nil {
			continue
		}
		if res.ExtGState == nil {
			continue
		}
		dict, ok := core.GetDict(res.ExtGState)
		if !ok {
			continue
		}
		names := dict.Keys()
		for _, name := range names {
			dict2, ok := core.GetDict(dict.Get(name))
			if !ok {
				continue
			}
			if checkExtGState(dict2) {
				return violations
			}
		}
	}
	for _, page := range r.PageList {
		res := page.Resources
		if res == nil {
			continue
		}
		dict, ok := core.GetDict(res.XObject)
		if !ok {
			continue
		}
		for _, name := range dict.Keys() {
			stream, ok := core.GetStream(dict.Get(name))
			if !ok {
				continue
			}
			resourcesDict, ok := core.GetDict(stream.Get("Resources"))
			if !ok {
				continue
			}
			extGStateDict, ok := core.GetDict(resourcesDict.Get("ExtGState"))
			if !ok {
				continue
			}
			for _, name := range extGStateDict.Keys() {
				dict, ok := core.GetDict(extGStateDict.Get(name))
				if !ok {
					continue
				}
				if checkExtGState(dict) {
					return violations
				}
			}
		}
	}
	return violations
}

func verifyCIDSystemInfo(dict *core.PdfObjectDictionary, streamData map[*core.PdfObjectStream][]byte, cmaps map[*core.PdfObjectStream]*cmap.CMap, clause string) ViolatedRule {
	s := clause + ".3-1"
	const (
		detail = "For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship. If the Encoding key in the Type 0 font dictionary is Identity-H or Identity-V, any values of Registry, Ordering, and Supplement may be used in the CIDSystemInfo entry of the CIDFont. Otherwise, the corresponding Registry and Ordering strings in both CIDSystemInfo dictionaries shall be identical, and the value of the Supplement key in the CIDSystemInfo dictionary of the CIDFont shall be greater than or equal to the Supplement key in the CIDSystemInfo dictionary of the CMap."
	)
	var str string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		str = subtypeName.String()
	}
	if str != "Type0" {
		return noViolation
	}
	encoding := dict.Get("Encoding")
	if encodingName, ok := core.GetName(encoding); ok {
		switch encodingName.String() {
		case "Identity-H", "Identity-V":
			return noViolation
		}
		cm, err := cmap.LoadPredefinedCMap(encodingName.String())
		if err != nil {
			return newViolatedRule(s, detail)
		}
		cidSystemInfo := cm.CIDSystemInfo()
		if cidSystemInfo.Ordering != cidSystemInfo.Registry {
			return newViolatedRule(s, detail)
		}
		return noViolation
	}
	encodingStream, ok := core.GetStream(encoding)
	if !ok {
		return newViolatedRule(s, detail)
	}
	cm, err := loadCMap(encodingStream, streamData, cmaps)
	if err != nil {
		return newViolatedRule(s, detail)
	}
	cidSystemInfo := cm.CIDSystemInfo()
	if cidSystemInfo.Ordering != cidSystemInfo.Registry {
		return newViolatedRule(s, detail)
	}
	return noViolation
}
func stubDocumentInfoPDFA1(r *model.CompliancePdfReader) ViolatedRule { return noViolation }
func verifyNoDataAfterEOF(r *model.CompliancePdfReader) ViolatedRule {
	if r.ParserMetadata().HasDataAfterEOF() {
		return newViolatedRule("6.1.3-3", "No data shall follow the last end-of-file marker except a single optional end-of-line marker.")
	}
	return noViolation
}

func verifyLogicalStructurePDFA1A(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	ok := true
	catalogMarkInfo, ok2 := r.GetCatalogMarkInfo()
	if !ok2 {
		ok = false
	} else {
		catalogMarkInfoDict, ok2 := core.GetDict(catalogMarkInfo)
		if ok2 {
			marked, ok2 := core.GetBool(catalogMarkInfoDict.Get("Marked"))
			if !ok2 || !bool(*marked) {
				ok = false
			}
		} else {
			ok = false
		}
	}
	if !ok {
		violations = append(violations, newViolatedRule("6.8.2.2-1", "The document catalog dictionary shall include a MarkInfo dictionary with a Marked entry in it, whose value shall be true"))
	}
	catalogStructTreeRoot, ok2 := r.GetCatalogStructTreeRoot()
	if !ok2 {
		violations = append(violations, newViolatedRule("6.8.3.3-1", "The logical structure of the conforming file shall be described by a structure hierarchy rooted in the StructTreeRoot entry of the document catalog dictionary, as described in PDF Reference 9.6."))
	}
	catalogStructTreeRootDict, ok2 := core.GetDict(catalogStructTreeRoot)
	if ok2 {
		roleMapName, ok := core.GetName(catalogStructTreeRootDict.Get("RoleMap"))
		if ok {
			roleMapDict, ok := core.GetDict(roleMapName)
			if ok {
				for _, name := range roleMapDict.Keys() {
					obj := roleMapDict.Get(name)
					if obj == nil {
						violations = append(violations, newViolatedRule("6.8.3.4-1", "All non-standard structure types shall be mapped to the nearest functionally equivalent standard type, as defined in PDF Reference 9.7.4, in the role map dictionary of the structure tree root."))
					}
				}
			}
		}
	}
	return violations
}

func verifyLinearizedPDFA1(r *model.CompliancePdfReader) ViolatedRule {
	return verifyLinearized(r, "6.1.3-4")
}

func verifyHeaderPDFA23(r *model.CompliancePdfReader) ViolatedRule {
	if r.ParserMetadata().HeaderPosition() != 0 {
		return newViolatedRule("6.1.2-1", "header position is not at the first byte")
	}
	if r.PdfVersion().Major != 1 {
		return newViolatedRule("6.1.2-1", "The file header shall consist of “%PDF-1.n” followed by a single EOL marker, where 'n' is a single digit number between 0 (30h) and 7 (37h)")
	}
	if r.PdfVersion().Minor < 0 || r.PdfVersion().Minor > 7 {
		return newViolatedRule("6.1.2-1", "The file header shall consist of “%PDF-1.n” followed by a single EOL marker, where 'n' is a single digit number between 0 (30h) and 7 (37h)")
	}
	return noViolation
}

type profile3 struct {
	standard standardType
	options  Profile3Options
}

const (
	xmpBaseNone xmpBaseType = iota
	xmpBaseText
	xmpBaseInteger
	xmpBaseReal
	xmpBaseBoolean
	xmpBaseDate
	xmpBaseRational
	xmpBaseOther
)
