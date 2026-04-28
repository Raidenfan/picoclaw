package email

import (
	"context"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
)

func TestIMAPConnection_Unconfigured(t *testing.T) {
	ch := &EmailChannel{
		BaseChannel: nil,
		config:      &config.EmailSettings{},
	}
	result := ch.TestIMAPConnection(context.Background())
	if result.Success {
		t.Error("expected failure for unconfigured IMAP")
	}
	if len(result.Steps) != 1 {
		t.Errorf("expected 1 step, got %d", len(result.Steps))
	}
	if result.Steps[0].Name != "connect" {
		t.Errorf("expected connect step, got %s", result.Steps[0].Name)
	}
}

func TestSMTPConnection_Unconfigured(t *testing.T) {
	ch := &EmailChannel{
		BaseChannel: nil,
		config:      &config.EmailSettings{},
	}
	result := ch.TestSMTPConnection(context.Background())
	if result.Success {
		t.Error("expected failure for unconfigured SMTP")
	}
	if len(result.Steps) != 1 {
		t.Errorf("expected 1 step, got %d", len(result.Steps))
	}
	if result.Steps[0].Name != "connect" {
		t.Errorf("expected connect step, got %s", result.Steps[0].Name)
	}
}

func TestIMAPConnection_WrongPassword(t *testing.T) {
	cfg := &config.Channel{Type: config.ChannelEmail}
	cfg.SetName("email")
	settings := &config.EmailSettings{
		IMAPServer:   "imap.gmail.com",
		IMAPPort:     993,
		IMAPTLS:      boolPtr(true),
		IMAPUser:     "test@example.com",
		IMAPPassword: plainSecureString("wrong-password"),
		Mailbox:      "INBOX",
	}
	ch, err := NewEmailChannel(cfg, settings, bus.NewMessageBus())
	if err != nil {
		t.Fatalf("NewEmailChannel() error = %v", err)
	}

	result := ch.TestIMAPConnection(context.Background())
	if result.Success {
		t.Error("expected failure with wrong password")
	}
	if result.Server != "imap.gmail.com:993" {
		t.Errorf("expected server imap.gmail.com:993, got %s", result.Server)
	}
	if len(result.Steps) < 2 {
		t.Errorf("expected at least 2 steps, got %d", len(result.Steps))
	}
}

func TestSMTPConnection_WrongPassword(t *testing.T) {
	cfg := &config.Channel{Type: config.ChannelEmail}
	cfg.SetName("email")
	settings := &config.EmailSettings{
		SMTPServer:   "smtp.gmail.com",
		SMTPPort:     465,
		SMTPTLS:      boolPtr(true),
		SMTPUser:     "test@example.com",
		SMTPPassword: plainSecureString("wrong-password"),
	}
	ch, err := NewEmailChannel(cfg, settings, bus.NewMessageBus())
	if err != nil {
		t.Fatalf("NewEmailChannel() error = %v", err)
	}

	result := ch.TestSMTPConnection(context.Background())
	if result.Success {
		t.Error("expected failure with wrong password")
	}
	if result.Server != "smtp.gmail.com:465" {
		t.Errorf("expected server smtp.gmail.com:465, got %s", result.Server)
	}
	if len(result.Steps) < 2 {
		t.Errorf("expected at least 2 steps, got %d", len(result.Steps))
	}
}

func plainSecureString(s string) config.SecureString {
	return *config.NewSecureString(s)
}

func boolPtr(b bool) *bool {
	return &b
}
