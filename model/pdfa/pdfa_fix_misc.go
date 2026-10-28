package pdfa

import (
	"errors"
	"image/color"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
)

func fixXObjects(doc *docutil.Document, part int) error {
	seen := map[*core.PdfObjectStream]struct{}{}
	for _, object := range doc.Objects {
		objectStream, ok := core.GetStream(object)
		if !ok {
			continue
		}
		if _, ok = seen[objectStream]; ok {
			continue
		}
		seen[objectStream] = struct{}{}
		subTypeName, ok := core.GetName(objectStream.Get("SubType"))
		if !ok {
			continue
		}
		if objectStream.Get("Ref") != nil {
			objectStream.Remove("Ref")
		}
		if subTypeName.String() == "PS" {
			objectStream.Remove("PS")
			continue
		}
		if subTypeName.String() == "Form" {
			if objectStream.Get("OPI") != nil {
				objectStream.Remove("OPI")
			}
			if objectStream.Get("PS") != nil {
				objectStream.Remove("PS")
			}
			if subtype2 := objectStream.Get("Subtype2"); subtype2 != nil {
				if subtype2Name, ok := core.GetName(subtype2); ok && *subtype2Name == "PS" {
					objectStream.Remove("Subtype2")
				}
			}
			continue
		}
		if subTypeName.String() == "Image" {
			interpolate, ok := core.GetBool(objectStream.Get("Interpolate"))
			if ok && bool(*interpolate) {
				objectStream.Set("Interpolate", core.MakeBool(false))
			}
			if part == 2 {
				if objectStream.Get("OPI") != nil {
					objectStream.Remove("OPI")
				}
			}
			if objectStream.Get("Alternates") != nil {
				objectStream.Remove("Alternates")
			}
			continue
		}
	}
	return nil
}

func fixInteractiveForm(doc *docutil.Document) error {
	for _, object := range doc.Objects {
		objectDict, ok := core.GetDict(object)
		if !ok {
			continue
		}
		typeObj := objectDict.Get("Type")
		if typeObj == nil {
			continue
		}
		if typeName, ok := core.GetName(typeObj); ok && typeName.String() != "AcroForm" {
			continue
		}
		needAppearances, ok2 := core.GetBool(objectDict.Get("NeedAppearances"))
		if ok2 && bool(*needAppearances) {
			objectDict.Set("NeedAppearances", core.MakeBool(false))
		}
		if objectDict.Get("XFA") != nil {
			objectDict.Remove("XFA")
		}
	}
	catalog, ok := doc.FindCatalog()
	if !ok {
		return errors.New("catalog not found")
	}
	if catalog.Object.Get("NeedsRendering") != nil {
		catalog.Object.Remove("NeedsRendering")
	}
	return nil
}

func fixActionsPDFA4(doc *docutil.Document, std standardType) error {
	for _, object := range doc.Objects {
		objectDict, ok := core.GetDict(object)
		if !ok {
			continue
		}
		typeObj := objectDict.Get("Type")
		if typeObj == nil {
			continue
		}
		if typeName, ok := core.GetName(typeObj); ok && typeName.String() != "Action" {
			continue
		}
		sName, ok2 := core.GetName(objectDict.Get("S"))
		if !ok2 {
			continue
		}
		switch model.PdfActionType(*sName) {
		case model.ActionTypeLaunch, model.ActionTypeSound, model.ActionTypeMovie, model.ActionTypeResetForm, model.ActionTypeImportData, model.ActionTypeJavaScript, model.ActionTypeHide, model.ActionTypeRendition, model.ActionTypeTrans:
			objectDict.Remove("S")
		case model.ActionTypeSetOCGState, model.ActionTypeGoTo3DView:
			if std.conformance != "E" {
				objectDict.Remove("S")
			}
		case model.ActionTypeNamed:
			nName, ok := core.GetName(objectDict.Get("N"))
			if !ok {
				continue
			}
			switch *nName {
			case "NextPage", "PrevPage", "FirstPage", "LastPage":
			default:
				objectDict.Remove("N")
			}
		}
	}
	return nil
}

