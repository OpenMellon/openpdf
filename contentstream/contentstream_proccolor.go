package contentstream

import (
	"errors"
	"fmt"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

func (proc *ContentStreamProcessor) handleCommand_cm(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	if len(op.Params) != 6 {
		common.Log.Debug("ERROR: Invalid number of parameters for cm: %d", len(op.Params))
		return errors.New("invalid number of parameters")
	}
	floats, err := core.GetNumbersAsFloat(op.Params)
	if err != nil {
		return err
	}
	v := transform.NewMatrix(floats[0], floats[1], floats[2], floats[3], floats[4], floats[5])
	proc.graphicsState.CTM.Concat(v)
	return nil
}

func (proc *ContentStreamProcessor) getColorspace(s string, resources *model.PdfPageResources) (model.PdfColorspace, error) {
	switch s {
	case "DeviceGray":
		return model.NewPdfColorspaceDeviceGray(), nil
	case "DeviceRGB":
		return model.NewPdfColorspaceDeviceRGB(), nil
	case "DeviceCMYK":
		return model.NewPdfColorspaceDeviceCMYK(), nil
	case "Pattern":
		return model.NewPdfColorspaceSpecialPattern(), nil
	}
	if resources != nil {
		cs, ok := resources.GetColorspaceByName(core.PdfObjectName(s))
		if ok {
			return cs, nil
		}
	}
	switch s {
	case "CalGray":
		return model.NewPdfColorspaceCalGray(), nil
	case "CalRGB":
		return model.NewPdfColorspaceCalRGB(), nil
	case "Lab":
		return model.NewPdfColorspaceLab(), nil
	}
	common.Log.Debug("Unknown colorspace requested: %s", s)
	return nil, fmt.Errorf("unsupported colorspace: %s", s)
}

func (proc *ContentStreamProcessor) handleCommand_g(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	v := model.NewPdfColorspaceDeviceGray()
	objs := op.Params
	if len(objs) != v.GetNumComponents() {
		common.Log.Debug("Invalid number of parameters for g")
		common.Log.Debug("Number %d not matching colorspace %T", len(objs), v)
		if !proc.relaxedMode {
			return errInvalidNumberOfParams
		}
		obj, err := autoConvertRGBToGray(objs)
		if err != nil {
			common.Log.Debug("ERROR: Fail to convert color to gray: %+v", objs)
			return err
		}
		objs = []core.PdfObject{obj}
	}
	col, err := v.ColorFromPdfObjects(objs)
	if err != nil {
		common.Log.Debug("ERROR: handleCommand_g Invalid params. cs=%T op=%s err=%v", v, op, err)
		return err
	}
	proc.graphicsState.ColorspaceNonStroking = v
	proc.graphicsState.ColorNonStroking = col
	return nil
}

func (proc *ContentStreamProcessor) handleCommand_scn(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	cs := proc.graphicsState.ColorspaceNonStroking
	objs := op.Params
	if !isPatternColorspace(cs) {
		if len(objs) != cs.GetNumComponents() {
			common.Log.Debug("Invalid number of parameters for SC")
			common.Log.Debug("Number %d not matching colorspace %T", len(objs), cs)
			if !proc.relaxedMode {
				return errInvalidNumberOfParams
			}
			obj, err := autoConvertRGBToGray(objs)
			if err != nil {
				common.Log.Debug("ERROR: Fail to convert color to gray: %+v", objs)
				return err
			}
			objs = []core.PdfObject{obj}
		}
	}
	col, err := cs.ColorFromPdfObjects(objs)
	if err != nil {
		common.Log.Debug("ERROR: Fail to get color from params: %+v (CS is %+v)", objs, cs)
		return err
	}
	proc.graphicsState.ColorNonStroking = col
	return nil
}

func (proc *ContentStreamProcessor) handleCommand_cs(op *ContentStreamOperation, resources *model.PdfPageResources) error {
	if len(op.Params) < 1 {
		common.Log.Debug("Invalid CS command, skipping over")
		return errors.New("too few parameters")
	}
	if len(op.Params) > 1 {
		common.Log.Debug("CS command with too many parameters - continuing")
		return errors.New("too many parameters")
	}
	name, ok := op.Params[0].(*core.PdfObjectName)
	if !ok {
		common.Log.Debug("ERROR: CS command with invalid parameter, skipping over")
		return errors.New("type check error")
	}
	cs, err := proc.getColorspace(string(*name), resources)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorspaceNonStroking = cs
	col, err := proc.getInitialColor(cs)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorNonStroking = col
	return nil
}

func (proc *ContentStreamProcessor) handleCommand_K(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	v := model.NewPdfColorspaceDeviceCMYK()
	if len(op.Params) != v.GetNumComponents() {
		common.Log.Debug("Invalid number of parameters for SC")
		common.Log.Debug("Number %d not matching colorspace %T", len(op.Params), v)
		return errInvalidNumberOfParams
	}
	col, err := v.ColorFromPdfObjects(op.Params)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorspaceStroking = v
	proc.graphicsState.ColorStroking = col
	return nil
}

func isPatternColorspace(cs model.PdfColorspace) bool {
	_, ok := cs.(*model.PdfColorspaceSpecialPattern)
	return ok
}

func (proc *ContentStreamProcessor) handleCommand_rg(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	v := model.NewPdfColorspaceDeviceRGB()
	objs := op.Params
	if len(objs) != v.GetNumComponents() {
		common.Log.Debug("Invalid number of parameters for SC")
		common.Log.Debug("Number %d not matching colorspace %T", len(objs), v)
		if !proc.relaxedMode {
			return errInvalidNumberOfParams
		}
		obj, err := autoConvertRGBToGray(objs)
		if err != nil {
			common.Log.Debug("ERROR: Fail to convert color to gray: %+v", objs)
			return err
		}
		objs = []core.PdfObject{obj}
	}
	col, err := v.ColorFromPdfObjects(objs)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorspaceNonStroking = v
	proc.graphicsState.ColorNonStroking = col
	return nil
}

func (proc *ContentStreamProcessor) handleCommand_SC(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	cs := proc.graphicsState.ColorspaceStroking
	objs := op.Params
	if len(objs) != cs.GetNumComponents() {
		common.Log.Debug("Invalid number of parameters for SC")
		common.Log.Debug("Number %d not matching colorspace %T", len(objs), cs)
		if !proc.relaxedMode {
			return errInvalidNumberOfParams
		}
		obj, err := autoConvertRGBToGray(objs)
		if err != nil {
			common.Log.Debug("ERROR: Fail to convert color to gray: %+v", objs)
			return err
		}
		objs = []core.PdfObject{obj}
	}
	col, err := cs.ColorFromPdfObjects(objs)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorStroking = col
	return nil
}

func (proc *ContentStreamProcessor) handleCommand_CS(op *ContentStreamOperation, resources *model.PdfPageResources) error {
	if len(op.Params) < 1 {
		common.Log.Debug("Invalid cs command, skipping over")
		return errors.New("too few parameters")
	}
	if len(op.Params) > 1 {
		common.Log.Debug("cs command with too many parameters - continuing")
		return errors.New("too many parameters")
	}
	name, ok := op.Params[0].(*core.PdfObjectName)
	if !ok {
		common.Log.Debug("ERROR: cs command with invalid parameter, skipping over")
		return errors.New("type check error")
	}
	cs, err := proc.getColorspace(string(*name), resources)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorspaceStroking = cs
	col, err := proc.getInitialColor(cs)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorStroking = col
	return nil
}

func (proc *ContentStreamProcessor) handleCommand_G(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	v := model.NewPdfColorspaceDeviceGray()
	objs := op.Params
	if len(objs) != v.GetNumComponents() {
		common.Log.Debug("Invalid number of parameters for SC")
		common.Log.Debug("Number %d not matching colorspace %T", len(objs), v)
		if !proc.relaxedMode {
			return errInvalidNumberOfParams
		}
		obj, err := autoConvertRGBToGray(objs)
		if err != nil {
			common.Log.Debug("ERROR: Fail to convert color to gray: %+v", objs)
			return err
		}
		objs = []core.PdfObject{obj}
	}
	col, err := v.ColorFromPdfObjects(objs)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorspaceStroking = v
	proc.graphicsState.ColorStroking = col
	return nil
}

func (proc *ContentStreamProcessor) getInitialColor(cs model.PdfColorspace) (model.PdfColor, error) {
	switch csp := cs.(type) {
	case *model.PdfColorspaceDeviceGray:
		return model.NewPdfColorDeviceGray(0.0), nil
	case *model.PdfColorspaceDeviceRGB:
		return model.NewPdfColorDeviceRGB(0.0, 0.0, 0.0), nil
	case *model.PdfColorspaceDeviceCMYK:
		return model.NewPdfColorDeviceCMYK(0.0, 0.0, 0.0, 1.0), nil
	case *model.PdfColorspaceCalGray:
		return model.NewPdfColorCalGray(0.0), nil
	case *model.PdfColorspaceCalRGB:
		return model.NewPdfColorCalRGB(0.0, 0.0, 0.0), nil
	case *model.PdfColorspaceLab:
		f := 0.0
		f2 := 0.0
		f3 := 0.0
		if csp.Range[0] > 0 {
			f = csp.Range[0]
		}
		if csp.Range[2] > 0 {
			f2 = csp.Range[2]
		}
		return model.NewPdfColorLab(f, f2, f3), nil
	case *model.PdfColorspaceICCBased:
		if csp.Alternate == nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("ICC Based not defined - attempting fall back (N = %d)", csp.N)
			}
			switch csp.N {
			case 1:
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Falling back to DeviceGray")
				}
				return proc.getInitialColor(model.NewPdfColorspaceDeviceGray())
			case 3:
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Falling back to DeviceRGB")
				}
				return proc.getInitialColor(model.NewPdfColorspaceDeviceRGB())
			case 4:
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Falling back to DeviceCMYK")
				}
				return proc.getInitialColor(model.NewPdfColorspaceDeviceCMYK())
			default:
				return nil, errors.New("alternate space not defined for ICC")
			}
		}
		return proc.getInitialColor(csp.Alternate)
	case *model.PdfColorspaceSpecialIndexed:
		if csp.Base == nil {
			return nil, errors.New("indexed base not specified")
		}
		return proc.getInitialColor(csp.Base)
	case *model.PdfColorspaceSpecialSeparation:
		if csp.AlternateSpace == nil {
			return nil, errors.New("alternate space not specified")
		}
		return proc.getInitialColor(csp.AlternateSpace)
	case *model.PdfColorspaceDeviceN:
		if csp.AlternateSpace == nil {
			return nil, errors.New("alternate space not specified")
		}
		return proc.getInitialColor(csp.AlternateSpace)
	case *model.PdfColorspaceSpecialPattern:
		return model.NewPdfColorPattern(), nil
	}
	common.Log.Debug("Unable to determine initial color for unknown colorspace: %T", cs)
	return nil, errors.New("unsupported colorspace")
}

