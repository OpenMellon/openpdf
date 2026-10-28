// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitwise

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/OpenMellon/openpdf/common"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

func (r *Reader) Read(p []byte) (n int, err error) {
	if r.remainingBits == 0 {
		return r.read(p)
	}
	for ; n < len(p); n++ {
		if p[n], err = r.readUnalignedByte(); err != nil {
			return 0, err
		}
	}
	return n, nil
}

func (w *BufferedWriter) Reset() {
	w.buffer = w.buffer[:0]
	w.byteOffset = 0
	w.bitOffset = 0
}

type BitWriter interface {
	WriteBit(bit int) error
	WriteBits(bits uint64, number int) (written int, err error)
	FinishByte()
	SkipBits(skip int) error
}

func (r *Reader) Reset() {
	r.bytePosition = r.markedBytePosition
	r.remainingBits = r.markedRemainingBits
	r.bufferedBits = r.markedBufferedBits
	r.lastByte = r.markedLastByte
}

func (w *BufferedWriter) Write(d []byte) (int, error) {
	w.expandIfNeeded(len(d))
	if w.bitOffset == 0 {
		return w.writeFullBytes(d), nil
	}
	return w.writeShiftedBytes(d), nil
}

func (r *Reader) ReadBit() (bit int, err error) {
	set, err := r.readBool()
	if err != nil {
		return 0, err
	}
	if set {
		bit = 1
	}
	return bit, nil
}

type BinaryWriter interface {
	BitWriter
	io.Writer
	io.ByteWriter
	Data() []byte
}

func (w *Writer) WriteByte(c byte) error { return w.writeByte(c) }

var _ io.ByteWriter = &BufferedWriter{}

func (r *Reader) readUnalignedByte() (value byte, err error) {
	remaining := r.remainingBits
	value = r.bufferedBits << (8 - remaining)
	r.bufferedBits, err = r.readBufferByte()
	if err != nil {
		return 0, err
	}
	value |= r.bufferedBits >> remaining
	r.bufferedBits &= 1<<remaining - 1
	return value, nil
}

type StreamReader interface {
	io.Reader
	io.ByteReader
	io.Seeker
	Align() byte
	BitPosition() int
	Mark()
	Length() uint64
	ReadBit() (int, error)
	ReadBits(n byte) (uint64, error)
	ReadBool() (bool, error)
	ReadUint32() (uint32, error)
	Reset()
	AbsolutePosition() int64
}

func (w *BufferedWriter) writeByte(value byte) {
	switch {
	case w.bitOffset == 0:
		w.buffer[w.byteOffset] = value
		w.byteOffset++
	case w.msbFirst:
		w.buffer[w.byteOffset] |= value >> w.bitOffset
		w.byteOffset++
		w.buffer[w.byteOffset] = byte(uint16(value) << (8 - w.bitOffset) & 0xff)
	default:
		w.buffer[w.byteOffset] |= byte(uint16(value) << w.bitOffset & 0xff)
		w.byteOffset++
		w.buffer[w.byteOffset] = value >> (8 - w.bitOffset)
	}
}

type BufferedWriter struct {
	buffer     []byte
	bitOffset  uint8
	byteOffset int
	msbFirst   bool
}

func (w *BufferedWriter) tryGrowByReslice(extra int) bool {
	if length := len(w.buffer); extra <= cap(w.buffer)-length {
		w.buffer = w.buffer[:length+extra]
		return true
	}
	return false
}

func (w *BufferedWriter) WriteBit(bit int) error {
	if bit != 1 && bit != 0 {
		return jbig2errors.Errorf("BufferedWriter.WriteBit", "bit value must be in range {0,1} but is: %d", bit)
	}
	if len(w.buffer)-1 < w.byteOffset {
		w.expandIfNeeded(1)
	}
	shift := w.bitOffset
	if w.msbFirst {
		shift = 7 - w.bitOffset
	}
	w.buffer[w.byteOffset] |= byte(uint16(bit<<shift) & 0xff)
	w.bitOffset++
	if w.bitOffset == 8 {
		w.byteOffset++
		w.bitOffset = 0
	}
	return nil
}

