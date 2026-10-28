package creator

import (
	"errors"
	"fmt"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/unidoc/typesetting/shaping"
)

// paragraphDrawer holds the state needed to render the lines of a styled
// paragraph that fit on the current page into a block.
type paragraphDrawer struct {
	block *Block
	para  *StyledParagraph
	ctx   DrawContext
	cc    *contentstream.ContentCreator

	// lines are the lines drawn on this page; rest are the lines that did not fit.
	lines [][]*TextChunk
	rest  [][]*TextChunk

	fontName   core.PdfObjectName
	fontSize   float64
	nextFont   int
	isRelative bool

	// fontNames and shaped are indexed by line and then by chunk.
	fontNames [][]core.PdfObjectName
	shaped    [][]shaping.Line
	advances  []float64
	height    float64

	firstLineFontSize float64

	dropCap *TextChunk
	dropGap float64

	// baseY is the baseline of the first line, lineY the baseline of the current line.
	baseY float64
	lineY float64

	colors     map[Color]model.PdfColor
	colorSpans map[Color]*[2]float64
	underlines []*contentdraw.BasicLine
	openedProp bool
}

func (d *paragraphDrawer) drawChunk(ls *lineState, j int, chunk *TextChunk) error {
	run := &chunkRun{d: d, ls: ls, chunk: chunk, style: &chunk.Style, index: j}
	run.hasOutline = run.style.OutlineColor != nil
	run.hasHScale = run.style.HorizontalScaling != DefaultHorizontalScaling
	run.hasOutlineSize = run.style.OutlineSize != 1
	run.hasRenderMode = run.style.RenderingMode != TextRenderingModeFill
	run.hasCharSpacing = run.style.CharSpacing != 0
	run.hasRise = run.style.TextRise != 0
	run.beginStyle()
	descent := run.applyVerticalAlignment()
	if err := run.selectFont(); err != nil {
		return err
	}
	run.openSpan()
	if err := run.writeText(); err != nil {
		return err
	}
	if err := run.flushData(); err != nil {
		return err
	}
	if len(run.props) > 0 {
		d.cc.Add_EMC()
	}
	width := ls.widths[ls.chunkIndex] / 1000.0
	d.drawUnderline(run, width)
	d.addAnnotations(run, width)
	d.extendColorSpan(run.style.Color, ls.x, width)
	ls.x += width
	run.endStyle(descent)
	ls.chunkIndex++
	return nil
}

// drawUnderline queues an underline below the chunk; it is stroked after the
// text object ends.
func (d *paragraphDrawer) drawUnderline(run *chunkRun, width float64) {
	style := run.style
	if !style.Underline {
		return
	}
	color := style.UnderlineStyle.Color
	if color == nil {
		color = run.chunk.Style.Color
	}
	r, g, b := color.ToRGB()
	x1 := run.ls.x - d.ctx.X
	offset := d.lineY - d.baseY + style.TextRise - style.UnderlineStyle.Offset
	d.underlines = append(d.underlines, &contentdraw.BasicLine{X1: x1, Y1: offset, X2: x1 + width, Y2: offset, LineWidth: run.chunk.Style.UnderlineStyle.Thickness, LineColor: model.NewPdfColorDeviceRGB(r, g, b)})
}

// addAnnotations sets the rectangle of each annotation attached to the chunk
// and adds it to the block.
func (d *paragraphDrawer) addAnnotations(run *chunkRun, width float64) {
	chunk, style, ls := run.chunk, run.style, run.ls
	for k, annotation := range chunk.annotations {
		var array *core.PdfObjectArray
		if len(chunk.lineBreaks) == k {
			switch context := annotation.GetContext().(type) {
			case *model.PdfAnnotationLink:
				array = core.MakeArray()
				context.Rect = array
				d.flipLinkDestination(context)
			case *model.PdfAnnotationHighlight:
				array = core.MakeArray()
				context.Rect = array
				llx := ls.x
				textRise := d.lineY + style.TextRise
				points := rotatedCorners(&model.PdfRectangle{Llx: llx, Lly: textRise, Urx: llx + width, Ury: textRise + ls.advance}, d.para.angle)
				context.QuadPoints = core.MakeArrayFromFloats([]float64{points[0].X, points[0].Y, points[1].X, points[1].Y, points[3].X, points[3].Y, points[2].X, points[2].Y})
			}
			chunk.lineBreaks = append(chunk.lineBreaks, true)
		}
		if array != nil {
			d.setAnnotationRect(array, ls.x, style.TextRise, width, ls.advance)
		}
		d.block.AddAnnotation(annotation)
	}
}

