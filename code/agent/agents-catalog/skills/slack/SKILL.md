---
name: slack
description: Respond to Slack messages in the channels the agent's bot is invited to — read new messages, reply in-thread, then mark each handled by adding a ✅ reaction. "Handled" state lives server-side in Slack (the bot's reaction), never on local disk. Drives the Slack Web API with `curl`; no bespoke client.
allowed-tools: Bash(curl:*), Bash(jq:*)
---

# Slack responder

You handle inbound Slack messages for the agent by driving the **Slack Web API**
directly with `curl` (JSON in, JSON out — always parse the JSON, never scrape).
There is no bespoke Slack client; the token and channel list come from the
environment.

Credentials and configuration come from the environment:

- `SLACK_BOT_TOKEN` — a **bot** token (`xoxb-…`). It can only see channels the bot
  has been invited to and can only do what its granted scopes allow. You never
  configure the app yourself.
- `SLACK_CHANNEL_IDS` — space-separated channel ids the bot watches (each a
  channel it has been invited to).
- `LINK_SLACK_DONE_EMOJI` — the reaction that marks a message handled (default
  `white_check_mark`).

Authenticate every call with `-H "Authorization: Bearer $SLACK_BOT_TOKEN"`.
Every Slack response has `"ok": true|false` — check it before continuing.

## State model — read this first

There is **no local state file**. A message is "unhandled" if and only if it does
**not** yet carry the bot's `$LINK_SLACK_DONE_EMOJI` reaction. You mark a message
handled by **adding that reaction**. This is idempotent and survives restarts: if
you crash after replying but before reacting, the message is simply reprocessed.
Therefore: **reply first, then react.** Never react to a message you have not
replied to. (The reaction is the Slack analogue of moving mail out of `INBOX`.)

## Loop

For each channel id in `$SLACK_CHANNEL_IDS`, repeat until no unhandled messages
remain:

1. **List recent messages** in the channel:

   ```bash
   curl -sS -G "https://slack.com/api/conversations.history" \
     -H "Authorization: Bearer $SLACK_BOT_TOKEN" \
     --data-urlencode "channel=$CHANNEL" --data-urlencode "limit=50" \
   | jq -r '.messages[] | select(.subtype == null) | .ts'
   ```

   For each `ts`, skip it if the bot has already reacted: a message is handled
   when `.reactions[]?.name` contains `$LINK_SLACK_DONE_EMOJI` **and** the bot's
   own user id is in that reaction's `.users`. Skip bot/own messages
   (`.bot_id`, or `user` == the bot's own id) so you never answer yourself.

2. **Read the message** (`text`, `user`, `thread_ts`) from the history payload
   above to understand what is being asked.

3. **Reply in-thread.** Post into the message's thread so the conversation stays
   threaded (`thread_ts` = the message's own `ts`, or its existing `thread_ts`):

   ```bash
   curl -sS -X POST "https://slack.com/api/chat.postMessage" \
     -H "Authorization: Bearer $SLACK_BOT_TOKEN" \
     -H "Content-type: application/json; charset=utf-8" \
     --data @- <<JSON
   {"channel":"$CHANNEL","thread_ts":"$TS","text":"…your reply…"}
   JSON
   ```

   Confirm the response has `"ok": true` before moving on. If it is `false`, do
   **not** react; surface the `error` field and stop.

4. **Mark the message handled** by adding the reaction:

   ```bash
   curl -sS -X POST "https://slack.com/api/reactions.add" \
     -H "Authorization: Bearer $SLACK_BOT_TOKEN" \
     -H "Content-type: application/json; charset=utf-8" \
     --data "{\"channel\":\"$CHANNEL\",\"timestamp\":\"$TS\",\"name\":\"$LINK_SLACK_DONE_EMOJI\"}"
   ```

   `already_reacted` is a benign result — treat it as handled.

5. Go back to step 1.

## Rules

- Reply exactly once per message; the reaction is what prevents duplicates.
- Only ever add `$LINK_SLACK_DONE_EMOJI`; never delete messages or remove others'
  reactions. The bot's scopes should not permit destructive actions.
- Never answer your own or another bot's messages.
- Discover more endpoints at `https://api.slack.com/methods`; prefer the narrowest
  method that does the job.
