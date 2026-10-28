// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package testutils

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"image"
	"io"
	"math/big"
	"net/http"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/OpenMellon/openpdf/core"
	"golang.org/x/crypto/ocsp"
)

func SinglePagePDF(resources string, contents []byte, extra ...FlateStream) ([]byte, error) {
	streams := append([]FlateStream{{Data: contents}}, extra...)
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, 4+len(streams))
	writeObj := func(num int, body string) {
		offsets[num] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", num, body)
	}
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObj(3, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 72 72] /Resources %s /Contents 4 0 R >>", resources))
	for i, s := range streams {
		encoded, err := core.NewFlateEncoder().EncodeBytes(s.Data)
		if err != nil {
			return nil, err
		}
		objNum := 4 + i
		offsets[objNum] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n<< %s /Length %d /Filter /FlateDecode >>\nstream\n", objNum, s.Dict, len(encoded))
		buf.Write(encoded)
		buf.WriteString("\nendstream\nendobj\n")
	}
	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for i := 1; i < len(offsets); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xrefOffset)
	return buf.Bytes(), nil
}

func (tr *pkiTransport) route(req *http.Request) ([]byte, string, error) {
	switch req.URL.String() {
	case caIssuerURL:
		return tr.pki.caCert.Raw, "application/pkix-cert", nil
	case crlURL:
		crl, err := tr.pki.makeCRL()
		return crl, "application/pkix-crl", err
	case ocspURL:
		if req.Body == nil {
			return nil, "", nil
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, "", err
		}
		resp, err := tr.pki.makeOCSP(body)
		return resp, "application/ocsp-response", err
	}
	return nil, "", nil
}

type retryT struct {
	failures []string
	logs     []string
}

func (p *PKI) NewLeaf(t testing.TB, opts LeafOpts) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	p.mu.Lock()
	p.nextSerial++
	serial := p.nextSerial
	p.mu.Unlock()
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: opts.CommonName, Organization: []string{"OpenPDF ehf. test"}}}
	applyLeafDefaults(tmpl, p.notBefore, p.notAfter)
	if !opts.NotBefore.IsZero() {
		tmpl.NotBefore = opts.NotBefore
	}
	if !opts.NotAfter.IsZero() {
		tmpl.NotAfter = opts.NotAfter
	}
	if opts.KeyUsage != 0 {
		tmpl.KeyUsage = opts.KeyUsage
	}
	if opts.ExtKeyUsage != nil {
		tmpl.ExtKeyUsage = opts.ExtKeyUsage
	}
	if opts.CRLDistributionPoints != nil {
		tmpl.CRLDistributionPoints = opts.CRLDistributionPoints
	}
	if opts.OCSPServer != nil {
		tmpl.OCSPServer = opts.OCSPServer
	}
	parent, signer := p.caCert, crypto.Signer(p.caKey)
	if opts.Issuer != nil {
		parent, signer = opts.Issuer, opts.IssuerKey
	}
	if opts.SelfSigned {
		parent, signer = tmpl, key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, signer)
	if err != nil {
		t.Fatalf("create leaf certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse leaf certificate: %v", err)
	}
	return key, cert
}

func RenderPDFToPNGs(pdfPath string, dpi int, outpathTpl string) error {
	if dpi <= 0 {
		dpi = 100
	}
	if _, err := exec.LookPath("gs"); err != nil {
		return ErrRenderNotSupported
	}
	return exec.Command("gs", "-sDEVICE=pngalpha", "-o", outpathTpl, fmt.Sprintf("-r%d", dpi), pdfPath).Run()
}

func (p *PKI) NewIntermediate(t testing.TB, commonName string, extKeyUsage []x509.ExtKeyUsage) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate intermediate key: %v", err)
	}
	p.mu.Lock()
	p.nextSerial++
	serial := p.nextSerial
	p.mu.Unlock()
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: commonName, Organization: []string{"OpenPDF ehf. test"}}, NotBefore: p.notBefore, NotAfter: p.notAfter, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, ExtKeyUsage: extKeyUsage}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, p.caCert, &key.PublicKey, p.caKey)
	if err != nil {
		t.Fatalf("create intermediate certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse intermediate certificate: %v", err)
	}
	return key, cert
}

func CompareImages(img1, img2 image.Image) (bool, error) {
	bounds := img1.Bounds()
	diff := 0
	for x := 0; x < bounds.Size().X; x++ {
		for y := 0; y < bounds.Size().Y; y++ {
			r1, g1, b1, _ := img1.At(x, y).RGBA()
			r2, g2, b2, _ := img2.At(x, y).RGBA()
			if r1 != r2 || g1 != g2 || b1 != b2 {
				diff++
			}
		}
	}
	fraction := float64(diff) / float64(bounds.Dx()*bounds.Dy())
	if fraction > 0.0001 {
		fmt.Printf("diff fraction: %v (%d)\n", fraction, diff)
		return false, nil
	}
	return true, nil
}

