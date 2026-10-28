package model

import (
	"bytes"
	"crypto/md5"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	_ "image/gif"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/core/security"
	"github.com/h2non/filetype"
	"golang.org/x/xerrors"
)

// NewEmbeddedFile constructs a new EmbeddedFile object from supplied file.
// The file type of the file would be detected automatically.
func NewEmbeddedFile(path string) (*EmbeddedFile, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	match, err := filetype.Match(file)
	if err != nil {
		common.Log.Debug("Failed to detect file type: %v", err)
	}
	data := md5.Sum(file)
	file2 := &EmbeddedFile{Name: filepath.Base(path), Content: file, FileType: match.MIME.Value, Hash: hex.EncodeToString(data[:])}
	return file2, nil
}

// NewEmbeddedFileFromObject construct a new EmbeddedFile from supplied object.
func NewEmbeddedFileFromObject(obj core.PdfObject) (*EmbeddedFile, error) {
	obj2 := core.TraceToDirectObject(obj)
	dict, ok := obj2.(*core.PdfObjectDictionary)
	if !ok {
		return nil, errors.New("invalid object")
	}
	f := core.TraceToDirectObject(dict.Get("F"))
	if f == nil {
		return nil, errors.New("invalid object structure")
	}
	stream, ok := f.(*core.PdfObjectStream)
	if !ok {
		return nil, errors.New("invalid stream")
	}
	dict2 := stream.PdfObjectDictionary
	params := dict2.Get("Params")
	if params == nil {
		return nil, errors.New("params object not available")
	}
	dict3, ok := params.(*core.PdfObjectDictionary)
	if !ok {
		return nil, errors.New("invalid Params object")
	}
	s := ""
	checkSum := dict3.Get("CheckSum")
	if checkSum != nil {
		s = checkSum.(*core.PdfObjectString).Str()
	}
	data, err := core.DecodeStream(stream)
	if err != nil {
		return nil, err
	}
	file := &EmbeddedFile{Content: data, Hash: s}
	return file, nil
}

// AddCerts adds certificates to DSS.
func (d *DSS) AddCerts(certs [][]byte) ([]*core.PdfObjectStream, error) {
	return d.add(&d.Certs, d.certsMap, certs)
}

// ToPdfObject implements interface PdfModel.
func (f *EmbeddedFile) ToPdfObject() core.PdfObject {
	flateEncoder := core.NewFlateEncoder()
	stream, err := core.MakeStream(f.Content, flateEncoder)
	if err != nil {
		common.Log.Debug("Failed to create embedded file stream: %v", err)
		return nil
	}
	dict := stream.PdfObjectDictionary
	dict.Set("Type", core.MakeName("EmbeddedFile"))
	dict.Set("Subtype", core.MakeEncodedString("text/plain", true))
	dict2 := core.MakeDict()
	dict2.Set("CheckSum", core.MakeString(f.Hash[:]))
	dict2.Set("Size", core.MakeInteger(int64(len(f.Content))))
	creationTime := f.CreationTime
	if creationTime.IsZero() {
		creationTime = time.Now()
	}
	modTime := f.ModTime
	if modTime.IsZero() {
		modTime = creationTime
	}
	date, err := NewPdfDateFromTime(creationTime)
	if err != nil {
		common.Log.Debug("Failed to create embedded file creation date: %v", err)
		return nil
	}
	date2, err := NewPdfDateFromTime(modTime)
	if err != nil {
		common.Log.Debug("Failed to create embedded file creation date: %v", err)
		return nil
	}
	dict2.Set("CreationDate", date.ToPdfObject())
	dict2.Set("ModDate", date2.ToPdfObject())
	dict.Set("Params", dict2)
	dict3 := core.MakeDict()
	dict3.Set(*core.MakeName("F"), stream)
	return dict3
}

// NewDSS returns a new DSS dictionary.
func NewDSS() *DSS {
	return &DSS{primitive: core.MakeIndirectObject(core.MakeDict()), VRI: map[string]*VRI{}}
}

