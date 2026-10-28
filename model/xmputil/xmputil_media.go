package xmputil

import (
	"time"
)

// MediaManagement are the values from the document media management metadata.
type MediaManagement struct {
	// OriginalDocumentID  as media is imported and projects is started, an original-document ID
	// must be created to identify a new document. This identifies a document as a conceptual entity.
	OriginalDocumentID GUID

	// DocumentID when a document is copied to a new file path or converted to a new format with
	// Save As, another new document ID should usually be assigned. This identifies a general version or
	// branch of a document. You can use it to track different versions or extracted portions of a document
	// with the same original-document ID.
	DocumentID GUID

	// InstanceID to track a document’s editing history, you must assign a new instance ID
	// whenever a document is saved after any changes. This uniquely identifies an exact version of a
	// document. It is used in resource references (to identify both the document or part itself and the
	// referenced or referencing documents), and in document-history resource events (to identify the
	// document instance that resulted from the change).
	InstanceID GUID

	// DerivedFrom references the source document from which this one is derived,
	// typically through a Save As operation that changes the file name or format. It is a minimal reference;
	// missing components can be assumed to be unchanged. For example, a new version might only need
	// to specify the instance ID and version number of the previous version, or a rendition might only need
	// to specify the instance ID and rendition class of the original.
	DerivedFrom *MediaManagementDerivedFrom

	// VersionID are meant to associate the document with a product version that is part of a release process. They can be useful in tracking the
	// document history, but should not be used to identify a document uniquely in any context.
	// Usually it simply works by incrementing integers 1,2,3...
	VersionID string

	// Versions is the history of the document versions along with the comments, timestamps and issuers.
	Versions []MediaManagementVersion
}

// MediaManagementDerivedFrom is a structure that contains references of identifiers and versions
// from which given document was derived.
type MediaManagementDerivedFrom struct {
	OriginalDocumentID GUID
	DocumentID         GUID
	InstanceID         GUID
	VersionID          string
}

// MediaManagementVersion is the version of the media management xmp metadata.
type MediaManagementVersion struct {
	VersionID  string
	ModifyDate time.Time
	Comments   string
	Modifier   string
}

// MediaManagementOptions are the options for the Media management xmp metadata.
type MediaManagementOptions struct {
	// OriginalDocumentID  as media is imported and projects is started, an original-document ID
	// must be created to identify a new document. This identifies a document as a conceptual entity.
	// By default, this value is generated.
	OriginalDocumentID string

	// NewDocumentID is a flag which generates a new Document identifier while setting media management.
	// This value should be set to true only if the document is stored and saved as new document.
	// Otherwise, if the document is modified and overwrites previous file, it should be set to false.
	NewDocumentID bool

	// DocumentID when a document is copied to a new file path or converted to a new format with
	// Save As, another new document ID should usually be assigned. This identifies a general version or
	// branch of a document. You can use it to track different versions or extracted portions of a document
	// with the same original-document ID.
	// By default, this value is generated if NewDocumentID is true or previous doesn't exist.
	DocumentID string

	// InstanceID to track a document’s editing history, you must assign a new instance ID
	// whenever a document is saved after any changes. This uniquely identifies an exact version of a
	// document. It is used in resource references (to identify both the document or part itself and the
	// referenced or referencing documents), and in document-history resource events (to identify the
	// document instance that resulted from the change).
	// By default, this value is generated.
	InstanceID string

	// DerivedFrom references the source document from which this one is derived,
	// typically through a Save As operation that changes the file name or format. It is a minimal reference;
	// missing components can be assumed to be unchanged. For example, a new version might only need
	// to specify the instance ID and version number of the previous version, or a rendition might only need
	// to specify the instance ID and rendition class of the original.
	// By default, the derived from structure is filled from previous XMP metadata (if exists).
	DerivedFrom string

	// VersionID are meant to associate the document with a product version that is part of a release process. They can be useful in tracking the
	// document history, but should not be used to identify a document uniquely in any context.
	// Usually it simply works by incrementing integers 1,2,3...
	// By default, this values is incremented or set to the next version number.
	VersionID string

	// ModifyComment is a comment to given modification
	ModifyComment string

	// ModifyDate is a custom modification date for the versions.
	// By default, this would be set to time.Now().
	ModifyDate time.Time

	// Modifier is a person who did the modification.
	Modifier string
}
