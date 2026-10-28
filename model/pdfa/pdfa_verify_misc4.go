package pdfa

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/model"
)

func verifySymbolicTrueTypeEncoding(font *model.PdfFont, dict *core.PdfObjectDictionary, clause string) ViolatedRule {
	s := clause + ".6-3"
	const (
		detail = "All symbolic TrueType fonts shall not specify an Encoding entry in the font dictionary."
	)
	var str string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		str = subtypeName.String()
	}
	if str != "TrueType" {
		return noViolation
	}
	pdfFontDescriptor := font.FontDescriptor()
	n, ok := core.GetIntVal(pdfFontDescriptor.Flags)
	if !ok {
		common.Log.Debug("flags not defined for the font descriptor")
		return newViolatedRule(s, detail)
	}
	count := (uint32(n) >> 3) & 1
	found := count != 0
	if !found {
		return noViolation
	}
	if dict.Get("Encoding") != nil {
		return newViolatedRule(s, detail)
	}
	return noViolation
}

func verifyToUnicode(dict *core.PdfObjectDictionary, streamData map[*core.PdfObjectStream][]byte, cmaps map[*core.PdfObjectStream]*cmap.CMap, clause string) ViolatedRule {
	s := clause + ".7-1"
	const (
		detail = "The font dictionary shall include a ToUnicode entry whose value is a CMap stream object that maps character codes to Unicode values, as described in PDF Reference 5.9, unless the font meets any of the following three conditions:\n - fonts that use the predefined encodings MacRomanEncoding, MacExpertEncoding or WinAnsiEncoding, or that use the predefined Identity-H or Identity-V CMaps;\n - Type 1 fonts whose character names are taken from the Adobe standard Latin character set or the set of named characters in the Symbol font, as defined in PDF Reference Appendix D;\n - Type 0 fonts whose descendant CIDFont uses the Adobe-GB1, Adobe-CNS1, Adobe-Japan1 or Adobe-Korea1 character collections."
	)
	toUnicodeStream, ok := core.GetStream(dict.Get("ToUnicode"))
	if ok {
		_, err := loadCMap(toUnicodeStream, streamData, cmaps)
		if err != nil {
			return newViolatedRule(s, detail)
		}
		return noViolation
	}
	subtypeName, ok := core.GetName(dict.Get("Subtype"))
	if !ok {
		return newViolatedRule(s, detail)
	}
	switch subtypeName.String() {
	case "Type1":
		return noViolation
	}
	return newViolatedRule(s, detail)
}

func verifySymbolicTrueTypeEncodingPDFA1(font *model.PdfFont, dict *core.PdfObjectDictionary) ViolatedRule {
	const (
		ruleNo = "6.3.7-2"
		detail = "All symbolic TrueType fonts shall not specify an Encoding entry in the font dictionary."
	)
	var s string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		s = subtypeName.String()
	}
	if s != "TrueType" {
		return noViolation
	}
	pdfFontDescriptor := font.FontDescriptor()
	n, ok := core.GetIntVal(pdfFontDescriptor.Flags)
	if !ok {
		common.Log.Debug("flags not defined for the font descriptor")
		return newViolatedRule(ruleNo, detail)
	}
	count := (uint32(n) >> 3) & 1
	found := count != 0
	if !found {
		return noViolation
	}
	if dict.Get("Encoding") != nil {
		return newViolatedRule(ruleNo, detail)
	}
	return noViolation
}

func verifyTrailer(r *model.CompliancePdfReader) ViolatedRule {
	trailer, err := r.PdfReader.GetTrailer()
	if err != nil {
		return newViolatedRule("6.1.3-1", "missing trailer in the document")
	}
	if trailer.Get("ID") == nil {
		return newViolatedRule("6.1.3-1", "The file trailer dictionary shall contain the 'ID' keyword")
	}
	if trailer.Get("Encrypt") != nil {
		return newViolatedRule("6.1.3-2", "The keyword 'Encrypt' shall not be used in the trailer dictionary. ")
	}
	return noViolation
}

func verifyXrefSubsectionsPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	parserMetadata := r.ParserMetadata()
	if parserMetadata.HasInvalidSubsectionHeader() {
		violations = append(violations, newViolatedRule("6.1.4-1", "n a cross reference subsection header the starting object number and the range shall be separated by a single SPACE character (20h)."))
	}
	if parserMetadata.HasInvalidSeparationAfterXRef() {
		violations = append(violations, newViolatedRule("6.1.4-2", "The xref keyword and the cross reference subsection header shall be separated by a single EOL marker."))
	}
	return violations
}

func verifyXrefKeywordEOL(r *model.CompliancePdfReader) ViolatedRule {
	parserMetadata := r.ParserMetadata()
	if parserMetadata.HasInvalidSeparationAfterXRef() {
		return newViolatedRule("6.1.4-2", "The xref keyword and the cross reference subsection header shall be separated by a single EOL marker.")
	}
	return noViolation
}

