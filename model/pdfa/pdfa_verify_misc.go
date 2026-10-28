package pdfa

import (
	"strings"

	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
)

func verifyCMapWMode(dict *core.PdfObjectDictionary, streamData map[*core.PdfObjectStream][]byte, cmaps map[*core.PdfObjectStream]*cmap.CMap, clause string) ViolatedRule {
	s := clause + ".3-4"
	const (
		detail = "For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream."
	)
	var str string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		str = subtypeName.String()
	}
	if str != "Type0" {
		return noViolation
	}
	encoding := dict.Get("Encoding")
	if _, ok := core.GetName(encoding); ok {
		return noViolation
	}
	encodingStream, ok := core.GetStream(encoding)
	if !ok {
		return newViolatedRule(s, detail)
	}
	cm, err := loadCMap(encodingStream, streamData, cmaps)
	if err != nil {
		return newViolatedRule(s, detail)
	}
	wMode, ok2 := core.GetIntVal(encodingStream.Get("WMode"))
	n, found := cm.WMode()
	if ok2 && found {
		if n != wMode {
			return newViolatedRule(s, detail)
		}
	}
	if (ok2 && !found) || (!ok2 && found) {
		return newViolatedRule(s, detail)
	}
	return noViolation
}

func verifyEmbeddedFilesPDFA4(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	objectNums := r.GetObjectNums()
	for _, objectNum := range objectNums {
		obj, err := r.GetIndirectObjectByNumber(objectNum)
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
		if typeName.String() != "Filespec" {
			continue
		}
		filespec, err := model.NewPdfFilespecFromObj(dict)
		if err != nil {
			continue
		}
		if filespec.EF != nil {
			if filespec.F == nil || filespec.UF == nil {
				violations = append(violations, newViolatedRule("6.9-2", "The file specification dictionary for an embedded file shall contain the F and UF keys."))
				break
			}
			if filespec.AFRelationship == nil {
				violations = append(violations, newViolatedRule("6.9-4", "In order to enable identification of the relationship between the file specification dictionary and the content that is referring to it, a new (required) key has been defined and its presence (in the dictionary) is required."))
				break
			}
		}
	}
	return violations
}

func verifyCMapWModePDFA1(dict *core.PdfObjectDictionary, streamData map[*core.PdfObjectStream][]byte, cmaps map[*core.PdfObjectStream]*cmap.CMap) ViolatedRule {
	const (
		ruleNo = "6.3.3-4"
		detail = "For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream."
	)
	var s string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		s = subtypeName.String()
	}
	if s != "Type0" {
		return noViolation
	}
	encoding := dict.Get("Encoding")
	if _, ok := core.GetName(encoding); ok {
		return noViolation
	}
	encodingStream, ok := core.GetStream(encoding)
	if !ok {
		return newViolatedRule(ruleNo, detail)
	}
	cm, err := loadCMap(encodingStream, streamData, cmaps)
	if err != nil {
		return newViolatedRule(ruleNo, detail)
	}
	wMode, ok2 := core.GetIntVal(encodingStream.Get("WMode"))
	n, found := cm.WMode()
	if ok2 && found {
		if n != wMode {
			return newViolatedRule(ruleNo, detail)
		}
	}
	if (ok2 && !found) || (!ok2 && found) {
		return newViolatedRule(ruleNo, detail)
	}
	return noViolation
}

func verifyContentStreamOperatorsPDFA1(r *model.CompliancePdfReader) ViolatedRule {
	for _, page := range r.PageList {
		contentStreams, err := page.GetContentStreams()
		if err != nil {
			continue
		}
		for _, contentStream := range contentStreams {
			contentStreamParser := contentstream.NewContentStreamParser(contentStream)
			_, err = contentStreamParser.Parse()
			if err != nil {
				return newViolatedRule("6.2.10-1", "A content stream shall not contain any operators not defined in PDF Reference even if such operators are bracketed by the BX/EX compatibility operators.")
			}
		}
	}
	return noViolation
}

