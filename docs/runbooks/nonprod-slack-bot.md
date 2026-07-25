# Run the Slack bot as a nonprod agent

This runbook deploys the `nonprod-bot` Slack responder as a controller-managed
Link Operator `Agent` in the Unsupervised nonprod Kubernetes cluster. Link
Operator recreates its single runtime pod after failure or restart; this
runbook does not provide an availability SLO. Slack Socket Mode requires
outbound TLS and no inbound ingress. The runtime also needs DNS and outbound
access to the configured model provider.

This runbook is limited to the following resources:

- Kubernetes context: `unsup-nonprod-engineer`, passed on every command.
- Link Operator namespace: `link-operator-system`.
- Agent namespace: `agent-nonprod-bot`, created and owned by Link Operator.
- Slack channel scope: every channel the bot has joined
  (`SLACK_CHANNEL_IDS=joined`). Inviting it to another channel expands that
  scope without a Kubernetes change.
- Credentials: the commands pass Slack tokens to `kubectl` over stdin and do
  not intentionally print them. Keep shell tracing disabled.
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
  [Channels local Slack runbook](../../../channels/docs/runbooks/slack-local.md);
- local Pi authentication in `$HOME/.pi/agent/auth.json`.

Preflight without reading any secret values:

```sh
kubectl --context unsup-nonprod-engineer get --raw=/readyz
kubectl --context unsup-nonprod-engineer auth can-i create \
  customresourcedefinitions.apiextensions.k8s.io
kubectl --context unsup-nonprod-engineer auth can-i create namespaces
docker buildx inspect --bootstrap
test -s "$HOME/.pi/agent/auth.json"
```

Confirm that both `kubectl` commands above target
`unsup-nonprod-engineer`. Do not substitute another context. The Pi check
confirms only that the auth file exists; successful authentication is verified
when the agent starts.

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
`configmap/nonprod-bot-runtime`, and `configmap/nonprod-bot-pi-ready` exist.

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

## Transfer Slack CLI credentials without printing them

From the sibling Channels checkout, first refresh the ignored Slack CLI
project:

```sh
cd ../channels
npm run setup:slack
```

Set `SLACK_CLI_TEAM_ID` to the workspace containing `nonprod-bot`. Replace the
ignored Slack CLI launcher with the credential-sync helper and run it. The trap
restores the launcher whether the sync succeeds or fails:

```sh
export SLACK_CLI_TEAM_ID=T7GCW93AA
backup="$(mktemp)"
cp dev/nonprod-bot/app.js "$backup"
trap 'cp "$backup" dev/nonprod-bot/app.js; rm -f "$backup"' EXIT
cp ../link-operator/dev/nonprod/slack-secret-sync.mjs \
  dev/nonprod-bot/app.js
LINK_KUBE_CONTEXT=unsup-nonprod-engineer \
LINK_AGENT_NAMESPACE=agent-nonprod-bot \
  "$HOME/.local/bin/slack" run \
    --app local \
    --team "$SLACK_CLI_TEAM_ID" \
    --force
cp "$backup" dev/nonprod-bot/app.js
rm -f "$backup"
trap - EXIT
```

The helper passes a Secret manifest to `kubectl` on stdin. It does not place
tokens in command arguments. List the Secret's key names without reading their
values. The command must print exactly `SLACK_APP_TOKEN` and
`SLACK_BOT_TOKEN`:

```sh
kubectl --context unsup-nonprod-engineer \
  -n agent-nonprod-bot get secret/nonprod-bot-slack \
  -o jsonpath='{range $k,$v := .data}{$k}{"\n"}{end}'
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

The logs must show Slack bot authentication and a Socket Mode connection
without printing token values.

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
backup="$(mktemp)"
cp dev/nonprod-bot/app.js "$backup"
trap 'cp "$backup" dev/nonprod-bot/app.js; rm -f "$backup"' EXIT
cp ../link-operator/dev/nonprod/slack-verify-app.mjs \
  dev/nonprod-bot/app.js
"$HOME/.local/bin/slack" run \
  --app local \
  --team "$SLACK_CLI_TEAM_ID" \
  --force
cp "$backup" dev/nonprod-bot/app.js
rm -f "$backup"
trap - EXIT
```

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
