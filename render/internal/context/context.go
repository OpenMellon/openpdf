// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package context

import (
	"errors"
	"github.com/OpenMellon/freetype/truetype"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
	font2 "golang.org/x/image/font"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

func (ts *TextState) ProcTD(tx, ty float64) { ts.Tl = -ty; ts.ProcTd(tx, ty) }
func (ts *TextState) Reset() {
	ts.Tm = transform.IdentityMatrix()
	ts.Tlm = transform.IdentityMatrix()
}
func (ts *TextState) ProcTd(tx, ty float64) {
	ts.Tlm.Concat(transform.TranslationMatrix(tx, ty))
	ts.Tm = ts.Tlm.Clone()
}

type BlendMode int

const maxCachedFaces = 16
const (
	BlendModeNormal BlendMode = iota
	BlendModeMultiply
	BlendModeScreen
	BlendModeOverlay
	BlendModeDarken
	BlendModeLighten
	BlendModeColorDodge
	BlendModeColorBurn
	BlendModeHardLight
	BlendModeSoftLight
	BlendModeDifference
	BlendModeExclusion
	BlendModeHue
	BlendModeSaturation
	BlendModeColor
	BlendModeLuminosity
)

func (f *TextFont) Clone() *TextFont {
	clone := *f
	clone.mu = &sync.Mutex{}
	clone.faces = map[int64]font2.Face{}
	clone.faceIDs = map[font2.Face]uint64{}
	return &clone
}

type TextState struct {
	Tc          float64
	Tw          float64
	Th          float64
	Tl          float64
	Tf          *TextFont
	Ts          float64
	Tm          transform.Matrix
	Tlm         transform.Matrix
	Tr          TextRenderingMode
	GlobalScale float64
}

var faceIDCounter atomic.Uint64

type ColorConverter func(r, g, b uint8) (uint8, uint8, uint8)
type LineCap int

const (
	LineCapRound LineCap = iota
	LineCapButt
	LineCapSquare
)

func NewTextFont(pdfFont *model.PdfFont, size float64) (*TextFont, error) {
	desc := pdfFont.FontDescriptor()
	if desc == nil {
		return nil, errors.New("could not get font descriptor")
	}
	stream, ok := core.GetStream(desc.FontFile2)
	if !ok {
		return nil, errors.New("missing font file stream")
	}
	data, err := core.DecodeStream(stream)
	if err != nil {
		return nil, err
	}
	ttf, err := truetype.Parse(data)
	if err != nil {
		return nil, err
	}
	name := pdfFont.FontDescriptor().FontName.String()
	subset := len(name) > 7 && name[6] == '+'
	if desc.Flags != nil {
		flags, convErr := strconv.Atoi(desc.Flags.String())
		if convErr == nil && flags == 32 {
			subset = false
		}
	}
	if !ttf.HasCmap() && (!strings.Contains(pdfFont.Encoder().String(), "Identity-") || !subset) {
		return nil, errors.New("no cmap and encoding is not identity")
	}
	return &TextFont{Font: pdfFont, Size: size, ttFont: ttf, mu: &sync.Mutex{}, faces: map[int64]font2.Face{}, faceIDs: map[font2.Face]uint64{}}, nil
}
func (f *TextFont) BytesToCharcodes(data []byte) []textencoding.CharCode {
	if f.originalFont != nil {
		return f.originalFont.BytesToCharcodes(data)
	}
	return f.Font.BytesToCharcodes(data)
}

type TextFont struct {
	Font         *model.PdfFont
	Size         float64
	ttFont       *truetype.Font
	originalFont *model.PdfFont
	mu           *sync.Mutex
	faces        map[int64]font2.Face
	faceIDs      map[font2.Face]uint64
}
type FillRule int

func (ts *TextState) ProcTf(font *TextFont) { ts.Tf = font }

type Gradient interface {
	Pattern
	AddColorStop(pos float64, c color.Color)
}

func (f *TextFont) NewFace(size float64) font2.Face {
	key := int64(math.Round(size * faceSizeScale))
	f.mu.Lock()
	defer f.mu.Unlock()
	if face, ok := f.faces[key]; ok {
		return face
	}
	if len(f.faces) >= maxCachedFaces {
		clear(f.faces)
		clear(f.faceIDs)
	}
	newFace := truetype.NewFace(f.ttFont, &truetype.Options{Size: size})
	f.faces[key] = newFace
	f.faceIDs[newFace] = faceIDCounter.Add(1)
	return newFace
}
func (ts *TextState) ProcTm(a, b, c, d, e, f float64) {
	ts.Tm = transform.NewMatrix(a, b, c, d, e, f)
	ts.Tlm = ts.Tm.Clone()
}

type TransparencyGroup struct {
	Convert  ColorConverter
	Isolated bool
	Knockout bool
}

func (ts *TextState) ProcTStar() { ts.ProcTd(0, -ts.Tl) }

const (
	LineJoinRound LineJoin = iota
	LineJoinBevel
)

type Context interface {
	Push()
	Pop()
	Matrix() transform.Matrix
	SetMatrix(m transform.Matrix)
	Translate(x, y float64)
	Scale(sx, sy float64)
	Rotate(angle float64)
	MoveTo(x, y float64)
	LineTo(x, y float64)
	CubicTo(x1, y1, x2, y2, x3, y3 float64)
	QuadraticTo(x1, y1, x2, y2 float64)
	NewSubPath()
	ClosePath()
	ClearPath()
	SavePath()
	RestorePath()
	Clip()
	ClipPreserve()
	ResetClip()
	ClipBounds() image.Rectangle
	LineWidth() float64
	SetLineWidth(width float64)
	SetLineCap(lineCap LineCap)
	SetLineJoin(lineJoin LineJoin)
	SetDash(dashes ...float64)
	SetDashOffset(offset float64)
	Fill()
	FillPreserve()
	Stroke()
	StrokePreserve()
	SetRGBA(r, g, b, a float64)
	SetFillRGBA(r, g, b, a float64)
	SetFillStyle(pattern Pattern)
	SetFillRule(rule FillRule)
	SetStrokeRGBA(r, g, b, a float64)
	SetStrokeStyle(pattern Pattern)
	FillPattern() Pattern
	StrokePattern() Pattern
	TextState() *TextState
	DrawString(s string, face font2.Face, x, y float64)
	MeasureString(s string, face font2.Face) (w, h float64)
	DrawRectangle(x, y, w, h float64)
	DrawImage(im image.Image, x, y int)
	DrawImageAnchored(im image.Image, x, y int, ax, ay float64)
	Height() int
	Width() int
	BlendMode() BlendMode
	SetBlendMode(mode BlendMode)
	FillAlpha() float64
	SetFillAlpha(alpha float64)
	StrokeAlpha() float64
	SetStrokeAlpha(alpha float64)
	SetSoftMask(mask *image.Alpha)
	BeginTransparencyGroup(group TransparencyGroup)
	EndTransparencyGroup()
}

func (f *TextFont) WithSize(size float64, originalFont *model.PdfFont) *TextFont {
	clone := *f
	clone.Size = size
	clone.originalFont = originalFont
	return &clone
}
func (ts *TextState) ProcTj(data []byte, ctx Context) {
	fontSize := ts.Tf.Size
	hScale := ts.Th / 100.0
	globalScale := ts.GlobalScale
	trm := transform.NewMatrix(fontSize*hScale, 0, 0, fontSize, 0, ts.Ts)
	ctm := ctx.Matrix()
	scaleY := ctm.Clone().Mult(ts.Tm.Clone().Mult(trm)).ScalingFactorY()
	face := ts.Tf.NewFace(scaleY)
	codes := ts.Tf.BytesToCharcodes(data)
	for _, code := range codes {
		glyphCode, runes := ts.Tf.CharcodeToRunes(code)
		text := string(runes)
		if text == "\x00" {
			continue
		}
		m := ctm.Clone().Mult(ts.Tm.Clone().Mult(trm))
		scale := m.ScalingFactorY()
		m = m.Scale(1/scale, -1/scale)
		if ts.Tr != TextRenderingModeInvisible {
			ctx.SetMatrix(m)
			ctx.DrawString(text, face, 0, 0)
			ctx.SetMatrix(ctm)
		}
		wordSpace := 0.0
		if text == " " {
			wordSpace = ts.Tw
		}
		advance, _, hasMetrics := ts.Tf.GetCharMetrics(glyphCode)
		if hasMetrics {
			advance = advance * 0.001 * fontSize
		} else {
			advance, _ = ctx.MeasureString(text, face)
			advance = advance / globalScale
		}
		tx := (advance + ts.Tc + wordSpace) * hScale
		ts.Tm = ts.Tm.Mult(transform.TranslationMatrix(tx, 0))
	}
}
func NewTextFontFromPath(filePath string, size float64) (*TextFont, error) {
	pdfFont, err := model.NewPdfFontFromTTFFile(filePath)
	if err != nil {
		return nil, err
	}
	return NewTextFont(pdfFont, size)
}
func (ts *TextState) ProcQ(data []byte, ctx Context) {
	ts.ProcTStar()
	ts.ProcTj(data, ctx)
}

type TextRenderingMode int
type LineJoin int

const (
	FillRuleWinding FillRule = iota
	FillRuleEvenOdd
)

func (f *TextFont) GetCharMetrics(code textencoding.CharCode) (float64, float64, bool) {
	if m, ok := f.Font.GetCharMetrics(code); ok && m.Wx != 0 {
		return m.Wx, m.Wy, ok
	}
	if f.originalFont == nil {
		return 0, 0, false
	}
	orig, origOk := f.originalFont.GetCharMetrics(code)
	return orig.Wx, orig.Wy, origOk && orig.Wx != 0
}

type Pattern interface{ ColorAt(x, y int) color.Color }

func (ts *TextState) ProcDQ(data []byte, aw, ac float64, ctx Context) {
	ts.Tw = aw
	ts.Tc = ac
	ts.ProcQ(data, ctx)
}
func (f *TextFont) charcodeToRunesSimple(code textencoding.CharCode) (textencoding.CharCode, []rune) {
	codes := []textencoding.CharCode{code}
	if f.Font.IsSimple() && f.ttFont != nil {
		if idx := f.ttFont.Index(rune(code)); idx > 0 {
			return code, []rune{rune(code)}
		}
	}
	if f.ttFont != nil && !f.ttFont.HasCmap() && strings.Contains(f.Font.Encoder().String(), "Identity-") {
		if enc, ok := f.Font.Encoder().(textencoding.GIDEncoder); ok {
			gid, found := enc.CharcodeToGID(code)
			if !found {
				return code, []rune{rune(0)}
			}
			if glyphIdx := f.ttFont.Index(rune(gid)); glyphIdx > 0 {
				return code, []rune{rune(gid)}
			}
			return code, []rune{rune(0)}
		}
	}
	return code, f.Font.CharcodesToUnicode(codes)
}
func (ts *TextState) Translate(tx, ty float64) {
	ts.Tm = ts.Tm.Mult(transform.TranslationMatrix(tx, ty))
}
func (f *TextFont) FaceID(face font2.Face) (uint64, bool) {
	f.mu.Lock()
	id, ok := f.faceIDs[face]
	f.mu.Unlock()
	return id, ok
}

const faceSizeScale = 1000

func (f *TextFont) CharcodeToRunes(charcode textencoding.CharCode) (textencoding.CharCode, []rune) {
	codes := []textencoding.CharCode{charcode}
	if f.originalFont == nil || f.originalFont == f.Font {
		return f.charcodeToRunesSimple(charcode)
	}
	runes := f.originalFont.CharcodesToUnicode(codes)
	encoded, _ := f.Font.RunesToCharcodeBytes(runes)
	mapped := f.Font.BytesToCharcodes(encoded)
	outCode := charcode
	if len(mapped) > 0 && mapped[0] != 0 {
		outCode = mapped[0]
	}
	if string(runes) == string(cmap.MissingCodeRune) && f.originalFont.BaseFont() == f.Font.BaseFont() {
		return f.charcodeToRunesSimple(charcode)
	}
	return outCode, runes
}
func NewTextState() TextState {
	return TextState{Th: 100, Tm: transform.IdentityMatrix(), Tlm: transform.IdentityMatrix()}
}

const (
	TextRenderingModeFill TextRenderingMode = iota
	TextRenderingModeStroke
	TextRenderingModeFillStroke
	TextRenderingModeInvisible
	TextRenderingModeFillClip
	TextRenderingModeStrokeClip
	TextRenderingModeFillStrokeClip
	TextRenderingModeClip
)
