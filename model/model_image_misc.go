package model

import (
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	"io"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/sampling"
)

// ToJBIG2Image converts current image to the core.JBIG2Image.
func (img *Image) ToJBIG2Image() (*core.JBIG2Image, error) {
	img2, err := img.ToGoImage()
	if err != nil {
		return nil, err
	}
	return core.GoImageToJBIG2(img2, core.JB2ImageAutoThreshold)
}

func (img Image) getBase() imageutil.ImageBase {
	return imageutil.NewImageBase(int(img.Width), int(img.Height), int(img.BitsPerComponent), img.ColorComponents, img.Data, img.alpha, img.decode)
}

var ImageHandling ImageHandler = DefaultImageHandler{}

// CanPassThroughJPEG reports whether the image still holds the original JPEG
// bytes of an 8-bit grayscale or RGB source, which a DCT encoder (including
// the default when no encoder is set) embeds unchanged instead of decoding
// and re-encoding.
func (img *Image) CanPassThroughJPEG() bool { return img.canPassThroughJPEG() }

// SetDecode sets the decode array: a (min, max) pair per color component.
// For gray images the pairs are in normalized component units, so that
// [0 1] is the identity and [1 0] inverts; for an Indexed image the index
// range is expressed as a fraction of 2^BitsPerComponent-1, as
// XObjectImage.ToImage produces it. Lab and colour-managed ICCBased images
// take their component ranges as is. DeviceRGB, DeviceCMYK and ICCBased
// images without a managed transform hand the array unscaled to the device
// sample readers, and the producers differ: XObjectImage.ToImage stores the
// PDF's values, inline images store sample units (0 to 2^BitsPerComponent-1)
// as those readers expect; unifying the two is pending.
func (img *Image) SetDecode(decode []float64) { img.decode = decode }

func (img *Image) canPassThroughJPEG() bool {
	return img.encodedFormat == "jpeg" && len(img.encodedData) > 0 && img.BitsPerComponent == 8 && (img.ColorComponents == 1 || img.ColorComponents == 3)
}

// Decode returns the image's decode array ([Dmin Dmax ...] values for each
// color component), or nil when the default sample mapping applies. The
// returned slice must not be mutated; use SetDecode to change it.
func (img *Image) Decode() []float64 { return img.decode }

func imageFromImageBase(base *imageutil.ImageBase) (img Image) {
	img.Width = int64(base.Width)
	img.Height = int64(base.Height)
	img.BitsPerComponent = int64(base.BitsPerComponent)
	img.ColorComponents = base.ColorComponents
	img.Data = base.Data
	img.decode = base.Decode
	img.alpha = base.Alpha
	return img
}

// EncodedData returns the original, undecoded source bytes retained for
// direct embedding (currently JPEG only), or nil if unavailable. The returned
// slice must not be mutated.
func (img *Image) EncodedData() []byte { return img.encodedData }

// ColorAt returns the color of the image pixel specified by the x and y coordinates.
func (img *Image) ColorAt(x, y int) (color.Color, error) {
	bytesPerLine := imageutil.BytesPerLine(int(img.Width), int(img.BitsPerComponent), img.ColorComponents)
	switch img.ColorComponents {
	case 1:
		return imageutil.ColorAtGrayscale(x, y, int(img.BitsPerComponent), bytesPerLine, img.Data, img.decode)
	case 3:
		return imageutil.ColorAtNRGBA(x, y, int(img.Width), bytesPerLine, int(img.BitsPerComponent), img.Data, img.alpha, img.decode)
	case 4:
		return imageutil.ColorAtCMYK(x, y, int(img.Width), img.Data, img.decode)
	}
	common.Log.Debug("ERROR: unsupported image. %d components, %d bits per component", img.ColorComponents, img.BitsPerComponent)
	return nil, errors.New("unsupported image colorspace")
}

// SetImageHandler sets the image handler used by the package.
func SetImageHandler(imgHandling ImageHandler) { ImageHandling = imgHandling }

