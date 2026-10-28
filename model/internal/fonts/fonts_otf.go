package fonts

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/typesetting/font"
	"github.com/OpenMellon/typesetting/font/cff"
	"github.com/OpenMellon/typesetting/font/opentype"
	"github.com/OpenMellon/typesetting/font/opentype/tables"
	"github.com/OpenMellon/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

func (f *OtfType) GetGlyphExtents(glyphID font.GID) (font.GlyphExtents, bool) {
	if f.Face != nil {
		return f.Face.GlyphExtents(glyphID)
	}
	return font.GlyphExtents{}, false
}

func (f *OtfType) GetBitmapSizes() []font.BitmapSize {
	if f.Font != nil {
		return f.Font.BitmapSizes()
	}
	return nil
}

func (f *OtfType) GetGlyphAdvance(glyphID font.GID) fixed.Int26_6 {
	if f.Face != nil {
		adv := f.Face.HorizontalAdvance(glyphID)
		return fixed.Int26_6(adv * 64)
	}
	return 0
}

func (p *otfParser) parsePost() error {
	table := p.readTable("post")
	if L := len(table); L < 32 {
		return fmt.Errorf("reading Post: "+"EOF: expected length: 32, got %d", L)
	}
	formatType := roundTo(readFixed(table[0:]), 1)
	p.otf.ItalicAngle = readFixed(table[4:])
	p.otf.UnderlinePosition = int16(binary.BigEndian.Uint16(table[8:]))
	p.otf.UnderlineThickness = int16(binary.BigEndian.Uint16(table[12:]))
	p.otf.IsFixedPitch = (binary.BigEndian.Uint16(table[16:]) & 1) != 0
	switch formatType {
	case 1.0:
		p.otf.GlyphNames = macGlyphNames
	case 2.0:
		names, _, err := tables.ParsePostNames20(table[32:])
		if err != nil {
			return err
		}
		glyphNames := make([]GlyphName, len(names.Strings))
		for i, s := range names.Strings {
			glyphNames[i] = GlyphName(s)
		}
		p.otf.GlyphNames = append(p.otf.GlyphNames, glyphNames...)
	case 3.0:
		common.Log.Debug("No PostScript name information is provided for the font.")
	default:
		common.Log.Debug("ERROR: Unknown formatType=%f", formatType)
	}
	return nil
}

func (p *otfParser) isImageFont() bool {
	if _, err := p.loader.RawTable(opentype.MustNewTag("EBDT")); err == nil {
		return true
	}
	if _, err := p.loader.RawTable(opentype.MustNewTag("EBLC")); err == nil {
		return true
	}
	if _, err := p.loader.RawTable(opentype.MustNewTag("bloc")); err == nil {
		return true
	}
	if _, err := p.loader.RawTable(opentype.MustNewTag("bdat")); err == nil {
		return true
	}
	if _, err := p.loader.RawTable(opentype.MustNewTag("sbix")); err == nil {
		return true
	}
	if _, err := p.loader.RawTable(opentype.MustNewTag("CBDT")); err == nil {
		return true
	}
	if _, err := p.loader.RawTable(opentype.MustNewTag("CBLC")); err == nil {
		return true
	}
	if _, err := p.loader.RawTable(opentype.MustNewTag("SVG ")); err == nil {
		return true
	}
	return false
}

func (p *otfParser) parseHead() error {
	p.otf.UnitsPerEm = p.font.Upem()
	head, _, err := tables.ParseHead(p.readTable("head"))
	if err != nil {
		return err
	}
	p.otf.IndexToLocFormat = head.IndexToLocFormat
	p.otf.Xmin = head.XMin
	p.otf.Xmax = head.XMax
	p.otf.Ymin = head.YMin
	p.otf.Ymax = head.YMax
	return nil
}

func (p *otfParser) parseCmap() error {
	p.otf.Chars = map[rune]GID{}
	it := p.font.Cmap.Iter()
	for it.Next() {
		r, gid := it.Char()
		p.otf.Chars[r] = GID(gid)
	}
	return nil
}

func (f *OtfType) ShapeText(text string, fontSize float64) shaping.Line {
	if f.Face == nil {
		return nil
	}
	return textencoding.ShapeTextLine(f.Face, text, fontSize)
}

func (f *OtfType) IsColorGlyph(glyphID font.GID) bool {
	if f.Face != nil {
		data := f.Face.GlyphData(glyphID)
		switch data.(type) {
		case *font.GlyphSVG, *font.GlyphBitmap:
			return true
		default:
			return false
		}
	}
	return false
}

func (p *otfParser) parseHmtx() error {
	hmtx, _, err := tables.ParseHmtx(p.readTable("hmtx"), int(p.numHMetrics), int(p.numGlyphs)-int(p.numHMetrics))
	if err != nil {
		return err
	}
	for _, m := range hmtx.Metrics {
		p.otf.Widths = append(p.otf.Widths, uint16(m.AdvanceWidth))
	}
	return nil
}

type otfParser struct {
	otf         OtfType
	reader      io.ReadSeeker
	loader      *opentype.Loader
	font        *font.Font
	fontPage    tables.FontPage
	numHMetrics uint16
	numGlyphs   uint16
}

func (p *otfParser) parseHhea() error {
	hhea, _, err := tables.ParseHhea(p.readTable("hhea"))
	if err != nil {
		return err
	}
	p.numHMetrics = hhea.NumOfLongMetrics
	return nil
}

func OtfParse(r io.ReadSeeker) (OtfType, error) { p := &otfParser{reader: r}; return p.Parse() }

