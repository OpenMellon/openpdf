// Package apicheck guards the exported API of every package in the module.
//
// The test lists each exported function, type, method, struct field (with its order), constant and variable in a
// normalised form (parameter and result names dropped at the top level of a signature, so renaming one is not an API
// change) and compares it with testdata/api.txt. Named non-struct types also record their underlying type, and
// generic types their type parameters. A refactor that only moves code between files must leave it untouched. After
// an intentional API change run go test ./internal/apicheck -update and review the diff.
//
// It covers internal and main packages too, so the golden also notices changes there; it is generated on the host's
// GOOS/GOARCH (the module has no build-tagged non-test files). Apart from this file the package contains only tests.
package apicheck
