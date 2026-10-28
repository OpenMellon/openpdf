// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package sanitize

import (
	"github.com/OpenMellon/openpdf/core"
)

var knownActionTypes = map[string]struct{}{"JavaScript": {}, "URI": {}, "GoTo": {}, "GoToR": {}, "Launch": {}, "SubmitForm": {}, "ImportData": {}, "Sound": {}, "Movie": {}}

func (opts SanitizationOpts) dropsAction(kind string) bool {
	switch kind {
	case "JavaScript":
		return opts.JavaScript
	case "URI":
		return opts.URI
	case "GoTo":
		return opts.GoTo
	case "GoToR":
		return opts.GoToR
	case "Launch":
		return opts.Launch
	case "SubmitForm":
		return opts.SubmitForm
	case "ImportData":
		return opts.ImportData
	case "Sound":
		return opts.Sound
	case "Movie":
		return opts.Movie
	}
	return false
}

func collectChainedActionPayloads(object core.PdfObject, collected map[core.PdfObject]struct{}) {
	switch value := object.(type) {
	case *core.PdfObjectDictionary:
		if kind, ok := actionType(value); ok {
			collectActionPayloads(kind, value, collected)
		}
	case *core.PdfObjectArray:
		for _, element := range value.Elements() {
			if dictionary, ok := element.(*core.PdfObjectDictionary); ok {
				if kind, ok := actionType(dictionary); ok {
					collectActionPayloads(kind, dictionary, collected)
				}
			}
		}
	}
}

// Optimize optimizes `objects` and returns updated list of objects.
func (sanitizer *Sanitizer) Optimize(objects []core.PdfObject) ([]core.PdfObject, error) {
	return sanitizer.processObjects(objects)
}

func countNameTreeEntries(object core.PdfObject, visited map[core.PdfObject]struct{}) (otherCount, totalCount int) {
	if object == nil {
		return 0, 0
	}
	if _, seen := visited[object]; seen {
		return 0, 0
	}
	visited[object] = struct{}{}
	if _, isNull := core.TraceToDirectObject(object).(*core.PdfObjectNull); isNull {
		return 0, 1
	}
	dictionary, ok := core.GetDict(object)
	if !ok {
		return 1, 1
	}
	if _, isAction := actionType(dictionary); isAction {
		if _, suspicious := suspiciousActionType(dictionary); suspicious {
			return 0, 1
		}
		return 1, 1
	}
	kids, hasKids := core.GetArray(dictionary.Get("Kids"))
	names, hasNames := core.GetArray(dictionary.Get("Names"))
	if !hasKids && !hasNames {
		return 1, 1
	}
	if hasKids {
		for _, child := range kids.Elements() {
			childOther, childTotal := countNameTreeEntries(child, visited)
			otherCount += childOther
			totalCount += childTotal
		}
	}
	if hasNames {
		entries := names.Elements()
		for i := 1; i < len(entries); i += 2 {
			childOther, childTotal := countNameTreeEntries(entries[i], visited)
			otherCount += childOther
			totalCount += childTotal
		}
	}
	return otherCount, totalCount
}

func collectActionPayloads(kind string, dictionary *core.PdfObjectDictionary, collected map[core.PdfObject]struct{}) {
	switch kind {
	case "JavaScript", "Rendition":
		clearJavaScript(dictionary)
	case "Sound":
		if stream, ok := core.GetStream(dictionary.Get("Sound")); ok {
			collected[stream] = struct{}{}
		}
	}
	collectChainedActionPayloads(dictionary.Get("Next"), collected)
}

func (sanitizer *Sanitizer) stripInlineFileSpec(object core.PdfObject, collected map[core.PdfObject]struct{}) {
	if !sanitizer.options.EmbeddedFiles {
		return
	}
	if dictionary, ok := object.(*core.PdfObjectDictionary); ok && (dictionary.Get("EF") != nil || dictionary.Get("RF") != nil) {
		stripFileSpecStreams(dictionary, collected)
	}
}

func collectNameTreeArrays(object core.PdfObject, arrays map[*core.PdfObjectArray]struct{}, visited map[core.PdfObject]struct{}) {
	if object == nil {
		return
	}
	if _, seen := visited[object]; seen {
		return
	}
	visited[object] = struct{}{}
	switch value := object.(type) {
	case *core.PdfObjectDictionary:
		for _, key := range value.Keys() {
			child := value.Get(key)
			if key == "Names" || key == "Nums" {
				if array, ok := core.GetArray(child); ok {
					arrays[array] = struct{}{}
				}
			}
			collectNameTreeArrays(child, arrays, visited)
		}
	case *core.PdfObjectArray:
		for _, child := range value.Elements() {
			collectNameTreeArrays(child, arrays, visited)
		}
	}
}

