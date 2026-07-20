# Quick start

Link Operator runs composable Dotagents agents on Kubernetes. An organization
owns its wiki, pinned agent catalogs, projects, and environments. An agent can
belong to multiple organizations and projects, but always runs in its own
namespace. That entire namespace is the agent's workspace: it has broad
namespaced administrator access while an operator-owned ResourceQuota bounds
its total consumption.

> **Implementation status:** this is the target user experience. The CRDs,
> controller, runtime image, and devenv tasks are specified but not implemented
> yet. Until they land, the commands below document the interface and will not
> complete successfully.

## Prerequisites

For the local path you need:

- Nix and devenv v2;
- a host capable of running the repository's microVM;
- `kubectl` (provided by the devenv shell once implemented);
- a writable Git wiki repository with Git LFS enabled;
- an IMAP/SMTP mailbox; the local environment supplies GreenMail; and
- credentials for the model selected by your Dotagents agent.

Remote Dotagents catalogs must be pinned to full commit SHAs. Review every
agent, skill, MCP server, plugin, and script in a catalog before trusting it.
M1 combines only disjoint resource identities; duplicate
`<resource-kind>/<slug>` entries across catalogs are rejected rather than
resolved by order.

## 1. Start the local cluster

From the repository root:

```sh
devenv shell
devenv tasks run cluster:up
devenv tasks run operator:install
```

The target `cluster:up` task starts a microVM containing single-node k3s,
GreenMail, a local image path, and the Link Operator. `operator:install` is
idempotent and waits until these resources are ready.

Confirm that the two CRDs are installed:

```sh
kubectl api-resources --api-group=link.aioutfitter.com
```

The output should contain `organizations` and `agents`. Projects and
environments are embedded data and do not appear as resources.

## 2. Configure an organization

Copy the sample before editing it:

```sh
cp config/samples/link_v1alpha1_organization.yaml /tmp/example-org.yaml
```

Replace the example wiki/catalog URLs and every placeholder revision. The wiki
repository must be writable by the agent's SSH identity. Every remote catalog
revision must be an immutable 40-character commit SHA.

Apply the organization and wait for its catalog composition to resolve:

```sh
kubectl apply -f /tmp/example-org.yaml
kubectl wait organization/example-org \
  --for=condition=Ready \
  --timeout=2m
kubectl get organization/example-org -o yaml
```

The example embeds one project with one common-shape environment. It
demonstrates the API, but M1 validates the entry without launching it.

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
ResourceQuota and LimitRange, and the runtime workload. The agent is not ready
yet: it should report `CredentialsReady=False` until you supply its Secrets.

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

The sample grants organization-level membership in `example-org`. It does not
grant access to every embedded project. See
[the multi-organization sample](../../config/samples/link_v1alpha1_agent_multi_org.yaml)
for explicit project memberships across two organizations.

## 4. Supply credentials

Use your cluster's secret manager in production. For local development, create
ignored files with mode `0600` and load them with `kubectl`; do not commit them
or put secret values directly in a custom resource.

`email.env` must contain:

```dotenv
address=researcher@link.test
imapHost=greenmail
imapPort=3143
imapUsername=researcher@link.test
imapPassword=REPLACE_ME
imapTLS=false
smtpHost=greenmail
smtpPort=3025
smtpUsername=researcher@link.test
smtpPassword=REPLACE_ME
smtpTLS=false
```

The cleartext TLS settings are permitted only inside the isolated GreenMail
demo. Use TLS for a real mailbox.

Create the email Secret:

```sh
kubectl -n agent-researcher create secret generic \
  researcher-email \
  --from-env-file=email.env
```

Create the model Secret with the environment variable expected by the selected
model provider:

```sh
kubectl -n agent-researcher create secret generic \
  researcher-model \
  --from-env-file=model.env
```

For private wiki or catalog repositories, create the SSH Secret and known-hosts
entry from local files:

```sh
kubectl -n agent-researcher create secret generic \
  researcher-ssh \
  --type=kubernetes.io/ssh-auth \
  --from-file=ssh-privatekey=./id_ed25519 \
  --from-file=known_hosts=./known_hosts
```

Remove the temporary credential files when they are no longer needed. Bootstrap
Secret volumes are mounted read-only. The agent is the administrator of its
namespace workspace and can manage its namespaced Secrets, but cannot access
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
Deployment are ready. It does not mean a research email has completed.

If readiness fails, inspect the agent's conditions first. They distinguish
invalid membership, unresolved catalogs, missing credentials, unresolved
profiles, and workload failures without exposing secret values.

## 6. Email a paper

Send one message to the configured agent address with exactly one PDF
attachment of at most 25 MiB. For M1, the email maps to the agent's
`defaultOrganization`.

The agent will:

1. receive and deduplicate the message by Message-ID;
2. clone the organization's wiki into storage it manages in its namespace
   workspace;
3. run the selected Dotagents agent through Outfitter and Pi;
4. preserve the PDF under `wiki/sources/` using Git LFS;
5. extract `content.md` with Docling and add a source note;
6. update concepts, the wiki index, and the append-only log;
7. list linked papers to explore next without downloading them;
8. create one local Git commit; and
9. reply in the original email thread with its summary and commit SHA.

M1 does not push the commit. Research traversal is limited to the emailed seed
paper; future recursive research has a hard maximum depth of five.

The fully scripted local scenario and its evidence requirements are in the
[M1 demo](../milestones/M1-email-paper-reserach/demo.md).

## Clean up

Delete the agent first so its finalizer can remove only its generated namespace:

```sh
kubectl delete agent/researcher
kubectl delete organization/example-org
devenv tasks run cluster:down
```

Normal shutdown preserves caches and evidence. Use a separately named
`reset`/`destroy` task only when you intend to remove cluster disks, model
caches, or the demo wiki fixture.

## Learn more

- [Organizations](../requirements/OPR-001-orgs.md)
- [Projects](../requirements/OPR-002-projects.md)
- [Agents](../requirements/OPR-003-agents.md)
- [Environments](../requirements/OPR-004-environments.md)
- [M1 implementation tasks](../milestones/M1-email-paper-reserach/task.md)
