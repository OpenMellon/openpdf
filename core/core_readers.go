package core

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
)

type bitReader struct {
	data  []byte
	pos   int
	acc   uint64
	nbits uint
}

func newBitReader(data []byte) *bitReader { return &bitReader{data: data} }

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

func (br *bitReader) skipToNextByte() {
	br.nbits -= br.nbits % 8
	if br.nbits == 0 {
		br.acc = 0
	}
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

// NewBitWriter returns an empty MSB-first bit writer.
func NewBitWriter() *BitWriter { return &BitWriter{} }

// Bytes returns the packed bytes, including any partial trailing byte zero-
// padded.
func (bw *BitWriter) Bytes() []byte {
	data := append([]byte(nil), bw.data...)
	if bw.nbits > 0 {
		data = append(data, byte(bw.acc<<(8-bw.nbits)))
	}
	return data
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

// AlignByte pads any partial byte out with zero bits so subsequent writes
// start on a byte boundary.
func (bw *BitWriter) AlignByte() {
	if bw.nbits == 0 {
		return
	}
	bw.WriteBits(0, 8-bw.nbits)
}

type byteReader interface {
	io.Reader
	io.ByteReader
}

func newByteReader(r io.Reader) byteReader {
	if br, ok := r.(byteReader); ok {
		return br
	}
	return bufio.NewReader(r)
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

type limitedDecodeReader struct {
	src         io.Reader
	limits      decodeLimits
	streamLimit int64
	read        int64
	filterName  string
	err         error
}

// Close closes the wrapped reader when it is an io.Closer.
func (r *limitedDecodeReader) Close() error {
	if c, ok := r.src.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (r *limitedDecodeReader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	if limit := r.limits.clampToTotal(r.streamLimit - r.read); limit < math.MaxInt64 && int64(len(p)) > limit+1 {
		p = p[:limit+1]
	}
	n, err := r.src.Read(p)
	allowed := r.limits.clampToTotal(r.streamLimit - r.read)
	if int64(n) > allowed {
		n = int(allowed)
		r.err = r.limits.exceeded(r.filterName, r.read+allowed)
		err = r.err
	}
	r.read += int64(n)
	r.limits.charge(int64(n))
	return n, err
}

type limitedDecoder interface{ setDecodeLimits(decodeLimits) }

type emptyReader struct{}

func (emptyReader) Read([]byte) (int, error) { return 0, io.EOF }

var bytePoolSizes = [...]int{poolSize16K, poolSize64K, poolSize256K, poolSize1M, poolSize4M, poolSize16M}

var bytePools = [len(bytePoolSizes)]sync.Pool{{New: func() any { data := make([]byte, poolSize16K); return &data }}, {New: func() any { data := make([]byte, poolSize64K); return &data }}, {New: func() any { data := make([]byte, poolSize256K); return &data }}, {New: func() any {
	data := make([]byte, poolSize1M)
	return &data
}}, {New: func() any { data := make([]byte, poolSize4M); return &data }}, {New: func() any { data := make([]byte, poolSize16M); return &data }}}

type offsetReaderAt struct {
	source io.ReaderAt
	offset int64
}

func (reader *offsetReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("offsetReaderAt.ReadAt: negative offset")
	}
	return reader.source.ReadAt(p, off+reader.offset)
}

func newOffsetReaderAt(source io.ReaderAt, offset int64) *offsetReaderAt {
	return &offsetReaderAt{source: source, offset: offset}
}

type ascii85Reader struct {
	src      byteReader
	err      error
	group    [4]byte
	groupLen int
	groupPos int
}

func (r *ascii85Reader) fillGroup() error {
	var data [5]byte
	n := 0
	ok := false
	for n < 5 {
		b, err := r.src.ReadByte()
		if err != nil {
			if err == io.EOF {
				if n == 0 {
					return io.EOF
				}
				ok = true
				break
			}
			return err
		}
		if IsWhiteSpace(b) {
			continue
		}
		if b == '~' {
			next, err2 := r.src.ReadByte()
			if err2 != nil || next != '>' {
				return errors.New("invalid code encountered")
			}
			ok = true
			break
		}
		if b == 'z' && n == 0 {
			r.group = [4]byte{0, 0, 0, 0}
			r.groupLen = 4
			r.groupPos = 0
			return nil
		}
		if b >= '!' && b <= 'u' {
			data[n] = b - '!'
			n++
			continue
		}
		return errors.New("invalid code encountered")
	}
	outLen := 4
	if ok {
		outLen = max(n-1, 0)
	}
	for i := n; i < 5; i++ {
		data[i] = 84
	}
	word := uint32(data[0])*85*85*85*85 + uint32(data[1])*85*85*85 + uint32(data[2])*85*85 + uint32(data[3])*85 + uint32(data[4])
	r.group[0] = byte(word >> 24)
	r.group[1] = byte(word >> 16)
	r.group[2] = byte(word >> 8)
	r.group[3] = byte(word)
	r.groupLen = outLen
	r.groupPos = 0
	if ok {
		return io.EOF
	}
	return nil
}

func (r *ascii85Reader) Read(p []byte) (int, error) {
	total := 0
	for total < len(p) {
		if r.groupPos < r.groupLen {
			copied := copy(p[total:], r.group[r.groupPos:r.groupLen])
			total += copied
			r.groupPos += copied
			continue
		}
		if r.err != nil {
			if total > 0 {
				return total, nil
			}
			return 0, r.err
		}
		r.err = r.fillGroup()
	}
	return total, nil
}
