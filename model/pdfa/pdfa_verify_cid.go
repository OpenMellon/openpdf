package pdfa

import (
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/model"
)

func verifyCIDSetPDFA1(font *model.PdfFont, dict *core.PdfObjectDictionary) ViolatedRule {
	const (
		ruleNo = "6.3.5-3"
		detail = "For all CIDFont subsets referenced within a conforming file, the font descriptor dictionary shall include a CIDSet stream identifying which CIDs are present in the embedded CIDFont file, as described in PDF Reference Table 5.20."
	)
	var s string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		s = subtypeName.String()
	}
	switch s {
	case "CIDFontType0", "CIDFontType2":
		pdfFontDescriptor := font.FontDescriptor()
		if pdfFontDescriptor.CIDSet == nil {
			return newViolatedRule(ruleNo, detail)
		}
		return noViolation
	default:
		return noViolation
	}
}

func verifyCIDToGIDMapPDFA1(dict *core.PdfObjectDictionary) ViolatedRule {
	const (
		ruleNo = "6.3.3-2"
		detail = "For all Type 2 CIDFonts that are used for rendering, the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in PDF Reference Table 5.13"
	)
	var s string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		s = subtypeName.String()
	}
	if s != "CIDFontType2" {
		return noViolation
	}
	if dict.Get("CIDToGIDMap") == nil {
		return newViolatedRule(ruleNo, detail)
	}
	return noViolation
}

func verifyCIDSystemInfoPDFA1(dict *core.PdfObjectDictionary, streamData map[*core.PdfObjectStream][]byte, cmaps map[*core.PdfObjectStream]*cmap.CMap) ViolatedRule {
	const (
		detail = "For any given composite (Type 0) font referenced within a conforming file, the CIDSystemInfo entries of its CIDFont and CMap dictionaries shall be compatible. In other words, the Registry and Ordering strings of the CIDSystemInfo dictionaries for that font shall be identical, unless the value of the Encoding key in the font dictionary is Identity-H or Identity-V."
		ruleNo = "6.3.3-1"
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
		}
		cm, err := cmap.LoadPredefinedCMap(encodingName.String())
		if err != nil {
			return newViolatedRule(ruleNo, detail)
		}
		cidSystemInfo := cm.CIDSystemInfo()
		if cidSystemInfo.Ordering != cidSystemInfo.Registry {
			return newViolatedRule(ruleNo, detail)
		}
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
	cidSystemInfo := cm.CIDSystemInfo()
	if cidSystemInfo.Ordering != cidSystemInfo.Registry {
		return newViolatedRule(ruleNo, detail)
	}
	return noViolation
}

func verifyCIDToGIDMap(dict *core.PdfObjectDictionary, clause string) ViolatedRule {
	s := clause + ".3-2"
	const (
		detail = "ISO 32000-1:2008, 9.7.4, Table 117 requires that all embedded Type 2 CIDFonts in the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-1:2008, 9.7.4, Table 117."
	)
	var str string
	if subtypeName, ok := core.GetName(dict.Get("Subtype")); ok {
		str = subtypeName.String()
	}
	if str != "CIDFontType2" {
		return noViolation
	}
	if dict.Get("CIDToGIDMap") == nil {
		return newViolatedRule(s, detail)
	}
	return noViolation
}

func verifyCIDSystemInfo(dict *core.PdfObjectDictionary, streamData map[*core.PdfObjectStream][]byte, cmaps map[*core.PdfObjectStream]*cmap.CMap, clause string) ViolatedRule {
	s := clause + ".3-1"
	const (
		detail = "For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship. If the Encoding key in the Type 0 font dictionary is Identity-H or Identity-V, any values of Registry, Ordering, and Supplement may be used in the CIDSystemInfo entry of the CIDFont. Otherwise, the corresponding Registry and Ordering strings in both CIDSystemInfo dictionaries shall be identical, and the value of the Supplement key in the CIDSystemInfo dictionary of the CIDFont shall be greater than or equal to the Supplement key in the CIDSystemInfo dictionary of the CMap."
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
		}
		cm, err := cmap.LoadPredefinedCMap(encodingName.String())
		if err != nil {
			return newViolatedRule(s, detail)
		}
		cidSystemInfo := cm.CIDSystemInfo()
		if cidSystemInfo.Ordering != cidSystemInfo.Registry {
			return newViolatedRule(s, detail)
		}
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
	cidSystemInfo := cm.CIDSystemInfo()
	if cidSystemInfo.Ordering != cidSystemInfo.Registry {
		return newViolatedRule(s, detail)
	}
	return noViolation
}
