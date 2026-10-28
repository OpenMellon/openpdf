// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package classer

import (
	"image"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

func (c *Classer) classifyCorrelation(boxes *bitmap.Boxes, comps *bitmap.Bitmaps, pageNumber int) error {
	const funcName = "classifyCorrelation"
	if boxes == nil {
		return jbig2errors.Error(funcName, "newComponents bounding boxes not found")
	}
	if comps == nil {
		return jbig2errors.Error(funcName, "newComponents bitmap array not found")
	}
	n := len(comps.Values)
	if n == 0 {
		common.Log.Debug("classifyCorrelation - provided pixas is empty")
		return nil
	}
	var (
		comp, bordered *bitmap.Bitmap
		err            error
	)
	borderedComps := &bitmap.Bitmaps{Values: make([]*bitmap.Bitmap, n)}
	for i, src := range comps.Values {
		bordered, err = src.AddBorderGeneral(JbAddedPixels, JbAddedPixels, JbAddedPixels, JbAddedPixels, 0)
		if err != nil {
			return jbig2errors.Wrap(err, funcName, "")
		}
		borderedComps.Values[i] = bordered
	}
	fgTemplates := c.FgTemplates
	sumTab := bitmap.MakePixelSumTab8()
	centroidTab := bitmap.MakePixelCentroidTab8()
	compPixelCounts := make([]int, n)
	rowSums := make([][]int, n)
	centroidStore := bitmap.Points(make([]bitmap.Point, n))
	centroids := &centroidStore
	var (
		xSum, ySum                      int
		pixelCount, rowOffset, rowCount int
		byteIdx, y                      int
		b                               byte
	)
	for idx, bm := range borderedComps.Values {
		rowSums[idx] = make([]int, bm.Height)
		xSum = 0
		ySum = 0
		rowOffset = (bm.Height - 1) * bm.RowStride
		pixelCount = 0
		for y = bm.Height - 1; y >= 0; y, rowOffset = y-1, rowOffset-bm.RowStride {
			rowSums[idx][y] = pixelCount
			rowCount = 0
			for byteIdx = 0; byteIdx < bm.RowStride; byteIdx++ {
				b = bm.Data[rowOffset+byteIdx]
				rowCount += sumTab[b]
				xSum += centroidTab[b] + byteIdx*8*sumTab[b]
			}
			pixelCount += rowCount
			ySum += rowCount * y
		}
		compPixelCounts[idx] = pixelCount
		if pixelCount > 0 {
			(*centroids)[idx] = bitmap.Point{X: float32(xSum) / float32(pixelCount), Y: float32(ySum) / float32(pixelCount)}
		} else {
			(*centroids)[idx] = bitmap.Point{X: float32(bm.Width) / float32(2), Y: float32(bm.Height) / float32(2)}
		}
	}
	if err = c.CentroidPoints.Add(centroids); err != nil {
		return jbig2errors.Wrap(err, funcName, "centroid add")
	}
	var (
		tmplArea, compPixels, tmplPixels int
		threshold                        float64
		cx, cy, tcx, tcy                 float32
		compPos, tmplPos                 bitmap.Point
		found                            bool
		finder                           *similarTemplatesFinder
		ci                               int
		tmpl                             *bitmap.Bitmap
		box                              *image.Rectangle
		instances                        *bitmap.Bitmaps
	)
	for ci, bordered = range borderedComps.Values {
		compPixels = compPixelCounts[ci]
		if cx, cy, err = centroids.GetGeometry(ci); err != nil {
			return jbig2errors.Wrap(err, funcName, "pta - i")
		}
		found = false
		newClass := len(c.UndilatedTemplates.Values)
		finder = newSimilarTemplatesFinder(c, bordered)
		for classID := finder.Next(); classID > -1; {
			if tmpl, err = c.UndilatedTemplates.GetBitmap(classID); err != nil {
				return jbig2errors.Wrap(err, funcName, "unidlated[iclass] = bm2")
			}
			if tmplPixels, err = fgTemplates.GetInt(classID); err != nil {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("FG Template [iclass] failed: %v", err)
				}
			}
			if tcx, tcy, err = c.CentroidPointsTemplates.GetGeometry(classID); err != nil {
				return jbig2errors.Wrap(err, funcName, "CentroidPointTemplates[iclass] = x2,y2 ")
			}
			if c.Settings.WeightFactor > 0.0 {
				if tmplArea, err = c.TemplateAreas.Get(classID); err != nil {
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("TemplateAreas[iclass] = area %v", err)
					}
				}
				threshold = c.Settings.Thresh + (1.0-c.Settings.Thresh)*c.Settings.WeightFactor*float64(tmplPixels)/float64(tmplArea)
			} else {
				threshold = c.Settings.Thresh
			}
			match, scoreErr := bitmap.CorrelationScoreThresholded(bordered, tmpl, compPixels, tmplPixels, compPos.X-tmplPos.X, compPos.Y-tmplPos.Y, MaxDiffWidth, MaxDiffHeight, sumTab, rowSums[ci], float32(threshold))
			if scoreErr != nil {
				return jbig2errors.Wrap(scoreErr, funcName, "")
			}
			if debugCorrelation {
				var (
					score, simpleScore      float64
					scoreCount, simpleCount int
				)
				score, scoreErr = bitmap.CorrelationScore(bordered, tmpl, compPixels, tmplPixels, cx-tcx, cy-tcy, MaxDiffWidth, MaxDiffHeight, sumTab)
				if scoreErr != nil {
					return jbig2errors.Wrap(scoreErr, funcName, "debugCorrelationScore")
				}
				simpleScore, scoreErr = bitmap.CorrelationScoreSimple(bordered, tmpl, compPixels, tmplPixels, cx-tcx, cy-tcy, MaxDiffWidth, MaxDiffHeight, sumTab)
				if scoreErr != nil {
					return jbig2errors.Wrap(scoreErr, funcName, "debugCorrelationScore")
				}
				scoreCount = int(math.Sqrt(score * float64(compPixels) * float64(tmplPixels)))
				simpleCount = int(math.Sqrt(simpleScore * float64(compPixels) * float64(tmplPixels)))
				if (score >= threshold) != (simpleScore >= threshold) {
					return jbig2errors.Errorf(funcName, "debug Correlation score mismatch - %d(%0.4f, %v) vs %d(%0.4f, %v) %0.4f", scoreCount, score, score >= threshold, simpleCount, simpleScore, simpleScore >= threshold, score-simpleScore)
				}
				if score >= threshold != match {
					return jbig2errors.Errorf(funcName, "debug Correlation score Mismatch between correlation / threshold. Comparison: %0.4f(%0.4f, %d) >= %0.4f(%0.4f) vs %v", score, score*float64(compPixels)*float64(tmplPixels), scoreCount, threshold, float32(threshold)*float32(compPixels)*float32(tmplPixels), match)
				}
			}
			if match {
				found = true
				if scoreErr = c.ClassIDs.Add(classID); scoreErr != nil {
					return jbig2errors.Wrap(scoreErr, funcName, "overThreshold")
				}
				if scoreErr = c.ComponentPageNumbers.Add(pageNumber); scoreErr != nil {
					return jbig2errors.Wrap(scoreErr, funcName, "overThreshold")
				}
				if c.Settings.KeepClassInstances {
					if comp, scoreErr = comps.GetBitmap(ci); scoreErr != nil {
						return jbig2errors.Wrap(scoreErr, funcName, "KeepClassInstances - i")
					}
					if instances, scoreErr = c.ClassInstances.GetBitmaps(classID); scoreErr != nil {
						return jbig2errors.Wrap(scoreErr, funcName, "KeepClassInstances - iClass")
					}
					instances.AddBitmap(comp)
					if box, scoreErr = boxes.Get(ci); scoreErr != nil {
						return jbig2errors.Wrap(scoreErr, funcName, "KeepClassInstances")
					}
					instances.AddBox(box)
				}
				break
			}
		}
		if !found {
			if err = c.ClassIDs.Add(newClass); err != nil {
				return jbig2errors.Wrap(err, funcName, "!found")
			}
			if err = c.ComponentPageNumbers.Add(pageNumber); err != nil {
				return jbig2errors.Wrap(err, funcName, "!found")
			}
			instances = &bitmap.Bitmaps{}
			if comp, err = comps.GetBitmap(ci); err != nil {
				return jbig2errors.Wrap(err, funcName, "!found")
			}
			instances.AddBitmap(comp)
			w, h := comp.Width, comp.Height
			area := uint64(h) * uint64(w)
			c.TemplatesSize.Add(area, newClass)
			if box, err = boxes.Get(ci); err != nil {
				return jbig2errors.Wrap(err, funcName, "!found")
			}
			instances.AddBox(box)
			c.ClassInstances.AddBitmaps(instances)
			c.CentroidPointsTemplates.AddPoint(cx, cy)
			c.FgTemplates.AddInt(compPixels)
			c.UndilatedTemplates.AddBitmap(comp)
			tmplArea = (bordered.Width - 2*JbAddedPixels) * (bordered.Height - 2*JbAddedPixels)
			if err = c.TemplateAreas.Add(tmplArea); err != nil {
				return jbig2errors.Wrap(err, funcName, "!found")
			}
		}
	}
	c.NumberOfClasses = len(c.UndilatedTemplates.Values)
	return nil
}

