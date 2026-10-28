package model

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
	_ "image/gif"
	"math/big"
	"regexp"
	"slices"
	"sort"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"golang.org/x/crypto/ocsp"
)

func newNames() *Names { return &Names{container: core.MakeIndirectObject(core.MakeDict())} }

type parsedCRL struct {
	*x509.RevocationList
	number *big.Int
	idp    *issuingDistributionPoint
	idpRaw []byte
	memo   map[[32]byte]bool
}

func padBytes(data []byte, n int) []byte {
	if len(data) >= n {
		return data
	}
	data2 := make([]byte, n)
	count := copy(data2, data)
	for i := count; i < n; i++ {
		data2[i] = ' '
	}
	return data2
}

const (
	NonFullScreenPageModeUseNone     NonFullScreenPageMode = "UseNone"
	NonFullScreenPageModeUseOutlines NonFullScreenPageMode = "UseOutlines"
	NonFullScreenPageModeUseThumbs   NonFullScreenPageMode = "UseThumbs"
	NonFullScreenPageModeUseOC       NonFullScreenPageMode = "UseOC"
	DirectionL2R                     Direction             = "L2R"
	DirectionR2L                     Direction             = "R2L"
	PageBoundaryMediaBox             PageBoundary          = "MediaBox"
	PageBoundaryCropBox              PageBoundary          = "CropBox"
	PageBoundaryBleedBox             PageBoundary          = "BleedBox"
	PageBoundaryTrimBox              PageBoundary          = "TrimBox"
	PageBoundaryArtBox               PageBoundary          = "ArtBox"
	PrintScalingNone                 PrintScaling          = "None"
	PrintScalingAppDefault           PrintScaling          = "AppDefault"
	DuplexNone                       Duplex                = "none"
	DuplexSimplex                    Duplex                = "Simplex"
	DuplexFlipShortEdge              Duplex                = "DuplexFlipShortEdge"
	DuplexFlipLongEdge               Duplex                = "DuplexFlipLongEdge"
)

type ocspAnswer struct {
	*ocsp.Response
	online     bool
	authorised bool
}

func newNamesFromPdfObject(names core.PdfObject) *Names {
	names2 := newNames()
	dict := names.Direct().(*core.PdfObjectDictionary)
	if dests := dict.Get("Dests"); dests != nil {
		names2.Dests = dests.Direct().(*core.PdfObjectDictionary)
	}
	if ap := dict.Get("AP"); ap != nil {
		names2.AP = ap.Direct().(*core.PdfObjectDictionary)
	}
	if javaScript := dict.Get("JavaScript"); javaScript != nil {
		names2.JavaScript = javaScript.Direct().(*core.PdfObjectDictionary)
	}
	if pages := dict.Get("Pages"); pages != nil {
		names2.Pages = pages.Direct().(*core.PdfObjectDictionary)
	}
	if templates := dict.Get("Templates"); templates != nil {
		names2.Templates = templates.Direct().(*core.PdfObjectDictionary)
	}
	if ids := dict.Get("IDS"); ids != nil {
		names2.IDS = ids.Direct().(*core.PdfObjectDictionary)
	}
	if urls := dict.Get("URLS"); urls != nil {
		names2.URLS = urls.Direct().(*core.PdfObjectDictionary)
	}
	if embeddedFiles := dict.Get("EmbeddedFiles"); embeddedFiles != nil {
		names2.EmbeddedFiles = embeddedFiles.Direct().(*core.PdfObjectDictionary)
	}
	if alternatePresentations := dict.Get("AlternatePresentations"); alternatePresentations != nil {
		names2.AlternatePresentations = alternatePresentations.Direct().(*core.PdfObjectDictionary)
	}
	if renditions := dict.Get("Renditions"); renditions != nil {
		names2.Renditions = renditions.Direct().(*core.PdfObjectDictionary)
	}
	return names2
}

var newlineRegexp = regexp.MustCompile("[\\n\\r]+")

// Names represents a PDF name tree.
//
// Ref: PDF32000_2008 chapter 7.7.4.
type Names struct {
	container *core.PdfIndirectObject

	// Dests is a name tree mapping name string to destinations.
	Dests *core.PdfObjectDictionary

	// AP is a name tree mapping name strings to annotation appearance streams.
	AP *core.PdfObjectDictionary

	// JavaScript is a name tree mapping name strings to JavaScript actions.
	JavaScript *core.PdfObjectDictionary

	// Pages is a name tree mapping name strings to visible pages for use in interactive forms.
	Pages *core.PdfObjectDictionary

	// Templates is a name tree mapping name strings to invisible (template) pages for use in interactive forms.
	Templates *core.PdfObjectDictionary

	// IDS is a name tree mapping digital identifies to Web Capture content sets.
	IDS *core.PdfObjectDictionary

	// URLS is a name tree mapping URLs to Web Capture content sets.
	URLS *core.PdfObjectDictionary

	// EmbeddedFiles is a name tree mapping name strings to file specifications for embedded file streams.
	EmbeddedFiles *core.PdfObjectDictionary

	// AlternatePresentations is a name tree mapping name strings to alternate presentations.
	AlternatePresentations *core.PdfObjectDictionary

	// Renditions is a name tree mapping name strings (which shall have Unicode encoding) to rendition objects.
	Renditions *core.PdfObjectDictionary
}

