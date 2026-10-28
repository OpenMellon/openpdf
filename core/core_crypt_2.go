package core

import (
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core/security"
	"github.com/OpenMellon/openpdf/core/security/crypt"
)

// String returns a descriptive information string about the encryption method used.
func (crypt *PdfCrypt) String() string {
	if crypt == nil {
		return ""
	}
	var out strings.Builder
	out.WriteString(crypt.encrypt.Filter + " - ")
	if crypt.isPubSec() {
		out.WriteString(fmt.Sprintf("%s (%d recipients) - ", crypt.pubSec.SubFilter, len(crypt.pubSec.Recipients)))
	}
	if crypt.encrypt.V == 0 {
		out.WriteString("Undocumented algorithm")
	} else if crypt.encrypt.V == 1 {
		out.WriteString("RC4: 40 bits")
	} else if crypt.encrypt.V == 2 {
		out.WriteString(fmt.Sprintf("RC4: %d bits", crypt.encrypt.Length))
	} else if crypt.encrypt.V == 3 {
		out.WriteString("Unpublished algorithm")
	} else if crypt.encrypt.V >= 4 {
		out.WriteString(fmt.Sprintf("Stream filter: %s - String filter: %s", crypt.streamFilter, crypt.stringFilter))
		out.WriteString("; Crypt filters:")
		for sKey, cryptFilter := range crypt.cryptFilters {
			out.WriteString(fmt.Sprintf(" - %s: %s (%d)", sKey, cryptFilter.Name(), cryptFilter.KeyLength()))
		}
	}
	accessPermissions := crypt.GetAccessPermissions()
	out.WriteString(fmt.Sprintf(" - %#v", accessPermissions))
	return out.String()
}

// GetAccessPermissions returns the PDF access permissions as an AccessPermissions object.
// For a public-key encrypted document they are the permissions granted to the
// authenticated recipient, and zero before authentication.
func (crypt *PdfCrypt) GetAccessPermissions() security.Permissions {
	if crypt.isPubSec() {
		return crypt.permissions
	}
	return crypt.stdEncrypt.P
}

func (crypt *PdfCrypt) encryptBytes(data []byte, filterName string, key []byte) ([]byte, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Encrypt bytes")
	}
	filter, ok := crypt.cryptFilters[filterName]
	if !ok {
		return nil, fmt.Errorf("unknown crypt filter (%s)", filterName)
	}
	return filter.EncryptBytes(data, key)
}

func (crypt *PdfCrypt) makeKey(filterName string, objNum, genNum uint32, fileKey []byte) ([]byte, error) {
	filter, ok := crypt.cryptFilters[filterName]
	if !ok {
		return nil, fmt.Errorf("unknown crypt filter (%s)", filterName)
	}
	return filter.MakeKey(objNum, genNum, fileKey)
}

func (crypt *PdfCrypt) cryptFilterDict(encryptDict *PdfObjectDictionary, filterName string) *PdfObjectDictionary {
	if filterName == "" {
		return nil
	}
	traceDirect := func(obj PdfObject) PdfObject {
		obj = TraceToDirectObject(obj)
		if ref, ok := obj.(*PdfObjectReference); ok && crypt.parser != nil {
			if byReference, err := crypt.parser.LookupByReference(*ref); err == nil {
				obj = TraceToDirectObject(byReference)
			}
		}
		return obj
	}
	cfDict, ok := traceDirect(encryptDict.Get("CF")).(*PdfObjectDictionary)
	if !ok {
		return nil
	}
	dict, _ := traceDirect(cfDict.Get(PdfObjectName(filterName))).(*PdfObjectDictionary)
	return dict
}

func (crypt *PdfCrypt) newEncryptDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName("Standard"))
	dict.Set("V", MakeInteger(int64(crypt.encrypt.V)))
	dict.Set("Length", MakeInteger(int64(crypt.encrypt.Length)))
	return dict
}

