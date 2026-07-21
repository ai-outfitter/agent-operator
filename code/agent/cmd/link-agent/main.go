package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ncrmro/link-operator/code/agent/internal/jmap"
	mailadapter "github.com/ncrmro/link-operator/code/agent/internal/mail"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Agent stopped", "event", "agent_failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	command := "run"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	switch command {
	case "run", "once":
		client, adapter, err := configuredAdapter(logger)
		if err != nil {
			return err
		}
		_ = client
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		if command == "once" {
			return adapter.Poll(ctx)
		}
		return adapter.Run(ctx)
	case "send":
		return send(logger, os.Args[2:])
	case "wait-reply":
		return waitReply(os.Args[2:])
	case "state":
		return printState(os.Args[2:])
	case "version":
		fmt.Println("link-agent dev")
		return nil
	default:
		return fmt.Errorf("unknown command %q (use run, once, send, wait-reply, state, or version)", command)
	}
}

func configuredAdapter(logger *slog.Logger) (*jmap.Client, *mailadapter.Loop, error) {
	client, err := configuredClient()
	if err != nil {
		return nil, nil, err
	}
	interval := 5 * time.Second
	if value := os.Getenv("LINK_MAIL_POLL_INTERVAL"); value != "" {
		interval, err = time.ParseDuration(value)
		if err != nil || interval < time.Second {
			return nil, nil, errors.New("LINK_MAIL_POLL_INTERVAL must be a duration of at least 1s")
		}
	}
	workspace := getenv("LINK_WORKSPACE", "/workspace")
	statePath := getenv("LINK_MAIL_STATE", filepath.Join(workspace, ".link", "mail-state.json"))
	readyPath := getenv("LINK_MAIL_READY", filepath.Join(workspace, ".link", "mail-loop-ready"))
	return client, mailadapter.New(client, statePath, readyPath, interval, logger), nil
}

func configuredClient() (*jmap.Client, error) {
	return jmap.New(os.Getenv("JMAP_URL"), os.Getenv("JMAP_USERNAME"), os.Getenv("JMAP_PASSWORD"))
}

func send(logger *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("send", flag.ContinueOnError)
	to := flags.String("to", "", "recipient email address")
	subject := flags.String("subject", "Link Operator mail-loop probe", "message subject")
	body := flags.String("body", "Verify that the Link agent JMAP loop observes this message.", "plain-text body")
	messageID := flags.String("message-id", "", "RFC 5322 Message-ID value, with or without angle brackets")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *to == "" {
		return errors.New("send requires --to")
	}
	client, err := configuredClient()
	if err != nil {
		return err
	}
	emailID, err := client.SendText(context.Background(), *to, *subject, *body, *messageID)
	if err != nil {
		return err
	}
	logger.Info("Submitted mail", "event", "mail_submitted", "emailId", emailID, "messageId", *messageID,
		"to", *to, "subject", *subject)
	return nil
}

func waitReply(args []string) error {
	flags := flag.NewFlagSet("wait-reply", flag.ContinueOnError)
	inReplyTo := flags.String("in-reply-to", "", "original RFC 5322 Message-ID")
	returnAddress := flags.String("return-address", "", "required reply return address (JMAP From)")
	to := flags.String("to", "", "required reply To address")
	timeout := flags.Duration("timeout", time.Minute, "maximum time to wait")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *inReplyTo == "" || *returnAddress == "" {
		return errors.New("wait-reply requires --in-reply-to and --return-address")
	}
	client, err := configuredClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	for {
		replies, err := matchingReplies(ctx, client, *inReplyTo, *returnAddress, *to)
		if err != nil {
			return err
		}
		switch len(replies) {
		case 1:
			return json.NewEncoder(os.Stdout).Encode(replies[0])
		case 0:
		case 2:
			fallthrough
		default:
			return fmt.Errorf("found %d replies to Message-ID %q; want exactly one", len(replies), *inReplyTo)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for reply to Message-ID %q: %w", *inReplyTo, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

type replyEvidence struct {
	JMAPID     string         `json:"jmapId"`
	MessageID  []string       `json:"messageId"`
	InReplyTo  []string       `json:"inReplyTo"`
	References []string       `json:"references"`
	From       []jmap.Address `json:"from"`
	To         []jmap.Address `json:"to"`
	Subject    string         `json:"subject"`
}

func matchingReplies(
	ctx context.Context,
	client *jmap.Client,
	inReplyTo, returnAddress, to string,
) ([]replyEvidence, error) {
	accountID, err := client.AccountID(ctx)
	if err != nil {
		return nil, err
	}
	inboxID, err := client.MailboxByRole(ctx, accountID, "inbox")
	if err != nil {
		return nil, err
	}
	wantedMessageID := normalizeMessageID(inReplyTo)
	const pageSize = 100
	result := make([]replyEvidence, 0, 1)
	for position := 0; ; {
		query, err := client.QueryEmails(ctx, accountID, inboxID, position, pageSize)
		if err != nil {
			return nil, err
		}
		emails, err := client.GetEmails(ctx, accountID, query.IDs)
		if err != nil {
			return nil, err
		}
		for _, email := range emails {
			if matchesReply(email, wantedMessageID, returnAddress, to) {
				result = append(result, replyEvidence{
					JMAPID: email.ID, MessageID: email.MessageID, InReplyTo: email.InReplyTo,
					References: email.References, From: email.From, To: email.To, Subject: email.Subject,
				})
			}
		}
		position += len(query.IDs)
		if len(query.IDs) == 0 || position >= query.Total {
			return result, nil
		}
	}
}

func matchesReply(email jmap.Email, inReplyTo, returnAddress, to string) bool {
	return containsMessageID(email.InReplyTo, inReplyTo) &&
		containsAddress(email.From, returnAddress) &&
		(to == "" || containsAddress(email.To, to))
}

func normalizeMessageID(value string) string {
	return strings.Trim(strings.TrimSpace(value), "<>")
}

func containsMessageID(values []string, wanted string) bool {
	for _, value := range values {
		if normalizeMessageID(value) == wanted {
			return true
		}
	}
	return false
}

func containsAddress(addresses []jmap.Address, wanted string) bool {
	for _, address := range addresses {
		if strings.EqualFold(strings.TrimSpace(address.Email), strings.TrimSpace(wanted)) {
			return true
		}
	}
	return false
}

func printState(args []string) error {
	flags := flag.NewFlagSet("state", flag.ContinueOnError)
	statePath := flags.String("path", getenv("LINK_MAIL_STATE", filepath.Join(getenv("LINK_WORKSPACE", "/workspace"), ".link", "mail-state.json")), "state file")
	subject := flags.String("has-subject", "", "exit successfully only if this exact subject was observed")
	if err := flags.Parse(args); err != nil {
		return err
	}
	state, err := mailadapter.LoadState(*statePath)
	if err != nil {
		return err
	}
	if *subject != "" {
		for _, message := range state.Messages {
			if message.Subject == *subject {
				return nil
			}
		}
		return fmt.Errorf("subject %q has not been observed", *subject)
	}
	contents, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(contents))
	return nil
}

func getenv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
