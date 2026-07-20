---
title: Agent
type: concept
status: draft
tags:
  - system/link-operator
  - topic/agent-runtime
  - topic/membership
  - control/access-control
created: 2026-07-19
updated: 2026-07-19
---

# Agent

An agent is both a cluster-deployed worker and a membership identity. It is the
second cluster-scoped Link Operator CRD, alongside [[Organization]]. An agent
may belong to multiple organizations and to explicitly named [[Project|projects]]
within each organization.

The CRD selects a Dotagents agent slug instead of duplicating profile details.
Identity, skills, subagents, model, thinking level, and tool policy live in the
pinned catalog definition. The operator runs the selected profile through
Outfitter with Pi as the M1 harness.

Every accepted agent receives its own [[Agent Namespace Workspace]] and
references namespaced Kubernetes Secrets for email, model-provider, and
optional SSH credentials. The intended behavior is specified by the
[[sources/2026-07-19-link-operator-requirements/source|M1 requirements]].

## First profile

The first concrete agent is [[Researcher Agent|`researcher`]].

