package main

import (
	"testing"

	"github.com/ncrmro/link-operator/code/agent/internal/jmap"
)

func TestMatchesReplyRequiresAgentReturnAddress(t *testing.T) {
	t.Parallel()
	email := jmap.Email{
		InReplyTo: []string{"probe@link.test"},
		From:      []jmap.Address{{Email: "researcher@link.test"}},
		To:        []jmap.Address{{Email: "demo-user@link.test"}},
	}

	if !matchesReply(email, "probe@link.test", "researcher@link.test", "demo-user@link.test") {
		t.Fatal("valid agent reply did not match")
	}
	if matchesReply(email, "probe@link.test", "somebody-else@link.test", "demo-user@link.test") {
		t.Fatal("reply matched the wrong return address")
	}
}

func TestMatchesReplyRequiresThreadAndSenderMailbox(t *testing.T) {
	t.Parallel()
	email := jmap.Email{
		InReplyTo: []string{"probe@link.test"},
		From:      []jmap.Address{{Email: "researcher@link.test"}},
		To:        []jmap.Address{{Email: "demo-user@link.test"}},
	}

	if matchesReply(email, "different@link.test", "researcher@link.test", "demo-user@link.test") {
		t.Fatal("reply matched the wrong thread")
	}
	if matchesReply(email, "probe@link.test", "researcher@link.test", "different@link.test") {
		t.Fatal("reply matched the wrong sender mailbox")
	}
}
