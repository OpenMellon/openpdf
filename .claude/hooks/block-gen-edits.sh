#!/bin/bash
# Blocks hand edits to generated *_gen.go files; regenerate them instead.
f=$(jq -r '.tool_input.file_path // empty')
case "$f" in
*_gen.go)
	echo "BLOCKED: $f is generated and must never be edited by hand. Change the generator (or its input) and run 'go generate' instead." >&2
	exit 2
	;;
esac
exit 0
