---
title: Link Operator Wiki
type: reference
status: active
tags:
  - system/link-operator
  - topic/knowledge-base
  - evidence/design
created: 2026-07-19
updated: 2026-07-20
---

# Link Operator Wiki

This vault captures durable domain knowledge for Link Operator. Product plans,
milestones, and acceptance checklists remain under `docs/`; this wiki explains
the systems, boundaries, and persistent problems behind them.

## Taxonomy

- `system/` identifies software and infrastructure systems.
- `topic/` identifies domain entities and enduring subjects.
- `process/` identifies operational flows.
- `method/` identifies implementation and reasoning techniques.
- `control/` identifies security, policy, and resource controls.
- `environment/` identifies execution settings.
- `evidence/` identifies the class of supporting evidence.
- `org/` identifies organizations that own or publish sources.

## Concepts

- [[Link Operator]] — Kubernetes operator for organization-owned, composable agents.
- [[Organization]] — ownership and policy boundary for wikis, catalogs, and projects.
- [[Forge Owner]] — GitHub/Forgejo-style owner namespace behind an organization.
- [[Project]] — embedded organization-owned unit of work.
- [[Agent]] — cluster-deployed worker and many-to-many membership identity.
- [[Researcher Agent]] — the M1 paper-ingestion agent profile named `researcher`.
- [[Agent Namespace Workspace]] — one namespace as an agent's complete autonomy boundary.
- [[Dotagents Catalog Composition]] — commit-pinned profile and skill resolution.
- [[Project Environment]] — embedded template for future project workloads.
- [[Email Paper Research Workflow]] — email-to-wiki processing flow.
- [[Research Wiki Maintainer]] — the paper-to-wiki composition and its operator / Actions / hybrid delivery models.
- [[Code Implementation Workflow]] — multi-agent feature/fix/milestone delivery through pull-request review.
- [[Wiki Source Ingestion]] — preservation, extraction, and graph reconciliation.
- [[Local Development Environment]] — devenv v2, microVM, k3s, and GreenMail stack.
- [[Recursive Literature Exploration]] — bounded follow-up traversal from a seed paper.

## Problems

- [[Safe Agent Autonomy]] — broad namespace freedom without cluster-wide authority.
- [[Idempotent Email Research]] — preventing duplicate commits and replies across retries.
- [[Source-Traceable Wiki Updates]] — keeping synthesis tied to immutable evidence.
- [[Catalog Resource Collisions]] — composing catalogs without hidden replacement.
- [[Bounded Recursive Research]] — controlling recursive paper discovery and cost.
- [[Secret Containment]] — making credentials usable without leaking them.
- [[Multi-Agent Review Convergence]] — making the implement/review loop terminate correctly.

## Sources

- [[sources/2026-07-19-link-operator-design-conversation/source|Link Operator design conversation]]
- [[sources/2026-07-19-link-operator-requirements/source|Link Operator M1 requirements and catalog]]
- [[sources/2025-11-20-kubernetes-resource-quotas/source|Kubernetes Resource Quotas documentation]]

## Maintenance

- [[log|Wiki change log]]
