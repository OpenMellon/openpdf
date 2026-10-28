package model

import (
	"crypto/md5"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	_ "image/gif"
	"slices"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/core/security"
	"github.com/OpenMellon/openpdf/internal/lazystream"
	"github.com/OpenMellon/openpdf/model/internal/docutil"
)

// Encrypt encrypts the output file with a specified user/owner password.
// An empty owner password is replaced by the user password, so the user
// password then grants full permissions regardless of options.Permissions.
//
// With AES_256bit the password bytes are interpreted as UTF-8 and prepared
// with the SASLprep profile (RFC 4013) before hashing, as required by
// ISO 32000-2; an error is returned if a password contains characters the
// profile prohibits (control characters, bidi violations, ...) or consists
// only of characters it removes. Input that is not valid UTF-8 is used as
// given without preparation. With the other algorithms the password bytes
// are used as given.
func (w *PdfWriter) Encrypt(userPass, ownerPass []byte, options *EncryptOptions) error {
	algo := RC4_128bit
	perms := security.PermOwner
	if options != nil {
		algo = options.Algorithm
		perms = options.Permissions
	}
	cryptFilter, err := newCryptFilter(algo)
	if err != nil {
		return err
	}
	pdfCryptNewEncrypt, encryptInfo, err := core.PdfCryptNewEncrypt(cryptFilter, userPass, ownerPass, perms)
	if err != nil {
		return err
	}
	w.setEncryption(pdfCryptNewEncrypt, encryptInfo)
	return nil
}

func (w *PdfWriter) addObjects(obj core.PdfObject) error {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Adding objects!")
	}
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Indirect")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("- %s (%p)", obj, indirect)
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("- %s", indirect.PdfObject)
		}
		if w.addObject(indirect) {
			err := w.addObjects(indirect.PdfObject)
			if err != nil {
				return err
			}
		}
		return nil
	}
	if stream, ok := obj.(*core.PdfObjectStream); ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Stream")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("- %s %p", obj, obj)
		}
		if w.addObject(stream) {
			err := w.addObjects(stream.PdfObjectDictionary)
			if err != nil {
				return err
			}
		}
		return nil
	}
	if dict, ok := obj.(*core.PdfObjectDictionary); ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Dict")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("- %s", obj)
		}
		for _, name := range dict.Keys() {
			obj := dict.Get(name)
			if ref, ok := obj.(*core.PdfObjectReference); ok {
				obj = ref.Resolve()
				dict.Set(name, obj)
			}
			if name != "Parent" {
				if err := w.addObjects(obj); err != nil {
					return err
				}
			} else {
				if _, ok := obj.(*core.PdfObjectNull); ok {
					continue
				}
				if ok := w.hasObject(obj); !ok {
					common.Log.Debug("Parent obj not added yet!! %T %p %v", obj, obj, obj)
					w.missingParents[obj] = append(w.missingParents[obj], dict)
				}
			}
		}
		return nil
	}
	if arr, ok := obj.(*core.PdfObjectArray); ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Array")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("- %s", obj)
		}
		if arr == nil {
			return errors.New("array is nil")
		}
		for i, obj := range arr.Elements() {
			if ref, ok := obj.(*core.PdfObjectReference); ok {
				obj = ref.Resolve()
				arr.Set(i, obj)
			}
			if err := w.addObjects(obj); err != nil {
				return err
			}
		}
		return nil
	}
	if _, ok := obj.(*core.PdfObjectReference); ok {
		common.Log.Debug("ERROR: Cannot be a reference - got %#v!", obj)
		return errors.New("reference not allowed")
	}
	return nil
}

func (w *PdfWriter) excludedFromObjectStream(obj core.PdfObject) bool {
	if w.encryptObj != nil && obj == w.encryptObj {
		return true
	}
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		if _, ok := indirect.PdfObject.(*pdfSignDictionary); ok {
			return true
		}
	}
	return false
}

