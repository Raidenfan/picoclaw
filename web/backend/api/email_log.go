package api

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	emailch "github.com/sipeed/picoclaw/pkg/channels/email"
	"github.com/sipeed/picoclaw/pkg/config"
)

func (h *Handler) registerEmailLogRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/channels/email/log", h.handleListEmailLog)
	mux.HandleFunc("GET /api/channels/email/log/{id}", h.handleGetEmailLog)
	mux.HandleFunc("DELETE /api/channels/email/log/{id}", h.handleDeleteEmailLog)
}

type emailLogListResponse struct {
	Entries []emailLogSummary `json:"entries"`
	Total   int               `json:"total"`
	Page    int               `json:"page"`
}

type emailLogSummary struct {
	ID          string                      `json:"id"`
	Timestamp   string                      `json:"timestamp"`
	Direction   emailch.EmailLogDirection   `json:"direction"`
	FromEmail   string                      `json:"from_email"`
	FromName    string                      `json:"from_name"`
	ToEmail     string                      `json:"to_email"`
	ToName      string                      `json:"to_name"`
	Subject     string                      `json:"subject"`
	MessageID   string                      `json:"message_id"`
	HasBody     bool                        `json:"has_body"`
	Attachments []emailLogSummaryAttachment `json:"attachments,omitempty"`
}

type emailLogSummaryAttachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

func (h *Handler) handleListEmailLog(w http.ResponseWriter, r *http.Request) {
	settings, err := h.loadEmailSettings()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	path := config.ExpandPath(settings.LogFile)
	if path == "" {
		writeJSON(w, http.StatusOK, emailLogListResponse{Entries: []emailLogSummary{}})
		return
	}

	entries, err := loadEmailLogEntries(path)
	if err != nil {
		writeJSON(w, http.StatusOK, emailLogListResponse{Entries: []emailLogSummary{}})
		return
	}

	// Filter by direction
	direction := strings.TrimSpace(r.URL.Query().Get("direction"))
	if direction != "" && direction != "in" && direction != "out" {
		direction = ""
	}
	filtered := filterLogEntries(entries, direction)

	// Filter by search term
	search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
	if search != "" {
		filtered = searchLogEntries(filtered, search)
	}
	sortLogEntriesNewestFirst(filtered)

	// Pagination
	pageStr := strings.TrimSpace(r.URL.Query().Get("page"))
	page := 1
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	pageSizeStr := strings.TrimSpace(r.URL.Query().Get("page_size"))
	pageSize := 50
	if pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 {
			pageSize = ps
			if pageSize > 200 {
				pageSize = 200
			}
		}
	}

	total := len(filtered)
	startIdx := (page - 1) * pageSize
	if startIdx >= total {
		page = 1
		startIdx = 0
	}
	endIdx := startIdx + pageSize
	if endIdx > total {
		endIdx = total
	}

	// Convert newest-first page to summaries.
	pageSlice := filtered[startIdx:endIdx]

	summaries := make([]emailLogSummary, 0, len(pageSlice))
	for _, e := range pageSlice {
		summaries = append(summaries, entryToSummary(e))
	}

	writeJSON(w, http.StatusOK, emailLogListResponse{
		Entries: summaries,
		Total:   total,
		Page:    page,
	})
}

func (h *Handler) handleGetEmailLog(w http.ResponseWriter, r *http.Request) {
	settings, err := h.loadEmailSettings()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	path := config.ExpandPath(settings.LogFile)
	if path == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "email log not configured"})
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid log entry id"})
		return
	}

	entries, err := loadEmailLogEntries(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "email log not found"})
		return
	}

	for _, e := range entries {
		if e.ID == id {
			writeJSON(w, http.StatusOK, e)
			return
		}
	}

	writeJSON(w, http.StatusNotFound, map[string]string{"error": "log entry not found"})
}

func (h *Handler) handleDeleteEmailLog(w http.ResponseWriter, r *http.Request) {
	settings, err := h.loadEmailSettings()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	path := config.ExpandPath(settings.LogFile)
	if path == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "email log not configured"})
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid log entry id"})
		return
	}

	store := emailch.NewEmailLogStoreForAPI(path, settings.LogMaxBodyBytes, settings.LogMaxEntries)
	if err := store.DeleteEntry(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func loadEmailLogEntries(path string) ([]emailch.EmailLogEntry, error) {
	data, err := readLogJSON(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var entries []emailch.EmailLogEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func filterLogEntries(entries []emailch.EmailLogEntry, direction string) []emailch.EmailLogEntry {
	if direction == "" {
		return entries
	}
	result := make([]emailch.EmailLogEntry, 0, len(entries))
	for _, e := range entries {
		if string(e.Direction) == direction {
			result = append(result, e)
		}
	}
	return result
}

func searchLogEntries(entries []emailch.EmailLogEntry, query string) []emailch.EmailLogEntry {
	result := make([]emailch.EmailLogEntry, 0, len(entries))
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Subject), query) ||
			strings.Contains(strings.ToLower(e.FromEmail), query) ||
			strings.Contains(strings.ToLower(e.ToEmail), query) ||
			strings.Contains(strings.ToLower(e.FromName), query) ||
			strings.Contains(strings.ToLower(e.ToName), query) {
			result = append(result, e)
		}
	}
	return result
}

func entryToSummary(e emailch.EmailLogEntry) emailLogSummary {
	summary := emailLogSummary{
		ID:        e.ID,
		Timestamp: e.Timestamp,
		Direction: e.Direction,
		FromEmail: e.FromEmail,
		FromName:  e.FromName,
		ToEmail:   e.ToEmail,
		ToName:    e.ToName,
		Subject:   e.Subject,
		MessageID: e.MessageID,
		HasBody:   e.BodyText != "",
	}
	if len(e.Attachments) > 0 {
		summary.Attachments = make([]emailLogSummaryAttachment, 0, len(e.Attachments))
		for _, a := range e.Attachments {
			summary.Attachments = append(summary.Attachments, emailLogSummaryAttachment{
				Filename:    a.Filename,
				ContentType: a.ContentType,
				SizeBytes:   a.SizeBytes,
			})
		}
	}
	return summary
}

func sortLogEntriesNewestFirst(entries []emailch.EmailLogEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Timestamp != entries[j].Timestamp {
			return entries[i].Timestamp > entries[j].Timestamp
		}
		return entries[i].ID > entries[j].ID
	})
}

func readLogJSON(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	return data, nil
}
