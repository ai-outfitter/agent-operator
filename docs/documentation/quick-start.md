# Quick start

Link Operator runs composable Dotagents agents on Kubernetes. It provides
**primitives** — a per-agent namespace workspace, generic secret/config exposure,
catalog resolution, and running the agent — and treats what the agent *does* as
composition. Channels (email, and later GitHub notifications or Signal) and tools
(a wiki, source ingestion) are supplied by the agent's Dotagents resources, not by
the operator. See [architecture.md](../architecture.md).

An organization owns generic Git repositories and one pinned agent catalog. An
agent runs in its own namespace; that entire namespace is the agent's workspace,
with a durable volume and broad namespaced administrator access, while an
operator-owned ResourceQuota bounds its total consumption. This guide stands up
the primitives with one example agent; what that agent *does* is a composition —
see the [use cases](usecases.researcher-wiki-maintainer.md).

> **Implementation status:** this is the target user experience. The CRDs,
> controller, and runtime image are specified but not implemented yet. Until they
> land, the commands below document the interface and will not complete
> successfully.

## Prerequisites

To follow this guide you need:

- a Kubernetes cluster and `kubectl` configured for it;
- the Link Operator installed on that cluster (see step 1); and
- credentials for the model selected by your Dotagents agent.

To stand up a local cluster with the operator for evaluation or development, see
[CONTRIBUTING.md](../../CONTRIBUTING.md).

A composition brings its own inputs on top — for example the
[researcher wiki maintainer](usecases.researcher-wiki-maintainer.md) needs a Git
wiki repository and a mailbox. These are composition inputs, not operator
requirements; a different composition (say a GitHub PR watcher) would need
different inputs.

The pinned Dotagents catalog must use a full commit SHA. Review every agent,
skill, MCP server, plugin, and script in it before trusting it. The operator
currently resolves one catalog per organization.

## 1. Install the operator

Install the Link Operator into your cluster. A Helm chart is planned; it will
install the controller and the two CRDs. (For a local cluster with the operator
preinstalled, see [CONTRIBUTING.md](../../CONTRIBUTING.md).)

Confirm that the two CRDs are installed:

```sh
kubectl api-resources --api-group=link.aioutfitter.com
```

The output should contain `organizations` and `agents`, the only two CRDs.

## 2. Configure an organization

Copy the sample before editing it:

```sh
cp config/samples/link_v1alpha1_organization.yaml /tmp/example-org.yaml
```

Replace the example repository/catalog URLs and every placeholder revision. A
repository the agent commits to (the demo's `wiki`) must be writable by the
agent's identity. The catalog revision must be an immutable 40-character commit
SHA.

Apply the organization and wait for its catalog composition to resolve:

```sh
kubectl apply -f /tmp/example-org.yaml
kubectl wait organization/example-org \
  --for=condition=Ready \
  --timeout=2m
kubectl get organization/example-org -o yaml
```

The organization owns generic repositories and one pinned catalog. Projects are
not used in this guide.

## 3. Create an agent namespace

Review [the basic agent sample](../../config/samples/link_v1alpha1_agent.yaml),
then apply it:

```sh
kubectl apply -f config/samples/link_v1alpha1_agent.yaml
kubectl wait agent/researcher \
  --for=condition=NamespaceReady \
  --timeout=1m
```

The controller creates `agent-researcher`, its service account, a
namespaced binding to the built-in `admin` ClusterRole, an operator-owned
ResourceQuota and LimitRange, a durable workspace volume, and the runtime
workload. The agent is not ready yet: it should report `CredentialsReady=False`
until you supply its Secrets.

Inspect the workspace boundary and budget:

```sh
kubectl -n agent-researcher describe resourcequota agent-workspace
kubectl -n agent-researcher get limitrange agent-workspace-defaults -o yaml
kubectl -n agent-researcher get rolebinding
```

ResourceQuota limits aggregate namespace consumption, including compute,
requested persistent storage, and object counts. The LimitRange supplies
default CPU and memory values because compute quotas can reject Pods that omit
requests or limits. See the Kubernetes
[ResourceQuota documentation](https://kubernetes.io/docs/concepts/policy/resource-quotas/).

The sample grants organization-level membership in `example-org`. `memberships`
is a list — an agent may belong to many organizations — though this guide uses one
entry. See
[the multi-organization sample](../../config/samples/link_v1alpha1_agent_multi_org.yaml)
for the multi-membership shape.

## 4. Supply credentials

`Agent.spec.credentials` references Secrets and ConfigMaps **by name** and
declares how each is exposed to the runtime (`as: env` or `as: volume`). The
operator waits for them to exist and projects them in; it never inspects their
contents (see [OPR-004](../requirements/OPR-004-config-secrets.md)). The keys
inside each object are a contract of the *composed agent* — below, the email
channel adapter's contract.

Use your cluster's secret manager in production. For local development, create the
referenced Secrets/ConfigMaps in the `agent-<name>` namespace with `kubectl`; do
not commit secret values or put them directly in a custom resource. A generic
example:

```sh
kubectl -n agent-researcher create secret generic \
  researcher-model \
  --from-env-file=model.env
```

*Which* Secrets and ConfigMaps an agent needs, and the keys inside them, depend on
its composition. For a complete concrete set — an email mailbox Secret, a model
Secret, and an SSH Secret for the wiki — see the
[researcher wiki maintainer use case](usecases.researcher-wiki-maintainer.md).

Exposed Secret volumes are mounted read-only. The agent is the administrator of
its namespace workspace and can manage its namespaced Secrets, but cannot access
Secrets in another namespace.

## 5. Wait for the agent

```sh
kubectl wait agent/researcher \
  --for=condition=Ready \
  --timeout=5m
kubectl get agent/researcher -o yaml
kubectl -n agent-researcher get all,pvc,configmap,secret
kubectl -n agent-researcher describe resourcequota agent-workspace
```

`Ready=True` means the organization membership, pinned catalogs, credentials,
Dotagents profile, namespace workspace and quota guardrails, and runtime
Deployment are ready. It does not mean the agent has done any work yet.

If readiness fails, inspect the agent's conditions first. They distinguish
invalid membership, unresolved catalogs, missing credentials, unresolved
profiles, and workload failures without exposing secret values.

## 6. Give the agent work

How a ready agent receives and does work is its composition's concern — its
channel and tools, not the operator. Walk through a concrete end-to-end example in
the [researcher wiki maintainer use case](usecases.researcher-wiki-maintainer.md):
email a PDF to the agent and get a threaded reply with a source-traceable wiki
commit.

## Clean up

Delete the agent first so its finalizer can remove only its generated namespace,
then the organization:

```sh
kubectl delete agent/researcher
kubectl delete organization/example-org
```

Tearing down a local development cluster is covered in
[CONTRIBUTING.md](../../CONTRIBUTING.md).

## Learn more

- [Use case: researcher wiki maintainer](usecases.researcher-wiki-maintainer.md)
- [Architecture](../architecture.md)
- [Organizations](../requirements/OPR-001-orgs.md)
- [Agents](../requirements/OPR-003-agents.md)
- [Credentials and configuration exposure](../requirements/OPR-004-config-secrets.md)
- [Subagent execution (Jobs)](../requirements/OPR-005-subagent-jobs.md)
- [Projects](../requirements/OPR-002-projects.md)
