---
title: Wiki Change Log
type: reference
status: active
tags:
  - system/agent-operator
  - topic/knowledge-base
  - evidence/design
created: 2026-07-19
updated: 2026-07-21
---

# Wiki Change Log

Append entries in the form `## [YYYY-MM-DD] operation | subject`. Existing
entries are immutable; corrections are new entries.

## [2026-07-19] setup | Initial Agent Operator knowledge graph

Created the vault taxonomy, index, 12 concept notes, 6 problem notes, and 3
source notes from the project design conversation, the current requirements,
and the official Kubernetes ResourceQuota documentation.

## [2026-07-19] update | Forge owner and project hierarchy

Added [[Forge Owner]] and clarified that the product-facing organization/owner
maps to a GitHub or Forgejo owner namespace, while Agent Operator projects group
one or more `owner/repository` resources into a user-meaningful work boundary.

## [2026-07-20] create | Research Wiki Maintainer delivery models

Added [[Research Wiki Maintainer]], framing the paper-to-wiki flow as a
composition and comparing three delivery models — pure operator (this project),
forge-native Actions/GitHub App, and a hybrid in-cluster coordinator that
delegates via forge issues/PRs. Introduced the `method/delivery-model` tag.

## [2026-07-20] create | Code Implementation Workflow and review convergence

Added [[Code Implementation Workflow]] — the multi-agent, pull-request-centric
flow that turns a feature/fix/milestone request into merged code via separate
implementer and reviewer agents — and the companion problem
[[Multi-Agent Review Convergence]]. Introduced the `process/code-review`,
`topic/pull-request`, and `method/multi-agent` tags.

## [2026-07-21] update | Split email transport M1 from paper research M2

Updated the living Agent Operator, agent, local-environment, catalog-composition,
email-research, and recursive-research notes after the milestone split. M1 now
means the graduated Stalwart JMAP receive/reply round trip; the
[[Email Paper Research Workflow]] and [[Researcher Agent]] profile execution are
M2. Preserved the 2026-07-19 source claims as historical evidence and recorded
the subsequent revision on both source notes.

## [2026-07-21] update | Align secret containment with JMAP exposure

Updated [[Secret Containment]] to use the JMAP credential contract and clarify
that the operator checks only whether referenced Secret objects exist, never
their key-level contents.
