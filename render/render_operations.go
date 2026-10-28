package render

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/render/internal/context"
)

func (r *renderer) renderOperations(ctx context.Context, ops *contentstream.ContentStreamOperations, pageRes *model.PdfPageResources, baseMatrix transform.Matrix) error {
	prevBase := r.baseMatrix
	r.baseMatrix = baseMatrix
	defer func() { r.baseMatrix = prevBase }()
	if r.fontByName == nil {
		r.fontByName = map[string]*context.TextFont{}
	}
	if r.textFontByDict == nil {
		r.textFontByDict = map[*core.PdfObjectDictionary]*context.TextFont{}
	}
	if r.pdfFontByDict == nil {
		r.pdfFontByDict = map[*core.PdfObjectDictionary]*model.PdfFont{}
	}
	ts := ctx.TextState()
	ts.GlobalScale = r.scale
	finder := systemFontFinder()
	var prevOp *contentstream.ContentStreamOperation
	var clipPending bool
	var clipRule context.FillRule
	depth := 0
	proc := contentstream.NewContentStreamProcessor(*ops)
	proc.AddHandler(contentstream.HandlerConditionEnumAllOperands, "", func(op *contentstream.ContentStreamOperation, gs contentstream.GraphicsState, res *model.PdfPageResources) error {
		if r.decodeLimitErr != nil {
			return r.decodeLimitErr
		}
		common.Log.Debug("Processing %s", op.Operand)
		switch op.Operand {
		case "q":
			ctx.Push()
			depth++
		case "Q":
			if depth == 0 {
				common.Log.Debug("Unbalanced Q operator; ignoring")
				break
			}
			ctx.Pop()
			depth--
			ts = ctx.TextState()
		case "cm":
			if len(op.Params) != 6 {
				return errRangeCheck
			}
			vals, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				return err
			}
			m := transform.NewMatrix(vals[0], vals[1], vals[2], vals[3], vals[4], vals[5])
			common.Log.Debug("Graphics state matrix: %+v", m)
			ctx.SetMatrix(ctx.Matrix().Mult(m))
		case "w":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			widthVals, widthErr := core.GetNumbersAsFloat(op.Params)
			if widthErr != nil {
				return widthErr
			}
			ctx.SetLineWidth(widthVals[0])
		case "J":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			capStyle, capOK := core.GetIntVal(op.Params[0])
			if !capOK {
				return errTypeCheck
			}
			switch capStyle {
			case 0:
				ctx.SetLineCap(context.LineCapButt)
			case 1:
				ctx.SetLineCap(context.LineCapRound)
			case 2:
				ctx.SetLineCap(context.LineCapSquare)
			default:
				common.Log.Debug("Invalid line cap style: %d", capStyle)
				return errRangeCheck
			}
		case "j":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			joinStyle, joinOK := core.GetIntVal(op.Params[0])
			if !joinOK {
				return errTypeCheck
			}
			switch joinStyle {
			case 0:
				ctx.SetLineJoin(context.LineJoinBevel)
			case 1:
				ctx.SetLineJoin(context.LineJoinRound)
			case 2:
				ctx.SetLineJoin(context.LineJoinBevel)
			default:
				common.Log.Debug("Invalid line join style: %d", joinStyle)
				return errRangeCheck
			}
		case "M":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			miterVals, miterErr := core.GetNumbersAsFloat(op.Params)
			if miterErr != nil {
				return miterErr
			}
			_ = miterVals
			common.Log.Debug("Miter limit not supported")
		case "d":
			if len(op.Params) != 2 {
				return errRangeCheck
			}
			dashArr, dashOK := core.GetArray(op.Params[0])
			if !dashOK {
				return errTypeCheck
			}
			dashPhase, dashOK := core.GetIntVal(op.Params[1])
			if !dashOK {
				_, phaseFloatOK := core.GetFloatVal(op.Params[1])
				if !phaseFloatOK {
					return errTypeCheck
				}
			}
			dashes, dashErr := core.GetNumbersAsFloat(dashArr.Elements())
			if dashErr != nil {
				return dashErr
			}
			ctx.SetDash(dashes...)
			_ = dashPhase
			common.Log.Debug("Line dash phase not supported")
		case "ri":
			common.Log.Debug("Rendering intent not supported")
		case "i":
			common.Log.Debug("Flatness tolerance not supported")
		case "gs":
			if err := r.applyExtGState(ctx, op, gs, res); err != nil {
				return err
			}
		case "m":
			if len(op.Params) != 2 {
				common.Log.Debug("WARN: error while processing `m` operator: %s. Output may be incorrect.", errRangeCheck)
				return nil
			}
			moveVals, moveErr := core.GetNumbersAsFloat(op.Params)
			if moveErr != nil {
				return moveErr
			}
			common.Log.Debug("Move to: %v", moveVals)
			ctx.NewSubPath()
			ctx.MoveTo(moveVals[0], moveVals[1])
		case "l":
			if len(op.Params) != 2 {
				common.Log.Debug("WARN: error while processing `l` operator: %s. Output may be incorrect.", errRangeCheck)
				return nil
			}
			lineVals, lineErr := core.GetNumbersAsFloat(op.Params)
			if lineErr != nil {
				return lineErr
			}
			ctx.LineTo(lineVals[0], lineVals[1])
		case "c":
			if len(op.Params) != 6 {
				return errRangeCheck
			}
			curveVals, curveErr := core.GetNumbersAsFloat(op.Params)
			if curveErr != nil {
				return curveErr
			}
			common.Log.Debug("Cubic bezier params: %+v", curveVals)
			ctx.CubicTo(curveVals[0], curveVals[1], curveVals[2], curveVals[3], curveVals[4], curveVals[5])
		case "v", "y":
			if len(op.Params) != 4 {
				return errRangeCheck
			}
			quadVals, quadErr := core.GetNumbersAsFloat(op.Params)
			if quadErr != nil {
				return quadErr
			}
			common.Log.Debug("Cubic bezier params: %+v", quadVals)
			ctx.QuadraticTo(quadVals[0], quadVals[1], quadVals[2], quadVals[3])
		case "h":
			ctx.ClosePath()
			ctx.NewSubPath()
		case "re":
			if len(op.Params) != 4 {
				return errRangeCheck
			}
			rectVals, rectErr := core.GetNumbersAsFloat(op.Params)
			if rectErr != nil {
				return rectErr
			}
			ctx.DrawRectangle(rectVals[0], rectVals[1], rectVals[2], rectVals[3])
			ctx.NewSubPath()
		case "S":
			if err := setStrokeFromColorspace(ctx, gs.ColorspaceStroking, gs.ColorStroking); err != nil {
				common.Log.Debug("Error converting color: %v", err)
				return err
			}
			ctx.Stroke()
		case "s":
			if err := setStrokeFromColorspace(ctx, gs.ColorspaceStroking, gs.ColorStroking); err != nil {
				common.Log.Debug("Error converting color: %v", err)
				return err
			}
			ctx.ClosePath()
			ctx.NewSubPath()
			ctx.Stroke()
		case "f", "F":
			if err := setFillFromColorspace(ctx, gs.ColorspaceNonStroking, gs.ColorNonStroking); err != nil {
				common.Log.Debug("Error converting color: %v", err)
				return err
			}
			ctx.SetFillRule(context.FillRuleWinding)
			ctx.Fill()
		case "f*":
			if err := setFillFromColorspace(ctx, gs.ColorspaceNonStroking, gs.ColorNonStroking); err != nil {
				common.Log.Debug("Error converting color: %v", err)
				return err
			}
			ctx.SetFillRule(context.FillRuleEvenOdd)
			ctx.Fill()
		case "B":
			if err := fillAndStroke(ctx, gs, context.FillRuleWinding, false); err != nil {
				common.Log.Debug("Error converting color: %v", err)
				return err
			}
		case "B*":
			if err := fillAndStroke(ctx, gs, context.FillRuleEvenOdd, false); err != nil {
				common.Log.Debug("Error converting color: %v", err)
				return err
			}
		case "b":
			ctx.ClosePath()
			if err := fillAndStroke(ctx, gs, context.FillRuleWinding, true); err != nil {
				common.Log.Debug("Error converting color: %v", err)
				return err
			}
		case "b*":
			ctx.ClosePath()
			if err := fillAndStroke(ctx, gs, context.FillRuleEvenOdd, true); err != nil {
				common.Log.Debug("Error converting color: %v", err)
				return err
			}
		case "n":
			if clipPending {
				ctx.SetFillRule(clipRule)
				ctx.ClipPreserve()
				clipPending = false
			}
			ctx.ClearPath()
		case "W":
			clipPending = true
			clipRule = context.FillRuleWinding
		case "W*":
			clipPending = true
			clipRule = context.FillRuleEvenOdd
		case "rg":
			devRGB, ok := gs.ColorNonStroking.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", gs.ColorNonStroking)
				return nil
			}
			ctx.SetFillRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "RG":
			devRGB, ok := gs.ColorStroking.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", gs.ColorStroking)
				return nil
			}
			ctx.SetStrokeRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "k":
			cmyk, ok := gs.ColorNonStroking.(*model.PdfColorDeviceCMYK)
			if !ok {
				common.Log.Debug("Error converting color: %v", gs.ColorNonStroking)
				return nil
			}
			rgb, err := gs.ColorspaceNonStroking.ColorToRGB(cmyk)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorNonStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetFillRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "K":
			cmyk, ok := gs.ColorStroking.(*model.PdfColorDeviceCMYK)
			if !ok {
				common.Log.Debug("Error converting color: %v", gs.ColorStroking)
				return nil
			}
			rgb, err := gs.ColorspaceStroking.ColorToRGB(cmyk)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetStrokeRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "g":
			gray, ok := gs.ColorNonStroking.(*model.PdfColorDeviceGray)
			if !ok {
				common.Log.Debug("Error converting color: %v", gs.ColorNonStroking)
				return nil
			}
			rgb, err := gs.ColorspaceNonStroking.ColorToRGB(gray)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorNonStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetFillRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "G":
			gray, ok := gs.ColorStroking.(*model.PdfColorDeviceGray)
			if !ok {
				common.Log.Debug("Error converting color: %v", gs.ColorStroking)
				return nil
			}
			rgb, err := gs.ColorspaceStroking.ColorToRGB(gray)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetStrokeRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "cs":
			if len(op.Params) > 0 {
				if name, ok := core.GetName(op.Params[0]); ok && name.String() == "Pattern" {
					break
				}
			}
			rgb, err := gs.ColorspaceNonStroking.ColorToRGB(gs.ColorNonStroking)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorNonStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetFillRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "sc":
			rgb, err := gs.ColorspaceNonStroking.ColorToRGB(gs.ColorNonStroking)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorNonStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetFillRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "scn":
			if pattern, isShading := r.scnPattern(ctx, op, prevOp, gs.ColorspaceNonStroking, gs.ColorNonStroking, res); pattern != nil {
				ctx.SetFillStyle(pattern)
				if isShading {
					ctx.SetStrokeStyle(pattern)
				}
				break
			}
			rgb, err := gs.ColorspaceNonStroking.ColorToRGB(gs.ColorNonStroking)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorNonStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetFillRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "CS":
			if len(op.Params) > 0 {
				if name, ok := core.GetName(op.Params[0]); ok && name.String() == "Pattern" {
					break
				}
			}
			rgb, err := gs.ColorspaceStroking.ColorToRGB(gs.ColorStroking)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetStrokeRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "SC":
			rgb, err := gs.ColorspaceStroking.ColorToRGB(gs.ColorStroking)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetStrokeRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "SCN":
			if pattern, isShading := r.scnPattern(ctx, op, prevOp, gs.ColorspaceStroking, gs.ColorStroking, res); pattern != nil {
				ctx.SetStrokeStyle(pattern)
				if isShading {
					ctx.SetFillStyle(pattern)
				}
				break
			}
			rgb, err := gs.ColorspaceStroking.ColorToRGB(gs.ColorStroking)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetStrokeRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "sh":
			r.paintShadingOperator(ctx, op, res)
		case "Do":
			if done, err := r.drawXObject(ctx, op, res); done {
				return err
			}
		case "BI":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			inline, ok := op.Params[0].(*contentstream.ContentStreamInlineImage)
			if !ok {
				return nil
			}
			img, err := inline.ToImage(res)
			if err != nil {
				r.noteDecodeLimit(err)
				common.Log.Debug("Rendering result may be incomplete. Image conversion error: %v", err)
				return nil
			}
			goImg, err := img.ToGoImage()
			if err != nil {
				common.Log.Debug("Rendering result may be incomplete. Image conversion error: %v", err)
				return nil
			}
			bounds := goImg.Bounds()
			ctx.Push()
			ctx.Scale(1.0/float64(bounds.Dx()), -1.0/float64(bounds.Dy()))
			ctx.DrawImageAnchored(goImg, 0, 0, 0, 1)
			ctx.Pop()
		case "BT":
			ts.Reset()
		case "ET":
			ts.Reset()
		case "Tr":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			mode, err := core.GetNumberAsFloat(op.Params[0])
			if err != nil {
				return err
			}
			ts.Tr = context.TextRenderingMode(mode)
		case "TL":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			leading, err := core.GetNumberAsFloat(op.Params[0])
			if err != nil {
				return err
			}
			ts.Tl = leading
		case "Tc":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			charSpace, err := core.GetNumberAsFloat(op.Params[0])
			if err != nil {
				return err
			}
			common.Log.Debug("Tc: %v", charSpace)
			ts.Tc = charSpace
		case "Tw":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			wordSpace, err := core.GetNumberAsFloat(op.Params[0])
			if err != nil {
				return err
			}
			common.Log.Debug("Tw: %v", wordSpace)
			ts.Tw = wordSpace
		case "Tz":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			scale, err := core.GetNumberAsFloat(op.Params[0])
			if err != nil {
				return err
			}
			ts.Th = scale
		case "Ts":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			rise, err := core.GetNumberAsFloat(op.Params[0])
			if err != nil {
				return err
			}
			ts.Ts = rise
		case "Td":
			if len(op.Params) != 2 {
				return errRangeCheck
			}
			vals, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				return err
			}
			common.Log.Debug("Td: %v", vals)
			ts.ProcTd(vals[0], vals[1])
		case "TD":
			if len(op.Params) != 2 {
				return errRangeCheck
			}
			vals, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				return err
			}
			common.Log.Debug("TD: %v", vals)
			ts.ProcTD(vals[0], vals[1])
		case "T*":
			ts.ProcTStar()
		case "Tm":
			if len(op.Params) != 6 {
				return errRangeCheck
			}
			vals, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				return err
			}
			common.Log.Debug("Text matrix: %+v", vals)
			ts.ProcTm(vals[0], vals[1], vals[2], vals[3], vals[4], vals[5])
		case "'":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			text, ok := core.GetStringBytes(op.Params[0])
			if !ok {
				return errTypeCheck
			}
			common.Log.Debug("' string: %s", string(text))
			ts.ProcQ(text, ctx)
		case "\"":
			if len(op.Params) != 3 {
				return errRangeCheck
			}
			wordSpacing, err := core.GetNumberAsFloat(op.Params[0])
			if err != nil {
				return err
			}
			charSpacing, err := core.GetNumberAsFloat(op.Params[1])
			if err != nil {
				return err
			}
			text, ok := core.GetStringBytes(op.Params[2])
			if !ok {
				return errTypeCheck
			}
			ts.ProcDQ(text, wordSpacing, charSpacing, ctx)
		case "Tj":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			text, ok := core.GetStringBytes(op.Params[0])
			if !ok {
				return errTypeCheck
			}
			common.Log.Debug("Tj string: `%s`", string(text))
			ts.ProcTj(text, ctx)
		case "TJ":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			arr, ok := core.GetArray(op.Params[0])
			if !ok {
				common.Log.Debug("Type: %T", arr)
				return errTypeCheck
			}
			common.Log.Debug("TJ array: %+v", arr)
			for _, elem := range arr.Elements() {
				switch v := elem.(type) {
				case *core.PdfObjectString:
					if v != nil {
						ts.ProcTj(v.Bytes(), ctx)
					}
				case *core.PdfObjectFloat, *core.PdfObjectInteger:
					adjust, err := core.GetNumberAsFloat(v)
					if err == nil {
						ts.Translate(-adjust*0.001*ts.Tf.Size*ts.Th/100.0, 0)
					}
				}
			}
		case "Tf":
			if err := r.setFontOperator(ctx, ts, finder, op, res); err != nil {
				return err
			}
		case "BMC", "BDC":
			if len(op.Params) == 2 && op.Params[0].String() == "Artifact" {
				props, propsOK := core.GetDict(op.Params[1])
				if propsOK {
					if props.Get("Subtype") != nil && props.Get("Subtype").String() == "Watermark" {
						ts.Tr = context.TextRenderingModeInvisible
					}
				}
			}
		case "EMC":
		default:
			common.Log.Debug("ERROR: unsupported operand: %s", op.Operand)
		}
		prevOp = op
		return nil
	})
	if err := proc.Process(pageRes); err != nil {
		return err
	}
	return r.decodeLimitErr
}
