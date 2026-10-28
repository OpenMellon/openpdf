package model

import (
	"errors"
	"fmt"
	_ "image/gif"
	"io"
	"sync"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// PdfReader represents a PDF file reader. It is a frontend to the lower level parsing mechanism and provides
// a higher level access to work with PDF structure and information, such as the page structure etc.
type PdfReader struct {
	parser         *core.PdfParser
	root           core.PdfObject
	pagesObj       *core.PdfIndirectObject
	pagesDict      *core.PdfObjectDictionary
	pageObjs       []*core.PdfIndirectObject
	PageList       []*PdfPage
	pageCount      int
	catalog        *core.PdfObjectDictionary
	outlineTree    *PdfOutlineTreeNode
	AcroForm       *PdfAcroForm
	DSS            *DSS
	Rotate         *int64
	perms          *Permissions
	revisionCache  map[*PdfReader]*PdfReader
	revisions      []*PdfReader
	modelManager   *modelManager
	lazy           bool
	skipOutlines   bool
	skipPerms      bool
	traversed      map[core.PdfObject]struct{}
	mu             sync.Mutex
	readSeeker     io.ReadSeeker
	complianceMode bool
	fileName       string
	opts           *ReaderOpts
	autoDecrypt    bool
}

// ToWriter creates a new writer from the current reader, based on the specified options.
// If no options are provided, all reader properties are copied to the writer.
func (r *PdfReader) ToWriter(opts *ReaderToWriterOpts) (*PdfWriter, error) {
	writer := NewPdfWriter()
	writer.SetFileName(r.fileName)
	if opts == nil {
		opts = &ReaderToWriterOpts{}
	}
	writer.SetSkipObjectCopy(opts.SkipObjectCopy)
	numPages, err := r.GetNumPages()
	if err != nil {
		common.Log.Debug("ERROR: %v", err)
		return nil, err
	}
	for i := 1; i <= numPages; i++ {
		page, err := r.GetPage(i)
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
			return nil, err
		}
		if opts.PageProcessCallback != nil {
			err = opts.PageProcessCallback(i, page)
			if err != nil {
				common.Log.Debug("ERROR: %v", err)
				return nil, err
			}
		}
		err = writer.AddPage(page)
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
			return nil, err
		}
	}
	writer.version = r.PdfVersion()
	if !opts.SkipInfo {
		pdfInfo, err := r.GetPdfInfo()
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
		} else {
			writer.infoObj.PdfObject = pdfInfo.ToPdfObject()
		}
	}
	if !opts.SkipMetadata {
		if metadata := r.catalog.Get("Metadata"); metadata != nil {
			if err := writer.SetCatalogMetadata(metadata); err != nil {
				return nil, err
			}
		}
	}
	if !opts.SkipMarkInfo {
		if catalogMarkInfo, ok := r.GetCatalogMarkInfo(); ok {
			if err := writer.SetCatalogMarkInfo(catalogMarkInfo); err != nil {
				return nil, err
			}
		}
	}
	if !opts.SkipAcroForm {
		err := writer.SetForms(r.AcroForm)
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
			return nil, err
		}
	}
	if !opts.SkipViewerPreferences {
		if catalogViewerPreferences, ok := r.GetCatalogViewerPreferences(); ok {
			if err := writer.SetCatalogViewerPreferences(catalogViewerPreferences); err != nil {
				return nil, err
			}
		}
	}
	if !opts.SkipLanguage {
		if catalogLanguage, ok := r.GetCatalogLanguage(); ok {
			if err := writer.SetCatalogLanguage(catalogLanguage); err != nil {
				return nil, err
			}
		}
	}
	if !opts.SkipPieceInfo {
		if pieceInfo, ok := r.GetPieceInfo(); ok {
			if err := writer.SetPieceInfo(pieceInfo); err != nil {
				return nil, err
			}
		}
	}
	if !opts.SkipOutlines {
		writer.AddOutlineTree(r.GetOutlineTree())
	}
	if !opts.SkipOCProperties {
		ocProperties, err := r.GetOCProperties()
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
		} else {
			err = writer.SetOCProperties(ocProperties)
			if err != nil {
				common.Log.Debug("ERROR: %v", err)
			}
		}
	}
	if !opts.SkipPageLabels {
		pageLabels, err := r.GetPageLabels()
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
		} else {
			err = writer.SetPageLabels(pageLabels)
			if err != nil {
				common.Log.Debug("ERROR: %v", err)
			}
		}
	}
	if !opts.SkipNamedDests {
		namedDestinations, err := r.GetNamedDestinations()
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
		} else {
			err = writer.SetNamedDestinations(namedDestinations)
			if err != nil {
				common.Log.Debug("ERROR: %v", err)
			}
		}
	}
	if !opts.SkipNameDictionary {
		nameDictionary, err := r.GetNameDictionary()
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
		} else {
			err = writer.SetNameDictionary(nameDictionary)
			if err != nil {
				common.Log.Debug("ERROR: %v", err)
			}
		}
	}
	catalogStructTreeRoot, ok := r.GetCatalogStructTreeRoot()
	if ok {
		err := writer.SetCatalogStructTreeRoot(catalogStructTreeRoot)
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
		}
	}
	if !opts.SkipRotation && r.Rotate != nil {
		if err := writer.SetRotation(*r.Rotate); err != nil {
			common.Log.Debug("ERROR: %v", err)
		}
	}
	return &writer, nil
}

