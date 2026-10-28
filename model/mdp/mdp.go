// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package mdp

import (
	"errors"
	"fmt"

	"github.com/OpenMellon/openpdf/core"
)

// String returns the state of the warning.
func (d *DiffResult) String() string {
	return fmt.Sprintf("%s in revisions #%d", d.Description, d.Revision)
}

// IsPermitted returns true if changes permitted.
func (r *DiffResults) IsPermitted() bool { return len(r.Errors) == 0 }

func (p *defaultDiffPolicy) compareRevisions(oldParser *core.PdfParser, newParser *core.PdfParser) (*DiffResults, error) {
	var err error
	p.updatedObjects, err = newParser.GetUpdatedObjects(oldParser)
	if err != nil {
		return &DiffResults{}, err
	}
	if len(p.updatedObjects) == 0 {
		return &DiffResults{}, nil
	}
	revision := newParser.GetRevisionNumber()
	oldRootInd, oldOk := core.GetIndirect(core.ResolveReference(oldParser.GetTrailer().Get("Root")))
	newRootInd, newOk := core.GetIndirect(core.ResolveReference(newParser.GetTrailer().Get("Root")))
	if !oldOk || !newOk {
		return &DiffResults{}, errors.New("error while getting root object")
	}
	oldRoot, oldOk := core.GetDict(core.ResolveReference(oldRootInd.PdfObject))
	newRoot, newOk := core.GetDict(core.ResolveReference(newRootInd.PdfObject))
	if !oldOk || !newOk {
		return &DiffResults{}, errors.New("error while getting a root's dictionary")
	}
	if acroFormInd, hasAcroForm := core.GetIndirect(newRoot.Get("AcroForm")); hasAcroForm {
		acroForm, ok := core.GetDict(acroFormInd)
		if !ok {
			return &DiffResults{}, errors.New("error while getting AcroForm's dictionary")
		}
		oldFields := make([]core.PdfObject, 0)
		if oldAcroFormInd, hasOldAcro := core.GetIndirect(oldRoot.Get("AcroForm")); hasOldAcro {
			if oldAcroForm, isDict := core.GetDict(oldAcroFormInd); isDict {
				if oldFieldsArr, hasFields := core.GetArray(oldAcroForm.Get("Fields")); hasFields {
					oldFields = oldFieldsArr.Elements()
				}
			}
		}
		newFieldsArr, ok := core.GetArray(acroForm.Get("Fields"))
		if !ok {
			return &DiffResults{}, errors.New("error while getting AcroForm's fields")
		}
		if cmpErr := p.compareFields(revision, oldFields, newFieldsArr.Elements()); cmpErr != nil {
			return &DiffResults{}, cmpErr
		}
	}
	newPages, ok := core.GetIndirect(newRoot.Get("Pages"))
	if !ok {
		return &DiffResults{}, errors.New("error while getting pages' object")
	}
	oldPages, ok := core.GetIndirect(oldRoot.Get("Pages"))
	if !ok {
		return &DiffResults{}, errors.New("error while getting pages' object")
	}
	if pagesErr := p.comparePages(revision, oldPages, newPages); pagesErr != nil {
		return &DiffResults{}, pagesErr
	}
	return p.results, nil
}