func (w *PdfWriter) optimizeDocument() error {
	if w.standardApplier == nil {
		return nil
	}
	infoObjDict, ok := core.GetDict(w.infoObj)
	if !ok {
		return errors.New("an info object is not a dictionary")
	}
	document := docutil.Document{ID: [2]string{w.id0, w.id1}, Version: w.version, Objects: w.objects, Info: infoObjDict, Crypt: w.crypter, UseHashBasedID: w.useHashBasedID}
	if err := w.standardApplier.ApplyStandard(&document); err != nil {
		return err
	}
	w.id0, w.id1 = document.ID[0], document.ID[1]
	w.version = document.Version
	w.objects = document.Objects
	w.infoObj.PdfObject = document.Info
	w.useHashBasedID = document.UseHashBasedID
	w.crypter = document.Crypt
	set := make(map[core.PdfObject]struct{}, len(w.objects))
	for _, object := range w.objects {
		set[object] = struct{}{}
	}
	w.hasObjects = set
	return nil
}

// SetFileName sets the pdf writer file name for metered usage tracker.
func (w *PdfWriter) SetFileName(name string) { w.fileName = name }

// EncryptForRecipients encrypts the document with the public-key security
// handler (/Filter /Adobe.PubSec) for the given recipient certificates, which
// must carry RSA public keys. Recipients open the document with their
// certificate and private key (PdfReader.DecryptWithCertificate or
// ReaderOpts); there is no password. All recipients get the permissions in
// options; granting every permission (security.PermOwner) makes them
// owner-equivalent. The cipher is AES_256bit (V5, PDF 2.0) unless options
// names AES_128bit (V4, PDF 1.5); RC4 is not available with this handler. A
// higher version already set on the writer is kept.
func (w *PdfWriter) EncryptForRecipients(recipients []*x509.Certificate, options *EncryptOptions) error {
	algo := AES_256bit
	perms := security.PermOwner
	if options != nil {
		if options.Algorithm != RC4_128bit {
			algo = options.Algorithm
		}
		perms = options.Permissions
	}
	cryptFilter, err := newCryptFilter(algo)
	if err != nil {
		return err
	}
	pdfCryptNewEncryptPubSec, encryptInfo, err := core.PdfCryptNewEncryptPubSec(cryptFilter, recipients, perms)
	if err != nil {
		return err
	}
	if w.version.Major > encryptInfo.Major || (w.version.Major == encryptInfo.Major && w.version.Minor > encryptInfo.Minor) {
		encryptInfo.Version = w.version
	}
	w.setEncryption(pdfCryptNewEncryptPubSec, encryptInfo)
	return nil
}

// SetLinearizationOptions sets the options that apply when linearized output
// is enabled via SetLinearized.
func (w *PdfWriter) SetLinearizationOptions(opts LinearizationOptions) { w.linearizationOptions = opts }

// AddExtension adds the specified extension to the Extensions dictionary.
// See section 7.1.2 "Extensions Dictionary" (pp. 108-109 PDF32000_2008).
func (w *PdfWriter) AddExtension(extName, baseVersion string, extLevel int) {
	extensions, ok := core.GetDict(w.catalog.Get("Extensions"))
	if !ok {
		extensions = core.MakeDict()
		w.catalog.Set("Extensions", extensions)
	}
	dict, ok := core.GetDict(extensions.Get(core.PdfObjectName(extName)))
	if !ok {
		dict = core.MakeDict()
		extensions.Set(core.PdfObjectName(extName), dict)
	}
	if s, _ := core.GetNameVal(dict.Get("BaseVersion")); s != baseVersion {
		dict.Set("BaseVersion", core.MakeName(baseVersion))
	}
	if extensionLevel, _ := core.GetIntVal(dict.Get("ExtensionLevel")); extensionLevel != extLevel {
		dict.Set("ExtensionLevel", core.MakeInteger(int64(extLevel)))
	}
}