func (r *PdfReader) newPdfActionThreadFromDict(dict *core.PdfObjectDictionary) (*PdfActionThread, error) {
	filespec, err := newPdfFilespecFromPdfObject(dict.Get("F"))
	if err != nil {
		return nil, err
	}
	return &PdfActionThread{D: dict.Get("D"), B: dict.Get("B"), F: filespec}, nil
}

func (r *PdfReader) loadAction(a core.PdfObject) (*PdfAction, error) {
	if aIndirect, ok := core.GetIndirect(a); ok {
		action, err := r.newPdfActionFromIndirectObject(aIndirect)
		if err != nil {
			return nil, err
		}
		return action, nil
	} else if !core.IsNullObject(a) {
		return nil, errors.New("action should point to an indirect object")
	}
	return nil, nil
}

func (r *PdfReader) newPdfActionGoTo3DViewFromDict(dict *core.PdfObjectDictionary) (*PdfActionGoTo3DView, error) {
	return &PdfActionGoTo3DView{TA: dict.Get("TA"), V: dict.Get("V")}, nil
}

func (r *PdfReader) newPdfActionMovieFromDict(dict *core.PdfObjectDictionary) (*PdfActionMovie, error) {
	return &PdfActionMovie{Annotation: dict.Get("Annotation"), T: dict.Get("T"), Operation: dict.Get("Operation")}, nil
}

func (r *PdfReader) newPdfActionSubmitFormFromDict(dict *core.PdfObjectDictionary) (*PdfActionSubmitForm, error) {
	filespec, err := newPdfFilespecFromPdfObject(dict.Get("F"))
	if err != nil {
		return nil, err
	}
	return &PdfActionSubmitForm{F: filespec, Fields: dict.Get("Fields"), Flags: dict.Get("Flags")}, nil
}

func (r *PdfReader) newPdfActionResetFormFromDict(dict *core.PdfObjectDictionary) (*PdfActionResetForm, error) {
	return &PdfActionResetForm{Fields: dict.Get("Fields"), Flags: dict.Get("Flags")}, nil
}

func (r *PdfReader) newPdfActionSoundFromDict(dict *core.PdfObjectDictionary) (*PdfActionSound, error) {
	return &PdfActionSound{Sound: dict.Get("Sound"), Volume: dict.Get("Volume"), Synchronous: dict.Get("Synchronous"), Repeat: dict.Get("Repeat"), Mix: dict.Get("Mix")}, nil
}

func (r *PdfReader) newPdfActionHideFromDict(dict *core.PdfObjectDictionary) (*PdfActionHide, error) {
	return &PdfActionHide{T: dict.Get("T"), H: dict.Get("H")}, nil
}

func (r *PdfReader) newPdfActionGotoFromDict(dict *core.PdfObjectDictionary) (*PdfActionGoTo, error) {
	return &PdfActionGoTo{D: dict.Get("D")}, nil
}

func (r *PdfReader) newPdfActionGotoEFromDict(dict *core.PdfObjectDictionary) (*PdfActionGoToE, error) {
	filespec, err := newPdfFilespecFromPdfObject(dict.Get("F"))
	if err != nil {
		return nil, err
	}
	return &PdfActionGoToE{D: dict.Get("D"), NewWindow: dict.Get("NewWindow"), T: dict.Get("T"), F: filespec}, nil
}

