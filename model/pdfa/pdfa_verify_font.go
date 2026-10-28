package pdfa

import (
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
)

func verifyFontDictionariesPDFA1(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_3_2_1, reported6_3_2_2, reported6_3_2_3, reported6_3_2_4, reported6_3_2_5, reported6_3_2_6, ok bool
	allReported := func() bool {
		return reported6_3_2_1 && reported6_3_2_2 && reported6_3_2_3 && reported6_3_2_4 && reported6_3_2_5 && reported6_3_2_6 && ok
	}
	for _, page := range r.PageList {
		if page.Resources == nil {
			continue
		}
		dict, ok := core.GetDict(page.Resources.Font)
		if !ok {
			continue
		}
		for _, name := range dict.Keys() {
			dict2, ok := core.GetDict(dict.Get(name))
			if !ok {
				if !reported6_3_2_1 {
					violations = append(violations, newViolatedRule("6.3.2-1", "All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5."))
					reported6_3_2_1 = true
					if allReported() {
						return violations
					}
				}
				continue
			}
			if typeName, ok := core.GetName(dict2.Get("Type")); !reported6_3_2_1 && (!ok || typeName.String() != "Font") {
				violations = append(violations, newViolatedRule("6.3.2-1", "Type - name - (Required) The type of PDF object that this dictionary describes; must be Font for a font dictionary."))
				reported6_3_2_1 = true
				if allReported() {
					return violations
				}
			}
			font, err := model.NewPdfFontFromPdfObject(dict2)
			if err != nil {
				continue
			}
			var s string
			if subtypeName, ok := core.GetName(dict2.Get("Subtype")); ok {
				s = subtypeName.String()
			}
			if !reported6_3_2_2 {
				switch s {
				case "Type0", "Type1", "MMType1", "TrueType", "CIDFontType0", "CIDFontType2":
				default:
					reported6_3_2_2 = true
					violations = append(violations, newViolatedRule("6.3.2-2", "Subtype - name - (Required) The type of font; must be \"Type1\" for Type 1 fonts, \"MMType1\" for multiple master fonts, \"TrueType\" for TrueType fonts \"Type3\" for Type 3 fonts, \"Type0\" for Type 0 fonts and \"CIDFontType0\" or \"CIDFontType2\" for CID fonts."))
					if allReported() {
						return violations
					}
				}
			}
			if !reported6_3_2_3 {
				if s != "Type3" {
					baseFontName, ok := core.GetName(dict2.Get("BaseFont"))
					if !ok || baseFontName.String() == "" {
						violations = append(violations, newViolatedRule("6.3.2-3", "BaseFont - name - (Required) The PostScript name of the font."))
						reported6_3_2_3 = true
						if allReported() {
							return violations
						}
					}
				}
			}
			if s != "Type1" {
				continue
			}
			found := fonts.IsStdFont(fonts.StdFontName(font.BaseFont()))
			if found {
				continue
			}
			firstChar, ok2 := core.GetIntVal(dict2.Get("FirstChar"))
			if !ok2 && !reported6_3_2_4 {
				violations = append(violations, newViolatedRule("6.3.2-4", "FirstChar - integer - (Required except for the standard 14 fonts) The first character code defined in the font's Widths array."))
				reported6_3_2_4 = true
				if allReported() {
					return violations
				}
			}
			lastChar, ok3 := core.GetIntVal(dict2.Get("LastChar"))
			if !ok3 && !reported6_3_2_5 {
				violations = append(violations, newViolatedRule("6.3.2-5", "LastChar - integer - (Required except for the standard 14 fonts) The last character code defined in the font's Widths array."))
				reported6_3_2_5 = true
				if allReported() {
					return violations
				}
			}
			if !reported6_3_2_6 {
				widthsArray, ok := core.GetArray(dict2.Get("Widths"))
				if !ok || !ok2 || !ok3 || widthsArray.Len() != lastChar-firstChar+1 {
					violations = append(violations, newViolatedRule("6.3.2-6", "Widths - array - (Required except for the standard 14 fonts; indirect reference preferred) An array of (LastChar − FirstChar + 1) widths."))
					reported6_3_2_6 = true
					if allReported() {
						return violations
					}
				}
			}
		}
	}
	return violations
}

