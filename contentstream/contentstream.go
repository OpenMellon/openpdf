// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package contentstream provides functionality for parsing and creating content streams for PDF files.
//
// For processing and manipulating content streams, it allows parse the content stream into a list of
// operands that can then be processed further for rendering or extraction of information.
// The ContentStreamProcessor offers a basic engine for processing the content stream and can be used
// to render or modify the contents.
//
// For creating content streams, see NewContentCreator.  It allows adding multiple operands and then can
// be converted to a string for embedding in a PDF file.
//
// The contentstream package uses the core and model packages.
package contentstream

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
	"image/color"
	"image/jpeg"
	"io"
	"math"
	"regexp"
	"strconv"
)

// Add_re appends 're' operand to the content stream:
// Append a rectangle to the current path as a complete subpath, with lower left corner (x,y).
//
// See section 8.5.2 "Path Construction Operators" and Table 59 (pp. 140-141 PDF32000_2008).
func (cc *ContentCreator) Add_re(x, y, width, height float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "re"
	op.Params = floatsToObjects([]float64{x, y, width, height})
	cc.operations = append(cc.operations, &op)
	return cc
}
func isKnownOperand(s string) bool { _, ok := knownOperands[s]; return ok }

// Add_M adds 'M' operand to the content stream: Set the miter limit (graphics state).
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_M(miterlimit float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "M"
	op.Params = floatsToObjects([]float64{miterlimit})
	cc.operations = append(cc.operations, &op)
	return cc
}
func (p *ContentStreamParser) parseBool() (core.PdfObjectBool, error) {
	buf, err := p.reader.Peek(4)
	if err != nil {
		return core.PdfObjectBool(false), err
	}
	if (len(buf) >= 4) && (string(buf[:4]) == "true") {
		p.reader.Discard(4)
		return core.PdfObjectBool(true), nil
	}
	buf, err = p.reader.Peek(5)
	if err != nil {
		return core.PdfObjectBool(false), err
	}
	if (len(buf) >= 5) && (string(buf[:5]) == "false") {
		p.reader.Discard(5)
		return core.PdfObjectBool(false), nil
	}
	return core.PdfObjectBool(false), errors.New("unexpected boolean string")
}

// Bytes converts the content stream operations to a content stream byte presentation, i.e. the kind that can be
// stored as a PDF stream or string format.
func (cc *ContentCreator) Bytes() []byte { return cc.operations.Bytes() }
func (p *ContentStreamParser) parseObject() (obj core.PdfObject, ok bool, err error) {
	p.skipSpaces()
	for {
		buf, err2 := p.reader.Peek(2)
		if err2 != nil {
			return nil, false, err2
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Peek string: %s", string(buf))
		}
		if buf[0] == '%' {
			p.skipComments()
			continue
		} else if buf[0] == '/' {
			name, err3 := p.parseName()
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Name: '%s'", name)
			}
			return &name, false, err3
		} else if buf[0] == '(' {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->String!")
			}
			str, err3 := p.parseString()
			return str, false, err3
		} else if buf[0] == '<' && buf[1] != '<' {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Hex String!")
			}
			str, err3 := p.parseHexString()
			return str, false, err3
		} else if buf[0] == '[' {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Array!")
			}
			arr, err3 := p.parseArray()
			return arr, false, err3
		} else if core.IsFloatDigit(buf[0]) || (buf[0] == '-' && core.IsFloatDigit(buf[1])) || (buf[0] == '+' && core.IsFloatDigit(buf[1])) {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Number!")
			}
			obj2, err3 := p.parseNumber()
			return obj2, false, err3
		} else if buf[0] == '<' && buf[1] == '<' {
			dict, err3 := p.parseDict()
			return dict, false, err3
		} else {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Operand or bool?")
			}
			buf, _ = p.reader.Peek(5)
			s := string(buf)
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("cont Peek str: %s", s)
			}
			if (len(s) > 3) && (s[:4] == "null") {
				v, err3 := p.parseNull()
				return &v, false, err3
			} else if (len(s) > 4) && (s[:5] == "false") {
				v, err3 := p.parseBool()
				return &v, false, err3
			} else if (len(s) > 3) && (s[:4] == "true") {
				v, err3 := p.parseBool()
				return &v, false, err3
			}
			str, err3 := p.parseOperand()
			if err3 != nil {
				return str, false, err3
			}
			if len(str.String()) < 1 {
				return str, false, ErrInvalidOperand
			}
			return str, true, nil
		}
	}
}

// String is same as Bytes() except returns as a string for convenience.
func (cc *ContentCreator) String() string { return string(cc.operations.Bytes()) }

// Scale applies x-y scaling to the transformation matrix.
func (cc *ContentCreator) Scale(sx, sy float64) *ContentCreator {
	return cc.Add_cm(sx, 0, 0, sy, 0, 0)
}

// Add_cm adds 'cm' operation to the content stream: Modifies the current transformation matrix (ctm)
// of the graphics state.
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_cm(a, b, c, d, e, f float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "cm"
	op.Params = floatsToObjects([]float64{a, b, c, d, e, f})
	cc.operations = append(cc.operations, &op)
	return cc
}

type handlerEntry struct {
	Condition HandlerConditionEnum
	Operand   string
	Handler   HandlerFunc
}

// Add_Tz appends 'Tz' operand to the content stream:
// Set horizontal scaling.
//
// See section 9.3 "Text State Parameters and Operators" and
// Table 105 (pp. 251-252 PDF32000_2008).
func (cc *ContentCreator) Add_Tz(scale float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Tz"
	op.Params = floatsToObjects([]float64{scale})
	cc.operations = append(cc.operations, &op)
	return cc
}
func (p *ContentStreamParser) parseString() (*core.PdfObjectString, error) {
	p.reader.ReadByte()
	var buf []byte
	n := 1
	for {
		buf2, err := p.reader.Peek(1)
		if err != nil {
			return core.MakeString(string(buf)), err
		}
		if buf2[0] == '\\' {
			p.reader.ReadByte()
			b, err2 := p.reader.ReadByte()
			if err2 != nil {
				return core.MakeString(string(buf)), err2
			}
			if core.IsOctalDigit(b) {
				buf3, err3 := p.reader.Peek(2)
				if err3 != nil {
					return core.MakeString(string(buf)), err3
				}
				var buf4 []byte
				buf4 = append(buf4, b)
				for _, b2 := range buf3 {
					if core.IsOctalDigit(b2) {
						buf4 = append(buf4, b2)
					} else {
						break
					}
				}
				p.reader.Discard(len(buf4) - 1)
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Numeric string \"%s\"", buf4)
				}
				v, err3 := strconv.ParseUint(string(buf4), 8, 32)
				if err3 != nil {
					return core.MakeString(string(buf)), err3
				}
				buf = append(buf, byte(v))
				continue
			}
			switch b {
			case 'n':
				buf = append(buf, '\n')
			case 'r':
				buf = append(buf, '\r')
			case 't':
				buf = append(buf, '\t')
			case 'b':
				buf = append(buf, '\b')
			case 'f':
				buf = append(buf, '\f')
			case '(':
				buf = append(buf, '(')
			case ')':
				buf = append(buf, ')')
			case '\\':
				buf = append(buf, '\\')
			}
			continue
		} else if buf2[0] == '(' {
			n++
		} else if buf2[0] == ')' {
			n--
			if n == 0 {
				p.reader.ReadByte()
				break
			}
		}
		b, _ := p.reader.ReadByte()
		buf = append(buf, b)
	}
	return core.MakeString(string(buf)), nil
}

// NewInlineImageFromImage makes a new content stream inline image object from an image.
func NewInlineImageFromImage(img model.Image, encoder core.StreamEncoder) (*ContentStreamInlineImage, error) {
	if encoder == nil {
		encoder = core.NewRawEncoder()
	}
	encoder.UpdateParams(img.GetParamsDict())
	v := ContentStreamInlineImage{}
	switch img.ColorComponents {
	case 1:
		v.ColorSpace = core.MakeName("G")
	case 3:
		v.ColorSpace = core.MakeName("RGB")
	case 4:
		v.ColorSpace = core.MakeName("CMYK")
	default:
		common.Log.Debug("Invalid number of color components for inline image: %d", img.ColorComponents)
		return nil, errors.New("invalid number of color components")
	}
	v.BitsPerComponent = core.MakeInteger(img.BitsPerComponent)
	v.Width = core.MakeInteger(img.Width)
	v.Height = core.MakeInteger(img.Height)
	buf, err := encoder.EncodeBytes(img.Data)
	if err != nil {
		return nil, err
	}
	v.data = buf
	s := encoder.GetFilterName()
	if s != core.StreamEncodingFilterNameRaw {
		v.Filter = core.MakeName(s)
	}
	return &v, nil
}

// Add_Tw appends 'Tw' operand to the content stream:
// Set word spacing.
//
// See section 9.3 "Text State Parameters and Operators" and
// Table 105 (pp. 251-252 PDF32000_2008).
func (cc *ContentCreator) Add_Tw(wordSpace float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Tw"
	op.Params = floatsToObjects([]float64{wordSpace})
	cc.operations = append(cc.operations, &op)
	return cc
}

// GetEncoder returns the encoder of the inline image.
func (img *ContentStreamInlineImage) GetEncoder() (core.StreamEncoder, error) {
	return newEncoderFromInlineImage(img)
}
func newDCTEncoderFromInlineImage(img *ContentStreamInlineImage) (*core.DCTEncoder, error) {
	v := core.NewDCTEncoder()
	v2 := bytes.NewReader(img.data)
	v3, err := jpeg.DecodeConfig(v2)
	if err != nil {
		common.Log.Debug("Error decoding file: %s", err)
		return nil, err
	}
	switch v3.ColorModel {
	case color.RGBAModel:
		v.BitsPerComponent = 8
		v.ColorComponents = 3
	case color.RGBA64Model:
		v.BitsPerComponent = 16
		v.ColorComponents = 3
	case color.GrayModel:
		v.BitsPerComponent = 8
		v.ColorComponents = 1
	case color.Gray16Model:
		v.BitsPerComponent = 16
		v.ColorComponents = 1
	case color.CMYKModel:
		v.BitsPerComponent = 8
		v.ColorComponents = 4
	case color.YCbCrModel:
		v.BitsPerComponent = 8
		v.ColorComponents = 3
	default:
		return nil, errors.New("unsupported color model")
	}
	v.Width = v3.Width
	v.Height = v3.Height
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("DCT Encoder: %+v", v)
	}
	return v, nil
}

// ContentStreamProcessor defines a data structure and methods for processing a content stream, keeping track of the
// current graphics state, and allowing external handlers to define their own functions as a part of the processing,
// for example rendering or extracting certain information.
type ContentStreamProcessor struct {
	gsStack              GraphicStateStack
	operations           []*ContentStreamOperation
	graphicsState        GraphicsState
	initialGraphicsState *GraphicsState
	handlers             []handlerEntry
	currentIndex         int
	relaxedMode          bool
}

// Add_Do adds 'Do' operation to the content stream:
// Displays an XObject (image or form) specified by `name`.
//
// See section 8.8 "External Objects" and Table 87 (pp. 209-220 PDF32000_2008).
func (cc *ContentCreator) Add_Do(name core.PdfObjectName) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Do"
	op.Params = namesToObjects([]core.PdfObjectName{name})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_TD appends 'TD' operand to the content stream:
