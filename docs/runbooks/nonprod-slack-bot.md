# Run the Slack bot as a nonprod agent

This runbook deploys the `nonprod-bot` Slack responder as a controller-managed
Link Operator `Agent` in the Unsupervised nonprod Kubernetes cluster. Link
Operator recreates its single runtime pod after failure or restart; this
runbook does not provide an availability SLO. Slack Socket Mode requires
outbound TLS and no inbound ingress. The runtime also needs DNS and outbound
access to the configured model provider and the read-only Grafana MCP endpoint.

This runbook is limited to the following resources:

- Kubernetes context: `unsup-nonprod-engineer`, passed on every command.
- Link Operator namespace: `link-operator-system`.
- Agent namespace: `agent-nonprod-bot`, created and owned by Link Operator.
- Slack channel scope: every channel the bot has joined
  (`SLACK_CHANNEL_IDS=joined`). Inviting it to another channel expands that
  scope without a Kubernetes change.
- Credentials: the commands pass Slack tokens and the Grafana MCP authorization
  header to `kubectl` over stdin and do not intentionally print them. Keep
  shell tracing disabled.
- Deployment images: linux/amd64 and pinned by immutable digest.

The manifests and build inputs live in [`dev/nonprod`](../../dev/nonprod).

## Prerequisites

You need:

- `kubectl` access through the `unsup-nonprod-engineer` context;
- Docker Buildx with linux/amd64 support;
- an authenticated OCI registry that the nonprod EKS nodes can pull from;
- the `channels` and `link-operator` repositories checked out as sibling
  directories;
