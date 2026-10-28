package model

import (
	"errors"
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

func (r *PdfReader) newPdfAnnotationFromIndirectObject(indirect *core.PdfIndirectObject) (*PdfAnnotation, error) {
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return nil, fmt.Errorf("annotation indirect object not containing a dictionary")
	}
	if model := r.modelManager.GetModelFromPrimitive(dict); model != nil {
		annot, ok := model.(*PdfAnnotation)
		if !ok {
			return nil, fmt.Errorf("cached model not a PDF annotation")
		}
		return annot, nil
	}
	annot := &PdfAnnotation{}
	annot.primitive = indirect
	r.modelManager.Register(dict, annot)
	if obj := dict.Get("Type"); obj != nil {
		name, ok := obj.(*core.PdfObjectName)
		if !ok {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Incompatibility! Invalid type of Type (%T) - should be Name", obj)
			}
		} else {
			if *name != "Annot" {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Unsuspected Type != Annot (%s)", *name)
				}
			}
		}
	}
	if rect := dict.Get("Rect"); rect != nil {
		annot.Rect = rect
	}
	if contents := dict.Get("Contents"); contents != nil {
		annot.Contents = contents
	}
	if p := dict.Get("P"); p != nil {
		annot.P = p
	}
	if nm := dict.Get("NM"); nm != nil {
		annot.NM = nm
	}
	if m := dict.Get("M"); m != nil {
		annot.M = m
	}
	if f := dict.Get("F"); f != nil {
		annot.F = f
	}
	if ap := dict.Get("AP"); ap != nil {
		annot.AP = ap
	}
	if as := dict.Get("AS"); as != nil {
		annot.AS = as
	}
	if border := dict.Get("Border"); border != nil {
		annot.Border = border
	}
	if c := dict.Get("C"); c != nil {
		annot.C = c
	}
	if structParent := dict.Get("StructParent"); structParent != nil {
		annot.StructParent = structParent
	}
	if oc := dict.Get("OC"); oc != nil {
		annot.OC = oc
	}
	subtype := dict.Get("Subtype")
	if subtype == nil {
		common.Log.Debug("WARNING: Compatibility issue - annotation Subtype missing - assuming no subtype")
		annot.context = nil
		return annot, nil
	}
	name, found := subtype.(*core.PdfObjectName)
	if !found {
		common.Log.Debug("ERROR: Invalid Subtype object type != name (%T)", subtype)
		return nil, fmt.Errorf("invalid Subtype object type != name (%T)", subtype)
	}
	switch *name {
	case "Text":
		text, err := r.newPdfAnnotationTextFromDict(dict)
		if err != nil {
			return nil, err
		}
		text.PdfAnnotation = annot
		annot.context = text
		return annot, nil
	case "Link":
		link, err := r.newPdfAnnotationLinkFromDict(dict)
		if err != nil {
			return nil, err
		}
		link.PdfAnnotation = annot
		annot.context = link
		return annot, nil
	case "FreeText":
		freeText, err := r.newPdfAnnotationFreeTextFromDict(dict)
		if err != nil {
			return nil, err
		}
		freeText.PdfAnnotation = annot
		annot.context = freeText
		return annot, nil
	case "Line":
		line, err := r.newPdfAnnotationLineFromDict(dict)
		if err != nil {
			return nil, err
		}
		line.PdfAnnotation = annot
		annot.context = line
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("LINE ANNOTATION: annot (%T): %+v\n", annot, annot)
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("LINE ANNOTATION: ctx (%T): %+v\n", line, line)
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("LINE ANNOTATION Markup: ctx (%T): %+v\n", line.PdfAnnotationMarkup, line.PdfAnnotationMarkup)
		}
		return annot, nil
	case "Square":
		square, err := r.newPdfAnnotationSquareFromDict(dict)
		if err != nil {
			return nil, err
		}
		square.PdfAnnotation = annot
		annot.context = square
		return annot, nil
	case "Circle":
		circle, err := r.newPdfAnnotationCircleFromDict(dict)
		if err != nil {
			return nil, err
		}
		circle.PdfAnnotation = annot
		annot.context = circle
		return annot, nil
	case "Polygon":
		polygon, err := r.newPdfAnnotationPolygonFromDict(dict)
		if err != nil {
			return nil, err
		}
		polygon.PdfAnnotation = annot
		annot.context = polygon
		return annot, nil
	case "PolyLine":
		polyLine, err := r.newPdfAnnotationPolyLineFromDict(dict)
		if err != nil {
			return nil, err
		}
		polyLine.PdfAnnotation = annot
		annot.context = polyLine
		return annot, nil
	case "Highlight":
		highlight, err := r.newPdfAnnotationHighlightFromDict(dict)
		if err != nil {
			return nil, err
		}
		highlight.PdfAnnotation = annot
		annot.context = highlight
		return annot, nil
	case "Underline":
		underline, err := r.newPdfAnnotationUnderlineFromDict(dict)
		if err != nil {
			return nil, err
		}
		underline.PdfAnnotation = annot
		annot.context = underline
		return annot, nil
	case "Squiggly":
		squiggly, err := r.newPdfAnnotationSquigglyFromDict(dict)
		if err != nil {
			return nil, err
		}
		squiggly.PdfAnnotation = annot
		annot.context = squiggly
		return annot, nil
	case "StrikeOut":
		strikeOut, err := r.newPdfAnnotationStrikeOut(dict)
		if err != nil {
			return nil, err
		}
		strikeOut.PdfAnnotation = annot
		annot.context = strikeOut
		return annot, nil
	case "Caret":
		caret, err := r.newPdfAnnotationCaretFromDict(dict)
		if err != nil {
			return nil, err
		}
		caret.PdfAnnotation = annot
		annot.context = caret
		return annot, nil
	case "Stamp":
		stamp, err := r.newPdfAnnotationStampFromDict(dict)
		if err != nil {
			return nil, err
		}
		stamp.PdfAnnotation = annot
		annot.context = stamp
		return annot, nil
	case "Ink":
		ink, err := r.newPdfAnnotationInkFromDict(dict)
		if err != nil {
			return nil, err
		}
		ink.PdfAnnotation = annot
		annot.context = ink
		return annot, nil
	case "Popup":
		popup, err := r.newPdfAnnotationPopupFromDict(dict)
		if err != nil {
			return nil, err
		}
		popup.PdfAnnotation = annot
		annot.context = popup
		return annot, nil
	case "FileAttachment":
		fileAttachment, err := r.newPdfAnnotationFileAttachmentFromDict(dict)
		if err != nil {
			return nil, err
		}
		fileAttachment.PdfAnnotation = annot
		annot.context = fileAttachment
		return annot, nil
	case "Sound":
		sound, err := r.newPdfAnnotationSoundFromDict(dict)
		if err != nil {
			return nil, err
		}
		sound.PdfAnnotation = annot
		annot.context = sound
		return annot, nil
	case "RichMedia":
		richMedia, err := r.newPdfAnnotationRichMediaFromDict(dict)
		if err != nil {
			return nil, err
		}
		richMedia.PdfAnnotation = annot
		annot.context = richMedia
		return annot, nil
	case "Movie":
		movie, err := r.newPdfAnnotationMovieFromDict(dict)
		if err != nil {
			return nil, err
		}
		movie.PdfAnnotation = annot
		annot.context = movie
		return annot, nil
	case "Screen":
		screen, err := r.newPdfAnnotationScreenFromDict(dict)
		if err != nil {
			return nil, err
		}
		screen.PdfAnnotation = annot
		annot.context = screen
		return annot, nil
	case "Widget":
		widget, err := r.newPdfAnnotationWidgetFromDict(dict)
		if err != nil {
			return nil, err
		}
		widget.PdfAnnotation = annot
		annot.context = widget
		return annot, nil
	case "PrinterMark":
		printerMark, err := r.newPdfAnnotationPrinterMarkFromDict(dict)
		if err != nil {
			return nil, err
		}
		printerMark.PdfAnnotation = annot
		annot.context = printerMark
		return annot, nil
	case "TrapNet":
		trapNet, err := r.newPdfAnnotationTrapNetFromDict(dict)
		if err != nil {
			return nil, err
		}
		trapNet.PdfAnnotation = annot
		annot.context = trapNet
		return annot, nil
	case "Watermark":
		watermark, err := r.newPdfAnnotationWatermarkFromDict(dict)
		if err != nil {
			return nil, err
		}
		watermark.PdfAnnotation = annot
		annot.context = watermark
		return annot, nil
	case "3D":
		annot3D, err := r.newPdfAnnotation3DFromDict(dict)
		if err != nil {
			return nil, err
		}
		annot3D.PdfAnnotation = annot
		annot.context = annot3D
		return annot, nil
	case "Projection":
		projection, err := r.newPdfAnnotationProjectionFromDict(dict)
		if err != nil {
			return nil, err
		}
		projection.PdfAnnotation = annot
		annot.context = projection
		return annot, nil
	case "Redact":
		redact, err := r.newPdfAnnotationRedactFromDict(dict)
		if err != nil {
			return nil, err
		}
		redact.PdfAnnotation = annot
		annot.context = redact
		return annot, nil
	}
	common.Log.Debug("ERROR: Ignoring unknown annotation: %s", *name)
	return nil, nil
}

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
