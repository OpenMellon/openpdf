// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package pdfua provides PDF/UA (ISO 14289) standard profiles for validating
// and applying accessibility requirements to PDF documents.
//
// Note that unlike PDF/A, applying a PDF/UA profile can only fix mechanical
// issues (identification metadata, viewer preferences, catalog entries).
// Semantic requirements - a complete and correct structure tree, alternate
// descriptions, reading order - must be authored, e.g. with
// creator.TagComponents. ApplyStandard returns an error when the document
// lacks the semantic foundation it cannot synthesize.
package pdfua

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/OpenMellon/go-xmp/models/dc"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
	"github.com/OpenMellon/openpdf/model/xmputil"
)

// Error implements error interface.
func (e VerificationError) Error() string {
	sb := strings.Builder{}
	sb.WriteString("Standard: ")
	sb.WriteString(fmt.Sprintf("PDF/UA-%d", e.Part))
	sb.WriteString(" Violated rules:\n")
	for i, rule := range e.ViolatedRules {
		sb.WriteString(rule.String())
		if i != len(e.ViolatedRules)-1 {
			sb.WriteRune('\n')
		}
	}
	return sb.String()
}

func newRoleMapResolver(root *core.PdfObjectDictionary) structTypeFunc {
	roleMap, _ := core.GetDict(core.TraceToDirectObject(root.Get("RoleMap")))
	return func(elem *core.PdfObjectDictionary) string { return resolveStandardType(elem, roleMap) }
}

func newNamespaceResolver(root *core.PdfObjectDictionary) structTypeFunc {
	roleMap, _ := core.GetDict(core.TraceToDirectObject(root.Get("RoleMap")))
	return func(elem *core.PdfObjectDictionary) string {
		typ, _ := resolveNamespacedType(elem, roleMap)
		return typ
	}
}

// StandardName gets the name of the standard.
func (p *Profile1) StandardName() string { return p.std.String() }

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

func elemNamespace(elem *core.PdfObjectDictionary) string {
	ns, _ := core.GetDict(core.TraceToDirectObject(elem.Get("NS")))
	return nsURI(ns)
}

func intAttr(elem, classMap *core.PdfObjectDictionary, key core.PdfObjectName, owner string, def int) int {
	obj := lookupAttr(elem, classMap, key, owner, isInt)
	if obj == nil {
		return def
	}
	val, _ := core.GetIntVal(obj)
	return val
}

func classifyStructType(elem *structElem, roleMap *core.PdfObjectDictionary, known map[string]bool) structureTypeFindings {
	var findings structureTypeFindings
	findings.circular = known[elem.typeName] || hasCircularNSMapping(elem.dict)
	if mapped, _ := resolveNamespacedType(elem.dict, roleMap); mapped != "" {
		return findings
	}
	if isStandardType(elem.typeName, elemNamespace(elem.dict)) {
		findings.remappedStandard = true
	} else {
		findings.unknownNamespace = !findings.circular
	}
	return findings
}

func dictGet(obj core.PdfObject, key core.PdfObjectName) core.PdfObject {
	if obj == nil {
		return nil
	}
	dict, ok := core.GetDict(core.TraceToDirectObject(obj))
	if !ok {
		return nil
	}
	return dict.Get(key)
}

var pdf20StructTypes = map[string]struct{}{"Document": {}, "DocumentFragment": {}, "Part": {}, "Sect": {}, "Div": {}, "Aside": {}, "Title": {}, "NonStruct": {}, "H": {}, "P": {}, "H1": {}, "H2": {}, "H3": {}, "H4": {}, "H5": {}, "H6": {}, "Lbl": {}, "L": {}, "LI": {}, "LBody": {}, "Table": {}, "TR": {}, "TH": {}, "TD": {}, "THead": {}, "TBody": {}, "TFoot": {}, "Span": {}, "FENote": {}, "Link": {}, "Annot": {}, "Figure": {}, "Formula": {}, "Form": {}, "Artifact": {}, "Caption": {}, "Sub": {}, "Em": {}, "Strong": {}, "Ruby": {}, "RB": {}, "RT": {}, "RP": {}, "Warichu": {}, "WT": {}, "WP": {}}

// String gets a string representation of the violated rule.
func (v ViolatedRule) String() string { return fmt.Sprintf("%s: %s", v.RuleNo, v.Detail) }

// Part gets the PDF/UA part.
func (p *Profile2) Part() int { return p.std.part }

func (u *fontUsage) walkOps(ops *contentstream.ContentStreamOperations, scope resourceScope, depth int) {
	for _, op := range *ops {
		if len(op.Params) == 0 {
			continue
		}
		switch op.Operand {
		case "Tf":
			if name, ok := core.GetName(op.Params[0]); ok {
				u.selectFont(*name, scope, depth)
			}
		case "Do":
			if name, ok := core.GetName(op.Params[0]); ok {
				u.walkForm(*name, scope, depth)
			}
		case "scn", "SCN":
			if name, ok := core.GetName(op.Params[len(op.Params)-1]); ok {
				u.walkPattern(*name, scope, depth)
			}
		}
	}
}
func ua1Standard() standardType { return standardType{part: 1} }
func sumColSpans(cells []*core.PdfObjectDictionary, classMap *core.PdfObjectDictionary) int {
	total := 0
	for _, cell := range cells {
		total += spanAttr(cell, classMap, "ColSpan")
	}
	return total
}

func streamResources(stream *core.PdfObjectStream) *core.PdfObjectDictionary {
	res, _ := core.GetDict(stream.Get("Resources"))
	return res
}

// NewProfile2 creates a new Profile2 with the given options.
func NewProfile2(options *Profile2Options) *Profile2 {
	if options == nil {
		options = DefaultProfile2Options()
	}
	applyProfile2Defaults(options)
	return &Profile2{std: ua2Standard(), opts: *options}
}

func classMapOf(root *core.PdfObjectDictionary) *core.PdfObjectDictionary {
	classMap, _ := core.GetDict(core.TraceToDirectObject(root.Get("ClassMap")))
	return classMap
}

const (
	annotFlagHidden = 1 << 1
	annotFlagNoView = 1 << 5
)

func sumRowSpans(rows [][]*core.PdfObjectDictionary, classMap *core.PdfObjectDictionary) int {
	total := 0
	for i := 0; i < len(rows); i++ {
		if len(rows[i]) == 0 {
			continue
		}
		span := spanAttr(rows[i][0], classMap, "RowSpan")
		total += span
		i += span - 1
	}
	return total
}

type resourceScope struct {
	local *model.PdfPageResources
	page  *model.PdfPageResources
}

func resolveNamespacedType(elem, roleMap *core.PdfObjectDictionary) (string, string) {
	name, ok := core.GetName(elem.Get("S"))
	if !ok {
		return "", ""
	}
	typ := string(*name)
	ns, _ := core.GetDict(core.TraceToDirectObject(elem.Get("NS")))
	key := func(typ string, nsDict *core.PdfObjectDictionary) string { return typ + "\x00" + nsURI(nsDict) }
	seen := map[string]bool{key(typ, ns): true}
	nextTyp, nextNS, ok := mapNamespacedType(typ, ns, roleMap)
	if !ok || seen[key(nextTyp, nextNS)] {
		if isStandardType(typ, nsURI(ns)) {
			return typ, nsURI(ns)
		}
		return "", ""
	}
	for i := 0; i < maxRoleMapHops; i++ {
		if isStandardType(nextTyp, nsURI(nextNS)) {
			return nextTyp, nsURI(nextNS)
		}
		seen[key(nextTyp, nextNS)] = true
		if nextTyp, nextNS, ok = mapNamespacedType(nextTyp, nextNS, roleMap); !ok || seen[key(nextTyp, nextNS)] {
			return "", ""
		}
	}
	return "", ""
}

func roleMapCycles(roleMap *core.PdfObjectDictionary) []string {
	if roleMap == nil {
		return nil
	}
	var cyclic []string
	for _, key := range roleMap.Keys() {
		start := string(key)
		seen := map[string]struct{}{start: {}}
		cur := start
		for {
			name, ok := core.GetName(roleMap.Get(core.PdfObjectName(cur)))
			if !ok {
				break
			}
			cur = string(*name)
			if _, ok := seen[cur]; ok {
				cyclic = append(cyclic, start)
				break
			}
			seen[cur] = struct{}{}
		}
	}
	return cyclic
}

func setDisplayDocTitle(doc *docutil.Document) {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return
	}
	catDict := catalog.Object
	prefsObj := catDict.Get("ViewerPreferences")
	prefs, ok := core.GetDict(prefsObj)
	if !ok {
		prefs = core.MakeDict()
		prefsInd := core.MakeIndirectObject(prefs)
		catDict.Set("ViewerPreferences", prefsInd)
		doc.Objects = append(doc.Objects, prefsInd)
	}
	prefs.Set("DisplayDocTitle", core.MakeBool(true))
}

func (w *contentWalker) emit(ruleNo, detail string) {
	if w.emitted[ruleNo] {
		return
	}
	w.violations = append(w.violations, newViolation(ruleNo, detail))
	w.emitted[ruleNo] = true
}

