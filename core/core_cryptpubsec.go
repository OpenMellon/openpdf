package core

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/OpenMellon/openpdf/core/security"
	"github.com/OpenMellon/openpdf/core/security/crypt"
)

func (crypt *PdfCrypt) isPubSec() bool { return crypt.pubSec != nil }

func parsePubSecRecipients(dict *PdfObjectDictionary) ([][]byte, error) {
	recipients, ok := GetArray(dict.Get("Recipients"))
	if !ok || recipients.Len() == 0 {
		return nil, errors.New("public-key encrypt dictionary missing Recipients")
	}
	datas := make([][]byte, 0, recipients.Len())
	for i, elem := range recipients.Elements() {
		stringBytes, hasStringBytes := GetStringBytes(elem)
		if !hasStringBytes || len(stringBytes) == 0 {
			return nil, fmt.Errorf("invalid Recipients entry %d", i)
		}
		datas = append(datas, stringBytes)
	}
	return datas, nil
}

var ErrCertificateRequired = errors.New("document is encrypted with a public-key security handler; a recipient certificate and private key are required")

func (crypt *PdfCrypt) encodeEncryptPubSec() (*PdfObjectDictionary, error) {
	pubSec := crypt.pubSec
	encryptDict := crypt.newEncryptDict()
	encryptDict.Set("Filter", MakeName(adobePubSecFilter))
	encryptDict.Set("SubFilter", MakeName(pubSec.SubFilter))
	if err := crypt.saveCryptFilters(encryptDict); err != nil {
		return nil, err
	}
	cfDict, ok := GetDict(encryptDict.Get("CF"))
	if !ok {
		return nil, errors.New("encryption dictionary has no CF entry")
	}
	dict, ok := GetDict(cfDict.Get(defaultCryptFilterKey))
	if !ok {
		return nil, fmt.Errorf("crypt filter %s missing from encryption dictionary", defaultCryptFilterKey)
	}
	dict.Set("Length", MakeInteger(int64(crypt.encrypt.Length)))
	array := MakeArray()
	for _, recipient := range pubSec.Recipients {
		array.Append(MakeHexString(string(recipient)))
	}
	dict.Set("Recipients", array)
	dict.Set("EncryptMetadata", MakeBool(pubSec.EncryptMetadata))
	return encryptDict, nil
}

const adobePubSecFilter = "Adobe.PubSec"

// CheckAccessRightsWithCertificate reports whether the certificate can open a
// public-key encrypted document and the permissions its recipient entry
// grants, without authenticating the parser. An unencrypted document grants
// full permissions; a document using the standard security handler returns
// ErrPasswordRequired.
func (parser *PdfParser) CheckAccessRightsWithCertificate(cert *x509.Certificate, key crypto.PrivateKey) (bool, security.Permissions, error) {
	if parser.crypter == nil {
		return true, security.PermOwner, nil
	}
	return parser.crypter.checkAccessRightsCertificate(cert, key)
}

// IsPublicKey reports whether the document is encrypted with a public-key
// security handler (/Filter /Adobe.PubSec), which is opened with a recipient
// certificate and private key rather than a password.
func (crypt *PdfCrypt) IsPublicKey() bool { return crypt.isPubSec() }

func (crypt *PdfCrypt) pubSecKeyBits() int {
	if crypt.encrypt.V >= 4 {
		for _, name := range []string{crypt.recipientFilter, crypt.streamFilter, crypt.stringFilter} {
			if name == "" || name == "Identity" {
				continue
			}
			if filter, ok := crypt.cryptFilters[name]; ok && filter.KeyLength() > 0 {
				return filter.KeyLength() * 8
			}
		}
	}
	return crypt.encrypt.Length
}

