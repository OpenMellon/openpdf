// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/graphic2d"
	"github.com/OpenMellon/openpdf/model"
)

func (gse *GraphicSVGElement) drawText(cc *contentstream.ContentCreator, resources *model.PdfPageResources) {
	cc.Add_BT()
	svgLength, err := parseSVGLength(gse.Attributes["x"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `x` value: %v", err.Error())
	}
	svgLength2, err := parseSVGLength(gse.Attributes["y"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `y` value: %v", err.Error())
	}
	attribute := gse.Attributes["fill"]
	var r, g, b float64
	if rgba, ok := graphic2d.ColorMap[attribute]; ok {
		r2, g2, b2, _ := rgba.RGBA()
		r, g, b = float64(r2), float64(g2), float64(b2)
	} else if strings.HasPrefix(attribute, "rgb(") {
		r, g, b = parseRGBColor(attribute)
	} else {
		r, g, b = ColorRGBFromHex(attribute).ToRGB()
	}
	cc.Add_rg(r, g, b)
	value := defaultSVGFontSize
	if attribute, ok := gse.Attributes["font-size"]; ok {
		value, err = strconv.ParseFloat(attribute, 64)
		if err != nil {
			common.Log.Debug("Error while parsing `font-size` value: %v", err.Error())
			value = defaultSVGFontSize
		}
	}
	fontSize := gse.scale * value * PPI / svgDPI
	keyName := core.PdfObjectName("SFont")
	defaultFont := model.DefaultFont()
	attribute2, ok := gse.Attributes["font-family"]
	if ok {
		if svgStandardFont2, err := svgStandardFont(attribute2); err == nil {
			defaultFont = svgStandardFont2
			i := 1
			for resources.HasFontByName(keyName) {
				keyName = core.PdfObjectName("SFont" + strconv.Itoa(i))
				i++
			}
		}
	}
	value2 := 0.0
	attribute3, ok := gse.Attributes["text-anchor"]
	if ok && attribute3 != "start" {
		var wx float64
		for _, r := range gse.Content {
			runeMetrics, ok := defaultFont.GetRuneMetrics(r)
			if !ok {
				common.Log.Debug("ERROR: unsupported rune %v in font", r)
			}
			wx += runeMetrics.Wx
		}
		wx = wx * fontSize / 1000.0
		switch attribute3 {
		case "middle":
			value2 = -wx / 2
		case "end":
			value2 = -wx
		}
	}
	cc.Add_Tm(1, 0, 0, -1, svgLength*gse.scale+value2, svgLength2*gse.scale)
	resources.SetFontByName(keyName, defaultFont.ToPdfObject())
	cc.Add_Tf(keyName, fontSize)
	content := gse.Content
	str := core.MakeString(content)
	cc.Add_Tj(*str)
	cc.Add_ET()
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

// GetPositioning returns the positioning mode (relative or absolute) of the
// table.
func (t *Table) GetPositioning() Positioning { return t.positioning }

// SetSection sets whether this row is a header row.
func (gr *GridRow) SetSection(role GridRowSection) { gr.section = role }

func (tp *templateProcessor) parseBackground(node *templateNode) (any, error) {
	background := &Background{}
	if err := tp.applyAttrs(node, background, backgroundAttrs); err != nil {
		return nil, err
	}
	return background, nil
}

func parseDivisionTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseDivision(node)
}

// GeneratePageBlocks draws the curve onto page blocks.
func (cu *Curve) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	contentCreator := contentstream.NewContentCreator()
	if cu.tagInfo != nil {
		contentCreator.Add_BDC(*core.MakeName(string(cu.tagInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(cu.tagInfo.Mcid)})
	}
	contentCreator.Add_q().Add_w(cu.width).SetStrokingColor(toPdfColor(cu.color)).Add_m(cu.x1, ctx.PageHeight-cu.y1).Add_v(cu.cx, ctx.PageHeight-cu.cy, cu.x2, ctx.PageHeight-cu.y2).Add_S().Add_Q()
	if cu.tagInfo != nil {
		contentCreator.Add_EMC()
	}
	err := block.addContentsByString(contentCreator.String())
	if err != nil {
		return nil, ctx, err
	}
	return []*Block{block}, ctx, nil
}

// RemoveArtifact removes the artifact associated with the block, if any.
func (b *Block) RemoveArtifact() { b.artifact = nil }

func (sp *StyledParagraph) getTextLineWidth(chunks []*TextChunk) float64 {
	var charSpacing float64
	n := len(chunks)
	for i, chunk := range chunks {
		style := &chunk.Style
		textLen := len(chunk.Text)
		for j, r := range chunk.Text {
			if r == '\u000A' {
				continue
			}
			runeMetrics, ok := style.Font.GetRuneMetrics(r)
			if !ok {
				common.Log.Debug("Rune char metrics not found! %v\n", r)
				return -1
			}
			charSpacing += style.FontSize * runeMetrics.Wx * style.horizontalScale()
			if r != ' ' && (i != n-1 || j != textLen-1) {
				charSpacing += style.CharSpacing * 1000.0
			}
		}
	}
	return charSpacing
}

var PPMM = float64(72 * 1.0 / 25.4)

func (b *Block) mergeBlocks(block *Block) error {
	err := mergeContentStreams(b.contents, b.resources, block.contents, block.resources)
	if err != nil {
		return err
	}
	for _, annotation := range block.annotations {
		b.AddAnnotation(annotation)
	}
	return nil
}

func parseImageTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseImage(node)
}

func allFirstPartsEmpty(rows [][2]*Table) bool {
	for _, tables := range rows {
		if tables[0] != nil {
			return false
		}
	}
	return true
}

func parseSVGPath(s string) (*Path, error) {
	svgCommands = newPathCommands()
	commands, err := parseCommands(tokenizePathData(s))
	if err != nil {
		return nil, err
	}
	return buildPath(commands), nil
}

// Height returns the height of the chart.
func (cha *Chart) Height() float64 { return float64(cha.chart.Height()) }

// SetLineWidth sets the line width.
func (po *Polyline) SetLineWidth(lineWidth float64) { po.polyline.LineWidth = lineWidth }

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

// SetMaxLines sets the maximum number of lines to be drawn.
func (sp *StyledParagraph) SetMaxLines(maxLines int) { sp.maxLines = maxLines }

// GetCoords returns the center coordinates of ellipse (`xc`, `yc`).
func (e *Ellipse) GetCoords() (float64, float64) { return e.xc, e.yc }

func parseTableTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseTable(node)
}

