---
name: mail-gmail
description: Process the researcher agent's Gmail INBOX with the GAMADV-XTD3 (`gam`) CLI — read each new message, send a genuine threaded reply, then move the original out of INBOX by relabelling it Processed. Reply-tracking state lives server-side in Gmail (label membership), never on local disk. Use this instead of the `mail` (JMAP/xin) skill when the mailbox is Google Workspace.
allowed-tools: Bash(gam:*), Bash(jq:*)
---

# Mail processing (Google Workspace / Gmail)

You handle inbound mail for the researcher agent over the Gmail API using
**GAMADV-XTD3** (`gam`). This is the Google Workspace counterpart to the `mail`
skill (which drives `xin` over JMAP) — the state model and loop are identical;
only the CLI and label semantics differ.

Connection and credentials come from the environment. `gam` reads its config —
an OAuth client (`client_secrets.json`) and a **per-mailbox** OAuth token
(`oauth2.txt`) — from the directory in `$GAMCFGDIR`. That token was consented by
the `$GMAIL_USER` mailbox itself and is valid **only** for that one mailbox and
only for the Gmail read/modify + send scopes; there is no service account and no
domain-wide delegation, so these credentials cannot touch any other mailbox. You
never configure accounts yourself. The target label for processed mail is in
`$LINK_MAIL_PROCESSED` (default `Processed`) and is guaranteed to exist before you
run.

Prefer CSV/JSON output and parse it — do not scrape human-formatted text.

## State model — read this first

There is **no local state file**. A message is "unprocessed" if and only if it is
still in **INBOX** (carries the `INBOX` label). You mark a message done by
**removing the `INBOX` label and adding the `$LINK_MAIL_PROCESSED` label** — the
Gmail equivalent of moving out of INBOX. This is idempotent and survives
restarts: if you crash after replying but before relabelling, the message is
simply reprocessed. Therefore: **reply first, then relabel.** Never relabel a
message you have not replied to.

Gmail uses the same `in:inbox` search syntax as the JMAP skill, so the loop below
mirrors the `mail` skill step for step.

## Loop

Repeat until INBOX is empty:

1. **List unprocessed mail.** Ask for message ids as CSV and read the id column:

   ```bash
   gam user "$GMAIL_USER" print messages query "in:inbox" \
     todrive false | tail -n +2 | cut -d, -f1
   ```

   If no ids are returned, there is nothing to do — end the turn.

2. **Read one message** to understand what is being asked:

   ```bash
   gam user "$GMAIL_USER" show message <id> format metadata,full
   ```

3. **Compose and send a genuine threaded reply.** Write a real, useful response to
   the sender's request (not a canned acknowledgement). Send it into the original
   thread so it threads correctly. Look up the sender, subject, `Message-ID`, and
   `threadId` from step 2, then reply in-thread with the threading headers set:

   ```bash
   gam user "$GMAIL_USER" sendemail \
     to "<sender>" \
     subject "Re: <original subject>" \
     replyto "$GMAIL_USER" \
     threadid <threadId> \
     header "In-Reply-To" "<original Message-ID>" \
     header "References" "<original Message-ID>" \
     message @/workspace/reply.txt
   ```

   Confirm `gam` reports the send succeeded (exit 0, no error line) before moving
   on. If the send fails, do **not** relabel the message; surface the error and
   stop.

4. **Move the original out of INBOX** to mark it processed:

   ```bash
   gam user "$GMAIL_USER" modify message <id> \
     removelabel INBOX addlabel "$LINK_MAIL_PROCESSED"
   ```

5. Go back to step 1.

## Rules

- Reply exactly once per message; the relabel is what prevents duplicates.
- Only ever `removelabel INBOX addlabel "$LINK_MAIL_PROCESSED"`; **never delete
  mail** and never `spamtrash`/`trash` a message — the granted scope
  (`gmail.modify`) cannot permanently delete, and you must not try.
- Discover flags with `gam help <command>` / the GAMADV-XTD3 wiki.
