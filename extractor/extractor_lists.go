package extractor

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

type lists []*list

func collectListItems(listV *list) []*list {
	var items []*list
	for _, child := range listV.children {
		switch child.kind {
		case "LI":
			lines := linesOfListItem(child)
			subItems := collectListItems(child)
			item := newList(lines, "bullet", subItems)
			text := joinLinesText(lines, "")
			item.text = text
			items = append(items, item)
		case "LBody":
			return collectListItems(child)
		case "L":
			nested := collectListItems(child)
			items = append(items, nested...)
			return items
		}
	}
	return items
}

func listsFromStructTree(kvs []*model.KValue, linesByMCID map[int][]*textLine, pageObj core.PdfObject) []*list {
	items := []*list{}
	for _, kv := range kvs {
		dict := kv.GetKDict()
		kvs2 := dict.GetChildren()
		lines := []*textLine{}
		subItems := []*list{}
		kind := dict.S.(*core.PdfObjectName).String()
		firstChild := kvs2[0]
		n := firstChild.GetMCID()
		if len(kvs2) == 1 && n != nil && *n != -1 {
			if dict.Pg == pageObj {
				lines = linesByMCID[*n]
			}
		} else {
			subItems = listsFromStructTree(kvs2, linesByMCID, pageObj)
		}
		listV := newList(lines, kind, subItems)
		items = append(items, listV)
	}
	return items
}

type list struct {
	lines    []*textLine
	kind     string
	children []*list
	text     string
}

func groupLinesByMCID(paras *paraList) map[int][]*textLine {
	byMCID := map[int][]*textLine{}
	for _, para := range *paras {
		for _, line := range para.lines {
			if !hasSingleMCID(line) {
				common.Log.Debug("groupLines: The text line contains more than one mcid number. It should be split.")
				continue
			}
			mcid := line.words[0].marks[0].mcid
			byMCID[mcid] = append(byMCID[mcid], line)
		}
		if para.table != nil {
			cells := para.table.cells
			for _, cell := range cells {
				for _, line := range cell.lines {
					if !hasSingleMCID(line) {
						common.Log.Debug("groupLines: The text line contains more than one mcid number. It should be split.")
						continue
					}
					mcid := line.words[0].marks[0].mcid
					byMCID[mcid] = append(byMCID[mcid], line)
				}
			}
		}
	}
	return byMCID
}

func buildList(root *model.StructTreeRoot, linesByMCID map[int][]*textLine, pageObj core.PdfObject) []*list {
	if root == nil {
		common.Log.Debug("buildList: treeRoot is nil")
		return nil
	}
	kvs := []*model.KValue{}
	var walk func(kv *model.KValue)
	walk = func(kv *model.KValue) {
		if kd := kv.GetKDict(); kd != nil {
			if kd.S.String() == "L" {
				kvs = append(kvs, kv)
			} else {
				for _, child := range kd.GetChildren() {
					walk(child)
				}
			}
		}
	}
	for _, kd := range root.K {
		rootKV := model.NewKValue()
		rootKV.SetKDict(kd)
		walk(rootKV)
	}
	top := listsFromStructTree(kvs, linesByMCID, pageObj)
	var out []*list
	for _, l := range top {
		items := collectListItems(l)
		out = append(out, items...)
	}
	return out
}

func linesWithBullets(lines []*textLine) []*textLine {
	out := []*textLine{}
	for _, line := range lines {
		text := line.text()
		match := listBulletRegexp.Find([]byte(text))
		if match != nil {
			out = append(out, line)
		}
	}
	return out
}

func joinLinesText(lines []*textLine, prefix string) string {
	var builder strings.Builder
	nextDepth := 0.0
	for i, line := range lines {
		text := line.text()
		depth := line.depth
		if i < len(lines)-1 {
			nextDepth = lines[i+1].depth
		} else {
			nextDepth = 0.0
		}
		builder.WriteString(prefix)
		builder.WriteString(text)
		if nextDepth != depth {
			builder.WriteString("\n")
		} else {
			builder.WriteString(" ")
		}
	}
	return builder.String()
}

