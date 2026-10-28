package pdfa

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func verifyAnnotationsPDFA23(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_3_1_1, reported6_3_2_1, reported6_3_2_2, reported6_3_2_3, reported6_3_3_2, reported6_4_1_1, reported6_4_1_1_2 bool
	allReported := func() bool {
		return reported6_3_1_1 && reported6_3_2_1 && reported6_3_2_2 && reported6_3_2_3 && reported6_3_3_2 && reported6_4_1_1 && reported6_4_1_1_2
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
			if !reported6_3_1_1 {
				switch annotation.GetContext().(type) {
				case *model.PdfAnnotationScreen, *model.PdfAnnotation3D, *model.PdfAnnotationSound, *model.PdfAnnotationMovie, nil:
					violations = append(violations, newViolatedRule("6.3.1-1", "Annotation types not defined in PDF Reference shall not be permitted. Additionally, the 3D, Sound, Screen and Movie types shall not be permitted."))
					reported6_3_1_1 = true
					if allReported() {
						return violations
					}
				}
			}
			dict, ok := core.GetDict(annotation.GetContainingPdfObject())
			if !ok {
				continue
			}
			_, found := annotation.GetContext().(*model.PdfAnnotationPopup)
			if !found && !reported6_3_2_1 {
				_, ok := core.GetIntVal(dict.Get("F"))
				if !ok {
					violations = append(violations, newViolatedRule("6.3.2-1", "Except for annotation dictionaries whose Subtype value is Popup, all annotation dictionaries shall contain the F key."))
					reported6_3_2_1 = true
					if allReported() {
						return violations
					}
				}
			}
			if !reported6_3_2_2 {
				f, ok := core.GetIntVal(dict.Get("F"))
				if ok && !(f&4 == 4 && f&1 == 0 && f&2 == 0 && f&32 == 0 && f&256 == 0) {
					violations = append(violations, newViolatedRule("6.3.2-2", "If present, the F key's Print flag bit shall be set to 1 and its Hidden, Invisible, ToggleNoView, and NoView flag bits shall be set to 0."))
					reported6_3_2_2 = true
					if allReported() {
						return violations
					}
				}
			}
			_, ok2 := annotation.GetContext().(*model.PdfAnnotationText)
			if ok2 && !reported6_3_2_3 {
				f, ok := core.GetIntVal(dict.Get("F"))
				if ok && !(f&8 == 8 && f&16 == 16) {
					violations = append(violations, newViolatedRule("6.3.2-3", "Text annotation has one of the flags NoZoom or NoRotate set to 0."))
					reported6_3_2_3 = true
					if allReported() {
						return violations
					}
				}
			}
			if !reported6_3_3_2 {
				apDict, ok := core.GetDict(dict.Get("AP"))
				if ok {
					n := apDict.Get("N")
					if n == nil || len(apDict.Keys()) > 1 {
						violations = append(violations, newViolatedRule("6.3.3-2", "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key. If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary; otherwise the value of the N key shall be an appearance stream."))
						reported6_3_3_2 = true
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
								violations = append(violations, newViolatedRule("6.3.3-2", "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key. If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary; otherwise the value of the N key shall be an appearance stream."))
								reported6_3_3_2 = true
								if allReported() {
									return violations
								}
								continue
							}
						}
					}
					_, ok2 := core.GetStream(n)
					if !ok2 {
						violations = append(violations, newViolatedRule("6.3.3-2", "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key. If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary; otherwise the value of the N key shall be an appearance stream."))
						reported6_3_3_2 = true
						if allReported() {
							return violations
						}
						continue
					}
				}
			}
			pdfAnnotationWidget, ok3 := annotation.GetContext().(*model.PdfAnnotationWidget)
			if !ok3 {
				continue
			}
			if !reported6_4_1_1 {
				if pdfAnnotationWidget.A != nil {
					violations = append(violations, newViolatedRule("6.4.1-1", "A Widget annotation dictionary shall not include an A entry."))
					reported6_4_1_1 = true
					if allReported() {
						return violations
					}
				}
			}
			if !reported6_4_1_1_2 {
				if pdfAnnotationWidget.AA != nil {
					violations = append(violations, newViolatedRule("6.4.1-1", "A Widget annotation dictionary shall not include an AA entry for an additional-actions dictionary."))
					reported6_4_1_1_2 = true
					if allReported() {
						return violations
					}
				}
			}
		}
	}
	return violations
}
