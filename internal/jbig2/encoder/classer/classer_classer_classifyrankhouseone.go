// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package classer

import (
	"image"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

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
