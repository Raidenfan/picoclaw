package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels/email"
	"github.com/sipeed/picoclaw/pkg/config"
)

func (h *Handler) registerEmailTestRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/channels/email/test", h.handleTestEmailConnection)
}

type emailTestRequest struct {
	TestIMAP *bool `json:"test_imap"`
	TestSMTP *bool `json:"test_smtp"`
}

type emailTestResponse struct {
	IMAP *email.IMAPTestResult `json:"imap,omitempty"`
	SMTP *email.SMTPTestResult `json:"smtp,omitempty"`
}

func (h *Handler) handleTestEmailConnection(w http.ResponseWriter, r *http.Request) {
	var req emailTestRequest
	if r.Body != nil {
		limited := http.MaxBytesReader(w, r.Body, 4096)
		_ = json.NewDecoder(limited).Decode(&req)
	}

	testIMAP := req.TestIMAP == nil || *req.TestIMAP
	testSMTP := req.TestSMTP == nil || *req.TestSMTP

	settings, err := h.loadEmailSettings()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Create a temporary EmailChannel from the loaded settings.
	tmpChannel := &config.Channel{Type: config.ChannelEmail}
	tmpChannel.SetName("email")
	ch, err := email.NewEmailChannel(tmpChannel, settings, bus.NewMessageBus())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp := emailTestResponse{}

	if testIMAP {
		result := ch.TestIMAPConnection(ctx)
		resp.IMAP = result
	}

	if testSMTP {
		result := ch.TestSMTPConnection(ctx)
		resp.SMTP = result
	}

	writeJSON(w, http.StatusOK, resp)
}
