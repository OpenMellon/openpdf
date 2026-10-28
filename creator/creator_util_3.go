package creator

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"text/template/parse"
	"unicode"

	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/model"
)

func wrapAsPaginationArtifact(block *Block, n int) {
	contents := (*block.contents)[n:]
	artifact := model.Artifact{Type: model.ArtifactTypePagination}
	contentCreator := contentstream.NewContentCreator()
	contentCreator.Add_BDC(*core.MakeName("Artifact"), artifact.GenerateMap())
	operations := *contentCreator.Operations()
	for _, content := range contents {
		switch content.Operand {
		case "BDC", "BMC", "EMC":
			continue
		}
		operations = append(operations, content)
	}
	operations = append(operations, &contentstream.ContentStreamOperation{Operand: "EMC"})
	*block.contents = append((*block.contents)[:n], operations...)
}

func pipeEndsWithRaw(pipeNode *parse.PipeNode) bool {
	if pipeNode == nil || len(pipeNode.Cmds) == 0 {
		return false
	}
	cmd := pipeNode.Cmds[len(pipeNode.Cmds)-1]
	if len(cmd.Args) == 0 {
		return false
	}
	switch arg := cmd.Args[0].(type) {
	case *parse.IdentifierNode:
		return arg.Ident == rawFuncName
	case *parse.PipeNode:
		return len(cmd.Args) == 1 && pipeEndsWithRaw(arg)
	}
	return false
}

var (
	stringerType = reflect.TypeFor[fmt.Stringer]()
	errorType    = reflect.TypeFor[error]()
)

func flushToken(tokens []token, text string) ([]token, string) {
	if text != "" {
		tokens = append(tokens, token{text, false})
		text = ""
	}
	return tokens, text
}

func maxMCID(kDict *model.KDict) int64 {
	maxMCID2 := int64(-1)
	if kDict == nil {
		return maxMCID2
	}
	if val, ok := core.GetIntVal(kDict.K); ok {
		maxMCID2 = int64(val)
	} else if arr, ok := core.GetArray(kDict.K); ok {
		for _, obj := range arr.Elements() {
			if val, ok := core.GetIntVal(obj); ok && int64(val) > maxMCID2 {
				maxMCID2 = int64(val)
			}
		}
	}
	for _, kValue := range kDict.GetChildren() {
		if mcid := kValue.GetMCID(); mcid != nil {
			if maxMCID := int64(*mcid); maxMCID > maxMCID2 {
				maxMCID2 = maxMCID
			}
		}
		if kDict := kValue.GetKDict(); kDict != nil {
			if maxMCID3 := maxMCID(kDict); maxMCID3 > maxMCID2 {
				maxMCID2 = maxMCID3
			}
		}
	}
	return maxMCID2
}

func chunksFontMetrics(chunks []*TextChunk) (maxCapHeight, maxFontSize, minDescent float64) {
	if len(chunks) == 0 {
		return 0, 0, 0
	}
	for _, chunk := range chunks {
		fontMetrics := newFontMetrics(chunk.Style.Font, chunk.Style.FontSize)
		if fontMetrics.capHeight > maxCapHeight {
			maxCapHeight = fontMetrics.capHeight
		}
		if fontMetrics.descent < minDescent {
			minDescent = fontMetrics.descent
		}
		if fontSize := chunk.Style.FontSize; fontSize > maxFontSize {
			maxFontSize = fontSize
		}
	}
	return maxCapHeight, maxFontSize, minDescent
}

func alphaToBitmask(img *model.Image) (*model.Image, bool) {
	var n int
	switch img.BitsPerComponent {
	case 8:
		n = 1
	case 16:
		n = 2
	default:
		return nil, false
	}
	alphaData := img.AlphaData()
	width, height := int(img.Width), int(img.Height)
	if len(alphaData) == 0 || len(alphaData) < width*height*n {
		return nil, false
	}
	n2 := (width + 7) / 8
	data := make([]byte, n2*height)
	n3 := 0
	for i := 0; i < height; i++ {
		i2 := i * n2
		for i := 0; i < width; i++ {
			b := alphaData[n3]
			if b != 0x00 && b != 0xff {
				return nil, false
			}
			if n == 2 && alphaData[n3+1] != b {
				return nil, false
			}
			if b == 0xff {
				data[i2+i/8] |= 0x80 >> uint(i%8)
			}
			n3 += n
		}
	}
	return &model.Image{Width: img.Width, Height: img.Height, BitsPerComponent: 1, ColorComponents: 1, Data: data}, true
}

