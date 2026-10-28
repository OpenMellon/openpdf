package model

import (
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
