package creator

import (
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"text/template/parse"
	"unicode"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/linebreak"
	"github.com/OpenMellon/openpdf/model"
)

const (
	styleElementName = "style"
	classElementName = "class"
	classAttrName    = "class"
)

func applySVGTransform(graphicSVGElement *GraphicSVGElement, cc *contentstream.ContentCreator) {
	attribute, ok := graphicSVGElement.Attributes["transform"]
	if ok {
		fields := strings.FieldsSeq(attribute)
		for field := range fields {
			fieldsFunc := strings.FieldsFunc(field, isTransformSeparator)
			if len(fieldsFunc) < 3 {
				common.Log.Debug("can't parse transform attribute %v", attribute)
				return
			}
			svgLength, err := parseSVGLength(fieldsFunc[1], 64)
			if err != nil {
				common.Log.Debug("can't parse transform attribute %v", attribute)
				return
			}
			ty, err := parseSVGLength(fieldsFunc[2], 64)
			if err != nil {
				common.Log.Debug("can't parse transform attribute %v", attribute)
				return
			}
			switch fieldsFunc[0] {
			case "translate":
				cc.Translate(svgLength, ty)
			case "scale":
				cc.Scale(svgLength, ty)
			default:
				common.Log.Debug("can't parse transform attribute %v", attribute)
				return
			}
		}
	}
}

func applyShapeColor(block *Block, color model.PdfColor, color2 Color, fn func() Rectangle) error {
	switch colorPatternType1 := color.(type) {
	case *model.PdfColorPatternType1:
		functionBasedShading, ok := color2.(*FunctionBasedShading)
		if !ok {
			return fmt.Errorf("Color is not FunctionBasedShading")
		}
		rect := fn()
		functionBasedShading.SetBoundingBox(rect.x, rect.y, rect.width, rect.height)
		name, err := functionBasedShading.AddPatternResource(block)
		if err != nil {
			return fmt.Errorf("failed adding pattern to resources: %v", err)
		}
		colorPatternType1.PatternName = name
	case *model.PdfColorPatternType2:
		linearShading, ok := color2.(*LinearShading)
		if !ok {
			return fmt.Errorf("Color is not LinearShading")
		}
		rect := fn()
		linearShading.SetBoundingBox(rect.x, rect.y, rect.width, rect.height)
		name, err := linearShading.AddPatternResource(block)
		if err != nil {
			return fmt.Errorf("failed adding pattern to resources: %v", err)
		}
		colorPatternType1.PatternName = name
	case *model.PdfColorPatternType3:
		radialShading, ok := color2.(*RadialShading)
		if !ok {
			return fmt.Errorf("Color is not RadialShading")
		}
		rect := fn()
		radialShading.SetBoundingBox(rect.x, rect.y, rect.width, rect.height)
		name, err := radialShading.AddPatternResource(block)
		if err != nil {
			return fmt.Errorf("failed adding pattern to resources: %v", err)
		}
		colorPatternType1.PatternName = name
	}
	return nil
}

func isTransformSeparator(r rune) bool { return r == '(' || r == ',' || r == ')' }

func isVariationSelector(r rune) bool {
	return (r >= 0xFE00 && r <= 0xFE0F) || (r >= 0xE0100 && r <= 0xE01EF)
}

func isSkinToneModifier(r rune) bool { return r >= 0x1F3FB && r <= 0x1F3FF }

func isGradientExpr(s string) bool {
	s2 := strings.TrimSpace(s)
	return (strings.HasPrefix(s2, "linear-gradient(") || strings.HasPrefix(s2, "radial-gradient(")) && strings.HasSuffix(s2, ")") && len(s2) > 17
}

func isKeycapSequence(s string) bool {
	rs := []rune(s)
	if len(rs) < 2 {
		return false
	}
	if !isKeycapBase(rs[0]) {
		return false
	}
	if len(rs) == 3 && rs[1] == 0xFE0F && isKeycapEnclosing(rs[2]) {
		return true
	}
	if len(rs) == 2 && isKeycapEnclosing(rs[1]) {
		return true
	}
	return false
}

