package model

import (
	"crypto/x509"
	"encoding/asn1"
	"fmt"
	_ "image/gif"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model/sigutil"
	"golang.org/x/crypto/ocsp"
)

// LTV represents an LTV (Long-Term Validation) client. It is used to LTV
// enable signatures by adding validation and revocation data (certificate,
// OCSP and CRL information) to the DSS dictionary of a PDF document.
//
// LTV is added through the DSS by:
//   - Adding certificates, OCSP and CRL information in the global scope of the
//     DSS. The global data is used for validating any of the signatures present
//     in the document.
//   - Adding certificates, OCSP and CRL information for a single signature,
//     through an entry in the VRI dictionary of the DSS. The added data is used
//     for validating that particular signature only. This is the recommended
//     method for adding validation data for a signature. However, this is not
//     possible in the same revision the signature is applied. Validation data
//     for a signature is added based on the Contents entry of the signature,
//     which is known only after the revision is written. Even if the Contents
//     are known (e.g. when signing externally), updating the DSS at that point
//     would invalidate the calculated signature. As a result, if adding LTV
//     in the same revision is a requirement, use the first method.
//     See LTV.EnableChain.
//
// The client applies both methods, when possible.
//
// If `LTV.SkipExisting` is set to true (the default), validations are
// not added for signatures which are already present in the VRI entry of the
// document's DSS dictionary.
type LTV struct {
	// CertClient is the client used to retrieve certificates.
	CertClient *sigutil.CertClient

	// OCSPClient is the client used to retrieve OCSP validation information.
	OCSPClient *sigutil.OCSPClient

	// CRLClient is the client used to retrieve CRL validation information.
	CRLClient *sigutil.CRLClient

	// SkipExisting specifies whether existing signature validations
	// should be skipped.
	SkipExisting bool
	appender     *PdfAppender
	dss          *DSS
}

func (l *LTV) getOCSPs(certs []*x509.Certificate, certByKey map[string]*x509.Certificate) ([][]byte, error) {
	dataList := make([][]byte, 0, len(certs))
	for _, cert := range certs {
		for _, ocspServer := range cert.OCSPServer {
			if l.CertClient.IsCA(cert) {
				continue
			}
			cert2, ok := certByKey[cert.Issuer.CommonName]
			if !ok {
				common.Log.Debug("WARN: Skipping OCSP request: issuer certificate not found")
				continue
			}
			_, data, err := l.OCSPClient.MakeRequest(ocspServer, cert, cert2)
			if err != nil {
				common.Log.Debug("WARN: OCSP request error: %v", err)
				continue
			}
			dataList = append(dataList, data)
		}
	}
	return dataList, nil
}

// EnableChain adds the specified certificate chain and validation data (OCSP
// and CRL information) for it to the global scope of the document DSS. The
// added data is used for validating any of the signatures present in the
// document. The LTV client attempts to build the certificate chain up to a
// trusted root by downloading any missing certificates.
func (l *LTV) EnableChain(chain []*x509.Certificate) error { return l.enable(nil, chain, "") }

// Enable LTV enables the specified signature. The signing certificate
// chain is extracted from the signature dictionary. Optionally, additional
// certificates can be specified through the `extraCerts` parameter.
// The LTV client attempts to build the certificate chain up to a trusted root
// by downloading any missing certificates.
func (l *LTV) Enable(sig *PdfSignature, extraCerts []*x509.Certificate) error {
	if err := l.validateSig(sig); err != nil {
		return err
	}
	s, err := signatureContentsDigestHex(sig)
	if err != nil {
		return err
	}
	if _, ok := l.dss.VRI[s]; ok && l.SkipExisting {
		return nil
	}
	certs, err := sig.GetCerts()
	if err != nil {
		return err
	}
	return l.enable(certs, extraCerts, s)
}