func (p *defaultDiffPolicy) comparePages(revision int, oldPages, newPages *core.PdfIndirectObject) error {
	if _, changed := p.updatedObjects[newPages.ObjectNumber]; changed {
		p.results.addErrorWithDescription(revision, "Pages were changed")
	}
	newDict, newOk := core.GetDict(newPages.PdfObject)
	oldDict, oldOk := core.GetDict(oldPages.PdfObject)
	if !newOk || !oldOk {
		return errors.New("unexpected Pages' object")
	}
	newKids, newOk := core.GetArray(newDict.Get("Kids"))
	oldKids, oldOk := core.GetArray(oldDict.Get("Kids"))
	if !newOk || !oldOk {
		return errors.New("unexpected Pages' dictionary")
	}
	n := newKids.Len()
	if n > oldKids.Len() {
		n = oldKids.Len()
	}
	for i := 0; i < n; i++ {
		oldPage, ok1 := core.GetIndirect(core.ResolveReference(oldKids.Get(i)))
		newPage, ok2 := core.GetIndirect(core.ResolveReference(newKids.Get(i)))
		if !ok1 || !ok2 {
			return errors.New("unexpected page object")
		}
		if oldPage.ObjectNumber != newPage.ObjectNumber {
			p.results.addErrorWithDescription(revision, fmt.Sprintf("page #%d was replaced", i))
		}
		newPageDict, ok1 := core.GetDict(newPage)
		oldPageDict, ok2 := core.GetDict(oldPage)
		if !ok1 || !ok2 {
			return errors.New("unexpected page's dictionary")
		}
		newAnnots, err := parseAnnots(newPageDict.Get("Annots"))
		if err != nil {
			return err
		}
		oldAnnots, err := parseAnnots(oldPageDict.Get("Annots"))
		if err != nil {
			return err
		}
		if cmpErr := p.compareAnnots(revision, oldAnnots, newAnnots); cmpErr != nil {
			return cmpErr
		}
	}
	for added := n + 1; added <= newKids.Len(); added++ {
		p.results.addErrorWithDescription(revision, fmt.Sprintf("page #%d was added", added))
	}
	for removed := n + 1; removed <= oldKids.Len(); removed++ {
		p.results.addErrorWithDescription(revision, fmt.Sprintf("page #%d was removed", removed))
	}
	return nil
}

// DiffPolicy interface for comparing two revisions of the Pdf document.
type DiffPolicy interface {
	// ReviewFile should check the revisions of the old and new parsers
	// and evaluate the differences between the revisions.
	// Each implementation of this interface must decide
	// how to handle cases where there are multiple revisions between the old and new revisions.
	ReviewFile(oldParser *core.PdfParser, newParser *core.PdfParser, params *MDPParameters) (*DiffResults, error)
}

func (r *DiffResults) addErrorWithDescription(revision int, description string) {
	if r.Errors == nil {
		r.Errors = make([]*DiffResult, 0)
	}
	r.Errors = append(r.Errors, &DiffResult{Revision: revision, Description: description})
}

func parseAnnots(obj core.PdfObject) ([]core.PdfObject, error) {
	annots := make([]core.PdfObject, 0)
	if obj != nil {
		target := obj
		if ind, isInd := core.GetIndirect(obj); isInd {
			target = ind.PdfObject
		}
		if arr, isArr := core.GetArray(target); isArr {
			annots = arr.Elements()
		} else {
			return nil, errors.New("unexpected annots' object")
		}
	}
	return annots, nil
}

