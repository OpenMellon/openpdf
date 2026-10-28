package pdfa

import (
	"fmt"
	"sort"
	"time"

	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
)

// ValidateStandard checks if provided input CompliancePdfReader matches rules that conforms PDF/A-1 standard.
func (p *profile1) ValidateStandard(r *model.CompliancePdfReader) error {
	verificationError := VerificationError{ConformanceLevel: p.standard.part, ConformanceVariant: p.standard.conformance}
	if rule := verifyHeaderPDFA1(r); rule != noViolation {
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
	if rule := verifyLinearizedPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyXrefSubsectionsPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := stubDocumentInfoPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyHexStrings(r, "6.1.6"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyStreamsPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := stubIndirectObjectsPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyFiltersPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyEmbeddedFilesPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyImplementationLimitsPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyOptionalContentPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyColorspacesPDFA1(r, false); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyXObjectsPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyFormXObjectsPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyReferenceXObjectsPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyPostScriptXObjectsPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := stubClause628PDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyContentStreamOperatorsPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyFontDictionariesPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyFontsPDFA1(r, p.standard); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyExtGStatePDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyXObjectSMaskPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyTransparencyGroupPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyAnnotationsPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyActionsPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyFieldActionsPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rule := verifyCatalogAdditionalActionsPDFA1(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyMetadataPDFA1(r, p.standard, false); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if p.standard == standard1A() {
		if rules := verifyLogicalStructurePDFA1A(r); len(rules) != 0 {
			verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
		}
	}
	if rules := verifyInteractiveFormsPDFA1(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if len(verificationError.ViolatedRules) > 0 {
		sort.Slice(verificationError.ViolatedRules, func(i, j int) bool {
			return verificationError.ViolatedRules[i].RuleNo < verificationError.ViolatedRules[j].RuleNo
		})
		return verificationError
	}
	return nil
}

// StandardName gets the name of the standard.
func (p *profile1) StandardName() string { return fmt.Sprintf("PDF/A-1%s", p.standard.conformance) }

// Part gets the PDF/A version level.
func (p *profile1) Part() int { return p.standard.part }

type profile1 struct {
	standard standardType
	options  Profile1Options
}

// Conformance gets the PDF/A conformance.
func (p *profile1) Conformance() string { return p.standard.conformance }

// Profile1B is the implementation of the PDF/A-1B standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type Profile1B struct{ profile1 }

// Profile1Options are the options that changes the way how optimizer may try to adapt document into PDF/A standard.
type Profile1Options struct {
	// CMYKDefaultColorSpace is an option that refers PDF/A-1
	CMYKDefaultColorSpace bool

	// Now is a function that returns current time.
	Now func() time.Time

	// Xmp is the xmp options information.
	Xmp XmpOptions
}

func setProfile1OptionDefaults(profile1Options *Profile1Options) {
	if profile1Options.Now == nil {
		profile1Options.Now = time.Now
	}
}

// Profile1A is the implementation of the PDF/A-1A standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type (
	Profile1A              struct{ profile1 }
	colorspaceModification struct {
		converter imageutil.ColorConverter
		target    model.PdfColorspace
	}
)

// NewProfile1A creates a new Profile1A with given options.
func NewProfile1A(options *Profile1Options) *Profile1A {
	if options == nil {
		options = DefaultProfile1Options()
	}
	setProfile1OptionDefaults(options)
	return &Profile1A{profile1{options: *options, standard: standard1A()}}
}

// NewProfile1B creates a new Profile1B with the given options.
func NewProfile1B(options *Profile1Options) *Profile1B {
	if options == nil {
		options = DefaultProfile1Options()
	}
	setProfile1OptionDefaults(options)
	return &Profile1B{profile1{options: *options, standard: standard1B()}}
}

// ApplyStandard tries to change the content of the writer to match the PDF/A-1 standard.
// Implements model.StandardApplier.
func (p *profile1) ApplyStandard(document *docutil.Document) (err error) {
	ensureMinimumVersion(document, 4)
	if err = updateInfoDates(document, p.options.Now); err != nil {
		return err
	}
	if err = setCatalogVersion(document); err != nil {
		return err
	}
	optimizePage, optimizeDocument := newColorspaceOptimizers(p.options.CMYKDefaultColorSpace, p.standard)
	err = optimizeColorspaces(document, []pageColorspaceOptimizeFunc{removeImageSoftMasks, optimizePage}, []documentColorspaceOptimizeFunc{optimizeDocument})
	if err != nil {
		return err
	}
	ensureDocumentID(document)
	if err = fixXObjects(document, p.standard.part); err != nil {
		return err
	}
	if err = fixTransparencyGroups(document); err != nil {
		return err
	}
	if err = fixAnnotationsPDFA1(document); err != nil {
		return err
	}
	if err = fixExtGStatePDFA1(document); err != nil {
		return err
	}
	if err = embedFonts(document); err != nil {
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
	if p.standard == standard1A() {
		if err = ensureStructTreeRoot(document); err != nil {
			return err
		}
	}
	if err = fixInteractiveFormPDFA1(document); err != nil {
		return err
	}
	return nil
}
