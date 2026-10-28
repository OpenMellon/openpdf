package core

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/OpenMellon/openpdf/common"
)

// Elements returns a slice of the PdfObject elements in the array.
// Preferred over accessing the array directly as type may be changed in future major versions (v3).
func (streams *PdfObjectStreams) Elements() []PdfObject {
	if streams == nil {
		return nil
	}
	return streams.vec
}

// Write outputs the object as it is to be written to file.
func (streams *PdfObjectStreams) Write() []byte {
	return withPooledBuffer(func(buf *bytes.Buffer) {
		buf.WriteString(strconv.FormatInt(streams.ObjectNumber, 10))
		buf.WriteString(" 0 R")
	})
}

type (
	indirectPair struct{ first, second *PdfIndirectObject }
	objectStream struct {
		N       int
		data    []byte
		offsets map[int]int64
	}
)

// String returns a string describing `stream`.
func (stream *PdfObjectStream) String() string {
	return fmt.Sprintf("Object stream %d: %s", stream.ObjectNumber, stream.PdfObjectDictionary)
}

type objectStreams map[int]objectStream

// MakeStream creates an PdfObjectStream with specified contents and encoding. If encoding is nil, then raw encoding
// will be used (i.e. no encoding applied).
func MakeStream(contents []byte, encoder StreamEncoder) (*PdfObjectStream, error) {
	stream := &PdfObjectStream{}
	if encoder == nil {
		encoder = NewRawEncoder()
	}
	stream.PdfObjectDictionary = encoder.MakeStreamDict()
	bytes, err := encoder.EncodeBytes(contents)
	if err != nil {
		return nil, err
	}
	stream.Set("Length", MakeInteger(int64(len(bytes))))
	stream.Stream = bytes
	return stream, nil
}

// Equals reports whether other has the same contents as this object (deep comparison).
func (stream *PdfObjectStream) Equals(other PdfObject) bool {
	if stream == nil {
		return other == nil
	}
	return equalObjects(stream, other, 0, nil)
}
func (stream *PdfObjectStream) Direct() PdfObject { return stream }

// PdfObjectStreams represents the primitive PDF object streams.
// 7.5.7 Object Streams (page 45).
type PdfObjectStreams struct {
	PdfObjectReference
	vec []PdfObject
}

// PdfObjectStream represents the primitive PDF Object stream.
type PdfObjectStream struct {
	PdfObjectReference
	*PdfObjectDictionary
	Stream []byte

	// A lazy stream keeps its data in TempFile and leaves Stream nil.
	Lazy     bool
	TempFile string
}

// GetObjectStreams returns the *PdfObjectStreams represented by the PdfObject. On type mismatch the found bool flag is
// false and a nil pointer is returned.
func GetObjectStreams(obj PdfObject) (streams *PdfObjectStreams, ok bool) {
	streams, ok = obj.(*PdfObjectStreams)
	return streams, ok
}

// Equals reports whether other has the same contents as this object (deep comparison).
func (streams *PdfObjectStreams) Equals(other PdfObject) bool {
	if streams == nil {
		return other == nil
	}
	return equalObjects(streams, other, 0, nil)
}

// Len returns the number of elements in the streams.
func (streams *PdfObjectStreams) Len() int {
	if streams == nil {
		return 0
	}
	return len(streams.vec)
}

// String returns a string describing `streams`.
func (streams *PdfObjectStreams) String() string {
	return fmt.Sprintf("Object stream %d", streams.ObjectNumber)
}

// Append appends PdfObject(s) to the streams.
func (streams *PdfObjectStreams) Append(objects ...PdfObject) {
	if streams == nil {
		common.Log.Debug("Warn - Attempt to append to a nil streams")
		return
	}
	streams.vec = append(streams.vec, objects...)
}

// Set sets the PdfObject at index i of the streams. An error is returned if the index is outside bounds.
func (streams *PdfObjectStreams) Set(i int, obj PdfObject) error {
	if i < 0 || i >= len(streams.vec) {
		return errors.New("outside bounds")
	}
	streams.vec[i] = obj
	return nil
}

// MakeObjectStreams creates an PdfObjectStreams from a list of PdfObjects.
func MakeObjectStreams(objects ...PdfObject) *PdfObjectStreams {
	return &PdfObjectStreams{vec: objects}
}
func (streams *PdfObjectStreams) Direct() PdfObject { return streams }

// MakeLazy moves the stream data into a temporary file to reduce memory usage,
// e.g. when creating a PDF with many images.
//
// A lazy stream is single-use: the temporary file is removed once a Write
// that serializes it succeeds, after which writing the stream again returns
// an error. A failed write leaves the stream intact so the document can be
// written again, with the same PdfWriter unless that write copied the
// objects (optimizer, append mode, linearization) or encrypted them, in
// which case the writer requires a new PdfWriter. When writing encrypted,
// the ciphertext is held in memory until the stream is written.
func (stream *PdfObjectStream) MakeLazy() error {
	if stream.Lazy {
		return nil
	}
	temp, err := os.CreateTemp("", "xobject")
	if err != nil {
		return err
	}
	defer temp.Close()
	_, err = temp.Write(stream.Stream)
	if err != nil {
		return err
	}
	stream.Lazy = true
	stream.Stream = nil
	stream.TempFile = temp.Name()
	return nil
}

// Write outputs the object as it is to be written to file.
func (stream *PdfObjectStream) Write() []byte {
	return withPooledBuffer(func(buf *bytes.Buffer) {
		buf.WriteString(strconv.FormatInt(stream.ObjectNumber, 10))
		buf.WriteString(" 0 R")
	})
}
