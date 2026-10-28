package pdfua

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/xmputil"
)

func classifyStructType(elem *structElem, roleMap *core.PdfObjectDictionary, known map[string]bool) structureTypeFindings {
	var findings structureTypeFindings
	findings.circular = known[elem.typeName] || hasCircularNSMapping(elem.dict)
	if mapped, _ := resolveNamespacedType(elem.dict, roleMap); mapped != "" {
		return findings
	}
	if isStandardType(elem.typeName, elemNamespace(elem.dict)) {
		findings.remappedStandard = true
	} else {
		findings.unknownNamespace = !findings.circular
	}
	return findings
}

func classMapOf(root *core.PdfObjectDictionary) *core.PdfObjectDictionary {
	classMap, _ := core.GetDict(core.TraceToDirectObject(root.Get("ClassMap")))
	return classMap
}

const (
	annotFlagHidden = 1 << 1
	annotFlagNoView = 1 << 5
)

func catalogVersion(r *model.CompliancePdfReader) (core.Version, bool) {
	verObj, ok := r.GetCatalogVersion()
	if !ok {
		return core.Version{}, false
	}
	name, ok := core.GetName(core.TraceToDirectObject(verObj))
	if !ok {
		return core.Version{}, false
	}
	parts := strings.SplitN(string(*name), ".", 2)
	if len(parts) != 2 {
		return core.Version{}, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return core.Version{}, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return core.Version{}, false
	}
	return core.Version{Major: major, Minor: minor}, true
}

type catalogXMP struct {
	present bool
	doc     *xmputil.Document
}

func loadCatalogXMP(r *model.CompliancePdfReader) catalogXMP {
	metaObj, ok := r.GetCatalogMetadata()
	if !ok {
		return catalogXMP{}
	}
	stream, ok := core.GetStream(metaObj)
	if !ok {
		return catalogXMP{}
	}
	if typ, ok := core.GetName(stream.Get("Type")); !ok || string(*typ) != "Metadata" {
		return catalogXMP{}
	}
	if subtype, ok := core.GetName(stream.Get("Subtype")); !ok || string(*subtype) != "XML" {
		return catalogXMP{}
	}
	doc, err := xmputil.LoadDocument(streamData(stream))
	if err != nil {
		return catalogXMP{present: true}
	}
	return catalogXMP{present: true, doc: doc}
}

func applyProfile2Defaults(opts *Profile2Options) {
	if opts.Xmp.MarshalIndent == "" {
		opts.Xmp.MarshalIndent = "\t"
	}
}

func collectFontDicts(r *model.CompliancePdfReader) []*core.PdfObjectDictionary {
	cands := &fontCandidates{seenFonts: map[core.PdfObject]struct{}{}, activeStreams: map[*core.PdfObjectStream]struct{}{}}
	numPages, err := r.GetNumPages()
	if err != nil {
		return nil
	}
	for pageNum := 1; pageNum <= numPages; pageNum++ {
		page, err := r.GetPage(pageNum)
		if err != nil {
			continue
		}
		if page.Resources != nil {
			res := core.MakeDict()
			if page.Resources.Font != nil {
				res.Set("Font", page.Resources.Font)
			}
			if page.Resources.XObject != nil {
				res.Set("XObject", page.Resources.XObject)
			}
			if page.Resources.Pattern != nil {
				res.Set("Pattern", page.Resources.Pattern)
			}
			cands.addResources(res, 0)
		}
		streams, _ := annotAppearanceStreams(page)
		for _, stream := range streams {
			cands.addResources(streamResources(stream), 0)
		}
	}
	return cands.fonts
}

func hasCircularNSMapping(elem *core.PdfObjectDictionary) bool {
	name, ok := core.GetName(elem.Get("S"))
	if !ok {
		return false
	}
	typ := string(*name)
	ns, _ := core.GetDict(core.TraceToDirectObject(elem.Get("NS")))
	key := func(typ string, nsDict *core.PdfObjectDictionary) string { return typ + "\x00" + nsURI(nsDict) }
	seen := map[string]bool{key(typ, ns): true}
	typeByNS := map[string]string{}
	nextTyp, nextNS, ok := mapNamespacedType(typ, ns, nil)
	for i := 0; ok && i < maxRoleMapHops; i++ {
		if curURI := nsURI(ns); curURI != "" {
			typeByNS[curURI] = typ
		}
		if nextURI := nsURI(nextNS); nextURI != "" {
			if prev, ok := typeByNS[nextURI]; ok && prev == nextTyp {
				return true
			}
		}
		if seen[key(nextTyp, nextNS)] {
			return false
		}
		seen[key(nextTyp, nextNS)] = true
		typ, ns = nextTyp, nextNS
		nextTyp, nextNS, ok = mapNamespacedType(typ, ns, nil)
	}
	return false
}

type annotStructInfo struct {
	elemType string
	hasAlt   bool
}

