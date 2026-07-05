package notifications

import (
	"context"
	"fmt"
	"time"

	"github.com/Astraxx04/pr-reviewer/pkg/logger"
)

// EmailSettings holds provider-agnostic email delivery settings, resolved per
// channel from EmailChannelConfig. All providers deliver over HTTPS — unlike
// SMTP, this isn't blocked by PaaS hosts (e.g. Render) that block outbound
// SMTP ports.
type EmailSettings struct {
	Provider string
	APIKey   string
}

func (s EmailSettings) configured() bool { return s.APIKey != "" }

// ResolveEmail returns the email settings and from address for a channel. The
// stored API key is decrypted here. Provider defaults to "resend" when unset,
// so configs written before Provider existed keep working.
func ResolveEmail(ec EmailChannelConfig) (EmailSettings, string) {
	provider := ec.Provider
	if provider == "" {
		provider = "resend"
	}
	return EmailSettings{
		Provider: provider,
		APIKey:   DecryptSecret(ec.APIKey),
	}, ec.From
}

// EmailProvider sends a single HTML email through a transactional email API.
// To add a provider, implement this and register it in emailProviders — no
// call site elsewhere needs to change.
type EmailProvider interface {
	Send(ctx context.Context, apiKey, from string, to []string, subject, htmlBody string) error
}

var emailProviders = map[string]EmailProvider{
	"resend": resendProvider{},
}

// SendEmail delivers an HTML email via the channel's configured provider. It
// returns a descriptive error on failure so callers — including the Test
// button — can surface exactly what went wrong.
func SendEmail(ctx context.Context, s EmailSettings, from string, to []string, subject, htmlBody string) error {
	if !s.configured() {
		return fmt.Errorf("email provider not configured (missing API key)")
	}
	if from == "" {
		return fmt.Errorf("from address not configured")
	}
	if len(to) == 0 {
		return nil
	}

	provider, ok := emailProviders[s.Provider]
	if !ok {
		return fmt.Errorf("unsupported email provider %q", s.Provider)
	}

	start := time.Now()
	err := provider.Send(ctx, s.APIKey, from, to, subject, htmlBody)
	logger.ExternalCall(ctx, s.Provider, "send", start, err, "recipients", len(to))
	return err
}
