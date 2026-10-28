package optimize

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"slices"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// Optimize optimizes PDF objects to decrease PDF size.
func (o *CombineDuplicateStreams) Optimize(objects []core.PdfObject) (result []core.PdfObject, err error) {
	replacements := make(map[core.PdfObject]core.PdfObject)
	removed := make(map[core.PdfObject]struct{})
	streamsByHash := make(map[string][]*core.PdfObjectStream)
	for _, obj := range objects {
		stream, ok := obj.(*core.PdfObjectStream)
		if !ok {
			continue
		}
		if stream.Lazy {
			continue
		}
		h := sha256.New()
		h.Write(stream.Stream)
		h.Write(stream.PdfObjectDictionary.Write())
		key := string(h.Sum(nil))
		streamsByHash[key] = append(streamsByHash[key], stream)
	}
	for _, group := range streamsByHash {
		if len(group) < 2 {
			continue
		}
		first := group[0]
		for i := 1; i < len(group); i++ {
			dup := group[i]
			replacements[dup] = first
			removed[dup] = struct{}{}
		}
	}
	result = make([]core.PdfObject, 0, len(objects)-len(removed))
	for _, obj := range objects {
		if _, isRemoved := removed[obj]; isRemoved {
			continue
		}
		result = append(result, obj)
	}
	replaceObjects(result, replacements)
	return result, nil
}

// CompressStreams compresses uncompressed streams.
// It implements interface model.Optimizer.
type CompressStreams struct{}

// CombineDuplicateStreams combines duplicated streams by its data hash.
// It implements interface model.Optimizer.
type CombineDuplicateStreams struct{}

// Chain allows to use sequence of optimizers.
// It implements interface model.Optimizer.
type Chain struct{ optimizers []model.Optimizer }

func collectUnusedResources(objects []core.PdfObject) (map[core.PdfObject]struct{}, error) {
	structure := findObjectStructure(objects)
	pages := structure.pageObjects
	unused := make(map[core.PdfObject]struct{})
	used := pageResourceNames(pages)
	for _, page := range pages {
		pageDict, ok := core.GetDict(page.PdfObject)
		if !ok {
			continue
		}
		resources, ok := core.GetDict(pageDict.Get("Resources"))
		if !ok {
			continue
		}
		usedXObjects := used["XObject"]
		xobjects, ok := core.GetDict(resources.Get("XObject"))
		if ok {
			names := dictKeyStrings(xobjects)
			for _, name := range names {
				if containsString(name, usedXObjects) {
					continue
				}
				key := *core.MakeName(name)
				obj := xobjects.Get(key)
				unused[obj] = struct{}{}
				xobjects.Remove(key)
				err := markReachable(obj, unused)
				if err != nil {
					common.Log.Debug("failed to traverse object %v", obj)
				}
			}
		}
		fonts, ok := core.GetDict(resources.Get("Font"))
		usedFonts := used["Font"]
		if ok {
			fontNames := dictKeyStrings(fonts)
			for _, name := range fontNames {
				if containsString(name, usedFonts) {
					continue
				}
				key := *core.MakeName(name)
				obj := fonts.Get(key)
				unused[obj] = struct{}{}
				fonts.Remove(key)
				err := markReachable(obj, unused)
				if err != nil {
					common.Log.Debug("Failed to traverse object %v\n", obj)
				}
			}
		}
		extGStates, ok := core.GetDict(resources.Get("ExtGState"))
		if ok {
			gsNames := dictKeyStrings(extGStates)
			usedGStates := used["ExtGState"]
			for _, name := range gsNames {
				if containsString(name, usedGStates) {
					continue
				}
				key := *core.MakeName(name)
				obj := extGStates.Get(key)
				unused[obj] = struct{}{}
				extGStates.Remove(key)
				err := markReachable(obj, unused)
				if err != nil {
					common.Log.Debug("failed to traverse object %v\n", obj)
				}
			}
		}
	}
	return unused, nil
}

// CombineDuplicateDirectObjects combines duplicated direct objects by its data hash.
// It implements interface model.Optimizer.
type CombineDuplicateDirectObjects struct{}

