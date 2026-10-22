# Public media on EKS (opt-in)

The existing private nodes cannot host public ICE candidates. This overlay adds
one LiveKit server on **one dedicated public-IP EKS node**, while broker/worker
remain on the existing private nodes. It adds no NLB, resident stack or controller.
LiveKit's [Kubernetes requirements](https://docs.livekit.io/transport/self-hosting/kubernetes/)
require host networking and direct node reachability. HTTPS ingress only carries
signaling; it cannot carry UDP media or fix NAT candidate advertisement.

`infra/` is a separate OpenTofu root, never included in existing infrastructure
applies. Supply the verified existing cluster name, worker IAM role, existing ingress-node security group IDs and public
subnets. It refuses other AWS accounts and subnets lacking automatic public IPv4.
Review the plan and verify internet-gateway routes, NACLs, node/control-plane
security group connectivity and the minimal worker-role permissions before applying.
No existing node group or subnet is modified. A new billed `c6i.large` public
node is required. No infrastructure was provisioned by preparing these files.

1. In account `216577824627`, inspect the cluster and existing public subnets;
   the worker role must already have node/ECR permissions (CNI permissions belong
   to its own workload identity where supported). Discover the **actual SGs** on
   the existing ingress-nginx worker nodes and pass
   `existing_node_security_group_ids`; the added rule permits private TCP7880
   from those SGs only. Do not assume cluster-primary SG membership. Use
   `tofu init`, `tofu fmt -check`, `tofu validate` and
   `tofu plan -var-file=PRIVATE.tfvars` in `infra`. Keep tfvars and state
   outside git; state ownership/backend must be settled before apply.
2. Provision the reviewed node group. Confirm its Kubernetes node has public
   ExternalIP and `aioutfitter.com/voice-media=public`; it must be tainted with
   `aioutfitter.com/voice-media=public:NoSchedule`. Existing private nodes are
   untouched. A private-only API endpoint is compatible if this subnet can reach
   it within the VPC; otherwise verify public API allowed CIDRs include the node.
3. Create `voice-media-preview` from `namespace.yaml`. Supply
   `voice-livekit-keys` Secret key `keys` out of band, using the **same** LiveKit
   key/secret as the broker and worker's `voice-media` Secret. Keep credentials
   out of OpenTofu. This namespace alone permits host networking. Run only this media container as UID0 with every capability dropped except
   NET_BIND_SERVICE, no privilege escalation and a read-only root filesystem.
   This avoids dependence on containerd ambient capabilities for nonroot
   low-port binding. Broker, worker and gateway remain nonroot.
4. Point **DNS-only** `turn-voice-preview.unsupervised.wtf` directly at that
   node's public IPv4. Point `livekit-voice-preview.unsupervised.wtf` at the
   existing public NGINX ingress. Never orange-cloud proxy media/TURN.
5. Apply `ingress.yaml` in `voice-media-preview` and wait for both Certificates
   to be Ready. The existing Cloudflare DNS ClusterIssuer must work in this
   namespace. TURN terminates TLS inside LiveKit using the mounted certificate;
   NGINX terminates signaling TLS. Apply the full overlay only after the node,
   DNS and certificate prerequisites hold; server dry-run first.
6. Set broker/worker server-side `voice-media.url` and `.publicUrl` both to
   `wss://livekit-voice-preview.unsupervised.wtf`. Their existing public egress
   policy permits WSS/TURN TLS 443, ICE TCP 7881, ICE UDP 7882 and TURN UDP 3478.
   Worker media therefore reaches the node by its public address, not a private
   hostNetwork endpoint. Node security groups/NACLs must permit return traffic.

The node SG opens only media ports to the internet. Explicit media-SG rules admit private ingress-to-node signaling on 7880 from
verified existing-node SGs. The media pod uses `dnsPolicy: Default` and the VPC
resolver rather than depending on cross-node CoreDNS connectivity. Host-network traffic is not protected reliably by pod
NetworkPolicy, so AWS SGs are the media boundary. No SSH port is added.

This is a singleton preview with disruption on replacement. Public IPv4 may
change when EKS replaces the node: update TURN DNS, wait for propagation, and
restart LiveKit before accepting calls. No automatic DNS controller or static IP
reassociation is provided. Mounting a renewed TURN certificate does not prove
LiveKit has reloaded it: restart the singleton after renewal and verify the
served certificate. Do not scale this topology beyond one node/replica; a shared
Redis and a reviewed multi-node signaling/media design are separate work.

Acceptance requires an external browser with microphone permission: verify WSS,
selected ICE candidate addresses, bidirectional audio, and a forced TURN/TLS
relay session with UDP disabled. Record candidate protocol/relay evidence without
recording tokens or audio. Internal room creation and TLS health alone do not
prove media. See [LiveKit deployment/TURN guidance](https://docs.livekit.io/transport/self-hosting/deployment/).

Rollback: stop new preview calls; delete this overlay's Deployment/Ingress only,
retain Secrets/certificates until review, then destroy only this OpenTofu root
with its reviewed state. Remove the two DNS records. Leave existing residents,
private node groups, broker PVC and unrelated ingress resources intact.

## Node identity boundary

`hostNetwork` can reach EC2 IMDS, even with IMDSv2 and the launch template's
hop-limit 1. That hop limit isolates ordinary pod networking; it does not isolate
this media process from the node IAM role. Supply a least-privilege worker role
with no application, provider, billing or account-administration permissions.
Review the role's actual attached/inline policies before provisioning; reusing
a broad existing role is not an acceptable substitute for that check. Prefer
CNI-specific IAM permissions on the existing CNI's own workload identity instead
of adding them to this media-node role, where the cluster configuration supports
that separation. See [AWS's IMDS guidance](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html).

This preview does not install Cilium or a privileged iptables init container to
block IMDS. Pod NetworkPolicy does not reliably isolate host networking, and a
host-wide metadata block could break node components; the narrow initial
boundary is the minimal node role plus trusted, pinned media image. A reviewed
host firewall rule limited to the media UID/cgroup can be added later if stronger
isolation is required. That is a separate node policy change, not a hidden
side effect of this application overlay.

The earlier nonroot TURN port 443 experiment against the pinned image used UID
1000, all capabilities dropped except NET_BIND_SERVICE, no-new-privileges, and
`net.ipv4.ip_unprivileged_port_start=1024`. With a temporary self-signed test
certificate, LiveKit started TURN/TLS on 443; `/proc/1/status` showed UID1000,
`CapEff=0000000000000400` and `NoNewPrivs=1`. The Docker-compatible local runtime
honors the added capability, but that does not establish EKS/containerd behavior.
The manifest therefore uses UID0 with only NET_BIND_SERVICE to make low-port
binding independent of ambient capability propagation. Confirm the listener
after the actual EKS/containerd rollout; this is not evidence of public TLS trust or a
working relay session. Production uses the cert-manager certificate, not the
local self-signed fixture.

Before deployment, verify no DaemonSet tolerating this node's taint binds host
443 or 3478. The container declares these host ports for scheduler conflict
checks. Record the TURN certificate's `notAfter` and next renewal check date in
the deployment receipt; schedule an operational reminder before expiry and
restart/verify LiveKit following renewal. No automated restart is installed.

The LiveKit v1.13.10 image is pinned to the pulled and locally tested amd64
manifest `sha256:e96a42b8623f738919a3f2006d433aeb19d0f3c67ce11eb986ea20e2cf65fbf5`.
The dedicated node group is amd64. Review a replacement digest before upgrades.
