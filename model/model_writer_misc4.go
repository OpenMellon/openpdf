package model

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	_ "image/gif"
	"io"
	"sort"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

func (w *PdfWriter) updateObjectNumbers() {
	objNumOffset := w.ObjNumOffset
	n := 0
	for _, object := range w.objects {
		count := int64(n + 1 + objNumOffset)
		ok := true
		if w.appendMode {
			if objNumMap, isObjNumMap := w.objNumMap[object]; isObjNumMap {
				count = objNumMap
				ok = false
			}
		}
		switch v := object.(type) {
		case *core.PdfIndirectObject:
			v.ObjectNumber = count
			v.GenerationNumber = 0
		case *core.PdfObjectStream:
			v.ObjectNumber = count
			v.GenerationNumber = 0
		case *core.PdfObjectStreams:
			v.ObjectNumber = count
			v.GenerationNumber = 0
		case *core.PdfObjectReference:
			v.ObjectNumber = count
			v.GenerationNumber = 0
		case *core.PdfObjectDictionary, *core.PdfObjectString:
		default:
			common.Log.Debug("ERROR: Unknown type %T - skipping", v)
			continue
		}
		if ok {
			n++
		}
	}
	fn := func(obj core.PdfObject) int64 {
		switch v := obj.(type) {
		case *core.PdfIndirectObject:
			return v.ObjectNumber
		case *core.PdfObjectStream:
			return v.ObjectNumber
		case *core.PdfObjectStreams:
			return v.ObjectNumber
		case *core.PdfObjectReference:
			return v.ObjectNumber
		}
		return 0
	}
	sort.SliceStable(w.objects, func(n, count int) bool { return fn(w.objects[n]) < fn(w.objects[count]) })
}

func (w *PdfWriter) getPdfVersion() string {
	return fmt.Sprintf("%d.%d", w.version.Major, w.version.Minor)
}

// GetVersion gets the document version.
func (w *PdfWriter) GetVersion() core.Version { return w.version }

// SetSkipObjectCopy controls whether Write skips the defensive deep copy of the
// source object graph.
//
// By default, when a mutating pipeline stage is present (optimizer, encryption,
// PDF/A conversion), the writer deep-copies source objects so those stages don't
// modify the originating PdfReader's objects. That copy is the single largest
// source of memory/time overhead when writing large documents.
//
// When skip is true the copy is bypassed and those stages mutate the source
// objects in place. This significantly reduces memory use and write time, but
// the source objects (and the PdfReader they came from) are consumed: they MUST
// NOT be read or written again after Write. Use this only for one-shot
// load -> (optimize/encrypt) -> write -> discard pipelines.
//
// The copy is still performed regardless of this setting in append mode and when
// orphan pages require it (see canSkipCopy), as those need the copy for
// correctness rather than source protection.
func (w *PdfWriter) SetSkipObjectCopy(skip bool) { w.skipObjectCopy = skip }

func (w *PdfWriter) snapshotObjectNumbers() func() {
	type savedNum struct {
		obj    core.PdfObject
		objNum int64
		genNum int64
	}
	savedNums := make([]savedNum, 0, len(w.objects))
	for _, object := range w.objects {
		switch v := object.(type) {
		case *core.PdfIndirectObject:
			savedNums = append(savedNums, savedNum{v, v.ObjectNumber, v.GenerationNumber})
		case *core.PdfObjectStream:
			savedNums = append(savedNums, savedNum{v, v.ObjectNumber, v.GenerationNumber})
		case *core.PdfObjectStreams:
			savedNums = append(savedNums, savedNum{v, v.ObjectNumber, v.GenerationNumber})
		case *core.PdfObjectReference:
			savedNums = append(savedNums, savedNum{v, v.ObjectNumber, v.GenerationNumber})
		}
	}
	return func() {
		for _, savedNum := range savedNums {
			switch v := savedNum.obj.(type) {
			case *core.PdfIndirectObject:
				v.ObjectNumber, v.GenerationNumber = savedNum.objNum, savedNum.genNum
			case *core.PdfObjectStream:
				v.ObjectNumber, v.GenerationNumber = savedNum.objNum, savedNum.genNum
			case *core.PdfObjectStreams:
				v.ObjectNumber, v.GenerationNumber = savedNum.objNum, savedNum.genNum
			case *core.PdfObjectReference:
				v.ObjectNumber, v.GenerationNumber = savedNum.objNum, savedNum.genNum
			}
		}
	}
}

