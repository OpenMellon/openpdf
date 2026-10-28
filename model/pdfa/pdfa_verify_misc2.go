package pdfa

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/xmputil"
)

func verifyLinearized(r *model.CompliancePdfReader, clause string) ViolatedRule {
	if !r.IsLinearized() {
		return noViolation
	}
	if linearizationViolation := r.ValidateLinearizationTrailerID(); linearizationViolation != nil {
		return newViolatedRule(clause, "linearized PDF: "+linearizationViolation.Message)
	}
	return noViolation
}

func verifyHeaderPDFA4(r *model.CompliancePdfReader) ViolatedRule {
	if r.ParserMetadata().HeaderPosition() != 0 {
		return newViolatedRule("6.1.2-1", "header position is not at the first byte")
	}
	if r.PdfVersion().Major != 2 {
		return newViolatedRule("6.1.2-1", "File header starts at non-zero offset or does not match the pattern %PDF-2.n, where 'n' is a single digit number between 0 and 9")
	}
	if r.PdfVersion().Minor < 0 || r.PdfVersion().Minor > 9 {
		return newViolatedRule("6.1.2-1", "File header starts at non-zero offset or does not match the pattern %PDF-2.n, where 'n' is a single digit number between 0 and 9")
	}
	return noViolation
}

func verifyInfoMatchesXMP(info *model.PdfInfo, xmp *xmputil.Document) (violations []ViolatedRule) {
	pdfInfo, ok := xmp.GetPdfInfo()
	if !ok || pdfInfo.InfoDict == nil {
		return nil
	}
	pdfInfoFromObject, err := model.NewPdfInfoFromObject(pdfInfo.InfoDict)
	if err != nil {
		return nil
	}
	for _, infoKeyRule := range infoKeyRules {
		if !infoEntryMatches(infoKeyRule.Key, info, pdfInfoFromObject) {
			violations = append(violations, newViolatedRule(infoKeyRule.RuleNo, infoXMPEquivalenceDetail(infoKeyRule.Key)))
		}
	}
	return violations
}

func verifyFormXObjectsPDFA1(r *model.CompliancePdfReader) (rule ViolatedRule) {
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
		subtype2Name, ok := core.GetName(stream.Get("Subtype2"))
		if !ok {
			continue
		}
		if *subtype2Name == "PS" {
			return newViolatedRule("6.2.5-1", "A form XObject dictionary shall not contain the Subtype2 key with a value of PS or the PS key.")
		}
		if stream.Get("PS") != nil {
			return newViolatedRule("6.2.5-1", "A form XObject dictionary shall not contain the Subtype2 key with a value of PS or the PS key.")
		}
	}
	return rule
}

func verifyInteractiveFormsPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
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
		if typeName.String() != "AcroForm" {
			continue
		}
		needAppearances, ok := core.GetBool(dict.Get("NeedAppearances"))
		if !ok {
			return violations
		}
		if bool(*needAppearances) {
			violations = append(violations, newViolatedRule("6.9-1", "The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false."))
		}
	}
	return violations
}

func verifyFieldActionsPDFA23(r *model.CompliancePdfReader) (rule ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return noViolation
	}
	acroFormDict, ok := core.GetDict(dict.Get("AcroForm"))
	if !ok {
		return noViolation
	}
	fieldsArray, ok := core.GetArray(acroFormDict.Get("Fields"))
	if !ok {
		return noViolation
	}
	for n := 0; n < fieldsArray.Len(); n++ {
		dict, ok := core.GetDict(fieldsArray.Get(n))
		if !ok {
			continue
		}
		if dict.Get("A") != nil {
			return newViolatedRule("6.4.1-2", "A Field dictionary shall not contain the A or AA keys.")
		}
		if dict.Get("AA") != nil {
			return newViolatedRule("6.4.1-2", "A Field dictionary shall not contain the A or AA keys.")
		}
	}
	return noViolation
}

