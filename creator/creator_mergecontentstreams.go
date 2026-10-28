// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"os"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/model"
)

func mergeContentStreams(contentStreamOperations *contentstream.ContentStreamOperations, resources *model.PdfPageResources, contentStreamOperations2 *contentstream.ContentStreamOperations, resources2 *model.PdfPageResources) error {
	m := map[core.PdfObjectName]core.PdfObjectName{}
	m2 := map[core.PdfObjectName]core.PdfObjectName{}
	m3 := map[core.PdfObjectName]core.PdfObjectName{}
	m4 := map[core.PdfObjectName]core.PdfObjectName{}
	m5 := map[core.PdfObjectName]core.PdfObjectName{}
	m6 := map[core.PdfObjectName]core.PdfObjectName{}
	for _, op := range *contentStreamOperations2 {
		switch op.Operand {
		case "Do":
			if len(op.Params) == 1 {
				if pdfObjectName, ok := op.Params[0].(*core.PdfObjectName); ok {
					if _, ok := m[*pdfObjectName]; !ok {
						var keyName core.PdfObjectName
						xObject, _ := resources2.GetXObjectByName(*pdfObjectName)
						if xObject != nil {
							keyName = *pdfObjectName
							for {
								xObject2, _ := resources.GetXObjectByName(keyName)
								if xObject2 == nil || xObject2 == xObject {
									break
								}
								keyName = *core.MakeName(nextResourceName(keyName.String()))
							}
						}
						resources.SetXObjectByName(keyName, xObject)
						m[*pdfObjectName] = keyName
					}
					s := m[*pdfObjectName]
					op.Params[0] = &s
				}
			}
		case "Tf":
			if len(op.Params) == 2 {
				if pdfObjectName, ok := op.Params[0].(*core.PdfObjectName); ok {
					if _, ok := m2[*pdfObjectName]; !ok {
						font, ok := resources2.GetFontByName(*pdfObjectName)
						keyName := *pdfObjectName
						if ok && font != nil {
							keyName = uniqueFontName(pdfObjectName.String(), font, resources)
						}
						resources.SetFontByName(keyName, font)
						m2[*pdfObjectName] = keyName
					}
					s := m2[*pdfObjectName]
					op.Params[0] = &s
				}
			}
		case "CS", "cs":
			if len(op.Params) == 1 {
				if pdfObjectName, ok := op.Params[0].(*core.PdfObjectName); ok {
					if _, ok := m3[*pdfObjectName]; !ok {
						var keyName core.PdfObjectName
						colorspace, ok := resources2.GetColorspaceByName(*pdfObjectName)
						if ok {
							keyName = *pdfObjectName
							for {
								colorspace2, ok := resources.GetColorspaceByName(keyName)
								if !ok || colorspace == colorspace2 {
									break
								}
								keyName = *core.MakeName(nextResourceName(keyName.String()))
							}
							resources.SetColorspaceByName(keyName, colorspace)
							m3[*pdfObjectName] = keyName
						} else {
							common.Log.Debug("Colorspace not found")
						}
					}
					if s, ok := m3[*pdfObjectName]; ok {
						op.Params[0] = &s
					} else {
						common.Log.Debug("Error: Colorspace %s not found", *pdfObjectName)
					}
				}
			}
		case "SCN", "scn":
			if len(op.Params) == 1 {
				if pdfObjectName, ok := op.Params[0].(*core.PdfObjectName); ok {
					if _, ok := m4[*pdfObjectName]; !ok {
						var keyName core.PdfObjectName
						pattern, ok := resources2.GetPatternByName(*pdfObjectName)
						if ok {
							keyName = *pdfObjectName
							for {
								pattern2, ok := resources.GetPatternByName(keyName)
								if !ok || pattern2 == pattern {
									break
								}
								keyName = *core.MakeName(nextResourceName(keyName.String()))
							}
							err := resources.SetPatternByName(keyName, pattern.ToPdfObject())
							if err != nil {
								return err
							}
							m4[*pdfObjectName] = keyName
						}
					}
					if s, ok := m4[*pdfObjectName]; ok {
						op.Params[0] = &s
					}
				}
			}
		case "sh":
			if len(op.Params) == 1 {
				if pdfObjectName, ok := op.Params[0].(*core.PdfObjectName); ok {
					if _, ok := m5[*pdfObjectName]; !ok {
						var keyName core.PdfObjectName
						shading, ok := resources2.GetShadingByName(*pdfObjectName)
						if ok {
							keyName = *pdfObjectName
							for {
								shading2, ok := resources.GetShadingByName(keyName)
								if !ok || shading == shading2 {
									break
								}
								keyName = *core.MakeName(nextResourceName(keyName.String()))
							}
							err := resources.SetShadingByName(keyName, shading.ToPdfObject())
							if err != nil {
								common.Log.Debug("ERROR Set shading: %v", err)
								return err
							}
							m5[*pdfObjectName] = keyName
						} else {
							common.Log.Debug("Shading not found")
						}
					}
					if s, ok := m5[*pdfObjectName]; ok {
						op.Params[0] = &s
					} else {
						common.Log.Debug("Error: Shading %s not found", *pdfObjectName)
					}
				}
			}
		case "gs":
			if len(op.Params) == 1 {
				if pdfObjectName, ok := op.Params[0].(*core.PdfObjectName); ok {
					if _, ok := m6[*pdfObjectName]; !ok {
						var keyName core.PdfObjectName
						extGState, ok := resources2.GetExtGState(*pdfObjectName)
						if ok {
							keyName = *pdfObjectName
							for {
								extGState2, ok := resources.GetExtGState(keyName)
								if !ok || extGState == extGState2 {
									break
								}
								keyName = *core.MakeName(nextResourceName(keyName.String()))
							}
						}
						resources.AddExtGState(keyName, extGState)
						m6[*pdfObjectName] = keyName
					}
					s := m6[*pdfObjectName]
					op.Params[0] = &s
				}
			}
		}
		*contentStreamOperations = append(*contentStreamOperations, op)
	}
	return nil
}

