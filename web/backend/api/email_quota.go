package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sipeed/picoclaw/pkg/config"
)

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// quotaEntry represents a single email quota record stored on disk.
type quotaEntry struct {
	Name      string `json:"name"`
	Remaining int    `json:"remaining"`
	ExpiresAt string `json:"expires_at"`
}

// quotaFile is the on-disk representation of the quota store.
type quotaFile struct {
	mu   sync.Mutex
	path string
	data map[string]quotaEntry
}

// globalQuotaStore holds the in-memory cache of the quota file per config path.
var globalQuotaStores sync.Map // configPath -> *quotaFile

func newQuotaFile(configPath string) *quotaFile {
	if store, ok := globalQuotaStores.Load(configPath); ok {
		return store.(*quotaFile)
	}
	qf := &quotaFile{path: configPath, data: make(map[string]quotaEntry)}
	globalQuotaStores.Store(configPath, qf)
	return qf
}

func (qf *quotaFile) resolvePath(cfg *config.Config) string {
	// Find the email channel's quota_file setting
	for _, ch := range cfg.Channels {
		if ch.Type == config.ChannelEmail {
			decoded, err := ch.GetDecoded()
			if err != nil {
				return ""
			}
			if es, ok := decoded.(*config.EmailSettings); ok {
				if es.QuotaFile != "" {
					return config.ExpandPath(es.QuotaFile)
				}
			}
		}
	}
	return ""
}

func (qf *quotaFile) load() error {
	qf.mu.Lock()
	defer qf.mu.Unlock()

	data, err := os.ReadFile(qf.path)
	if err != nil {
		if os.IsNotExist(err) {
			qf.data = make(map[string]quotaEntry)
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		qf.data = make(map[string]quotaEntry)
		return nil
	}
	return json.Unmarshal(data, &qf.data)
}

func (qf *quotaFile) save() error {
	qf.mu.Lock()
	defer qf.mu.Unlock()

	dir := filepath.Dir(qf.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp := qf.path + ".tmp"
	data, err := json.MarshalIndent(qf.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, qf.path)
}

func (h *Handler) registerEmailQuotaRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/channels/email/quotas", h.handleGetEmailQuotas)
	mux.HandleFunc("POST /api/channels/email/quotas", h.handleCreateEmailQuota)
	mux.HandleFunc("PUT /api/channels/email/quotas/{email}", h.handleUpdateEmailQuota)
	mux.HandleFunc("DELETE /api/channels/email/quotas/{email}", h.handleDeleteEmailQuota)
}

type quotaListResponse struct {
	Quotas []quotaEntryResponse `json:"quotas"`
}

type quotaEntryResponse struct {
	Email     string `json:"email"`
	Name      string `json:"name"`
	Remaining int    `json:"remaining"`
	ExpiresAt string `json:"expires_at"`
}

func (h *Handler) handleGetEmailQuotas(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	qf := newQuotaFile(h.configPath)
	path := qf.resolvePath(cfg)
	if path == "" {
		writeJSON(w, http.StatusOK, quotaListResponse{Quotas: []quotaEntryResponse{}})
		return
	}

	qf.path = path
	if err := qf.load(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	entries := make([]quotaEntryResponse, 0, len(qf.data))
	for email, entry := range qf.data {
		entries = append(entries, quotaEntryResponse{
			Email:     email,
			Name:      entry.Name,
			Remaining: entry.Remaining,
			ExpiresAt: entry.ExpiresAt,
		})
	}

	writeJSON(w, http.StatusOK, quotaListResponse{Quotas: entries})
}

func (h *Handler) handleCreateEmailQuota(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "request body too large", http.StatusBadRequest)
		return
	}

	var req quotaEntryResponse
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Email == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email is required"})
		return
	}

	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	qf := newQuotaFile(h.configPath)
	path := qf.resolvePath(cfg)
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "quota_file not configured in email channel"})
		return
	}

	qf.path = path
	if err := qf.load(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if _, exists := qf.data[req.Email]; exists {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "email already exists"})
		return
	}

	qf.data[req.Email] = quotaEntry{
		Name:      req.Name,
		Remaining: req.Remaining,
		ExpiresAt: req.ExpiresAt,
	}

	if err := qf.save(); err != nil {
		http.Error(w, fmt.Sprintf("failed to save: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleUpdateEmailQuota(w http.ResponseWriter, r *http.Request) {
	email := strings.ToLower(r.PathValue("email"))
	if email == "" {
		http.Error(w, "email path parameter is required", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "request body too large", http.StatusBadRequest)
		return
	}

	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	qf := newQuotaFile(h.configPath)
	path := qf.resolvePath(cfg)
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "quota_file not configured in email channel"})
		return
	}

	qf.path = path
	if err := qf.load(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	entry, exists := qf.data[email]
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "email not found"})
		return
	}

	if name, ok := req["name"].(string); ok {
		entry.Name = name
	}
	if remaining, ok := req["remaining"].(float64); ok {
		entry.Remaining = int(remaining)
	}
	if expiresAt, ok := req["expires_at"].(string); ok {
		entry.ExpiresAt = expiresAt
	}

	qf.data[email] = entry

	if err := qf.save(); err != nil {
		http.Error(w, fmt.Sprintf("failed to save: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleDeleteEmailQuota(w http.ResponseWriter, r *http.Request) {
	email := strings.ToLower(r.PathValue("email"))
	if email == "" {
		http.Error(w, "email path parameter is required", http.StatusBadRequest)
		return
	}

	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	qf := newQuotaFile(h.configPath)
	path := qf.resolvePath(cfg)
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "quota_file not configured in email channel"})
		return
	}

	qf.path = path
	if err := qf.load(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if _, exists := qf.data[email]; !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "email not found"})
		return
	}

	delete(qf.data, email)

	if err := qf.save(); err != nil {
		http.Error(w, fmt.Sprintf("failed to save: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
