# Use case: Signal responder

An example composition over the Link Operator [primitives](../architecture.md)
that swaps the **channel** while reusing the same workspace, credential exposure,
catalog, and delegation primitives as the
[researcher wiki maintainer](usecases.researcher-wiki-maintainer.md). Here Signal
is the channel: the agent receives messages sent to its own Signal number and
replies to the sender or group.

Because the operator is channel-agnostic (see [architecture](../architecture.md)),
this composition adds **no operator, controller, or CRD changes** — only a
`signal-responder` skill (from the
[`ai-outfitter/community-profiles`](https://github.com/ai-outfitter/community-profiles)
catalog your `Organization` pins) that drives the mature `signal-cli`, and a
scoped credential Secret.

Follow the [quick start](quick-start.md) first to stand up the cluster, an
organization, and the agent's workspace.

## What it needs

Beyond the operator and cluster prerequisites, this composition needs:

- a **dedicated Signal number** for the agent, provisioned as a `signal-cli`
  account (see below); and
- credentials for the model the agent selects.

## Credentials

The operator exposes referenced Secrets/ConfigMaps by name without inspecting
them; the keys inside are *this composition's* contract, defined here.

### Dedicated, revocable Signal identity (least privilege)

The agent authenticates as **its own dedicated Signal number** — never a personal
account. Two ways to provision it with `signal-cli`, in order of preference:

1. **Link as a secondary device (recommended).** Register the number on a phone
   you control, then link the agent as an additional device
   (`signal-cli link -n "link-agent"`) and approve it from the phone. The master
   identity key stays on the phone; the agent holds only a **device key that you
   can revoke at any time** from *Settings → Linked devices*. This is the security
   boundary: compromise of the agent's namespace exposes one revocable device on
   one dedicated number, not a durable identity.
2. **Register the number directly** (`signal-cli -a "$SIGNAL_NUMBER" register`,
   then `verify`) if the number is the agent's alone. Revocation then means
   re-registering the number, which is coarser than unlinking a device.

Either way the number is **single-purpose** — used only by the agent — so its
reachable surface is exactly the people and groups that message it.

`signal-cli` stores identity and session state in a **data directory**. Capture
that directory after provisioning and mount it from a Secret. The `signal-responder` skill
reads this `signal.env` contract plus the data directory:

```dotenv
SIGNAL_NUMBER=+15550100
SIGNAL_CLI_CONFIG=/var/run/secrets/signal
```

Create the Secret with the env contract and the `signal-cli` data directory
(mounted as files because `signal-cli` reads them from disk):

```sh
kubectl -n agent-responder create secret generic \
  responder-signal \
  --from-env-file=signal.env \
  --from-file=./signal-cli-data/
```

Reference it from the Agent's `credentials` with `as: volume` so the data lands
at `$SIGNAL_CLI_CONFIG`. If the credential is ever exposed, **unlink the device**
(or re-register the number) — that immediately revokes the agent's access without
affecting any other account.

## Respond to a message

Send a Signal message (direct, or in a group the agent's number is in). On its
next cycle the agent will:

1. `receive` the pending messages (Signal delivers each **once**);
2. read the request and do the work needed to answer it well;
3. run the selected Dotagents agent through Outfitter and Pi; and
4. reply to the same conversation — the sender for a direct message, or back into
   the group.

> **Delivery is at-most-once — a real difference from email.** Signal has no
> server-side "processed" folder; a received message is drained from the server
> queue on delivery. The skill therefore answers every message in a received batch
> before receiving the next one, keeping the receive→reply window small. There is
> no local state file, so the identity survives workspace-volume loss, but a crash
> in that narrow window can drop a message (unlike the reply-first-then-move
> idempotency of the mail skill).
