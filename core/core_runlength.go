package core

import (
	"bytes"
	"io"
)

// UpdateParams updates the parameter values of the encoder.
func (runLength *RunLengthEncoder) UpdateParams(params *PdfObjectDictionary) {}

// DecodeBytes decodes a byte slice from Run length encoding.
//
// 7.4.5 RunLengthDecode Filter
// The RunLengthDecode filter decodes data that has been encoded in a simple byte-oriented format based on run length.
// The encoded data shall be a sequence of runs, where each run shall consist of a length byte followed by 1 to 128
// bytes of data. If the length byte is in the range 0 to 127, the following length + 1 (1 to 128) bytes shall be
// copied literally during decompression. If length is in the range 129 to 255, the following single byte shall be
// copied 257 - length (2 to 128) times during decompression. A length value of 128 shall denote EOD.
func (runLength *RunLengthEncoder) DecodeBytes(encoded []byte) ([]byte, error) {
	reader := bytes.NewReader(encoded)
	var data []byte
	remaining := runLength.limits.remaining()
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if b != 128 {
			count := int64(b) + 1
			if b > 128 {
				count = 257 - int64(b)
			}
			if int64(len(data))+count > remaining {
				return nil, runLength.limits.exceeded(StreamEncodingFilterNameRunLength, remaining)
			}
		}
		if b > 128 {
			c, err2 := reader.ReadByte()
			if err2 != nil {
				return nil, err2
			}
			for i := 0; i < 257-int(b); i++ {
				data = append(data, c)
			}
		} else if b < 128 {
			for i := 0; i < int(b)+1; i++ {
				c, err2 := reader.ReadByte()
				if err2 != nil {
					return nil, err2
				}
				data = append(data, c)
			}
		} else {
			break
		}
	}
	runLength.limits.charge(int64(len(data)))
	return data, nil
}

func (runLength *RunLengthEncoder) setDecodeLimits(limits decodeLimits) { runLength.limits = limits }

func (r *runLengthReader) fillRun() error {
	b, err := r.src.ReadByte()
	if err != nil {
		return err
	}
	switch {
	case b == 128:
		return io.EOF
	case b > 128:
		c, err2 := r.src.ReadByte()
		if err2 != nil {
			return err2
		}
		count := 257 - int(b)
		for i := 0; i < count; i++ {
			r.run[i] = c
		}
		r.runEnd = count
		r.runPos = 0
		return nil
	default:
		count := int(b) + 1
		if _, err2 := io.ReadFull(r.src, r.run[:count]); err2 != nil {
			return err2
		}
		r.runEnd = count
		r.runPos = 0
		return nil
	}
}

// RunLengthEncoder represents Run length encoding.
type RunLengthEncoder struct{ limits decodeLimits }

func (r *runLengthReader) Read(p []byte) (int, error) {
	total := 0
	for total < len(p) {
		if r.runPos < r.runEnd {
			copied := copy(p[total:], r.run[r.runPos:r.runEnd])
			total += copied
			r.runPos += copied
			continue
		}
		if r.err != nil {
			if total > 0 {
				return total, nil
			}
			return 0, r.err
		}
		r.err = r.fillRun()
	}
	return total, nil
}

// DecodeStream decodes RunLengthEncoded stream object and give back decoded bytes.
func (runLength *RunLengthEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return runLength.DecodeBytes(streamObj.Stream)
}

// NewRunLengthEncoder makes a new run length encoder
func NewRunLengthEncoder() *RunLengthEncoder { return &RunLengthEncoder{} }

// GetFilterName returns the name of the encoding filter.
func (runLength *RunLengthEncoder) GetFilterName() string { return StreamEncodingFilterNameRunLength }

// EncodeBytes encodes a bytes array and return the encoded value based on the encoder parameters.
func (runLength *RunLengthEncoder) EncodeBytes(data []byte) ([]byte, error) {
	reader := bytes.NewReader(data)
	var encoded []byte
	var literal []byte
	b, err := reader.ReadByte()
	if err == io.EOF {
		return []byte{}, nil
	} else if err != nil {
		return nil, err
	}
	runLen := 1
	for {
		next, err2 := reader.ReadByte()
		if err2 == io.EOF {
			break
		} else if err2 != nil {
			return nil, err2
		}
		if next == b {
			if len(literal) > 0 {
				literal = literal[:len(literal)-1]
				if len(literal) > 0 {
					encoded = append(encoded, byte(len(literal)-1))
					encoded = append(encoded, literal...)
				}
				runLen = 1
				literal = []byte{}
			}
			runLen++
			if runLen >= 127 {
				encoded = append(encoded, byte(257-runLen), b)
				runLen = 0
			}
		} else {
			if runLen > 0 {
				if runLen == 1 {
					literal = []byte{b}
				} else {
					encoded = append(encoded, byte(257-runLen), b)
				}
				runLen = 0
			}
			literal = append(literal, next)
			if len(literal) >= 127 {
				encoded = append(encoded, byte(len(literal)-1))
				encoded = append(encoded, literal...)
				literal = []byte{}
			}
		}
		b = next
	}
	if len(literal) > 0 {
		encoded = append(encoded, byte(len(literal)-1))
		encoded = append(encoded, literal...)
	} else if runLen > 0 {
		encoded = append(encoded, byte(257-runLen), b)
	}
	encoded = append(encoded, 128)
	return encoded, nil
}

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
func (runLength *RunLengthEncoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName(runLength.GetFilterName()))
	return dict
}

type runLengthReader struct {
	src    byteReader
	err    error
	run    [128]byte
	runEnd int
	runPos int
}

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (runLength *RunLengthEncoder) MakeDecodeParams() PdfObject { return nil }

// DecodeReader returns a reader that RunLength-decodes from r. A length byte
// in [0,127] introduces length+1 literal bytes; a length byte in [129,255]
// repeats the next byte 257-length times; a length byte of 128 marks EOD.
func (runLength *RunLengthEncoder) DecodeReader(r io.Reader) io.Reader {
	return runLength.limits.reader(StreamEncodingFilterNameRunLength, &runLengthReader{src: newByteReader(r)})
}

func newRunLengthEncoderFromStream(_ *PdfObjectStream, _ *PdfObjectDictionary) (*RunLengthEncoder, error) {
	return NewRunLengthEncoder(), nil
}