func ocspResponderAuthorizedByIssuer(resp *ocsp.Response, cert, cert2 *x509.Certificate) bool {
	certificate := resp.Certificate
	if certificate == nil || certificate.Equal(cert2) {
		return true
	}
	if certificate.Equal(cert) || !hasExtKeyUsage(certificate, x509.ExtKeyUsageOCSPSigning) {
		return false
	}
	return !resp.ProducedAt.Before(certificate.NotBefore) && !resp.ProducedAt.After(certificate.NotAfter)
}

type (
	ocspEvidence struct {
		raw    []byte
		digest [32]byte
		online bool
	}
	crlEvidence struct {
		*parsedCRL
		online bool
	}
)

func (c *parsedCRL) signedBy(cert *x509.Certificate, data [32]byte) bool {
	ok, found := c.memo[data]
	if !found {
		ok = c.CheckSignatureFrom(cert) == nil
		if c.memo == nil {
			c.memo = map[[32]byte]bool{}
		}
		c.memo[data] = ok
	}
	return ok
}

func objectNumbers(obj core.PdfObject) (int64, int64) {
	switch v := obj.(type) {
	case *core.PdfIndirectObject:
		return v.ObjectNumber, v.GenerationNumber
	case *core.PdfObjectStream:
		return v.ObjectNumber, v.GenerationNumber
	case *core.PdfObjectStreams:
		return v.ObjectNumber, v.GenerationNumber
	}
	return 0, 0
}

func (c *parsedCRL) covers(cert *x509.Certificate) bool {
	idp := c.idp
	if idp == nil {
		return true
	}
	if idp.OnlyContainsAttributeCerts || idp.OnlySomeReasons.BitLength > 0 {
		return false
	}
	ok := cert.BasicConstraintsValid && cert.IsCA
	if idp.OnlyContainsUserCerts && ok || idp.OnlyContainsCACerts && !ok {
		return false
	}
	if len(idp.DistributionPoint.RelativeName.FullBytes) > 0 {
		return false
	}
	if len(idp.DistributionPoint.FullName) == 0 {
		return true
	}
	for _, fullName := range idp.DistributionPoint.FullName {
		if fullName.Class != asn1.ClassContextSpecific || fullName.Tag != 6 {
			continue
		}
		if slices.Contains(cert.CRLDistributionPoints, string(fullName.Bytes)) {
			return true
		}
	}
	return false
}

// ToPdfObject generates a PdfObject representation of the Names struct.
func (n *Names) ToPdfObject() core.PdfObject {
	indirect := n.container
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("Invalid object")
		return indirect
	}
	if n.EmbeddedFiles != nil {
		dict.Set("EmbeddedFiles", core.MakeIndirectObject(n.EmbeddedFiles))
	}
	return indirect
}

func ocspEvidenceFromDER(dataList [][]byte) []ocspEvidence {
	ocsps := make([]ocspEvidence, 0, len(dataList))
	for _, data := range dataList {
		ocsps = append(ocsps, newOCSPEvidence(data, false))
	}
	return ocsps
}

// NonFullScreenPageMode represents the document’s page mode when exiting
// full-screen mode.
type NonFullScreenPageMode string

var (
	oidAuthorityKeyIdentifier = asn1.ObjectIdentifier{2, 5, 29, 35}
	oidCRLNumber              = asn1.ObjectIdentifier{2, 5, 29, 20}
	oidCRLReason              = asn1.ObjectIdentifier{2, 5, 29, 21}
	oidInvalidityDate         = asn1.ObjectIdentifier{2, 5, 29, 24}
)

// Optimizer is the interface that performs optimization of PDF object structure for output writing.
//
// Optimize receives a slice of input `objects`, performs optimization, including removing, replacing objects and
// output the optimized slice of objects.
type Optimizer interface {
	Optimize(objs []core.PdfObject) ([]core.PdfObject, error)
}

func ocspResponderAuthorized(resp *ocsp.Response, cert *x509.Certificate) bool {
	certificate := resp.Certificate
	if certificate == nil || certificate.Equal(cert) {
		return true
	}
	if resp.ThisUpdate.After(resp.ProducedAt) {
		return false
	}
	return resp.NextUpdate.IsZero() || !resp.NextUpdate.After(certificate.NotAfter)
}

