package pdfua

import (
	"fmt"

	"github.com/OpenMellon/go-xmp/models/dc"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func checkStructTreeRootAndMarkInfo(r *model.CompliancePdfReader, rules ruleNos) []ViolatedRule {
	var violations []ViolatedRule
	if _, ok := structTreeRootOf(r); !ok {
		violations = append(violations, newViolation(rules.structTreeRoot, "the logical structure shall be described by a structure hierarchy rooted in the StructTreeRoot entry of the document catalog"))
	}
	marked := false
	suspects := false
	if markInfoObj, ok := r.GetCatalogMarkInfo(); ok {
		if markInfo, ok := core.GetDict(markInfoObj); ok {
			if markedVal, ok := core.GetBool(markInfo.Get("Marked")); ok {
				marked = bool(*markedVal)
			}
			if suspectsVal, ok := core.GetBool(markInfo.Get("Suspects")); ok {
				suspects = bool(*suspectsVal)
			}
		}
	}
	if !marked {
		violations = append(violations, newViolation(rules.markInfoMarked, "the MarkInfo dictionary shall contain the Marked entry with value true"))
	}
	if suspects && rules.markInfoSuspects != "" {
		violations = append(violations, newViolation(rules.markInfoSuspects, "the MarkInfo Suspects entry shall not have the value true"))
	}
	return violations
}

func checkTables(r *model.CompliancePdfReader) []ViolatedRule {
	root, ok := structTreeRootOf(r)
	if !ok {
		return nil
	}
	roleMap, _ := core.GetDict(core.TraceToDirectObject(root.Get("RoleMap")))
	classMap := classMapOf(root)
	resolve := func(elem *core.PdfObjectDictionary) string { return resolveStandardType(elem, roleMap) }
	var violations []ViolatedRule
	walkStructTree(root, newRoleMapResolver(root), func(elem *structElem) {
		if elem.stdType != "Table" {
			return
		}
		violations = append(violations, checkTable(elem.dict, classMap, resolve)...)
	})
	return violations
}

func checkTabsEntries(r *model.CompliancePdfReader) []ViolatedRule {
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
		if !pageHasReportableAnnots(page) {
			continue
		}
		tabs, ok := core.GetName(page.Tabs)
		if !ok {
			violations = append(violations, newViolation("8.9.3.3-1", fmt.Sprintf("page %d contains annotations but has no /Tabs entry (shall be A, W, or S)", pageNum)))
			continue
		}
		switch string(*tabs) {
		case "A", "W", "S":
		default:
			violations = append(violations, newViolation("8.9.3.3-1", fmt.Sprintf("page %d contains annotations but has Tabs = %s (shall be A, W, or S)", pageNum, string(*tabs))))
		}
	}
	return violations
}

func checkXMPIdentifier(xmp catalogXMP, part, rev int, rules ruleNos) []ViolatedRule {
	if !xmp.present {
		return nil
	}
	missing := []ViolatedRule{newViolation(rules.xmpPartMissing, "the document XMP metadata does not contain a pdfuaid:part entry matching the conformance level")}
	if xmp.doc == nil {
		return missing
	}
	id, ok := xmp.doc.GetPdfUAID()
	if !ok || id.Part == 0 {
		return missing
	}
	var violations []ViolatedRule
	if id.Part != part {
		violations = append(violations, newViolation(rules.xmpPartMismatch, fmt.Sprintf("the document XMP metadata pdfuaid:part is %d but shall be %d for this conformance level", id.Part, part)))
	}
	if rules.xmpRevMismatch != "" && id.Rev != rev {
		violations = append(violations, newViolation(rules.xmpRevMismatch, fmt.Sprintf("the document XMP metadata pdfuaid:rev is %d but shall be %d", id.Rev, rev)))
	}
	return violations
}

