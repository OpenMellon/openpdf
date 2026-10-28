package creator

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/model"
)

func (tp *templateProcessor) parseRadialGradientAttr(creator *Creator, s2 string) (Color, error) {
	radialGradientAttr := ColorBlack
	if s2 == "" {
		return radialGradientAttr, nil
	}
	var (
		err         error
		x           = 0.0
		y           = 0.0
		outerRadius = -1.0
		strs        = strings.Split(s2[16:len(s2)-1], ",")
	)
	fn := func(strs []string) bool {
		return len(strs) > 0 && (strs[0][0] == '#' || tp.parseColor(strs[0]) != nil)
	}
	fields := strings.Fields(strs[0])
	if len(fields) == 2 && !fn(fields) {
		if finiteFloat, err2 := parseFiniteFloat(fields[0]); err2 != nil {
			common.Log.Debug("Failed parsing radial gradient X position: %v", err2)
			setFirstError(&err, fmt.Errorf("invalid radial gradient X position %q", fields[0]))
		} else {
			x = finiteFloat
		}
		if finiteFloat, err2 := parseFiniteFloat(fields[1]); err2 != nil {
			common.Log.Debug("Failed parsing radial gradient Y position: %v", err2)
			setFirstError(&err, fmt.Errorf("invalid radial gradient Y position %q", fields[1]))
		} else {
			y = finiteFloat
		}
		strs = strs[1:]
	}
	if len(strs) == 0 {
		setFirstError(&err, errors.New("gradient has no color stops"))
		return radialGradientAttr, err
	}
	s := strings.TrimSpace(strs[0])
	if len(s) > 0 && !fn(strings.Fields(s)) {
		if finiteFloat, err2 := parseFiniteFloat(s); err2 != nil {
			common.Log.Debug("Failed parsing radial gradient size: %v", err2)
			setFirstError(&err, fmt.Errorf("radial gradient component %q is neither a size nor a color", s))
		} else {
			outerRadius = finiteFloat
		}
		strs = strs[1:]
	}
	colors, positions, err2 := tp.processGradientColorPair(strs)
	setFirstError(&err, err2)
	if colors == nil || positions == nil {
		return radialGradientAttr, err
	}
	if len(colors) == 1 {
		return colors[0], err
	}
	radialGradientColor := creator.NewRadialGradientColor(x, y, 0, outerRadius, []*ColorPoint{})
	for i := 0; i < len(colors); i++ {
		radialGradientColor.AddColorStop(colors[i], positions[i])
	}
	return radialGradientColor, err
}

func (tp *templateProcessor) getNodeErrorLocation(node *templateNode, format string, a ...any) string {
	s := fmt.Sprintf(format, a...)
	s2 := fmt.Sprintf("%d", node.offset)
	if node.line != 0 {
		s2 = fmt.Sprintf("%d:%d", node.line, node.col)
	}
	if tp.filename != "" {
		return fmt.Sprintf("%s [%s:%s]", s, tp.filename, s2)
	}
	return fmt.Sprintf("%s [%s]", s, s2)
}

