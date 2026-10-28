package pdfa

import (
	"encoding/xml"
	"errors"
	"fmt"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/colorprofile"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
	"github.com/OpenMellon/openpdf/model/xmputil/pdfaextension"
	"github.com/adrg/sysfont"
)

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

var _ Profile = (*Profile3U)(nil)

var noViolation = ViolatedRule{}

var _ Profile = (*Profile1A)(nil)

func ensureMinimumPDF2Version(doc *docutil.Document, minor int) {
	if doc.Version.Major < 2 {
		doc.Version.Major = 2
		doc.Version.Minor = 0
	}
	if doc.Version.Minor < minor {
		doc.Version.Minor = minor
	}
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

var _ Profile = (*Profile1B)(nil)

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

var _ Profile = (*Profile3B)(nil)

type imageModifications struct {
	colorspace *colorspaceModification
	encoder    core.StreamEncoder
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

var infoKeyRules = []struct {
	Key    string
	RuleNo string
}{{Key: "CreationDate", RuleNo: "6.7.3-1"}, {Key: "Title", RuleNo: "6.7.3-2"}, {Key: "Author", RuleNo: "6.7.3-3"}, {Key: "Subject", RuleNo: "6.7.3-4"}, {Key: "Keywords", RuleNo: "6.7.3-5"}, {Key: "Creator", RuleNo: "6.7.3-6"}, {Key: "Producer", RuleNo: "6.7.3-7"}, {Key: "ModDate", RuleNo: "6.7.3-8"}}

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

type pageColorspaceOptimizeFunc func(doc *docutil.Document, page *docutil.Page, imgs []*docutil.Image) error

func ensureDocumentID(doc *docutil.Document) {
	if doc.ID[0] != "" && doc.ID[1] != "" {
		return
	}
	doc.UseHashBasedID = true
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

var _ Profile = (*Profile4E)(nil)

func ensureMinimumVersion(doc *docutil.Document, minor int) {
	if doc.Version.Major == 0 {
		doc.Version.Major = 1
	}
	if doc.Version.Minor < minor {
		doc.Version.Minor = minor
	}
}

var _ Profile = (*Profile3A)(nil)

var _ Profile = (*Profile4F)(nil)

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

func infoXMPEquivalenceDetail(s string) string {
	return "The value of the " + s + " entry from the document information dictionary, if present, " + "and its analogous XMP property shall be equivalent."
}
