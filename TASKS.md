# Link Operator Tasks

This repository is being built around two cluster-scoped custom resources:
`Organization` and `Agent`. Projects and their environments are embedded in an
organization; they are not additional CRDs.

The target user workflow starts in the
[quick start](docs/documentation/quick-start.md), with example custom resources
under [`config/samples/`](config/samples/).

## P0 — M1: email a paper and receive a research reply

The active milestone is
[M1 email paper research](docs/milestones/M1-email-paper-reserach/task.md). Its
acceptance demo is defined in
[demo.md](docs/milestones/M1-email-paper-reserach/demo.md).

- [x] Define the first-pass organization, project, agent, and environment
      requirements.
- [ ] Scaffold `code/operator` with Go, Kubebuilder, controller-runtime, and
      envtest.
- [ ] Generate and install the `Organization` and `Agent` CRDs.
- [ ] Reconcile an agent namespace as its complete workspace, with a service
      account, namespaced `admin` RoleBinding, operator-owned ResourceQuota and
      LimitRange, and runtime Deployment.
- [ ] Build the agent runtime image from the pinned Outfitter source and launch
      Dotagents profiles through Pi.
- [ ] Resolve commit-pinned `owner/.agents`, `owner/.agent`, and colocated
      `.agents` catalogs declared by an organization; concatenate disjoint
      resources and reject duplicate `<resource-kind>/<slug>` identities.
- [ ] Mount namespaced email, model-provider, and optional SSH Secrets without
      exposing secret values through either CRD.
- [ ] Poll IMAP idempotently, accept one PDF attachment, and send a threaded
      reply through SMTP.
- [ ] Clone the organization wiki into the agent workspace and run the pinned
      `wiki` and `source-ingest` skills.
- [ ] Store the original paper with Git LFS, extract `content.md` with Docling,
      update the wiki graph, and create one local Git commit.
- [ ] Provide a devenv v2 microVM/k3s development environment with GreenMail
      and agent-facing setup, demo, verification, and teardown tasks.
- [ ] Pass the scripted M1 demo and retain its evidence bundle.

## P1 — recursive research

- [ ] Turn the seed paper's linked-paper candidates into a durable work queue.
- [ ] Add breadth/depth controls with a hard maximum depth of five.
- [ ] Deduplicate papers by DOI, canonical URL, and content digest.
- [ ] Enforce per-run paper, byte, time, and model-cost budgets.
- [ ] Download and ingest depth-one papers before enabling deeper traversal.
- [ ] Make partial progress resumable and report terminal and retryable
      failures by source.

## P2 — projects and environments

- [ ] Materialize the common embedded environment shape as labeled Jobs in the
      invoking agent's namespace.
- [ ] Introduce development/deployment or other environment kinds only when
      they select different enforced reconciliation or admission behavior.
- [ ] Launch bounded Dotagents subagents for project work.
- [ ] Add concurrency limits, cancellation, and run history within the existing
      namespace quota boundary.
- [ ] Add production email-provider conformance tests while retaining the
      provider-neutral IMAP/SMTP contract.
- [ ] Add reviewed branch/PR and direct-push wiki publication modes.
- [ ] Add cross-catalog override semantics only if a concrete use case needs
      them, with explicit precedence, conflict reporting, and replacement tests.

## Requirement index

- [OPR-001 — Organizations](docs/requirements/OPR-001-orgs.md)
- [OPR-002 — Projects](docs/requirements/OPR-002-projects.md)
- [OPR-003 — Agents](docs/requirements/OPR-003-agents.md)
- [OPR-004 — Environments](docs/requirements/OPR-004-environments.md)
