---
title: Source-Traceable Wiki Updates
type: problem
status: open
tags:
  - topic/wiki
  - topic/research-paper
  - process/source-ingestion
  - control/provenance
  - evidence/design
created: 2026-07-19
updated: 2026-07-19
---

# Source-Traceable Wiki Updates

## Problem

LLM-maintained wiki summaries can become detached from original evidence,
silently duplicate existing concepts, or promote unverified linked-paper claims
into facts. A durable knowledge graph needs provenance that survives later
revisions.

## Current approach

[[Wiki Source Ingestion]] separates immutable source artifacts from extracted
searchable text and synthesized notes. Each source gets a `source.md` graph node;
concepts link back to source directories; the index is maintained; and the log
is append-only. Original papers are stored through Git LFS.

## Open questions

- How should conflicting claims between papers be represented?
- What review state is required before an automatically updated concept is trusted?
- How should candidate links be verified before entering the exploration queue?

The first required workflow is described by
[[sources/2026-07-19-link-operator-requirements/source|the M1 requirements]].

