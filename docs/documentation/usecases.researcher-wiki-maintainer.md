# Use case: researcher wiki maintainer

This is the target composition for
[M2: Email Paper Research](../milestones/M2-email-paper-research/task.md).

An example composition over the Link Operator [primitives](../architecture.md): an
agent that watches an email inbox, ingests emailed research papers into an
organization's wiki, and replies in-thread with a source-traceable commit. Email
is its **channel**; the `wiki` and `source-ingest` skills are its **tools**. A
different composition would swap the channel (GitHub notifications, Signal) or the
tools while reusing the same workspace, secret/config exposure, catalog, and
delegation primitives.

Follow the [quick start](quick-start.md) first to stand up the cluster, an
organization, and the `researcher` agent's workspace. This page covers what the
researcher composition adds on top.

## What it needs

Beyond the operator and cluster prerequisites, this composition needs:

- a writable Git wiki repository with Git LFS enabled — the organization's `wiki`
  repository, which the agent commits to;
- a mailbox for the agent (see *Choosing a mailbox backend* below); and
- credentials for the model the `researcher` agent selects.

### Choosing a mailbox backend

Email is the researcher's **channel**, and the channel is an agent-runtime concern
the operator never touches (see [architecture](../architecture.md)). That makes
the mailbox backend a swappable choice — the operator projects whatever credential
Secret you name, and a mailbox-specific skill drives an existing CLI against it.
Two backends are documented here; both preserve the same invariants (reply-then-
move, no local state, state lives server-side) and the same downstream behavior in
*Email a paper*:

| Backend | Protocol / CLI | Skill | When to use |
| --- | --- | --- | --- |
| **Stalwart (JMAP)** | JMAP via `xin` | `mail` | The self-contained local demo; any JMAP mailbox. Offline, no internet egress. |
| **Google Workspace (Gmail)** | Gmail API via GAMADV-XTD3 (`gam`) | `mail-gmail` | Your organization already runs Google Workspace. |

Pick one; the two are parallel, not layered. The rest of this page covers each
backend's credential contract in turn.

## Credentials

The operator exposes referenced Secrets/ConfigMaps by name without inspecting them
(quick start §4); the keys inside are *this composition's* contract, defined here.
Create these Secrets in the `agent-researcher` namespace — via your cluster's
secret manager in production, or ignored `0600` files loaded with `kubectl` for
local development.

The mailbox credentials go in the `researcher-email` Secret. Its keys depend on
which backend you chose above.

#### Stalwart (JMAP) mailbox

The `mail` skill and `xin` CLI use this `email.env` contract:

```dotenv
XIN_BASE_URL=http://stalwart.link-system.svc.cluster.local:8080
XIN_BASIC_USER=researcher@link.test
XIN_BASIC_PASS=REPLACE_ME
```

Cleartext HTTP is permitted only inside the isolated Stalwart demo. Use HTTPS for
a real mailbox.

Create the email Secret:

```sh
kubectl -n agent-researcher create secret generic \
  researcher-email \
  --from-env-file=email.env
```

#### Google Workspace (Gmail) mailbox

For a Google Workspace organization the `mail-gmail` skill drives GAMADV-XTD3
(`gam`) against the Gmail API. The agent authenticates as **exactly one mailbox**
using a **per-mailbox OAuth 2.0 token** that the researcher mailbox consents to
once. There is **no service account and no domain-wide delegation**, so the
credential cannot read or send as any other user — it is bound to the one mailbox
that authorized it, and to two narrow scopes.

> **Why not domain-wide delegation.** A DWD service account can impersonate *any*
> user in the domain for its scopes — Google offers no way to bind a DWD grant to
> a single mailbox. That is an org-wide credential in the agent's namespace, which
> we deliberately avoid. A per-mailbox OAuth token is issued to, and valid only
> for, the account that consented.
>
> **Why not IMAP/SMTP.** Gmail's IMAP/SMTP OAuth accepts *only* the full
> `https://mail.google.com/` scope (it rejects granular scopes), which includes
> permanent delete. Staying on the Gmail API lets us keep the restricted
> read/modify + send scopes below — Google's own recommendation.