func writeListText(listV *list, builder *strings.Builder, indent *string) {
	s := indentListText(listV, indent)
	builder.WriteString(s)
	for _, children := range listV.children {
		childIndent := *indent + "   "
		writeListText(children, builder, &childIndent)
	}
}

func buildListsFromLines(lines []*textLine, byStartX map[float64][]*textLine) []*list {
	depths := sortedLineDepths(byStartX)
	items := []*list{}
	if len(depths) == 0 {
		return items
	}
	depth2 := depths[0]
	level := 1
	topLines := byStartX[depth2]
	for i, line := range topLines {
		var limit float64
		children := []*list{}
		depth := line.depth
		nextDepth := -1.0
		if i < len(topLines)-1 {
			nextDepth = topLines[i+1].depth
		}
		if level < len(depths) {
			children = buildChildLists(lines, byStartX, depths, level, depth, nextDepth)
		}
		limit = nextDepth
		if len(children) > 0 {
			firstChild := children[0]
			if len(firstChild.lines) > 0 {
				limit = firstChild.lines[0].depth
			}
		}
		itemLines := []*textLine{line}
		cont := bulletContinuationLines(line, lines, depth, limit)
		itemLines = append(itemLines, cont...)
		item := newList(itemLines, "bullet", children)
		item.text = joinLinesText(itemLines, "")
		items = append(items, item)
	}
	return items
}

func newList(lines []*textLine, kind string, children []*list) *list {
	return &list{lines: lines, kind: kind, children: children}
}

func bulletContinuationLines(line *textLine, lines []*textLine, startDepth, endDepth float64) []*textLine {
	out := []*textLine{}
	for _, l := range lines {
		if l.depth >= startDepth {
			if endDepth != -1 && l.depth < endDepth {
				if l.text() != line.text() {
					if math.Round(l.Llx) < math.Round(line.Llx) {
						break
					}
					out = append(out, l)
				}
			} else if endDepth == -1 {
				if l.depth == line.depth {
					if l.text() != line.text() {
						out = append(out, l)
					}
					continue
				}
				nextDepth := nextLineDepth(line, lines)
				if nextDepth != -1 && l.depth <= nextDepth {
					out = append(out, l)
				}
			}
		}
	}
	return out
}

// Text returns the text content of the `bulletLists`.
func (l *lists) Text() string {
	builder := &strings.Builder{}
	for _, listV := range *l {
		text := listV.Text()
		builder.WriteString(text)
	}
	return builder.String()
}

func startsWithBulletByte(b byte) bool {
	for _, bc := range bulletChars {
		if []byte(bc)[0] == b {
			return true
		}
	}
	return false
}

func endsWithHyphen(s string) bool {
	if utf8.RuneCountInString(s) < minHyphenatedLen {
		return false
	}
	last, size := utf8.DecodeLastRuneInString(s)
	if size <= 0 || !unicode.Is(unicode.Hyphen, last) {
		return false
	}
	last, size = utf8.DecodeLastRuneInString(s[:len(s)-size])
	return size > 0 && !unicode.IsSpace(last)
}

func hasSingleMCID(line *textLine) bool {
	single := true
	firstMCID := -1
	for _, word := range line.words {
		for _, mark := range word.marks {
			mcid := mark.mcid
			if firstMCID == -1 {
				firstMCID = mcid
			} else {
				if firstMCID != mcid {
					single = false
					break
				}
			}
		}
	}
	return single
}

func indentListText(listV *list, indent *string) string {
	lines := strings.Split(listV.text, "\n")
	builder := &strings.Builder{}
	for _, line := range lines {
		if line != "" {
			builder.WriteString(*indent)
			builder.WriteString(line)
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func linesOfListItem(listV *list) []*textLine {
	for _, children := range listV.children {
		switch children.kind {
		case "LBody":
			if len(children.lines) != 0 {
				return children.lines
			}
			return linesOfListItem(children)
		case "Span":
			return children.lines
		case "InlineShape":
			return children.lines
		}
	}
	return nil
}

// Text gets the extracted text contained in `l`.
func (l *list) Text() string {
	b := &strings.Builder{}
	indent := ""
	writeListText(l, b, &indent)
	return b.String()
}
