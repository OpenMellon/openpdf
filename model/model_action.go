package model

import (
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/core"
)

// PdfActionImportData represents a importData action.
type PdfActionImportData struct {
	*PdfAction
	F *PdfFilespec
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionImportData) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeImportData)))
	if a.F != nil {
		dict.Set("F", a.F.ToPdfObject())
	}
	return indirect
}

// NewPdfActionImportData returns a new "import data" action.
func NewPdfActionImportData() *PdfActionImportData {
	action := NewPdfAction()
	importDataAction := &PdfActionImportData{}
	importDataAction.PdfAction = action
	action.SetContext(importDataAction)
	return importDataAction
}

// PdfActionHide represents a hide action.
type PdfActionHide struct {
	*PdfAction
	T core.PdfObject
	H core.PdfObject
}

// NewPdfActionHide returns a new "hide" action.
func NewPdfActionHide() *PdfActionHide {
	action := NewPdfAction()
	hideAction := &PdfActionHide{}
	hideAction.PdfAction = action
	action.SetContext(hideAction)
	return hideAction
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionHide) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeHide)))
	dict.SetIfNotNil("T", a.T)
	dict.SetIfNotNil("H", a.H)
	return indirect
}

// PdfActionSetOCGState represents a SetOCGState action.
type PdfActionSetOCGState struct {
	*PdfAction
	State      core.PdfObject
	PreserveRB core.PdfObject
}

// NewPdfActionSetOCGState returns a new "named" action.
func NewPdfActionSetOCGState() *PdfActionSetOCGState {
	action := NewPdfAction()
	setOCGStateAction := &PdfActionSetOCGState{}
	setOCGStateAction.PdfAction = action
	action.SetContext(setOCGStateAction)
	return setOCGStateAction
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionSetOCGState) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeSetOCGState)))
	dict.SetIfNotNil("State", a.State)
	dict.SetIfNotNil("PreserveRB", a.PreserveRB)
	return indirect
}

// PdfAction represents an action in PDF (section 12.6 p. 412).
type PdfAction struct {
	context   PdfModel
	Type      core.PdfObject
	S         core.PdfObject
	Next      core.PdfObject
	primitive *core.PdfIndirectObject
}

// NewPdfAction returns an initialized generic PDF action model.
func NewPdfAction() *PdfAction {
	action := &PdfAction{}
	action.primitive = core.MakeIndirectObject(core.MakeDict())
	return action
}

// GetContainingPdfObject implements interface PdfModel.
func (a *PdfAction) GetContainingPdfObject() core.PdfObject { return a.primitive }

// ToPdfObject implements interface PdfModel.
func (a *PdfAction) ToPdfObject() core.PdfObject {
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.Clear()
	dict.Set("Type", core.MakeName("Action"))
	dict.SetIfNotNil("S", a.S)
	dict.SetIfNotNil("Next", a.Next)
	return indirect
}

// GetContext returns the action context which contains the specific type-dependent context.
// The context represents the subaction.
func (a *PdfAction) GetContext() PdfModel {
	if a == nil {
		return nil
	}
	return a.context
}

// String implements interface PdfObject.
func (a *PdfAction) String() string {
	indirect, ok := a.ToPdfObject().(*core.PdfIndirectObject)
	if ok {
		return fmt.Sprintf("%T: %s", a.context, indirect.PdfObject.String())
	}
	return ""
}

// SetContext sets the sub action (context).
func (a *PdfAction) SetContext(ctx PdfModel) { a.context = ctx }

// PdfActionMovie represents a movie action.
type PdfActionMovie struct {
	*PdfAction
	Annotation core.PdfObject
	T          core.PdfObject
	Operation  core.PdfObject
}

