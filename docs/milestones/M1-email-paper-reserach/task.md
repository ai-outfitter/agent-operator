# M1: Email Paper Research

## Summary

Prove the operator's primitives end-to-end by running one real composition. A
developer starts a local k3s cluster, applies one `Organization` and one `Agent`,
emails a research paper to the agent, and receives a threaded reply after the
agent creates a source-traceable local commit in the organization's wiki.

The requirements ([OPR-001](../../requirements/OPR-001-orgs.md)…
[OPR-005](../../requirements/OPR-005-subagent-jobs.md)) describe the eventual
system; this milestone decides which slice of them is built first. The composition
this milestone builds is documented for users as the
[researcher wiki maintainer use case](../../documentation/usecases.researcher-wiki-maintainer.md);
the executable acceptance contract is [demo.md](demo.md).

## Motivation

The operator's primitives are only worth building if they hold up under a real,
useful agent. Email-paper-research is a self-contained composition — a channel
plus tools — that exercises every primitive while needing none of the system's
deferred parts. Passing it demonstrates the primitives-vs-composition split
described in [architecture.md](../../architecture.md) is real, not cosmetic. The
primitives it exercises, and the composition itself, are enumerated in Goals.

## Goals

In scope for M1:

- The two cluster-scoped CRDs and the controller for the **operator primitives**:
  agent namespace workspace (service account, `admin` binding, ResourceQuota,
  LimitRange, durable per-agent volume, Deployment), generic Secret/ConfigMap
  exposure, Outfitter catalog settings, and running the agent.
- One `Organization` with one repository (the wiki) and one pinned catalog.
- One `Agent` with a single organization membership.
- The **researcher composition**: an in-runtime email channel adapter plus the
  vendored `wiki` and `source-ingest` tools, ingesting an emailed PDF into the
  wiki as one local commit and replying in-thread.
- A local devenv/k3s/Stalwart JMAP demo harness that runs and verifies the above.

## Non-Goals

Out of scope for M1 — these are eventual goals of the requirements, deliberately
not built yet:

- **Projects** ([OPR-002](../../requirements/OPR-002-projects.md)) and the public
  environment-launch API / run history / concurrency
  ([OPR-005](../../requirements/OPR-005-subagent-jobs.md)). M1 uses no projects and
  launches no subagent Jobs — the researcher ingests inline.
- **Many-to-many membership routing** across multiple organizations
  ([OPR-003](../../requirements/OPR-003-agents.md)); M1 exercises one membership.
- **Multi-catalog union + duplicate-slug rejection**
  ([OPR-001](../../requirements/OPR-001-orgs.md)); M1 passes one pinned source to
  Outfitter and delegates resolution entirely.
- **Recursive research** beyond the emailed seed paper (the eventual hard maximum
  depth is five).
- **Wiki publication** — pushing the commit, opening a branch or PR, or resolving
  merge conflicts. M1 makes one local commit and stops.
- **Multi-tenant hardening** — NetworkPolicy/egress, per-tenant identity.
- Concurrent mailbox work, parallel subagents, cancellation.
- Production mail-server provisioning, spam handling, DKIM, SPF, DMARC.

## Proposal

### 1. Repository and operator foundation

- [x] Initialize the repository and scaffold `code/operator` with Go, Kubebuilder,
      controller-runtime, and envtest.
- [x] Establish generation, formatting, lint, unit-test, image-build, and CRD
      manifest checks.
- [x] Add the two cluster-scoped APIs at `link.aioutfitter.com/v1alpha1` and no
      other CRDs.

### 2. Organization reconciliation

- [x] Implement `Organization` validation and conditions over `repositories` and
      one pinned catalog. (Multi-catalog union is a Non-Goal; keep the
      `agentCatalogs` list shape without implementing concatenation.)
- [x] Validate the single standalone or colocated Dotagents catalog source is
      commit-pinned, then pass that source through in Outfitter settings. The
      controller does not fetch, index, merge, or resolve catalog contents.
- [x] Produce redacted status containing only resolved repositories and revisions.

### 3. Agent workspace primitives (controller)

Channel- and tool-agnostic controller work.

- [x] Reconcile `agent-<name>` as the entire agent workspace, with its service
      account, namespaced `admin` RoleBinding, operator-owned ResourceQuota and
      LimitRange, durable per-agent workspace volume, and Deployment. No
      channel-state (mailbox) resource is operator-owned.
- [ ] Expose aggregate quota hard/used values and make quota rejection a clear,
      non-looping agent failure mode.
- [x] Expose referenced Secrets/ConfigMaps to the runtime and wait only for their
      existence (`CredentialsReady`); never inspect their contents.
