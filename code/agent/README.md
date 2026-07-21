# Link agent runtime

`link-agent` is the persistent, composition-side runtime process. It is not part
of the operator controller. The current slice discovers the configured JMAP
account, tracks the Inbox with `Email/query` followed by incremental
`Email/queryChanges`, records each Message-ID sequentially on the agent PVC, and
submits a threaded acknowledgement through JMAP.

The image also contains the pinned Outfitter and Pi binaries. Processing a
received work item through Outfitter/Pi and replacing the acknowledgement with
the research result are the next M1 composition steps; the mail loop deliberately
does not put email behavior into the controller.

## Runtime contract

Required environment variables:

- `JMAP_URL`
- `JMAP_USERNAME`
- `JMAP_PASSWORD`

Optional variables:

- `LINK_MAIL_POLL_INTERVAL` (default `5s`, minimum `1s`)
- `LINK_WORKSPACE` (default `/workspace`)
- `LINK_MAIL_STATE` (default `/workspace/.link/mail-state.json`)
- `LINK_MAIL_READY` (default `/workspace/.link/mail-loop-ready`)

The state file is replaced atomically with mode `0600` after every receive and
reply transition. A repeated Message-ID is not recorded or replied to twice,
even if Stalwart assigns a different JMAP object ID.

For local development, `devenv tasks run demo:mail-loop` copies the developer's
entire `$HOME/.pi` directory directly into `/workspace/.pi` on the agent PVC
before it creates the ConfigMap that unblocks the Deployment. The `.pi` contents
are never committed, baked into the image, or stored in the Kubernetes API.
