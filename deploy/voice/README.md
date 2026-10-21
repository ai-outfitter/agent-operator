# Shared voice preview

This opt-in deployment runs the voice runtime from webapp PR #492 in nonproduction.
It adds no operator controller, CRD, or automatic resident discovery. The resident
operator and resident Deployments remain unchanged. One worker pool and one media
endpoint serve all explicitly configured targets.

## Layout

- `nonprod`: one broker, one worker, SQLite PVC, ingress, service accounts and
  network policy, all in `voice-preview`. Not applied by any existing overlay.
- `resident-access.yaml`: separately applied policy admitting only the preview
  broker to `agent-outfitter-vega` on port 8788. Delete it during rollback.
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
| `voice-broker` | `serviceToken` (32+ characters), `targets.json` | Broker only |
| `voice-provider` | `apiKey` | Worker only |
| `voice-livekit-keys` | `keys.yaml` (LiveKit key mapping) | Optional internal LiveKit |

`url` is server-side; `publicUrl` is browser-side. Use an explicitly approved
nonprod provider and one allowlisted test user. The target schema is in webapp's
`workspace/voice/targets.example.json`; copy only the selected resident credential,
not the entire gateway Secret. The allowed organization ID must match the
branch preview's authenticated fixture organization, not an arbitrary browser
input. Test resident tokens must actually authorize the A2A endpoint.

Direct provider credentials are a bounded preview exception. Managed realtime
inference and automatic resident discovery remain deferred. Limit this preview
to one organization and authorized test identities.

## Image and review

Build `workspace/voice/Dockerfile` at the exact webapp commit reviewed by Claude:

```sh
docker build -f workspace/voice/Dockerfile -t VOICE_IMAGE:COMMIT .
docker push VOICE_IMAGE:COMMIT
```

Use a nonprod ECR repository with immutable tags and record the commit/digest
mapping. Replace the placeholder in `nonprod/kustomization.yaml` with that digest
before deployment. This repository does not publish a new operator image.

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
kubectl --context webapp-nonprod apply -k deploy/voice/nonprod
kubectl --context webapp-nonprod apply -f deploy/voice/resident-access.yaml
# Optional internal smoke transport only:
kubectl --context webapp-nonprod apply -k deploy/voice/livekit-internal
kubectl --context webapp-nonprod -n voice-preview rollout status deployment/voice-broker
kubectl --context webapp-nonprod -n voice-preview rollout status deployment/voice-worker
```

Broker health is `/health:4190`; production worker health is `/:8081`. Config and
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
continue). Delete the preview Ingress/Service and `allow-voice-preview-a2a` policy.
If internal LiveKit was applied, delete only that overlay. Keep the broker PVC and
namespace until receipts have been reviewed; do not delete the namespace or run
`delete -k nonprod` as ordinary rollback because that deletes the PVC. Review and
remove retained Secrets when no longer needed. Recheck existing resident health.
