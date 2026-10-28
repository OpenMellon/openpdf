package pdfua

import (
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func (u *fontUsage) walkOps(ops *contentstream.ContentStreamOperations, scope resourceScope, depth int) {
	for _, op := range *ops {
		if len(op.Params) == 0 {
			continue
		}
		switch op.Operand {
		case "Tf":
			if name, ok := core.GetName(op.Params[0]); ok {
				u.selectFont(*name, scope, depth)
			}
		case "Do":
			if name, ok := core.GetName(op.Params[0]); ok {
				u.walkForm(*name, scope, depth)
			}
		case "scn", "SCN":
			if name, ok := core.GetName(op.Params[len(op.Params)-1]); ok {
				u.walkPattern(*name, scope, depth)
			}
		}
	}
}

func (u *fontUsage) budget(depth int) bool {
	if depth >= maxResourceDepth || u.visits >= maxStreamVisits {
		u.determined = false
		return false
	}
	u.visits++
	return true
}

func (c *fontCandidates) addResources(res *core.PdfObjectDictionary, depth int) {
	if res == nil || depth > maxResourceDepth {
		return
	}
	if fonts, ok := core.GetDict(res.Get("Font")); ok {
		for _, key := range fonts.Keys() {
			fontObj := core.TraceToDirectObject(fonts.Get(key))
			if _, seen := c.seenFonts[fontObj]; seen {
				continue
			}
			c.seenFonts[fontObj] = struct{}{}
			fontDict, ok := core.GetDict(fontObj)
			if !ok {
				continue
			}
			c.fonts = append(c.fonts, fontDict)
			if !c.addType3Resources(fontDict, depth) {
				return
			}
		}
	}
	for _, pattern := range streamsInDict(res.Get("Pattern")) {
		if !c.descend(pattern, depth) {
			return
		}
	}
	xobjects, ok := core.GetDict(res.Get("XObject"))
	if !ok {
		return
	}
	for _, key := range xobjects.Keys() {
		stream, ok := core.GetStream(xobjects.Get(key))
		if !ok {
			continue
		}
		if subtype, ok := core.GetName(stream.Get("Subtype")); !ok || string(*subtype) != "Form" {
			continue
		}
		if !c.descend(stream, depth) {
			return
		}
	}
}

func (u *fontUsage) walkPattern(name core.PdfObjectName, scope resourceScope, depth int) {
	for _, res := range scope.lookIn() {
		if res == nil {
			continue
		}
		patObj := dictGet(res.Pattern, name)
		if patObj == nil {
			continue
		}
		stream, ok := core.GetStream(patObj)
		if !ok {
			return
		}
		key := walkKey{obj: stream, pageRes: scope.page}
		if u.visited[key] {
			return
		}
		if !u.budget(depth) {
			return
		}
		u.visited[key] = true
		u.walkStream(stream, resourceScope{page: scope.page}, depth+1)
		return
	}
}

func (u *fontUsage) walkStream(stream *core.PdfObjectStream, scope resourceScope, depth int) {
	data, err := core.DecodeStream(stream)
	if err != nil {
		u.determined = false
		return
	}
	u.walkContent(data, scope.with(resourcesFromDict(streamResources(stream))), depth)
}

func (c *fontCandidates) descend(stream *core.PdfObjectStream, depth int) bool {
	if _, seen := c.activeStreams[stream]; seen {
		return true
	}
	if c.visits >= maxStreamVisits {
		return false
	}
	c.visits++
	c.activeStreams[stream] = struct{}{}
	c.addResources(streamResources(stream), depth+1)
	delete(c.activeStreams, stream)
	return true
}

type fontUsage struct {
	used        map[*core.PdfObjectDictionary]bool
	determined  bool
	activeForms map[*core.PdfObjectStream]bool
	visited     map[walkKey]bool
	visits      int
}

type fontCandidates struct {
	fonts         []*core.PdfObjectDictionary
	seenFonts     map[core.PdfObject]struct{}
	activeStreams map[*core.PdfObjectStream]struct{}
	visits        int
}

func (u *fontUsage) selectFont(name core.PdfObjectName, scope resourceScope, depth int) {
	for _, res := range scope.lookIn() {
		if res == nil {
			continue
		}
		fontObj, ok := res.GetFontByName(name)
		if !ok {
			continue
		}
		fontDict, isDict := core.GetDict(core.TraceToDirectObject(fontObj))
		if !isDict {
			return
		}
		u.used[fontDict] = true
		u.walkCharProcs(fontDict, scope, depth)
		return
	}
}

func (u *fontUsage) walkForm(name core.PdfObjectName, scope resourceScope, depth int) {
	for _, res := range scope.lookIn() {
		if res == nil || dictGet(res.XObject, name) == nil {
			continue
		}
		stream, xobjType := res.GetXObjectByName(name)
		if xobjType != model.XObjectTypeForm {
			return
		}
		if u.activeForms[stream] {
			return
		}
		if !u.budget(depth) {
			return
		}
		form, err := res.GetXObjectFormByName(name)
		if err != nil {
			u.determined = false
			return
		}
		data, contentErr := form.GetContentStream()
		if contentErr != nil {
			u.determined = false
			return
		}
		u.activeForms[stream] = true
		u.walkContent(data, scope.with(form.Resources), depth+1)
		delete(u.activeForms, stream)
		return
	}
}

func (u *fontUsage) addPage(page *model.PdfPage, ops *contentstream.ContentStreamOperations) {
	scope := resourceScope{page: page.Resources}
	if ops == nil {
		u.determined = false
	} else {
		u.walkOps(ops, scope, 0)
	}
	streams, ok := annotAppearanceStreams(page)
	if !ok {
		u.determined = false
		return
	}
	for _, stream := range streams {
		u.walkStream(stream, scope, 0)
	}
}

func (c *fontCandidates) addType3Resources(font *core.PdfObjectDictionary, depth int) bool {
	if subtype, ok := core.GetName(font.Get("Subtype")); !ok || string(*subtype) != "Type3" {
		return true
	}
	if res, ok := core.GetDict(core.TraceToDirectObject(font.Get("Resources"))); ok {
		c.addResources(res, depth+1)
	}
	counted := false
	for _, proc := range charProcStreams(font) {
		if _, seen := c.activeStreams[proc]; seen {
			continue
		}
		if !counted {
			if c.visits >= maxStreamVisits {
				return false
			}
			c.visits++
			counted = true
		}
		c.activeStreams[proc] = struct{}{}
		c.addResources(streamResources(proc), depth+1)
		delete(c.activeStreams, proc)
	}
	return true
}

func newFontUsage() *fontUsage {
	return &fontUsage{used: map[*core.PdfObjectDictionary]bool{}, determined: true, activeForms: map[*core.PdfObjectStream]bool{}, visited: map[walkKey]bool{}}
}

func (u *fontUsage) walkContent(data []byte, scope resourceScope, depth int) {
	ops, err := contentstream.NewContentStreamParser(string(data)).Parse()
	if err != nil {
		u.determined = false
		return
	}
	u.walkOps(ops, scope, depth)
}

func (u *fontUsage) walkCharProcs(font *core.PdfObjectDictionary, scope resourceScope, depth int) {
	if subtype, ok := core.GetName(font.Get("Subtype")); !ok || string(*subtype) != "Type3" {
		return
	}
	procs := charProcStreams(font)
	if len(procs) == 0 {
		return
	}
	key := walkKey{obj: font, pageRes: scope.page}
	if u.visited[key] {
		return
	}
	if !u.budget(depth) {
		return
	}
	u.visited[key] = true
	resDict, _ := core.GetDict(core.TraceToDirectObject(font.Get("Resources")))
	procScope := resourceScope{local: resourcesFromDict(resDict), page: scope.page}
	for _, proc := range procs {
		u.walkStream(proc, procScope, depth+1)
	}
}
