---
title: Link Operator
type: concept
status: draft
tags:
  - system/link-operator
  - system/kubernetes
  - topic/agent-runtime
  - control/resource-governance
created: 2026-07-19
updated: 2026-07-19
---

# Link Operator

Link Operator is a Kubernetes operator intended to run composable agents for
organizations. Its public API has exactly two cluster-scoped custom resources:
[[Organization]] and [[Agent]]. [[Project]] and
[[Project Environment|project environments]] are embedded data rather than
additional CRDs.

The operator reconciles each accepted agent into an
[[Agent Namespace Workspace]], resolves its
[[Dotagents Catalog Composition|Dotagents catalogs]], and runs its selected
profile through Outfitter. The first profile is the [[Researcher Agent]], which
implements the [[Email Paper Research Workflow]].

These statements describe the intended product interface, not implemented
behavior. The implementation-status distinction comes from the
[[sources/2026-07-19-link-operator-requirements/source|Link Operator M1 requirements and catalog]].

## Related problems

- [[Safe Agent Autonomy]]
- [[Catalog Resource Collisions]]
- [[Secret Containment]]
