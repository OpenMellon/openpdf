package pdfa

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strings"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/colorprofile"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
)

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

func resolveItemText(s string, contentKind xmpContentKind, ok bool) (string, bool) {
	switch contentKind {
	case xmpContentValue:
		return s, false
	case xmpContentStruct:
		return "", true
	}
	return s, ok
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

func stubClause628PDFA1(r *model.CompliancePdfReader) ViolatedRule { return noViolation }

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

func standard4F() standardType { return standardType{part: 4, conformance: "F"} }

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

func stubClause618PDFA23(r *model.CompliancePdfReader) []ViolatedRule { return nil }

func standard2A() standardType { return standardType{part: 2, conformance: "A"} }

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

func standard1B() standardType { return standardType{part: 1, conformance: "B"} }

func standard3U() standardType { return standardType{part: 3, conformance: "U"} }

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

func standard3A() standardType { return standardType{part: 3, conformance: "A"} }

func standard1A() standardType { return standardType{part: 1, conformance: "A"} }

func standard4() standardType { return standardType{part: 4, conformance: ""} }

func standard4E() standardType { return standardType{part: 4, conformance: "E"} }

var predefinedNamespaces = map[string]struct{}{"http://purl.org/dc/elements/1.1/": {}, "http://ns.adobe.com/xap/1.0/": {}, "http://ns.adobe.com/xap/1.0/rights/": {}, "http://ns.adobe.com/xap/1.0/mm/": {}, "http://ns.adobe.com/xap/1.0/bj/": {}, "http://ns.adobe.com/xap/1.0/t/pg/": {}, "http://ns.adobe.com/xmp/1.0/DynamicMedia/": {}, "http://ns.adobe.com/pdf/1.3/": {}, "http://ns.adobe.com/photoshop/1.0/": {}, "http://ns.adobe.com/camera-raw-settings/1.0/": {}, "http://ns.adobe.com/exif/1.0/": {}, "http://ns.adobe.com/exif/1.0/aux/": {}, "http://ns.adobe.com/tiff/1.0/": {}, "http://ns.adobe.com/xap/1.0/g/": {}, "http://ns.adobe.com/xap/1.0/g/img/": {}, "http://ns.adobe.com/xap/1.0/sType/Dimensions#": {}, "http://ns.adobe.com/xap/1.0/sType/Font#": {}, "http://ns.adobe.com/xap/1.0/sType/Job#": {}, "http://ns.adobe.com/xap/1.0/sType/ResourceEvent#": {}, "http://ns.adobe.com/xap/1.0/sType/ResourceRef#": {}, "http://ns.adobe.com/xap/1.0/sType/Version#": {}, "http://ns.adobe.com/xmp/Identifier/qual/1.0/": {}, "http://www.aiim.org/pdfa/ns/id/": {}, "http://www.aiim.org/pdfa/ns/extension/": {}, "http://www.aiim.org/pdfa/ns/schema#": {}, "http://www.aiim.org/pdfa/ns/property#": {}, "http://www.aiim.org/pdfa/ns/type#": {}, "http://www.aiim.org/pdfa/ns/field#": {}}

func stubClause6xxPDFA23(r *model.CompliancePdfReader) (violations []ViolatedRule) { return violations }

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

func (s standardType) String() string {
	return fmt.Sprintf("PDF/A-%d%s", s.part, s.conformance)
}

func stubClause6xxFormsPDFA23(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	return violations
}

func stubClause6xxPDFA23b(r *model.CompliancePdfReader) ViolatedRule { return noViolation }

func standard2U() standardType { return standardType{part: 2, conformance: "U"} }

func isPredefinedNamespace(s string) bool { _, ok := predefinedNamespaces[s]; return ok }

var predefinedPropertyOverrides = map[string]map[string]xmpPropertyType{"http://ns.adobe.com/photoshop/1.0/": {"SupplementalCategories": {xmpArrayNone, xmpBaseText}}}

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

const (
	rdfNamespace = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	xmlNamespace = "http://www.w3.org/XML/1998/namespace"
)

func stubIndirectObjectsPDFA1(r *model.CompliancePdfReader) []ViolatedRule { return nil }

func standard3B() standardType { return standardType{part: 3, conformance: "B"} }

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

type standardType struct {
	part        int
	conformance string
}

func stubDocumentInfoPDFA1(r *model.CompliancePdfReader) ViolatedRule { return noViolation }