// SetHeight sets the height of the rectangle.
func (r *Rectangle) SetHeight(height float64) { r.height = height }

func (b *Block) setOpacity(val float64, val2 float64) (string, error) {
	if (val < 0 || val >= 1.0) && (val2 < 0 || val2 >= 1.0) {
		return "", nil
	}
	n := 0
	setOpacity := fmt.Sprintf("GS%d", n)
	for b.resources.HasExtGState(core.PdfObjectName(setOpacity)) {
		n++
		setOpacity = fmt.Sprintf("GS%d", n)
	}
	dict := core.MakeDict()
	if val >= 0 && val < 1.0 {
		dict.Set("ca", core.MakeFloat(val))
	}
	if val2 >= 0 && val2 < 1.0 {
		dict.Set("CA", core.MakeFloat(val2))
	}
	err := b.resources.AddExtGState(core.PdfObjectName(setOpacity), dict)
	if err != nil {
		return "", err
	}
	return setOpacity, nil
}

// GetTextVerticalAlignment returns the vertical alignment of the text within
// the bounds of the paragraph.
func (sp *StyledParagraph) GetTextVerticalAlignment() TextVerticalAlignment {
	return sp.verticalAlignment
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
func (i *Invoice) GenerateKDict() (*model.KDict, error) { return nil, nil }
