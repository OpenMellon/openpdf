package model

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/png"
	"os"
	"strings"

	"github.com/OpenMellon/freetype"
	"github.com/OpenMellon/freetype/truetype"
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/license"
	imagefont "golang.org/x/image/font"
)

func (p *PdfPage) getParentResources() (*PdfPageResources, error) {
	parent := p.Parent
	for parent != nil {
		parentDict, ok := core.GetDict(parent)
		if !ok {
			common.Log.Debug("ERROR: invalid parent node")
			return nil, errors.New("invalid parent object")
		}
		if resources := parentDict.Get("Resources"); resources != nil {
			resourcesDict, ok := core.GetDict(resources)
			if !ok {
				return nil, errors.New("invalid resource dict")
			}
			resources, err := NewPdfPageResourcesFromDict(resourcesDict)
			if err != nil {
				return nil, err
			}
			return resources, nil
		}
		parent = parentDict.Get("Parent")
	}
	return nil, nil
}

// GetPageDict converts the Page to a PDF object dictionary.
func (p *PdfPage) GetPageDict() *core.PdfObjectDictionary {
	pageDict := p.pageDict
	pageDict.Clear()
	pageDict.Set("Type", core.MakeName("Page"))
	pageDict.Set("Parent", p.Parent)
	if p.LastModified != nil {
		pageDict.Set("LastModified", p.LastModified.ToPdfObject())
	}
	if p.Resources != nil {
		pageDict.Set("Resources", p.Resources.ToPdfObject())
	}
	if p.CropBox != nil {
		pageDict.Set("CropBox", p.CropBox.ToPdfObject())
	}
	if p.MediaBox != nil {
		pageDict.Set("MediaBox", p.MediaBox.ToPdfObject())
	}
	if p.BleedBox != nil {
		pageDict.Set("BleedBox", p.BleedBox.ToPdfObject())
	}
	if p.TrimBox != nil {
		pageDict.Set("TrimBox", p.TrimBox.ToPdfObject())
	}
	if p.ArtBox != nil {
		pageDict.Set("ArtBox", p.ArtBox.ToPdfObject())
	}
	pageDict.SetIfNotNil("BoxColorInfo", p.BoxColorInfo)
	pageDict.SetIfNotNil("Contents", p.Contents)
	if p.Rotate != nil {
		pageDict.Set("Rotate", core.MakeInteger(*p.Rotate))
	}
	pageDict.SetIfNotNil("Group", p.Group)
	pageDict.SetIfNotNil("Thumb", p.Thumb)
	pageDict.SetIfNotNil("B", p.B)
	pageDict.SetIfNotNil("Dur", p.Dur)
	pageDict.SetIfNotNil("Trans", p.Trans)
	pageDict.SetIfNotNil("AA", p.AA)
	pageDict.SetIfNotNil("Metadata", p.Metadata)
	pageDict.SetIfNotNil("PieceInfo", p.PieceInfo)
	pageDict.SetIfNotNil("StructParents", p.StructParents)
	pageDict.SetIfNotNil("ID", p.ID)
	pageDict.SetIfNotNil("PZ", p.PZ)
	pageDict.SetIfNotNil("SeparationInfo", p.SeparationInfo)
	pageDict.SetIfNotNil("Tabs", p.Tabs)
	pageDict.SetIfNotNil("TemplateInstantiated", p.TemplateInstantiated)
	pageDict.SetIfNotNil("PresSteps", p.PresSteps)
	pageDict.SetIfNotNil("UserUnit", p.UserUnit)
	pageDict.SetIfNotNil("VP", p.VP)
	if annotationsSnapshot := p.annotationsSnapshot(); annotationsSnapshot != nil {
		array := core.MakeArray()
		for _, annot := range annotationsSnapshot {
			if model := annot.GetContext(); model != nil {
				array.Append(model.ToPdfObject())
			} else {
				array.Append(annot.ToPdfObject())
			}
		}
		if array.Len() > 0 {
			pageDict.Set("Annots", array)
		}
	} else if p.Annots != nil {
		pageDict.SetIfNotNil("Annots", p.Annots)
	}
	return pageDict
}

// GetPageAsIndirectObject returns the page as a dictionary within an PdfIndirectObject.
func (p *PdfPage) GetPageAsIndirectObject() *core.PdfIndirectObject { return p.primitive }

