package pdfua

import (
	"fmt"
	"strings"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
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

// String gets a string representation of the violated rule.
func (v ViolatedRule) String() string { return fmt.Sprintf("%s: %s", v.RuleNo, v.Detail) }

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

func summarizeIDs(names []string) string {
	if len(names) <= maxListedIDs {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(names[:maxListedIDs], ", "), len(names)-maxListedIDs)
}

func scanContent(r *model.CompliancePdfReader, rules ruleNos) contentScan {
	suspects := findSuspectFonts(r, rules)
	w := newContentWalker(r, rules, len(suspects) != 0)
	w.run()
	return contentScan{contentViolations: w.violations, fontViolations: selectSuspectViolations(suspects, w.usedFonts(), w.usageDetermined())}
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

func ua2Standard() standardType { return standardType{part: 2, rev: 2024} }

const structRootRuleTable5 = "Table 5. StructTreeRoot-Document-1"

// VerificationError is the PDF/UA verification error containing all violated rules.
type VerificationError struct {
	// ViolatedRules are the rules that were violated during verification.
	ViolatedRules []ViolatedRule

	// Part defines the PDF/UA part (1 or 2) on which verification failed.
	Part int
}

var ua2Rules = ruleNos{metadataStream: "8.11.1-2", xmpPartMissing: "5-1", xmpPartMismatch: "5-2", xmpRevMismatch: "5-5", structTreeRoot: "8.2.1-1", markInfoMarked: "6.2-1", markInfoSuspects: "", docTitle: "8.11.1-1", displayDocTitle: "8.11.2-1", langMissing: "8.4.4-1", langEmpty: "8.4.4-2", figureAlt: "8.2.5.28.2-1", unmarkedContent: "8.2.2-1", taggedArtifact: "8.2.2-1", fontEmbedded: "8.4.5.5.1-1", fontToUnicode: "8.4.5.8-1", fontCIDToGIDMap: "8.4.5.3.2-1", xmpTitleOnly: true}

var ua1Rules = ruleNos{metadataStream: "7.1-8", xmpPartMissing: "5-1", xmpPartMismatch: "5-2", structTreeRoot: "7.1-11", markInfoMarked: "6.2-1", markInfoSuspects: "7.1-4", docTitle: "7.1-9", displayDocTitle: "7.1-10", langMissing: "", langEmpty: "7.2-29", figureAlt: "7.3-1", unmarkedContent: "7.1-3", taggedArtifact: "7.1-2", fontEmbedded: "7.21.4.1-1", fontToUnicode: "7.21.7-1", fontCIDToGIDMap: "7.21.3.2-1"}

func (s resourceScope) lookIn() [2]*model.PdfPageResources {
	return [2]*model.PdfPageResources{s.local, s.page}
}

type tableGrid struct {
	rows, cols int
	cells      []*core.PdfObjectDictionary
	placements []tableCellPlacement
}

func resourcesFromDict(dict *core.PdfObjectDictionary) *model.PdfPageResources {
	if dict == nil {
		return nil
	}
	res, _ := model.NewPdfPageResourcesFromDict(dict)
	return res
}

type structTypeFunc func(elem *core.PdfObjectDictionary) string

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

// ViolatedRule defines a violated PDF/UA rule. RuleNo follows the clause
// numbering of ISO 14289 (e.g. "7.1-3").
type ViolatedRule struct {
	RuleNo string
	Detail string
}

func (g *tableGrid) at(r, c int) *core.PdfObjectDictionary {
	if r < 0 || c < 0 || r >= g.rows || c >= g.cols {
		return nil
	}
	return g.cells[r*g.cols+c]
}

type structureTypeFindings struct {
	circular         bool
	remappedStandard bool
	unknownNamespace bool
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

type standardType struct {
	part int
	rev  int
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
