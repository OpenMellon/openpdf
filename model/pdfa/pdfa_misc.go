package pdfa

import (
	"encoding/xml"
	"errors"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/colorprofile"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
)

// DefaultProfile1Options are the default options for the Profile1.
func DefaultProfile1Options() *Profile1Options {
	return &Profile1Options{Now: time.Now, Xmp: XmpOptions{MarshalIndent: "\t"}}
}

func newColorspaceOptimizers(cmykDefault bool, std standardType) (pageColorspaceOptimizeFunc, documentColorspaceOptimizeFunc) {
	var hasRGB, hasGray, hasCMYK bool
	optimizePage := func(doc *docutil.Document, page *docutil.Page, imgs []*docutil.Image) error {
		hasGray = true
		for _, img := range imgs {
			switch img.Colorspace {
			case "DeviceGray":
				hasGray = true
			case "DeviceRGB":
				hasRGB = true
			case "DeviceCMYK":
				hasCMYK = true
			}
		}
		contents, ok := page.GetContents()
		if !ok {
			return nil
		}
		for _, content := range contents {
			data, err := content.GetData()
			if err != nil {
				continue
			}
			contentStreamParser := contentstream.NewContentStreamParser(string(data))
			ops, err := contentStreamParser.Parse()
			if err != nil {
				continue
			}
			for _, op := range *ops {
				switch op.Operand {
				case "G", "g":
					hasGray = true
				case "RG", "rg":
					hasRGB = true
				case "K", "k":
					hasCMYK = true
				case "CS", "cs":
					if len(op.Params) == 0 {
						continue
					}
					name, ok := core.GetName(op.Params[0])
					if !ok {
						continue
					}
					switch name.String() {
					case "RGB", "DeviceRGB":
						hasRGB = true
					case "G", "DeviceGray":
						hasGray = true
					case "CMYK", "DeviceCMYK":
						hasCMYK = true
					}
				}
			}
		}
		streams := page.FindXObjectForms()
		for _, stream := range streams {
			contentStreamParser := contentstream.NewContentStreamParser(string(stream.Stream))
			ops, err := contentStreamParser.Parse()
			if err != nil {
				continue
			}
			for _, op := range *ops {
				switch op.Operand {
				case "G", "g":
					hasGray = true
				case "RG", "rg":
					hasRGB = true
				case "K", "k":
					hasCMYK = true
				case "CS", "cs":
					if len(op.Params) == 0 {
						continue
					}
					name, ok := core.GetName(op.Params[0])
					if !ok {
						continue
					}
					switch name.String() {
					case "RGB", "DeviceRGB":
						hasRGB = true
					case "G", "DeviceGray":
						hasGray = true
					case "CMYK", "DeviceCMYK":
						hasCMYK = true
					}
				}
			}
			annotsArray, ok := core.GetArray(page.Object.Get("Annots"))
			if !ok {
				return nil
			}
			for _, obj := range annotsArray.Elements() {
				dict, ok := core.GetDict(obj)
				if !ok {
					continue
				}
				c := dict.Get("C")
				if c == nil {
					continue
				}
				cArray, ok := core.GetArray(c)
				if !ok {
					continue
				}
				switch cArray.Len() {
				case 0:
				case 1:
					hasGray = true
				case 3:
					hasRGB = true
				case 4:
					hasCMYK = true
				}
			}
		}
		return nil
	}
	optimizeDocument := func(doc *docutil.Document, imgs []*docutil.Image) error {
		catalog, ok := doc.FindCatalog()
		if !ok {
			return nil
		}
		outputIntents, ok := catalog.GetOutputIntents()
		if ok && outputIntents.Len() > 0 {
			return nil
		}
		if !ok {
			outputIntents = catalog.NewOutputIntents()
		}
		if !(hasRGB || hasCMYK || hasGray) {
			return nil
		}
		defer catalog.SetOutputIntents(outputIntents)
		if hasRGB && !hasCMYK && !hasGray {
			return addSRGBOutputIntent(doc, std, outputIntents)
		}
		if hasCMYK && !hasRGB && !hasGray {
			return addCMYKOutputIntent(std, outputIntents)
		}
		if hasGray && !hasRGB && !hasCMYK {
			return addOutputIntent(std, outputIntents)
		}
		if (hasRGB && hasCMYK) || (hasRGB && hasGray) || (hasCMYK && hasGray) {
			if err := convertImagesColorspace(imgs, cmykDefault); err != nil {
				return err
			}
			if err := convertPageContentColors(doc, cmykDefault); err != nil {
				return err
			}
			if err := optimizeFormXObjectColors(doc, cmykDefault); err != nil {
				return err
			}
			if err := fixAnnotationColors(doc, cmykDefault); err != nil {
				return err
			}
			if cmykDefault {
				return addCMYKOutputIntent(std, outputIntents)
			}
			return addSRGBOutputIntent(doc, std, outputIntents)
		}
		return nil
	}
	return optimizePage, optimizeDocument
}

