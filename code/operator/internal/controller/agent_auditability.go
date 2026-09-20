package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
)

type pensieveProbeState string

const (
	pensieveProbeNotRequested pensieveProbeState = "not-requested"
	pensieveProbeRunning      pensieveProbeState = "running"
	pensieveProbeSucceeded    pensieveProbeState = "succeeded"
	pensieveProbeFailed       pensieveProbeState = "failed"
)

const pensieveProbePrompt = "Use the read tool exactly once to read /workspace/probe/input.txt. " +
	"Think briefly about the value, then reply with the exact file contents and no other text."

func (r *AgentReconciler) ensurePensieveProbe(
	ctx context.Context,
	agent *aioutfitterv1alpha1.Agent,
	configuration *managedPensieveConfiguration,
) (*aioutfitterv1alpha1.AgentAuditabilityProbeStatus, pensieveProbeState, error) {
	if agent.Spec.Auditability == nil || agent.Spec.Auditability.ProbeNonce == "" {
		return nil, pensieveProbeNotRequested, nil
	}
	seed := fmt.Sprintf("%s:%d:%s:%s:%s", agent.UID, agent.Generation,
		agent.Spec.Auditability.ProbeNonce, configuration.CollectorRevision, configuration.PolicyDigest)
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(seed)))
	run := "resident-audit-" + digest[:32]
	jobName := "pensieve-probe-" + digest[:20]
	if current := agent.Status.Auditability; current != nil &&
		current.ObservedGeneration == agent.Generation &&
		current.CollectorImage == configuration.CollectorImage &&
		current.CollectorRevision == configuration.CollectorRevision &&
		current.PolicyDigest == configuration.PolicyDigest && current.Probe != nil &&
		current.Probe.Nonce == agent.Spec.Auditability.ProbeNonce &&
		current.Probe.Run == run && current.Probe.Succeeded {
		job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: agentNamespace(agent.Name)}}
		if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil && !apierrors.IsNotFound(err) {
			return nil, pensieveProbeFailed, fmt.Errorf("delete completed Pensieve probe Job: %w", err)
		}
		status := *current.Probe
		return &status, pensieveProbeSucceeded, nil
	}
	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}
	credentialSecret := &corev1.Secret{}
	credentialKey := types.NamespacedName{
		Namespace: agentNamespace(agent.Name), Name: r.PensieveProbeCredentialSecret,
	}
	if r.PensieveProbeCredentialSecret == "" {
		return nil, pensieveProbeFailed, fmt.Errorf("Pensieve probe credential Secret is not configured")
	}
	if err := reader.Get(ctx, credentialKey, credentialSecret); err != nil {
		return nil, pensieveProbeFailed, fmt.Errorf("read Pensieve probe credential Secret: %w", err)
	}
	provider, model, found := strings.Cut(agent.Spec.Profile.Model, "/")
	if !found || provider == "" || model == "" {
		return nil, pensieveProbeFailed, fmt.Errorf("audited Agent profile.model must select provider/model")
	}
	runtimeArgs := []string{
		"run", PensieveProbeAgentName, "--strict", "--",
		"--print", pensieveProbePrompt,
		"--session-id", run,
		"--provider", provider, "--model", model,
		"--thinking", "medium",
	}
	workspaceVolumeName := "pensieve-probe-workspace"
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: agentNamespace(agent.Name)}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, job, func() error {
		job.Labels = mergeLabels(job.Labels, ownershipLabels(agent))
		if err := controllerutil.SetControllerReference(agent, job, r.Scheme); err != nil {
			return err
		}
		job.Spec.BackoffLimit = ptr.To[int32](0)
		job.Spec.ActiveDeadlineSeconds = ptr.To[int64](900)
		job.Spec.Template.Labels = mergeLabels(job.Spec.Template.Labels, ownershipLabels(agent))
		job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
		job.Spec.Template.Spec.ServiceAccountName = RuntimeName
		job.Spec.Template.Spec.AutomountServiceAccountToken = ptr.To(false)
		job.Spec.Template.Spec.SecurityContext = &corev1.PodSecurityContext{
			RunAsNonRoot: ptr.To(true), RunAsUser: agentFSGroup, RunAsGroup: agentFSGroup, FSGroup: agentFSGroup,
		}
		job.Spec.Template.Spec.InitContainers = []corev1.Container{
			{
				Name: PensieveCollectorInitName, Image: configuration.CollectorImage,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command:         []string{"sh", "-c", "set -eu; cp -a " + PensieveCollectorSourcePath + "/. /managed/"},
				VolumeMounts:    []corev1.VolumeMount{{Name: PensieveCollectorVolumeName, MountPath: "/managed"}},
			},
			{
				Name: "prepare-audit-probe", Image: r.agentImage(agent), ImagePullPolicy: corev1.PullIfNotPresent,
				Command: []string{"sh", "-c", "set -eu; mkdir -p /workspace/probe; " +
					"printf 'pensieve-audit-probe:%s\\n' \"$PROBE_NONCE\" > /workspace/probe/input.txt"},
				Env:          []corev1.EnvVar{{Name: "PROBE_NONCE", Value: agent.Spec.Auditability.ProbeNonce}},
				VolumeMounts: []corev1.VolumeMount{{Name: workspaceVolumeName, MountPath: WorkspaceMount}},
			},
		}
		job.Spec.Template.Spec.Containers = []corev1.Container{{
			Name: "audit-probe", Image: r.agentImage(agent), ImagePullPolicy: corev1.PullIfNotPresent,
			Args: runtimeArgs,
			EnvFrom: []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: r.PensieveProbeCredentialSecret},
			}}},
			Env: []corev1.EnvVar{
				{Name: HomeEnvName, Value: WorkspaceMount},
				{Name: "PENSIEVE_RUN", Value: run},
				// The probe workspace is ephemeral. It must not report success while
				// evidence remains only in a spool that Kubernetes will delete.
				{Name: "PENSIEVE_FAIL_CLOSED", Value: PensieveProbeFailClosed},
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: workspaceVolumeName, MountPath: WorkspaceMount},
				{Name: PensieveProbeProfileVolumeName, MountPath: PensieveProbeProfileMountPath, ReadOnly: true},
				{Name: PensieveTokenVolumeName, MountPath: PensieveTokenMountPath, ReadOnly: true},
				{Name: PensieveCollectorVolumeName, MountPath: PensieveCollectorMountPath, ReadOnly: true},
				{Name: PensieveHookVolumeName, MountPath: PensieveHookMountPath, ReadOnly: true},
			},
		}}
		job.Spec.Template.Spec.Volumes = []corev1.Volume{
			{Name: workspaceVolumeName, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			{Name: PensieveCollectorVolumeName, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			pensieveTokenVolume(),
			{Name: PensieveHookVolumeName, VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: PensieveHookConfigMapName},
				Items:                []corev1.KeyToPath{{Key: PensieveHookFileName, Path: PensieveHookFileName}},
			}}},
			{Name: PensieveProbeProfileVolumeName, VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: PensieveHookConfigMapName},
				Items: []corev1.KeyToPath{
					{Key: PensieveProbeSettingsFileName, Path: "settings.yml"},
					{Key: PensieveProbeModelsFileName, Path: "models.json"},
					{Key: PensieveProbeAgentFileName, Path: "catalog/agents/audit-probe/agent.md"},
				},
			}}},
		}
		return nil
	})
	if err != nil {
		return nil, pensieveProbeFailed, err
	}
	startedAt := job.CreationTimestamp
	if job.Status.StartTime != nil {
		startedAt = *job.Status.StartTime
	}
	status := &aioutfitterv1alpha1.AgentAuditabilityProbeStatus{
		Nonce: agent.Spec.Auditability.ProbeNonce, Run: run, StartedAt: startedAt,
	}
	if job.Status.Succeeded > 0 {
		status.Succeeded = true
		status.CompletedAt = job.Status.CompletionTime
		return status, pensieveProbeSucceeded, nil
	}
	if job.Status.Failed > 0 {
		status.CompletedAt = job.Status.CompletionTime
		for _, condition := range job.Status.Conditions {
			if condition.Type == batchv1.JobFailed {
				status.Message = condition.Message
				break
			}
		}
		if status.Message == "" {
			status.Message = "Pensieve audit probe Job failed"
		}
		return status, pensieveProbeFailed, nil
	}
	return status, pensieveProbeRunning, nil
}
