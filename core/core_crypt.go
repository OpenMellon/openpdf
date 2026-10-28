package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core/security"
	"github.com/OpenMellon/openpdf/core/security/crypt"
)

func (crypt *PdfCrypt) decryptBytes(stream []byte, filterName string, objKey []byte) ([]byte, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Decrypt bytes")
	}
	filter, ok := crypt.cryptFilters[filterName]
	if !ok {
		return nil, fmt.Errorf("unknown crypt filter (%s)", filterName)
	}
	return filter.DecryptBytes(stream, objKey)
}

// Decrypt an object with specified key. For numbered objects,
// the key argument is not used and a new one is generated based
// on the object and generation number.
// Traverses through all the subobjects (recursive).
//
// Does not look up references..  That should be done prior to calling.
func (crypt *PdfCrypt) Decrypt(obj PdfObject, parentObjNum, parentGenNum int64) error {
	if crypt.isDecrypted(obj) {
		return nil
	}
	switch v := obj.(type) {
	case *PdfIndirectObject:
		crypt.decryptedObjects[v] = true
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Decrypting indirect %d %d obj!", v.ObjectNumber, v.GenerationNumber)
		}
		objectNumber := v.ObjectNumber
		generationNumber := v.GenerationNumber
		err := crypt.Decrypt(v.PdfObject, objectNumber, generationNumber)
		if err != nil {
			return err
		}
		return nil
	case *PdfObjectStream:
		crypt.decryptedObjects[v] = true
		pdfObjectDictionary := v.PdfObjectDictionary
		if crypt.stdEncrypt.R != 5 {
			if typeName, ok := pdfObjectDictionary.Get("Type").(*PdfObjectName); ok && *typeName == "XRef" {
				return nil
			}
		}
		objectNumber := v.ObjectNumber
		generationNumber := v.GenerationNumber
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Decrypting stream %d %d !", objectNumber, generationNumber)
		}
		filterName := stdCryptFilterName
		if crypt.encrypt.V >= 4 {
			filterName = crypt.streamFilter
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("this.streamFilter = %s", crypt.streamFilter)
			}
			if filter, ok := pdfObjectDictionary.Get("Filter").(*PdfObjectArray); ok {
				if name, hasName := GetName(filter.Get(0)); hasName {
					if *name == "Crypt" {
						filterName = "Identity"
						if decodeParms, isDict := pdfObjectDictionary.Get("DecodeParms").(*PdfObjectDictionary); isDict {
							if filterName2, isName := decodeParms.Get("Name").(*PdfObjectName); isName {
								if _, ok2 := crypt.cryptFilters[string(*filterName2)]; ok2 {
									if common.Log.IsLogLevel(common.LogLevelTrace) {
										common.Log.Trace("Using stream filter %s", *filterName2)
									}
									filterName = string(*filterName2)
								}
							}
						}
					}
				}
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("with %s filter", filterName)
			}
			if filterName == "Identity" {
				return nil
			}
		}
		err := crypt.Decrypt(pdfObjectDictionary, objectNumber, generationNumber)
		if err != nil {
			return err
		}
		key, err := crypt.makeKey(filterName, uint32(objectNumber), uint32(generationNumber), crypt.encryptionKey)
		if err != nil {
			return err
		}
		v.Stream, err = crypt.decryptBytes(v.Stream, filterName, key)
		if err != nil {
			return err
		}
		pdfObjectDictionary.Set("Length", MakeInteger(int64(len(v.Stream))))
		return nil
	case *PdfObjectString:
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Decrypting string!")
		}
		filterName := stdCryptFilterName
		if crypt.encrypt.V >= 4 {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("with %s filter", crypt.stringFilter)
			}
			if crypt.stringFilter == "Identity" {
				return nil
			}
			filterName = crypt.stringFilter
		}
		key, err := crypt.makeKey(filterName, uint32(parentObjNum), uint32(parentGenNum), crypt.encryptionKey)
		if err != nil {
			return err
		}
		str := v.Str()
		data := make([]byte, len(str))
		for i := 0; i < len(str); i++ {
			data[i] = str[i]
		}
		if len(data) > 0 {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Decrypt string: %s : % x", data, data)
			}
			data, err = crypt.decryptBytes(data, filterName, key)
			if err != nil {
				return err
			}
		}
		v.val = string(data)
		return nil
	case *PdfObjectArray:
		for _, elem := range v.Elements() {
			err := crypt.Decrypt(elem, parentObjNum, parentGenNum)
			if err != nil {
				return err
			}
		}
		return nil
	case *PdfObjectDictionary:
		ok := false
		if typeVal := v.Get("Type"); typeVal != nil {
			name, isName := typeVal.(*PdfObjectName)
			if isName && *name == "Sig" {
				ok = true
			}
		}
		for _, key := range v.Keys() {
			child := v.Get(key)
			if ok && string(key) == "Contents" {
				continue
			}
			if string(key) != "Parent" && string(key) != "Prev" && string(key) != "Last" {
				err := crypt.Decrypt(child, parentObjNum, parentGenNum)
				if err != nil {
					return err
				}
			}
		}
		return nil
	}
	return nil
}

