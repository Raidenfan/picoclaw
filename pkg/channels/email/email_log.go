package email

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
)

// EmailLogDirection represents the direction of email traffic.
type EmailLogDirection string

const (
	DirectionIn  EmailLogDirection = "in"
	DirectionOut EmailLogDirection = "out"
)

// EmailLogAttachment stores metadata only, not binary data.
type EmailLogAttachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	Path        string `json:"path,omitempty"`
}

// EmailLogEntry is a single log record for an email sent or received.
type EmailLogEntry struct {
	ID          string               `json:"id"`
	Timestamp   string               `json:"timestamp"`
	Direction   EmailLogDirection    `json:"direction"`
	FromEmail   string               `json:"from_email"`
	FromName    string               `json:"from_name"`
	ToEmail     string               `json:"to_email"`
	ToName      string               `json:"to_name"`
	Subject     string               `json:"subject"`
	MessageID   string               `json:"message_id"`
	BodyText    string               `json:"body_text"`
	Attachments []EmailLogAttachment `json:"attachments,omitempty"`
	Status      string               `json:"status,omitempty"`
}

type emailLogStore struct {
	path       string
	entries    []EmailLogEntry
	mu         sync.Mutex
	maxBody    int
	maxEntries int
}

func newEmailLogStore(path string, maxBody, maxEntries int) *emailLogStore {
	return &emailLogStore{
		path:       path,
		maxBody:    maxBody,
		maxEntries: maxEntries,
	}
}

// NewEmailLogStoreForAPI creates a store instance for use by the web API handlers.
func NewEmailLogStoreForAPI(path string, maxBody, maxEntries int) *emailLogStore {
	if maxBody <= 0 {
		maxBody = config.DefaultEmailLogMaxBodyBytes
	}
	if maxEntries <= 0 {
		maxEntries = config.DefaultEmailLogMaxEntries
	}
	return newEmailLogStore(path, maxBody, maxEntries)
}

// DeleteEntry removes a log entry by ID. This is a standalone operation (no cached state).
func (s *emailLogStore) DeleteEntry(id string) error {
	return s.deleteEntry(id)
}

func (s *emailLogStore) load() ([]EmailLogEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	var entries []EmailLogEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("corrupted email log file: %w", err)
	}
	return entries, nil
}

func (s *emailLogStore) appendEntry(entry EmailLogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.readEntriesLocked()
	if err != nil {
		entries = []EmailLogEntry{}
	}

	entries = append(entries, entry)
	entries = s.rotateEntries(entries)

	return s.writeEntriesLocked(entries)
}

func (s *emailLogStore) readEntriesLocked() ([]EmailLogEntry, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	var entries []EmailLogEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *emailLogStore) writeEntriesLocked(entries []EmailLogEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *emailLogStore) rotateEntries(entries []EmailLogEntry) []EmailLogEntry {
	if s.maxEntries > 0 && len(entries) > s.maxEntries {
		return entries[len(entries)-s.maxEntries:]
	}
	return entries
}

func (s *emailLogStore) deleteEntry(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.readEntriesLocked()
	if err != nil {
		return err
	}

	for i, e := range entries {
		if e.ID == id {
			entries = append(entries[:i], entries[i+1:]...)
			return s.writeEntriesLocked(entries)
		}
	}
	return fmt.Errorf("log entry not found")
}

func generateLogID() string {
	return time.Now().UTC().Format("20060102-150405") + "-" + randomHex(6)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "000000"
	}
	const hex = "0123456789abcdef"
	out := make([]byte, n)
	for i := range b {
		out[i] = hex[b[i]&0xf]
	}
	return string(out)
}

func truncateBody(body string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = config.DefaultEmailLogMaxBodyBytes
	}
	if len(body) <= maxBytes {
		return body
	}
	// Truncate at UTF-8 boundary
	truncated := body[:maxBytes]
	for len(truncated) > 0 && (truncated[len(truncated)-1]&0xc0) == 0x80 {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated
}

func mapToLogAttachments(attachments []emailAttachment) []EmailLogAttachment {
	result := make([]EmailLogAttachment, 0, len(attachments))
	for _, a := range attachments {
		result = append(result, EmailLogAttachment{
			Filename:    a.Filename,
			ContentType: a.ContentType,
			SizeBytes:   a.SizeBytes,
			Path:        a.LocalPath,
		})
	}
	return result
}

func (c *EmailChannel) initLogStore() {
	path := config.ExpandPath(c.config.LogFile)
	logger.InfoCF("email", "initLogStore called", map[string]any{
		"raw_log_file":  c.config.LogFile,
		"expanded_path": path,
	})
	if path == "" {
		return
	}
	maxBody := c.config.LogMaxBodyBytes
	if maxBody <= 0 {
		maxBody = config.DefaultEmailLogMaxBodyBytes
	}
	maxEntries := c.config.LogMaxEntries
	if maxEntries <= 0 {
		maxEntries = config.DefaultEmailLogMaxEntries
	}
	c.logStore = newEmailLogStore(path, maxBody, maxEntries)
}

func (c *EmailChannel) logEntry(entry EmailLogEntry) {
	if c.logStore == nil {
		return
	}
	if err := c.logStore.appendEntry(entry); err != nil {
		logger.WarnCF("email", "Failed to append email log entry", map[string]any{
			"channel": c.Name(),
			"id":      entry.ID,
			"error":   err.Error(),
		})
	}
}

func (c *EmailChannel) logInboundEmail(parsed *parsedEmail, status string) {
	if c.logStore == nil {
		logger.WarnCF("email", "logInboundEmail: logStore is nil, skipping", map[string]any{
			"from":    parsed.FromEmail,
			"subject": parsed.Subject,
			"status":  status,
		})
		return
	}
	entry := EmailLogEntry{
		ID:        generateLogID(),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Direction: DirectionIn,
		FromEmail: parsed.FromEmail,
		FromName:  parsed.FromName,
		ToEmail:   strings.TrimSpace(c.config.From),
		Subject:   parsed.Subject,
		MessageID: parsed.MessageID,
		BodyText:  truncateBody(parsed.Body, c.logStore.maxBody),
		Status:    status,
	}
	if len(parsed.Attachments) > 0 {
		entry.Attachments = mapToLogAttachments(parsed.Attachments)
	}
	c.logEntry(entry)
}

func (c *EmailChannel) logOutboundEmail(to, subject, body string, attachments []EmailLogAttachment) {
	if c.logStore == nil {
		return
	}
	from := strings.TrimSpace(c.config.From)
	if from == "" {
		from = strings.TrimSpace(c.config.SMTPUser)
	}
	entry := EmailLogEntry{
		ID:        generateLogID(),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Direction: DirectionOut,
		FromEmail: from,
		ToEmail:   to,
		Subject:   subject,
		BodyText:  truncateBody(body, c.logStore.maxBody),
	}
	if len(attachments) > 0 {
		entry.Attachments = attachments
	}
	c.logEntry(entry)
}
