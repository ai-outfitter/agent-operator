---
title: Project
type: concept
status: draft
tags:
  - system/agent-operator
  - topic/organization-model
  - topic/project
  - topic/repository
created: 2026-07-19
updated: 2026-07-19
---

# Project

A project is an [[Organization|organization-owned]] unit of work that groups
repositories and reusable [[Project Environment|environment templates]]. It is
embedded in `Organization.spec.projects`; it is not a Kubernetes custom
resource.

The project is more meaningful to a Agent Operator user than a single forge
repository: one `<owner>/<project>` can include many repositories, each resolved
through the [[Forge Owner]] as `<owner>/<repository>`. This keeps repository
identity compatible with GitHub and Forgejo while letting the product model a
larger body of work.

An [[Agent]] gains project access only when its membership explicitly names the
project and it has suitable repository credentials. An empty project list means
organization-level work only, not access to every project. Keeping membership
on the agent side avoids competing member lists in a many-to-many model.

M1 validates projects but does not materialize project workloads. The validation
boundary is documented by the
[[sources/2026-07-19-agent-operator-requirements/source|M1 requirements]]; the
forge-owner mapping was added in the
[[sources/2026-07-19-agent-operator-design-conversation/source|design conversation]].
