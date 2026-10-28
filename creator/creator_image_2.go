package creator

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"os"

	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// The Image type is used to draw an image onto PDF.
type Image struct {
	taggedDrawable
	xobj                  *model.XObjectImage
	img                   *model.Image
	altText               string
	angle                 float64
	width, height         float64
	origWidth, origHeight float64
	positioning           Positioning
	horizontalAlignment   HorizontalAlignment
	xPos                  float64
	yPos                  float64
	opacity               float64
	margins               Margins
	unusedA, unusedB      float64
	encoder               core.StreamEncoder
	fitMode               FitMode
	lazy                  bool
	mask                  *imageMask
	artifact              *model.Artifact
}

// SetEncoder sets the encoding/compression mechanism for the image.
//
// Note: for an image loaded from a JPEG file/data, setting a DCTEncoder (the
// default) embeds the original JPEG bytes unchanged, so encoder settings such
// as quality are not applied. Set a non-DCT encoder (e.g. a FlateEncoder) to
// force a re-encode.
func (im *Image) SetEncoder(encoder core.StreamEncoder) { im.encoder = encoder }

func newImageFromFile(name string) (*Image, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	img, err := model.ImageHandling.Read(file)
	if err != nil {
		common.Log.Error("Error loading image: %s", err)
		return nil, err
	}
	return newImage(img)
}

// SetAngle sets Image rotation angle in degrees.
func (im *Image) SetAngle(angle float64) { im.angle = angle }

// GetArtifact returns the artifact associated with the image, if any.
func (im *Image) GetArtifact() *model.Artifact { return im.artifact }

func newImageFromGoImage(img image.Image) (*Image, error) {
	image, err := model.ImageHandling.NewImageFromGoImage(img)
	if err != nil {
		return nil, err
	}
	return newImage(image)
}

func (im *Image) rotatedSize() (float64, float64) {
	width := im.width
	height := im.height
	angle := im.angle
	if angle == 0 {
		return width, height
	}
	boundingBox := contentdraw.Path{Points: []contentdraw.Point{contentdraw.NewPoint(0, 0).Rotate(angle), contentdraw.NewPoint(width, 0).Rotate(angle), contentdraw.NewPoint(0, height).Rotate(angle), contentdraw.NewPoint(width, height).Rotate(angle)}}.GetBoundingBox()
	return boundingBox.Width, boundingBox.Height
}

// Height returns Image's document height.
func (im *Image) Height() float64 { return im.height }

// Scale scales Image by a constant factor, both width and height.
func (im *Image) Scale(xFactor, yFactor float64) {
	im.width = xFactor * im.width
	im.height = yFactor * im.height
}

// SetAlternateText sets the alternate text for the image.
func (im *Image) SetAlternateText(text string) { im.altText = text }

// GeneratePageBlocks generate the Page blocks. Draws the Image on a block, implementing the Drawable interface.
func (im *Image) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	if im.xobj == nil {
		if err := im.makeXObject(); err != nil {
			return nil, ctx, err
		}
	}
	var blocks []*Block
	ctx2 := ctx
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	if im.positioning.IsRelative() {
		im.applyFitMode(ctx.Width)
		ctx.X += im.margins.Left
		ctx.Y += im.margins.Top
		ctx.Width -= im.margins.Left + im.margins.Right
		ctx.Height -= im.margins.Top + im.margins.Bottom
		if im.height > ctx.Height {
			blocks = append(blocks, block)
			block = NewBlock(ctx.PageWidth, ctx.PageHeight)
			ctx.Page++
			ctx2 := ctx
			ctx2.Y = ctx.Margins.Top + im.margins.Top
			ctx2.X = ctx.Margins.Left + im.margins.Left
			ctx2.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom - im.margins.Top - im.margins.Bottom
			ctx2.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right - im.margins.Left - im.margins.Right
			ctx = ctx2
		}
	} else {
		ctx.X = im.xPos
		ctx.Y = im.yPos
	}
	ctx, err := drawImageBlock(block, im, ctx)
	if err != nil {
		return nil, ctx, err
	}
	blocks = append(blocks, block)
	if im.positioning.IsAbsolute() {
		ctx = ctx2
	} else {
		ctx.X = ctx2.X
		ctx.Width = ctx2.Width
		ctx.Y += im.margins.Bottom
	}
	return blocks, ctx, nil
}

// ConvertToBinary converts current image data into binary (Bi-level image) format.
// If provided image is RGB or GrayScale the function converts it into binary image
// using histogram auto threshold method.
func (im *Image) ConvertToBinary() error { return im.img.ConvertToBinary() }

