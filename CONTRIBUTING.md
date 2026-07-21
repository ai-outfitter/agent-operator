# Contributing

This covers the local development environment. For *using* the operator against a
cluster you already have, see the [quick start](docs/documentation/quick-start.md).

> **Implementation status:** the controller, local cluster, and persistent JMAP
> receive/reply loop are implemented. PDF ingestion, wiki updates, and the final
> research reply remain M1 work.

## Prerequisites

For the local development path you need:

- [Nix](https://nixos.org/) and [devenv](https://devenv.sh/) v2;
- a host capable of running the repository's microVM; and
- credentials for the model selected by the Dotagents agent you test with.

`kubectl` and the rest of the toolchain are provided by the devenv shell.

## Start a local cluster

From the repository root:

```sh
devenv shell
devenv tasks run cluster:up
devenv tasks run operator:install
```

`cluster:up` starts a microVM containing single-node k3s, Stalwart (an isolated
JMAP server for compositions that need mail), and a local image path.
`operator:install` builds and loads both the pinned Outfitter/Pi agent image and
the Link Operator image, installs the operator idempotently, and waits until the
controller is ready.

Confirm the two CRDs are installed:

```sh
kubectl api-resources --api-group=link.aioutfitter.com
```

From here the [quick start](docs/documentation/quick-start.md) and the
[use cases](docs/documentation/usecases.researcher-wiki-maintainer.md) work
against your local cluster.

## Verify the mail loop

The mail-loop scenario applies a demo `Organization` and `Agent`, copies the
local `$HOME/.pi` directory directly into the agent's durable volume, starts the
agent, and submits a uniquely identified message through Stalwart JMAP. It then
logs back into the sender mailbox and proves exactly one threaded reply has
the return address `From: researcher@link.test`, `To: demo-user@link.test`, and
the original Message-ID in `In-Reply-To`, including after a Deployment restart:

```sh
devenv tasks run demo:mail-loop
```

The task never stores the local `.pi` payload in a Kubernetes Secret or committed
image. It streams the directory through a temporary pod into the researcher PVC,
deletes the pod, and only then unblocks the agent Deployment. Redacted mail-loop
evidence is retained under `.devenv/state/link-cluster/shared/evidence/mail-loop/`.

This proves the persistent JMAP receive/reply transport slice of the agent
runtime. The full PDF/wiki/research-response acceptance contract remains in the
[email-research milestone](docs/milestones/M1-email-paper-reserach/demo.md).

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
