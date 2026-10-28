// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package sampling

import (
	"io"

	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/imageutil"
)

func ResampleUint32(data []uint32, bitsPerInputSample int, bitsPerOutputSample int) []uint32 {
	var output []uint32
	outputBitsRemaining := bitsPerOutputSample
	var outputSample uint32
	var remainingInput uint32
	inputBitsRemaining := 0
	outputCount := 0
	inputIndex := 0
	for inputIndex < len(data) {
		if inputBitsRemaining > 0 {
			bitsToCopy := inputBitsRemaining
			if outputBitsRemaining < bitsToCopy {
				bitsToCopy = outputBitsRemaining
			}
			outputSample = (outputSample << uint(bitsToCopy)) | (remainingInput >> uint(bitsPerInputSample-bitsToCopy))
			inputBitsRemaining -= bitsToCopy
			if inputBitsRemaining > 0 {
				remainingInput = remainingInput << uint(bitsToCopy)
			} else {
				remainingInput = 0
			}
			outputBitsRemaining -= bitsToCopy
			if outputBitsRemaining == 0 {
				output = append(output, outputSample)
				outputBitsRemaining = bitsPerOutputSample
				outputSample = 0
				outputCount++
			}
		} else {
			inputSample := data[inputIndex]
			inputIndex++
			bitsToCopy := bitsPerInputSample
			if outputBitsRemaining < bitsToCopy {
				bitsToCopy = outputBitsRemaining
			}
			inputBitsRemaining = bitsPerInputSample - bitsToCopy
			outputSample = (outputSample << uint(bitsToCopy)) | (inputSample >> uint(inputBitsRemaining))
			if bitsToCopy < bitsPerInputSample {
				remainingInput = inputSample << uint(bitsToCopy)
			}
			outputBitsRemaining -= bitsToCopy
			if outputBitsRemaining == 0 {
				output = append(output, outputSample)
				outputBitsRemaining = bitsPerOutputSample
				outputSample = 0
				outputCount++
			}
		}
	}
	for inputBitsRemaining >= bitsPerOutputSample {
		bitsToCopy := inputBitsRemaining
		if outputBitsRemaining < bitsToCopy {
			bitsToCopy = outputBitsRemaining
		}
		outputSample = (outputSample << uint(bitsToCopy)) | (remainingInput >> uint(bitsPerInputSample-bitsToCopy))
		inputBitsRemaining -= bitsToCopy
		if inputBitsRemaining > 0 {
			remainingInput = remainingInput << uint(bitsToCopy)
		} else {
			remainingInput = 0
		}
		outputBitsRemaining -= bitsToCopy
		if outputBitsRemaining == 0 {
			output = append(output, outputSample)
			outputBitsRemaining = bitsPerOutputSample
			outputSample = 0
			outputCount++
		}
	}
	if outputBitsRemaining > 0 && outputBitsRemaining < bitsPerOutputSample {
		outputSample <<= uint(outputBitsRemaining)
		output = append(output, outputSample)
	}
	return output
}

type SampleWriter interface {
	WriteSample(sample uint32) error
	WriteSamples(samples []uint32) error
}

func NewReader(img imageutil.ImageBase) *Reader {
	return &Reader{bits: bitwise.NewReader(img.Data), image: img, componentsRemaining: img.ColorComponents, paddedRows: img.BytesPerLine*8 != img.ColorComponents*img.BitsPerComponent*img.Width}
}

type Reader struct {
	image                            imageutil.ImageBase
	bits                             *bitwise.Reader
	column, row, componentsRemaining int
	paddedRows                       bool
}
type SampleReader interface {
	ReadSample() (uint32, error)
	ReadSamples(samples []uint32) error
}

func (r *Reader) ReadSample() (uint32, error) {
	if r.row == r.image.Height {
		return 0, io.EOF
	}
	value, err := r.bits.ReadBits(byte(r.image.BitsPerComponent))
	if err != nil {
		return 0, err
	}
	r.componentsRemaining--
	if r.componentsRemaining == 0 {
		r.componentsRemaining = r.image.ColorComponents
		r.column++
	}
	if r.column == r.image.Width {
		if r.paddedRows {
			r.bits.ConsumeRemainingBits()
		}
		r.column = 0
		r.row++
	}
	return uint32(value), nil
}

