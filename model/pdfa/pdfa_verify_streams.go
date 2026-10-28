package pdfa

import (
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func verifyStreamsPDFA23(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_1_7_1, reported6_1_7_3, reported6_1_7_4 bool
	if r.ParserMetadata().HasNonConformantStream() {
		violations = []ViolatedRule{newViolatedRule("6.1.7-2", "The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah) character sequence or by a single LINE FEED character. The endstream keyword shall be preceded by an EOL marker.")}
	}
	for _, n := range r.GetObjectNums() {
		obj, _ := r.GetIndirectObjectByNumber(n)
		if obj == nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		if !reported6_1_7_1 {
			length := stream.Get("Length")
			if length == nil {
				violations = append(violations, newViolatedRule("6.1.7-1", "no 'Length' key found in the stream object"))
				reported6_1_7_1 = true
			} else {
				n, ok := core.GetIntVal(length)
				if !ok {
					violations = append(violations, newViolatedRule("6.1.7-1", "stream 'Length' key should be an integer"))
					reported6_1_7_1 = true
				} else {
					if len(stream.Stream) != n {
						violations = append(violations, newViolatedRule("6.1.7-1", "stream object length value should match the size of the stream"))
						reported6_1_7_1 = true
					}
				}
			}
		}
		if !reported6_1_7_3 {
			if stream.Get("F") != nil {
				reported6_1_7_3 = true
				violations = append(violations, newViolatedRule("6.1.7-3", "stream object should not contain 'F', 'FFilter', or 'FDecodeParams' key"))
			}
			if stream.Get("FFilter") != nil && !reported6_1_7_3 {
				reported6_1_7_3 = true
				violations = append(violations, newViolatedRule("6.1.7-3", "stream object should not contain 'F', 'FFilter', or 'FDecodeParams' key"))
				continue
			}
			if stream.Get("FDecodeParams") != nil && !reported6_1_7_3 {
				reported6_1_7_3 = true
				violations = append(violations, newViolatedRule("6.1.7-3", "stream object should not contain 'F', 'FFilter', or 'FDecodeParams' key"))
				continue
			}
		}
		if !reported6_1_7_4 {
			filterName, ok := core.GetName(core.TraceToDirectObject(stream.Get("Filter")))
			if !ok {
				continue
			}
			if *filterName == core.StreamEncodingFilterNameLZW {
				reported6_1_7_4 = true
				violations = append(violations, newViolatedRule("6.1.7-4", "The LZWDecode filter shall not be permitted."))
			}
		}
	}
	return violations
}

func verifyStreamsPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_1_7_2, reported6_1_7_3, reported6_1_10_1 bool
	if r.ParserMetadata().HasNonConformantStream() {
		violations = []ViolatedRule{newViolatedRule("6.1.7-1", "The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah) character sequence or by a single LINE FEED character. The endstream keyword shall be preceded by an EOL marker.")}
	}
	for _, n := range r.GetObjectNums() {
		obj, _ := r.GetIndirectObjectByNumber(n)
		if obj == nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		if !reported6_1_7_2 {
			length := stream.Get("Length")
			if length == nil {
				violations = append(violations, newViolatedRule("6.1.7-2", "no 'Length' key found in the stream object"))
				reported6_1_7_2 = true
			} else {
				n, ok := core.GetIntVal(length)
				if !ok {
					violations = append(violations, newViolatedRule("6.1.7-2", "stream 'Length' key should be an integer"))
					reported6_1_7_2 = true
				} else {
					if len(stream.Stream) != n {
						violations = append(violations, newViolatedRule("6.1.7-2", "stream object length value should match the size of the stream"))
						reported6_1_7_2 = true
					}
				}
			}
		}
		if !reported6_1_7_3 {
			if stream.Get("F") != nil {
				reported6_1_7_3 = true
				violations = append(violations, newViolatedRule("6.1.7-3", "stream object should not contain 'F','FFilter','FDecodeParams' key"))
			}
			if stream.Get("FFilter") != nil && !reported6_1_7_3 {
				reported6_1_7_3 = true
				violations = append(violations, newViolatedRule("6.1.7-3", "stream object should not contain 'F','FFilter','FDecodeParams' key"))
				continue
			}
			if stream.Get("FDecodeParams") != nil && !reported6_1_7_3 {
				reported6_1_7_3 = true
				violations = append(violations, newViolatedRule("6.1.7-3", "stream object should not contain 'F','FFilter','FDecodeParams' key"))
				continue
			}
		}
		if !reported6_1_10_1 {
			filterName, ok := core.GetName(core.TraceToDirectObject(stream.Get("Filter")))
			if !ok {
				continue
			}
			if *filterName == core.StreamEncodingFilterNameLZW {
				reported6_1_10_1 = true
				violations = append(violations, newViolatedRule("6.1.10-1", "The LZWDecode filter shall not be permitted."))
			}
		}
	}
	return violations
}

func verifyStreamsPDFA4(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_1_6_1_1, reported6_1_6_1_2, reported6_1_6_2_1 bool
	if r.ParserMetadata().HasNonConformantStream() {
		violations = []ViolatedRule{newViolatedRule("6.1.6.1-1", "The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah) character sequence or by a single LINE FEED character. The endstream keyword shall be preceded by an EOL marker.")}
	}
	for _, n := range r.GetObjectNums() {
		obj, _ := r.GetIndirectObjectByNumber(n)
		if obj == nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		if !reported6_1_6_1_1 {
			length := stream.Get("Length")
			if length == nil {
				violations = append(violations, newViolatedRule("6.1.6.1-1", "no 'Length' key found in the stream object"))
				reported6_1_6_1_1 = true
			} else {
				n, ok := core.GetIntVal(length)
				if !ok {
					violations = append(violations, newViolatedRule("6.1.6.1-1", "stream 'Length' key should be an integer"))
					reported6_1_6_1_1 = true
				} else {
					if len(stream.Stream) != n {
						violations = append(violations, newViolatedRule("6.1.6.1-1", "stream object length value should match the size of the stream"))
						reported6_1_6_1_1 = true
					}
				}
			}
		}
		if !reported6_1_6_1_2 {
			if stream.Get("F") != nil {
				reported6_1_6_1_2 = true
				violations = append(violations, newViolatedRule("6.1.6.1-2", "stream object should not contain 'F', 'FFilter', or 'FDecodeParams' key"))
			}
			if stream.Get("FFilter") != nil && !reported6_1_6_1_2 {
				reported6_1_6_1_2 = true
				violations = append(violations, newViolatedRule("6.1.6.1-2", "stream object should not contain 'F', 'FFilter', or 'FDecodeParams' key"))
				continue
			}
			if stream.Get("FDecodeParams") != nil && !reported6_1_6_1_2 {
				reported6_1_6_1_2 = true
				violations = append(violations, newViolatedRule("6.1.6.1-2", "stream object should not contain 'F', 'FFilter', or 'FDecodeParams' key"))
				continue
			}
		}
		if !reported6_1_6_2_1 {
			filterName, ok := core.GetName(core.TraceToDirectObject(stream.Get("Filter")))
			if !ok {
				continue
			}
			if *filterName == core.StreamEncodingFilterNameLZW {
				reported6_1_6_2_1 = true
				violations = append(violations, newViolatedRule("6.1.6.2-1", "The LZWDecode filter shall not be permitted."))
			}
		}
	}
	return violations
}
