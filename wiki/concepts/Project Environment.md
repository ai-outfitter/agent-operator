---
title: Project Environment
type: concept
status: draft
tags:
  - system/link-operator
  - topic/project
  - topic/environment-template
  - environment/agent-namespace
created: 2026-07-19
updated: 2026-07-19
---

# Project Environment

A project environment is a reusable execution template embedded in a
[[Project]]. It selects a Dotagents profile and describes an immutable workload
image, resources, timeout, workspace behavior, and allow-listed credential
classes.

The first API shape has no development/deployment `kind`. Every environment
uses the same validation and eventual materialization path. A discriminator is
reserved for a later phase in which different kinds actually select different
enforced reconciliation or admission behavior.

Future launches create Jobs inside the invoking [[Agent Namespace Workspace]],
never a shared project namespace. M1 validates the embedded shape but does not
launch it. This simplification originated in the
[[sources/2026-07-19-link-operator-design-conversation/source|design conversation]]
and is formalized by the
[[sources/2026-07-19-link-operator-requirements/source|requirements]].
