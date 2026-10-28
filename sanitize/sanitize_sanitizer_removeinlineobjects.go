// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package sanitize

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

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
