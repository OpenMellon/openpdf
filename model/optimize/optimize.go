// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package optimize

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/extractor"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/unitype"
	"golang.org/x/image/draw"
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

// CompressStreams compresses uncompressed streams.
// It implements interface model.Optimizer.
type CompressStreams struct{}

// CombineDuplicateStreams combines duplicated streams by its data hash.
// It implements interface model.Optimizer.
type CombineDuplicateStreams struct{}

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

// Optimize optimizes PDF objects to decrease PDF size.
func (o *Image) Optimize(objects []core.PdfObject) (result []core.PdfObject, err error) {
	if o.ImageQuality <= 0 {
		return objects, nil
	}
	images := findImages(objects)
	if len(images) == 0 {
		return objects, nil
	}
	replacements := make(map[core.PdfObject]core.PdfObject)
	smasks := make(map[core.PdfObject]struct{})
	for _, info := range images {
		smask := info.Stream.Get("SMask")
		smasks[smask] = struct{}{}
	}
	for i, info := range images {
		stream := info.Stream
		if _, isSMask := smasks[stream]; isSMask {
			continue
		}
		xobj, err := model.NewXObjectImageFromStream(stream)
		if err != nil {
			common.Log.Debug("ERROR: %+v", err)
			continue
		}
		switch xobj.Filter.(type) {
		case *core.JBIG2Encoder:
			continue
		case *core.CCITTFaxEncoder:
			continue
		}
		img, err := xobj.ToImage()
		if err != nil {
			common.Log.Debug("ERROR: %+v", err)
			continue
		}
		dct := core.NewDCTEncoder()
		dct.ColorComponents = img.ColorComponents
		dct.Quality = o.ImageQuality
		dct.BitsPerComponent = info.BitsPerComponent
		dct.Width = info.Width
		dct.Height = info.Height
		dctData, err := dct.EncodeBytes(img.Data)
		if err != nil {
			common.Log.Debug("ERROR: %+v", err)
			continue
		}
		var encoder core.StreamEncoder
		encoder = dct
		{
			flate := core.NewFlateEncoder()
			multi := core.NewMultiEncoder()
			multi.AddEncoder(flate)
			multi.AddEncoder(dct)
			multiData, err := multi.EncodeBytes(img.Data)
			if err != nil {
				common.Log.Debug("ERROR: %+v", err)
				continue
			}
			if len(multiData) < len(dctData) {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Multi enc improves: %d to %d (orig %d)", len(dctData), len(multiData), len(stream.Stream))
				}
				dctData = multiData
				encoder = multi
			}
		}
		origLen := len(stream.Stream)
		if origLen < len(dctData) {
			continue
		}
		newStream := &core.PdfObjectStream{Stream: dctData}
		newStream.PdfObjectReference = stream.PdfObjectReference
		newStream.PdfObjectDictionary = core.MakeDict()
		newStream.Merge(stream.PdfObjectDictionary)
		newStream.Merge(encoder.MakeStreamDict())
		newStream.Set("Length", core.MakeInteger(int64(len(dctData))))
		replacements[stream] = newStream
		images[i].Stream = newStream
	}
	result = make([]core.PdfObject, len(objects))
	copy(result, objects)
	replaceObjects(result, replacements)
	return result, nil
}

// Chain allows to use sequence of optimizers.
// It implements interface model.Optimizer.
type Chain struct{ optimizers []model.Optimizer }

// Linearize toggles linearized ("Fast Web View") output on the given writer.
// It is a convenience wrapper over model.PdfWriter.SetLinearized — exposed
// in the optimize package so callers reaching for size/structure tweaks can
// find linearization in the same namespace.
//
// Linearization is not a true Optimizer (the Optimizer interface transforms
// an object list, while linearization is a write-strategy decision), so
// this function does not implement that interface.
func Linearize(w *model.PdfWriter) { w.SetLinearized(true) }

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

// ImagePPI optimizes images by scaling images such that the PPI (pixels per inch) is never higher than ImageUpperPPI.
// TODO(a5i): Add support for inline images.
// It implements interface model.Optimizer.
type ImagePPI struct{ ImageUpperPPI float64 }

// CombineDuplicateDirectObjects combines duplicated direct objects by its data hash.
// It implements interface model.Optimizer.
type CombineDuplicateDirectObjects struct{}

// Image optimizes images by rewrite images into JPEG format with quality equals to ImageQuality.
// TODO(a5i): Add support for inline images.
// It implements interface model.Optimizer.
type Image struct{ ImageQuality int }

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

