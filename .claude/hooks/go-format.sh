#!/usr/bin/env bash
# PostToolUse hook for Edit|Write: gofmt the edited .go file, then lint its package and
# report only the edited file's issues. Exit 2 feeds the issues back to Claude.
input=$(cat)
file=$(printf '%s' "$input" | sed -n 's/.*"file_path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)
file=${file//\\\\/\/}   # JSON-escaped backslashes -> forward slashes
file=${file//\\/\/}
case "$file" in *.go) ;; *) exit 0 ;; esac
[ -f "$file" ] || exit 0

gofmt -w "$file"

dir=$(dirname "$file")
root=${CLAUDE_PROJECT_DIR:-$(git -C "$dir" rev-parse --show-toplevel 2>/dev/null)}
[ -n "$root" ] || exit 0
base=$(basename "$file")
out=$(cd "$root" && go tool -modfile=tools/go.mod golangci-lint run --build-tags integration "$dir" 2>&1 \
	| grep -F "$base:" )
if [ -n "$out" ]; then
	echo "$out" >&2
	exit 2
fi
exit 0
