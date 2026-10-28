package creator

import (
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/model"
)

func (tp *templateProcessor) walk(activeParent2 *templateNode, attrs map[string]string, ok2 bool) error {
	if activeParent2.start.Name.Local == styleElementName {
		return nil
	}
	activeParent2.attrs = attrs
	templateTag, ok := templateTags[activeParent2.start.Name.Local]
	if !ok {
		if tp.options.Strict {
			return tp.nodeError(activeParent2, "unknown tag <%s>", activeParent2.start.Name.Local)
		}
		tp.nodeLogDebug(activeParent2, "Unsupported template tag <%s>. Skipping. Output may be incorrect.", activeParent2.start.Name.Local)
	} else {
		effectiveParentName := tp.effectiveParentName(activeParent2)
		s, ok3 := effectiveParentName, false
		if _, ok := templateTag.allowedParents[effectiveParentName]; ok {
			ok3 = true
			if activeParent2.start.Name.Local == "page-break" {
				if pageBreakInvalidContext := tp.pageBreakInvalidContext(activeParent2); pageBreakInvalidContext != "" {
					ok3, s = false, pageBreakInvalidContext
				}
			}
		}
		if !ok3 {
			if tp.options.Strict {
				return tp.nodeError(activeParent2, "tag <%s> is not valid inside <%s>", activeParent2.start.Name.Local, s)
			}
			tp.nodeLogDebug(activeParent2, "Tag <%s> is not valid inside <%s>. Skipping.", activeParent2.start.Name.Local, s)
		} else {
			if templateTag2 := templateTags[effectiveParentName]; templateTag2 != nil && templateTag2.singleChild && !templateTag.allowMultipleChildren {
				if activeParent := activeParent2.activeParent(); !activeParent.hasChild {
					activeParent.hasChild = true
				} else {
					if tp.options.Strict {
						return tp.nodeError(activeParent2, "tag <%s> takes a single child; wrap multiple components in <division>", effectiveParentName)
					}
					tp.nodeLogError(activeParent2, "tag <%s> takes a single child; wrap multiple components in <division>. Skipping <%s>.", effectiveParentName, activeParent2.start.Name.Local)
					ok2 = true
				}
			}
			if ok2 {
				activeParent2.hasHandler = templateTag.parse != nil
				if err := tp.applyAttrs(activeParent2, nil, templateTag.attrs); err != nil {
					return err
				}
				if err := tp.validateLink(activeParent2); err != nil {
					return err
				}
				if activeParent2.start.Name.Local == "table" {
					if err := tp.validateTable(activeParent2); err != nil {
						return err
					}
				}
			} else if parse := templateTag.parse; parse != nil {
				value, err := parse(tp, activeParent2)
				if err != nil {
					return err
				}
				activeParent2.value = value
				activeParent2.hasHandler = value != nil
			}
		}
	}
	childStyle := tp.childStyle(activeParent2)
	activeParent2.textAttrs = childStyle
	n, n2 := -1, -1
	if activeParent2.hasHandler && activeParent2.start.Name.Local == "paragraph" {
		n, n2 = firstLastTextChild(activeParent2.childValues)
	}
	for i, childValue := range activeParent2.childValues {
		switch childValue2 := childValue.(type) {
		case *templateNode:
			if err := tp.walk(childValue2, childStyle, ok2); err != nil {
				return err
			}
		case *templateText:
			activeParent := activeParent2
			if !activeParent.hasHandler {
				activeParent = activeParent.activeParent()
			}
			if activeParent == nil {
				if !isBlank(childValue2.text) {
					tp.nodeLogDebug(childValue2.pos(), "Text content inside <%s> has no enclosing tag that takes it. Skipping.", activeParent2.start.Name.Local)
				}
				continue
			}
			text := childValue2.text
			if activeParent.start.Name.Local == "paragraph" {
				ok := activeParent == activeParent2
				text = collapseWhitespace(text, ok && (n < 0 || i <= n), ok && (n2 < 0 || i >= n2))
			}
			ok, err := tp.checkNodeText(activeParent, text, childValue2)
			if err != nil {
				return err
			}
			if ok && !ok2 {
				if err := tp.addNodeText(activeParent, text, childValue2); err != nil {
					return err
				}
			}
		}
	}
	if !ok2 && activeParent2.value != nil {
		if table, ok := activeParent2.value.(*Table); ok {
			tp.clampTableHeaderRows(activeParent2, table)
		}
		if err := tp.renderNode(activeParent2); err != nil {
			return err
		}
	}
	return nil
}

