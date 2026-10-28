package optimize

import (
	"errors"
	"math"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/model"
	"golang.org/x/image/draw"
)

// LinearizeWithOptions is the same as Linearize but additionally configures
// the linearization options. Callers wanting object-stream packaging
// (LinearizationOptions.UseObjectStreams) use this entry point.
func LinearizeWithOptions(w *model.PdfWriter, opts model.LinearizationOptions) {
	w.SetLinearized(true)
	w.SetLinearizationOptions(opts)
}

type objectStructure struct {
	catalog     *core.PdfObjectDictionary
	pages       *core.PdfObjectDictionary
	pageObjects []*core.PdfIndirectObject
}

// ObjectStreams groups PDF objects to object streams.
// It implements interface model.Optimizer.
type ObjectStreams struct{}

// New creates a optimizers chain from options.
func New(options Options) *Chain {
	chain := new(Chain)
	if options.CleanFonts || options.SubsetFonts {
		chain.Append(&CleanFonts{Subset: options.SubsetFonts})
	}
	if options.CleanContentstream {
		chain.Append(new(CleanContentstream))
	}
	if options.ImageUpperPPI > 0 {
		ppi := new(ImagePPI)
		ppi.ImageUpperPPI = options.ImageUpperPPI
		chain.Append(ppi)
	}
	if options.ImageQuality > 0 {
		img := new(Image)
		img.ImageQuality = options.ImageQuality
		chain.Append(img)
	}
	if options.CombineDuplicateDirectObjects {
		chain.Append(new(CombineDuplicateDirectObjects))
	}
	if options.CombineDuplicateStreams {
		chain.Append(new(CombineDuplicateStreams))
	}
	if options.CombineIdenticalIndirectObjects {
		chain.Append(new(CombineIdenticalIndirectObjects))
	}
	if options.UseObjectStreams {
		chain.Append(new(ObjectStreams))
	}
	if options.CompressStreams {
		chain.Append(new(CompressStreams))
	}
	if options.CleanUnusedResources {
		chain.Append(new(CleanUnusedResources))
	}
	return chain
}

// Linearize toggles linearized ("Fast Web View") output on the given writer.
// It is a convenience wrapper over model.PdfWriter.SetLinearized — exposed
// in the optimize package so callers reaching for size/structure tweaks can
// find linearization in the same namespace.
//
// Linearization is not a true Optimizer (the Optimizer interface transforms
// an object list, while linearization is a write-strategy decision), so
// this function does not implement that interface.
func Linearize(w *model.PdfWriter) { w.SetLinearized(true) }

func replaceObjects(objects []core.PdfObject, replacements map[core.PdfObject]core.PdfObject) {
	if len(replacements) == 0 {
		return
	}
	for i, obj := range objects {
		if replacement, ok := replacements[obj]; ok {
			objects[i] = replacement
			continue
		}
		replacements[obj] = obj
		switch typed := obj.(type) {
		case *core.PdfObjectArray:
			elems := make([]core.PdfObject, typed.Len())
			copy(elems, typed.Elements())
			replaceObjects(elems, replacements)
			for j, elem := range elems {
				typed.Set(j, elem)
			}
		case *core.PdfObjectStreams:
			replaceObjects(typed.Elements(), replacements)
		case *core.PdfObjectStream:
			wrapped := []core.PdfObject{typed.PdfObjectDictionary}
			replaceObjects(wrapped, replacements)
			typed.PdfObjectDictionary = wrapped[0].(*core.PdfObjectDictionary)
		case *core.PdfObjectDictionary:
			keys := typed.Keys()
			values := make([]core.PdfObject, len(keys))
			for j, key := range keys {
				values[j] = typed.Get(key)
			}
			replaceObjects(values, replacements)
			for j, key := range keys {
				typed.Set(key, values[j])
			}
		case *core.PdfIndirectObject:
			wrapped := []core.PdfObject{typed.PdfObject}
			replaceObjects(wrapped, replacements)
			typed.PdfObject = wrapped[0]
		}
	}
}

