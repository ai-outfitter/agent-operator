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

The CA-bundle fix was separately Claude-reviewed, built, and published from webapp
`142a2c46486c8f39ef94fbcf3f0f80bc4f7d9b84`. The built native WebRTC client validated
public WSS, and the runtime was deployed by immutable digest. Preview health
reports that exact revision. Dedicated voice/gateway CI run `39879988694` passed.

Actual Chromium acceptance now passes with a synthetic microphone: session create
200, OpenAI-generated speech and captions, received RTP (3,093 bytes, 27 packets,
nonzero decoded audio energy), outbound microphone RTP (21,855 bytes), hangup 200,
and removal of browser audio elements. Direct selected candidates used UDP.
This validates live gateway-resolved OpenAI and public browser media, not a human's
microphone/speaker experience or speech-driven task completion.

A second Chromium run forced relay policy and allowed only
`turns:turn-voice-preview.unsupervised.wtf:443?transport=tcp`. The selected pair
reported `relayProtocol=tls` and that exact URL; received audio had 1,938 bytes,
15 packets and nonzero decoded energy, with 2,292 outbound bytes and captions.
Hangup returned 200 and audio elements were removed. No DNS override was used.
This is an ICE-configuration restriction, not a test behind an actual UDP-blocking
firewall. Chromium classified its local candidate as `prflx`; validation used the
selected pair's TLS relay protocol/URL rather than assuming the candidate label.

Remaining gates: allowlist a verified human preview identity; test real speech,
interruptions, spoken task completion and latency; verify on a real restrictive
network. The Access service token needs rotation within 30 days; media public IP
and DNS must be reconciled after node replacement. Shared CNI/IMDS permissions,
certificate reload, and the media UDP-buffer warning remain documented preview
operational limitations. This deployment does not add meeting bots or a new
operator binary, and does not merge either PR.

## October 23 availability follow-up

The human preview reported an indefinite availability check. EKS broker/worker/
gateway were healthy with no restarts. All nine ELB targets were InService and
27 NodePort probes succeeded. Intermittent Worker-to-broker requests timed out
before ingress upstream dispatch; this was not evidence of an unhealthy resident.

Webapp `e74fac45112980a15b44e64a62d33bc5c3b3638f` bounds availability checks to ten
seconds and offers retry, including a clear account-not-enabled result. Targeted
Playwright covers stalled requests, 503, empty access and successful retry. Claude
approved the fix, required hooks passed, preview health matches the commit, and
voice/gateway CI run `39984013798` passed.

The broker hostname previously depended on the zone's proxied wildcard. An exact
DNS-only CNAME now maps `voice-preview.unsupervised.wtf` to the existing EKS ingress
ELB, preserving origin TLS and broker authentication. The shared wildcard is
unchanged. Both ingress addresses passed authenticated discovery in ~110–120 ms.
The internal cause of the earlier Cloudflare transport stalls is not established.

The existing verified preview user for the requested human email was confirmed as
an owner of `foundry-governance-preview` with an active session. Only that user's
ID was added to Vega's allowlist; no user/session/membership records were changed.
The broker was restarted to load it. Its target remains the existing
`foundry-reviewer.agent-outfitter-vega.svc.cluster.local:8788` service on nonprod EKS.
Human browser login was not impersonated. Existing agent access remains enabled.

After DNS caches expired, the actual authenticated branch preview loaded Vega and
three consecutive Worker-backed discovery requests returned 200 in 379, 377 and
252 ms. Direct broker scope checks admitted the verified human and test agent,
while an unlisted caller received no residents. No human call was initiated.
Claude approved the scoped DNS/allowlist correction with no blockers.

DNS-only intentionally removes Cloudflare proxy-layer protections for this one
hostname; origin TLS, exact ingress routes and strong broker bearer authentication
remain. The human allowlist is in the private `voice-preview/voice-broker` Secret,
`targets.json`; it is not generated by these manifests. Preserve it during Secret
rotation. Rollback removes only the exact CNAME (restoring wildcard resolution),
removes the added user ID from that Secret, and restarts only `voice-broker`.
The latency samples are acceptance evidence, not an availability guarantee.

### Shared-resident operator activation, 2026-10-24

Reviewed commit `334a199e4314d7be556fc45d43d80c29b74b311c` was published and
activated on nonprod as operator digest
`sha256:1d9c321408f668918c4d473f48e3673812e8510b30915cb4d4005233aa335c35`.
The generated Agent membership schema was applied, removing the one-membership
limit. Old operator termination preceded new startup; the new pod became Ready
with zero restarts and acquired leadership. Before/after Deployment specs were
identical except the manager image: all arguments, defaults, environment and one
replica were preserved, including the existing forge-gateway image argument.
All five then-existing Agents remained Accepted/WorkloadReady/Ready. Private
backups are under `~/.local/state/ai-outfitter/voice-preview/operator-20261024/`.
This receipt establishes operator/schema activation, not duplicate retirement,
secondary membership activation or canonical voice acceptance.