func isHexColor(s string) bool {
	if (len(s) != 4 && len(s) != 7) || s[0] != '#' {
		return false
	}
	for _, r := range s[1:] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

func isTagCharacter(r rune) bool { return r >= 0xE0020 && r <= 0xE007F }

func isRegionalIndicator(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

func isEmoji(r rune) bool {
	switch {
	case r >= 0x1F600 && r <= 0x1F64F:
		return true
	case r >= 0x1F300 && r <= 0x1F5FF:
		return true
	case r >= 0x1F680 && r <= 0x1F6FF:
		return true
	case r >= 0x1F1E6 && r <= 0x1F1FF:
		return true
	case r >= 0x1F900 && r <= 0x1F9FF:
		return true
	case r >= 0x1FA70 && r <= 0x1FAFF:
		return true
	case r >= 0x2600 && r <= 0x26FF:
		return true
	case r >= 0x2700 && r <= 0x27BF:
		return true
	default:
		return false
	}
}

func isKeycapBase(r rune) bool { return (r >= '0' && r <= '9') || r == '#' || r == '*' }

func isBlank(s string) bool { return strings.Trim(s, whitespaceChars) == "" }

func isNilValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return value.IsNil()
	}
	return false
}

func isKeycapEnclosing(r rune) bool { return r == 0x20E3 }

func isZeroWidthJoiner(r rune) bool { return r == 0x200D }

func svgStandardFont(s string) (*model.PdfFont, error) {
	basefont, ok := map[string]model.StdFontName{"courier": model.CourierName, "courier-bold": model.CourierBoldName, "courier-oblique": model.CourierObliqueName, "courier-bold-oblique": model.CourierBoldObliqueName, "helvetica": model.HelveticaName, "helvetica-bold": model.HelveticaBoldName, "helvetica-oblique": model.HelveticaObliqueName, "helvetica-bold-oblique": model.HelveticaBoldObliqueName, "symbol": model.SymbolName, "zapf-dingbats": model.ZapfDingbatsName, "times": model.TimesRomanName, "times-bold": model.TimesBoldName, "times-italic": model.TimesItalicName, "times-bold-italic": model.TimesBoldItalicName}[s]
	if !ok {
		return nil, fmt.Errorf("font-family %s not found in the standard fonts list", s)
	}
	standard14Font, err := model.NewStandard14Font(basefont)
	if err != nil {
		return nil, err
	}
	return standard14Font, nil
}

var svgCommands commands

func addEscapeToTemplate(node parse.Node) {
	switch listNode := node.(type) {
	case *parse.ListNode:
		if listNode == nil {
			return
		}
		for _, node := range listNode.Nodes {
			addEscapeToTemplate(node)
		}
	case *parse.ActionNode:
		addEscapeToPipe(listNode.Pipe)
	case *parse.IfNode:
		addEscapeToTemplate(listNode.List)
		addEscapeToTemplate(listNode.ElseList)
	case *parse.RangeNode:
		addEscapeToTemplate(listNode.List)
		addEscapeToTemplate(listNode.ElseList)
	case *parse.WithNode:
		addEscapeToTemplate(listNode.List)
		addEscapeToTemplate(listNode.ElseList)
	}
}

func addEscapeToPipe(pipeNode *parse.PipeNode) {
	if pipeNode == nil || len(pipeNode.Decl) > 0 || len(pipeNode.Cmds) == 0 {
		return
	}
	if pipeEndsWithRaw(pipeNode) {
		return
	}
	commandNode := &parse.CommandNode{NodeType: parse.NodeCommand}
	commandNode.Args = append(commandNode.Args, parse.NewIdentifier(xmlEscapeFuncName))
	pipeNode.Cmds = append(pipeNode.Cmds, commandNode)
}

func decoderInputPos(decoder *xml.Decoder) (int, int) { return decoder.InputPos() }

