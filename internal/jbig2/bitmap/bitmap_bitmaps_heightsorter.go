// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitmap

import (
	"sort"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

func (bms *Bitmaps) HeightSorter() func(i, j int) bool {
	return func(i, j int) bool {
		less := bms.Values[i].Height < bms.Values[j].Height
		common.Log.Debug("Height: %v < %v = %v", bms.Values[i].Height, bms.Values[j].Height, less)
		return less
	}
}

func seedfillBinaryLow4(seed, mask *Bitmap, h, wpl int) (err error) {
	const processName = "seedfillBinaryLow4"
	var (
		i, j, seedLine, maskLine                                                int
		seedByte, maskByte, aboveByte, leftByte, belowByte, rightByte, prevByte byte
	)
	for i = 0; i < h; i++ {
		seedLine = i * seed.RowStride
		maskLine = i * mask.RowStride
		for j = 0; j < wpl; j++ {
			seedByte, err = seed.GetByte(seedLine + j)
			if err != nil {
				return jbig2errors.Wrap(err, processName, "first get")
			}
			maskByte, err = mask.GetByte(maskLine + j)
			if err != nil {
				return jbig2errors.Wrap(err, processName, "second get")
			}
			if i > 0 {
				aboveByte, err = seed.GetByte(seedLine - seed.RowStride + j)
				if err != nil {
					return jbig2errors.Wrap(err, processName, "i > 0")
				}
				seedByte |= aboveByte
			}
			if j > 0 {
				leftByte, err = seed.GetByte(seedLine + j - 1)
				if err != nil {
					return jbig2errors.Wrap(err, processName, "j > 0")
				}
				seedByte |= leftByte << 7
			}
			seedByte &= maskByte
			if seedByte == 0 || (^seedByte) == 0 {
				if err = seed.SetByte(seedLine+j, seedByte); err != nil {
					return jbig2errors.Wrap(err, processName, "bt == 0 || (^bt) == 0")
				}
				continue
			}
			for {
				prevByte = seedByte
				seedByte = (seedByte | (seedByte >> 1) | (seedByte << 1)) & maskByte
				if (seedByte ^ prevByte) == 0 {
					if err = seed.SetByte(seedLine+j, seedByte); err != nil {
						return jbig2errors.Wrap(err, processName, "setting prev byte")
					}
					break
				}
			}
		}
	}
	for i = h - 1; i >= 0; i-- {
		seedLine = i * seed.RowStride
		maskLine = i * mask.RowStride
		for j = wpl - 1; j >= 0; j-- {
			if seedByte, err = seed.GetByte(seedLine + j); err != nil {
				return jbig2errors.Wrap(err, processName, "reverse first get")
			}
			if maskByte, err = mask.GetByte(maskLine + j); err != nil {
				return jbig2errors.Wrap(err, processName, "reverse get mask byte")
			}
			if i < h-1 {
				if belowByte, err = seed.GetByte(seedLine + seed.RowStride + j); err != nil {
					return jbig2errors.Wrap(err, processName, "reverse i < h -1")
				}
				seedByte |= belowByte
			}
			if j < wpl-1 {
				if rightByte, err = seed.GetByte(seedLine + j + 1); err != nil {
					return jbig2errors.Wrap(err, processName, "reverse j < wpl - 1")
				}
				seedByte |= rightByte >> 7
			}
			seedByte &= maskByte
			if seedByte == 0 || (^seedByte) == 0 {
				if err = seed.SetByte(seedLine+j, seedByte); err != nil {
					return jbig2errors.Wrap(err, processName, "setting masked byte failed")
				}
				continue
			}
			for {
				prevByte = seedByte
				seedByte = (seedByte | (seedByte >> 1) | (seedByte << 1)) & maskByte
				if (seedByte ^ prevByte) == 0 {
					if err = seed.SetByte(seedLine+j, seedByte); err != nil {
						return jbig2errors.Wrap(err, processName, "reverse setting prev byte")
					}
					break
				}
			}
		}
	}
	return nil
}

func init() {
	for i := 0; i < 256; i++ {
		popCountTab[i] = uint8(i&0x1) + (uint8(i>>1) & 0x1) + (uint8(i>>2) & 0x1) + (uint8(i>>3) & 0x1) + (uint8(i>>4) & 0x1) + (uint8(i>>5) & 0x1) + (uint8(i>>6) & 0x1) + (uint8(i>>7) & 0x1)
	}
}

func Centroid(bm *Bitmap, centTab, sumTab []int) (Point, error) { return bm.centroid(centTab, sumTab) }

func (bm *Bitmap) setBit(bitIdx int) { bm.Data[(bitIdx >> 3)] |= 0x80 >> uint(bitIdx&7) }

func (bm *Bitmap) GetComponents(components Component, maxWidth, maxHeight int) (bitmaps *Bitmaps, boxes *Boxes, err error) {
	const processName = "Bitmap.GetComponents"
	if bm == nil {
		return nil, nil, jbig2errors.Error(processName, "source Bitmap not defined.")
	}
	switch components {
	case ComponentConn, ComponentCharacters, ComponentWords:
	default:
		return nil, nil, jbig2errors.Error(processName, "invalid components parameter")
	}
	if bm.Zero() {
		boxes = &Boxes{}
		bitmaps = &Bitmaps{}
		return bitmaps, boxes, nil
	}
	switch components {
	case ComponentConn:
		bitmaps = &Bitmaps{}
		if boxes, err = bm.ConnComponents(bitmaps, 8); err != nil {
			return nil, nil, jbig2errors.Wrap(err, processName, "no preprocessing")
		}
	case ComponentCharacters:
		closed, err2 := MorphSequence(bm, MorphProcess{Operation: MopClosing, Arguments: []int{1, 6}})
		if err2 != nil {
			return nil, nil, jbig2errors.Wrap(err2, processName, "characters preprocessing")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("ComponentCharacters bitmap after closing: %s", closed.String())
		}
		connBitmaps := &Bitmaps{}
		boxes, err2 = closed.ConnComponents(connBitmaps, 8)
		if err2 != nil {
			return nil, nil, jbig2errors.Wrap(err2, processName, "characters preprocessing")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("ComponentCharacters bitmap after connectivity: %s", connBitmaps.String())
		}
		if bitmaps, err2 = connBitmaps.ClipToBitmap(bm); err2 != nil {
			return nil, nil, jbig2errors.Wrap(err2, processName, "characters preprocessing")
		}
	case ComponentWords:
		factor := 1
		var reduced *Bitmap
		switch {
		case bm.XResolution <= 200:
			reduced = bm
		case bm.XResolution <= 400:
			factor = 2
			reduced, err = reduceRankBinaryCascade(bm, 1, 0, 0, 0)
			if err != nil {
				return nil, nil, jbig2errors.Wrap(err, processName, "word preprocess - xres<=400")
			}
		default:
			factor = 4
			reduced, err = reduceRankBinaryCascade(bm, 1, 1, 0, 0)
			if err != nil {
				return nil, nil, jbig2errors.Wrap(err, processName, "word preprocess - xres > 400")
			}
		}
		wordMask, _, err2 := wordMaskByDilation(reduced)
		if err2 != nil {
			return nil, nil, jbig2errors.Wrap(err2, processName, "word preprocess")
		}
		expanded, err2 := expandReplicate(wordMask, factor)
		if err2 != nil {
			return nil, nil, jbig2errors.Wrap(err2, processName, "word preprocess")
		}
		wordBitmaps := &Bitmaps{}
		if boxes, err2 = expanded.ConnComponents(wordBitmaps, 4); err2 != nil {
			return nil, nil, jbig2errors.Wrap(err2, processName, "word preprocess, connect expanded")
		}
		if bitmaps, err2 = wordBitmaps.ClipToBitmap(bm); err2 != nil {
			return nil, nil, jbig2errors.Wrap(err2, processName, "word preprocess")
		}
	}
	bitmaps, err = bitmaps.SelectBySize(maxWidth, maxHeight, LocSelectIfBoth, SizeSelectIfLTE)
	if err != nil {
		return nil, nil, jbig2errors.Wrap(err, processName, "")
	}
	boxes, err = boxes.SelectBySize(maxWidth, maxHeight, LocSelectIfBoth, SizeSelectIfLTE)
	if err != nil {
		return nil, nil, jbig2errors.Wrap(err, processName, "")
	}
	return bitmaps, boxes, nil
}

func (boxes *Boxes) SelectBySize(width, height int, tp LocationFilter, relation SizeComparison) (result *Boxes, err error) {
	const processName = "Boxes.SelectBySize"
	if boxes == nil {
		return nil, jbig2errors.Error(processName, "boxes 'b' not defined")
	}
	if len(*boxes) == 0 {
		return boxes, nil
	}
	switch tp {
	case LocSelectWidth, LocSelectHeight, LocSelectIfEither, LocSelectIfBoth:
	default:
		return nil, jbig2errors.Errorf(processName, "invalid filter type: %d", tp)
	}
	switch relation {
	case SizeSelectIfLT, SizeSelectIfGT, SizeSelectIfLTE, SizeSelectIfGTE:
	default:
		return nil, jbig2errors.Errorf(processName, "invalid relation type: '%d'", tp)
	}
	indicator := boxes.makeSizeIndicator(width, height, tp, relation)
	selected, err := boxes.selectWithIndicator(indicator)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	return selected, nil
}

func makeExpandTab4x() (tab [256]uint32) {
	for i := 0; i < 256; i++ {
		if i&0x01 != 0 {
			tab[i] |= 0xf
		}
		if i&0x02 != 0 {
			tab[i] |= 0xf0
		}
		if i&0x04 != 0 {
			tab[i] |= 0xf00
		}
		if i&0x08 != 0 {
			tab[i] |= 0xf000
		}
		if i&0x10 != 0 {
			tab[i] |= 0xf0000
		}
		if i&0x20 != 0 {
			tab[i] |= 0xf00000
		}
		if i&0x40 != 0 {
			tab[i] |= 0xf000000
		}
		if i&0x80 != 0 {
			tab[i] |= 0xf0000000
		}
	}
	return tab
}

func (cp *ClassedPoints) GroupByY() ([]*ClassedPoints, error) {
	const processName = "ClassedPoints.GroupByY"
	if err := cp.validateIntSlice(); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	if cp.IntSlice.Size() == 0 {
		return nil, jbig2errors.Error(processName, "No classes provided")
	}
	cp.SortByY()
	var (
		groups []*ClassedPoints
		y      int
	)
	prevY := -1
	var group *ClassedPoints
	for i := 0; i < len(cp.IntSlice); i++ {
		y = int(cp.YAtIndex(i))
		if y != prevY {
			group = &ClassedPoints{Points: cp.Points}
			prevY = y
			groups = append(groups, group)
		}
		group.IntSlice = append(group.IntSlice, cp.IntSlice[i])
	}
	for _, g := range groups {
		g.SortByX()
	}
	return groups, nil
}

const (
	shiftLeft shift = iota
	shiftRight
)

func (boxes *Boxes) makeSizeIndicator(width, height int, filter LocationFilter, relation SizeComparison) *basic.NumSlice {
	indicator := &basic.NumSlice{}
	var flag, w, h int
	for _, box := range *boxes {
		flag = 0
		w, h = box.Dx(), box.Dy()
		switch filter {
		case LocSelectWidth:
			if (relation == SizeSelectIfLT && w < width) || (relation == SizeSelectIfGT && w > width) || (relation == SizeSelectIfLTE && w <= width) || (relation == SizeSelectIfGTE && w >= width) {
				flag = 1
			}
		case LocSelectHeight:
			if (relation == SizeSelectIfLT && h < height) || (relation == SizeSelectIfGT && h > height) || (relation == SizeSelectIfLTE && h <= height) || (relation == SizeSelectIfGTE && h >= height) {
				flag = 1
			}
		case LocSelectIfEither:
			if (relation == SizeSelectIfLT && (h < height || w < width)) || (relation == SizeSelectIfGT && (h > height || w > width)) || (relation == SizeSelectIfLTE && (h <= height || w <= width)) || (relation == SizeSelectIfGTE && (h >= height || w >= width)) {
				flag = 1
			}
		case LocSelectIfBoth:
			if (relation == SizeSelectIfLT && (h < height && w < width)) || (relation == SizeSelectIfGT && (h > height && w > width)) || (relation == SizeSelectIfLTE && (h <= height && w <= width)) || (relation == SizeSelectIfGTE && (h >= height && w >= width)) {
				flag = 1
			}
		}
		indicator.AddInt(flag)
	}
	return indicator
}

const (
	asymmetricMorphBC BoundaryCondition = iota
	symmetricMorphBC
)

func makeExpandTab8x() (tab [256]uint64) {
	for i := 0; i < 256; i++ {
		if i&0x01 != 0 {
			tab[i] |= 0xff
		}
		if i&0x02 != 0 {
			tab[i] |= 0xff00
		}
		if i&0x04 != 0 {
			tab[i] |= 0xff0000
		}
		if i&0x08 != 0 {
			tab[i] |= 0xff000000
		}
		if i&0x10 != 0 {
			tab[i] |= 0xff00000000
		}
		if i&0x20 != 0 {
			tab[i] |= 0xff0000000000
		}
		if i&0x40 != 0 {
			tab[i] |= 0xff000000000000
		}
		if i&0x80 != 0 {
			tab[i] |= 0xff00000000000000
		}
	}
	return tab
}
func (cp *ClassedPoints) SortByY() { cp.lessFunc = cp.ySortFunction(); sort.Sort(cp) }
