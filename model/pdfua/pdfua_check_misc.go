package pdfua

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func checkLang(r *model.CompliancePdfReader, rules ruleNos) ViolatedRule {
	lang, ok := r.GetCatalogLanguage()
	if !ok {
		if rules.langMissing == "" {
			return noViolation
		}
		return newViolation(rules.langMissing, "the document catalog shall contain a Lang entry declaring the default natural language")
	}
	str, ok := core.GetString(lang)
	if !ok || str.Decoded() == "" {
		return newViolation(rules.langEmpty, "the catalog Lang entry shall be a non-empty language identifier")
	}
	return noViolation
}

func checkFormFields(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	acroForm := r.AcroForm
	if acroForm == nil {
		return nil
	}
	if acroForm.XFA != nil {
		violations = append(violations, newViolation("7.15-1", "XFA forms shall not be present"))
	}
	var annotInfo map[int64]annotStructInfo
	if root, ok := structTreeRootOf(r); ok {
		annotInfo = collectAnnotStructInfo(root)
	}
	widgetPages := annotPageMap(r)
	for _, field := range acroForm.AllFields() {
		if len(field.Annotations) == 0 {
			continue
		}
		if fieldHasTU(field) {
			continue
		}
		needsTU := false
		for _, widget := range field.Annotations {
			if widgetHiddenOrNoView(widget) {
				continue
			}
			if objNum, ok := objectNumber(widget.GetContainingPdfObject()); ok {
				if info, ok := annotInfo[objNum]; ok && info.hasAlt {
					continue
				}
				if page, ok := widgetPages[objNum]; ok {
					if dict, ok := core.GetDict(widget.GetContainingPdfObject()); ok && rectOffPage(dict.Get("Rect"), page) {
						continue
					}
				}
			}
			needsTU = true
			break
		}
		if !needsTU {
			continue
		}
		fieldName := ""
		if field.T != nil {
			fieldName = field.T.Decoded()
		}
		violations = append(violations, newViolation("7.18.1-3", fmt.Sprintf("form field %q has no TU alternate description", fieldName)))
	}
	return violations
}

func checkAnnotationAltText(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	numPages, err := r.GetNumPages()
	if err != nil {
		return nil
	}
	var annotInfo map[int64]annotStructInfo
	if root, ok := structTreeRootOf(r); ok {
		annotInfo = collectAnnotStructInfo(root)
	}
	for pageNum := 1; pageNum <= numPages; pageNum++ {
		page, err := r.GetPage(pageNum)
		if err != nil {
			continue
		}
		annots, err := page.GetAnnotations()
		if err != nil || len(annots) == 0 {
			continue
		}
		hasVisible := false
		for _, annot := range annots {
			dict, ok := core.GetDict(annot.GetContainingPdfObject())
			if !ok || !isReportableAnnot(dict) || rectOffPage(dict.Get("Rect"), page) {
				continue
			}
			hasVisible = true
			subtype := ""
			if name, ok := core.GetName(dict.Get("Subtype")); ok {
				subtype = string(*name)
			}
			if subtype == "Widget" {
				continue
			}
			hasContents := false
			if contents, ok := core.GetString(dict.Get("Contents")); ok && contents.Decoded() != "" {
				hasContents = true
			}
			if subtype == "Link" && !hasContents {
				violations = append(violations, newViolation("7.18.5-2", fmt.Sprintf("page %d: Link annotation has no Contents alternate description", pageNum)))
			}
			if hasContents {
				continue
			}
			if alt, ok := core.GetString(core.TraceToDirectObject(dict.Get("Alt"))); ok && alt.Decoded() != "" {
				continue
			}
			if objNum, ok := objectNumber(annot.GetContainingPdfObject()); ok {
				if info, ok := annotInfo[objNum]; ok && info.hasAlt {
					continue
				}
			}
			violations = append(violations, newViolation("7.18.1-2", fmt.Sprintf("page %d: %s annotation has no Contents or Alt alternate description", pageNum, subtype)))
		}
		if hasVisible {
			tabs, ok := core.GetName(page.Tabs)
			if !ok || string(*tabs) != "S" {
				violations = append(violations, newViolation("7.18.3-1", fmt.Sprintf("page %d contains annotations but does not declare Tabs = S", pageNum)))
			}
		}
	}
	return violations
}

