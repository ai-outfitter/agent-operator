#!/usr/bin/env bash
#
# Exercises deploy.sh end to end against a stub kubectl and fixture catalogs.
# No cluster, no credentials: the point is to prove the selection and the
# assertions, which are where a deploy decides what it is allowed to touch.
#
#   actions/deploy-catalog/tests/deploy-test.sh
#
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
deploy="$here/../deploy.sh"
export KUBECTL="$here/kubectl-stub"
revision="$(printf 'a%.0s' {1..40})"

passed=0
failed=0

run() {
  # run <catalog> [env assignments...] -- [args]
  local catalog="$1"; shift
  ( cd "$here/fixtures/$catalog" && env CATALOG_REVISION="$revision" \
      CATALOG_SOURCE_NAME=this-catalog "$@" bash "$deploy" 2>&1 )
}

expect_ok() {
  local label="$1"; shift
  local output status
  output="$("$@")"; status=$?
  if (( status == 0 )); then
    printf 'ok    %s\n' "$label"; passed=$((passed + 1))
  else
    printf 'FAIL  %s (exit %s)\n%s\n' "$label" "$status" "$output"; failed=$((failed + 1))
  fi
}

expect_fail() {
  # expect_fail <label> <substring> <command...>
  local label="$1" needle="$2"; shift 2
  local output status
  output="$("$@")"; status=$?
  if (( status == 0 )); then
    printf 'FAIL  %s (expected failure, got success)\n' "$label"; failed=$((failed + 1))
  elif [[ "$output" != *"$needle"* ]]; then
    printf 'FAIL  %s (expected %q in output)\n%s\n' "$label" "$needle" "$output"; failed=$((failed + 1))
  else
    printf 'ok    %s\n' "$label"; passed=$((passed + 1))
  fi
}

contains() {
  # contains <label> <substring> <command...>
  local label="$1" needle="$2"; shift 2
  local output
  output="$("$@")"
  if [[ "$output" == *"$needle"* ]]; then
    printf 'ok    %s\n' "$label"; passed=$((passed + 1))
  else
    printf 'FAIL  %s (expected %q)\n%s\n' "$label" "$needle" "$output"; failed=$((failed + 1))
  fi
}

# ── The glob path keeps working, unchanged ──────────────────────────────────
contains "glob selects every agents/<id>/deployment.yaml" \
  "tree declares: alpha beta" run globbed
expect_ok "glob deploy succeeds" run globbed

# ── The cluster table selects by name ───────────────────────────────────────
contains "cluster 'nonprod' selects only its named agents" \
  "names for 'nonprod': alpha beta" run clustered CLUSTER=nonprod
contains "cluster 'prod' selects only its own agent" \
  "names for 'prod': gamma" run clustered CLUSTER=prod
expect_ok "clustered deploy succeeds" run clustered CLUSTER=prod

# A manifest present in the tree but absent from the requested cluster must not
# deploy. This is the property the glob cannot provide.
output="$(run clustered CLUSTER=prod)"
if [[ "$output" == *"alpha"* ]]; then
  printf 'FAIL  an agent outside the cluster entry must not be touched\n%s\n' "$output"
  failed=$((failed + 1))
else
  printf 'ok    an agent outside the cluster entry must not be touched\n'
  passed=$((passed + 1))
fi

# ── An organization prefixes every Agent it deploys ─────────────────────────
# A persona is deployed once per catalog, so the CR name is <org>-<id> and the
# operator's derived namespace becomes agent-<org>-<id>. The prefix is a
# catalog-chosen short name (here "acme"), not a forge org login, rendered
# from clusters.yaml exactly like __REVISION__.
contains "the selection log still names bare ids" \
  "names for 'prod': luce vega" run organized CLUSTER=prod
contains "the declared organization is logged" \
  "organization: acme" run organized CLUSTER=prod
contains "__ORG__ renders into the Agent name" \
  "acme-luce is ready" run organized CLUSTER=prod
contains "every selected agent is prefixed" \
  "acme-vega is ready" run organized CLUSTER=prod
expect_ok "an organization-scoped deploy succeeds" run organized CLUSTER=prod

# The rendered name is what the preflight and the converge loop use, so a grant
# scoped to the unprefixed name must not satisfy them.
expect_fail "the preflight asserts the prefixed name" "expected authorization was denied" \
  run organized CLUSTER=prod STUB_DENY="patch agents.aioutfitter.com/acme-luce"

# Fail closed in both directions.
expect_fail "__ORG__ without an organization is refused" "no organization is declared" \
  run organized CLUSTER=prod CLUSTERS_FILE=clusters-no-org.yaml
expect_fail "an organization without __ORG__ is refused" "contains no __ORG__ placeholder" \
  run organized CLUSTER=unprefixed

# ── Refusals ────────────────────────────────────────────────────────────────
expect_fail "unknown cluster is refused" "defines no cluster 'staging'" \
  run clustered CLUSTER=staging
expect_fail "unknown cluster lists what exists" "nonprod, prod" \
  run clustered CLUSTER=staging
expect_fail "a missing clusters.yaml is refused" "does not exist" \
  run globbed CLUSTER=nonprod
expect_fail "an agent with no manifest in the tree is refused" "does not exist" \
  run clustered CLUSTER=missing-manifest
expect_fail "an agent named twice is refused" "more than once" \
  run clustered CLUSTER=duplicated
expect_fail "a manifest declaring a different Agent is refused" "declares no Agent named delta" \
  run clustered CLUSTER=renamed
# ── The permission preflight fails closed in both directions ────────────────
expect_fail "a missing grant fails before anything applies" "expected authorization was denied" \
  run clustered CLUSTER=prod STUB_DENY="patch agents.aioutfitter.com/gamma"
expect_fail "an over-wide grant fails before anything applies" "forbidden authorization was granted" \
  run clustered CLUSTER=prod STUB_ALLOW="create agents.aioutfitter.com"

printf '\n%s passed, %s failed\n' "$passed" "$failed"
(( failed == 0 ))
