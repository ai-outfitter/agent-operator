# Shared voice preview

This opt-in deployment runs the voice runtime from webapp PR #492 in nonproduction.
It adds no operator controller, CRD, or automatic resident discovery. The resident
operator and resident Deployments remain unchanged. One worker pool and one media
endpoint serve all explicitly configured targets.

## Layout

- `nonprod`: one broker, one worker, SQLite PVC, ingress, service accounts and
  network policy, all in `voice-preview`. Not applied by any existing overlay.
- `resident-access.yaml`: separately applied policy admitting only the preview
  broker to `agent-unsupervised-luce`, `agent-unsupervised-nonprod-bot`, and
  `agent-unsupervised-vega` on port 8788. Delete these policies during rollback.
- `livekit-internal`: optional cluster-only media server for smoke tests. It has
  no public ingress or media endpoint and cannot establish a public browser call.
  Omit it when using an existing reachable LiveKit installation. There is no
  second worker pool or LiveKit installation per resident.

## Prerequisites

Use the explicit `webapp-nonprod` context. Confirm its AWS account is
`216577824627`, the existing NGINX ingress is reachable, and the encrypted CSI
storage class and cert-manager ClusterIssuer exist. Public audio requires a
separately verified media endpoint. Private EKS nodes plus HTTPS ingress alone
are not sufficient. No hosted account or public media load balancer is created
by these manifests.

Create Secrets out of band, never in Kustomize or git:

| Secret | Keys | Consumers |
| --- | --- | --- |
| `voice-media` | `url`, `publicUrl`, `apiKey`, `apiSecret` | Broker and worker |
| `voice-broker` | `serviceToken` (32+ characters), `targets.json`; gateway mode adds distinct `authorityToken` | Broker; only `authorityToken` also goes to isolated gateway |
| `voice-provider` | `apiKey` | Worker only |
| `voice-livekit-keys` | `keys` (LiveKit key mapping) | Optional internal or public LiveKit (in its namespace) |

`url` is server-side; optional `publicUrl` is browser-side and must be nonempty
when supplied. Omitting it uses the same endpoint for both. Use an explicitly approved
nonprod provider and one allowlisted test user. The target schema is in webapp's
`workspace/voice/targets.example.json`; copy only the selected resident credential,
not the entire gateway Secret. The allowed organization ID must match the
branch preview's authenticated fixture organization, not an arbitrary browser
input. Test resident tokens must actually authorize the A2A endpoint.

Direct provider credentials in `modes/env` are a bounded preview exception.
`modes/gateway` requires the separately reviewed realtime gateway integration
described below. Automatic resident discovery remains deferred. Limit this
preview to one organization and authorized test identities.

## Image and review

Build `workspace/voice/Dockerfile` at the exact webapp commit reviewed by Claude:

```sh
docker build -f workspace/voice/Dockerfile -t VOICE_IMAGE:COMMIT .
docker push VOICE_IMAGE:COMMIT
```

Use a nonprod ECR repository with immutable tags and record the commit/digest
mapping. The checked-in image is webapp commit `6465f9b3b0bf8cdb30f9b38158c2569a9b2b0249`,
ECR digest `sha256:b5b1e369e1aac7e71d9907a00ba8ab8276b1ca4910bd28858ca9cbeb5abd8ef1`.
Update that pin only after reviewing the replacement image source. This repository does not publish a new operator image.

Claude reviews both this diff and the runtime changes before cluster apply.
See `docs/plans/shared-voice-preview.md` and PR #108 for review evidence.

## Apply and verify

Local render checks and tools are described in CONTRIBUTING.md. Create the preview
namespace first, then supply its Secrets. Server-side dry-run all resources before
applying them. Apply only these opt-in paths; never apply the repository's default
operator overlay for this milestone.

```sh
kubectl --context webapp-nonprod apply -f deploy/voice/nonprod/namespace.yaml
# Provision Secrets through the approved private channel.
kubectl --context webapp-nonprod apply --dry-run=server -k deploy/voice/nonprod
# Optional internal smoke transport first; wait before starting the worker:
kubectl --context webapp-nonprod apply -k deploy/voice/livekit-internal
kubectl --context webapp-nonprod -n voice-preview rollout status deployment/voice-livekit
kubectl --context webapp-nonprod apply -k deploy/voice/nonprod
kubectl --context webapp-nonprod apply -f deploy/voice/resident-access.yaml
kubectl --context webapp-nonprod -n voice-preview rollout status deployment/voice-broker
kubectl --context webapp-nonprod -n voice-preview rollout status deployment/voice-worker
```

Broker health is `/health:4191`; production worker health is `/:8081`. Config and
Secrets are read at startup; explicitly restart only the relevant preview workload
after rotation. The worker has one prewarmed process and a 2Gi memory limit.
Configuring the broker with a private `publicUrl` is allowed for cluster smoke
only; do not advertise this as usable calling in the webapp.

