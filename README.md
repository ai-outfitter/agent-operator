# Agent Operator

A Kubernetes operator that provides **primitives** for a fleet of autonomous
agents — a bounded namespace workspace, generic secret/config exposure, catalog
resolution, and running the agent. Channels (email, GitHub, Signal) and tools
(a wiki, source ingestion) are **composed at the agent layer**, not baked into the
operator.

> **Status:** design stage. The CRDs and controller are specified but not yet
> implemented.

## Images

This repository publishes **one** image: the controller,
`ghcr.io/ai-outfitter/agent-operator`. It publishes **no agent runtime image**
(see [#13](https://github.com/ai-outfitter/agent-operator/issues/13) and
[#28](https://github.com/ai-outfitter/agent-operator/pull/28)).

The agent runtime is the published Outfitter container,
`ghcr.io/ai-outfitter/outfitter:<version>`. As of Outfitter
[v1.4.0](https://github.com/ai-outfitter/outfitter/releases/tag/v1.4.0) that
image is deliberately extensible — it carries a shell and a `root` account — so a
consumer can build `FROM ghcr.io/ai-outfitter/outfitter:1.4.0`. An agent that
needs more than the stock runtime publishes that derived image from its own
org's `<org>/.agents` repository, beside the profiles it runs — never from an
application repository. `Agent.spec.image` selects the image per Agent.

`containers.agent` in `devenv.nix` still builds a runtime image, for the **local
dev cluster only**. It is never released, so nothing downstream can pin it. The
controller's `--agent-image` flag likewise still defaults to `agent-runtime:dev`;
pointing that default at the Outfitter container is pending follow-up work.

## Docs

- [Quick start](docs/documentation/quick-start.md) — install the operator and run
  an agent.
- [Use case: researcher wiki maintainer](docs/documentation/usecases.researcher-wiki-maintainer.md)
  — an end-to-end example composition (email a paper, get a wiki commit).
- [Architecture](docs/architecture.md) — the primitives-vs-composition design.
- [Contributing](CONTRIBUTING.md) — local development (Nix, devenv, a local
  cluster).
