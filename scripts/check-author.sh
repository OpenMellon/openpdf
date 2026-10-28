#!/usr/bin/env bash
# Rejects commits not authored and committed as OpenMellon.
#
# With no arguments (pre-commit hook) it checks the identity git is about to commit with.
# With a revision range (CI) it checks every commit in that range.
set -euo pipefail

ALLOWED_EMAIL="348942936+OpenMellon@users.noreply.github.com"

if [ $# -eq 0 ]; then
  idents=$(git var GIT_AUTHOR_IDENT; git var GIT_COMMITTER_IDENT)
else
  idents=$(git log --format='%an <%ae>%n%cn <%ce>' "$@")
fi

bad=$(grep -vF "<$ALLOWED_EMAIL>" <<<"$idents" | sort -u || true)
if [ -n "$bad" ]; then
  echo "Commits must be authored and committed as OpenMellon <$ALLOWED_EMAIL>. Found:" >&2
  sed 's/^/  /' <<<"$bad" >&2
  echo "Fix with: git config user.name 'Open Mellon' && git config user.email '$ALLOWED_EMAIL'" >&2
  exit 1
fi
