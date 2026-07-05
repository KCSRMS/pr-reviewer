package notifications

import (
	"context"

	"github.com/resend/resend-go/v3"
)

// resendProvider sends email through Resend's transactional email API.
type resendProvider struct{}

func (resendProvider) Send(ctx context.Context, apiKey, from string, to []string, subject, htmlBody string) error {
	client := resend.NewClient(apiKey)
	_, err := client.Emails.SendWithContext(ctx, &resend.SendEmailRequest{
		From:    from,
		To:      to,
		Subject: subject,
		Html:    htmlBody,
	})
	return err
}