func (w *PdfWriter) flushWriter() error {
	if w.writeErr == nil {
		w.writeErr = w.bufWriter.Flush()
	}
	return w.writeErr
}

// SetLinearized toggles linearized output for the next Write call. When
// enabled, the writer rearranges the document into the layout described by
// ISO 32000-1 Annex F: header → linearization parameter dictionary → first
// xref → hint stream → first-page section → remaining objects → main xref.
func (w *PdfWriter) SetLinearized(enabled bool) { w.linearized = enabled }

func (w *PdfWriter) optimize() error {
	if w.optimizer == nil {
		return nil
	}
	var err error
	w.objects, err = w.optimizer.Optimize(w.objects)
	if err != nil {
		return err
	}
	set := make(map[core.PdfObject]struct{}, len(w.objects))
	for _, object := range w.objects {
		set[object] = struct{}{}
	}
	w.hasObjects = set
	return nil
}

func (w *PdfWriter) addObject(obj core.PdfObject) bool {
	ok := w.hasObject(obj)
	if !ok {
		err := core.ResolveReferencesDeep(obj, w.traversed)
		if err != nil {
			common.Log.Debug("ERROR: %v - skipping", err)
		}
		w.objects = append(w.objects, obj)
		w.hasObjects[obj] = struct{}{}
		return true
	}
	return false
}

// SetOptimizer sets the optimizer to optimize PDF before writing.
func (w *PdfWriter) SetOptimizer(optimizer Optimizer) { w.optimizer = optimizer }

// SetOCProperties sets the optional content properties.
func (w *PdfWriter) SetOCProperties(ocProperties core.PdfObject) error {
	catalog := w.catalog
	if ocProperties != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Setting OC Properties...")
		}
		catalog.Set("OCProperties", ocProperties)
		return w.addObjects(ocProperties)
	}
	return nil
}

func (w *PdfWriter) finishLazyStreams(err error) {
	w.writeFailed = err != nil
	if err == nil {
		for _, lazyStream := range w.lazyStreams {
			removeLazyTempFile(lazyStream.TempFile)
			lazyStream.TempFile = ""
			if lazySource, ok := w.lazySources[lazyStream]; ok {
				lazySource.TempFile = ""
			}
		}
	} else if w.crypter != nil {
		lazystream.Drop(w.crypter)
	}
	w.lazySources, w.lazyStreams = nil, nil
}

func (w *PdfWriter) ensureLinearizedDocumentIDs(preObjs []preserializedObject) {
	if w.ids != nil {
		return
	}
	if w.id0 != "" && w.id1 != "" {
		w.ids = core.MakeArray(core.MakeHexString(w.id0), core.MakeHexString(w.id1))
		return
	}
	digest := md5.New()
	for _, preObj := range preObjs {
		digest.Write(preObj.data)
	}
	data := digest.Sum(nil)
	w.id0 = hex.EncodeToString(data[:8])
	w.id1 = hex.EncodeToString(data[8:])
	w.ids = core.MakeArray(core.MakeHexString(w.id0), core.MakeHexString(w.id1))
}

// SetForms sets the Acroform for a PDF file.
func (w *PdfWriter) SetForms(form *PdfAcroForm) error { w.acroForm = form; return nil }

// SetNameDictionary sets the Names entry in the PDF catalog.
// See section 7.7.4 "Name Dictionary" (p. 80 PDF32000_2008).
func (w *PdfWriter) SetNameDictionary(names core.PdfObject) error {
	if names == nil {
		return nil
	}
	w.names = newNamesFromPdfObject(names)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Setting catalog Names...")
	}
	w.catalog.Set("Names", names)
	return w.addObjects(names)
}

// AddOutlineTree adds outlines to a PDF file.
func (w *PdfWriter) AddOutlineTree(outlineTree *PdfOutlineTreeNode) {
	w.outlineTree = outlineTree
}