// Move to start of next line with offset (`tx`, `ty`).
//
// See section 9.4.2 "Text Positioning Operators" and
// Table 108 (pp. 257-258 PDF32000_2008).
func (cc *ContentCreator) Add_TD(tx, ty float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "TD"
	op.Params = floatsToObjects([]float64{tx, ty})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_d adds 'd' operand to the content stream: Set the line dash pattern.
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_d(dashArray []int64, dashPhase int64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "d"
	op.Params = []core.PdfObject{}
	op.Params = append(op.Params, core.MakeArrayFromIntegers64(dashArray))
	op.Params = append(op.Params, core.MakeInteger(dashPhase))
	cc.operations = append(cc.operations, &op)
	return cc
}

// GetColorSpace returns the colorspace of the inline image.
func (img *ContentStreamInlineImage) GetColorSpace(resources *model.PdfPageResources) (model.PdfColorspace, error) {
	if img.ColorSpace == nil {
		common.Log.Debug("Inline image not having specified colorspace, assuming Gray")
		return model.NewPdfColorspaceDeviceGray(), nil
	}
	if arr, ok := img.ColorSpace.(*core.PdfObjectArray); ok {
		return newIndexedColorspaceFromArray(arr)
	}
	name, ok := img.ColorSpace.(*core.PdfObjectName)
	if !ok {
		common.Log.Debug("Error: Invalid object type (%T;%+v)", img.ColorSpace, img.ColorSpace)
		return nil, errors.New("type check error")
	}
	switch *name {
	case "G", "DeviceGray":
		return model.NewPdfColorspaceDeviceGray(), nil
	case "RGB", "DeviceRGB":
		return model.NewPdfColorspaceDeviceRGB(), nil
	case "CMYK", "DeviceCMYK":
		return model.NewPdfColorspaceDeviceCMYK(), nil
	case "I", "Indexed":
		return nil, errors.New("unsupported Index colorspace")
	default:
		if resources.ColorSpace == nil {
			common.Log.Debug("Error, unsupported inline image colorspace: %s", *name)
			return nil, errors.New("unknown colorspace")
		}
		cs, ok2 := resources.GetColorspaceByName(*name)
		if !ok2 {
			common.Log.Debug("Error, unsupported inline image colorspace: %s", *name)
			return nil, errors.New("unknown colorspace")
		}
		return cs, nil
	}
}

// Translate applies a simple x-y translation to the transformation matrix.
func (cc *ContentCreator) Translate(tx, ty float64) *ContentCreator {
	return cc.Add_cm(1, 0, 0, 1, tx, ty)
}

// ToImage exports the inline image to Image which can be transformed or exported easily.
// Page resources are needed to look up colorspace information.
func (img *ContentStreamInlineImage) ToImage(resources *model.PdfPageResources) (*model.Image, error) {
	v, err := img.toImageBase(resources)
	if err != nil {
		return nil, err
	}
	v2, err := newEncoderFromInlineImage(img)
	if err != nil {
		return nil, err
	}
	dict, ok := core.GetDict(img.DecodeParms)
	if ok {
		v2.UpdateParams(dict)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("encoder: %+v %T", v2, v2)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("inline image: %+v", img)
	}
	buf, err := v2.DecodeBytes(img.data)
	if err != nil {
		return nil, err
	}
	v3 := &model.Image{Width: int64(v.Width), Height: int64(v.Height), BitsPerComponent: int64(v.BitsPerComponent), ColorComponents: v.ColorComponents, Data: buf}
	if len(v.Decode) > 0 {
		floats := append([]float64(nil), v.Decode...)
		ok2 := false
		switch icc := img.colorspace.(type) {
		case *model.PdfColorspaceSpecialIndexed:
			floats, _ = imageutil.NormalizeIndexedDecode(floats, v.BitsPerComponent)
		case *model.PdfColorspaceDeviceRGB, *model.PdfColorspaceDeviceCMYK:
			ok2 = true
		case *model.PdfColorspaceICCBased:
			ok2 = !icc.IsColorManaged() && (icc.N == 3 || icc.N == 4)
		}
		if ok2 {
			f := float64((int(1) << uint(v.BitsPerComponent)) - 1)
			for n := range floats {
				floats[n] *= f
			}
		}
		if floats != nil {
			v3.SetDecode(floats)
		}
	}
	return v3, nil
}
func (p *ContentStreamParser) parseOperand() (*core.PdfObjectString, error) {
	var buf []byte
	for {
		buf2, err := p.reader.Peek(1)
		if err != nil {
			return core.MakeString(string(buf)), err
		}
		if core.IsDelimiter(buf2[0]) {
			break
		}
		if core.IsWhiteSpace(buf2[0]) {
			break
		}
		b, _ := p.reader.ReadByte()
		buf = append(buf, b)
	}
	return core.MakeString(string(buf)), nil
}

// Add_m adds 'm' operand to the content stream: Move the current point to (x,y).
//
// See section 8.5.2 "Path Construction Operators" and Table 59 (pp. 140-141 PDF32000_2008).
func (cc *ContentCreator) Add_m(x, y float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "m"
	op.Params = floatsToObjects([]float64{x, y})
	cc.operations = append(cc.operations, &op)
	return cc
}

// AddOperand adds a specified operand.
func (cc *ContentCreator) AddOperand(op ContentStreamOperation) *ContentCreator {
	cc.operations = append(cc.operations, &op)
	return cc
}
func (proc *ContentStreamProcessor) handleCommand_cm(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	if len(op.Params) != 6 {
		common.Log.Debug("ERROR: Invalid number of parameters for cm: %d", len(op.Params))
		return errors.New("invalid number of parameters")
	}
	floats, err := core.GetNumbersAsFloat(op.Params)
	if err != nil {
		return err
	}
	v := transform.NewMatrix(floats[0], floats[1], floats[2], floats[3], floats[4], floats[5])
	proc.graphicsState.CTM.Concat(v)
	return nil
}
func (ops *ContentStreamOperations) isWrapped() bool {
	if len(*ops) < 2 {
		return false
	}
	n := 0
	for _, op := range *ops {
		switch op.Operand {
		case "q":
			n++
		case "Q":
			n--
		default:
			if n < 1 {
				return false
			}
		}
	}
	return n == 0
}

// Add_J adds 'J' operand to the content stream: Set the line cap style (graphics state).
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_J(lineCapStyle string) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "J"
	op.Params = namesToObjects([]core.PdfObjectName{core.PdfObjectName(lineCapStyle)})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_b_starred appends 'b*' operand to the content stream:
// Close, fill and then stroke the path (even-odd winding number rule).
//
// See section 8.5.3 "Path Painting Operators" and Table 60 (p. 143 PDF32000_2008).
func (cc *ContentCreator) Add_b_starred() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "b*"
	cc.operations = append(cc.operations, &op)
	return cc
}

// IsMask checks if an image is a mask.
// The image mask entry in the image dictionary specifies that the image data shall be used as a stencil
// mask for painting in the current color. The mask data is 1bpc, grayscale.
func (img *ContentStreamInlineImage) IsMask() (bool, error) {
	if img.ImageMask != nil {
		v, ok := img.ImageMask.(*core.PdfObjectBool)
		if !ok {
			common.Log.Debug("Image mask not a boolean")
			return false, errors.New("invalid object type")
		}
		return bool(*v), nil
	}
	return false, nil
}
func newMultiEncoderFromInlineImage(img *ContentStreamInlineImage) (*core.MultiEncoder, error) {
	v := core.NewMultiEncoder()
	var dict *core.PdfObjectDictionary
	var objs []core.PdfObject
	if obj := img.DecodeParms; obj != nil {
		dict2, ok := obj.(*core.PdfObjectDictionary)
		if ok {
			dict = dict2
		}
		arr, ok2 := obj.(*core.PdfObjectArray)
		if ok2 {
			for _, obj2 := range arr.Elements() {
				if dict3, ok3 := obj2.(*core.PdfObjectDictionary); ok3 {
					objs = append(objs, dict3)
				} else {
					objs = append(objs, nil)
				}
			}
		}
	}
	obj := img.Filter
	if obj == nil {
		return nil, fmt.Errorf("filter missing")
	}
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("multi filter can only be made from array")
	}
	for n, obj2 := range arr.Elements() {
		name, ok2 := obj2.(*core.PdfObjectName)
		if !ok2 {
			return nil, fmt.Errorf("multi filter array element not a name")
		}
		var obj3 core.PdfObject
		if dict != nil {
			obj3 = dict
		} else {
			if len(objs) > 0 {
				if n >= len(objs) {
					return nil, fmt.Errorf("missing elements in decode params array")
				}
				obj3 = objs[n]
			}
		}
		var dict2 *core.PdfObjectDictionary
		if dict3, ok3 := obj3.(*core.PdfObjectDictionary); ok3 {
			dict2 = dict3
		}
		switch *name {
		case core.StreamEncodingFilterNameFlate, "Fl":
			v2, err := newFlateEncoderFromInlineImage(img, dict2)
			if err != nil {
				return nil, err
			}
			v.AddEncoder(v2)
		case core.StreamEncodingFilterNameLZW:
			v2, err := newLZWEncoderFromInlineImage(img, dict2)
			if err != nil {
				return nil, err
			}
			v.AddEncoder(v2)
		case core.StreamEncodingFilterNameASCIIHex:
			v2 := core.NewASCIIHexEncoder()
			v.AddEncoder(v2)
		case core.StreamEncodingFilterNameASCII85, "A85":
			v2 := core.NewASCII85Encoder()
			v.AddEncoder(v2)
		default:
			common.Log.Error("Unsupported filter %s", *name)
			return nil, fmt.Errorf("invalid filter in multi filter array")
		}
	}
	return v, nil
}

// Add_K appends 'K' operand to the content stream:
// Set the stroking colorspace to DeviceCMYK and sets the c,m,y,k color (0-1 each component).
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_K(c, m, y, k float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "K"
	op.Params = floatsToObjects([]float64{c, m, y, k})
	cc.operations = append(cc.operations, &op)
	return cc
}
func autoConvertRGBToGray(objs []core.PdfObject) (core.PdfObject, error) {
	common.Log.Debug("Auto convert color to grayscale, you can turn off this by setting RelaxedMode to false")
	if len(objs) != 3 {
		return nil, errInvalidNumberOfParams
	}
	floats, err := core.GetNumbersAsFloat(objs)
	if err != nil {
		return nil, err
	}
	n := int(math.Round(floats[0] * 255))
	n2 := int(math.Round(floats[1] * 255))
	n3 := int(math.Round(floats[2] * 255))
	n4 := core.RGBToGrayscale(n, n2, n3)
	return core.MakeFloat(float64(n4) / 255.0), nil
}

// Add_quote appends "'" operand to the content stream:
// Move to next line and show a string.
//
// See section 9.4.3 "Text Showing Operators" and
// Table 209 (pp. 258-259 PDF32000_2008).
func (cc *ContentCreator) Add_quote(textstr core.PdfObjectString) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "'"
	op.Params = stringsToObjects([]core.PdfObjectString{textstr})
	cc.operations = append(cc.operations, &op)
	return cc
}
func (proc *ContentStreamProcessor) getColorspace(s string, resources *model.PdfPageResources) (model.PdfColorspace, error) {
	switch s {
	case "DeviceGray":
		return model.NewPdfColorspaceDeviceGray(), nil
	case "DeviceRGB":
		return model.NewPdfColorspaceDeviceRGB(), nil
	case "DeviceCMYK":
		return model.NewPdfColorspaceDeviceCMYK(), nil
	case "Pattern":
		return model.NewPdfColorspaceSpecialPattern(), nil
	}
	if resources != nil {
		cs, ok := resources.GetColorspaceByName(core.PdfObjectName(s))
		if ok {
			return cs, nil
		}
	}
	switch s {
	case "CalGray":
		return model.NewPdfColorspaceCalGray(), nil
	case "CalRGB":
		return model.NewPdfColorspaceCalRGB(), nil
	case "Lab":
		return model.NewPdfColorspaceLab(), nil
	}
	common.Log.Debug("Unknown colorspace requested: %s", s)
	return nil, fmt.Errorf("unsupported colorspace: %s", s)
}

