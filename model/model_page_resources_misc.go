package model

import (
	"errors"
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// PdfPageResources is a Page resources model.
// Implements PdfModel.
type PdfPageResources struct {
	ExtGState   core.PdfObject
	ColorSpace  core.PdfObject
	Pattern     core.PdfObject
	Shading     core.PdfObject
	XObject     core.PdfObject
	Font        core.PdfObject
	ProcSet     core.PdfObject
	Properties  core.PdfObject
	primitive   *core.PdfObjectDictionary
	colorspaces *PdfPageResourcesColorspaces
}

// HasFontByName checks whether a font is defined by the specified keyName.
func (r *PdfPageResources) HasFontByName(keyName core.PdfObjectName) bool {
	_, ok := r.GetFontByName(keyName)
	return ok
}

// SetPatternByName sets a pattern resource specified by keyName.
func (r *PdfPageResources) SetPatternByName(keyName core.PdfObjectName, pattern core.PdfObject) error {
	if r.Pattern == nil {
		r.Pattern = core.MakeDict()
	}
	patternDict, ok := core.GetDict(r.Pattern)
	if !ok {
		return core.ErrTypeError
	}
	patternDict.Set(keyName, pattern)
	return nil
}

// GetShadingByName gets the shading specified by keyName. Returns nil if not existing.
// The bool flag indicated whether it was found or not.
func (r *PdfPageResources) GetShadingByName(keyName core.PdfObjectName) (*PdfShading, bool) {
	if r.Shading == nil {
		return nil, false
	}
	dict, ok := core.TraceToDirectObject(r.Shading).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Invalid Shading entry - not a dict (got %T)", r.Shading)
		return nil, false
	}
	if obj := dict.Get(keyName); obj != nil {
		shading, err := newPdfShadingFromPdfObject(obj)
		if err != nil {
			common.Log.Debug("ERROR: failed to load pdf shading: %v", err)
			return nil, false
		}
		return shading, true
	}
	return nil, false
}

// HasColorspaceByName checks if the colorspace with the specified name exists in the page resources.
func (r *PdfPageResources) HasColorspaceByName(keyName core.PdfObjectName) bool {
	colorspaces, err := r.GetColorspaces()
	if err != nil {
		common.Log.Debug("ERROR getting colorsprace: %v", err)
		return false
	}
	if colorspaces == nil {
		return false
	}
	_, ok := colorspaces.Colorspaces[string(keyName)]
	return ok
}

// SetFontByName sets the font specified by keyName to the given object.
func (r *PdfPageResources) SetFontByName(keyName core.PdfObjectName, obj core.PdfObject) error {
	if r.Font == nil {
		r.Font = core.MakeDict()
	}
	dict, ok := core.TraceToDirectObject(r.Font).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Font not a dictionary! (got %T)", core.TraceToDirectObject(r.Font))
		return core.ErrTypeError
	}
	dict.Set(keyName, obj)
	return nil
}

// ToPdfObject returns the PDF representation of the page resources.
func (r *PdfPageResources) ToPdfObject() core.PdfObject {
	dict := r.primitive
	dict.SetIfNotNil("ExtGState", r.ExtGState)
	if r.colorspaces != nil {
		r.ColorSpace = r.colorspaces.ToPdfObject()
	}
	dict.SetIfNotNil("ColorSpace", r.ColorSpace)
	dict.SetIfNotNil("Pattern", r.Pattern)
	dict.SetIfNotNil("Shading", r.Shading)
	dict.SetIfNotNil("XObject", r.XObject)
	dict.SetIfNotNil("Font", r.Font)
	dict.SetIfNotNil("ProcSet", r.ProcSet)
	dict.SetIfNotNil("Properties", r.Properties)
	return dict
}

// ToPdfObject returns the PDF representation of the colorspace.
func (r *PdfPageResourcesColorspaces) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	for _, name := range r.Names {
		dict.Set(core.PdfObjectName(name), r.Colorspaces[name].ToPdfObject())
	}
	if r.container != nil {
		r.container.PdfObject = dict
		return r.container
	}
	return dict
}

// GetFontByName gets the font specified by keyName. Returns the PdfObject which
// the entry refers to. Returns a bool value indicating whether or not the entry was found.
func (r *PdfPageResources) GetFontByName(keyName core.PdfObjectName) (core.PdfObject, bool) {
	if r.Font == nil {
		return nil, false
	}
	dict, ok := core.TraceToDirectObject(r.Font).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Font not a dictionary! (got %T)", core.TraceToDirectObject(r.Font))
		return nil, false
	}
	if obj := dict.Get(keyName); obj != nil {
		return obj, true
	}
	return nil, false
}