### Legacy Foundry dependency inspection, 2026-10-24

The sanitized [inspection receipt](receipts/2026-10-24-foundry-inspection.json)
records authenticated read-only inspection of both existing Outfitter Foundry
subscriptions. Both were already configuration-invalid with no reviewer, active
run or pending delivery. The legacy Foundry platform endpoint returned 404; the
current app-owned inspect endpoint returned 200. No subscription was reconfigured.

The unused old Outfitter Vega gateway target can therefore be decommissioned
with duplicate retirement. Reactivating resident execution requires a newly
approved canonical resident binding with its actual model, literal prompt digest,
identity policy and new target revision. Do not retain the old minimal-profile
claim for a different resident. The current Foundry configure operation creates
workspace bindings for all roles; it must not be replayed as a resident refresh.

### Voice-specific resident result adaptation, 2026-10-24

Reviewed webapp head `be2d77f0c2948412b929158e00f3d2c0f4ad1c90` produced runtime
`sha256:0278491d0ef179d1709b9316fd02924eac9f5f18fb1dd539cd2ed03743d1ae4c`.
Read-only deployment inspection confirmed both broker and worker at that immutable
digest with observed generations current and one updated Ready replica each.
The source overlays pin both runtime modes to that digest; the separate inference
gateway remains at its existing `6d947c3e…` digest. The worker change adapts actual
terminal resident prose for voice without relaxing the generic typed workflow
result contract. Actual Claude review and focused transport/voice tests passed.

This establishes reviewed runtime activation, not spoken-result acceptance.
Fresh strict calls must independently prove actual task completion and a spoken
result for each of Luce, Vega and nonprod-bot. The retirement script revision pin
is updated to this exact head; it remains unexecuted pending all three confirmed
evidence files and the parent's final retirement decision.

### Identifier preservation instruction, 2026-10-24

Reviewed head `0415a850511c93717a6063bbd2c74c7b4b5f9892` produced runtime
`sha256:eb5a0cf93d5f2bff4b1b5cf81e351afb086aca58dcafeba23dbf027638f7dd63`.
The voice instruction now preserves repository identifiers and asks for
clarification when uncertain. Read-only inspection confirmed broker and worker
at this digest with one updated Ready replica each. Both source runtime modes
pin it, while the inference gateway digest remains unchanged. Actual Claude
review and focused worker checks passed. This instruction change does not
establish correct spoken identifiers or task results: all three strict live
proofs must be recorded at this exact revision before retirement. The retirement
script remains unexecuted and rejects older-revision evidence.

### Bot acceptance fixture scope, 2026-10-24

The nonprod-bot correctly rejected the proposed boot-ID file read because that
operation is outside its configured Grafana/Slack/A2A role. That failed fixture
is retained and does not count as acceptance. The replacement is a permitted
read-only Grafana `list_datasources` authentication diagnostic. An independent
call from the bot pod returned HTTP 401 `Unauthorized`; the expected voice task
must actually complete that diagnostic and speak the same failure accurately.

The bot retirement gate therefore requires the independently verified 401 and
`Unauthorized` result, explicit scope `read-only Grafana authentication diagnostic;
provider query failed`, and a matching completed task and confirmed spoken result
at `0415a850511c93717a6063bbd2c74c7b4b5f9892`. No profile permissions, runtime
image or engineer proof requirements change. A successful diagnostic would prove
truthful reporting of an authentication failure, not working Grafana access.
Retirement remains gated on the parent's review of all three valid proofs.

### Final canonical cutover and operator recovery, 2026-10-24

The reviewed retirement completed: only `unsupervised-luce`,
`unsupervised-vega` and `unsupervised-nonprod-bot` remain as Agent CRs. All three
are Ready with Unsupervised first and Outfitter second in their memberships.
Both organization gateways are Ready and each has two canonical engineer routes;
the bot retains its separate explicit voice endpoint and unchanged role.
The retired Outfitter runtime Deployments remain at zero with no pods. Their
namespaces and original home PVCs remain, Bound to PVs with reclaim policy Retain.
No old home was moved or deleted.

The first multi-organization reconciliation exposed a credential merge defect:
each gateway moved its own unchanged principal to the end of the credentials
array. Alternating organizations changed the transport hash repeatedly and
triggered continuous Recreate rollouts. The operator was paused to contain it.
Fix `526c0f886dc44cb133a94b444d8c7d0f05e9ebcf` updates an existing principal in
place, preserving order and extra fields, and appends only new principals.
It preserves unrelated voice and organization credentials and normal token
rotation. Actual Claude approved; full operator tests and lint passed. The new
convergence regression fails immediately with the old implementation and passes
both repeated merges and actual multi-organization reconciliation with the fix.