func newImage(img *model.Image) (*Image, error) {
	width := float64(img.Width)
	height := float64(img.Height)
	return &Image{img: img, origWidth: width, origHeight: height, width: width, height: height, angle: 0, opacity: 1.0, positioning: PositionRelative, taggedDrawable: taggedDrawable{structType: model.StructureTypeFigure}}, nil
}

// SetOpacity sets opacity for Image.
func (im *Image) SetOpacity(opacity float64) { im.opacity = opacity }

// SetStencilMask sets a 1-bit stencil mask (/ImageMask true XObject on /Mask)
// for the image: white areas of the mask image mark areas of the base image
// that are painted, black areas are masked out (PDF 32000-1:2008 8.9.6).
// Stencil masks are binary — for partial transparency use SetSoftMask, but a
// stencil stores 1 bit per pixel instead of 8 and is preferred when the mask
// is binary anyway.
//
// The optional threshold selects the grayscale luminance (0-255) at or above
// which a mask pixel counts as white; when omitted, a histogram-based auto
// threshold is used. A mask that is already 1 bit per component is used
// as-is, with set bits marking painted areas.
//
// The mask replaces any SMask derived automatically from the base image's
// alpha channel. An image can carry a single mask: setting a mask of a
// different type than a previously set one returns an error.
func (im *Image) SetStencilMask(mask *model.Image, threshold ...uint8) error {
	if mask == nil {
		return errors.New("mask image is nil")
	}
	if len(threshold) > 1 {
		return fmt.Errorf("expected at most one threshold value, got %d", len(threshold))
	}
	imageMask2 := &imageMask{kind: imageMaskStencil, img: mask}
	if len(threshold) == 1 {
		threshold2 := threshold[0]
		imageMask2.threshold = &threshold2
	}
	return im.setMask(imageMask2)
}

func (im *Image) applyFitMode(left float64) {
	left -= im.margins.Left + im.margins.Right
	switch im.fitMode {
	case FitModeFillWidth:
		im.ScaleToWidth(left)
	}
}

// SetFitMode sets the fit mode of the image.
// NOTE: The fit mode is only applied if relative positioning is used.
func (im *Image) SetFitMode(fitMode FitMode) { im.fitMode = fitMode }

// Lazy gets the lazy mode for the image.
func (im *Image) Lazy() bool { return im.lazy }

// SetWidth set the Image's document width to specified w. This does not change the raw image data, i.e.
// no actual scaling of data is performed. That is handled by the PDF viewer.
func (im *Image) SetWidth(w float64) { im.width = w }

// RemoveArtifact removes the artifact associated with the image, if any.
func (im *Image) RemoveArtifact() { im.artifact = nil }

var imageAttrs = map[string]attrApplyFunc{"src": nil, "align": enumAttr(horizontalAlignmentValues, func(img *Image, alignment HorizontalAlignment) { img.SetHorizontalAlignment(alignment) }), "opacity": floatAttr(func(img *Image, opacity float64) { img.SetOpacity(opacity) }), "margin": marginAttr(func(img *Image, margins Margins) {
	img.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
}), "fit-mode": enumAttr(fitModeValues, func(img *Image, fitMode FitMode) { img.SetFitMode(fitMode) }), "x": floatAttr(func(img *Image, x float64) { img.SetPos(x, img.yPos) }), "y": floatAttr(func(img *Image, y float64) { img.SetPos(img.xPos, y) }), "width": floatAttr(func(img *Image, w float64) { img.SetWidth(w) }), "height": floatAttr(func(img *Image, h float64) { img.SetHeight(h) }), "angle": floatAttr(func(img *Image, angle float64) { img.SetAngle(angle) }), "encoder": encoderAttr(func(img *Image, encoder core.StreamEncoder) { img.SetEncoder(encoder) })}

// FitMode returns the fit mode of the image.
func (im *Image) FitMode() FitMode { return im.fitMode }

func parseImageTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseImage(node)
}

