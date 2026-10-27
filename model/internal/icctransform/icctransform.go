// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package icctransform

import (
	"encoding/binary"
	"errors"
	"math"
)

var errNotMatrixTRC = errors.New("icctransform: not a matrix/TRC RGB profile")

type Transform struct {
	redTRC, greenTRC, blueTRC curve
	toSRGB                    mat3
}

func readUint32(data []byte, off int) (v uint32, ok bool) {
	if off < 0 || off+4 > len(data) {
		return 0, false
	}
	return binary.BigEndian.Uint32(data[off : off+4]), true
}
func NewTransform(profile []byte) (*Transform, error) {
	header, err := parseHeader(profile)
	if err != nil {
		return nil, err
	}
	if !header.isMatrixTRCCandidate() {
		return nil, errNotMatrixTRC
	}
	tags, err := parseTagTable(profile)
	if err != nil {
		return nil, err
	}
	cols, err := parseColorants(tags)
	if err != nil {
		return nil, err
	}
	curves, err := parseToneCurves(tags)
	if err != nil {
		return nil, err
	}
	m := xyzToLinearSRGB.mul(d50ToD65).mul(colorantMatrix(cols))
	return &Transform{redTRC: curves.red, greenTRC: curves.green, blueTRC: curves.blue, toSRGB: m}, nil
}

var wantedTags = map[string]struct{}{tagRXYZ: {}, tagGXYZ: {}, tagBXYZ: {}, tagRTRC: {}, tagGTRC: {}, tagBTRC: {}, tagWtpt: {}}

func (t *Transform) LinearizeChannel(channel int, x float64) float64 {
	switch channel {
	case 0:
		return t.redTRC(x)
	case 1:
		return t.greenTRC(x)
	default:
		return t.blueTRC(x)
	}
}
func parseHeader(data []byte) (iccHeader, error) {
	if len(data) < headerLen {
		return iccHeader{}, errShortHeader
	}
	if string(data[signatureOffset:signatureOffset+4]) != "acsp" {
		return iccHeader{}, errBadSignature
	}
	return iccHeader{majorVersion: int(data[8]), colorSpace: string(data[16:20]), pcs: string(data[20:24])}, nil
}
func readS15Fixed16(data []byte, off int) float64 {
	return float64(int32(binary.BigEndian.Uint32(data[off:off+4]))) / 65536.0
}

const (
	sigCurv       = "curv"
	curvHeaderLen = 12
)

type mat3 [3][3]float64

func parseCurv(data []byte) (curve, error) {
	if len(data) < curvHeaderLen {
		return nil, errMalformedTRC
	}
	count := binary.BigEndian.Uint32(data[8:12])
	switch count {
	case 0:
		return func(x float64) float64 { return x }, nil
	case 1:
		if len(data) < curvHeaderLen+2 {
			return nil, errMalformedTRC
		}
		gamma := float64(binary.BigEndian.Uint16(data[curvHeaderLen:curvHeaderLen+2])) / 256.0
		return gammaCurve(gamma), nil
	default:
		if int64(curvHeaderLen)+int64(count)*2 > int64(len(data)) {
			return nil, errMalformedTRC
		}
		table := make([]float64, count)
		for i := range table {
			off := curvHeaderLen + i*2
			table[i] = float64(binary.BigEndian.Uint16(data[off:off+2])) / 65535.0
		}
		return tableCurve(table), nil
	}
}

var (
	errShortHeader  = errors.New("icctransform: profile shorter than 128-byte header")
	errBadSignature = errors.New("icctransform: missing 'acsp' profile signature")
)