func (tp *templateProcessor) clampTableHeaderRows(node *templateNode, table *Table) {
	if !table.hasHeaderRows || table.headerEndRow <= table.rows {
		return
	}
	tp.nodeLogDebug(node, "Table header rows %d-%d exceed the table's %d rows.", table.headerStartRow, table.headerEndRow, table.rows)
	if table.headerStartRow > table.rows {
		table.hasHeaderRows = false
		table.headerStartRow, table.headerEndRow = 0, 0
	} else {
		table.headerEndRow = table.rows
	}
}

func (tp *templateProcessor) effectiveAttrs(node *templateNode, m map[string]attrApplyFunc) []xml.Attr {
	var attrs []xml.Attr
	if _, ok := styledTextTags[node.start.Name.Local]; ok {
		for attrKey, attr := range node.attrs {
			if _, ok := m[attrKey]; ok {
				attrs = append(attrs, xml.Attr{Name: xml.Name{Local: attrKey}, Value: attr})
			}
		}
	}
	for _, s := range node.classNames() {
		attrs = append(attrs, tp.classStyles[s]...)
	}
	for _, attr := range node.start.Attr {
		if attr.Name.Space == "" && attr.Name.Local == classAttrName {
			continue
		}
		attrs = append(attrs, attr)
	}
	return attrs
}

func (tp *templateProcessor) pageBreakInvalidContext(node *templateNode) string {
	for activeParent := node.activeParent(); activeParent != nil; activeParent = activeParent.activeParent() {
		switch activeParent.start.Name.Local {
		case "division":
			continue
		case "chapter":
			return ""
		default:
			return activeParent.start.Name.Local
		}
	}
	return ""
}

func (tp *templateProcessor) addNodeText(parent *templateNode, text string, templateText2 *templateText) error {
	value := parent.value
	if value == nil {
		return nil
	}
	switch value2 := value.(type) {
	case *TextChunk:
		if parent.hasText {
			value2.Text += text
		} else {
			value2.Text = text
			parent.hasText = true
		}
	case *StyledParagraph:
		switch parent.start.Name.Local {
		case "paragraph":
			if isBlank(text) {
				return nil
			}
			node := &templateNode{start: xml.StartElement{Name: xml.Name{Local: "text-chunk"}}, parent: parent, attrs: parent.textAttrs, line: templateText2.line, col: templateText2.col, offset: templateText2.offset}
			textChunk, err := tp.parseTextChunk(node, nil)
			if err != nil {
				return err
			}
			chunk := textChunk.(*TextChunk)
			chunk.Text = text
			value2.appendChunk(chunk)
		case "chapter-heading":
			if componentParent := parent.componentParent(); componentParent != nil {
				if chapter, ok := componentParent.value.(*Chapter); ok {
					if parent.hasText {
						chapter.title += text
					} else {
						chapter.title = text
						parent.hasText = true
					}
					value2.SetText(chapter.headingText())
				}
			}
		}
	}
	return nil
}

func (tp *templateProcessor) applyAttrs(node *templateNode, target any, m map[string]attrApplyFunc) error {
	if err := tp.checkClasses(node); err != nil {
		return err
	}
	for _, attr := range tp.effectiveAttrs(node, m) {
		if attr.Name.Space != "" {
			continue
		}
		local := attr.Name.Local
		attrApplyFunc, ok := m[local]
		if !ok {
			_, ok := carrierAttrs[local]
			_, ok2 := containerTags[node.start.Name.Local]
			if ok && ok2 {
				if err := tp.validateCarrierValue(node, local, attr.Value); err != nil {
					return err
				}
				continue
			}
			if tp.options.Strict {
				return tp.nodeError(node, "unknown attribute %q on <%s>", local, node.start.Name.Local)
			}
			tp.nodeLogDebug(node, "Unsupported %s attribute: `%s`. Skipping.", node.start.Name.Local, local)
			continue
		}
		if attrApplyFunc == nil {
			continue
		}
		if err := attrApplyFunc(tp, node, local, target, attr.Value); err != nil {
			return err
		}
	}
	return nil
}

func (tp *templateProcessor) resolveFontUncached(s string) (*model.PdfFont, error) {
	fontRegular := tp.creator.fontRegular
	if strings.TrimSpace(s) == "" {
		return fontRegular, nil
	}
	ok := false
	for s2 := range strings.SplitSeq(s, ",") {
		s2 = strings.TrimSpace(s2)
		if s2 == "" {
			continue
		}
		ok = true
		if fontUncached, ok := tp.options.FontMap[s2]; ok {
			return fontUncached, nil
		}
		stdFontName, ok := stdFontNames[s2]
		if ok {
			if standard14Font, err := model.NewStandard14Font(stdFontName); err == nil {
				return standard14Font, nil
			}
		}
		if attrPropList := tp.parseAttrPropList(s2); len(attrPropList) > 0 {
			if filePath, ok := attrPropList["path"]; ok {
				fn := model.NewPdfFontFromTTFFile
				if s, ok := attrPropList["type"]; ok && s == "composite" {
					fn = model.NewCompositePdfFontFile
				}
				if fontUncached, err := fn(filePath); err != nil {
					common.Log.Debug("Could not load font `%s`: %v.", filePath, err)
				} else {
					return fontUncached, nil
				}
			}
		}
	}
	if ok {
		return fontRegular, fmt.Errorf("unresolved font %q", s)
	}
	return fontRegular, nil
}