// flipLinkDestination converts the y coordinate of an XYZ destination from
// the creator's top-down system to PDF's bottom-up one.
func (d *paragraphDrawer) flipLinkDestination(link *model.PdfAnnotationLink) {
	dest, ok := link.Dest.(*core.PdfObjectArray)
	if !ok || dest.Len() != 5 {
		return
	}
	name, ok := dest.Get(1).(*core.PdfObjectName)
	if !ok || name.String() != "XYZ" {
		return
	}
	if top, err := core.GetNumberAsFloat(dest.Get(3)); err == nil {
		dest.Set(3, core.MakeFloat(d.ctx.PageHeight-top))
	}
}

// setAnnotationRect fills array with the rotated bounds of a chunk of the
// given width and height that starts at chunkX on the current line.
func (d *paragraphDrawer) setAnnotationRect(array *core.PdfObjectArray, chunkX, textRise, width, height float64) {
	angle := d.para.angle
	point := contentdraw.NewPoint(chunkX-d.ctx.X, d.lineY+textRise-d.baseY).Rotate(angle)
	point.X += d.ctx.X
	point.Y += d.baseY
	bx, by, bw, bh := rotatedBounds(width, height, angle)
	point.X += bx
	point.Y += by
	array.Clear()
	array.Append(core.MakeFloat(point.X))
	array.Append(core.MakeFloat(point.Y))
	array.Append(core.MakeFloat(point.X + bw))
	array.Append(core.MakeFloat(point.Y + bh))
}

func (d *paragraphDrawer) draw() (DrawContext, [][]*TextChunk, error) {
	para := d.para
	if err := d.registerDefaultFont(); err != nil {
		return d.ctx, nil, err
	}
	if err := d.measureLines(); err != nil {
		return d.ctx, nil, err
	}
	var capHeight, minDescent float64
	if len(d.lines) > 0 {
		capHeight, d.firstLineFontSize, minDescent = chunksFontMetrics(d.lines[0])
	}
	var firstAdvance float64
	if len(d.advances) > 0 {
		firstAdvance = d.advances[0]
	}
	lineHeight := capHeight * para.lineHeight
	if para.dropCapsActive() {
		d.dropCap = para.chunks[0]
		ref := d.dropCapReference()
		firstAdvance = ref.Style.FontSize * ref.Style.effectiveLineHeight(para.lineHeight)
	}
	if len(d.lines) == 0 {
		return d.ctx, d.rest, nil
	}
	d.cc = contentstream.NewContentCreator()
	d.cc.Add_q()
	value4 := firstAdvance
	if para.verticalAlignment == TextVerticalAlignmentCenter {
		value4 = d.firstLineFontSize + (capHeight+minDescent-d.firstLineFontSize)/2 + (firstAdvance-d.firstLineFontSize)/2
	}
	d.baseY = d.ctx.PageHeight - d.ctx.Y - value4
	d.cc.Translate(d.ctx.X, d.baseY)
	d.lineY = d.baseY
	if para.angle != 0 {
		d.cc.RotateDeg(para.angle)
	}
	if err := d.registerShadings(); err != nil {
		return d.ctx, nil, err
	}
	if para.textOverflow == TextOverflowHidden {
		d.cc.Add_re(0, -d.height+lineHeight+1, para.width, d.height).Add_W().Add_n()
	}
	d.cc.Add_BT()
	d.openMarkedContent()
	if d.dropCap != nil {
		if err := d.drawDropCap(); err != nil {
			return d.ctx, nil, err
		}
	}
	for i, chunks := range d.lines {
		if err := d.drawLine(i, chunks); err != nil {
			return d.ctx, nil, err
		}
	}
	if err := d.finalizeShadings(); err != nil {
		return d.ctx, nil, err
	}
	d.finish()
	return d.ctx, d.rest, nil
}

// registerDefaultFont registers the paragraph's default font under the first
// free Font<n> resource name.
func (d *paragraphDrawer) registerDefaultFont() error {
	n := 1
	keyName := core.PdfObjectName(fmt.Sprintf("Font%d", n))
	for d.block.resources.HasFontByName(keyName) {
		n++
		keyName = core.PdfObjectName(fmt.Sprintf("Font%d", n))
	}
	if err := d.block.resources.SetFontByName(keyName, d.para.defaultStyle.Font.ToPdfObject()); err != nil {
		return err
	}
	d.nextFont = n + 1
	d.fontName = keyName
	return nil
}