func (p *defaultDiffPolicy) compareAnnots(revision int, oldAnnots, newAnnots []core.PdfObject) error {
	oldByNum := make(map[int64]*core.PdfObjectDictionary)
	for _, oldRef := range oldAnnots {
		ind, ok := core.GetIndirect(oldRef)
		if !ok {
			return errors.New("unexpected annot's structure")
		}
		dict, ok := core.GetDict(ind.PdfObject)
		if !ok {
			return errors.New("unexpected annot's structure")
		}
		oldByNum[ind.ObjectNumber] = dict
	}
	for _, newRef := range newAnnots {
		newInd, ok := core.GetIndirect(newRef)
		if !ok {
			return errors.New("unexpected annot's structure")
		}
		newDict, ok := core.GetDict(newInd.PdfObject)
		if !ok {
			return errors.New("unexpected annot's structure")
		}
		title, _ := core.GetStringVal(newDict.Get("T"))
		subtype, _ := core.GetNameVal(newDict.Get("Subtype"))
		if _, found := oldByNum[newInd.ObjectNumber]; !found {
			switch p.level {
			case NoRestrictions, FillFormsAndAnnots:
				p.results.addWarningWithDescription(revision, fmt.Sprintf("%s annotation %s was added", subtype, title))
			default:
				dict, ok := core.GetDict(newInd.PdfObject)
				if !ok {
					return errors.New("undefined annotation")
				}
				subtypeName, ok := core.GetNameVal(dict.Get("Subtype"))
				if !ok {
					return errors.New("undefined annotation's subtype")
				}
				if subtypeName == "Widget" {
					switch p.level {
					case NoRestrictions, FillFormsAndAnnots, FillForms:
						p.results.addWarningWithDescription(revision, fmt.Sprintf("%s annotation %s was added", subtype, title))
					default:
						p.results.addErrorWithDescription(revision, fmt.Sprintf("%s annotation %s was added", subtype, title))
					}
				} else {
					p.results.addErrorWithDescription(revision, fmt.Sprintf("%s annotation %s was added", subtype, title))
				}
			}
		} else {
			delete(oldByNum, newInd.ObjectNumber)
			if changedObj, changed := p.updatedObjects[newInd.ObjectNumber]; changed {
				switch p.level {
				case NoRestrictions, FillFormsAndAnnots:
					p.results.addWarningWithDescription(revision, fmt.Sprintf("%s annotation %s was changed", subtype, title))
				default:
					changedInd, ok := core.GetIndirect(changedObj)
					if !ok {
						return errors.New("undefined annotation")
					}
					changedDict, ok := core.GetDict(changedInd.PdfObject)
					if !ok {
						return errors.New("undefined annotation")
					}
					changedSubtype, ok := core.GetNameVal(changedDict.Get("Subtype"))
					if !ok {
						return errors.New("undefined annotation's subtype")
					}
					if changedSubtype == "Widget" {
						switch p.level {
						case NoRestrictions, FillFormsAndAnnots, FillForms:
							p.results.addWarningWithDescription(revision, fmt.Sprintf("%s annotation %s was changed", subtype, title))
						default:
							p.results.addErrorWithDescription(revision, fmt.Sprintf("%s annotation %s was changed", subtype, title))
						}
					} else {
						p.results.addErrorWithDescription(revision, fmt.Sprintf("%s annotation %s was changed", subtype, title))
					}
				}
			}
		}
	}
	for _, removed := range oldByNum {
		removedTitle, _ := core.GetStringVal(removed.Get("T"))
		removedSubtype, _ := core.GetNameVal(removed.Get("Subtype"))
		switch p.level {
		case NoRestrictions, FillFormsAndAnnots:
			p.results.addWarningWithDescription(revision, fmt.Sprintf("%s annotation %s was removed", removedSubtype, removedTitle))
		default:
			p.results.addErrorWithDescription(revision, fmt.Sprintf("%s annotation %s was removed", removedSubtype, removedTitle))
		}
	}
	return nil
}

// ReviewFile implementation of DiffPolicy interface
// The default policy only checks the next types of objects:
// Page, Pages (container for page objects), Annot, Annots (container for annotation objects), Field.
// It checks adding, removing and modifying objects of these types.
func (p *defaultDiffPolicy) ReviewFile(oldParser *core.PdfParser, newParser *core.PdfParser, params *MDPParameters) (*DiffResults, error) {
	if oldParser.GetRevisionNumber() > newParser.GetRevisionNumber() {
		return nil, errors.New("old revision greater than new revision")
	}
	if oldParser.GetRevisionNumber() == newParser.GetRevisionNumber() {
		if oldParser != newParser {
			return nil, errors.New("same revisions, but different parsers")
		}
		return &DiffResults{}, nil
	}
	if params == nil {
		p.level = NoRestrictions
	} else {
		p.level = params.DocMDPLevel
	}
	results := &DiffResults{}
	for rev := oldParser.GetRevisionNumber() + 1; rev <= newParser.GetRevisionNumber(); rev++ {
		prevRev, err := newParser.GetRevision(rev - 1)
		if err != nil {
			return nil, err
		}
		curRev, err := newParser.GetRevision(rev)
		if err != nil {
			return nil, err
		}
		diff, err := p.compareRevisions(prevRev, curRev)
		if err != nil {
			return nil, err
		}
		results.Warnings = append(results.Warnings, diff.Warnings...)
		results.Errors = append(results.Errors, diff.Errors...)
	}
	return results, nil
}

// DiffResult describes the warning or the error for the DiffPolicy results.
type DiffResult struct {
	Revision    int
	Description string
}

func (r *DiffResults) addError(d *DiffResult) {
	if r.Errors == nil {
		r.Errors = make([]*DiffResult, 0)
	}
	r.Errors = append(r.Errors, d)
}

const (
	NoRestrictions     DocMDPPermission = 0
	NoChanges          DocMDPPermission = 1
	FillForms          DocMDPPermission = 2
	FillFormsAndAnnots DocMDPPermission = 3
)

// MDPParameters describes parameters for the MDP checks (now only DocMDP).
type MDPParameters struct{ DocMDPLevel DocMDPPermission }