Before and after apply, record resident operator/runtime image digests, generations,
pod UIDs and restart counts. Concurrent unrelated reconciliations can change
resourceVersions; investigate rather than attribute every change to this PR.
Verify room creation, named worker dispatch, room deletion, denied callers,
resident task receipts, and restart behavior. Provider requests incur usage; use
short sessions. Do not log audio, keys, room tokens, or task contents.

Set `VOICE_SERVICE_URL=https://voice-preview.unsupervised.wtf` and the matching
`VOICE_SERVICE_TOKEN` only on the webapp branch preview, not production or shared
preview base config. Check the exact revision, then authenticated `/voice` and
`/api/voice/residents`. `/calls/*` must not be publicly routed. Complete the real
microphone/audio and task acceptance table in webapp's
`docs/resident-live-voice.md` before calling the feature ready. This first milestone
does not require a second model provider.

## Rollback

Remove the two branch-preview settings first. Stop new calls, then scale only
`voice-worker` and `voice-broker` to zero (active calls may end; resident tasks
continue). Delete the preview Ingress/Service and the three `allow-voice-preview-a2a` policies.
If internal LiveKit was applied, delete only that overlay. Keep the broker PVC and
namespace until receipts have been reviewed; do not delete the namespace or run
`delete -k nonprod` as ordinary rollback because that deletes the PVC. Review and
remove retained Secrets when no longer needed. Recheck existing resident health.

## Public EKS media and inference mode options

[Public media setup](livekit-public/README.md) provides an opt-in dedicated public
EKS node and LiveKit overlay; it does not turn the existing private nodes into
public media servers. [Inference modes](modes/README.md) provides explicit `env`
and `gateway` overlays, including the gateway rollout/network dependencies.
Prepare/review their infrastructure and new runtime image before applying.

## Shared enterprise resident inventory

The nonprod Unsupervised pool is `unsupervised-luce`, `unsupervised-nonprod-bot`
and `unsupervised-vega`. These are the voice targets for both enterprise webapp
organizations. Broker target credentials and caller authorization remain explicit
private configuration; NetworkPolicy access alone does not grant voice access.
The two `outfitter-*` duplicates are excluded from the desired voice policies.

Retirement must preserve data. The Agent finalizer explicitly deletes its
UID-labeled namespace, including workspace PVCs: `--cascade=orphan` alone does
not prevent that finalizer action. Before retiring duplicates, inventory active
tasks and preserve pending review work (including PR 108), archive CRs and
namespace resources privately, and snapshot or copy each workspace with verified
restore evidence. Stop the duplicate runtimes and source automation that would
recreate them. To retain the namespaces, pause operator reconciliation first. While paused,
remove only `agents.aioutfitter.com/finalizer` from each retiring Agent, delete
that CR with orphan propagation, and confirm its namespace and storage remain.
Restore reconciliation only after both duplicate CRs are absent. Verify the exact
live controller finalizer implementation first. Orphan propagation does not bypass
the explicit finalizer. Preserve all other finalizers, labels and owner references.
Confirm retained PVCs, PVs, Secrets and
ConfigMaps after deletion. Do not delete the namespaces or storage.

The operator validates every Organization membership while retaining the first
membership as the catalog, inherited credential and runtime identity. Each
Organization gateway retains its single repository-owner boundary and routes to
the same canonical Agent through its own A2A principal and network policy. Deploy
the reviewed operator version before adding secondary memberships. No homes move,
and gateway owner restrictions remain unchanged.

Membership and inbound webhook routing do not confer outbound GitHub access.
The resident's stdio `github-mcp-server` and native `gh` still use its configured
credentials. Verify each canonical resident's actual account identity and repository
permissions for both GitHub owners before claiming shared implementation/review
capability. One owner's fine-grained PAT does not automatically authorize the
other owner; no outbound credential-selection wrapper is added here.

This is an explicit shared-enterprise trust boundary: all memberships share one
workspace PVC, memory, primary catalog and inherited credentials; inference is
attributed to the primary organization. Keep Unsupervised first. Removing or
reordering that membership changes the primary identity and requires a separate
review, even though the namespace name stays fixed. This design is unsuitable
for unrelated tenants requiring credential or memory isolation.

After moving voice access, explicitly delete the old `allow-voice-preview-a2a`
NetworkPolicy in `agent-outfitter-vega`; applying the new file does not prune it.
Archive before cleanup: retirement reconciliation removes obsolete gateway
principals from retained Secrets and removes their gateway policies. Storage and
Secret objects remain, but archived Secret data may differ from reconciled data.
Set backing PV reclaim policy to `Retain` and verify restore evidence first.
Run only one operator version during migration. An old binary cannot reconcile
the new Agent-owned shared Service. Operator rollback requires a reviewed Service
ownership migration; do not simply start the old binary alongside or after it.
