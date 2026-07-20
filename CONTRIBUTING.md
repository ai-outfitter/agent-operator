# Contributing

This covers the local development environment. For *using* the operator against a
cluster you already have, see the [quick start](docs/documentation/quick-start.md).

> **Implementation status:** the controller, runtime image, and devenv tasks below
> are specified but not implemented yet. The commands document the intended
> developer workflow.

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

`cluster:up` starts a microVM containing single-node k3s, GreenMail (an isolated
SMTP/IMAP server for compositions that need mail), a local image path, and the
Link Operator. `operator:install` is idempotent and waits until these resources
are ready.

Confirm the two CRDs are installed:

```sh
kubectl api-resources --api-group=link.aioutfitter.com
```

From here the [quick start](docs/documentation/quick-start.md) and the
[use cases](docs/documentation/usecases.researcher-wiki-maintainer.md) work
against your local cluster.

## Run the scripted scenario

The email-research demo has scripted `devenv` tasks that apply the sample
resources, send a paper, and verify the result:

```sh
devenv tasks run demo:m1
devenv tasks run demo:verify
```

The full acceptance contract and evidence requirements are in the
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
