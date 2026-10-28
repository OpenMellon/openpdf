// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package classer

import (
	"image"
	"math"

	"github.com/OpenMellon/openpdf/common"
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
