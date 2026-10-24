# OPR-006: Agent Run (the launch surface)

[OPR-005](OPR-005-subagent-jobs.md) defines the *subagent Job* — the unit of
delegated work — and the *environment* — its reusable template. It does not define
the **surface that requests a launch**. Today the only way to start one is for the
in-namespace agent to create a Job directly with its `admin` rights; there is no
declarative, recordable request an external party can make, and no place the
operator can enforce run history and concurrency (both required by OPR-005.6).

This requirement adds that surface: a namespaced **`Run`** custom resource (the
launch request) that the operator **materializes** into an OPR-005 subagent Job.
The Job itself remains **not a CRD** (OPR-005); the `Run` is only the request and
its recorded outcome. A `Run` is the single surface for **both** producers OPR-005
already sanctions — *"a Job the agent, **or the operator on the agent's behalf**,
creates"* — so an agent's channel handler and an external control plane launch work
the same way.

This is deferred, milestone-gated work (subagent Jobs are a Non-Goal through M2). It
is a proposal for when OPR-005 is picked up, and it depends on
[OPR-002.4](OPR-002-projects.md)'s embedded `environments` being materialized.

## OPR-006.1: The Run is the launch request

A `Run` MUST be a namespaced custom resource created **in the invoking agent's own
namespace workspace** (`agent-<name>`). It names the work to launch either by
reference — the qualified environment identity
`<organization>/<project>/<environment>` (OPR-005.1) — or inline, carrying the same
fields an environment declares (OPR-005.2): a Dotagents agent slug, an optional
harness override, and a workload with an immutable container image, resource
requests/limits, and a timeout. A `Run` MUST resolve to exactly one environment
shape; inline fields and an `environmentRef` MUST NOT both be set.

A `Run` MUST NOT contain a `kind` discriminator (OPR-005.5). All `Run`s use the
same validation and materialization path.

## OPR-006.2: Typed inputs (trusted identifiers only)

A `Run` MAY carry `inputs`: a typed map of **trusted identifiers** the launched
agent needs to locate its work — for example a repository name, a Git ref or pull
request number, a run or task identifier, and a `callback` (a URL and an OPR-004
credential reference for reporting status back to the requester). Inputs MUST carry
identifiers only; untrusted content — issue and PR bodies, email text, fetched
pages — MUST NOT be embedded. The launched agent fetches that content itself under
its own identity (OPR-006.6). Secret values MUST never be embedded; credentials are
referenced via the [OPR-004](OPR-004-config-secrets.md) mechanism, exactly as an
environment's environment variables are (OPR-005.2).

## OPR-006.3: Materialization into a Job

The operator MUST reconcile a `Run` into exactly one OPR-005 subagent Job in the
same namespace, and MUST NOT create the Job in, or let it escape to, any other
namespace. The materialized Job MUST:

- run under the agent's `agent-runtime` service account, storage, quota, and
  credentials boundary (OPR-005.3/.4);
- carry the OPR-005.3 labels — organization, project, environment, agent, and
  parent run — plus a reference back to the owning `Run`, and a collision-resistant
  run suffix in its name;
- enforce the environment's timeout as the Job's active deadline (OPR-005.3); and
- project the `Run`'s `inputs` into the workload (as environment for identifiers,
  and via OPR-004 for the callback credential).

The `Run` MUST own the Job (owner reference) so deleting the `Run` deletes the Job.

## OPR-006.4: Launch lifecycle, history, and concurrency

The operator MUST record run history on the `Run`'s status (at least a phase, the
materialized Job reference, and start/completion times) and MUST enforce
concurrency limits within the namespace's aggregate `ResourceQuota` boundary
(OPR-005.6). A launch MUST NOT escape the agent namespace, modify the Namespace, or
modify the operator-owned `ResourceQuota` or `LimitRange`.

**Cancellation deletes the Job, never the namespace** (OPR-005.6). Deleting the
`Run` MUST delete its Job; a `Run` MAY also expose an explicit cancel that stops the
Job while retaining the `Run` record.

## OPR-006.5: Access — who may create a Run

The `Run` custom resource MUST be reachable by a namespace-`admin` principal so that
**both** producers use it without new grants:

- the **in-namespace agent** — the persistent agent `Deployment`, whose channel
  handlers (email, Signal, Telegram, and a GitHub-notification handler) create a
  `Run` from inside the pod using the mounted `agent-runtime` token; and
- an **external operator on the agent's behalf** — a control plane holding
  namespace access, which creates the same `Run` and supplies a `callback`.

To make the first work without widening the agent's rights, the operator MUST
aggregate the `Run` custom resource into the built-in `admin` ClusterRole (the
`rbac.authorization.k8s.io/aggregate-to-admin: "true"` label on its RBAC), so the
existing `agent-runtime` → `admin` RoleBinding (OPR-003.3) already permits creating
`Run`s in the namespace. No new RoleBinding is added to the workspace.

## OPR-006.6: Runtime contract — headless, identity-scoped work

The materialized Job's workload SHOULD invoke the runtime as a **headless, one-shot**
process — `outfitter run <agent> --harness <harness>` against the `Run`'s `inputs`
(OPR-003.2) — that does one unit of work and exits, rather than the persistent
main-loop the agent `Deployment` runs. The operator does not interpret the profile
or the inputs; it only materializes and bounds the Job.

Because the Job runs in the agent's own namespace under its credentials boundary
(OPR-005.4), work is performed under **the agent's own external identity** — e.g. a
GitHub-notification `Run` bound to a pull request acts as the agent's GitHub
account, via a credential the namespace exposes through OPR-004. External systems of
record remain external (OPR-003.6): the `Run` records the launch, not the work
product.
