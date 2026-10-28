package creator

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

type templateProcessor struct {
	creator     *Creator
	data        []byte
	options     *TemplateOptions
	renderer    componentRenderer
	filename    string
	classStyles map[string][]xml.Attr
	fontCache   map[string]*model.PdfFont
}

func (tp *templateProcessor) parseImage(node *templateNode) (any, error) {
	effectiveAttrValue, _ := tp.effectiveAttrValue(node, "src")
	image, s, err := tp.loadImageFromSrc(effectiveAttrValue)
	if err != nil {
		return nil, tp.nodeError(node, "could not load image `%s`: %v", effectiveAttrValue, err)
	}
	var dctEncoder core.StreamEncoder
	if s == ".jpg" || s == ".jpeg" {
		dctEncoder = core.NewDCTEncoder()
	} else {
		dctEncoder = core.NewFlateEncoder()
	}
	image.SetEncoder(dctEncoder)
	if err := tp.applyAttrs(node, image, imageAttrs); err != nil {
		return nil, err
	}
	return image, nil
}

func (tp *templateProcessor) parseList(node *templateNode) (any, error) {
	list := tp.creator.NewList()
	if err := tp.applyAttrs(node, list, listAttrs); err != nil {
		return nil, err
	}
	return list, nil
}

func (tp *templateProcessor) parseRectangle(node *templateNode) (any, error) {
	rectangle := tp.creator.NewRectangle(0, 0, 0, 0)
	if err := tp.applyAttrs(node, rectangle, rectangleAttrs); err != nil {
		return nil, err
	}
	return rectangle, nil
}

func (tp *templateProcessor) parsePageBreak(node *templateNode) (any, error) {
	pageBreak := newPageBreak()
	if err := tp.applyAttrs(node, pageBreak, emptyAttrs); err != nil {
		return nil, err
	}
	return pageBreak, nil
}

func (tp *templateProcessor) validateFontValue(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	ok := false
	for s2 := range strings.SplitSeq(s, ",") {
		s2 = strings.TrimSpace(s2)
		if s2 == "" {
			continue
		}
		ok = true
		if _, ok := tp.options.FontMap[s2]; ok {
			return nil
		}
		if _, ok := stdFontNames[s2]; ok {
			return nil
		}
		if attrPropList := tp.parseAttrPropList(s2); len(attrPropList) > 0 {
			if _, ok := attrPropList["path"]; ok {
				return nil
			}
		}
	}
	if ok {
		return fmt.Errorf("unresolved font %q", s)
	}
	return nil
}

func (tp *templateProcessor) checkStyleText(node *templateNode) error {
	for _, childValue := range node.childValues {
		if templateText2, ok := childValue.(*templateText); ok {
			if _, err := tp.checkNodeText(node, templateText2.text, templateText2); err != nil {
				return err
			}
		}
	}
	return nil
}

func (tp *templateProcessor) validateCarrierValue(node *templateNode, s2, s string) error {
	if !tp.options.Strict {
		return nil
	}
	if err := tp.typographyValueErr(s2, s); err != nil {
		return tp.nodeError(node, "invalid value %q for attribute %q: %v", s, s2, err)
	}
	return nil
}

func (tp *templateProcessor) validateTable(node *templateNode) error {
	if _, err := tp.tableColumns(node); err != nil {
		return err
	}
	fn := func(s string) int {
		effectiveAttrValue, _ := tp.effectiveAttrValue(node, s)
		int642, _ := parseInt64(effectiveAttrValue)
		return int(int642)
	}
	return tp.setTableHeaderRows(node, &Table{}, fn("header-start-row"), fn("header-end-row"))
}

func (tp *templateProcessor) parseChapter(node *templateNode) (any, error) {
	fn := tp.creator.NewChapter
	if componentParent := node.componentParent(); componentParent != nil {
		if chapter, ok := componentParent.value.(*Chapter); ok {
			fn = chapter.NewSubchapter
		}
	}
	chapter := fn("")
	if err := tp.applyAttrs(node, chapter, chapterAttrs); err != nil {
		return nil, err
	}
	return chapter, nil
}

