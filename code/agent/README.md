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

The container is **an agent**: a resident Pi session with a generic recurring
prompt loop. Three pieces:

- **`entrypoint.sh` is tiny** — it starts `outfitter run` in pi's headless RPC
  mode, bootstraps `/loop`, and keeps stdin open. `outfitter run` composes the
  selected loadout and projects its pinned extensions to Pi.
- **JMAP setup is a user-declared setup step** (`mail-bootstrap` in the Agent
  CR): it ensures the `Processed` mailbox exists and waits for JMAP connectivity
  using the image's `xin`. The agent starts after the setup init container
  completes, so init completion is the readiness gate.
- **Scheduling is generic; behavior comes from the loadout.** The researcher
  selects the prebuilt `@pi-agents/loop` extension and the
  [`mail` skill](agents-catalog/skills/mail/SKILL.md). Each wakeup asks the agent
  to survey its inputs; the researcher identity and mail skill determine that
  this means read → reply → move-to-Processed. Neither the entrypoint nor the
  loop extension contains JMAP or mailbox logic.

pi generates the actual reply content (the M2 "real research result"); the old
canned acknowledgement is gone. The `agents-catalog/` tree is baked into the
image at `/opt/link/.agents`; the operator supplies `default_agent`/harness via
the mounted `settings.yml`. (TODO: move the catalog to the Organization's remote
Outfitter catalog once runtime egress allows.)

## Image

Component packages are exposed by `flake.nix`; Outfitter and its bundled pi come
from the locked `ai-outfitter/outfitter` `main` flake, while `xin` and the pinned
generic loop extension are built by `nix/xin.nix` and `nix/pi-loop.nix`.
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

- `LINK_AGENT_LOOP_INTERVAL` (default `10m`) — fixed recurring wakeup cadence.
  The Pi loop uses minute-granularity cron scheduling; the M1 demo overrides it
  to `1m`.
- `LINK_MAIL_PROCESSED` (default `Processed`) — target mailbox for processed mail
  (supplied by the demo runtime ConfigMap to both containers).

The loop package is built by Nix into Outfitter's extension-cache layout. The
runtime sets `PI_OFFLINE=1` and launches Outfitter in strict mode, so it never
installs npm packages during startup and fails if the selected extension is not
present in the image.

For local development, `devenv tasks run demo:m1` copies the developer's entire
`$HOME/.pi` directory directly into `/workspace/.pi` on the agent PVC before it
creates the ConfigMap that unblocks the Deployment. The `.pi` contents are never
committed, baked into the image, or stored in the Kubernetes API.
