# Use case: Slack responder

An example composition over the Link Operator [primitives](../architecture.md)
that swaps the **channel** while reusing the same workspace, credential exposure,
catalog, and delegation primitives as the
[researcher wiki maintainer](usecases.researcher-wiki-maintainer.md). Here Slack
is the channel: the agent watches the channels its bot is invited to, replies in
the message's thread, and marks each message handled with a reaction.

Because the operator is channel-agnostic (see [architecture](../architecture.md)),
this composition adds **no operator, controller, or CRD changes** — only a `slack`
skill that drives the Slack Web API, and a scoped credential Secret.

Follow the [quick start](quick-start.md) first to stand up the cluster, an
organization, and the agent's workspace.

## What it needs

Beyond the operator and cluster prerequisites, this composition needs:

- a **Slack app** installed to your workspace with a scoped **bot** token; and
- credentials for the model the agent selects.

## Credentials

The operator exposes referenced Secrets/ConfigMaps by name without inspecting
them; the keys inside are *this composition's* contract, defined here.

### Scoped Slack app (bot token, least privilege)

Create a Slack app and install it with a **bot** token (`xoxb-…`) — **not** a user
token. This is the security boundary:

> **Why a bot token, not a user token.** A user token (`xoxp-…`) acts *as a
> person*: it can read and post anywhere that user can, across every channel and
> DM they belong to. A bot token is confined to the channels the bot is
> explicitly **invited to** and to the scopes you grant it — so its blast radius
> is exactly the channels you add it to, nothing else. Invite the bot only to the
> channels it should answer in.

Grant the app **only** these bot scopes (Slack API → *OAuth & Permissions*):

```
channels:history     # read messages in public channels the bot is in
groups:history       # (only if it must answer in private channels)
chat:write           # post replies
reactions:read       # see whether a message is already handled
reactions:write      # mark a message handled with a reaction
```

Do **not** grant destructive or broad scopes (`chat:write.customize`,
`channels:manage`, admin scopes, or any user scope). The agent never deletes
messages or edits others' content, so nothing it holds should permit that.

Then **invite the bot to the specific channels** it should watch
(`/invite @your-bot` in each), and note their channel ids.

The `slack` skill reads this `slack.env` contract:

```dotenv
SLACK_BOT_TOKEN=xoxb-REPLACE_ME
SLACK_CHANNEL_IDS=C0123ABCD C0456EFGH
LINK_SLACK_DONE_EMOJI=white_check_mark
```

Create the Secret:

```sh
kubectl -n agent-responder create secret generic \
  responder-slack \
  --from-env-file=slack.env
```

Reference it from the Agent's `credentials` as `as: env`. Rotate the token from
the app's *OAuth & Permissions* page if it is ever exposed; a leaked bot token
exposes only the channels the bot was invited to, and only the scopes above.

> **Receiving without a public endpoint.** This skill **polls**
> `conversations.history` on the bot's channels, so it needs no inbound ingress
> and no Events-API/Socket-Mode listener. If you prefer push delivery instead of
> polling, Slack's event-driven path (Socket Mode with an app-level `xapp-` token,
> or an Events-API webhook) fits the operator's webhook-driven channel model — a
> separate composition, not required here.

## Respond to a message

Post a message (or `@mention` the bot) in one of the watched channels. On its next
poll the agent will:

1. see the message has no handled-reaction from the bot yet;
2. read the request and do the work needed to answer it well;
3. run the selected Dotagents agent through Outfitter and Pi;
4. reply in the message's thread; and
5. add the `$LINK_SLACK_DONE_EMOJI` reaction, which is the server-side
   handled-tracking state — reply first, then react, so a crash mid-run simply
   reprocesses the message.

Handled-tracking state lives entirely in Slack (the bot's reaction); there is no
local state file, so the agent survives workspace-volume loss.