// PdfCrypt provides PDF encryption/decryption support.
// The PDF standard supports encryption of strings and streams (Section 7.6).
type PdfCrypt struct {
	encrypt          encryptDict
	stdEncrypt       security.StdEncryptDict
	pubSec           *security.PubSecEncryptDict
	recipientFilter  string
	permissions      security.Permissions
	id0              string
	encryptionKey    []byte
	decryptedObjects map[PdfObject]bool
	encryptedObjects map[PdfObject]bool
	authenticated    bool
	cryptFilters     cryptFilters
	streamFilter     string
	stringFilter     string
	parser           *PdfParser
	encryptedObjNums map[int]struct{}
	lazyStreamData   map[*PdfObjectStream][]byte
}

// PdfCryptNewDecrypt makes the document crypt handler based on the encryption dictionary
// and trailer dictionary. Returns an error on failure to process.
func PdfCryptNewDecrypt(parser *PdfParser, ed, trailer *PdfObjectDictionary) (*PdfCrypt, error) {
	crypt := &PdfCrypt{authenticated: false, decryptedObjects: make(map[PdfObject]bool), encryptedObjects: make(map[PdfObject]bool), encryptedObjNums: make(map[int]struct{}), parser: parser}
	filter, ok := ed.Get("Filter").(*PdfObjectName)
	if !ok {
		common.Log.Debug("ERROR Crypt dictionary missing required Filter field!")
		return crypt, errors.New("required crypt field Filter missing")
	}
	switch *filter {
	case "Standard", adobePubSecFilter:
	default:
		common.Log.Debug("ERROR Unsupported filter (%s)", *filter)
		return crypt, errors.New("unsupported Filter")
	}
	crypt.encrypt.Filter = string(*filter)
	ok2 := crypt.encrypt.Filter == adobePubSecFilter
	if subFilter, hasSubFilter := GetName(ed.Get("SubFilter")); hasSubFilter {
		crypt.encrypt.SubFilter = string(*subFilter)
	} else if str, ok3 := GetString(ed.Get("SubFilter")); ok3 {
		crypt.encrypt.SubFilter = str.Str()
	}
	if crypt.encrypt.SubFilter != "" {
		common.Log.Debug("Using subfilter %s", crypt.encrypt.SubFilter)
	}
	if L, isNum := ed.Get("Length").(*PdfObjectInteger); isNum {
		if (*L % 8) != 0 {
			common.Log.Debug("ERROR Invalid encryption length")
			return crypt, errors.New("invalid encryption length")
		}
		crypt.encrypt.Length = int(*L)
	} else {
		crypt.encrypt.Length = 40
	}
	crypt.encrypt.V = 0
	if vNum, isNum := ed.Get("V").(*PdfObjectInteger); isNum {
		V := int(*vNum)
		crypt.encrypt.V = V
		if V >= 1 && V <= 2 {
			crypt.cryptFilters = defaultCryptFilters(crypt.encrypt.Length)
		} else if V >= 4 && V <= 5 {
			if err := crypt.loadCryptFilters(ed); err != nil {
				return crypt, err
			}
		} else {
			common.Log.Debug("ERROR Unsupported encryption algo V = %d", V)
			return crypt, errors.New("unsupported algorithm")
		}
	}
	if ok2 {
		pubSecEncryptDict, err := parsePubSecEncryptDict(crypt, ed)
		if err != nil {
			return crypt, err
		}
		crypt.pubSec = pubSecEncryptDict
	} else {
		if err := parseStdEncryptDict(&crypt.stdEncrypt, ed); err != nil {
			return crypt, err
		}
	}
	s := ""
	if idArr, isArr := trailer.Get("ID").(*PdfObjectArray); isArr && idArr.Len() >= 1 {
		str, hasStr := GetString(idArr.Get(0))
		if !hasStr {
			return crypt, errors.New("invalid trailer ID")
		}
		s = str.Str()
	} else {
		common.Log.Debug("Trailer ID array missing or invalid!")
	}
	crypt.id0 = s
	return crypt, nil
}

