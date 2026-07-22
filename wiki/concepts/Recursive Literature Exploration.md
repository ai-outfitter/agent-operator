---
title: Recursive Literature Exploration
type: concept
status: draft
tags:
  - topic/research-paper
  - process/source-ingestion
  - method/graph-traversal
  - control/research-budget
created: 2026-07-19
updated: 2026-07-21
---

# Recursive Literature Exploration

Recursive literature exploration follows references or links from a seed paper
to candidate sources, ingests selected candidates, and repeats from the newly
added evidence. The seed is depth zero and directly linked candidates are depth
one.

Link Operator defers downloading candidates beyond the seed during M2. The
future workflow has a hard maximum depth of five and is expected to deduplicate
by DOI, canonical URL, and content digest while enforcing paper, byte, time, and
model-cost budgets.

The depth limit and deferred behavior came from the
[[sources/2026-07-19-link-operator-design-conversation/source|design conversation]]
and are captured in the
[[sources/2026-07-19-link-operator-requirements/source|task requirements]].

## Persistent problem

See [[Bounded Recursive Research]].
