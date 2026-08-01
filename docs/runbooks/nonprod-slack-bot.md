# Run the Slack bot as a nonprod agent

This runbook deploys the `nonprod-bot` Slack responder as a controller-managed
Agent Operator `Agent` in the Unsupervised nonprod Kubernetes cluster. Agent
Operator recreates its single runtime pod after failure or restart; this
runbook does not provide an availability SLO. Slack Socket Mode requires
outbound TLS and no inbound ingress. The runtime also needs DNS and outbound
access to the configured model provider and the read-only Grafana MCP endpoint.

This runbook is limited to the following resources:

- Kubernetes context: `unsup-nonprod-engineer`, passed on every command.
- Agent Operator namespace: `agent-operator-system`.
- Agent namespace: `agent-nonprod-bot`, created and owned by Agent Operator.
- Slack channel scope: every channel the bot has joined
  (`SLACK_CHANNEL_IDS=joined`). Inviting it to another channel expands that
  scope without a Kubernetes change.
- Credentials: the commands pass Slack, relay, and Grafana credentials to
  `kubectl` without intentionally printing them. Keep shell tracing disabled.
- Deployment images: linux/amd64 and pinned by immutable digest.

This repository was renamed from `link-operator` to
[`agent-operator`](https://github.com/ai-outfitter/agent-operator), and every
identifier in this repository now uses the `agent-operator` naming: the
`agent-operator-system` namespace, the `agent-operator`/`agent-runtime` image
names, and `agent-*` labels and deployment names. The nonprod cluster deployed
before the rename still runs the legacy `link-*` equivalents
(`link-operator-system`, `ghcr.io/ai-outfitter/link-agent`, `link-*` labels);
commands in this runbook target a fresh deployment with the new names — when
operating the pre-rename deployment, substitute the legacy identifiers, and
retire it by redeploying with this procedure.

The operator manifests live in [`dev/nonprod`](../../dev/nonprod). The agent
composition — profile, `slack-grafana-responder` skill, MCP declaration, and
deployment manifests — lives in the internal
[`Unsupervisedcom/.agents`](https://github.com/Unsupervisedcom/.agents)
catalog, consumed by immutable commit from `unsupervised-main`. This repository
carries no Unsupervised-specific agent runtime content (see
[issue #13](https://github.com/ai-outfitter/agent-operator/issues/13)).

## Prerequisites

You need:

- `kubectl` access through the `unsup-nonprod-engineer` context;
- Docker Buildx with linux/amd64 support for the local pre-publish test;
- authenticated `gh` access to this repository and its Actions runs;
- the `channels` and `agent-operator` repositories checked out as sibling
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
gh auth status
test -s "$HOME/.pi/agent/auth.json"
```

Confirm that deployment commands target `unsup-nonprod-engineer` and only the
gateway Secret read targets `unsup-prod-admin`. Do not substitute other
contexts. The Pi check confirms only that the auth file exists; successful
authentication is verified when the agent starts.

## Build and publish immutable images through GitHub

Images are published only by GitHub Actions. Do not publish Link images to ECR
and do not deploy `latest`, `main`, or another mutable reference.

This repository's release workflow,
[release-images.yml](../../.github/workflows/release-images.yml), publishes the
operator image and the generic agent runtime image. The nonprod bot's agent
image is owned by the consuming deployment: it is built from the
`Unsupervisedcom/.agents` catalog and published by that pipeline, then selected
on the `Agent` resource via `spec.image` (added in
[PR #14](https://github.com/ai-outfitter/agent-operator/pull/14)). Follow the build,
test, and publication procedure in the internal catalog's documentation to
obtain `AGENT_RUNTIME_TAG`.

Resolve the published agent image and released operator image to registry
digests. The resulting values must contain `@sha256:`:

```sh
export AGENT_OPERATOR_TAG="ghcr.io/ai-outfitter/agent-operator:<release-tag>"
export AGENT_OPERATOR_IMAGE="$(
  docker buildx imagetools inspect \
    "$AGENT_OPERATOR_TAG" \
    --format '{{json .Manifest.Digest}}' | tr -d '"'
)"
export AGENT_RUNTIME_IMAGE="$(
  docker buildx imagetools inspect \
    "$AGENT_RUNTIME_TAG" \
    --format '{{json .Manifest.Digest}}' | tr -d '"'
)"
export AGENT_OPERATOR_IMAGE="ghcr.io/ai-outfitter/agent-operator@$AGENT_OPERATOR_IMAGE"
export AGENT_RUNTIME_IMAGE="ghcr.io/ai-outfitter/agent-runtime@$AGENT_RUNTIME_IMAGE"
```

## Install Agent Operator

Render the nonprod kustomization, replace only its two zero-digest image
placeholders in the rendered output, and perform a server-side dry run before
applying it. Never edit the committed placeholders in place.

```sh
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
kubectl kustomize dev/nonprod | sed \
  -e "s#example.invalid/agent-operator@sha256:[0-9a-f]*#$AGENT_OPERATOR_IMAGE#" \
  -e "s#example.invalid/agent-runtime@sha256:[0-9a-f]*#$AGENT_RUNTIME_IMAGE#" \
  >"$work/operator.yaml"
test "$(grep -c 'example.invalid' "$work/operator.yaml")" -eq 0
kubectl --context unsup-nonprod-engineer create namespace \
  agent-operator-system --dry-run=client -o yaml | \
  kubectl --context unsup-nonprod-engineer apply -f -
kubectl --context unsup-nonprod-engineer label namespace \
  agent-operator-system app.kubernetes.io/managed-by=agent-operator --overwrite
kubectl --context unsup-nonprod-engineer apply \
  --dry-run=server -f "$work/operator.yaml"
kubectl --context unsup-nonprod-engineer apply -f "$work/operator.yaml"
kubectl --context unsup-nonprod-engineer \
  -n agent-operator-system rollout status \
  deployment/agent-operator-controller-manager --timeout=5m
kubectl --context unsup-nonprod-engineer \
  api-resources --api-group=aioutfitter.com
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

Render the seeder with `$AGENT_RUNTIME_IMAGE`, then apply it. After the seeder pod
is ready, review and copy the entire local `$HOME/.pi` tree into the agent
workspace volume. This includes credentials and any other Pi state in that
directory:

```sh
sed \
  "s#example.invalid/agent-runtime@sha256:[0-9a-f]*#$AGENT_RUNTIME_IMAGE#" \
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
  --from-literal=AGENT_PI_CONFIG_READY=true \
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
ignored Slack CLI launcher with the credential-sync helper and run it:

```sh
export SLACK_CLI_TEAM_ID=T7GCW93AA
cp ../agent-operator/dev/nonprod/slack-secret-sync.mjs \
  dev/nonprod-bot/app.js
(
  cd dev/nonprod-bot
  AGENT_KUBE_CONTEXT=unsup-nonprod-engineer \
  AGENT_NAMESPACE=agent-nonprod-bot \
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

## Create scoped relay credentials

The relay is a loopback-only debugging channel hosted in the same Pi process as
Slack. It has no Service or ingress; an operator reaches it only through an
authorized `kubectl port-forward`.

Create independent agent and operator bearer tokens without printing them:

```sh
agent_relay_token="$(openssl rand -hex 32)"
operator_relay_token="$(openssl rand -hex 32)"
kubectl --context unsup-nonprod-engineer \
  -n agent-nonprod-bot create secret generic nonprod-bot-relay \
  --from-literal=AGENT_RELAY_TOKEN="$agent_relay_token" \
  --from-literal=AGENT_RELAY_OPERATOR_TOKEN="$operator_relay_token" \
  --dry-run=client -o yaml | \
  kubectl --context unsup-nonprod-engineer apply -f -
unset agent_relay_token operator_relay_token
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

The logs must show both `slack` and `agent` Channels sources, the relay profile,
Slack bot authentication, a Socket Mode connection, and the Grafana MCP server
connected without printing credential values.

To debug the same Slack-based agent directly, keep the relay port-forward open:

```sh
kubectl --context unsup-nonprod-engineer \
  -n agent-nonprod-bot port-forward deployment/agent-runtime 8787:8787
```

In another shell, run the relay smoke client from the internal
`Unsupervisedcom/.agents` catalog checkout, authenticating with the operator
token (read it into the environment, never into command arguments):

```sh
AGENT_RELAY_TOKEN="$(
  kubectl --context unsup-nonprod-engineer \
    -n agent-nonprod-bot get secret/nonprod-bot-relay \
    -o jsonpath='{.data.AGENT_RELAY_OPERATOR_TOKEN}' | base64 --decode
)" node relay-smoke.mjs \
  "Use Grafana MCP to list the configured datasources and report the tool result."
```

This is an additional debugging ingress. Slack remains active in the same Pi
session throughout the relay exchange.

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
cp ../agent-operator/dev/nonprod/slack-verify-app.mjs \
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
    get pod -l app.kubernetes.io/name=agent-runtime \
    -o jsonpath='{.items[0].metadata.uid}'
)"
kubectl --context unsup-nonprod-engineer -n agent-nonprod-bot \
  rollout restart deployment/agent-runtime
kubectl --context unsup-nonprod-engineer -n agent-nonprod-bot \
  rollout status deployment/agent-runtime --timeout=5m
after="$(
  kubectl --context unsup-nonprod-engineer -n agent-nonprod-bot \
    get pod -l app.kubernetes.io/name=agent-runtime \
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

Removing Agent Operator itself is a separate cluster-wide decision because
future agents may share it. Do not delete its CRDs or namespace as part of bot
rotation or ordinary bot teardown.

## Current nonprod deployment

The agent image was updated on 2026-07-28 to the GitHub-published
[`ghcr.io/ai-outfitter/link-agent`](https://github.com/ai-outfitter/agent-operator/pkgs/container/link-agent)
image (the pre-rename package name; new publications use `agent-runtime`) from
[this publication run](https://github.com/ai-outfitter/agent-operator/actions/runs/30637837298).
Read the exact deployed digest from the cluster rather than from this document:

```sh
kubectl --context unsup-nonprod-engineer -n agent-nonprod-bot \
  get deployment/agent-runtime \
  -o jsonpath='{.spec.template.spec.containers[?(@.name=="agent")].image}'
```

The operator remains on the deployment established 2026-07-25 from commit
[`d5ffe5d`](https://github.com/ai-outfitter/agent-operator/commit/d5ffe5dd05c10796f6793493aab06740d0fc32ff);
migrating it to an immutable GHCR digest and retiring the remaining ECR
references is tracked in
[issue #12](https://github.com/ai-outfitter/agent-operator/issues/12).

Migration history: the deployed agent image was built from this repository's
`dev/nonprod` composition, since moved to the internal `Unsupervisedcom/.agents`
catalog. The next agent image rollout should come from that catalog's pipeline
and be selected with `Agent.spec.image`.

Agent Operator reported `Agent/nonprod-bot` Ready, both encrypted CSI volumes
bound, and the replacement pod established a new Socket Mode connection after a
Deployment restart. Record the Slack round-trip verifier result here after the
first human mention passes.
