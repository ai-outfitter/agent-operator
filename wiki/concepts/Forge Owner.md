---
title: Forge Owner
type: concept
status: draft
tags:
  - system/agent-operator
  - topic/organization-model
  - topic/forge-owner
  - topic/repository
created: 2026-07-19
updated: 2026-07-19
---

# Forge Owner

A forge owner is the account or organization namespace that forms the first
segment of a GitHub-, Forgejo-, or compatible forge repository identity:
`<owner>/<repository>`.

In Agent Operator, [[Organization]] is the product-facing owner boundary and is
intended to map directly to this forge namespace. Repository declarations retain
the familiar owner/repository identity, but the primary user hierarchy is
`<owner>/<project>` because one [[Project]] may group many repositories,
environments, and agents around a larger unit of work.

This mapping is a design direction recorded in the
[[sources/2026-07-19-agent-operator-design-conversation/source|project design conversation]].
It has not yet been reconciled into the CRD schema or tested against individual
forge providers.

