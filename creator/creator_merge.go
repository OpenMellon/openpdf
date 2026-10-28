package creator

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
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

func mergeSubtableSplits(subtableSplits []subtableSplit) ([][2]*Table, bool) {
	nums := make([]int, len(subtableSplits))
	nums2 := make([]int, len(subtableSplits))
	nums3 := make([]int, len(subtableSplits))
	num := -1
	for i, subtableSplit := range subtableSplits {
		nums[i] = subtableSplit.table.topHeaderRows()
		nums2[i] = subtableSplit.table.rows - nums[i]
		nums3[i] = max(subtableSplit.table.maxFittingRows(subtableSplit.ctx.Width, subtableSplit.ctx.Height)-nums[i], 0)
		if nums3[i] < nums2[i] && (num < 0 || nums3[i] < num) {
			num = nums3[i]
		}
	}
	if num < 0 {
		return nil, false
	}
	for ok := true; ok; {
		ok = false
		for i, subtableSplit := range subtableSplits {
			num2 := min(num, nums2[i])
			if num3 := max(subtableSplit.table.clampCut(num2+nums[i])-nums[i], 0); num3 < num2 {
				num = num3
				ok = true
			}
		}
	}
	tables := make([]*Table, len(subtableSplits))
	nums4 := make([]int, len(subtableSplits))
	ok := true
	for i, subtableSplit := range subtableSplits {
		tables[i], nums4[i] = subtableSplit.table, num+1+nums[i]
		switch {
		case num >= nums2[i]:
			nums4[i] = subtableSplit.table.rows
		case nums3[i] > num:
			if subtableSplit.table.clampCut(nums4[i]) != nums4[i] {
				ok = false
			}
		default:
			if tables[i] = subtableSplit.table.withPartialRow(num+nums[i], subtableSplit.ctx, subtableSplit.height); tables[i] == nil {
				ok = false
			}
		}
		if !ok {
			break
		}
	}
	if !ok {
		for i, subtableSplit := range subtableSplits {
			tables[i], nums4[i] = subtableSplit.table, min(num+nums[i], subtableSplit.table.rows)
		}
	}
	rows := make([][2]*Table, len(subtableSplits))
	for i := range subtableSplits {
		head, tail := tables[i].splitAtRow(tables[i].headerCut(nums4[i]))
		rows[i] = [2]*Table{head, tail}
	}
	return rows, true
}

func mergeColorspaces(resources, resources2 *model.PdfPageResources) error {
	colorspaces, _ := resources.GetColorspaces()
	if colorspaces != nil && len(colorspaces.Colorspaces) > 0 {
		for colorspaceKey, colorspace := range colorspaces.Colorspaces {
			name := *core.MakeName(colorspaceKey)
			if resources2.HasColorspaceByName(name) {
				continue
			}
			err := resources2.SetColorspaceByName(name, colorspace)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
