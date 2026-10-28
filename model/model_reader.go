package model

import (
	_ "image/gif"
)

// ReaderToWriterOpts options used to generate a PdfWriter.
type ReaderToWriterOpts struct {
	SkipAcroForm          bool
	SkipInfo              bool
	SkipNameDictionary    bool
	SkipNamedDests        bool
	SkipOCProperties      bool
	SkipOutlines          bool
	SkipPageLabels        bool
	SkipRotation          bool
	SkipMetadata          bool
	SkipMarkInfo          bool
	SkipViewerPreferences bool
	SkipLanguage          bool
	SkipPieceInfo         bool

	// SkipObjectCopy bypasses the writer's defensive deep copy of the source
	// object graph (see PdfWriter.SetSkipObjectCopy). It reduces memory use and
	// write time for large documents, but consumes the source: the reader and
	// its objects MUST NOT be used after the resulting writer's Write call.
	SkipObjectCopy      bool
	PageProcessCallback PageProcessCallback
}