func (w *PdfWriter) hasOrphanPages() bool {
	for _, object := range w.objects {
		indirect, ok := object.(*core.PdfIndirectObject)
		if !ok {
			continue
		}
		dict, ok := core.GetDict(indirect.PdfObject)
		if !ok {
			continue
		}
		if s, _ := core.GetNameVal(dict.Get("Type")); s != "Page" {
			continue
		}
		if _, ok := w.pageDicts[dict]; !ok {
			return true
		}
	}
	return false
}

func (w *PdfWriter) makeOffSetReference(n int64) {
	s := fmt.Sprintf("startxref\n%d\n", n)
	w.writeString(s)
	w.writeString("%%EOF\n")
}

// SetOpenAction sets the OpenAction in the PDF catalog.
// The value shall be either an array defining a destination (12.3.2 "Destinations" PDF32000_2008),
// or an action dictionary representing an action (12.6 "Actions" PDF32000_2008).
func (w *PdfWriter) SetOpenAction(dest core.PdfObject) error {
	if dest == nil || core.IsNullObject(dest) {
		return nil
	}
	w.catalog.Set("OpenAction", dest)
	return w.addObjects(dest)
}

func (w *PdfWriter) mapObjectStreams(ok bool) (map[core.PdfObject]bool, bool) {
	set := make(map[core.PdfObject]bool)
	objs := make([]core.PdfObject, 0, len(w.objects))
	for _, object := range w.objects {
		objStreams, isObjStreams := object.(*core.PdfObjectStreams)
		if !isObjStreams {
			objs = append(objs, object)
			continue
		}
		n := 0
		for _, obj := range objStreams.Elements() {
			if w.excludedFromObjectStream(obj) {
				continue
			}
			set[obj] = true
			if indirect, ok := obj.(*core.PdfIndirectObject); ok {
				set[indirect.PdfObject] = true
			}
			n++
		}
		if n == 0 {
			delete(w.hasObjects, object)
			continue
		}
		ok = true
		objs = append(objs, object)
	}
	w.objects = objs
	return set, ok
}

func (w *PdfWriter) setHashIDs(digest hash.Hash) error {
	data := digest.Sum(nil)
	if w.id0 == "" {
		w.id0 = hex.EncodeToString(data[:8])
	}
	w.setDocumentIDs(w.id0, hex.EncodeToString(data[8:]))
	return nil
}

// GetOptimizer returns current PDF optimizer.
func (w *PdfWriter) GetOptimizer() Optimizer { return w.optimizer }

func (w *PdfWriter) setEncryption(crypter *core.PdfCrypt, encryptInfo *core.EncryptInfo) {
	w.crypter = crypter
	if encryptInfo.Major != 0 {
		w.SetVersion(encryptInfo.Major, encryptInfo.Minor)
	}
	w.encryptDict = encryptInfo.Encrypt
	w.id0, w.id1 = encryptInfo.ID0, encryptInfo.ID1
	if encryptObj := w.encryptObj; encryptObj != nil {
		delete(w.hasObjects, encryptObj)
		w.objects = slices.DeleteFunc(w.objects, func(obj core.PdfObject) bool { return obj == encryptObj })
	}
	indirectObject := core.MakeIndirectObject(encryptInfo.Encrypt)
	w.encryptObj = indirectObject
	w.addObject(indirectObject)
}

func (w *PdfWriter) hasObject(obj core.PdfObject) bool {
	_, ok := w.hasObjects[obj]
	return ok
}

// SetNamedDestinations sets the Dests entry in the PDF catalog.
// See section 12.3.2.3 "Named Destinations" (p. 367 PDF32000_2008).
func (w *PdfWriter) SetNamedDestinations(dests core.PdfObject) error {
	if dests == nil {
		return nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Setting catalog Dests...")
	}
	w.catalog.Set("Dests", dests)
	return w.addObjects(dests)
}
