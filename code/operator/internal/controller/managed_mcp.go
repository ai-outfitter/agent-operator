package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	managedMcpVolumeName = "managed-mcp"
	managedMcpMount      = "/var/run/agent/managed-mcp"
	managedMcpConfigKey  = "mcp.json"
	managedMcpConfigEnv  = "OUTFITTER_MANAGED_MCP_CONFIG"
	managedMcpEnvPrefix  = "OUTFITTER_MANAGED_MCP_"
)

// Mirrors Outfitter's managed-mcp.schema.json; the same conformance fixtures
// exercise both packages. Unknown native fields remain the runtime's concern.
func validManagedMcp(raw []byte) bool {
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return false
	}
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		return false
	}
	for name, value := range servers {
		server, ok := value.(map[string]any)
		if name == "" || !ok || !validManagedMcpServer(server) {
			return false
		}
	}
	return true
}

func validManagedMcpServer(server map[string]any) bool {
	hasTransport := false
	for _, key := range []string{"command", "url"} {
		if value, exists := server[key]; exists {
			text, ok := value.(string)
			if !ok || text == "" {
				return false
			}
			hasTransport = true
		}
	}
	if !hasTransport {
		return false
	}
	if value, exists := server["args"]; exists {
		args, ok := value.([]any)
		if !ok {
			return false
		}
		for _, arg := range args {
			if _, ok := arg.(string); !ok {
				return false
			}
		}
	}
	for _, key := range []string{"env", "headers"} {
		if value, exists := server[key]; exists {
			values, ok := value.(map[string]any)
			if !ok {
				return false
			}
			for _, value := range values {
				if _, ok := value.(string); !ok {
					return false
				}
			}
		}
	}
	return true
}

func (r *AgentReconciler) managedMcpEnvironment(ctx context.Context, agent *aioutfitterv1alpha1.Agent) ([]corev1.EnvVar, string, error) {
	managed := agent.Spec.ManagedMcp
	if managed == nil {
		return nil, "", nil
	}
	if managed.SecretName == agentCredentialSecretName(agent) {
		return nil, "managedMcp must use a dedicated Secret, not the Agent credential Secret", nil
	}
	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}
	secret := &corev1.Secret{}
	if err := reader.Get(ctx, client.ObjectKey{Namespace: agentNamespace(agent.Name), Name: managed.SecretName}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, "", nil
		}
		return nil, "", err
	}
	if !validManagedMcp(secret.Data[managedMcpConfigKey]) {
		return nil, "managedMcp Secret must contain mcp.json matching the native MCP schema", nil
	}
	existing, err := r.credentialEnvironmentKeys(ctx, agent)
	if err != nil {
		return nil, "", err
	}
	names := make([]string, 0, len(secret.Data))
	for name := range secret.Data {
		if name != managedMcpConfigKey {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	env := make([]corev1.EnvVar, 0, len(names))
	for _, name := range names {
		if !strings.HasPrefix(name, managedMcpEnvPrefix) || name == managedMcpConfigEnv || existing[name] {
			return nil, fmt.Sprintf("managedMcp credential key %q must use an unoccupied OUTFITTER_MANAGED_MCP_ name other than CONFIG", name), nil
		}
		env = append(env, corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: managed.SecretName}, Key: name}}})
	}
	return env, "", nil
}

func configureManagedMcp(container *corev1.Container, volumes *[]corev1.Volume, initContainers []corev1.Container, agent *aioutfitterv1alpha1.Agent, env []corev1.EnvVar) {
	managed := agent.Spec.ManagedMcp
	if managed == nil {
		return
	}
	mount := corev1.VolumeMount{Name: managedMcpVolumeName, MountPath: managedMcpMount, ReadOnly: true}
	container.Env = append(container.Env, env...)
	*volumes = append(*volumes, corev1.Volume{Name: managedMcpVolumeName, VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: managed.SecretName, Items: []corev1.KeyToPath{{Key: managedMcpConfigKey, Path: managedMcpConfigKey}}}}})
	if agent.Spec.Pi == nil {
		container.Env = append(container.Env, corev1.EnvVar{Name: managedMcpConfigEnv, Value: path.Join(managedMcpMount, managedMcpConfigKey)})
		container.VolumeMounts = append(container.VolumeMounts, mount)
		return
	}
	// Pi's configuration is rebuilt in this emptyDir on every boot. No shadow
	// PVC file, ownership journal, JavaScript merge, or extra init container.
	for i := range initContainers {
		if initContainers[i].Name == copySettingsContainerName {
			initContainers[i].VolumeMounts = append(initContainers[i].VolumeMounts, mount)
			destination := path.Join(agentConfigPath(agent), managedMcpConfigKey)
			initContainers[i].Command[2] += " && cp -L " + path.Join(managedMcpMount, managedMcpConfigKey) + " " + destination + " && chmod 600 " + destination
		}
	}
}
