package model

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
)

// HasXObjectByName checks if an XObject with a specified keyName is defined.
func (r *PdfPageResources) HasXObjectByName(keyName core.PdfObjectName) bool {
	xObjectByName, _ := r.GetXObjectByName(keyName)
	return xObjectByName != nil
}

// HasXObjectByName checks if has XObject resource by name.
func (p *PdfPage) HasXObjectByName(name core.PdfObjectName) bool {
	dict, ok := p.Resources.XObject.(*core.PdfObjectDictionary)
	if !ok {
		return false
	}
	if obj := dict.Get(name); obj != nil {
		return true
	}
	return false
}

// GetXObjectFormByName returns the XObjectForm with the specified name from the
// page resources, if it exists.
func (r *PdfPageResources) GetXObjectFormByName(keyName core.PdfObjectName) (*XObjectForm, error) {
	xObjectByName, xObjectType := r.GetXObjectByName(keyName)
	if xObjectByName == nil {
		return nil, nil
	}
	if xObjectType != XObjectTypeForm {
		return nil, errors.New("not a form")
	}
	xObjectForm, err := NewXObjectFormFromStream(xObjectByName)
	if err != nil {
		return nil, err
	}
	return xObjectForm, nil
}

// SetXObjectImageByNameLazy adds the provided XObjectImage to the page resources.
// The added XObjectImage is identified by the specified name.
func (r *PdfPageResources) SetXObjectImageByNameLazy(keyName core.PdfObjectName, ximg *XObjectImage, lazy bool) error {
	stream := ximg.ToPdfObject().(*core.PdfObjectStream)
	if lazy {
		stream.MakeLazy()
	}
	err := r.SetXObjectByName(keyName, stream)
	return err
}

// GetXObjectByName returns the XObject with the specified keyName and the object type.
func (r *PdfPageResources) GetXObjectByName(keyName core.PdfObjectName) (*core.PdfObjectStream, XObjectType) {
	if r.XObject == nil {
		return nil, XObjectTypeUndefined
	}
	dict, ok := core.TraceToDirectObject(r.XObject).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: XObject not a dictionary! (got %T)", core.TraceToDirectObject(r.XObject))
		return nil, XObjectTypeUndefined
	}
	if obj := dict.Get(keyName); obj != nil {
		stream, ok := core.GetStream(obj)
		if !ok {
			common.Log.Debug("XObject not pointing to a stream %T", obj)
			return nil, XObjectTypeUndefined
		}
		dict := stream.PdfObjectDictionary
		name, ok := core.TraceToDirectObject(dict.Get("Subtype")).(*core.PdfObjectName)
		if !ok {
			common.Log.Debug("XObject Subtype not a Name, dict: %s", dict.String())
			return nil, XObjectTypeUndefined
		}
		if *name == "Image" {
			return stream, XObjectTypeImage
		} else if *name == "Form" {
			return stream, XObjectTypeForm
		} else if *name == "PS" {
			return stream, XObjectTypePS
		} else {
			common.Log.Debug("XObject Subtype not known (%s)", *name)
			return nil, XObjectTypeUndefined
		}
	} else {
		return nil, XObjectTypeUndefined
	}
}

// Size returns the width and the height of the page. The method reports
// the page dimensions as displayed by a PDF viewer (i.e. page rotation is
// taken into account).
func (p *PdfPage) Size() (float64, float64, error) {
	mediaBox, err := p.GetMediaBox()
	if err != nil {
		return 0, 0, err
	}
	width, height := mediaBox.Width(), mediaBox.Height()
	rotate, err := p.GetRotate()
	if err != nil {
		common.Log.Debug("ERROR: %s - ignoring and assuming no rotation\n", err.Error())
	}
	if n := rotate; n%360 != 0 && n%90 == 0 {
		if count := (360 + n%360) % 360; count == 90 || count == 270 {
			width, height = height, width
		}
	}
	return width, height, nil
}

