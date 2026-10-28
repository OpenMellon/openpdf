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

var (
	TwoByTwoWalk     = []int{0, 0, 0, 1, -1, 0, 0, -1, 1, 0, -1, 1, 1, 1, -1, -1, 1, -1, 0, -2, 2, 0, 0, 2, -2, 0, -1, -2, 1, -2, 2, -1, 2, 1, 1, 2, -1, 2, -2, 1, -2, -1, -2, -2, 2, -2, 2, 2, -2, 2}
	debugCorrelation bool
)

const (
	MaxDiffWidth  = 2
	MaxDiffHeight = 2
)

func (s *Settings) SetDefault() {
	if s.MaxCompWidth == 0 {
		switch s.Components {
		case bitmap.ComponentConn:
			s.MaxCompWidth = MaxConnCompWidth
		case bitmap.ComponentCharacters:
			s.MaxCompWidth = MaxCharCompWidth
		case bitmap.ComponentWords:
			s.MaxCompWidth = MaxWordCompWidth
		}
	}
	if s.MaxCompHeight == 0 {
		s.MaxCompHeight = MaxCompHeight
	}
	if s.Thresh == 0.0 {
		s.Thresh = 0.9
	}
	if s.WeightFactor == 0.0 {
		s.WeightFactor = 0.75
	}
	if s.RankHaus == 0.0 {
		s.RankHaus = 0.97
	}
	if s.SizeHaus == 0 {
		s.SizeHaus = 2
	}
}

const JbAddedPixels = 6

func (c *Classer) classifyRankHaus(boxes *bitmap.Boxes, comps *bitmap.Bitmaps, pageNumber int) error {
	const funcName = "classifyRankHaus"
	if boxes == nil {
		return jbig2errors.Error(funcName, "boxa not defined")
	}
	if comps == nil {
		return jbig2errors.Error(funcName, "pixa not defined")
	}
	n := len(comps.Values)
	if n == 0 {
		return jbig2errors.Error(funcName, "empty new components")
	}
	fgCounts := comps.CountPixels()
	size := c.Settings.SizeHaus
	sel := bitmap.SelCreateBrick(size, size, size/2, size/2, bitmap.SelHit)
	borderedComps := &bitmap.Bitmaps{Values: make([]*bitmap.Bitmap, n)}
	dilatedComps := &bitmap.Bitmaps{Values: make([]*bitmap.Bitmap, n)}
	var (
		comp, bordered, dilated *bitmap.Bitmap
		err                     error
	)
	for i := 0; i < n; i++ {
		comp, err = comps.GetBitmap(i)
		if err != nil {
			return jbig2errors.Wrap(err, funcName, "")
		}
		bordered, err = comp.AddBorderGeneral(JbAddedPixels, JbAddedPixels, JbAddedPixels, JbAddedPixels, 0)
		if err != nil {
			return jbig2errors.Wrap(err, funcName, "")
		}
		dilated, err = bitmap.Dilate(nil, bordered, sel)
		if err != nil {
			return jbig2errors.Wrap(err, funcName, "")
		}
		borderedComps.Values[n] = bordered
		dilatedComps.Values[n] = dilated
	}
	centroids, err := bitmap.Centroids(borderedComps.Values)
	if err != nil {
		return jbig2errors.Wrap(err, funcName, "")
	}
	if err = centroids.Add(c.CentroidPoints); err != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("No centroids to add")
		}
	}
	if c.Settings.RankHaus == 1.0 {
		err = c.classifyRankHouseOne(boxes, comps, borderedComps, dilatedComps, centroids, pageNumber)
	} else {
		err = c.classifyRankHouseNonOne(boxes, comps, borderedComps, dilatedComps, centroids, fgCounts, pageNumber)
	}
	if err != nil {
		return jbig2errors.Wrap(err, funcName, "")
	}
	return nil
}

