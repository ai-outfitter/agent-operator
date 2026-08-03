#!/usr/bin/env bash
#
# agent-runtime entrypoint — generic resident Outfitter/Pi session.
#
# Channel setup and credentials are supplied by the Agent CR. The selected
# Outfitter profile chooses the Channels extension and its channel-facing skills;
# the runtime itself contains no email, Slack, or identity-specific policy.
#
# Pi's headless resident mode is `--mode rpc`. Channels opens push connections
# during session_start and wakes the agent only for real work. Holding stdin open
# keeps the resident session alive without polling or an initial model turn.
#
# Offline is the default: the image ships a prebuilt extension cache and the
# runtime never fetches executable code. An Agent CR may set PI_OFFLINE=0
# together with a writable XDG_CACHE_HOME (e.g. /workspace/.cache) so a
# profile-pinned npm extension version is installed once from the registry and
# then served from the workspace cache; Outfitter verifies exact-pinned npm
# versions against that cache on every start.
#
set -euo pipefail
export HOME="/workspace"
export PATH="/bin:/usr/bin:/usr/local/bin${PATH:+:$PATH}"
export NIX_CONFIG="${NIX_CONFIG:-experimental-features = nix-command flakes}"
export XDG_CACHE_HOME="${XDG_CACHE_HOME:-/opt/agent/.cache}"
export PI_CODING_AGENT_SESSION_DIR="${PI_CODING_AGENT_SESSION_DIR:-/workspace/.pi/agent/sessions}"
if [ -z "${PI_OFFLINE:-}" ]; then
  export PI_OFFLINE=1
fi

install -d -m 0700 "$PI_CODING_AGENT_SESSION_DIR"

offline_args=()
if [ "$PI_OFFLINE" = "1" ] || [ "$PI_OFFLINE" = "true" ]; then
  offline_args+=(--offline)
fi

# Launch from $HOME, not the baked payload directory. Outfitter's workspace
# layer is <cwd>/.agents and it outranks every configured source, so running
# from there made the image-baked payload shadow the Organization catalog:
# a catalog-root agents.md could never reach an agent. The baked payload
# stays available because the operator renders it as the last settings
# source, where the catalog outranks it file by file.
cd "$HOME"
exec outfitter run --strict "${AGENT_SLUG:-researcher}" -- \
  --mode rpc \
  --continue \
  ${offline_args[@]+"${offline_args[@]}"} \
  < <(tail -f /dev/null)
