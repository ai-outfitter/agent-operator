package jmap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	CoreCapability       = "urn:ietf:params:jmap:core"
	MailCapability       = "urn:ietf:params:jmap:mail"
	SubmissionCapability = "urn:ietf:params:jmap:submission"
)

type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client
	session  *Session
}

type Session struct {
	APIURL          string            `json:"apiUrl"`
	PrimaryAccounts map[string]string `json:"primaryAccounts"`
}

type Email struct {
	ID            string    `json:"id"`
	MessageID     []string  `json:"messageId"`
	InReplyTo     []string  `json:"inReplyTo"`
	References    []string  `json:"references"`
	Subject       string    `json:"subject"`
	From          []Address `json:"from"`
	To            []Address `json:"to"`
	ReplyTo       []Address `json:"replyTo"`
	ReceivedAt    string    `json:"receivedAt"`
	HasAttachment bool      `json:"hasAttachment"`
}

type Address struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

type QueryResult struct {
	AccountID  string   `json:"accountId"`
	QueryState string   `json:"queryState"`
	CanChanges bool     `json:"canCalculateChanges"`
	Position   int      `json:"position"`
	IDs        []string `json:"ids"`
	Total      int      `json:"total"`
}

type AddedItem struct {
	ID    string `json:"id"`
	Index int    `json:"index"`
}

type QueryChangesResult struct {
	AccountID     string      `json:"accountId"`
	OldQueryState string      `json:"oldQueryState"`
	NewQueryState string      `json:"newQueryState"`
	Removed       []string    `json:"removed"`
	Added         []AddedItem `json:"added"`
	HasMore       bool        `json:"hasMoreChanges"`
}

type MethodError struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

func (e *MethodError) Error() string {
	if e.Description == "" {
		return "JMAP method failed: " + e.Type
	}
	return fmt.Sprintf("JMAP method failed: %s: %s", e.Type, e.Description)
}

func New(baseURL, username, password string) (*Client, error) {
	if baseURL == "" || username == "" || password == "" {
		return nil, errors.New("JMAP_URL, JMAP_USERNAME, and JMAP_PASSWORD are required")
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("parse JMAP URL: %w", err)
	}
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		http:     &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *Client) Discover(ctx context.Context) (*Session, error) {
	sessionURL := c.baseURL
	if !strings.HasSuffix(sessionURL, "/.well-known/jmap") && !strings.HasSuffix(sessionURL, "/jmap/session") {
		sessionURL += "/.well-known/jmap"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sessionURL, nil)
	if err != nil {
		return nil, err
	}
	c.authorize(req)
	response, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discover JMAP session: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("discover JMAP session: HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var session Session
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("decode JMAP session: %w", err)
	}
	if session.APIURL == "" || session.PrimaryAccounts[MailCapability] == "" {
		return nil, errors.New("JMAP session does not advertise a primary mail account")
	}
	// The development cluster is reached through a host port-forward while
	// Stalwart advertises its in-cluster service origin. Keep the configured
	// origin, which is the one the caller proved reachable, and retain the
	// protocol path advertised by the session.
	configuredOrigin, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, err
	}
	apiURL, err := url.Parse(session.APIURL)
	if err != nil {
		return nil, fmt.Errorf("parse JMAP API URL: %w", err)
	}
	apiURL.Scheme = configuredOrigin.Scheme
	apiURL.Host = configuredOrigin.Host
	session.APIURL = apiURL.String()
	c.session = &session
	return &session, nil
}

func (c *Client) AccountID(ctx context.Context) (string, error) {
	if c.session == nil {
		if _, err := c.Discover(ctx); err != nil {
			return "", err
		}
	}
	return c.session.PrimaryAccounts[MailCapability], nil
}

func (c *Client) MailboxByRole(ctx context.Context, accountID, role string) (string, error) {
	var result QueryResult
	err := c.call(ctx, []string{CoreCapability, MailCapability}, "Mailbox/query", map[string]any{
		"accountId": accountID,
		"filter":    map[string]string{"role": role},
		"limit":     2,
	}, &result)
	if err != nil {
		return "", err
	}
	if len(result.IDs) != 1 {
		return "", fmt.Errorf("expected one %s mailbox, got %d", role, len(result.IDs))
	}
	return result.IDs[0], nil
}

func (c *Client) QueryEmails(ctx context.Context, accountID, mailboxID string, position, limit int) (QueryResult, error) {
	var result QueryResult
	err := c.call(ctx, []string{CoreCapability, MailCapability}, "Email/query", map[string]any{
		"accountId":       accountID,
		"filter":          map[string]string{"inMailbox": mailboxID},
		"sort":            []map[string]any{{"property": "receivedAt", "isAscending": true}},
		"position":        position,
		"limit":           limit,
		"calculateTotal":  true,
		"collapseThreads": false,
	}, &result)
	return result, err
}

