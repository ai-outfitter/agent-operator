# Nonproduction preview validation — 2026-10-21

PR #108 remains draft and unmerged. Claude Code adversarially reviewed the plan,
implementation, fixes, and simplifications; review records are linked in the PR.
The resulting milestone uses ordinary Kustomize manifests, with no new controller,
CRD, or operator binary. Existing operator and resident workloads were not replaced.

## Deployed revisions

- Cluster context: `webapp-nonprod`, AWS account `216577824627`, namespace `voice-preview`.
- Deployment manifests: `a6a4901e325fe4ad90ac520d75e2e9cd6e1a827d`.
- Broker/worker source: webapp `6465f9b3b0bf8cdb30f9b38158c2569a9b2b0249`.
- ECR image: `sha256:b5b1e369e1aac7e71d9907a00ba8ab8276b1ca4910bd28858ca9cbeb5abd8ef1`.
- Webapp preview: `2d31050a855812e351a14918cb263d4b1f4a6ef7`, verified by `/api/health`.
- Browser: https://org-feat-resident-live-voice.ai-outfitter.dev/voice
- Broker: https://voice-preview.unsupervised.wtf

The three Deployments (broker, worker, internal LiveKit) each have one ready replica.
Only the branch preview has broker credentials. Resident access is limited to Vega,
the preview fixture organization, and the `preview-agent` test identity. Human users
are not yet allowlisted. Provider credentials stay in the worker Secret.

## Verified

- Local manifest render invariants and Kubernetes server-side dry-runs passed.
- Voice runtime TypeScript checks passed; seven local tests passed, with the local
  LiveKit integration skipped. Dedicated voice CI run `39686491735` passed, including
  real LiveKit. This does not establish full webapp CI at the final edge-only commit.
- Public broker rejects missing authentication; `/calls/*` is not publicly routed.
  Internal checks reject an unlisted caller and admit only the configured resident.
- Real LiveKit room creation, room-scoped token, named worker dispatch and explicit
  room deletion passed. Worker registration succeeded.
- A harmless read-only task submitted through the broker reached the real resident.
  Task `6d3937b0-738a-4a4a-a555-0808816d24ca` completed with the expected result after
  hangup. Its receipt survived broker/worker restart without resubmission. The closed
  call's stored state remained `working`; a read-only poll of its external resident
  receipt verified completion. Active-call restart recovery was not tested.
- Before/after checks of ten existing resident/operator Deployment and runtime Pod
  objects showed unchanged images, generations, pod identities and restart counts.
- Authenticated Chromium loaded the allowed resident and created a real broker
  session (HTTP 200). Desktop/mobile screenshots were inspected; no mobile overflow.
  The browser recovered to a disconnected state because media is internal-only.
  The browser used a synthetic microphone, so this is not audio acceptance.

Live validation found two integration issues that were fixed and Claude-reviewed:
Node fetch blocks port 4190 (broker now explicitly uses 4191), and Cloudflare Workers
rejects `redirect: 'error'` (edge fetch now uses `manual` and rejects non-success).
The temporary diagnostic Worker used for the latter was removed.

## Remaining acceptance gates

- Configure reachable LiveKit signaling and media: either self-hosted public EKS
  networking or an explicitly selected existing hosted endpoint. No public media
  load balancer or SaaS account was provisioned in this milestone.
- Replace the configured nonproduction OpenAI credential: the real provider rejected
  it with `invalid_api_key`. No alternative private credential was silently substituted.
- Allowlist a human test identity; test actual microphone/speaker conversation,
  interruption, verified spoken task completion, and latency.
- Publish browser evidence through the producing Foundry run when available; local
  screenshots are not hosted PR demo evidence. Complete final CI/review gates.

No working browser audio, meeting participation, second-provider acceptance,
automatic resident provisioning, or deployment of a new operator binary is claimed.
Rollback steps, including preserving the receipt PVC, are in README.md.

## October 22 follow-up: gateway inference and public media

Implementation source: operator `b0f6c50`, webapp `a026e98` (CI path-filter follow-up
`d1e54a8`). These changes are not deployed to EKS. AWS SSO expired; the device
login did not complete, so no new infrastructure plan/apply or image publication
was attempted with stale credentials. The October 21 evidence above describes
the earlier internal-only stack, not this gateway/public-media implementation.

- Thirty gateway/resolver/regression tests passed, including organization-ID
  binding, scoped platform authorization, real WebSocket forwarding to a mock
  upstream, reset/error cleanup and model selection.
- Twelve voice tests passed with a real local LiveKit server, including SDK
  handshake path/authentication tests. These are not live OpenAI audio acceptance.
- Webapp lint/type checks passed with zero errors; build passed. Voice and root
  gateway container builds passed locally during implementation; reviewed final
  source must still be published and pinned before activation.
- OpenTofu formatting/validation and all env/gateway/public-media render checks
  passed. No AWS plan or public WebRTC result is claimed.
- Claude Code reviewed both PRs and the fixes. Adopted simplifications include
  ordinary manifests, explicit modes, existing broker call credentials, no model
  alias rewriting, small routing helpers, and VPC DNS for the public media pod.

Activation still requires scoped preview credentials, resolver/usage URLs pointing
at the authenticating preview's identity database, new immutable runtime images,
reviewed live node/subnet/security-group inputs, DNS/certificates, and actual
external microphone/speaker plus UDP-blocked TURN validation. Public media remains
an opt-in dedicated node, not a change to existing resident/operator workloads.

## October 23 activation: public EKS media and scoped gateway

This supersedes the earlier AWS-authentication and internal-only deployment
blockers. Both PRs remain unmerged.

- Applied the reviewed S3-backed plan: seven additions, zero changes/deletions.
  The new `voice-preview-public-media` node group has one Ready public node,
  `35.173.195.253`; all ten prior aws-auth role mappings were preserved.
- Public LiveKit runs in `voice-media-preview`. Signaling HTTPS returns 200
  through both ingress addresses. TURN on public TCP443 serves a trusted
  certificate; LiveKit validates and advertises its public address. Both DNS
  records are DNS-only. These checks alone do not establish a working call.
- TURN certificate expires 2027-01-20 23:42:36 UTC; cert-manager renewal is due
  2026-12-21 23:42:36 UTC. Check renewal and restart/verify LiveKit then: no
  automatic certificate-reload controller or scheduled reminder is installed.
- Gateway image is webapp `29406c0`, pinned by immutable ECR digest. The gateway
  has its own PVC and a dedicated branch-scoped platform token. OpenAI resolution
  returned a configured provider key and the preview enables `gpt-realtime`.
  Provider keys are no longer mounted in the voice worker.
- A dedicated 30-day Cloudflare Access token is admitted only by two exact
  branch-preview internal endpoint applications. Resolver returned 200; usage
  rejected an invalid payload with 400 after Access admission. The same token
  was denied on the preview UI and another branch. Existing agent access was
  preserved. The shared Access application was not modified.
- Claude approved the infrastructure plan, gateway Access fix, and deployment
  delta. Render invariants, server dry-runs and OpenTofu validation passed.
- Twenty-five existing resident/operator Deployment and non-Job Pod objects
  retained their identities and specifications. Shared gateway and operator
  deployments were unchanged. CronJob lifecycle changes were excluded.

The first actual browser call reached the worker and exposed a missing distro
CA bundle in the voice runtime. Native WebRTC rejected public WSS before joining.
The public media and gateway health checks must not be reported as audio acceptance.
