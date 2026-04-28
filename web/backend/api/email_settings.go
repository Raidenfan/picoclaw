package api

import (
	"fmt"

	"github.com/sipeed/picoclaw/pkg/config"
)

func (h *Handler) loadEmailSettings() (*config.EmailSettings, error) {
	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		return nil, err
	}
	bc := cfg.Channels.GetByType(config.ChannelEmail)
	if bc == nil {
		return nil, fmt.Errorf("email channel is not configured")
	}
	decoded, err := bc.GetDecoded()
	if err != nil {
		return nil, err
	}
	settings, ok := decoded.(*config.EmailSettings)
	if !ok || settings == nil {
		return nil, fmt.Errorf("email settings are invalid")
	}
	config.ApplyEmailSettingsDefaults(settings)
	return settings, nil
}
