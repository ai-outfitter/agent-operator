# M2 Demo: Email a Paper and Receive a Research Reply

This is the acceptance contract for M2. The commands named here are the
developer-facing interface the implementation must provide; until their tasks
in [task.md](task.md) are complete, this document is a specification rather
than a claim that the demo already runs.

It builds on the graduated
[M1 local email round trip](../M1-email-round-trip/demo.md); M2 reuses that JMAP
transport and replaces the acknowledgement with a paper-research result.

## Demonstrated behavior

One `Agent` receives a PDF over JMAP, uses an Outfitter Dotagents profile to
ingest it into one `Organization` wiki, creates one local Git commit, and sends
a threaded JMAP reply. No linked paper is downloaded and no Git remote is
modified.

The email channel and wiki tools are **agent-layer composition**; the operator
provides only the workspace, secret/config exposure, Outfitter settings, and
running the agent (see [architecture.md](../../architecture.md)). Relevant
requirements:

- [organization and catalog ownership](../../requirements/OPR-001-orgs.md)
- [agent workspace primitives](../../requirements/OPR-003-agents.md)
- [credential and config exposure](../../requirements/OPR-004-config-secrets.md)
- [subagent-Job delegation seam](../../requirements/OPR-005-subagent-jobs.md) (not exercised here)

## Fixed demo inputs

- A devenv v2 shell on a host capable of running the configured microVM.
- Single-node k3s in that microVM.
- Stalwart providing isolated JMAP mailboxes; its pod has no Internet egress.
- A bare writable wiki fixture with the `wiki/` layout, Git LFS enabled, and a
  clean default branch.
- A known research PDF at `fixtures/m2/seed-paper.pdf`, no larger than 25 MiB.
- An agent mailbox `researcher@link.test` and sender mailbox
  `demo-user@link.test`.
- A model-provider test Secret suitable for the selected Pi model.
- The M2 organization and agent examples derived from OPR-001 and OPR-003.

The runtime image is built from Outfitter commit
`c44205ef35265c893ad9f088772c35c71753bfb7` and uses Dotagents protocol revision
`502a9d5`. The only M2 catalog is the commit-pinned Link Operator repository,
payload path `.agents`. That payload defines `researcher` and vendors both
required skills. Its provenance is recorded in `.agents/README.md`.

## 1. Start the environment

From the repository root:

```sh
devenv tasks run cluster:up
devenv tasks run operator:install
```

`cluster:up` MUST start or resume the microVM, wait for the k3s API, deploy and
declaratively seed Stalwart, and print the kubeconfig path. `operator:install`
MUST build and load the local operator image, install the CRDs/controller, and
wait for the controller rollout.

`operator:install` MUST be idempotent. The environment is ready only when the
controller and Stalwart report ready and both CRDs are discoverable.

## 2. Apply the organization and agent

The demo task MUST apply:

1. organization `ai-outfitter`, with the seeded wiki and the single pinned Link
   Operator `.agents` catalog;
2. agent `researcher`, with organization-level membership and the
   Dotagents agent slug `researcher`; and
3. email, model, and SSH Secrets in namespace `agent-researcher` after the
   controller creates that namespace.

Secret values come from a demo-only SecretSpec/devenv profile and MUST not be
committed or printed. The SSH key may authenticate the wiki clone; M2 will not
use it to push.

The task MUST wait for:

```text
Organization/ai-outfitter: Accepted, CatalogSourcesReady, Ready
Agent/researcher: Accepted, NamespaceReady, WorkspaceReady,
                  CredentialsReady, OutfitterSettingsReady, WorkloadReady, Ready
```

Before credentials are created, the observable intermediate state MUST be
`CredentialsReady=False` while the Deployment exists and Kubernetes reports the
missing non-optional references through the Pod's standard container status.

The namespace MUST also contain `ResourceQuota/agent-workspace`,
`LimitRange/agent-workspace-defaults`, a durable per-agent workspace volume, and
a RoleBinding to the built-in `admin` ClusterRole. The agent may freely create
namespaced resources while the operator-owned quota bounds their aggregate
consumption.

## 3. Send the paper