func (u *fontUsage) budget(depth int) bool {
	if depth >= maxResourceDepth || u.visits >= maxStreamVisits {
		u.determined = false
		return false
	}
	u.visits++
	return true
}

func catalogVersion(r *model.CompliancePdfReader) (core.Version, bool) {
	verObj, ok := r.GetCatalogVersion()
	if !ok {
		return core.Version{}, false
	}
	name, ok := core.GetName(core.TraceToDirectObject(verObj))
	if !ok {
		return core.Version{}, false
	}
	parts := strings.SplitN(string(*name), ".", 2)
	if len(parts) != 2 {
		return core.Version{}, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return core.Version{}, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return core.Version{}, false
	}
	return core.Version{Major: major, Minor: minor}, true
}

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

func streamsFromObject(obj core.PdfObject) []*core.PdfObjectStream {
	obj = core.TraceToDirectObject(obj)
	if stream, ok := core.GetStream(obj); ok {
		return []*core.PdfObjectStream{stream}
	}
	dict, ok := core.GetDict(obj)
	if !ok {
		return nil
	}
	var streams []*core.PdfObjectStream
	for _, key := range dict.Keys() {
		if stream, ok := core.GetStream(dict.Get(key)); ok {
			streams = append(streams, stream)
		}
	}
	return streams
}

// ValidateStandard checks if the input CompliancePdfReader matches the PDF/UA-1 standard.
//
// Known limitations (may yield false negatives): font checks (embedding/ToUnicode,
// 7.21.4.1-1, 7.21.7-1) take their candidates from page resources and the Form
// XObjects those resources declare, and resolve which of them render by walking
// page content streams and the forms they invoke. A font selected only in an
// annotation appearance stream, a Type3 CharProcs entry or tiling-pattern content
// is not seen. ParentTree/MCID cross-reference validity is not verified. See the
// scope comment at the top of verifiers.go for the full list of deferred rules.
func (p *Profile1) ValidateStandard(r *model.CompliancePdfReader) error {
	verr := VerificationError{Part: p.std.part}
	xmp := loadCatalogXMP(r)
	verr.ViolatedRules = append(verr.ViolatedRules, checkXMPIdentifier(xmp, p.std.part, p.std.rev, ua1Rules)...)
	if structIssues := checkStructTreeRootAndMarkInfo(r, ua1Rules); len(structIssues) != 0 {
		verr.ViolatedRules = append(verr.ViolatedRules, structIssues...)
	}
	if metaIssue := checkMetadataPresent(xmp, ua1Rules); metaIssue != noViolation {
		verr.ViolatedRules = append(verr.ViolatedRules, metaIssue)
	}
	if titleIssues := checkTitleAndViewerPrefs(r, xmp, ua1Rules); len(titleIssues) != 0 {
		verr.ViolatedRules = append(verr.ViolatedRules, titleIssues...)
	}
	if langIssue := checkLang(r, ua1Rules); langIssue != noViolation {
		verr.ViolatedRules = append(verr.ViolatedRules, langIssue)
	}
	if roleMapIssues := checkRoleMapUA1(r); len(roleMapIssues) != 0 {
		verr.ViolatedRules = append(verr.ViolatedRules, roleMapIssues...)
	}
	if figureIssues := checkFigureAlt(r, ua1Rules, newRoleMapResolver); len(figureIssues) != 0 {
		verr.ViolatedRules = append(verr.ViolatedRules, figureIssues...)
	}
	if headingIssues := checkHeadings(r); len(headingIssues) != 0 {
		verr.ViolatedRules = append(verr.ViolatedRules, headingIssues...)
	}
	if tableIssues := checkTables(r); len(tableIssues) != 0 {
		verr.ViolatedRules = append(verr.ViolatedRules, tableIssues...)
	}
	if listIssues := checkListItems(r); len(listIssues) != 0 {
		verr.ViolatedRules = append(verr.ViolatedRules, listIssues...)
	}
	if encryptIssue := checkEncryption(r); encryptIssue != noViolation {
		verr.ViolatedRules = append(verr.ViolatedRules, encryptIssue)
	}
	if annotIssues := checkAnnotationAltText(r); len(annotIssues) != 0 {
		verr.ViolatedRules = append(verr.ViolatedRules, annotIssues...)
	}
	if formIssues := checkFormFields(r); len(formIssues) != 0 {
		verr.ViolatedRules = append(verr.ViolatedRules, formIssues...)
	}
	if linkIssues := checkLinkAnnots(r); len(linkIssues) != 0 {
		verr.ViolatedRules = append(verr.ViolatedRules, linkIssues...)
	}
	scan := scanContent(r, ua1Rules)
	if contentIssues := scan.contentViolations; len(contentIssues) != 0 {
		verr.ViolatedRules = append(verr.ViolatedRules, contentIssues...)
	}
	if fontIssues := scan.fontViolations; len(fontIssues) != 0 {
		verr.ViolatedRules = append(verr.ViolatedRules, fontIssues...)
	}
	if len(verr.ViolatedRules) > 0 {
		return verr
	}
	return nil
}

const maxHeaderChecks = 1 << 22

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

type catalogXMP struct {
	present bool
	doc     *xmputil.Document
}

const (
	maxListedIDs = 10
	maxKidVisits = 100_000
)

func mapNamespacedType(typ string, ns, roleMap *core.PdfObjectDictionary) (string, *core.PdfObjectDictionary, bool) {
	if ns != nil {
		if roleMapNS, ok := core.GetDict(core.TraceToDirectObject(ns.Get("RoleMapNS"))); ok {
			switch mapped := core.TraceToDirectObject(roleMapNS.Get(core.PdfObjectName(typ))).(type) {
			case *core.PdfObjectName:
				return string(*mapped), nil, true
			case *core.PdfObjectArray:
				if mapped.Len() < 2 {
					return "", nil, false
				}
				name, ok := core.GetName(mapped.Get(0))
				if !ok {
					return "", nil, false
				}
				targetNS, ok := core.GetDict(core.TraceToDirectObject(mapped.Get(1)))
				if !ok || nsURI(targetNS) == "" {
					return "", nil, false
				}
				return string(*name), targetNS, true
			}
			return "", nil, false
		}
		if nsURI(ns) != model.StructureNamespacePDF17 {
			return "", nil, false
		}
	}
	std, ok := mapRole(roleMap, typ)
	return std, nil, ok
}

var groupingTypes = map[string]struct{}{"NonStruct": {}, "Div": {}, "Part": {}}

