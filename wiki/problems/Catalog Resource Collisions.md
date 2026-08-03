---
title: Catalog Resource Collisions
type: problem
status: open
tags:
  - system/dotagents
  - topic/agent-catalog
  - method/catalog-composition
  - control/supply-chain
created: 2026-07-19
updated: 2026-07-21
---

# Catalog Resource Collisions

## Problem

Multiple Dotagents catalogs can define the same resource identity. Silently
choosing one by declaration order can mask configuration mistakes and makes an
[[Agent]] profile depend on an implicit precedence rule.

## Current approach

Agent Operator delegates catalog union and collision behavior to Outfitter. The
operator validates and writes pinned source declarations but does not inspect
resource identities or choose a winner. Collision policy therefore belongs to
Outfitter's documented composition contract.

## Deferred alternative

If Agent Operator ever adds policy above Outfitter, it needs a concrete use case,
explicit precedence, conflict reporting, and tests that exercise replacement.
The initial design selected controller-side duplicate rejection in the
[[sources/2026-07-19-agent-operator-design-conversation/source|design conversation]],
but the later architecture revision removed that parallel resolver.