func splitGraphemeClusters(s string) []graphemeCluster {
	const maxTagSequence = 16
	rs2 := []rune(s)
	if len(rs2) == 0 {
		return nil
	}
	graphemeClusters := make([]graphemeCluster, 0, len(rs2))
	n := 0
	for n < len(rs2) {
		n2 := n
		rs := []rune{rs2[n]}
		n++
		if isRegionalIndicator(rs2[n2]) {
			if n < len(rs2) && isRegionalIndicator(rs2[n]) {
				rs = append(rs, rs2[n])
				n++
			}
			graphemeClusters = append(graphemeClusters, graphemeCluster{runes: rs, text: string(rs)})
			continue
		}
		if isKeycapBase(rs2[n2]) && n < len(rs2) && (isVariationSelector(rs2[n]) || isKeycapEnclosing(rs2[n])) {
			if isVariationSelector(rs2[n]) {
				rs = append(rs, rs2[n])
				n++
			}
			if n < len(rs2) && isKeycapEnclosing(rs2[n]) {
				rs = append(rs, rs2[n])
				n++
			}
			graphemeClusters = append(graphemeClusters, graphemeCluster{runes: rs, text: string(rs)})
			continue
		}
		if isEmoji(rs2[n2]) {
			for n < len(rs2) {
				if isVariationSelector(rs2[n]) {
					rs = append(rs, rs2[n])
					n++
					continue
				}
				if isSkinToneModifier(rs2[n]) {
					rs = append(rs, rs2[n])
					n++
					continue
				}
				if isTagCharacter(rs2[n]) {
					n2 := 0
					for n < len(rs2) && isTagCharacter(rs2[n]) && n2 < maxTagSequence {
						rs = append(rs, rs2[n])
						n2++
						n++
						if rs[len(rs)-1] == 0xE007F {
							break
						}
					}
					continue
				}
				if isZeroWidthJoiner(rs2[n]) {
					rs = append(rs, rs2[n])
					n++
					if n < len(rs2) && isEmoji(rs2[n]) {
						rs = append(rs, rs2[n])
						n++
						if n < len(rs2) && isVariationSelector(rs2[n]) {
							rs = append(rs, rs2[n])
							n++
						}
					}
					continue
				}
				break
			}
		}
		graphemeClusters = append(graphemeClusters, graphemeCluster{runes: rs, text: string(rs)})
	}
	return graphemeClusters
}

func splitDrawableToFit(drawable VectorDrawable, ctx DrawContext) VectorDrawable {
	switch para := drawable.(type) {
	case *StyledParagraph:
		ctx2 := ctx
		ctx2.Height = math.Floor(ctx.Height - para.margins.Top - para.margins.Bottom - 0.5*para.getTextHeight())
		if ctx2.Height <= 0 {
			return nil
		}
		head, _, err := para.split(ctx2)
		if err != nil || head == nil {
			return nil
		}
		return head
	case *Division:
		ctx2 := ctx
		ctx2.Height = math.Floor(ctx.Height - para.margins.Top - para.margins.Bottom)
		if ctx2.Height <= 0 {
			return nil
		}
		head, _ := para.split(ctx2, 0)
		if head == nil {
			return nil
		}
		return head
	case *List:
		ctx2 := ctx
		ctx2.Height = math.Floor(ctx.Height - para.margins.Vertical())
		if ctx2.Height <= 0 {
			return nil
		}
		head, _ := para.split(ctx2, 0)
		if head == nil {
			return nil
		}
		return head
	case *Table:
		head, _ := para.split(ctx, 0)
		if head == nil {
			return nil
		}
		return head
	}
	return nil
}

func splitUnitSuffix(value2 string) (value, unit string) {
	if value2 == "" || (value2[len(value2)-1] >= '0' && value2[len(value2)-1] <= '9') {
		return value2, ""
	}
	value = value2
	for _, lengthUnit := range lengthUnits {
		if strings.Contains(value, lengthUnit) {
			unit = lengthUnit
		}
		value = strings.TrimSuffix(value, lengthUnit)
	}
	return
}

func splitIntoWords(s string) ([]string, error) {
	if !utf8.ValidString(s) {
		return []string{s}, errors.New("input is not valid UTF-8")
	}
	var (
		strs []string
		rs   []rune
	)
	for _, r := range s {
		if r == '\u000A' {
			if len(rs) > 0 {
				strs = append(strs, string(rs))
			}
			strs = append(strs, string(r))
			rs = nil
			continue
		}
		rs = append(rs, r)
	}
	if len(rs) > 0 {
		strs = append(strs, string(rs))
	}
	var intoWords []string
	for _, str := range strs {
		r := []rune(str)
		scanner := linebreak.NewScanner(r)
		var rs []rune
		for i := 0; i < len(r); i++ {
			_, action, err := scanner.Next()
			if err != nil {
				return nil, err
			}
			if action == linebreak.BreakProhibited || unicode.IsSpace(r[i]) {
				rs = append(rs, r[i])
				if unicode.IsSpace(r[i]) {
					intoWords = append(intoWords, string(rs))
					rs = []rune{}
				}
				continue
			} else {
				if len(rs) > 0 {
					intoWords = append(intoWords, string(rs))
				}
				rs = []rune{r[i]}
			}
		}
		if len(rs) > 0 {
			intoWords = append(intoWords, string(rs))
		}
	}
	return intoWords, nil
}

