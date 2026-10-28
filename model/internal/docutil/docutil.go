// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package docutil

import (
	"errors"
	"fmt"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

func (d *Document) GetPages() ([]Page, bool) {
	catalog, ok := d.FindCatalog()
	if !ok {
		return nil, false
	}
	return catalog.GetPages()
}

type OutputIntents struct {
	array    *core.PdfObjectArray
	doc      *Document
	indirect *core.PdfIndirectObject
}

func (c *Catalog) SetMarkInfo(mi core.PdfObject) {
	if mi == nil {
		c.Object.Remove("MarkInfo")
		return
	}
	ind := core.MakeIndirectObject(mi)
	c.Object.Set("MarkInfo", ind)
	c.doc.Objects = append(c.doc.Objects, ind)
}

func (p Page) GetResourcesXObject() (*core.PdfObjectDictionary, bool) {
	res, ok := p.GetResources()
	if !ok {
		return nil, false
	}
	return core.GetDict(res.Get("XObject"))
}

func (p Page) FindXObjectImages() ([]*Image, error) {
	xobjects, ok := p.GetResourcesXObject()
	if !ok {
		return nil, nil
	}
	var images []*Image
	var err error
	indexByStream := map[*core.PdfObjectStream]int{}
	smasks := map[*core.PdfObjectStream]struct{}{}
	var idx int
	for _, key := range xobjects.Keys() {
		stream, ok := core.GetStream(xobjects.Get(key))
		if !ok {
			continue
		}
		if _, seen := indexByStream[stream]; seen {
			continue
		}
		subtype, ok := core.GetName(stream.Get("Subtype"))
		if !ok || subtype.String() != "Image" {
			continue
		}
		img := Image{BitsPerComponent: 8, Stream: stream, Name: string(key)}
		if img.Colorspace, err = colorSpaceName(stream.Get("ColorSpace")); err != nil {
			common.Log.Error("Error determine color space %s", err)
			continue
		}
		if bpc, ok := core.GetIntVal(stream.Get("BitsPerComponent")); ok {
			img.BitsPerComponent = bpc
		}
		if w, ok := core.GetIntVal(stream.Get("Width")); ok {
			img.Width = w
		}
		if h, ok := core.GetIntVal(stream.Get("Height")); ok {
			img.Height = h
		}
		if smask, ok := core.GetStream(stream.Get("SMask")); ok {
			img.SMask = &ImageSMask{Image: &img, Stream: smask}
			smasks[smask] = struct{}{}
		}
		switch img.Colorspace {
		case "DeviceRGB":
			img.ColorComponents = 3
		case "DeviceGray":
			img.ColorComponents = 1
		case "DeviceCMYK":
			img.ColorComponents = 4
		default:
			img.ColorComponents = -1
		}
		indexByStream[stream] = idx
		images = append(images, &img)
		idx++
	}
	var smaskIdx []int
	for _, im := range images {
		if im.SMask != nil {
			i, found := indexByStream[im.SMask.Stream]
			if found {
				smaskIdx = append(smaskIdx, i)
			}
		}
	}
	filtered := make([]*Image, len(images)-len(smaskIdx))
	idx = 0
outer:
	for i, im := range images {
		for _, j := range smaskIdx {
			if i == j {
				continue outer
			}
		}
		filtered[idx] = im
		idx++
	}
	return images, nil
}

func (c *Catalog) SetOutputIntents(outputIntents *OutputIntents) {
	if existing := c.Object.Get("OutputIntents"); existing != nil {
		for i, o := range c.doc.Objects {
			if o == existing {
				if outputIntents.indirect == existing {
					return
				}
				c.doc.Objects = append(c.doc.Objects[:i], c.doc.Objects[i+1:]...)
				break
			}
		}
	}
	ind := outputIntents.indirect
	if ind == nil {
		ind = core.MakeIndirectObject(outputIntents.array)
	}
	c.Object.Set("OutputIntents", ind)
	c.doc.Objects = append(c.doc.Objects, ind)
}

func (c *Catalog) HasMetadata() bool {
	md := c.Object.Get("Metadata")
	return md != nil
}

type OutputIntent struct{ Object *core.PdfObjectDictionary }

func (c *Catalog) GetPages() ([]Page, bool) {
	pages, ok := core.GetDict(c.Object.Get("Pages"))
	if !ok {
		return nil, false
	}
	kids, ok := core.GetArray(pages.Get("Kids"))
	if !ok {
		return nil, false
	}
	result := make([]Page, kids.Len())
	for i, kid := range kids.Elements() {
		dict, ok := core.GetDict(kid)
		if !ok {
			continue
		}
		result[i] = Page{Object: dict, number: i + 1, doc: c.doc}
	}
	return result, true
}
func (o *OutputIntents) Len() int { return o.array.Len() }
func (c *Catalog) SetMetadata(data []byte) error {
	stream, err := core.MakeStream(data, nil)
	if err != nil {
		return err
	}
	stream.Set("Type", core.MakeName("Metadata"))
	stream.Set("Subtype", core.MakeName("XML"))
	c.Object.Set("Metadata", stream)
	c.doc.Objects = append(c.doc.Objects, stream)
	return nil
}

func (c *Catalog) GetOutputIntents() (*OutputIntents, bool) {
	obj := c.Object.Get("OutputIntents")
	if obj == nil {
		return nil, false
	}
	ind, ok := core.GetIndirect(obj)
	if !ok {
		return nil, false
	}
	arr, ok := core.GetArray(ind.PdfObject)
	if !ok {
		return nil, false
	}
	return &OutputIntents{indirect: ind, array: arr, doc: c.doc}, true
}
func (p *Page) Number() int { return p.number }
func (d *Document) FindCatalog() (*Catalog, bool) {
	var catalogDict *core.PdfObjectDictionary
	for _, obj := range d.Objects {
		dict, ok := core.GetDict(obj)
		if !ok {
			continue
		}
		if name, ok := core.GetName(dict.Get("Type")); ok && *name == "Catalog" {
			catalogDict = dict
			break
		}
	}
	if catalogDict == nil {
		return nil, false
	}
	return &Catalog{Object: catalogDict, doc: d}, true
}

type Page struct {
	number int
	Object *core.PdfObjectDictionary
	doc    *Document
}
type Catalog struct {
	Object *core.PdfObjectDictionary
	doc    *Document
}

func (p Page) GetContents() ([]Content, bool) {
	arr, ok := core.GetArray(p.Object.Get("Contents"))
	if !ok {
		stream, ok := core.GetStream(p.Object.Get("Contents"))
		if !ok {
			return nil, false
		}
		return []Content{{Stream: stream, page: p, index: 0}}, true
	}
	contents := make([]Content, arr.Len())
	for i, el := range arr.Elements() {
		stream, ok := core.GetStream(el)
		if !ok {
			continue
		}
		contents[i] = Content{Stream: stream, page: p, index: i}
	}
	return contents, true
}

type ImageSMask struct {
	Image  *Image
	Stream *core.PdfObjectStream
}

func (c *Content) SetData(data []byte) error {
	stream, err := core.MakeStream(data, core.NewFlateEncoder())
	if err != nil {
		return err
	}
	arr, ok := core.GetArray(c.page.Object.Get("Contents"))
	if !ok && c.index == 0 {
		c.page.Object.Set("Contents", stream)
	} else {
		if err = arr.Set(c.index, stream); err != nil {
			return err
		}
	}
	c.page.doc.Objects = append(c.page.doc.Objects, stream)
	return nil
}

func (c *Catalog) GetStructTreeRoot() (*core.PdfObjectDictionary, bool) {
	return core.GetDict(c.Object.Get("StructTreeRoot"))
}

func colorSpaceName(obj core.PdfObject) (core.PdfObjectName, error) {
	var name *core.PdfObjectName
	var arr *core.PdfObjectArray
	if ind, ok := obj.(*core.PdfIndirectObject); ok {
		if a, ok := ind.PdfObject.(*core.PdfObjectArray); ok {
			arr = a
		} else if n, ok := ind.PdfObject.(*core.PdfObjectName); ok {
			name = n
		}
	} else if a, ok := obj.(*core.PdfObjectArray); ok {
		arr = a
	} else if n, ok := obj.(*core.PdfObjectName); ok {
		name = n
	}
	if name != nil {
		switch *name {
		case "DeviceGray", "DeviceRGB", "DeviceCMYK":
			return *name, nil
		case "Pattern":
			return *name, nil
		}
	}
	if arr != nil && arr.Len() > 0 {
		if first, ok := arr.Get(0).(*core.PdfObjectName); ok {
			switch *first {
			case "DeviceGray", "DeviceRGB", "DeviceCMYK":
				if arr.Len() == 1 {
					return *first, nil
				}
			case "CalGray", "CalRGB", "Lab":
				return *first, nil
			case "ICCBased", "Pattern", "Indexed":
				return *first, nil
			case "Separation", "DeviceN":
				return *first, nil
			}
		}
	}
	return "", nil
}

func (d *Document) AddIndirectObject(indirect *core.PdfIndirectObject) {
	for _, o := range d.Objects {
		if o == indirect {
			return
		}
	}
	d.Objects = append(d.Objects, indirect)
}

func (c Content) GetData() ([]byte, error) {
	enc, err := core.NewEncoderFromStream(c.Stream)
	if err != nil {
		return nil, err
	}
	data, err := enc.DecodeStream(c.Stream)
	if err != nil {
		return nil, err
	}
	return data, nil
}

type Content struct {
	Stream *core.PdfObjectStream
	index  int
	page   Page
}
type Image struct {
	Name             string
	Width            int
	Height           int
	Colorspace       core.PdfObjectName
	ColorComponents  int
	BitsPerComponent int
	SMask            *ImageSMask
	Stream           *core.PdfObjectStream
}

func (c *Catalog) NewOutputIntents() *OutputIntents { return &OutputIntents{doc: c.doc} }
func (o *OutputIntents) Add(oi core.PdfObject) error {
	dict, ok := oi.(*core.PdfObjectDictionary)
	if !ok {
		return errors.New("input output intent should be an object dictionary")
	}
	if profile, ok := core.GetStream(dict.Get("DestOutputProfile")); ok {
		o.doc.Objects = append(o.doc.Objects, profile)
	}
	ind, ok := oi.(*core.PdfIndirectObject)
	if !ok {
		ind = core.MakeIndirectObject(oi)
	}
	if o.array == nil {
		o.array = core.MakeArray(ind)
	} else {
		o.array.Append(ind)
	}
	o.doc.Objects = append(o.doc.Objects, ind)
	return nil
}

func (p Page) FindXObjectForms() []*core.PdfObjectStream {
	xobjects, ok := p.GetResourcesXObject()
	if !ok {
		return nil
	}
	forms := map[*core.PdfObjectStream]struct{}{}
	var collect func(_ *core.PdfObjectDictionary, _ map[*core.PdfObjectStream]struct{})
	collect = func(xo *core.PdfObjectDictionary, visited map[*core.PdfObjectStream]struct{}) {
		for _, key := range xo.Keys() {
			stream, ok := core.GetStream(xo.Get(key))
			if !ok {
				continue
			}
			if _, seen := visited[stream]; seen {
				continue
			}
			subtype, ok := core.GetName(stream.Get("Subtype"))
			if !ok || subtype.String() != "Form" {
				continue
			}
			visited[stream] = struct{}{}
			res, ok := core.GetDict(stream.Get("Resources"))
			if !ok {
				continue
			}
			nested, ok := core.GetDict(res.Get("XObject"))
			if ok {
				collect(nested, visited)
			}
		}
	}
	collect(xobjects, forms)
	var result []*core.PdfObjectStream
	for f := range forms {
		result = append(result, f)
	}
	return result
}

func (o *OutputIntents) Get(i int) (OutputIntent, bool) {
	if o.array == nil {
		return OutputIntent{}, false
	}
	if i >= o.array.Len() {
		return OutputIntent{}, false
	}
	obj := o.array.Get(i)
	ind, ok := core.GetIndirect(obj)
	if !ok {
		dict, ok := core.GetDict(obj)
		return OutputIntent{Object: dict}, ok
	}
	dict, ok := core.GetDict(ind.PdfObject)
	return OutputIntent{Object: dict}, ok
}

func (c *Catalog) GetMetadata() (*core.PdfObjectStream, bool) {
	return core.GetStream(c.Object.Get("Metadata"))
}

func (c *Catalog) SetStructTreeRoot(structTreeRoot core.PdfObject) {
	if structTreeRoot == nil {
		c.Object.Remove("StructTreeRoot")
		return
	}
	ind := core.MakeIndirectObject(structTreeRoot)
	c.Object.Set("StructTreeRoot", ind)
	c.doc.Objects = append(c.doc.Objects, ind)
}

type Document struct {
	ID             [2]string
	Version        core.Version
	Objects        []core.PdfObject
	Info           core.PdfObject
	Crypt          *core.PdfCrypt
	UseHashBasedID bool
}

func (c *Catalog) GetMarkInfo() (*core.PdfObjectDictionary, bool) {
	d, ok := core.GetDict(c.Object.Get("MarkInfo"))
	return d, ok
}

func (p Page) GetResources() (*core.PdfObjectDictionary, bool) {
	return core.GetDict(p.Object.Get("Resources"))
}

func (d *Document) AddStream(stream *core.PdfObjectStream) {
	for _, o := range d.Objects {
		if o == stream {
			return
		}
	}
	d.Objects = append(d.Objects, stream)
}

func (c *Catalog) SetVersion() {
	c.Object.Set("Version", core.MakeName(fmt.Sprintf("%d.%d", c.doc.Version.Major, c.doc.Version.Minor)))
}
