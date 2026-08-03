---
title: Idempotent Email Research
type: problem
status: open
tags:
  - process/email-processing
  - process/source-ingestion
  - method/idempotency
  - system/agent-operator
created: 2026-07-19
updated: 2026-07-21
---

# Idempotent Email Research

## Problem

The [[Email Paper Research Workflow]] crosses JMAP, filesystem, Git, model, and
wiki boundaries without a single transaction. A crash after committing but
before replying can cause duplicate wiki commits or replies when the same
message is processed again.

## Current approach

Persist a state machine keyed by RFC Message-ID, with a content digest fallback.
Record the source digest and local commit SHA, send at most one permanent-failure
reply, and mark the source message complete only after JMAP accepts the reply.

## Verification need

The acceptance demo must redeliver the same message and show that both commit
count and reply count remain unchanged. This is an M2 requirement, first
specified in
[[sources/2026-07-19-agent-operator-requirements/source|the initial requirements]];
the implementation still needs failure-injection tests at every state boundary.