func newPdfPageResourcesColorspacesFromPdfObject(colorSpace core.PdfObject) (*PdfPageResourcesColorspaces, error) {
	pageResourcesColorspaces := &PdfPageResourcesColorspaces{}
	if indirect, ok := colorSpace.(*core.PdfIndirectObject); ok {
		pageResourcesColorspaces.container = indirect
		colorSpace = indirect.PdfObject
	}
	colorSpaceDict, ok := core.GetDict(colorSpace)
	if !ok {
		return nil, errors.New("CS attribute type error")
	}
	pageResourcesColorspaces.Names = []string{}
	pageResourcesColorspaces.Colorspaces = map[string]PdfColorspace{}
	for _, name := range colorSpaceDict.Keys() {
		obj := colorSpaceDict.Get(name)
		pageResourcesColorspaces.Names = append(pageResourcesColorspaces.Names, string(name))
		cs, err := NewPdfColorspaceFromPdfObject(obj)
		if err != nil {
			return nil, err
		}
		pageResourcesColorspaces.Colorspaces[string(name)] = cs
	}
	return pageResourcesColorspaces, nil
}

// HasPatternByName checks whether a pattern object is defined by the specified keyName.
func (r *PdfPageResources) HasPatternByName(keyName core.PdfObjectName) bool {
	_, ok := r.GetPatternByName(keyName)
	return ok
}

// GetPatternByName gets the pattern specified by keyName. Returns nil if not existing.
// The bool flag indicated whether it was found or not.
func (r *PdfPageResources) GetPatternByName(keyName core.PdfObjectName) (*PdfPattern, bool) {
	if r.Pattern == nil {
		return nil, false
	}
	dict, ok := core.TraceToDirectObject(r.Pattern).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Invalid Pattern entry - not a dict (got %T)", r.Pattern)
		return nil, false
	}
	if obj := dict.Get(keyName); obj != nil {
		pattern, err := newPdfPatternFromPdfObject(obj)
		if err != nil {
			common.Log.Debug("ERROR: failed to load pdf pattern: %v", err)
			return nil, false
		}
		return pattern, true
	}
	return nil, false
}

// GetContainingPdfObject returns the container of the resources object (indirect object).
func (r *PdfPageResources) GetContainingPdfObject() core.PdfObject { return r.primitive }

// NewPdfPageResourcesColorspaces returns a new PdfPageResourcesColorspaces object.
func NewPdfPageResourcesColorspaces() *PdfPageResourcesColorspaces {
	pageResourcesColorspaces := &PdfPageResourcesColorspaces{}
	pageResourcesColorspaces.Names = []string{}
	pageResourcesColorspaces.Colorspaces = map[string]PdfColorspace{}
	pageResourcesColorspaces.container = &core.PdfIndirectObject{}
	return pageResourcesColorspaces
}

// SetColorspaceByName adds the provided colorspace to the page resources.
func (r *PdfPageResources) SetColorspaceByName(keyName core.PdfObjectName, cs PdfColorspace) error {
	colorspaces, err := r.GetColorspaces()
	if err != nil {
		common.Log.Debug("ERROR getting colorsprace: %v", err)
		return err
	}
	if colorspaces == nil {
		colorspaces = NewPdfPageResourcesColorspaces()
		r.SetColorSpace(colorspaces)
	}
	colorspaces.Set(keyName, cs)
	return nil
}

// GetExtGState gets the ExtGState specified by keyName. Returns a bool
// indicating whether it was found or not.
func (r *PdfPageResources) GetExtGState(keyName core.PdfObjectName) (core.PdfObject, bool) {
	if r.ExtGState == nil {
		return nil, false
	}
	dict, ok := core.TraceToDirectObject(r.ExtGState).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Invalid ExtGState entry - not a dict (got %T)", r.ExtGState)
		return nil, false
	}
	if obj := dict.Get(keyName); obj != nil {
		return obj, true
	}
	return nil, false
}

// HasExtGState checks whether a font is defined by the specified keyName.
func (r *PdfPageResources) HasExtGState(keyName core.PdfObjectName) bool {
	_, ok := r.GetFontByName(keyName)
	return ok
}

// SetShadingByName sets a shading resource specified by keyName.
func (r *PdfPageResources) SetShadingByName(keyName core.PdfObjectName, shadingObj core.PdfObject) error {
	if r.Shading == nil {
		r.Shading = core.MakeDict()
	}
	shadingDict, ok := core.GetDict(r.Shading)
	if !ok {
		return core.ErrTypeError
	}
	shadingDict.Set(keyName, shadingObj)
	return nil
}

