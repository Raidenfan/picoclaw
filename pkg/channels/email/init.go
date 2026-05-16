package email

import (
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
)

func init() {
	channels.RegisterFactory(
		config.ChannelEmail,
		func(channelName, _ string, cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
			bc := cfg.Channels[channelName]
			if bc == nil {
				return nil, nil
			}
			decoded, err := bc.GetDecoded()
			if err != nil {
				return nil, err
			}
			settings, ok := decoded.(*config.EmailSettings)
			if !ok {
				return nil, channels.ErrSendFailed
			}
			ch, err := NewEmailChannel(bc, settings, b)
			if err != nil {
				return nil, err
			}
			if channelName != bc.Name() {
				ch.SetName(channelName)
			}
			return ch, nil
		},
	)
}
