package pdfa

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
)

func fixExtGStatePDFA23(doc *docutil.Document) error {
	fixExtGState := func(dict *core.PdfObjectDictionary) error {
		if dict.Get("TR") != nil {
			common.Log.Debug("ExtGState object contains TR key")
			dict.Remove("TR")
		}
		tR2 := dict.Get("TR2")
		if tR2 != nil {
			s := tR2.String()
			if s != "Default" {
				common.Log.Debug("ExtGState object contains TR2 key with value other than Default")
				dict.Set("TR2", core.MakeName("Default"))
			}
		}
		if dict.Get("HTP") != nil {
			common.Log.Debug("ExtGState object contains HTP key")
			dict.Remove("HTP")
		}
		bm := dict.Get("BM")
		if bm != nil {
			bmName, ok := core.GetName(bm)
			if !ok {
				common.Log.Debug("ExtGState object 'BM' is not a Name")
				bmName = core.MakeName("")
			}
			s := bmName.String()
			switch s {
			case "Normal", "Compatible", "Multiply", "Screen", "Overlay", "Darken", "Lighten", "ColorDodge", "ColorBurn", "HardLight", "SoftLight", "Difference", "Exclusion", "Hue", "Saturation", "Color", "Luminosity":
			default:
				dict.Set("BM", core.MakeName("Normal"))
			}
		}
		return nil
	}
	pages, ok := doc.GetPages()
	if !ok {
		return nil
	}
	for _, page := range pages {
		resources, ok := page.GetResources()
		if !ok {
			continue
		}
		extGStateDict, ok2 := core.GetDict(resources.Get("ExtGState"))
		if !ok2 {
			return nil
		}
		names := extGStateDict.Keys()
		for _, name := range names {
			dict, ok := core.GetDict(extGStateDict.Get(name))
			if !ok {
				continue
			}
			err := fixExtGState(dict)
			if err != nil {
				continue
			}
		}
	}
	for _, page := range pages {
		contents, ok := page.GetContents()
		if !ok {
			return nil
		}
		for _, content := range contents {
			data, err := content.GetData()
			if err != nil {
				continue
			}
			contentStreamParser := contentstream.NewContentStreamParser(string(data))
			ops, err := contentStreamParser.Parse()
			if err != nil {
				continue
			}
			for _, op := range *ops {
				if len(op.Params) == 0 {
					continue
				}
				_, ok := core.GetName(op.Params[0])
				if !ok {
					continue
				}
				resourcesXObject, ok2 := page.GetResourcesXObject()
				if !ok2 {
					continue
				}
				for _, name := range resourcesXObject.Keys() {
					stream, ok := core.GetStream(resourcesXObject.Get(name))
					if !ok {
						continue
					}
					resourcesDict, ok := core.GetDict(stream.Get("Resources"))
					if !ok {
						continue
					}
					extGStateDict, ok := core.GetDict(resourcesDict.Get("ExtGState"))
					if !ok {
						continue
					}
					for _, name := range extGStateDict.Keys() {
						dict, ok := core.GetDict(extGStateDict.Get(name))
						if !ok {
							continue
						}
						err := fixExtGState(dict)
						if err != nil {
							continue
						}
					}
				}
			}
		}
	}
	return nil
}

func fixExtGStatePDFA1(doc *docutil.Document) error {
	fixExtGState := func(dict *core.PdfObjectDictionary) error {
		if sMask := dict.Get("SMask"); sMask != nil {
			dict.Set("SMask", core.MakeName("None"))
		}
		ca := dict.Get("CA")
		if ca != nil {
			f, err := core.GetNumberAsFloat(ca)
			if err != nil {
				common.Log.Debug("ExtGState object CA value is not a float: %v", err)
				f = 0
			}
			if f != 1.0 {
				dict.Set("CA", core.MakeFloat(1.0))
			}
		}
		ca = dict.Get("ca")
		if ca != nil {
			f, err := core.GetNumberAsFloat(ca)
			if err != nil {
				common.Log.Debug("ExtGState object 'ca' value is not a float: %v", err)
				f = 0
			}
			if f != 1.0 {
				dict.Set("ca", core.MakeFloat(1.0))
			}
		}
		bm := dict.Get("BM")
		if bm != nil {
			bmName, ok := core.GetName(bm)
			if !ok {
				common.Log.Debug("ExtGState object 'BM' is not a Name")
				bmName = core.MakeName("")
			}
			s := bmName.String()
			switch s {
			case "Normal", "Compatible":
			default:
				dict.Set("BM", core.MakeName("Normal"))
			}
		}
		tr := dict.Get("TR")
		if tr != nil {
			common.Log.Debug("ExtGState object contains TR key")
			dict.Remove("TR")
		}
		tR2 := dict.Get("TR2")
		if tR2 != nil {
			s := tR2.String()
			if s != "Default" {
				common.Log.Debug("ExtGState object contains TR2 key with value other than Default")
				dict.Set("TR2", core.MakeName("Default"))
			}
		}
		return nil
	}
	pages, ok := doc.GetPages()
	if !ok {
		return nil
	}
	for _, page := range pages {
		resources, ok := page.GetResources()
		if !ok {
			continue
		}
		extGStateDict, ok2 := core.GetDict(resources.Get("ExtGState"))
		if !ok2 {
			return nil
		}
		names := extGStateDict.Keys()
		for _, name := range names {
			dict, ok := core.GetDict(extGStateDict.Get(name))
			if !ok {
				continue
			}
			err := fixExtGState(dict)
			if err != nil {
				continue
			}
		}
	}
	for _, page := range pages {
		contents, ok := page.GetContents()
		if !ok {
			return nil
		}
		for _, content := range contents {
			data, err := content.GetData()
			if err != nil {
				continue
			}
			contentStreamParser := contentstream.NewContentStreamParser(string(data))
			ops, err := contentStreamParser.Parse()
			if err != nil {
				continue
			}
			for _, op := range *ops {
				if len(op.Params) == 0 {
					continue
				}
				_, ok := core.GetName(op.Params[0])
				if !ok {
					continue
				}
				resourcesXObject, ok2 := page.GetResourcesXObject()
				if !ok2 {
					continue
				}
				for _, name := range resourcesXObject.Keys() {
					stream, ok := core.GetStream(resourcesXObject.Get(name))
					if !ok {
						continue
					}
					resourcesDict, ok := core.GetDict(stream.Get("Resources"))
					if !ok {
						continue
					}
					extGStateDict, ok := core.GetDict(resourcesDict.Get("ExtGState"))
					if !ok {
						continue
					}
					for _, name := range extGStateDict.Keys() {
						dict, ok := core.GetDict(extGStateDict.Get(name))
						if !ok {
							continue
						}
						err := fixExtGState(dict)
						if err != nil {
							continue
						}
					}
				}
			}
		}
	}
	return nil
}
