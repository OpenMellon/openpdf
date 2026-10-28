package core

import (
	"regexp"
)

type lookupResult struct {
	obj       PdfObject
	decrypted bool
}

var xrefLineRegexp = regexp.MustCompile("[\\r\\n]\\s*(xref)\\s*[\\r\\n]")

var xrefKeywordRegexp = regexp.MustCompile("\\s*xref\\s*")

var xrefEntryRegexp = regexp.MustCompile("(\\d+)\\s+(\\d+)\\s+([nf])\\s*$")

var xrefSubsectionRegexp = regexp.MustCompile("(\\d+)\\s+(\\d+)\\s*$")

type xrefType int

const (
	XrefTypeTableEntry   xrefType = iota
	XrefTypeObjectStream xrefType = iota
)

// XrefObject defines a cross reference entry which is a map between object number (with generation number) and the
// location of the actual object, either as a file offset (xref table entry), or as a location within an xref
// stream object (xref object stream).
type XrefObject struct {
	XType        xrefType
	ObjectNumber int
	Generation   int

	// For normal xrefs (defined by OFFSET)
	Offset int64

	// For xrefs to object streams.
	OsObjNumber int
	OsObjIndex  int
}

// XrefTable represents the cross references in a PDF, i.e. the table of
// objects and information where to access within the PDF file. Instances are
// treated as immutable snapshots once published via parser.setXrefs — repair
// paths build a new XrefTable rather than mutating an existing one.
type XrefTable struct {
	ObjectMap map[int]XrefObject
	sorted    []XrefObject
}

var eofRegexp = regexp.MustCompile("%%EOF?")

type eofMarker struct{ markerOffset, endOffset int64 }

type repairResult struct {
	xrefs *XrefTable
	err   error
}
