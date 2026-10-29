# openpdf

## Git

- Commit straight to `main`. No feature branches, worktrees or PRs for this repo; this overrides the global
  "new branch or worktree for every PR" rule. Keep commits small and focused, and run `go build ./...` and
  `go test ./...` (what CI runs) before pushing.

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

When a repository's documentation and its existing code disagree, follow the conventions in the existing code. In your final reply, list each inconsistency you found between the documentation and the code.
