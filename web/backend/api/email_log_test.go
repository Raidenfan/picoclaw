package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	emailch "github.com/sipeed/picoclaw/pkg/channels/email"
)

func TestHandleListEmailLogReturnsNewestEntriesFirstBeforePagination(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "email-log.json")
	configPath := filepath.Join(dir, "config.json")

	entries := make([]emailch.EmailLogEntry, 0, 35)
	base := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 35; i++ {
		entries = append(entries, emailch.EmailLogEntry{
			ID:        fmt.Sprintf("entry-%02d", i),
			Timestamp: base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
			Direction: emailch.DirectionIn,
			FromEmail: fmt.Sprintf("sender-%02d@example.com", i),
			ToEmail:   "bot@example.com",
			Subject:   fmt.Sprintf("message %02d", i),
			BodyText:  "body",
		})
	}
	logData, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(logPath, logData, 0o600); err != nil {
		t.Fatalf("WriteFile(log) error = %v", err)
	}

	configData := []byte(fmt.Sprintf(`{
		"version": 3,
		"channel_list": {
			"email": {
				"enabled": true,
				"type": "email",
				"settings": {
					"log_file": %q
				}
			}
		}
	}`, logPath))
	if err := os.WriteFile(configPath, configData, 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	h := NewHandler(configPath)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/channels/email/log?page=1&page_size=2", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp struct {
		Entries []struct {
			ID string `json:"id"`
		} `json:"entries"`
		Total int `json:"total"`
		Page  int `json:"page"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if resp.Total != len(entries) {
		t.Fatalf("total = %d, want %d", resp.Total, len(entries))
	}
	if resp.Page != 1 {
		t.Fatalf("page = %d, want 1", resp.Page)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("entries len = %d, want 2", len(resp.Entries))
	}
	if resp.Entries[0].ID != "entry-35" || resp.Entries[1].ID != "entry-34" {
		t.Fatalf("entry ids = %#v, want entry-35, entry-34", resp.Entries)
	}
}
