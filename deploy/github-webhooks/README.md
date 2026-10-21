# GitHub webhook deployment

These patches target the existing Unsupervised and Outfitter organizations.
The identity and organization patches stage authenticated A2A receivers while
leaving the current GitHub intake mode unchanged. Cutover patches are separate.

1. Preserve the current Organizations, Agents, organization credentials, and
   resident home PVCs. The gateway also needs its durable `forge-spool` PVC.
2. Add a randomly generated `FORGE_WEBHOOK_SECRET` to each existing
   `organization-credentials` Secret. Preserve every other key; never commit
   secret values. Reuse this value when registering the matching GitHub hook.
3. Merge-patch each Agent with its `*-identity.patch.json`, then merge-patch
   each Organization with its `*-organization.patch.json`.
4. Apply the two `*-ingress.json` resources. Wait for both gateway Deployments
   and `github-events-tls` certificates to become ready. Verify a signed ping
   returns 204 and an invalid signature returns 401.
5. Check that each gateway credential authenticates a read-only `GET /tasks`
   against its resident A2A server. The controller hashes the transport credential keys into the pod
   template, so credential updates restart the affected consumer automatically.
   Gateway replacements use Recreate to preserve a single SQLite writer.
6. Before webhook cutover, merge-patch each resident with its `*-pi.patch.json`.
   These patches pin the Pi-only image, replace the catalog profile and bootstrap
   with CRD instructions, and preserve existing intake until the next step.
   Verify Pi starts, GitHub CLI authenticates as the expected account, and A2A
   accepts requests.
7. Register each GitHub organization webhook, initially inactive, with its
   ingress URL ending `/webhooks/github`, JSON content, SSL verification on,
   and events `issues`, `issue_comment`, `pull_request`, `pull_request_review`,
   and `pull_request_review_comment`. This needs `admin:org_hook` authorization.
8. Merge-patch each routed Agent with its `*-cutover.patch.json`. Verify the
   effective channel configuration excludes `github`, preserving Slack for
   Unsupervised Luce and Vega. Then enable the corresponding GitHub webhook.
9. Verify a real provider delivery and durable resident acceptance, followed by
   the intended workflow. Signed pings and authenticated reads alone do not
   prove that complete path.

Apply patches with `kubectl patch agent NAME --type=merge --patch-file FILE`
or `kubectl patch organization NAME --type=merge --patch-file FILE`.
Do not apply this directory recursively: patch files are partial resources.

For rollback, deactivate the GitHub hooks before restoring prior Agent intake
configuration. Keep both gateway and resident durable state for reconciliation.
