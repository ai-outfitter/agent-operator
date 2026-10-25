# Shared voice preview milestone

Status: simplified after Claude review of plan commit 30f27c0. Do not merge or release during this milestone.

## Outcome

Deploy the smallest opt-in shared voice stack from reviewed PR commits to the
existing nonproduction EKS cluster, connect webapp PR #492, and test one browser
call. Keep existing residents and their operator running unchanged. One LiveKit
service and one worker pool serve all configured residents; never deploy
LiveKit or a worker pool per resident.

## Existing pieces and constraints

- Webapp PR #492 contains a broker, worker, browser UI, OpenAI/Gemini adapters,
  and broker tests. Broker accepts an explicit target allowlist with organization,
  installation, revision, user IDs and A2A credentials. It uses local SQLite.
- Agent Operator main already manages resident A2A and organization gateways.
  Its current main additionally supports direct Pi residents. Do not roll this
  entire new main into live resident workloads just to prove voice deployment.
- Current nonprod has NGINX ingress, Cilium, and gp3-encrypted-csi storage.
  Nodes have no public IPs; there is no deployed LiveKit. Ordinary HTTPS
  ingress is insufficient for public WebRTC audio. Do not invent a reachable
  media address or claim HTTP health proves audio.
- Claude Code is available for both plan and code reviews. Record exact commits,
  findings, resolutions and remaining limitations in the PR.

## Proposed first implementation

Put preview deployment assets under deploy/voice in Agent Operator. Reuse the
broker/worker image built from webapp PR #492. Use a dedicated voice-preview
namespace, one broker replica (Recreate, PVC), one shared worker replica,
separate Secrets, least-privilege service accounts, probes and resource limits.
Use existing task transport and one explicitly allowlisted test resident/user.
No new user identity database, meeting provider, provider switching UI, or new
conversation-memory contract.

Claude review decision: manifests only. No controller mode, CRD, operator image,
or automatic Agent/Organization voice provisioning. The existing resident
operator stays unchanged and this milestone does not re-test its binary.
One flat deployment directory reuses the webapp image. A separate, explicitly
applied NetworkPolicy in agent-outfitter-vega allows only the preview broker to
reach A2A; no resident Deployment or existing policy is patched.

Build the exact webapp commit manually, push it to a nonprod ECR repository with
immutable tags, and pin the resulting digest. Split internal LIVEKIT_URL from
browser LIVEKIT_PUBLIC_URL. Publish only exact /residents, /sessions and /end
broker paths. Worker routes remain ClusterIP-only. Warm one worker job process.

Media decision is pending user input. The internal-only overlay supports cluster
smoke tests. Public media infrastructure or an existing hosted project is required
for the real browser audio gate; do not mark that gate complete for an API test.

LiveKit is optional infrastructure: the runtime always requires a configured
LiveKit endpoint, but the deployment may use an existing shared service instead
of creating one. For isolated cluster smoke tests, an internal-only LiveKit
instance can prove room/dispatch lifecycle. Browser voice acceptance requires
reachable signaling and media, a tested TURN path, or an existing hosted endpoint.
Do not create a paid SaaS account. Test actual cluster media feasibility before
adding public node groups or load balancers. Separate media infrastructure changes
from ordinary app deployment.

Keep provider credentials only in the worker Secret for this bounded preview,
never in the browser or resident. This is an explicit prototype exception;
managed realtime inference identity is deferred, not claimed implemented.
Use only one authorized organization/provider and document where credentials
came from without logging values. Do not copy production credentials blindly.

## Sequence and merge gates

1. Open draft plan PR. Claude adversarially reviews scope, architecture,
   isolation, networking and tests, with particular attention to simplification.
2. Resolve the review and implement only the resulting milestone. Preserve
   static runtime interfaces wherever possible. Add meaningful validation of
   manifest shape, secret separation, singleton SQLite, and runtime smoke tests.
3. Claude reviews exact implementation diff. Fix and re-review substantive
   issues before deployment. Publish branch images by immutable digest; do not
   release or merge. Record upstream base and image source commits.
4. Apply only isolated nonprod resources after checking kubectl context, RBAC,
   storage, image pull and connection prerequisites. Do not replace the active
   resident operator or patch/delete existing residents. The separately named preview NetworkPolicy is the only resident-namespace
   addition. Stop if replacing resident workloads is necessary.
5. Configure only the webapp branch preview with broker URL/token; preserve
   production/base-preview settings. Authenticate and exercise the browser UI.
6. Record exactly which gates passed and keep the PR draft until live acceptance.

## Validation

- Render and server-side dry-run deployment configuration. Verify Secrets never
  appear in rendered review artifacts.
- Broker health, worker registration, room creation and cleanup, scoped room token,
  and configured resident access from within the cluster.
- Browser on exact PR revision: list allowed resident, start/mute/hang up, failure
  recovery; verify media connection and actual agent audio. Fixture tests are
  marked synthetic and cannot satisfy the real-provider gate.
- Real provider conversation, interruption, harmless resident task and verified
  result; record latency and credentials/configuration prerequisites.
- Unauthorized organization/user denied; hangup does not cancel or replay work.
- Restart broker/worker: preserve receipts and fail calls explicitly; no task
  replay. PVC remains single writer.
- Rollback: disable preview connection, stop new calls, drain/stop preview worker
  and broker, remove only preview deployments; preserve PVC/receipts for review.
  Document opt-in deletion separately. Existing residents remain healthy.

## Deferred

Automatic resident discovery/voice CRDs, managed realtime inference gateway,
worker autoscaling, high availability, cross-channel memory, meeting bots,
Slack huddles, and fully self-hosted speech models.
