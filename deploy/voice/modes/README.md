# Explicit inference modes

Apply exactly one complete runtime overlay: `modes/env` or `modes/gateway`.
Both set `VOICE_INFERENCE_MODE` on broker and worker and `VOICE_MODEL` on the
broker; model authorization therefore uses the broker's configured model.
The env overlay retains the earlier runtime pin; gateway mode requires new image
pins. Rebuild/review/pin the runtime containing mode support before deployment.

`env` is the bounded direct-provider preview: only worker gets
`voice-provider.apiKey`; broker gets no provider key.

`gateway` removes the provider Secret reference entirely. Worker uses the internal
`http://voice-inference-gateway:4040` root (it adds `/inference/v1/realtime`). Only OpenAI
realtime is currently supported. No platform API key, provider key or broker
service token is mounted in worker; it exchanges the per-call worker credential.

The overlay runs an isolated gateway with its own empty SQLite PVC. It mounts no
Kubernetes token and configures no workflow callbacks; it cannot scan or execute
the shared gateway's persisted workflow state. Its Service is internal and no
Ingress exposes its platform endpoints. It has the same namespace isolation as
the voice stack: the base policy admits intra-namespace worker/gateway/broker
traffic, including private authority callback TCP4191 and worker inference
TCP4040. Public HTTPS egress permits provider and platform resolution requests.

Before applying, supply `voice-gateway-platform` ConfigMap keys `resolveUrl` and
`usageUrl` as **HTTPS** URLs (HTTP is rejected at startup). Both must target the
same branch-preview deployment and identity database that authenticated the
caller. Never point this isolated preview gateway at a production resolver merely
because the organization slug looks the same. Verify the returned canonical
organization ID against the stored call ID binding before using provider keys.
Supply a Secret of the same
name with `platformToken` (32+ characters) equal to that branch preview's
**dedicated `VOICE_GATEWAY_TOKEN`**, accepted only by inference resolution and
usage handlers. It must differ from the platform-wide `WORKSPACE_GATEWAY_TOKEN`.
The gateway process reads this limited credential through its existing
`WORKSPACE_GATEWAY_TOKEN` variable for its resolve/usage requests; that variable
name does not grant platform-wide access. Only this gateway mounts the scoped
voice credential. Gateway and broker mount a separate `voice-broker.authorityToken` as
`VOICE_AUTHORITY_TOKEN` (32+ characters, distinct from the webapp service token) and call the broker's private `/inference-authority`
endpoint. That credential is mounted only in broker and gateway. The callback
must return canonical organization/user scope from stored call state, never
trust scope from worker input. The broker ingress exposes only
residents/sessions/end, never authority or worker callback routes.

**Deployment gate:** the gateway image deliberately uses an `invalid.local`
placeholder. Build the root webapp Dockerfile at the reviewed
realtime-gateway source and replace the image with its immutable digest in
`gateway/kustomization.yaml`. The gateway overlay also replaces the old base voice runtime pin with a
deliberately unrunnable `invalid.local` placeholder. Rebuild and pin the voice
runtime for explicit inference-mode support before selecting gateway mode.
The base/env overlay retains its existing image pin. Neither current checked-in pin
proves that new code is available. The existing shared gateway is never replaced.

Rollback scales broker/worker/gateway to zero and removes only preview routing.
Retain both voice preview PVCs until receipt/usage review; do not use wholesale
`delete -k` as routine rollback because it deletes PVCs.

The isolated process still starts empty-database reconciliation loops and may
log a Kubernetes issuer-discovery warning without a service-account token. Its
internal maintenance/platform routes are token protected and namespace private.
Keep `INFERENCE_PROVIDERS` and `OPENAI_*` unset; provider selection comes solely
from the scoped resolver. Final image review must verify `ws` and all imported
`workspace/*.mjs` modules are present before updating the image pin.
