# OPR-003: Agents

An agent runs as a persistent Deployment; its namespace is its workspace. Channel
implementations and tools are composed at the agent layer; the controller only
projects the typed runtime channel selection. See
[architecture.md](../architecture.md).

An `Agent` is a cluster-deployed worker and membership identity. Its selected
Dotagents agent definition is its composable Outfitter profile; there is no
separate profile resource. The operator provisions the agent's workspace and runs
it, but treats what the agent *does* — its channels, tools, and subagents — as
opaque composition.

## OPR-003.1: API identity and membership

`Agent` MUST be a cluster-scoped resource served as
`aioutfitter.com/v1alpha1`, kind `Agent`. Its name MUST be a DNS label no
longer than 57 characters so its namespace can be `agent-<name>`.

`spec.memberships` MUST be a **list** of `{organization, projects?}` entries with
unique organization references. This is a many-to-many relationship: an agent may
belong to many organizations and, through them, many projects; an organization or
project may be referenced by many agents.

Every referenced organization and project MUST exist before `Accepted=True`. An
empty or omitted `projects` list grants organization-level access only; it MUST
NOT mean every project. The memberships are how an agent knows which
organizations, projects, teams, and environments it may act within.

## OPR-003.2: Dotagents runtime

`spec.image` MAY select a user-owned runtime image. When it is omitted, the
operator MUST use its configured default image. The operator MUST pass the image
reference through to Kubernetes without adding registry, signature, or
immutability policy; cluster owners can enforce those constraints with standard
Kubernetes admission controls.

`spec.profile.agent` MUST select an agent slug resolved from the organization's
commit-pinned catalogs. `spec.profile.harness` MUST default to `pi`. The
runtime MUST invoke the equivalent of:

```text
outfitter run <agent-slug> --harness pi
```

The agent definition supplies identity, skills, subagents, model, thinking
level, and tool policy according to the pinned Dotagents revision. The operator
MUST NOT copy those fields into the `Agent` CRD.

The operator MAY inspect the image reference for one purpose only: deciding
whether to provision the persistent Nix-store machinery. A tag ending `-nix`
(the published convention for the Nix closure variant) selects the machinery; a
strict `MAJOR.MINOR.PATCH` tag of `1.5.0` or later — no `v` prefix, no
prerelease or build suffix — (the Debian-base primary tag) and a bare digest
reference omit it; every other reference, including `v`-prefixed or prerelease
tags, keeps the machinery as a conservative default so existing closure-image
deployments continue to work.

The published Outfitter image is the default generic runtime. Users MAY select it
directly or supply a derivative containing additional tools. Channel and tool
dependencies belong to the selected profile or user-owned image, not to the
operator's contract; the operator does not care what capabilities the image
contains.

## OPR-003.3: Namespace workspace and owned resources

The entire agent namespace MUST be the agent's workspace and autonomy boundary,
not one designated PVC or directory. The agent may organize its work as any
namespaced Kubernetes resources it needs, subject to its quota.

The controller MUST reconcile the following guardrails and bootstrap resources
for every accepted agent:

- namespace `agent-<agent-name>`;
- one runtime service account;
- one RoleBinding from that service account to the built-in `admin` ClusterRole;
- one operator-owned ResourceQuota named `agent-workspace`;
- one operator-owned LimitRange named `agent-workspace-defaults`;
- one durable per-agent workspace volume; and
- one long-running agent Deployment.

The durable volume gives the agent a working cache and Git working tree that
survive pod restarts. It is a cache, not a system of record: authoritative state
lives in external services (a mail server, GitHub/Forgejo, the wiki's Git
remote), and the agent manages any additional persistence it wants within quota.
The operator does NOT own a channel-state resource such as a mailbox ConfigMap;
channel and processing state is the agent's concern.

Every owned object MUST carry the agent name and UID as labels. Cross-namespace
owner references MUST NOT be used. Namespace cleanup MUST be guarded by an
agent finalizer and MUST affect only the deterministic agent namespace. PVCs,
Jobs, Services, Secrets, ConfigMaps, and other objects the agent creates inside
that namespace are part of the workspace and share its lifecycle.

