package controller

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

const (
	githubWebhookMode           = "webhook"
	githubChannelSource         = "github"
	githubPATKey                = "GITHUB_PERSONAL_ACCESS_TOKEN"
	piSessionIDFlag             = "--session-id"
	piRPCMode                   = "rpc"
	piModeFlag                  = "--mode"
	catalogSyncContainerName    = "sync-agent-catalog"
	workflowExportContainerName = "export-workflow"
)

func agentConfigPath(agent *aioutfitterv1alpha1.Agent) string {
	if agent.Spec.Pi != nil {
		return path.Join(agentHome(agent), ".config/pi-runtime")
	}
	return path.Join(agentHome(agent), ".agents")
}

func renderPiSettings(agent *aioutfitterv1alpha1.Agent) (map[string]string, error) {
	spec := agent.Spec.Pi
	provider, model, ok := strings.Cut(spec.Model, "/")
	if !ok || provider == "" || model == "" {
		return nil, fmt.Errorf("pi.model must be provider/model")
	}
	if strings.TrimSpace(spec.SystemPrompt) == "" {
		return nil, fmt.Errorf("pi.systemPrompt must not be empty")
	}
	settings := map[string]any{"defaultProvider": provider, "defaultModel": model, "packages": spec.Packages, "quietStartup": true}
	if spec.Thinking != "" {
		settings["defaultThinkingLevel"] = spec.Thinking
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	data := map[string]string{"settings.json": string(raw), "SYSTEM.md": spec.SystemPrompt}
	if spec.Models != nil {
		if !json.Valid(spec.Models.Raw) {
			return nil, fmt.Errorf("pi.models must be valid JSON")
		}
		data[ModelsFile] = string(spec.Models.Raw)
	}

	return data, nil
}

func configureDirectPi(container *corev1.Container, agent *aioutfitterv1alpha1.Agent) {
	container.Command = []string{"pi"}
	container.Args = []string{piModeFlag, piRPCMode, piSessionIDFlag, agent.Name, "--no-approve", "--no-skills", "--no-prompt-templates", "--no-themes"}
	if agent.Spec.Forge != nil {
		container.Env = append(container.Env, corev1.EnvVar{Name: "GH_TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: agentCredentialSecretName(agent)}, Key: githubPATKey, Optional: ptr.To(true)}}})
	}
	container.Env = append(container.Env,
		corev1.EnvVar{Name: "PI_CODING_AGENT_DIR", Value: agentConfigPath(agent)},
		corev1.EnvVar{Name: "PI_CODING_AGENT_SESSION_DIR", Value: path.Join(agentHome(agent), ".pi/agent/sessions")},
		corev1.EnvVar{Name: "PI_TELEMETRY", Value: "0"},
	)
}
