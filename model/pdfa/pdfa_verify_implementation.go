package pdfa

import (
	"math"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func verifyImplementationLimitsPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var (
		reported6_1_12_1, reported6_1_12_2, reported6_1_12_3, reported6_1_12_4, reported6_1_12_5, reported6_1_12_11, reported6_1_12_9 bool
		visit                                                                                                                         func(core.PdfObject)
	)
	visit = func(obj core.PdfObject) {
		switch v := obj.(type) {
		case *core.PdfObjectInteger:
			if !reported6_1_12_1 && (int64(*v) > math.MaxInt32 || int64(*v) < -math.MaxInt32) {
				violations = append(violations, newViolatedRule("6.1.12-1", "Largest Integer value is 2,147,483,647. Smallest integer value is -2,147,483,648."))
				reported6_1_12_1 = true
			}
		case *core.PdfObjectFloat:
			if !reported6_1_12_2 && (math.Abs(float64(*v)) > 32767.0) {
				violations = append(violations, newViolatedRule("6.1.12-2", "Absolute real value must be less than or equal to 32767.0."))
			}
		case *core.PdfObjectString:
			if !reported6_1_12_3 && len([]byte(v.Str())) > 65535 {
				violations = append(violations, newViolatedRule("6.1.12-3", "Maximum length of a string (in bytes) is 65535."))
				reported6_1_12_3 = true
			}
		case *core.PdfObjectName:
			if !reported6_1_12_4 && len([]byte(*v)) > 127 {
				violations = append(violations, newViolatedRule("6.1.12-4", "Maximum length of a name (in bytes) is 127."))
				reported6_1_12_4 = true
			}
		case *core.PdfObjectArray:
			if !reported6_1_12_5 && v.Len() > 8191 {
				violations = append(violations, newViolatedRule("6.1.12-5", "Maximum length of an array (in elements) is 8191."))
				reported6_1_12_5 = true
			}
			for _, obj := range v.Elements() {
				visit(obj)
			}
			if !reported6_1_12_9 && (v.Len() == 4 || v.Len() == 5) {
				name, ok := core.GetName(v.Get(0))
				if !ok {
					return
				}
				if *name != "DeviceN" {
					return
				}
				obj := v.Get(1)
				obj = core.TraceToDirectObject(obj)
				arr, ok := core.GetArray(obj)
				if !ok {
					return
				}
				if arr.Len() > 8 {
					violations = append(violations, newViolatedRule("6.1.12-9", "Maximum number of DeviceN components is 8."))
					reported6_1_12_9 = true
				}
			}
		case *core.PdfObjectDictionary:
			names := v.Keys()
			if !reported6_1_12_11 && len(names) > 4095 {
				violations = append(violations, newViolatedRule("6.1.12-11", "Maximum capacity of a dictionary (in entries) is 4095."))
				reported6_1_12_11 = true
			}
			for i, name := range names {
				visit(&names[i])
				visit(v.Get(name))
			}
		case *core.PdfObjectStream:
			visit(v.PdfObjectDictionary)
		case *core.PdfObjectStreams:
			for _, obj := range v.Elements() {
				visit(obj)
			}
		case *core.PdfObjectReference:
			visit(v.Resolve())
		}
	}
	objectNums := r.GetObjectNums()
	if len(objectNums) > 8388607 {
		violations = append(violations, newViolatedRule("6.1.12-7", "Maximum number of indirect objects in a PDF file is 8,388,607."))
	}
	for _, objectNum := range objectNums {
		obj, err := r.GetIndirectObjectByNumber(objectNum)
		if err != nil {
			continue
		}
		obj2 := core.TraceToDirectObject(obj)
		visit(obj2)
	}
	return violations
}

func verifyImplementationLimitsPDFA23(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var (
		reported6_1_13_1, reported6_1_13_2, reported6_1_13_3, reported6_1_13_4, reported6_1_13_9 bool
		visit                                                                                    func(core.PdfObject)
	)
	visit = func(obj core.PdfObject) {
		switch v := obj.(type) {
		case *core.PdfObjectInteger:
			if !reported6_1_13_1 && (int64(*v) > math.MaxInt32 || int64(*v) < -math.MaxInt32) {
				violations = append(violations, newViolatedRule("6.1.13-1", "Largest Integer value is 2,147,483,647. Smallest integer value is -2,147,483,648."))
				reported6_1_13_1 = true
			}
		case *core.PdfObjectFloat:
			if !reported6_1_13_2 && (math.Abs(float64(*v)) > math.MaxFloat32) {
				violations = append(violations, newViolatedRule("6.1.13-2", "A conforming file shall not contain any real number outside the range of +/-3.403 x 10^38."))
			}
		case *core.PdfObjectString:
			if !reported6_1_13_3 && len([]byte(v.Str())) > 32767 {
				violations = append(violations, newViolatedRule("6.1.13-3", "Maximum length of a string (in bytes) is 32767."))
				reported6_1_13_3 = true
			}
		case *core.PdfObjectName:
			if !reported6_1_13_4 && len([]byte(*v)) > 127 {
				violations = append(violations, newViolatedRule("6.1.13-4", "Maximum length of a name (in bytes) is 127."))
				reported6_1_13_4 = true
			}
		case *core.PdfObjectArray:
			for _, obj := range v.Elements() {
				visit(obj)
			}
			if !reported6_1_13_9 && (v.Len() == 4 || v.Len() == 5) {
				name, ok := core.GetName(v.Get(0))
				if !ok {
					return
				}
				if *name != "DeviceN" {
					return
				}
				obj := v.Get(1)
				obj = core.TraceToDirectObject(obj)
				arr, ok := core.GetArray(obj)
				if !ok {
					return
				}
				if arr.Len() > 32 {
					violations = append(violations, newViolatedRule("6.1.13-9", "Maximum number of DeviceN components is 32."))
					reported6_1_13_9 = true
				}
			}
		case *core.PdfObjectDictionary:
			names := v.Keys()
			for i, name := range names {
				visit(&names[i])
				visit(v.Get(name))
			}
		case *core.PdfObjectStream:
			visit(v.PdfObjectDictionary)
		case *core.PdfObjectStreams:
			for _, obj := range v.Elements() {
				visit(obj)
			}
		case *core.PdfObjectReference:
			visit(v.Resolve())
		}
	}
	objectNums := r.GetObjectNums()
	if len(objectNums) > 8388607 {
		violations = append(violations, newViolatedRule("6.1.13-7", "Maximum number of indirect objects in a PDF file is 8,388,607."))
	}
	for _, objectNum := range objectNums {
		obj, err := r.GetIndirectObjectByNumber(objectNum)
		if err != nil {
			continue
		}
		obj2 := core.TraceToDirectObject(obj)
		visit(obj2)
	}
	return violations
}