const (
	MaxConnCompWidth = 350
	MaxCharCompWidth = 350
	MaxWordCompWidth = 1000
	MaxCompHeight    = 120
)

type Classer struct {
	BaseIndex               int
	Settings                Settings
	ComponentsNumber        *basic.IntSlice
	TemplateAreas           *basic.IntSlice
	Widths                  map[int]int
	Heights                 map[int]int
	NumberOfClasses         int
	ClassInstances          *bitmap.BitmapsArray
	UndilatedTemplates      *bitmap.Bitmaps
	DilatedTemplates        *bitmap.Bitmaps
	TemplatesSize           basic.IntsMap
	FgTemplates             *basic.NumSlice
	CentroidPoints          *bitmap.Points
	CentroidPointsTemplates *bitmap.Points
	ClassIDs                *basic.IntSlice
	ComponentPageNumbers    *basic.IntSlice
	PtaUL                   *bitmap.Points
	PtaLL                   *bitmap.Points
}

func (f *similarTemplatesFinder) Next() int {
	var (
		h, w, n, templateIdx int
		ok                   bool
		bm                   *bitmap.Bitmap
		err                  error
	)
	for {
		if f.Index >= 25 {
			return -1
		}
		w = f.Width + TwoByTwoWalk[2*f.Index]
		h = f.Height + TwoByTwoWalk[2*f.Index+1]
		if h < 1 || w < 1 {
			f.Index++
			continue
		}
		if len(f.CurrentNumbers) == 0 {
			f.CurrentNumbers, ok = f.Classer.TemplatesSize.GetSlice(uint64(w) * uint64(h))
			if !ok {
				f.Index++
				continue
			}
			f.N = 0
		}
		n = len(f.CurrentNumbers)
		for ; f.N < n; f.N++ {
			templateIdx = f.CurrentNumbers[f.N]
			bm, err = f.Classer.DilatedTemplates.GetBitmap(templateIdx)
			if err != nil {
				common.Log.Debug("FindNextTemplate: template not found: ")
				return 0
			}
			if bm.Width-2*JbAddedPixels == w && bm.Height-2*JbAddedPixels == h {
				return templateIdx
			}
		}
		f.Index++
		f.CurrentNumbers = nil
	}
}

