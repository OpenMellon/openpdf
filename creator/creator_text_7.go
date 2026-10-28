package creator

import (
	"reflect"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/model"
)

func fontAttr(i any) attrApplyFunc {
	value2 := validateSetter("fontAttr", i, 0, pdfFontPtrType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		if target == nil {
			if err := proc.validateFontValue(value); err != nil {
				if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
					return err2
				}
			}
			return nil
		}
		font, err := proc.resolveFont(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
		}
		callSetter(value2, target, reflect.ValueOf(font))
		return nil
	}
}

// TextAlignment options for paragraph.
type TextAlignment int

const (
	TextAlignmentLeft TextAlignment = iota
	TextAlignmentRight
	TextAlignmentCenter
	TextAlignmentJustify
)

// TextOverflow determines the behavior of paragraph text which does
// not fit in the available space.
type TextOverflow int

const (
	TextOverflowVisible TextOverflow = iota
	TextOverflowHidden
)

// UnsupportedRuneError is an error that occurs when there is unsupported glyph being used.
type UnsupportedRuneError struct {
	Message string
	Rune    rune
}

// String implements error interface.
func (ure UnsupportedRuneError) Error() string { return ure.Message }

type fontMetrics struct {
	capHeight float64
	xHeight   float64
	ascent    float64
	descent   float64
}

func newFontMetrics(font *model.PdfFont, value float64) *fontMetrics {
	fontMetrics2 := &fontMetrics{}
	if font == nil {
		common.Log.Debug("WARN: font is nil")
		return fontMetrics2
	}
	fontDescriptor, err := font.GetFontDescriptor()
	if err != nil {
		common.Log.Debug("ERROR: Unable to get font descriptor: %v", err)
		return fontMetrics2
	}
	if fontMetrics2.capHeight, err = fontDescriptor.GetCapHeight(); err != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("WARN: Unable to get font cap height: %v", err)
		}
	}
	if int(fontMetrics2.capHeight) <= 0 {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("WARN: Cap Height not available - setting to 1000")
		}
		fontMetrics2.capHeight = 1000
	}
	fontMetrics2.capHeight *= value / 1000.0
	if fontMetrics2.xHeight, err = fontDescriptor.GetXHeight(); err != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("WARN: Unable to get font x-height: %v", err)
		}
	}
	fontMetrics2.xHeight *= value / 1000.0
	if fontMetrics2.ascent, err = fontDescriptor.GetAscent(); err != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("WARN: Unable to get font ascent: %v", err)
		}
	}
	fontMetrics2.ascent *= value / 1000.0
	if fontMetrics2.descent, err = fontDescriptor.GetDescent(); err != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("WARN: Unable to get font descent: %v", err)
		}
	}
	fontMetrics2.descent *= value / 1000.0
	return fontMetrics2
}

var textContentTags = map[string]struct{}{"paragraph": {}, "text-chunk": {}, "chapter-heading": {}, "list-marker": {}}

// DropCapsScope defines what portion of the paragraph text receives drop caps treatment.
type DropCapsScope int

const (
	DropCapsFirstCharacter DropCapsScope = iota
	DropCapsFirstWord
)