// Optimize optimizes PDF objects to decrease PDF size.
func (o *ObjectStreams) Optimize(objects []core.PdfObject) (result []core.PdfObject, err error) {
	streams := &core.PdfObjectStreams{}
	rest := make([]core.PdfObject, 0, len(objects))
	for _, obj := range objects {
		if ind, ok := obj.(*core.PdfIndirectObject); ok && ind.GenerationNumber == 0 {
			streams.Append(obj)
		} else {
			rest = append(rest, obj)
		}
	}
	if streams.Len() == 0 {
		return rest, nil
	}
	result = make([]core.PdfObject, 0, len(rest)+streams.Len()+1)
	if streams.Len() > 1 {
		result = append(result, streams)
	}
	result = append(result, streams.Elements()...)
	result = append(result, rest...)
	return result, nil
}

func scaleImage(img *model.Image, scale float64) (*model.Image, error) {
	goImg, err := img.ToGoImage()
	if err != nil {
		return nil, err
	}
	var scaled imageutil.Image
	mono, isMono := goImg.(*imageutil.Monochrome)
	if isMono {
		if err = mono.ResolveDecode(); err != nil {
			return nil, err
		}
		scaled, err = mono.Scale(scale)
		if err != nil {
			return nil, err
		}
	} else {
		w := int(math.RoundToEven(float64(img.Width) * scale))
		h := int(math.RoundToEven(float64(img.Height) * scale))
		scaled, err = imageutil.NewImage(w, h, int(img.BitsPerComponent), img.ColorComponents, nil, nil, nil)
		if err != nil {
			return nil, err
		}
		draw.CatmullRom.Scale(scaled, scaled.Bounds(), goImg, goImg.Bounds(), draw.Over, &draw.Options{})
	}
	base := scaled.Base()
	result := &model.Image{Width: int64(base.Width), Height: int64(base.Height), BitsPerComponent: int64(base.BitsPerComponent), ColorComponents: base.ColorComponents, Data: base.Data}
	result.SetDecode(base.Decode)
	result.SetAlpha(base.Alpha)
	return result, nil
}

func findImages(objects []core.PdfObject) []*imageInfo {
	subtype := core.PdfObjectName("Subtype")
	seen := make(map[*core.PdfObjectStream]struct{})
	var images []*imageInfo
	for _, obj := range objects {
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		if _, isSeen := seen[stream]; isSeen {
			continue
		}
		seen[stream] = struct{}{}
		subtypeObj := stream.PdfObjectDictionary.Get(subtype)
		name, ok := core.GetName(subtypeObj)
		if !ok || string(*name) != "Image" {
			continue
		}
		info := &imageInfo{Stream: stream, BitsPerComponent: 8}
		if bpc, ok := core.GetIntVal(stream.Get("BitsPerComponent")); ok {
			info.BitsPerComponent = bpc
		}
		if width, ok := core.GetIntVal(stream.Get("Width")); ok {
			info.Width = width
		}
		if height, ok := core.GetIntVal(stream.Get("Height")); ok {
			info.Height = height
		}
		cs, err := model.NewPdfColorspaceFromPdfObject(stream.Get("ColorSpace"))
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
			continue
		}
		if cs == nil {
			filter, ok := core.GetName(stream.Get("Filter"))
			if ok {
				switch filter.String() {
				case "CCITTFaxDecode", "JBIG2Decode":
					cs = model.NewPdfColorspaceDeviceGray()
					info.BitsPerComponent = 1
				}
			}
		}
		switch typed := cs.(type) {
		case *model.PdfColorspaceDeviceRGB:
			info.ColorComponents = 3
		case *model.PdfColorspaceDeviceGray:
			info.ColorComponents = 1
		default:
			common.Log.Debug("Optimization is not supported for color space %T - skip", typed)
			continue
		}
		images = append(images, info)
	}
	return images
}

