package pdfa

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/colorprofile"
)

func verifyColorspaces(r *model.CompliancePdfReader) (violations []ViolatedRule) {
	var reported6_2_4_3_4, reported6_2_4_3_2, reported6_2_4_3_3, reported6_2_4_2_1 bool
	allReported := func() bool { return reported6_2_4_3_4 && reported6_2_4_3_2 && reported6_2_4_3_3 && reported6_2_4_2_1 }
	intent, ok := firstOutputIntent(r)
	var profileHeader colorprofile.ProfileHeader
	if ok {
		profileHeader, _ = colorprofile.ParseHeader(intent.DestOutputProfile)
	}
	seen := map[core.PdfObject]struct{}{}
	var checkColorspace func(cs model.PdfColorspace) bool
	checkColorspace = func(cs model.PdfColorspace) bool {
		switch cs := cs.(type) {
		case *model.PdfColorspaceDeviceGray:
			if !reported6_2_4_3_4 {
				if !ok {
					violations = append(violations, newViolatedRule("6.2.4.3-4", "DeviceGray shall only be used if a device independent DefaultGray colour space has been set when the DeviceGray colour space is used, or if a PDF/A OutputIntent is present."))
					reported6_2_4_3_4 = true
					if allReported() {
						return true
					}
				}
			}
		case *model.PdfColorspaceDeviceRGB:
			if !reported6_2_4_3_2 {
				if !ok || profileHeader.ColorSpace != colorprofile.ColorSpaceRGB {
					violations = append(violations, newViolatedRule("6.2.4.3-2", "DeviceRGB shall only be used if a device independent DefaultRGB colour space has been set when the DeviceRGB colour space is used, or if the file has a PDF/A OutputIntent that contains an RGB destination profile."))
					reported6_2_4_3_2 = true
					if allReported() {
						return true
					}
				}
			}
		case *model.PdfColorspaceDeviceCMYK:
			if !reported6_2_4_3_3 {
				if !ok || profileHeader.ColorSpace != colorprofile.ColorSpaceCMYK {
					violations = append(violations, newViolatedRule("6.2.4.3-3", "DeviceCMYK shall only be used if a device independent DefaultCMYK colour space has been set or if a DeviceN-based DefaultCMYK colour space has been set when the DeviceCMYK colour space is used or the file has a PDF/A OutputIntent that contains a CMYK destination profile."))
					reported6_2_4_3_3 = true
					if allReported() {
						return true
					}
				}
			}
		case *model.PdfColorspaceICCBased:
			if !reported6_2_4_2_1 {
				profileHeader, err := colorprofile.ParseHeader(cs.Data)
				if err != nil {
					common.Log.Debug("parsing ICCBased header failed: %v", err)
					violations = append(violations, func() ViolatedRule {
						return newViolatedRule("6.2.4.2-1", "The profile that forms the stream of an ICCBased colour space shall conform to ICC.1:1998-09, ICC.1:2001-12, ICC.1:2003-09 or ISO 15076-1.")
					}())
					reported6_2_4_2_1 = true
					if allReported() {
						return true
					}
				}
				if !reported6_2_4_2_1 {
					var badDeviceClass, badColorSpace bool
					switch profileHeader.DeviceClass {
					case colorprofile.DeviceClassPRTR, colorprofile.DeviceClassMNTR, colorprofile.DeviceClassSCNR, colorprofile.DeviceClassSPAC:
					default:
						badDeviceClass = true
					}
					switch profileHeader.ColorSpace {
					case colorprofile.ColorSpaceRGB, colorprofile.ColorSpaceCMYK, colorprofile.ColorSpaceGRAY, colorprofile.ColorSpaceLAB:
					default:
						badColorSpace = true
					}
					if badDeviceClass || badColorSpace {
						violations = append(violations, newViolatedRule("6.2.4.2-1", "The profile that forms the stream of an ICCBased colour space shall conform to ICC.1:1998-09, ICC.1:2001-12, ICC.1:2003-09 or ISO 15076-1."))
						reported6_2_4_2_1 = true
						if allReported() {
							return true
						}
					}
				}
			}
			if cs.Alternate != nil {
				return checkColorspace(cs.Alternate)
			}
		}
		return false
	}
	for _, n := range r.GetObjectNums() {
		obj, err := r.GetIndirectObjectByNumber(n)
		if err != nil {
			continue
		}
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		typeName, ok := core.GetName(stream.Get("Type"))
		if !ok || typeName.String() != "XObject" {
			continue
		}
		subtypeName, ok := core.GetName(stream.Get("Subtype"))
		if !ok {
			continue
		}
		seen[stream] = struct{}{}
		switch subtypeName.String() {
		case "Image":
			xObjectImage, err := model.NewXObjectImageFromStream(stream)
			if err != nil {
				continue
			}
			seen[stream] = struct{}{}
			if checkColorspace(xObjectImage.ColorSpace) {
				return violations
			}
		case "Form":
			groupDict, ok := core.GetDict(stream.Get("Group"))
			if !ok {
				continue
			}
			cs := groupDict.Get("CS")
			if cs == nil {
				continue
			}
			colorspace, err := model.NewPdfColorspaceFromPdfObject(cs)
			if err != nil {
				continue
			}
			if checkColorspace(colorspace) {
				return violations
			}
		}
	}
	for _, page := range r.PageList {
		contentStreams, err := page.GetContentStreams()
		if err != nil {
			continue
		}
		for _, contentStream := range contentStreams {
			ops, err := contentstream.NewContentStreamParser(contentStream).Parse()
			if err != nil {
				continue
			}
			for _, op := range *ops {
				if len(op.Params) > 1 {
					continue
				}
				switch op.Operand {
				case "BI":
					contentStreamInlineImage, ok := op.Params[0].(*contentstream.ContentStreamInlineImage)
					if !ok {
						continue
					}
					colorSpace, err := contentStreamInlineImage.GetColorSpace(page.Resources)
					if err != nil {
						continue
					}
					if checkColorspace(colorSpace) {
						return violations
					}
				case "Do":
					name, ok := core.GetName(op.Params[0])
					if !ok {
						continue
					}
					xObjectByName, xObjectType := page.Resources.GetXObjectByName(*name)
					if _, ok := seen[xObjectByName]; ok {
						continue
					}
					switch xObjectType {
					case model.XObjectTypeImage:
						xObjectImage, err := model.NewXObjectImageFromStream(xObjectByName)
						if err != nil {
							continue
						}
						seen[xObjectByName] = struct{}{}
						if checkColorspace(xObjectImage.ColorSpace) {
							return violations
						}
					case model.XObjectTypeForm:
						groupDict, ok := core.GetDict(xObjectByName.Get("Group"))
						if !ok {
							continue
						}
						csName, ok := core.GetName(groupDict.Get("CS"))
						if !ok {
							continue
						}
						colorspace, err := model.NewPdfColorspaceFromPdfObject(csName)
						if err != nil {
							continue
						}
						seen[xObjectByName] = struct{}{}
						if checkColorspace(colorspace) {
							return violations
						}
					}
				}
			}
		}
	}
	return violations
}
