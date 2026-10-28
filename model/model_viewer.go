package model

import (
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/core"
)

// ViewClip returns the value of the viewClip.
func (vp *ViewerPreferences) ViewClip() PageBoundary { return vp.viewClip }

// SetNumCopies sets the value of the numCopies.
func (vp *ViewerPreferences) SetNumCopies(numCopies int) { vp.numCopies = numCopies }

// SetPickTrayByPDFSize sets the value of the pickTrayByPDFSize flag.
func (vp *ViewerPreferences) SetPickTrayByPDFSize(pickTrayByPDFSize bool) {
	vp.pickTrayByPDFSize = &pickTrayByPDFSize
}

// SetHideWindowUI sets the value of the hideWindowUI flag.
func (vp *ViewerPreferences) SetHideWindowUI(hideWindowUI bool) { vp.hideWindowUI = &hideWindowUI }

// NonFullScreenPageMode returns the value of the nonFullScreenPageMode.
func (vp *ViewerPreferences) NonFullScreenPageMode() NonFullScreenPageMode {
	return vp.nonFullScreenPageMode
}

// IsFitWindow returns the value of the fitWindow flag.
func (vp *ViewerPreferences) IsFitWindow() bool {
	if vp.fitWindow == nil {
		return false
	}
	return *vp.fitWindow
}

// SetHideMenubar sets the value of the hideMenubar flag.
func (vp *ViewerPreferences) SetHideMenubar(hideMenubar bool) { vp.hideMenubar = &hideMenubar }

// IsHideMenubar returns the value of the hideMenubar flag.
func (vp *ViewerPreferences) IsHideMenubar() bool {
	if vp.hideMenubar == nil {
		return false
	}
	return *vp.hideMenubar
}

// SetViewClip sets the value of the viewClip.
func (vp *ViewerPreferences) SetViewClip(viewClip PageBoundary) { vp.viewClip = viewClip }

// SetDuplex sets the value of the duplex.
func (vp *ViewerPreferences) SetDuplex(duplex Duplex) { vp.duplex = duplex }

// NewViewerPreferences returns a new ViewerPreferences object with
// default empty values.
func NewViewerPreferences() *ViewerPreferences { return &ViewerPreferences{} }

// ViewArea returns the value of the viewArea.
func (vp *ViewerPreferences) ViewArea() PageBoundary { return vp.viewArea }

// SetPrintScaling sets the value of the printScaling.
func (vp *ViewerPreferences) SetPrintScaling(printScaling PrintScaling) {
	vp.printScaling = printScaling
}

