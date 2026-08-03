---
title: Secret Containment
type: problem
status: open
tags:
  - system/kubernetes
  - topic/agent-runtime
  - control/credential-management
  - control/access-control
  - process/email-processing
created: 2026-07-19
updated: 2026-07-21
---

# Secret Containment

## Problem

The [[Researcher Agent]] needs JMAP, model-provider, and sometimes SSH
credentials. Those values must be available to the runtime without entering CRD
specifications, catalog settings, Git worktrees, status, events, logs, or email
replies.

## Current approach

The [[Agent]] CRD stores Secret names only. Values live in ordinary Kubernetes
Secrets inside the [[Agent Namespace Workspace]], and bootstrap volumes are
mounted read-only. The operator reports missing referenced objects using
redacted readiness conditions but does not inspect or validate their keys.

## Limitation

The namespace-admin agent can read and manage Secrets in its own workspace; the
boundary prevents cross-namespace access rather than hiding workspace Secrets
from the agent itself. Credential-class allow-listing for child
[[Project Environment|environment]] Pods is planned but does not revoke the
parent agent's namespace authority.

This trust model is defined by the
[[sources/2026-07-19-agent-operator-requirements/source|M1 requirements]].
