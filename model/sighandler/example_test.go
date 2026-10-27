package sighandler_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/OpenMellon/openpdf/annotator"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/creator"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/sighandler"
)

// Sign a PDF with a PKCS#7 detached signature and validate it again.
//
// The certificate here is generated on the fly; in practice load yours from
// a PEM or PKCS#12 file. Validation checks integrity only - whether the
// signer is trusted is a separate step (see PdfReader.ValidateSignaturesWithOpts).
func Example() {
	dir, _ := os.MkdirTemp("", "openpdf")
	defer os.RemoveAll(dir)
	in, out := filepath.Join(dir, "in.pdf"), filepath.Join(dir, "signed.pdf")

	c := creator.New()
	c.NewPage()
	if err := c.WriteToFile(in); err != nil {
		log.Fatal(err)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Example Signer"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		log.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		log.Fatal(err)
	}

	handler, err := sighandler.NewAdobePKCS7Detached(key, cert)
	if err != nil {
		log.Fatal(err)
	}

	reader, f, err := model.NewPdfReaderFromFile(in, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	appender, err := model.NewPdfAppender(reader)
	if err != nil {
		log.Fatal(err)
	}

	sig := model.NewPdfSignature(handler)
	sig.SetName("Example Signer")
	sig.SetReason("Approved")
	if err := sig.Initialize(); err != nil {
		log.Fatal(err)
	}
	opts := annotator.NewSignatureFieldOpts()
	opts.Rect = []float64{50, 700, 250, 750}
	field, err := annotator.NewSignatureField(sig, []*annotator.SignatureLine{
		annotator.NewSignatureLine("Signed by", "Example Signer"),
	}, opts)
	if err != nil {
		log.Fatal(err)
	}
	field.T = core.MakeString("Signature1")
	if err := appender.Sign(1, field); err != nil {
		log.Fatal(err)
	}
	if err := appender.WriteToFile(out); err != nil {
		log.Fatal(err)
	}

	signed, sf, err := model.NewPdfReaderFromFile(out, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer sf.Close()
	results, err := signed.ValidateSignatures([]model.SignatureHandler{handler})
	if err != nil {
		log.Fatal(err)
	}
	for _, r := range results {
		fmt.Println("signed:", r.IsSigned, "verified:", r.IsVerified)
	}
	// Output: signed: true verified: true
}
