package forgegateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

const (
	testGitHubSecret = "github-test-secret"
	testLuce         = "luce"
	testVega         = "vega"
)

func TestGitHubWorkflowRouting(t *testing.T) {
	// A fresh gateway has no route-state history: assignment actions must still
	// target only the changed assignee, and comments must not become assignments.
	g := &Gateway{routes: map[string]Route{testLuce: {}, testVega: {}}}
	tests := []struct {
		name, kind, payload string
		want                map[string]string
	}{
		{"assignment", eventIssues, `{"action":"assigned","assignee":{"login":"luce"},"issue":{"number":38,"assignees":[{"login":"luce"},{"login":"vega"}]}}`, map[string]string{testLuce: workflowImplement}},
		{"unrelated assignment", eventIssues, `{"action":"assigned","assignee":{"login":"human"},"issue":{"number":38,"assignees":[{"login":"luce"},{"login":"human"}]}}`, nil},
		{"new assigned issue", eventIssues, `{"action":"opened","issue":{"number":38,"assignees":[{"login":"luce"}],"body":"@luce implement this"}}`, map[string]string{testLuce: workflowImplement}},
		{"ordinary comment on old assignment", eventIssueComment, `{"action":"created","issue":{"number":38,"assignees":[{"login":"luce"},{"login":"vega"}]},"comment":{"body":"Here is a proposed direction"}}`, nil},
		{"explicit question", eventIssueComment, `{"action":"created","issue":{"number":38,"assignees":[{"login":"luce"}]},"comment":{"body":"@luce what do you think?"}}`, map[string]string{testLuce: workflowRespond}},
		{"reply on agent thread", eventIssueComment, `{"action":"created","issue":{"number":38,"user":{"login":"luce"}},"comment":{"body":"Can you explain this?"}}`, map[string]string{testLuce: workflowRespond}},
		{"no self reply", eventIssueComment, `{"action":"created","sender":{"login":"luce"},"issue":{"number":38,"user":{"login":"luce"}},"comment":{"body":"@luce done"}}`, nil},
		{"review request", eventPullRequest, `{"action":"review_requested","requested_reviewer":{"login":"vega"},"pull_request":{"number":43,"head":{"sha":"abc"}}}`, map[string]string{testVega: workflowReview}},
		{"review feedback", eventReview, `{"action":"submitted","pull_request":{"number":43,"user":{"login":"luce"}},"review":{"state":"changes_requested","commit_id":"abc","body":"Fix this"}}`, map[string]string{testLuce: workflowRevise}},
		{"review question", eventReview, `{"action":"submitted","pull_request":{"number":43,"user":{"login":"luce"}},"review":{"state":"commented","body":"Why this approach?"}}`, map[string]string{testLuce: workflowRespond}},
		{"edited issue is not assignment", eventIssues, `{"action":"edited","issue":{"number":38,"assignees":[{"login":"luce"}]}}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var e event
			if err := json.Unmarshal([]byte(tt.payload), &e); err != nil {
				t.Fatal(err)
			}
			got := g.githubTargets(tt.kind, e)
			if len(got) != len(tt.want) {
				t.Fatalf("targets=%+v want=%v", got, tt.want)
			}
			for user, workflow := range tt.want {
				if got[user].Workflow != workflow {
					t.Fatalf("%s=%+v want %s", user, got[user], workflow)
				}
			}
		})
	}
}

func TestGitHubSignedIntakeAndDurableReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool.db")
	cfg := Config{Provider: providerGitHub, Organization: "unsupervised", Owner: "Unsupervisedcom", Secret: testGitHubSecret, SpoolPath: path, Routes: []Route{{Username: testLuce, URL: "http://127.0.0.1:1", Token: "test"}}}
	g, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"action":"assigned","repository":{"full_name":"Unsupervisedcom/2119","owner":{"login":"Unsupervisedcom"}},"issue":{"number":38},"assignee":{"login":"luce"}}`
	request := func(secret string) *http.Request {
		r := signedRequest(t, body, "github-delivery", secret)
		r.URL.Path = "/webhooks/github"
		r.Header.Set("X-GitHub-Event", eventIssues)
		r.Header.Set("X-GitHub-Delivery", "github-delivery")
		r.Header.Set("X-Hub-Signature-256", "sha256="+r.Header.Get("X-Forgejo-Signature"))
		return r
	}
	w := httptest.NewRecorder()
	g.Handler().ServeHTTP(w, request("wrong"))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("signature status %d", w.Code)
	}
	w = httptest.NewRecorder()
	g.Handler().ServeHTTP(w, request(testGitHubSecret))
	if w.Code != http.StatusAccepted {
		t.Fatalf("accept status %d: %s", w.Code, w.Body.String())
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	g, err = Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = g.Close() }()
	w = httptest.NewRecorder()
	g.Handler().ServeHTTP(w, request(testGitHubSecret))
	if w.Code != http.StatusNoContent {
		t.Fatalf("replay status %d", w.Code)
	}
	var payload, id string
	if err := g.db.QueryRow("SELECT message_id,payload FROM a2a_deliveries").Scan(&id, &payload); err != nil {
		t.Fatal(err)
	}
	var item forwarded
	if err := json.Unmarshal([]byte(payload), &item); err != nil {
		t.Fatal(err)
	}
	if id != "github:unsupervised:github-delivery:luce" || item.Provider != providerGitHub || item.Event != eventIssues || item.Workflow != workflowImplement {
		t.Fatalf("id=%s item=%+v", id, item)
	}
}

func TestGitHubDispatchCarriesWorkflowAndStableMessage(t *testing.T) {
	received := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer route-token" {
			t.Error("missing route authentication")
		}
		var message map[string]any
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
			t.Error(err)
		}
		select {
		case received <- message:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	g, err := Open(Config{Provider: providerGitHub, Organization: "example", Owner: "example", Secret: testGitHubSecret, SpoolPath: filepath.Join(t.TempDir(), "spool.db"), Routes: []Route{{Username: testLuce, URL: server.URL, Token: "route-token"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = g.Close() }()
	body := `{"action":"created","repository":{"full_name":"example/repo","owner":{"login":"example"}},"issue":{"number":38,"assignees":[{"login":"luce"}]},"comment":{"body":"@luce explain this","html_url":"https://github.com/example/repo/issues/38#issuecomment-1"},"sender":{"login":"human"}}`
	req := signedRequest(t, body, "question", testGitHubSecret)
	req.URL.Path = "/webhooks/github"
	req.Header.Set("X-GitHub-Event", eventIssueComment)
	req.Header.Set("X-GitHub-Delivery", "question")
	req.Header.Set("X-Hub-Signature-256", "sha256="+req.Header.Get("X-Forgejo-Signature"))
	w := httptest.NewRecorder()
	g.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d", w.Code)
	}
	select {
	case envelope := <-received:
		message := envelope["message"].(map[string]any)
		if message["messageId"] != "github:example:question:luce" {
			t.Fatalf("message=%v", message)
		}
		parts := message["parts"].([]any)
		data := parts[0].(map[string]any)["data"].(map[string]any)
		if data["Workflow"] != workflowRespond || data["Event"] != eventIssueComment || data["Number"] != float64(38) {
			t.Fatalf("activation=%v", data)
		}
		if parts[1].(map[string]any)["text"] != githubWorkflowInstruction(workflowRespond) {
			t.Fatal("wrong workflow instructions")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("A2A task was not delivered")
	}
}
