---
title: Local Development Environment
type: concept
status: draft
tags:
  - system/link-operator
  - system/kubernetes
  - environment/local-development
  - method/integration-testing
  - process/email-processing
created: 2026-07-19
updated: 2026-07-19
---

# Local Development Environment

The intended local stack uses devenv v2 to expose developer tasks and a
microVM containing single-node k3s. GreenMail supplies isolated IMAP and SMTP,
and the environment also hosts the operator, agent image, and writable wiki
fixture needed for the [[Email Paper Research Workflow]].

The developer interface is expected to provide tasks for cluster startup,
operator installation, the M1 demo, verification, and normal shutdown. Normal
shutdown preserves reusable images, model caches, and evidence; destructive
reset operations must be named explicitly.

This is a specified target environment rather than a currently implemented
facility. Its components and status are recorded in the
[[sources/2026-07-19-link-operator-requirements/source|M1 requirements]].