// NewPdfActionMovie returns a new "movie" action.
func NewPdfActionMovie() *PdfActionMovie {
	action := NewPdfAction()
	movieAction := &PdfActionMovie{}
	movieAction.PdfAction = action
	action.SetContext(movieAction)
	return movieAction
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionMovie) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeMovie)))
	dict.SetIfNotNil("Annotation", a.Annotation)
	dict.SetIfNotNil("T", a.T)
	dict.SetIfNotNil("Operation", a.Operation)
	return indirect
}

// PdfActionGoToR represents a GoToR action.
type PdfActionGoToR struct {
	*PdfAction
	F         *PdfFilespec
	D         core.PdfObject
	NewWindow core.PdfObject
}

// NewPdfActionGoToR returns a new "go to remote" action.
func NewPdfActionGoToR() *PdfActionGoToR {
	action := NewPdfAction()
	goToRAction := &PdfActionGoToR{}
	goToRAction.PdfAction = action
	action.SetContext(goToRAction)
	return goToRAction
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionGoToR) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeGoToR)))
	if a.F != nil {
		dict.Set("F", a.F.ToPdfObject())
	}
	dict.SetIfNotNil("D", a.D)
	dict.SetIfNotNil("NewWindow", a.NewWindow)
	return indirect
}

// PdfActionRendition represents a Rendition action.
type PdfActionRendition struct {
	*PdfAction
	R  core.PdfObject
	AN core.PdfObject
	OP core.PdfObject
	JS core.PdfObject
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionRendition) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeRendition)))
	dict.SetIfNotNil("R", a.R)
	dict.SetIfNotNil("AN", a.AN)
	dict.SetIfNotNil("OP", a.OP)
	dict.SetIfNotNil("JS", a.JS)
	return indirect
}

// NewPdfActionRendition returns a new "rendition" action.
func NewPdfActionRendition() *PdfActionRendition {
	action := NewPdfAction()
	renditionAction := &PdfActionRendition{}
	renditionAction.PdfAction = action
	action.SetContext(renditionAction)
	return renditionAction
}

// PdfActionJavaScript represents a javaScript action.
type PdfActionJavaScript struct {
	*PdfAction
	JS core.PdfObject
}

// NewPdfActionJavaScript returns a new "javaScript" action.
func NewPdfActionJavaScript() *PdfActionJavaScript {
	action := NewPdfAction()
	javaScriptAction := &PdfActionJavaScript{}
	javaScriptAction.PdfAction = action
	action.SetContext(javaScriptAction)
	return javaScriptAction
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionJavaScript) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeJavaScript)))
	dict.SetIfNotNil("JS", a.JS)
	return indirect
}

// PdfActionSound represents a sound action.
type PdfActionSound struct {
	*PdfAction
	Sound       core.PdfObject
	Volume      core.PdfObject
	Synchronous core.PdfObject
	Repeat      core.PdfObject
	Mix         core.PdfObject
}

// NewPdfActionSound returns a new "sound" action.
func NewPdfActionSound() *PdfActionSound {
	action := NewPdfAction()
	soundAction := &PdfActionSound{}
	soundAction.PdfAction = action
	action.SetContext(soundAction)
	return soundAction
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionSound) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeSound)))
	dict.SetIfNotNil("Sound", a.Sound)
	dict.SetIfNotNil("Volume", a.Volume)
	dict.SetIfNotNil("Synchronous", a.Synchronous)
	dict.SetIfNotNil("Repeat", a.Repeat)
	dict.SetIfNotNil("Mix", a.Mix)
	return indirect
}

// PdfActionGoToE represents a GoToE action.
type PdfActionGoToE struct {
	*PdfAction
	F         *PdfFilespec
	D         core.PdfObject
	NewWindow core.PdfObject
	T         core.PdfObject
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionGoToE) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeGoToE)))
	if a.F != nil {
		dict.Set("F", a.F.ToPdfObject())
	}
	dict.SetIfNotNil("D", a.D)
	dict.SetIfNotNil("NewWindow", a.NewWindow)
	dict.SetIfNotNil("T", a.T)
	return indirect
}

