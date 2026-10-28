package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sync/atomic"

	"github.com/OpenMellon/openpdf/common"
)

// DecodeStream decodes the stream data and returns the decoded data.
// An error is returned upon failure.
func DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Decode stream")
	}
	encoderFromStream, err := NewEncoderFromStream(streamObj)
	if err != nil {
		common.Log.Debug("ERROR: Stream decoding failed: %v", err)
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Encoder: %#v\n", encoderFromStream)
	}
	stream, err := encoderFromStream.DecodeStream(streamObj)
	if err != nil {
		common.Log.Debug("ERROR: Stream decoding failed: %v", err)
		return nil, err
	}
	return stream, nil
}

func decodePredictor(data []byte, predictor, columns, bitsPerComponent, colors int) ([]byte, error) {
	rowBytes, bytesPerPixel, bitsPerComp, err := normalizePredictorParams(columns, bitsPerComponent, colors)
	if err != nil {
		return nil, err
	}
	switch {
	case predictor == 2:
		return decodeTIFFPredictor(data, columns, bitsPerComp, colors, rowBytes)
	case predictor >= 10 && predictor <= 15:
		return decodePNGPredictor(data, rowBytes, bytesPerPixel)
	}
	return nil, fmt.Errorf("unsupported predictor (%d)", predictor)
}

func decodePNGPredictor(data []byte, rowBytes, bpp int) ([]byte, error) {
	stride := rowBytes + 1
	if len(data)%stride != 0 {
		return nil, fmt.Errorf("invalid row length (%d/%d)", len(data), stride)
	}
	if stride > len(data) {
		return nil, errors.New("range check error")
	}
	outOff := 0
	zeroRow := make([]byte, rowBytes)
	prevRow := zeroRow
	for i := 0; i < len(data); i += stride {
		b, row := data[i], data[i+1:i+stride]
		switch b {
		case predictorNone:
		case predictorSub:
			for j := bpp; j < rowBytes; j++ {
				row[j] += row[j-bpp]
			}
		case predictorUp:
			for j := range row {
				row[j] += prevRow[j]
			}
		case predictorAverage:
			for j := range row {
				var leftInt int
				if j >= bpp {
					leftInt = int(row[j-bpp])
				}
				row[j] += byte((leftInt + int(prevRow[j])) / 2)
			}
		case predictorPaeth:
			for j := range row {
				var left, upperLeft byte
				if j >= bpp {
					left, upperLeft = row[j-bpp], prevRow[j-bpp]
				}
				row[j] += paethPredictor(left, prevRow[j], upperLeft)
			}
		default:
			return nil, fmt.Errorf("invalid filter byte (%d)", b)
		}
		copy(data[outOff:], row)
		prevRow = data[outOff : outOff+rowBytes]
		outOff += rowBytes
	}
	return data[:outOff], nil
}

func decodeTIFFPredictor(data []byte, columns, bitsPerComp, colors, rowBytes int) ([]byte, error) {
	if rowBytes < 1 {
		return data[:0], nil
	}
	if len(data)%rowBytes != 0 {
		return nil, fmt.Errorf("invalid row length (%d/%d)", len(data), rowBytes)
	}
	if rowBytes > len(data) {
		return nil, errors.New("range check error")
	}
	for i := 0; i < len(data); i += rowBytes {
		row := data[i : i+rowBytes]
		switch bitsPerComp {
		case 8:
			for j := columns; j < rowBytes; j++ {
				row[j] += row[j-columns]
			}
		case 16:
			for j := 2 * columns; j+1 < rowBytes; j += 2 {
				sum := binary.BigEndian.Uint16(row[j:]) + binary.BigEndian.Uint16(row[j-2*columns:])
				binary.BigEndian.PutUint16(row[j:], sum)
			}
		default:
			b := byte(1)<<bitsPerComp - 1
			bitPos := func(col int) (int, int) { bit := col * bitsPerComp; return bit / 8, 8 - bitsPerComp - bit%8 }
			for j := columns; j < colors*columns; j++ {
				byteIdx, shift := bitPos(j)
				bytePrev, shiftPrev := bitPos(j - columns)
				sum := (row[byteIdx]>>shift + row[bytePrev]>>shiftPrev) & b
				row[byteIdx] = row[byteIdx]&^(b<<shift) | sum<<shift
			}
		}
	}
	return data, nil
}

type decodeLimits struct {
	maxStream int64
	maxTotal  int64
	total     *atomic.Int64
}

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

func (limits decodeLimits) remaining() int64 { return limits.clampToTotal(limits.streamLimit()) }

func (limits decodeLimits) charge(n int64) {
	if limits.total != nil && n > 0 {
		limits.total.Add(n)
	}
}

func (limits decodeLimits) clampToTotal(n int64) int64 {
	if limits.maxTotal > 0 && limits.total != nil {
		if remaining := limits.maxTotal - limits.total.Load(); remaining < n {
			n = remaining
		}
	}
	return max(n, 0)
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

func (limits decodeLimits) exceeded(name string, limit int64) error {
	return fmt.Errorf("%w: %s output over %d bytes", ErrDecodedSizeLimitExceeded, name, limit)
}