// DocMDPPermission is values for set up access permissions for DocMDP.
// (Section 12.8.2.2, Table 254 - Entries in a signature dictionary p. 471 in PDF32000_2008).
type (
	DocMDPPermission  int64
	defaultDiffPolicy struct {
		updatedObjects map[int64]core.PdfObject
		results        *DiffResults
		level          DocMDPPermission
	}
)

func (r *DiffResults) addWarningWithDescription(revision int, description string) {
	if r.Warnings == nil {
		r.Warnings = make([]*DiffResult, 0)
	}
	r.Warnings = append(r.Warnings, &DiffResult{Revision: revision, Description: description})
}

func NewDefaultDiffPolicy() DiffPolicy {
	return &defaultDiffPolicy{updatedObjects: nil, results: &DiffResults{}, level: 0}
}

func (p *defaultDiffPolicy) compareFields(revision int, oldFields, newFields []core.PdfObject) error {
	oldByNum := make(map[int64]*core.PdfObjectDictionary)
	for _, oldRef := range oldFields {
		ind, ok := core.GetIndirect(oldRef)
		if !ok {
			return errors.New("unexpected field's structure")
		}
		dict, ok := core.GetDict(ind.PdfObject)
		if !ok {
			return errors.New("unexpected annot's structure")
		}
		oldByNum[ind.ObjectNumber] = dict
	}
	for _, newRef := range newFields {
		newInd, ok := core.GetIndirect(newRef)
		if !ok {
			return errors.New("unexpected field's structure")
		}
		newDict, ok := core.GetDict(newInd.PdfObject)
		if !ok {
			return errors.New("unexpected field's structure")
		}
		T := newDict.Get("T")
		if _, changed := p.updatedObjects[newInd.ObjectNumber]; changed {
			switch p.level {
			case NoRestrictions, FillForms, FillFormsAndAnnots:
				p.results.addWarningWithDescription(revision, fmt.Sprintf("Field %s was changed", T))
			default:
				p.results.addErrorWithDescription(revision, fmt.Sprintf("Field %s was changed", T))
			}
		}
		if _, found := oldByNum[newInd.ObjectNumber]; !found {
			switch p.level {
			case NoRestrictions, FillForms, FillFormsAndAnnots:
				p.results.addWarningWithDescription(revision, fmt.Sprintf("Field %s was added", newDict.Get("T")))
			default:
				p.results.addErrorWithDescription(revision, fmt.Sprintf("Field %s was added", newDict.Get("T")))
			}
		} else {
			delete(oldByNum, newInd.ObjectNumber)
			if _, changed := p.updatedObjects[newInd.ObjectNumber]; changed {
				switch p.level {
				case NoRestrictions, FillForms, FillFormsAndAnnots:
					p.results.addWarningWithDescription(revision, fmt.Sprintf("Field %s was changed", newDict.Get("T")))
				default:
					p.results.addErrorWithDescription(revision, fmt.Sprintf("Field %s was changed", newDict.Get("T")))
				}
			}
		}
		if FT, ok := core.GetNameVal(newDict.Get("FT")); ok {
			if FT == "Sig" {
				if valInd, ok := core.GetIndirect(newDict.Get("V")); ok {
					if _, changed := p.updatedObjects[valInd.ObjectNumber]; changed {
						switch p.level {
						case NoRestrictions, FillForms, FillFormsAndAnnots:
							p.results.addWarningWithDescription(revision, fmt.Sprintf("Signature for %s field was changed", T))
						default:
							p.results.addErrorWithDescription(revision, fmt.Sprintf("Signature for %s field was changed", T))
						}
					}
				}
			}
		}
	}
	for _, removed := range oldByNum {
		switch p.level {
		case NoRestrictions:
			p.results.addWarningWithDescription(revision, fmt.Sprintf("Field %s was removed", removed.Get("T")))
		default:
			p.results.addErrorWithDescription(revision, fmt.Sprintf("Field %s was removed", removed.Get("T")))
		}
	}
	return nil
}

// DiffResults describes the results of the DiffPolicy.
type DiffResults struct {
	Warnings []*DiffResult
	Errors   []*DiffResult
}

func (r *DiffResults) addWarning(d *DiffResult) {
	if r.Warnings == nil {
		r.Warnings = make([]*DiffResult, 0)
	}
	r.Warnings = append(r.Warnings, d)
}
