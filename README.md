# Link Operator

A Kubernetes operator that provides **primitives** for a fleet of autonomous
agents — a bounded namespace workspace, generic secret/config exposure, catalog
resolution, and running the agent. Channels (email, GitHub, Signal) and tools
(a wiki, source ingestion) are **composed at the agent layer**, not baked into the
operator.

> **Status:** design stage. The CRDs, controller, and runtime image are specified
> but not yet implemented.

## Docs

- [Quick start](docs/documentation/quick-start.md) — install the operator and run
  an agent.
- [Use case: researcher wiki maintainer](docs/documentation/usecases.researcher-wiki-maintainer.md)
  — an end-to-end example composition (email a paper, get a wiki commit).
- [Architecture](docs/architecture.md) — the primitives-vs-composition design.
- [Contributing](CONTRIBUTING.md) — local development (Nix, devenv, a local
  cluster).