// GenerateHashMaps generates DSS hashmaps for Certificates, OCSPs and CRLs to make sure they are unique.
func (d *DSS) GenerateHashMaps() error {
	generateHashMap, err := d.generateHashMap(d.Certs)
	if err != nil {
		return err
	}
	streamByKey, err := d.generateHashMap(d.OCSPs)
	if err != nil {
		return err
	}
	key, err := d.generateHashMap(d.CRLs)
	if err != nil {
		return err
	}
	d.certsMap = generateHashMap
	d.ocspsMap = streamByKey
	d.crlsMap = key
	return nil
}

var (
	ErrRequiredAttributeMissing = errors.New("required attribute missing")
	ErrInvalidAttribute         = errors.New("invalid attribute")
	ErrTypeCheck                = errors.New("type check")
	errRangeCheck               = errors.New("range check error")
	ErrEncrypted                = errors.New("file needs to be decrypted first")
	ErrNoFont                   = errors.New("font not defined")
	ErrFontNotSupported         = xerrors.Errorf("unsupported font (%w)", core.ErrNotSupported)
	ErrType1CFontNotSupported   = xerrors.Errorf("Type1C fonts are not currently supported (%w)", core.ErrNotSupported)
	ErrType3FontNotSupported    = xerrors.Errorf("Type3 fonts are not currently supported (%w)", core.ErrNotSupported)
	ErrTTCmapNotSupported       = xerrors.Errorf("unsupported TrueType cmap format (%w)", core.ErrNotSupported)
	ErrSignNotEnoughSpace       = xerrors.Errorf("insufficient space allocated for the signature contents")
	ErrSignNoCertificates       = xerrors.Errorf("could not retrieve certificate chain")
	ErrNoCertClient             = xerrors.Errorf("cert client is not set")
	ErrNoCrlClient              = xerrors.Errorf("crl client is not set")
	ErrNoOcspClient             = xerrors.Errorf("ocsp client is not set")
)

var ErrColorOutOfRange = errors.New("color out of range")

func (d *DSS) add(streams *[]*core.PdfObjectStream, streamByKey map[string]*core.PdfObjectStream, dataList [][]byte) ([]*core.PdfObjectStream, error) {
	streams2 := make([]*core.PdfObjectStream, 0, len(dataList))
	for _, data := range dataList {
		data2, err := sha1Digest(data)
		if err != nil {
			return nil, err
		}
		stream, ok := streamByKey[string(data2)]
		if !ok {
			stream, err = core.MakeStream(data, core.NewRawEncoder())
			if err != nil {
				return nil, err
			}
			streamByKey[string(data2)] = stream
			*streams = append(*streams, stream)
		}
		streams2 = append(streams2, stream)
	}
	return streams2, nil
}

type distributionPointName struct {
	FullName []asn1.RawValue `asn1:"optional,tag:0"`

	// RelativeName is nameRelativeToCRLIssuer, a single
	// RelativeDistinguishedName; only its presence matters.
	RelativeName asn1.RawValue `asn1:"optional,tag:1"`
}

func hasExtKeyUsage(cert *x509.Certificate, usage x509.ExtKeyUsage) bool {
	return slices.Contains(cert.ExtKeyUsage, usage)
}

type fetchResult struct {
	body []byte
	crl  *parsedCRL
	err  error
}

// GetContainingPdfObject returns the container of the DSS (indirect object).
func (d *DSS) GetContainingPdfObject() core.PdfObject { return d.primitive }

// DetermineColorspaceNameFromPdfObject determines PDF colorspace from a PdfObject.  Returns the colorspace name and
// an error on failure. If the colorspace was not found, will return an empty string.
func DetermineColorspaceNameFromPdfObject(obj core.PdfObject) (core.PdfObjectName, error) {
	var name *core.PdfObjectName
	var arr *core.PdfObjectArray
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		if arr2, ok := indirect.PdfObject.(*core.PdfObjectArray); ok {
			arr = arr2
		} else if name2, ok := indirect.PdfObject.(*core.PdfObjectName); ok {
			name = name2
		}
	} else if arr2, ok := obj.(*core.PdfObjectArray); ok {
		arr = arr2
	} else if name2, ok := obj.(*core.PdfObjectName); ok {
		name = name2
	}
	if name != nil {
		switch *name {
		case "DeviceGray", "DeviceRGB", "DeviceCMYK":
			return *name, nil
		case "Pattern":
			return *name, nil
		}
	}
	if arr != nil && arr.Len() > 0 {
		if name, ok := arr.Get(0).(*core.PdfObjectName); ok {
			switch *name {
			case "DeviceGray", "DeviceRGB", "DeviceCMYK":
				if arr.Len() == 1 {
					return *name, nil
				}
			case "CalGray", "CalRGB", "Lab":
				return *name, nil
			case "ICCBased", "Pattern", "Indexed":
				return *name, nil
			case "Separation", "DeviceN":
				return *name, nil
			}
		}
	}
	return "", nil
}