// AlphaData returns the image's alpha channel data (stored with the image's
// bits per component and a single color component), or nil if the image has
// none. The returned slice must not be mutated; use SetAlpha or AlphaMap to
// change the alpha channel.
func (img *Image) AlphaData() []byte { return img.alpha }

func (img *Image) clearSourceMetadata() {
	img.iccProfile = nil
	img.encodedData = nil
	img.encodedFormat = ""
}

// GetParamsDict returns *core.PdfObjectDictionary with a set of basic image parameters.
func (img *Image) GetParamsDict() *core.PdfObjectDictionary {
	dict := core.MakeDict()
	dict.Set("Width", core.MakeInteger(img.Width))
	dict.Set("Height", core.MakeInteger(img.Height))
	dict.Set("ColorComponents", core.MakeInteger(int64(img.ColorComponents)))
	dict.Set("BitsPerComponent", core.MakeInteger(img.BitsPerComponent))
	return dict
}

// SetAlpha sets the alpha layer for the image.
func (img *Image) SetAlpha(alpha []byte) { img.alpha = alpha }

// ImageHandler interface implements common image loading and processing tasks.
// Implementing as an interface allows for the possibility to use non-standard libraries for faster
// loading and processing of images.
type ImageHandler interface {
	// Read any image type and load into a new Image object.
	Read(r io.Reader) (*Image, error)

	// NewImageFromGoImage loads a NRGBA32 OpenPDF Image from a standard Go image structure.
	NewImageFromGoImage(img image.Image) (*Image, error)

	// NewGrayImageFromGoImage loads a grayscale OpenPDF Image from a standard Go image structure.
	NewGrayImageFromGoImage(img image.Image) (*Image, error)

	// Compress an image.
	Compress(img *Image, n int64) (*Image, error)
}

// Release returns the image's data buffers to the shared byte pool.
// After Release, Image.Data and the alpha channel are nil and the Image
// must not be used further.
//
// The pool classifies buffers by capacity, not origin: buffers whose cap
// exactly matches a bucket size are accepted for reuse regardless of
// whether they came from AcquireBytes or from a plain make; buffers whose
// cap does not match are dropped. In practice this means Release is safe
// to call on any Image, but callers that assign Image.Data via make and
// happen to hit a bucket size will hand that slab to the pool — the
// "must not be used further" rule is what keeps that safe.
//
// Release is optional. Callers that don't invoke it pay normal GC cost.
func (img *Image) Release() {
	if img == nil {
		return
	}
	if img.Data != nil {
		core.ReleaseBytes(img.Data)
		img.Data = nil
	}
	if img.alpha != nil {
		core.ReleaseBytes(img.alpha)
		img.alpha = nil
	}
}

// ToGoImage converts the OpenPDF Image to a golang Image structure.
func (img *Image) ToGoImage() (image.Image, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Converting to go image")
	}
	img2, err := imageutil.NewImage(int(img.Width), int(img.Height), int(img.BitsPerComponent), img.ColorComponents, img.Data, img.alpha, img.decode)
	if err != nil {
		return nil, err
	}
	return img2, nil
}

func (img *Image) resampleLowBits(nums []uint32) {
	bytesPerLine := imageutil.BytesPerLine(int(img.Width), int(img.BitsPerComponent), img.ColorComponents)
	acquireBytes := core.AcquireBytes(bytesPerLine * int(img.Height))
	clear(acquireBytes)
	n := img.ColorComponents * int(img.Width)
	var (
		count, num int
		size       uint32
	)
	for i := 0; i < int(img.Height); i++ {
		num = i * bytesPerLine
		b := uint8(8)
		for i := 0; i < n; i++ {
			if count >= len(nums) {
				break
			}
			size = nums[count]
			b -= uint8(img.BitsPerComponent)
			acquireBytes[num] |= byte(size) << b
			if b == 0 {
				b = 8
				num++
			}
			count++
		}
	}
	img.Data = acquireBytes
}

// ICCProfile returns the raw ICC colour profile preserved from the source
// image, or nil if the source carried none. The returned slice must not be
// mutated.
func (img *Image) ICCProfile() []byte { return img.iccProfile }

