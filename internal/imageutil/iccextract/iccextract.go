// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package iccextract

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
)

func extractPNG(data []byte) []byte {
	const pngSignatureLen = 8
	pos := pngSignatureLen
	for pos+8 <= len(data) {
		length := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		chunkType := data[pos+4 : pos+8]
		dataStart := pos + 8
		if length < 0 || dataStart+length+4 > len(data) {
			return nil
		}
		chunk := data[dataStart : dataStart+length]
		if string(chunkType) == "iCCP" {
			return decodeICCPChunk(chunk)
		}
		if string(chunkType) == "IEND" {
			return nil
		}
		pos = dataStart + length + 4
	}
	return nil
}

var iccProfileTag = []byte("ICC_PROFILE\x00")

const (
	FormatUnknown Format = ""
	FormatJPEG    Format = "jpeg"
	FormatPNG     Format = "png"
	FormatTIFF    Format = "tiff"
)
const tagICCProfile = 34675

var tiffTypeSize = map[uint16]int{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1, 8: 2, 9: 4, 10: 8, 11: 4, 12: 8}

func Detect(data []byte) Format {
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return FormatJPEG
	case len(data) >= 8 && data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 && data[4] == 0x0D && data[5] == 0x0A && data[6] == 0x1A && data[7] == 0x0A:
		return FormatPNG
	case len(data) >= 4 && ((data[0] == 'I' && data[1] == 'I' && data[2] == 0x2A && data[3] == 0x00) || (data[0] == 'M' && data[1] == 'M' && data[2] == 0x00 && data[3] == 0x2A)):
		return FormatTIFF
	default:
		return FormatUnknown
	}
}

const maxProfileSize = 8 << 20

func extractJPEG(data []byte) []byte {
	chunks := map[int][]byte{}
	total := 0
	pos := 2
	for pos+1 < len(data) {
		if data[pos] != 0xFF {
			return nil
		}
		marker := data[pos+1]
		if marker == 0xFF {
			pos++
			continue
		}
		if marker == 0xD8 || marker == 0xD9 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			pos += 2
			continue
		}
		if marker == markerSOS {
			break
		}
		if pos+4 > len(data) {
			break
		}
		segLen := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		if segLen < 2 || pos+2+segLen > len(data) {
			return nil
		}
		payload := data[pos+4 : pos+2+segLen]
		if marker == markerAPP2 && len(payload) > len(iccProfileTag)+2 && bytes.HasPrefix(payload, iccProfileTag) {
			seq := int(payload[len(iccProfileTag)])
			count := int(payload[len(iccProfileTag)+1])
			part := payload[len(iccProfileTag)+2:]
			if seq >= 1 && count >= 1 {
				if total == 0 {
					total = count
				}
				if count == total && seq <= total {
					chunks[seq] = append([]byte(nil), part...)
				}
			}
		}
		pos += 2 + segLen
	}
	if total == 0 {
		return nil
	}
	var profile []byte
	for i := 1; i <= total; i++ {
		part, ok := chunks[i]
		if !ok {
			return nil
		}
		profile = append(profile, part...)
	}
	if len(profile) == 0 {
		return nil
	}
	return profile
}

const (
	markerAPP2 = 0xE2
	markerSOS  = 0xDA
)

type Format string

func decodeICCPChunk(chunk []byte) []byte {
	nameEnd := bytes.IndexByte(chunk, 0x00)
	if nameEnd < 0 || nameEnd > 79 {
		return nil
	}
	rest := chunk[nameEnd+1:]
	if len(rest) < 2 {
		return nil
	}
	if rest[0] != 0x00 {
		return nil
	}
	zr, err := zlib.NewReader(bytes.NewReader(rest[1:]))
	if err != nil {
		return nil
	}
	defer zr.Close()
	profile, err := io.ReadAll(io.LimitReader(zr, maxProfileSize+1))
	if err != nil {
		return nil
	}
	if len(profile) == 0 || len(profile) > maxProfileSize {
		return nil
	}
	return profile
}

func extractTIFF(data []byte) []byte {
	if len(data) < 8 {
		return nil
	}
	var order binary.ByteOrder
	switch {
	case data[0] == 'I' && data[1] == 'I':
		order = binary.LittleEndian
	case data[0] == 'M' && data[1] == 'M':
		order = binary.BigEndian
	default:
		return nil
	}
	ifdOffset := int(order.Uint32(data[4:8]))
	if ifdOffset < 8 || ifdOffset+2 > len(data) {
		return nil
	}
	entryCount := int(order.Uint16(data[ifdOffset : ifdOffset+2]))
	entriesStart := ifdOffset + 2
	if entriesStart+entryCount*12 > len(data) {
		return nil
	}
	for i := 0; i < entryCount; i++ {
		entry := entriesStart + i*12
		tag := order.Uint16(data[entry : entry+2])
		if tag != tagICCProfile {
			continue
		}
		typ := order.Uint16(data[entry+2 : entry+4])
		count := int(order.Uint32(data[entry+4 : entry+8]))
		elemSize, ok := tiffTypeSize[typ]
		if !ok || count <= 0 {
			return nil
		}
		byteLen := int64(count) * int64(elemSize)
		var valueOffset int
		if byteLen <= 4 {
			valueOffset = entry + 8
		} else {
			valueOffset = int(order.Uint32(data[entry+8 : entry+12]))
		}
		if valueOffset < 0 || int64(valueOffset)+byteLen > int64(len(data)) {
			return nil
		}
		return append([]byte(nil), data[valueOffset:valueOffset+int(byteLen)]...)
	}
	return nil
}

func Extract(data []byte) (icc []byte, format Format) {
	format = Detect(data)
	switch format {
	case FormatJPEG:
		return extractJPEG(data), FormatJPEG
	case FormatPNG:
		return extractPNG(data), FormatPNG
	case FormatTIFF:
		return extractTIFF(data), FormatTIFF
	default:
		return nil, FormatUnknown
	}
}
