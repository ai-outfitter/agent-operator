# Run a resident agent from a private catalog

This runbook deploys a controller-managed resident `Agent` whose Organization
catalog is a private Git repository. `AgentCatalog` has no credential field.
The Organization controller validates the source but does not resolve it, and
rejects a catalog URI containing credentials. Under OPR-001, Outfitter owns
catalog fetching and profile resolution; the operator MUST NOT fetch the
catalog.

The resident runtime starts with `outfitter run`, which does not synchronize
remote sources first. Without an already populated cache, Outfitter cannot
resolve the selected profile. Until the runtime synchronizes catalogs itself,
the `Agent` MUST use a `setup` step to fetch the pinned catalog into the exact
cache path read by Outfitter.

This runbook is limited to the following resources:

- Organization: `example-org`, with one private catalog at
  `https://github.com/example-org/.agents.git`.
- Agent: `<agent>`, in the operator-owned `agent-<agent>` namespace.
- Catalog revision: one immutable, full commit SHA.
- Forge credentials: two runtime tokens and one mounted catalog credential.
- Catalog payload: `agents/<agent>/agent.md` at the repository root.

## Prerequisites

You need:

- a private catalog repository;
- a Kubernetes cluster with Agent Operator installed;
- an agent profile at `agents/<agent>/agent.md` in the catalog;
- an immutable, lowercase, full 40-character commit SHA containing the reviewed
  profile; and
- a runtime image containing every executable used by the setup step, including
  `git`, `base64`, `install`, `ln`, `mktemp`, `readlink`, and `mv` with `-T`
  support.

If the catalog uses SSH, the runtime image MUST also contain the SSH client and
trusted host configuration support.

## Prepare three credential roles

Keep the following credential roles distinct, even when `GITHUB_TOKEN` also
supplies the catalog credential:

- `GITHUB_NOTIFY_TOKEN` MUST be a classic personal access token with only the
  `notifications` scope. GitHub's `GET /notifications` endpoint does not accept
  the fine-grained token used for repository work.
- `GITHUB_TOKEN` MUST be a fine-grained personal access token limited to one
  organization, only the repositories the agent works, and only the permissions
  its composition needs.
- The catalog read credential MUST be supplied separately to the setup
  operation. When the catalog is in the same organization, it SHOULD be the
  fine-grained `GITHUB_TOKEN`, presented over HTTPS by `GIT_ASKPASS`. When the
  catalog is in a different organization, it MUST be a read-only deploy key
  because a fine-grained personal access token cannot span organizations.

