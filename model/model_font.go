package model

import (
	"fmt"
	_ "image/gif"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
)

// PdfFontDescriptor specifies metrics and other attributes of a font and can refer to a FontFile
// for embedded fonts.
// 9.8 Font Descriptors (page 281)
type PdfFontDescriptor struct {
	FontName     core.PdfObject
	FontFamily   core.PdfObject
	FontStretch  core.PdfObject
	FontWeight   core.PdfObject
	Flags        core.PdfObject
	FontBBox     core.PdfObject
	ItalicAngle  core.PdfObject
	Ascent       core.PdfObject
	Descent      core.PdfObject
	Leading      core.PdfObject
	CapHeight    core.PdfObject
	XHeight      core.PdfObject
	StemV        core.PdfObject
	StemH        core.PdfObject
	AvgWidth     core.PdfObject
	MaxWidth     core.PdfObject
	MissingWidth core.PdfObject
	FontFile     core.PdfObject
	FontFile2    core.PdfObject
	FontFile3    core.PdfObject
	CharSet      core.PdfObject
	flags        int
	missingWidth float64
	*fontFile
	ttf *fonts.TtfType

	// Additional entries for CIDFonts
	Style     core.PdfObject
	Lang      core.PdfObject
	FD        core.PdfObject
	CIDSet    core.PdfObject
	primitive *core.PdfIndirectObject
}

// ToPdfObject returns the PdfFontDescriptor as a PDF dictionary inside an indirect object.
func (d *PdfFontDescriptor) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	if d.primitive == nil {
		d.primitive = &core.PdfIndirectObject{}
	}
	d.primitive.PdfObject = dict
	dict.Set("Type", core.MakeName("FontDescriptor"))
	if d.FontName != nil {
		dict.Set("FontName", d.FontName)
	}
	if d.FontFamily != nil {
		dict.Set("FontFamily", d.FontFamily)
	}
	if d.FontStretch != nil {
		dict.Set("FontStretch", d.FontStretch)
	}
	if d.FontWeight != nil {
		dict.Set("FontWeight", d.FontWeight)
	}
	if d.Flags != nil {
		dict.Set("Flags", d.Flags)
	}
	if d.FontBBox != nil {
		dict.Set("FontBBox", d.FontBBox)
	}
	if d.ItalicAngle != nil {
		dict.Set("ItalicAngle", d.ItalicAngle)
	}
	if d.Ascent != nil {
		dict.Set("Ascent", d.Ascent)
	}
	if d.Descent != nil {
		dict.Set("Descent", d.Descent)
	}
	if d.Leading != nil {
		dict.Set("Leading", d.Leading)
	}
	if d.CapHeight != nil {
		dict.Set("CapHeight", d.CapHeight)
	}
	if d.XHeight != nil {
		dict.Set("XHeight", d.XHeight)
	}
	if d.StemV != nil {
		dict.Set("StemV", d.StemV)
	}
	if d.StemH != nil {
		dict.Set("StemH", d.StemH)
	}
	if d.AvgWidth != nil {
		dict.Set("AvgWidth", d.AvgWidth)
	}
	if d.MaxWidth != nil {
		dict.Set("MaxWidth", d.MaxWidth)
	}
	if d.MissingWidth != nil {
		dict.Set("MissingWidth", d.MissingWidth)
	}
	if d.FontFile != nil {
		dict.Set("FontFile", d.FontFile)
	}
	if d.FontFile2 != nil {
		dict.Set("FontFile2", d.FontFile2)
	}
	if d.FontFile3 != nil {
		dict.Set("FontFile3", d.FontFile3)
	}
	if d.CharSet != nil {
		dict.Set("CharSet", d.CharSet)
	}
	if d.Style != nil {
		dict.Set("FontName", d.FontName)
	}
	if d.Lang != nil {
		dict.Set("Lang", d.Lang)
	}
	if d.FD != nil {
		dict.Set("FD", d.FD)
	}
	if d.CIDSet != nil {
		dict.Set("CIDSet", d.CIDSet)
	}
	return d.primitive
}