func (sanitizer *Sanitizer) analyze(objects []core.PdfObject) {
	counts := map[string]int{}
	visited := map[core.PdfObject]struct{}{}
	attachmentSpecs := map[core.PdfObject]struct{}{}
	for _, object := range objects {
		indirect, ok := object.(*core.PdfIndirectObject)
		if !ok {
			continue
		}
		dictionary, ok := core.GetDict(indirect.PdfObject)
		if !ok {
			continue
		}
		if subtype, ok := core.GetName(dictionary.Get("Subtype")); ok && *subtype == "FileAttachment" {
			if fileSpec, ok := core.GetIndirect(dictionary.Get("FS")); ok {
				attachmentSpecs[fileSpec] = struct{}{}
			}
		}
	}
	for _, object := range objects {
		switch value := object.(type) {
		case *core.PdfObjectStream:
			countSuspiciousObjects(value.PdfObjectDictionary, counts, visited)
		case *core.PdfIndirectObject:
			countSuspiciousObjects(value.PdfObject, counts, visited)
			dictionary, ok := core.GetDict(value.PdfObject)
			if ok {
				if dictionary.Get("EF") != nil || dictionary.Get("RF") != nil {
					if _, isAttachmentSpec := attachmentSpecs[value]; !isAttachmentSpec {
						counts["EmbeddedFiles"]++
					}
				} else if subtype, ok := core.GetName(dictionary.Get("Subtype")); ok && *subtype == "FileAttachment" {
					counts["EmbeddedFiles"]++
				}
				if objectType, ok := core.GetName(dictionary.Get("Type")); ok && *objectType == "Catalog" {
					if isPresentNonNull(dictionary.Get("OpenAction")) {
						counts["OpenAction"]++
					}
				} else if _, isAction := actionType(dictionary); isAction {
					if kind, suspicious := suspiciousActionType(dictionary); suspicious {
						counts[kind]++
					}
				} else if javascriptTree := dictionary.Get("JavaScript"); javascriptTree != nil {
					otherCount, totalCount := countNameTreeEntries(javascriptTree, map[core.PdfObject]struct{}{})
					if totalCount == 0 {
						counts["JavaScript"]++
					} else {
						counts["JavaScript"] += otherCount
					}
				} else if parent, ok := core.GetIndirect(dictionary.Get("Parent")); ok {
					if parentDict, ok := core.GetDict(parent.PdfObject); ok {
						if additionalActions, ok := core.GetDict(parentDict.Get("AA")); ok {
							keystroke := additionalActions.Get("K")
							keystrokeIndirect, ok := core.GetIndirect(keystroke)
							if ok {
								if keystrokeDict, ok := core.GetDict(keystrokeIndirect.PdfObject); ok {
									if kind, ok := core.GetName(keystrokeDict.Get("S")); ok && *kind == "JavaScript" {
										counts["JavaScript"]++
									} else if _, hasJS := core.GetString(keystrokeDict.Get("JS")); hasJS {
										counts["JavaScript"]++
									} else {
										formatAction := additionalActions.Get("F")
										if formatAction != nil {
											formatIndirect, ok := core.GetIndirect(formatAction)
											if ok {
												if formatDict, ok := core.GetDict(formatIndirect.PdfObject); ok {
													if formatKind, ok := core.GetName(formatDict.Get("S")); ok {
														counts[formatKind.String()]++
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	sanitizer.suspiciousCounts = counts
}

func countActionDictionary(object core.PdfObject, counts map[string]int) bool {
	dictionary, ok := object.(*core.PdfObjectDictionary)
	if !ok {
		return false
	}
	if _, ok := actionType(dictionary); !ok {
		return false
	}
	if kind, ok := suspiciousActionType(dictionary); ok {
		counts[kind]++
	}
	return true
}

func hasJavaScriptPayload(dictionary *core.PdfObjectDictionary) bool {
	payload := dictionary.Get("JS")
	if _, ok := core.GetStream(payload); ok {
		return true
	}
	_, ok := core.GetString(payload)
	return ok
}

func suspiciousActionType(dictionary *core.PdfObjectDictionary) (string, bool) {
	kind, ok := actionType(dictionary)
	if !ok {
		return "", false
	}
	if _, ok := knownActionTypes[kind]; ok {
		return kind, true
	}
	if kind == "Rendition" && hasJavaScriptPayload(dictionary) {
		return "Rendition", true
	}
	return "", false
}
