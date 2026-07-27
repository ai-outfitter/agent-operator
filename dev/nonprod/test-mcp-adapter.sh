#!/bin/sh
set -eu

IMAGE="${1:-link-agent:nonprod-mcp-test}"
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"

docker run --rm --platform linux/amd64 \
	--entrypoint sh \
	-v "$SCRIPT_DIR/mcp-adapter.integration.mjs:/fixture.mjs:ro" \
	"$IMAGE" -lc '
		cp -R /usr/local/lib/node_modules/pi-mcp-adapter /tmp/pi-mcp-adapter
		mkdir -p /tmp/pi-mcp-adapter/node_modules/@earendil-works
		pi_dependencies=/usr/local/lib/node_modules/@ai-outfitter/outfitter/node_modules/@earendil-works/pi-coding-agent/node_modules
		ln -s "$pi_dependencies/@earendil-works/pi-ai" /tmp/pi-mcp-adapter/node_modules/@earendil-works/pi-ai
		ln -s "$pi_dependencies/@earendil-works/pi-tui" /tmp/pi-mcp-adapter/node_modules/@earendil-works/pi-tui
		ln -s "$pi_dependencies/typebox" /tmp/pi-mcp-adapter/node_modules/typebox
		PI_MCP_ADAPTER_TEST_ROOT=/tmp/pi-mcp-adapter node /fixture.mjs
	'