func (proc *ContentStreamProcessor) handleCommand_k(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	v := model.NewPdfColorspaceDeviceCMYK()
	if len(op.Params) != v.GetNumComponents() {
		common.Log.Debug("Invalid number of parameters for SC")
		common.Log.Debug("Number %d not matching colorspace %T", len(op.Params), v)
		return errInvalidNumberOfParams
	}
	col, err := v.ColorFromPdfObjects(op.Params)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorspaceNonStroking = v
	proc.graphicsState.ColorNonStroking = col
	return nil
}

func (proc *ContentStreamProcessor) handleCommand_RG(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	v := model.NewPdfColorspaceDeviceRGB()
	objs := op.Params
	if len(objs) != v.GetNumComponents() {
		common.Log.Debug("Invalid number of parameters for RG")
		common.Log.Debug("Number %d not matching colorspace %T", len(objs), v)
		if !proc.relaxedMode {
			return errInvalidNumberOfParams
		}
		obj, err := autoConvertRGBToGray(objs)
		if err != nil {
			common.Log.Debug("ERROR: Fail to convert color to gray: %+v", objs)
			return err
		}
		objs = []core.PdfObject{obj}
	}
	col, err := v.ColorFromPdfObjects(objs)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorspaceStroking = v
	proc.graphicsState.ColorStroking = col
	return nil
}