func verifyEmbeddedFilesPDFA3(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	objectNums := r.GetObjectNums()
	for _, objectNum := range objectNums {
		obj, err := r.GetIndirectObjectByNumber(objectNum)
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
		if typeName.String() != "Filespec" {
			continue
		}
		filespec, err := model.NewPdfFilespecFromObj(dict)
		if err != nil {
			continue
		}
		if filespec.EF != nil {
			if filespec.F == nil || filespec.UF == nil {
				violations = append(violations, newViolatedRule("6.8-2", "The file specification dictionary for an embedded file shall contain the F and UF keys."))
				break
			}
			if filespec.AFRelationship == nil {
				violations = append(violations, newViolatedRule("6.8-3", "In order to enable identification of the relationship between the file specification dictionary and the content that is referring to it, a new (required) key has been defined and its presence (in the dictionary) is required."))
				break
			}
		}
	}
	return violations
}

func verifyCMapsEmbedded(dict *core.PdfObjectDictionary, streamData map[*core.PdfObjectStream][]byte, cmaps map[*core.PdfObjectStream]*cmap.CMap, clause string) ViolatedRule {
	s := clause + ".3-3"
	const (
		detail = "All CMaps used within a PDF/A-2 file, except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118, shall be embedded in that file as described in ISO 32000-1:2008, 9.7.5."
	)
	var str string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		str = subtypeName.String()
	}
	if str != "Type0" {
		return noViolation
	}
	encoding := dict.Get("Encoding")
	if encodingName, ok := core.GetName(encoding); ok {
		switch encodingName.String() {
		case "Identity-H", "Identity-V":
			return noViolation
		default:
			return newViolatedRule(s, detail)
		}
	}
	encodingStream, ok := core.GetStream(encoding)
	if !ok {
		return newViolatedRule(s, detail)
	}
	_, err := loadCMap(encodingStream, streamData, cmaps)
	if err != nil {
		return newViolatedRule(s, detail)
	}
	return noViolation
}

func verifyCatalogAdditionalActionsPDFA1(r *model.CompliancePdfReader) (rule ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return noViolation
	}
	if dict.Get("AA") != nil {
		return newViolatedRule("6.6.2-3", "The document catalog dictionary shall not include an AA entry for an additional-actions dictionary.")
	}
	return noViolation
}

func verifyEmbeddedFilesPDFA2(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	objectNums := r.GetObjectNums()
	for _, objectNum := range objectNums {
		obj, err := r.GetIndirectObjectByNumber(objectNum)
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
		if typeName.String() != "Filespec" {
			continue
		}
		filespec, err := model.NewPdfFilespecFromObj(dict)
		if err != nil {
			continue
		}
		if filespec.EF != nil {
			if filespec.F == nil || filespec.UF == nil {
				violations = append(violations, newViolatedRule("6.8-2", "The file specification dictionary for an embedded file shall contain the F and UF keys."))
				break
			}
			if filespec.AFRelationship == nil {
				violations = append(violations, newViolatedRule("6.8-3", "In order to enable identification of the relationship between the file specification dictionary and the content that is referring to it, a new (required) key has been defined and its presence (in the dictionary) is required."))
				break
			}
			embeddedFile, err := model.NewEmbeddedFileFromObject(filespec.EF)
			if err != nil {
				continue
			}
			if strings.ToLower(embeddedFile.FileType) != "application/pdf" {
				violations = append(violations, newViolatedRule("6.8-4", "All embedded files should be a PDF file that conform to PDF/A-1 or PDF/A-2."))
				break
			}
		}
	}
	return violations
}

func verifyContentStreamOperators(r *model.CompliancePdfReader) ViolatedRule {
	for _, page := range r.PageList {
		contentStreams, err := page.GetContentStreams()
		if err != nil {
			continue
		}
		for _, contentStream := range contentStreams {
			contentStreamParser := contentstream.NewContentStreamParser(contentStream)
			_, err = contentStreamParser.Parse()
			if err != nil {
				return newViolatedRule("6.2.2-1", "A content stream shall not contain any operators not defined in PDF Reference even if such operators are bracketed by the BX/EX compatibility operators.")
			}
		}
	}
	return noViolation
}

