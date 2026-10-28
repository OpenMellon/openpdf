package optimize

import (
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/model"
)

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

// ImagePPI optimizes images by scaling images such that the PPI (pixels per inch) is never higher than ImageUpperPPI.
// TODO(a5i): Add support for inline images.
// It implements interface model.Optimizer.
type ImagePPI struct{ ImageUpperPPI float64 }

// Image optimizes images by rewrite images into JPEG format with quality equals to ImageQuality.
// TODO(a5i): Add support for inline images.
// It implements interface model.Optimizer.
type Image struct{ ImageQuality int }

type imageModifications struct {
	Scale    float64
	Encoding core.StreamEncoder
}

type imageInfo struct {
	BitsPerComponent int
	ColorComponents  int
	Width            int
	Height           int
	Stream           *core.PdfObjectStream
	PPI              float64
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