func parsePubSecEncryptDict(crypt *PdfCrypt, ed *PdfObjectDictionary) (*security.PubSecEncryptDict, error) {
	pubSecDict := &security.PubSecEncryptDict{SubFilter: crypt.encrypt.SubFilter}
	switch pubSecDict.SubFilter {
	case security.PubSecSubFilterS4, security.PubSecSubFilterS5:
	case "":
		return nil, errors.New("public-key encrypt dictionary missing SubFilter")
	default:
		return nil, fmt.Errorf("unsupported public-key SubFilter (%s)", pubSecDict.SubFilter)
	}
	switch crypt.encrypt.V {
	case 1, 2, 4, 5:
	default:
		return nil, fmt.Errorf("unsupported V (%d) for public-key encryption", crypt.encrypt.V)
	}
	dict := ed
	crypt.recipientFilter = ""
	if crypt.encrypt.V >= 4 {
		findRecipientFilter := func(filterNames []string) bool {
			for _, name := range filterNames {
				if name == "" || name == "Identity" {
					continue
				}
				if filterDict := crypt.cryptFilterDict(ed, name); filterDict != nil && filterDict.Get("Recipients") != nil {
					dict = filterDict
					crypt.recipientFilter = name
					return true
				}
			}
			return false
		}
		strs := []string{crypt.streamFilter, crypt.stringFilter}
		if eff, ok := GetName(ed.Get("EFF")); ok {
			strs = append(strs, string(*eff))
		}
		if !findRecipientFilter(strs) && ed.Get("Recipients") == nil {
			var sortedKeys []string
			for sKey := range crypt.cryptFilters {
				sortedKeys = append(sortedKeys, sKey)
			}
			sort.Strings(sortedKeys)
			findRecipientFilter(sortedKeys)
		}
	}
	var err error
	pubSecDict.Recipients, err = parsePubSecRecipients(dict)
	if err != nil {
		return nil, err
	}
	if crypt.encrypt.V >= 4 {
		recipientFilter := crypt.recipientFilter
		if recipientFilter == "" {
			recipientFilter = "top-level /Recipients"
		}
		for sKey := range crypt.cryptFilters {
			if sKey == "Identity" || sKey == crypt.recipientFilter {
				continue
			}
			filterDict := crypt.cryptFilterDict(ed, sKey)
			if filterDict == nil || filterDict.Get("Recipients") == nil {
				continue
			}
			pubSecRecipients, err2 := parsePubSecRecipients(filterDict)
			if err2 != nil {
				return nil, err2
			}
			if !slices.EqualFunc(pubSecDict.Recipients, pubSecRecipients, bytes.Equal) {
				return nil, fmt.Errorf("unsupported public-key encryption: %s and crypt filter %s have different recipient lists", recipientFilter, sKey)
			}
		}
	}
	pubSecDict.EncryptMetadata = true
	if encryptMetadata, ok := GetBool(dict.Get("EncryptMetadata")); ok {
		pubSecDict.EncryptMetadata = bool(*encryptMetadata)
	} else if boolObj, hasEncryptMetadata := GetBool(ed.Get("EncryptMetadata")); hasEncryptMetadata && dict != ed {
		pubSecDict.EncryptMetadata = bool(*boolObj)
	}
	return pubSecDict, nil
}

func (crypt *PdfCrypt) unwrapRecipient(cert *x509.Certificate, privKey crypto.PrivateKey) ([]byte, [4]byte, int, error) {
	if !crypt.isPubSec() {
		return nil, [4]byte{}, -1, ErrPasswordRequired
	}
	return security.PubSecUnwrap(crypt.pubSec.Recipients, cert, privKey)
}

func (crypt *PdfCrypt) authenticateCertificate(cert *x509.Certificate, key crypto.PrivateKey) (bool, error) {
	data, perms, _, err := crypt.unwrapRecipient(cert, key)
	if err != nil {
		return false, err
	}
	pubSec := crypt.pubSec
	crypt.encryptionKey = security.PubSecFileKey(data, pubSec.Recipients, crypt.encrypt.V, crypt.pubSecKeyBits(), pubSec.EncryptMetadata)
	crypt.permissions = security.PubSecPermissions(perms)
	crypt.authenticated = true
	return true, nil
}