func fixAnnotationsPDFA1(doc *docutil.Document) error {
	pages, ok := doc.GetPages()
	if !ok {
		return nil
	}
	for _, page := range pages {
		annotsArray, ok := core.GetArray(page.Object.Get("Annots"))
		if !ok {
			continue
		}
		for _, obj := range annotsArray.Elements() {
			obj = core.ResolveReference(obj)
			if _, ok := obj.(*core.PdfObjectNull); ok {
				continue
			}
			dict, ok := core.GetDict(obj)
			if !ok {
				continue
			}
			f, _ := core.GetIntVal(dict.Get("F"))
			f &= ^(1 << 0)
			f &= ^(1 << 1)
			f &= ^(1 << 5)
			f |= 1 << 2
			dict.Set("F", core.MakeInteger(int64(f)))
			found := false
			if subtype := dict.Get("Subtype"); subtype != nil {
				subtypeName, ok := core.GetName(subtype)
				if ok && subtypeName.String() == "Widget" {
					found = true
					if dict.Get("AA") != nil {
						dict.Remove("AA")
					}
				}
			}
			if dict.Get("C") != nil || dict.Get("IC") != nil {
				dict2, ok := firstOutputIntentDict(doc)
				if !ok {
					dict.Remove("C")
					dict.Remove("IC")
				} else {
					n, ok := core.GetIntVal(dict2.Get("N"))
					if !ok || n != 3 {
						dict.Remove("C")
						dict.Remove("IC")
					}
				}
			}
			apDict, ok := core.GetDict(dict.Get("AP"))
			if ok {
				n := apDict.Get("N")
				if n == nil {
					continue
				}
				if len(apDict.Keys()) > 1 {
					apDict.Clear()
					apDict.Set("N", n)
				}
				if found {
					ftName, ok := core.GetName(dict.Get("FT"))
					if ok && *ftName == "Btn" {
						continue
					}
				}
			}
		}
	}
	return nil
}

func fixAnnotationsPDFA23(doc *docutil.Document) error {
	pages, ok := doc.GetPages()
	if !ok {
		return nil
	}
	for _, page := range pages {
		annotsArray, ok := core.GetArray(page.Object.Get("Annots"))
		if !ok {
			continue
		}
		for _, obj := range annotsArray.Elements() {
			obj = core.ResolveReference(obj)
			if _, ok := obj.(*core.PdfObjectNull); ok {
				continue
			}
			dict, ok := core.GetDict(obj)
			if !ok {
				continue
			}
			f, _ := core.GetIntVal(dict.Get("F"))
			f &= ^(1 << 0)
			f &= ^(1 << 1)
			f &= ^(1 << 5)
			f &= ^(1 << 8)
			f |= 1 << 2
			dict.Set("F", core.MakeInteger(int64(f)))
			found := false
			if subtype := dict.Get("Subtype"); subtype != nil {
				subtypeName, ok := core.GetName(subtype)
				if ok && subtypeName.String() == "Widget" {
					found = true
					if dict.Get("AA") != nil {
						dict.Remove("AA")
					}
					if dict.Get("A") != nil {
						dict.Remove("A")
					}
				}
				if ok && subtypeName.String() == "Text" {
					f, _ := core.GetIntVal(dict.Get("F"))
					f |= 1 << 3
					f |= 1 << 4
					dict.Set("F", core.MakeInteger(int64(f)))
				}
			}
			apDict, ok := core.GetDict(dict.Get("AP"))
			if ok {
				n := apDict.Get("N")
				if n == nil {
					continue
				}
				if len(apDict.Keys()) > 1 {
					apDict.Clear()
					apDict.Set("N", n)
				}
				if found {
					ftName, ok := core.GetName(dict.Get("FT"))
					if ok && *ftName == "Btn" {
						continue
					}
				}
			}
		}
	}
	return nil
}

func fixActions(doc *docutil.Document, part int) error {
	for _, object := range doc.Objects {
		objectDict, ok := core.GetDict(object)
		if !ok {
			continue
		}
		typeObj := objectDict.Get("Type")
		if typeObj == nil {
			continue
		}
		if typeName, ok := core.GetName(typeObj); ok && typeName.String() != "Action" {
			continue
		}
		sName, ok2 := core.GetName(objectDict.Get("S"))
		if !ok2 {
			continue
		}
		switch model.PdfActionType(*sName) {
		case model.ActionTypeLaunch, model.ActionTypeSound, model.ActionTypeMovie, model.ActionTypeResetForm, model.ActionTypeImportData, model.ActionTypeJavaScript:
			objectDict.Remove("S")
		case model.ActionTypeHide, model.ActionTypeSetOCGState, model.ActionTypeRendition, model.ActionTypeTrans, model.ActionTypeGoTo3DView:
			if part == 2 {
				objectDict.Remove("S")
			}
		case model.ActionTypeNamed:
			nName, ok := core.GetName(objectDict.Get("N"))
			if !ok {
				continue
			}
			switch *nName {
			case "NextPage", "PrevPage", "FirstPage", "LastPage":
			default:
				objectDict.Remove("N")
			}
		}
	}
	return nil
}

func fixInteractiveFormPDFA1(doc *docutil.Document) error {
	for _, object := range doc.Objects {
		objectDict, ok := core.GetDict(object)
		if !ok {
			continue
		}
		typeObj := objectDict.Get("Type")
		if typeObj == nil {
			continue
		}
		if typeName, ok := core.GetName(typeObj); ok && typeName.String() != "AcroForm" {
			continue
		}
		needAppearances, ok2 := core.GetBool(objectDict.Get("NeedAppearances"))
		if ok2 {
			if bool(*needAppearances) {
				objectDict.Set("NeedAppearances", core.MakeBool(false))
			}
		}
		a := objectDict.Get("A")
		if a != nil {
			objectDict.Remove("A")
		}
		fieldsArray, ok2 := core.GetArray(objectDict.Get("Fields"))
		if ok2 {
			for n := 0; n < fieldsArray.Len(); n++ {
				dict, ok := core.GetDict(fieldsArray.Get(n))
				if !ok {
					continue
				}
				if dict.Get("AA") != nil {
					dict.Remove("AA")
				}
			}
		}
	}
	return nil
}