func finalAlignmentPositioning(src *bitmap.Bitmap, x, y, shiftX, shiftY int, tmpl *bitmap.Bitmap) (pt image.Point, err error) {
	const funcName = "finalAlignmentPositioning"
	if src == nil {
		return pt, jbig2errors.Error(funcName, "source not provided")
	}
	if tmpl == nil {
		return pt, jbig2errors.Error(funcName, "template not provided")
	}
	w, h := tmpl.Width, tmpl.Height
	bx, by := x-shiftX-JbAddedPixels, y-shiftY-JbAddedPixels
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("x: '%d', y: '%d', w: '%d', h: '%d', bx: '%d', by: '%d'", x, y, w, h, bx, by)
	}
	box, err := bitmap.Rect(bx, by, w, h)
	if err != nil {
		return pt, jbig2errors.Wrap(err, funcName, "")
	}
	clipped, _, err := src.ClipRectangle(box)
	if err != nil {
		common.Log.Error("Can't clip rectangle: %v", box)
		return pt, jbig2errors.Wrap(err, funcName, "")
	}
	work, err := bitmap.New(clipped.Width, clipped.Height)
	if err != nil {
		return pt, jbig2errors.Wrap(err, funcName, "")
	}
	minCount := math.MaxInt32
	var dy, dx, count, bestDx, bestDy int
	for dy = -1; dy <= 1; dy++ {
		for dx = -1; dx <= 1; dx++ {
			if _, err = bitmap.Copy(work, clipped); err != nil {
				return pt, jbig2errors.Wrap(err, funcName, "")
			}
			if err = work.RasterOperation(dx, dy, w, h, bitmap.PixSrcXorDst, tmpl, 0, 0); err != nil {
				return pt, jbig2errors.Wrap(err, funcName, "")
			}
			count = work.CountPixels()
			if count < minCount {
				bestDx = dx
				bestDy = dy
				minCount = count
			}
		}
	}
	pt.X = bestDx
	pt.Y = bestDy
	return pt, nil
}

func newSimilarTemplatesFinder(c *Classer, bm *bitmap.Bitmap) *similarTemplatesFinder {
	return &similarTemplatesFinder{Width: bm.Width, Height: bm.Height, Classer: c}
}

const (
	RankHaus Method = iota
	Correlation
)

func (c *Classer) AddPage(inputPage *bitmap.Bitmap, pageNumber int, method Method) (err error) {
	const funcName = "Classer.AddPage"
	c.Widths[pageNumber] = inputPage.Width
	c.Heights[pageNumber] = inputPage.Height
	if err = c.verifyMethod(method); err != nil {
		return jbig2errors.Wrap(err, funcName, "")
	}
	comps, boxes, err := inputPage.GetComponents(c.Settings.Components, c.Settings.MaxCompWidth, c.Settings.MaxCompHeight)
	if err != nil {
		return jbig2errors.Wrap(err, funcName, "")
	}
	common.Log.Debug("Components: %v", comps)
	if err = c.addPageComponents(inputPage, boxes, comps, pageNumber, method); err != nil {
		return jbig2errors.Wrap(err, funcName, "")
	}
	return nil
}

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

