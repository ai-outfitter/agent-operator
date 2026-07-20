# OPR-005: Credentials and configuration exposure

Status: first-pass requirement; this is a core MVP primitive. See
[architecture.md](../architecture.md).

Exposing secrets and configuration to an agent is a first-class operator
primitive, reused by every channel and tool an agent composes. The operator is a
generic delivery mechanism: it projects named Kubernetes Secrets and ConfigMaps
into the agent runtime and waits for them to exist. It never learns what they
contain.

## OPR-005.1: Reference shape

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

Keeping `credentials` a list preserves the forward-compatible shape: the MVP may
reference only a few objects, but no migration is needed to reference more.

## OPR-005.2: Existence-only contract

Referenced objects hold their own values; the operator creates the agent
namespace first and then waits with `CredentialsReady=False` until every
referenced Secret and ConfigMap **exists**. The controller MUST NOT:

- read, log, copy, or emit the contents of any referenced object;
- validate that a Secret contains particular keys (for example `imapHost`); or
- copy exposed values into catalog settings, Git working trees, status, events,
  or any agent output.

Key-level contracts are owned by the **composed agent**, not the operator. For
example, the email channel adapter defines which keys its Secret must contain;
that contract lives with the agent/demo (see the
[M1 milestone](../milestones/M1-email-paper-reserach/task.md)), not in this CRD.

Exposed volumes MUST be mounted read-only. Because the agent is the administrator
of its namespace workspace, it can read and manage these namespaced objects.
Cross-namespace references are forbidden.

## OPR-005.3: Configuration rides the same mechanism

Non-secret runtime configuration — channel routing, endpoints, feature flags —
uses the same primitive via ConfigMaps. This is how a value such as an email
channel's default organization reaches the agent without the operator modeling a
channel-specific field. Anything the agent needs to be told is a ConfigMap the
agent consumes; anything it must keep private is a Secret.

## OPR-005.4: Status

The `CredentialsReady` condition MUST report which referenced objects are still
missing, by name only. It MUST NOT reference any key or value. Once every
referenced object exists, `CredentialsReady=True` and the operator may proceed to
`WorkloadReady`.