// GetStandardApplier gets currently used StandardApplier..
func (w *PdfWriter) GetStandardApplier() StandardApplier { return w.standardApplier }

func (w *PdfWriter) writeXRefStreams(n int, count int64) error {
	num := n + 1
	w.crossReferences[num] = crossReference{Type: 1, ObjectNumber: num, Offset: count}
	buffer := bytes.NewBuffer(nil)
	array := core.MakeArray()
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
		array.Append(core.MakeInteger(int64(i)), core.MakeInteger(int64(count-i)))
		for j := i; j < count; j++ {
			xref := w.crossReferences[j]
			switch xref.Type {
			case 0:
				binary.Write(buffer, binary.BigEndian, byte(0))
				binary.Write(buffer, binary.BigEndian, uint32(0))
				binary.Write(buffer, binary.BigEndian, uint16(0xFFFF))
			case 1:
				binary.Write(buffer, binary.BigEndian, byte(1))
				binary.Write(buffer, binary.BigEndian, uint32(xref.Offset))
				binary.Write(buffer, binary.BigEndian, uint16(xref.Generation))
			case 2:
				binary.Write(buffer, binary.BigEndian, byte(2))
				binary.Write(buffer, binary.BigEndian, uint32(xref.ObjectNumber))
				binary.Write(buffer, binary.BigEndian, uint16(xref.Index))
			}
		}
		i = count + 1
	}
	stream, err := core.MakeStream(buffer.Bytes(), core.NewFlateEncoder())
	if err != nil {
		return err
	}
	stream.ObjectNumber = int64(num)
	stream.PdfObjectDictionary.Set("Type", core.MakeName("XRef"))
	stream.PdfObjectDictionary.Set("W", core.MakeArray(core.MakeInteger(1), core.MakeInteger(4), core.MakeInteger(2)))
	stream.PdfObjectDictionary.Set("Index", array)
	stream.PdfObjectDictionary.Set("Size", core.MakeInteger(int64(num)))
	stream.PdfObjectDictionary.Set("Info", w.infoObj)
	stream.PdfObjectDictionary.Set("Root", w.root)
	if w.appendMode && w.prevXrefOffset > 0 {
		stream.PdfObjectDictionary.Set("Prev", core.MakeInteger(w.prevXrefOffset))
	}
	if w.crypter != nil {
		stream.Set("Encrypt", w.encryptObj)
	}
	if w.ids == nil && w.id0 != "" && w.id1 != "" {
		w.ids = core.MakeArray(core.MakeHexString(w.id0), core.MakeHexString(w.id1))
	}
	if w.ids != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Ids: %s", w.ids)
		}
		stream.Set("ID", w.ids)
	}
	return w.writeObject(int(stream.ObjectNumber), stream)
}

// SetVersion sets the PDF version of the output file.
func (w *PdfWriter) SetVersion(majorVersion, minorVersion int) {
	w.version.Major = majorVersion
	w.version.Minor = minorVersion
}

// GetPdfVersion gets the version of the PDF used within this document.
func (w *PdfWriter) GetPdfVersion() string { return w.getPdfVersion() }

func (w *PdfWriter) setWriter(writer io.Writer) {
	w.writePos = w.startOffset
	w.writeErr = nil
	w.bufWriter = bufio.NewWriter(writer)
}