func NewWriter(img imageutil.ImageBase) *Writer {
	return &Writer{bits: bitwise.NewWriterMSB(img.Data), image: img, componentsRemaining: img.ColorComponents, paddedRows: img.BytesPerLine*8 != img.ColorComponents*img.BitsPerComponent*img.Width}
}

func (w *Writer) WriteSamples(samples []uint32) error {
	for i := 0; i < len(samples); i++ {
		if err := w.WriteSample(samples[i]); err != nil {
			return err
		}
	}
	return nil
}

type Writer struct {
	image                       imageutil.ImageBase
	bits                        *bitwise.Writer
	column, componentsRemaining int
	paddedRows                  bool
}

func (r *Reader) ReadSamples(samples []uint32) (err error) {
	for i := 0; i < len(samples); i++ {
		samples[i], err = r.ReadSample()
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *Writer) WriteSample(sample uint32) error {
	if _, err := w.bits.WriteBits(uint64(sample), w.image.BitsPerComponent); err != nil {
		return err
	}
	w.componentsRemaining--
	if w.componentsRemaining == 0 {
		w.componentsRemaining = w.image.ColorComponents
		w.column++
	}
	if w.column == w.image.Width {
		if w.paddedRows {
			w.bits.FinishByte()
		}
		w.column = 0
	}
	return nil
}

func ResampleBytes(data []byte, bitsPerSample int) []uint32 {
	var output []uint32
	outputBitsRemaining := bitsPerSample
	var outputSample uint32
	var remainingInput byte
	inputBitsRemaining := 0
	outputCount := 0
	inputIndex := 0
	for inputIndex < len(data) {
		if inputBitsRemaining > 0 {
			bitsToCopy := inputBitsRemaining
			if outputBitsRemaining < bitsToCopy {
				bitsToCopy = outputBitsRemaining
			}
			outputSample = (outputSample << uint(bitsToCopy)) | uint32(remainingInput>>uint(8-bitsToCopy))
			inputBitsRemaining -= bitsToCopy
			if inputBitsRemaining > 0 {
				remainingInput = remainingInput << uint(bitsToCopy)
			} else {
				remainingInput = 0
			}
			outputBitsRemaining -= bitsToCopy
			if outputBitsRemaining == 0 {
				output = append(output, outputSample)
				outputBitsRemaining = bitsPerSample
				outputSample = 0
				outputCount++
			}
		} else {
			inputByte := data[inputIndex]
			inputIndex++
			bitsToCopy := 8
			if outputBitsRemaining < bitsToCopy {
				bitsToCopy = outputBitsRemaining
			}
			inputBitsRemaining = 8 - bitsToCopy
			outputSample = (outputSample << uint(bitsToCopy)) | uint32(inputByte>>uint(inputBitsRemaining))
			if bitsToCopy < 8 {
				remainingInput = inputByte << uint(bitsToCopy)
			}
			outputBitsRemaining -= bitsToCopy
			if outputBitsRemaining == 0 {
				output = append(output, outputSample)
				outputBitsRemaining = bitsPerSample
				outputSample = 0
				outputCount++
			}
		}
	}
	for inputBitsRemaining >= bitsPerSample {
		bitsToCopy := inputBitsRemaining
		if outputBitsRemaining < bitsToCopy {
			bitsToCopy = outputBitsRemaining
		}
		outputSample = (outputSample << uint(bitsToCopy)) | uint32(remainingInput>>uint(8-bitsToCopy))
		inputBitsRemaining -= bitsToCopy
		if inputBitsRemaining > 0 {
			remainingInput = remainingInput << uint(bitsToCopy)
		} else {
			remainingInput = 0
		}
		outputBitsRemaining -= bitsToCopy
		if outputBitsRemaining == 0 {
			output = append(output, outputSample)
			outputBitsRemaining = bitsPerSample
			outputSample = 0
			outputCount++
		}
	}
	return output
}