var (
	oidDeltaCRLIndicator        = asn1.ObjectIdentifier{2, 5, 29, 27}
	oidIssuingDistributionPoint = asn1.ObjectIdentifier{2, 5, 29, 28}
	oidCertificateIssuer        = asn1.ObjectIdentifier{2, 5, 29, 29}
)

func (n *Names) addEmbeddedFile(file *EmbeddedFile) error {
	if n.EmbeddedFiles == nil {
		n.EmbeddedFiles = core.MakeDict()
		n.EmbeddedFiles.Set("Names", core.MakeArray())
	}
	filespec := NewPdfFileSpecFromEmbeddedFile(file)
	names := n.EmbeddedFiles.Get("Names")
	arr, ok := names.(*core.PdfObjectArray)
	if !ok {
		return errors.New("Invalid Names array")
	}
	type FileSpecMap struct {
		name     string
		filespec *PdfFilespec
	}
	fileSpecMaps := []FileSpecMap{}
	for i := 0; i < len(arr.Elements()); i += 2 {
		if i%2 == 0 {
			obj := arr.Get(i)
			if obj != nil {
				str := obj.(*core.PdfObjectString)
				obj := arr.Get(i + 1)
				filespec, err := NewPdfFilespecFromObj(obj)
				if err != nil {
					return err
				}
				fileSpecMaps = append(fileSpecMaps, FileSpecMap{name: str.String(), filespec: filespec})
			}
		}
	}
	fileSpecMaps = append(fileSpecMaps, FileSpecMap{name: file.Name, filespec: filespec})
	sort.Slice(fileSpecMaps, func(n, count int) bool { return fileSpecMaps[n].name < fileSpecMaps[count].name })
	arr = core.MakeArray()
	for _, fileSpecMap := range fileSpecMaps {
		arr.Append(core.MakeString(fileSpecMap.name))
		arr.Append(fileSpecMap.filespec.ToPdfObject())
	}
	n.EmbeddedFiles.Set("Names", arr)
	return nil
}

type objectStreamsState struct {
	members         []*core.PdfIndirectObject
	packedObjNums   []int64
	streams         *core.PdfObjectStreams
	streamObjNum    int64
	mainXrefObjNum  int64
	firstXrefObjNum int64
}

func (c *parsedCRL) newerThan(crl *parsedCRL) bool {
	if c.Number != nil && crl.Number != nil && bytes.Equal(c.idpRaw, crl.idpRaw) {
		return c.Number.Cmp(crl.Number) > 0
	}
	return c.ThisUpdate.After(crl.ThisUpdate)
}

const noTrustRootsMessage = "no trust roots configured"

func (c *parsedCRL) isDelta() bool { return c.number != nil }

func newOCSPEvidence(body []byte, ok bool) ocspEvidence {
	return ocspEvidence{raw: body, digest: sha256.Sum256(body), online: ok}
}

func (c *parsedCRL) entryFor(cert *x509.Certificate) *x509.RevocationListEntry {
	if c.idp != nil && c.idp.OnlyContainsAttributeCerts {
		return nil
	}
	ok := true
	for i := range c.RevokedCertificateEntries {
		revokedCertificateEntry := &c.RevokedCertificateEntries[i]
		for _, extension := range revokedCertificateEntry.Extensions {
			if extension.Id.Equal(oidCertificateIssuer) {
				ok = matchesAuthorityCertIssuer(extension.Value, cert.RawIssuer)
			}
		}
		if ok && revokedCertificateEntry.SerialNumber.Cmp(cert.SerialNumber) == 0 {
			return revokedCertificateEntry
		}
	}
	return nil
}

func pdfObjectToString(obj core.PdfObject) (string, error) {
	obj = core.TraceToDirectObject(obj)
	switch v := obj.(type) {
	case *core.PdfObjectString:
		return v.Str(), nil
	case *core.PdfObjectStream:
		stream, err := core.DecodeStream(v)
		if err != nil {
			return "", err
		}
		return string(stream), nil
	}
	return "", fmt.Errorf("invalid content stream object holder (%T)", obj)
}

func partitionObjectNumbers(writer *PdfWriter, layout *linearizationLayout, objByNum map[int64]core.PdfObject) (nums, nums2 []int64) {
	for _, mainSection := range layout.mainSection {
		obj := objByNum[mainSection]
		if obj == nil {
			nums2 = append(nums2, mainSection)
			continue
		}
		if canStoreInObjectStream(obj, writer) {
			nums = append(nums, mainSection)
		} else {
			nums2 = append(nums2, mainSection)
		}
	}
	return nums, nums2
}