func (c *Client) QueryEmailChanges(
	ctx context.Context,
	accountID, mailboxID, sinceQueryState string,
	maxChanges int,
) (QueryChangesResult, error) {
	var result QueryChangesResult
	err := c.call(ctx, []string{CoreCapability, MailCapability}, "Email/queryChanges", map[string]any{
		"accountId":       accountID,
		"filter":          map[string]string{"inMailbox": mailboxID},
		"sort":            []map[string]any{{"property": "receivedAt", "isAscending": true}},
		"sinceQueryState": sinceQueryState,
		"maxChanges":      maxChanges,
		"collapseThreads": false,
	}, &result)
	sort.SliceStable(result.Added, func(i, j int) bool { return result.Added[i].Index < result.Added[j].Index })
	return result, err
}

func (c *Client) GetEmails(ctx context.Context, accountID string, ids []string) ([]Email, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var result struct {
		List     []Email  `json:"list"`
		NotFound []string `json:"notFound"`
	}
	err := c.call(ctx, []string{CoreCapability, MailCapability}, "Email/get", map[string]any{
		"accountId": accountID,
		"ids":       ids,
		"properties": []string{
			"id", "messageId", "inReplyTo", "references", "subject", "from", "to",
			"replyTo", "receivedAt", "hasAttachment",
		},
	}, &result)
	if err != nil {
		return nil, err
	}
	if len(result.NotFound) > 0 {
		return nil, fmt.Errorf("JMAP Email/get did not find ids: %s", strings.Join(result.NotFound, ","))
	}
	byID := make(map[string]Email, len(result.List))
	for _, email := range result.List {
		byID[email.ID] = email
	}
	ordered := make([]Email, 0, len(ids))
	for _, id := range ids {
		if email, ok := byID[id]; ok {
			ordered = append(ordered, email)
		}
	}
	return ordered, nil
}

func (c *Client) SendText(
	ctx context.Context,
	to, subject, body, messageID string,
) (string, error) {
	return c.sendText(ctx, outgoingMessage{
		To: []Address{{Email: to}}, Subject: subject, Body: body, MessageID: messageID,
	})
}

func (c *Client) SendReply(ctx context.Context, original Email, body string) (string, error) {
	recipients := original.ReplyTo
	if len(recipients) == 0 {
		recipients = original.From
	}
	to := make([]Address, 0, len(recipients))
	for _, address := range recipients {
		if strings.TrimSpace(address.Email) != "" && !strings.EqualFold(address.Email, c.username) {
			to = append(to, address)
		}
	}
	if len(to) == 0 {
		return "", errors.New("message has no external reply address")
	}
	inReplyTo := normalizeMessageIDs(original.MessageID)
	if len(inReplyTo) == 0 {
		return "", errors.New("message has no Message-ID for a threaded reply")
	}
	references := append(normalizeMessageIDs(original.References), inReplyTo...)
	return c.sendText(ctx, outgoingMessage{
		To: to, Subject: replySubject(original.Subject), Body: body,
		InReplyTo: inReplyTo, References: uniqueStrings(references),
	})
}

type outgoingMessage struct {
	To         []Address
	Subject    string
	Body       string
	MessageID  string
	InReplyTo  []string
	References []string
}