// Add_Tm appends 'Tm' operand to the content stream:
// Set the text line matrix.
//
// See section 9.4.2 "Text Positioning Operators" and
// Table 108 (pp. 257-258 PDF32000_2008).
func (cc *ContentCreator) Add_Tm(a, b, c, d, e, f float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Tm"
	op.Params = floatsToObjects([]float64{a, b, c, d, e, f})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_Ts appends 'Ts' operand to the content stream:
// Set text rise.
//
// See section 9.3 "Text State Parameters and Operators" and
// Table 105 (pp. 251-252 PDF32000_2008).
func (cc *ContentCreator) Add_Ts(rise float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Ts"
	op.Params = floatsToObjects([]float64{rise})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_c adds 'c' operand to the content stream: Append a Bezier curve to the current path from
// the current point to (x3,y3) with (x1,x1) and (x2,y2) as control points.
//
// See section 8.5.2 "Path Construction Operators" and Table 59 (pp. 140-141 PDF32000_2008).
func (cc *ContentCreator) Add_c(x1, y1, x2, y2, x3, y3 float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "c"
	op.Params = floatsToObjects([]float64{x1, y1, x2, y2, x3, y3})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_W appends 'W' operand to the content stream:
// Modify the current clipping path by intersecting with the current path (nonzero winding rule).
//
// See section 8.5.4 "Clipping Path Operators" and Table 61 (p. 146 PDF32000_2008).
func (cc *ContentCreator) Add_W() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "W"
	cc.operations = append(cc.operations, &op)
	return cc
}
func (p *ContentStreamParser) parseNumber() (core.PdfObject, error) {
	return core.ParseNumber(p.reader)
}

// Add_rg appends 'rg' operand to the content stream:
// Same as RG but used for nonstroking operations.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_rg(r, g, b float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "rg"
	op.Params = floatsToObjects([]float64{r, g, b})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_ET appends 'ET' operand to the content stream:
// End text.
//
// See section 9.4 "Text Objects" and Table 107 (p. 256 PDF32000_2008).
func (cc *ContentCreator) Add_ET() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "ET"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_k appends 'k' operand to the content stream:
// Same as K but used for nonstroking operations.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_k(c, m, y, k float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "k"
	op.Params = floatsToObjects([]float64{c, m, y, k})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Direct returns the object itself; an inline image is always a direct object.
func (img *ContentStreamInlineImage) Direct() core.PdfObject { return img }
func (proc *ContentStreamProcessor) handleCommand_g(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	v := model.NewPdfColorspaceDeviceGray()
	objs := op.Params
	if len(objs) != v.GetNumComponents() {
		common.Log.Debug("Invalid number of parameters for g")
		common.Log.Debug("Number %d not matching colorspace %T", len(objs), v)
		if !proc.relaxedMode {
			return errInvalidNumberOfParams
		}
		obj, err := autoConvertRGBToGray(objs)
		if err != nil {
			common.Log.Debug("ERROR: Fail to convert color to gray: %+v", objs)
			return err
		}
		objs = []core.PdfObject{obj}
	}
	col, err := v.ColorFromPdfObjects(objs)
	if err != nil {
		common.Log.Debug("ERROR: handleCommand_g Invalid params. cs=%T op=%s err=%v", v, op, err)
		return err
	}
	proc.graphicsState.ColorspaceNonStroking = v
	proc.graphicsState.ColorNonStroking = col
	return nil
}
func (proc *ContentStreamProcessor) handleCommand_scn(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	cs := proc.graphicsState.ColorspaceNonStroking
	objs := op.Params
	if !isPatternColorspace(cs) {
		if len(objs) != cs.GetNumComponents() {
			common.Log.Debug("Invalid number of parameters for SC")
			common.Log.Debug("Number %d not matching colorspace %T", len(objs), cs)
			if !proc.relaxedMode {
				return errInvalidNumberOfParams
			}
			obj, err := autoConvertRGBToGray(objs)
			if err != nil {
				common.Log.Debug("ERROR: Fail to convert color to gray: %+v", objs)
				return err
			}
			objs = []core.PdfObject{obj}
		}
	}
	col, err := cs.ColorFromPdfObjects(objs)
	if err != nil {
		common.Log.Debug("ERROR: Fail to get color from params: %+v (CS is %+v)", objs, cs)
		return err
	}
	proc.graphicsState.ColorNonStroking = col
	return nil
}

// Add_CS appends 'CS' operand to the content stream:
// Set the current colorspace for stroking operations.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_CS(name core.PdfObjectName) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "CS"
	op.Params = namesToObjects([]core.PdfObjectName{name})
	cc.operations = append(cc.operations, &op)
	return cc
}
func (p *ContentStreamParser) parseNull() (core.PdfObjectNull, error) {
	_, err := p.reader.Discard(4)
	return core.PdfObjectNull{}, err
}

// Add_quotes appends `"` operand to the content stream:
// Move to next line and show a string, using `aw` and `ac` as word
// and character spacing respectively.
//
// See section 9.4.3 "Text Showing Operators" and
// Table 209 (pp. 258-259 PDF32000_2008).
func (cc *ContentCreator) Add_quotes(textstr core.PdfObjectString, aw, ac float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "\""
	op.Params = floatsToObjects([]float64{aw, ac})
	op.Params = append(op.Params, stringsToObjects([]core.PdfObjectString{textstr})...)
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_TJ appends 'TJ' operand to the content stream:
// Show one or more text string. Array of numbers (displacement) and strings.
//
// See section 9.4.3 "Text Showing Operators" and
// Table 209 (pp. 258-259 PDF32000_2008).
func (cc *ContentCreator) Add_TJ(vals ...core.PdfObject) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "TJ"
	op.Params = []core.PdfObject{core.MakeArray(vals...)}
	cc.operations = append(cc.operations, &op)
	return cc
}

// HasUnclosedQ checks if all the `q` operator is properly closed by `Q` operator.
func (ops *ContentStreamOperations) HasUnclosedQ() bool {
	n := 0
	for _, op := range *ops {
		switch op.Operand {
		case "q":
			n++
		case "Q":
			n--
		}
	}
	return n != 0
}

// Add_SC appends 'SC' operand to the content stream:
// Set color for stroking operations.  Input: c1, ..., cn.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_SC(c ...float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "SC"
	op.Params = floatsToObjects(c)
	cc.operations = append(cc.operations, &op)
	return cc
}

// ContentStreamOperations is a slice of ContentStreamOperations.
type ContentStreamOperations []*ContentStreamOperation

// Add_w adds 'w' operand to the content stream, which sets the line width.
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_w(lineWidth float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "w"
	op.Params = floatsToObjects([]float64{lineWidth})
	cc.operations = append(cc.operations, &op)
	return cc
}
func (proc *ContentStreamProcessor) handleCommand_cs(op *ContentStreamOperation, resources *model.PdfPageResources) error {
	if len(op.Params) < 1 {
		common.Log.Debug("Invalid CS command, skipping over")
		return errors.New("too few parameters")
	}
	if len(op.Params) > 1 {
		common.Log.Debug("CS command with too many parameters - continuing")
		return errors.New("too many parameters")
	}
	name, ok := op.Params[0].(*core.PdfObjectName)
	if !ok {
		common.Log.Debug("ERROR: CS command with invalid parameter, skipping over")
		return errors.New("type check error")
	}
	cs, err := proc.getColorspace(string(*name), resources)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorspaceNonStroking = cs
	col, err := proc.getInitialColor(cs)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorNonStroking = col
	return nil
}

// Add_cs appends 'cs' operand to the content stream:
// Same as CS but for non-stroking operations.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_cs(name core.PdfObjectName) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "cs"
	op.Params = namesToObjects([]core.PdfObjectName{name})
	cc.operations = append(cc.operations, &op)
	return cc
}

// ContentStreamOperation represents an operation in PDF contentstream which consists of
// an operand and parameters.
type ContentStreamOperation struct {
	Params  []core.PdfObject
	Operand string
}

func (proc *ContentStreamProcessor) handleCommand_K(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	v := model.NewPdfColorspaceDeviceCMYK()
	if len(op.Params) != v.GetNumComponents() {
		common.Log.Debug("Invalid number of parameters for SC")
		common.Log.Debug("Number %d not matching colorspace %T", len(op.Params), v)
		return errInvalidNumberOfParams
	}
	col, err := v.ColorFromPdfObjects(op.Params)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorspaceStroking = v
	proc.graphicsState.ColorStroking = col
	return nil
}

// ContentStreamInlineImage is a representation of an inline image in a Content stream. Everything between the BI and EI operands.
// ContentStreamInlineImage implements the core.PdfObject interface although strictly it is not a PDF object.
type ContentStreamInlineImage struct {
	colorspace       model.PdfColorspace
	BitsPerComponent core.PdfObject
	ColorSpace       core.PdfObject
	Decode           core.PdfObject
	DecodeParms      core.PdfObject
	Filter           core.PdfObject
	Height           core.PdfObject
	ImageMask        core.PdfObject
	Intent           core.PdfObject
	Interpolate      core.PdfObject
	Width            core.PdfObject
	data             []byte
	imageBase        *imageutil.ImageBase
}

// Bytes converts a set of content stream operations to a content stream byte presentation,
// i.e. the kind that can be stored as a PDF stream or string format.
func (ops *ContentStreamOperations) Bytes() []byte {
	var buf bytes.Buffer
	for _, op := range *ops {
		if op == nil {
			continue
		}
		if op.Operand == "BI" {
			buf.WriteString(op.Operand + "\n")
			buf.Write(op.Params[0].Write())
		} else {
			for _, obj := range op.Params {
				buf.Write(obj.Write())
				buf.WriteString(" ")
			}
			buf.WriteString(op.Operand + "\n")
		}
	}
	return buf.Bytes()
}

// Equals reports whether other is an inline image with the same serialized contents.
func (img *ContentStreamInlineImage) Equals(other core.PdfObject) bool {
	img2, ok := other.(*ContentStreamInlineImage)
	if !ok {
		return false
	}
	if img == nil || img2 == nil {
		return img == nil && img2 == nil
	}
	return bytes.Equal(img.Write(), img2.Write())
}

// Add_B appends 'B' operand to the content stream:
// Fill and then stroke the path (nonzero winding number rule).
//
// See section 8.5.3 "Path Painting Operators" and Table 60 (p. 143 PDF32000_2008).
func (cc *ContentCreator) Add_B() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "B"
	cc.operations = append(cc.operations, &op)
	return cc
}
func (p *ContentStreamParser) parseArray() (*core.PdfObjectArray, error) {
	arr := core.MakeArray()
	p.reader.ReadByte()
	for {
		p.skipSpaces()
		buf, err := p.reader.Peek(1)
		if err != nil {
			return arr, err
		}
		if buf[0] == ']' {
			p.reader.ReadByte()
			break
		}
		obj, _, err := p.parseObject()
		if err != nil {
			return arr, err
		}
		arr.Append(obj)
	}
	return arr, nil
}
func (p *ContentStreamParser) parseHexString() (*core.PdfObjectString, error) {
	p.reader.ReadByte()
	buf := []byte("0123456789abcdefABCDEF")
	var buf2 []byte
	for {
		p.skipSpaces()
		buf3, err := p.reader.Peek(1)
		if err != nil {
			return core.MakeString(""), err
		}
		if buf3[0] == '>' {
			p.reader.ReadByte()
			break
		}
		b, _ := p.reader.ReadByte()
		if bytes.IndexByte(buf, b) >= 0 {
			buf2 = append(buf2, b)
		}
	}
	if len(buf2)%2 == 1 {
		buf2 = append(buf2, '0')
	}
	buf3, _ := hex.DecodeString(string(buf2))
	return core.MakeHexString(string(buf3)), nil
}

