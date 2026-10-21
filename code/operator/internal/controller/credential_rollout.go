package controller

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

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
