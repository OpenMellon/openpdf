package model

import (
	"errors"
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/core"
)

func (r *PdfReader) newPdfPageFromDict(dict *core.PdfObjectDictionary) (*PdfPage, error) {
	page := NewPdfPage()
	page.pageDict = dict
	page.originalDict = dict.ShallowCopy()
	name, ok := dict.Get("Type").(*core.PdfObjectName)
	if !ok {
		return nil, errors.New("missing/invalid Page dictionary Type")
	}
	if *name != "Page" {
		return nil, errors.New("page dictionary Type != Page")
	}
	if parent := dict.Get("Parent"); parent != nil {
		page.Parent = parent
	}
	if lastModified := dict.Get("LastModified"); lastModified != nil {
		lastModifiedStr, ok := core.GetString(lastModified)
		if !ok {
			return nil, errors.New("page dictionary LastModified != string")
		}
		date, err := NewPdfDate(lastModifiedStr.Str())
		if err != nil {
			return nil, err
		}
		page.LastModified = &date
	}
	if resources := dict.Get("Resources"); resources != nil && !core.IsNullObject(resources) {
		resourcesDict, ok := core.GetDict(resources)
		if !ok {
			return nil, fmt.Errorf("invalid resource dictionary (%T)", resources)
		}
		var err error
		page.Resources, err = NewPdfPageResourcesFromDict(resourcesDict)
		if err != nil {
			return nil, err
		}
	} else {
		parentResources, err := page.getParentResources()
		if err != nil {
			return nil, err
		}
		if parentResources == nil {
			parentResources = NewPdfPageResources()
		}
		page.Resources = parentResources
	}
	if mediaBox := dict.Get("MediaBox"); mediaBox != nil {
		mediaBoxArr, ok := core.GetArray(mediaBox)
		if !ok {
			return nil, errors.New("page MediaBox not an array")
		}
		var err error
		page.MediaBox, err = NewPdfRectangle(*mediaBoxArr)
		if err != nil {
			return nil, err
		}
	}
	if cropBox := dict.Get("CropBox"); cropBox != nil {
		cropBoxArr, ok := core.GetArray(cropBox)
		if !ok {
			return nil, errors.New("page CropBox not an array")
		}
		var err error
		page.CropBox, err = NewPdfRectangle(*cropBoxArr)
		if err != nil {
			return nil, err
		}
	}
	if bleedBox := dict.Get("BleedBox"); bleedBox != nil {
		bleedBoxArr, ok := core.GetArray(bleedBox)
		if !ok {
			return nil, errors.New("page BleedBox not an array")
		}
		var err error
		page.BleedBox, err = NewPdfRectangle(*bleedBoxArr)
		if err != nil {
			return nil, err
		}
	}
	if trimBox := dict.Get("TrimBox"); trimBox != nil {
		trimBoxArr, ok := core.GetArray(trimBox)
		if !ok {
			return nil, errors.New("page TrimBox not an array")
		}
		var err error
		page.TrimBox, err = NewPdfRectangle(*trimBoxArr)
		if err != nil {
			return nil, err
		}
	}
	if artBox := dict.Get("ArtBox"); artBox != nil {
		artBoxArr, ok := core.GetArray(artBox)
		if !ok {
			return nil, errors.New("page ArtBox not an array")
		}
		var err error
		page.ArtBox, err = NewPdfRectangle(*artBoxArr)
		if err != nil {
			return nil, err
		}
	}
	if boxColorInfo := dict.Get("BoxColorInfo"); boxColorInfo != nil {
		page.BoxColorInfo = boxColorInfo
	}
	if contents := dict.Get("Contents"); contents != nil {
		page.Contents = contents
	}
	if rotate := dict.Get("Rotate"); rotate != nil {
		n, err := core.GetNumberAsInt64(rotate)
		if err != nil {
			return nil, errors.New("invalid Page Rotate object")
		}
		page.Rotate = &n
	}
	if group := dict.Get("Group"); group != nil {
		page.Group = group
	}
	if thumb := dict.Get("Thumb"); thumb != nil {
		page.Thumb = thumb
	}
	if b := dict.Get("B"); b != nil {
		page.B = b
	}
	if dur := dict.Get("Dur"); dur != nil {
		page.Dur = dur
	}
	if trans := dict.Get("Trans"); trans != nil {
		page.Trans = trans
	}
	if aa := dict.Get("AA"); aa != nil {
		page.AA = aa
	}
	if metadata := dict.Get("Metadata"); metadata != nil {
		page.Metadata = metadata
	}
	if pieceInfo := dict.Get("PieceInfo"); pieceInfo != nil {
		page.PieceInfo = pieceInfo
	}
	if structParents := dict.Get("StructParents"); structParents != nil {
		page.StructParents = structParents
	}
	if id := dict.Get("ID"); id != nil {
		page.ID = id
	}
	if pz := dict.Get("PZ"); pz != nil {
		page.PZ = pz
	}
	if separationInfo := dict.Get("SeparationInfo"); separationInfo != nil {
		page.SeparationInfo = separationInfo
	}
	if tabs := dict.Get("Tabs"); tabs != nil {
		page.Tabs = tabs
	}
	if templateInstantiated := dict.Get("TemplateInstantiated"); templateInstantiated != nil {
		page.TemplateInstantiated = templateInstantiated
	}
	if presSteps := dict.Get("PresSteps"); presSteps != nil {
		page.PresSteps = presSteps
	}
	if userUnit := dict.Get("UserUnit"); userUnit != nil {
		page.UserUnit = userUnit
	}
	if vp := dict.Get("VP"); vp != nil {
		page.VP = vp
	}
	if annots := dict.Get("Annots"); annots != nil {
		page.Annots = annots
	}
	page.reader = r
	return page, nil
}

// GetPageLabels returns the PageLabels entry in the PDF catalog.
// See section 12.4.2 "Page Labels" (p. 382 PDF32000_2008).
func (r *PdfReader) GetPageLabels() (core.PdfObject, error) {
	pageLabels := core.ResolveReference(r.catalog.Get("PageLabels"))
	if pageLabels == nil {
		return nil, nil
	}
	if !r.lazy {
		err := r.traverseObjectData(pageLabels)
		if err != nil {
			return nil, err
		}
	}
	return pageLabels, nil
}

// GetPage returns the PdfPage model for the specified page number.
func (r *PdfReader) GetPage(pageNumber int) (*PdfPage, error) {
	if r.parser.GetCrypter() != nil && !r.parser.IsAuthenticated() {
		return nil, fmt.Errorf("file needs to be decrypted first")
	}
	if len(r.pageObjs) < pageNumber {
		return nil, errors.New("invalid page number (page count too short)")
	}
	n := pageNumber - 1
	if n < 0 {
		return nil, fmt.Errorf("page numbering must start at 1")
	}
	page := r.PageList[n]
	return page, nil
}

// PageFromIndirectObject returns the PdfPage and page number for a given indirect object.
func (r *PdfReader) PageFromIndirectObject(ind *core.PdfIndirectObject) (*PdfPage, int, error) {
	if len(r.PageList) != len(r.pageObjs) {
		return nil, 0, errors.New("page list invalid")
	}
	for i, pageObj := range r.pageObjs {
		if pageObj == ind {
			return r.PageList[i], i + 1, nil
		}
	}
	return nil, 0, errors.New("page not found")
}
