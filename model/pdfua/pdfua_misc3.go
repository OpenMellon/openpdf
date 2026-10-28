package pdfua

import (
	"errors"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
)

func newNamespaceResolver(root *core.PdfObjectDictionary) structTypeFunc {
	roleMap, _ := core.GetDict(core.TraceToDirectObject(root.Get("RoleMap")))
	return func(elem *core.PdfObjectDictionary) string {
		typ, _ := resolveNamespacedType(elem, roleMap)
		return typ
	}
}

// StandardName gets the name of the standard.
func (p *Profile1) StandardName() string { return p.std.String() }

var pdf20StructTypes = map[string]struct{}{"Document": {}, "DocumentFragment": {}, "Part": {}, "Sect": {}, "Div": {}, "Aside": {}, "Title": {}, "NonStruct": {}, "H": {}, "P": {}, "H1": {}, "H2": {}, "H3": {}, "H4": {}, "H5": {}, "H6": {}, "Lbl": {}, "L": {}, "LI": {}, "LBody": {}, "Table": {}, "TR": {}, "TH": {}, "TD": {}, "THead": {}, "TBody": {}, "TFoot": {}, "Span": {}, "FENote": {}, "Link": {}, "Annot": {}, "Figure": {}, "Formula": {}, "Form": {}, "Artifact": {}, "Caption": {}, "Sub": {}, "Em": {}, "Strong": {}, "Ruby": {}, "RB": {}, "RT": {}, "RP": {}, "Warichu": {}, "WT": {}, "WP": {}}

// Part gets the PDF/UA part.
func (p *Profile2) Part() int { return p.std.part }

// NewProfile2 creates a new Profile2 with the given options.
func NewProfile2(options *Profile2Options) *Profile2 {
	if options == nil {
		options = DefaultProfile2Options()
	}
	applyProfile2Defaults(options)
	return &Profile2{std: ua2Standard(), opts: *options}
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

const (
	maxListedIDs = 10
	maxKidVisits = 100_000
)

func isReportableAnnot(annot *core.PdfObjectDictionary) bool {
	if subtype, ok := core.GetName(annot.Get("Subtype")); ok && string(*subtype) == "Popup" {
		return false
	}
	if flags, ok := core.GetIntVal(annot.Get("F")); ok && flags&(annotFlagHidden|annotFlagNoView) != 0 {
		return false
	}
	return true
}

// StandardName gets the name of the standard.
func (p *Profile2) StandardName() string { return p.std.String() }

func setPDF20Version(doc *docutil.Document) {
	doc.Version.Major, doc.Version.Minor = 2, 0
	catalog, ok := doc.FindCatalog()
	if !ok {
		return
	}
	catalog.SetVersion()
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

const maxSpan = 1024

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

// NewProfile1 creates a new Profile1 with given options.
func NewProfile1(options *Profile1Options) *Profile1 {
	if options == nil {
		options = DefaultProfile1Options()
	}
	applyProfile1Defaults(options)
	return &Profile1{std: ua1Standard(), opts: *options}
}

type mcFrame struct {
	tagged   bool
	artifact bool
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

const maxTreeDepth = 1000

const maxGridCells = 1 << 20

// Profile1Options are the options for the PDF/UA-1 profile.
type Profile1Options struct {
	// DefaultLanguage is set as the catalog Lang entry by ApplyStandard when
	// the document defines none. Ignored when empty.
	DefaultLanguage string

	// Xmp is the xmp options information.
	Xmp XmpOptions
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

var _ Profile = (*Profile2)(nil)

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

const maxRoleMapHops = 32

var noViolation = ViolatedRule{}

const maxResourceDepth = 16

var _ Profile = (*Profile1)(nil)

// Profile is the model.StandardImplementer enhanced by PDF/UA part information.
type Profile interface {
	model.StandardImplementer
	Part() int
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

// Part gets the PDF/UA part.
func (p *Profile1) Part() int { return p.std.part }

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

// Profile1 is the implementation of the PDF/UA-1 (ISO 14289-1) standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type Profile1 struct {
	std  standardType
	opts Profile1Options
}
