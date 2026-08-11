#!/usr/bin/env bash
#
# Deploy every Agent this catalog declares.
#
# The tree decides what deploys: each agents/<id>/deployment.yaml is one
# agent's manifests. There is no deploy list to maintain, so an agent added
# to the tree is an agent added to the fleet, and the RBAC preflight below is
# derived from the same glob — the permission assertion can never drift from
# the set of agents it protects.
#
# This script only *moves* objects that already exist. Bootstrap — namespaces,
# Secrets, the operator, images, and the RBAC this identity runs under — is an
# administrator's job, and the preflight asserts that boundary in both
# directions.
#
# Preconditions, deliberately not inputs: kubectl on PATH (or $KUBECTL)
# already pointing at the target cluster as the deploy identity, and jq.
# Identity is the one genuinely per-deployment part — AWS OIDC, a Forgejo
# runner's local context, anything — so it stays outside this action.
#
set -euo pipefail
umask 077

root="${CATALOG_ROOT:-.}"
kubectl_bin="${KUBECTL:-kubectl}"
revision="${CATALOG_REVISION:?CATALOG_REVISION is required}"
catalog_source="${CATALOG_SOURCE_NAME:?CATALOG_SOURCE_NAME is required}"
field_manager="${FIELD_MANAGER:-deploy-catalog}"
converge_deadline="${CONVERGE_DEADLINE:-600}"
rollout_timeout="${ROLLOUT_TIMEOUT:-10m}"

if [[ ! "$revision" =~ ^[0-9a-f]{40}$ ]]; then
  echo "deploy-catalog: CATALOG_REVISION must be a full 40-character Git SHA" >&2
  exit 1
fi
for tool in "$kubectl_bin" jq; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "deploy-catalog: required tool is unavailable: $tool" >&2
    exit 1
  fi
done

kube=("$kubectl_bin")

# ── 1. Discover ─────────────────────────────────────────────────────────────
# The directory name is the agent id; the glob is the whole deploy list.
declared=()
for manifest in "$root"/agents/*/deployment.yaml; do
  [[ -e "$manifest" ]] || break
  declared+=("$(basename "$(dirname "$manifest")")")
done
if (( ${#declared[@]} == 0 )); then
  echo "deploy-catalog: no agents/*/deployment.yaml under $root — nothing to deploy" >&2
  exit 1
fi
echo "deploy-catalog: tree declares: ${declared[*]}"

# ── 2. Render ───────────────────────────────────────────────────────────────
# Every place that pins the revision gets the same one, and a placeholder that
# survives rendering fails the deploy: a manifest still saying __REVISION__
# would apply cleanly and quietly deploy nothing. A file with no placeholder
# is legal — an agent whose profile a different catalog defines pins that
# catalog's revision, not this one — but it is worth a line in the log.
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT

rendered=()
for id in "${declared[@]}"; do
  src="$root/agents/$id/deployment.yaml"
  out="$temporary/$id.yaml"
  sed "s#__REVISION__#$revision#g" "$src" > "$out"
  if grep -Fq '__REVISION__' "$out"; then
    echo "deploy-catalog: failed to render the revision into $src" >&2
    exit 1
  fi
  if ! grep -Fq "$revision" "$out"; then
    echo "deploy-catalog: note: $src does not pin this catalog's revision (foreign-catalog agent?)"
  fi
  rendered+=("$out")
done

# ── 3. Enumerate what the manifests contain ────────────────────────────────
# Client-side dry-run parses the documents without touching cluster state;
# from it we take the Agent names the convergence loop needs and the
# Organization names the RBAC preflight asserts. Agent is cluster-scoped and
# owns its namespace: the operator derives `agent-<name>`, so we derive the
# same — a manifest never carries it.
objects="$temporary/objects.json"
for out in "${rendered[@]}"; do
  "${kube[@]}" apply --dry-run=client --output=json --filename="$out"
done | jq -s '[.[] | if .kind == "List" then .items[] else . end]' > "$objects"

