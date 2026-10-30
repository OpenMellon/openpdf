package model

import (
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
	_ "image/gif"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/textencoding"
)

func parseCIDWidths(obj core.PdfObject) (map[textencoding.CharCode]float64, error) {
	if obj == nil {
		return nil, nil
	}
	arr, ok := core.GetArray(obj)
	if !ok {
		return nil, nil
	}
	valByCode := map[textencoding.CharCode]float64{}
	arrLen := arr.Len()
	for i := 0; i < arrLen-1; i++ {
		obj := core.TraceToDirectObject(arr.Get(i))
		n, ok := core.GetIntVal(obj)
		if !ok {
			return nil, fmt.Errorf("bad font W obj0: i=%d %#v", i, obj)
		}
		i++
		if i > arrLen-1 {
			return nil, fmt.Errorf("bad font W array: arr2=%+v", arr)
		}
		obj2 := core.TraceToDirectObject(arr.Get(i))
		switch obj2.(type) {
		case *core.PdfObjectArray:
			arr, _ := core.GetArray(obj2)
			if floats, err := arr.ToFloat64Array(); err == nil {
				for i := 0; i < len(floats); i++ {
					valByCode[textencoding.CharCode(n+i)] = floats[i]
				}
			} else {
				return nil, fmt.Errorf("bad font W array obj1: i=%d %#v", i, obj2)
			}
		case *core.PdfObjectInteger:
			count, ok := core.GetIntVal(obj2)
			if !ok {
				return nil, fmt.Errorf("bad font W int obj1: i=%d %#v", i, obj2)
			}
			i++
			if i > arrLen-1 {
				return nil, fmt.Errorf("bad font W array: arr2=%+v", arr)
			}
			obj := arr.Get(i)
			f, err := core.GetNumberAsFloat(obj)
			if err != nil {
				return nil, fmt.Errorf("bad font W int obj2: i=%d %#v", i, obj)
			}
			for i := n; i <= count; i++ {
				valByCode[textencoding.CharCode(i)] = f
			}
		default:
			return nil, fmt.Errorf("bad font W obj1 type: i=%d %#v", i, obj2)
		}
	}
	return valByCode, nil
}

func parseDefaultAppearance(s string) (s2 string, f float64, col PdfColor) {
	if s == "" {
		return s2, f, col
	}
	var strs []string
	fn := func(n int) ([]string, bool) {
		if len(strs) < n {
			return nil, false
		}
		strs2 := append([]string(nil), strs[len(strs)-n:]...)
		strs = strs[:len(strs)-n]
		return strs2, true
	}
	fn2 := func(strs []string) ([]float64, bool) {
		floats := make([]float64, len(strs))
		for i, str := range strs {
			f, err := strconv.ParseFloat(str, 64)
			if err != nil {
				return nil, false
			}
			floats[i] = f
		}
		return floats, true
	}
	for field := range strings.FieldsSeq(s) {
		switch field {
		case "Tf":
			strs, ok := fn(2)
			if !ok {
				continue
			}
			if strings.HasPrefix(strs[0], "/") {
				s2 = strs[0][1:]
			}
			if v, err := strconv.ParseFloat(strs[1], 64); err == nil {
				f = v
			}
		case "g", "G":
			strs, ok := fn(1)
			if !ok {
				continue
			}
			floats, ok := fn2(strs)
			if !ok {
				continue
			}
			if col2, err := NewPdfColorspaceDeviceGray().ColorFromFloats(floats); err == nil {
				col = col2
			}
		case "rg", "RG":
			strs, ok := fn(3)
			if !ok {
				continue
			}
			floats, ok := fn2(strs)
			if !ok {
				continue
			}
			if col2, err := NewPdfColorspaceDeviceRGB().ColorFromFloats(floats); err == nil {
				col = col2
			}
		case "k", "K":
			strs, ok := fn(4)
			if !ok {
				continue
			}
			floats, ok := fn2(strs)
			if !ok {
				continue
			}
			if col2, err := NewPdfColorspaceDeviceCMYK().ColorFromFloats(floats); err == nil {
				col = col2
			}
		default:
			strs = append(strs, field)
		}
	}
	return s2, f, col
}

func parseType1Encoding(s string) (map[textencoding.CharCode]textencoding.GlyphName, error) {
	split := strings.Split(s, "\n")
	glyphByCode := make(map[textencoding.CharCode]textencoding.GlyphName)
	for _, s := range split {
		stringSubmatch := type1EncodingEntryRegexp.FindStringSubmatch(s)
		if stringSubmatch == nil {
			continue
		}
		s2, s3 := stringSubmatch[1], stringSubmatch[2]
		atoi, err := strconv.ParseUint(s2, 10, 16)
		if err != nil {
			common.Log.Debug("ERROR: Bad encoding line. %q", s)
			return nil, core.ErrTypeError
		}
		glyphByCode[textencoding.CharCode(atoi)] = textencoding.GlyphName(s3)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("getEncodings: keyValues=%#v", glyphByCode)
	}
	return glyphByCode, nil
}

