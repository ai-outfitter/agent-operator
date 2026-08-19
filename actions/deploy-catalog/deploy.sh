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
cluster="${CLUSTER:-}"
clusters_file="${CLUSTERS_FILE:-clusters.yaml}"
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
required_tools=("$kubectl_bin" jq)
# yq is required only by the cluster table, so a caller that globs keeps the
# original two-tool contract and needs no new runner image.
[[ -n "$cluster" ]] && required_tools+=(yq)
for tool in "${required_tools[@]}"; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "deploy-catalog: required tool is unavailable: $tool" >&2
    [[ "$tool" == "yq" ]] && echo "deploy-catalog: yq is needed to read $clusters_file (cluster: $cluster)" >&2
    exit 1
  fi
done

kube=("$kubectl_bin")

# ── 1. Select ───────────────────────────────────────────────────────────────
# Two ways to decide what deploys, and a catalog picks one.
#
# Without `cluster`, the tree decides: each agents/<id>/deployment.yaml is one
# agent and the glob is the whole deploy list, so an agent added to the tree is
# an agent added to the fleet and there is no list to fall out of date. Right
# for a catalog that serves one cluster.
#
# With `cluster`, the catalog's clusters.yaml decides. One catalog then serves
# several clusters — a nonprod fleet and a production one — without a manifest
# committed to a convenient path deploying itself to the wrong place. Naming
# the set is also what lets the permission preflight below mean something: a
# resourceNames-scoped deploy role is written per cluster, so a set that does
# not match that role fails the positive check before anything is applied. A
# glob can only ever confirm what it just discovered.
declared=()
sources=()

if [[ -n "$cluster" ]]; then
  temporary_table="$(mktemp -d)"
  trap 'rm -rf "$temporary_table"' EXIT
  table="$root/$clusters_file"
  if [[ ! -f "$table" ]]; then
    echo "deploy-catalog: cluster '$cluster' was requested but $table does not exist" >&2
    exit 1
  fi
  # Two incompatible programs are called yq: mikefarah's Go implementation
  # (on the GitHub runner images) and the Python jq wrapper. Their expression
  # languages differ, so yq is used for exactly one thing — YAML to JSON — and
  # every selection below is jq, which is already a hard requirement.
  yaml_to_json() {
    if yq --version 2>&1 | grep -qi mikefarah; then
      yq -o=json '.' "$1"
    else
      yq '.' "$1"
    fi
  }
  table_json="$temporary_table/clusters.json"
  if ! yaml_to_json "$table" > "$table_json"; then
    echo "deploy-catalog: could not parse $table" >&2
    exit 1
  fi

  if [[ "$(jq -r --arg c "$cluster" '.clusters | has($c)' "$table_json")" != "true" ]]; then
    echo "deploy-catalog: $clusters_file defines no cluster '$cluster'" >&2
    echo "deploy-catalog: it defines: $(jq -r '.clusters | keys | join(", ")' "$table_json")" >&2
    exit 1
  fi

  # An entry names agent ids, and the layout stays agents/<id>/deployment.yaml
  # in every cluster. Nothing here needs to move a manifest elsewhere: with the
  # deploy list named, a manifest sitting in the tree is inert until a cluster
  # names it, which is the property a path could only approximate.
  while read -r id; do
    [[ -n "$id" ]] || continue
    manifest="agents/$id/deployment.yaml"
    if [[ ! -f "$root/$manifest" ]]; then
      echo "deploy-catalog: cluster '$cluster' names $id, but $manifest does not exist" >&2
      exit 1
    fi
    if printf '%s\n' "${declared[@]:-}" | grep -Fxq "$id"; then
      echo "deploy-catalog: cluster '$cluster' names $id more than once" >&2
      exit 1
    fi
    declared+=("$id")
    sources+=("$root/$manifest")
  done < <(jq -r --arg c "$cluster" '.clusters[$c].agents // [] | .[]' "$table_json")

  if (( ${#declared[@]} == 0 )); then
    echo "deploy-catalog: cluster '$cluster' names no agents" >&2
    exit 1
  fi
  echo "deploy-catalog: $clusters_file names for '$cluster': ${declared[*]}"
else
  for manifest in "$root"/agents/*/deployment.yaml; do
    [[ -e "$manifest" ]] || break
    declared+=("$(basename "$(dirname "$manifest")")")
    sources+=("$manifest")
  done
  if (( ${#declared[@]} == 0 )); then
    echo "deploy-catalog: no agents/*/deployment.yaml under $root — nothing to deploy" >&2
    exit 1
  fi
  echo "deploy-catalog: tree declares: ${declared[*]}"
fi

# ── 2. Render ───────────────────────────────────────────────────────────────
# Every place that pins the revision gets the same one, and a placeholder that
# survives rendering fails the deploy: a manifest still saying __REVISION__
# would apply cleanly and quietly deploy nothing. A file with no placeholder
# is legal — an agent whose profile a different catalog defines pins that
# catalog's revision, not this one — but it is worth a line in the log.
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary" "${temporary_table:-}"' EXIT

rendered=()
for index in "${!declared[@]}"; do
  id="${declared[$index]}"
  src="${sources[$index]}"
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

# Every manifest must declare the Agent its name promises — a mismatch means
# the deploy list and the cluster would disagree about what "deployed" means.
# It also catches a rename, which is the dangerous edit: Agent is cluster-scoped
# and owns its namespace by owner reference, so renaming one is a delete plus a
# create that cascade-deletes every Secret, ConfigMap, and PVC beneath it.
for index in "${!declared[@]}"; do
  id="${declared[$index]}"
  if ! printf '%s\n' "${agents[@]}" | grep -Fxq "$id"; then
    echo "deploy-catalog: ${sources[$index]} declares no Agent named $id" >&2
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

for name in "${agents[@]}"; do
  # Per-agent budget: one slow agent must not starve the checks behind it.
  deadline=$((SECONDS + converge_deadline))
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