// DefaultProfile4Options the default options for the Profile4.
func DefaultProfile4Options() *Profile4Options {
	return &Profile4Options{Now: time.Now, Xmp: XmpOptions{MarshalIndent: "\t"}}
}

func parseDescriptionProperties(dec *xml.Decoder) []xmpPropertyValue {
	var props []xmpPropertyValue
	for {
		tok, err := dec.Token()
		if err != nil {
			return props
		}
		switch tok := tok.(type) {
		case xml.EndElement:
			return props
		case xml.StartElement:
			prop, err := parsePropertyElement(dec, tok)
			if err != nil {
				return props
			}
			props = append(props, prop)
		}
	}
}

func parseArrayItems(dec *xml.Decoder) (items []xmpArrayItem, ok bool, err error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return items, ok, err
		}
		switch tok := tok.(type) {
		case xml.EndElement:
			return items, ok, nil
		case xml.StartElement:
			if tok.Name.Space != rdfNamespace || tok.Name.Local != "li" {
				if err := skipElement(dec, 0); err != nil {
					return items, ok, err
				}
				continue
			}
			for _, attr := range tok.Attr {
				if attr.Name.Space == xmlNamespace && attr.Name.Local == "lang" {
					ok = true
				}
			}
			item := xmpArrayItem{Struct: isResourceParseType(tok)}
			s, contentKind, err := readContent(dec)
			if err != nil {
				return items, ok, err
			}
			item.Text, item.Struct = resolveItemText(s, contentKind, item.Struct)
			items = append(items, item)
		}
	}
}

func addCMYKOutputIntent(std standardType, intents *docutil.OutputIntents) error {
	cmykIsoCoatedV2OutputIntent, err := colorprofile.NewCmykIsoCoatedV2OutputIntent(std.outputIntentSubtype())
	if err != nil {
		return err
	}
	if err = intents.Add(cmykIsoCoatedV2OutputIntent.ToPdfObject()); err != nil {
		return err
	}
	return nil
}

func (d *documentImages) hasOnlyDeviceCMYK() bool { return d.hasCMYK && !d.hasRGB && !d.hasGray }

func (d *documentImages) hasOnlyDeviceGray() bool { return d.hasGray && !d.hasRGB && !d.hasCMYK }

func collectDocumentImages(objs []core.PdfObject) (*documentImages, error) {
	name := core.PdfObjectName("Subtype")
	seen := make(map[*core.PdfObjectStream]struct{})
	set := make(map[core.PdfObject]struct{})
	var (
		hasRGB, hasCMYK, hasGray bool
		imgInfos                 []*imageInfo
		err                      error
	)
	for _, obj := range objs {
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		if _, ok := seen[stream]; ok {
			continue
		}
		seen[stream] = struct{}{}
		obj2 := stream.PdfObjectDictionary.Get(name)
		obj2Name, ok := core.GetName(obj2)
		if !ok || string(*obj2Name) != "Image" {
			continue
		}
		if sMask := stream.PdfObjectDictionary.Get("SMask"); sMask != nil {
			set[sMask] = struct{}{}
		}
		imgInfo := imageInfo{BitsPerComponent: 8, Stream: stream}
		imgInfo.ColorSpace, err = model.DetermineColorspaceNameFromPdfObject(stream.PdfObjectDictionary.Get("ColorSpace"))
		if err != nil {
			return nil, err
		}
		if bitsPerComponent, ok := core.GetIntVal(stream.PdfObjectDictionary.Get("BitsPerComponent")); ok {
			imgInfo.BitsPerComponent = bitsPerComponent
		}
		if width, ok := core.GetIntVal(stream.PdfObjectDictionary.Get("Width")); ok {
			imgInfo.Width = width
		}
		if height, ok := core.GetIntVal(stream.PdfObjectDictionary.Get("Height")); ok {
			imgInfo.Height = height
		}
		switch imgInfo.ColorSpace {
		case "DeviceGray":
			hasGray = true
			imgInfo.ColorComponents = 1
		case "DeviceRGB":
			hasRGB = true
			imgInfo.ColorComponents = 3
		case "DeviceCMYK":
			hasCMYK = true
			imgInfo.ColorComponents = 4
		default:
			imgInfo.otherColorspace = true
		}
		imgInfos = append(imgInfos, &imgInfo)
	}
	if len(set) > 0 {
		if len(set) == len(imgInfos) {
			imgInfos = nil
		} else {
			imgInfos2 := make([]*imageInfo, len(imgInfos)-len(set))
			var n int
			for _, imgInfo := range imgInfos {
				if _, ok := set[imgInfo.Stream]; ok {
					continue
				}
				imgInfos2[n] = imgInfo
				n++
			}
			imgInfos = imgInfos2
		}
	}
	return &documentImages{hasRGB: hasRGB, hasCMYK: hasCMYK, hasGray: hasGray, softMasks: set, images: imgInfos}, nil
}

