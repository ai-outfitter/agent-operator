---
title: Wiki Source Ingestion
type: concept
status: draft
tags:
  - process/source-ingestion
  - topic/wiki
  - topic/research-paper
  - method/document-extraction
  - control/provenance
created: 2026-07-19
updated: 2026-07-19
---

# Wiki Source Ingestion

Wiki source ingestion preserves a source artifact and reconciles the durable
knowledge graph around it. For a paper, the [[Researcher Agent]] stores the
untouched PDF under a dated `wiki/sources/<source>/` directory, tracks it with
Git LFS, extracts searchable `content.md` with Docling, and writes a verified
`source.md` provenance note.

The process then updates or creates relevant concepts, maintains `wiki/index.md`,
and appends `wiki/log.md`. Candidate linked papers remain claims to verify, not
evidence, until their identifiers or URLs are checked and the papers are
actually ingested.

This repository vendors separate `wiki` and `source-ingest` skills in the
[[Dotagents Catalog Composition|Dotagents catalog]]. Their first integration
target is specified by the
[[sources/2026-07-19-agent-operator-requirements/source|M1 requirements and catalog]].

## Persistent problem

See [[Source-Traceable Wiki Updates]].
