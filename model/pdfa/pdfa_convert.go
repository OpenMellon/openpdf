package pdfa

import (
	"errors"
	"image/color"

	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
)

func convertContentStreamColors(res *model.PdfPageResources, ops *contentstream.ContentStreamOperations, cmykDefault bool) ([]byte, error) {
	var changed bool
	for _, op := range *ops {
	opSwitch:
		switch op.Operand {
		case "BI":
			contentStreamInlineImage, ok := op.Params[0].(*contentstream.ContentStreamInlineImage)
			if !ok {
				break
			}
			colorSpace, err := contentStreamInlineImage.GetColorSpace(res)
			if err != nil {
				return nil, err
			}
			switch colorSpace.(type) {
			case *model.PdfColorspaceDeviceCMYK:
				if cmykDefault {
					break opSwitch
				}
			case *model.PdfColorspaceDeviceGray:
			case *model.PdfColorspaceDeviceRGB:
				if !cmykDefault {
					break opSwitch
				}
			default:
				break opSwitch
			}
			changed = true
			image, err := contentStreamInlineImage.ToImage(res)
			if err != nil {
				return nil, err
			}
			image2, err := image.ToGoImage()
			if err != nil {
				return nil, err
			}
			if cmykDefault {
				image2, err = imageutil.CMYKConverter.Convert(image2)
			} else {
				image2, err = imageutil.NRGBAConverter.Convert(image2)
			}
			if err != nil {
				return nil, err
			}
			img, ok := image2.(imageutil.Image)
			if !ok {
				return nil, errors.New("image doesn't implement imageutil.Image")
			}
			imageBase := img.Base()
			image3 := model.Image{Width: int64(imageBase.Width), Height: int64(imageBase.Height), BitsPerComponent: int64(imageBase.BitsPerComponent), ColorComponents: imageBase.ColorComponents, Data: imageBase.Data}
			image3.SetDecode(imageBase.Decode)
			image3.SetAlpha(imageBase.Alpha)
			encoder, err := contentStreamInlineImage.GetEncoder()
			if err != nil {
				encoder = core.NewFlateEncoder()
			}
			inlineImage, err := contentstream.NewInlineImageFromImage(image3, encoder)
			if err != nil {
				return nil, err
			}
			op.Params[0] = inlineImage
		case "G", "g":
			if len(op.Params) != 1 {
				break
			}
			f, err := core.GetNumberAsFloat(op.Params[0])
			if err != nil {
				break
			}
			if cmykDefault {
				op.Params = []core.PdfObject{core.MakeFloat(0), core.MakeFloat(0), core.MakeFloat(0), core.MakeFloat(1 - f)}
				operand := "K"
				if op.Operand == "g" {
					operand = "k"
				}
				op.Operand = operand
			} else {
				op.Params = []core.PdfObject{core.MakeFloat(f), core.MakeFloat(f), core.MakeFloat(f)}
				operand := "RG"
				if op.Operand == "g" {
					operand = "rg"
				}
				op.Operand = operand
			}
			changed = true
		case "RG", "rg":
			if !cmykDefault {
				break
			}
			if len(op.Params) != 3 {
				break
			}
			nums, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				break
			}
			changed = true
			red, green, blue := nums[0], nums[1], nums[2]
			c, m, y, k := color.RGBToCMYK(uint8(red*255), uint8(green*255), uint8(255*blue))
			op.Params = []core.PdfObject{core.MakeFloat(float64(c) / 255), core.MakeFloat(float64(m) / 255), core.MakeFloat(float64(y) / 255), core.MakeFloat(float64(k) / 255)}
			operand := "K"
			if op.Operand == "rg" {
				operand = "k"
			}
			op.Operand = operand
		case "K", "k":
			if cmykDefault {
				break
			}
			if len(op.Params) != 4 {
				break
			}
			nums, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				break
			}
			cyan, magenta, yellow, black := nums[0], nums[1], nums[2], nums[3]
			red, green, blue := color.CMYKToRGB(uint8(255*cyan), uint8(255*magenta), uint8(255*yellow), uint8(255*black))
			op.Params = []core.PdfObject{core.MakeFloat(float64(red) / 255), core.MakeFloat(float64(green) / 255), core.MakeFloat(float64(blue) / 255)}
			operand := "RG"
			if op.Operand == "k" {
				operand = "rg"
			}
			op.Operand = operand
			changed = true
		}
	}
	if !changed {
		return nil, nil
	}
	contentCreator := contentstream.NewContentCreator()
	for _, op := range *ops {
		contentCreator.AddOperand(*op)
	}
	data := contentCreator.Bytes()
	return data, nil
}

func convertImagesColorspace(imgs []*docutil.Image, cmykDefault bool) error {
	detail := core.PdfObjectName("DeviceRGB")
	if cmykDefault {
		detail = "DeviceCMYK"
	}
	for _, img := range imgs {
		if img.Colorspace == detail {
			continue
		}
		xObjectImage, err := model.NewXObjectImageFromStream(img.Stream)
		if err != nil {
			return err
		}
		image, err := xObjectImage.ToImage()
		if err != nil {
			return err
		}
		image2, err := image.ToGoImage()
		if err != nil {
			return err
		}
		var cs model.PdfColorspace
		if cmykDefault {
			cs = model.NewPdfColorspaceDeviceCMYK()
			image2, err = imageutil.CMYKConverter.Convert(image2)
		} else {
			cs = model.NewPdfColorspaceDeviceRGB()
			image2, err = imageutil.NRGBAConverter.Convert(image2)
		}
		if err != nil {
			return err
		}
		img2, ok := image2.(imageutil.Image)
		if !ok {
			return errors.New("image doesn't implement imageutil.Image")
		}
		imageBase := img2.Base()
		image3 := &model.Image{Width: int64(imageBase.Width), Height: int64(imageBase.Height), BitsPerComponent: int64(imageBase.BitsPerComponent), ColorComponents: imageBase.ColorComponents, Data: imageBase.Data}
		image3.SetDecode(imageBase.Decode)
		image3.SetAlpha(imageBase.Alpha)
		if err = xObjectImage.SetImage(image3, cs); err != nil {
			return err
		}
		xObjectImage.ToPdfObject()
		img.ColorComponents = imageBase.ColorComponents
		img.Colorspace = detail
	}
	return nil
}

func convertPageContentColors(doc *docutil.Document, cmykDefault bool) error {
	pages, ok := doc.GetPages()
	if !ok {
		return nil
	}
	for _, page := range pages {
		contents, ok := page.GetContents()
		if !ok {
			continue
		}
		var res *model.PdfPageResources
		resources, ok := page.GetResources()
		if ok {
			res, _ = model.NewPdfPageResourcesFromDict(resources)
		}
		for i, content := range contents {
			data, err := content.GetData()
			if err != nil {
				continue
			}
			contentStreamParser := contentstream.NewContentStreamParser(string(data))
			ops, err := contentStreamParser.Parse()
			if err != nil {
				continue
			}
			data2, err := convertContentStreamColors(res, ops, cmykDefault)
			if err != nil {
				return err
			}
			if data2 == nil {
				continue
			}
			if err = (&contents[i]).SetData(data2); err != nil {
				return err
			}
		}
	}
	return nil
}
