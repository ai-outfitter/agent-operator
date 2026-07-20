---
title: Dotagents Catalog Composition
type: concept
status: draft
tags:
  - system/dotagents
  - system/link-operator
  - topic/agent-catalog
  - method/catalog-composition
  - control/supply-chain
created: 2026-07-19
updated: 2026-07-19
---

# Dotagents Catalog Composition

An [[Organization]] declares Git-backed Dotagents catalogs containing agent
profiles, skills, and related resources. Remote catalogs are pinned to immutable
commit SHAs. An [[Agent]] selects an agent slug resolved from this set, and
Outfitter runs the resulting profile.

M1 concatenates disjoint catalog resources. It indexes each resource by
`<resource-kind>/<slug>` and rejects any duplicate, even when the duplicate
contents are identical. Declaration order has no precedence meaning; override,
shadowing, and last-source-wins behavior are deferred until a concrete use case
can define and test explicit precedence.

For the first milestone, the Link Operator repository's `.agents` payload is a
single catalog that defines [[Researcher Agent|`researcher`]] and vendors the
`wiki` and `source-ingest` skills. The model and pinned revisions are recorded
by the
[[sources/2026-07-19-link-operator-requirements/source|requirements and catalog source]].

## Persistent risk

See [[Catalog Resource Collisions]].
