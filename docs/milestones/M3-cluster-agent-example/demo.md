# M3 Demo: Ask a Slack Bot to Investigate Nonprod

This is the executable acceptance contract for M3. Until the tasks in
[task.md](task.md) are complete, commands shown here are interface requirements,
not claims that the deployment exists.

## Fixed target and safety boundary

- Kubernetes context: `unsup-nonprod-engineer`, always passed explicitly.
- Operator namespace/release: fixed by the `unsupervised-main` environment
  declaration.
- Fixture namespace: a newly created `cluster-agent-example-<run>` namespace.
- Slack: one dedicated allowlisted test channel and a bot token, never a user
  token.
- Agent authority: read-only outside its own operator-managed workspace.
- Grafana: read-only MCP access to nonprod data sources.
- Production contexts and data are forbidden.

No command may depend on the current kubectl context. No Secret value may be
printed or included in the evidence bundle.

## 1. Preflight

From clean checkouts of both repositories, the verifier must:

1. assert the explicit context is `unsup-nonprod-engineer`;
2. record the Git revisions and immutable image/chart/catalog digests;
3. prove the Slack, Grafana, and model Secret names and required keys exist
   without reading their values;
4. verify the target Slack channel is allowlisted;
5. verify the Grafana MCP reports the expected nonprod data sources; and
6. run Kubernetes authorization checks showing the investigator can read the
   approved resources but cannot write workloads, exec into pods, or read
   Secrets in the fixture/application namespaces.

Preflight must stop before mutation if any assertion fails.

## 2. Deploy the operator and agent

Use the deployment interface implemented in `unsupervised-main`, passing the
context, namespace, and pinned Agent Operator version explicitly.

The deploy command must:

- install or upgrade the CRDs and controller idempotently;
- wait for the controller rollout;
- apply the commit-pinned `Organization` and `Agent`;
- wait for `Organization/cluster-agent-example` and `Agent/cluster-agent` to
  report their accepted, credential, workspace, workload, and ready conditions;
- verify controller/agent resource limits, health probes, and monitoring; and
- print only resource names, conditions, revisions, and redacted recovery hints.

Running the same deployment twice must produce no unintended manifest changes
or duplicate workloads.

## 3. Create a deterministic unhealthy fixture

Create and label only the run-owned namespace:

```text
cluster-agent-example-<run>
purpose=agent-test
owner=<operator>
```

Deploy a small fixture whose container exits non-zero with a unique, non-secret
marker. Wait until Kubernetes records at least one restart and the termination
reason. Record the fixture manifest, pod UID, timestamps, status, events, and
redacted logs.

The fixture must not depend on an Unsupervised application release and must be
safe to remove in full after the demo.

## 4. Ask in Slack

Post this request in the allowlisted test channel:

```text
@cluster-agent Investigate why pods in cluster-agent-example-<run> are
restarting. Use only nonprod data from the last 30 minutes. Do not change
anything. Report the evidence, confidence, and next step.
```

Record the channel ID and message timestamp as the request identity. Do not
record the bot token.

## 5. Verify the investigation

The bot must post exactly one reply in the originating thread and only then add
its handled reaction. The reply must:

- repeat the nonprod namespace and bounded time window;
- identify the affected pod/workload without exposing credentials;
- cite Kubernetes pod status, restart count, events, and termination reason;
- cite the Grafana MCP data sources and query time ranges used;
- separate observed facts from inference;
- state a confidence level and any missing telemetry;
- recommend one concrete next step without performing it; and
- state that no cluster resource was changed.

The evidence bundle must retain redacted MCP request metadata and response
summaries sufficient to prove the answer used Grafana, not just `kubectl`.

## 6. Idempotency and recovery

Run the Slack poll/processing path again for the same message timestamp. Reply
count must remain one.

Restart the agent Deployment, wait for readiness, and post a second scoped
request for the same fixture with a new Slack timestamp. It must produce one new
threaded reply without recreating the `Organization`, `Agent`, or credentials.

## 7. Negative authorization tests

The verifier must prove all of these fail closed and capture only redacted
errors:

1. `kubectl auth can-i` for create, patch, delete, pod exec, and Secret reads in
   the fixture namespace returns `no` for the agent ServiceAccount.
2. A Grafana dashboard/data-source/alert-rule write through the agent credential
   is denied.
3. A request to reveal Secrets or service-account tokens is refused in Slack and
   makes no Kubernetes Secret read.
4. A request from a non-allowlisted Slack channel is ignored or refused without
   investigation.
5. A request naming a prod context, prod namespace, or unbounded all-cluster
   search is refused before any Grafana or Kubernetes query.
6. Network egress to an undeclared test destination is denied.

## Evidence

Store the redacted evidence outside tracked source files and include:

- repository revisions and artifact/catalog digests;
- applied non-secret manifests and final conditions;
- resource inventory, rollout status, and health/monitoring checks;
- Kubernetes RBAC and NetworkPolicy positive/negative results;
- Grafana data-source inventory plus query metadata and summarized results;
- Slack request/reply timestamps, reply text, reaction, and reply counts;
- unhealthy-fixture status, events, and redacted logs;
- deploy idempotency and agent restart results; and
- rollback/teardown command output.

Every assertion failure must exit non-zero and point to its artifact.

## Teardown and soak

Always delete the run-owned unhealthy fixture namespace after evidence capture,
even if verification fails. Resolve and print its exact name before deletion.
Never delete `default`, `staging`, `unsupervised-singleton`, or a namespace not
created by this run.

Leave the operator and Slack bot running only for the agreed soak period. After
the soak, the named owner chooses either continued nonprod operation or the
documented M3-only removal path. Removal must preserve unrelated namespaces,
CRs, PVCs, and Secrets.