func (tp *templateProcessor) childStyle(node *templateNode) map[string]string {
	nodeOwnStyle := tp.nodeOwnStyle(node)
	if len(nodeOwnStyle) == 0 {
		return node.attrs
	}
	m := make(map[string]string, len(node.attrs)+len(nodeOwnStyle))
	maps.Copy(m, node.attrs)
	maps.Copy(m, nodeOwnStyle)
	return m
}

func (tp *templateProcessor) checkTopLevelText(templateText2 *templateText) error {
	if isBlank(templateText2.text) {
		return nil
	}
	if tp.options.Strict {
		return tp.nodeError(templateText2.pos(), "text content is not valid outside a tag")
	}
	tp.nodeLogDebug(templateText2.pos(), "Text content outside a tag. Skipping.")
	return nil
}

func (tp *templateProcessor) checkNodeText(node *templateNode, s string, templateText2 *templateText) (bool, error) {
	local := node.start.Name.Local
	if _, ok := textContentTags[local]; ok {
		return true, nil
	}
	if isBlank(s) {
		return false, nil
	}
	if tp.options.Strict {
		return false, tp.nodeError(templateText2.pos(), "tag <%s> does not take text content", local)
	}
	tp.nodeLogDebug(templateText2.pos(), "Tag <%s> does not take text content. Skipping.", local)
	return false, nil
}

func (tp *templateProcessor) effectiveAttrValue(node *templateNode, s string) (string, bool) {
	var (
		value string
		ok    bool
	)
	for _, s2 := range node.classNames() {
		for _, attr := range tp.classStyles[s2] {
			if attr.Name.Local == s {
				value, ok = attr.Value, true
			}
		}
	}
	for _, attr := range node.start.Attr {
		if attr.Name.Space == "" && attr.Name.Local == s {
			value, ok = attr.Value, true
		}
	}
	return value, ok
}

func (tp *templateProcessor) parseParagraph(node *templateNode, styledParagraph *StyledParagraph) (any, error) {
	if styledParagraph == nil {
		styledParagraph = tp.creator.NewStyledParagraph()
	}
	if err := tp.applyAttrs(node, styledParagraph, chapterHeadingAttrs); err != nil {
		return nil, err
	}
	return styledParagraph, nil
}

func (tp *templateProcessor) setTableHeaderRows(node *templateNode, table *Table, startRow, endRow int) error {
	if startRow != 0 && endRow == 0 {
		endRow = startRow
	} else if startRow == 0 && endRow != 0 {
		startRow = 1
	}
	table.headerStartRow, table.headerEndRow = 0, 0
	if startRow == 0 {
		return nil
	}
	if err := table.SetHeaderRows(startRow, endRow); err != nil {
		if tp.options.Strict {
			return tp.nodeError(node, "invalid table header rows: %v", err)
		}
		tp.nodeLogDebug(node, "Could not set table header rows: %v.", err)
	}
	return nil
}

func (tp *templateProcessor) parseAttrPropList(s string) map[string]string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil
	}
	m := map[string]string{}
	for _, field := range fields {
		stringSubmatch := attrFuncRe.FindStringSubmatch(field)
		if len(stringSubmatch) < 3 {
			continue
		}
		s, s2 := strings.TrimSpace(stringSubmatch[1]), stringSubmatch[2]
		if s == "" {
			continue
		}
		m[s] = s2
	}
	return m
}

func (tp *templateProcessor) parseStyledParagraph(node *templateNode) (any, error) {
	styledParagraph := tp.creator.NewStyledParagraph()
	if err := tp.applyAttrs(node, styledParagraph, styledParagraphAttrs); err != nil {
		return nil, err
	}
	return styledParagraph, nil
}

func (tp *templateProcessor) nodeLogDebug(node *templateNode, format string, a ...any) {
	common.Log.Debug(tp.getNodeErrorLocation(node, format, a...))
}

func (tp *templateProcessor) nodeLogError(node *templateNode, format string, a ...any) {
	common.Log.Error(tp.getNodeErrorLocation(node, format, a...))
}