var documentSigningEKUOIDs = []asn1.ObjectIdentifier{{1, 3, 6, 1, 5, 5, 7, 3, 36}, {1, 3, 6, 1, 4, 1, 311, 10, 3, 12}, {1, 2, 840, 113583, 1, 1, 5}}

// ToPdfObject returns the PDF representation of the DSS dictionary.
func (d *DSS) ToPdfObject() core.PdfObject {
	dict := d.primitive.PdfObject.(*core.PdfObjectDictionary)
	dict.Clear()
	dict2 := core.MakeDict()
	for s, vri := range d.VRI {
		dict2.Set(*core.MakeName(s), vri.ToPdfObject())
	}
	dict.SetIfNotNil("Certs", streamsToArray(d.Certs))
	dict.SetIfNotNil("OCSPs", streamsToArray(d.OCSPs))
	dict.SetIfNotNil("CRLs", streamsToArray(d.CRLs))
	dict.Set("VRI", dict2)
	return d.primitive
}

func encodeImageData(encoder core.StreamEncoder, img *Image) ([]byte, error) {
	if _, ok := encoder.(*core.DCTEncoder); ok && img.canPassThroughJPEG() {
		return img.encodedData, nil
	}
	return encoder.EncodeBytes(img.Data)
}

func newDSSFromDict(indirect *core.PdfIndirectObject, dict *core.PdfObjectDictionary) (*DSS, error) {
	if indirect == nil {
		indirect = core.MakeIndirectObject(nil)
	}
	indirect.PdfObject = core.MakeDict()
	vriByKey := map[string]*VRI{}
	if vri, ok := core.GetDict(dict.Get("VRI")); ok {
		for _, name := range vri.Keys() {
			if dict, ok := core.GetDict(vri.Get(name)); ok {
				vriByKey[strings.ToUpper(name.String())] = newVRIFromDict(dict)
			}
		}
	}
	return &DSS{Certs: streamsFromObject(dict.Get("Certs")), OCSPs: streamsFromObject(dict.Get("OCSPs")), CRLs: streamsFromObject(dict.Get("CRLs")), VRI: vriByKey, primitive: indirect}, nil
}

func encryptedWriterCatalog(writer *PdfWriter) *core.PdfIndirectObject {
	if writer.crypter == nil {
		return nil
	}
	return writer.encryptObj
}

// Duplex represents the paper handling option that shall be used when printing
// the file from the print dialog.
type Duplex string

// Direction represents the predominant reading order for text.
type Direction string

func equalDistinguishedNames(data, rawIssuer []byte) bool {
	if bytes.Equal(data, rawIssuer) {
		return true
	}
	var rdnSequence, sequence pkix.RDNSequence
	if data2, err := asn1.Unmarshal(data, &rdnSequence); err != nil || len(data2) > 0 {
		return false
	}
	if data, err := asn1.Unmarshal(rawIssuer, &sequence); err != nil || len(data) > 0 || len(rdnSequence) != len(sequence) {
		return false
	}
	for i := range rdnSequence {
		if len(rdnSequence[i]) != len(sequence[i]) {
			return false
		}
		for j := range rdnSequence[i] {
			attributeTypeAndValue, value := rdnSequence[i][j], sequence[i][j]
			if !attributeTypeAndValue.Type.Equal(value.Type) {
				return false
			}
			s, ok := attributeTypeAndValue.Value.(string)
			s2, found := value.Value.(string)
			if !ok || !found {
				if !reflect.DeepEqual(attributeTypeAndValue.Value, value.Value) {
					return false
				}
				continue
			}
			if !strings.EqualFold(strings.Join(strings.Fields(s), " "), strings.Join(strings.Fields(s2), " ")) {
				return false
			}
		}
	}
	return true
}

