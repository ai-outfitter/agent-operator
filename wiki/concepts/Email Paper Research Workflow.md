---
title: Email Paper Research Workflow
type: concept
status: draft
tags:
  - system/link-operator
  - process/email-processing
  - process/source-ingestion
  - method/idempotency
  - topic/research-paper
created: 2026-07-19
updated: 2026-07-19
---

# Email Paper Research Workflow

The first end-to-end Link Operator workflow begins when the [[Researcher Agent]]
receives an email containing exactly one PDF. It maps the request to an
[[Organization]], preserves the paper, updates the organization's wiki, creates
one local Git commit, and sends a reply in the original email thread.

The durable processing states are `received`, `running`, `committed`, `replied`,
and `failed`. Message-ID, attachment digest, and commit SHA let a restart resume
without repeating completed side effects. SMTP acceptance precedes marking the
IMAP message complete.

The research portion uses [[Wiki Source Ingestion]]. It records linked papers as
depth-one candidates but does not fetch them in M1. The workflow and its
acceptance boundary are defined by the
[[sources/2026-07-19-link-operator-requirements/source|M1 requirements]].

## Persistent problems

- [[Idempotent Email Research]]
- [[Source-Traceable Wiki Updates]]
- [[Secret Containment]]

