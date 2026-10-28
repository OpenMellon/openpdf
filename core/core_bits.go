package core

import (
	"bufio"
	"fmt"
	"io"
)

func newBitReader(data []byte) *bitReader { return &bitReader{data: data} }

// NewBitWriter returns an empty MSB-first bit writer.
func NewBitWriter() *BitWriter { return &BitWriter{} }

func (br *bitReader) seek(pos int) error {
	if pos < 0 || pos > len(br.data) {
		return fmt.Errorf("hint stream: seek out of range (%d, len=%d)", pos, len(br.data))
	}
	br.pos = pos
	br.acc = 0
	br.nbits = 0
	return nil
}

func (br *bitReader) readBits(bits uint) (uint32, error) {
	if bits == 0 {
		return 0, nil
	}
	if bits > 32 {
		return 0, fmt.Errorf("hint stream: readBits n=%d exceeds 32", bits)
	}
	for br.nbits < bits {
		if br.pos >= len(br.data) {
			return 0, errHintShortRead
		}
		br.acc = (br.acc << 8) | uint64(br.data[br.pos])
		br.pos++
		br.nbits += 8
	}
	shift := br.nbits - bits
	mask := uint64(1)<<bits - 1
	val := uint32((br.acc >> shift) & mask)
	br.nbits -= bits
	br.acc &= (uint64(1) << br.nbits) - 1
	return val, nil
}

type bitReader struct {
	data  []byte
	pos   int
	acc   uint64
	nbits uint
}

func (br *bitReader) skipBits(nbits uint) error {
	for nbits > 32 {
		if _, err := br.readBits(32); err != nil {
			return err
		}
		nbits -= 32
	}
	_, err := br.readBits(nbits)
	return err
}

// Bytes returns the packed bytes, including any partial trailing byte zero-
// padded.
func (bw *BitWriter) Bytes() []byte {
	data := append([]byte(nil), bw.data...)
	if bw.nbits > 0 {
		data = append(data, byte(bw.acc<<(8-bw.nbits)))
	}
	return data
}

type byteReader interface {
	io.Reader
	io.ByteReader
}

// Len returns the number of complete bytes plus 1 for a partial byte.
func (bw *BitWriter) Len() int {
	if bw.nbits == 0 {
		return len(bw.data)
	}
	return len(bw.data) + 1
}

// WriteBits writes the low n bits of v into the buffer, MSB-first.
// Caller is responsible for ensuring v fits in n bits.
func (bw *BitWriter) WriteBits(v uint32, n uint) {
	if n == 0 {
		return
	}
	bw.acc = (bw.acc << n) | uint64(v&((1<<n)-1))
	bw.nbits += n
	for bw.nbits >= 8 {
		bw.nbits -= 8
		bw.data = append(bw.data, byte((bw.acc>>bw.nbits)&0xFF))
		bw.acc &= (1 << bw.nbits) - 1
	}
}

const intBits = 32 << (^uint(0) >> 63)

func newByteReader(r io.Reader) byteReader {
	if br, ok := r.(byteReader); ok {
		return br
	}
	return bufio.NewReader(r)
}

// AlignByte pads any partial byte out with zero bits so subsequent writes
// start on a byte boundary.
func (bw *BitWriter) AlignByte() {
	if bw.nbits == 0 {
		return
	}
	bw.WriteBits(0, 8-bw.nbits)
}

// BitWriter is an MSB-first bit packer. PDF hint tables specify all fields
// as variable-width unsigned integers packed with no padding between fields;
// alignment to a byte boundary is only required between top-level tables.
//
// Exported so the model package can use it for hint stream encoding.
type BitWriter struct {
	data  []byte
	acc   uint64
	nbits uint
}

func (br *bitReader) skipToNextByte() {
	br.nbits -= br.nbits % 8
	if br.nbits == 0 {
		br.acc = 0
	}
}