**One-time setup** — done once, by (or on behalf of) the researcher mailbox; no
super-admin domain settings are changed:

1. In a **dedicated, single-purpose GCP project**, enable the Gmail API and create
   an **OAuth client** (Desktop-app type) → `client_secrets.json`. Set the OAuth
   consent screen's **User type to _Internal_** so only accounts in your Workspace
   org can ever consent to this client.
2. Point `gam` at a scratch config dir holding that `client_secrets.json` and run
   `gam oauth create`. From the scope menu, select **only** Gmail
   **read/modify** and Gmail **send**, deselect everything else, and complete the
   browser consent **signed in as `researcher@yourcompany.com`**. This writes an
   `oauth2.txt` bound to that mailbox with just these scopes:

   ```
   https://www.googleapis.com/auth/gmail.modify   # read bodies + add/remove labels; cannot permanently delete
   https://www.googleapis.com/auth/gmail.send     # post the threaded reply
   ```

   `gmail.modify` is the narrowest scope that still lets the agent read a message
   and move it `INBOX`→`Processed`; it explicitly **cannot delete** mail.

The `mail-gmail` skill reads this `email.env` contract:

```dotenv
GMAIL_USER=researcher@yourcompany.com
GAMCFGDIR=/var/run/secrets/gmail
```

Create the Secret with the env contract plus the two `gam` config files (mounted
as files because `gam` reads them from `$GAMCFGDIR` on disk):

```sh
kubectl -n agent-researcher create secret generic \
  researcher-email \
  --from-env-file=email.env \
  --from-file=client_secrets.json=./client_secrets.json \
  --from-file=oauth2.txt=./oauth2.txt
```

Reference it from the Agent's `credentials` with `as: volume` (so both files land
in `$GAMCFGDIR`) — the operator projects it unchanged. Then swap the agent's skill
from `mail` to `mail-gmail` and replace the `mail-bootstrap` setup step's `xin`
commands with their `gam` equivalents (verify the token works, then `gam user
"$GMAIL_USER" create label "$LINK_MAIL_PROCESSED"`, tolerating "already exists").

Remove the temporary `client_secrets.json` and `oauth2.txt` files once the Secret
exists, and rotate the token by re-running `gam oauth create` if it is ever
exposed. Because the OAuth client is _Internal_ and the token holds only these two
scopes for one mailbox, a leaked token exposes exactly that mailbox's mail — never
the rest of the domain.

Create the model Secret with the environment variable expected by the selected
model provider:

```sh
kubectl -n agent-researcher create secret generic \
  researcher-model \
  --from-env-file=model.env
```

For private wiki or catalog repositories, create the SSH Secret and known-hosts
entry from local files:

```sh
kubectl -n agent-researcher create secret generic \
  researcher-ssh \
  --type=kubernetes.io/ssh-auth \
  --from-file=ssh-privatekey=./id_ed25519 \
  --from-file=known_hosts=./known_hosts
```

Remove the temporary credential files when they are no longer needed. Bootstrap
Secret volumes are mounted read-only. The agent is the administrator of its
namespace workspace and can manage its namespaced Secrets, but cannot access
Secrets in another namespace.

Once these exist, `CredentialsReady` and then `Ready` become true (see quick start
§5).

## Email a paper

Email is this composition's **channel** — the way the researcher receives work.
Send one message to the configured agent address with exactly one PDF attachment
of at most 25 MiB. The email maps to the agent's configured default organization
(delivered as runtime config, not a CRD field).

The agent will:

1. receive the message while it is in `INBOX`;
2. clone the organization's wiki into storage it manages in its namespace
   workspace;
3. run the selected Dotagents agent through Outfitter and Pi;
4. preserve the PDF under `wiki/sources/` using Git LFS;
5. extract `content.md` with Docling and add a source note;
6. update concepts, the wiki index, and the append-only log;
7. list linked papers to explore next without downloading them;
8. create one local Git commit; and
9. reply in the original email thread with its summary and commit SHA; and
10. move the original from `INBOX` to `Processed`, which is the server-side
    reply-tracking state.

Today the agent does not push the commit, and research traversal is limited to the
emailed seed paper; future recursive research has a hard maximum depth of five.
