package creator

import (
	"image"
	"log"
	"math"
)

// Crop crops the Image to the specified bounds. Parts of the rectangle
// outside the image are padded with transparent pixels. The alpha channel,
// if any, is cropped along with the color samples, as is the matching region
// of a soft or stencil mask set on the image, and the image is rebuilt from
// the cropped data on its next draw.
func (im *Image) Crop(x0, y0, x1, y1 int) {
	rect := image.Rect(x0, y0, x1, y1)
	img, err := cropImage(im.img, rect)
	if err != nil {
		log.Fatalf("Error cropping image: %v", err)
	}
	if mask := im.mask; mask != nil && mask.img != nil && im.img.Width > 0 && im.img.Height > 0 {
		width, height := float64(mask.img.Width), float64(mask.img.Height)
		width2, height2 := float64(im.img.Width), float64(im.img.Height)
		rect2 := image.Rect(int(math.Floor(float64(rect.Min.X)*width/width2)), int(math.Floor(float64(rect.Min.Y)*height/height2)), int(math.Ceil(float64(rect.Max.X)*width/width2)), int(math.Ceil(float64(rect.Max.Y)*height/height2)))
		if rect2.Dx() < 1 {
			rect2.Max.X = rect2.Min.X + 1
		}
		if rect2.Dy() < 1 {
			rect2.Max.Y = rect2.Min.Y + 1
		}
		img, err := cropImage(mask.img, rect2)
		if err != nil {
			log.Fatalf("Error cropping image mask: %v", err)
		}
		img.SetAlpha(nil)
		mask.img = img
	}
	width := float64(img.Width)
	height := float64(img.Height)
	im.img = img
	im.origWidth = width
	im.origHeight = height
	im.width = width
	im.height = height
	im.xobj = nil
}

// ScaleToWidth scale Image to a specified width w, maintaining the aspect ratio.
func (im *Image) ScaleToWidth(w float64) {
	height := im.height / im.width
	im.width = w
	im.height = w * height
}

// GetHorizontalAlignment returns the horizontal alignment of the image.
func (im *Image) GetHorizontalAlignment() HorizontalAlignment { return im.horizontalAlignment }