func verifyTransparencyGroupPDFA1(r *model.CompliancePdfReader) ViolatedRule {
	seen := map[*core.PdfObjectStream]struct{}{}
	for _, page := range r.PageList {
		if page.Resources == nil && page.Contents == nil {
			continue
		}
		if pageDict := page.GetPageDict(); pageDict != nil {
			groupDict, ok := core.GetDict(pageDict.Get("Group"))
			if ok {
				if s := groupDict.Get("S"); s != nil {
					sName, ok := core.GetName(s)
					if ok && sName.String() == "Transparency" {
						return newViolatedRule("6.4-3", "A Group object with an S xObject with a value of Transparency shall not be included in a form XObject.\nA Group object with an S xObject with a value of Transparency shall not be included in a page dictionary.")
					}
				}
			}
		}
		if page.Resources != nil {
			if dict, ok := core.GetDict(page.Resources.XObject); ok {
				for _, name := range dict.Keys() {
					stream, ok := core.GetStream(dict.Get(name))
					if !ok {
						continue
					}
					if _, ok := seen[stream]; ok {
						continue
					}
					groupDict, ok := core.GetDict(stream.Get("Group"))
					if !ok {
						seen[stream] = struct{}{}
						continue
					}
					s := groupDict.Get("S")
					if s != nil {
						sName, ok := core.GetName(s)
						if ok && sName.String() == "Transparency" {
							return newViolatedRule("6.4-3", "A Group object with an S xObject with a value of Transparency shall not be included in a form XObject.\nA Group object with an S xObject with a value of Transparency shall not be included in a page dictionary.")
						}
					}
					seen[stream] = struct{}{}
					continue
				}
			}
		}
		if page.Contents != nil {
			contentStreams, err := page.GetContentStreams()
			if err != nil {
				continue
			}
			for _, contentStream := range contentStreams {
				ops, err := contentstream.NewContentStreamParser(contentStream).Parse()
				if err != nil {
					continue
				}
				for _, op := range *ops {
					if len(op.Params) == 0 {
						continue
					}
					name, ok := core.GetName(op.Params[0])
					if !ok {
						continue
					}
					xObjectByName, xObjectType := page.Resources.GetXObjectByName(*name)
					if xObjectType != model.XObjectTypeForm {
						continue
					}
					if _, ok := seen[xObjectByName]; ok {
						continue
					}
					groupDict, ok := core.GetDict(xObjectByName.Get("Group"))
					if !ok {
						seen[xObjectByName] = struct{}{}
						continue
					}
					s := groupDict.Get("S")
					if s != nil {
						sName, ok := core.GetName(s)
						if ok && sName.String() == "Transparency" {
							return newViolatedRule("6.4-3", "A Group object with an S xObject with a value of Transparency shall not be included in a form XObject.\nA Group object with an S xObject with a value of Transparency shall not be included in a page dictionary.")
						}
					}
					seen[xObjectByName] = struct{}{}
				}
			}
		}
	}
	return noViolation
}

func verifySymbolicTrueTypeCmapPDFA1(font *model.PdfFont, dict *core.PdfObjectDictionary) ViolatedRule {
	const (
		ruleNo = "6.3.7-3"
		detail = "Font programs' \"cmap\" tables for all symbolic TrueType fonts shall contain exactly one encoding."
	)
	var s string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		s = subtypeName.String()
	}
	if s != "TrueType" {
		return noViolation
	}
	pdfFontDescriptor := font.FontDescriptor()
	n, ok := core.GetIntVal(pdfFontDescriptor.Flags)
	if !ok {
		common.Log.Debug("flags not defined for the font descriptor")
		return newViolatedRule(ruleNo, detail)
	}
	found := (uint32(n) >> 3) != 0
	if !found {
		return noViolation
	}
	return noViolation
}

func verifyToUnicodePDFA1(dict *core.PdfObjectDictionary, streamData map[*core.PdfObjectStream][]byte, cmaps map[*core.PdfObjectStream]*cmap.CMap) ViolatedRule {
	const (
		ruleNo = "6.3.8-1"
		detail = "The font dictionary shall include a ToUnicode entry whose value is a CMap stream object that maps character codes to Unicode values, as described in PDF Reference 5.9, unless the font meets any of the following three conditions:\n - fonts that use the predefined encodings MacRomanEncoding, MacExpertEncoding or WinAnsiEncoding, or that use the predefined Identity-H or Identity-V CMaps;\n - Type 1 fonts whose character names are taken from the Adobe standard Latin character set or the set of named characters in the Symbol font, as defined in PDF Reference Appendix D;\n - Type 0 fonts whose descendant CIDFont uses the Adobe-GB1, Adobe-CNS1, Adobe-Japan1 or Adobe-Korea1 character collections."
	)
	encoding := dict.Get("Encoding")
	if encodingName, ok := core.GetName(encoding); ok {
		if encodingName.String() == "Identity-H" || encodingName.String() == "Identity-V" || encodingName.String() == "MacRomanEncoding" || encodingName.String() == "MacExpertEncoding" || encodingName.String() == "WinAnsiEncoding" {
			return noViolation
		}
	}
	toUnicodeStream, ok := core.GetStream(dict.Get("ToUnicode"))
	if ok {
		_, err := loadCMap(toUnicodeStream, streamData, cmaps)
		if err != nil {
			return newViolatedRule(ruleNo, detail)
		}
		return noViolation
	}
	subtypeName, ok := core.GetName(dict.Get("Subtype"))
	if !ok {
		return newViolatedRule(ruleNo, detail)
	}
	switch subtypeName.String() {
	case "Type1":
		return noViolation
	}
	return newViolatedRule(ruleNo, detail)
}
