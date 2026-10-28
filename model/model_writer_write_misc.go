package model

import (
	"bufio"
	"crypto/md5"
	"errors"
	"fmt"
	"hash"
	_ "image/gif"
	"io"
	"os"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

func (w *PdfWriter) writeLinearized(writer io.Writer) error {
	if err := w.prepareLinearizedDocument(); err != nil {
		return err
	}
	if len(w.pageObjs) == 0 {
		return fmt.Errorf("linearization: cannot linearize a document with no pages")
	}
	ok := w.linearizationOptions.UseObjectStreams
	w.adjustXRefAffectedVersion(ok)
	w.updateObjectNumbers()
	layout, err := w.computeLinearizationLayout()
	if err != nil {
		return err
	}
	reserveLinearizationObjectNumbers := w.reserveLinearizationObjectNumbers(layout)
	var objStm *objectStreamsState
	if ok {
		objStm, err = w.setupObjectStreams(layout)
		if err != nil {
			return err
		}
	}
	serializeForLinearization, err := w.serializeForLinearization(layout)
	if err != nil {
		return err
	}
	w.ensureLinearizedDocumentIDs(serializeForLinearization)
	plan, err := planLinearization(w, layout, serializeForLinearization, reserveLinearizationObjectNumbers, objStm)
	if err != nil {
		return err
	}
	w2 := bufio.NewWriter(writer)
	if err := plan.emit(w2); err != nil {
		return err
	}
	return w2.Flush()
}

func (w *PdfWriter) writeOutlines() error {
	if w.outlineTree == nil {
		return nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("OutlineTree: %+v", w.outlineTree)
	}
	obj := w.outlineTree.ToPdfObject()
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Outlines: %+v (%T, p:%p)", obj, obj, obj)
	}
	w.catalog.Set("Outlines", obj)
	err := w.addObjects(obj)
	if err != nil {
		return err
	}
	return nil
}

func (w *PdfWriter) writeTrailer(n int) {
	w.writeString("xref\r\n")
	for i := 0; i <= n; {
		for ; i <= n; i++ {
			xref, ok := w.crossReferences[i]
			if ok && (!w.appendMode || w.appendMode && (xref.Type == 1 && xref.Offset >= w.originalSize || xref.Type == 0)) {
				break
			}
		}
		var count int
		for count = i + 1; count <= n; count++ {
			xref, ok := w.crossReferences[count]
			if ok && (!w.appendMode || w.appendMode && (xref.Type == 1 && xref.Offset > w.originalSize)) {
				continue
			}
			break
		}
		s := fmt.Sprintf("%d %d\r\n", i, count-i)
		w.writeString(s)
		for j := i; j < count; j++ {
			xref := w.crossReferences[j]
			switch xref.Type {
			case 0:
				s = fmt.Sprintf("%.10d %.5d f\r\n", 0, 65535)
				w.writeString(s)
			case 1:
				s = fmt.Sprintf("%.10d %.5d n\r\n", xref.Offset, 0)
				w.writeString(s)
			}
		}
		i = count + 1
	}
	dict := core.MakeDict()
	dict.Set("Info", w.infoObj)
	dict.Set("Root", w.root)
	dict.Set("Size", core.MakeInteger(int64(n+1)))
	if w.appendMode && w.prevXrefOffset > 0 {
		dict.Set("Prev", core.MakeInteger(w.prevXrefOffset))
	}
	if w.crypter != nil {
		dict.Set("Encrypt", w.encryptObj)
	}
	if w.ids == nil && w.id0 != "" && w.id1 != "" {
		w.ids = core.MakeArray(core.MakeHexString(w.id0), core.MakeHexString(w.id1))
	}
	if w.ids != nil {
		dict.Set("ID", w.ids)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Ids: %s", w.ids)
		}
	}
	w.writeString("trailer\n")
	w.writeBytes(dict.Write())
	w.writeString("\n")
}

// WriteLinearized writes a linearized PDF to the given WriteSeeker. This is
// equivalent to SetLinearized(true) followed by Write — the WriteSeeker is
// accepted so a future zero-copy variant can patch in place; v1 buffers
// internally regardless.
//
// Memory: v1 holds the document in memory roughly three times over at peak —
// the live object graph, the per-object serialized bytes
// (serializeForLinearization), and the assembled first/main section buffers
// during plan convergence. Peak RSS is therefore on the order of 3x the
// output file size. This is a known v1 limitation; the io.WriteSeeker
// signature reserves a future streaming/zero-copy path.
func (w *PdfWriter) WriteLinearized(ws io.WriteSeeker) error {
	w.linearized = true
	return w.Write(ws)
}

