package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
)

const (
	credentialDefaultPrefix       = "default."
	inheritedCredentialAnnotation = "aioutfitter.com/inherited-credential-hashes"
)

func credentialDigest(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func (r *AgentReconciler) ensureStandardAgentCredentials(
	ctx context.Context,
	agent *aioutfitterv1alpha1.Agent,
	organization *aioutfitterv1alpha1.Organization,
) ([]string, error) {
	organizationSecret := &corev1.Secret{}
	organizationKey := client.ObjectKey{
		Namespace: organizationNamespace(organization.Name),
		Name:      organizationCredentialSecretName(organization),
	}
	if err := r.Get(ctx, organizationKey, organizationSecret); err != nil {
		if apierrors.IsNotFound(err) {
			return []string{fmt.Sprintf("Secret/%s/%s", organizationKey.Namespace, organizationKey.Name)}, nil
		}
		return nil, err
	}

	defaults := map[string][]byte{}
	for key, value := range organizationSecret.Data {
		if !strings.HasPrefix(key, credentialDefaultPrefix) {
			continue
		}
		target := strings.TrimPrefix(key, credentialDefaultPrefix)
		if target == "" {
			return nil, fmt.Errorf("organization credential key %q has an empty inherited name", key)
		}
		if target == agentA2ACredentialsKey {
			return nil, fmt.Errorf("organization credential key %q targets reserved Agent key %q", key, target)
		}
		defaults[target] = value
	}

	agentSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Namespace: agentNamespace(agent.Name),
		Name:      agentCredentialSecretName(agent),
	}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, agentSecret, func() error {
		agentSecret.Labels = mergeLabels(agentSecret.Labels, ownershipLabels(agent))
		if agentSecret.Data == nil {
			agentSecret.Data = map[string][]byte{}
		}
		if agentSecret.Annotations == nil {
			agentSecret.Annotations = map[string]string{}
		}
		inherited := map[string]string{}
		if raw := agentSecret.Annotations[inheritedCredentialAnnotation]; raw != "" {
			if err := json.Unmarshal([]byte(raw), &inherited); err != nil {
				return fmt.Errorf("parse inherited credential state: %w", err)
			}
		}

		for key, previousDigest := range inherited {
			current, exists := agentSecret.Data[key]
			if !exists || credentialDigest(current) != previousDigest {
				delete(inherited, key)
			}
		}
		for key, value := range defaults {
			current, exists := agentSecret.Data[key]
			previousDigest, wasInherited := inherited[key]
			switch {
			case !exists:
				agentSecret.Data[key] = value
				inherited[key] = credentialDigest(value)
			case wasInherited && credentialDigest(current) == previousDigest:
				agentSecret.Data[key] = value
				inherited[key] = credentialDigest(value)
			}
		}
		for key, previousDigest := range inherited {
			if _, stillDefaulted := defaults[key]; stillDefaulted {
				continue
			}
			if current, exists := agentSecret.Data[key]; exists && credentialDigest(current) == previousDigest {
				delete(agentSecret.Data, key)
			}
			delete(inherited, key)
		}
		if len(inherited) == 0 {
			delete(agentSecret.Annotations, inheritedCredentialAnnotation)
			return nil
		}
		encoded, err := json.Marshal(inherited)
		if err != nil {
			return err
		}
		agentSecret.Annotations[inheritedCredentialAnnotation] = string(encoded)
		return nil
	}); err != nil {
		return nil, err
	}
	return nil, nil
}
