#!/usr/bin/env bash
# Starts the Playwright MCP server named in .mcp.json.
#
# With direnv (the flake devshell) it runs nixpkgs' playwright-mcp against the
# devshell's browsers, linked into a writable directory since the store copy
# is read-only. Without it (a cloud session) it runs @playwright/mcp, which
# the environment's setup script installs, against the Chromium the image
# ships under /opt/pw-browsers; that runs as root, hence --no-sandbox.
set -euo pipefail

if command -v direnv >/dev/null; then
	exec direnv exec . bash -c 'D=$(mktemp -d); for b in $PLAYWRIGHT_BROWSERS_PATH/*; do ln -s "$(readlink -f "$b")" "$D/$(basename "$b")"; done; PLAYWRIGHT_BROWSERS_PATH=$D PLAYWRIGHT_MCP_BROWSER=chromium exec .playwright-mcp-wrapped --headless --output-dir .playwright-mcp'
fi

if ! command -v playwright-mcp >/dev/null; then
	echo "playwright-mcp: neither direnv nor @playwright/mcp is installed" >&2
	exit 1
fi
chrome=$(ls -d /opt/pw-browsers/chromium-*/chrome-linux64/chrome 2>/dev/null | sort -V | tail -1)
if [ -z "$chrome" ]; then
	echo "playwright-mcp: no Chromium under /opt/pw-browsers" >&2
	exit 1
fi
exec playwright-mcp --headless --browser chromium --executable-path "$chrome" \
	--no-sandbox --isolated --output-dir .playwright-mcp