// AddWatermarkText adds a text watermark to the page.
func (p *PdfPage) AddWatermarkText(text string, opt WatermarkTextOptions) error {
	if text == "" {
		return errors.New("no text set")
	}
	if opt.FontPath == "" {
		return errors.New("no font path set")
	}
	if opt.Alpha == 0 {
		opt.Alpha = 0.5
	}
	if opt.FontSize == 0 {
		opt.FontSize = 20.0
	}
	if opt.FontColor == nil {
		opt.FontColor = color.Black
	}
	generateImage, err := p.generateImage(text, opt.FontSize, opt.FontPath, opt.FontColor)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(bytes.NewReader(generateImage))
	if err != nil {
		return err
	}
	img2, err := ImageHandling.NewImageFromGoImage(img)
	if err != nil {
		return err
	}
	xObjectImage, err := NewXObjectImageFromImage(img2, nil, core.NewFlateEncoder())
	if err != nil {
		return err
	}
	watermarkImageOptions := WatermarkImageOptions{Alpha: opt.Alpha, FitToWidth: false, PreserveAspectRatio: true, Angle: opt.Angle}
	return p.AddWatermarkImage(xObjectImage, watermarkImageOptions)
}

// GetXObjectImageByName returns the XObjectImage with the specified name from the
// page resources, if it exists.
func (r *PdfPageResources) GetXObjectImageByName(keyName core.PdfObjectName) (*XObjectImage, error) {
	xObjectByName, xObjectType := r.GetXObjectByName(keyName)
	if xObjectByName == nil {
		return nil, nil
	}
	if xObjectType != XObjectTypeImage {
		return nil, errors.New("not an image")
	}
	xObjectImage, err := NewXObjectImageFromStream(xObjectByName)
	if err != nil {
		return nil, err
	}
	return xObjectImage, nil
}

// GetStructParentsKey returns the StructParents key.
// If not set, returns -1.
func (p *PdfPage) GetStructParentsKey() int {
	if n, ok := core.GetIntVal(p.StructParents); ok {
		return n
	}
	return -1
}

// SetXObjectByName adds the XObject from the passed in stream to the page resources.
// The added XObject is identified by the specified name.
func (r *PdfPageResources) SetXObjectByName(keyName core.PdfObjectName, stream *core.PdfObjectStream) error {
	if r.XObject == nil {
		r.XObject = core.MakeDict()
	}
	obj := core.TraceToDirectObject(r.XObject)
	dict, ok := obj.(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("Invalid XObject, got %T/%T", r.XObject, obj)
		return errors.New("type check error")
	}
	dict.Set(keyName, stream)
	return nil
}

// SetXObjectFormByName adds the provided XObjectForm to the page resources.
// The added XObjectForm is identified by the specified name.
func (r *PdfPageResources) SetXObjectFormByName(keyName core.PdfObjectName, xform *XObjectForm) error {
	stream := xform.ToPdfObject().(*core.PdfObjectStream)
	err := r.SetXObjectByName(keyName, stream)
	return err
}

// GetStructRoot gets the StructTreeRoot object
func (p *PdfPage) GetStructTreeRoot() (*core.PdfObject, bool) {
	catalogStructTreeRoot, ok := p.reader.GetCatalogStructTreeRoot()
	return &catalogStructTreeRoot, ok
}

// GetXObjectByName gets XObject by name.
func (p *PdfPage) GetXObjectByName(name core.PdfObjectName) (core.PdfObject, bool) {
	dict, ok := p.Resources.XObject.(*core.PdfObjectDictionary)
	if !ok {
		return nil, false
	}
	if obj := dict.Get(name); obj != nil {
		return obj, true
	}
	return nil, false
}

// SetTabOrder sets the tab order for the page.
func (p *PdfPage) SetTabOrder(order TabOrderType) { p.Tabs = core.MakeName(string(order)) }

