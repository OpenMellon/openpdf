package regress

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// comparePNG decodes both images and requires identical pixels. Comparing pixels rather than file bytes keeps the
// goldens valid across Go releases that change the PNG encoder.
func comparePNG(t *testing.T, outDir, file string, want, got []byte) {
	t.Helper()
	wantImg, err := png.Decode(bytes.NewReader(want))
	if err != nil {
		t.Errorf("%s: golden is not a PNG: %v", file, err)
		return
	}
	gotImg, err := png.Decode(bytes.NewReader(got))
	if err != nil {
		t.Errorf("%s: actual is not a PNG: %v", file, err)
		return
	}
	diff, n := diffImages(wantImg, gotImg)
	if n == 0 {
		return
	}
	t.Errorf("%s: %d pixels differ (golden %v, actual %v)", file, n, wantImg.Bounds(), gotImg.Bounds())
	saveActual(t, outDir, file, got)
	if data, err := encodePNG(diff); err == nil {
		path := filepath.Join(outDir, "diff-"+file)
		if err := os.WriteFile(path, data, 0o644); err == nil {
			t.Logf("diff (differences in red) saved to %s", path)
		}
	}
}

// diffImages returns an image with every differing pixel painted red over a faded copy of want, and the number of
// differing pixels. Pixels outside the overlap of the two bounds count as differing.
func diffImages(want, got image.Image) (image.Image, int) {
	wb, gb := want.Bounds(), got.Bounds()
	b := wb.Union(gb)
	out := image.NewRGBA(b)
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p := image.Pt(x, y)
			if !p.In(wb) || !p.In(gb) || !samePixel(want.At(x, y), got.At(x, y)) {
				out.Set(x, y, color.RGBA{R: 255, A: 255})
				n++
				continue
			}
			r, g, bl, _ := want.At(x, y).RGBA()
			out.Set(x, y, color.RGBA{R: fade(r), G: fade(g), B: fade(bl), A: 255})
		}
	}
	return out, n
}

func samePixel(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// fade lightens a 16-bit channel towards white so the red differences stand out.
func fade(v uint32) uint8 { return uint8((v>>8)/2 + 128) }
