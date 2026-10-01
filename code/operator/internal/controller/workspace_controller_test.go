package controller

import (
	"context"
	"strings"
	"time"

	api "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Temporary Workspace lifecycle", func() {
	It("sleeps without losing storage and expires without deleting its tenant or neighbor", func() {
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "workspace-test-", Labels: map[string]string{tenantLabel: "test"}}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		r := &WorkspaceReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Image: "example.test/runtime:dev", GatewayURL: "http://gateway:4040", GatewayNamespace: "outfitter-cloud", Model: "test", Now: func() time.Time { return now }}
		create := func(name string) *api.Workspace {
			w := &api.Workspace{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name}, Spec: api.EphemeralWorkspaceSpec{CredentialSecretName: name, AwakeUntil: metav1.NewTime(now.Add(time.Minute)), ExpiresAt: metav1.NewTime(now.Add(time.Hour)), StorageSize: "1Gi"}}
			Expect(k8sClient.Create(ctx, w)).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name, Labels: map[string]string{workspaceLabel: name}}, Data: map[string][]byte{workspaceTokenKey: []byte(strings.Repeat("t", 32))}})).To(Succeed())
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(w)})
			Expect(err).NotTo(HaveOccurred())
			return w
		}
		w := create("ws-first")
		neighbor := create("ws-neighbor")
		key := client.ObjectKeyFromObject(w)
		dep := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, key, dep)).To(Succeed())
		Expect(*dep.Spec.Replicas).To(Equal(int32(1)))
		Expect(*dep.Spec.Template.Spec.AutomountServiceAccountToken).To(BeFalse())
		Expect(dep.Spec.Template.Spec.Containers[0].Env).ToNot(ContainElement(HaveField("Name", "SPARK_AUTHORIZATION")))
		// A no-op reconciliation must not write another Deployment or Service revision.
		revision := dep.ResourceVersion
		svc := &corev1.Service{}
		Expect(k8sClient.Get(ctx, key, svc)).To(Succeed())
		serviceRevision := svc.ResourceVersion
		_, reconcileErr := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(reconcileErr).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, dep)).To(Succeed())
		Expect(dep.ResourceVersion).To(Equal(revision))
		Expect(k8sClient.Get(ctx, key, svc)).To(Succeed())
		Expect(svc.ResourceVersion).To(Equal(serviceRevision))
		template := dep.Spec.Template.DeepCopy()
		pvc := &corev1.PersistentVolumeClaim{}
		Expect(k8sClient.Get(ctx, key, pvc)).To(Succeed())
		volumeUID := pvc.UID
		policy := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "workspace-boundary", Namespace: ns.Name}, policy)).To(Succeed())
		Expect(policy.Spec.Ingress).To(HaveLen(1))
		Expect(policy.Spec.Egress).To(HaveLen(2))
		Expect(policy.Spec.Ingress[0].From[0].NamespaceSelector.MatchLabels[namespaceNameLabel]).To(Equal("outfitter-cloud"))
		now = now.Add(2 * time.Minute)
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, dep)).To(Succeed())
		Expect(*dep.Spec.Replicas).To(Equal(int32(0)))
		Expect(dep.Spec.Template).To(Equal(*template))
		Expect(k8sClient.Get(ctx, key, pvc)).To(Succeed())
		Expect(pvc.UID).To(Equal(volumeUID))
		Expect(k8sClient.Get(ctx, key, w)).To(Succeed())
		w.Spec.AwakeUntil = metav1.NewTime(now.Add(time.Minute))
		Expect(k8sClient.Update(ctx, w)).To(Succeed())
		_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, dep)).To(Succeed())
		Expect(*dep.Spec.Replicas).To(Equal(int32(1)))
		// envtest has no storage-protection controller; simulate its release of an unused PVC.
		Expect(k8sClient.Get(ctx, key, pvc)).To(Succeed())
		pvc.Finalizers = nil
		Expect(k8sClient.Update(ctx, pvc)).To(Succeed())
		now = now.Add(2 * time.Hour)
		for range 8 {
			_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, w))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, pvc))).To(BeTrue())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(ns), ns)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(neighbor), neighbor)).To(Succeed())
	})
})