func checkFont(fontDict *core.PdfObjectDictionary, rules ruleNos) []ViolatedRule {
	font, err := model.NewPdfFontFromPdfObject(fontDict)
	if err != nil {
		return nil
	}
	baseFont := ""
	if name, ok := core.GetName(fontDict.Get("BaseFont")); ok {
		baseFont = string(*name)
	}
	subtype := ""
	if name, ok := core.GetName(fontDict.Get("Subtype")); ok {
		subtype = string(*name)
	}
	var violations []ViolatedRule
	desc := font.FontDescriptor()
	embedded := subtype == "Type3" || (desc != nil && (desc.FontFile != nil || desc.FontFile2 != nil || desc.FontFile3 != nil))
	if !embedded {
		violations = append(violations, newViolation(rules.fontEmbedded, fmt.Sprintf("font %s is not embedded", baseFont)))
	}
	if subtype == "Type0" {
		if fontDict.Get("ToUnicode") == nil && !hasAdobeCIDOrdering(fontDict) {
			violations = append(violations, newViolation(rules.fontToUnicode, fmt.Sprintf("composite font %s has no ToUnicode CMap", baseFont)))
		}
		if descendants, ok := core.GetArray(fontDict.Get("DescendantFonts")); ok && descendants.Len() > 0 {
			if cidFont, ok := core.GetDict(descendants.Get(0)); ok {
				cidSubtype, _ := core.GetName(cidFont.Get("Subtype"))
				if cidSubtype != nil && string(*cidSubtype) == "CIDFontType2" && cidFont.Get("CIDToGIDMap") == nil {
					violations = append(violations, newViolation(rules.fontCIDToGIDMap, fmt.Sprintf("Type 2 CIDFont %s has no CIDToGIDMap", baseFont)))
				}
			}
		}
	}
	return violations
}

func checkLinkAnnots(r *model.CompliancePdfReader) []ViolatedRule {
	root, ok := structTreeRootOf(r)
	if !ok {
		return nil
	}
	annotInfo := collectAnnotStructInfo(root)
	numPages, err := r.GetNumPages()
	if err != nil {
		return nil
	}
	var violations []ViolatedRule
	for pageNum := 1; pageNum <= numPages; pageNum++ {
		page, err := r.GetPage(pageNum)
		if err != nil {
			continue
		}
		annots, err := page.GetAnnotations()
		if err != nil {
			continue
		}
		for _, annot := range annots {
			dict, ok := core.GetDict(annot.GetContainingPdfObject())
			if !ok || !isReportableAnnot(dict) || rectOffPage(dict.Get("Rect"), page) {
				continue
			}
			if subtype, ok := core.GetName(dict.Get("Subtype")); !ok || string(*subtype) != "Link" {
				continue
			}
			objNum, ok := objectNumber(annot.GetContainingPdfObject())
			if !ok {
				continue
			}
			if _, ok := core.GetIntVal(core.TraceToDirectObject(dict.Get("StructParent"))); !ok {
				violations = append(violations, newViolation("7.18.5-1", fmt.Sprintf("page %d: link annotation has no usable StructParent entry identifying its enclosing structure element", pageNum)))
				continue
			}
			info, found := annotInfo[objNum]
			if !found || (info.elemType != "Link" && info.elemType != "Reference") {
				violations = append(violations, newViolation("7.18.5-1", fmt.Sprintf("page %d: link annotation is not enclosed in a Link or Reference structure element", pageNum)))
			}
		}
	}
	return violations
}

func checkListItems(r *model.CompliancePdfReader) []ViolatedRule {
	root, ok := structTreeRootOf(r)
	if !ok {
		return nil
	}
	roleMap, _ := core.GetDict(core.TraceToDirectObject(root.Get("RoleMap")))
	resolve := func(elem *core.PdfObjectDictionary) string { return resolveStandardType(elem, roleMap) }
	var violations []ViolatedRule
	walkStructTree(root, newRoleMapResolver(root), func(elem *structElem) {
		if elem.stdType != "LI" {
			return
		}
		parent := elem.parent
		parentType := ""
		for parent != nil {
			parentType = resolve(parent.dict)
			if _, ok := groupingTypes[parentType]; !ok {
				break
			}
			parent = parent.parent
		}
		if parent == nil || parentType != "L" {
			violations = append(violations, newViolation("7.2-17", "LI structure element is not a child of an L element"))
		}
		var bad []string
		for _, child := range structChildren(elem.dict.Get("K"), resolve) {
			switch childType := resolve(child); childType {
			case "", "Lbl", "LBody":
			default:
				bad = append(bad, childType)
			}
		}
		if len(bad) > 0 {
			violations = append(violations, newViolation("7.2-20", "LI element may contain only Lbl and LBody elements (found "+strings.Join(bad, ", ")+")"))
		}
	})
	return violations
}

