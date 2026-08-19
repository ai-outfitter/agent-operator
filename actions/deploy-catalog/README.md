# deploy-catalog

Deploy the agent-operator `Agent`s a catalog declares.

What one run does, in order: select → render `__REVISION__` → assert RBAC in
both directions → server-side dry-run everything → apply → wait for each
`Agent` to converge (`observedGeneration`, `Ready`, and the resolved catalog
revision — all three).

## Selecting what deploys

**By default the tree decides.** Each `agents/<id>/deployment.yaml` is one
agent's manifests, the glob is the whole deploy list, and the RBAC preflight is
derived from the same glob — so the permission assertion can never drift from
the set of agents it protects, and an agent added to the tree is an agent added
to the fleet.

**Set `cluster` and the catalog's `clusters.yaml` decides.** One catalog then
serves several clusters, and nothing is discovered:

```yaml
# clusters.yaml, at the catalog root
clusters:
  nonprod:
    agents: [luce, nonprod-bot]
  prod:
    agents: [prod-bot]
```

```yaml
- uses: ai-outfitter/agent-operator/actions/deploy-catalog@<full-sha>
  with:
    cluster: prod
    revision: ${{ github.sha }}
    catalog-source: my-org-agents
```

The layout does not change: every manifest stays at
`agents/<id>/deployment.yaml` whichever cluster runs it. **A manifest in the
tree is inert until a cluster names it**, so a catalog can hold a production
agent and a nonprod fleet side by side without either reaching the other's
cluster, and without a second tree to keep a glob honest.

Naming the set is also what lets the permission preflight mean something. A
deploy role is `resourceNames`-scoped per cluster, so a set that does not match
that role fails the positive check before anything is applied — point one
cluster's manifests at another's endpoint and the deploy stops. A glob can only
ever confirm what it just discovered.

Using `cluster` requires `yq` on the runner as well as `jq`; the glob path still
needs only `jq`. Either implementation of `yq` works — it is used solely to
convert the table to JSON.

## Identity is a precondition, not an input

The step before this one must leave `kubectl` pointing at the target cluster
as the deploy identity. That is the only genuinely per-deployment part —
GitHub OIDC exchanged for a cloud role, a Forgejo runner's local context —
and it stays outside the action. The action asserts what that identity can
and cannot do; it never establishes it.

Requires `kubectl` (or `$KUBECTL`) and `jq` on the runner.

## Usage

```yaml
- uses: actions/checkout@v6
# ... establish cluster identity (e.g. aws-actions/configure-aws-credentials
#     + aws eks update-kubeconfig) ...
- uses: ai-outfitter/agent-operator/actions/deploy-catalog@<full-sha>
  with:
    revision: ${{ github.sha }}
    catalog-source: my-org-agents
```

On Forgejo, qualify the reference fully — a bare `uses:` resolves against the
instance's configured actions host:

```yaml
- uses: https://github.com/ai-outfitter/agent-operator/actions/deploy-catalog@<full-sha>
```

Pin an exact ref — a full commit SHA, or an exact `agent-operator-vX.Y.Z` tag
once one containing this action exists (the first is the release after this
directory landed). The action patches custom resources
whose API group has already changed once in this repository's tag history
(`link-operator-v0.3.0` → `agent-operator-v0.4.0`); the exact pin is what
moves your deploy step and the CRDs it patches as one bump. There is no
floating major tag before 1.0, deliberately — `bump-minor-pre-major` puts
breaking changes in minor bumps, which a floating tag would silently cross.

| Input | Default | Meaning |
| --- | --- | --- |
| `catalog-root` | `.` | Directory holding `agents/` |
| `cluster` | — | Cluster named in `clusters.yaml`; unset globs instead |
| `clusters-file` | `clusters.yaml` | Cluster table, relative to `catalog-root` |
| `revision` | — | Full 40-char SHA rendered into every `__REVISION__` |
| `catalog-source` | — | This repo's catalog-source name; convergence checks its resolved revision |
| `field-manager` | `deploy-catalog` | Server-side-apply field manager |
| `converge-deadline` | `600` | Seconds to wait for the fleet to converge |
| `rollout-timeout` | `10m` | Per-agent `rollout status` timeout |

A `deployment.yaml` with no `__REVISION__` placeholder is legal and logged:
that is an agent whose profile a different catalog defines, pinned to that
catalog's revision instead of this one.

## Tests

`tests/deploy-test.sh` runs the whole script against a stub `kubectl` and
fixture catalogs — no cluster and no credentials — covering both selection
paths and every refusal: an unknown cluster, an agent with no manifest in the
tree, an agent named twice, a manifest declaring a different `Agent`, a missing
grant, and a grant that is too wide. One case asserts the property the glob
cannot provide: agents sharing the tree with the selected one are not touched.

## Adding an agent is a two-person change

CI moves objects that already exist. The namespace, Secrets, the operator,
and the `resourceNames` entries in the deploy role are an administrator's to
create — a newly selected agent fails the positive preflight with the exact
permission the administrator must add, and nothing has been applied when it
does.

Each manifest must also declare the `Agent` its id promises. That catches the
dangerous edit as well as the careless one: `Agent` is cluster-scoped and owns
its namespace by owner reference, so renaming one is a delete plus a create
that cascade-deletes every Secret, ConfigMap, and PVC beneath it.
