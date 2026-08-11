# deploy-catalog

Deploy every agent-operator `Agent` a catalog declares. The tree decides what
deploys: each `agents/<id>/deployment.yaml` in the catalog is one agent's
manifests, the glob is the whole deploy list, and the RBAC preflight is
derived from the same glob — so the permission assertion can never drift from
the set of agents it protects.

What one run does, in order: glob → render `__REVISION__` → assert RBAC in
both directions → server-side dry-run everything → apply → wait for each
`Agent` to converge (`observedGeneration`, `Ready`, and the resolved catalog
revision — all three).

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
- uses: ai-outfitter/agent-operator/actions/deploy-catalog@agent-operator-v0.6.0
  with:
    revision: ${{ github.sha }}
    catalog-source: my-org-agents
```

On Forgejo, qualify the reference fully — a bare `uses:` resolves against the
instance's configured actions host:

```yaml
- uses: https://github.com/ai-outfitter/agent-operator/actions/deploy-catalog@agent-operator-v0.6.0
```

Pin an exact `agent-operator-vX.Y.Z` tag. The action patches custom resources
whose API group has already changed once in this repository's tag history
(`link-operator-v0.3.0` → `agent-operator-v0.4.0`); the exact pin is what
moves your deploy step and the CRDs it patches as one bump. There is no
floating major tag before 1.0, deliberately — `bump-minor-pre-major` puts
breaking changes in minor bumps, which a floating tag would silently cross.

| Input | Default | Meaning |
| --- | --- | --- |
| `catalog-root` | `.` | Directory holding `agents/` |
| `revision` | — | Full 40-char SHA rendered into every `__REVISION__` |
| `catalog-source` | — | This repo's catalog-source name; convergence checks its resolved revision |
| `field-manager` | `deploy-catalog` | Server-side-apply field manager |
| `converge-deadline` | `600` | Seconds to wait for the fleet to converge |
| `rollout-timeout` | `10m` | Per-agent `rollout status` timeout |

A `deployment.yaml` with no `__REVISION__` placeholder is legal and logged:
that is an agent whose profile a different catalog defines, pinned to that
catalog's revision instead of this one.

## Adding an agent is a two-person change

CI moves objects that already exist. The namespace, Secrets, the operator,
and the `resourceNames` entries in the deploy role are an administrator's to
create — a new `agents/<id>/` directory fails the positive preflight with the
exact permission the administrator must add, and nothing has been applied
when it does.