// EnableByName LTV enables the signature dictionary of the PDF AcroForm
// field identified the specified name. The signing certificate chain is
// extracted from the signature dictionary. Optionally, additional certificates
// can be specified through the `extraCerts` parameter. The LTV client attempts
// to build the certificate chain up to a trusted root by downloading any
// missing certificates.
func (l *LTV) EnableByName(name string, extraCerts []*x509.Certificate) error {
	acroForm := l.appender.roReader.AcroForm
	for _, allField := range acroForm.AllFields() {
		signatureField, _ := allField.GetContext().(*PdfFieldSignature)
		if signatureField == nil {
			continue
		}
		if partialName := signatureField.PartialName(); partialName != name {
			continue
		}
		return l.Enable(signatureField.V, extraCerts)
	}
	return nil
}

func (l *LTV) buildCertChain(certs, certs2 []*x509.Certificate) ([]*x509.Certificate, map[string]*x509.Certificate, error) {
	certByKey := map[string]*x509.Certificate{}
	for _, cert := range certs {
		certByKey[cert.Subject.CommonName] = cert
	}
	certs3 := certs
	for _, cert := range certs2 {
		commonName := cert.Subject.CommonName
		if _, ok := certByKey[commonName]; ok {
			continue
		}
		certByKey[commonName] = cert
		certs3 = append(certs3, cert)
	}
	if len(certs3) == 0 {
		return nil, nil, ErrSignNoCertificates
	}
	var err error
	for cert := certs3[0]; cert != nil && !l.CertClient.IsCA(cert); {
		cert2, ok := certByKey[cert.Issuer.CommonName]
		if !ok {
			if cert2, err = l.CertClient.GetIssuer(cert); err != nil {
				common.Log.Debug("WARN: Could not retrieve certificate issuer: %v", err)
				break
			}
			certByKey[cert.Issuer.CommonName] = cert2
			certs3 = append(certs3, cert2)
		}
		cert = cert2
	}
	return certs3, certByKey, nil
}

func (l *LTV) enable(certs, chain []*x509.Certificate, s string) error {
	certChain, certByKey, err := l.buildCertChain(certs, chain)
	if err != nil {
		return err
	}
	dataList, err := l.getCerts(certChain)
	if err != nil {
		return err
	}
	var list, list2 [][]byte
	if l.OCSPClient != nil {
		list, err = l.getOCSPs(certChain, certByKey)
		if err != nil {
			return err
		}
	}
	if l.CRLClient != nil {
		list2, err = l.getCRLs(certChain)
		if err != nil {
			return err
		}
	}
	dss := l.dss
	addCerts, err := dss.AddCerts(dataList)
	if err != nil {
		return err
	}
	addOCSPs, err := dss.AddOCSPs(list)
	if err != nil {
		return err
	}
	addCRLs, err := dss.AddCRLs(list2)
	if err != nil {
		return err
	}
	if s != "" {
		dss.VRI[s] = &VRI{Cert: addCerts, OCSP: addOCSPs, CRL: addCRLs}
	}
	l.appender.SetDSS(dss)
	return nil
}

func (l *LTV) getCerts(certs []*x509.Certificate) ([][]byte, error) {
	dataList := make([][]byte, 0, len(certs))
	for _, cert := range certs {
		dataList = append(dataList, cert.Raw)
	}
	return dataList, nil
}

func (l *LTV) validateSig(sig *PdfSignature) error {
	if sig == nil || sig.Contents == nil || len(sig.Contents.Bytes()) == 0 {
		return fmt.Errorf("invalid signature field: %v", sig)
	}
	return nil
}

// EnableAll LTV enables all signatures in the PDF document.
// The signing certificate chain is extracted from each signature dictionary.
// Optionally, additional certificates can be specified through the
// `extraCerts` parameter. The LTV client attempts to build the certificate
// chain up to a trusted root by downloading any missing certificates.
func (l *LTV) EnableAll(extraCerts []*x509.Certificate) error {
	acroForm := l.appender.roReader.AcroForm
	for _, allField := range acroForm.AllFields() {
		signatureField, _ := allField.GetContext().(*PdfFieldSignature)
		if signatureField == nil {
			continue
		}
		sig := signatureField.V
		if err := l.validateSig(sig); err != nil {
			common.Log.Debug("WARN: Skipping invalid signature field: %v", err)
		}
		if err := l.Enable(sig, extraCerts); err != nil {
			return err
		}
	}
	return nil
}

