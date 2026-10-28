// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package jbig2

import (
	"sort"

	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder"
	"github.com/OpenMellon/openpdf/internal/jbig2/document"
	"github.com/OpenMellon/openpdf/internal/jbig2/document/segments"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

func DecodeGlobals(encoded []byte) (Globals, error) {
	const funcName = "DecodeGlobals"
	r := bitwise.NewReader(encoded)
	doc, err := document.DecodeDocument(r, nil)
	if err != nil {
		return nil, jbig2errors.Wrap(err, funcName, "")
	}
	if doc.GlobalSegments == nil || (doc.GlobalSegments.Segments == nil) {
		return nil, jbig2errors.Error(funcName, "no global segments found")
	}
	globals := Globals{}
	for _, header := range doc.GlobalSegments.Segments {
		globals[int(header.SegmentNumber)] = header
	}
	return globals, nil
}

func (g Globals) ToDocumentGlobals() *document.Globals {
	if g == nil {
		return nil
	}
	headers := []*segments.Header{}
	for _, header := range g {
		headers = append(headers, header)
	}
	sort.Slice(headers, func(i, j int) bool { return headers[i].SegmentNumber < headers[j].SegmentNumber })
	return &document.Globals{Segments: headers}
}

type Globals map[int]*segments.Header

func DecodeBytes(encoded []byte, parameters decoder.Parameters, globals ...Globals) ([]byte, error) {
	var g Globals
	if len(globals) > 0 {
		g = globals[0]
	}
	dec, err := decoder.Decode(encoded, parameters, g.ToDocumentGlobals())
	if err != nil {
		return nil, err
	}
	return dec.DecodeNextPage()
}