// GetMediaBox gets the inheritable media box value, either from the page
// or a higher up page/pages struct.
func (p *PdfPage) GetMediaBox() (*PdfRectangle, error) {
	if p.MediaBox != nil {
		return p.MediaBox, nil
	}
	parent := p.Parent
	for parent != nil {
		parentDict, ok := core.GetDict(parent)
		if !ok {
			return nil, errors.New("invalid parent objects dictionary")
		}
		if mediaBox := parentDict.Get("MediaBox"); mediaBox != nil {
			mediaBoxArr, ok := core.GetArray(mediaBox)
			if !ok {
				return nil, errors.New("invalid media box")
			}
			rect, err := NewPdfRectangle(*mediaBoxArr)
			if err != nil {
				return nil, err
			}
			return rect, nil
		}
		parent = parentDict.Get("Parent")
	}
	return nil, errors.New("media box not defined")
}

// AddImageResource adds an image to the XObject resources.
func (p *PdfPage) AddImageResource(name core.PdfObjectName, ximg *XObjectImage) error {
	var dict *core.PdfObjectDictionary
	if p.Resources.XObject == nil {
		dict = core.MakeDict()
		p.Resources.XObject = dict
	} else {
		var ok bool
		dict, ok = p.Resources.XObject.(*core.PdfObjectDictionary)
		if !ok {
			return errors.New("invalid xres dict type")
		}
	}
	dict.Set(name, ximg.ToPdfObject())
	return nil
}

// RemveTabOrder removes the tab order for the page.
func (p *PdfPage) RemoveTabOrder() { p.Tabs = nil }

func pageToPdfObjectLicensed(page *PdfPage) core.PdfObject {
	licenseKey := license.GetLicenseKey()
	if licenseKey != nil && licenseKey.IsLicensed() {
		return page.ToPdfObject()
	}
	name := core.PdfObjectName("UF1")
	if !page.Resources.HasFontByName(name) {
		page.Resources.SetFontByName(name, DefaultFont().ToPdfObject())
	}
	var strs []string
	strs = append(strs, "q")
	strs = append(strs, "BT")
	strs = append(strs, fmt.Sprintf("/%s 14 Tf", name.String()))
	strs = append(strs, "1 0 0 rg")
	strs = append(strs, "10 10 Td")
	s := "Unlicensed OpenPDF"
	strs = append(strs, fmt.Sprintf("(%s) Tj", s))
	strs = append(strs, "ET")
	strs = append(strs, "Q")
	join := strings.Join(strs, "\n")
	page.AddContentStreamByString(join)
	return page.ToPdfObject()
}

// AddFont adds a font dictionary to the Font resources.
func (p *PdfPage) AddFont(name core.PdfObjectName, font core.PdfObject) error {
	if p.Resources == nil {
		p.Resources = NewPdfPageResources()
	}
	if p.Resources.Font == nil {
		p.Resources.Font = core.MakeDict()
	}
	dict, ok := core.TraceToDirectObject(p.Resources.Font).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("Expected font dictionary is not a dictionary: %v", core.TraceToDirectObject(p.Resources.Font))
		return errors.New("type check error")
	}
	dict.Set(name, font)
	return nil
}

func (p *PdfPage) removeXObj(dict *core.PdfObjectDictionary, name core.PdfObjectName) {
	dict.Remove(name)
	flateEncoder := core.NewFlateEncoder()
	contentStreamObjs := p.GetContentStreamObjs()
	array := core.MakeArray()
	for _, contentStreamObj := range contentStreamObjs {
		s, _ := pdfObjectToString(contentStreamObj)
		if strings.Contains(s, name.String()) {
			index := strings.Index(s, name.String())
			lastIndex := strings.LastIndex(s[:index], "q")
			n := strings.Index(s[index:], "Q") + index + 1
			if lastIndex >= 0 && n >= 0 {
				var builder strings.Builder
				builder.WriteString(s[:lastIndex])
				builder.WriteString(s[n:])
				stream, err := core.MakeStream([]byte(builder.String()), flateEncoder)
				if err == nil {
					array.Append(stream)
					continue
				}
			}
		}
		array.Append(contentStreamObj)
	}
	p.Contents = array
}

