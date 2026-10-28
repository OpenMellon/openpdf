package creator

import (
	"errors"
	"fmt"
	"image"
	imagedraw "image/draw"
	"math"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
	"github.com/unidoc/typesetting/di"
	"github.com/unidoc/typesetting/shaping"
)

func collectStructureTags(drawable Drawable, structureTags []*model.StructureTagInfo) []*model.StructureTagInfo {
	var tagInfo *model.StructureTagInfo
	var drawables []VectorDrawable
	switch table := drawable.(type) {
	case *Table:
		tagInfo = table.tagInfo
		for _, cell := range table.cells {
			drawables = append(drawables, cell.content)
		}
	case *List:
		tagInfo = table.tagInfo
		for _, item := range table.items {
			drawables = append(drawables, item.content)
		}
	case *Division:
		tagInfo = table.tagInfo
		drawables = table.components
	case *Grid:
		tagInfo = table.tagInfo
		for _, row := range table.rows {
			for _, cell := range row.cells {
				drawables = append(drawables, cell.content)
			}
		}
	}
	if tagInfo != nil && tagInfo.ApplyTag {
		structureTags = append(structureTags, tagInfo)
	}
	for _, drawable := range drawables {
		if drawable != nil {
			structureTags = collectStructureTags(drawable, structureTags)
		}
	}
	return structureTags
}

func init() {
	for _, templateTag := range templateTags {
		for allowedParentKey := range templateTag.allowedParents {
			containerTags[allowedParentKey] = struct{}{}
		}
	}
}

func roundTo(value float64, multiple float64) float64 { return math.Round(value/multiple) * multiple }

const whitespaceChars = " \t\r\n"

func normalizeTemplateNumber(v any) (any, error) {
	switch n := v.(type) {
	case uint8:
		return int64(n), nil
	case int8:
		return int64(n), nil
	case uint16:
		return int64(n), nil
	case int16:
		return int64(n), nil
	case uint32:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case uint64:
		return int64(n), nil
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	case float32:
		return float64(n), nil
	case float64:
		return n, nil
	}
	return nil, fmt.Errorf("invalid value, %v is not a number", v)
}

var PPI float64 = 72

func intAttr(i any) attrApplyFunc {
	value2 := validateSetter("intAttr", i, 0, int64Type)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		i, err := parseInt64(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
			i = 0
		}
		callSetter(value2, target, reflect.ValueOf(i))
		return nil
	}
}

func intArrayAttr(i any) attrApplyFunc {
	value2 := validateSetter("intArrayAttr", i, 0, int64SliceType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		int64List, err := parseInt64List(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
		}
		callSetter(value2, target, reflect.ValueOf(int64List))
		return nil
	}
}

var (
	lengthUnits  = []string{"cm", "mm", "px", "pt"}
	unitToPoints = map[string]float64{"cm": cmToPt, "mm": mmToPt, "px": pxToPt, "pt": 1}
)

func cropImageGeneric(img *model.Image, rect, rect2 image.Rectangle) (*model.Image, error) {
	goImage, err := img.ToGoImage()
	if err != nil {
		return nil, err
	}
	rgba := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	imagedraw.Draw(rgba, image.Rect(0, 0, rect2.Dx(), rect2.Dy()), goImage, rect2.Min, imagedraw.Src)
	image, err := model.ImageHandling.NewImageFromGoImage(rgba)
	if err != nil {
		return nil, err
	}
	if alpha := cropAlphaData(img, rect, rect2); alpha != nil {
		image.SetAlpha(alpha)
	}
	return image, nil
}

func cropImage(img *model.Image, rect2 image.Rectangle) (*model.Image, error) {
	rect := image.Rect(0, 0, int(img.Width), int(img.Height))
	rect3 := rect2.Intersect(rect)
	switch {
	case rect3.Empty():
		return model.ImageHandling.NewImageFromGoImage(&image.RGBA{})
	case img.BitsPerComponent == 8 && len(img.Decode()) == 0 && (img.ColorComponents == 1 || img.ColorComponents == 3) && len(img.Data) >= int(img.Width)*int(img.Height)*img.ColorComponents:
		return cropImageFast(img, rect2, rect3), nil
	default:
		return cropImageGeneric(img, rect2, rect3)
	}
}

