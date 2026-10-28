// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package classer

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

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
