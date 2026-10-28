# Contributing to OpenPDF

Thanks for helping improve OpenPDF. This guide covers how to report problems, set up a development environment and
get a pull request merged.

By taking part you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Before you start

- **Questions** go to [Discussions](https://github.com/OpenMellon/openpdf/discussions), not issues.
- **Bugs**: open a [bug report](https://github.com/OpenMellon/openpdf/issues/new?template=bug_report.yml) with a
  minimal Go program and, wherever possible, the PDF that triggers the problem.
- **Security vulnerabilities**: report them privately; see [SECURITY.md](SECURITY.md).
- **Large changes** (new packages, public API changes, behaviour changes): open an issue first so the approach can
  be agreed before you spend time on it. Small fixes can go straight to a pull request.

## Development setup

You need:

- Go, at the version in [`go.mod`](go.mod)
- [pre-commit](https://pre-commit.com/#install)

The formatters (`goimports`, `gofumpt`) are pinned as tools in `go.mod` and run through `go tool`, so there is
nothing else to install. Clone the repo and enable the hooks:

~~~
git clone https://github.com/OpenMellon/openpdf.git
cd openpdf
pre-commit install
~~~

The hooks format Go code, run `go vet`, `go mod tidy` and the tests, and check for secrets, spelling and workflow
errors. Run them across the whole repo with `pre-commit run --all-files`.

## Making a change

1. Fork the repo and create a branch from `main`.
2. Make your change. Keep each pull request to one logical change.
3. Add or update tests. Bug fixes should include a test that fails without the fix.
4. New user-facing packages, types or features need a Go [testable example](https://go.dev/blog/examples): an
   `ExampleXxx` function in an `example_test.go` file in the external `package foo_test`, with a deterministic
   `// Output:` comment so `go test` checks it. Examples write to `os.MkdirTemp` and clean up after themselves.
   Link new examples from the README feature table.
5. Check that it builds and passes the same checks CI runs:

   ~~~
   go build ./...
   go test ./...
   ~~~

## Updating the regression goldens

`internal/regress` builds PDFs with the creator, annotator and other packages and compares them with committed
goldens in `internal/regress/testdata/<scenario>/`: the exact PDF bytes (`doc.pdf`), a readable dump of every object
(`doc.dump.txt`), the extracted text (`text.txt`) and, for pages whose text is embedded TrueType, a PNG per page.

If a change alters output on purpose, regenerate and review the result before committing it:

~~~
go test ./internal/regress -update
git diff --stat internal/regress
~~~

Read the `doc.dump.txt` diffs to see what changed in the PDF and look at the changed `page-N.png` files. Commit them
with the change. If a golden fails and you did not mean to change output, set `OPENPDF_REGRESS_OUT=<dir>` to get the
actual files and a `diff-page-N.png` with the differing pixels in red.

The goldens also cover PDFs OpenPDF only reads (`internal/regress/testdata/inputs`). See
`internal/regress/testdata/README.md` for how they are made and how to run the corpus smoke test on your own PDFs.

The same package holds fuzz targets for the reader, content stream parser and CMap parser. `go test` runs their seeds and
any committed crashers; the Nightly workflow fuzzes each for five minutes. To fuzz one locally, run
`go test ./internal/regress -run '^$' -fuzz '^FuzzReader$' -fuzztime 1m`, and commit any file it writes under
`internal/regress/testdata/fuzz`.

## Pull requests

- Fill in the pull request template, including how you verified the change and the AI disclosure.
- If the change affects PDF output, say which viewers you checked it in and attach or add the sample PDFs used.
- Link the issue it fixes (`Closes #123`).
- CI must pass before a pull request is merged.

## Third-party code

Some packages (`freetype`, `garabic`, `go-xmp`, `pkcs7`, `timestamp`, `typesetting`, `unichart`, `unitype`) are
derived from other projects and keep their own licences; see [ACKNOWLEDGEMENTS.md](ACKNOWLEDGEMENTS.md). If you
bring in code from elsewhere, it must have a compatible licence and be credited there.

## Licence

By contributing, you agree that your contributions are licensed under the project's [MIT licence](LICENSE).
