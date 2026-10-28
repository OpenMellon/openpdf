package model

import (
	"errors"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/core"
)

func (r *PdfReader) newPdfAnnotationStampFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationStamp, error) {
	stamp := PdfAnnotationStamp{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	stamp.PdfAnnotationMarkup = markup
	stamp.Name = dict.Get("Name")
	return &stamp, nil
}

func (r *PdfReader) newPdfAnnotationLineFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationLine, error) {
	line := PdfAnnotationLine{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	line.PdfAnnotationMarkup = markup
	line.L = dict.Get("L")
	line.BS = dict.Get("BS")
	line.LE = dict.Get("LE")
	line.IC = dict.Get("IC")
	line.LL = dict.Get("LL")
	line.LLE = dict.Get("LLE")
	line.Cap = dict.Get("Cap")
	line.IT = dict.Get("IT")
	line.LLO = dict.Get("LLO")
	line.CP = dict.Get("CP")
	line.Measure = dict.Get("Measure")
	line.CO = dict.Get("CO")
	return &line, nil
}

func (r *PdfReader) newPdfAnnotationMovieFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationMovie, error) {
	movie := PdfAnnotationMovie{}
	movie.T = dict.Get("T")
	movie.Movie = dict.Get("Movie")
	movie.A = dict.Get("A")
	return &movie, nil
}

func (r *PdfReader) newPdfAnnotationLinkFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationLink, error) {
	link := PdfAnnotationLink{}
	link.A = dict.Get("A")
	link.Dest = dict.Get("Dest")
	link.H = dict.Get("H")
	link.PA = dict.Get("PA")
	link.QuadPoints = dict.Get("QuadPoints")
	link.BS = dict.Get("BS")
	return &link, nil
}

func (r *PdfReader) newPdfAnnotationWidgetFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationWidget, error) {
	widget := PdfAnnotationWidget{}
	widget.H = dict.Get("H")
	widget.MK = dict.Get("MK")
	widget.A = dict.Get("A")
	widget.AA = dict.Get("AA")
	widget.BS = dict.Get("BS")
	widget.Parent = dict.Get("Parent")
	return &widget, nil
}

func (r *PdfReader) newPdfAnnotationWatermarkFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationWatermark, error) {
	watermark := PdfAnnotationWatermark{}
	watermark.FixedPrint = dict.Get("FixedPrint")
	return &watermark, nil
}

func (r *PdfReader) newPdfAnnotationCaretFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationCaret, error) {
	caret := PdfAnnotationCaret{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	caret.PdfAnnotationMarkup = markup
	caret.RD = dict.Get("RD")
	caret.Sy = dict.Get("Sy")
	return &caret, nil
}

func (r *PdfReader) newPdfAnnotationCircleFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationCircle, error) {
	circle := PdfAnnotationCircle{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	circle.PdfAnnotationMarkup = markup
	circle.BS = dict.Get("BS")
	circle.IC = dict.Get("IC")
	circle.BE = dict.Get("BE")
	circle.RD = dict.Get("RD")
	return &circle, nil
}

func (r *PdfReader) newPdfAnnotationStrikeOut(dict *core.PdfObjectDictionary) (*PdfAnnotationStrikeOut, error) {
	strikeOut := PdfAnnotationStrikeOut{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	strikeOut.PdfAnnotationMarkup = markup
	strikeOut.QuadPoints = dict.Get("QuadPoints")
	return &strikeOut, nil
}

func (r *PdfReader) newPdfAnnotationFreeTextFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationFreeText, error) {
	freeText := PdfAnnotationFreeText{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	freeText.PdfAnnotationMarkup = markup
	freeText.DA = dict.Get("DA")
	freeText.Q = dict.Get("Q")
	freeText.RC = dict.Get("RC")
	freeText.DS = dict.Get("DS")
	freeText.CL = dict.Get("CL")
	freeText.IT = dict.Get("IT")
	freeText.BE = dict.Get("BE")
	freeText.RD = dict.Get("RD")
	freeText.BS = dict.Get("BS")
	freeText.LE = dict.Get("LE")
	return &freeText, nil
}

func (r *PdfReader) newPdfAnnotationSquigglyFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationSquiggly, error) {
	squiggly := PdfAnnotationSquiggly{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	squiggly.PdfAnnotationMarkup = markup
	squiggly.QuadPoints = dict.Get("QuadPoints")
	return &squiggly, nil
}

func (r *PdfReader) newPdfAnnotationPrinterMarkFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationPrinterMark, error) {
	printerMark := PdfAnnotationPrinterMark{}
	printerMark.MN = dict.Get("MN")
	return &printerMark, nil
}

func (r *PdfReader) newPdfAnnotationPopupFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationPopup, error) {
	popup := PdfAnnotationPopup{}
	popup.Parent = dict.Get("Parent")
	popup.Open = dict.Get("Open")
	return &popup, nil
}

func (r *PdfReader) newPdfAnnotationFileAttachmentFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationFileAttachment, error) {
	fileAttachment := PdfAnnotationFileAttachment{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	fileAttachment.PdfAnnotationMarkup = markup
	fileAttachment.FS = dict.Get("FS")
	fileAttachment.Name = dict.Get("Name")
	return &fileAttachment, nil
}

func (r *PdfReader) newPdfAnnotationRedactFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationRedact, error) {
	redact := PdfAnnotationRedact{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	redact.PdfAnnotationMarkup = markup
	redact.QuadPoints = dict.Get("QuadPoints")
	redact.IC = dict.Get("IC")
	redact.RO = dict.Get("RO")
	redact.OverlayText = dict.Get("OverlayText")
	redact.Repeat = dict.Get("Repeat")
	redact.DA = dict.Get("DA")
	redact.Q = dict.Get("Q")
	return &redact, nil
}

func (r *PdfReader) newPdfAnnotationScreenFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationScreen, error) {
	screen := PdfAnnotationScreen{}
	screen.T = dict.Get("T")
	screen.MK = dict.Get("MK")
	screen.A = dict.Get("A")
	screen.AA = dict.Get("AA")
	return &screen, nil
}

