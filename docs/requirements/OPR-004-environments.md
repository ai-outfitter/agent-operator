# OPR-004: Environments

Status: first-pass interface; workload materialization is deferred beyond M1.

An environment is a reusable project execution template. It describes work an
agent may launch; it is neither a CRD nor a permanently shared namespace.

## OPR-004.1: Identity

An environment MUST be embedded in a project and have a DNS-label name unique
within that project. Its qualified identity is
`<organization>/<project>/<environment>`.

## OPR-004.2: Profile and workload template

An environment MUST select a Dotagents agent slug. It MAY override the harness
and declare allowed subagent slugs, but every selected resource MUST resolve
from the invoking organization's pinned catalogs.

Its workload template MUST declare an immutable container image, command or
entrypoint when different from the image default, resource requests/limits,
timeout, and workspace policy. Environment variables may contain literal
non-secret values or Secret key references; secret values MUST never be
embedded.

## OPR-004.3: Launch isolation

An agent may launch an environment only when its membership names the owning
project. Every environment and subagent workload MUST be a Job in the invoking
agent's own namespace workspace. Shared projects MUST NOT cause agents to share
a namespace, service account, storage, quota, or credentials.

Jobs MUST carry labels for organization, project, environment, agent, and
parent run. Names MUST include a collision-resistant run suffix. The operator
or agent MUST enforce the environment timeout and expose cancellation by
deleting the Job, not the agent namespace.

## OPR-004.4: Namespace workspace and credentials

The environment runs inside the agent's namespace workspace and consumes the
same aggregate ResourceQuota. It MAY create per-run PVCs, ConfigMaps, Services,
and other namespaced resources and MUST clean up ephemeral resources when the
run ends. Durable resources MUST be labeled so the agent can retain or garbage
collect them deliberately.

Credential mounts SHOULD be allow-listed by class in the environment template
and matched against Secret references on the invoking agent. A child Pod does
not receive a Secret merely because it exists in the namespace, although the
namespace-admin agent can explicitly grant or mount it.

## OPR-004.5: No implicit variants

The v1alpha1 environment shape MUST NOT contain a `kind` discriminator. Image,
profile, timeout, resources, retry policy, workspace behavior, and credential
classes MUST be explicit fields rather than inferred from names such as
`development`, `staging`, or `deployment`.

All environments use the same validation and eventual materialization path.
They MAY create RBAC and workload objects inside the agent namespace but cannot
escape it, modify the Namespace, or modify the operator-owned ResourceQuota.

A kind discriminator MAY be introduced in P2 only when at least two kinds have
different enforced reconciliation or admission behavior. Adding labels that
affect nothing but validation is not sufficient.

## OPR-004.6: M1 boundary

M1 MUST validate environment identity, image form, profile reference, timeout,
resource fields, credential references, and membership. It MUST reject a
`kind` field as unknown. It MUST NOT launch an environment merely because its
project or organization is reconciled.

The M1 email worker may launch its one research execution internally, but that
does not establish the public environment-launch API. Public launch requests,
run history, concurrency, kind-specific behavior, and materialization are P2
work. The namespace ResourceQuota is part of M1.