func valueToString(i any) (string, error) {
	if i == nil {
		return "", nil
	}
	valueOf := reflect.ValueOf(i)
	for (valueOf.Kind() == reflect.Pointer || valueOf.Kind() == reflect.Interface) && !valueOf.Type().Implements(stringerType) && !valueOf.Type().Implements(errorType) && !valueOf.IsNil() {
		valueOf = valueOf.Elem()
	}
	if !valueOf.IsValid() {
		return "", nil
	}
	if !valueOf.Type().Implements(stringerType) && !valueOf.Type().Implements(errorType) {
		switch valueOf.Kind() {
		case reflect.Chan, reflect.Func:
			return "", fmt.Errorf("can't print %s of type %s", valueOf.Kind(), valueOf.Type())
		}
	}
	return fmt.Sprint(valueOf.Interface()), nil
}

func floatArrayAttr(i any) attrApplyFunc {
	value2 := validateSetter("floatArrayAttr", i, 0, float64SliceType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		floatArray, err := parseFloatArray(value)
		if err != nil {
			if strictError := proc.strictErr(node, attrName, value, err); strictError != nil {
				return strictError
			}
		}
		callSetter(value2, target, reflect.ValueOf(floatArray))
		return nil
	}
}

func floatAttr(i any) attrApplyFunc {
	value2 := validateSetter("floatAttr", i, 0, float64Type)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		f, err := parseFloat64(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
			f = 0
		}
		callSetter(value2, target, reflect.ValueOf(f))
		return nil
	}
}

func floatArrayAttrErr(i any) attrApplyFunc {
	value2 := validateSetter("floatArrayAttrErr", i, 1, float64SliceType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		floatArray, err := parseFloatArray(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
		}
		values := callSetter(value2, target, reflect.ValueOf(floatArray))
		if values == nil {
			return nil
		}
		if err, _ := values[0].Interface().(error); err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
		}
		return nil
	}
}

// New creates a new instance of the PDF Creator.
func New() *Creator {
	c := &Creator{}
	c.pages = []*model.PdfPage{}
	c.blocks = map[*model.PdfPage]*Block{}
	c.pageTransforms = map[*model.PdfPage]*pageTransformations{}
	c.pageMCIDs = map[int64]int64{}
	c.SetPageSize(PageSizeLetter)
	pageWidth := 0.1 * c.pageWidth
	c.pageMargins.Left = pageWidth
	c.pageMargins.Right = pageWidth
	c.pageMargins.Top = pageWidth
	c.pageMargins.Bottom = pageWidth
	var err error
	c.fontRegular, err = model.NewStandard14Font(model.HelveticaName)
	if err != nil {
		c.fontRegular = model.DefaultFont()
	}
	c.fontBold, err = model.NewStandard14Font(model.HelveticaBoldName)
	if err != nil {
		c.fontBold = model.DefaultFont()
	}
	c.toc = c.NewTOC("Table of Contents")
	c.AddOutlines = true
	c.outline = model.NewOutline()
	c.AutofixPageContentStream = true
	return c
}

func newURILinkAnnotation(s, s2 string) *model.PdfAnnotation {
	pdfAnnotationLink := model.NewPdfAnnotationLink()
	borderStyle := model.NewBorderStyle()
	borderStyle.SetBorderWidth(0)
	pdfAnnotationLink.BS = borderStyle.ToPdfObject()
	pdfActionURI := model.NewPdfActionURI()
	pdfActionURI.URI = core.MakeString(s)
	pdfAnnotationLink.SetAction(pdfActionURI.PdfAction)
	if s2 != "" {
		pdfAnnotationLink.Contents = core.MakeString(s2)
	}
	return pdfAnnotationLink.PdfAnnotation
}

