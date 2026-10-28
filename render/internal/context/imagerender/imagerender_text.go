package imagerender

import (
	"container/list"
	"image"
	color2 "image/color"
	"math"
	"os"

	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/render/internal/context"
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/math/f64"
	"golang.org/x/image/math/fixed"
)

func (dc *Context) MeasureString(s string, face font.Face) (width, height float64) {
	drawer := &font.Drawer{Face: face}
	advance := drawer.MeasureString(s)
	return float64(advance >> 6), dc.textState.Tf.Size
}
func (dc *Context) SetGlyphCache(c *GlyphCache) { dc.glyphCache = c }

func (gc *GlyphCache) lookup(fontID uint64, face font.Face, pos fixed.Point26_6, r rune) (image.Rectangle, *image.Alpha, image.Point, fixed.Int26_6, bool) {
	frac := int(pos.X) & 63
	subpixel := uint8(frac * glyphSubpixelSteps >> 6)
	key := glyphKey{fontID: fontID, r: r, subpixel: subpixel}
	origin := image.Pt(int(pos.X>>6), int(pos.Y>>6))
	if elem, hit := gc.entries[key]; hit {
		gc.lru.MoveToFront(elem)
		entry := elem.Value.(*glyphEntry)
		return entry.bounds.Add(origin), entry.mask, image.Point{}, entry.advance, true
	}
	subX := fixed.Int26_6((int(subpixel)*2 + 1) * 64 / (2 * glyphSubpixelSteps))
	bounds, glyphMask, maskPt, advance, ok := face.Glyph(fixed.Point26_6{X: subX}, r)
	if !ok {
		return image.Rectangle{}, nil, image.Point{}, 0, false
	}
	mask := copyToAlpha(glyphMask, maskPt, bounds)
	if size := len(mask.Pix); size <= gc.maxBytes {
		newEntry := &glyphEntry{key: key, bounds: bounds, mask: mask, advance: advance, size: size}
		gc.entries[key] = gc.lru.PushFront(newEntry)
		gc.usedBytes += size
		for gc.usedBytes > gc.maxBytes {
			back := gc.lru.Back()
			if back == nil {
				break
			}
			oldEntry := back.Value.(*glyphEntry)
			delete(gc.entries, oldEntry.key)
			gc.lru.Remove(back)
			gc.usedBytes -= oldEntry.size
		}
	}
	return bounds.Add(origin), mask, image.Point{}, advance, true
}

func (dc *Context) DrawStringAnchored(s string, face font.Face, x, y, ax, ay float64) {
	w, h := dc.MeasureString(s, face)
	dc.drawString(s, face, x-ax*w, y+ay*h)
}

type GlyphCache struct {
	enabled   bool
	maxBytes  int
	usedBytes int
	lru       *list.List
	entries   map[glyphKey]*list.Element
}

func (dc *Context) DrawString(s string, face font.Face, x, y float64) {
	dc.DrawStringAnchored(s, face, x, y, 0, 0)
}

const defaultGlyphCacheBytes = 64 << 20

func (dc *Context) drawStringFast(s string, face font.Face, pos fixed.Point26_6) {
	m := dc.matrix
	flipped := m[4] < 0
	xShift := fixed.Int26_6(math.Round(m[6] * 64))
	baseY := int(math.Round(fromFixed(pos.Y)*m[4] + m[7]))
	r, g, b, a := dc.color.RGBA()
	fontID, cacheable := dc.glyphFontID(face)
	prev := rune(-1)
	for _, ch := range s {
		if prev >= 0 {
			pos.X += face.Kern(prev, ch)
		}
		bounds, mask, maskPt, advance, ok := dc.fastGlyph(cacheable, fontID, face, fixed.Point26_6{X: pos.X + xShift}, ch)
		if !ok {
			continue
		}
		dx := bounds.Min.X
		dy := bounds.Min.Y + baseY
		if flipped {
			dy = baseY - bounds.Max.Y
		}
		dc.blitGlyphMask(mask, maskPt, bounds.Dx(), bounds.Dy(), dx, dy, r, g, b, a, flipped)
		pos.X += advance
		prev = ch
	}
}
func (dc *Context) TextState() *context.TextState { return &dc.textState }

const glyphSubpixelSteps = 4

func NewGlyphCache() *GlyphCache {
	return &GlyphCache{enabled: glyphCacheEnabled, maxBytes: defaultGlyphCacheBytes, lru: list.New(), entries: make(map[glyphKey]*list.Element)}
}

var glyphCacheEnabled = os.Getenv("OPENPDF_GLYPH_CACHE") != "0"

func (dc *Context) GlyphCache() *GlyphCache { return dc.glyphCache }

func (dc *Context) drawString(s string, face font.Face, x, y float64) {
	pos := toFixedPoint(transform.NewPoint(x, y))
	if dc.softMask == nil && dc.blendMode == context.BlendModeNormal && !dc.knockout && canUseFastGlyphPath(dc.matrix) {
		dc.drawStringFast(s, face, pos)
		return
	}
	dc.drawStringSlow(s, face, pos)
}