func (w *BufferedWriter) FinishByte() {
	if w.bitOffset == 0 {
		return
	}
	w.bitOffset = 0
	w.byteOffset++
}
func (w *Writer) Data() []byte { return w.buffer }
func (w *Writer) Write(p []byte) (int, error) {
	if len(p) > w.byteCapacity() {
		return 0, io.EOF
	}
	for _, value := range p {
		if err := w.writeByte(value); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (w *BufferedWriter) writeShiftedBytes(data []byte) int {
	for _, value := range data {
		w.writeByte(value)
	}
	return len(data)
}
func NewWriterMSB(data []byte) *Writer    { return &Writer{buffer: data, msbFirst: true} }
func (w *Writer) ResetBit()               { w.bitOffset = 0 }
func (w *BufferedWriter) ResetBitIndex()  { w.bitOffset = 0 }
func (r *Reader) AbsolutePosition() int64 { return r.bytePosition + int64(r.source.offset) }
func NewReader(data []byte) *Reader {
	return &Reader{source: readerSource{data: data, length: len(data), offset: 0}}
}

var _ BinaryWriter = &BufferedWriter{}

func (w *BufferedWriter) expandIfNeeded(extra int) {
	if !w.tryGrowByReslice(extra) {
		w.grow(extra)
	}
}

func (r *Reader) ReadByte() (byte, error) {
	if r.remainingBits == 0 {
		return r.readBufferByte()
	}
	return r.readUnalignedByte()
}

func (r *Reader) NewPartialReader(offset, length int, relative bool) (*Reader, error) {
	if offset < 0 {
		return nil, errors.New("partial reader offset cannot be negative")
	}
	if relative {
		offset = r.source.offset + offset
	}
	if length > 0 {
		limit := len(r.source.data)
		if relative {
			limit = r.source.length
		}
		if offset+length > limit {
			return nil, fmt.Errorf("partial reader offset(%d)+length(%d)=%d is greater than the original reader length: %d", offset, length, offset+length, r.source.length)
		}
	}
	if length < 0 {
		limit := len(r.source.data)
		if relative {
			limit = r.source.length
		}
		length = limit - offset
	}
	return &Reader{source: readerSource{data: r.source.data, length: length, offset: offset}}, nil
}
func (w *BufferedWriter) Data() []byte { return w.buffer }
func (r *Reader) readBufferByte() (byte, error) {
	if r.bytePosition >= int64(r.source.length) {
		return 0, io.EOF
	}
	r.unusedReadState = -1
	value := r.source.data[int64(r.source.offset)+r.bytePosition]
	r.bytePosition++
	r.lastByte = int(value)
	return value, nil
}

func (r *Reader) Align() (skipped byte) {
	skipped = r.remainingBits
	r.remainingBits = 0
	return skipped
}

func (w *Writer) WriteBit(bit int) error {
	switch bit {
	case 0, 1:
		return w.writeBit(uint8(bit))
	}
	return jbig2errors.Error("WriteBit", "invalid bit value")
}

type Writer struct {
	buffer     []byte
	bitOffset  uint8
	byteOffset int
	msbFirst   bool
}

func (w *Writer) SkipBits(skip int) error {
	const operation = "Writer.SkipBits"
	if skip == 0 {
		return nil
	}
	position := int(w.bitOffset) + skip
	if position >= 0 && position < 8 {
		w.bitOffset = uint8(position)
		return nil
	}
	position = int(w.bitOffset) + w.byteOffset*8 + skip
	if position < 0 {
		return jbig2errors.Errorf(operation, "index out of range")
	}
	byteIndex := position / 8
	bitIndex := position % 8
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("SkipBits")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("BitIndex: '%d' ByteIndex: '%d', FullBits: '%d', Len: '%d', Cap: '%d'", w.bitOffset, w.byteOffset, int(w.bitOffset)+w.byteOffset*8, len(w.buffer), cap(w.buffer))
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Skip: '%d', d: '%d', bitIndex: '%d'", skip, position, bitIndex)
	}
	w.bitOffset = uint8(bitIndex)
	if byteDifference := byteIndex - w.byteOffset; byteDifference > 0 && len(w.buffer)-1 < byteIndex {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("ByteDiff: %d", byteDifference)
		}
		return jbig2errors.Errorf(operation, "index out of range")
	}
	w.byteOffset = byteIndex
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("BitIndex: '%d', ByteIndex: '%d'", w.bitOffset, w.byteOffset)
	}
	return nil
}
func (r *Reader) Length() uint64 { return uint64(r.source.length) }

