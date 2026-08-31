package forgegateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Route struct{ Username, URL, Token string }
type Config struct {
	Listen, Organization, Owner, Secret, SpoolPath string
	Routes                                         []Route
	Logger                                         *slog.Logger
}
type Gateway struct {
	cfg    Config
	db     *sql.DB
	client *http.Client
	routes map[string]Route
}

type forgeUser struct {
	Login    string `json:"login"`
	Username string `json:"username"`
}

func (u forgeUser) name() string {
	if u.Username != "" {
		return u.Username
	}
	return u.Login
}

type repository struct {
	FullName string    `json:"full_name"`
	HTMLURL  string    `json:"html_url"`
	Owner    forgeUser `json:"owner"`
}
type issue struct {
	Number    int         `json:"number"`
	HTMLURL   string      `json:"html_url"`
	Body      string      `json:"body"`
	Assignees []forgeUser `json:"assignees"`
	User      forgeUser   `json:"user"`
}
type pullRequest struct {
	Number            int         `json:"number"`
	HTMLURL           string      `json:"html_url"`
	Body              string      `json:"body"`
	Assignees         []forgeUser `json:"assignees"`
	RequestedReviewer *forgeUser  `json:"requested_reviewer"`
	User              forgeUser
}
type comment struct {
	Body    string    `json:"body"`
	HTMLURL string    `json:"html_url"`
	User    forgeUser `json:"user"`
}
type event struct {
	Action            string
	Repository        repository
	Issue             *issue
	PullRequest       *pullRequest `json:"pull_request"`
	Comment           *comment
	Assignee          *forgeUser
	RequestedReviewer *forgeUser `json:"requested_reviewer"`
	Sender            forgeUser
}
type forwarded struct {
	Provider, Action, Actor, Repository string
	Number                              int
	URL, Reason                         string
}

const actionOpened = "opened"