func newPdfFontDescriptorFromPdfObject(obj core.PdfObject) (*PdfFontDescriptor, error) {
	desc := &PdfFontDescriptor{}
	obj = core.ResolveReference(obj)
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		desc.primitive = indirect
		obj = indirect.PdfObject
	}
	dict, ok := core.GetDict(obj)
	if !ok {
		common.Log.Debug("ERROR: FontDescriptor not given by a dictionary (%T)", obj)
		return nil, core.ErrTypeError
	}
	if fontName := dict.Get("FontName"); fontName != nil {
		desc.FontName = fontName
	} else {
		common.Log.Debug("Incompatibility: FontName (Required) missing")
	}
	fontNameName, _ := core.GetName(desc.FontName)
	if obj := dict.Get("Type"); obj != nil {
		name, ok := obj.(*core.PdfObjectName)
		if !ok || string(*name) != "FontDescriptor" {
			common.Log.Debug("Incompatibility: Font descriptor Type invalid (%T) font=%q %T", obj, fontNameName, desc.FontName)
		}
	} else {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Incompatibility: Type (Required) missing. font=%q %T", fontNameName, desc.FontName)
		}
	}
	desc.FontFamily = dict.Get("FontFamily")
	desc.FontStretch = dict.Get("FontStretch")
	desc.FontWeight = dict.Get("FontWeight")
	desc.Flags = dict.Get("Flags")
	desc.FontBBox = dict.Get("FontBBox")
	desc.ItalicAngle = dict.Get("ItalicAngle")
	desc.Ascent = dict.Get("Ascent")
	desc.Descent = dict.Get("Descent")
	desc.Leading = dict.Get("Leading")
	desc.CapHeight = dict.Get("CapHeight")
	desc.XHeight = dict.Get("XHeight")
	desc.StemV = dict.Get("StemV")
	desc.StemH = dict.Get("StemH")
	desc.AvgWidth = dict.Get("AvgWidth")
	desc.MaxWidth = dict.Get("MaxWidth")
	desc.MissingWidth = dict.Get("MissingWidth")
	desc.FontFile = dict.Get("FontFile")
	desc.FontFile2 = dict.Get("FontFile2")
	desc.FontFile3 = dict.Get("FontFile3")
	desc.CharSet = dict.Get("CharSet")
	desc.Style = dict.Get("Style")
	desc.Lang = dict.Get("Lang")
	desc.FD = dict.Get("FD")
	desc.CIDSet = dict.Get("CIDSet")
	if desc.Flags != nil {
		if n, ok := core.GetIntVal(desc.Flags); ok {
			desc.flags = n
		}
	}
	if desc.MissingWidth != nil {
		if f, err := core.GetNumberAsFloat(desc.MissingWidth); err == nil {
			desc.missingWidth = f
		}
	}
	if desc.FontFile != nil {
		file, err := newFontFileFromPdfObject(desc.FontFile)
		if err != nil {
			return desc, err
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("fontFile=%s", file)
		}
		desc.fontFile = file
	}
	if desc.FontFile2 != nil {
		fontFile2, err := fonts.NewFontFile2FromPdfObject(desc.FontFile2)
		if err != nil {
			return desc, err
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("fontFile2=%s", fontFile2.String())
		}
		desc.ttf = &fontFile2
	}
	return desc, nil
}

// GetCapHeight returns the CapHeight of the font `descriptor`.
func (d *PdfFontDescriptor) GetCapHeight() (float64, error) {
	return core.GetNumberAsFloat(d.CapHeight)
}

// GetXHeight returns the XHeight of the font `descriptor`.
func (d *PdfFontDescriptor) GetXHeight() (float64, error) {
	return core.GetNumberAsFloat(d.XHeight)
}

