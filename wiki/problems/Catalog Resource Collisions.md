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
updated: 2026-07-19
---

# Catalog Resource Collisions

## Problem

Multiple Dotagents catalogs can define the same resource identity. Silently
choosing one by declaration order can mask configuration mistakes and makes an
[[Agent]] profile depend on an implicit precedence rule.

## Current approach

[[Dotagents Catalog Composition]] concatenates disjoint resources and rejects
duplicate `<resource-kind>/<slug>` identities with a stable error. M1 has no
override, shadowing, or last-source-wins semantics.

## Deferred alternative

Replacement can be introduced later only with a concrete need, explicit
precedence, conflict reporting, and tests that exercise replacement. This
simplification was selected in the
[[sources/2026-07-19-link-operator-design-conversation/source|design conversation]]
and is specified in the
[[sources/2026-07-19-link-operator-requirements/source|requirements]].