func fixAnnotationColors(doc *docutil.Document, cmykDefault bool) error {
	pages, ok := doc.GetPages()
	if !ok {
		return nil
	}
	for _, page := range pages {
		annotsArray, ok := core.GetArray(page.Object.Get("Annots"))
		if !ok {
			continue
		}
		for _, obj := range annotsArray.Elements() {
			dict, ok := core.GetDict(obj)
			if !ok {
				continue
			}
			c := dict.Get("C")
			if c == nil {
				continue
			}
			cArray, ok := core.GetArray(c)
			if !ok {
				continue
			}
			asFloat64Slice, err := cArray.GetAsFloat64Slice()
			if err != nil {
				return err
			}
			switch cArray.Len() {
			case 0, 1:
				if cmykDefault {
					dict.Set("C", core.MakeArrayFromIntegers([]int{1, 1, 1, 1}))
				} else {
					dict.Set("C", core.MakeArrayFromIntegers([]int{1, 1, 1}))
				}
			case 3:
				if cmykDefault {
					c, m, y, k := color.RGBToCMYK(uint8(asFloat64Slice[0]*255), uint8(asFloat64Slice[1]*255), uint8(asFloat64Slice[2]*255))
					dict.Set("C", core.MakeArrayFromFloats([]float64{float64(c) / 255, float64(m) / 255, float64(y) / 255, float64(k) / 255}))
				}
			case 4:
				if !cmykDefault {
					red, green, blue := color.CMYKToRGB(uint8(asFloat64Slice[0]*255), uint8(asFloat64Slice[1]*255), uint8(asFloat64Slice[2]*255), uint8(asFloat64Slice[3]*255))
					dict.Set("C", core.MakeArrayFromFloats([]float64{float64(red) / 255, float64(green) / 255, float64(blue) / 255}))
				}
			}
		}
	}
	return nil
}

func fixPermissions(doc *docutil.Document) error {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return errors.New("catalog not found")
	}
	permsDict, ok := core.GetDict(catalog.Object.Get("Perms"))
	if ok {
		dict := core.MakeDict()
		names := permsDict.Keys()
		for _, name := range names {
			if name.String() == "UR3" || name.String() == "DocMDP" {
				dict.Set(name, permsDict.Get(name))
			}
		}
		catalog.Object.Set("Perms", dict)
	}
	return nil
}

func fixOptionalContent(doc *docutil.Document) error {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return errors.New("catalog not found")
	}
	ocPropertiesDict, ok := core.GetDict(catalog.Object.Get("OCProperties"))
	if !ok {
		return nil
	}
	dDict, ok := core.GetDict(ocPropertiesDict.Get("D"))
	if ok {
		if dDict.Get("AS") != nil {
			dDict.Remove("AS")
		}
	}
	configsArray, ok := core.GetArray(ocPropertiesDict.Get("Configs"))
	if ok {
		for n := 0; n < configsArray.Len(); n++ {
			dict, ok := core.GetDict(configsArray.Get(n))
			if !ok {
				continue
			}
			if dict.Get("AS") != nil {
				dict.Remove("AS")
			}
		}
	}
	return nil
}

func fixTransparencyGroups(doc *docutil.Document) error {
	pages, ok := doc.GetPages()
	if !ok {
		return nil
	}
	for _, page := range pages {
		streams := page.FindXObjectForms()
		for _, stream := range streams {
			groupDict, ok := core.GetDict(stream.Get("Group"))
			if ok {
				if s := groupDict.Get("S"); s != nil {
					sName, ok := core.GetName(s)
					if ok && sName.String() == "Transparency" {
						stream.Remove("Group")
					}
				}
			}
		}
		resourcesXObject, ok := page.GetResourcesXObject()
		if ok {
			groupDict, ok := core.GetDict(resourcesXObject.Get("Group"))
			if ok {
				s := groupDict.Get("S")
				if s != nil {
					sName, ok := core.GetName(s)
					if ok && sName.String() == "Transparency" {
						resourcesXObject.Remove("Group")
					}
				}
			}
		}
		groupDict, ok2 := core.GetDict(page.Object.Get("Group"))
		if ok2 {
			s := groupDict.Get("S")
			if s != nil {
				sName, ok := core.GetName(s)
				if ok && sName.String() == "Transparency" {
					page.Object.Remove("Group")
				}
			}
		}
	}
	return nil
}
