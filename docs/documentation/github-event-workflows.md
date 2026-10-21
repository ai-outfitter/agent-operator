# GitHub event workflows

GitHub notification polling and signed webhook delivery are separate intake
modes. Both feed resident tasks; a webhook task carries the actual event and
action rather than a notification subscription reason.

The workflow contract is independent of intake mode: a new assignment starts
implementation, an addressed question starts a response, and a review request
starts a review. A response can lead to implementation when the addressed
request explicitly asks for that work. A standing assignment is context, not
evidence that each later comment requests another implementation run.

Polling adapters must distinguish newly observed assignments from subsequent
thread activity before selecting a workflow. GitHub's notification reason
`assign` alone does not make that distinction. The existing Channels poller
still needs that reconciliation; retaining polling support does not mean it
already has semantic parity with webhook routing. Select one GitHub intake
mode per resident to avoid concurrent dispatch through both paths.

| GitHub event | Resident workflow |
| --- | --- |
| `issues.assigned` | The changed assignee implements the issue as a draft PR. |
| `issues.opened` | Mentioned agents respond; implementation starts from `issues.assigned`. |
| New comment mentioning an agent | Respond to the addressed request; a question alone does not request implementation. |
| New reply on an agent-authored issue or PR | Respond to the thread. |
| `pull_request.review_requested` | The requested reviewer reviews the PR. |
| Submitted change-request review | The PR author revises the existing PR. |
| Review questions and inline thread replies | The PR author responds on the existing PR. |

Self-directed events are not forwarded. Ordinary comments on another author's
issue do not inherit the issue's standing assignment as an implementation
request. Issue edits and PR synchronization do not become new assignments.

## Configuration

The Organization configures its own gateway and durable delivery spool:

```yaml
spec:
  forge:
    provider: github
    owner: Unsupervisedcom
    serverURL: https://github.com
    spool:
      size: 1Gi
      storageClassName: gp3-encrypted-csi
```

Its organization credential Secret needs `FORGE_WEBHOOK_SECRET`. Expose the
gateway Service through a deployment-owned HTTPS Ingress, with the path
`/webhooks/github`. Subscribe the organization or repository webhook to
`issues`, `issue_comment`, `pull_request`, `pull_request_review`, and
`pull_request_review_comment`. GitHub App webhook delivery can use the same
signed endpoint when the App is installed on the intended repositories.

Each routed resident declares its GitHub login and selects webhook intake:

```yaml
spec:
  forge:
    username: luce-unsup
  github:
    mode: webhook
  channels: [slack]
```

Webhook mode takes precedence over legacy `OUTFITTER_CHANNELS` projections.
Only the explicitly selected channels start; when none are selected, channel
sources are off. A2A task delivery remains available. Including the `github`
polling source in webhook mode is an error. Other deployments may retain
`github.mode: polling` and `channels: [github]`; Channels polling is not removed.

## Delivery and recovery

The gateway verifies HMAC-SHA256 and organization scope, then atomically records
the provider delivery and its recipient tasks in SQLite before returning 202.
Redeliveries reuse the same delivery ID. A2A retries reuse the stable message ID
`github:<organization>:<delivery>:<recipient>`. Delivery acknowledgment means the
resident accepted the task, not that it completed the workflow.

The organization `forge-spool` PVC and the residents' `agent-home` PVCs contain
different state. Preserve both during storage changes. The gateway spool holds
delivery and retry history; resident storage holds task acceptance, execution,
and session history. Restoring just one cannot prove that previously published
work will not be repeated. Reconcile pending deliveries and existing PRs before
resuming after an incomplete restore. Assignment routing uses the webhook's
changed `assignee`, not a comparison against an empty assignment cache.

## Cutover verification

Prepare the gateway, route credentials, and HTTPS ingress before enabling the
GitHub webhook. Verify signed event routing with an isolated recipient. At
cutover, select webhook mode and confirm each resident's effective channel list
excludes GitHub polling. Then enable the webhook and confirm a GitHub delivery
receipt, durable A2A acceptance, and the intended resident workflow. Preserve
the prior configuration and spool for rollback; do not clear task databases.

Local regression commands belong in [CONTRIBUTING](../../CONTRIBUTING.md).
