package render

import (
	"errors"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/render/internal/context"
	"github.com/adrg/sysfont"
)

func (r *renderer) setFontOperator(ctx context.Context, ts *context.TextState, finder *sysfont.Finder, op *contentstream.ContentStreamOperation, res *model.PdfPageResources) error {
	if len(op.Params) != 2 {
		return errRangeCheck
	}
	common.Log.Debug("%#v", op.Params)
	fontName, ok := core.GetName(op.Params[0])
	if !ok || fontName == nil {
		common.Log.Debug("invalid font name object: %v", op.Params[0])
		return errTypeCheck
	}
	common.Log.Debug("Font name: %s", fontName.String())
	size, err := core.GetNumberAsFloat(op.Params[1])
	if err != nil {
		common.Log.Debug("invalid font size object: %v", op.Params[1])
		return errTypeCheck
	}
	maxDim := ctx.Width()
	if ctx.Height() > maxDim {
		maxDim = ctx.Height()
	}
	maxSize := float64(maxDim)
	if size > maxSize {
		common.Log.Debug("Font size exceeds image dimension, scaling down font size from %v to %v", size, maxSize)
		size = maxSize
	}
	common.Log.Debug("Font size: %v", size)
	fontObj, fontFound := res.GetFontByName(*fontName)
	if !fontFound {
		common.Log.Debug("ERROR: Font %s not found", fontName.String())
		return errors.New("font not found")
	}
	common.Log.Debug("Font: %T", fontObj)
	fontDict, ok := core.GetDict(fontObj)
	if !ok {
		common.Log.Debug("ERROR: could not get font dict")
		return errTypeCheck
	}
	pdfFont, ok := r.pdfFontByDict[fontDict]
	if !ok {
		pdfFont, err = model.NewPdfFontFromPdfObject(fontDict)
		if err != nil {
			common.Log.Debug("ERROR: could not load font from object")
			return err
		}
		r.pdfFontByDict[fontDict] = pdfFont
	}
	baseFont := pdfFont.BaseFont()
	if baseFont == "" {
		baseFont = fontName.String()
	}
	textFont, ok := r.textFontByDict[fontDict]
	if !ok {
		textFont, err = context.NewTextFont(pdfFont, size)
		if err != nil {
			r.noteDecodeLimit(err)
			if r.decodeLimitErr != nil {
				return r.decodeLimitErr
			}
			common.Log.Debug("ERROR: %v", err)
		}
	}
	if textFont == nil {
		if cached, cachedOK := r.fontByName[baseFont]; cachedOK {
			textFont = cached
		}
	}
	if textFont == nil {
		origName := baseFont
		if len(baseFont) > 7 && baseFont[6] == '+' {
			baseFont = baseFont[7:]
		}
		bold, italic, serif := fontStyleHints(baseFont, pdfFont.FontDescriptor())
		candidates := fontNameCandidates(baseFont, bold, italic, serif)
		loadFont := func(sf *sysfont.Font, cacheKey string) *context.TextFont {
			if shared, sharedOK := fontFileCache.Load(sf.Filename); sharedOK {
				return shared.(*context.TextFont).Clone()
			}
			font, loadErr := context.NewTextFontFromPath(sf.Filename, size)
			if loadErr != nil {
				common.Log.Debug("could not load font file %s", sf.Filename)
				if len(r.fontByName) >= maxFontCacheEntries {
					clear(r.fontByName)
				}
				r.fontByName[cacheKey] = nil
				return nil
			}
			if stored, loaded := fontFileCache.LoadOrStore(sf.Filename, font); loaded {
				font = stored.(*context.TextFont)
			}
			common.Log.Debug("Substituting font %s with %s (%s)", baseFont, sf.Name, sf.Filename)
			return font.Clone()
		}
		styled := bold || italic
		var fallback *sysfont.Font
		var fallbackName string
		for _, candidate := range candidates {
			if entry, entryOK := r.fontByName[candidate]; entryOK && entry == nil {
				continue
			}
			match := finder.Match(candidate)
			if match == nil {
				if common.Log.IsLogLevel(common.LogLevelDebug) {
					common.Log.Debug("could not find font file %s", candidate)
				}
				continue
			}
			if styled && !fontNameMatchesStyle(match.Name, bold, italic) {
				if fallback == nil {
					fallback, fallbackName = match, candidate
				}
				continue
			}
			if loaded := loadFont(match, candidate); loaded != nil {
				textFont = loaded
				break
			}
		}
		if textFont == nil && fallback != nil {
			textFont = loadFont(fallback, fallbackName)
		}
		if textFont != nil {
			if len(r.fontByName) >= maxFontCacheEntries {
				clear(r.fontByName)
			}
			r.fontByName[origName] = textFont
		}
	}
	if textFont != nil {
		if _, inDictCache := r.textFontByDict[fontDict]; !inDictCache {
			if len(r.textFontByDict) >= maxFontDictCacheEntries {
				clear(r.textFontByDict)
			}
			r.textFontByDict[fontDict] = textFont
		}
	}
	if textFont == nil {
		common.Log.Debug("ERROR: could not find any suitable font")
		return errors.New("could not find any suitable font")
	}
	ts.ProcTf(textFont.WithSize(size, pdfFont))
	return nil
}