// newFontName returns the next unused Font<n> resource name.
func (d *paragraphDrawer) newFontName() core.PdfObjectName {
	return core.PdfObjectName(fmt.Sprintf("Font%d", d.nextFont))
}

// openMarkedContent starts the tagged or artifact marked-content sequence
// that wraps the paragraph text.
func (d *paragraphDrawer) openMarkedContent() {
	para := d.para
	propertyList := map[string]core.PdfObject{}
	if para.artifact == nil {
		if para.tagInfo != nil {
			propertyList["MCID"] = core.MakeInteger(para.tagInfo.Mcid)
		}
		if para.languageID != "" {
			propertyList["Lang"] = core.MakeString(para.languageID)
		}
		if len(propertyList) > 0 {
			d.cc.Add_BDC(*core.MakeName(string(para.tagInfo.StructureType)), propertyList)
		}
	} else {
		propertyList = para.artifact.GenerateMap()
		if len(propertyList) > 0 {
			d.cc.Add_BDC(*core.MakeName(string("Artifact")), propertyList)
		} else {
			d.cc.Add_BMC(*core.MakeName(string("Artifact")))
		}
	}
	d.openedProp = len(propertyList) > 0
}

// finish closes the text object and emits the underlines, then adds the
// content to the block and advances the draw context.
func (d *paragraphDrawer) finish() {
	if d.openedProp {
		d.cc.Add_EMC()
	}
	d.cc.Add_ET()
	for _, basicLine := range d.underlines {
		d.cc.SetStrokingColor(basicLine.LineColor).Add_w(basicLine.LineWidth).Add_m(basicLine.X1, basicLine.Y1).Add_l(basicLine.X2, basicLine.Y2).Add_s()
	}
	d.cc.Add_Q()
	operations := d.cc.Operations()
	operations.WrapIfNeeded()
	d.block.addWrappedContents(operations)
	if d.isRelative {
		d.ctx.Y += d.height
		d.ctx.Height -= d.height
		if d.ctx.Inline {
			d.ctx.X += d.para.Width() + d.para.margins.Right
		}
	}
}

// drawDropCap draws the drop cap character at the start of the first line
// and records the horizontal gap the following lines must leave for it.
func (d *paragraphDrawer) drawDropCap() error {
	chunk := d.dropCap
	style := &chunk.Style
	dropCaps := d.para.dropCaps
	fontName := d.newFontName()
	if err := d.block.resources.SetFontByName(fontName, style.Font.ToPdfObject()); err != nil {
		return err
	}
	d.nextFont++
	data := style.Font.Encoder().Encode(chunk.Text)
	var charSpacing float64
	text := []rune(chunk.Text)
	for i, r := range text {
		runeMetrics, ok := style.Font.GetRuneMetrics(r)
		if ok {
			charSpacing += style.FontSize * runeMetrics.Wx * style.horizontalScale() / 1000.0
			if i < len(text)-1 {
				charSpacing += style.CharSpacing
			}
		}
	}
	ref := d.dropCapReference()
	refMetrics := newFontMetrics(ref.Style.Font, ref.Style.FontSize)
	dropMetrics := newFontMetrics(style.Font, style.FontSize)
	var lift float64
	switch dropCaps.Type {
	case DropCapsDrop:
		lift = refMetrics.ascent - dropMetrics.ascent
	case DropCapsInline:
		lift = refMetrics.capHeight - dropMetrics.capHeight
	default:
		return nil
	}
	d.cc.SetNonStrokingColor(d.pdfColorFor(style.Color)).Add_Tf(fontName, style.FontSize).Add_Td(0, lift).Add_TJ([]core.PdfObject{core.MakeStringFromBytes(data)}...)
	d.cc.Add_Td(charSpacing+dropCaps.Gap, -lift)
	d.dropGap = charSpacing + dropCaps.Gap
	d.extendColorSpan(style.Color, d.ctx.X, charSpacing)
	return nil
}