// String returns a string describing the font descriptor.
func (d *PdfFontDescriptor) String() string {
	var strs []string
	if d.FontName != nil {
		strs = append(strs, d.FontName.String())
	}
	if d.FontFamily != nil {
		strs = append(strs, d.FontFamily.String())
	}
	if d.fontFile != nil {
		strs = append(strs, d.fontFile.String())
	}
	if d.ttf != nil {
		strs = append(strs, d.ttf.String())
	}
	strs = append(strs, fmt.Sprintf("FontFile3=%t", d.FontFile3 != nil))
	return fmt.Sprintf("FONT_DESCRIPTOR{%s}", strings.Join(strs, ", "))
}

func newPdfFontDescriptorFromTtf(ttf *fonts.TtfType, f float64, n int) *PdfFontDescriptor {
	v := 1000.0 / float64(ttf.UnitsPerEm)
	if ttf.UnitsPerEm == 0 {
		v = 1.0
	}
	desc := &PdfFontDescriptor{FontName: core.MakeName(ttf.PostScriptName), Ascent: core.MakeFloat(v * float64(ttf.TypoAscender)), Descent: core.MakeFloat(v * float64(ttf.TypoDescender)), CapHeight: core.MakeFloat(v * float64(ttf.CapHeight)), FontBBox: core.MakeArrayFromFloats([]float64{v * float64(ttf.Xmin), v * float64(ttf.Ymin), v * float64(ttf.Xmax), v * float64(ttf.Ymax)}), ItalicAngle: core.MakeFloat(ttf.ItalicAngle), MissingWidth: core.MakeFloat(f)}
	if ttf.Bold {
		desc.StemV = core.MakeInteger(120)
	} else {
		desc.StemV = core.MakeInteger(70)
	}
	count := n
	if ttf.IsFixedPitch {
		count |= fontFlagFixedPitch
	}
	if ttf.ItalicAngle != 0 {
		count |= fontFlagItalic
	}
	desc.Flags = core.MakeInteger(int64(count))
	return desc
}

// GetDescent returns the Descent of the font `descriptor`.
func (d *PdfFontDescriptor) GetDescent() (float64, error) {
	return core.GetNumberAsFloat(d.Descent)
}

// GetAscent returns the Ascent of the font `descriptor`.
func (d *PdfFontDescriptor) GetAscent() (float64, error) {
	return core.GetNumberAsFloat(d.Ascent)
}

type fontCommon struct {
	baseFont       string
	subtype        string
	name           string
	toUnicode      core.PdfObject
	toUnicodeCMap  *cmap.CMap
	fontDescriptor *PdfFontDescriptor
	objectNumber   int64
}

func (c fontCommon) asPdfObjectDictionary(s string) *core.PdfObjectDictionary {
	if s != "" && c.subtype != "" && s != c.subtype {
		common.Log.Debug("ERROR: asPdfObjectDictionary. Overriding subtype to %#q %s", s, c)
	} else if s == "" && c.subtype == "" {
		common.Log.Debug("ERROR: asPdfObjectDictionary no subtype. font=%s", c)
	} else if c.subtype == "" {
		c.subtype = s
	}
	dict := core.MakeDict()
	dict.Set("Type", core.MakeName("Font"))
	dict.Set("BaseFont", core.MakeName(c.baseFont))
	dict.Set("Subtype", core.MakeName(c.subtype))
	if c.fontDescriptor != nil {
		dict.Set("FontDescriptor", c.fontDescriptor.ToPdfObject())
	}
	if c.toUnicode != nil {
		dict.Set("ToUnicode", c.toUnicode)
	} else if c.toUnicodeCMap != nil {
		stream, err := c.toUnicodeCMap.Stream()
		if err != nil {
			common.Log.Debug("WARN: could not get CMap stream. err=%v", err)
		} else {
			dict.Set("ToUnicode", stream)
		}
	}
	return dict
}

