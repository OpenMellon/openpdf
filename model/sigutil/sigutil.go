// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package sigutil

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/timestamp"
	"golang.org/x/crypto/ocsp"
	"io"
	"net/http"
	"time"
)

// Get retrieves the certificate at the specified URL. A status other
// than 200 or a response larger than MaxResponseSize is an error.
func (c *CertClient) Get(url string) (*x509.Certificate, error) {
	if c.HTTPClient == nil {
		c.HTTPClient = newHTTPClient()
	}
	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := readLimitedBody(resp, c.MaxResponseSize, maxCertSize)
	if err != nil {
		return nil, err
	}
	if block, _ := pem.Decode(body); block != nil {
		body = block.Bytes
	}
	cert, err := x509.ParseCertificate(body)
	if err != nil {
		return nil, err
	}
	return cert, nil
}

// OCSPClient represents a OCSP (Online Certificate Status Protocol) client.
// It is used to request revocation data from OCSP servers.
type OCSPClient struct {

	// HTTPClient is the HTTP client used to make OCSP requests.
	// By default, an HTTP client with a 5 second timeout per request is used.
	HTTPClient *http.Client

	// Hash is the hash function  used when constructing the OCSP
	// requests. If zero, SHA-1 will be used.
	Hash crypto.Hash

	// MaxResponseSize is the largest OCSP response accepted, in bytes.
	// Zero or negative selects 1 MiB.
	MaxResponseSize int64
}

