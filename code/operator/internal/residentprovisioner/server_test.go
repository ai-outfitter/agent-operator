package residentprovisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	api "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func testConfig() Config {
	return Config{Token: strings.Repeat("server", 8), CatalogRepository: "ai-outfitter/community-profiles", CatalogRevision: strings.Repeat("a", 40), RuntimeImage: "ghcr.io/ai-outfitter/outfitter@sha256:" + strings.Repeat("b", 64), Model: "openai/test", ServiceOrigins: []string{"https://ai-outfitter.com"}, OperatorNamespace: "operator-system"}
}
func testRequest() Request {
	return Request{Generation: 1, Workspace: Workspace{ID: "user:123", Login: "owner", Type: "User"}, InstallationID: 12, Repositories: []Repository{{ID: 45, FullName: "owner/repo"}}, ProjectManagerName: "Project Manager", EngineerName: "Engineer", ServiceBaseURL: "https://ai-outfitter.com", ProjectManagerToken: strings.Repeat("manager", 8), EngineerToken: strings.Repeat("engineer", 8), TaskToken: strings.Repeat("task", 10)}
}
func testServer(t *testing.T, objects ...client.Object) *Server {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{api.AddToScheme, appsv1.AddToScheme, corev1.AddToScheme, networkingv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&api.Agent{}, &api.Organization{}).WithObjects(objects...).Build()
	server, err := New(kube, testConfig(), "")
	if err != nil {
		t.Fatal(err)
	}
	return server
}
func call(t *testing.T, s *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	req.Header.Set("Authorization", "Bearer "+s.config.Token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	return w
}
func provision(t *testing.T, s *Server) {
	t.Helper()
	w := call(t, s, "PUT", "/v1/residents/user%3A123", testRequest())
	if w.Code != 200 {
		t.Fatalf("provision: %d %s", w.Code, w.Body)
	}
}
func makeReady(t *testing.T, s *Server) {
	t.Helper()
	ctx := context.Background()
	org, pm, eng := names("user:123")
	organization := &api.Organization{}
	if err := s.kube.Get(ctx, client.ObjectKey{Name: org}, organization); err != nil {
		t.Fatal(err)
	}
	organization.Status.ObservedGeneration = organization.Generation
	organization.Status.Conditions = []metav1.Condition{{Type: api.AgentConditionReady, Status: metav1.ConditionTrue, ObservedGeneration: organization.Generation, Reason: api.AgentConditionReady, LastTransitionTime: metav1.Now()}}
	if err := s.kube.Status().Update(ctx, organization); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{pm, eng} {
		agent := &api.Agent{}
		if err := s.kube.Get(ctx, client.ObjectKey{Name: name}, agent); err != nil {
			t.Fatal(err)
		}
		deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: runtimeName, Namespace: "agent-" + name, Generation: 1}, Status: appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1}}
		if err := s.kube.Create(ctx, deployment); err != nil {
			t.Fatal(err)
		}
		agent.Status.ObservedGeneration = agent.Generation
		agent.Status.Conditions = []metav1.Condition{{Type: api.AgentConditionReady, Status: metav1.ConditionTrue, ObservedGeneration: agent.Generation, Reason: api.AgentConditionReady, LastTransitionTime: metav1.Now()}}
		if err := s.kube.Status().Update(ctx, agent); err != nil {
			t.Fatal(err)
		}
	}
}
func assertRoleAgent(t *testing.T, agent api.Agent, org, pm, eng string) {
	t.Helper()
	if agent.Spec.Memberships[0].Organization != org || agent.Spec.Profile.Harness != "pi" || agent.Spec.Profile.Model != "outfitter/openai/test" || agent.Spec.Workspace.Volume.Size.String() != "10Gi" {
		t.Fatal("incorrect resident spec")
	}
	if agent.Name == pm && (agent.Spec.TaskPlane == nil || agent.Spec.TaskPlane.Workflow != triageWorkflow || agent.Spec.Profile.Agent != "resident-project-manager") {
		t.Fatal("manager workflow missing")
	}
	if agent.Name == eng && (agent.Spec.TaskPlane != nil || agent.Spec.Profile.Agent != "resident-engineer") {
		t.Fatal("engineer must remain idle")
	}
	if len(agent.Spec.Setup) != 1 || !strings.Contains(agent.Spec.Setup[0].Script, "const ghWrapper =") || strings.Contains(agent.Spec.Setup[0].Script, testRequest().ProjectManagerToken) {
		t.Fatal("unsafe runtime setup")
	}
}