func (tp *templateProcessor) collectStylesNode(node *templateNode) error {
	for _, node2 := range node.children {
		if node2.start.Name.Local == styleElementName {
			if err := tp.collectClasses(node2); err != nil {
				return err
			}
			continue
		}
		if err := tp.collectStylesNode(node2); err != nil {
			return err
		}
	}
	return nil
}

func (tp *templateProcessor) validate() error { return tp.process(true) }

func (tp *templateProcessor) parseLine(node *templateNode) (any, error) {
	line := tp.creator.NewLine(0, 0, 0, 0)
	if err := tp.applyAttrs(node, line, lineAttrs); err != nil {
		return nil, err
	}
	return line, nil
}

func (tp *templateProcessor) parseTableCell(node *templateNode) (any, error) {
	componentParent := node.componentParent()
	if componentParent == nil {
		tp.nodeLogError(node, "Table cell parent cannot be nil.")
		return nil, errInvalidParent
	}
	table, ok := componentParent.value.(*Table)
	if !ok {
		tp.nodeLogError(node, "Table cell parent (%T) is not a table.", componentParent.value)
		return nil, errInvalidParent
	}
	var n, n2 int64
	for _, s := range []string{"colspan", "rowspan"} {
		effectiveAttrValue, ok := tp.effectiveAttrValue(node, s)
		if !ok {
			continue
		}
		int642, err := parseInt64(effectiveAttrValue)
		if err != nil {
			if err2 := tp.strictErr(node, s, effectiveAttrValue, err); err2 != nil {
				return nil, err2
			}
			int642 = 0
		}
		if s == "colspan" {
			n = int642
		} else {
			n2 = int642
		}
	}
	if n <= 0 {
		n = 1
	}
	if n2 <= 0 {
		n2 = 1
	}
	multiCell := table.MultiCell(int(n2), int(n))
	if err := tp.applyAttrs(node, multiCell, tableCellAttrs); err != nil {
		return nil, err
	}
	return multiCell, nil
}

func (tp *templateProcessor) parseTable(node *templateNode) (any, error) {
	tableColumns, err := tp.tableColumns(node)
	if err != nil {
		return nil, err
	}
	table := tp.creator.NewTable(int(tableColumns))
	if err := tp.applyAttrs(node, table, tableAttrs); err != nil {
		return nil, err
	}
	if err := tp.setTableHeaderRows(node, table, table.headerStartRow, table.headerEndRow); err != nil {
		return nil, err
	}
	return table, nil
}

func (tp *templateProcessor) processGradientColorPair(strs []string) (colors []Color, positions []float64, err error) {
	var flags []bool
	for _, s := range strs {
		fields := strings.Fields(s)
		if len(fields) == 0 {
			setFirstError(&err, errors.New("empty gradient color stop"))
			continue
		}
		if len(fields) > 2 {
			setFirstError(&err, fmt.Errorf("gradient stop %q has extra values", strings.TrimSpace(s)))
		}
		finiteFloat2, ok := 0.0, false
		if len(fields) > 1 {
			field := fields[1]
			if field == "0" {
				ok = true
			} else if !strings.HasSuffix(field, "%") {
				setFirstError(&err, fmt.Errorf("gradient stop position %q must be a percentage", field))
			} else if finiteFloat, err2 := parseFiniteFloat(field[:len(field)-1]); err2 != nil {
				common.Log.Debug("Failed parsing point value: %v", err2)
				setFirstError(&err, fmt.Errorf("invalid gradient stop position %q", field))
			} else {
				finiteFloat2, ok = finiteFloat/100.0, true
			}
		}
		color := tp.parseColor(fields[0])
		if color == nil {
			setFirstError(&err, fmt.Errorf("unresolved gradient stop color %q", fields[0]))
			continue
		}
		colors = append(colors, color)
		positions = append(positions, finiteFloat2)
		flags = append(flags, ok)
	}
	colorsLen := len(colors)
	if colorsLen == 0 {
		common.Log.Debug("Invalid gradient color definition!")
		setFirstError(&err, errors.New("gradient has no color stops"))
		return nil, nil, err
	}
	if colorsLen == 1 {
		setFirstError(&err, errors.New("gradient needs at least two color stops"))
	}
	if !flags[0] {
		positions[0], flags[0] = 0, true
	}
	if !flags[colorsLen-1] {
		positions[colorsLen-1], flags[colorsLen-1] = 1, true
	}
	position2 := 0.0
	for i, position := range positions {
		if !flags[i] {
			continue
		}
		if position < 0 || position > 1 {
			setFirstError(&err, fmt.Errorf("gradient stop position %g%% is outside 0-100%%", position*100))
			position = math.Max(0, math.Min(1, position))
		}
		if position < position2 {
			setFirstError(&err, fmt.Errorf("gradient stop position %g%% is less than an earlier stop", position*100))
			position = position2
		}
		positions[i], position2 = position, position
	}
	n := 0
	for i := 1; i < colorsLen; i++ {
		if !flags[i] {
			continue
		}
		for j := n + 1; j < i; j++ {
			positions[j] = positions[n] + float64(j-n)*(positions[i]-positions[n])/float64(i-n)
		}
		n = i
	}
	return colors, positions, err
}

