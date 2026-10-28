// Package regress holds golden-file regression tests for OpenPDF.
//
// Each scenario builds (or reads) a PDF, then compares four artefacts with the files under testdata/<name>/:
//
//   - doc.pdf: the exact bytes written, for scenarios whose output is deterministic
//   - doc.dump.txt: a normalised textual dump of every object, which is what to review in a diff
//   - text.txt: the text the extractor reads from every page
//   - page-N.png: every page rendered at 72 dpi, for scenarios whose text is embedded TrueType
//
// Run go test ./internal/regress -update after an intentional change to output, then review the diff of the dumps and
// the PNGs before committing them. The package contains only tests.
package regress