// NewPdfActionGoToE returns a new "go to embedded" action.
func NewPdfActionGoToE() *PdfActionGoToE {
	action := NewPdfAction()
	goToEAction := &PdfActionGoToE{}
	goToEAction.PdfAction = action
	action.SetContext(goToEAction)
	return goToEAction
}

// PdfActionGoTo represents a GoTo action.
type PdfActionGoTo struct {
	*PdfAction
	D core.PdfObject
}

// NewPdfActionGoTo returns a new "go to" action.
func NewPdfActionGoTo() *PdfActionGoTo {
	action := NewPdfAction()
	goToAction := &PdfActionGoTo{}
	goToAction.PdfAction = action
	action.SetContext(goToAction)
	return goToAction
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionGoTo) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeGoTo)))
	dict.SetIfNotNil("D", a.D)
	return indirect
}

// PdfActionNamed represents a named action.
type PdfActionNamed struct {
	*PdfAction
	N core.PdfObject
}

// NewPdfActionNamed returns a new "named" action.
func NewPdfActionNamed() *PdfActionNamed {
	action := NewPdfAction()
	namedAction := &PdfActionNamed{}
	namedAction.PdfAction = action
	action.SetContext(namedAction)
	return namedAction
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionNamed) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeNamed)))
	dict.SetIfNotNil("N", a.N)
	return indirect
}

// PdfActionSubmitForm represents a submitForm action.
type PdfActionSubmitForm struct {
	*PdfAction
	F      *PdfFilespec
	Fields core.PdfObject
	Flags  core.PdfObject
}

// NewPdfActionSubmitForm returns a new "submit form" action.
func NewPdfActionSubmitForm() *PdfActionSubmitForm {
	action := NewPdfAction()
	submitFormAction := &PdfActionSubmitForm{}
	submitFormAction.PdfAction = action
	action.SetContext(submitFormAction)
	return submitFormAction
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionSubmitForm) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeSubmitForm)))
	if a.F != nil {
		dict.Set("F", a.F.ToPdfObject())
	}
	dict.SetIfNotNil("Fields", a.Fields)
	dict.SetIfNotNil("Flags", a.Flags)
	return indirect
}

// PdfActionResetForm represents a resetForm action.
type PdfActionResetForm struct {
	*PdfAction
	Fields core.PdfObject
	Flags  core.PdfObject
}

// NewPdfActionResetForm returns a new "reset form" action.
func NewPdfActionResetForm() *PdfActionResetForm {
	action := NewPdfAction()
	resetFormAction := &PdfActionResetForm{}
	resetFormAction.PdfAction = action
	action.SetContext(resetFormAction)
	return resetFormAction
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionResetForm) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeResetForm)))
	dict.SetIfNotNil("Fields", a.Fields)
	dict.SetIfNotNil("Flags", a.Flags)
	return indirect
}

// PdfActionLaunch represents a launch action.
type PdfActionLaunch struct {
	*PdfAction
	F         *PdfFilespec
	Win       core.PdfObject
	Mac       core.PdfObject
	Unix      core.PdfObject
	NewWindow core.PdfObject
}

// NewPdfActionLaunch returns a new "launch" action.
func NewPdfActionLaunch() *PdfActionLaunch {
	action := NewPdfAction()
	launchAction := &PdfActionLaunch{}
	launchAction.PdfAction = action
	action.SetContext(launchAction)
	return launchAction
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionLaunch) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeLaunch)))
	if a.F != nil {
		dict.Set("F", a.F.ToPdfObject())
	}
	dict.SetIfNotNil("Win", a.Win)
	dict.SetIfNotNil("Mac", a.Mac)
	dict.SetIfNotNil("Unix", a.Unix)
	dict.SetIfNotNil("NewWindow", a.NewWindow)
	return indirect
}

// PdfActionGoTo3DView represents a GoTo3DView action.
type PdfActionGoTo3DView struct {
	*PdfAction
	TA core.PdfObject
	V  core.PdfObject
}

