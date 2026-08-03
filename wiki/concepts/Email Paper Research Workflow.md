---
title: Email Paper Research Workflow
type: concept
status: draft
tags:
  - system/agent-operator
  - process/email-processing
  - process/source-ingestion
  - method/idempotency
  - topic/research-paper
created: 2026-07-19
updated: 2026-07-21
---

# Email Paper Research Workflow

The M2 Agent Operator workflow begins when the [[Researcher Agent]]
receives an email containing exactly one PDF. It maps the request to an
[[Organization]], preserves the paper, updates the organization's wiki, creates
one local Git commit, and sends a reply in the original email thread.

The durable processing states are `received`, `running`, `committed`, `replied`,
and `failed`. Message-ID, attachment digest, and commit SHA let a restart resume
without repeating completed side effects. Successful JMAP submission precedes
marking the source message complete.

The research portion uses [[Wiki Source Ingestion]]. It records linked papers as
depth-one candidates but does not fetch them in M2. The workflow's current
acceptance boundary is under `docs/milestones/M2-email-paper-research/`; it was
originally defined as M1 in the
[[sources/2026-07-19-agent-operator-requirements/source|initial requirements]].

## Persistent problems

- [[Idempotent Email Research]]
- [[Source-Traceable Wiki Updates]]
- [[Secret Containment]]
