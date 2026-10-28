# Regression goldens and inputs

- `<scenario>/` holds the goldens for one scenario (see `internal/regress/doc.go`). Regenerate with
  `go test ./internal/regress -update`.
- `inputs/` holds PDFs that OpenPDF reads rather than writes. They were authored for this repo, so there is no
  third-party licence to track. All but the three encrypted files are written byte for byte by
  `TestGenerateInputs` (`go test ./internal/regress -run TestGenerateInputs -gen-inputs`); the encrypted ones use
  OpenPDF's writer once, with random salts, so regenerating them changes their bytes and dumps.

| File | What it exercises |
|---|---|
| `xref-table.pdf` | classic cross-reference table |
| `incremental-update.pdf` | a later revision replaces an object, `/Prev` chain |
| `damaged-xref.pdf` | wrong xref offsets and `startxref`, forcing repair |
| `xref-stream.pdf` | xref stream plus an object stream |
| `inline-image.pdf` | `BI`/`ID`/`EI` inline image with `/AHx` |
| `stream-filters.pdf` | ASCII85+Flate, RunLength and ASCIIHex content streams |
| `page-tree.pdf` | nested page tree with inherited `MediaBox` and `/Rotate` |
| `acroform.pdf` | text field with an appearance stream |
| `cid-identity-h.pdf` | embedded TrueType as Type 0 / CIDFontType2, Identity-H, ToUnicode |
| `rc4-128.pdf`, `aes-128.pdf`, `aes-256.pdf` | decryption (user password `user`) |

Real-world PDFs (for example from the qpdf test suite, Apache-2.0) can be added here once each file's licence is
confirmed; list provenance in this file when they are.

To run the smoke test over a larger private corpus without committing it, point `OPENPDF_CORPUS` at a directory:

    OPENPDF_CORPUS=~/pdfs OPENPDF_REGRESS_OUT=/tmp/out go test ./internal/regress -run TestCorpus -v

It writes `corpus.csv` (status, pages, text and render hashes, timings per file) to `OPENPDF_REGRESS_OUT`.
