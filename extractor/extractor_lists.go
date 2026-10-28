package extractor

import (
	"regexp"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

type list struct {
	lines    []*textLine
	kind     string
	children []*list
	text     string
}

var listBulletRegexp *regexp.Regexp = regexp.MustCompile(romanNumeralPattern + "|" + enumeratorPattern)

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

func writeListText(listV *list, builder *strings.Builder, indent *string) {
	s := indentListText(listV, indent)
	builder.WriteString(s)
	for _, children := range listV.children {
		childIndent := *indent + "   "
		writeListText(children, builder, &childIndent)
	}
}

func newList(lines []*textLine, kind string, children []*list) *list {
	return &list{lines: lines, kind: kind, children: children}
}

// Text gets the extracted text contained in `l`.
func (l *list) Text() string {
	b := &strings.Builder{}
	indent := ""
	writeListText(l, b, &indent)
	return b.String()
}

type lists []*list

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

// Text returns the text content of the `bulletLists`.
func (l *lists) Text() string {
	builder := &strings.Builder{}
	for _, listV := range *l {
		text := listV.Text()
		builder.WriteString(text)
	}
	return builder.String()
}
