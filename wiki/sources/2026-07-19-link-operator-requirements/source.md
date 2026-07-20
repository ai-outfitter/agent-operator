---
title: Link Operator M1 Requirements and Catalog
type: source
source_kind: specification
status: reviewed
authors:
  - ncrmro
  - OpenAI Codex
publication: Link Operator repository
published: 2026-07-19
revision: 5df121e82daac28ff3567280df95cdb9e140df97
tags:
  - system/link-operator
  - evidence/design
  - topic/organization-model
  - topic/agent-runtime
  - topic/agent-catalog
  - process/email-processing
  - process/source-ingestion
created: 2026-07-19
updated: 2026-07-19
---

# Link Operator M1 Requirements and Catalog

## Provenance

This source represents the initial committed Link Operator specification at Git
revision `5df121e82daac28ff3567280df95cdb9e140df97`. Its primary repository paths
are:

- `TASKS.md`
- `docs/requirements/OPR-001-orgs.md`
- `docs/requirements/OPR-002-projects.md`
- `docs/requirements/OPR-003-agents.md`
- `docs/requirements/OPR-004-environments.md`
- `docs/milestones/M1-email-paper-reserach/demo.md`
- `.agents/README.md`
- `.agents/agents/researcher/agent.md`

The files remain canonical for exact normative wording. This note summarizes
their durable domain model so the wiki graph can link claims to one source node.

## Summary

[[Link Operator]] defines [[Organization]] and [[Agent]] as its only
cluster-scoped custom resources. Organizations own wikis, pinned catalogs, and
embedded [[Project|projects]]. Agents carry many-to-many memberships, select a
profile through [[Dotagents Catalog Composition]], and run in an isolated
[[Agent Namespace Workspace]].

The first profile is [[Researcher Agent|`researcher`]]. Its
[[Email Paper Research Workflow]] receives one PDF, performs
[[Wiki Source Ingestion]], creates one local commit, and sends one threaded
reply. A future
[[Recursive Literature Exploration]] phase follows verified candidates under a
hard depth and budget boundary.

[[Project Environment|Project environments]] are validated embedded templates,
not CRDs. The first shape has no kind discriminator and is not materialized in
M1. The specified [[Local Development Environment]] uses devenv v2, a microVM,
k3s, and GreenMail.

## Catalog provenance

The `.agents` payload defines `researcher` and vendors two skills:

- `wiki` from `ncrmro/.agents` revision
  `0750c51f7afc236d85ed43fe6f032a1ffa6be88b`;
- `source-ingest` from `scifireality/artera/.agents` revision
  `a621fe191bb1e758839fd99322e4e134d02698e9`.

The requirements pin the M1 Outfitter source to commit
`c44205ef35265c893ad9f088772c35c71753bfb7` and identify Dotagents protocol
revision `502a9d5`. This source records the pins but does not independently audit
those upstream repositories.

## Persistent engineering problems

- [[Safe Agent Autonomy]]
- [[Idempotent Email Research]]
- [[Source-Traceable Wiki Updates]]
- [[Catalog Resource Collisions]]
- [[Bounded Recursive Research]]
- [[Secret Containment]]

## Limitations

The source is a first-pass specification. It describes target behavior and does
not prove that the CRDs, controller, local environment, or email demo exist.
