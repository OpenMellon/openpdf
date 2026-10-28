package pdfa

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/colorprofile"
)

func verifyColorspacesPDFA1(r *model.CompliancePdfReader, checkOutputIntents bool) (violations []ViolatedRule) {
	var reportedUnused1, reportedUnused2, reported6_2_3_1, reported6_2_3_2, reported6_2_3_3, reported6_2_3_4, reported6_2_3_1_2 bool
	allReported := func() bool {
		return reportedUnused1 && reportedUnused2 && reported6_2_3_1 && reported6_2_3_2 && reported6_2_3_3 && reported6_2_3_4 && reported6_2_3_1_2
	}
	intent, ok := firstOutputIntent(r)
	var profileHeader colorprofile.ProfileHeader
	if ok {
		profileHeader, _ = colorprofile.ParseHeader(intent.DestOutputProfile)
	}
	var deviceColorViolation bool
	seen := map[core.PdfObject]struct{}{}
	var checkColorspace func(cs model.PdfColorspace) bool
	checkColorspace = func(cs model.PdfColorspace) bool {
		switch cs := cs.(type) {
		case *model.PdfColorspaceDeviceGray:
			if !reported6_2_3_4 {
				if !ok {
					deviceColorViolation = true
					violations = append(violations, newViolatedRule("6.2.3-4", "DeviceGray may be used only if the file has a PDF/A-1 OutputIntent."))
					reported6_2_3_4 = true
					if allReported() {
						return true
					}
				}
			}
		case *model.PdfColorspaceDeviceRGB:
			if !reported6_2_3_2 {
				if !ok || profileHeader.ColorSpace != colorprofile.ColorSpaceRGB {
					deviceColorViolation = true
					violations = append(violations, newViolatedRule("6.2.3-2", "DeviceRGB may be used only if the file has a PDF/A-1 OutputIntent that uses an RGB color space."))
					reported6_2_3_2 = true
					if allReported() {
						return true
					}
				}
			}
		case *model.PdfColorspaceDeviceCMYK:
			if !reported6_2_3_3 {
				if !ok || profileHeader.ColorSpace != colorprofile.ColorSpaceCMYK {
					deviceColorViolation = true
					violations = append(violations, newViolatedRule("6.2.3-3", "DeviceCMYK may be used only if the file has a PDF/A-1 OutputIntent that uses an CMYK color space."))
					reported6_2_3_3 = true
					if allReported() {
						return true
					}
				}
			}
		case *model.PdfColorspaceICCBased:
			if !reported6_2_3_1 || !reported6_2_3_1_2 {
				profileHeader, err := colorprofile.ParseHeader(cs.Data)
				if err != nil {
					common.Log.Debug("parsing ICCBased header failed: %v", err)
					violations = append(violations, func() ViolatedRule {
						return newViolatedRule("6.2.3-1", "All ICCBased color spaces shall be embedded as ICC profile streams as described in PDF Reference 4.5")
					}())
					reported6_2_3_1 = true
					if allReported() {
						return true
					}
				}
				if !reported6_2_3_1 {
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
						violations = append(violations, newViolatedRule("6.2.3-1", "All ICCBased color spaces shall be embedded as ICC profile streams as described in PDF Reference 4.5"))
						reported6_2_3_1 = true
						if allReported() {
							return true
						}
					}
				}
				if !reported6_2_3_1_2 {
					stream, _ := core.GetStream(cs.GetContainingPdfObject())
					if stream.Get("N") == nil || (cs.N == 1 && profileHeader.ColorSpace != colorprofile.ColorSpaceGRAY) || (cs.N == 3 && !(profileHeader.ColorSpace == colorprofile.ColorSpaceRGB || profileHeader.ColorSpace == colorprofile.ColorSpaceLAB)) || (cs.N == 4 && profileHeader.ColorSpace != colorprofile.ColorSpaceCMYK) {
						violations = append(violations, newViolatedRule("6.2.3-5", "If an uncalibrated color space is used in a file then that file shall contain a PDF/A-1 OutputIntent, as defined in 6.2.2."))
						reported6_2_3_1_2 = true
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
	if !deviceColorViolation {
		return violations
	}
	if (profileHeader.DeviceClass == colorprofile.DeviceClassPRTR || profileHeader.DeviceClass == colorprofile.DeviceClassMNTR) && (profileHeader.ColorSpace == colorprofile.ColorSpaceRGB || profileHeader.ColorSpace == colorprofile.ColorSpaceCMYK || profileHeader.ColorSpace == colorprofile.ColorSpaceGRAY) {
		return violations
	}
	if !checkOutputIntents {
		return violations
	}
	dict, ok2 := catalogDict(r)
	if !ok2 {
		return violations
	}
	outputIntentsArray, ok2 := core.GetArray(dict.Get("OutputIntents"))
	if !ok2 {
		violations = append(violations, newViolatedRule("6.2.2-1", "A PDF/A-1 OutputIntent is an OutputIntent dictionary, as defined by PDF Reference 9.10.4, that is included in the file's OutputIntents array and has GTS_PDFA1 as the value of its S key and a valid ICC profile stream as the value its DestOutputProfile key."), newViolatedRule("6.2.2-2", "If a file's OutputIntents array contains more than one entry, then all entries that contain a DestOutputProfile key shall have as the value of that key the same indirect object, which shall be a valid ICC profile stream."))
		return violations
	}
	if outputIntentsArray.Len() > 1 {
		seen := map[*core.PdfObjectDictionary]struct{}{}
		for n := 0; n < outputIntentsArray.Len(); n++ {
			dict, ok := core.GetDict(outputIntentsArray.Get(n))
			if !ok {
				continue
			}
			if n == 0 {
				seen[dict] = struct{}{}
				continue
			}
			if _, ok := seen[dict]; !ok {
				violations = append(violations, newViolatedRule("6.2.2-2", "If a file's OutputIntents array contains more than one entry, then all entries that contain a DestOutputProfile key shall have as the value of that key the same indirect object, which shall be a valid ICC profile stream."))
				break
			}
		}
	}
	return violations
}
