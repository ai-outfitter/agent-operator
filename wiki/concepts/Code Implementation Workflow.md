---
title: Code Implementation Workflow
type: concept
status: draft
tags:
  - system/agent-operator
  - topic/agent-runtime
  - topic/repository
  - topic/pull-request
  - process/code-review
  - method/multi-agent
  - control/access-control
created: 2026-07-20
updated: 2026-07-20
---

# Code Implementation Workflow

The Code Implementation Workflow turns a request for a **feature, fix, or
milestone** into merged code through a multi-agent, pull-request-centric loop.
Distinct [[Agent|agents]] play separate roles — one implements, others review —
and all coordination happens through a [[Forge Owner|forge]]'s issues and pull
requests, which serve as both the work queue and the system of record. It is the
engineering sibling of the [[Research Wiki Maintainer]] flow: the forge, not the
operator, holds the state of the work.

## Intake

Work enters one of two ways:

- **Requested** — a human (or another system) asks for a feature, fix, or a whole
  milestone.
- **Self-directed** — an [[Agent]] selects work from its own prioritized queue and
  decides how to decompose it, breaking a milestone into tasks or issues.

A planning role may split a large request into tracked issues before any code is
written, so each unit of work maps to one pull request.

## The loop

1. **Implement.** An *implementer* agent takes one task, writes the change, and
   opens a pull request.
2. **Review.** One or more *reviewer* agents — deliberately **distinct** from the
   implementer — read the change and comment on the pull request with requested
   changes, questions, or approval. Reviewers may specialize (tests, security,
   style, architecture).
3. **Iterate.** An implementer agent (the same or a fresh one) addresses the
   requested changes and pushes updates; reviewers re-review.
4. **Converge and merge.** The loop repeats until the reviewers — and any required
   human gate — approve, at which point the change is merged.

Separating implementer from reviewer is a deliberate **separation of duties**: the
review gate, not the author, is where correctness and policy are enforced
([[Safe Agent Autonomy]]).

## How it maps to Agent Operator

- Each role is an [[Agent]], or a subagent launched as a Kubernetes Job from a
  [[Project Environment]] inside an [[Agent Namespace Workspace]], bounded by the
  namespace quota.
- The code lives in the repositories of a [[Project]] under an [[Organization]];
  access is scoped by the agent's membership.
- Implementer and reviewer **profiles** are resolved through
  [[Dotagents Catalog Composition]], so the same catalog can supply several review
  personas.
- The pull request is the coordination substrate: comments, requested changes, and
  approvals are durable forge state, not in-cluster state.

## Delivery models

Like the [[Research Wiki Maintainer]], this flow can run pure-operator,
forge-native (Actions / App), or hybrid — see that note's comparison. Because
review and iteration are inherently pull-request-centric, this flow leans on the
forge as the collaboration plane: the hybrid model (a persistent in-cluster
coordinator that opens and shepherds PRs while implementer and reviewer runs
happen as Jobs or Actions) fits it most naturally.

## Related problems

- [[Multi-Agent Review Convergence]]
- [[Safe Agent Autonomy]]
- [[Secret Containment]]