func (r *PdfReader) newPdfAnnotationUnderlineFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationUnderline, error) {
	underline := PdfAnnotationUnderline{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	underline.PdfAnnotationMarkup = markup
	underline.QuadPoints = dict.Get("QuadPoints")
	return &underline, nil
}

func (r *PdfReader) newPdfAnnotationInkFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationInk, error) {
	ink := PdfAnnotationInk{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	ink.PdfAnnotationMarkup = markup
	ink.InkList = dict.Get("InkList")
	ink.BS = dict.Get("BS")
	return &ink, nil
}

func (r *PdfReader) newPdfAnnotationRichMediaFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationRichMedia, error) {
	richMedia := &PdfAnnotationRichMedia{}
	richMedia.RichMediaSettings = dict.Get("RichMediaSettings")
	richMedia.RichMediaContent = dict.Get("RichMediaContent")
	return richMedia, nil
}

func (r *PdfReader) newPdfAnnotationTextFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationText, error) {
	text := PdfAnnotationText{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	text.PdfAnnotationMarkup = markup
	text.Open = dict.Get("Open")
	text.Name = dict.Get("Name")
	text.State = dict.Get("State")
	text.StateModel = dict.Get("StateModel")
	return &text, nil
}

func (r *PdfReader) newPdfAnnotationMarkupFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationMarkup, error) {
	markup := &PdfAnnotationMarkup{}
	if t := dict.Get("T"); t != nil {
		markup.T = t
	}
	if popup := dict.Get("Popup"); popup != nil {
		indirect, ok := popup.(*core.PdfIndirectObject)
		if !ok {
			if _, ok := popup.(*core.PdfObjectNull); !ok {
				return nil, errors.New("popup should point to an indirect object")
			}
		} else {
			annot, err := r.newPdfAnnotationFromIndirectObject(indirect)
			if err != nil {
				return nil, err
			}
			if annot != nil {
				popup, ok := annot.context.(*PdfAnnotationPopup)
				if !ok {
					return nil, errors.New("object not referring to a popup annotation")
				}
				markup.Popup = popup
			}
		}
	}
	if ca := dict.Get("CA"); ca != nil {
		markup.CA = ca
	}
	if rc := dict.Get("RC"); rc != nil {
		markup.RC = rc
	}
	if creationDate := dict.Get("CreationDate"); creationDate != nil {
		markup.CreationDate = creationDate
	}
	if irt := dict.Get("IRT"); irt != nil {
		markup.IRT = irt
	}
	if subj := dict.Get("Subj"); subj != nil {
		markup.Subj = subj
	}
	if rt := dict.Get("RT"); rt != nil {
		markup.RT = rt
	}
	if it := dict.Get("IT"); it != nil {
		markup.IT = it
	}
	if exData := dict.Get("ExData"); exData != nil {
		markup.ExData = exData
	}
	return markup, nil
}

func (r *PdfReader) newPdfAnnotationProjectionFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationProjection, error) {
	projection := &PdfAnnotationProjection{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	projection.PdfAnnotationMarkup = markup
	return projection, nil
}

func (r *PdfReader) newPdfAnnotationPolygonFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationPolygon, error) {
	polygon := PdfAnnotationPolygon{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	polygon.PdfAnnotationMarkup = markup
	polygon.Vertices = dict.Get("Vertices")
	polygon.LE = dict.Get("LE")
	polygon.BS = dict.Get("BS")
	polygon.IC = dict.Get("IC")
	polygon.BE = dict.Get("BE")
	polygon.IT = dict.Get("IT")
	polygon.Measure = dict.Get("Measure")
	return &polygon, nil
}

func (r *PdfReader) newPdfAnnotationPolyLineFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationPolyLine, error) {
	polyLine := PdfAnnotationPolyLine{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	polyLine.PdfAnnotationMarkup = markup
	polyLine.Vertices = dict.Get("Vertices")
	polyLine.LE = dict.Get("LE")
	polyLine.BS = dict.Get("BS")
	polyLine.IC = dict.Get("IC")
	polyLine.BE = dict.Get("BE")
	polyLine.IT = dict.Get("IT")
	polyLine.Measure = dict.Get("Measure")
	return &polyLine, nil
}

func (r *PdfReader) newPdfAnnotationSoundFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationSound, error) {
	sound := PdfAnnotationSound{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	sound.PdfAnnotationMarkup = markup
	sound.Name = dict.Get("Name")
	sound.Sound = dict.Get("Sound")
	return &sound, nil
}

func (r *PdfReader) newPdfAnnotationSquareFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationSquare, error) {
	square := PdfAnnotationSquare{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	square.PdfAnnotationMarkup = markup
	square.BS = dict.Get("BS")
	square.IC = dict.Get("IC")
	square.BE = dict.Get("BE")
	square.RD = dict.Get("RD")
	return &square, nil
}

func (r *PdfReader) newPdfAnnotationHighlightFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationHighlight, error) {
	highlight := PdfAnnotationHighlight{}
	markup, err := r.newPdfAnnotationMarkupFromDict(dict)
	if err != nil {
		return nil, err
	}
	highlight.PdfAnnotationMarkup = markup
	highlight.QuadPoints = dict.Get("QuadPoints")
	return &highlight, nil
}

func (r *PdfReader) newPdfAnnotationTrapNetFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotationTrapNet, error) {
	trapNet := PdfAnnotationTrapNet{}
	return &trapNet, nil
}