func (c *Classer) addPageComponents(page *bitmap.Bitmap, boxes *bitmap.Boxes, comps *bitmap.Bitmaps, pageNumber int, method Method) error {
	const funcName = "Classer.AddPageComponents"
	if page == nil {
		return jbig2errors.Error(funcName, "nil input page")
	}
	if boxes == nil || comps == nil || len(*boxes) == 0 {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("AddPageComponents: %s. No components found", page)
		}
		return nil
	}
	var err error
	switch method {
	case RankHaus:
		err = c.classifyRankHaus(boxes, comps, pageNumber)
	case Correlation:
		err = c.classifyCorrelation(boxes, comps, pageNumber)
	default:
		common.Log.Debug("Unknown classify method: '%v'", method)
		return jbig2errors.Error(funcName, "unknown classify method")
	}
	if err != nil {
		return jbig2errors.Wrap(err, funcName, "")
	}
	if err = c.getULCorners(page, boxes); err != nil {
		return jbig2errors.Wrap(err, funcName, "")
	}
	n := len(*boxes)
	c.BaseIndex += n
	if err = c.ComponentsNumber.Add(n); err != nil {
		return jbig2errors.Wrap(err, funcName, "")
	}
	return nil
}

type Settings struct {
	MaxCompWidth       int
	MaxCompHeight      int
	SizeHaus           int
	RankHaus           float64
	Thresh             float64
	WeightFactor       float64
	KeepClassInstances bool
	Components         bitmap.Component
	Method             Method
}