func contentStreamString(obj core.PdfObject) (string, error) {
	direct := core.TraceToDirectObject(obj)
	switch typed := direct.(type) {
	case *core.PdfObjectString:
		return typed.Str(), nil
	case *core.PdfObjectStream:
		data, err := core.DecodeStream(typed)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return "", fmt.Errorf("invalid content stream object holder (%T)", direct)
}

// GetOptimizers gets the list of optimizers in chain `c`.
func (c *Chain) GetOptimizers() []model.Optimizer { return c.optimizers }

// Optimize optimizes PDF objects to decrease PDF size.
func (c *Chain) Optimize(objects []core.PdfObject) (result []core.PdfObject, err error) {
	current := objects
	for _, opt := range c.optimizers {
		next, err := opt.Optimize(current)
		if err != nil {
			common.Log.Debug("ERROR Optimization: %+v", err)
			continue
		}
		current = next
	}
	return current, nil
}

func applyImageModifications(xobj *model.XObjectImage, mods imageModifications) error {
	img, err := xobj.ToImage()
	if err != nil {
		return err
	}
	if mods.Scale != 0 {
		img, err = scaleImage(img, mods.Scale)
		if err != nil {
			return err
		}
	}
	if mods.Encoding != nil {
		xobj.Filter = mods.Encoding
	}
	xobj.Decode = nil
	switch f := xobj.Filter.(type) {
	case *core.FlateEncoder:
		if f.Predictor != 1 && f.Predictor != 11 {
			f.Predictor = 1
		}
	}
	if err = xobj.SetImage(img, nil); err != nil {
		common.Log.Debug("Error setting image: %v", err)
		return err
	}
	xobj.ToPdfObject()
	return nil
}

// Optimize optimizes PDF objects to decrease PDF size.
func (o *CombineDuplicateDirectObjects) Optimize(objects []core.PdfObject) (result []core.PdfObject, err error) {
	renumberObjects(objects)
	dictsByHash := make(map[string][]*core.PdfObjectDictionary)
	var collect func(dict *core.PdfObjectDictionary)
	collect = func(dict *core.PdfObjectDictionary) {
		for _, key := range dict.Keys() {
			val := dict.Get(key)
			if d, ok := val.(*core.PdfObjectDictionary); ok {
				if keys := d.Keys(); len(keys) == 0 {
					continue
				}
				h := md5.New()
				h.Write(d.Write())
				hash := string(h.Sum(nil))
				dictsByHash[hash] = append(dictsByHash[hash], d)
				collect(d)
			}
		}
	}
	for _, obj := range objects {
		ind, ok := obj.(*core.PdfIndirectObject)
		if !ok {
			continue
		}
		if dict, ok := ind.PdfObject.(*core.PdfObjectDictionary); ok {
			collect(dict)
		}
	}
	newObjs := make([]core.PdfObject, 0, len(dictsByHash))
	replacements := make(map[core.PdfObject]core.PdfObject)
	for _, group := range dictsByHash {
		if len(group) < 2 {
			continue
		}
		merged := core.MakeDict()
		merged.Merge(group[0])
		indirect := core.MakeIndirectObject(merged)
		newObjs = append(newObjs, indirect)
		for i := 0; i < len(group); i++ {
			dup := group[i]
			replacements[dup] = indirect
		}
	}
	result = make([]core.PdfObject, len(objects))
	copy(result, objects)
	result = append(newObjs, result...)
	replaceObjects(result, replacements)
	return result, nil
}

func annotationContents(annots core.PdfObject) []content {
	if annots == nil {
		return nil
	}
	arr, ok := core.GetArray(annots)
	if !ok {
		common.Log.Debug("Annots not an array")
		return nil
	}
	var result []content
	for _, elem := range arr.Elements() {
		annot, ok := core.GetDict(elem)
		if !ok {
			common.Log.Debug("Ignoring non-dict element in Annots")
			continue
		}
		ap, ok := core.GetDict(annot.Get("AP"))
		if !ok {
			common.Log.Debug("No AP entry - skipping")
			continue
		}
		normal := core.TraceToDirectObject(ap.Get("N"))
		if normal == nil {
			common.Log.Debug("No N entry - skipping")
			continue
		}
		var form *core.PdfObjectStream
		switch typed := normal.(type) {
		case *core.PdfObjectDictionary:
			state, ok := core.GetName(annot.Get("AS"))
			if !ok {
				common.Log.Debug("No AS entry - skipping")
				continue
			}
			form, ok = core.GetStream(typed.Get(*state))
			if !ok {
				common.Log.Debug("Form not found - skipping")
				continue
			}
		case *core.PdfObjectStream:
			form = typed
		}
		if form == nil {
			common.Log.Debug("Form not found (nil) - skipping")
			continue
		}
		xform, err := model.NewXObjectFormFromStream(form)
		if err != nil {
			common.Log.Debug("Error loading form: %v - ignoring", err)
			continue
		}
		data, err := xform.GetContentStream()
		if err != nil {
			common.Log.Debug("Error decoding contents: %v", err)
			continue
		}
		result = append(result, content{contents: string(data), resources: xform.Resources})
	}
	return result
}

func decodeContents(obj core.PdfObject) (text string, objs []core.PdfObject) {
	var buf bytes.Buffer
	switch typed := obj.(type) {
	case *core.PdfIndirectObject:
		objs = append(objs, typed)
		obj = typed.PdfObject
	}
	switch typed := obj.(type) {
	case *core.PdfObjectStream:
		if data, err := core.DecodeStream(typed); err == nil {
			buf.Write(data)
			objs = append(objs, typed)
		}
	case *core.PdfObjectArray:
		for _, elem := range typed.Elements() {
			switch typed := elem.(type) {
			case *core.PdfObjectStream:
				if data, err := core.DecodeStream(typed); err == nil {
					buf.Write(data)
					objs = append(objs, typed)
				}
			}
		}
	}
	return buf.String(), objs
}

// Optimize optimizes PDF objects to decrease PDF size.
func (o *CombineIdenticalIndirectObjects) Optimize(objects []core.PdfObject) (result []core.PdfObject, err error) {
	renumberObjects(objects)
	replacements := make(map[core.PdfObject]core.PdfObject)
	removed := make(map[core.PdfObject]struct{})
	groups := make(map[string][]*core.PdfIndirectObject)
	for _, obj := range objects {
		ind, ok := obj.(*core.PdfIndirectObject)
		if !ok {
			continue
		}
		if dict, ok := ind.PdfObject.(*core.PdfObjectDictionary); ok {
			if typeName, ok := dict.Get("Type").(*core.PdfObjectName); ok && *typeName == "Page" {
				continue
			}
			if keys := dict.Keys(); len(keys) == 0 {
				continue
			}
			h := md5.New()
			h.Write(dict.Write())
			hash := string(h.Sum(nil))
			groups[hash] = append(groups[hash], ind)
		}
	}
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		first := group[0]
		for i := 1; i < len(group); i++ {
			dup := group[i]
			replacements[dup] = first
			removed[dup] = struct{}{}
		}
	}
	result = make([]core.PdfObject, 0, len(objects)-len(removed))
	for _, obj := range objects {
		if _, isRemoved := removed[obj]; isRemoved {
			continue
		}
		result = append(result, obj)
	}
	replaceObjects(result, replacements)
	return result, nil
}