// PdfCryptNewEncrypt makes the document crypt handler based on a specified crypt filter.
func PdfCryptNewEncrypt(cf crypt.Filter, userPass, ownerPass []byte, perm security.Permissions) (*PdfCrypt, *EncryptInfo, error) {
	perm = perm.Normalized()
	if len(ownerPass) == 0 && len(userPass) > 0 {
		common.Log.Warning("Empty owner password: the user password is used as the owner password and grants full permissions")
		ownerPass = userPass
	}
	crypt := &PdfCrypt{encryptedObjects: make(map[PdfObject]bool), cryptFilters: make(cryptFilters), stdEncrypt: security.StdEncryptDict{P: perm, EncryptMetadata: true}}
	var version Version
	if cf != nil {
		nums := cf.PDFVersion()
		version.Major, version.Minor = nums[0], nums[1]
		V, R := cf.HandlerVersion()
		crypt.encrypt.V = V
		crypt.stdEncrypt.R = R
		crypt.encrypt.Length = cf.KeyLength() * 8
	}
	const (
		cryptFilterName = stdCryptFilterName
	)
	crypt.cryptFilters[cryptFilterName] = cf
	if crypt.encrypt.V >= 4 {
		crypt.streamFilter = cryptFilterName
		crypt.stringFilter = cryptFilterName
	}
	encryptDict := crypt.newEncryptDict()
	id0, id1 := generateFileIDs()
	crypt.id0 = id0
	err := crypt.generateParams(userPass, ownerPass)
	if err != nil {
		return nil, nil, err
	}
	writeStdEncryptDict(&crypt.stdEncrypt, encryptDict)
	if crypt.encrypt.V >= 4 {
		if err2 := crypt.saveCryptFilters(encryptDict); err2 != nil {
			return nil, nil, err2
		}
	}
	return crypt, &EncryptInfo{Version: version, Encrypt: encryptDict, ID0: id0, ID1: id1}, nil
}

func (crypt *PdfCrypt) setLazyStreamData(stream *PdfObjectStream, data []byte) {
	if crypt.lazyStreamData == nil {
		crypt.lazyStreamData = map[*PdfObjectStream][]byte{}
	}
	crypt.lazyStreamData[stream] = data
}

func (crypt *PdfCrypt) checkAccessRights(password []byte) (bool, security.Permissions, error) {
	if crypt.isPubSec() {
		return false, 0, ErrCertificateRequired
	}
	stdHandler := crypt.securityHandler()
	data, perms, err := stdHandler.Authenticate(&crypt.stdEncrypt, password)
	if err != nil {
		return false, 0, err
	} else if len(data) == 0 {
		return false, 0, nil
	}
	return true, perms, nil
}

