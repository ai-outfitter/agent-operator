---
title: Research Wiki Maintainer
type: concept
status: draft
tags:
  - system/agent-operator
  - topic/agent-runtime
  - process/source-ingestion
  - process/email-processing
  - topic/research-paper
  - method/delivery-model
  - control/resource-governance
created: 2026-07-20
updated: 2026-07-20
---

# Research Wiki Maintainer

The Research Wiki Maintainer is the composition that turns an inbound research
paper into a source-traceable update of an [[Organization]] wiki: it receives a
paper, performs [[Wiki Source Ingestion]], and records one commit. In Link
Operator today it is realized by the [[Researcher Agent]] running the
[[Email Paper Research Workflow]] on an [[Agent Namespace Workspace]]. The
composition is independent of *how* it is hosted, and the same flow can be
delivered three different ways. This note compares those delivery models; the
user-facing walkthrough lives in `docs/documentation/usecases.researcher-wiki-maintainer.md`.

## Delivery models

### Pure operator (Agent Operator, this project)

A persistent, in-cluster agent Deployment runs the whole flow itself.
[[Agent Operator]] reconciles the agent's [[Agent Namespace Workspace]], resolves
its catalog, and runs it; the agent watches its channel (email), ingests the paper,
and commits — all inside a bounded namespace governed by a ResourceQuota and an
`admin` RoleBinding. Kubernetes is the runtime, and durable state lives in the
namespace and its workspace volume.

### Actions / GitHub App (forge-native)

No standing agent runtime. A GitHub (or Forgejo) App / bot reacts to events —
webhooks, schedules, an opened issue — under a [[Forge Owner]], and dispatches an
Actions (CI) workflow that runs the agent as an ephemeral job. The job ingests the
paper and opens a commit or pull request, then exits. The forge holds the state:
the issue/PR is the work item, its history is the memory, and review happens
through normal PR mechanics.

### Hybrid (in-cluster coordinator + forge delegation)

A persistent in-cluster agent stays always-on to triage and coordinate, but does
not do the heavy work itself. It opens forge **issues/PRs** describing units of
work that Actions (or humans) then execute and review. The cluster plane provides
the durable event loop and prioritization; the forge plane provides execution,
review, and audit. This keeps a responsive coordinator without paying for
always-on execution capacity.

## Comparison

| Dimension | Pure operator | Actions / App | Hybrid |
| --- | --- | --- | --- |
| Execution locus & lifecycle | persistent in-cluster Deployment | ephemeral CI job per event | persistent coordinator + delegated CI/human execution |
| Trigger model | agent's own loop / channel poll | forge & webhook events | agent loop coordinates, forge events execute |
| State & memory | durable namespace + workspace volume | forge is the state (issues/PRs/history) | durable coordinator + forge as system of record |
| Review & governance | direct local commit | PR-native review | PR-native review + agent triage |
| Isolation & resource governance | namespace + ResourceQuota + `admin` RBAC | CI runner limits | both planes |
| Infra & ops burden | run a cluster + operator | none beyond forge/CI | cluster + forge (both planes) |
| Credentials | Kubernetes Secrets in-namespace ([[Secret Containment]]) | CI / App secrets | both |
| Cost model | always-on compute | per-run CI minutes | mixed: light standing coordinator + on-demand runs |
| Best fit | always-on, high-frequency, long-running, stateful agents | event-driven, PR-centric, low-infra work | always-on coordination with forge-native execution & review |

## Choosing a model

The pure operator wins when the agent needs an **always-on loop**, durable
in-cluster state, or namespace-level isolation and quota — the guarantees behind
[[Safe Agent Autonomy]]. The forge-native model wins when there is **no appetite
for standing infrastructure** and PR-based review is desirable out of the box, at
the cost of cold starts and no persistent memory beyond the forge. The hybrid buys
both an always-on coordinator and forge-native execution/review, but runs two
planes and must keep their credentials and audit trails coherent. All three share
the same ingestion contract, so [[Source-Traceable Wiki Updates]] and idempotency
must hold regardless of where the work runs.

## Related problems

- [[Safe Agent Autonomy]]
- [[Secret Containment]]
- [[Source-Traceable Wiki Updates]]