func (dc *Context) fastGlyph(cached bool, fontID uint64, face font.Face, pos fixed.Point26_6, r rune) (image.Rectangle, *image.Alpha, image.Point, fixed.Int26_6, bool) {
	if cached {
		return dc.glyphCache.lookup(fontID, face, pos, r)
	}
	bounds, glyphMask, maskPt, advance, ok := face.Glyph(pos, r)
	if !ok {
		return image.Rectangle{}, nil, image.Point{}, 0, false
	}
	if alpha, isAlpha := glyphMask.(*image.Alpha); isAlpha {
		return bounds, alpha, maskPt, advance, true
	}
	return bounds, copyToAlpha(glyphMask, maskPt, bounds), image.Point{}, advance, true
}

type glyphKey struct {
	fontID   uint64
	r        rune
	subpixel uint8
}

func (dc *Context) glyphFontID(face font.Face) (id uint64, ok bool) {
	if dc.glyphCache == nil || !dc.glyphCache.enabled {
		return 0, false
	}
	tf := dc.textState.Tf
	if tf == nil {
		return 0, false
	}
	return tf.FaceID(face)
}

func canUseFastGlyphPath(m transform.Matrix) bool {
	return m[1] == 0 && m[3] == 0 && math.Abs(m[0]-1) < epsilon && math.Abs(math.Abs(m[4])-1) < epsilon
}

type glyphEntry struct {
	key     glyphKey
	bounds  image.Rectangle
	mask    *image.Alpha
	advance fixed.Int26_6
	size    int
}

func (dc *Context) blitGlyphMask(mask *image.Alpha, maskPt image.Point, w, h, dx, dy int, r, g, b, a uint32, flip bool) {
	const max16 = 0xffff
	dst := dc.img
	bounds := dst.Bounds()
	for i := 0; i < h; i++ {
		y := dy + i
		if y < bounds.Min.Y || y >= bounds.Max.Y {
			continue
		}
		my := maskPt.Y + i
		if flip {
			my = maskPt.Y + (h - 1 - i)
		}
		mo := mask.PixOffset(maskPt.X, my)
		for j := 0; j < w; j++ {
			x := dx + j
			if x < bounds.Min.X || x >= bounds.Max.X {
				continue
			}
			cov := uint32(mask.Pix[mo+j])
			if cov == 0 {
				continue
			}
			cov16 := cov * 0x101
			if dc.mask != nil {
				clip16 := uint32(dc.mask.Pix[dc.mask.PixOffset(x, y)]) * 0x101
				cov16 = cov16 * clip16 / max16
				if cov16 == 0 {
					continue
				}
			}
			inv := (max16 - a*cov16/max16) * 0x101
			off := dst.PixOffset(x, y)
			dst.Pix[off+0] = uint8((uint32(dst.Pix[off+0])*inv + r*cov16) / max16 >> 8)
			dst.Pix[off+1] = uint8((uint32(dst.Pix[off+1])*inv + g*cov16) / max16 >> 8)
			dst.Pix[off+2] = uint8((uint32(dst.Pix[off+2])*inv + b*cov16) / max16 >> 8)
			dst.Pix[off+3] = uint8((uint32(dst.Pix[off+3])*inv + a*cov16) / max16 >> 8)
		}
	}
}

func (dc *Context) drawStringSlow(s string, face font.Face, pos fixed.Point26_6) {
	src := image.NewUniform(dc.color)
	var mask, softMask *image.Alpha
	if dc.knockout {
		mask, softMask = dc.mask, dc.softMask
	} else {
		mask = dc.effectiveMask()
	}
	var opts *draw.Options
	if mask != nil {
		opts = &draw.Options{DstMask: mask, DstMaskP: image.Point{}}
	}
	needsBlend := dc.blendMode != context.BlendModeNormal || dc.knockout
	alpha := 1.0
	if dc.knockout {
		col := color2.NRGBAModel.Convert(dc.color).(color2.NRGBA)
		alpha = float64(col.A) / 255
		col.A = 0xff
		src = image.NewUniform(col)
	}
	prev := rune(-1)
	for _, r := range s {
		if prev >= 0 {
			pos.X += face.Kern(prev, r)
		}
		bounds, glyphMask, maskPt, advance, ok := face.Glyph(pos, r)
		if !ok {
			continue
		}
		local := bounds.Sub(bounds.Min)
		glyph := image.NewRGBA(local)
		draw.DrawMask(glyph, local, src, image.Point{}, glyphMask, maskPt, draw.Over)
		m := dc.matrix.Clone().Translate(float64(bounds.Min.X), float64(bounds.Min.Y))
		aff := f64.Aff3{m[0], m[3], m[6], m[1], m[4], m[7]}
		if needsBlend {
			area := transformedBounds(aff, local).Intersect(dc.img.Bounds())
			if !area.Empty() {
				tmp := image.NewRGBA(area)
				dc.interpolator.Transform(tmp, aff, glyph, local, draw.Over, opts)
				compositeBlend(dc.img, tmp, dc.backdrop, dc.blendMode, alpha, softMask, dc.knockout)
			}
		} else {
			dc.interpolator.Transform(dc.img, aff, glyph, local, draw.Over, opts)
		}
		pos.X += advance
		prev = r
	}
}
