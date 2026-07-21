package mail

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/ncrmro/link-operator/code/agent/internal/jmap"
)

type fakeClient struct {
	queryState   string
	initial      []jmap.Email
	changes      []jmap.Email
	queryCalls   int
	changesCalls int
	replies      []jmap.Email
	replyErr     error
}

func (f *fakeClient) AccountID(context.Context) (string, error) { return "account", nil }
func (f *fakeClient) MailboxByRole(context.Context, string, string) (string, error) {
	return "inbox", nil
}
func (f *fakeClient) QueryEmails(context.Context, string, string, int, int) (jmap.QueryResult, error) {
	f.queryCalls++
	ids := make([]string, 0, len(f.initial))
	for _, email := range f.initial {
		ids = append(ids, email.ID)
	}
	return jmap.QueryResult{QueryState: "q1", IDs: ids, Total: len(ids)}, nil
}
func (f *fakeClient) QueryEmailChanges(context.Context, string, string, string, int) (jmap.QueryChangesResult, error) {
	f.changesCalls++
	added := make([]jmap.AddedItem, 0, len(f.changes))
	for index, email := range f.changes {
		added = append(added, jmap.AddedItem{ID: email.ID, Index: index})
	}
	return jmap.QueryChangesResult{NewQueryState: "q2", Added: added}, nil
}
func (f *fakeClient) GetEmails(_ context.Context, _ string, ids []string) ([]jmap.Email, error) {
	all := append(append([]jmap.Email{}, f.initial...), f.changes...)
	byID := map[string]jmap.Email{}
	for _, email := range all {
		byID[email.ID] = email
	}
	result := make([]jmap.Email, 0, len(ids))
	for _, id := range ids {
		result = append(result, byID[id])
	}
	return result, nil
}
func (f *fakeClient) SendReply(_ context.Context, email jmap.Email, _ string) (string, error) {
	if f.replyErr != nil {
		return "", f.replyErr
	}
	f.replies = append(f.replies, email)
	return "reply-" + email.ID, nil
}

func TestPollPersistsInitialMessagesAndThenUsesChanges(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "state", "mail.json")
	client := &fakeClient{initial: []jmap.Email{
		{ID: "e1", MessageID: []string{"<one@example.test>"}, Subject: "one"},
		{ID: "e2", MessageID: []string{"<two@example.test>"}, Subject: "two"},
	}}
	loop := New(client, statePath, filepath.Join(t.TempDir(), "ready"), time.Second,
		slog.New(slog.NewJSONHandler(io.Discard, nil)))

	if err := loop.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.QueryState != "q1" || len(state.Messages) != 2 {
		t.Fatalf("unexpected initial state: %#v", state)
	}
	for _, message := range state.Messages {
		if message.State != "replied" || message.ReplyJMAPID == "" {
			t.Fatalf("initial message was not replied to: %#v", message)
		}
	}

	client.changes = []jmap.Email{{ID: "e3", MessageID: []string{"<three@example.test>"}, Subject: "three"}}
	if err := loop.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err = LoadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.QueryState != "q2" || len(state.Messages) != 3 {
		t.Fatalf("unexpected changed state: %#v", state)
	}
	if client.queryCalls != 1 || client.changesCalls != 1 {
		t.Fatalf("query calls=%d queryChanges calls=%d", client.queryCalls, client.changesCalls)
	}
	if len(client.replies) != 3 {
		t.Fatalf("reply calls=%d, want 3", len(client.replies))
	}
}

func TestRepeatedMessageIDIsDeduplicatedAcrossJMAPIDs(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "mail.json")
	client := &fakeClient{initial: []jmap.Email{
		{ID: "delivery-1", MessageID: []string{"<same@example.test>"}, Subject: "first"},
		{ID: "delivery-2", MessageID: []string{"<same@example.test>"}, Subject: "second"},
	}}
	loop := New(client, statePath, filepath.Join(t.TempDir(), "ready"), time.Second,
		slog.New(slog.NewJSONHandler(io.Discard, nil)))

	if err := loop.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 1 {
		t.Fatalf("expected one Message-ID state, got %d", len(state.Messages))
	}
	if state.Messages["<same@example.test>"].JMAPID != "delivery-1" {
		t.Fatalf("first delivery did not win: %#v", state.Messages)
	}
	if len(client.replies) != 1 {
		t.Fatalf("reply calls=%d, want 1", len(client.replies))
	}
}

func TestReplyFailureLeavesMessagePendingAndRetries(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "mail.json")
	client := &fakeClient{
		initial:  []jmap.Email{{ID: "e1", MessageID: []string{"one@example.test"}, Subject: "one"}},
		replyErr: errors.New("submission unavailable"),
	}
	loop := New(client, statePath, filepath.Join(t.TempDir(), "ready"), time.Second,
		slog.New(slog.NewJSONHandler(io.Discard, nil)))

	if err := loop.Poll(context.Background()); err == nil {
		t.Fatal("expected reply failure")
	}
	state, err := LoadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.Messages["one@example.test"].State != "received" {
		t.Fatalf("failed reply was not left pending: %#v", state.Messages)
	}

	client.replyErr = nil
	if err := loop.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err = LoadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.Messages["one@example.test"].State != "replied" || len(client.replies) != 1 {
		t.Fatalf("pending reply was not completed exactly once: state=%#v replies=%d", state.Messages, len(client.replies))
	}
}
