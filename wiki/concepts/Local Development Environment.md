---
title: Local Development Environment
type: concept
status: active
tags:
  - system/agent-operator
  - system/kubernetes
  - environment/local-development
  - method/integration-testing
  - process/email-processing
created: 2026-07-19
updated: 2026-07-21
---

# Local Development Environment

The local stack uses devenv v2 to expose developer tasks and a microVM
containing single-node k3s. Stalwart supplies isolated JMAP mailboxes, and the
environment also hosts the operator and agent images. M1 uses this stack for a
plain-message receive/reply round trip; the [[Email Paper Research Workflow]]
adds the writable wiki fixture in M2.

The developer interface provides tasks for cluster startup, operator
installation, the M1 email demo, and normal shutdown. Normal shutdown preserves
reusable images, model caches, and evidence; destructive reset operations are
named explicitly. M2 will add its paper-research and verification tasks.

The implemented M1 contract is under `docs/milestones/M1-email-round-trip/`.
The original target environment was recorded in the
[[sources/2026-07-19-agent-operator-requirements/source|initial requirements]],
which used GreenMail before the JMAP/Stalwart revision.
