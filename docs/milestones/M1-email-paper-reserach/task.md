# M1: Email Paper Research

## Outcome

A developer starts a local k3s cluster, applies one organization and one agent,
emails a research paper to the agent, and receives a threaded reply after the
agent creates a source-traceable local commit in the organization's wiki.

The executable acceptance contract is [demo.md](demo.md). Product obligations
are split across:

- [OPR-001 — Organizations](../../requirements/OPR-001-orgs.md)
- [OPR-002 — Projects](../../requirements/OPR-002-projects.md)
- [OPR-003 — Agents](../../requirements/OPR-003-agents.md)
- [OPR-004 — Environments](../../requirements/OPR-004-environments.md)

## P0 work

### 1. Repository and operator foundation

- [ ] Initialize the repository and scaffold `code/operator` with Go,
      Kubebuilder, controller-runtime, and envtest.
- [ ] Establish generation, formatting, lint, unit-test, image-build, and CRD
      manifest checks.
- [ ] Add the two cluster-scoped APIs at `link.aioutfitter.com/v1alpha1` and no
      other CRDs.

### 2. Organization reconciliation

- [ ] Implement `Organization` validation, conditions, and project/environment
      structural validation.
- [ ] Resolve commit-pinned standalone or colocated Dotagents catalogs,
      concatenate disjoint resources, and reject duplicate
      `<resource-kind>/<slug>` identities before invoking Outfitter.
- [ ] Produce redacted status containing only resolved repositories and
      revisions.

### 3. Agent isolation and runtime

- [ ] Reconcile `agent-<name>` as the entire agent workspace, with its service
      account, namespaced `admin` RoleBinding, operator-owned ResourceQuota and
      LimitRange, mailbox-state ConfigMap, and Deployment.
- [ ] Expose aggregate quota hard/used values and make quota rejection a clear,
      non-looping agent failure mode.
- [ ] Wait for namespaced Secrets and report precise readiness conditions.
- [ ] Build an immutable runtime image from Outfitter commit
      `c44205ef35265c893ad9f088772c35c71753bfb7` with Git, Git LFS, SSH, Pi, and
      the Docling runtime dependencies.
- [ ] Generate Outfitter settings from the organization's pinned catalogs and
      run the selected Dotagents agent through Pi.

### 4. Email worker

- [ ] Poll IMAP sequentially and persist the Message-ID state machine.
- [ ] Validate one PDF of at most 25 MiB and resolve the target organization.
- [ ] Preserve thread headers and send success or permanent-failure replies by
      SMTP.
- [ ] Make retries after each state transition safe, especially commit-before-
      reply restarts.

### 5. Wiki research run

- [ ] Clone the configured organization wiki into the durable workspace.
- [ ] Load the repository's `.agents` payload, where the `researcher` agent
      composes the vendored `wiki` and `source-ingest` skills.
- [ ] Add a catalog fixture proving multiple catalogs with disjoint resource
      identities concatenate successfully.
- [ ] Add a catalog fixture proving duplicate slugs produce
      `CatalogsResolved=False`/`DuplicateResourceSlug` rather than shadowing.
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
  behavior.
- Concurrent mailbox work, parallel subagents, cancellation, and run history.
- Production mail-server provisioning, spam handling, DKIM, SPF, and DMARC.