func verifyFontDictionaries(r *model.CompliancePdfReader, clause string) (violations []ViolatedRule) {
	var reportedType, reportedSubtype, reportedBaseFont, reportedFirstChar, reportedLastChar, reportedWidths bool
	allReported := func() bool {
		return reportedType && reportedSubtype && reportedBaseFont && reportedFirstChar && reportedLastChar && reportedWidths
	}
	for _, page := range r.PageList {
		if page.Resources == nil {
			continue
		}
		dict, ok := core.GetDict(page.Resources.Font)
		if !ok {
			continue
		}
		for _, name := range dict.Keys() {
			dict2, ok := core.GetDict(dict.Get(name))
			if !ok {
				if !reportedType {
					violations = append(violations, newViolatedRule(clause+"-1", "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7."))
					reportedType = true
					if allReported() {
						return violations
					}
				}
				continue
			}
			if typeName, ok := core.GetName(dict2.Get("Type")); !reportedType && (!ok || typeName.String() != "Font") {
				violations = append(violations, newViolatedRule(clause+"-1", "Type - name - (Required) The type of PDF object that this dictionary describes; must be Font for a font dictionary."))
				reportedType = true
				if allReported() {
					return violations
				}
			}
			font, err := model.NewPdfFontFromPdfObject(dict2)
			if err != nil {
				continue
			}
			var s string
			if subtypeName, ok := core.GetName(dict2.Get("Subtype")); ok {
				s = subtypeName.String()
			}
			if !reportedSubtype {
				switch s {
				case "Type0", "Type1", "MMType1", "TrueType", "CIDFontType0", "CIDFontType2":
				default:
					reportedSubtype = true
					violations = append(violations, newViolatedRule(clause+"-2", "Subtype - name - (Required) The type of font; must be \"Type1\" for Type 1 fonts, \"MMType1\" for multiple master fonts, \"TrueType\" for TrueType fonts \"Type3\" for Type 3 fonts, \"Type0\" for Type 0 fonts and \"CIDFontType0\" or \"CIDFontType2\" for CID fonts."))
					if allReported() {
						return violations
					}
				}
			}
			if !reportedBaseFont {
				if s != "Type3" {
					baseFontName, ok := core.GetName(dict2.Get("BaseFont"))
					if !ok || baseFontName.String() == "" {
						violations = append(violations, newViolatedRule(clause+"-3", "BaseFont - name - (Required) The PostScript name of the font."))
						reportedBaseFont = true
						if allReported() {
							return violations
						}
					}
				}
			}
			if s != "Type1" {
				continue
			}
			found := fonts.IsStdFont(fonts.StdFontName(font.BaseFont()))
			if found {
				continue
			}
			firstChar, ok2 := core.GetIntVal(dict2.Get("FirstChar"))
			if !ok2 && !reportedFirstChar {
				violations = append(violations, newViolatedRule(clause+"-4", "FirstChar - integer - (Required except for the standard 14 fonts) The first character code defined in the font's Widths array."))
				reportedFirstChar = true
				if allReported() {
					return violations
				}
			}
			lastChar, ok3 := core.GetIntVal(dict2.Get("LastChar"))
			if !ok3 && !reportedLastChar {
				violations = append(violations, newViolatedRule(clause+"-5", "LastChar - integer - (Required except for the standard 14 fonts) The last character code defined in the font's Widths array."))
				reportedLastChar = true
				if allReported() {
					return violations
				}
			}
			if !reportedWidths {
				widthsArray, ok := core.GetArray(dict2.Get("Widths"))
				if !ok || !ok2 || !ok3 || widthsArray.Len() != lastChar-firstChar+1 {
					violations = append(violations, newViolatedRule(clause+"-6", "Widths - array - (Required except for the standard 14 fonts; indirect reference preferred) An array of (LastChar − FirstChar + 1) widths."))
					reportedWidths = true
					if allReported() {
						return violations
					}
				}
			}
		}
	}
	return violations
}

func verifyFontEmbeddedPDFA1(font *model.PdfFont, dict *core.PdfObjectDictionary, invisibleText bool) ViolatedRule {
	const (
		ruleNo = "6.3.4-1"
		detail = "The font programs for all fonts used within a conforming file shall be embedded within that file, as defined in PDF Reference 5.8, except when the fonts are used exclusively with text rendering mode 3."
	)
	if invisibleText {
		return noViolation
	}
	pdfFontDescriptor := font.FontDescriptor()
	var s string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		s = subtypeName.String()
	}
	switch s {
	case "Type1":
		if pdfFontDescriptor.FontFile == nil {
			return newViolatedRule(ruleNo, detail)
		}
	case "TrueType":
		if pdfFontDescriptor.FontFile2 == nil {
			return newViolatedRule(ruleNo, detail)
		}
	case "Type0", "Type3":
	default:
		if pdfFontDescriptor.FontFile3 == nil {
			return newViolatedRule(ruleNo, detail)
		}
	}
	return noViolation
}

func verifyFontEmbedded(font *model.PdfFont, dict *core.PdfObjectDictionary, invisibleText bool, clause string) ViolatedRule {
	s := clause + ".4-1"
	const (
		detail = "The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-1:2008, 9.9."
	)
	if invisibleText {
		return noViolation
	}
	pdfFontDescriptor := font.FontDescriptor()
	var str string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		str = subtypeName.String()
	}
	switch str {
	case "Type1":
		if pdfFontDescriptor.FontFile == nil {
			return newViolatedRule(s, detail)
		}
	case "TrueType":
		if pdfFontDescriptor.FontFile2 == nil {
			return newViolatedRule(s, detail)
		}
	case "Type0", "Type3":
	default:
		if pdfFontDescriptor.FontFile3 == nil {
			return newViolatedRule(s, detail)
		}
	}
	return noViolation
}