func (l *LTV) getCRLs(certs []*x509.Certificate) ([][]byte, error) {
	dataList := make([][]byte, 0, len(certs))
	for _, cert := range certs {
		for _, crlDistributionPoint := range cert.CRLDistributionPoints {
			if l.CertClient.IsCA(cert) {
				continue
			}
			request, err := l.CRLClient.MakeRequest(crlDistributionPoint, cert)
			if err != nil {
				common.Log.Debug("WARN: CRL request error: %v", err)
				continue
			}
			dataList = append(dataList, request)
		}
	}
	return dataList, nil
}

// RevocationStatus is the outcome of a revocation check for a signature's
// certificate chain.
type RevocationStatus int

const (
	RevocationStatusNotChecked RevocationStatus = iota
	RevocationStatusUnknown
	RevocationStatusGood
	RevocationStatusRevoked
)

func (s RevocationStatus) String() string {
	switch s {
	case RevocationStatusUnknown:
		return "unknown"
	case RevocationStatusGood:
		return "good"
	case RevocationStatusRevoked:
		return "revoked"
	}
	return "not checked"
}

// RevocationCheck selects the revocation data sources consulted during
// signature validation.
type RevocationCheck int

const (
	RevocationCheckNone RevocationCheck = iota
	RevocationCheckOffline
	RevocationCheckOnline
)

const revocationUnknownMessage = "revocation status unknown: no usable revocation data for the certificate chain"

type revocationEvidence struct {
	ocsps []ocspEvidence
	crls  []crlEvidence
}

// VRI represents a Validation-Related Information dictionary.
// The VRI dictionary contains validation data in the form of
// certificates, OCSP and CRL information, for a single signature.
// See ETSI TS 102 778-4 V1.1.1 for more information.
type VRI struct {
	Cert []*core.PdfObjectStream
	OCSP []*core.PdfObjectStream
	CRL  []*core.PdfObjectStream
	TU   *core.PdfObjectString
	TS   *core.PdfObjectString
}

// ToPdfObject returns the PDF representation of the VRI dictionary.
func (v *VRI) ToPdfObject() *core.PdfObjectDictionary {
	dict := core.MakeDict()
	dict.SetIfNotNil(core.PdfObjectName("Cert"), streamsToArray(v.Cert))
	dict.SetIfNotNil(core.PdfObjectName("OCSP"), streamsToArray(v.OCSP))
	dict.SetIfNotNil(core.PdfObjectName("CRL"), streamsToArray(v.CRL))
	dict.SetIfNotNil("TU", v.TU)
	dict.SetIfNotNil("TS", v.TS)
	return dict
}

type certRevocation struct {
	status    RevocationStatus
	revokedAt time.Time
	onHold    bool
	ocspFound bool
	crlFound  bool
}

func (c certRevocation) settled() bool {
	return c.status == RevocationStatusGood || c.status == RevocationStatusRevoked && !c.onHold
}

type chainRevocation struct {
	status      RevocationStatus
	revokedCert *x509.Certificate
	revokedAt   time.Time
	leaf        certRevocation
	warnings    []string
}

func crlEvidenceFromParsed(crls []*parsedCRL) []crlEvidence {
	crls2 := make([]crlEvidence, 0, len(crls))
	for _, crl := range crls {
		crls2 = append(crls2, crlEvidence{parsedCRL: crl})
	}
	return crls2
}

type convergedPlanInputs struct {
	layout               *linearizationLayout
	objects              map[int64]preserializedObject
	objStreams           *objectStreamsState
	hintObjNum           int64
	firstXrefObjs        []int64
	trailerSize          int64
	linDictLen           int
	firstXrefLen         int
	hintLen              int
	xrefFirstEntryOffset int64
	firstSectionStart    int64
	mainSectionStart     int64
}