func buildTableGrid(tableElem, classMap *core.PdfObjectDictionary, resolve structTypeFunc, checkGroups bool) (*tableGrid, []ViolatedRule) {
	tr := collectTableRows(tableElem, resolve)
	numRows := sumRowSpans(tr.rows, classMap)
	if numRows == 0 {
		return nil, nil
	}
	numCols := sumColSpans(tr.rows[0], classMap)
	if numCols != 0 && numRows > maxGridCells/numCols {
		return nil, []ViolatedRule{newViolation("8.2.5.26", fmt.Sprintf("table too large to check for regularity (grid exceeds %d cells); not validated", maxGridCells))}
	}
	grid := &tableGrid{rows: numRows, cols: numCols, cells: make([]*core.PdfObjectDictionary, numRows*numCols)}
	setCell := func(r, c int, cell *core.PdfObjectDictionary) {
		if r < numRows && c < numCols {
			grid.cells[r*numCols+c] = cell
		}
	}
	for r, rowCells := range tr.rows {
		c := 0
		for _, cell := range rowCells {
			rowSpan := spanAttr(cell, classMap, "RowSpan")
			colSpan := spanAttr(cell, classMap, "ColSpan")
			for c < numCols && grid.at(r, c) != nil {
				c++
			}
			if c+colSpan > numCols {
				if numCols == 0 {
					return nil, []ViolatedRule{newViolation("8.2.5.26-3", fmt.Sprintf("table row %d has cells but the first row declares no columns", r+1))}
				}
				return nil, []ViolatedRule{newViolation("8.2.5.26-3", fmt.Sprintf("table row %d spans more than the %d columns of the first row", r+1, numCols))}
			}
			if r+rowSpan > numRows {
				return nil, []ViolatedRule{newViolation("8.2.5.26-2", fmt.Sprintf("a cell in table column %d spans past the last row of the table", c+1))}
			}
			if checkGroups && rowSpan > 1 {
				for _, bound := range tr.groupBounds {
					if r < bound && r+rowSpan > bound {
						return nil, []ViolatedRule{newViolation("8.2.5.26-2", fmt.Sprintf("a cell in table column %d spans across a THead/TBody/TFoot row grouping boundary", c+1))}
					}
				}
			}
			for dr := 0; dr < rowSpan; dr++ {
				for dc := 0; dc < colSpan; dc++ {
					if grid.at(r+dr, c+dc) != nil {
						return nil, []ViolatedRule{newViolation("8.2.5.26-1", "a table cell intersects another cell")}
					}
					setCell(r+dr, c+dc, cell)
				}
			}
			grid.placements = append(grid.placements, tableCellPlacement{cell: cell, row: r, col: c, rowSpan: rowSpan, colSpan: colSpan})
			c += colSpan
		}
		if len(rowCells) == 0 && numCols > 0 {
			return nil, []ViolatedRule{newViolation("8.2.5.26-4", fmt.Sprintf("table row %d contains no cells; the first row spans %d columns", r+1, numCols))}
		}
	}
	for r := 0; r < numRows; r++ {
		filled := 0
		for c := 0; c < numCols; c++ {
			if grid.cells[r*numCols+c] != nil {
				filled++
			}
		}
		if filled != numCols {
			return nil, []ViolatedRule{newViolation("8.2.5.26-4", fmt.Sprintf("table row %d spans %d columns, but the first row spans %d", r+1, filled, numCols))}
		}
	}
	return grid, nil
}

func collectTableRows(tableElem *core.PdfObjectDictionary, resolve structTypeFunc) tableRows {
	cellsOf := func(row *core.PdfObjectDictionary) []*core.PdfObjectDictionary {
		var cells []*core.PdfObjectDictionary
		for _, kid := range structChildren(row.Get("K"), resolve) {
			switch resolve(kid) {
			case "TH", "TD":
				cells = append(cells, kid)
			}
		}
		return cells
	}
	var tr tableRows
	for _, kid := range structChildren(tableElem.Get("K"), resolve) {
		switch resolve(kid) {
		case "TR":
			tr.rows = append(tr.rows, cellsOf(kid))
		case "THead", "TBody", "TFoot":
			tr.groupBounds = append(tr.groupBounds, len(tr.rows))
			for _, rowKid := range structChildren(kid.Get("K"), resolve) {
				if resolve(rowKid) != "TR" {
					continue
				}
				tr.rows = append(tr.rows, cellsOf(rowKid))
			}
			tr.groupBounds = append(tr.groupBounds, len(tr.rows))
		}
	}
	return tr
}