func (tp *templateProcessor) parseTextChunk(node *templateNode, textChunk *TextChunk) (any, error) {
	if node.parent == nil {
		tp.nodeLogError(node, "Text chunk parent cannot be nil.")
		return nil, errInvalidParent
	}
	var (
		textStyle = tp.creator.NewTextStyle()
		ok2       bool
		ok3       bool
	)
	effectiveAttrValue, ok := tp.effectiveAttrValue(node, "link")
	if ok {
		componentParent := node.componentParent()
		para, ok := templateNodeValue(componentParent).(*StyledParagraph)
		if !ok {
			if tp.options.Strict {
				return nil, tp.nodeError(node, "link attribute is only valid on a paragraph text chunk")
			}
			tp.nodeLogError(node, "Link attribute only applicable to paragraph's text chunk.")
			ok2 = true
		} else {
			textStyle = para.linkStyle
			ok3 = true
		}
	}
	if textChunk == nil {
		textChunk = NewTextChunk("", textStyle)
	}
	if err := tp.applyAttrs(node, textChunk, textChunkAttrs); err != nil {
		return nil, err
	}
	if ok3 {
		if _, ok := tp.nodeOwnStyle(node)["color"]; !ok {
			textChunk.Style.Color = textStyle.Color
		}
	}
	if ok && !ok2 {
		link, err := tp.resolveLink(effectiveAttrValue)
		if err != nil {
			if err2 := tp.strictErr(node, "link", effectiveAttrValue, err); err2 != nil {
				return nil, err2
			}
		}
		if link != nil {
			textChunk.AddAnnotation(link)
		}
	}
	return textChunk, nil
}

func (tp *templateProcessor) resolveFont(s string) (*model.PdfFont, error) {
	if font, ok := tp.fontCache[s]; ok {
		return font, nil
	}
	fontUncached, err := tp.resolveFontUncached(s)
	if err == nil {
		if tp.fontCache == nil {
			tp.fontCache = map[string]*model.PdfFont{}
		}
		tp.fontCache[s] = fontUncached
	}
	return fontUncached, err
}

func (tp *templateProcessor) collectClasses(node *templateNode) error {
	if tp.options.Strict {
		for _, attr := range node.start.Attr {
			if attr.Name.Space == "" {
				return tp.nodeError(node, "<style> does not take attributes")
			}
		}
	}
	if err := tp.checkStyleText(node); err != nil {
		return err
	}
	for _, node2 := range node.children {
		if node2.start.Name.Local != classElementName {
			if tp.options.Strict {
				return tp.nodeError(node2, "only <class> is allowed inside <style>, found <%s>", node2.start.Name.Local)
			}
			tp.nodeLogDebug(node2, "Only <class> is allowed inside <style>. Skipping <%s>.", node2.start.Name.Local)
			continue
		}
		if tp.options.Strict && len(node2.children) > 0 {
			return tp.nodeError(node2, "<class> does not take child elements")
		}
		if err := tp.checkStyleText(node2); err != nil {
			return err
		}
		var (
			value string
			attrs []xml.Attr
		)
		for _, attr := range node2.start.Attr {
			if attr.Name.Space != "" {
				continue
			}
			if attr.Name.Local == "name" {
				value = attr.Value
				continue
			}
			attrs = append(attrs, attr)
		}
		if value == "" {
			if tp.options.Strict {
				return tp.nodeError(node2, "<class> requires a name")
			}
			tp.nodeLogDebug(node2, "Template <class> requires a name. Skipping.")
			continue
		}
		tp.classStyles[value] = attrs
	}
	return nil
}
