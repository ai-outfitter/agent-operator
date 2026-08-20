# Link agent runtime

The link agent is the persistent, composition-side runtime process. It is not
part of the operator controller. It watches the researcher account's INBOX and,
for each new message, uses the [`xin`](https://github.com/onevcat/xin) JMAP CLI
to send a genuine threaded reply, then moves the original into a `Processed`
mailbox.

**No client adapters.** We deliberately do not hand-roll a JMAP/mail client in
this repo — the agent drives the existing, well-maintained `xin` CLI. `xin` is
JMAP-native, agent-first (stable JSON by default), authenticates with Basic auth
from the environment, and does threaded replies for us.

**State lives in Stalwart, not on local disk.** A message is "unprocessed" iff
it is still in INBOX. The agent marks a message done by moving it out of INBOX
into the `Processed` mailbox (`xin batch modify … --remove inbox --add
Processed`). This is idempotent and survives pod restarts and PVC loss; there is
no local state file. The agent always replies before moving, so a crash between
the two simply reprocesses the message.

## How it runs

The container is **an agent**: a resident Pi session with push-driven Channels.
Three pieces:

- **`entrypoint.sh` is tiny** — it starts `outfitter run` in pi's headless RPC
  mode, resumes the most recent session from the workspace, and keeps stdin
  open. `outfitter run` composes the selected loadout and projects its pinned
  extensions to Pi.
- **JMAP setup is a user-declared setup step** (`mail-bootstrap` in the Agent
  CR): it ensures the `Processed` mailbox exists and waits for JMAP connectivity
  using the image's `xin`. The agent starts after the setup init container
  completes, so init completion is the readiness gate.
- **Wake transport is generic; behavior comes from the loadout.** The researcher
  selects the commit-pinned Channels extension and the
  [`mail` skill](agents-catalog/skills/mail/SKILL.md). JMAP state changes wake
  the agent; its identity and mail skill determine that the work is read → reply
  → move-to-Processed. The entrypoint contains no JMAP or mailbox logic.

pi generates the actual reply content (the M2 "real research result"); the old
canned acknowledgement is gone. The `agents-catalog/` tree is baked into the
image at `/opt/agent/.agents`; the operator supplies `default_agent`/harness via
the mounted `settings.yml`. (TODO: move the catalog to the Organization's remote
Outfitter catalog once runtime egress allows.)

## Image

Component packages are exposed by `flake.nix`; Outfitter and its bundled pi come
from the locked `ai-outfitter/outfitter` `main` flake, while `xin` and the pinned
Channels extension are built by `nix/xin.nix` and `nix/channels.nix`.
Devenv assembles those packages into the `agent` and `operator` OCI containers
(`devenv container build agent`) and the image tasks copy OCI archives into the
local cluster.

The agent image is a **Nix-enabled container** — it ships the `nix` CLI, and the
operator mounts a persistent `/nix` volume (seeded from the image), so packages
an agent `nix profile install`s survive restarts.

## Runtime contract

Credentials (`xin`-native, supplied via the credential secret to both the
init and agent containers):

- `XIN_BASE_URL`
- `XIN_BASIC_USER`
- `XIN_BASIC_PASS`
- `XIN_TRUST_REDIRECT_HOSTS` (optional) — allowlist the origin Stalwart
  advertises in its JMAP session when it differs from `XIN_BASE_URL` (e.g. behind
  a port-forward).

Optional runtime configuration:

- `OUTFITTER_CHANNELS` — explicit comma-separated channel selection. The mail
  demo sets `jmap`; composed resident agents can set `jmap,agent`. In an
  operator-managed deployment this may be a key in the env-exposed Secret that
  already carries those channels' credentials; a dedicated runtime ConfigMap is
  not required. The operator deliberately does not model this selector as a
  typed `Agent` field.
- `AGENT_RELAY_URL` and `AGENT_RELAY_TOKEN` — authenticated outbound WSS relay
  connection for the native `agent` channel. Supply both through existing
  Secret/ConfigMap credential references; neither belongs in an `Agent` spec.
- `AGENT_MAIL_PROCESSED` (default `Processed`) — target mailbox for processed mail
  (supplied by the demo runtime ConfigMap to both containers).

The operator projects native-channel defaults into every
resident runtime:

- `AGENT_ENDPOINT_ID=link:<agent>` and the matching `AGENT_PRINCIPAL_ID`.
  `Agent` names are cluster-scoped, so this remains unique and within the
  channel protocol's identifier bound across pod replacement;
- `AGENT_SPOOL_PATH=/workspace/.channels/agent`, on the persistent workspace
  volume, for unacknowledged local delivery only.

Pi sessions are stored under `/workspace/.pi/agent/sessions` and the entrypoint
uses `--continue`, so a restarted pod resumes the durable session instead of
minting an unrelated ephemeral session. Native agent-channel messages and replay
state are recorded as Pi session entries, making this session on the workspace
PVC the conversation source of truth. The relay token remains independently
revocable and is never derived from the endpoint ID.

Channels is built by Nix into Outfitter's extension-cache layout. The
runtime sets `PI_OFFLINE=1` and launches Outfitter in strict mode, so it never
installs extensions during startup and fails if the selected extension is not
present in the image. Channel connections are opened without inference; only a
matching event initiates a model turn.

For local development, `devenv tasks run demo:m1` copies the developer's entire
`$HOME/.pi` directory directly into `/workspace/.pi` on the agent PVC before it
creates the ConfigMap that unblocks the Deployment. The `.pi` contents are never
committed, baked into the image, or stored in the Kubernetes API.
