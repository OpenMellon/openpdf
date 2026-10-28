package pdfa

import (
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func verifyExtGState(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_2_5_1, reported6_2_5_2, reported6_2_5_3, reported6_2_5_4, reported6_2_5_5, reported6_2_10_1, reported6_2_10_1_2 bool
	allReported := func() bool {
		return reported6_2_5_1 && reported6_2_5_2 && reported6_2_5_3 && reported6_2_5_4 && reported6_2_5_5 && reported6_2_10_1 && reported6_2_10_1_2
	}
	checkExtGState := func(dict *core.PdfObjectDictionary) bool {
		if !reported6_2_5_1 && dict.Get("TR") != nil {
			reported6_2_5_1 = true
			violations = append(violations, newViolatedRule("6.2.5-1", "An ExtGState dictionary shall not contain the TR key."))
		}
		if tR2 := dict.Get("TR2"); !reported6_2_5_2 && tR2 != nil {
			tR2Name, ok := core.GetName(tR2)
			if !ok || (ok && *tR2Name != "Default") {
				reported6_2_5_2 = true
				violations = append(violations, newViolatedRule("6.2.5-2", "An ExtGState dictionary shall not contain the TR2 key with a value other than Default."))
				if allReported() {
					return true
				}
			}
		}
		if !reported6_2_5_3 && dict.Get("HTP") != nil {
			reported6_2_5_3 = true
			violations = append(violations, newViolatedRule("6.2.5-3", "An ExtGState dictionary shall not contain the HTP key."))
		}
		htDict, ok := core.GetDict(dict.Get("HT"))
		if ok {
			if halftoneType := htDict.Get("HalftoneType"); !reported6_2_5_4 && halftoneType != nil {
				n, ok := core.GetInt(halftoneType)
				if !ok || (ok && !(*n == 1 || *n == 5)) {
					violations = append(violations, newViolatedRule(" 6.2.5-4", "All halftones in a conforming PDF/A-2 file shall have the value 1 or 5 for the HalftoneType key."))
					if allReported() {
						return true
					}
				}
			}
			if halftoneName := htDict.Get("HalftoneName"); !reported6_2_5_5 && halftoneName != nil {
				reported6_2_5_5 = true
				violations = append(violations, newViolatedRule("6.2.5-5", "Halftones in a conforming PDF/A-2 file shall not contain a HalftoneName key."))
				if allReported() {
					return true
				}
			}
		}
		_, ok2 := firstOutputIntent(r)
		var reported6_2_10_2 bool
		groupDict, ok := core.GetDict(dict.Get("Group"))
		if ok {
			_, ok := core.GetName(groupDict.Get("CS"))
			if ok {
				reported6_2_10_2 = true
			}
		}
		if bm := dict.Get("BM"); !reported6_2_10_1 && !reported6_2_10_1_2 && bm != nil {
			bmName, ok := core.GetName(bm)
			if ok {
				switch bmName.String() {
				case "Normal", "Compatible", "Multiply", "Screen", "Overlay", "Darken", "Lighten", "ColorDodge", "ColorBurn", "HardLight", "SoftLight", "Difference", "Exclusion", "Hue", "Saturation", "Color", "Luminosity":
				default:
					reported6_2_10_1 = true
					violations = append(violations, newViolatedRule("6.2.10-1", "Only blend modes that are specified in ISO 32000-1:2008 shall be used for the value of the BM key in an extended graphic state dictionary."))
					if allReported() {
						return true
					}
				}
				if bmName.String() != "Normal" && !ok2 && !reported6_2_10_2 {
					reported6_2_10_1_2 = true
					violations = append(violations, newViolatedRule("6.2.10-2", "If the document does not contain a PDF/A OutputIntent, then all Page objects that contain transparency shall include the Group key, and the attribute dictionary that forms the value of that Group key shall include a CS entry whose value shall be used as the default blending colour space."))
					if allReported() {
						return true
					}
				}
			}
		}
		if _, ok = core.GetDict(dict.Get("SMask")); !reported6_2_10_1_2 && ok && !ok2 && !reported6_2_10_2 {
			reported6_2_10_1_2 = true
			violations = append(violations, newViolatedRule("6.2.10-2", "If the document does not contain a PDF/A OutputIntent, then all Page objects that contain transparency shall include the Group key, and the attribute dictionary that forms the value of that Group key shall include a CS entry whose value shall be used as the default blending colour space."))
			if allReported() {
				return true
			}
		}
		if ca := dict.Get("CA"); !reported6_2_10_1_2 && ca != nil && !ok2 && !reported6_2_10_2 {
			f, err := core.GetNumberAsFloat(ca)
			if err == nil && f < 1.0 {
				reported6_2_10_1_2 = true
				violations = append(violations, newViolatedRule("6.2.10-2", "If the document does not contain a PDF/A OutputIntent, then all Page objects that contain transparency shall include the Group key, and the attribute dictionary that forms the value of that Group key shall include a CS entry whose value shall be used as the default blending colour space."))
				if allReported() {
					return true
				}
			}
		}
		if ca := dict.Get("ca"); !reported6_2_10_1_2 && ca != nil && !ok2 && !reported6_2_10_2 {
			f, err := core.GetNumberAsFloat(ca)
			if err == nil && f < 1.0 {
				reported6_2_10_1_2 = true
				violations = append(violations, newViolatedRule("6.2.10-2", "If the document does not contain a PDF/A OutputIntent, then all Page objects that contain transparency shall include the Group key, and the attribute dictionary that forms the value of that Group key shall include a CS entry whose value shall be used as the default blending colour space."))
				if allReported() {
					return true
				}
			}
		}
		return false
	}
	for _, page := range r.PageList {
		res := page.Resources
		if res == nil {
			continue
		}
		if res.ExtGState == nil {
			continue
		}
		dict, ok := core.GetDict(res.ExtGState)
		if !ok {
			continue
		}
		names := dict.Keys()
		for _, name := range names {
			dict2, ok := core.GetDict(dict.Get(name))
			if !ok {
				continue
			}
			if checkExtGState(dict2) {
				return violations
			}
		}
	}
	for _, page := range r.PageList {
		res := page.Resources
		if res == nil {
			continue
		}
		dict, ok := core.GetDict(res.XObject)
		if !ok {
			continue
		}
		for _, name := range dict.Keys() {
			stream, ok := core.GetStream(dict.Get(name))
			if !ok {
				continue
			}
			resourcesDict, ok := core.GetDict(stream.Get("Resources"))
			if !ok {
				continue
			}
			extGStateDict, ok := core.GetDict(resourcesDict.Get("ExtGState"))
			if !ok {
				continue
			}
			for _, name := range extGStateDict.Keys() {
				dict, ok := core.GetDict(extGStateDict.Get(name))
				if !ok {
					continue
				}
				if checkExtGState(dict) {
					return violations
				}
			}
		}
	}
	return violations
}

func verifyExtGStatePDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_2_8_1, reported6_2_8_2, reported6_4_1, reported6_4_4, reported6_4_5, reported6_4_6 bool
	allReported := func() bool {
		return reported6_2_8_1 && reported6_2_8_2 && reported6_4_1 && reported6_4_4 && reported6_4_5 && reported6_4_6
	}
	checkExtGState := func(dict *core.PdfObjectDictionary) bool {
		if !reported6_2_8_1 && dict.Get("TR") != nil {
			reported6_2_8_1 = true
			violations = append(violations, newViolatedRule("6.2.8-1", "An ExtGState dictionary shall not contain the TR key."))
		}
		if tR2 := dict.Get("TR2"); !reported6_2_8_2 && tR2 != nil {
			tR2Name, ok := core.GetName(tR2)
			if !ok || (ok && *tR2Name != "Default") {
				reported6_2_8_2 = true
				violations = append(violations, newViolatedRule("6.2.8-2", "An ExtGState dictionary shall not contain the TR2 key with a value other than Default."))
				if allReported() {
					return true
				}
			}
		}
		if sMask := dict.Get("SMask"); !reported6_4_1 && sMask != nil {
			sMaskName, ok := core.GetName(sMask)
			if !ok || (ok && *sMaskName != "None") {
				reported6_4_1 = true
				violations = append(violations, newViolatedRule("6.4-1", "If an SMask key appears in an ExtGState dictionary, its value shall be None."))
				if allReported() {
					return true
				}
			}
		}
		if ca := dict.Get("CA"); !reported6_4_5 && ca != nil {
			f, err := core.GetNumberAsFloat(ca)
			if err == nil && f != 1.0 {
				reported6_4_5 = true
				violations = append(violations, newViolatedRule("6.4-5", "The following keys, if present in an ExtGState object, shall have the values shown: CA - 1.0."))
				if allReported() {
					return true
				}
			}
		}
		if ca := dict.Get("ca"); !reported6_4_6 && ca != nil {
			f, err := core.GetNumberAsFloat(ca)
			if err == nil && f != 1.0 {
				reported6_4_6 = true
				violations = append(violations, newViolatedRule("6.4-6", "The following keys, if present in an ExtGState object, shall have the values shown: ca - 1.0."))
				if allReported() {
					return true
				}
			}
		}
		if bm := dict.Get("BM"); !reported6_4_4 && bm != nil {
			bmName, ok := core.GetName(bm)
			if ok {
				switch bmName.String() {
				case "Normal", "Compatible":
				default:
					reported6_4_4 = true
					violations = append(violations, newViolatedRule("6.4-4", "The following keys, if present in an ExtGState object, shall have the values shown: BM - Normal or Compatible."))
					if allReported() {
						return true
					}
				}
			}
		}
		return false
	}
	for _, page := range r.PageList {
		res := page.Resources
		if res == nil {
			continue
		}
		if res.ExtGState == nil {
			continue
		}
		dict, ok := core.GetDict(res.ExtGState)
		if !ok {
			continue
		}
		names := dict.Keys()
		for _, name := range names {
			dict2, ok := core.GetDict(dict.Get(name))
			if !ok {
				continue
			}
			if checkExtGState(dict2) {
				return violations
			}
		}
	}
	for _, page := range r.PageList {
		res := page.Resources
		if res == nil {
			continue
		}
		dict, ok := core.GetDict(res.XObject)
		if !ok {
			continue
		}
		for _, name := range dict.Keys() {
			stream, ok := core.GetStream(dict.Get(name))
			if !ok {
				continue
			}
			resourcesDict, ok := core.GetDict(stream.Get("Resources"))
			if !ok {
				continue
			}
			extGStateDict, ok := core.GetDict(resourcesDict.Get("ExtGState"))
			if !ok {
				continue
			}
			for _, name := range extGStateDict.Keys() {
				dict, ok := core.GetDict(extGStateDict.Get(name))
				if !ok {
					continue
				}
				if checkExtGState(dict) {
					return violations
				}
			}
		}
	}
	return violations
}