func cropImageFast(img2 *model.Image, rect, rect2 image.Rectangle) *model.Image {
	colorComponents := img2.ColorComponents
	dx, dy := rect.Dx(), rect.Dy()
	colorComponents2 := int(img2.Width) * colorComponents
	dx2 := dx * colorComponents
	colorComponents3 := rect2.Dx() * colorComponents
	data := make([]byte, dx2*dy)
	for i := rect2.Min.Y; i < rect2.Max.Y; i++ {
		n := i*colorComponents2 + rect2.Min.X*colorComponents
		dx := (i - rect2.Min.Y) * dx2
		copy(data[dx:dx+colorComponents3], img2.Data[n:n+colorComponents3])
	}
	img := &model.Image{Width: int64(dx), Height: int64(dy), BitsPerComponent: 8, ColorComponents: colorComponents, Data: data}
	img.SetAlpha(cropAlphaData(img2, rect, rect2))
	img.SetICCProfile(img2.ICCProfile())
	return img
}

func cropAlphaData(img *model.Image, rect, rect2 image.Rectangle) []byte {
	alphaData := img.AlphaData()
	ok := rect != rect2
	if len(alphaData) == 0 && !ok {
		return nil
	}
	width, height := int(img.Width), int(img.Height)
	n := 0
	if len(alphaData) != 0 {
		switch img.BitsPerComponent {
		case 8:
			n = 1
		case 16:
			n = 2
		}
		if n == 0 {
			return nil
		}
		if len(alphaData) != n*width*height {
			common.Log.Debug("Alpha channel has %d bytes, expected %d; missing samples are treated as opaque", len(alphaData), n*width*height)
		}
	}
	dx := rect.Dx()
	data := make([]byte, dx*rect.Dy())
	for i := rect2.Min.Y; i < rect2.Max.Y; i++ {
		data2 := data[(i-rect2.Min.Y)*dx:][:rect2.Dx()]
		for j := range data2 {
			n2 := n * (i*width + rect2.Min.X + j)
			if n > 0 && n2 < len(alphaData) {
				data2[j] = alphaData[n2]
			} else {
				data2[j] = 0xff
			}
		}
	}
	for _, b := range data {
		if b != 0xff {
			return data
		}
	}
	return nil
}

func transformRectangle(rect *model.PdfRectangle, matrix transform.Matrix) *model.PdfRectangle {
	var transformRectangle model.PdfRectangle
	transformRectangle.Llx, transformRectangle.Lly = matrix.Transform(rect.Llx, rect.Lly)
	transformRectangle.Urx, transformRectangle.Ury = matrix.Transform(rect.Urx, rect.Ury)
	transformRectangle.Normalize()
	return &transformRectangle
}

func reverseFloats(reverseFloats []float64) []float64 {
	for i, j := 0, len(reverseFloats)-1; i < j; i, j = i+1, j-1 {
		reverseFloats[i], reverseFloats[j] = reverseFloats[j], reverseFloats[i]
	}
	return reverseFloats
}

func beginTaggedContent(block *Block, structureTagInfo *model.StructureTagInfo) {
	contentCreator := contentstream.NewContentCreator()
	m := map[string]core.PdfObject{}
	if structureTagInfo != nil {
		m["MCID"] = core.MakeInteger(structureTagInfo.Mcid)
	}
	contentCreator.Add_BDC(*core.MakeName(string(structureTagInfo.StructureType)), m)
	block.addContents(contentCreator.Operations())
}

var ErrContentNotFit = errors.New("cannot fit content into an existing space")

func collapseWhitespace(s string, ok, ok2 bool) string {
	if !strings.Contains(s, "\n") {
		return s
	}
	if ok {
		if s2 := strings.TrimLeft(s, whitespaceChars); strings.Contains(s[:len(s)-len(s2)], "\n") {
			s = s2
		}
	}
	if ok2 {
		if s2 := strings.TrimRight(s, whitespaceChars); strings.Contains(s[len(s2):], "\n") {
			s = s2
		}
	}
	return newlineWhitespaceRe.ReplaceAllString(s, " ")
}

func endMarkedContent(block *Block) {
	contentCreator := contentstream.NewContentCreator()
	contentCreator.Add_EMC()
	block.addContents(contentCreator.Operations())
}