// DecryptWithCertificate authenticates a document encrypted with a public-key
// security handler (/Filter /Adobe.PubSec) using a recipient's certificate and
// private key, and builds the document key. The key must be an
// *rsa.PrivateKey: RSA key transport is the only recipient type supported.
// It returns security.ErrNotRecipient when the certificate is not among the
// recipients, security.ErrRecipientNotOpened when the recipient's entry does
// not open with the key, and ErrPasswordRequired when the document uses the
// standard security handler. The deferred xref health check behaves as for
// Decrypt.
func (parser *PdfParser) DecryptWithCertificate(cert *x509.Certificate, key crypto.PrivateKey) (bool, error) {
	if parser.crypter == nil {
		return false, errors.New("check encryption first")
	}
	ok, err := parser.crypter.authenticateCertificate(cert, key)
	if err != nil {
		return false, err
	}
	if ok {
		if err2 := parser.afterAuthentication(); err2 != nil {
			return true, err2
		}
	}
	return ok, nil
}

// PdfCryptNewEncryptPubSec makes the document crypt handler for the public-key
// security handler (/Filter /Adobe.PubSec), encrypting with cf for the given
// recipient certificates, which all receive perm and must carry RSA public
// keys. cf must be a crypt filter (AESV2 for V4, AESV3 for V5); the output
// uses SubFilter adbe.pkcs7.s5, the layout that carries /Recipients in the
// crypt filter dictionary.
func PdfCryptNewEncryptPubSec(cf crypt.Filter, recipients []*x509.Certificate, perm security.Permissions) (*PdfCrypt, *EncryptInfo, error) {
	if cf == nil {
		return nil, nil, errors.New("crypt filter is required")
	}
	V, _ := cf.HandlerVersion()
	if V < 4 {
		return nil, nil, fmt.Errorf("public-key encryption requires a crypt filter (AESV2 or AESV3), not %s", cf.Name())
	}
	crypt := &PdfCrypt{encryptedObjects: make(map[PdfObject]bool), cryptFilters: cryptFilters{defaultCryptFilterKey: cf}, streamFilter: defaultCryptFilterKey, stringFilter: defaultCryptFilterKey, recipientFilter: defaultCryptFilterKey}
	var version Version
	nums := cf.PDFVersion()
	version.Major, version.Minor = nums[0], nums[1]
	crypt.encrypt.Filter = adobePubSecFilter
	crypt.encrypt.V = V
	crypt.encrypt.Length = cf.KeyLength() * 8
	data := make([]byte, security.PubSecSeedLength)
	if _, err := rand.Read(data); err != nil {
		return nil, nil, fmt.Errorf("generating seed: %w", err)
	}
	permBytes := security.PubSecPermissionBytes(perm)
	datas, err := security.PubSecWrap(data, permBytes, recipients)
	if err != nil {
		return nil, nil, err
	}
	pubSecDict := &security.PubSecEncryptDict{SubFilter: security.PubSecSubFilterS5, Recipients: datas, EncryptMetadata: true}
	crypt.pubSec = pubSecDict
	crypt.encryptionKey = security.PubSecFileKey(data, datas, V, crypt.encrypt.Length, pubSecDict.EncryptMetadata)
	crypt.permissions = security.PubSecPermissions(permBytes)
	crypt.authenticated = true
	id0, id1 := generateFileIDs()
	crypt.id0 = id0
	encryptPubSec, err := crypt.encodeEncryptPubSec()
	if err != nil {
		return nil, nil, err
	}
	return crypt, &EncryptInfo{Version: version, Encrypt: encryptPubSec, ID0: id0, ID1: id1}, nil
}

func (crypt *PdfCrypt) checkAccessRightsCertificate(cert *x509.Certificate, key crypto.PrivateKey) (bool, security.Permissions, error) {
	_, data, _, err := crypt.unwrapRecipient(cert, key)
	if err != nil {
		return false, 0, err
	}
	return true, security.PubSecPermissions(data), nil
}
