# OPR-004: Credentials and configuration exposure

See [architecture.md](../architecture.md) for the primitives-vs-composition split.

Exposing secrets and configuration to an agent is a first-class operator
primitive, reused by every channel and tool an agent composes. The operator is a
generic delivery mechanism: it projects named Kubernetes Secrets and ConfigMaps
into the agent runtime and waits for them to exist. It never learns what they
contain.

## OPR-004.1: Reference shape

`Agent.spec.credentials` MUST be a list. Each entry MUST reference exactly one
object in the agent namespace and declare how it is exposed:

```yaml
credentials:
  - secret: researcher-email      # a Secret in namespace agent-<name>
    as: env                       # env | volume
  - secret: researcher-model
    as: env
  - secret: researcher-ssh
    as: volume
  - configMap: researcher-runtime # non-secret runtime configuration
    as: env
```

- Exactly one of `secret` or `configMap` MUST be set per entry.
- `as: env` MUST project the object's keys into the runtime container as
  environment variables (`envFrom`).
- `as: volume` MUST mount the object read-only at a conventional path derived
  from its name; the runtime and the composed agent agree on that convention.

An agent references as many objects as its composition needs.

## OPR-004.2: Existence-only contract

Referenced objects hold their own values. The operator creates the agent
namespace and workload even when referenced objects are absent, while reporting
`CredentialsReady=False` until every referenced Secret and ConfigMap **exists**.
Non-optional native Kubernetes projections keep the Pod's containers from
starting until the objects exist. The controller MUST NOT:

- read, log, copy, or emit the contents of any referenced object;
- validate that a Secret contains particular keys (for example `JMAP_URL`); or
- copy exposed values into catalog settings, Git working trees, status, events,
  or any agent output.

Key-level contracts are owned by the **composed agent**, not the operator. For
example, an email channel adapter defines which keys its Secret must contain; that
contract lives with the agent, not in this CRD.

Exposed volumes MUST be mounted read-only. Because the agent is the administrator
of its namespace workspace, it can read and manage these namespaced objects.
Cross-namespace references are forbidden.

## OPR-004.3: Configuration rides the same mechanism

Non-secret runtime configuration — channel routing, endpoints, feature flags —
uses the same primitive via ConfigMaps. This is how a value such as an email
channel's default organization reaches the agent without the operator modeling a
channel-specific field. Anything the agent needs to be told is a ConfigMap the
agent consumes; anything it must keep private is a Secret.

Typed, cross-runtime controls explicitly defined by the Agent API are the
exception. `Agent.spec.channels` projects `OUTFITTER_CHANNELS` according to
OPR-003.5; catalogs do not need a ConfigMap solely for that selector.

## OPR-004.4: Status

The `CredentialsReady` condition MUST report which referenced objects are still
missing, by name only. It MUST NOT reference any key or value. Once every
referenced object exists, `CredentialsReady=True`. Workload reconciliation is
independent: `WorkloadReady` reflects Deployment availability, and aggregate
`Ready` remains false while either credentials or workload are not ready.