func Open(cfg Config) (*Gateway, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	db, err := sql.Open("sqlite", cfg.SpoolPath)
	if err != nil {
		return nil, err
	}
	for _, q := range []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE IF NOT EXISTS webhook_deliveries (id TEXT PRIMARY KEY, digest TEXT NOT NULL, event TEXT NOT NULL, received_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS route_state (subject TEXT PRIMARY KEY, usernames TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS a2a_deliveries (message_id TEXT PRIMARY KEY, username TEXT NOT NULL, payload TEXT NOT NULL, delivered_at INTEGER, attempts INTEGER NOT NULL DEFAULT 0, next_attempt INTEGER NOT NULL DEFAULT 0)`,
	} {
		if _, err = db.Exec(q); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	routes := map[string]Route{}
	for _, route := range cfg.Routes {
		key := strings.ToLower(route.Username)
		if _, exists := routes[key]; exists {
			_ = db.Close()
			return nil, fmt.Errorf("duplicate case-insensitive forge username %q", route.Username)
		}
		routes[key] = route
	}
	g := &Gateway{cfg: cfg, db: db, client: &http.Client{Timeout: 15 * time.Second}, routes: routes}
	go g.retryLoop()
	return g, nil
}
func (g *Gateway) Close() error { return g.db.Close() }
func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/webhooks/forgejo", g.webhook)
	return mux
}

func (g *Gateway) webhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	delivery := r.Header.Get("X-Forgejo-Delivery")
	if delivery == "" {
		http.Error(w, "missing delivery", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	want, err := hex.DecodeString(strings.TrimPrefix(r.Header.Get("X-Forgejo-Signature"), "sha256="))
	if err != nil {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	mac := hmac.New(sha256.New, []byte(g.cfg.Secret))
	mac.Write(body)
	if !hmac.Equal(want, mac.Sum(nil)) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	var e event
	if err := json.Unmarshal(body, &e); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}
	owner := e.Repository.Owner.name()
	if owner == "" && strings.Contains(e.Repository.FullName, "/") {
		owner = strings.SplitN(e.Repository.FullName, "/", 2)[0]
	}
	if !strings.EqualFold(owner, g.cfg.Owner) {
		http.Error(w, "repository owner rejected", http.StatusForbidden)
		return
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	eventName := r.Header.Get("X-Forgejo-Event")
	tx, err := g.db.Begin()
	if err != nil {
		http.Error(w, "spool unavailable", http.StatusServiceUnavailable)
		return
	}
	defer func() { _ = tx.Rollback() }()
	var prior string
	err = tx.QueryRow(`SELECT digest FROM webhook_deliveries WHERE id=?`, delivery).Scan(&prior)
	if err == nil {
		if prior != digest {
			http.Error(w, "delivery payload collision", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "spool unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, err = tx.Exec(`INSERT INTO webhook_deliveries(id,digest,event,received_at) VALUES(?,?,?,?)`, delivery, digest, eventName, time.Now().Unix()); err != nil {
		http.Error(w, "spool unavailable", http.StatusServiceUnavailable)
		return
	}
	targets, states := g.targets(tx, eventName, e)
	for subject, names := range states {
		encoded, _ := json.Marshal(names)
		if _, err = tx.Exec(`INSERT INTO route_state(subject,usernames) VALUES(?,?) ON CONFLICT(subject) DO UPDATE SET usernames=excluded.usernames`, subject, string(encoded)); err != nil {
			http.Error(w, "spool unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	for username, item := range targets {
		item.Provider = "forgejo"
		item.Action = e.Action
		raw, _ := json.Marshal(item)
		mid := fmt.Sprintf("forgejo:%s:%s:%s", g.cfg.Organization, delivery, username)
		if _, err = tx.Exec(`INSERT OR IGNORE INTO a2a_deliveries(message_id,username,payload) VALUES(?,?,?)`, mid, username, string(raw)); err != nil {
			http.Error(w, "spool unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "spool unavailable", http.StatusServiceUnavailable)
		return
	}
	go g.deliverPending(context.Background())
	w.WriteHeader(http.StatusAccepted)
}

func usernames(users []forgeUser) []string {
	out := make([]string, 0, len(users))
	seen := map[string]bool{}
	for _, u := range users {
		n := strings.ToLower(u.name())
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}
func mentions(text string) []string {
	re := regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_.-])@([A-Za-z0-9_.-]+)`)
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		n := strings.ToLower(m[1])
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}
func difference(now, before []string) []string {
	old := map[string]bool{}
	for _, n := range before {
		old[n] = true
	}
	var out []string
	for _, n := range now {
		if !old[n] {
			out = append(out, n)
		}
	}
	return out
}
func (g *Gateway) prior(tx *sql.Tx, subject string) []string {
	var raw string
	if tx.QueryRow(`SELECT usernames FROM route_state WHERE subject=?`, subject).Scan(&raw) == nil {
		var v []string
		_ = json.Unmarshal([]byte(raw), &v)
		return v
	}
	return nil
}
func (g *Gateway) targets(tx *sql.Tx, kind string, e event) (map[string]forwarded, map[string][]string) {
	t := map[string]forwarded{}
	state := map[string][]string{}
	repo := e.Repository.FullName
	actor := e.Sender.name()
	number := 0
	url := ""
	text := ""
	add := func(names []string, reason string) {
		for _, n := range names {
			if _, ok := g.routes[n]; ok {
				t[n] = forwarded{Actor: actor, Repository: repo, Number: number, URL: url, Reason: reason}
			}
		}
	}
	subject := ""
	switch kind {
	case "issues":
		if e.Issue == nil {
			return t, state
		}
		number = e.Issue.Number
		url = e.Issue.HTMLURL
		text = e.Issue.Body
		subject = fmt.Sprintf("%s:issue:%d", repo, number)
		now := usernames(e.Issue.Assignees)
		if e.Action == actionOpened || e.Action == "assigned" {
			add(difference(now, g.prior(tx, subject+":assignees")), "assigned")
		}
		state[subject+":assignees"] = now
	case "pull_request":
		if e.PullRequest == nil {
			return t, state
		}
		number = e.PullRequest.Number
		url = e.PullRequest.HTMLURL
		text = e.PullRequest.Body
		subject = fmt.Sprintf("%s:pull:%d", repo, number)
		now := usernames(e.PullRequest.Assignees)
		if e.Action == actionOpened || e.Action == "assigned" {
			add(difference(now, g.prior(tx, subject+":assignees")), "assigned")
		}
		state[subject+":assignees"] = now
		if e.Action == "review_requested" {
			u := e.RequestedReviewer
			if u == nil {
				u = e.PullRequest.RequestedReviewer
			}
			if u != nil {
				add([]string{strings.ToLower(u.name())}, "review_requested")
			}
		}
	case "issue_comment":
		if e.Comment == nil {
			return t, state
		}
		url = e.Comment.HTMLURL
		text = e.Comment.Body
		if e.Issue != nil {
			number = e.Issue.Number
		} else if e.PullRequest != nil {
			number = e.PullRequest.Number
		}
		subject = fmt.Sprintf("%s:comment:%s", repo, url)
	default:
		return t, state
	}
	if e.Action == actionOpened || e.Action == "created" || e.Action == "edited" {
		now := mentions(text)
		add(difference(now, g.prior(tx, subject+":mentions")), "mentioned")
		state[subject+":mentions"] = now
	}
	return t, state
}

func (g *Gateway) retryLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		g.deliverPending(context.Background())
	}
}
func (g *Gateway) deliverPending(ctx context.Context) {
	rows, err := g.db.QueryContext(ctx, `SELECT message_id,username,payload,attempts FROM a2a_deliveries WHERE delivered_at IS NULL AND next_attempt<=? ORDER BY rowid LIMIT 100`, time.Now().Unix())
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	type item struct {
		m, u, p string
		a       int
	}
	var list []item
	for rows.Next() {
		var x item
		if rows.Scan(&x.m, &x.u, &x.p, &x.a) == nil {
			list = append(list, x)
		}
	}
	for _, x := range list {
		route, ok := g.routes[x.u]
		if !ok {
			continue
		}
		body, _ := json.Marshal(map[string]any{"message": map[string]any{"messageId": x.m, "role": "ROLE_USER", "parts": []map[string]any{{"text": x.p}}}, "configuration": map[string]any{"returnImmediately": true}})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(route.URL, "/")+"/message:send", strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer "+route.Token)
		req.Header.Set("Content-Type", "application/a2a+json")
		req.Header.Set("a2a-version", "1.0")
		resp, err := g.client.Do(req)
		if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			_ = resp.Body.Close()
			_, _ = g.db.Exec(`UPDATE a2a_deliveries SET delivered_at=? WHERE message_id=?`, time.Now().Unix(), x.m)
			continue
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		delay := 1 << min(x.a, 8)
		_, _ = g.db.Exec(`UPDATE a2a_deliveries SET attempts=attempts+1,next_attempt=? WHERE message_id=?`, time.Now().Add(time.Duration(delay)*time.Second).Unix(), x.m)
	}
}