func firstLastTextChild(vals []any) (int, int) {
	n, n2 := -1, -1
	for i, v := range vals {
		if hasTextChunk(v) {
			if n < 0 {
				n = i
			}
			n2 = i
		}
	}
	return n, n2
}

var PPMM = float64(72 * 1.0 / 25.4)

func allFirstPartsEmpty(rows [][2]*Table) bool {
	for _, tables := range rows {
		if tables[0] != nil {
			return false
		}
	}
	return true
}

func layoutCellContent(drawable VectorDrawable, left float64, top float64, n CellVerticalAlignment, ok bool, rootKObj *model.KDict, mcid int64, left2 float64) (float64, float64, float64, *model.KDict, bool, error) {
	var (
		ok2           bool
		kDict         *model.KDict
		maxCapHeight3 float64
		err           error
	)
	switch para := drawable.(type) {
	case *StyledParagraph:
		if para.enableWrap {
			left = para.getMaxLineWidth() / 1000.0
		}
		maxCapHeight, maxFontSize, minDescent := para.getLineMetrics(0)
		maxCapHeight2, maxFontSize2 := maxCapHeight*para.lineHeight, maxFontSize*para.lineHeight
		if para.verticalAlignment == TextVerticalAlignmentCenter {
			maxCapHeight3 = maxFontSize2 - (maxFontSize + (maxCapHeight+minDescent-maxFontSize)/2 + (maxFontSize2-maxFontSize)/2)
		}
		if len(para.lines) == 1 {
			top = maxCapHeight2
		} else {
			top = top - maxFontSize2 + maxCapHeight2
		}
		maxCapHeight3 += maxCapHeight2 - maxFontSize2
		switch n {
		case CellVerticalAlignmentTop:
			maxCapHeight3 += maxCapHeight2 * 0.5
		case CellVerticalAlignmentBottom:
			maxCapHeight3 -= maxCapHeight2 * 0.5
		}
		left += para.margins.Left + para.margins.Right
		top += para.margins.Top + para.margins.Bottom
		if rootKObj != nil {
			para.SetMarkedContentID(mcid)
			para.SetStructureType(model.StructureTypeParagraph)
			kDict, err = para.GenerateKDict()
			if err != nil {
				return 0, 0, 0, nil, false, fmt.Errorf("failed to generate K dictionary for styled paragraph: %w", err)
			}
		}
	case *Table:
		left = left2
		if rootKObj != nil {
			para.SetMarkedContentID(mcid)
			para.AddTag(rootKObj)
			ok2 = true
			kDict, err = para.GenerateKDict()
			if err != nil {
				return 0, 0, 0, nil, false, fmt.Errorf("failed to generate K dictionary for table: %w", err)
			}
		}
	case *List:
		left = left2
		if rootKObj != nil {
			para.SetMarkedContentID(mcid)
			para.AddTag(rootKObj)
			ok2 = true
			kDict, err = para.GenerateKDict()
			if err != nil {
				return 0, 0, 0, nil, false, fmt.Errorf("failed to generate K dictionary for list: %w", err)
			}
		}
	case *Division:
		left = left2
		if rootKObj != nil {
			para.SetMarkedContentID(mcid)
			para.AddTag(rootKObj)
			ok2 = true
			kDict, err = para.GenerateKDict()
			if err != nil {
				return 0, 0, 0, nil, false, fmt.Errorf("failed to generate K dictionary for division: %w", err)
			}
		}
	case *Chart:
		left = left2
	case *Line:
		top += para.margins.Top + para.margins.Bottom
		maxCapHeight3 -= para.Height() / 2
	case *Image:
		left += para.margins.Left + para.margins.Right
		top += para.margins.Top + para.margins.Bottom
	}
	return left, top, maxCapHeight3, kDict, ok2, nil
}

var carrierAttrs = map[string]struct{}{"font": {}, "font-size": {}, "color": {}, "line-height": {}, "text-align": {}}

var emptyAttrs = map[string]attrApplyFunc{}

func setFirstError(err2 *error, err error) {
	if *err2 == nil {
		*err2 = err
	}
}