func (c fontCommon) isCIDFont() bool {
	if c.subtype == "" {
		common.Log.Debug("ERROR: isCIDFont. context is nil. font=%s", c)
	}
	ok := false
	switch c.subtype {
	case "Type0", "CIDFontType0", "CIDFontType2":
		ok = true
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("isCIDFont: isCID=%t font=%s", ok, c)
	}
	return ok
}

func (c fontCommon) coreString() string {
	s := ""
	if c.fontDescriptor != nil {
		s = c.fontDescriptor.String()
	}
	return fmt.Sprintf("%#q %#q %q obj=%d ToUnicode=%t flags=0x%0x %s", c.subtype, c.baseFont, c.name, c.objectNumber, c.toUnicode != nil, c.fontFlags(), s)
}

func newFontCommonFromPdfObject(obj core.PdfObject) (*core.PdfObjectDictionary, *fontCommon, error) {
	fc := &fontCommon{}
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		fc.objectNumber = indirect.ObjectNumber
	}
	dict, ok := core.GetDict(obj)
	if !ok {
		common.Log.Debug("ERROR: Font not given by a dictionary (%T)", obj)
		return nil, nil, ErrFontNotSupported
	}
	s, ok := core.GetNameVal(dict.Get("Type"))
	if !ok {
		common.Log.Debug("ERROR: Font Incompatibility. Type (Required) missing")
		return nil, nil, ErrRequiredAttributeMissing
	}
	if s != "Font" {
		common.Log.Debug("ERROR: Font Incompatibility. Type=%q. Should be %q.", s, "Font")
		return nil, nil, core.ErrTypeError
	}
	subtype, ok := core.GetNameVal(dict.Get("Subtype"))
	if !ok {
		common.Log.Debug("ERROR: Font Incompatibility. Subtype (Required) missing")
		return nil, nil, ErrRequiredAttributeMissing
	}
	fc.subtype = subtype
	name, ok := core.GetNameVal(dict.Get("Name"))
	if ok {
		fc.name = name
	}
	toUnicode := dict.Get("ToUnicode")
	if toUnicode != nil {
		fc.toUnicode = core.TraceToDirectObject(toUnicode)
		cm, err := toUnicodeCMapFromObject(fc.toUnicode, fc)
		if err != nil {
			return dict, fc, err
		}
		fc.toUnicodeCMap = cm
	} else if subtype == "CIDFontType0" || subtype == "CIDFontType2" {
		cidSystemInfo, err := cmap.NewCIDSystemInfo(dict.Get("CIDSystemInfo"))
		if err != nil {
			return dict, fc, err
		}
		s := fmt.Sprintf("%s-%s-UCS2", cidSystemInfo.Registry, cidSystemInfo.Ordering)
		if cmap.IsPredefinedCMap(s) {
			fc.toUnicodeCMap, err = cmap.LoadPredefinedCMap(s)
			if err != nil {
				common.Log.Debug("WARN: could not load predefined CMap %s: %v", s, err)
			}
		}
	}
	fontDescriptor := dict.Get("FontDescriptor")
	if fontDescriptor != nil {
		desc, err := newPdfFontDescriptorFromPdfObject(fontDescriptor)
		if err != nil {
			common.Log.Debug("ERROR: Bad font descriptor. err=%v", err)
			return dict, fc, err
		}
		fc.fontDescriptor = desc
	}
	if subtype != "Type3" {
		baseFont, ok := core.GetNameVal(dict.Get("BaseFont"))
		if !ok {
			common.Log.Debug("ERROR: Font Incompatibility. BaseFont (Required) missing")
			return dict, fc, ErrRequiredAttributeMissing
		}
		fc.baseFont = baseFont
	}
	return dict, fc, nil
}

// String returns a string that describes `base`.
func (c fontCommon) String() string { return fmt.Sprintf("FONT{%s}", c.coreString()) }

func (c fontCommon) fontFlags() int {
	if c.fontDescriptor == nil {
		return 0
	}
	return c.fontDescriptor.flags
}
