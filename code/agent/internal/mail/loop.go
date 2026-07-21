package mail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ncrmro/link-operator/code/agent/internal/jmap"
)

const stateVersion = 1

const replyBody = "Your message was received by the researcher agent."

type JMAPClient interface {
	AccountID(context.Context) (string, error)
	MailboxByRole(context.Context, string, string) (string, error)
	QueryEmails(context.Context, string, string, int, int) (jmap.QueryResult, error)
	QueryEmailChanges(context.Context, string, string, string, int) (jmap.QueryChangesResult, error)
	GetEmails(context.Context, string, []string) ([]jmap.Email, error)
	SendReply(context.Context, jmap.Email, string) (string, error)
}

type Loop struct {
	client       JMAPClient
	statePath    string
	readyPath    string
	pollInterval time.Duration
	logger       *slog.Logger
}

type State struct {
	Version    int                     `json:"version"`
	AccountID  string                  `json:"accountId"`
	InboxID    string                  `json:"inboxId"`
	QueryState string                  `json:"queryState"`
	Messages   map[string]MessageState `json:"messages"`
}

type MessageState struct {
	JMAPID        string   `json:"jmapId"`
	MessageID     string   `json:"messageId"`
	Subject       string   `json:"subject"`
	From          []string `json:"from,omitempty"`
	ReceivedAt    string   `json:"receivedAt,omitempty"`
	HasAttachment bool     `json:"hasAttachment"`
	State         string   `json:"state"`
	ObservedAt    string   `json:"observedAt"`
	ReplyJMAPID   string   `json:"replyJmapId,omitempty"`
	RepliedAt     string   `json:"repliedAt,omitempty"`
}

func New(client JMAPClient, statePath, readyPath string, pollInterval time.Duration, logger *slog.Logger) *Loop {
	return &Loop{
		client: client, statePath: statePath, readyPath: readyPath,
		pollInterval: pollInterval, logger: logger,
	}
}

func (l *Loop) Run(ctx context.Context) error {
	if err := os.Remove(l.readyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear mail loop readiness: %w", err)
	}
	if err := l.Poll(ctx); err != nil {
		return err
	}
	if err := writeReady(l.readyPath); err != nil {
		return err
	}
	l.logger.Info("Mail loop ready", "event", "mail_loop_ready", "pollInterval", l.pollInterval.String())
	ticker := time.NewTicker(l.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := l.Poll(ctx); err != nil {
				l.logger.Error("Mail poll failed", "event", "mail_poll_failed", "error", err)
			}
		}
	}
}

func (l *Loop) Poll(ctx context.Context) error {
	state, err := LoadState(l.statePath)
	if err != nil {
		return err
	}
	accountID, err := l.client.AccountID(ctx)
	if err != nil {
		return err
	}
	inboxID, err := l.client.MailboxByRole(ctx, accountID, "inbox")
	if err != nil {
		return err
	}
	if state.AccountID != accountID || state.InboxID != inboxID {
		state.AccountID = accountID
		state.InboxID = inboxID
		state.QueryState = ""
		state.Messages = map[string]MessageState{}
	}
	if state.QueryState == "" {
		return l.fullSync(ctx, state)
	}
	if err := l.resumePending(ctx, state); err != nil {
		return err
	}
	return l.changeSync(ctx, state)
}

func (l *Loop) resumePending(ctx context.Context, state *State) error {
	ids := make([]string, 0)
	for _, message := range state.Messages {
		if message.State == "received" && message.JMAPID != "" {
			ids = append(ids, message.JMAPID)
		}
	}
	sort.Strings(ids)
	return l.receiveIDs(ctx, state, ids)
}

func (l *Loop) fullSync(ctx context.Context, state *State) error {
	const pageSize = 100
	position := 0
	var queryState string
	for {
		result, err := l.client.QueryEmails(ctx, state.AccountID, state.InboxID, position, pageSize)
		if err != nil {
			return err
		}
		queryState = result.QueryState
		if err := l.receiveIDs(ctx, state, result.IDs); err != nil {
			return err
		}
		position += len(result.IDs)
		if len(result.IDs) == 0 || position >= result.Total {
			break
		}
	}
	state.QueryState = queryState
	return SaveState(l.statePath, state)
}