func crlIsCurrent(thisUpdate, nextUpdate time.Time, cert *x509.Certificate, t time.Time, online bool) bool {
	if thisUpdate.After(cert.NotAfter) {
		return false
	}
	if nextUpdate.IsZero() {
		return online && time.Since(thisUpdate) <= maxCRLAge
	}
	return !t.After(nextUpdate)
}

const (
	crlReasonCertificateHold = 6
	crlReasonRemoveFromCRL   = 8
)

// DSS represents a Document Security Store dictionary.
// The DSS dictionary contains both global and signature specific validation
// information. The certificates and revocation data in the `Certs`, `OCSPs`,
// and `CRLs` fields can be used to validate any signature in the document.
// Additionally, the VRI entry contains validation data per signature.
// The keys in the VRI entry are calculated as upper(hex(sha1(sig.Contents))).
// The values are VRI dictionaries containing certificates and revocation
// information used for validating a single signature.
// See ETSI TS 102 778-4 V1.1.1 for more information.
type DSS struct {
	primitive *core.PdfIndirectObject
	Certs     []*core.PdfObjectStream
	OCSPs     []*core.PdfObjectStream
	CRLs      []*core.PdfObjectStream
	VRI       map[string]*VRI
	certsMap  map[string]*core.PdfObjectStream
	ocspsMap  map[string]*core.PdfObjectStream
	crlsMap   map[string]*core.PdfObjectStream
}

// AddCerts adds certificates to DSS.
func (d *DSS) AddCerts(certs [][]byte) ([]*core.PdfObjectStream, error) {
	return d.add(&d.Certs, d.certsMap, certs)
}

// GenerateHashMaps generates DSS hashmaps for Certificates, OCSPs and CRLs to make sure they are unique.
func (d *DSS) GenerateHashMaps() error {
	generateHashMap, err := d.generateHashMap(d.Certs)
	if err != nil {
		return err
	}
	streamByKey, err := d.generateHashMap(d.OCSPs)
	if err != nil {
		return err
	}
	key, err := d.generateHashMap(d.CRLs)
	if err != nil {
		return err
	}
	d.certsMap = generateHashMap
	d.ocspsMap = streamByKey
	d.crlsMap = key
	return nil
}

func (d *DSS) add(streams *[]*core.PdfObjectStream, streamByKey map[string]*core.PdfObjectStream, dataList [][]byte) ([]*core.PdfObjectStream, error) {
	streams2 := make([]*core.PdfObjectStream, 0, len(dataList))
	for _, data := range dataList {
		data2, err := sha1Digest(data)
		if err != nil {
			return nil, err
		}
		stream, ok := streamByKey[string(data2)]
		if !ok {
			stream, err = core.MakeStream(data, core.NewRawEncoder())
			if err != nil {
				return nil, err
			}
			streamByKey[string(data2)] = stream
			*streams = append(*streams, stream)
		}
		streams2 = append(streams2, stream)
	}
	return streams2, nil
}

// GetContainingPdfObject returns the container of the DSS (indirect object).
func (d *DSS) GetContainingPdfObject() core.PdfObject { return d.primitive }

// ToPdfObject returns the PDF representation of the DSS dictionary.
func (d *DSS) ToPdfObject() core.PdfObject {
	dict := d.primitive.PdfObject.(*core.PdfObjectDictionary)
	dict.Clear()
	dict2 := core.MakeDict()
	for s, vri := range d.VRI {
		dict2.Set(*core.MakeName(s), vri.ToPdfObject())
	}
	dict.SetIfNotNil("Certs", streamsToArray(d.Certs))
	dict.SetIfNotNil("OCSPs", streamsToArray(d.OCSPs))
	dict.SetIfNotNil("CRLs", streamsToArray(d.CRLs))
	dict.Set("VRI", dict2)
	return d.primitive
}

// AddOCSPs adds OCSPs to DSS.
func (d *DSS) AddOCSPs(ocsps [][]byte) ([]*core.PdfObjectStream, error) {
	return d.add(&d.OCSPs, d.ocspsMap, ocsps)
}

