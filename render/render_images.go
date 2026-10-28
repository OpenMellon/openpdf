package render

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/render/internal/context"
	xdraw "golang.org/x/image/draw"
)

func savePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func setFillFromColorspace(ctx context.Context, cs model.PdfColorspace, c model.PdfColor) error {
	if isPatternColorspace(cs) {
		return nil
	}
	rgb, err := cs.ColorToRGB(c)
	if err != nil {
		return err
	}
	if rgbColor, ok := rgb.(*model.PdfColorDeviceRGB); ok {
		ctx.SetFillRGBA(rgbColor.R(), rgbColor.G(), rgbColor.B(), 1)
	}
	return nil
}

func saveJPEG(path string, img image.Image, quality int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return jpeg.Encode(f, img, &jpeg.Options{Quality: quality})
}

func isPatternColorspace(cs model.PdfColorspace) bool {
	_, ok := cs.(*model.PdfColorspaceSpecialPattern)
	return ok
}

func maskImageToRGBA(img *model.Image) image.Image {
	w, h := int(img.Width), int(img.Height)
	if w <= 0 || h <= 0 {
		common.Log.Debug("invalid image dimensions (%d, %d)", w, h)
		return nil
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c, err := img.ColorAt(x, y)
			if err != nil {
				common.Log.Debug("could not retrieve image mask value at (%d, %d). Output may be incorrect.", x, y)
				continue
			}
			r, g, b, _ := c.RGBA()
			var px color.Color
			if r+g+b == 0 {
				px = color.Transparent
			} else {
				gray := uint8(core.RGBToGrayscale(int(r>>8), int(g>>8), int(b>>8)))
				px = color.RGBA{R: 255, G: 255, B: 255, A: gray}
			}
			out.Set(x, y, px)
		}
	}
	return out
}

func applyImageMask(img, mask image.Image, interp xdraw.Interpolator) image.Image {
	maskSize, imgSize := mask.Bounds().Size(), img.Bounds().Size()
	w, h := maskSize.X, maskSize.Y
	if imgSize.X > w {
		w = imgSize.X
	}
	if imgSize.Y > h {
		h = imgSize.Y
	}
	rect := image.Rect(0, 0, w, h)
	if maskSize.X != w || maskSize.Y != h {
		scaledMask := image.NewRGBA(rect)
		interp.Scale(scaledMask, rect, mask, mask.Bounds(), xdraw.Over, nil)
		mask = scaledMask
	}
	if imgSize.X != w || imgSize.Y != h {
		scaledImg := image.NewRGBA(rect)
		interp.Scale(scaledImg, rect, img, img.Bounds(), xdraw.Over, nil)
		img = scaledImg
	}
	out := image.NewRGBA(rect)
	xdraw.DrawMask(out, rect, img, image.Point{}, mask, image.Point{}, xdraw.Over)
	return out
}

func stencilMaskImage(img *model.Image, c color.Color) image.Image {
	w, h := int(img.Width), int(img.Height)
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			px, err := img.ColorAt(x, y)
			if err != nil {
				common.Log.Debug("could not retrieve image mask value at (%d, %d). Output may be incorrect.", x, y)
				continue
			}
			r, g, b, _ := px.RGBA()
			var outPx color.Color
			if r+g+b == 0 {
				outPx = c
			} else {
				outPx = color.Transparent
			}
			out.Set(x, y, outPx)
		}
	}
	return out
}

func fillAndStroke(ctx context.Context, gs contentstream.GraphicsState, rule context.FillRule, newSubPath bool) error {
	if err := setFillFromColorspace(ctx, gs.ColorspaceNonStroking, gs.ColorNonStroking); err != nil {
		return err
	}
	if newSubPath {
		ctx.NewSubPath()
	}
	ctx.SetFillRule(rule)
	ctx.FillPreserve()
	if err := setStrokeFromColorspace(ctx, gs.ColorspaceStroking, gs.ColorStroking); err != nil {
		return err
	}
	ctx.Stroke()
	return nil
}

func explicitMaskImage(obj core.PdfObject, c color.Color) (image.Image, error) {
	stream, ok := core.GetStream(obj)
	if !ok {
		return nil, nil
	}
	xi, err := model.NewXObjectImageFromStream(stream)
	if err != nil {
		return nil, err
	}
	img, err := xi.ToImage()
	if err != nil {
		return nil, err
	}
	return stencilMaskImage(img, c), nil
}

func setStrokeFromColorspace(ctx context.Context, cs model.PdfColorspace, c model.PdfColor) error {
	if isPatternColorspace(cs) {
		return nil
	}
	rgb, err := cs.ColorToRGB(c)
	if err != nil {
		return err
	}
	if devRGB, ok := rgb.(*model.PdfColorDeviceRGB); ok {
		ctx.SetStrokeRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
	}
	return nil
}
