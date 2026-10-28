package core

import (
	"fmt"
	"io"
	"math"
	"sync/atomic"
)

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func applyDecodeLimits(encoder StreamEncoder, limits decodeLimits) {
	if limited, ok := encoder.(limitedDecoder); ok {
		limited.setDecodeLimits(limits)
	}
}

func (limits decodeLimits) streamLimit() int64 {
	if limits.maxStream <= 0 {
		return DefaultMaxDecodedStreamSize()
	}
	return limits.maxStream
}

// StreamingDecoder is implemented by stream encoders that can decode their
// input incrementally, without materializing the full decoded output in
// memory. Decoders that do not implement this interface (image codecs such
// as DCT, JPX, CCITTFax, JBIG2) must be consumed via DecodeBytes.
//
// The reader returned by DecodeReader produces the same bytes that
// DecodeBytes would produce for the same valid input. Decoding errors are
// surfaced lazily via Read.
type StreamingDecoder interface {
	DecodeReader(r io.Reader) io.Reader
}

// Close closes the wrapped reader when it is an io.Closer.
func (r *limitedDecodeReader) Close() error {
	if c, ok := r.src.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (limits decodeLimits) readAll(filterName string, r io.Reader) ([]byte, error) {
	remaining := limits.remaining()
	data := AcquireBytes(poolSize16K)[:0]
	var err error
	for {
		if len(data) == cap(data) {
			if int64(len(data)) >= remaining {
				var ok bool
				if ok, err = readerHasData(r); ok {
					ReleaseBytes(data)
					return nil, limits.exceeded(filterName, remaining)
				}
				break
			}
			grown := AcquireBytes(int(min(2*int64(cap(data)), remaining, math.MaxInt)))[:len(data)]
			copy(grown, data)
			ReleaseBytes(data)
			data = grown
		}
		var read int
		read, err = r.Read(data[len(data):cap(data)])
		data = data[:len(data)+read]
		if int64(len(data)) > remaining {
			ReleaseBytes(data)
			return nil, limits.exceeded(filterName, remaining)
		}
		if err != nil {
			break
		}
	}
	if err == io.EOF {
		err = nil
	}
	if len(data) == 0 {
		ReleaseBytes(data)
		return nil, err
	}
	if len(data) < minPoolBufSize {
		dst := make([]byte, len(data))
		copy(dst, data)
		ReleaseBytes(data)
		return dst, err
	}
	if cap(data) > poolSize16M && cap(data)-len(data) > len(data)/2 {
		dst := make([]byte, len(data))
		copy(dst, data)
		data = dst
	}
	return data, err
}

func (limits decodeLimits) check(filterName string, n int64) error {
	if remaining := limits.remaining(); n > remaining {
		return limits.exceeded(filterName, remaining)
	}
	return nil
}

func (limits decodeLimits) reader(filterName string, r io.Reader) io.Reader {
	return &limitedDecodeReader{src: r, limits: limits, streamLimit: limits.streamLimit(), filterName: filterName}
}

type limitedDecoder interface{ setDecodeLimits(decodeLimits) }

func (limits decodeLimits) remaining() int64 { return limits.clampToTotal(limits.streamLimit()) }

func readerHasData(r io.Reader) (bool, error) {
	var data [1]byte
	for {
		nr, err := r.Read(data[:])
		if nr > 0 {
			return true, nil
		}
		if err != nil {
			return false, err
		}
	}
}

func (limits decodeLimits) charge(n int64) {
	if limits.total != nil && n > 0 {
		limits.total.Add(n)
	}
}

type emptyReader struct{}

func (limits decodeLimits) clampToTotal(n int64) int64 {
	if limits.maxTotal > 0 && limits.total != nil {
		if remaining := limits.maxTotal - limits.total.Load(); remaining < n {
			n = remaining
		}
	}
	return max(n, 0)
}

type limitedDecodeReader struct {
	src         io.Reader
	limits      decodeLimits
	streamLimit int64
	read        int64
	filterName  string
	err         error
}
type errorReader struct{ err error }

func (emptyReader) Read([]byte) (int, error) { return 0, io.EOF }

type decodeLimits struct {
	maxStream int64
	maxTotal  int64
	total     *atomic.Int64
}

// SetDefaultMaxDecodedStreamSize sets the maximum number of bytes a single
// stream, or a single filter stage of a filter chain, may decode to when no
// ParserOpts.MaxDecodedStreamSize applies. That covers streams created in
// memory and streams read by a parser whose options leave the limit at 0.
// A value <= 0 restores the built-in default of 256 MiB.
// Decoding past the limit fails with ErrDecodedSizeLimitExceeded.
func SetDefaultMaxDecodedStreamSize(n int64) {
	if n <= 0 {
		n = defaultMaxDecodedStreamSize
	}
	maxDecodedStreamSize.Store(n)
}

func decodeLimitsFromStream(streamObj *PdfObjectStream) decodeLimits {
	if streamObj == nil {
		return decodeLimits{}
	}
	parser := streamObj.GetParser()
	if parser == nil {
		return decodeLimits{}
	}
	limits := decodeLimits{total: parser.decodedTotal}
	if parser.Opts != nil {
		limits.maxStream = parser.Opts.MaxDecodedStreamSize
		limits.maxTotal = parser.Opts.MaxDecodedTotalSize
	}
	return limits
}

var maxDecodedStreamSize atomic.Int64

func (limits decodeLimits) exceeded(name string, limit int64) error {
	return fmt.Errorf("%w: %s output over %d bytes", ErrDecodedSizeLimitExceeded, name, limit)
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

// DefaultMaxDecodedStreamSize returns the per-stream decoded size limit set by
// SetDefaultMaxDecodedStreamSize.
func DefaultMaxDecodedStreamSize() int64 { return maxDecodedStreamSize.Load() }

const defaultMaxDecodedStreamSize = 256 << 20
