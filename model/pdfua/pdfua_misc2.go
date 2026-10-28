package pdfua

import (
	"errors"
	"regexp"
	"slices"

	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
)

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

func (w *contentWalker) done() bool { return w.taggingDone() && w.fonts == nil }

func (w *contentWalker) markUndetermined() {
	if w.fonts != nil {
		w.fonts.determined = false
	}
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

var errNotTagged = errors.New("pdfua: document is not a tagged PDF (no StructTreeRoot); PDF/UA structure cannot be generated automatically")

var headingRe = regexp.MustCompile("^H([1-9][0-9]*)$")

// DefaultProfile1Options are the default options for the Profile1.
func DefaultProfile1Options() *Profile1Options {
	return &Profile1Options{Xmp: XmpOptions{MarshalIndent: "\t"}}
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

func isInt(obj core.PdfObject) bool {
	_, ok := core.GetIntVal(obj)
	return ok
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

func (w *contentWalker) taggingDone() bool {
	return w.emitted[w.rules.unmarkedContent] && w.emitted[w.rules.taggedArtifact]
}

func (w *contentWalker) usedFonts() map[*core.PdfObjectDictionary]bool {
	if w.fonts == nil {
		return nil
	}
	return w.fonts.used
}

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

// DefaultProfile2Options are the default options for Profile2.
func DefaultProfile2Options() *Profile2Options {
	return &Profile2Options{Xmp: XmpOptions{MarshalIndent: "\t"}}
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

func findSuspectFonts(r *model.CompliancePdfReader, rules ruleNos) []suspectFont {
	var suspects []suspectFont
	for _, font := range collectFontDicts(r) {
		if violations := checkFont(font, rules); len(violations) != 0 {
			suspects = append(suspects, suspectFont{font: font, violations: violations})
		}
	}
	return suspects
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
