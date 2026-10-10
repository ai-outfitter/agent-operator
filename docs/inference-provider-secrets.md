# Inference provider Secrets

How saved inference provider keys reach agents (ai-outfitter/agent-operator#97,
written by the webapp gateway in ai-outfitter/webapp#222).

## Flow

1. **Organization namespace.** Each `Organization` resource has a namespace
   `outfitter-org-<slug>` labelled `outfitter.ai/organization=<slug>`. The
   slug is the Organization resource name (cluster-scoped, so it is unique),
   so it must be at most 49 characters. The operator creates or labels this
   namespace for every Organization. The webapp gateway may create it first.
   The namespace is not owned by the Organization, so deleting the resource
   leaves the canonical Secrets in place.
2. **Canonical Secrets.** The webapp writes provider Secrets into that
   namespace with the labels `outfitter.ai/inference-provider=<kind>`,
   `outfitter.ai/owner-kind` (`user` or `organization`) and
   `outfitter.ai/owner-id`, and the data keys `apiKey`, `baseUrl` and `model`.
3. **Copies.** A Secret can't be referenced across namespaces, so the
   `providersecret` controller copies each organization-owned
   (`outfitter.ai/owner-kind=organization`) Secret into `agent-<name>` for
   every Agent whose `spec.memberships` names the Organization. A member's own
   key (`owner-kind=user`) is never copied to agents; it stays in the
   organization namespace for the gateway's per-run use. A copy is named `inference-<source name>` and keeps the
   source labels, plus `outfitter.ai/organization=<slug>`, the Agent ownership
   labels and an `outfitter.ai/source=<namespace>/<name>` annotation. Copies
   are owned by the Organization, so garbage collection removes them with it.
   Deleting a source, or removing an Agent's membership, deletes its copies on
   the next reconcile. The controller never overwrites a Secret in an agent
   namespace that isn't a copy of the same source; it logs and skips that one
   copy so the organization's others still sync.
4. **Agent pods.** The agent container mounts all copies as one projected
   Secret volume:

   ```
   /var/run/outfitter/inference/<source secret name>/apiKey
   /var/run/outfitter/inference/<source secret name>/baseUrl
   /var/run/outfitter/inference/<source secret name>/model
   ```

   The kubelet refreshes the files after a rotation without a restart, usually
   within a minute. For consumers that read keys only at start-up, the pod
   template also carries `outfitter.ai/inference-providers-checksum`, a hash
   of the copies' data. A change rolls the Deployment. Adding or removing a
   provider changes the volume and also rolls the pod.

## Inference relay sidecar

Resident agent Pods run a native sidecar, `inference`, from the webapp image
(`node /app/workspace/inference-relay.mjs`), the same relay Workspace Pods run.
It is the first init container with `restartPolicy: Always`, so it starts
before the other init containers and runs beside the agent; it has no probes,
because it listens on loopback only and a kubelet probe targets the Pod IP. A
projected `agent-runtime` ServiceAccount token with audience
`outfitter-inference` is mounted into that container only; the agent
container, the other init containers and the browser sidecar never mount it.
The relay listens on `127.0.0.1:4141` and forwards chat completions to the
gateway with that token. The gateway identifies the agent from the
ServiceAccount `agent-runtime` and the namespace labels `aioutfitter.com/agent`
and `aioutfitter.com/organization`.

What the isolation does and does not provide: the token stays out of the agent
process and its environment. It does not stay out of reach. The
`agent-runtime` ServiceAccount is bound to the namespace `admin` ClusterRole,
so an agent can `exec` into the sidecar or request a token for the same
ServiceAccount itself. Either way the principal the gateway sees is the same
agent, so the agent gains no identity it did not already have. Workspace
runtimes are different: their Pods mount no API token
(`automountServiceAccountToken: false`), so a workspace never holds the
identity.

Operator flags:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--inference-relay-image` | empty | Relay sidecar image, shared with Workspace Pods. Required: the operator exits at startup without it when the Agent controller runs (no `--workspace-only`) or `--workspace-image` is set. With `--workspace-image-require-digest` it must be digest-pinned. |
| `--inference-gateway` | `http://outfitter-webapp.outfitter-cloud.svc.cluster.local:4040` | Gateway URL the agent relay sidecars call. Workspaces keep using `--workspace-gateway`. |
| `--inference-model` | `GLM-5.3-Flash-EXL3` | Model the `outfitter` provider offers. |

The shipped manifests (`config/manager/manager.yaml`, the Helm chart values)
pin the relay to the webapp image digest the gateway currently runs; the relay
image is promoted together with the gateway image.

Upgrading to this release changes every agent's Pod template, so every agent
Pod restarts once. The sidecar requests 50m CPU and 64Mi memory (limits 500m
and 256Mi); a namespace already at its ResourceQuota ceiling needs that much
headroom or the new Pod is not admitted.

The `outfitter-settings` ConfigMap carries a `models.json` that defines the Pi
provider `outfitter` (`baseUrl: http://127.0.0.1:4141/v1`,
`api: openai-completions`, one model named by `--inference-model`). Its
`apiKey` reads `OUTFITTER_INFERENCE_TOKEN`, which the operator sets to a
placeholder: Pi requires an env-backed key and the relay discards the bearer.

The ConfigMap is mounted read-only at `/etc/outfitter-settings`, and the
`copy-settings` init container copies its files (`cp -L`) into an `emptyDir`
that every later init container and the agent mount as `/workspace/.agents`.
A ConfigMap volume serves each key as a symlink into `..data`, and
`outfitter dump --strict` rejects a root file that resolves through a symlink,
even when it is mounted with `subPath`. The underlying cause is Outfitter's
containment check (`Containment.ts`), which treats any relative path starting
with `..` as an escape, including `..data`; the copy works around it here.

The provider is opt-in. An Agent with `profile.model: outfitter/<model>` routes
inference through the relay and the gateway. Other providers are untouched, so
`profile.model: dgx-spark/...` still goes direct to the Spark.

## Not covered yet

- **Workspaces.** `Workspace` has no organization reference in the API, so
  workspace namespaces don't receive copies.
- **Gateway read access.** Kubernetes RBAC can't restrict Secret reads by
  label. Limiting the workspace gateway to the labelled Secrets needs either a
  Role per organization namespace or a dedicated namespace.
- **Encryption at rest.** EKS envelope encryption with KMS for Secrets is an
  infrastructure prerequisite and is configured on the cluster, not by the
  operator.