func (tp *templateProcessor) resolveLink(s string) (*model.PdfAnnotation, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if strings.HasPrefix(s, "url('") && strings.HasSuffix(s, "')") && len(s) > 7 {
		return newURILinkAnnotation(s[5:len(s)-2], ""), nil
	}
	if strings.HasPrefix(s, "page(") && strings.HasSuffix(s, ")") && len(s) > 6 {
		var (
			err  error
			n    int64
			val  float64
			val2 float64
			val3 = 1.0
			strs = strings.Split(s[5:len(s)-1], ",")
		)
		n, err = strconv.ParseInt(strings.TrimSpace(strs[0]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("link page parameter: %v", err)
		}
		if len(strs) >= 2 {
			val, err = strconv.ParseFloat(strings.TrimSpace(strs[1]), 64)
			if err != nil {
				return nil, fmt.Errorf("link X parameter: %v", err)
			}
		}
		if len(strs) >= 3 {
			val2, err = strconv.ParseFloat(strings.TrimSpace(strs[2]), 64)
			if err != nil {
				return nil, fmt.Errorf("link Y parameter: %v", err)
			}
		}
		if len(strs) >= 4 {
			val3, err = strconv.ParseFloat(strings.TrimSpace(strs[3]), 64)
			if err != nil {
				return nil, fmt.Errorf("link zoom parameter: %v", err)
			}
		}
		return newInternalLinkAnnotation(n-1, val, val2, val3, ""), nil
	}
	return nil, fmt.Errorf("unrecognized link %q", s)
}

func (tp *templateProcessor) process(ok bool) error {
	tree, err := tp.parseTree()
	if err != nil {
		return err
	}
	if err := tp.collectStyles(tree); err != nil {
		return err
	}
	for _, childValue := range tree.childValues {
		switch childValue2 := childValue.(type) {
		case *templateNode:
			if err := tp.walk(childValue2, nil, ok); err != nil {
				return err
			}
		case *templateText:
			if err := tp.checkTopLevelText(childValue2); err != nil {
				return err
			}
		}
	}
	return nil
}

func (tp *templateProcessor) checkClasses(node *templateNode) error {
	for _, s := range node.classNames() {
		if _, ok := tp.classStyles[s]; ok {
			continue
		}
		if tp.options.Strict {
			return tp.nodeError(node, "undefined class %q", s)
		}
		tp.nodeLogDebug(node, "Undefined class %q. Skipping.", s)
	}
	return nil
}

func (tp *templateProcessor) validateLink(node *templateNode) error {
	templateTag := templateTags[node.start.Name.Local]
	if templateTag == nil {
		return nil
	}
	if _, ok := templateTag.attrs["link"]; !ok {
		return nil
	}
	effectiveAttrValue, ok := tp.effectiveAttrValue(node, "link")
	if !ok {
		return nil
	}
	if tp.effectiveParentName(node) != "paragraph" {
		if tp.options.Strict {
			return tp.nodeError(node, "link attribute is only valid on a paragraph text chunk")
		}
		return nil
	}
	if _, err := tp.resolveLink(effectiveAttrValue); err != nil {
		if validateLink := tp.strictErr(node, "link", effectiveAttrValue, err); validateLink != nil {
			return validateLink
		}
	}
	return nil
}

func (tp *templateProcessor) nodeError(node *templateNode, format string, a ...any) error {
	return fmt.Errorf("%s", tp.getNodeErrorLocation(node, format, a...))
}

func (tp *templateProcessor) parseChart(node *templateNode) (any, error) {
	sourceName, _ := tp.effectiveAttrValue(node, "src")
	if sourceName == "" {
		return nil, tp.nodeError(node, "chart `src` attribute cannot be empty")
	}
	chart, ok := tp.options.ChartMap[sourceName]
	if !ok {
		return nil, tp.nodeError(node, "could not find chart resource: `%s`", sourceName)
	}
	chart2 := NewChart(chart)
	if err := tp.applyAttrs(node, chart2, chartAttrs); err != nil {
		return nil, err
	}
	return chart2, nil
}

func (tp *templateProcessor) collectStyles(node *templateNode) error {
	tp.classStyles = map[string][]xml.Attr{}
	return tp.collectStylesNode(node)
}

func (tp *templateProcessor) tableColumns(node *templateNode) (int64, error) {
	var tableColumns int64
	if effectiveAttrValue, ok := tp.effectiveAttrValue(node, "columns"); ok {
		int642, err := parseInt64(effectiveAttrValue)
		if err != nil {
			if err2 := tp.strictErr(node, "columns", effectiveAttrValue, err); err2 != nil {
				return 0, err2
			}
			int642 = 0
		}
		tableColumns = int642
	}
	if tableColumns <= 0 {
		if tp.options.Strict {
			return 0, tp.nodeError(node, "table columns must be a positive number, got %d", tableColumns)
		}
		tp.nodeLogDebug(node, "Invalid number of table columns: %d. Setting to 1. Output may be incorrect.", tableColumns)
		tableColumns = 1
	}
	return tableColumns, nil
}

func (tp *templateProcessor) render() error { return tp.process(false) }

func (tp *templateProcessor) renderNode(node *templateNode) error {
	value := node.value
	if value == nil {
		return nil
	}
	local := node.start.Name.Local
	if _, ok := templateTags[local]; !ok {
		tp.nodeLogDebug(node, "Invalid tag <%s>. Skipping.", local)
		return nil
	}
	var renderer any
	if componentParent := node.componentParent(); componentParent != nil {
		renderer = componentParent.value
	} else {
		renderer = tp.renderer
	}
	switch renderer2 := renderer.(type) {
	case componentRenderer:
		if background, ok := value.(*Background); ok {
			if block, ok := renderer2.(*Block); ok {
				block.SetBackground(background)
			}
			return nil
		}
		drawable, ok := value.(Drawable)
		if !ok {
			tp.nodeLogError(node, "Tag <%s> (%T) is not a drawable.", local, value)
			return errInvalidChild
		}
		err := renderer2.Draw(drawable)
		if err != nil {
			return tp.nodeError(node, "Cannot draws tag <%s> (%T): %s.", local, value, err)
		}
	case *Division:
		switch value2 := value.(type) {
		case *Background:
			renderer2.SetBackground(value2)
		case VectorDrawable:
			err := renderer2.Add(value2)
			if err != nil {
				return tp.nodeError(node, "Cannot add tag <%s> (%T) into a Division: %s.", local, value, err)
			}
		}
	case *TableCell:
		drawable, ok := value.(VectorDrawable)
		if !ok {
			tp.nodeLogError(node, "Tag <%s> (%T) is not a vector drawable.", local, value)
			return errInvalidChild
		}
		err := renderer2.SetContent(drawable)
		if err != nil {
			return tp.nodeError(node, "Cannot add tag <%s> (%T) into a table cell: %s.", local, value, err)
		}
	case *StyledParagraph:
		chunk, ok := value.(*TextChunk)
		if !ok {
			tp.nodeLogError(node, "Tag <%s> (%T) is not a text chunk.", local, value)
			return errInvalidChild
		}
		renderer2.appendChunk(chunk)
	case *Chapter:
		switch value2 := value.(type) {
		case *Chapter:
			return nil
		case *Background:
			renderer2.SetBackground(value2)
		case *StyledParagraph:
			if node.start.Name.Local == "chapter-heading" {
				return nil
			}
			err := renderer2.Add(value2)
			if err != nil {
				return tp.nodeError(node, "Cannot add tag <%s> (%T) into a Chapter: %s.", local, value, err)
			}
		case Drawable:
			err := renderer2.Add(value2)
			if err != nil {
				return tp.nodeError(node, "Cannot add tag <%s> (%T) into a Chapter: %s.", local, value, err)
			}
		}
	case *List:
		switch value2 := value.(type) {
		case *TextChunk:
		case *listItem:
			renderer2.items = append(renderer2.items, value2)
		default:
			tp.nodeLogError(node, "Tag <%s> (%T) is not a list item. Skipping.", local, value)
		}
	case *listItem:
		switch value2 := value.(type) {
		case *TextChunk:
		case *StyledParagraph:
			renderer2.content = value2
		case *List:
			if value2.defaultIndent {
				value2.indent = 15
			}
			renderer2.content = value2
		case *Image:
			renderer2.content = value2
		case *Division:
			renderer2.content = value2
		case *Table:
			renderer2.content = value2
		default:
			tp.nodeLogError(node, "Tag <%s> (%T) is not supported in a list.", local, value)
			return errInvalidChild
		}
	}
	return nil
}

func (tp *templateProcessor) nodeOwnStyle(node *templateNode) map[string]string {
	var m map[string]string
	fn := func(s2, s string) {
		if _, ok := carrierAttrs[s2]; !ok {
			return
		}
		if s2 != "color" || !isGradientExpr(s) {
			if err := tp.typographyValueErr(s2, s); err != nil {
				tp.nodeLogDebug(node, "Ignoring invalid inherited %s value %q: %v.", s2, s, err)
				return
			}
		}
		if m == nil {
			m = map[string]string{}
		}
		m[s2] = s
	}
	for _, s := range node.classNames() {
		for _, attr := range tp.classStyles[s] {
			fn(attr.Name.Local, attr.Value)
		}
	}
	for _, attr := range node.start.Attr {
		if attr.Name.Space != "" {
			continue
		}
		fn(attr.Name.Local, attr.Value)
	}
	return m
}

func (tp *templateProcessor) parseListMarker(node *templateNode) (any, error) {
	componentParent := node.componentParent()
	if componentParent == nil {
		tp.nodeLogError(node, "List marker parent cannot be nil.")
		return nil, errInvalidParent
	}
	var marker *TextChunk
	switch value := componentParent.value.(type) {
	case *List:
		marker = &value.marker
	case *listItem:
		marker = &value.marker
	default:
		tp.nodeLogError(node, "%v is not a valid parent node for list marker.", value)
		return nil, errInvalidParent
	}
	if _, err := tp.parseTextChunk(node, marker); err != nil {
		if tp.options.Strict {
			return nil, err
		}
		tp.nodeLogError(node, "Could not parse list marker: `%v`.", err)
		return nil, nil
	}
	return marker, nil
}

func (tp *templateProcessor) typographyValueErr(s2, s string) error {
	switch s2 {
	case "font-size", "line-height":
		_, err := parseFloat64(s)
		return err
	case "color":
		_, err := tp.resolveColor(s)
		return err
	case "font":
		return tp.validateFontValue(s)
	case "text-align":
		if _, ok := textAlignmentValues[s]; !ok {
			return fmt.Errorf("invalid value %q", s)
		}
	}
	return nil
}