See the Outfitter
[forge credential model](https://github.com/ai-outfitter/outfitter/blob/main/docs/architecture/forge-credential-model.md)
for the credential reasoning.

Keep shell tracing disabled. Put the two runtime tokens in a local `forge.env`
file without printing them:

```text
GITHUB_NOTIFY_TOKEN=<classic-notifications-token>
GITHUB_TOKEN=<fine-grained-repository-token>
```

For the preferred HTTPS transport, put the catalog username and the same
fine-grained token in separate local files. The token file is the catalog
credential mounted for the setup operation; it is not written into the
`Organization`, `Agent`, or setup script.

```sh
chmod 0600 forge.env catalog-username catalog-token
```

## Declare the pinned catalog and setup step

Use a credential-free HTTPS URI in the `Organization`. Replace every
placeholder before applying the manifests:

```yaml
apiVersion: aioutfitter.com/v1alpha1
kind: Organization
metadata:
  name: example-org
spec:
  agentCatalogs:
    - name: private-catalog
      uri: https://github.com/example-org/.agents.git
      revision: <lowercase-40-character-commit-sha>
---
apiVersion: aioutfitter.com/v1alpha1
kind: Agent
metadata:
  name: <agent>
spec:
  memberships:
    - organization: example-org
  profile:
    agent: <agent>
    harness: pi
  credentials:
    - secret: <agent>-forge
      as: env
    - secret: <agent>-catalog-read
      as: volume
  setup:
    - name: private-catalog
      script: |
        set -eu
        umask 077

        catalog_uri='https://github.com/example-org/.agents.git'
        catalog_revision='<lowercase-40-character-commit-sha>'
        expected_profile='agents/<agent>/agent.md'
        credential_root='/var/run/agent/credentials/secrets/<agent>-catalog-read'

        cache_key="$(
          printf '%s' "${catalog_uri}#${catalog_revision}" | \
            base64 | tr -d '\n' | tr '+/' '-_' | tr -d '='
        )"
        repos_root='/workspace/.agents-cache/repos'
        cache_path="${repos_root}/${cache_key}"
        install -d -m 0700 "$repos_root"

        checkout="$(mktemp -d "${repos_root}/.private-catalog-${catalog_revision}.XXXXXX")"
        helper_root="$(mktemp -d)"
        new_link=''
        cleanup() {
          if [ -n "$checkout" ] && [ -e "$checkout" ]; then
            rm -rf "$checkout"
          fi
          rm -rf "$helper_root"
          if [ -n "$new_link" ] && [ -e "$new_link" ]; then
            rm -f "$new_link"
          fi
        }
        trap cleanup EXIT HUP INT TERM

        askpass="${helper_root}/askpass.sh"
        cat >"$askpass" <<'ASKPASS'
        #!/bin/sh
        case "$1" in
          *Username*) printf '%s\n' "$CATALOG_USERNAME" ;;
          *Password*) printf '%s\n' "$CATALOG_TOKEN" ;;
          *) exit 1 ;;
        esac
        ASKPASS
        chmod 0700 "$askpass"

        git -C "$checkout" init --quiet
        CATALOG_USERNAME="$(cat "${credential_root}/username")" \
        CATALOG_TOKEN="$(cat "${credential_root}/token")" \
        GIT_ASKPASS="$askpass" \
        GIT_TERMINAL_PROMPT=0 \
          git -C "$checkout" -c credential.helper= fetch --quiet --depth=1 \
            "$catalog_uri" "$catalog_revision"
        git -C "$checkout" checkout --quiet --detach FETCH_HEAD

        resolved_revision="$(git -C "$checkout" rev-parse HEAD)"
        test "$resolved_revision" = "$catalog_revision"
        test -f "${checkout}/${expected_profile}"

        old_target=''
        if [ -L "$cache_path" ]; then
          old_target="$(readlink "$cache_path")"
          case "$old_target" in
            .private-catalog-*/*)
              echo 'private-catalog: refusing an unsafe cache link' >&2
              exit 1
              ;;
            .private-catalog-*) ;;
            *)
              echo 'private-catalog: refusing an unexpected cache link' >&2
              exit 1
              ;;
          esac
        elif [ -e "$cache_path" ]; then
          echo 'private-catalog: refusing an unmanaged cache entry' >&2
          exit 1
        fi

        new_link="$(mktemp "${repos_root}/.catalog-link.XXXXXX")"
        rm -f "$new_link"
        ln -s "$(basename "$checkout")" "$new_link"
        mv -fT "$new_link" "$cache_path"
        new_link=''
        checkout=''
        if [ -n "$old_target" ]; then
          rm -rf "${repos_root}/${old_target}"
        fi
        printf 'private-catalog: cached revision %s\n' "$resolved_revision"
  workspace:
    resourceQuota:
      hard:
        requests.cpu: "1"
        requests.memory: 2Gi
        limits.cpu: "2"
        limits.memory: 4Gi
        requests.storage: 10Gi
        persistentvolumeclaims: "2"
        count/pods: "4"
        count/jobs.batch: "4"
        count/services: "2"
        count/configmaps: "10"
        count/secrets: "10"
    limitRange:
      container:
        defaultRequest:
          cpu: 100m
          memory: 256Mi
        default:
          cpu: "1"
          memory: 2Gi
    volume:
      size: 10Gi
```

`encodeRemoteSource` in Outfitter's `SourceCache` derives a source directory by
applying base64url encoding to `<credential-free-uri>#<revision>` under
`/workspace/.agents-cache/repos/`. `redactSourceUriCredentials` removes embedded
credentials before that key is derived. The script's `catalog_uri` therefore
MUST contain no credentials and MUST be byte-identical to
`Organization.spec.agentCatalogs[].uri`. Differences such as a trailing slash,
a `.git` suffix, or a different scheme produce a cache entry that Outfitter
never reads.

The script fetches only the pinned revision, verifies `HEAD`, and checks the
selected profile before changing the live cache. The cache path is a symlink to
a versioned checkout. The prepared replacement symlink and live symlink are
siblings on the workspace volume, so `mv -T` replaces the cache entry with one
atomic filesystem rename. If the fetch, validation, or rename fails, the
previous symlink and checkout remain intact. A later successful run removes the
previous checkout after switching the symlink.

Apply the manifests before creating the Secrets. The controller creates the
agent namespace, but the non-optional Secret projections prevent the setup step
and runtime from starting:

```sh
kubectl apply -f organization.yaml -f agent.yaml
kubectl wait agent/<agent> \
  --for=condition=NamespaceReady \
  --timeout=2m
```

Create the runtime-token Secret and the separately mounted catalog credential.
These commands pass values through files and stdin without intentionally
printing them:

```sh
kubectl -n agent-<agent> create secret generic <agent>-forge \
  --from-env-file=forge.env \
  --dry-run=client -o yaml | \
  kubectl -n agent-<agent> apply -f -
kubectl -n agent-<agent> create secret generic <agent>-catalog-read \
  --from-file=username=catalog-username \
  --from-file=token=catalog-token \
  --dry-run=client -o yaml | \
  kubectl -n agent-<agent> apply -f -
```

List only the Secret key names. The first command MUST report
`GITHUB_NOTIFY_TOKEN` and `GITHUB_TOKEN`; the second MUST report `token` and
`username`:

```sh
kubectl -n agent-<agent> get secret/<agent>-forge \
  -o go-template='{{range $key, $value := .data}}{{printf "%s\n" $key}}{{end}}'
kubectl -n agent-<agent> get secret/<agent>-catalog-read \
  -o go-template='{{range $key, $value := .data}}{{printf "%s\n" $key}}{{end}}'
```

## Use a deploy key across organizations

When the private catalog belongs to a different organization from the one to
which `GITHUB_TOKEN` is scoped, create a repository-scoped, read-only deploy
key. The private key and pinned `known_hosts` entry MUST be mounted from the
catalog Secret. The SSH client MUST come from the runtime image.

Create the Secret from local files without printing the key:

```sh
chmod 0600 catalog-deploy-key catalog-known-hosts
kubectl -n agent-<agent> create secret generic <agent>-catalog-read \
  --from-file=id_ed25519=catalog-deploy-key \
  --from-file=known_hosts=catalog-known-hosts \
  --dry-run=client -o yaml | \
  kubectl -n agent-<agent> apply -f -
```

Use a credential-free URI such as
`ssh://<forge-host>/<org>/.agents.git` in both the Organization and script.
Replace the askpass helper and HTTPS fetch command in the setup step with an
ephemeral private-key copy and a noninteractive SSH transport:

```sh
ephemeral_key="${helper_root}/id_ed25519"
cp "${credential_root}/id_ed25519" "$ephemeral_key"
chmod 0600 "$ephemeral_key"
git_ssh_command="ssh -l git -i ${ephemeral_key}"
git_ssh_command="${git_ssh_command} -o IdentitiesOnly=yes -o BatchMode=yes"
git_ssh_command="${git_ssh_command} -o UserKnownHostsFile=${credential_root}/known_hosts"
git_ssh_command="${git_ssh_command} -o StrictHostKeyChecking=yes"
GIT_SSH_COMMAND="$git_ssh_command" \
  git -C "$checkout" fetch --quiet --depth=1 \
    "$catalog_uri" "$catalog_revision"
```

The ephemeral key is removed with `helper_root` when the setup container exits.
The host key MUST be reviewed and pinned before deployment; disabling host-key
checking is not an acceptable substitute.

## Move both revision pins together

The immutable revision appears in two places: the Organization catalog pin and
the setup script's `catalog_revision`. An update MUST change both copies to the
same lowercase, full 40-character commit SHA in one reviewed manifest change.
The operator does not compare the script with the Organization field.

Changing the script changes the Deployment pod template and forces a rollout.
That rollout is intentional: the new setup container populates and validates
the cache before the replacement resident runtime starts.

## Prohibitions

- Credential material MUST NOT be written onto the workspace PVC. It survives
  Secret deletion and pod restarts, is readable by later workloads with volume
  access, and remains in volume snapshots. The setup step MUST read the
  credential directly from the read-only Secret mount for the operation that
  needs it and keep any necessary copy on the setup container's ephemeral
  filesystem.
- Executables MUST NOT be copied onto the workspace PVC out-of-band from the
  image. No image scanner, signature policy, or drift check covers them. This is
  especially important for an SSH client because it handles the private key and
  validates the remote host. Required executables MUST be built into the pinned
  runtime image.
- The classic `GITHUB_NOTIFY_TOKEN` MUST NOT be reused for catalog reads or
  repository work. Its only purpose is GitHub notification access.

## Verify the deployment and pin

Wait for the Organization and Agent. Compare both reported catalog revisions
with the reviewed pin to verify that the pin propagated through the controller.
Then verify that the completed setup container logged the same revision after
its `rev-parse` assertion:

```sh
catalog_revision='<lowercase-40-character-commit-sha>'
kubectl get organization/example-org
kubectl wait organization/example-org \
  --for=condition=Ready \
  --timeout=2m
kubectl wait agent/<agent> \
  --for=condition=Ready \
  --timeout=5m
test "$(
  kubectl get organization/example-org \
    -o jsonpath='{.status.catalogSources[?(@.name=="private-catalog")].revision}'
)" = "$catalog_revision"
test "$(
  kubectl get agent/<agent> \
    -o jsonpath='{.status.catalogSources[?(@.name=="private-catalog")].revision}'
)" = "$catalog_revision"
kubectl -n agent-<agent> rollout status \
  deployment/agent-runtime --timeout=5m
pod="$(
  kubectl -n agent-<agent> get pod \
    -l aioutfitter.com/agent=<agent> \
    -o jsonpath='{.items[0].metadata.name}'
)"
setup_log="$(
  kubectl -n agent-<agent> logs pod/"$pod" \
    -c setup-private-catalog
)"
printf '%s\n' "$setup_log"
printf '%s\n' "$setup_log" | \
  grep -F "private-catalog: cached revision $catalog_revision"
kubectl -n agent-<agent> logs deployment/agent-runtime --tail=100
```

The setup log MUST show no credential values, and the runtime log SHOULD show
the runtime starting with the selected profile and no missing-source or
missing-profile error. `Agent` `Ready=True` covers the Kubernetes workload only.
It does NOT prove that the forge notification channel is configured or that a
notification wake reaches the resident agent; verify that round trip separately
for the composed channel.

## Operator-managed synchronization

Set `Agent.spec.catalogSync.enabled: true` to have the operator add a dedicated
`sync-agent-catalog` init container. It runs `outfitter sync` with the rendered
settings, durable workspace, and the Agent's generic credential projections
before user setup steps and the resident runtime. The sync container does not
receive the Agent's Kubernetes API token.

The generic credential projection still exposes a catalog credential to the
resident runtime when the same reference is declared under `spec.credentials`.
A future credential binding MAY narrow a named credential to catalog sync only;
under OPR-004.2, that binding MUST remain content-agnostic.
