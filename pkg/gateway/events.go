package gateway

import (
	"context"
	"time"

	"github.com/sipeed/picoclaw/pkg/agent"
	runtimeevents "github.com/sipeed/picoclaw/pkg/events"
)

const gatewayEventPublishTimeout = 100 * time.Millisecond

type gatewayEventPayload struct {
	DurationMS int64  `json:"duration_ms,omitempty"`
	Error      string `json:"error,omitempty"`
}

func publishGatewayEvent(
	al *agent.AgentLoop,
	kind runtimeevents.Kind,
	startedAt time.Time,
	err error,
) {
	if al == nil || al.RuntimeEventBus() == nil {
		return
	}

	severity := runtimeevents.SeverityInfo
	payload := gatewayEventPayload{}
	if !startedAt.IsZero() {
		payload.DurationMS = time.Since(startedAt).Milliseconds()
	}
	if err != nil {
		severity = runtimeevents.SeverityError
		payload.Error = err.Error()
	}

	ctx, cancel := context.WithTimeout(context.Background(), gatewayEventPublishTimeout)
	defer cancel()
	al.RuntimeEventBus().Publish(ctx, runtimeevents.Event{
		Kind:     kind,
		Source:   runtimeevents.Source{Component: "gateway"},
		Severity: severity,
		Payload:  payload,
	})
}