func (im *Image) makeXObject() error {
	mask := im.mask
	var img2 *model.Image
	if mask != nil && mask.kind == imageMaskOptimizedAlpha {
		var ok bool
		if img2, ok = alphaToBitmask(im.img); !ok {
			common.Log.Debug("Image alpha channel is no longer binary; keeping it as an SMask")
			mask = nil
		}
	}
	if mask == nil {
		xObjectImage, err := model.NewXObjectImageFromImageLazy(im.img, nil, im.encoder, im.lazy)
		if err != nil {
			common.Log.Error("Failed to create xobject image: %s", err)
			return err
		}
		im.xobj = xObjectImage
		return nil
	}
	img := im.img
	encoder := im.encoder
	if len(im.img.AlphaData()) > 0 {
		img2 := *im.img
		img2.SetAlpha(nil)
		img = &img2
		if encoder == nil {
			encoder = core.NewFlateEncoder()
		}
	}
	if mask.kind == imageMaskColorKey && encoder == nil && !im.img.CanPassThroughJPEG() {
		encoder = core.NewFlateEncoder()
	}
	xObjectImage, err := model.NewXObjectImageFromImageLazy(img, nil, encoder, false)
	if err != nil {
		common.Log.Error("Failed to create xobject image: %s", err)
		return err
	}
	if err := im.applyMask(xObjectImage, img2); err != nil {
		return err
	}
	if im.lazy {
		if err := xObjectImage.MakeLazy(); err != nil {
			return err
		}
	}
	im.xobj = xObjectImage
	return nil
}

// GenerateKDict generates a K dictionary for the image, wiring the alternate
// text (see SetAlternateText) into the Figure structure element's /Alt entry.
func (im *Image) GenerateKDict() (*model.KDict, error) {
	kDict, err := im.taggedDrawable.GenerateKDict()
	if err != nil {
		return nil, err
	}
	if kDict != nil && im.altText != "" {
		kDict.Alt = core.MakeEncodedString(im.altText, true)
	}
	if kDict != nil && im.tagInfo != nil {
		im.tagInfo.ComponentKObj = kDict
	}
	return kDict, nil
}

// OptimizeAlphaMask converts a binary alpha channel — one where every pixel
// is either fully opaque or fully transparent — into a 1-bit stencil mask
// (see SetStencilMask), which stores one bit per alpha sample instead of the
// full-depth SMask the alpha channel would otherwise produce. It reports
// whether the conversion applies: an image without an alpha channel, with
// partial transparency anywhere, with an alpha depth other than 8 or 16 bits
// per sample, or with a mask already set is left unchanged.
//
// The stencil is derived from the image's alpha channel when the image is
// drawn, so later operations that change the pixels and alpha together (such
// as Crop) stay consistent with the mask. The underlying model.Image keeps
// its alpha channel; when no encoder is set with SetEncoder, the image is
// flate encoded, matching the encoding an alpha-carrying image receives.
func (im *Image) OptimizeAlphaMask() bool {
	if im.mask != nil {
		return false
	}
	if _, ok := alphaToBitmask(im.img); !ok {
		return false
	}
	return im.setMask(&imageMask{kind: imageMaskOptimizedAlpha}) == nil
}

// ScaleToHeight scale Image to a specified height h, maintaining the aspect ratio.
func (im *Image) ScaleToHeight(h float64) {
	width := im.width / im.height
	im.height = h
	im.width = h * width
}

// SetColorKeyMask sets a color key mask (/Mask range array) for the image:
// pixels whose component values all fall inside the given ranges are not
// painted (PDF 32000-1:2008 8.9.6.4). The ranges are given as [min1 max1 ...
// minN maxN] pairs in the image's color component order, e.g. 6 values for an
// RGB image, and are validated against the image's final colorspace and bit
// depth when the image is drawn.
//
// Color key masking compares raw integer sample values, before any Decode
// array mapping (PDF 32000-1:2008 8.9.6.4), so the ranges are given in raw
// sample units [0, 2^BitsPerComponent-1]. Exact matching makes it unreliable
// for lossily (DCT/JPEG) encoded pixels, where compression noise shifts
// sample values; when no encoder is set with SetEncoder, a color-key-masked
// image is therefore flate (losslessly) encoded, except for pass-through
// JPEG images, whose original samples are embedded unchanged.
//
// The mask replaces any SMask derived automatically from the base image's
// alpha channel. An image can carry a single mask: setting a mask of a
// different type than a previously set one returns an error.
func (im *Image) SetColorKeyMask(ranges []int) error {
	if len(ranges) == 0 || len(ranges)%2 != 0 {
		return fmt.Errorf("color key mask ranges must be non-empty [min max] pairs, got %d values", len(ranges))
	}
	for i := 0; i < len(ranges); i += 2 {
		if ranges[i] < 0 || ranges[i+1] < 0 {
			return fmt.Errorf("color key mask values must not be negative, got [%d %d]", ranges[i], ranges[i+1])
		}
		if ranges[i] > ranges[i+1] {
			return fmt.Errorf("color key mask range min %d exceeds max %d", ranges[i], ranges[i+1])
		}
	}
	nums := make([]int, len(ranges))
	copy(nums, ranges)
	return im.setMask(&imageMask{kind: imageMaskColorKey, colorKey: nums})
}

