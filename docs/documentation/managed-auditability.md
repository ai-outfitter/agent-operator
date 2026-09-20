# Managed resident auditability

Agent Operator can attach an operator-owned Pensieve collector to a resident
agent and gate readiness on an isolated acceptance probe. The catalog cannot
replace the collector, sink, workload-token audience, or capture policy.

This feature proves that one exact Agent generation can write a complete Pi
trace to the configured sink. It does not configure the sink's workload-identity
trust, retention, or read-side auditor authorization; the cluster administrator
must manage those independently.

## Configure the controller

Add these arguments to `controllerManager.container.args` in the Helm values:

```yaml
- --pensieve-collector-image=ghcr.io/ai-outfitter/pensieve@sha256:<digest>
- --pensieve-collector-revision=<40-character-source-commit>
- --pensieve-sink=https://<pensieve-endpoint>
- --pensieve-probe-credential-secret=pensieve-probe-inference
- --pensieve-probe-model-base-url=https://<openai-compatible-endpoint>/v1
- --pensieve-probe-model-api-key-env=OPENAI_API_KEY
```

The collector image must use a `sha256` digest and the revision must be the
source commit contained in that image. Use an HTTPS model endpoint. The sink can
be an in-cluster HTTP service when the cluster network is the trust boundary.

Create the named probe Secret in each audited agent namespace. It must contain
only the inference credential named by `--pensieve-probe-model-api-key-env`.
The operator verifies that the Secret exists without using or logging its
values, mounts it only into the short-lived probe Job, and disables that Job's
Kubernetes API token. The resident service account remains a namespace
administrator under the Agent workspace contract, so this is least-privilege
credential separation for the probe, not a secrecy boundary against the
resident itself.

Configure Pensieve to trust service-account tokens with audience `pensieve` for
the audited namespaces. The resident and probe receive that dedicated projected
token separately from the resident's Kubernetes API token.

## Opt an Agent in

Select an explicit `provider/model` and set a unique operation nonce:

```yaml
spec:
  profile:
    name: luce
    model: provider/model-id
  auditability:
    profile: resident-complete-trace-v1
    probeNonce: provisioning-operation-123
```

Changing `probeNonce` requests a new acceptance run. The operator creates a
bounded, no-retry Job with a fresh workspace, asks Pi to read a nonce marker,
and enables Pensieve's probe-only fail-closed delivery mode. The Job fails if
any record remains in its ephemeral spool; ordinary resident sessions retain
availability-first capture on their durable workspace. The operator records the
deterministic run identifier in Agent status and removes the completed Job only
after the successful status is durable.

Wait for the generation-bound result:

```sh
kubectl wait agent/<name> --for=condition=AuditabilityReady --timeout=15m
kubectl get agent/<name> -o jsonpath='{.status.auditability}'
```

Before treating the Agent as billable, an independent auditor must retrieve the
reported run from Pensieve and verify its signed immutable records, complete
capture classes, terminal session record, workload identity, collector image
and revision, policy digest, and the exact probe nonce in both the read-tool
result and transcript. `AuditabilityReady=True` alone is not proof of read-side
verification or billing acceptance.

## Managed boundary

For audited Agents, the operator rejects mutable credential inputs that expose
`PENSIEVE_*` or `OUTFITTER_SYSTEM_DIR`. It installs the collector and root-owned
system hook from operator configuration, so a catalog update cannot silently
turn capture off or redirect it. The status binds `observedGeneration`,
`collectorImage`, `collectorRevision`, `policyDigest`, and probe result for the
external verifier.
