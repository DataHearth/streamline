#!/usr/bin/env bash
# PostToolUse hook for Edit/Write: format + lint the changed file based on
# its path, blocking the edit (exit 2) when lint reports violations.
#
# Args: $1 = project root (absolute path)
# Hook stdin: JSON with .tool_input.file_path (absolute path).

set -uo pipefail

root=${1:-}
[ -z "$root" ] && { echo "post-edit.sh: missing project root arg" >&2; exit 1; }

f=$(jq -r '.tool_input.file_path // empty')
[ -z "$f" ] && exit 0

# Only act on files inside the project root.
case "$f" in
"$root"/*) rel=${f#"$root/"} ;;
*) exit 0 ;;
esac

# Linter stdout is redirected to stderr so Claude Code surfaces the
# violation message as the block reason; without 1>&2 the hook exits 2
# silently and the user sees an empty error.
case "$rel" in
*.go)
	go tool golangci-lint fmt "$f"
	go tool golangci-lint run "$(dirname "$f")" 1>&2 || exit 2
	;;
web/static/js/*.js | web/static/css/*.css)
	biome format --write "$f"
	biome lint "$f" 1>&2 || exit 2
	case "$rel" in
	web/static/js/docs.js) task build:js ;;
	web/static/css/input.css) task build:css ;;
	esac
	;;
esac
