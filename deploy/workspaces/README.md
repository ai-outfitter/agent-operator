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
The nonprod overlay pins immutable ECR tags `operator-v0.16.0` and
`runtime-v0.2.0`, populated by mirroring the corresponding released operator and
webapp runtime images. Publish and mirror those releases before applying the
overlay. The ECR repository enforces immutable tags. The Ocean overlay records
the earlier private development deployment and remains unchanged.

The controller uses deadlines and owner references to manage one Deployment,
PVC, Service, ServiceAccount and credential Secret per Workspace. Sleep sets
replicas to zero. Expiry waits for pods to stop, deletes the PVC and supporting
objects, then releases the finalizer. Tenant namespaces remain available for
other workspaces. Tests cover unchanged reconciliation, sleep/wake preservation
and expiry without deleting a neighbor.
