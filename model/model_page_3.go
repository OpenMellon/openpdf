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
	"math"
	"os"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/unidoc/freetype"
	"github.com/unidoc/freetype/truetype"
	imagefont "golang.org/x/image/font"
)

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

// GetStructParentsKey returns the StructParents key.
// If not set, returns -1.
func (p *PdfPage) GetStructParentsKey() int {
	if n, ok := core.GetIntVal(p.StructParents); ok {
		return n
	}
	return -1
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
