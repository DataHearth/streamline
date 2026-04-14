#!/usr/bin/env bash
# PreToolUse hook for Edit/Write: block edits to generated files. Exits 2
# (the block code) with a stderr message pointing at the regen command.
#
# Args: $1 = project root (absolute path)
# Hook stdin: JSON with .tool_input.file_path (absolute path).

set -uo pipefail

root=${1:-}
[ -z "$root" ] && { echo "pre-edit.sh: missing project root arg" >&2; exit 1; }

f=$(jq -r '.tool_input.file_path // empty')
[ -z "$f" ] && exit 0

# Only enforce rules on files inside the project root.
case "$f" in
"$root"/*) rel=${f#"$root/"} ;;
*) exit 0 ;;
esac

# Allow-list: hand-edited files that live inside otherwise-generated dirs.
case "$rel" in
ent/schema/*) exit 0 ;;
ent/generate.go) exit 0 ;;
ent/migrate/main.go) exit 0 ;;
esac

reason=""
case "$rel" in
ent/*.go) reason="ent generated code — re-run 'task generate' instead" ;;
internal/server/restapi/gen.go) reason="oapi-codegen output — re-run 'task generate' instead" ;;
*/mocks/mock_*.go) reason="mockery output — re-run 'task generate' instead" ;;
web/static/js/docs.min.js) reason="esbuild docs bundle — re-run 'task build:js' instead" ;;
web/static/dist/spa.min.js | web/static/dist/spa.min.css) reason="esbuild SPA bundle — re-run 'task build:js' instead" ;;
web/app/.routify/*) reason="routify-generated route tree — re-run 'task build:js' instead" ;;
web/static/css/style.css | web/static/css/docs.min.css) reason="Tailwind/esbuild output — re-run 'task build:css' instead" ;;
coverage.html) reason="test coverage report — re-run 'task test:coverage' instead" ;;
*) exit 0 ;;
esac

echo "Refusing to edit generated file: $f" >&2
echo "Reason: $reason" >&2
exit 2