func (r *PdfReader) newPdfActionJavaScriptFromDict(dict *core.PdfObjectDictionary) (*PdfActionJavaScript, error) {
	return &PdfActionJavaScript{JS: dict.Get("JS")}, nil
}

func (r *PdfReader) newPdfActionImportDataFromDict(dict *core.PdfObjectDictionary) (*PdfActionImportData, error) {
	filespec, err := newPdfFilespecFromPdfObject(dict.Get("F"))
	if err != nil {
		return nil, err
	}
	return &PdfActionImportData{F: filespec}, nil
}

func (r *PdfReader) newPdfActionSetOCGStateFromDict(dict *core.PdfObjectDictionary) (*PdfActionSetOCGState, error) {
	return &PdfActionSetOCGState{State: dict.Get("State"), PreserveRB: dict.Get("PreserveRB")}, nil
}

func (r *PdfReader) newPdfActionNamedFromDict(dict *core.PdfObjectDictionary) (*PdfActionNamed, error) {
	return &PdfActionNamed{N: dict.Get("N")}, nil
}

func (r *PdfReader) newPdfActionRenditionFromDict(dict *core.PdfObjectDictionary) (*PdfActionRendition, error) {
	return &PdfActionRendition{R: dict.Get("R"), AN: dict.Get("AN"), OP: dict.Get("OP"), JS: dict.Get("JS")}, nil
}

func (r *PdfReader) newPdfActionURIFromDict(dict *core.PdfObjectDictionary) (*PdfActionURI, error) {
	return &PdfActionURI{URI: dict.Get("URI"), IsMap: dict.Get("IsMap")}, nil
}

func (r *PdfReader) newPdfActionGotoRFromDict(dict *core.PdfObjectDictionary) (*PdfActionGoToR, error) {
	filespec, err := newPdfFilespecFromPdfObject(dict.Get("F"))
	if err != nil {
		return nil, err
	}
	return &PdfActionGoToR{D: dict.Get("D"), NewWindow: dict.Get("NewWindow"), F: filespec}, nil
}

func (r *PdfReader) newPdfActionTransFromDict(dict *core.PdfObjectDictionary) (*PdfActionTrans, error) {
	return &PdfActionTrans{Trans: dict.Get("Trans")}, nil
}

func (r *PdfReader) newPdfActionLaunchFromDict(dict *core.PdfObjectDictionary) (*PdfActionLaunch, error) {
	filespec, err := newPdfFilespecFromPdfObject(dict.Get("F"))
	if err != nil {
		return nil, err
	}
	return &PdfActionLaunch{Win: dict.Get("Win"), Mac: dict.Get("Mac"), Unix: dict.Get("Unix"), NewWindow: dict.Get("NewWindow"), F: filespec}, nil
}

