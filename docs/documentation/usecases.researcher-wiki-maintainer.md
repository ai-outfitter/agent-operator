# Use case: researcher wiki maintainer

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
- an IMAP/SMTP mailbox for the agent (the local environment supplies GreenMail);
  and
- credentials for the model the `researcher` agent selects.

## Credentials

The operator exposes referenced Secrets/ConfigMaps by name without inspecting them
(quick start §4); the keys inside are *this composition's* contract, defined here.
Create these Secrets in the `agent-researcher` namespace — via your cluster's
secret manager in production, or ignored `0600` files loaded with `kubectl` for
local development.

The email channel adapter's `email.env` must contain — this is the adapter's
authoritative key contract:

```dotenv
address=researcher@link.test
imapHost=greenmail
imapPort=3143
imapUsername=researcher@link.test
imapPassword=REPLACE_ME
imapTLS=false
smtpHost=greenmail
smtpPort=3025
smtpUsername=researcher@link.test
smtpPassword=REPLACE_ME
smtpTLS=false
```

The cleartext TLS settings are permitted only inside the isolated GreenMail demo.
Use TLS for a real mailbox.

Create the email Secret:

```sh
kubectl -n agent-researcher create secret generic \
  researcher-email \
  --from-env-file=email.env
```

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

1. receive and deduplicate the message by Message-ID;
2. clone the organization's wiki into storage it manages in its namespace
   workspace;
3. run the selected Dotagents agent through Outfitter and Pi;
4. preserve the PDF under `wiki/sources/` using Git LFS;
5. extract `content.md` with Docling and add a source note;
6. update concepts, the wiki index, and the append-only log;
7. list linked papers to explore next without downloading them;
8. create one local Git commit; and
9. reply in the original email thread with its summary and commit SHA.

Today the agent does not push the commit, and research traversal is limited to the
emailed seed paper; future recursive research has a hard maximum depth of five.