func checkFigureAlt(r *model.CompliancePdfReader, rules ruleNos, newResolver func(*core.PdfObjectDictionary) structTypeFunc) []ViolatedRule {
	root, ok := structTreeRootOf(r)
	if !ok {
		return nil
	}
	missing := 0
	walkStructTree(root, newResolver(root), func(elem *structElem) {
		if elem.stdType != "Figure" {
			return
		}
		if alt, ok := core.GetString(core.TraceToDirectObject(elem.dict.Get("Alt"))); ok && alt.Decoded() != "" {
			return
		}
		if _, ok := core.GetString(core.TraceToDirectObject(elem.dict.Get("ActualText"))); ok {
			return
		}
		missing++
	})
	if missing > 0 {
		return []ViolatedRule{newViolation(rules.figureAlt, strconv.Itoa(missing)+" Figure structure element(s) have neither Alt nor ActualText")}
	}
	return nil
}

func checkEncryption(r *model.CompliancePdfReader) ViolatedRule {
	trailer, err := r.GetTrailer()
	if err != nil {
		return noViolation
	}
	encrypt, ok := core.GetDict(trailer.Get("Encrypt"))
	if !ok {
		return noViolation
	}
	perms, ok := core.GetIntVal(encrypt.Get("P"))
	if !ok {
		return noViolation
	}
	const extractBit = 1 << 9
	if uint32(perms)&extractBit == 0 {
		return newViolation("7.16-1", "the encryption permissions do not grant content extraction for accessibility (bit 10)")
	}
	return noViolation
}

func checkPDF20Version(r *model.CompliancePdfReader) ViolatedRule {
	ver := r.PdfVersion()
	major, minor := ver.Major, ver.Minor
	if catVer, ok := catalogVersion(r); ok && (catVer.Major > major || (catVer.Major == major && catVer.Minor > minor)) {
		major, minor = catVer.Major, catVer.Minor
	}
	if major != 2 || minor != 0 {
		return newViolation("8.1-1", "PDF/UA-2 requires a PDF 2.0 document")
	}
	return noViolation
}

func checkMetadataPresent(xmp catalogXMP, rules ruleNos) ViolatedRule {
	if xmp.present {
		return noViolation
	}
	return newViolation(rules.metadataStream, "the document catalog shall contain a Metadata entry holding a metadata stream with Type /Metadata and Subtype /XML")
}

func checkHeadings(r *model.CompliancePdfReader) []ViolatedRule {
	root, ok := structTreeRootOf(r)
	if !ok {
		return nil
	}
	var violations []ViolatedRule
	lastLevel := 0
	reportedMix := false
	sawH := false
	sawHn := false
	checkMix := func() {
		if sawH && sawHn && !reportedMix {
			violations = append(violations, newViolation("7.4.4-2", "documents shall not mix unnumbered (H) and numbered (Hn) heading structure types"))
			reportedMix = true
		}
	}
	walkStructTree(root, newRoleMapResolver(root), func(elem *structElem) {
		match := headingRe.FindStringSubmatch(elem.stdType)
		if match == nil {
			if elem.stdType == "H" {
				sawH = true
				checkMix()
			}
			return
		}
		sawHn = true
		checkMix()
		level, _ := strconv.Atoi(match[1])
		if level > lastLevel+1 {
			if lastLevel == 0 {
				violations = append(violations, newViolation("7.4.2-1", fmt.Sprintf("first heading is %s; the first heading shall be H1", elem.stdType)))
			} else {
				violations = append(violations, newViolation("7.4.2-1", "heading level skipped: "+elem.stdType+" follows H"+strconv.Itoa(lastLevel)))
			}
		}
		lastLevel = level
	})
	return violations
}