func (m mat3) mul(o mat3) mat3 {
	var out mat3
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			out[i][j] = m[i][0]*o[0][j] + m[i][1]*o[1][j] + m[i][2]*o[2][j]
		}
	}
	return out
}
func parametricCurve(funcType uint16, params []float64) curve {
	switch funcType {
	case 0:
		g := params[0]
		return func(x float64) float64 { return safePow(x, g) }
	case 1:
		g, a, b := params[0], params[1], params[2]
		threshold := -b / a
		return func(x float64) float64 {
			if x >= threshold {
				return safePow(a*x+b, g)
			}
			return 0
		}
	case 2:
		g, a, b, c := params[0], params[1], params[2], params[3]
		threshold := -b / a
		return func(x float64) float64 {
			if x >= threshold {
				return safePow(a*x+b, g) + c
			}
			return c
		}
	case 3:
		g, a, b, c, d := params[0], params[1], params[2], params[3], params[4]
		return func(x float64) float64 {
			if x >= d {
				return safePow(a*x+b, g)
			}
			return c * x
		}
	case 4:
		g, a, b, c, d, e, f := params[0], params[1], params[2], params[3], params[4], params[5], params[6]
		return func(x float64) float64 {
			if x >= d {
				return safePow(a*x+b, g) + e
			}
			return c*x + f
		}
	}
	return nil
}

const (
	sigRGB = "RGB "
	sigXYZ = "XYZ "
	sigLab = "Lab "
)

func (h iccHeader) isMatrixTRCCandidate() bool { return h.colorSpace == sigRGB && h.pcs == sigXYZ }

type colorants struct {
	red, green, blue [3]float64
	whitePoint       [3]float64
}

func colorantMatrix(c colorants) mat3 {
	return mat3{{c.red[0], c.green[0], c.blue[0]}, {c.red[1], c.green[1], c.blue[1]}, {c.red[2], c.green[2], c.blue[2]}}
}

var xyzToLinearSRGB = mat3{{3.2404542, -1.5371385, -0.4985314}, {-0.9692660, 1.8760108, 0.0415560}, {0.0556434, -0.2040259, 1.0572252}}

func (m mat3) apply(v [3]float64) [3]float64 {
	return [3]float64{m[0][0]*v[0] + m[0][1]*v[1] + m[0][2]*v[2], m[1][0]*v[0] + m[1][1]*v[1] + m[1][2]*v[2], m[2][0]*v[0] + m[2][1]*v[1] + m[2][2]*v[2]}
}
func (t *Transform) EncodeLinear(lr, lg, lb float64) (r, g, b float64) {
	rgb := t.toSRGB.apply([3]float64{lr, lg, lb})
	return encodeSRGB(rgb[0]), encodeSRGB(rgb[1]), encodeSRGB(rgb[2])
}

const (
	sigXYZType    = "XYZ "
	xyzDataOffset = 8
	xyzDataLen    = 12
)

func tableCurve(table []float64) curve {
	n := len(table)
	return func(x float64) float64 {
		switch {
		case x <= 0:
			return table[0]
		case x >= 1:
			return table[n-1]
		}
		pos := x * float64(n-1)
		idx := int(pos)
		frac := pos - float64(idx)
		return table[idx] + frac*(table[idx+1]-table[idx])
	}
}

type curve func(float64) float64

func safePow(x, exp float64) float64 {
	if x <= 0 {
		return 0
	}
	return math.Pow(x, exp)
}
func encodeSRGB(v float64) float64 {
	if math.IsNaN(v) || v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	if v <= 0.0031308 {
		return 12.92 * v
	}
	return 1.055*math.Pow(v, 1.0/2.4) - 0.055
}

type iccHeader struct {
	majorVersion int
	colorSpace   string
	pcs          string
}

func parseXYZ(data []byte) ([3]float64, error) {
	if len(data) < xyzDataOffset+xyzDataLen {
		return [3]float64{}, errMalformedXYZ
	}
	if string(data[0:4]) != sigXYZType {
		return [3]float64{}, errMalformedXYZ
	}
	return [3]float64{readS15Fixed16(data, xyzDataOffset), readS15Fixed16(data, xyzDataOffset+4), readS15Fixed16(data, xyzDataOffset+8)}, nil
}

const (
	sigPara       = "para"
	paraHeaderLen = 12
)

func parseParametric(data []byte) (curve, error) {
	if len(data) < paraHeaderLen {
		return nil, errMalformedTRC
	}
	funcType := binary.BigEndian.Uint16(data[8:10])
	if int(funcType) >= len(paraParamCounts) {
		return nil, errUnsupportedTRC
	}
	nParams := paraParamCounts[funcType]
	if int64(paraHeaderLen)+int64(nParams)*4 > int64(len(data)) {
		return nil, errMalformedTRC
	}
	params := make([]float64, nParams)
	for i := range params {
		params[i] = readS15Fixed16(data, paraHeaderLen+i*4)
	}
	return parametricCurve(funcType, params), nil
}

