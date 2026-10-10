package controller

import (
	"context"
	"strings"
	"time"

	api "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	testImageV1   = "example.test/runtime:v1"
	testImageV2   = "example.test/runtime:v2"
	testStorage   = "1Gi"
	testModel     = "test"
	testGateway   = "http://gateway:4040"
	testGatewayNS = "outfitter-cloud"
	testRelay     = "example.test/webapp:relay"
)

const testDigest = "@sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

var _ = Describe("Temporary Workspace lifecycle", func() {
	ctx := context.Background()
	ready := func(w *api.Workspace) metav1.Condition {
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(w), w)).To(Succeed())
		c := meta.FindStatusCondition(w.Status.Conditions, "Ready")
		Expect(c).NotTo(BeNil())
		return *c
	}
	reconcile := func(r *WorkspaceReconciler, w *api.Workspace) {
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(w)})
		Expect(err).NotTo(HaveOccurred())
	}
	tenant := func(prefix string) *corev1.Namespace {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: prefix, Labels: map[string]string{tenantLabel: testModel}}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		return ns
	}
	credential := func(ns, name string) {
		Expect(k8sClient.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{workspaceLabel: name}}, Data: map[string][]byte{workspaceTokenKey: []byte(strings.Repeat("t", 32))}})).To(Succeed())
	}
	reconciler := func(now *time.Time) *WorkspaceReconciler {
		return &WorkspaceReconciler{Client: k8sClient, APIReader: k8sClient, Scheme: k8sClient.Scheme(), Image: testImageV1, RelayImage: testRelay, GatewayURL: testGateway, GatewayNamespace: testGatewayNS, Model: testModel, Now: func() time.Time { return *now }}
	}
	spec := func(name string, now time.Time) api.EphemeralWorkspaceSpec {
		return api.EphemeralWorkspaceSpec{CredentialSecretName: name, AwakeUntil: metav1.NewTime(now.Add(time.Minute)), ExpiresAt: metav1.NewTime(now.Add(time.Hour)), StorageSize: testStorage, ComputeGeneration: 1}
	}
	workspacePods := func(w *api.Workspace) []corev1.Pod {
		pods := &corev1.PodList{}
		Expect(k8sClient.List(ctx, pods, client.InNamespace(w.Namespace), client.MatchingLabels{workspaceLabel: w.Name})).To(Succeed())
		return pods.Items
	}
	podNamed := func(ns, name string) *corev1.Pod {
		pod := &corev1.Pod{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, pod)).To(Succeed())
		return pod
	}
	noPod := func(ns, name string) {
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, &corev1.Pod{}))).To(BeTrue(), name)
	}
	// envtest has no scheduler or kubelet: unscheduled Pods delete immediately, scheduled ones stay
	// Terminating until something finishes the graceful delete, and phases never change on their own.
	schedule := func(pod *corev1.Pod) {
		binding := &corev1.Binding{ObjectMeta: metav1.ObjectMeta{Name: pod.Name, Namespace: pod.Namespace}, Target: corev1.ObjectReference{Kind: "Node", Name: "envtest-node"}}
		Expect(k8sClient.SubResource("binding").Create(ctx, pod, binding)).To(Succeed())
	}
	setPhase := func(pod *corev1.Pod, phase corev1.PodPhase, podReady bool) {
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())
		pod.Status.Phase = phase
		status := corev1.ConditionFalse
		if podReady {
			status = corev1.ConditionTrue
		}
		pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: status}}
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
	}
	finishDelete := func(pod *corev1.Pod) {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, pod, client.GracePeriodSeconds(0)))).To(Succeed())
	}
	// running creates a workspace and drives its first generation to Running on a scheduled Pod.
	running := func(r *WorkspaceReconciler, ns, name string, now time.Time) (*api.Workspace, *corev1.Pod) {
		w := &api.Workspace{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Spec: spec(name, now)}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		credential(ns, name)
		reconcile(r, w)
		pod := podNamed(ns, name+"-g1")
		schedule(pod)
		setPhase(pod, corev1.PodRunning, true)
		reconcile(r, w)
		Expect(ready(w).Reason).To(Equal("Running"))
		return w, pod
	}

	DescribeTable("cleans credentials when expiry precedes successful provisioning", func(partiallyProvisioned bool) {
		now := time.Now().UTC().Truncate(time.Second)
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "workspace-expiry-", Labels: map[string]string{tenantLabel: "expiry-test"}}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		w := &api.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "ws-expired", Namespace: ns.Name}, Spec: api.EphemeralWorkspaceSpec{CredentialSecretName: "ws-expired", AwakeUntil: metav1.NewTime(now.Add(time.Minute)), ExpiresAt: metav1.NewTime(now.Add(time.Hour)), ComputeGeneration: 1}}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		credential(ns.Name, w.Name)
		r := &WorkspaceReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Now: func() time.Time { return now }}
		key := client.ObjectKeyFromObject(w)
		if partiallyProvisioned {
			reconcile(r, w)
			Expect(ready(w).Reason).To(Equal("NotConfigured"))
		}
		now = now.Add(2 * time.Hour)
		for range 5 {
			reconcile(r, w)
		}
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, w))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, &corev1.Secret{}))).To(BeTrue())
	}, Entry("before its first reconciliation", false), Entry("after configuration prevents credential adoption", true))

	It("refuses to start a runtime without the inference relay image", func() {
		now := time.Now().UTC().Truncate(time.Second)
		ns := tenant("workspace-relay-")
		r := reconciler(&now)
		r.RelayImage = ""
		w := &api.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "ws-relay", Namespace: ns.Name}, Spec: spec("ws-relay", now)}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		credential(ns.Name, w.Name)
		reconcile(r, w)
		Expect(ready(w)).To(And(HaveField("Status", metav1.ConditionFalse), HaveField("Reason", "NotConfigured")))
		Expect(workspacePods(w)).To(BeEmpty())
	})

	It("runs one immutable Pod per compute generation, sleeps and expires without touching a neighbor", func() {
		now := time.Now().UTC().Truncate(time.Second)
		ns := tenant("workspace-pod-")
		r := reconciler(&now)
		create := func(name string) *api.Workspace {
			w := &api.Workspace{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name}, Spec: spec(name, now)}
			Expect(k8sClient.Create(ctx, w)).To(Succeed())
			credential(ns.Name, name)
			reconcile(r, w)
			return w
		}
		w := create("ws-first")
		neighbor := create("ws-neighbor")
		key := client.ObjectKeyFromObject(w)
		Expect(w.Spec.StoragePolicy).To(Equal(api.StoragePolicyReusable), "storagePolicy defaults to reusable")
		pod := podNamed(ns.Name, "ws-first-g1")
		Expect(metav1.IsControlledBy(pod, w)).To(BeTrue())
		Expect(pod.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyNever))
		Expect(*pod.Spec.TerminationGracePeriodSeconds).To(Equal(defaultTerminationGraceSeconds), "a running stage may finish within the one-hour run bound")
		Expect(pod.Labels).To(HaveKeyWithValue(workspaceWorkloadLabel, workspaceWorkloadValue), "the boundary NetworkPolicy selects this label")
		Expect(pod.Spec.Containers[0].Image).To(Equal(testImageV1))
		Expect(*pod.Spec.AutomountServiceAccountToken).To(BeFalse())
		Expect(pod.Spec.Containers[0].Env).ToNot(ContainElement(HaveField("Name", "SPARK_AUTHORIZATION")))
		// Inference leaves the Pod only through the relay sidecar, which alone holds the audience-bound token.
		runtime, relay := pod.Spec.Containers[0], pod.Spec.Containers[1]
		Expect(runtime.Name).To(Equal("runtime"), "status reads the runtime image from the first container")
		Expect(runtime.Env).To(ContainElement(And(HaveField("Name", "INFERENCE_BASE_URL"), HaveField("Value", "http://127.0.0.1:4141/v1"))))
		Expect(runtime.Env).To(ContainElement(HaveField("Name", "WORKSPACE_TOKEN")))
		Expect(runtime.VolumeMounts).ToNot(ContainElement(HaveField("Name", inferenceTokenVolumeName)), "the runtime never holds the inference identity")
		Expect(relay.Name).To(Equal(inferenceRelayName))
		Expect(relay.Image).To(Equal(testRelay))
		Expect(relay.Command).To(Equal([]string{"node", "workspace/inference-relay.mjs"}))
		Expect(relay.Env).To(ContainElements(
			And(HaveField("Name", "INFERENCE_GATEWAY_URL"), HaveField("Value", testGateway)),
			And(HaveField("Name", "INFERENCE_TOKEN_FILE"), HaveField("Value", "/var/run/secrets/outfitter/inference/token")),
			And(HaveField("Name", "PORT"), HaveField("Value", "4141"))))
		Expect(relay.VolumeMounts).To(ConsistOf(corev1.VolumeMount{Name: inferenceTokenVolumeName, MountPath: "/var/run/secrets/outfitter/inference", ReadOnly: true}))
		Expect(relay.SecurityContext).To(Equal(runtime.SecurityContext))
		Expect(relay.ReadinessProbe.HTTPGet.Path).To(Equal("/health"))
		Expect(relay.ReadinessProbe.HTTPGet.Port.IntValue()).To(Equal(4141))
		Expect(pod.Spec.Volumes).To(ContainElement(And(HaveField("Name", inferenceTokenVolumeName), HaveField("Projected.Sources", ConsistOf(HaveField("ServiceAccountToken", And(
			HaveField("Audience", "outfitter-inference"), HaveField("ExpirationSeconds", HaveValue(Equal(int64(3600)))), HaveField("Path", "token"))))))))
		Expect(ready(w).Reason).To(Equal("Starting"))
		Expect(w.Status.ComputeGeneration).To(Equal(int64(1)))
		Expect(w.Status.PodName).To(Equal("ws-first-g1"))
		Expect(w.Status.ResolvedImage).To(Equal(testImageV1))
		policy := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "workspace-boundary", Namespace: ns.Name}, policy)).To(Succeed())
		Expect(policy.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue(workspaceWorkloadLabel, workspaceWorkloadValue))
		svc := &corev1.Service{}
		Expect(k8sClient.Get(ctx, key, svc)).To(Succeed())
		Expect(svc.Spec.PublishNotReadyAddresses).To(BeTrue(), "a draining runtime stays reachable through the Service")
		// A no-op reconciliation must not write another Pod or Service revision.
		podRevision, serviceRevision := pod.ResourceVersion, svc.ResourceVersion
		reconcile(r, w)
		Expect(podNamed(ns.Name, pod.Name).ResourceVersion).To(Equal(podRevision))
		Expect(k8sClient.Get(ctx, key, svc)).To(Succeed())
		Expect(svc.ResourceVersion).To(Equal(serviceRevision))
		schedule(pod)
		setPhase(pod, corev1.PodRunning, true)
		reconcile(r, w)
		Expect(ready(w)).To(And(HaveField("Status", metav1.ConditionTrue), HaveField("Reason", "Running")))
		// A new runtime default never rewrites an existing Pod or its recorded image.
		r.Image = testImageV2
		reconcile(r, w)
		Expect(podNamed(ns.Name, pod.Name).Spec.Containers[0].Image).To(Equal(testImageV1))
		Expect(ready(w).Reason).To(Equal("Running"))
		Expect(w.Status.ResolvedImage).To(Equal(testImageV1))
		pvc := &corev1.PersistentVolumeClaim{}
		Expect(k8sClient.Get(ctx, key, pvc)).To(Succeed())
		volumeUID := pvc.UID
		// Idle sleep forgets the Pod first, then stops it with the full grace, and keeps storage.
		now = now.Add(2 * time.Minute)
		reconcile(r, w)
		pod = podNamed(ns.Name, pod.Name)
		Expect(pod.DeletionTimestamp).NotTo(BeNil())
		Expect(*pod.DeletionGracePeriodSeconds).To(Equal(defaultTerminationGraceSeconds), "sleep lets an accepted run finish")
		Expect(ready(w).Reason).To(Equal("Stopping"))
		Expect(w.Status.PodName).To(BeEmpty())
		Expect(w.Status.ComputeGeneration).To(Equal(int64(1)))
		// A late renewal in the same generation is not a failure and does not start a second Pod.
		w.Spec.AwakeUntil = metav1.NewTime(now.Add(time.Minute))
		Expect(k8sClient.Update(ctx, w)).To(Succeed())
		reconcile(r, w)
		Expect(ready(w).Reason).To(Equal("Stopping"))
		finishDelete(pod)
		reconcile(r, w)
		Expect(ready(w).Reason).To(Equal("Sleeping"))
		Expect(workspacePods(w)).To(BeEmpty())
		Expect(k8sClient.Get(ctx, key, pvc)).To(Succeed())
		Expect(pvc.UID).To(Equal(volumeUID))
		// A normal wake increments the generation and starts a fresh Pod on the current default.
		w.Spec.ComputeGeneration = 2
		Expect(k8sClient.Update(ctx, w)).To(Succeed())
		reconcile(r, w)
		pod2 := podNamed(ns.Name, "ws-first-g2")
		Expect(pod2.Spec.Containers[0].Image).To(Equal(testImageV2))
		Expect(ready(w).Reason).To(Equal("Starting"))
		Expect(w.Status.ComputeGeneration).To(Equal(int64(2)))
		Expect(w.Status.PodName).To(Equal("ws-first-g2"))
		Expect(w.Status.ResolvedImage).To(Equal(testImageV2))
		// envtest has no storage-protection controller; simulate its release of an unused PVC.
		Expect(k8sClient.Get(ctx, key, pvc)).To(Succeed())
		pvc.Finalizers = nil
		Expect(k8sClient.Update(ctx, pvc)).To(Succeed())
		schedule(pod2)
		now = now.Add(2 * time.Minute)
		reconcile(r, w)
		Expect(*podNamed(ns.Name, pod2.Name).DeletionGracePeriodSeconds).To(Equal(defaultTerminationGraceSeconds))
		now = now.Add(2 * time.Hour)
		for range 3 {
			reconcile(r, w)
		}
		// Expiry is bounded even for a Pod still draining: the volume is going away, so the grace shrinks.
		Expect(*podNamed(ns.Name, pod2.Name).DeletionGracePeriodSeconds).To(Equal(cleanupGraceSeconds))
		finishDelete(pod2)
		for range 8 {
			reconcile(r, w)
		}
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, w))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, pvc))).To(BeTrue())
		Expect(workspacePods(w)).To(BeEmpty())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(ns), ns)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(neighbor), neighbor)).To(Succeed())
		podNamed(ns.Name, "ws-neighbor-g1")
	})

	It("allows one writer per volume and never recovers an interrupted environment", func() {
		now := time.Now().UTC().Truncate(time.Second)
		ns := tenant("workspace-writer-")
		r := reconciler(&now)
		w := &api.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "ws-writer", Namespace: ns.Name}, Spec: spec("ws-writer", now)}
		w.Spec.Image = "example.test/pinned" + testDigest
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		credential(ns.Name, w.Name)
		reconcile(r, w)
		pod := podNamed(ns.Name, "ws-writer-g1")
		Expect(pod.Spec.Containers[0].Image).To(Equal("example.test/pinned"+testDigest), "spec.image wins over the default")
		schedule(pod)
		setPhase(pod, corev1.PodRunning, true)
		reconcile(r, w)
		Expect(ready(w).Reason).To(Equal("Running"))
		// A superseded generation is forgotten, stopped, and must be fully gone before the next Pod mounts.
		w.Spec.ComputeGeneration = 2
		Expect(k8sClient.Update(ctx, w)).To(Succeed())
		reconcile(r, w)
		noPod(ns.Name, "ws-writer-g2")
		Expect(podNamed(ns.Name, pod.Name).DeletionTimestamp).NotTo(BeNil())
		Expect(ready(w).Reason).To(Equal("Starting"))
		Expect(w.Status.PodName).To(BeEmpty())
		reconcile(r, w)
		Expect(ready(w).Reason).To(Equal("Starting"), "a stopping superseded Pod is not a failure")
		Expect(workspacePods(w)).To(HaveLen(1))
		finishDelete(pod)
		reconcile(r, w)
		pod2 := podNamed(ns.Name, "ws-writer-g2")
		Expect(ready(w).Reason).To(Equal("Starting"))
		Expect(w.Status.PodName).To(Equal("ws-writer-g2"))
		Expect(w.Status.ComputeGeneration).To(Equal(int64(2)))
		// An exited runtime interrupts the environment permanently: no recreation, even on a new generation.
		schedule(pod2)
		setPhase(pod2, corev1.PodFailed, false)
		reconcile(r, w)
		Expect(ready(w)).To(And(HaveField("Status", metav1.ConditionFalse), HaveField("Reason", "Interrupted")))
		w.Spec.ComputeGeneration = 3
		w.Spec.AwakeUntil = metav1.NewTime(now.Add(10 * time.Minute))
		Expect(k8sClient.Update(ctx, w)).To(Succeed())
		for range 3 {
			reconcile(r, w)
		}
		Expect(ready(w).Reason).To(Equal("Interrupted"))
		noPod(ns.Name, "ws-writer-g3")
		Expect(podNamed(ns.Name, pod2.Name).DeletionTimestamp).To(BeNil(), "the failed Pod is retained for inspection")
		now = now.Add(time.Hour)
		reconcile(r, w)
		Expect(ready(w).Reason).To(Equal("Interrupted"), "sleep does not clear an interruption")
		// A Pod that vanishes while awake is also an interruption.
		lost := &api.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "ws-lost", Namespace: ns.Name}, Spec: spec("ws-lost", now)}
		Expect(k8sClient.Create(ctx, lost)).To(Succeed())
		credential(ns.Name, lost.Name)
		reconcile(r, lost)
		Expect(k8sClient.Delete(ctx, podNamed(ns.Name, "ws-lost-g1"))).To(Succeed())
		reconcile(r, lost)
		Expect(ready(lost).Reason).To(Equal("Interrupted"))
	})

	It("classifies a failed Pod before sleep or supersession can hide it", func() {
		now := time.Now().UTC().Truncate(time.Second)
		ns := tenant("workspace-classify-")
		r := reconciler(&now)
		// Failure observed in the same reconcile as the idle lapse: Interrupted, Pod retained, no sleep.
		asleep, pod := running(r, ns.Name, "ws-fail-sleep", now)
		setPhase(pod, corev1.PodFailed, false)
		now = now.Add(2 * time.Minute)
		reconcile(r, asleep)
		Expect(ready(asleep).Reason).To(Equal("Interrupted"))
		Expect(asleep.Status.PodName).To(Equal(pod.Name))
		Expect(podNamed(ns.Name, pod.Name).DeletionTimestamp).To(BeNil())
		// Failure observed only after the gateway raised the generation: Interrupted, no new Pod.
		now = time.Now().UTC().Truncate(time.Second)
		bumped, pod := running(r, ns.Name, "ws-fail-bump", now)
		setPhase(pod, corev1.PodFailed, false)
		bumped.Spec.ComputeGeneration = 2
		Expect(k8sClient.Update(ctx, bumped)).To(Succeed())
		reconcile(r, bumped)
		Expect(ready(bumped).Reason).To(Equal("Interrupted"))
		noPod(ns.Name, "ws-fail-bump-g2")
		Expect(podNamed(ns.Name, pod.Name).DeletionTimestamp).To(BeNil())
		// A Pod lost while asleep-pending is still a loss, not a sleep.
		gone, pod := running(r, ns.Name, "ws-gone-sleep", now)
		finishDelete(pod)
		now = now.Add(2 * time.Minute)
		reconcile(r, gone)
		Expect(ready(gone).Reason).To(Equal("Interrupted"))
	})

	It("reports Stopping while an externally terminated runtime drains, then Interrupted", func() {
		now := time.Now().UTC().Truncate(time.Second)
		ns := tenant("workspace-drain-")
		r := reconciler(&now)
		w, pod := running(r, ns.Name, "ws-drain", now)
		// Eviction or any delete the controller did not issue: the Pod keeps its recorded name.
		Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		reconcile(r, w)
		Expect(ready(w)).To(And(HaveField("Status", metav1.ConditionFalse), HaveField("Reason", "Stopping")))
		Expect(w.Status.PodName).To(Equal(pod.Name), "the draining Pod stays addressable and recorded")
		// Neither a wake nor a new generation may start compute while it drains.
		w.Spec.AwakeUntil = metav1.NewTime(now.Add(10 * time.Minute))
		w.Spec.ComputeGeneration = 2
		Expect(k8sClient.Update(ctx, w)).To(Succeed())
		reconcile(r, w)
		Expect(ready(w).Reason).To(Equal("Stopping"))
		noPod(ns.Name, "ws-drain-g2")
		// Once the drained runtime exits the environment is interrupted; a sleep in between changes nothing.
		now = now.Add(20 * time.Minute)
		reconcile(r, w)
		Expect(ready(w).Reason).To(Equal("Stopping"))
		setPhase(pod, corev1.PodFailed, false)
		reconcile(r, w)
		Expect(ready(w).Reason).To(Equal("Interrupted"))
		noPod(ns.Name, "ws-drain-g2")
		// The same drain that disappears before exiting is also an interruption.
		w2, pod2 := running(r, ns.Name, "ws-drain-lost", now)
		Expect(k8sClient.Delete(ctx, pod2)).To(Succeed())
		reconcile(r, w2)
		Expect(ready(w2).Reason).To(Equal("Stopping"))
		finishDelete(pod2)
		reconcile(r, w2)
		Expect(ready(w2).Reason).To(Equal("Interrupted"))
	})

	It("rejects generation rollback, policy changes and unpinned images at the API", func() {
		now := time.Now().UTC().Truncate(time.Second)
		ns := tenant("workspace-schema-")
		w := &api.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "ws-schema", Namespace: ns.Name}, Spec: api.EphemeralWorkspaceSpec{CredentialSecretName: "ws-schema", AwakeUntil: metav1.NewTime(now), ExpiresAt: metav1.NewTime(now.Add(time.Hour)), StoragePolicy: api.StoragePolicyTask, ComputeGeneration: 2}}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		attempt := func(mutate func(*api.Workspace)) error {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(w), w)).To(Succeed())
			mutate(w)
			return k8sClient.Update(ctx, w)
		}
		Expect(attempt(func(w *api.Workspace) { w.Spec.ComputeGeneration = 1 })).To(MatchError(ContainSubstring("computeGeneration")))
		Expect(attempt(func(w *api.Workspace) { w.Spec.StoragePolicy = api.StoragePolicyReusable })).To(MatchError(ContainSubstring("storagePolicy")))
		Expect(attempt(func(w *api.Workspace) { w.Spec.Image = "example.test/runtime:latest" })).To(MatchError(ContainSubstring("image")))
		Expect(attempt(func(w *api.Workspace) {
			w.Spec.Image = "example.test/runtime" + testDigest
			w.Spec.ComputeGeneration = 3
		})).To(Succeed())
		missing := &api.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "ws-nogen", Namespace: ns.Name}, Spec: api.EphemeralWorkspaceSpec{CredentialSecretName: "ws-nogen", AwakeUntil: metav1.NewTime(now), ExpiresAt: metav1.NewTime(now.Add(time.Hour))}}
		Expect(k8sClient.Create(ctx, missing)).To(MatchError(ContainSubstring("computeGeneration")), "the generation contract is required")
		unpinned := &api.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "ws-tagged", Namespace: ns.Name}, Spec: api.EphemeralWorkspaceSpec{CredentialSecretName: "ws-tagged", AwakeUntil: metav1.NewTime(now), ExpiresAt: metav1.NewTime(now.Add(time.Hour)), ComputeGeneration: 1, Image: testImageV1}}
		Expect(k8sClient.Create(ctx, unpinned)).To(MatchError(ContainSubstring("image")))
		Expect(ImageIsDigestPinned("example.test/runtime" + testDigest)).To(BeTrue())
		Expect(ImageIsDigestPinned(testImageV1)).To(BeFalse())
	})
})
