---
title: Link Operator Design Conversation
type: source
source_kind: conversation
status: reviewed
authors:
  - ncrmro
  - OpenAI Codex
publication: Link Operator project design session
published: 2026-07-19
tags:
  - system/link-operator
  - evidence/design
  - topic/organization-model
  - topic/agent-runtime
  - process/source-ingestion
  - environment/local-development
created: 2026-07-19
updated: 2026-07-21
---

# Link Operator Design Conversation

## Provenance

Project design conversation held on 2026-07-19 and distilled into this
repository's initial tasks, requirements, samples, and user documentation. The
conversation itself is not stored as a transcript in this source directory;
this note records the decisions subsequently captured in version-controlled
project files.

## Design decisions

- The public API has exactly two top-level CRDs: [[Organization]] and [[Agent]].
- The organization is the [[Forge Owner|owner]] boundary and maps directly to a
  GitHub/Forgejo-style `<owner>` namespace; repositories retain
  `<owner>/<repository>` identities.
- An organization embeds many [[Project|projects]], while agents can belong to
  multiple organizations and projects. The `<owner>/<project>` hierarchy is the
  user-facing focus because one project can coordinate many repositories.
- The whole namespace is an [[Agent Namespace Workspace]], with broad
  namespaced Kubernetes access bounded by ResourceQuota.
- The first agent and Dotagents profile are both named [[Researcher Agent|`researcher`]].
- The first milestone is the [[Email Paper Research Workflow]]: email a paper,
  update the organization wiki, and receive a reply.
- The repository reuses the existing `wiki` and `source-ingest` skills for
  [[Wiki Source Ingestion]], including Docling extraction and Git LFS storage.
- Development/deployment environment kinds do not belong in M1;
  [[Project Environment|environments]] use one common shape until variants enforce
  different behavior.
- [[Dotagents Catalog Composition]] concatenates disjoint catalogs and rejects
  duplicate slugs rather than applying precedence.
- [[Recursive Literature Exploration]] is deferred and has a future maximum
  depth of five.
- The desired [[Local Development Environment]] uses devenv v2, k3s in a
  microVM, and developer-oriented setup scripts.

## Limitations

This is design evidence, not evidence that the operator or demo is implemented.
The version-controlled requirements are the more precise source for field-level
behavior. See
[[sources/2026-07-19-link-operator-requirements/source|Link Operator M1 requirements and catalog]].

The milestone sequence was revised on 2026-07-21: a local JMAP email round trip
became M1, while the paper/wiki workflow described above moved to M2. The
original decision remains here as historical evidence.

## Problems identified

- [[Safe Agent Autonomy]]
- [[Idempotent Email Research]]
- [[Source-Traceable Wiki Updates]]
- [[Catalog Resource Collisions]]
- [[Bounded Recursive Research]]
- [[Secret Containment]]
