# Separate temporary workspace controller

Apply `code/operator/config/crd/bases/aioutfitter.com_workspaces.yaml`, create
namespace `outfitter-cloud`, and adapt `operator.yaml` with your operator image,
runtime image and gateway address. The webapp repository provides the matching
gateway/web deployment and its self-hosting guide in `docs/workspaces.md`.

`--workspace-only` skips resident Organization and Agent reconcilers and uses a
separate leader-election lease. Workspace support stays disabled in existing
operator deployments unless `--workspace-image` is configured. Installing this
instance does not require replacing the resident operator.

Build the binary in `code/operator` with `CGO_ENABLED=0 go build -o manager ./cmd`.
Use this directory's Dockerfile with that binary in the image build context.
The nonprod overlay pins the runtime image by digest and passes
`--workspace-image-require-digest`, so the controller refuses to start with a
tag; webapp CI promotes that argument on the live Deployment by digest. The
operator image must be pinned to the released operator tag that contains
direct-Pod support, set after Nicholas approves that release. It is not
`operator-v0.16.0` (which has the earlier CRD and no digest flag) and must not
be guessed. Apply the new CRD before the overlay. The ECR repository enforces
immutable tags.

The controller uses deadlines and owner references to manage one PVC, Service,
ServiceAccount and credential Secret per Workspace, plus one directly managed
runtime Pod per compute generation. Expiry waits for Pods to stop, deletes the
PVC and supporting objects, then releases the finalizer. Tenant namespaces
remain available for other workspaces.

## Runtime Pods and compute generations

`spec.computeGeneration` is required and selects the runtime. The gateway sets
`1` on creation and increments it on every normal wake from `Sleeping`;
renewals of an active run leave it unchanged, and it can never decrease. For
generation `N` the controller creates exactly one Pod named `<workspace>-gN`
with `restartPolicy: Never`, and records `status.computeGeneration`,
`status.podName` and `status.resolvedImage`. The image is `spec.image` when
set, otherwise the `--workspace-image` default, and is captured when the Pod is
created. Changing the default never rewrites an existing Pod; it applies to the
next generation only. `Ready` is reported with reason `Starting` (including
"Starting compute generation N" before the Pod exists), `Running`, `Stopping`,
`Sleeping` or `Interrupted`.

At most one Pod mounts a workspace volume. A new generation first forgets the
previous Pod, stops it, and waits until it is gone. Idle sleep does the same:
`status.podName` is cleared (with an optimistic lock, so a concurrent renewal
wins), the Pod is deleted with its full grace, and the workspace reports
`Stopping` until the Pod is gone and `Sleeping` afterwards. Extending
`awakeUntil` without a new generation never starts a second Pod for the same
generation.

### Graceful termination and interruption

Runtime Pods get `terminationGracePeriodSeconds` from
`--workspace-termination-grace-seconds` (default 3900, sized so a stage of a
one-hour run can finish and persist its state after SIGTERM). The Service
publishes not-ready addresses, so a draining Pod stays reachable for the
gateway to read the accepted run's final state. When a Pod the controller did
not stop is terminating (eviction, node drain, manual delete), the workspace
reports `Stopping`: no wake and no new generation starts while it drains. Once
that Pod exits or disappears the `Ready` condition becomes `Interrupted`.

There is no failure recovery. A runtime Pod that exits, is lost, or is
terminated externally while recorded in `status.podName` interrupts the
environment. The recorded Pod is classified before any sleep or generation
change can hide it, so a failure that is only observed when the idle lease
lapses or after the gateway raised the generation is still `Interrupted`. The
state is sticky until the Workspace is deleted: the controller does not
recreate the Pod even when the generation is incremented, and the gateway
rejects new tasks and wakes. The failed Pod is retained for log inspection and
removed with the workspace. Expiry does not wait for a run: cleanup deletes
every Pod with a 30-second grace, shortening an in-progress drain, because the
volume is deleted next.

Storage policy (`reusable` or `task`) is immutable and enforced by the gateway,
which fixes `expiresAt` after a task finishes. The controller deletes exactly at
`expiresAt`.

### Image references

`spec.image` must be digest-pinned (`name@sha256:<64 hex>`); the CRD rejects
tags. The `--workspace-image` default is only validated at startup: pass
`--workspace-image-require-digest` in production overlays so the controller
refuses to start with a tag. Without that flag the controller logs a warning and
accepts tags, which local clusters need for images such as
`outfitter-workspace:local` that are loaded rather than pulled by digest.
Published runtime images must be immutable regardless of how they are
referenced.

### Cutover

This release manages direct Pods only. It does not read, keep or migrate the
per-workspace Deployments of earlier releases, and Workspaces without
`spec.computeGeneration` are rejected by the CRD (existing objects report
`InvalidSpec`). Cut a cluster over explicitly, in this order, because the new
cleanup deletes a PVC that storage protection holds while an old Pod still
mounts it:

1. Stop the old controller (scale the `workspace-operator` Deployment to 0).
2. Delete the old per-workspace Deployments, or scale them to 0, and wait for
   their Pods to go: `kubectl delete deployment -A -l aioutfitter.com/workload=workspace`.
3. Delete the old Workspaces: `kubectl delete workspaces -A --all`. They keep
   their cleanup finalizer until a controller runs; deleting them with
   `--cascade=foreground` is an alternative to step 2.
4. Apply the new CRD, then the new controller (released tag above) and a
   gateway that sets generations. Cleanup finishes and removes PVCs, Services,
   ServiceAccounts and credential Secrets.

The earlier private Ocean development overlay has been removed; it layered a
Deployment-based operator build over this base and no longer rendered a
working combination.

### Lost nodes

A Pod stuck `Terminating` on a lost or partitioned node is never finished by
its kubelet. The workspace stays `Stopping`, and after expiry cleanup requeues
until the Pod is gone, holding the PVC. This needs manual operator action: remove
the node, or force-delete the Pod only after confirming the node is gone. The
controller does not do this itself and does not recreate the Pod afterwards; the
environment ends `Interrupted`.

The workspace ClusterRole needs `pods: create, delete` for direct Pods. It does
not include `pods/exec` and no longer needs Deployments.