func (w *PdfWriter) writeObjectsInStreams(set map[core.PdfObject]bool) error {
	for _, object := range w.objects {
		if ok := set[object]; ok {
			continue
		}
		n := int64(0)
		switch v := object.(type) {
		case *core.PdfIndirectObject:
			n = v.ObjectNumber
		case *core.PdfObjectStream:
			n = v.ObjectNumber
		case *core.PdfObjectStreams:
			n = v.ObjectNumber
		case *core.PdfObjectDictionary, *core.PdfObjectString:
		default:
			common.Log.Debug("ERROR: Unsupported type in writer objects: %T (type %T)", object, v)
			return ErrTypeCheck
		}
		_, ok := object.(*core.PdfObjectStreams)
		if w.crypter != nil && object != w.encryptObj && !ok {
			w.encrypted = true
			err := w.crypter.Encrypt(object, n, 0)
			if err != nil {
				common.Log.Debug("ERROR: Failed encrypting (%s)", err)
				return err
			}
		}
		if err := w.writeObject(int(n), object); err != nil {
			return err
		}
	}
	return nil
}

func (w *PdfWriter) writeString(s string) {
	if w.writeErr != nil {
		return
	}
	writeString, err := w.bufWriter.WriteString(s)
	w.writePos += int64(writeString)
	w.writeErr = err
}

func (w *PdfWriter) writeNamesDictionary() error {
	if w.names == nil {
		return nil
	}
	obj := w.names.ToPdfObject()
	w.catalog.Set("Names", obj)
	err := w.addObjects(obj)
	if err != nil {
		return err
	}
	return nil
}

func (w *PdfWriter) writeBytes(data []byte) {
	if w.writeErr != nil {
		return
	}
	n, err := w.bufWriter.Write(data)
	w.writePos += int64(n)
	w.writeErr = err
}

func (w *PdfWriter) writeAcroFormFields() error {
	if w.acroForm == nil {
		return nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Writing acro forms")
	}
	obj := w.acroForm.ToPdfObject()
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("AcroForm: %+v", obj)
	}
	w.catalog.Set("AcroForm", obj)
	err := w.addObjects(obj)
	if err != nil {
		return err
	}
	return nil
}

func (w *PdfWriter) writeOutputIntents() error {
	if len(w.outputIntents) == 0 {
		return nil
	}
	objs := make([]core.PdfObject, len(w.outputIntents))
	for i, outputIntent := range w.outputIntents {
		obj := outputIntent.ToPdfObject()
		objs[i] = core.MakeIndirectObject(obj)
	}
	indirectObject := core.MakeIndirectObject(core.MakeArray(objs...))
	w.catalog.Set("OutputIntents", indirectObject)
	if err := w.addObjects(indirectObject); err != nil {
		return err
	}
	return nil
}

// WriteToFile writes the output PDF to file.
func (w *PdfWriter) WriteToFile(outputFilePath string) error {
	file, err := os.Create(outputFilePath)
	if err != nil {
		return err
	}
	defer file.Close()
	return w.Write(file)
}

// Write writes out the PDF.
func (w *PdfWriter) Write(writer io.Writer) (err error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Write()")
	}
	if file, ok := writer.(*os.File); ok {
		w.SetFileName(file.Name())
	}
	defer func() {
		if v := recover(); v != nil {
			w.finishLazyStreams(fmt.Errorf("write panicked: %v", v))
			panic(v)
		}
		w.finishLazyStreams(err)
	}()
	if w.written && (w.encrypted || (w.copied && w.writeFailed)) {
		return errors.New("cannot write again with the same PdfWriter after encrypting or after a failed write that copied the objects; use a new PdfWriter")
	}
	w.written = true
	if w.linearized {
		return w.writeLinearized(writer)
	}
	if err = w.writeOutlines(); err != nil {
		return err
	}
	if err = w.writeAcroFormFields(); err != nil {
		return err
	}
	if err = w.writeNamesDictionary(); err != nil {
		return err
	}
	w.checkPendingObjects()
	if err = w.writeOutputIntents(); err != nil {
		return err
	}
	w.setCatalogVersion()
	if w.canSkipCopy() {
		if !w.skipObjectCopy {
			defer w.snapshotObjectNumbers()()
		}
	} else {
		w.copyObjects()
	}
	if err = w.optimize(); err != nil {
		return err
	}
	if err = w.optimizeDocument(); err != nil {
		return err
	}
	var digest hash.Hash
	if w.useHashBasedID {
		digest = md5.New()
		writer = io.MultiWriter(digest, writer)
	}
	w.setWriter(writer)
	ok := w.checkCrossReferenceStream()
	mapObjectStreams, ok := w.mapObjectStreams(ok)
	w.adjustXRefAffectedVersion(ok)
	w.writeDocumentVersion()
	w.updateObjectNumbers()
	w.writeObjects()
	if err = w.writeObjectsInStreams(mapObjectStreams); err != nil {
		return err
	}
	writePos := w.writePos
	var n int
	for key := range w.crossReferences {
		if key > n {
			n = key
		}
	}
	if w.useHashBasedID {
		if err = w.setHashIDs(digest); err != nil {
			return err
		}
	}
	if ok {
		if err = w.writeXRefStreams(n, writePos); err != nil {
			return err
		}
	} else {
		w.writeTrailer(n)
	}
	w.makeOffSetReference(writePos)
	if err = w.flushWriter(); err != nil {
		return err
	}
	return nil
}

