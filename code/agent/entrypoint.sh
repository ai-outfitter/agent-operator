#!/usr/bin/env bash
#
# link-agent entrypoint — generic resident Pi loop.
#
# JMAP setup (Processed mailbox, connectivity, readiness) runs in an init
# container and the researcher uses its `mail` skill to drive `xin`. The runtime
# itself knows nothing about mail: it starts the selected agent and asks the
# generic loop extension to survey whatever inputs its identity and skills own.
#
# pi's headless resident mode is `--mode rpc` (it stays alive on stdin while the
# loop drives turns). We run it from /opt/link so Outfitter resolves the baked
# loadout, send one RPC bootstrap command, and hold stdin open afterward.
#
set -euo pipefail
export HOME="/workspace"
export PATH="/bin:/usr/bin:/usr/local/bin${PATH:+:$PATH}"
export NIX_CONFIG="${NIX_CONFIG:-experimental-features = nix-command flakes}"
export XDG_CACHE_HOME="/opt/link/.cache"
export PI_OFFLINE=1

loop_interval="${LINK_AGENT_LOOP_INTERVAL:-10m}"
if [[ ! "$loop_interval" =~ ^[1-9][0-9]*[mhd]$ ]]; then
  echo "link-agent: LINK_AGENT_LOOP_INTERVAL must be a positive minute/hour/day interval (for example 10m)" >&2
  exit 1
fi

loop_prompt="Survey your available inputs using your identity and active skills. Process all actionable work to completion; if none exists, end this iteration."
rpc_bootstrap="$(jq -cn \
  --arg message "/loop $loop_interval $loop_prompt" \
  '{type: "prompt", message: $message}')"

cd /opt/link
exec outfitter run --strict "${LINK_AGENT_SLUG:-researcher}" -- \
  --mode rpc \
  < <(printf '%s\n' "$rpc_bootstrap"; tail -f /dev/null)
