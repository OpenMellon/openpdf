package render

import (
	"errors"
	"fmt"
	"image"
	imagedraw "image/draw"
	"math"
	"path/filepath"
	"strings"

	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/render/internal/context/imagerender"
	xdraw "golang.org/x/image/draw"
)

// RenderWithOpts converts the specified PDF page into an image, optionally flattens annotations and returns the result.
func (d *ImageDevice) RenderWithOpts(page *model.PdfPage, skipFlattening bool) (image.Image, error) {
	mediaBox, err := page.GetMediaBox()
	if err != nil {
		return nil, err
	}
	mediaBox.Normalize()
	cropBox := page.CropBox
	var cropW, cropH float64
	if cropBox != nil {
		cropBox.Normalize()
		cropW, cropH = cropBox.Width(), cropBox.Height()
	}
	rotate := page.Rotate
	originX, originY, pageW, pageH := mediaBox.Llx, mediaBox.Lly, mediaBox.Width(), mediaBox.Height()
	pageMatrix := transform.IdentityMatrix()
	if rotate != nil && *rotate%360 != 0 && *rotate%90 == 0 {
		angle := -float64(*rotate)
		rotated := rotatedBBox(pageW, pageH, angle)
		pageMatrix = pageMatrix.Translate((rotated.Width-pageW)/2+pageW/2, (rotated.Height-pageH)/2+pageH/2).Rotate(angle*math.Pi/180).Translate(-pageW/2, -pageH/2)
		pageW, pageH = rotated.Width, rotated.Height
		if cropBox != nil {
			rotatedCrop := rotatedBBox(cropW, cropH, angle)
			cropW, cropH = rotatedCrop.Width, rotatedCrop.Height
		}
	}
	if originX != 0 || originY != 0 {
		pageMatrix = pageMatrix.Translate(-originX, -originY)
	}
	d.scale = 1.0
	if d.OutputWidth != 0 {
		baseW := pageW
		if cropBox != nil {
			baseW = cropW
		}
		d.scale = float64(d.OutputWidth) / baseW
		pageW, pageH, cropW, cropH = pageW*d.scale, pageH*d.scale, cropW*d.scale, cropH*d.scale
		pageMatrix = transform.ScaleMatrix(d.scale, d.scale).Mult(pageMatrix)
	}
	ctx := imagerender.NewContext(int(pageW), int(pageH))
	ctx.SetInterpolator(d.interpolator)
	ctx.SetGlyphCache(d.glyphCache)
	defer ctx.ReleaseBuffers()
	if err := d.renderPage(ctx, page, pageMatrix, skipFlattening); err != nil {
		return nil, err
	}
	img := ctx.Image()
	if cropBox != nil {
		cropX, cropY := (cropBox.Llx-originX)*d.scale, (cropBox.Lly-originY)*d.scale
		cropRect := image.Rect(0, 0, int(cropW), int(cropH))
		srcPt := image.Pt(int(cropX), int(pageH-cropY-cropH))
		cropped := image.NewRGBA(cropRect)
		imagedraw.Draw(cropped, cropRect, img, srcPt, imagedraw.Src)
		img = cropped
	}
	return img, nil
}

// Render converts the specified PDF page into an image, flattens annotations by default and returns the result.
func (d *ImageDevice) Render(page *model.PdfPage) (image.Image, error) {
	return d.RenderWithOpts(page, false)
}

// NewImageDevice returns a new image device.
func NewImageDevice() *ImageDevice {
	d := &ImageDevice{renderer: &renderer{}, interpolator: xdraw.BiLinear, glyphCache: imagerender.NewGlyphCache()}
	return d
}

// RenderToPath converts the specified PDF page into an image and saves the
// result at the specified location.
func (d *ImageDevice) RenderToPath(page *model.PdfPage, outputPath string) error {
	img, err := d.Render(page)
	if err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(outputPath))
	if ext == "" {
		return errors.New("could not recognize output file type")
	}
	switch ext {
	case ".png":
		return savePNG(outputPath, img)
	case ".jpg", ".jpeg":
		return saveJPEG(outputPath, img, 100)
	}
	return fmt.Errorf("unrecognized output file type: %s", ext)
}

// ImageDevice is used to render PDF pages to image targets.
type ImageDevice struct {
	*renderer

	// OutputWidth represents the width of the rendered images in pixels.
	// The heights of the output images are calculated based on the selected
	// width and the original height of each rendered page.
	OutputWidth  int
	interpolator xdraw.Interpolator
	glyphCache   *imagerender.GlyphCache
}

// SetInterpolator sets the interpolation algorithm to use for image scaling and transformations.
// The interpolator determines the quality of image scaling. Available interpolators from
// golang.org/x/image/draw include:
//   - draw.NearestNeighbor (fastest, lowest quality)
//   - draw.ApproxBiLinear (fast, good quality)
//   - draw.BiLinear (balanced, default)
//   - draw.CatmullRom (high quality, slower)
func (d *ImageDevice) SetInterpolator(interpolator xdraw.Interpolator) {
	d.interpolator = interpolator
	d.tilingCache = nil
}