func (f *OtfType) NewEncoder() textencoding.TextEncoder {
	return textencoding.NewOpenTypeFontEncoder(f.Face, f.Chars)
}

func (p *otfParser) parseName() error {
	name, _, err := tables.ParseName(p.readTable("name"))
	if err != nil {
		return err
	}
	p.otf.PostScriptName = name.Name(6)
	return nil
}

type OtfType struct {
	*TtfType
	Loader           *opentype.Loader
	Font             *font.Font
	Face             *font.Face
	IndexToLocFormat int16
	Glyf             tables.Glyf
	CFF              *cff.CFF
	CFF2             *cff.CFF2
	OutlineType      OTFOutlineType
	RawData          []byte
}

const (
	OTFOutlineTypePostScript OTFOutlineType = 1
	OTFOutlineTypeTrueType   OTFOutlineType = 2
)

func (p *otfParser) Parse() (OtfType, error) {
	data, err := io.ReadAll(p.reader)
	if err != nil {
		common.Log.Debug("ERROR: Unable to read font contents: %v", err)
		return OtfType{}, err
	}
	r := bytes.NewReader(data)
	p.loader, err = opentype.NewLoader(r)
	if err != nil {
		common.Log.Debug("ERROR: Unable to create loader: %v", err)
		return OtfType{}, err
	}
	p.font, err = font.NewFont(p.loader)
	if err != nil {
		common.Log.Debug("ERROR: Unable to create font: %v", err)
		return OtfType{}, err
	}
	p.otf = OtfType{TtfType: &TtfType{}, Loader: p.loader, Font: p.font, Face: font.NewFace(p.font)}
	if err = p.parseComponents(); err != nil {
		return OtfType{}, err
	}
	if p.otf.CFF != nil || p.otf.CFF2 != nil {
		p.otf.OutlineType = OTFOutlineTypePostScript
	} else if p.otf.Glyf != nil {
		p.otf.OutlineType = OTFOutlineTypeTrueType
	} else if p.isImageFont() {
		common.Log.Info("Font appears to be an image font (color bitmap or SVG based)")
	} else {
		return OtfType{}, fmt.Errorf("unable to determine outline type: no CFF, CFF2 or Glyf table found")
	}
	return p.otf, nil
}

func (p *otfParser) readTable(tag string) []byte {
	data, err := p.loader.RawTable(opentype.MustNewTag(tag))
	if err != nil {
		common.Log.Debug("ERROR: Unable to load table: %v", err)
	}
	return data
}

func (p *otfParser) parseOS2() error {
	os2, _, err := tables.ParseOs2(p.readTable("OS/2"))
	if err != nil {
		return err
	}
	p.otf.Bold = (os2.FsSelection & 32) != 0
	p.otf.TypoAscender = os2.STypoAscender
	p.otf.TypoDescender = os2.STypoDescender
	p.otf.CapHeight = 0
	p.fontPage = os2.FontPage()
	if os2.Version >= 2 {
		p.otf.CapHeight = int16(binary.BigEndian.Uint16(os2.HigherVersionData[10:]))
	}
	return nil
}

func (p *otfParser) parseGlyf() error {
	var glyf tables.Glyf
	glyfData, err := p.loader.RawTable(opentype.MustNewTag("glyf"))
	if err != nil {
		return err
	}
	locaData, err := p.loader.RawTable(opentype.MustNewTag("loca"))
	if err != nil {
		return err
	}
	loca, err := tables.ParseLoca(locaData, int(p.numGlyphs), p.otf.IndexToLocFormat == 1)
	if err == nil {
		glyf, _ = tables.ParseGlyf(glyfData, loca)
	}
	p.otf.Glyf = glyf
	return nil
}

type OTFOutlineType int

func (p *otfParser) parseCFF() error {
	var cffFont *cff.CFF
	data, err := p.loader.RawTable(opentype.MustNewTag("CFF "))
	if err != nil {
		return err
	}
	cffFont, err = cff.Parse(data)
	if err != nil {
		return err
	}
	p.otf.CFF = cffFont
	return nil
}

func (p *otfParser) parseCFF2() error {
	var cff2 *cff.CFF2
	data, err := p.loader.RawTable(opentype.MustNewTag("CFF2"))
	if err != nil {
		return err
	}
	cff2, err = cff.ParseCFF2(data)
	if err != nil {
		return err
	}
	p.otf.CFF2 = cff2
	return nil
}

func (p *otfParser) parseComponents() error {
	if err := p.parseHead(); err != nil {
		return err
	}
	if err := p.parseHhea(); err != nil {
		return err
	}
	if err := p.parseMaxp(); err != nil {
		return err
	}
	if err := p.parseHmtx(); err != nil {
		return err
	}
	if err := p.parseName(); err != nil {
		return err
	}
	if err := p.parseOS2(); err != nil {
		return err
	}
	if err := p.parsePost(); err != nil {
		return err
	}
	if err := p.parseCmap(); err != nil {
		return err
	}
	err := p.parseGlyf()
	if err != nil {
		common.Log.Debug("Glyf table not found: %v", err)
	}
	err = p.parseCFF()
	if err != nil {
		common.Log.Debug("CFF table not found: %v", err)
	}
	err = p.parseCFF2()
	if err != nil {
		common.Log.Debug("CFF2 table not found: %v", err)
	}
	return nil
}

func (p *otfParser) parseMaxp() error {
	maxp, _, err := tables.ParseMaxp(p.readTable("maxp"))
	if err != nil {
		return err
	}
	p.numGlyphs = maxp.NumGlyphs
	return nil
}
