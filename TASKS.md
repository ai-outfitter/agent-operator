# Link Operator Roadmap

A Kubernetes operator that provides **primitives** for a fleet of autonomous
agents — a bounded namespace workspace, generic secret/config exposure, catalog
resolution, and running the agent — while channels (email, GitHub, Signal) and
tools (wiki, source-ingest) are **agent-layer composition**. The split is
described in [docs/architecture.md](docs/architecture.md).

There are two cluster-scoped custom resources: `Organization` and `Agent`.
The target user workflow is in the
[quick start](docs/documentation/quick-start.md), with example custom resources
under [`config/samples/`](config/samples/).

This file is the roadmap. **Requirements** describe the eventual system as goals,
in priority order. **Milestones** decide which slice of those goals is built next.

## Requirements (eventual goals, in priority order)

Priority follows how an agent reaches work: it acts within an **organization** (a
forge org), scoped to that org's **projects** and repositories; an **agent** holds
the memberships that grant this access; that access is powered by exposed
**credentials/config**; and with those in hand it delegates work as
**subagent Jobs**.

1. [OPR-001 — Organizations](docs/requirements/OPR-001-orgs.md) — the forge-org
   boundary owning repositories, projects, and catalogs.
2. [OPR-002 — Projects](docs/requirements/OPR-002-projects.md) — org-owned units of
   work grouping repositories and environments.
3. [OPR-003 — Agents](docs/requirements/OPR-003-agents.md) — the persistent worker,
   its namespace workspace, and its memberships.
4. [OPR-004 — Credentials and configuration exposure](docs/requirements/OPR-004-config-secrets.md)
   — the generic secret/config delivery primitive, a prerequisite for running
   agents and subagents.
5. [OPR-005 — Subagent execution (Jobs)](docs/requirements/OPR-005-subagent-jobs.md)
   — environments and delegated work as Jobs in the agent namespace.

## Milestones

Each milestone carries its own scope (Goals / Non-Goals / Graduation Criteria).

### M1 — email paper research (current)

[docs/milestones/M1-email-paper-reserach/](docs/milestones/M1-email-paper-reserach/task.md)
— the first proof of the primitives via one real composition: email a PDF, ingest
it into an organization wiki, reply in-thread. Scope is defined there; at a glance
it builds the operator primitives plus the researcher composition, and defers
projects, multi-org routing, multi-catalog union, subagent launch, and recursive
research.

- [x] Define the requirements (OPR-001…005) and this milestone.
- [ ] The M1 Proposal items in
      [task.md](docs/milestones/M1-email-paper-reserach/task.md#proposal).

### Later milestones (not yet scoped)

Candidate themes, each to be scoped as its own milestone when picked up:

- Recursive research: candidate queue, breadth/depth controls (hard max depth
  five), DOI/URL/digest dedup, per-run budgets.
- Projects and the operator-driven environment-launch API: run history,
  concurrency, cancellation.
- Many-to-many multi-organization routing and multi-catalog union (with
  duplicate-slug rejection and explicit conflict reporting).
- Additional channels — a GitHub pull-request watcher, a Signal/Telegram responder
  — reusing the same primitives; possibly a channel `Trigger`/`EventSource` CRD if
  one proves worthwhile over agent-runtime adapters.
- Reviewed branch/PR and direct-push wiki publication modes.
- Multi-tenant hardening: NetworkPolicy/egress guardrails and per-tenant identity.