// AddWatermarkImage adds an image watermark to the page.
func (p *PdfPage) AddWatermarkImage(ximg *XObjectImage, opt WatermarkImageOptions) error {
	mediaBox, err := p.GetMediaBox()
	if err != nil {
		return err
	}
	f := mediaBox.Urx - mediaBox.Llx
	v := mediaBox.Ury - mediaBox.Lly
	val := opt.Angle * math.Pi / 180
	width := float64(*ximg.Width)
	x := (f - width*math.Cos(val)) / 2
	if opt.FitToWidth {
		width = f
		x = 0
	}
	y := v
	y2 := float64(0)
	if opt.PreserveAspectRatio {
		y = width * float64(*ximg.Height) / float64(*ximg.Width)
		y2 = (v - y) / 2
		y2 -= float64(*ximg.Width) / 2 * math.Sin(val)
	}
	if p.Resources == nil {
		p.Resources = NewPdfPageResources()
	}
	xObjectForm := NewXObjectForm()
	xObjectForm.BBox = core.MakeArrayFromFloats([]float64{0, 0, width, y})
	xObjectForm.Resources = NewPdfPageResources()
	n := 0
	name := core.PdfObjectName(fmt.Sprintf("Imw%d", n))
	for xObjectForm.Resources.HasXObjectByName(name) {
		n++
		name = core.PdfObjectName(fmt.Sprintf("Imw%d", n))
	}
	err = xObjectForm.Resources.SetXObjectImageByName(name, ximg)
	if err != nil {
		return err
	}
	n = 0
	name2 := core.PdfObjectName(fmt.Sprintf("GS%d", n))
	for xObjectForm.Resources.HasExtGState(name2) {
		n++
		name2 = core.PdfObjectName(fmt.Sprintf("GS%d", n))
	}
	dict := core.MakeDict()
	dict.Set("BM", core.MakeName("Normal"))
	dict.Set("CA", core.MakeFloat(opt.Alpha))
	dict.Set("ca", core.MakeFloat(opt.Alpha))
	err = xObjectForm.Resources.AddExtGState(name2, dict)
	if err != nil {
		return err
	}
	s := fmt.Sprintf("q\n"+"/%s gs\n"+"%.0f 0 0 %.0f %.4f %.4f cm\n"+"/%s Do\n"+"Q", name2, width, y, 0.0, 0.0, name)
	xObjectForm.SetContentStream([]byte(s), nil)
	rotate := transform.IdentityMatrix().Rotate(val)
	floats := []float64{rotate[0], rotate[1], rotate[3], rotate[4], rotate[6], rotate[7]}
	xObjectForm.Matrix = core.MakeArrayFromFloats(floats)
	generateXObjectName := p.Resources.GenerateXObjectName()
	err = p.Resources.SetXObjectFormByName(generateXObjectName, xObjectForm)
	if err != nil {
		return err
	}
	s2 := fmt.Sprintf("q\n"+"%.0f 0 0 %.0f %.4f %.4f cm\n"+"/%s Do\n"+"Q", 1.0, 1.0, x, y2, generateXObjectName)
	return p.AppendContentBytes([]byte(s2), true)
}

// SetXObjectImageByName adds the provided XObjectImage to the page resources.
// The added XObjectImage is identified by the specified name.
func (r *PdfPageResources) SetXObjectImageByName(keyName core.PdfObjectName, ximg *XObjectImage) error {
	stream := ximg.ToPdfObject().(*core.PdfObjectStream)
	err := r.SetXObjectByName(keyName, stream)
	return err
}

// SetStructParentsKey sets the StructParents key.
func (p *PdfPage) SetStructParentsKey(key int) {
	if key == -1 {
		p.StructParents = nil
	} else {
		p.StructParents = core.MakeInteger(int64(key))
	}
}

// GetRotate gets the inheritable rotate value, either from the page
// or a higher up page/pages struct.
func (p *PdfPage) GetRotate() (int64, error) {
	if p.Rotate != nil {
		return *p.Rotate, nil
	}
	parent := p.Parent
	for parent != nil {
		parentDict, ok := core.GetDict(parent)
		if !ok {
			return 0, errors.New("invalid parent objects dictionary")
		}
		if rotate := parentDict.Get("Rotate"); rotate != nil {
			rotateInt, ok := core.GetInt(rotate)
			if !ok {
				return 0, errors.New("invalid rotate value")
			}
			if rotateInt != nil {
				return int64(*rotateInt), nil
			}
			return 0, errors.New("rotate value is nil")
		}
		parent = parentDict.Get("Parent")
	}
	return 0, errors.New("rotate not defined")
}
