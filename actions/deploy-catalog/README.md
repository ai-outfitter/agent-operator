# deploy-catalog

Deploy the agent-operator `Agent`s a catalog declares.

What one run does, in order: select → render `__REVISION__` and `__ORG__` → assert RBAC in
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

## One persona per catalog

A persona — `luce`, `vega` — is deployed **once per catalog**. `Agent` is
cluster-scoped and the operator derives the namespace `agent-<name>` from it,
so two catalogs that both run `luce` on the same cluster would land on the
same cluster-scoped object and the same namespace. The name is therefore
`<organization>-<id>`, and the namespace `agent-<organization>-<id>`.

`organization` is a short deployment prefix. The catalog chooses this prefix.
The prefix does not need to match the catalog's forge organization login. The
prefix MUST be unique among the catalogs that deploy to the same cluster.

A forge login can be long or awkward as a Kubernetes name. For example, the
`ai-outfitter` GitHub org's bot login stutters (`ai-outfitter-outfitter-bot`),
and `unsupervisedcom-luce` reads poorly. So each catalog picks its own short
prefix instead: the `Unsupervisedcom` GitHub org's catalog declares
`organization: unsupervised`, and the `ai-outfitter` GitHub org's catalog
declares `organization: outfitter`.

The prefix is rendered, never hand-written. A catalog declares it once, at the
top of `clusters.yaml`:

```yaml
# clusters.yaml, at the catalog root
organization: unsupervised      # this catalog's deployment prefix, unique on this cluster
clusters:
  prod:
    agents: [luce, vega]
```

and each manifest names its `Agent` with the `__ORG__` placeholder:

```yaml
apiVersion: aioutfitter.com/v1alpha1
kind: Agent
metadata:
  name: __ORG__-luce            # applied as unsupervised-luce
```

`agents/luce/deployment.yaml` must then declare exactly
`Agent/unsupervised-luce`. The RBAC preflight, the convergence loop, the
derived namespace `agent-unsupervised-luce`, and the action's `agents`
output all use the rendered name.

The key and the placeholder are required together, and the deploy fails closed
in both directions:

| Situation | Result |
| --- | --- |
| `organization` declared, manifest has `__ORG__` | Renders; `Agent/<org>-<id>` |
| `__ORG__` present, no `organization` declared | **Refused** — an unrendered placeholder would apply as a literal name |
| `organization` declared, manifest has no `__ORG__` | **Refused** — an unprefixed Agent in an org-scoped catalog is the collision this prevents |
| Neither — no `organization`, no `__ORG__` | Deploys as before; `Agent/<id>` |

`organization` is read only on the cluster path. The glob path does not read
`clusters.yaml` at all, so it never carries a prefix.

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

### Standard AWS identity

An AWS-hosted catalog has exactly one deploy identity for each repository and
environment, shared by every agent that catalog selects there. Its IAM role,
Kubernetes username, and Kubernetes group are all named:

```text
<organization-slug>-catalog-deploy-<environment>
```

`organization-slug` identifies the catalog owner (for example,
`ai-outfitter`), and `environment` is `nonprod` or `prod`. This identity slug
is independent of the shorter `organization` prefix in `clusters.yaml`, which
names Kubernetes `Agent` objects.

[`aws/identity-stack.yaml`](aws/identity-stack.yaml) is the canonical
CloudFormation template. It creates the IAM role, trusts one exact GitHub
repository and ref through the account's existing GitHub OIDC provider, and
grants only `eks:DescribeCluster` on one exact cluster. It does not create the
account-wide OIDC provider, alter EKS authentication, or create Kubernetes
RBAC. The action inputs do not change.

Each catalog keeps its non-secret values in
`deploy/identity/<environment>.parameters.json`, using CloudFormation's
parameter-array format:

```json
[
  { "ParameterKey": "OrganizationSlug", "ParameterValue": "example-org" },
  { "ParameterKey": "Environment", "ParameterValue": "nonprod" },
  { "ParameterKey": "GitHubOrganization", "ParameterValue": "example-org" },
  { "ParameterKey": "GitHubRepository", "ParameterValue": ".agents" },
  { "ParameterKey": "GitHubRef", "ParameterValue": "refs/heads/main" },
  { "ParameterKey": "ClusterName", "ParameterValue": "nonprod" },
  { "ParameterKey": "OidcProviderArn", "ParameterValue": "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com" }
]
```

An administrator deploys the pinned template with the standardized stack name
`catalog-deploy-<organization-slug>-<environment>`:

```sh
aws cloudformation deploy \
  --stack-name catalog-deploy-example-org-nonprod \
  --template-file <agent-operator-checkout>/actions/deploy-catalog/aws/identity-stack.yaml \
  --parameter-overrides file://deploy/identity/nonprod.parameters.json \
  --capabilities CAPABILITY_NAMED_IAM
```

Pin `<agent-operator-checkout>` to the same exact release tag or commit policy
used for the action. Do not copy the shared template into the catalog.

### Map the identity through `aws-auth`

GitHub OIDC authenticates the workflow to AWS; it does not authorize the IAM
role in Kubernetes. For clusters that use `aws-auth`, each catalog therefore
keeps an eksctl `ClusterConfig` alongside the parameters, for example
`deploy/identity/nonprod.aws-auth.yaml`:

```yaml
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: nonprod
  region: us-east-1
iamIdentityMappings:
  - arn: arn:aws:iam::123456789012:role/example-org-catalog-deploy-nonprod
    username: example-org-catalog-deploy-nonprod
    groups:
      - example-org-catalog-deploy-nonprod
    noDuplicateARNs: true
```

Apply it as an administrator with
`eksctl create iamidentitymapping -f deploy/identity/nonprod.aws-auth.yaml`.
The catalog's reviewed `RoleBinding` binds that exact group; its `Role` remains
responsible for the fine-grained, `resourceNames`-scoped authorization.

Before changing a mapping, inspect the cluster authentication mode:

```sh
aws eks describe-cluster --name nonprod \
  --query 'cluster.accessConfig.authenticationMode' --output text
```

`CONFIG_MAP` and `API_AND_CONFIG_MAP` support this procedure. If the result is
`API`, stop: do not improvise an EKS Access Entry or change the cluster's
authentication mode as part of a catalog rollout. That requires a separately
tested cluster-owner migration.

### Migrate without cutting off rollback

Create the new IAM role first, add its `aws-auth` mapping, and bind its group
alongside the old group before changing the workflow role ARN. Verify both the
expected permissions and the action's forbidden-permission preflight in two
fresh OIDC runs. During this overlap, rollback is only a workflow ARN change.

Remove the old RoleBinding subject, `aws-auth` mapping, and IAM role only after
the new identity has converged the full environment twice. To revoke the new
path during an incident, switch the workflow back first, then remove the new
RoleBinding subject and mapping. Keep an independent administrator kubeconfig
throughout.

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
| `clusters-file` | `clusters.yaml` | Cluster table, relative to `catalog-root`; may declare `organization` |
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
tree, an agent named twice, a manifest declaring a different `Agent`, `__ORG__`
with no `organization` behind it, an `organization` with no `__ORG__` in the
manifest, a missing grant, and a grant that is too wide. One case asserts the property the glob
cannot provide: agents sharing the tree with the selected one are not touched.
The organized fixture uses `organization: acme` — a name chosen for the
fixture, not a real forge org login, which itself demonstrates that the value
is a free-form prefix.

## Adding an agent is a two-person change

CI moves objects that already exist. The namespace, Secrets, the operator,
and the `resourceNames` entries in the deploy role are an administrator's to
create — a newly selected agent fails the positive preflight with the exact
permission the administrator must add, and nothing has been applied when it
does.

Each manifest must also declare the `Agent` its id promises — `<org>-<id>`
when the catalog declares an organization, `<id>` when it does not. That catches the
dangerous edit as well as the careless one: `Agent` is cluster-scoped and owns
its namespace by owner reference, so renaming one is a delete plus a create
that cascade-deletes every Secret, ConfigMap, and PVC beneath it.