The fixed operator was published and activated at immutable digest
`sha256:2393b9f79d03e80388ba4aec17a19d17212d816bf46cfdab1f3bffa801662d86`.
The deployment change preserved its full spec except the image and restored one
replica. Four snapshots over 90 seconds then showed unchanged deployment
generations (Luce 135, Vega 120, bot 44), stable credential hashes and one updated
Ready replica each. No new operator errors appeared. The final cutover verifier
passed the exact three identities/memberships, both gateway routes and retained
old home checks.

Voice runtime and edge revision remain
`0415a850511c93717a6063bbd2c74c7b4b5f9892`; no voice pin was changed for this
operator fix. All three strict synthetic-microphone proofs at that revision
passed actual task completion and matching spoken-result checks. Luce and Vega
read back real GitHub PR facts; the bot truthfully spoke the independently verified
Grafana 401 Unauthorized diagnostic. That bot proof does not establish working
Grafana access. After cutover, all six organization-to-resident bindings still
passed authenticated read-only checks with identity/profile unchanged.
Authenticated human and preview-agent discovery returned exactly the same three
residents for both enterprise organizations; an unlisted caller saw none. Exact
edge health and discovery HTTP 200 were verified after refreshing the test session.

These checks establish the nonprod canonical cutover and stable reconciliation.
A real human microphone/speaker conversation and a physical restrictive-network
call remain separate acceptance checks. Production rollout and PR merge were not
performed. Private task text, credentials, audio and raw home data are omitted
from this receipt; the parent retained the private evidence and backups.

## 2026-10-25 direct main-Pi runtime activation (acceptance pending)

AWS account `216577824627` and kubectl context `webapp-nonprod` were verified
before changes. Fresh configuration and credential backups were saved privately
with mode 0600; no credential values were logged. Immediately before rollout,
the broker had zero active unexpired calls and zero unfinished task records.
All three resident task stores had zero nonterminal tasks. Mounted credential
principal names (not tokens) confirmed `voice-broker:voice-preview` for each
canonical resident. All six existing target URLs already used their canonical
Service root on port 8788, so URLs, credentials and user allowlists were retained.

Published ECR tags and digests were independently checked:

- Voice runtime `pi-voice-8123ab0`:
  `sha256:a13a77d18408c09b19af3135da9765adc3f84ce09fc77f4119e5bb3f9cba397f`.
- Separate speech gateway `pi-speech-8123ab0`:
  `sha256:869fff49557bbf439a9ece8773e1b661fe71ac6ff8abd41e4953ea20d2ffd419`.
- Direct-Pi resident `pi-conversation-988fb91`:
  `sha256:ff417d81de0def1dec730952686efcd87e325a42b3a99a254666bac4ff3d8f6e`.

Actual Claude reviews approved the source preparation, published pin delta and
follow-up documentation. Both env and gateway render checks passed with the new
pins, and server dry-runs validated the narrow live patches. Luce and Vega now
use the new direct-Pi image. Their existing `envFrom` arrays were preserved with
only the non-optional `resident-voice-conversation` ConfigMap appended. Every
other Agent spec field, including models, tools, credentials and memberships,
was retained. Read-only authenticated polling of a nonexistent turn returned
`404 turn not found`, confirming the enabled conversation adapter without
creating a lease, call or resident task.

The bot kept its existing image
`ghcr.io/unsupervisedcom/nonprod-bot-agent:6f08c52-r33429105296`, launcher,
profile and permissions. Its canonical Organization catalog revision and embedded
catalog-bootstrap revision changed together to the independently reviewed
`6295169817c24b9cbeddb98604c1de7acfdd8473` (private catalog draft PR 57).
That catalog changes only the bot's Channels dependency to
`988fb91c46b9586572d06bbd27e5918a6e70254f` (Channels PR 91). Independent
post-rollout inspection confirmed the actual cached Channels HEAD, Outfitter
1.5.0 / Pi 0.80.10, the retained bearer principal and the same authenticated
read-only polling response. No bot image rebuild or policy expansion was needed.

Gateway, worker and broker were updated in that order, checking for active calls
before each update. Only their images and the reviewed speech settings changed;
all other Deployment spec fields were preserved. Broker and worker now agree on
`gateway`, `openai`, `gpt-4o-mini-transcribe`, `gpt-4o-mini-tts` and default
`coral`; `VOICE_MODEL` is absent. All three completed rollout with one updated,
Ready replica. Broker local `/health` returned HTTP 200. The resident operator's
full Deployment spec stayed unchanged, including image
`sha256:2393b9f79d03e80388ba4aec17a19d17212d816bf46cfdab1f3bffa801662d86`.

This receipt records nonproduction activation and readiness, not speech
acceptance. Real microphone transcription, persistent Pi context, tool use,
audible literal Pi output and interruption behavior are being checked separately.
No acceptance result, production rollout or PR merge is claimed here. Rollouts
and restarts stopped before the parent's first live Luce acceptance call.