func NewViewerPreferencesFromPdfObject(obj core.PdfObject) (*ViewerPreferences, error) {
	obj2 := core.ResolveReference(obj)
	dict, ok := core.GetDict(obj2)
	if !ok {
		return nil, fmt.Errorf("existing viewer preferences is not a dictionary")
	}
	viewerPreferences := NewViewerPreferences()
	if hideToolbar := dict.Get("HideToolbar"); hideToolbar != nil {
		if hideToolbarBool, ok := core.GetBool(hideToolbar); ok {
			viewerPreferences.hideToolbar = (*bool)(hideToolbarBool)
		}
	}
	if hideMenubar := dict.Get("HideMenubar"); hideMenubar != nil {
		if hideMenubarBool, ok := core.GetBool(hideMenubar); ok {
			viewerPreferences.hideMenubar = (*bool)(hideMenubarBool)
		}
	}
	if hideWindowUI := dict.Get("HideWindowUI"); hideWindowUI != nil {
		if hideWindowUIBool, ok := core.GetBool(hideWindowUI); ok {
			viewerPreferences.hideWindowUI = (*bool)(hideWindowUIBool)
		}
	}
	if fitWindow := dict.Get("FitWindow"); fitWindow != nil {
		if fitWindowBool, ok := core.GetBool(fitWindow); ok {
			viewerPreferences.fitWindow = (*bool)(fitWindowBool)
		}
	}
	if centerWindow := dict.Get("CenterWindow"); centerWindow != nil {
		if centerWindowBool, ok := core.GetBool(centerWindow); ok {
			viewerPreferences.centerWindow = (*bool)(centerWindowBool)
		}
	}
	if displayDocTitle := dict.Get("DisplayDocTitle"); displayDocTitle != nil {
		if displayDocTitleBool, ok := core.GetBool(displayDocTitle); ok {
			viewerPreferences.displayDocTitle = (*bool)(displayDocTitleBool)
		}
	}
	if nonFullScreenPageMode := dict.Get("NonFullScreenPageMode"); nonFullScreenPageMode != nil {
		if nonFullScreenPageModeName, ok := core.GetName(nonFullScreenPageMode); ok {
			viewerPreferences.nonFullScreenPageMode = NonFullScreenPageMode(*nonFullScreenPageModeName)
		}
	}
	if direction := dict.Get("Direction"); direction != nil {
		if directionName, ok := core.GetName(direction); ok {
			viewerPreferences.direction = Direction(*directionName)
		}
	}
	if viewArea := dict.Get("ViewArea"); viewArea != nil {
		if viewAreaName, ok := core.GetName(viewArea); ok {
			viewerPreferences.viewArea = PageBoundary(*viewAreaName)
		}
	}
	if viewClip := dict.Get("ViewClip"); viewClip != nil {
		if viewClipName, ok := core.GetName(viewClip); ok {
			viewerPreferences.viewClip = PageBoundary(*viewClipName)
		}
	}
	if printArea := dict.Get("PrintArea"); printArea != nil {
		if printAreaName, ok := core.GetName(printArea); ok {
			viewerPreferences.printArea = PageBoundary(*printAreaName)
		}
	}
	if printClip := dict.Get("PrintClip"); printClip != nil {
		if printClipName, ok := core.GetName(printClip); ok {
			viewerPreferences.printClip = PageBoundary(*printClipName)
		}
	}
	if printScaling := dict.Get("PrintScaling"); printScaling != nil {
		if printScalingName, ok := core.GetName(printScaling); ok {
			viewerPreferences.printScaling = PrintScaling(*printScalingName)
		}
	}
	if duplex := dict.Get("Duplex"); duplex != nil {
		if duplexName, ok := core.GetName(duplex); ok {
			viewerPreferences.duplex = Duplex(*duplexName)
		}
	}
	if pickTrayByPDFSize := dict.Get("PickTrayByPDFSize"); pickTrayByPDFSize != nil {
		if pickTrayByPDFSizeBool, ok := core.GetBool(pickTrayByPDFSize); ok {
			viewerPreferences.pickTrayByPDFSize = (*bool)(pickTrayByPDFSizeBool)
		}
	}
	if printPageRange := dict.Get("PrintPageRange"); printPageRange != nil {
		if printPageRangeArr, ok := core.GetArray(printPageRange); ok {
			viewerPreferences.printPageRange = make([]int, printPageRangeArr.Len())
			for i := range viewerPreferences.printPageRange {
				if obj := printPageRangeArr.Get(i); obj != nil {
					if num, ok := core.GetInt(obj); ok {
						viewerPreferences.printPageRange[i] = int(*num)
					}
				}
			}
		}
	}
	if numCopies := dict.Get("NumCopies"); numCopies != nil {
		if numCopiesInt, ok := core.GetInt(numCopies); ok {
			viewerPreferences.numCopies = int(*numCopiesInt)
		}
	}
	return viewerPreferences, nil
}

// IsHideToolbar returns the value of the hideToolbar flag.
func (vp *ViewerPreferences) IsHideToolbar() bool {
	if vp.hideToolbar == nil {
		return false
	}
	return *vp.hideToolbar
}

// IsHideWindowUI returns the value of the hideWindowUI flag.
func (vp *ViewerPreferences) IsHideWindowUI() bool {
	if vp.hideWindowUI == nil {
		return false
	}
	return *vp.hideWindowUI
}

// SetHideToolbar sets the value of the hideToolbar flag.
func (vp *ViewerPreferences) SetHideToolbar(hideToolbar bool) { vp.hideToolbar = &hideToolbar }

// PrintArea returns the value of the printArea.
func (vp *ViewerPreferences) PrintArea() PageBoundary { return vp.printArea }

// Direction returns the value of the direction.
func (vp *ViewerPreferences) Direction() Direction { return vp.direction }

// ToPdfObject returns a PDF object representation of the ViewerPreferences.
func (vp *ViewerPreferences) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	if vp.hideToolbar != nil {
		dict.Set("HideToolbar", core.MakeBool(*vp.hideToolbar))
	}
	if vp.hideMenubar != nil {
		dict.Set("HideMenubar", core.MakeBool(*vp.hideMenubar))
	}
	if vp.hideWindowUI != nil {
		dict.Set("HideWindowUI", core.MakeBool(*vp.hideWindowUI))
	}
	if vp.fitWindow != nil {
		dict.Set("FitWindow", core.MakeBool(*vp.fitWindow))
	}
	if vp.centerWindow != nil {
		dict.Set("CenterWindow", core.MakeBool(*vp.centerWindow))
	}
	if vp.displayDocTitle != nil {
		dict.Set("DisplayDocTitle", core.MakeBool(*vp.displayDocTitle))
	}
	if vp.nonFullScreenPageMode != "" {
		dict.Set("NonFullScreenPageMode", core.MakeName(string(vp.nonFullScreenPageMode)))
	}
	if vp.direction != "" {
		dict.Set("Direction", core.MakeName(string(vp.direction)))
	}
	if vp.viewArea != "" {
		dict.Set("ViewArea", core.MakeName(string(vp.viewArea)))
	}
	if vp.viewClip != "" {
		dict.Set("ViewClip", core.MakeName(string(vp.viewClip)))
	}
	if vp.printArea != "" {
		dict.Set("PrintArea", core.MakeName(string(vp.printArea)))
	}
	if vp.printClip != "" {
		dict.Set("PrintClip", core.MakeName(string(vp.printClip)))
	}
	if vp.printScaling != "" {
		dict.Set("PrintScaling", core.MakeName(string(vp.printScaling)))
	}
	if vp.duplex != "" {
		dict.Set("Duplex", core.MakeName(string(vp.duplex)))
	}
	if vp.pickTrayByPDFSize != nil {
		dict.Set("PickTrayByPDFSize", core.MakeBool(*vp.pickTrayByPDFSize))
	}
	if vp.printPageRange != nil {
		dict.Set("PrintPageRange", core.MakeArrayFromIntegers(vp.printPageRange))
	}
	if vp.numCopies != 0 {
		dict.Set("NumCopies", core.MakeInteger(int64(vp.numCopies)))
	}
	return dict
}