- [ ] Generate Outfitter settings for the pinned source and run the selected
      Dotagents agent through Pi. Outfitter owns source fetching, catalog/profile
      resolution, composition, and launch behavior.

The demo runtime image is built from Outfitter commit
`c44205ef35265c893ad9f088772c35c71753bfb7`: a generic Pi/Outfitter/git/ssh base
plus the researcher composition's tools (Git LFS, Docling). Those tool
dependencies belong to the agent composition, not the operator contract.

### 4. Email channel adapter (agent runtime — composition)

Agent-runtime behavior delivered by the researcher composition, **not** the
controller. Mailbox state is agent-managed; the mail server is the system of
record.

- [x] Consume JMAP mailbox changes sequentially and persist a Message-ID state
      machine in agent-managed workspace state.
- [ ] Validate one PDF of at most 25 MiB and resolve the target organization
      (routing config supplied via a ConfigMap through OPR-004).
- [ ] Preserve thread headers and send success or permanent-failure replies with
      JMAP `Email/set` and `EmailSubmission/set`.
- [ ] Make retries after each state transition safe, especially commit-before-
      reply restarts, leaning on external JMAP mailbox state plus a local dedup
      cache.

The adapter's email Secret is referenced by name only in `Agent.spec.credentials`
and never schema-validated by the operator; its authoritative key contract is the
[researcher wiki maintainer use case](../../documentation/usecases.researcher-wiki-maintainer.md).

Email is only the *first* channel adapter. The same primitives run other
compositions unchanged — a GitHub pull-request watcher or a Signal/Telegram
responder — each swapping the channel while reusing the workspace, secret-exposure,
catalog, and delegation primitives.

### 5. Wiki research run (agent runtime — composition)

- [ ] Clone the organization's `wiki` repository into the durable workspace.
- [ ] Load the repository's `.agents` payload, where the `researcher` agent
      composes the vendored `wiki` and `source-ingest` skills.
- [ ] Store the original PDF with Git LFS and extract searchable Markdown with
      Docling.
- [ ] Create the source note, reconcile concepts, update the index, append the
      log, and record linked-paper candidates.
- [ ] Create exactly one local commit and include its SHA in the reply. Do not
      push.

### 6. Local developer experience

- [x] Add devenv v2 configuration and a microVM containing single-node k3s.
- [x] Run Stalwart in the local cluster with deterministic JMAP test accounts and
      no Internet egress.
- [ ] Provide `cluster:up`, `operator:install`, `demo:m1`, `demo:verify`, and
      `cluster:down` tasks plus an explicitly named destructive reset task.
- [ ] Cache large Docling models outside disposable agent Jobs so repeated demos
      are practical.
- [ ] Print readiness and recovery guidance instead of requiring manual `kubectl`
      archaeology.

## Risks

- **Untrusted input.** Email bodies, attachments, extracted text, and fetched
  pages are data, never instructions; the composition must never let them override
  agent policy or leak credentials.
- **Duplicate ingest on restart.** A crash between commit and reply must not create
  a second commit or reply; idempotency leans on external JMAP mailbox state plus
  a local dedup cache.
- **Docling/model cost.** Large models make repeated demos slow; cache them outside
  disposable Jobs.
- **Primitive leakage.** The controller must stay channel-agnostic; any email/wiki
  knowledge creeping into the operator is a design regression to reject.

## Graduation Criteria

- [ ] [The demo](demo.md) passes from a clean checkout.
- [ ] The evidence bundle is retained: applied manifests, conditions, redacted
      logs, email headers/body, Git diff/commit, LFS listing, and wiki validation
      output.
- [ ] A second delivery of the same Message-ID sends no second reply and creates no
      second commit (the idempotency proof in demo.md §5).

## Implementation History

- Operator foundation, two CRDs, workspace reconciliation, and the microVM/k3s
  Stalwart environment implemented.
- Persistent mail loop implemented with JMAP `Email/queryChanges`, durable
  Message-ID state, local `.pi` PVC seeding, threaded acknowledgement submission,
  sender-mailbox return-address verification, and restart persistence. PDF
  ingestion, Outfitter/Pi work execution, wiki commit, and the final research
  reply body remain.

## Alternatives

- **Bake the email/wiki behavior into the operator** (the original design). Rejected
  — it couples the operator to one channel/toolset and blocks other compositions
  (GitHub, Signal) without CRD changes. See [architecture.md](../../architecture.md).
- **A first-class channel `Trigger`/`EventSource` CRD.** Deferred — with a single
  channel, an in-runtime adapter is simpler; revisit once several channels exist.
- **An M0 "primitive-only" demo before M1.** Rejected — a real composition proves
  the primitives more convincingly than a trivial agent.
