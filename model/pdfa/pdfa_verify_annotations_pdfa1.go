package pdfa

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func verifyAnnotationsPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_5_2_1, reported6_5_3_1, reported6_5_3_2, reported6_5_3_4, reported6_5_3_3, reported6_6_1_3, reported6_6_2_1 bool
	allReported := func() bool {
		return reported6_5_2_1 && reported6_5_3_1 && reported6_5_3_2 && reported6_5_3_4 && reported6_5_3_3 && reported6_6_1_3 && reported6_6_2_1
	}
	for _, page := range r.PageList {
		annotations, err := page.GetAnnotations()
		if err != nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("loading annotations failed: %v", err)
			}
			continue
		}
		for _, annotation := range annotations {
			if !reported6_5_2_1 {
				switch annotation.GetContext().(type) {
				case *model.PdfAnnotationFileAttachment, *model.PdfAnnotationSound, *model.PdfAnnotationMovie, nil:
					violations = append(violations, newViolatedRule("6.5.2-1", "Annotation types not defined in PDF Reference shall not be permitted. Additionally, the FileAttachment, Sound and Movie types shall not be permitted."))
					reported6_5_2_1 = true
					if allReported() {
						return violations
					}
				}
			}
			dict, ok := core.GetDict(annotation.GetContainingPdfObject())
			if !ok {
				continue
			}
			if !reported6_5_3_1 {
				ca, ok := core.GetFloatVal(dict.Get("CA"))
				if ok && ca != 1.0 {
					violations = append(violations, newViolatedRule("6.5.3-1", "An annotation dictionary shall not contain the CA key with a value other than 1.0."))
					reported6_5_3_1 = true
					if allReported() {
						return violations
					}
				}
			}
			if !reported6_5_3_2 {
				f, ok := core.GetIntVal(dict.Get("F"))
				if !(ok && f&4 == 4 && f&1 == 0 && f&2 == 0 && f&32 == 0) {
					violations = append(violations, newViolatedRule("6.5.3-2", "An annotation dictionary shall contain the F key. The F key's Print flag bit shall be set to 1 and its Hidden, Invisible and NoView flag bits shall be set to 0."))
					reported6_5_3_2 = true
					if allReported() {
						return violations
					}
				}
			}
			if !reported6_5_3_4 {
				apDict, ok := core.GetDict(dict.Get("AP"))
				if ok {
					n := apDict.Get("N")
					if n == nil || len(apDict.Keys()) > 1 {
						violations = append(violations, newViolatedRule("6.5.3-4", "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key. If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary; otherwise the value of the N key shall be an appearance stream."))
						reported6_5_3_4 = true
						if allReported() {
							return violations
						}
						continue
					}
					_, ok := annotation.GetContext().(*model.PdfAnnotationWidget)
					if ok {
						ftName, ok := core.GetName(dict.Get("FT"))
						if ok && *ftName == "Btn" {
							if _, ok := core.GetDict(n); !ok {
								violations = append(violations, newViolatedRule("6.5.3-4", "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key. If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary; otherwise the value of the N key shall be an appearance stream."))
								reported6_5_3_4 = true
								if allReported() {
									return violations
								}
								continue
							}
						}
					}
					_, ok2 := core.GetStream(n)
					if !ok2 {
						violations = append(violations, newViolatedRule("6.5.3-4", "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key. If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary; otherwise the value of the N key shall be an appearance stream."))
						reported6_5_3_4 = true
						if allReported() {
							return violations
						}
						continue
					}
				}
			}
			if !reported6_5_3_3 {
				if dict.Get("C") != nil || dict.Get("IC") != nil {
					dict, ok := firstOutputIntentDictFromReader(r)
					if !ok {
						violations = append(violations, newViolatedRule("6.5.3-3", "An annotation dictionary shall not contain the C array or the IC array unless the color space of the DestOutputProfile in the PDF/A-1 OutputIntent dictionary, defined in 6.2.2, is RGB."))
						reported6_5_3_3 = true
						if allReported() {
							return violations
						}
					} else {
						n, ok := core.GetIntVal(dict.Get("N"))
						if !ok || n != 3 {
							violations = append(violations, newViolatedRule("6.5.3-3", "An annotation dictionary shall not contain the C array or the IC array unless the color space of the DestOutputProfile in the PDF/A-1 OutputIntent dictionary, defined in 6.2.2, is RGB."))
							reported6_5_3_3 = true
							if allReported() {
								return violations
							}
						}
					}
				}
			}
			pdfAnnotationWidget, found := annotation.GetContext().(*model.PdfAnnotationWidget)
			if !found {
				continue
			}
			if !reported6_6_1_3 {
				if pdfAnnotationWidget.A != nil {
					violations = append(violations, newViolatedRule("6.6.1-3", "A Widget annotation dictionary shall not include an A entry."))
					reported6_6_1_3 = true
					if allReported() {
						return violations
					}
				}
			}
			if !reported6_6_2_1 {
				if pdfAnnotationWidget.AA != nil {
					violations = append(violations, newViolatedRule("6.6.2-1", "A Widget annotation dictionary shall not include an AA entry for an additional-actions dictionary."))
					reported6_6_2_1 = true
					if allReported() {
						return violations
					}
				}
			}
		}
	}
	return violations
}
