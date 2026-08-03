---
title: Agent Operator
type: concept
status: active
tags:
  - system/agent-operator
  - system/kubernetes
  - topic/agent-runtime
  - control/resource-governance
created: 2026-07-19
updated: 2026-07-21
---

# Agent Operator

Agent Operator is a Kubernetes operator intended to run composable agents for
organizations. Its public API has exactly two cluster-scoped custom resources:
[[Organization]] and [[Agent]]. [[Project]] and
[[Project Environment|project environments]] are embedded data rather than
additional CRDs.

The operator reconciles each accepted agent into an
[[Agent Namespace Workspace]] and renders pinned source declarations for
[[Dotagents Catalog Composition]]. Outfitter, running inside the agent runtime,
owns source fetching, profile resolution, composition, and launch. The first
profile is the [[Researcher Agent]], whose paper workflow is M2.

The CRDs, controller primitives, local cluster, and M1 email round trip are
implemented. The M2 [[Email Paper Research Workflow]] remains target behavior.
The initial interface came from the
[[sources/2026-07-19-agent-operator-requirements/source|Agent Operator M1 requirements and catalog]].

## Related problems

- [[Safe Agent Autonomy]]
- [[Catalog Resource Collisions]]
- [[Secret Containment]]
