package residentprovisioner

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	api "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var errStale = errors.New("stale provisioning generation")

var errCollision = errors.New("resource ownership collision")

type Server struct {
	kube    client.Client
	config  Config
	http    *http.Client
	address string
}

func New(kube client.Client, config Config, address string) (*Server, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Server{kube: kube, config: config, address: address, http: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// Start participates in manager shutdown. There is no listener unless explicitly configured.
func (s *Server) Start(ctx context.Context) error {
	server := &http.Server{Addr: s.address, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	err := server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func (s *Server) NeedLeaderElection() bool { return false }
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /v1/residents/{workspace}", s.put)
	mux.HandleFunc("GET /v1/residents/{workspace}", s.get)
	mux.HandleFunc("POST /v1/residents/{workspace}/tasks", s.task)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.config.Token)) != 1 {
			writeError(w, 401, "unauthorized")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}
func decode(w http.ResponseWriter, r *http.Request, value any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func failure(w http.ResponseWriter, err error) {
	code, status := "provisioning_failed", 500
	if errors.Is(err, errStale) || apierrors.IsConflict(err) {
		code, status = "stale_generation", 409
	} else if errors.Is(err, errCollision) {
		code, status = "resource_collision", 409
	} else if apierrors.IsNotFound(err) {
		code, status = "not_found", 404
	}
	writeError(w, status, code)
}
func (s *Server) put(w http.ResponseWriter, r *http.Request) {
	var input Request
	if decode(w, r, &input) != nil || s.config.validateRequest(r.PathValue("workspace"), input) != nil {
		writeError(w, 400, "invalid_request")
		return
	}
	if err := s.reconcile(r.Context(), input); err != nil {
		failure(w, err)
		return
	}
	state, err := s.state(r.Context(), input.Workspace.ID)
	if err != nil {
		failure(w, err)
		return
	}
	if state.Generation != input.Generation {
		failure(w, errStale)
		return
	}
	writeJSON(w, 200, state)
}
func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspace")
	if !workspacePattern.MatchString(workspace) {
		writeError(w, 400, "invalid_workspace")
		return
	}
	state, err := s.state(r.Context(), workspace)
	if err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, state)
}

type TaskRequest struct {
	ID              string     `json:"id"`
	Repository      Repository `json:"repository"`
	IssueNumber     int64      `json:"issueNumber"`
	AvailableLabels []string   `json:"availableLabels"`
	Message         string     `json:"message"`
}

func validTask(input TaskRequest) bool {
	if input.ID == "" || len(input.ID) > 200 || input.IssueNumber <= 0 || input.Message == "" || len(input.Message) > 64000 || len(input.AvailableLabels) > 100 {
		return false
	}
	for _, label := range input.AvailableLabels {
		if len(label) > 100 {
			return false
		}
	}
	return true
}

func (s *Server) task(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspace")
	var input TaskRequest
	if !workspacePattern.MatchString(workspace) || decode(w, r, &input) != nil || !validTask(input) {
		writeError(w, 400, "invalid_task")
		return
	}
	orgName, manager, _ := names(workspace)
	org := &api.Organization{}
	if err := s.kube.Get(r.Context(), client.ObjectKey{Name: orgName}, org); err != nil {
		failure(w, err)
		return
	}
	if !owned(org, workspace) {
		failure(w, errCollision)
		return
	}
	var repositories []Repository
	if json.Unmarshal([]byte(org.Annotations[repositoriesAnnotation]), &repositories) != nil {
		failure(w, errors.New("invalid scope"))
		return
	}
	selected := false
	for _, repo := range repositories {
		if repo.ID == input.Repository.ID && repo.FullName == input.Repository.FullName {
			selected = true
		}
	}
	if !selected {
		writeError(w, 403, "repository_not_selected")
		return
	}
	state, err := s.state(r.Context(), workspace)
	if err != nil {
		failure(w, err)
		return
	}
	if !state.Agents[0].Ready {
		writeError(w, 503, "manager_not_ready")
		return
	}
	secret := &corev1.Secret{}
	if err := s.kube.Get(r.Context(), client.ObjectKey{Namespace: "agent-" + manager, Name: agentCredentials}, secret); err != nil {
		failure(w, err)
		return
	}
	if !owned(secret, workspace) {
		failure(w, errCollision)
		return
	}
	var credentials struct {
		Credentials []struct {
			Token     string `json:"token"`
			Principal string `json:"principal"`
		} `json:"credentials"`
	}
	if json.Unmarshal(secret.Data["a2a-credentials.json"], &credentials) != nil || len(credentials.Credentials) != 1 || credentials.Credentials[0].Principal != "hosted:"+workspace {
		failure(w, errors.New("invalid intake credential"))
		return
	}
	// The exact task ID and canonical payload survive retries. Channels owns durable principal+messageId deduplication.
	payload, _ := json.Marshal(map[string]any{
		"workflow": triageWorkflow, "delivery_id": input.ID, "available_labels": input.AvailableLabels,
		"workspace": workspace, "repository": input.Repository, "issue_number": input.IssueNumber, "message": input.Message,
		"comment_marker": "<!-- outfitter-triage:" + input.ID + " -->",
	})
	body, _ := json.Marshal(map[string]any{"message": map[string]any{"messageId": input.ID, "role": "ROLE_USER", "parts": []map[string]string{{"text": string(payload)}}}, "configuration": map[string]bool{"returnImmediately": true}})
	endpoint := fmt.Sprintf("http://agent-runtime.agent-%s.svc.cluster.local:8788/message:send", manager)
	request, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		failure(w, err)
		return
	}
	request.Header.Set("Authorization", "Bearer "+credentials.Credentials[0].Token)
	request.Header.Set("Content-Type", "application/a2a+json")
	request.Header.Set("A2A-Version", "1.0")
	response, err := s.http.Do(request)
	if err != nil {
		writeError(w, 502, "intake_unavailable")
		return
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == 409 {
		writeError(w, 409, "task_identity_conflict")
		return
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		writeError(w, 502, "intake_unavailable")
		return
	}
	var result struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result) != nil || strings.TrimSpace(result.Task.ID) == "" {
		writeError(w, 502, "intake_not_accepted")
		return
	}
	writeJSON(w, 202, map[string]any{"accepted": true, "taskId": result.Task.ID})
}
