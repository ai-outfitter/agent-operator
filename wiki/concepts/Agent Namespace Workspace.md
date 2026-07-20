---
title: Agent Namespace Workspace
type: concept
status: draft
tags:
  - system/kubernetes
  - topic/agent-runtime
  - control/access-control
  - control/resource-governance
  - environment/agent-namespace
created: 2026-07-19
updated: 2026-07-19
---

# Agent Namespace Workspace

An agent's entire Kubernetes namespace is its workspace and autonomy boundary.
The design does not restrict the workspace to one PVC or directory. Within its
namespace the agent can organize work using namespaced workloads, storage,
configuration, service accounts, and RBAC resources.

The operator bootstraps a service account, a namespaced binding to the built-in
`admin` ClusterRole, a ResourceQuota, a LimitRange, a runtime Deployment, and
bounded mailbox state. The operator owns the quota and defaults; the agent owns
the work it schedules within those constraints.

Kubernetes documents ResourceQuota as limiting aggregate consumption and object
counts per namespace, rejecting quota violations, and using LimitRange defaults
when workloads omit compute requests or limits. See
[[sources/2025-11-20-kubernetes-resource-quotas/source|Kubernetes Resource Quotas documentation]].
The Link Operator-specific boundary comes from the
[[sources/2026-07-19-link-operator-requirements/source|M1 requirements]].

## Design consequence

Every [[Project Environment]] and subagent workload remains inside the invoking
agent's namespace and consumes the same aggregate quota. This is the core
mechanism for [[Safe Agent Autonomy]].
