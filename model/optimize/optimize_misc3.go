package optimize

import (
	"bytes"
	"errors"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/extractor"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/unitype"
)

func subsetFont(stream *core.PdfObjectStream, runes []rune, glyphs []unitype.GlyphIndex) error {
	stream, ok := core.GetStream(stream)
	if !ok {
		common.Log.Debug("Embedded font object not found -- ABORT subsetting")
		return errors.New("fontfile2 not found")
	}
	data, err := core.DecodeStream(stream)
	if err != nil {
		common.Log.Debug("Decode error: %v", err)
		return err
	}
	font, err := unitype.Parse(bytes.NewReader(data))
	if err != nil {
		common.Log.Debug("Error parsing %d byte font", len(stream.Stream))
		return err
	}
	indices := glyphs
	if len(runes) > 0 {
		runeGlyphs := font.LookupRunes(runes)
		indices = append(indices, runeGlyphs...)
	}
	font, err = font.SubsetKeepIndices(indices)
	if err != nil {
		common.Log.Debug("ERROR subsetting font: %v", err)
		return err
	}
	var buf bytes.Buffer
	err = font.Write(&buf)
	if err != nil {
		common.Log.Debug("ERROR Writing font: %v", err)
		return err
	}
	if buf.Len() > len(data) {
		common.Log.Debug("Re-written font is larger than original - skip")
		return nil
	}
	newStream, err := core.MakeStream(buf.Bytes(), core.NewFlateEncoder())
	if err != nil {
		common.Log.Debug("ERROR Writing font: %v", err)
		return err
	}
	*stream = *newStream
	stream.Set("Length1", core.MakeInteger(int64(buf.Len())))
	return nil
}

func subsetFonts(objects []core.PdfObject) (subsetted map[*core.PdfObjectStream]struct{}, err error) {
	subsetted = map[*core.PdfObjectStream]struct{}{}
	fonts := map[*model.PdfFont]struct{}{}
	structure := findObjectStructure(objects)
	for _, page := range structure.pageObjects {
		pageDict, ok := core.GetDict(page.PdfObject)
		if !ok {
			continue
		}
		resDict, ok := core.GetDict(pageDict.Get("Resources"))
		if !ok {
			continue
		}
		contents, _ := decodeContents(pageDict.Get("Contents"))
		resources, err := model.NewPdfPageResourcesFromDict(resDict)
		if err != nil {
			return nil, err
		}
		items := []content{{contents: contents, resources: resources}}
		annots := annotationContents(pageDict.Get("Annots"))
		if annots != nil {
			items = append(items, annots...)
		}
		for _, item := range items {
			ext, err := extractor.NewFromContents(item.contents, item.resources)
			if err != nil {
				return nil, err
			}
			pageText, _, _, err := ext.ExtractPageText()
			if err != nil {
				return nil, err
			}
			for _, mark := range pageText.Marks().Elements() {
				if mark.Font == nil {
					continue
				}
				if _, ok := fonts[mark.Font]; !ok {
					fonts[mark.Font] = struct{}{}
				}
			}
		}
	}
	fontsByStream := map[*core.PdfObjectStream][]*model.PdfFont{}
	for font := range fonts {
		desc := font.FontDescriptor()
		if desc == nil || desc.FontFile2 == nil {
			continue
		}
		fontFile, ok := core.GetStream(desc.FontFile2)
		if !ok {
			continue
		}
		fontsByStream[fontFile] = append(fontsByStream[fontFile], font)
	}
	for fontStream := range fontsByStream {
		var runes []rune
		var glyphs []unitype.GlyphIndex
		for _, f := range fontsByStream[fontStream] {
			switch enc := f.Encoder().(type) {
			case textencoding.GIDRegistry:
				for _, gid := range enc.RegisteredGIDs() {
					glyphs = append(glyphs, unitype.GlyphIndex(gid))
				}
			case textencoding.RuneRegistry:
				runes = append(runes, enc.RegisteredRunes()...)
			case textencoding.SimpleEncoder:
				codes := enc.Charcodes()
				for _, code := range codes {
					r, ok := enc.CharcodeToRune(code)
					if !ok {
						common.Log.Debug("Charcode<->rune not found: %d", code)
						continue
					}
					runes = append(runes, r)
				}
			}
			if shaped, ok := f.Encoder().(textencoding.ShapedGlyphRegistry); ok {
				for _, gid := range shaped.RegisteredShapedGIDs() {
					glyphs = append(glyphs, unitype.GlyphIndex(gid))
				}
			}
		}
		if len(runes) == 0 && len(glyphs) == 0 {
			common.Log.Debug("No registered runes or indices - skip subsetting")
			continue
		}
		err = subsetFont(fontStream, runes, glyphs)
		if err != nil {
			common.Log.Debug("ERROR subsetting font stream: %v", err)
			return nil, err
		}
		subsetted[fontStream] = struct{}{}
	}
	return subsetted, nil
}
