#!/usr/bin/env bash
#
# Build a devenv container, copy it to an OCI archive in the cluster share, and
# (when the cluster is reachable) wait for the in-VM importer to load it via the
# sha256 stamp handshake.
#
# Usage: build-image.sh <container-name> <archive.tar> <stamp.sha256>
#
set -euo pipefail

container="$1"
archive="$2"
stamp="$3"

mkdir -p "$(dirname "$archive")" "$(dirname "$stamp")"
temporary="$archive.tmp"
rm -f "$temporary"
trap 'rm -f "$temporary"' EXIT
devenv container copy --no-tui --refresh-eval-cache \
  --registry "oci-archive:$temporary:" \
  "$container"
mv "$temporary" "$archive"
trap - EXIT

digest="$(sha256sum "$archive" | cut -d' ' -f1)"
if kubectl get --raw=/readyz >/dev/null 2>&1; then
  until [ -s "$stamp" ] && [ "$(tr -d '\n' <"$stamp")" = "$digest" ]; do
    sleep 2
  done
fi
