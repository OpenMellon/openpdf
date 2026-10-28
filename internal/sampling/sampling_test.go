package sampling_test

import (
	"io"
	"reflect"
	"testing"

	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/sampling"
)

func TestResampleWidths(t *testing.T) {
	want := []uint32{0xa, 0xb, 0xc, 0xd}
	if got := sampling.ResampleBytes([]byte{0xab, 0xcd}, 4); !reflect.DeepEqual(got, want) {
		t.Fatalf("ResampleBytes = %v", got)
	}
	// Preserve the current uint32 conversion behavior pending separate investigation.
	if got := sampling.ResampleUint32([]uint32{0xab, 0xcd}, 8, 4); !reflect.DeepEqual(got, []uint32{0xa, 0xab, 0xc, 0xcd}) {
		t.Fatalf("ResampleUint32 = %v", got)
	}
}

func TestPaddedRowSampleRoundTrip(t *testing.T) {
	img := imageutil.ImageBase{Width: 3, Height: 2, BitsPerComponent: 2, ColorComponents: 1, BytesPerLine: 1, Data: make([]byte, 2)}
	input := []uint32{0, 1, 2, 3, 2, 1}
	if err := sampling.NewWriter(img).WriteSamples(input); err != nil {
		t.Fatal(err)
	}
	reader := sampling.NewReader(img)
	got := make([]uint32, len(input))
	if err := reader.ReadSamples(got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, input) {
		t.Fatalf("samples = %v, want %v", got, input)
	}
	if _, err := reader.ReadSample(); err != io.EOF {
		t.Fatalf("end = %v, want EOF", err)
	}
}