func verifyHexStrings(r *model.CompliancePdfReader, clause string) (violations []ViolatedRule) {
	if r.ParserMetadata().HasOddLengthHexStrings() {
		violations = append(violations, newViolatedRule(clause+"-1", "hexadecimal strings should be of even size"))
	}
	if r.ParserMetadata().HasOddLengthHexStrings() {
		violations = append(violations, newViolatedRule(clause+"-2", "hexadecimal strings should contain only characters from range [0-9;A-F;a-f]"))
	}
	return violations
}

func verifyFormXObjectsPDFA23(r *model.CompliancePdfReader) (rule ViolatedRule) {
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
		_, ok = core.GetName(stream.Get("OPI"))
		if ok {
			return newViolatedRule("6.2.9-1", "A form XObject dictionary shall not contain any of the following: - the OPI key; - the Subtype2 key with a value of PS; - the PS key.")
		}
		subtype2Name, ok := core.GetName(stream.Get("Subtype2"))
		if !ok {
			continue
		}
		if *subtype2Name == "PS" {
			return newViolatedRule("6.2.9-1", "A form XObject dictionary shall not contain any of the following: - the OPI key; - the Subtype2 key with a value of PS; - the PS key.")
		}
		if stream.Get("PS") != nil {
			return newViolatedRule("6.2.9-1", "A form XObject dictionary shall not contain any of the following: - the OPI key; - the Subtype2 key with a value of PS; - the PS key.")
		}
	}
	return rule
}

func verifyInteractiveFormsPDFA23(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
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
		if typeName.String() != "AcroForm" {
			continue
		}
		needAppearances, ok := core.GetBool(dict.Get("NeedAppearances"))
		if ok && bool(*needAppearances) {
			violations = append(violations, newViolatedRule("6.4.1-3", "The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false."))
		}
		if dict.Get("XFA") != nil {
			violations = append(violations, newViolatedRule("6.4.2-1", "The document's interactive form dictionary that forms the value of the AcroForm key in the document's Catalog of a PDF/A-2 file, if present, shall not contain the XFA key."))
		}
	}
	dict, ok := catalogDict(r)
	if ok && dict.Get("NeedsRendering") != nil {
		violations = append(violations, newViolatedRule("6.4.2-2", "A document's Catalog shall not contain the NeedsRendering key."))
	}
	return violations
}

func verifyInteractiveFormsPDFA4(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
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
		if typeName.String() != "AcroForm" {
			continue
		}
		needAppearances, ok := core.GetBool(dict.Get("NeedAppearances"))
		if ok && bool(*needAppearances) {
			violations = append(violations, newViolatedRule("6.4.1-2", "The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false."))
		}
		if dict.Get("XFA") != nil {
			violations = append(violations, newViolatedRule("6.4.2-1", "The document's interactive form dictionary that forms the value of the AcroForm key in the document's Catalog of a PDF/A-2 file, if present, shall not contain the XFA key."))
		}
	}
	dict, ok := catalogDict(r)
	if ok && dict.Get("NeedsRendering") != nil {
		violations = append(violations, newViolatedRule("6.4.2-2", "A document's Catalog shall not contain the NeedsRendering key."))
	}
	return violations
}

func verifyFieldActionsPDFA1(r *model.CompliancePdfReader) (rule ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return noViolation
	}
	acroFormDict, ok := core.GetDict(dict.Get("AcroForm"))
	if !ok {
		return noViolation
	}
	fieldsArray, ok := core.GetArray(acroFormDict.Get("Fields"))
	if !ok {
		return noViolation
	}
	for n := 0; n < fieldsArray.Len(); n++ {
		dict, ok := core.GetDict(fieldsArray.Get(n))
		if !ok {
			continue
		}
		if dict.Get("AA") != nil {
			return newViolatedRule("6.6.2-2", "A Field dictionary shall not include an AA entry for an additional-actions dictionary.")
		}
	}
	return noViolation
}

