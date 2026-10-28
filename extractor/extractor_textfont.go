package extractor

import (
	"errors"
	"sort"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func (to *textObject) getFontDict(name string) (fontObj core.PdfObject, err error) {
	resources := to.resources
	if resources == nil {
		common.Log.Debug("getFontDict. No resources. name=%#q", name)
		return nil, nil
	}
	fontObj, ok := resources.GetFontByName(core.PdfObjectName(name))
	if !ok {
		common.Log.Debug("ERROR: getFontDict: Font not found: name=%#q", name)
		return nil, errors.New("font not in resources")
	}
	return fontObj, nil
}

func hasDirectObject(marks *TextMarkArray) bool {
	return len(marks.marks) > 0 && marks.marks[0].DirectObject != nil
}

func (to *textObject) getFontDirect(name string) (*model.PdfFont, error) {
	fontDict, err := to.getFontDict(name)
	if err != nil {
		return nil, err
	}
	font, err := model.NewPdfFontFromPdfObject(fontDict)
	if err != nil {
		common.Log.Debug("getFontDirect: NewPdfFontFromPdfObject failed. name=%#q err=%v", name, err)
	}
	return font, err
}

// PageFonts represents extracted fonts on a PDF page.
type PageFonts struct{ Fonts []Font }

func containsFont(fonts []Font, name string) bool {
	for _, font := range fonts {
		if font.FontName == name {
			return true
		}
	}
	return false
}

func (to *textObject) setFont(name string, size float64) error {
	if to == nil {
		return nil
	}
	to.state.tfs = size
	font, err := to.getFont(name)
	if err != nil {
		return err
	}
	to.state.tfont = font
	return nil
}

func (to *textObject) getFont(name string) (*model.PdfFont, error) {
	if to.extractor.fonts != nil {
		dict, err := to.getFontDict(name)
		if err != nil {
			common.Log.Debug("ERROR: getFont: name=%s, error: %s", name, err.Error())
			return nil, err
		}
		to.extractor.fontClock++
		entry, ok := to.extractor.fonts[dict.String()]
		if ok {
			entry.lastUsed = to.extractor.fontClock
			return entry.font, nil
		}
	}
	dict, err2 := to.getFontDict(name)
	if err2 != nil {
		return nil, err2
	}
	fontDirect, err2 := to.getFontDirect(name)
	if err2 != nil {
		return nil, err2
	}
	if to.extractor.fonts != nil {
		entry := fontEntry{fontDirect, to.extractor.fontClock}
		if len(to.extractor.fonts) >= maxCachedFonts {
			var keys []string
			for key := range to.extractor.fonts {
				keys = append(keys, key)
			}
			sort.Slice(keys, func(i, j int) bool {
				return to.extractor.fonts[keys[i]].lastUsed < to.extractor.fonts[keys[j]].lastUsed
			})
			delete(to.extractor.fonts, keys[0])
		}
		to.extractor.fonts[dict.String()] = entry
	}
	return fontDirect, nil
}

// Font represents the font properties on a PDF page.
type Font struct {
	PdfFont *model.PdfFont

	// FontName represents Font Name from font properties.
	FontName string

	// FontType represents Font Subtype entry in the font dictionary inside page resources.
	// Examples : type0, Type1, MMType1, Type3, TrueType, CIDFont.
	FontType string

	// ToUnicode is true if font provides a `ToUnicode` mapping.
	ToUnicode bool

	// IsCID is true if underlying font is a composite font.
	// Composite font is represented by a font dictionary whose Subtype is `Type0`
	IsCID bool

	// IsSimple is true if font is simple font.
	// A simple font is limited to only 8 bit (255) character codes.
	IsSimple bool

	// FontData represents the raw data of the embedded font file.
	// It can have format TrueType (TTF), PostScript Font (PFB) or Compact Font Format (CCF).
	// FontData value can be indicates from `FontFile`, `FontFile2` or `FontFile3` inside Font Descriptor.
	// At most, only one of `FontFile`, `FontFile2` or `FontFile3` will be FontData value.
	FontData []byte

	// FontFileName is a name representing the font. it has format:
	// (Font Name) + (Font Type Extension), example: helvetica.ttf.
	FontFileName string

	// FontDescriptor represents metrics and other attributes inside font properties from PDF Structure (Font Descriptor).
	FontDescriptor *model.PdfFontDescriptor
}

func (to *textObject) getCurrentFont() *model.PdfFont {
	tfont := to.state.tfont
	if tfont == nil {
		common.Log.Debug("ERROR: No font defined. Using default.")
		return model.DefaultFont()
	}
	return tfont
}

// ExtractFonts returns all font information from the page extractor, including
// font name, font type, the raw data of the embedded font file (if embedded), font descriptor and more.
//
// The argument `previousPageFonts` is used when trying to build a complete font catalog for multiple pages or the entire document.
// The entries from `previousPageFonts` are added to the returned result unless already included in the page, i.e. no duplicate entries.
//
// NOTE: If previousPageFonts is nil, all fonts from the page will be returned. Use it when building up a full list of fonts for a document or page range.
func (e *Extractor) ExtractFonts(previousPageFonts *PageFonts) (*PageFonts, error) {
	fonts := PageFonts{}
	err := fonts.extractPageResourcesToFont(e.resources)
	if err != nil {
		return nil, err
	}
	if previousPageFonts != nil {
		for _, font := range previousPageFonts.Fonts {
			if !containsFont(fonts.Fonts, font.FontName) {
				fonts.Fonts = append(fonts.Fonts, font)
			}
		}
	}
	return &PageFonts{Fonts: fonts.Fonts}, nil
}

func (pf *PageFonts) extractPageResourcesToFont(resources *model.PdfPageResources) error {
	if resources.Font == nil {
		return errors.New(msgNoFontFound)
	}
	fontDict, ok := core.GetDict(resources.Font)
	if !ok {
		return errors.New(msgFontConvert)
	}
	for _, key := range fontDict.Keys() {
		var (
			hasToUnicode = true
			data         []byte
			fileName     string
		)
		fontObj, ok2 := resources.GetFontByName(key)
		if !ok2 {
			return errors.New(msgFontPropsNotFound)
		}
		pdfFont, err := model.NewPdfFontFromPdfObject(fontObj)
		if err != nil {
			return err
		}
		descriptor := pdfFont.FontDescriptor()
		fontName := pdfFont.FontDescriptor().FontName.String()
		subtype := pdfFont.Subtype()
		if containsFont(pf.Fonts, fontName) {
			continue
		}
		if len(pdfFont.ToUnicode()) == 0 {
			hasToUnicode = false
		}
		if descriptor.FontFile != nil {
			if stream, ok3 := core.GetStream(descriptor.FontFile); ok3 {
				data, err = core.DecodeStream(stream)
				if err != nil {
					return err
				}
				fileName = fontName + ".pfb"
			}
		} else if descriptor.FontFile2 != nil {
			if stream, ok3 := core.GetStream(descriptor.FontFile2); ok3 {
				data, err = core.DecodeStream(stream)
				if err != nil {
					return err
				}
				fileName = fontName + ".ttf"
			}
		} else if descriptor.FontFile3 != nil {
			if stream, ok3 := core.GetStream(descriptor.FontFile3); ok3 {
				data, err = core.DecodeStream(stream)
				if err != nil {
					return err
				}
				fileName = fontName + ".cff"
			}
		}
		if len(fileName) < 1 {
			common.Log.Debug(msgFontStreamInvalid)
		}
		font := Font{FontName: fontName, PdfFont: pdfFont, IsCID: pdfFont.IsCID(), IsSimple: pdfFont.IsSimple(), ToUnicode: hasToUnicode, FontType: subtype, FontData: data, FontFileName: fileName, FontDescriptor: descriptor}
		pf.Fonts = append(pf.Fonts, font)
	}
	return nil
}

type fontEntry struct {
	font     *model.PdfFont
	lastUsed int64
}
