# OPR-001: Organizations

Status: first-pass requirement; M1 obligations are identified explicitly.

An organization is the ownership and policy boundary for wikis, projects, and
shared Dotagents catalogs. `Organization` and `Agent` are the only top-level
CRDs in this system.

## OPR-001.1: API identity and scope

`Organization` MUST be a cluster-scoped resource served as
`link.aioutfitter.com/v1alpha1`, kind `Organization`. Its Kubernetes object name
MUST be a DNS label and is the stable identifier referenced by agents.

Deletion MUST use a finalizer while operator-owned resources still require
cleanup. The controller MUST NOT delete an external wiki or catalog repository.

## OPR-001.2: Wiki repository

`spec.wiki.repository` MUST identify one Git repository by clone URL. It MAY
also specify a default branch and subdirectory. An immutable commit SHA MAY be
used as the initial revision, but the agent workspace MUST remain writable so
the research run can create a new commit.

M1 MUST support one wiki repository per organization. Its expected knowledge
layout is governed by the selected `wiki` skill rather than duplicated in the
CRD schema.

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

M1 MUST concatenate the resources resolved from all catalogs into one effective
set. Catalog declaration order has no precedence meaning. Before invoking
Outfitter, the controller MUST index each resource by `<resource-kind>/<slug>`
and reject duplicates, including duplicates whose contents are identical. On a
collision it MUST set `CatalogsResolved=False` with reason
`DuplicateResourceSlug` and identify the resource and source names without
including credentials.

M1 MUST NOT implement override, shadowing, or last-source-wins behavior. Such
composition MAY be introduced after M1 only with an explicit precedence rule,
a user-facing conflict explanation, and tests that exercise replacement. The
resolved source list and revisions MUST be visible in status.

## OPR-001.4: Embedded projects

`spec.projects` MUST contain zero or more projects conforming to
[OPR-002](OPR-002-projects.md). Project names MUST be unique within an
organization. Projects and environments MUST NOT be installed as CRDs.

M1 MUST validate and preserve embedded project data, but it does not have to
launch project environments.

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
  wiki:
    repository:
      uri: ssh://git@example.test/ai-outfitter/wiki.git
      defaultBranch: main
  agentCatalogs:
    - name: link-operator-agents
      github: ncrmro/link-operator
      # Replace with the commit containing the reviewed .agents payload.
      revision: 0123456789abcdef0123456789abcdef01234567
      path: .agents
  projects: []
```

The repository's `.agents` payload vendors the `wiki` and `source-ingest`
skills and defines the `researcher` agent, so M1 needs only one catalog.