func hasAdobeCIDOrdering(font *core.PdfObjectDictionary) bool {
	descendants, ok := core.GetArray(font.Get("DescendantFonts"))
	if !ok || descendants.Len() == 0 {
		return false
	}
	cidFont, ok := core.GetDict(descendants.Get(0))
	if !ok {
		return false
	}
	sysInfo, ok := core.GetDict(core.TraceToDirectObject(cidFont.Get("CIDSystemInfo")))
	if !ok {
		return false
	}
	registry, okReg := core.GetString(sysInfo.Get("Registry"))
	ordering, okOrd := core.GetString(sysInfo.Get("Ordering"))
	return okReg && registry.Decoded() == "Adobe" && okOrd && adobeCJKOrderings[ordering.Decoded()]
}

func collectAnnotStructInfo(root *core.PdfObjectDictionary) map[int64]annotStructInfo {
	info := map[int64]annotStructInfo{}
	walkStructTree(root, newRoleMapResolver(root), func(elem *structElem) {
		hasAlt := false
		if alt, ok := core.GetString(core.TraceToDirectObject(elem.dict.Get("Alt"))); ok && alt.Decoded() != "" {
			hasAlt = true
		}
		forEachOBJR(elem.dict, func(obj core.PdfObject) {
			objNum, ok := objectNumber(obj)
			if !ok {
				return
			}
			entry := annotStructInfo{elemType: elem.stdType, hasAlt: hasAlt}
			if prev, ok := info[objNum]; ok {
				entry.hasAlt = entry.hasAlt || prev.hasAlt
				if prev.elemType == "Link" || prev.elemType == "Reference" {
					entry.elemType = prev.elemType
				}
			}
			info[objNum] = entry
		})
	})
	return info
}

func annotAppearanceStreams(page *model.PdfPage) ([]*core.PdfObjectStream, bool) {
	if page.Annots == nil {
		return nil, true
	}
	annots, ok := core.GetArray(page.Annots)
	if !ok {
		return nil, false
	}
	var streams []*core.PdfObjectStream
	for _, item := range annots.Elements() {
		dict, ok := core.GetDict(item)
		if !ok {
			continue
		}
		streams = append(streams, appearanceStreams(dict)...)
	}
	return streams, true
}

func cellScope(elem, classMap *core.PdfObjectDictionary) string {
	obj := lookupAttr(elem, classMap, "Scope", "Table", isName)
	if obj == nil {
		return ""
	}
	name, _ := core.GetName(obj)
	return string(*name)
}

var adobeCJKOrderings = map[string]bool{"Japan1": true, "GB1": true, "CNS1": true, "Korea1": true, "KR": true}

func applyProfile1Defaults(opts *Profile1Options) {
	if opts.Xmp.MarshalIndent == "" {
		opts.Xmp.MarshalIndent = "\t"
	}
}

func charProcStreams(font *core.PdfObjectDictionary) []*core.PdfObjectStream {
	procs, ok := core.GetDict(core.TraceToDirectObject(font.Get("CharProcs")))
	if !ok {
		return nil
	}
	var streams []*core.PdfObjectStream
	for _, key := range procs.Keys() {
		if stream, ok := core.GetStream(procs.Get(key)); ok {
			streams = append(streams, stream)
		}
	}
	return streams
}

func appearanceStreams(annot *core.PdfObjectDictionary) []*core.PdfObjectStream {
	ap, ok := core.GetDict(core.TraceToDirectObject(annot.Get("AP")))
	if !ok {
		return nil
	}
	var streams []*core.PdfObjectStream
	for _, key := range []core.PdfObjectName{"N", "D", "R"} {
		streams = append(streams, streamsFromObject(ap.Get(key))...)
	}
	return streams
}

func annotPageMap(r *model.CompliancePdfReader) map[int64]*model.PdfPage {
	pages := map[int64]*model.PdfPage{}
	numPages, err := r.GetNumPages()
	if err != nil {
		return pages
	}
	for pageNum := 1; pageNum <= numPages; pageNum++ {
		page, err := r.GetPage(pageNum)
		if err != nil {
			continue
		}
		annots, err := page.GetAnnotations()
		if err != nil {
			continue
		}
		for _, annot := range annots {
			if objNum, ok := objectNumber(annot.GetContainingPdfObject()); ok {
				pages[objNum] = page
			}
		}
	}
	return pages
}

func containsLinkOBJR(elem *core.PdfObjectDictionary) bool {
	isLinkOBJR := func(obj core.PdfObject) bool {
		d, ok := core.GetDict(core.TraceToDirectObject(obj))
		if !ok {
			return false
		}
		typ, ok := core.GetName(d.Get("Type"))
		if !ok || string(*typ) != "OBJR" {
			return false
		}
		target, ok := core.GetDict(core.TraceToDirectObject(d.Get("Obj")))
		if !ok {
			return false
		}
		subtype, ok := core.GetName(target.Get("Subtype"))
		return ok && string(*subtype) == "Link"
	}
	kids := core.TraceToDirectObject(elem.Get("K"))
	if arr, ok := core.GetArray(kids); ok {
		return slices.ContainsFunc(arr.Elements(), isLinkOBJR)
	}
	return isLinkOBJR(kids)
}
