package pdfa

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func verifyActionsPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_6_1_1, reported6_6_1_2 bool
	allReported := func() bool { return reported6_6_1_1 && reported6_6_1_2 }
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			common.Log.Debug("Getting object with number %d failed: %v", n, err)
			continue
		}
		dict, ok := core.GetDict(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(dict.Get("Type"))
		if !ok {
			continue
		}
		if *typeName != "Action" {
			continue
		}
		sName, ok := core.GetName(dict.Get("S"))
		if !ok {
			if !reported6_6_1_1 {
				violations = append(violations, newViolatedRule("6.6.1-1", "The Launch, Sound, Movie, ResetForm, ImportData and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and no-op actions shall not be permitted. The Hide action shall not be permitted."))
				reported6_6_1_1 = true
				if allReported() {
					return violations
				}
			}
			continue
		}
		switch model.PdfActionType(*sName) {
		case model.ActionTypeLaunch, model.ActionTypeSound, model.ActionTypeMovie, model.ActionTypeResetForm, model.ActionTypeImportData, model.ActionTypeJavaScript:
			if !reported6_6_1_1 {
				violations = append(violations, newViolatedRule("6.6.1-1", "The Launch, Sound, Movie, ResetForm, ImportData and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and no-op actions shall not be permitted. The Hide action shall not be permitted."))
				reported6_6_1_1 = true
				if allReported() {
					return violations
				}
			}
			continue
		case model.ActionTypeNamed:
			if !reported6_6_1_2 {
				nName, ok := core.GetName(dict.Get("N"))
				if !ok {
					violations = append(violations, newViolatedRule("6.6.1-2", "Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted."))
					reported6_6_1_2 = true
					if allReported() {
						return violations
					}
					continue
				}
				switch *nName {
				case "NextPage", "PrevPage", "FirstPage", "LastPage":
				default:
					violations = append(violations, newViolatedRule("6.6.1-2", "Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted."))
					reported6_6_1_2 = true
					if allReported() {
						return violations
					}
					continue
				}
			}
		}
	}
	return violations
}

func verifyActionsPDFA4(r *model.CompliancePdfReader, std standardType) (violations []ViolatedRule) {
	var reported6_6_1_1, reported6_6_1_2 bool
	allReported := func() bool { return reported6_6_1_1 && reported6_6_1_2 }
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			common.Log.Debug("Getting object with number %d failed: %v", n, err)
			continue
		}
		dict, ok := core.GetDict(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(dict.Get("Type"))
		if !ok {
			continue
		}
		if *typeName != "Action" {
			continue
		}
		sName, ok := core.GetName(dict.Get("S"))
		if !ok {
			if !reported6_6_1_1 {
				violations = append(violations, newViolatedRule("6.6.1-1", "The Launch, Sound, Movie, ResetForm, ImportData, Hide, Rendition, Trans, and JavaScript actions shall not be permitted. SetOCGState and GoTo3DView are allowed only in 4E profile. Additionally, the deprecated set-state and noop actions shall not be permitted."))
				reported6_6_1_1 = true
				if allReported() {
					return violations
				}
			}
			continue
		}
		switch model.PdfActionType(*sName) {
		case model.ActionTypeLaunch, model.ActionTypeSound, model.ActionTypeMovie, model.ActionTypeResetForm, model.ActionTypeImportData, model.ActionTypeJavaScript, model.ActionTypeHide, model.ActionTypeRendition, model.ActionTypeTrans:
			if !reported6_6_1_1 {
				violations = append(violations, newViolatedRule("6.6.1-1", "The Launch, Sound, Movie, ResetForm, ImportData, Hide, Rendition, Trans, and JavaScript actions shall not be permitted. SetOCGState and GoTo3DView are allowed only in 4E profile. Additionally, the deprecated set-state and noop actions shall not be permitted."))
				reported6_6_1_1 = true
				if allReported() {
					return violations
				}
			}
			continue
		case model.ActionTypeSetOCGState, model.ActionTypeGoTo3DView:
			if std.conformance != "E" {
				if !reported6_6_1_1 {
					violations = append(violations, newViolatedRule("6.6.1-1", "The Launch, Sound, Movie, ResetForm, ImportData, Hide, Rendition, Trans, and JavaScript actions shall not be permitted. SetOCGState and GoTo3DView are allowed only in 4E profile. Additionally, the deprecated set-state and noop actions shall not be permitted."))
					reported6_6_1_1 = true
					if allReported() {
						return violations
					}
				}
			}
			continue
		case model.ActionTypeNamed:
			if !reported6_6_1_2 {
				nName, ok := core.GetName(dict.Get("N"))
				if !ok {
					violations = append(violations, newViolatedRule("6.6.1-2", "Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted."))
					reported6_6_1_2 = true
					if allReported() {
						return violations
					}
					continue
				}
				switch *nName {
				case "NextPage", "PrevPage", "FirstPage", "LastPage":
				default:
					violations = append(violations, newViolatedRule("6.6.1-2", "Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted."))
					reported6_6_1_2 = true
					if allReported() {
						return violations
					}
					continue
				}
			}
		}
	}
	return violations
}

func verifyActionsPDFA23(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_5_1_1, reported6_5_1_2 bool
	allReported := func() bool { return reported6_5_1_1 && reported6_5_1_2 }
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			common.Log.Debug("Getting object with number %d failed: %v", n, err)
			continue
		}
		dict, ok := core.GetDict(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(dict.Get("Type"))
		if !ok {
			continue
		}
		if *typeName != "Action" {
			continue
		}
		sName, ok := core.GetName(dict.Get("S"))
		if !ok {
			if !reported6_5_1_1 {
				violations = append(violations, newViolatedRule("6.5.1-1", "The Launch, Sound, Movie, ResetForm, ImportData, Hide, SetOCGState, Rendition, Trans, GoTo3DView and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and noop actions shall not be permitted."))
				reported6_5_1_1 = true
				if allReported() {
					return violations
				}
			}
			continue
		}
		switch model.PdfActionType(*sName) {
		case model.ActionTypeLaunch, model.ActionTypeSound, model.ActionTypeMovie, model.ActionTypeResetForm, model.ActionTypeImportData, model.ActionTypeJavaScript, model.ActionTypeHide, model.ActionTypeSetOCGState, model.ActionTypeRendition, model.ActionTypeTrans, model.ActionTypeGoTo3DView:
			if !reported6_5_1_1 {
				violations = append(violations, newViolatedRule("6.5.1-1", "The Launch, Sound, Movie, ResetForm, ImportData, Hide, SetOCGState, Rendition, Trans, GoTo3DView and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and noop actions shall not be permitted."))
				reported6_5_1_1 = true
				if allReported() {
					return violations
				}
			}
			continue
		case model.ActionTypeNamed:
			if !reported6_5_1_2 {
				nName, ok := core.GetName(dict.Get("N"))
				if !ok {
					violations = append(violations, newViolatedRule("6.5.1-2", "Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted."))
					reported6_5_1_2 = true
					if allReported() {
						return violations
					}
					continue
				}
				switch *nName {
				case "NextPage", "PrevPage", "FirstPage", "LastPage":
				default:
					violations = append(violations, newViolatedRule("6.5.1-2", "Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted."))
					reported6_5_1_2 = true
					if allReported() {
						return violations
					}
					continue
				}
			}
		}
	}
	return violations
}
