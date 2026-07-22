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
updated: 2026-07-21
---

# Dotagents Catalog Composition

An [[Organization]] declares Git-backed Dotagents catalogs containing agent
profiles, skills, and related resources. Remote catalogs are pinned to immutable
commit SHAs. An [[Agent]] selects an agent slug resolved from this set, and
Outfitter runs the resulting profile.

The operator validates source declarations and serializes their pinned
revisions into `.agents/settings.yml` in declaration order. It does not clone,
index, merge, or resolve catalog resources. Outfitter owns fetching,
union/conflict behavior, profile resolution, composition, and launch; the
operator must not grow a parallel Dotagents resolver.

M2 uses the Link Operator repository's `.agents` payload as the single source
that defines [[Researcher Agent|`researcher`]] and vendors the `wiki` and
`source-ingest` skills. The model and pinned revisions were recorded by the
[[sources/2026-07-19-link-operator-requirements/source|requirements and catalog source]].

## Persistent risk

See [[Catalog Resource Collisions]].
