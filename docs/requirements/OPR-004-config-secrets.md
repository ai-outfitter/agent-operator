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
uses the same projection primitive. ConfigMaps are the default when configuration
has its own lifecycle. Configuration MAY instead be colocated in an env-exposed
Secret when it configures the same runtime integration as that Secret's
credentials and a separate per-agent ConfigMap would add no ownership boundary.

`OUTFITTER_CHANNELS` specifically remains an opaque runtime selector, not a typed
`Agent` field. A catalog MAY place it in the env-exposed Secret that already
carries the selected channels' credentials. The operator forwards the key through
Kubernetes `envFrom` without reading it. This keeps channel composition outside
the CRD while allowing catalogs to retire ConfigMaps created only for that one
selector.

## OPR-004.4: Status

The `CredentialsReady` condition MUST report which referenced objects are still
missing, by name only. It MUST NOT reference any key or value. Once every
referenced object exists, `CredentialsReady=True`. Workload reconciliation is
independent: `WorkloadReady` reflects Deployment availability, and aggregate
`Ready` remains false while either credentials or workload are not ready.