Run:

```sh
devenv tasks run demo:m2
```

The task creates a standards-compliant message and submits it through Stalwart's
JMAP API:

```text
From: demo-user@link.test
To: researcher@link.test
Subject: Research this paper for the AI Outfitter wiki
Message-ID: <m2-seed-paper@link.test>

Please ingest the attached paper, update the organization wiki, and tell me
which papers should be explored next.
```

It attaches `fixtures/m2/seed-paper.pdf` as `application/pdf` and records the
original message headers and attachment SHA-256 in the evidence directory.

## 4. Observe processing

The agent MUST:

1. receive the message through JMAP mailbox changes and persist `received`;
2. validate the request and persist `running`;
3. clone or reset a clean organization wiki working tree without discarding a
   prior completed M2 commit;
4. run `outfitter run researcher --harness pi` with the composed catalogs;
5. treat the email and PDF as untrusted research material, not system
   instructions;
6. place the untouched PDF in a dated `wiki/sources/<source>/` directory;
7. track the PDF through Git LFS and generate `content.md` with Docling;
8. add a verified `source.md`, update or create relevant concepts, update
   `wiki/index.md`, and append `wiki/log.md`;
9. record cited or linked papers as verified candidates at depth one without
   downloading them;
10. create exactly one local commit and persist `committed` with its SHA; and
11. create and submit the reply through JMAP, then persist `replied` before
    marking the source message complete.

The commit subject MUST begin `wiki(ingest):`. The working tree MUST be clean
after the commit.

## 5. Verify the reply and wiki

Run:

```sh
devenv tasks run demo:m2:verify
```

The verifier MUST query the sender mailbox through JMAP and prove:

- exactly one reply exists for `<m2-seed-paper@link.test>`;
- `In-Reply-To` equals that Message-ID and `References` contains it;
- the body reports success, source title, concise summary, organization, local
  commit SHA, changed paths, candidate papers, and warnings if any;
- no credential, service-account token, or private key is present.

It MUST inspect the agent workspace and prove:

- the reported commit exists locally and was not pushed;
- exactly one new commit was created;
- the committed PDF digest equals the attachment digest;
- `git lfs ls-files` includes the PDF and Git stores an LFS pointer;
- `content.md` is non-empty and contains recognizable paper structure;
- `source.md` contains real provenance and links to the affected wiki notes;
- relevant concepts, `wiki/index.md`, and the append-only `wiki/log.md` changed;
- the wiki link/tag validation commands supplied by the pinned `wiki` skill
  pass; and
- there are candidate links but no depth-one paper source directories.

The verifier then submits the identical message again. After the agent becomes
idle, the commit count and reply count MUST remain unchanged. This is the M2
idempotency proof.

## Evidence and failure behavior

The demo MUST place these redacted artifacts under an ignored evidence
directory:

- tool and image revisions;
- applied organization/agent manifests without Secrets;
- final conditions and namespace resource inventory;
- ResourceQuota hard/used values, LimitRange defaults, and the durable
  per-agent workspace volume;
- redacted controller and agent logs;
- original and reply headers plus reply text;
- attachment and committed-source digests;
- `git status`, commit metadata, diff statistics, and `git lfs ls-files`;
- wiki validation output; and
- duplicate-delivery commit/reply counts.

A failed assertion MUST make `demo:m2:verify` non-zero and print the relevant
artifact path. It MUST distinguish validation failure, catalog/profile failure,
Docling failure, model failure, Git failure, and JMAP submission failure.

## Teardown

```sh
devenv tasks run cluster:down
```

Normal teardown stops the microVM while preserving reusable images, model
caches, and demo evidence. Any task that deletes the cluster disk, wiki fixture,
or caches MUST include `reset` or `destroy` in its name and require explicit
confirmation.

## Out of scope

- Real Internet mail delivery or production mail-server administration.
- Fetching any linked paper, even when the seed paper provides a direct PDF.
- Traversal beyond the seed (`depth=0`); the eventual hard maximum depth is
  five.
- Pushing the wiki commit or opening a pull request.
- Project environment launches, kind-specific behavior, or concurrent
  subagents.