func checkTable(tableElem, classMap *core.PdfObjectDictionary, resolve structTypeFunc) []ViolatedRule {
	grid, _ := buildTableGrid(tableElem, classMap, resolve, false)
	if grid == nil {
		return nil
	}
	ids := map[string]struct{}{}
	allScoped := true
	for _, pl := range grid.placements {
		if typeOrEmpty(pl.cell, resolve) != "TH" {
			continue
		}
		if id, ok := core.GetString(pl.cell.Get("ID")); ok && id.Str() != "" {
			ids[id.Str()] = struct{}{}
		}
		if cellScope(pl.cell, classMap) == "" {
			allScoped = false
		}
	}
	if allScoped {
		return nil
	}
	budget := maxHeaderChecks
	for _, pl := range grid.placements {
		if typeOrEmpty(pl.cell, resolve) != "TD" || (pl.row == 0 && pl.col == 0) {
			continue
		}
		headers := headerIDs(pl.cell, classMap)
		undefined := undefinedIDs(headers, ids)
		if len(headers) > 0 && len(undefined) == 0 {
			continue
		}
		found, ok := findHeaderCells(grid, classMap, resolve, pl, &budget)
		if !ok {
			return nil
		}
		if found {
			continue
		}
		if len(undefined) > 0 {
			return []ViolatedRule{newViolation("7.5-2", fmt.Sprintf("the table cell at row %d, column %d references undefined header ID(s) %s, and its header cells cannot be determined otherwise", pl.row+1, pl.col+1, summarizeIDs(undefined)))}
		}
		return []ViolatedRule{newViolation("7.5-1", fmt.Sprintf("the header cells for the table cell at row %d, column %d cannot be determined: it declares no Headers attribute, and no scoped TH covers it", pl.row+1, pl.col+1))}
	}
	return nil
}

func checkRoleMapUA2(r *model.CompliancePdfReader) []ViolatedRule {
	root, ok := structTreeRootOf(r)
	if !ok {
		return nil
	}
	roleMap, _ := core.GetDict(core.TraceToDirectObject(root.Get("RoleMap")))
	classMap := classMapOf(root)
	resolve := func(elem *core.PdfObjectDictionary) string {
		typ, _ := resolveNamespacedType(elem, roleMap)
		return typ
	}
	var violations []ViolatedRule
	cyclic := map[string]bool{}
	for _, name := range roleMapCycles(roleMap) {
		cyclic[name] = true
	}
	violations = append(violations, checkRootDocument(root, roleMap)...)
	reportedUnknown := map[string]struct{}{}
	reportedRemapped := map[string]struct{}{}
	reportedCircular := map[string]struct{}{}
	sawH := false
	walkStructTree(root, resolve, func(elem *structElem) {
		if elem.dict.Get("P") == nil {
			violations = append(violations, newViolation("8.2.1-2", "structure element "+elem.typeName+" has no P (parent) entry"))
		}
		findings := classifyStructType(elem, roleMap, cyclic)
		if findings.circular {
			if _, ok := reportedCircular[elem.typeName]; !ok {
				reportedCircular[elem.typeName] = struct{}{}
				violations = append(violations, newViolation("8.2.4-2", "the RoleMap shall not contain circular mappings ("+elem.typeName+")"))
			}
		}
		if findings.remappedStandard {
			if _, ok := reportedRemapped[elem.typeName]; !ok {
				reportedRemapped[elem.typeName] = struct{}{}
				violations = append(violations, newViolation("8.2.4-4", "the standard structure type "+elem.typeName+" is remapped to a non-standard type"))
			}
		}
		if findings.unknownNamespace {
			if _, ok := reportedUnknown[elem.typeName]; !ok {
				reportedUnknown[elem.typeName] = struct{}{}
				violations = append(violations, newViolation("8.2.4-1", "structure type "+elem.typeName+" does not belong to and is not role mapped to the PDF 1.7, PDF 2.0 or MathML namespace"))
			}
		}
		if elem.stdType == "H" {
			sawH = true
		}
		if containsLinkOBJR(elem.dict) && elem.stdType != "Link" && elem.stdType != "Reference" {
			violations = append(violations, newViolation("8.2.5.20-1", "a link annotation shall be enclosed in a Link or Reference structure element (found "+elem.typeName+")"))
		}
		if elem.stdType == "L" && listHasLabels(elem.dict, resolve) {
			numbering := nameAttr(elem.dict, classMap, "ListNumbering", "List")
			if numbering == "" || numbering == "None" {
				violations = append(violations, newViolation("8.2.5.25-1", "L element with Lbl children shall declare a ListNumbering attribute with a value other than None"))
			}
		}
		if elem.stdType == "Table" {
			violations = append(violations, checkTableRegularity(elem.dict, classMap, resolve)...)
		}
	})
	if sawH {
		violations = append(violations, newViolation("8.2.5.12-1", "the H structure type shall not be used; use numbered Hn headings"))
	}
	return violations
}