// dropCapIndent returns how far line i must be indented and how far the
// previous line was indented to leave room for the drop cap.
func (d *paragraphDrawer) dropCapIndent(i int) (indent, prevIndent float64) {
	if d.dropCap == nil {
		return 0, 0
	}
	dropCaps := d.para.dropCaps
	if i > 0 {
		if dropCaps.Type == DropCapsDrop && i-1 < dropCaps.NumLines {
			prevIndent = d.dropGap
		} else if dropCaps.Type == DropCapsInline {
			prevIndent = d.dropGap
		}
	}
	switch dropCaps.Type {
	case DropCapsDrop:
		if i < dropCaps.NumLines {
			indent = d.dropGap
		}
	case DropCapsInline:
		indent = d.dropGap
	}
	return indent, prevIndent
}

// measureLines registers the font of every chunk, shapes the text when
// enabled and works out how many lines fit in the available height. Lines
// that do not fit are moved to d.rest.
func (d *paragraphDrawer) measureLines() error {
	d.shaped = [][]shaping.Line{}
	d.advances = make([]float64, 0, len(d.lines))
	for i, chunks := range d.lines {
		fontSize, advance := d.lineFontSizes(i, chunks)
		names, shapedLine, err := d.registerLineFonts(i, chunks, &fontSize, &advance)
		if err != nil {
			return err
		}
		d.shaped = append(d.shaped, shapedLine)
		if d.isRelative && d.height+advance > d.ctx.Height {
			d.rest = d.lines[i:]
			d.lines = d.lines[:i]
			break
		}
		d.height += advance
		d.advances = append(d.advances, advance)
		d.fontNames = append(d.fontNames, names)
	}
	return nil
}

// lineFontSizes returns the initial font size and vertical advance of a line,
// taken from its first chunk (or its second when the first is a drop cap).
func (d *paragraphDrawer) lineFontSizes(i int, chunks []*TextChunk) (fontSize, advance float64) {
	para := d.para
	if len(chunks) == 0 {
		return 0, 0
	}
	fontSize = chunks[0].Style.FontSize
	advance = chunks[0].Style.FontSize * chunks[0].Style.effectiveLineHeight(para.lineHeight)
	if i == 0 && para.dropCapsActive() && len(chunks) > 1 {
		fontSize = chunks[1].Style.FontSize
		advance = chunks[1].Style.FontSize * chunks[1].Style.effectiveLineHeight(para.lineHeight)
	}
	return fontSize, advance
}

// registerLineFonts registers the font of each chunk of a line as a page
// resource, growing fontSize and advance to the largest chunk.
func (d *paragraphDrawer) registerLineFonts(i int, chunks []*TextChunk, fontSize, advance *float64) ([]core.PdfObjectName, []shaping.Line, error) {
	para := d.para
	var names []core.PdfObjectName
	shapedLine := []shaping.Line{}
	for j, chunk := range chunks {
		if i == 0 && j == 0 && para.dropCapsActive() {
			continue
		}
		style := chunk.Style
		if chunk.Text != "" {
			if style.FontSize > *fontSize {
				*fontSize = style.FontSize
			}
			if size := style.FontSize * style.effectiveLineHeight(para.lineHeight); size > *advance {
				*advance = size
			}
		}
		if *fontSize > d.ctx.PageHeight {
			return nil, nil, errors.New("paragraph height can't be larger than page height")
		}
		keyName := d.newFontName()
		names = append(names, keyName)
		if para.enableShaping {
			line := style.Font.ShapeText(chunk.Text, style.FontSize)
			shapedLine = append(shapedLine, line)
			for _, shapingOutput := range line {
				chunk.Style.Font.RegisterLigatureMapping(shapingOutput, chunk.Text)
			}
		}
		if err := d.block.resources.SetFontByName(keyName, style.Font.ToPdfObject()); err != nil {
			return nil, nil, err
		}
		d.nextFont++
	}
	return names, shapedLine, nil
}

// dropCapReference returns the first chunk of the paragraph that is not the
// drop cap itself; the drop cap is sized relative to it.
func (d *paragraphDrawer) dropCapReference() *TextChunk {
	first := d.para.lines[0]
	ref := first[0]
	if len(first) > 1 && ref == d.dropCap {
		ref = first[1]
	}
	return ref
}