func (c *Client) sendText(ctx context.Context, message outgoingMessage) (string, error) {
	accountID, err := c.AccountID(ctx)
	if err != nil {
		return "", err
	}
	draftsID, err := c.MailboxByRole(ctx, accountID, "drafts")
	if err != nil {
		return "", err
	}
	identityID, err := c.firstIdentity(ctx, accountID)
	if err != nil {
		return "", err
	}

	createdEmail := map[string]any{
		"mailboxIds": map[string]bool{draftsID: true},
		"keywords":   map[string]bool{"$draft": true},
		"from":       []Address{{Email: c.username}},
		"to":         message.To,
		"subject":    message.Subject,
		"textBody":   []map[string]string{{"partId": "body", "type": "text/plain"}},
		"bodyValues": map[string]map[string]any{"body": {"value": message.Body}},
	}
	if messageID := normalizeMessageID(message.MessageID); messageID != "" {
		createdEmail["header:Message-ID:asMessageIds"] = []string{messageID}
	}
	if values := normalizeMessageIDs(message.InReplyTo); len(values) > 0 {
		createdEmail["header:In-Reply-To:asMessageIds"] = values
	}
	if values := normalizeMessageIDs(message.References); len(values) > 0 {
		createdEmail["header:References:asMessageIds"] = values
	}

	methods := []methodCall{
		{name: "Email/set", id: "create-email", arguments: map[string]any{
			"accountId": accountID,
			"create":    map[string]any{"message": createdEmail},
		}},
		{name: "EmailSubmission/set", id: "submit-email", arguments: map[string]any{
			"accountId": accountID,
			"create": map[string]any{"submission": map[string]string{
				"emailId":    "#message",
				"identityId": identityID,
			}},
		}},
	}
	responses, err := c.callMany(ctx, []string{CoreCapability, MailCapability, SubmissionCapability}, methods)
	if err != nil {
		return "", err
	}
	if len(responses) != 2 {
		return "", fmt.Errorf("expected two JMAP responses, got %d", len(responses))
	}
	var setResult struct {
		Created map[string]struct {
			ID string `json:"id"`
		} `json:"created"`
		NotCreated map[string]MethodError `json:"notCreated"`
	}
	if err := decodeMethodResponse(responses[0], "Email/set", &setResult); err != nil {
		return "", err
	}
	if failure, ok := setResult.NotCreated["message"]; ok {
		return "", &failure
	}
	var submissionResult struct {
		Created map[string]struct {
			ID string `json:"id"`
		} `json:"created"`
		NotCreated map[string]MethodError `json:"notCreated"`
	}
	if err := decodeMethodResponse(responses[1], "EmailSubmission/set", &submissionResult); err != nil {
		return "", err
	}
	if failure, ok := submissionResult.NotCreated["submission"]; ok {
		return "", &failure
	}
	created, ok := setResult.Created["message"]
	if !ok || created.ID == "" {
		return "", errors.New("JMAP Email/set did not return a created message")
	}
	return created.ID, nil
}

func replySubject(subject string) string {
	subject = strings.TrimSpace(subject)
	if strings.HasPrefix(strings.ToLower(subject), "re:") {
		return subject
	}
	return "Re: " + subject
}

func normalizeMessageID(value string) string {
	return strings.Trim(strings.TrimSpace(value), "<>")
}

func normalizeMessageIDs(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = normalizeMessageID(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (c *Client) firstIdentity(ctx context.Context, accountID string) (string, error) {
	var result struct {
		List []struct {
			ID string `json:"id"`
		} `json:"list"`
	}
	err := c.call(ctx, []string{CoreCapability, MailCapability, SubmissionCapability}, "Identity/get", map[string]any{
		"accountId": accountID,
		"ids":       nil,
	}, &result)
	if err != nil {
		return "", err
	}
	if len(result.List) == 0 || result.List[0].ID == "" {
		return "", errors.New("JMAP account has no submission identity")
	}
	return result.List[0].ID, nil
}

type methodCall struct {
	name      string
	arguments any
	id        string
}

func (c *Client) call(ctx context.Context, using []string, name string, arguments any, output any) error {
	responses, err := c.callMany(ctx, using, []methodCall{{name: name, arguments: arguments, id: "call"}})
	if err != nil {
		return err
	}
	if len(responses) != 1 {
		return fmt.Errorf("expected one JMAP response, got %d", len(responses))
	}
	return decodeMethodResponse(responses[0], name, output)
}

func (c *Client) callMany(ctx context.Context, using []string, calls []methodCall) ([]json.RawMessage, error) {
	if c.session == nil {
		if _, err := c.Discover(ctx); err != nil {
			return nil, err
		}
	}
	methodCalls := make([][3]any, 0, len(calls))
	for _, call := range calls {
		methodCalls = append(methodCalls, [3]any{call.name, call.arguments, call.id})
	}
	payload, err := json.Marshal(map[string]any{"using": using, "methodCalls": methodCalls})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.session.APIURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)
	response, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call JMAP API: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("call JMAP API: HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var envelope struct {
		MethodResponses []json.RawMessage `json:"methodResponses"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode JMAP response: %w", err)
	}
	return envelope.MethodResponses, nil
}

func decodeMethodResponse(raw json.RawMessage, expected string, output any) error {
	var parts []json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil || len(parts) != 3 {
		return errors.New("invalid JMAP method response")
	}
	var name string
	if err := json.Unmarshal(parts[0], &name); err != nil {
		return errors.New("invalid JMAP method response name")
	}
	if name == "error" {
		var methodError MethodError
		if err := json.Unmarshal(parts[1], &methodError); err != nil {
			return errors.New("invalid JMAP method error")
		}
		return &methodError
	}
	if name != expected {
		return fmt.Errorf("expected JMAP %s response, got %s", expected, name)
	}
	if err := json.Unmarshal(parts[1], output); err != nil {
		return fmt.Errorf("decode JMAP %s response: %w", expected, err)
	}
	return nil
}

func (c *Client) authorize(req *http.Request) {
	req.SetBasicAuth(c.username, c.password)
}