func pageResourceNames(pages []*core.PdfIndirectObject) map[string][]string {
	used := map[string][]string{}
	for _, page := range pages {
		pageDict, ok := core.GetDict(page.PdfObject)
		if !ok {
			continue
		}
		contentsObj := pageDict.Get("Contents")
		contents := core.TraceToDirectObject(contentsObj)
		text := ""
		if arr, ok := contents.(*core.PdfObjectArray); ok {
			var parts []string
			for _, elem := range arr.Elements() {
				s, err := contentStreamString(elem)
				if err != nil {
					continue
				}
				parts = append(parts, s)
			}
			text = strings.Join(parts, " ")
		}
		if stream, ok := contents.(*core.PdfObjectStream); ok {
			data, err := core.DecodeStream(stream)
			if err != nil {
				continue
			}
			text = string(data)
		}
		parser := contentstream.NewContentStreamParser(text)
		ops, err := parser.Parse()
		if err != nil {
			continue
		}
		for _, op := range *ops {
			operand := op.Operand
			params := op.Params
			switch operand {
			case "Do":
				name := params[0].String()
				if _, exists := used["XObject"]; !exists {
					used["XObject"] = []string{name}
				} else {
					used["XObject"] = append(used["XObject"], name)
				}
			case "Tf":
				name := params[0].String()
				if _, exists := used["Font"]; !exists {
					used["Font"] = []string{name}
				} else {
					used["Font"] = append(used["Font"], name)
				}
			case "gs":
				name := params[0].String()
				if _, exists := used["ExtGState"]; !exists {
					used["ExtGState"] = []string{name}
				} else {
					used["ExtGState"] = append(used["ExtGState"], name)
				}
			}
		}
	}
	return used
}

func findObjectStructure(objects []core.PdfObject) objectStructure {
	structure := objectStructure{}
	found := false
	for _, obj := range objects {
		switch typed := obj.(type) {
		case *core.PdfIndirectObject:
			dict, ok := core.GetDict(typed)
			if !ok {
				continue
			}
			typeName, ok := core.GetName(dict.Get("Type"))
			if !ok {
				continue
			}
			switch typeName.String() {
			case "Catalog":
				structure.catalog = dict
				found = true
			}
		}
		if found {
			break
		}
	}
	if !found {
		return structure
	}
	pages, ok := core.GetDict(structure.catalog.Get("Pages"))
	if !ok {
		return structure
	}
	structure.pages = pages
	kids, ok := core.GetArray(pages.Get("Kids"))
	if !ok {
		return structure
	}
	for _, kid := range kids.Elements() {
		ind, ok := core.GetIndirect(kid)
		if !ok {
			break
		}
		structure.pageObjects = append(structure.pageObjects, ind)
	}
	return structure
}

func markReachable(obj core.PdfObject, visited map[core.PdfObject]struct{}) error {
	if ind, ok := obj.(*core.PdfIndirectObject); ok {
		visited[obj] = struct{}{}
		err := markReachable(ind.PdfObject, visited)
		if err != nil {
			return err
		}
		return nil
	}
	if stream, ok := obj.(*core.PdfObjectStream); ok {
		visited[stream] = struct{}{}
		err := markReachable(stream.PdfObjectDictionary, visited)
		if err != nil {
			return err
		}
		return nil
	}
	if dict, ok := obj.(*core.PdfObjectDictionary); ok {
		for _, key := range dict.Keys() {
			val := dict.Get(key)
			_ = val
			if ref, ok := val.(*core.PdfObjectReference); ok {
				val = ref.Resolve()
				dict.Set(key, val)
			}
			if key != "Parent" {
				if err := markReachable(val, visited); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if arr, ok := obj.(*core.PdfObjectArray); ok {
		if arr == nil {
			return errors.New("array is nil")
		}
		for i, elem := range arr.Elements() {
			if ref, ok := elem.(*core.PdfObjectReference); ok {
				elem = ref.Resolve()
				arr.Set(i, elem)
			}
			if err := markReachable(elem, visited); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}

func renumberObjects(objects []core.PdfObject) {
	for i, obj := range objects {
		switch typed := obj.(type) {
		case *core.PdfIndirectObject:
			typed.ObjectNumber = int64(i + 1)
			typed.GenerationNumber = 0
		case *core.PdfObjectStream:
			typed.ObjectNumber = int64(i + 1)
			typed.GenerationNumber = 0
		case *core.PdfObjectStreams:
			typed.ObjectNumber = int64(i + 1)
			typed.GenerationNumber = 0
		}
	}
}

// Options describes PDF optimization parameters.
type Options struct {
	CombineDuplicateStreams         bool
	CombineDuplicateDirectObjects   bool
	ImageUpperPPI                   float64
	ImageQuality                    int
	UseObjectStreams                bool
	CombineIdenticalIndirectObjects bool
	CompressStreams                 bool
	CleanFonts                      bool
	SubsetFonts                     bool
	CleanContentstream              bool
	CleanUnusedResources            bool
}