// Add_gs adds 'gs' operand to the content stream: Set the graphics state.
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_gs(dictName core.PdfObjectName) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "gs"
	op.Params = namesToObjects([]core.PdfObjectName{dictName})
	cc.operations = append(cc.operations, &op)
	return cc
}

// SetRelaxedMode sets the relaxed mode for the content stream processor.
func (proc *ContentStreamProcessor) SetRelaxedMode(val bool) { proc.relaxedMode = val }

// Add_Tj appends 'Tj' operand to the content stream:
// Show a text string.
//
// See section 9.4.3 "Text Showing Operators" and
// Table 209 (pp. 258-259 PDF32000_2008).
func (cc *ContentCreator) Add_Tj(textstr core.PdfObjectString) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Tj"
	op.Params = stringsToObjects([]core.PdfObjectString{textstr})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_SCN appends 'SCN' operand to the content stream:
// Same as SC but supports more colorspaces.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_SCN(c ...float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "SCN"
	op.Params = floatsToObjects(c)
	cc.operations = append(cc.operations, &op)
	return cc
}

// RotateDeg applies a rotation to the transformation matrix.
func (cc *ContentCreator) RotateDeg(angle float64) *ContentCreator {
	f := math.Cos(angle * math.Pi / 180.0)
	f2 := math.Sin(angle * math.Pi / 180.0)
	f3 := -math.Sin(angle * math.Pi / 180.0)
	f4 := math.Cos(angle * math.Pi / 180.0)
	return cc.Add_cm(f, f2, f3, f4, 0, 0)
}

// ContentCreator is a builder for PDF content streams.
type ContentCreator struct{ operations ContentStreamOperations }

// Add_v appends 'v' operand to the content stream: Append a Bezier curve to the current path from the
// current point to (x3,y3) with the current point and (x2,y2) as control points.
//
// See section 8.5.2 "Path Construction Operators" and Table 59 (pp. 140-141 PDF32000_2008).
func (cc *ContentCreator) Add_v(x2, y2, x3, y3 float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "v"
	op.Params = floatsToObjects([]float64{x2, y2, x3, y3})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_S appends 'S' operand to the content stream: Stroke the path.
//
// See section 8.5.3 "Path Painting Operators" and Table 60 (p. 143 PDF32000_2008).
func (cc *ContentCreator) Add_S() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "S"
	cc.operations = append(cc.operations, &op)
	return cc
}
func newEncoderFromInlineImage(img *ContentStreamInlineImage) (core.StreamEncoder, error) {
	if img.Filter == nil {
		return core.NewRawEncoder(), nil
	}
	name, ok := img.Filter.(*core.PdfObjectName)
	if !ok {
		arr, ok2 := img.Filter.(*core.PdfObjectArray)
		if !ok2 {
			return nil, fmt.Errorf("filter not a Name or Array object")
		}
		if arr.Len() == 0 {
			return core.NewRawEncoder(), nil
		}
		if arr.Len() != 1 {
			v, err := newMultiEncoderFromInlineImage(img)
			if err != nil {
				common.Log.Error("Failed creating multi encoder: %v", err)
				return nil, err
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Multi enc: %s\n", v)
			}
			return v, nil
		}
		obj := arr.Get(0)
		name, ok2 = obj.(*core.PdfObjectName)
		if !ok2 {
			return nil, fmt.Errorf("filter array member not a Name object")
		}
	}
	switch *name {
	case "AHx", "ASCIIHexDecode":
		return core.NewASCIIHexEncoder(), nil
	case "A85", "ASCII85Decode":
		return core.NewASCII85Encoder(), nil
	case "DCT", "DCTDecode":
		return newDCTEncoderFromInlineImage(img)
	case "Fl", "FlateDecode":
		return newFlateEncoderFromInlineImage(img, nil)
	case "LZW", "LZWDecode":
		return newLZWEncoderFromInlineImage(img, nil)
	case "CCF", "CCITTFaxDecode":
		return core.NewCCITTFaxEncoder(), nil
	case "RL", "RunLengthDecode":
		return core.NewRunLengthEncoder(), nil
	default:
		common.Log.Debug("Unsupported inline image encoding filter name : %s", *name)
		return nil, errors.New("unsupported inline encoding method")
	}
}

// HandlerFunc is the function syntax that the ContentStreamProcessor handler must implement.
type HandlerFunc func(op *ContentStreamOperation, gs GraphicsState, resources *model.PdfPageResources) error

// Add_G appends 'G' operand to the content stream:
// Set the stroking colorspace to DeviceGray and sets the gray level (0-1).
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_G(gray float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "G"
	op.Params = floatsToObjects([]float64{gray})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_q adds 'q' operand to the content stream: Pushes the current graphics state on the stack.
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_q() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "q"
	cc.operations = append(cc.operations, &op)
	return cc
}

// AddHandler adds a new ContentStreamProcessor `handler` of type `condition` for `operand`.
func (proc *ContentStreamProcessor) AddHandler(condition HandlerConditionEnum, operand string, handler HandlerFunc) {
	v := handlerEntry{}
	v.Condition = condition
	v.Operand = operand
	v.Handler = handler
	proc.handlers = append(proc.handlers, v)
}

// Add_scn appends 'scn' operand to the content stream:
// Same as SC but for nonstroking operations.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_scn(c ...float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "scn"
	op.Params = floatsToObjects(c)
	cc.operations = append(cc.operations, &op)
	return cc
}

// Write outputs the object as a byte array.
func (img *ContentStreamInlineImage) Write() []byte {
	var buf bytes.Buffer
	if img.BitsPerComponent != nil {
		buf.WriteString("/BPC ")
		buf.Write(img.BitsPerComponent.Write())
		buf.WriteByte('\n')
	}
	if img.ColorSpace != nil {
		buf.WriteString("/CS ")
		buf.Write(img.ColorSpace.Write())
		buf.WriteByte('\n')
	}
	if img.Decode != nil {
		buf.WriteString("/D ")
		buf.Write(img.Decode.Write())
		buf.WriteByte('\n')
	}
	if img.DecodeParms != nil {
		buf.WriteString("/DP ")
		buf.Write(img.DecodeParms.Write())
		buf.WriteByte('\n')
	}
	if img.Filter != nil {
		buf.WriteString("/F ")
		buf.Write(img.Filter.Write())
		buf.WriteByte('\n')
	}
	if img.Height != nil {
		buf.WriteString("/H ")
		buf.Write(img.Height.Write())
		buf.WriteByte('\n')
	}
	if img.ImageMask != nil {
		buf.WriteString("/IM ")
		buf.Write(img.ImageMask.Write())
		buf.WriteByte('\n')
	}
	if img.Intent != nil {
		buf.WriteString("/Intent ")
		buf.Write(img.Intent.Write())
		buf.WriteByte('\n')
	}
	if img.Interpolate != nil {
		buf.WriteString("/I ")
		buf.Write(img.Interpolate.Write())
		buf.WriteByte('\n')
	}
	if img.Width != nil {
		buf.WriteString("/W ")
		buf.Write(img.Width.Write())
		buf.WriteByte('\n')
	}
	buf.WriteString("ID ")
	buf.Write(img.data)
	buf.WriteString("\nEI\n")
	return buf.Bytes()
}

// Add_l adds 'l' operand to the content stream:
// Append a straight line segment from the current point to (x,y).
//
// See section 8.5.2 "Path Construction Operators" and Table 59 (pp. 140-141 PDF32000_2008).
func (cc *ContentCreator) Add_l(x, y float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "l"
	op.Params = floatsToObjects([]float64{x, y})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_ri adds 'ri' operand to the content stream, which sets the color rendering intent.
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_ri(intent core.PdfObjectName) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "ri"
	op.Params = namesToObjects([]core.PdfObjectName{intent})
	cc.operations = append(cc.operations, &op)
	return cc
}
func (p *ContentStreamParser) skipComments() error {
	if _, err := p.skipSpaces(); err != nil {
		return err
	}
	ok := true
	for {
		buf, err := p.reader.Peek(1)
		if err != nil {
			common.Log.Debug("Error %s", err.Error())
			return err
		}
		if ok && buf[0] != '%' {
			return nil
		}
		ok = false
		if (buf[0] != '\r') && (buf[0] != '\n') {
			p.reader.ReadByte()
		} else {
			break
		}
	}
	return p.skipComments()
}

// Process processes the entire list of operations. Maintains the graphics state that is passed to any
// handlers that are triggered during processing (either on specific operators or all).
func (proc *ContentStreamProcessor) Process(resources *model.PdfPageResources) error {
	proc.graphicsState.ColorspaceStroking = model.NewPdfColorspaceDeviceGray()
	proc.graphicsState.ColorspaceNonStroking = model.NewPdfColorspaceDeviceGray()
	proc.graphicsState.ColorStroking = model.NewPdfColorDeviceGray(0)
	proc.graphicsState.ColorNonStroking = model.NewPdfColorDeviceGray(0)
	proc.graphicsState.CTM = transform.IdentityMatrix()
	if init := proc.initialGraphicsState; init != nil {
		if init.ColorspaceStroking != nil {
			proc.graphicsState.ColorspaceStroking = init.ColorspaceStroking
			if init.ColorStroking != nil {
				proc.graphicsState.ColorStroking = init.ColorStroking
			} else {
				col, err := proc.getInitialColor(init.ColorspaceStroking)
				if err != nil {
					return err
				}
				proc.graphicsState.ColorStroking = col
			}
		}
		if init.ColorspaceNonStroking != nil {
			proc.graphicsState.ColorspaceNonStroking = init.ColorspaceNonStroking
			if init.ColorNonStroking != nil {
				proc.graphicsState.ColorNonStroking = init.ColorNonStroking
			} else {
				col, err := proc.getInitialColor(init.ColorspaceNonStroking)
				if err != nil {
					return err
				}
				proc.graphicsState.ColorNonStroking = col
			}
		}
		if init.CTM != (transform.Matrix{}) {
			proc.graphicsState.CTM = init.CTM
		}
	}
	for _, op := range proc.operations {
		var err error
		switch op.Operand {
		case "q":
			proc.gsStack.Push(proc.graphicsState)
		case "Q":
			if len(proc.gsStack) == 0 {
				common.Log.Debug("WARN: invalid `Q` operator. Graphics state stack is empty. Skipping.")
				continue
			}
			proc.graphicsState = proc.gsStack.Pop()
		case "CS":
			err = proc.handleCommand_CS(op, resources)
		case "cs":
			err = proc.handleCommand_cs(op, resources)
		case "SC":
			err = proc.handleCommand_SC(op, resources)
		case "SCN":
			err = proc.handleCommand_SCN(op, resources)
		case "sc":
			err = proc.handleCommand_sc(op, resources)
		case "scn":
			err = proc.handleCommand_scn(op, resources)
		case "G":
			err = proc.handleCommand_G(op, resources)
		case "g":
			err = proc.handleCommand_g(op, resources)
		case "RG":
			err = proc.handleCommand_RG(op, resources)
		case "rg":
			err = proc.handleCommand_rg(op, resources)
		case "K":
			err = proc.handleCommand_K(op, resources)
		case "k":
			err = proc.handleCommand_k(op, resources)
		case "cm":
			err = proc.handleCommand_cm(op, resources)
		}
		if err != nil {
			common.Log.Debug("Processor handling error (%s): %v", op.Operand, err)
			common.Log.Debug("Operand: %#v", op.Operand)
			return err
		}
		for _, v := range proc.handlers {
			var err2 error
			if v.Condition.All() {
				err2 = v.Handler(op, proc.graphicsState, resources)
			} else if v.Condition.Operand() && op.Operand == v.Operand {
				err2 = v.Handler(op, proc.graphicsState, resources)
			}
			if err2 != nil {
				common.Log.Debug("Processor handler error: %v", err2)
				return err2
			}
		}
	}
	return nil
}