func TestIdempotentPairAndPersistence(t *testing.T) {
	s := testServer(t)
	provision(t, s)
	ctx := context.Background()
	org, pm, eng := names("user:123")
	agents := &api.AgentList{}
	if err := s.kube.List(ctx, agents); err != nil {
		t.Fatal(err)
	}
	if len(agents.Items) != 2 {
		t.Fatal("expected pair")
	}
	versions := map[string]string{}
	for _, agent := range agents.Items {
		versions[agent.Name] = agent.ResourceVersion
		assertRoleAgent(t, agent, org, pm, eng)
	}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "agent-workspace", Namespace: "agent-" + pm, Annotations: map[string]string{"sentinel": "retained"}}}
	if err := s.kube.Create(ctx, pvc); err != nil {
		t.Fatal(err)
	}
	provision(t, s)
	if err := s.kube.List(ctx, agents); err != nil {
		t.Fatal(err)
	}
	for _, agent := range agents.Items {
		if versions[agent.Name] != agent.ResourceVersion {
			t.Fatal("identical request changed Agent")
		}
	}
	if err := s.kube.Get(ctx, client.ObjectKeyFromObject(pvc), pvc); err != nil || pvc.Annotations["sentinel"] != "retained" {
		t.Fatal("PVC changed")
	}
	secret := &corev1.Secret{}
	if err := s.kube.Get(ctx, client.ObjectKey{Namespace: "agent-" + pm, Name: agentCredentials}, secret); err != nil {
		t.Fatal(err)
	}
	if string(secret.Data["OUTFITTER_RESIDENT_TOKEN"]) != testRequest().ProjectManagerToken || strings.Contains(string(secret.Data["a2a-credentials.json"]), testRequest().EngineerToken) {
		t.Fatal("credentials not role scoped")
	}
	if _, exists := secret.Data["OPENROUTER_API_KEY"]; exists {
		t.Fatal("upstream key must never be projected")
	}
	if string(secret.Data["PATH"]) != "/workspace/.hosted/bin:/usr/local/bin:/usr/bin:/bin" {
		t.Fatal("wrapper path not installed")
	}
	response := call(t, s, "GET", "/v1/residents/user:123", nil)
	if strings.Contains(response.Body.String(), testRequest().ProjectManagerToken) || strings.Contains(response.Body.String(), testRequest().TaskToken) {
		t.Fatal("status leaked credentials")
	}
}
func TestRejectForeignResourcesBeforeMutation(t *testing.T) {
	org, pm, _ := names("user:123")
	for _, foreign := range []client.Object{&api.Organization{ObjectMeta: metav1.ObjectMeta{Name: org}}, &corev1.Namespace{ObjectMeta: metadata("agent-"+pm, "", "org:456")}, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "organization-credentials", Namespace: "org-" + org}}} {
		s := testServer(t, foreign)
		w := call(t, s, "PUT", "/v1/residents/user:123", testRequest())
		if w.Code != 409 {
			t.Fatalf("expected collision: %d", w.Code)
		}
		agents := &api.AgentList{}
		if err := s.kube.List(context.Background(), agents); err != nil {
			t.Fatal(err)
		}
		if len(agents.Items) != 0 {
			t.Fatal("partial mutation on collision")
		}
	}
}
func TestAuthenticationAndValidation(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("PUT", "/v1/residents/user:123", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	invalid := []func(*Request){func(r *Request) { r.Workspace.ID = "org:123" }, func(r *Request) { r.Workspace.Type = "Organization" }, func(r *Request) { r.ServiceBaseURL = "https://attacker.example" }, func(r *Request) { r.ProjectManagerToken = "short" }, func(r *Request) { r.ProjectManagerName = strings.Repeat("x", 64) }, func(r *Request) { r.Repositories = append(r.Repositories, r.Repositories[0]) }, func(r *Request) { r.InstallationID = 0 }}
	for _, change := range invalid {
		r := testRequest()
		change(&r)
		if result := call(t, s, "PUT", "/v1/residents/user:123", r); result.Code != 400 {
			t.Fatalf("invalid request accepted: %s", result.Body)
		}
	}
	if result := call(t, s, "PUT", "/v1/residents/user:123", map[string]any{"runtimeImage": "evil"}); result.Code != 400 {
		t.Fatal("caller image accepted")
	}
	if result := call(t, s, "GET", "/v1/residents/user:123", nil); result.Code != 404 {
		t.Fatal(result.Code)
	}
	if result := call(t, s, "GET", "/v1/residents/not-a-workspace", nil); result.Code != 400 {
		t.Fatal(result.Code)
	}
}
func TestGenerationBoundReadinessAndCredentialRotation(t *testing.T) {
	s := testServer(t)
	provision(t, s)
	makeReady(t, s)
	state, err := s.state(context.Background(), "user:123")
	if err != nil || state.State != readyState {
		t.Fatalf("state %#v %v", state, err)
	}
	_, pm, _ := names("user:123")
	agent := &api.Agent{}
	if err := s.kube.Get(context.Background(), client.ObjectKey{Name: pm}, agent); err != nil {
		t.Fatal(err)
	}
	old := agent.Spec.Setup[0].Script
	agent.Generation++
	if err := s.kube.Update(context.Background(), agent); err != nil {
		t.Fatal(err)
	}
	state, err = s.state(context.Background(), "user:123")
	if err != nil || state.State != "provisioning" || state.Agents[0].Ready {
		t.Fatal("stale readiness accepted")
	}
	r := testRequest()
	r.Generation++
	r.ProjectManagerToken = strings.Repeat("rotated", 8)
	if response := call(t, s, "PUT", "/v1/residents/user:123", r); response.Code != 200 {
		t.Fatal(response.Code)
	}
	if err := s.kube.Get(context.Background(), client.ObjectKey{Name: pm}, agent); err != nil {
		t.Fatal(err)
	}
	if agent.Spec.Setup[0].Script == old {
		t.Fatal("credential rotation did not change pod setup")
	}
	agent.Status.ObservedGeneration = agent.Generation
	agent.Status.Conditions = []metav1.Condition{{Type: "Accepted", Status: metav1.ConditionFalse, ObservedGeneration: agent.Generation, Reason: "InvalidSpecification", LastTransitionTime: metav1.Now()}}
	if err := s.kube.Status().Update(context.Background(), agent); err != nil {
		t.Fatal(err)
	}
	state, err = s.state(context.Background(), "user:123")
	if err != nil || state.State != failedState {
		t.Fatal("failed state missing")
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTaskAcceptanceScopeAndDurableIdentity(t *testing.T) {
	s := testServer(t)
	provision(t, s)
	task := TaskRequest{ID: "github-delivery-123", Repository: testRequest().Repositories[0], IssueNumber: 9, AvailableLabels: []string{"bug"}, Message: "Triage this issue only."}
	if response := call(t, s, "POST", "/v1/residents/user:123/tasks", task); response.Code != 503 {
		t.Fatal("unready manager accepted task")
	}
	makeReady(t, s)
	seen := map[string]string{}
	calls := 0
	s.http = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "agent-runtime.agent-hosted-user-123-pm.svc.cluster.local:8788" || r.Header.Get("A2A-Version") != "1.0" || r.Header.Get("Authorization") != "Bearer "+testRequest().TaskToken {
			t.Fatal("incorrect manager route or auth")
		}
		payload, _ := io.ReadAll(r.Body)
		var input struct {
			Message struct {
				MessageID string `json:"messageId"`
			} `json:"message"`
		}
		if err := json.Unmarshal(payload, &input); err != nil {
			t.Fatal(err)
		}
		code := 200
		body := `{"task":{"id":"durable-task"}}`
		if prior, ok := seen[input.Message.MessageID]; ok && prior != string(payload) {
			code = 409
			body = `{}`
		}
		seen[input.Message.MessageID] = string(payload)
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	for range 2 {
		if response := call(t, s, "POST", "/v1/residents/user:123/tasks", task); response.Code != 202 {
			t.Fatalf("accept failed: %d %s", response.Code, response.Body)
		}
	}
	if calls != 2 || len(seen) != 1 {
		t.Fatal("retry identity changed")
	}
	task.Message = "different payload"
	if response := call(t, s, "POST", "/v1/residents/user:123/tasks", task); response.Code != 409 {
		t.Fatal("conflicting identity accepted")
	}
	task.Repository.ID = 999
	if response := call(t, s, "POST", "/v1/residents/user:123/tasks", task); response.Code != 403 {
		t.Fatal("foreign repository accepted")
	}
	if calls != 3 {
		t.Fatal("foreign task reached manager")
	}
}
func TestTaskRequiresExplicitAcceptedTask(t *testing.T) {
	s := testServer(t)
	provision(t, s)
	makeReady(t, s)
	task := TaskRequest{ID: "id", Repository: testRequest().Repositories[0], IssueNumber: 1, Message: "triage"}
	for _, body := range []string{`{}`, `{"message":{"messageId":"direct"}}`, `invalid`} {
		s.http = &http.Client{Transport: transportFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		if response := call(t, s, "POST", "/v1/residents/user:123/tasks", task); response.Code != 502 {
			t.Fatal("unaccepted message acknowledged")
		}
	}
}
func TestImmutableOperatorConfiguration(t *testing.T) {
	for _, change := range []func(*Config){func(c *Config) { c.CatalogRevision = "main" }, func(c *Config) { c.RuntimeImage = "image:latest" }, func(c *Config) { c.ServiceOrigins = []string{"http://remote.test"} }, func(c *Config) { c.ServiceOrigins = []string{"https://user:password@example.test"} }, func(c *Config) { c.Token = "short" }} {
		c := testConfig()
		change(&c)
		if c.Validate() == nil {
			t.Fatal("invalid operator configuration accepted")
		}
	}
	c := testConfig()
	c.ServiceOrigins = []string{"http://127.0.0.1:8080"}
	c.AllowLocalHTTP = true
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestProvisioningGenerationFencesDelayedReplica(t *testing.T) {
	s := testServer(t)
	provision(t, s)
	makeReady(t, s)
	old := testRequest()
	next := old
	next.Generation++
	next.EngineerName = "New engineer"
	desired := s.desired(next)
	// Simulate another replica stopping midway after publishing its newer fence.
	for _, obj := range desired[:3] {
		if err := s.apply(context.Background(), obj, old.Workspace.ID); err != nil {
			t.Fatal(err)
		}
	}
	state, err := s.state(context.Background(), old.Workspace.ID)
	if err != nil || state.State == readyState || state.Generation != 2 {
		t.Fatalf("partial rollout %#v %v", state, err)
	}
	if response := call(t, s, "PUT", "/v1/residents/user:123", old); response.Code != 409 {
		t.Fatalf("stale replica %d", response.Code)
	}
	if response := call(t, s, "PUT", "/v1/residents/user:123", next); response.Code != 200 {
		t.Fatalf("resume %d %s", response.Code, response.Body)
	}
	// A request that passed preflight before the newer replica must still fail
	// inside each individual object's resourceVersion-guarded update.
	for _, obj := range s.desired(old) {
		if err := s.apply(context.Background(), obj, old.Workspace.ID); !errors.Is(err, errStale) {
			t.Fatalf("late write %T: %v", obj, err)
		}
	}
	next.EngineerName = "Conflicting same generation"
	if response := call(t, s, "PUT", "/v1/residents/user:123", next); response.Code != 409 {
		t.Fatalf("generation reused %d", response.Code)
	}
}

func TestReadinessRejectsPreviousDeploymentGeneration(t *testing.T) {
	s := testServer(t)
	provision(t, s)
	makeReady(t, s)
	_, pm, _ := names("user:123")
	deployment := &appsv1.Deployment{}
	key := client.ObjectKey{Namespace: "agent-" + pm, Name: runtimeName}
	if err := s.kube.Get(context.Background(), key, deployment); err != nil {
		t.Fatal(err)
	}
	deployment.Generation++
	if err := s.kube.Update(context.Background(), deployment); err != nil {
		t.Fatal(err)
	}
	state, err := s.state(context.Background(), "user:123")
	if err != nil || state.State == readyState || state.Agents[0].Ready {
		t.Fatalf("old replica accepted %#v %v", state, err)
	}
}
