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
