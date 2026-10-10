package controller

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

// The inference relay sidecar is the runtime's only route to inference. It presents a projected
// ServiceAccount token bound to inferenceAudience, mounted into the sidecar only, so the runtime
// container never holds the Pod's inference identity. The runtime reaches it over Pod-shared loopback.
// Workspace Pods and resident Agent Deployments share this sidecar.
const (
	inferenceRelayName             = "inference"
	inferenceTokenVolumeName       = "inference-token"
	inferenceAudience              = "outfitter-inference"
	inferenceTokenMountPath        = "/var/run/secrets/outfitter/inference"
	inferenceTokenFile             = "token"
	inferenceRelayPort       int32 = 4141
)

// inferenceTokenExpirationSeconds is the projected token lifetime; the kubelet rotates it and the
// relay re-reads the file on every request.
var inferenceTokenExpirationSeconds = ptr.To[int64](3600)

// inferenceRelayCommand starts the relay shipped in the webapp image.
var inferenceRelayCommand = []string{"node", "workspace/inference-relay.mjs"}

// inferenceRelayURL is the relay's loopback address as seen from any container in the Pod.
var inferenceRelayURL = "http://127.0.0.1:" + strconv.Itoa(int(inferenceRelayPort))

// restrictedContainerSecurity is the container SecurityContext the relay runs with.
func restrictedContainerSecurity() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(true),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
}

// inferenceTokenVolume projects a ServiceAccount token bound to the inference audience. Only the
// relay container mounts it.
func inferenceTokenVolume() corev1.Volume {
	return corev1.Volume{Name: inferenceTokenVolumeName, VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
		Sources: []corev1.VolumeProjection{{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
			Audience: inferenceAudience, ExpirationSeconds: inferenceTokenExpirationSeconds, Path: inferenceTokenFile,
		}}},
	}}}
}

// inferenceRelayContainer forwards loopback chat completions to gatewayURL with the projected token.
func inferenceRelayContainer(image, gatewayURL string) corev1.Container {
	return corev1.Container{
		Name: inferenceRelayName, Image: image, ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         inferenceRelayCommand,
		SecurityContext: restrictedContainerSecurity(),
		Env: []corev1.EnvVar{
			{Name: "INFERENCE_GATEWAY_URL", Value: gatewayURL},
			{Name: "INFERENCE_TOKEN_FILE", Value: inferenceTokenMountPath + "/" + inferenceTokenFile},
			{Name: "PORT", Value: strconv.Itoa(int(inferenceRelayPort))},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("256Mi")},
		},
		VolumeMounts: []corev1.VolumeMount{{Name: inferenceTokenVolumeName, MountPath: inferenceTokenMountPath, ReadOnly: true}},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler:  corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt32(inferenceRelayPort), Scheme: corev1.URISchemeHTTP}},
			PeriodSeconds: 3, TimeoutSeconds: 2, SuccessThreshold: 1, FailureThreshold: 3,
		},
	}
}