func (crypt *PdfCrypt) authenticate(password []byte) (bool, error) {
	if crypt.isPubSec() {
		return false, ErrCertificateRequired
	}
	crypt.authenticated = false
	stdHandler := crypt.securityHandler()
	data, _, err := stdHandler.Authenticate(&crypt.stdEncrypt, password)
	if err != nil {
		return false, err
	} else if len(data) == 0 {
		return false, nil
	}
	crypt.authenticated = true
	crypt.encryptionKey = data
	return true, nil
}

// Encrypt an object with specified key. For numbered objects,
// the key argument is not used and a new one is generated based
// on the object and generation number.
// Traverses through all the subobjects (recursive).
//
// Does not look up references..  That should be done prior to calling.
func (crypt *PdfCrypt) Encrypt(obj PdfObject, parentObjNum, parentGenNum int64) error {
	if crypt.isEncrypted(obj) {
		return nil
	}
	switch v := obj.(type) {
	case *PdfIndirectObject:
		crypt.encryptedObjects[v] = true
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Encrypting indirect %d %d obj!", v.ObjectNumber, v.GenerationNumber)
		}
		objectNumber := v.ObjectNumber
		generationNumber := v.GenerationNumber
		err := crypt.Encrypt(v.PdfObject, objectNumber, generationNumber)
		if err != nil {
			return err
		}
		return nil
	case *PdfObjectStream:
		crypt.encryptedObjects[v] = true
		pdfObjectDictionary := v.PdfObjectDictionary
		if typeName, ok := pdfObjectDictionary.Get("Type").(*PdfObjectName); ok && *typeName == "XRef" {
			return nil
		}
		objectNumber := v.ObjectNumber
		generationNumber := v.GenerationNumber
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Encrypting stream %d %d !", objectNumber, generationNumber)
		}
		filterName := stdCryptFilterName
		if crypt.encrypt.V >= 4 {
			filterName = crypt.streamFilter
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("this.streamFilter = %s", crypt.streamFilter)
			}
			if filter, ok := pdfObjectDictionary.Get("Filter").(*PdfObjectArray); ok {
				if name, hasName := GetName(filter.Get(0)); hasName {
					if *name == "Crypt" {
						filterName = "Identity"
						if decodeParms, isDict := pdfObjectDictionary.Get("DecodeParms").(*PdfObjectDictionary); isDict {
							if filterName2, isName := decodeParms.Get("Name").(*PdfObjectName); isName {
								if _, ok2 := crypt.cryptFilters[string(*filterName2)]; ok2 {
									if common.Log.IsLogLevel(common.LogLevelTrace) {
										common.Log.Trace("Using stream filter %s", *filterName2)
									}
									filterName = string(*filterName2)
								}
							}
						}
					}
				}
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("with %s filter", filterName)
			}
			if filterName == "Identity" {
				if v.Lazy {
					crypt.setLazyStreamData(v, nil)
				}
				return nil
			}
		}
		err := crypt.Encrypt(v.PdfObjectDictionary, objectNumber, generationNumber)
		if err != nil {
			return err
		}
		key, err := crypt.makeKey(filterName, uint32(objectNumber), uint32(generationNumber), crypt.encryptionKey)
		if err != nil {
			return err
		}
		if v.Lazy {
			return crypt.encryptLazyStream(v, filterName, key)
		}
		v.Stream, err = crypt.encryptBytes(v.Stream, filterName, key)
		if err != nil {
			return err
		}
		pdfObjectDictionary.Set("Length", MakeInteger(int64(len(v.Stream))))
		return nil
	case *PdfObjectString:
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Encrypting string!")
		}
		filterName := stdCryptFilterName
		if crypt.encrypt.V >= 4 {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("with %s filter", crypt.stringFilter)
			}
			if crypt.stringFilter == "Identity" {
				return nil
			}
			filterName = crypt.stringFilter
		}
		key, err := crypt.makeKey(filterName, uint32(parentObjNum), uint32(parentGenNum), crypt.encryptionKey)
		if err != nil {
			return err
		}
		str := v.Str()
		data := make([]byte, len(str))
		for i := 0; i < len(str); i++ {
			data[i] = str[i]
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Encrypt string: %s : % x", data, data)
		}
		data, err = crypt.encryptBytes(data, filterName, key)
		if err != nil {
			return err
		}
		v.val = string(data)
		return nil
	case *PdfObjectArray:
		for _, elem := range v.Elements() {
			err := crypt.Encrypt(elem, parentObjNum, parentGenNum)
			if err != nil {
				return err
			}
		}
		return nil
	case *PdfObjectDictionary:
		ok := false
		if typeVal := v.Get("Type"); typeVal != nil {
			name, isName := typeVal.(*PdfObjectName)
			if isName && *name == "Sig" {
				ok = true
			}
		}
		for _, key := range v.Keys() {
			child := v.Get(key)
			if ok && string(key) == "Contents" {
				continue
			}
			if string(key) != "Parent" && string(key) != "Prev" && string(key) != "Last" {
				err := crypt.Encrypt(child, parentObjNum, parentGenNum)
				if err != nil {
					return err
				}
			}
		}
		return nil
	}
	return nil
}