func newTOC(text string, lineNumberStyle, style2 TextStyle) *TOC {
	style := style2
	style.FontSize = 14
	styledParagraph := newStyledParagraph(style)
	styledParagraph.SetEnableWrap(true)
	styledParagraph.SetTextAlignment(TextAlignmentLeft)
	styledParagraph.SetMargins(0, 0, 0, 5)
	chunk := styledParagraph.Append(text)
	chunk.Style = style
	return &TOC{heading: styledParagraph, lines: []*TOCLine{}, lineNumberStyle: lineNumberStyle, lineTitleStyle: lineNumberStyle, lineSeparatorStyle: lineNumberStyle, linePageStyle: lineNumberStyle, lineSeparator: ".", lineLevelOffset: 10, lineMargins: Margins{0, 0, 2, 2}, positioning: PositionRelative, defaultStyle: lineNumberStyle, showLinks: true, taggedDrawable: taggedDrawable{structType: model.StructureTypeTOC}}
}

func newInternalLinkAnnotation(val int64, val2, val3, val4 float64, s string) *model.PdfAnnotation {
	pdfAnnotationLink := model.NewPdfAnnotationLink()
	borderStyle := model.NewBorderStyle()
	borderStyle.SetBorderWidth(0)
	pdfAnnotationLink.BS = borderStyle.ToPdfObject()
	if val < 0 {
		val = 0
	}
	pdfAnnotationLink.Dest = core.MakeArray(core.MakeInteger(val), core.MakeName("XYZ"), core.MakeFloat(val2), core.MakeFloat(val3), core.MakeFloat(val4))
	if s != "" {
		pdfAnnotationLink.Contents = core.MakeString(s)
	}
	return pdfAnnotationLink.PdfAnnotation
}

func copyAnnotation(annotation *model.PdfAnnotation) *model.PdfAnnotation {
	if annotation == nil {
		return nil
	}
	var pdfAnnotation *model.PdfAnnotation
	switch context := annotation.GetContext().(type) {
	case *model.PdfAnnotationLink:
		if linkAnnotation := copyLinkAnnotation(context); linkAnnotation != nil {
			pdfAnnotation = linkAnnotation.PdfAnnotation
		}
	case *model.PdfAnnotationHighlight:
		if highlightAnnotation := copyHighlightAnnotation(context); highlightAnnotation != nil {
			pdfAnnotation = highlightAnnotation.PdfAnnotation
		}
	}
	return pdfAnnotation
}

func copyLinkAnnotation(annotationLink *model.PdfAnnotationLink) *model.PdfAnnotationLink {
	if annotationLink == nil {
		return nil
	}
	pdfAnnotationLink := model.NewPdfAnnotationLink()
	pdfAnnotationLink.CopyFrom(annotationLink.PdfAnnotation)
	pdfAnnotationLink.BS = annotationLink.BS
	pdfAnnotationLink.A = annotationLink.A
	if action, err := annotationLink.GetAction(); err == nil && action != nil {
		pdfAnnotationLink.SetAction(action)
	}
	if pdfObjectArray, ok := annotationLink.Dest.(*core.PdfObjectArray); ok {
		pdfAnnotationLink.Dest = core.MakeArray(pdfObjectArray.Elements()...)
	}
	return pdfAnnotationLink
}

func copyHighlightAnnotation(annotationHighlight *model.PdfAnnotationHighlight) *model.PdfAnnotationHighlight {
	if annotationHighlight == nil {
		return nil
	}
	pdfAnnotationHighlight := model.NewPdfAnnotationHighlight()
	pdfAnnotationHighlight.CopyFrom(annotationHighlight.PdfAnnotation)
	pdfAnnotationHighlight.CA = annotationHighlight.CA
	pdfAnnotationHighlight.QuadPoints = annotationHighlight.QuadPoints
	return pdfAnnotationHighlight
}

func copyAnnotations(annotations2 []*model.PdfAnnotation) []*model.PdfAnnotation {
	var annotations []*model.PdfAnnotation
	for _, annotation := range annotations2 {
		annotations = append(annotations, copyAnnotation(annotation))
	}
	return annotations
}

const (
	xmlEscapeFuncName = "__xmlescape"
	rawFuncName       = "raw"
)