var _ BinaryWriter = &Writer{}

func (w *BufferedWriter) byteCapacity() int {
	capacity := len(w.buffer) - w.byteOffset
	if w.bitOffset != 0 {
		capacity--
	}
	return capacity
}

const (
	initialBufferCapacity = 64
	maxInt                = int(^uint(0) >> 1)
)

type Reader struct {
	source              readerSource
	bufferedBits        byte
	remainingBits       byte
	bytePosition        int64
	lastByte            int
	unusedReadState     int
	markedBytePosition  int64
	markedRemainingBits byte
	markedBufferedBits  byte
	markedLastByte      int
}

func (r *Reader) ConsumeRemainingBits() (uint64, error) {
	if r.remainingBits != 0 {
		return r.ReadBits(r.remainingBits)
	}
	return 0, nil
}

func (w *BufferedWriter) WriteByte(bt byte) error {
	if w.byteOffset > len(w.buffer)-1 || (w.byteOffset == len(w.buffer)-1 && w.bitOffset != 0) {
		w.expandIfNeeded(1)
	}
	w.writeByte(bt)
	return nil
}

func (r *Reader) readBool() (bit bool, err error) {
	if r.remainingBits == 0 {
		r.bufferedBits, err = r.readBufferByte()
		if err != nil {
			return false, err
		}
		bit = (r.bufferedBits & 0x80) != 0
		r.bufferedBits, r.remainingBits = r.bufferedBits&0x7f, 7
		return bit, nil
	}
	r.remainingBits--
	bit = (r.bufferedBits & (1 << r.remainingBits)) != 0
	r.bufferedBits &= 1<<r.remainingBits - 1
	return bit, nil
}
func (r *Reader) ReadBool() (bool, error) { return r.readBool() }
func (w *BufferedWriter) Len() int        { return w.byteCapacity() }
func NewWriter(data []byte) *Writer       { return &Writer{buffer: data} }
func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	r.unusedReadState = -1
	r.remainingBits = 0
	r.bufferedBits = 0
	r.lastByte = 0
	var position int64
	switch whence {
	case io.SeekStart:
		position = offset
	case io.SeekCurrent:
		position = r.bytePosition + offset
	case io.SeekEnd:
		position = int64(r.source.length) + offset
	default:
		return 0, errors.New("reader.Reader.Seek: invalid whence")
	}
	if position < 0 {
		return 0, errors.New("reader.Reader.Seek: negative position")
	}
	r.bytePosition = position
	r.remainingBits = 0
	return position, nil
}
func (r *Reader) RelativePosition() int64 { return r.bytePosition }
func BufferedMSB() *BufferedWriter        { return &BufferedWriter{msbFirst: true} }
func (w *Writer) writeByte(value byte) error {
	if w.byteOffset > len(w.buffer)-1 {
		return io.EOF
	}
	if w.byteOffset == len(w.buffer)-1 && w.bitOffset != 0 {
		return io.EOF
	}
	if w.bitOffset == 0 {
		w.buffer[w.byteOffset] = value
		w.byteOffset++
		return nil
	}
	if w.msbFirst {
		w.buffer[w.byteOffset] |= value >> w.bitOffset
		w.byteOffset++
		w.buffer[w.byteOffset] = byte(uint16(value) << (8 - w.bitOffset) & 0xff)
	} else {
		w.buffer[w.byteOffset] |= byte(uint16(value) << w.bitOffset & 0xff)
		w.byteOffset++
		w.buffer[w.byteOffset] = value >> (8 - w.bitOffset)
	}
	return nil
}