func (proc *ContentStreamProcessor) handleCommand_sc(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	cs := proc.graphicsState.ColorspaceNonStroking
	objs := op.Params
	if !isPatternColorspace(cs) {
		if len(objs) != cs.GetNumComponents() {
			common.Log.Debug("Invalid number of parameters for SC")
			common.Log.Debug("Number %d not matching colorspace %T", len(objs), cs)
			if !proc.relaxedMode {
				return errInvalidNumberOfParams
			}
			obj, err := autoConvertRGBToGray(objs)
			if err != nil {
				common.Log.Debug("ERROR: Fail to convert color to gray: %+v", objs)
				return err
			}
			objs = []core.PdfObject{obj}
		}
	}
	col, err := cs.ColorFromPdfObjects(objs)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorNonStroking = col
	return nil
}

func (proc *ContentStreamProcessor) handleCommand_SCN(op *ContentStreamOperation, _ *model.PdfPageResources) error {
	cs := proc.graphicsState.ColorspaceStroking
	objs := op.Params
	if !isPatternColorspace(cs) {
		if len(objs) != cs.GetNumComponents() {
			common.Log.Debug("Invalid number of parameters for SC")
			common.Log.Debug("Number %d not matching colorspace %T", len(objs), cs)
			if !proc.relaxedMode {
				return errInvalidNumberOfParams
			}
			obj, err := autoConvertRGBToGray(objs)
			if err != nil {
				common.Log.Debug("ERROR: Fail to convert color to gray: %+v", objs)
				return err
			}
			objs = []core.PdfObject{obj}
		}
	}
	col, err := cs.ColorFromPdfObjects(objs)
	if err != nil {
		return err
	}
	proc.graphicsState.ColorStroking = col
	return nil
}
