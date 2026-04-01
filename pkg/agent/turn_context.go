package agent

import "github.com/sipeed/picoclaw/pkg/bus"

// TurnContext carries normalized turn-scoped facts that can be shared across
// events, hooks, and other runtime observers without re-parsing legacy fields.
type TurnContext struct {
	Inbound *bus.InboundContext `json:"inbound,omitempty"`
}

func newTurnContext(inbound *bus.InboundContext) *TurnContext {
	if inbound == nil {
		return nil
	}
	return &TurnContext{
		Inbound: cloneInboundContext(inbound),
	}
}

func cloneTurnContext(ctx *TurnContext) *TurnContext {
	if ctx == nil {
		return nil
	}
	cloned := *ctx
	cloned.Inbound = cloneInboundContext(ctx.Inbound)
	return &cloned
}

func cloneInboundContext(ctx *bus.InboundContext) *bus.InboundContext {
	if ctx == nil {
		return nil
	}
	cloned := *ctx
	cloned.ReplyHandles = cloneStringMap(ctx.ReplyHandles)
	cloned.Raw = cloneStringMap(ctx.Raw)
	return &cloned
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(src))
	for k, v := range src {
		cloned[k] = v
	}
	return cloned
}

func cloneEventMeta(meta EventMeta) EventMeta {
	meta.Context = cloneTurnContext(meta.Context)
	return meta
}