// RemoveWatermarkImage removes watermark.
// If you don't know exact name of watermark PDF object, pass empty string as an argument.
func (p *PdfPage) RemoveWatermarkImage(name string) error {
	if p.Resources != nil && p.Resources.XObject != nil {
		if xObjectDict, ok := core.GetDict(p.Resources.XObject); ok {
			for _, name2 := range xObjectDict.Keys() {
				if name == name2.String() {
					p.removeXObj(xObjectDict, name2)
					continue
				}
				stream, ok := core.GetStream(xObjectDict.Get(name2))
				if !ok {
					continue
				}
				pieceInfo, ok := core.GetDict(stream.Get("PieceInfo"))
				if !ok {
					continue
				}
				adbeCompoundType, ok := core.GetDict(pieceInfo.Get("ADBE_CompoundType"))
				if !ok {
					continue
				}
				if adbeCompoundType.Get("Private") != nil && adbeCompoundType.Get("Private").String() == "Watermark" {
					p.removeXObj(xObjectDict, name2)
				}
			}
		}
	}
	annotations, err := p.GetAnnotations()
	if err == nil {
		annots := make([]*PdfAnnotation, 0)
		for _, annotation := range annotations {
			switch annotation.GetContext().(type) {
			case *PdfAnnotationWatermark:
			default:
				annots = append(annots, annotation)
			}
		}
		p.SetAnnotations(annots)
	}
	return nil
}

func (p *PdfPage) loadFont(s string) (*truetype.Font, error) {
	file, err := os.ReadFile(s)
	if err != nil {
		return nil, err
	}
	font, err := freetype.ParseFont(file)
	if err != nil {
		return nil, err
	}
	return font, nil
}

// RemoveStructParentsKey removes the StructParents key.
func (p *PdfPage) RemoveStructParentsKey() { p.StructParents = nil }

// PageProcessCallback callback function used in page loading
// that could be used to modify the page content.
//
// If an error is returned, the `ToWriter` process would fail.
type PageProcessCallback func(n int, page *PdfPage) error

// ToPdfObject converts the PdfPage to a dictionary within an indirect object container.
func (p *PdfPage) ToPdfObject() core.PdfObject {
	indirect := p.primitive
	p.GetPageDict()
	return indirect
}

func (p *PdfPage) generateImage(text string, fontSize float64, fontPath string, fontColor color.Color) ([]byte, error) {
	rgba, n, count, _ := fontColor.RGBA()
	rgba2 := color.RGBA{uint8(rgba >> 8), uint8(n >> 8), uint8(count >> 8), 255}
	rgba3 := color.RGBA{0xff, 0xff, 0xff, 0x00}
	font, err := p.loadFont(fontPath)
	if err != nil {
		return nil, err
	}
	replaceAll := strings.ReplaceAll(text, "\t", "    ")
	split := strings.Split(replaceAll, "\n")
	f := 1.0
	for _, s := range split {
		if float64(len(s))*fontSize > f {
			f = float64(len(s)) * fontSize
		}
	}
	if f > p.MediaBox.Width() {
		f = p.MediaBox.Width()
	}
	uniform := image.NewUniform(rgba2)
	uniform2 := image.NewUniform(rgba3)
	rgba4 := image.NewRGBA(image.Rect(0, 0, int(f), int(fontSize*1.5*float64(len(split)))))
	draw.Draw(rgba4, rgba4.Bounds(), uniform2, image.Pt(0, 0), draw.Src)
	ctx := freetype.NewContext()
	ctx.SetDPI(72)
	ctx.SetFont(font)
	ctx.SetFontSize(fontSize)
	ctx.SetHinting(imagefont.HintingNone)
	ctx.SetClip(rgba4.Bounds())
	ctx.SetDst(rgba4)
	ctx.SetSrc(uniform)
	num := 50
	size := 10 + int(ctx.PointToFixed(fontSize)>>6)
	point26_6 := freetype.Pt(num, size)
	for _, s := range split {
		_, err = ctx.DrawString(strings.ReplaceAll(s, "\r", ""), point26_6)
		if err != nil {
			return nil, err
		}
		point26_6.Y += ctx.PointToFixed(fontSize * 1.5)
	}
	buf := new(bytes.Buffer)
	if err := png.Encode(buf, rgba4); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// HasFontByName checks if has font resource by name.
func (p *PdfPage) HasFontByName(name core.PdfObjectName) bool {
	dict, ok := p.Resources.Font.(*core.PdfObjectDictionary)
	if !ok {
		return false
	}
	if obj := dict.Get(name); obj != nil {
		return true
	}
	return false
}