- the `nonprod-bot` Slack CLI app configured by following the
  [Channels local Slack runbook](https://github.com/ai-outfitter/channels/blob/main/docs/runbooks/slack-local.md);
- read access to key `nonprod-bot-authorization` in
  `secret/mcp-grafana-auth` through context `unsup-prod-admin`;
- local Pi authentication in `$HOME/.pi/agent/auth.json`.

Preflight without reading any secret values:

```sh
kubectl --context unsup-nonprod-engineer get --raw=/readyz
kubectl --context unsup-nonprod-engineer auth can-i create \
  customresourcedefinitions.apiextensions.k8s.io
kubectl --context unsup-nonprod-engineer auth can-i create namespaces
kubectl --context unsup-prod-admin -n unsupervised-singleton auth can-i get \
  secret/mcp-grafana-auth
docker buildx inspect --bootstrap
test -s "$HOME/.pi/agent/auth.json"
```

Confirm that deployment commands target `unsup-nonprod-engineer` and only the
gateway Secret read targets `unsup-prod-admin`. Do not substitute other
contexts. The Pi check confirms only that the auth file exists; successful
authentication is verified when the agent starts.

## Build and publish immutable images

Set a registry repository and a build tag. A commit-derived tag can be reused
when rebuilding a dirty worktree; deployment immutability comes from the
resolved digest. Do not deploy `latest`, `main`, or another mutable reference.

```sh
export LINK_IMAGE_REPOSITORY="<registry>/<owner>"
export LINK_IMAGE_TAG="$(git rev-parse --short=12 HEAD)"
export CHANNELS_REVISION="$(git -C ../channels rev-parse HEAD)"
```

Build and push linux/amd64 images:

```sh
docker buildx build \
  --platform linux/amd64 \
  --file dev/nonprod/Dockerfile.operator \
  --tag "$LINK_IMAGE_REPOSITORY/link-operator:$LINK_IMAGE_TAG" \
  --push .

docker buildx build \
  --platform linux/amd64 \
  --build-context channels=../channels \
  --build-arg "CHANNELS_REVISION=$CHANNELS_REVISION" \
  --file dev/nonprod/Dockerfile.agent \
  --tag "$LINK_IMAGE_REPOSITORY/link-agent:$LINK_IMAGE_TAG" \
  --push .
```

Before pushing or deploying the agent image, exercise the exact pinned MCP
adapter in that image against a local authenticated Streamable HTTP MCP fixture.
The test proves environment expansion in the authorization header, tool
discovery, tool invocation, and rejection of an invalid credential:

```sh
docker buildx build \
  --platform linux/amd64 \
  --build-context channels=../channels \
  --build-arg "CHANNELS_REVISION=$CHANNELS_REVISION" \
  --file dev/nonprod/Dockerfile.agent \
  --tag link-agent:nonprod-mcp-test \
  --load .
dev/nonprod/test-mcp-adapter.sh link-agent:nonprod-mcp-test
```

The final command must print a JSON object with `"result":"passed"`.

Resolve both pushed images to registry digests. The resulting values must
contain `@sha256:`:

```sh
export LINK_OPERATOR_IMAGE="$(
  docker buildx imagetools inspect \
    "$LINK_IMAGE_REPOSITORY/link-operator:$LINK_IMAGE_TAG" \
    --format '{{json .Manifest.Digest}}' | tr -d '"'
)"
export LINK_AGENT_IMAGE="$(
  docker buildx imagetools inspect \
    "$LINK_IMAGE_REPOSITORY/link-agent:$LINK_IMAGE_TAG" \
    --format '{{json .Manifest.Digest}}' | tr -d '"'
)"
export LINK_OPERATOR_IMAGE="$LINK_IMAGE_REPOSITORY/link-operator@$LINK_OPERATOR_IMAGE"
export LINK_AGENT_IMAGE="$LINK_IMAGE_REPOSITORY/link-agent@$LINK_AGENT_IMAGE"
```

## Install Link Operator

Render the nonprod kustomization, replace only its two zero-digest image
placeholders in the rendered output, and perform a server-side dry run before
applying it. Never edit the committed placeholders in place.

```sh
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
kubectl kustomize dev/nonprod | sed \
  -e "s#example.invalid/link-operator@sha256:[0-9a-f]*#$LINK_OPERATOR_IMAGE#" \
  -e "s#example.invalid/link-agent@sha256:[0-9a-f]*#$LINK_AGENT_IMAGE#" \
  >"$work/operator.yaml"
test "$(grep -c 'example.invalid' "$work/operator.yaml")" -eq 0
kubectl --context unsup-nonprod-engineer create namespace \
  link-operator-system --dry-run=client -o yaml | \
  kubectl --context unsup-nonprod-engineer apply -f -
kubectl --context unsup-nonprod-engineer label namespace \
  link-operator-system app.kubernetes.io/managed-by=link-operator --overwrite
kubectl --context unsup-nonprod-engineer apply \
  --dry-run=server -f "$work/operator.yaml"
kubectl --context unsup-nonprod-engineer apply -f "$work/operator.yaml"
kubectl --context unsup-nonprod-engineer \
  -n link-operator-system rollout status \
  deployment/link-operator-controller-manager --timeout=5m
kubectl --context unsup-nonprod-engineer \
  api-resources --api-group=link.aioutfitter.com
```

## Create the agent workspace

Apply the organization and agent first. The controller creates
`agent-nonprod-bot`, its service account, and its persistent volume claims. It
does not create the runtime Deployment until `secret/nonprod-bot-slack`,
`secret/nonprod-bot-grafana`, `configmap/nonprod-bot-runtime`, and
`configmap/nonprod-bot-pi-ready` exist.

```sh
kubectl --context unsup-nonprod-engineer apply \
  -f dev/nonprod/organization.yaml \
  -f dev/nonprod/agent.yaml
kubectl --context unsup-nonprod-engineer wait \
  --for=jsonpath='{.status.namespace}'=agent-nonprod-bot \
  agent/nonprod-bot --timeout=3m
kubectl --context unsup-nonprod-engineer apply \
  -f dev/nonprod/runtime-config.yaml
```

Render the seeder with `$LINK_AGENT_IMAGE`, then apply it. After the seeder pod
is ready, review and copy the entire local `$HOME/.pi` tree into the agent
workspace volume. This includes credentials and any other Pi state in that
directory:

```sh
sed \
  "s#example.invalid/link-agent@sha256:[0-9a-f]*#$LINK_AGENT_IMAGE#" \
  dev/nonprod/pi-seeder.yaml | \
  kubectl --context unsup-nonprod-engineer apply -f -
kubectl --context unsup-nonprod-engineer \
  -n agent-nonprod-bot wait --for=condition=Ready \
  pod/nonprod-bot-pi-seeder --timeout=3m
tar -C "$HOME" -cf - .pi | \
  kubectl --context unsup-nonprod-engineer \
    -n agent-nonprod-bot exec -i nonprod-bot-pi-seeder -- \
    tar -C /workspace -xf -
kubectl --context unsup-nonprod-engineer \
  -n agent-nonprod-bot exec nonprod-bot-pi-seeder -- \
  test -s /workspace/.pi/agent/auth.json
kubectl --context unsup-nonprod-engineer \
  -n agent-nonprod-bot delete pod/nonprod-bot-pi-seeder --wait=true
kubectl --context unsup-nonprod-engineer \
  -n agent-nonprod-bot create configmap nonprod-bot-pi-ready \
  --from-literal=LINK_PI_CONFIG_READY=true \
  --dry-run=client -o yaml | \
  kubectl --context unsup-nonprod-engineer apply -f -
```

## Transfer Grafana MCP credentials without printing them

Pipe the dedicated bot authorization header from the Grafana MCP gateway Secret
directly into the scoped sync helper. The credential is distinct from Grafana
admin and developer credentials. Keep shell tracing disabled:

```sh
kubectl --context unsup-prod-admin \
  -n unsupervised-singleton get secret/mcp-grafana-auth \
  -o go-template='{{index .data "nonprod-bot-authorization" | base64decode}}' |
  LINK_KUBE_CONTEXT=unsup-nonprod-engineer \
  LINK_AGENT_NAMESPACE=agent-nonprod-bot \
    node dev/nonprod/grafana-secret-sync.mjs
```

The helper passes a Secret manifest to `kubectl` on stdin and never places the
header in command arguments. Confirm only the key name:

```sh
kubectl --context unsup-nonprod-engineer \
  -n agent-nonprod-bot get secret/nonprod-bot-grafana \
  -o go-template='{{range $key, $value := .data}}{{printf "%s\n" $key}}{{end}}'
```

The command must print exactly `MCP_GRAFANA_BASIC_AUTH_HEADER`.

## Transfer Slack CLI credentials without printing them

From the sibling Channels checkout, first refresh the ignored Slack CLI
project:

```sh
cd ../channels
npm run setup:slack
```

Set `SLACK_CLI_TEAM_ID` to the workspace containing `nonprod-bot`. Replace the
ignored Slack CLI launcher with the credential-sync helper and run it:

```sh
export SLACK_CLI_TEAM_ID=T7GCW93AA
cp ../link-operator/dev/nonprod/slack-secret-sync.mjs \
  dev/nonprod-bot/app.js
(
  cd dev/nonprod-bot
  LINK_KUBE_CONTEXT=unsup-nonprod-engineer \
  LINK_AGENT_NAMESPACE=agent-nonprod-bot \
    "$HOME/.local/bin/slack" run \
    --app local \
    --team "$SLACK_CLI_TEAM_ID" \
    --force
)
```

After Slack CLI prints `secret/nonprod-bot-slack created` or `configured`,
press Ctrl-C. Slack CLI remains open after the one-shot helper exits. Restore
the ignored launcher immediately:

```sh
npm run setup:slack
```

The helper passes a Secret manifest to `kubectl` on stdin and does not place
tokens in command arguments. List the Secret's key names without reading their
values. The command must print exactly `SLACK_APP_TOKEN` and `SLACK_BOT_TOKEN`:

```sh
kubectl --context unsup-nonprod-engineer \
  -n agent-nonprod-bot get secret/nonprod-bot-slack \
  -o go-template='{{range $key, $value := .data}}{{printf "%s\n" $key}}{{end}}'
```

## Verify persistent operation

Wait for the controller-managed Deployment:

```sh
kubectl --context unsup-nonprod-engineer wait \
  --for=condition=Ready agent/nonprod-bot --timeout=5m
kubectl --context unsup-nonprod-engineer \
  -n agent-nonprod-bot rollout status \
  deployment/agent-runtime --timeout=5m
kubectl --context unsup-nonprod-engineer \
  -n agent-nonprod-bot logs deployment/agent-runtime --tail=100
```

The logs must show Slack bot authentication, a Socket Mode connection, and the
Grafana MCP server connected without printing credential values.

Mention the bot in a channel it has joined:

```text
@nonprod-bot [channels-nonprod-smoke] Reply with a one-sentence confirmation
that the persistent nonprod channel test works.
```

From the sibling `channels` checkout, set the smoke-test channel ID, replace
the ignored launcher with the verifier helper, and run it through Slack CLI.
Continue only after the verifier exits zero and reports exactly one threaded
reply plus the handled reaction:

```sh
cd ../channels
export SLACK_VERIFY_CHANNEL_IDS="<channel-id>"
cp ../link-operator/dev/nonprod/slack-verify-app.mjs \
  dev/nonprod-bot/app.js
(
  cd dev/nonprod-bot
  "$HOME/.local/bin/slack" run \
    --app local \
    --team "$SLACK_CLI_TEAM_ID" \
    --force
)
```

After the verifier reports success, press Ctrl-C and run
`npm run setup:slack` to restore the normal ignored launcher.

Verify restart recovery:

```sh
before="$(
  kubectl --context unsup-nonprod-engineer -n agent-nonprod-bot \
    get pod -l app.kubernetes.io/name=link-agent \
    -o jsonpath='{.items[0].metadata.uid}'
)"
kubectl --context unsup-nonprod-engineer -n agent-nonprod-bot \
  rollout restart deployment/agent-runtime
kubectl --context unsup-nonprod-engineer -n agent-nonprod-bot \
  rollout status deployment/agent-runtime --timeout=5m
after="$(
  kubectl --context unsup-nonprod-engineer -n agent-nonprod-bot \
    get pod -l app.kubernetes.io/name=link-agent \
    -o jsonpath='{.items[0].metadata.uid}'
)"
test "$before" != "$after"
```

Post a second marked mention. Restart recovery passes only if the replacement
pod has a different UID and the verifier reports exactly one reply to the new
mention. A passing result shows that the replacement pod reread the Pi
credentials and Slack Secret and established a new Socket Mode connection.

## Rotate credentials

- Slack: rotate the app or bot token in Slack, rerun the credential-transfer
  step, then restart `deployment/agent-runtime`.
- Grafana MCP: rotate the dedicated `nonprod-bot` gateway credential, rerun the
  Grafana credential-transfer step, then restart the Deployment.
- Pi: rerun the seeder flow and restart the Deployment.
- Images: repeat the immutable-image render and server-side dry-run procedure
  above with the new digests, apply the rendered manifest, and wait for the
  controller and agent rollouts.

Never export Secret values from Kubernetes during rotation.

## Scoped teardown

Deleting the `Agent` invokes its finalizer and removes its owned namespace.
Delete only the resources named here:

```sh
kubectl --context unsup-nonprod-engineer delete agent/nonprod-bot
kubectl --context unsup-nonprod-engineer delete \
  organization/nonprod-channels
```

Removing Link Operator itself is a separate cluster-wide decision because
future agents may share it. Do not delete its CRDs or namespace as part of bot
rotation or ordinary bot teardown.

## Current nonprod deployment

The deployment established on 2026-07-25 uses:

- deployment-manifest source: `20320e1`;
- operator binary source: `d5ffe5dd05c10796f6793493aab06740d0fc32ff`;
- Channels source: `4139900df418f013c18412247fa530043391eb9b`;
- operator image:
  `216577824627.dkr.ecr.us-east-1.amazonaws.com/ai-outfitter/link-operator@sha256:9a86250b8e59f188e077c0f0077dd48ea9b3ae2ee25f9f9fb2601a250def01df`;
- agent image:
  `216577824627.dkr.ecr.us-east-1.amazonaws.com/ai-outfitter/link-agent@sha256:551b2340bbc83da580d2362c2e8a384882a842bdd36a605f4a4c732d1d4a24be`.

Both ECR scans completed with no findings before deployment. Link Operator
reported `Agent/nonprod-bot` Ready, both encrypted CSI volumes bound, and the
replacement pod established a new Socket Mode connection after a Deployment
restart. Record the Slack round-trip verifier result here after the first
human mention passes.
