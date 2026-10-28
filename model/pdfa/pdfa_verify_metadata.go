package pdfa

import (
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/xmputil"
)

func verifyMetadataPDFA23(r *model.CompliancePdfReader, std standardType, unused bool) (violations []ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return []ViolatedRule{newViolatedRule("6.6.2.1-1", "catalog not found.")}
	}
	metadata := dict.Get("Metadata")
	if metadata == nil {
		return []ViolatedRule{newViolatedRule("6.6.2.1-1", "The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream")}
	}
	metadataStream, ok := core.GetStream(metadata)
	if !ok {
		return []ViolatedRule{newViolatedRule("6.6.2.1-1", "The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream")}
	}
	xmp, err := xmputil.LoadDocument(metadataStream.Stream)
	if err != nil {
		violations = []ViolatedRule{newViolatedRule("6.6.2.1-4", "All metadata streams present in the PDF shall conform to the XMP Specification. All content of all XMP packets shall be well-formed")}
		if xmputil.PacketEncoding(metadataStream.Stream) != xmputil.EncodingUTF8 {
			violations = append(violations, newViolatedRule("6.6.2.1-5", "All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8"))
		}
		return violations
	}
	if xmp.Encoding() != xmputil.EncodingUTF8 {
		violations = append(violations, newViolatedRule("6.6.2.1-5", "All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8"))
	}
	seen := map[string]struct{}{}
	for _, prop := range parseXMPProperties(xmp.Packet()) {
		if isPredefinedNamespace(prop.Namespace) {
			continue
		}
		seen[prop.Namespace] = struct{}{}
	}
	found := true
	pdfaExtensionSchemas, err := xmp.GetPdfaExtensionSchemas()
	if err != nil {
		found = false
	} else {
		for s := range seen {
			var ok bool
			for i := range pdfaExtensionSchemas {
				if s == pdfaExtensionSchemas[i].NamespaceURI {
					ok = true
					break
				}
			}
			if !ok {
				found = false
				break
			}
		}
	}
	if !found {
		violations = append(violations, newViolatedRule("6.6.2.3.1-1", "All properties specified in XMP form shall use either the predefined schemas defined in the XMP Specification, ISO 19005-1 or this part of ISO 19005, or any extension schemas that comply with 6.6.2.3.2."))
	}
	if !validateXMPProperties(xmp.Packet(), pdfaExtensionSchemas, false) {
		violations = append(violations, newViolatedRule("6.6.2.3.1-2", "The value of an XMP property does not have the type its schema declares for it."))
	}
	pdfAID, ok := xmp.GetPdfAID()
	if !ok {
		violations = append(violations, newViolatedRule("6.6.4-1", "The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema."))
	} else {
		if pdfAID.Part != std.part {
			violations = append(violations, newViolatedRule("6.6.4-2", "The value of pdfaid:part shall be the part number of ISO 19005 to which the file conforms."))
		}
		if std.conformance == "A" && pdfAID.Conformance != "A" {
			violations = append(violations, newViolatedRule("6.6.4-3", "A Level A conforming file shall specify the value of pdfaid:conformance as A. A Level B conforming file shall specify the value of pdfaid:conformance as B. A Level U conforming file shall specify the value of pdfaid:conformance as U."))
		} else if std.conformance == "U" && (pdfAID.Conformance != "A" && pdfAID.Conformance != "U") {
			violations = append(violations, newViolatedRule("6.6.4-3", "A Level A conforming file shall specify the value of pdfaid:conformance as A. A Level B conforming file shall specify the value of pdfaid:conformance as B. A Level U conforming file shall specify the value of pdfaid:conformance as U."))
		} else if std.conformance == "B" && (pdfAID.Conformance != "A" && pdfAID.Conformance != "B" && pdfAID.Conformance != "U") {
			violations = append(violations, newViolatedRule("6.6.4-3", "A Level A conforming file shall specify the value of pdfaid:conformance as A. A Level B conforming file shall specify the value of pdfaid:conformance as B. A Level U conforming file shall specify the value of pdfaid:conformance as U."))
		}
	}
	return violations
}