func (w *PdfWriter) writeObject(n int, obj core.PdfObject) error {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Write obj #%d\n", n)
	}
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		w.crossReferences[n] = crossReference{Type: 1, Offset: w.writePos, Generation: indirect.GenerationNumber}
		s := fmt.Sprintf("%d 0 obj\n", n)
		if signDict, ok := indirect.PdfObject.(*pdfSignDictionary); ok {
			signDict.fileOffset = w.writePos + int64(len(s))
		}
		if indirect.PdfObject == nil {
			common.Log.Debug("Error: indirect object's PdfObject should never be nil - setting to PdfObjectNull")
			indirect.PdfObject = core.MakeNull()
		}
		s += string(indirect.PdfObject.Write())
		s += "\nendobj\n"
		w.writeString(s)
		return nil
	}
	if stream, ok := obj.(*core.PdfObjectStream); ok {
		w.crossReferences[n] = crossReference{Type: 1, Offset: w.writePos, Generation: stream.GenerationNumber}
		w.writeString(fmt.Sprintf("%d 0 obj\n", n))
		w.writeBytes(stream.PdfObjectDictionary.Write())
		w.writeString("\nstream\n")
		if stream.Lazy {
			if err := writeLazyStream(lazyStreamWriter{w}, n, stream, w.crypter); err != nil {
				return err
			}
			w.lazyStreams = append(w.lazyStreams, stream)
		} else {
			w.writeBytes(stream.Stream)
		}
		w.writeString("\nendstream\nendobj\n")
		return nil
	}
	if objStreams, ok := obj.(*core.PdfObjectStreams); ok {
		w.crossReferences[n] = crossReference{Type: 1, Offset: w.writePos, Generation: objStreams.GenerationNumber}
		s := fmt.Sprintf("%d 0 obj\n", n)
		var builder, builder2 strings.Builder
		var count int64
		num := 0
		for _, obj := range objStreams.Elements() {
			indirect, ok := obj.(*core.PdfIndirectObject)
			if !ok {
				common.Log.Debug("ERROR: Object streams N %d contains non indirect pdf object %v", n, obj)
				continue
			}
			if w.excludedFromObjectStream(indirect) {
				continue
			}
			data := append(indirect.PdfObject.Write(), ' ')
			builder2.Write(data)
			fmt.Fprintf(&builder, "%d %d ", indirect.ObjectNumber, count)
			w.crossReferences[int(indirect.ObjectNumber)] = crossReference{Type: 2, ObjectNumber: n, Index: num}
			num++
			count += int64(len(data))
		}
		flateEncoder := core.NewFlateEncoder()
		streamDict := flateEncoder.MakeStreamDict()
		streamDict.Set(core.PdfObjectName("Type"), core.MakeName("ObjStm"))
		streamDict.Set(core.PdfObjectName("N"), core.MakeInteger(int64(num)))
		builderLen := int64(builder.Len())
		streamDict.Set(core.PdfObjectName("First"), core.MakeInteger(builderLen))
		encodeBytes, _ := flateEncoder.EncodeBytes([]byte(builder.String() + builder2.String()))
		if w.crypter != nil {
			stream := &core.PdfObjectStream{PdfObjectDictionary: streamDict, Stream: encodeBytes}
			stream.ObjectNumber = int64(n)
			if err := w.crypter.Encrypt(stream, int64(n), 0); err != nil {
				return fmt.Errorf("failed encrypting object stream %d: %w", n, err)
			}
			encodeBytes = stream.Stream
		}
		size := int64(len(encodeBytes))
		streamDict.Set(core.PdfObjectName("Length"), core.MakeInteger(size))
		s += string(streamDict.Write())
		s += "\nstream\n"
		w.writeString(s)
		w.writeBytes(encodeBytes)
		w.writeString("\nendstream\nendobj\n")
		return nil
	}
	w.writeBytes(obj.Write())
	return nil
}

func (w *PdfWriter) writeObjects() {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Writing %d obj", len(w.objects))
	}
	w.crossReferences = make(map[int]crossReference)
	w.crossReferences[0] = crossReference{Type: 0, ObjectNumber: 0, Generation: 0xFFFF}
	if w.prevXrefs.ObjectMap != nil {
		for n, objectMap := range w.prevXrefs.ObjectMap {
			if n == 0 {
				continue
			}
			if objectMap.XType == core.XrefTypeObjectStream {
				xref := crossReference{Type: 2, ObjectNumber: objectMap.OsObjNumber, Index: objectMap.OsObjIndex}
				w.crossReferences[n] = xref
			}
			if objectMap.XType == core.XrefTypeTableEntry {
				xref := crossReference{Type: 1, ObjectNumber: objectMap.ObjectNumber, Offset: objectMap.Offset}
				w.crossReferences[n] = xref
			}
		}
	}
}

func (w *PdfWriter) writeDocumentVersion() {
	if w.appendMode {
		w.writeString("\n")
	} else {
		w.writeString(fmt.Sprintf("%%PDF-%d.%d\n", w.version.Major, w.version.Minor))
		w.writeString("%âãÏÓ\n")
	}
}
