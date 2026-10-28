// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package fdf provides support for loading form field data from Forms Data Format (FDF) files
// and for exporting the field data of a PDF form to an FDF file.
package fdf

import (
	"errors"
	"io"
	"strconv"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

func (p *fdfParser) parseIndirectObject() (core.PdfObject, error) {
	ind := core.PdfIndirectObject{}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("-Read indirect obj")
	}
	peek, err := p.reader.Peek(20)
	if err != nil {
		common.Log.Debug("ERROR: Fail to read indirect obj")
		return &ind, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("(indirect obj peek \"%s\"", string(peek))
	}
	loc := reIndirectObj.FindStringSubmatchIndex(string(peek))
	if len(loc) < 6 {
		common.Log.Debug("ERROR: Unable to find object signature (%s)", string(peek))
		return &ind, errors.New("unable to detect indirect object signature")
	}
	p.reader.Discard(loc[0])
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Offsets % d", loc)
	}
	sigLen := loc[1] - loc[0]
	sig := make([]byte, sigLen)
	_, err = p.readAtLeast(sig, sigLen)
	if err != nil {
		common.Log.Debug("ERROR: unable to read - %s", err)
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("textline: %s", sig)
	}
	m := reIndirectObj.FindStringSubmatch(string(sig))
	if len(m) < 3 {
		common.Log.Debug("ERROR: Unable to find object signature (%s)", string(sig))
		return &ind, errors.New("unable to detect indirect object signature")
	}
	objNum, _ := strconv.Atoi(m[1])
	genNum, _ := strconv.Atoi(m[2])
	ind.ObjectNumber = int64(objNum)
	ind.GenerationNumber = int64(genNum)
	for {
		next, err := p.reader.Peek(2)
		if err != nil {
			return &ind, err
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Ind. peek: %s (% x)!", string(next), string(next))
		}
		if core.IsWhiteSpace(next[0]) {
			p.skipSpaces()
		} else if next[0] == '%' {
			p.skipComments()
		} else if (next[0] == '<') && (next[1] == '<') {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Call ParseDict")
			}
			ind.PdfObject, err = p.parseDict()
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("EOF Call ParseDict: %v", err)
			}
			if err != nil {
				return &ind, err
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Parsed dictionary... finished.")
			}
		} else if (next[0] == '/') || (next[0] == '(') || (next[0] == '[') || (next[0] == '<') {
			ind.PdfObject, err = p.parseObject()
			if err != nil {
				return &ind, err
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Parsed object ... finished.")
			}
		} else {
			if next[0] == 'e' {
				line, lineErr := p.readTextLine()
				if lineErr != nil {
					return nil, lineErr
				}
				if len(line) >= 6 && line[0:6] == "endobj" {
					break
				}
			} else if next[0] == 's' {
				next, _ = p.reader.Peek(10)
				if string(next[:6]) == "stream" {
					off := 6
					if len(next) > 6 {
						if core.IsWhiteSpace(next[off]) && next[off] != '\r' && next[off] != '\n' {
							common.Log.Debug("Non-conformant FDF not ending stream line properly with EOL marker")
							off++
						}
						switch next[off] {
						case '\r':
							off++
							if next[off] == '\n' {
								off++
							}
						case '\n':
							off++
						}
					}
					p.reader.Discard(off)
					dict, isDict := ind.PdfObject.(*core.PdfObjectDictionary)
					if !isDict {
						return nil, errors.New("stream object missing dictionary")
					}
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("Stream dict %s", dict)
					}
					lengthObj, isInt := dict.Get("Length").(*core.PdfObjectInteger)
					if !isInt {
						return nil, errors.New("stream length needs to be an integer")
					}
					length := *lengthObj
					if length < 0 {
						return nil, errors.New("stream needs to be longer than 0")
					}
					if int64(length) > p.fileSize {
						common.Log.Debug("ERROR: Stream length cannot be larger than file size")
						return nil, errors.New("invalid stream length, larger than file size")
					}
					data := make([]byte, length)
					_, err = p.readAtLeast(data, int(length))
					if err != nil {
						common.Log.Debug("ERROR stream (%d): %X", len(data), data)
						common.Log.Debug("ERROR: %v", err)
						return nil, err
					}
					stream := core.PdfObjectStream{
						Stream:              data,
						PdfObjectDictionary: ind.PdfObject.(*core.PdfObjectDictionary),
						PdfObjectReference: core.PdfObjectReference{
							ObjectNumber:     ind.ObjectNumber,
							GenerationNumber: ind.GenerationNumber,
						},
					}
					p.skipSpaces()
					p.reader.Discard(9)
					p.skipSpaces()
					return &stream, nil
				}
			}
			ind.PdfObject, err = p.parseObject()
			return &ind, err
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Returning indirect!")
	}
	return &ind, nil
}

func (p *fdfParser) parseFdfVersion() (int, int, error) {
	p.rs.Seek(0, io.SeekStart)
	n := 20
	head := make([]byte, n)
	p.rs.Read(head)
	m := reFdfVersion.FindStringSubmatch(string(head))
	if len(m) < 3 {
		fbMajor, fbMinor, err := p.seekFdfVersionTopDown()
		if err != nil {
			common.Log.Debug("Failed recovery - unable to find version")
			return 0, 0, err
		}
		return fbMajor, fbMinor, nil
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, err
	}
	minor, err := strconv.Atoi(m[2])
	if err != nil {
		return 0, 0, err
	}
	common.Log.Debug("Fdf version %d.%d", major, minor)
	return major, minor, nil
}