// NewPdfActionGoTo3DView returns a new "goTo3DView" action.
func NewPdfActionGoTo3DView() *PdfActionGoTo3DView {
	action := NewPdfAction()
	goTo3DViewAction := &PdfActionGoTo3DView{}
	goTo3DViewAction.PdfAction = action
	action.SetContext(goTo3DViewAction)
	return goTo3DViewAction
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionGoTo3DView) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeGoTo3DView)))
	dict.SetIfNotNil("TA", a.TA)
	dict.SetIfNotNil("V", a.V)
	return indirect
}

// PdfActionURI represents an URI action.
type PdfActionURI struct {
	*PdfAction
	URI   core.PdfObject
	IsMap core.PdfObject
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionURI) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeURI)))
	dict.SetIfNotNil("URI", a.URI)
	dict.SetIfNotNil("IsMap", a.IsMap)
	return indirect
}

// NewPdfActionURI returns a new "Uri" action.
func NewPdfActionURI() *PdfActionURI {
	action := NewPdfAction()
	uriAction := &PdfActionURI{}
	uriAction.PdfAction = action
	action.SetContext(uriAction)
	return uriAction
}

// PdfActionThread represents a thread action.
type PdfActionThread struct {
	*PdfAction
	F *PdfFilespec
	D core.PdfObject
	B core.PdfObject
}

// NewPdfActionThread returns a new "thread" action.
func NewPdfActionThread() *PdfActionThread {
	action := NewPdfAction()
	threadAction := &PdfActionThread{}
	threadAction.PdfAction = action
	action.SetContext(threadAction)
	return threadAction
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionThread) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeThread)))
	if a.F != nil {
		dict.Set("F", a.F.ToPdfObject())
	}
	dict.SetIfNotNil("D", a.D)
	dict.SetIfNotNil("B", a.B)
	return indirect
}

// PdfActionTrans represents a trans action.
type PdfActionTrans struct {
	*PdfAction
	Trans core.PdfObject
}

// NewPdfActionTrans returns a new "trans" action.
func NewPdfActionTrans() *PdfActionTrans {
	action := NewPdfAction()
	transAction := &PdfActionTrans{}
	transAction.PdfAction = action
	action.SetContext(transAction)
	return transAction
}

// ToPdfObject implements interface PdfModel.
func (a *PdfActionTrans) ToPdfObject() core.PdfObject {
	a.PdfAction.ToPdfObject()
	indirect := a.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.SetIfNotNil("S", core.MakeName(string(ActionTypeTrans)))
	dict.SetIfNotNil("Trans", a.Trans)
	return indirect
}

// PdfActionType represents an action type in PDF (section 12.6.4 p. 417).
type PdfActionType string

const (
	ActionTypeGoTo        PdfActionType = "GoTo"
	ActionTypeGoTo3DView  PdfActionType = "GoTo3DView"
	ActionTypeGoToE       PdfActionType = "GoToE"
	ActionTypeGoToR       PdfActionType = "GoToR"
	ActionTypeHide        PdfActionType = "Hide"
	ActionTypeImportData  PdfActionType = "ImportData"
	ActionTypeJavaScript  PdfActionType = "JavaScript"
	ActionTypeLaunch      PdfActionType = "Launch"
	ActionTypeMovie       PdfActionType = "Movie"
	ActionTypeNamed       PdfActionType = "Named"
	ActionTypeRendition   PdfActionType = "Rendition"
	ActionTypeResetForm   PdfActionType = "ResetForm"
	ActionTypeSetOCGState PdfActionType = "SetOCGState"
	ActionTypeSound       PdfActionType = "Sound"
	ActionTypeSubmitForm  PdfActionType = "SubmitForm"
	ActionTypeThread      PdfActionType = "Thread"
	ActionTypeTrans       PdfActionType = "Trans"
	ActionTypeURI         PdfActionType = "URI"
)