func (crypt *PdfCrypt) saveCryptFilters(dict *PdfObjectDictionary) error {
	if crypt.encrypt.V < 4 {
		return errors.New("can only be used with V>=4")
	}
	cfDict := MakeDict()
	dict.Set("CF", cfDict)
	for sKey, cryptFilter := range crypt.cryptFilters {
		if sKey == "Identity" {
			continue
		}
		cryptFilterDict := makeCryptFilterDict(cryptFilter, "")
		cfDict.Set(PdfObjectName(sKey), cryptFilterDict)
	}
	dict.Set("StrF", MakeName(crypt.stringFilter))
	dict.Set("StmF", MakeName(crypt.streamFilter))
	return nil
}

func (crypt *PdfCrypt) securityHandler() security.StdHandler {
	if crypt.stdEncrypt.R >= 5 {
		return security.NewHandlerR6()
	}
	return security.NewHandlerR4(crypt.id0, crypt.encrypt.Length)
}

func (c *PdfCrypt) loadCryptFilters(ed *PdfObjectDictionary) error {
	c.cryptFilters = cryptFilters{}
	cf := ed.Get("CF")
	cf = TraceToDirectObject(cf)
	if ref, ok := cf.(*PdfObjectReference); ok {
		byReference, err := c.parser.LookupByReference(*ref)
		if err != nil {
			common.Log.Debug("Error looking up CF reference")
			return err
		}
		cf = TraceToDirectObject(byReference)
	}
	dict, ok := cf.(*PdfObjectDictionary)
	if !ok {
		common.Log.Debug("Invalid CF, type: %T", cf)
		return errors.New("invalid CF")
	}
	for _, key := range dict.Keys() {
		obj := dict.Get(key)
		if ref, isRef := obj.(*PdfObjectReference); isRef {
			byReference, err := c.parser.LookupByReference(*ref)
			if err != nil {
				common.Log.Debug("Error lookup up dictionary reference")
				return err
			}
			obj = TraceToDirectObject(byReference)
		}
		cfDict, isDict := obj.(*PdfObjectDictionary)
		if !isDict {
			return fmt.Errorf("invalid dict in CF (name %s) - not a dictionary but %T", key, obj)
		}
		if key == "Identity" {
			common.Log.Debug("ERROR - Cannot overwrite the identity filter - Trying next")
			continue
		}
		var filterDict crypt.FilterDict
		if err := parseCryptFilterDict(&filterDict, cfDict); err != nil {
			return err
		}
		filter, err := crypt.NewFilter(filterDict)
		if err != nil {
			return err
		}
		c.cryptFilters[string(key)] = filter
	}
	c.cryptFilters["Identity"] = crypt.NewIdentity()
	c.stringFilter = "Identity"
	if strF, isName := ed.Get("StrF").(*PdfObjectName); isName {
		if _, ok2 := c.cryptFilters[string(*strF)]; !ok2 {
			return fmt.Errorf("crypt filter for StrF not specified in CF dictionary (%s)", *strF)
		}
		c.stringFilter = string(*strF)
	}
	c.streamFilter = "Identity"
	if stmF, isName := ed.Get("StmF").(*PdfObjectName); isName {
		if _, ok2 := c.cryptFilters[string(*stmF)]; !ok2 {
			return fmt.Errorf("crypt filter for StmF not specified in CF dictionary (%s)", *stmF)
		}
		c.streamFilter = string(*stmF)
	}
	return nil
}

func (crypt *PdfCrypt) generateParams(userPass, ownerPass []byte) error {
	stdHandler := crypt.securityHandler()
	data, err := stdHandler.GenerateParams(&crypt.stdEncrypt, ownerPass, userPass)
	if err != nil {
		return err
	}
	crypt.encryptionKey = data
	return nil
}

func (crypt *PdfCrypt) isPubSec() bool { return crypt.pubSec != nil }

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