func verifyCatalogAdditionalActionsPDFA4(r *model.CompliancePdfReader) (rule ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return noViolation
	}
	aaDict, ok := core.GetDict(dict.Get("AA"))
	if !ok {
		return noViolation
	}
	for _, name := range aaDict.Keys() {
		if name != "E" && name != "X" && name != "D" && name != "U" && name != "Fo" && name != "Bl" {
			return newViolatedRule("6.6.3-1", "If a document catalog dictionary includes an AA entry, its value (which is an additional-actions dictionary) shall only contain keys from the following list: E, X, D, U, Fo, and Bl")
		}
	}
	return noViolation
}

func verifyCMapsEmbeddedPDFA1(dict *core.PdfObjectDictionary, streamData map[*core.PdfObjectStream][]byte, cmaps map[*core.PdfObjectStream]*cmap.CMap) ViolatedRule {
	const (
		ruleNo = "6.3.3-3"
		detail = "All CMaps used within a conforming file, except Identity-H and Identity-V, shall be embedded in that file as described in PDF Reference 5.6.4."
	)
	var s string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		s = subtypeName.String()
	}
	if s != "Type0" {
		return noViolation
	}
	encoding := dict.Get("Encoding")
	if encodingName, ok := core.GetName(encoding); ok {
		switch encodingName.String() {
		case "Identity-H", "Identity-V":
			return noViolation
		default:
			return newViolatedRule(ruleNo, detail)
		}
	}
	encodingStream, ok := core.GetStream(encoding)
	if !ok {
		return newViolatedRule(ruleNo, detail)
	}
	_, err := loadCMap(encodingStream, streamData, cmaps)
	if err != nil {
		return newViolatedRule(ruleNo, detail)
	}
	return noViolation
}

func verifyCharSetPDFA1(font *model.PdfFont, dict *core.PdfObjectDictionary) ViolatedRule {
	const (
		ruleNo = "6.3.5-2"
		detail = "For all Type 1 font subsets referenced within a conforming file, the font descriptor dictionary shall include a CharSet string listing the character names defined in the font subset, as described in PDF Reference Table 5.18."
	)
	var s string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		s = subtypeName.String()
	}
	if s != "Type1" {
		return noViolation
	}
	if fonts.IsStdFont(fonts.StdFontName(font.BaseFont())) {
		return noViolation
	}
	pdfFontDescriptor := font.FontDescriptor()
	if pdfFontDescriptor.CharSet == nil {
		return newViolatedRule(ruleNo, detail)
	}
	return noViolation
}

func verifyCatalogAdditionalActionsPDFA23(r *model.CompliancePdfReader) (rule ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return noViolation
	}
	if dict.Get("AA") != nil {
		return newViolatedRule("6.5.2-1", "The document catalog shall not include an AA entry for an additional-actions dictionary.")
	}
	return noViolation
}

func verifyAlternatePresentations(r *model.CompliancePdfReader, clause string) (violations []ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return violations
	}
	namesDict, ok := core.GetDict(dict.Get("Names"))
	if !ok {
		return violations
	}
	if namesDict.Get("AlternatePresentations") != nil {
		violations = append(violations, newViolatedRule(clause+"-1", "There shall be no AlternatePresentations entry in the document's name dictionary."))
	}
	return violations
}

func verifyEmbeddedFilesPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	objectNums := r.GetObjectNums()
	for _, objectNum := range objectNums {
		obj, err := r.GetIndirectObjectByNumber(objectNum)
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
		if typeName.String() != "Filespec" {
			continue
		}
		if dict.Get("EF") != nil {
			violations = append(violations, newViolatedRule("6.1.11-1", "A file specification dictionary, as defined in PDF 3.10.2, shall not contain the EF key."))
			break
		}
	}
	dict, ok := catalogDict(r)
	if !ok {
		return violations
	}
	namesDict, ok := core.GetDict(dict.Get("Names"))
	if !ok {
		return violations
	}
	if namesDict.Get("EmbeddedFiles") != nil {
		violations = append(violations, newViolatedRule("6.1.11-2", "A file's name dictionary, as defined in PDF Reference 3.6.3, shall not contain the EmbeddedFiles key."))
	}
	return violations
}