func verifyMetadataPDFA1(r *model.CompliancePdfReader, std standardType, unused bool) (violations []ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return []ViolatedRule{newViolatedRule("6.7.2-1", "catalog not found.")}
	}
	metadata := dict.Get("Metadata")
	if metadata == nil {
		return []ViolatedRule{newViolatedRule("6.7.2-1", "no 'Metadata' key found in the document catalog."), newViolatedRule("6.7.3-1", "If [a document information dictionary appears in a document], then all of its entries that have analogous properties in predefined XMP schemas … shall also be embedded in the file in XMP form with equivalent values.")}
	}
	metadataStream, ok := core.GetStream(metadata)
	if !ok {
		return []ViolatedRule{newViolatedRule("6.7.2-2", "catalog 'Metadata' object is not a stream."), newViolatedRule("6.7.3-1", "If [a document information dictionary appears in a document], then all of its entries that have analogous properties in predefined XMP schemas … shall also be embedded in the file in XMP form with equivalent values.")}
	}
	if metadataStream.Get("Filter") != nil {
		violations = append(violations, newViolatedRule("6.7.2-2", "Metadata object stream dictionaries shall not contain the Filter key."))
	}
	xmp, err := xmputil.LoadDocument(metadataStream.Stream)
	if err != nil {
		return []ViolatedRule{newViolatedRule("6.7.9-1", "The metadata stream shall conform to XMP Specification and well formed PDFAExtension Schema for all extensions.")}
	}
	seen := map[string]struct{}{}
	for _, prop := range parseXMPProperties(xmp.Packet()) {
		if isPredefinedNamespace(prop.Namespace) {
			continue
		}
		seen[prop.Namespace] = struct{}{}
	}
	found := true
	pdfaExtensionSchemas, err := xmp.GetPdfaExtensionSchemas()
	if err != nil {
		found = false
	} else {
		for s := range seen {
			var ok bool
			for i := range pdfaExtensionSchemas {
				if s == pdfaExtensionSchemas[i].NamespaceURI {
					ok = true
					break
				}
			}
			if !ok {
				found = false
				break
			}
		}
	}
	if !found {
		violations = append(violations, newViolatedRule("6.7.9-2", "Properties specified in XMP form shall use either the predefined schemas defined in XMP Specification, or extension schemas that comply with XMP Specification."))
	}
	if !validateXMPProperties(xmp.Packet(), pdfaExtensionSchemas, true) {
		violations = append(violations, newViolatedRule("6.7.9-3", "The value of an XMP property does not have the type its schema declares for it."))
	}
	if pdfInfo, err := r.GetPdfInfo(); err == nil {
		violations = append(violations, verifyInfoMatchesXMP(pdfInfo, xmp)...)
	}
	pdfAID, ok := xmp.GetPdfAID()
	if !ok {
		violations = append(violations, newViolatedRule("6.7.11-1", "The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema."))
	} else {
		if pdfAID.Part != std.part {
			violations = append(violations, newViolatedRule("6.7.11-2", "The value of pdfaid:part shall be the part number of ISO 19005 to which the file conforms."))
		}
		if std.conformance == "A" && pdfAID.Conformance != "A" {
			violations = append(violations, newViolatedRule("6.7.11-3", "A Level A conforming file shall specify the value of pdfaid:conformance as A. A Level B conforming file shall specify the value of pdfaid:conformance as B."))
		} else if std.conformance == "B" && (pdfAID.Conformance != "A" && pdfAID.Conformance != "B") {
			violations = append(violations, newViolatedRule("6.7.11-3", "A Level A conforming file shall specify the value of pdfaid:conformance as A. A Level B conforming file shall specify the value of pdfaid:conformance as B."))
		}
	}
	return violations
}

func verifyMetadataPDFA4(r *model.CompliancePdfReader, std standardType, unused bool) (violations []ViolatedRule) {
	dict, ok := catalogDict(r)
	if !ok {
		return []ViolatedRule{newViolatedRule("6.7.2.1-1", "catalog not found.")}
	}
	metadata := dict.Get("Metadata")
	if metadata == nil {
		return []ViolatedRule{newViolatedRule("6.7.2.1", "The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream")}
	}
	metadataStream, ok := core.GetStream(metadata)
	if !ok {
		return []ViolatedRule{newViolatedRule("6.7.2.1-1", "The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream")}
	}
	xmp, err := xmputil.LoadDocument(metadataStream.Stream)
	if err != nil {
		violations = []ViolatedRule{newViolatedRule("6.7.2.1-4", "All content of all XMP packets located in any metadata stream present in the PDF shall be well-formed")}
		if xmputil.PacketEncoding(metadataStream.Stream) != xmputil.EncodingUTF8 {
			violations = append(violations, newViolatedRule("6.7.2.1-5", "All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8"))
		}
		return violations
	}
	if xmp.Encoding() != xmputil.EncodingUTF8 {
		violations = append(violations, newViolatedRule("6.7.2.1-5", "All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8"))
	}
	pdfAID, ok := xmp.GetPdfAID()
	if !ok {
		violations = append(violations, newViolatedRule("6.7.3-1", "The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema."))
	} else {
		if pdfAID.Part != std.part {
			violations = append(violations, newViolatedRule("6.7.3-2", "The value of pdfaid:part shall be the part number of ISO 19005 to which the file conforms."))
		}
		if std.conformance == "" && pdfAID.Conformance != "" {
			violations = append(violations, newViolatedRule("6.7.3-3", "A conforming PDF/A-4 file shall specify the value of pdfaid:conformance as nil. A Level E conforming file shall specify the value of pdfaid:conformance as E. A Level F conforming file shall specify the value of pdfaid:conformance as F."))
		} else if std.conformance == "E" && pdfAID.Conformance != "E" {
			violations = append(violations, newViolatedRule("6.7.3-3", "A conforming PDF/A-4 file shall specify the value of pdfaid:conformance as nil. A Level E conforming file shall specify the value of pdfaid:conformance as E. A Level F conforming file shall specify the value of pdfaid:conformance as F."))
		} else if std.conformance == "F" && pdfAID.Conformance != "F" {
			violations = append(violations, newViolatedRule("6.7.3-3", "A conforming PDF/A-4 file shall specify the value of pdfaid:conformance as nil. A Level E conforming file shall specify the value of pdfaid:conformance as E. A Level F conforming file shall specify the value of pdfaid:conformance as F."))
		}
		if pdfAID.Rev < 1000 || pdfAID.Rev > 9999 {
			violations = append(violations, newViolatedRule("6.7.3-5", "The value of pdfaid:rev shall be the four digit year."))
		}
	}
	return violations
}
