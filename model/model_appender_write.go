package model

import (
	"bytes"
	"errors"
	_ "image/gif"
	"io"
	"os"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// WriteToFile writes the Appender output to file specified by path.
func (a *PdfAppender) WriteToFile(outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()
	return a.Write(file)
}

// Write writes the Appender output to io.Writer.
// It can only be called once and further invocations will result in an error.
func (a *PdfAppender) Write(w io.Writer) error {
	if a.written {
		return errors.New("appender write can only be invoked once")
	}
	writer := NewPdfWriter()
	pagesDict, ok := core.GetDict(writer.pages)
	if !ok {
		return errors.New("invalid Pages obj (not a dict)")
	}
	arr, ok := pagesDict.Get("Kids").(*core.PdfObjectArray)
	if !ok {
		return errors.New("invalid Pages Kids obj (not an array)")
	}
	num, ok := pagesDict.Get("Count").(*core.PdfObjectInteger)
	if !ok {
		return errors.New("invalid Pages Count object (not an integer)")
	}
	parser := a.roReader.parser
	trailer := parser.GetTrailer()
	if trailer == nil {
		return errors.New("missing trailer")
	}
	root, ok := core.GetIndirect(trailer.Get("Root"))
	if !ok {
		return errors.New("catalog container not found")
	}
	rootDict, ok := core.GetDict(root)
	if !ok {
		common.Log.Debug("ERROR: Missing catalog: (root %q) (trailer %s)", root, trailer)
		return errors.New("missing catalog")
	}
	found := false
	for _, signatureField := range a.roReader.AcroForm.signatureFields() {
		if signatureField.Lock != nil {
			found = true
			break
		}
	}
	if found {
		writer.root = root
	}
	for _, name := range rootDict.Keys() {
		if writer.catalog.Get(name) == nil {
			obj := rootDict.Get(name)
			writer.catalog.Set(name, obj)
		}
	}
	if a.acroForm != nil {
		if a.acroForm.direct {
			if obj := a.acroForm.ToPdfObject().Direct(); !core.IsNullObject(obj) {
				writer.catalog.Set("AcroForm", obj)
				a.updateObjectsDeep(obj, nil)
			} else {
				common.Log.Debug("Unable to trace AcroForm object, failed to add AcroForm.")
			}
		} else {
			writer.catalog.Set("AcroForm", a.acroForm.ToPdfObject())
			a.updateObjectsDeep(a.acroForm.ToPdfObject(), nil)
		}
	}
	if a.dss != nil {
		a.updateObjectsDeep(a.dss.ToPdfObject(), nil)
		writer.catalog.Set("DSS", a.dss.GetContainingPdfObject())
	}
	if a.perms != nil {
		writer.catalog.Set("Perms", a.perms.ToPdfObject())
		a.updateObjectsDeep(a.perms.ToPdfObject(), nil)
	}
	if writer.version.Major < 2 {
		writer.AddExtension("ESIC", "1.7", 5)
		writer.AddExtension("ADBE", "1.7", 8)
	}
	if info, ok := core.GetDict(trailer.Get("Info")); ok {
		if infoObjDict, ok := core.GetDict(writer.infoObj); ok {
			for _, name := range info.Keys() {
				if infoObjDict.Get(name) == nil {
					infoObjDict.Set(name, info.Get(name))
				}
			}
		}
	}
	if a.docInfo != nil {
		writer.infoObj = core.MakeIndirectObject(a.docInfo.ToPdfObject())
	}
	a.updateObjectsDeep(writer.infoObj, nil)
	a.updateObjectsDeep(writer.root, nil)
	done := false
	if len(a.roReader.PageList) != len(a.pages) {
		done = true
	} else {
		for i := range a.roReader.PageList {
			switch a.pages[i] {
			case a.roReader.PageList[i]:
			case a.Reader.PageList[i]:
			default:
				done = true
			}
			if done {
				break
			}
		}
	}
	if done {
		a.updateObjectsDeep(writer.pages, nil)
	} else {
		a.skipObjects[writer.pages] = struct{}{}
	}
	writer.pages.ObjectNumber = a.Reader.pagesObj.ObjectNumber
	a.objNumMap[writer.pages] = a.Reader.pagesObj.ObjectNumber
	names := []core.PdfObjectName{"Resources", "MediaBox", "CropBox", "Rotate"}
	for _, page := range a.pages {
		obj := page.ToPdfObject()
		*num = *num + 1
		if indirect, ok := obj.(*core.PdfIndirectObject); ok && indirect.GetParser() == a.roReader.parser {
			arr.Append(&indirect.PdfObjectReference)
			continue
		}
		if dict, isDict := core.GetDict(obj); isDict {
			indirect, isIndirect := dict.Get("Parent").(*core.PdfIndirectObject)
			for isIndirect {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Page Parent: %T", indirect)
				}
				dict2, isDict := indirect.PdfObject.(*core.PdfObjectDictionary)
				if !isDict {
					return errors.New("invalid Parent object")
				}
				for _, name := range names {
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("Field %s", name)
					}
					if obj := dict.Get(name); obj != nil {
						if common.Log.IsLogLevel(common.LogLevelTrace) {
							common.Log.Trace("- page has already")
						}
						if len(page.originalDict.Keys()) > 0 && !done {
							originalDict := page.originalDict
							if obj2 := originalDict.Get(name); obj2 != nil {
								if obj != obj2 {
									if common.Log.IsLogLevel(common.LogLevelTrace) {
										common.Log.Trace("Inheriting original field %s, %T", name, obj2)
									}
									dict.Set(name, obj2)
								}
							}
						}
						continue
					}
					if obj := dict2.Get(name); obj != nil {
						if common.Log.IsLogLevel(common.LogLevelTrace) {
							common.Log.Trace("Inheriting field %s", name)
						}
						dict.Set(name, obj)
					}
				}
				indirect, isIndirect = dict2.Get("Parent").(*core.PdfIndirectObject)
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Next parent: %T", dict2.Get("Parent"))
				}
			}
			if done {
				dict.Set("Parent", writer.pages)
			}
		}
		a.updateObjectsDeep(obj, nil)
		arr.Append(obj)
	}
	if _, err := a.readSeeker.Seek(0, io.SeekStart); err != nil {
		return err
	}
	writerBySignatureHandler := make(map[SignatureHandler]io.Writer)
	array := core.MakeArray()
	for _, newObject := range a.newObjects {
		if newObjectIndirect, ok := core.GetIndirect(newObject); ok {
			if signDict, ok := newObjectIndirect.PdfObject.(*pdfSignDictionary); ok {
				handler := *signDict.handler
				var err error
				writerBySignatureHandler[handler], err = handler.NewDigest(signDict.signature)
				if err != nil {
					return err
				}
				array.Append(core.MakeInteger(0xfffff), core.MakeInteger(0xfffff))
			}
		}
	}
	if array.Len() > 0 {
		array.Append(core.MakeInteger(0xfffff), core.MakeInteger(0xfffff))
	}
	for _, newObject := range a.newObjects {
		if newObjectIndirect, ok := core.GetIndirect(newObject); ok {
			if signDict, ok := newObjectIndirect.PdfObject.(*pdfSignDictionary); ok {
				signDict.Set("ByteRange", array)
			}
		}
	}
	flag := len(writerBySignatureHandler) > 0
	var readSeeker io.Reader = a.readSeeker
	if flag {
		ws := make([]io.Writer, 0, len(writerBySignatureHandler))
		for _, w := range writerBySignatureHandler {
			ws = append(ws, w)
		}
		readSeeker = io.TeeReader(a.readSeeker, io.MultiWriter(ws...))
	}
	n, err := io.Copy(w, readSeeker)
	if err != nil {
		return err
	}
	if len(a.newObjects) == 0 {
		return nil
	}
	if flag && a.roReader.parser.IsLinearized() {
		if n := a.roReader.parser.LinearizationDictObjNum(); n > 0 && !a.hasPriorSignature() {
			indirectObject := core.MakeIndirectObject(core.MakeDict())
			indirectObject.ObjectNumber = int64(n)
			a.objNumMap[indirectObject] = int64(n)
			a.addNewObject(indirectObject)
		}
	}
	writer.startOffset = n
	writer.ObjNumOffset = a.greatestObjNum
	writer.appendMode = true
	writer.prevXrefs = a.xrefs
	writer.prevXrefOffset = a.xrefOffset
	writer.originalSize = a.originalSize
	writer.version = a.roReader.PdfVersion()
	writer.objNumMap = a.objNumMap
	writer.crypter = a.parser.GetCrypter()
	writer.encryptObj = a.parser.GetEncryptObj()
	xrefType := a.parser.GetXrefType()
	if xrefType != nil {
		ok := *xrefType == core.XrefTypeObjectStream
		writer.useObjectStreams = &ok
	}
	writer.hasObjects = map[core.PdfObject]struct{}{}
	writer.objects = []core.PdfObject{}
	for _, newObject := range a.newObjects {
		if _, ok := a.skipObjects[newObject]; ok {
			continue
		}
		writer.addObject(newObject)
	}
	w2 := w
	if flag {
		w2 = bytes.NewBuffer(nil)
	}
	if a.password != "" && writer.crypter == nil {
		if err := writer.Encrypt([]byte(a.password), []byte(a.password), a.encryptOptions); err != nil {
			return err
		}
	}
	if id := trailer.Get("ID"); id != nil {
		if idArr, ok := core.GetArray(id); ok {
			writer.ids = idArr
		}
	}
	if err := writer.Write(w2); err != nil {
		return err
	}
	if flag {
		data := w2.(*bytes.Buffer).Bytes()
		array := core.MakeArray()
		var signDicts []*pdfSignDictionary
		var count int64
		for _, object := range writer.objects {
			if indirect, ok := core.GetIndirect(object); ok {
				if signDict, ok := indirect.PdfObject.(*pdfSignDictionary); ok {
					signDicts = append(signDicts, signDict)
					num := signDict.fileOffset + int64(signDict.contentsOffsetStart)
					array.Append(core.MakeInteger(count), core.MakeInteger(num-count))
					count = signDict.fileOffset + int64(signDict.contentsOffsetEnd)
				}
			}
		}
		array.Append(core.MakeInteger(count), core.MakeInteger(n+int64(len(data))-count))
		data2 := array.Write()
		for _, signDict := range signDicts {
			count := int(signDict.fileOffset - n)
			for byteRangeOffsetStart := signDict.byteRangeOffsetStart; byteRangeOffsetStart < signDict.byteRangeOffsetEnd; byteRangeOffsetStart++ {
				data[count+byteRangeOffsetStart] = ' '
			}
			data3 := data[count+signDict.byteRangeOffsetStart : count+signDict.byteRangeOffsetEnd]
			copy(data3, data2)
		}
		var num int
		for _, signDict := range signDicts {
			size := int(signDict.fileOffset - n)
			data2 := data[num : size+signDict.contentsOffsetStart]
			handler := *signDict.handler
			writerBySignatureHandler[handler].Write(data2)
			num = size + signDict.contentsOffsetEnd
		}
		for _, signDict := range signDicts {
			data2 := data[num:]
			handler := *signDict.handler
			writerBySignatureHandler[handler].Write(data2)
		}
		for _, signDict := range signDicts {
			count := int(signDict.fileOffset - n)
			handler := *signDict.handler
			w := writerBySignatureHandler[handler]
			if err := handler.Sign(signDict.signature, w); err != nil {
				return err
			}
			signDict.signature.ByteRange = array
			data3 := signDict.signature.Contents.Write()
			for byteRangeOffsetStart := signDict.byteRangeOffsetStart; byteRangeOffsetStart < signDict.byteRangeOffsetEnd; byteRangeOffsetStart++ {
				data[count+byteRangeOffsetStart] = ' '
			}
			for contentsOffsetStart := signDict.contentsOffsetStart; contentsOffsetStart < signDict.contentsOffsetEnd; contentsOffsetStart++ {
				data[count+contentsOffsetStart] = ' '
			}
			data4 := data[count+signDict.byteRangeOffsetStart : count+signDict.byteRangeOffsetEnd]
			copy(data4, data2)
			data4 = data[count+signDict.contentsOffsetStart : count+signDict.contentsOffsetEnd]
			copy(data4, data3)
		}
		buffer := bytes.NewBuffer(data)
		_, err = io.Copy(w, buffer)
		if err != nil {
			return err
		}
	}
	a.written = true
	return nil
}