// Draw processes the specified Drawable widget and generates blocks that can
// be rendered to the output document. The generated blocks can span over one
// or more pages. Additional pages are added if the contents go over the current
// page. Each generated block is assigned to the creator page it will be
// rendered to. In order to render the generated blocks to the creator pages,
// call Finalize, Write or WriteToFile.
func (c *Creator) Draw(d Drawable) error {
	if c.getActivePage() == nil {
		c.NewPage()
	}
	var (
		pagesLen      int64
		mcid          int64
		componentKObj *model.KDict
		n             int
	)
	if c.taggingEnabled {
		pagesLen = int64(len(c.pages))
		d.SetStructPageNumber(&pagesLen)
		mcid = c.pageMCIDs[pagesLen]
		n = len(c.docKDict.GetChildren())
		switch d2 := d.(type) {
		case *Table:
			d2.AddTag(c.docKDict)
			d2.SetMarkedContentID(mcid)
			componentKObj = d2.tagInfo.ComponentKObj
		case *Grid:
			d2.AddTag(c.docKDict)
			d2.SetMarkedContentID(mcid)
			componentKObj = d2.tagInfo.ComponentKObj
		case *List:
			d2.AddTag(c.docKDict)
			d2.SetMarkedContentID(mcid)
		case *Division:
			d2.AddTag(c.docKDict)
			d2.SetMarkedContentID(mcid)
			componentKObj = d2.tagInfo.ComponentKObj
		default:
			d2.SetMarkedContentID(mcid)
			kDict, err := d2.GenerateKDict()
			if err != nil {
				return err
			}
			if kDict != nil {
				c.docKDict.AddKChild(kDict)
			}
			componentKObj = kDict
		}
	}
	pageBlocks, ctx, err := d.GeneratePageBlocks(c.context)
	if err != nil {
		return err
	}
	if len(ctx.errs) > 0 {
		c.Errors = append(c.Errors, ctx.errs...)
	}
	if c.taggingEnabled {
		if componentKObj == nil {
			if children := c.docKDict.GetChildren(); len(children) > n {
				componentKObj = children[len(children)-1].GetKDict()
			}
		}
		if componentKObj != nil {
			mcid2 := mcid + 1
			if n := maxMCID(componentKObj); n >= mcid2 {
				mcid2 = n + 1
			}
			c.pageMCIDs[pagesLen] = mcid2
		}
	}
	for i, pageBlock := range pageBlocks {
		if i > 0 {
			c.NewPage()
		}
		activePage := c.getActivePage()
		if block, ok := c.blocks[activePage]; ok {
			if err := block.mergeBlocks(pageBlock); err != nil {
				return err
			}
			if err := mergeColorspaces(pageBlock.resources, block.resources); err != nil {
				return err
			}
		} else {
			c.blocks[activePage] = pageBlock
		}
	}
	c.context.X = ctx.X
	c.context.Y = ctx.Y
	c.context.Height = precision.RoundDefault(ctx.PageHeight - ctx.Y - ctx.Margins.Bottom)
	return nil
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

func (t *Table) maxFittingRows(value, top float64) int {
	t.sortCells()
	t.updateRowHeights(value - t.margins.Left - t.margins.Right)
	top -= t.margins.Top + t.margins.Bottom
	var rowHeight float64
	row := 0
	for i := 1; i <= t.rows && i-1 < len(t.rowHeights); i++ {
		rowHeight += t.rowHeights[i-1]
		if rowHeight > top {
			break
		}
		row = i
	}
	return t.clampCut(row)
}

// GetCoords returns coordinates of border.
func (bo *border) GetCoords() (float64, float64) { return bo.x, bo.y }

// Width is not used. Not used as a Table element is designed to fill into
// available width depending on the context. Returns 0.
func (t *Table) Width() float64 { return 0 }

func (gse *GraphicSVGElement) getGradientAngle() float64 {
	attribute, ok := gse.Attributes["gradientTransform"]
	if ok {
		fields := strings.FieldsSeq(attribute)
		for field := range fields {
			fieldsFunc := strings.FieldsFunc(field, isTransformSeparator)
			if len(fieldsFunc) < 2 {
				continue
			}
			if fieldsFunc[0] == "rotate" {
				colorComponent, err := parseColorComponent(fieldsFunc[1])
				if err != nil {
					common.Log.Debug("can't parse transform attribute %v", attribute)
					return 0.0
				}
				return colorComponent
			}
		}
	}
	return 0.0
}
func templateArray(templateArray ...any) []any { return templateArray }

// FitMode returns the fit mode of the ellipse.
func (e *Ellipse) FitMode() FitMode { return e.fitMode }

// WriteToFile writes the Creator output to file specified by path.
func (c *Creator) WriteToFile(outputPath string) error {
	ws, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer ws.Close()
	return c.Write(ws)
}

// Terms returns the terms and conditions section of the invoice as a
// title-content pair.
func (i *Invoice) Terms() (string, string)    { return i.terms[0], i.terms[1] }
func (tp *templateProcessor) validate() error { return tp.process(true) }

// Lines returns all the lines the table of contents has.
func (to *TOC) Lines() []*TOCLine { return to.lines }

func defaultTextStyle(font *model.PdfFont) TextStyle {
	return TextStyle{Color: ColorRGBFrom8bit(0, 0, 0), Font: font, FontSize: 10, OutlineSize: 1, HorizontalScaling: DefaultHorizontalScaling, UnderlineStyle: TextDecorationLineStyle{Offset: 1, Thickness: 1}}
}

// SetFillOpacity sets the fill opacity.
func (cp *CurvePolygon) SetFillOpacity(opacity float64) { cp.fillOpacity = opacity }

// BorderOpacity returns the border opacity of the ellipse (0-1).
func (e *Ellipse) BorderOpacity() float64 { return e.borderOpacity }
