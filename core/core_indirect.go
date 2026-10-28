package core

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
)

func indirectObjectIDs(obj PdfObject) (int64, int64, error) {
	if indirect, ok := obj.(*PdfIndirectObject); ok {
		return indirect.ObjectNumber, indirect.GenerationNumber, nil
	}
	if stream, ok := obj.(*PdfObjectStream); ok {
		return stream.ObjectNumber, stream.GenerationNumber, nil
	}
	return 0, 0, errors.New("not an indirect/stream object")
}

// PdfIndirectObject represents the primitive PDF indirect object.
type PdfIndirectObject struct {
	PdfObjectReference
	PdfObject
}

// Direct traces the indirect object to the direct object it contains.
func (indirectObject *PdfIndirectObject) Direct() PdfObject {
	return TraceToDirectObject(indirectObject)
}

// Equals reports whether other has the same contents as this object (deep comparison).
func (ind *PdfIndirectObject) Equals(other PdfObject) bool {
	if ind == nil {
		return other == nil
	}
	return equalObjects(ind, other, 0, nil)
}

// Write outputs the object as it is to be written to file.
func (ind *PdfIndirectObject) Write() []byte {
	return withPooledBuffer(func(buf *bytes.Buffer) {
		buf.WriteString(strconv.FormatInt(ind.ObjectNumber, 10))
		buf.WriteString(" 0 R")
	})
}

// String returns a string describing `ind`.
func (ind *PdfIndirectObject) String() string {
	return fmt.Sprintf("IObject:%d", (*ind).ObjectNumber)
}

type (
	indirectPair struct{ first, second *PdfIndirectObject }
	objectStream struct {
		N       int
		data    []byte
		offsets map[int]int64
	}
)
