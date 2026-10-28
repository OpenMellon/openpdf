// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package testutils

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ocsp"
)

func (p *PKI) makeCRL() ([]byte, error) {
	list := &x509.RevocationList{Number: big.NewInt(1), ThisUpdate: time.Now().Add(-time.Minute), NextUpdate: time.Now().AddDate(1, 0, 0)}
	for _, entry := range p.revokedEntries() {
		list.RevokedCertificateEntries = append(list.RevokedCertificateEntries, x509.RevocationListEntry{SerialNumber: entry.serial, RevocationTime: entry.revokedAt})
	}
	return x509.CreateRevocationList(rand.Reader, list, p.caCert, p.caKey)
}
func (p *PKI) MockTransport() http.RoundTripper { return &pkiTransport{pki: p} }
func (r *retryT) Helper()                       {}

type revokedSerial struct {
	serial    *big.Int
	revokedAt time.Time
}
type FlateStream struct {
	Dict string
	Data []byte
}

func (p *PKI) Revoke(t testing.TB, serial *big.Int, at time.Time) {
	t.Helper()
	key := serial.String()
	p.mu.Lock()
	if p.revoked == nil {
		p.revoked = map[string]revokedSerial{}
	}
	p.revoked[key] = revokedSerial{serial: new(big.Int).Set(serial), revokedAt: at}
	p.mu.Unlock()
	t.Cleanup(func() { p.mu.Lock(); delete(p.revoked, key); p.mu.Unlock() })
}

type abortExtendedTestingT struct{}

func (p *PKI) makeOCSP(reqBytes []byte) ([]byte, error) {
	req, err := ocsp.ParseRequest(reqBytes)
	if err != nil {
		return nil, err
	}
	resp := ocsp.Response{Status: ocsp.Good, SerialNumber: req.SerialNumber, ThisUpdate: time.Now().Add(-time.Minute), NextUpdate: time.Now().AddDate(0, 0, 7)}
	p.mu.Lock()
	entry, revoked := p.revoked[req.SerialNumber.String()]
	p.mu.Unlock()
	if revoked {
		resp.Status = ocsp.Revoked
		resp.RevokedAt = entry.revokedAt
		resp.RevocationReason = ocsp.Unspecified
	}
	return ocsp.CreateResponse(p.caCert, p.caCert, resp, p.caKey)
}

