---
title: Bounded Recursive Research
type: problem
status: open
tags:
  - topic/research-paper
  - process/source-ingestion
  - method/graph-traversal
  - control/research-budget
created: 2026-07-19
updated: 2026-07-21
---

# Bounded Recursive Research

## Problem

Following every citation from every paper creates an expanding graph of network,
storage, extraction, and model work. Cycles, duplicate publications, unavailable
papers, and adversarial documents make an unbounded traversal unsafe and
non-terminating.

## Current direction

[[Recursive Literature Exploration]] will enforce a hard depth ceiling of five,
deduplicate candidates, and apply paper, byte, time, and model-cost budgets.
M2 records verified depth-one candidates but fetches none of them.

## Open questions

- How are candidates prioritized within a budget?
- What evidence is retained for inaccessible or rejected candidates?
- How does a partially completed traversal resume deterministically?

The boundary is documented by the
[[sources/2026-07-19-agent-operator-requirements/source|current requirements]].
