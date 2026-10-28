package pdfa

import (
	"fmt"
	"sort"
	"time"

	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
)

// NewProfile4 creates a new Profile4 with given options.
func NewProfile4(options *Profile4Options) *Profile4 {
	if options == nil {
		options = DefaultProfile4Options()
	}
	setProfile4OptionDefaults(options)
	return &Profile4{profile4{options: *options, standard: standard4()}}
}

// StandardName gets the name of the standard.
func (p *profile4) StandardName() string { return fmt.Sprintf("PDF/A-4%s", p.standard.conformance) }

// ApplyStandard tries to change the content of the writer to match the PDF/A-4 standard.
// Implements model.StandardApplier.
func (p *profile4) ApplyStandard(document *docutil.Document) (err error) {
	ensureMinimumPDF2Version(document, 0)
	if err = setLastModified(document, p.options.Now()); err != nil {
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
	if err = fixActionsPDFA4(document, p.standard); err != nil {
		return err
	}
	if err = filterCatalogAdditionalActions(document); err != nil {
		return err
	}
	if err := updateXMPMetadataPDFA4(document, p.standard, p.options.Xmp); err != nil {
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

// Part gets the PDF/A version level.
func (p *profile4) Part() int { return p.standard.part }

func setProfile4OptionDefaults(profile4Options *Profile4Options) {
	if profile4Options.Now == nil {
		profile4Options.Now = time.Now
	}
}

// ValidateStandard checks if provided input CompliancePdfReader matches rules that conforms PDF/A-4 standard.
func (p *profile4) ValidateStandard(r *model.CompliancePdfReader) error {
	verificationError := VerificationError{ConformanceLevel: p.standard.part, ConformanceVariant: p.standard.conformance}
	if rule := verifyHeaderPDFA4(r); rule != noViolation {
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
	if rules := verifyHexStrings(r, "6.1.5"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyStreamsPDFA4(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyPermissions(r, "6.1.11"); len(rules) != 0 {
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
	if rules := verifyXObjectsPDFA4(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyFormXObjectOPIPDFA4(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyFontDictionaries(r, "6.2.10.2"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyFonts(r, p.standard, "6.2.10"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyAnnotationsPDFA4(r, p.standard); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyInteractiveFormsPDFA4(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyActionsPDFA4(r, p.standard); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyCatalogAdditionalActionsPDFA4(r); rule != noViolation {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rule)
	}
	if rules := verifyMetadataPDFA4(r, p.standard, false); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyEmbeddedFilesPDFA4(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyOptionalContentPDFA4(r); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rules := verifyAlternatePresentations(r, "6.11"); len(rules) != 0 {
		verificationError.ViolatedRules = append(verificationError.ViolatedRules, rules...)
	}
	if rule := verifyRequirements(r, "6.12"); rule != noViolation {
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

// Conformance gets the PDF/A conformance.
func (p *profile4) Conformance() string { return p.standard.conformance }

// Profile4Options are the options that changes the way how optimizer may try to adapt document into PDF/A standard.
type Profile4Options struct {
	// CMYKDefaultColorSpace is an option that refers PDF/A
	CMYKDefaultColorSpace bool

	// Now is a function that returns current time.
	Now func() time.Time

	// Xmp is the xmp options information.
	Xmp XmpOptions
}

type profile4 struct {
	standard standardType
	options  Profile4Options
}

// Profile4 is the implementation of the PDF/A-4 standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type Profile4 struct{ profile4 }

// Profile4F is the implementation of the PDF/A-4F standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type Profile4F struct{ profile4 }

// NewProfile4E creates a new Profile4E with the given options.
func NewProfile4E(options *Profile4Options) *Profile4E {
	if options == nil {
		options = DefaultProfile4Options()
	}
	setProfile4OptionDefaults(options)
	return &Profile4E{profile4{options: *options, standard: standard4E()}}
}

// Profile4E is the implementation of the PDF/A-4E standard profile.
// Implements model.StandardImplementer, Profile interfaces.
type (
	Profile4E   struct{ profile4 }
	xmpBaseType uint8
)

// NewProfile4F creates a new Profile4F with the given options.
func NewProfile4F(options *Profile4Options) *Profile4F {
	if options == nil {
		options = DefaultProfile4Options()
	}
	setProfile4OptionDefaults(options)
	return &Profile4F{profile4{options: *options, standard: standard4F()}}
}