func (r *PdfReader) newPdfActionFromIndirectObject(indirect *core.PdfIndirectObject) (*PdfAction, error) {
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return nil, fmt.Errorf("action indirect object not containing a dictionary")
	}
	if model := r.modelManager.GetModelFromPrimitive(dict); model != nil {
		action, ok := model.(*PdfAction)
		if !ok {
			return nil, fmt.Errorf("cached model not a PDF action")
		}
		return action, nil
	}
	action := &PdfAction{}
	action.primitive = indirect
	r.modelManager.Register(dict, action)
	if obj := dict.Get("Type"); obj != nil {
		name, ok := obj.(*core.PdfObjectName)
		if !ok {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Incompatibility! Invalid type of Type (%T) - should be Name", obj)
			}
		} else {
			if *name != "Action" {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Unsuspected Type != Action (%s)", *name)
				}
			}
			action.Type = name
		}
	}
	if next := dict.Get("Next"); next != nil {
		action.Next = next
	}
	if s := dict.Get("S"); s != nil {
		action.S = s
	}
	name, found := action.S.(*core.PdfObjectName)
	if !found {
		common.Log.Debug("ERROR: Invalid S object type != name (%T)", action.S)
		return nil, fmt.Errorf("invalid S object type != name (%T)", action.S)
	}
	typeAction := PdfActionType(name.String())
	switch typeAction {
	case ActionTypeGoTo:
		goToAction, err := r.newPdfActionGotoFromDict(dict)
		if err != nil {
			return nil, err
		}
		goToAction.PdfAction = action
		action.context = goToAction
		return action, nil
	case ActionTypeGoToR:
		goToRAction, err := r.newPdfActionGotoRFromDict(dict)
		if err != nil {
			return nil, err
		}
		goToRAction.PdfAction = action
		action.context = goToRAction
		return action, nil
	case ActionTypeGoToE:
		goToEAction, err := r.newPdfActionGotoEFromDict(dict)
		if err != nil {
			return nil, err
		}
		goToEAction.PdfAction = action
		action.context = goToEAction
		return action, nil
	case ActionTypeLaunch:
		launchAction, err := r.newPdfActionLaunchFromDict(dict)
		if err != nil {
			return nil, err
		}
		launchAction.PdfAction = action
		action.context = launchAction
		return action, nil
	case ActionTypeThread:
		threadAction, err := r.newPdfActionThreadFromDict(dict)
		if err != nil {
			return nil, err
		}
		threadAction.PdfAction = action
		action.context = threadAction
		return action, nil
	case ActionTypeURI:
		uriAction, err := r.newPdfActionURIFromDict(dict)
		if err != nil {
			return nil, err
		}
		uriAction.PdfAction = action
		action.context = uriAction
		return action, nil
	case ActionTypeSound:
		soundAction, err := r.newPdfActionSoundFromDict(dict)
		if err != nil {
			return nil, err
		}
		soundAction.PdfAction = action
		action.context = soundAction
		return action, nil
	case ActionTypeMovie:
		movieAction, err := r.newPdfActionMovieFromDict(dict)
		if err != nil {
			return nil, err
		}
		movieAction.PdfAction = action
		action.context = movieAction
		return action, nil
	case ActionTypeHide:
		hideAction, err := r.newPdfActionHideFromDict(dict)
		if err != nil {
			return nil, err
		}
		hideAction.PdfAction = action
		action.context = hideAction
		return action, nil
	case ActionTypeNamed:
		namedAction, err := r.newPdfActionNamedFromDict(dict)
		if err != nil {
			return nil, err
		}
		namedAction.PdfAction = action
		action.context = namedAction
		return action, nil
	case ActionTypeSubmitForm:
		submitFormAction, err := r.newPdfActionSubmitFormFromDict(dict)
		if err != nil {
			return nil, err
		}
		submitFormAction.PdfAction = action
		action.context = submitFormAction
		return action, nil
	case ActionTypeResetForm:
		resetFormAction, err := r.newPdfActionResetFormFromDict(dict)
		if err != nil {
			return nil, err
		}
		resetFormAction.PdfAction = action
		action.context = resetFormAction
		return action, nil
	case ActionTypeImportData:
		importDataAction, err := r.newPdfActionImportDataFromDict(dict)
		if err != nil {
			return nil, err
		}
		importDataAction.PdfAction = action
		action.context = importDataAction
		return action, nil
	case ActionTypeSetOCGState:
		setOCGStateAction, err := r.newPdfActionSetOCGStateFromDict(dict)
		if err != nil {
			return nil, err
		}
		setOCGStateAction.PdfAction = action
		action.context = setOCGStateAction
		return action, nil
	case ActionTypeRendition:
		renditionAction, err := r.newPdfActionRenditionFromDict(dict)
		if err != nil {
			return nil, err
		}
		renditionAction.PdfAction = action
		action.context = renditionAction
		return action, nil
	case ActionTypeTrans:
		transAction, err := r.newPdfActionTransFromDict(dict)
		if err != nil {
			return nil, err
		}
		transAction.PdfAction = action
		action.context = transAction
		return action, nil
	case ActionTypeGoTo3DView:
		goTo3DViewAction, err := r.newPdfActionGoTo3DViewFromDict(dict)
		if err != nil {
			return nil, err
		}
		goTo3DViewAction.PdfAction = action
		action.context = goTo3DViewAction
		return action, nil
	case ActionTypeJavaScript:
		javaScriptAction, err := r.newPdfActionJavaScriptFromDict(dict)
		if err != nil {
			return nil, err
		}
		javaScriptAction.PdfAction = action
		action.context = javaScriptAction
		return action, nil
	}
	common.Log.Debug("ERROR: Ignoring unknown action: %s", typeAction)
	return nil, nil
}
