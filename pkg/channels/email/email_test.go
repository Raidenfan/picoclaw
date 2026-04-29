package email

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emersion/go-imap"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/media"
)

func TestPrefixReplySubject(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Hello", "Re: Hello"},
		{"Re: Hello", "Re: Hello"},
		{"FW: Hello", "FW: Hello"},
		{"", "Re: Your message"},
	}
	for _, tt := range tests {
		if got := prefixReplySubject(tt.in); got != tt.want {
			t.Fatalf("prefixReplySubject(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestComposeInboundContent(t *testing.T) {
	ch := newTestEmailChannel(t)
	msg := &parsedEmail{
		FromName:  "Alice",
		FromEmail: "alice@example.com",
		Subject:   "Question",
		MessageID: "<msg-1@example.com>",
		Date:      "Mon, 1 Jan 2026 00:00:00 +0000",
		Body:      "Hello world",
		ReplyPolicy: emailReplyPolicy{
			Allowed:   true,
			Remaining: 2,
			Reason:    "quota available",
		},
		Attachments: []emailAttachment{
			{Filename: "notes.txt", ContentType: "text/plain", SizeBytes: 12, Ref: "media://abc"},
		},
	}

	content, raw := ch.composeInboundContent(msg)
	if !strings.Contains(content, "Reply allowed: yes") {
		t.Fatalf("content missing reply policy: %s", content)
	}
	if !strings.Contains(content, "notes.txt") || !strings.Contains(content, "media://abc") {
		t.Fatalf("content missing attachment summary: %s", content)
	}
	if raw["email_from"] != "alice@example.com" {
		t.Fatalf("raw email_from = %q", raw["email_from"])
	}
	if raw["email_remaining_uses"] != "2" {
		t.Fatalf("raw email_remaining_uses = %q", raw["email_remaining_uses"])
	}
}

func TestInboundEmailLogUsesStoredAttachmentPath(t *testing.T) {
	ch := newTestEmailChannel(t)
	ch.SetMediaStore(media.NewFileMediaStore())
	logPath := filepath.Join(t.TempDir(), "email-log.json")
	ch.logStore = newEmailLogStore(logPath, config.DefaultEmailLogMaxBodyBytes, 10)

	if err := os.MkdirAll(media.TempDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	tmp, err := os.CreateTemp(media.TempDir(), "email-log-test-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmp.Name()
	t.Cleanup(func() { _ = os.Remove(tmpPath) })
	if _, err := tmp.WriteString("attachment body"); err != nil {
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}

	parsed := &parsedEmail{
		FromEmail: "alice@example.com",
		Subject:   "with attachment",
		MessageID: "<msg-attachment@example.com>",
		Body:      "see attached",
		Attachments: []emailAttachment{
			{
				Filename:    "notes.txt",
				ContentType: "text/plain",
				SizeBytes:   int64(len("attachment body")),
				TempPath:    tmpPath,
			},
		},
	}

	parsed.Attachments = ch.storeAttachments(parsed.Attachments, parsed.FromEmail, parsed.MessageID)
	ch.logInboundEmail(parsed, "")

	entries := readEmailLogEntriesForTest(t, logPath)
	if len(entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(entries))
	}
	if got := entries[0].Attachments[0].Path; got != tmpPath {
		t.Fatalf("attachment path = %q, want %q", got, tmpPath)
	}
	if _, err := os.Stat(entries[0].Attachments[0].Path); err != nil {
		t.Fatalf("logged attachment path should exist: %v", err)
	}
	if parsed.Attachments[0].Ref == "" {
		t.Fatal("stored attachment ref is empty")
	}
	if parsed.Attachments[0].TempPath != "" {
		t.Fatalf("stored attachment TempPath = %q, want empty", parsed.Attachments[0].TempPath)
	}
}

func TestRejectedInboundEmailLogOmitsTemporaryAttachmentPath(t *testing.T) {
	ch := newTestEmailChannel(t)
	logPath := filepath.Join(t.TempDir(), "email-log.json")
	ch.logStore = newEmailLogStore(logPath, config.DefaultEmailLogMaxBodyBytes, 10)

	tmp, err := os.CreateTemp(t.TempDir(), "email-log-denied-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}

	parsed := &parsedEmail{
		FromEmail: "blocked@example.com",
		Subject:   "blocked",
		Attachments: []emailAttachment{
			{Filename: "blocked.txt", ContentType: "text/plain", TempPath: tmpPath},
		},
	}

	ch.logInboundEmail(parsed, "用户限额不足，不能处理。")
	ch.cleanupTemporaryAttachments(parsed.Attachments)

	entries := readEmailLogEntriesForTest(t, logPath)
	if len(entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(entries))
	}
	if got := entries[0].Attachments[0].Path; got != "" {
		t.Fatalf("rejected attachment path = %q, want empty", got)
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("temporary attachment should be removed, stat err=%v", err)
	}
}

func TestEmailLogAttachmentForMediaPartUsesMediaStorePath(t *testing.T) {
	store := media.NewFileMediaStore()
	localPath := filepath.Join(t.TempDir(), "report.txt")
	const content = "report body"
	if err := os.WriteFile(localPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	ref, err := store.Store(localPath, media.MediaMeta{
		Filename:    "report.txt",
		ContentType: "text/plain",
		Source:      "test",
	}, "scope")
	if err != nil {
		t.Fatal(err)
	}

	attachment := emailLogAttachmentForMediaPart(store, bus.MediaPart{Ref: ref})
	if attachment.Path != localPath {
		t.Fatalf("path = %q, want %q", attachment.Path, localPath)
	}
	if attachment.SizeBytes != int64(len(content)) {
		t.Fatalf("size = %d, want %d", attachment.SizeBytes, len(content))
	}
	if attachment.Filename != "report.txt" {
		t.Fatalf("filename = %q, want report.txt", attachment.Filename)
	}
	if attachment.ContentType != "text/plain" {
		t.Fatalf("content type = %q, want text/plain", attachment.ContentType)
	}
}

func TestEvaluateReplyPolicy(t *testing.T) {
	dir := t.TempDir()
	quotaFile := filepath.Join(dir, "quotas.json")
	if err := os.WriteFile(quotaFile, []byte(`{"alice@example.com": {"name": "Alice", "remaining": 2, "expires_at": ""}, "bob@example.com": {"name": "Bob", "remaining": 0, "expires_at": ""}, "carol@example.com": {"name": "Carol", "remaining": -1, "expires_at": ""}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	ch := newTestEmailChannel(t)
	ch.config.EnableQuota = true
	ch.config.QuotaFile = quotaFile

	policy := ch.evaluateReplyPolicy("alice@example.com")
	if !policy.Allowed || policy.Remaining != 2 {
		t.Fatalf("alice policy = %+v", policy)
	}

	policy = ch.evaluateReplyPolicy("bob@example.com")
	if policy.Allowed {
		t.Fatalf("bob policy should be denied: %+v", policy)
	}

	policy = ch.evaluateReplyPolicy("carol@example.com")
	if !policy.Allowed || policy.Remaining != -1 {
		t.Fatalf("carol policy = %+v, want unlimited allowed", policy)
	}
	if ch.decrementQuotaFile("carol@example.com") {
		t.Fatal("unlimited quota should not be decremented")
	}
	data, err := os.ReadFile(quotaFile)
	if err != nil {
		t.Fatal(err)
	}
	var quotas map[string]emailQuotaEntry
	if err := json.Unmarshal(data, &quotas); err != nil {
		t.Fatal(err)
	}
	if quotas["carol@example.com"].Remaining != -1 {
		t.Fatalf("carol remaining = %d, want -1", quotas["carol@example.com"].Remaining)
	}
}

func TestQuotaAccessDisabled(t *testing.T) {
	ch := newTestEmailChannel(t)
	ch.config.EnableQuota = false

	if ok, reason := ch.checkQuotaAccess("anyone@example.com"); !ok {
		t.Fatalf("any sender should be allowed when quota disabled: %s", reason)
	}
}

func TestQuotaAccessEnabled(t *testing.T) {
	dir := t.TempDir()
	quotaFile := filepath.Join(dir, "quotas.json")
	if err := os.WriteFile(quotaFile, []byte(`{"alice@example.com": {"name": "Alice", "remaining": 1, "expires_at": ""}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	ch := newTestEmailChannel(t)
	ch.config.EnableQuota = true
	ch.config.QuotaFile = quotaFile

	if ok, _ := ch.checkQuotaAccess("alice@example.com"); !ok {
		t.Fatal("alice should be allowed")
	}
	if ok, _ := ch.checkQuotaAccess("unknown@example.com"); ok {
		t.Fatal("unknown should be denied")
	}
}

func TestNewEmailChannel(t *testing.T) {
	cfg := &config.Channel{Type: config.ChannelEmail}
	settings := &config.EmailSettings{
		SMTPServer: "smtp.example.com",
		IMAPServer: "imap.example.com",
	}
	ch, err := NewEmailChannel(cfg, settings, bus.NewMessageBus())
	if err != nil {
		t.Fatalf("NewEmailChannel() error = %v", err)
	}
	if ch.smtpAddr == "" || ch.imapAddr == "" {
		t.Fatalf("expected resolved addresses, got smtp=%q imap=%q", ch.smtpAddr, ch.imapAddr)
	}
}

func TestEmailTransportTLSFallback(t *testing.T) {
	trueValue := true
	falseValue := false

	tests := []struct {
		name        string
		settings    config.EmailSettings
		wantSMTPTLS bool
		wantIMAPTLS bool
	}{
		{
			name: "legacy tls applies to both transports",
			settings: config.EmailSettings{
				TLS: true,
			},
			wantSMTPTLS: true,
			wantIMAPTLS: true,
		},
		{
			name: "transport-specific flags override legacy tls",
			settings: config.EmailSettings{
				TLS:     true,
				SMTPTLS: &falseValue,
				IMAPTLS: &trueValue,
			},
			wantSMTPTLS: false,
			wantIMAPTLS: true,
		},
		{
			name:        "zero-value legacy tls stays disabled",
			settings:    config.EmailSettings{},
			wantSMTPTLS: false,
			wantIMAPTLS: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := emailSMTPImplicitTLS(&tt.settings); got != tt.wantSMTPTLS {
				t.Fatalf("emailSMTPImplicitTLS() = %t, want %t", got, tt.wantSMTPTLS)
			}
			if got := emailIMAPTLS(&tt.settings); got != tt.wantIMAPTLS {
				t.Fatalf("emailIMAPTLS() = %t, want %t", got, tt.wantIMAPTLS)
			}
		})
	}
}

func TestDefaultEmailSMTPUsesStartTLS(t *testing.T) {
	cfg := config.DefaultConfig()
	bc := cfg.Channels.Get(config.ChannelEmail)
	if bc == nil {
		t.Fatal("default email channel missing")
	}

	decoded, err := bc.GetDecoded()
	if err != nil {
		t.Fatalf("GetDecoded() error = %v", err)
	}
	settings, ok := decoded.(*config.EmailSettings)
	if !ok {
		t.Fatalf("decoded settings type = %T, want *config.EmailSettings", decoded)
	}
	if got := emailSMTPImplicitTLS(settings); got {
		raw, _ := json.Marshal(settings)
		t.Fatalf("default SMTP implicit TLS = true, want false; settings=%s", raw)
	}
	if !settings.SMTPStartTLS {
		t.Fatal("default SMTP STARTTLS = false, want true")
	}
}

func TestReplySubjectFallsBackToGenericSubject(t *testing.T) {
	ch := newTestEmailChannel(t)
	subject := ch.replySubject(bus.OutboundMessage{
		Content: "first line\nsecond line",
	})
	if subject != "Re: Your message" {
		t.Fatalf("replySubject() = %q, want generic fallback", subject)
	}
}

func TestBuildRawMessageSanitizesHeaders(t *testing.T) {
	ch := newTestEmailChannel(t)
	raw := string(ch.buildRawMessage(
		"Pico Bot <bot@example.com>",
		"alice@example.com",
		"Hello\nInjected: nope",
		"body",
		"<msg-1@example.com>\nInjected: nope",
		"<msg-1@example.com>\r\nInjected: nope",
	))
	if strings.Contains(raw, "\nInjected: nope") || strings.Contains(raw, "\r\nInjected: nope") {
		t.Fatalf("raw message contains unsanitized injected header:\n%s", raw)
	}
	if !strings.Contains(raw, "Subject:") || !strings.Contains(raw, "MIME-Version:") {
		t.Fatalf("raw message missing expected headers:\n%s", raw)
	}
}

func TestEnvelopeEmailAddressParsesDisplayAddress(t *testing.T) {
	if got := envelopeEmailAddress("Pico Bot <bot@example.com>"); got != "bot@example.com" {
		t.Fatalf("envelopeEmailAddress() = %q, want bot@example.com", got)
	}
	if got := envelopeEmailAddress("alice@example.com"); got != "alice@example.com" {
		t.Fatalf("envelopeEmailAddress() = %q, want alice@example.com", got)
	}
}

func TestReadAttachmentPartSkipsOversizedAttachment(t *testing.T) {
	ch := newTestEmailChannel(t)
	ch.config.MaxAttachmentSizeBytes = 4

	attachment, err := ch.readAttachmentPart(
		bytes.NewReader([]byte("0123456789")),
		"",
		emailAttachment{Filename: "big.bin", ContentType: "application/octet-stream"},
	)
	if err != nil {
		t.Fatalf("readAttachmentPart() error = %v", err)
	}
	if !attachment.Skipped {
		t.Fatalf("expected attachment to be skipped: %+v", attachment)
	}
	if attachment.TempPath != "" {
		t.Fatalf("oversized attachment should not keep a temp file: %+v", attachment)
	}
	if !strings.Contains(attachment.SkipReason, "max_attachment_size_bytes") {
		t.Fatalf("unexpected skip reason: %q", attachment.SkipReason)
	}
	if attachment.SizeBytes <= ch.config.MaxAttachmentSizeBytes {
		t.Fatalf("attachment size = %d, want > %d", attachment.SizeBytes, ch.config.MaxAttachmentSizeBytes)
	}
}

func TestStartFetchMessagesUsesFetcherOwnedChannelClose(t *testing.T) {
	messages := startFetchMessages(func(ch chan *imap.Message) error {
		ch <- &imap.Message{Uid: 42}
		close(ch)
		return nil
	}, 1)

	msg, ok := <-messages
	if !ok {
		t.Fatal("expected fetched message")
	}
	if msg == nil || msg.Uid != 42 {
		t.Fatalf("unexpected fetched message: %+v", msg)
	}

	if _, ok := <-messages; ok {
		t.Fatal("expected channel to be closed by fetcher")
	}
}

func newTestEmailChannel(t *testing.T) *EmailChannel {
	t.Helper()
	cfg := &config.Channel{Type: config.ChannelEmail}
	cfg.SetName("email")
	settings := &config.EmailSettings{SMTPServer: "smtp.example.com"}
	ch, err := NewEmailChannel(cfg, settings, bus.NewMessageBus())
	if err != nil {
		t.Fatalf("NewEmailChannel() error = %v", err)
	}
	ch.SetRunning(true)
	return ch
}

func readEmailLogEntriesForTest(t *testing.T, path string) []EmailLogEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entries []EmailLogEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	return entries
}
