package jmap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscoverUsesConfiguredOriginWithAdvertisedAPIPath(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/.well-known/jmap" {
			t.Errorf("unexpected path %q", request.URL.Path)
		}
		username, password, ok := request.BasicAuth()
		if !ok || username != "agent@example.test" || password != "secret" {
			t.Error("request did not use configured Basic authentication")
		}
		response.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(response).Encode(Session{
			APIURL:          "http://stalwart.internal.test:8080/jmap/",
			PrimaryAccounts: map[string]string{MailCapability: "account"},
		}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()

	client, err := New(server.URL, "agent@example.test", "secret")
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if session.APIURL != server.URL+"/jmap/" {
		t.Fatalf("API URL = %q, want %q", session.APIURL, server.URL+"/jmap/")
	}
}