func (d *paragraphDrawer) drawLine(i int, chunks []*TextChunk) error {
	para := d.para
	ascent, _, descent := para.getLineMetrics(i)
	ls := &lineState{index: i, chunks: chunks, x: d.ctx.X, isLast: i == len(d.lines)-1}
	ls.extent = ascent + descent
	lineHeight := ls.extent * para.lineHeight
	ls.advance = d.advances[i]
	if lineHeight > ls.advance {
		ls.advance = lineHeight
	}
	if i != 0 {
		d.cc.Add_TD(0, -ls.advance)
		d.lineY -= ls.advance
	}
	indent, prevIndent := d.dropCapIndent(i)
	if i > 0 {
		if shift := indent - prevIndent; shift != 0 {
			d.cc.Add_Td(shift, 0)
		}
	}
	ls.x += indent
	w, err := d.measureLineWidths(ls)
	if err != nil {
		return err
	}
	ls.widths = w.widths
	ls.refMetrics = w.refMetrics
	ls.spaceWx = d.alignLine(ls, w)
	for j, chunk := range chunks {
		if i == 0 && j == 0 && d.dropCap != nil {
			continue
		}
		if err := d.drawChunk(ls, j, chunk); err != nil {
			return err
		}
	}
	return nil
}

// measureLineWidths measures the width of every chunk on a line and totals
// the space characters, which justification later stretches.
func (d *paragraphDrawer) measureLineWidths(ls *lineState) (lineWidths, error) {
	var (
		w          lineWidths
		maxSize    float64
		spaceTotal float64
	)
	for j, chunk := range ls.chunks {
		if ls.index == 0 && j == 0 && d.dropCap != nil {
			continue
		}
		style := &chunk.Style
		if style.FontSize > maxSize {
			maxSize = style.FontSize
			w.refMetrics = newFontMetrics(chunk.Style.Font, style.FontSize)
		}
		if ls.extent > maxSize {
			maxSize = ls.extent
		}
		spaceMetrics, ok := style.Font.GetRuneMetrics(' ')
		if spaceMetrics.Wx == 0 && style.MultiFont != nil {
			spaceMetrics, ok = style.MultiFont.GetRuneMetrics(' ')
			style.MultiFont.Reset()
		}
		if !ok {
			return w, errors.New("the font does not have a space glyph")
		}
		var spaces uint
		var width float64
		textLen := len(chunk.Text)
		for k, r := range chunk.Text {
			if r == ' ' {
				spaces++
				continue
			}
			if r == '\u000A' {
				continue
			}
			runeMetrics, ok := style.Font.GetRuneMetrics(r)
			if runeMetrics.Wx == 0 && style.MultiFont != nil {
				runeMetrics, ok = style.MultiFont.GetRuneMetrics(' ')
				style.MultiFont.Reset()
			}
			if !ok {
				common.Log.Debug("Unsupported rune %v in font\n", r)
				return w, errors.New("unsupported text glyph")
			}
			width += style.FontSize * runeMetrics.Wx * style.horizontalScale()
			if k != textLen-1 {
				width += style.CharSpacing * 1000.0
			}
		}
		w.widths = append(w.widths, width)
		w.total += width
		spaceTotal += float64(spaces) * spaceMetrics.Wx * style.FontSize * style.horizontalScale()
		w.spaceCount += spaces
	}
	w.spaceWx = spaceTotal
	return w, nil
}

// alignLine applies the paragraph's horizontal alignment to the line. It
// emits the offset needed for centred and right-aligned text, moves ls.x and
// returns the space width to use (stretched when justifying).
func (d *paragraphDrawer) alignLine(ls *lineState, w lineWidths) float64 {
	para := d.para
	var objs []core.PdfObject
	spaceWx := w.spaceWx
	width := para.width * 1000.0
	switch para.alignment {
	case TextAlignmentJustify:
		if w.spaceCount > 0 && !ls.isLast {
			spaceWx = (width - w.total) / float64(w.spaceCount) / d.fontSize
		}
	case TextAlignmentCenter:
		offset := (width - w.total - spaceWx) / 2
		objs = append(objs, core.MakeFloat(-(offset / d.fontSize)))
		ls.x += offset / 1000.0
	case TextAlignmentRight:
		offset := width - w.total - spaceWx
		objs = append(objs, core.MakeFloat(-(offset / d.fontSize)))
		ls.x += offset / 1000.0
	}
	if len(objs) > 0 {
		d.cc.Add_Tf(d.fontName, d.fontSize).Add_TL(d.fontSize * para.lineHeight).Add_TJ(objs...)
	}
	return spaceWx
}
