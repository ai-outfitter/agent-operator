# Use case: Grafana alert investigator

An example composition over the Link Operator [primitives](../architecture.md):
an agent that is woken when an observability alert fires, investigates the
alerting resource across Grafana's signals and — because it runs in the failing
environment's cluster — the Kubernetes API, and comments its diagnosis on the
issue that already tracks the alert. The **alert (an Alertmanager webhook) is its
channel**; the `grafana-alert-investigate` and `alert-issue-triage` skills are
its **tools**. A different composition would swap the channel (email, GitHub
notifications) or the tools while reusing the same workspace, secret/config
exposure, catalog, and delegation primitives.

It is grounded in a recurring cost. We run long, large, resource-heavy jobs, and
the [`kube-prometheus-stack`](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack)
(Prometheus + Alertmanager + Loki + Alloy + Grafana) fires alerts against them
that a human triages one by one. Two patterns dominate:

- **Known-noisy** — a high-CPU alert on a job we already know is CPU-bound. The
  agent confirms it is sitting at its normal ceiling and recommends ignoring this
  instance or tuning the alert.
- **Real anomaly** — a process that randomly dies (OOMKill, non-zero exit, node
  eviction). The agent investigates across logs, metrics, traces, profiles, and
  pod state, and recommends a concrete next step.

The agent's scope ends at the comment. It never mutates a workload and never
attempts the fix — it documents that the issue could be **assigned to a team,
another agent, or a human** to carry the fix out.

The agent composition itself lives in the community catalog as the agnostic
[`grafana-alert-investigator`](https://github.com/ai-outfitter/community-profiles/blob/main/docs/grafana-alert-investigator.md)
profile; this page covers what the operator adds to run it in-cluster.

Follow the [quick start](quick-start.md) first to stand up the cluster, an
organization, and an agent's workspace.

## What it needs

Beyond the operator and cluster prerequisites, this composition needs:

- the community catalog pinned by the organization (the profile, the two skills,
  and its Grafana MCP declaration);
- an already-installed **Grafana MCP server**
  ([`grafana/mcp-grafana`](https://github.com/grafana/mcp-grafana)) exposing
  Loki logs, Prometheus metrics, Tempo traces, and Pyroscope profiles;
- an already-installed **`kube-prometheus-stack`** that raises the alerts and
  posts them to the agent (see [How it is triggered](#how-it-is-triggered));
- **read-only** Kubernetes RBAC on the agent's service account for the namespaces
  it investigates, so it can inspect pod state, events, and previous logs in the
  failing environment; and
- a GitHub (or Forgejo) token for `gh` to search issues and post the comment.

The operator installs none of the observability stack. It resolves the catalog,
exposes the credentials, grants the read-only RBAC, and runs the agent.

## Pin the catalog

An `Organization` pins the community catalog by revision (mirror
`dev/demo/mail-loop/organization.yaml`):

```yaml
apiVersion: link.aioutfitter.com/v1alpha1
kind: Organization
metadata:
  name: alert-investigator-demo
spec:
  displayName: Alert investigator demo
  agentCatalogs:
    - name: community-profiles
      github: ai-outfitter/community-profiles
      revision: 4a666180d1805f401f246d2c77253da8354255b7
      path: .
```

The `Agent` selects the profile and receives the credentials:

```yaml
apiVersion: link.aioutfitter.com/v1alpha1
kind: Agent
metadata:
  name: grafana-alert-investigator
spec:
  memberships:
    - organization: alert-investigator-demo
  profile:
    agent: grafana-alert-investigator
    harness: pi
  credentials:
    - secret: grafana-mcp
      as: env
    - secret: alert-investigator-github
      as: env
```

## Credentials

The operator exposes referenced Secrets by name without inspecting them (quick
start §4); the keys inside are *this composition's* contract, defined here.
Create these Secrets in the agent's namespace.

The Grafana MCP server reads this `grafana.env` contract:

```dotenv
GRAFANA_URL=https://grafana.monitoring.svc.cluster.local
GRAFANA_API_KEY=REPLACE_ME
```

```sh
kubectl -n agent-grafana-alert-investigator create secret generic \
  grafana-mcp \
  --from-env-file=grafana.env
```

Create the GitHub token Secret the `gh`-based skill uses:

```sh
kubectl -n agent-grafana-alert-investigator create secret generic \
  alert-investigator-github \
  --from-env-file=github.env
```

Remove the temporary credential files when they are no longer needed. Bootstrap
Secret volumes are mounted read-only. The agent is the administrator of its
namespace workspace and cannot access Secrets in another namespace.

## How it is triggered

This composition is **event-driven** — the alert wakes it — rather than the
resident poll loop the researcher composition runs. A firing alert is delivered
to a webhook receiver in the agent's namespace, which materializes one
investigation per alert. There are two ways to send it, and both deliver an
Alertmanager-compatible webhook payload, so the receiver and the agent's
`trigger_context` are identical either way:

- **Grafana-managed alerting (preferred).** Grafana's own unified alerting
  evaluates the rule and routes it through a **notification policy** to a
  **webhook contact point** pointed at the receiver. This is the more cohesive
  path: the same Grafana the agent already investigates through owns the rule,
  so the alert, its rule definition, and the evidence live in one place — and the
  agent can read the rule back through the Grafana MCP.
- **Alertmanager.** The `kube-prometheus-stack` Alertmanager routes the alert to
  a webhook receiver alongside the receivers that page humans (`continue: true`).
  Use this when the alerts are Prometheus-evaluated rules you already run through
  Alertmanager.

Supporting the receiver that materializes one investigation per alert builds on
the operator's existing **Channel** and **Subagent = Job** primitives; see
[Webhook-driven agents](../design/webhook-driven-agents.md). The receiver plumbing
does not exist yet, so this page describes the composition; the design note
describes the trigger to build.

## Investigate an alert

Once triggered for a firing alert, the agent will:

1. receive the alert's labels and annotations (alertname, namespace, workload,
   severity, start time) as a `trigger_context` of opaque identifiers;
2. run the selected Dotagents agent through Outfitter and Pi;
3. gather evidence for the alerting resource via the Grafana MCP — the firing
   rule and threshold, the Prometheus series against its own history and limits,
   Loki logs, Tempo traces, and Pyroscope profiles;
4. corroborate with the read-only Kubernetes API — `describe pod`, `get events`,
   and `logs --previous` for exit reasons, OOMKills, and evictions;
5. classify the alert as `expected` (known-noisy → recommend ignoring or tuning)
   or `anomaly` (real → investigate) with a confidence level;
6. find the existing issue tracking the alert and post one comment with the
   classification, evidence, and one recommended next step; and
7. note in the comment that the issue can be assigned to a team, another agent,
   or a human to carry out the fix.

Today the agent only reads and only comments: it never scales, restarts, or edits
a workload, never opens or edits issues, and posts exactly one comment per run.
