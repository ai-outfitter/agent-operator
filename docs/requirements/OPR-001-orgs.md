# OPR-001: Organizations

Status: first-pass requirement; M1 obligations are identified explicitly. See
[architecture.md](../architecture.md) for how organizations fit the
primitives-vs-composition split.

An organization is the ownership and policy boundary for an owner's repositories
and a pinned Dotagents catalog. It is domain-agnostic: it does not model wikis,
mailboxes, or any other channel or tool. `Organization` and `Agent` are the only
top-level CRDs in this system.

## OPR-001.1: API identity and scope

`Organization` MUST be a cluster-scoped resource served as
`link.aioutfitter.com/v1alpha1`, kind `Organization`. Its Kubernetes object name
MUST be a DNS label and is the stable identifier referenced by agents.

Deletion MUST use a finalizer while operator-owned resources still require
cleanup. The controller MUST NOT delete an external repository or catalog.

## OPR-001.2: Repositories

`spec.repositories` MUST be a list of named Git repositories the organization
owns. Each entry MUST provide a clone URL and MAY specify a default branch and a
subdirectory. Names MUST be unique within the organization. The repositories are
generic: the operator attaches no meaning such as "wiki" to any of them.

An immutable commit SHA MAY be used as an initial revision, but a repository the
agent works in MUST remain writable so a run can create a new commit.

M1 supports one repository per organization — the demo's wiki, named `wiki`. Its
expected knowledge layout is governed by the agent's `wiki` skill, not by the CRD
schema. See the [M1 milestone](../milestones/M1-email-paper-reserach/task.md) for
how the researcher composition uses it.

## OPR-001.3: Dotagents catalogs

`spec.agentCatalogs` MUST be a list of named Git sources. A source MUST use
exactly one of these forms:

- a GitHub `owner/repository` shorthand;
- a cloneable Git URI; or
- a local path used only by development fixtures.

A remote source MUST include an immutable full commit SHA. A source MAY name a
payload subdirectory, including a colocated `.agents` directory. Standalone
`owner/.agents` and `owner/.agent` repositories have the Dotagents payload at
their root.

The MVP supports a **single catalog per organization** — its own pinned
`.agents` payload — so no cross-catalog composition is required. The resolved
source and its revision MUST be visible in status.

Multi-catalog resolution is deferred. When a second catalog is needed, the
controller MUST concatenate the disjoint resources from all catalogs into one
effective set, index each resource by `<resource-kind>/<slug>`, and reject
duplicates (including identical ones) by setting `CatalogsResolved=False` with
reason `DuplicateResourceSlug`, identifying the resource and source names without
credentials. Override, shadowing, and last-source-wins behavior MUST NOT be
introduced without an explicit precedence rule, a user-facing conflict
explanation, and tests that exercise replacement. See
[architecture.md](../architecture.md) deferred items.

## OPR-001.4: Embedded projects (deferred)

Projects grouping is **deferred** for the single-owner fleet; see
[OPR-002](OPR-002-projects.md). The MVP organization schema is
`repositories` + one catalog. When projects are reintroduced, `spec.projects`
will contain zero or more entries conforming to OPR-002, with unique names and no
separate CRD. The useful in-MVP seam — an agent launching subagent Jobs — lives
in [OPR-004](OPR-004-environments.md), not in projects.

## OPR-001.5: Status and conditions

`status.observedGeneration` MUST report the last reconciled generation.
`status.conditions` MUST use Kubernetes conditions and include:

- `Accepted`: the specification and internal references are valid;
- `CatalogsResolved`: every pinned catalog can be resolved and validated; and
- `Ready`: the organization is usable by agents.

A failed external fetch MUST set a condition with a stable reason and useful
message. It MUST NOT copy a URI containing credentials into status or events.

## M1 example

```yaml
apiVersion: link.aioutfitter.com/v1alpha1
kind: Organization
metadata:
  name: ai-outfitter
spec:
  displayName: AI Outfitter
  repositories:
    # A generic repository; the researcher composition treats "wiki" as its wiki.
    - name: wiki
      uri: ssh://git@example.test/ai-outfitter/wiki.git
      defaultBranch: main
  agentCatalogs:
    - name: link-operator-agents
      github: ncrmro/link-operator
      # Replace with the commit containing the reviewed .agents payload.
      revision: 0123456789abcdef0123456789abcdef01234567
      path: .agents
```

The single catalog's `.agents` payload vendors the `wiki` and `source-ingest`
skills and defines the `researcher` agent, so the MVP needs only one catalog.