func (tp *templateProcessor) parseListItem(node *templateNode) (any, error) {
	componentParent := node.componentParent()
	if componentParent == nil {
		tp.nodeLogError(node, "List item parent cannot be nil.")
		return nil, errInvalidParent
	}
	list, ok := componentParent.value.(*List)
	if !ok {
		tp.nodeLogError(node, "List item parent is not List.")
		return nil, errInvalidParent
	}
	listItem := newListItem()
	listItem.marker = list.marker
	if err := tp.applyAttrs(node, listItem, emptyAttrs); err != nil {
		return nil, err
	}
	return listItem, nil
}

func (tp *templateProcessor) parseChapterHeading(node *templateNode) (any, error) {
	componentParent := node.componentParent()
	if componentParent == nil {
		tp.nodeLogError(node, "Chapter heading parent cannot be nil.")
		return nil, errInvalidParent
	}
	chapter, ok := componentParent.value.(*Chapter)
	if !ok {
		tp.nodeLogError(node, "Chapter heading parent (%T) is not a chapter.", componentParent.value)
		return nil, errInvalidParent
	}
	heading := chapter.GetHeading()
	if _, err := tp.parseParagraph(node, heading); err != nil {
		return nil, err
	}
	return heading, nil
}

func (tp *templateProcessor) parseTree() (*templateNode, error) {
	b := bytes.TrimPrefix(tp.data, []byte("\ufeff"))
	decoder := xml.NewDecoder(bytes.NewReader(b))
	node := &templateNode{}
	parent := node
	for {
		line, col := decoderInputPos(decoder)
		inputOffset := decoder.InputOffset()
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if token == nil {
			break
		}
		switch token2 := token.(type) {
		case xml.StartElement:
			common.Log.Debug("Parsing template start tag: `%s`.", token2.Name.Local)
			node := &templateNode{start: token2.Copy(), parent: parent, line: line, col: col, offset: inputOffset}
			parent.children = append(parent.children, node)
			parent.childValues = append(parent.childValues, node)
			parent = node
		case xml.EndElement:
			common.Log.Debug("Parsing template end tag: `%s`.", token2.Name.Local)
			if parent != node {
				parent = parent.parent
			}
		case xml.CharData:
			templateText2 := newTemplateText(b, string(token2), line, col, inputOffset)
			if childValuesLen := len(parent.childValues); childValuesLen > 0 {
				if templateText3, ok := parent.childValues[childValuesLen-1].(*templateText); ok {
					if isBlank(templateText3.text) && !isBlank(templateText2.text) {
						templateText3.line, templateText3.col, templateText3.offset = templateText2.line, templateText2.col, templateText2.offset
					}
					templateText3.text += templateText2.text
					break
				}
			}
			parent.childValues = append(parent.childValues, templateText2)
		case xml.Comment:
			common.Log.Debug("Parsing template comment: `%s`.", string(token2))
		}
	}
	return node, nil
}

func (tp *templateProcessor) parseDivision(node *templateNode) (any, error) {
	division := tp.creator.NewDivision()
	if err := tp.applyAttrs(node, division, divisionAttrs); err != nil {
		return nil, err
	}
	return division, nil
}

func (tp *templateProcessor) strictErr(node *templateNode, s, s2 string, err error) error {
	if tp.options.Strict {
		return tp.nodeError(node, "invalid value %q for attribute %q: %v", s2, s, err)
	}
	return nil
}

func (tp *templateProcessor) parseColor(hexStr string) Color {
	if hexStr == "" {
		return nil
	}
	color, ok := tp.options.ColorMap[hexStr]
	if ok {
		return color
	}
	if hexStr[0] == '#' {
		if isHexColor(hexStr) {
			return ColorRGBFromHex(hexStr)
		}
		return nil
	}
	return nil
}

func (tp *templateProcessor) parseEllipse(node *templateNode) (any, error) {
	ellipse := tp.creator.NewEllipse(0, 0, 0, 0)
	if err := tp.applyAttrs(node, ellipse, ellipseAttrs); err != nil {
		return nil, err
	}
	return ellipse, nil
}

