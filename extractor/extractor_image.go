package extractor

import (
	"errors"
	"image"
	"image/color"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/streamlimits"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

// ImageExtractOptions contains options for controlling image extraction from
// PDF pages.
type ImageExtractOptions struct{ IncludeInlineStencilMasks bool }

type imageExtractContext struct {
	images          []ImageMark
	inlineCount     int
	xObjectCount    int
	formCount       int
	xObjectImages   map[*core.PdfObjectStream]*model.Image
	inlineImages    map[inlineImageKey]*model.Image
	parsedStreams   map[*core.PdfObjectStream]*parsedImageStream
	visitedPatterns map[patternWalk]struct{}
	patternStack    []patternWalk
	ctm             transform.Matrix
	options         *ImageExtractOptions
	walker          streamlimits.Walker
}

func (iec *imageExtractContext) extractXObjectImage(name *core.PdfObjectName, gs contentstream.GraphicsState, resources *model.PdfPageResources) error {
	xobj, _ := resources.GetXObjectByName(*name)
	if xobj == nil {
		return nil
	}
	rgbImg, ok := iec.xObjectImages[xobj]
	if !ok {
		xImg, err := resources.GetXObjectImageByName(*name)
		if err != nil {
			return err
		}
		if xImg == nil {
			return nil
		}
		img, err := xImg.ToImage()
		if err != nil {
			return err
		}
		var mask image.Image
		if xImg.Mask != nil {
			if mask, err = loadMaskImage(xImg.Mask, color.Opaque); err != nil {
				common.Log.Debug("WARN: could not get explicit image mask. Output may be incorrect.")
			}
		} else if xImg.SMask != nil {
			mask, err = loadSoftMaskImage(xImg.SMask, color.Opaque)
			if err != nil {
				common.Log.Debug("WARN: could not get soft image mask. Output may be incorrect.")
			}
		}
		if mask != nil {
			goImg, err2 := img.ToGoImage()
			if err2 != nil {
				return err2
			}
			goImg = compositeImages(goImg, mask)
			switch xImg.ColorSpace.String() {
			case "DeviceGray", "Indexed":
				img, err2 = model.ImageHandling.NewGrayImageFromGoImage(goImg)
				if err2 != nil {
					return err2
				}
			default:
				img, err2 = model.ImageHandling.NewImageFromGoImage(goImg)
				if err2 != nil {
					return err2
				}
			}
		}
		rgb, err := xImg.ColorSpace.ImageToRGB(*img)
		if err != nil {
			return err
		}
		rgbImg = unpoolImage(rgb)
		iec.xObjectImages[xobj] = rgbImg
	}
	common.Log.Debug("@Do CTM: %s", gs.CTM.String())
	imgCopy := *rgbImg
	mark := ImageMark{Image: &imgCopy, Width: gs.CTM.ScalingFactorX(), Height: gs.CTM.ScalingFactorY(), Angle: gs.CTM.Angle()}
	mark.X, mark.Y = gs.CTM.Translation()
	iec.images = append(iec.images, mark)
	iec.xObjectCount++
	return nil
}

func (iec *imageExtractContext) extractFormImages(stream *core.PdfObjectStream, name *core.PdfObjectName, gs contentstream.GraphicsState, resources *model.PdfPageResources) error {
	ps, ok := iec.parsedStreams[stream]
	if !ok {
		xf, err := model.NewXObjectFormFromStream(stream)
		if err != nil {
			return err
		}
		content, err := xf.GetContentStream()
		if err != nil {
			return err
		}
		ops, err := contentstream.NewContentStreamParser(string(content)).Parse()
		if err != nil {
			return err
		}
		matrix, err := transform.MatrixFromPdfObject(xf.Matrix)
		if err != nil {
			common.Log.Debug("ERROR: form %s: %v; using identity", name, err)
		}
		ps = &parsedImageStream{ops: ops, resources: xf.Resources, matrix: matrix}
		iec.parsedStreams[stream] = ps
	}
	iec.walker.Charge(stream, len(*ps.ops), 0, 0)
	res := ps.resources
	if res == nil {
		res = resources
	}
	formGS := gs
	formGS.CTM = gs.CTM.Mult(ps.matrix)
	numImages, inlineCount, xObjectCount, formCount := len(iec.images), iec.inlineCount, iec.xObjectCount, iec.formCount
	numPatternStack := len(iec.patternStack)
	err2 := iec.extractOperationImages(ps.ops, res, &formGS)
	var fpe *formProcessingError
	if err2 != nil && !errors.As(err2, &fpe) && (newColorStateKey(gs.ColorspaceStroking, gs.ColorStroking) != blackColorKey || newColorStateKey(gs.ColorspaceNonStroking, gs.ColorNonStroking) != blackColorKey) {
		iec.images = iec.images[:numImages]
		iec.inlineCount, iec.xObjectCount, iec.formCount = inlineCount, xObjectCount, formCount
		for _, p := range iec.patternStack[numPatternStack:] {
			delete(iec.visitedPatterns, p)
		}
		iec.patternStack = iec.patternStack[:numPatternStack]
		err2 = iec.extractOperationImages(ps.ops, res, &contentstream.GraphicsState{CTM: formGS.CTM})
	}
	if err2 != nil {
		return &formProcessingError{name: name.String(), err: err2}
	}
	iec.formCount++
	return nil
}

func (iec *imageExtractContext) extractContentStreamImages(contents string, resources *model.PdfPageResources, gs *contentstream.GraphicsState) error {
	parser := contentstream.NewContentStreamParser(contents)
	ops, err := parser.Parse()
	if err != nil {
		return err
	}
	return iec.extractOperationImages(ops, resources, gs)
}

func (iec *imageExtractContext) extractInlineImage(inline *contentstream.ContentStreamInlineImage, gs contentstream.GraphicsState, resources *model.PdfPageResources) error {
	key := inlineImageKey{image: inline, resources: resources}
	rgbImg, ok := iec.inlineImages[key]
	if !ok {
		img, err := inline.ToImage(resources)
		if err != nil {
			return err
		}
		cs, err := inline.GetColorSpace(resources)
		if err != nil {
			return err
		}
		if cs == nil {
			cs = model.NewPdfColorspaceDeviceGray()
		}
		rgb, err := cs.ImageToRGB(*img)
		if err != nil {
			return err
		}
		rgbImg = unpoolImage(rgb)
		iec.inlineImages[key] = rgbImg
	}
	imgCopy := *rgbImg
	mark := ImageMark{Image: &imgCopy, Width: gs.CTM.ScalingFactorX(), Height: gs.CTM.ScalingFactorY(), Angle: gs.CTM.Angle()}
	mark.X, mark.Y = gs.CTM.Translation()
	iec.images = append(iec.images, mark)
	iec.inlineCount++
	return nil
}

func (iec *imageExtractContext) processOperand(op *contentstream.ContentStreamOperation, gs contentstream.GraphicsState, resources *model.PdfPageResources) error {
	if op.Operand == "BI" && len(op.Params) == 1 {
		inline, ok2 := op.Params[0].(*contentstream.ContentStreamInlineImage)
		if !ok2 {
			return nil
		}
		if isMask, ok := core.GetBoolVal(inline.ImageMask); ok {
			if isMask && !iec.options.IncludeInlineStencilMasks {
				return nil
			}
		}
		return iec.extractInlineImage(inline, gs, resources)
	} else if op.Operand == "Do" && len(op.Params) == 1 {
		name, ok := core.GetName(op.Params[0])
		if !ok {
			common.Log.Debug("ERROR: Type")
			return errTypeCheck
		}
		xobj, xType := resources.GetXObjectByName(*name)
		switch xType {
		case model.XObjectTypeImage:
			return iec.extractXObjectImage(name, gs, resources)
		case model.XObjectTypeForm:
			if !iec.walker.Enter(xobj, name.String()) {
				return nil
			}
			defer iec.walker.Leave(xobj)
			return iec.extractFormImages(xobj, name, gs, resources)
		}
	} else if (op.Operand == "scn" && isPatternColorspace(gs.ColorspaceNonStroking) || op.Operand == "SCN" && isPatternColorspace(gs.ColorspaceStroking)) && len(op.Params) == 1 {
		name, ok := core.GetName(op.Params[0])
		if !ok {
			common.Log.Debug("ERROR: Type")
			return errTypeCheck
		}
		pat, ok := resources.GetPatternByName(*name)
		if !ok {
			common.Log.Debug("ERROR: Pattern not found")
			return nil
		}
		if pat.IsTiling() {
			stream, ok2 := core.GetStream(core.TraceToDirectObject(pat.GetContainingPdfObject()))
			if !ok2 {
				return nil
			}
			pw := patternWalk{stream: stream, ctm: iec.ctm}
			if _, ok3 := iec.visitedPatterns[pw]; ok3 {
				return nil
			}
			if !iec.walker.Enter(stream, name.String()) {
				return nil
			}
			defer iec.walker.Leave(stream)
			ps, ok2 := iec.parsedStreams[stream]
			if !ok2 {
				tiling := pat.GetAsTilingPattern()
				content, err := tiling.GetContentStream()
				if err != nil {
					return err
				}
				ops, err := contentstream.NewContentStreamParser(string(content)).Parse()
				if err != nil {
					return err
				}
				matrix, err := transform.MatrixFromPdfObject(tiling.Matrix)
				if err != nil {
					common.Log.Debug("ERROR: pattern %s: %v; using identity", name, err)
				}
				ps = &parsedImageStream{ops: ops, resources: tiling.Resources, matrix: matrix}
				iec.parsedStreams[stream] = ps
			}
			iec.walker.Charge(stream, len(*ps.ops), 0, 0)
			iec.visitedPatterns[pw] = struct{}{}
			iec.patternStack = append(iec.patternStack, pw)
			patGS := contentstream.GraphicsState{CTM: iec.ctm.Mult(ps.matrix)}
			err2 := iec.extractOperationImages(ps.ops, ps.resources, &patGS)
			if err2 != nil {
				return err2
			}
		}
	}
	return nil
}

func (iec *imageExtractContext) extractOperationImages(ops *contentstream.ContentStreamOperations, resources *model.PdfPageResources, gs *contentstream.GraphicsState) error {
	if iec.xObjectImages == nil {
		iec.xObjectImages = map[*core.PdfObjectStream]*model.Image{}
		iec.inlineImages = map[inlineImageKey]*model.Image{}
		iec.parsedStreams = map[*core.PdfObjectStream]*parsedImageStream{}
		iec.visitedPatterns = map[patternWalk]struct{}{}
	}
	if iec.options == nil {
		iec.options = &ImageExtractOptions{}
	}
	savedCTM := iec.ctm
	iec.ctm = transform.IdentityMatrix()
	if gs != nil && gs.CTM != (transform.Matrix{}) {
		iec.ctm = gs.CTM
	}
	defer func() { iec.ctm = savedCTM }()
	proc := contentstream.NewContentStreamProcessor(*ops)
	if gs != nil {
		proc.SetInitialGraphicsState(*gs)
	}
	proc.AddHandler(contentstream.HandlerConditionEnumAllOperands, "", iec.processOperand)
	return proc.Process(resources)
}

// ImageMark represents an image drawn on a page and its position in device coordinates.
// All coordinates are in device coordinates.
type ImageMark struct {
	// Image is the image converted to RGB. Each mark has its own Image, but
	// marks of the same image drawn more than once on a page share its Data
	// and alpha. Release on one mark leaves the others intact; changing the
	// data in place, for example with AlphaMap, changes every mark of that
	// image, so copy it first.
	Image *model.Image

	// Dimensions of the image as displayed in the PDF.
	Width  float64
	Height float64

	// Position of the image in PDF coordinates (lower left corner).
	X float64
	Y float64

	// Angle in degrees, if rotated.
	Angle float64
}
