---
title: Organization
type: concept
status: draft
tags:
  - system/link-operator
  - topic/organization-model
  - topic/wiki
  - topic/agent-catalog
created: 2026-07-19
updated: 2026-07-19
---

# Organization

An organization is the ownership and policy boundary for one writable wiki,
shared commit-pinned agent catalogs, embedded [[Project|projects]], and agent
membership. In the Link Operator API it is represented by the cluster-scoped
`Organization` CRD.

The product-facing organization is also the [[Forge Owner|owner]] boundary. It
maps directly to a GitHub, Forgejo, or compatible forge owner namespace, where
repositories have the familiar `<owner>/<repository>` identity. Link Operator
then emphasizes `<owner>/<project>` as the user-facing work hierarchy because a
project can coordinate many repositories and environments rather than being
limited to one repository.

Agents reference organizations from their own membership list, allowing one
organization to have many agents and one [[Agent]] to belong to many
organizations. Organization-level membership does not implicitly grant access
to every project.

The organization owns catalog declarations, while the
[[Dotagents Catalog Composition|catalog resources]] determine the actual agent
definitions and skills. This model is defined by the
[[sources/2026-07-19-link-operator-requirements/source|M1 requirements]].

## Relationships

- Owns the wiki changed by [[Wiki Source Ingestion]].
- Maps to a [[Forge Owner]] namespace for repository resolution.
- Embeds [[Project|projects]] and their [[Project Environment|environments]].
- Is selected by the [[Email Paper Research Workflow]] for each request.