// AlphaMap performs mapping of alpha data for transformations. Allows custom filtering of alpha data etc.
func (img *Image) AlphaMap(mapFunc AlphaMapFunc) {
	for i, alpha := range img.alpha {
		img.alpha[i] = mapFunc(alpha)
	}
}

// EncodedFormat returns the container format of the bytes returned by
// EncodedData ("jpeg"), or "" when none are retained.
func (img *Image) EncodedFormat() string { return img.encodedFormat }

// Image interface is a basic representation of an image used in PDF.
// The colorspace is not specified, but must be known when handling the image.
type Image struct {
	Width            int64
	Height           int64
	BitsPerComponent int64
	ColorComponents  int
	Data             []byte
	alpha            []byte
	decode           []float64
	iccProfile       []byte
	encodedData      []byte
	encodedFormat    string
}

// SetICCProfile sets the raw ICC colour profile describing the image samples,
// as embedded by NewXObjectImageFromImage. It must match the current samples;
// nil clears the profile.
func (img *Image) SetICCProfile(profile []byte) { img.iccProfile = profile }

// Resample resamples the image data converting from current BitsPerComponent to a target BitsPerComponent
// value.  Sets the image's BitsPerComponent to the target value following resampling.
//
// For example, converting an 8-bit RGB image to 1-bit grayscale (common for scanned images):
//
//	// Convert RGB image to grayscale.
//	rgbColorSpace := pdf.NewPdfColorspaceDeviceRGB()
//	grayImage, err := rgbColorSpace.ImageToGray(rgbImage)
//	if err != nil {
//	  return err
//	}
//	// Resample as 1 bit.
//	grayImage.Resample(1)
func (img *Image) Resample(targetBitsPerComponent int64) {
	if img.BitsPerComponent == targetBitsPerComponent {
		return
	}
	img.clearSourceMetadata()
	samples := img.GetSamples()
	if targetBitsPerComponent < img.BitsPerComponent {
		n := img.BitsPerComponent - targetBitsPerComponent
		for i := range samples {
			samples[i] >>= uint(n)
		}
	} else if targetBitsPerComponent > img.BitsPerComponent {
		n := targetBitsPerComponent - img.BitsPerComponent
		for i := range samples {
			samples[i] <<= uint(n)
		}
	}
	img.BitsPerComponent = targetBitsPerComponent
	if img.BitsPerComponent < 8 {
		img.resampleLowBits(samples)
		return
	}
	bytesPerLine := imageutil.BytesPerLine(int(img.Width), int(img.BitsPerComponent), img.ColorComponents)
	n := int(img.Width) * img.ColorComponents
	acquireBytes := core.AcquireBytes(bytesPerLine * int(img.Height))
	clear(acquireBytes)
	for i := 0; i < int(img.Height); i++ {
		count := i * n
		num := min(count+n, len(samples))
		resampleUint32 := sampling.ResampleUint32(samples[count:num], int(targetBitsPerComponent), 8)
		size := i * bytesPerLine
		for i, length := range resampleUint32 {
			if size+i >= len(acquireBytes) {
				break
			}
			acquireBytes[size+i] = byte(length)
		}
	}
	img.Data = acquireBytes
}

// ConvertToBinary converts current image into binary (bi-level) format.
// Binary images are composed of single bits per pixel (only black or white).
// If provided image has more color components, then it would be converted into binary image using
// histogram auto threshold function.
func (img *Image) ConvertToBinary() error {
	if img.ColorComponents == 1 && img.BitsPerComponent == 1 {
		return nil
	}
	img2, err := img.ToGoImage()
	if err != nil {
		return err
	}
	convert, err := imageutil.MonochromeConverter.Convert(img2)
	if err != nil {
		return err
	}
	img.Data = convert.Base().Data
	img.alpha, err = imageutil.ScaleAlphaToMonochrome(img.alpha, int(img.Width), int(img.Height))
	if err != nil {
		return err
	}
	img.BitsPerComponent = 1
	img.ColorComponents = 1
	img.decode = nil
	img.clearSourceMetadata()
	return nil
}