// SetDisplayDocTitle sets the value of the displayDocTitle flag.
func (vp *ViewerPreferences) SetDisplayDocTitle(displayDocTitle bool) {
	vp.displayDocTitle = &displayDocTitle
}

// NumCopies returns the value of the numCopies.
func (vp *ViewerPreferences) NumCopies() int { return vp.numCopies }

// IsCenterWindow returns the value of the centerWindow flag.
func (vp *ViewerPreferences) IsCenterWindow() bool {
	if vp.centerWindow == nil {
		return false
	}
	return *vp.centerWindow
}

// SetDirection sets the value of the direction.
func (vp *ViewerPreferences) SetDirection(direction Direction) { vp.direction = direction }

// PickTrayByPDFSize returns the value of the pickTrayByPDFSize flag.
func (vp *ViewerPreferences) PickTrayByPDFSize() bool {
	if vp.pickTrayByPDFSize == nil {
		return false
	}
	return *vp.pickTrayByPDFSize
}

// SetViewArea sets the value of the viewArea.
func (vp *ViewerPreferences) SetViewArea(viewArea PageBoundary) { vp.viewArea = viewArea }

// SetPrintArea sets the value of the printArea.
func (vp *ViewerPreferences) SetPrintArea(printArea PageBoundary) { vp.printArea = printArea }

// PrintScaling returns the value of the printScaling.
func (vp *ViewerPreferences) PrintScaling() PrintScaling { return vp.printScaling }

// SetFitWindow sets the value of the fitWindow flag.
func (vp *ViewerPreferences) SetFitWindow(fitWindow bool) { vp.fitWindow = &fitWindow }

// SetPrintClip sets the value of the printClip.
func (vp *ViewerPreferences) SetPrintClip(printClip PageBoundary) { vp.printClip = printClip }

// SetPrintPageRange sets the value of the printPageRange.
func (vp *ViewerPreferences) SetPrintPageRange(printPageRange []int) {
	vp.printPageRange = printPageRange
}

// ViewerPreferences represents the viewer preferences of a PDF document.
type ViewerPreferences struct {
	hideToolbar           *bool
	hideMenubar           *bool
	hideWindowUI          *bool
	fitWindow             *bool
	centerWindow          *bool
	displayDocTitle       *bool
	nonFullScreenPageMode NonFullScreenPageMode
	direction             Direction
	viewArea              PageBoundary
	viewClip              PageBoundary
	printArea             PageBoundary
	printClip             PageBoundary
	printScaling          PrintScaling
	duplex                Duplex
	pickTrayByPDFSize     *bool
	printPageRange        []int
	numCopies             int
}

// Duplex returns the value of the duplex.
func (vp *ViewerPreferences) Duplex() Duplex { return vp.duplex }

// SetNonFullScreenPageMode sets the value of the nonFullScreenPageMode.
func (vp *ViewerPreferences) SetNonFullScreenPageMode(nonFullScreenPageMode NonFullScreenPageMode) {
	vp.nonFullScreenPageMode = nonFullScreenPageMode
}

// PrintClip returns the value of the printClip.
func (vp *ViewerPreferences) PrintClip() PageBoundary { return vp.printClip }

// IsDisplayDocTitle returns the value of the displayDocTitle flag.
func (vp *ViewerPreferences) IsDisplayDocTitle() bool {
	if vp.displayDocTitle == nil {
		return false
	}
	return *vp.displayDocTitle
}

// SetCenterWindow sets the value of the centerWindow flag.
func (vp *ViewerPreferences) SetCenterWindow(centerWindow bool) { vp.centerWindow = &centerWindow }

// PrintPageRange returns the value of the printPageRange.
func (vp *ViewerPreferences) PrintPageRange() []int { return vp.printPageRange }
