package forgegateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func signedRequest(t *testing.T, body, delivery, secret string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/forgejo", strings.NewReader(body))
	req.Header.Set("X-Forgejo-Event", "issues")
	req.Header.Set("X-Forgejo-Delivery", delivery)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = io.WriteString(mac, body)
	req.Header.Set("X-Forgejo-Signature", hex.EncodeToString(mac.Sum(nil)))
	return req
}

func TestSignedDurableFanoutAndCollision(t *testing.T) {
	const artera = "artera"
	var mu sync.Mutex
	received := map[string]string{}
	a2a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		received[r.Header.Get("Authorization")] = string(raw)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer a2a.Close()
	path := filepath.Join(t.TempDir(), "spool.db")
	g, err := Open(Config{Organization: artera, Owner: artera, Secret: "secret", SpoolPath: path, Routes: []Route{{Username: "mira", URL: a2a.URL, Token: "mira-token"}, {Username: "atlas", URL: a2a.URL, Token: "atlas-token"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = g.Close() }()
	body := `{"action":"opened","repository":{"full_name":"artera/artera","owner":{"username":"artera"}},"issue":{"number":7,"html_url":"https://git.ncrmro.com/artera/artera/issues/7","body":"hello @atlas","assignees":[{"username":"mira"}]},"sender":{"username":"nicholas"}}`
	w := httptest.NewRecorder()
	g.Handler().ServeHTTP(w, signedRequest(t, body, "delivery-1", "secret"))
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(received)
		mu.Unlock()
		if n == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("deliveries=%v", received)
	}
	if !strings.Contains(received["Bearer mira-token"], `forgejo:artera:delivery-1:mira`) {
		t.Fatal("missing Mira message id")
	}
	if !strings.Contains(received["Bearer atlas-token"], `forgejo:artera:delivery-1:atlas`) {
		t.Fatal("missing Atlas message id")
	}
	w = httptest.NewRecorder()
	g.Handler().ServeHTTP(w, signedRequest(t, body, "delivery-1", "secret"))
	if w.Code != http.StatusNoContent {
		t.Fatalf("replay status=%d", w.Code)
	}
	w = httptest.NewRecorder()
	g.Handler().ServeHTTP(w, signedRequest(t, strings.Replace(body, "hello", "changed", 1), "delivery-1", "secret"))
	if w.Code != http.StatusConflict {
		t.Fatalf("collision status=%d", w.Code)
	}
}

func TestRejectsSignatureOwnerAndDuplicateRoutes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool.db")
	g, err := Open(Config{Organization: "artera", Owner: "artera", Secret: "secret", SpoolPath: path, Routes: []Route{{Username: "mira", URL: "http://invalid", Token: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = g.Close() }()
	body := `{"action":"opened","repository":{"full_name":"other/repo","owner":{"username":"other"}},"issue":{"number":1}}`
	w := httptest.NewRecorder()
	g.Handler().ServeHTTP(w, signedRequest(t, body, "d1", "wrong"))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("signature status=%d", w.Code)
	}
	w = httptest.NewRecorder()
	g.Handler().ServeHTTP(w, signedRequest(t, body, "d2", "secret"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("owner status=%d", w.Code)
	}
	if _, err := Open(Config{SpoolPath: filepath.Join(t.TempDir(), "dup.db"), Routes: []Route{{Username: "Aster"}, {Username: "aster"}}}); err == nil {
		t.Fatal("duplicate usernames accepted")
	}
}
