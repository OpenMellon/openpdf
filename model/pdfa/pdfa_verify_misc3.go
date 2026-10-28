package pdfa

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/colorprofile"
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