// Operand returns true if `hce` is equivalent to HandlerConditionEnumOperand.
func (v HandlerConditionEnum) Operand() bool { return v == HandlerConditionEnumOperand }
func (p *ContentStreamParser) skipSpaces() (int, error) {
	n := 0
	for {
		buf, err := p.reader.Peek(1)
		if err != nil {
			return 0, err
		}
		if core.IsWhiteSpace(buf[0]) {
			p.reader.ReadByte()
			n++
		} else {
			break
		}
	}
	return n, nil
}

// Add_Td appends 'Td' operand to the content stream:
// Move to start of next line with offset (`tx`, `ty`).
//
// See section 9.4.2 "Text Positioning Operators" and
// Table 108 (pp. 257-258 PDF32000_2008).
func (cc *ContentCreator) Add_Td(tx, ty float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Td"
	op.Params = floatsToObjects([]float64{tx, ty})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_BDC appends 'BDC' operand to the content stream:
// Begins a marked-content sequence with an associated property list terminated by a balancing EMC operator.
// `tag` shall be a name object indicating the role or significance of
// the sequence.
// `propertyList` shall be a dictionary containing the properties of the
//
// See section 14.6 "Marked Content" and Table 320 (p. 561 PDF32000_2008).
func (cc *ContentCreator) Add_BDC(tag core.PdfObjectName, propertyList map[string]core.PdfObject) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "BDC"
	op.Params = namesToObjects([]core.PdfObjectName{tag})
	if len(propertyList) > 0 {
		op.Params = append(op.Params, core.MakeDictMap(propertyList))
	}
	cc.operations = append(cc.operations, &op)
	return cc
}

// WrapIfNeeded wraps the entire contents within q ... Q.  If unbalanced, then adds extra Qs at the end.
// Only does if needed. Ensures that when adding new content, one start with all states
// in the default condition.
func (ops *ContentStreamOperations) WrapIfNeeded() *ContentStreamOperations {
	if len(*ops) == 0 {
		return ops
	}
	if ops.isWrapped() {
		return ops
	}
	*ops = append([]*ContentStreamOperation{{Operand: "q"}}, *ops...)
	n := 0
	for _, op := range *ops {
		switch op.Operand {
		case "q":
			n++
		case "Q":
			n--
		}
	}
	for n > 0 {
		*ops = append(*ops, &ContentStreamOperation{Operand: "Q"})
		n--
	}
	return ops
}

// Add_Tc appends 'Tc' operand to the content stream:
// Set character spacing.
//
// See section 9.3 "Text State Parameters and Operators" and
// Table 105 (pp. 251-252 PDF32000_2008).
func (cc *ContentCreator) Add_Tc(charSpace float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Tc"
	op.Params = floatsToObjects([]float64{charSpace})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_BMC appends 'BMC' operand to the content stream:
// Begins a marked-content sequence terminated by a balancing EMC operator.
// `tag` shall be a name object indicating the role or significance of
// the sequence.
//
// See section 14.6 "Marked Content" and Table 320 (p. 561 PDF32000_2008).
func (cc *ContentCreator) Add_BMC(tag core.PdfObjectName) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "BMC"
	op.Params = namesToObjects([]core.PdfObjectName{tag})
	cc.operations = append(cc.operations, &op)
	return cc
}
func isPatternColorspace(cs model.PdfColorspace) bool {
	_, ok := cs.(*model.PdfColorspaceSpecialPattern)
	return ok
}
func (proc *ContentStreamProcessor) handleCommand_rg(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	v := model.NewPdfColorspaceDeviceRGB()
	objs := op.Params
	if len(objs) != v.GetNumComponents() {
		common.Log.Debug("Invalid number of parameters for SC")
		common.Log.Debug("Number %d not matching colorspace %T", len(objs), v)
		if !proc.relaxedMode {
			return errInvalidNumberOfParams
		}
		obj, err := autoConvertRGBToGray(objs)
		if err != nil {
			common.Log.Debug("ERROR: Fail to convert color to gray: %+v", objs)
			return err
		}
		objs = []core.PdfObject{obj}
	}
	col, err := v.ColorFromPdfObjects(objs)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorspaceNonStroking = v
	proc.graphicsState.ColorNonStroking = col
	return nil
}

// Add_RG appends 'RG' operand to the content stream:
// Set the stroking colorspace to DeviceRGB and sets the r,g,b colors (0-1 each).
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_RG(r, g, b float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "RG"
	op.Params = floatsToObjects([]float64{r, g, b})
	cc.operations = append(cc.operations, &op)
	return cc
}
func integersToObjects(v []int64) []core.PdfObject {
	var objs []core.PdfObject
	for _, n := range v {
		objs = append(objs, core.MakeInteger(n))
	}
	return objs
}
func (p *ContentStreamParser) parseDict() (*core.PdfObjectDictionary, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Reading content stream dict!")
	}
	dict := core.MakeDict()
	b, _ := p.reader.ReadByte()
	if b != '<' {
		return nil, errors.New("invalid dict")
	}
	b, _ = p.reader.ReadByte()
	if b != '<' {
		return nil, errors.New("invalid dict")
	}
	for {
		p.skipSpaces()
		buf, err := p.reader.Peek(2)
		if err != nil {
			return nil, err
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Dict peek: %s (% x)!", string(buf), string(buf))
		}
		if (buf[0] == '>') && (buf[1] == '>') {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("EOF dictionary")
			}
			p.reader.ReadByte()
			p.reader.ReadByte()
			break
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Parse the name!")
		}
		name, err := p.parseName()
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Key: %s", name)
		}
		if err != nil {
			common.Log.Debug("ERROR Returning name err %s", err)
			return nil, err
		}
		if len(name) > 4 && name[len(name)-4:] == "null" {
			name2 := name[0 : len(name)-4]
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Taking care of null bug (%s)", name)
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("New key \"%s\" = null", name2)
			}
			p.skipSpaces()
			buf2, _ := p.reader.Peek(1)
			if buf2[0] == '/' {
				dict.Set(name2, core.MakeNull())
				continue
			}
		}
		p.skipSpaces()
		obj, _, err := p.parseObject()
		if err != nil {
			return nil, err
		}
		dict.Set(name, obj)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("dict[%s] = %s", name, obj.String())
		}
	}
	return dict, nil
}

// Add_Q adds 'Q' operand to the content stream: Pops the most recently stored state from the stack.
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_Q() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Q"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Operations returns the list of operations.
func (cc *ContentCreator) Operations() *ContentStreamOperations { return &cc.operations }

// HandlerConditionEnum represents the type of operand content stream processor (handler).
// The handler may process a single specific named operand or all operands.
type HandlerConditionEnum int

// GraphicsState is a basic graphics state implementation for PDF processing.
// Initially only implementing and tracking a portion of the information specified. Easy to add more.
type GraphicsState struct {
	ColorspaceStroking    model.PdfColorspace
	ColorspaceNonStroking model.PdfColorspace
	ColorStroking         model.PdfColor
	ColorNonStroking      model.PdfColor
	CTM                   transform.Matrix
}

var errInvalidNumberOfParams = errors.New("invalid number of parameters")

// Add_g appends 'g' operand to the content stream:
// Same as G but used for nonstroking operations.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_g(gray float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "g"
	op.Params = floatsToObjects([]float64{gray})
	cc.operations = append(cc.operations, &op)
	return cc
}
func (p *ContentStreamParser) parseName() (core.PdfObjectName, error) {
	s := ""
	ok := false
	for {
		buf, err := p.reader.Peek(1)
		if err == io.EOF {
			break
		}
		if err != nil {
			return core.PdfObjectName(s), err
		}
		if !ok {
			if buf[0] == '/' {
				ok = true
				p.reader.ReadByte()
			} else {
				common.Log.Error("Name starting with %s (% x)", buf, buf)
				return core.PdfObjectName(s), fmt.Errorf("invalid name: (%c)", buf[0])
			}
		} else {
			if core.IsWhiteSpace(buf[0]) {
				break
			} else if (buf[0] == '/') || (buf[0] == '[') || (buf[0] == '(') || (buf[0] == ']') || (buf[0] == '<') || (buf[0] == '>') {
				break
			} else if buf[0] == '#' {
				buf2, err2 := p.reader.Peek(3)
				if err2 != nil {
					return core.PdfObjectName(s), err2
				}
				p.reader.Discard(3)
				buf3, err2 := hex.DecodeString(string(buf2[1:3]))
				if err2 != nil {
					return core.PdfObjectName(s), err2
				}
				s += string(buf3)
			} else {
				b, _ := p.reader.ReadByte()
				s += string(b)
			}
		}
	}
	return core.PdfObjectName(s), nil
}

// Wrap ensures that the contentstream is wrapped within a balanced q ... Q expression.
func (cc *ContentCreator) Wrap() { cc.operations.WrapIfNeeded() }

// Transform returns coordinates x, y transformed by the CTM.
func (gs *GraphicsState) Transform(x, y float64) (float64, float64) {
	return gs.CTM.Transform(x, y)
}
func (proc *ContentStreamProcessor) handleCommand_SC(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	cs := proc.graphicsState.ColorspaceStroking
	objs := op.Params
	if len(objs) != cs.GetNumComponents() {
		common.Log.Debug("Invalid number of parameters for SC")
		common.Log.Debug("Number %d not matching colorspace %T", len(objs), cs)
		if !proc.relaxedMode {
			return errInvalidNumberOfParams
		}
		obj, err := autoConvertRGBToGray(objs)
		if err != nil {
			common.Log.Debug("ERROR: Fail to convert color to gray: %+v", objs)
			return err
		}
		objs = []core.PdfObject{obj}
	}
	col, err := cs.ColorFromPdfObjects(objs)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorStroking = col
	return nil
}

// Add_W_starred appends 'W*' operand to the content stream:
// Modify the current clipping path by intersecting with the current path (even odd rule).
//
// See section 8.5.4 "Clipping Path Operators" and Table 61 (p. 146 PDF32000_2008).
func (cc *ContentCreator) Add_W_starred() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "W*"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Push pushes `gs` on the `gsStack`.
func (stack *GraphicStateStack) Push(gs GraphicsState) { *stack = append(*stack, gs) }

