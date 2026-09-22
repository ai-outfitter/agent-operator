# Contributing

This covers the local development environment. For *using* the operator against a
cluster you already have, see the [quick start](docs/documentation/quick-start.md).

> **Implementation status:** the controller, local cluster, and persistent JMAP
> receive/reply loop are implemented. PDF ingestion, wiki updates, and the final
> research reply are M2 work.

## Prerequisites

For the local development path you need:

- [Nix](https://nixos.org/) and [devenv](https://devenv.sh/) v2;
- a host capable of running the repository's microVM; and
- credentials for the model selected by the Dotagents agent you test with.

`kubectl` and the rest of the toolchain are provided by the devenv shell.

### Auto-activation with direnv (optional)

The repo ships an `.envrc` that loads the devenv shell automatically. Install
[direnv](https://direnv.net/), hook it into your shell, then run `direnv allow`
once in the repo root — entering the directory then activates the environment
without a manual `devenv shell`.

## Start a local cluster

From the repository root:

```sh
devenv shell
devenv tasks run cluster:up
devenv tasks run operator:install
```

`cluster:up` starts a microVM containing single-node k3s, Stalwart (an isolated
JMAP server for compositions that need mail), and a local image path.
`operator:install` uses Devenv's container builder to build and load both the
locked Outfitter/Pi agent image and the Agent Operator image, installs the
operator idempotently, and waits until the controller is ready. To build a
container specification without installing it, run `devenv container build
agent` or `devenv container build operator`.

Confirm the two CRDs are installed:

```sh
kubectl api-resources --api-group=aioutfitter.com
```

From here the [quick start](docs/documentation/quick-start.md) and the
[use cases](docs/documentation/usecases.researcher-wiki-maintainer.md) work
against your local cluster.

## Verify M1

The mail-loop scenario applies a demo `Organization` and `Agent`, copies the
local `$HOME/.pi` directory directly into the agent's durable volume, starts the
agent, and submits a uniquely identified message through Stalwart JMAP. It then
logs back into the sender mailbox and proves exactly one threaded reply has
the return address `From: researcher@outfitter.test`, `To: demo-user@outfitter.test`, and
the original Message-ID in `In-Reply-To`, including after a Deployment restart:

```sh
devenv tasks run demo:m1
```

The task never stores the local `.pi` payload in a Kubernetes Secret or committed
image. It streams the directory through a temporary pod into the researcher PVC,
deletes the pod, and only then unblocks the agent Deployment. Redacted M1
evidence is retained under
`.devenv/state/agent-cluster/shared/evidence/m1-email-flow/`.

This is the complete [M1 acceptance demo](docs/milestones/M1-email-round-trip/demo.md).
The PDF/wiki/research-response composition is the
[M2 milestone](docs/milestones/M2-email-paper-research/demo.md).

## Teardown

```sh
devenv tasks run cluster:down
```

Normal shutdown stops the microVM while preserving reusable images, model caches,
and demo evidence. A task that removes cluster disks, model caches, or fixtures
MUST include `reset` or `destroy` in its name and require explicit confirmation.

## Git workflow

See [AGENTS.md](AGENTS.md): work on the current branch and do not create branches,
commit, or push unless asked.

## Hosted resident provisioning preview

The optional `--resident-provisioner-address=:8082` listener is disabled by default.
Before enabling it, configure these server-owned environment variables:

- `RESIDENT_PROVISIONER_TOKEN`: a server-only random bearer credential (at least 32 characters).
- `RESIDENT_CATALOG_REPOSITORY` and `RESIDENT_CATALOG_REVISION`: public catalog shorthand and full commit SHA containing the resident profiles and triage workflow.
- `RESIDENT_RUNTIME_IMAGE`: digest-pinned image with Outfitter, Pi, Channels, Node.js, and `gh`.
- `RESIDENT_MODEL`: a gateway model ID authorized for resident tokens.
- `RESIDENT_SERVICE_ORIGINS`: comma-separated HTTPS origins allowed for inference and GitHub token brokering.
- `POD_NAMESPACE`: the operator namespace, used to restrict manager A2A ingress.
- `RESIDENT_RUNTIME_PATH`: optional original image executable PATH; defaults to `/usr/local/bin:/usr/bin:/bin`.

The optional internal Service under `code/operator/config/residents/` can be included
in the deployment overlay. It does not enable the listener or provide credentials.
Keep the listener behind a private authenticated service transport; do not expose it
as a public ingress. The website is responsible for GitHub ownership and installation
checks before invoking this server-only API.

`PUT /v1/residents/{workspace}` creates one stable Organization and two Agents for
`user:<id>` or `org:<id>`. Caller names are display metadata. Kubernetes identities,
profiles, workflow, quotas, and runtime configuration are operator controlled. Repeat
requests preserve workspace PVCs. Unmanaged/cross-workspace resource collisions fail
closed. GET returns current generation-bound operator readiness without credentials.

Only the manager exposes A2A intake, bound to `resident-issue-triage`. The engineer
remains idle. Task requests validate selected repository ID and name, then forward a
stable message ID to Channels `/message:send`. Channels owns durable principal-scoped
deduplication; the server returns 202 only after an explicit accepted Task response.
The catalog/runtime pins must include Channels' durable A2A deduplication contract.

Initialization discovers gateway models with each role token and writes a native Pi
`outfitter` provider using an environment credential reference. It never receives
OpenRouter or Spark credentials. The managed `gh` wrapper requires an explicitly
selected repository, asks the website broker for a fresh repository-scoped installation
token on each invocation, and permits only issue triage commands. The broker must
restrict installation tokens to `contents:read`, `issues:write`, `metadata:read`.
Rotating role credentials changes initialization configuration, causing a new rollout
without replacing the durable workspace.

Run focused checks with `devenv shell -- sh -c 'cd code/operator && GOMAXPROCS=2 go test -p 2 ./internal/residentprovisioner'`.
Before activation, separately verify a pinned live deployment, authenticated A2A
acceptance, one GitHub issue response, scoped billing, and negative push permissions.
Unit tests do not establish those deployment acceptance conditions.

Each provisioning PUT includes a positive monotonic `generation`. Increment it
whenever enrollment, names, repository scope, or credentials change; retries must
preserve the complete original request. The operator persists a generation and
request digest on every managed resource and rejects older or conflicting writes
with `409 stale_generation`, including writes racing across operator replicas.
GET/PUT status includes `generation`; clients must match it to the requested
revision. Partial rollouts cannot report ready until all resource fences agree.
