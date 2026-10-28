package pdfa

import (
	"fmt"
	"sort"
	"time"

	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
)

// Conformance gets the PDF/A conformance.
func (p *profile2) Conformance() string { return p.standard.conformance }

// StandardName gets the name of the standard.
func (p *profile2) StandardName() string { return fmt.Sprintf("PDF/A-2%s", p.standard.conformance) }

// NewProfile2B creates a new Profile2B with the given options.
func NewProfile2B(options *Profile2Options) *Profile2B {
	if options == nil {
		options = DefaultProfile2Options()
	}
	setProfile2OptionDefaults(options)
	return &Profile2B{profile2{options: *options, standard: standard2B()}}
}

// Part gets the PDF/A version level.
func (p *profile2) Part() int { return p.standard.part }

// Profile2A is the implementation of the PDF/A-2A standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type Profile2A struct{ profile2 }

// Profile2U is the implementation of the PDF/A-2U standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type Profile2U struct{ profile2 }

// ValidateStandard checks if provided input CompliancePdfReader matches rules that conforms PDF/A-2 standard.
func (p *profile2) ValidateStandard(r *model.CompliancePdfReader) error {
	verificationError := VerificationError{ConformanceLevel: p.standard.part, ConformanceVariant: p.standard.conformance}
	if rule := verifyHeaderPDFA23(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyHeaderBinaryComment(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyTrailer(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyNoDataAfterEOF(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyXrefKeywordEOL(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyHexStrings(r, "6.1.6"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyStreamsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := stubClause618PDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyLinearizedPDFA23(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyPermissions(r, "6.1.12"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyImplementationLimitsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyContentStreamOperators(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyColorspaces(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyExtGState(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := stubClause6xxPDFA23b(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyXObjectsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := stubClause6xxPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyFormXObjectsPDFA23(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyFontDictionaries(r, "6.2.11.2"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyFonts(r, p.standard, "6.2.11"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyAnnotationsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyInteractiveFormsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := stubClause6xxFormsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyFieldActionsPDFA23(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyActionsPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyCatalogAdditionalActionsPDFA23(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyMetadataPDFA23(r, p.standard, false); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if p.standard == standard2A() {
		if rules := verifyLogicalStructurePDFA23(r); len(rules) != 0 {
			verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
		}
	}
	if rules := verifyEmbeddedFilesPDFA2(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyOptionalContentPDFA23(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyAlternatePresentations(r, "6.10"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyRequirements(r, "6.11"); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if len(verificationError.ViolatedRules) > 0 {
		sort.Slice(verificationError.ViolatedRules, func(i, j int) bool {
			return verificationError.ViolatedRules[i].RuleNo < verificationError.ViolatedRules[j].RuleNo
		})
		return verificationError
	}
	return nil
}

// NewProfile2U creates a new Profile2U with the given options.
func NewProfile2U(options *Profile2Options) *Profile2U {
	if options == nil {
		options = DefaultProfile2Options()
	}
	setProfile2OptionDefaults(options)
	return &Profile2U{profile2{options: *options, standard: standard2U()}}
}

// ApplyStandard tries to change the content of the writer to match the PDF/A-2 standard.
// Implements model.StandardApplier.
func (p *profile2) ApplyStandard(document *docutil.Document) (err error) {
	ensureMinimumVersion(document, 7)
	if err = updateInfoDates(document, p.options.Now); err != nil {
		return err
	}
	if err = setCatalogVersion(document); err != nil {
		return err
	}
	optimizePage, optimizeDocument := newColorspaceOptimizers(p.options.CMYKDefaultColorSpace, p.standard)
	err = optimizeColorspaces(document, []pageColorspaceOptimizeFunc{optimizePage}, []documentColorspaceOptimizeFunc{optimizeDocument})
	if err != nil {
		return err
	}
	ensureDocumentID(document)
	if err = fixPermissions(document); err != nil {
		return err
	}
	if err = fixXObjects(document, p.standard.part); err != nil {
		return err
	}
	if err = fixAnnotationsPDFA23(document); err != nil {
		return err
	}
	if err = fixExtGStatePDFA23(document); err != nil {
		return err
	}
	if err = embedFonts(document); err != nil {
		return err
	}
	if err = fixInteractiveForm(document); err != nil {
		return err
	}
	if p.standard.conformance == "A" {
		setMarkInfoMarked(document)
	}
	if err = fixActions(document, p.standard.part); err != nil {
		return err
	}
	if err = removeCatalogAdditionalActions(document); err != nil {
		return err
	}
	if err := updateXMPMetadata(document, p.standard, p.options.Xmp); err != nil {
		return err
	}
	if p.standard == standard2A() {
		if err = ensureStructTreeRoot(document); err != nil {
			return err
		}
	}
	if err = fixOptionalContent(document); err != nil {
		return err
	}
	if err = removeAlternatePresentations(document); err != nil {
		return err
	}
	if err = removeRequirements(document); err != nil {
		return err
	}
	return nil
}

// Profile2Options are the options that changes the way how optimizer may try to adapt document into PDF/A standard.
type Profile2Options struct {
	// CMYKDefaultColorSpace is an option that refers PDF/A
	CMYKDefaultColorSpace bool

	// Now is a function that returns current time.
	Now func() time.Time

	// Xmp is the xmp options information.
	Xmp XmpOptions
}

func setProfile2OptionDefaults(profile2Options *Profile2Options) {
	if profile2Options.Now == nil {
		profile2Options.Now = time.Now
	}
}

type profile2 struct {
	standard standardType
	options  Profile2Options
}

// NewProfile2A creates a new Profile2A with given options.
func NewProfile2A(options *Profile2Options) *Profile2A {
	if options == nil {
		options = DefaultProfile2Options()
	}
	setProfile2OptionDefaults(options)
	return &Profile2A{profile2{options: *options, standard: standard2A()}}
}

// Profile2B is the implementation of the PDF/A-2B standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type Profile2B struct{ profile2 }
