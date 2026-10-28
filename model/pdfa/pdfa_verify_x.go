package pdfa

import (
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func verifyXObjectsPDFA23(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_2_8_1, reported6_2_9_1, reported6_2_9_1_2, reported6_2_9_2, reported6_2_8_3, reported6_2_9_3, reported6_2_8_2 bool
	seen := map[*core.PdfObjectStream]struct{}{}
	for _, n := range r.GetObjectNums() {
		if reported6_2_8_1 && reported6_2_9_1 && reported6_2_8_3 && reported6_2_9_1_2 && reported6_2_9_2 && reported6_2_9_3 && reported6_2_8_2 {
			return violations
		}
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		if _, ok = seen[stream]; ok {
			continue
		}
		seen[stream] = struct{}{}
		subTypeName, ok := core.GetName(stream.Get("SubType"))
		if !ok {
			continue
		}
		if !reported6_2_9_2 {
			if stream.Get("Ref") != nil {
				violations = append(violations, newViolatedRule("6.2.9-2", "A conforming file shall not contain any reference XObjects."))
				reported6_2_9_2 = true
			}
		}
		if subTypeName.String() == "PS" {
			if !reported6_2_9_3 {
				violations = append(violations, newViolatedRule("6.2.9-3", "A conforming file shall not contain any PostScript XObjects."))
				reported6_2_9_3 = true
				continue
			}
		}
		if subTypeName.String() == "Form" {
			if reported6_2_9_1 && reported6_2_9_1_2 && reported6_2_9_2 {
				continue
			}
			if !reported6_2_9_1 && stream.Get("OPI") != nil {
				violations = append(violations, newViolatedRule("6.2.9-1", "A form XObject dictionary shall not contain the OPI key."))
				reported6_2_9_1 = true
			}
			if !reported6_2_9_1_2 {
				if stream.Get("PS") != nil {
					reported6_2_9_1_2 = true
				}
				if subtype2 := stream.Get("Subtype2"); subtype2 != nil && !reported6_2_9_1_2 {
					if subtype2Name, ok := core.GetName(subtype2); ok && *subtype2Name == "PS" {
						reported6_2_9_1_2 = true
					}
				}
				if reported6_2_9_1_2 {
					violations = append(violations, newViolatedRule("6.2.9-1", "A form XObject dictionary shall not contain the Subtype2 key with a value of PS and the PS key."))
				}
			}
			continue
		}
		if subTypeName.String() != "Image" {
			continue
		}
		if !reported6_2_8_1 && stream.Get("Alternates") != nil {
			violations = append(violations, newViolatedRule("6.2.8-1", "An Image dictionary shall not contain the Alternates key."))
			reported6_2_8_1 = true
		}
		if !reported6_2_8_2 && stream.Get("OPI") != nil {
			violations = append(violations, newViolatedRule("6.2.8-2", "An Image dictionary shall not contain the OPI key."))
			reported6_2_8_2 = true
		}
		if !reported6_2_8_3 && stream.Get("Interpolate") != nil {
			interpolate, ok := core.GetBool(stream.Get("Interpolate"))
			if ok && bool(*interpolate) {
				continue
			}
			violations = append(violations, newViolatedRule("6.2.8-3", "If an Image dictionary contains the Interpolate key, its value shall be false."))
			reported6_2_8_3 = true
		}
	}
	return violations
}

func verifyXObjectsPDFA4(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_2_7_1_1, reported6_2_8_1_1, reportedUnused2, reported6_2_8_2_1, reported6_2_7_1_3, reportedUnused1, reported6_2_7_1_2 bool
	seen := map[*core.PdfObjectStream]struct{}{}
	for _, n := range r.GetObjectNums() {
		if reported6_2_7_1_1 && reported6_2_8_1_1 && reported6_2_7_1_3 && reportedUnused2 && reported6_2_8_2_1 && reportedUnused1 && reported6_2_7_1_2 {
			return violations
		}
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		if _, ok = seen[stream]; ok {
			continue
		}
		seen[stream] = struct{}{}
		subTypeName, ok := core.GetName(stream.Get("SubType"))
		if !ok {
			continue
		}
		if !reported6_2_8_2_1 {
			if stream.Get("Ref") != nil {
				violations = append(violations, newViolatedRule("6.2.8.2-1", "A conforming file shall not contain any reference XObjects."))
				reported6_2_8_2_1 = true
			}
		}
		if subTypeName.String() == "Form" {
			if reported6_2_8_1_1 && reportedUnused2 && reported6_2_8_2_1 {
				continue
			}
			if !reported6_2_8_1_1 && stream.Get("OPI") != nil {
				violations = append(violations, newViolatedRule("6.2.8.1-1", "A form XObject dictionary shall not contain the OPI key."))
				reported6_2_8_1_1 = true
			}
			continue
		}
		if subTypeName.String() != "Image" {
			continue
		}
		if !reported6_2_7_1_1 && stream.Get("Alternates") != nil {
			violations = append(violations, newViolatedRule("6.2.7.1-1", "An Image dictionary shall not contain the Alternates key."))
			reported6_2_7_1_1 = true
		}
		if !reported6_2_7_1_2 && stream.Get("OPI") != nil {
			violations = append(violations, newViolatedRule("6.2.7.1-2", "An Image dictionary shall not contain the OPI key."))
			reported6_2_7_1_2 = true
		}
		if !reported6_2_7_1_3 && stream.Get("Interpolate") != nil {
			interpolate, ok := core.GetBool(stream.Get("Interpolate"))
			if ok && bool(*interpolate) {
				continue
			}
			violations = append(violations, newViolatedRule("6.2.7.1-3", "If an Image dictionary contains the Interpolate key, its value shall be false."))
			reported6_2_7_1_3 = true
		}
	}
	return violations
}

func verifyXObjectSMaskPDFA1(r *model.CompliancePdfReader) ViolatedRule {
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(stream.Get("Type"))
		if !ok {
			continue
		}
		if *typeName != "XObject" {
			continue
		}
		if stream.Get("SMask") != nil {
			return newViolatedRule("6.4-2", "An XObject dictionary shall not contain the SMask key.")
		}
	}
	return noViolation
}

func verifyXObjectsPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_2_4_1, reported6_2_4_2, reported6_2_5_1, reported6_2_6_1, reported6_2_4_3, reported6_2_7_1 bool
	seen := map[*core.PdfObjectStream]struct{}{}
	for _, n := range r.GetObjectNums() {
		if reported6_2_4_1 && reported6_2_4_2 && reported6_2_4_3 && reported6_2_5_1 && reported6_2_6_1 && reported6_2_7_1 {
			return violations
		}
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		if _, ok = seen[stream]; ok {
			continue
		}
		seen[stream] = struct{}{}
		subTypeName, ok := core.GetName(stream.Get("SubType"))
		if !ok {
			continue
		}
		if !reported6_2_6_1 {
			if stream.Get("Ref") != nil {
				violations = append(violations, newViolatedRule("6.2.6-1", "A conforming file shall not contain any reference XObjects."))
				reported6_2_6_1 = true
			}
		}
		if subTypeName.String() == "PS" {
			if !reported6_2_7_1 {
				violations = append(violations, newViolatedRule("6.2.7-1", "A conforming file shall not contain any PostScript XObjects."))
				reported6_2_7_1 = true
				continue
			}
		}
		if subTypeName.String() == "Form" {
			if reported6_2_4_2 && reported6_2_5_1 && reported6_2_6_1 {
				continue
			}
			if !reported6_2_4_2 && stream.Get("OPI") != nil {
				violations = append(violations, newViolatedRule("6.2.4-2", "An XObject dictionary (Image or Form) shall not contain the OPI key."))
				reported6_2_4_2 = true
			}
			if !reported6_2_5_1 {
				if stream.Get("PS") != nil {
					reported6_2_5_1 = true
				}
				if subtype2 := stream.Get("Subtype2"); subtype2 != nil && !reported6_2_5_1 {
					if subtype2Name, ok := core.GetName(subtype2); ok && *subtype2Name == "PS" {
						reported6_2_5_1 = true
					}
				}
				if reported6_2_5_1 {
					violations = append(violations, newViolatedRule("6.2.5-1", "A form XObject dictionary shall not contain the Subtype2 key with a value of PS or the PS key."))
				}
			}
			continue
		}
		if subTypeName.String() != "Image" {
			continue
		}
		if !reported6_2_4_1 && stream.Get("Alternates") != nil {
			violations = append(violations, newViolatedRule("6.2.4-1", "An Image dictionary shall not contain the Alternates key."))
			reported6_2_4_1 = true
		}
		if !reported6_2_4_3 && stream.Get("Interpolate") != nil {
			interpolate, ok := core.GetBool(stream.Get("Interpolate"))
			if ok && bool(*interpolate) {
				continue
			}
			violations = append(violations, newViolatedRule("6.2.4-3", "If an Image dictionary contains the Interpolate key, its value shall be false."))
			reported6_2_4_3 = true
		}
	}
	return violations
}