func CommentPadding(n int) []byte {
	line := []byte("% decoded size limit padding\n")
	padded := bytes.Repeat(line, n/len(line)+1)
	return padded[:n]
}

func (p *PKI) OCSPResponse(t testing.TB, tmpl ocsp.Response) []byte {
	t.Helper()
	der, err := ocsp.CreateResponse(p.caCert, p.caCert, tmpl, p.caKey)
	if err != nil {
		t.Fatalf("create OCSP response: %v", err)
	}
	return der
}

func PDFFromObjects(objects map[int]string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := map[int]int{}
	maxNum := 0
	for num := range objects {
		maxNum = max(maxNum, num)
	}
	for num := 1; num <= maxNum; num++ {
		body, ok := objects[num]
		if !ok {
			continue
		}
		offsets[num] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", num, body)
	}
	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", maxNum+1)
	for num := 1; num <= maxNum; num++ {
		if off, ok := offsets[num]; ok {
			fmt.Fprintf(&buf, "%010d 00000 n \n", off)
		} else {
			buf.WriteString("0000000000 65535 f \n")
		}
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF", maxNum+1, xrefOffset)
	return buf.Bytes()
}

func newPKI() (*PKI, error) {
	notBefore := time.Now().Add(-time.Hour)
	notAfter := notBefore.AddDate(100, 0, 0)
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	caTmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: caCommonName, Organization: []string{"OpenPDF ehf. test"}}, NotBefore: notBefore, NotAfter: notAfter, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, err
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	rsaTmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "john@example.com", Organization: []string{"OpenPDF ehf. test"}}}
	applyLeafDefaults(rsaTmpl, notBefore, notAfter)
	rsaDER, err := x509.CreateCertificate(rand.Reader, rsaTmpl, caCert, &rsaKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	rsaCert, err := x509.ParseCertificate(rsaDER)
	if err != nil {
		return nil, err
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	ecTmpl := &x509.Certificate{SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "John Smith", Organization: []string{"OpenPDF"}}}
	applyLeafDefaults(ecTmpl, notBefore, notAfter)
	ecDER, err := x509.CreateCertificate(rand.Reader, ecTmpl, caCert, &ecKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	ecCert, err := x509.ParseCertificate(ecDER)
	if err != nil {
		return nil, err
	}
	return &PKI{caCert: caCert, caKey: caKey, rsaCert: rsaCert, rsaKey: rsaKey, ecdsaCert: ecCert, ecdsaKey: ecKey, notBefore: notBefore, notAfter: notAfter, nextSerial: 100}, nil
}

func (tr *pkiTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		defer req.Body.Close()
	}
	body, contentType, err := tr.route(req)
	if err != nil {
		return nil, err
	}
	status := http.StatusOK
	if body == nil {
		status = http.StatusNotFound
	}
	resp := &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1, Header: make(http.Header), Request: req, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}
	if body != nil {
		resp.Header.Set("Content-Type", contentType)
	}
	return resp, nil
}

func (p *PKI) revokedEntries() []revokedSerial {
	p.mu.Lock()
	defer p.mu.Unlock()
	entries := make([]revokedSerial, 0, len(p.revoked))
	for _, entry := range p.revoked {
		entries = append(entries, entry)
	}
	return entries
}

func TestPKI(t testing.TB) *PKI {
	t.Helper()
	pkiOnce.Do(func() { sharedPKI, pkiErr = newPKI() })
	if pkiErr != nil {
		t.Fatalf("build test PKI: %v", pkiErr)
	}
	return sharedPKI
}
func (p *PKI) RSALeaf() (*rsa.PrivateKey, *x509.Certificate) { return p.rsaKey, p.rsaCert }

var (
	pkiOnce   sync.Once
	sharedPKI *PKI
	pkiErr    error
)

func applyLeafDefaults(cert *x509.Certificate, notBefore, notAfter time.Time) {
	cert.NotBefore = notBefore
	cert.NotAfter = notAfter
	cert.KeyUsage = x509.KeyUsageDigitalSignature
	cert.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection, x509.ExtKeyUsageClientAuth}
	cert.OCSPServer = []string{ocspURL}
	cert.IssuingCertificateURL = []string{caIssuerURL}
	cert.CRLDistributionPoints = []string{crlURL}
}

func (r *retryT) Logf(format string, args ...any) {
	r.logs = append(r.logs, fmt.Sprintf(format, args...))
}