func enumAttr(i any, i2 any) attrApplyFunc {
	valueOf := reflect.ValueOf(i)
	if !valueOf.IsValid() || valueOf.Kind() != reflect.Map || valueOf.Type().Key() != stringType {
		panic(fmt.Sprintf("creator: enumAttr requires a map[string]E value map, got %T", i))
	}
	elem := valueOf.Type().Elem()
	value2 := validateSetter("enumAttr", i2, 0, elem)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		mapIndex := valueOf.MapIndex(reflect.ValueOf(value))
		if !mapIndex.IsValid() {
			if proc.options.Strict {
				return proc.nodeError(node, "invalid value %q for attribute %q", value, attrName)
			}
			mapIndex = reflect.Zero(elem)
		}
		callSetter(value2, target, mapIndex)
		return nil
	}
}

func renderBackground(background *Background, width, height float64) (*Block, error) {
	rectangle := newRectangle(0, 0, width, height)
	rectangle.SetFillColor(background.FillColor)
	rectangle.SetBorderColor(background.BorderColor)
	rectangle.SetBorderWidth(background.BorderSize)
	rectangle.SetBorderRadius(background.BorderRadiusTopLeft, background.BorderRadiusTopRight, background.BorderRadiusBottomLeft, background.BorderRadiusBottomRight)
	ctx := DrawContext{PageWidth: width, PageHeight: height, Width: width, Height: height}
	pageBlocks, _, err := rectangle.GeneratePageBlocks(ctx)
	if err != nil {
		return nil, err
	}
	return pageBlocks[0], nil
}

func rotatedBounds(x2, y2, theta float64) (x, y, width, height float64) {
	if theta == 0 {
		return 0, 0, x2, y2
	}
	boundingBox := contentdraw.Path{Points: []contentdraw.Point{contentdraw.NewPoint(0, 0).Rotate(theta), contentdraw.NewPoint(x2, 0).Rotate(theta), contentdraw.NewPoint(0, y2).Rotate(theta), contentdraw.NewPoint(x2, y2).Rotate(theta)}}.GetBoundingBox()
	return boundingBox.X, boundingBox.Y, boundingBox.Width, boundingBox.Height
}

func rotatedCorners(rect *model.PdfRectangle, value float64) [4]contentdraw.Point {
	x := value * math.Pi / 180.0
	width := rect.Width()
	height := rect.Height()
	value2 := math.Sin(x)
	value3 := math.Cos(x)
	points := [4]contentdraw.Point{{X: precision.RoundFloat(rect.Llx, 3), Y: precision.RoundFloat(rect.Lly, 3)}, {X: precision.RoundFloat(rect.Llx+width*value3, 3), Y: precision.RoundFloat(rect.Lly+width*value2, 3)}, {X: precision.RoundFloat(rect.Llx+width*value3-height*value2, 3), Y: precision.RoundFloat(rect.Lly+width*value2+height*value3, 3)}, {X: precision.RoundFloat(rect.Llx-height*value2, 3), Y: precision.RoundFloat(rect.Lly+height*value3, 3)}}
	return points
}

const (
	pxToPt             = 0.72
	cmToPt             = 28.3464
	mmToPt             = cmToPt / 10
	bezierCircleKappa  = 0.551784
	svgDPI             = 96
	defaultSVGFontSize = 16.0
)

func callSetter(value reflect.Value, i any, values2 ...reflect.Value) []reflect.Value {
	if i == nil {
		return nil
	}
	typ := value.Type()
	values := make([]reflect.Value, 0, len(values2)+1)
	values = append(values, argOrZero(reflect.ValueOf(i), typ.In(0)))
	for i, argOrZero2 := range values2 {
		values = append(values, argOrZero(argOrZero2, typ.In(i+1)))
	}
	return value.Call(values)
}

func uniqueFontName(s string, obj core.PdfObject, resources *model.PdfPageResources) core.PdfObjectName {
	s2 := strings.TrimRightFunc(strings.TrimSpace(s), func(r rune) bool { return unicode.IsNumber(r) })
	if s2 == "" {
		s2 = "Font"
	}
	n := 0
	keyName := core.PdfObjectName(s)
	for {
		font, ok := resources.GetFontByName(keyName)
		if !ok || font == obj {
			break
		}
		n++
		keyName = core.PdfObjectName(fmt.Sprintf("%s%d", s2, n))
	}
	return keyName
}
