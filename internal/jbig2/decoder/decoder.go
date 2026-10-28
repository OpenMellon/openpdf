// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package decoder

import (
	"image"

	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	"github.com/OpenMellon/openpdf/internal/jbig2/document"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

func (d *Decoder) DecodeNextPage() ([]byte, error) {
	d.currentPage++
	pageNumber := d.currentPage
	return d.decodePage(pageNumber)
}

type Decoder struct {
	reader      *bitwise.Reader
	document    *document.Document
	currentPage int
	params      Parameters
}

func (d *Decoder) decodePage(pageNumber int) ([]byte, error) {
	const processName = "decodePage"
	if pageNumber < 0 {
		return nil, jbig2errors.Errorf(processName, "invalid page number: '%d'", pageNumber)
	}
	if pageNumber > int(d.document.NumberOfPages) {
		return nil, jbig2errors.Errorf(processName, "page: '%d' not found in the decoder", pageNumber)
	}
	page, err := d.document.GetPage(pageNumber)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	bm, err := page.GetBitmap()
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	bm.InverseData()
	if !d.params.UnpaddedData {
		return bm.Data, nil
	}
	return bm.GetUnpaddedData()
}

func Decode(input []byte, parameters Parameters, globals *document.Globals) (*Decoder, error) {
	r := bitwise.NewReader(input)
	doc, err := document.DecodeDocument(r, globals)
	if err != nil {
		return nil, err
	}
	return &Decoder{reader: r, document: doc, params: parameters}, nil
}

func (d *Decoder) DecodePageImage(pageNumber int) (image.Image, error) {
	const processName = "decoder.DecodePageImage"
	img, err := d.decodePageImage(pageNumber)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	return img, nil
}

type Parameters struct {
	UnpaddedData bool
	Color        bitmap.Color
}

func (d *Decoder) DecodePage(pageNumber int) ([]byte, error) { return d.decodePage(pageNumber) }
func (d *Decoder) PageNumber() (int, error) {
	const processName = "Decoder.PageNumber"
	if d.document == nil {
		return 0, jbig2errors.Error(processName, "decoder not initialized yet")
	}
	return int(d.document.NumberOfPages), nil
}

func (d *Decoder) decodePageImage(pageNumber int) (image.Image, error) {
	const processName = "decodePageImage"
	if pageNumber < 0 {
		return nil, jbig2errors.Errorf(processName, "invalid page number: '%d'", pageNumber)
	}
	if pageNumber > int(d.document.NumberOfPages) {
		return nil, jbig2errors.Errorf(processName, "page: '%d' not found in the decoder", pageNumber)
	}
	page, err := d.document.GetPage(pageNumber)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	bm, err := page.GetBitmap()
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	bm.InverseData()
	return bm.ToImage(), nil
}