func toPdfColor(color Color) model.PdfColor {
	if color == nil {
		color = ColorBlack
	}
	switch grayColor2 := color.(type) {
	case grayColor:
		return model.NewPdfColorDeviceGray(grayColor2.gray)
	case cmykColor:
		return model.NewPdfColorDeviceCMYK(grayColor2.c, grayColor2.m, grayColor2.y, grayColor2.k)
	case *FunctionBasedShading:
		return model.NewPdfColorPatternType1()
	case *LinearShading:
		return model.NewPdfColorPatternType2()
	case *RadialShading:
		return model.NewPdfColorPatternType3()
	}
	return model.NewPdfColorDeviceRGB(color.ToRGB())
}

func hasEmojiSequenceMarker(s string) bool {
	for _, r := range s {
		switch {
		case r == 0x200D:
			return true
		case r >= 0xFE00 && r <= 0xFE0F:
			return true
		case r >= 0xE0100 && r <= 0xE01EF:
			return true
		case r >= 0x1F3FB && r <= 0x1F3FF:
			return true
		case r >= 0x1F1E6 && r <= 0x1F1FF:
			return true
		case r == 0x20E3:
			return true
		case r >= 0xE0020 && r <= 0xE007F:
			return true
		}
	}
	return false
}

func makeGrayMaskXObject(img3 *model.Image) (*model.XObjectImage, error) {
	img2 := *img3
	img2.SetAlpha(nil)
	img := &img2
	if img2.ColorComponents != 1 {
		goImage, err := img2.ToGoImage()
		if err != nil {
			return nil, err
		}
		img, err = model.ImageHandling.NewGrayImageFromGoImage(goImage)
		if err != nil {
			return nil, err
		}
	}
	xObjectImage, err := model.NewXObjectImageFromImage(img, model.NewPdfColorspaceDeviceGray(), core.NewFlateEncoder())
	if err != nil {
		return nil, err
	}
	if vals := img.Decode(); len(vals) == 2 {
		xObjectImage.Decode = core.MakeArrayFromFloats(vals)
	}
	return xObjectImage, nil
}

func nextResourceName(s string) string {
	allString := trailingDigitsRe.FindAllString(s, -1)
	if len(allString) == 0 {
		s = s + "0"
	} else {
		n, err := strconv.Atoi(allString[len(allString)-1])
		if err != nil {
			common.Log.Debug("Error converting digit component in resources name, fallback to basic method: %v", err)
			s = s + "0"
		} else {
			n++
			lastIndex := strings.LastIndex(s, allString[len(allString)-1])
			if lastIndex == -1 {
				s = fmt.Sprintf("%s%d", s[:len(s)-1], n)
			} else {
				s = s[:lastIndex] + strconv.Itoa(n)
			}
		}
	}
	return s
}

var stdFontNames = map[string]model.StdFontName{"courier": model.CourierName, "courier-bold": model.CourierBoldName, "courier-oblique": model.CourierObliqueName, "courier-bold-oblique": model.CourierBoldObliqueName, "helvetica": model.HelveticaName, "helvetica-bold": model.HelveticaBoldName, "helvetica-oblique": model.HelveticaObliqueName, "helvetica-bold-oblique": model.HelveticaBoldObliqueName, "symbol": model.SymbolName, "zapf-dingbats": model.ZapfDingbatsName, "times": model.TimesRomanName, "times-bold": model.TimesBoldName, "times-italic": model.TimesItalicName, "times-bold-italic": model.TimesBoldItalicName}

var (
	boolType          = reflect.TypeOf(false)
	int64Type         = reflect.TypeFor[int64]()
	float64Type       = reflect.TypeFor[float64]()
	int64SliceType    = reflect.TypeFor[[]int64]()
	float64SliceType  = reflect.TypeFor[[]float64]()
	stringType        = reflect.TypeFor[string]()
	marginsType       = reflect.TypeFor[Margins]()
	errorIfaceType    = reflect.TypeFor[error]()
	colorType         = reflect.TypeFor[Color]()
	streamEncoderType = reflect.TypeFor[core.StreamEncoder]()
	pdfFontPtrType    = reflect.TypeFor[*model.PdfFont]()
)

func boolAttr(i any) attrApplyFunc {
	value2 := validateSetter("boolAttr", i, 0, boolType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		flag, err := parseBool(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
			flag = false
		}
		callSetter(value2, target, reflect.ValueOf(flag))
		return nil
	}
}