func (c *fontCandidates) addResources(res *core.PdfObjectDictionary, depth int) {
	if res == nil || depth > maxResourceDepth {
		return
	}
	if fonts, ok := core.GetDict(res.Get("Font")); ok {
		for _, key := range fonts.Keys() {
			fontObj := core.TraceToDirectObject(fonts.Get(key))
			if _, seen := c.seenFonts[fontObj]; seen {
				continue
			}
			c.seenFonts[fontObj] = struct{}{}
			fontDict, ok := core.GetDict(fontObj)
			if !ok {
				continue
			}
			c.fonts = append(c.fonts, fontDict)
			if !c.addType3Resources(fontDict, depth) {
				return
			}
		}
	}
	for _, pattern := range streamsInDict(res.Get("Pattern")) {
		if !c.descend(pattern, depth) {
			return
		}
	}
	xobjects, ok := core.GetDict(res.Get("XObject"))
	if !ok {
		return
	}
	for _, key := range xobjects.Keys() {
		stream, ok := core.GetStream(xobjects.Get(key))
		if !ok {
			continue
		}
		if subtype, ok := core.GetName(stream.Get("Subtype")); !ok || string(*subtype) != "Form" {
			continue
		}
		if !c.descend(stream, depth) {
			return
		}
	}
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

func selectSuspectViolations(fonts []suspectFont, used map[*core.PdfObjectDictionary]bool, restrict bool) []ViolatedRule {
	var violations []ViolatedRule
	for _, f := range fonts {
		if restrict && !used[f.font] {
			continue
		}
		violations = append(violations, f.violations...)
	}
	return violations
}

func (w *contentWalker) evalContent(stack []mcFrame) {
	state := 0
	inArtifact := false
	for _, frame := range slices.Backward(stack) {

		if frame.artifact {
			inArtifact = true
			if state == 0 {
				state = 2
			}
		} else if frame.tagged && state == 0 {
			state = 1
		}
	}
	switch {
	case state == 1 && inArtifact:
		w.emit(w.rules.taggedArtifact, "tagged content shall not be nested inside an Artifact marked-content sequence")
	case state == 0:
		w.emit(w.rules.unmarkedContent, "real content shall be inside a marked-content sequence identified by an MCID (BDC with MCID property)")
	}
}

func isReportableAnnot(annot *core.PdfObjectDictionary) bool {
	if subtype, ok := core.GetName(annot.Get("Subtype")); ok && string(*subtype) == "Popup" {
		return false
	}
	if flags, ok := core.GetIntVal(annot.Get("F")); ok && flags&(annotFlagHidden|annotFlagNoView) != 0 {
		return false
	}
	return true
}

func (u *fontUsage) walkPattern(name core.PdfObjectName, scope resourceScope, depth int) {
	for _, res := range scope.lookIn() {
		if res == nil {
			continue
		}
		patObj := dictGet(res.Pattern, name)
		if patObj == nil {
			continue
		}
		stream, ok := core.GetStream(patObj)
		if !ok {
			return
		}
		key := walkKey{obj: stream, pageRes: scope.page}
		if u.visited[key] {
			return
		}
		if !u.budget(depth) {
			return
		}
		u.visited[key] = true
		u.walkStream(stream, resourceScope{page: scope.page}, depth+1)
		return
	}
}
func (w *contentWalker) done() bool { return w.taggingDone() && w.fonts == nil }
func summarizeIDs(names []string) string {
	if len(names) <= maxListedIDs {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(names[:maxListedIDs], ", "), len(names)-maxListedIDs)
}

func (w *contentWalker) markUndetermined() {
	if w.fonts != nil {
		w.fonts.determined = false
	}
}

func scanContent(r *model.CompliancePdfReader, rules ruleNos) contentScan {
	suspects := findSuspectFonts(r, rules)
	w := newContentWalker(r, rules, len(suspects) != 0)
	w.run()
	return contentScan{contentViolations: w.violations, fontViolations: selectSuspectViolations(suspects, w.usedFonts(), w.usageDetermined())}
}

// StandardName gets the name of the standard.
func (p *Profile2) StandardName() string { return p.std.String() }

func (u *fontUsage) walkStream(stream *core.PdfObjectStream, scope resourceScope, depth int) {
	data, err := core.DecodeStream(stream)
	if err != nil {
		u.determined = false
		return
	}
	u.walkContent(data, scope.with(resourcesFromDict(streamResources(stream))), depth)
}

func loadCatalogXMP(r *model.CompliancePdfReader) catalogXMP {
	metaObj, ok := r.GetCatalogMetadata()
	if !ok {
		return catalogXMP{}
	}
	stream, ok := core.GetStream(metaObj)
	if !ok {
		return catalogXMP{}
	}
	if typ, ok := core.GetName(stream.Get("Type")); !ok || string(*typ) != "Metadata" {
		return catalogXMP{}
	}
	if subtype, ok := core.GetName(stream.Get("Subtype")); !ok || string(*subtype) != "XML" {
		return catalogXMP{}
	}
	doc, err := xmputil.LoadDocument(streamData(stream))
	if err != nil {
		return catalogXMP{present: true}
	}
	return catalogXMP{present: true, doc: doc}
}

func setPDF20Version(doc *docutil.Document) {
	doc.Version.Major, doc.Version.Minor = 2, 0
	catalog, ok := doc.FindCatalog()
	if !ok {
		return
	}
	catalog.SetVersion()
}

func (c *fontCandidates) descend(stream *core.PdfObjectStream, depth int) bool {
	if _, seen := c.activeStreams[stream]; seen {
		return true
	}
	if c.visits >= maxStreamVisits {
		return false
	}
	c.visits++
	c.activeStreams[stream] = struct{}{}
	c.addResources(streamResources(stream), depth+1)
	delete(c.activeStreams, stream)
	return true
}

// ApplyStandard applies the mechanical subset of PDF/UA-2 requirements.
func (p *Profile2) ApplyStandard(document *docutil.Document) error {
	if err := requireStructTree(document); err != nil {
		return err
	}
	setPDF20Version(document)
	ensureMarkInfo(document)
	setDisplayDocTitle(document)
	setDefaultLanguage(document, p.opts.DefaultLanguage)
	setTabsToStructure(document)
	ensurePDF2Namespace(document)
	return writeXMP(document, p.std, p.opts.Xmp)
}

type contentScan struct {
	contentViolations []ViolatedRule
	fontViolations    []ViolatedRule
}

func (w *contentWalker) run() {
	numPages, err := w.r.GetNumPages()
	if err != nil {
		w.markUndetermined()
		return
	}
	if root, ok := structTreeRootOf(w.r); ok {
		walkStructTree(root, newRoleMapResolver(root), func(elem *structElem) { w.hasStructTree = true })
	}
	for pageNum := 1; pageNum <= numPages; pageNum++ {
		if w.done() {
			w.truncated = true
			break
		}
		page, err := w.r.GetPage(pageNum)
		if err != nil {
			continue
		}
		var ops *contentstream.ContentStreamOperations
		if content, err := page.GetAllContentStreams(); err == nil {
			if parsed, err := contentstream.NewContentStreamParser(content).Parse(); err == nil {
				ops = parsed
			}
		}
		if ops != nil && !w.taggingDone() {
			w.walk(ops, page.Resources, nil, 0)
		}
		if w.fonts != nil {
			w.fonts.addPage(page, ops)
		}
	}
}

func newViolation(ruleNo string, detail string) ViolatedRule {
	return ViolatedRule{RuleNo: ruleNo, Detail: detail}
}

func spanAttr(elem, classMap *core.PdfObjectDictionary, key core.PdfObjectName) int {
	n := intAttr(elem, classMap, key, "Table", 1)
	if n < 1 {
		return 1
	}
	if n > maxSpan {
		return maxSpan
	}
	return n
}

type structElem struct {
	dict     *core.PdfObjectDictionary
	stdType  string
	typeName string
	parent   *structElem
}

const maxStreamVisits = 4096

func pageHasReportableAnnots(page *model.PdfPage) bool {
	annots, err := page.GetAnnotations()
	if err != nil {
		return false
	}
	for _, annot := range annots {
		if dict, ok := core.GetDict(annot.GetContainingPdfObject()); ok && isReportableAnnot(dict) && !rectOffPage(dict.Get("Rect"), page) {
			return true
		}
	}
	return false
}

var errNotTagged = errors.New("pdfua: document is not a tagged PDF (no StructTreeRoot); PDF/UA structure cannot be generated automatically")

func applyProfile2Defaults(opts *Profile2Options) {
	if opts.Xmp.MarshalIndent == "" {
		opts.Xmp.MarshalIndent = "\t"
	}
}

type tableRows struct {
	rows        [][]*core.PdfObjectDictionary
	groupBounds []int
}

func structTreeRootOf(r *model.CompliancePdfReader) (*core.PdfObjectDictionary, bool) {
	obj, ok := r.GetCatalogStructTreeRoot()
	if !ok {
		return nil, false
	}
	return core.GetDict(core.TraceToDirectObject(obj))
}

type ruleNos struct {
	metadataStream   string
	xmpPartMissing   string
	xmpPartMismatch  string
	xmpRevMismatch   string
	structTreeRoot   string
	markInfoMarked   string
	markInfoSuspects string
	docTitle         string
	displayDocTitle  string
	langMissing      string
	langEmpty        string
	figureAlt        string
	unmarkedContent  string
	taggedArtifact   string
	fontEmbedded     string
	fontToUnicode    string
	fontCIDToGIDMap  string
	xmpTitleOnly     bool
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

var headingRe = regexp.MustCompile("^H([1-9][0-9]*)$")

type walkKey struct {
	obj     core.PdfObject
	pageRes *model.PdfPageResources
}

func setTabsToStructure(doc *docutil.Document) {
	pages, ok := doc.GetPages()
	if !ok {
		return
	}
	for i := range pages {
		pageDict := pages[i].Object
		annots := pageDict.Get("Annots")
		if annots == nil {
			continue
		}
		if arr, ok := core.GetArray(annots); ok && arr.Len() == 0 {
			continue
		}
		pageDict.Set("Tabs", core.MakeName("S"))
	}
}

// ApplyStandard applies the mechanical subset of PDF/UA-1 requirements to the
// document. It cannot synthesize semantics: if the document is not a tagged
// PDF (no StructTreeRoot), an error is returned.
func (p *Profile1) ApplyStandard(document *docutil.Document) error {
	if err := requireStructTree(document); err != nil {
		return err
	}
	ensureMarkInfo(document)
	setDisplayDocTitle(document)
	setDefaultLanguage(document, p.opts.DefaultLanguage)
	setTabsToStructure(document)
	if err := writeXMP(document, p.std, p.opts.Xmp); err != nil {
		return err
	}
	return nil
}

// DefaultProfile1Options are the default options for the Profile1.
func DefaultProfile1Options() *Profile1Options {
	return &Profile1Options{Xmp: XmpOptions{MarshalIndent: "\t"}}
}

func objectNumber(obj core.PdfObject) (int64, bool) {
	switch ref := obj.(type) {
	case *core.PdfObjectReference:
		return ref.ObjectNumber, true
	case *core.PdfIndirectObject:
		return ref.ObjectNumber, true
	case *core.PdfObjectStream:
		return ref.ObjectNumber, true
	}
	return 0, false
}

type contentWalker struct {
	r             *model.CompliancePdfReader
	rules         ruleNos
	hasStructTree bool
	violations    []ViolatedRule
	emitted       map[string]bool
	fonts         *fontUsage
	truncated     bool
	visiting      map[*core.PdfObjectStream]bool
	formVisits    int
}

func typeOrEmpty(elem *core.PdfObjectDictionary, resolve structTypeFunc) string {
	if elem == nil {
		return ""
	}
	return resolve(elem)
}

func isStandardType(typ, ns string) bool {
	switch ns {
	case model.StructureNamespaceMathML:
		return true
	case model.StructureNamespacePDF2:
		return isPDF20Type(typ)
	case model.StructureNamespacePDF17, "":
		_, ok := pdf17StructTypes[typ]
		return ok
	}
	return false
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

func rectOffPage(rectObj core.PdfObject, page *model.PdfPage) bool {
	arr, ok := core.GetArray(core.TraceToDirectObject(rectObj))
	if !ok || arr.Len() != 4 {
		return false
	}
	rect, err := model.NewPdfRectangle(*arr)
	if err != nil {
		return false
	}
	box := page.CropBox
	if box == nil {
		box = page.MediaBox
	}
	if box == nil {
		return false
	}
	minX, maxX := min(rect.Llx, rect.Urx), max(rect.Llx, rect.Urx)
	minY, maxY := min(rect.Lly, rect.Ury), max(rect.Lly, rect.Ury)
	boxMinX, boxMaxX := min(box.Llx, box.Urx), max(box.Llx, box.Urx)
	boxMinY, boxMaxY := min(box.Lly, box.Ury), max(box.Lly, box.Ury)
	return maxX < boxMinX || minX > boxMaxX || maxY < boxMinY || minY > boxMaxY
}

func streamsInDict(obj core.PdfObject) []*core.PdfObjectStream {
	dict, ok := core.GetDict(core.TraceToDirectObject(obj))
	if !ok {
		return nil
	}
	var streams []*core.PdfObjectStream
	for _, key := range dict.Keys() {
		if stream, ok := core.GetStream(dict.Get(key)); ok {
			streams = append(streams, stream)
		}
	}
	return streams
}

type fontUsage struct {
	used        map[*core.PdfObjectDictionary]bool
	determined  bool
	activeForms map[*core.PdfObjectStream]bool
	visited     map[walkKey]bool
	visits      int
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

// ValidateStandard checks if the input matches the PDF/UA-2 standard.
func (p *Profile2) ValidateStandard(r *model.CompliancePdfReader) error {
	verr := VerificationError{Part: p.std.part}
	addOne := func(v ViolatedRule) {
		if v != noViolation {
			verr.ViolatedRules = append(verr.ViolatedRules, v)
		}
	}
	addAll := func(vs []ViolatedRule) {
		for _, v := range vs {
			if v != noViolation {
				verr.ViolatedRules = append(verr.ViolatedRules, v)
			}
		}
	}
	xmp := loadCatalogXMP(r)
	addOne(checkPDF20Version(r))
	addAll(checkXMPIdentifier(xmp, p.std.part, p.std.rev, ua2Rules))
	addAll(checkStructTreeRootAndMarkInfo(r, ua2Rules))
	addOne(checkMetadataPresent(xmp, ua2Rules))
	addAll(checkTitleAndViewerPrefs(r, xmp, ua2Rules))
	addOne(checkLang(r, ua2Rules))
	addAll(checkFigureAlt(r, ua2Rules, newNamespaceResolver))
	scan := scanContent(r, ua2Rules)
	addAll(scan.contentViolations)
	addAll(scan.fontViolations)
	addAll(checkRoleMapUA2(r))
	addAll(checkXFA(r))
	addAll(checkTabsEntries(r))
	if len(verr.ViolatedRules) > 0 {
		return verr
	}
	return nil
}

func collectFontDicts(r *model.CompliancePdfReader) []*core.PdfObjectDictionary {
	cands := &fontCandidates{seenFonts: map[core.PdfObject]struct{}{}, activeStreams: map[*core.PdfObjectStream]struct{}{}}
	numPages, err := r.GetNumPages()
	if err != nil {
		return nil
	}
	for pageNum := 1; pageNum <= numPages; pageNum++ {
		page, err := r.GetPage(pageNum)
		if err != nil {
			continue
		}
		if page.Resources != nil {
			res := core.MakeDict()
			if page.Resources.Font != nil {
				res.Set("Font", page.Resources.Font)
			}
			if page.Resources.XObject != nil {
				res.Set("XObject", page.Resources.XObject)
			}
			if page.Resources.Pattern != nil {
				res.Set("Pattern", page.Resources.Pattern)
			}
			cands.addResources(res, 0)
		}
		streams, _ := annotAppearanceStreams(page)
		for _, stream := range streams {
			cands.addResources(streamResources(stream), 0)
		}
	}
	return cands.fonts
}

const maxSpan = 1024

func hasCircularNSMapping(elem *core.PdfObjectDictionary) bool {
	name, ok := core.GetName(elem.Get("S"))
	if !ok {
		return false
	}
	typ := string(*name)
	ns, _ := core.GetDict(core.TraceToDirectObject(elem.Get("NS")))
	key := func(typ string, nsDict *core.PdfObjectDictionary) string { return typ + "\x00" + nsURI(nsDict) }
	seen := map[string]bool{key(typ, ns): true}
	typeByNS := map[string]string{}
	nextTyp, nextNS, ok := mapNamespacedType(typ, ns, nil)
	for i := 0; ok && i < maxRoleMapHops; i++ {
		if curURI := nsURI(ns); curURI != "" {
			typeByNS[curURI] = typ
		}
		if nextURI := nsURI(nextNS); nextURI != "" {
			if prev, ok := typeByNS[nextURI]; ok && prev == nextTyp {
				return true
			}
		}
		if seen[key(nextTyp, nextNS)] {
			return false
		}
		seen[key(nextTyp, nextNS)] = true
		typ, ns = nextTyp, nextNS
		nextTyp, nextNS, ok = mapNamespacedType(typ, ns, nil)
	}
	return false
}
func ua2Standard() standardType { return standardType{part: 2, rev: 2024} }
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

const structRootRuleTable5 = "Table 5. StructTreeRoot-Document-1"

type annotStructInfo struct {
	elemType string
	hasAlt   bool
}

func nsURI(ns *core.PdfObjectDictionary) string {
	if ns == nil {
		return ""
	}
	uri, ok := core.GetString(ns.Get("NS"))
	if !ok {
		return ""
	}
	return uri.Decoded()
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

func buildTableGrid(tableElem, classMap *core.PdfObjectDictionary, resolve structTypeFunc, checkGroups bool) (*tableGrid, []ViolatedRule) {
	tr := collectTableRows(tableElem, resolve)
	numRows := sumRowSpans(tr.rows, classMap)
	if numRows == 0 {
		return nil, nil
	}
	numCols := sumColSpans(tr.rows[0], classMap)
	if numCols != 0 && numRows > maxGridCells/numCols {
		return nil, []ViolatedRule{newViolation("8.2.5.26", fmt.Sprintf("table too large to check for regularity (grid exceeds %d cells); not validated", maxGridCells))}
	}
	grid := &tableGrid{rows: numRows, cols: numCols, cells: make([]*core.PdfObjectDictionary, numRows*numCols)}
	setCell := func(r, c int, cell *core.PdfObjectDictionary) {
		if r < numRows && c < numCols {
			grid.cells[r*numCols+c] = cell
		}
	}
	for r, rowCells := range tr.rows {
		c := 0
		for _, cell := range rowCells {
			rowSpan := spanAttr(cell, classMap, "RowSpan")
			colSpan := spanAttr(cell, classMap, "ColSpan")
			for c < numCols && grid.at(r, c) != nil {
				c++
			}
			if c+colSpan > numCols {
				if numCols == 0 {
					return nil, []ViolatedRule{newViolation("8.2.5.26-3", fmt.Sprintf("table row %d has cells but the first row declares no columns", r+1))}
				}
				return nil, []ViolatedRule{newViolation("8.2.5.26-3", fmt.Sprintf("table row %d spans more than the %d columns of the first row", r+1, numCols))}
			}
			if r+rowSpan > numRows {
				return nil, []ViolatedRule{newViolation("8.2.5.26-2", fmt.Sprintf("a cell in table column %d spans past the last row of the table", c+1))}
			}
			if checkGroups && rowSpan > 1 {
				for _, bound := range tr.groupBounds {
					if r < bound && r+rowSpan > bound {
						return nil, []ViolatedRule{newViolation("8.2.5.26-2", fmt.Sprintf("a cell in table column %d spans across a THead/TBody/TFoot row grouping boundary", c+1))}
					}
				}
			}
			for dr := 0; dr < rowSpan; dr++ {
				for dc := 0; dc < colSpan; dc++ {
					if grid.at(r+dr, c+dc) != nil {
						return nil, []ViolatedRule{newViolation("8.2.5.26-1", "a table cell intersects another cell")}
					}
					setCell(r+dr, c+dc, cell)
				}
			}
			grid.placements = append(grid.placements, tableCellPlacement{cell: cell, row: r, col: c, rowSpan: rowSpan, colSpan: colSpan})
			c += colSpan
		}
		if len(rowCells) == 0 && numCols > 0 {
			return nil, []ViolatedRule{newViolation("8.2.5.26-4", fmt.Sprintf("table row %d contains no cells; the first row spans %d columns", r+1, numCols))}
		}
	}
	for r := 0; r < numRows; r++ {
		filled := 0
		for c := 0; c < numCols; c++ {
			if grid.cells[r*numCols+c] != nil {
				filled++
			}
		}
		if filled != numCols {
			return nil, []ViolatedRule{newViolation("8.2.5.26-4", fmt.Sprintf("table row %d spans %d columns, but the first row spans %d", r+1, filled, numCols))}
		}
	}
	return grid, nil
}

func ensureMarkInfo(doc *docutil.Document) {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return
	}
	if markInfo, ok := catalog.GetMarkInfo(); ok && markInfo != nil {
		markInfo.Set("Marked", core.MakeBool(true))
		markInfo.Remove("Suspects")
		return
	}
	markInfo := core.MakeDict()
	markInfo.Set("Marked", core.MakeBool(true))
	catalog.SetMarkInfo(markInfo)
}

type fontCandidates struct {
	fonts         []*core.PdfObjectDictionary
	seenFonts     map[core.PdfObject]struct{}
	activeStreams map[*core.PdfObjectStream]struct{}
	visits        int
}

func listHasLabels(list *core.PdfObjectDictionary, resolve structTypeFunc) bool {
	for _, item := range structChildren(list.Get("K"), resolve) {
		if resolve(item) != "LI" {
			continue
		}
		for _, child := range structChildren(item.Get("K"), resolve) {
			if resolve(child) == "Lbl" {
				return true
			}
		}
	}
	return false
}

var paintingOperators = map[string]struct{}{"Tj": {}, "TJ": {}, "'": {}, "\"": {}, "S": {}, "s": {}, "f": {}, "F": {}, "f*": {}, "B": {}, "B*": {}, "b": {}, "b*": {}, "Do": {}, "sh": {}, "BI": {}}

func nameAttr(elem, classMap *core.PdfObjectDictionary, key core.PdfObjectName, owner string) string {
	for cur, depth := elem, 0; cur != nil && depth <= maxTreeDepth; depth++ {
		if obj := lookupAttr(cur, classMap, key, owner, isName); obj != nil {
			name, _ := core.GetName(obj)
			return string(*name)
		}
		cur, _ = core.GetDict(cur.Get("P"))
	}
	return ""
}

func collectTableRows(tableElem *core.PdfObjectDictionary, resolve structTypeFunc) tableRows {
	cellsOf := func(row *core.PdfObjectDictionary) []*core.PdfObjectDictionary {
		var cells []*core.PdfObjectDictionary
		for _, kid := range structChildren(row.Get("K"), resolve) {
			switch resolve(kid) {
			case "TH", "TD":
				cells = append(cells, kid)
			}
		}
		return cells
	}
	var tr tableRows
	for _, kid := range structChildren(tableElem.Get("K"), resolve) {
		switch resolve(kid) {
		case "TR":
			tr.rows = append(tr.rows, cellsOf(kid))
		case "THead", "TBody", "TFoot":
			tr.groupBounds = append(tr.groupBounds, len(tr.rows))
			for _, rowKid := range structChildren(kid.Get("K"), resolve) {
				if resolve(rowKid) != "TR" {
					continue
				}
				tr.rows = append(tr.rows, cellsOf(rowKid))
			}
			tr.groupBounds = append(tr.groupBounds, len(tr.rows))
		}
	}
	return tr
}

func findAttr(attrs core.PdfObject, key core.PdfObjectName, owner string, accept func(core.PdfObject) bool) core.PdfObject {
	fromDict := func(d *core.PdfObjectDictionary) core.PdfObject {
		if ownerName, ok := core.GetName(d.Get("O")); !ok || string(*ownerName) != owner {
			return nil
		}
		val := d.Get(key)
		if val == nil || !accept(val) {
			return nil
		}
		return val
	}
	attrs = core.TraceToDirectObject(attrs)
	if d, ok := core.GetDict(attrs); ok {
		return fromDict(d)
	}
	if arr, ok := core.GetArray(attrs); ok {
		for _, item := range arr.Elements() {
			d, ok := core.GetDict(core.TraceToDirectObject(item))
			if !ok {
				continue
			}
			if val := fromDict(d); val != nil {
				return val
			}
		}
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

// NewProfile1 creates a new Profile1 with given options.
func NewProfile1(options *Profile1Options) *Profile1 {
	if options == nil {
		options = DefaultProfile1Options()
	}
	applyProfile1Defaults(options)
	return &Profile1{std: ua1Standard(), opts: *options}
}

// VerificationError is the PDF/UA verification error containing all violated rules.
type VerificationError struct {
	// ViolatedRules are the rules that were violated during verification.
	ViolatedRules []ViolatedRule

	// Part defines the PDF/UA part (1 or 2) on which verification failed.
	Part int
}

func hasAdobeCIDOrdering(font *core.PdfObjectDictionary) bool {
	descendants, ok := core.GetArray(font.Get("DescendantFonts"))
	if !ok || descendants.Len() == 0 {
		return false
	}
	cidFont, ok := core.GetDict(descendants.Get(0))
	if !ok {
		return false
	}
	sysInfo, ok := core.GetDict(core.TraceToDirectObject(cidFont.Get("CIDSystemInfo")))
	if !ok {
		return false
	}
	registry, okReg := core.GetString(sysInfo.Get("Registry"))
	ordering, okOrd := core.GetString(sysInfo.Get("Ordering"))
	return okReg && registry.Decoded() == "Adobe" && okOrd && adobeCJKOrderings[ordering.Decoded()]
}

func isInt(obj core.PdfObject) bool {
	_, ok := core.GetIntVal(obj)
	return ok
}

type mcFrame struct {
	tagged   bool
	artifact bool
}

func ensurePDF2Namespace(doc *docutil.Document) {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return
	}
	root, ok := catalog.GetStructTreeRoot()
	if !ok {
		return
	}
	nsObj := core.TraceToDirectObject(root.Get("Namespaces"))
	if arr, ok := core.GetArray(nsObj); ok {
		for _, item := range arr.Elements() {
			d, ok := core.GetDict(core.TraceToDirectObject(item))
			if !ok {
				continue
			}
			uri, ok := core.GetString(d.Get("NS"))
			if ok && uri.Decoded() == model.StructureNamespacePDF2 {
				return
			}
		}
	}
	nsModel := model.NewPdfStructureNamespace(model.StructureNamespacePDF2)
	newNS := nsModel.ToPdfObject()
	if ind, ok := newNS.(*core.PdfIndirectObject); ok {
		doc.AddIndirectObject(ind)
	}
	var namespaces *core.PdfObjectArray
	if existing, ok := core.GetArray(nsObj); ok {
		namespaces = existing
	} else {
		namespaces = core.MakeArray()
	}
	namespaces.Append(newNS)
	root.Set("Namespaces", namespaces)
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

func hasMCID(props core.PdfObject, res *model.PdfPageResources) bool {
	dict := propertyDict(props, res)
	if dict == nil {
		return false
	}
	_, ok := core.GetIntVal(dict.Get("MCID"))
	return ok
}

func isName(obj core.PdfObject) bool {
	_, ok := core.GetName(obj)
	return ok
}

func (u *fontUsage) selectFont(name core.PdfObjectName, scope resourceScope, depth int) {
	for _, res := range scope.lookIn() {
		if res == nil {
			continue
		}
		fontObj, ok := res.GetFontByName(name)
		if !ok {
			continue
		}
		fontDict, isDict := core.GetDict(core.TraceToDirectObject(fontObj))
		if !isDict {
			return
		}
		u.used[fontDict] = true
		u.walkCharProcs(fontDict, scope, depth)
		return
	}
}

func writeXMP(doc *docutil.Document, std standardType, opts XmpOptions) error {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return errors.New("pdfua: document catalog not found")
	}
	var xmpDoc *xmputil.Document
	if metaStream, ok := catalog.GetMetadata(); ok {
		loaded, err := xmputil.LoadDocument(streamData(metaStream))
		if err != nil {
			xmpDoc = xmputil.NewDocument()
		} else {
			xmpDoc = loaded
		}
	} else {
		xmpDoc = xmputil.NewDocument()
	}
	infoOpts := xmputil.PdfInfoOptions{InfoDict: doc.Info, PdfVersion: fmt.Sprintf("%d.%d", doc.Version.Major, doc.Version.Minor), Copyright: opts.Copyright, Overwrite: true}
	markInfo, ok := catalog.GetMarkInfo()
	if ok {
		marked, ok := core.GetBool(markInfo.Get("Marked"))
		if ok && bool(*marked) {
			infoOpts.Marked = true
		}
	}
	if err := xmpDoc.SetPdfInfo(&infoOpts); err != nil {
		return err
	}
	var err error
	if std.rev != 0 {
		err = xmpDoc.SetPdfUAIDWithRev(std.part, std.rev)
	} else {
		err = xmpDoc.SetPdfUAID(std.part)
	}
	if err != nil {
		return err
	}
	data, err := xmpDoc.MarshalIndent(opts.MarshalPrefix, opts.MarshalIndent)
	if err != nil {
		return err
	}
	return catalog.SetMetadata(data)
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

func collectAnnotStructInfo(root *core.PdfObjectDictionary) map[int64]annotStructInfo {
	info := map[int64]annotStructInfo{}
	walkStructTree(root, newRoleMapResolver(root), func(elem *structElem) {
		hasAlt := false
		if alt, ok := core.GetString(core.TraceToDirectObject(elem.dict.Get("Alt"))); ok && alt.Decoded() != "" {
			hasAlt = true
		}
		forEachOBJR(elem.dict, func(obj core.PdfObject) {
			objNum, ok := objectNumber(obj)
			if !ok {
				return
			}
			entry := annotStructInfo{elemType: elem.stdType, hasAlt: hasAlt}
			if prev, ok := info[objNum]; ok {
				entry.hasAlt = entry.hasAlt || prev.hasAlt
				if prev.elemType == "Link" || prev.elemType == "Reference" {
					entry.elemType = prev.elemType
				}
			}
			info[objNum] = entry
		})
	})
	return info
}

func (u *fontUsage) walkForm(name core.PdfObjectName, scope resourceScope, depth int) {
	for _, res := range scope.lookIn() {
		if res == nil || dictGet(res.XObject, name) == nil {
			continue
		}
		stream, xobjType := res.GetXObjectByName(name)
		if xobjType != model.XObjectTypeForm {
			return
		}
		if u.activeForms[stream] {
			return
		}
		if !u.budget(depth) {
			return
		}
		form, err := res.GetXObjectFormByName(name)
		if err != nil {
			u.determined = false
			return
		}
		data, contentErr := form.GetContentStream()
		if contentErr != nil {
			u.determined = false
			return
		}
		u.activeForms[stream] = true
		u.walkContent(data, scope.with(form.Resources), depth+1)
		delete(u.activeForms, stream)
		return
	}
}

var ua2Rules = ruleNos{metadataStream: "8.11.1-2", xmpPartMissing: "5-1", xmpPartMismatch: "5-2", xmpRevMismatch: "5-5", structTreeRoot: "8.2.1-1", markInfoMarked: "6.2-1", markInfoSuspects: "", docTitle: "8.11.1-1", displayDocTitle: "8.11.2-1", langMissing: "8.4.4-1", langEmpty: "8.4.4-2", figureAlt: "8.2.5.28.2-1", unmarkedContent: "8.2.2-1", taggedArtifact: "8.2.2-1", fontEmbedded: "8.4.5.5.1-1", fontToUnicode: "8.4.5.8-1", fontCIDToGIDMap: "8.4.5.3.2-1", xmpTitleOnly: true}

func annotAppearanceStreams(page *model.PdfPage) ([]*core.PdfObjectStream, bool) {
	if page.Annots == nil {
		return nil, true
	}
	annots, ok := core.GetArray(page.Annots)
	if !ok {
		return nil, false
	}
	var streams []*core.PdfObjectStream
	for _, item := range annots.Elements() {
		dict, ok := core.GetDict(item)
		if !ok {
			continue
		}
		streams = append(streams, appearanceStreams(dict)...)
	}
	return streams, true
}

// XmpOptions are the options used when writing the PDF/UA XMP metadata.
type XmpOptions struct {
	// Copyright information.
	Copyright string

	// MarshalIndent defines marshaling indent of the XMP metadata.
	MarshalIndent string

	// MarshalPrefix defines marshaling prefix of the XMP metadata.
	MarshalPrefix string
}

var ua1Rules = ruleNos{metadataStream: "7.1-8", xmpPartMissing: "5-1", xmpPartMismatch: "5-2", structTreeRoot: "7.1-11", markInfoMarked: "6.2-1", markInfoSuspects: "7.1-4", docTitle: "7.1-9", displayDocTitle: "7.1-10", langMissing: "", langEmpty: "7.2-29", figureAlt: "7.3-1", unmarkedContent: "7.1-3", taggedArtifact: "7.1-2", fontEmbedded: "7.21.4.1-1", fontToUnicode: "7.21.7-1", fontCIDToGIDMap: "7.21.3.2-1"}

func (u *fontUsage) addPage(page *model.PdfPage, ops *contentstream.ContentStreamOperations) {
	scope := resourceScope{page: page.Resources}
	if ops == nil {
		u.determined = false
	} else {
		u.walkOps(ops, scope, 0)
	}
	streams, ok := annotAppearanceStreams(page)
	if !ok {
		u.determined = false
		return
	}
	for _, stream := range streams {
		u.walkStream(stream, scope, 0)
	}
}

func (s resourceScope) lookIn() [2]*model.PdfPageResources {
	return [2]*model.PdfPageResources{s.local, s.page}
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

type tableGrid struct {
	rows, cols int
	cells      []*core.PdfObjectDictionary
	placements []tableCellPlacement
}

const maxTreeDepth = 1000

func (w *contentWalker) taggingDone() bool {
	return w.emitted[w.rules.unmarkedContent] && w.emitted[w.rules.taggedArtifact]
}

const maxGridCells = 1 << 20

// Profile1Options are the options for the PDF/UA-1 profile.
type Profile1Options struct {
	// DefaultLanguage is set as the catalog Lang entry by ApplyStandard when
	// the document defines none. Ignored when empty.
	DefaultLanguage string

	// Xmp is the xmp options information.
	Xmp XmpOptions
}

func resourcesFromDict(dict *core.PdfObjectDictionary) *model.PdfPageResources {
	if dict == nil {
		return nil
	}
	res, _ := model.NewPdfPageResourcesFromDict(dict)
	return res
}

func cellScope(elem, classMap *core.PdfObjectDictionary) string {
	obj := lookupAttr(elem, classMap, "Scope", "Table", isName)
	if obj == nil {
		return ""
	}
	name, _ := core.GetName(obj)
	return string(*name)
}

func (c *fontCandidates) addType3Resources(font *core.PdfObjectDictionary, depth int) bool {
	if subtype, ok := core.GetName(font.Get("Subtype")); !ok || string(*subtype) != "Type3" {
		return true
	}
	if res, ok := core.GetDict(core.TraceToDirectObject(font.Get("Resources"))); ok {
		c.addResources(res, depth+1)
	}
	counted := false
	for _, proc := range charProcStreams(font) {
		if _, seen := c.activeStreams[proc]; seen {
			continue
		}
		if !counted {
			if c.visits >= maxStreamVisits {
				return false
			}
			c.visits++
			counted = true
		}
		c.activeStreams[proc] = struct{}{}
		c.addResources(streamResources(proc), depth+1)
		delete(c.activeStreams, proc)
	}
	return true
}

func (w *contentWalker) usedFonts() map[*core.PdfObjectDictionary]bool {
	if w.fonts == nil {
		return nil
	}
	return w.fonts.used
}

type structTypeFunc func(elem *core.PdfObjectDictionary) string

func (w *contentWalker) descend(name core.PdfObjectName, stream *core.PdfObjectStream, res *model.PdfPageResources, stack []mcFrame, depth int) {
	if w.visiting[stream] || depth >= maxResourceDepth || w.formVisits >= maxStreamVisits {
		return
	}
	w.formVisits++
	form, err := res.GetXObjectFormByName(name)
	if err != nil {
		return
	}
	data, err := form.GetContentStream()
	if err != nil {
		return
	}
	ops, err := contentstream.NewContentStreamParser(string(data)).Parse()
	if err != nil {
		return
	}
	formRes := form.Resources
	if formRes == nil {
		formRes = res
	}
	w.visiting[stream] = true
	w.walk(ops, formRes, append([]mcFrame(nil), stack...), depth+1)
	w.visiting[stream] = false
}

var adobeCJKOrderings = map[string]bool{"Japan1": true, "GB1": true, "CNS1": true, "Korea1": true, "KR": true}

func newFontUsage() *fontUsage {
	return &fontUsage{used: map[*core.PdfObjectDictionary]bool{}, determined: true, activeForms: map[*core.PdfObjectStream]bool{}, visited: map[walkKey]bool{}}
}

func checkTableRegularity(tableElem, classMap *core.PdfObjectDictionary, resolve structTypeFunc) []ViolatedRule {
	_, violations := buildTableGrid(tableElem, classMap, resolve, true)
	return violations
}

type tableCellPlacement struct {
	cell     *core.PdfObjectDictionary
	row, col int
	rowSpan  int
	colSpan  int
}

func walkStructTree(root *core.PdfObjectDictionary, resolve structTypeFunc, visit func(elem *structElem)) {
	active := map[*core.PdfObjectDictionary]struct{}{}
	count := 0
	var walk func(kids core.PdfObject, parent *structElem, depth int)
	walk = func(kids core.PdfObject, parent *structElem, depth int) {
		if depth > maxTreeDepth {
			return
		}
		count++
		if count > maxStructNodes {
			return
		}
		kids = core.TraceToDirectObject(kids)
		if kids == nil {
			return
		}
		switch node := kids.(type) {
		case *core.PdfObjectArray:
			for _, kid := range node.Elements() {
				walk(kid, parent, depth+1)
			}
		case *core.PdfObjectDictionary:
			if _, ok := active[node]; ok {
				return
			}
			if typ, ok := core.GetName(node.Get("Type")); ok && string(*typ) == "OBJR" {
				return
			}
			active[node] = struct{}{}
			defer delete(active, node)
			s, ok := core.GetName(node.Get("S"))
			if !ok {
				walk(node.Get("K"), parent, depth+1)
				return
			}
			elem := &structElem{dict: node, typeName: string(*s), parent: parent}
			elem.stdType = resolve(node)
			visit(elem)
			walk(node.Get("K"), elem, depth+1)
		}
	}
	walk(root.Get("K"), nil, 0)
}

func applyProfile1Defaults(opts *Profile1Options) {
	if opts.Xmp.MarshalIndent == "" {
		opts.Xmp.MarshalIndent = "\t"
	}
}

// DefaultProfile2Options are the default options for Profile2.
func DefaultProfile2Options() *Profile2Options {
	return &Profile2Options{Xmp: XmpOptions{MarshalIndent: "\t"}}
}

// ViolatedRule defines a violated PDF/UA rule. RuleNo follows the clause
// numbering of ISO 14289 (e.g. "7.1-3").
type ViolatedRule struct {
	RuleNo string
	Detail string
}

func fieldHasTU(field *model.PdfField) bool {
	for f := field; f != nil; f = f.Parent {
		if f.TU != nil && f.TU.Decoded() != "" {
			return true
		}
		if d, ok := core.GetDict(core.TraceToDirectObject(f.GetContainingPdfObject())); ok {
			if tu, ok := core.GetString(core.TraceToDirectObject(d.Get("TU"))); ok && tu.Decoded() != "" {
				return true
			}
		}
	}
	return false
}

func newContentWalker(r *model.CompliancePdfReader, rules ruleNos, trackFonts bool) *contentWalker {
	w := &contentWalker{r: r, rules: rules, emitted: map[string]bool{}, visiting: map[*core.PdfObjectStream]bool{}}
	if trackFonts {
		w.fonts = newFontUsage()
	}
	return w
}

const maxStructNodes = 2_000_000

func isPDF20Type(typ string) bool {
	if _, ok := pdf20StructTypes[typ]; ok {
		return true
	}
	return headingRe.MatchString(typ)
}

// Profile2 implements the PDF/UA-2 (ISO 14289-2) standard profile.
type Profile2 struct {
	std  standardType
	opts Profile2Options
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

func forEachOBJR(elem *core.PdfObjectDictionary, visit func(core.PdfObject)) {
	check := func(kid core.PdfObject) {
		d, ok := core.GetDict(core.TraceToDirectObject(kid))
		if !ok {
			return
		}
		if typ, ok := core.GetName(d.Get("Type")); ok && string(*typ) == "OBJR" {
			if obj := d.Get("Obj"); obj != nil {
				visit(obj)
			}
		}
	}
	kids := core.TraceToDirectObject(elem.Get("K"))
	if arr, ok := core.GetArray(kids); ok {
		for _, kid := range arr.Elements() {
			check(kid)
		}
		return
	}
	check(kids)
}

var _ Profile = (*Profile2)(nil)

func findSuspectFonts(r *model.CompliancePdfReader, rules ruleNos) []suspectFont {
	var suspects []suspectFont
	for _, font := range collectFontDicts(r) {
		if violations := checkFont(font, rules); len(violations) != 0 {
			suspects = append(suspects, suspectFont{font: font, violations: violations})
		}
	}
	return suspects
}

func requireStructTree(doc *docutil.Document) error {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return errors.New("pdfua: document catalog not found")
	}
	if _, ok := catalog.GetStructTreeRoot(); !ok {
		return errNotTagged
	}
	return nil
}

func charProcStreams(font *core.PdfObjectDictionary) []*core.PdfObjectStream {
	procs, ok := core.GetDict(core.TraceToDirectObject(font.Get("CharProcs")))
	if !ok {
		return nil
	}
	var streams []*core.PdfObjectStream
	for _, key := range procs.Keys() {
		if stream, ok := core.GetStream(procs.Get(key)); ok {
			streams = append(streams, stream)
		}
	}
	return streams
}

const maxRoleMapHops = 32

var noViolation = ViolatedRule{}

const maxResourceDepth = 16

func (u *fontUsage) walkContent(data []byte, scope resourceScope, depth int) {
	ops, err := contentstream.NewContentStreamParser(string(data)).Parse()
	if err != nil {
		u.determined = false
		return
	}
	u.walkOps(ops, scope, depth)
}

func (g *tableGrid) at(r, c int) *core.PdfObjectDictionary {
	if r < 0 || c < 0 || r >= g.rows || c >= g.cols {
		return nil
	}
	return g.cells[r*g.cols+c]
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

func setDefaultLanguage(doc *docutil.Document, lang string) {
	if lang == "" {
		return
	}
	catalog, ok := doc.FindCatalog()
	if !ok {
		return
	}
	catDict := catalog.Object
	if catDict.Get("Lang") == nil {
		catDict.Set("Lang", core.MakeString(lang))
	}
}

func (w *contentWalker) usageDetermined() bool {
	return w.fonts != nil && w.fonts.determined && !w.truncated
}

func (w *contentWalker) walk(ops *contentstream.ContentStreamOperations, res *model.PdfPageResources, stack []mcFrame, depth int) {
	for _, op := range *ops {
		if w.done() {
			w.truncated = true
			return
		}
		switch op.Operand {
		case "BDC":
			frame := mcFrame{}
			if len(op.Params) > 0 {
				if name, ok := core.GetName(op.Params[0]); ok && string(*name) == "Artifact" {
					frame.artifact = true
				}
			}
			if !frame.artifact && len(op.Params) > 1 {
				frame.tagged = w.hasStructTree && hasMCID(op.Params[1], res)
			}
			stack = append(stack, frame)
		case "BMC":
			frame := mcFrame{}
			if len(op.Params) > 0 {
				if name, ok := core.GetName(op.Params[0]); ok && string(*name) == "Artifact" {
					frame.artifact = true
				}
			}
			stack = append(stack, frame)
		case "EMC":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case "Do":
			if res != nil && len(op.Params) > 0 {
				if name, ok := core.GetName(op.Params[0]); ok {
					if stream, xobjType := res.GetXObjectByName(*name); xobjType == model.XObjectTypeForm {
						w.descend(*name, stream, res, stack, depth)
						continue
					}
				}
			}
			w.evalContent(stack)
		default:
			if _, ok := paintingOperators[op.Operand]; ok {
				w.evalContent(stack)
			}
		}
	}
}

func headerIDs(elem, classMap *core.PdfObjectDictionary) []string {
	obj := lookupAttr(elem, classMap, "Headers", "Table", func(v core.PdfObject) bool { _, ok := core.GetArray(v); return ok })
	arr, ok := core.GetArray(obj)
	if !ok {
		return nil
	}
	var ids []string
	for _, item := range arr.Elements() {
		if str, ok := core.GetString(item); ok {
			ids = append(ids, str.Str())
		}
	}
	return ids
}

var _ Profile = (*Profile1)(nil)

func mapRole(roleMap *core.PdfObjectDictionary, typ string) (string, bool) {
	if roleMap == nil {
		return "", false
	}
	name, ok := core.GetName(roleMap.Get(core.PdfObjectName(typ)))
	if !ok {
		return "", false
	}
	return string(*name), true
}

func appearanceStreams(annot *core.PdfObjectDictionary) []*core.PdfObjectStream {
	ap, ok := core.GetDict(core.TraceToDirectObject(annot.Get("AP")))
	if !ok {
		return nil
	}
	var streams []*core.PdfObjectStream
	for _, key := range []core.PdfObjectName{"N", "D", "R"} {
		streams = append(streams, streamsFromObject(ap.Get(key))...)
	}
	return streams
}

func (u *fontUsage) walkCharProcs(font *core.PdfObjectDictionary, scope resourceScope, depth int) {
	if subtype, ok := core.GetName(font.Get("Subtype")); !ok || string(*subtype) != "Type3" {
		return
	}
	procs := charProcStreams(font)
	if len(procs) == 0 {
		return
	}
	key := walkKey{obj: font, pageRes: scope.page}
	if u.visited[key] {
		return
	}
	if !u.budget(depth) {
		return
	}
	u.visited[key] = true
	resDict, _ := core.GetDict(core.TraceToDirectObject(font.Get("Resources")))
	procScope := resourceScope{local: resourcesFromDict(resDict), page: scope.page}
	for _, proc := range procs {
		u.walkStream(proc, procScope, depth+1)
	}
}

type structureTypeFindings struct {
	circular         bool
	remappedStandard bool
	unknownNamespace bool
}

func findHeaderCells(grid *tableGrid, classMap *core.PdfObjectDictionary, resolve structTypeFunc, pl tableCellPlacement, budget *int) (found, ok bool) {
	scopedTH := func(r, c int, scope string) bool {
		cell := grid.at(r, c)
		if typeOrEmpty(cell, resolve) != "TH" {
			return false
		}
		sc := cellScope(cell, classMap)
		return sc == "Both" || sc == scope
	}
	if pl.row > 0 {
		for c := pl.col; c < pl.col+pl.colSpan; c++ {
			sawTH := false
			for r := pl.row - 1; r >= 0; r-- {
				if *budget--; *budget < 0 {
					return false, false
				}
				if scopedTH(r, c, "Column") {
					return true, true
				}
				if typeOrEmpty(grid.at(r, c), resolve) == "TH" {
					sawTH = true
				} else if sawTH {
					break
				}
			}
		}
	}
	if pl.col > 0 {
		for r := pl.row; r < pl.row+pl.rowSpan; r++ {
			sawTH := false
			for c := pl.col - 1; c >= 0; c-- {
				if *budget--; *budget < 0 {
					return false, false
				}
				if scopedTH(r, c, "Row") {
					return true, true
				}
				if typeOrEmpty(grid.at(r, c), resolve) == "TH" {
					sawTH = true
				} else if sawTH {
					break
				}
			}
		}
	}
	return false, true
}

func undefinedIDs(ids []string, defined map[string]struct{}) []string {
	var missing []string
	for _, id := range ids {
		if _, ok := defined[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing
}

// Profile is the model.StandardImplementer enhanced by PDF/UA part information.
type Profile interface {
	model.StandardImplementer
	Part() int
}
type suspectFont struct {
	font       *core.PdfObjectDictionary
	violations []ViolatedRule
}

func (s standardType) String() string {
	return fmt.Sprintf("PDF/UA-%d", s.part)
}

func streamData(stream *core.PdfObjectStream) []byte {
	if data, err := core.DecodeStream(stream); err == nil {
		return data
	}
	return stream.Stream
}

func annotPageMap(r *model.CompliancePdfReader) map[int64]*model.PdfPage {
	pages := map[int64]*model.PdfPage{}
	numPages, err := r.GetNumPages()
	if err != nil {
		return pages
	}
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
			if objNum, ok := objectNumber(annot.GetContainingPdfObject()); ok {
				pages[objNum] = page
			}
		}
	}
	return pages
}

func resolveStandardType(elem, roleMap *core.PdfObjectDictionary) string {
	name, ok := core.GetName(elem.Get("S"))
	if !ok {
		return ""
	}
	typ := string(*name)
	seen := map[string]bool{typ: true}
	next, ok := mapRole(roleMap, typ)
	if !ok || seen[next] {
		if _, ok := pdf17StructTypes[typ]; ok {
			return typ
		}
		return ""
	}
	for i := 0; i < maxRoleMapHops; i++ {
		if _, ok := pdf17StructTypes[next]; ok {
			return next
		}
		seen[next] = true
		if next, ok = mapRole(roleMap, next); !ok || seen[next] {
			return ""
		}
	}
	return ""
}

// Profile2Options are the options for the PDF/UA-2 profile.
type Profile2Options struct {
	// DefaultLanguage is set as the catalog Lang entry by ApplyStandard when
	// the document defines none. Ignored when empty.
	DefaultLanguage string

	// Xmp is the xmp options information.
	Xmp XmpOptions
}

var pdf17StructTypes = map[string]struct{}{"Document": {}, "Part": {}, "Art": {}, "Sect": {}, "Div": {}, "BlockQuote": {}, "Caption": {}, "TOC": {}, "TOCI": {}, "Index": {}, "NonStruct": {}, "Private": {}, "P": {}, "H": {}, "H1": {}, "H2": {}, "H3": {}, "H4": {}, "H5": {}, "H6": {}, "L": {}, "LI": {}, "Lbl": {}, "LBody": {}, "Table": {}, "TR": {}, "TH": {}, "TD": {}, "THead": {}, "TBody": {}, "TFoot": {}, "Span": {}, "Quote": {}, "Note": {}, "Reference": {}, "BibEntry": {}, "Code": {}, "Link": {}, "Annot": {}, "Ruby": {}, "RB": {}, "RT": {}, "RP": {}, "Warichu": {}, "WT": {}, "WP": {}, "Figure": {}, "Formula": {}, "Form": {}}

type standardType struct {
	part int
	rev  int
}

// Part gets the PDF/UA part.
func (p *Profile1) Part() int { return p.std.part }

func lookupAttr(elem, classMap *core.PdfObjectDictionary, key core.PdfObjectName, owner string, accept func(core.PdfObject) bool) core.PdfObject {
	if val := findAttr(elem.Get("A"), key, owner, accept); val != nil {
		return val
	}
	if classMap == nil {
		return nil
	}
	classes := elem.Get("C")
	if className, ok := core.GetName(classes); ok {
		return findAttr(classMap.Get(*className), key, owner, accept)
	}
	if arr, ok := core.GetArray(classes); ok {
		for _, item := range arr.Elements() {
			className, ok := core.GetName(item)
			if !ok {
				continue
			}
			if val := findAttr(classMap.Get(*className), key, owner, accept); val != nil {
				return val
			}
		}
	}
	return nil
}

func containsLinkOBJR(elem *core.PdfObjectDictionary) bool {
	isLinkOBJR := func(obj core.PdfObject) bool {
		d, ok := core.GetDict(core.TraceToDirectObject(obj))
		if !ok {
			return false
		}
		typ, ok := core.GetName(d.Get("Type"))
		if !ok || string(*typ) != "OBJR" {
			return false
		}
		target, ok := core.GetDict(core.TraceToDirectObject(d.Get("Obj")))
		if !ok {
			return false
		}
		subtype, ok := core.GetName(target.Get("Subtype"))
		return ok && string(*subtype) == "Link"
	}
	kids := core.TraceToDirectObject(elem.Get("K"))
	if arr, ok := core.GetArray(kids); ok {
		return slices.ContainsFunc(arr.Elements(), isLinkOBJR)
	}
	return isLinkOBJR(kids)
}

func propertyDict(obj core.PdfObject, res *model.PdfPageResources) *core.PdfObjectDictionary {
	obj = core.TraceToDirectObject(obj)
	if d, ok := obj.(*core.PdfObjectDictionary); ok {
		return d
	}
	if name, ok := obj.(*core.PdfObjectName); ok && res != nil {
		propsObj := core.TraceToDirectObject(res.Properties)
		if props, ok := propsObj.(*core.PdfObjectDictionary); ok {
			propObj := core.TraceToDirectObject(props.Get(*name))
			if d, ok := propObj.(*core.PdfObjectDictionary); ok {
				return d
			}
		}
	}
	return nil
}

func widgetHiddenOrNoView(w *model.PdfAnnotationWidget) bool {
	d, ok := core.GetDict(w.GetContainingPdfObject())
	if !ok {
		return false
	}
	if flags, ok := core.GetInt(d.Get("F")); ok {
		return int64(*flags)&(annotFlagHidden|annotFlagNoView) != 0
	}
	return false
}

// Profile1 is the implementation of the PDF/UA-1 (ISO 14289-1) standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type Profile1 struct {
	std  standardType
	opts Profile1Options
}

func structChildren(kids core.PdfObject, resolve structTypeFunc) []*core.PdfObjectDictionary {
	var result []*core.PdfObjectDictionary
	active := map[*core.PdfObjectDictionary]struct{}{}
	count := 0
	var walkKids func(obj core.PdfObject, depth int)
	visitKid := func(obj core.PdfObject, depth int) {
		count++
		if count > maxKidVisits {
			return
		}
		d, ok := core.GetDict(core.TraceToDirectObject(obj))
		if !ok {
			return
		}
		if typ, ok := core.GetName(d.Get("Type")); ok && string(*typ) == "OBJR" {
			return
		}
		if d.Get("S") == nil {
			return
		}
		if _, ok := groupingTypes[resolve(d)]; !ok {
			result = append(result, d)
			return
		}
		if _, ok := active[d]; ok {
			return
		}
		active[d] = struct{}{}
		walkKids(d.Get("K"), depth+1)
		delete(active, d)
	}
	walkKids = func(obj core.PdfObject, depth int) {
		if obj == nil || depth > maxTreeDepth {
			return
		}
		obj = core.TraceToDirectObject(obj)
		if arr, ok := core.GetArray(obj); ok {
			for _, item := range arr.Elements() {
				if count > maxKidVisits {
					return
				}
				visitKid(item, depth)
			}
			return
		}
		visitKid(obj, depth)
	}
	walkKids(kids, 0)
	return result
}

func (s resourceScope) with(res *model.PdfPageResources) resourceScope {
	if res == nil {
		return s
	}
	return resourceScope{local: res, page: s.page}
}

func checkXFA(r *model.CompliancePdfReader) []ViolatedRule {
	if r.AcroForm == nil || r.AcroForm.XFA == nil {
		return nil
	}
	return []ViolatedRule{newViolation("8.10.1-3", "XFA forms shall not be present")}
}