// Add_BT appends 'BT' operand to the content stream:
// Begin text.
//
// See section 9.4 "Text Objects" and Table 107 (p. 256 PDF32000_2008).
func (cc *ContentCreator) Add_BT() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "BT"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_n appends 'n' operand to the content stream:
// End the path without filling or stroking.
//
// See section 8.5.3 "Path Painting Operators" and Table 60 (p. 143 PDF32000_2008).
func (cc *ContentCreator) Add_n() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "n"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_i adds 'i' operand to the content stream: Set the flatness tolerance in the graphics state.
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_i(flatness float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "i"
	op.Params = floatsToObjects([]float64{flatness})
	cc.operations = append(cc.operations, &op)
	return cc
}
func (img *ContentStreamInlineImage) String() string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("InlineImage(len=%d)\n", len(img.data)))
	if img.BitsPerComponent != nil {
		buf.WriteString("/BPC ")
		buf.Write(img.BitsPerComponent.Write())
		buf.WriteByte('\n')
	}
	if img.ColorSpace != nil {
		buf.WriteString("/CS ")
		buf.Write(img.ColorSpace.Write())
		buf.WriteByte('\n')
	}
	if img.Decode != nil {
		buf.WriteString("/D ")
		buf.Write(img.Decode.Write())
		buf.WriteByte('\n')
	}
	if img.DecodeParms != nil {
		buf.WriteString("/DP ")
		buf.Write(img.DecodeParms.Write())
		buf.WriteByte('\n')
	}
	if img.Filter != nil {
		buf.WriteString("/F ")
		buf.Write(img.Filter.Write())
		buf.WriteByte('\n')
	}
	if img.Height != nil {
		buf.WriteString("/H ")
		buf.Write(img.Height.Write())
		buf.WriteByte('\n')
	}
	if img.ImageMask != nil {
		buf.WriteString("/IM ")
		buf.Write(img.ImageMask.Write())
		buf.WriteByte('\n')
	}
	if img.Intent != nil {
		buf.WriteString("/Intent ")
		buf.Write(img.Intent.Write())
		buf.WriteByte('\n')
	}
	if img.Interpolate != nil {
		buf.WriteString("/I ")
		buf.Write(img.Interpolate.Write())
		buf.WriteByte('\n')
	}
	if img.Width != nil {
		buf.WriteString("/W ")
		buf.Write(img.Width.Write())
		buf.WriteByte('\n')
	}
	return buf.String()
}

// SetNonStrokingColor sets the non-stroking `color` where color can be one of
// PdfColorDeviceGray, PdfColorDeviceRGB, or PdfColorDeviceCMYK.
func (cc *ContentCreator) SetNonStrokingColor(color model.PdfColor) *ContentCreator {
	switch c := color.(type) {
	case *model.PdfColorDeviceGray:
		cc.Add_g(c.Val())
	case *model.PdfColorDeviceRGB:
		cc.Add_rg(c.R(), c.G(), c.B())
	case *model.PdfColorDeviceCMYK:
		cc.Add_k(c.C(), c.M(), c.Y(), c.K())
	case *model.PdfColorPatternType1:
		cc.Add_cs(*core.MakeName("Pattern"))
		cc.Add_scn_pattern(c.PatternName)
	case *model.PdfColorPatternType2:
		cc.Add_cs(*core.MakeName("Pattern"))
		cc.Add_scn_pattern(c.PatternName)
	case *model.PdfColorPatternType3:
		cc.Add_cs(*core.MakeName("Pattern"))
		cc.Add_scn_pattern(c.PatternName)
	default:
		common.Log.Debug("SetNonStrokingColor: unsupported color: %T", c)
	}
	return cc
}

// Add_SCN_pattern appends 'SCN' operand to the content stream for pattern `name`:
// SCN with name attribute (for pattern). Syntax: c1 ... cn name SCN.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_SCN_pattern(name core.PdfObjectName, c ...float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "SCN"
	op.Params = floatsToObjects(c)
	op.Params = append(op.Params, core.MakeName(string(name)))
	cc.operations = append(cc.operations, &op)
	return cc
}

// Pop pops and returns the topmost GraphicsState off the `gsStack`.
func (stack *GraphicStateStack) Pop() GraphicsState {
	gs := (*stack)[len(*stack)-1]
	*stack = (*stack)[:len(*stack)-1]
	return gs
}

// ContentStreamParser represents a content stream parser for parsing content streams in PDFs.
type ContentStreamParser struct{ reader *bufio.Reader }

func floatsToObjects(floats []float64) []core.PdfObject {
	var objs []core.PdfObject
	for _, f := range floats {
		objs = append(objs, core.MakeFloat(f))
	}
	return objs
}
func (proc *ContentStreamProcessor) handleCommand_CS(op *ContentStreamOperation, resources *model.PdfPageResources) error {
	if len(op.Params) < 1 {
		common.Log.Debug("Invalid cs command, skipping over")
		return errors.New("too few parameters")
	}
	if len(op.Params) > 1 {
		common.Log.Debug("cs command with too many parameters - continuing")
		return errors.New("too many parameters")
	}
	name, ok := op.Params[0].(*core.PdfObjectName)
	if !ok {
		common.Log.Debug("ERROR: cs command with invalid parameter, skipping over")
		return errors.New("type check error")
	}
	cs, err := proc.getColorspace(string(*name), resources)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorspaceStroking = cs
	col, err := proc.getInitialColor(cs)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorStroking = col
	return nil
}

// Add_f_starred appends 'f*' operand to the content stream.
// f*: Fill the path using the even-odd rule to determine fill region.
//
// See section 8.5.3 "Path Painting Operators" and Table 60 (p. 143 PDF32000_2008).
func (cc *ContentCreator) Add_f_starred() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "f*"
	cc.operations = append(cc.operations, &op)
	return cc
}
func (proc *ContentStreamProcessor) handleCommand_G(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	v := model.NewPdfColorspaceDeviceGray()
	objs := op.Params
	if len(objs) != v.GetNumComponents() {
		common.Log.Debug("Invalid number of parameters for SC")
		common.Log.Debug("Number %d not matching colorspace %T", len(objs), v)
		if !proc.relaxedMode {
			return errInvalidNumberOfParams
		}
		obj, err := autoConvertRGBToGray(objs)
		if err != nil {
			common.Log.Debug("ERROR: Fail to convert color to gray: %+v", objs)
			return err
		}
		objs = []core.PdfObject{obj}
	}
	col, err := v.ColorFromPdfObjects(objs)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorspaceStroking = v
	proc.graphicsState.ColorStroking = col
	return nil
}
func newIndexedColorspaceFromArray(obj core.PdfObject) (model.PdfColorspace, error) {
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		common.Log.Debug("Error: Invalid indexed cs not in array (%#v)", obj)
		return nil, errors.New("type check error")
	}
	if arr.Len() != 4 {
		common.Log.Debug("Error: Invalid cs array, length != 4 (%d)", arr.Len())
		return nil, errors.New("range check error")
	}
	name, ok := arr.Get(0).(*core.PdfObjectName)
	if !ok {
		common.Log.Debug("Error: Invalid cs array first element not a name (array: %#v)", *arr)
		return nil, errors.New("type check error")
	}
	if *name != "I" && *name != "Indexed" {
		common.Log.Debug("Error: Invalid cs array first element != I (got: %v)", *name)
		return nil, errors.New("range check error")
	}
	name, ok = arr.Get(1).(*core.PdfObjectName)
	if !ok {
		common.Log.Debug("Error: Invalid cs array 2nd element not a name (array: %#v)", *arr)
		return nil, errors.New("type check error")
	}
	if *name != "G" && *name != "RGB" && *name != "CMYK" && *name != "DeviceGray" && *name != "DeviceRGB" && *name != "DeviceCMYK" {
		common.Log.Debug("Error: Invalid cs array 2nd element != G/RGB/CMYK (got: %v)", *name)
		return nil, errors.New("range check error")
	}
	s := ""
	switch *name {
	case "G", "DeviceGray":
		s = "DeviceGray"
	case "RGB", "DeviceRGB":
		s = "DeviceRGB"
	case "CMYK", "DeviceCMYK":
		s = "DeviceCMYK"
	}
	arr2 := core.MakeArray(core.MakeName("Indexed"), core.MakeName(s), arr.Get(2), arr.Get(3))
	return model.NewPdfColorspaceFromPdfObject(arr2)
}

// Parse parses all commands in content stream, returning a list of operation data.
func (p *ContentStreamParser) Parse() (*ContentStreamOperations, error) {
	v := ContentStreamOperations{}
	for {
		op := ContentStreamOperation{}
		for {
			obj, ok, err := p.parseObject()
			if err != nil {
				if err == io.EOF {
					return &v, nil
				}
				return &v, err
			}
			if ok {
				op.Operand, _ = core.GetStringVal(obj)
				v = append(v, &op)
				break
			} else {
				op.Params = append(op.Params, obj)
			}
		}
		if op.Operand == "BI" {
			img, err := p.ParseInlineImage()
			if err != nil {
				return &v, err
			}
			op.Params = append(op.Params, img)
		}
	}
}

// Add_Tf appends 'Tf' operand to the content stream:
// Set font and font size specified by font resource `fontName` and `fontSize`.
//
// See section 9.3 "Text State Parameters and Operators" and
// Table 105 (pp. 251-252 PDF32000_2008).
func (cc *ContentCreator) Add_Tf(fontName core.PdfObjectName, fontSize float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Tf"
	op.Params = namesToObjects([]core.PdfObjectName{fontName})
	op.Params = append(op.Params, floatsToObjects([]float64{fontSize})...)
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_Tstar appends 'T*' operand to the content stream:
// Move to the start of next line.
//
// See section 9.4.2 "Text Positioning Operators" and
// Table 108 (pp. 257-258 PDF32000_2008).
func (cc *ContentCreator) Add_Tstar() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "T*"
	cc.operations = append(cc.operations, &op)
	return cc
}

// String returns `ops.Bytes()` as a string.
func (ops *ContentStreamOperations) String() string { return string(ops.Bytes()) }

// GraphicStateStack represents a stack of GraphicsState.
type GraphicStateStack []GraphicsState

var (
	ErrInvalidOperand = errors.New("invalid operand")
	ErrEarlyExit      = errors.New("terminate process early exit")
)

func (proc *ContentStreamProcessor) getInitialColor(cs model.PdfColorspace) (model.PdfColor, error) {
	switch csp := cs.(type) {
	case *model.PdfColorspaceDeviceGray:
		return model.NewPdfColorDeviceGray(0.0), nil
	case *model.PdfColorspaceDeviceRGB:
		return model.NewPdfColorDeviceRGB(0.0, 0.0, 0.0), nil
	case *model.PdfColorspaceDeviceCMYK:
		return model.NewPdfColorDeviceCMYK(0.0, 0.0, 0.0, 1.0), nil
	case *model.PdfColorspaceCalGray:
		return model.NewPdfColorCalGray(0.0), nil
	case *model.PdfColorspaceCalRGB:
		return model.NewPdfColorCalRGB(0.0, 0.0, 0.0), nil
	case *model.PdfColorspaceLab:
		f := 0.0
		f2 := 0.0
		f3 := 0.0
		if csp.Range[0] > 0 {
			f = csp.Range[0]
		}
		if csp.Range[2] > 0 {
			f2 = csp.Range[2]
		}
		return model.NewPdfColorLab(f, f2, f3), nil
	case *model.PdfColorspaceICCBased:
		if csp.Alternate == nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("ICC Based not defined - attempting fall back (N = %d)", csp.N)
			}
			switch csp.N {
			case 1:
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Falling back to DeviceGray")
				}
				return proc.getInitialColor(model.NewPdfColorspaceDeviceGray())
			case 3:
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Falling back to DeviceRGB")
				}
				return proc.getInitialColor(model.NewPdfColorspaceDeviceRGB())
			case 4:
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Falling back to DeviceCMYK")
				}
				return proc.getInitialColor(model.NewPdfColorspaceDeviceCMYK())
			default:
				return nil, errors.New("alternate space not defined for ICC")
			}
		}
		return proc.getInitialColor(csp.Alternate)
	case *model.PdfColorspaceSpecialIndexed:
		if csp.Base == nil {
			return nil, errors.New("indexed base not specified")
		}
		return proc.getInitialColor(csp.Base)
	case *model.PdfColorspaceSpecialSeparation:
		if csp.AlternateSpace == nil {
			return nil, errors.New("alternate space not specified")
		}
		return proc.getInitialColor(csp.AlternateSpace)
	case *model.PdfColorspaceDeviceN:
		if csp.AlternateSpace == nil {
			return nil, errors.New("alternate space not specified")
		}
		return proc.getInitialColor(csp.AlternateSpace)
	case *model.PdfColorspaceSpecialPattern:
		return model.NewPdfColorPattern(), nil
	}
	common.Log.Debug("Unable to determine initial color for unknown colorspace: %T", cs)
	return nil, errors.New("unsupported colorspace")
}