func ParseIndirectObjects(rawpdf string) (map[int64]core.PdfObject, error) {
	parser := core.NewParserFromString(rawpdf)
	objects := map[int64]core.PdfObject{}
	err := parser.ParseIndirectObjectsSequential(0, func(obj core.PdfObject) error {
		switch typed := obj.(type) {
		case *core.PdfIndirectObject:
			objects[typed.ObjectNumber] = obj
		case *core.PdfObjectStream:
			objects[typed.ObjectNumber] = obj
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, o := range objects {
		resolveReferences(o, objects)
	}
	return objects, nil
}

func RunRenderTest(t *testing.T, pdfPath, outputDir, baselineRenderPath string, saveBaseline bool) {
	name := strings.TrimSuffix(filepath.Base(pdfPath), filepath.Ext(pdfPath))
	t.Run("render", func(sub *testing.T) {
		base := filepath.Join(outputDir, name)
		pattern := base + "-%d.png"
		if err := RenderPDFToPNGs(pdfPath, 0, pattern); err != nil {
			sub.Skip(err)
		}
		for page := 1; true; page++ {
			actual := fmt.Sprintf("%s-%d.png", base, page)
			expected := filepath.Join(baselineRenderPath, fmt.Sprintf("%s-%d_exp.png", name, page))
			if _, statErr := os.Stat(actual); statErr != nil {
				break
			}
			sub.Logf("%s", expected)
			if saveBaseline {
				sub.Logf("Copying %s -> %s", actual, expected)
				copyErr := CopyFile(actual, expected)
				if copyErr != nil {
					sub.Fatalf("ERROR copying to %s: %v", expected, copyErr)
				}
				continue
			}
			sub.Run(fmt.Sprintf("page%d", page), func(pt *testing.T) {
				pt.Logf("Comparing %s vs %s", actual, expected)
				same, cmpErr := ComparePNGFiles(actual, expected)
				if os.IsNotExist(cmpErr) {
					pt.Fatal("image file missing")
				} else if !same {
					pt.Fatal("wrong page rendered")
				}
			})
		}
	})
}

func (p *PKI) CRL(t testing.TB, tmpl *x509.RevocationList) []byte {
	t.Helper()
	crl, err := x509.CreateRevocationList(rand.Reader, tmpl, p.caCert, p.caKey)
	if err != nil {
		t.Fatalf("create CRL: %v", err)
	}
	return crl
}

func (r *retryT) Errorf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}
func (p *PKI) ECDSALeaf() (*ecdsa.PrivateKey, *x509.Certificate) { return p.ecdsaKey, p.ecdsaCert }
func (p *PKI) CACert() *x509.Certificate                         { return p.caCert }

var ErrRenderNotSupported = errors.New("rendering PDF files is not supported on this system")

type PKI struct {
	caCert     *x509.Certificate
	caKey      *rsa.PrivateKey
	rsaCert    *x509.Certificate
	rsaKey     *rsa.PrivateKey
	ecdsaCert  *x509.Certificate
	ecdsaKey   *ecdsa.PrivateKey
	notBefore  time.Time
	notAfter   time.Time
	mu         sync.Mutex
	nextSerial int64
	revoked    map[string]revokedSerial
}

func HashFile(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func resolveReferences(obj core.PdfObject, objects map[int64]core.PdfObject) error {
	switch typed := obj.(type) {
	case *core.PdfIndirectObject:
		ind := typed
		resolveReferences(ind.PdfObject, objects)
	case *core.PdfObjectDictionary:
		dict := typed
		for _, key := range dict.Keys() {
			val := dict.Get(key)
			if ref, isRef := val.(*core.PdfObjectReference); isRef {
				target, ok := objects[ref.ObjectNumber]
				if !ok {
					return errors.New("reference to outside object")
				}
				dict.Set(key, target)
			} else {
				resolveReferences(val, objects)
			}
		}
	case *core.PdfObjectArray:
		arr := typed
		for i, elem := range arr.Elements() {
			if ref, isRef := elem.(*core.PdfObjectReference); isRef {
				target, ok := objects[ref.ObjectNumber]
				if !ok {
					return errors.New("reference to outside object")
				}
				arr.Set(i, target)
			} else {
				resolveReferences(elem, objects)
			}
		}
	}
	return nil
}

func ReadPNG(file string) (image.Image, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

type (
	pkiTransport struct{ pki *PKI }
	LeafOpts     struct {
		CommonName            string
		SelfSigned            bool
		NotBefore             time.Time
		NotAfter              time.Time
		KeyUsage              x509.KeyUsage
		ExtKeyUsage           []x509.ExtKeyUsage
		CRLDistributionPoints []string
		OCSPServer            []string
		Issuer                *x509.Certificate
		IssuerKey             crypto.Signer
	}
)

func CompareDictionariesDeep(d1, d2 *core.PdfObjectDictionary) bool {
	if len(d1.Keys()) != len(d2.Keys()) {
		common.Log.Debug("Dict entries mismatch (%d != %d)", len(d1.Keys()), len(d2.Keys()))
		common.Log.Debug("Was '%s' vs '%s'", d1.Write(), d2.Write())
		return false
	}
	for _, key := range d1.Keys() {
		if key == "Parent" {
			continue
		}
		v1 := core.TraceToDirectObject(d1.Get(key))
		v2 := core.TraceToDirectObject(d2.Get(key))
		if v1 == nil {
			common.Log.Debug("v1 is nil")
			return false
		}
		if v2 == nil {
			common.Log.Debug("v2 is nil")
			return false
		}
		switch t := v1.(type) {
		case *core.PdfObjectDictionary:
			dict2, ok := v2.(*core.PdfObjectDictionary)
			if !ok {
				common.Log.Debug("Type mismatch %T vs %T", v1, v2)
				return false
			}
			if !CompareDictionariesDeep(t, dict2) {
				return false
			}
			continue
		case *core.PdfObjectArray:
			arr2, ok := v2.(*core.PdfObjectArray)
			if !ok {
				common.Log.Debug("v2 not an array")
				return false
			}
			if t.Len() != arr2.Len() {
				common.Log.Debug("array length mismatch (%d != %d)", t.Len(), arr2.Len())
				return false
			}
			for i := 0; i < t.Len(); i++ {
				e1 := core.TraceToDirectObject(t.Get(i))
				e2 := core.TraceToDirectObject(arr2.Get(i))
				if dict1, isDict := e1.(*core.PdfObjectDictionary); isDict {
					dict2, ok := e2.(*core.PdfObjectDictionary)
					if !ok {
						return false
					}
					if !CompareDictionariesDeep(dict1, dict2) {
						return false
					}
				} else {
					w1 := e1.Write()
					w2 := e2.Write()
					if string(w1) != string(w2) {
						common.Log.Debug("Mismatch '%s' != '%s'", w1, w2)
						return false
					}
				}
			}
			continue
		}
		if v1.String() != v2.String() {
			common.Log.Debug("key=%s Mismatch! '%s' != '%s'", key, v1.String(), v2.String())
			common.Log.Debug("For '%T' - '%T'", v1, v2)
			common.Log.Debug("For '%+v' - '%+v'", v1, v2)
			return false
		}
	}
	return true
}

const (
	ocspURL      = "http://pki.test/ocsp"
	caIssuerURL  = "http://pki.test/aia/ca.crt"
	crlURL       = "http://pki.test/crl/ca.crl"
	caCommonName = "OpenPDF ehf. test CA"
)

type ExtendedTestingT interface {
	require.TestingT
	Logf(format string, args ...any)
	Helper()
}

func (r *retryT) FailNow() { panic(abortExtendedTestingT{}) }
func RetryHelper(t *testing.T, maxAttempts int, delay time.Duration, fn func(et ExtendedTestingT)) error {
	if maxAttempts < 1 {
		return errors.New("maxAttempts must be at least 1")
	}
	t.Helper()
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		t.Logf("Starting attempt %d of %d...", attempt, maxAttempts)
		rt := &retryT{}
		passed := func() bool {
			defer func() {
				if rec := recover(); rec != nil {
					if _, isAbort := rec.(abortExtendedTestingT); !isAbort {
						panic(rec)
					}
				}
				for _, line := range rt.logs {
					t.Log(line)
				}
			}()
			fn(rt)
			return len(rt.failures) == 0
		}()
		if passed {
			return nil
		}
		lastErr = errors.New(strings.Join(rt.failures, "\n"))
		t.Logf("Attempt %d failed: %v", attempt, lastErr)
		if attempt < maxAttempts {
			time.Sleep(delay)
		}
	}
	return fmt.Errorf("max attempts reached: %d, err: %v", maxAttempts, lastErr)
}

func ComparePNGFiles(file1, file2 string) (bool, error) {
	hash1, err := HashFile(file1)
	if err != nil {
		return false, err
	}
	hash2, err := HashFile(file2)
	if err != nil {
		return false, err
	}
	if hash1 == hash2 {
		return true, nil
	}
	img1, err := ReadPNG(file1)
	if err != nil {
		return false, err
	}
	img2, err := ReadPNG(file2)
	if err != nil {
		return false, err
	}
	if img1.Bounds() != img2.Bounds() {
		return false, nil
	}
	return CompareImages(img1, img2)
}