func (c *Classer) classifyRankHouseOne(boxes *bitmap.Boxes, comps, bordered, dilated *bitmap.Bitmaps, centroids *bitmap.Points, pageNumber int) (err error) {
	const funcName = "Classer.classifyRankHouseOne"
	var (
		cx, cy, tcx, tcy                       float32
		classID                                int
		comp, bm, bmDilated, tmpl, tmplDilated *bitmap.Bitmap
		found, match                           bool
	)
	for i := 0; i < len(comps.Values); i++ {
		bm = bordered.Values[i]
		bmDilated = dilated.Values[i]
		cx, cy, err = centroids.GetGeometry(i)
		if err != nil {
			return jbig2errors.Wrapf(err, funcName, "first geometry")
		}
		newClass := len(c.UndilatedTemplates.Values)
		found = false
		finder := newSimilarTemplatesFinder(c, bm)
		for classID = finder.Next(); classID > -1; {
			tmpl, err = c.UndilatedTemplates.GetBitmap(classID)
			if err != nil {
				return jbig2errors.Wrap(err, funcName, "bm3")
			}
			tmplDilated, err = c.DilatedTemplates.GetBitmap(classID)
			if err != nil {
				return jbig2errors.Wrap(err, funcName, "bm4")
			}
			tcx, tcy, err = c.CentroidPointsTemplates.GetGeometry(classID)
			if err != nil {
				return jbig2errors.Wrap(err, funcName, "CentroidTemplates")
			}
			match, err = bitmap.HausTest(bm, bmDilated, tmpl, tmplDilated, cx-tcx, cy-tcy, MaxDiffWidth, MaxDiffHeight)
			if err != nil {
				return jbig2errors.Wrap(err, funcName, "")
			}
			if match {
				found = true
				if err = c.ClassIDs.Add(classID); err != nil {
					return jbig2errors.Wrap(err, funcName, "")
				}
				if err = c.ComponentPageNumbers.Add(pageNumber); err != nil {
					return jbig2errors.Wrap(err, funcName, "")
				}
				if c.Settings.KeepClassInstances {
					instances, innerErr := c.ClassInstances.GetBitmaps(classID)
					if innerErr != nil {
						return jbig2errors.Wrap(innerErr, funcName, "KeepPixaa")
					}
					comp, innerErr = comps.GetBitmap(i)
					if innerErr != nil {
						return jbig2errors.Wrap(innerErr, funcName, "KeepPixaa")
					}
					instances.AddBitmap(comp)
					box, innerErr := boxes.Get(i)
					if innerErr != nil {
						return jbig2errors.Wrap(innerErr, funcName, "KeepPixaa")
					}
					instances.AddBox(box)
				}
				break
			}
		}
		if !found {
			if err = c.ClassIDs.Add(newClass); err != nil {
				return jbig2errors.Wrap(err, funcName, "")
			}
			if err = c.ComponentPageNumbers.Add(pageNumber); err != nil {
				return jbig2errors.Wrap(err, funcName, "")
			}
			newInstances := &bitmap.Bitmaps{}
			comp, err = comps.GetBitmap(i)
			if err != nil {
				return jbig2errors.Wrap(err, funcName, "!found")
			}
			newInstances.Values = append(newInstances.Values, comp)
			w, h := comp.Width, comp.Height
			c.TemplatesSize.Add(uint64(h)*uint64(w), newClass)
			newBox, boxErr := boxes.Get(i)
			if boxErr != nil {
				return jbig2errors.Wrap(boxErr, funcName, "!found")
			}
			newInstances.AddBox(newBox)
			c.ClassInstances.AddBitmaps(newInstances)
			c.CentroidPointsTemplates.AddPoint(cx, cy)
			c.UndilatedTemplates.AddBitmap(bm)
			c.DilatedTemplates.AddBitmap(bmDilated)
		}
	}
	return nil
}

type Method int

func (s Settings) Validate() error {
	const funcName = "Settings.Validate"
	if s.Thresh < 0.4 || s.Thresh > 0.98 {
		return jbig2errors.Error(funcName, "jbig2 encoder thresh not in range [0.4 - 0.98]")
	}
	if s.WeightFactor < 0.0 || s.WeightFactor > 1.0 {
		return jbig2errors.Error(funcName, "jbig2 encoder weight factor not in range [0.0 - 1.0]")
	}
	if s.RankHaus < 0.5 || s.RankHaus > 1.0 {
		return jbig2errors.Error(funcName, "jbig2 encoder rank haus value not in range [0.5 - 1.0]")
	}
	if s.SizeHaus < 1 || s.SizeHaus > 10 {
		return jbig2errors.Error(funcName, "jbig2 encoder size haus value not in range [1 - 10]")
	}
	switch s.Components {
	case bitmap.ComponentConn, bitmap.ComponentCharacters, bitmap.ComponentWords:
	default:
		return jbig2errors.Error(funcName, "invalid classer component")
	}
	return nil
}

