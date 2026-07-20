# Architecture

Link Operator is a Kubernetes operator for running a **single owner's fleet of
autonomous agents**. Its job is to provide **primitives** — an isolated,
quota-bounded workspace for each agent, a generic way to expose secrets and
configuration, and resolution of the agent's capability catalog — and then run
the agent. It deliberately does **not** implement any agent behavior. Channels
(email, GitHub notifications, Signal) and tools (a wiki, source ingestion) are
**composed at the agent layer** and are invisible to the controller.

This document is the anchor for the more detailed requirement documents under
[`requirements/`](requirements/) (the eventual goals) and the milestones under
[`milestones/`](milestones/) (which decide scope).

## Core principle

> The operator controller never talks to a channel, never polls a mailbox, and
> never reads or schema-validates a credential's contents. It provisions the
> namespace workspace (including a durable volume), exposes referenced Secrets
> and ConfigMaps, resolves the pinned catalog, and runs the agent Deployment.
> Everything channel- or tool-shaped runs inside the agent runtime as composable
> Dotagents resources; a running agent may launch subagent Jobs in its own
> namespace within its quota.

Keeping the operator channel- and tool-agnostic is what lets the same platform
run an email research agent today and a GitHub pull-request worker or a Signal
responder tomorrow, without any change to the CRDs or the controller.

## Two layers

### Operator primitives (the platform)

These are the operator's contract. They are domain-agnostic.

- **Organization** — the ownership and policy boundary. It owns a set of generic
  Git repositories and one pinned Dotagents catalog. See
  [OPR-001](requirements/OPR-001-orgs.md).
- **Agent workspace** — for each `Agent`, a namespace `agent-<name>` that is the
  agent's entire workspace and autonomy boundary, containing:
  - one runtime service account;
  - a RoleBinding from that service account to the built-in `admin` ClusterRole,
    scoped to the namespace only;
  - an operator-owned `ResourceQuota` and `LimitRange` (the agent cannot weaken
    or delete these);
  - a durable per-agent workspace volume; and
  - the long-running agent `Deployment`.

  See [OPR-003](requirements/OPR-003-agents.md).
- **Secret / config exposure** — a generic mechanism to project named Secrets and
  ConfigMaps from the agent namespace into the runtime as environment variables
  or read-only mounts. The operator waits only for their **existence**; it never
  reads, logs, copies, or validates their contents. See
  [OPR-004](requirements/OPR-004-config-secrets.md).
- **Catalog resolution + run** — resolve the organization's commit-pinned
  Dotagents catalog and run the selected agent (`outfitter run <agent> --harness
  pi`). The runtime image is a generic base (Pi, Outfitter, git, ssh).

### Agent composition (the behavior)

None of this is in the operator's contract. It is delivered by the agent's
Dotagents resources and its runtime image.

- **The main-agent loop.** Each agent runs a persistent process that, on each
  tick (a stop-hook / agentic loop), surveys a prioritized set of input sources,
  turns them into tasks, and works or delegates them. Staying responsive is a
  goal: heavy work is pushed to background subagents.
- **Channels.** Adapters for external event and message sources — email first,
  then GitHub notifications, Signal, Telegram, WhatsApp. Each is a Dotagents
  skill / MCP server / Pi extension inside the runtime. Task and notification
  handling may become dedicated MCP tooling over time. The operator models none
  of this.
- **Tools.** Capabilities such as the `wiki` and `source-ingest` skills.
- **Subagent delegation.** A running agent may launch subagents as Kubernetes
  Jobs in its own namespace, using its `admin` rights and bounded by the shared
  `ResourceQuota`. See [OPR-005](requirements/OPR-005-subagent-jobs.md).
- **External systems of record.** The authoritative state for a mailbox is a mail
  server (JMAP / Stalwart); for issues and pull requests it is GitHub / Forgejo;
  for the wiki it is a Git repository. The agent's durable volume is a working
  cache and Git working tree, not the source of truth.

## Execution model

- **Agent = persistent Deployment.** One long-running pod per agent runs the main
  loop.
