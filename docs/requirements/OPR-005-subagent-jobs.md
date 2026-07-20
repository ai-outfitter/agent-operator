# OPR-005: Subagent execution (Jobs)

A running agent stays responsive by pushing heavy or long work to **subagents that
run as Kubernetes Jobs in its own namespace**, using its `admin` rights and
bounded by the shared `ResourceQuota`. A subagent is not a CRD and not a shared
namespace — it is a Job the agent, or the operator on the agent's behalf, creates
and tears down. An **environment** is the reusable, named template for such work,
embedded in a project. See [architecture.md](../architecture.md).

## OPR-005.1: The Job is the unit of delegated work

A subagent MUST be a Job in the invoking agent's own namespace workspace. It
selects a Dotagents agent slug resolvable from the organization's pinned catalogs
and MAY declare allowed subagent slugs. An agent MAY create subagent Jobs
directly, or launch a project **environment** — a reusable template with the
qualified identity `<organization>/<project>/<environment>` — that materializes
into the same kind of Job.

## OPR-005.2: Environment template and Job shape

An environment selects a Dotagents agent slug, MAY override the harness, and MAY
declare allowed subagent slugs; every selected resource MUST resolve from the
invoking organization's pinned catalogs. Its workload MUST declare an immutable
container image, resource requests/limits, and a timeout. Environment variables
MAY carry literal non-secret values or Secret/ConfigMap references via the
[OPR-004](OPR-004-config-secrets.md) mechanism; secret values MUST never be
embedded.

## OPR-005.3: Launch isolation and labels

An agent may launch an environment only when its membership names the owning
project. Every environment and ad-hoc subagent Job MUST run in the invoking
agent's own namespace workspace and share its service account, storage, quota, and
credentials boundary. Shared projects MUST NOT cause agents to share a namespace,
service account, storage, quota, or credentials.

Jobs MUST carry labels for organization, project, environment, agent, and parent
run. Names MUST include a collision-resistant run suffix. Timeouts MUST be
enforced and cancellation MUST be expressed by deleting the Job, never the agent
namespace.

## OPR-005.4: Namespace workspace and credentials

A subagent Job runs inside the agent's namespace workspace and consumes the same
aggregate ResourceQuota. It MAY create per-run PVCs, ConfigMaps, Services, and
other namespaced resources and MUST clean up ephemeral resources when the run
ends. Durable resources MUST be labeled so the agent can retain or garbage collect
them deliberately.

A child Pod does not receive a Secret merely because it exists in the namespace;
the namespace-admin agent explicitly grants or mounts it via OPR-004. Subagent
Jobs MAY create RBAC and workload objects inside the agent namespace but cannot
escape it, modify the Namespace, or modify the operator-owned ResourceQuota.

## OPR-005.5: No implicit variants

An environment MUST NOT contain a `kind` discriminator. Image, profile, timeout,
resources, retry policy, workspace behavior, and credential classes MUST be
explicit fields rather than inferred from names such as `development`, `staging`,
or `deployment`. All environments use the same validation and materialization
path. A kind discriminator MAY be added only when at least two kinds have
genuinely different enforced reconciliation or admission behavior; labels that
affect only validation are not sufficient.

## OPR-005.6: Launch lifecycle

Public launch requests MUST record run history and enforce concurrency limits
within the namespace quota boundary. A launch MUST NOT escape the agent namespace,
modify the Namespace, or modify the operator-owned ResourceQuota. Cancellation
deletes the Job, not the namespace.
