// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package sighandler implements digital signature handlers for PDF signature validation and signing.
//
// A handler's Validate checks signature integrity only: the signature value
// is verified with the certificate embedded in the document, so IsVerified
// holds for any well-formed signature, including one made with a self-signed
// certificate. Whether the signer is trusted is decided separately by
// model.PdfReader.ValidateSignaturesWithOpts against caller-supplied roots.
package sighandler
