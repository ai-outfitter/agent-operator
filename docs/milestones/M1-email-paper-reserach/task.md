# M1: Email Paper Research

## Outcome

A developer starts a local k3s cluster, applies one organization and one agent,
emails a research paper to the agent, and receives a threaded reply after the
agent creates a source-traceable local commit in the organization's wiki.

The primitives-vs-composition split is described in
[architecture.md](../../architecture.md): the operator provides the workspace,
secret/config exposure, catalog resolution, and runs the agent; the email channel
and wiki tools are agent-layer composition. The executable acceptance contract is
[demo.md](demo.md). Product obligations are split across:

- [OPR-001 — Organizations](../../requirements/OPR-001-orgs.md)
- [OPR-003 — Agents](../../requirements/OPR-003-agents.md)
- [OPR-005 — Credentials and configuration exposure](../../requirements/OPR-005-config-secrets.md)
- [OPR-004 — Subagent execution (Jobs)](../../requirements/OPR-004-environments.md) (delegation seam; not exercised by M1)
- [OPR-002 — Projects](../../requirements/OPR-002-projects.md) (deferred)

## P0 work

### 1. Repository and operator foundation

- [ ] Initialize the repository and scaffold `code/operator` with Go,
      Kubebuilder, controller-runtime, and envtest.
- [ ] Establish generation, formatting, lint, unit-test, image-build, and CRD
      manifest checks.
- [ ] Add the two cluster-scoped APIs at `link.aioutfitter.com/v1alpha1` and no
      other CRDs.

### 2. Organization reconciliation

- [ ] Implement `Organization` validation and conditions over `repositories` and
      one pinned catalog. Multi-catalog union and projects are deferred; keep the
      `agentCatalogs` list shape without implementing concatenation.
- [ ] Resolve the single commit-pinned standalone or colocated Dotagents catalog
      before invoking Outfitter.
- [ ] Produce redacted status containing only resolved repositories and
      revisions.

### 3. Agent workspace primitives (controller)

The controller's work is channel- and tool-agnostic (see
[OPR-003](../../requirements/OPR-003-agents.md) and
[OPR-005](../../requirements/OPR-005-config-secrets.md)).

- [ ] Reconcile `agent-<name>` as the entire agent workspace, with its service
      account, namespaced `admin` RoleBinding, operator-owned ResourceQuota and
      LimitRange, durable per-agent workspace volume, and Deployment. No
      channel-state (mailbox) resource is operator-owned.
- [ ] Expose aggregate quota hard/used values and make quota rejection a clear,
      non-looping agent failure mode.
- [ ] Expose referenced Secrets/ConfigMaps to the runtime and wait only for their
      existence (`CredentialsReady`); never inspect their contents.
- [ ] Resolve the organization's single pinned catalog, generate Outfitter
      settings, and run the selected Dotagents agent through Pi.

The demo runtime image is built from Outfitter commit
`c44205ef35265c893ad9f088772c35c71753bfb7`. It is a generic Pi/Outfitter/git/ssh
base plus the researcher composition's tools (Git LFS, Docling); those tool
dependencies belong to the agent, not the operator contract.

### 4. Email channel adapter (agent runtime — composition)

This is agent-runtime behavior delivered by the researcher composition, **not** the
controller. Mailbox state is agent-managed; the mail server is the system of
record.

- [ ] Poll IMAP sequentially and persist a Message-ID state machine in
      agent-managed workspace state.
- [ ] Validate one PDF of at most 25 MiB and resolve the target organization
      (routing config supplied via a ConfigMap through OPR-005).
- [ ] Preserve thread headers and send success or permanent-failure replies by
      SMTP.
- [ ] Make retries after each state transition safe, especially commit-before-
      reply restarts, leaning on external mailbox read-state plus a local dedup
      cache.

The adapter's email Secret (referenced by name only in `Agent.spec.credentials`,
never schema-validated by the operator) MUST contain:

| Key | Meaning |
| --- | --- |
| `address` | Mailbox and reply From address |
| `imapHost`, `imapPort` | Incoming server endpoint |
| `imapUsername`, `imapPassword` | Incoming authentication |
| `imapTLS` | `true` for TLS, `false` only in the isolated demo |
| `smtpHost`, `smtpPort` | Submission server endpoint |
| `smtpUsername`, `smtpPassword` | Submission authentication |
| `smtpTLS` | `true` for TLS, `false` only in the isolated demo |

Email is only the *first* channel adapter. The same primitives run other
compositions unchanged — a GitHub pull-request watcher (GitHub notifications API)
or a Signal/Telegram responder — each swapping the channel while reusing the
workspace, secret-exposure, catalog, and delegation primitives.

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

- [ ] Add devenv v2 configuration and a microVM containing single-node k3s.
- [ ] Run GreenMail in the local cluster with deterministic test credentials.
- [ ] Provide `cluster:up`, `operator:install`, `demo:m1`, `demo:verify`, and
      `cluster:down` tasks plus an explicitly named destructive reset task.
- [ ] Cache large Docling models outside disposable agent Jobs so repeated demos
      are practical.
- [ ] Print readiness and recovery guidance instead of requiring manual
      `kubectl` archaeology.

### 7. Acceptance

- [ ] Run [the demo](demo.md) from a clean checkout.
- [ ] Retain the applied manifests, relevant conditions, redacted logs, email
      headers/body, Git diff/commit, LFS listing, and wiki validation output.
- [ ] Prove a second delivery of the same Message-ID sends no second reply and
      creates no second commit.

## Explicitly deferred

- Fetching any linked paper beyond the emailed seed paper.
- Research traversal deeper than zero; the future hard limit is five.
- Pushing the wiki commit, opening a branch or PR, or resolving merge conflicts.
- Public project-environment launch APIs, materialization, and kind-specific
  behavior (the internal subagent-Job seam stays; see OPR-004).
- Multi-catalog union + duplicate-slug rejection (single catalog for the MVP; the
  `agentCatalogs` list shape is retained).
- Many-to-many multi-organization routing (the `memberships` list shape is
  retained; the MVP exercises one entry).
- Concurrent mailbox work, parallel subagents, cancellation, and run history.
- Production mail-server provisioning, spam handling, DKIM, SPF, and DMARC.