// DefaultProfile2Options are the default options for the Profile2.
func DefaultProfile2Options() *Profile2Options {
	return &Profile2Options{Now: time.Now, Xmp: XmpOptions{MarshalIndent: "\t"}}
}

func (d *documentImages) hasOnlyDeviceRGB() bool { return d.hasRGB && !d.hasCMYK && !d.hasGray }

func arrayKindFromName(s string) xmpArrayKind {
	switch s {
	case "Seq":
		return xmpArraySeq
	case "Bag":
		return xmpArrayBag
	case "Alt":
		return xmpArrayAlt
	}
	return xmpArrayNone
}

func loadCMap(stream *core.PdfObjectStream, streamData map[*core.PdfObjectStream][]byte, cmaps map[*core.PdfObjectStream]*cmap.CMap) (*cmap.CMap, error) {
	cm, ok := cmaps[stream]
	if !ok {
		var err error
		data, ok := streamData[stream]
		if !ok {
			data, err = core.DecodeStream(stream)
			if err != nil {
				common.Log.Debug("decoding stream failed: %v", err)
				return nil, err
			}
			streamData[stream] = data
		}
		cm, err = cmap.LoadCmapFromData(data, false)
		if err != nil {
			return nil, err
		}
		cmaps[stream] = cm
	}
	return cm, nil
}

// DefaultProfile3Options the default options for the Profile3.
func DefaultProfile3Options() *Profile3Options {
	return &Profile3Options{Now: time.Now, Xmp: XmpOptions{MarshalIndent: "\t"}}
}

func catalogDict(r *model.CompliancePdfReader) (*core.PdfObjectDictionary, bool) {
	trailer, err := r.GetTrailer()
	if err != nil {
		common.Log.Debug("Cannot get document trailer: %v", err)
		return nil, false
	}
	ref, ok := trailer.Get("Root").(*core.PdfObjectReference)
	if !ok {
		common.Log.Debug("Cannot find document root")
		return nil, false
	}
	refDict, ok := core.GetDict(core.ResolveReference(ref))
	if !ok {
		common.Log.Debug("cannot resolve catalog dictionary")
		return nil, false
	}
	return refDict, true
}

func (d *documentImages) hasUncalibratedImages() bool { return d.hasRGB || d.hasCMYK || d.hasGray }

type documentImages struct {
	hasRGB, hasCMYK, hasGray bool
	softMasks                map[core.PdfObject]struct{}
	images                   []*imageInfo
}

type documentColorspaceOptimizeFunc func(doc *docutil.Document, imgs []*docutil.Image) error

func applyImageModifications(img *model.XObjectImage, imageModifications2 imageModifications) error {
	image, err := img.ToImage()
	if err != nil {
		return err
	}
	if imageModifications2.encoder != nil {
		img.Filter = imageModifications2.encoder
	}
	dict := core.MakeDict()
	dict.Set("Quality", core.MakeInteger(100))
	dict.Set("Predictor", core.MakeInteger(1))
	img.Decode = nil
	if err = img.SetImage(image, nil); err != nil {
		return err
	}
	img.ToPdfObject()
	return nil
}

func setCatalogVersion(doc *docutil.Document) error {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return errors.New("catalog not found")
	}
	catalog.SetVersion()
	return nil
}
