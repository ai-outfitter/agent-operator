---
title: Multi-Agent Review Convergence
type: problem
status: open
tags:
  - system/agent-operator
  - topic/agent-runtime
  - process/code-review
  - method/multi-agent
  - control/access-control
created: 2026-07-20
updated: 2026-07-20
---

# Multi-Agent Review Convergence

## Problem

In the [[Code Implementation Workflow]] an implementer agent and one or more
reviewer agents exchange pull-request feedback until a change is approved. Without
a mechanism that guarantees termination, the loop can fail in several ways: endless
back-and-forth, reviewers rubber-stamping to close the task, implementers thrashing
on contradictory feedback, or two agents agreeing on a change that is still wrong.
The challenge is to make the implement–review cycle converge on a correct,
policy-compliant change in a bounded number of rounds.

## Approaches

Competing options, none yet chosen:

- **Bounded rounds** — cap review iterations and escalate to a human or a senior
  agent when the cap is hit.
- **Evidence-gated review** — require reviewers to ground requested changes in
  tests, CI results, or a policy, rather than opinion, so disagreements are
  decidable.
- **Distinct reviewer perspectives** — assign non-overlapping concerns (tests,
  security, style, architecture) to avoid redundant or conflicting loops.
- **Human or senior-agent tiebreaker** — a required approval gate that resolves
  deadlock and prevents mutual rubber-stamping.
- **Separation of duties** — implementer and reviewer are always distinct
  [[Agent|agents]], so no agent approves its own work ([[Safe Agent Autonomy]]).

## Open questions

- How many review rounds are typical before diminishing returns, and where should
  the cap sit?
- How is reviewer disagreement adjudicated without a human in every loop?
- How is rubber-stamping detected — is a passing test suite a sufficient gate?
- Does a fresh implementer per iteration reduce or increase thrash?
