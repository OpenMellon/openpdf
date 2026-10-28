package core

// ParserOpts defines configuration options for Parser.
// These options allow setting customization flags to control parsing behaviors.
type ParserOpts struct {
	// RelaxedMode enables tolerant parsing by ignoring certain non-critical PDF errors.
	// When set to true, the parser will attempt to continue processing despite certain
	// structural errors such as:
	// - Missing or invalid /XRefStm entries
	// - Non-/Page or non-/Pages objects found in the page tree (/Kids array)
	RelaxedMode bool

	// AutoRepairXrefTable enables automatic repair of the xref table when it is found to be malformed or missing objects.
	// The check runs when the parser is constructed; for encrypted files it is
	// deferred to Decrypt, so a file that is never decrypted is never repaired.
	AutoRepairXrefTable bool

	// MaxDecodedStreamSize is the maximum number of bytes a single stream, or a single filter
	// stage of a filter chain, may decode to. 0 selects DefaultMaxDecodedStreamSize.
	// Decoding past the limit fails with ErrDecodedSizeLimitExceeded.
	// Not covered: decoders registered with RegisterCustomStreamEncoder, and inline images,
	// which have no stream object and use DefaultMaxDecodedStreamSize. JBIG2 is checked against
	// the page size its page information segment declares; a striped page that leaves its
	// height open is checked after decoding, bounded by the decoder's 256 MiB per-bitmap cap.
	MaxDecodedStreamSize int64

	// MaxDecodedTotalSize is the maximum number of bytes that stream decodes through the parser
	// and its revisions may produce together. Every Flate, LZW, RunLength, ASCIIHex, ASCII85,
	// CCITTFax, DCT and JBIG2 decode counts, including repeated decodes of the same stream, each stage of
	// a filter chain, and the object, cross-reference and hint streams the parser decodes itself.
	// Concurrent decodes each check the budget when they start, so together they can exceed it
	// by up to one MaxDecodedStreamSize each. 0 disables the limit.
	MaxDecodedTotalSize int64
}

// ParserMetadata is the parser based metadata information about document.
// The data here could be used on document verification.
type ParserMetadata struct {
	headerPosition             int
	eolAfterHeader             bool
	headerCommentBytes         [4]byte
	dataAfterEOF               bool
	oddLengthHexStrings        bool
	invalidHexRunes            bool
	nonConformantStream        bool
	invalidSubsectionHeader    bool
	invalidSeparationAfterXRef bool
}

// HasOddLengthHexStrings checks if the document has odd length hexadecimal strings.
func (meta ParserMetadata) HasOddLengthHexStrings() bool { return meta.oddLengthHexStrings }

// HasInvalidSubsectionHeader implements core.ParserMetadata interface.
func (meta ParserMetadata) HasInvalidSubsectionHeader() bool { return meta.invalidSubsectionHeader }

// HasInvalidSeparationAfterXRef implements core.ParserMetadata interface.
func (meta ParserMetadata) HasInvalidSeparationAfterXRef() bool {
	return meta.invalidSeparationAfterXRef
}

// HeaderCommentBytes gets the header comment bytes.
func (meta ParserMetadata) HeaderCommentBytes() [4]byte { return meta.headerCommentBytes }

// HasEOLAfterHeader gets information if there is a EOL after the version header.
func (meta ParserMetadata) HasEOLAfterHeader() bool { return meta.eolAfterHeader }

// HasDataAfterEOF checks if there is some data after EOF marker.
func (meta ParserMetadata) HasDataAfterEOF() bool { return meta.dataAfterEOF }

// HasNonConformantStream implements core.ParserMetadata.
func (meta ParserMetadata) HasNonConformantStream() bool { return meta.nonConformantStream }

// HasInvalidHexRunes implements core.ParserMetadata interface.
func (meta ParserMetadata) HasInvalidHexRunes() bool { return meta.invalidHexRunes }

// HeaderPosition gets the file header position.
func (meta ParserMetadata) HeaderPosition() int { return meta.headerPosition }