func (tp *templateProcessor) parseLinearGradientAttr(creator *Creator, s2 string) (Color, error) {
	linearGradientAttr := ColorBlack
	if s2 == "" {
		return linearGradientAttr, nil
	}
	linearGradientColor := creator.NewLinearGradientColor([]*ColorPoint{})
	linearGradientColor.SetExtends(true, true)
	var (
		strs = strings.Split(s2[16:len(s2)-1], ",")
		s    = strings.TrimSpace(strs[0])
		err  error
	)
	if strings.HasSuffix(s, "deg") {
		finiteFloat, err2 := parseFiniteFloat(s[:len(s)-3])
		if err2 != nil {
			common.Log.Debug("Failed parsing gradient angle: %v", err2)
			setFirstError(&err, fmt.Errorf("invalid gradient angle %q", s))
		} else {
			linearGradientColor.SetAngle(finiteFloat)
		}
		strs = strs[1:]
	}
	colors, positions, err2 := tp.processGradientColorPair(strs)
	setFirstError(&err, err2)
	if colors == nil || positions == nil {
		return linearGradientAttr, err
	}
	if len(colors) == 1 {
		return colors[0], err
	}
	for i := 0; i < len(colors); i++ {
		linearGradientColor.AddColorStop(colors[i], positions[i])
	}
	return linearGradientColor, err
}

func newTemplateProcessor(creator *Creator, filename string, data []byte, templateOptions *TemplateOptions, renderer componentRenderer) *templateProcessor {
	if templateOptions == nil {
		templateOptions = &TemplateOptions{}
	}
	templateOptions.init()
	if renderer == nil {
		renderer = creator
	}
	return &templateProcessor{creator: creator, data: data, options: templateOptions, renderer: renderer, filename: filename}
}

func (tp *templateProcessor) parseBackground(node *templateNode) (any, error) {
	background := &Background{}
	if err := tp.applyAttrs(node, background, backgroundAttrs); err != nil {
		return nil, err
	}
	return background, nil
}

func (tp *templateProcessor) loadImageFromSrc(s string) (*Image, string, error) {
	if s == "" {
		common.Log.Error("Image `src` attribute cannot be empty.")
		return nil, "", errInvalidResource
	}
	strs := strings.SplitSeq(s, ",")
	for s := range strs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		img, ok := tp.options.ImageMap[s]
		if ok {
			image, err := newImage(img)
			return image, "", err
		}
		if attrPropList := tp.parseAttrPropList(s); len(attrPropList) > 0 {
			if name, ok := attrPropList["path"]; ok {
				if image, err := newImageFromFile(name); err != nil {
					common.Log.Debug("Could not load image `%s`: %v.", name, err)
				} else {
					return image, strings.ToLower(filepath.Ext(name)), nil
				}
			}
		}
	}
	common.Log.Error("Could not find image resource: `%s`.", s)
	return nil, "", errInvalidResource
}

func (tp *templateProcessor) effectiveParentName(node *templateNode) string {
	if activeParent := node.activeParent(); activeParent != nil {
		return activeParent.start.Name.Local
	}
	if _, ok := tp.renderer.(*Block); ok {
		return "block"
	}
	return "creator"
}

func (tp *templateProcessor) resolveColor(s string) (Color, error) {
	hexStr := strings.TrimSpace(s)
	if hexStr == "" {
		return ColorBlack, nil
	}
	if isGradientExpr(hexStr) {
		if strings.HasPrefix(hexStr, "linear-gradient(") {
			return tp.parseLinearGradientAttr(tp.creator, hexStr)
		}
		return tp.parseRadialGradientAttr(tp.creator, hexStr)
	}
	if color, ok := tp.options.ColorMap[hexStr]; ok {
		return color, nil
	}
	if strings.HasPrefix(hexStr, "#") && isHexColor(hexStr) {
		return ColorRGBFromHex(hexStr), nil
	}
	return ColorBlack, fmt.Errorf("unresolved color %q", s)
}