// GetEncodedToken executes the timestamp request and returns the DER
// encoded timestamp token bytes. A status other than 200 or a response
// larger than MaxResponseSize is an error.
func (c *TimestampClient) GetEncodedToken(serverURL string, req *timestamp.Request) ([]byte, error) {
	if serverURL == "" {
		return nil, fmt.Errorf("must provide timestamp server URL")
	}
	if req == nil {
		return nil, fmt.Errorf("timestamp request cannot be nil")
	}
	reqBytes, err := req.Marshal()
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequest("POST", serverURL, bytes.NewBuffer(reqBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/timestamp-query")
	if c.BeforeHTTPRequest != nil {
		if hookErr := c.BeforeHTTPRequest(httpReq); hookErr != nil {
			return nil, hookErr
		}
	}
	client := c.HTTPClient
	if client == nil {
		client = newHTTPClient()
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := readLimitedBody(resp, c.MaxResponseSize, maxTimestampSize)
	if err != nil {
		return nil, err
	}
	var tsResp struct {
		Version asn1.RawValue
		Content asn1.RawValue
	}
	if _, err = asn1.Unmarshal(body, &tsResp); err != nil {
		return nil, err
	}
	return tsResp.Content.FullBytes, nil
}

// TimestampClient represents a RFC 3161 timestamp client.
// It is used to obtain signed tokens from timestamp authority servers.
type TimestampClient struct {

	// HTTPClient is the HTTP client used to make timestamp requests.
	// By default, an HTTP client with a 5 second timeout per request is used.
	HTTPClient *http.Client

	// Callbacks.
	BeforeHTTPRequest func(req *http.Request) error

	// MaxResponseSize is the largest timestamp response accepted, in
	// bytes. Zero or negative selects 1 MiB.
	MaxResponseSize int64
}

// NewCRLClient returns a new CRL client.
func NewCRLClient() *CRLClient { return &CRLClient{HTTPClient: newHTTPClient()} }

// NewTimestampClient returns a new timestamp client.
func NewTimestampClient() *TimestampClient { return &TimestampClient{HTTPClient: newHTTPClient()} }

// NewTimestampRequest returns a new timestamp request based
// on the specified options.
func NewTimestampRequest(body io.Reader, opts *timestamp.RequestOptions) (*timestamp.Request, error) {
	if opts == nil {
		opts = &timestamp.RequestOptions{}
	}
	if opts.Hash == 0 {
		opts.Hash = crypto.SHA256
	}
	if !opts.Hash.Available() {
		return nil, x509.ErrUnsupportedAlgorithm
	}
	h := opts.Hash.New()
	if _, err := io.Copy(h, body); err != nil {
		return nil, err
	}
	return &timestamp.Request{HashAlgorithm: opts.Hash, HashedMessage: h.Sum(nil), Certificates: opts.Certificates, TSAPolicyOID: opts.TSAPolicyOID, Nonce: opts.Nonce}, nil
}

const (
	maxCRLSize       = 64 << 20
	maxOCSPSize      = 1 << 20
	maxTimestampSize = 1 << 20
	maxCertSize      = 1 << 20
)

func readLimitedBody(resp *http.Response, limit, defaultLimit int64) ([]byte, error) {
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status code: %d", resp.StatusCode)
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, err
	}
	if n, _ := io.ReadFull(resp.Body, make([]byte, 1)); n > 0 {
		return nil, fmt.Errorf("response larger than %d bytes", limit)
	}
	return body, nil
}

// CRLClient represents a CRL (Certificate revocation list) client.
// It is used to request revocation data from CRL servers.
type CRLClient struct {

	// HTTPClient is the HTTP client used to make CRL requests.
	// By default, an HTTP client with a 5 second timeout per request is used.
	HTTPClient *http.Client

	// MaxResponseSize is the largest CRL accepted, in bytes. Zero or
	// negative selects 64 MiB.
	MaxResponseSize int64
}

func newHTTPClient() *http.Client { return &http.Client{Timeout: 5 * time.Second} }

// NewCertClient returns a new certificate client.
func NewCertClient() *CertClient { return &CertClient{HTTPClient: newHTTPClient()} }

// CertClient represents a X.509 certificate client. Its primary purpose
// is to download certificates.
type CertClient struct {

	// HTTPClient is the HTTP client used to make certificate requests.
	// By default, an HTTP client with a 5 second timeout per request is used.
	HTTPClient *http.Client

	// MaxResponseSize is the largest certificate accepted, in bytes. Zero
	// or negative selects 1 MiB.
	MaxResponseSize int64
}

// MakeRequest makes a CRL request to the specified server and returns the
// response. If a server URL is not provided, it is extracted from the
// certificate. A status other than 200 or a response larger than
// MaxResponseSize is an error.
func (c *CRLClient) MakeRequest(serverURL string, cert *x509.Certificate) ([]byte, error) {
	if c.HTTPClient == nil {
		c.HTTPClient = newHTTPClient()
	}
	if serverURL == "" {
		if len(cert.CRLDistributionPoints) == 0 {
			return nil, errors.New("certificate does not specify any CRL servers")
		}
		serverURL = cert.CRLDistributionPoints[0]
	}
	resp, err := c.HTTPClient.Get(serverURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := readLimitedBody(resp, c.MaxResponseSize, maxCRLSize)
	if err != nil {
		return nil, err
	}
	if block, _ := pem.Decode(body); block != nil {
		body = block.Bytes
	}
	return body, nil
}

// MakeRequest makes a OCSP request to the specified server and returns
// the parsed and raw responses. If a server URL is not provided, it is
// extracted from the certificate. A status other than 200 or a response
// larger than MaxResponseSize is an error.
func (c *OCSPClient) MakeRequest(serverURL string, cert, issuer *x509.Certificate) (*ocsp.Response, []byte, error) {
	if c.HTTPClient == nil {
		c.HTTPClient = newHTTPClient()
	}
	if serverURL == "" {
		if len(cert.OCSPServer) == 0 {
			return nil, nil, errors.New("certificate does not specify any OCSP servers")
		}
		serverURL = cert.OCSPServer[0]
	}
	reqBytes, err := ocsp.CreateRequest(cert, issuer, &ocsp.RequestOptions{Hash: c.Hash})
	if err != nil {
		return nil, nil, err
	}
	resp, err := c.HTTPClient.Post(serverURL, "application/ocsp-request", bytes.NewReader(reqBytes))
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := readLimitedBody(resp, c.MaxResponseSize, maxOCSPSize)
	if err != nil {
		return nil, nil, err
	}
	if block, _ := pem.Decode(body); block != nil {
		body = block.Bytes
	}
	ocspResp, err := ocsp.ParseResponseForCert(body, cert, issuer)
	if err != nil {
		return nil, nil, err
	}
	return ocspResp, body, nil
}

// GetIssuer retrieves the issuer of the provided certificate.
func (c *CertClient) GetIssuer(cert *x509.Certificate) (*x509.Certificate, error) {
	for _, u := range cert.IssuingCertificateURL {
		issuer, err := c.Get(u)
		if err != nil {
			common.Log.Debug("WARN: could not download issuer for certificate %v: %v", cert.Subject.CommonName, err)
			continue
		}
		return issuer, nil
	}
	return nil, fmt.Errorf("issuer certificate not found")
}

// NewOCSPClient returns a new OCSP client.
func NewOCSPClient() *OCSPClient { return &OCSPClient{HTTPClient: newHTTPClient(), Hash: crypto.SHA1} }

// IsCA returns true if the provided certificate appears to be a CA certificate.
func (c *CertClient) IsCA(cert *x509.Certificate) bool {
	return cert.IsCA && bytes.Equal(cert.RawIssuer, cert.RawSubject)
}
