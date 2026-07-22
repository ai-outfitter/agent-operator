---
title: Researcher Agent
type: concept
status: draft
tags:
  - system/link-operator
  - system/dotagents
  - topic/agent-runtime
  - process/source-ingestion
  - process/email-processing
created: 2026-07-19
updated: 2026-07-21
---

# Researcher Agent

`researcher` is the initial Dotagents profile and the name of the M1 email
[[Agent]]. Profile execution and paper research are M2 behavior.
It composes the vendored `wiki` and `source-ingest` skills, treats messages and
papers as untrusted research material, updates an [[Organization]] wiki, and
reports the resulting local commit.

The profile is intended to perform [[Wiki Source Ingestion]] for an emailed PDF
and identify candidate papers for later [[Recursive Literature Exploration]].
It must not fetch those candidates during M2 and must not put credentials or
private keys into the wiki, Git history, logs, or replies.

The profile name and composition were chosen in the
[[sources/2026-07-19-link-operator-design-conversation/source|design conversation]]
and are encoded in the
[[sources/2026-07-19-link-operator-requirements/source|repository catalog]].
