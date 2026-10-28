// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package sanitize

import (
	"github.com/OpenMellon/openpdf/common"
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

func (sanitizer *Sanitizer) processObjects(objects []core.PdfObject) ([]core.PdfObject, error) {
	options := sanitizer.options
	removed := map[core.PdfObject]struct{}{}
	visitedFiles := map[core.PdfObject]struct{}{}
	for _, object := range objects {
		switch value := object.(type) {
		case *core.PdfIndirectObject:
			dictionary, ok := core.GetDict(value)
			if ok {
				if objectType, ok := core.GetName(dictionary.Get("Type")); ok && *objectType == "Catalog" {
					if openAction := dictionary.Get("OpenAction"); isPresentNonNull(openAction) && options.OpenAction {
						if action, ok := core.GetDict(openAction); ok {
							if kind, ok := actionType(action); ok {
								collectActionPayloads(kind, action, removed)
							}
						}
						dictionary.Remove("OpenAction")
					}
					if options.EmbeddedFiles {
						removeEmbeddedFilesNameTree(dictionary, removed, visitedFiles)
					}
				} else if kind, ok := actionType(dictionary); ok {
					if kind == "Rendition" {
						if options.RenditionJS {
							clearJavaScript(dictionary)
						}
					} else if options.dropsAction(kind) {
						collectActionPayloads(kind, dictionary, removed)
						if common.Log.IsLogLevel(common.LogLevelDebug) {
							common.Log.Debug("%s action skipped.", kind)
						}
						removed[value] = struct{}{}
					}
				} else if javascriptTree := dictionary.Get("JavaScript"); javascriptTree != nil && options.JavaScript {
					collectEmbeddedFileTree(javascriptTree, removed, visitedFiles)
					dictionary.Remove("JavaScript")
				} else if objectType, ok := core.GetName(dictionary.Get("Type")); ok && *objectType == "Annot" && options.JavaScript {
					if parent, ok := core.GetIndirect(dictionary.Get("Parent")); ok {
						if parentDict, ok := core.GetDict(parent.PdfObject); ok {
							if additionalActions, ok := core.GetDict(parentDict.Get("AA")); ok {
								keystroke, ok := core.GetIndirect(additionalActions.Get("K"))
								if ok {
									if keystrokeDict, ok := core.GetDict(keystroke.PdfObject); ok {
										if kind, ok := core.GetName(keystrokeDict.Get("S")); ok && *kind == "JavaScript" {
											keystrokeDict.Clear()
										} else if formatAction := additionalActions.Get("F"); formatAction != nil {
											if formatObject, ok := core.GetIndirect(formatAction); ok {
												if formatDict, ok := core.GetDict(formatObject.PdfObject); ok {
													if kind, ok := core.GetName(formatDict.Get("S")); ok && *kind == "JavaScript" {
														formatDict.Clear()
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
				if options.EmbeddedFiles {
					if subtype, ok := core.GetName(dictionary.Get("Subtype")); ok && *subtype == "FileAttachment" {
						if fileSpec, ok := core.GetDict(dictionary.Get("FS")); ok {
							stripFileSpecStreams(fileSpec, removed)
						}
						if popup, ok := core.GetIndirect(dictionary.Get("Popup")); ok {
							removed[popup] = struct{}{}
						}
						common.Log.Debug("FileAttachment annotation skipped.")
						removed[value] = struct{}{}
					} else if dictionary.Get("EF") != nil || dictionary.Get("RF") != nil {
						stripFileSpecStreams(dictionary, removed)
					}
				}
			}
		case *core.PdfObjectStream:
			if options.EmbeddedFiles {
				if objectType, ok := core.GetName(value.Get("Type")); ok && *objectType == "EmbeddedFile" {
					common.Log.Debug("Embedded file stream skipped.")
					removed[value] = struct{}{}
				}
			}
		case *core.PdfObjectStreams:
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("pdf object streams type %T", value)
			}
		default:
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("unknown pdf object %T", value)
			}
		}
	}
	nameArrays := map[*core.PdfObjectArray]struct{}{}
	needsInlineScan := options.dropsAnyAction() || options.EmbeddedFiles || options.RenditionJS
	if needsInlineScan || len(removed) > 0 {
		visitedArrays := map[core.PdfObject]struct{}{}
		for _, object := range objects {
			if _, skip := removed[object]; skip {
				continue
			}
			switch value := object.(type) {
			case *core.PdfIndirectObject:
				collectNameTreeArrays(value.PdfObject, nameArrays, visitedArrays)
			case *core.PdfObjectStream:
				collectNameTreeArrays(value.PdfObjectDictionary, nameArrays, visitedArrays)
			}
		}
	}
	if needsInlineScan {
		visitedInline := map[core.PdfObject]struct{}{}
		for _, object := range objects {
			if _, skip := removed[object]; skip {
				continue
			}
			switch value := object.(type) {
			case *core.PdfIndirectObject:
				sanitizer.removeInlineObjects(value.PdfObject, removed, nameArrays, visitedInline)
			case *core.PdfObjectStream:
				sanitizer.removeInlineObjects(value.PdfObjectDictionary, removed, nameArrays, visitedInline)
			}
		}
	}
	if len(removed) > 0 {
		visitedRemoval := map[core.PdfObject]struct{}{}
		for _, object := range objects {
			if _, skip := removed[object]; skip {
				continue
			}
			switch value := object.(type) {
			case *core.PdfIndirectObject:
				removeCollectedObjects(value.PdfObject, removed, nameArrays, visitedRemoval)
			case *core.PdfObjectStream:
				removeCollectedObjects(value.PdfObjectDictionary, removed, nameArrays, visitedRemoval)
			}
		}
	}
	kept := make([]core.PdfObject, 0, len(objects))
	for _, object := range objects {
		if _, skip := removed[object]; skip {
			continue
		}
		kept = append(kept, object)
	}
	sanitizer.analyze(kept)
	return kept, nil
}

func collectEmbeddedFileTree(object core.PdfObject, collected map[core.PdfObject]struct{}, visited map[core.PdfObject]struct{}) {
	if object == nil {
		return
	}
	if _, seen := visited[object]; seen {
		return
	}
	visited[object] = struct{}{}
	dictionary, ok := core.GetDict(object)
	if !ok {
		return
	}
	if dictionary.Get("Kids") != nil || dictionary.Get("Names") != nil {
		if indirect, ok := object.(*core.PdfIndirectObject); ok {
			collected[indirect] = struct{}{}
		}
		if kids, ok := core.GetArray(dictionary.Get("Kids")); ok {
			for _, child := range kids.Elements() {
				collectEmbeddedFileTree(child, collected, visited)
			}
		}
		if names, ok := core.GetArray(dictionary.Get("Names")); ok {
			for _, entry := range names.Elements() {
				collectEmbeddedFileTree(entry, collected, visited)
			}
		}
		return
	}
	stripFileSpecStreams(dictionary, collected)
}

// New returns a new sanitizer object.
func New(opts SanitizationOpts) *Sanitizer { return &Sanitizer{options: opts} }

// GetSuspiciousObjects returns a count of each detected suspicious object.
func (sanitizer *Sanitizer) GetSuspiciousObjects() map[string]int {
	return sanitizer.suspiciousCounts
}

func isPresentNonNull(object core.PdfObject) bool {
	if object == nil {
		return false
	}
	_, isNull := object.(*core.PdfObjectNull)
	return !isNull
}

func stripFileSpecStreams(dictionary *core.PdfObjectDictionary, collected map[core.PdfObject]struct{}) {
	if embedded := dictionary.Get("EF"); embedded != nil {
		if embeddedDict, ok := core.GetDict(embedded); ok {
			for _, key := range embeddedDict.Keys() {
				if stream, ok := core.GetStream(embeddedDict.Get(key)); ok {
					collected[stream] = struct{}{}
				}
			}
		}
		dictionary.Remove("EF")
	}
	if related := dictionary.Get("RF"); related != nil {
		if relatedDict, ok := core.GetDict(related); ok {
			for _, key := range relatedDict.Keys() {
				if array, ok := core.GetArray(relatedDict.Get(key)); ok {
					for _, item := range array.Elements() {
						if stream, ok := core.GetStream(item); ok {
							collected[stream] = struct{}{}
						}
					}
				}
			}
		}
		dictionary.Remove("RF")
	}
}

func (sanitizer *Sanitizer) sanitizeInlineAction(object core.PdfObject, collected map[core.PdfObject]struct{}) bool {
	dictionary, ok := object.(*core.PdfObjectDictionary)
	if !ok {
		return false
	}
	kind, ok := actionType(dictionary)
	if !ok {
		return false
	}
	if kind == "Rendition" {
		if sanitizer.options.RenditionJS {
			clearJavaScript(dictionary)
		}
		return false
	}
	if _, known := knownActionTypes[kind]; known && sanitizer.options.dropsAction(kind) {
		collectActionPayloads(kind, dictionary, collected)
		return true
	}
	return false
}

func clearJavaScript(dictionary *core.PdfObjectDictionary) {
	payload := dictionary.Get("JS")
	if stream, ok := core.GetStream(payload); ok {
		emptyStream, err := core.MakeStream([]byte{}, nil)
		if err == nil {
			*stream = *emptyStream
		}
	} else if text, ok := core.GetString(payload); ok {
		*text = *core.MakeString("")
	}
}

func countSuspiciousObjects(object core.PdfObject, counts map[string]int, visited map[core.PdfObject]struct{}) {
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
			if !countActionDictionary(child, counts) && key != "FS" {
				countEmbeddedFileSpec(child, counts)
			}
			countSuspiciousObjects(child, counts, visited)
		}
	case *core.PdfObjectArray:
		for _, child := range value.Elements() {
			if !countActionDictionary(child, counts) {
				countEmbeddedFileSpec(child, counts)
			}
			countSuspiciousObjects(child, counts, visited)
		}
	}
}

func actionType(dictionary *core.PdfObjectDictionary) (string, bool) {
	name, ok := core.GetName(dictionary.Get("S"))
	if !ok {
		return "", false
	}
	kind := name.String()
	switch kind {
	case "Sound":
		if typeName, ok := core.GetName(dictionary.Get("Type")); ok && *typeName != "Action" {
			return "", false
		}
		if dictionary.Get("Sound") == nil {
			return "", false
		}
	case "Movie":
		if typeName, ok := core.GetName(dictionary.Get("Type")); ok && *typeName != "Action" {
			return "", false
		}
		if dictionary.Get("P") != nil {
			return "", false
		}
	}
	return kind, true
}

func (opts SanitizationOpts) dropsAnyAction() bool {
	return opts.JavaScript || opts.URI || opts.GoTo || opts.GoToR || opts.Launch || opts.SubmitForm || opts.ImportData || opts.Sound || opts.Movie
}

func (sanitizer *Sanitizer) removeInlineObjects(object core.PdfObject, collected map[core.PdfObject]struct{}, nameArrays map[*core.PdfObjectArray]struct{}, visited map[core.PdfObject]struct{}) {
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
			if sanitizer.sanitizeInlineAction(child, collected) {
				value.Remove(key)
				continue
			}
			sanitizer.stripInlineFileSpec(child, collected)
			sanitizer.removeInlineObjects(child, collected, nameArrays, visited)
		}
	case *core.PdfObjectArray:
		if _, isNameArray := nameArrays[value]; isNameArray {
			for i, child := range value.Elements() {
				if sanitizer.sanitizeInlineAction(child, collected) {
					value.Set(i, core.MakeNull())
					continue
				}
				sanitizer.stripInlineFileSpec(child, collected)
				sanitizer.removeInlineObjects(child, collected, nameArrays, visited)
			}
			return
		}
		elements := value.Elements()
		kept := make([]core.PdfObject, 0, len(elements))
		for _, child := range elements {
			if sanitizer.sanitizeInlineAction(child, collected) {
				continue
			}
			sanitizer.stripInlineFileSpec(child, collected)
			sanitizer.removeInlineObjects(child, collected, nameArrays, visited)
			kept = append(kept, child)
		}
		if len(kept) != len(elements) {
			value.Clear()
			value.Append(kept...)
		}
	}
}

// SanitizationOpts specifies the objects to be removed during sanitization.
type SanitizationOpts struct {
	// JavaScript specifies whether JavaScript action should be removed. JavaScript Actions, section 12.6.4.16 of PDF32000_2008
	// JavaScript embedded in a Rendition action is not covered by this option; it is governed by RenditionJS.
	JavaScript bool

	// URI specifies if URI actions should be removed. 12.6.4.7 URI Actions, PDF32000_2008.
	URI bool

	// GoToR removes remote GoTo actions. 12.6.4.3 Remote Go-To Actions, PDF32000_2008.
	GoToR bool

	// GoTo specifies whether GoTo actions should be removed. 12.6.4.2 Go-To Actions, PDF32000_2008.
	GoTo bool

	// RenditionJS enables blanking of the `JS` entry of a Rendition Action, in both its text string and stream forms.
	// The `JS` entry has a value of text string or stream containing a JavaScript script that shall be executed when the action is triggered.
	// 12.6.4.13 Rendition Actions Table 214, PDF32000_2008.
	// The gate covers Rendition actions that are kept; the payload of an action that is being
	// dropped (for example a Rendition sub-action of a removed action) is blanked regardless.
	RenditionJS bool

	// OpenAction removes OpenAction entry from the document catalog.
	OpenAction bool

	// Launch specifies whether Launch Action should be removed.
	// A launch action launches an application or opens or prints a document.
	// 12.6.4.5 Launch Actions, PDF32000_2008.
	Launch bool

	// EmbeddedFiles specifies whether embedded files should be removed: the EmbeddedFiles
	// name tree in the document catalog (7.7.4), EF and RF entries in file specification
	// dictionaries (7.11.3, 7.11.4) and file attachment annotations with their popups
	// (12.5.6.15), PDF32000_2008. File specification dictionaries are kept, stripped of
	// their embedded content.
	EmbeddedFiles bool

	// SubmitForm specifies whether submit-form actions should be removed.
	// A submit-form action transmits the names and values of form fields to a specified URL.
	// 12.7.5.2 Submit-Form Action, PDF32000_2008.
	SubmitForm bool

	// ImportData specifies whether import-data actions should be removed.
	// An import-data action imports form data from an external FDF file into the document.
	// 12.7.5.4 Import-Data Action, PDF32000_2008.
	ImportData bool

	// Sound specifies whether sound actions should be removed.
	// A sound action plays a sound through the sound hardware (deprecated in PDF 2.0).
	// 12.6.4.8 Sound Actions, PDF32000_2008.
	Sound bool

	// Movie specifies whether movie actions should be removed.
	// A movie action plays a movie in a floating window or within the annotation rectangle
	// (deprecated in PDF 2.0). 12.6.4.9 Movie Actions, PDF32000_2008.
	Movie bool
}

func countEmbeddedFileSpec(object core.PdfObject, counts map[string]int) {
	if dictionary, ok := object.(*core.PdfObjectDictionary); ok && (dictionary.Get("EF") != nil || dictionary.Get("RF") != nil) {
		counts["EmbeddedFiles"]++
	}
}

func removeCollectedObjects(object core.PdfObject, collected map[core.PdfObject]struct{}, nameArrays map[*core.PdfObjectArray]struct{}, visited map[core.PdfObject]struct{}) {
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
			if _, remove := collected[child]; remove {
				value.Remove(key)
				continue
			}
			removeCollectedObjects(child, collected, nameArrays, visited)
		}
	case *core.PdfObjectArray:
		if _, isNameArray := nameArrays[value]; isNameArray {
			for i, child := range value.Elements() {
				if _, remove := collected[child]; remove {
					value.Set(i, core.MakeNull())
					continue
				}
				removeCollectedObjects(child, collected, nameArrays, visited)
			}
			return
		}
		elements := value.Elements()
		kept := make([]core.PdfObject, 0, len(elements))
		for _, child := range elements {
			if _, remove := collected[child]; remove {
				continue
			}
			removeCollectedObjects(child, collected, nameArrays, visited)
			kept = append(kept, child)
		}
		if len(kept) != len(elements) {
			value.Clear()
			value.Append(kept...)
		}
	}
}

func removeEmbeddedFilesNameTree(catalog *core.PdfObjectDictionary, collected map[core.PdfObject]struct{}, visited map[core.PdfObject]struct{}) {
	names, ok := core.GetDict(catalog.Get("Names"))
	if !ok {
		return
	}
	if embeddedFiles := names.Get("EmbeddedFiles"); embeddedFiles != nil {
		collectEmbeddedFileTree(embeddedFiles, collected, visited)
		names.Remove("EmbeddedFiles")
		common.Log.Debug("EmbeddedFiles name tree skipped.")
	}
}

// Sanitizer represents a sanitizer object.
// It implements the Optimizer interface to access the objects field from the writer.
type Sanitizer struct {
	options          SanitizationOpts
	suspiciousCounts map[string]int
}