## OPR-003.4: Namespace autonomy and ResourceQuota

This boundary follows Kubernetes
[ResourceQuota](https://kubernetes.io/docs/concepts/policy/resource-quotas/)
semantics: quota constrains aggregate consumption and object counts within one
namespace.

The runtime service account MUST authenticate through its projected Kubernetes
service-account token. A RoleBinding to the Kubernetes built-in `admin`
ClusterRole MUST give the agent broad read/write control over namespaced
resources, including workloads, storage, configuration, Secrets, service
accounts, Roles, and RoleBindings. That RoleBinding has effect only in the
agent namespace and MUST NOT grant access to Nodes, Namespaces, CRDs, or any
other namespace.

`admin` intentionally does not permit writes to the Namespace or ResourceQuota.
The operator MUST own and continuously reconcile both the quota and LimitRange;
the agent MUST be unable to weaken or delete those guardrails. Normal Kubernetes
RBAC privilege-escalation prevention remains in force when the agent creates
Roles or RoleBindings.

`spec.workspace.resourceQuota.hard` MUST be a non-empty map passed to the
ResourceQuota `spec.hard` field. It MUST bound aggregate CPU requests and
limits, memory requests and limits, requested persistent storage, PVC count,
and object counts for Pods, Jobs, Services, ConfigMaps, and Secrets. The API MAY
accept additional Kubernetes-supported quota keys.

`spec.workspace.limitRange.container` MUST define default CPU and memory
requests and limits. The controller MUST translate it into one container
LimitRange item. These defaults ensure Pods created autonomously by the agent
are admitted when compute quotas require requests or limits.

If a request would exceed quota, Kubernetes rejects it with `403 Forbidden`.
The agent MUST treat this as a bounded-capacity result: clean up completed work,
request a quota change, or report failure. It MUST NOT retry an unchanged
quota-violating request indefinitely.

## OPR-003.5: Network policy

`spec.networkPolicy.mode` MAY be `Unmanaged` or `Isolated`. The Agent setting
MUST override `Organization.spec.networkPolicy`; if neither resource declares a
mode, it MUST default to `Unmanaged`.

In `Unmanaged` mode, the operator MUST NOT create a general Agent network
policy. Cluster administrators MAY apply policies to the stable Agent Pod
labels. In `Isolated` mode, the operator MUST select the complete Agent Pod,
deny ingress and egress by default, and allow TCP and UDP DNS. Additional
NetworkPolicy resources MAY grant environment-specific access because
Kubernetes combines matching policies additively. The operator MUST continue
to reconcile its separately owned control-path policies, such as forge-gateway
A2A ingress.

Network policy applies to the complete Pod, including the browser sidecar. The
operator MUST NOT represent it as container-specific isolation.

## OPR-003.6: Credentials and configuration

`Agent.spec.credentials` references Secrets and ConfigMaps in the agent namespace
**by name only** and declares how each is exposed to the runtime. The operator
reports whether they exist (`CredentialsReady`) but always reconciles the
Deployment, leaving missing non-optional projections to standard Kubernetes
Pod status. Except for checking whether a referenced env projection defines one
of the four runtime-configuration migration keys below, it does not inspect
object contents; key-level contracts (for example the email channel adapter's
JMAP keys) belong to the composed agent, not here. This is the generic primitive
defined in [OPR-004](OPR-004-config-secrets.md) — see it for the full contract.

`Agent.spec.channels` MAY select a non-empty set of runtime channel IDs. The
operator MUST project a deterministic comma-separated value as
`OUTFITTER_CHANNELS`; when omitted, the runtime retains its own source-discovery
behavior.

`Agent.spec.catalogSync.enabled: true` MUST add a dedicated init container that
runs `outfitter sync` before user-supplied setup steps and the resident runtime.
The container MUST receive the rendered Outfitter settings, durable workspace,
and generic Agent credential projections. It MUST NOT receive the Agent's
Kubernetes API token. Omitting the option or setting it to false MUST preserve
the existing runtime behavior.

`Agent.spec.github` controls the resident GitHub notification source. The
operator projects `GITHUB_NOTIFY_ORGS`, `GITHUB_NOTIFY_POLL_MS`, and
`GITHUB_NOTIFY_FILTERS` into the runtime. `pollMs` defaults to `60000`; filters
default to `mention,assigned_issue,assigned_pr,review_requested,author`; and
`notifyOrgs` defaults to the forge owner in the accepted Organization's GitHub
catalog shorthand. Explicit Agent values override those defaults. During
migration, the same keys — including `OUTFITTER_CHANNELS` — in an env-exposed
credential Secret or ConfigMap win over the operator values, so existing runtime
configuration keeps working.

## OPR-003.7: Runtime execution and delegation

The controller runs the agent as a long-running Deployment and treats it as
**opaque**. It launches the equivalent of `outfitter run <agent-slug> --harness
pi` with the resolved catalog and the exposed credentials/config, and does not
model what the agent does next.

The runtime MUST be launched with a stable session identity equal to the Agent
name (`--session-id <agent-name>`) so the resident conversation resumes across
pod restarts. The session identity MUST NOT be the profile slug: two Agents MAY
share a profile but MUST NOT share a conversation. The session transcript on
the durable workspace volume is the one exception to the cache framing in
OPR-003.3 — it is the canonical record of the resident conversation. Channel
implementations and tools are supplied by the agent's Dotagents resources and
runtime image, not by the operator; the operator only selects enabled channels
through `spec.channels`.

A running agent MAY delegate work to **subagents that run as Kubernetes Jobs** in
its own namespace, using its `admin` rights and bounded by the shared
`ResourceQuota`. The delegation contract is defined in
[OPR-005](OPR-005-subagent-jobs.md). Systems of record for the agent's work are
external services (a mail server, GitHub/Forgejo, a Git remote); the durable
workspace volume is a cache.

Inputs the agent processes — message bodies, attachments, extracted text, fetched
pages — are **untrusted data**. They MUST NOT override the selected agent policy
or be treated as operator instructions. This rule holds at the agent layer
regardless of channel.

## OPR-003.8: Status

Status MUST include `observedGeneration`, `namespace`, the pinned Outfitter and
catalog-source revisions, resolved image digest, and the ResourceQuota hard/used
summary. It MUST include Kubernetes conditions:

- `Accepted`;
- `NamespaceReady`;
- `NetworkPolicyReady`;
- `WorkspaceReady` for the admin binding, ResourceQuota, and LimitRange;
- `CredentialsReady`;
- `OutfitterSettingsReady`;
- `WorkloadReady`; and
- `Ready`.

Messages MUST identify missing references or failed reconciliation stages while
redacting credential values. `OutfitterSettingsReady` means only that the
operator rendered the pinned sources and defaults; it MUST NOT claim that the
controller resolved a profile. Status reflects only the operator's primitives —
it says nothing about channel or tool progress, which is the agent's concern.

## Example

```yaml
apiVersion: aioutfitter.com/v1alpha1
kind: Agent
metadata:
  name: researcher
spec:
  # Optional; omit this to use the operator's default Outfitter image.
  image: ghcr.io/example/research-agent:v1
  memberships:
    - organization: ai-outfitter
      projects: []
  profile:
    agent: researcher
    harness: pi
  # Optional Agent override; omit to inherit the Organization or Unmanaged.
  networkPolicy:
    mode: Isolated
  credentials:
    # Names only. The operator exposes these but never inspects their contents.
    # Key-level contracts belong to the composed agent.
    - secret: researcher-email
      as: env
    - secret: researcher-model
      as: env
    - secret: researcher-ssh
      as: volume
    # Non-secret runtime config (e.g. channel routing) rides the same mechanism.
    - configMap: researcher-runtime
      as: env
  workspace:
    resourceQuota:
      hard:
        requests.cpu: "4"
        requests.memory: 8Gi
        limits.cpu: "8"
        limits.memory: 16Gi
        requests.storage: 50Gi
        persistentvolumeclaims: "8"
        count/pods: "20"
        count/jobs.batch: "50"
        count/services: "10"
        count/configmaps: "50"
        count/secrets: "20"
    limitRange:
      container:
        defaultRequest: {cpu: 100m, memory: 128Mi}
        default: {cpu: "1", memory: 1Gi}
```