func verifyFiltersPDFA1(r *model.CompliancePdfReader) ViolatedRule {
	for _, page := range r.PageList {
		contentStreamObjs := page.GetContentStreamObjs()
		for _, contentStreamObj := range contentStreamObjs {
			contentStreamObj = core.TraceToDirectObject(contentStreamObj)
			var s string
			switch contentStream := contentStreamObj.(type) {
			case *core.PdfObjectString:
				s = contentStream.Str()
			case *core.PdfObjectStream:
				filterName, ok := core.GetName(core.TraceToDirectObject(contentStream.Get("Filter")))
				if ok {
					if *filterName == core.StreamEncodingFilterNameLZW {
						return newViolatedRule("6.1.10-2", "The LZWDecode filter shall not be permitted.")
					}
				}
				data, err := core.DecodeStream(contentStream)
				if err != nil {
					common.Log.Debug("Err: %v", err)
					continue
				}
				s = string(data)
			default:
				common.Log.Debug("Invalid content stream object: %T", contentStreamObj)
				continue
			}
			contentStreamParser := contentstream.NewContentStreamParser(s)
			ops, err := contentStreamParser.Parse()
			if err != nil {
				common.Log.Debug("Invalid content stream: %v", err)
				continue
			}
			for _, op := range *ops {
				if !(op.Operand == "BI" && len(op.Params) == 1) {
					continue
				}
				contentStreamInlineImage, ok := op.Params[0].(*contentstream.ContentStreamInlineImage)
				if !ok {
					continue
				}
				encoder, err := contentStreamInlineImage.GetEncoder()
				if err != nil {
					common.Log.Debug("getting inline image encoder failed: %v", err)
					continue
				}
				if encoder.GetFilterName() == core.StreamEncodingFilterNameLZW {
					return newViolatedRule("6.1.10-2", "The LZWDecode filter shall not be permitted.")
				}
			}
		}
	}
	return noViolation
}

func verifyHeaderPDFA1(r *model.CompliancePdfReader) ViolatedRule {
	if r.ParserMetadata().HeaderPosition() != 0 {
		return newViolatedRule("6.1.2-1", "header position is not at the first byte")
	}
	return noViolation
}

func verifyLinearizedPDFA23(r *model.CompliancePdfReader) ViolatedRule {
	return verifyLinearized(r, "6.1.11")
}

func verifyHeaderBinaryComment(r *model.CompliancePdfReader) ViolatedRule {
	bs := r.ParserMetadata().HeaderCommentBytes()
	if bs[0] > 127 && bs[1] > 127 && bs[2] > 127 && bs[3] > 127 {
		return noViolation
	}
	return newViolatedRule("6.1.2-2", "The file header line shall be immediately followed by a comment consisting of a % character followed by at\tleast four characters, each of whose encoded byte values shall have a decimal value greater than 127.")
}

func verifyFormXObjectOPIPDFA4(r *model.CompliancePdfReader) (rule ViolatedRule) {
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
		_, ok = core.GetName(stream.Get("OPI"))
		if ok {
			return newViolatedRule("6.2.8.1-1", "A form XObject dictionary shall not contain an OPI key.")
		}
	}
	return rule
}

func verifyLinearizedPDFA1(r *model.CompliancePdfReader) ViolatedRule {
	return verifyLinearized(r, "6.1.3-4")
}

func verifyHeaderPDFA23(r *model.CompliancePdfReader) ViolatedRule {
	if r.ParserMetadata().HeaderPosition() != 0 {
		return newViolatedRule("6.1.2-1", "header position is not at the first byte")
	}
	if r.PdfVersion().Major != 1 {
		return newViolatedRule("6.1.2-1", "The file header shall consist of “%PDF-1.n” followed by a single EOL marker, where 'n' is a single digit number between 0 (30h) and 7 (37h)")
	}
	if r.PdfVersion().Minor < 0 || r.PdfVersion().Minor > 7 {
		return newViolatedRule("6.1.2-1", "The file header shall consist of “%PDF-1.n” followed by a single EOL marker, where 'n' is a single digit number between 0 (30h) and 7 (37h)")
	}
	return noViolation
}