func (d *DSS) generateHashMap(streams []*core.PdfObjectStream) (map[string]*core.PdfObjectStream, error) {
	streamByKey := map[string]*core.PdfObjectStream{}
	for _, stream := range streams {
		data, err := core.DecodeStream(stream)
		if err != nil {
			return nil, err
		}
		data2, err := sha1Digest(data)
		if err != nil {
			return nil, err
		}
		streamByKey[string(data2)] = stream
	}
	return streamByKey, nil
}

// AddCRLs adds CRLs to DSS.
func (d *DSS) AddCRLs(crls [][]byte) ([]*core.PdfObjectStream, error) {
	return d.add(&d.CRLs, d.crlsMap, crls)
}

type distributionPointName struct {
	FullName []asn1.RawValue `asn1:"optional,tag:0"`

	// RelativeName is nameRelativeToCRLIssuer, a single
	// RelativeDistinguishedName; only its presence matters.
	RelativeName asn1.RawValue `asn1:"optional,tag:1"`
}

type fetchResult struct {
	body []byte
	crl  *parsedCRL
	err  error
}

type fetchKey struct {
	url        string
	certHash   [32]byte
	issuerHash [32]byte
}

type holdReport struct {
	revokedAt  time.Time
	thisUpdate time.Time
	crl        *parsedCRL
}

func (h holdReport) precedes(thisUpdate time.Time, crl *parsedCRL) bool {
	if h.crl != nil && crl != nil {
		return crl.newerThan(h.crl)
	}
	return thisUpdate.After(h.thisUpdate)
}

func findHoldReport(answers []ocspAnswer, crls []crlEvidence, cert *x509.Certificate, t time.Time) (holdReport, bool) {
	var report holdReport
	ok := false
	fn := func(report2 holdReport) {
		if !ok || report.precedes(report2.thisUpdate, report2.crl) {
			report, ok = report2, true
		}
	}
	for _, answer := range answers {
		if answer.Status == ocsp.Revoked && answer.RevocationReason == ocsp.CertificateHold && !answer.RevokedAt.After(t) {
			fn(holdReport{revokedAt: answer.RevokedAt, thisUpdate: answer.ThisUpdate})
		}
	}
	for _, crl := range crls {
		if entryFor := crl.entryFor(cert); entryFor != nil && entryFor.ReasonCode == crlReasonCertificateHold && !entryFor.RevocationTime.After(t) {
			fn(holdReport{revokedAt: entryFor.RevocationTime, thisUpdate: crl.ThisUpdate, crl: crl.parsedCRL})
		}
	}
	return report, ok
}

func (h holdReport) lifted(answers []ocspAnswer, crls []crlEvidence, cert *x509.Certificate, t time.Time) bool {
	fn := func(t2 time.Time) bool { return t.IsZero() || !t2.After(t) }
	for _, answer := range answers {
		if answer.Status == ocsp.Good && answer.authorised && fn(answer.ThisUpdate) && !answer.ThisUpdate.After(cert.NotAfter) && h.precedes(answer.ThisUpdate, nil) {
			return true
		}
	}
	for _, crl := range crls {
		if !fn(crl.ThisUpdate) || crl.ThisUpdate.After(cert.NotAfter) || !h.precedes(crl.ThisUpdate, crl.parsedCRL) {
			continue
		}
		entryFor := crl.entryFor(cert)
		if entryFor != nil && entryFor.ReasonCode == crlReasonRemoveFromCRL {
			return true
		}
		if entryFor == nil && !crl.isDelta() && crl.covers(cert) {
			return true
		}
	}
	return false
}

type issuingDistributionPoint struct {
	DistributionPoint          distributionPointName `asn1:"optional,tag:0"`
	OnlyContainsUserCerts      bool                  `asn1:"optional,tag:1"`
	OnlyContainsCACerts        bool                  `asn1:"optional,tag:2"`
	OnlySomeReasons            asn1.BitString        `asn1:"optional,tag:3"`
	IndirectCRL                bool                  `asn1:"optional,tag:4"`
	OnlyContainsAttributeCerts bool                  `asn1:"optional,tag:5"`
}