func checkTitleAndViewerPrefs(r *model.CompliancePdfReader, xmp catalogXMP, rules ruleNos) []ViolatedRule {
	var violations []ViolatedRule
	if xmp.present {
		hasTitle := false
		if xmp.doc != nil {
			if dcModel := dc.FindModel(xmp.doc.GetGoXmpDocument()); dcModel != nil && len(dcModel.Title) > 0 {
				hasTitle = true
			}
		}
		if !hasTitle && !rules.xmpTitleOnly {
			if info, err := r.GetPdfInfo(); err == nil && info != nil && info.Title != nil && info.Title.Decoded() != "" {
				hasTitle = true
			}
		}
		if !hasTitle {
			violations = append(violations, newViolation(rules.docTitle, "the document title shall be defined (dc:title in XMP metadata)"))
		}
	}
	displayDocTitle := false
	if prefsObj, ok := r.GetCatalogViewerPreferences(); ok {
		if prefs, ok := core.GetDict(prefsObj); ok {
			if val, ok := core.GetBool(prefs.Get("DisplayDocTitle")); ok && bool(*val) {
				displayDocTitle = true
			}
		}
	}
	if !displayDocTitle {
		violations = append(violations, newViolation(rules.displayDocTitle, "the ViewerPreferences dictionary shall contain DisplayDocTitle with value true"))
	}
	return violations
}

func checkRootDocument(root, roleMap *core.PdfObjectDictionary) []ViolatedRule {
	kids := structChildren(root.Get("K"), func(elem *core.PdfObjectDictionary) string {
		typ, _ := resolveNamespacedType(elem, roleMap)
		return typ
	})
	var docCount int
	for _, kid := range kids {
		switch typ, _ := resolveNamespacedType(kid, roleMap); typ {
		case "":
		case "Document":
			docCount++
		default:
			docCount = -1
		}
		if docCount < 0 {
			break
		}
	}
	if docCount != 1 {
		const msg = "the structure tree root shall contain a single Document structure element as its only child"
		return []ViolatedRule{newViolation("8.2.5.2-1", msg), newViolation(structRootRuleTable5, msg)}
	}
	if _, ns := resolveNamespacedType(kids[0], roleMap); ns != model.StructureNamespacePDF2 {
		return []ViolatedRule{newViolation("8.2.5.2-2", "the Document structure element shall be in the PDF 2.0 namespace")}
	}
	return nil
}

func checkTableRegularity(tableElem, classMap *core.PdfObjectDictionary, resolve structTypeFunc) []ViolatedRule {
	_, violations := buildTableGrid(tableElem, classMap, resolve, true)
	return violations
}

func checkRoleMapUA1(r *model.CompliancePdfReader) []ViolatedRule {
	root, ok := structTreeRootOf(r)
	if !ok {
		return nil
	}
	var violations []ViolatedRule
	cyclic := map[string]struct{}{}
	if roleMap, ok := core.GetDict(core.TraceToDirectObject(root.Get("RoleMap"))); ok {
		for _, name := range roleMapCycles(roleMap) {
			cyclic[name] = struct{}{}
			violations = append(violations, newViolation("7.1-6", "the RoleMap shall not contain circular mappings ("+name+")"))
		}
		for _, key := range roleMap.Keys() {
			if _, ok := cyclic[string(key)]; ok {
				continue
			}
			if _, ok := pdf17StructTypes[string(key)]; ok {
				violations = append(violations, newViolation("7.1-7", "standard structure type "+string(key)+" shall not be remapped in the RoleMap"))
			}
		}
	}
	reported := map[string]struct{}{}
	walkStructTree(root, newRoleMapResolver(root), func(elem *structElem) {
		if _, ok := pdf17StructTypes[elem.typeName]; ok {
			return
		}
		if _, ok := cyclic[elem.typeName]; ok {
			return
		}
		if _, ok := pdf17StructTypes[elem.stdType]; ok {
			return
		}
		if _, ok := reported[elem.typeName]; ok {
			return
		}
		reported[elem.typeName] = struct{}{}
		violations = append(violations, newViolation("7.1-5", "structure type "+elem.typeName+" is neither a standard structure type nor role mapped to one"))
	})
	return violations
}

func checkXFA(r *model.CompliancePdfReader) []ViolatedRule {
	if r.AcroForm == nil || r.AcroForm.XFA == nil {
		return nil
	}
	return []ViolatedRule{newViolation("8.10.1-3", "XFA forms shall not be present")}
}