func parseType1Dictionary(s string) map[string]string {
	split := newlineRegexp.Split(s, -1)
	textByKey := map[string]string{}
	for _, s := range split {
		stringSubmatch := type1DefLineRegexp.FindStringSubmatch(s)
		if stringSubmatch == nil {
			continue
		}
		s, s2 := stringSubmatch[1], stringSubmatch[2]
		textByKey[s] = s2
	}
	return textByKey
}

func parseByteRange(arr *core.PdfObjectArray, n int64) ([]int64, error) {
	arrLen := arr.Len()
	if arrLen < 2 || arrLen%2 != 0 {
		return nil, fmt.Errorf("invalid ByteRange length %d", arrLen)
	}
	nums := make([]int64, 0, arrLen)
	for i := 0; i < arrLen; i++ {
		n, err := core.GetNumberAsInt64(arr.Get(i))
		if err != nil {
			return nil, fmt.Errorf("invalid ByteRange entry %d: %v", i, err)
		}
		if n < 0 {
			return nil, fmt.Errorf("negative ByteRange entry %d", i)
		}
		nums = append(nums, n)
	}
	for i := 0; i < arrLen; i += 2 {
		if nums[i] > n || nums[i+1] > n-nums[i] {
			return nil, fmt.Errorf("ByteRange pair (%d, %d) outside of file", nums[i], nums[i+1])
		}
	}
	return nums, nil
}

func parseCertificates(cert []*core.PdfObjectStream) []*x509.Certificate {
	var certs []*x509.Certificate
	for _, stream := range decodeStreams(cert) {
		certificate, err := x509.ParseCertificate(stream)
		if err != nil {
			common.Log.Debug("ERROR: skipping unparsable DSS certificate: %v", err)
			continue
		}
		certs = append(certs, certificate)
	}
	return certs
}

func parseCRL(data []byte) *parsedCRL {
	revocationList, err := x509.ParseRevocationList(data)
	if err != nil {
		return nil
	}
	crl := &parsedCRL{RevocationList: revocationList}
	for _, extension := range revocationList.Extensions {
		switch {
		case extension.Id.Equal(oidAuthorityKeyIdentifier), extension.Id.Equal(oidCRLNumber):
		case extension.Id.Equal(oidDeltaCRLIndicator):
			if crl.number != nil {
				return nil
			}
			if data, err := asn1.Unmarshal(extension.Value, &crl.number); err != nil || len(data) > 0 || crl.number.Sign() < 0 {
				return nil
			}
		case extension.Id.Equal(oidIssuingDistributionPoint):
			if crl.idp != nil {
				return nil
			}
			crl.idp, crl.idpRaw = &issuingDistributionPoint{}, extension.Value
			if data, err := asn1.Unmarshal(extension.Value, crl.idp); err != nil || len(data) > 0 {
				return nil
			}
		default:
			if extension.Critical {
				return nil
			}
		}
	}
	ok := crl.idp != nil && crl.idp.IndirectCRL
	for _, revokedCertificateEntry := range revocationList.RevokedCertificateEntries {
		for _, extension := range revokedCertificateEntry.Extensions {
			switch {
			case extension.Id.Equal(oidCertificateIssuer):
				if !ok {
					return nil
				}
			case extension.Id.Equal(oidCRLReason), extension.Id.Equal(oidInvalidityDate):
			default:
				if extension.Critical {
					return nil
				}
			}
		}
	}
	return crl
}

func parseMatte(arr *core.PdfObjectArray) (float64, error) {
	floats, err := arr.ToFloat64Array()
	if err != nil {
		common.Log.Debug("Bad Matte array: matte=%s err=%v", arr, err)
	}
	switch len(floats) {
	case 1:
		return floats[0], nil
	case 3:
		deviceRGBCS := PdfColorspaceDeviceRGB{}
		col, err := deviceRGBCS.ColorFromFloats(floats)
		if err != nil {
			return 0.0, err
		}
		return col.(*PdfColorDeviceRGB).ToGray().Val(), nil
	case 4:
		deviceCMYKCS := PdfColorspaceDeviceCMYK{}
		col, err := deviceCMYKCS.ColorFromFloats(floats)
		if err != nil {
			return 0.0, err
		}
		colorToRGB, err := deviceCMYKCS.ColorToRGB(col.(*PdfColorDeviceCMYK))
		if err != nil {
			return 0.0, err
		}
		return colorToRGB.(*PdfColorDeviceRGB).ToGray().Val(), nil
	}
	err = errors.New("bad Matte color")
	common.Log.Error("toGray: matte=%s err=%v", arr, err)
	return 0.0, err
}
