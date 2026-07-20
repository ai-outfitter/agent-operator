# OPR-004: Subagent execution (Jobs)

Status: the **delegation seam** (an agent launching subagent Jobs in its own
namespace) is the in-MVP concept; the public, operator-driven environment-launch
API and its project-scoped templates are deferred. See
[architecture.md](../architecture.md).

The durable model is delegation: a running agent stays responsive by pushing
heavy or long work to **subagents that run as Kubernetes Jobs in its own
namespace**, using its `admin` rights and bounded by the shared `ResourceQuota`.
A subagent is not a CRD and not a shared namespace — it is a Job the agent (or,
later, the operator on the agent's behalf) creates and tears down.

M1 does not exercise this: the researcher ingests inline. The seam is specified
here so the composition model is clear and forward-compatible.

## OPR-004.1: The Job is the unit of delegated work

A subagent MUST be a Job in the invoking agent's own namespace workspace. It
selects a Dotagents agent slug resolvable from the organization's pinned catalog
and MAY declare allowed subagent slugs. The **public API** for describing
reusable execution templates — including project-embedded environments and their
qualified identity `<organization>/<project>/<environment>` — is **deferred**;
until then, an agent creates subagent Jobs directly.

## OPR-004.2: Job shape

A subagent Job selects a Dotagents agent slug and MAY override the harness and
declare allowed subagent slugs, but every selected resource MUST resolve from the
organization's pinned catalog. Its workload MUST use an immutable container image
and declare resource requests/limits and a timeout. Environment variables may
carry literal non-secret values or Secret/ConfigMap references via the OPR-005
mechanism; secret values MUST never be embedded.

A reusable, operator-validated *template* for these fields (the "environment"
object) is **deferred**; see OPR-004.6.

## OPR-004.3: Isolation and labels

Every subagent Job MUST run in the invoking agent's own namespace workspace and
share its service account, storage, quota, and credentials boundary. Nothing
about delegation causes agents to share a namespace or escape one.

Jobs MUST carry labels for organization, agent, and parent run (and, once
projects return, project and environment). Names MUST include a
collision-resistant run suffix. Timeouts MUST be enforced and cancellation MUST
be expressed by deleting the Job, never the agent namespace.

## OPR-004.4: Namespace workspace and credentials

A subagent Job runs inside the agent's namespace workspace and consumes the same
aggregate ResourceQuota. It MAY create per-run PVCs, ConfigMaps, Services, and
other namespaced resources and MUST clean up ephemeral resources when the run
ends. Durable resources MUST be labeled so the agent can retain or garbage
collect them deliberately.

A child Pod does not receive a Secret merely because it exists in the namespace;
the namespace-admin agent explicitly grants or mounts it via OPR-005. Subagent
Jobs MAY create RBAC and workload objects inside the agent namespace but cannot
escape it, modify the Namespace, or modify the operator-owned ResourceQuota.

## OPR-004.5: No implicit variants (when the template returns)

When the reusable template is introduced, it MUST NOT contain a `kind`
discriminator. Image, profile, timeout, resources, retry policy, workspace
behavior, and credential classes MUST be explicit fields rather than inferred
from names such as `development`, `staging`, or `deployment`. A kind
discriminator MAY be added only when at least two kinds have genuinely different
enforced reconciliation or admission behavior; labels that affect only validation
are not sufficient.

## OPR-004.6: Deferred — the public launch API

The operator-driven, project-scoped environment-launch API — reusable templates,
public launch requests, run history, concurrency limits, kind-specific behavior,
and materialization — is deferred. Reintroduce it when an agent needs
operator-managed, project-scoped work rather than self-launched subagent Jobs.
The namespace `ResourceQuota` that bounds all in-namespace Jobs is part of the
MVP.