// Add_TL appends 'TL' operand to the content stream:
// Set leading.
//
// See section 9.3 "Text State Parameters and Operators" and
// Table 105 (pp. 251-252 PDF32000_2008).
func (cc *ContentCreator) Add_TL(leading float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "TL"
	op.Params = floatsToObjects([]float64{leading})
	cc.operations = append(cc.operations, &op)
	return cc
}
func (proc *ContentStreamProcessor) handleCommand_k(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	v := model.NewPdfColorspaceDeviceCMYK()
	if len(op.Params) != v.GetNumComponents() {
		common.Log.Debug("Invalid number of parameters for SC")
		common.Log.Debug("Number %d not matching colorspace %T", len(op.Params), v)
		return errInvalidNumberOfParams
	}
	col, err := v.ColorFromPdfObjects(op.Params)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorspaceNonStroking = v
	proc.graphicsState.ColorNonStroking = col
	return nil
}
func (proc *ContentStreamProcessor) handleCommand_RG(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	v := model.NewPdfColorspaceDeviceRGB()
	objs := op.Params
	if len(objs) != v.GetNumComponents() {
		common.Log.Debug("Invalid number of parameters for RG")
		common.Log.Debug("Number %d not matching colorspace %T", len(objs), v)
		if !proc.relaxedMode {
			return errInvalidNumberOfParams
		}
		obj, err := autoConvertRGBToGray(objs)
		if err != nil {
			common.Log.Debug("ERROR: Fail to convert color to gray: %+v", objs)
			return err
		}
		objs = []core.PdfObject{obj}
	}
	col, err := v.ColorFromPdfObjects(objs)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorspaceStroking = v
	proc.graphicsState.ColorStroking = col
	return nil
}
func (img *ContentStreamInlineImage) toImageBase(resources *model.PdfPageResources) (*imageutil.ImageBase, error) {
	if img.imageBase != nil {
		return img.imageBase, nil
	}
	v := imageutil.ImageBase{}
	if img.Height == nil {
		return nil, errors.New("height attribute missing")
	}
	num, ok := img.Height.(*core.PdfObjectInteger)
	if !ok {
		return nil, errors.New("invalid height")
	}
	v.Height = int(*num)
	if img.Width == nil {
		return nil, errors.New("width attribute missing")
	}
	num2, ok := img.Width.(*core.PdfObjectInteger)
	if !ok {
		return nil, errors.New("invalid width")
	}
	v.Width = int(*num2)
	ok2, err := img.IsMask()
	if err != nil {
		return nil, err
	}
	if ok2 {
		v.BitsPerComponent = 1
		v.ColorComponents = 1
	} else {
		if img.BitsPerComponent == nil {
			common.Log.Debug("Inline Bits per component missing - assuming 8")
			v.BitsPerComponent = 8
		} else {
			num3, ok3 := img.BitsPerComponent.(*core.PdfObjectInteger)
			if !ok3 {
				common.Log.Debug("Error invalid bits per component value, type %T", img.BitsPerComponent)
				return nil, errors.New("BPC Type error")
			}
			v.BitsPerComponent = int(*num3)
		}
		if img.ColorSpace != nil {
			cs, err2 := img.GetColorSpace(resources)
			if err2 != nil {
				return nil, err2
			}
			img.colorspace = cs
			v.ColorComponents = cs.GetNumComponents()
		} else {
			common.Log.Debug("Inline Image colorspace not specified - assuming 1 color component")
			v.ColorComponents = 1
		}
	}
	if arr, ok3 := core.GetArray(img.Decode); ok3 {
		v.Decode, err = arr.ToFloat64Array()
		if err != nil {
			return nil, err
		}
	}
	img.imageBase = &v
	return img.imageBase, nil
}

// NewContentStreamProcessor returns a new ContentStreamProcessor for operations `ops`.
func NewContentStreamProcessor(ops []*ContentStreamOperation) *ContentStreamProcessor {
	v := ContentStreamProcessor{}
	v.gsStack = GraphicStateStack{}
	gs := GraphicsState{}
	v.graphicsState = gs
	v.handlers = []handlerEntry{}
	v.currentIndex = 0
	v.operations = ops
	return &v
}

// SetInitialGraphicsState sets the graphics state that Process starts from
// instead of the PDF defaults (DeviceGray colorspaces, black colors, identity
// CTM). Use it for a stream that inherits the caller's state, such as a form
// XObject's content stream (PDF32000_2008 8.10.1). Nil fields and a zero CTM
// fall back to the defaults; a colorspace seeded without its color gets that
// colorspace's initial color, and a color seeded without its colorspace is
// ignored.
func (proc *ContentStreamProcessor) SetInitialGraphicsState(gs GraphicsState) {
	proc.initialGraphicsState = &gs
}
func newLZWEncoderFromInlineImage(img *ContentStreamInlineImage, dict *core.PdfObjectDictionary) (*core.LZWEncoder, error) {
	v := core.NewLZWEncoder()
	if dict == nil {
		if img.DecodeParms != nil {
			dict2, ok := core.GetDict(img.DecodeParms)
			if !ok {
				common.Log.Debug("Error: DecodeParms not a dictionary (%T)", img.DecodeParms)
				return nil, fmt.Errorf("invalid DecodeParms")
			}
			dict = dict2
		}
	}
	if dict == nil {
		return v, nil
	}
	obj := dict.Get("EarlyChange")
	if obj != nil {
		num, ok := obj.(*core.PdfObjectInteger)
		if !ok {
			common.Log.Debug("Error: EarlyChange specified but not numeric (%T)", obj)
			return nil, fmt.Errorf("invalid EarlyChange")
		}
		if *num != 0 && *num != 1 {
			return nil, fmt.Errorf("invalid EarlyChange value (not 0 or 1)")
		}
		v.EarlyChange = int(*num)
	} else {
		v.EarlyChange = 1
	}
	obj = dict.Get("Predictor")
	if obj != nil {
		num, ok := obj.(*core.PdfObjectInteger)
		if !ok {
			common.Log.Debug("Error: Predictor specified but not numeric (%T)", obj)
			return nil, fmt.Errorf("invalid Predictor")
		}
		v.Predictor = int(*num)
	}
	obj = dict.Get("BitsPerComponent")
	if obj != nil {
		num, ok := obj.(*core.PdfObjectInteger)
		if !ok {
			common.Log.Debug("ERROR: Invalid BitsPerComponent")
			return nil, fmt.Errorf("invalid BitsPerComponent")
		}
		v.BitsPerComponent = int(*num)
	}
	if v.Predictor > 1 {
		v.Columns = 1
		obj = dict.Get("Columns")
		if obj != nil {
			num, ok := obj.(*core.PdfObjectInteger)
			if !ok {
				return nil, fmt.Errorf("predictor column invalid")
			}
			v.Columns = int(*num)
		}
		v.Colors = 1
		obj = dict.Get("Colors")
		if obj != nil {
			num, ok := obj.(*core.PdfObjectInteger)
			if !ok {
				return nil, fmt.Errorf("predictor colors not an integer")
			}
			v.Colors = int(*num)
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("decode params: %s", dict.String())
	}
	return v, nil
}

// Add_b appends 'b' operand to the content stream:
// Close, fill and then stroke the path (nonzero winding number rule).
//
// See section 8.5.3 "Path Painting Operators" and Table 60 (p. 143 PDF32000_2008).
func (cc *ContentCreator) Add_b() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "b"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_f appends 'f' operand to the content stream:
// Fill the path using the nonzero winding number rule to determine fill region.
//
// See section 8.5.3 "Path Painting Operators" and Table 60 (p. 143 PDF32000_2008).
func (cc *ContentCreator) Add_f() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "f"
	cc.operations = append(cc.operations, &op)
	return cc
}

const (
	HandlerConditionEnumOperand HandlerConditionEnum = iota
	HandlerConditionEnumAllOperands
)

func stringsToObjects(v []core.PdfObjectString) []core.PdfObject {
	var objs []core.PdfObject
	for _, v2 := range v {
		objs = append(objs, core.MakeString(v2.Str()))
	}
	return objs
}

// Add_scn_pattern appends 'scn' operand to the content stream for pattern `name`:
// scn with name attribute (for pattern). Syntax: c1 ... cn name scn.
//
// See section 8.6.8 "Colour Operators" and Table 74 (p. 179-180 PDF32000_2008).
func (cc *ContentCreator) Add_scn_pattern(name core.PdfObjectName, c ...float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "scn"
	op.Params = floatsToObjects(c)
	op.Params = append(op.Params, core.MakeName(string(name)))
	cc.operations = append(cc.operations, &op)
	return cc
}
func (proc *ContentStreamProcessor) handleCommand_sc(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	cs := proc.graphicsState.ColorspaceNonStroking
	objs := op.Params
	if !isPatternColorspace(cs) {
		if len(objs) != cs.GetNumComponents() {
			common.Log.Debug("Invalid number of parameters for SC")
			common.Log.Debug("Number %d not matching colorspace %T", len(objs), cs)
			if !proc.relaxedMode {
				return errInvalidNumberOfParams
			}
			obj, err := autoConvertRGBToGray(objs)
			if err != nil {
				common.Log.Debug("ERROR: Fail to convert color to gray: %+v", objs)
				return err
			}
			objs = []core.PdfObject{obj}
		}
	}
	col, err := cs.ColorFromPdfObjects(objs)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorNonStroking = col
	return nil
}

// Add_h appends 'h' operand to the content stream:
// Close the current subpath by adding a line between the current position and the starting position.
//
// See section 8.5.2 "Path Construction Operators" and Table 59 (pp. 140-141 PDF32000_2008).
func (cc *ContentCreator) Add_h() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "h"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_Tr appends 'Tr' operand to the content stream:
// Set text rendering mode.
//
// See section 9.3 "Text State Parameters and Operators" and
// Table 105 (pp. 251-252 PDF32000_2008).
func (cc *ContentCreator) Add_Tr(render int64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Tr"
	op.Params = integersToObjects([]int64{render})
	cc.operations = append(cc.operations, &op)
	return cc
}

// NewContentStreamParser creates a new instance of the content stream parser from an input content
// stream string.
func NewContentStreamParser(contentStr string) *ContentStreamParser {
	v := ContentStreamParser{}
	contentStr = string(leadingSlashesRegexp.ReplaceAll([]byte(contentStr), []byte("/")))
	buf := bytes.NewBufferString(contentStr + "\n")
	v.reader = bufio.NewReader(buf)
	return &v
}