var (
	errMalformedTRC   = errors.New("icctransform: malformed TRC tag")
	errUnsupportedTRC = errors.New("icctransform: unsupported TRC curve type")
)

func gammaCurve(gamma float64) curve { return func(x float64) float64 { return safePow(x, gamma) } }

var errMalformedTagTable = errors.New("icctransform: malformed tag table")

func parseToneCurves(tags map[string][]byte) (toneCurves, error) {
	var tc toneCurves
	var err error
	if tc.red, err = parseTRC(tags[tagRTRC]); err != nil {
		return toneCurves{}, err
	}
	if tc.green, err = parseTRC(tags[tagGTRC]); err != nil {
		return toneCurves{}, err
	}
	if tc.blue, err = parseTRC(tags[tagBTRC]); err != nil {
		return toneCurves{}, err
	}
	return tc, nil
}

const (
	tagTableOffset = headerLen
	tagEntryLen    = 12
)

var paraParamCounts = [...]int{1, 3, 4, 5, 7}

func (t *Transform) Convert(r, g, b float64) (outR, outG, outB float64) {
	return t.EncodeLinear(t.redTRC(r), t.greenTRC(g), t.blueTRC(b))
}

type toneCurves struct{ red, green, blue curve }

func parseTagTable(profile []byte) (map[string][]byte, error) {
	count, ok := readUint32(profile, tagTableOffset)
	if !ok {
		return nil, errMalformedTagTable
	}
	entriesStart := int64(tagTableOffset) + 4
	if entriesStart+int64(count)*tagEntryLen > int64(len(profile)) {
		return nil, errMalformedTagTable
	}
	tags := make(map[string][]byte, len(wantedTags))
	for i := int64(0); i < int64(count); i++ {
		entry := int(entriesStart + i*tagEntryLen)
		sig := string(profile[entry : entry+4])
		if _, wanted := wantedTags[sig]; !wanted {
			continue
		}
		offset := binary.BigEndian.Uint32(profile[entry+4 : entry+8])
		size := binary.BigEndian.Uint32(profile[entry+8 : entry+12])
		end := int64(offset) + int64(size)
		if end > int64(len(profile)) {
			return nil, errMalformedTagTable
		}
		if _, seen := tags[sig]; !seen {
			tags[sig] = profile[offset:end]
		}
	}
	return tags, nil
}

var d50ToD65 = mat3{{0.9555766, -0.0230393, 0.0631636}, {-0.0282895, 1.0099416, 0.0210077}, {0.0122982, -0.0204830, 1.3299098}}

func parseColorants(tags map[string][]byte) (colorants, error) {
	var c colorants
	var err error
	if c.red, err = parseXYZ(tags[tagRXYZ]); err != nil {
		return colorants{}, err
	}
	if c.green, err = parseXYZ(tags[tagGXYZ]); err != nil {
		return colorants{}, err
	}
	if c.blue, err = parseXYZ(tags[tagBXYZ]); err != nil {
		return colorants{}, err
	}
	if wp, wpErr := parseXYZ(tags[tagWtpt]); wpErr == nil {
		c.whitePoint = wp
	}
	return c, nil
}
func parseTRC(data []byte) (curve, error) {
	if len(data) < 4 {
		return nil, errMalformedTRC
	}
	switch string(data[0:4]) {
	case sigCurv:
		return parseCurv(data)
	case sigPara:
		return parseParametric(data)
	default:
		return nil, errUnsupportedTRC
	}
}

var errMalformedXYZ = errors.New("icctransform: malformed XYZ tag")

const (
	headerLen       = 128
	signatureOffset = 36
)
const (
	tagRXYZ = "rXYZ"
	tagGXYZ = "gXYZ"
	tagBXYZ = "bXYZ"
	tagRTRC = "rTRC"
	tagGTRC = "gTRC"
	tagBTRC = "bTRC"
	tagWtpt = "wtpt"
)
