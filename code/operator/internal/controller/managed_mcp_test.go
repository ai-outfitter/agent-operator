package controller

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testManagedMcpSecret = "resident-mcp"
const testManagedMcpToken = "OUTFITTER_MANAGED_MCP_TOKEN"

func TestManagedMcpConformance(t *testing.T) {
	raw, err := os.ReadFile("testdata/managed-mcp-conformance.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string          `json:"name"`
		Valid    bool            `json:"valid"`
		Document json.RawMessage `json:"document"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			if validManagedMcp(test.Document) != test.Valid {
				t.Fatalf("validation disagrees with shared schema case: %s", test.Name)
			}
		})
	}
	if validManagedMcp([]byte("not JSON")) {
		t.Fatal("accepted malformed JSON")
	}
}

func TestManagedMcpCopiesAtomicWriterInputIntoActualPiConfiguration(t *testing.T) {
	agent := validAgent("resident", "example")
	agent.Spec.Pi = &aioutfitterv1alpha1.PiSpec{SystemPrompt: "Preserve instructions", Model: "provider/managed-model"}
	agent.Spec.ManagedMcp = &aioutfitterv1alpha1.ManagedMcpSpec{SecretName: testManagedMcpSecret}
	root := t.TempDir()
	source, target, settings := filepath.Join(root, "secret"), filepath.Join(root, "runtime"), filepath.Join(root, "settings")
	for _, dir := range []string{filepath.Join(source, "..2026"), target, settings} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	expected := `{"mcpServers":{"managed":{"url":"https://example.test"}}}`
	if err := os.WriteFile(filepath.Join(source, "..2026", managedMcpConfigKey), []byte(expected), 0600); err != nil {
		t.Fatal(err)
	}
	for _, link := range []struct{ Target, Name string }{{"..2026", "..data"}, {"..data/mcp.json", "mcp.json"}} {
		if err := os.Symlink(link.Target, filepath.Join(source, link.Name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(settings, "settings.json"), []byte(`{"defaultModel":"preserved"}`), 0600); err != nil {
		t.Fatal(err)
	}
	inits := []corev1.Container{copySettingsContainer("pi:test", agent)}
	container, volumes := corev1.Container{}, []corev1.Volume{}
	configureManagedMcp(&container, &volumes, inits, agent, nil)
	script := strings.NewReplacer(SettingsMountPath, settings, managedMcpMount, source, agentConfigPath(agent), target).Replace(inits[0].Command[2])
	if output, err := exec.Command("sh", "-c", script).CombinedOutput(); err != nil {
		t.Fatalf("copy-settings: %v %s", err, output)
	}
	got, err := os.ReadFile(filepath.Join(target, managedMcpConfigKey))
	if err != nil || string(got) != expected {
		t.Fatal("Secret AtomicWriter input was not copied", err)
	}
	stat, err := os.Stat(filepath.Join(target, managedMcpConfigKey))
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatal("MCP configuration must be private", err)
	}
	if raw, err := os.ReadFile(filepath.Join(target, "settings.json")); err != nil || string(raw) != `{"defaultModel":"preserved"}` {
		t.Fatal("unrelated native settings changed", err)
	}
}

func TestManagedMcpRejectsCredentialCollisions(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	agent := validAgent("resident", "example")
	agent.Spec.ManagedMcp = &aioutfitterv1alpha1.ManagedMcpSpec{SecretName: testManagedMcpSecret}
	for _, test := range []struct{ name, key, standard string }{
		{"reserved", "NODE_OPTIONS", ""}, {"config", "OUTFITTER_MANAGED_MCP_CONFIG", ""}, {"collision", testManagedMcpToken, testManagedMcpToken},
	} {
		t.Run(test.name, func(t *testing.T) {
			managed := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: testManagedMcpSecret, Namespace: agentNamespace(agent.Name)}, Data: map[string][]byte{managedMcpConfigKey: []byte(`{"mcpServers":{}}`), test.key: []byte("private")}}
			standard := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: defaultAgentCredentials, Namespace: agentNamespace(agent.Name)}, Data: map[string][]byte{}}
			if test.standard != "" {
				standard.Data[test.standard] = []byte("existing")
			}
			r := &AgentReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(managed, standard).Build()}
			env, message, err := r.managedMcpEnvironment(context.Background(), agent)
			if err != nil || message == "" || len(env) != 0 || strings.Contains(message, "private") {
				t.Fatal("unsafe credentials accepted or disclosed", message, err)
			}
		})
	}
	agent.Spec.ManagedMcp.SecretName = defaultAgentCredentials
	r := &AgentReconciler{}
	if _, message, err := r.managedMcpEnvironment(context.Background(), agent); err != nil || !strings.Contains(message, "dedicated") {
		t.Fatal("accepted shared credential Secret", message, err)
	}
}
