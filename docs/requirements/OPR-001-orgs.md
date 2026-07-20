# OPR-001: Organizations

An organization is the outermost ownership and policy boundary, mirroring a forge
organization (GitHub, Forgejo, Gitea): it is where an agent's access begins. An
organization owns repositories, projects, and Dotagents catalogs. It is
domain-agnostic — it does not model wikis, mailboxes, or any other channel or
tool. `Organization` and `Agent` are the only top-level CRDs in this system. See
[architecture.md](../architecture.md) for the primitives-vs-composition split.

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

The operator attaches no meaning to any repository. What a repository is *for* —
a wiki, source code, a knowledge base — is decided by the agent composition that
uses it, never by the CRD schema.

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

Resolution MUST concatenate the disjoint resources from all catalogs into one
effective set and index each resource by `<resource-kind>/<slug>`. Duplicate
identities MUST be rejected — with `CatalogsResolved=False`, reason
`DuplicateResourceSlug`, and the resource and source names (no credentials) —
rather than resolved by order. Override, shadowing, and last-source-wins behavior
MUST NOT be introduced without an explicit precedence rule, a user-facing
conflict explanation, and tests that exercise replacement. The resolved sources
and their revisions MUST be visible in status.

## OPR-001.4: Embedded projects

`spec.projects` MUST contain zero or more projects conforming to
[OPR-002](OPR-002-projects.md). Project names MUST be unique within an
organization. Projects and environments are embedded data, not separate CRDs. An
organization's projects and their repositories are how an agent discovers what it
has access to within that organization.

## OPR-001.5: Status and conditions

`status.observedGeneration` MUST report the last reconciled generation.
`status.conditions` MUST use Kubernetes conditions and include:

- `Accepted`: the specification and internal references are valid;
- `CatalogsResolved`: every pinned catalog can be resolved and validated; and
- `Ready`: the organization is usable by agents.

A failed external fetch MUST set a condition with a stable reason and useful
message. It MUST NOT copy a URI containing credentials into status or events.

## Example

```yaml
apiVersion: link.aioutfitter.com/v1alpha1
kind: Organization
metadata:
  name: ai-outfitter
spec:
  displayName: AI Outfitter
  repositories:
    # A generic repository; a composition decides what it is for.
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
