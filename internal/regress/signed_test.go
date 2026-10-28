package regress

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	"github.com/OpenMellon/openpdf/annotator"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/sighandler"
)

// testSigner returns a runtime-generated key and self-signed certificate whose validity window is fixed far from
// the wall clock, so the check does not depend on when it runs. No key material is committed.
func testSigner(t *testing.T) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Regress Signer"},
		NotBefore:    time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return key, cert
}

// TestSigned signs a document and checks the signature validates and that tampering with the signed bytes is
// detected. The output is not byte-deterministic (signing time, RSA padding), so it has no golden.
func TestSigned(t *testing.T) {
	key, cert := testSigner(t)
	handler, err := sighandler.NewAdobePKCS7Detached(key, cert)
	if err != nil {
		t.Fatal(err)
	}
	appender, err := model.NewPdfAppender(readerOf(t, twoPageDoc(t, "Signed")))
	if err != nil {
		t.Fatal(err)
	}
	sig := model.NewPdfSignature(handler)
	sig.SetName("Regress Signer")
	sig.SetReason("Approved")
	if err := sig.Initialize(); err != nil {
		t.Fatal(err)
	}
	opts := annotator.NewSignatureFieldOpts()
	opts.Rect = []float64{50, 700, 250, 750}
	field, err := annotator.NewSignatureField(sig, []*annotator.SignatureLine{annotator.NewSignatureLine("Signed by", "Regress Signer")}, opts)
	if err != nil {
		t.Fatal(err)
	}
	field.T = core.MakeString("Signature1")
	if err := appender.Sign(1, field); err != nil {
		t.Fatal(err)
	}
	var signed bytes.Buffer
	if err := appender.Write(&signed); err != nil {
		t.Fatal(err)
	}

	results, err := readerOf(t, signed.Bytes()).ValidateSignatures([]model.SignatureHandler{handler})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].IsSigned || !results[0].IsVerified {
		t.Fatalf("validation of untouched file: %+v", results)
	}

	// Change a byte inside the signed range (the first page's uncompressed MediaBox) and it must stop verifying.
	tampered := bytes.Replace(signed.Bytes(), []byte("[0 0 612 792]"), []byte("[0 0 613 792]"), 1)
	if bytes.Equal(tampered, signed.Bytes()) {
		t.Fatal("tamper marker not found in the signed file")
	}
	results, err = readerOf(t, tampered).ValidateSignatures([]model.SignatureHandler{handler})
	if err != nil {
		t.Fatalf("tampered file should parse and fail verification, got error: %v", err)
	}
	if len(results) != 1 || results[0].IsVerified {
		t.Fatalf("tampered file result %+v, want one unverified signature", results)
	}
}
