# OPR-002: Projects

> **Deferred (post-MVP).** Projects grouping is not part of the single-owner-fleet
> MVP. This document is retained as a forward design note, not an MVP obligation.
> The useful in-MVP seam — an agent launching subagent Jobs — lives in
> [OPR-004](OPR-004-environments.md), not in projects. Reintroduce projects when an
> agent needs operator-managed, project-scoped work rather than self-launched
> subagent Jobs. See [architecture.md](../architecture.md).

Status: deferred design note.

A project is an organization-owned unit of work. It groups repositories and
reusable execution environments without introducing another top-level CRD.

## OPR-002.1: Ownership and identity

A project MUST be an entry in `Organization.spec.projects`. Its `name` MUST be
a DNS label and MUST be unique within that organization. The stable qualified
identity is `<organization-name>/<project-name>`.

Moving a project between organizations is a delete-and-create operation because
organization ownership affects membership, repository policy, and catalog
composition.

## OPR-002.2: Repositories

`repositories` MAY contain named Git repositories used by the project. Names
MUST be unique within the project. Each repository MUST provide a clone URL and
MAY provide a default branch, a working subdirectory, and the name of an SSH
Secret reference declared by the invoking agent.

Repository credentials MUST NOT appear in an organization or agent
specification. A repository declaration does not grant an agent access; access
requires both project membership and suitable credentials.

## OPR-002.3: Agent membership

Projects MUST NOT contain an independent member list. An agent opts into
projects through its organization membership as defined by
[OPR-003](OPR-003-agents.md). This keeps the many-to-many relation on one side
and avoids two conflicting sources of truth.

An empty project list on a membership grants organization-level work only. It
MUST NOT mean every project. A named project that does not exist MUST make the
agent membership invalid.

## OPR-002.4: Embedded environments

`environments` MAY contain entries conforming to
[OPR-004](OPR-004-environments.md). Environment names MUST be unique within the
project. Every M1 environment uses the same shape and has no kind discriminator.

## OPR-002.5: MVP boundary

Projects grouping is deferred, so the MVP carries no `spec.projects` schema. When
reintroduced, validation MUST cover project, repository, environment, and agent
reference names, and MUST NOT create project namespaces, Deployments, or Jobs
solely because a project exists. Organization-level agents (an empty membership
`projects` list) work without any project.

## Example embedded project

```yaml
spec:
  projects:
    - name: link-operator
      displayName: Link Operator
      repositories:
        - name: source
          uri: ssh://git@example.test/ai-outfitter/link-operator.git
          defaultBranch: main
      environments:
        - name: default
          profile:
            agent: engineer
          workload:
            image: registry.example.test/link-workspace@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
            timeout: 2h
```