type imageModifications struct {
	Scale    float64
	Encoding core.StreamEncoder
}

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

// CleanFonts cleans up embedded fonts, reducing font sizes.
type CleanFonts struct {
	// Subset embedded fonts if encountered (if true).
	// Otherwise attempts to reduce the font program.
	Subset bool
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

func cleanContentStream(stream *core.PdfObjectStream) error {
	data, err := core.DecodeStream(stream)
	if err != nil {
		return err
	}
	parser := contentstream.NewContentStreamParser(string(data))
	ops, err := parser.Parse()
	if err != nil {
		return err
	}
	ops = cleanOperations(ops)
	cleaned := ops.Bytes()
	if len(cleaned) >= len(data) {
		return nil
	}
	newStream, err := core.MakeStream(ops.Bytes(), core.NewFlateEncoder())
	if err != nil {
		return err
	}
	stream.Stream = newStream.Stream
	stream.Merge(newStream.PdfObjectDictionary)
	return nil
}

// Optimize optimizes PDF objects to decrease PDF size.
func (o *CleanFonts) Optimize(objects []core.PdfObject) (result []core.PdfObject, err error) {
	var subsetted map[*core.PdfObjectStream]struct{}
	if o.Subset {
		var subsetErr error
		subsetted, subsetErr = subsetFonts(objects)
		if subsetErr != nil {
			common.Log.Debug("ERROR: Failed subsetting: %v", subsetErr)
			return nil, subsetErr
		}
	}
	for _, obj := range objects {
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		if _, isSubsetted := subsetted[stream]; isSubsetted {
			continue
		}
		encoder, err := core.NewEncoderFromStream(stream)
		if err != nil {
			common.Log.Debug("ERROR getting encoder: %v - ignoring", err)
			continue
		}
		data, err := encoder.DecodeStream(stream)
		if err != nil {
			common.Log.Debug("Decoding error : %v - ignoring", err)
			continue
		}
		if len(data) < 4 {
			continue
		}
		magic := string(data[:4])
		if magic == "OTTO" {
			continue
		}
		if magic != "\x00\x01\x00\x00" && magic != "true" {
			continue
		}
		font, err := unitype.Parse(bytes.NewReader(data))
		if err != nil {
			common.Log.Debug("ERROR Parsing font: %v - ignoring", err)
			continue
		}
		err = font.Optimize()
		if err != nil {
			common.Log.Debug("ERROR Optimizing font: %v - skipping", err)
			continue
		}
		var buf bytes.Buffer
		err = font.Write(&buf)
		if err != nil {
			common.Log.Debug("ERROR Writing font: %v - ignoring", err)
			continue
		}
		if buf.Len() > len(data) {
			common.Log.Debug("Re-written font is larger than original - skip")
			continue
		}
		newStream, err := core.MakeStream(buf.Bytes(), core.NewFlateEncoder())
		if err != nil {
			continue
		}
		*stream = *newStream
		stream.Set("Length1", core.MakeInteger(int64(buf.Len())))
	}
	return objects, nil
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

// CleanContentstream cleans up redundant operands in content streams, including Page and XObject Form
// contents. This process includes:
// 1. Marked content operators are removed.
// 2. Some operands are simplified (shorter form).
// TODO: Add more reduction methods and improving the methods for identifying unnecessary operands.
type CleanContentstream struct{}

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

type imageInfo struct {
	BitsPerComponent int
	ColorComponents  int
	Width            int
	Height           int
	Stream           *core.PdfObjectStream
	PPI              float64
}

func cleanOperations(ops *contentstream.ContentStreamOperations) *contentstream.ContentStreamOperations {
	if ops == nil {
		return nil
	}
	result := contentstream.ContentStreamOperations{}
	for _, op := range *ops {
		switch op.Operand {
		case "BDC", "BMC", "EMC":
			continue
		case "Tm":
			if len(op.Params) == 6 {
				if nums, err := core.GetNumbersAsFloat(op.Params); err == nil {
					if nums[0] == 1 && nums[1] == 0 && nums[2] == 0 && nums[3] == 1 {
						op = &contentstream.ContentStreamOperation{Params: []core.PdfObject{op.Params[4], op.Params[5]}, Operand: "Td"}
					}
				}
			}
		}
		result = append(result, op)
	}
	return &result
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

// Optimize optimizes PDF objects to decrease PDF size.
func (o *ImagePPI) Optimize(objects []core.PdfObject) (result []core.PdfObject, err error) {
	if o.ImageUpperPPI <= 0 {
		return objects, nil
	}
	images := findImages(objects)
	if len(images) == 0 {
		return objects, nil
	}
	smasks := make(map[core.PdfObject]struct{})
	for _, info := range images {
		smask := info.Stream.PdfObjectDictionary.Get("SMask")
		smasks[smask] = struct{}{}
	}
	byStream := make(map[*core.PdfObjectStream]*imageInfo)
	for _, info := range images {
		byStream[info.Stream] = info
	}
	var catalog *core.PdfObjectDictionary
	for _, obj := range objects {
		if dict, ok := core.GetDict(obj); catalog == nil && ok {
			if typeName, ok := core.GetName(dict.Get("Type")); ok && *typeName == "Catalog" {
				catalog = dict
			}
		}
	}
	if catalog == nil {
		return objects, nil
	}
	pages, ok := core.GetDict(catalog.Get("Pages"))
	if !ok {
		return objects, nil
	}
	kids, kidsOK := core.GetArray(pages.Get("Kids"))
	if !kidsOK {
		return objects, nil
	}
	for _, kid := range kids.Elements() {
		byName := make(map[string]*imageInfo)
		page, ok := core.GetDict(kid)
		if !ok {
			continue
		}
		contents, _ := decodeContents(page.Get("Contents"))
		if len(contents) == 0 {
			continue
		}
		resDict, resOK := core.GetDict(page.Get("Resources"))
		if !resOK {
			continue
		}
		resources, err := model.NewPdfPageResourcesFromDict(resDict)
		if err != nil {
			common.Log.Debug("ERROR parsing resources - ignoring: %v", err)
			continue
		}
		xobjects, xobjOK := core.GetDict(resDict.Get("XObject"))
		if !xobjOK {
			continue
		}
		names := xobjects.Keys()
		for _, name := range names {
			if stream, ok := core.GetStream(xobjects.Get(name)); ok {
				if img, ok := byStream[stream]; ok {
					byName[string(name)] = img
				}
			}
		}
		parser := contentstream.NewContentStreamParser(contents)
		ops, err := parser.Parse()
		if err != nil {
			common.Log.Debug("ERROR: %+v", err)
			continue
		}
		processor := contentstream.NewContentStreamProcessor(*ops)
		processor.AddHandler(contentstream.HandlerConditionEnumAllOperands, "", func(op *contentstream.ContentStreamOperation, gs contentstream.GraphicsState, res *model.PdfPageResources) error {
			switch op.Operand {
			case "Do":
				if len(op.Params) != 1 {
					common.Log.Debug("ERROR: Ignoring Do with len(params) != 1")
					return nil
				}
				nameObj, ok := core.GetName(op.Params[0])
				if !ok {
					common.Log.Debug("ERROR: Ignoring Do with non Name parameter")
					return nil
				}
				if img, ok := byName[string(*nameObj)]; ok {
					scaleX := gs.CTM.ScalingFactorX()
					scaleY := gs.CTM.ScalingFactorY()
					widthIn, heightIn := scaleX/72.0, scaleY/72.0
					ppiX, ppiY := float64(img.Width)/widthIn, float64(img.Height)/heightIn
					if widthIn == 0 || heightIn == 0 {
						ppiX = 72.0
						ppiY = 72.0
					}
					img.PPI = math.Max(img.PPI, ppiX)
					img.PPI = math.Max(img.PPI, ppiY)
				}
			}
			return nil
		})
		err = processor.Process(resources)
		if err != nil {
			common.Log.Debug("ERROR processing: %+v", err)
			continue
		}
	}
	for _, info := range images {
		if _, isSMask := smasks[info.Stream]; isSMask {
			continue
		}
		if info.PPI <= o.ImageUpperPPI {
			continue
		}
		xobj, err := model.NewXObjectImageFromStream(info.Stream)
		if err != nil {
			common.Log.Debug("ERROR: %+v", err)
			continue
		}
		var mods imageModifications
		mods.Scale = o.ImageUpperPPI / info.PPI
		if info.BitsPerComponent == 1 && info.ColorComponents == 1 {
			ratio := math.Round(info.PPI / o.ImageUpperPPI)
			pow2 := imageutil.NextPowerOf2(uint(ratio))
			if imageutil.InDelta(float64(pow2), 1/mods.Scale, 0.3) {
				mods.Scale = float64(1) / float64(pow2)
			}
			if _, isJBIG2 := xobj.Filter.(*core.JBIG2Encoder); !isJBIG2 {
				mods.Encoding = core.NewJBIG2Encoder()
			}
		}
		if err = applyImageModifications(xobj, mods); err != nil {
			common.Log.Debug("Error scale image keep original image: %s", err)
			continue
		}
		mods.Encoding = nil
		if smaskStream, ok := core.GetStream(info.Stream.PdfObjectDictionary.Get("SMask")); ok {
			smaskImg, err := model.NewXObjectImageFromStream(smaskStream)
			if err != nil {
				common.Log.Debug("ERROR: %+v", err)
				continue
			}
			if err = applyImageModifications(smaskImg, mods); err != nil {
				common.Log.Debug("ERROR: %+v", err)
				continue
			}
		}
	}
	return objects, nil
}

// Optimize implements Optimizer interface.
func (o *CleanUnusedResources) Optimize(objects []core.PdfObject) (result []core.PdfObject, err error) {
	unused, err := collectUnusedResources(objects)
	if err != nil {
		return nil, err
	}
	kept := []core.PdfObject{}
	for _, obj := range objects {
		_, isUnused := unused[obj]
		if isUnused {
			continue
		}
		kept = append(kept, obj)
	}
	return kept, nil
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

func subsetFont(stream *core.PdfObjectStream, runes []rune, glyphs []unitype.GlyphIndex) error {
	stream, ok := core.GetStream(stream)
	if !ok {
		common.Log.Debug("Embedded font object not found -- ABORT subsetting")
		return errors.New("fontfile2 not found")
	}
	data, err := core.DecodeStream(stream)
	if err != nil {
		common.Log.Debug("Decode error: %v", err)
		return err
	}
	font, err := unitype.Parse(bytes.NewReader(data))
	if err != nil {
		common.Log.Debug("Error parsing %d byte font", len(stream.Stream))
		return err
	}
	indices := glyphs
	if len(runes) > 0 {
		runeGlyphs := font.LookupRunes(runes)
		indices = append(indices, runeGlyphs...)
	}
	font, err = font.SubsetKeepIndices(indices)
	if err != nil {
		common.Log.Debug("ERROR subsetting font: %v", err)
		return err
	}
	var buf bytes.Buffer
	err = font.Write(&buf)
	if err != nil {
		common.Log.Debug("ERROR Writing font: %v", err)
		return err
	}
	if buf.Len() > len(data) {
		common.Log.Debug("Re-written font is larger than original - skip")
		return nil
	}
	newStream, err := core.MakeStream(buf.Bytes(), core.NewFlateEncoder())
	if err != nil {
		common.Log.Debug("ERROR Writing font: %v", err)
		return err
	}
	*stream = *newStream
	stream.Set("Length1", core.MakeInteger(int64(buf.Len())))
	return nil
}

// Optimize optimizes PDF objects to decrease PDF size.
func (o *CleanContentstream) Optimize(objects []core.PdfObject) (result []core.PdfObject, err error) {
	seen := map[*core.PdfObjectStream]struct{}{}
	var streams []*core.PdfObjectStream
	add := func(stream *core.PdfObjectStream) {
		if _, exists := seen[stream]; !exists {
			seen[stream] = struct{}{}
			streams = append(streams, stream)
		}
	}
	removed := map[core.PdfObject]bool{}
	added := map[core.PdfObject]bool{}
	for _, obj := range objects {
		switch typed := obj.(type) {
		case *core.PdfIndirectObject:
			switch inner := typed.PdfObject.(type) {
			case *core.PdfObjectDictionary:
				if typeName, ok := core.GetName(inner.Get("Type")); !ok || typeName.String() != "Page" {
					continue
				}
				if contents, ok := core.GetStream(inner.Get("Contents")); ok {
					add(contents)
				} else if arr, ok := core.GetArray(inner.Get("Contents")); ok {
					var parts []*core.PdfObjectStream
					for _, elem := range arr.Elements() {
						if s, ok := core.GetStream(elem); ok {
							parts = append(parts, s)
						}
					}
					if len(parts) > 0 {
						var buf bytes.Buffer
						for _, part := range parts {
							if data, err := core.DecodeStream(part); err == nil {
								buf.Write(data)
							}
							removed[part] = true
						}
						merged, err := core.MakeStream(buf.Bytes(), core.NewFlateEncoder())
						if err != nil {
							return nil, err
						}
						added[merged] = true
						inner.Set("Contents", merged)
						add(merged)
					}
				}
			}
		case *core.PdfObjectStream:
			if typeName, ok := core.GetName(typed.Get("Type")); !ok || typeName.String() != "XObject" {
				continue
			}
			if subtypeName, ok := core.GetName(typed.Get("Subtype")); !ok || subtypeName.String() != "Form" {
				continue
			}
			add(typed)
		}
	}
	for _, stream := range streams {
		err = cleanContentStream(stream)
		if err != nil {
			return nil, err
		}
	}
	result = nil
	for _, obj := range objects {
		if removed[obj] {
			continue
		}
		result = append(result, obj)
	}
	for obj := range added {
		result = append(result, obj)
	}
	return result, nil
}

// CleanUnusedResources represents an optimizer used to clean unused resources.
type CleanUnusedResources struct{}

func subsetFonts(objects []core.PdfObject) (subsetted map[*core.PdfObjectStream]struct{}, err error) {
	subsetted = map[*core.PdfObjectStream]struct{}{}
	fonts := map[*model.PdfFont]struct{}{}
	structure := findObjectStructure(objects)
	for _, page := range structure.pageObjects {
		pageDict, ok := core.GetDict(page.PdfObject)
		if !ok {
			continue
		}
		resDict, ok := core.GetDict(pageDict.Get("Resources"))
		if !ok {
			continue
		}
		contents, _ := decodeContents(pageDict.Get("Contents"))
		resources, err := model.NewPdfPageResourcesFromDict(resDict)
		if err != nil {
			return nil, err
		}
		items := []content{{contents: contents, resources: resources}}
		annots := annotationContents(pageDict.Get("Annots"))
		if annots != nil {
			items = append(items, annots...)
		}
		for _, item := range items {
			ext, err := extractor.NewFromContents(item.contents, item.resources)
			if err != nil {
				return nil, err
			}
			pageText, _, _, err := ext.ExtractPageText()
			if err != nil {
				return nil, err
			}
			for _, mark := range pageText.Marks().Elements() {
				if mark.Font == nil {
					continue
				}
				if _, ok := fonts[mark.Font]; !ok {
					fonts[mark.Font] = struct{}{}
				}
			}
		}
	}
	fontsByStream := map[*core.PdfObjectStream][]*model.PdfFont{}
	for font := range fonts {
		desc := font.FontDescriptor()
		if desc == nil || desc.FontFile2 == nil {
			continue
		}
		fontFile, ok := core.GetStream(desc.FontFile2)
		if !ok {
			continue
		}
		fontsByStream[fontFile] = append(fontsByStream[fontFile], font)
	}
	for fontStream := range fontsByStream {
		var runes []rune
		var glyphs []unitype.GlyphIndex
		for _, f := range fontsByStream[fontStream] {
			switch enc := f.Encoder().(type) {
			case textencoding.GIDRegistry:
				for _, gid := range enc.RegisteredGIDs() {
					glyphs = append(glyphs, unitype.GlyphIndex(gid))
				}
			case textencoding.RuneRegistry:
				runes = append(runes, enc.RegisteredRunes()...)
			case textencoding.SimpleEncoder:
				codes := enc.Charcodes()
				for _, code := range codes {
					r, ok := enc.CharcodeToRune(code)
					if !ok {
						common.Log.Debug("Charcode<->rune not found: %d", code)
						continue
					}
					runes = append(runes, r)
				}
			}
			if shaped, ok := f.Encoder().(textencoding.ShapedGlyphRegistry); ok {
				for _, gid := range shaped.RegisteredShapedGIDs() {
					glyphs = append(glyphs, unitype.GlyphIndex(gid))
				}
			}
		}
		if len(runes) == 0 && len(glyphs) == 0 {
			common.Log.Debug("No registered runes or indices - skip subsetting")
			continue
		}
		err = subsetFont(fontStream, runes, glyphs)
		if err != nil {
			common.Log.Debug("ERROR subsetting font stream: %v", err)
			return nil, err
		}
		subsetted[fontStream] = struct{}{}
	}
	return subsetted, nil
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

// CombineIdenticalIndirectObjects combines identical indirect objects.
// It implements interface model.Optimizer.
type CombineIdenticalIndirectObjects struct{}

func containsString(s string, list []string) bool {
	return slices.Contains(list, s)
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
type content struct {
	contents  string
	resources *model.PdfPageResources
}
