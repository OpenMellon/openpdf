package extractor

import (
	"errors"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

func (e *Extractor) extractPageTextFromOps(ops *contentstream.ContentStreamOperations, resources *model.PdfPageResources, parentCTM transform.Matrix, formDepth int, isAnnotation bool, initGS *contentstream.GraphicsState) (*PageText, int, int, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("extractPageTextFromOps: level=%d", formDepth)
	}
	pageText := &PageText{pageSize: e.mediaBox, structTreeRoot: e.structTreeRoot, pageObject: e.pageObject}
	state := newTextState(e.mediaBox)
	var stack stateStack
	var gs contentstream.GraphicsState
	if initGS != nil {
		gs = *initGS
	}
	tobj := newTextObject(e, resources, gs, &state, &stack)
	shapes := shapesState{parentCTM: parentCTM, ctm: transform.IdentityMatrix(), textObj: tobj}
	var inText bool
	mcid := -1
	actualText := ""
	if formDepth > maxFormDepth {
		err := errors.New("form stack overflow")
		common.Log.Debug("ERROR: extractPageTextFromOps. recursion level=%d err=%v", formDepth, err)
		return pageText, state.numChars, state.numMisparsedChars, err
	}
	pageText.ops = ops
	proc := contentstream.NewContentStreamProcessor(*ops)
	if initGS != nil {
		proc.SetInitialGraphicsState(*initGS)
	}
	if e.options != nil {
		proc.SetRelaxedMode(e.options.RelaxedMode)
	}
	proc.AddHandler(contentstream.HandlerConditionEnumAllOperands, "", func(op *contentstream.ContentStreamOperation, gstate contentstream.GraphicsState, res *model.PdfPageResources) error {
		operand := op.Operand
		if debugOperands {
			common.Log.Info("&&& op=%s", op)
		}
		switch operand {
		case "q":
			if debugShapes {
				common.Log.Info("ctm=%s", shapes.ctm)
			}
			stack.push(&state)
		case "Q":
			if !stack.empty() {
				state = *stack.pop()
			}
			shapes.ctm = gstate.CTM
			if debugShapes {
				common.Log.Info("ctm=%s", shapes.ctm)
			}
		case "BDC":
			if len(op.Params) < 2 {
				common.Log.Debug("ERROR: BDC op=%s insufficient params", op)
				return nil
			}
			dict, ok := core.GetDict(op.Params[1])
			if !ok {
				common.Log.Debug("ERROR: BDC op=%s GetDict failed", op)
				return nil
			}
			mcidObj := dict.Get("MCID")
			if mcidObj != nil {
				v, ok2 := core.GetIntVal(mcidObj)
				if !ok2 {
					common.Log.Debug("ERROR: BDC op=%s. Bad numerical object. o=%s", op, mcidObj)
				}
				mcid = v
			} else {
				mcid = -1
			}
			if e.structTreeRoot != nil && mcid != -1 && e.structParentsKey != -1 && e.structTreeRoot.ParentTree != nil {
				parentsKey := e.structParentsKey
				var walk func(kvs []*model.KValue) bool
				walk = func(kvs []*model.KValue) bool {
					for _, kv := range kvs {
						if kd := kv.GetKDict(); kd != nil {
							children := kd.GetChildren()
							if len(children) == 1 && children[0].GetMCID() != nil {
								if *children[0].GetMCID() == mcid {
									if kd.ActualText != nil {
										actualText = kd.ActualText.Decoded()
										return true
									}
									return false
								}
							} else {
								return walk(children)
							}
						}
					}
					return false
				}
				if nums := e.structTreeRoot.ParentTree.Get("Nums"); nums != nil {
					arr, ok2 := core.GetArray(nums)
					if !ok2 {
						common.Log.Debug("ERROR: BDC op=%s. Bad Nums array. o=%s", op, nums)
					} else {
						for i := 0; i < arr.Len(); i += 2 {
							if key, ok3 := core.GetInt(arr.Get(i)); ok3 {
								if int(*key) == parentsKey {
									if entry := arr.Get(i + 1); entry != nil {
										if entries, ok4 := core.GetArray(entry); ok4 {
											for _, kdObj := range entries.Elements() {
												kd, err := model.NewKDictFromPdfObject(kdObj)
												if err != nil {
													common.Log.Debug("ERROR: BDC op=%s. Bad KDict. o=%s", op, kdObj)
													continue
												}
												children := kd.GetChildren()
												if len(children) == 1 && children[0].GetMCID() != nil {
													if *children[0].GetMCID() == mcid {
														if kd.ActualText != nil {
															actualText = kd.ActualText.Decoded()
														}
														break
													} else if walk(children) {
														break
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
			if actualText == "" {
				if str, ok2 := core.GetString(dict.Get("ActualText")); ok2 {
					actualText = str.Decoded()
				}
			}
		case "EMC":
			mcid = -1
			actualText = ""
		case "BT":
			if inText {
				common.Log.Debug("BT called while in a text object")
				pageText.marks = append(pageText.marks, tobj.marks...)
			}
			inText = true
			btGS := gstate
			if isAnnotation {
				btGS = contentstream.GraphicsState{}
				btGS.CTM = shapes.ctm
			}
			btGS.CTM = parentCTM.Mult(btGS.CTM)
			tobj = newTextObject(e, res, btGS, &state, &stack)
			shapes.textObj = tobj
		case "ET":
			if !inText {
				common.Log.Debug("ET called outside of a text object")
			}
			inText = false
			pageText.marks = append(pageText.marks, tobj.marks...)
			tobj.reset()
		case "T*":
			tobj.nextLine()
		case "Td":
			if ok, err := tobj.checkOp(op, 2, true); !ok {
				common.Log.Debug("ERROR: err=%v", err)
				return err
			}
			tx, ty, err2 := twoFloatOperands(op.Params)
			if err2 != nil {
				return err2
			}
			tobj.moveText(tx, ty)
		case "TD":
			if ok, err := tobj.checkOp(op, 2, true); !ok {
				common.Log.Debug("ERROR: err=%v", err)
				return err
			}
			tx, ty, err2 := twoFloatOperands(op.Params)
			if err2 != nil {
				common.Log.Debug("ERROR: err=%v", err2)
				return err2
			}
			tobj.moveTextSetLeading(tx, ty)
		case "Tj":
			if ok, err := tobj.checkOp(op, 1, true); !ok {
				common.Log.Debug("ERROR: Tj op=%s err=%v", op, err)
				return err
			}
			obj := core.TraceToDirectObject(op.Params[0])
			stringBytes, ok2 := core.GetStringBytes(obj)
			if !ok2 {
				common.Log.Debug("ERROR: Tj op=%s GetStringBytes failed", op)
				return core.ErrTypeError
			}
			return tobj.showText(obj, stringBytes, mcid, actualText)
		case "TJ":
			if ok, err := tobj.checkOp(op, 1, true); !ok {
				common.Log.Debug("ERROR: TJ err=%v", err)
				return err
			}
			array, ok2 := core.GetArray(op.Params[0])
			if !ok2 {
				common.Log.Debug("ERROR: TJ op=%s GetArrayVal failed", op)
				return nil
			}
			return tobj.showTextAdjusted(array, mcid, actualText)
		case "'":
			if ok, err := tobj.checkOp(op, 1, true); !ok {
				common.Log.Debug("ERROR: ' err=%v", err)
				return err
			}
			obj := core.TraceToDirectObject(op.Params[0])
			stringBytes, ok2 := core.GetStringBytes(obj)
			if !ok2 {
				common.Log.Debug("ERROR: ' op=%s GetStringBytes failed", op)
				return core.ErrTypeError
			}
			tobj.nextLine()
			return tobj.showText(obj, stringBytes, mcid, actualText)
		case "\"":
			if ok, err := tobj.checkOp(op, 3, true); !ok {
				common.Log.Debug("ERROR: \" err=%v", err)
				return err
			}
			charSpace, wordSpace, err2 := twoFloatOperands(op.Params[:2])
			if err2 != nil {
				return err2
			}
			obj := core.TraceToDirectObject(op.Params[2])
			stringBytes, ok2 := core.GetStringBytes(obj)
			if !ok2 {
				common.Log.Debug("ERROR: \" op=%s GetStringBytes failed", op)
				return core.ErrTypeError
			}
			tobj.setCharSpacing(charSpace)
			tobj.setWordSpacing(wordSpace)
			tobj.nextLine()
			return tobj.showText(obj, stringBytes, mcid, actualText)
		case "TL":
			leading, err := singleFloatOperand(op)
			if err != nil {
				common.Log.Debug("ERROR: TL err=%v", err)
				return err
			}
			tobj.setTextLeading(leading)
		case "Tc":
			charSpace, err := singleFloatOperand(op)
			if err != nil {
				common.Log.Debug("ERROR: Tc err=%v", err)
				return err
			}
			tobj.setCharSpacing(charSpace)
		case "Tf":
			if ok, err := tobj.checkOp(op, 2, true); !ok {
				common.Log.Debug("ERROR: Tf err=%v", err)
				return err
			}
			nameVal, ok2 := core.GetNameVal(op.Params[0])
			if !ok2 {
				common.Log.Debug("ERROR: Tf op=%s GetNameVal failed", op)
				return core.ErrTypeError
			}
			numberAsFloat, err2 := core.GetNumberAsFloat(op.Params[1])
			if !ok2 {
				common.Log.Debug("ERROR: Tf op=%s GetFloatVal failed. err=%v", op, err2)
				return err2
			}
			err2 = tobj.setFont(nameVal, numberAsFloat)
			tobj.unsupportedFont = errors.Is(err2, core.ErrNotSupported)
			if err2 != nil && !tobj.unsupportedFont {
				return err2
			}
		case "Tm":
			if ok, err := tobj.checkOp(op, 6, true); !ok {
				common.Log.Debug("ERROR: Tm err=%v", err)
				return err
			}
			numbersAsFloat, err2 := core.GetNumbersAsFloat(op.Params)
			if err2 != nil {
				common.Log.Debug("ERROR: err=%v", err2)
				return err2
			}
			tobj.setTextMatrix(numbersAsFloat)
		case "Tr":
			if ok, err := tobj.checkOp(op, 1, true); !ok {
				common.Log.Debug("ERROR: Tr err=%v", err)
				return err
			}
			intVal, ok2 := core.GetIntVal(op.Params[0])
			if !ok2 {
				common.Log.Debug("ERROR: Tr op=%s GetIntVal failed", op)
				return core.ErrTypeError
			}
			tobj.setTextRenderMode(intVal)
		case "Ts":
			if ok, err := tobj.checkOp(op, 1, true); !ok {
				common.Log.Debug("ERROR: Ts err=%v", err)
				return err
			}
			numberAsFloat, err2 := core.GetNumberAsFloat(op.Params[0])
			if err2 != nil {
				common.Log.Debug("ERROR: err=%v", err2)
				return err2
			}
			tobj.setTextRise(numberAsFloat)
		case "Tw":
			if ok, err := tobj.checkOp(op, 1, true); !ok {
				common.Log.Debug("ERROR: err=%v", err)
				return err
			}
			numberAsFloat, err2 := core.GetNumberAsFloat(op.Params[0])
			if err2 != nil {
				common.Log.Debug("ERROR: err=%v", err2)
				return err2
			}
			tobj.setWordSpacing(numberAsFloat)
		case "Tz":
			if ok, err := tobj.checkOp(op, 1, true); !ok {
				common.Log.Debug("ERROR: err=%v", err)
				return err
			}
			numberAsFloat, err2 := core.GetNumberAsFloat(op.Params[0])
			if err2 != nil {
				common.Log.Debug("ERROR: err=%v", err2)
				return err2
			}
			tobj.setHorizScaling(numberAsFloat)
		case "cm":
			if !isAnnotation {
				shapes.ctm = gstate.CTM
			}
			if shapes.ctm.Singular() {
				fixed := transform.IdentityMatrix().Translate(shapes.ctm.Translation())
				common.Log.Debug("Singular ctm=%s→%s", shapes.ctm, fixed)
				shapes.ctm = fixed
			}
			if debugShapes {
				common.Log.Info("ctm=%s", shapes.ctm)
			}
			if inText {
				tobj.gs.CTM = parentCTM.Mult(gstate.CTM)
			}
		case "m":
			if e.isTextOnly() {
				break
			}
			if len(op.Params) != 2 {
				common.Log.Debug("WARN: error while processing `m` operator: %v. Output may be incorrect.", errRangeCheck)
				return nil
			}
			numbersAsFloat, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				return err
			}
			shapes.moveTo(numbersAsFloat[0], numbersAsFloat[1])
		case "l":
			if e.isTextOnly() {
				break
			}
			if len(op.Params) != 2 {
				common.Log.Debug("WARN: error while processing `l` operator: %v. Output may be incorrect.", errRangeCheck)
				return nil
			}
			numbersAsFloat, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				return err
			}
			shapes.lineTo(numbersAsFloat[0], numbersAsFloat[1])
		case "c":
			if e.isTextOnly() {
				break
			}
			if len(op.Params) != 6 {
				return errRangeCheck
			}
			numbersAsFloat, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				return err
			}
			common.Log.Debug("Cubic bezier params: %.2f", numbersAsFloat)
			shapes.cubicTo(numbersAsFloat[0], numbersAsFloat[1], numbersAsFloat[2], numbersAsFloat[3], numbersAsFloat[4], numbersAsFloat[5])
		case "v", "y":
			if e.isTextOnly() {
				break
			}
			if len(op.Params) != 4 {
				return errRangeCheck
			}
			numbersAsFloat, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				return err
			}
			common.Log.Debug("Cubic bezier params: %.2f", numbersAsFloat)
			shapes.quadraticTo(numbersAsFloat[0], numbersAsFloat[1], numbersAsFloat[2], numbersAsFloat[3])
		case "h":
			if !e.isTextOnly() {
				shapes.closePath()
			}
		case "re":
			if e.isTextOnly() {
				break
			}
			if len(op.Params) != 4 {
				return errRangeCheck
			}
			numbersAsFloat, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				return err
			}
			shapes.drawRectangle(numbersAsFloat[0], numbersAsFloat[1], numbersAsFloat[2], numbersAsFloat[3])
			shapes.closePath()
		case "S":
			if !e.isTextOnly() {
				shapes.stroke(&pageText.strokes)
			}
			shapes.clearPath()
		case "s":
			if !e.isTextOnly() {
				shapes.closePath()
				shapes.stroke(&pageText.strokes)
			}
			shapes.clearPath()
		case "F":
			if !e.isTextOnly() {
				shapes.fill(&pageText.fills)
			}
			shapes.clearPath()
		case "f", "f*":
			if !e.isTextOnly() {
				shapes.closePath()
				shapes.fill(&pageText.fills)
			}
			shapes.clearPath()
		case "B", "B*":
			if !e.isTextOnly() {
				shapes.fill(&pageText.fills)
				shapes.stroke(&pageText.strokes)
			}
			shapes.clearPath()
		case "b", "b*":
			if !e.isTextOnly() {
				shapes.closePath()
				shapes.fill(&pageText.fills)
				shapes.stroke(&pageText.strokes)
			}
			shapes.clearPath()
		case "n":
			shapes.clearPath()
		case "Do":
			if len(op.Params) == 0 {
				common.Log.Debug("ERROR: expected XObject name operand for Do operator. Got %+v.", op.Params)
				return core.ErrRangeError
			}
			name, ok := core.GetName(op.Params[0])
			if !ok {
				common.Log.Debug("ERROR: invalid Do operator XObject name operand: %+v.", op.Params[0])
				return core.ErrTypeError
			}
			xObjectByName, xType := res.GetXObjectByName(*name)
			if xType != model.XObjectTypeForm {
				break
			}
			form, ok := e.parsedForms[xObjectByName]
			if !ok {
				xObjectFormByName, err := res.GetXObjectFormByName(*name)
				if err != nil {
					common.Log.Debug("ERROR: %v", err)
					return err
				}
				contentStream, err := xObjectFormByName.GetContentStream()
				if err != nil {
					common.Log.Debug("ERROR: %v", err)
					return err
				}
				contentStreamParser, err := contentstream.NewContentStreamParser(string(contentStream)).Parse()
				if err != nil {
					common.Log.Debug("ERROR: form parse failed. err=%v", err)
					return err
				}
				form = parsedForm{ops: contentStreamParser, form: xObjectFormByName}
				e.parsedForms[xObjectByName] = form
			}
			resources2 := form.form.Resources
			if resources2 == nil {
				resources2 = res
			}
			formGS := contentstream.GraphicsState{ColorspaceStroking: gstate.ColorspaceStroking, ColorspaceNonStroking: gstate.ColorspaceNonStroking, ColorStroking: gstate.ColorStroking, ColorNonStroking: gstate.ColorNonStroking}
			key := makeFormCacheKey(xObjectByName, parentCTM.Mult(gstate.CTM), resources2, formGS)
			result, ok := e.formResults[key]
			if !ok {
				ctm := gstate.CTM
				if array, ok2 := core.GetArray(form.form.Matrix); ok2 {
					m, err := transform.MatrixFromPdfObject(array)
					if err != nil {
						return err
					}
					ctm = gstate.CTM.Mult(m)
				}
				e.formExtractions++
				formText, numChars, numMisparsed, err2 := e.extractPageTextFromOps(form.ops, resources2, parentCTM.Mult(ctm), formDepth+1, false, &formGS)
				var fpe *formProcessingError
				if err2 != nil && !errors.As(err2, &fpe) && (key.strokeColor != blackColorKey || key.fillColor != blackColorKey) {
					e.formExtractions++
					formText, numChars, numMisparsed, err2 = e.extractPageTextFromOps(form.ops, resources2, parentCTM.Mult(ctm), formDepth+1, false, nil)
				}
				if err2 != nil {
					common.Log.Debug("ERROR: %v", err2)
					return &formProcessingError{name: name.String(), err: err2}
				}
				result = textResult{pageText: *formText, numChars: numChars, numMisparsedChars: numMisparsed, form: form.form}
				e.formResults[key] = result
			}
			shapes.ctm = gstate.CTM
			if debugShapes {
				common.Log.Info("ctm=%s", shapes.ctm)
			}
			pageText.marks = append(pageText.marks, result.pageText.marks...)
			pageText.strokes = append(pageText.strokes, result.pageText.strokes...)
			pageText.fills = append(pageText.fills, result.pageText.fills...)
			state.numChars += result.numChars
			state.numMisparsedChars += result.numMisparsedChars
			if result.form != nil {
				pageText.appendFormContent(name.String(), result.form, result.pageText.ops)
				for _, form2 := range result.pageText.forms {
					pageText.appendFormContent(form2.Name, form2.Form, form2.Operations)
				}
			}
		case "rg", "g", "k", "cs", "sc", "scn":
			tobj.gs.ColorspaceNonStroking = gstate.ColorspaceNonStroking
			tobj.gs.ColorNonStroking = gstate.ColorNonStroking
		case "RG", "G", "K", "CS", "SC", "SCN":
			tobj.gs.ColorspaceStroking = gstate.ColorspaceStroking
			tobj.gs.ColorStroking = gstate.ColorStroking
		}
		return nil
	})
	err3 := proc.Process(resources)
	if e.options != nil && e.options.IncludeAnnotations && !isAnnotation {
		for _, annotation := range e.annotations {
			dict, ok := core.GetDict(annotation.AP)
			if !ok {
				continue
			}
			get, ok := dict.Get("N").(*core.PdfObjectStream)
			if !ok {
				continue
			}
			decodeStream, err2 := core.DecodeStream(get)
			if err2 != nil {
				common.Log.Debug("Error on decode stream: %v", err2)
				continue
			}
			apObj := get.PdfObjectDictionary.Get("Resources")
			pdfPageResourcesFromDict, err2 := model.NewPdfPageResourcesFromDict(apObj.(*core.PdfObjectDictionary))
			if err2 != nil {
				common.Log.Debug("Error on getting annotation resources: %v", err2)
				continue
			}
			m := transform.IdentityMatrix()
			if get2, ok2 := get.PdfObjectDictionary.Get("Matrix").(*core.PdfObjectArray); ok2 {
				matrixFromPdfObject, err := transform.MatrixFromPdfObject(get2)
				if err != nil {
					common.Log.Debug("Invalid annotation appearance Matrix: %v", err)
					continue
				}
				m = matrixFromPdfObject
			}
			result, ok := e.annotationResults[get.String()]
			if !ok {
				pageText2, numChars, numMisparsed, err := e.extractPageText(string(decodeStream), pdfPageResourcesFromDict, m, formDepth+1, true, nil)
				if err != nil {
					common.Log.Debug("ERROR extracting annotation texts: %v", err)
					continue
				}
				result = textResult{pageText: *pageText2, numChars: numChars, numMisparsedChars: numMisparsed}
				e.annotationResults[get.String()] = result
			}
			pageText.marks = append(pageText.marks, result.pageText.marks...)
			pageText.strokes = append(pageText.strokes, result.pageText.strokes...)
			pageText.fills = append(pageText.fills, result.pageText.fills...)
			state.numChars += result.numChars
			state.numMisparsedChars += result.numMisparsedChars
		}
	}
	return pageText, state.numChars, state.numMisparsedChars, err3
}