func (c *Classer) classifyRankHouseNonOne(boxes *bitmap.Boxes, comps, borderedComps, dilatedComps *bitmap.Bitmaps, centroids *bitmap.Points, fgCounts *basic.NumSlice, pageNumber int) (err error) {
	const funcName = "Classer.classifyRankHouseOne"
	var (
		cx, cy, tcx, tcy                           float32
		compPixels, tmplPixels, classID            int
		comp, bordered, dilated, tmpl, tmplDilated *bitmap.Bitmap
		found, match                               bool
	)
	sumTab := bitmap.MakePixelSumTab8()
	for i := 0; i < len(comps.Values); i++ {
		if bordered, err = borderedComps.GetBitmap(i); err != nil {
			return jbig2errors.Wrap(err, funcName, "bms1.Get(i)")
		}
		if compPixels, err = fgCounts.GetInt(i); err != nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Getting FGTemplates at: %d failed: %v", i, err)
			}
		}
		if dilated, err = dilatedComps.GetBitmap(i); err != nil {
			return jbig2errors.Wrap(err, funcName, "bms2.Get(i)")
		}
		if cx, cy, err = centroids.GetGeometry(i); err != nil {
			return jbig2errors.Wrapf(err, funcName, "pta[i].Geometry")
		}
		newClass := len(c.UndilatedTemplates.Values)
		found = false
		finder := newSimilarTemplatesFinder(c, bordered)
		for classID = finder.Next(); classID > -1; {
			if tmpl, err = c.UndilatedTemplates.GetBitmap(classID); err != nil {
				return jbig2errors.Wrap(err, funcName, "pixat.[iClass]")
			}
			if tmplPixels, err = c.FgTemplates.GetInt(classID); err != nil {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Getting FGTemplate[%d] failed: %v", classID, err)
				}
			}
			if tmplDilated, err = c.DilatedTemplates.GetBitmap(classID); err != nil {
				return jbig2errors.Wrap(err, funcName, "pixatd[iClass]")
			}
			if tcx, tcy, err = c.CentroidPointsTemplates.GetGeometry(classID); err != nil {
				return jbig2errors.Wrap(err, funcName, "CentroidPointsTemplates[iClass]")
			}
			match, err = bitmap.RankHausTest(bordered, dilated, tmpl, tmplDilated, cx-tcx, cy-tcy, MaxDiffWidth, MaxDiffHeight, compPixels, tmplPixels, float32(c.Settings.RankHaus), sumTab)
			if err != nil {
				return jbig2errors.Wrap(err, funcName, "")
			}
			if match {
				found = true
				if err = c.ClassIDs.Add(classID); err != nil {
					return jbig2errors.Wrap(err, funcName, "")
				}
				if err = c.ComponentPageNumbers.Add(pageNumber); err != nil {
					return jbig2errors.Wrap(err, funcName, "")
				}
				if c.Settings.KeepClassInstances {
					instances, innerErr := c.ClassInstances.GetBitmaps(classID)
					if innerErr != nil {
						return jbig2errors.Wrap(innerErr, funcName, "c.Pixaa.GetBitmaps(iClass)")
					}
					if comp, innerErr = comps.GetBitmap(i); innerErr != nil {
						return jbig2errors.Wrap(innerErr, funcName, "pixa[i]")
					}
					instances.Values = append(instances.Values, comp)
					box, innerErr := boxes.Get(i)
					if innerErr != nil {
						return jbig2errors.Wrap(innerErr, funcName, "boxa.Get(i)")
					}
					instances.Boxes = append(instances.Boxes, box)
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
			newInstances := &bitmap.Bitmaps{}
			comp = comps.Values[i]
			newInstances.AddBitmap(comp)
			w, h := comp.Width, comp.Height
			c.TemplatesSize.Add(uint64(w)*uint64(h), newClass)
			newBox, boxErr := boxes.Get(i)
			if boxErr != nil {
				return jbig2errors.Wrap(boxErr, funcName, "!found")
			}
			newInstances.AddBox(newBox)
			c.ClassInstances.AddBitmaps(newInstances)
			c.CentroidPointsTemplates.AddPoint(cx, cy)
			c.UndilatedTemplates.AddBitmap(bordered)
			c.DilatedTemplates.AddBitmap(dilated)
			c.FgTemplates.AddInt(compPixels)
		}
	}
	c.NumberOfClasses = len(c.UndilatedTemplates.Values)
	return nil
}

func Init(settings Settings) (*Classer, error) {
	const funcName = "classer.Init"
	c := &Classer{Settings: settings, Widths: map[int]int{}, Heights: map[int]int{}, TemplatesSize: basic.IntsMap{}, TemplateAreas: &basic.IntSlice{}, ComponentPageNumbers: &basic.IntSlice{}, ClassIDs: &basic.IntSlice{}, ComponentsNumber: &basic.IntSlice{}, CentroidPoints: &bitmap.Points{}, CentroidPointsTemplates: &bitmap.Points{}, UndilatedTemplates: &bitmap.Bitmaps{}, DilatedTemplates: &bitmap.Bitmaps{}, ClassInstances: &bitmap.BitmapsArray{}, FgTemplates: &basic.NumSlice{}}
	if err := c.Settings.Validate(); err != nil {
		return nil, jbig2errors.Wrap(err, funcName, "")
	}
	return c, nil
}

