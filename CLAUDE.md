# openpdf

## Git

- Every change goes through a pull request; `main` is protected and cannot be pushed to directly. Branch from `main`
  (a plain branch is fine here, no worktree needed), keep commits small and focused, and run `go build ./...` and
  `go test ./...` (what CI runs) before pushing. Open the PR with the `dev-loop:pr-create` skill and merge only once CI
  is green.

## Examples

- Any new user-facing package, type or feature gets a Go testable example (`func ExampleXxx()` /
  `func Example_suffix()` in an external `package foo_test` file named `example_test.go`), per
  https://go.dev/blog/examples. Give it a deterministic `// Output:` comment so `go test` runs and checks it.
- Do not add `main`-package demo programs under `examples/`; they are not tested and go stale.
- Examples must write to `os.MkdirTemp` and clean up, never the working directory.
- Link new examples from the README feature table.

## Generated code

- Never edit `*_gen.go` files by hand. Change the generator or its input and run `go generate`. A PreToolUse hook
  (`.claude/hooks/block-gen-edits.sh`) blocks Edit/Write on them; it cannot see edits made through Bash, so don't
  use `sed` or heredocs to get round it.

## Documentation vs code

When a repository's documentation and its existing code disagree, follow the conventions in the existing code for the task in hand, then sort each inconsistency out rather than only reporting it (Ed, 30 Oct 2026). For each one, ask Ed how to proceed - one AskUserQuestion question per inconsistency, naming the doc and the code that disagree, with the options (fix the doc to match the code, change the code to match the doc, or leave it) and your recommendation first - then carry out the answer in the same session. Fix one yourself without asking only when the doc is plainly stale (it names a file, flag or value the code has since replaced) and the fix is to the doc alone. In your final reply, list each inconsistency and what was done about it.