- **Subagent = ephemeral Job.** Delegated work runs as a Job in the same
  namespace, sharing the agent's service account and quota. A simple composition
  may ingest inline instead; the seam exists for those that delegate.
- **Restart safety.** The loop is resumable because durable state lives in the
  per-agent volume and, more importantly, in the external systems of record.
  Idempotency leans on external read-state (a seen/flagged message, a read
  notification) plus a small local dedup cache, not on the operator being a
  database.

## Trust and isolation (single-owner)

The current target is a single owner running the whole fleet, so the isolation
boundary is the agent namespace plus its `ResourceQuota` and `LimitRange`:

- the agent is `admin` **within its own namespace only** and cannot reach Nodes,
  other Namespaces, CRDs, or its own quota/namespace object;
- the operator continuously reconciles the quota and LimitRange so the agent
  cannot widen its own budget; and
- email bodies, attachments, extracted text, and fetched pages are **untrusted
  data, never instructions**, and must not override agent policy.

Multi-tenant hardening — NetworkPolicy / egress control, per-tenant identity, and
API-stability discipline — is explicitly deferred. It is the first thing to add
before the platform hosts mutually distrusting owners.

## Non-goals / deferred

Each is deferred until a concrete use case exercises it. Deferral is
**implementation-only**: where the future direction is known (multiple
memberships, multiple catalogs, multiple credentials), the CRD keeps the
plural/list shape now so we never have to migrate a scalar to a list later. We
defer the behavior, not the schema.

| Deferred | Reintroduce when |
| --- | --- |
| A channel `Trigger`/`EventSource` CRD | a second channel makes an operator-level primitive clearly worth it over agent-runtime adapters |
| Projects grouping ([OPR-002](requirements/OPR-002-projects.md)) and the public environment-launch API | an agent needs operator-managed, project-scoped work rather than self-launched subagent Jobs |
| Many-to-many membership routing | an agent must serve more than one organization |
| Multi-catalog union + duplicate-slug rejection | an organization needs more than its single pinned catalog |
| NetworkPolicy / egress + multi-tenant identity | the fleet hosts mutually distrusting owners |
| Recursive research beyond the seed (depth > 0) | after the first research milestone; the eventual hard maximum depth is five |
| Wiki push / branch / PR publication | a reviewed publication mode is needed beyond the local commit |

## Example: attributing a composition's steps

To make the split concrete, here is how an email-research agent's steps divide
between the two layers. Nothing email- or wiki-shaped is an operator primitive.

| Step | Owner |
| --- | --- |
| Create the agent namespace, service account, `admin` binding, quota, LimitRange, durable volume, Deployment | Operator primitive |
| Wait for the referenced Secrets/ConfigMaps to exist and project them into the runtime | Operator primitive (generic exposure) |
| Resolve the pinned catalog and run `outfitter run <agent> --harness pi` | Operator primitive |
| Poll IMAP, accept one PDF, keep Message-ID idempotency state | Agent composition (email channel adapter) |
| Preserve the PDF with Git LFS, extract `content.md` with Docling | Agent composition (`source-ingest` tool) |
| Update source notes, concepts, index, log; create one local commit | Agent composition (`wiki` tool) |
| Send the threaded SMTP reply | Agent composition (email channel adapter) |

The [researcher wiki maintainer](documentation/usecases.researcher-wiki-maintainer.md)
is exactly this composition — the first proof of the primitives.

## Glossary

- **Organization** — ownership/policy boundary owning repositories and a catalog.
- **Agent** — a cluster-deployed worker; one persistent Deployment in its own
  namespace workspace.
- **Subagent** — background work an agent launches as a Job in its own namespace.
- **Channel** — an adapter to an external event/message source (email, GitHub,
  Signal); an agent-runtime concern, never an operator primitive.
- **Tool** — a capability such as the `wiki` or `source-ingest` skill.
- **Catalog** — a commit-pinned Dotagents payload supplying agents, skills,
  subagents, MCP servers, and plugins.
- **Workspace** — the agent's entire namespace, plus its durable volume, bounded
  by the operator-owned quota.