func (crypt *PdfCrypt) takeLazyStreamData(stream *PdfObjectStream) ([]byte, bool) {
	data, ok := crypt.lazyStreamData[stream]
	if ok {
		delete(crypt.lazyStreamData, stream)
	}
	return data, ok
}

func (crypt *PdfCrypt) encryptLazyStream(stream *PdfObjectStream, filterName string, key []byte) error {
	if stream.TempFile == "" {
		return fmt.Errorf("lazy stream %d: already written (lazy streams are consumed by the first write)", stream.ObjectNumber)
	}
	file, err := os.ReadFile(filepath.Clean(stream.TempFile))
	if err != nil {
		return fmt.Errorf("lazy stream %d: reading temp file: %w", stream.ObjectNumber, err)
	}
	data, err := crypt.encryptBytes(file, filterName, key)
	if err != nil {
		return err
	}
	crypt.setLazyStreamData(stream, data)
	stream.Set("Length", MakeInteger(int64(len(data))))
	return nil
}

func (crypt *PdfCrypt) isEncrypted(obj PdfObject) bool {
	_, ok := crypt.encryptedObjects[obj]
	if ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Already encrypted")
		}
		return true
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Not encrypted yet")
	}
	return false
}

func (crypt *PdfCrypt) isDecrypted(obj PdfObject) bool {
	_, ok := crypt.decryptedObjects[obj]
	if ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Already decrypted")
		}
		return true
	}
	switch v := obj.(type) {
	case *PdfObjectStream:
		if crypt.stdEncrypt.R != 5 {
			if typeName, isName := v.Get("Type").(*PdfObjectName); isName && *typeName == "XRef" {
				return true
			}
		}
	case *PdfIndirectObject:
		if _, ok = crypt.encryptedObjNums[int(v.ObjectNumber)]; ok {
			return true
		}
		switch v2 := v.PdfObject.(type) {
		case *PdfObjectDictionary:
			if filter, isName := v2.Get("Filter").(*PdfObjectName); isName && *filter == adobePubSecFilter {
				return true
			}
			ok2 := true
			for _, encryptDictKey := range encryptDictKeys {
				if v2.Get(encryptDictKey) == nil {
					ok2 = false
					break
				}
			}
			if ok2 {
				return true
			}
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Not decrypted yet")
	}
	return false
}
func (crypt *PdfCrypt) dropLazyStreamData() { crypt.lazyStreamData = nil }

var ErrPasswordRequired = errors.New("document is encrypted with the standard security handler; a password is required")

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