// Append appends optimizers to the chain.
func (c *Chain) Append(optimizers ...model.Optimizer) {
	c.optimizers = append(c.optimizers, optimizers...)
}

func dictKeyStrings(dict *core.PdfObjectDictionary) []string {
	keys := []string{}
	for _, key := range dict.Keys() {
		keys = append(keys, key.String())
	}
	return keys
}

// Optimize optimizes PDF objects to decrease PDF size.
func (o *CompressStreams) Optimize(objects []core.PdfObject) (result []core.PdfObject, err error) {
	result = make([]core.PdfObject, len(objects))
	copy(result, objects)
	for _, obj := range objects {
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		if filter := stream.Get("Filter"); filter != nil {
			if _, isName := core.GetName(filter); isName {
				continue
			}
			if arr, ok := core.GetArray(filter); ok && arr.Len() > 0 {
				continue
			}
		}
		enc := core.NewFlateEncoder()
		var encoded []byte
		encoded, err = enc.EncodeBytes(stream.Stream)
		if err != nil {
			return result, err
		}
		dict := enc.MakeStreamDict()
		if len(encoded)+len(dict.Write()) < len(stream.Stream) {
			stream.Stream = encoded
			stream.PdfObjectDictionary.Merge(dict)
			stream.PdfObjectDictionary.Set("Length", core.MakeInteger(int64(len(stream.Stream))))
		}
	}
	return result, nil
}

// CombineIdenticalIndirectObjects combines identical indirect objects.
// It implements interface model.Optimizer.
type CombineIdenticalIndirectObjects struct{}

func containsString(s string, list []string) bool {
	return slices.Contains(list, s)
}

type content struct {
	contents  string
	resources *model.PdfPageResources
}