// AddOCSPs adds OCSPs to DSS.
func (d *DSS) AddOCSPs(ocsps [][]byte) ([]*core.PdfObjectStream, error) {
	return d.add(&d.OCSPs, d.ocspsMap, ocsps)
}

// EmbeddedFile represents an embedded file.
type EmbeddedFile struct {
	Name         string
	Content      []byte
	FileType     string
	Description  string
	Relationship FileRelationship
	Hash         string
	CreationTime time.Time
	ModTime      time.Time
}

// EncryptionAlgorithm is used in EncryptOptions to change the default algorithm used to encrypt the document.
type EncryptionAlgorithm int

func (d *DSS) generateHashMap(streams []*core.PdfObjectStream) (map[string]*core.PdfObjectStream, error) {
	streamByKey := map[string]*core.PdfObjectStream{}
	for _, stream := range streams {
		data, err := core.DecodeStream(stream)
		if err != nil {
			return nil, err
		}
		data2, err := sha1Digest(data)
		if err != nil {
			return nil, err
		}
		streamByKey[string(data2)] = stream
	}
	return streamByKey, nil
}

// DSS represents a Document Security Store dictionary.
// The DSS dictionary contains both global and signature specific validation
// information. The certificates and revocation data in the `Certs`, `OCSPs`,
// and `CRLs` fields can be used to validate any signature in the document.
// Additionally, the VRI entry contains validation data per signature.
// The keys in the VRI entry are calculated as upper(hex(sha1(sig.Contents))).
// The values are VRI dictionaries containing certificates and revocation
// information used for validating a single signature.
// See ETSI TS 102 778-4 V1.1.1 for more information.
type DSS struct {
	primitive *core.PdfIndirectObject
	Certs     []*core.PdfObjectStream
	OCSPs     []*core.PdfObjectStream
	CRLs      []*core.PdfObjectStream
	VRI       map[string]*VRI
	certsMap  map[string]*core.PdfObjectStream
	ocspsMap  map[string]*core.PdfObjectStream
	crlsMap   map[string]*core.PdfObjectStream
}

type fetchKey struct {
	url        string
	certHash   [32]byte
	issuerHash [32]byte
}

// NewEmbeddedFileFromContent construct a new EmbeddedFile from supplied file content.
func NewEmbeddedFileFromContent(content []byte) (*EmbeddedFile, error) {
	match, err := filetype.Match(content)
	if err != nil {
		common.Log.Debug("Failed to detect file type: %v", err)
	}
	data := md5.Sum(content)
	file := &EmbeddedFile{Name: "attachment", Content: content, FileType: match.MIME.Type, Hash: hex.EncodeToString(data[:])}
	return file, nil
}

// EncryptOptions represents encryption options for an output PDF.
type EncryptOptions struct {
	// Permissions granted when the document is opened with the user password
	// (Encrypt) or to every recipient (EncryptForRecipients). The zero value
	// grants none; security.PermOwner grants all, as does a nil
	// EncryptOptions. Encrypt sets the reserved bits of /P itself.
	// EncryptForRecipients writes only bits 3-6 and 9-12, always sets bit 10
	// as PDF 2.0 requires, and makes recipients granted every permission
	// owner-equivalent (able to change the security settings).
	Permissions security.Permissions

	// Algorithm selects the cipher. The zero value is RC4_128bit, which
	// Encrypt uses as its default; EncryptForRecipients does not produce RC4
	// output and treats the zero value as AES_256bit.
	Algorithm EncryptionAlgorithm
}

// AddCRLs adds CRLs to DSS.
func (d *DSS) AddCRLs(crls [][]byte) ([]*core.PdfObjectStream, error) {
	return d.add(&d.CRLs, d.crlsMap, crls)
}
