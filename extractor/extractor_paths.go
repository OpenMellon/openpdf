package extractor

import (
	"fmt"
	"image/color"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

// String returns a human readable description of `ss`.
func (ss *shapesState) String() string {
	return fmt.Sprintf("{%d subpaths fresh=%t}", len(ss.subpaths), ss.fresh)
}
func (sp *subpath) add(pts ...transform.Point) { sp.points = append(sp.points, pts...) }

type subpath struct {
	points []transform.Point
	closed bool
}

func (sp *subpath) isQuadrilateral() bool {
	if len(sp.points) < 4 || len(sp.points) > 5 {
		return false
	}
	if len(sp.points) == 5 {
		first := sp.points[0]
		last := sp.points[4]
		if first.X != last.X || first.Y != last.Y {
			return false
		}
	}
	return true
}

func (ss *shapesState) lastpointEstablished() (transform.Point, bool) {
	if ss.fresh {
		return ss.currentPoint, false
	}
	numSubpaths := len(ss.subpaths)
	if numSubpaths > 0 && ss.subpaths[numSubpaths-1].closed {
		return ss.subpaths[numSubpaths-1].last(), false
	}
	return transform.Point{}, true
}

func (ss *shapesState) fill(sections *[]pathSection) {
	section := pathSection{subpaths: ss.subpaths, Color: ss.textObj.getFillColor()}
	*sections = append(*sections, section)
	if debugRulings {
		rect := section.bbox()
		fmt.Printf("    FILL: %2d fills (%d new) ss=%s color=%3v %6.2f=%6.2fx%6.2f\n", len(*sections), len(section.subpaths), ss, section.Color, rect, rect.Width(), rect.Height())
		if debugSubpaths {
			for i, path := range section.subpaths {
				fmt.Printf("%8d: %s\n", i, path)
				if i == 10 {
					break
				}
			}
		}
	}
}

func lineToRuling(p0, p1 transform.Point, col color.Color) (*ruling, bool) {
	lr := lineRuling{p0: p0, p1: p1, kind: rulingKindOfLine(p0, p1), Color: col}
	if lr.kind == rulingNone {
		return nil, false
	}
	return lr.asRuling()
}

func (sp *subpath) removeDuplicates() {
	if len(sp.points) == 0 {
		return
	}
	pts := []transform.Point{sp.points[0]}
	for _, pt := range sp.points[1:] {
		if !pointsEqual(pt, pts[len(pts)-1]) {
			pts = append(pts, pt)
		}
	}
	sp.points = pts
}

func (rr rectRuling) asRuling() (*ruling, bool) {
	r := ruling{kind: rr.kind, Color: rr.Color, mark: markKindFill}
	switch rr.kind {
	case rulingVert:
		r.xy = 0.5 * (rr.Llx + rr.Urx)
		r.lo = rr.Lly
		r.hi = rr.Ury
		width, ok := rr.checkWidth(rr.Llx, rr.Urx)
		if !ok {
			if debugRectRulings {
				common.Log.Error("rectRuling.asRuling: rulingVert !checkWidth v=%+v", rr)
			}
			return nil, false
		}
		r.width = width
	case rulingHorz:
		r.xy = 0.5 * (rr.Lly + rr.Ury)
		r.lo = rr.Llx
		r.hi = rr.Urx
		width, ok := rr.checkWidth(rr.Lly, rr.Ury)
		if !ok {
			if debugRectRulings {
				common.Log.Error("rectRuling.asRuling: rulingHorz !checkWidth v=%+v", rr)
			}
			return nil, false
		}
		r.width = width
	default:
		common.Log.Error("bad primary kind=%d", rr.kind)
		return nil, false
	}
	return &r, true
}

type rectRuling struct {
	kind rulingKind
	mark markKind
	color.Color
	model.PdfRectangle
}

func newSubpath(pt transform.Point) *subpath { return &subpath{points: []transform.Point{pt}} }

func (ss *shapesState) quadraticTo(x1, y1, x2, y2 float64) {
	if debugShapes {
		common.Log.Info("quadraticTo:")
	}
	ss.addPoint(x2, y2)
}
func (sp *subpath) last() transform.Point { return sp.points[len(sp.points)-1] }

func (ss *shapesState) stroke(sections *[]pathSection) {
	section := pathSection{subpaths: ss.subpaths, Color: ss.textObj.getStrokeColor()}
	*sections = append(*sections, section)
	if debugRulings {
		fmt.Printf("    STROKE: %d strokes ss=%s color=%+v %6.2f\n", len(*sections), ss, ss.textObj.getStrokeColor(), section.bbox())
		if debugSubpaths {
			for i, sp := range ss.subpaths {
				fmt.Printf("%8d: %s\n", i, sp)
				if i == 10 {
					break
				}
			}
		}
	}
}
func (sp *subpath) clear()           { *sp = subpath{} }
func (lr lineRuling) xMean() float64 { return 0.5 * (lr.p0.X + lr.p1.X) }

func (ss *shapesState) addPoint(x, y float64) {
	sp := ss.establishSubpath()
	pt := ss.devicePoint(x, y)
	if sp == nil {
		ss.fresh = true
		ss.currentPoint = pt
	} else {
		sp.add(pt)
	}
}

func (rr rectRuling) checkWidth(lo, hi float64) (float64, bool) {
	w := hi - lo
	ok := w <= alignTolerance
	return w, ok
}

type pathSection struct {
	subpaths []*subpath
	color.Color
}

func (lr lineRuling) yMean() float64 { return 0.5 * (lr.p0.Y + lr.p1.Y) }

func (ss *shapesState) devicePoint(x, y float64) transform.Point {
	m := ss.parentCTM.Mult(ss.ctm)
	x, y = m.Transform(x, y)
	return transform.NewPoint(x, y)
}

func (ss *shapesState) moveTo(x, y float64) {
	ss.fresh = true
	ss.currentPoint = ss.devicePoint(x, y)
	if debugShapes {
		common.Log.Info("moveTo: %.2f,%.2f device=%.2f", x, y, ss.currentPoint)
	}
}

func makeStrokeRulings(sections []pathSection) rulingList {
	granularize(sections)
	if debugRulings {
		common.Log.Info("makeStrokeRulings: %d strokes", len(sections))
	}
	var rulings rulingList
	for _, section := range sections {
		for _, sp := range section.subpaths {
			if len(sp.points) < 2 {
				continue
			}
			p := sp.points[0]
			for _, next := range sp.points[1:] {
				if r, ok := lineToRuling(p, next, section.Color); ok {
					rulings = append(rulings, r)
				}
				p = next
			}
		}
	}
	if debugRulings {
		common.Log.Info("makeStrokeRulings: %s", rulings)
	}
	return rulings
}

type shapesState struct {
	ctm          transform.Matrix
	parentCTM    transform.Matrix
	subpaths     []*subpath
	fresh        bool
	currentPoint transform.Point
	textObj      *textObject
}

// String returns a human readable description of `path`.
func (sp *subpath) String() string {
	points := sp.points
	numPoints := len(points)
	if numPoints <= 5 {
		return fmt.Sprintf("%d: %6.2f", numPoints, points)
	}
	return fmt.Sprintf("%d: %6.2f %6.2f ... %6.2f", numPoints, points[0], points[1], points[numPoints-1])
}

func (ss *shapesState) lineTo(x, y float64) {
	if debugShapes {
		common.Log.Info("lineTo(%.2f,%.2f p=%.2f", x, y, ss.devicePoint(x, y))
	}
	ss.addPoint(x, y)
}

func (sp *subpath) makeRectRuling(col color.Color) (*ruling, bool) {
	if debugRectRulings {
		common.Log.Info("makeRectRuling: path=%v", sp)
	}
	pts := sp.points[:4]
	kinds := make(map[int]rulingKind, len(pts))
	for i, p0 := range pts {
		p1 := sp.points[(i+1)%4]
		kinds[i] = rulingKindOfSegment(p0, p1)
		if debugRectRulings {
			fmt.Printf("%4d: %s = %6.2f - %6.2f", i, kinds[i], p0, p1)
		}
	}
	if debugRectRulings {
		fmt.Printf("   kinds=%+v\n", kinds)
	}
	var vert, horz []int
	for i, kind := range kinds {
		switch kind {
		case rulingHorz:
			horz = append(horz, i)
		case rulingVert:
			vert = append(vert, i)
		}
	}
	if debugRectRulings {
		fmt.Printf("   horzs=%d %+v\n", len(horz), horz)
		fmt.Printf("   verts=%d %+v\n", len(vert), vert)
	}
	valid := (len(horz) == 2 && len(vert) == 2) || (len(horz) == 2 && len(vert) == 0 && isNearlyHorizontal(pts[horz[0]], pts[horz[1]])) || (len(vert) == 2 && len(horz) == 0 && isFlatSegment(pts[vert[0]], pts[vert[1]]))
	if debugRectRulings {
		fmt.Printf("   horzs=%d verts=%d ok=%t\n", len(horz), len(vert), valid)
	}
	if !valid {
		if debugRectRulings {
			common.Log.Error("!!makeRectRuling: path=%v", sp)
			fmt.Printf("   horzs=%d verts=%d ok=%t\n", len(horz), len(vert), valid)
		}
		return &ruling{}, false
	}
	if len(vert) == 0 {
		for i, kind := range kinds {
			if kind != rulingHorz {
				vert = append(vert, i)
			}
		}
	}
	if len(horz) == 0 {
		for i, kind := range kinds {
			if kind != rulingVert {
				horz = append(horz, i)
			}
		}
	}
	if debugRectRulings {
		common.Log.Info("makeRectRuling: horzs=%d verts=%d points=%d\n"+"\t horzs=%+v\n"+"\t verts=%+v\n"+"\tpoints=%+v", len(horz), len(vert), len(pts), horz, vert, pts)
	}
	var vp0, vp1, hp0, hp1 transform.Point
	if pts[horz[0]].Y > pts[horz[1]].Y {
		hp0, hp1 = pts[horz[0]], pts[horz[1]]
	} else {
		hp0, hp1 = pts[horz[1]], pts[horz[0]]
	}
	if pts[vert[0]].X > pts[vert[1]].X {
		vp0, vp1 = pts[vert[0]], pts[vert[1]]
	} else {
		vp0, vp1 = pts[vert[1]], pts[vert[0]]
	}
	rect := model.PdfRectangle{Llx: vp0.X, Urx: vp1.X, Lly: hp1.Y, Ury: hp0.Y}
	if rect.Llx > rect.Urx {
		rect.Llx, rect.Urx = rect.Urx, rect.Llx
	}
	if rect.Lly > rect.Ury {
		rect.Lly, rect.Ury = rect.Ury, rect.Lly
	}
	rr := rectRuling{PdfRectangle: rect, kind: rectRulingKind(rect), Color: col}
	if rr.kind == rulingNone {
		if debugRectRulings {
			common.Log.Error("makeRectRuling: kind=nil")
		}
		return nil, false
	}
	r, ok := rr.asRuling()
	if !ok {
		if debugRectRulings {
			common.Log.Error("makeRectRuling: !isRuling")
		}
		return nil, false
	}
	if debugRulings {
		fmt.Printf("   r=%s\n", r.String())
	}
	return r, true
}

func (ss *shapesState) drawRectangle(x, y, w, h float64) {
	if debugShapes {
		p0 := ss.devicePoint(x, y)
		p1 := ss.devicePoint(x+w, y+h)
		rect := model.PdfRectangle{Llx: p0.X, Lly: p0.Y, Urx: p1.X, Ury: p1.Y}
		common.Log.Info("drawRectangle: %6.2f", rect)
	}
	ss.newSubPath()
	ss.moveTo(x, y)
	ss.lineTo(x+w, y)
	ss.lineTo(x+w, y+h)
	ss.lineTo(x, y+h)
	ss.closePath()
}

func (ss *shapesState) newSubPath() {
	ss.clearPath()
	if debugShapes {
		common.Log.Info("newSubPath: %s", ss)
	}
}

func (ps pathSection) bbox() model.PdfRectangle {
	start := ps.subpaths[0].points[0]
	rect := model.PdfRectangle{Llx: start.X, Urx: start.X, Lly: start.Y, Ury: start.Y}
	grow := func(p transform.Point) {
		if p.X < rect.Llx {
			rect.Llx = p.X
		} else if p.X > rect.Urx {
			rect.Urx = p.X
		}
		if p.Y < rect.Lly {
			rect.Lly = p.Y
		} else if p.Y > rect.Ury {
			rect.Ury = p.Y
		}
	}
	for _, p := range ps.subpaths[0].points[1:] {
		grow(p)
	}
	for _, sp := range ps.subpaths[1:] {
		for _, p := range sp.points {
			grow(p)
		}
	}
	return rect
}

func (ss *shapesState) cubicTo(x1, y1, x2, y2, x3, y3 float64) {
	if debugShapes {
		common.Log.Info("cubicTo:")
	}
	ss.addPoint(x3, y3)
}

func (sp *subpath) close() {
	if !pointsEqual(sp.points[0], sp.last()) {
		sp.add(sp.points[0])
	}
	sp.closed = true
	sp.removeDuplicates()
}

func (lr lineRuling) asRuling() (*ruling, bool) {
	r := ruling{kind: lr.kind, Color: lr.Color, mark: markKindStroke}
	switch lr.kind {
	case rulingVert:
		r.xy = lr.xMean()
		r.lo = math.Min(lr.p0.Y, lr.p1.Y)
		r.hi = math.Max(lr.p0.Y, lr.p1.Y)
	case rulingHorz:
		r.xy = lr.yMean()
		r.lo = math.Min(lr.p0.X, lr.p1.X)
		r.hi = math.Max(lr.p0.X, lr.p1.X)
	default:
		common.Log.Error("bad primary kind=%d", lr.kind)
		return nil, false
	}
	return &r, true
}

func (ss *shapesState) clearPath() {
	ss.subpaths = nil
	ss.fresh = false
	if debugShapes {
		common.Log.Info("CLEAR: ss=%s", ss)
	}
}

func makeFillRulings(sections []pathSection) rulingList {
	granularize(sections)
	if debugRulings {
		common.Log.Info("makeFillRulings: %d fills", len(sections))
	}
	var rulings rulingList
	for _, section := range sections {
		for _, path := range section.subpaths {
			if !path.isQuadrilateral() {
				if debugRulings {
					common.Log.Error("!isQuadrilateral: %s", path)
				}
				continue
			}
			if r, ok := path.makeRectRuling(section.Color); ok {
				rulings = append(rulings, r)
			} else {
				if debugRectRulings {
					common.Log.Error("!makeRectRuling: %s", path)
				}
			}
		}
	}
	if debugRulings {
		common.Log.Info("makeFillRulings: %s", rulings.String())
	}
	return rulings
}

func rectRulingKind(rect model.PdfRectangle) rulingKind {
	width := rect.Width()
	height := rect.Height()
	if width > height {
		if width >= minRulingLength {
			return rulingHorz
		}
	} else {
		if height >= minRulingLength {
			return rulingVert
		}
	}
	return rulingNone
}

func kindFromDeltas(dx, dy, minLen float64) rulingKind {
	if dx >= minLen && isRatioSmall(dy, dx) {
		return rulingHorz
	}
	if dy >= minLen && isRatioSmall(dx, dy) {
		return rulingVert
	}
	return rulingNone
}

type lineRuling struct {
	kind rulingKind
	mark markKind
	color.Color
	p0, p1 transform.Point
}

func (ss *shapesState) closePath() {
	if ss.fresh {
		ss.subpaths = append(ss.subpaths, newSubpath(ss.currentPoint))
		ss.fresh = false
	} else if len(ss.subpaths) == 0 {
		if debugShapes {
			common.Log.Debug("closePath with no path")
		}
		ss.fresh = false
		return
	}
	ss.subpaths[len(ss.subpaths)-1].close()
	if debugShapes {
		common.Log.Info("closePath: %s", ss)
	}
}

func (ss *shapesState) establishSubpath() *subpath {
	lastpointEstablished, ok := ss.lastpointEstablished()
	if !ok {
		ss.subpaths = append(ss.subpaths, newSubpath(lastpointEstablished))
	}
	if len(ss.subpaths) == 0 {
		return nil
	}
	ss.fresh = false
	return ss.subpaths[len(ss.subpaths)-1]
}