func (l *Loop) changeSync(ctx context.Context, state *State) error {
	for {
		result, err := l.client.QueryEmailChanges(ctx, state.AccountID, state.InboxID, state.QueryState, 100)
		if err != nil {
			var methodError *jmap.MethodError
			if errors.As(err, &methodError) && methodError.Type == "cannotCalculateChanges" {
				state.QueryState = ""
				return l.fullSync(ctx, state)
			}
			return err
		}
		ids := make([]string, 0, len(result.Added))
		for _, added := range result.Added {
			ids = append(ids, added.ID)
		}
		if err := l.receiveIDs(ctx, state, ids); err != nil {
			return err
		}
		state.QueryState = result.NewQueryState
		if err := SaveState(l.statePath, state); err != nil {
			return err
		}
		if !result.HasMore {
			return nil
		}
	}
}

func (l *Loop) receiveIDs(ctx context.Context, state *State, ids []string) error {
	const getBatchSize = 100
	for start := 0; start < len(ids); start += getBatchSize {
		end := min(start+getBatchSize, len(ids))
		emails, err := l.client.GetEmails(ctx, state.AccountID, ids[start:end])
		if err != nil {
			return err
		}
		for _, email := range emails {
			if err := l.receive(ctx, state, email); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *Loop) receive(ctx context.Context, state *State, email jmap.Email) error {
	messageID := firstMessageID(email)
	key := messageID
	if key == "" {
		key = "jmap:" + email.ID
	}
	message, exists := state.Messages[key]
	if exists && message.State == "replied" {
		return nil
	}
	if !exists {
		from := make([]string, 0, len(email.From))
		for _, address := range email.From {
			from = append(from, address.Email)
		}
		message = MessageState{
			JMAPID: email.ID, MessageID: messageID, Subject: email.Subject,
			From: from, ReceivedAt: email.ReceivedAt, HasAttachment: email.HasAttachment,
			State: "received", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}
		state.Messages[key] = message
		if err := SaveState(l.statePath, state); err != nil {
			delete(state.Messages, key)
			return err
		}
		l.logger.Info("Received mail", "event", "mail_received", "messageId", messageID,
			"jmapId", email.ID, "subject", email.Subject, "from", strings.Join(from, ","),
			"hasAttachment", email.HasAttachment)
	}
	replyID, err := l.client.SendReply(ctx, email, replyBody)
	if err != nil {
		return fmt.Errorf("reply to %s: %w", key, err)
	}
	message.State = "replied"
	message.ReplyJMAPID = replyID
	message.RepliedAt = time.Now().UTC().Format(time.RFC3339Nano)
	state.Messages[key] = message
	if err := SaveState(l.statePath, state); err != nil {
		return err
	}
	l.logger.Info("Replied to mail", "event", "mail_replied", "messageId", messageID,
		"jmapId", email.ID, "replyJmapId", replyID, "subject", email.Subject)
	return nil
}

func firstMessageID(email jmap.Email) string {
	for _, value := range email.MessageID {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func LoadState(path string) (*State, error) {
	state := &State{Version: stateVersion, Messages: map[string]MessageState{}}
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read mail state: %w", err)
	}
	if err := json.Unmarshal(contents, state); err != nil {
		return nil, fmt.Errorf("decode mail state: %w", err)
	}
	if state.Version != stateVersion {
		return nil, fmt.Errorf("unsupported mail state version %d", state.Version)
	}
	if state.Messages == nil {
		state.Messages = map[string]MessageState{}
	}
	return state, nil
}

func SaveState(path string, state *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create mail state directory: %w", err)
	}
	contents, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode mail state: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".mail-state-*")
	if err != nil {
		return fmt.Errorf("create temporary mail state: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace mail state: %w", err)
	}
	return nil
}

func writeReady(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0o600)
}
