package pdfa

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/timeutils"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
	"github.com/OpenMellon/openpdf/model/xmputil"
	"github.com/OpenMellon/openpdf/model/xmputil/pdfaextension"
)

type xmpPropertyType struct {
	Array xmpArrayKind
	Base  xmpBaseType
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

// Validate checks if provided input document reader matches given PDF/A profile.
func Validate(d *model.CompliancePdfReader, profile Profile) error {
	return profile.ValidateStandard(d)
}

const (
	xmpArrayNone xmpArrayKind = iota
	xmpArraySeq
	xmpArrayBag
	xmpArrayAlt
	xmpArrayLangAlt
)

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

func newViolatedRule(clause string, detail string) ViolatedRule {
	return ViolatedRule{RuleNo: clause, Detail: detail}
}

// String gets a string representation of the violated rule.
func (r ViolatedRule) String() string { return fmt.Sprintf("%s: %s", r.RuleNo, r.Detail) }

type xmpArrayKind uint8

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

const (
	xmpContentText xmpContentKind = iota
	xmpContentValue
	xmpContentStruct
)

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

// ViolatedRule is the structure that defines violated PDF/A rule.
type ViolatedRule struct {
	RuleNo string
	Detail string
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

type xmpContentKind uint8

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
