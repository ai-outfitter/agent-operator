---
title: Safe Agent Autonomy
type: problem
status: open
tags:
  - system/kubernetes
  - topic/agent-runtime
  - control/access-control
  - control/resource-governance
  - environment/agent-namespace
created: 2026-07-19
updated: 2026-07-19
---

# Safe Agent Autonomy

## Problem

An [[Agent]] needs enough Kubernetes authority to schedule, inspect, and clean
up its own work without waiting for the operator to model every workload. The
same authority must not allow it to consume unbounded cluster capacity, enter
another namespace, or weaken the guardrails defining its boundary.

## Current approach

The [[Agent Namespace Workspace]] combines a namespaced `admin` RoleBinding
with an operator-owned ResourceQuota and LimitRange. Kubernetes documents quota
as an aggregate namespace constraint and recommends preventing users from
updating or deleting the quota itself; see
[[sources/2025-11-20-kubernetes-resource-quotas/source|Kubernetes Resource Quotas documentation]].

## Open questions

- Which admission controls are needed in addition to RBAC and quota?
- How should quota exhaustion be surfaced without retry loops?
- Which namespace resources require extra policy despite namespaced `admin`?

The chosen boundary is a design requirement, not yet an experimentally verified
security claim. See
[[sources/2026-07-19-agent-operator-requirements/source|Agent Operator M1 requirements]].
