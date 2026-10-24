# Explicit inference modes

Apply exactly one complete runtime overlay: `modes/env` or `modes/gateway`.
Both set `VOICE_INFERENCE_MODE`, `VOICE_BACKEND` and `VOICE_MODEL` on broker and
worker. Keep their backend, model and optional `VOICE_VOICE` default identical:
the broker validates the browser's selected built-in voice against that backend
and persists the selection for the worker. The default manifests use OpenAI,
`gpt-realtime` and `coral`. If `VOICE_VOICE` is omitted, OpenAI uses Coral and
Gemini uses Puck; a configured built-in default is retained. Gateway mode remains
OpenAI-only. Runtime image pins must support this broker configuration.

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

For an Access-protected preview, also provide ConfigMap `accessOrigin` and Secret
`accessClientId`/`accessClientSecret`, all three together. Use a dedicated Access
service token limited to this branch's internal resolver and usage paths. Never
mount the shared preview-agent token. Headers are restricted to that exact HTTPS
origin and are not forwarded to providers or the authority callback.

**Image provenance:** the runtime revision is recorded next to its image pin in
`gateway/kustomization.yaml`; the separate gateway comes from `29406c0`. Both
images require review before publication. Keep the runtime image transformer before the gateway rename:
both use the same ECR repository, and reversing the order replaces the gateway
with the voice image. `check-render.py` rejects identical images. Base/env and gateway mode use the same reviewed voice runtime; the existing
shared gateway is never replaced.

Rollback scales broker/worker/gateway to zero and removes only preview routing.
Retain both voice preview PVCs until receipt/usage review; do not use wholesale
`delete -k` as routine rollback because it deletes PVCs.

The isolated process still starts empty-database reconciliation loops and may
log a Kubernetes issuer-discovery warning without a service-account token. Its
internal maintenance/platform routes are token protected and namespace private.
Keep `INFERENCE_PROVIDERS` and `OPENAI_*` unset; provider selection comes solely
from the scoped resolver. Final image review must verify `ws` and all imported
`workspace/*.mjs` modules are present before updating the image pin.

Set `VOICE_GATEWAY_ORGANIZATION_IDS` on that branch-preview webapp to the
comma-separated canonical organization IDs admitted by this preview. An empty
allowlist refuses the scoped token; the voice worker never receives this token.


When activating a new voice-selection runtime, wait for active calls to finish,
then update the worker before the broker. The new broker requires
`VOICE_BACKEND`; supply it together with the matching model and optional default
voice. Render and validate both modes before applying. Do not interrupt human
calls to roll out this configuration.
