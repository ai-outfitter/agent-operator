# OPR-003: Agents

Status: first-pass requirement; namespace isolation and email research are M1.

An `Agent` is a cluster-deployed worker and membership identity. Its selected
Dotagents agent definition is its composable Outfitter profile; there is no
separate profile resource.

## OPR-003.1: API identity and memberships

`Agent` MUST be a cluster-scoped resource served as
`link.aioutfitter.com/v1alpha1`, kind `Agent`. Its name MUST be a DNS label no
longer than 57 characters so its namespace can be `agent-<name>`.

`spec.memberships` MUST contain one or more unique organization references. A
membership MAY list project names from that organization. This is a
many-to-many relationship: one agent may belong to many organizations and
projects, and an organization or project may be referenced by many agents.

An empty `projects` list means organization-level access only. Every referenced
organization and project MUST exist before `Accepted=True`.

## OPR-003.2: Dotagents runtime

`spec.profile.agent` MUST select an agent slug resolved from the organization's
commit-pinned catalogs. `spec.profile.harness` MUST default to `pi` for M1. The
runtime MUST invoke the equivalent of:

```text
outfitter run <agent-slug> --harness pi
```

The agent definition supplies identity, skills, subagents, model, thinking
level, and tool policy according to the pinned Dotagents revision. The operator
MUST NOT copy those fields into the `Agent` CRD.

The M1 image MUST be reproducibly built from Outfitter commit
`c44205ef35265c893ad9f088772c35c71753bfb7`. A Deployment MUST ultimately use
an immutable image digest; a mutable development tag may be accepted only until
the local image is loaded and its digest recorded in status.

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
- one long-running agent Deployment; and
- one ConfigMap for bounded mailbox processing state.

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

## OPR-003.5: Credentials

Secret values MUST live in ordinary Kubernetes Secrets in the generated agent
namespace. `Agent.spec.credentials` contains names only. The operator creates
the namespace first and then waits with `CredentialsReady=False` until required
Secrets exist and contain their required keys.

The M1 email Secret MUST contain:

| Key | Meaning |
| --- | --- |
| `address` | Mailbox and reply From address |
| `imapHost`, `imapPort` | Incoming server endpoint |
| `imapUsername`, `imapPassword` | Incoming authentication |
| `imapTLS` | `true` for TLS, `false` only in the isolated demo |
| `smtpHost`, `smtpPort` | Submission server endpoint |
| `smtpUsername`, `smtpPassword` | Submission authentication |
| `smtpTLS` | `true` for TLS, `false` only in the isolated demo |

A referenced SSH Secret MUST use `kubernetes.io/ssh-auth`, with optional
`known_hosts`. Model-provider Secrets are mounted as explicitly selected
environment variables; the controller MUST not log their keys or values.
Bootstrap Secret volumes MUST be mounted read-only and MUST not be copied into
catalog settings, Git working trees, status, events, or email replies. Because
the agent is the administrator of its namespace workspace, it can read and
manage namespaced Secrets; cross-namespace Secret access remains forbidden.

## OPR-003.6: Email processing and idempotency

M1 MUST poll the configured IMAP mailbox and process one message at a time. A
valid request has exactly one `application/pdf` attachment no larger than 25
MiB and maps unambiguously to one organization membership. M1 may use
`spec.email.defaultOrganization` because its demo agent has one active research
organization.

Mailbox state MUST be keyed by the RFC Message-ID, with a content digest
fallback when Message-ID is absent. The state machine is `received`, `running`,
`committed`, `replied`, or `failed`. It MUST persist the source digest and local
commit SHA so a restart after committing cannot create a duplicate ingest.
IMAP mail MUST be marked complete only after the reply is accepted by SMTP.

The reply MUST set `In-Reply-To` and `References`, retain a recognizable
subject, and report the organization, source title, summary, local commit SHA,
changed wiki paths, linked-paper candidates, and any warnings. A permanent
validation failure MUST receive one failure reply; a retryable infrastructure
failure MUST remain retryable without sending repeated replies.

Email bodies, attachments, extracted text, and linked pages are untrusted data.
They MUST NOT override the selected agent policy or be treated as operator
instructions.

## OPR-003.7: M1 research result

The selected agent MUST compose the pinned `wiki` and `source-ingest` skills.
For the seed PDF it MUST:

1. add the untouched PDF under a dated `wiki/sources/<source>/` directory and
   track it with Git LFS;
2. generate and verify `content.md` with Docling;
3. create `source.md` with verified provenance and links;
4. update or create durable concepts without duplicating existing notes;
5. update `wiki/index.md` and append to `wiki/log.md`;
6. record verified linked-paper candidates as depth-one follow-up work without
   downloading them; and
7. create exactly one local Git commit and perform no push.

Recursive ingestion is deferred. Future traversal MUST have a hard maximum
depth of five.

## OPR-003.8: Status

Status MUST include `observedGeneration`, `namespace`, resolved Outfitter and
catalog revisions, resolved image digest, and the ResourceQuota hard/used
summary. It MUST include Kubernetes conditions:

- `Accepted`;
- `NamespaceReady`;
- `WorkspaceReady` for the admin binding, ResourceQuota, and LimitRange;
- `CredentialsReady`;
- `ProfileResolved`;
- `WorkloadReady`; and
- `Ready`.

Messages MUST identify missing references or failed reconciliation stages while
redacting credentials and email content.

## M1 example

```yaml
apiVersion: link.aioutfitter.com/v1alpha1
kind: Agent
metadata:
  name: researcher
spec:
  memberships:
    - organization: ai-outfitter
      projects: []
    - organization: example-lab
      projects: [literature-review]
  profile:
    agent: researcher
    harness: pi
  credentials:
    emailSecret: researcher-email
    modelSecret: researcher-model
    sshSecret: researcher-ssh
  email:
    defaultOrganization: ai-outfitter
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
