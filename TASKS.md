# Link Operator Tasks

This repository builds a Kubernetes operator that provides **primitives** for a
single owner's fleet of autonomous agents, and treats channels (email, GitHub,
Signal) and tools (wiki, source-ingest) as **agent-layer composition**. The split
is described in [docs/architecture.md](docs/architecture.md).

There are two cluster-scoped custom resources: `Organization` and `Agent`.
Projects and environments are not CRDs; projects grouping is deferred and the
subagent-Job delegation seam lives in [OPR-004](docs/requirements/OPR-004-environments.md).

The target user workflow starts in the
[quick start](docs/documentation/quick-start.md), with example custom resources
under [`config/samples/`](config/samples/).

## Track A — operator primitives (P0)

Channel- and tool-agnostic controller work.

- [x] Define the first-pass organization, agent, credentials, and delegation
      requirements.
- [ ] Scaffold `code/operator` with Go, Kubebuilder, controller-runtime, and
      envtest.
- [ ] Generate and install the `Organization` and `Agent` CRDs, and no others.
- [ ] Validate `Organization` (`repositories` + one pinned catalog) and resolve
      the single commit-pinned `owner/.agents`, `owner/.agent`, or colocated
      `.agents` catalog. Keep `agentCatalogs` a list without implementing union.
- [ ] Reconcile an agent namespace as its complete workspace: service account,
      namespaced `admin` RoleBinding, operator-owned ResourceQuota and LimitRange,
      a durable per-agent workspace volume, and the runtime Deployment. No
      operator-owned channel-state (mailbox) resource.
- [ ] Expose referenced Secrets/ConfigMaps to the runtime (`as: env|volume`) and
      wait only for their existence (`CredentialsReady`); never inspect contents.
- [ ] Build the agent runtime base image from the pinned Outfitter source and
      launch Dotagents profiles through Pi (`outfitter run <agent> --harness pi`).
- [ ] Provide a devenv v2 microVM/k3s development environment with operator
      install, and readiness/recovery guidance.

## Track B — M1 email-research demo composition (P0, priority)

The active milestone is
[M1 email paper research](docs/milestones/M1-email-paper-reserach/task.md); its
acceptance demo is [demo.md](docs/milestones/M1-email-paper-reserach/demo.md).
This work is **agent-runtime composition**, not controller work, and takes
priority over all other tracks.

- [ ] Ship the `researcher` agent + vendored `wiki` and `source-ingest` skills in
      the pinned `.agents` catalog.
- [ ] Implement the email channel adapter in the runtime: poll IMAP idempotently,
      accept one PDF attachment, keep Message-ID state in agent-managed workspace
      state (backed by external mailbox read-state), and send a threaded SMTP
      reply. The operator never sees this.
- [ ] Clone the organization's `wiki` repository into the durable workspace and
      run the pinned skills.
- [ ] Store the original paper with Git LFS, extract `content.md` with Docling,
      update the wiki graph, record linked-paper candidates, and create exactly
      one local Git commit (no push).
- [ ] Add GreenMail to the local cluster and provide `demo:m1`, `demo:verify`, and
      teardown tasks; cache Docling models for repeatable demos.
- [ ] Pass the scripted M1 demo, retain its evidence bundle, and prove a duplicate
      Message-ID delivery creates no second reply or commit.

## Track C — deferred (reintroduce when a use case needs it)

The schema stays forward-compatible; only the behavior is deferred.

- [ ] Subagent-Job public launch API, run history, concurrency, and cancellation
      — when an agent needs operator-managed, project-scoped work.
- [ ] Projects grouping ([OPR-002](docs/requirements/OPR-002-projects.md)) — when
      project-scoped ownership is required.
- [ ] Many-to-many multi-organization routing — the `memberships` list already
      supports it; add routing when an agent must serve more than one org.
- [ ] Multi-catalog union + duplicate `<resource-kind>/<slug>` rejection — the
      `agentCatalogs` list already supports it; add union when a second catalog
      exists, with explicit conflict reporting and no order-based shadowing.
- [ ] Channel `Trigger`/`EventSource` CRD — only if an operator-level primitive
      beats agent-runtime adapters once several channels exist.
- [ ] NetworkPolicy/egress guardrails and per-tenant identity — before the fleet
      hosts mutually distrusting owners.
- [ ] Recursive research: durable candidate queue, breadth/depth controls (hard
      max depth five), DOI/URL/digest dedup, and per-run budgets.
- [ ] Reviewed branch/PR and direct-push wiki publication modes.

## Requirement index

- [Architecture](docs/architecture.md)
- [OPR-001 — Organizations](docs/requirements/OPR-001-orgs.md)
- [OPR-003 — Agents](docs/requirements/OPR-003-agents.md)
- [OPR-005 — Credentials and configuration exposure](docs/requirements/OPR-005-config-secrets.md)
- [OPR-004 — Subagent execution (Jobs)](docs/requirements/OPR-004-environments.md)
- [OPR-002 — Projects (deferred)](docs/requirements/OPR-002-projects.md)
