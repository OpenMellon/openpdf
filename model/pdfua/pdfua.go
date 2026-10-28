// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package pdfua provides PDF/UA (ISO 14289) standard profiles for validating
// and applying accessibility requirements to PDF documents.
//
// Note that unlike PDF/A, applying a PDF/UA profile can only fix mechanical
// issues (identification metadata, viewer preferences, catalog entries).
// Semantic requirements - a complete and correct structure tree, alternate
// descriptions, reading order - must be authored, e.g. with
// creator.TagComponents. ApplyStandard returns an error when the document
// lacks the semantic foundation it cannot synthesize.
package pdfua
