package pdfa

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/model"
)

func verifyFonts(r *model.CompliancePdfReader, std standardType, clause string) (violations []ViolatedRule) {
	var cidSystemInfoReported, cidToGIDMapReported, cMapsEmbeddedReported, cMapWModeReported, fontEmbeddedReported, symbolicTrueTypeEncodingReported, toUnicodeReported bool
	allReported := func() bool {
		return cidSystemInfoReported && cidToGIDMapReported && cMapsEmbeddedReported && cMapWModeReported && fontEmbeddedReported && symbolicTrueTypeEncodingReported && toUnicodeReported
	}
	cmaps := map[*core.PdfObjectStream]*cmap.CMap{}
	streamData := map[*core.PdfObjectStream][]byte{}
	fontByObj := map[core.PdfObject]*model.PdfFont{}
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
		if *typeName != "Font" {
			continue
		}
		font, err := model.NewPdfFontFromPdfObject(dict)
		if err != nil {
			common.Log.Debug("getting font from object failed: %v", err)
			continue
		}
		fontByObj[dict] = font
	}
	for _, page := range r.PageList {
		contentStreams, err := page.GetContentStreams()
		if err != nil {
			common.Log.Debug("Getting page content streams failed")
			continue
		}
		for _, contentStream := range contentStreams {
			contentStreamParser := contentstream.NewContentStreamParser(contentStream)
			ops, err := contentStreamParser.Parse()
			if err != nil {
				common.Log.Debug("parsing contentstream failed: %v", err)
				continue
			}
			var invisibleText bool
			for _, op := range *ops {
				if op.Operand != "Tr" {
					continue
				}
				if len(op.Params) != 1 {
					common.Log.Debug("invalid number of parameters for the 'Tr' operand, expected '1' but is: '%d'", len(op.Params))
					continue
				}
				n, ok := core.GetIntVal(op.Params[0])
				if !ok {
					common.Log.Debug("rendering mode is not an integer")
					continue
				}
				if n == 3 {
					invisibleText = true
					break
				}
			}
			for _, op := range *ops {
				if op.Operand != "Tf" {
					continue
				}
				if len(op.Params) != 2 {
					common.Log.Debug("invalid number of parameters for the 'Tf' operand, expected '2' is: '%d'", len(op.Params))
					continue
				}
				name, ok := core.GetName(op.Params[0])
				if !ok {
					common.Log.Debug("ERROR: Tf op=%s GetNameVal failed", op)
					continue
				}
				fontByName, ok2 := page.Resources.GetFontByName(*name)
				if !ok2 {
					common.Log.Debug("font not found")
					continue
				}
				fontByDict, ok := core.GetDict(fontByName)
				if !ok {
					common.Log.Debug("font dict not found")
					continue
				}
				font, ok := fontByObj[fontByDict]
				if !ok {
					var err error
					font, err = model.NewPdfFontFromPdfObject(fontByDict)
					if err != nil {
						common.Log.Debug("getting font from object: %v", err)
						continue
					}
					fontByObj[fontByDict] = font
				}
				if !cidSystemInfoReported {
					rule := verifyCIDSystemInfo(fontByDict, streamData, cmaps, clause)
					if rule != noViolation {
						violations = append(violations, rule)
						cidSystemInfoReported = true
						if allReported() {
							return violations
						}
					}
				}
				if !cidToGIDMapReported {
					rule := verifyCIDToGIDMap(fontByDict, clause)
					if rule != noViolation {
						violations = append(violations, rule)
						cidToGIDMapReported = true
						if allReported() {
							return violations
						}
					}
				}
				if !cMapsEmbeddedReported {
					rule := verifyCMapsEmbedded(fontByDict, streamData, cmaps, clause)
					if rule != noViolation {
						violations = append(violations, rule)
						cMapsEmbeddedReported = true
						if allReported() {
							return violations
						}
					}
				}
				if !cMapWModeReported {
					rule := verifyCMapWMode(fontByDict, streamData, cmaps, clause)
					if rule != noViolation {
						violations = append(violations, rule)
						cMapWModeReported = true
						if allReported() {
							return violations
						}
					}
				}
				if !fontEmbeddedReported {
					rule := verifyFontEmbedded(font, fontByDict, invisibleText, clause)
					if rule != noViolation {
						fontEmbeddedReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
				if !symbolicTrueTypeEncodingReported {
					rule := verifySymbolicTrueTypeEncoding(font, fontByDict, clause)
					if rule != noViolation {
						symbolicTrueTypeEncodingReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
				if !toUnicodeReported && (std.conformance == "A" || std.conformance == "U" || std.part == 4) {
					rule := verifyToUnicode(fontByDict, streamData, cmaps, clause)
					if rule != noViolation {
						toUnicodeReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
			}
		}
	}
	return violations
}

func verifyFontsPDFA1(r *model.CompliancePdfReader, std standardType) (violations []ViolatedRule) {
	var cidSystemInfoReported, cidToGIDMapReported, cMapsEmbeddedReported, cMapWModeReported, fontEmbeddedReported, charSetReported, cidSetReported, nonSymbolicTrueTypeEncodingReported, symbolicTrueTypeEncodingReported, symbolicTrueTypeCmapReported, toUnicodeReported bool
	allReported := func() bool {
		return cidSystemInfoReported && cidToGIDMapReported && cMapsEmbeddedReported && cMapWModeReported && fontEmbeddedReported && charSetReported && cidSetReported && nonSymbolicTrueTypeEncodingReported && symbolicTrueTypeEncodingReported && symbolicTrueTypeCmapReported && toUnicodeReported
	}
	cmaps := map[*core.PdfObjectStream]*cmap.CMap{}
	streamData := map[*core.PdfObjectStream][]byte{}
	fontByObj := map[core.PdfObject]*model.PdfFont{}
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
		if *typeName != "Font" {
			continue
		}
		font, err := model.NewPdfFontFromPdfObject(dict)
		if err != nil {
			common.Log.Debug("getting font from object failed: %v", err)
			continue
		}
		fontByObj[dict] = font
	}
	for _, page := range r.PageList {
		contentStreams, err := page.GetContentStreams()
		if err != nil {
			common.Log.Debug("Getting page content streams failed")
			continue
		}
		for _, contentStream := range contentStreams {
			contentStreamParser := contentstream.NewContentStreamParser(contentStream)
			ops, err := contentStreamParser.Parse()
			if err != nil {
				common.Log.Debug("parsing contentstream failed: %v", err)
				continue
			}
			var invisibleText bool
			for _, op := range *ops {
				if op.Operand != "Tr" {
					continue
				}
				if len(op.Params) != 1 {
					common.Log.Debug("invalid number of parameters for the 'Tr' operand, expected '1' but is: '%d'", len(op.Params))
					continue
				}
				n, ok := core.GetIntVal(op.Params[0])
				if !ok {
					common.Log.Debug("rendering mode is not an integer")
					continue
				}
				if n == 3 {
					invisibleText = true
					break
				}
			}
			for _, op := range *ops {
				if op.Operand != "Tf" {
					continue
				}
				if len(op.Params) != 2 {
					common.Log.Debug("invalid number of parameters for the 'Tf' operand, expected '2' is: '%d'", len(op.Params))
					continue
				}
				name, ok := core.GetName(op.Params[0])
				if !ok {
					common.Log.Debug("ERROR: Tf op=%s GetNameVal failed", op)
					continue
				}
				fontByName, ok2 := page.Resources.GetFontByName(*name)
				if !ok2 {
					common.Log.Debug("font not found")
					continue
				}
				fontByDict, ok := core.GetDict(fontByName)
				if !ok {
					common.Log.Debug("font dict not found")
					continue
				}
				font, ok := fontByObj[fontByDict]
				if !ok {
					var err error
					font, err = model.NewPdfFontFromPdfObject(fontByDict)
					if err != nil {
						common.Log.Debug("getting font from object: %v", err)
						continue
					}
					fontByObj[fontByDict] = font
				}
				if !cidSystemInfoReported {
					rule := verifyCIDSystemInfoPDFA1(fontByDict, streamData, cmaps)
					if rule != noViolation {
						violations = append(violations, rule)
						cidSystemInfoReported = true
						if allReported() {
							return violations
						}
					}
				}
				if !cidToGIDMapReported {
					rule := verifyCIDToGIDMapPDFA1(fontByDict)
					if rule != noViolation {
						violations = append(violations, rule)
						cidToGIDMapReported = true
						if allReported() {
							return violations
						}
					}
				}
				if !cMapsEmbeddedReported {
					rule := verifyCMapsEmbeddedPDFA1(fontByDict, streamData, cmaps)
					if rule != noViolation {
						violations = append(violations, rule)
						cMapsEmbeddedReported = true
						if allReported() {
							return violations
						}
					}
				}
				if !cMapWModeReported {
					rule := verifyCMapWModePDFA1(fontByDict, streamData, cmaps)
					if rule != noViolation {
						violations = append(violations, rule)
						cMapWModeReported = true
						if allReported() {
							return violations
						}
					}
				}
				if !fontEmbeddedReported {
					rule := verifyFontEmbeddedPDFA1(font, fontByDict, invisibleText)
					if rule != noViolation {
						fontEmbeddedReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
				if !charSetReported {
					rule := verifyCharSetPDFA1(font, fontByDict)
					if rule != noViolation {
						charSetReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
				if !cidSetReported {
					rule := verifyCIDSetPDFA1(font, fontByDict)
					if rule != noViolation {
						cidSetReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
				if !nonSymbolicTrueTypeEncodingReported {
					rule := verifyNonSymbolicTrueTypeEncodingPDFA1(font, fontByDict)
					if rule != noViolation {
						nonSymbolicTrueTypeEncodingReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
				if !symbolicTrueTypeEncodingReported {
					rule := verifySymbolicTrueTypeEncodingPDFA1(font, fontByDict)
					if rule != noViolation {
						symbolicTrueTypeEncodingReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
				if !symbolicTrueTypeCmapReported {
					rule := verifySymbolicTrueTypeCmapPDFA1(font, fontByDict)
					if rule != noViolation {
						symbolicTrueTypeCmapReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
				if !toUnicodeReported && std.conformance == "A" {
					rule := verifyToUnicodePDFA1(fontByDict, streamData, cmaps)
					if rule != noViolation {
						toUnicodeReported = true
						violations = append(violations, rule)
						if allReported() {
							return violations
						}
					}
				}
			}
		}
	}
	return violations
}