func (w *BufferedWriter) WriteBits(bits uint64, number int) (written int, err error) {
	const operation = "BufferedWriter.WriterBits"
	if number < 0 || number > 64 {
		return 0, jbig2errors.Errorf(operation, "bits number must be in range <0,64>, is: '%d'", number)
	}
	fullBytes := number / 8
	if fullBytes > 0 {
		remainingBits := number - fullBytes*8
		for i := fullBytes - 1; i >= 0; i-- {
			value := byte((bits >> uint(i*8+remainingBits)) & 0xff)
			if err = w.WriteByte(value); err != nil {
				return written, jbig2errors.Wrapf(err, operation, "byte: '%d'", fullBytes-i+1)
			}
		}
		number -= fullBytes * 8
		if number == 0 {
			return fullBytes, nil
		}
	}
	var bit int
	for i := 0; i < number; i++ {
		if w.msbFirst {
			bit = int((bits >> uint(number-1-i)) & 0x1)
		} else {
			bit = int(bits & 0x1)
			bits >>= 1
		}
		if err = w.WriteBit(bit); err != nil {
			return written, jbig2errors.Wrapf(err, operation, "bit: %d", i)
		}
	}
	return fullBytes, nil
}

var (
	_ io.Reader     = &Reader{}
	_ io.ByteReader = &Reader{}
	_ io.Seeker     = &Reader{}
	_ StreamReader  = &Reader{}
)

func (w *Writer) writeBit(bit uint8) error {
	if len(w.buffer)-1 < w.byteOffset {
		return io.EOF
	}
	shift := w.bitOffset
	if w.msbFirst {
		shift = 7 - w.bitOffset
	}
	w.buffer[w.byteOffset] |= byte(uint16(bit<<shift) & 0xff)
	w.bitOffset++
	if w.bitOffset == 8 {
		w.byteOffset++
		w.bitOffset = 0
	}
	return nil
}

type readerSource struct {
	data   []byte
	offset int
	length int
}

func (w *BufferedWriter) SkipBits(skip int) error {
	if skip == 0 {
		return nil
	}
	position := int(w.bitOffset) + skip
	if position >= 0 && position < 8 {
		w.bitOffset = uint8(position)
		return nil
	}
	position = int(w.bitOffset) + w.byteOffset*8 + skip
	if position < 0 {
		return jbig2errors.Errorf("Writer.SkipBits", "index out of range")
	}
	byteIndex := position / 8
	bitIndex := position % 8
	w.bitOffset = uint8(bitIndex)
	if additionalBytes := byteIndex - w.byteOffset; additionalBytes > 0 && len(w.buffer)-1 < byteIndex {
		if w.bitOffset != 0 {
			additionalBytes++
		}
		w.expandIfNeeded(additionalBytes)
	}
	w.byteOffset = byteIndex
	return nil
}

func (r *Reader) read(p []byte) (int, error) {
	if r.bytePosition >= int64(r.source.length) {
		return 0, io.EOF
	}
	r.unusedReadState = -1
	n := copy(p, r.source.data[(int64(r.source.offset)+r.bytePosition):(r.source.offset+r.source.length)])
	r.bytePosition += int64(n)
	return n, nil
}

func (w *BufferedWriter) grow(extra int) {
	if w.buffer == nil && extra < initialBufferCapacity {
		w.buffer = make([]byte, extra, initialBufferCapacity)
		return
	}
	length := len(w.buffer)
	if w.bitOffset != 0 {
		length++
	}
	capacity := cap(w.buffer)
	switch {
	case extra <= capacity/2-length:
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("[BufferedWriter] grow - reslice only. Len: '%d', Cap: '%d', n: '%d'", len(w.buffer), cap(w.buffer), extra)
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace(" n <= c / 2 -m. C: '%d', m: '%d'", capacity, length)
		}
		copy(w.buffer, w.buffer[w.fullOffset():])
	case capacity > maxInt-capacity-extra:
		common.Log.Error("BUFFER too large")
		return
	default:
		buffer := make([]byte, 2*capacity+extra)
		copy(buffer, w.buffer)
		w.buffer = buffer
	}
	w.buffer = w.buffer[:length+extra]
}