func argOrZero(argOrZero reflect.Value, typ reflect.Type) reflect.Value {
	if !argOrZero.IsValid() {
		return reflect.Zero(typ)
	}
	return argOrZero
}

// shapedGlyphObjects converts shaped output to TJ operands. Right-to-left
// runs are reversed, keeping leading and trailing spaces in place.
func shapedGlyphObjects(text []rune, line shaping.Line) []core.PdfObject {
	isSpace := func(glyph shaping.Glyph) bool {
		return glyph.RunesCount() == 1 && glyph.TextIndex() < len(text) && text[glyph.TextIndex()] == ' '
	}
	var objs []core.PdfObject
	add := func(glyph shaping.Glyph) {
		if glyph.GlyphID > 0xffff {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("Shaped glyph ID %d exceeds uint16 - skipping", glyph.GlyphID)
			}
			return
		}
		objs = append(objs, core.MakeHexStringFromHexNumber(uint32(glyph.GlyphID), 2))
	}
	for _, output := range line {
		if output.Direction.Progression() != di.TowardTopLeft || len(output.Glyphs) == 0 {
			for _, glyph := range output.Glyphs {
				add(glyph)
			}
			continue
		}
		leading := 0
		for _, glyph := range output.Glyphs {
			if !isSpace(glyph) {
				break
			}
			leading++
		}
		trailing := 0
		for k := len(output.Glyphs) - 1; k >= leading; k-- {
			if !isSpace(output.Glyphs[k]) {
				break
			}
			trailing++
		}
		if leading == 0 && trailing == 0 {
			for _, glyph := range output.Glyphs {
				add(glyph)
			}
			continue
		}
		for _, glyph := range output.Glyphs[len(output.Glyphs)-trailing:] {
			add(glyph)
		}
		for _, glyph := range output.Glyphs[leading : len(output.Glyphs)-trailing] {
			add(glyph)
		}
		for _, glyph := range output.Glyphs[:leading] {
			add(glyph)
		}
	}
	return objs
}

const (
	DefaultHorizontalScaling = 100
)

func readPagesFromFile(r *os.File) ([]*model.PdfPage, error) {
	pdfReader, err := model.NewPdfReader(r)
	if err != nil {
		return nil, err
	}
	numPages, err := pdfReader.GetNumPages()
	if err != nil {
		return nil, err
	}
	var pages []*model.PdfPage
	for i := 0; i < numPages; i++ {
		page, err := pdfReader.GetPage(i + 1)
		if err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	return pages, nil
}

func validateSetter(s string, i any, n int, types ...reflect.Type) reflect.Value {
	valueOf := reflect.ValueOf(i)
	if !valueOf.IsValid() || valueOf.Kind() != reflect.Func {
		panic(fmt.Sprintf("creator: %s requires a setter function, got %T", s, i))
	}
	typ := valueOf.Type()
	if typ.IsVariadic() || typ.NumIn() != len(types)+1 {
		panic(fmt.Sprintf("creator: %s setter must take a component and %d value(s), got %s", s, len(types), typ))
	}
	if typ.NumOut() != n {
		panic(fmt.Sprintf("creator: %s setter must return %d value(s), got %s", s, n, typ))
	}
	for i, typ2 := range types {
		if in := typ.In(i + 1); in != typ2 {
			panic(fmt.Sprintf("creator: %s setter value %d must be %s, got %s in %s", s, i+1, typ2, in, typ))
		}
	}
	if n == 1 && typ.Out(0) != errorIfaceType {
		panic(fmt.Sprintf("creator: %s setter must return error, got %s", s, typ))
	}
	return valueOf
}

var trailingDigitsRe = regexp.MustCompile("\\d+")

func encoderAttr(i any) attrApplyFunc {
	value2 := validateSetter("encoderAttr", i, 0, streamEncoderType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		encoderFactory, ok := encoderFactories[value]
		if !ok {
			if proc.options.Strict {
				return proc.nodeError(node, "invalid value %q for attribute %q", value, attrName)
			}
			return nil
		}
		callSetter(value2, target, reflect.ValueOf(encoderFactory()))
		return nil
	}
}