// Set sets the colorspace corresponding to key. Add to Names if not set.
func (r *PdfPageResourcesColorspaces) Set(key core.PdfObjectName, val PdfColorspace) {
	if _, ok := r.Colorspaces[string(key)]; !ok {
		r.Names = append(r.Names, string(key))
	}
	r.Colorspaces[string(key)] = val
}

// GetColorspaceByName returns the colorspace with the specified name from the page resources.
func (r *PdfPageResources) GetColorspaceByName(keyName core.PdfObjectName) (PdfColorspace, bool) {
	colorspaces, err := r.GetColorspaces()
	if err != nil {
		common.Log.Debug("ERROR getting colorsprace: %v", err)
		return nil, false
	}
	if colorspaces == nil {
		return nil, false
	}
	colorspace, ok := colorspaces.Colorspaces[string(keyName)]
	if !ok {
		return nil, false
	}
	return colorspace, true
}

// NewPdfPageResources returns a new PdfPageResources object.
func NewPdfPageResources() *PdfPageResources {
	resources := &PdfPageResources{}
	resources.primitive = core.MakeDict()
	return resources
}

// SetColorSpace sets `r` colorspace object to `colorspace`.
func (r *PdfPageResources) SetColorSpace(colorspace *PdfPageResourcesColorspaces) {
	r.colorspaces = colorspace
}

// AddExtGState add External Graphics State (GState). The gsDict can be specified
// either directly as a dictionary or an indirect object containing a dictionary.
func (r *PdfPageResources) AddExtGState(gsName core.PdfObjectName, gsDict core.PdfObject) error {
	if r.ExtGState == nil {
		r.ExtGState = core.MakeDict()
	}
	extGState := r.ExtGState
	dict, ok := core.TraceToDirectObject(extGState).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ExtGState type error (got %T/%T)", extGState, core.TraceToDirectObject(extGState))
		return core.ErrTypeError
	}
	dict.Set(gsName, gsDict)
	return nil
}

// NewPdfPageResourcesFromDict creates and returns a new PdfPageResources object
// from the input dictionary.
func NewPdfPageResourcesFromDict(dict *core.PdfObjectDictionary) (*PdfPageResources, error) {
	resources := NewPdfPageResources()
	if extGState := dict.Get("ExtGState"); extGState != nil {
		resources.ExtGState = extGState
	}
	if colorSpace := dict.Get("ColorSpace"); colorSpace != nil && !core.IsNullObject(colorSpace) {
		resources.ColorSpace = colorSpace
	}
	if pattern := dict.Get("Pattern"); pattern != nil {
		resources.Pattern = pattern
	}
	if shading := dict.Get("Shading"); shading != nil {
		resources.Shading = shading
	}
	if xObject := dict.Get("XObject"); xObject != nil {
		resources.XObject = xObject
	}
	if font := core.ResolveReference(dict.Get("Font")); font != nil {
		resources.Font = font
	}
	if procSet := dict.Get("ProcSet"); procSet != nil {
		resources.ProcSet = procSet
	}
	if properties := dict.Get("Properties"); properties != nil {
		resources.Properties = properties
	}
	return resources, nil
}

// HasShadingByName checks whether a shading is defined by the specified keyName.
func (r *PdfPageResources) HasShadingByName(keyName core.PdfObjectName) bool {
	_, ok := r.GetShadingByName(keyName)
	return ok
}

// PdfPageResourcesColorspaces contains the colorspace in the PdfPageResources.
// Needs to have matching name and colorspace map entry. The Names define the order.
type PdfPageResourcesColorspaces struct {
	Names       []string
	Colorspaces map[string]PdfColorspace
	container   *core.PdfIndirectObject
}

// GetColorspaces loads PdfPageResourcesColorspaces from `r.ColorSpace` and returns an error if there
// is a problem loading. Once loaded, the same object is returned on multiple calls.
func (r *PdfPageResources) GetColorspaces() (*PdfPageResourcesColorspaces, error) {
	if r.colorspaces != nil {
		return r.colorspaces, nil
	}
	if r.ColorSpace == nil {
		return nil, nil
	}
	pageResourcesColorspaces, err := newPdfPageResourcesColorspacesFromPdfObject(r.ColorSpace)
	if err != nil {
		return nil, err
	}
	r.colorspaces = pageResourcesColorspaces
	return r.colorspaces, nil
}

// GenerateXObjectName generates an unused XObject name that can be used for
// adding new XObjects. Uses format XObj1, XObj2, ...
func (r *PdfPageResources) GenerateXObjectName() core.PdfObjectName {
	n := 1
	for {
		name := core.MakeName(fmt.Sprintf("XObj%d", n))
		if !r.HasXObjectByName(*name) {
			return *name
		}
		n++
	}
}
