// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package sanitize

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

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