mapfile -t agents < <(jq -r '.[] | select(.kind == "Agent") | .metadata.name' "$objects")
mapfile -t organizations < <(jq -r '.[] | select(.kind == "Organization")
  | .metadata.name' "$objects" | sort -u)

if (( ${#agents[@]} == 0 )); then
  echo "deploy-catalog: the manifests contain no Agent objects" >&2
  exit 1
fi

# Every directory must declare the Agent its name promises — a mismatch means
# the tree and the cluster would disagree about what "deployed" means.
for id in "${declared[@]}"; do
  if ! printf '%s\n' "${agents[@]}" | grep -Fxq "$id"; then
    echo "deploy-catalog: agents/$id/deployment.yaml declares no Agent named $id" >&2
    exit 1
  fi
done

# ── 4. Assert permissions in both directions ────────────────────────────────
report_identity() {
  "${kube[@]}" auth whoami >&2 || true
}
require_allowed() {
  local answer
  answer="$("${kube[@]}" auth can-i "$@" 2>/dev/null || true)"
  if [[ "$answer" != "yes" ]]; then
    echo "deploy-catalog: expected authorization was denied: $*" >&2
    report_identity
    echo "deploy-catalog: an administrator must widen the deploy role to cover it" >&2
    exit 1
  fi
}
require_denied() {
  local answer
  answer="$("${kube[@]}" auth can-i "$@" 2>/dev/null || true)"
  if [[ "$answer" != "no" ]]; then
    echo "deploy-catalog: forbidden authorization was granted: $*" >&2
    report_identity
    exit 1
  fi
}

# Positive: everything the tree declares must be movable.
for name in "${agents[@]}"; do
  require_allowed get "agents.aioutfitter.com/$name"
  require_allowed patch "agents.aioutfitter.com/$name"
  require_allowed get "deployments.apps/agent-runtime" --namespace="agent-$name"
done
for organization in "${organizations[@]}"; do
  require_allowed get "organizations.aioutfitter.com/$organization"
  require_allowed patch "organizations.aioutfitter.com/$organization"
done

# Negative: the identity must not be able to reach past the tree. A
# resourceName-scoped role cannot list, so instead of enumerating the cluster
# we assert the unscoped verbs: an identity that can patch or create *any*
# agent has drifted wider than the tree, and nothing else would ever notice.
require_denied patch agents.aioutfitter.com
require_denied create agents.aioutfitter.com
require_denied delete agents.aioutfitter.com
for name in "${agents[@]}"; do
  require_denied delete "agents.aioutfitter.com/$name"
  require_denied get secrets --namespace="agent-$name"
done
require_denied patch deployments.apps --namespace=agent-operator-system

# ── 5. Dry-run, apply ───────────────────────────────────────────────────────
# All files dry-run before any file applies: a rejected Agent should not leave
# an Organization already pointing at a revision nothing resolved.
for out in "${rendered[@]}"; do
  "${kube[@]}" apply \
    --server-side \
    --force-conflicts \
    --dry-run=server \
    --field-manager="$field_manager" \
    --filename="$out"
done
for out in "${rendered[@]}"; do
  "${kube[@]}" apply \
    --server-side \
    --force-conflicts \
    --field-manager="$field_manager" \
    --filename="$out"
done

# ── 6. Converge, per agent ──────────────────────────────────────────────────
# Converged is three facts, not one: the operator observed this spec, the
# Agent reports Ready, and — when the agent consumes this catalog — the
# catalog it actually resolved is the revision we pushed. Ready alone passes
# while the old pod still serves the previous profile.
# Agent is cluster-scoped, so the gets take no namespace; only the runtime
# Deployment lives in the derived agent-<name> namespace.
converged() {
  local name="$1"
  local generation observed ready catalog_revision consumes
  generation="$("${kube[@]}" get agents.aioutfitter.com "$name" -o jsonpath='{.metadata.generation}')"
  observed="$("${kube[@]}" get agents.aioutfitter.com "$name" -o jsonpath='{.status.observedGeneration}')"
  ready="$("${kube[@]}" get agents.aioutfitter.com "$name" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}')"
  [[ "$generation" == "$observed" && "$ready" == "True" ]] || return 1
  consumes="$("${kube[@]}" get agents.aioutfitter.com "$name" \
    -o jsonpath="{.status.catalogSources[?(@.name==\"$catalog_source\")].name}")"
  if [[ -n "$consumes" ]]; then
    catalog_revision="$("${kube[@]}" get agents.aioutfitter.com "$name" \
      -o jsonpath="{.status.catalogSources[?(@.name==\"$catalog_source\")].revision}")"
    [[ "$catalog_revision" == "$revision" ]] || return 1
  fi
  return 0
}

deadline=$((SECONDS + converge_deadline))
for name in "${agents[@]}"; do
  while (( SECONDS < deadline )) && ! converged "$name"; do
    sleep 5
  done
  if ! converged "$name"; then
    echo "deploy-catalog: Agent/$name did not converge to $revision" >&2
    "${kube[@]}" get agents.aioutfitter.com "$name" -o jsonpath='{.status.conditions}' >&2
    echo >&2
    exit 1
  fi
  "${kube[@]}" rollout status \
    "deployment/agent-runtime" \
    --namespace="agent-$name" \
    --timeout="$rollout_timeout"
  echo "deploy-catalog: $name is ready at $revision"
done

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  echo "agents=${declared[*]}" >> "$GITHUB_OUTPUT"
fi
echo "deploy-catalog: fleet of ${#declared[@]} converged at $revision"