func (r *Reader) ReadBits(n byte) (value uint64, err error) {
	if n < r.remainingBits {
		remaining := r.remainingBits - n
		value = uint64(r.bufferedBits >> remaining)
		r.bufferedBits &= 1<<remaining - 1
		r.remainingBits = remaining
		return value, nil
	}
	if n > r.remainingBits {
		if r.remainingBits > 0 {
			value = uint64(r.bufferedBits)
			n -= r.remainingBits
		}
		for n >= 8 {
			next, readErr := r.readBufferByte()
			if readErr != nil {
				return 0, readErr
			}
			value = value<<8 + uint64(next)
			n -= 8
		}
		if n > 0 {
			if r.bufferedBits, err = r.readBufferByte(); err != nil {
				return 0, err
			}
			remaining := 8 - n
			value = value<<n + uint64(r.bufferedBits>>remaining)
			r.bufferedBits &= 1<<remaining - 1
			r.remainingBits = remaining
		} else {
			r.remainingBits = 0
		}
		return value, nil
	}
	r.remainingBits = 0
	return uint64(r.bufferedBits), nil
}
func (r *Reader) BitPosition() int { return int(r.remainingBits) }
func (w *Writer) byteCapacity() int {
	capacity := len(w.buffer) - w.byteOffset
	if w.bitOffset != 0 {
		capacity--
	}
	return capacity
}

func (r *Reader) ReadUint32() (uint32, error) {
	buffer := make([]byte, 4)
	_, err := r.Read(buffer)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(buffer), nil
}

func (w *Writer) WriteBits(bits uint64, number int) (written int, err error) {
	const operation = "Writer.WriterBits"
	if number < 0 || number > 64 {
		return 0, jbig2errors.Errorf(operation, "bits number must be in range <0,64>, is: '%d'", number)
	}
	if number == 0 {
		return 0, nil
	}
	fullBytes := number / 8
	if fullBytes > 0 {
		remainingBits := number - fullBytes*8
		for i := fullBytes - 1; i >= 0; i-- {
			value := byte((bits >> uint(i*8+remainingBits)) & 0xff)
			if err = w.WriteByte(value); err != nil {
				return written, jbig2errors.Wrapf(err, operation, "byte: '%d'", fullBytes-i+1)
			}
		}
		number -= fullBytes * 8
		if number == 0 {
			return fullBytes, nil
		}
	}
	var bit int
	for i := 0; i < number; i++ {
		if w.msbFirst {
			bit = int((bits >> uint(number-1-i)) & 0x1)
		} else {
			bit = int(bits & 0x1)
			bits >>= 1
		}
		if err = w.WriteBit(bit); err != nil {
			return written, jbig2errors.Wrapf(err, operation, "bit: %d", i)
		}
	}
	return fullBytes, nil
}

func (w *BufferedWriter) writeFullBytes(data []byte) int {
	written := copy(w.buffer[w.fullOffset():], data)
	w.byteOffset += written
	return written
}
func (r *Reader) AbsoluteLength() uint64 { return uint64(len(r.source.data)) }
func (w *BufferedWriter) fullOffset() int {
	offset := w.byteOffset
	if w.bitOffset != 0 {
		offset++
	}
	return offset
}

func (r *Reader) Mark() {
	r.markedBytePosition = r.bytePosition
	r.markedRemainingBits = r.remainingBits
	r.markedBufferedBits = r.bufferedBits
	r.markedLastByte = r.lastByte
}

var _ io.Writer = &BufferedWriter{}

func (w *Writer) UseMSB() bool { return w.msbFirst }
func (w *Writer) FinishByte() {
	if w.bitOffset == 0 {
		return
	}
	w.bitOffset = 0
	w.byteOffset++
}
