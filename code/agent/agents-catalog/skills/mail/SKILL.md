---
name: mail
description: Process the researcher agent's INBOX with the xin JMAP CLI — read each new message, send a genuine threaded reply, then move the original into the Processed mailbox. Reply-tracking state lives server-side in Stalwart (mailbox membership), never on local disk.
allowed-tools: Bash(xin:*), Bash(jq:*)
---

# Mail processing

You handle inbound mail for the researcher agent over JMAP using the `xin` CLI.
`xin` emits **stable JSON by default** — always parse the JSON, never `--plain`.

Connection and credentials come from the environment (`XIN_BASE_URL`,
`XIN_BASIC_USER`, `XIN_BASIC_PASS`); you never configure accounts yourself.
The target mailbox for processed mail is in `$LINK_MAIL_PROCESSED` (default
`Processed`) and is guaranteed to exist before you run.

## State model — read this first

There is **no local state file**. A message is "unprocessed" if and only if it
is still in **INBOX**. You mark a message done by **moving it out of INBOX into
the Processed mailbox**. This is idempotent and survives restarts: if you crash
after replying but before moving, the message is simply reprocessed. Therefore:
**reply first, then move.** Never move a message you have not replied to.

## Loop

Repeat until INBOX is empty:

1. **List unprocessed mail** (oldest first is fine):

   ```bash
   xin messages search "in:inbox" --max 200 | jq -r '.data.items[].emailId'
   ```

   If `.data.items` is empty, there is nothing to do — end the turn.

2. **Read one message** to understand what is being asked:

   ```bash
   xin get <emailId> --format full
   ```

3. **Compose and send a genuine reply.** Write a real, useful response to the
   sender's request (not a canned acknowledgement). `xin reply` sets the
   threading headers (`In-Reply-To`, `References`) and a `Re:` subject for you:

   ```bash
   xin reply <emailId> --text "…your reply…"
   # long bodies: xin reply <emailId> --text @/workspace/reply.txt
   ```

   Confirm the returned JSON has `"ok": true` before moving on. If it is
   `"ok": false`, do **not** move the message; surface the error and stop.

4. **Move the original out of INBOX** to mark it processed:

   ```bash
   xin batch modify <emailId> --remove inbox --add "$LINK_MAIL_PROCESSED"
   ```

5. Go back to step 1.

## Rules

- Reply exactly once per message; the move is what prevents duplicates.
- Only ever `--remove inbox --add "$LINK_MAIL_PROCESSED"`; do not delete mail.
- Preview a risky change with `--dry-run` first if unsure.
- Discover flags with `xin <command> --help`.
