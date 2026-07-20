---
title: Kubernetes Resource Quotas
type: source
source_kind: documentation
status: reviewed
authors:
  - Kubernetes Authors
publication: Kubernetes Documentation
published: 2025-11-20
url: https://kubernetes.io/docs/concepts/policy/resource-quotas/
retrieved: 2026-07-19
tags:
  - system/kubernetes
  - control/resource-governance
  - control/access-control
  - environment/agent-namespace
  - evidence/documentation
created: 2026-07-19
updated: 2026-07-19
---

# Kubernetes Resource Quotas

## Provenance

Official Kubernetes documentation page “Resource Quotas,” last modified
2025-11-20 and retrieved 2026-07-19:
<https://kubernetes.io/docs/concepts/policy/resource-quotas/>.

## Relevant findings

- A ResourceQuota constrains aggregate resource consumption within a namespace.
- Quota can cover infrastructure resources and counts of API objects.
- The control plane tracks namespace usage against hard quota limits.
- A create or update that violates quota is rejected with HTTP `403 Forbidden`.
- CPU or memory quotas can require Pods to specify requests or limits.
- A LimitRange can supply defaults for workloads that omit those values.
- Administrators should restrict users from deleting or updating the quota if
  the boundary depends on continued enforcement.

These findings support the resource-governance portion of
[[Agent Namespace Workspace]] and frame the open [[Safe Agent Autonomy]]
problem. The Link Operator-specific choice of a namespaced `admin` RoleBinding
and operator-owned quota comes from
[[sources/2026-07-19-link-operator-requirements/source|the project requirements]],
not from Kubernetes documentation.

## Limitations

ResourceQuota controls aggregate namespace consumption; it is not by itself a
complete security boundary. RBAC, admission controls, workload security, and
cluster configuration remain separate concerns.