// SetPos sets the absolute position. Changes object positioning to absolute.
func (im *Image) SetPos(x, y float64) {
	im.positioning = PositionAbsolute
	im.xPos = x
	im.yPos = y
}

// Width returns Image's document width.
func (im *Image) Width() float64 { return im.width }

func (im *Image) applyMask(xObjectImage *model.XObjectImage, img2 *model.Image) error {
	switch im.mask.kind {
	case imageMaskSoft:
		grayMaskXObject, err := makeGrayMaskXObject(im.mask.img)
		if err != nil {
			return err
		}
		xObjectImage.SMask = grayMaskXObject.ToPdfObject()
	case imageMaskStencil, imageMaskOptimizedAlpha:
		img := img2
		var data []uint8
		if im.mask.kind == imageMaskStencil {
			img = im.mask.img
			if im.mask.threshold != nil {
				data = []uint8{*im.mask.threshold}
			}
		}
		xObjectImageStencilMask, err := model.NewXObjectImageStencilMask(img, nil, data...)
		if err != nil {
			return err
		}
		xObjectImage.Mask = xObjectImageStencilMask.ToPdfObject()
	case imageMaskColorKey:
		if err := validateColorKeyMask(im.mask.colorKey, xObjectImage); err != nil {
			return err
		}
		xObjectImage.Mask = core.MakeArrayFromIntegers(im.mask.colorKey)
	}
	return nil
}

func newImageFromBytes(b []byte) (*Image, error) {
	reader := bytes.NewReader(b)
	img, err := model.ImageHandling.Read(reader)
	if err != nil {
		common.Log.Error("Error loading image: %s", err)
		return nil, err
	}
	return newImage(img)
}

func (im *Image) setMask(mask *imageMask) error {
	if im.mask != nil && im.mask.kind != mask.kind {
		return errors.New("image already has a mask of a different type")
	}
	im.mask = mask
	im.xobj = nil
	return nil
}

// SetHorizontalAlignment sets the horizontal alignment of the image.
func (im *Image) SetHorizontalAlignment(alignment HorizontalAlignment) {
	im.horizontalAlignment = alignment
}

// SetSoftMask sets a soft mask (/SMask) for the image: an 8-bit grayscale
// alpha channel where white marks fully opaque areas of the image, black
// fully transparent ones, and intermediate values partial transparency.
//
// The mask image may have different dimensions and encoding than the base
// image and, unlike Image.SetAlpha on the underlying model image, works with
// pass-through JPEG images without touching their pixel data. A mask with
// more than one color component is converted to grayscale luminance; a
// single-component mask is used as-is. The mask's own alpha channel, if any,
// is ignored.
//
// The mask replaces any SMask derived automatically from the base image's
// alpha channel. An image can carry a single mask: setting a mask of a
// different type than a previously set one returns an error.
func (im *Image) SetSoftMask(mask *model.Image) error {
	if mask == nil {
		return errors.New("mask image is nil")
	}
	return im.setMask(&imageMask{kind: imageMaskSoft, img: mask})
}

// SetHeight sets the Image's document height to specified h.
func (im *Image) SetHeight(h float64) { im.height = h }

// SetLazy sets the lazy mode for the image.
func (im *Image) SetLazy(lazy bool) { im.lazy = lazy }

// SetMargins sets the margins for the Image (in relative mode): left, right, top, bottom.
func (im *Image) SetMargins(left, right, top, bottom float64) {
	im.margins.Left = left
	im.margins.Right = right
	im.margins.Top = top
	im.margins.Bottom = bottom
}

// GetMargins returns the Image's margins: left, right, top, bottom.
func (im *Image) GetMargins() (float64, float64, float64, float64) {
	return im.margins.Left, im.margins.Right, im.margins.Top, im.margins.Bottom
}

// MarkAsArtifact marks the image as an artifact of the specified type.
//
// This would override regular structure tagging and accessibility
// properties, and the image will be treated as an artifact in the PDF.
func (im *Image) MarkAsArtifact(artifactType model.ArtifactType) *model.Artifact {
	im.artifact = &model.Artifact{Type: artifactType}
	return im.artifact
}
