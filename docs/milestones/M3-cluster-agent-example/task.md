# M3: Nonprod Cluster Agent Example

## Summary

Deploy a released Link Operator to the Unsupervised nonprod Kubernetes cluster
and prove one useful in-cluster composition: a person asks a Slack bot to
investigate cluster state, the bot uses the Grafana MCP plus read-only Kubernetes
access, and it replies in the originating Slack thread with a source-attributed
diagnosis.

This is the first shared-cluster milestone. M1 and M2 use an isolated local
cluster; M3 must exercise the deployment, security, observability, and recovery
paths needed for a long-running agent in
`~/repos/unsupervised/unsupervised/unsupervised-main`.
The executable acceptance contract is [demo.md](demo.md).
The persistent Slack runtime setup is captured in the
[nonprod Slack bot runbook](../../runbooks/nonprod-slack-bot.md).

## Outcome

A message such as:

> @cluster-agent Investigate why pods in the demo namespace are restarting.
> Use the last 30 minutes and do not change anything.

receives exactly one threaded reply containing the scoped findings, the Grafana
queries and Kubernetes evidence used, a confidence level, and a recommended next
step. The agent runs inside nonprod, reads the nonprod Grafana data through its
MCP server, and cannot mutate application workloads.

## Repository ownership

This milestone intentionally spans two repositories.

| Repository | Owns |
| --- | --- |
| `ai-outfitter/link-operator` | Releasable operator and agent images, install artifact, `Organization`/`Agent` examples, Slack/Grafana agent composition, and end-to-end verification |
| `Unsupervisedcom/unsupervised-main` | Nonprod namespace/release declaration, environment-specific values, secret references, read-only investigation RBAC, NetworkPolicy, Grafana endpoint/auth wiring, and deployment validation |

Credentials and environment-specific endpoints belong in the consuming
`unsupervised-main` deployment or its secret manager, never in the Link Operator
catalog or this repository.

## Dependencies to resolve

- Land or supersede `link-operator` branch `feat/slack-channel`. It documents a
  least-privilege polling Slack channel and delegates the `slack-responder` skill
  to `ai-outfitter/community-profiles`.
- Land or supersede `link-operator` branch
  `docs/usecase-grafana-alert-investigator`. Reuse its Grafana/Kubernetes
  evidence model, but M3 is user-requested through Slack rather than
  Alertmanager-triggered and does not comment on GitHub issues.
- Pin released revisions of Link Operator, Outfitter, the agent runtime, and the
  community catalog. Do not deploy mutable `main`, `staging`, or `latest`.
- Confirm the nonprod Grafana API transport and least-privilege authentication
  contract. `unsupervised-main` currently documents developer-side Grafana MCP
  access through `.mcp.json`; M3 must run that MCP capability inside the agent
  pod without copying a developer's `.env.local`.

## Goals and tasks

### 1. Produce deployable, pinned artifacts (`link-operator`)

- [ ] Decide and document the shared-cluster install surface (versioned Helm
      chart or versioned rendered manifest); include CRDs, controller RBAC,
      controller Deployment, ServiceAccount, and image references.
- [ ] Publish immutable multi-architecture operator and agent-runtime images and
      record their digests.
- [ ] Make installation and upgrade idempotent; prove CRDs can be applied before
      the controller and that a same-version reconcile is a no-op.
- [ ] Add release provenance and rollback instructions. Rollback must preserve
      `Organization`, `Agent`, workspace PVCs, and Secrets.
- [ ] Run unit, controller/envtest, manifest-generation, lint, and image smoke
      tests from a clean checkout.

### 2. Define the nonprod deployment (`unsupervised-main`)

- [ ] Use kube context `unsup-nonprod-engineer` explicitly for all routine
      commands. Identify any cluster-scoped install step that requires
      `unsup-nonprod-admin` and require separate human approval for it.
- [ ] Create a dedicated, clearly labeled namespace/release for the operator and
      a dedicated agent workspace; do not install into `default`, `staging`, or
      `unsupervised-singleton`.
- [ ] Add the pinned Link Operator install and environment values using the
      repository's existing Helm/environment conventions.
- [ ] Declare only Secret names/keys in Git. Source Slack, model-provider, and
      Grafana credentials from the nonprod secret manager and document rotation.
- [ ] Add resource requests/limits, PodDisruptionBudget if appropriate, health
      probes, and monitoring for the controller and agent.
- [ ] Add default-deny NetworkPolicy with explicit egress for Kubernetes DNS/API,
      Slack HTTPS, the model provider, the nonprod Grafana API, and any pinned
      catalog fetch required at startup.
- [ ] Add a rollback and removal path that removes only M3-owned resources.

### 3. Compose the cluster investigator (`link-operator`)

- [ ] Add or pin a `cluster-agent` Dotagents agent that composes:
      `slack-responder` as its channel, a read-only cluster-investigation skill,
      and the Grafana MCP declaration as its observability tool.
- [ ] Keep Slack and Grafana behavior out of the operator controller and CRDs;
      they are agent-layer composition exposed through generic Secret/ConfigMap
      references.
- [ ] Add `Organization` and `Agent` examples with a commit-pinned catalog,
      explicit Slack channel allowlist, default nonprod cluster identity, and
      bounded query window.
- [ ] Make each request carry the Slack channel, message timestamp, requesting
      user, requested namespace/workload, and time window as untrusted runtime
      input.
