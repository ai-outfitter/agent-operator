# Design note: webhook-driven agents

**Status:** design stage. Motivated by the
[Grafana alert investigator](../documentation/usecases.grafana-alert-investigator.md)
use case; the receiver plumbing described here is not yet implemented.

## The gap

Today an agent runs a **resident poll loop**: a persistent Deployment that, on
each tick, surveys a prioritized set of input sources and works or delegates them
(see [architecture.md](../architecture.md), "The main-agent loop"). That fits a
mailbox — poll for unprocessed messages — but not an alert. An observability
alert is a **push**: it arrives once, when it fires, and wants a bounded
investigation for that specific alert. Polling Grafana on a timer would be both
laggy and redundant against a system whose whole job is to notify.

The alert can be pushed by either **Grafana-managed alerting** (a webhook
contact point on a notification policy — the preferred, more cohesive path since
the agent already works through Grafana) or **Alertmanager** (a webhook receiver
in `kube-prometheus-stack`). Both send an Alertmanager-compatible webhook
payload, so the receiver below is the same for either.

We want the alert to **wake** the agent, run one investigation scoped to that
alert, and stop — without a resident loop per alert stream and without the
operator learning anything alert-shaped.

## The primitives already fit

Nothing new is needed in the operator's contract. Two existing primitives
compose into the trigger:

- **Channel** — an adapter for an external event source, delivered as a Dotagents
  skill / MCP server / Pi extension inside the runtime. The operator models none
  of it (architecture.md, "Channels"). An Alertmanager webhook is just another
  channel — a push one instead of a poll one.
- **Subagent = ephemeral Job** — a running agent (or the operator on its behalf)
  launches delegated work as a Kubernetes Job in its own namespace, sharing its
  service account, quota, and credentials boundary, bounded by a timeout and
  deleted on cancellation (see [OPR-005](../requirements/OPR-005-subagent-jobs.md)).

An investigation *is* a subagent: one alert in, one bounded read-only run out.

## Sketch

```
Grafana-managed alerting              Alertmanager
(webhook contact point)      —or—     (kube-prometheus-stack webhook receiver)
    │                                     │
    └──────────────┬──────────────────────┘
                   ▼  Alertmanager-compatible webhook payload
Receiver Service  (in the agent's namespace workspace)
    │  validates + normalizes the alert into a trigger_context
    ▼
Subagent Job  (OPR-005)   ── one per firing alert ──▶  grafana-alert-investigator
    profile: grafana-alert-investigator                (investigate → classify →
    read-only RBAC + Grafana MCP + GitHub token         comment on the issue)
    timeout, run-suffix name, quota-bounded
```

1. **Receiver.** A small webhook receiver runs as a Service in the agent's
   namespace, registered as a Grafana webhook contact point or an Alertmanager
   webhook receiver. It authenticates the request, deduplicates by alert
   fingerprint + `startsAt`, and normalizes the payload into the same
   `trigger_context` shape the profile expects (alertname, namespace, workload,
   severity, start time). It treats the payload strictly as data.
2. **Materialize a Job.** Per firing alert (grouped/throttled by the alerting
   source), the receiver launches one subagent Job selecting the
   `grafana-alert-investigator` profile, exactly as OPR-005 describes: same
   namespace, same service account, quota-bounded, timeout-enforced,
   collision-resistant run suffix, labeled with the parent run. Cancellation
   deletes the Job, never the namespace.
3. **Investigate and stop.** The Job runs the headless agent once — gather
   evidence, classify, comment on the tracking issue — and exits. Idempotency
   leans on external read-state (the alert fingerprint and the existing issue
   comment), not on the operator being a database.

The resident agent Deployment is optional in this shape: the receiver can launch
Jobs directly. If a resident agent is present, the receiver can instead enqueue
onto the agent's input sources and let its loop delegate — the same Job either
way.

## What this needs

- **Receiver.** A minimal, generic webhook→Job launcher (not alert-specific). It
  belongs to the composition/runtime layer, mirroring how channels are composed
  today, and should stay alert-agnostic so other push channels (GitHub webhooks,
  CI events) reuse it.
- **Concurrency + backoff.** Per-alertname concurrency limits and dedup so a
  flapping alert cannot fan out into unbounded Jobs — within the namespace quota
  boundary, as OPR-005.6 requires.
- **RBAC.** The read-only Kubernetes access the investigation needs is granted on
  the agent's service account, which the subagent Job inherits.

## Open questions

- Does the receiver live inside the resident agent runtime, or as a standalone
  operator-managed component per agent namespace?
- Should "launch a Job from an inbound event" become a first-class operator
  affordance, or stay entirely in the composition layer like other channels?
- How is the receiver's registration expressed — a Grafana contact point /
  notification policy or an Alertmanager receiver — via a `setup:` step, a
  composition-owned manifest, or out of band?
- Who owns the *setup* side — provisioning the alerting, the receiver, the MCP,
  and the RBAC? This is the platform-provisioning half the
  [grafana-alert-investigator use case](../documentation/usecases.grafana-alert-investigator.md)
  leaves manual today; a conventional **platform profile** is a candidate home
  for it, under evaluation in
  [ai-outfitter/outfitter#197](https://github.com/ai-outfitter/outfitter/issues/197).
