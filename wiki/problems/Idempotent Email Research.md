---
title: Idempotent Email Research
type: problem
status: open
tags:
  - process/email-processing
  - process/source-ingestion
  - method/idempotency
  - system/link-operator
created: 2026-07-19
updated: 2026-07-19
---

# Idempotent Email Research

## Problem

The [[Email Paper Research Workflow]] crosses IMAP, filesystem, Git, model, and
SMTP boundaries without a single transaction. A crash after committing but
before replying can cause duplicate wiki commits or replies when the same
message is processed again.

## Current approach

Persist a state machine keyed by RFC Message-ID, with a content digest fallback.
Record the source digest and local commit SHA, send at most one permanent-failure
reply, and mark IMAP complete only after SMTP accepts the reply.

## Verification need

The acceptance demo must redeliver the same message and show that both commit
count and reply count remain unchanged. These requirements are specified in
[[sources/2026-07-19-link-operator-requirements/source|the M1 requirements]];
the implementation still needs failure-injection tests at every state boundary.

