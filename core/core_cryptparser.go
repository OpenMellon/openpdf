package core

import (
	"errors"
	"fmt"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core/security"
)

func (parser *PdfParser) afterAuthentication() error {
	parser.clearObjCache()
	var err error
	if parser.pendingXrefRepair {
		parser.pendingXrefRepair = false
		err = parser.checkAndRepairXrefTable(parser.trailer)
	}
	if parser.encryptObj != nil {
		parser.StoreCachedObject(int(parser.encryptObj.ObjectNumber), parser.encryptObj)
	}
	return err
}

// IsEncrypted checks if the document is encrypted. A bool flag is returned indicating the result.
// First time when called, will check if the Encrypt dictionary is accessible through the trailer dictionary.
// If encrypted, prepares a crypt datastructure which can be used to authenticate and decrypt the document.
// On failure, an error is returned.
func (parser *PdfParser) IsEncrypted() (bool, error) {
	if parser.crypter != nil {
		return true, nil
	} else if parser.trailer == nil {
		return false, nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Checking encryption dictionary!")
	}
	encrypt := parser.trailer.Get("Encrypt")
	if encrypt == nil {
		return false, nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Is encrypted!")
	}
	var dict *PdfObjectDictionary
	switch v := encrypt.(type) {
	case *PdfObjectDictionary:
		dict = v
	case *PdfObjectReference:
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("0: Look up ref %q", v)
		}
		byReference, err := parser.LookupByReference(*v)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("1: %q", byReference)
		}
		if err != nil {
			return false, err
		}
		indirect, ok := byReference.(*PdfIndirectObject)
		if !ok {
			common.Log.Debug("Encryption object not an indirect object")
			return false, errors.New("type check error")
		}
		indirectDict, ok := indirect.PdfObject.(*PdfObjectDictionary)
		parser.encryptObj = indirect
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("2: %q", indirectDict)
		}
		if !ok {
			return false, errors.New("trailer Encrypt object non dictionary")
		}
		dict = indirectDict
	case *PdfObjectNull:
		common.Log.Debug("Encrypt is a null object. File should not be encrypted.")
		return false, nil
	default:
		return false, fmt.Errorf("unsupported type: %T", v)
	}
	crypt, err := PdfCryptNewDecrypt(parser, dict, parser.trailer)
	if err != nil {
		return false, err
	}
	for _, key := range []string{"Encrypt"} {
		obj := parser.trailer.Get(PdfObjectName(key))
		if obj == nil {
			continue
		}
		switch v := obj.(type) {
		case *PdfObjectReference:
			crypt.encryptedObjNums[int(v.ObjectNumber)] = struct{}{}
		case *PdfIndirectObject:
			crypt.decryptedObjects[v] = true
			crypt.encryptedObjNums[int(v.ObjectNumber)] = struct{}{}
		}
	}
	parser.crypter = crypt
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Crypter object %b", crypt)
	}
	return true, nil
}

// GetCrypter returns the PdfCrypt instance which has information about the PDFs encryption.
func (parser *PdfParser) GetCrypter() *PdfCrypt { return parser.crypter }

// IsAuthenticated returns true if the PDF has already been authenticated for accessing.
func (parser *PdfParser) IsAuthenticated() bool { return parser.crypter.authenticated }

// GetEncryptObj returns the PdfIndirectObject which has information about the PDFs encryption details.
func (parser *PdfParser) GetEncryptObj() *PdfIndirectObject { return parser.encryptObj }

func (parser *PdfParser) trailerHasEncrypt(trailer *PdfObjectDictionary) bool {
	if trailer == nil {
		return false
	}
	switch v := trailer.Get("Encrypt").(type) {
	case *PdfObjectDictionary:
		return true
	case *PdfObjectReference:
		byReference, err := parser.LookupByReference(*v)
		if err != nil {
			return false
		}
		indirect, ok := byReference.(*PdfIndirectObject)
		if !ok {
			return false
		}
		_, isDict := indirect.PdfObject.(*PdfObjectDictionary)
		return isDict
	}
	return false
}

// Decrypt attempts to decrypt the PDF file with a specified password.  Also tries to
// decrypt with an empty password.  Returns true if successful, false otherwise.
// An error is returned when there is a problem with decrypting.
//
// Decrypt must not run concurrently with object lookups: on success it
// clears the object cache, and a lookup in flight during authentication
// could re-populate it with a still-encrypted object.
//
// With ParserOpts.AutoRepairXrefTable, the deferred xref table health check
// runs on the first successful authentication. If the table then cannot be
// rebuilt, the password is reported as accepted and an error is returned.
func (parser *PdfParser) Decrypt(password []byte) (bool, error) {
	if parser.crypter == nil {
		return false, errors.New("check encryption first")
	}
	ok, err := parser.crypter.authenticate(password)
	if err != nil {
		return false, err
	}
	if !ok {
		ok, err = parser.crypter.authenticate([]byte(""))
	}
	if ok {
		if err2 := parser.afterAuthentication(); err2 != nil {
			return true, err2
		}
	}
	return ok, err
}

// CheckAccessRights checks access rights and permissions for a specified password. If either user/owner password is
// specified, full rights are granted, otherwise the access rights are specified by the Permissions flag.
//
// The bool flag indicates that the user can access and view the file.
// The AccessPermissions shows what access the user has for editing etc.
// An error is returned if there was a problem performing the authentication.
func (parser *PdfParser) CheckAccessRights(password []byte) (bool, security.Permissions, error) {
	if parser.crypter == nil {
		return true, security.PermOwner, nil
	}
	return parser.crypter.checkAccessRights(password)
}