func (c *Classer) verifyMethod(method Method) error {
	if method != RankHaus && method != Correlation {
		return jbig2errors.Error("verifyMethod", "invalid classer method")
	}
	return nil
}

func (c *Classer) getULCorners(page *bitmap.Bitmap, boxes *bitmap.Boxes) error {
	const funcName = "getULCorners"
	if page == nil {
		return jbig2errors.Error(funcName, "nil image bitmap")
	}
	if boxes == nil {
		return jbig2errors.Error(funcName, "nil bounds")
	}
	if c.PtaUL == nil {
		c.PtaUL = &bitmap.Points{}
	}
	n := len(*boxes)
	var (
		idx, classID, dx, dy int
		cx, cy, tcx, tcy     float32
		err                  error
		box                  *image.Rectangle
		tmpl                 *bitmap.Bitmap
		pos                  image.Point
	)
	for i := 0; i < n; i++ {
		idx = c.BaseIndex + i
		if cx, cy, err = c.CentroidPoints.GetGeometry(idx); err != nil {
			return jbig2errors.Wrap(err, funcName, "CentroidPoints")
		}
		if classID, err = c.ClassIDs.Get(idx); err != nil {
			return jbig2errors.Wrap(err, funcName, "ClassIDs.Get")
		}
		if tcx, tcy, err = c.CentroidPointsTemplates.GetGeometry(classID); err != nil {
			return jbig2errors.Wrap(err, funcName, "CentroidPointsTemplates")
		}
		fdx := tcx - cx
		fdy := tcy - cy
		if fdx >= 0 {
			dx = int(fdx + 0.5)
		} else {
			dx = int(fdx - 0.5)
		}
		if fdy >= 0 {
			dy = int(fdy + 0.5)
		} else {
			dy = int(fdy - 0.5)
		}
		if box, err = boxes.Get(i); err != nil {
			return jbig2errors.Wrap(err, funcName, "")
		}
		x, y := box.Min.X, box.Min.Y
		tmpl, err = c.UndilatedTemplates.GetBitmap(classID)
		if err != nil {
			return jbig2errors.Wrap(err, funcName, "UndilatedTemplates.Get(iClass)")
		}
		pos, err = finalAlignmentPositioning(page, x, y, dx, dy, tmpl)
		if err != nil {
			return jbig2errors.Wrap(err, funcName, "")
		}
		c.PtaUL.AddPoint(float32(x-dx+pos.X), float32(y-dy+pos.Y))
	}
	return nil
}

type similarTemplatesFinder struct {
	Classer        *Classer
	Width          int
	Height         int
	Index          int
	CurrentNumbers []int
	N              int
}

func DefaultSettings() Settings { s := &Settings{}; s.SetDefault(); return *s }
func (c *Classer) ComputeLLCorners() (err error) {
	const funcName = "Classer.ComputeLLCorners"
	if c.PtaUL == nil {
		return jbig2errors.Error(funcName, "UL Corners not defined")
	}
	n := len(*c.PtaUL)
	c.PtaLL = &bitmap.Points{}
	var (
		x, y       float32
		classID, h int
		tmpl       *bitmap.Bitmap
	)
	for i := 0; i < n; i++ {
		x, y, err = c.PtaUL.GetGeometry(i)
		if err != nil {
			common.Log.Debug("Getting PtaUL failed: %v", err)
			return jbig2errors.Wrap(err, funcName, "PtaUL Geometry")
		}
		classID, err = c.ClassIDs.Get(i)
		if err != nil {
			common.Log.Debug("Getting ClassID failed: %v", err)
			return jbig2errors.Wrap(err, funcName, "ClassID")
		}
		tmpl, err = c.UndilatedTemplates.GetBitmap(classID)
		if err != nil {
			common.Log.Debug("Getting UndilatedTemplates failed: %v", err)
			return jbig2errors.Wrap(err, funcName, "Undilated Templates")
		}
		h = tmpl.Height
		c.PtaLL.AddPoint(x, y+float32(h))
	}
	return nil
}