- [ ] Require a scope before investigation. Reject requests for prod, unbounded
      cluster-wide searches, secret contents, or workload mutation with a clear
      Slack reply.
- [ ] Reply in the originating thread and add the handled reaction only after a
      successful reply. Use the Slack message timestamp as the idempotency key.

### 4. Wire least-privilege access (`unsupervised-main`)

- [ ] Give the agent ServiceAccount only `get`, `list`, and `watch` access to the
      approved nonprod resources and namespaces needed for diagnosis. Do not
      grant `admin`, write verbs, `pods/exec`, `pods/attach`, `secrets`, or
      service-account token reads outside its own operator-managed workspace.
- [ ] If Link Operator's default namespaced `admin` binding remains necessary for
      its workspace, separate that workspace authority from investigation access
      to Unsupervised namespaces and prove the latter is read-only.
- [ ] Package and launch `grafana/mcp-grafana` as an MCP process inside the agent
      pod, and connect it to the Grafana endpoint reachable from the agent
      namespace. Configure read-only access to the nonprod Loki, Prometheus,
      Tempo, and Pyroscope data sources that actually exist.
- [ ] Scope Grafana credentials to query/read operations. Confirm the agent
      cannot edit dashboards, alert rules, data sources, users, or access policy.
- [ ] Restrict the Slack bot to one dedicated test channel and the minimum bot
      scopes: channel history, thread replies, and handled reactions. Use no user
      token or admin scope.
- [ ] Verify NetworkPolicy, Kubernetes RBAC, Grafana permissions, and Slack
      channel allowlisting with explicit negative tests.

### 5. Prove a real investigation

- [ ] Create a disposable `cluster-agent-example-*` namespace in nonprod with a
      deterministic unhealthy fixture (for example, a pod that exits non-zero).
      Label it for agent testing and keep it separate from application releases.
- [ ] Ask the bot in Slack to investigate that namespace over a narrow time
      window.
- [ ] Require the agent to correlate Grafana evidence with Kubernetes pod status,
      events, restart count, and previous container termination reason.
- [ ] Require the reply to distinguish observed facts from inference, name each
      source/data source and time range, report confidence, and recommend one
      non-mutating next step.
- [ ] Send the same Slack request through the poll loop again and prove it creates
      no duplicate thread reply.
- [ ] Restart the agent, send a second request, and prove the channel and MCP
      configuration recover without reinstallation.
- [ ] Run the negative tests in [demo.md](demo.md): forbidden Kubernetes writes,
      forbidden Grafana writes, secret requests, a disallowed Slack channel, and
      an attempted prod investigation all fail closed.

### 6. Automate acceptance and operations

- [ ] Provide one documented deploy command and one verifier for M3. Both must
      require an explicit kube context and namespace; neither may use the current
      kubectl context implicitly.
- [ ] Capture a redacted evidence bundle containing artifact digests, applied
      non-secret manifests, conditions, RBAC/NetworkPolicy checks, Grafana query
      metadata, Slack timestamps/reply text, and restart/idempotency results.
- [ ] Make every failed assertion non-zero and point to the relevant redacted
      artifact.
- [ ] Add an owner/runbook for credential rotation, image rollback, stuck Slack
      messages, MCP failures, and safe teardown.
- [ ] Tear down the unhealthy fixture after verification. Retain the operator and
      bot for an agreed soak period, then make continued operation or removal an
      explicit owner decision.

## Non-goals

- Deploying to or reading from production.
- Letting the bot remediate, restart, scale, patch, exec into, or delete cluster
  resources.
- Reading Kubernetes Secrets or returning credentials, tokens, raw service
  account JWTs, or unrestricted logs to Slack.
- Installing or replacing the Unsupervised observability stack.
- Alertmanager/webhook-triggered investigations or GitHub issue comments; those
  remain a separate composition.
- A general-purpose cluster administrator bot or support for arbitrary Slack
  workspaces/channels.

## Risks

- **Privilege composition.** The agent is an administrator of its own workspace,
  so investigation RBAC must be a distinct read-only grant to target namespaces.
- **Data leakage to Slack/model provider.** Logs can contain customer or secret
  data. Queries must be narrow, evidence must be summarized/redacted, and raw log
  dumps must not be posted.
- **Prompt injection in logs and Slack.** Treat messages, logs, labels,
  annotations, and traces as untrusted evidence, never as instructions.
- **Shared-cluster blast radius.** All tests use a disposable namespace and pinned
  artifacts; teardown targets are resolved and verified before deletion.
- **False confidence.** Absence of Grafana results is not health. The reply must
  identify missing/partial data and corroborate with the Kubernetes API.
- **Credential drift.** Grafana, Slack, and model tokens need named owners,
  rotation procedures, and observable expiry failures.

## Graduation criteria

- [ ] The pinned Link Operator and agent are healthy in Unsupervised nonprod for
      the agreed soak period.
- [ ] [The demo](demo.md) passes from a clean checkout against
      `unsup-nonprod-engineer`.
- [ ] A scoped Slack request produces one accurate, source-attributed threaded
      diagnosis using both Grafana MCP and Kubernetes evidence.
- [ ] Duplicate delivery and agent restart tests pass.
- [ ] All Kubernetes, Grafana, Slack, secret-access, prod-scope, and network
      negative tests fail closed.
- [ ] Rollback and M3-only teardown are documented and rehearsed.
- [ ] The redacted evidence bundle and owning team/sign-off are recorded.
