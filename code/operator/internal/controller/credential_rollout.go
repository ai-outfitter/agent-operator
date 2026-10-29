package controller

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const credentialHashAnnotation = "aioutfitter.com/transport-credentials-hash"

// Only hash the keys consumed at transport startup. Unrelated credentials
// must not restart the resident or gateway.
func transportCredentialHash(ctx context.Context, c client.Client, namespace, name string, keys ...string) (string, error) {
	secret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	selected := make(map[string][]byte, len(keys))
	if len(keys) == 0 {
		selected = secret.Data
	}
	for _, key := range keys {
		selected[key] = secret.Data[key]
	}
	data, err := json.Marshal(selected)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func agentTransportCredentialHash(ctx context.Context, c client.Client, agent *aioutfitterv1alpha1.Agent) (string, error) {
	namespace := agentNamespace(agent.Name)
	transportHash := ""
	if agent.Spec.Pi != nil || agent.Spec.Forge != nil || agent.Spec.TaskPlane != nil {
		keys := []string{agentA2ACredentialsKey}
		if agent.Spec.Pi != nil {
			keys = nil
		}
		var err error
		transportHash, err = transportCredentialHash(ctx, c, namespace, agentCredentialSecretName(agent), keys...)
		if err != nil {
			return "", err
		}
	}
	if managed := agent.Spec.ManagedMcp; managed != nil {
		managedHash, err := transportCredentialHash(ctx, c, namespace, managed.SecretName)
		if err != nil {
			return "", err
		}
		transportHash = credentialDigest([]byte(transportHash + managedHash))
	}
	return transportHash, nil
}
