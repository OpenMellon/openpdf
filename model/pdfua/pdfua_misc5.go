package pdfua

import (
	"errors"
	"fmt"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
	"github.com/OpenMellon/openpdf/model/xmputil"
)

func writeXMP(doc *docutil.Document, std standardType, opts XmpOptions) error {
	catalog, ok := doc.FindCatalog()
	if !ok {
		return errors.New("pdfua: document catalog not found")
	}
	var xmpDoc *xmputil.Document
	if metaStream, ok := catalog.GetMetadata(); ok {
		loaded, err := xmputil.LoadDocument(streamData(metaStream))
		if err != nil {
			xmpDoc = xmputil.NewDocument()
		} else {
			xmpDoc = loaded
		}
	} else {
		xmpDoc = xmputil.NewDocument()
	}
	infoOpts := xmputil.PdfInfoOptions{InfoDict: doc.Info, PdfVersion: fmt.Sprintf("%d.%d", doc.Version.Major, doc.Version.Minor), Copyright: opts.Copyright, Overwrite: true}
	markInfo, ok := catalog.GetMarkInfo()
	if ok {
		marked, ok := core.GetBool(markInfo.Get("Marked"))
		if ok && bool(*marked) {
			infoOpts.Marked = true
		}
	}
	if err := xmpDoc.SetPdfInfo(&infoOpts); err != nil {
		return err
	}
	var err error
	if std.rev != 0 {
		err = xmpDoc.SetPdfUAIDWithRev(std.part, std.rev)
	} else {
		err = xmpDoc.SetPdfUAID(std.part)
	}
	if err != nil {
		return err
	}
	data, err := xmpDoc.MarshalIndent(opts.MarshalPrefix, opts.MarshalIndent)
	if err != nil {
		return err
	}
	return catalog.SetMetadata(data)
}

// XmpOptions are the options used when writing the PDF/UA XMP metadata.
type XmpOptions struct {
	// Copyright information.
	Copyright string

	// MarshalIndent defines marshaling indent of the XMP metadata.
	MarshalIndent string

	// MarshalPrefix defines marshaling prefix of the XMP metadata.
	MarshalPrefix string
}
