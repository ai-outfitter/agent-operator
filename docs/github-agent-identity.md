# GitHub agent identity and authorization

Decision recorded 2026-10-22. This is the intended architecture; gateway-managed
GitHub App user authorization and refresh remain to be implemented.

## Resident sign-in

Luce and Vega have their own GitHub accounts. Each resident should sign in using
Chrome MCP and its authorized password manager through its persistent Chromium
profile at `$HOME/.config/chromium`. A human can assist with consent, MFA, and
bootstrap. Manually copying PATs between residents is not the intended lifecycle.
Browser sign-in and API authorization are distinct; the browser completes the
appropriate authorization flow for API tools.

## Grants and identity

| Grant | What it permits |
| --- | --- |
| App installation | Approved permissions on selected organization or personal repositories/resources. |
| Individual App authorization | Acting on that user's behalf; each agent account authorizes separately. |

Installation does not permit impersonation of organization members. An App user
access token attributes actions to the authorizing user and records the App's
involvement. An installation access token acts as the App bot. Never substitute
Nicholas's account or the App bot when the intended author is Luce or Vega.
Effective access is the intersection of user and App access. Successful identity
lookup does not establish repository write access.

See [GitHub user access tokens](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-user-access-token-for-a-github-app).

## Intended authorization lifecycle (not implemented)

1. The resident signs into its own account through Chrome MCP/password manager
   and authorizes the App. Device flow is an alternative when enabled for the App.
2. The authorization backend (the service managing App grants for the gateway)
   validates OAuth state and exchanges the temporary callback code;
   use PKCE for browser authorization. Bind the grant to the verified GitHub user
   ID and resident, not a requested username.
3. The gateway uses the resident's user grant for outbound operations, enforcing
   resident and repository bindings. Store credentials securely in the backend.
   Declarative Agent configuration references identities and credential bindings,
   not raw passwords or tokens.
4. Background work does not require an active browser session. With expiration
   enabled, access tokens last eight hours and refresh tokens six months.
   Refresh invalidates the previous pair. Serialize refresh per grant and
   securely persist both replacements together before concurrent use.
5. Revocation or refresh expiration requires browser reauthorization. Stop using
   revoked grants and surface an actionable sign-in task without changing identity.

See [GitHub token refresh](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/refreshing-user-access-tokens).

## Event intake and workflow intent

Polling and webhooks are separate intake methods. Both should preserve workflow
intent: assignment starts implementation; a mention starts a response to the
addressed question or request; a review request starts review. Implementation
requires assignment or an explicit implementation request in the addressed
comment. App webhook intake needs no resident credential; notification polling
uses the resident's own credential. Outbound user authorization is separate.

## Current implementation and temporary workaround

The deployed App forwards webhook events through the organization gateway to
residents configured for webhook intake. Outbound `gh` commands use the resident's
`GH_TOKEN`, injected from the `GITHUB_PERSONAL_ACCESS_TOKEN` Secret key. Despite
its legacy name, that key can hold the temporary GitHub CLI OAuth credential.
It does not yet hold a gateway-managed App user grant.

For temporary sign-in, run `gh auth login --web` with `GH_TOKEN` and
`GITHUB_TOKEN` unset. This authorizes GitHub CLI, not the installed Outfitter App.
The cached login does not override the injected credential: securely copy the
resulting credential into the resident's existing Secret key and let the operator
roll out the updated environment. Verify the account identity and target
repository write access before activation, then verify the active resident.
Never record credentials or device codes in documentation.

## Acceptance evidence still required

- Each resident authorizes its own identity through its browser/password manager.
- A real webhook produces a response task and a gateway-mediated reply attributed
  to the agent through the App user grant, without a resident PAT.
- API calls work after closing the browser and after token refresh; revocation
  produces a reauthorization task.