// Add_s appends 's' operand to the content stream: Close and stroke the path.
//
// See section 8.5.3 "Path Painting Operators" and Table 60 (p. 143 PDF32000_2008).
func (cc *ContentCreator) Add_s() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "s"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_j adds 'j' operand to the content stream: Set the line join style (graphics state).
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_j(lineJoinStyle string) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "j"
	op.Params = namesToObjects([]core.PdfObjectName{core.PdfObjectName(lineJoinStyle)})
	cc.operations = append(cc.operations, &op)
	return cc
}
func newFlateEncoderFromInlineImage(img *ContentStreamInlineImage, dict *core.PdfObjectDictionary) (*core.FlateEncoder, error) {
	v := core.NewFlateEncoder()
	if img.imageBase != nil {
		v.SetImage(img.imageBase)
	}
	if dict == nil {
		obj := img.DecodeParms
		if obj != nil {
			dict2, ok := core.GetDict(obj)
			if !ok {
				common.Log.Debug("Error: DecodeParms not a dictionary (%T)", obj)
				return nil, fmt.Errorf("invalid DecodeParms")
			}
			dict = dict2
		}
	}
	if dict == nil {
		return v, nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("decode params: %s", dict.String())
	}
	obj := dict.Get("Predictor")
	if obj == nil {
		common.Log.Debug("Error: Predictor missing from DecodeParms - Continue with default (1)")
	} else {
		num, ok := obj.(*core.PdfObjectInteger)
		if !ok {
			common.Log.Debug("Error: Predictor specified but not numeric (%T)", obj)
			return nil, fmt.Errorf("invalid Predictor")
		}
		v.Predictor = int(*num)
	}
	obj = dict.Get("BitsPerComponent")
	if obj != nil {
		num, ok := obj.(*core.PdfObjectInteger)
		if !ok {
			common.Log.Debug("ERROR: Invalid BitsPerComponent")
			return nil, fmt.Errorf("invalid BitsPerComponent")
		}
		v.BitsPerComponent = int(*num)
	}
	if v.Predictor > 1 {
		v.Columns = 1
		obj = dict.Get("Columns")
		if obj != nil {
			num, ok := obj.(*core.PdfObjectInteger)
			if !ok {
				return nil, fmt.Errorf("predictor column invalid")
			}
			v.Columns = int(*num)
		}
		v.Colors = 1
		obj2 := dict.Get("Colors")
		if obj2 != nil {
			num, ok := obj2.(*core.PdfObjectInteger)
			if !ok {
				return nil, fmt.Errorf("predictor colors not an integer")
			}
			v.Colors = int(*num)
		}
	}
	return v, nil
}
func (proc *ContentStreamProcessor) handleCommand_SCN(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	cs := proc.graphicsState.ColorspaceStroking
	objs := op.Params
	if !isPatternColorspace(cs) {
		if len(objs) != cs.GetNumComponents() {
			common.Log.Debug("Invalid number of parameters for SC")
			common.Log.Debug("Number %d not matching colorspace %T", len(objs), cs)
			if !proc.relaxedMode {
				return errInvalidNumberOfParams
			}
			obj, err := autoConvertRGBToGray(objs)
			if err != nil {
				common.Log.Debug("ERROR: Fail to convert color to gray: %+v", objs)
				return err
			}
			objs = []core.PdfObject{obj}
		}
	}
	col, err := cs.ColorFromPdfObjects(objs)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorStroking = col
	return nil
}

var leadingSlashesRegexp = regexp.MustCompile("^/{2,}")

// SetStrokingColor sets the stroking `color` where color can be one of
// PdfColorDeviceGray, PdfColorDeviceRGB, or PdfColorDeviceCMYK.
func (cc *ContentCreator) SetStrokingColor(color model.PdfColor) *ContentCreator {
	switch c := color.(type) {
	case *model.PdfColorDeviceGray:
		cc.Add_G(c.Val())
	case *model.PdfColorDeviceRGB:
		cc.Add_RG(c.R(), c.G(), c.B())
	case *model.PdfColorDeviceCMYK:
		cc.Add_K(c.C(), c.M(), c.Y(), c.K())
	case *model.PdfColorPatternType1:
		cc.Add_CS(*core.MakeName("Pattern"))
		cc.Add_SCN_pattern(c.PatternName)
	case *model.PdfColorPatternType2:
		cc.Add_CS(*core.MakeName("Pattern"))
		cc.Add_SCN_pattern(c.PatternName)
	case *model.PdfColorPatternType3:
		cc.Add_CS(*core.MakeName("Pattern"))
		cc.Add_SCN_pattern(c.PatternName)
	default:
		common.Log.Debug("SetStrokingColor: unsupported color: %T", c)
	}
	return cc
}

// NewContentCreator returns a new initialized ContentCreator.
func NewContentCreator() *ContentCreator {
	cc := &ContentCreator{}
	cc.operations = ContentStreamOperations{}
	return cc
}

// Add_sh appends 'sh' operand to the content stream:
// Paints the shape and colour shading described by a shading dictionary specified by `name`,
// subject to the current clipping path
//
// See section 8.7.4 "Shading Patterns" and Table 77 (p. 190 PDF32000_2008).
func (cc *ContentCreator) Add_sh(name core.PdfObjectName) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "sh"
	op.Params = namesToObjects([]core.PdfObjectName{name})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_y appends 'y' operand to the content stream: Append a Bezier curve to the current path from the
// current point to (x3,y3) with (x1, y1) and (x3,y3) as control points.
//
// See section 8.5.2 "Path Construction Operators" and Table 59 (pp. 140-141 PDF32000_2008).
func (cc *ContentCreator) Add_y(x1, y1, x3, y3 float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "y"
	op.Params = floatsToObjects([]float64{x1, y1, x3, y3})
	cc.operations = append(cc.operations, &op)
	return cc
}

// ParseInlineImage parses an inline image from a content stream, both reading its properties and binary data.
// When called, "BI" has already been read from the stream.  This function
// finishes reading through "EI" and then returns the ContentStreamInlineImage.
func (p *ContentStreamParser) ParseInlineImage() (*ContentStreamInlineImage, error) {
	v := ContentStreamInlineImage{}
	for {
		p.skipSpaces()
		obj, ok, err := p.parseObject()
		if err != nil {
			return nil, err
		}
		if !ok {
			name, ok2 := core.GetName(obj)
			if !ok2 {
				common.Log.Debug("Invalid inline image property (expecting name) - %T", obj)
				return nil, fmt.Errorf("invalid inline image property (expecting name) - %T", obj)
			}
			obj2, ok3, err2 := p.parseObject()
			if err2 != nil {
				return nil, err2
			}
			if ok3 {
				return nil, fmt.Errorf("not expecting an operand")
			}
			switch *name {
			case "BPC", "BitsPerComponent":
				v.BitsPerComponent = obj2
			case "CS", "ColorSpace":
				v.ColorSpace = obj2
			case "D", "Decode":
				v.Decode = obj2
			case "DP", "DecodeParms":
				v.DecodeParms = obj2
			case "F", "Filter":
				v.Filter = obj2
			case "H", "Height":
				v.Height = obj2
			case "IM", "ImageMask":
				v.ImageMask = obj2
			case "Intent":
				v.Intent = obj2
			case "I", "Interpolate":
				v.Interpolate = obj2
			case "W", "Width":
				v.Width = obj2
			case "Length", "L", "Subtype", "Type":
				common.Log.Debug("Ignoring inline parameter %s", *name)
			default:
				return nil, fmt.Errorf("unknown inline image parameter %s", *name)
			}
		}
		if ok {
			str, ok2 := obj.(*core.PdfObjectString)
			if !ok2 {
				return nil, fmt.Errorf("failed to read inline image - invalid operand")
			}
			if str.Str() == "EI" {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Inline image finished...")
				}
				return &v, nil
			} else if str.Str() == "ID" {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("ID start")
				}
				buf, err2 := p.reader.Peek(1)
				if err2 != nil {
					return nil, err2
				}
				if core.IsWhiteSpace(buf[0]) {
					p.reader.Discard(1)
				}
				v.data = []byte{}
				n := 0
				var buf2 []byte
				for {
					b, err3 := p.reader.ReadByte()
					if err3 != nil {
						common.Log.Debug("Unable to find end of image EI in inline image data")
						return nil, err3
					}
					if n == 0 {
						if core.IsWhiteSpace(b) {
							buf2 = []byte{}
							buf2 = append(buf2, b)
							n = 1
						} else if b == 'E' {
							buf2 = append(buf2, b)
							n = 2
						} else {
							v.data = append(v.data, b)
						}
					} else if n == 1 {
						buf2 = append(buf2, b)
						if b == 'E' {
							n = 2
						} else {
							v.data = append(v.data, buf2...)
							buf2 = []byte{}
							if core.IsWhiteSpace(b) {
								n = 1
							} else {
								n = 0
							}
						}
					} else if n == 2 {
						buf2 = append(buf2, b)
						if b == 'I' {
							n = 3
						} else {
							v.data = append(v.data, buf2...)
							buf2 = []byte{}
							n = 0
						}
					} else if n == 3 {
						buf2 = append(buf2, b)
						if core.IsWhiteSpace(b) {
							buf3, err4 := p.reader.Peek(20)
							if err4 != nil && err4 != io.EOF {
								return nil, err4
							}
							p2 := NewContentStreamParser(string(buf3))
							ok3 := true
							for n2 := 0; n2 < 3; n2++ {
								obj2, ok4, err5 := p2.parseObject()
								if err5 != nil {
									if err5 == io.EOF {
										break
									}
									ok3 = false
									continue
								}
								if ok4 && !isKnownOperand(obj2.String()) {
									ok3 = false
									break
								}
							}
							if ok3 {
								if len(v.data) > 100 {
									if common.Log.IsLogLevel(common.LogLevelTrace) {
										common.Log.Trace("Image stream (%d): % x ...", len(v.data), v.data[:100])
									}
								} else {
									if common.Log.IsLogLevel(common.LogLevelTrace) {
										common.Log.Trace("Image stream (%d): % x", len(v.data), v.data)
									}
								}
								return &v, nil
							}
						}
						v.data = append(v.data, buf2...)
						buf2 = []byte{}
						n = 0
					}
				}
			}
		}
	}
}

// Add_B_starred appends 'B*' operand to the content stream:
// Fill and then stroke the path (even-odd rule).
//
// See section 8.5.3 "Path Painting Operators" and Table 60 (p. 143 PDF32000_2008).
func (cc *ContentCreator) Add_B_starred() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "B*"
	cc.operations = append(cc.operations, &op)
	return cc
}

var knownOperands = map[string]struct{}{"b": {}, "B": {}, "b*": {}, "B*": {}, "BDC": {}, "BI": {}, "BMC": {}, "BT": {}, "BX": {}, "c": {}, "cm": {}, "CS": {}, "cs": {}, "d": {}, "d0": {}, "d1": {}, "Do": {}, "DP": {}, "EI": {}, "EMC": {}, "ET": {}, "EX": {}, "f": {}, "F": {}, "f*": {}, "G": {}, "g": {}, "gs": {}, "h": {}, "i": {}, "ID": {}, "j": {}, "J": {}, "K": {}, "k": {}, "l": {}, "m": {}, "M": {}, "MP": {}, "n": {}, "q": {}, "Q": {}, "re": {}, "RG": {}, "rg": {}, "ri": {}, "s": {}, "S": {}, "SC": {}, "sc": {}, "SCN": {}, "scn": {}, "sh": {}, "T*": {}, "Tc": {}, "Td": {}, "TD": {}, "Tf": {}, "Tj": {}, "TJ": {}, "TL": {}, "Tm": {}, "Tr": {}, "Ts": {}, "Tw": {}, "Tz": {}, "v": {}, "w": {}, "W": {}, "W*": {}, "y": {}, "'": {}, "\"": {}}

// All returns true if `hce` is equivalent to HandlerConditionEnumAllOperands.
func (v HandlerConditionEnum) All() bool { return v == HandlerConditionEnumAllOperands }
func namesToObjects(v []core.PdfObjectName) []core.PdfObject {
	var objs []core.PdfObject
	for _, name := range v {
		objs = append(objs, core.MakeName(string(name)))
	}
	return objs
}

// Add_EMC appends 'EMC' operand to the content stream:
// Ends a marked-content sequence.
//
// See section 14.6 "Marked Content" and Table 320 (p. 561 PDF32000_2008).
func (cc *ContentCreator) Add_EMC() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "EMC"
	cc.operations = append(cc.operations, &op)
	return cc
}
