#!/usr/bin/env bash
#
# link-agent entrypoint — generic resident Outfitter/Pi session.
#
# Channel setup and credentials are supplied by the Agent CR. The selected
# Outfitter profile chooses the Channels extension and its channel-facing skills;
# the runtime itself contains no email, Slack, or identity-specific policy.
#
# Pi's headless resident mode is `--mode rpc`. Channels opens push connections
# during session_start and wakes the agent only for real work. Holding stdin open
# keeps the resident session alive without polling or an initial model turn.
#
set -euo pipefail
export HOME="/workspace"
export PATH="/bin:/usr/bin:/usr/local/bin${PATH:+:$PATH}"
export NIX_CONFIG="${NIX_CONFIG:-experimental-features = nix-command flakes}"
export XDG_CACHE_HOME="/opt/link/.cache"
export PI_OFFLINE=1

cd /opt/link
exec outfitter run --strict "${LINK_AGENT_SLUG:-researcher}" -- \
  --mode rpc \
  --no-session \
  --offline \
  < <(tail -f /dev/null)
