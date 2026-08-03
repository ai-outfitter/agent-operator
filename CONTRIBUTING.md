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
